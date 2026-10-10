package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// LLMProviderType is which model API a provider speaks. It maps onto the
// `type` of a Crewlet providers.llm entry: Anthropic is `anthropic`, OpenAI is
// `openai`, OpenAICompatible is `openai-compatible` and CLIAgent is
// `cli-agent`.
// +kubebuilder:validation:Enum=Anthropic;OpenAI;OpenAICompatible;CLIAgent
type LLMProviderType string

// ClaudeModelID is one id of Crewlet's table of Claude models, which records
// the request shape each model accepts (its thinking mode, its effort levels,
// whether it takes a sampling parameter, its output cap). An Anthropic
// provider whose model is a gateway alias names the row it is an alias of.
// Crewlet refuses on every write a claude_model its table does not list,
// exactly as written, so the values are the table's ids at the engine the
// operator is built against; a test holds them to that engine's refusal.
// +kubebuilder:validation:Enum=claude-fable-5-1;claude-mythos-5-1;claude-fable-5;claude-mythos-5;claude-opus-5-5;claude-opus-5;claude-opus-4-8;claude-opus-4-7;claude-sonnet-5-5;claude-sonnet-5;claude-opus-4-6;claude-sonnet-4-6;claude-opus-4-5;claude-sonnet-4-5;claude-haiku-4-5;claude-opus-4-1;claude-opus-4-0;claude-opus-4;claude-sonnet-4-0;claude-sonnet-4;claude-3-7-sonnet
type ClaudeModelID string

// The LLM provider types.
const (
	// LLMProviderAnthropic is Anthropic's Messages API.
	LLMProviderAnthropic LLMProviderType = "Anthropic"
	// LLMProviderOpenAI is OpenAI's API.
	LLMProviderOpenAI LLMProviderType = "OpenAI"
	// LLMProviderOpenAICompatible is any endpoint that speaks the OpenAI wire
	// format: a gateway, an aggregator, a self-hosted inference server.
	LLMProviderOpenAICompatible LLMProviderType = "OpenAICompatible"
	// LLMProviderCLIAgent drives a coding CLI (Claude Code, Codex, ...) on a
	// subscription login, installed in the engine's image: see
	// LLMProviderSpec.cli.
	LLMProviderCLIAgent LLMProviderType = "CLIAgent"
)

// CLIAgentName is which coding CLI a CLIAgent provider drives. It maps onto
// Crewlet's cli.agent (ClaudeCode is `claude-code`, Codex is `codex`,
// GeminiCLI is `gemini-cli`, QwenCode is `qwen-code`, OpenCode is `opencode`,
// CursorAgent is `cursor-agent`, Copilot is `copilot`, Grok is `grok`,
// MuseCode is `muse-code`, KimiCode is `kimi-code`, Hermes is `hermes`, Pi is
// `pi`). Crewlet's `custom`, a CLI described wholly by profile overrides, is
// not offered: the overrides name the binary and the arguments the engine's
// pod runs.
// +kubebuilder:validation:Enum=ClaudeCode;Codex;GeminiCLI;QwenCode;OpenCode;CursorAgent;Copilot;Grok;MuseCode;KimiCode;Hermes;Pi
type CLIAgentName string

// The coding CLIs.
const (
	CLIAgentClaudeCode  CLIAgentName = "ClaudeCode"
	CLIAgentCodex       CLIAgentName = "Codex"
	CLIAgentGeminiCLI   CLIAgentName = "GeminiCLI"
	CLIAgentQwenCode    CLIAgentName = "QwenCode"
	CLIAgentOpenCode    CLIAgentName = "OpenCode"
	CLIAgentCursorAgent CLIAgentName = "CursorAgent"
	CLIAgentCopilot     CLIAgentName = "Copilot"
	CLIAgentGrok        CLIAgentName = "Grok"
	CLIAgentMuseCode    CLIAgentName = "MuseCode"
	CLIAgentKimiCode    CLIAgentName = "KimiCode"
	CLIAgentHermes      CLIAgentName = "Hermes"
	CLIAgentPi          CLIAgentName = "Pi"
)

// CLIAgentMode is how the engine uses the CLI. It maps onto Crewlet's
// cli.mode (Text is `text`, Agent is `agent`).
// +kubebuilder:validation:Enum=Text;Agent
type CLIAgentMode string

// The CLI agent modes.
const (
	// CLIAgentModeText drives the CLI as a text model behind the engine's
	// own tool loop, as a process in the engine's pod.
	CLIAgentModeText CLIAgentMode = "Text"
	// CLIAgentModeAgent runs a seat's executor as the CLI's own agentic
	// run, in a sandbox, with the seat's tools reached back through the
	// engine's tool bridge. It is offered for the CLIs Crewlet has a
	// sandbox runner for, ClaudeCode and OpenCode.
	CLIAgentModeAgent CLIAgentMode = "Agent"
)

// CLIAgentRunIn is where an Agent-mode run happens. It maps onto Crewlet's
// cli.run_in (E2B is `e2b`). Crewlet's local cells, `direct` and `container`,
// are not offered, as the Company's spec.sandbox does not offer them.
// +kubebuilder:validation:Enum=E2B
type CLIAgentRunIn string

// The places an Agent-mode run happens.
const (
	// CLIAgentRunInE2B is the Company's E2B sandbox.
	CLIAgentRunInE2B CLIAgentRunIn = "E2B"
)

// CLIAgentAuthMode is how a CLIAgent provider authenticates. It maps onto
// Crewlet's cli.auth.mode (Subscription is `subscription`, APIKey is
// `api-key`).
//
// Crewlet's `inherit-env`, which hands the CLI whichever token or key the
// engine's own environment holds, is not offered: a credential in the
// engine's environment is outside the encrypted store, readable by every
// process the engine runs, and resolvable by a ${NAME} written in any field of
// the company document, so it would be the one credential the API takes by
// another route than a Secret reference. Subscription and APIKey cover what
// it is for.
// +kubebuilder:validation:Enum=Subscription;APIKey
type CLIAgentAuthMode string

// The CLI agent authentication modes.
const (
	// CLIAgentAuthSubscription signs the CLI in to a subscription with
	// tokenSecretRef or credentialBundleSecretRef, or signs Hermes, Pi or
	// OpenCode in with its model provider's key in the cli block's env.
	CLIAgentAuthSubscription CLIAgentAuthMode = "Subscription"
	// CLIAgentAuthAPIKey gives the CLI the provider's apiKeySecretRefs
	// key, in the key variable Crewlet's profile for it names, billing the
	// metered account. CursorAgent's key is a Cursor account's API key
	// (CURSOR_API_KEY), not a model provider's.
	CLIAgentAuthAPIKey CLIAgentAuthMode = "APIKey"
)

// CLIAgentAuth is how a CLIAgent provider signs in. It maps onto Crewlet's
// cli.auth.
//
// A subscription login is made at a terminal, by `crewlet llm login` or the
// CLI's own command, and a pod has no terminal: Subscription signs in with a
// headless token, such as the CLAUDE_CODE_OAUTH_TOKEN `claude setup-token`
// mints, or with a credential bundle `crewlet llm export` wrote from a login
// made elsewhere. A bundle is restored only while the provider's login on the
// data volume is empty, so the refreshed login a running engine keeps there
// wins over the bundle; a token never needs refreshing, which makes it the
// better of the two wherever the CLI mints one. A provider names one of the
// two: given both, which login the CLI acts on would be the CLI's choice
// between its environment and its own files, which nothing here can see.
//
// Crewlet hands a CLI a token or a key only through the variable its profile
// names, and refuses one for a CLI whose profile names none, since it would
// reach nothing: so CLIAgentSettings holds tokenSecretRef to the CLIs with a
// token variable and mode APIKey to the CLIs with a key variable.
//
// Hermes, Pi and OpenCode may sign in to a Subscription with neither: each
// fronts many model providers and reads each provider's key from that
// provider's own variable, so Crewlet's profile for it names no key variable
// and marks it as signing in through its environment (env_sign_in), and
// Crewlet documents the provider's key in the cli block's env as its metered
// sign-in, and hands that env to the CLI untouched. Crewlet counts an entry of
// env as a sign-in only when its name says it holds a credential: one of
// its parts between underscores is, in any case, KEY, KEYS, APIKEY,
// ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
// CREDENTIALS, AUTH, OAUTH or PAT (cliprofile.IsCredentialName). So
// OPENROUTER_API_KEY signs such a CLI in and HTTPS_PROXY or
// HERMES_MAX_TOKENS does not, and one whose env holds no such name is
// signed in to nothing, and refused. Every other CLI keeps to a token or a
// bundle: one with a key variable takes its key through mode APIKey, and
// Crewlet refuses that variable in env, which auth owns. The rule is
// CLIAgentSettings's, which sees the agent and env beside auth.
// +kubebuilder:validation:XValidation:rule="!(has(self.mode) && self.mode != 'Subscription') || (!has(self.tokenSecretRef) && !has(self.credentialBundleSecretRef))",message="tokenSecretRef and credentialBundleSecretRef sign in to a subscription: they apply to mode Subscription only"
// +kubebuilder:validation:XValidation:rule="!has(self.tokenSecretRef) || !has(self.credentialBundleSecretRef)",message="tokenSecretRef and credentialBundleSecretRef are two logins, and the CLI is given one: keep the headless token, which never needs refreshing, or the bundle where the CLI mints no token"
// +kubebuilder:validation:MinProperties=1
type CLIAgentAuth struct {
	// mode is how the CLI signs in. Unset is Subscription. Crewlet's
	// `mode`.
	// +optional
	Mode CLIAgentAuthMode `json:"mode,omitempty"`

	// tokenSecretRef is the Secret key holding a long-lived subscription
	// token the CLI mints for headless use. Crewlet's `token`, a credential
	// position.
	// +optional
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef,omitzero"`

	// credentialBundleSecretRef is the Secret key holding a credential
	// bundle `crewlet llm export` wrote. Crewlet's `credential_bundle`, a
	// credential position.
	// +optional
	CredentialBundleSecretRef SecretKeyReference `json:"credentialBundleSecretRef,omitzero"`
}

// CLIAgentSettings is the coding CLI a CLIAgent provider drives. It maps onto
// Crewlet's providers.llm entry's cli block.
//
// The CLI is a binary in the engine's image, which the official image does
// not ship: a class running such a provider names an image derived from the
// engine's with the CLI installed (docs/guides/cli-agents.md). Its login, and
// the per-seat homes the CLI keeps, are on the data volume, so a restart or a
// new pod keeps them.
//
// Its rules hold every credential to a variable Crewlet's profile for the
// CLI reads, as Crewlet's own admission does (providers.llm.*.cli): the
// engine refuses a write that names a credential its CLI would never be
// given, so the API refuses it on the object instead. The lists of CLIs the
// rules name are those whose profiles set token_env, api_key_env or
// env_sign_in at the engine version the operator is built against; a test
// holds them to that engine's own verdict.
//
// Mode Agent runs the CLIs Crewlet has a sandbox runner for, ClaudeCode and
// OpenCode: the Company's coding agents. Crewlet hands an Agent-mode run its
// provider's model exactly as the provider's text calls pass it (the
// reviewer's, a delegated worker's, the summariser's), so one model serves
// every phase. The run happens in the Company's E2B sandbox, which never
// receives a credential bundle: the bundle's login carries a refresh token
// the engine keeps rotating on its data volume. So a Subscription login
// reaches the run only as a variable of its environment, which for ClaudeCode
// is its headless token and for OpenCode its model provider's key in env.
// +kubebuilder:validation:XValidation:rule="!has(self.runIn) || (has(self.mode) && self.mode == 'Agent')",message="runIn applies to mode Agent only: in mode Text the CLI is a process in the engine's pod"
// +kubebuilder:validation:XValidation:rule="!has(self.mode) || self.mode != 'Agent' || self.agent in ['ClaudeCode', 'OpenCode']",message="mode Agent runs a CLI Crewlet has a sandbox runner for: ClaudeCode or OpenCode"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || (has(self.auth.mode) && self.auth.mode != 'Subscription') || has(self.auth.tokenSecretRef) || has(self.auth.credentialBundleSecretRef) || (self.agent in ['Hermes', 'Pi', 'OpenCode'] && has(self.env) && self.env.exists(e, e.name.matches('(?i)(^|_)(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)(_|$)')))",message="a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef: a pod has no terminal to log in at. Hermes, Pi and OpenCode alone may sign in with their model provider's key in env instead, under a name Crewlet reads as a credential, one of whose parts between underscores is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT (such as OPENROUTER_API_KEY), which Crewlet hands them untouched. A CLI with a key variable takes its key through auth mode APIKey, as Crewlet refuses that variable in env",fieldPath=".auth"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.tokenSecretRef) || self.agent == 'ClaudeCode'",message="auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode (CLAUDE_CODE_OAUTH_TOKEN): its profile for every other CLI names no token variable. Sign that CLI in with auth.credentialBundleSecretRef, with auth mode APIKey where it has a key variable, or, for Hermes, Pi and OpenCode, with its model provider's key in env"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.mode) || self.auth.mode != 'APIKey' || self.agent in ['ClaudeCode', 'Codex', 'GeminiCLI', 'QwenCode', 'CursorAgent', 'Copilot', 'Grok', 'MuseCode']",message="auth mode APIKey takes a CLI Crewlet hands a key to: ClaudeCode, Codex, GeminiCLI, QwenCode, CursorAgent, Copilot, Grok or MuseCode. Crewlet's profiles for OpenCode, KimiCode, Hermes and Pi name no key variable: sign Hermes, Pi or OpenCode in with its model provider's key in env under mode Subscription, and KimiCode, which reads a key only from its own files, with auth.credentialBundleSecretRef"
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(e, (self.agent == 'ClaudeCode' && e.name in ['CLAUDE_CODE_OAUTH_TOKEN', 'ANTHROPIC_API_KEY']) || (self.agent == 'Codex' && e.name == 'OPENAI_API_KEY') || (self.agent == 'GeminiCLI' && e.name == 'GEMINI_API_KEY') || (self.agent == 'QwenCode' && e.name == 'DASHSCOPE_API_KEY') || (self.agent == 'CursorAgent' && e.name == 'CURSOR_API_KEY') || (self.agent == 'Copilot' && e.name == 'GITHUB_TOKEN') || (self.agent == 'Grok' && e.name == 'XAI_API_KEY') || (self.agent == 'MuseCode' && e.name == 'META_API_KEY'))",message="env may not set the CLI's own token or key variable (ClaudeCode's CLAUDE_CODE_OAUTH_TOKEN and ANTHROPIC_API_KEY, Codex's OPENAI_API_KEY, GeminiCLI's GEMINI_API_KEY, QwenCode's DASHSCOPE_API_KEY, CursorAgent's CURSOR_API_KEY, Copilot's GITHUB_TOKEN, Grok's XAI_API_KEY, MuseCode's META_API_KEY): Crewlet's auth owns it, and whether a value in env reaches the CLI would turn on the auth mode. Give a token as auth.tokenSecretRef, and a key as apiKeySecretRefs with auth mode APIKey",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="!has(self.mode) || self.mode != 'Agent' || (has(self.auth) && has(self.auth.mode) && self.auth.mode != 'Subscription') || (self.agent == 'ClaudeCode' && has(self.auth) && has(self.auth.tokenSecretRef)) || (self.agent == 'OpenCode' && has(self.env) && self.env.exists(e, e.name.matches('(?i)(^|_)(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)(_|$)')))",message="a Subscription login reaches an Agent-mode run in a remote sandbox only as a variable of its environment: give ClaudeCode auth.tokenSecretRef, or use auth mode APIKey, and give OpenCode its model provider's key in env under a name Crewlet reads as a credential, such as ANTHROPIC_API_KEY or OPENROUTER_API_KEY. A credential bundle stays on the engine's data volume"
type CLIAgentSettings struct {
	// agent is which CLI to drive. The provider's model is written as this
	// CLI reads it, and an Agent-mode run is handed it unchanged: OpenCode
	// reads `<provider>/<model>`, such as `anthropic/claude-sonnet-5`.
	// Crewlet's `agent`.
	// +required
	Agent CLIAgentName `json:"agent,omitempty"`

	// mode is how the engine uses the CLI. Unset is Text. Crewlet's `mode`.
	// +optional
	Mode CLIAgentMode `json:"mode,omitempty"`

	// runIn is where an Agent-mode run happens. Unset is the Company's
	// sandbox, which is E2B. Crewlet's `run_in`.
	// +optional
	RunIn CLIAgentRunIn `json:"runIn,omitempty"`

	// auth is how the CLI signs in.
	// +required
	Auth CLIAgentAuth `json:"auth,omitzero"`

	// env is extra environment of every invocation of the CLI: the child
	// gets an allowlisted environment, not the engine's, so a variable the
	// CLI needs beyond that is given here. It is how Hermes, Pi and OpenCode
	// sign in under auth mode Subscription with no login of their own: their
	// model provider's key, under a name Crewlet reads as a credential, such
	// as OPENROUTER_API_KEY. It may not set the CLI's own token or key
	// variable, which auth owns. An OpenCode provider's credential-named
	// variables also travel into every box of an Agent-mode run, and of a
	// seat's code work that runs on it, where the coding agent can read
	// them. Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SecretEnvVar `json:"env,omitempty"`

	// timeoutSeconds caps one invocation of the CLI, launch included. Unset
	// takes Crewlet's default of 300. Crewlet's `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// maxConcurrent is how many CLI processes the provider runs at once;
	// each takes 200 to 400 MB of the engine pod's memory. Unset takes
	// Crewlet's default of 4. Crewlet's `max_concurrent`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=64
	MaxConcurrent int32 `json:"maxConcurrent,omitempty"`

	// loginName names the login, and the per-seat homes, the provider
	// keeps on the data volume. Providers of the company with the same
	// loginName share one login, which is how several models run off one
	// subscription; they must drive the same CLI. Unset is the provider's
	// own metadata.name. Crewlet's `state_dir`, as
	// /var/lib/crewlet/llm-cli/<loginName>.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	LoginName string `json:"loginName,omitempty"`
}

// ReasoningEffort is how hard a model thinks. It maps onto Crewlet's
// `reasoning_effort` (Low is `low`, XHigh is `xhigh`, and so on).
// +kubebuilder:validation:Enum=Low;Medium;High;XHigh;Max
type ReasoningEffort string

// The reasoning-effort levels, lowest first.
const (
	ReasoningEffortLow    ReasoningEffort = "Low"
	ReasoningEffortMedium ReasoningEffort = "Medium"
	ReasoningEffortHigh   ReasoningEffort = "High"
	ReasoningEffortXHigh  ReasoningEffort = "XHigh"
	ReasoningEffortMax    ReasoningEffort = "Max"
)

// LLMCredentialCooldowns is how long a key that failed is set aside before it
// is tried again. It maps onto Crewlet's `cooldowns` block.
// +kubebuilder:validation:MinProperties=1
type LLMCredentialCooldowns struct {
	// rateLimitSeconds is how long a key that hit a rate limit is set aside.
	// Unset takes Crewlet's default of 3600.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	RateLimitSeconds int32 `json:"rateLimitSeconds,omitempty"`

	// authSeconds is how long a key the provider refused is set aside. Unset
	// takes Crewlet's default of 300.
	// +optional
	// +kubebuilder:validation:Minimum=60
	// +kubebuilder:validation:Maximum=86400
	AuthSeconds int32 `json:"authSeconds,omitempty"`
}

// LLMProviderSettings is everything about a model provider except where its
// keys come from. LLMProvider and the EngineClass's platform providers share
// it, so a platform-provided model is described exactly like a tenant's own.
// Each field maps onto the same-named field of a Crewlet providers.llm entry.
//
// The cross-field rules over these fields are declared on each type that
// embeds this one (LLMProviderSpec and PlatformLLMProvider), because
// controller-gen does not carry a validation declared on an embedded type into
// the schema of the type that inlines it. The API tests exercise both copies.
type LLMProviderSettings struct {
	// type is which model API the provider speaks. Crewlet's `type`.
	// +required
	Type LLMProviderType `json:"type,omitempty"`

	// model is the model id requests are sent for, as the provider names it.
	// Crewlet's `model`. Crewlet trims it of white space and refuses a
	// provider with nothing left, as naming no model.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="model may not be white space alone: Crewlet trims it, and refuses a provider that names no model"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="model may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	Model string `json:"model,omitempty"`

	// claudeModel names the Claude model whose request shape an Anthropic
	// provider uses when model is a gateway alias Crewlet cannot recognise.
	// Crewlet's `claude_model`; type Anthropic only.
	// +optional
	ClaudeModel ClaudeModelID `json:"claudeModel,omitempty"`

	// baseURL sends requests to this endpoint instead of the vendor's own,
	// for a gateway or a proxy. Required for type OpenAICompatible. For type
	// Anthropic it is the gateway's root, without /v1: Crewlet's own calls,
	// Claude Code (as ANTHROPIC_BASE_URL) and OpenCode each add the version
	// themselves. A seat's code work running on the provider hands it to the
	// coding agent in each box. Crewlet's `base_url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="baseURL must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="baseURL may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
	BaseURL string `json:"baseURL,omitempty"`

	// reasoning turns on an OpenAI reasoning model's effort, so that
	// reasoningEffort is sent. Crewlet's `reasoning`; type OpenAI only.
	// +optional
	Reasoning *bool `json:"reasoning,omitempty"`

	// reasoningEffort is how hard the model thinks. For Anthropic it is sent
	// on every call, and unset means High. For OpenAI it is sent only while
	// reasoning is true, so it requires reasoning; an OpenAICompatible
	// provider cannot turn reasoning on and takes no reasoningEffort.
	// Crewlet's `reasoning_effort`.
	// +optional
	ReasoningEffort ReasoningEffort `json:"reasoningEffort,omitempty"`

	// reasoningBudgetTokens is the thinking budget of an older, budget-based
	// Claude model. Unset means such a model does not think. Crewlet's
	// `reasoning_budget_tokens`; type Anthropic only.
	// +optional
	// +kubebuilder:validation:Minimum=1024
	ReasoningBudgetTokens int32 `json:"reasoningBudgetTokens,omitempty"`

	// timeoutSeconds bounds one HTTP attempt: the whole of a unary call, the
	// longest silence in a streamed one. Unset takes Crewlet's default of
	// 600. Crewlet's `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// cooldowns is how long a failing key is set aside. Crewlet's
	// `cooldowns`.
	// +optional
	Cooldowns LLMCredentialCooldowns `json:"cooldowns,omitzero"`
}

// LLMProviderSpec is one model provider of a company. It renders to one entry
// of Crewlet's providers.llm, keyed by the LLMProvider's metadata.name; Seats
// and the Company's model order refer to it by that name.
// +kubebuilder:validation:XValidation:rule="!has(self.claudeModel) || self.type == 'Anthropic'",message="claudeModel applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoning) || self.type == 'OpenAI'",message="reasoning applies to type OpenAI only; an Anthropic model's thinking depth is reasoningEffort"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningEffort) || self.type == 'Anthropic' || (self.type == 'OpenAI' && has(self.reasoning) && self.reasoning)",message="reasoningEffort is sent to an OpenAI model only while reasoning is true, and never to an OpenAICompatible one"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningBudgetTokens) || self.type == 'Anthropic'",message="reasoningBudgetTokens applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="self.type != 'OpenAICompatible' || has(self.baseURL)",message="baseURL is required for type OpenAICompatible, which has no default endpoint"
// +kubebuilder:validation:XValidation:rule="self.type in ['OpenAICompatible', 'CLIAgent'] || has(self.apiKeySecretRefs)",message="apiKeySecretRefs needs at least one key for types Anthropic and OpenAI"
// +kubebuilder:validation:XValidation:rule="(self.type == 'CLIAgent') == has(self.cli)",message="cli is required with type CLIAgent and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.type != 'CLIAgent' || !(has(self.baseURL) || has(self.timeoutSeconds) || has(self.cooldowns))",message="baseURL, timeoutSeconds and cooldowns configure an HTTP client, and a CLIAgent provider runs a process: its timeout is cli.timeoutSeconds"
// +kubebuilder:validation:XValidation:rule="self.type != 'CLIAgent' || !has(self.cli) || has(self.apiKeySecretRefs) == (has(self.cli.auth) && has(self.cli.auth.mode) && self.cli.auth.mode == 'APIKey')",message="a CLIAgent provider takes apiKeySecretRefs with cli.auth.mode APIKey, and only then"
// +kubebuilder:validation:XValidation:rule="self.type != 'CLIAgent' || !has(self.apiKeySecretRefs) || self.apiKeySecretRefs.size() == 1",message="a CLIAgent provider gives its CLI one key: it rotates none"
// +kubebuilder:validation:XValidation:rule="self.type != 'CLIAgent' || !has(self.cli) || self.cli.agent != 'OpenCode' || !has(self.model) || (self.model.indexOf('/') >= 0 && self.model.substring(0, self.model.indexOf('/')).trim().size() > 0 && self.model.substring(self.model.indexOf('/') + 1).trim().size() > 0)",message="an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash: a model with no slash, or with nothing but white space before or after it, names no provider or no model, and every call the provider makes, an Agent-mode run's included, would fail inside OpenCode with Model not found. Name the provider as OpenCode spells it (opencode models lists them), such as anthropic/claude-sonnet-5 or openrouter/anthropic/claude-sonnet-5, with that provider's key in cli.env",fieldPath=".model"
type LLMProviderSpec struct {
	// companyRef names the Company this provider belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	LLMProviderSettings `json:",inline"`

	// apiKeySecretRefs are the provider's API keys, each a key of a Secret in
	// this namespace. With more than one, Crewlet rotates through them,
	// setting aside a key that hits a rate limit or is refused, in the order
	// listed when several are equally free. Crewlet's `api_keys`, a
	// credential position: the values are pushed into the engine's secret
	// store and the document refers to them by name only. Required for types
	// Anthropic and OpenAI; an OpenAICompatible endpoint that needs no key
	// may omit it. A CLIAgent provider takes exactly one, with
	// cli.auth.mode APIKey, which it gives the CLI.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	APIKeySecretRefs []SecretKeyReference `json:"apiKeySecretRefs,omitempty"`

	// cli is the coding CLI a provider of type CLIAgent drives. Required
	// for, and only for, type CLIAgent. Crewlet's `cli`.
	// +optional
	CLI CLIAgentSettings `json:"cli,omitzero"`
}

// LLMProviderStatus is what the operator observed about an LLMProvider.
// +kubebuilder:validation:MinProperties=1
type LLMProviderStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (every
	// Secret key exists), Applied (the provider is in the revision the
	// engine runs) and, on a provider of type CLIAgent in mode Agent,
	// AgentModeRunnable (whether its runs can call a seat's tools back
	// through the Company's public address).
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// LLMProvider is a model provider a company's seats can run on: one entry of
// Crewlet's providers.llm, keyed by its name. The name default is refused:
// Crewlet runs a seat that names no chain on the provider keyed default when
// there is one, whatever the Company's spec.llm.order says.
//
// +kubebuilder:validation:XValidation:rule="self.metadata.name != 'default'",message="an LLMProvider may not be named default: Crewlet runs every seat that names no chain on the provider keyed default, ahead of spec.llm.order"
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=llmp,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="AgentMode",type=string,JSONPath=`.status.conditions[?(@.type=="AgentModeRunnable")].status`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LLMProvider struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata. Its name is the provider's
	// key in the company document.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the provider's desired state.
	// +required
	Spec LLMProviderSpec `json:"spec,omitzero"`

	// status is the provider's observed state.
	// +optional
	Status LLMProviderStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LLMProviderList contains a list of LLMProvider.
type LLMProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LLMProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &LLMProvider{}, &LLMProviderList{})
		return nil
	})
}
