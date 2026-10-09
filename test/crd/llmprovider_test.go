package crd_test

import (
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

func TestLLMProviderValidation(t *testing.T) {
	openAICompatible := func(p *v1.LLMProvider) {
		p.Spec.Type = v1.LLMProviderOpenAICompatible
		p.Spec.Model = "llama-3.3-70b"
		p.Spec.BaseURL = "http://vllm.inference:8000/v1"
		p.Spec.APIKeySecretRefs = nil
	}
	openAI := func(p *v1.LLMProvider) {
		p.Spec.Type = v1.LLMProviderOpenAI
		p.Spec.Model = "gpt-5"
	}
	and := func(first, then func(*v1.LLMProvider)) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			first(p)
			then(p)
		}
	}

	runCreateCases(t, llmProvider, []createCase[*v1.LLMProvider]{
		{name: "minimal provider is accepted"},
		{
			name: "an Anthropic provider with every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.ClaudeModel = "claude-sonnet-4-5"
				p.Spec.BaseURL = "https://gateway.example/anthropic"
				p.Spec.ReasoningEffort = v1.ReasoningEffortHigh
				p.Spec.ReasoningBudgetTokens = 4096
				p.Spec.TimeoutSeconds = 600
				p.Spec.Cooldowns = v1.LLMCredentialCooldowns{RateLimitSeconds: 3600, AuthSeconds: 300}
				p.Spec.APIKeySecretRefs = append(p.Spec.APIKeySecretRefs, secretKey(providerName, "apiKey2"))
			},
		},
		{
			name: "an OpenAI reasoning provider is accepted",
			mutate: and(openAI, func(p *v1.LLMProvider) {
				p.Spec.Reasoning = new(true)
				p.Spec.ReasoningEffort = v1.ReasoningEffortXHigh
			}),
		},
		{name: "an OpenAICompatible provider needs no key", mutate: openAICompatible},
		{
			name:   "an Anthropic provider without keys is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.APIKeySecretRefs = nil },
			want:   msgKeysRequired,
		},
		{
			name:   "an OpenAI provider without keys is refused",
			mutate: and(openAI, func(p *v1.LLMProvider) { p.Spec.APIKeySecretRefs = nil }),
			want:   msgKeysRequired,
		},
		{
			name:   "an OpenAICompatible provider without baseURL is refused",
			mutate: and(openAICompatible, func(p *v1.LLMProvider) { p.Spec.BaseURL = "" }),
			want:   msgBaseURLRequired,
		},
		{
			name:   "claudeModel on an OpenAI provider is refused",
			mutate: and(openAI, func(p *v1.LLMProvider) { p.Spec.ClaudeModel = claudeModel }),
			want:   msgClaudeModel,
		},
		{
			name:   "reasoning on an Anthropic provider is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Reasoning = new(true) },
			want:   msgReasoning,
		},
		{
			name:   "a thinking budget on an OpenAI provider is refused",
			mutate: and(openAI, func(p *v1.LLMProvider) { p.Spec.ReasoningBudgetTokens = 2048 }),
			want:   msgBudgetTokens,
		},
		{
			name:   "reasoningEffort on an OpenAI provider without reasoning is refused",
			mutate: and(openAI, func(p *v1.LLMProvider) { p.Spec.ReasoningEffort = v1.ReasoningEffortHigh }),
			want:   msgReasoningEffort,
		},
		{
			name: "reasoningEffort on an OpenAI provider with reasoning off is refused",
			mutate: and(openAI, func(p *v1.LLMProvider) {
				p.Spec.Reasoning = new(false)
				p.Spec.ReasoningEffort = v1.ReasoningEffortLow
			}),
			want: msgReasoningEffort,
		},
		{
			name:   "reasoningEffort on an OpenAICompatible provider is refused",
			mutate: and(openAICompatible, func(p *v1.LLMProvider) { p.Spec.ReasoningEffort = v1.ReasoningEffortHigh }),
			want:   msgReasoningEffort,
		},
		{
			name:   "a baseURL that is not a URL is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.BaseURL = "gateway.example" },
			want:   msgBaseURLShape,
		},
		{
			name:   "the cli-agent type is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.Type = "CLIAgent" },
			want:   `Unsupported value: "CLIAgent"`,
		},
		// Crewlet runs every chainless seat on the provider keyed default,
		// ahead of the Company's order.
		{
			name:   "a provider named default is refused",
			mutate: func(p *v1.LLMProvider) { p.Name = "default" },
			want:   "an LLMProvider may not be named default",
		},
	})
}
