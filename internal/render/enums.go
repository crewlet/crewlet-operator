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

func trackerBackend(b v1alpha1.TrackerBackend) string {
	switch b {
	case v1alpha1.TrackerNative:
		return "native"
	case v1alpha1.TrackerNone:
		return "none"
	}
	return ""
}

func knowledgeBackend(b v1alpha1.KnowledgeBackend) string {
	switch b {
	case v1alpha1.KnowledgeNative:
		return "native"
	case v1alpha1.KnowledgeNone:
		return "none"
	}
	return ""
}

func llmProviderType(t v1alpha1.LLMProviderType) string {
	switch t {
	case v1alpha1.LLMProviderAnthropic:
		return "anthropic"
	case v1alpha1.LLMProviderOpenAI:
		return "openai"
	case v1alpha1.LLMProviderOpenAICompatible:
		return "openai-compatible"
	}
	return ""
}

func reasoningEffort(e v1alpha1.ReasoningEffort) string {
	switch e {
	case v1alpha1.ReasoningEffortLow:
		return "low"
	case v1alpha1.ReasoningEffortMedium:
		return "medium"
	case v1alpha1.ReasoningEffortHigh:
		return "high"
	case v1alpha1.ReasoningEffortXHigh:
		return "xhigh"
	case v1alpha1.ReasoningEffortMax:
		return "max"
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

func codingAgent(a v1alpha1.CodingAgent) string {
	switch a {
	case v1alpha1.CodingAgentClaudeCode:
		return "claude-code"
	case v1alpha1.CodingAgentOpenCode:
		return "opencode"
	}
	return ""
}

func mcpTransport(t v1alpha1.MCPTransport) string {
	switch t {
	case v1alpha1.MCPTransportStdio, "":
		// An unset transport is the API's default, Stdio.
		return "stdio"
	case v1alpha1.MCPTransportHTTP:
		return "http"
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
