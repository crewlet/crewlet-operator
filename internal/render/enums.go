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

func gitHubAccessTier(t v1alpha1.GitHubAccessTier) string {
	switch t {
	case v1alpha1.GitHubAccessReadOnly:
		return "read_only"
	case v1alpha1.GitHubAccessReview:
		return "review"
	case v1alpha1.GitHubAccessFullAccess:
		return "full_access"
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
	}
	return ""
}

func gitLabAccessLevel(l v1alpha1.GitLabAccessLevel) string {
	switch l {
	case v1alpha1.GitLabDeveloper:
		return "developer"
	case v1alpha1.GitLabMaintainer:
		return "maintainer"
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
