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
| Secret `nimbus-bootstrap` | The data pod's `crewlet.yaml`, rendered as [What the engine is given](company-document.md#the-data-pods-crewletyaml) describes, with the keyring, the API tokens and the object store's keys in it; and the values the EngineClass takes from the platform's own Secrets for the engine's environment (the telemetry headers, `podTemplate.spec.env` values from Secrets), under `otlpHeaders` and `env.<NAME>`. |
| Secret `nimbus-secret-sync` | What the config reconciler last pushed into the engine's secret store: a keyed fingerprint of each value, never the value, by which it decides what to push or remove next ([Applying the document](applying.md)). |
| ServiceAccount `nimbus-engine` | The engine's identity, carrying the class's `serviceAccount.annotations` for a workload identity. Its token is never mounted: the engine does not call the Kubernetes API. |
| StatefulSet `nimbus-data` | The data pod. |
| Service `nimbus-engine` | ClusterIP on port 8080, routing to ready data pods. The routes and `status.endpoints.api` point at it. |
| Service `nimbus-data` | Headless, the StatefulSet's governing Service, publishing each data pod's address before it is ready. |
| HTTPRoute or Ingress `nimbus-webhooks`, `nimbus-api` | The routes the class asks for; see [Exposure](#exposure). |
| NetworkPolicy `nimbus-engine` | With the class's `networkPolicy.mode: Isolated`; see [Network policy](#network-policy). |

The operator writes an object only if it does not exist or the Company
controls it. An object of one of these names that someone else created (a
namespace's own `nimbus-engine` Service, a default-deny policy that happens to
share the name) is left alone: the operator would otherwise rewrite it and,
through the owner reference it adds, have the garbage collector delete it with
the Company. The Company reports `RuntimeReady=False` (`Exposed=False` for a
route), reason `NameConflict`, naming the object; rename or delete it, or give
the Company another name. Objects an earlier Company of the same name left
behind are taken over.

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
| Command | `crewlet run -config /etc/crewlet/crewlet.yaml`, the bootstrap Secret's `crewlet.yaml` mounted read-only, readable by the engine's user alone (mode `0440`, group `fsGroup`). |
| Storage | One volume claim per pod (`data-nimbus-data-0`) at `/var/lib/crewlet`, from the class's `data.storage`: the store, the event stream (three quarters of the volume, [sized from the claim](company-document.md#the-data-pods-crewletyaml)), and the store's native library cache (`TURSO_GO_CACHE_DIR`), so a restart does not extract it again. |
| Environment | `CREWLET_NODE_ID`, the pod's name; `HOME`; `CREWLET_MCP_BRIDGE_URL`, the Company's public address, while an LLMProvider of the company runs a CLI in `mode: Agent` (see [Exposure](#exposure)); the OpenTelemetry variables when the class sets `telemetry`; then the class's `podTemplate.spec.env` and the Company's `runtime.env`, merged by name. No credential of the engine's own is an environment variable (see [What the engine is given](company-document.md#the-data-pods-crewletyaml)). |
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

The pod template carries the annotation `crewlet.ai/bootstrap-hash`, the hash
of the bootstrap Secret's data, which rolls the pod when what it reads at
start changes: a token, a keyring key, the object store's keys, a platform
value, or the document itself. Kubernetes reads a Secret into an environment
variable, and the engine its `crewlet.yaml`, only when the container starts.
It hashes the values rather than naming the Secrets' versions, so a label or
an annotation written to a Secret restarts nothing. A Secret the Company's own
`runtime.env` references is read the same way, but the operator does not
watch it: restart the pod after changing one.

## Changes, versions and suspension

A change to the class or the Company that changes the pod (an image, the
resources, a toleration, the bootstrap document) rolls the data pod. The data
volume is the exception: a StatefulSet's volume claims cannot change, so a
class's `data.storage` applies to companies created after it changes, and an
existing company keeps its volume.

The engine migrates its data forward only. The operator records the image the
data pod last ran *and was ready on* in `status.engine.image`, and its tag in
`status.engine.version`, and records that version on each data volume claim
as the annotation `crewlet.ai/engine-version`, which outlives the status: a
Company created again over retained volumes, or restored from a backup that
dropped its status, still knows what its data was last run on. A Company or
class that asks for an image whose tag is older, as a semantic version, than
the newest of the two is refused with `RuntimeReady=False`, reason
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
sealed data opens only with its keys), a tokens Secret the platform supplied
that lacks a token or holds one shorter than 43 characters or outside
base64, base64url and hex (`APITokensInvalid`), or a platform Secret the
class references that is missing (`SecretNotFound`). Suspending still
scales the pod to zero meanwhile.

## Exposure

Vendors deliver webhooks to `/webhooks/`. Everything else the engine serves on
port 8080 (its configuration, its secret store, the dashboard) stays inside the
cluster unless the class routes the API through an internal gateway.

| Class `exposure.webhooks.mode` | What the operator creates | `status.endpoints.publicBaseURL` |
|---|---|---|
| `Gateway` | HTTPRoute `<name>-webhooks` on the class's Gateways: the expanded `hostname`, path prefix `/webhooks/` only (and `/mcp/`, below), to the engine Service | `spec.exposure.publicBaseURL`, else `https://<hostname>` |
| `Ingress` | Ingress `<name>-webhooks`: the class's IngressClass, annotations and TLS Secret, the expanded `hostname`, path prefix `/webhooks/` only (and `/mcp/`, below) | the same |
| `External` | Nothing: something outside the operator forwards `/webhooks/` (and `/mcp/`, below) to `<name>-engine:8080` | `spec.exposure.publicBaseURL`, which is required |
| `None` | Nothing | unset |

While an LLMProvider of the company runs a CLI in `mode: Agent`, the
webhooks' route also sends the path prefix `/mcp/` to the engine, and the
engine is given the public address as `CREWLET_MCP_BRIDGE_URL`. That is the
engine's tool bridge: an agent-mode run happens in a remote sandbox and calls
the seat's own tools back there, each request carrying a token signed for its
one run, which expires with it. Crewlet requires the address to reach the
node that runs the seat, which the single data pod is. With no such provider
the bridge is neither mounted nor routed; adding or removing one changes the
pod's environment, so the data pod rolls.

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

With the class's `networkPolicy.mode: Isolated` (the default) the operator
creates a NetworkPolicy that selects the company's data pods and admits
traffic to port 8080 only from:

- the operator's own pods, so it can reach the engines it configures;
- the class's `networkPolicy.apiConsumers`, for a platform's own components;
- the namespaces the class's routes arrive from: each Gateway route's
  `gateway.proxyNamespaces`, or the namespaces of its Gateways when unset
  (right for implementations that run a proxy beside each Gateway), and each
  Ingress route's `ingress.controllerNamespaces`, which `Isolated` requires.

Everything else is refused. Outward, the engine, which runs code its tenant
supplies, may reach DNS, the pods of its own namespace, the class's
`networkPolicy.egress.to`, and every address but the cloud metadata
endpoints and the class's `networkPolicy.egress.exceptCIDRs`, where the
cluster's own networks belong ([Network policy](../guides/exposure.md#network-policy)).
A CNI that enforces NetworkPolicy is required for any of this to hold. Some
Gateway implementations deliver traffic from the node rather than from a pod
(Cilium's, for example), which no NetworkPolicy can admit by namespace; with
those, set `networkPolicy.mode: None` and isolate the engines with the CNI's
own policy.

## Deletion

A Company carries the finalizer `crewlet.ai/company`. When it is deleted, the
operator:

1. deletes the data StatefulSet and waits until no data pod is left, each
   draining as on any stop. Under `Retain` the StatefulSet is deleted with
   the `Orphan` propagation policy and the operator deletes its pods itself:
   a claim the StatefulSet controller made while the policy was `Delete`
   carries an owner reference to the StatefulSet until that controller next
   syncs, and a background deletion would have the garbage collector delete
   the claim with it. Orphaning releases every claim first;
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
| `conditions` `SecretsSynced`, `ConfigApplied`, `EngineHealthy`; `config` | the config reconciler, which [applies the company document](applying.md) |

`Ready` is `True` when `Accepted`, `RuntimeReady`, `Exposed` and
`ConfigApplied` all are. Otherwise its reason is the reason of the first of
them that is not, in that order, or `ConfigPending` while `ConfigApplied` has
not been reported for the Company's current generation; a suspended Company
reports `Suspended`. `ConfigApplied` is `True` only once the engine reports
the epoch of the operator's revision applied, not when the write returns
(`Applying` in between, `ApplyFailed` when the engine did not apply it). So
`Ready=True` with an `observedGeneration` equal to `metadata.generation`
means the engine runs the document of that generation, which is what a
client waiting for a change to take effect checks.

`EngineHealthy` is the engine's own alarm table, read from its `/health` each
time the config reconciler reaches it: `True` (`NoAlarms`), `False` (`Alarms`,
with how many and the one standing longest) or `Unknown`
(`AlarmsNotReported`, before the engine has evaluated them). `Ready` does not
include it: an engine with an alarm still runs the company.

## Backups

The operator takes no backup, and Crewlet needs them for more than recovery:
it trims its logs (the work tracker, the knowledge base's pages and
vectors, usage) only behind a verified backup younger than its
`stream.tracker_retention.backup_max_age`, 24 hours by default, which the
operator leaves as it is. Without one the logs grow until they reach the
ceilings the data volume is sized for, and the engine then refuses ordinary
writes. A new company raises the `backup_age` and `trim_blocked` alarms from
the start, which `EngineHealthy=False Alarms` shows.

Take backups through the engine, which is the only process that can read its
estate. `crewlet backup` writes a verified copy into a directory on the data
pod's volume, without stopping the engine:

```bash
kubectl exec -n acme acme-data-0 -- crewlet backup -config /etc/crewlet/crewlet.yaml \
  -url http://127.0.0.1:8080 -dir /var/lib/crewlet/backups/$(date -u +%Y-%m-%dT%H-%M)
```

Run it at least daily. Copy each backup off the volume (`kubectl cp`), since
one on the volume it protects survives nothing that loses the volume, and
delete old ones from the volume: they count against its size. Back up the `<company>-keyring` Secret with the volume's copies: one
opens nothing without the other. Crewlet's own guide, *Backups & Restore*,
covers what a backup holds and how to restore one.

## Reaching the engine

A consumer in the cluster reads the Secret `status.apiAccessSecretRef` names
and sends its `token` as a bearer token to its `url`, from a pod the class's
`networkPolicy.apiConsumers` admit when the class isolates the engine.
The token is the engine's administrator: Crewlet has no token scopes, so
whoever holds it can rewrite the company document and reveal every value in
the secret store, platform model keys included. Grant read on that Secret by
`resourceNames` to whoever may do that, and to no one else.

The `nimbus-engine` Service routes only to a ready pod, and an engine is ready
only once it runs a document; a client that has to reach an engine before
that, as the operator does to apply the first one, addresses the pod directly
through the headless Service:
`http://nimbus-data-0.nimbus-data.<namespace>.svc:8080`.
