package render

// The types in this file are the company document in Crewlet's own shape:
// each field carries the key Crewlet reads (internal/config in Crewlet), and
// nothing else. They exist only to be encoded. A field the custom resources
// left unset is omitted, so the engine applies its own default rather than a
// copy of it frozen into the operator.

type companyDoc struct {
	Name         string          `json:"name"`
	Mission      string          `json:"mission,omitempty"`
	Vision       string          `json:"vision,omitempty"`
	Policies     []string        `json:"policies,omitempty"`
	Timezone     string          `json:"timezone,omitempty"`
	Tracker      trackerDoc      `json:"tracker,omitzero"`
	Knowledge    knowledgeDoc    `json:"knowledge,omitzero"`
	TokenBudget  tokenBudgetDoc  `json:"token_budget,omitzero"`
	Assistant    assistantDoc    `json:"assistant,omitzero"`
	Providers    providersDoc    `json:"providers,omitzero"`
	Integrations integrationsDoc `json:"integrations,omitzero"`
	MCPServers   []mcpServerDoc  `json:"mcp_servers,omitempty"`
	Roles        []roleDoc       `json:"roles,omitempty"`
	Units        []unitDoc       `json:"units,omitempty"`
}

type trackerDoc struct {
	Backend string             `json:"backend,omitempty"`
	Worklet *trackerWorkletDoc `json:"worklet,omitempty"`
}

type trackerWorkletDoc struct {
	InboxRetentionDays int32 `json:"inbox_retention_days,omitempty"`
}

type knowledgeDoc struct {
	Backend         string   `json:"backend,omitempty"`
	Scope           []string `json:"scope,omitempty"`
	SkillsContainer *string  `json:"skills_container,omitempty"`
	RootSpace       string   `json:"root_space,omitempty"`
	Vectors         *bool    `json:"vectors,omitempty"`
}

type tokenBudgetDoc struct {
	Day   int64 `json:"day,omitempty"`
	Week  int64 `json:"week,omitempty"`
	Month int64 `json:"month,omitempty"`
}

type assistantDoc struct {
	Enabled            *bool          `json:"enabled,omitempty"`
	LLM                []string       `json:"llm,omitempty"`
	MaxSteps           int32          `json:"max_steps,omitempty"`
	TurnTimeoutSeconds int32          `json:"turn_timeout_seconds,omitempty"`
	ProposalTTLSeconds int32          `json:"proposal_ttl_seconds,omitempty"`
	RetentionDays      int32          `json:"retention_days,omitempty"`
	TokenBudget        tokenBudgetDoc `json:"token_budget,omitzero"`
	DeepWork           *deepWorkDoc   `json:"deep_work,omitempty"`
}

type deepWorkDoc struct {
	LLM            string `json:"llm"`
	TimeoutSeconds int32  `json:"timeout_seconds,omitempty"`
	MaxSteps       int32  `json:"max_steps,omitempty"`
	MaxTokens      int32  `json:"max_tokens,omitempty"`
}

type providersDoc struct {
	LLM        map[string]llmProviderDoc `json:"llm,omitempty"`
	LLMOrder   []string                  `json:"llm_order,omitempty"`
	Embeddings *embeddingsDoc            `json:"embeddings,omitempty"`
	Sandbox    *sandboxDoc               `json:"sandbox,omitempty"`
}

type llmProviderDoc struct {
	Agent          string            `json:"agent"`
	Model          string            `json:"model"`
	RunIn          string            `json:"run_in,omitempty"`
	StateDir       string            `json:"state_dir"`
	TimeoutSeconds int32             `json:"timeout_seconds,omitempty"`
	MaxConcurrent  int32             `json:"max_concurrent,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Auth           llmAuthDoc        `json:"auth,omitzero"`
	Overrides      *llmOverridesDoc  `json:"overrides,omitempty"`
}

// llmOverridesDoc is the part of a CLI profile's overrides the API sets:
// the web stance alone. Every other override names the CLI's binary and
// arguments, which the engine's pod runs, and is not offered.
type llmOverridesDoc struct {
	WebTools string `json:"web_tools"`
}

type llmAuthDoc struct {
	Mode             string `json:"mode,omitempty"`
	Token            string `json:"token,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	CredentialBundle string `json:"credential_bundle,omitempty"`
}

type embeddingsDoc struct {
	Type           string `json:"type"`
	Model          string `json:"model"`
	APIKey         string `json:"api_key,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	Dimensions     int32  `json:"dimensions,omitempty"`
	MaxInputTokens int32  `json:"max_input_tokens,omitempty"`
	MaxBatchInputs int32  `json:"max_batch_inputs,omitempty"`
	MaxBatchTokens int32  `json:"max_batch_tokens,omitempty"`
}

type sandboxDoc struct {
	E2B                    *e2bDoc          `json:"e2b,omitempty"`
	Local                  *localSandboxDoc `json:"local,omitempty"`
	DefaultRunIn           string           `json:"default_run_in"`
	DefaultTimeoutSeconds  int32            `json:"default_timeout_seconds,omitempty"`
	DefaultPauseTTLSeconds *int32           `json:"default_pause_ttl_seconds,omitempty"`
	DefaultMaxTurns        *int32           `json:"default_max_turns,omitempty"`
	Setup                  []setupStepDoc   `json:"setup,omitempty"`
}

// localSandboxDoc is Crewlet's local backend as the operator renders it: the
// direct cell alone, with its boxes on the engine pod's sandboxes volume. Its
// other fields configure the container cell, which an engine pod cannot run
// and Crewlet refuses where no run is placed in it.
type localSandboxDoc struct {
	StateDir string `json:"state_dir"`
}

type e2bDoc struct {
	APIKey   string `json:"api_key"`
	Domain   string `json:"domain,omitempty"`
	Template string `json:"template,omitempty"`
}

type setupStepDoc struct {
	Name           string            `json:"name"`
	Files          map[string]string `json:"files,omitempty"`
	Commands       []string          `json:"commands,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	OptionalKeys   []string          `json:"optional_keys,omitempty"`
	Brief          string            `json:"brief,omitempty"`
	TimeoutSeconds int32             `json:"timeout_seconds,omitempty"`
}

type integrationsDoc struct {
	// Slack is a pointer because an empty block is meaningful to Crewlet:
	// `slack: {}` turns Slack on with nothing overridden.
	Slack                *slackDoc      `json:"slack,omitempty"`
	GitHub               *githubDoc     `json:"github,omitempty"`
	GitLab               *gitlabDoc     `json:"gitlab,omitempty"`
	Mattermost           *mattermostDoc `json:"mattermost,omitempty"`
	Jira                 *jiraDoc       `json:"jira,omitempty"`
	Confluence           *confluenceDoc `json:"confluence,omitempty"`
	Datadog              *datadogDoc    `json:"datadog,omitempty"`
	Atlassian            *atlassianDoc  `json:"atlassian,omitempty"`
	CheckIntervalSeconds int32          `json:"check_interval_seconds,omitempty"`
}

type gitlabDoc struct {
	Enabled       bool                   `json:"enabled"`
	URL           string                 `json:"url"`
	SigningSecret string                 `json:"signing_secret"`
	Token         string                 `json:"token,omitempty"`
	WebhookName   string                 `json:"webhook_name,omitempty"`
	Provisioning  *gitlabProvisioningDoc `json:"provisioning,omitempty"`
}

type gitlabProvisioningDoc struct {
	Group          string            `json:"group"`
	AdminToken     string            `json:"admin_token"`
	Mode           string            `json:"mode,omitempty"`
	AccessLevel    string            `json:"access_level,omitempty"`
	AccessLevels   map[string]string `json:"access_levels,omitempty"`
	UsernamePrefix string            `json:"username_prefix,omitempty"`
	Projects       []string          `json:"projects,omitempty"`
	GroupWebhook   string            `json:"group_webhook,omitempty"`
	TokenScopes    []string          `json:"token_scopes,omitempty"`
}

type mattermostDoc struct {
	Enabled      bool                       `json:"enabled"`
	URL          string                     `json:"url"`
	Team         string                     `json:"team"`
	TypingStatus string                     `json:"typing_status,omitempty"`
	Provisioning *mattermostProvisioningDoc `json:"provisioning,omitempty"`
}

type mattermostProvisioningDoc struct {
	AdminToken        string   `json:"admin_token"`
	UsernamePrefix    string   `json:"username_prefix,omitempty"`
	Channels          []string `json:"channels,omitempty"`
	DisplayNameSuffix string   `json:"display_name_suffix,omitempty"`
}

type jiraDoc struct {
	URL           string `json:"url,omitempty"`
	CloudID       string `json:"cloud_id,omitempty"`
	SiteURL       string `json:"site_url,omitempty"`
	Email         string `json:"email,omitempty"`
	Token         string `json:"token"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	WebhookName   string `json:"webhook_name,omitempty"`
}

type confluenceDoc struct {
	URL           string `json:"url,omitempty"`
	CloudID       string `json:"cloud_id,omitempty"`
	SiteURL       string `json:"site_url,omitempty"`
	Email         string `json:"email,omitempty"`
	Token         string `json:"token"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	WebhookToken  string `json:"webhook_token,omitempty"`
}

type datadogDoc struct {
	Enabled      bool                   `json:"enabled"`
	WebhookToken string                 `json:"webhook_token"`
	RouteTo      string                 `json:"route_to"`
	WebhookName  string                 `json:"webhook_name,omitempty"`
	HandleTag    string                 `json:"handle_tag,omitempty"`
	Provisioning datadogProvisioningDoc `json:"provisioning"`
}

type datadogProvisioningDoc struct {
	Site        string `json:"site"`
	APIKey      string `json:"api_key"`
	AppKey      string `json:"app_key"`
	Role        string `json:"role,omitempty"`
	EmailDomain string `json:"email_domain,omitempty"`
}

type atlassianDoc struct {
	OrgID  string `json:"org_id"`
	APIKey string `json:"api_key"`
}

type slackDoc struct {
	TypingStatus  string           `json:"typing_status,omitempty"`
	StatusPhrases statusPhrasesDoc `json:"status_phrases,omitzero"`
}

type statusPhrasesDoc struct {
	Onboarding []string `json:"onboarding,omitempty"`
	Execute    []string `json:"execute,omitempty"`
	Review     []string `json:"review,omitempty"`
	Default    []string `json:"default,omitempty"`
}

type githubDoc struct {
	Enabled       bool                   `json:"enabled"`
	URL           string                 `json:"url,omitempty"`
	WebhookSecret string                 `json:"webhook_secret"`
	Token         string                 `json:"token,omitempty"`
	Provisioning  *githubProvisioningDoc `json:"provisioning,omitempty"`
}

type githubProvisioningDoc struct {
	Org        string   `json:"org,omitempty"`
	Repos      []string `json:"repos,omitempty"`
	OrgWebhook string   `json:"org_webhook,omitempty"`
}

type mcpServerDoc struct {
	Name                  string                        `json:"name"`
	Transport             string                        `json:"transport"`
	Shared                *bool                         `json:"shared,omitempty"`
	Command               string                        `json:"command,omitempty"`
	Args                  []string                      `json:"args,omitempty"`
	Env                   map[string]string             `json:"env,omitempty"`
	URL                   string                        `json:"url,omitempty"`
	Headers               map[string]string             `json:"headers,omitempty"`
	OptionalKeys          []string                      `json:"optional_keys,omitempty"`
	ToolPrefix            string                        `json:"tool_prefix,omitempty"`
	ToolAnnotations       map[string]toolAnnotationsDoc `json:"tool_annotations,omitempty"`
	StartupTimeoutSeconds int32                         `json:"startup_timeout_seconds,omitempty"`
	RequestTimeoutSeconds int32                         `json:"request_timeout_seconds,omitempty"`
}

type toolAnnotationsDoc struct {
	ReadOnly    *bool `json:"read_only,omitempty"`
	Destructive *bool `json:"destructive,omitempty"`
	Idempotent  *bool `json:"idempotent,omitempty"`
	OpenWorld   *bool `json:"open_world,omitempty"`
}

type unitDoc struct {
	Name      string                       `json:"name"`
	ID        string                       `json:"id"`
	Type      string                       `json:"type,omitempty"`
	Purpose   string                       `json:"purpose,omitempty"`
	Lead      string                       `json:"lead,omitempty"`
	Goals     []string                     `json:"goals,omitempty"`
	Channel   string                       `json:"channel,omitempty"`
	Knowledge []string                     `json:"knowledge,omitempty"`
	MCPEnv    map[string]map[string]string `json:"mcp_env,omitempty"`
	Project   string                       `json:"project,omitempty"`
	Space     string                       `json:"space,omitempty"`
	Schedules []scheduleDoc                `json:"schedules,omitempty"`
	Roles     []roleDoc                    `json:"roles,omitempty"`
	Children  []unitDoc                    `json:"children,omitempty"`
}

type roleDoc struct {
	Name                 string                       `json:"name"`
	Handle               string                       `json:"handle"`
	Kind                 string                       `json:"kind"`
	Email                string                       `json:"email,omitempty"`
	Contact              map[string]string            `json:"contact,omitempty"`
	Availability         string                       `json:"availability,omitempty"`
	Goal                 string                       `json:"goal,omitempty"`
	Backstory            string                       `json:"backstory,omitempty"`
	Responsibilities     []string                     `json:"responsibilities,omitempty"`
	BehavioralGuidelines []string                     `json:"behavioral_guidelines,omitempty"`
	Manages              []string                     `json:"manages,omitempty"`
	Workers              []string                     `json:"workers,omitempty"`
	TokenBudget          tokenBudgetDoc               `json:"token_budget,omitzero"`
	LLM                  phaseLLMDoc                  `json:"llm,omitzero"`
	LearningEnabled      *bool                        `json:"learning_enabled,omitempty"`
	MCPEnv               map[string]map[string]string `json:"mcp_env,omitempty"`
	Sandbox              *roleSandboxDoc              `json:"sandbox,omitempty"`
	Placement            *placementDoc                `json:"placement,omitempty"`
	Integrations         roleIntegrationsDoc          `json:"integrations,omitzero"`
	Project              string                       `json:"project,omitempty"`
	Space                string                       `json:"space,omitempty"`
	Schedules            []scheduleDoc                `json:"schedules,omitempty"`
}

// scheduleDoc is one entry of a role's or a unit's schedules. Enabled and
// Catchup are pointers because Crewlet reads an absent toggle as its own
// default (true) and an explicit false as a setting.
type scheduleDoc struct {
	Name           string `json:"name"`
	Cron           string `json:"cron"`
	Task           string `json:"task"`
	Timezone       string `json:"timezone,omitempty"`
	Target         string `json:"target,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	TimeoutSeconds int32  `json:"timeout_seconds,omitempty"`
	Catchup        *bool  `json:"catchup,omitempty"`
}

// phaseLLMDoc is Crewlet's per-phase mapping form of a seat's llm field. The
// mapping is always used, even for a default chain alone, so that the shape
// of the document does not depend on how many phases a Seat sets.
type phaseLLMDoc struct {
	Default   []string `json:"default,omitempty"`
	Review    []string `json:"review,omitempty"`
	Subagent  []string `json:"subagent,omitempty"`
	Auxiliary []string `json:"auxiliary,omitempty"`
	Judge     []string `json:"judge,omitempty"`
}

type roleSandboxDoc struct {
	PauseTTLSeconds *int32            `json:"pause_ttl_seconds,omitempty"`
	MaxTurns        *int32            `json:"max_turns,omitempty"`
	MCP             *roleSandboxMCP   `json:"mcp,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	OptionalKeys    []string          `json:"optional_keys,omitempty"`
	Setup           []setupStepDoc    `json:"setup,omitempty"`
}

type roleSandboxMCP struct {
	Servers []string `json:"servers"`
}

type placementDoc struct {
	Labels map[string]string `json:"labels"`
}

type roleIntegrationsDoc struct {
	Slack      *roleSlackDoc      `json:"slack,omitempty"`
	GitHub     *roleGitHubDoc     `json:"github,omitempty"`
	Mattermost *roleMattermostDoc `json:"mattermost,omitempty"`
}

type roleMattermostDoc struct {
	BotToken string `json:"bot_token"`
	Username string `json:"username,omitempty"`
	Channel  string `json:"channel,omitempty"`
}

type roleSlackDoc struct {
	BotToken      string `json:"bot_token"`
	SigningSecret string `json:"signing_secret"`
}

type roleGitHubDoc struct {
	Tier           string   `json:"tier,omitempty"`
	Repos          []string `json:"repos,omitempty"`
	Workflows      *bool    `json:"workflows,omitempty"`
	AppID          int64    `json:"app_id,omitempty"`
	AppSlug        string   `json:"app_slug,omitempty"`
	InstallationID int64    `json:"installation_id,omitempty"`
	PrivateKey     string   `json:"private_key,omitempty"`
	WebhookSecret  string   `json:"webhook_secret,omitempty"`
}
