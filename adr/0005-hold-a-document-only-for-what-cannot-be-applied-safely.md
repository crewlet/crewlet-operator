# ADR-0005 — Hold a company's document only for what cannot be applied safely

- **Status:** accepted
- **Authority:** `internal/render` — `Basis`, `RefError` and the package
  doc; `docs/concepts/company-document.md`, "What blocks a document"
- **Enforced-by:** a compile error (`refErrors.add` takes a `Basis`),
  `internal/render.TestEveryFindingHasABasis`,
  `internal/render.TestTheDocsStateTheBases`,
  `internal/render.TestAnUndeclaredPoolPinsNoPod`,
  `internal/render.TestContractEngineRefusesWhatItHolds`,
  `internal/render.TestContractReportedIsApplied`,
  `internal/render.TestContractExecutorIsAppliedWhereItCannotRun`,
  `internal/render.TestContractExecutorRefusalsReachTheirObject`,
  `internal/secretsync.TestContractStoreRefusesWhatResolveRefuses`

## Context

The operator applies a company's document whole or not at all, and the last
good revision keeps running while it is held (`Applied=False` on the part,
`ConfigApplied=False` on the Company). A hold costs every object of the
company: no change to any of them reaches the engine until the object at
fault is fixed. Some findings describe a document Crewlet would refuse
anyway, or one whose effect reaches objects that are not at fault. Others
describe one object that would merely not work, which Crewlet itself
applies and then refuses at run time, or runs without: a seat's executor
placed in E2B on a login that cannot follow it there, or where its box has
no tool bridge to call the seat's tools back through, a seat pinned to a
pool nobody declared. Held for the second kind,
one seat's mistake stops every other change to the company.

## Decision

The operator holds the document for a finding only when it cannot be
applied safely, which is when one of these is true, and each place in the
render that records a finding names which (`render.Basis`):

1. **EngineRefuses**: the engine cannot take the object as it is. It
   refuses the document on a write, or the document has no place to write
   the object (a Schedule lives inside the entry of the role or unit that
   runs it, so one of a seat that does not exist cannot be written; a seat
   whose executor chain has no CLI that runs it, or a company with agent
   seats and no sandbox to place their runs in, is refused on every write).
   Applying it is impossible; refusing it first only says so on the object
   at fault.
2. **HarmsBeyond**: applied, it would harm more than the object at fault:
   other objects' identity, authority, credentials or work (a Unit left out
   with its subtree, a `${NAME}` that resolves another object's value, a
   name written for an object that does not exist yet and so granted to
   whichever object takes it later, a CLI login every seat pod refreshes on
   its own).
3. **ClassPolicy**: the EngineClass's administrator forbids it (seat pools,
   PerSeat pools, the seat-pod budget, the platform providers offered).

Anything else is applied and reported (`Reported`), on the object at fault
and on each object that cannot do its job because of it: `ResolvedRefs=False`
with the reason when a reference the object holds names what is not there,
`Ready=False` with the reason when the object cannot do its job at all (a
seat whose every executor launch Crewlet refuses: `NotExposed`,
`SandboxLoginNotPortable`), and a condition for the
capability when only part of it fails. The operator records a Warning event
when such a condition turns False or changes its reason, since nothing on
the Company says so. What the operator writes for an object is that object's spec, whole:
it never leaves part of one out, nor one object of several, to make a
document applicable. And what it writes for a reported finding must not
reach past its object later either: a seat placed in a pool the Company does
not declare is pinned to a label of its own (`crewlet.ai/undeclared-pool`),
never the pool's, because the pods of the pool, once declared, would carry
the pool's label and run the seat under a revision that was never checked
against what they can run (a CLI login kept as a credential bundle, held as
harm beyond the object), and the running revision's pins keep pods and open
the data pods' listener.

The controller's own holds follow the same rule: a Secret key too large for
the engine's store (`SecretTooLarge`) and the engine's refusals of a dry run
or a write (`Invalid`, `WriteRefused`) on the engine's word; a missing Secret
(`SecretNotFound`, one store entry serves every object that references the
key, and a deleted Secret is often a revocation) and a store-state Secret
that is someone else's (`NameConflict`) as harm beyond the object.

## Consequences

A seat whose executor cannot run, a seat pinned to a pool the Company does
not declare, a provisioned credential nothing
provisions yet and a person named in GitLab's access levels no longer hold
the rest of the company. The cost is that a reported finding can sit
unnoticed by a health check that reads only the Company's `Ready`, which is
why it is on the affected object's own `Ready` where it stops the object
entirely, and why each comes with an event. A finding classified
`EngineRefuses` is held to the engine by a contract case, so an
engine that starts accepting it fails a test rather than leaving the
operator stricter than the engine; one that is `Reported` is held the other
way, so an engine that starts refusing it fails too. One reason can hold on
one basis in one place and on another, or not at all, elsewhere
(`RefNotFound` is the engine's refusal as Datadog's route, harm beyond the
object as a GitLab access level), so the basis belongs to the place, not the
reason. The obvious alternative, holding every finding, makes the whole
company's next revision depend on its least-configured seat; the other
obvious one, writing what the render can and leaving the rest out, writes a
company nobody asked for. This record supersedes one clause of the last
sentence of ADR-0004's decision, that a seat names a provisioned value only
while an Integration provisions: such a seat is now applied and reported
(`NoProvisioner`). That a Unit's credentials cannot be provisioned stands. It does not decide which conditions exist, nor how the
engine treats a refused run.
