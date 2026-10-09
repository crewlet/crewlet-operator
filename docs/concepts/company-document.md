# What the engine is given

A Crewlet engine reads two documents: the **company document** (Crewlet's
Tier B: the company, its org chart, models, tools and integrations), which
the operator applies over the engine's API, and the data pod's
**`crewlet.yaml`** (Tier A: how the node boots), which the operator keeps in
a Secret and mounts into the pod. The operator renders both from the `crewlet.ai` objects. This
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
| `LLMProvider` | `providers.llm.<metadata.name>`; a `CLIAgent` provider's `cli.state_dir` is `/var/lib/crewlet/llm-cli/<loginName>`, on the data volume |
| `MCPServer` | one entry of `mcp_servers`, named by its `metadata.name` |
| `Integration` | one of `integrations.slack`, `github`, `gitlab`, `mattermost`, `jira`, `confluence`, `datadog` and `atlassian`; a Seat it names (Datadog's `routeTo`, GitLab's `accessLevels`) by the seat's handle |
| `Unit` | one entry of `units`, or of its parent's `children` |
| `Seat` | one entry of its Unit's `roles`, or of the top-level `roles` when it has no Unit |
| `Schedule` | one entry of the `schedules` of the Seat's role or the Unit it targets, named by its `metadata.name` |

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

### Schedules

A Schedule is written into the entry of what it targets: a Seat's role, or a
Unit. Within one, schedules are ordered by `metadata.name`, which is also the
schedule's Crewlet `name`, half of the identity the engine claims each fire
under; since a `metadata.name` never changes, editing a Schedule never lets a
fire run twice.

| Field | Crewlet's | Notes |
|---|---|---|
| `spec.schedule` | `cron` | Five fields. The API checks the shape; the engine's dry run checks each field's values against the grammar its scheduler evaluates with |
| `spec.timeZone` | `timezone` | Unset fires on the company's clock (`timezone`, from the Company's `spec.timeZone`) |
| `spec.prompt` | `task` | |
| `spec.target.fanOut` | `target` | `each` or `lead`; a unit's schedule only. Unset is Crewlet's own default, `each` |
| `spec.suspend: true` | `enabled: false` | Unset or `false` is left out: Crewlet's default fires the schedule |
| `spec.timeoutSeconds` | `timeout_seconds` | Unset is Crewlet's default, 180 seconds |
| `spec.catchUp` | `catchup` | Unset is Crewlet's default, `true` |

Crewlet refuses a schedule on a person's seat, since it runs no turn for a
person: a Schedule whose `target.seatRef` names a Human seat cannot be
rendered, and reports `ResolvedRefs=False` with the reason `HumanSeat`. The
mistakes only the engine can see — a cron value out of range, a zone it
cannot load, `Each` on a unit with no agent seat directly in it, `Lead` on a
unit whose lead is a person — are refused by its dry run at the schedule's
own place in the document, and so reported on the Schedule.

The engine does not report when a schedule last fired in a form a status
field could carry: its record of the fires it dispatched is each node's own
table, which the answering node serves, while the fleet's claim on each fire
is internal to its coordination store. A Schedule therefore has no
`lastScheduleTime`; the engine's dashboard (**Agents › Schedules**) shows
each schedule's next fire and its history.

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
| `Seat` | it names a provisioned credential and no Integration of the company has `provisioning: Engine` | `NoProvisioner` |
| `Unit` | `parentRef`, `leadRef` or a `spec.mcp` entry names an object that does not exist | `RefNotFound` |
| `Unit` | its chain of parents leads back to it | `Cycle` |
| `Company` | `spec.llm.order` names a provider that does not exist or is not offered to it | `RefNotFound` |
| `LLMProvider` | its name is also the name of a platform provider the company uses | `Conflict` |
| `LLMProvider` | it runs a CLI in `mode: Agent` and the company has no public address for the sandbox to call its tools back through | `NotExposed` |
| `Integration` | another Integration of the company, earlier by name, connects the same vendor | `Conflict` |
| `Integration` | Datadog's `routeTo.seatRef` or a GitLab `accessLevels` entry names a Seat that does not exist | `RefNotFound` |
| `Integration` | Datadog's `routeTo.seatRef` or a GitLab `accessLevels` entry names a Human seat | `HumanSeat` |
| any, the `Company` included | a string it renders into the document, `engineConfig` included, holds a `${NAME}` reference | `ReferenceNotAllowed` |

The last row is the operator's, not Crewlet's. Crewlet resolves a `${NAME}`
from the engine's secret store and then its environment in many fields that
hold no credential (an MCP server's `url`, a provider's `base_url`, a
sandbox setup command, a skill variable), so a reference an object holds
itself would name values that are not its own: the class's platform model
keys in the store, or the engine's environment. The only references in a
rendered document are the ones the operator writes for a Secret key the
object references, and for a seat's credentials the engine provisions
([Credentials](credentials.md#credentials-the-engine-provisions)). The API server already refuses a `${NAME}` in the typed
fields Crewlet is known to expand; the render check covers every other
string. A sandbox setup command keeps shell syntax Crewlet leaves alone,
such as `$HOME` and `${1:-x}`.

Everything else Crewlet checks itself, when the operator dry-runs the
rendered document. Crewlet locates each problem by its path in the document
(`units[0].roles[1].llm`), and the operator traces that path back to the
object that rendered that part of it, which reports `Applied=False` with the
reason `Invalid` and the engine's own words. [How the document is
applied](applying.md) covers the rest of what is reported where.
