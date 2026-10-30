# Contributing to the Crewlet Operator

Thanks for your interest in contributing. This document covers getting a
development environment running, the rules a change has to follow, and how to
land it.

## Development setup

Prerequisites: **Go** (the version `go.mod` names; with the default
`GOTOOLCHAIN=auto` the go command fetches it for you), **Docker** (28 or
newer for `make test-e2e`), **make**,
**git** and **curl**. That is the whole list. Every other tool — controller-gen,
kustomize, setup-envtest and the Kubernetes test binaries, golangci-lint,
kind, kubectl, helm, kubebuilder and crd-ref-docs — is pinned in the
[`Makefile`](Makefile) and installed into `bin/` the first time a target needs
it, so your machine's copies never stand in for them. The Crewlet engine is
installed there too, and is the one thing not pinned: every suite runs
Crewlet's `main` ([The engine suite](#the-engine-suite)).

```bash
git clone https://github.com/crewlet/crewlet-operator.git
cd crewlet-operator
make help     # every target, grouped
make build    # bin/manager
```

To run the operator against a cluster, see
[docs/getting-started.md](docs/getting-started.md).

### Running the manager from your machine

While working on the operator, `make run` runs the manager in your terminal
against the current kubeconfig, after `make install`. It regenerates the CRDs
and RBAC first, so it always runs the code in your working tree. It runs
with your kubeconfig's credentials rather than its own ServiceAccount, so
RBAC mistakes do not show up, and it cannot reach the engines it runs: it
addresses each by its pod's DNS name inside the cluster, so every Company
reports `ConfigApplied=False` with the reason `EngineUnavailable`. Stop the
in-cluster manager first (`make undeploy`), or the two compete.

## Two Go modules

The repository holds two modules:

| Module | Path | Contains |
|---|---|---|
| `github.com/crewlet/crewlet-operator` | `/` | the manager (`cmd/`), the controllers (`internal/`), the e2e suite (`test/`) |
| `github.com/crewlet/crewlet-operator/api` | `api/` | the `crewlet.ai` API types, their markers and generated deepcopy |

**Why the API is a module of its own:** consumers such as a control plane that
provisions tenants import the types to write these objects, and they should
get exactly the types — not controller-runtime, not the operator's dependency
tree, and not a version bump every time the operator's own dependencies move.
The API module therefore depends on `k8s.io/api` and `k8s.io/apimachinery`
only, and registers its types with apimachinery's scheme builder. A depguard
rule in [`.golangci.yml`](.golangci.yml) refuses controller-runtime and the
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

- `make test-e2e` creates a kind cluster, builds the manager image, pulls the
  Crewlet engine image, loads both, deploys the manager with kustomize, and
  checks it from the outside: that it runs and serves metrics, and that a
  company runs end to end — its engine becomes ready and is given its
  document and Secret values, a consumer its class admits reads the API with
  the published token, is refused a change to the document the operator
  enforces, and a pod of another namespace is refused, an edited Seat
  becomes a new revision, and deleting the Company keeps or deletes its
  volume and keyring as its `deletionPolicy` says; and that a company's seat
  pools run, the engine places each seat in its own pool, and the data pod's
  leaf listener admits the company's own seat pods and no other pod; and
  that the cluster's StatefulSet controller replaces a pod not ready on an
  older template as it rolls a StatefulSet that rolls as a seat StatefulSet
  does, which the operator leaves to it. Then it deletes the cluster, unless
  the suite failed: the cluster is left for a look, and
  `make cleanup-test-e2e` deletes it. CI runs it on every pull request and
  every push to `main`. The engine image is `CREWLET_IMG`,
  Crewlet's `main` image unless you name another, which `make crewlet-image`
  pulls for this machine's platform, printing the Crewlet commit it was
  built from; another image Docker already has is run as it is. So an
  engine built from a Crewlet checkout runs too: in it, `make image-context
  VERSION=main-dev`, then `docker buildx build --platform linux/$(go env
  GOARCH) --load --label org.opencontainers.image.revision="$(git rev-parse
  HEAD)" -t crewlet:dev -f Dockerfile build/image`, and here `make test-e2e
  CREWLET_IMG=crewlet:dev`. The suite loads the engine image, and the
  manager image, into kind as an archive of this platform alone
  (`test/utils`, with `docker save --platform`, which is Docker 28's),
  because `kind load docker-image` imports every platform of a pulled image
  and fails, on Docker's containerd image store, on the platforms that were
  never pulled. On a host that cannot run kind's default cluster,
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
engine's encoding of it reads back as no difference. It also runs the engine
as a data pod, with variables in its environment, and holds what the
[tool-servers guide](docs/guides/tool-servers.md#stdio-and-http) and the
`MCPServer` API say a stdio tool server is handed of it to what the engine
starts one with. The CRD suite in
[`test/crd`](test/crd) holds the `LLMProvider` rules over a provider's
sign-in, and the sign-in table of
[`docs/guides/cli-agents.md`](docs/guides/cli-agents.md), to the same binary:
it reads each CLI's token and key variable from the engine's own refusals,
tries every CLI the API offers in every sign-in shape, each driving a seat,
and compares the API server's verdict with what `crewlet validate` admits
and doubts and what `crewlet llm list` reports the CLI signed in with, so an
engine whose profiles move a token or key variable fails it. It holds the
rule over an OpenCode provider's `model` the same way, trying every CLI with
models with and without a provider, and with segments of white space, so an
engine that marks another profile as reading `<provider>/<model>` fails it
too, as does one whose refusal of such a model quotes another error of
OpenCode's, or another command listing its providers, than the rule's
message and the guide quote. It reads the variables the engine sets to keep
each CLI inside a seat's own home from its refusal of `HOME` in a provider's
`env`, which names every one for that CLI, and tries each on every CLI, so an
engine that gives a profile another such variable, or drops one, fails the
rule over `env`, the list its message states and the guide's table of them.
A mock would agree with the code by
construction; the real engine is what tells the two apart when Crewlet's API
or schema moves.

What no validation shows is what a seat's executor launch runs and signs its
CLI in with, which the renderer's verdict on a seat's executor turns on. The
renderer's `TestExecutorSignInFollowsTheEngineSource` and
`TestRunnerCLIsFollowTheEngineSource` read it from the engine's own source
(`enginetest.Source`, the module `make crewlet` installed from): the
variables that sign Claude Code in from a run's environment, Claude Code's
token and key variables among them, and the CLIs Crewlet has an in-box
runner for, which test/crd's `TestCLIAgentRunInFollowsTheEngine` holds to
the engine's verdict on a provider's `run_in` too. The rules around those
lists decide only at a launch, which no engine test reaches, so
`TestMirroredEngineDeclarationsAreUnchanged` holds their code:
[`internal/render/mirror_test.go`](internal/render/mirror_test.go) names every
engine function whose decision the operator copies at a launch —
`LaunchExecutor` and the `launch` it makes past the turn's fence, the
`member` it picks a run's provider by among
`executorRunners`, the `agentPlacement` that places it and
`Placement.OnEngineHost`, `entryEnv` and `entrySignInName`, `sandboxCredentials` and the
`entrySignsIn` it judges a sign-in by, what a direct box is made of on the
pod (`NewLocal`, `DefaultBoxRoot`, `localOptions`, `(*directBox).childEnv`
and `PassthroughEnv`), and where deep work is placed
(`(*Company).DeepWorkPlacement`), in Crewlet's `internal/engine`,
`internal/sandbox`, `internal/hostbox` and `internal/config`, and the two start-time
decisions the renderer holds a data pod back on: whether the address the
engine is told is on loopback (`externalLoopback` in
`internal/config/api.go`, which `TestContractSignInPosture` also holds at the
addresses it tries), and whether an address is one the engine refuses to
start on at all (`checkExternalURL`, `checkPort`, `checkBrowserHost`,
`endsInNumber`, `checkOrigin` and their callers in `internal/config/api.go`,
`hasHTTPScheme` in `internal/config/integrations.go`, and the trim of every
Tier A string in `internal/config/normalize.go`, which
`TestContractAddressShapes` holds the render to and test/crd's
`TestAddressShapesFollowTheEngine` the API's rules on a Company's and a
class's addresses, at the shapes they try) — with
every operator file that copies it and a digest of the function as it was
read. Those files are the renderer's; for the addresses, also the Company
and EngineClass API types whose rules refuse them, the controller's message
on a Company held back for one, and the [exposure](docs/guides/exposure.md)
and [troubleshooting](docs/guides/troubleshooting.md) guides that state
them; and, for how an executor run's CLI signs in where it runs, also the
LLMProvider API's rule on `runIn`, the [CLI agents guide](docs/guides/cli-agents.md)
that states it and `test/crd`'s reading of the engine's verdicts on it, and,
for what a coding box carries of its provider, the
[credentials guide](docs/guides/credentials.md#what-a-coding-box-carries)
and the [credentials concept page](docs/concepts/credentials.md). For a
value a tool server or an executor run may go without, the table names what
the engine starts each with (`resolveServer` and `(*refGap).hold` in
`internal/engine/toolcredentials.go`, `(*Engine).runEnvironment` in
`internal/engine/sandbox.go`), which instances of a server it starts and
with whose values (`startSharedServers` and `seatSpecs` in
`internal/engine/mcp.go`, `sandboxMCP` in `internal/engine/sandbox.go`,
`(*MCPServer).Grants` in `internal/config/mcp.go`,
`(*Organization).inheritMCPEnv` in `internal/org/organization.go`), and
the spellings of a header it refuses (`validateHeaderSpellings` and
`spelledUnlike` in `internal/config/filerules.go`); those are read against
the controller's report of such a value as well
(`internal/controller/config_optional.go`), the MCPServer, Seat and
Company API types, and the [tool servers](docs/guides/tool-servers.md),
[CLI agents](docs/guides/cli-agents.md) and credentials guides. The table
also names every constant and function that bounds a wait on a data pod's or
a seat pod's boot, which the controller's startup windows add up
(`dataPodBootSeconds` and `seatPodBootSeconds` in `internal/controller`), the
EngineClass API floors `data.startupSeconds` at, and the runtime and
troubleshooting pages quote, and one step the windows leave out, past the
recovery boundary: the boot's read of the company's secrets (`bootSecrets`),
which waits out a store that does not answer. Which steps a boot runs, and in
what order, is `engine.New`'s, which is not read, since it changes with most
of the engine's work. The digest (`enginetest.Fingerprint`) is taken over the
function, or over the one spec that declares a constant, as `go/printer`
lays it out without its comments or the source's line breaks, so a comment
or a rewrapped line leaves it alone and a changed statement, value, spread
or alias moves it. When the engine the suites run changes one, the test
fails, naming the declaration, what the operator copies from it, the files
to read it against and the `git log` that shows the change; read it, make
the operator follow it, and record the digest and version the failure
prints. When the engine removes or renames one, the test says so: name the
declaration that decides it now in its place, with any digest at first, and
the next run prints the digest to record. Of the renderer's executor
verdicts, `NotExposed` and `SandboxLoginNotPortable`
mirror what the engine refuses only when a seat launches its executor, never
on a write, so the operator reports them on the Seat's `Ready` and applies
the seat, as the engine does: drift there leaves a stale condition, not a held
company. `NoExecutorCLI`, `SandboxRequired` and `SandboxPlacementNotOffered`
mirror refusals on every write and block the document. Three tests hold the
split to the engine: `TestContractExecutorIsAppliedWhereItCannotRun` has it
accept every document the renderer only reports on, so an engine that starts
refusing one on a write fails it; `TestContractEngineRefusesWhatItHolds`,
through `engineCases` in `internal/render/basis_test.go`, has it refuse each
of the three the renderer refuses, at the document path its case names, so
an engine that stops refusing one fails it; and
`TestContractExecutorRefusalsReachTheirObject` has its dry run refuse what
the renderer leaves to it, a setup file at an absolute path, which a
`Direct` box writes nowhere, and checks that the refusal is reported on the
object that holds the setting.

### When a finding holds the document

A company's document is applied whole, so a finding that holds it holds every
object of the company on the last revision.
[ADR-0005](adr/0005-hold-a-document-only-for-what-cannot-be-applied-safely.md)
allows that for three reasons only: the engine cannot take the object
as it is (`EngineRefuses`), applying it would harm more than the object at
fault (`HarmsBeyond`), or the class forbids it (`ClassPolicy`). Anything else
is applied and reported (`Reported`). So when you add a finding to
`internal/render`:

- Pick its basis where you record it: `refErrors.add` and `notFound` take a
  `render.Basis` first, and `TestEveryFindingHasABasis` fails until the
  table in `internal/render/basis_test.go` classifies the new place, with
  why. One reason can have a different basis in different places. State it
  in `docs/concepts/company-document.md` too, in the table of "What blocks a
  document" or the list of what is applied and reported instead:
  `TestTheDocsStateTheBases` fails until the page agrees with the table.
- Make sure what is applied for a `Reported` finding cannot reach past its
  object later. A seat in a pool the Company does not declare is pinned to a
  label of its own, never the pool's, because the pool's pods, once declared,
  would run it under a revision nobody checked against them.
- For `EngineRefuses`, add a case to `TestContractEngineRefusesWhatItHolds`
  that writes the object into the document as its spec says and has the
  engine refuse it there; it fails until every place of that basis has
  one. For `Reported`, add a case to `reportedCases` in
  `internal/render/refs_test.go`, which `TestContractReportedIsApplied` hands
  to the engine to accept.
- Report a finding that holds nothing on the object at fault, and on each
  object that cannot do its job because of it: `ResolvedRefs=False` when a
  reference names what is not there, `Ready=False` when the object cannot do
  its job at all, and a condition of its own when only part of it fails, with
  a Warning event when it turns so.

The verdict on a seat's executor, which the engine applies and refuses only
at a launch, is a statement about the spec beside the render (`ExecutorOf`),
never a finding. It mirrors which provider a launch runs on, where it places
the run, and whether the box reaches the seat's tools and signs its CLI in
there; `TestExecutorFollowsTheEngineSource` holds the variable the operator
sets to the engine's, `TestContractExecutorIsAppliedWhereItCannotRun` holds
the documents to the engine, and `TestMirroredEngineDeclarationsAreUnchanged`
holds the code that launches a seat's executor and refuses it (`LaunchExecutor`,
`launch`, `member`, `executorRunners` and `agentPlacement` in Crewlet's
`internal/engine/agentrun.go`, `Build` in `internal/api/mcpbridge/build.go`)
to the code `internal/render/executor.go`, `internal/render/signin.go` and
the head of a seat's executor chain in
`internal/render/seatpod_providers.go` were read against. It holds
which engine pod's boxes dial their tool bridge on loopback
(`boxHostURL` and `loopbackURL` in Crewlet's `cmd/crewlet/listeners.go`,
`(*Bridge).Reaches` in `internal/api/mcpbridge/mcpbridge.go`) to
`internal/controller/company_workload.go`, which gives every engine pod the
Company's public address as `CREWLET_MCP_BRIDGE_URL` for the runs placed in
E2B and Ask Crewlet's deep work alone, to
`internal/render/bootstrap.go`, which binds a seat pod's `api.port` on every
interface so that its own `Direct` boxes reach it on loopback, to
`internal/controller/company_exposure.go`, which under `Isolated` lets the
data pods alone reach the public route's proxies that a deep-work call lands
on, and to `internal/render/executor.go`, whose verdict reports a `Direct`
seat as runnable in any pool, with or without that address.

The engine is the one thing in the Makefile that is not pinned. Every suite
runs Crewlet's `main`, as Crewlet publishes it at
`ghcr.io/crewlet/crewlet:main` (`CREWLET_MAIN_IMG`, which `CREWLET_IMG`
defaults to), because the operator is
written against what Crewlet ships next and is told the day Crewlet breaks it
([ADR-0002](adr/0002-test-against-crewlet-main-and-fail-without-it.md)).
`make crewlet` (which `make test` runs first) reads the commit that image was
built from out of its `org.opencontainers.image.revision` label, with
`test/enginecommit`, and installs the Crewlet module at that commit into
`bin/crewlet`, printing both: the e2e suite runs the image, and every other
suite the same commit. It never installs the module proxy's `@main`, which
runs ahead of the image: Crewlet builds the image minutes after a merge, and
not at all for a merge that starts no workflow or whose build fails. To run
another engine, such as the one a failed run's log names, set
`CREWLET_VERSION` to its commit or module version:
`make test CREWLET_VERSION=a20ed7519ba7`. A branch of Crewlet's repository
works too, which is how a Crewlet change is tried against the operator
before it merges (and `CREWLET_IMG`, above, for the e2e suite). To run these
tests outside make — from an IDE, or with a plain `go test ./internal/render`
or `go test ./test/crd` — install it once with `make crewlet`, or point
`CREWLET_BIN` at a `crewlet` binary of your own. A test that finds no binary
**fails** and says which of the two to do; it never skips, because a skipped
contract test reads as a passing one. A test that reads the engine's source
(`enginetest.Source`) fails too on a binary built from a Crewlet checkout
with uncommitted changes: the go command stamps it with the commit's version
and `+dirty`, and the module proxy serves that version without the changes,
so the test would read code the binary does not run. When a test fails, the
engine's log is printed with it.

So the operator's CI can fail with no change to the operator: a Crewlet
change broke it, or exposed something the operator had wrong. That shows on
the operator's next pull request or push to `main`, and the failed job's log
names the Crewlet commit it ran.

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
  because the API cannot tell the two apart from the value; the one exception
  is a tool server's `env` and `headers` and a Unit's or Seat's `spec.mcp`,
  where a setting is told apart by its name: a literal `value` is refused
  under any name Crewlet reads as a credential's (`credentialNamePattern` in
  `internal/render/signin.go`, which tests hold to the engine's
  `cliprofile.IsCredentialName` and to the names it reads a seat's tool
  credentials under), because a seat needs a setting of its own there, such as
  a cache path. No field accepts a literal secret, and the pass-through
  `engineConfig` may not name a block that holds credentials. [docs/concepts/credentials.md](docs/concepts/credentials.md)
  maps every Crewlet credential position to its field, from the table in
  [`internal/render/credentials_table_test.go`](internal/render/credentials_table_test.go);
  a test holds that table to the positions the engine's schemas mark
  `x-crewlet-secret`, in both directions, and the page to the table, so a
  credential Crewlet adds fails the build until it has a field or a reason
  it has none. Another holds the `engineConfig` rule to the same marks: a
  credential Crewlet adds under a new top-level block fails it until the rule
  refuses that block. `TestCredentialsDocListsThePositions` prints the rows
  the page must hold.
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
an update. It also applies every sample in `config/samples`. A rule that
has to agree with Crewlet on an address states the shape itself rather than
leaning on `isURL()` and `url()`, which are the API server's own Go
`net/url` as it was built and is run: as Kubernetes builds it, it reads a
`]` after an IPv6 zone into the zone, and run with
`GODEBUG=urlstrictcolons=0` it takes a second `:` after a host. The suite
offers the addresses the engine is handed to a second API server run with
that setting as well, and holds both to the engine
(`TestAddressShapesFollowTheEngine`, `TestS3SettingsFollowTheEngine`), so
a rule that leaned on `isURL()` there fails. The suite in
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
| `v1alpha1` (now) | Anything, including renaming or removing fields and kinds, with no conversion, alias or deprecation. A surface no tag has shipped breaks without ceremony; once a tag ships it, a break carries a `!` and a `BREAKING CHANGE:` footer (see [Commit messages](#commit-messages)). |
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
| the Crewlet engine (`CREWLET_MAIN_IMG`) | `Makefile` | not a pin: Crewlet's `main` image, whose commit `make crewlet` reads at every run ([The engine suite](#the-engine-suite)); a release keeps the one it was tested against ([RELEASING.md](RELEASING.md)) |
| `ENVTEST_K8S_VERSION` | `Makefile` | `bin/setup-envtest list`: the newest release of the supported Kubernetes minor |
| golangci-lint and its plugins | `.custom-gcl.yml` (version must equal `GOLANGCI_LINT_VERSION`) | the Go module proxy |
| the curl image | `test/e2e/e2e_test.go` | Docker Hub |
| the Gateway API CRDs the controller suite installs | `internal/controller/testdata/gateway-api` | `make gateway-api-crds`, after Dependabot bumps `sigs.k8s.io/gateway-api`; the suite fails until the two match |
| cosign (`cosign-release`) | `.github/workflows/release.yml` | sigstore/cosign's releases |
| Claude Code (`CLAUDE_CODE_VERSION`) | the Dockerfile in `docs/guides/cli-agents.md` | `https://downloads.claude.ai/claude-code-releases/latest`, the release Anthropic's installer installs |
| OpenCode (`OPENCODE_VERSION`, `OPENCODE_INTEGRITY_*`) | the Dockerfile in `docs/guides/cli-agents.md` | `https://registry.npmjs.org/@opencode/cli/latest` (never the `beta` or `dev` tag), within the major Crewlet's opencode profile is written for (`written_for` in its `internal/providers/llm/cliagent/cliprofile/profiles.yaml`, OpenCode 2 today); the integrities each the `dist.integrity` of `@opencode/cli-linux-x64-baseline` and `@opencode/cli-linux-arm64` at that version |
| uv (`UV_IMAGE`), Python (`PYTHON_VERSION`), `mcp-server-mattermost` and `mcp-atlassian` (`MCP_*_VERSION`), and `RESOLVED_AT` | the Dockerfile in `docs/guides/tool-servers.md` | `ghcr.io/astral-sh/uv`, by digest; `https://pypi.org/pypi/<package>/json`; the newest Python of the minor version the comment at the pin holds it to, which the pinned uv must offer; `RESOLVED_AT` the day the versions were read |
| glab (`GLAB_VERSION`, `GLAB_SHA256_*`) | the Dockerfile in `docs/guides/tool-servers.md` | `https://gitlab.com/api/v4/projects/gitlab-org%2Fcli/releases/permalink/latest`, the digests from the release's `checksums.txt` |
| slack-mcp-server (`SLACK_MCP_SERVER_VERSION`, `SLACK_MCP_SERVER_INTEGRITY_*`) | the Dockerfile in `docs/guides/tool-servers.md` | `https://registry.npmjs.org/slack-mcp-server/latest`, the integrities each platform package's `dist.integrity` |
| the supported Kubernetes minor (1.37) | `README.md` (its badge and Requirements), `docs/guides/installing.md` and `CLAUDE.md`'s Versions; it is also the minor of `ENVTEST_K8S_VERSION`, of the pinned kind's default node image and of `kubernetesVersion` in `hack/crd-ref-docs/config.yaml` | the newest minor that both the pinned kind (its default node image) and `bin/setup-envtest list` offer, moved to once both do; the minor alone is supported, so code, tests and docs kept only for an older one go with the move. A release names the minor it was tested against in its notes ([RELEASING.md](RELEASING.md)) |

No CI job builds the Dockerfiles in `docs/guides/`: after moving one of their
pins, build it from the engine image, and run the guide's checks against what
you built: for the tool servers, which start each server as the engine's pod
does and ask it for its tools over MCP, and for OpenCode, its `auth list
--standalone`, which starts OpenCode's private server and loads every native
library it unpacks, on mounts that allow exec as the pod's do, and reads
OpenCode's log for a library that did not load, since OpenCode carries on
without one. Run OpenCode's on a machine of each platform the image is built
for: under emulation it misses a deadline of OpenCode's own and fails.

### A Dependabot bump merges itself

[`.github/workflows/dependabot-merge.yml`](.github/workflows/dependabot-merge.yml)
approves a pull request Dependabot opened and queues it with `gh pr merge
--auto --squash`, so a bump lands without waiting on a maintainer. It does not
land without review: CI *is* the review a dependency change gets here, and
`--auto` holds the merge until every check `main`'s protection rule requires
has passed. A bump CI rejects sits there red until a person looks at it, which
is exactly where it would have sat anyway. It is the same workflow
[Crewlet](https://github.com/crewlet/crewlet) runs.

Three settings hold that up, and none of them is in this repository's files:

- `main`'s protection rule must **require** the checks [`ci.yml`](.github/workflows/ci.yml)
  runs on a pull request: `lint`, `test (envtest)`, `verify (generated files,
  go.mod)`, `build (binary, image)`, `sign-off` and `e2e (kind)`. `--auto`
  waits for the checks a rule names and for nothing else, so with no rule a
  bump merges the instant it is mergeable. A rule matches a check by its exact
  name, so renaming a job is a change to the rule too.
- **Allow auto-merge** must be on (Settings → General), or the merge step
  fails.
- **Allow GitHub Actions to create and approve pull requests** must be on
  (Settings → Actions → General), or the approval step fails.

The job runs only when Dependabot is both the pull request's author *and* the
actor that triggered the run: anyone with write access can push a commit onto
a Dependabot branch, and the workflow must not hand that commit an approval.
So do not push onto a Dependabot branch. A bump that needs a code change goes
into a pull request of your own, with the change; Dependabot closes its own
once `main` has the version. Left to itself, Dependabot rebases a bump that
falls into conflict, which is why this repository does without Crewlet's
`dependabot-recreate.yml`. If you clicked **Update branch** on a bump,
Dependabot will not rebase it again and, under a rule that dismisses stale
approvals, it is left unapproved: comment `@dependabot recreate` and Dependabot
rebuilds it as its own.

Nothing checks the workflow for you, and its job holds `contents: write` and
`pull-requests: write`: read its `if:`, the approval's condition and the merge
command on any diff that touches it.

### A newer Kubebuilder scaffold is proposed

[`.github/workflows/auto_update.yml`](.github/workflows/auto_update.yml) runs
every Tuesday, and by hand from the Actions tab. When Kubebuilder has a
release newer than `PROJECT`'s `cliVersion`, it runs `kubebuilder alpha
update`: the scaffold of both releases is regenerated from `PROJECT`, this
repository is merged into the newer one, and the result is pushed as one
commit on the branch `kubebuilder-update-from-<from>-to-<to>`, with an issue
whose link opens a pull request from it. Kubebuilder's `autoupdate/v1-alpha`
plugin wrote the workflow (`bin/kubebuilder edit
--plugins=autoupdate/v1-alpha`, which also lists the plugin in `PROJECT`), and
it is curated by hand since: running that command again puts the plugin's
template back.

The branch is where an update starts, not a change to merge as it is. In the
pull request from it, whoever takes the update over:

- resolves the conflicts. The merge is committed with its markers (`--force`)
  rather than stopping the run, and this repository's files differ enough
  from the scaffold's that some are to be expected.
- deletes again what the merge brings back of the scaffold that this
  repository does without, the scaffold's own workflows beside `ci.yml` among
  them (`make chart` deletes `test-chart.yml` for the same reason).
- moves `KUBEBUILDER_VERSION` in the Makefile to the release `PROJECT` now
  names, so `make chart` and new kinds are scaffolded by it, then runs `make
  manifests generate chart api-docs` and `make check`.
- signs the update's commit off: the workflow's commit carries no
  `Signed-off-by`, and the sign-off gate fails the pull request until someone
  certifies it (`git rebase --signoff <merge-base>`, the remedy the gate
  prints).
- titles the pull request like any other, since the title is the release note.

While that branch is still there, a later run for the same two releases fails
at the push rather than overwrite it, so delete the branch once its pull
request is merged or closed. The job's token can push to any branch and open
issues; `main`'s protection rule is what keeps its push off `main`.

## Documentation

Docs are part of the change. A pull request that changes an API field,
behaviour, a make target or the deployment updates the page under
[`docs/`](docs/README.md) that describes it, and regenerates
`docs/reference/api.md` (`make api-docs`) when the types changed. A field's
documentation is written once, as the doc comment on the field; the API
reference is generated from it.

Comments in code explain **why**, and especially why the obvious alternative
is wrong; the diff already shows what the code does. Diagrams are Mermaid.

### How the docs are published

`docs/` is published twice: as it reads on GitHub, and on the documentation
site at
[docs.crewlet.ai/operator/latest/](https://docs.crewlet.ai/operator/latest/),
beside the engine's documentation. The site renders the files as they are —
write plain Markdown that reads correctly on GitHub, with relative links — and
a few things follow from that:

- **`docs/README.md` is the site's navigation and its front page.** Each `## `
  section of it is a sidebar group, and each entry is one line, unindented,
  `- **[Title](page.md)** — description`, with an em dash between the two: the
  site lists the entries in the sidebar, in order, and on the index page draws
  each section's list as a grid of cards, but only when every item of the list
  has exactly that shape (a colon or a hyphen for the dash, an item that is
  not an entry, or a blank line between items or under one, and the whole list
  stays plain, silently). Three more mistakes pass without a word: a link in a
  description cannot be clicked, since a card lays its title's link over its
  whole face; the site's navigation reads a description to the end of its
  first line only; and it does not read an entry indented under another at
  all. `test/docs` refuses each of them. Write the title as the page's H1 says
  it, in Title Case, and the description on one line with no full stop. A page
  under `docs/` that no entry lists has no way to be reached, so the site
  refuses to build it; add the entry in the same change as the page, under a
  `## ` heading (an entry above the first one is in no group, and refused
  too). The site serves the index as the section's home, so there is no
  `docs/index.md` beside it. Its first paragraph is the operator's description
  on the site's front page and in search results: keep it a pitch about the
  operator, under 160 characters, with the alpha status in the callout after
  it, worded so it stays true in a released copy.
- **`docs/assets/` is published with every version**, less its `README.md`: an
  image the docs show is a committed SVG there, linked relatively
  (`assets/x.svg` from `docs/README.md`, `../assets/x.svg` from a page below
  it, `docs/assets/x.svg` from the root README, which is not published). An
  image anywhere else becomes a link to a GitHub page on the site, not an
  image. [`docs/assets/README.md`](docs/assets/README.md) says where each
  figure comes from and how the icons are drawn.
- **Asides are blockquotes with a bold lead-in**, `> **Lead-in.** …`, which the
  site draws as a callout titled with the lead-in, in a tone read from the
  lead-in alone: `never`, `irreversible` or `data loss` make a danger
  callout; `caution`, `warning`, `do not` or `avoid` a warning; `tip`, `prefer`
  or `recommended` a success; anything else a note. GitHub's `> [!NOTE]` and
  `:::note` are not used: each renders as literal text on one of the two.
- **Diagrams are Mermaid 11**, drawn ahead of time on the site and in the
  reader's browser on GitHub. The site's build fails on a diagram that does
  not parse, so check a new one where it renders before it lands. Colour only
  with the site's three semantic classes (`classDef ok stroke:#46a758`,
  `warning stroke:#f59e0b`, `danger stroke:#ef4444`); any other colour fails
  the site's build.
- **Link the engine's documentation by its GitHub URL at `main`**, as
  `https://github.com/crewlet/crewlet/blob/main/docs/<page>.md#<heading>`.
  That is what works on GitHub, and the site turns it into its own page of the
  engine's documentation for `main` (`/engine/next/`). Link a page, not a
  directory (`tree/main/docs/<dir>`): the site serves a directory only as its
  index page, and most of the engine's have none. The engine's `docs/index.md`
  lists each directory's pages under a heading, so
  `blob/main/docs/index.md#integrations` is the link for "the engine's
  integration pages".
- **Commit files, not symbolic links.** The site refuses a symbolic link
  anywhere under `docs/`, so that nothing in it can make the site's build read
  a file outside it.
- **Links outside `docs/`** (this file, `config/samples`, the chart) are links
  into this repository on the site, so a relative path is right for both.
- **Every version has its own path.** `main` is always `/operator/next/`, the
  newest release is `/operator/latest/`, and an older release line is
  `/operator/vX.Y/`. Until the first `v*` tag only `main` is built, every page
  carries a notice that there is no release yet, and `/operator/latest/…` and
  `/operator/` redirect to `/operator/next/…`, so a `latest` link written today
  names the release once there is one. Link the site at `/operator/latest/`;
  the URLs the site used before this scheme (`/operator/<page>/`) are not
  forwarded.

`test/docs` holds the tree to all of it in `make test`: every page of `docs/`
but `assets/README.md` is listed exactly once in the index, under a section,
as a card entry, and every file there is a regular one; every object the
README shows is one the getting-started guide applies; every relative link and image
names a file that exists, every `#anchor` names a heading its target renders
(by GitHub's slug rule, which the site's renderer shares), and every link into
the engine's repository names a file and heading of Crewlet's `main`, the
engine every suite runs, so a Crewlet change that renames a page fails the
next run. An
absolute `https://docs.crewlet.ai` link, in any text file of the module (the
chart's `NOTES.txt` and the Go included, not the Markdown alone), must name
what the site builds: under a product and a version, a page with its trailing
slash, and for `latest/` or `next/` a page this tree has, or under `/engine/`
a page, schema or skill file of Crewlet's `main`.

The site rebuilds when `.github/workflows/docs-publish.yml` asks it to: after
a push to `main` that touches `docs/`, after a release, or when run by hand
from `main`. It reads two secrets, `DOCS_REPO` and `DOCS_TOKEN`, that name the
site's renderer and carry a token allowed to dispatch to it; without them the
run fails, naming the missing one, and the site keeps the pages it last built.
The job runs in this repository only, so a fork's pushes skip it. They
are **environment secrets** of the `docs-publish` environment, whose
deployment branches are limited to `main`, never repository secrets: the only
permission that allows the dispatch also allows a push to the renderer, and a
repository secret is readable by a workflow on any branch anyone with write
access pushes.

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
