# Choosing where seats run

A company always has one data pod, and every agent seat runs there unless you
say otherwise. Seat pools add stateless pods beside it that run seats and
nothing else. This guide is for deciding whether a company needs them, which
kind, and what they buy; [How a company's engine
runs](../concepts/runtime.md#seat-pools) describes the objects the operator
makes for them.

## Data pods and seat pods

Crewlet runs one binary in two shapes, chosen only by its `crewlet.yaml`:

| | Data pod | Seat pod |
|---|---|---|
| Crewlet roles | `data`, `ingress`, `seats`, `workers` | `seats` |
| Holds | the company's store, its event stream and its coordination buckets, on a volume | nothing: a scratch store deleted at every start |
| Event stream | a broker member | a NATS leaf of the data pods, with JetStream off, holding nothing and voting in nothing |
| Serves | the API and the dashboard on port 8080, the webhooks and a sandbox's calls back on its public listener, port 8081; the scheduler, the sandbox waiter, integration reconciles and maintenance | its probes on port 8080, and nothing else |
| Company document | applied to it by the operator | taken from the data pod at start |
| Can be a company's only pod | yes | no: without a data pod it has nothing to join, and gives up within a minute |

So the smallest company is one data pod, and seat pools only ever add pods
beside it. A seat pod writes its tracker and audit records through a data pod
and hands it its last records when it stops.

## Do you need seat pools?

Crewlet's own guidance is to scale up before scaling out: what bounds a
company is its model providers' rate limits and its host's memory, not the
number of processes. A seat is serial, taking one batch of messages at a
time, so a pod of its own does not make one seat faster. A data pod with
enough CPU and memory, and the class's `seatPools.policy` left at
`Forbidden`, is the right shape for most companies.

Seat pools are worth their cost when a company needs one of these:

- **More concurrent turns than one pod should run**, or seats whose tool
  servers and coding agents need more memory than you want beside the
  estate: a Shared pool spreads the seats over several pods.
- **A pod that can fail without stopping the company**: a seat pod that runs
  out of memory or wedges costs the seats on it, which another pod of the
  pool takes over, while the data pod keeps serving.
- **Compute and failures of its own for a few seats**: a PerSeat pool gives
  each of its seats its own pods, each of the class's seat pod size.

## Shared and PerSeat pools

| | Shared pool | PerSeat pool |
|---|---|---|
| Pods | `replicas` of them, 0 to 100, for all the seats placed in the pool | 1 or 2 for each seat placed in the pool |
| Which pod runs a seat | the engine spreads the pool's seats evenly over its pods that run, and moves them when a pod comes or goes | the seat's own pods; with 2, the second is a warm standby that takes the seat when the first stops |
| Scaling | change `replicas`; at 0 the pool's seats run nowhere and their messages wait | none: one pod (or two) per seat |
| The class must set | `seatPools.policy: Allowed` and a `maxPods` | that, and `seatPools.allowPerSeat: true` |

Every seat pod of the company counts against the class's `seatPools.maxPods`.
Seats are placed with their `Seat`'s `spec.placement.pool`, or the Company's
`spec.topology.defaultSeatPool`:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Company
metadata:
  name: nimbus
  namespace: nimbus
spec:
  engineClassName: pro        # seatPools: {policy: Allowed, allowPerSeat: true, maxPods: 6}
  companyName: Nimbus
  topology:
    seatPools:
      - name: general
        mode: Shared
        replicas: 2
      - name: isolated
        mode: PerSeat
        replicas: 1
---
apiVersion: crewlet.ai/v1alpha1
kind: Seat
metadata:
  name: release-manager
  namespace: nimbus
spec:
  companyRef: {name: nimbus}
  goal: Ship a release every week.
  placement:
    pool: isolated
```

## What a pod of its own isolates, and what it does not

A PerSeat pool is the closest the current engine comes to one pod per agent.
It is an assembly rather than an engine feature: Crewlet has no node bound to
one seat, so the operator pins every agent seat to its pool and gives each
pod exactly one placement label ([Placement](../concepts/company-document.md#placement)),
and the engine then runs on each PerSeat pod its own seat and no other.

| It gives the seat | It does not give it |
|---|---|
| CPU and memory of its own: a pod of the class's seat pod size (`seatPools.defaults.resources`) that no other seat's work shares | **Credential isolation.** Every node of a company holds the keyring and reads the whole secret store, and resolves every seat's vendor identities. A seat pod that is compromised can reach every credential of the company, its own seat's or not. Treat every pod of a company as holding all of its secrets. |
| A filesystem and processes of its own: other seats' tool servers and coding agents do not run beside it | Speed: the seat still takes one batch of messages at a time. |
| A failure of its own: an out-of-memory kill or a wedged engine costs this seat alone | Continuity at `replicas: 1`: while its pod restarts, the seat runs nowhere and its messages wait. `replicas: 2` keeps a standby. |

## What pools cost

The cost grows with pods times seats, not with pods alone:

- Every node, data or seat pod, walks every seat's mailbox at start and on
  every revision applied, and lists every node's presence every five seconds.
- Every node runs its own copy of every shared MCP server of the company.
- Moving a seat interrupts it: it is released, its tool servers start on the
  new pod and its memory is read back there. A Shared pool moves seats each
  time it gains or loses a pod.
- One data pod is a single point of failure for the whole company, seat pods
  included: a seat pod that loses the data pod for more than a minute
  releases its seats.

That is why the class allows no pools unless it says so, allows no PerSeat
pool unless it says so, and caps a company's seat pods with `maxPods`.
PerSeat is for the few seats whose compute or failures need a pod of their
own, not a default for every agent.

A PerSeat pod is no network or scheduling boundary of its own. Every seat
pod of a company is selected by the one `<company>-seats` NetworkPolicy and
scheduled by the class's one `podTemplate` (its node selector, tolerations,
affinity and topology spread apply to the data pods and every seat pod
alike, so a required anti-affinity among `app.kubernetes.io/name: crewlet`
pods allows no more engine pods than nodes). NetworkPolicies only add: under
the class's `networkPolicy.mode: Isolated`, `<company>-seats` already admits
the egress the engine needs, and a policy of a tenant's own cannot narrow it
for one seat. Only under `mode: None` does a tenant's NetworkPolicy that
selects `crewlet.ai/seat: <handle>` narrow that seat's egress.

## Where each seat runs now

The engine decides which pod holds which seat; the operator reads it from the
engine's `GET /fleet` each time it looks at the engine, and sooner while
seats are being placed, and reports it on the Seat:

```bash
kubectl get seats -n nimbus -o wide
```

```
NAME              COMPANY   KIND    UNIT   APPLIED   READY   PAUSED   CODEWORK   POOL       NODE                                     AGE
release-manager   nimbus    Agent          True      True    False    True       isolated   nimbus-seat-release-manager-89b42764-0   3d
triage            nimbus    Agent          True      True    False               general    nimbus-seats-general-c53f6a26-1          3d
```

| Seat field | What it says |
|---|---|
| `status.placement.pool` | the pool the seat is placed in |
| `status.placement.workload` | the StatefulSet whose pods may run it |
| `status.placement.node` | the pod whose engine holds the seat's lease and runs it, unset while none does |
| `CodeWorkRunnable` (the `CODEWORK` column) | whether the seat's coding agent can run on its sandbox provider, or on the sign-in its sandbox environment gives it; blank on a seat without `spec.sandbox` ([Code work](cli-agents.md#code-work)) |
| `Ready` | `True`, `LeaseHeld`, while a pod holds it; `False`, `Unplaced`, while none does, saying why: its pool runs no pod, the seat is not in the revision the engine runs yet, or the engine has not claimed it yet (a few seats every five seconds); `False`, `Suspended`, while the Company is suspended; `Unknown`, `EngineUnavailable`, when the engine's fleet could not be read |

A Human seat is never run by the engine and has no `Ready`. On the Company,
`status.topology.seatPools` counts each pool's pods and seats, and
`SeatsPlaced` says whether every pool with a seat has a ready pod: one that
has joined the fleet and may claim seats, which its `/ready` says ([Seat
pools](../concepts/runtime.md#seat-pools)). With the API token, the engine's
own `GET /fleet` lists every node, its roles and labels, and the holder of
every seat.

## Lifecycle and rollouts

- **Start and resume.** Seat pods start only once the data pod is ready on
  its current template.
- **Rollouts.** The data pod rolls first; the seat pods keep their template
  until it is ready, then every seat StatefulSet rolls at once. Pods are
  replaced one at a time: the next once the last is ready, so has joined the
  fleet and may claim seats, and ten seconds after, while the others give
  back to it what is past their share.
- **Moving a seat to another pool** keeps it where it runs until a pod of
  the new pool is ready (the Seat reports `Applied=False`,
  `PlacementPending` meanwhile), then moves it with one release and one
  claim.
- **Scaling a Shared pool** moves seats onto new pods, and off pods that go.
  A stopping pod finishes its running turns, hands back the waiting ones and
  releases its seats, so give a pool a `terminationGracePeriodSeconds` longer
  than its longest turn (five minutes unless the pool or the class says
  otherwise).
- **Suspension and deletion** stop the seat pods first and the data pod
  after them.
- **Removing a pool** keeps its pods running for as long as the revision the
  engine runs places a seat in it, at the size they are, still following the
  data pod's image and keyring.
- **A class that stops allowing pools** refuses the company's next revision
  while it declares any, and keeps the pods and the data pod's listener that
  the running revision needs, until a revision that runs every seat on the
  data pod is applied.

[The seat pods follow the data pods](../concepts/runtime.md#the-seat-pods-follow-the-data-pods)
has the details.

## Refusals, and why

| Reported | When | Why |
|---|---|---|
| Seat `ResolvedRefs=False`, `PoolNotFound` | `spec.placement.pool` names no pool of the Company | A seat pinned to a label no pod carries would never run, silently. |
| Seat `Applied=False`, `PerSeatNotAllowed` | the seat is in a PerSeat pool and the class does not allow them | Pods of their own are the most expensive shape; the class decides which companies get them. |
| Seat `Applied=False`, `PoolCapacityExceeded` | the seat's own pods would take the company past the class's `maxPods`; the seats already running in pods of their own are admitted first, then the rest oldest first, and the first that does not fit is refused with every seat after it | The cost above grows with every pod. |
| Seat `Applied=False`, `AgentModeBridgeUnsupported` | the seat is in a seat pool, and its executor's chain (`spec.llm.default`, or the Company's first provider when the seat has none) starts with a `CLIAgent` provider in `mode: Agent` | An agent-mode run calls the seat's tools back through the tool bridge of the pod that holds the seat, at an address the sandbox reaches; the operator publishes none for a seat pod, so the engine would refuse every run, after the revision applied. |
| Seat `Applied=False`, `CLIAuthNotPortable` | the seat is in a seat pool, and a chain of it names a `CLIAgent` provider signed in with `credentialBundleSecretRef` | A seat pod keeps the CLI's login on a volume it loses at every start, so it restores the bundle each time and refreshes it on its own, beside the data pod; a vendor that rotates refresh tokens then logs every pod out. A headless token (`tokenSecretRef`), `auth.mode: APIKey` or the model provider's key a Hermes, Pi or OpenCode takes in `cli.env` never refreshes. |
| Company `ConfigApplied=False`, `SeatPoolsNotAllowed` | the Company declares pools and its class's policy is not `Allowed` | The class's data pods open no listener for seat pods to join. |
| Company `ConfigApplied=False`, `PoolCapacityExceeded` | the Shared pools alone pass `maxPods` | As above; the operator keeps the pools as they are and starts no pod. |

A refusal blocks the company's next revision, like any other, and the last
good revision keeps running. Users cannot write Crewlet node labels or pin a
seat to a node themselves: one stray label would let a seat run on a pod it
was never placed on, a seat pod that cannot serve it among them, and the
operator could no longer tell which pool a pod serves.

## What the current engine does not do

- **One data pod.** A company has one broker member, so the data pod is a
  single point of failure; a highly available data tier comes later.
- **No autoscaling.** There is no `/scale` subresource and the operator never
  changes a pool's `replicas` itself: moving seats interrupts them, and more
  pods buy availability, not throughput. A platform sets `replicas` itself
  ([Tiers, limits and offboarding](multi-tenancy.md#tiers-limits-and-offboarding)).
- **No tool bridge on seat pods.** A coding CLI in agent mode calls the
  seat's tools back through the engine pod that holds the seat, which must
  serve them on an address the sandbox reaches. The engine would serve the
  bridge on a seat pod's own listener, but the operator publishes no address
  of a seat pod a remote sandbox could dial, and the company's public address
  reaches the data pod only, so a seat whose work runs on such a provider
  stays on the data pods
  ([Running seats on a coding CLI](cli-agents.md#seat-pools)).
- **Code runs in a remote sandbox.** v1alpha1 offers only E2B for a seat's
  sandbox, which works from any pod; Crewlet's local sandbox cells would be
  polled by a data pod for a box on another pod, and fail.
