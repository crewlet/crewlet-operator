package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TrackerBackend is which work tracker a company runs. It maps onto Crewlet's
// tracker.backend (Worklet is `worklet`, Jira is `jira`, None is `none`).
// +kubebuilder:validation:Enum=Worklet;Jira;None
type TrackerBackend string

// The tracker backends.
const (
	// TrackerWorklet is Worklet, the engine's own tracker.
	TrackerWorklet TrackerBackend = "Worklet"
	// TrackerJira is Jira, connected by the company's Jira Integration.
	TrackerJira TrackerBackend = "Jira"
	// TrackerNone is a company with no tracker.
	TrackerNone TrackerBackend = "None"
)

// WorkletSettings is the policy of Worklet. It maps onto Crewlet's
// tracker.worklet.
// +kubebuilder:validation:MinProperties=1
type WorkletSettings struct {
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
// +kubebuilder:validation:XValidation:rule="!has(self.worklet) || self.backend == 'Worklet'",message="worklet applies to backend Worklet only"
// +kubebuilder:validation:MinProperties=1
type TrackerSettings struct {
	// backend is which tracker the company runs.
	// +optional
	// +default="Worklet"
	Backend TrackerBackend `json:"backend,omitempty"`

	// worklet is the policy of Worklet, for backend Worklet.
	// +optional
	Worklet WorkletSettings `json:"worklet,omitzero"`
}

// KnowledgeBackend is which knowledge base a company runs. It maps onto
// Crewlet's knowledge.backend (Doclet is `doclet`, Confluence is
// `confluence`, None is `none`).
// +kubebuilder:validation:Enum=Doclet;Confluence;None
type KnowledgeBackend string

// The knowledge backends.
const (
	// KnowledgeDoclet is Doclet, the engine's own knowledge base.
	KnowledgeDoclet KnowledgeBackend = "Doclet"
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
	// +default="Doclet"
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

// CompanyAssistant is whether the company runs the Dashboard Assistant, the
// models it runs on and the bounds of one turn. It maps onto Crewlet's
// `assistant` block, each field onto the field it names.
//
// The assistant answers a signed-in person's questions about the company and
// prepares changes for that person to approve, acting only as them: every
// read is a request of theirs, and nothing is written until they approve.
// Every provider in llm must drive a CLI Crewlet runs with no tool of its
// own (ClaudeCode or Pi; Codex keeps a shell its sandbox only confines, and
// OpenCode's has not been measured to go), with webTools Denied: the
// assistant runs it in the engine's pod, never as a seat's executor runs. Crewlet refuses any other in the dry run before every apply, and
// the refusal is reported on the Company, since the rule turns on the
// providers' own specs.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="!has(self.enabled) || !self.enabled || (has(self.llm) && size(self.llm) > 0)",message="an enabled assistant needs llm: name the model provider it runs on, one whose webTools is Denied",fieldPath=".llm"
type CompanyAssistant struct {
	// enabled runs the assistant. Unset or false leaves it off. Crewlet's
	// `enabled`.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// llm is the assistant's model chain, the first provider tried first,
	// referred to as a Seat's chain is. Required when enabled is true.
	// Crewlet's `llm`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="llm must name each provider once"
	LLM []LLMProviderReference `json:"llm,omitempty"`

	// maxSteps is the most tool steps one turn may take. Unset takes
	// Crewlet's default of 30. Crewlet's `max_steps`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=40
	MaxSteps int32 `json:"maxSteps,omitempty"`

	// turnTimeoutSeconds is the longest one turn may run. Unset takes
	// Crewlet's default of 900. Crewlet's `turn_timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:validation:Maximum=1200
	TurnTimeoutSeconds int32 `json:"turnTimeoutSeconds,omitempty"`

	// proposalTTLSeconds is how long a proposed change waits for its
	// owner's approval. Unset takes Crewlet's default of 900. Crewlet's
	// `proposal_ttl_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=3600
	ProposalTTLSeconds int32 `json:"proposalTTLSeconds,omitempty"`

	// retentionDays is how long a person's conversations are kept. Unset
	// takes Crewlet's default of 30. Crewlet's `retention_days`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=365
	RetentionDays int32 `json:"retentionDays,omitempty"`

	// tokenBudget caps the tokens each person's assistant may spend per
	// window, charged on top of the company's own spec.tokenBudget.
	// Crewlet's `token_budget`.
	// +optional
	TokenBudget TokenBudget `json:"tokenBudget,omitzero"`

	// deepWork lets the assistant hand a large task (research, scripts,
	// files, many steps) to a coding CLI running its own tools in a box of
	// its own, which needs spec.sandbox. Unset, the assistant answers in
	// quick mode only. Crewlet's `deep_work`.
	// +optional
	DeepWork CompanyAssistantDeepWork `json:"deepWork,omitzero"`
}

// CompanyAssistantDeepWork is the Dashboard Assistant's deep work: the model
// a large task is handed to and the bounds of one run. It maps onto Crewlet's
// `assistant.deep_work` block, each field onto the field it names.
//
// A deep-work run is Claude Code in its own harness, with its shell, its
// files and its web tools: the point of the escalation. Its one way into
// Crewlet is the signed-in person's own assistant tools over the tool
// bridge, so every read is still that person's request and every write still
// a proposal they approve. The provider llm names must drive ClaudeCode:
// Crewlet confines a deep-work run on the engine pod around that one CLI's
// install and temporary directories, which no other CLI has been measured
// against. Crewlet refuses any other in the dry run before every apply, and
// the refusal is reported on the Company, since the rule turns on the
// provider's own spec.
//
// Crewlet places a run in its provider's own runIn, else in the Company's
// E2B sandbox when spec.sandbox.e2b configures one, else on the data pod
// that opened the turn, in a box the engine confines with bubblewrap where
// the pod lets it start a namespace, else with the kernel's Landlock: kept
// from the engine's data, its configuration, its keyring and every other
// box. On a pod that enforces neither Crewlet starts no such run, and gives
// that as the reason. Crewlet makes that box beside its store, on the data
// volume (/var/lib/crewlet/store/assistant-boxes), not on the sandboxes
// volume: what the run writes there is bounded by no sandboxSizeLimit, and
// counts against the volume the store and the event stream live on, which a
// run that filled it would stop, and the company with them; a Company that
// will not take that risk places deep work in E2B (spec.sandbox.e2b, or its
// provider's runIn). A run reaches the bridge at the Company's public address
// wherever it is placed, so a Company with no public address runs no deep
// work. It is not placed by spec.sandbox.defaultRunIn, which places seats'
// executors: a seat's Direct box is not confined. deepWork requires
// spec.sandbox all the same, so that whether deep work runs in E2B or on
// the engine pod is a choice the Company states rather than one its absence
// makes.
type CompanyAssistantDeepWork struct {
	// llm is the provider deep work runs on: one driving ClaudeCode, an
	// LLMProvider or a platform model provider. Crewlet's `llm`.
	// +required
	LLM LLMProviderReference `json:"llm,omitzero"`

	// timeoutSeconds is the longest one deep-work run may take. Unset takes
	// Crewlet's default of 1200, which is also the most: a run lives inside
	// the turn's read grant. Crewlet's `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=1200
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// maxSteps is the most agentic rounds and Crewlet tool calls one
	// deep-work task may make. Unset takes Crewlet's default of 150.
	// Crewlet's `max_steps`.
	// +optional
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=400
	MaxSteps int32 `json:"maxSteps,omitempty"`

	// maxTokens is the most tokens one deep-work task may spend before it
	// is stopped. Unset takes Crewlet's default of 3000000. The person's
	// tokenBudget and the company's still apply. Crewlet's `max_tokens`.
	// +optional
	// +kubebuilder:validation:Minimum=10000
	// +kubebuilder:validation:Maximum=20000000
	MaxTokens int32 `json:"maxTokens,omitempty"`
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
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="model may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
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
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="baseURL may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="baseURL may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
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

// SandboxPlacement is where a seat's executor run happens: the cell of the
// box its coding CLI's own run is placed in. It maps onto Crewlet's run_in and
// default_run_in (Direct is `direct`, E2B is `e2b`). Crewlet's `container`
// cell is not offered: it starts a container per box, and an engine pod has
// no container runtime.
// +kubebuilder:validation:Enum=Direct;E2B
type SandboxPlacement string

// The places an executor run happens.
const (
	// SandboxPlacementDirect runs the CLI as a process tree of the engine
	// pod that holds the seat, a data pod or a seat pod, in a box of its own
	// on the pod's sandboxes volume, with its HOME, XDG and temporary
	// directories inside the box and an allowlisted environment. That
	// isolates the run's state, as Crewlet puts it, but not the host: the
	// CLI runs as the engine's user, with the pod's network, CPU and memory,
	// and reads what the engine reads, the keyring in its crewlet.yaml and
	// the store among it, and on a data pod the data volume. A box is seeded
	// with its provider's login, a credential bundle's included, and calls
	// the seat's tools back on the pod's own listener, on loopback, a data
	// pod's or a seat pod's, with or without a public address. A run dies
	// with its pod: a restart or a rolling update ends the runs in flight
	// on it, and Crewlet carries each run's round on in a fresh run, at
	// most twice a round.
	SandboxPlacementDirect SandboxPlacement = "Direct"
	// SandboxPlacementE2B runs the CLI in the Company's E2B sandbox, a
	// machine of its own per run, which calls the seat's tools back at the
	// Company's public address and is given only the variables that sign
	// its CLI in.
	SandboxPlacementE2B SandboxPlacement = "E2B"
)

// SandboxFile is a file written into a seat's executor box before its
// commands run. Its content is a credential position in Crewlet (it typically
// holds a git or registry login), so it always comes from a Secret.
type SandboxFile struct {
	// path is where the file is written in a seat's executor box, whose
	// rule turns on the box's cell. A relative path, such as .npmrc, and one
	// starting ~/ are joined onto the box's home in either cell, and that is
	// the shape that works in both.
	//
	// A Direct box is a directory Crewlet mints for each run on the engine
	// pod, and writes a setup file only inside its own home: an absolute
	// path, one that climbs out with .., and one naming the home itself are
	// refused, since no document can name a home minted per box.
	//
	// An E2B box's own agent writes the file as the account every template
	// runs as, under its home, /home/user, and creates any directory it
	// needs: a relative path is cleaned, so one that climbs with .. (../x)
	// lands outside the home, and an absolute path is written where it
	// names, which works only where that account may write (not under /root,
	// for one). It refuses a path that names a directory rather than a
	// file: the home and every directory above it (such as ~, . or /home)
	// in every template, and a directory only the template made, such as
	// /tmp, at setup, where nothing can tell beforehand.
	//
	// Either box refuses a path starting ~ and a name, such as ~root/.npmrc,
	// since neither expands another account's home. Crewlet refuses a
	// company document that gives a seat's box a path its cell refuses, in
	// every cell a seat's executor can launch in, and a path of white space
	// alone on every step. Crewlet's `files` key.
	//
	// The API refuses white space alone and another account's home itself.
	// The rest turns on the cell and on how .. and repeated separators
	// resolve, so it is left to Crewlet, whose refusal is reported on the
	// Company as ConfigApplied=False Invalid for a step of the Company's own,
	// and on the Seat as Applied=False Invalid for a step of a Seat's.
	// Either way the document is held and the running revision stays.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="path may not be white space alone: Crewlet refuses a setup file whose path names nothing"
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('~') || size(self) == 1 || self.substring(1, 2) in ['/', '\\\\']",message="path may not start with ~ followed by a name, such as ~root: a box does not expand another account's home, so write the file relative to its own, or under ~/"
	Path string `json:"path,omitempty"`

	// valueFrom is the Secret key holding the file's content.
	// +required
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// The optionalKeys rule of SandboxSetupStep compares every entry with every
// variable of env and every path of files, and the API server sums its
// estimated cost over every step a Company's or a Seat's spec.sandbox.setup
// may hold: 411,413 per step, 6,582,608 for the sixteen, two thirds of the
// 10,000,000 a rule may cost before the API server refuses the CRD. Raising
// setup's, env's, files' or optionalKeys' MaxItems, or a path's or an
// entry's MaxLength, can take it over, and the CRDs no longer install, which
// every test/crd case is the first to say.

// SandboxSetupStep is one provisioning step applied to a seat's executor box
// before its coding CLI starts. It maps onto a Crewlet sandbox setup step,
// which Crewlet refuses when it does nothing (no files, commands, env or
// brief): that is almost always an unfinished edit, which would otherwise
// apply cleanly for ever and look like it was working.
// +kubebuilder:validation:XValidation:rule="(has(self.files) && size(self.files) > 0) || (has(self.commands) && size(self.commands) > 0) || (has(self.env) && size(self.env) > 0) || has(self.brief)",message="a setup step must give files, commands, env or a brief: Crewlet refuses a step that does nothing"
// +kubebuilder:validation:XValidation:rule="!has(self.optionalKeys) || self.optionalKeys.all(k, (has(self.env) && self.env.exists(e, e.name == k)) || (has(self.files) && self.files.exists(f, f.path == k)))",message="optionalKeys may name only a variable of this step's env or a path of its files: an entry naming anything else leaves nothing out",fieldPath=".optionalKeys"
type SandboxSetupStep struct {
	// name identifies the step in logs and failure messages, so Crewlet
	// refuses one of white space alone, and so does the API.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="name may not be white space alone: Crewlet refuses a setup step with no name, since it is what a failure message points at"
	Name string `json:"name,omitempty"`

	// files are written into the box before the commands run. Crewlet's
	// `files`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=path
	// +kubebuilder:validation:MaxItems=16
	Files []SandboxFile `json:"files,omitempty"`

	// commands are shell commands run in order; one that exits non-zero
	// fails the box. Crewlet's `commands`. Crewlet resolves nothing in a
	// command: the box's shell expands $NAME and ${NAME} from the box's own
	// environment (its HOME and PATH among it) and the run's, never from the
	// engine's. The run's is the steps' env, the Seat's sandbox env, and
	// what Crewlet adds: the agent's handle and, when it has one, its email,
	// and the variables that sign the executor's CLI in to the provider it
	// runs on (docs/guides/credentials.md says which, and when the seat's
	// own env leaves them out). The API refuses ${NAME} (NAME an identifier)
	// all the same, as the operator refuses every ${NAME} it did not write
	// in a string it renders: write a box variable as $NAME. Parameter
	// expansions such as ${1:-x} are allowed.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=4096
	// +kubebuilder:validation:XValidation:rule="self.all(x, !x.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]'))",message="commands may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name; write a box variable as $NAME"
	Commands []string `json:"commands,omitempty"`

	// env is merged into the environment of the commands and of the run.
	// Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SecretEnvVar `json:"env,omitempty"`

	// optionalKeys names variables of env, and paths of files, whose value
	// may go missing. A seat's executor run starts without a value that does
	// not resolve, a Secret that does not exist, holds no such key or holds
	// an empty value, whether or not it is listed here: the run is the
	// seat's turn itself, and refusing it would fail every message the seat
	// could answer meanwhile. Unlisted, the engine logs each such value as a
	// gap of the run (sandbox_env_unresolved), and the operator reports an
	// empty one on the object that gives it; listed, the variable is left
	// out of the run and the file is not written, quietly, as an absence
	// that is expected. A missing Secret or key under one is then no failure
	// of the object's references and holds nothing back. Under the Enforce
	// policy the operator also takes the value it pushed for it out of the
	// engine's secret store, and has the engine read the store again whether
	// or not the document can be applied, so no run starts with a deleted
	// Secret's value, but in a revision the engine runs that reads the key as
	// required (one from before the key was listed) or whose reading the
	// operator does not know (another document somebody else activated),
	// which keeps the value until the next document runs, and that one is
	// activated once with it and at once again without it; a Secret key that
	// is also read where it is required holds the document while it is
	// missing, and keeps the value it had in a revision the engine runs that
	// requires it too until the document applies. A later step's variable of
	// the same name wins over this one's, and the Seat's spec.sandbox.env
	// over every step's, and the list beside the value that wins is the one
	// that counts for it; a file is the step's own. Crewlet's
	// `optional_keys`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=48
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	OptionalKeys []string `json:"optionalKeys,omitempty"`

	// brief tells the executor's CLI what this step made true about its
	// box.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Brief string `json:"brief,omitempty"`

	// timeoutSeconds caps each command of the step.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// E2BSandbox is the remote sandbox service a seat's executor run placed in
// E2B happens in. Each field maps onto the same-named field of Crewlet's
// providers.sandbox.e2b.
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
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="domain may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Domain string `json:"domain,omitempty"`

	// template is the sandbox template, which also fixes each sandbox's CPU,
	// memory and disk. Unset, each run gets the vendor's prebuilt template
	// for its CLI (claude or opencode); a template set here is used for every
	// run, so it must carry the CLI of every ClaudeCode or OpenCode provider
	// whose executor runs are placed in E2B. For OpenCode that is
	// OpenCode 2, the @opencode/cli package (opencode --version prints
	// opencode v2.…): Crewlet's OpenCode runner ends a run at once in a box
	// with OpenCode 1.x (npm's opencode-ai), and E2B does not say which
	// release its prebuilt opencode template carries, so a company whose
	// OpenCode runs end that way sets template to one built with
	// @opencode/cli.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="template may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Template string `json:"template,omitempty"`
}

// SandboxSettings is where a company's agent seats' executors run. It maps
// onto Crewlet's providers.sandbox.
//
// Every agent seat's executor is its coding CLI's own run in a box, placed
// by the provider it launches on (its runIn) or by defaultRunIn. Crewlet
// refuses a company with an agent seat and a model provider that configures
// none, which the operator reports on the Company (SandboxRequired). Direct
// needs nothing more: it runs on the engine pods, which keep its boxes on
// their sandboxes volume. E2B needs e2b. Crewlet's `container` cell is not
// offered, as an engine pod has no container runtime.
// +kubebuilder:validation:XValidation:rule="self.defaultRunIn != 'E2B' || has(self.e2b)",message="defaultRunIn E2B places every executor run whose provider names no runIn in the Company's E2B sandbox: set e2b, or run them Direct",fieldPath=".defaultRunIn"
type SandboxSettings struct {
	// defaultRunIn is where a seat's executor run happens when the provider
	// it launches on names no runIn: Direct, on the engine pod that holds
	// the seat, or E2B, in the Company's E2B sandbox. Crewlet's
	// `default_run_in`.
	// +required
	DefaultRunIn SandboxPlacement `json:"defaultRunIn,omitempty"`

	// e2b is the remote sandbox service, required where a run is placed in
	// E2B: by defaultRunIn, or by the runIn of a provider a seat's executor
	// can launch on (SandboxPlacementNotOffered otherwise).
	// +optional
	E2B E2BSandbox `json:"e2b,omitzero"`

	// defaultTimeoutSeconds is how long a box outlives an engine that
	// stopped renewing it, in either cell. It never stops a running job:
	// the engine renews a box while its run works. An engine pod gone for
	// this long has the Direct runs it launched, which were its processes
	// and died with it, found lost (launch_node_gone), and the pod that holds
	// each run's seat next carries its round on in a fresh run. Unset takes
	// Crewlet's default of 900.
	// Crewlet's `default_timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	DefaultTimeoutSeconds int32 `json:"defaultTimeoutSeconds,omitempty"`

	// defaultPauseTTLSeconds is how long a box waiting on a person's
	// answer stays paused before it is reclaimed. Unset takes Crewlet's
	// default of 1800; 0 never pauses. Crewlet's
	// `default_pause_ttl_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DefaultPauseTTLSeconds *int32 `json:"defaultPauseTTLSeconds,omitempty"`

	// defaultMaxTurns caps the agentic rounds of one executor run on seats
	// that name none. It is the only bound on a seat's turn: an executor run
	// is never stopped on a clock. Unset takes Crewlet's default of 96; 0 is
	// uncapped. Crewlet's `default_max_turns`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DefaultMaxTurns *int32 `json:"defaultMaxTurns,omitempty"`

	// setup is applied to every executor box before each seat's own steps.
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

	// maxConcurrentTurns is how many turn segments one of the pool's pods
	// runs at once in the engine (a seat's onboarding pass, the launch of its
	// executor, the collection of its run and the review): Crewlet's
	// node.max_concurrent. An executor run is not one: its turn gives the
	// place back while the run works, and a seat runs one at a time, so the
	// seats a pod holds bound the Direct runs it has running, beside those
	// parked on a person's question, whose processes are stopped. Unset
	// takes the class's seatPools.defaults.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentTurns int32 `json:"maxConcurrentTurns,omitempty"`

	// terminationGracePeriodSeconds is how long one of the pool's pods may
	// take to stop, finishing the turn segments the engine runs in its own
	// process (a seat's launch, the collection of its run, the review, a
	// delegated worker): the engine puts no bound of its own on that drain,
	// so it must cover the longest of them. A seat's executor run is not
	// waited for: a Direct one on the pod ends with it, and its box with the
	// pod's sandboxes volume, and Crewlet carries its round on in a fresh
	// run, once a pod of the same name is back or once the pod has been gone
	// for spec.sandbox.defaultTimeoutSeconds (launch_node_gone); an E2B one
	// carries on in its sandbox. Unset takes the class's seatPools.defaults,
	// else 300.
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
	// publicBaseURL is the HTTPS address vendors deliver webhooks to and
	// coding runs call the engine back on, overriding the one the class's
	// webhooks.hostname derives. A seat's executor run in E2B calls the
	// engine's tool bridge there, and exports its traces there while the
	// class sends telemetry, and the Dashboard Assistant's deep work calls
	// the bridge there wherever it runs; a seat's Direct run, on a data pod
	// or a seat pod, dials its own pod's listener on loopback for both.
	// Required
	// when the class's webhooks.mode is External, where something outside
	// the operator (a relay, an API gateway) forwards it to the engine's
	// public listener. A trailing '/' is dropped, as Crewlet drops it.
	// Crewlet's api.public.external_url, in the data pod's crewlet.yaml, and
	// its CREWLET_MCP_BRIDGE_URL on every engine pod, with its
	// CREWLET_SANDBOX_OTEL_RECEIVER_URL while the class sends telemetry,
	// which the engine reads only at start: changing it restarts the data
	// pod and the seat pods.
	// Its host is a name, an IPv4
	// address or an IPv6 address in brackets, followed by at most one port,
	// 1 to 65535: Crewlet refuses to start on any other, but for an IPv6
	// address whose zone holds a ']' (https://[fe80::1%25eth0]]/), which it
	// starts on and no vendor reaches. The API judges the host and the port
	// itself rather than leaving them to the API server's URL parser, which
	// is Go's net/url as the API server was built and is run: it takes such
	// a zone, and under Go's GODEBUG setting urlstrictcolons=0
	// https://h.example:80:80 too.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && self.startsWith('https://') && url(self).getHostname() != ''",message="publicBaseURL must be an absolute URL starting with https://"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^https?://[^/]*:(0+|0*([1-9][0-9]{5,}|[7-9][0-9]{4}|6[6-9][0-9]{3}|65[6-9][0-9]{2}|655[4-9][0-9]|6553[6-9]))(/|$)')",message="publicBaseURL's port must be 1 to 65535: Crewlet refuses to start on any other; leave the port out for https's own, 443"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://') || self.matches('^[^:/?#]+://([^/?#]*@)?([[](([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){1,7}:|:(:[0-9A-Fa-f]{1,4}){1,7}|::|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){1,6}|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){1,5}|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){1,4}|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){1,3}|([0-9A-Fa-f]{1,4}:){5}(:[0-9A-Fa-f]{1,4}){1,2}|([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4})|(([0-9A-Fa-f]{1,4}:){6}|:(:[0-9A-Fa-f]{1,4}){0,5}:|([0-9A-Fa-f]{1,4}:){5}:|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){0,4}:|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){0,3}:|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){0,2}:|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){0,1}:)(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])([.](25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3})(%25[^]/?#]+)?[]]|[^/?#:[]*)(:[0-9]*)?([/?#]|$)')",message="publicBaseURL's host must be a name, an IPv4 address or an IPv6 address in brackets, and at most one ':' and a port may follow it: Crewlet refuses to start on any other address, or starts on one no vendor reaches (write https://[2001:db8::1]:8443, not https://2001:db8::1:8443)"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="publicBaseURL may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?') && !self.contains('#')",message="publicBaseURL may not have a query or a fragment: Crewlet builds every webhook and sandbox address by appending a path to it"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="publicBaseURL may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
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
	//
	// Crewlet compares the origin of a person's write with this address as a
	// browser spells it, and refuses to start on an address whose host a
	// browser rewrites before it sends it, so the API refuses it too: a host
	// outside ASCII, as it is or percent-encoded (write its xn-- form,
	// https://xn--bcher-kva.example for https://bücher.example), a host
	// whose last label is a number and that is not an IPv4 address of four
	// decimal octets (127.1, 2130706433, 0x7f.0.0.1, 192.168.010.1,
	// crewlet.123: write 127.0.0.1, or a name that ends in a word), and an
	// IPv6 address with a zone ([fe80::1%25eth0]). Its host is a name, an
	// IPv4 address or an IPv6 address in brackets, followed by at most one
	// port, 1 to 65535 (http://[::1]:8080, not http://::1:8080), which the
	// API judges itself, as for publicBaseURL.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://')) && url(self).getHostname() != ''",message="dashboardBaseURL must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^https?://[^/]*:(0+|0*([1-9][0-9]{5,}|[7-9][0-9]{4}|6[6-9][0-9]{3}|65[6-9][0-9]{2}|655[4-9][0-9]|6553[6-9]))(/|$)')",message="dashboardBaseURL's port must be 1 to 65535: Crewlet refuses to start on any other; leave the port out for the scheme's own"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://') || self.matches('^[^:/?#]+://([^/?#]*@)?([[](([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){1,7}:|:(:[0-9A-Fa-f]{1,4}){1,7}|::|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){1,6}|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){1,5}|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){1,4}|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){1,3}|([0-9A-Fa-f]{1,4}:){5}(:[0-9A-Fa-f]{1,4}){1,2}|([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4})|(([0-9A-Fa-f]{1,4}:){6}|:(:[0-9A-Fa-f]{1,4}){0,5}:|([0-9A-Fa-f]{1,4}:){5}:|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){0,4}:|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){0,3}:|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){0,2}:|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){0,1}:)(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])([.](25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3})(%25[^]/?#]+)?[]]|[^/?#:[]*)(:[0-9]*)?([/?#]|$)')",message="dashboardBaseURL's host must be a name, an IPv4 address or an IPv6 address in brackets, and at most one ':' and a port may follow it: Crewlet refuses to start on any other address (write http://[::1]:8080, not http://::1:8080)"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^https?://[^/]*[^[:ascii:]]')",message="dashboardBaseURL's host must be ASCII: a browser sends a host outside ASCII in its xn-- form, which Crewlet cannot compare it with, so write that form (https://xn--bcher-kva.example for https://bücher.example)"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^https?://[^/]*%')",message="dashboardBaseURL's host may not hold a '%': Crewlet cannot compare a browser's origin with a percent-encoded host, nor can a browser reach an IPv6 zone (%25eth0), so write the host as a browser sends it, in ASCII (its xn-- form) and without a zone"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^https?://([^/:]*[.])?([0-9]+|0[xX][0-9a-fA-F]*)[.]?(:[0-9]*)?(/|$)') || self.matches('^https?://((25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])[.]){3}(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(:[0-9]*)?(/|$)')",message="dashboardBaseURL's host ends in a number, which a browser reads as an IPv4 address and rewrites before it sends it: write an IPv4 address as four decimal octets, such as 127.0.0.1, and a host name whose last label is not a number"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="dashboardBaseURL may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?') && !self.contains('#')",message="dashboardBaseURL may not have a query or a fragment: Crewlet builds every link a person follows, an invitation and a password link included, by appending a path to it"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="dashboardBaseURL may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	DashboardBaseURL string `json:"dashboardBaseURL,omitempty"`

	// artifactsBaseURL is where a browser reaches the artifact origin,
	// ahead of the class's artifacts route: a tunnel, or http://localhost:8082
	// for a browser that port-forwards it. Crewlet's api.artifacts.external_url;
	// changing it restarts the data pod. https, or http on localhost or
	// 127.x.x.x; {artifact} at the start of the host gives each artifact a host
	// of its own. It may not be the dashboard's or the public listener's
	// origin, nor a parent of either's host (RuntimeReady=False AddressRefused).
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="self.matches('^https?://([{]artifact[}][a-z0-9-]*[.])?[a-z0-9]([-a-z0-9]*[a-z0-9])?([.][a-z0-9]([-a-z0-9]*[a-z0-9])?)*(:[0-9]+)?/?$')",message="artifactsBaseURL must be a scheme and a lowercase host name, with at most a port and a '/' after it, and {artifact} only at the start of its first label, such as https://{artifact}.artifacts.example.net or https://artifacts.example.com: Crewlet serves artifacts from the root of their host"
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('http://') || self.matches('^http://([^/:]*[.])?localhost(:[0-9]+)?/?$') || self.matches('^http://127([.][0-9]{1,3}){3}(:[0-9]+)?/?$')",message="artifactsBaseURL may use plain http only on localhost, a name under it or a 127.x.x.x address: Crewlet refuses an artifact origin reached over plain http on a routable host, since every byte a page loads would travel in the clear"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^https?://[^/]*:(0+|0*([1-9][0-9]{5,}|[7-9][0-9]{4}|6[6-9][0-9]{3}|65[6-9][0-9]{2}|655[4-9][0-9]|6553[6-9]))(/|$)')",message="artifactsBaseURL's port must be 1 to 65535: Crewlet refuses to start on any other; leave the port out for the scheme's own"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^https?://[{]artifact[}][a-z0-9-]{38,}[.]')",message="artifactsBaseURL may follow {artifact} with at most 37 characters of its label: every artifact's label is 26 characters and that suffix, and a DNS label holds 63"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^https?://([^/:]*[.])?([0-9]+|0x[0-9a-f]*)(:[0-9]+)?/?$') || self.matches('^https?://((25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])[.]){3}(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(:[0-9]+)?/?$')",message="artifactsBaseURL's host ends in a number, which a browser reads as an IPv4 address and rewrites before it sends it: write an IPv4 address as four decimal octets, such as 127.0.0.1, and a host name whose last label is not a number"
	ArtifactsBaseURL string `json:"artifactsBaseURL,omitempty"`
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
	// class's variable of the same name. They are the engine's environment,
	// not that of the processes it starts: a Stdio tool server is handed only
	// the part Crewlet's allowlist names, such as PATH and the TLS and proxy
	// variables, a coding CLI that part and the variables its Crewlet profile
	// passes through, such as GeminiCLI's GOOGLE_CLOUD_PROJECT and
	// GOOGLE_CLOUD_LOCATION, a seat's Direct executor run that part alone
	// (the allowlist: PATH, the locale, TERM, TZ, the TLS trust and proxy
	// variables), and an E2B run none of it. So a variable a tool server
	// reads goes in its MCPServer's env (docs/guides/tool-servers.md), one a
	// coding CLI reads in the engine's pod in its LLMProvider's spec.env,
	// and one a seat's executor run reads in the Seat's spec.sandbox.env.
	//
	// Names starting with CREWLET_ or OTEL_ are the operator's own.
	// ANTHROPIC_API_KEY and OPENAI_API_KEY are refused too: Crewlet reads them
	// as the key of a model provider in auth mode api-key, or an embeddings
	// provider, that names none, so a value here would be a credential outside
	// the engine's secret store; give the provider its key with
	// auth.apiKeySecretRef or apiKeySecretRef.
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
	// tenant runs in the pod (a stdio MCP server, a coding CLI) can read
	// them. Unset uses the pod's own credentials through the SDK's
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
// +kubebuilder:validation:XValidation:rule="!has(self.assistant) || !has(self.assistant.deepWork) || has(self.sandbox)",message="assistant.deepWork needs spec.sandbox: a deep-work run has its own shell, files and web tools, and runs in its provider's runIn, else in the Company's E2B sandbox when spec.sandbox.e2b configures one, else confined on the engine pod, so the Company states which",fieldPath=".assistant.deepWork"
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
	// carries it. Crewlet's `mission`, held to its bounds: at most 1024
	// characters and one line, both judged with the space around it trimmed
	// (CEL's trim() is Go's strings.TrimSpace, which Crewlet trims with, and
	// size() counts characters as Crewlet does).
	//
	// Judged only when it CHANGES, as Crewlet judges a charter: a Company
	// written before these bounds keeps applying, and is refused only for a
	// mission it edits and leaves at fault. maxLength is the bound this field
	// had before them, kept so such a Company stays valid; it also bounds the
	// rules' cost.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.trim().size() <= 1024",message="mission can be at most 1024 characters: every seat reads it on every turn",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="oldSelf.trim() == self.trim() || self.trim().size() <= 1024",message="mission can be at most 1024 characters: every seat reads it on every turn"
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || !self.trim().matches('[\\n\\r\\v\\f\\u0085\\u2028\\u2029]')",message="mission is one line: every seat reads it inline",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="oldSelf.trim() == self.trim() || !self.trim().matches('[\\n\\r\\v\\f\\u0085\\u2028\\u2029]')",message="mission is one line: every seat reads it inline"
	Mission string `json:"mission,omitempty"`

	// vision is a longer statement of where the company is going, in
	// paragraphs if it needs them. Crewlet's `vision`, held to its bound: at
	// most 2048 characters with the space around it trimmed, judged only when
	// it changes, and with the bound it had before as maxLength, as on
	// mission.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.trim().size() <= 2048",message="vision can be at most 2048 characters: every seat reads it on every turn",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="oldSelf.trim() == self.trim() || self.trim().size() <= 2048",message="vision can be at most 2048 characters: every seat reads it on every turn"
	Vision string `json:"vision,omitempty"`

	// policies are company-wide rules every seat's prompt carries, one item
	// of a list each. Crewlet's `policies`, held to its bounds: at most 25,
	// each with the space around it trimmed not blank, at most 1024
	// characters and one line, and none a repeat of another.
	//
	// Judged on what a change makes, as Crewlet judges it: a policy the
	// Company already held is not judged again, so reordering and removing
	// policies are never refused; the count only when it passes 25 and grows;
	// a repeat only when the change adds a copy. One difference is forced by
	// the API server's budget for a rule's cost: this API recognises a policy
	// it already held, and a repeat, by its exact text, where Crewlet compares
	// them trimmed. Two policies differing only in the space around them are
	// admitted here and refused by Crewlet's write, which the Company reports
	// as Applied=False. maxItems and items' maxLength are the bounds this
	// field had before, as on mission.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.size() <= 25",message="a company can keep at most 25 policies: every seat reads all of them on every turn",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="self.size() <= 25 || self.size() <= oldSelf.size()",message="a company can keep at most 25 policies: every seat reads all of them on every turn"
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.all(p, p.trim().size() > 0)",message="a policy cannot be blank",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="self.all(p, p.trim().size() > 0 || p in oldSelf)",message="a policy cannot be blank"
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.all(p, p.trim().size() <= 1024)",message="a policy can be at most 1024 characters: every seat reads it on every turn",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="self.all(p, p.trim().size() <= 1024 || p in oldSelf)",message="a policy can be at most 1024 characters: every seat reads it on every turn"
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.all(p, !p.trim().matches('[\\n\\r\\v\\f\\u0085\\u2028\\u2029]'))",message="a policy is one line: every seat reads each as one item of a list",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="self.all(p, !p.trim().matches('[\\n\\r\\v\\f\\u0085\\u2028\\u2029]') || p in oldSelf)",message="a policy is one line: every seat reads each as one item of a list"
	// +kubebuilder:validation:XValidation:rule="oldSelf.hasValue() || self.all(p, self.filter(q, q == p).size() == 1)",message="policies must not repeat one another",optionalOldSelf=true
	// +kubebuilder:validation:XValidation:rule="self.all(p, self.filter(q, q == p).size() <= (p in oldSelf ? oldSelf.filter(q, q == p).size() : 1))",message="policies must not repeat one another"
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
	// Each window binds every agent seat, so Crewlet refuses one below
	// turn_engine.executor_min_budget_tokens (25000 unless spec.engineConfig
	// sets turn_engine): an executor launch needs that much of every window
	// left, and a seat under a smaller ceiling would never launch one. The
	// refusal comes from the engine's dry run, Invalid on the Company.
	// Crewlet's `token_budget`.
	// +optional
	TokenBudget TokenBudget `json:"tokenBudget,omitzero"`

	// llm is how the company chooses among its model providers.
	// +optional
	LLM CompanyLLMSettings `json:"llm,omitzero"`

	// assistant is the company's Dashboard Assistant. Unset leaves it off.
	// Crewlet's `assistant`.
	// +optional
	Assistant CompanyAssistant `json:"assistant,omitzero"`

	// embeddings is the provider vectors are computed with. Unset leaves
	// search keyword-only. Crewlet's providers.embeddings.
	// +optional
	Embeddings EmbeddingSettings `json:"embeddings,omitzero"`

	// sandbox is where the company's agent seats' executors run. Required
	// of a company with an Agent seat and a model provider, which Crewlet
	// refuses without it (SandboxRequired). Crewlet's providers.sandbox.
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
	// assistant, providers, integrations, mcp_servers, roles or units. Every
	// position Crewlet holds a credential in lives under one of those keys, so
	// engineConfig can never carry a credential: the API server refuses a
	// Company whose engineConfig names one of them, before anything is
	// stored.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:rule="self.all(e, !(e.name in ['name', 'mission', 'vision', 'policies', 'timezone', 'tracker', 'knowledge', 'token_budget', 'assistant', 'providers', 'integrations', 'mcp_servers', 'roles', 'units']))",message="engineConfig may not set name, mission, vision, policies, timezone, tracker, knowledge, token_budget, assistant, providers, integrations, mcp_servers, roles or units: those are fields and objects of this API, among them every block that holds a credential"
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

	// artifactsBaseURL is where a browser reaches the artifact origin:
	// spec.exposure.artifactsBaseURL, else the class's artifacts route while it
	// serves https. Unset, the data pod opens no artifact listener.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	ArtifactsBaseURL string `json:"artifactsBaseURL,omitempty"`
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
