# ADR-0008 — Run a seat's executor in its own engine pod or in E2B

- **Status:** accepted
- **Authority:** `internal/render` — `ExecutorOf` and the comment on where an
  agent seat's executor runs, in `executor.go`;
  `docs/guides/cli-agents.md`, "Where a seat's executor runs"
- **Enforced-by:** `internal/render.TestExecutor`,
  `internal/render.TestContractExecutorIsAppliedWhereItCannotRun`,
  `internal/render.TestContractDirectCell`,
  `internal/render.TestRunnerCLIsFollowTheEngineSource`,
  `internal/render.TestRunnerCLIsAreTheRunIn`,
  `internal/render.TestContractEngineRefusesWhatItHolds`,
  `internal/render.TestContractDeepWorkFollowsTheEngine`,
  `internal/render.TestContractExecutorFloorIsTheEngines`,
  `internal/render.TestMirroredEngineDeclarationsAreUnchanged`,
  `test/crd.TestCLIAgentRunInFollowsTheEngine`,
  `test/crd.TestSandboxPlacementValidation`,
  `test/crd.TestSeatEmptySandboxIsRefused`,
  `test/crd.TestEngineClassSandboxes`,
  `test/crd.TestEngineClassDataScratch`,
  `internal/controller.TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn`,
  `internal/controller.TestEnginePodsBoundWhatTheyWriteOutsideABox`,
  `internal/controller.TestEveryEnginePodIsGivenTheFleetsBridgeAddress`,
  the runtime suite's bridge spec, the runtime and pools suites' pod and
  policy specs,
  the config suite's executor specs, the e2e pools spec

## Context

Every agent seat's executor is its coding CLI's own run: the vendor's whole
harness, with its own shell and editor, detached from the turn that launched
it, in a box. Crewlet places the run in a cell of its `providers.sandbox`
catalogue: the `run_in` of the `providers.llm` entry the launch picks, else
the catalogue's `default_run_in`. Its cells are `direct`, a process tree of
the engine's own host, `container`, a container the engine starts, and `e2b`,
a machine of its own per run. It launches a run only on `claude-code` and
`opencode`, the CLIs it has an in-box runner for, passing every other member
of a seat's chain over, and it refuses on every write a company with an agent
seat and a model provider and no catalogue, a seat whose chain holds no such
CLI, and a `run_in` on any other CLI.

A box calls the seat's tools back through the tool bridge, `/mcp/`. A
`direct` box dials its own node's listener on loopback: a node that serves
the API's ingress serves the bridge on its public listener where it binds
one, and a node without the role on `api.port`, to loopback alone, where
loopback reaches it (Crewlet's `boxHostURL`). Every other box, an `e2b` one,
dials `CREWLET_MCP_BRIDGE_URL`, which is one address for the whole fleet
(Crewlet's ADR-0056), and Crewlet refuses each launch it can reach no bridge
for. A `direct` box is made under
`providers.sandbox.local.state_dir`, else under the engine user's home, and
its run is a process of the node that launched it, which alone drives it
(Crewlet's ADR-0057), and dies with that node's container, when Crewlet
carries the run's round on in a fresh run. An `e2b` box is
given only the variables that sign its CLI in, so a login a credential bundle
restores, whose refresh token the engine keeps rotating on its data volume,
cannot follow a run there.

The operator runs the engine on two kinds of pod: a data pod, which holds the
company's estate on its volume and serves the API and the public listener,
and a seat pod, which runs seats alone and serves its probes. Both run
non-root, with the security context the restricted Pod Security Standard
admits, on a read-only root filesystem, with no container runtime. A data
pod's volume is three quarters event stream (`store_max_bytes`), and a seat
pod's home is on its scratch volume, which its class bounds. A tenant's
code already runs in the pod as the engine's user: a stdio tool server is a
command an `MCPServer` names. The operator applies a company's document whole
and holds it only for what cannot be applied safely
([ADR-0005](0005-hold-a-document-only-for-what-cannot-be-applied-safely.md)).

## Decision

**Two cells, chosen per provider.** `SandboxPlacement` offers `Direct`, a
process tree of the engine pod that holds the seat, a data pod or a seat pod
alike, and `E2B`, the Company's E2B sandbox. A run's cell is its provider's
`LLMProvider.spec.runIn`, else the Company's `spec.sandbox.defaultRunIn`,
which a `spec.sandbox` must state: there is no implicit cell, and the
document always writes `default_run_in`. The API admits `runIn` on
`ClaudeCode` and `OpenCode` alone (`render.RunnerCLIs`, held to Crewlet's
runners), `defaultRunIn: E2B` only beside `spec.sandbox.e2b`, and no empty
`Seat.spec.sandbox`, which would say nothing to Crewlet while reading as a
switch. A platform provider names no `runIn` and takes the default.

**Refused where Crewlet refuses it, on the object to change**
(`EngineRefuses`): a Company with an agent seat and a model provider and no
`spec.sandbox` (`SandboxRequired`), a Seat whose executor chain holds no
`ClaudeCode` or `OpenCode` provider (`NoExecutorCLI`, at `spec.llm.default`,
or at `spec.llm` for a seat that runs on the head of the Company's order),
and an LLMProvider placed in `E2B` that a seat's executor or the Company's
deep work can launch on while the Company configures no `e2b`
(`SandboxPlacementNotOffered`).

**Reported where Crewlet applies the seat and refuses its launches**: on the
Agent Seat's `Ready`, with no condition of its own. `NotExposed`: the run
goes to `E2B` and the Company has no public address.
`SandboxLoginNotPortable`: the run goes to `E2B` and nothing in the run's
environment signs its CLI in. The verdict judges the first runner member of
the chain, as a launch does, and while a node holds a seat that can run, the
`LeaseHeld` message names the provider, its CLI, its cell and the providers
a launch falls back to.

**Every engine pod has a volume for its boxes.** A disk-backed `emptyDir`
named `sandboxes`, mounted from the pod's first start at `render.SandboxDir`,
which the document names as `providers.sandbox.local.state_dir` whenever a
run can be placed `Direct`, and configures no local backend otherwise. The
EngineClass bounds it (`data.sandboxSizeLimit`,
`seatPools.defaults.sandboxSizeLimit`): 20Gi unset, at least 1Gi, a ceiling
that reserves nothing (`render.DefaultSandboxSizeLimit` says why).

**Every other place an engine pod writes is bounded too, but its data
volume.** Crewlet confines a seat's `Direct` run to nothing and points only
its home and temporary directories into its box, so a command that names
`/tmp` or the engine's home outright writes outside the box. Both are on the
pod's `scratch` `emptyDir`, a data pod's as a seat pod's, bounded by the
class (`data.scratchSizeLimit`, `seatPools.defaults.scratchSizeLimit`):
10Gi unset, at least 256Mi, a ceiling that reserves nothing
(`render.DefaultScratchSizeLimit` says why). One volume holds both, so one
limit bounds what lands outside the data volume and the boxes.

**Every engine pod is given the bridge's address.** `CREWLET_MCP_BRIDGE_URL`
is the Company's `status.endpoints.publicBaseURL` while it has one, on a data
pod and a seat pod alike, and unset otherwise; nothing else decides it, and
the controller reads no LLMProvider for it. A seat's `Direct` box dials its
own pod's listener on loopback, a data pod's public listener or a seat pod's
`api.port`, which the render binds on every interface so that loopback
reaches it; every `E2B` box and every deep-work run, on the data pod too,
dial the public address, where a data pod relays each call to the pod
holding the run. While the class sends telemetry, every engine pod is given
the same address as `CREWLET_SANDBOX_OTEL_RECEIVER_URL`, to which an `E2B`
box exports its traces; a `Direct` box exports to its own pod's listener on
loopback, which every pod given the collector serves its own boxes.

**No class gate on `Direct`.** An EngineClass bounds a `Direct` run by the
pod's resources (`data.resources`, `seatPools.defaults.resources`) and its
sandboxes volume, and forbids nothing about it.

**Deep work follows Crewlet's own placement**: its provider's `runIn`, else
`E2B` where the Company configures it, else the data pod that opened the
turn, confined; wherever it runs, it calls Crewlet back at the Company's
public address, never on loopback. The API requires `spec.sandbox` beside
`spec.assistant.deepWork`, so where deep work runs is a choice the Company
states, and admits any provider, a platform one included; Crewlet's dry run
refuses one that drives another CLI than `ClaudeCode`. The API docs and the
guides say where a run on the pod keeps its box, and what that costs (below).

**A token ceiling below Crewlet's executor floor is not refused here.**
`turn_engine.executor_min_budget_tokens` is tunable through
`spec.engineConfig`, which the operator does not interpret, so the dry run's
refusal reaches the Seat or the Company as `Invalid`, and the API docs state
the floor.

## Consequences

A `Direct` run is a process of the engine pod, as the engine's user, with
the CLI's own shell: it reads what the engine reads, the keyring and the
secret store among it, and on a data pod the company's data volume. What
confines it is the pod's boundary, the restricted security context and the
class's network policy. A company that wants its runs off the data volume
places its agent seats in a seat pool or runs them in `E2B`. A `Direct` run
ends with its container: a restart, a rolling update or an eviction of the
pod (a box past its volume's limit evicts the whole pod, as a run's writes
past the scratch volume's do) ends the runs in flight on it, while an `E2B`
run goes on. Crewlet carries each lost run's round on in a fresh run, at
most twice a round, the third loss ending the seat's turn. A restart of the
container alone keeps the `sandboxes` `emptyDir`, and the fresh run carries
on in the lost run's box, its working tree as that run left it; a pod that
is replaced takes the boxes with it, and the fresh run starts in a fresh
box, from what the lost one pushed, a loss of the working tree that a volume
that outlived the pod would spare only at the cost of a volume claim for
every engine pod. Nothing measured
sets the 20Gi and 10Gi defaults; they err high because they cost nothing
until written and reaching one evicts the pod. A `Direct` run on a data pod can
still write the data volume by its path, which no `emptyDir` limit bounds:
the volumes bound where a run's writes land, not what the run may name, and
confining a `Direct` run to its box is Crewlet's to do.

A deep-work run placed on the data pod is not on the sandboxes volume.
Crewlet makes its confined box beside its store
(`(*assistantDeep).boxRoot`: `/var/lib/crewlet/store/assistant-boxes` under
the store path the render writes), not under
`providers.sandbox.local.state_dir`, so what the run writes counts against
the data volume, which no `sandboxSizeLimit` bounds and which a run that
filled it would stop the store and the broker on: the risk the sandboxes
volume exists to keep a seat's `Direct` run from. The confinement keeps the
run from the engine's files, not from the volume's space. A Company that
will not take that risk places deep work in `E2B`. Moving the box root is
Crewlet's to do, and the mirror of `boxRoot` fails the day it moves.

A seat's `Direct` box reaches its tools and sends its traces on its own
pod's listener, a seat pod's included, so a Company with no public address
runs every `Direct` executor, in any pool, and only its `E2B` runs and its
deep work need the address. A deep-work run on a data pod reaches Crewlet
through the Company's public route and a data pod's relay, so the data pods
need egress to the public address. Inside the cluster a call to the route's
load balancer usually lands on the route's proxies without leaving it,
which a CNI that polices after the translation (Calico) or never matches a
pod by its address (Cilium) judges by the proxy's pod, so under `Isolated`
the data pods may reach the peers the public port admits (the webhooks
route's proxy namespaces, or its external forwarders), on any port, as
their ports are the implementation's; a load balancer the call leaves the
cluster through must stay outside `egress.exceptCIDRs`. A seat pod, where
nothing calls the public route, is given no such rule. A Company that gains
or changes its public address restarts its seat pods as well as its data
pods, for the runs they launch in `E2B`. Should Crewlet stop serving a seat
pod's own boxes on loopback, the mirror of Crewlet's `boxHostURL` fails the
day it does.

Rejected: Crewlet's `container` cell, which starts a container per box, and
an engine pod has no container runtime, nor the privileges under its
security context to run one. A class-level switch forbidding `Direct`
(`ClassPolicy`): a tenant's code already runs as the engine's user in its
pod, and a platform provider's key is already one the tenant can read
(`PlatformLLMProvider`), so it would guard nothing the tenant cannot reach.
Boxes on the data volume, which a run could fill under the store and the
broker, or under the engine's home, the pod's scratch volume, whose limit is
sized for what the engine's processes write outside a box. Separate
`emptyDir` volumes for a data pod's `/tmp` and `HOME`, two limits whose sum
nobody set, or none, which let what a run writes outside its box fill the
node's disk. A generic ephemeral volume, which would turn the eviction into
a disk-full error inside the box at the price of a claim provisioned on
every pod start, a storage dependency a seat pod otherwise does not have,
for a box with nothing to keep past its pod.
The bridge's address only while some run is placed in `E2B`: which cell a
run takes is the document's, applied without a restart, so the variable
would roll every pod each time a provider moved and leave a pod started
before the move without the address its runs now dial. An in-cluster
address for seat pods: the address is one fleet value, and an `E2B` box
handed it could reach nothing. Holding the document over a seat whose
launches fail: Crewlet applies it and fails that seat alone. Deep work in
`E2B` alone: on the data pod Crewlet confines it, kept from the engine's
data, configuration and keyring, which a seat's `Direct` run is not, so what
once kept it off the pod, the secret store there, no longer sets it apart.
What the confinement does not bound is the data volume's space, where
Crewlet keeps the box (Consequences): a run there can stop the whole company
that placed it, not only the person's deep work. It is what a run may do
once placed where its Company's own document put it, as a seat's `Direct`
run on the data pod may, writing the volume as the engine's user, not what
applying the document does to another object, which is what ADR-0005 holds
a document for: so it is stated, in the API docs and the guides beside the
way out (`E2B`), and not held. A minimum on `tokenBudget`: it would be
wrong for every Company whose `engineConfig` moves the floor.

Nothing here runs a real executor: the published engine image carries no
CLI, and no test calls a model. The contract suites hand every document the
render writes for either cell to the engine, and the e2e suite writes to the
sandboxes volume as the engine's user on a data pod and a seat pod.

This record does not decide which seats run on which pod (the Company's
`spec.topology`), how the public address is chosen
([ADR-0006](0006-tell-the-engine-an-address-it-is-reached-at.md)), which of
a provider's variables reach a box, how Crewlet isolates a `direct` box or
confines a deep-work run, nor what an E2B template carries; Crewlet and the
Company's own objects decide those.
