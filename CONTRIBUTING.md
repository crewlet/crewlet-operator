# Contributing to crewlet-operator

Thanks for your interest in contributing. This document covers getting a
development environment running, the rules a change has to follow, and how to
land it.

## Development setup

Prerequisites: **Go** (the version `go.mod` names; with the default
`GOTOOLCHAIN=auto` the go command fetches it for you), **Docker**, **make**,
**git** and **curl**. That is the whole list. Every other tool — controller-gen,
kustomize, setup-envtest and the Kubernetes test binaries, golangci-lint,
kind, kubectl, helm, kubebuilder, crd-ref-docs, and the Crewlet engine itself —
is pinned in the [`Makefile`](Makefile) and installed into `bin/` the first
time a target needs it, so your machine's copies never stand in for them.

```bash
git clone https://github.com/crewlet/crewlet-operator.git
cd crewlet-operator
make help     # every target, grouped
make build    # bin/manager
```

To run the operator against a cluster, see
[docs/getting-started.md](docs/getting-started.md).

## Two Go modules

The repository holds two modules:

| Module | Path | Contains |
|---|---|---|
| `github.com/crewlet/crewlet-operator` | `/` | the manager (`cmd/`), the controllers (`internal/`), the e2e suite (`test/`) |
| `github.com/crewlet/crewlet-operator/api` | `api/` | the `crewlet.ai` API types, their markers and generated deepcopy |

**Why the API is a module of its own:** consumers such as a provisioning control
plane import the types to write these objects, and they should get exactly
the types — not controller-runtime, not the operator's dependency tree, and
not a version bump every time the operator's own dependencies move. The API
module therefore depends on `k8s.io/api` and `k8s.io/apimachinery` only, and
registers its types with apimachinery's scheme builder. A depguard rule in
[`.golangci.yml`](.golangci.yml) refuses controller-runtime and the
operator's packages under `api/`; do not add other dependencies there without
discussing it first. Its `go` line is deliberately the floor its own
dependencies impose rather than the newest Go, because a library's `go` line
is a minimum every importer inherits.

The root module requires the API module through `replace ./api`, so a change
to the types and the code that uses them lands in one pull request. Every Go
target in the Makefile (`fmt`, `vet`, `tidy`, `lint`, `test`) runs in both
modules, because `./...` and controller-gen's package loading stop at a
nested module's boundary. When you add a dependency, run `make tidy`, which
tidies both.

The API module is versioned with its own tags, `api/vX.Y.Z`; see
[RELEASING.md](RELEASING.md).

## Running checks

```bash
make check
```

That is one target because it answers one question — *would CI accept this
change?* It runs, in order:

| Step | What it checks |
|---|---|
| `fmt-check` | every Go file in both modules is gofmt-formatted |
| `tidy-check` | `go mod tidy -diff` is empty in both modules |
| `verify-generated` | regenerating deepcopy, CRDs, RBAC, the chart and the API reference changes nothing |
| `vet` | `go vet` in both modules |
| `lint-config` | `.golangci.yml` is a valid golangci-lint configuration |
| `lint` | golangci-lint in both modules, with the logcheck and kube-api-linter plugins |
| `helm-lint` | the Helm chart passes `helm lint --strict` and renders with its default values |
| `build` | the manager binary builds |
| `test` | unit, envtest and engine suites of both modules, under `-race` |
| `signoff-check` | every commit your branch adds carries a DCO sign-off |
| `signoff-test` | the sign-off gate's own suite |

`verify-generated` runs before `build` and `test` on purpose: both regenerate
files, and a step that rewrites the tree before the comparison would hide the
drift it exists to catch.

Two more suites are not part of `make check`:

- `make test-e2e` creates a kind cluster, builds the manager image and the
  Crewlet engine image, loads both, deploys the manager with kustomize, and
  checks it from the outside: that it runs and serves metrics, and that a
  company runs end to end — its engine becomes ready and is given its
  document and Secret values, a consumer its class admits reads the API with
  the published token, is refused a change to the document the operator
  enforces, and a pod of another namespace is refused, an edited Seat becomes a new revision, and deleting the Company
  keeps or deletes its volume and keyring as its `deletionPolicy` says; and
  that a company's seat pools run, the engine places each seat in its own
  pool, and the data pod's leaf listener admits the company's own seat pods
  and no other pod. Then
  it deletes the cluster, unless the suite failed: the cluster is left for a
  look, and `make cleanup-test-e2e` deletes it. CI runs it on every pull
  request. The engine image
  is built from source at `CREWLET_VERSION` with `make crewlet-image`, from
  the crewlet module the Go command downloads and checks against the
  checksum database, the same source `make crewlet` builds `bin/crewlet`
  from. On a host that cannot run kind's default cluster,
  `KIND_CONFIG` names a kind cluster configuration to create it with.
- `make docker-build` builds the image; CI's `build` job runs it too.

The [`ci.yml`](.github/workflows/ci.yml) jobs call these same targets, so a
local run and CI run the same commands with the same pinned tools.

### The engine suite

Every package that speaks to Crewlet is tested against a real Crewlet engine,
not a mock of one. `internal/engine/enginetest` starts the `crewlet` binary on
a free loopback port with a throwaway configuration in the test's temporary
directory, and stops it when the test ends; the engine client in
[`internal/engine`](internal/engine), the secret sync in
[`internal/secretsync`](internal/secretsync) and the config reconciler's
envtest suite in [`internal/controller`](internal/controller), which hands
each Company an engine of its own, run against it. The renderer in
[`internal/render`](internal/render) hands every document it renders to the
same binary's `crewlet validate`, with the secret sync's store names in every
credential position, and applies it to a running engine to check that the
engine's encoding of it reads back as no difference. The CRD suite in
[`test/crd`](test/crd) holds the `LLMProvider` rules over a `CLIAgent`
provider's sign-in, and the sign-in table of
[`docs/guides/cli-agents.md`](docs/guides/cli-agents.md), to the same binary:
it reads each CLI's token and key variable from the engine's own refusals,
tries every CLI the API offers in every sign-in shape, each driving a seat,
and compares the API server's verdict with what `crewlet validate` admits
and doubts and what `crewlet llm list` reports the CLI signed in with, so an
engine whose profiles move a token or key variable fails it. It holds the
rule over an OpenCode provider's `model` the same way, trying every CLI with
models with and without a provider, and with segments of white space, so an
engine that marks another profile as reading `<provider>/<model>` fails it
too. And it holds the `claudeModel` enum to the engine's table of Claude
models, read from its refusal of an id the table does not list, so an
engine that adds or drops a row fails it. A mock would agree with the code by construction; the real engine is
what tells the two apart when Crewlet's API or schema moves.

What no validation shows is what signs a coding agent in at a launch, which
the renderer's refusals of a seat's code work turn on. The renderer's
`TestCodeWorkFollowsTheEngineSource` reads it from the pinned engine's own
source (`enginetest.Source`, the module `make crewlet` built from): the
variables that sign Claude Code in from a seat's environment, and Claude
Code's token and key variables. It holds the lists and not the rules around
them, so when you move `CREWLET_VERSION`, read `newCodingRun`,
`claudeCodeOnForeignEntry` and `sandboxCredentials` in crewlet's
`internal/engine/sandbox.go` against `internal/render/codework.go`. Two of
the renderer's three refusals there, `SandboxModelUnsupported` and
`SandboxLoginNotPortable`, mirror what the engine refuses only when a seat
launches a coding run, never on a write: no `crewlet validate` can show
them drifting, and a refusal the engine has dropped would keep a whole
company from applying over a seat that now works. That is a deliberate
choice, as `AgentModeBridgeUnsupported` is: the engine would apply such a
seat, report it applied, and refuse every coding run it started.

The engine is pinned like every other tool, as `CREWLET_VERSION` in the
Makefile, and `make crewlet` builds it into `bin/crewlet` (`make test` does so
first). To run these tests outside make — from an IDE, or with a plain
`go test ./internal/render` or `go test ./test/crd` — build it once with
`make crewlet`, or point `CREWLET_BIN` at a `crewlet` binary of your own. A
test that finds no binary **fails** and says which of the two to do; it
never skips, because a skipped contract test reads as a passing one. When a
test fails, the engine's log is printed with it.

## Generated files

Never edit these by hand; change their source and regenerate.

| Output | Source | Target |
|---|---|---|
| `api/**/zz_generated.deepcopy.go` | the types in `api/` | `make generate` |
| `config/crd/bases/*.yaml` | `+kubebuilder` markers in `api/` | `make manifests` |
| `config/rbac/role.yaml` | `+kubebuilder:rbac` markers in `internal/` | `make manifests` |
| `dist/chart/templates/**` | the kustomize output of `config/` | `make chart` |
| `docs/reference/api.md` | doc comments and markers in `api/` | `make api-docs` |

After changing an API type or an RBAC marker, run
`make manifests generate chart api-docs` and commit the result with the change.
In the chart, `Chart.yaml`, `values.yaml`, `templates/NOTES.txt` and
`templates/_helpers.tpl` are written once by the generator and then curated by
hand; `values.schema.json` and `templates/tenant-provisioner/` are written by
hand, since the generator has no way to make a template opt-in; everything
else under `templates/` is regenerated on every run.

New kinds and webhooks are scaffolded with the pinned `bin/kubebuilder`
(`make kubebuilder`), never written from scratch, and the
`// +kubebuilder:scaffold:*` markers stay where they are: the CLI inserts code
at them. Run kubebuilder with no `--group`: the API group is the domain
itself, `crewlet.ai`.

## Changing the API

The API follows the
[Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
closely enough that nobody has to learn a house style:

- `spec` and `status` with the status subresource; `metav1.Condition`s with a
  `Ready` summary and `observedGeneration` on the status and on every
  condition, so Argo CD's and Flux's health checks work unmodified.
- Enum values in PascalCase; lists of named things as `+listType=map` with
  `+listMapKey=name`, never maps. The one exception is the string-to-string
  maps Kubernetes itself uses for labels, annotations and node selectors.
- **Credentials by reference, never by value.** A credential is a
  `SecretKeyReference` (one key of a Secret) or a `SecretReference` (a whole
  Secret whose keys the field documents), in the object's own namespace; only
  `EngineClass` uses the `Namespaced…` variants. Every position Crewlet treats
  as a credential takes a reference, even where the value would not be secret,
  because the API cannot tell the two apart. No field accepts a literal
  secret, and the pass-through `engineConfig` may not name a block that holds
  credentials. [docs/concepts/credentials.md](docs/concepts/credentials.md)
  maps every Crewlet credential position to its field, from the table in
  [`internal/render/credentials_table_test.go`](internal/render/credentials_table_test.go);
  a test holds that table to the positions the pinned engine's schemas mark
  `x-crewlet-secret`, in both directions, and the page to the table, so a
  credential Crewlet adds fails the build until it has a field or a reason
  it has none. Another holds the `engineConfig` rule to the same marks: a
  credential Crewlet adds under a new top-level block fails it until the rule
  refuses that block.
- References stay within the object's namespace. Only the cluster-scoped
  `EngineClass` may reference objects in another namespace.
- Validation is OpenAPI plus CEL (`x-kubernetes-validations`) — immutability
  as `self == oldSelf`, unions, cross-field rules — each rule with a message a
  user can act on. There are no admission webhooks in `v1alpha1`, so
  installing the operator never requires cert-manager.
- Durations are integer `…Seconds` fields; every kind is in the `crewlet`
  category; everything the operator creates carries `app.kubernetes.io/*`
  labels and an owner reference.

Every API change comes with a test that the API server enforces it. The
suite in [`test/crd/`](test/crd) installs the generated CRDs into envtest and
is table-driven per kind: a valid object first, then, for each rule, the
object it must refuse and the rule's message; transition rules are tested by
an update. It also applies every sample in `config/samples`. The suite in
[`test/chart/`](test/chart) does the same for the chart's RBAC and admission
policies: it renders the chart with `bin/helm`, installs them, and acts as
the manager and the tenant provisioner to check what each may do. `make lint` runs
[kube-api-linter](https://github.com/kubernetes-sigs/kube-api-linter) over
`api/`, which holds the types to the conventions above: `+optional` or
`+required` on every field, a pointer only where the zero value is a setting
of its own, bounds on every string and list, a list type on every list.

### Compatibility, by API version

| Version | What a change may do |
|---|---|
| `v1alpha1` (now) | Anything, including renaming or removing fields and kinds. Breaking changes are called out in the pull request and the release notes. No conversion is provided. |
| `v1beta1` | Fields and kinds are deprecated before they are removed, and removed only in a later API version. Conversion between served versions is provided by a conversion webhook. |
| `v1` | Stable: no field is removed or changes meaning within `v1`. |

From `v1beta1` on, deprecations follow the
[Kubernetes deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/):
a deprecated beta version is still served for 9 months or 3 releases,
whichever is longer; a GA version is never removed within a major version;
and a field or kind is never removed from an API version that still serves
it.

## Latest versions

Every dependency, tool, base image and GitHub Action is on its newest release,
and every pin is exact: Go modules by version, images by digest with the tag
beside it, Actions by full commit SHA with the version in a trailing comment.
"Newest" is read from the source each time — the Go module proxy
(`https://proxy.golang.org/<module>/@latest`), the project's releases, the
image registry — never from memory.

A version that is held back on purpose carries its reason in a comment at the
pin, and the reason has to be one somebody can re-check: an image tag that is
not published yet, a release that breaks something named.

Two cases are settled once, here:

- **Indirect Go dependencies** stay where the direct ones put them. The
  `k8s.io/*` staging modules and their helpers (`k8s.io/kube-openapi`,
  `sigs.k8s.io/structured-merge-diff`) are released in lockstep, and a blanket
  `go get -u ./...` mixes generations that do not build together. The
  `k8s.io/*` staging modules themselves all sit on the same patch release.
- **The API module's `go` line** is the floor of its own dependencies, as
  described [above](#two-go-modules).

Dependabot opens weekly pull requests for Go modules (both modules), GitHub
Actions and the Dockerfile's base images. It cannot see the rest, so these are
checked by hand when you touch the build, and bumped in a `build(deps)`
commit:

| Pin | Where | Newest from |
|---|---|---|
| tool versions (`*_VERSION`) | `Makefile` | the Go module proxy, or the project's releases |
| the Crewlet engine (`CREWLET_VERSION`) | `Makefile` | crewlet's releases; between releases, the commit of `main` the engine client is written against |
| `ENVTEST_K8S_VERSION` | `Makefile` | `bin/setup-envtest list` |
| golangci-lint and its plugins | `.custom-gcl.yml` (version must equal `GOLANGCI_LINT_VERSION`) | the Go module proxy |
| the curl image | `test/e2e/e2e_test.go` | Docker Hub |
| the Gateway API CRDs the controller suite installs | `internal/controller/testdata/gateway-api` | `make gateway-api-crds`, after Dependabot bumps `sigs.k8s.io/gateway-api`; the suite fails until the two match |
| cosign (`cosign-release`) | `.github/workflows/release.yml` | sigstore/cosign's releases |

## Documentation

Docs are part of the change. A pull request that changes an API field,
behaviour, a make target or the deployment updates the page under
[`docs/`](docs/README.md) that describes it, and regenerates
`docs/reference/api.md` (`make api-docs`) when the types changed. A field's
documentation is written once, as the doc comment on the field; the API
reference is generated from it.

Comments in code explain **why**, and especially why the obvious alternative
is wrong; the diff already shows what the code does. Diagrams are Mermaid.

A decision that binds more than one package is recorded in [`adr/`](adr/README.md).

## Commit messages

Subjects follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): summary

Optional body explaining why, wrapped at 72 columns.

Fixes #123
Signed-off-by: Jane Developer <jane@example.com>
```

- **Type**, required: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`,
  `build` (the Makefile, the Dockerfile, `go.mod`, and every dependency bump),
  `ci` (`.github/workflows`, `.github/dependabot.yml`), `chore`, `revert`.
- **Scope**, the area the change lands in: `api`, `controller`, `render`,
  `engine`, `secretsync`, `chart`, `e2e`, `ci`, `docs`, `build`, `deps`,
  `scripts`. Drop it only for a change that spans the whole repository.
- **Summary**: imperative ("add", not "added"), lowercase first word unless it
  is an identifier, no trailing period, 72 characters or fewer including the
  prefix. Say what changed for someone using the operator.
- A breaking change to a released surface gets a `!` (`feat(api)!: …`) and a
  `BREAKING CHANGE:` footer that says how to migrate.
- One change per commit: a fix made in passing gets its own commit.

### Sign your work

Every commit needs a `Signed-off-by` trailer, which `git commit -s` writes:

```bash
git commit -s -m "fix(controller): requeue a company whose class is missing"
```

The trailer certifies the [Developer Certificate of Origin](DCO) over that
commit: that you wrote the change or have the right to submit it under the
project's MIT licence, and that the commit and your sign-off are public and
kept indefinitely. Because the DCO also covers relaying somebody else's work
(clauses (b) and (c)), a commit may carry several sign-offs and the trailer
need not name the author.

This is enforced. `scripts/check-signoff.sh` judges the commits your branch
adds, from its merge base with the base branch; it runs in `make check` and in
CI's `sign-off` job, and fails the build on a missing trailer. Merge commits
are exempt. To repair commits you already made, rewriting them:

```bash
git commit --amend -s --no-edit             # the last commit
git rebase --signoff <merge-base>           # every commit on the branch
```

Use the merge-base SHA the gate prints rather than `origin/main`: in a fork
whose `main` is stale, rebasing onto the ref restamps commits you never wrote.

## Pull requests

1. Fork the repository and create a branch.
2. Make the change, with tests and documentation.
3. Commit with `git commit -s`.
4. Run `make check`.
5. Open the pull request. **Its title is a release note:** GitHub generates
   each release's notes from the titles of the pull requests merged since the
   previous tag, and a squash merge makes the title the commit subject on
   `main` as well. Use the `type(scope): summary` form and write the summary for
   someone deciding whether to upgrade. Labels sort it into the categories in
   [`.github/release.yml`](.github/release.yml).

Small, focused pull requests are much easier to review. For a substantial
change — a new kind, a new controller, a reshaped field — open an issue first
to agree on the approach.

## Reporting bugs and requesting features

Use [GitHub issues](https://github.com/crewlet/crewlet-operator/issues) and
the templates there. For security issues, do **not** open a public issue — see
[SECURITY.md](SECURITY.md).
