# Credentials

Every credential reaches the operator as a reference to a Kubernetes `Secret`,
never as a value in a field. This page lists every position in a Crewlet
company document that holds a credential, and the field of the `crewlet.ai`
API that fills it. The list is Crewlet's own: the fields its configuration
marks as secret, which it masks on every read.

How references work is explained in
[How the operator models a company](overview.md#credentials-by-reference-never-by-value).
In short: the operator reads each referenced value, pushes it into the
engine's encrypted secret store, and writes only the value's name into the
document. Rewriting a Secret rotates its value without restarting the engine.
Secrets referenced from a namespaced object are in that object's namespace;
those referenced from an `EngineClass` name their namespace.

## Covered in v1alpha1

| Crewlet position | Field | Reference |
|---|---|---|
| `providers.llm.<key>.api_keys` | `LLMProvider.spec.apiKeySecretRefs` | one `SecretKeyReference` per key |
| `providers.llm.<key>.api_keys`, for a model the platform provides | `EngineClass.spec.platformLLMProviders[].apiKeySecretRefs` | one `NamespacedSecretKeyReference` per key |
| `providers.embeddings.api_key` | `Company.spec.embeddings.apiKeySecretRef` | `SecretKeyReference` |
| `providers.sandbox.e2b.api_key` | `Company.spec.sandbox.e2b.apiKeySecretRef` | `SecretKeyReference` |
| `providers.sandbox.setup[].files` | `Company.spec.sandbox.setup[].files[].valueFrom.secretKeyRef` | one `SecretKeyReference` per file |
| `providers.sandbox.setup[].env` | `Company.spec.sandbox.setup[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `mcp_servers[].env` | `MCPServer.spec.env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `mcp_servers[].headers` | `MCPServer.spec.headers[].valueFrom.secretKeyRef` | one `SecretKeyReference` per header |
| `units[].mcp_env` | `Unit.spec.mcp[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable or header |
| `roles[].mcp_env` | `Seat.spec.mcp[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable or header |
| `roles[].sandbox.env` | `Seat.spec.sandbox.env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `roles[].sandbox.setup[].files` | `Seat.spec.sandbox.setup[].files[].valueFrom.secretKeyRef` | one `SecretKeyReference` per file |
| `roles[].sandbox.setup[].env` | `Seat.spec.sandbox.setup[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `roles[].integrations.slack.bot_token` | `Seat.spec.integrations.slack.credentialsSecretRef` | `SecretReference`, key `botToken` (`SlackBotTokenKey`) |
| `roles[].integrations.slack.signing_secret` | `Seat.spec.integrations.slack.credentialsSecretRef` | `SecretReference`, key `signingSecret` (`SlackSigningSecretKey`) |
| `roles[].integrations.github.private_key` | `Seat.spec.integrations.github.app.credentialsSecretRef` | `SecretReference`, key `privateKey` (`GitHubAppPrivateKeyKey`) |
| `roles[].integrations.github.webhook_secret` | `Seat.spec.integrations.github.app.credentialsSecretRef` | `SecretReference`, key `webhookSecret` (`GitHubAppWebhookSecretKey`) |
| `integrations.github.webhook_secret` | `Integration.spec.github.webhookSecretRef` | `SecretKeyReference` |
| `integrations.github.token` | `Integration.spec.github.tokenSecretRef` | `SecretKeyReference` |

An `env` or `headers` entry takes its value from a Secret even when the value
is not secret (a feature flag a tool server reads, say): Crewlet treats the
whole list as credentials, and the API cannot tell one entry from another.
Keep such settings in the same Secret as the tool's token.

## Not offered in v1alpha1

These positions belong to features v1alpha1 does not offer. No field can fill
them, and nothing can put a literal there either: they sit in the blocks
`integrations`, `providers`, `mcp_servers`, `roles` and `units`, and the API
server refuses a Company whose `spec.engineConfig` names any of those (or any
other block a field of the API owns), so such a Company is never stored.

| Crewlet position | Why it is not offered |
|---|---|
| `integrations.gitlab.signing_secret`, `integrations.gitlab.token`, `integrations.gitlab.provisioning.admin_token` | The GitLab integration is not offered yet. |
| `integrations.mattermost.provisioning.admin_token`, `roles[].integrations.mattermost.bot_token` | The Mattermost integration is not offered yet. |
| `integrations.jira.token`, `integrations.jira.webhook_secret` | The Jira integration is not offered yet; so the tracker backend is Native or None. |
| `integrations.confluence.token`, `integrations.confluence.webhook_secret`, `integrations.confluence.webhook_token` | The Confluence integration is not offered yet; so the knowledge backend is Native or None. |
| `integrations.datadog.webhook_token`, `integrations.datadog.provisioning.api_key`, `integrations.datadog.provisioning.app_key` | The Datadog integration is not offered yet. |
| `integrations.atlassian.api_key` | The Atlassian organization integration is not offered yet. |
| `providers.llm.<key>.cli.auth.token`, `providers.llm.<key>.cli.auth.credential_bundle`, `providers.llm.<key>.cli.env` | The `cli-agent` provider type is not offered: the engine image ships no coding CLI, and a subscription login belongs to one machine, which a pod that can be rescheduled at any time is not. |

Each integration arrives as a new member of the `Integration` union, with a
reference field for each of its credentials.

## Credentials the operator makes or copies itself

Some credentials belong to how the engine runs rather than to the company
document. The operator handles them without a field:

- **The secret-store keyring and the API tokens** are generated by the
  operator, once per company, into Secrets it owns in the company's
  namespace. A token for consumers of the engine's API (a control plane, a
  dashboard) is published in the Secret named by
  `Company.status.apiAccessSecretRef`; the operator uses a token of its own,
  so revoking the consumers' token never locks the operator out.
- **Object-store keys** (`EngineClass.spec.objects.s3.credentialsSecretRef`,
  keys `accessKeyID` and `secretAccessKey`), **telemetry headers**
  (`EngineClass.spec.telemetry.headersSecretRef`) and **class-wide
  environment variables from a Secret**
  (`EngineClass.spec.podTemplate.spec.env[].valueFrom`) are read by the engine
  from its environment. The operator copies them into a Secret it owns in the
  company's namespace and references that Secret from the pod, so the values
  never appear in a pod template. Without `credentialsSecretRef` the engine
  reaches the bucket with the pod's own identity: EKS Pod Identity, the
  node's role, or a workload identity bound through the ServiceAccount's
  annotations (`EngineClass.spec.serviceAccount.annotations`, such as
  `eks.amazonaws.com/role-arn` or `iam.gke.io/gcp-service-account`).
- **Platform wiring** goes in the engine's environment: what every company
  of a class shares in `EngineClass.spec.podTemplate.spec.env` (from a
  Secret in the platform's namespace), and what is one company's own in
  `Company.spec.runtime.env` (from a Secret in its namespace), which is
  merged over the class's by name. Use `valueFrom` for anything sensitive.
  Neither accepts `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`: Crewlet reads
  those as the key of a provider that names none, which would make them
  credentials outside the secret store, so a provider's key always comes
  from its own Secret reference.
