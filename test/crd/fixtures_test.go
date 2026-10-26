package crd_test

import (
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Names the fixtures share. The Secrets they name never exist: the API server
// validates a reference's shape, and whether the Secret is there is the
// operator's question, reported in an object's status.
const (
	companyName   = "nimbus"
	claudeModel   = "claude-sonnet-5"
	providerName  = "anthropic"
	unitName      = "platform"
	seatHandle    = "ana"
	apiKeyKey     = "apiKey"
	webhookHost   = "{{uid}}.hooks.example.com"
	engineImage   = "ghcr.io/crewlet/crewlet:v0.1.0"
	tenantsBucket = "crewlet-tenants"
	tenantsRegion = "eu-central-1"
	githubKeys    = "nimbus-github"
	githubServer  = "https://api.githubcopilot.com/mcp/"
	platformKeyNS = "crewlet-system"
	containerKey  = "PLAT"
	poolName      = "general"
	// whiteSpace is a value of white space alone, which every rule that
	// trims refuses as empty.
	whiteSpace = " \t "
)

// Models the OpenCode cases name, which OpenCode reads as <provider>/<model>:
// Anthropic's model on Anthropic's key, and the same model through
// OpenRouter.
const (
	modelViaAnthropic  = "anthropic/claude-sonnet-5"
	modelViaOpenRouter = "openrouter/anthropic/claude-sonnet-5"
)

// Environment variables the provider cases name: a model provider's key
// some CLI's profile reads, a proxy, which is no credential, and the address
// of a gateway Claude Code reads from its environment, which is none either.
const (
	envAnthropicKey = "ANTHROPIC_API_KEY"
	envOpenAIKey    = "OPENAI_API_KEY"
	envProxy        = "HTTPS_PROXY"
	envBaseURL      = "ANTHROPIC_BASE_URL"
)

// cliVariable is the variable of the headless token and of the key Crewlet's
// profile for a CLI names (token_env, api_key_env), empty where it names none.
type cliVariable struct{ headless, key string }

// cliVariables are the variables of every CLI whose profile names one in the
// engine every suite runs, the ones the LLMProvider API's own-variable rule lists.
// TestCLIAgentSignInFollowsTheEngine reads them from the engine and fails
// while this copy, which the rule's cases are built from, differs.
var cliVariables = map[v1.CLIAgentName]cliVariable{
	v1.CLIAgentClaudeCode:  {headless: "CLAUDE_CODE_OAUTH_TOKEN", key: envAnthropicKey},
	v1.CLIAgentCodex:       {key: "CODEX_API_KEY"},
	v1.CLIAgentGeminiCLI:   {key: "GEMINI_API_KEY"},
	v1.CLIAgentQwenCode:    {key: envOpenAIKey},
	v1.CLIAgentCursorAgent: {key: "CURSOR_API_KEY"},
	v1.CLIAgentCopilot:     {key: "COPILOT_GITHUB_TOKEN"},
	v1.CLIAgentGrok:        {key: "XAI_API_KEY"},
	v1.CLIAgentMuseCode:    {key: "META_API_KEY"},
}

// envSignIn are the CLIs Crewlet's profiles mark env_sign_in in the engine
// every suite runs: a credential-named variable in env signs them in.
// TestCLIAgentSignInFollowsTheEngine holds it to the engine too.
var envSignIn = []v1.CLIAgentName{v1.CLIAgentHermes, v1.CLIAgentPi, v1.CLIAgentOpenCode}

// homeIsolation are the variables Crewlet sets on every call of every CLI to
// keep it inside its seat's own home (cliprofile.HomeIsolation), and
// cliIsolation those each CLI's profile adds for its own configuration
// directory or switch (config_env, work_config_env, isolation_env), in the
// engine every suite runs: the variables the LLMProvider API's isolation rule
// refuses in env. TestCLIAgentIsolationFollowsTheEngine reads them from the
// engine's refusals and fails while this copy, which the rule's cases are
// built from, differs.
var (
	homeIsolation = []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "TMPDIR"}
	cliIsolation  = map[v1.CLIAgentName][]string{
		v1.CLIAgentClaudeCode:  {"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_DISABLE_CLAUDE_MDS", "CLAUDE_CODE_DISABLE_AUTO_MEMORY"},
		v1.CLIAgentCodex:       {"CODEX_HOME", "CODEX_SQLITE_HOME"},
		v1.CLIAgentOpenCode:    {"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_PROJECT_DISABLE"},
		v1.CLIAgentCursorAgent: {"CURSOR_CONFIG_DIR"},
		v1.CLIAgentKimiCode:    {"KIMI_CODE_HOME"},
		v1.CLIAgentHermes:      {"HERMES_HOME"},
		v1.CLIAgentPi:          {"PI_CODING_AGENT_DIR"},
	}
)

// subscriptionEnv are the variables each CLI's profile sets under auth.mode
// subscription alone, after env, to point it at the login its seat's home
// holds (subscription_env), in the engine every suite runs: the variables the
// LLMProvider API's subscription rule refuses in env under Subscription.
// TestCLIAgentSubscriptionEnvFollowsTheEngine reads them from the engine's
// profiles and fails while this copy, which the rule's cases are built from,
// differs.
var subscriptionEnv = map[v1.CLIAgentName][]string{
	v1.CLIAgentGeminiCLI: {"GOOGLE_GENAI_USE_GCA"},
}

// The kinds the tests that apply manifests treat specially.
const (
	kindEngineClass = "EngineClass"
	kindCompany     = "Company"
)

// The messages of the rules more than one case checks. Each is the message
// declared on the rule, or the start of it.
const (
	msgGatewayMode                 = "gateway is required with mode Gateway and not allowed otherwise"
	msgIngressMode                 = "ingress is required with mode Ingress and not allowed otherwise"
	msgIngressControllerNamespaces = "networkPolicy Isolated with an Ingress needs ingress.controllerNamespaces"
	msgHostnameMode                = "hostname is required with modes Gateway and Ingress and not allowed otherwise"
	msgHostnameVars                = "hostname may use only the variables"
	msgHostnameUnique              = "hostname must contain {{uid}}, or {{name}} and {{namespace}} joined by a '.'"
	msgHostnameDNS                 = "hostname must be a lowercase DNS name once its variables are replaced"
	msgPrefixUnique                = "prefix must contain {{uid}}, or {{namespace}} and {{name}} joined by a '/' or '.'"
	msgPrefixSlash                 = "prefix must end with '/'"
	msgS3Backend                   = "s3 is required with backend S3 and not allowed otherwise"
	msgReservedLabels              = "labels under crewlet.ai/ and app.kubernetes.io/ are the operator's own"
	msgBaseURLRequired             = "baseURL is required for type OpenAICompatible"
	msgBaseURLShape                = "baseURL must be an absolute http or https URL"
	msgCompanyName                 = "a Company's name must be a DNS label of at most 47 characters"
	msgMissionLength               = "mission can be at most 1024 characters"
	msgMissionLine                 = "mission is one line"
	msgVisionLength                = "vision can be at most 2048 characters"
	msgPolicyCount                 = "a company can keep at most 25 policies"
	msgPolicyBlank                 = "a policy cannot be blank"
	msgPolicyLength                = "a policy can be at most 1024 characters"
	msgPolicyLine                  = "a policy is one line"
	msgPolicyRepeat                = "policies must not repeat one another"
	msgEnvExactlyOne               = "exactly one of value and valueFrom must be set"
	msgLiteralCredential           = "value may not be given to a name that says it holds a credential"
	msgLiteralReference            = "value may not hold a ${NAME} reference"
	msgEnvReserved                 = "variables starting with CREWLET_ or OTEL_ are set by the operator"
	msgPublicBaseURL               = "publicBaseURL must be an absolute URL starting with https://"
	msgDashboardBaseURL            = "dashboardBaseURL must be an absolute URL starting with http:// or https://"
	msgPublicBaseURLQuery          = "publicBaseURL may not have a query or a fragment"
	msgDashboardBaseURLQuery       = "dashboardBaseURL may not have a query or a fragment"
	msgDashboardBaseURLPort        = "dashboardBaseURL's port must be 1 to 65535"
	msgPublicBaseURLPort           = "publicBaseURL's port must be 1 to 65535"
	msgDashboardBaseURLASCII       = "dashboardBaseURL's host must be ASCII"
	msgDashboardBaseURLPercent     = "dashboardBaseURL's host may not hold a '%'"
	msgDashboardBaseURLNumber      = "dashboardBaseURL's host ends in a number"
	msgDashboardBaseURLHost        = "dashboardBaseURL's host must be a name, an IPv4 address or an IPv6 address"
	msgPublicBaseURLHost           = "publicBaseURL's host must be a name, an IPv4 address or an IPv6 address"
	msgHostnameNumber              = "hostname may not end in a label that is a number"
	msgS3RegionBlank               = "region may not be white space alone"
	msgS3BucketBlank               = "bucket may not be white space alone"
	msgS3Region                    = "region must be letters, digits and '-', in labels of at most 63"
	msgS3RegionDot                 = "region may not hold a '.' unless pathStyle is true"
	msgS3RegionFIPSEndpoint        = "region names a FIPS endpoint (fips- or -fips in it)"
	msgS3RegionFIPSChina           = "region names a FIPS endpoint in Amazon's China regions"
	msgS3Bucket                    = "bucket must name a general purpose bucket"
	msgS3EndpointQuery             = "endpoint may not have a query"
	msgS3EndpointHost              = "endpoint's host must be a name, an IPv4 address or an IPv6 address in brackets"
	msgS3EndpointPort              = "endpoint's port must be 1 to 65535"
	msgUnitID                      = "a Unit's name is its Crewlet unit id"
	msgDuplicate                   = "Duplicate value"
	msgThreeReplicas               = "Unsupported value: 3"
	msgStorageFloor                = "size must be at least 9Gi"
	msgStorageFloorNATS            = "data.storage.size must be at least 12Gi with objects.backend NATS"
	msgStartupFloorS3              = "data.startupSeconds must be at least 1886 with objects.backend S3"
	msgSetupFileTilde              = "path may not start with ~ followed by a name, such as ~root"
	msgSetupFileBlank              = "path may not be white space alone"
	msgSetupStepName               = "name may not be white space alone"
	msgSetupStepEmpty              = "a setup step must give files, commands, env or a brief"
	msgHeaderCase                  = "names must differ in more than case: HTTP header names are case-insensitive"
	msgContainerKey                = `should match '^[A-Z][A-Z0-9]{1,9}$'`
	msgProviderKeyEnv              = "ANTHROPIC_API_KEY and OPENAI_API_KEY are model provider keys"
	msgBlankModel                  = "model may not be white space alone"
	msgBlankEmbeddingModel         = "refuses an embeddings provider that names no model"
	msgSeatName                    = "without spec.handle the Seat's name is its handle"
	msgHandleImmutable             = "the seat's handle is immutable"
	msgAgentContact                = "contact and availability apply to Human seats only"
	msgHumanForbidden              = "a Human seat is never run by the engine"
	msgSlackUserID                 = "spec.contact.slackUserID in body should match"
	msgGitHubLogin                 = "spec.contact.githubLogin in body should match"
	msgMattermostUsername          = "spec.contact.mattermostUsername in body should match"
	msgMattermostReserved          = "mattermostUsername may not be all, channel, here, matterbot or system"
	msgAtlassianAccount            = "spec.contact.atlassianAccount in body should match"
	msgAtlassianReference          = "atlassianAccount may not hold ${"
	msgGitLabUsername              = "spec.contact.gitlabUsername in body should match"
	msgStdioCommand                = "command is required with transport Stdio and not allowed with HTTP"
	msgStdioOnly                   = "args and env apply to transport Stdio only"
	msgHTTPURL                     = "url is required with transport HTTP and not allowed with Stdio"
	msgOneVendor                   = "exactly one vendor (slack, github, gitlab, mattermost, jira, " +
		"confluence, datadog or atlassian) must be set"
	msgCompanyRefFrozen = "companyRef is immutable"
	msgPauseDetails     = "pauseReason and stopRunning describe a pause: they apply only while paused is true"
	msgPoolName         = `should match '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'`
	msgPoolsAllowedOnly = "allowPerSeat, maxPods and defaults apply to policy Allowed only"
	msgPoolsCapped      = "a class that allows seat pools caps them: set maxPods, the most seat pods one company may run"
	msgScratchFloor     = "scratchSizeLimit must be at least 256Mi"
	msgDataPoolName     = "data is the pool of the data pods"
	msgPerSeatReplicas  = "a PerSeat pool runs 1 or 2 replicas for each seat"
	msgDefaultSeatPool  = "defaultSeatPool must be data or the name of a Shared pool of seatPools"
)

func companyRef() v1.CompanyReference { return v1.CompanyReference{Name: companyName} }

func secretKey(name, key string) v1.SecretKeyReference {
	return v1.SecretKeyReference{Name: name, Key: key}
}

func fromSecret(name, key string) v1.SecretValueSource {
	return v1.SecretValueSource{SecretKeyRef: secretKey(name, key)}
}

// seatFromSecret is a seat's or a unit's credential read from a Secret key.
func seatFromSecret(name, key string) v1.SeatCredentialSource {
	return v1.SeatCredentialSource{SecretKeyRef: secretKey(name, key)}
}

// engineClass is the smallest valid EngineClass. It is cluster-scoped, so the
// namespace argument is unused; the name carries the test's prefix instead.
func engineClass(_, name string) *v1.EngineClass {
	return &v1.EngineClass{
		Name: name,
		Spec: v1.EngineClassSpec{
			Engine: v1.EngineSettings{Image: engineImage},
			Data: v1.DataPodSettings{
				Storage: v1.DataStorage{Size: new(resource.MustParse("16Gi"))},
			},
		},
	}
}

// company is the smallest valid Company.
func company(namespace, name string) *v1.Company {
	return &v1.Company{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.CompanySpec{CompanyName: "Nimbus"},
	}
}

// llmProvider is the smallest valid LLMProvider: Claude Code on a metered
// key.
func llmProvider(namespace, name string) *v1.LLMProvider {
	return &v1.LLMProvider{
		Namespace: namespace,
		Name:      name,
		Spec: v1.LLMProviderSpec{
			CompanyRef:          companyRef(),
			LLMProviderSettings: v1.LLMProviderSettings{Agent: v1.CLIAgentClaudeCode, Model: claudeModel},
			Auth: v1.CLIAgentAuth{
				Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: secretKey(providerName, apiKeyKey),
			},
		},
	}
}

// unit is the smallest valid Unit.
func unit(namespace, name string) *v1.Unit {
	return &v1.Unit{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.UnitSpec{CompanyRef: companyRef()},
	}
}

// seat is the smallest valid Seat: an agent.
func seat(namespace, name string) *v1.Seat {
	return &v1.Seat{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.SeatSpec{CompanyRef: companyRef()},
	}
}

// mcpServer is the smallest valid MCPServer: a Stdio server.
func mcpServer(namespace, name string) *v1.MCPServer {
	return &v1.MCPServer{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.MCPServerSpec{CompanyRef: companyRef(), Command: "uvx"},
	}
}

// slackIntegration is the smallest valid Integration: Slack with defaults.
func slackIntegration(namespace, name string) *v1.Integration {
	return &v1.Integration{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.IntegrationSpec{CompanyRef: companyRef(), Slack: &v1.SlackIntegration{}},
	}
}

// unitSchedule is the smallest valid Schedule: a unit's, which each of its
// agent seats runs.
func unitSchedule(namespace, name string) *v1.Schedule {
	return &v1.Schedule{
		Namespace: namespace,
		Name:      name,
		Spec: v1.ScheduleSpec{
			CompanyRef: companyRef(),
			Target:     v1.ScheduleTarget{UnitRef: v1.LocalObjectReference{Name: unitName}},
			Schedule:   "30 9 * * 1-5",
			Prompt:     "Post your standup.",
		},
	}
}

func githubMember() v1.GitHubIntegration {
	return v1.GitHubIntegration{WebhookSecretRef: secretKey(githubKeys, "webhookSecret")}
}

// The tool server the Slack cases name, and the variables slack-mcp-server
// reads: the one it takes its bot's token from, and the file it keeps the
// channels the bot can see in, which each seat names for itself.
const (
	slackServer      = "slack"
	envSlackBot      = "SLACK_MCP_XOXB_TOKEN"
	envSlackChannels = "SLACK_MCP_CHANNELS_CACHE"
)

// Settings of tool servers the cases write out as literal values: an address
// mcp-atlassian reads, and a header GitHub's remote server reads its toolsets
// from.
const (
	envJiraURL     = "JIRA_URL"
	headerToolsets = "X-MCP-Toolsets"
)

// slackChannelsCache is a seat's own channel cache for slack-mcp-server, in
// the engine's home.
func slackChannelsCache(handle string) v1.MCPCredentialValue {
	path := "/home/crewlet/.cache/slack-mcp-server/" + handle + "-channels.json"
	return v1.MCPCredentialValue{Name: envSlackChannels, Value: &path}
}

// mcpCredentials gives a seat or a unit its own Authorization header for a
// server.
func mcpCredentials(server string) v1.MCPCredentials {
	return v1.MCPCredentials{
		Name: server,
		Env: []v1.MCPCredentialValue{{
			Name:      "Authorization",
			ValueFrom: seatFromSecret(seatHandle+"-"+server, "authorization"),
		}},
	}
}

func providers(names ...string) []v1.LLMProviderReference {
	refs := make([]v1.LLMProviderReference, 0, len(names))
	for _, name := range names {
		refs = append(refs, v1.LLMProviderReference{Name: name})
	}
	return refs
}
