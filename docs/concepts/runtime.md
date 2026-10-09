# How a company's engine runs

For every accepted `Company` the operator runs one Crewlet engine: a
StatefulSet with one data pod, which holds the company's estate, serves the
engine's API, runs the background workers and runs the seats placed on it;
and, when the Company declares seat pools and its class allows them,
StatefulSets of stateless seat pods that run the other seats. This page
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
| NetworkPolicy `nimbus-broker` | While the class's `seatPools.policy` is `Allowed`, whatever its `networkPolicy` says: who may reach the data pods' leaf listener; see [Network policy](#network-policy). |
| Secret `nimbus-bootstrap-<pool>-<hash>` | A seat pool's `crewlet.yaml` ([A seat pod's `crewlet.yaml`](company-document.md#a-seat-pods-crewletyaml)), with the class's environment values as in `nimbus-bootstrap`. One per pool, a PerSeat pool's seats included. |
| StatefulSet `nimbus-seats-<pool>-<hash>`, `nimbus-seat-<handle>-<hash>` | A Shared pool's pods, and a PerSeat seat's own; see [Seat pools](#seat-pools). |
| Service `nimbus-seats` | Headless, with no port: the seat StatefulSets' governing Service. |
| PodDisruptionBudget `nimbus-seats-<pool>-<hash>`, `nimbus-seat-<handle>-<hash>` | `maxUnavailable: 1` for each Shared pool, and for each PerSeat seat with two pods. |
| NetworkPolicy `nimbus-seats` | While the company runs seat pods: no connection reaches one. |

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
| Ports | 8080, the API, dashboard and webhooks; and 7422, the leaf listener seat pods join the event stream through, while the class's `seatPools.policy` is `Allowed`. The headless Service `nimbus-data` names both. |
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

## Seat pools

A Company's `spec.topology.seatPools` are stateless pods that run seats:
Crewlet with `node.roles: [seats]` alone, a scratch store deleted at every
start, joined to the data pods' event stream as a NATS leaf. The class's
`seatPools.policy` must be `Allowed`, with a `maxPods`; its `allowPerSeat`
and `maxPods` bound what a Company may run, and its
`seatPools.defaults.resources` what each seat pod may take. Where each seat runs is decided by its `Seat`'s
`spec.placement.pool` ([Placement](company-document.md#placement)).
[Choosing where seats run](../guides/topology.md) says when a company needs
pools, and which kind.

| | Shared pool `general` | PerSeat pool, seat `cy` |
|---|---|---|
| StatefulSet | `nimbus-seats-general-c53f6a26` | `nimbus-seat-cy-8ae373fd`, one for each seat placed in the pool |
| Replicas | the pool's `replicas`, 0 to 100 | the pool's `replicas`, 1 or 2 (a warm standby) |
| Selector | `crewlet.ai/pool: general`, and no `crewlet.ai/seat` label | `crewlet.ai/seat: cy` |
| Environment | `CREWLET_NODE_ID`, the pod's name; `HOME`; `TURSO_GO_CACHE_DIR` on the scratch volume; the class's and the Company's variables, as on a data pod | the same, and `CREWLET_SEAT: cy` |
| Storage | one `emptyDir` at `/var/lib/crewlet`, of the class's `seatPools.defaults.scratchSizeLimit` (2Gi unless set), holding the scratch store, the store's native library, `/tmp` and `HOME`: the limit bounds what the processes the engine runs write too | the same |
| Stopping | the pool's `terminationGracePeriodSeconds`, else the class's default, else five minutes: running turns finish, waiting ones are handed back, and the seats are released with their mailboxes for another pod to claim | the same |
| Updates | `RollingUpdate`, `maxUnavailable: 1`, `podManagementPolicy: Parallel`, `minReadySeconds: 60` | the same |
| Resources | the class's `seatPools.defaults.resources`: the class sizes every seat pod, as it sizes the data pods, and a Company chooses only how many its pools run, within `maxPods` | the same |

A seat pod runs as a data pod does, under the `restricted` Pod Security
Standard, as the engine's ServiceAccount with no token mounted, scheduled by
the class's `podTemplate`, and on the data pods' image: it never runs another
version than the data pods.

A seat pod has **no port and no probe**. The engine this operator pins serves
nothing on a node without the `ingress` role, health and readiness included,
so Kubernetes counts a seat pod ready as soon as its container runs, and a
wedged engine exits by itself (with code 75) rather than wait for a liveness
probe. `minReadySeconds` is what paces a rollout instead: the next pod is
replaced only once the last one has run for a minute. Where the Kubernetes
`MaxUnavailableStatefulSet` feature is on (the default from 1.37), pods that
are already unavailable, such as seat pods crash-looping through a data pod
outage, are all replaced at once; available ones still go one at a time.

A seat pool's objects are named `<company>-seats-<pool>`,
`<company>-seat-<handle>` and `<company>-bootstrap-<pool>`, then a dash and
eight hexadecimal digits of a hash of the Company's name, the kind of object
and the pool or handle. The hash keeps a pool or a handle from spelling
another Company's fixed name in a shared namespace (a pool named `keyring`
of Company `a` would otherwise make `a-bootstrap-keyring`, Company
`a-bootstrap`'s keyring). Names are held to 52 characters, the most a
StatefulSet's pods' `controller-revision-hash` label leaves room for: a
longer one keeps its first 43 characters before the hash. Each Seat's
`status.placement.workload` names the StatefulSet that runs it.

### The seat pods follow the data pods

- **Start.** A seat StatefulSet is created, and a pool gains pods, only while
  every data pod is ready and runs the current template: a leaf that finds
  no data pod to join within a minute fails to start. Taking pods away
  never waits.
- **Upgrade.** A change that rolls the data pods (an image, the keyring, the
  class's `seatPools.policy`) rolls them first. The seat StatefulSets keep
  their template, and their Secrets, until every data pod is ready on the
  new one, and then every seat StatefulSet takes its new template in the
  same reconcile. Meanwhile the Company reports `SeatsPlaced=False`, reason
  `RolloutBlocked`, naming each seat StatefulSet whose template differs from
  the one it would be given now, whatever differs in it. A change to a pool
  alone rolls its pods at once.
- **Suspension.** `spec.suspend` scales every seat StatefulSet to zero, and
  the data StatefulSet only once no seat pod is left: a stopping seat pod
  hands its last records to a data pod, the only kind that keeps them.
  Each seat StatefulSet records how many pods it ran
  (`crewlet.ai/suspended-replicas`). Resuming starts the data pods, and the
  seat pods once those are ready: as many as each pool's `replicas` asks,
  and for a StatefulSet kept only for the running revision, or frozen while
  the Company is refused `PoolCapacityExceeded`, as many as it ran.
- **A data pod outage.** A seat pod that cannot reach the estate for more
  than a minute releases its seats, and one that restarts meanwhile
  crash-loops; with Kubernetes' back-off it may take up to about five
  minutes to rejoin after the data pod returns. One data pod is a single
  point of failure for the whole company.

### A pool removed while seats still run in it

The engine places a seat only on a pod whose labels match its pin, never
anywhere else, so a pod stopped while its seat is still pinned to it leaves
the seat held by nobody until another revision moves it. The config
reconciler records where the revision the engine runs pins its seats, in
`status.config.placement` ([What each object
reports](applying.md#what-each-object-reports)). A seat StatefulSet the
Company no longer asks for, because its pool was removed or its seat moved
elsewhere, keeps running, at the size it is, for as long as that record pins
a seat to it, and is deleted with its disruption budget and, once no
StatefulSet of the pool is left, the pool's Secret, as soon as a revision
that pins nothing to it runs. Meanwhile it still follows the data pods: it
takes their new image and keyring once they are ready on them, with the
grace period and turns its pool had when the operator last wrote it (the
annotation `crewlet.ai/seat-pool` records them), and the class's current
resources, since a pod restarted from an image or a keyring the data pods
have left would not join them. A document that cannot be applied (a Seat
still placed in the removed pool is refused `PoolNotFound`) therefore keeps
the pool running with the revision that needs it. `SeatsPlaced` names the
StatefulSets kept that way.

A seat moved to another pool keeps running where it runs until a pod of its
new pool is ready: the config reconciler keeps it pinned where the revision
the engine runs pins it, reports `Applied=False`, reason `PlacementPending`,
on the Seat, and moves the pin with the first revision it writes once the
StatefulSet of the new pool (the seat's own, for a PerSeat pool) has a ready
pod. The old pods stay for as long as that revision pins the seat to them,
so the move is one release and one claim, a turn boundary apart, rather than
a seat held by nobody while the new pod is scheduled, pulled and started. A
pool that never gets a ready pod (one at `replicas: 0`) keeps the seat where
it is, and says so.

### What the class allows

| The class | What the operator runs |
|---|---|
| `seatPools.policy` is not `Allowed` | No new seat pod, and the Company's document is refused while it declares pools: `SeatsPlaced=False`, `SeatPoolsNotAllowed`. The engine keeps running its last revision, so the seat StatefulSets that revision pins seats to keep running, and the data pods keep their leaf listener open for them, until a revision that pins every seat to the data pods runs; then the seat pods go and the data pods roll to close the listener. A company with no seat pinned to a seat pod has its data pods rolled at once: changing the policy opens or closes the listener. |
| A Seat in a PerSeat pool without `allowPerSeat`, or one whose pods would pass `maxPods` | No pod for that seat; the Seat reports `Applied=False`. The PerSeat seats are admitted in one order: first those the running revision already runs in pods of their own, then the rest, each group oldest first (by creation, then by handle). The first seat whose pods would pass `maxPods` is refused, and so is every seat after it, so a seat moved into a full pool is refused rather than the one already running there. |
| `maxPods` that the Shared pools' replicas alone pass | Every seat StatefulSet keeps its size, and none is created: `SeatsPlaced=False`, `PoolCapacityExceeded`. Their pods still follow the data pods' image and keyring. |

### Disruptions

A Shared pool's StatefulSet, and a PerSeat seat's with two pods, has a
PodDisruptionBudget of `maxUnavailable: 1`, so a node drain takes one pod at
a time while the others keep the seats; a PerSeat seat with one pod has
nothing to keep and gets none. Pods that are not running may always be
evicted (`unhealthyPodEvictionPolicy: AlwaysAllow`), so a crash-looping pod
never holds up a drain. Moving seats between pods interrupts them: each move
releases the seat, starts its tool servers on the new pod and rehydrates its
memory there.

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
`deletionPolicy` says). A company with seat pools stops them first, and its
data pods only once no seat pod is left ([The seat pods follow the data
pods](#the-seat-pods-follow-the-data-pods)): meanwhile `RuntimeReady` is
`False` with the reason `Suspending`. Once the data pods are stopped the
Company reports `Ready=False` and `RuntimeReady=False`, both with the reason
`Suspended`. Setting it back starts the engine on the same volume. Suspending takes effect whatever else is wrong
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
node that runs the seat, which only the single data pod is: a seat pod serves
no bridge, so a seat in a seat pool whose work runs on such a provider is
refused (`AgentModeBridgeUnsupported`, see [Seat pools](../guides/cli-agents.md#seat-pools)),
and the variable is given to the data pod alone. With no such provider
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

Two more policies come with seat pools:

- `nimbus-broker` exists while a data pod opens the leaf listener, **whatever
  `networkPolicy.mode` says**: while the data pods' template opens it (the
  class allows seat pools, or the running revision still pins a seat to a
  seat pod), and for as long as any data pod that runs, or is stopping, was
  started from a template that opened it (the pod's annotation
  `crewlet.ai/leaf-listener: open`), however long the rollout that closes it
  takes or is held back. Crewlet's leaf listener accepts any
  connection, with no authentication and no TLS, and whoever joins it reads
  and writes the company's whole event stream. It selects the data pods and
  admits port 7422 only from pods labelled as this company's seat pods. Under
  `None` it also admits port 8080 from anywhere, which keeps the API as open
  as it is without a policy and closes only the broker.
- `nimbus-seats` exists while the company runs seat pods. It selects them and
  admits nothing: a seat pod serves nothing. Under `Isolated` it also holds
  them to the data pods' egress, port 7422 of the data pods included.

NetworkPolicies are additive: a pod two policies select is open to what
either admits. That is why `nimbus-engine` and `nimbus-broker` select the data
pods alone and never a seat pod, and why the isolation of the seat pods holds
only while no policy of anyone else's selects them.

**The namespace is the trust boundary.** A policy selects pods by their
labels, and whoever can create a pod in the namespace can give it a seat
pod's labels and join the company's event stream; they can also mount the
company's keyring Secret into it and open its secret store, which no policy
governs. Companies that must not read each other's streams and secrets need
namespaces of their own, or a namespace where nobody but the operator creates
pods. Two more limits hold for every NetworkPolicy, on most CNIs (Calico and
Cilium among them): traffic from the node itself, and from `hostNetwork` pods
on the data pod's node, is not policed. And the leaf link carries the event
stream (turn transcripts, tool results) unencrypted over the pod network:
where that network is not trusted, run a CNI that encrypts it (WireGuard or
IPsec, as Calico and Cilium offer).
A CNI that enforces NetworkPolicy is required for any of this to hold. Some
Gateway implementations deliver traffic from the node rather than from a pod
(Cilium's, for example), which no NetworkPolicy can admit by namespace; with
those, set `networkPolicy.mode: None` and isolate the engines with the CNI's
own policy.

## Deletion

A Company carries the finalizer `crewlet.ai/company`. When it is deleted, the
operator:

1. deletes every seat StatefulSet and waits until no seat pod is left, each
   draining into a data pod as on any stop; then
2. deletes the data StatefulSet and waits until no data pod is left, each
   draining as on any stop. Under `Retain` the StatefulSet is deleted with
   the `Orphan` propagation policy and the operator deletes its pods itself:
   a claim the StatefulSet controller made while the policy was `Delete`
   carries an owner reference to the StatefulSet until that controller next
   syncs, and a background deletion would have the garbage collector delete
   the claim with it. Orphaning releases every claim first;
3. settles the data volume claims as `spec.deletionPolicy` says: `Delete`
   deletes them; `Retain` (the default) keeps them, and releases the keyring
   Secret from the Company so it is kept too, since the volumes cannot be read
   without it;
4. removes the finalizer, after which Kubernetes deletes the rest.

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
| `conditions` `Accepted`, `RuntimeReady`, `Exposed`, `SeatsPlaced`, `Ready` | the runtime reconciler (this page) |
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

`status.topology` counts the pods: `data` the data pods', and `seatPools`
each declared pool's, every seat's StatefulSet together for a PerSeat pool,
with how many agent seats the pool runs. `SeatsPlaced` says whether every
pool an agent seat is placed in, `data` included, has a ready pod:

| Status | Reason | When |
|---|---|---|
| True | `AllPoolsServing` | Every pool with a seat has a ready pod. |
| False | `PoolEmpty` | A pool with a seat has no ready pod: a Shared pool at zero replicas, a PerSeat seat's pods down, or pods not created yet because the data pods are not ready. The message names them; their seats' messages wait. |
| False | `RolloutBlocked` | Seat StatefulSets keep their template until the data pods are ready on theirs. |
| False | `SeatPoolsNotAllowed`, `PoolCapacityExceeded` | The class runs none of the Company's seat pods, or keeps them as they are; see [What the class allows](#what-the-class-allows). |
| False | `Suspended` | The Company is suspended. |

`SeatsPlaced` is not part of `Ready`: a Shared pool scaled to zero parks its
seats on purpose. Its message also names the seat StatefulSets kept for the
running revision, and how many Seats are refused a pool.

Which pod runs each seat is the engine's to decide, and each agent Seat
reports what the engine says: `status.placement.node` names the pod that holds
its lease, and its `Ready` is `True` (`LeaseHeld`) while one does and `False`
(`Unplaced`) while none does ([What each object
reports](applying.md#what-each-object-reports)).

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
