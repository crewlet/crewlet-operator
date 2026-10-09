# What the engine is given

A Crewlet engine reads two documents: the **company document** (Crewlet's
Tier B: the company, its org chart, models, tools and integrations), which
the operator applies over the engine's API, and each engine pod's
**`crewlet.yaml`** (Tier A: how the node boots), a data pod's or a seat
pool's, which the operator keeps in a Secret and mounts into the pod. The
operator renders them from the `crewlet.ai` objects. This page says what they
contain, so that what the engine reports can be traced back to an object.

Both documents are rendered deterministically: the same objects always give
the same bytes, whatever order the API server lists them in. Keys are
sorted, units and their child units are ordered by `metadata.name`, seats by
handle, and model providers and tool servers by name.

## The company document

| Object | Becomes |
|---|---|
| `Company` | `name`, `mission`, `vision`, `policies`, `timezone`, `tracker`, `knowledge`, `token_budget`, `providers.embeddings`, `providers.sandbox`, `providers.llm_order`, `integrations.check_interval_seconds`, and one top-level key per `spec.engineConfig` entry |
| `EngineClass` | `providers.llm.<name>` for each of its `platformLLMProviders`, when the Company sets `spec.llm.usePlatformProviders` |
| `LLMProvider` | `providers.llm.<metadata.name>` |
| `MCPServer` | one entry of `mcp_servers`, named by its `metadata.name` |
| `Integration` | `integrations.slack` or `integrations.github` |
| `Unit` | one entry of `units`, or of its parent's `children` |
| `Seat` | one entry of its Unit's `roles`, or of the top-level `roles` when it has no Unit |

`integrations.public_base_url` is the Company's public address, from
`spec.exposure.publicBaseURL` or the class's `webhooks.hostname`, and is left
out while the company has none.

### Names and identities

- A Unit's `metadata.name` is its Crewlet `id`, and its `displayName` (or,
  unset, its `metadata.name`) its Crewlet `name`.
- A Seat's handle is always written out (`spec.handle`, or its
  `metadata.name`), so its identity never depends on how Crewlet would derive
  one from its name. Its Crewlet `name` is its `displayName`, or its handle.
- Crewlet names a unit's `lead` and a seat's `manages` entries by **name**,
  not by handle or id, so that is how the operator writes them. Crewlet reads
  a name that belongs to both a seat and a unit as the seat, so a Seat that
  manages a Unit named like another Seat cannot be rendered (see below).

### Models

`providers.llm_order` is written out in full: the Company's `spec.llm.order`,
then its own `LLMProvider`s by name, then the class's platform providers by
name. A seat with no `default` chain runs on the first one alone: Crewlet
tries no other provider of the order when that one's keys are all cooling
down, so a seat that needs a fallback names a chain. No provider may be named
`default`, since Crewlet would run every such seat on the provider keyed
`default` instead, whatever the order says.

Each Seat's `spec.llm` becomes Crewlet's per-phase `llm` mapping, each chain
naming providers by their key in `providers.llm`.

### Placement

Every agent seat runs in a pool: the one its Seat's `spec.placement.pool`
names, or else the Company's `spec.topology.defaultSeatPool` (`data` unless
it says otherwise). `data` is the data pods; any other pool is one of the
Company's `spec.topology.seatPools`, stateless pods that run seats and hold
nothing (see [A seat pod's `crewlet.yaml`](#a-seat-pods-crewletyaml)). Each
agent seat is rendered with Crewlet's `role.placement`, a hard pin: the engine
runs the seat only on a node carrying every label it names, and never widens
it, so a seat whose pool has no pod runs nowhere and its messages wait.

| The seat is placed in | Its `placement` | The `node.labels` of the pods that run it |
|---|---|---|
| `data` | `{labels: {crewlet.ai/pool: data}}` | the data pods: `{crewlet.ai/pool: data}` |
| a Shared pool `P` | `{labels: {crewlet.ai/pool: P}}` | every pod of `P`: `{crewlet.ai/pool: P}` |
| a PerSeat pool, as seat `h` | `{labels: {crewlet.ai/seat: h}}` | the seat's own pods: `{crewlet.ai/seat: h}` |

Every pod that runs seats carries exactly one of these labels, so each pod is
in exactly one of Crewlet's placement groups. That is what makes the pins
hold: Crewlet gives a node a share of every group it matches and fills the
sum from whichever seats it reaches first, so a pod matching two groups could
fill itself from one and leave a seat of the other held by nobody, with no
warning. For the same reason every agent seat is pinned, those on the data
pods included: an unpinned seat could land on any pod that runs seats. Pins
are literal values, since Crewlet expands no `${NAME}` in a placement. Human
seats are never run by the engine and have no placement.

### Credentials

Every credential position holds a reference, `${NAME}`, to an entry of the
engine's encrypted secret store, never a value. Each referenced Secret key
gets its own entry, whose value the operator pushes into the store; the
[credentials](credentials.md) page lists every position and the field that
fills it.

## The data pod's `crewlet.yaml`

The data pod holds the company's estate, serves the API on port 8080, runs
the background workers and runs the seats placed in `data` (`node.roles:
[data, ingress, seats, workers]`, `node.labels: {crewlet.ai/pool: data}`).
Its `crewlet.yaml` is shaped for a company that adds seat pods, so that
adding them moves nothing on the data pod's disk: coordination is Crewlet's
replicated `embedded-kv`, and the event stream is persisted on the data
volume under `/var/lib/crewlet`.

When the EngineClass's `seatPools.policy` is `Allowed`, the data pod also
opens the leaf listener seat pods join the event stream through
(`stream.leaf.port: 7422`), from its first start and whether or not the
Company has a pool yet: the class decides it, not the Company, so a company
that adds its first pool restarts nothing, and a class that changes its
policy restarts its companies' data pods. Like every port of Crewlet's
embedded broker, the listener accepts any connection that reaches it, with no
authentication and no TLS.

The event stream is given three quarters of the data volume
(`stream.store_max_bytes`), measured from the size the pod's volume claim
requests. Left unset, Crewlet would take three quarters of the free space it
finds on the filesystem, which is the volume only when the volume is a disk
of its own: a provisioner that carves claims out of one node filesystem
(local-path, hostPath, kind's default) shows every engine on the node the
node's whole free space, and engines that share it over-commit it until the
last one is refused with `insufficient storage resources available`. Crewlet
needs at least 5 GiB for the stream, so an EngineClass's `data.storage.size`
is at least `7Gi`.

It holds the node's credentials as values: the two API tokens (the
operator's own, and the one published for consumers in
`status.apiAccessSecretRef`), every key of the secret store's keyring, and
the object store's keys when the class keeps files in S3 and the Company
names its own (`spec.objects.credentialsSecretRef`). That is why it lives in a Secret,
`<company>-bootstrap`, and why the pod mounts that one key of it, readable
by the engine's user alone. Its one reference is `${CREWLET_NODE_ID}`, the
pod's name and the node's stable identity, which the pod's environment
carries.

None of these credentials is an environment variable, on purpose. Crewlet
resolves a `${NAME}` in the company document from its secret store and then
from its process environment, and it does so in fields that hold no
credential as well, such as an MCP server's `url` or a model provider's
`base_url`. A credential in the engine's environment would be one anyone who
can write such a field, or hold the API's access token, could have the
engine send to a server of their choosing.

The engine's API answers no read without a token
(`api.auth.allow_anonymous_read: false`): its reads include every seat's
conversations and memory, and every consumer of an operator-run engine has a
token. Health probes and vendors' webhooks need none.

Under the management policy `Enforce` it also names the operator's token the
company document's only writer (`api.auth.company_writers: [operator]`), so the
engine refuses every other token a change to the document, which the
operator would otherwise overwrite at its next look. Under `Observe` and
`Seed` the list is absent: there the engine's own users own the document. The
pod reads `crewlet.yaml` only at start, so changing the policy to or from
`Enforce` restarts it ([Switching policies](../guides/management-policies.md#switching-policies)).

## A seat pod's `crewlet.yaml`

A seat pool's pods hold no data: they run seats and nothing else
(`node.roles: [seats]`), and need no company document, since they take the
one the data pod runs from the company's coordination store. Each pool's
pods share one `crewlet.yaml`, kept like the data pod's in a Secret,
`<company>-bootstrap-<pool>` ([Seat pools](runtime.md#seat-pools)):

| Block | What it holds, and why |
|---|---|
| `node` | `id: ${CREWLET_NODE_ID}`, the pod's name; the pool's placement label, `crewlet.ai/pool: <pool>` for a Shared pool and `crewlet.ai/seat: ${CREWLET_SEAT}` for a PerSeat pool, whose pods are each given their seat's handle in `CREWLET_SEAT`; and `max_concurrent`, the pool's `maxConcurrentTurns` or the class's `seatPools.defaults.maxConcurrentTurns`, left to the engine's default of 32 when neither says. |
| `store` | `scratch: true`: the store at `/var/lib/crewlet/store/crewlet.db` is deleted at every start. It is a cache of the pod's seats' memory and its working rows, rebuilt from the event stream when the pod claims a seat; the pod's tracker and audit records go to a data pod. `objects` is the data pod's block, value for value: every node checks the object store it names against the one the company recorded and refuses to start on another. |
| `stream` | `type: embedded` with `leaf.urls`, one `nats-leaf://` URL on port 7422 for every data pod, by its name in the data StatefulSet's headless Service: the pod's broker joins the data pods' as a leaf, with JetStream off, holding nothing and voting in nothing, through any member that answers. `replicas` is the data pod's, since a leaf that is first to use a stream creates it on the members with its own count. |
| `coordination` | `embedded-kv`, over the same link. |
| `secrets` | The company's keyring, as the data pod has it: the pod opens the company document and the secret store itself. |
| `retention` | The class's `backupOwner`, as the data pod has it. |

There is no `api` block and no API token: a node without the `ingress` role
serves nothing, so a seat pod has no listener at all. The engine pinned by
this operator serves no health or readiness endpoint on such a node either;
a wedged one exits by itself.

## What blocks a document

A document is applied whole or not at all, so one object that cannot be
rendered blocks the next revision for the whole company. The API server
checks each object on its own; what only the set of objects can show is
refused when the document is rendered, and reported on the object that holds
the reference:

| On | When | Reason |
|---|---|---|
| `Seat` | `unitRef`, a `manages` entry, a `spec.llm` provider, a `spec.mcp` entry or a `sandbox.mcpServerRefs` entry names an object that does not exist | `RefNotFound` |
| `Seat` | a `spec.llm` entry names a platform provider the class does not offer, or the Company does not set `spec.llm.usePlatformProviders` | `RefNotFound` |
| `Seat` | a `manages` entry names a Unit whose name is also a Seat's | `Conflict` |
| `Seat` | `spec.placement.pool` is neither `data` nor one of the Company's `spec.topology.seatPools` | `PoolNotFound` |
| `Seat` | it is placed in a PerSeat pool, and the class does not set `seatPools.allowPerSeat` (reported on `Applied`) | `PerSeatNotAllowed` |
| `Seat` | it is placed in a PerSeat pool, and the company's seat pods, its own included, would number more than the class's `seatPools.maxPods` (reported on `Applied`) | `PoolCapacityExceeded` |
| `Company` | it declares `spec.topology.seatPools`, and its class's `seatPools.policy` is not `Allowed` | `SeatPoolsNotAllowed` |
| `Company` | its Shared pools' replicas alone number more than the class's `seatPools.maxPods` | `PoolCapacityExceeded` |
| `Unit` | `parentRef`, `leadRef` or a `spec.mcp` entry names an object that does not exist | `RefNotFound` |
| `Unit` | its chain of parents leads back to it | `Cycle` |
| `Company` | `spec.llm.order` names a provider that does not exist or is not offered to it | `RefNotFound` |
| `LLMProvider` | its name is also the name of a platform provider the company uses | `Conflict` |
| `Integration` | another Integration of the company, earlier by name, connects the same vendor | `Conflict` |
| any, the `Company` included | a string it renders into the document, `engineConfig` included, holds a `${NAME}` reference | `ReferenceNotAllowed` |

The last row is the operator's, not Crewlet's. Crewlet resolves a `${NAME}`
from the engine's secret store and then its environment in many fields that
hold no credential (an MCP server's `url`, a provider's `base_url`, a
sandbox setup command, a skill variable), so a reference an object holds
itself would name values that are not its own: the class's platform model
keys in the store, or the engine's environment. The only references in a
rendered document are the ones the operator writes for a Secret key the
object references. The API server already refuses a `${NAME}` in the typed
fields Crewlet is known to expand; the render check covers every other
string. A sandbox setup command keeps shell syntax Crewlet leaves alone,
such as `$HOME` and `${1:-x}`.

Everything else Crewlet checks itself, when the operator dry-runs the
rendered document. Crewlet locates each problem by its path in the document
(`units[0].roles[1].llm`), and the operator traces that path back to the
object that rendered that part of it, which reports `Applied=False` with the
reason `Invalid` and the engine's own words. [How the document is
applied](applying.md) covers the rest of what is reported where.
