# Installing the Operator

Install the operator with its signed Helm chart, run the engine it was tested
against, and decide the few values a production cluster needs.

The operator ships as a Helm chart and an image, both signed, from every
release ([RELEASING.md](../../RELEASING.md) shows how to verify them). This
page installs it on a cluster you run companies on. The
[Quickstart](../getting-started.md) does the same on a laptop, from source.

## Before you install

- Kubernetes 1.37. The operator supports one minor release of Kubernetes,
  the one it is tested against: a release supports the minor its tests ran
  on, which its release notes name, and the operator moves to each new minor
  once kind and envtest, which the tests run on, ship it. A cluster on any
  other minor is not supported. The chart's admission policies are
  ValidatingAdmissionPolicies, built into the API server.
- A CNI that enforces NetworkPolicy, for the isolation around each engine
  ([Exposure and network policy](exposure.md#network-policy)).
- A default StorageClass, or the one your EngineClasses name, that provisions
  `ReadWriteOnce` volumes of at least 12Gi (9Gi for an EngineClass that keeps
  its files in S3).
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
PodDisruptionBudgets, volume claims, NetworkPolicies and routes in every
namespace, and read every Secret. RBAC cannot narrow that to the objects the operator makes, so the
chart and `dist/install.yaml` also install a ValidatingAdmissionPolicy,
`crewlet-operator-manager-writes`, which holds the manager's ServiceAccount
to them: every object it writes, changes or deletes carries
`app.kubernetes.io/managed-by: crewlet-operator` (a Secret it adopts may gain
the label, with its data unchanged), every Secret it writes is `Opaque`, and
every StatefulSet runs its pods as a ServiceAccount named `*-engine`, without
the node's network, PID or IPC namespaces, host paths or privileged
containers, and every PodDisruptionBudget and NetworkPolicy selects a
company's engine pods (by `app.kubernetes.io/name: crewlet`, a component of
`data` or `seats`, and `crewlet.ai/company`) and nothing else. Whoever takes
over the manager's token therefore cannot mint a token for another
ServiceAccount, run a pod on a node's terms, hold up every node drain with a
budget over the cluster's DNS, or cut off a namespace with a policy that
selects all of its pods. It can
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

### Engine image

Each EngineClass names the engine image its companies run
(`spec.engine.image`). Every model a seat runs on is a coding CLI the engine
starts in its own container, and Crewlet's image ships none, so the image a
class names to run working seats is one built from the engine's with the
CLI its companies' providers drive
([The image needs the CLI](cli-agents.md#the-image-needs-the-cli)). The
operator is tested against Crewlet's `main`, as
Crewlet publishes it at `ghcr.io/crewlet/crewlet:main`: every test suite runs
that image, or the Crewlet commit it was built from, so the operator writes
the data pod's `crewlet.yaml` in the settings of the engine `main` runs, and
an engine refuses at start a setting it does not know. Crewlet builds the
image after a push to its `main`, for the newest commit (a merge that starts
no build, such as a Dependabot bump, waits for the next push), for
linux/amd64 and linux/arm64, labels it with the commit
(`org.opencontainers.image.revision`) and signs it from its workflow.

**With a release of the operator**, run the engine the release was tested
against. Each release copies the image its tests ran into
`ghcr.io/crewlet/crewlet-operator/engine`, under the release's version, and
signs the copy from its own workflow, as it signs the operator's image; the
release's notes name it by tag and digest, with the Crewlet commit it was
built from:

```bash
cosign verify ghcr.io/crewlet/crewlet-operator/engine:v0.2.0 \
  --certificate-identity-regexp '^https://github.com/crewlet/crewlet-operator/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

```yaml
spec:
  engine:
    image: ghcr.io/crewlet/crewlet-operator/engine:v0.2.0
```

Move the engine when you move the operator, to the engine of the release
you move to. Its tag is a semantic version, so the operator orders it: once a
company has run one release's engine, a class that names an older release's
is refused with `DowngradeRefused`, since the engine migrates its data
forward only ([Changes, versions and
suspension](../concepts/runtime.md#changes-versions-and-suspension)).

**With an operator built from a commit of this repository**, run Crewlet's
`main` image, which that commit's CI tested on the day it ran. Check its
signature with cosign 3, or with cosign 2 and `--new-bundle-format`, since
Crewlet signs it in cosign 3's bundle format. `main` moves at a push and is
signed a moment later, so a check right after a push can fail until the
signature lands; try again a minute later.

```bash
cosign verify ghcr.io/crewlet/crewlet:main \
  --certificate-identity 'https://github.com/crewlet/crewlet/.github/workflows/main-image.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

`main` is a tag that moves, and GHCR deletes the image it named once `main`
has moved on and that image is an hour old, which is usually minutes after
the next push to Crewlet's `main`
([Images from main](https://github.com/crewlet/crewlet/blob/main/RELEASING.md#images-from-main)),
so no digest of it can be pinned for long. To try the operator out, name the
tag, as the [Quickstart](../getting-started.md) does. For anything longer,
copy the image into a registry of your own while it is still `main`, both
platforms with it, and name the copy by digest. `docker buildx imagetools
create` copies the image index as it is, so the copy has the digest you
verified. It does not copy Crewlet's signature, which stays in GHCR with the
original and goes when GHCR deletes it, so verify the image before you copy
it, as here:

```bash
digest="$(docker buildx imagetools inspect ghcr.io/crewlet/crewlet:main --format '{{.Manifest.Digest}}')"
cosign verify "ghcr.io/crewlet/crewlet@$digest" \
  --certificate-identity 'https://github.com/crewlet/crewlet/.github/workflows/main-image.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
commit="$(docker buildx imagetools inspect "ghcr.io/crewlet/crewlet@$digest" \
  --format '{{index (index .Image "linux/amd64").Config.Labels "org.opencontainers.image.revision"}}')"
docker buildx imagetools create --tag "<your registry>/crewlet:$commit" "ghcr.io/crewlet/crewlet@$digest"
```

and in the class:

```yaml
spec:
  engine:
    image: <your registry>/crewlet@sha256:<digest>   # crewlet <commit>
```

Move the engine when you move the operator: an operator built from a commit
of this repository was tested against the `main` of the day its CI ran,
which the CI jobs' logs name (the `Crewlet engine:` lines). An image named by
digest, by `main` or by a commit is no semantic version, so the operator
cannot tell an older Crewlet commit from a newer one, and `DowngradeRefused`
never protects a class that names one: never point such a class at an older
commit than its companies have run. To go back, restore the data volume from
a backup taken before the newer engine first ran on it.

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

> **Caution: writing EngineClasses is as good as reading any Secret in the
> cluster.** A class may reference a Secret in any namespace (model keys,
> telemetry headers, env values), and the operator copies it into each of its
> companies' namespaces or engines, where the class's author can read it
> through a Company of their own. Grant the `engineclass-editor-role` helper
> only to whoever may read every Secret.

### A tenant provisioner

A platform that hosts tenants usually has a control plane that creates each
tenant's namespace, its crewlet.ai objects and the Secrets they reference
([Hosting many tenants on the operator](multi-tenancy.md)). The chart has
roles for it, off by default:

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

## See also

- [Quickstart](../getting-started.md) — the same on a laptop, from source
- [Exposure and Network Policy](exposure.md) — routes and the isolation around each engine
- [Hosting Many Tenants on the Operator](multi-tenancy.md) — the tenant provisioner role in use
