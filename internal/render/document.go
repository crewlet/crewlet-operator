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
	Inbox        inboxDoc        `json:"inbox,omitzero"`
	SignIn       signInDoc       `json:"sign_in,omitzero"`
	Atlas        atlasDoc        `json:"atlas,omitzero"`
	Diagnostics  diagnosticsDoc  `json:"diagnostics,omitzero"`
	Chat         chatDoc         `json:"chat,omitzero"`
	TokenBudget  tokenBudgetDoc  `json:"token_budget,omitzero"`
	Assistant    assistantDoc    `json:"assistant,omitzero"`
	Providers    providersDoc    `json:"providers,omitzero"`
	Integrations integrationsDoc `json:"integrations,omitzero"`
	MCPRegistry  *mcpRegistryDoc `json:"mcp_registry,omitempty"`
	MCPServers   []mcpServerDoc  `json:"mcp_servers,omitempty"`
	Roles        []roleDoc       `json:"roles,omitempty"`
	Units        []unitDoc       `json:"units,omitempty"`
	// SeatAccessMigrated is every integration kind whose per-seat access
	// the document states in the seats' own blocks (seatAccessKinds).
	SeatAccessMigrated []string `json:"seat_access_migrated,omitempty"`
}

// mcpRegistryDoc is the community MCP registry, from spec.mcpRegistry.
type mcpRegistryDoc struct {
	URL string `json:"url"`
}

type trackerDoc struct {
	Backend string `json:"backend,omitempty"`
}

type knowledgeDoc struct {
	Backend         string   `json:"backend,omitempty"`
	Scope           []string `json:"scope,omitempty"`
	SkillsContainer *string  `json:"skills_container,omitempty"`
	RootSpace       string   `json:"root_space,omitempty"`
	Vectors         *bool    `json:"vectors,omitempty"`
}

// signInDoc is the company's sign-in policy; each zero is left out, so the
// engine's default stands.
type signInDoc struct {
	RequireSecondFactor    *bool `json:"require_second_factor,omitempty"`
	MinPasswordLength      int32 `json:"min_password_length,omitempty"`
	SessionLifetimeSeconds int32 `json:"session_lifetime_seconds,omitempty"`
	IdleTimeoutSeconds     int32 `json:"idle_timeout_seconds,omitempty"`
	StepUpSeconds          int32 `json:"step_up_seconds,omitempty"`
}

type inboxDoc struct {
	RetentionDays int32 `json:"retention_days,omitempty"`
}

type atlasDoc struct {
	RetentionDays          int32 `json:"retention_days,omitempty"`
	RefreshIntervalSeconds int32 `json:"refresh_interval_seconds,omitempty"`
	AgentRecords           *bool `json:"agent_records,omitempty"`
	OwnerWakes             *bool `json:"owner_wakes,omitempty"`
}

type diagnosticsDoc struct {
	IssueReports issueReportsDoc `json:"issue_reports,omitzero"`
}

type issueReportsDoc struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type chatDoc struct {
	Backend string      `json:"backend,omitempty"`
	Chatlet *chatletDoc `json:"chatlet,omitempty"`
}

type chatletDoc struct {
	MessageRetentionDays  *int32 `json:"message_retention_days,omitempty"`
	DefaultChannelPrivate *bool  `json:"default_channel_private,omitempty"`
	TypingStatus          string `json:"typing_status,omitempty"`
}

type tokenBudgetDoc struct {
	Day   int64 `json:"day,omitempty"`
	Week  int64 `json:"week,omitempty"`
	Month int64 `json:"month,omitempty"`
}

type assistantDoc struct {
	Model       string         `json:"model,omitempty"`
	TokenBudget tokenBudgetDoc `json:"token_budget,omitzero"`
}

type providersDoc struct {
	LLM        map[string]llmProviderDoc `json:"llm,omitempty"`
	LLMOrder   []string                  `json:"llm_order,omitempty"`
	LLMFleets  map[string]llmFleetDoc    `json:"llm_fleets,omitempty"`
	Embeddings *embeddingsDoc            `json:"embeddings,omitempty"`
	Sandbox    *sandboxDoc               `json:"sandbox,omitempty"`
}

type llmFleetDoc struct {
	Name            string   `json:"name"`
	Strategy        string   `json:"strategy,omitempty"`
	Members         []string `json:"members,omitempty"`
	ThresholdPoints int32    `json:"threshold_points,omitempty"`
}

type llmProviderDoc struct {
	Harness        string            `json:"harness"`
	Model          string            `json:"model"`
	StateDir       string            `json:"state_dir"`
	TimeoutSeconds int32             `json:"timeout_seconds,omitempty"`
	MaxConcurrent  int32             `json:"max_concurrent,omitempty"`
	Effort         string            `json:"effort,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Auth           llmAuthDoc        `json:"auth,omitzero"`
	Overrides      *llmOverridesDoc  `json:"overrides,omitempty"`
}

// llmOverridesDoc is the part of a harness profile's overrides the API sets:
// the web stance alone. Every other override names the harness's binary and
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
	Local                  *localSandboxDoc `json:"local"`
	DefaultRunIn           string           `json:"default_run_in"`
	HostGraceSeconds       int32            `json:"host_grace_seconds,omitempty"`
	DefaultPauseTTLSeconds *int32           `json:"default_pause_ttl_seconds,omitempty"`
	DefaultMaxTurns        *int32           `json:"default_max_turns,omitempty"`
	CommitAttribution      *bool            `json:"commit_attribution,omitempty"`
	Setup                  []setupStepDoc   `json:"setup,omitempty"`
}

// localSandboxDoc is Crewlet's local backend as the operator renders it: the
// direct cell alone, with its boxes on the engine pod's sandboxes volume. Its
// other fields configure the container cell, which an engine pod cannot run.
type localSandboxDoc struct {
	StateDir string `json:"state_dir"`
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
	Slack                *slackDoc           `json:"slack,omitempty"`
	GitHub               *githubDoc          `json:"github,omitempty"`
	GitLab               *gitlabDoc          `json:"gitlab,omitempty"`
	Mattermost           *mattermostDoc      `json:"mattermost,omitempty"`
	Jira                 *jiraDoc            `json:"jira,omitempty"`
	Confluence           *confluenceDoc      `json:"confluence,omitempty"`
	Datadog              *datadogDoc         `json:"datadog,omitempty"`
	Cloudflare           *cloudflareDoc      `json:"cloudflare,omitempty"`
	Expo                 *expoDoc            `json:"expo,omitempty"`
	DigitalOcean         *digitaloceanDoc    `json:"digitalocean,omitempty"`
	Scaleway             *scalewayDoc        `json:"scaleway,omitempty"`
	AWS                  *awsDoc             `json:"aws,omitempty"`
	GoogleCloud          *googleCloudDoc     `json:"google_cloud,omitempty"`
	GoogleAnalytics      *googleAnalyticsDoc `json:"google_analytics,omitempty"`
	AdMob                *adMobDoc           `json:"admob,omitempty"`
	AdSense              *adSenseDoc         `json:"adsense,omitempty"`
	AppStoreConnect      *appStoreConnectDoc `json:"app_store_connect,omitempty"`
	AppleAds             *appleAdsDoc        `json:"apple_ads,omitempty"`
	MetaAds              *metaAdsDoc         `json:"meta_ads,omitempty"`
	GoogleAds            *googleAdsDoc       `json:"google_ads,omitempty"`
	SearchConsole        *searchConsoleDoc   `json:"search_console,omitempty"`
	GooglePlay           *googlePlayDoc      `json:"google_play,omitempty"`
	Atlassian            *atlassianDoc       `json:"atlassian,omitempty"`
	CheckIntervalSeconds int32               `json:"check_interval_seconds,omitempty"`
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
	Group          string   `json:"group"`
	AdminToken     string   `json:"admin_token"`
	Mode           string   `json:"mode,omitempty"`
	UsernamePrefix string   `json:"username_prefix,omitempty"`
	Projects       []string `json:"projects,omitempty"`
	GroupWebhook   string   `json:"group_webhook,omitempty"`
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
	EmailDomain string `json:"email_domain,omitempty"`
}

type cloudflareDoc struct {
	Enabled      bool                      `json:"enabled"`
	Provisioning cloudflareProvisioningDoc `json:"provisioning"`
}

type cloudflareProvisioningDoc struct {
	AccountID string   `json:"account_id"`
	APIToken  string   `json:"api_token"`
	Zones     []string `json:"zones,omitempty"`
}

type expoDoc struct {
	Enabled      bool                `json:"enabled"`
	Provisioning expoProvisioningDoc `json:"provisioning"`
	Atlas        *atlasTokenDoc      `json:"atlas,omitempty"`
}

// atlasTokenDoc is the atlas's own credential at a vendor whose company half
// holds none: Expo's and DigitalOcean's `atlas` block.
type atlasTokenDoc struct {
	Token string `json:"token"`
}

type expoProvisioningDoc struct {
	AccountName string `json:"account_name"`
}

type digitaloceanDoc struct {
	Enabled      bool                        `json:"enabled"`
	Provisioning digitaloceanProvisioningDoc `json:"provisioning"`
	Atlas        *atlasTokenDoc              `json:"atlas,omitempty"`
}

type digitaloceanProvisioningDoc struct {
	Team string `json:"team"`
}

type scalewayDoc struct {
	Enabled      bool                    `json:"enabled"`
	Provisioning scalewayProvisioningDoc `json:"provisioning"`
	Atlas        bool                    `json:"atlas,omitempty"`
}

type scalewayProvisioningDoc struct {
	OrganizationID string `json:"organization_id"`
	AccessKey      string `json:"access_key"`
	SecretKey      string `json:"secret_key"`
}

type awsDoc struct {
	Enabled      bool               `json:"enabled"`
	Provisioning awsProvisioningDoc `json:"provisioning"`
}

type awsProvisioningDoc struct {
	Slug                 string   `json:"slug"`
	HomeRegion           string   `json:"home_region"`
	Regions              []string `json:"regions,omitempty"`
	IAMRead              bool     `json:"iam_read,omitempty"`
	AtlasRead            bool     `json:"atlas_read,omitempty"`
	CertificateAuthority string   `json:"certificate_authority"`
	Connection           string   `json:"connection,omitempty"`
}

type googleCloudDoc struct {
	Enabled      bool                       `json:"enabled"`
	Provisioning googleCloudProvisioningDoc `json:"provisioning"`
}

type googleCloudProvisioningDoc struct {
	Provisioner   string `json:"provisioner"`
	Provider      string `json:"provider,omitempty"`
	AtlasRead     bool   `json:"atlas_read,omitempty"`
	FederationKey string `json:"federation_key"`
}

// googleAnalyticsDoc holds no credential: the seats sign in as the growth
// identities the Google Cloud connection makes them.
type googleAnalyticsDoc struct {
	Enabled    bool                  `json:"enabled"`
	Properties []string              `json:"properties,omitempty"`
	OAuth      *googleOAuthClientDoc `json:"oauth,omitempty"`
	Approvers  []string              `json:"approvers,omitempty"`
}

// adMobDoc never holds the grant a person signs in with, which the engine
// seals itself: its one credential is the OAuth client's secret.
type adMobDoc struct {
	Enabled   bool                 `json:"enabled"`
	OAuth     googleOAuthClientDoc `json:"oauth"`
	Accounts  []string             `json:"accounts,omitempty"`
	Approvers []string             `json:"approvers,omitempty"`
}

// adSenseDoc is adMobDoc's shape: the grant stays in the engine, and its
// OAuth client may be AdMob's own, secret and all.
type adSenseDoc struct {
	Enabled   bool                 `json:"enabled"`
	OAuth     googleOAuthClientDoc `json:"oauth"`
	Accounts  []string             `json:"accounts,omitempty"`
	Approvers []string             `json:"approvers,omitempty"`
}

type googleOAuthClientDoc struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type appStoreConnectDoc struct {
	Enabled     bool                              `json:"enabled"`
	Approvers   []string                          `json:"approvers,omitempty"`
	Connections map[string]appStoreConnectTeamDoc `json:"connections"`
}

type appStoreConnectTeamDoc struct {
	IssuerID string `json:"issuer_id"`
}

// appleAdsDoc holds each API user's private key as a reference, the one
// credential position of the block: no seat holds a key or a token.
type appleAdsDoc struct {
	Enabled     bool                             `json:"enabled"`
	Approvers   []string                         `json:"approvers,omitempty"`
	AdAccounts  []string                         `json:"ad_accounts,omitempty"`
	Connections map[string]appleAdsConnectionDoc `json:"connections"`
	Spend       *growthSpendDoc                  `json:"spend,omitempty"`
}

type appleAdsConnectionDoc struct {
	OrgID      string `json:"org_id,omitempty"`
	ClientID   string `json:"client_id"`
	TeamID     string `json:"team_id"`
	KeyID      string `json:"key_id"`
	PrivateKey string `json:"private_key"`
}

type growthSpendDoc struct {
	Accounts []growthCapDoc `json:"accounts"`
}

type growthCapDoc struct {
	Account   string `json:"account"`
	Currency  string `json:"currency,omitempty"`
	PerChange string `json:"per_change,omitempty"`
	Daily     string `json:"daily,omitempty"`
}

// metaAdsDoc holds the app secret and the system user's tokens as
// references, the block's credential positions: no seat holds a token.
type metaAdsDoc struct {
	Enabled              bool            `json:"enabled"`
	Approvers            []string        `json:"approvers,omitempty"`
	AppID                string          `json:"app_id"`
	AppSecret            string          `json:"app_secret"`
	ReadToken            string          `json:"read_token"`
	OperateToken         string          `json:"operate_token,omitempty"`
	AdminToken           string          `json:"admin_token,omitempty"`
	AdminSystemUserToken string          `json:"admin_system_user_token,omitempty"`
	BusinessID           string          `json:"business_id,omitempty"`
	AdAccounts           []string        `json:"ad_accounts,omitempty"`
	Spend                *growthSpendDoc `json:"spend,omitempty"`
}

// googleAdsDoc holds no credential: the seats sign in as the growth
// identities the Google Cloud connection makes them.
type googleAdsDoc struct {
	Enabled   bool                   `json:"enabled"`
	Customers []googleAdsCustomerDoc `json:"customers,omitempty"`
	Approvers []string               `json:"approvers,omitempty"`
	Spend     *growthSpendDoc        `json:"spend,omitempty"`
}

type googleAdsCustomerDoc struct {
	ID      string `json:"id"`
	Manager string `json:"manager,omitempty"`
}

// searchConsoleDoc holds no credential: the seats sign in as the growth
// identities the Google Cloud connection makes them.
type searchConsoleDoc struct {
	Enabled   bool     `json:"enabled"`
	Sites     []string `json:"sites,omitempty"`
	Approvers []string `json:"approvers,omitempty"`
}

// googlePlayDoc holds no credential: the seats sign in as the growth
// identities the Google Cloud connection makes them.
type googlePlayDoc struct {
	Enabled           bool                  `json:"enabled"`
	Apps              []string              `json:"apps,omitempty"`
	DeveloperAccounts []string              `json:"developer_accounts,omitempty"`
	ReportsBuckets    []string              `json:"reports_buckets,omitempty"`
	OAuth             *googleOAuthClientDoc `json:"oauth,omitempty"`
	Approvers         []string              `json:"approvers,omitempty"`
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
	Enabled               *bool                         `json:"enabled,omitempty"`
	Description           string                        `json:"description,omitempty"`
	Transport             string                        `json:"transport"`
	Shared                *bool                         `json:"shared,omitempty"`
	Access                *mcpAccessDoc                 `json:"access,omitempty"`
	Command               string                        `json:"command,omitempty"`
	Args                  []string                      `json:"args,omitempty"`
	Env                   map[string]string             `json:"env,omitempty"`
	URL                   string                        `json:"url,omitempty"`
	Headers               map[string]string             `json:"headers,omitempty"`
	OAuth                 *mcpOAuthDoc                  `json:"oauth,omitempty"`
	OptionalKeys          []string                      `json:"optional_keys,omitempty"`
	ToolPrefix            string                        `json:"tool_prefix,omitempty"`
	SeatCredentials       []seatCredentialDoc           `json:"seat_credentials,omitempty"`
	ToolAnnotations       map[string]toolAnnotationsDoc `json:"tool_annotations,omitempty"`
	Tools                 *toolsDoc                     `json:"tools,omitempty"`
	StartupTimeoutSeconds int32                         `json:"startup_timeout_seconds,omitempty"`
	RequestTimeoutSeconds int32                         `json:"request_timeout_seconds,omitempty"`
}

// mcpAccessDoc is which agent seats a server reaches: units by their keys,
// seats by their handles. A pointer in mcpServerDoc, because Crewlet reads
// `access: {}` as reaching nobody and an absent one as reaching every agent
// seat.
type mcpAccessDoc struct {
	Units  []string `json:"units,omitempty"`
	Seats  []string `json:"seats,omitempty"`
	Except []string `json:"except,omitempty"`
}

// mcpOAuthDoc is how the engine signs the company in to a server with OAuth.
// Its client_secret is the pointer to a Secret key's store entry, never a
// value. grant is left out where the MCPServer gives none, which Crewlet
// reads as a person's sign-in, and client is left out of a service identity,
// which Crewlet refuses it on.
type mcpOAuthDoc struct {
	Grant        string   `json:"grant,omitempty"`
	Client       string   `json:"client,omitempty"`
	ClientID     string   `json:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// seatCredentialDoc is one per-seat credential a server reads. Its as is left
// out: Crewlet takes the server's transport's, which is the only value it
// accepts.
type seatCredentialDoc struct {
	Kind   string `json:"kind"`
	Field  string `json:"field,omitempty"`
	Key    string `json:"key"`
	Prefix string `json:"prefix,omitempty"`
}

// toolsDoc is the level each tool of a server runs at, levels keyed by the
// server's own name for the tool, and whether its tool changes are accepted
// without a review.
type toolsDoc struct {
	Default           string            `json:"default,omitempty"`
	Levels            map[string]string `json:"levels,omitempty"`
	AutoAcceptChanges bool              `json:"auto_accept_changes,omitempty"`
}

type toolAnnotationsDoc struct {
	ReadOnly    *bool `json:"read_only,omitempty"`
	Destructive *bool `json:"destructive,omitempty"`
	Idempotent  *bool `json:"idempotent,omitempty"`
	OpenWorld   *bool `json:"open_world,omitempty"`
}

// mcpLimitDoc is one entry of a role's or a unit's mcp_limits: the server
// turned off (Enabled false; nil where the limit leaves it on), and each tool
// held back by the server's own name for it.
type mcpLimitDoc struct {
	Enabled *bool             `json:"enabled,omitempty"`
	Tools   map[string]string `json:"tools,omitempty"`
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
	MCPLimits map[string]mcpLimitDoc       `json:"mcp_limits,omitempty"`
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
	MCPLimits            map[string]mcpLimitDoc       `json:"mcp_limits,omitempty"`
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
	Triage    []string `json:"triage,omitempty"`
}

type roleSandboxDoc struct {
	PauseTTLSeconds   *int32            `json:"pause_ttl_seconds,omitempty"`
	MaxTurns          *int32            `json:"max_turns,omitempty"`
	MaxConcurrentRuns *int32            `json:"max_concurrent_runs,omitempty"`
	MCP               *roleSandboxMCP   `json:"mcp,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	OptionalKeys      []string          `json:"optional_keys,omitempty"`
	Setup             []setupStepDoc    `json:"setup,omitempty"`
}

type roleSandboxMCP struct {
	Servers []string `json:"servers"`
}

type placementDoc struct {
	Labels map[string]string `json:"labels"`
}

// roleIntegrationsDoc is a seat's blocks of Crewlet's role integrations.
// Each block carries enabled whatever it says, since Crewlet requires it in
// every block: a block is a stored setting, never inferred from what else the
// seat holds.
type roleIntegrationsDoc struct {
	Slack           *roleSlackDoc           `json:"slack,omitempty"`
	GitHub          *roleGitHubDoc          `json:"github,omitempty"`
	GitLab          *roleGitLabDoc          `json:"gitlab,omitempty"`
	Mattermost      *roleMattermostDoc      `json:"mattermost,omitempty"`
	Jira            *roleJiraDoc            `json:"jira,omitempty"`
	Confluence      *roleConfluenceDoc      `json:"confluence,omitempty"`
	Datadog         *roleDatadogDoc         `json:"datadog,omitempty"`
	Cloudflare      *roleCloudflareDoc      `json:"cloudflare,omitempty"`
	Expo            *roleExpoDoc            `json:"expo,omitempty"`
	DigitalOcean    *roleDigitalOceanDoc    `json:"digitalocean,omitempty"`
	Scaleway        *roleScalewayDoc        `json:"scaleway,omitempty"`
	AWS             *roleAWSDoc             `json:"aws,omitempty"`
	GoogleCloud     *roleGoogleCloudDoc     `json:"google_cloud,omitempty"`
	GoogleAnalytics *roleGoogleAnalyticsDoc `json:"google_analytics,omitempty"`
	AdMob           *roleAdMobDoc           `json:"admob,omitempty"`
	AdSense         *roleAdSenseDoc         `json:"adsense,omitempty"`
	AppStoreConnect *roleAppStoreConnectDoc `json:"app_store_connect,omitempty"`
	AppleAds        *roleAppleAdsDoc        `json:"apple_ads,omitempty"`
	MetaAds         *roleMetaAdsDoc         `json:"meta_ads,omitempty"`
	GoogleAds       *roleGoogleAdsDoc       `json:"google_ads,omitempty"`
	SearchConsole   *roleSearchConsoleDoc   `json:"search_console,omitempty"`
	GooglePlay      *roleGooglePlayDoc      `json:"google_play,omitempty"`
}

type roleSlackDoc struct {
	Enabled       bool   `json:"enabled"`
	BotToken      string `json:"bot_token"`
	SigningSecret string `json:"signing_secret"`
}

type roleGitHubDoc struct {
	Enabled        bool              `json:"enabled"`
	Permissions    map[string]string `json:"permissions,omitempty"`
	Repos          []string          `json:"repos,omitempty"`
	AppID          int64             `json:"app_id,omitempty"`
	AppSlug        string            `json:"app_slug,omitempty"`
	InstallationID int64             `json:"installation_id,omitempty"`
	PrivateKey     string            `json:"private_key,omitempty"`
	WebhookSecret  string            `json:"webhook_secret,omitempty"`
}

type roleGitLabDoc struct {
	Enabled      bool     `json:"enabled"`
	AccessLevel  int32    `json:"access_level,omitempty"`
	MemberRoleID int64    `json:"member_role_id,omitempty"`
	Projects     []string `json:"projects,omitempty"`
}

type roleMattermostDoc struct {
	Enabled  bool     `json:"enabled"`
	Roles    []string `json:"roles,omitempty"`
	Channels []string `json:"channels,omitempty"`
	BotToken string   `json:"bot_token,omitempty"`
	Username string   `json:"username,omitempty"`
}

type roleJiraDoc struct {
	Enabled     bool     `json:"enabled"`
	ProjectRole string   `json:"project_role,omitempty"`
	Projects    []string `json:"projects,omitempty"`
}

type roleConfluenceDoc struct {
	Enabled    bool     `json:"enabled"`
	SpaceRole  string   `json:"space_role,omitempty"`
	Operations []string `json:"operations,omitempty"`
	Spaces     []string `json:"spaces,omitempty"`
}

type roleDatadogDoc struct {
	Enabled bool   `json:"enabled"`
	Role    string `json:"role,omitempty"`
}

type roleCloudflareDoc struct {
	Enabled bool   `json:"enabled"`
	Level   string `json:"level,omitempty"`
}

type roleExpoDoc struct {
	Enabled bool   `json:"enabled"`
	Level   string `json:"level,omitempty"`
}

type roleDigitalOceanDoc struct {
	Enabled bool   `json:"enabled"`
	Level   string `json:"level,omitempty"`
}

type roleScalewayDoc struct {
	Enabled  bool     `json:"enabled"`
	Level    string   `json:"level,omitempty"`
	Projects []string `json:"projects,omitempty"`
}

type roleAWSDoc struct {
	Enabled bool   `json:"enabled"`
	Level   string `json:"level,omitempty"`
}

type roleGoogleCloudDoc struct {
	Enabled  bool     `json:"enabled"`
	Level    string   `json:"level,omitempty"`
	Projects []string `json:"projects,omitempty"`
}

type roleGoogleAnalyticsDoc struct {
	Enabled    bool     `json:"enabled"`
	Level      string   `json:"level,omitempty"`
	Role       string   `json:"role,omitempty"`
	Properties []string `json:"properties,omitempty"`
}

type roleAdMobDoc struct {
	Enabled  bool     `json:"enabled"`
	Level    string   `json:"level,omitempty"`
	Accounts []string `json:"accounts,omitempty"`
}

type roleAdSenseDoc struct {
	Enabled  bool     `json:"enabled"`
	Level    string   `json:"level,omitempty"`
	Accounts []string `json:"accounts,omitempty"`
}

type roleAppStoreConnectDoc struct {
	Enabled       bool     `json:"enabled"`
	Level         string   `json:"level,omitempty"`
	Apps          []string `json:"apps,omitempty"`
	TeamResources bool     `json:"team_resources,omitempty"`
}

type roleAppleAdsDoc struct {
	Enabled    bool     `json:"enabled"`
	Level      string   `json:"level,omitempty"`
	AdAccounts []string `json:"ad_accounts,omitempty"`
}

type roleMetaAdsDoc struct {
	Enabled           bool     `json:"enabled"`
	Level             string   `json:"level,omitempty"`
	AdAccounts        []string `json:"ad_accounts,omitempty"`
	BusinessResources bool     `json:"business_resources,omitempty"`
	Identity          string   `json:"identity,omitempty"`
}

type roleGoogleAdsDoc struct {
	Enabled   bool     `json:"enabled"`
	Level     string   `json:"level,omitempty"`
	Customers []string `json:"customers,omitempty"`
}

type roleSearchConsoleDoc struct {
	Enabled bool     `json:"enabled"`
	Level   string   `json:"level,omitempty"`
	Sites   []string `json:"sites,omitempty"`
}

type roleGooglePlayDoc struct {
	Enabled bool     `json:"enabled"`
	Level   string   `json:"level,omitempty"`
	Apps    []string `json:"apps,omitempty"`
}
