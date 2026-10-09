# Installing the operator

The operator ships as a Helm chart and an image, both signed, from every
release ([RELEASING.md](../../RELEASING.md) shows how to verify them). This
page installs it on a cluster you run companies on, and says which of the
chart's values matter there. [Getting started](../getting-started.md) does
the same on a laptop, from source.

## Before you install

- Kubernetes on one of its four newest minor releases (1.34 to 1.37 today).
- A CNI that enforces NetworkPolicy, for the isolation around each engine
  ([Exposure and network policy](exposure.md#network-policy)).
- A default StorageClass, or the one your EngineClasses name, that provisions
  `ReadWriteOnce` volumes of at least 7Gi.
- For webhook routes: the Gateway API and an implementation of it, or an
  Ingress controller ([Exposure](exposure.md)). The operator detects the
  Gateway API when it starts.

Nothing else: there are no admission webhooks, so no cert-manager.

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
([Hosting many tenants on the operator](multi-tenancy.md)). The chart has a ClusterRole for
it, off by default:

```yaml
tenantProvisioner:
  enabled: true
  subjects:
    - kind: ServiceAccount
      name: control-plane
      namespace: platform
```

It grants namespaces, Secrets and every namespaced crewlet.ai kind with its
status, cluster-wide, and read access to EngineClasses. Secrets in every
namespace is a lot: bind it to the control plane's own identity and nothing
else. Leave `subjects` empty to install the role alone and bind it yourself.

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
