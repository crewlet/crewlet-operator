# ADR-0008 — Run a seat's executor in the engine pod that holds the seat

- **Status:** accepted
- **Authority:** `internal/render` — `ExecutorOf` and the comment on where an
  agent seat's executor runs, in `executor.go`;
  `docs/guides/harnesses.md`, "Where a seat's executor runs"
- **Enforced-by:** `internal/render.TestExecutor`,
  `internal/render.TestContractDirectCell`,
  `internal/render.TestExecutorHarnessesFollowTheEngineSource`,
  `internal/render.TestContractExecutorRefusalsReachTheirObject`,
  `internal/render.TestContractExecutorFloorIsTheEngines`,
  `internal/render.TestMirroredEngineDeclarationsAreUnchanged`,
  `test/crd.TestSandboxSettingsValidation`,
  `test/crd.TestSandboxSetupFileValidation`,
  `test/crd.TestSetupFilePathsFollowTheEngine`,
  `test/crd.TestSeatEmptySandboxIsRefused`,
  `test/crd.TestEngineClassSandboxes`,
  `test/crd.TestEngineClassDataScratch`,
  `internal/render.TestTheScratchFloorHoldsTheEnginesSpoolRoom`,
  `internal/controller.TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn`,
  `internal/controller.TestEnginePodsBoundWhatTheyWriteOutsideABox`,
  `internal/controller.TestNoEnginePodTemplateReadsThePublicAddress`,
  the runtime and pools suites' pod and policy specs,
  the config suite's executor specs, the e2e pools spec

## Context

Every agent seat's executor is its harness's own run: the vendor's whole
harness, with its own shell and editor, detached from the turn that launched
it, in a box. Crewlet places the run in a cell of its `providers.sandbox`
catalogue: the `run_in` of the `providers.llm` entry the launch picks, else
the catalogue's `default_run_in`. Its cells are `direct`, a process tree of
the engine's own host, and `container`, a container the engine starts, both
served by its `local:` block, which names neither alone, so the catalogue
states a `default_run_in`. It launches a run only on a harness it has an
in-box runner for, its built-in profiles that carry an `executor` block,
passing every other member of a seat's chain over, and it refuses on every
write a company with an agent seat and a model provider and no catalogue,
and a seat whose chain holds no such harness.

A box calls the seat's tools back through the tool bridge, `/mcp/`, and sends
its traces to the telemetry receiver. A `direct` box dials its own node's
listener on loopback: a node that serves the API's ingress serves both on its
public listener where it binds one, and a node without the role on
`api.port`, to loopback alone, where loopback reaches it (Crewlet's
`boxHostURL`). Only a box off its node's loopback, a `container` one, dials
an address the node is handed for it. A `direct` box is made under
`providers.sandbox.local.state_dir`, else under the engine user's home, is
seeded with its provider's login, and its run is a process of the node that
launched it, which alone drives it (Crewlet's ADR-0065), and dies with that
node's container, when Crewlet carries the run's round on in a fresh run.

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

**One cell, on the pod that holds the seat.** Every executor run is
Crewlet's `direct` cell, a process tree of the engine pod that holds the
seat, a data pod or a seat pod alike. The document writes
`providers.sandbox` for every company, `default_run_in: direct` and its
`local.state_dir`, and no `providers.llm` entry names a `run_in`. The
Company's `spec.sandbox` only tunes the boxes (Crewlet's
`host_grace_seconds`, `default_pause_ttl_seconds`, `default_max_turns`,
`commit_attribution` and `setup`), and the API refuses an empty one, and an
empty `Seat.spec.sandbox`, which would say nothing to Crewlet while reading
as a switch. The API refuses a setup file at an absolute path, which no box
writes, and leaves a relative one that climbs out of the home to Crewlet,
since how `..` resolves is no rule the API server can follow.

**Nothing about a seat's executor is refused or reported.** Every harness
the API offers has an in-box runner (`render.ExecutorHarnesses`, held to
Crewlet's built-in profiles that carry an `executor` block), so every chain
runs: a box on the pod reaches the seat's tools on loopback in any pool,
with or without a public address, and is seeded with any sign-in. While a
node holds the seat, the `LeaseHeld` message names the provider a launch
runs on, its harness, the pod it runs in, and the providers a launch falls
back to.

**Every engine pod has a volume for its boxes.** A disk-backed `emptyDir`
named `sandboxes`, mounted from the pod's first start at `render.SandboxDir`,
which the document names as `providers.sandbox.local.state_dir`. The
EngineClass bounds it (`data.sandboxSizeLimit`,
`seatPools.defaults.sandboxSizeLimit`): 20Gi unset, at least 1Gi, a ceiling
that reserves nothing (`render.DefaultSandboxSizeLimit` says why).

**Every other place an engine pod writes is bounded too, but its data
volume.** Crewlet confines a seat's run to nothing and points only its home
and temporary directories into its box, so a command that names `/tmp` or
the engine's home outright writes outside the box. Both are on the pod's
`scratch` `emptyDir`, a data pod's as a seat pod's, bounded by the class
(`data.scratchSizeLimit`, `seatPools.defaults.scratchSizeLimit`): 10Gi
unset, at least 561Mi (`render.MinScratchSizeLimit` says why), a ceiling
that reserves nothing (`render.DefaultScratchSizeLimit` says why). One volume
holds both, so one limit bounds what lands outside the data volume and the
boxes.

**No engine pod is handed an address for its boxes.** A box dials its own
pod's listener on loopback, a data pod's public listener or a seat pod's
`api.port`, which the render binds on every interface so that loopback
reaches it, and exports its traces there wherever the class sends telemetry.
So no engine pod's environment reads the Company's public address.

**No class gate on a run.** An EngineClass bounds a run by the pod's
resources (`data.resources`, `seatPools.defaults.resources`) and its
sandboxes volume, and forbids nothing about it.

**A token ceiling below Crewlet's executor floor is not refused here.**
`turn_engine.executor_min_budget_tokens` is tunable through
`spec.engineConfig`, which the operator does not interpret, so the dry run's
refusal reaches the Seat or the Company as `Invalid`, and the API docs state
the floor.

## Consequences

A run is a process of the engine pod, as the engine's user, with the
harness's own shell: it reads what the engine reads, the keyring and the
secret store among it, and on a data pod the company's data volume. What
confines it is the pod's boundary, the restricted security context and the
class's network policy. A company that wants its runs off the data volume
places its agent seats in a seat pool. A run ends with its container: a
restart, a rolling update or an eviction of the pod (a box past its volume's
limit evicts the whole pod, as a run's writes past the scratch volume's do)
ends the runs in flight on it. Crewlet carries each lost run's round on in a
fresh run, at most twice a round, the third loss ending the seat's turn. A
restart of the container alone keeps the `sandboxes` `emptyDir`, and the
fresh run carries on in the lost run's box, its working tree as that run
left it; a pod that is replaced takes the boxes with it, and the fresh run
starts in a fresh box, from what the lost one pushed, a loss of the working
tree that a volume that outlived the pod would spare only at the cost of a
volume claim for every engine pod. Nothing measured sets the 20Gi and 10Gi
defaults; they err high because they cost nothing until written and
reaching one evicts the pod. A run on a data pod can still write the data
volume by its path, which no `emptyDir` limit bounds: the volumes bound
where a run's writes land, not what the run may name, and confining a run
to its box is Crewlet's to do.

A Company with no public address runs every executor, in any pool, and
nothing on an engine pod calls the public route, so under `Isolated` no
engine pod is given egress to the route's proxies, and a Company that gains
or changes its public address restarts its data pods alone. Should Crewlet
stop serving a seat pod's own boxes on loopback, the mirror of Crewlet's
`boxHostURL` fails the day it does.

Rejected: Crewlet's `container` cell, which starts a container per box, and
an engine pod has no container runtime, nor the privileges under its
security context to run one. A field on the provider and one on the Company
naming the cell, each with one legal value: a field that can say one thing
is a ceremony, and it would read as a choice. Handing every engine pod the
Company's public address for its boxes: no box the operator's pods make
dials it, and a pod reads its environment only when it starts, so every
change of the address would restart every seat pod for nothing. A
class-level switch forbidding runs on the pod (`ClassPolicy`): a tenant's
code already runs as the engine's user in its pod, and a platform provider's
key is already one the tenant can read (`PlatformLLMProvider`), so it would
guard nothing the tenant cannot reach. Boxes on the data volume, which a run
could fill under the store and the broker, or under the engine's home, the
pod's scratch volume, whose limit is sized for what the engine's processes
write outside a box. Separate `emptyDir` volumes for a data pod's `/tmp` and
`HOME`, two limits whose sum nobody set, or none, which let what a run
writes outside its box fill the node's disk. A generic ephemeral volume,
which would turn the eviction into a disk-full error inside the box at the
price of a claim provisioned on every pod start, a storage dependency a seat
pod otherwise does not have, for a box with nothing to keep past its pod. A
minimum on `tokenBudget`: it would be wrong for every Company whose
`engineConfig` moves the floor.

Nothing here runs a real executor: the published engine image carries no
harness, and no test calls a model. The contract suites hand every document
the render writes to the engine, and the e2e suite writes to the sandboxes
volume as the engine's user on a data pod and a seat pod.

This record does not decide which seats run on which pod (the Company's
`spec.topology`), how the public address is chosen
([ADR-0006](0006-tell-the-engine-an-address-it-is-reached-at.md)), which of
a provider's variables reach a box, nor how Crewlet isolates a `direct` box;
Crewlet and the Company's own objects decide those.
