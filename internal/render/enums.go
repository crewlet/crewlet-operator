package render

import "github.com/crewlet/crewlet-operator/api/v1alpha1"

// The API spells its enums in PascalCase, as Kubernetes APIs do, and Crewlet
// spells them its own way. Each function below maps one enum; it has no
// default branch, so the exhaustive linter fails the build when the API gains
// a value nobody mapped. The empty string, an unset optional field, maps to
// the API's default where the API has one, and otherwise to the empty
// string, which the document omits.

func seatKind(k v1alpha1.SeatKind) string {
	switch k {
	case v1alpha1.SeatKindHuman:
		return "human"
	case v1alpha1.SeatKindAgent, "":
		// An unset kind is the API's default, Agent.
		return "agent"
	}
	return ""
}

func unitType(t v1alpha1.UnitType) string {
	switch t {
	case v1alpha1.UnitTypeDivision:
		return "division"
	case v1alpha1.UnitTypeDepartment:
		return "department"
	case v1alpha1.UnitTypeGroup:
		return "group"
	case v1alpha1.UnitTypeTeam:
		return "team"
	case v1alpha1.UnitTypeSquad:
		return "squad"
	case v1alpha1.UnitTypePod:
		return "pod"
	case v1alpha1.UnitTypeGuild:
		return "guild"
	case v1alpha1.UnitTypeChapter:
		return "chapter"
	case v1alpha1.UnitTypeUnit:
		return "unit"
	}
	return ""
}

// crewletNone is how Crewlet spells the absence of a backend, and a Datadog
// route_to that wakes nobody.
const crewletNone = "none"

func trackerBackend(b v1alpha1.TrackerBackend) string {
	switch b {
	case v1alpha1.TrackerWorklet:
		return "worklet"
	case v1alpha1.TrackerJira:
		return "jira"
	case v1alpha1.TrackerNone:
		return crewletNone
	}
	return ""
}

func knowledgeBackend(b v1alpha1.KnowledgeBackend) string {
	switch b {
	case v1alpha1.KnowledgeDoclet:
		return "doclet"
	case v1alpha1.KnowledgeConfluence:
		return "confluence"
	case v1alpha1.KnowledgeNone:
		return crewletNone
	}
	return ""
}

func embeddingProviderType(t v1alpha1.EmbeddingProviderType) string {
	switch t {
	case v1alpha1.EmbeddingProviderOpenAI, "":
		// An unset type is the API's default, OpenAI.
		return "openai"
	case v1alpha1.EmbeddingProviderOpenAICompatible:
		return "openai-compatible"
	}
	return ""
}

func mcpTransport(t v1alpha1.MCPTransport) string {
	switch t {
	case v1alpha1.MCPTransportStdio, "":
		// An unset transport is the API's default, Stdio.
		return "stdio"
	case v1alpha1.MCPTransportHTTP:
		return schemeHTTP
	}
	return ""
}

func workingStatus(s v1alpha1.WorkingStatus) string {
	switch s {
	case v1alpha1.WorkingStatusAlways:
		return "always"
	case v1alpha1.WorkingStatusAddressed:
		return "addressed"
	}
	return ""
}

// containerWebhookMode maps onto Crewlet's org_webhook, whose values are the
// strings "auto", "true" and "false": a YAML boolean there would be refused.
func containerWebhookMode(m v1alpha1.ContainerWebhookMode) string {
	switch m {
	case v1alpha1.ContainerWebhookAuto:
		return "auto"
	case v1alpha1.ContainerWebhookRequire:
		return "true"
	case v1alpha1.ContainerWebhookNever:
		return "false"
	}
	return ""
}

// gitHubPermission maps onto the level GitHub's API takes for an app
// permission.
func gitHubPermission(p v1alpha1.GitHubPermission) string {
	switch p {
	case v1alpha1.GitHubPermissionRead:
		return "read"
	case v1alpha1.GitHubPermissionWrite:
		return "write"
	}
	return ""
}

func objectBackend(b v1alpha1.ObjectBackend) string {
	switch b {
	case v1alpha1.ObjectBackendNATS, "":
		// An unset backend is the API's default, NATS.
		return "nats"
	case v1alpha1.ObjectBackendS3:
		return "s3"
	}
	return ""
}

func scheduleFanOut(f v1alpha1.ScheduleFanOut) string {
	switch f {
	case v1alpha1.ScheduleFanOutEach:
		return "each"
	case v1alpha1.ScheduleFanOutLead:
		return "lead"
	}
	return ""
}

func cliAgentName(a v1alpha1.CLIAgentName) string {
	switch a {
	case v1alpha1.CLIAgentClaudeCode:
		return "claude-code"
	case v1alpha1.CLIAgentCodex:
		return "codex"
	case v1alpha1.CLIAgentGeminiCLI:
		return "gemini-cli"
	case v1alpha1.CLIAgentQwenCode:
		return "qwen-code"
	case v1alpha1.CLIAgentOpenCode:
		return "opencode"
	case v1alpha1.CLIAgentCursorAgent:
		return "cursor-agent"
	case v1alpha1.CLIAgentCopilot:
		return "copilot"
	case v1alpha1.CLIAgentGrok:
		return "grok"
	case v1alpha1.CLIAgentMuseCode:
		return "muse-code"
	case v1alpha1.CLIAgentKimiCode:
		return "kimi-code"
	case v1alpha1.CLIAgentHermes:
		return "hermes"
	case v1alpha1.CLIAgentPi:
		return "pi"
	}
	return ""
}

func sandboxPlacement(p v1alpha1.SandboxPlacement) string {
	switch p {
	case v1alpha1.SandboxPlacementDirect:
		return "direct"
	case v1alpha1.SandboxPlacementE2B:
		return "e2b"
	}
	return ""
}

func cliAgentWebTools(w v1alpha1.CLIAgentWebTools) string {
	switch w {
	case v1alpha1.CLIAgentWebToolsAllowed:
		return "allowed"
	case v1alpha1.CLIAgentWebToolsDenied:
		return "denied"
	}
	return ""
}

func cliAgentAuthMode(m v1alpha1.CLIAgentAuthMode) string {
	switch m {
	case v1alpha1.CLIAgentAuthSubscription:
		return "subscription"
	case v1alpha1.CLIAgentAuthAPIKey:
		return "api-key"
	case v1alpha1.CLIAgentAuthEnv:
		return "env"
	}
	return ""
}

// gitLabAccessLevel maps a GitLab role onto the integer GitLab's Members API
// takes for it, which is what Crewlet stores. Zero is an unset role.
func gitLabAccessLevel(l v1alpha1.GitLabAccessLevel) int32 {
	switch l {
	case v1alpha1.GitLabGuest:
		return 10
	case v1alpha1.GitLabPlanner:
		return 15
	case v1alpha1.GitLabReporter:
		return 20
	case v1alpha1.GitLabDeveloper:
		return 30
	case v1alpha1.GitLabMaintainer:
		return 40
	case v1alpha1.GitLabOwner:
		return 50
	}
	return 0
}

// mattermostRole maps onto the identifier Mattermost's API takes for a role.
func mattermostRole(r v1alpha1.MattermostRole) string {
	switch r {
	case v1alpha1.MattermostSystemUser:
		return "system_user"
	case v1alpha1.MattermostSystemPostAllPublic:
		return "system_post_all_public"
	case v1alpha1.MattermostSystemPostAll:
		return "system_post_all"
	case v1alpha1.MattermostSystemAdmin:
		return "system_admin"
	}
	return ""
}

func gitLabAccountOwner(o v1alpha1.GitLabAccountOwner) string {
	switch o {
	case v1alpha1.GitLabAccountOwnerGroup:
		return "group"
	case v1alpha1.GitLabAccountOwnerInstance:
		return "instance"
	}
	return ""
}

// datadogSite maps a region onto the host name Crewlet names it by.
func datadogSite(s v1alpha1.DatadogSite) string {
	switch s {
	case v1alpha1.DatadogSiteUS1:
		return "datadoghq.com"
	case v1alpha1.DatadogSiteUS3:
		return "us3.datadoghq.com"
	case v1alpha1.DatadogSiteUS5:
		return "us5.datadoghq.com"
	case v1alpha1.DatadogSiteEU1:
		return "datadoghq.eu"
	case v1alpha1.DatadogSiteAP1:
		return "ap1.datadoghq.com"
	case v1alpha1.DatadogSiteAP2:
		return "ap2.datadoghq.com"
	case v1alpha1.DatadogSiteUS1FED:
		return "ddog-gov.com"
	}
	return ""
}

// secondFactor maps onto Crewlet's api.auth.totp. Required, and an unset
// value, which is the API's default, Required, map to nothing: an unset totp
// is Crewlet's required, so the document leaves it out (see apiAuthDoc for
// why it is not written).
func secondFactor(f v1alpha1.SecondFactor) string {
	switch f {
	case v1alpha1.SecondFactorOptional:
		return "optional"
	case v1alpha1.SecondFactorRequired, "":
		return ""
	}
	return ""
}

// provisionedVendor maps onto the vendor's name in the engine's own name for a
// credential it mints, which is also the key of its integrations block.
func provisionedVendor(v v1alpha1.ProvisionedVendor) string {
	switch v {
	case v1alpha1.ProvisionedGitLab:
		return VendorGitLab
	case v1alpha1.ProvisionedMattermost:
		return VendorMattermost
	case v1alpha1.ProvisionedDatadog:
		return VendorDatadog
	case v1alpha1.ProvisionedAtlassian:
		return VendorAtlassian
	}
	return ""
}

// The fields of the engine's own names for the credentials it mints.
const (
	sealedToken    = "TOKEN"
	sealedEmail    = "EMAIL"
	sealedAppKey   = "APP_KEY"
	sealedBotToken = "BOT_TOKEN"
)

// provisionedField maps onto the field in the engine's own name for a
// credential it mints.
func provisionedField(f v1alpha1.ProvisionedField) string {
	switch f {
	case v1alpha1.ProvisionedToken:
		return sealedToken
	case v1alpha1.ProvisionedEmail:
		return sealedEmail
	case v1alpha1.ProvisionedAppKey:
		return sealedAppKey
	case v1alpha1.ProvisionedBotToken:
		return sealedBotToken
	}
	return ""
}

// seatCredentialKind maps onto the integration's name in Crewlet's
// seat_credentials.
func seatCredentialKind(k v1alpha1.SeatCredentialKind) string {
	switch k {
	case v1alpha1.SeatCredentialAtlassian:
		return VendorAtlassian
	case v1alpha1.SeatCredentialDatadog:
		return VendorDatadog
	case v1alpha1.SeatCredentialGitHub:
		return VendorGitHub
	case v1alpha1.SeatCredentialGitLab:
		return VendorGitLab
	case v1alpha1.SeatCredentialMattermost:
		return VendorMattermost
	case v1alpha1.SeatCredentialSlack:
		return VendorSlack
	}
	return ""
}

// seatCredentialField maps onto the field of Crewlet's seat_credentials,
// which is the field of the engine's own name for the credential.
func seatCredentialField(f v1alpha1.SeatCredentialField) string {
	switch f {
	case v1alpha1.SeatCredentialToken:
		return sealedToken
	case v1alpha1.SeatCredentialEmail:
		return sealedEmail
	}
	return ""
}
