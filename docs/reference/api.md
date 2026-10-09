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

No field holds a credential. Every position Crewlet marks as a credential is
filled from a Secret through a SecretKeyReference or a SecretReference
(NamespacedSecretKeyReference and NamespacedSecretReference on the
cluster-scoped EngineClass), or is not offered in v1alpha1: the credentials
of the integrations other than Slack and GitHub (see IntegrationSpec) and of
the cli-agent model provider (see LLMProviderType). docs/concepts/credentials.md
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



APIExposure is how a company's engine API is reached. The API always
requires a bearer token, which the operator generates per company and
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
| `publicBaseURL` _string_ | publicBaseURL is the address vendors deliver webhooks to, when the<br />company has one: spec.exposure.publicBaseURL, or https://<hostname><br />from the class's webhooks.hostname. Every vendor route is a path under<br />it, such as /webhooks/forge for an Atlassian Forge relay. Unset while<br />the class's webhooks.mode is None, which exposes nothing. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### CompanyExposure



CompanyExposure overrides how a company is reached.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `publicBaseURL` _string_ | publicBaseURL is the HTTPS address vendors deliver webhooks to,<br />overriding the one the class's webhooks.hostname derives. Required when<br />the class's webhooks.mode is External, where something outside the<br />operator (a relay, an API gateway) forwards its /webhooks/ path to the<br />engine. A trailing '/' is dropped, as Crewlet drops it. Crewlet's<br />integrations.public_base_url. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


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
| `version` _string_ | version replaces the tag of the class's engine image, for example<br />v0.14.3. It is honoured only when the class sets<br />engine.allowVersionOverride; the operator refuses a version older than<br />the one the company's data was last written by. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9_][A-Za-z0-9_.-]\{0,127\}$` <br />Optional: \{\} <br /> |
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
| `suspend` _boolean_ | suspend stops the engine and keeps its data volumes; setting it back<br />to false starts it again. It takes effect whatever else is wrong with<br />the Company, even while it is not Accepted. The Company reports<br />Ready=False with the reason Suspended meanwhile. Vendors keep delivering webhooks meanwhile,<br />and some disable a subscription after a long run of failed deliveries. |  | Optional: \{\} <br /> |
| `deletionPolicy` _[DeletionPolicy](#deletionpolicy)_ | deletionPolicy is what happens to the data volumes when the Company is<br />deleted. With Retain the keyring Secret is kept as well, since the<br />volumes cannot be read without it. | Retain | Enum: [Retain Delete] <br />Optional: \{\} <br /> |


#### CompanyStatus



CompanyStatus is what the operator observed about a Company.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Company](#company)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Ready (the summary), Accepted, RuntimeReady,<br />SecretsSynced, ConfigApplied and Exposed, which Ready summarizes, and<br />EngineHealthy, which it does not. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `engine` _[EngineStatus](#enginestatus)_ | engine is what runs the company. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `topology` _[TopologyStatus](#topologystatus)_ | topology is the state of the company's pods. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `config` _[ConfigStatus](#configstatus)_ | config is the revision the engine runs. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `endpoints` _[CompanyEndpoints](#companyendpoints)_ | endpoints is where the company is reached. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiAccessSecretRef` _[SecretReference](#secretreference)_ | apiAccessSecretRef names the Secret, in the Company's namespace, that<br />holds a bearer token for the engine's API under the key token<br />(APIAccessTokenKey) and the API's in-cluster base URL under the key<br />url (APIAccessURLKey). It is for the company's consumers (a control<br />plane, a dashboard); the operator uses a token of its own, so revoking<br />this one never locks the operator out.<br />The token is the engine's administrator: Crewlet has no token scopes,<br />so whoever holds it can rewrite the company document and reveal every<br />value in the engine's secret store, the class's platform model keys<br />included. Grant read on this Secret only to whoever may do that, by<br />resourceNames: read on every Secret of the namespace also exposes<br /><company>-api-tokens, <company>-keyring and <company>-bootstrap. |  | Optional: \{\} <br /> |


#### CompanyTopology



CompanyTopology is how a company's engine is laid out in pods. In v1alpha1
it is one data pod that serves the API, runs every seat and runs the
background workers.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `data` _[DataTopology](#datatopology)_ | data is the pods that hold the estate. | \{ replicas:1 \} | MinProperties: 1 <br />Optional: \{\} <br /> |


#### ConfigSettings



ConfigSettings is how the operator manages a company's document.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanySpec](#companyspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `managementPolicy` _[ManagementPolicy](#managementpolicy)_ | managementPolicy says who owns the document. Under Enforce the data<br />pod's crewlet.yaml sets Crewlet's api.auth.company_writers to the<br />operator's own token, so the engine refuses every other token a change<br />to the document; under Observe and Seed it sets none. The engine reads<br />crewlet.yaml only at start, so changing the policy to or from Enforce<br />restarts the data pod. | Enforce | Enum: [Enforce Observe Seed] <br />Optional: \{\} <br /> |


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


#### ContainerWebhookMode

_Underlying type:_ _string_

ContainerWebhookMode is how webhooks are registered at GitHub: once on the
organization, or once per repository. It maps onto Crewlet's org_webhook
(Auto is `auto`, Require is `true`, Never is `false`).

_Validation:_
- Enum: [Auto Require Never]

_Appears in:_
- [GitHubProvisioning](#githubprovisioning)

| Field | Description |
| --- | --- |
| `Auto` | ContainerWebhookAuto uses one organization hook where the token may<br />create one, and per-repository hooks otherwise.<br /> |
| `Require` | ContainerWebhookRequire demands an organization hook.<br /> |
| `Never` | ContainerWebhookNever always registers per-repository hooks.<br /> |


#### DataPodSettings



DataPodSettings sizes the pods that hold a company's estate. In v1alpha1 a
company runs exactly one data pod, which also serves the API, runs every
seat and runs the background workers.



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
| `size` _[Quantity](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#quantity-resource-api)_ | size is the capacity requested for each data volume. It holds the<br />store, the event stream and the store's cache. There is no default: how<br />much a tenant may keep is a platform decision.<br />Three quarters of it is the engine's event stream, Crewlet's<br />stream.store_max_bytes: the stream reserves its logs' ceilings against<br />that share up front. Crewlet's own floor for it is 5Gi, so a volume<br />must be at least 7Gi.<br />It applies to the data volumes of companies created after it is set.<br />An existing company's volumes keep the size they were created with:<br />they are claimed through the StatefulSet's volume claim templates,<br />which Kubernetes does not let change, and resizing a volume is a<br />decision about one company's storage class and data, not the class's. |  | Required: \{\} <br /> |


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
| `template` _string_ | template is the sandbox template, which also fixes each sandbox's CPU,<br />memory and disk. |  | MaxLength: 253 <br />MinLength: 1 <br />Optional: \{\} <br /> |


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
| `model` _string_ | model is the embedding model id. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `apiKeySecretRef` _[SecretKeyReference](#secretkeyreference)_ | apiKeySecretRef is the Secret key holding the API key. Crewlet's<br />`api_key`, a credential position. Required for type OpenAI: Crewlet<br />reads OPENAI_API_KEY from the environment when it is unset, and the<br />engine's environment never carries one (see spec.runtime.env). An<br />OpenAICompatible endpoint that needs no key may omit it. |  | Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL is the endpoint of an OpenAI-compatible service. Crewlet's<br />`base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `dimensions` _integer_ | dimensions is the vector width. Unset takes the model's own width,<br />which Crewlet knows for the models it names; any other model needs it<br />stated, and it must match what the model produces. |  | Maximum: 4096 <br />Minimum: 64 <br />Optional: \{\} <br /> |
| `maxInputTokens` _integer_ | maxInputTokens is the most tokens one input may hold. Unset takes the<br />model's documented window; it may only lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxBatchInputs` _integer_ | maxBatchInputs is the most inputs one request may carry. Unset takes<br />the model's documented limit; it may only lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `maxBatchTokens` _integer_ | maxBatchTokens is the most tokens one request may carry, summed over<br />its inputs. Unset takes the model's documented limit; it may only<br />lower it. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### EngineClass



EngineClass is a platform's defaults and guard rails for running Crewlet
engines: the image, the size of the data pods, how companies are exposed,
and limits. A Company names one in spec.engineClassName; in a multi-tenant platform, a service tier
usually maps to a class.

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
| `stream` _[StreamSettings](#streamsettings)_ | stream is how each company's event stream runs. | \{ mode:Embedded \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `objects` _[ObjectSettings](#objectsettings)_ | objects is where each company's files are kept. | \{ backend:NATS \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `exposure` _[ExposureSettings](#exposuresettings)_ | exposure is how each company is reached from outside its namespace. | \{ api:map[mode:ClusterInternal] webhooks:map[mode:None] \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `networkPolicy` _[NetworkPolicySettings](#networkpolicysettings)_ | networkPolicy is the NetworkPolicy the operator puts around each<br />company's engine pods. | \{ mode:Isolated \} | MinProperties: 1 <br />Optional: \{\} <br /> |
| `podTemplate` _[EnginePodTemplate](#enginepodtemplate)_ | podTemplate is what is added to every engine pod. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `serviceAccount` _[EngineServiceAccount](#engineserviceaccount)_ | serviceAccount is what is added to each company's engine<br />ServiceAccount. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
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
Company. A control plane should refuse it before it writes the Company; this
is the cluster's defence in depth.

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
| `image` _string_ | image is the Crewlet engine image, by tag or by digest, for example<br />ghcr.io/crewlet/crewlet:v0.14.2. There is no default: the platform<br />decides which build its tenants run. A Company's spec.runtime.version<br />replaces the tag, so an image pinned by digest cannot be combined with<br />allowVersionOverride. |  | MaxLength: 512 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `imagePullPolicy` _[PullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#pullpolicy-v1-core)_ | imagePullPolicy is the pull policy of the engine container. Unset<br />takes the Kubernetes default for the image reference. |  | Enum: [Always IfNotPresent Never] <br />Optional: \{\} <br /> |
| `imagePullSecrets` _[LocalObjectReference](#localobjectreference) array_ | imagePullSecrets name Secrets in each Company's namespace that pull<br />the image. They are referenced, not copied: the platform puts them<br />into the tenant namespaces it creates. |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `allowVersionOverride` _boolean_ | allowVersionOverride lets a Company of this class choose its own engine<br />version with spec.runtime.version. Off by default: the class usually<br />decides which version its tenants run. |  | Optional: \{\} <br /> |


#### EngineStatus



EngineStatus is what runs a company.

_Validation:_
- MinProperties: 1

_Appears in:_
- [CompanyStatus](#companystatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `image` _string_ | image is the engine image the data pods last ran on, recorded once<br />every data pod ran it and was ready. A rollout that has not finished<br />leaves the previous image here. |  | MaxLength: 512 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `version` _string_ | version is the tag of that image, such as v0.14.2; unset for an image<br />pinned by digest alone. The engine migrates its data forward only, so<br />the operator refuses to roll the data pods to an image whose tag is an<br />older semantic version than this one (RuntimeReady=False,<br />DowngradeRefused). The operator also records it on each data volume<br />claim (the annotation crewlet.ai/engine-version), which keeps the floor<br />when this status is lost. A version that never became ready is never<br />recorded, so a rollout that failed can still be taken back. |  | MaxLength: 128 <br />MinLength: 1 <br />Optional: \{\} <br /> |


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


#### GatewayExposure



GatewayExposure attaches a company's routes to Gateways.



_Appears in:_
- [APIExposure](#apiexposure)
- [WebhookExposure](#webhookexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `parentRefs` _[GatewayParentReference](#gatewayparentreference) array_ | parentRefs are the Gateways the HTTPRoute attaches to. |  | MaxItems: 8 <br />MinItems: 1 <br />Required: \{\} <br /> |
| `proxyNamespaces` _string array_ | proxyNamespaces are the namespaces the pods that carry the Gateways'<br />traffic run in. Under networkPolicy Isolated they are admitted to the<br />engine's port; nothing else outside the operator and the API consumers<br />is. Unset is the namespaces of parentRefs, which is right for<br />implementations that run a proxy beside each Gateway (Istio, for<br />example); implementations with a shared data plane run it elsewhere<br />(Envoy Gateway in envoy-gateway-system, for example) and need it named<br />here. |  | MaxItems: 8 <br />MinItems: 1 <br />items:MaxLength: 63 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |


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
| `controllerNamespaces` _string array_ | controllerNamespaces are the namespaces the Ingress controller's pods<br />run in. Under networkPolicy Isolated they are admitted to the engine's<br />port, and they are required there: an Ingress does not say which pods<br />serve it, and without them the policy would refuse every request the<br />Ingress forwards. |  | MaxItems: 8 <br />MinItems: 1 <br />items:MaxLength: 63 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$` <br />Optional: \{\} <br /> |


#### IngressTLS



IngressTLS terminates TLS on a company's Ingress.



_Appears in:_
- [IngressExposure](#ingressexposure)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | secretName is the TLS Secret, in the Company's namespace, the Ingress<br />presents. It is a template like the hostname: \{\{namespace\}\}, \{\{name\}\}<br />and \{\{uid\}\} are replaced. A certificate controller that issues<br />certificates from Ingress annotations creates it; set those annotations<br />under ingress.annotations. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### Integration



Integration connects a company to a vendor (Slack, GitHub): the company-wide
settings and the credentials that verify the vendor's webhooks. Credentials
that belong to one seat, such as its own Slack app, are on the Seat.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `crewlet.ai/v1alpha1` | | |
| `kind` _string_ | `Integration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  | Optional: \{\} <br /> |
| `spec` _[IntegrationSpec](#integrationspec)_ | spec is the integration's desired state. |  | Required: \{\} <br /> |
| `status` _[IntegrationStatus](#integrationstatus)_ | status is the integration's observed state. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### IntegrationSpec



IntegrationSpec connects a company to one vendor. Exactly one vendor member
is set; the Integration's kind of vendor cannot change, and a company has at
most one Integration per vendor (a second is reported as a conflict).

For both vendors, every per-seat credential (a seat's Slack app, its GitHub
App) comes from a Secret its Seat references, whoever created the app: the
engine mints no credential for Slack or GitHub. So an Integration has no
switch between credentials the platform provides and credentials the engine
provisions; one arrives with the vendors whose apps the engine can create
(GitLab, Mattermost).

v1alpha1 has the members slack and github. Crewlet's other integrations
arrive as further members of this union, each with a reference field per
credential. Until then their credential positions cannot be set at all:
integrations.gitlab (signing_secret, token, provisioning.admin_token),
integrations.mattermost.provisioning.admin_token and a seat's
integrations.mattermost.bot_token, integrations.jira (token,
webhook_secret), integrations.confluence (token, webhook_secret,
webhook_token), integrations.datadog (webhook_token, provisioning.api_key,
provisioning.app_key) and integrations.atlassian.api_key.



_Appears in:_
- [Integration](#integration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this integration belongs to. Immutable. |  | Required: \{\} <br /> |
| `slack` _[SlackIntegration](#slackintegration)_ | slack connects Slack. |  | Optional: \{\} <br /> |
| `github` _[GitHubIntegration](#githubintegration)_ | github connects GitHub. |  | Optional: \{\} <br /> |


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
| `webhookURL` _string_ | webhookURL is the company-wide URL the vendor delivers webhooks to,<br />when the company is exposed and the vendor has one. Per-seat URLs are<br />in each Seat's status. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### KnowledgeBackend

_Underlying type:_ _string_

KnowledgeBackend is which knowledge base a company runs. It maps onto
Crewlet's knowledge.backend (Native is `native`, None is `none`). Crewlet's
`confluence` backend arrives with the Confluence integration.

_Validation:_
- Enum: [Native None]

_Appears in:_
- [KnowledgeSettings](#knowledgesettings)

| Field | Description |
| --- | --- |
| `Native` | KnowledgeNative is the engine's own knowledge base.<br /> |
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
| `backend` _[KnowledgeBackend](#knowledgebackend)_ | backend is which knowledge base the company runs. | Native | Enum: [Native None] <br />Optional: \{\} <br /> |
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
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _string_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. Crewlet's<br />`base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
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
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _string_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. Crewlet's<br />`base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `reasoning` _boolean_ | reasoning turns on an OpenAI reasoning model's effort, so that<br />reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only. |  | Optional: \{\} <br /> |
| `reasoningEffort` _[ReasoningEffort](#reasoningeffort)_ | reasoningEffort is how hard the model thinks. For Anthropic it is sent<br />on every call, and unset means High. For OpenAI it is sent only while<br />reasoning is true, so it requires reasoning; an OpenAICompatible<br />provider cannot turn reasoning on and takes no reasoningEffort.<br />Crewlet's `reasoning_effort`. |  | Enum: [Low Medium High XHigh Max] <br />Optional: \{\} <br /> |
| `reasoningBudgetTokens` _integer_ | reasoningBudgetTokens is the thinking budget of an older, budget-based<br />Claude model. Unset means such a model does not think. Crewlet's<br />`reasoning_budget_tokens`; type Anthropic only. |  | Minimum: 1024 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the<br />longest silence in a streamed one. Unset takes Crewlet's default of<br />600. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `cooldowns` _[LLMCredentialCooldowns](#llmcredentialcooldowns)_ | cooldowns is how long a failing key is set aside. Crewlet's<br />`cooldowns`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiKeySecretRefs` _[SecretKeyReference](#secretkeyreference) array_ | apiKeySecretRefs are the provider's API keys, each a key of a Secret in<br />this namespace. With more than one, Crewlet rotates through them,<br />setting aside a key that hits a rate limit or is refused, in the order<br />listed when several are equally free. Crewlet's `api_keys`, a<br />credential position: the values are pushed into the engine's secret<br />store and the document refers to them by name only. Required for types<br />Anthropic and OpenAI; an OpenAICompatible endpoint that needs no key<br />may omit it. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### LLMProviderStatus



LLMProviderStatus is what the operator observed about an LLMProvider.

_Validation:_
- MinProperties: 1

_Appears in:_
- [LLMProvider](#llmprovider)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists), ResolvedRefs (every<br />Secret key exists) and Applied (the provider is in the revision the<br />engine runs). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |


#### LLMProviderType

_Underlying type:_ _string_

LLMProviderType is which model API a provider speaks. It maps onto the
`type` of a Crewlet providers.llm entry: Anthropic is `anthropic`, OpenAI is
`openai` and OpenAICompatible is `openai-compatible`.

Crewlet's fourth type, `cli-agent`, drives a coding CLI installed next to
the engine on a subscription login. It is not offered in v1alpha1: the
engine image ships no coding CLI, and its credentials (a subscription token
or an exported credential bundle) are per-node logins that a pod which can
be rescheduled at any time cannot hold.

_Validation:_
- Enum: [Anthropic OpenAI OpenAICompatible]

_Appears in:_
- [LLMProviderSettings](#llmprovidersettings)
- [LLMProviderSpec](#llmproviderspec)
- [PlatformLLMProvider](#platformllmprovider)

| Field | Description |
| --- | --- |
| `Anthropic` | LLMProviderAnthropic is Anthropic's Messages API.<br /> |
| `OpenAI` | LLMProviderOpenAI is OpenAI's API.<br /> |
| `OpenAICompatible` | LLMProviderOpenAICompatible is any endpoint that speaks the OpenAI wire<br />format: a gateway, an aggregator, a self-hosted inference server.<br /> |


#### LocalObjectReference



LocalObjectReference names another object of the kind the referring field
documents, in the referring object's own namespace.



_Appears in:_
- [EngineSettings](#enginesettings)
- [ScheduleTarget](#scheduletarget)
- [SeatSandbox](#seatsandbox)
- [SeatSpec](#seatspec)
- [UnitSpec](#unitspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the metadata.name of the referenced object. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |


#### MCPCredentialValue



MCPCredentialValue is one value of a seat's credentials for a tool server.
Whether it is an environment variable or a request header depends on the
server's transport, which is why its name follows the grammar of an HTTP
header name, of which an environment variable's name is a subset.



_Appears in:_
- [MCPCredentials](#mcpcredentials)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the environment variable's name (Stdio) or the header's name<br />(HTTP). |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9!#$%&'*+.^_\|~-]+$` <br />Required: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom is the Secret key the value is read from. For a header the<br />Secret key holds the whole header value, scheme included (for example<br />"Bearer <token>"). |  | Required: \{\} <br /> |


#### MCPCredentials



MCPCredentials are the credentials one tool server is started with for a
seat: environment variables for a Stdio server, request headers for an HTTP
one. They map onto one server's entry of Crewlet's mcp_env, a credential
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
| `name` _string_ | name is the metadata.name of the MCPServer, in this namespace, the<br />credentials are for. |  | MaxLength: 253 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `env` _[MCPCredentialValue](#mcpcredentialvalue) array_ | env are the values. For a Stdio server each is an environment<br />variable of its process; for an HTTP server each is a request header<br />(the name is the header's name). Names must differ in more than case:<br />HTTP header names are case-insensitive, so Authorization and<br />authorization are one header. |  | MaxItems: 32 <br />MinItems: 1 <br />Required: \{\} <br /> |


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
Seats and Units give it per-seat credentials by that name.



_Appears in:_
- [MCPServer](#mcpserver)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `companyRef` _[CompanyReference](#companyreference)_ | companyRef names the Company this server belongs to. Immutable. |  | Required: \{\} <br /> |
| `transport` _[MCPTransport](#mcptransport)_ | transport is how the engine reaches the server. Crewlet's `transport`. | Stdio | Enum: [Stdio HTTP] <br />Optional: \{\} <br /> |
| `shared` _boolean_ | shared launches one instance whose tools every agent calls. false<br />makes the server a template instead: every seat that has credentials<br />for it in spec.mcp gets an instance of its own, which is how each<br />agent acts as itself on the server. Crewlet's `shared`. | true | Optional: \{\} <br /> |
| `command` _string_ | command is the executable a Stdio server is launched with. Crewlet's<br />`command`. |  | MaxLength: 1024 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `args` _string array_ | args are the executable's arguments. Crewlet's `args`. |  | MaxItems: 64 <br />items:MaxLength: 4096 <br />Optional: \{\} <br /> |
| `env` _[SecretEnvVar](#secretenvvar) array_ | env is the environment of a Stdio server's process, shared by every<br />instance; per-seat values belong in a Seat's or a Unit's spec.mcp.<br />Crewlet's `env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
| `url` _string_ | url is the endpoint of an HTTP server. Crewlet's `url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `headers` _[SecretHeader](#secretheader) array_ | headers are sent with every request to an HTTP server, shared by every<br />instance; per-seat values belong in a Seat's or a Unit's spec.mcp.<br />Header names are case-insensitive, so names must differ in more than<br />case. Crewlet's `headers`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
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
| `Enforce` | ManagementPolicyEnforce makes the operator the document's only<br />writer. The engine refuses every other token a change to the document<br />(403 config_managed), the published access token included, and its<br />dashboard shows the document read-only; a revision made some other way<br />all the same (the engine's own writes, or one made before the data pod<br />restarted onto the policy) is reported and overwritten.<br /> |
| `Observe` | ManagementPolicyObserve renders and compares, and never writes; a<br />difference is reported only. The engine's own users may change the<br />document.<br /> |
| `Seed` | ManagementPolicySeed writes the document once, into an engine that<br />has none, and then leaves it to the engine and its dashboard, whose<br />users may change it. Changes to the Company's children are ignored<br />afterwards and say so in their status.<br /> |


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
| `Isolated` | NetworkPolicyIsolated admits traffic to the engine's API port only<br />from the operator's pods, from the namespaces the class's Gateway<br />proxies or Ingress controller run in (gateway.proxyNamespaces,<br />ingress.controllerNamespaces), and from the class's apiConsumers; and<br />lets the engine connect only as networkPolicy.egress says. It needs a<br />CNI that enforces NetworkPolicy, egress included.<br /> |
| `None` | NetworkPolicyNone creates no NetworkPolicy.<br /> |


#### NetworkPolicyPeer



NetworkPolicyPeer is the pods its podSelector selects in the namespaces
its namespaceSelector selects, as a NetworkPolicy peer that sets both.



_Appears in:_
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
| `apiConsumers` _[NetworkPolicyPeer](#networkpolicypeer) array_ | apiConsumers are the pods admitted to the engine's API port besides<br />the operator and the class's Gateway proxies or Ingress controller:<br />the platform's own components (a control plane, a dashboard) that<br />call the engines of this class's companies. Each holds an access<br />token, which the engine still requires. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |
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
| `type` _[LLMProviderType](#llmprovidertype)_ | type is which model API the provider speaks. Crewlet's `type`. |  | Enum: [Anthropic OpenAI OpenAICompatible] <br />Required: \{\} <br /> |
| `model` _string_ | model is the model id requests are sent for, as the provider names it.<br />Crewlet's `model`. |  | MaxLength: 256 <br />MinLength: 1 <br />Required: \{\} <br /> |
| `claudeModel` _string_ | claudeModel names the Claude model whose request shape an Anthropic<br />provider uses when model is a gateway alias Crewlet cannot recognise.<br />Crewlet's `claude_model`; type Anthropic only. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `baseURL` _string_ | baseURL sends requests to this endpoint instead of the vendor's own,<br />for a gateway or a proxy. Required for type OpenAICompatible. Crewlet's<br />`base_url`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `reasoning` _boolean_ | reasoning turns on an OpenAI reasoning model's effort, so that<br />reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only. |  | Optional: \{\} <br /> |
| `reasoningEffort` _[ReasoningEffort](#reasoningeffort)_ | reasoningEffort is how hard the model thinks. For Anthropic it is sent<br />on every call, and unset means High. For OpenAI it is sent only while<br />reasoning is true, so it requires reasoning; an OpenAICompatible<br />provider cannot turn reasoning on and takes no reasoningEffort.<br />Crewlet's `reasoning_effort`. |  | Enum: [Low Medium High XHigh Max] <br />Optional: \{\} <br /> |
| `reasoningBudgetTokens` _integer_ | reasoningBudgetTokens is the thinking budget of an older, budget-based<br />Claude model. Unset means such a model does not think. Crewlet's<br />`reasoning_budget_tokens`; type Anthropic only. |  | Minimum: 1024 <br />Optional: \{\} <br /> |
| `timeoutSeconds` _integer_ | timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the<br />longest silence in a streamed one. Unset takes Crewlet's default of<br />600. Crewlet's `timeout_seconds`. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `cooldowns` _[LLMCredentialCooldowns](#llmcredentialcooldowns)_ | cooldowns is how long a failing key is set aside. Crewlet's<br />`cooldowns`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `apiKeySecretRefs` _[NamespacedSecretKeyReference](#namespacedsecretkeyreference) array_ | apiKeySecretRefs are the provider's API keys, rotated as an<br />LLMProvider's are. |  | MaxItems: 16 <br />MinItems: 1 <br />Optional: \{\} <br /> |


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

v1alpha1 has no way to pause a seat while keeping it in the chart. Crewlet
pauses a seat through its API rather than its company document, and a
paused field arrives with the operator's use of that API; until then,
Company's spec.suspend stops the whole engine.

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
surfaces. It maps onto Crewlet's role `contact`. Crewlet refuses two seats
that claim one account, so a Seat whose slackUserID or githubLogin (in any
case: GitHub logins are compared without it) another Seat of the company
already claims is reported as a Conflict.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `slackUserID` _string_ | slackUserID is the person's Slack member id, such as U0123ABCD.<br />Crewlet's `slack_user_id`. |  | MaxLength: 64 <br />MinLength: 1 <br />Pattern: `^[A-Z0-9]+$` <br />Optional: \{\} <br /> |
| `githubLogin` _string_ | githubLogin is the person's GitHub login. Crewlet's `github_login`. |  | MaxLength: 39 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9-]+$` <br />Optional: \{\} <br /> |


#### SeatIntegrations



SeatIntegrations is a seat's own identity on the company's integrations:
the chat app it speaks as, the code-host app it acts as. Its tool
credentials are in spec.mcp instead.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `slack` _[SlackSeatIntegration](#slackseatintegration)_ | slack is the seat's own Slack app. |  | Optional: \{\} <br /> |
| `github` _[GitHubSeatIntegration](#githubseatintegration)_ | github is the seat's identity on GitHub. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


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
| `sandbox` _[LLMProviderReference](#llmproviderreference) array_ | sandbox is the chain the coding agent in the seat's sandbox runs on.<br />Crewlet's `sandbox`. |  | MaxItems: 8 <br />MinItems: 1 <br />Optional: \{\} <br /> |


#### SeatPlacement



SeatPlacement is where a seat runs.



_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pool` _string_ | pool is the pool of pods that runs the seat. In v1alpha1 every seat<br />runs on the company's data pod, whose pool is data. | data | MaxLength: 20 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### SeatPlacementStatus



SeatPlacementStatus is where the engine runs a seat.

_Validation:_
- MinProperties: 1

_Appears in:_
- [SeatStatus](#seatstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `pool` _string_ | pool is the pool of pods that runs the seat: spec.placement.pool, or<br />data. The seat is rendered with role.placement.labels<br />\{crewlet.ai/pool: <pool>\}, which only the pool's pods carry. |  | MaxLength: 20 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### SeatSandbox



SeatSandbox gives a seat a sandbox to run code in, on the Company's
spec.sandbox. It maps onto Crewlet's role `sandbox`; its presence is
Crewlet's `enabled: true`.



_Appears in:_
- [SeatSpec](#seatspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `codingAgent` _[CodingAgent](#codingagent)_ | codingAgent is the coding agent the seat's sandbox runs. Unset takes<br />the Company's spec.sandbox.defaultCodingAgent. Crewlet's<br />`coding_agent`. |  | Enum: [ClaudeCode OpenCode] <br />Optional: \{\} <br /> |
| `pauseTTLSeconds` _integer_ | pauseTTLSeconds is how long the seat's sandbox stays paused while it<br />waits on a person. Unset takes the Company's default; 0 never pauses.<br />Crewlet's `pause_ttl_seconds`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `maxTurns` _integer_ | maxTurns caps the agentic rounds of the seat's coding runs. Unset<br />takes the Company's default; 0 is uncapped. Crewlet's `max_turns`. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `mcpServerRefs` _[LocalObjectReference](#localobjectreference) array_ | mcpServerRefs name the MCPServers the coding agent may use. Crewlet's<br />mcp.servers. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `env` _[SecretEnvVar](#secretenvvar) array_ | env is the environment of the seat's sandbox runs, for the external<br />tokens its code work needs. Crewlet's `env`, a credential position. |  | MaxItems: 32 <br />Optional: \{\} <br /> |
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
| `displayName` _string_ | displayName is the seat's name as people read it, such as "Ana<br />(Platform Engineer)". Unset is the handle. Crewlet's `name`. Crewlet<br />refuses two seats with one name, so a Seat whose displayName (or, unset,<br />handle) another Seat of the company already uses is reported as a<br />Conflict. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `email` _string_ | email is the seat's email address. Crewlet's `email`. |  | MaxLength: 254 <br />MinLength: 3 <br />Pattern: `^[^@\s]+@[^@\s]+$` <br />Optional: \{\} <br /> |
| `unitRef` _[LocalObjectReference](#localobjectreference)_ | unitRef names the Unit, in this namespace, the seat belongs to. Unset<br />is a seat outside every unit, such as a chief executive. |  | Optional: \{\} <br /> |
| `goal` _string_ | goal is what the seat is for; it reaches the seat's prompt. Crewlet's<br />`goal`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `backstory` _string_ | backstory is who the seat is; it reaches the seat's prompt. Crewlet's<br />`backstory`. |  | MaxLength: 8192 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `responsibilities` _string array_ | responsibilities are what the seat owns. Crewlet's<br />`responsibilities`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `behavioralGuidelines` _string array_ | behavioralGuidelines are how the seat should work. Crewlet's<br />`behavioral_guidelines`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `manages` _[ManagedReference](#managedreference) array_ | manages names the seats and units this seat manages; a unit stands<br />for every seat in it and below it. Crewlet's `manages`. |  | MaxItems: 64 <br />Optional: \{\} <br /> |
| `workers` _string array_ | workers narrows which of the company's delegate templates (the<br />workers block of the Company's engineConfig) the seat may use, by the<br />templates' names: a lowercase letter or digit, then lowercase letters,<br />digits, '_' and '-'. Unset is every one. Crewlet's `workers`. |  | MaxItems: 32 <br />items:MaxLength: 128 <br />items:MinLength: 1 <br />items:Pattern: `^[a-z0-9][a-z0-9_-]*$` <br />Optional: \{\} <br /> |
| `tokenBudget` _[TokenBudget](#tokenbudget)_ | tokenBudget caps the tokens this seat may spend per window, on top of<br />the Company's caps. Crewlet's `token_budget`. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `llm` _[SeatLLM](#seatllm)_ | llm is the model providers the seat runs on. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `learningEnabled` _boolean_ | learningEnabled overrides the company's learning setting for this<br />seat: false stops reflection after the seat's turns. Unset inherits.<br />Crewlet's `learning_enabled`. |  | Optional: \{\} <br /> |
| `mcp` _[MCPCredentials](#mcpcredentials) array_ | mcp are the seat's own tool-server credentials, which make it act as<br />itself on each server. They are merged over its unit's, per variable.<br />One entry per server, keyed by its name. Crewlet's `mcp_env`, a<br />credential position. |  | MaxItems: 16 <br />Optional: \{\} <br /> |
| `sandbox` _[SeatSandbox](#seatsandbox)_ | sandbox gives the seat a sandbox to run code in. Unset gives it none. |  | Optional: \{\} <br /> |
| `placement` _[SeatPlacement](#seatplacement)_ | placement is where the seat runs. |  | Optional: \{\} <br /> |
| `integrations` _[SeatIntegrations](#seatintegrations)_ | integrations is the seat's own identity on the company's<br />integrations. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `project` _string_ | project is the tracker project a seat outside every unit owns, by its<br />key; a seat in a unit takes its unit's. Crewlet requires a container<br />key to be an upper-case letter followed by 1 to 9 upper-case letters or<br />digits, such as LEAD. Crewlet's `project`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `space` _string_ | space is the knowledge container a seat outside every unit owns, by<br />its key, which follows the same rule as project; a seat in a unit<br />takes its unit's. Crewlet's `space`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `contact` _[SeatContact](#seatcontact)_ | contact is how agents reach the person in a Human seat. Required for,<br />and only for, kind Human. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `availability` _string_ | availability tells colleagues of a Human seat when to expect an<br />answer, such as "CET business hours". Crewlet's `availability`. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |


#### SeatStatus



SeatStatus is what the operator observed about a Seat.

_Validation:_
- MinProperties: 1

_Appears in:_
- [Seat](#seat)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.37/#condition-v1-meta) array_ | conditions are Accepted (the Company exists, and no other Seat has the<br />handle, the name or a contact account; Conflict names the other),<br />ResolvedRefs (the unit, the providers, the tool servers and<br />every Secret key exist) and Applied (the seat is in the revision the<br />engine runs). |  | MaxItems: 8 <br />Optional: \{\} <br /> |
| `observedGeneration` _integer_ | observedGeneration is the metadata.generation this status describes. |  | Minimum: 1 <br />Optional: \{\} <br /> |
| `handle` _string_ | handle is the seat's handle: spec.handle, or the metadata.name. |  | MaxLength: 63 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `seatID` _string_ | seatID is the seat's identity in the engine, derived from the company<br />name and the handle, so that callers need not derive it themselves.<br />Unset for a Human seat, which the engine does not run and gives no<br />identity, and for a seat whose handle another Seat derives too. |  | MaxLength: 36 <br />MinLength: 36 <br />Optional: \{\} <br /> |
| `placement` _[SeatPlacementStatus](#seatplacementstatus)_ | placement is where the engine runs the seat. Unset for a Human seat,<br />which the engine does not run. |  | MinProperties: 1 <br />Optional: \{\} <br /> |
| `integrations` _[SeatIntegrationsStatus](#seatintegrationsstatus)_ | integrations is the addresses the seat's vendor apps must use. Unset<br />for a Human seat, which has no vendor apps of its own. |  | MinProperties: 1 <br />Optional: \{\} <br /> |


#### SecretEnvVar



SecretEnvVar is a named credential delivered as an environment variable,
such as a tool server's API token. Crewlet treats every value in the
positions that use this type as a credential, so a literal value is not
accepted: the value always comes from a Secret.



_Appears in:_
- [MCPServerSpec](#mcpserverspec)
- [SandboxSetupStep](#sandboxsetupstep)
- [SeatSandbox](#seatsandbox)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the variable's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z_][A-Za-z0-9_]*$` <br />Required: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom is the Secret key the value is read from. |  | Required: \{\} <br /> |


#### SecretHeader



SecretHeader is a named credential sent as an HTTP request header, such as
the Authorization header of a remote tool server. The Secret key holds the
whole header value, scheme included (for example "Bearer <token>").



_Appears in:_
- [MCPServerSpec](#mcpserverspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is the header's name. |  | MaxLength: 128 <br />MinLength: 1 <br />Pattern: `^[A-Za-z0-9!#$%&'*+.^_\|~-]+$` <br />Required: \{\} <br /> |
| `valueFrom` _[SecretValueSource](#secretvaluesource)_ | valueFrom is the Secret key the value is read from. |  | Required: \{\} <br /> |


#### SecretKeyReference



SecretKeyReference selects one key of a Secret in the referring object's
namespace. The operator reads the value when it renders the company and
pushes it into the engine's encrypted secret store; the value itself never
appears in the company document, in a pod template or in any status.



_Appears in:_
- [E2BSandbox](#e2bsandbox)
- [EmbeddingSettings](#embeddingsettings)
- [GitHubIntegration](#githubintegration)
- [LLMProviderSpec](#llmproviderspec)
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
- [MCPCredentialValue](#mcpcredentialvalue)
- [SandboxFile](#sandboxfile)
- [SecretEnvVar](#secretenvvar)
- [SecretHeader](#secretheader)

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
OTEL_METRICS_EXPORTER cannot be set in env). Code a sandbox runs exports
nothing: v1alpha1 does not wire Crewlet's sandbox telemetry receiver.



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


#### TrackerBackend

_Underlying type:_ _string_

TrackerBackend is which work tracker a company runs. It maps onto Crewlet's
tracker.backend (Native is `native`, None is `none`). Crewlet's `jira`
backend arrives with the Jira integration.

_Validation:_
- Enum: [Native None]

_Appears in:_
- [TrackerSettings](#trackersettings)

| Field | Description |
| --- | --- |
| `Native` | TrackerNative is the engine's own tracker.<br /> |
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
| `backend` _[TrackerBackend](#trackerbackend)_ | backend is which tracker the company runs. | Native | Enum: [Native None] <br />Optional: \{\} <br /> |
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
| `displayName` _string_ | displayName is the unit's name as people read it, in prompts and on<br />boards. Unset is the Unit's metadata.name. The metadata.name itself is<br />the unit's stable id, which survives a rename. Crewlet's `name`; the<br />metadata.name is Crewlet's `id`. Crewlet refuses two units that<br />answer to one key, and a unit answers to its name and to its id,<br />compared without case: a Unit whose displayName (or, unset,<br />metadata.name) is another Unit's displayName or metadata.name, in any<br />case, is reported as a Conflict. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `type` _[UnitType](#unittype)_ | type is an informational label. Crewlet's `type`. |  | Enum: [Division Department Group Team Squad Pod Guild Chapter Unit] <br />Optional: \{\} <br /> |
| `parentRef` _[LocalObjectReference](#localobjectreference)_ | parentRef names the Unit this one belongs to. Unset makes it a<br />top-level unit. A parent that does not exist, or a chain of parents<br />that leads back to this unit, is reported on this Unit. |  | Optional: \{\} <br /> |
| `leadRef` _[LocalObjectReference](#localobjectreference)_ | leadRef names the Seat that leads the unit. Unset inherits the<br />parent's lead. Crewlet's `lead`. |  | Optional: \{\} <br /> |
| `purpose` _string_ | purpose is what the unit is for. Crewlet's `purpose`. |  | MaxLength: 2048 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `goals` _string array_ | goals are what the unit is working toward. Crewlet's `goals`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `channel` _string_ | channel is the unit's channel on the company's chat surface, such as<br />#platform; child units without one inherit it. Crewlet's `channel`. |  | MaxLength: 256 <br />MinLength: 1 <br />Optional: \{\} <br /> |
| `knowledge` _string array_ | knowledge are free-text references to knowledge the unit relies on.<br />They are not a read scope. Crewlet's `knowledge`. |  | MaxItems: 32 <br />items:MaxLength: 1024 <br />items:MinLength: 1 <br />Optional: \{\} <br /> |
| `project` _string_ | project is the tracker project the unit files work in, by its key.<br />Crewlet requires a container key to be an upper-case letter followed<br />by 1 to 9 upper-case letters or digits, such as PLAT. Crewlet's<br />`project`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `space` _string_ | space is the knowledge container the unit writes pages in, by its<br />key, which follows the same rule as project. Crewlet's `space`. |  | MaxLength: 10 <br />MinLength: 2 <br />Pattern: `^[A-Z][A-Z0-9]\{1,9\}$` <br />Optional: \{\} <br /> |
| `mcp` _[MCPCredentials](#mcpcredentials) array_ | mcp are tool-server credentials every agent seat directly in this unit<br />inherits; a seat's own credentials win per variable. One entry per<br />server, keyed by its name. Crewlet's `mcp_env`, a credential position. |  | MaxItems: 16 <br />Optional: \{\} <br /> |


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



WebhookExposure is how vendors' webhooks reach a company's engine. Only the
path prefix /webhooks/ is ever exposed by it: the engine's admin API shares
its port and must not be public.

_Validation:_
- MinProperties: 1

_Appears in:_
- [ExposureSettings](#exposuresettings)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `mode` _[WebhookExposureMode](#webhookexposuremode)_ | mode is how webhooks reach the engine. | None | Enum: [Gateway Ingress External None] <br />Optional: \{\} <br /> |
| `gateway` _[GatewayExposure](#gatewayexposure)_ | gateway is the Gateways to attach to, for mode Gateway. |  | Optional: \{\} <br /> |
| `ingress` _[IngressExposure](#ingressexposure)_ | ingress is the Ingress settings, for mode Ingress. |  | Optional: \{\} <br /> |
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
| `Gateway` | WebhookExposureGateway routes the public hostname's /webhooks/ path to<br />the engine with an HTTPRoute.<br /> |
| `Ingress` | WebhookExposureIngress does the same with an Ingress.<br /> |
| `External` | WebhookExposureExternal creates nothing public: something outside the<br />operator (a relay, an API gateway) forwards to the engine's Service,<br />and each Company states the address it is reached on in<br />spec.exposure.publicBaseURL.<br /> |
| `None` | WebhookExposureNone exposes nothing; integrations that need inbound<br />delivery report that they are not exposed.<br /> |


#### WorkingStatus

_Underlying type:_ _string_

WorkingStatus is when a seat shows that it is working on a chat message. It
maps onto Crewlet's typing_status (Always is `always`, Addressed is
`addressed`).

_Validation:_
- Enum: [Always Addressed]

_Appears in:_
- [SlackIntegration](#slackintegration)

| Field | Description |
| --- | --- |
| `Always` | WorkingStatusAlways shows it on every turn a chat message started.<br /> |
| `Addressed` | WorkingStatusAddressed shows it only when a person is plausibly<br />waiting: a direct message, a mention, or a thread the seat follows.<br /> |


