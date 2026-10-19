# Credential Positions

Every credential reaches the operator as a reference to a Kubernetes Secret,
never as a value. This page lists every position that holds one, and its
field.

The list is Crewlet's own: the positions of a company document, and of a data
pod's `crewlet.yaml`, that its JSON Schema marks `"x-crewlet-secret": true`
(`crewlet schema company`, `crewlet schema bootstrap`), which are the fields
its configuration masks on every read. A position is written as the schema
places it: `[]` is every element of a list and `*` every value of a map. The
org chart nests, so a seat's positions are written from `roles[]` wherever the
chart puts the seat, and a unit's from `units[]` however deep among other
units' `children[]` it sits.

> **Checked against the engine.** A test holds the tables below to the marks
> of the engine the operator is tested against, Crewlet's `main`, in both
> directions: a credential position Crewlet adds fails the operator's build
> until it is covered (or listed as not offered, with the reason), and so does
> a row for a position Crewlet no longer marks. Another holds the rule on a
> Company's `engineConfig` to the same marks: it refuses every top-level block
> Crewlet holds a credential under, so a credential can never be written there
> as a literal. Nor in an address: every URL field refuses a user name and
> password (`https://user:password@host`), which an HTTP client sends as
> Basic authentication to whatever the address names.

How references work is explained in
[How the operator models a company](overview.md#credentials-by-reference-never-by-value).
In short: the operator reads each referenced value, pushes it into the
engine's encrypted secret store, and writes only the value's name into the
document. Rewriting a Secret rotates its value without restarting the engine.
Secrets referenced from a namespaced object are in that object's namespace;
those referenced from an `EngineClass` name their namespace.

---

## Who can read a value

By reference does not mean out of the tenant's reach. A value the engine
uses is readable by the company's tenant:

- The engine runs the tenant's code in its own container, as its own user:
  a `Stdio` MCP server, a coding CLI, and whatever an
  agent's tool calls have them do. It hands such a process only an
  allowlist of its environment
  ([Stdio and HTTP](../guides/tool-servers.md#stdio-and-http)), and a
  coding CLI also the variables its Crewlet profile passes through, but the
  process can read the rest of it, read `crewlet.yaml`, and open the secret
  store on the data volume with the keyring the pod holds: the allowlist
  keeps the engine from handing its values over, not a process from reading
  them.
- The access token the Company publishes (`status.apiAccessSecretRef`)
  holds Crewlet's `admin` grant, which takes in every app: the engine's API
  reveals any value in the secret store to it and lets it write one, and it
  may invite or create the company's people. Under `Enforce` the engine
  refuses it a change to the company document, since the data pod's
  `crewlet.yaml` names the operator's token the document's only writer
  (`api.auth.company_writers`); a value the document references can still
  be replaced through the secret store.
- A seat's code work and an agent-mode executor's run, the sandbox's setup
  commands included, run in the Company's E2B sandbox, in the Company's own
  E2B account, where the coding agent, or the executor's CLI, runs with
  every permission.
  Crewlet exports into each box of code work the first `apiKeySecretRefs`
  key of the provider the code work runs on, an `Anthropic`, `OpenAI` or
  `OpenAICompatible` `LLMProvider` or platform provider (as
  `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`), and under Claude Code its
  `baseURL` (`ANTHROPIC_BASE_URL`), unless the seat's own sandbox
  environment signs Claude Code in; a `CLIAgent` provider's token, key or
  credential-named variable in `cli.env` travels the same way, and so does
  an agent-mode provider's into each of its executor's runs. The coding
  agent or the CLI can read it, and the run's report is redacted by the
  shape of a credential, not by its value ([What a coding box
  carries](../guides/credentials.md#what-a-coding-box-carries)).

> **Caution: what a class gives its companies is every tenant's.** A
> credential an `EngineClass` gives its companies (a platform model key,
> telemetry headers, a class env value) is readable by every tenant of the
> class. Give a class only what each tenant may hold: per-tenant keys, or
> per-tenant virtual keys from an LLM gateway. Object-store keys are a
> Company's own (`spec.objects.credentialsSecretRef`) for that reason. In the
> tenant's namespace, grant `get` on `<company>-api-access` by
> `resourceNames`: read on every Secret there also exposes
> `<company>-api-tokens`, `<company>-keyring`, `<company>-bootstrap` and each
> seat pool's `<company>-bootstrap-<pool>-<hash>`, which hold the same keyring.

---

## Covered in v1alpha1

Every position of the company document but the one below is covered: each
has a field that fills it with a reference.

| Crewlet position | Field | Reference |
|---|---|---|
| `providers.llm.*.api_keys[]` | `LLMProvider.spec.apiKeySecretRefs` | one `SecretKeyReference` per key |
| `providers.llm.*.api_keys[]`, for a model the platform provides | `EngineClass.spec.platformLLMProviders[].apiKeySecretRefs` | one `NamespacedSecretKeyReference` per key |
| `providers.embeddings.api_key` | `Company.spec.embeddings.apiKeySecretRef` | `SecretKeyReference` |
| `providers.sandbox.e2b.api_key` | `Company.spec.sandbox.e2b.apiKeySecretRef` | `SecretKeyReference` |
| `providers.sandbox.setup[].files.*` | `Company.spec.sandbox.setup[].files[].valueFrom.secretKeyRef` | one `SecretKeyReference` per file |
| `providers.sandbox.setup[].env.*` | `Company.spec.sandbox.setup[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `mcp_servers[].env.*` | `MCPServer.spec.env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `mcp_servers[].headers.*` | `MCPServer.spec.headers[].valueFrom.secretKeyRef` | one `SecretKeyReference` per header |
| `units[].mcp_env.*.*` | `Unit.spec.mcp[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable or header |
| `roles[].mcp_env.*.*` | `Seat.spec.mcp[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable or header |
| `roles[].mcp_env.*.*`, under an Integration with provisioning Engine (GitLab, Mattermost, Datadog, Atlassian) | `Seat.spec.mcp[].env[].valueFrom.provisioned` | `ProvisionedCredential`: no Secret; the engine mints the value into its own store |
| `roles[].sandbox.env.*` | `Seat.spec.sandbox.env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `roles[].sandbox.env.*`, under an Integration with provisioning Engine | `Seat.spec.sandbox.env[].valueFrom.provisioned` | `ProvisionedCredential`: no Secret; the engine mints the value into its own store |
| `roles[].sandbox.setup[].files.*` | `Seat.spec.sandbox.setup[].files[].valueFrom.secretKeyRef` | one `SecretKeyReference` per file |
| `roles[].sandbox.setup[].env.*` | `Seat.spec.sandbox.setup[].env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |
| `roles[].integrations.slack.bot_token` | `Seat.spec.integrations.slack.credentialsSecretRef` | `SecretReference`, key `botToken` (`SlackBotTokenKey`) |
| `roles[].integrations.slack.signing_secret` | `Seat.spec.integrations.slack.credentialsSecretRef` | `SecretReference`, key `signingSecret` (`SlackSigningSecretKey`) |
| `roles[].integrations.github.private_key` | `Seat.spec.integrations.github.app.credentialsSecretRef` | `SecretReference`, key `privateKey` (`GitHubAppPrivateKeyKey`) |
| `roles[].integrations.github.webhook_secret` | `Seat.spec.integrations.github.app.credentialsSecretRef` | `SecretReference`, key `webhookSecret` (`GitHubAppWebhookSecretKey`) |
| `integrations.github.webhook_secret` | `Integration.spec.github.webhookSecretRef` | `SecretKeyReference` |
| `integrations.github.token` | `Integration.spec.github.tokenSecretRef` | `SecretKeyReference` |
| `integrations.gitlab.signing_secret` | `Integration.spec.gitlab.signingSecretRef` | `SecretKeyReference` |
| `integrations.gitlab.token` | `Integration.spec.gitlab.tokenSecretRef` | `SecretKeyReference` |
| `integrations.gitlab.provisioning.admin_token` | `Integration.spec.gitlab.engineProvisioning.adminTokenSecretRef` | `SecretKeyReference` |
| `integrations.mattermost.provisioning.admin_token` | `Integration.spec.mattermost.engineProvisioning.adminTokenSecretRef` | `SecretKeyReference` |
| `roles[].integrations.mattermost.bot_token` | `Seat.spec.integrations.mattermost.botToken.secretKeyRef` | `SecretKeyReference` |
| `roles[].integrations.mattermost.bot_token`, under a Mattermost Integration with provisioning Engine | `Seat.spec.integrations.mattermost.botToken.provisioned` | `ProvisionedCredential`: no Secret; the engine mints the value into its own store |
| `integrations.jira.token` | `Integration.spec.jira.tokenSecretRef` | `SecretKeyReference` |
| `integrations.jira.webhook_secret` | `Integration.spec.jira.webhookSecretRef` | `SecretKeyReference` |
| `integrations.confluence.token` | `Integration.spec.confluence.tokenSecretRef` | `SecretKeyReference` |
| `integrations.confluence.webhook_secret` | `Integration.spec.confluence.webhookSecretRef` | `SecretKeyReference` |
| `integrations.confluence.webhook_token` | `Integration.spec.confluence.webhookTokenSecretRef` | `SecretKeyReference` |
| `integrations.datadog.webhook_token` | `Integration.spec.datadog.webhookTokenSecretRef` | `SecretKeyReference` |
| `integrations.datadog.provisioning.api_key` | `Integration.spec.datadog.apiKeySecretRef` | `SecretKeyReference` |
| `integrations.datadog.provisioning.app_key` | `Integration.spec.datadog.appKeySecretRef` | `SecretKeyReference` |
| `integrations.atlassian.api_key` | `Integration.spec.atlassian.apiKeySecretRef` | `SecretKeyReference` |
| `providers.llm.*.cli.auth.token` | `LLMProvider.spec.cli.auth.tokenSecretRef` | `SecretKeyReference` |
| `providers.llm.*.cli.auth.credential_bundle` | `LLMProvider.spec.cli.auth.credentialBundleSecretRef` | `SecretKeyReference` |
| `providers.llm.*.cli.env.*` | `LLMProvider.spec.cli.env[].valueFrom.secretKeyRef` | one `SecretKeyReference` per variable |

A tool server's `env` and `headers` and the values of a Unit's or a Seat's
`spec.mcp` also take a setting that is no credential (a feature flag a tool
server reads, a seat's own cache path) as a literal `value`, which the
company document carries as written. Crewlet treats every entry of those
lists as a credential and masks it on every read, and cannot tell a setting
from one; the name can. So the API refuses a `value` under a name Crewlet
reads as a credential's (one of its parts, cut at every character that is not
a letter or a digit, is, in any case, `KEY`, `KEYS`, `APIKEY`, `ACCESSKEY`,
`SECRETKEY`, `TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`, `CREDENTIAL`,
`CREDENTIALS`, `AUTH`, `OAUTH` or `PAT`, as its `cliprofile.IsCredentialName`
reads one), and under the `Authorization`, `Proxy-Authorization` and `Cookie`
headers, and a test holds that rule to every name the engine reads a seat's
tool credential under. A credential under a name that says nothing is still
yours to keep in a Secret
([Settings and credentials](../guides/tool-servers.md#settings-and-credentials)).
The other lists of variables in the table, a sandbox's, its setup steps' and a
coding CLI's, take no literal value.

## Not offered in v1alpha1

| Crewlet position | Why it is not offered |
|---|---|
| `providers.sandbox.local.runtime_env.*` | A seat's code work runs in E2B alone (`Company.spec.sandbox.e2b`), and `spec.engineConfig` may not set `providers`, so no company runs a sandbox on the engine's host, whose container runtime this environment is for. |

---

## Credentials the engine provisions

An `Integration` with `provisioning: Engine` (GitLab, Mattermost, Datadog and
an Atlassian organization) has the engine create each agent seat's account at
the vendor, with the administrator credential the Integration references, and
mint the account's credential itself. That value never exists in a Secret, so
a seat names it as **provisioned** rather than with a `secretKeyRef`:

```yaml
spec:
  mcp:
    - name: gitlab
      env:
        - name: GITLAB_TOKEN
          valueFrom:
            provisioned:
              name: gitlab-token
  sandbox:
    env:
      - name: GITLAB_TOKEN
        valueFrom:
          provisioned:
            name: gitlab-token   # the same token as the tool server's
```

A provisioned value is a reference, like a Secret's, to an entry of the
engine's secret store, named for the seat's handle and the provisioned
`name` (see [below](#names-in-the-engines-secret-store)). The engine finds
the reference where its provisioning looks for a seat's credential (the
GitLab, Datadog and Atlassian tool servers' variables, a seat's Mattermost
bot token), mints the account's credential into that entry, and heals it on
every reconcile pass; the operator never writes or deletes it. Every place a
seat names one provisioned `name` is one value. Only an Integration of its
company with `provisioning: Engine` creates the value: while none does, the
seat is applied with the reference all the same, its tool or chat app starts
without the value, and the Seat reports `ResolvedRefs=False` with reason
`NoProvisioner` (which holds nothing else back). A Unit's credentials are
every member's, so they always come from Secrets.

Under `provisioning: External`, the default, the Integration holds no
administrator credential, the engine creates nothing at the vendor, and
every seat's credential is a Secret its Seat references.
[Integrations](../guides/integrations.md) says, vendor by vendor, which
variables the engine provisions.

---

## In the data pod's `crewlet.yaml`

The data pod's `crewlet.yaml` (Crewlet's Tier A) has credential positions of
its own:

| Crewlet position | Filled from |
|---|---|
| `api.auth.tokens[].token` | Secret `<company>-api-tokens`, which the operator generates once (or a platform supplies): key `operator` for the operator's own token, `access` for the one published in `status.apiAccessSecretRef` |
| `secrets.keys[].material` | Secret `<company>-keyring`, which the operator generates once (or a platform supplies): one key per keyring key, the annotation `crewlet.ai/active-key-id` naming the one that seals |
| `store.objects.s3.secret_access_key` | `Company.spec.objects.credentialsSecretRef`: `SecretReference`, key `secretAccessKey` (`S3SecretAccessKeyKey`); unset, the pod's own identity |
| `stream.token` | Not offered. The data pod runs Crewlet's embedded broker, which needs no token; an external NATS cluster (`stream.type: nats`), where Crewlet reads one, is not offered. |

Crewlet suggests a `${VAR}` at these positions, resolved from the node's
environment. The operator writes the values themselves instead, into the
`crewlet.yaml` it keeps in the Secret `<company>-bootstrap` (and the keyring
and object-store keys into each seat pool's
`<company>-bootstrap-<pool>-<hash>`, whose seat pods need them too), because the
engine also expands a `${NAME}` in the company document from its environment,
in fields that are not credentials, such as an MCP server's `url`: a
credential in the environment is one anyone who can write the document could
have the engine send anywhere
([The data pod's `crewlet.yaml`](company-document.md#the-data-pods-crewletyaml)).
A test renders each case and checks that every credential it was given sits
at one of these positions and nowhere else.

---

## Credentials the operator makes or copies itself

Some credentials belong to how the engine runs rather than to the company
document. The operator handles them without a field:

- **The secret-store keyring and the API tokens** are generated by the
  operator, once per company, into Secrets it owns in the company's
  namespace. A token for consumers of the engine's API (a control plane, a
  dashboard) is published in the Secret named by
  `Company.status.apiAccessSecretRef`; the operator uses a token of its own,
  so revoking the consumers' token never locks the operator out.
- **Object-store keys** are each company's own:
  `Company.spec.objects.credentialsSecretRef`, keys `accessKeyID` and
  `secretAccessKey`, in the company's namespace, for the class's S3 bucket.
  Issue them per company, limited to the company's prefix of the bucket. The
  operator writes them into the pod's `crewlet.yaml`, with the keyring and
  the API tokens, in the Secret it owns for the data pods,
  `<company>-bootstrap`, and into the one it owns for each seat pool,
  `<company>-bootstrap-<pool>-<hash>`. Without them the engine reaches the
  bucket with the pod's own identity: EKS Pod Identity, the node's role, or a
  workload identity bound through the ServiceAccount's annotations
  (`EngineClass.spec.serviceAccount.annotations`, such as
  `eks.amazonaws.com/role-arn` or `iam.gke.io/gcp-service-account`), which
  every company of the class shares unless the identity is scoped per
  tenant.
- **Telemetry headers** (`EngineClass.spec.telemetry.headersSecretRef`) and
  **class-wide environment variables from a Secret**
  (`EngineClass.spec.podTemplate.spec.env[].valueFrom`) are copied by the
  operator into `<company>-bootstrap` too, and the engine's environment
  reads them from there, so the values never appear in a pod template. No
  credential of the engine's own is an environment variable, since the
  engine resolves a `${NAME}` in its company document from its
  environment. The telemetry headers never reach a sandbox either: a remote
  sandbox exports its telemetry to the engine's public listener with a
  token signed for its one run (`CREWLET_SANDBOX_OTEL_RECEIVER_URL`), and
  the engine adds the headers when it forwards to the collector.
- **Platform wiring** goes in the engine's environment: what every company
  of a class shares in `EngineClass.spec.podTemplate.spec.env` (from a
  Secret in the platform's namespace), and what is one company's own in
  `Company.spec.runtime.env` (from a Secret in its namespace), which is
  merged over the class's by name. Use `valueFrom` for anything sensitive.
  Neither accepts `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`: Crewlet reads
  those as the key of a provider that names none, which would make them
  credentials outside the secret store, so a provider's key always comes
  from its own Secret reference. That environment is the engine's alone: a
  stdio tool server the engine starts is handed only the part of it
  Crewlet's allowlist names, the proxy and TLS variables among it
  ([Stdio and HTTP](../guides/tool-servers.md#stdio-and-http)), and a
  coding CLI that part and the variables its Crewlet profile passes
  through, so a setting a tool server reads goes in its `MCPServer`'s
  `env`, and one a coding CLI reads in its `LLMProvider`'s `cli.env`.

---

## Names in the engine's secret store

Each referenced key becomes one entry in the engine's encrypted secret store,
and the company document refers to it as `${NAME}`. The name is derived from
the reference:

```
K8S_<SECRET NAME>_<KEY>_<HASH>
```

The Secret's name and the key are upper-cased, with each run of other
characters, and each camelCase word boundary, turned into one underscore, and
are cut to 40 and 32 characters. The hash is twelve base32 characters of the
SHA-256 of the full reference, namespace included, so two references that
read the same — `ana-slack` and `ana.slack`, or one Secret name in two
namespaces — still have entries of their own. Key `botToken` of Secret
`ana-slack` is `K8S_ANA_SLACK_BOT_TOKEN_` and a hash, and no name is longer
than 90 characters. A reference keeps its name for as long as it exists, so
rotating a Secret rewrites an entry rather than replacing it.

> **Names of that shape are reserved.** The operator writes them with its own
> token, rewrites one that the engine lists as last written with any other
> credential (a person's session, the published access token), records a
> Warning event `SecretOverwritten` naming who, and deletes one once nothing
> references it. Who wrote an entry is what the engine records from the
> write's authentication, so nothing a writer says can make its value pass for
> the operator's. It never touches a name of any other shape, so credentials
> written into the store by hand or by Crewlet's own provisioning are left
> alone. A name the running configuration still references is kept until the
> next configuration is applied.

A seat's provisioned credential has an entry of another shape, which the
engine fills and the operator never writes or deletes:

```
PROVISIONED_<HANDLE>_<NAME>_<HASH>
```

The seat's handle and the provisioned `name` are written as above, and the
hash is twelve base32 characters of the SHA-256 of both, so two seats, or
two values of one seat, never share an entry. A seat keeps its handle for
life, so its provisioned credentials keep their entries across every render,
reconcile and restart of the operator; another name would point the seat at
an entry the engine never minted into, which it would then mint again.

The engine's listing carries names, never values, so to know which values
changed the operator keeps a fingerprint of each value it wrote, in a Secret
it owns in the company's namespace. A fingerprint is an HMAC-SHA256 of the
name and the value under a random key held in the same Secret: it confirms no
guess about a value without that key, and two names holding one value have
different fingerprints. Deleting that Secret is harmless; the operator writes
every value once more and starts a new one.

---

## See also

- [Supplying Credentials](../guides/credentials.md) — the Secret for each position, written and rotated
- [Connecting Integrations](../guides/integrations.md) — which credentials the engine provisions, vendor by vendor
- [Crewlet's secret store](https://github.com/crewlet/crewlet/blob/main/docs/concepts/secret-store.md) — where the values live in the engine
