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
engine's pods: S3 credentials for the object store
(`objects.s3.credentialsSecretRef`, keys `accessKeyID` and
`secretAccessKey`), telemetry headers (`telemetry.headersSecretRef`) and
class-wide environment variables (`podTemplate.spec.env[].valueFrom`). The
operator copies those values into a Secret it owns in each company's
namespace (`<company>-platform`), because a pod can read only Secrets in its
own namespace. A Company's own `spec.runtime.env` reads Secrets in its
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
keys: add the new one as a second entry of `apiKeySecretRefs`, wait for the
revision, revoke the old one at the provider, then remove its entry.

Secrets the engine's pods read from their environment are the exception:
Kubernetes reads them when a container starts. The operator watches its own
copies (the API tokens, the keyring, the platform Secret) and rolls the data
pod when one changes. A Secret a Company's `spec.runtime.env` references is
not watched; restart the data pod after changing it
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
  the Secret `status.apiAccessSecretRef` names.

Deleting the tokens Secret makes the operator generate new tokens and roll
the data pod; consumers then read the new one from the access Secret. Never
delete the keyring of a company that has data: the values sealed with it
cannot be read without it.

## When a credential does not arrive

| You see | It means |
|---|---|
| `ResolvedRefs=False SecretNotFound` on an object, `SecretsSynced=False SecretNotFound` on the Company | The Secret or the key is missing in the object's namespace. The message names both. |
| `SecretTooLarge` | The value is over the 64 KiB the engine's secret store accepts. |
| `SecretsSynced=False EngineUnavailable` | The values could not be written to the engine; the message says why. The operator retries. |
| `SecretsSynced=Unknown NotManaged` | The management policy keeps the operator out of the engine's secret store (`Observe`, and `Seed` once seeded). |
| `RuntimeReady=False SecretNotFound` | A Secret the EngineClass references for the pods is missing in the platform's namespace, or the API tokens Secret lacks a token. |
| `Integration` `Healthy=False CheckFailed` | The engine reached the vendor and the credential did not work; the message carries the vendor's answer. |

[Troubleshooting](troubleshooting.md) lists every condition.
