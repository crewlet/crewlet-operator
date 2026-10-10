# Running seats on a coding CLI

An `LLMProvider` of type `CLIAgent` drives a coding CLI (Claude Code, Codex,
Gemini CLI and the others Crewlet knows) on a subscription login, rather than
calling a model API with a key. It renders Crewlet's `cli-agent` provider;
Crewlet's [Subscription LLM backends](https://github.com/crewlet/crewlet/blob/main/docs/concepts/subscription-llm-backends.md)
describes what the engine does with it. This guide covers what changes when
the engine runs in a pod: the image, the login, and agent mode.

## The image needs the CLI

The CLI is a program the engine starts, in the engine's own container, and
Crewlet's image ships none: its Dockerfile leaves every runtime a CLI needs
(Node.js for most of them) to a derived image. Build one from the engine
image you run, the one [Installing](installing.md#engine-image) builds at
the operator's `CREWLET_VERSION`, pinning both the engine and the CLI:

```dockerfile
# The engine image you built and pushed: <your registry>/crewlet:<CREWLET_VERSION>,
# by digest. Never an older engine than the operator's CREWLET_VERSION.
ARG ENGINE_IMAGE
FROM ${ENGINE_IMAGE}
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends nodejs npm \
    && npm install -g @anthropic-ai/claude-code@2.1.295 \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*
USER crewlet
```

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet@sha256:<digest> \
  -t <your registry>/crewlet-claude-code:<CREWLET_VERSION> .
```

and name it in the class, which every company of the class then runs:

```yaml
# EngineClass with-claude-code, excerpt
spec:
  engine:
    image: <your registry>/crewlet-claude-code@sha256:<digest>   # <CREWLET_VERSION>
```

The engine's root filesystem is read-only: a CLI writes under the home the
engine gives each seat and under `/tmp`, both writable. Each running CLI
takes 200 to 400 MB, so size the class's `data.resources` for
`cli.maxConcurrent` of them per provider.

## Signing in

A subscription login is made at a terminal, by `crewlet llm login` or the
CLI's own command, and a pod has none. `cli.auth` therefore takes the login
from a Secret, in one of two forms; takes a metered key instead; or, for the
three CLIs that read their model provider's key from their environment,
takes none and signs in with that key in `cli.env`:

| Sign-in | What the Secret holds | Use it when |
|---|---|---|
| `auth.tokenSecretRef` | a long-lived headless token the CLI mints, such as the `CLAUDE_CODE_OAUTH_TOKEN` of `claude setup-token` | the CLI is Claude Code, the one CLI whose Crewlet profile names a token variable: it never needs refreshing, and it is the only subscription login an agent-mode run can carry |
| `auth.credentialBundleSecretRef` | a bundle of the CLI's credential files, from `crewlet llm export <key>` on a machine where you logged in | any CLI: the others mint no token Crewlet hands on |
| `auth.mode: APIKey` with one `apiKeySecretRefs` key | the CLI's metered API key | the CLI's Crewlet profile names a key variable ([below](#a-metered-key)) |
| `env`, with `auth.mode: Subscription` | the key of the model provider the CLI runs on, in the variable the CLI reads for it (`OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `NOUS_API_KEY`, ...) | the CLI is Hermes, Pi or OpenCode: with its model provider's key in its environment, it needs no login |

What each CLI takes, at the engine version the operator is built against
(Crewlet's profiles name the variables; a test holds the API's rules and this
table to that engine's own verdict on every CLI and every sign-in, so neither
can fall behind it):

| `cli.agent` | `tokenSecretRef` | `auth.mode: APIKey` fills | A key in `env` | `credentialBundleSecretRef` | `mode: Agent` |
|---|---|---|---|---|---|
| `ClaudeCode` | `CLAUDE_CODE_OAUTH_TOKEN` | `ANTHROPIC_API_KEY` | no | yes | on the token or the key |
| `Codex` | no | `OPENAI_API_KEY` | no | yes | no |
| `GeminiCLI` | no | `GEMINI_API_KEY` | no | yes | no |
| `QwenCode` | no | `DASHSCOPE_API_KEY` | no | yes | no |
| `CursorAgent` | no | `CURSOR_API_KEY`, a Cursor account's API key | no | yes | no |
| `Copilot` | no | `GITHUB_TOKEN` | no | yes | no |
| `Grok` | no | `XAI_API_KEY` | no | yes | no |
| `MuseCode` | no | `META_API_KEY` | no | yes | no |
| `KimiCode` | no | no: it reads a key only from its own `config.toml`, so give that in the bundle | no | yes | no |
| `OpenCode` | no | no | yes | yes | no: no one `model` serves its run and its text calls ([below](#text-mode-and-agent-mode)) |
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
  as signing in through its environment: Crewlet passes the CLI's `env` to
  it untouched, which is the route its profile documents for a metered key.
  Crewlet counts an entry as a sign-in only when its name says it holds a
  credential: one of its parts between underscores is, in any case, `KEY`,
  `KEYS`, `APIKEY`, `ACCESSKEY`, `SECRETKEY`, `TOKEN`, `SECRET`, `PASSWORD`,
  `PASSWD`, `CREDENTIAL`, `CREDENTIALS`, `AUTH`, `OAUTH` or `PAT`. So
  `OPENROUTER_API_KEY` and `GH_TOKEN` sign the CLI in, and `HTTPS_PROXY` or
  `HERMES_MAX_TOKENS` (a count of model tokens) do not; the API server
  refuses a provider whose `env` holds no such name, which `crewlet llm
  list` would report signed in to nothing. Write `auth.mode: Subscription`
  explicitly, as `auth` may not be empty. A key in `env` is a `${NAME}` the
  engine resolves from its encrypted store, so it never refreshes and runs
  on a seat pod too. Beside a credential bundle it is a second sign-in,
  which the CLI settles by its own rules rather than by the order you wrote
  them in. Hermes, for one, loads its home's `.env` over its environment, so
  a variable the bundle's `.env` also sets takes the file's value, while a
  model provider's key in `env` wins over an OAuth login in the bundle's
  `auth.json`, so Hermes runs on the metered key and the subscription goes
  unused. Name only the sign-in you mean the CLI to use.
- **Every other CLI** keeps to a token or a bundle. A CLI whose profile names
  a key variable takes its key with `auth.mode: APIKey`; the engine refuses
  that variable in `env` (below), and a key in `env` under any other name is
  one the CLI does not read.

`env` may never set the CLI's own token or key variable, the ones the table
names (`ANTHROPIC_API_KEY` for `ClaudeCode`, `CURSOR_API_KEY` for
`CursorAgent`, and so on), in any auth mode: `auth` owns them, and whether a
value in `env` reached the CLI would turn on the mode and on what else holds
a credential, so the engine refuses the document and the API server refuses
the provider first. Another CLI's variable is no such conflict:
`ANTHROPIC_API_KEY` is OpenCode's Anthropic key.

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: hermes
spec:
  companyRef: {name: acme}
  type: CLIAgent
  model: anthropic/claude-sonnet-5   # an OpenRouter model id: OPENROUTER_API_KEY selects OpenRouter
  cli:
    agent: Hermes
    auth:
      mode: Subscription
    env:
      - name: OPENROUTER_API_KEY
        valueFrom:
          secretKeyRef: {name: openrouter, key: apiKey}
```

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
  type: CLIAgent
  model: sonnet
  cli:
    agent: ClaudeCode
    auth:
      tokenSecretRef: {name: claude-subscription, key: oauthToken}
```

The login, and the homes the CLI keeps for each seat, live on the data
volume, under `/var/lib/crewlet/llm-cli/<loginName>`, so a restarted data pod
keeps them; a seat pod keeps them on a scratch volume it loses at every start
(see [Seat pools](#seat-pools)). That matters for a bundle: the engine restores one only while
the login there is empty, so the login the running engine refreshed wins
over the bundle in the Secret, whose refresh token the vendor may have
retired since. A bundle restored over a newer login is how a vendor that
rotates refresh tokens logs a deployment out, which is why a token is the
better of the two wherever the CLI mints one.

Providers that set the same `loginName` share one login and its seats'
homes, which is how several models run off one subscription; they must
drive the same CLI, or the engine refuses the document.

### A metered key

`auth.mode: APIKey` gives the CLI the provider's one `apiKeySecretRefs` key
instead, billing the metered account. Crewlet hands the key over in the
variable the CLI's profile names, and at the pinned engine eight name one
(the table above); for the others the key would reach nothing, so the engine
refuses the mode for them and the API server refuses it first. Hermes,
Pi and OpenCode take their model provider's key in `env` instead, and
KimiCode its key in the bundle. `CursorAgent`'s key is a Cursor account's
API key, from the Cursor dashboard, not a model provider's.

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: cursor
spec:
  companyRef: {name: acme}
  type: CLIAgent
  model: auto
  apiKeySecretRefs:
    - {name: cursor, key: apiKey}
  cli:
    agent: CursorAgent
    auth:
      mode: APIKey
```

## Text mode and agent mode

`cli.mode: Text` (the default) runs the CLI as a model behind the engine's
own tool loop, as a process in the engine's pod. `cli.mode: Agent` runs a
seat's executor as the CLI's own agentic run, with its own shell and editor,
in the company's sandbox. Every other phase on the same provider (the
reviewer, a delegated worker, the summariser) is still a text call, so the
CLI must be in the engine's image either way. In agent mode:

- The run is a sandbox run: the Company needs `spec.sandbox`, whose template
  must carry the CLI too. `cli.runIn` is `E2B`, the only cell v1alpha1
  offers: Crewlet's local cells would run every seat's code in the engine's
  pod, with its credentials.
- Only Claude Code runs in agent mode. The engine has a sandbox runner for
  OpenCode too, one of the coding agents `spec.sandbox` offers, but at the
  pinned engine an agent-mode OpenCode run names its model to OpenCode as
  `anthropic/<model>`, the family taken from Crewlet's OpenCode profile,
  while every other phase on the same provider is a text call that passes
  `model` as written, and OpenCode reads a model as `<provider>/<model>`.
  No value of `model` serves both, so such a provider would fail its runs
  or its text calls, and the API server refuses it. Run OpenCode in text
  mode.
- The login must follow the run into a remote sandbox, which only a headless
  token or a key can: an agent-mode Claude Code takes `auth.tokenSecretRef`,
  or `auth.mode: APIKey`. A credential bundle stays on the data volume. A
  key in the Seat's `spec.sandbox.env` reaches the box as well, but the API
  server cannot see the Seat from the provider, so it does not count it.
- The run calls the seat's own tools back through the engine's tool bridge.
  While any LLMProvider of the company is in agent mode, the operator gives
  the engine its public address as the bridge's (`CREWLET_MCP_BRIDGE_URL`),
  and the engine serves the bridge on its public listener, beside the
  webhooks ([Exposure](exposure.md)). A company with no public address
  cannot run one: the LLMProvider reports `ResolvedRefs=False NotExposed`.

```yaml
spec:                       # LLMProvider claude-agent
  type: CLIAgent
  model: sonnet
  cli:
    agent: ClaudeCode
    mode: Agent
    loginName: claude-code        # the same login as the text-mode provider
    auth:
      tokenSecretRef: {name: claude-subscription, key: oauthToken}
```

A seat runs on it by naming it in `spec.llm.default`; see the Seat's
`spec.llm` in the [API reference](../reference/api.md).

## Seat pools

A seat placed in a [seat pool](topology.md) runs on a seat pod, which keeps
nothing between starts and serves only its probes. Two things a data pod
does for a CLI it cannot do:

- **Agent mode.** A run calls the seat's tools back through the tool bridge
  of the very pod that holds the seat, on an address the sandbox reaches; the
  operator gives the data pod the company's public address for it. The
  engine would serve the bridge on a seat pod too, on the listener its
  probes use, but nothing publishes an address of a seat pod's own that a
  remote sandbox could dial: the company's public address reaches the data
  pod alone, and a seat pod is reached by its kubelet and nothing else.
  Without an address the engine refuses every agent-mode run on the pod,
  after the revision has applied. Crewlet runs a turn in agent
  mode only when the HEAD of the executor's chain is in `mode: Agent`, so a
  seat in a seat pool whose executor's chain (`spec.llm.default`, or the
  Company's first provider when the seat names none) starts with such a
  provider is refused with `Applied=False`, `AgentModeBridgeUnsupported`. The
  same provider behind another in that chain, or in another phase's chain, is
  a text call, and is fine.
- **A credential bundle.** The login lives under the CLI's state directory,
  which on a seat pod is its scratch volume, so every seat pod restores the
  bundle at each start and then refreshes the login on its own, beside the
  data pod. A vendor that rotates refresh tokens retires the token the other
  pods hold at each refresh, and logs them out. A seat in a seat pool that
  runs on a provider with `credentialBundleSecretRef`, in any phase, is
  refused with `Applied=False`, `CLIAuthNotPortable`: sign it in with
  `tokenSecretRef` (Claude Code), `auth.mode: APIKey` (a CLI the table
  above gives a key variable) or, for Hermes, Pi or OpenCode, its model
  provider's key in `env`, or place the seat in `data`.

## What v1alpha1 does not offer

Crewlet's `custom` CLI and the profile `overrides` of the built-in ones are
not offered: they name the binary and the arguments the engine's pod runs.
Nor are the `direct` and `container` cells, for the reason above.

Nor is Crewlet's `inherit-env` login, which gives the CLI whichever token or
key the engine's container environment holds. A credential there is outside
the engine's encrypted store: every process the engine runs (the CLI, a
stdio MCP server) can read it, and the engine resolves a `${NAME}` written in
any field of the company document from its environment too, so whoever can
write such a field can have the engine send the value anywhere. The operator
puts no credential of the engine's own into its environment for the same
reason ([Credentials](../concepts/credentials.md)); `tokenSecretRef`,
`credentialBundleSecretRef`, `auth.mode: APIKey` and a key in `env` for
Hermes, Pi or OpenCode take the login from a Secret instead.
