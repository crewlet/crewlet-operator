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
image you run, pinning both the engine and the CLI:

```dockerfile
FROM ghcr.io/crewlet/crewlet:v0.1.0
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends nodejs npm \
    && npm install -g @anthropic-ai/claude-code@2.1.295 \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*
USER crewlet
```

and name it in the class, which every company of the class then runs:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: with-claude-code
spec:
  engine:
    image: registry.example.com/crewlet-claude-code:v0.1.0
```

The engine's root filesystem is read-only: a CLI writes under the home the
engine gives each seat and under `/tmp`, both writable. Each running CLI
takes 200 to 400 MB, so size the class's `data.resources` for
`cli.maxConcurrent` of them per provider.

## Signing in

A subscription login is made at a terminal, by `crewlet llm login` or the
CLI's own command, and a pod has none. `cli.auth` therefore takes the login
from a Secret, in one of two forms:

| `auth` | What the Secret holds | Use it when |
|---|---|---|
| `tokenSecretRef` | a long-lived headless token the CLI mints, such as the `CLAUDE_CODE_OAUTH_TOKEN` of `claude setup-token` | the CLI mints one: it never needs refreshing, and it is the only login an agent-mode run can carry |
| `credentialBundleSecretRef` | a bundle of the CLI's credential files, from `crewlet llm export <key>` on a machine where you logged in | the CLI mints no token (Codex, Gemini CLI) |

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

`auth.mode: APIKey` gives the CLI the provider's one `apiKeySecretRefs` key
instead, billing the metered account.

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
- Only Claude Code and OpenCode have a sandbox runner in the engine.
- The login must follow the run into a remote sandbox, which only a headless
  token can: an agent-mode Claude Code takes `auth.tokenSecretRef`, or
  `auth.mode: APIKey`. A credential bundle stays on the data volume.
- The run calls the seat's own tools back through the engine's tool bridge.
  While any LLMProvider of the company is in agent mode, the operator gives
  the engine its public address as the bridge's
  (`CREWLET_MCP_BRIDGE_URL`) and routes `/mcp/` beside `/webhooks/`
  ([Exposure](exposure.md)). A company with no public address cannot run
  one: the LLMProvider reports `ResolvedRefs=False NotExposed`.

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
nothing between starts and serves no port. Two things a data pod does for a
CLI it cannot do:

- **Agent mode.** A run calls the seat's tools back through the tool bridge
  of the very pod that holds the seat, on an address the sandbox reaches; the
  operator gives the data pod the company's public address for it, and a
  seat pod has neither a bridge nor an address. A seat in a seat pool whose
  executor's chain (`spec.llm.default`, or the Company's first provider when
  the seat names none) holds a provider in `mode: Agent` is refused with
  `Applied=False`, `AgentModeBridgeUnsupported`. The same provider in another
  phase's chain is a text call, and is fine.
- **A credential bundle.** The login lives under the CLI's state directory,
  which on a seat pod is its scratch volume, so every seat pod restores the
  bundle at each start and then refreshes the login on its own, beside the
  data pod. A vendor that rotates refresh tokens retires the token the other
  pods hold at each refresh, and logs them out. A seat in a seat pool that
  runs on a provider with `credentialBundleSecretRef`, in any phase, is
  refused with `Applied=False`, `CLIAuthNotPortable`: sign it in with
  `tokenSecretRef` or `auth.mode: APIKey`, or place the seat in `data`.

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
`credentialBundleSecretRef` and `auth.mode: APIKey` take the login from a
Secret instead.
