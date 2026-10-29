package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// CLIAgentName is which coding CLI a model provider drives. It maps onto the
// `agent` of a Crewlet providers.llm entry (ClaudeCode is `claude-code`, Codex
// is `codex`, GeminiCLI is `gemini-cli`, QwenCode is `qwen-code`, OpenCode is
// `opencode`, CursorAgent is `cursor-agent`, Copilot is `copilot`, Grok is
// `grok`, MuseCode is `muse-code`, KimiCode is `kimi-code`, Hermes is
// `hermes`, Pi is `pi`). Crewlet's `custom`, a CLI described wholly by profile
// overrides, is not offered: the overrides name the binary and the arguments
// the engine's pod runs.
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

// CLIAgentWebTools is whether a model provider's CLI keeps its own web tools
// (its fetch and its search). It maps onto the `web_tools` of a Crewlet
// providers.llm entry's overrides (Allowed is `allowed`, Denied is
// `denied`).
//
// A seat keeps its CLI's web tools: it must not have less reach than the
// same CLI at a terminal. A provider the Company's Dashboard Assistant runs
// on turns them off, which Crewlet requires of every model in
// spec.assistant.llm: the assistant reads only what the signed-in person's
// own requests return, and a page the model fetched for itself is text no
// authority decision saw. Crewlet turns them off only for a CLI whose profile
// carries a measured way to (its web_denial): ClaudeCode, Codex, OpenCode and
// Pi.
// +kubebuilder:validation:Enum=Allowed;Denied
type CLIAgentWebTools string

// The web tool stances.
const (
	// CLIAgentWebToolsAllowed keeps the CLI's web tools, every seat's
	// default.
	CLIAgentWebToolsAllowed CLIAgentWebTools = "Allowed"
	// CLIAgentWebToolsDenied runs the CLI with its web tools off.
	CLIAgentWebToolsDenied CLIAgentWebTools = "Denied"
)

// CLIAgentAuthMode is how a model provider's CLI authenticates. It maps onto
// the `auth.mode` of a Crewlet providers.llm entry (Subscription is
// `subscription`, APIKey is `api-key`, Env is `env`).
//
// Crewlet's `inherit-env`, which hands the CLI whichever token or key the
// engine's own environment holds, is not offered: a credential in the
// engine's environment is outside the encrypted store, readable by every
// process the engine runs, and resolvable by a ${NAME} written in many fields
// of the company document, so it would be the one credential the API takes by
// another route than a Secret reference. Subscription, APIKey and Env cover
// what it is for.
// +kubebuilder:validation:Enum=Subscription;APIKey;Env
type CLIAgentAuthMode string

// The CLI agent authentication modes.
const (
	// CLIAgentAuthSubscription signs the CLI in to a subscription with
	// tokenSecretRef or credentialBundleSecretRef, or signs Hermes, Pi or
	// OpenCode in with its model provider's key in the provider's env.
	CLIAgentAuthSubscription CLIAgentAuthMode = "Subscription"
	// CLIAgentAuthAPIKey gives the CLI the key of apiKeySecretRef, in the
	// key variable Crewlet's profile for it names, billing the metered
	// account. CursorAgent's key is a Cursor account's API key
	// (CURSOR_API_KEY), not a model provider's.
	CLIAgentAuthAPIKey CLIAgentAuthMode = "APIKey"
	// CLIAgentAuthEnv signs the CLI in with the provider's env alone: a
	// cloud provider's switch and its credentials (ClaudeCode's
	// CLAUDE_CODE_USE_BEDROCK with the AWS variables), a gateway's address
	// and the token it takes (ANTHROPIC_BASE_URL with ANTHROPIC_AUTH_TOKEN),
	// or, for Hermes, Pi and OpenCode, their model provider's key. Crewlet
	// hands the CLI neither its token variable nor its key variable, and
	// seeds no login, so nothing competes with what env signs in with.
	CLIAgentAuthEnv CLIAgentAuthMode = "Env"
)

// CLIAgentAuth is how a company's model provider signs its CLI in. It maps
// onto the `auth` of a Crewlet providers.llm entry.
//
// A subscription login is made at a terminal, by `crewlet llm login` or the
// CLI's own command, and a pod has no terminal: Subscription signs in with a
// headless token, such as the CLAUDE_CODE_OAUTH_TOKEN `claude setup-token`
// mints, or with a credential bundle `crewlet llm export` wrote from a login
// made elsewhere: the CLI's login, which is its credential files, or, for
// OpenCode, which keeps its logins in a database of its own, the document
// OpenCode 2's auth export printed. A bundle is restored only into a
// provider's login on the data volume that is empty or that was installed
// before the bundle's own login, by the install record each carries; a
// refresh keeps its login's record, so the refreshed login a running engine
// keeps there wins over a bundle of the same login, and only a bundle
// exported after a newer login replaces it. A token never needs refreshing,
// which makes it the better of the two wherever the CLI mints one. A provider names one of the two: given both, which login the CLI acts
// on would be the CLI's choice between its environment and its own files,
// which nothing here can see.
//
// APIKey signs in with the one key of apiKeySecretRef, which Crewlet puts in
// the CLI's key variable. Without it Crewlet would read that variable itself
// from its secret store and the engine's environment, which hold credentials
// that are not this object's to name, so the key is required with the mode;
// it is refused under any other, where Crewlet reads none.
//
// Crewlet hands a CLI a token or a key only through the variable its profile
// names, and refuses one for a CLI whose profile names none, since it would
// reach nothing: so LLMProviderSpec holds tokenSecretRef to the CLIs with a
// token variable and mode APIKey to the CLIs with a key variable.
//
// Env signs the CLI in with the provider's env and nothing else: Crewlet
// hands it neither its token variable nor its key variable and seeds no
// login, so a cloud provider or a gateway is the CLI's one sign-in rather than
// a second one the CLI ranks against a token or a key by rules of its own. It
// takes a CLI whose Crewlet profile says what in env signs it in: ClaudeCode,
// whose profile lists the names that do (sign_in_env: ANTHROPIC_AUTH_TOKEN,
// CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX and CLAUDE_CODE_USE_FOUNDRY),
// one of which env must set, and Hermes, Pi and OpenCode, whose env must hold
// their model provider's key under a credential's name (below). Those
// ClaudeCode names are refused in env under every other mode, which hands the
// CLI a credential of its own beside them. tokenSecretRef and
// credentialBundleSecretRef are refused with it, as no token or login reaches
// the CLI. The rules are LLMProviderSpec's, which sees the agent and env beside
// auth.
//
// Hermes, Pi and OpenCode may sign in to a Subscription with neither: each
// fronts many model providers and reads each provider's key from that
// provider's own variable, so Crewlet's profile for it names no key variable
// and marks it as signing in through its environment (env_sign_in), and
// Crewlet documents the provider's key in the entry's env as its metered
// sign-in, and hands that env to the CLI untouched. Crewlet counts an entry of
// env as a sign-in only when its name says it holds a credential: one of
// its parts between underscores is, in any case, KEY, KEYS, APIKEY,
// ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL,
// CREDENTIALS, AUTH, OAUTH or PAT (cliprofile.IsCredentialName). So
// OPENROUTER_API_KEY signs such a CLI in and HTTPS_PROXY or
// HERMES_MAX_TOKENS does not, and one whose env holds no such name is
// signed in to nothing, and refused. Every other CLI keeps to a token, a
// bundle or a key: one with a key variable takes its key through mode APIKey,
// and Crewlet refuses that variable in env, which auth owns. The rule is
// LLMProviderSpec's, which sees the agent and env beside auth.
// +kubebuilder:validation:XValidation:rule="!(has(self.mode) && self.mode != 'Subscription') || (!has(self.tokenSecretRef) && !has(self.credentialBundleSecretRef))",message="tokenSecretRef and credentialBundleSecretRef sign in to a subscription: they apply to mode Subscription only"
// +kubebuilder:validation:XValidation:rule="!has(self.tokenSecretRef) || !has(self.credentialBundleSecretRef)",message="tokenSecretRef and credentialBundleSecretRef are two logins, and the CLI is given one: keep the headless token, which never needs refreshing, or the bundle where the CLI mints no token"
// +kubebuilder:validation:XValidation:rule="has(self.apiKeySecretRef) == (has(self.mode) && self.mode == 'APIKey')",message="apiKeySecretRef is the key auth mode APIKey hands the CLI: it is required with mode APIKey and not allowed otherwise, as Crewlet reads a key under no other mode"
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

	// apiKeySecretRef is the Secret key holding the metered API key auth
	// mode APIKey hands the CLI, in the key variable Crewlet's profile for
	// it names (ClaudeCode's ANTHROPIC_API_KEY, Codex's CODEX_API_KEY).
	// One key: a CLI holds one sign-in and rotates nothing, so a second key
	// is a provider of its own, later in a seat's chain. Crewlet's
	// `api_key`, a credential position: the value is pushed into the
	// engine's secret store and the document refers to it by name only.
	// +optional
	APIKeySecretRef SecretKeyReference `json:"apiKeySecretRef,omitzero"`

	// credentialBundleSecretRef is the Secret key holding a credential
	// bundle `crewlet llm export` wrote. A bundle holding a login OpenCode
	// 1.x made (its auth.json) is refused when the engine restores it, and
	// the provider runs signed out: export it again from OpenCode 2.
	// Crewlet's `credential_bundle`, a credential position.
	// +optional
	CredentialBundleSecretRef SecretKeyReference `json:"credentialBundleSecretRef,omitzero"`
}

// LLMProviderSettings is what a company's model provider and a platform's
// share: the coding CLI, the model it runs, and the bounds on its calls.
// LLMProvider and the EngineClass's platform providers embed it, so a
// platform-provided model is described exactly like a tenant's own. Each field
// maps onto the same-named field of a Crewlet providers.llm entry.
//
// Every model Crewlet runs is a coding CLI it drives: Claude Code for
// Anthropic's models, on a subscription or an API key, Codex for OpenAI's,
// OpenCode for open models on any provider. The CLI is a binary in the
// engine's image, which the official image does not ship: a class running a
// company's seats names an image derived from the engine's with the CLI
// installed (docs/guides/cli-agents.md). For OpenCode that is OpenCode 2, the
// @opencode/cli package: Crewlet drives no other release, and every command
// it runs carries --standalone, which OpenCode 1.x (npm's opencode-ai)
// rejects.
//
// The cross-field rules over these fields are declared on each type that
// embeds this one (LLMProviderSpec and PlatformLLMProvider), because
// controller-gen does not carry a validation declared on an embedded type into
// the schema of the type that inlines it. The API tests exercise both copies.
type LLMProviderSettings struct {
	// agent is which CLI to drive. The model is written as this CLI reads
	// it, and a seat's executor run on the provider is handed it unchanged:
	// OpenCode reads `<provider>/<model>`, such as
	// `anthropic/claude-sonnet-5`. Only ClaudeCode and OpenCode run a
	// seat's executor; every CLI serves a seat's other phases. Crewlet's
	// `agent`.
	// +required
	Agent CLIAgentName `json:"agent,omitempty"`

	// model is the model the CLI runs, in the CLI's own grammar: an alias
	// or an id for ClaudeCode (`sonnet`, `claude-opus-5-5`), an OpenAI id
	// for Codex, `<provider>/<model>` for OpenCode. Crewlet's `model`.
	// Crewlet trims it of white space and refuses a provider with nothing
	// left, as naming no model.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="model may not be white space alone: Crewlet trims it, and refuses a provider that names no model"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="model may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	Model string `json:"model,omitempty"`

	// timeoutSeconds bounds one stretch of the CLI's model work, wall clock,
	// with no call of a seat's tool in flight: from the CLI's launch, or the
	// answer to its last tool call, to its next tool call or its end, the
	// model's answer and the CLI's own retries in it. It is not a bound on
	// a run of the CLI, which asks the model many times and calls the
	// seat's tools between: a tool call stops the clock however long it
	// takes (a delegation, a slow tool server), and the next stretch starts
	// afresh. Unset takes Crewlet's default of 600, the ceiling Anthropic
	// documents for one answer of a current Claude model at its full
	// output. A timeout is retryable, so a seat's chain moves on to its
	// next provider. It bounds the phases the CLI runs in the engine's
	// process (the reviewer, delegated workers, the summariser, the judge);
	// a seat's executor run on the provider, its CLI's own run in a box, is
	// bounded by its round cap instead (the Company's
	// spec.sandbox.defaultMaxTurns and the Seat's spec.sandbox.maxTurns).
	// Crewlet's `timeout_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// maxConcurrent is how many of the provider's CLI processes run at once
	// in each engine pod; each takes 200 to 450 MB of the pod's memory,
	// OpenCode's at the top of that range (docs/guides/cli-agents.md). One
	// process lives for a whole run of the CLI, many model requests and
	// tool calls long, and holds its place for all of it, so this is also
	// how many seats' runs the provider has going at once in a pod: a run
	// past it waits for a place. A model call made on the same provider
	// from inside one of a run's tool calls (a delegated worker's) borrows
	// the run's place rather than wait for another, which the runs holding
	// every place could leave it waiting for forever. Unset takes
	// Crewlet's default of 4. A seat's executor run takes none: it is a
	// process of its own box, and a seat runs one at a time. Crewlet's
	// `max_concurrent`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=64
	MaxConcurrent int32 `json:"maxConcurrent,omitempty"`

	// webTools is whether the CLI keeps its own web tools. Unset is
	// Allowed, every seat's default. Denied is required of every provider
	// the Company's Dashboard Assistant runs on (spec.assistant.llm), and
	// is refused for a CLI Crewlet has no way to turn the web off for: it
	// takes ClaudeCode, Codex, OpenCode or Pi. Crewlet's
	// `overrides.web_tools`.
	// +optional
	WebTools CLIAgentWebTools `json:"webTools,omitempty"`
}

// LLMProviderSpec is one model provider of a company: a coding CLI the engine
// drives. It renders to one entry of Crewlet's providers.llm, keyed by the
// LLMProvider's metadata.name; Seats and the Company's model order refer to it
// by that name.
//
// Its login, and the per-seat homes the CLI keeps, are on the data volume, so
// a restart or a new pod keeps them.
//
// Its rules hold every credential to a variable Crewlet's profile for the
// CLI reads, as Crewlet's own admission does (providers.llm.*): the engine
// refuses a write that names a credential its CLI would never be given, so
// the API refuses it on the object instead. The lists of CLIs the rules name
// and the variables they quote are those whose profiles set token_env,
// api_key_env, env_sign_in or sign_in_env in the engine the operator is tested
// against, Crewlet's main; a test holds them to that engine's own verdict. The rules also keep env off the variables
// Crewlet sets on every call to keep each seat's CLI inside its own home and
// its call's working directory (HOME, the XDG directories, TMPDIR, and the
// configuration directory or switch each CLI's profile names in config_env,
// work_config_env and isolation_env): env is layered over them, so a value
// there would replace that isolation for every seat on the provider, and
// Crewlet refuses the document. A test holds that list to the engine's
// refusals too.
//
// A seat's executor, which does the seat's work, is its CLI's own agentic
// run: the CLI's loop with its own shell and editor, fully permissioned, as
// a headless run has nobody to answer a permission prompt, calling the
// seat's tools as one MCP server through the engine's tool bridge. Crewlet
// runs it on the first member of the seat's executor chain that drives
// ClaudeCode or OpenCode, the CLIs it has an in-box runner for, and is not
// sitting out its spent plan on the node, in a box placed by that member's
// runIn, else by the Company's spec.sandbox.defaultRunIn: Direct, a process
// of the engine pod that holds the seat, or E2B, the Company's E2B sandbox.
// Every other CLI serves a seat's other phases alone (the reviewer,
// delegated workers, the summariser and the judge), in the engine's pod.
// Crewlet hands an executor run its provider's model exactly as the
// provider's runs in the engine's pod pass it, so one model serves every
// phase. A Direct box is seeded with the provider's login, its credential
// files or the document OpenCode 2's export printed, and writes a refreshed
// login back. An E2B box never receives it, as the login carries a refresh
// token the engine keeps rotating, so there a Subscription login reaches the
// run only as a variable of its environment, which for ClaudeCode is its
// headless token and for OpenCode its model provider's key in env. Under
// auth mode APIKey or Env no login is seeded in either cell: the key, or
// env, is the sign-in, and it follows the run wherever it runs.
// +kubebuilder:validation:XValidation:rule="!has(self.webTools) || self.webTools != 'Denied' || self.agent in ['ClaudeCode', 'Codex', 'OpenCode', 'Pi']",message="webTools Denied takes a CLI Crewlet can turn the web off for: ClaudeCode, Codex, OpenCode or Pi. Crewlet's profile for every other CLI carries no measured web denial, and refuses the setting",fieldPath=".webTools"
// +kubebuilder:validation:XValidation:rule="!has(self.runIn) || self.agent in ['ClaudeCode', 'OpenCode']",message="runIn places the provider's executor runs, and Crewlet launches an executor run only on ClaudeCode or OpenCode: a provider of another CLI serves a seat's other phases alone, in the engine's pod, and Crewlet refuses a runIn on it",fieldPath=".runIn"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || (has(self.auth.mode) && self.auth.mode != 'Subscription') || has(self.auth.tokenSecretRef) || has(self.auth.credentialBundleSecretRef) || (self.agent in ['Hermes', 'Pi', 'OpenCode'] && has(self.env) && self.env.exists(e, e.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)')))",message="a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef: a pod has no terminal to log in at. Hermes, Pi and OpenCode alone may sign in with their model provider's key in env instead, under a name Crewlet reads as a credential, one of whose parts between underscores is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT (such as OPENROUTER_API_KEY), which Crewlet hands them untouched. A CLI with a key variable takes its key through auth mode APIKey, as Crewlet refuses that variable in env, and a cloud provider or a gateway signs the CLI in from env under auth mode Env",fieldPath=".auth"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.mode) || self.auth.mode != 'Env' || self.agent in ['ClaudeCode', 'Hermes', 'Pi', 'OpenCode']",message="auth mode Env takes a CLI Crewlet signs in from its env: ClaudeCode, by a gateway's token or a cloud provider's switch, and Hermes, Pi or OpenCode, by their model provider's key. Crewlet's profiles for every other CLI name no variable that signs it in from env (sign_in_env) and read no model provider's key from it (env_sign_in), so the provider would sign in to nothing, and Crewlet refuses the document: sign it in with auth mode Subscription or APIKey",fieldPath=".auth"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.mode) || self.auth.mode != 'Env' || !(self.agent in ['ClaudeCode', 'Hermes', 'Pi', 'OpenCode']) || (has(self.env) && self.env.exists(e, (self.agent == 'ClaudeCode' && e.name in ['ANTHROPIC_AUTH_TOKEN', 'CLAUDE_CODE_USE_BEDROCK', 'CLAUDE_CODE_USE_VERTEX', 'CLAUDE_CODE_USE_FOUNDRY']) || (self.agent in ['Hermes', 'Pi', 'OpenCode'] && e.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)'))))",message="auth mode Env signs the CLI in with env alone, and env sets nothing that signs it in, so the provider would sign in to nothing and Crewlet refuses the document: give ClaudeCode ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX or CLAUDE_CODE_USE_FOUNDRY, with the gateway's address or the cloud provider's credentials beside it, and Hermes, Pi or OpenCode their model provider's key under a name Crewlet reads as a credential, one of whose parts between underscores is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT (such as OPENROUTER_API_KEY)",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="(has(self.auth) && has(self.auth.mode) && self.auth.mode == 'Env') || !has(self.env) || !self.env.exists(e, self.agent == 'ClaudeCode' && e.name in ['ANTHROPIC_AUTH_TOKEN', 'CLAUDE_CODE_USE_BEDROCK', 'CLAUDE_CODE_USE_VERTEX', 'CLAUDE_CODE_USE_FOUNDRY'])",message="env may set a variable that signs the CLI in from env (ClaudeCode's ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX and CLAUDE_CODE_USE_FOUNDRY) only under auth mode Env: Subscription and APIKey hand the CLI a credential of their own beside it, which the CLI then ranks by rules of its own, so Crewlet refuses the document. Set auth mode Env, so that env is the provider's one sign-in",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="self.agent != 'QwenCode' || (has(self.auth) && has(self.auth.mode) && self.auth.mode == 'APIKey')",message="QwenCode signs in with its key alone, under auth mode APIKey: Crewlet's profile for it keeps no login and names no token variable, as the builds of Qwen Code Crewlet supports refuse the OAuth login it once kept, so under Subscription it would sign in to nothing. Give it its key with auth mode APIKey and auth.apiKeySecretRef",fieldPath=".auth"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.tokenSecretRef) || self.agent == 'ClaudeCode'",message="auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode (CLAUDE_CODE_OAUTH_TOKEN): its profile for every other CLI names no token variable. Sign that CLI in with auth.credentialBundleSecretRef, with auth mode APIKey where it has a key variable, or, for Hermes, Pi and OpenCode, with its model provider's key in env"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.mode) || self.auth.mode != 'APIKey' || self.agent in ['ClaudeCode', 'Codex', 'GeminiCLI', 'QwenCode', 'CursorAgent', 'Copilot', 'Grok', 'MuseCode']",message="auth mode APIKey takes a CLI Crewlet hands a key to: ClaudeCode, Codex, GeminiCLI, QwenCode, CursorAgent, Copilot, Grok or MuseCode. Crewlet's profiles for OpenCode, KimiCode, Hermes and Pi name no key variable: sign Hermes, Pi or OpenCode in with its model provider's key in env under mode Subscription, and KimiCode, which reads a key only from its own files, with auth.credentialBundleSecretRef"
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(e, (self.agent == 'ClaudeCode' && e.name in ['CLAUDE_CODE_OAUTH_TOKEN', 'ANTHROPIC_API_KEY']) || (self.agent == 'Codex' && e.name == 'CODEX_API_KEY') || (self.agent == 'GeminiCLI' && e.name == 'GEMINI_API_KEY') || (self.agent == 'QwenCode' && e.name == 'OPENAI_API_KEY') || (self.agent == 'CursorAgent' && e.name == 'CURSOR_API_KEY') || (self.agent == 'Copilot' && e.name == 'COPILOT_GITHUB_TOKEN') || (self.agent == 'Grok' && e.name == 'XAI_API_KEY') || (self.agent == 'MuseCode' && e.name == 'META_API_KEY'))",message="env may not set the CLI's own token or key variable (ClaudeCode's CLAUDE_CODE_OAUTH_TOKEN and ANTHROPIC_API_KEY, Codex's CODEX_API_KEY, GeminiCLI's GEMINI_API_KEY, QwenCode's OPENAI_API_KEY, CursorAgent's CURSOR_API_KEY, Copilot's COPILOT_GITHUB_TOKEN, Grok's XAI_API_KEY, MuseCode's META_API_KEY): Crewlet's auth owns it, and whether a value in env reaches the CLI would turn on the auth mode. Give a token as auth.tokenSecretRef, and a key as auth.apiKeySecretRef with auth mode APIKey",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(e, e.name in ['HOME', 'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'TMPDIR'] || (self.agent == 'ClaudeCode' && e.name in ['CLAUDE_CONFIG_DIR', 'CLAUDE_CODE_DISABLE_CLAUDE_MDS', 'CLAUDE_CODE_DISABLE_AUTO_MEMORY']) || (self.agent == 'Codex' && e.name in ['CODEX_HOME', 'CODEX_SQLITE_HOME']) || (self.agent == 'OpenCode' && e.name in ['OPENCODE_CONFIG_DIR', 'OPENCODE_CONFIG_PROJECT_DISABLE']) || (self.agent == 'CursorAgent' && e.name == 'CURSOR_CONFIG_DIR') || (self.agent == 'KimiCode' && e.name == 'KIMI_CODE_HOME') || (self.agent == 'Hermes' && e.name == 'HERMES_HOME') || (self.agent == 'Pi' && e.name == 'PI_CODING_AGENT_DIR'))",message="env may not set a variable Crewlet sets on every call of the CLI to keep it inside its seat's own home and its call's working directory: HOME, XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME, XDG_CACHE_HOME or TMPDIR for any CLI, and ClaudeCode's CLAUDE_CONFIG_DIR, CLAUDE_CODE_DISABLE_CLAUDE_MDS and CLAUDE_CODE_DISABLE_AUTO_MEMORY, Codex's CODEX_HOME and CODEX_SQLITE_HOME, OpenCode's OPENCODE_CONFIG_DIR and OPENCODE_CONFIG_PROJECT_DISABLE, CursorAgent's CURSOR_CONFIG_DIR, KimiCode's KIMI_CODE_HOME, Hermes's HERMES_HOME or Pi's PI_CODING_AGENT_DIR. A value there would replace that isolation for every seat on the provider, so Crewlet refuses the document: drop it",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="(has(self.auth) && has(self.auth.mode) && self.auth.mode != 'Subscription') || !has(self.env) || !self.env.exists(e, self.agent == 'GeminiCLI' && e.name == 'GOOGLE_GENAI_USE_GCA')",message="under auth mode Subscription, env may not set a variable Crewlet sets there to sign the CLI in with the login the seat's home holds: GeminiCLI's GOOGLE_GENAI_USE_GCA, which Crewlet sets to true. A value there would be replaced on every call, so Crewlet refuses the document: drop it",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="self.agent != 'OpenCode' || !has(self.model) || (self.model.indexOf('/') >= 0 && self.model.substring(0, self.model.indexOf('/')).trim().size() > 0 && self.model.substring(self.model.indexOf('/') + 1).trim().size() > 0)",message="an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash: a model with no slash, or with nothing but white space before or after it, names no provider or no model, and every call the provider makes, an executor run's included, would fail inside OpenCode, which OpenCode 2 refuses as Invalid model reference. Name the provider as OpenCode spells it (opencode models --standalone lists them), such as anthropic/claude-sonnet-5 or openrouter/anthropic/claude-sonnet-5, with that provider's key in env, or a login of OpenCode's own in auth.credentialBundleSecretRef, which a Direct run is seeded with and an E2B run never receives",fieldPath=".model"
type LLMProviderSpec struct {
	// companyRef names the Company this provider belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	LLMProviderSettings `json:",inline"`

	// runIn is where a seat's executor run launched on the provider happens:
	// Direct, a process of the engine pod that holds the seat, or E2B, the
	// Company's E2B sandbox, which spec.sandbox.e2b must then configure.
	// Unset takes the Company's spec.sandbox.defaultRunIn. It is read
	// wherever a seat's executor can launch on the provider: as the first
	// ClaudeCode or OpenCode member of its executor chain, and as a later
	// one a launch falls back to while the members before it sit out their
	// spent plans, so each run is placed by the member it runs on. Only
	// ClaudeCode and OpenCode take it, as only they run an executor.
	// Crewlet's `run_in`.
	// +optional
	RunIn SandboxPlacement `json:"runIn,omitempty"`

	// auth is how the CLI signs in. Crewlet's `auth`.
	// +required
	Auth CLIAgentAuth `json:"auth,omitzero"`

	// env is extra environment of the CLI, in every phase a seat runs on the
	// provider, its executor run included. In the engine's pod the CLI does
	// not get the engine's environment: it gets the part Crewlet's
	// allowlist names, such as PATH and the TLS and proxy variables, and the
	// variables the CLI's Crewlet profile passes through, such as
	// GeminiCLI's GOOGLE_CLOUD_PROJECT, with a HOME, a TMPDIR and XDG
	// directories of its seat's own rather than the engine's.
	// So a variable the CLI needs beyond that is given here, such as the
	// address of a gateway a CLI reads from its environment (ClaudeCode's
	// ANTHROPIC_BASE_URL). It is how Hermes, Pi and OpenCode sign in under
	// auth mode Subscription or Env with no login of their own: their model
	// provider's key, under a name Crewlet reads as a credential, such as
	// OPENROUTER_API_KEY. Under auth mode Env it is the whole sign-in of
	// ClaudeCode too: a gateway's ANTHROPIC_AUTH_TOKEN, or a cloud
	// provider's switch (CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX,
	// CLAUDE_CODE_USE_FOUNDRY) with that provider's credentials, names it may
	// set under no other mode, which hands the CLI a credential of its own
	// beside them. It may not set the CLI's own token or key
	// variable, which auth owns, nor a variable Crewlet sets to keep the CLI
	// inside its seat's home and its call's working directory: HOME, the XDG
	// directories, TMPDIR, and the CLI's own configuration directory or
	// switch (docs/guides/cli-agents.md). Under auth mode Subscription it may
	// not set a variable Crewlet sets there to sign the CLI in with its
	// seat's login either: GeminiCLI's GOOGLE_GENAI_USE_GCA.
	//
	// A seat's executor run on the provider, Direct or E2B, gets all of env,
	// beneath the Seat's own spec.sandbox.env, which wins a name: a
	// gateway's address and the bearer token it takes, a cloud switch such
	// as CLAUDE_CODE_USE_BEDROCK and its region, a proxy, so the run reaches
	// its model as the provider's other phases do. So under auth mode Env,
	// env signs an E2B run in as it signs the provider's other phases in. A
	// Seat whose spec.sandbox.env signs ClaudeCode in itself
	// keeps every sign-in of the provider's out of its runs, and the rest of
	// env in them. Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SecretEnvVar `json:"env,omitempty"`

	// loginName names the login, and the per-seat homes, the provider
	// keeps on the data volume. Providers of the company with the same
	// loginName that drive the same CLI share one login, which is how
	// several models run off one subscription; each CLI keeps its own
	// beside the others' (Crewlet's <state_dir>/<agent>), so one of another
	// CLI shares the directory and nothing in it. Unset is the provider's
	// own metadata.name. Crewlet's `state_dir`, as
	// /var/lib/crewlet/llm-cli/<loginName>.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	LoginName string `json:"loginName,omitempty"`
}

// LLMProviderStatus is what the operator observed about an LLMProvider.
// +kubebuilder:validation:MinProperties=1
type LLMProviderStatus struct {
	// conditions are Accepted (the Company exists), ResolvedRefs (every
	// Secret key exists) and Applied (the provider is in the revision the
	// engine runs; False with SandboxPlacementNotOffered while a seat's
	// executor can launch on it in a cell the Company's spec.sandbox does
	// not configure).
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

// LLMProvider is a model a company's seats can run on: a coding CLI the
// engine drives, one entry of Crewlet's providers.llm, keyed by its name. The
// name default is refused: Crewlet runs a seat that names no chain on the
// provider keyed default when there is one, whatever the Company's
// spec.llm.order says.
//
// +kubebuilder:validation:XValidation:rule="self.metadata.name != 'default'",message="an LLMProvider may not be named default: Crewlet runs every seat that names no chain on the provider keyed default, ahead of spec.llm.order"
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=llmp,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Agent",type=string,JSONPath=`.spec.agent`
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=`.spec.model`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="RunIn",type=string,JSONPath=`.spec.runIn`,priority=1
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
