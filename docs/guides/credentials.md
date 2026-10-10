# Supplying credentials

Every credential a company uses — a model provider's API key, an agent's
Slack app, a tool server's token — is a key of a Kubernetes `Secret` that a
field of the `crewlet.ai` API refers to. No field accepts the value itself.
This guide shows how to supply each one, how to rotate it, and what to look
at when one does not arrive.
[Credentials](../concepts/credentials.md) is the complete reference: every
credential position Crewlet has and the field that fills it.

## How a value reaches the engine

1. You write a Secret in the object's namespace. Who writes it does not
   matter to the operator: `kubectl`, External Secrets, a CSI driver's
   synced Secret, or a control plane.
2. An object refers to one of its keys (`SecretKeyReference`, `{name, key}`)
   or to the whole Secret (`SecretReference`, `{name}`, where the field
   documents the keys it must hold).
3. The operator reads the value, pushes it into the company's engine, into its
   encrypted secret store, and writes only the entry's name into the company
   document as `${K8S_…}`.

The value never appears in a pod, an environment variable, an annotation, a
status, an event or a log line. The operator reads Secrets from the API
server when it applies a document and does not keep them in its cache.

A Secret must exist before the document that references it can be applied.
Until it does, the next revision waits and the engine keeps running the last
one; the object that references it reports `ResolvedRefs=False` with the
reason `SecretNotFound`, naming the Secret and key, and the Company reports
`SecretsSynced=False`. Create the Secret first, or in the same apply: the
operator waits two quiet seconds before it renders, so objects and Secrets
written together are applied together.

## Each credential

### A model provider's API keys

```bash
kubectl create secret generic anthropic -n acme --from-literal=apiKey=sk-ant-...
```

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: anthropic
spec:
  companyRef: {name: acme}
  type: Anthropic
  model: claude-sonnet-5
  apiKeySecretRefs:
    - {name: anthropic, key: apiKey}
    - {name: anthropic, key: apiKeyBackup}   # optional: more keys, rotated through
```

With several keys Crewlet spreads requests over them and sets aside a key that
hits a rate limit or is refused, for `cooldowns.rateLimitSeconds` or
`cooldowns.authSeconds`. An `OpenAICompatible` provider that needs no key may
leave `apiKeySecretRefs` out.

A model the platform provides is declared once on the `EngineClass`, in
`spec.platformLLMProviders`, with references that name their namespace:
the Secret stays in the platform's namespace, which tenants cannot read, and
a Company opts in with `spec.llm.usePlatformProviders: true`.

### Embeddings and the code sandbox

```yaml
spec:                       # Company
  embeddings:
    type: OpenAI
    model: text-embedding-3-large
    apiKeySecretRef: {name: openai, key: apiKey}
  sandbox:
    e2b:
      apiKeySecretRef: {name: e2b, key: apiKey}
```

Variables and files the sandbox sets up for every run (`sandbox.setup[].env`,
`sandbox.setup[].files`) and a Seat's own sandbox variables
(`Seat.spec.sandbox.env`) take `valueFrom.secretKeyRef` for every entry.

### What a coding box carries

A seat's code work signs its coding agent in with the provider it runs on:
the head of `spec.llm.sandbox`, else of `spec.llm.default`, else of the
Company's order. From crewlet 4e45c6b99, the engine exports into every box
of such a seat:

- for an `Anthropic`, `OpenAI` or `OpenAICompatible` provider, an
  `LLMProvider` or a platform provider, the **first** key of its
  `apiKeySecretRefs`, as `ANTHROPIC_API_KEY` (`Anthropic`) or
  `OPENAI_API_KEY` (the others), and its `baseURL`: under Claude Code as
  `ANTHROPIC_BASE_URL`, and under OpenCode in the box's `opencode.json`,
  as the endpoint of the provider the run is pointed at;
- for a `CLIAgent` provider, its headless token, its key, or the
  credential-named variables of its `cli.env`, never its credential bundle.

The box runs in the Company's own E2B account, and the coding agent in it
runs with every permission, so it can read the key; the report the run
sends back is redacted by the shape of a credential, not by its value. A
box holds the key it started with and never rotates it, and only the first
key travels, so rotating the provider's keys reaches a running box only
when its next run starts. Give code work a key of its own: a provider whose
key is scoped and limited for code work, first in `spec.llm.sandbox`.

An `Anthropic` provider's `baseURL` is the gateway's root, without `/v1`
(`https://gateway.example.com`, not `https://gateway.example.com/v1`):
Crewlet's own calls, Claude Code and OpenCode each add the version
themselves, so a `baseURL` ending in `/v1` is called at `/v1/v1` by all
three. An `OpenAI` or `OpenAICompatible` provider's `baseURL` ends where
the OpenAI wire expects it, usually in `/v1`, and reaches the box as
written.

A seat that signs Claude Code in itself, with `CLAUDE_CODE_OAUTH_TOKEN`,
`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or a `CLAUDE_CODE_USE_*` toggle
in `spec.sandbox.env` or a setup step's `env`, gets neither the provider's
key nor its endpoint: its own sign-in is what the run uses. Under OpenCode
the provider's key is added beneath the seat's variables, so a seat variable
of the same name wins.

### An agent's Slack app

Each agent seat is its own Slack app, so it posts and is mentioned as itself.
One Secret per seat, with the two keys Slack issues:

```bash
kubectl create secret generic lead-slack -n acme \
  --from-literal=botToken=xoxb-... --from-literal=signingSecret=...
```

```yaml
spec:                       # Seat lead
  integrations:
    slack:
      credentialsSecretRef: {name: lead-slack}   # keys botToken, signingSecret
```

The app's request URL is in the Seat's status,
`status.integrations.slack.requestURL`, as soon as the Company has a Slack
`Integration` and a public address; it does not depend on the credentials,
so it can go into the app's manifest before the app exists.

### An agent's GitHub App

```bash
kubectl create secret generic lead-github-app -n acme \
  --from-file=privateKey=lead.private-key.pem --from-literal=webhookSecret=...
```

```yaml
spec:                       # Seat lead
  integrations:
    github:
      tier: Review
      repos: [acme/widgets]
      app:
        id: 1234567
        slug: acme-lead
        installationID: 89012345
        credentialsSecretRef: {name: lead-github-app}   # keys privateKey, webhookSecret
```

The webhook URL for the app is `status.integrations.github.webhookURL`.

### The company's GitHub organization

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: github
spec:
  companyRef: {name: acme}
  github:
    webhookSecretRef: {name: acme-github, key: webhookSecret}
    tokenSecretRef: {name: acme-github, key: orgToken}
```

### An agent's Mattermost bot

Each agent seat speaks on Mattermost as its own bot, whose one token covers
everything it does there:

```yaml
spec:                       # Seat lead
  integrations:
    mattermost:
      botToken:
        secretKeyRef: {name: lead-mattermost, key: botToken}
```

### The other integrations

GitLab's signing token and read token, Jira's and Confluence's administrator
tokens and webhook secrets, Datadog's webhook token and organization keys,
and the administrator credentials the engine creates the seats' accounts
with are each a `SecretKeyReference` of the vendor's `Integration`;
[Connecting integrations](integrations.md) shows every one.

### Credentials the engine provisions

Under an `Integration` with `provisioning: Engine`, the engine creates each
agent seat's account at the vendor and mints its credential itself, into its
own secret store. No Secret holds that value, so the seat names it as
**provisioned** where it would name a Secret key:

```yaml
spec:                       # Seat lead
  mcp:
    - name: gitlab
      env:
        - name: GITLAB_TOKEN
          valueFrom:
            provisioned: {name: gitlab-token}
```

The engine rotates and heals it; the operator never writes, rewrites or
deletes it. [Credentials](../concepts/credentials.md#credentials-the-engine-provisions)
says how it is named.

### A coding CLI's login

A `CLIAgent` provider signs its CLI in with a headless subscription token or
with a credential bundle `crewlet llm export` wrote, each a Secret key; with
`auth.mode: APIKey`, with the provider's `apiKeySecretRefs` key; or, for
Hermes, Pi and OpenCode, with its model provider's key in `cli.env`, which
may never set the CLI's own token or key variable, since `auth` owns it:

```yaml
spec:                       # LLMProvider claude-code
  type: CLIAgent
  model: sonnet
  cli:
    agent: ClaudeCode
    auth:
      tokenSecretRef: {name: claude-subscription, key: oauthToken}
    env:
      - name: HTTPS_PROXY       # every variable comes from a Secret
        valueFrom:
          secretKeyRef: {name: claude-subscription, key: proxy}
```

[Running seats on a coding CLI](cli-agents.md) says which to use.

### A tool server's token

An `MCPServer` shared by every seat takes its credentials as environment
variables (Stdio) or headers (HTTP), each from a Secret:

```yaml
spec:                       # MCPServer linear
  transport: HTTP
  url: https://mcp.linear.app/mcp
  headers:
    - name: Authorization
      valueFrom:
        secretKeyRef: {name: linear, key: authorization}   # "Bearer lin_api_..."
```

A server with `shared: false` starts one instance per seat, and each Seat (or
the Unit it is in) brings its own credentials for it in `spec.mcp`:

```yaml
spec:                       # Seat lead
  mcp:
    - name: github          # the MCPServer's name
      env:
        - name: Authorization
          valueFrom:
            secretKeyRef: {name: lead-github, key: authorization}
```

Every `env` and `headers` entry comes from a Secret, even a setting that is
not secret: Crewlet treats the whole list as credentials. Keep such settings
in the same Secret as the token.

### What the platform supplies

An `EngineClass` may reference Secrets in the platform's own namespace for the
engine's pods: telemetry headers (`telemetry.headersSecretRef`) and
class-wide environment variables (`podTemplate.spec.env[].valueFrom`). The
operator copies those values into the Secret it owns in each company's
namespace for the engine's pod (`<company>-bootstrap`), because a pod can
read only Secrets in its own namespace.

The keys of a class's S3 bucket are each company's own, in its namespace:

```yaml
apiVersion: v1
kind: Secret
metadata: {name: acme-objects, namespace: acme}
stringData:
  accessKeyID: AKIA...          # a key whose policy admits the company's prefix only
  secretAccessKey: ...
---
# Company acme
spec:
  objects:
    credentialsSecretRef: {name: acme-objects}
```

Issue them per company, with a policy that admits only the company's prefix
of the bucket (the class's `objects.s3.prefix`, expanded): a key the class
shared would let every tenant read and write every other tenant's files,
because whatever reaches an engine pod reaches code the tenant runs there. A Company's own `spec.runtime.env` reads Secrets in its
namespace directly.

## Rotating a credential

Write the new value into the same Secret and key. The operator sees the
change, pushes the new value into the engine's secret store, and applies a
revision with the same document, because the engine reads a value when a
revision activates. The revision's summary names the Secret. The engine does
not restart and no seat loses its conversation.

```bash
kubectl create secret generic anthropic -n acme --from-literal=apiKey=sk-ant-new... \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl get company acme -n acme -o jsonpath='{.status.config.revisionID}{"\n"}'
```

A change that leaves every referenced value as it was (a label, an
annotation, another key nothing references) writes nothing.

To rotate without a moment where the provider refuses the old key, use two
keys. For a provider no seat's code work runs on: add the new one as a
second entry of `apiKeySecretRefs`, wait for the revision, revoke the old
one at the provider, then remove its entry; the engine benches a refused
key and calls with the other meanwhile. A coding box cannot: it carries the
provider's first key alone and never rotates it
([What a coding box carries](#what-a-coding-box-carries)). So for a
provider that backs code work, add the new key first in
`apiKeySecretRefs`, or remove the old entry once the new one is there, wait
for that revision and for the coding runs already going to end, and only
then revoke the old key.

What the engine reads when it starts is the exception. The values in its
`crewlet.yaml` (the API tokens, the keyring, the Company's object-store keys,
the class's telemetry headers and env values) are read once: the operator
watches the Secrets they come from and rolls the data pod when one changes.
A Secret a Company's `spec.runtime.env` references is not watched; restart
the data pod after changing it
(`kubectl rollout restart statefulset/<company>-data`).

## The engine's own Secrets

The operator generates two Secrets for every company and never rewrites them:

- `<company>-keyring`: the keys that seal the engine's secret store. With
  `deletionPolicy: Retain` it outlives the Company along with the data
  volume, since neither is readable without the other. A platform may
  supply it instead (an ExternalSecret, say): a Secret of that name that
  another controller owns is used as it is. Its annotation
  `crewlet.ai/active-key-id` names the key that seals new writes.
- `<company>-api-tokens`: the operator's own token and the one for the
  company's consumers, which is also published, with the API's address, in
  the Secret `status.apiAccessSecretRef` names. A platform may supply this
  one too; each token must be at least 43 characters of base64, base64url
  or hex (32 random bytes: `openssl rand -base64 32`), or the pod is held
  back with `RuntimeReady=False APITokensInvalid`.

Deleting the tokens Secret makes the operator generate new tokens and roll
the data pod; consumers then read the new one from the access Secret. Never
delete the keyring of a company that has data: the values sealed with it
cannot be read without it.

Rotating the keyring is not supported in v1alpha1. Crewlet rotates a key by
adding the new one, making it active, re-sealing both its secret store and
its stored company configuration under it (`crewlet secrets rekey` and
`crewlet config rekey`), and only then dropping the old one; the operator
runs none of those steps, and a key removed before the re-seal leaves
whatever it sealed unreadable, the engine refusing a snapshot it cannot
open whole. Adding a key to the Secret is safe (the pod rolls onto a
keyring that opens everything it did); removing one is not.

## When a credential does not arrive

| You see | It means |
|---|---|
| `ResolvedRefs=False SecretNotFound` on an object, `SecretsSynced=False SecretNotFound` on the Company | The Secret or the key is missing in the object's namespace. The message names both. |
| `SecretTooLarge` | The value is over the 64 KiB the engine's secret store accepts. |
| `SecretsSynced=False EngineUnavailable` | The values could not be written to the engine; the message says why. The operator retries. |
| `SecretsSynced=Unknown NotManaged` | The management policy keeps the operator out of the engine's secret store (`Observe`, and `Seed` once seeded). |
| `RuntimeReady=False SecretNotFound` | A Secret the EngineClass references for the pods is missing in the platform's namespace, or the Company's `spec.objects.credentialsSecretRef` is. |
| `RuntimeReady=False APITokensInvalid` | A supplied `<company>-api-tokens` lacks a token or holds one the engine should not be guarded by; the message says which. |
| `Integration` `Healthy=False CheckFailed` | The engine reached the vendor and the credential did not work; the message carries the vendor's answer. |
| `Seat` `ResolvedRefs=False NoProvisioner` | The Seat names a provisioned credential, and no Integration of its company has `provisioning: Engine` to create it. |

[Troubleshooting](troubleshooting.md) lists every condition.
