# Running Seats on a Coding CLI

Every model a seat runs on is a coding CLI the engine drives: Claude Code for
Anthropic's models, Codex for OpenAI's, OpenCode for open models on any
provider, and the other CLIs of the table below. An `LLMProvider` names the
CLI (`agent`), the model in that CLI's own grammar, and how it signs in: on a
subscription you already have, or on a metered API key.

It renders one entry of Crewlet's `providers.llm`; Crewlet's
[LLM backends](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md)
describes what the engine does with it. This guide covers what changes when
the engine runs in a pod: the image, the login, and the two modes a CLI runs
in.

## The image needs the CLI

The CLI is a program the engine starts, in the engine's own container, and
Crewlet's image ships none, so a company whose seats should do any work runs
on a class that names a derived image. It adds the CLI, with whatever runtime
it needs (Node.js for most of them; Claude Code and OpenCode need none, each
being one native binary). Build one from the engine image you run, named by
digest: the engine of the operator's release, or the copy of Crewlet's `main`
image that [Installing](installing.md#engine-image) makes in your registry.
Pin both the engine and the CLI.

A class names one engine image, so a company that runs two CLIs, or a CLI
and stdio tool servers, needs one image with all of them. Each Dockerfile
here and the one of [Building the image](tool-servers.md#building-the-image)
takes the image it extends as `ENGINE_IMAGE`: push the first image, then
build the next Dockerfile with its `ENGINE_IMAGE` naming it, rather than
pasting one Dockerfile into another
([One image with a coding CLI](tool-servers.md#one-image-with-a-coding-cli)).

The engine's root filesystem is read-only: a CLI writes under the home the
engine gives each seat and under `/tmp`, both writable, and the engine keeps
the socket each host-mode run reaches the seat's tools through in a directory
of its own under `/tmp` ([Host mode and box mode](#host-mode-and-box-mode)).
Each running CLI takes 200 to 450 MB, so size the class's `data.resources`
for `maxConcurrent` of them per provider; one now lives for a seat's whole
run, its tool calls included, so where seats wait on each other for a place,
raise `maxConcurrent` and the pod's memory together
([Host mode and box mode](#host-mode-and-box-mode)). An OpenCode call is at
the top of that range: measured on OpenCode 2.0.32, one call held about 440
MB at its peak, some 365 MB of it the private server every call starts, and
each call running beside it about 250 to 350 MB more, the binary's pages
being shared between them; a short `opencode api session.get` reading a
call's usage follows each. OpenCode also unpacks about 19 MB of native
libraries into each seat's home, under the `TMPDIR` the engine gives it
(`<home>/tmp`), beside a database of about 7 MB while a seat's calls run:
about 26 MB per OpenCode seat on the data volume, and on a seat pod's
scratch volume (`scratchSizeLimit`) for each seat the pod holds.

### Claude Code

Anthropic's installer runs in a build stage of its own and only the binary it
installed is kept:

```dockerfile
# The engine image you run, by digest, and the Claude Code release to add.
ARG ENGINE_IMAGE
ARG CLAUDE_CODE_VERSION=2.1.303

# The installer needs curl, which the engine image does not ship, and leaves a
# download cache, a launcher and the build user's ~/.claude.json behind, none
# of which a seat reads: only the binary leaves this stage.
FROM ${ENGINE_IMAGE} AS claude-code
ARG CLAUDE_CODE_VERSION
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl
USER crewlet
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN curl -fsSL https://claude.ai/install.sh | bash -s "${CLAUDE_CODE_VERSION}"

FROM ${ENGINE_IMAGE}
ARG CLAUDE_CODE_VERSION
COPY --from=claude-code --chown=0:0 \
    /home/crewlet/.local/share/claude/versions/${CLAUDE_CODE_VERSION} /usr/local/bin/claude
# The image is how the CLI is updated. The engine starts it with only an
# allowlist of its own environment, which DISABLE_UPDATES is not on, so an ENV
# here would not reach it: the switch goes in the managed settings every
# claude reads.
COPY <<'JSON' /etc/claude-code/managed-settings.json
{"env": {"DISABLE_UPDATES": "1"}}
JSON
```

The installer checks the binary's SHA-256 against Anthropic's release manifest
before it installs it, and a `CLAUDE_CODE_VERSION` that names no release fails
the build. Updates are off because, left on, Claude Code would download each
new release into each seat's home on the data volume, about 250 MB a copy that
nothing runs: the engine runs the `claude` on its `PATH`.

Build it from the engine image,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet@sha256:<digest> \
  -t <your registry>/crewlet-claude-code:<crewlet commit> .
```

and name it in the class, which every company of the class then runs:

```yaml
# EngineClass with-claude-code, excerpt
spec:
  engine:
    image: <your registry>/crewlet-claude-code@sha256:<digest>   # crewlet <commit>
```

### OpenCode

Crewlet drives OpenCode 2 only, npm's `@opencode/cli`. npm's `opencode-ai` is
OpenCode 1.x and rejects the `--standalone` every command of Crewlet's
profile carries, so on it every call fails. The package's install script
puts the binary of one of its platform packages in place, a self-contained
program that needs no Node.js. This Dockerfile takes that binary straight
from the platform package and checks it against the integrity npm's registry
records, as the Slack server's is ([Building the image](tool-servers.md#building-the-image)):

```dockerfile
# The engine image you run, by digest.
ARG ENGINE_IMAGE

# The platform package is fetched and checked here, with a curl the engine
# image does not ship: only the binary leaves this stage.
FROM ${ENGINE_IMAGE} AS opencode
ARG TARGETARCH
# The OpenCode 2 release, and each platform package's dist.integrity on npm's
# registry. amd64 takes the baseline build, which runs on any x86-64 node: the
# other needs AVX2, and the image runs on whichever node its pod lands on.
ARG OPENCODE_VERSION=2.0.32
ARG OPENCODE_INTEGRITY_AMD64=sha512-NrB4iglJb7vGZWJ6B0qVH1EMHhWBZ4ADsAG9W0xLanzgxJ6eLEEsGBvxRX0cm/KPag5E/N0yZGsDVDZYcnz/OQ==
ARG OPENCODE_INTEGRITY_ARM64=sha512-YR5O03irvmot/G0f8W49ehvhn5uqCFN3vf9JTsChvf47W6ldjKCNvsaCSXS2SP26dPOUMQBn7WqbKnyHzrNR5w==
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
WORKDIR /fetch
RUN case "${TARGETARCH}" in \
      amd64) package=cli-linux-x64-baseline integrity=${OPENCODE_INTEGRITY_AMD64} ;; \
      arm64) package=cli-linux-arm64 integrity=${OPENCODE_INTEGRITY_ARM64} ;; \
      *) echo "no OpenCode build for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo opencode.tgz "https://registry.npmjs.org/@opencode/${package}/-/${package}-${OPENCODE_VERSION}.tgz"; \
    test "sha512-$(openssl dgst -sha512 -binary opencode.tgz | base64 -w0)" = "${integrity}"; \
    tar -xzf opencode.tgz --strip-components=2 package/bin/opencode

FROM ${ENGINE_IMAGE}
COPY --from=opencode --chown=0:0 /fetch/opencode /usr/local/bin/opencode
```

Pin the newest `@opencode/cli` of npm's `latest` tag, never its `beta` or
`dev`, within the major Crewlet's opencode profile is written for (OpenCode 2
today), with the `dist.integrity` of its `cli-linux-x64-baseline` and
`cli-linux-arm64` packages at that version. The obvious alternatives are
wrong:

- **OpenCode's own installer**, `curl https://opencode.ai/v2/install`,
  fetches the same package with no checksum, and picks the baseline or musl
  build from the CPU and libc of the machine running the build, not of the
  node the pod runs on.
- **No update switch** is needed, unlike Claude Code's. Every call the engine
  makes carries `update: disable` in the configuration it writes for that
  call, and the commands it runs outside a call (`auth import`, `auth
  export`, `auth list`, `--version`) check for no release. OpenCode's updater
  replaces only what a package manager (npm, pnpm, Bun, Yarn, Vite+,
  Homebrew or WinGet) or its own installer put in place, so it cannot touch
  `/usr/local/bin/opencode`, and the root filesystem is read-only anyway.
- **Never add an `/etc/opencode` configuration** to the image: OpenCode reads
  it as machine-wide managed configuration over every seat's call, and
  nothing the engine sets overrides it
  ([Isolation](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md#isolation-the-part-that-actually-matters)).
- **The binary links glibc**, which the engine image's Debian has; the
  `musl` packages are for Alpine. Of the native libraries it unpacks, only
  its file watcher's needs more, libstdc++ and libgcc_s, which Debian has
  too. The engine switches the watcher off on every command it runs
  (`OPENCODE_FILEWATCHER_DISABLE`), and the check below loads it anyway,
  since a provider's `env` can switch it back on.

Build it from the engine image, or from another image of
[One image with a coding CLI](tool-servers.md#one-image-with-a-coding-cli),

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet@sha256:<digest> \
  -t <your registry>/crewlet-opencode:<crewlet commit> .
```

check that OpenCode runs in it as the engine's pod runs it, as uid 10001 on
a read-only root, with an empty `HOME` and `/tmp` that, like the pod's,
allow running what is written there, no capabilities and no network,

```bash
image=<your registry>/crewlet-opencode:<crewlet commit>
docker run --rm --read-only --network none --user 10001:10001 --cap-drop ALL \
  --tmpfs /tmp:exec --tmpfs /home/crewlet:exec,uid=10001,gid=10001 -e HOME=/home/crewlet \
  --entrypoint /usr/local/bin/opencode "$image" auth list --standalone --print-logs 2>&1 |
  grep -E 'level=ERROR|No authenticated integrations'
```

which passes when it prints one line, `No authenticated integrations`. It
starts the private server on loopback that every call the engine makes
starts, and loads every native library the binary unpacks, where
`--version` alone loads one. A library that does not load is no failure to
OpenCode: it carries on without it, prints the same answer, exits 0, and
says so only in its log, as `level=ERROR` with `watcher backend not
supported`, which is why the check reads the log. That is what a missing
libstdc++ looks like, and what a plain `--tmpfs` does, which mounts
`noexec`: the pod's `/tmp` and home are `emptyDir` volumes, which allow
exec, and each seat's `TMPDIR` is on the data volume, which ordinarily does
too. Run the check on a machine of the platform the image is for: under
emulation, as `docker buildx` runs another platform's image, OpenCode
misses its own five-second deadline for activating its plugins and fails
with `Location plugins did not finish activating`. The binary adds about
210 MB to the image. Then name it in the class:

```yaml
# EngineClass with-opencode, excerpt
spec:
  engine:
    image: <your registry>/crewlet-opencode@sha256:<digest>   # crewlet <commit>
```

## Signing in

A subscription login is made at a terminal, by `crewlet llm login` or the
CLI's own command, and a pod has none. `auth` therefore takes the login
from a Secret, in one of two forms; takes a metered key instead; or, for the
three CLIs that read their model provider's key from their environment,
takes none and signs in with that key in `env`:

| Sign-in | What the Secret holds | Use it when |
|---|---|---|
| `auth.tokenSecretRef` | a long-lived headless token the CLI mints, such as the `CLAUDE_CODE_OAUTH_TOKEN` of `claude setup-token` | the CLI is Claude Code, the one CLI whose Crewlet profile names a token variable: it never needs refreshing, and it is the only one of Claude Code's subscription logins a box-mode run can carry (a box-mode OpenCode carries its model provider's key in `env` instead) |
| `auth.credentialBundleSecretRef` | a bundle of the CLI's login (its credential files, or for OpenCode the document `opencode auth export --standalone` prints), from `crewlet llm export <key>` on a machine where you logged in | any CLI but `QwenCode`, which keeps no login: the others mint no token Crewlet hands on |
| `auth.mode: APIKey` with `auth.apiKeySecretRef` | the CLI's metered API key | the CLI's Crewlet profile names a key variable ([below](#a-metered-key)) |
| `env`, with `auth.mode: Subscription` | the key of the model provider the CLI runs on, in the variable the CLI reads for it (`OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `NOUS_API_KEY`, ...) | the CLI is Hermes, Pi or OpenCode: with its model provider's key in its environment, it needs no login |

What each CLI takes, in the engine the operator is tested against, Crewlet's
`main` (Crewlet's profiles name the variables; a test holds the API's rules and this
table to that engine's own verdict on every CLI and every sign-in, so neither
can fall behind it):

| `agent` | `tokenSecretRef` | `auth.mode: APIKey` fills | A key in `env` | `credentialBundleSecretRef` | `mode: Box` |
|---|---|---|---|---|---|
| `ClaudeCode` | `CLAUDE_CODE_OAUTH_TOKEN` | `ANTHROPIC_API_KEY` | no | yes | on the token or the key |
| `Codex` | no | `CODEX_API_KEY` | no | yes | no |
| `GeminiCLI` | no | `GEMINI_API_KEY` | no | yes | no |
| `QwenCode` | no | `OPENAI_API_KEY`, with `--auth-type openai`: DashScope's key by default, or another OpenAI-compatible service's with its `OPENAI_BASE_URL` in `env` | no | no: it keeps no login, so `auth.mode: APIKey` is its only sign-in | no |
| `CursorAgent` | no | `CURSOR_API_KEY`, a Cursor account's API key | no | yes | no |
| `Copilot` | no | `COPILOT_GITHUB_TOKEN`, the first of the three variables it reads, ahead of `GH_TOKEN` and `GITHUB_TOKEN` | no | yes | no |
| `Grok` | no | `XAI_API_KEY` | no | yes | no |
| `MuseCode` | no | `META_API_KEY` | no | yes | no |
| `KimiCode` | no | no: it reads a key only from its own `config.toml`, so give that in the bundle | no | yes | no |
| `OpenCode` | no | no | yes | yes | on the key in `env` ([below](#host-mode-and-box-mode)) |
| `Hermes` | no | no | yes | yes | no |
| `Pi` | no | no | yes | yes | no |

A provider names one of the two logins: given both, which login the CLI acts
on would be its own choice between its environment and its files, so the
API server refuses the pair. Crewlet hands a token to a CLI only through the
variable its profile names, and the engine refuses a token for a CLI whose
profile names none, since it would reach nothing, so the API server
refuses `tokenSecretRef` on any agent but `ClaudeCode` first. A
Subscription provider that names neither is refused, with one exception:

- **Hermes, Pi and OpenCode on a key in `env`.** Each fronts many model
  providers and reads each provider's key from that provider's own
  variable, so Crewlet's profile for it names no key variable and marks it
  as signing in through its environment: Crewlet passes the provider's `env`
  to it untouched, which is the route its profile documents for a metered
  key. Crewlet counts an entry as a sign-in only by its name
  ([below](#which-env-entries-sign-a-cli-in)).
- **Every other CLI** keeps to a token, a bundle or a key. A CLI whose
  profile names a key variable takes its key with `auth.mode: APIKey`; the
  engine refuses that variable in `env` (below), and a key in `env` under any
  other name is one the CLI does not read.
- **QwenCode** keeps to a key alone. The builds of Qwen Code Crewlet supports
  refuse the OAuth login it once kept, so Crewlet's profile for it keeps no
  login and names no token variable: under `Subscription` it would sign in to
  nothing, a bundle included. The engine refuses that, and the API server
  refuses a QwenCode provider in any mode but `APIKey` first.

`env` may never set the CLI's own token or key variable, the ones the table
names (`ANTHROPIC_API_KEY` for `ClaudeCode`, `CURSOR_API_KEY` for
`CursorAgent`, and so on), in any auth mode: `auth` owns them, and whether a
value in `env` reached the CLI would turn on the mode and on what else holds
a credential, so the engine refuses the document and the API server refuses
the provider first. Another CLI's variable is no such conflict:
`ANTHROPIC_API_KEY` is OpenCode's Anthropic key.

Nor may `env` set a variable the engine sets on every call to keep the CLI
inside its seat's own home and the call's working directory: the home, the
XDG directories and the temporary directory of every CLI, and the CLI's own
configuration directory (and, for `Codex`, the directory of its state
database), or, for `OpenCode`, its configuration directory and
`OPENCODE_CONFIG_PROJECT_DISABLE`, the switch that keeps OpenCode from reading
project configuration from the directories above a seat's call, and, for
`ClaudeCode`, `CLAUDE_CODE_DISABLE_CLAUDE_MDS` and
`CLAUDE_CODE_DISABLE_AUTO_MEMORY`, the switches that keep every `CLAUDE.md`
above a seat's call, and Claude Code's auto-memory, out of it. `env` is
layered over them, so a value there would replace that isolation for every
seat on the provider: the engine refuses the document, and the API server
refuses the provider first. Another CLI's variable is no conflict here
either: `CODEX_HOME` means nothing to OpenCode. A test holds the API's rule,
its message and this table to the engine's lists:

| `env` may not set | For |
|---|---|
| `HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `TMPDIR` | every CLI |
| `CLAUDE_CONFIG_DIR`, `CLAUDE_CODE_DISABLE_CLAUDE_MDS`, `CLAUDE_CODE_DISABLE_AUTO_MEMORY` | `ClaudeCode` |
| `CODEX_HOME`, `CODEX_SQLITE_HOME` | `Codex` |
| `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG_PROJECT_DISABLE` | `OpenCode` |
| `CURSOR_CONFIG_DIR` | `CursorAgent` |
| `KIMI_CODE_HOME` | `KimiCode` |
| `HERMES_HOME` | `Hermes` |
| `PI_CODING_AGENT_DIR` | `Pi` |

Under `auth.mode: Subscription`, `env` may not set a variable the engine sets
there to sign the CLI in with the login its seat's home holds either:
`GeminiCLI`'s `GOOGLE_GENAI_USE_GCA`, which the engine sets to `true`, since
Gemini CLI picks its sign-in from its environment and a login alone selects
nothing. The engine sets it after `env`, so a value there would be replaced on
every call: it refuses the document, and the API server refuses the provider
first. Under `APIKey` the engine sets nothing there, and the variable is yours.

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: hermes
spec:
  companyRef: {name: acme}
  agent: Hermes
  model: anthropic/claude-sonnet-5   # an OpenRouter model id: OPENROUTER_API_KEY selects OpenRouter
  auth:
    mode: Subscription
  env:
    - name: OPENROUTER_API_KEY
      valueFrom:
        secretKeyRef: {name: openrouter, key: apiKey}
```

OpenCode reads its model as `<provider>/<model>`, splitting it at the first
slash: `anthropic/claude-sonnet-5` runs Anthropic's model on
`ANTHROPIC_API_KEY`, and `openrouter/anthropic/claude-sonnet-5` runs it
through OpenRouter on `OPENROUTER_API_KEY` (`opencode models --standalone`
lists the providers it knows). A model with no slash, or with nothing but
white space before or after it, names no provider or no model, and every call
the provider made would fail inside OpenCode, which OpenCode 2 refuses with
`Invalid model reference`, so the engine refuses it and the API server
refuses it first, at `spec.model`.

For Claude Code, on a headless token:

```bash
claude setup-token          # on your machine: prints the token
kubectl create secret generic claude-subscription -n acme --from-literal=oauthToken=...
```

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: claude-code
spec:
  companyRef: {name: acme}
  agent: ClaudeCode
  model: sonnet
  auth:
    tokenSecretRef: {name: claude-subscription, key: oauthToken}
```

The login, and the homes the CLI keeps for each seat, live on the data
volume, under `/var/lib/crewlet/llm-cli/<loginName>`, so a restarted data pod
keeps them; a seat pod keeps them on a scratch volume it loses at every start
(see [Seat pools](#seat-pools)). That matters for a bundle: the engine
restores one, when it starts and at every revision it applies, only while the
login there is empty, so the login the running engine refreshed wins over the
bundle in the Secret, whose refresh token the vendor may have retired since,
and a new bundle in the Secret reaches nothing until the provider moves to
another `loginName` ([Rotating a credential](credentials.md#rotating-a-credential)).
Nor does it restore one into a login `crewlet llm logout` emptied
([A seat on a coding CLI runs signed out](troubleshooting.md#a-seat-on-a-coding-cli-runs-signed-out)).
A bundle restored over a newer login is how a vendor that rotates refresh
tokens logs a deployment out, which is why a token is the better of the two
wherever the CLI mints one.

Providers that set the same `loginName` and drive the same CLI share one
login and its seats' homes, which is how several models run off one
subscription. Each CLI keeps its files in a directory of its own under the
login (`/var/lib/crewlet/llm-cli/<loginName>/<cli>`, such as `claude-code` or
`codex`), so a provider of another CLI with that `loginName` shares the
directory and nothing in it
([Per-phase models](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md#per-phase-models)).

**Moving a provider to another `agent`** is an ordinary change, applied
with no restart: the new CLI starts signed out in a login of its own beside
the old one's, which stays there for a move back. Sign the new CLI in. A
bundle belongs to the CLI it was exported from, so a provider's
`auth.credentialBundleSecretRef` still holds the old CLI's files after the
move, and the engine refuses to restore it into the new CLI's empty login,
logging `cli_agent_bundle_restore_failed`, until the Secret holds a bundle
exported from the new CLI
([An entry moved to another CLI](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md#an-entry-moved-to-another-cli)).

### An OpenCode login in a bundle

OpenCode 2 keeps its logins in a database of its own rather than in files,
so an OpenCode bundle holds one file: the document `opencode auth export
--standalone` printed. The engine keeps it as the login of record
(`opencode/credentials/exported-credentials` under
`/var/lib/crewlet/llm-cli/<loginName>`), imports it into an empty database in
a seat's home before the seat's calls, and exports it back when OpenCode
refreshed it
([Token refresh across seats](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md#token-refresh-across-seats)).

Make it on a machine with OpenCode 2, npm's `@opencode/cli`:
`crewlet llm login <key>` runs OpenCode's own `opencode auth login
--standalone` in a login home of the entry's own, or sign in with OpenCode
yourself and adopt that login with `crewlet llm login <key> -from-host`,
which reads it through your OpenCode's export
([Broker the vendor's own login](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md#1-broker-the-vendors-own-login-any-cli));
then `crewlet llm export <key>`. The export prints every login the OpenCode
it runs in holds, keys stored with `opencode auth login` included, so a
bundle adopted from your own home carries every provider you signed in to
there: adopt from a home holding only the login the provider should run on
(`-from-host -home <dir>`), or sign in with `crewlet llm login`, whose login
home holds the entry's login alone.

A login OpenCode 1.x made (`~/.local/share/opencode/auth.json`) is not read:
a bundle holding one is refused when the engine restores it, which it logs
as `cli_agent_bundle_restore_failed`, and the provider runs signed out, so
export it again from OpenCode 2. A bundle serves mode Host only: a
box-mode run never receives it ([Host mode and box mode](#host-mode-and-box-mode)).

### Which `env` entries sign a CLI in

This is the sign-in of a Hermes, Pi or OpenCode provider in
`auth.mode: Subscription` with neither a token nor a bundle: a key in its
`env`. Crewlet counts an entry as that sign-in only when its name says it
holds a credential: one of its parts between underscores is, in any case,
`KEY`, `KEYS`, `APIKEY`, `ACCESSKEY`, `SECRETKEY`, `TOKEN`, `SECRET`,
`PASSWORD`, `PASSWD`, `CREDENTIAL`, `CREDENTIALS`, `AUTH`, `OAUTH` or `PAT`.
So `OPENROUTER_API_KEY` and `GH_TOKEN` sign the CLI in, and `HTTPS_PROXY` or
`HERMES_MAX_TOKENS` (a count of model tokens) do not; the API server refuses
such a provider when its `env` holds no such name, since `crewlet llm list`
would report it signed in to nothing. Write its `auth.mode: Subscription`
explicitly, as `auth` may not be empty. A key in `env` is a `${NAME}` the
engine resolves from its encrypted store, so it never refreshes and runs on a
seat pod too.

> **Name only the sign-in you mean the CLI to use.** Beside a credential
> bundle a key in `env` is a second sign-in, which the CLI settles by its own
> rules rather than by the order you wrote them in. Hermes, for one, loads its
> home's `.env` over its environment, so a variable the bundle's `.env` also
> sets takes the file's value, while a model provider's key in `env` wins over
> an OAuth login in the bundle's `auth.json`, so Hermes runs on the metered key
> and the subscription goes unused.

### A metered key

`auth.mode: APIKey` gives the CLI the key of `auth.apiKeySecretRef` instead,
billing the metered account: Claude Code on an Anthropic API key, Codex on an
OpenAI one. Crewlet hands the key over in the variable the CLI's profile
names, and in Crewlet's `main` eight name one (the table above); for the
others the key would reach nothing, so the engine refuses the mode for them
and the API server refuses it first. Hermes, Pi and OpenCode take their
model provider's key in `env` instead, and KimiCode its key in the bundle.
`CursorAgent`'s key is a Cursor account's API key, from the Cursor
dashboard, not a model provider's.

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: claude-metered
spec:
  companyRef: {name: acme}
  agent: ClaudeCode
  model: claude-sonnet-5
  auth:
    mode: APIKey
    apiKeySecretRef: {name: anthropic, key: apiKey}
```

The key is one key: a CLI holds one sign-in and rotates nothing, so a second
key is a second provider, later in a seat's chain, which a seat falls back to
when the first is out of its limits. The API server refuses
`apiKeySecretRef` without `auth.mode: APIKey`, and the mode without it:
Crewlet would read the CLI's key variable from its own secret store and the
engine's environment then, which hold no key this provider names. A gateway
or proxy in front of the vendor is a variable the CLI reads from its
environment, which goes in `env` from a Secret, such as Claude Code's
`ANTHROPIC_BASE_URL`.

## Host mode and box mode

In either mode the CLI drives its own loop: it asks the model, calls the
seat's tools, which reach it as the MCP tools of one server, and asks again,
until the model stops or the engine ends the run. Every call goes through
the engine's gates on its way to the tool, so a seat's tools are the same in
both. What `mode` decides is where that loop runs and how long it lives.

`mode: Host` (the default) runs the CLI in the engine's pod that holds the
seat, a data pod or a seat pod alike, turn by turn, with the CLI's own shell
and editor denied, so the seat's tools are the only ones it has. The CLI
reaches them through `crewlet mcp-relay`, the engine's own binary, which it
starts as a stdio MCP server and which carries its calls over a unix socket
in a directory the engine makes for itself under the pod's `/tmp`, open to
the engine's user alone. That is the whole of it: no listener, no token and
no network, so a host-mode provider needs no public address and no network
policy of its own. It needs the CLI in the engine's image and a writable
`/tmp`, which every engine pod the operator runs has.

`mode: Box` runs a seat's executor as a detached run of the CLI in the
company's sandbox, with the CLI's own shell and editor, and the seat's tools
reached back over the engine's tool bridge; the turn waits until the run
reports. Everywhere else, in every other phase on the same provider (the
reviewer, a delegated worker, the summariser) and behind another provider at
the head of the executor's chain, the provider runs as in host mode, so the
CLI must be in the engine's image either way.

Two fields bound a provider's CLI in the engine's pod, and since a run of the
CLI now asks the model many times and calls the seat's tools in between,
neither is a bound on one request:

- `timeoutSeconds` bounds one stretch of the CLI's model work with no tool
  call in flight, from its launch or the answer to its last tool call to its
  next call or its end (600 seconds unset). A tool call stops the clock,
  however long a delegation or a slow tool server takes, and the next
  stretch starts afresh. A stretch past it ends the run with a timeout,
  which is retryable: a seat's chain moves on to its next provider.
- `maxConcurrent` bounds the provider's live CLI processes in each pod (4
  unset). One process lives for a whole run and holds its place through the
  run's tool calls, so it is also how many seats' runs the provider has going
  at once in the pod, and a run past it waits. A model call made on the same
  provider from inside a run's tool call, such as a delegated worker's,
  borrows that run's place: waiting for one of its own could leave every
  place held by runs that are each waiting on such a call.

A box-mode executor's run, in the sandbox, is bounded by neither.

```mermaid
flowchart LR
    subgraph pod["The data pod"]
        ENG["<b>Engine</b><br/><i>the seat's turn</i>"]
        HOST["<b>CLI, host mode</b><br/><i>its own loop, the seat's<br/>tools its only tools</i>"]
        RELAY["<b>crewlet mcp-relay</b><br/><i>a unix socket under /tmp</i>"]
        PUB["<b>:8081 public listener</b><br/>/mcp/ tool bridge"]
    end
    subgraph box["The company's E2B sandbox"]
        BOX["<b>CLI, box mode</b><br/><i>its own loop, shell<br/>and editor</i>"]
    end
    ENG -->|"starts a run"| HOST
    HOST -->|"the seat's tools, over MCP"| RELAY
    RELAY --> ENG
    ENG -->|"starts a detached run"| BOX
    BOX -->|"the seat's tools, with a token<br/>signed for the run"| PUB
    PUB --> ENG
```

In box mode:

- The run is a sandbox run: the Company needs `spec.sandbox`. With no
  `spec.sandbox.e2b.template`, each run gets E2B's prebuilt template for its
  CLI (`claude` for Claude Code, `opencode` for OpenCode); a template you
  set is used for every run, so it must carry the CLI of every coding agent
  and box-mode provider the company runs. For OpenCode that is OpenCode 2,
  the `@opencode/cli` package (`opencode --version` prints `opencode
  v2.…`): Crewlet's runner ends a run at once in a box that has OpenCode
  1.x, npm's `opencode-ai`, with `this box runs OpenCode 1.…; the opencode
  runner drives OpenCode 2 (the @opencode/cli package)` in the run's report.
  E2B does not say which OpenCode its prebuilt `opencode` template carries,
  so where runs end that way, set `spec.sandbox.e2b.template` to a template
  built with `@opencode/cli`, which, used for every run, must also carry
  Claude Code if any seat's code work runs it
  ([Crewlet's OpenCode runner](https://github.com/crewlet/crewlet/blob/main/docs/concepts/code-sandbox.md#opencode)).
  `runIn` is `E2B`, the only cell v1alpha1 offers: Crewlet's local cells
  would run every seat's code in the engine's pod, with its credentials.
- Claude Code and OpenCode run in box mode: they are the CLIs the engine
  has a sandbox runner for, the coding agents `spec.sandbox` offers, and the
  API server refuses `mode: Box` for any other. The run is handed `model`
  exactly as the provider's host-mode runs pass it, so one model serves the
  executor's run and every other phase.
- The login must follow the run into the remote sandbox, which only a
  variable of the run's environment can: a box-mode Claude Code takes
  `auth.tokenSecretRef` or `auth.mode: APIKey`, and a box-mode OpenCode
  its model provider's key in `env`, under a name the engine reads as a
  credential, which the engine exports into the box. A credential bundle
  stays on the data volume, since its login carries a refresh token the
  engine keeps rotating there, so the API server refuses a box-mode
  provider signed in with nothing else. A key in the Seat's
  `spec.sandbox.env` reaches the box as well, but the API server cannot see
  the Seat from the provider, so it does not count it.
- The run calls the seat's own tools back through the engine's tool bridge.
  While any LLMProvider of the company is in box mode, the operator gives
  the engine its public address as the bridge's (`CREWLET_MCP_BRIDGE_URL`),
  and the engine serves the bridge on its public listener, beside the
  webhooks ([Exposure](exposure.md)). A company with no public address
  cannot run one: the engine applies the provider and refuses every turn
  of a seat whose executor's chain it heads. The operator applies it too
  and reports it, so the rest of the company still runs: the LLMProvider
  reports `BoxModeRunnable=False`, `NotExposed`, and each such Seat
  `Ready=False`, `NotExposed`. With an address, `BoxModeRunnable` is
  `True`, `ToolBridgeServed`, naming the bridge's address.

```yaml
spec:                       # LLMProvider claude-box
  agent: ClaudeCode
  model: sonnet
  mode: Box
  loginName: claude-code        # the same login as the host-mode provider
  auth:
    tokenSecretRef: {name: claude-subscription, key: oauthToken}
```

or OpenCode, on its model provider's key:

```yaml
spec:                       # LLMProvider opencode-box
  agent: OpenCode
  model: openrouter/anthropic/claude-sonnet-5
  mode: Box
  auth:
    mode: Subscription
  env:
    - name: OPENROUTER_API_KEY
      valueFrom:
        secretKeyRef: {name: openrouter, key: apiKey}
```

A seat runs on it by naming it in `spec.llm.default`; see the Seat's
`spec.llm` in the [API reference](../reference/api.md).

## Code work

A seat with `spec.sandbox` runs code in the company's sandbox with a coding
agent, `ClaudeCode` or `OpenCode` (`spec.sandbox.codingAgent`, else the
Company's `spec.sandbox.defaultCodingAgent`, else `ClaudeCode`), on the
provider at the head of `spec.llm.sandbox`, else of `spec.llm.default`, else
of the Company's order. That provider is the run's model and sign-in, so it
has to drive the coding agent's own CLI. The Seat reports whether its code
work can run on its `CodeWorkRunnable` condition, saying what the run uses,
and naming the field to change when it cannot. Only the first case below
also holds the company's document (`Applied=False`), because Crewlet refuses
it on every write; Crewlet applies a seat in the second, which runs its
turns while Crewlet refuses each of its coding runs, so the operator applies
it too and the company is not held over one seat's code work. `CodeWorkRunnable` does not count towards
the Seat's `Ready`, and the operator records a Warning event on the Seat each
time the condition becomes `False` or its reason changes.

- **The provider must drive the coding agent's own CLI**: `ClaudeCode`
  code work runs on a `ClaudeCode` provider, `OpenCode` code work on an
  `OpenCode` one. A provider's model is written for its CLI and its sign-in
  is that CLI's, so Crewlet refuses any other pairing on every write
  (`CodingAgentMismatch`). Codex, Gemini CLI, Kimi Code, Hermes, Pi and the
  other CLIs have no coding agent, so they cannot back code work: put a
  provider of the seat's coding agent first in `spec.llm.sandbox`. An
  `OpenCode` coding run needs OpenCode 2 in the box, as a box-mode run
  does ([Host mode and box mode](#host-mode-and-box-mode)).
- **The login must reach the remote sandbox.** A credential bundle never
  does, so a `ClaudeCode` provider signed in by a bundle, with nothing in
  the seat's sandbox environment (`spec.sandbox.env`, its setup steps'
  `env`, or the Company's `spec.sandbox.setup` steps') that signs Claude Code
  in, or an `OpenCode` one with no credential-named variable in its `env` or
  the seat's sandbox environment, has each coding run refused
  (`SandboxLoginNotPortable`). A headless token, an `auth.apiKeySecretRef`
  key and an `OpenCode` provider's key in `env` all follow the run.

A seat whose sandbox environment signs Claude Code in itself, with
`CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`,
`CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX` or
`CLAUDE_CODE_USE_FOUNDRY`, runs its `ClaudeCode` code work on that sign-in
alone, with the provider's model: Crewlet hands the run none of the
provider's sign-in then, since Claude Code ranks its sign-ins by name, and a
provider's key beside the seat's plan token would win.

A seat whose executor runs in box mode has no code work of its own:
Crewlet never offers it `run_sandbox`, since it codes in its executor's run,
on that provider's CLI, so neither case above applies, its
`spec.llm.sandbox` is not read for one, and it has no `CodeWorkRunnable`.
Its `spec.sandbox.codingAgent` drives nothing: leave it out. Crewlet refuses
one that names another CLI than the executor's on every write, and the
operator refuses it too (`CodingAgentMismatch` on `Applied`, at
`spec.sandbox.codingAgent`).

### A value a run may go without

Crewlet starts a `run_sandbox` coding run only once every value of its
environment and files holds something: a value read from a Secret that does
not exist, holds no such key or holds an empty value has every run refused
before a box exists. A value the run can do without is listed by its key:
in `spec.sandbox.optionalKeys`, a variable of `spec.sandbox.env`; in a setup
step's `optionalKeys`, the Company's or the Seat's, a variable of the step's
`env` or a path of its `files`.

```yaml
spec:                       # Company nimbus
  sandbox:
    setup:
      - name: npm-registry
        files:
          - path: .npmrc
            valueFrom:
              secretKeyRef: {name: nimbus-npm, key: npmrc}
        optionalKeys: [.npmrc]
```

Under a listed key, such a value is left out: the variable is not set, or
the file not written, and the run starts without it. A missing Secret or key
there holds nothing back, and the object that gives the value names it in its
`ResolvedRefs=True` message, with the list that lets it go: the Seat for its
own `spec.sandbox` and steps, and the Company, which has no `ResolvedRefs`,
in its `SecretsSynced` message for its own steps. Once the Secret is back,
the operator pushes its value and applies the document again. When it is
deleted, the operator, under the `Enforce` policy, takes the value out of the
engine's secret store and has the engine read the store again, by applying
the next document or, while that one is held, by activating the revision the
engine runs again, so no run starts with a deleted Secret's value, but in a
revision the engine runs that reads the key as required, as one from before
the key was listed does, or whose reading the operator does not know, which
keeps the value until the next document runs, the new revision activating
once with it and then again without it
([Applying a document](../concepts/applying.md#what-one-reconcile-does)); a
Secret key also read where it is required holds the document instead, and
keeps its value in a revision the engine runs that requires it too until the
next one applies. The layers of a run's environment are laid in order, the
Company's steps, then the Seat's, then `spec.sandbox.env`, a later one
winning a variable, and the list beside the value that wins is the one that
counts for it; a step's files are its own. A provisioned value an
Integration mints for the seat is never left out, whatever the list says:
until it is minted, each run is refused, the seat told to try again. One
nothing mints, while no Integration of the company has
`provisioning: Engine`, is left out. A box-mode executor's run starts
without a value that holds nothing, listed or not (Crewlet's
[Credentials and the run environment](https://github.com/crewlet/crewlet/blob/main/docs/concepts/code-sandbox.md#credentials-and-the-run-environment)).

The API refuses an entry naming no variable or path of its own block. A
Secret key that holds an empty value under a key nothing lists does not hold
the document, since Crewlet applies the seat and refuses each run: the
object that gives the value reports `ResolvedRefs=False SecretValueEmpty`,
with a Warning event, or the Company's `SecretsSynced` for its own steps, and
the Seat's `CodeWorkRunnable=False SecretValueEmpty`, as does a provisioned
value nothing mints (`NoProvisioner`). Either is reported only where it is
the value a run is given: a step's variable the Seat's own `env` replaces is
none.

The verdict above reads the variables that sign the coding agent in once the
reconcile has read their Secrets: one a run goes without, listed and missing
or empty, signs nothing in, and the box is given the provider's sign-in as it
is with no variable of the seat's. What the box receives from the provider
is in
[What a coding box carries](credentials.md#what-a-coding-box-carries).

## Seat pools

A seat placed in a [seat pool](topology.md) runs on a seat pod, which keeps
nothing between starts and serves only its probes. A provider in host mode
runs there as it does on a data pod, its CLI reaching the seat's tools through
a socket under the seat pod's own `/tmp`. Two things a data pod does for a
CLI a seat pod cannot do:

- **Box mode.** A run calls the seat's tools back through the tool bridge
  of the very pod that holds the seat, on an address the sandbox reaches; the
  operator gives the data pod the company's public address for it. The
  engine would serve the bridge on a seat pod too, on the listener its
  probes use, but nothing publishes an address of a seat pod's own that a
  remote sandbox could dial: the company's public address reaches the data
  pod alone, and a seat pod is reached by its kubelet and nothing else.
  Without an address the engine refuses every box-mode turn on the pod.
  Crewlet runs a turn in box mode only when the HEAD of the executor's
  chain is in `mode: Box`, so a seat in a seat pool whose executor's
  chain (`spec.llm.default`, or the Company's first provider when the seat
  names none) starts with such a provider can run no turn. The engine
  applies it all the same, and so does the operator, which reports it on
  the Seat as `Ready=False`, `BoxModeBridgeUnsupported`, with a Warning
  event, rather than hold the rest of the company's changes: place the seat
  in `data`, or put a provider in `mode: Host` at the head of the chain. The
  same provider behind another in that chain, or in another phase's chain,
  runs in the seat pod as in host mode, and is fine.
- **A credential bundle.** The login lives under the CLI's state directory,
  which on a seat pod is its scratch volume, so every seat pod restores the
  bundle at each start and then refreshes the login on its own, beside the
  data pod. A vendor that rotates refresh tokens retires the token the other
  pods hold at each refresh, and logs them out. A seat in a seat pool that
  runs on a provider with `credentialBundleSecretRef`, in any phase, is
  refused with `Applied=False`, `CLIAuthNotPortable`, which holds the
  company's document: applied, it would log out every seat on that login,
  the data pod's included. Sign it in with
  `tokenSecretRef` (Claude Code), `auth.mode: APIKey` (a CLI the table
  above gives a key variable) or, for Hermes, Pi or OpenCode, its model
  provider's key in `env`, or place the seat in `data`.

## A provider for the Dashboard Assistant

A seat keeps its CLI's own web tools. The Company's Dashboard Assistant
(`spec.assistant`) does not: it reads only what the signed-in person's own
requests return, so Crewlet requires every provider in `spec.assistant.llm`
to run in mode `Host`, on a CLI Crewlet runs with no tool of its own (no
shell, no file reader, no tool server but the engine's own, which serves the
person's assistant tools), with its web tools off. Today that is
`ClaudeCode` or `Pi`: `Codex` keeps a shell its read-only sandbox only
confines, and `OpenCode`'s deny-all policy has not been measured to take its
tools away, so both are refused for the assistant though either may still
turn its web off for a seat. `webTools: Denied` turns them off, and renders as the
entry's `overrides: {web_tools: denied}`:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: assistant
spec:
  companyRef:
    name: nimbus
  agent: ClaudeCode
  model: sonnet
  timeoutSeconds: 90
  maxConcurrent: 4
  webTools: Denied
  auth:
    mode: APIKey
    apiKeySecretRef:
      name: anthropic
      key: apiKey
```

The API admits `webTools: Denied` only for a CLI Crewlet can turn the web off
for: `ClaudeCode`, `Codex`, `OpenCode` and `Pi`. Every other CLI's profile
carries no measured web denial, and Crewlet refuses the setting. A provider
of its own keeps the person in the drawer from queueing behind a seat's long
call, and its timeout is sized for one round rather than for a coding task.
Crewlet refuses an assistant chain naming a provider in mode `Box`, one that
keeps its web tools, or one on a CLI that keeps a tool of its own, in the dry
run before every apply, and the refusal is reported on the Company. Crewlet's
[configuration reference](https://github.com/crewlet/crewlet/blob/main/docs/getting-started/configuration.md)
describes the block and each bound.

### Deep work

`spec.assistant.deepWork` lets the assistant hand a large task (research,
scripts, files, many steps) to a coding CLI running its own tools: its shell,
its files and its web tools, which are the point of the escalation. The run
reaches Crewlet only through the signed-in person's own assistant tools, over
the tool bridge, so every read is still their request and every write still a
proposal they approve. Unset, the assistant answers in quick mode only.

Deep work runs on an `LLMProvider` driving `ClaudeCode` in mode `Box`, the
one CLI whose run Crewlet continues in its own session after the person
approves a proposal. It is a provider of its own, beside the assistant's
chain, which keeps mode `Host`:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: assistant-deep
spec:
  companyRef:
    name: nimbus
  agent: ClaudeCode
  model: sonnet
  mode: Box
  auth:
    mode: Subscription
    tokenSecretRef:
      name: claude
      key: token
```

```yaml
spec:
  assistant:
    enabled: true
    llm:
      - name: assistant
    deepWork:
      llm:
        name: assistant-deep
      timeoutSeconds: 1200   # 60 to 1200; one run lives inside the turn's read grant
      maxSteps: 150          # 10 to 400 agentic rounds and Crewlet calls
      maxTokens: 3000000     # 10000 to 20000000; the person's and the company's budgets still apply
  sandbox:
    e2b:
      apiKeySecretRef:
        name: e2b
        key: apiKey
```

**Where it runs.** Crewlet places a deep-work run in its provider's own
`run_in`, else in the first remote or container cell the company configures,
else on the engine host under operating system confinement. The operator
offers the remote cell alone, as it does for every box-mode run: the engine
pod holds the company's data volume and secret store, and a run steered by
whatever pages it read is not one to start beside them, confined or not. So
the API refuses `deepWork` on a Company without `spec.sandbox`, and every
deep-work run is placed in the Company's E2B sandbox. Its sign-in follows it
there as a box-mode executor's does: a headless token (`tokenSecretRef`)
or `auth.mode: APIKey`, never a credential bundle, which the LLMProvider API
already requires of a provider in mode `Box`. Like a box-mode executor,
it calls back through the Company's public address, so the provider's
`BoxModeRunnable` condition applies to it too.

The API refuses a platform model provider for deep work, since one runs in
mode `Host`. Crewlet refuses a provider in mode `Host`, or one driving another
CLI, in the dry run before every apply, and the refusal is reported on the
Company, as the assistant chain's is.

## What v1alpha1 does not offer

Crewlet's `custom` CLI and the profile `overrides` of the built-in ones are
not offered, but for the web stance above: they name the binary and the
arguments the engine's pod runs. Nor are the `direct` and `container` cells,
for the reason above.

Nor is Crewlet's `inherit-env` login, which gives the CLI whichever token or
key the engine's container environment holds. A credential there is outside
the engine's encrypted store: every process the engine runs (the CLI, a
stdio MCP server) can read it, and the engine resolves a `${NAME}` written in
many fields of the company document from its environment too, so whoever can
write such a field can have the engine send the value anywhere. The operator
puts no credential of the engine's own into its environment for the same
reason ([Credential Positions](../concepts/credentials.md)); `tokenSecretRef`,
`credentialBundleSecretRef`, `auth.mode: APIKey` and a key in `env` for
Hermes, Pi or OpenCode take the login from a Secret instead.

## See also

- [Crewlet's LLM backends](https://github.com/crewlet/crewlet/blob/main/docs/concepts/llm-backends.md) — what the engine does with a CLI
- [Choosing Where Seats Run](topology.md) — seat pools, and what a CLI cannot do there
- [Exposure and Network Policy](exposure.md) — the public listener the tool bridge is served on
