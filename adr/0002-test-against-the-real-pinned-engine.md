# ADR-0002 — Test against the real, pinned engine, and fail without it

- **Status:** accepted
- **Authority:** `internal/engine/enginetest` — the package doc
- **Enforced-by:** `internal/engine/enginetest.TestBinaryFailsWithoutTheEngine`

## Context

The operator speaks to Crewlet in two ways: it renders documents the engine
must accept (`internal/render`), and it calls the engine's REST API
(`internal/engine`, `internal/secretsync`, the config reconciler in
`internal/controller`). Both are Crewlet's contract, not the operator's, and
Crewlet changes them on its own schedule: a field renamed, a rule added, a
reference resolved in a field nobody expected. A fake engine reproduces what
its author believed about the engine at the time, so a test against one
keeps passing after the engine has moved.

## Decision

Every test of what the engine accepts or does runs the real engine binary,
built from the `github.com/crewlet/crewlet` module at the version the
Makefile pins (`CREWLET_VERSION`, `make crewlet`), through
`internal/engine/enginetest`: `crewlet validate` for rendered documents, and
a running engine for the API. A test that needs the binary and does not find
it fails, naming the command that builds it; it never skips. Fakes stay for
what the engine cannot be made to do on demand (a refusal of a given shape,
a proxy in front of it), in the engine client's own unit tests.

## Consequences

Bumping `CREWLET_VERSION` runs every contract against the new engine, which
is how a drift is found before a cluster finds it. The cost is a Go build of
the engine before the suites run and an engine process per test that needs
one, and that a developer cannot run those packages with a bare `go test`
until `make crewlet` (or `CREWLET_BIN`) provides the binary. A skip would be
cheaper and is wrong here: a skipped contract test reads as a pass in a CI
summary. The e2e suite's engine image is built from the same module and
version (`make crewlet-image`); this record does not decide which engine
image a platform runs (`docs/guides/installing.md`).
