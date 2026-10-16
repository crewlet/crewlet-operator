package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TrackerBackend is which work tracker a company runs. It maps onto Crewlet's
// tracker.backend (Native is `native`, Jira is `jira`, None is `none`).
// +kubebuilder:validation:Enum=Native;Jira;None
type TrackerBackend string

// The tracker backends.
const (
	// TrackerNative is the engine's own tracker.
	TrackerNative TrackerBackend = "Native"
	// TrackerJira is Jira, connected by the company's Jira Integration.
	TrackerJira TrackerBackend = "Jira"
	// TrackerNone is a company with no tracker.
	TrackerNone TrackerBackend = "None"
)

// NativeTrackerSettings is the policy of the engine's own tracker. It maps
// onto Crewlet's tracker.native.
// +kubebuilder:validation:MinProperties=1
type NativeTrackerSettings struct {
	// inboxRetentionDays is how long a person's inbox keeps a row; the
	// history the row points at is kept regardless. Unset takes Crewlet's
	// default of 365. Crewlet's `inbox_retention_days`.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:validation:Maximum=3650
	InboxRetentionDays int32 `json:"inboxRetentionDays,omitempty"`
}

// TrackerSettings is which work tracker a company runs. It maps onto
// Crewlet's tracker block.
// +kubebuilder:validation:XValidation:rule="!has(self.native) || self.backend == 'Native'",message="native applies to backend Native only"
// +kubebuilder:validation:MinProperties=1
type TrackerSettings struct {
	// backend is which tracker the company runs.
	// +optional
	// +default="Native"
	Backend TrackerBackend `json:"backend,omitempty"`

	// native is the policy of the engine's own tracker, for backend Native.
	// +optional
	Native NativeTrackerSettings `json:"native,omitzero"`
}

// KnowledgeBackend is which knowledge base a company runs. It maps onto
// Crewlet's knowledge.backend (Native is `native`, Confluence is
// `confluence`, None is `none`).
// +kubebuilder:validation:Enum=Native;Confluence;None
type KnowledgeBackend string

// The knowledge backends.
const (
	// KnowledgeNative is the engine's own knowledge base.
	KnowledgeNative KnowledgeBackend = "Native"
	// KnowledgeConfluence is Confluence, connected by the company's
	// Confluence Integration.
	KnowledgeConfluence KnowledgeBackend = "Confluence"
	// KnowledgeNone is a company with no knowledge base.
	KnowledgeNone KnowledgeBackend = "None"
)

// KnowledgeSettings is which knowledge base a company runs and how it is
// searched. It maps onto Crewlet's knowledge block.
// +kubebuilder:validation:MinProperties=1
type KnowledgeSettings struct {
	// backend is which knowledge base the company runs.
	// +optional
	// +default="Native"
	Backend KnowledgeBackend `json:"backend,omitempty"`

	// scope narrows every knowledge search to these containers. Empty
	// searches the whole company. Each is a container key, which Crewlet
	// requires to be an upper-case letter followed by 1 to 9 upper-case
	// letters or digits, such as PLAT. Crewlet's `scope`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=2
	// +kubebuilder:validation:items:MaxLength=10
	// +kubebuilder:validation:items:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	Scope []string `json:"scope,omitempty"`

	// skillsContainer is the container holding tool-skill pages, a container
	// key (see scope). Unset takes Crewlet's default container; an empty
	// string turns tool skills off. Crewlet's `skills_container`.
	// +optional
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:XValidation:rule="self == '' || self.matches('^[A-Z][A-Z0-9]{1,9}$')",message="skillsContainer must be empty or a container key: an upper-case letter followed by 1 to 9 upper-case letters or digits"
	SkillsContainer *string `json:"skillsContainer,omitempty"`

	// rootSpace is the container holding the company's own pages, such as
	// the root onboarding page, a container key (see scope). Unset takes
	// Crewlet's default. Crewlet's `root_space`.
	// +optional
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	RootSpace string `json:"rootSpace,omitempty"`

	// vectors fuses semantic recall into knowledge and work search; false
	// also stops embedding the corpus. Unset is on exactly when the company
	// has an embeddings provider; true requires one (spec.embeddings).
	// Crewlet's `vectors`.
	// +optional
	Vectors *bool `json:"vectors,omitempty"`
}

// LLMProviderKind is the kind of model provider a reference names.
// +kubebuilder:validation:Enum=LLMProvider;PlatformLLMProvider
type LLMProviderKind string

// The model provider kinds.
const (
	// LLMProviderKindLLMProvider is an LLMProvider in the referring object's
	// namespace.
	LLMProviderKindLLMProvider LLMProviderKind = "LLMProvider"
	// LLMProviderKindPlatform is an entry of the Company's EngineClass
	// spec.platformLLMProviders; the Company must set
	// spec.llm.usePlatformProviders.
	LLMProviderKindPlatform LLMProviderKind = "PlatformLLMProvider"
)

// LLMProviderReference names a model provider: an LLMProvider in the
// referring object's namespace, or a provider the EngineClass offers.
type LLMProviderReference struct {
	// kind is the kind of provider named.
	// +optional
	// +default="LLMProvider"
	Kind LLMProviderKind `json:"kind,omitempty"`

	// name is the LLMProvider's metadata.name, or the platform provider's
	// name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// CompanyLLMSettings is how a company chooses among its model providers.
// +kubebuilder:validation:MinProperties=1
type CompanyLLMSettings struct {
	// order is the providers' precedence. A seat with no default chain runs
	// on the first provider here, and on it alone: Crewlet tries no other
	// provider of the order when its keys are all cooling down, so give a
	// seat a chain for a fallback. The providers the order leaves out follow
	// it: the company's LLMProviders by name, then the platform providers by
	// name. Crewlet's providers.llm_order.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="order must name each provider once"
	Order []LLMProviderReference `json:"order,omitempty"`

	// usePlatformProviders adds the EngineClass's platformLLMProviders to the
	// company, so seats can run on models the platform provides. Their keys
	// are pushed into the company's engine, where the tenant can read them
	// (see the class's platformLLMProviders).
	// +optional
	UsePlatformProviders *bool `json:"usePlatformProviders,omitempty"`
}

// EmbeddingProviderType is which embeddings API a company uses. It maps onto
// Crewlet's providers.embeddings.type (OpenAI is `openai`, OpenAICompatible
// is `openai-compatible`).
// +kubebuilder:validation:Enum=OpenAI;OpenAICompatible
type EmbeddingProviderType string

// The embedding provider types.
const (
	// EmbeddingProviderOpenAI is OpenAI's embeddings API.
	EmbeddingProviderOpenAI EmbeddingProviderType = "OpenAI"
	// EmbeddingProviderOpenAICompatible is any endpoint that speaks it.
	EmbeddingProviderOpenAICompatible EmbeddingProviderType = "OpenAICompatible"
)

// EmbeddingSettings is the provider a company computes vectors with, for
// semantic knowledge search and for recall of a seat's memory. Each field maps
// onto the same-named field of Crewlet's providers.embeddings.
// +kubebuilder:validation:XValidation:rule="self.type != 'OpenAICompatible' || has(self.baseURL)",message="baseURL is required for type OpenAICompatible"
// +kubebuilder:validation:XValidation:rule="self.type != 'OpenAI' || has(self.apiKeySecretRef)",message="apiKeySecretRef is required for type OpenAI"
type EmbeddingSettings struct {
	// type is which embeddings API the provider speaks.
	// +optional
	// +default="OpenAI"
	Type EmbeddingProviderType `json:"type,omitempty"`

	// model is the embedding model id. Crewlet's `model`. Crewlet trims it of
	// white space and refuses an embeddings provider with nothing left, as
	// naming no model.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="model may not be white space alone: Crewlet trims it, and refuses an embeddings provider that names no model"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="model may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	Model string `json:"model,omitempty"`

	// apiKeySecretRef is the Secret key holding the API key. Crewlet's
	// `api_key`, a credential position. Required for type OpenAI: Crewlet
	// reads OPENAI_API_KEY from the environment when it is unset, and the
	// engine's environment never carries one (see spec.runtime.env). An
	// OpenAICompatible endpoint that needs no key may omit it.
	// +optional
	APIKeySecretRef SecretKeyReference `json:"apiKeySecretRef,omitzero"`

	// baseURL is the endpoint of an OpenAI-compatible service. Crewlet's
	// `base_url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="baseURL must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="baseURL may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	BaseURL string `json:"baseURL,omitempty"`

	// dimensions is the vector width. Unset takes the model's own width,
	// which Crewlet knows for the models it names; any other model needs it
	// stated, and it must match what the model produces.
	// +optional
	// +kubebuilder:validation:Minimum=64
	// +kubebuilder:validation:Maximum=4096
	Dimensions int32 `json:"dimensions,omitempty"`

	// maxInputTokens is the most tokens one input may hold. Unset takes the
	// model's documented window; it may only lower it.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxInputTokens int32 `json:"maxInputTokens,omitempty"`

	// maxBatchInputs is the most inputs one request may carry. Unset takes
	// the model's documented limit; it may only lower it.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxBatchInputs int32 `json:"maxBatchInputs,omitempty"`

	// maxBatchTokens is the most tokens one request may carry, summed over
	// its inputs. Unset takes the model's documented limit; it may only
	// lower it.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxBatchTokens int32 `json:"maxBatchTokens,omitempty"`
}

// CodingAgent is the coding agent a sandbox runs. It maps onto Crewlet's
// coding agents (ClaudeCode is `claude-code`, OpenCode is `opencode`).
// +kubebuilder:validation:Enum=ClaudeCode;OpenCode
type CodingAgent string

// The coding agents.
const (
	// CodingAgentClaudeCode is Claude Code.
	CodingAgentClaudeCode CodingAgent = "ClaudeCode"
	// CodingAgentOpenCode is OpenCode.
	CodingAgentOpenCode CodingAgent = "OpenCode"
)

// SandboxFile is a file written into a sandbox before its commands run. Its
// content is a credential position in Crewlet (it typically holds a git or
// registry login), so it always comes from a Secret.
type SandboxFile struct {
	// path is where the file is written inside the sandbox.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Path string `json:"path,omitempty"`

	// valueFrom is the Secret key holding the file's content.
	// +required
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// SandboxSetupStep is one provisioning step applied to a sandbox before the
// coding agent starts. It maps onto a Crewlet sandbox setup step.
type SandboxSetupStep struct {
	// name identifies the step in logs and failure messages.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name,omitempty"`

	// files are written into the sandbox before the commands run. Crewlet's
	// `files`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=path
	// +kubebuilder:validation:MaxItems=16
	Files []SandboxFile `json:"files,omitempty"`

	// commands are shell commands run in order; one that exits non-zero
	// fails the sandbox. Crewlet's `commands`. Crewlet substitutes every
	// ${NAME} in them (NAME an identifier) from the engine's own secret
	// store and environment before the sandbox runs them, so ${HOME} would be
	// the engine's, not the sandbox's, and the API refuses ${NAME}: write a
	// sandbox variable as $NAME. Parameter expansions such as ${1:-x} are
	// not substituted and are allowed.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="self.all(x, !x.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]'))",message="commands may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	Commands []string `json:"commands,omitempty"`

	// env is merged into the environment of the commands and of the run.
	// Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SecretEnvVar `json:"env,omitempty"`

	// brief tells the coding agent what this step made true about the
	// sandbox.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Brief string `json:"brief,omitempty"`

	// timeoutSeconds caps each command of the step.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// E2BSandbox is the remote sandbox service seats run code in. Each field maps
// onto the same-named field of Crewlet's providers.sandbox.e2b.
type E2BSandbox struct {
	// apiKeySecretRef is the Secret key holding the service's API key.
	// Crewlet's `api_key`, a credential position.
	// +required
	APIKeySecretRef SecretKeyReference `json:"apiKeySecretRef,omitzero"`

	// domain points at a self-hosted cluster of the service. Unset is the
	// vendor's cloud.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="domain may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	Domain string `json:"domain,omitempty"`

	// template is the sandbox template, which also fixes each sandbox's CPU,
	// memory and disk. Unset, each run gets the vendor's prebuilt template
	// for its coding agent's CLI (claude or opencode); a template set here is
	// used for every run, so it must carry the CLI of every coding agent and
	// Agent-mode CLIAgent provider the company runs.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="template may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	Template string `json:"template,omitempty"`
}

// SandboxSettings is where a company's seats run code. It maps onto Crewlet's
// providers.sandbox.
//
// v1alpha1 offers Crewlet's remote sandbox (E2B) only. Crewlet's local
// placements are not offered: `direct` would run every seat's code inside the
// engine pod with the engine's own credentials, and `container` needs a
// container runtime a pod does not have.
type SandboxSettings struct {
	// e2b is the remote sandbox service.
	// +required
	E2B E2BSandbox `json:"e2b,omitzero"`

	// defaultCodingAgent is the coding agent of seats that name none.
	// Crewlet's `default_coding_agent`.
	// +optional
	DefaultCodingAgent CodingAgent `json:"defaultCodingAgent,omitempty"`

	// defaultTimeoutSeconds is how long a sandbox outlives an engine that
	// stopped renewing it. It never stops a running job. Crewlet's
	// `default_timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	DefaultTimeoutSeconds int32 `json:"defaultTimeoutSeconds,omitempty"`

	// defaultPauseTTLSeconds is how long a sandbox waiting on a person's
	// answer stays paused before it is reclaimed. Unset takes Crewlet's
	// default of 1800; 0 never pauses. Crewlet's
	// `default_pause_ttl_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DefaultPauseTTLSeconds *int32 `json:"defaultPauseTTLSeconds,omitempty"`

	// defaultMaxTurns caps the agentic rounds of a coding run on seats that
	// name none. Unset or 0 is uncapped. Crewlet's `default_max_turns`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DefaultMaxTurns *int32 `json:"defaultMaxTurns,omitempty"`

	// setup is applied to every sandbox before each seat's own steps.
	// Crewlet's `setup`.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	Setup []SandboxSetupStep `json:"setup,omitempty"`
}

// CompanyIntegrationSettings is company-wide behaviour of the integration
// reconcile loop. The vendors themselves are Integration objects.
// +kubebuilder:validation:MinProperties=1
type CompanyIntegrationSettings struct {
	// checkIntervalSeconds is how long a working integration is trusted
	// before the engine reads it back, and so how long access revoked at the
	// vendor goes unnoticed. Unset takes Crewlet's default of 600. Crewlet's
	// integrations.check_interval_seconds.
	// +optional
	// +kubebuilder:validation:Minimum=60
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds,omitempty"`
}

// ManagementPolicy says who owns a company's live document.
// +kubebuilder:validation:Enum=Enforce;Observe;Seed
type ManagementPolicy string

// The management policies.
const (
	// ManagementPolicyEnforce makes the operator the document's only
	// writer. The engine refuses every other credential a change to the
	// document (403 config_managed), the published access token and a person
	// signed in at its dashboard included, and its dashboard shows the
	// document read-only; a revision made some other way all the same (the
	// engine's own writes, or one made before the data pod restarted onto the
	// policy) is reported and overwritten.
	ManagementPolicyEnforce ManagementPolicy = "Enforce"
	// ManagementPolicyObserve renders and compares, and never writes; a
	// difference is reported only. The engine's own users may change the
	// document.
	ManagementPolicyObserve ManagementPolicy = "Observe"
	// ManagementPolicySeed writes the document once, into an engine that
	// has none, and then leaves it to the engine and its dashboard, whose
	// users may change it. Changes to the Company's children are ignored
	// afterwards and say so in their status.
	ManagementPolicySeed ManagementPolicy = "Seed"
)

// ConfigSettings is how the operator manages a company's document.
// +kubebuilder:validation:MinProperties=1
type ConfigSettings struct {
	// managementPolicy says who owns the document. Under Enforce the data
	// pod's crewlet.yaml sets Crewlet's api.auth.company_writers to the
	// operator's own token, so the engine refuses every other credential a
	// change to the document; under Observe and Seed it sets none, and
	// Crewlet's authority table decides who may. The engine reads
	// crewlet.yaml only at start, so changing the policy to or from Enforce
	// restarts the data pod.
	// +optional
	// +default="Enforce"
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`
}

// DataTopology is the pods that hold a company's estate.
// +kubebuilder:validation:MinProperties=1
type DataTopology struct {
	// replicas is the number of data pods. It must be one of the class's
	// spec.data.allowedReplicas. v1alpha1 runs exactly one data pod, so 1 is
	// the only value admitted. A highly available estate of 3 or 5 is a
	// later addition, and it comes with a rule that the count cannot change
	// once set, because the event stream's replication is fixed when the
	// estate is created: admitting 3 or 5 before the operator can run them
	// would leave Companies that could never be corrected.
	// +optional
	// +default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Enum=1
	Replicas int32 `json:"replicas,omitempty"`
}

// DataPool is the name of the pool of a company's data pods. A seat placed in
// it runs on the data pods; no seat pool may take the name.
const DataPool = "data"

// SeatPoolMode is how a seat pool runs the seats placed in it.
// +kubebuilder:validation:Enum=Shared;PerSeat
type SeatPoolMode string

// The seat pool modes.
const (
	// SeatPoolShared runs replicas pods that share the pool's seats between
	// them: each takes an even share, and a pod that stops hands its seats
	// to the others.
	SeatPoolShared SeatPoolMode = "Shared"
	// SeatPoolPerSeat runs replicas pods for each seat placed in the pool,
	// each holding that seat alone. It needs the class's
	// seatPools.allowPerSeat.
	SeatPoolPerSeat SeatPoolMode = "PerSeat"
)

// SeatPool is a set of stateless pods that run seats: they hold no data, join
// the company's event stream through the data pods, and keep a scratch store
// they delete at every start. A seat runs in it when its Seat's
// spec.placement.pool names it, or when it is the Company's defaultSeatPool
// and the Seat names no pool. The class's spec.seatPools.policy must be
// Allowed.
//
// The operator runs a Shared pool as the StatefulSet
// <company>-seats-<name>-<hash>, and each seat of a PerSeat pool as
// <company>-seat-<handle>-<hash> (the hash keeps the name the Company's own
// in a namespace another Company shares), started only
// once the data pods are ready and rolled only after them. The pods serve
// their probes alone, on the engine's port: a pod is ready once its engine
// has joined the company's fleet and may claim seats, and a rollout replaces
// the next pod only then.
// +kubebuilder:validation:XValidation:rule="!has(self.mode) || self.mode != 'PerSeat' || !has(self.replicas) || (self.replicas >= 1 && self.replicas <= 2)",message="a PerSeat pool runs 1 or 2 replicas for each seat: a second pod is a warm standby, and the seat is never held by more than one"
type SeatPool struct {
	// name identifies the pool: a DNS label of at most 20 characters, other
	// than data, which is the data pods' pool. It names the pool's pods and
	// is the value of the crewlet.ai/pool label a Shared pool's seats are
	// pinned to.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self != 'data'",message="data is the pool of the data pods, which spec.topology.data sizes: name the pool something else"
	Name string `json:"name,omitempty"`

	// mode is how the pool runs its seats.
	// +optional
	// +default="Shared"
	Mode SeatPoolMode `json:"mode,omitempty"`

	// replicas is, for a Shared pool, how many pods it runs, 0 to 100: at 0
	// its seats run nowhere, and their messages wait for a pod. For a
	// PerSeat pool it is how many pods it runs for each of its seats, 1 or
	// 2.
	// +optional
	// +default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Replicas *int32 `json:"replicas,omitempty"`

	// maxConcurrentTurns is how many turns one of the pool's pods runs at
	// once: Crewlet's node.max_concurrent. Unset takes the class's
	// seatPools.defaults.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentTurns int32 `json:"maxConcurrentTurns,omitempty"`

	// terminationGracePeriodSeconds is how long one of the pool's pods may
	// take to stop, finishing its running turns: the engine puts no bound
	// of its own on that drain, so it must cover the longest turn. Unset
	// takes the class's seatPools.defaults, else 300.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:validation:Maximum=3600
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// CompanyTopology is how a company's engine is laid out in pods: one data pod
// that holds the estate, serves the API and runs the background workers, and
// the seat pools, stateless pods that run the seats placed in them. Without
// seat pools every seat runs on the data pod.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="!has(self.defaultSeatPool) || self.defaultSeatPool == 'data' || (has(self.seatPools) && self.seatPools.exists(p, p.name == self.defaultSeatPool && (!has(p.mode) || p.mode == 'Shared')))",message="defaultSeatPool must be data or the name of a Shared pool of seatPools: every seat that names no pool runs there, and a PerSeat default would give each of them a pod of its own"
type CompanyTopology struct {
	// data is the pods that hold the estate.
	// +optional
	// +default={"replicas": 1}
	Data DataTopology `json:"data,omitzero"`

	// defaultSeatPool is the pool of every agent seat whose Seat names no
	// spec.placement.pool: data, or the name of a Shared pool of seatPools.
	// +optional
	// +default="data"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	DefaultSeatPool string `json:"defaultSeatPool,omitempty"`

	// seatPools are stateless pods that run seats. They need the class's
	// spec.seatPools.policy Allowed, and together run at most its maxPods
	// pods.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=8
	SeatPools []SeatPool `json:"seatPools,omitempty"`
}

// CompanyExposure overrides how a company is reached.
// +kubebuilder:validation:MinProperties=1
type CompanyExposure struct {
	// publicBaseURL is the HTTPS address vendors deliver webhooks to, and a
	// remote sandbox calls the engine back on, overriding the one the class's
	// webhooks.hostname derives. Required when the class's webhooks.mode is
	// External, where something outside the operator (a relay, an API
	// gateway) forwards it to the engine's public listener. A trailing '/'
	// is dropped, as Crewlet drops it. Crewlet's api.public.external_url,
	// in the data pod's crewlet.yaml, which the engine reads only at start:
	// changing it restarts the data pod.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https' && url(self).getHostname() != ''",message="publicBaseURL must be an absolute https URL"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?') && !self.contains('#')",message="publicBaseURL may not have a query or a fragment: Crewlet builds every webhook and sandbox address by appending a path to it"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="publicBaseURL may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	PublicBaseURL string `json:"publicBaseURL,omitempty"`

	// dashboardBaseURL is where a browser reaches the engine: its dashboard,
	// and the sign-in of the company's people. It overrides the address of
	// the class's API route: set it when the class routes no API, or people
	// reach it through something in front of the route (a single sign-on
	// proxy, say). Crewlet's api.external_url, in the data pod's
	// crewlet.yaml, which the engine reads only at start: changing it
	// restarts the data pod. The engine takes from it the origin a person's
	// write must come from, whether the session cookie is Secure (https)
	// and its scope, and the base of every link a person follows: a link an
	// agent composes, an invitation (/dashboard#/invite/...), a password
	// link. An address other than the route's must serve the engine, its
	// /dashboard and its /auth routes included. The route, when the class
	// has one, stays an origin the engine accepts. It is never the public
	// address: the public listener serves no dashboard. A trailing '/' is
	// dropped.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https'] && url(self).getHostname() != ''",message="dashboardBaseURL must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?') && !self.contains('#')",message="dashboardBaseURL may not have a query or a fragment: Crewlet builds every link a person follows, an invitation and a password link included, by appending a path to it"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="dashboardBaseURL may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	DashboardBaseURL string `json:"dashboardBaseURL,omitempty"`
}

// CompanyRuntime is a company's bounded overrides of its class.
// +kubebuilder:validation:MinProperties=1
type CompanyRuntime struct {
	// version replaces the tag of the class's engine image, for example
	// v1.2.4. It is honoured only when the class sets
	// engine.allowVersionOverride; the operator refuses a version older than
	// the one the company's data was last written by.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`
	Version string `json:"version,omitempty"`

	// env adds environment variables to the engine container, for platform
	// wiring the engine reads from its environment. They are merged over the
	// class's podTemplate.spec.env by name: a variable here replaces the
	// class's variable of the same name.
	//
	// Names starting with CREWLET_ or OTEL_ are the operator's own.
	// ANTHROPIC_API_KEY and OPENAI_API_KEY are refused too: Crewlet reads them
	// as the key of a model or embeddings provider that names none, so a
	// value here would be a credential outside the engine's secret store;
	// give the provider its key with apiKeySecretRefs or apiKeySecretRef.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(e, !e.name.startsWith('CREWLET_') && !e.name.startsWith('OTEL_'))",message="variables starting with CREWLET_ or OTEL_ are set by the operator"
	// +kubebuilder:validation:XValidation:rule="self.all(e, !(e.name in ['ANTHROPIC_API_KEY', 'OPENAI_API_KEY']))",message="ANTHROPIC_API_KEY and OPENAI_API_KEY are model provider keys: give a provider its key with a Secret reference instead"
	Env []EnvVar `json:"env,omitempty"`
}

// engineConfig is a list of entries rather than one opaque object, which is how
// Crewlet would spell it, because the API server's rules can see a typed name
// but not the keys of an object that has no schema: the list is what lets the
// rule on engineConfig refuse a reserved key at admission, before a credential
// could ever be stored. A list keyed by name also lets server-side apply merge
// it entry by entry.

// EngineConfigEntry is one top-level key of the company document that
// spec.engineConfig passes through, and its value.
type EngineConfigEntry struct {
	// name is the key, as Crewlet spells it, such as turn_engine.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// value is the key's value: an object, a list, a string, a number or a
	// boolean, kept as written.
	// +required
	Value *EngineConfigValue `json:"value,omitempty"`
}

// EngineConfigValue is a type of its own rather than runtime.RawExtension
// because a RawExtension's schema is an object, which would refuse a scalar
// setting such as notification_rate_limit; this type's schema has no type at
// all.

// EngineConfigValue is any JSON value, kept byte for byte as it was written.
// +kubebuilder:validation:Type=""
// +kubebuilder:pruning:PreserveUnknownFields
type EngineConfigValue struct {
	// Raw is the value's JSON encoding.
	Raw []byte `json:"-"`
}

// MarshalJSON returns the value as it was written.
func (v EngineConfigValue) MarshalJSON() ([]byte, error) {
	if v.Raw == nil {
		return []byte("null"), nil
	}
	return v.Raw, nil
}

// UnmarshalJSON keeps a copy of the value's encoding.
func (v *EngineConfigValue) UnmarshalJSON(data []byte) error {
	v.Raw = append(v.Raw[:0], data...)
	return nil
}

// DeletionPolicy is what happens to a company's data volumes when the Company
// is deleted.
// +kubebuilder:validation:Enum=Retain;Delete
type DeletionPolicy string

// The deletion policies.
const (
	// DeletionPolicyRetain keeps the data volumes and the keyring Secret
	// <name>-keyring that opens the secrets sealed on them. A Company created
	// again with the same name and namespace finds both and its estate again.
	DeletionPolicyRetain DeletionPolicy = "Retain"
	// DeletionPolicyDelete deletes them with the Company.
	DeletionPolicyDelete DeletionPolicy = "Delete"
)

// CompanyObjects is a company's own access to its class's object store.
// +kubebuilder:validation:MinProperties=1
type CompanyObjects struct {
	// credentialsSecretRef is a Secret, in the Company's namespace, holding
	// the keys accessKeyID and secretAccessKey (S3AccessKeyIDKey,
	// S3SecretAccessKeyKey) the engine reaches its class's S3 bucket with:
	// Crewlet's store.objects.s3.access_key_id and secret_access_key. Used
	// only while the class's objects.backend is S3.
	//
	// Issue them per company, with a policy that admits the company's own
	// prefix of the bucket only (the class's objects.s3.prefix, expanded):
	// they are written into the engine pod's crewlet.yaml, and code the
	// tenant runs in the pod (a stdio MCP server, a sandbox setup command)
	// can read them. Unset uses the pod's own credentials through the SDK's
	// default chain: a workload identity bound through the class's
	// serviceAccount.annotations, which every company of the class shares
	// unless it is scoped per tenant.
	// +optional
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef,omitzero"`
}

// CompanySpec is a company: the engine that runs it and the company-wide part
// of its document. Its units, seats, model providers, tool servers and
// integrations are objects of their own that name the Company in their
// spec.companyRef.
// +kubebuilder:validation:XValidation:rule="!has(self.knowledge) || !has(self.knowledge.vectors) || !self.knowledge.vectors || has(self.embeddings)",message="knowledge.vectors needs spec.embeddings: there is nothing to compute an embedding with"
type CompanySpec struct {
	// engineClassName is the EngineClass the company runs with. Unset uses
	// the class annotated crewlet.ai/is-default-class=true.
	//
	// Whoever may write a Company may name any class, including one that
	// offers platformLLMProviders or larger limits; a class admits only the
	// namespaces its spec.allowedNamespaces selects, and a Company in any
	// other is not accepted (ClassNotAllowed).
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	EngineClassName string `json:"engineClassName,omitempty"`

	// companyName is the company's name. Crewlet derives every seat's
	// identity from it and the seat's handle, so it cannot be changed: a new
	// name would orphan every seat's memory. Crewlet's `name`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyName is immutable: every seat's identity is derived from it"
	CompanyName string `json:"companyName,omitempty"`

	// mission is one line on what the company is for; every seat's prompt
	// carries it. Crewlet's `mission`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Mission string `json:"mission,omitempty"`

	// vision is a longer statement of where the company is going. Crewlet's
	// `vision`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	Vision string `json:"vision,omitempty"`

	// policies are company-wide rules every seat's prompt carries. Crewlet's
	// `policies`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=2048
	Policies []string `json:"policies,omitempty"`

	// timeZone is the company's clock, an IANA time zone such as
	// Europe/Berlin: where its days begin for budgets, due dates and
	// schedules. Unset is UTC. Crewlet's `timezone`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`
	TimeZone string `json:"timeZone,omitempty"`

	// tracker is which work tracker the company runs.
	// +optional
	Tracker TrackerSettings `json:"tracker,omitzero"`

	// knowledge is which knowledge base the company runs.
	// +optional
	Knowledge KnowledgeSettings `json:"knowledge,omitzero"`

	// tokenBudget caps the tokens all seats together may spend per window.
	// Crewlet's `token_budget`.
	// +optional
	TokenBudget TokenBudget `json:"tokenBudget,omitzero"`

	// llm is how the company chooses among its model providers.
	// +optional
	LLM CompanyLLMSettings `json:"llm,omitzero"`

	// embeddings is the provider vectors are computed with. Unset leaves
	// search keyword-only. Crewlet's providers.embeddings.
	// +optional
	Embeddings EmbeddingSettings `json:"embeddings,omitzero"`

	// sandbox is where seats run code. Unset gives no seat a sandbox.
	// Crewlet's providers.sandbox.
	// +optional
	Sandbox SandboxSettings `json:"sandbox,omitzero"`

	// integrations is company-wide integration behaviour.
	// +optional
	Integrations CompanyIntegrationSettings `json:"integrations,omitzero"`

	// engineConfig is passed through into the company document as written,
	// for the engine's tuning blocks that have no field here: turn_engine,
	// learning, scheduling, workers, skill_variables and the
	// notification_* settings, spelled as Crewlet spells them. Each entry is
	// one top-level key of the document and its value. The operator does not
	// interpret it; the engine validates it in the dry run before every
	// apply, and a refusal is reported on the Company.
	//
	// It may not set what another field or object of this API owns: name,
	// mission, vision, policies, timezone, tracker, knowledge, token_budget,
	// providers, integrations, mcp_servers, roles or units. Every position
	// Crewlet holds a credential in lives under one of those keys, so
	// engineConfig can never carry a credential: the API server refuses a
	// Company whose engineConfig names one of them, before anything is
	// stored.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="self.all(e, !(e.name in ['name', 'mission', 'vision', 'policies', 'timezone', 'tracker', 'knowledge', 'token_budget', 'providers', 'integrations', 'mcp_servers', 'roles', 'units']))",message="engineConfig may not set name, mission, vision, policies, timezone, tracker, knowledge, token_budget, providers, integrations, mcp_servers, roles or units: those are fields and objects of this API, and the only blocks that hold credentials"
	EngineConfig []EngineConfigEntry `json:"engineConfig,omitempty"`

	// config is how the operator manages the company's document.
	// +optional
	// +default={"managementPolicy": "Enforce"}
	Config ConfigSettings `json:"config,omitzero"`

	// topology is how the engine is laid out in pods.
	// +optional
	// +default={"data": {"replicas": 1}}
	Topology CompanyTopology `json:"topology,omitzero"`

	// exposure overrides how the company is reached.
	// +optional
	Exposure CompanyExposure `json:"exposure,omitzero"`

	// runtime is the company's bounded overrides of its class.
	// +optional
	Runtime CompanyRuntime `json:"runtime,omitzero"`

	// objects is the company's own access to its class's object store.
	// +optional
	Objects CompanyObjects `json:"objects,omitzero"`

	// suspend stops the engine and keeps its data volumes; setting it back
	// to false starts it again. It takes effect whatever else is wrong with
	// the Company, even while it is not Accepted. Meanwhile the Company
	// reports Ready=False with the reason Suspended, and vendors keep
	// delivering webhooks, some disabling a subscription after a long run of
	// failed deliveries.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// deletionPolicy is what happens to the data volumes when the Company is
	// deleted. With Retain the keyring Secret is kept as well, since the
	// volumes cannot be read without it.
	// +optional
	// +default="Retain"
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// EngineStatus is what runs a company.
// +kubebuilder:validation:MinProperties=1
type EngineStatus struct {
	// image is the engine image the data pods last ran on, recorded once
	// every data pod ran it and was ready. A rollout that has not finished
	// leaves the previous image here.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image,omitempty"`

	// version is the tag of that image, such as v1.2.3; unset for an image
	// pinned by digest alone. The engine migrates its data forward only, so
	// the operator refuses to roll the data pods to an image whose tag is an
	// older semantic version than this one (RuntimeReady=False,
	// DowngradeRefused). The operator also records it on each data volume
	// claim (the annotation crewlet.ai/engine-version), which keeps the floor
	// when this status is lost. A version that never became ready is never
	// recorded, so a rollout that failed can still be taken back.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Version string `json:"version,omitempty"`
}

// DataTopologyStatus is the state of a company's data pods.
// +kubebuilder:validation:MinProperties=1
type DataTopologyStatus struct {
	// replicas is the number of data pods.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of data pods that are ready.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`

	// updatedReplicas is the number of data pods that run the current pod
	// template.
	// +optional
	// +kubebuilder:validation:Minimum=0
	UpdatedReplicas *int32 `json:"updatedReplicas,omitempty"`
}

// SeatPoolStatus is the state of one seat pool's pods.
type SeatPoolStatus struct {
	// name is the pool's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	Name string `json:"name,omitempty"`

	// mode is how the pool runs its seats.
	// +optional
	Mode SeatPoolMode `json:"mode,omitempty"`

	// replicas is the number of the pool's pods, every seat's together for
	// a PerSeat pool.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of the pool's pods that are ready.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`

	// updatedReplicas is the number of the pool's pods that run the
	// current pod template.
	// +optional
	// +kubebuilder:validation:Minimum=0
	UpdatedReplicas *int32 `json:"updatedReplicas,omitempty"`

	// seats is the number of agent seats placed in the pool.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Seats *int32 `json:"seats,omitempty"`
}

// TopologyStatus is the state of a company's pods.
// +kubebuilder:validation:MinProperties=1
type TopologyStatus struct {
	// data is the state of the data pods.
	// +optional
	Data DataTopologyStatus `json:"data,omitzero"`

	// seatPools is the state of each seat pool's pods.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=8
	SeatPools []SeatPoolStatus `json:"seatPools,omitempty"`
}

// AppliedObjectCounts is how many objects of each kind the applied revision
// was rendered from.
// +kubebuilder:validation:MinProperties=1
type AppliedObjectCounts struct {
	// units is the number of Units.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Units *int32 `json:"units,omitempty"`

	// seats is the number of Seats.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Seats *int32 `json:"seats,omitempty"`

	// llmProviders is the number of model providers, platform providers
	// included.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LLMProviders *int32 `json:"llmProviders,omitempty"`

	// mcpServers is the number of MCPServers.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MCPServers *int32 `json:"mcpServers,omitempty"`

	// integrations is the number of Integrations.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Integrations *int32 `json:"integrations,omitempty"`

	// schedules is the number of Schedules.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Schedules *int32 `json:"schedules,omitempty"`
}

// ConfigStatus is the revision of the company document the engine runs.
// +kubebuilder:validation:MinProperties=1
type ConfigStatus struct {
	// revisionID is the engine's id of the last revision the operator saw
	// run the rendered document: the one it applied, one the engine
	// activated with the same content (a reload), or, under Observe, the
	// one it found in sync. Under Seed it is the revision the document was
	// seeded as.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	RevisionID string `json:"revisionID,omitempty"`

	// epoch is the engine's activation counter for that revision. The
	// engine applies a revision asynchronously; it runs it once the applied
	// epoch its /health reports has reached this.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Epoch *int64 `json:"epoch,omitempty"`

	// appliedHash is the hash of the rendered document and of the values
	// of the Secret keys it references, as last applied: a change to
	// either is applied as a new revision, since the engine reads a
	// rotated value only when a revision activates. The values enter it
	// only as keyed fingerprints, so it reveals nothing about them.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	AppliedHash string `json:"appliedHash,omitempty"`

	// appliedAt is when the operator last applied a revision.
	// +optional
	AppliedAt *metav1.Time `json:"appliedAt,omitempty"`

	// objects counts the objects the revision was rendered from.
	// +optional
	Objects AppliedObjectCounts `json:"objects,omitzero"`

	// placement is where the revision the engine runs pins its agent seats,
	// read from the engine's document under every management policy. The
	// operator keeps a seat pool's pods running while the running revision
	// pins a seat to them, even after the pool or the seat's placement is
	// gone from the spec: a pod stopped while its seat is still pinned to
	// it would leave the seat held by nobody, and a blocked document keeps
	// the last good revision running. Once a revision that pins nothing to
	// them runs, the pods are deleted.
	// +optional
	Placement RevisionPlacement `json:"placement,omitzero"`
}

// RevisionPlacement is where a revision of the company document pins its
// agent seats: the values of the two placement labels its roles name. Only
// values that can name one of the company's pods are recorded: a pool name
// and a seat handle.
// +kubebuilder:validation:MinProperties=1
type RevisionPlacement struct {
	// pools are the values of the crewlet.ai/pool placement label: data,
	// and each Shared pool a seat is pinned to.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=20
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Pools []string `json:"pools,omitempty"`

	// seats are the values of the crewlet.ai/seat placement label: the
	// handle of each seat pinned to its own pods in a PerSeat pool. A
	// revision that pins more seats than the list holds records the first
	// in handle order; the pods of the others are kept only while their
	// seats stay in a PerSeat pool.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=1024
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Seats []string `json:"seats,omitempty"`
}

// CompanyEndpoints is where a company is reached. Every address is a base URL
// with its scheme, so that a caller appends a path to it and never
// reconstructs one from the EngineClass's templates.
// +kubebuilder:validation:MinProperties=1
type CompanyEndpoints struct {
	// api is the in-cluster base URL of the engine's API: the company's
	// Service, for callers inside the cluster.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	API string `json:"api,omitempty"`

	// exposedAPI is the base URL of the engine's API through the class's
	// exposure.api, for callers outside the cluster: https://<hostname>
	// when the route terminates TLS (the Ingress has tls, or the Gateway
	// listener the route attaches to is HTTPS), http://<hostname> otherwise,
	// with the hostname template expanded. Set only while exposure.api.mode
	// is Gateway or Ingress and the route has been accepted.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	ExposedAPI string `json:"exposedAPI,omitempty"`

	// publicBaseURL is the address vendors deliver webhooks to, when the
	// company has one: spec.exposure.publicBaseURL, or https://<hostname>
	// from the class's webhooks.hostname. It reaches the engine's public
	// listener, and every vendor route is a path under it, such as
	// /webhooks/forge for an Atlassian Forge relay. Unset while the class's
	// webhooks.mode is None, which exposes nothing, and under External
	// before the Company states its address: the engine is then told the
	// listener's address inside the cluster, http://<company>-engine.
	// <namespace>.svc:8081, which no vendor can reach.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	PublicBaseURL string `json:"publicBaseURL,omitempty"`

	// dashboardBaseURL is where a browser reaches the engine, its dashboard
	// and its people's sign-in: spec.exposure.dashboardBaseURL, else the
	// address of the class's API route, on the scheme exposedAPI takes, else
	// http://localhost:8080, the address kubectl port-forward serves the
	// engine Service's API port at. It does not wait for the route to be
	// accepted, as exposedAPI does: it is the engine's api.external_url,
	// which the engine reads only at start, and a flip of acceptance would
	// otherwise restart the data pod. The engine builds every link a person
	// follows on it, an invitation's included, so with the third these lead
	// to a localhost only a port-forwarding browser reaches.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	DashboardBaseURL string `json:"dashboardBaseURL,omitempty"`
}

// CompanyStatus is what the operator observed about a Company.
// +kubebuilder:validation:MinProperties=1
type CompanyStatus struct {
	// conditions are Ready (the summary), Accepted, RuntimeReady,
	// SecretsSynced, ConfigApplied and Exposed, which Ready summarizes, and
	// EngineHealthy, IdentityClaimed and SeatsPlaced, which it does not.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=16
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// engine is what runs the company.
	// +optional
	Engine EngineStatus `json:"engine,omitzero"`

	// topology is the state of the company's pods.
	// +optional
	Topology TopologyStatus `json:"topology,omitzero"`

	// config is the revision the engine runs.
	// +optional
	Config ConfigStatus `json:"config,omitzero"`

	// endpoints is where the company is reached.
	// +optional
	Endpoints CompanyEndpoints `json:"endpoints,omitzero"`

	// apiAccessSecretRef names the Secret, in the Company's namespace, that
	// holds a bearer token for the engine's API under the key token
	// (APIAccessTokenKey) and the API's in-cluster base URL under the key
	// url (APIAccessURLKey). It is for the company's consumers (a control
	// plane, a dashboard); the operator uses a token of its own, so revoking
	// this one never locks the operator out.
	//
	// The token holds Crewlet's admin grant, which takes in every app, so
	// whoever holds it can rewrite the company document where the operator
	// does not manage it, reveal every value in the engine's secret store
	// (the class's platform model keys included: it is a deployment's own
	// token, not a person's machine token, which Crewlet refuses a reveal),
	// and invite or create the company's people. Grant read on this Secret
	// only to whoever may do that, by resourceNames: read on every Secret of
	// the namespace also exposes <company>-api-tokens, <company>-keyring and
	// <company>-bootstrap.
	// +optional
	APIAccessSecretRef SecretReference `json:"apiAccessSecretRef,omitzero"`
}

// Company is one Crewlet company: the operator runs an engine for it and keeps
// the engine's company document equal to what the Company and the objects that
// name it declare.
//
// The Company's name becomes part of the names of the objects the operator
// creates for it (the data StatefulSet and the engine Service), so it must be
// a DNS label of at most 47 characters: a StatefulSet name longer than 52
// characters overflows its pods' controller-revision-hash label.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cmp,categories=crewlet
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.engineClassName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Pods",type=integer,JSONPath=`.status.topology.data.readyReplicas`
// +kubebuilder:printcolumn:name="Seats",type=integer,JSONPath=`.status.config.objects.seats`
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.status.config.revisionID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$') && size(self.metadata.name) <= 47",message="a Company's name must be a DNS label of at most 47 characters that starts with a letter, because it names the company's StatefulSet and Service"
type Company struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the company's desired state.
	// +required
	Spec CompanySpec `json:"spec,omitzero"`

	// status is the company's observed state.
	// +optional
	Status CompanyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CompanyList contains a list of Company.
type CompanyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Company `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Company{}, &CompanyList{})
		return nil
	})
}
