package crd_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

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
	msgCLIAgentRunner = "mode Agent runs ClaudeCode only"
	msgCLIToken       = "auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode"
	msgCLIKeyAgent    = "auth mode APIKey takes a CLI Crewlet hands a key to"
	msgCLITwoLogins   = "tokenSecretRef and credentialBundleSecretRef are two logins"
	msgCLIRemoteLogin = "a Subscription login reaches an Agent-mode run in a remote sandbox only as a headless token: " +
		"give ClaudeCode auth.tokenSecretRef, or use auth mode APIKey"
	msgCLINoLogin     = "a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef"
	msgCLILoginFields = "tokenSecretRef and credentialBundleSecretRef sign in to a subscription"
	msgCLIOwnVariable = "env may not set the CLI's own token or key variable"
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
	// keyInEnv signs agent in to a subscription with no login of its own,
	// its model provider's key in env: the route Crewlet's Hermes, Pi and
	// OpenCode profiles document, as they name no key variable for the
	// engine to fill.
	keyInEnv := func(agent v1.CLIAgentName, key string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = "anthropic/claude-sonnet-5"
			p.Spec.CLI.Agent = agent
			p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription}
			p.Spec.CLI.Env = []v1.SecretEnvVar{{Name: key, ValueFrom: fromSecret("model-keys", "key")}}
		}
	}

	cases := []createCase[*v1.LLMProvider]{
		{name: "Claude Code on a headless token is accepted"},
		{
			name: "every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				c := &p.Spec.CLI
				c.Mode = v1.CLIAgentModeAgent
				c.RunIn = v1.CLIAgentRunInE2B
				c.Auth.Mode = v1.CLIAgentAuthSubscription
				c.Env = []v1.SecretEnvVar{{Name: envProxy, ValueFrom: fromSecret("claude", "proxy")}}
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
			name: "Claude Code in mode Agent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
		},
		{
			name: "Codex on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Agent = v1.CLIAgentCodex
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
		},
		{
			// Crewlet runs OpenCode in its sandbox, but names the run's
			// model to it as anthropic/<model> while every text call on
			// the provider passes the model as written: no one model
			// serves both, whichever key signs it in.
			name: "OpenCode in mode Agent on its model provider's key in env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey)(p)
				agentMode(p)
			},
			want: msgCLIAgentRunner,
		},
		{
			name: "OpenCode in mode Agent on an exported login is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.CLI.Agent = v1.CLIAgentOpenCode
				p.Spec.CLI.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("opencode", "bundle")}
			},
			want: msgCLIAgentRunner,
		},
		{
			// Hermes signs in through env too, and has no sandbox
			// runner.
			name: "Hermes in mode Agent is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				agentMode(p)
			},
			want: msgCLIAgentRunner,
		},
		{
			name: "a headless token for a CLI other than Claude Code is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Agent = v1.CLIAgentCodex
			},
			want: msgCLIToken,
		},
		{
			// Crewlet's cursor-agent profile names CURSOR_API_KEY, a
			// Cursor account's key.
			name: "CursorAgent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Model = "auto"
				p.Spec.CLI.Agent = v1.CLIAgentCursorAgent
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
		},
		{
			name: "auth mode APIKey for a CLI with no key variable is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Agent = v1.CLIAgentHermes
				p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
				p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
			},
			want: msgCLIKeyAgent,
		},
		{
			name: "a headless token beside an exported login is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Auth.CredentialBundleSecretRef = secretKey("claude", "bundle")
			},
			want: msgCLITwoLogins,
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
			// The rule sits on cli, which sees the agent and env, and
			// reports the field a person has to change.
			name:   "a subscription with no login is reported at auth",
			mutate: func(p *v1.LLMProvider) { p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription} },
			want:   "spec.cli.auth: Invalid value",
		},
		{
			// Crewlet compares a name's parts ignoring case.
			name:   "Pi on a key in env under a lower-case name is accepted",
			mutate: keyInEnv(v1.CLIAgentPi, "openrouter_api_key"),
		},
		{
			// A name whose parts are KEY or TOKEN only inside a longer
			// word holds no credential to Crewlet, which reports such a
			// CLI signed in to nothing.
			name:   "Hermes on a count of model tokens in env is refused",
			mutate: keyInEnv(v1.CLIAgentHermes, "HERMES_MAX_TOKENS"),
			want:   msgCLINoLogin,
		},
		{
			// Two sign-ins, which Hermes settles itself: a variable the
			// bundle's .env also sets takes the file's value, and a model
			// provider's key in env wins over an OAuth login in the
			// bundle's auth.json. Neither is refused.
			name: "Hermes on an exported login beside a key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.CLI.Auth.CredentialBundleSecretRef = secretKey("hermes", "bundle")
			},
		},
		{
			name: "Hermes on a subscription with neither a login nor env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.CLI.Env = nil
			},
			want: msgCLINoLogin,
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
	}
	// The exception is the env sign-in CLIs' alone, so every other CLI the
	// API offers is refused a key in env, read from the installed CRD's enum
	// so a CLI added later is held to the rule without being listed here. A
	// CLI whose profile names a key variable (Codex's OPENAI_API_KEY) has it
	// dropped under Subscription, so the key would never reach it; one whose
	// profile names none (KimiCode) reads a key only from its own files.
	for _, agent := range enumOf(t, "llmproviders.crewlet.ai", "spec", "cli", "agent") {
		if slices.Contains(envSignIn, v1.CLIAgentName(agent)) {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   agent + " on its model provider's key in env is accepted",
					mutate: keyInEnv(v1.CLIAgentName(agent), "OPENROUTER_API_KEY"),
				},
				createCase[*v1.LLMProvider]{
					// A proxy is configuration, and signs the CLI in
					// to nothing.
					name:   agent + " on a proxy alone in env is refused",
					mutate: keyInEnv(v1.CLIAgentName(agent), envProxy),
					want:   msgCLINoLogin,
				})
			continue
		}
		cases = append(cases, createCase[*v1.LLMProvider]{
			name:   agent + " on a key in env is refused",
			mutate: keyInEnv(v1.CLIAgentName(agent), "OPENROUTER_API_KEY"),
			want:   msgCLINoLogin,
		})
	}
	// Crewlet's auth owns the variables a profile names for a token and a
	// key, and refuses each in env, in every auth mode: whether a value
	// there reached the CLI would turn on the mode and on what else holds a
	// credential. Each CLI is signed in some other way the API admits, a
	// bundle beside a token variable and its own key beside a key variable,
	// so the variable is the only fault.
	bundle := v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("cli", "bundle")}
	for _, agent := range slices.Sorted(maps.Keys(cliVariables)) {
		own := cliVariables[agent]
		for _, variable := range []string{own.headless, own.key} {
			if variable == "" {
				continue
			}
			cases = append(cases, createCase[*v1.LLMProvider]{
				name: fmt.Sprintf("%s with its own %s in env is refused", agent, variable),
				mutate: func(p *v1.LLMProvider) {
					p.Spec.CLI.Agent = agent
					p.Spec.CLI.Auth = bundle
					if variable == own.key {
						p.Spec.CLI.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey}
						p.Spec.APIKeySecretRefs = []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)}
					}
					p.Spec.CLI.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("cli", "value")}}
				},
				want: msgCLIOwnVariable,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			// The rule sits on cli, and reports the field to change.
			name: "a CLI's own variable in env is reported at env",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.CLI.Env = []v1.SecretEnvVar{{Name: envAnthropicKey, ValueFrom: fromSecret("cli", "value")}}
			},
			want: "spec.cli.env: Invalid value",
		},
		createCase[*v1.LLMProvider]{
			// Another CLI's variable is that CLI's business: OpenCode
			// reads ANTHROPIC_API_KEY as its model provider's key.
			name:   "another CLI's key variable in env is accepted",
			mutate: keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey),
		},
	)
	runCreateCases(t, cliProvider, cases)
}

// TestCLIAgentEnvSignInEmptyEnv proves an explicit empty env signs an env
// sign-in CLI in to nothing: has() is true of a list written as [], which
// only an unstructured object can send, since the typed client drops an
// empty list.
func TestCLIAgentEnvSignInEmptyEnv(t *testing.T) {
	namespace := newNamespace(t)
	for _, agent := range envSignIn {
		t.Run(string(agent), func(t *testing.T) {
			p := llmProvider(namespace, strings.ToLower(string(agent)))
			p.Spec.Type = v1.LLMProviderCLIAgent
			p.Spec.Model = "anthropic/claude-sonnet-5"
			p.Spec.APIKeySecretRefs = nil
			p.Spec.CLI = v1.CLIAgentSettings{
				Agent: agent,
				Auth:  v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription},
			}
			content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
			if err != nil {
				t.Fatal(err)
			}
			obj := &unstructured.Unstructured{Object: content}
			obj.SetGroupVersionKind(v1.GroupVersion.WithKind("LLMProvider"))
			if err := unstructured.SetNestedSlice(obj.Object, []any{}, "spec", "cli", "env"); err != nil {
				t.Fatal(err)
			}
			expect(t, k8sClient.Create(ctx, obj), msgCLINoLogin)
		})
	}
}
