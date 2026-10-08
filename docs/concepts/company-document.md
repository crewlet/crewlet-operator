# What the engine is given

A Crewlet engine reads two documents: the **company document** (Crewlet's
Tier B: the company, its org chart, models, tools and integrations), which
the operator applies over the engine's API, and the data pod's
**`crewlet.yaml`** (Tier A: how the node boots), which the operator mounts
into the pod. The operator renders both from the `crewlet.ai` objects. This
page says what they contain, so that what the engine reports can be traced
back to an object.

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
name. A seat that names no provider for a phase runs on the first one.

Each Seat's `spec.llm` becomes Crewlet's per-phase `llm` mapping, each chain
naming providers by their key in `providers.llm`.

### Placement

Every agent seat is rendered with `placement: {labels: {crewlet.ai/pool:
data}}`, and the data pod carries the label `crewlet.ai/pool: data`. In
v1alpha1 every seat runs on the data pod, so the pin changes nothing today;
it is what will keep a seat on its own pool once a company can run more than
one kind of pod. Human seats are never run by the engine and have no
placement.

### Credentials

Every credential position holds a reference, `${NAME}`, to an entry of the
engine's encrypted secret store, never a value. Each referenced Secret key
gets its own entry, whose value the operator pushes into the store; the
[credentials](credentials.md) page lists every position and the field that
fills it.

## The data pod's `crewlet.yaml`

The data pod holds the company's estate, serves the API on port 8080, runs
every seat and runs the background workers (`node.roles: [data, ingress,
seats, workers]`). Its `crewlet.yaml` is already shaped for a company that
later adds pods of other kinds, so that adding them moves nothing on the
data pod's disk: coordination is Crewlet's replicated `embedded-kv`, and the
event stream is persisted on the data volume under `/var/lib/crewlet`.

It holds no credential either. It refers to environment variables, which the
pod reads from Secrets the operator owns in the Company's namespace:

| Variable | Holds |
|---|---|
| `CREWLET_NODE_ID` | the pod's name, the node's stable identity |
| `CREWLET_TOKEN_OPERATOR`, `CREWLET_TOKEN_ACCESS` | the API tokens: the operator's own, and the one published for consumers in `status.apiAccessSecretRef` |
| `CREWLET_SECRET_KEY_<ID>` | each key of the secret store's keyring |
| `CREWLET_S3_ACCESS_KEY_ID`, `CREWLET_S3_SECRET_ACCESS_KEY` | the object store's keys, when the class's `objects.s3` names a `credentialsSecretRef` |

The engine's API answers no read without a token
(`api.auth.allow_anonymous_read: false`): its reads include every seat's
conversations and memory, and every consumer of an operator-run engine has a
token. Health probes and vendors' webhooks need none.

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
| `Unit` | `parentRef`, `leadRef` or a `spec.mcp` entry names an object that does not exist | `RefNotFound` |
| `Unit` | its chain of parents leads back to it | `Cycle` |
| `Company` | `spec.llm.order` names a provider that does not exist or is not offered to it | `RefNotFound` |
| `LLMProvider` | its name is also the name of a platform provider the company uses | `Conflict` |
| `Integration` | another Integration of the company, earlier by name, connects the same vendor | `Conflict` |

Everything else Crewlet checks itself, when the operator dry-runs the
rendered document. Crewlet locates each problem by its path in the document
(`units[0].roles[1].llm`), and the operator traces that path back to the
object that rendered that part of it, which reports `Applied=False` with the
reason `Invalid` and the engine's own words. [How the document is
applied](applying.md) covers the rest of what is reported where.
