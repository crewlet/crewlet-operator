# Architecture decision records

A decision that binds **more than one package** — or the API and the
controllers together — is recorded here. A decision that lives inside one
package is recorded in that package's doc comment, where `go doc` puts it
beside the code, and gets no record here. That single rule keeps this
directory from becoming a second, drifting copy of the package docs.

## The fields

| Field | What it is for |
|---|---|
| **Status** | `proposed`, `accepted`, or `superseded by ADR-NNNN`. Once a release has carried a record, it is never edited into a different decision, because the reasoning somebody acted on has to stay readable: a superseded record keeps its number and its text, and a record a later one changes in part stays `accepted` and names the part that later record supersedes. A record no release has carried has had no reader outside this repository, so when its decision changes it is rewritten in place, its file renamed for its new title, like any other surface no release has shipped. Numbers are never reused. |
| **Authority** | The one package or page whose own doc holds the detail. The record states the decision and why; the detail stays at the authority, and the authority's doc names the record back (`ADR-NNNN`), so the decision is in front of whoever is about to change the code. |
| **Enforced-by** | A named test, a compile error, or the literal `nothing`. A rule held by prose alone decays; this field makes it visible which rules have a gate. `nothing` is an allowed and honest answer, but it has to be written down. |
| **Context** | The forces that make the decision necessary. |
| **Decision** | What is decided and what it forbids. |
| **Consequences** | What it costs, the obvious alternative and why it is wrong here, and what the record does not decide. |

## Writing one

Copy [`0000-template.md`](0000-template.md), take the next free number, name
the file `NNNN-a-short-imperative-title.md`, add the record's id to its
authority's doc, and add a row below. A record is proposed in a pull request
like any other change.

## The records

| # | Decision | Enforced by |
|---|---|---|
| [0001](0001-name-store-entries-by-their-secret-reference.md) | Name every store entry the operator owns by its Secret reference | `TestCompanyRefsAreExactlyThePointers`, `TestStoreNameIsDeterministic`, `TestOwnsOnlyTheOperatorsNames`, `TestSyncCollectsOnlyTheOperatorsNames` |
| [0002](0002-test-against-crewlet-main-and-fail-without-it.md) | Test against the real engine, at Crewlet's main, and fail without it | `TestBinaryFailsWithoutTheEngine`, `TestMirroredEngineDeclarationsAreUnchanged`, `TestSamplesAreAccepted`, `TestGettingStartedIsAccepted` |
| [0003](0003-own-a-seats-pause-only-where-the-seat-asks.md) | Own a seat's pause only where its Seat sets `spec.paused` | `TestSyncPauseChangesNoPauseItDoesNotOwn`, the config suite's pause spec |
| [0004](0004-leave-a-seats-provisioned-credentials-to-the-engine.md) | Leave a seat's provisioned credentials to the engine, under a name the operator derives | `TestProvisionedNameIsTheEnginesAlone`, `TestProvisionedNameIsDeterministic`, `TestCompanyRefsAreExactlyThePointers`, `TestEveryReferenceInADocumentIsTheOperators` |
| [0005](0005-hold-a-document-only-for-what-cannot-be-applied-safely.md) | Hold a company's document only for what cannot be applied safely | a compile error, `TestEveryFindingHasABasis`, `TestTheDocsStateTheBases`, `TestAnUndeclaredPoolPinsNoPod`, `TestContractEngineRefusesWhatItHolds`, `TestContractReportedIsApplied`, `TestContractBoxModeIsAppliedWhereItCannotRun`, `TestContractCodeWorkOnWrite`, `TestContractStoreRefusesWhatResolveRefuses` |
| [0006](0006-tell-the-engine-an-address-it-is-reached-at.md) | Tell the engine an address it is reached at, always | `TestContractBootstrap`, `TestContractAddressShapes`, `TestAddressShapesFollowTheEngine`, `TestBootstrapNeedsTheEnginesAddresses`, `TestEngineAddressesFollowWhereTheEngineIsReached`, the runtime suite's exposure specs |
| [0007](0007-open-the-artifact-origin-only-where-it-is-reached.md) | Open the artifact origin only where it is reached | `TestBootstrapServesOutsidePartiesOnTheirOwnListener`, `TestContractArtifactAddressShapes`, `TestArtifactsBaseURLFollowsTheEngine`, `TestEngineAddressesFollowWhereTheEngineIsReached` |
