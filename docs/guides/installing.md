# Installing the operator

The operator ships as a Helm chart and an image, both signed, from every
release ([RELEASING.md](../../RELEASING.md) shows how to verify them). This
page installs it on a cluster you run companies on, and says which of the
chart's values matter there. [Getting started](../getting-started.md) does
the same on a laptop, from source.

## Before you install

- Kubernetes on one of its four newest minor releases (1.34 to 1.37 today);
  the chart's admission policies are ValidatingAdmissionPolicies, built into
  the API server.
- A CNI that enforces NetworkPolicy, for the isolation around each engine
  ([Exposure and network policy](exposure.md#network-policy)).
- A default StorageClass, or the one your EngineClasses name, that provisions
  `ReadWriteOnce` volumes of at least 7Gi.
- A way to take each company's backups: the operator takes none, and the
  engine trims its logs only behind a recent one, refusing writes once they
  are full ([Backups](../concepts/runtime.md#backups)).
- For webhook routes: the Gateway API and an implementation of it, or an
  Ingress controller ([Exposure](exposure.md)). The operator detects the
  Gateway API when it starts.

Nothing else: there are no admission webhooks, so no cert-manager.

## What the operator may do

A Company may live in any namespace, so the manager's ClusterRole lets it
create, change and delete Secrets, ServiceAccounts, Services, StatefulSets,
volume claims, NetworkPolicies and routes in every namespace, and read every
Secret. RBAC cannot narrow that to the objects the operator makes, so the
chart and `dist/install.yaml` also install a ValidatingAdmissionPolicy,
`crewlet-operator-manager-writes`, which holds the manager's ServiceAccount
to them: every object it writes, changes or deletes carries
`app.kubernetes.io/managed-by: crewlet-operator` (a Secret it adopts may gain
the label, with its data unchanged), every Secret it writes is `Opaque`, and
every StatefulSet runs its pods as a ServiceAccount named `*-engine`, without
the node's network, PID or IPC namespaces, host paths or privileged
containers. Whoever takes over the manager's token therefore cannot mint a
token for another ServiceAccount or run a pod on a node's terms. It can
still read every Secret in the cluster, and run an engine-shaped pod with any
image as a company's engine ServiceAccount: protect the manager's namespace
as you would any controller with cluster-wide Secret read.

The policy matches the manager's ServiceAccount by the end of its name,
`crewlet-operator-controller-manager`; a `nameOverride` or `fullnameOverride`
that changes it leaves the manager unchecked.

## Install

```bash
helm install crewlet-operator oci://ghcr.io/crewlet/charts/crewlet-operator \
  --version <version> \
  --namespace crewlet-operator-system --create-namespace
```

Until the first release is published, install the chart from a checkout
with an image you build: `make docker-build docker-push helm-deploy
IMG=<your registry>/crewlet-operator:<tag>`.

The chart installs the CRDs and keeps them on uninstall (`crd.keep`), since
deleting a CRD deletes every company declared with it.

## Values worth setting

Object names below are those of a release named `crewlet-operator`; the
chart prefixes them with the release's name otherwise.

Every value is described in the chart's
[`values.yaml`](../../dist/chart/values.yaml). These are the ones a production
install decides on:

| Value | Default | Why |
|---|---|---|
| `manager.replicas` | `2` | One replica reconciles and holds a Lease; the other takes over when it goes, after at most the lease duration. The manager elects a leader by default; never pass `--leader-elect=false` in `manager.args` with more than one replica. The replicas prefer separate nodes (`manager.affinity`). |
| `manager.resources` | 50m CPU and 128Mi requested, 512Mi memory limit | The manager caches every Company, its objects and its StatefulSet; raise the memory for thousands of companies. The engines are pods of their own, sized by their EngineClass. |
| `metrics.enabled`, `metrics.secure` | `true`, `true` | Metrics on `:8443` over HTTPS, every request authenticated and authorised against the Kubernetes API. A scraper needs the ClusterRole `crewlet-operator-metrics-reader`. |
| `prometheus.enabled` | `false` | A ServiceMonitor, for the Prometheus Operator. `certManager.enabled` makes it verify the endpoint's certificate from a Secret you issue. |
| `rbac.helpers.enabled` | `false` | Per-kind admin, editor and viewer ClusterRoles, to grant on one kind at a time. |
| `tenantProvisioner.enabled`, `.subjects` | `false`, `[]` | The ClusterRole for a control plane that provisions tenants, and whom to bind it to; see below. |

### Who may write crewlet.ai objects

Two ClusterRoles are always installed and aggregated into Kubernetes' own
roles, so that people work with crewlet.ai objects with the roles they
already have:

| ClusterRole | Aggregated into | Grants |
|---|---|---|
| `crewlet-operator-crewlet-edit` | `admin`, `edit` | Every namespaced crewlet.ai kind: create, update, patch, delete |
| `crewlet-operator-crewlet-view` | `admin`, `edit`, `view` | Every namespaced crewlet.ai kind and its status: get, list, watch |

So whoever may edit a namespace may run a company in it, with the same
limits on what it costs that the namespace's other workloads have
(ResourceQuotas apply to the engine's pod and volume). EngineClasses are not
in either role: they are the platform's, written by its administrators.
Whoever may write a Company may name any class, and the class brings what it
references with it: the platform's model keys, telemetry headers and env
values, the workload identity on the engine's ServiceAccount, and its
scheduling onto the platform's nodes. A class that carries any of that should
admit only the namespaces it is for, with `spec.allowedNamespaces`, a
namespace label selector; a Company elsewhere reports `Accepted=False
ClassNotAllowed`. Select by a label only the platform can set: a namespace's
editors cannot change its labels, since Namespaces are cluster-scoped.

The reverse holds too: **writing EngineClasses is as good as reading any
Secret in the cluster.** A class may reference a Secret in any namespace
(model keys, telemetry headers, env values), and the operator copies it into
each of its companies' namespaces or engines, where the class's author can
read it through a Company of their own. Grant the `engineclass-editor-role`
helper only to whoever may read every Secret.

### A tenant provisioner

A platform that hosts tenants usually has a control plane that creates each
tenant's namespace, its crewlet.ai objects and the Secrets they reference
([Hosting many tenants on the operator](multi-tenancy.md)). The chart has roles for it, off
by default:

```yaml
tenantProvisioner:
  enabled: true
  subjects:
    - kind: ServiceAccount
      name: control-plane
      namespace: platform
  namespaceLabels:
    tenant: crewlet.ai/tenant
    required:
      pod-security.kubernetes.io/enforce: restricted
    protectedPrefixes:
      - pod-security.kubernetes.io/
```

The `crewlet-operator-tenant-provisioner` ClusterRole, bound to `subjects`,
lets the control plane create namespaces, bind the
`crewlet-operator-tenant-access` ClusterRole in them, and read the crewlet.ai
objects and EngineClasses. It holds no Secret access of its own: the
tenant-access role, once bound in a tenant's namespace, grants Secrets and
the crewlet.ai kinds there. A role that could write Secrets or relabel
namespaces everywhere would be as good as cluster-admin, so the chart also
installs a ValidatingAdmissionPolicy that holds the `subjects`, and no one
else, to this:

- a namespace is created with the tenant label and, under the protected
  prefixes, exactly the `required` labels;
- only namespaces with the tenant label are changed or deleted, the tenant
  label is never removed, and labels under the protected prefixes are never
  set, changed or removed;
- RoleBindings are made only in tenants' namespaces, and only to the
  tenant-access role;
- Secrets are written only in tenants' namespaces, and never as
  `kubernetes.io/service-account-token`, whose token the API server would
  fill in for any ServiceAccount.

Add the prefix of any namespace label that grants something (a Gateway's
`allowedRoutes` selector, an EngineClass's `allowedNamespaces`) to
`protectedPrefixes` when the control plane is not the one to set it.
`subjects` is required when the provisioner is enabled, since the policy
matches by them.

### Not supported

The operator is cluster-scoped: it reads the cluster-scoped EngineClasses and
runs companies in every namespace, so it cannot run with a namespaced Role.
The chart refuses `rbac.namespaced`. To keep tenants apart, give each its own
namespace and isolate the engines with the class's network policy.

## Upgrade

```bash
helm upgrade crewlet-operator oci://ghcr.io/crewlet/charts/crewlet-operator \
  --version <new> --namespace crewlet-operator-system
```

The chart's CRDs are ordinary templates rather than files in `crds/`, so
`helm upgrade` updates them along with the manager. Upgrading the operator
restarts an engine only when the new release changes what its pod is given.
A new engine version is a change of the EngineClass's `engine.image`: each
company's data pod rolls to it as the operator reconciles that company.
