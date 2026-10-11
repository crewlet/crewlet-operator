# ADR-0002 — Test against the real engine, at Crewlet's main, and fail without it

- **Status:** accepted
- **Authority:** `internal/engine/enginetest` — the package doc
- **Enforced-by:** `internal/engine/enginetest.TestBinaryFailsWithoutTheEngine`,
  `internal/render.TestMirroredEngineFunctionsAreUnchanged`,
  `test/crd.TestSamplesAreAccepted`, `test/crd.TestGettingStartedIsAccepted`

## Context

The operator speaks to Crewlet in two ways: it renders documents the engine
must accept (`internal/render`), and it calls the engine's REST API
(`internal/engine`, `internal/secretsync`, the config reconciler in
`internal/controller`). Both are Crewlet's contract, not the operator's, and
Crewlet changes them on its own schedule: a field renamed, a rule added, a
reference resolved in a field nobody expected. A fake engine reproduces what
its author believed about the engine at the time, so a test against one
keeps passing after the engine has moved.

Crewlet has released no engine that carries its Go code, and its `main`
changes every day. A pinned commit of it is an engine nobody runs a week
later: the operator would keep passing against it while `main` moved away,
and would meet every change at once, whenever somebody moved the pin.
Crewlet publishes `main` as an image, `ghcr.io/crewlet/crewlet:main`,
built after each push for the newest commit of its `main`, signed by its
workflow and labelled with the commit it was built from
(`org.opencontainers.image.revision`).

## Decision

Every test of what the engine accepts or does runs the real engine, and the
engine is Crewlet's `main` as that image publishes it, never a pin of it.
The e2e suite runs the image (the Makefile's `CREWLET_IMG`, whose default is
`CREWLET_MAIN_IMG`, pulled and loaded into kind by `make crewlet-image`).
Every other suite runs the Crewlet binary `make crewlet` installs from the
Crewlet module at the commit the image was built from, which
`test/enginecommit` reads from the image's label in its registry; never at
the module proxy's `@main`, which runs ahead of the image, since Crewlet
builds it minutes after a merge and not at all for a merge that starts no
workflow or whose build fails. Those suites reach it
through `internal/engine/enginetest`: `crewlet validate` for rendered
documents, a running engine for the API, and the engine's module source for
the decisions the engine makes only at run time, whose lists the operator
copies and whose functions it fingerprints
(`TestMirroredEngineFunctionsAreUnchanged`). The samples and the guide that
name an engine image name this one.

A test that needs the binary and does not find it fails, naming the command
that installs it; it never skips. Fakes stay for what the engine cannot be
made to do on demand (a refusal of a given shape, a proxy in front of it), in
the engine client's own unit tests. CI runs the suites on every pull request
and every push to main. `CREWLET_VERSION` names another engine
for `make crewlet` (a Crewlet commit or module version), to run one again,
and `CREWLET_IMG` names another image for the e2e suite: one the container
tool already has, such as an image built from a Crewlet checkout, is run as
it is, not pulled.

A release of the operator keeps the engine it was tested against: its
workflow copies Crewlet's `main` image, by the digest it resolved, into
`ghcr.io/crewlet/crewlet-operator/engine` before its gate tests that
commit, and once the gate passes tags the copy with the release's version
and signs it. The release names it, and installers run it.

## Consequences

The operator's CI can turn red with no change to the operator, and that is
the point: a Crewlet change that breaks the operator fails the operator's
next run, rather than the pull request that
eventually moves a pin over it and a dozen other changes. The fix goes where
the fault is, in the operator when Crewlet's change is right and in Crewlet
when it is not.

A run is reproduced only by naming its engine. Every job that runs one logs
its commit (`make crewlet` and `make crewlet-image` print it), and
`CREWLET_VERSION=<commit>` installs it again for the suites that run the
binary. The e2e suite needs an image of that commit too, and GHCR deletes
the image `main` named once `main` has moved on and the image is an hour
old, usually minutes after the next push
([Images from main](https://github.com/crewlet/crewlet/blob/main/RELEASING.md#images-from-main)).
So an e2e run of an older commit is repeated from a copy somebody made of
its image while it was `main`, from a release's engine image, or from an
image built from Crewlet's source at the commit (Crewlet's `make
image-context`, then `docker buildx build --load`), named as `CREWLET_IMG`.
The test and e2e jobs each read `main` when they start, so a run in which
`main` moved between them tests two commits, each named in its own job's
log. A release reads it once, and its gate tests the commit it kept.

What the engine decides only at run time, the operator can hold only by its
code: a fingerprinted function the engine changes fails the build until
somebody has read it against the operator file that copies it and recorded
its new digest, which costs a read for every change to those functions,
comment edits and reflowed lines aside.

The cost of running the engine at all is a Go build of it whenever `main`
moves, an engine process per test that needs one, a network read of the
registry and the module proxy each time `make crewlet` runs, and that a
developer cannot run those packages with a bare `go test` until `make
crewlet` (or `CREWLET_BIN`) provides the binary. A skip would be cheaper and
is wrong here: a skipped contract test reads as a pass in a CI summary.

An engine image named by digest, by `main` or by a commit carries no
version the operator can order, so `DowngradeRefused` never protects a class
that names one; a release's engine is tagged with the release's version,
which it does order.

This record does not decide which engine image a platform runs
([Installing](../docs/guides/installing.md#engine-image)), nor how a
release publishes the engine it keeps ([RELEASING.md](../RELEASING.md)).
