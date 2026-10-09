# How a company's engine runs

For every accepted `Company` the operator runs one Crewlet engine: a
StatefulSet with one data pod, which holds the company's estate, serves the
engine's API, runs every seat and runs the background workers. This page
describes what the operator creates for it, how it is reached, how it changes,
and what happens when the Company is suspended or deleted.

An engine starts unconfigured, and its pod is not ready until the config
reconciler has applied the company document: [How the document is
applied](applying.md) describes that half.

## What the operator creates

Every object is in the Company's namespace, named after the Company, labelled
`app.kubernetes.io/*` and `crewlet.ai/company: <name>`, and owned by the
Company, so it is deleted with it. For a Company `nimbus`:

| Object | What it is |
|---|---|
| Secret `nimbus-keyring` | The keyring of the engine's encrypted secret store: one key per keyring key, named by the key's id (`key-1`), holding base64 of 32 random bytes, the form `crewlet secrets keygen` prints. The annotation `crewlet.ai/active-key-id` names the key that seals new writes. Generated once and never rewritten. |
| Secret `nimbus-api-tokens` | Two bearer tokens for the engine's API, `operator` (the operator's own, never handed out) and `access` (for the company's consumers). Generated once and never rewritten. |
| Secret `nimbus-api-access` | For consumers: the `access` token under `token` and the API's in-cluster URL under `url`. `status.apiAccessSecretRef` names it. |
| Secret `nimbus-platform` | Copies of the values the EngineClass takes from the platform's own Secrets: the S3 credentials, the telemetry headers, and `podTemplate.spec.env` values from Secrets. Created only when the class has any. |
| ConfigMap `nimbus-bootstrap-data` | The data pod's `crewlet.yaml`, rendered as [What the engine is given](company-document.md#the-data-pods-crewletyaml) describes. It holds `${VAR}` references only, never a value. |
| ServiceAccount `nimbus-engine` | The engine's identity, carrying the class's `serviceAccount.annotations` for a workload identity. Its token is never mounted: the engine does not call the Kubernetes API. |
| StatefulSet `nimbus-data` | The data pod. |
| Service `nimbus-engine` | ClusterIP on port 8080, routing to ready data pods. The routes and `status.endpoints.api` point at it. |
| Service `nimbus-data` | Headless, the StatefulSet's governing Service, publishing each data pod's address before it is ready. |
| HTTPRoute or Ingress `nimbus-webhooks`, `nimbus-api` | The routes the class asks for; see [Exposure](#exposure). |
| NetworkPolicy `nimbus` | With the class's `networkPolicy: Isolated`; see [Network policy](#network-policy). |

The keyring and the tokens are the only objects the operator generates rather
than derives. If a Secret of that name already exists it is used as it is: one
that no object controls is adopted (a [Retain](#deletion) deletion of an
earlier Company of the same name leaves its keyring behind), and one another
controller owns, such as an ExternalSecret, is left to it. That is how a
platform supplies the keyring from its own secret store instead.

Two cases are refused rather than guessed at:

- A keyring Secret that disappears while the company's data StatefulSet or
  data volume claims exist is not generated again: the pods would roll onto
  keys that cannot open the secret store and the configuration revisions
  sealed on the volume. The operator holds the pod back with
  `RuntimeReady=False`, reason `KeyringMissing`, until the Secret is restored
  from a backup (with its `crewlet.ai/active-key-id` annotation), or until
  the claims are deleted, which discards the data; it then generates a new
  keyring. Back the keyring Secret up with the volumes.
- A keyring or tokens Secret that an earlier Company of the same name still
  controls (one deleted with `deletionPolicy: Delete` moments ago, whose
  objects the garbage collector has not removed yet) is not used: it is about
  to be deleted. The pod waits, with `RuntimeReady=False`, reason
  `PreviousCompanyObjects`, and starts once the Secret is gone. The objects
  the operator writes whole (Services, the API access Secret, routes) are
  simply taken over from the earlier Company.

## The data pod

| | |
|---|---|
| Image | The class's `engine.image`; with `engine.allowVersionOverride`, its tag replaced by the Company's `spec.runtime.version`. |
| Command | `crewlet run -config /etc/crewlet/crewlet.yaml`, the bootstrap ConfigMap mounted read-only. |
| Storage | One volume claim per pod (`data-nimbus-data-0`) at `/var/lib/crewlet`, from the class's `data.storage`: the store, the event stream (three quarters of the volume, [sized from the claim](company-document.md#the-data-pods-crewletyaml)), and the store's native library cache (`TURSO_GO_CACHE_DIR`), so a restart does not extract it again. |
| Environment | Exactly the variables the bootstrap document references, each from a Secret or the pod's name; `HOME`; the OpenTelemetry variables when the class sets `telemetry`; then the class's `podTemplate.spec.env` and the Company's `runtime.env`, merged by name. |
| Probes | `/health` for startup (up to five minutes for the first start) and liveness, `/ready` for readiness. `/health` answers while the engine drains, so liveness never kills a pod finishing its turns. |
| Stopping | A five-second `preStop` sleep, so the endpoint is gone from the Services and Gateways before the engine drains, and the class's `data.terminationGracePeriodSeconds` (default five minutes) for the drain: running turns finish, waiting ones are handed back. |
| Updates | `RollingUpdate` with `maxUnavailable: 1`, `podManagementPolicy: Parallel`. |
| Scheduling | The class's `podTemplate`: labels, annotations, node selector, tolerations, affinity, topology spread, runtime and priority class; and its `data.resources`. |

The pod runs under the `restricted` Pod Security Standard: as the image's
user (uid 10001, `runAsNonRoot`), with the `RuntimeDefault` seccomp profile,
no privilege escalation and every capability dropped. Its root filesystem is
read-only. The engine writes only to its data volume, its home and `/tmp`;
the processes it spawns (git, stdio MCP servers, coding agents) write to the
latter two, which are `emptyDir` volumes, and `HOME` is set explicitly so an
image built on another base still has a writable one.

The pod template carries two annotations that roll the pod when what it reads
at start changes: `crewlet.ai/bootstrap-hash`, the hash of `crewlet.yaml`, and
`crewlet.ai/secrets-version`, the `resourceVersion`s of the keyring, token and
platform Secrets. Kubernetes reads a Secret into an environment variable only
when the container starts, so a token or platform credential that changes
takes effect by a rollout. A Secret the Company's own `runtime.env` references
is read the same way, but the operator does not watch it: restart the pod after
changing one.

## Changes, versions and suspension

A change to the class or the Company that changes the pod (an image, the
resources, a toleration, the bootstrap document) rolls the data pod. The data
volume is the exception: a StatefulSet's volume claims cannot change, so a
class's `data.storage` applies to companies created after it changes, and an
existing company keeps its volume.

The engine migrates its data forward only. The operator records the image the
data pod last ran *and was ready on* in `status.engine.image`, and its tag in
`status.engine.version`. A Company or class that asks for an image whose tag is
an older semantic version is refused with `RuntimeReady=False`, reason
`DowngradeRefused`; the pod keeps running the newer image, and every other
change still applies. A version that never became ready is never recorded, so
an upgrade that failed can be taken back. Tags that are not versions (`latest`)
are not compared. `spec.runtime.version` on a class without
`allowVersionOverride` is refused the same way, with
`VersionOverrideNotAllowed`.

`spec.suspend: true` scales the StatefulSet to zero and keeps the volume claim
(the StatefulSet's retention policy keeps claims on scale-down whatever
`deletionPolicy` says). The Company reports `Ready=False` and
`RuntimeReady=False`, both with the reason `Suspended`. Setting it back starts
the engine on the same volume. Suspending takes effect whatever else is wrong
with the Company: one that is not `Accepted` (its class is gone, or it is over
its class's limits) is still scaled to zero, so a tenant cannot keep a
suspended engine running by breaking its own Company. `Ready` says the engine
is stopped only once `RuntimeReady` reports it for the current generation.

When the operator cannot give the pod what it needs, it holds the pod back
and keeps what runs: a keyring Secret it cannot read as a keyring
(`KeyringInvalid`; the operator never rewrites a keyring, since the engine's
sealed data opens only with its keys), a platform Secret the class references
that is missing, or a token missing from a tokens Secret the platform
supplied (`SecretNotFound`). Suspending still scales the
pod to zero meanwhile.

## Exposure

Vendors deliver webhooks to `/webhooks/`. Everything else the engine serves on
port 8080 (its configuration, its secret store, the dashboard) stays inside the
cluster unless the class routes the API through an internal gateway.

| Class `exposure.webhooks.mode` | What the operator creates | `status.endpoints.publicBaseURL` |
|---|---|---|
| `Gateway` | HTTPRoute `<name>-webhooks` on the class's Gateways: the expanded `hostname`, path prefix `/webhooks/` only, to the engine Service | `spec.exposure.publicBaseURL`, else `https://<hostname>` |
| `Ingress` | Ingress `<name>-webhooks`: the class's IngressClass, annotations and TLS Secret, the expanded `hostname`, path prefix `/webhooks/` only | the same |
| `External` | Nothing: something outside the operator forwards `/webhooks/` to `<name>-engine:8080` | `spec.exposure.publicBaseURL`, which is required |
| `None` | Nothing | unset |

`exposure.api.mode` `Gateway` or `Ingress` creates `<name>-api` the same way
for every path, and `status.endpoints.exposedAPI` is its address once the route
is accepted: `https://` when the Ingress has TLS or a Gateway listener the
route attaches to is HTTPS. `status.endpoints.api` is always the in-cluster
address, `http://<name>-engine.<namespace>.svc:8080`.

The `Exposed` condition is `True` with the reason `RouteAccepted` once every
route exists and was accepted: an HTTPRoute when each Gateway it names reports
it `Accepted`, an Ingress when its controller published an address for it.
Until then it is `False` with `RouteNotAccepted` and what the controller
reported. A class that routes nothing gives `Exposed=True` with `NoRoutes`.
`External` without the Company's `publicBaseURL` is `PublicBaseURLMissing`.

The Gateway API is optional. The operator checks for its HTTPRoute kind when
it starts and runs without it; a class that routes through Gateways is then
reported as `GatewayAPINotInstalled`. Install the Gateway API and restart the
operator to use it. An HTTPRoute in a tenant's namespace attaches to a Gateway
in another only if the Gateway's listener admits routes from that namespace
(`allowedRoutes.namespaces`).

## Network policy

With the class's `networkPolicy: Isolated` (the default) the operator creates a
NetworkPolicy that selects the company's data pods and admits traffic to port
8080 only from:

- the operator's own namespace, so it can reach the engines it configures;
- namespaces labelled `crewlet.ai/api-consumer: "true"`, for a platform's own
  components;
- the namespaces the class's routes arrive from: each Gateway route's
  `gateway.proxyNamespaces`, or the namespaces of its Gateways when unset
  (right for implementations that run a proxy beside each Gateway), and each
  Ingress route's `ingress.controllerNamespaces`, which `Isolated` requires.

Everything else is refused. It restricts ingress only; the engine reaches
vendors and model providers on its own. A CNI that enforces NetworkPolicy is
required for any of this to hold. Some Gateway implementations deliver traffic
from the node rather than from a pod (Cilium's, for example), which no
NetworkPolicy can admit by namespace; with those, set `networkPolicy: None`
and isolate the engines with the CNI's own policy.

## Deletion

A Company carries the finalizer `crewlet.ai/company`. When it is deleted, the
operator:

1. deletes the data StatefulSet and waits until no data pod is left, each
   draining as on any stop;
2. settles the data volume claims as `spec.deletionPolicy` says: `Delete`
   deletes them; `Retain` (the default) keeps them, and releases the keyring
   Secret from the Company so it is kept too, since the volumes cannot be read
   without it;
3. removes the finalizer, after which Kubernetes deletes the rest.

A Company created again with the same name and namespace after a `Retain`
deletion adopts the kept keyring and finds its volume, and with it its estate.
To discard a retained company for good, delete the claims first and the
keyring Secret after them: a Company that finds its claims without its
keyring holds its pod back rather than generate a keyring that cannot open
them (`KeyringMissing`).

## Status

The Company's status is written by two reconcilers, each owning its own
fields and writing them with an optimistic lock, so neither overwrites the
other:

| Field | Written by |
|---|---|
| `conditions` `Accepted`, `RuntimeReady`, `Exposed`, `Ready` | the runtime reconciler (this page) |
| `observedGeneration`, `engine`, `topology`, `endpoints`, `apiAccessSecretRef` | the runtime reconciler |
| `conditions` `SecretsSynced`, `ConfigApplied`; `config` | the config reconciler, which [applies the company document](applying.md) |

`Ready` is `True` when `Accepted`, `RuntimeReady`, `Exposed` and
`ConfigApplied` all are. Otherwise its reason is the reason of the first of
them that is not, in that order, or `ConfigPending` while `ConfigApplied` has
not been reported for the Company's current generation; a suspended Company
reports `Suspended`. So `Ready=True` with an `observedGeneration` equal to
`metadata.generation` means the engine runs the document of that generation,
which is what a client waiting for a change to take effect checks.

## Reaching the engine

A consumer in the cluster reads the Secret `status.apiAccessSecretRef` names
and sends its `token` as a bearer token to its `url`, from a namespace
labelled `crewlet.ai/api-consumer: "true"` when the class isolates the engine.
The `nimbus-engine` Service routes only to a ready pod, and an engine is ready
only once it runs a document; a client that has to reach an engine before
that, as the operator does to apply the first one, addresses the pod directly
through the headless Service:
`http://nimbus-data-0.nimbus-data.<namespace>.svc:8080`.
