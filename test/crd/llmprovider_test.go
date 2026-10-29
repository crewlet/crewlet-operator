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

// msgWebTools is the start of the message of the rule that holds webTools
// Denied to the CLIs Crewlet can turn the web off for, on an LLMProvider and
// on a platform provider alike.
const msgWebTools = "webTools Denied takes a CLI Crewlet can turn the web off for"

// TestLLMProviderWebToolsValidation covers the web stance: Denied on a CLI
// whose Crewlet profile carries a web denial is accepted, Denied on any other
// is refused, and Allowed is every CLI's. The render suite holds the rule's
// list of CLIs to the engine's own verdict
// (TestContractWebToolsDeniedFollowsTheEngine).
func TestLLMProviderWebToolsValidation(t *testing.T) {
	stance := func(agent v1.CLIAgentName, web v1.CLIAgentWebTools) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Agent = agent
			p.Spec.WebTools = web
		}
	}
	runCreateCases(t, llmProvider, []createCase[*v1.LLMProvider]{
		{name: "web denied on Claude Code is accepted", mutate: stance(v1.CLIAgentClaudeCode, v1.CLIAgentWebToolsDenied)},
		{name: "web denied on Codex is accepted", mutate: stance(v1.CLIAgentCodex, v1.CLIAgentWebToolsDenied)},
		{name: "web allowed on Gemini CLI is accepted", mutate: stance(v1.CLIAgentGeminiCLI, v1.CLIAgentWebToolsAllowed)},
		{
			name:   "web denied on Gemini CLI is refused",
			mutate: stance(v1.CLIAgentGeminiCLI, v1.CLIAgentWebToolsDenied),
			want:   msgWebTools,
		},
		{
			name:   "web denied on Grok is refused",
			mutate: stance(v1.CLIAgentGrok, v1.CLIAgentWebToolsDenied),
			want:   msgWebTools,
		},
		{
			name:   "a web stance outside the enum is refused",
			mutate: stance(v1.CLIAgentClaudeCode, "Off"),
			want:   "spec.webTools: Unsupported value",
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
	msgCLIKey   = "apiKeySecretRef is the key auth mode APIKey hands the CLI"
	msgCLIRunIn = "runIn places the provider's executor runs, and Crewlet launches an executor run " +
		"only on ClaudeCode or OpenCode"
	msgCLIToken       = "auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode"
	msgCLIKeyAgent    = "auth mode APIKey takes a CLI Crewlet hands a key to"
	msgCLITwoLogins   = "tokenSecretRef and credentialBundleSecretRef are two logins"
	msgOpenCodeModel  = "an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash"
	msgCLINoLogin     = "a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef"
	msgCLILoginFields = "tokenSecretRef and credentialBundleSecretRef sign in to a subscription"
	msgCLIOwnVariable = "env may not set the CLI's own token or key variable"
	msgCLIIsolation   = "env may not set a variable Crewlet sets on every call of the CLI to keep it inside its " +
		"seat's own home"
	msgCLISubscriptionEnv = "under auth mode Subscription, env may not set a variable Crewlet sets there to sign " +
		"the CLI in with the login the seat's home holds"
	msgQwenKeyOnly = "QwenCode signs in with its key alone, under auth mode APIKey"
	msgCLIZone     = "env may not set TZ: Crewlet hands every CLI it starts the engine's own zone"
	msgCLIEnvAgent = "auth mode Env takes a CLI Crewlet signs in from its env"
	msgCLIEnvUnset = "auth mode Env signs the CLI in with env alone, and env sets nothing that signs it in"
	msgCLIEnvOnly  = "env may set a variable that signs the CLI in from env"
	// atEnv and atAuth are how the API server reports a rule on spec that
	// names env, or auth, as the field to change.
	atEnv  = "spec.env: Invalid value"
	atAuth = "spec.auth: Invalid value"
)

// TestLLMProviderSignInValidation covers the rules over a provider's CLI and
// how it signs in, each of which Crewlet's admission holds too (the CRD
// suite's engine tests hold the lists they name to it).
func TestLLMProviderSignInValidation(t *testing.T) {
	// tokenProvider is Claude Code on a headless token.
	tokenProvider := func(namespace, name string) *v1.LLMProvider {
		p := llmProvider(namespace, name)
		p.Spec.Model = "sonnet"
		p.Spec.Auth = v1.CLIAgentAuth{TokenSecretRef: secretKey("claude", "oauthToken")}
		return p
	}
	inE2B := func(p *v1.LLMProvider) { p.Spec.RunIn = v1.SandboxPlacementE2B }
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
				p.Spec.RunIn = v1.SandboxPlacementE2B
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
			name: "Claude Code in E2B on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
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
			name: "OpenCode in E2B on its model provider's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envAnthropicKey)(p)
				inE2B(p)
			},
		},
		{
			name: "OpenCode run Direct through a gateway's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
				p.Spec.RunIn = v1.SandboxPlacementDirect
				p.Spec.Model = modelViaOpenRouter
			},
		},
		{
			// Where a login can follow a run is the engine's to judge at
			// each launch, on the seat the run is for, whose own
			// spec.sandbox may sign the CLI in: the operator reports a
			// seat whose executor it cannot sign in on the Seat's Ready
			// (SandboxLoginNotPortable), and admits the provider.
			name: "OpenCode in E2B on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Model = modelViaAnthropic
				p.Spec.Agent = v1.CLIAgentOpenCode
				p.Spec.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("opencode", "bundle")}
			},
		},
		{
			name: "Claude Code in E2B on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Auth = v1.CLIAgentAuth{CredentialBundleSecretRef: secretKey("claude", "bundle")}
			},
		},
		{
			name: "OpenCode in E2B on a proxy alone in env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, envProxy)(p)
				inE2B(p)
			},
			want: msgCLINoLogin,
		},
		{
			// Hermes signs in through env too, and has no in-box runner.
			name: "Hermes with a runIn is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				inE2B(p)
			},
			want: msgCLIRunIn,
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
			name: "a runIn on a CLI with no in-box runner is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.RunIn = v1.SandboxPlacementDirect
				p.Spec.Agent = v1.CLIAgentCodex
				p.Spec.Auth = metered
			},
			want: msgCLIRunIn,
		},
		{
			name: "a runIn refusal is reported at runIn",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Agent = v1.CLIAgentCodex
				p.Spec.Auth = metered
			},
			want: "spec.runIn: Invalid value",
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
			want:   atAuth,
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
			// An engine pod has no container runtime to run one in.
			name:   "a container run is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.RunIn = "Container" },
			want:   `Unsupported value: "Container"`,
		},
	}
	// The exception is the env sign-in CLIs' alone, so every other CLI the
	// API offers is refused a key in env, read from the installed CRD's enum
	// so a CLI added later is held to the rule without being listed here. A
	// CLI whose profile names a key variable (Codex's CODEX_API_KEY) has it
	// dropped under Subscription, so the key would never reach it; one whose
	// profile names none (KimiCode) reads a key only from its own files.
	for _, agent := range llmProviderEnum(t, "spec", "agent") {
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
	// Each CLI is signed in with a bundle, which the API admits for every
	// one, so the variable is the only fault.
	inEnvBesideBundle := func(agent v1.CLIAgentName, variable string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Agent = agent
			p.Spec.Auth = bundle
			p.Spec.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("cli", "value")}}
		}
	}
	for _, agent := range llmProviderEnum(t, "spec", "agent") {
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
		createCase[*v1.LLMProvider]{
			name:   "TZ in env is reported at env",
			mutate: inEnvBesideBundle(v1.CLIAgentClaudeCode, "TZ"),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			name:   "a zone's name in lower case is accepted",
			mutate: inEnvBesideBundle(v1.CLIAgentClaudeCode, "tz"),
		},
	)
	// Crewlet hands every CLI it starts the engine's own zone, and reads its
	// limit resets in it, so it refuses TZ in env for every CLI.
	for _, agent := range llmProviderEnum(t, "spec", "agent") {
		cases = append(cases, createCase[*v1.LLMProvider]{
			name:   agent + " with TZ in env is refused",
			mutate: inEnvBesideBundle(v1.CLIAgentName(agent), "TZ"),
			want:   msgCLIZone,
		})
	}
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
			want:   atAuth,
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
	// Under auth mode Env the provider's env alone signs its CLI in: Crewlet
	// hands it neither its token nor its key variable and seeds no login.
	// So env must set what signs the CLI in, one of the names its profile
	// lists (sign_in_env) or, for an env sign-in CLI, a credential-named
	// one, and a CLI whose profile has neither cannot take the mode. A name
	// of sign_in_env is refused under every other mode, which hands the CLI a
	// credential of its own beside it.
	envMode := func(agent v1.CLIAgentName, names ...string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Agent = agent
			p.Spec.Auth = v1.CLIAgentAuth{Mode: v1.CLIAgentAuthEnv}
			p.Spec.Env = nil
			for _, name := range names {
				p.Spec.Env = append(p.Spec.Env, v1.SecretEnvVar{Name: name, ValueFrom: fromSecret("cloud", name)})
			}
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			name: "Claude Code on Bedrock under auth mode Env is accepted",
			mutate: envMode(v1.CLIAgentClaudeCode,
				"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "AWS_BEARER_TOKEN_BEDROCK"),
		},
		createCase[*v1.LLMProvider]{
			name:   "Claude Code through a gateway's token under auth mode Env is accepted",
			mutate: envMode(v1.CLIAgentClaudeCode, envBaseURL, "ANTHROPIC_AUTH_TOKEN"),
		},
		createCase[*v1.LLMProvider]{
			name: "Claude Code in E2B under auth mode Env is accepted",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.CLIAgentClaudeCode, "CLAUDE_CODE_USE_VERTEX", "CLOUD_ML_REGION")(p)
				inE2B(p)
			},
		},
		createCase[*v1.LLMProvider]{
			// A gateway's address says where the calls go, not whom
			// they are made as.
			name:   "Claude Code on a gateway's address alone under auth mode Env is refused",
			mutate: envMode(v1.CLIAgentClaudeCode, envBaseURL),
			want:   msgCLIEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name:   "Claude Code under auth mode Env with no env is refused",
			mutate: envMode(v1.CLIAgentClaudeCode),
			want:   msgCLIEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name:   "an env that signs nothing in under auth mode Env is reported at env",
			mutate: envMode(v1.CLIAgentClaudeCode, envBaseURL),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			// Another CLI's key signs Claude Code in to nothing.
			name:   "Claude Code on a model provider's key under auth mode Env is refused",
			mutate: envMode(v1.CLIAgentClaudeCode, "OPENROUTER_API_KEY"),
			want:   msgCLIEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name: "a headless token under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.CLIAgentClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.TokenSecretRef = secretKey("claude", "oauthToken")
			},
			want: msgCLILoginFields,
		},
		createCase[*v1.LLMProvider]{
			name: "a bundle under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.CLIAgentClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("claude", "bundle")
			},
			want: msgCLILoginFields,
		},
		createCase[*v1.LLMProvider]{
			name: "a key under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.CLIAgentClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgCLIKey,
		},
		createCase[*v1.LLMProvider]{
			name:   "the CLI's own key variable in env under auth mode Env is refused",
			mutate: envMode(v1.CLIAgentClaudeCode, "ANTHROPIC_AUTH_TOKEN", envAnthropicKey),
			want:   msgCLIOwnVariable,
		},
	)
	for _, agent := range llmProviderEnum(t, "spec", "agent") {
		name := v1.CLIAgentName(agent)
		switch {
		case slices.Contains(envSignIn, name):
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   agent + " on its model provider's key under auth mode Env is accepted",
					mutate: envMode(name, "OPENROUTER_API_KEY"),
				},
				createCase[*v1.LLMProvider]{
					name:   agent + " on a proxy alone under auth mode Env is refused",
					mutate: envMode(name, envProxy),
					want:   msgCLIEnvUnset,
				},
				createCase[*v1.LLMProvider]{
					// A cloud switch is no credential, and signs only
					// Claude Code in.
					name:   agent + " on Claude Code's cloud switch under auth mode Env is refused",
					mutate: envMode(name, "CLAUDE_CODE_USE_BEDROCK"),
					want:   msgCLIEnvUnset,
				})
		case len(signInEnv[name]) > 0:
			// Each of its names is tried below, under every mode.
		default:
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   agent + " under auth mode Env is refused",
					mutate: envMode(name, "OPENROUTER_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
					want:   msgCLIEnvAgent,
				},
				createCase[*v1.LLMProvider]{
					name:   agent + " under auth mode Env is reported at auth",
					mutate: envMode(name, "OPENROUTER_API_KEY"),
					want:   atAuth,
				})
		}
	}
	for agent, names := range signInEnv {
		for _, variable := range names {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s on %s under auth mode Env is accepted", agent, variable),
					mutate: envMode(agent, variable),
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s with %s in env under Subscription is refused", agent, variable),
					mutate: inEnvBesideBundle(agent, variable),
					want:   msgCLIEnvOnly,
				},
				createCase[*v1.LLMProvider]{
					name: fmt.Sprintf("%s with %s in env under APIKey is refused", agent, variable),
					mutate: func(p *v1.LLMProvider) {
						inEnvBesideBundle(agent, variable)(p)
						p.Spec.Auth = metered
					},
					want: msgCLIEnvOnly,
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s in env under Subscription is reported at env", variable),
					mutate: inEnvBesideBundle(agent, variable),
					want:   atEnv,
				},
				createCase[*v1.LLMProvider]{
					// The name signs only its own CLI in: to OpenCode it
					// is a variable like any other.
					name:   fmt.Sprintf("another CLI with %s in env under Subscription is accepted", variable),
					mutate: inEnvBesideBundle(v1.CLIAgentOpenCode, variable),
				})
		}
	}
	// OpenCode splits its model at the first slash into a provider and a
	// model id, and Crewlet refuses a model with nothing but white space on
	// either side of it, white space as Go's strings.TrimSpace reads it: the
	// Unicode spaces beyond ASCII's among it.
	openCodeModel := func(model string, runIn v1.SandboxPlacement) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
			p.Spec.RunIn = runIn
			p.Spec.Model = model
		}
	}
	for _, runIn := range []v1.SandboxPlacement{"", v1.SandboxPlacementDirect, v1.SandboxPlacementE2B} {
		for _, model := range []string{
			modelViaAnthropic, modelViaOpenRouter,
			" anthropic/claude-sonnet-5", "anthropic/ claude-sonnet-5", "a/b",
		} {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("an OpenCode model %q with runIn %q is accepted", model, runIn),
				mutate: openCodeModel(model, runIn),
			})
		}
		for _, model := range []string{
			"claude-sonnet-5", "anthropic/", "/claude-sonnet-5", "/", " /claude-sonnet-5",
			"anthropic/ ", "anthropic/\t\n", "\u00a0/claude-sonnet-5", "anthropic/\u3000",
			"\u2028/x", "anthropic/\u0085", "   ",
		} {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("an OpenCode model %q with runIn %q is refused", model, runIn),
				mutate: openCodeModel(model, runIn),
				want:   msgOpenCodeModel,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			name:   "an OpenCode model with no provider is reported at model",
			mutate: openCodeModel(claudeModel, ""),
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

// TestLLMProviderEnvSignInEmptyEnv proves an explicit empty env signs a CLI in
// to nothing where env is its sign-in: an env sign-in CLI's under
// Subscription, and every CLI's that takes auth mode Env under it. has() is
// true of a list written as [], which only an unstructured object can send,
// since the typed client drops an empty list.
func TestLLMProviderEnvSignInEmptyEnv(t *testing.T) {
	namespace := newNamespace(t)
	type probe struct {
		agent v1.CLIAgentName
		mode  v1.CLIAgentAuthMode
		want  string
	}
	probes := make([]probe, 0, 2*len(envSignIn)+len(signInEnv))
	for _, agent := range envSignIn {
		probes = append(probes,
			probe{agent: agent, mode: v1.CLIAgentAuthSubscription, want: msgCLINoLogin},
			probe{agent: agent, mode: v1.CLIAgentAuthEnv, want: msgCLIEnvUnset})
	}
	for _, agent := range slices.Sorted(maps.Keys(signInEnv)) {
		probes = append(probes, probe{agent: agent, mode: v1.CLIAgentAuthEnv, want: msgCLIEnvUnset})
	}
	for _, pr := range probes {
		t.Run(string(pr.agent)+" "+string(pr.mode), func(t *testing.T) {
			p := llmProvider(namespace, strings.ToLower(string(pr.agent)))
			p.Spec.Agent = pr.agent
			p.Spec.Model = modelViaAnthropic
			p.Spec.Auth = v1.CLIAgentAuth{Mode: pr.mode}
			content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(p)
			if err != nil {
				t.Fatal(err)
			}
			obj := &unstructured.Unstructured{Object: content}
			obj.SetGroupVersionKind(v1.GroupVersion.WithKind("LLMProvider"))
			if err := unstructured.SetNestedSlice(obj.Object, []any{}, "spec", "env"); err != nil {
				t.Fatal(err)
			}
			expect(t, k8sClient.Create(ctx, obj), pr.want)
		})
	}
}
