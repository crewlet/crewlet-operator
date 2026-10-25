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
	runCreateCases(t, llmProvider, []createCase[*v1.LLMProvider]{
		{name: "minimal provider is accepted"},
		{
			name: "every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Model = "claude-opus-5-5"
				p.Spec.TimeoutSeconds = 900
				p.Spec.MaxConcurrent = 2
				p.Spec.LoginName = "claude"
				p.Spec.Env = []v1.SecretEnvVar{{Name: envBaseURL, ValueFrom: fromSecret("gateway", "url")}}
			},
		},
		{
			// Crewlet trims a model and refuses an empty one, white space
			// as Go's strings.TrimSpace reads it.
			name:   "a model of white space alone is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Model = whiteSpace },
			want:   msgBlankModel,
		},
		{
			name:   "a model of Unicode white space alone is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Model = "\u3000\u00a0" },
			want:   msgBlankModel,
		},
		{
			name:   "a provider with no agent is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Agent = "" },
			want:   "spec.agent: Required value",
		},
		{
			name:   "a provider with no auth is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.CLIAgentAuth{} },
			want:   "spec.auth: Required value",
		},
		{
			name:   "a timeout below a second is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.TimeoutSeconds = -1 },
			want:   "spec.timeoutSeconds: Invalid value",
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

// The phrases the OpenCode model rule's message quotes from the engine's own
// refusal of such a model: the error OpenCode 2 refuses it with, and the
// command that lists the providers OpenCode knows. TestCLIAgentModelFollowsTheEngine
// holds the rule's message, the engine's refusal and docs/guides/cli-agents.md
// to both.
const (
	openCodeModelRefusal = "Invalid model reference"
	openCodeModelListing = "opencode models --standalone"
)

// The messages of the rules over a provider's CLI and its sign-in.
const (
	msgCLIKey         = "apiKeySecretRef is the key auth mode APIKey hands the CLI"
	msgCLIRunIn       = "runIn applies to mode Agent only"
	msgCLIAgentRunner = "mode Agent runs a CLI Crewlet has a sandbox runner for: ClaudeCode or OpenCode"
	msgCLIToken       = "auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode"
	msgCLIKeyAgent    = "auth mode APIKey takes a CLI Crewlet hands a key to"
	msgCLITwoLogins   = "tokenSecretRef and credentialBundleSecretRef are two logins"
	msgCLIRemoteLogin = "a Subscription login reaches an Agent-mode run in a remote sandbox only as a variable of its " +
		"environment: give ClaudeCode auth.tokenSecretRef, or use auth mode APIKey, and give OpenCode its model " +
		"provider's key in env"
	msgOpenCodeModel  = "an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash"
	msgCLINoLogin     = "a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef"
	msgCLILoginFields = "tokenSecretRef and credentialBundleSecretRef sign in to a subscription"
	msgCLIOwnVariable = "env may not set the CLI's own token or key variable"
	msgCLIIsolation   = "env may not set a variable Crewlet sets on every call of the CLI to keep it inside its " +
		"seat's own home"
	msgCLISubscriptionEnv = "under auth mode Subscription, env may not set a variable Crewlet sets there to sign " +
		"the CLI in with the login the seat's home holds"
	msgQwenKeyOnly = "QwenCode signs in with its key alone, under auth mode APIKey"
	// atEnv is how the API server reports a rule on spec that names env as
	// the field to change.
	atEnv = "spec.env: Invalid value"
)

// TestLLMProviderSignInValidation covers the rules over a provider's CLI and
// how it signs in, each of which Crewlet's admission holds too (the CRD
// suite's engine tests hold the lists they name to it).
func TestLLMProviderSignInValidation(t *testing.T) {
	// tokenProvider is Claude Code as a text model on a headless token.
	tokenProvider := func(namespace, name string) *v1.LLMProvider {
		p := llmProvider(namespace, name)
		p.Spec.Model = "sonnet"
		p.Spec.Auth = v1.CLIAgentAuth{TokenSecretRef: secretKey("claude", "oauthToken")}
		return p
	}
	agentMode := func(p *v1.LLMProvider) { p.Spec.Mode = v1.CLIAgentModeAgent }
	// metered signs the provider in with auth mode APIKey on one key.
	metered := v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: secretKey(providerName, apiKeyKey)}
	// keyInEnv signs agent in to a subscription with no login of its own,
	// its model provider's key in env: the route Crewlet's Hermes, Pi and
	// OpenCode profiles document, as they name no key variable for the
	// engine to fill.
	keyInEnv := func(agent v1.CLIAgentName, key string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Agent = agent
			p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription}
			p.Spec.Env = []v1.SecretEnvVar{{Name: key, ValueFrom: fromSecret("model-keys", "key")}}
		}
	}

	cases := []createCase[*v1.LLMProvider]{
		{name: "Claude Code on a headless token is accepted"},
		{
			name: "every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Mode = v1.CLIAgentModeAgent
				p.Spec.RunIn = v1.CLIAgentRunInE2B
				p.Spec.Auth.Mode = v1.CLIAgentAuthSubscription
				p.Spec.Env = []v1.SecretEnvVar{{Name: envProxy, ValueFrom: fromSecret("claude", "proxy")}}
				p.Spec.TimeoutSeconds = 900
				p.Spec.MaxConcurrent = 2
				p.Spec.LoginName = "claude"
			},
		},
		{
			name: "Codex on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Agent = v1.CLIAgentCodex
				p.Spec.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("codex", "bundle")}
			},
		},
		{
			name: "Claude Code in mode Agent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.Auth = metered
			},
		},
		{
			name: "Codex on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Agent = v1.CLIAgentCodex
				p.Spec.Auth = metered
			},
		},
		{
			// Crewlet hands the run the model as the provider's text
			// calls pass it, and exports a key in env into the remote
			// sandbox, where it signs OpenCode in.
			name: "OpenCode in mode Agent on its model provider's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey)(p)
				agentMode(p)
				p.Spec.RunIn = v1.CLIAgentRunInE2B
			},
		},
		{
			name: "OpenCode in mode Agent through a gateway's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
				agentMode(p)
				p.Spec.Model = modelViaOpenRouter
			},
		},
		{
			// The bundle never reaches the sandbox; the key in env does,
			// and the engine reports the provider signed in through its
			// environment.
			name: "OpenCode in mode Agent on an exported login beside a key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey)(p)
				agentMode(p)
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("opencode", "bundle")
			},
		},
		{
			name: "OpenCode in mode Agent on an exported login is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.Model = modelViaAnthropic
				p.Spec.Agent = v1.CLIAgentOpenCode
				p.Spec.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("opencode", "bundle")}
			},
			want: msgCLIRemoteLogin,
		},
		{
			// A proxy beside the bundle is configuration: nothing in the
			// run's environment signs OpenCode in.
			name: "OpenCode in mode Agent on an exported login beside a proxy in env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envProxy)(p)
				agentMode(p)
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("opencode", "bundle")
			},
			want: msgCLIRemoteLogin,
		},
		{
			name: "OpenCode in mode Agent on a proxy alone in env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envProxy)(p)
				agentMode(p)
			},
			want: msgCLINoLogin,
		},
		{
			name: "OpenCode in mode Agent on auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey)(p)
				agentMode(p)
				p.Spec.Env = nil
				p.Spec.Auth = metered
			},
			want: msgCLIKeyAgent,
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
				p.Spec.Agent = v1.CLIAgentCodex
			},
			want: msgCLIToken,
		},
		{
			// Crewlet's cursor-agent profile names CURSOR_API_KEY, a
			// Cursor account's key.
			name: "CursorAgent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Model = "auto"
				p.Spec.Agent = v1.CLIAgentCursorAgent
				p.Spec.Auth = metered
			},
		},
		{
			name: "auth mode APIKey for a CLI with no key variable is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Agent = v1.CLIAgentHermes
				p.Spec.Auth = metered
			},
			want: msgCLIKeyAgent,
		},
		{
			name: "a headless token beside an exported login is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("claude", "bundle")
			},
			want: msgCLITwoLogins,
		},
		{
			name: "a key outside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgCLIKey,
		},
		{
			// Crewlet would read the CLI's key variable from its secret
			// store and the engine's environment, which are not this
			// object's to name.
			name:   "auth mode APIKey without a key is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthAPIKey} },
			want:   msgCLIKey,
		},
		{
			name:   "runIn in mode Text is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.RunIn = v1.CLIAgentRunInE2B },
			want:   msgCLIRunIn,
		},
		{
			name: "a CLI with no sandbox runner in mode Agent is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.Agent = v1.CLIAgentCodex
			},
			want: msgCLIAgentRunner,
		},
		{
			name: "an exported login in mode Agent is refused",
			mutate: func(p *v1.LLMProvider) {
				agentMode(p)
				p.Spec.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("claude", "bundle")}
			},
			want: msgCLIRemoteLogin,
		},
		{
			name:   "a subscription with no login is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription} },
			want:   msgCLINoLogin,
		},
		{
			// The rule sits on spec, which sees the agent and env, and
			// reports the field a person has to change.
			name:   "a subscription with no login is reported at auth",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription} },
			want:   "spec.auth: Invalid value",
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
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("hermes", "bundle")
			},
		},
		{
			name: "Hermes on a subscription with neither a login nor env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.Env = nil
			},
			want: msgCLINoLogin,
		},
		{
			name: "a subscription token beside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.Mode = v1.CLIAgentAuthAPIKey
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgCLILoginFields,
		},
		{
			// A credential in the engine's environment is one a ${NAME}
			// in many fields of the company document can have the engine
			// send anywhere, and the CLI's own children can read.
			name:   "a login inherited from the engine's environment is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.CLIAgentAuth{Mode: "InheritEnv"} },
			want:   `Unsupported value: "InheritEnv"`,
		},
		{
			name:   "Crewlet's custom profile is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.Agent = "Custom" },
			want:   `Unsupported value: "Custom"`,
		},
		{
			name:   "a local run is not offered",
			mutate: func(p *v1.LLMProvider) { agentMode(p); p.Spec.RunIn = "Direct" },
			want:   `Unsupported value: "Direct"`,
		},
	}
	// The exception is the env sign-in CLIs' alone, so every other CLI the
	// API offers is refused a key in env, read from the installed CRD's enum
	// so a CLI added later is held to the rule without being listed here. A
	// CLI whose profile names a key variable (Codex's CODEX_API_KEY) has it
	// dropped under Subscription, so the key would never reach it; one whose
	// profile names none (KimiCode) reads a key only from its own files.
	for _, agent := range enumOf(t, "llmproviders.crewlet.ai", "spec", "agent") {
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
					p.Spec.Agent = agent
					p.Spec.Auth = bundle
					if variable == own.key {
						p.Spec.Auth = metered
					}
					p.Spec.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("cli", "value")}}
				},
				want: msgCLIOwnVariable,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			// The rule sits on spec, and reports the field to change.
			name: "a CLI's own variable in env is reported at env",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Env = []v1.SecretEnvVar{{Name: envAnthropicKey, ValueFrom: fromSecret("cli", "value")}}
			},
			want: atEnv,
		},
		createCase[*v1.LLMProvider]{
			// Another CLI's variable is that CLI's business: OpenCode
			// reads ANTHROPIC_API_KEY as its model provider's key.
			name:   "another CLI's key variable in env is accepted",
			mutate: keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey),
		},
	)
	// Crewlet sets HOME, the XDG directories and TMPDIR on every call of
	// every CLI, and each profile's own configuration directory or switch,
	// to keep the CLI inside its seat's home and its call's working
	// directory; env is layered over them, so Crewlet refuses each in env.
	// Each CLI is signed in with a bundle in mode Text, which the API admits
	// for every one, so the variable is the only fault.
	inEnvBesideBundle := func(agent v1.CLIAgentName, variable string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Agent = agent
			p.Spec.Auth = bundle
			p.Spec.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("cli", "value")}}
		}
	}
	for _, agent := range enumOf(t, "llmproviders.crewlet.ai", "spec", "agent") {
		name := v1.CLIAgentName(agent)
		for _, variable := range append(slices.Clone(homeIsolation), cliIsolation[name]...) {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("%s with %s in env is refused", agent, variable),
				mutate: inEnvBesideBundle(name, variable),
				want:   msgCLIIsolation,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			// The rule sits on spec, and reports the field to change.
			name:   "an isolation variable in env is reported at env",
			mutate: inEnvBesideBundle(v1.CLIAgentClaudeCode, "HOME"),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			// A profile's own directory is its CLI's alone: OpenCode
			// does not read Codex's.
			name:   "another CLI's configuration directory in env is accepted",
			mutate: inEnvBesideBundle(v1.CLIAgentOpenCode, "CODEX_HOME"),
		},
		createCase[*v1.LLMProvider]{
			// Crewlet matches a variable's name exactly, as the
			// environment does.
			name:   "an isolation variable's name in lower case is accepted",
			mutate: inEnvBesideBundle(v1.CLIAgentClaudeCode, "home"),
		},
	)
	// Qwen Code keeps no login of its own in the builds Crewlet supports, so
	// under Subscription it would sign in to nothing, whatever it is handed.
	qwen := func(auth v1.CLIAgentAuth) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = "qwen3-coder-plus"
			p.Spec.Agent = v1.CLIAgentQwenCode
			p.Spec.Auth = auth
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			name:   "QwenCode on a bundle is refused",
			mutate: qwen(bundle),
			want:   msgQwenKeyOnly,
		},
		createCase[*v1.LLMProvider]{
			name:   "QwenCode on a bundle is reported at auth",
			mutate: qwen(bundle),
			want:   "spec.auth: Invalid value",
		},
		createCase[*v1.LLMProvider]{
			name:   "QwenCode on its key under APIKey is accepted",
			mutate: qwen(metered),
		},
	)
	// Under Subscription Crewlet sets, after env, what points a CLI that picks
	// its sign-in from its environment at the login its seat's home holds
	// (GeminiCLI's GOOGLE_GENAI_USE_GCA), so it refuses that variable in env
	// there; under APIKey it sets nothing, and the variable is the operator's.
	for agent, variables := range subscriptionEnv {
		for _, variable := range variables {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s with %s in env under Subscription is refused", agent, variable),
					mutate: inEnvBesideBundle(agent, variable),
					want:   msgCLISubscriptionEnv,
				},
				createCase[*v1.LLMProvider]{
					name: fmt.Sprintf("%s with %s in env under APIKey is accepted", agent, variable),
					mutate: func(p *v1.LLMProvider) {
						inEnvBesideBundle(agent, variable)(p)
						p.Spec.Auth = metered
					},
				},
				createCase[*v1.LLMProvider]{
					// Another CLI's variable is that CLI's business.
					name:   fmt.Sprintf("another CLI with %s in env under Subscription is accepted", variable),
					mutate: inEnvBesideBundle(v1.CLIAgentClaudeCode, variable),
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s in env under Subscription is reported at env", variable),
					mutate: inEnvBesideBundle(agent, variable),
					want:   atEnv,
				})
		}
	}
	// OpenCode splits its model at the first slash into a provider and a
	// model id, and Crewlet refuses a model with nothing but white space on
	// either side of it, white space as Go's strings.TrimSpace reads it: the
	// Unicode spaces beyond ASCII's among it.
	openCodeModel := func(model string, mode v1.CLIAgentMode) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
			p.Spec.Mode = mode
			p.Spec.Model = model
		}
	}
	for _, mode := range []v1.CLIAgentMode{v1.CLIAgentModeText, v1.CLIAgentModeAgent} {
		for _, model := range []string{
			modelViaAnthropic, modelViaOpenRouter,
			" anthropic/claude-sonnet-5", "anthropic/ claude-sonnet-5", "a/b",
		} {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("an OpenCode model %q in mode %s is accepted", model, mode),
				mutate: openCodeModel(model, mode),
			})
		}
		for _, model := range []string{
			"claude-sonnet-5", "anthropic/", "/claude-sonnet-5", "/", " /claude-sonnet-5",
			"anthropic/ ", "anthropic/\t\n", "\u00a0/claude-sonnet-5", "anthropic/\u3000",
			"\u2028/x", "anthropic/\u0085", "   ",
		} {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("an OpenCode model %q in mode %s is refused", model, mode),
				mutate: openCodeModel(model, mode),
				want:   msgOpenCodeModel,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			name:   "an OpenCode model with no provider is reported at model",
			mutate: openCodeModel(claudeModel, v1.CLIAgentModeText),
			want:   "spec.model: Invalid value",
		},
		createCase[*v1.LLMProvider]{
			// The grammar is OpenCode's: Hermes takes a model it routes
			// without one.
			name: "a Hermes model with no provider is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.Model = claudeModel
			},
		},
	)
	runCreateCases(t, tokenProvider, cases)
}

// TestLLMProviderEnvSignInEmptyEnv proves an explicit empty env signs an env
// sign-in CLI in to nothing: has() is true of a list written as [], which
// only an unstructured object can send, since the typed client drops an
// empty list.
func TestLLMProviderEnvSignInEmptyEnv(t *testing.T) {
	namespace := newNamespace(t)
	for _, agent := range envSignIn {
		t.Run(string(agent), func(t *testing.T) {
			p := llmProvider(namespace, strings.ToLower(string(agent)))
			p.Spec.Agent = agent
			p.Spec.Model = modelViaAnthropic
			p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthSubscription}
			content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
			if err != nil {
				t.Fatal(err)
			}
			obj := &unstructured.Unstructured{Object: content}
			obj.SetGroupVersionKind(v1.GroupVersion.WithKind("LLMProvider"))
			if err := unstructured.SetNestedSlice(obj.Object, []any{}, "spec", "env"); err != nil {
				t.Fatal(err)
			}
			expect(t, k8sClient.Create(ctx, obj), msgCLINoLogin)
		})
	}
}
