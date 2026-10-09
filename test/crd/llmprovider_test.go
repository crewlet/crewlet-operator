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
		// Crewlet runs every chainless seat on the provider keyed default,
		// ahead of the Company's order.
		{
			name:   "a provider named default is refused",
			mutate: func(p *v1.LLMProvider) { p.Name = "default" },
			want:   "an LLMProvider may not be named default",
		},
	})
}

// The messages of the rules over a CLIAgent provider.
const (
	msgCLIBlock       = "cli is required with type CLIAgent and not allowed otherwise"
	msgCLIHTTP        = "baseURL, timeoutSeconds and cooldowns configure an HTTP client"
	msgCLIKey         = "a CLIAgent provider takes apiKeySecretRefs with cli.auth.mode APIKey, and only then"
	msgCLIOneKey      = "a CLIAgent provider gives its CLI one key"
	msgCLIRunIn       = "runIn applies to mode Agent only"
	msgCLIAgentRunner = "mode Agent runs ClaudeCode or OpenCode"
	msgCLIRemoteLogin = "a Subscription login reaches an Agent-mode run in a remote sandbox only as a headless token"
	msgCLINoLogin     = "a Subscription login needs tokenSecretRef or credentialBundleSecretRef"
	msgCLILoginFields = "tokenSecretRef and credentialBundleSecretRef sign in to a subscription"
)

func TestCLIAgentProviderValidation(t *testing.T) {
	// cliProvider is Claude Code as a text model on a headless token.
	cliProvider := func(namespace, name string) *v1.LLMProvider {
		p := llmProvider(namespace, name)
		p.Spec.Type = v1.LLMProviderCLIAgent
		p.Spec.Model = "sonnet"
		p.Spec.APIKeySecretRefs = nil
		p.Spec.CLI = v1.CLIAgentSettings{
			Agent: v1.CLIAgentClaudeCode,
			Auth:  v1.CLIAgentAuth{TokenSecretRef: secretKey("claude", "oauthToken")},
		}
		return p
	}
	agentMode := func(p *v1.LLMProvider) { p.Spec.CLI.Mode = v1.CLIAgentModeAgent }

	runCreateCases(t, cliProvider, []createCase[*v1.LLMProvider]{
		{name: "Claude Code on a headless token is accepted"},
		{
			name: "every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				c := &p.Spec.CLI
				c.Mode = v1.CLIAgentModeAgent
				c.RunIn = v1.CLIAgentRunInE2B
				c.Auth.Mode = v1.CLIAgentAuthSubscription
				c.Env = []v1.SecretEnvVar{{Name: "HTTPS_PROXY", ValueFrom: fromSecret("claude", "proxy")}}
				c.TimeoutSeconds = 900
				c.MaxConcurrent = 2
				c.LoginName = "claude"
			},
		},
		{
			name: "Codex on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Agent = v1.CLIAgentCodex
				p.Spec.CLI.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("codex", "bundle")}
			},
		},
		{
			name: "OpenCode in mode Agent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.CLI.Agent = v1.CLIAgentOpenCode
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
		},
		{
			name:   "type CLIAgent without cli is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI = v1.CLIAgentSettings{} },
			want:   msgCLIBlock,
		},
		{
			name: "cli on an Anthropic provider is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Type = v1.LLMProviderAnthropic
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
			want: msgCLIBlock,
		},
		{
			name:   "an HTTP timeout is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.TimeoutSeconds = 600 },
			want:   msgCLIHTTP,
		},
		{
			name:   "a baseURL is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.BaseURL = "https://gateway.example" },
			want:   msgCLIHTTP,
		},
		{
			name: "a key outside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
			want: msgCLIKey,
		},
		{
			name:   "auth mode APIKey without a key is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey} },
			want:   msgCLIKey,
		},
		{
			name: "two keys are refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{
					secretKey(providerName, "first"), secretKey(providerName, "second"),
				}
			},
			want: msgCLIOneKey,
		},
		{
			name:   "runIn in mode Text is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.RunIn = v1.CLIAgentRunInE2B },
			want:   msgCLIRunIn,
		},
		{
			name: "a CLI with no sandbox runner in mode Agent is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.CLI.Agent = v1.CLIAgentCodex
			},
			want: msgCLIAgentRunner,
		},
		{
			name: "an exported login in mode Agent is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.CLI.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("claude", "bundle")}
			},
			want: msgCLIRemoteLogin,
		},
		{
			name:   "a subscription with no login is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription} },
			want:   msgCLINoLogin,
		},
		{
			name: "a subscription token beside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Auth.Mode = v1.CLIAgentAuthAPIKey
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
			want: msgCLILoginFields,
		},
		{
			// A credential in the engine's environment is one a ${NAME}
			// in any field of the company document can have the engine
			// send anywhere, and the CLI's own children can read.
			name:   "a login inherited from the engine's environment is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: "InheritEnv"} },
			want:   `Unsupported value: "InheritEnv"`,
		},
		{
			name:   "Crewlet's custom profile is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.Agent = "Custom" },
			want:   `Unsupported value: "Custom"`,
		},
		{
			name:   "a local run is not offered",
			mutate: func(p *v1.LLMProvider) { agentMode(p); p.Spec.CLI.RunIn = "Direct" },
			want:   `Unsupported value: "Direct"`,
		},
	})
}
