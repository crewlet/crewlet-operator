# API Reference

## Packages
- [crewlet.ai/v1alpha1](#crewletaiv1alpha1)


## crewlet.ai/v1alpha1

Package v1alpha1 contains the v1alpha1 API of the crewlet.ai group: a
Crewlet company and its org chart as Kubernetes objects.

Each typed field maps onto one field of Crewlet's company document, which
its doc comment names; enum values are PascalCase here and Crewlet's own
spelling there. Engine tuning without a typed field passes through
Company.spec.engineConfig.

No field holds a credential. Every position Crewlet marks as a credential in
the company document is filled from a Secret through a SecretKeyReference or
a SecretReference (NamespacedSecretKeyReference and NamespacedSecretReference
on the cluster-scoped EngineClass), or, for a seat's account the engine
creates under an Integration with provisioning Engine, is a value the engine
mints into its own store (ProvisionedCredential). docs/concepts/credentials.md
in the repository maps every position to its field.


### Resource Types
- [Company](#company)
- [EngineClass](#engineclass)
- [Integration](#integration)
- [LLMProvider](#llmprovider)
- [MCPServer](#mcpserver)
- [Schedule](#schedule)
- [Seat](#seat)
- [Unit](#unit)



#### APIExposure



APIExposure is how a company's engine API is reached. Every request needs
a credential, but for the probes, the vendors' webhooks and a sandbox's
per-run endpoints: a person's session, signed in at the dashboard, or a
bearer token, such as the one the operator generates per company and
publishes in a Secret named by the Company's status.apiAccessSecretRef.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ExposureSettings](#exposuresettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[APIExposureMode](#apiexposuremode)_ | mode is how the API is reached. | ClusterInternal | Enum: [ClusterInternal Gateway Ingress] <br />Optional: \{\} <br /> |
| `gateway` _[GatewayExposure](#gatewayexposure)_ | gateway is the Gateways to attach to, for mode Gateway. |  | Optional: \{\} <br /> |
| `ingress` _[IngressExposure](#ingressexposure)_ | ingress is the Ingress settings, for mode Ingress. |  | Optional: \{\} <br /> |
| `hostname` _string_ | hostname is the API's host name, as a template with the same<br />variables and rules as webhooks.hostname. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### APIExposureMode

_Underlying type:_ _string_

APIExposureMode is how a company's engine API is reached from outside its
namespace.

_Validation:_
- Enum: [ClusterInternal Gateway Ingress]

_Appears in:_
- [APIExposure](#apiexposure)

| Field | Description |
| --- | --- |
| `ClusterInternal` | APIExposureClusterInternal reaches the API only through the company's<br />Service, from inside the cluster.<br /> |
| `Gateway` | APIExposureGateway also routes a host name to the API with an<br />HTTPRoute. Attach it to an internal Gateway: the API includes the<br />company's configuration and secret store.<br /> |
| `Ingress` | APIExposureIngress does the same with an Ingress.<br /> |


#### AppliedObjectCounts



AppliedObjectCounts is how many objects of each kind the applied revision
was rendered from.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ConfigStatus](#configstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `units` _integer_ | units is the number of Units. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `seats` _integer_ | seats is the number of Seats. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `llmProviders` _integer_ | llmProviders is the number of model providers, platform providers<br />included. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `mcpServers` _integer_ | mcpServers is the number of MCPServers. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `integrations` _integer_ | integrations is the number of Integrations. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `schedules` _integer_ | schedules is the number of Schedules. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### AtlassianIntegration



AtlassianIntegration connects a company's Atlassian Cloud organization, in
which the engine creates a service account for each agent seat, grants it
the company's Jira and Confluence sites, and mints its API token. It maps
onto Crewlet's integrations.atlassian. Each seat names the minted token and
the account's address as provisioned values of its MCPServer named
atlassian, so the Integration's provisioning is Engine. A Data Center
deployment has no organization; its accounts are made by hand and each
seat's token comes from a Secret.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `orgID` _string_ | orgID is the organization's id, from admin.atlassian.com. Crewlet's<br />`org_id`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Required: \{\} <br /> |
| `apiKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | apiKeySecretRef is the Secret key holding an organization API key<br />created without scopes: Atlassian refuses a scoped key the call that<br />creates an account. Crewlet's `api_key`, a credential position. |  | Required: \{\} <br /> |


#### CLIAgentAuth



CLIAgentAuth is how a CLIAgent provider signs in. It maps onto Crewlet's
cli.auth.

A subscription login is made at a terminal, by `crewlet llm login` or the
CLI's own command, and a pod has no terminal: Subscription signs in with a
headless token, such as the CLAUDE_CODE_OAUTH_TOKEN `claude setup-token`
mints, or with a credential bundle `crewlet llm export` wrote from a login
made elsewhere. A bundle is restored only while the provider's login on the
data volume is empty, so the refreshed login a running engine keeps there
wins over the bundle; a token never needs refreshing, which makes it the
better of the two wherever the CLI mints one. A provider names one of the
two: given both, which login the CLI acts on would be the CLI's choice
between its environment and its own files, which nothing here can see.

Crewlet hands a CLI a token or a key only through the variable its profile
names, and refuses one for a CLI whose profile names none, since it would
reach nothing: so CLIAgentSettings holds tokenSecretRef to the CLIs with a
token variable and mode APIKey to the CLIs with a key variable.

Hermes, Pi and OpenCode may sign in to a Subscription with neither: each
fronts many model providers and reads each provider's key from that
provider's own variable, so Crewlet's profile for it names no key variable
and marks it as signing in through its environment (env_sign_in), and
Crewlet documents the provider's key in the cli block's env as its metered
sign-in, and hands that env to the CLI untouched. Crewlet counts an entry of
env as a sign-in only when its name says it holds a credential: one of
its parts between underscores is, in any case, KEY, KEYS, APIKEY,
ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
CREDENTIALS, AUTH, OAUTH or PAT (cliprofile.IsCredentialName). So
OPENROUTER_API_KEY signs such a CLI in and HTTPS_PROXY or
HERMES_MAX_TOKENS does not, and one whose env holds no such name is
signed in to nothing, and refused. Every other CLI keeps to a token or a
bundle: one with a key variable takes its key through mode APIKey, and
Crewlet refuses that variable in env, which auth owns. The rule is
CLIAgentSettings's, which sees the agent and env beside auth.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CLIAgentSettings](#cliagentsettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[CLIAgentAuthMode](#cliagentauthmode)_ | mode is how the CLI signs in. Unset is Subscription. Crewlet's<br />`mode`. |  | Enum: [Subscription APIKey] <br />Optional: \{\} <br /> |
| `tokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | tokenSecretRef is the Secret key holding a long-lived subscription<br />token the CLI mints for headless use. Crewlet's `token`, a credential<br />position. |  | Optional: \{\} <br /> |
| `credentialBundleSecretRef` _[SecretKeyReference](#secretkeyreference)_ | credentialBundleSecretRef is the Secret key holding a credential<br />bundle `crewlet llm export` wrote. Crewlet's `credential_bundle`, a<br />credential position. |  | Optional: \{\} <br /> |


#### CLIAgentAuthMode

_Underlying type:_ _string_

CLIAgentAuthMode is how a CLIAgent provider authenticates. It maps onto
Crewlet's cli.auth.mode (Subscription is `subscription`, APIKey is
`api-key`).

Crewlet's `inherit-env`, which hands the CLI whichever token or key the
engine's own environment holds, is not offered: a credential in the
engine's environment is outside the encrypted store, readable by every
process the engine runs, and resolvable by a ${NAME} written in any field of
the company document, so it would be the one credential the API takes by
another route than a Secret reference. Subscription and APIKey cover what
it is for.

_Validation:_
- Enum: [Subscription APIKey]

_Appears in:_
- [CLIAgentAuth](#cliagentauth)

| Field | Description |
| --- | --- |
| `Subscription` | CLIAgentAuthSubscription signs the CLI in to a subscription with<br />tokenSecretRef or credentialBundleSecretRef, or signs Hermes, Pi or<br />OpenCode in with its model provider's key in the cli block's env.<br /> |
| `APIKey` | CLIAgentAuthAPIKey gives the CLI the provider's apiKeySecretRefs<br />key, in the key variable Crewlet's profile for it names, billing the<br />metered account. CursorAgent's key is a Cursor account's API key<br />(CURSOR_API_KEY), not a model provider's.<br /> |


#### CLIAgentMode

_Underlying type:_ _string_

CLIAgentMode is how the engine uses the CLI. It maps onto Crewlet's
cli.mode (Text is `text`, Agent is `agent`).

_Validation:_
- Enum: [Text Agent]

_Appears in:_
- [CLIAgentSettings](#cliagentsettings)

| Field | Description |
| --- | --- |
| `Text` | CLIAgentModeText drives the CLI as a text model behind the engine's<br />own tool loop, as a process in the engine's pod.<br /> |
| `Agent` | CLIAgentModeAgent runs a seat's executor as the CLI's own agentic<br />run, in a sandbox, with the seat's tools reached back through the<br />engine's tool bridge. It is offered for the CLIs Crewlet has a<br />sandbox runner for, ClaudeCode and OpenCode.<br /> |


#### CLIAgentName

_Underlying type:_ _string_

CLIAgentName is which coding CLI a CLIAgent provider drives. It maps onto
Crewlet's cli.agent (ClaudeCode is `claude-code`, Codex is `codex`,
GeminiCLI is `gemini-cli`, QwenCode is `qwen-code`, OpenCode is `opencode`,
CursorAgent is `cursor-agent`, Copilot is `copilot`, Grok is `grok`,
MuseCode is `muse-code`, KimiCode is `kimi-code`, Hermes is `hermes`, Pi is
`pi`). Crewlet's `custom`, a CLI described wholly by profile overrides, is
not offered: the overrides name the binary and the arguments the engine's
pod runs.

_Validation:_
- Enum: [ClaudeCode Codex GeminiCLI QwenCode OpenCode CursorAgent Copilot Grok MuseCode KimiCode Hermes Pi]

_Appears in:_
- [CLIAgentSettings](#cliagentsettings)

| Field | Description |
| --- | --- |
| `ClaudeCode` |  |
| `Codex` |  |
| `GeminiCLI` |  |
| `QwenCode` |  |
| `OpenCode` |  |
| `CursorAgent` |  |
| `Copilot` |  |
| `Grok` |  |
| `MuseCode` |  |
| `KimiCode` |  |
| `Hermes` |  |
| `Pi` |  |


#### CLIAgentRunIn

_Underlying type:_ _string_

CLIAgentRunIn is where an Agent-mode run happens. It maps onto Crewlet's
cli.run_in (E2B is `e2b`). Crewlet's local cells, `direct` and `container`,
are not offered, as the Company's spec.sandbox does not offer them.

_Validation:_
- Enum: [E2B]

_Appears in:_
- [CLIAgentSettings](#cliagentsettings)

| Field | Description |
| --- | --- |
| `E2B` | CLIAgentRunInE2B is the Company's E2B sandbox.<br /> |


#### CLIAgentSettings



CLIAgentSettings is the coding CLI a CLIAgent provider drives. It maps onto
Crewlet's providers.llm entry's cli block.

The CLI is a binary in the engine's image, which the official image does
not ship: a class running such a provider names an image derived from the
engine's with the CLI installed (docs/guides/cli-agents.md). Its login, and
the per-seat homes the CLI keeps, are on the data volume, so a restart or a
new pod keeps them.

Its rules hold every credential to a variable Crewlet's profile for the
CLI reads, as Crewlet's own admission does (providers.llm.*.cli): the
engine refuses a write that names a credential its CLI would never be
given, so the API refuses it on the object instead. The lists of CLIs the
rules name are those whose profiles set token_env, api_key_env or
env_sign_in in the engine the operator is tested against, Crewlet's main;
a test holds them to that engine's own verdict.

Mode Agent runs the CLIs Crewlet has a sandbox runner for, ClaudeCode and
OpenCode: the Company's coding agents. Crewlet hands an Agent-mode run its
provider's model exactly as the provider's text calls pass it (the
reviewer's, a delegated worker's, the summariser's), so one model serves
every phase. The run happens in the Company's E2B sandbox, which never
receives a credential bundle: the bundle's login carries a refresh token
the engine keeps rotating on its data volume. So a Subscription login
reaches the run only as a variable of its environment, which for ClaudeCode
is its headless token and for OpenCode its model provider's key in env.



_Appears in:_
- [LLMProviderSpec](#llmproviderspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `agent` _[CLIAgentName](#cliagentname)_ | agent is which CLI to drive. The provider's model is written as this<br />CLI reads it, and an Agent-mode run is handed it unchanged: OpenCode<br />reads `<provider>/<model>`, such as `anthropic/claude-sonnet-5`.<br />Crewlet's `agent`. |  | Enum: [ClaudeCode Codex GeminiCLI QwenCode OpenCode CursorAgent Copilot Grok MuseCode KimiCode Hermes Pi] <br />Required: \{\} <br /> |
| `mode` _[CLIAgentMode](#cliagentmode)_ | mode is how the engine uses the CLI. Unset is Text. Crewlet's `mode`. |  | Enum: [Text Agent] <br />Optional: \{\} <br /> |
| `runIn` _[CLIAgentRunIn](#cliagentrunin)_ | runIn is where an Agent-mode run happens. Unset is the Company's<br />sandbox, which is E2B. Crewlet's `run_in`. |  | Enum: [E2B] <br />Optional: \{\} <br /> |
| `auth` _[CLIAgentAuth](#cliagentauth)_ | auth is how the CLI signs in. |  | MinProperties: 1 <br />Required: \{\} <br /> |
| `env` _[SecretEnvVar](#secretenvvar) array_ | env is extra environment of every invocation of the CLI: the child<br />gets an allowlisted environment, not the engine's, so a variable the<br />CLI needs beyond that is given here. It is how Hermes, Pi and OpenCode<br />sign in under auth mode Subscription with no login of their own: their<br />model provider's key, under a name Crewlet reads as a credential, such<br />as OPENROUTER_API_KEY. It may not set the CLI's own token or key<br />variable, which auth owns. An OpenCode provider's credential-named<br />variables also travel into every box of an Agent-mode run, and of a<br />seat's code work that runs on it, where the coding agent can read<br />them. Crewlet's `env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds caps one invocation of the CLI, launch included. Unset<br />takes Crewlet's default of 300. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxConcurrent` _integer_ | maxConcurrent is how many CLI processes the provider runs at once;<br />each takes 200 to 400 MB of the engine pod's memory. Unset takes<br />Crewlet's default of 4. Crewlet's `max_concurrent`. |  | Maximum: 64 <br />Minimum: 1 <br />Optional: \{\} <br /> |
| `loginName` _string_ | loginName names the login, and the per-seat homes, the provider<br />keeps on the data volume. Providers of the company with the same<br />loginName share one login, which is how several models run off one<br />subscription; they must drive the same CLI. Unset is the provider's<br />own metadata.name. Crewlet's `state_dir`, as<br />/var/lib/crewlet/llm-cli/<loginName>. |  | MaxLength: 63 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9-]*$` <br />Optional: \{\} <br /> |


#### ClassEnvVar



ClassEnvVar is an environment variable an EngineClass sets on the engine
pods of every Company of the class: platform wiring such as the address of
the platform's own API. A value from a Secret is copied by the operator into
a Secret it owns in each Company's namespace and referenced from the pod, as
telemetry headers are, so the value never appears in the pod template and the
platform's Secret never has to be readable from a tenant namespace. The
value is in every tenant's engine pod all the same, where code the tenant
runs can read it: put nothing here a tenant must not hold.



_Appears in:_
- [EnginePodSpec](#enginepodspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `value` _string_ | value is the variable's literal value. An empty string is a valid<br />value, which is why the field is a pointer. |  | MaxLength: 4096 <br />Optional: \{\} <br /> |
| `valueFrom` _[NamespacedSecretValueSource](#namespacedsecretvaluesource)_ | valueFrom reads the value from a Secret key in a namespace the<br />platform controls. |  | Optional: \{\} <br /> |


#### ClaudeModelID

_Underlying type:_ _string_

ClaudeModelID is one id of Crewlet's table of Claude models, which records
the request shape each model accepts (its thinking mode, its effort levels,
whether it takes a sampling parameter, its output cap). An Anthropic
provider whose model is a gateway alias names the row it is an alias of.
Crewlet refuses on every write a claude_model its table does not list,
exactly as written, so the values are the table's ids in the engine the
operator is tested against, Crewlet's main; a test holds them to that
engine's refusal.

_Validation:_
- Enum: [claude-fable-5-1 claude-mythos-5-1 claude-fable-5 claude-mythos-5 claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4-7 claude-sonnet-5-5 claude-sonnet-5 claude-opus-4-6 claude-sonnet-4-6 claude-opus-4-5 claude-sonnet-4-5 claude-haiku-4-5 claude-opus-4-1 claude-opus-4-0 claude-opus-4 claude-sonnet-4-0 claude-sonnet-4 claude-3-7-sonnet]

_Appears in:_
- [LLMProviderSettings](#llmprovidersettings)
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)



#### CodingAgent

_Underlying type:_ _string_

CodingAgent is the coding agent a sandbox runs. It maps onto Crewlet's
coding agents (ClaudeCode is `claude-code`, OpenCode is `opencode`).

_Validation:_
- Enum: [ClaudeCode OpenCode]

_Appears in:_
- [SandboxSettings](#sandboxsettings)
- [SeatSandbox](#seatsandbox)

| Field | Description |
| --- | --- |
| `ClaudeCode` | CodingAgentClaudeCode is Claude Code.<br /> |
| `OpenCode` | CodingAgentOpenCode is OpenCode.<br /> |


#### Company



Company is one Crewlet company: the operator runs an engine for it and keeps
the engine's company document equal to what the Company and the objects that
name it declare.

The Company's name becomes part of the names of the objects the operator
creates for it (the data StatefulSet and the engine Service), so it must be
a DNS label of at most 47 characters: a StatefulSet name longer than 52
characters overflows its pods' controller-revision-hash label.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Company` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[CompanySpec](#companyspec)_ | spec is the company's desired state. |  | Required: \{\} <br /> |
| `status` _[CompanyStatus](#companystatus)_ | status is the company's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### CompanyEndpoints



CompanyEndpoints is where a company is reached. Every address is a base URL
with its scheme, so that a caller appends a path to it and never
reconstructs one from the EngineClass's templates.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyStatus](#companystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `api` _string_ | api is the in-cluster base URL of the engine's API: the company's<br />Service, for callers inside the cluster. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `exposedAPI` _string_ | exposedAPI is the base URL of the engine's API through the class's<br />exposure.api, for callers outside the cluster: https://<hostname><br />when the route terminates TLS (the Ingress has tls, or the Gateway<br />listener the route attaches to is HTTPS), http://<hostname> otherwise,<br />with the hostname template expanded. Set only while exposure.api.mode<br />is Gateway or Ingress and the route has been accepted. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `publicBaseURL` _string_ | publicBaseURL is the address vendors deliver webhooks to, when the<br />company has one: spec.exposure.publicBaseURL, or https://<hostname><br />from the class's webhooks.hostname. It reaches the engine's public<br />listener, and every vendor route is a path under it, such as<br />/webhooks/forge for an Atlassian Forge relay. Unset while the class's<br />webhooks.mode is None, which exposes nothing, and under External<br />before the Company states its address: the engine is then told the<br />listener's address inside the cluster, http://<company>-engine.<br /><namespace>.svc:8081, which no vendor can reach. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dashboardBaseURL` _string_ | dashboardBaseURL is where a browser reaches the engine, its dashboard<br />and its people's sign-in: spec.exposure.dashboardBaseURL, else the<br />address of the class's API route, on the scheme exposedAPI takes, else<br />http://localhost:8080, the address kubectl port-forward serves the<br />engine Service's API port at. It does not wait for the route to be<br />accepted, as exposedAPI does: it is the engine's api.external_url,<br />which the engine reads only at start, and a flip of acceptance would<br />otherwise restart the data pod. The engine builds every link a person<br />follows on it, an invitation's included, so with the third these lead<br />to a localhost only a port-forwarding browser reaches. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### CompanyExposure



CompanyExposure overrides how a company is reached.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `publicBaseURL` _string_ | publicBaseURL is the HTTPS address vendors deliver webhooks to, and a<br />remote sandbox calls the engine back on, overriding the one the class's<br />webhooks.hostname derives. Required when the class's webhooks.mode is<br />External, where something outside the operator (a relay, an API<br />gateway) forwards it to the engine's public listener. A trailing '/'<br />is dropped, as Crewlet drops it. Crewlet's api.public.external_url,<br />in the data pod's crewlet.yaml, which the engine reads only at start:<br />changing it restarts the data pod. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dashboardBaseURL` _string_ | dashboardBaseURL is where a browser reaches the engine: its dashboard,<br />and the sign-in of the company's people. It overrides the address of<br />the class's API route: set it when the class routes no API, or people<br />reach it through something in front of the route (a single sign-on<br />proxy, say). Crewlet's api.external_url, in the data pod's<br />crewlet.yaml, which the engine reads only at start: changing it<br />restarts the data pod. The engine takes from it the origin a person's<br />write must come from, whether the session cookie is Secure (https)<br />and its scope, and the base of every link a person follows: a link an<br />agent composes, an invitation (/dashboard#/invite/...), a password<br />link. An address other than the route's must serve the engine, its<br />/dashboard and its /auth routes included. The route, when the class<br />has one, stays an origin the engine accepts. It is never the public<br />address: the public listener serves no dashboard. A trailing '/' is<br />dropped. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### CompanyIntegrationSettings



CompanyIntegrationSettings is company-wide behaviour of the integration
reconcile loop. The vendors themselves are Integration objects.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `checkIntervalSeconds` _integer_ | checkIntervalSeconds is how long a working integration is trusted<br />before the engine reads it back, and so how long access revoked at the<br />vendor goes unnoticed. Unset takes Crewlet's default of 600. Crewlet's<br />integrations.check_interval_seconds. |  | Minimum: 60 <br />Optional: \{\} <br /> |


#### CompanyLLMSettings



CompanyLLMSettings is how a company chooses among its model providers.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `order` _[LLMProviderReference](#llmproviderreference) array_ | order is the providers' precedence. A seat with no default chain runs<br />on the first provider here, and on it alone: Crewlet tries no other<br />provider of the order when its keys are all cooling down, so give a<br />seat a chain for a fallback. The providers the order leaves out follow<br />it: the company's LLMProviders by name, then the platform providers by<br />name. Crewlet's providers.llm_order. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `usePlatformProviders` _boolean_ | usePlatformProviders adds the EngineClass's platformLLMProviders to the<br />company, so seats can run on models the platform provides. Their keys<br />are pushed into the company's engine, where the tenant can read them<br />(see the class's platformLLMProviders). |  | Optional: \{\} <br /> |


#### CompanyObjects



CompanyObjects is a company's own access to its class's object store.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `credentialsSecretRef` _[SecretReference](#secretreference)_ | credentialsSecretRef is a Secret, in the Company's namespace, holding<br />the keys accessKeyID and secretAccessKey (S3AccessKeyIDKey,<br />S3SecretAccessKeyKey) the engine reaches its class's S3 bucket with:<br />Crewlet's store.objects.s3.access_key_id and secret_access_key. Used<br />only while the class's objects.backend is S3.<br />Issue them per company, with a policy that admits the company's own<br />prefix of the bucket only (the class's objects.s3.prefix, expanded):<br />they are written into the engine pod's crewlet.yaml, and code the<br />tenant runs in the pod (a stdio MCP server, a sandbox setup command)<br />can read them. Unset uses the pod's own credentials through the SDK's<br />default chain: a workload identity bound through the class's<br />serviceAccount.annotations, which every company of the class shares<br />unless it is scoped per tenant. |  | Optional: \{\} <br /> |


#### CompanyReference



CompanyReference names the Company a namespaced object belongs to. The
Company is always in the referring object's own namespace: an object can be
part of exactly one company, and a reference across namespaces would let one
tenant attach objects to another's company.



_Appears in:_
- [IntegrationSpec](#integrationspec)
- [LLMProviderSpec](#llmproviderspec)
- [MCPServerSpec](#mcpserverspec)
- [ScheduleSpec](#schedulespec)
- [SeatSpec](#seatspec)
- [UnitSpec](#unitspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of a Company in this object's namespace. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### CompanyRuntime



CompanyRuntime is a company's bounded overrides of its class.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `version` _string_ | version replaces the tag of the class's engine image, for example<br />v1.2.4. It is honoured only when the class sets<br />engine.allowVersionOverride; the operator refuses a version older than<br />the one the company's data was last written by. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9_][A-Za-z0-9_.-]\{0,127\}$` <br />Optional: \{\} <br /> |
| `env` _[EnvVar](#envvar) array_ | env adds environment variables to the engine container, for platform<br />wiring the engine reads from its environment. They are merged over the<br />class's podTemplate.spec.env by name: a variable here replaces the<br />class's variable of the same name.<br />Names starting with CREWLET_ or OTEL_ are the operator's own.<br />ANTHROPIC_API_KEY and OPENAI_API_KEY are refused too: Crewlet reads them<br />as the key of a model or embeddings provider that names none, so a<br />value here would be a credential outside the engine's secret store;<br />give the provider its key with apiKeySecretRefs or apiKeySecretRef. |  | MaxItems: 32 <br />Optional: \{\} <br /> |


#### CompanySpec



CompanySpec is a company: the engine that runs it and the company-wide part
of its document. Its units, seats, model providers, tool servers and
integrations are objects of their own that name the Company in their
spec.companyRef.



_Appears in:_
- [Company](#company)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `engineClassName` _string_ | engineClassName is the EngineClass the company runs with. Unset uses<br />the class annotated crewlet.ai/is-default-class=true.<br />Whoever may write a Company may name any class, including one that<br />offers platformLLMProviders or larger limits; a class admits only the<br />namespaces its spec.allowedNamespaces selects, and a Company in any<br />other is not accepted (ClassNotAllowed). |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `companyName` _string_ | companyName is the company's name. Crewlet derives every seat's<br />identity from it and the seat's handle, so it cannot be changed: a new<br />name would orphan every seat's memory. Crewlet's `name`. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `mission` _string_ | mission is one line on what the company is for; every seat's prompt<br />carries it. Crewlet's `mission`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `vision` _string_ | vision is a longer statement of where the company is going. Crewlet's<br />`vision`. |  | MaxLength: 8192 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `policies` _string array_ | policies are company-wide rules every seat's prompt carries. Crewlet's<br />`policies`. |  | MaxItems: 64 <br />items:MaxLength: 2048 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `timeZone` _string_ | timeZone is the company's clock, an IANA time zone such as<br />Europe/Berlin: where its days begin for budgets, due dates and<br />schedules. Unset is UTC. Crewlet's `timezone`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$` <br />Optional: \{\} <br /> |
| `tracker` _[TrackerSettings](#trackersettings)_ | tracker is which work tracker the company runs. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `knowledge` _[KnowledgeSettings](#knowledgesettings)_ | knowledge is which knowledge base the company runs. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `tokenBudget` _[TokenBudget](#tokenbudget)_ | tokenBudget caps the tokens all seats together may spend per window.<br />Crewlet's `token_budget`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `llm` _[CompanyLLMSettings](#companyllmsettings)_ | llm is how the company chooses among its model providers. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `embeddings` _[EmbeddingSettings](#embeddingsettings)_ | embeddings is the provider vectors are computed with. Unset leaves<br />search keyword-only. Crewlet's providers.embeddings. |  | Optional: \{\} <br /> |
| `sandbox` _[SandboxSettings](#sandboxsettings)_ | sandbox is where seats run code. Unset gives no seat a sandbox.<br />Crewlet's providers.sandbox. |  | Optional: \{\} <br /> |
| `integrations` _[CompanyIntegrationSettings](#companyintegrationsettings)_ | integrations is company-wide integration behaviour. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `engineConfig` _[EngineConfigEntry](#engineconfigentry) array_ | engineConfig is passed through into the company document as written,<br />for the engine's tuning blocks that have no field here: turn_engine,<br />learning, scheduling, workers, skill_variables and the<br />notification_* settings, spelled as Crewlet spells them. Each entry is<br />one top-level key of the document and its value. The operator does not<br />interpret it; the engine validates it in the dry run before every<br />apply, and a refusal is reported on the Company.<br />It may not set what another field or object of this API owns: name,<br />mission, vision, policies, timezone, tracker, knowledge, token_budget,<br />providers, integrations, mcp_servers, roles or units. Every position<br />Crewlet holds a credential in lives under one of those keys, so<br />engineConfig can never carry a credential: the API server refuses a<br />Company whose engineConfig names one of them, before anything is<br />stored. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `config` _[ConfigSettings](#configsettings)_ | config is how the operator manages the company's document. | \{ managementPolicy:Enforce \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `topology` _[CompanyTopology](#companytopology)_ | topology is how the engine is laid out in pods. | \{ data:map[replicas:1] \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `exposure` _[CompanyExposure](#companyexposure)_ | exposure overrides how the company is reached. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `runtime` _[CompanyRuntime](#companyruntime)_ | runtime is the company's bounded overrides of its class. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `objects` _[CompanyObjects](#companyobjects)_ | objects is the company's own access to its class's object store. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `suspend` _boolean_ | suspend stops the engine and keeps its data volumes; setting it back<br />to false starts it again. It takes effect whatever else is wrong with<br />the Company, even while it is not Accepted. Meanwhile the Company<br />reports Ready=False with the reason Suspended, and vendors keep<br />delivering webhooks, some disabling a subscription after a long run of<br />failed deliveries. |  | Optional: \{\} <br /> |
| `deletionPolicy` _[DeletionPolicy](#deletionpolicy)_ | deletionPolicy is what happens to the data volumes when the Company is<br />deleted. With Retain the keyring Secret is kept as well, since the<br />volumes cannot be read without it. | Retain | Enum: [Retain Delete] <br />Optional: \{\} <br /> |


#### CompanyStatus



CompanyStatus is what the operator observed about a Company.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Company](#company)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Ready (the summary), Accepted, RuntimeReady,<br />SecretsSynced, ConfigApplied and Exposed, which Ready summarizes, and<br />EngineHealthy, IdentityClaimed and SeatsPlaced, which it does not. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `engine` _[EngineStatus](#enginestatus)_ | engine is what runs the company. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `topology` _[TopologyStatus](#topologystatus)_ | topology is the state of the company's pods. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `config` _[ConfigStatus](#configstatus)_ | config is the revision the engine runs. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `endpoints` _[CompanyEndpoints](#companyendpoints)_ | endpoints is where the company is reached. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiAccessSecretRef` _[SecretReference](#secretreference)_ | apiAccessSecretRef names the Secret, in the Company's namespace, that<br />holds a bearer token for the engine's API under the key token<br />(APIAccessTokenKey) and the API's in-cluster base URL under the key<br />url (APIAccessURLKey). It is for the company's consumers (a control<br />plane, a dashboard); the operator uses a token of its own, so revoking<br />this one never locks the operator out.<br />The token holds Crewlet's admin grant, which takes in every app, so<br />whoever holds it can rewrite the company document where the operator<br />does not manage it, reveal every value in the engine's secret store<br />(the class's platform model keys included: it is a deployment's own<br />token, not a person's machine token, which Crewlet refuses a reveal),<br />and invite or create the company's people. Grant read on this Secret<br />only to whoever may do that, by resourceNames: read on every Secret of<br />the namespace also exposes <company>-api-tokens, <company>-keyring and<br /><company>-bootstrap. |  | Optional: \{\} <br /> |


#### CompanyTopology



CompanyTopology is how a company's engine is laid out in pods: one data pod
that holds the estate, serves the API and runs the background workers, and
the seat pools, stateless pods that run the seats placed in them. Without
seat pools every seat runs on the data pod.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `data` _[DataTopology](#datatopology)_ | data is the pods that hold the estate. | \{ replicas:1 \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `defaultSeatPool` _string_ | defaultSeatPool is the pool of every agent seat whose Seat names no<br />spec.placement.pool: data, or the name of a Shared pool of seatPools. | data | MaxLength: 20 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |
| `seatPools` _[SeatPool](#seatpool) array_ | seatPools are stateless pods that run seats. They need the class's<br />spec.seatPools.policy Allowed, and together run at most its maxPods<br />pods. |  | MaxItems: 8 <br />Optional: \{\} <br /> |


#### ConfigSettings



ConfigSettings is how the operator manages a company's document.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementPolicy` _[ManagementPolicy](#managementpolicy)_ | managementPolicy says who owns the document. Under Enforce the data<br />pod's crewlet.yaml sets Crewlet's api.auth.company_writers to the<br />operator's own token, so the engine refuses every other credential a<br />change to the document; under Observe and Seed it sets none, and<br />Crewlet's authority table decides who may. The engine reads<br />crewlet.yaml only at start, so changing the policy to or from Enforce<br />restarts the data pod. | Enforce | Enum: [Enforce Observe Seed] <br />Optional: \{\} <br /> |


#### ConfigStatus



ConfigStatus is the revision of the company document the engine runs.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyStatus](#companystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `revisionID` _string_ | revisionID is the engine's id of the last revision the operator saw<br />run the rendered document: the one it applied, one the engine<br />activated with the same content (a reload), or, under Observe, the<br />one it found in sync. Under Seed it is the revision the document was<br />seeded as. |  | MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `epoch` _integer_ | epoch is the engine's activation counter for that revision. The<br />engine applies a revision asynchronously; it runs it once the applied<br />epoch its /health reports has reached this. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `appliedHash` _string_ | appliedHash is the hash of the rendered document and of the values<br />of the Secret keys it references, as last applied: a change to<br />either is applied as a new revision, since the engine reads a<br />rotated value only when a revision activates. The values enter it<br />only as keyed fingerprints, so it reveals nothing about them. |  | MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `appliedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#time-v1-meta)_ | appliedAt is when the operator last applied a revision. |  | Optional: \{\} <br /> |
| `objects` _[AppliedObjectCounts](#appliedobjectcounts)_ | objects counts the objects the revision was rendered from. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `placement` _[RevisionPlacement](#revisionplacement)_ | placement is where the revision the engine runs pins its agent seats,<br />read from the engine's document under every management policy. The<br />operator keeps a seat pool's pods running while the running revision<br />pins a seat to them, even after the pool or the seat's placement is<br />gone from the spec: a pod stopped while its seat is still pinned to<br />it would leave the seat held by nobody, and a blocked document keeps<br />the last good revision running. Once a revision that pins nothing to<br />them runs, the pods are deleted. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### ConfluenceIntegration



ConfluenceIntegration turns Confluence on for a company: page changes wake
the seats they concern, and the company may run Confluence as its knowledge
base (the Company's spec.knowledge.backend Confluence). It maps onto
Crewlet's integrations.confluence. Each seat's Confluence identity is its
own token, a credential of the MCPServer named atlassian in its Seat's
spec.mcp.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | url is a Data Center instance's or a Cloud site's base URL. Unset<br />with cloudID unset, the company's Atlassian Integration discovers the<br />site. Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `cloudID` _string_ | cloudID is an Atlassian Cloud site's id; requests go through the API<br />gateway of the Cloud siteURL is on: Atlassian Government Cloud's,<br />api.atlassian-us-gov-mod.com, for a site under<br />atlassian-us-gov-mod.net, and api.atlassian.com otherwise. Crewlet's<br />`cloud_id`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Optional: \{\} <br /> |
| `siteURL` _string_ | siteURL is the site's address as a browser opens it, for the links<br />the engine hands people; needed with cloudID, which is read through<br />the gateway of this site's Cloud. Unset is url. Crewlet's<br />`site_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `email` _string_ | email is the account's address, for Confluence Cloud's basic<br />authentication. Unset sends the token as a bearer token. Crewlet's<br />`email`. |  | MaxLength: 254 <br />MinLength: 3 <br />Pattern: `^[^@\s]+@[^@\s]+$` <br />Optional: \{\} <br /> |
| `tokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | tokenSecretRef is the Secret key holding the API token (Cloud) or<br />personal access token (Data Center) of the account a seat without a<br />Confluence credential of its own searches as, which also reads the<br />tool-skill pages. Crewlet's `token`, a credential position. |  | Required: \{\} <br /> |
| `webhookSecretRef` _[SecretKeyReference](#secretkeyreference)_ | webhookSecretRef is the Secret key holding the secret a Data Center<br />instance signs webhook deliveries with. Required for Data Center.<br />Crewlet's `webhook_secret`, a credential position. |  | Optional: \{\} <br /> |
| `webhookTokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | webhookTokenSecretRef is the Secret key holding the token a Confluence<br />Cloud webhook carries in its delivery URL, Cloud signing nothing: at<br />least 26 characters, random. Crewlet's `webhook_token`, a credential<br />position. |  | Optional: \{\} <br /> |


#### ContainerWebhookMode

_Underlying type:_ _string_

ContainerWebhookMode is how webhooks are registered at GitHub: once on the
organization, or once per repository. It maps onto Crewlet's org_webhook
(Auto is `auto`, Require is `true`, Never is `false`).

_Validation:_
- Enum: [Auto Require Never]

_Appears in:_
- [GitHubProvisioning](#githubprovisioning)
- [GitLabEngineProvisioning](#gitlabengineprovisioning)

| Field | Description |
| --- | --- |
| `Auto` | ContainerWebhookAuto uses one organization hook where the token may<br />create one, and per-repository hooks otherwise.<br /> |
| `Require` | ContainerWebhookRequire demands an organization hook.<br /> |
| `Never` | ContainerWebhookNever always registers per-repository hooks.<br /> |


#### DataPodSettings



DataPodSettings sizes the pods that hold a company's estate. In v1alpha1 a
company runs exactly one data pod, which also serves the API, runs the
background workers and runs every seat placed in the pool data.



_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `allowedReplicas` _integer array_ | allowedReplicas is the set of data-pod counts a Company of this class<br />may choose in spec.topology.data.replicas. v1alpha1 runs exactly one<br />data pod, so the only value admitted is 1; 3 and 5, a highly available<br />estate, are added when the operator can run them. | [1] | MaxItems: 1 <br />MinItems: 1 <br />items:Enum: [1] <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#resourcerequirements-v1-core)_ | resources are the compute resources of the engine container. |  | Optional: \{\} <br /> |
| `storage` _[DataStorage](#datastorage)_ | storage is the data volume each data pod gets. |  | Required: \{\} <br /> |
| `terminationGracePeriodSeconds` _integer_ | terminationGracePeriodSeconds is how long a data pod may take to stop.<br />The engine finishes running turns before it exits and puts no bound on<br />that itself, so the default is five minutes, which covers a long turn,<br />rather than the Kubernetes default of thirty seconds, which does not. | 300 | Maximum: 3600 <br />Minimum: 30 <br />Optional: \{\} <br /> |


#### DataStorage



DataStorage is the volume each data pod keeps the company's estate on.



_Appears in:_
- [DataPodSettings](#datapodsettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `storageClassName` _string_ | storageClassName is the StorageClass of the data volumes. Unset uses<br />the cluster's default class. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#quantity-resource-api)_ | size is the capacity requested for each data volume. It holds the<br />store, the event stream and the store's cache. There is no default: how<br />much a tenant may keep is a platform decision.<br />Three quarters of it is the engine's event stream, Crewlet's<br />stream.store_max_bytes: the stream reserves its logs' ceilings against<br />that share up front. Crewlet's own floor for it is 6Gi<br />(config.StoreMaxBytesFloor: every node reserves all five of its state<br />logs' ceilings, 4.5Gi at their smallest, and keeps room beside them for<br />its mailboxes and coordination), so a volume must be at least 8Gi.<br />It applies to the data volumes of companies created after it is set.<br />An existing company's volumes keep the size they were created with:<br />they are claimed through the StatefulSet's volume claim templates,<br />which Kubernetes does not let change, and resizing a volume is a<br />decision about one company's storage class and data, not the class's. |  | Required: \{\} <br /> |


#### DataTopology



DataTopology is the pods that hold a company's estate.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyTopology](#companytopology)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | replicas is the number of data pods. It must be one of the class's<br />spec.data.allowedReplicas. v1alpha1 runs exactly one data pod, so 1 is<br />the only value admitted. A highly available estate of 3 or 5 is a<br />later addition, and it comes with a rule that the count cannot change<br />once set, because the event stream's replication is fixed when the<br />estate is created: admitting 3 or 5 before the operator can run them<br />would leave Companies that could never be corrected. | 1 | Enum: [1] <br />Minimum: 1 <br />Optional: \{\} <br /> |


#### DataTopologyStatus



DataTopologyStatus is the state of a company's data pods.

_Validation:_
- MinProperties: 1

_Appears in:_
- [TopologyStatus](#topologystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | replicas is the number of data pods. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `readyReplicas` _integer_ | readyReplicas is the number of data pods that are ready. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `updatedReplicas` _integer_ | updatedReplicas is the number of data pods that run the current pod<br />template. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### DatadogIntegration



DatadogIntegration turns Datadog on for a company: a firing monitor wakes
the seat its tags name. It maps onto Crewlet's integrations.datadog. The
engine registers the webhook at Datadog itself and creates a service
account for each agent seat whose credentials for the MCPServer named
datadog name a provisioned application key, so the Integration's
provisioning is Engine.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `webhookTokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | webhookTokenSecretRef is the Secret key holding the token every<br />delivery carries in its X-Crewlet-Token header: at least 26<br />characters, random. Datadog signs nothing, so this token is the whole<br />of the check. Crewlet's `webhook_token`, a credential position. |  | Required: \{\} <br /> |
| `routeTo` _[DatadogRouteTo](#datadogrouteto)_ | routeTo is where an alert goes whose monitor names no seat.<br />Crewlet's `route_to`. |  | MinProperties: 1 <br />Required: \{\} <br /> |
| `site` _[DatadogSite](#datadogsite)_ | site is the region the organization, and so its keys, are in.<br />Crewlet's `provisioning.site`. |  | Enum: [US1 US3 US5 EU1 AP1 AP2 US1FED] <br />Required: \{\} <br /> |
| `apiKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | apiKeySecretRef is the Secret key holding an API key of the<br />organization, which says which organization a call is for. Crewlet's<br />`provisioning.api_key`, a credential position. |  | Required: \{\} <br /> |
| `appKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | appKeySecretRef is the Secret key holding an application key, which<br />says which user acts: one allowed to manage webhooks and service<br />accounts. Crewlet's `provisioning.app_key`, a credential position. |  | Required: \{\} <br /> |
| `role` _string_ | role is the Datadog role each seat's service account is created<br />with, by its name. Unset takes Crewlet's default, Datadog Read Only<br />Role. Crewlet's `provisioning.role`. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `emailDomain` _string_ | emailDomain is the domain the service accounts' addresses are built<br />under; Datadog requires an address and never writes to it. Unset<br />takes Crewlet's default, agents.crewlet.invalid. Crewlet's<br />`provisioning.email_domain`. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$` <br />Optional: \{\} <br /> |
| `webhookName` _string_ | webhookName is the name of the webhook the engine keeps at Datadog,<br />and so the handle monitors write: @webhook-<name>. Unset is crewlet.<br />Crewlet's `webhook_name`. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[^ @,]+$` <br />Optional: \{\} <br /> |
| `handleTag` _string_ | handleTag is the monitor tag key whose value names the seat an alert<br />wakes, such as crewlet:sre-lead. Unset is crewlet. Crewlet's<br />`handle_tag`. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[a-z][a-z0-9_./-]*$` <br />Optional: \{\} <br /> |


#### DatadogRouteTo



DatadogRouteTo is where an alert goes whose monitor names no seat.

_Validation:_
- MinProperties: 1

_Appears in:_
- [DatadogIntegration](#datadogintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `seatRef` _[LocalObjectReference](#localobjectreference)_ | seatRef names the agent Seat, in this namespace, the alert wakes.<br />Crewlet's `route_to`, as the seat's handle. |  | Optional: \{\} <br /> |
| `dismiss` _boolean_ | dismiss wakes nobody: only monitors that name a seat reach an agent,<br />and the rest stay with whatever Datadog already does about them.<br />Crewlet's `route_to: none`. |  | Optional: \{\} <br /> |


#### DatadogSite

_Underlying type:_ _string_

DatadogSite is the Datadog region a company's organization is in. It maps
onto Crewlet's provisioning.site, the region's host name (US1 is
datadoghq.com, US3 us3.datadoghq.com, US5 us5.datadoghq.com, EU1
datadoghq.eu, AP1 ap1.datadoghq.com, AP2 ap2.datadoghq.com, US1FED
ddog-gov.com).

_Validation:_
- Enum: [US1 US3 US5 EU1 AP1 AP2 US1FED]

_Appears in:_
- [DatadogIntegration](#datadogintegration)

| Field | Description |
| --- | --- |
| `US1` |  |
| `US3` |  |
| `US5` |  |
| `EU1` |  |
| `AP1` |  |
| `AP2` |  |
| `US1FED` |  |


#### DeletionPolicy

_Underlying type:_ _string_

DeletionPolicy is what happens to a company's data volumes when the Company
is deleted.

_Validation:_
- Enum: [Retain Delete]

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description |
| --- | --- |
| `Retain` | DeletionPolicyRetain keeps the data volumes and the keyring Secret<br /><name>-keyring that opens the secrets sealed on them. A Company created<br />again with the same name and namespace finds both and its estate again.<br /> |
| `Delete` | DeletionPolicyDelete deletes them with the Company.<br /> |


#### E2BSandbox



E2BSandbox is the remote sandbox service seats run code in. Each field maps
onto the same-named field of Crewlet's providers.sandbox.e2b.



_Appears in:_
- [SandboxSettings](#sandboxsettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | apiKeySecretRef is the Secret key holding the service's API key.<br />Crewlet's `api_key`, a credential position. |  | Required: \{\} <br /> |
| `domain` _string_ | domain points at a self-hosted cluster of the service. Unset is the<br />vendor's cloud. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `template` _string_ | template is the sandbox template, which also fixes each sandbox's CPU,<br />memory and disk. Unset, each run gets the vendor's prebuilt template<br />for its coding agent's CLI (claude or opencode); a template set here is<br />used for every run, so it must carry the CLI of every coding agent and<br />Agent-mode CLIAgent provider the company runs. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### EmbeddingProviderType

_Underlying type:_ _string_

EmbeddingProviderType is which embeddings API a company uses. It maps onto
Crewlet's providers.embeddings.type (OpenAI is `openai`, OpenAICompatible
is `openai-compatible`).

_Validation:_
- Enum: [OpenAI OpenAICompatible]

_Appears in:_
- [EmbeddingSettings](#embeddingsettings)

| Field | Description |
| --- | --- |
| `OpenAI` | EmbeddingProviderOpenAI is OpenAI's embeddings API.<br /> |
| `OpenAICompatible` | EmbeddingProviderOpenAICompatible is any endpoint that speaks it.<br /> |


#### EmbeddingSettings



EmbeddingSettings is the provider a company computes vectors with, for
semantic knowledge search and for recall of a seat's memory. Each field maps
onto the same-named field of Crewlet's providers.embeddings.



_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[EmbeddingProviderType](#embeddingprovidertype)_ | type is which embeddings API the provider speaks. | OpenAI | Enum: [OpenAI OpenAICompatible] <br />Optional: \{\} <br /> |
| `model` _string_ | model is the embedding model id. Crewlet's `model`. Crewlet trims it of<br />white space and refuses an embeddings provider with nothing left, as<br />naming no model. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `apiKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | apiKeySecretRef is the Secret key holding the API key. Crewlet's<br />`api_key`, a credential position. Required for type OpenAI: Crewlet<br />reads OPENAI_API_KEY from the environment when it is unset, and the<br />engine's environment never carries one (see spec.runtime.env). An<br />OpenAICompatible endpoint that needs no key may omit it. |  | Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL is the endpoint of an OpenAI-compatible service. Crewlet's<br />`base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dimensions` _integer_ | dimensions is the vector width. Unset takes the model's own width,<br />which Crewlet knows for the models it names; any other model needs it<br />stated, and it must match what the model produces. |  | Maximum: 4096 <br />Minimum: 64 <br />Optional: \{\} <br /> |
| `maxInputTokens` _integer_ | maxInputTokens is the most tokens one input may hold. Unset takes the<br />model's documented window; it may only lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxBatchInputs` _integer_ | maxBatchInputs is the most inputs one request may carry. Unset takes<br />the model's documented limit; it may only lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxBatchTokens` _integer_ | maxBatchTokens is the most tokens one request may carry, summed over<br />its inputs. Unset takes the model's documented limit; it may only<br />lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### EngineClass



EngineClass is a platform's defaults and guard rails for running Crewlet
engines: the image, the size of the data pods, how companies are exposed,
and limits. A Company names one in spec.engineClassName; a platform that
hosts many tenants usually writes one class for each service tier it
offers them.

Anyone who may write a Company may name any class; a class admits the
namespaces its spec.allowedNamespaces selects, and no others.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `EngineClass` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[EngineClassSpec](#engineclassspec)_ | spec is the class's desired state. |  | Required: \{\} <br /> |
| `status` _[EngineClassStatus](#engineclassstatus)_ | status is the class's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### EngineClassSpec



EngineClassSpec is the defaults and guard rails a platform sets once for
every Company that names the class, the way a StorageClass does for volumes.



_Appears in:_
- [EngineClass](#engineclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `allowedNamespaces` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#labelselector-v1-meta)_ | allowedNamespaces selects, by their labels, the namespaces whose<br />Companies may run with this class, as a Gateway's<br />allowedRoutes.namespaces.selector does; a Company in any other<br />namespace is not accepted (Accepted=False, ClassNotAllowed). Unset, or<br />an empty selector, admits every namespace.<br />Running with a class is a privilege, and any subject that may write a<br />Company may name any class: the class carries the platform's Secrets<br />it references (platform model keys, telemetry headers, env values)<br />into each of its companies' namespaces and engines, its workload<br />identity onto the engine ServiceAccount, and its scheduling onto the<br />platform's nodes. A class with any of those should name the<br />namespaces it is for. To select namespaces by name, match the label<br />kubernetes.io/metadata.name, which the API server sets on every<br />namespace. |  | Optional: \{\} <br /> |
| `engine` _[EngineSettings](#enginesettings)_ | engine is the engine image. |  | Required: \{\} <br /> |
| `data` _[DataPodSettings](#datapodsettings)_ | data sizes the pods that hold each company's estate. |  | Required: \{\} <br /> |
| `seatPools` _[SeatPoolSettings](#seatpoolsettings)_ | seatPools is whether each company may run seats on stateless pods of<br />its own, and how many. | \{ policy:Forbidden \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `stream` _[StreamSettings](#streamsettings)_ | stream is how each company's event stream runs. | \{ mode:Embedded \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `objects` _[ObjectSettings](#objectsettings)_ | objects is where each company's files are kept. | \{ backend:NATS \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `exposure` _[ExposureSettings](#exposuresettings)_ | exposure is how each company is reached from outside its namespace. | \{ api:map[mode:ClusterInternal] webhooks:map[mode:None] \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `networkPolicy` _[NetworkPolicySettings](#networkpolicysettings)_ | networkPolicy is the NetworkPolicy the operator puts around each<br />company's engine pods. | \{ mode:Isolated \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `podTemplate` _[EnginePodTemplate](#enginepodtemplate)_ | podTemplate is what is added to every engine pod. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `serviceAccount` _[EngineServiceAccount](#engineserviceaccount)_ | serviceAccount is what is added to each company's engine<br />ServiceAccount. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `service` _[EngineService](#engineservice)_ | service is what is added to each company's engine Service, which the<br />class's routes send to. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `platformLLMProviders` _[PlatformLLMProvider](#platformllmprovider) array_ | platformLLMProviders are model providers the platform offers to<br />companies that opt in. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `limits` _[EngineLimits](#enginelimits)_ | limits caps what a company may declare. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `telemetry` _[TelemetrySettings](#telemetrysettings)_ | telemetry sends the engine's traces to a collector. Unset exports<br />nothing. |  | Optional: \{\} <br /> |
| `retention` _[RetentionSettings](#retentionsettings)_ | retention is who answers for backups. |  | Optional: \{\} <br /> |


#### EngineClassStatus



EngineClassStatus is what the operator observed about an EngineClass.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClass](#engineclass)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions has Accepted: whether the objects the class names in other<br />namespaces exist, which its schema cannot check. It is False with<br />SecretNotFound when a Secret or key the class references does not<br />exist, GatewayAPINotInstalled when it routes through Gateways on a<br />cluster without the Gateway API, and GatewayNotFound when a Gateway<br />a route names with its namespace does not exist. Companies of a class<br />that is not accepted still run, and report what the problem does to<br />them on their own conditions. |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### EngineConfigEntry



EngineConfigEntry is one top-level key of the company document that
spec.engineConfig passes through, and its value.



_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the key, as Crewlet spells it, such as turn_engine. |  | MaxLength: 63 <br />MinLength: 1 <br />Pattern: `^[a-z][a-z0-9_]*$` <br />Required: \{\} <br /> |
| `value` _[EngineConfigValue](#engineconfigvalue)_ | value is the key's value: an object, a list, a string, a number or a<br />boolean, kept as written. |  | Type:  <br />Required: \{\} <br /> |


#### EngineConfigValue



EngineConfigValue is any JSON value, kept byte for byte as it was written.

_Validation:_
- Type: 

_Appears in:_
- [EngineConfigEntry](#engineconfigentry)



#### EngineLimits



EngineLimits caps what a Company of a class may declare. The operator
refuses to apply a company that exceeds a limit and reports it on the
Company. A control plane should refuse such a request before it writes the
Company; this is the cluster's defence in depth.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxSeats` _integer_ | maxSeats is the most Seats a Company may have. Unset is unlimited. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `maxMCPServers` _integer_ | maxMCPServers is the most MCPServers a Company may have. Unset is<br />unlimited. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### EnginePodMetadata



EnginePodMetadata is metadata added to every engine pod.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EnginePodTemplate](#enginepodtemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `labels` _object (keys:string, values:string)_ | labels are added to every engine pod. |  | MaxProperties: 32 <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | annotations are added to every engine pod. |  | MaxProperties: 32 <br />Optional: \{\} <br /> |


#### EnginePodSpec



EnginePodSpec is the part of a pod's spec a class may set on engine pods.
It is a closed list rather than a whole PodSpec: the operator owns the
containers, volumes, probes and security context, because the engine's data
safety depends on them.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EnginePodTemplate](#enginepodtemplate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `nodeSelector` _object (keys:string, values:string)_ | nodeSelector constrains engine pods to nodes with these labels. |  | MaxProperties: 32 <br />Optional: \{\} <br /> |
| `tolerations` _[Toleration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#toleration-v1-core) array_ | tolerations let engine pods run on tainted nodes. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `affinity` _[Affinity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#affinity-v1-core)_ | affinity is the engine pods' scheduling affinity. |  | Optional: \{\} <br /> |
| `topologySpreadConstraints` _[TopologySpreadConstraint](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#topologyspreadconstraint-v1-core) array_ | topologySpreadConstraints spread engine pods across topology domains. |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `runtimeClassName` _string_ | runtimeClassName is the RuntimeClass engine pods run with, for example<br />a sandboxed runtime such as gVisor. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `priorityClassName` _string_ | priorityClassName is the PriorityClass of engine pods. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `env` _[ClassEnvVar](#classenvvar) array_ | env adds environment variables to the engine container of every<br />Company of the class: platform wiring that is the same for all of<br />them, such as the address of the platform's own API. Every value,<br />one from a Secret included, reaches every tenant of the class: code<br />the tenant runs in the engine's pod can read its environment. A Company's<br />spec.runtime.env is merged over it by name. The same names are<br />refused as there: those starting with CREWLET_ or OTEL_, and<br />ANTHROPIC_API_KEY and OPENAI_API_KEY. |  | MaxItems: 32 <br />Optional: \{\} <br /> |


#### EnginePodTemplate



EnginePodTemplate is what a class adds to every engine pod.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `metadata` _[EnginePodMetadata](#enginepodmetadata)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `spec` _[EnginePodSpec](#enginepodspec)_ | spec is merged into every engine pod's spec. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### EngineService



EngineService is what a class adds to the Service <company>-engine the
operator creates in front of each Company's data pods, which the class's
routes send to. It has no Crewlet field: the engine never sees its
Service.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `annotations` _object (keys:string, values:string)_ | annotations are added to the Service, for a load balancer controller<br />that reads a backend's settings from it. On GKE an Ingress takes its<br />health check from cloud.google.com/backend-config, naming a<br />BackendConfig in each company's namespace: point its check at port<br />8080, path /ready, since the public port the webhooks' route sends to<br />answers 404 on every path it does not serve, / included. |  | MaxProperties: 16 <br />Optional: \{\} <br /> |


#### EngineServiceAccount



EngineServiceAccount is what a class adds to the ServiceAccount the operator
creates for each Company's engine pods.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `annotations` _object (keys:string, values:string)_ | annotations are added to the ServiceAccount, for a workload identity<br />that is bound through one: eks.amazonaws.com/role-arn for IRSA,<br />iam.gke.io/gcp-service-account for GKE Workload Identity. That is how<br />the engine reaches an S3 bucket when its Company names no<br />spec.objects.credentialsSecretRef. The same annotations go on every<br />company's ServiceAccount, so the identity is every tenant's: scope it<br />per tenant (a role per company, or a session policy limited to the<br />company's prefix), or give each Company keys of its own instead. |  | MaxProperties: 16 <br />Optional: \{\} <br /> |


#### EngineSettings



EngineSettings is the engine image a class runs.



_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _string_ | image is the Crewlet engine image, by tag or by digest, for example<br />registry.example.com/crewlet:v1.2.3. There is no default: the platform<br />decides which build its tenants run. A Company's spec.runtime.version<br />replaces the tag, so an image pinned by digest cannot be combined with<br />allowVersionOverride. |  | MaxLength: 512 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#pullpolicy-v1-core)_ | imagePullPolicy is the pull policy of the engine container. Unset<br />takes the Kubernetes default for the image reference. |  | Enum: [Always IfNotPresent Never] <br />Optional: \{\} <br /> |
| `imagePullSecrets` _[LocalObjectReference](#localobjectreference) array_ | imagePullSecrets name Secrets in each Company's namespace that pull<br />the image. They are referenced, not copied: the platform puts them<br />into the tenant namespaces it creates. |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `allowVersionOverride` _boolean_ | allowVersionOverride lets a Company of this class choose its own engine<br />version with spec.runtime.version. Off by default: the platform<br />usually decides which version a class's tenants run. |  | Optional: \{\} <br /> |


#### EngineStatus



EngineStatus is what runs a company.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyStatus](#companystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _string_ | image is the engine image the data pods last ran on, recorded once<br />every data pod ran it and was ready. A rollout that has not finished<br />leaves the previous image here. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `version` _string_ | version is the tag of that image, such as v1.2.3; unset for an image<br />pinned by digest alone. The engine migrates its data forward only, so<br />the operator refuses to roll the data pods to an image whose tag is an<br />older semantic version than this one (RuntimeReady=False,<br />DowngradeRefused). The operator also records it on each data volume<br />claim (the annotation crewlet.ai/engine-version), which keeps the floor<br />when this status is lost. A version that never became ready is never<br />recorded, so a rollout that failed can still be taken back. |  | MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### EnvVar



EnvVar is an environment variable set on the engine's pods. Unlike
SecretEnvVar it may carry a literal value, because it is not a Crewlet
credential position but platform wiring (a feature flag, an endpoint); a
sensitive value is referenced from a Secret with valueFrom, which Kubernetes
resolves when it starts the container, so the value never appears in the pod
template.



_Appears in:_
- [CompanyRuntime](#companyruntime)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `value` _string_ | value is the variable's literal value. An empty string is a valid<br />value, which is why the field is a pointer. |  | MaxLength: 4096 <br />Optional: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom reads the value from a Secret key in the Company's namespace. |  | Optional: \{\} <br /> |


#### ExposureSettings



ExposureSettings is how a class's companies are reached.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `webhooks` _[WebhookExposure](#webhookexposure)_ | webhooks is how vendors' webhooks reach the engine. | \{ mode:None \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `api` _[APIExposure](#apiexposure)_ | api is how the engine's API is reached. | \{ mode:ClusterInternal \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `trustedProxies` _string array_ | trustedProxies are the address blocks of what forwards requests to<br />the engine: the Gateway's proxies or the Ingress controller of the<br />class's routes, and whatever balances in front of them. Crewlet takes<br />a request's client address from its X-Forwarded-For header only when<br />the request arrives from one of these, and otherwise uses the address<br />it arrives from, which behind a proxy is the proxy's for every<br />caller: the sign-in throttle then counts everyone's attempts at one<br />login together, so a stranger guessing at a person's login holds up<br />that person's own sign-in, and the audit trail names the proxy. Each<br />is a canonical IPv4 or IPv6 CIDR narrower than the whole address<br />space; a single host is a /32 or /128. Unset, no forwarded header is<br />believed. Crewlet's api.trusted_proxies. |  | MaxItems: 16 <br />MinItems: 1 <br />items:MaxLength: 43 <br />Optional: \{\} <br /> |


#### ExternalExposure



ExternalExposure is where the forwarder of a class's webhooks runs, when
something outside the operator forwards them (webhooks.mode External).

_Validation:_
- MinProperties: 1

_Appears in:_
- [WebhookExposure](#webhookexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `forwarders` _[NetworkPolicyPeer](#networkpolicypeer) array_ | forwarders are the pods that forward webhooks to the engine's public<br />port: a tunnel's or a relay's pods in the cluster. They are required<br />under networkPolicy Isolated, which admits them to that port and to<br />nothing else of the engine, and refused under any other mode, which<br />has no policy to admit them to. A forwarder outside the pod network (a<br />load balancer of your own that sends traffic from the nodes or from<br />outside the cluster) cannot be admitted by namespace: isolate those<br />engines with your CNI's own policy and networkPolicy.mode None. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### GatewayExposure



GatewayExposure attaches a company's routes to Gateways.



_Appears in:_
- [APIExposure](#apiexposure)
- [WebhookExposure](#webhookexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `parentRefs` _[GatewayParentReference](#gatewayparentreference) array_ | parentRefs are the Gateways the HTTPRoute attaches to. |  | MaxItems: 8 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `proxyNamespaces` _string array_ | proxyNamespaces are the namespaces the pods that carry the Gateways'<br />traffic run in. Under networkPolicy Isolated they are admitted to the<br />one port of the engine the route reaches: the public listener for the<br />webhooks' route, the API port for the API's. Unset is the namespaces<br />of parentRefs, which is right for<br />implementations that run a proxy beside each Gateway (Istio, for<br />example); implementations with a shared data plane run it elsewhere<br />(Envoy Gateway in envoy-gateway-system, for example) and need it named<br />here. |  | MaxItems: 8 <br />MinItems: 1 <br />items:MaxLength: 63 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |


#### GatewayParentReference



GatewayParentReference names a Gateway API Gateway (or one listener of it)
that the operator attaches a company's HTTPRoute to.



_Appears in:_
- [GatewayExposure](#gatewayexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | namespace is the Gateway's namespace. Unset is the Company's own<br />namespace. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `name` _string_ | name is the Gateway's name. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `sectionName` _string_ | sectionName is the name of one listener of the Gateway. Unset attaches<br />to every listener that accepts the route. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### GitHubAccessTier

_Underlying type:_ _string_

GitHubAccessTier is how much a seat may do in a repository. It maps onto
Crewlet's role integrations.github.tier (ReadOnly is `read_only`, Review is
`review`, FullAccess is `full_access`).

_Validation:_
- Enum: [ReadOnly Review FullAccess]

_Appears in:_
- [GitHubSeatIntegration](#githubseatintegration)

| Field | Description |
| --- | --- |
| `ReadOnly` |  |
| `Review` |  |
| `FullAccess` |  |


#### GitHubApp



GitHubApp is the GitHub App a seat acts as. An app carries exactly one bot
identity, so every agent seat that works on GitHub has its own.



_Appears in:_
- [GitHubSeatIntegration](#githubseatintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `id` _integer_ | id is the app's numeric id. Crewlet's `app_id`. |  | Minimum: 1 <br />Required: \{\} <br /> |
| `slug` _string_ | slug is the app's slug; the bot's login, and so how the seat is<br />mentioned, derives from it. Crewlet's `app_slug`. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9-]*$` <br />Required: \{\} <br /> |
| `installationID` _integer_ | installationID is the app's installation on the organization. Unset<br />while the app is created but not yet installed. Crewlet's<br />`installation_id`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `credentialsSecretRef` _[SecretReference](#secretreference)_ | credentialsSecretRef is a Secret holding the app's credentials under<br />the keys privateKey (the app's PEM key) and webhookSecret (which<br />verifies the app's own webhook deliveries). Crewlet's `private_key`<br />and `webhook_secret`, both credential positions. |  | Required: \{\} <br /> |


#### GitHubIntegration



GitHubIntegration turns GitHub on for a company: webhooks from github.com or
a GitHub Enterprise Server wake the seats they concern. It maps onto
Crewlet's integrations.github. Each agent seat's own GitHub App is in its
Seat's spec.integrations.github; the GitHub tools are an MCPServer.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | url is the base URL of a GitHub Enterprise Server, such as<br />https://github.example.com. Unset is github.com. Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `webhookSecretRef` _[SecretKeyReference](#secretkeyreference)_ | webhookSecretRef is the Secret key holding the secret that verifies<br />webhooks delivered to the company-wide route. Crewlet's<br />`webhook_secret`, a credential position. |  | Required: \{\} <br /> |
| `tokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | tokenSecretRef is the Secret key holding an organization token with<br />the admin:org_hook scope, which the engine registers the company's<br />webhooks with. Unset registers none. Crewlet's `token`, a credential<br />position. |  | Optional: \{\} <br /> |
| `provisioning` _[GitHubProvisioning](#githubprovisioning)_ | provisioning is where the webhooks are registered. It requires<br />tokenSecretRef, without which the engine registers nothing. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### GitHubProvisioning



GitHubProvisioning is where the engine registers the company's webhooks at
GitHub. It maps onto Crewlet's integrations.github.provisioning. The engine
registers them with the organization token (tokenSecretRef); without one it
registers nothing and each seat's own app delivers its own webhooks.

_Validation:_
- MinProperties: 1

_Appears in:_
- [GitHubIntegration](#githubintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `org` _string_ | org is the GitHub organization holding the repositories. Crewlet's<br />`org`. |  | MaxLength: 39 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Optional: \{\} <br /> |
| `repos` _string array_ | repos are repositories, as owner/name, to register a webhook on one by<br />one. Crewlet's `repos`. |  | MaxItems: 128 <br />items:MaxLength: 201 <br />items:Pattern: `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$` <br />Optional: \{\} <br /> |
| `orgWebhook` _[ContainerWebhookMode](#containerwebhookmode)_ | orgWebhook is whether one organization hook replaces the<br />per-repository ones. Unset is Auto. Crewlet's `org_webhook`. |  | Enum: [Auto Require Never] <br />Optional: \{\} <br /> |


#### GitHubSeatIntegration



GitHubSeatIntegration is a seat's identity on GitHub. It maps onto
Crewlet's role integrations.github and needs the company's GitHub
Integration.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatIntegrations](#seatintegrations)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tier` _[GitHubAccessTier](#githubaccesstier)_ | tier is how much the seat may do in a repository. Unset is ReadOnly.<br />Crewlet's `tier`. |  | Enum: [ReadOnly Review FullAccess] <br />Optional: \{\} <br /> |
| `repos` _string array_ | repos are the repositories the seat works in, as owner/name. Unset is<br />every repository the app's installation covers. Crewlet's `repos`. |  | MaxItems: 64 <br />items:MaxLength: 201 <br />items:Pattern: `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$` <br />Optional: \{\} <br /> |
| `app` _[GitHubApp](#githubapp)_ | app is the GitHub App the seat acts as. |  | Optional: \{\} <br /> |


#### GitHubSeatStatus



GitHubSeatStatus is the GitHub side of a seat, as the operator computed it.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatIntegrationsStatus](#seatintegrationsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `webhookURL` _string_ | webhookURL is the URL the seat's GitHub App must deliver webhooks to,<br /><publicBaseURL>/webhooks/github/<handle>. Like the Slack requestURL it<br />is set whenever the Company has a GitHub Integration and a public base<br />URL, before the seat's app exists, and it never changes for a seat. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### GitLabAccessLevel

_Underlying type:_ _string_

GitLabAccessLevel is the membership level the engine gives a seat's GitLab
service account. It maps onto Crewlet's access_level (Developer is
`developer`, Maintainer is `maintainer`).

_Validation:_
- Enum: [Developer Maintainer]

_Appears in:_
- [GitLabEngineProvisioning](#gitlabengineprovisioning)
- [GitLabSeatAccess](#gitlabseataccess)

| Field | Description |
| --- | --- |
| `Developer` | GitLabDeveloper can push branches and open merge requests.<br /> |
| `Maintainer` | GitLabMaintainer can also merge to protected branches.<br /> |


#### GitLabAccountOwner

_Underlying type:_ _string_

GitLabAccountOwner is where the engine creates the seats' GitLab service
accounts. It maps onto Crewlet's provisioning mode (Group is `group`,
Instance is `instance`).

_Validation:_
- Enum: [Group Instance]

_Appears in:_
- [GitLabEngineProvisioning](#gitlabengineprovisioning)

| Field | Description |
| --- | --- |
| `Group` | GitLabAccountOwnerGroup creates them under the group, which is all<br />GitLab.com offers.<br /> |
| `Instance` | GitLabAccountOwnerInstance creates them on a self-managed instance<br />itself, with an instance administrator's token.<br /> |


#### GitLabEngineProvisioning



GitLabEngineProvisioning is how the engine provisions the seats' GitLab
accounts under provisioning Engine. It maps onto Crewlet's
integrations.gitlab.provisioning: for each agent seat whose credentials for
the MCPServer named gitlab name a provisioned value, the engine creates a
service account, adds it to the group and the projects, mints a token into
that value, and registers the webhook.



_Appears in:_
- [GitLabIntegration](#gitlabintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `group` _string_ | group is the top-level group, by its path, the service accounts join<br />and the webhook is registered on. Crewlet's `group`. |  | MaxLength: 255 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9_.][A-Za-z0-9_./-]*$` <br />Required: \{\} <br /> |
| `adminTokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | adminTokenSecretRef is the Secret key holding a personal access token<br />with the api scope of an Owner of the group (of an instance<br />administrator, for accountOwner Instance), which creates and removes<br />the service accounts. Crewlet's `admin_token`, a credential position. |  | Required: \{\} <br /> |
| `accountOwner` _[GitLabAccountOwner](#gitlabaccountowner)_ | accountOwner is where the service accounts are created. Unset is<br />Group. Crewlet's `mode`. |  | Enum: [Group Instance] <br />Optional: \{\} <br /> |
| `accessLevel` _[GitLabAccessLevel](#gitlabaccesslevel)_ | accessLevel is the membership level of every seat's account. Unset<br />takes Crewlet's default, Developer. Crewlet's `access_level`. |  | Enum: [Developer Maintainer] <br />Optional: \{\} <br /> |
| `accessLevels` _[GitLabSeatAccess](#gitlabseataccess) array_ | accessLevels override accessLevel for single seats. Crewlet's<br />`access_levels`. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `usernamePrefix` _string_ | usernamePrefix is put before each seat's handle to form its account's<br />username, for a group shared with people. Crewlet's<br />`username_prefix`. |  | MaxLength: 32 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9_.-]+$` <br />Optional: \{\} <br /> |
| `projects` _string array_ | projects are projects, by path, the accounts also join, and the ones<br />the webhook is registered on one by one when no group hook is.<br />Crewlet's `projects`. |  | MaxItems: 128 <br />items:MaxLength: 255 <br />items:MinLength: 1 <br />items:Pattern: `^[A-Za-z0-9_.][A-Za-z0-9_./-]*$` <br />Optional: \{\} <br /> |
| `groupWebhook` _[ContainerWebhookMode](#containerwebhookmode)_ | groupWebhook is whether one group hook replaces the per-project ones.<br />Unset is Auto. Crewlet's `group_webhook`. |  | Enum: [Auto Require Never] <br />Optional: \{\} <br /> |
| `tokenScopes` _string array_ | tokenScopes are the scopes minted on each seat's token. Unset takes<br />Crewlet's default, api. Crewlet's `token_scopes`. |  | MaxItems: 16 <br />items:MaxLength: 64 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z_]+$` <br />Optional: \{\} <br /> |


#### GitLabIntegration



GitLabIntegration turns GitLab on for a company: webhooks from a GitLab
instance wake the seats they concern. It maps onto Crewlet's
integrations.gitlab. Each seat's GitLab token is a credential of the
MCPServer named gitlab in its Seat's spec.mcp, which is where Crewlet reads
a seat's GitLab identity from.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | url is the instance's base URL, such as https://gitlab.com. Crewlet's<br />`url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `signingSecretRef` _[SecretKeyReference](#secretkeyreference)_ | signingSecretRef is the Secret key holding the signing token every<br />webhook delivery is verified against: whsec_ followed by the standard<br />base64 of 32 bytes, the only form GitLab signs with (its "Generate<br />signing token" button makes one). Crewlet's `signing_secret`, a<br />credential position. |  | Required: \{\} <br /> |
| `tokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | tokenSecretRef is the Secret key holding a personal access token with<br />the read_api scope, which the engine reads an issue's or merge<br />request's participants with, so that everyone in a thread hears its<br />activity. Unset routes to the people a delivery names only. Crewlet's<br />`token`, a credential position. |  | Optional: \{\} <br /> |
| `webhookName` _string_ | webhookName is the name of the hooks the engine registers, which is<br />how it tells its own from others on the instance: two deployments<br />watching one instance need two names. Unset is crewlet. Crewlet's<br />`webhook_name`. |  | MaxLength: 255 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `engineProvisioning` _[GitLabEngineProvisioning](#gitlabengineprovisioning)_ | engineProvisioning is how the engine creates the seats' accounts.<br />Required with provisioning Engine, and not allowed with External. |  | Optional: \{\} <br /> |


#### GitLabSeatAccess



GitLabSeatAccess overrides the access level of one seat's service account.



_Appears in:_
- [GitLabEngineProvisioning](#gitlabengineprovisioning)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the Seat, in this namespace. Crewlet keys<br />the override by the seat's handle, which the operator renders. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `accessLevel` _[GitLabAccessLevel](#gitlabaccesslevel)_ | accessLevel is the seat's membership level. |  | Enum: [Developer Maintainer] <br />Required: \{\} <br /> |


#### IngressExposure



IngressExposure puts a company's routes on an Ingress.



_Appears in:_
- [APIExposure](#apiexposure)
- [WebhookExposure](#webhookexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `className` _string_ | className is the IngressClass. Unset uses the cluster's default class. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `annotations` _object (keys:string, values:string)_ | annotations are added to the Ingress, for the controller that serves it<br />or a certificate controller. |  | MaxProperties: 32 <br />Optional: \{\} <br /> |
| `tls` _[IngressTLS](#ingresstls)_ | tls terminates TLS on the Ingress. Unset serves plain HTTP, which is<br />right only behind something else that terminates TLS: vendors deliver<br />webhooks over HTTPS only. |  | Optional: \{\} <br /> |
| `controllerNamespaces` _string array_ | controllerNamespaces are the namespaces the Ingress controller's pods<br />run in. Under networkPolicy Isolated they are admitted to the one port<br />of the engine the Ingress reaches (the public listener for the<br />webhooks' Ingress, the API port for the API's), and they are required<br />there: an Ingress does not say which pods<br />serve it, and without them the policy would refuse every request the<br />Ingress forwards. |  | MaxItems: 8 <br />MinItems: 1 <br />items:MaxLength: 63 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |


#### IngressTLS



IngressTLS terminates TLS on a company's Ingress.



_Appears in:_
- [IngressExposure](#ingressexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | secretName is the TLS Secret, in the Company's namespace, the Ingress<br />presents. It is a template like the hostname: \{\{namespace\}\}, \{\{name\}\}<br />and \{\{uid\}\} are replaced. A certificate controller that issues<br />certificates from Ingress annotations creates it; set those annotations<br />under ingress.annotations. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### Integration



Integration connects a company to a vendor (Slack, GitHub, GitLab,
Mattermost, Jira, Confluence, Datadog or an Atlassian organization): the
company-wide settings, the credentials that verify the vendor's webhooks,
and, where the engine creates the seats' accounts, the administrator
credential it creates them with. Credentials that belong to one seat, such
as its own Slack app or its GitLab token, are on the Seat.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Integration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[IntegrationSpec](#integrationspec)_ | spec is the integration's desired state. |  | Required: \{\} <br /> |
| `status` _[IntegrationStatus](#integrationstatus)_ | status is the integration's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### IntegrationProvisioning

_Underlying type:_ _string_

IntegrationProvisioning is who creates the seats' accounts at a vendor, and
so where their credentials come from.

_Validation:_
- Enum: [External Engine]

_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description |
| --- | --- |
| `External` | IntegrationProvisioningExternal leaves the seats' accounts to someone<br />outside the engine (a person, a platform): each seat's credentials are<br />Secrets its Seat references, and the engine is given no administrator<br />credential, so it creates, changes and removes nothing at the vendor.<br /> |
| `Engine` | IntegrationProvisioningEngine has the engine create each agent seat's<br />account with an administrator credential the Integration references,<br />mint the account's credential into the engine's secret store, register<br />the company's webhooks, and heal all of it on a schedule. Seats name<br />the minted credentials as provisioned values.<br /> |


#### IntegrationSpec



IntegrationSpec connects a company to one vendor. Exactly one vendor member
is set; the Integration's kind of vendor cannot change, and a company has at
most one Integration per vendor (a second is reported as a conflict).

provisioning says who creates the seats' accounts at the vendor. Under
External (the default) someone else does, and every per-seat credential
comes from a Secret its Seat references; the Integration then holds no
administrator credential. Under Engine the engine creates them, from the
administrator credential the vendor member references, and seats name the
credentials it mints as provisioned values. The engine can create accounts
at GitLab, Mattermost, Datadog and Atlassian; Datadog and Atlassian are
always provisioned by the engine, and Slack, GitHub, Jira and Confluence
never are (the engine mints no credential there).



_Appears in:_
- [Integration](#integration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this integration belongs to. Immutable. |  | Required: \{\} <br /> |
| `provisioning` _[IntegrationProvisioning](#integrationprovisioning)_ | provisioning is who creates the seats' accounts at the vendor. Unset<br />is External. |  | Enum: [External Engine] <br />Optional: \{\} <br /> |
| `slack` _[SlackIntegration](#slackintegration)_ | slack connects Slack. |  | Optional: \{\} <br /> |
| `github` _[GitHubIntegration](#githubintegration)_ | github connects GitHub. |  | Optional: \{\} <br /> |
| `gitlab` _[GitLabIntegration](#gitlabintegration)_ | gitlab connects GitLab. |  | Optional: \{\} <br /> |
| `mattermost` _[MattermostIntegration](#mattermostintegration)_ | mattermost connects Mattermost. |  | Optional: \{\} <br /> |
| `jira` _[JiraIntegration](#jiraintegration)_ | jira connects Jira. |  | Optional: \{\} <br /> |
| `confluence` _[ConfluenceIntegration](#confluenceintegration)_ | confluence connects Confluence. |  | Optional: \{\} <br /> |
| `datadog` _[DatadogIntegration](#datadogintegration)_ | datadog connects Datadog. |  | Optional: \{\} <br /> |
| `atlassian` _[AtlassianIntegration](#atlassianintegration)_ | atlassian connects an Atlassian Cloud organization. |  | Optional: \{\} <br /> |


#### IntegrationStatus



IntegrationStatus is what the operator observed about an Integration.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Integration](#integration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists and has no other<br />Integration for the vendor), ResolvedRefs (every Secret key exists),<br />Applied (the integration is in the revision the engine runs) and<br />Healthy (the engine's own check of the vendor passes). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `webhookURL` _string_ | webhookURL is the company-wide URL the vendor delivers webhooks to,<br />when the company is exposed and the vendor has one:<br /><publicBaseURL>/webhooks/<vendor> for GitHub, GitLab, Jira,<br />Confluence (a Data Center instance's; Confluence Cloud delivers to<br /><webhookURL>/<event>, a hook the engine registers per event) and<br />Datadog. Slack, Mattermost and Atlassian have none; Slack's and<br />GitHub's per-seat URLs are in each Seat's status. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### JiraIntegration



JiraIntegration turns Jira on for a company: webhooks from a Jira site wake
the seats they concern, and the company may run Jira as its tracker (the
Company's spec.tracker.backend Jira). It maps onto Crewlet's
integrations.jira. Each seat's Jira identity is its own token, a credential
of the MCPServer named atlassian (or jira) in its Seat's spec.mcp.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | url is a Data Center instance's or a Cloud site's base URL. Unset<br />with cloudID unset, the company's Atlassian Integration discovers the<br />site. Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `cloudID` _string_ | cloudID is an Atlassian Cloud site's id; requests go through the API<br />gateway of the Cloud siteURL is on: Atlassian Government Cloud's,<br />api.atlassian-us-gov-mod.com, for a site under<br />atlassian-us-gov-mod.net, and api.atlassian.com otherwise. Crewlet's<br />`cloud_id`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Optional: \{\} <br /> |
| `siteURL` _string_ | siteURL is the site's address as a browser opens it, for the links<br />the engine hands people; needed with cloudID, whose gateway address<br />opens nothing in a browser, and which is read through the gateway of<br />this site's Cloud. Unset is url. Crewlet's `site_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `email` _string_ | email is the account's address, for Jira Cloud's basic<br />authentication. Unset sends the token as a bearer token, as a service<br />account and a Data Center personal access token want. Crewlet's<br />`email`. |  | MaxLength: 254 <br />MinLength: 3 <br />Pattern: `^[^@\s]+@[^@\s]+$` <br />Optional: \{\} <br /> |
| `tokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | tokenSecretRef is the Secret key holding the API token (Cloud) or<br />personal access token (Data Center) of an administrator account. The<br />engine reads an issue's watchers with it, which no webhook carries,<br />and registers the company's webhook. Crewlet's `token`, a credential<br />position. |  | Required: \{\} <br /> |
| `webhookSecretRef` _[SecretKeyReference](#secretkeyreference)_ | webhookSecretRef is the Secret key holding the secret webhook<br />deliveries are signed with. Required for a Data Center instance, whose<br />route answers none without it; on Cloud, webhooks the engine<br />registers are signed with it, and the Forge relay needs none.<br />Crewlet's `webhook_secret`, a credential position. |  | Optional: \{\} <br /> |
| `webhookName` _string_ | webhookName is the name of the webhook the engine registers, which is<br />how it tells its own from others on the site: two deployments<br />watching one site need two names. Unset is crewlet. Crewlet's<br />`webhook_name`. |  | MaxLength: 255 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### KnowledgeBackend

_Underlying type:_ _string_

KnowledgeBackend is which knowledge base a company runs. It maps onto
Crewlet's knowledge.backend (Native is `native`, Confluence is
`confluence`, None is `none`).

_Validation:_
- Enum: [Native Confluence None]

_Appears in:_
- [KnowledgeSettings](#knowledgesettings)

| Field | Description |
| --- | --- |
| `Native` | KnowledgeNative is the engine's own knowledge base.<br /> |
| `Confluence` | KnowledgeConfluence is Confluence, connected by the company's<br />Confluence Integration.<br /> |
| `None` | KnowledgeNone is a company with no knowledge base.<br /> |


#### KnowledgeSettings



KnowledgeSettings is which knowledge base a company runs and how it is
searched. It maps onto Crewlet's knowledge block.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backend` _[KnowledgeBackend](#knowledgebackend)_ | backend is which knowledge base the company runs. | Native | Enum: [Native Confluence None] <br />Optional: \{\} <br /> |
| `scope` _string array_ | scope narrows every knowledge search to these containers. Empty<br />searches the whole company. Each is a container key, which Crewlet<br />requires to be an upper-case letter followed by 1 to 9 upper-case<br />letters or digits, such as PLAT. Crewlet's `scope`. |  | MaxItems: 64 <br />items:MaxLength: 10 <br />items:MinLength: 2 <br />items:Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `skillsContainer` _string_ | skillsContainer is the container holding tool-skill pages, a container<br />key (see scope). Unset takes Crewlet's default container; an empty<br />string turns tool skills off. Crewlet's `skills_container`. |  | MaxLength: 10 <br />Optional: \{\} <br /> |
| `rootSpace` _string_ | rootSpace is the container holding the company's own pages, such as<br />the root onboarding page, a container key (see scope). Unset takes<br />Crewlet's default. Crewlet's `root_space`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `vectors` _boolean_ | vectors fuses semantic recall into knowledge and work search; false<br />also stops embedding the corpus. Unset is on exactly when the company<br />has an embeddings provider; true requires one (spec.embeddings).<br />Crewlet's `vectors`. |  | Optional: \{\} <br /> |


#### LLMCredentialCooldowns



LLMCredentialCooldowns is how long a key that failed is set aside before it
is tried again. It maps onto Crewlet's `cooldowns` block.

_Validation:_
- MinProperties: 1

_Appears in:_
- [LLMProviderSettings](#llmprovidersettings)
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `rateLimitSeconds` _integer_ | rateLimitSeconds is how long a key that hit a rate limit is set aside.<br />Unset takes Crewlet's default of 3600. |  | Maximum: 86400 <br />Minimum: 60 <br />Optional: \{\} <br /> |
| `authSeconds` _integer_ | authSeconds is how long a key the provider refused is set aside. Unset<br />takes Crewlet's default of 300. |  | Maximum: 86400 <br />Minimum: 60 <br />Optional: \{\} <br /> |


#### LLMProvider



LLMProvider is a model provider a company's seats can run on: one entry of
Crewlet's providers.llm, keyed by its name. The name default is refused:
Crewlet runs a seat that names no chain on the provider keyed default when
there is one, whatever the Company's spec.llm.order says.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `LLMProvider` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[LLMProviderSpec](#llmproviderspec)_ | spec is the provider's desired state. |  | Required: \{\} <br /> |
| `status` _[LLMProviderStatus](#llmproviderstatus)_ | status is the provider's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### LLMProviderKind

_Underlying type:_ _string_

LLMProviderKind is the kind of model provider a reference names.

_Validation:_
- Enum: [LLMProvider PlatformLLMProvider]

_Appears in:_
- [LLMProviderReference](#llmproviderreference)

| Field | Description |
| --- | --- |
| `LLMProvider` | LLMProviderKindLLMProvider is an LLMProvider in the referring object's<br />namespace.<br /> |
| `PlatformLLMProvider` | LLMProviderKindPlatform is an entry of the Company's EngineClass<br />spec.platformLLMProviders; the Company must set<br />spec.llm.usePlatformProviders.<br /> |


#### LLMProviderReference



LLMProviderReference names a model provider: an LLMProvider in the
referring object's namespace, or a provider the EngineClass offers.



_Appears in:_
- [CompanyLLMSettings](#companyllmsettings)
- [SeatLLM](#seatllm)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kind` _[LLMProviderKind](#llmproviderkind)_ | kind is the kind of provider named. | LLMProvider | Enum: [LLMProvider PlatformLLMProvider] <br />Optional: \{\} <br /> |
| `name` _string_ | name is the LLMProvider's metadata.name, or the platform provider's<br />name. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### LLMProviderSettings



LLMProviderSettings is everything about a model provider except where its
keys come from. LLMProvider and the EngineClass's platform providers share
it, so a platform-provided model is described exactly like a tenant's own.
Each field maps onto the same-named field of a Crewlet providers.llm entry.

The cross-field rules over these fields are declared on each type that
embeds this one (LLMProviderSpec and PlatformLLMProvider), because
controller-gen does not carry a validation declared on an embedded type into
the schema of the type that inlines it. The API tests exercise both copies.



_Appears in:_
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible CLIAgent] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. Crewlet trims it of white space and refuses a<br />provider with nothing left, as naming no model. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _[ClaudeModelID](#claudemodelid)_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | Enum: [claude-fable-5-1 claude-mythos-5-1 claude-fable-5 claude-mythos-5 claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4-7 claude-sonnet-5-5 claude-sonnet-5 claude-opus-4-6 claude-sonnet-4-6 claude-opus-4-5 claude-sonnet-4-5 claude-haiku-4-5 claude-opus-4-1 claude-opus-4-0 claude-opus-4 claude-sonnet-4-0 claude-sonnet-4 claude-3-7-sonnet] <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. For type<br />Anthropic it is the gateway's root, without /v1: Crewlet's own calls,<br />Claude Code (as ANTHROPIC_BASE_URL) and OpenCode each add the version<br />themselves. A seat's code work running on the provider hands it to the<br />coding agent in each box. Crewlet's `base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `reasoning` _boolean_ | reasoning turns on an OpenAI reasoning model's effort, so that<br />reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only. |  | Optional: \{\} <br /> |
| `reasoningEffort` _[ReasoningEffort](#reasoningeffort)_ | reasoningEffort is how hard the model thinks. For Anthropic it is sent<br />on every call, and unset means High. For OpenAI it is sent only while<br />reasoning is true, so it requires reasoning; an OpenAICompatible<br />provider cannot turn reasoning on and takes no reasoningEffort.<br />Crewlet's `reasoning_effort`. |  | Enum: [Low Medium High XHigh Max] <br />Optional: \{\} <br /> |
| `reasoningBudgetTokens` _integer_ | reasoningBudgetTokens is the thinking budget of an older, budget-based<br />Claude model. Unset means such a model does not think. Crewlet's<br />`reasoning_budget_tokens`; type Anthropic only. |  | Minimum: 1024 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the<br />longest silence in a streamed one. Unset takes Crewlet's default of<br />600. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `cooldowns` _[LLMCredentialCooldowns](#llmcredentialcooldowns)_ | cooldowns is how long a failing key is set aside. Crewlet's<br />`cooldowns`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### LLMProviderSpec



LLMProviderSpec is one model provider of a company. It renders to one entry
of Crewlet's providers.llm, keyed by the LLMProvider's metadata.name; Seats
and the Company's model order refer to it by that name.



_Appears in:_
- [LLMProvider](#llmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this provider belongs to. Immutable. |  | Required: \{\} <br /> |
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible CLIAgent] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. Crewlet trims it of white space and refuses a<br />provider with nothing left, as naming no model. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _[ClaudeModelID](#claudemodelid)_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | Enum: [claude-fable-5-1 claude-mythos-5-1 claude-fable-5 claude-mythos-5 claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4-7 claude-sonnet-5-5 claude-sonnet-5 claude-opus-4-6 claude-sonnet-4-6 claude-opus-4-5 claude-sonnet-4-5 claude-haiku-4-5 claude-opus-4-1 claude-opus-4-0 claude-opus-4 claude-sonnet-4-0 claude-sonnet-4 claude-3-7-sonnet] <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. For type<br />Anthropic it is the gateway's root, without /v1: Crewlet's own calls,<br />Claude Code (as ANTHROPIC_BASE_URL) and OpenCode each add the version<br />themselves. A seat's code work running on the provider hands it to the<br />coding agent in each box. Crewlet's `base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `reasoning` _boolean_ | reasoning turns on an OpenAI reasoning model's effort, so that<br />reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only. |  | Optional: \{\} <br /> |
| `reasoningEffort` _[ReasoningEffort](#reasoningeffort)_ | reasoningEffort is how hard the model thinks. For Anthropic it is sent<br />on every call, and unset means High. For OpenAI it is sent only while<br />reasoning is true, so it requires reasoning; an OpenAICompatible<br />provider cannot turn reasoning on and takes no reasoningEffort.<br />Crewlet's `reasoning_effort`. |  | Enum: [Low Medium High XHigh Max] <br />Optional: \{\} <br /> |
| `reasoningBudgetTokens` _integer_ | reasoningBudgetTokens is the thinking budget of an older, budget-based<br />Claude model. Unset means such a model does not think. Crewlet's<br />`reasoning_budget_tokens`; type Anthropic only. |  | Minimum: 1024 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the<br />longest silence in a streamed one. Unset takes Crewlet's default of<br />600. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `cooldowns` _[LLMCredentialCooldowns](#llmcredentialcooldowns)_ | cooldowns is how long a failing key is set aside. Crewlet's<br />`cooldowns`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiKeySecretRefs` _[SecretKeyReference](#secretkeyreference) array_ | apiKeySecretRefs are the provider's API keys, each a key of a Secret in<br />this namespace. With more than one, Crewlet rotates through them,<br />setting aside a key that hits a rate limit or is refused, in the order<br />listed when several are equally free. Crewlet's `api_keys`, a<br />credential position: the values are pushed into the engine's secret<br />store and the document refers to them by name only. Required for types<br />Anthropic and OpenAI; an OpenAICompatible endpoint that needs no key<br />may omit it. A CLIAgent provider takes exactly one, with<br />cli.auth.mode APIKey, which it gives the CLI. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `cli` _[CLIAgentSettings](#cliagentsettings)_ | cli is the coding CLI a provider of type CLIAgent drives. Required<br />for, and only for, type CLIAgent. Crewlet's `cli`. |  | Optional: \{\} <br /> |


#### LLMProviderStatus



LLMProviderStatus is what the operator observed about an LLMProvider.

_Validation:_
- MinProperties: 1

_Appears in:_
- [LLMProvider](#llmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists), ResolvedRefs (every<br />Secret key exists), Applied (the provider is in the revision the<br />engine runs) and, on a provider of type CLIAgent in mode Agent,<br />AgentModeRunnable (whether its runs can call a seat's tools back<br />through the Company's public address). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### LLMProviderType

_Underlying type:_ _string_

LLMProviderType is which model API a provider speaks. It maps onto the
`type` of a Crewlet providers.llm entry: Anthropic is `anthropic`, OpenAI is
`openai`, OpenAICompatible is `openai-compatible` and CLIAgent is
`cli-agent`.

_Validation:_
- Enum: [Anthropic OpenAI OpenAICompatible CLIAgent]

_Appears in:_
- [LLMProviderSettings](#llmprovidersettings)
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)

| Field | Description |
| --- | --- |
| `Anthropic` | LLMProviderAnthropic is Anthropic's Messages API.<br /> |
| `OpenAI` | LLMProviderOpenAI is OpenAI's API.<br /> |
| `OpenAICompatible` | LLMProviderOpenAICompatible is any endpoint that speaks the OpenAI wire<br />format: a gateway, an aggregator, a self-hosted inference server.<br /> |
| `CLIAgent` | LLMProviderCLIAgent drives a coding CLI (Claude Code, Codex, ...) on a<br />subscription login, installed in the engine's image: see<br />LLMProviderSpec.cli.<br /> |


#### LocalObjectReference



LocalObjectReference names another object of the kind the referring field
documents, in the referring object's own namespace.



_Appears in:_
- [DatadogRouteTo](#datadogrouteto)
- [EngineSettings](#enginesettings)
- [ScheduleTarget](#scheduletarget)
- [SeatSandbox](#seatsandbox)
- [SeatSpec](#seatspec)
- [UnitSpec](#unitspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the referenced object. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### MCPCredentialValue



MCPCredentialValue is one value a tool server is started with for a seat:
one of its credentials, from a Secret or provisioned by the engine, or a
setting that is no credential, written as a literal value. Whether it is an
environment variable or a request header depends on the server's
transport, which is why its name follows the grammar of an HTTP header
name, of which an environment variable's name is a subset.

A literal value is for a setting such as a path: slack-mcp-server, for one,
keeps the channels its bot can see in a file named after the workspace
alone, so each seat names its own (SLACK_MCP_CHANNELS_CACHE), or seats on
one engine pod are shown each other's private channels. Crewlet treats every
value of mcp_env as a credential and masks it on every read, and cannot
tell a setting from a credential; the name can, so a name that says it
holds a credential, as Crewlet reads one (see MCPEnvVar), takes valueFrom.



_Appears in:_
- [MCPCredentials](#mcpcredentials)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the environment variable's name (Stdio) or the header's name<br />(HTTP). |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9!#$%&'*+.^_\|~-]+$` <br />Required: \{\} <br /> |
| `value` _string_ | value is a literal value, for a setting that is no credential. It<br />lands in the company document as written, where anyone who can read<br />this object reads it too. An empty string is a valid value, which is<br />why the field is a pointer. |  | MaxLength: 4096 <br />Optional: \{\} <br /> |
| `valueFrom` _[SeatCredentialSource](#seatcredentialsource)_ | valueFrom is where a credential comes from: a Secret key, or, for a<br />Seat's own credentials, the engine (provisioned). For a header the<br />value is the whole header, scheme included (for example "Bearer<br /><token>"); a provisioned value is the credential alone, so provision<br />one for a variable rather than a header. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### MCPCredentials



MCPCredentials are the values one tool server is started with for a seat:
environment variables for a Stdio server, request headers for an HTTP one.
They map onto one server's entry of Crewlet's mcp_env, a credential
position.

The server is named by a top-level name, as an imagePullSecrets entry names
its Secret, rather than by a nested reference, because name is the key of
the list that holds these: with a scalar key, server-side apply merges the
list entry by entry, so that one field manager (a control plane's GitHub
flow, say) can own the credentials of one server without owning, or
overwriting, every other server's.



_Appears in:_
- [SeatSpec](#seatspec)
- [UnitSpec](#unitspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the MCPServer, in this namespace, the<br />values are for. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `env` _[MCPCredentialValue](#mcpcredentialvalue) array_ | env are the values. For a Stdio server each is an environment<br />variable of its process; for an HTTP server each is a request header<br />(the name is the header's name). Names must differ in more than case:<br />HTTP header names are case-insensitive, so Authorization and<br />authorization are one header. |  | MaxItems: 32 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### MCPEnvVar



MCPEnvVar is one variable of a Stdio tool server's environment, which every
instance of the server starts with: a credential, read from a Secret, or a
setting that is no credential, written as a literal value, such as the
address of the service the server reaches or a switch that turns one of its
tools on.

Crewlet treats a server's whole environment as credentials, masking every
value on every read, so it cannot tell the two apart; a name can. A name
says it holds a credential when one of its parts, cut at every character
that is not a letter or a digit and read without case, is KEY, KEYS,
APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
CREDENTIALS, AUTH, OAUTH or PAT, which is how Crewlet itself reads a
credential's name (cliprofile.IsCredentialName), or when it is one of the
HTTP headers that carry one, Authorization, Proxy-Authorization and
Cookie; such a name takes valueFrom, never a literal. So GITLAB_TOKEN,
TAVILY_API_KEY and X-Api-Key come from Secrets, and JIRA_URL or
SLACK_MCP_ADD_MESSAGE_TOOL may be written out. The rule keeps a credential
from being typed where a setting goes; a credential under a name that says
nothing (a URL with a password in it, say) is still yours to keep in a
Secret.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `value` _string_ | value is a literal value, for a setting that is no credential. It<br />lands in the company document as written, where anyone who can read<br />this object reads it too. An empty string is a valid value, which is<br />why the field is a pointer. |  | MaxLength: 4096 <br />Optional: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom reads a credential from a Secret key. |  | Optional: \{\} <br /> |


#### MCPHeader



MCPHeader is one header of every request to an HTTP tool server, which every
instance of the server sends: a credential, read from a Secret, such as the
Authorization header, or a setting that is no credential, written as a
literal value, by the rule MCPEnvVar states.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the header's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9!#$%&'*+.^_\|~-]+$` <br />Required: \{\} <br /> |
| `value` _string_ | value is a literal value, for a setting that is no credential. It<br />lands in the company document as written, where anyone who can read<br />this object reads it too. An empty string is a valid value, which is<br />why the field is a pointer. |  | MaxLength: 4096 <br />Optional: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom reads a credential from a Secret key, which holds the whole<br />header value, scheme included (for example "Bearer <token>"). |  | Optional: \{\} <br /> |


#### MCPServer



MCPServer is a Model Context Protocol tool server a company's agents call.

The name crewlet is reserved: the engine gives a seat's own tool bridge that
name inside a coding agent's sandbox.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `MCPServer` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[MCPServerSpec](#mcpserverspec)_ | spec is the server's desired state. |  | Required: \{\} <br /> |
| `status` _[MCPServerStatus](#mcpserverstatus)_ | status is the server's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### MCPServerSpec



MCPServerSpec is one tool server a company's agents call. It renders to one
entry of Crewlet's mcp_servers, named by the MCPServer's metadata.name;
Seats and Units give it per-seat values by that name.



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this server belongs to. Immutable. |  | Required: \{\} <br /> |
| `transport` _[MCPTransport](#mcptransport)_ | transport is how the engine reaches the server. Crewlet's `transport`. | Stdio | Enum: [Stdio HTTP] <br />Optional: \{\} <br /> |
| `shared` _boolean_ | shared launches one instance whose tools every agent calls. false<br />makes the server a template instead: every agent seat that names it<br />in spec.mcp, its own or its unit's, gets an instance of its own,<br />which is how each agent acts as itself on the server. An entry of<br />settings alone starts one too, with none of the seat's credentials.<br />Crewlet's `shared`. | true | Optional: \{\} <br /> |
| `command` _string_ | command is the executable a Stdio server is launched with, as a process<br />of the engine's container, by the engine's user. It must be installed<br />in the engine image, and Crewlet's image ships no Python or Node.js<br />runtime (docs/guides/tool-servers.md). Crewlet's `command`. |  | MaxLength: 1024 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `args` _string array_ | args are the executable's arguments. Crewlet's `args`. |  | MaxItems: 64 <br />items:MaxLength: 4096 <br />Optional: \{\} <br /> |
| `env` _[MCPEnvVar](#mcpenvvar) array_ | env is the environment of a Stdio server's process, shared by every<br />instance; per-seat values belong in a Seat's or a Unit's spec.mcp.<br />Crewlet's `env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `url` _string_ | url is the endpoint of an HTTP server. Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `headers` _[MCPHeader](#mcpheader) array_ | headers are sent with every request to an HTTP server, shared by every<br />instance; per-seat values belong in a Seat's or a Unit's spec.mcp.<br />Header names are case-insensitive, so names must differ in more than<br />case. Crewlet's `headers`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `toolPrefix` _string_ | toolPrefix is put in front of the server's tool names, to keep them<br />apart from another server's. Crewlet's `tool_prefix`. |  | MaxLength: 32 <br />MinLength: 1 <br />Pattern: `^[A-Za-z][A-Za-z0-9_-]*$` <br />Optional: \{\} <br /> |
| `toolAnnotations` _[MCPToolAnnotations](#mcptoolannotations) array_ | toolAnnotations correct the hints individual tools advertise.<br />Crewlet's `tool_annotations`. |  | MaxItems: 128 <br />Optional: \{\} <br /> |
| `startupTimeoutSeconds` _integer_ | startupTimeoutSeconds bounds starting the server, the handshake and<br />its first tool listing. Unset takes Crewlet's default of 60. Crewlet's<br />`startup_timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `requestTimeoutSeconds` _integer_ | requestTimeoutSeconds bounds one tool call. Unset takes Crewlet's<br />default of 300. Crewlet's `request_timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### MCPServerStatus



MCPServerStatus is what the operator observed about an MCPServer.

_Validation:_
- MinProperties: 1

_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists), ResolvedRefs (every<br />Secret key exists) and Applied (the server is in the revision the<br />engine runs). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### MCPToolAnnotations



MCPToolAnnotations corrects the behavioural hints one tool advertises. The
engine decides from them what a tool may be used for, such as whether a
delegated worker may call it. Each field maps onto Crewlet's
tool_annotations entry; unset keeps what the server says.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `tool` _string_ | tool is the tool's name as the server publishes it, without any<br />prefix. |  | MaxLength: 128 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `readOnly` _boolean_ | readOnly marks a tool that changes nothing. Crewlet's `read_only`. |  | Optional: \{\} <br /> |
| `destructive` _boolean_ | destructive marks a tool whose effect cannot be undone. Crewlet's<br />`destructive`. |  | Optional: \{\} <br /> |
| `idempotent` _boolean_ | idempotent marks a tool that can be repeated safely. Crewlet's<br />`idempotent`. |  | Optional: \{\} <br /> |
| `openWorld` _boolean_ | openWorld marks a tool that reaches outside the company. Crewlet's<br />`open_world`. |  | Optional: \{\} <br /> |


#### MCPTransport

_Underlying type:_ _string_

MCPTransport is how the engine reaches a tool server. It maps onto
Crewlet's mcp_servers[].transport (Stdio is `stdio`, HTTP is `http`).

_Validation:_
- Enum: [Stdio HTTP]

_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description |
| --- | --- |
| `Stdio` | MCPTransportStdio launches the server as a process next to the engine<br />and speaks to it over its standard input and output. The command must<br />exist in the engine image.<br /> |
| `HTTP` | MCPTransportHTTP connects to a remote server over Streamable HTTP.<br /> |


#### ManagedKind

_Underlying type:_ _string_

ManagedKind is the kind of object a seat manages.

_Validation:_
- Enum: [Seat Unit]

_Appears in:_
- [ManagedReference](#managedreference)

| Field | Description |
| --- | --- |
| `Seat` | ManagedKindSeat is a single Seat.<br /> |
| `Unit` | ManagedKindUnit is every seat of a Unit and of the units below it.<br /> |


#### ManagedReference



ManagedReference names a Seat or a Unit, in the referring Seat's namespace,
that a seat manages.



_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `kind` _[ManagedKind](#managedkind)_ | kind is the kind of object named. | Seat | Enum: [Seat Unit] <br />Optional: \{\} <br /> |
| `name` _string_ | name is the object's metadata.name. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### ManagementPolicy

_Underlying type:_ _string_

ManagementPolicy says who owns a company's live document.

_Validation:_
- Enum: [Enforce Observe Seed]

_Appears in:_
- [ConfigSettings](#configsettings)

| Field | Description |
| --- | --- |
| `Enforce` | ManagementPolicyEnforce makes the operator the document's only<br />writer. The engine refuses every other credential a change to the<br />document (403 config_managed), the published access token and a person<br />signed in at its dashboard included, and its dashboard shows the<br />document read-only; a revision made some other way all the same (the<br />engine's own writes, or one made before the data pod restarted onto the<br />policy) is reported and overwritten.<br /> |
| `Observe` | ManagementPolicyObserve renders and compares, and never writes; a<br />difference is reported only. The engine's own users may change the<br />document.<br /> |
| `Seed` | ManagementPolicySeed writes the document once, into an engine that<br />has none, and then leaves it to the engine and its dashboard, whose<br />users may change it. Changes to the Company's children are ignored<br />afterwards and say so in their status.<br /> |


#### MattermostEngineProvisioning



MattermostEngineProvisioning is how the engine provisions the seats'
Mattermost bots under provisioning Engine. It maps onto Crewlet's
integrations.mattermost.provisioning: for each agent seat whose
spec.integrations.mattermost.botToken is provisioned, the engine creates a
bot, adds it to the team and the channels, and mints its token into that
value.



_Appears in:_
- [MattermostIntegration](#mattermostintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `adminTokenSecretRef` _[SecretKeyReference](#secretkeyreference)_ | adminTokenSecretRef is the Secret key holding a system<br />administrator's personal access token, which creates and disables the<br />bots. Crewlet's `admin_token`, a credential position. |  | Required: \{\} <br /> |
| `usernamePrefix` _string_ | usernamePrefix is put before each seat's handle to form its bot's<br />username, for a server shared with people. Crewlet's<br />`username_prefix`. |  | MaxLength: 32 <br />MinLength: 1 <br />Pattern: `^[a-z0-9._-]+$` <br />Optional: \{\} <br /> |
| `channels` _string array_ | channels are channels, by name (the URL slug), every bot joins: a bot<br />hears only the channels it is in. Crewlet's `channels`. |  | MaxItems: 64 <br />items:MaxLength: 64 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9][a-z0-9_-]*$` <br />Optional: \{\} <br /> |
| `displayNameSuffix` _string_ | displayNameSuffix is put after each bot's display name, such as<br />" (AI)", to tell agents from people at a glance. Crewlet's<br />`display_name_suffix`. |  | MaxLength: 64 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### MattermostIntegration



MattermostIntegration turns Mattermost on for a company. It maps onto
Crewlet's integrations.mattermost. Mattermost delivers no webhook the engine
can use: the engine holds a websocket per seat's bot instead, so the
integration needs no public address. Each seat's bot is in its Seat's
spec.integrations.mattermost.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | url is the address the engine reaches the server at, such as<br />https://chat.example.com: each seat's websocket, the REST calls and<br />the bots' provisioning go to it. It need not be the server's Site URL,<br />which people browse and from which Mattermost builds its links; the<br />engine builds none from url. Any route to the server serves, such as<br />its in-cluster Service: the engine's websockets carry no Origin<br />header, so Mattermost's origin check, which holds a browser to the<br />Site URL, never applies to them. The server has to answer its API at<br />url without a redirect, which neither the engine nor the seats'<br />Mattermost tool server, given the same address as its MATTERMOST_URL,<br />follows: on the scheme that address is served on, and at the Site<br />URL's path, outside which Mattermost redirects every request.<br />Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `team` _string_ | team is the team, by name (the URL slug), the bots belong to.<br />Crewlet's `team`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9_-]*$` <br />Required: \{\} <br /> |
| `typingStatus` _[WorkingStatus](#workingstatus)_ | typingStatus is when a seat shows that it is typing. Unset takes<br />Crewlet's default, Always; each indicator costs a request every few<br />seconds while the seat works. Crewlet's `typing_status`. |  | Enum: [Always Addressed] <br />Optional: \{\} <br /> |
| `engineProvisioning` _[MattermostEngineProvisioning](#mattermostengineprovisioning)_ | engineProvisioning is how the engine creates the seats' bots.<br />Required with provisioning Engine, and not allowed with External. |  | Optional: \{\} <br /> |


#### MattermostSeatIntegration



MattermostSeatIntegration is a seat's own Mattermost bot. It maps onto
Crewlet's role integrations.mattermost and needs the company's Mattermost
Integration. One credential covers everything the bot does: the websocket
the engine listens on, its REST calls, and the Mattermost tool server, whose
credentials (in spec.mcp) name the same token.



_Appears in:_
- [SeatIntegrations](#seatintegrations)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `botToken` _[SeatCredentialSource](#seatcredentialsource)_ | botToken is the bot account's personal access token: from a Secret,<br />or provisioned by the engine under a Mattermost Integration with<br />provisioning Engine, which creates the bot and mints the token.<br />Crewlet's `bot_token`, a credential position. |  | MinProperties: 1 <br />Required: \{\} <br /> |
| `username` _string_ | username is the bot's username, for a bot that exists under a name<br />other than the seat's handle (with the Integration's<br />engineProvisioning.usernamePrefix before it). Crewlet's `username`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9._-]*$` <br />Optional: \{\} <br /> |
| `channel` _string_ | channel is a channel, by its name (the URL slug), the engine adds the<br />bot to when it provisions it, on top of the Integration's<br />engineProvisioning.channels. Crewlet's `channel`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9_-]*$` <br />Optional: \{\} <br /> |


#### NamespacedSecretKeyReference



NamespacedSecretKeyReference selects one key of a Secret in a named
namespace. Only the cluster-scoped EngineClass uses it: a platform-owned
value (a managed LLM key, telemetry headers) lives in a namespace the
platform controls rather than in each tenant's. The value itself still
reaches every tenant that runs with it (see the field that references
it); the namespace keeps tenants from the platform's Secret, not from the
value.



_Appears in:_
- [NamespacedSecretValueSource](#namespacedsecretvaluesource)
- [PlatformLLMProvider](#platformllmprovider)
- [TelemetrySettings](#telemetrysettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespace` _string_ | namespace is the namespace the Secret is in. |  | MaxLength: 63 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `name` _string_ | name is the metadata.name of the Secret. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `key` _string_ | key is the key within the Secret's data. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[-._a-zA-Z0-9]+$` <br />Required: \{\} <br /> |




#### NamespacedSecretValueSource



NamespacedSecretValueSource says where a value set by the cluster-scoped
EngineClass comes from: a Secret in a namespace the platform controls.



_Appears in:_
- [ClassEnvVar](#classenvvar)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretKeyRef` _[NamespacedSecretKeyReference](#namespacedsecretkeyreference)_ | secretKeyRef selects the Secret key that holds the value. |  | Required: \{\} <br /> |


#### NativeTrackerSettings



NativeTrackerSettings is the policy of the engine's own tracker. It maps
onto Crewlet's tracker.native.

_Validation:_
- MinProperties: 1

_Appears in:_
- [TrackerSettings](#trackersettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `inboxRetentionDays` _integer_ | inboxRetentionDays is how long a person's inbox keeps a row; the<br />history the row points at is kept regardless. Unset takes Crewlet's<br />default of 365. Crewlet's `inbox_retention_days`. |  | Maximum: 3650 <br />Minimum: 30 <br />Optional: \{\} <br /> |


#### NetworkPolicyEgress



NetworkPolicyEgress is where the engine may connect to, beyond DNS and
its own namespace.

_Validation:_
- MinProperties: 1

_Appears in:_
- [NetworkPolicySettings](#networkpolicysettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `exceptCIDRs` _string array_ | exceptCIDRs are address blocks the engine may not reach, besides the<br />cloud metadata endpoints. List the cluster's pod, Service and node<br />networks here: a NetworkPolicy has no other way to tell an address in<br />the cluster from one outside it, and without them an engine reaches<br />other tenants' pods, the Kubernetes API server and the nodes. Each is<br />a canonical IPv4 or IPv6 CIDR, narrower than the whole address space. |  | MaxItems: 32 <br />MinItems: 1 <br />items:MaxLength: 43 <br />Optional: \{\} <br /> |
| `to` _[NetworkPolicyPeer](#networkpolicypeer) array_ | to are pods in the cluster the engine may reach although<br />exceptCIDRs covers them: an in-cluster model gateway, the telemetry<br />collector, an object store. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### NetworkPolicyMode

_Underlying type:_ _string_

NetworkPolicyMode is whether the operator restricts traffic to the engine.

_Validation:_
- Enum: [Isolated None]

_Appears in:_
- [NetworkPolicySettings](#networkpolicysettings)

| Field | Description |
| --- | --- |
| `Isolated` | NetworkPolicyIsolated admits traffic to the engine's API port only<br />from the operator's pods, from the namespaces the Gateway proxies or<br />Ingress controller of the class's API route run in<br />(gateway.proxyNamespaces, ingress.controllerNamespaces), and from the<br />class's apiConsumers; to its public port only from where the webhooks'<br />route arrives from (the same fields of exposure.webhooks, or<br />webhooks.external.forwarders); and<br />lets the engine connect only as networkPolicy.egress says. It needs a<br />CNI that enforces NetworkPolicy, egress included.<br /> |
| `None` | NetworkPolicyNone creates no NetworkPolicy.<br /> |


#### NetworkPolicyPeer



NetworkPolicyPeer is the pods its podSelector selects in the namespaces
its namespaceSelector selects, as a NetworkPolicy peer that sets both.



_Appears in:_
- [ExternalExposure](#externalexposure)
- [NetworkPolicyEgress](#networkpolicyegress)
- [NetworkPolicySettings](#networkpolicysettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `namespaceSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#labelselector-v1-meta)_ | namespaceSelector selects namespaces by their labels; an empty one<br />selects every namespace. Select a namespace by name with the label<br />kubernetes.io/metadata.name, which the API server sets on every<br />namespace. |  | Required: \{\} <br /> |
| `podSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#labelselector-v1-meta)_ | podSelector selects pods in those namespaces by their labels. Unset<br />selects every pod in them. |  | Optional: \{\} <br /> |


#### NetworkPolicySettings



NetworkPolicySettings is the NetworkPolicy the operator puts around each
company's engine pods.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[NetworkPolicyMode](#networkpolicymode)_ | mode is whether the operator restricts the engine's traffic. | Isolated | Enum: [Isolated None] <br />Optional: \{\} <br /> |
| `apiConsumers` _[NetworkPolicyPeer](#networkpolicypeer) array_ | apiConsumers are the pods admitted to the engine's API port besides<br />the operator and the Gateway proxies or Ingress controller of the<br />class's API route, and to nothing else of the engine (the public<br />port, which the webhooks reach, admits only where they arrive from):<br />the platform's own components (a control plane, a dashboard) that<br />call the engines of this class's companies. Each holds an access<br />token, which the engine still requires. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `egress` _[NetworkPolicyEgress](#networkpolicyegress)_ | egress narrows where the engine may connect to. The engine runs code<br />the tenant supplies (stdio MCP servers, sandbox commands, what its<br />agents write), so under Isolated its pods may reach DNS, the pods of<br />their own namespace, and every address outside the cluster except the<br />cloud metadata endpoints (169.254.169.254, fd00:ec2::254), which hand<br />out the node's credentials. What else is in the cluster is<br />unreachable only if listed in exceptCIDRs. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### ObjectBackend

_Underlying type:_ _string_

ObjectBackend is where a company's files are kept. It maps onto Crewlet's
store.objects.backend (NATS is `nats`, S3 is `s3`).

_Validation:_
- Enum: [NATS S3]

_Appears in:_
- [ObjectSettings](#objectsettings)

| Field | Description |
| --- | --- |
| `NATS` | ObjectBackendNATS keeps files in a bucket on the company's own event<br />stream, on the data volume.<br /> |
| `S3` | ObjectBackendS3 keeps files in an S3-compatible bucket.<br /> |


#### ObjectSettings



ObjectSettings is where a class's companies keep their files.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backend` _[ObjectBackend](#objectbackend)_ | backend is where files are kept. Changing it does not move existing<br />files. | NATS | Enum: [NATS S3] <br />Optional: \{\} <br /> |
| `s3` _[S3ObjectSettings](#s3objectsettings)_ | s3 is the bucket, for backend S3. |  | Optional: \{\} <br /> |


#### PlatformLLMProvider



PlatformLLMProvider is a model provider the platform offers to every
Company of a class that sets spec.llm.usePlatformProviders. Its keys live in
a namespace the platform controls and are pushed into the secret store of
each opted-in company's engine.

That store is the tenant's to read: the engine's API reveals any value in
it to whoever holds the company's access token (status.apiAccessSecretRef
of the Company), and code the tenant runs in the engine's pod can open it
with the keyring the pod holds. So every opted-in tenant can read these
keys. Offer keys you would give each of them: one per tenant, or the
per-tenant virtual keys of an LLM gateway, never the platform's own.



_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the provider's name. Seats and the Company's model order refer<br />to it with kind PlatformLLMProvider. It may not be default, which<br />Crewlet would run every chainless seat of every opted-in company on,<br />whatever the company's own order says. |  | MaxLength: 63 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Required: \{\} <br /> |
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible CLIAgent] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. Crewlet trims it of white space and refuses a<br />provider with nothing left, as naming no model. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _[ClaudeModelID](#claudemodelid)_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | Enum: [claude-fable-5-1 claude-mythos-5-1 claude-fable-5 claude-mythos-5 claude-opus-5-5 claude-opus-5 claude-opus-4-8 claude-opus-4-7 claude-sonnet-5-5 claude-sonnet-5 claude-opus-4-6 claude-sonnet-4-6 claude-opus-4-5 claude-sonnet-4-5 claude-haiku-4-5 claude-opus-4-1 claude-opus-4-0 claude-opus-4 claude-sonnet-4-0 claude-sonnet-4 claude-3-7-sonnet] <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. For type<br />Anthropic it is the gateway's root, without /v1: Crewlet's own calls,<br />Claude Code (as ANTHROPIC_BASE_URL) and OpenCode each add the version<br />themselves. A seat's code work running on the provider hands it to the<br />coding agent in each box. Crewlet's `base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `reasoning` _boolean_ | reasoning turns on an OpenAI reasoning model's effort, so that<br />reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only. |  | Optional: \{\} <br /> |
| `reasoningEffort` _[ReasoningEffort](#reasoningeffort)_ | reasoningEffort is how hard the model thinks. For Anthropic it is sent<br />on every call, and unset means High. For OpenAI it is sent only while<br />reasoning is true, so it requires reasoning; an OpenAICompatible<br />provider cannot turn reasoning on and takes no reasoningEffort.<br />Crewlet's `reasoning_effort`. |  | Enum: [Low Medium High XHigh Max] <br />Optional: \{\} <br /> |
| `reasoningBudgetTokens` _integer_ | reasoningBudgetTokens is the thinking budget of an older, budget-based<br />Claude model. Unset means such a model does not think. Crewlet's<br />`reasoning_budget_tokens`; type Anthropic only. |  | Minimum: 1024 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the<br />longest silence in a streamed one. Unset takes Crewlet's default of<br />600. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `cooldowns` _[LLMCredentialCooldowns](#llmcredentialcooldowns)_ | cooldowns is how long a failing key is set aside. Crewlet's<br />`cooldowns`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiKeySecretRefs` _[NamespacedSecretKeyReference](#namespacedsecretkeyreference) array_ | apiKeySecretRefs are the provider's API keys, rotated as an<br />LLMProvider's are. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### ProvisionedCredential



ProvisionedCredential is a seat's credential the engine creates: an
Integration with provisioning Engine (GitLab, Mattermost, Datadog,
Atlassian) makes the seat's account at the vendor, mints the account's
credential, and seals it in the engine's own secret store. The company
document refers to it by a name the operator derives from the seat's handle
and this name, and nothing else ever writes that store entry: the operator
pushes no value into it and deletes none it finds there.



_Appears in:_
- [SeatCredentialSource](#seatcredentialsource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name names the value among the seat's provisioned credentials. Every<br />place a seat names the same provisioned name refers to one value, so<br />a token given to a tool server's environment and to the seat's<br />sandbox is one token; different names are different values, such as<br />an account's token and its email address. |  | MaxLength: 32 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9-]*$` <br />Required: \{\} <br /> |


#### ReasoningEffort

_Underlying type:_ _string_

ReasoningEffort is how hard a model thinks. It maps onto Crewlet's
`reasoning_effort` (Low is `low`, XHigh is `xhigh`, and so on).

_Validation:_
- Enum: [Low Medium High XHigh Max]

_Appears in:_
- [LLMProviderSettings](#llmprovidersettings)
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)

| Field | Description |
| --- | --- |
| `Low` |  |
| `Medium` |  |
| `High` |  |
| `XHigh` |  |
| `Max` |  |


#### RetentionSettings



RetentionSettings is who answers for a class's companies' backups.



_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backupOwner` _string_ | backupOwner names whoever is responsible for backing up the companies'<br />data volumes; the engine warns while none is named. Crewlet's<br />retention.backup_owner. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### RevisionPlacement



RevisionPlacement is where a revision of the company document pins its
agent seats: the values of the two placement labels its roles name. Only
values that can name one of the company's pods are recorded: a pool name
and a seat handle.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ConfigStatus](#configstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pools` _string array_ | pools are the values of the crewlet.ai/pool placement label: data,<br />and each Shared pool a seat is pinned to. |  | MaxItems: 64 <br />items:MaxLength: 20 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |
| `seats` _string array_ | seats are the values of the crewlet.ai/seat placement label: the<br />handle of each seat pinned to its own pods in a PerSeat pool. A<br />revision that pins more seats than the list holds records the first<br />in handle order; the pods of the others are kept only while their<br />seats stay in a PerSeat pool. |  | MaxItems: 1024 <br />items:MaxLength: 63 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9][a-z0-9-]*$` <br />Optional: \{\} <br /> |


#### S3ObjectSettings



S3ObjectSettings is the S3-compatible bucket a class's companies keep their
files in. Each field maps onto the same-named field of Crewlet's
store.objects.s3.

The bucket's credentials are each Company's own (its
spec.objects.credentialsSecretRef), not the class's: whatever reaches a
company's engine pod is readable by code the tenant runs there (a stdio
MCP server, a sandbox setup command), so a key the class shared would let
every tenant read and write every other tenant's files.



_Appears in:_
- [ObjectSettings](#objectsettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | endpoint is the S3 API's base URL. Unset is Amazon S3's own endpoint<br />for the region. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `region` _string_ | region is the bucket's region. Every request is signed for one, even<br />where the provider ignores it (Cloudflare R2: auto, MinIO: us-east-1). |  | MaxLength: 64 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `bucket` _string_ | bucket is the bucket's name. |  | MaxLength: 63 <br />MinLength: 3 <br />Required: \{\} <br /> |
| `prefix` _string_ | prefix is Crewlet's store.objects.s3.prefix, prepended to every key as<br />it is, so that one bucket holds many companies. It is a template:<br />\{\{namespace\}\}, \{\{name\}\} and \{\{uid\}\} are replaced by the Company's<br />namespace, name and UID, and the result must differ between<br />companies, so it must contain \{\{uid\}\}, or \{\{namespace\}\} and \{\{name\}\}<br />joined by a '/' or '.', which neither can hold (with a '-' between<br />them, team-a/b-web and team-a-b/web would share keys); and it ends<br />with '/', so that no company's prefix starts another's. The default<br />keys files by namespace and name, which is also what the data volumes<br />are named by: a Company re-created under its old name after a Retain<br />deletion finds both its estate and its files again. | \{\{namespace\}\}/\{\{name\}\}/ | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `pathStyle` _boolean_ | pathStyle addresses the bucket in the URL path rather than in the host<br />name; MinIO and most self-hosted gateways need it. |  | Optional: \{\} <br /> |


#### SandboxFile



SandboxFile is a file written into a sandbox before its commands run. Its
content is a credential position in Crewlet (it typically holds a git or
registry login), so it always comes from a Secret.



_Appears in:_
- [SandboxSetupStep](#sandboxsetupstep)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ | path is where the file is written inside the sandbox. |  | MaxLength: 1024 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom is the Secret key holding the file's content. |  | Required: \{\} <br /> |


#### SandboxSettings



SandboxSettings is where a company's seats run code. It maps onto Crewlet's
providers.sandbox.

v1alpha1 offers Crewlet's remote sandbox (E2B) only. Crewlet's local
placements are not offered: `direct` would run every seat's code inside the
engine pod with the engine's own credentials, and `container` needs a
container runtime a pod does not have.



_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `e2b` _[E2BSandbox](#e2bsandbox)_ | e2b is the remote sandbox service. |  | Required: \{\} <br /> |
| `defaultCodingAgent` _[CodingAgent](#codingagent)_ | defaultCodingAgent is the coding agent of seats that name none.<br />Crewlet's `default_coding_agent`. |  | Enum: [ClaudeCode OpenCode] <br />Optional: \{\} <br /> |
| `defaultTimeoutSeconds` _integer_ | defaultTimeoutSeconds is how long a sandbox outlives an engine that<br />stopped renewing it. It never stops a running job. Crewlet's<br />`default_timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `defaultPauseTTLSeconds` _integer_ | defaultPauseTTLSeconds is how long a sandbox waiting on a person's<br />answer stays paused before it is reclaimed. Unset takes Crewlet's<br />default of 1800; 0 never pauses. Crewlet's<br />`default_pause_ttl_seconds`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `defaultMaxTurns` _integer_ | defaultMaxTurns caps the agentic rounds of a coding run on seats that<br />name none. Unset or 0 is uncapped. Crewlet's `default_max_turns`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `setup` _[SandboxSetupStep](#sandboxsetupstep) array_ | setup is applied to every sandbox before each seat's own steps.<br />Crewlet's `setup`. |  | MaxItems: 16 <br />Optional: \{\} <br /> |


#### SandboxSetupStep



SandboxSetupStep is one provisioning step applied to a sandbox before the
coding agent starts. It maps onto a Crewlet sandbox setup step.



_Appears in:_
- [SandboxSettings](#sandboxsettings)
- [SeatSandbox](#seatsandbox)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name identifies the step in logs and failure messages. |  | MaxLength: 63 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `files` _[SandboxFile](#sandboxfile) array_ | files are written into the sandbox before the commands run. Crewlet's<br />`files`, a credential position. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `commands` _string array_ | commands are shell commands run in order; one that exits non-zero<br />fails the sandbox. Crewlet's `commands`. Crewlet substitutes every<br />$\{NAME\} in them (NAME an identifier) from the engine's own secret<br />store and environment before the sandbox runs them, so $\{HOME\} would be<br />the engine's, not the sandbox's, and the API refuses $\{NAME\}: write a<br />sandbox variable as $NAME. Parameter expansions such as $\{1:-x\} are<br />not substituted and are allowed. |  | MaxItems: 32 <br />items:MaxLength: 4096 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `env` _[SecretEnvVar](#secretenvvar) array_ | env is merged into the environment of the commands and of the run.<br />Crewlet's `env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `brief` _string_ | brief tells the coding agent what this step made true about the<br />sandbox. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds caps each command of the step. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### Schedule



Schedule is recurring work a seat or a unit owns, fired on a cron schedule:
the CronJob of a company.

The Schedule's metadata.name is the schedule's name in the engine, which
is half of the identity the engine claims each fire under, so that a fire
runs once however often the engine evaluates its minute. Two Schedules of
one seat or unit therefore never share a name, and a fire is never run
twice.

A fire that comes due while its seat is paused is skipped, not queued.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Schedule` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[ScheduleSpec](#schedulespec)_ | spec is the schedule's desired state. |  | Required: \{\} <br /> |
| `status` _[ScheduleStatus](#schedulestatus)_ | status is the schedule's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### ScheduleFanOut

_Underlying type:_ _string_

ScheduleFanOut is who runs a schedule that targets a unit. It maps onto
Crewlet's schedule `target` (Each is `each`, Lead is `lead`).

_Validation:_
- Enum: [Each Lead]

_Appears in:_
- [ScheduleTarget](#scheduletarget)

| Field | Description |
| --- | --- |
| `Each` | ScheduleFanOutEach runs one task per agent seat directly in the unit:<br />never the seats of the units below it, and never a Human seat. Each<br />seat runs its own turn, so a slow one never holds up the others.<br /> |
| `Lead` | ScheduleFanOutLead runs one task as the unit's lead: its leadRef, or,<br />unset, the lead it inherits from the units above it. It follows a<br />change of lead without a change to the Schedule.<br /> |


#### ScheduleSpec



ScheduleSpec is recurring work a seat or a unit owns: a task the engine
hands its runner on a cron schedule, as a CronJob runs a Job. It renders to
one entry of a Crewlet role's or unit's `schedules`, whose `name` is the
Schedule's metadata.name.



_Appears in:_
- [Schedule](#schedule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this schedule belongs to. Immutable. |  | Required: \{\} <br /> |
| `target` _[ScheduleTarget](#scheduletarget)_ | target is the seat or the unit whose schedule this is. |  | MinProperties: 1 <br />Required: \{\} <br /> |
| `schedule` _string_ | schedule is when the task fires: a standard cron expression of five<br />fields (minute, hour, day of month, month, day of week), such as<br />"30 9 * * 1-5", evaluated in timeZone. When both the day of month and<br />the day of week are restricted, a day matches if either does. The<br />engine's dry run checks each field's values. Crewlet's `cron`. |  | MaxLength: 256 <br />MinLength: 9 <br />Required: \{\} <br /> |
| `timeZone` _string_ | timeZone is the IANA time zone the schedule is evaluated in, such as<br />Asia/Tokyo. Unset is the Company's spec.timeZone, which is UTC when<br />that is unset too. The engine refuses a zone it cannot load, and<br />Local, which would be whatever zone the engine's host is set to.<br />Crewlet's `timezone`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$` <br />Optional: \{\} <br /> |
| `prompt` _string_ | prompt is the task the runner is handed each time the schedule fires,<br />such as "Post your standup: what shipped yesterday, what is next,<br />what blocks you." Crewlet's `task`. |  | MaxLength: 8192 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `suspend` _boolean_ | suspend keeps the schedule in the company without firing it. Unset<br />is false. Crewlet's `enabled`, inverted. |  | Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds caps one scheduled turn's wall-clock time: a turn past<br />it starts no further round and ends failed. Unset is the engine's<br />default, 180 seconds. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `catchUp` _boolean_ | catchUp says whether an engine that starts after missing a fire runs<br />the most recent missed one, when it is recent enough (half the<br />schedule's period, within the engine's catch-up bounds); older misses<br />are never run. Unset is true. Crewlet's `catchup`. |  | Optional: \{\} <br /> |


#### ScheduleStatus



ScheduleStatus is what the operator observed about a Schedule.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Schedule](#schedule)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists), ResolvedRefs (the seat<br />or unit it targets exists, and a seat is an Agent) and Applied (the<br />schedule is in the revision the engine runs). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### ScheduleTarget



ScheduleTarget is whose recurring work a schedule is: one seat's, or a
unit's. It decides which entry of the company document the schedule is
rendered into: the seat's role, or the unit's.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ScheduleSpec](#schedulespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `seatRef` _[LocalObjectReference](#localobjectreference)_ | seatRef names the Seat, in this namespace, whose schedule this is; it<br />always runs as that seat. The Seat must be an Agent: the engine runs<br />no turn for a person. Rendered into the seat's role `schedules`. |  | Optional: \{\} <br /> |
| `unitRef` _[LocalObjectReference](#localobjectreference)_ | unitRef names the Unit, in this namespace, whose schedule this is;<br />fanOut says who in it runs it. A unit's schedule is not inherited by<br />the units below it. Rendered into the unit's `schedules`. |  | Optional: \{\} <br /> |
| `fanOut` _[ScheduleFanOut](#schedulefanout)_ | fanOut is who in the unit runs it. Unset is Each. Crewlet's `target`.<br />The engine refuses a schedule that could never run while it is not<br />suspended: Each on a unit with no agent seat directly in it, and Lead<br />on a unit whose lead is a Human seat. |  | Enum: [Each Lead] <br />Optional: \{\} <br /> |


#### Seat



Seat is one seat of a company's org chart: an AI agent the engine runs, or a
person agents work with.

spec.paused pauses an agent seat while keeping it in the chart; Company's
spec.suspend stops the whole engine instead.

Unless spec.handle is set, the Seat's metadata.name is its handle and must
follow the handle's rules: lowercase letters, digits and '-', starting with
a letter or digit, at most 63 characters. The handle cannot change.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Seat` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[SeatSpec](#seatspec)_ | spec is the seat's desired state. |  | Required: \{\} <br /> |
| `status` _[SeatStatus](#seatstatus)_ | status is the seat's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SeatContact



SeatContact is how agents reach a person: their accounts on the company's
surfaces, which tie what the person does there to their seat. It maps onto
Crewlet's role `contact`, one field for each of its keys but one (below).

Crewlet refuses two seats that claim one account, so a Seat claiming an
account another Seat of the company already claims is reported as a
Conflict. Each field is compared as Crewlet compares it: githubLogin,
gitlabUsername and mattermostUsername without case, as GitHub, GitLab and
Mattermost compare them; slackUserID and atlassianAccount exactly.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `slackUserID` _string_ | slackUserID is the person's Slack member id, such as U0123ABCD.<br />Crewlet's `slack_user_id`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Z0-9]+$` <br />Optional: \{\} <br /> |
| `mattermostUsername` _string_ | mattermostUsername is the person's username on the company's<br />Mattermost, such as jane.doe, written without the @: the name an<br />agent mentions them by and opens a direct message with, and the<br />sender an inbound post is attributed by. Crewlet's<br />`mattermost_user_id`, which holds the username despite its name, and<br />never the 26-character user id. Mattermost's own rule for a username<br />holds it: lowercase letters, digits, '.', '-' and '_', at most 64<br />characters, and none of the names Mattermost reserves (all, channel,<br />matterbot, system) nor here: a mention of all, channel or here<br />notifies the whole channel rather than the person. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[a-z0-9._-]+$` <br />Optional: \{\} <br /> |
| `atlassianAccount` _string_ | atlassianAccount is the person's identity on the company's Jira and<br />Confluence, one value for both: their account id on Atlassian Cloud,<br />such as 557058:f58131cb-b67d-43c7-b30d-6b58d40bd077 and never their<br />email address, and their username on a Data Center instance, such as<br />jane.doe. Webhooks name the person by it, so it attributes what they do<br />there to their seat, and agents assign and mention them by it.<br />Crewlet's `atlassian_account_id`, which holds the username on Data<br />Center despite its name. Any value up to 255 characters without<br />whitespace is taken, since a Data Center username may hold almost any<br />character. Where the company reaches Jira or Confluence on Atlassian<br />Cloud, a value outside the form of an account id (letters, digits,<br />'_', '\|', ':' and '-', at most 128 characters) is applied and reported<br />on the Seat as ResolvedRefs=False with the reason NotAnAccountID. |  | MaxLength: 255 <br />MinLength: 1 <br />Pattern: `^\S+$` <br />Optional: \{\} <br /> |
| `githubLogin` _string_ | githubLogin is the person's GitHub login. Crewlet's `github_login`. |  | MaxLength: 39 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Optional: \{\} <br /> |
| `gitlabUsername` _string_ | gitlabUsername is the person's GitLab username, written without the<br />@: assignments, review requests, mentions, and the sender an inbound<br />webhook is attributed by. Crewlet's `gitlab_username`. GitLab's own<br />rule for a username's form holds it: letters, digits, '_', '-' and<br />'.', at most 255 characters, not starting with '-' and not ending<br />with '.'. |  | MaxLength: 255 <br />MinLength: 1 <br />Pattern: `^([A-Za-z0-9_.][A-Za-z0-9_.-]*[A-Za-z0-9_-]\|[A-Za-z0-9_])$` <br />Optional: \{\} <br /> |


#### SeatCredentialSource



SeatCredentialSource says where one of a seat's own credentials comes from:
a Secret key, or the engine, which provisions it (see
ProvisionedCredential).

_Validation:_
- MinProperties: 1

_Appears in:_
- [MCPCredentialValue](#mcpcredentialvalue)
- [MattermostSeatIntegration](#mattermostseatintegration)
- [SeatEnvVar](#seatenvvar)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretKeyRef` _[SecretKeyReference](#secretkeyreference)_ | secretKeyRef selects the Secret key that holds the value. |  | Optional: \{\} <br /> |
| `provisioned` _[ProvisionedCredential](#provisionedcredential)_ | provisioned is a value the engine creates and keeps, under an<br />Integration with provisioning Engine. |  | Optional: \{\} <br /> |


#### SeatEnvVar



SeatEnvVar is a named credential of a seat delivered as an environment
variable: from a Secret, or provisioned by the engine.



_Appears in:_
- [SeatSandbox](#seatsandbox)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `valueFrom` _[SeatCredentialSource](#seatcredentialsource)_ | valueFrom is where the value comes from. |  | MinProperties: 1 <br />Required: \{\} <br /> |


#### SeatIntegrations



SeatIntegrations is a seat's own identity on the company's integrations:
the chat app it speaks as, the code-host app it acts as. Its tool
credentials, the GitLab, Jira, Confluence and Datadog ones included, are in
spec.mcp instead, which is where Crewlet reads them.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `slack` _[SlackSeatIntegration](#slackseatintegration)_ | slack is the seat's own Slack app. |  | Optional: \{\} <br /> |
| `github` _[GitHubSeatIntegration](#githubseatintegration)_ | github is the seat's identity on GitHub. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `mattermost` _[MattermostSeatIntegration](#mattermostseatintegration)_ | mattermost is the seat's own Mattermost bot. |  | Optional: \{\} <br /> |


#### SeatIntegrationsStatus



SeatIntegrationsStatus is the addresses a seat's vendor apps must use.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatStatus](#seatstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `slack` _[SlackSeatStatus](#slackseatstatus)_ | slack is the Slack side of the seat. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `github` _[GitHubSeatStatus](#githubseatstatus)_ | github is the GitHub side of the seat. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SeatKind

_Underlying type:_ _string_

SeatKind is whether a seat is an AI agent or a person. It maps onto
Crewlet's role `kind` (Agent is `agent`, Human is `human`).

_Validation:_
- Enum: [Agent Human]

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description |
| --- | --- |
| `Agent` | SeatKindAgent is an AI agent: the engine runs it, with its own inbox,<br />models and tools.<br /> |
| `Human` | SeatKindHuman is a person: part of the chart (a lead, an escalation<br />target, a colleague to mention) whom the engine never runs.<br /> |


#### SeatLLM



SeatLLM is the model providers a seat runs on, one fallback chain per phase
of its work: when every key of the first provider is cooling down, the next
is tried. It maps onto Crewlet's per-phase `llm` mapping. A phase left unset
uses the default chain; a seat with no default chain runs on the first
provider of the Company's spec.llm.order alone, with no fallback.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `default` _[LLMProviderReference](#llmproviderreference) array_ | default is the chain of every phase not set below, including the<br />executor that does the seat's work. Crewlet's `default`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `review` _[LLMProviderReference](#llmproviderreference) array_ | review is the chain of the reviewer that judges the executor's work.<br />Crewlet's `review`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `subagent` _[LLMProviderReference](#llmproviderreference) array_ | subagent is the chain of the short-lived workers the seat delegates<br />to. Crewlet's `subagent`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `auxiliary` _[LLMProviderReference](#llmproviderreference) array_ | auxiliary is the chain of the cheap model used for reflection and<br />summaries. Crewlet's `auxiliary`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `judge` _[LLMProviderReference](#llmproviderreference) array_ | judge is the chain of the model that decides whether a turn that ran<br />out of rounds may have more. Crewlet's `judge`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |
| `sandbox` _[LLMProviderReference](#llmproviderreference) array_ | sandbox is the chain the coding agent in the seat's sandbox runs on.<br />Unset takes default, then the Company's model order. A coding run uses<br />the chain's head alone, as its model and its sign-in, so the head must<br />be a provider the coding agent can read: an Anthropic provider, or for<br />OpenCode an OpenAI or OpenAICompatible one too, or a CLIAgent provider<br />of the coding agent's own CLI. The operator refuses a CLIAgent provider<br />of another CLI (CodingAgentMismatch), and reports any other head the<br />coding agent cannot run on in the CodeWorkRunnable condition. Every<br />box of the seat carries the head's first API key, and under ClaudeCode<br />its baseURL, unless spec.sandbox.env signs ClaudeCode in itself: give<br />code work a provider whose key is scoped for it. Crewlet's `sandbox`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### SeatPauseStatus



SeatPauseStatus is a seat's pause, as the engine reports it.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatStatus](#seatstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pausedBy` _string_ | pausedBy is who paused the seat, as Crewlet records the author of a<br />write: the seat of the person who paused it, or token:<id> for a<br />token no seat is bound to. A pause the operator took reads<br />token:operator. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `pausedAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#time-v1-meta)_ | pausedAt is when the seat was paused, to the second. |  | Optional: \{\} <br /> |
| `reason` _string_ | reason is why, in the pauser's words. |  | MaxLength: 500 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `stopRunning` _boolean_ | stopRunning is whether the pause also ended the turn the seat was on. |  | Optional: \{\} <br /> |


#### SeatPlacement



SeatPlacement is where a seat runs.



_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pool` _string_ | pool is the pool of pods that runs the seat: data, the company's data<br />pods, or the name of one of the Company's spec.topology.seatPools.<br />The seat is pinned to the pool with Crewlet's role.placement, which<br />the engine never widens: while the pool runs no pod, the seat runs<br />nowhere and its messages wait for one. |  | MaxLength: 20 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Required: \{\} <br /> |


#### SeatPlacementStatus



SeatPlacementStatus is where the engine runs a seat.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatStatus](#seatstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pool` _string_ | pool is the pool of pods that runs the seat: spec.placement.pool, or<br />the Company's spec.topology.defaultSeatPool. The seat is rendered with<br />role.placement.labels \{crewlet.ai/pool: <pool>\}, which only the pool's<br />pods carry, or, in a PerSeat pool, \{crewlet.ai/seat: <handle>\}, which<br />only the seat's own pods carry. |  | MaxLength: 20 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `workload` _string_ | workload is the name of the StatefulSet whose pods may run the seat:<br />the data pods', the Shared pool's, or the seat's own in a PerSeat<br />pool. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `node` _string_ | node is the engine node, a pod's name, that holds the seat's lease<br />and runs it, as the engine's GET /fleet last reported. Unset while no<br />node holds it. |  | MaxLength: 64 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### SeatPool



SeatPool is a set of stateless pods that run seats: they hold no data, join
the company's event stream through the data pods, and keep a scratch store
they delete at every start. A seat runs in it when its Seat's
spec.placement.pool names it, or when it is the Company's defaultSeatPool
and the Seat names no pool. The class's spec.seatPools.policy must be
Allowed.

The operator runs a Shared pool as the StatefulSet
<company>-seats-<name>-<hash>, and each seat of a PerSeat pool as
<company>-seat-<handle>-<hash> (the hash keeps the name the Company's own
in a namespace another Company shares), started only
once the data pods are ready and rolled only after them. The pods serve
their probes alone, on the engine's port: a pod is ready once its engine
has joined the company's fleet and may claim seats, and a rollout replaces
the next pod only then.



_Appears in:_
- [CompanyTopology](#companytopology)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name identifies the pool: a DNS label of at most 20 characters, other<br />than data, which is the data pods' pool. It names the pool's pods and<br />is the value of the crewlet.ai/pool label a Shared pool's seats are<br />pinned to. |  | MaxLength: 20 <br />MinLength: 1 <br />Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Required: \{\} <br /> |
| `mode` _[SeatPoolMode](#seatpoolmode)_ | mode is how the pool runs its seats. | Shared | Enum: [Shared PerSeat] <br />Optional: \{\} <br /> |
| `replicas` _integer_ | replicas is, for a Shared pool, how many pods it runs, 0 to 100: at 0<br />its seats run nowhere, and their messages wait for a pod. For a<br />PerSeat pool it is how many pods it runs for each of its seats, 1 or<br />2. | 1 | Maximum: 100 <br />Minimum: 0 <br />Optional: \{\} <br /> |
| `maxConcurrentTurns` _integer_ | maxConcurrentTurns is how many turns one of the pool's pods runs at<br />once: Crewlet's node.max_concurrent. Unset takes the class's<br />seatPools.defaults. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `terminationGracePeriodSeconds` _integer_ | terminationGracePeriodSeconds is how long one of the pool's pods may<br />take to stop, finishing its running turns: the engine puts no bound<br />of its own on that drain, so it must cover the longest turn. Unset<br />takes the class's seatPools.defaults, else 300. |  | Maximum: 3600 <br />Minimum: 30 <br />Optional: \{\} <br /> |


#### SeatPoolDefaults



SeatPoolDefaults sizes the pods of a seat pool that does not size its own,
and the compute of every seat pod, which the class alone sets: a
Company's pools choose how many pods they run, within maxPods, and the
class what each of them may take, as it does for the data pods.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatPoolSettings](#seatpoolsettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `maxConcurrentTurns` _integer_ | maxConcurrentTurns is how many turns one seat pod runs at once: Crewlet's<br />node.max_concurrent. Unset takes the engine's default, 32. A seat runs<br />one turn at a time, so a pod never runs more turns at once than it<br />holds seats. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `resources` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#resourcerequirements-v1-core)_ | resources are the compute resources of every seat pod's engine<br />container. Unset sets none. |  | Optional: \{\} <br /> |
| `terminationGracePeriodSeconds` _integer_ | terminationGracePeriodSeconds is how long a seat pod may take to stop.<br />A seat pod finishes its running turns before it exits, with no bound<br />of its own, so unset is five minutes, as for a data pod, rather than<br />the Kubernetes default of thirty seconds. |  | Maximum: 3600 <br />Minimum: 30 <br />Optional: \{\} <br /> |
| `scratchSizeLimit` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#quantity-resource-api)_ | scratchSizeLimit caps the emptyDir a seat pod keeps its scratch store<br />in: a store the engine deletes at every start, its native library<br />(about 20 MB, extracted at every start) and the files of the<br />processes it runs (stdio MCP servers, git). A pod that writes more is<br />evicted. Unset is 2Gi. |  | Optional: \{\} <br /> |


#### SeatPoolMode

_Underlying type:_ _string_

SeatPoolMode is how a seat pool runs the seats placed in it.

_Validation:_
- Enum: [Shared PerSeat]

_Appears in:_
- [SeatPool](#seatpool)
- [SeatPoolStatus](#seatpoolstatus)

| Field | Description |
| --- | --- |
| `Shared` | SeatPoolShared runs replicas pods that share the pool's seats between<br />them: each takes an even share, and a pod that stops hands its seats<br />to the others.<br /> |
| `PerSeat` | SeatPoolPerSeat runs replicas pods for each seat placed in the pool,<br />each holding that seat alone. It needs the class's<br />seatPools.allowPerSeat.<br /> |


#### SeatPoolPolicy

_Underlying type:_ _string_

SeatPoolPolicy is whether a class's companies may run seats on pods of
their own, outside the data pods.

_Validation:_
- Enum: [Forbidden Allowed]

_Appears in:_
- [SeatPoolSettings](#seatpoolsettings)

| Field | Description |
| --- | --- |
| `Forbidden` | SeatPoolsForbidden runs every seat on the data pods. A Company of the<br />class may declare no spec.topology.seatPools.<br /> |
| `Allowed` | SeatPoolsAllowed lets a Company declare seat pools, and opens the<br />leaf listener its seat pods join the company's event stream through<br />on every data pod of the class, from the data pod's first start, so<br />that a company adding its first pool restarts nothing.<br /> |


#### SeatPoolSettings



SeatPoolSettings is whether, and how far, a class's companies may run seats
on stateless pods of their own (a Company's spec.topology.seatPools). A seat
pod holds no data: it joins the company's event stream as a NATS leaf of the
data pods, keeps a scratch store it deletes at every start, and runs the
seats placed in its pool.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `policy` _[SeatPoolPolicy](#seatpoolpolicy)_ | policy is whether the class's companies may declare seat pools.<br />Changing it restarts every data pod of the class, which opens or<br />closes the leaf listener. | Forbidden | Enum: [Forbidden Allowed] <br />Optional: \{\} <br /> |
| `allowPerSeat` _boolean_ | allowPerSeat lets a Company declare a PerSeat pool, which runs one<br />pod (or two, a warm standby) for each seat placed in it. Each costs a<br />pod, and every pod's work grows with the number of seats of the<br />company, so it is off by default and meant for the few seats that<br />need a pod to themselves. It isolates a seat's compute and its<br />failures, not its credentials: every pod of a company can read the<br />company's whole secret store. |  | Optional: \{\} <br /> |
| `maxPods` _integer_ | maxPods is the most seat pods a Company of the class may run, all of<br />its pools together: each Shared pool's replicas, and each PerSeat<br />pool's replicas for every seat placed in it. The data pods are not<br />counted. Required with policy Allowed: a class's companies size their<br />own pools, and only this bounds how many seat pods a company of the<br />class runs. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `defaults` _[SeatPoolDefaults](#seatpooldefaults)_ | defaults size the pods of a pool that does not size its own. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SeatPoolStatus



SeatPoolStatus is the state of one seat pool's pods.



_Appears in:_
- [TopologyStatus](#topologystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the pool's name. |  | MaxLength: 20 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `mode` _[SeatPoolMode](#seatpoolmode)_ | mode is how the pool runs its seats. |  | Enum: [Shared PerSeat] <br />Optional: \{\} <br /> |
| `replicas` _integer_ | replicas is the number of the pool's pods, every seat's together for<br />a PerSeat pool. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `readyReplicas` _integer_ | readyReplicas is the number of the pool's pods that are ready. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `updatedReplicas` _integer_ | updatedReplicas is the number of the pool's pods that run the<br />current pod template. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `seats` _integer_ | seats is the number of agent seats placed in the pool. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### SeatSandbox



SeatSandbox gives a seat a sandbox to run code in, on the Company's
spec.sandbox. It maps onto Crewlet's role `sandbox`; its presence is
Crewlet's `enabled: true`.



_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `codingAgent` _[CodingAgent](#codingagent)_ | codingAgent is the coding agent the seat's sandbox runs. Unset takes<br />the Company's spec.sandbox.defaultCodingAgent. ClaudeCode runs on an<br />Anthropic provider or a ClaudeCode CLIAgent one, unless the seat's<br />sandbox environment signs it in itself; OpenCode runs on an<br />Anthropic, OpenAI or OpenAICompatible provider or an OpenCode<br />CLIAgent one. Crewlet's `coding_agent`. |  | Enum: [ClaudeCode OpenCode] <br />Optional: \{\} <br /> |
| `pauseTTLSeconds` _integer_ | pauseTTLSeconds is how long the seat's sandbox stays paused while it<br />waits on a person. Unset takes the Company's default; 0 never pauses.<br />Crewlet's `pause_ttl_seconds`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `maxTurns` _integer_ | maxTurns caps the agentic rounds of the seat's coding runs. Unset<br />takes the Company's default; 0 is uncapped. Crewlet's `max_turns`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `mcpServerRefs` _[LocalObjectReference](#localobjectreference) array_ | mcpServerRefs name the MCPServers the coding agent may use. Crewlet's<br />mcp.servers. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `env` _[SeatEnvVar](#seatenvvar) array_ | env is the environment of the seat's sandbox runs, for the external<br />tokens its code work needs: from Secrets, or provisioned by the<br />engine, such as the GitLab token a provisioning Integration mints for<br />the seat, named as its tool server's is so both are one token. A<br />ClaudeCode sign-in here (CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY,<br />ANTHROPIC_AUTH_TOKEN or a CLAUDE_CODE_USE_* toggle) keeps the key and<br />endpoint of the provider the code work runs on out of the box, and a<br />variable named as one the provider would set wins over it. Crewlet's<br />`env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `setup` _[SandboxSetupStep](#sandboxsetupstep) array_ | setup is the seat's own provisioning, applied after the Company's.<br />Crewlet's `setup`. |  | MaxItems: 16 <br />Optional: \{\} <br /> |


#### SeatSpec



SeatSpec is one seat of a company: an AI agent or a person. It renders to
one entry of Crewlet's roles, nested under its unit's.



_Appears in:_
- [Seat](#seat)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this seat belongs to. Immutable. |  | Required: \{\} <br /> |
| `handle` _string_ | handle is the seat's canonical slug. Unset is the Seat's<br />metadata.name. Crewlet derives the seat's identity, and with it the<br />seat's memory, from the company name and the handle, so the handle<br />cannot be changed; a Seat deleted and created again with the same<br />handle gets its memory back. Crewlet's `handle`. |  | MaxLength: 63 <br />MinLength: 1 <br />Pattern: `^[a-z0-9][a-z0-9-]*$` <br />Optional: \{\} <br /> |
| `kind` _[SeatKind](#seatkind)_ | kind is whether the seat is an AI agent or a person. Crewlet's `kind`. | Agent | Enum: [Agent Human] <br />Optional: \{\} <br /> |
| `displayName` _string_ | displayName is the seat's name as people read it, such as "Ana<br />(Platform Engineer)". Unset is the handle. Crewlet's `name`. It is for<br />reading only: one seat names another, a unit its lead and a seat what<br />it manages, by the handle, so two Seats may share a displayName. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `email` _string_ | email is the seat's email address. Crewlet's `email`. Each seat has its<br />own: the engine finds a seat by its address, compared lower-cased and<br />with any plus tag dropped, so a Seat whose address another Seat's<br />matches that way is reported as a Conflict. |  | MaxLength: 254 <br />MinLength: 3 <br />Pattern: `^[^@\s]+@[^@\s]+$` <br />Optional: \{\} <br /> |
| `unitRef` _[LocalObjectReference](#localobjectreference)_ | unitRef names the Unit, in this namespace, the seat belongs to. Unset<br />is a seat outside every unit, such as a chief executive. |  | Optional: \{\} <br /> |
| `goal` _string_ | goal is what the seat is for; it reaches the seat's prompt. Crewlet's<br />`goal`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `backstory` _string_ | backstory is who the seat is; it reaches the seat's prompt. Crewlet's<br />`backstory`. |  | MaxLength: 8192 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `responsibilities` _string array_ | responsibilities are what the seat owns. Crewlet's<br />`responsibilities`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `behavioralGuidelines` _string array_ | behavioralGuidelines are how the seat should work. Crewlet's<br />`behavioral_guidelines`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `manages` _[ManagedReference](#managedreference) array_ | manages names the seats and units this seat manages; a unit stands<br />for every seat in it and below it. Crewlet's `manages`, which names a<br />Seat by its handle and a Unit by its metadata.name, the Unit's key; a<br />key that is also a seat's handle reads as the seat, so a Unit named so<br />is refused here as a Conflict. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `workers` _string array_ | workers narrows which of the company's delegate templates (the<br />workers block of the Company's engineConfig) the seat may use, by the<br />templates' names: a lowercase letter or digit, then lowercase letters,<br />digits, '_' and '-'. Unset is every one. Crewlet's `workers`. |  | MaxItems: 32 <br />items:MaxLength: 128 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9][a-z0-9_-]*$` <br />Optional: \{\} <br /> |
| `tokenBudget` _[TokenBudget](#tokenbudget)_ | tokenBudget caps the tokens this seat may spend per window, on top of<br />the Company's caps. Crewlet's `token_budget`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `llm` _[SeatLLM](#seatllm)_ | llm is the model providers the seat runs on. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `learningEnabled` _boolean_ | learningEnabled overrides the company's learning setting for this<br />seat: false stops reflection after the seat's turns. Unset inherits.<br />Crewlet's `learning_enabled`. |  | Optional: \{\} <br /> |
| `mcp` _[MCPCredentials](#mcpcredentials) array_ | mcp are the seat's own tool-server credentials, which make it act as<br />itself on each server, and its own settings, such as the channel cache<br />of slack-mcp-server, which no other seat may share. They are merged<br />over its unit's, per variable. One entry per server, keyed by its<br />name. Crewlet's `mcp_env`, a credential position. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `sandbox` _[SeatSandbox](#seatsandbox)_ | sandbox gives the seat a sandbox to run code in. Unset gives it none. |  | Optional: \{\} <br /> |
| `placement` _[SeatPlacement](#seatplacement)_ | placement is where the seat runs. Unset runs it in the Company's<br />spec.topology.defaultSeatPool. |  | Optional: \{\} <br /> |
| `paused` _boolean_ | paused says whether the seat may take work. True pauses it: it starts<br />no new turn, the mail it is sent waits on its inbox in order, and its<br />schedules' fires are skipped rather than queued; the turn it is on<br />finishes first unless stopRunning is set. False resumes it, and what<br />waited is delivered first. Unset leaves the seat's pause to the people<br />who run the company in the engine's dashboard: the operator reports a<br />pause they take, and never lifts it.<br />A pause is not part of the company document. Crewlet keeps it in its<br />coordination store, changed through its operator catalogue's<br />pause_seat and resume_seat tools, which the operator calls under the<br />Company's Enforce policy whenever the seat's pause differs from this<br />field; under Observe and Seed it only reports the difference. |  | Optional: \{\} <br /> |
| `pauseReason` _string_ | pauseReason is why the seat is paused, shown on the seat and in its<br />feed in the engine. Only with paused true. The engine keeps the<br />reason a pause was taken with, so a reason changed while the seat<br />stays paused shows from the seat's next pause on. Crewlet's pause<br />`reason`. |  | MaxLength: 500 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `stopRunning` _boolean_ | stopRunning ends the turn the seat is on at its next round when it is<br />paused, rather than letting the turn finish; the stopped turn is not<br />run again. Only with paused true. Setting it on a seat already paused<br />stops the turn it is on then; unsetting it changes nothing, since a<br />stopped turn stays stopped. Crewlet's pause `stop_running`. |  | Optional: \{\} <br /> |
| `integrations` _[SeatIntegrations](#seatintegrations)_ | integrations is the seat's own identity on the company's<br />integrations. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `project` _string_ | project is the tracker project a seat outside every unit owns, by its<br />key; a seat in a unit takes its unit's. Crewlet requires a container<br />key to be an upper-case letter followed by 1 to 9 upper-case letters or<br />digits, such as LEAD. Crewlet's `project`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `space` _string_ | space is the knowledge container a seat outside every unit owns, by<br />its key, which follows the same rule as project; a seat in a unit<br />takes its unit's. Crewlet's `space`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `contact` _[SeatContact](#seatcontact)_ | contact is how agents reach the person in a Human seat on a chat<br />surface, and how what the person does there reaches the engine as<br />theirs. It applies to Human seats only, and is optional: a person who<br />works only through the engine's dashboard has no account to name, and<br />signs in on the seat through the engine's own identity directory. The<br />engine warns of a Human seat without one, since nothing addressed to it<br />on a chat surface reaches anybody, and the operator records the warning<br />as an EngineWarnings event on the Company. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `availability` _string_ | availability tells colleagues of a Human seat when to expect an<br />answer, such as "CET business hours". Crewlet's `availability`. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### SeatStatus



SeatStatus is what the operator observed about a Seat.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Seat](#seat)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists, and no other Seat has the<br />handle, the email or a contact account; Conflict names the other),<br />ResolvedRefs (the unit, the providers, the tool servers and every<br />Secret key exist), Applied (the seat is in the revision the engine<br />runs) and, on an Agent seat, Ready (an engine node holds the seat's<br />lease and runs it: LeaseHeld, or Unplaced, or PoolNotFound; or the<br />seat can run no turn wherever it is held, AgentModeBridgeUnsupported or<br />NotExposed) and, once the engine runs it, Paused (whether the engine<br />holds it paused, and whether that is what spec.paused asks), and, on<br />an Agent seat with spec.sandbox in a Company with spec.sandbox,<br />CodeWorkRunnable (whether its coding agent can run on the head of its<br />sandbox chain or on the sign-in the seat's sandbox environment gives<br />it; it does not count towards Ready, as the seat runs its turns either<br />way). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `handle` _string_ | handle is the seat's handle: spec.handle, or the metadata.name. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `seatID` _string_ | seatID is the seat's identity in the engine, derived from the company<br />name and the handle, so that callers need not derive it themselves.<br />Unset for a Human seat, which the engine does not run and gives no<br />identity, and for a seat whose handle another Seat derives too. |  | MaxLength: 36 <br />MinLength: 36 <br />Optional: \{\} <br /> |
| `placement` _[SeatPlacementStatus](#seatplacementstatus)_ | placement is where the engine runs the seat. Unset for a Human seat,<br />which the engine does not run. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `integrations` _[SeatIntegrationsStatus](#seatintegrationsstatus)_ | integrations is the addresses the seat's vendor apps must use. Unset<br />for a Human seat, which has no vendor apps of its own. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `pause` _[SeatPauseStatus](#seatpausestatus)_ | pause is the seat's pause while the engine holds it paused, whoever<br />paused it: the operator, for spec.paused, or a person in the engine's<br />dashboard. Unset while the seat takes work. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SecretEnvVar



SecretEnvVar is a named credential delivered as an environment variable,
such as a sandbox setup step's token or a coding CLI's model key. Crewlet
treats every value in the positions that use this type as a credential, so
a literal value is not accepted: the value always comes from a Secret.



_Appears in:_
- [CLIAgentSettings](#cliagentsettings)
- [SandboxSetupStep](#sandboxsetupstep)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom is the Secret key the value is read from. |  | Required: \{\} <br /> |


#### SecretKeyReference



SecretKeyReference selects one key of a Secret in the referring object's
namespace. The operator reads the value when it renders the company and
pushes it into the engine's encrypted secret store; the value itself never
appears in the company document, in a pod template or in any status.



_Appears in:_
- [AtlassianIntegration](#atlassianintegration)
- [CLIAgentAuth](#cliagentauth)
- [ConfluenceIntegration](#confluenceintegration)
- [DatadogIntegration](#datadogintegration)
- [E2BSandbox](#e2bsandbox)
- [EmbeddingSettings](#embeddingsettings)
- [GitHubIntegration](#githubintegration)
- [GitLabEngineProvisioning](#gitlabengineprovisioning)
- [GitLabIntegration](#gitlabintegration)
- [JiraIntegration](#jiraintegration)
- [LLMProviderSpec](#llmproviderspec)
- [MattermostEngineProvisioning](#mattermostengineprovisioning)
- [SeatCredentialSource](#seatcredentialsource)
- [SecretValueSource](#secretvaluesource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the Secret. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `key` _string_ | key is the key within the Secret's data. |  | MaxLength: 253 <br />MinLength: 1 <br />Pattern: `^[-._a-zA-Z0-9]+$` <br />Required: \{\} <br /> |


#### SecretReference



SecretReference names a whole Secret in the referring object's namespace.
It is used where one credential is several values that are always issued
together (a Slack app's bot token and signing secret, for example); the
field that holds the reference documents the keys the Secret must carry.



_Appears in:_
- [CompanyObjects](#companyobjects)
- [CompanyStatus](#companystatus)
- [GitHubApp](#githubapp)
- [SlackSeatIntegration](#slackseatintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the Secret. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### SecretValueSource



SecretValueSource says where a credential's value comes from. It has one
member today; it is a struct so that a second source can be added without
reshaping every list that carries credentials.



_Appears in:_
- [EnvVar](#envvar)
- [MCPEnvVar](#mcpenvvar)
- [MCPHeader](#mcpheader)
- [SandboxFile](#sandboxfile)
- [SecretEnvVar](#secretenvvar)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretKeyRef` _[SecretKeyReference](#secretkeyreference)_ | secretKeyRef selects the Secret key that holds the value. |  | Required: \{\} <br /> |


#### SlackIntegration



SlackIntegration turns Slack on for a company. It maps onto Crewlet's
integrations.slack. Slack has no company-wide credential: every agent seat
that speaks on Slack has its own app, in its Seat's spec.integrations.slack.



_Appears in:_
- [IntegrationSpec](#integrationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `typingStatus` _[WorkingStatus](#workingstatus)_ | typingStatus is when a seat shows that it is working. Unset takes<br />Crewlet's default, Always. Crewlet's `typing_status`. |  | Enum: [Always Addressed] <br />Optional: \{\} <br /> |
| `statusPhrases` _[StatusPhrases](#statusphrases)_ | statusPhrases replaces the words the working indicator shows.<br />Crewlet's `status_phrases`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SlackSeatIntegration



SlackSeatIntegration is a seat's own Slack app, so the seat posts as itself.
It maps onto Crewlet's role integrations.slack and needs the company's Slack
Integration.



_Appears in:_
- [SeatIntegrations](#seatintegrations)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `credentialsSecretRef` _[SecretReference](#secretreference)_ | credentialsSecretRef is a Secret holding the app's credentials under<br />the keys botToken (the bot token) and signingSecret (which verifies<br />the app's webhooks). Crewlet's `bot_token` and `signing_secret`, both<br />credential positions. Rewriting the Secret rotates them without a<br />restart. |  | Required: \{\} <br /> |


#### SlackSeatStatus



SlackSeatStatus is the Slack side of a seat, as the operator computed it.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatIntegrationsStatus](#seatintegrationsstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `requestURL` _string_ | requestURL is the URL the seat's Slack app must deliver events to: its<br />Event Subscriptions request URL, <publicBaseURL>/webhooks/slack/<handle>,<br />where publicBaseURL is the Company's status.endpoints.publicBaseURL.<br />It is set whenever the Company has a Slack Integration and a public<br />base URL, whether or not this Seat sets spec.integrations.slack: a<br />platform that creates the seat's Slack app needs the URL for the app's<br />manifest before the app, and so its credentials, exist. Derived from<br />the handle, which never changes, it never changes for a seat either. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### StatusPhrases



StatusPhrases replaces the words a seat's working indicator shows. Each line
completes the sentence "<seat> is …", and one line is picked per phase.
Each list maps onto the same-named list of Crewlet's status_phrases.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SlackIntegration](#slackintegration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `onboarding` _string array_ | onboarding is shown during a seat's first-turn onboarding pass. |  | MaxItems: 32 <br />items:MaxLength: 256 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `execute` _string array_ | execute is shown while the seat works. |  | MaxItems: 32 <br />items:MaxLength: 256 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `review` _string array_ | review is shown while the reviewer judges the seat's work. |  | MaxItems: 32 <br />items:MaxLength: 256 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `default` _string array_ | default is shown in any phase without lines of its own. |  | MaxItems: 32 <br />items:MaxLength: 256 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |


#### StreamMode

_Underlying type:_ _string_

StreamMode is where a company's event stream runs.

_Validation:_
- Enum: [Embedded]

_Appears in:_
- [StreamSettings](#streamsettings)

| Field | Description |
| --- | --- |
| `Embedded` | StreamModeEmbedded runs NATS JetStream inside the engine process, with<br />its data on the data volume. An external NATS cluster is not offered in<br />v1alpha1.<br /> |


#### StreamSettings



StreamSettings is how a company's event stream runs.

_Validation:_
- MinProperties: 1

_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[StreamMode](#streammode)_ | mode is where the stream runs. Embedded is the only mode in v1alpha1. | Embedded | Enum: [Embedded] <br />Optional: \{\} <br /> |


#### TelemetrySettings



TelemetrySettings sends the engine's traces and metrics to an OpenTelemetry
collector. The engine reads its exporter configuration from the standard
OTEL_* environment variables, which the operator sets from these fields;
with an endpoint set, Crewlet exports metrics as well as traces, and the
two cannot be turned off separately (OTEL_ names are the operator's, so
OTEL_METRICS_EXPORTER cannot be set in env). While a Company has a public
address (status.endpoints.publicBaseURL), the operator also wires Crewlet's
sandbox telemetry receiver there (CREWLET_SANDBOX_OTEL_RECEIVER_URL): code
a remote sandbox runs exports to the engine's public listener with a token
signed for its one run, and the engine forwards it to this collector with
the headers below, which never enter the sandbox.



_Appears in:_
- [EngineClassSpec](#engineclassspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `otlpEndpoint` _string_ | otlpEndpoint is the collector's OTLP/HTTP endpoint<br />(OTEL_EXPORTER_OTLP_ENDPOINT). |  | MaxLength: 2048 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `headersSecretRef` _[NamespacedSecretKeyReference](#namespacedsecretkeyreference)_ | headersSecretRef is a Secret key holding the headers sent to the<br />collector, in the OTEL_EXPORTER_OTLP_HEADERS format<br />(key1=value1,key2=value2), usually to authenticate. The operator copies<br />the value into a Secret it owns in each Company's namespace and<br />references it from the pod; the value never appears in the pod<br />template. It is in the engine's environment, which code the tenant<br />runs in the pod can read, so it reaches every tenant of the class: use<br />a credential that may only write telemetry, scoped per tenant where<br />the collector allows it. |  | Optional: \{\} <br /> |


#### TokenBudget



TokenBudget caps how many LLM tokens may be spent per calendar window on the
company's clock (spec.timeZone of the Company). Every window is optional and
independent; an absent window is uncapped. It maps onto Crewlet's
token_budget block.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `day` _integer_ | day is the most tokens one local day, midnight to midnight, may spend. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `week` _integer_ | week is the most tokens one ISO week, starting on Monday, may spend. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `month` _integer_ | month is the most tokens one calendar month may spend. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### TopologyStatus



TopologyStatus is the state of a company's pods.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyStatus](#companystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `data` _[DataTopologyStatus](#datatopologystatus)_ | data is the state of the data pods. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `seatPools` _[SeatPoolStatus](#seatpoolstatus) array_ | seatPools is the state of each seat pool's pods. |  | MaxItems: 8 <br />Optional: \{\} <br /> |


#### TrackerBackend

_Underlying type:_ _string_

TrackerBackend is which work tracker a company runs. It maps onto Crewlet's
tracker.backend (Native is `native`, Jira is `jira`, None is `none`).

_Validation:_
- Enum: [Native Jira None]

_Appears in:_
- [TrackerSettings](#trackersettings)

| Field | Description |
| --- | --- |
| `Native` | TrackerNative is the engine's own tracker.<br /> |
| `Jira` | TrackerJira is Jira, connected by the company's Jira Integration.<br /> |
| `None` | TrackerNone is a company with no tracker.<br /> |


#### TrackerSettings



TrackerSettings is which work tracker a company runs. It maps onto
Crewlet's tracker block.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `backend` _[TrackerBackend](#trackerbackend)_ | backend is which tracker the company runs. | Native | Enum: [Native Jira None] <br />Optional: \{\} <br /> |
| `native` _[NativeTrackerSettings](#nativetrackersettings)_ | native is the policy of the engine's own tracker, for backend Native. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### Unit



Unit is one node of a company's org chart. Units nest through
spec.parentRef; Seats join one through spec.unitRef.

The Unit's metadata.name is the unit's stable id in Crewlet, which has its
own rules: it must start with a lowercase letter and be at most 64
characters, and may hold lowercase letters, digits and '-'. (Crewlet also
admits '_' in a unit id, which no Kubernetes name can hold.)





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Unit` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[UnitSpec](#unitspec)_ | spec is the unit's desired state. |  | Required: \{\} <br /> |
| `status` _[UnitStatus](#unitstatus)_ | status is the unit's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### UnitSpec



UnitSpec is one node of a company's org chart: a division, a team, a squad.
It renders to one entry of Crewlet's units, nested under its parent's.



_Appears in:_
- [Unit](#unit)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this unit belongs to. Immutable. |  | Required: \{\} <br /> |
| `displayName` _string_ | displayName is the unit's name as people read it, in prompts and on<br />boards. Unset is the Unit's metadata.name. The metadata.name itself is<br />the unit's stable id, which survives a rename. Crewlet's `name`; the<br />metadata.name is Crewlet's `id`, the key a seat's manages entry names<br />the unit by. The displayName is for reading only: nothing refers to a<br />unit by it, so two Units may share one. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `type` _[UnitType](#unittype)_ | type is an informational label. Crewlet's `type`. |  | Enum: [Division Department Group Team Squad Pod Guild Chapter Unit] <br />Optional: \{\} <br /> |
| `parentRef` _[LocalObjectReference](#localobjectreference)_ | parentRef names the Unit this one belongs to. Unset makes it a<br />top-level unit. A parent that does not exist, or a chain of parents<br />that leads back to this unit, is reported on this Unit. |  | Optional: \{\} <br /> |
| `leadRef` _[LocalObjectReference](#localobjectreference)_ | leadRef names the Seat that leads the unit. Unset inherits the<br />parent's lead. Crewlet's `lead`. |  | Optional: \{\} <br /> |
| `purpose` _string_ | purpose is what the unit is for. Crewlet's `purpose`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `goals` _string array_ | goals are what the unit is working toward. Crewlet's `goals`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `channel` _string_ | channel is the unit's channel on the company's chat surface, such as<br />#platform; child units without one inherit it. Crewlet's `channel`. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `knowledge` _string array_ | knowledge are free-text references to knowledge the unit relies on.<br />They are not a read scope. Crewlet's `knowledge`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `project` _string_ | project is the tracker project the unit files work in, by its key.<br />Crewlet requires a container key to be an upper-case letter followed<br />by 1 to 9 upper-case letters or digits, such as PLAT. Crewlet's<br />`project`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `space` _string_ | space is the knowledge container the unit writes pages in, by its<br />key, which follows the same rule as project. Crewlet's `space`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `mcp` _[MCPCredentials](#mcpcredentials) array_ | mcp are tool-server credentials and settings every agent seat<br />directly in this unit inherits; a seat's own values win per variable.<br />One entry per server, keyed by its name. Crewlet's `mcp_env`, a<br />credential position. Every credential comes from a Secret: the engine<br />provisions a credential for one seat, and a unit's are every member's.<br />A value one seat must not share, such as slack-mcp-server's channel<br />cache, belongs on the Seat. |  | MaxItems: 16 <br />Optional: \{\} <br /> |


#### UnitStatus



UnitStatus is what the operator observed about a Unit.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Unit](#unit)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists), ResolvedRefs (the<br />parent, the lead, the tool servers and every Secret key exist, and the<br />parents form no cycle) and Applied (the unit is in the revision the<br />engine runs). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `depth` _integer_ | depth is the unit's distance from the top of the chart: 0 for a<br />top-level unit. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `seats` _integer_ | seats is the number of Seats directly in the unit. |  | Minimum: 0 <br />Optional: \{\} <br /> |


#### UnitType

_Underlying type:_ _string_

UnitType is an informational label for a unit; nothing in the engine
behaves differently for one. It maps onto Crewlet's well-known unit types
(Division is `division`, Team is `team`, and so on).

_Validation:_
- Enum: [Division Department Group Team Squad Pod Guild Chapter Unit]

_Appears in:_
- [UnitSpec](#unitspec)

| Field | Description |
| --- | --- |
| `Division` |  |
| `Department` |  |
| `Group` |  |
| `Team` |  |
| `Squad` |  |
| `Pod` |  |
| `Guild` |  |
| `Chapter` |  |
| `Unit` |  |


#### WebhookExposure



WebhookExposure is how vendors' webhooks, and the calls a remote sandbox
makes back to the engine, reach a company's engine. Whatever the mode, they
reach the engine's public listener (Crewlet's api.public), a port of its
own that serves only the routes outside parties call without an operator
credential (/webhooks/, /otlp/{token}, /mcp/{token}) and answers every
other path as one nothing serves; the admin API, the dashboard and the
probes are on the API port, which this never reaches.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ExposureSettings](#exposuresettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[WebhookExposureMode](#webhookexposuremode)_ | mode is how webhooks reach the engine. | None | Enum: [Gateway Ingress External None] <br />Optional: \{\} <br /> |
| `gateway` _[GatewayExposure](#gatewayexposure)_ | gateway is the Gateways to attach to, for mode Gateway. |  | Optional: \{\} <br /> |
| `ingress` _[IngressExposure](#ingressexposure)_ | ingress is the Ingress settings, for mode Ingress. |  | Optional: \{\} <br /> |
| `external` _[ExternalExposure](#externalexposure)_ | external is where the forwarder runs, for mode External. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `hostname` _string_ | hostname is the public host name of each company, as a template:<br />\{\{namespace\}\}, \{\{name\}\} and \{\{uid\}\} are replaced by the Company's<br />namespace, name and UID. The result is the company's public base URL,<br />https://<hostname>, which every vendor is told to deliver to. Prefer<br />\{\{uid\}\}: the host name is public, and a UID says nothing about the<br />tenant where a name can; and a UID is never reused, so a vendor still<br />configured for a deleted company can never reach a company created<br />later under the same name. The template must contain \{\{uid\}\}, or<br />\{\{name\}\} and \{\{namespace\}\} joined by a '.', which neither can hold, so<br />that no two companies share a host name: with a '-' between them,<br />team-a/b-web and team-a-b/web would. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### WebhookExposureMode

_Underlying type:_ _string_

WebhookExposureMode is how vendors' webhooks reach a company's engine.

_Validation:_
- Enum: [Gateway Ingress External None]

_Appears in:_
- [WebhookExposure](#webhookexposure)

| Field | Description |
| --- | --- |
| `Gateway` | WebhookExposureGateway routes the public hostname to the engine's<br />public listener with an HTTPRoute.<br /> |
| `Ingress` | WebhookExposureIngress does the same with an Ingress.<br /> |
| `External` | WebhookExposureExternal creates nothing public: something outside the<br />operator (a relay, an API gateway) forwards to the public port of the<br />engine's Service, and each Company states the address it is reached<br />on in spec.exposure.publicBaseURL.<br /> |
| `None` | WebhookExposureNone exposes nothing; integrations that need inbound<br />delivery report that they are not exposed.<br /> |


#### WorkingStatus

_Underlying type:_ _string_

WorkingStatus is when a seat shows that it is working on a chat message. It
maps onto Crewlet's typing_status (Always is `always`, Addressed is
`addressed`).

_Validation:_
- Enum: [Always Addressed]

_Appears in:_
- [MattermostIntegration](#mattermostintegration)
- [SlackIntegration](#slackintegration)

| Field | Description |
| --- | --- |
| `Always` | WorkingStatusAlways shows it on every turn a chat message started.<br /> |
| `Addressed` | WorkingStatusAddressed shows it only when a person is plausibly<br />waiting: a direct message, a mention, or a thread the seat follows.<br /> |


