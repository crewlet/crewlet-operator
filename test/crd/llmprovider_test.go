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
			name:   "a provider with no harness is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Harness = "" },
			want:   "spec.harness: Required value",
		},
		{
			name:   "a provider with no auth is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.HarnessAuth{} },
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
// Denied to the harnesses Crewlet can turn the web off for, on an LLMProvider and
// on a platform provider alike.
const msgWebTools = "webTools Denied takes a harness Crewlet can turn the web off for"

// TestLLMProviderWebToolsValidation covers the web stance: Denied on a harness
// whose Crewlet profile carries a web denial is accepted, Denied on any other
// is refused, and Allowed is every harness's. The render suite holds the rule's
// list of harnesses to the engine's own verdict
// (TestContractWebToolsDeniedFollowsTheEngine).
func TestLLMProviderWebToolsValidation(t *testing.T) {
	stance := func(harness v1.Harness, web v1.HarnessWebTools) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Harness = harness
			p.Spec.WebTools = web
		}
	}
	runCreateCases(t, llmProvider, []createCase[*v1.LLMProvider]{
		{name: "web denied on Claude Code is accepted", mutate: stance(v1.HarnessClaudeCode, v1.HarnessWebToolsDenied)},
		{name: "web denied on Codex is accepted", mutate: stance(v1.HarnessCodex, v1.HarnessWebToolsDenied)},
		{name: "web allowed on Gemini CLI is accepted", mutate: stance(v1.HarnessGeminiCLI, v1.HarnessWebToolsAllowed)},
		{
			name:   "web denied on Gemini CLI is refused",
			mutate: stance(v1.HarnessGeminiCLI, v1.HarnessWebToolsDenied),
			want:   msgWebTools,
		},
		{
			name:   "web denied on Grok is refused",
			mutate: stance(v1.HarnessGrok, v1.HarnessWebToolsDenied),
			want:   msgWebTools,
		},
		{
			name:   "a web stance outside the enum is refused",
			mutate: stance(v1.HarnessClaudeCode, "Off"),
			want:   "spec.webTools: Unsupported value",
		},
	})
}

// The phrases the OpenCode model rule's message quotes from the engine's own
// refusal of such a model: the error OpenCode 2 refuses it with, and the
// command that lists the providers OpenCode knows. TestHarnessModelFollowsTheEngine
// holds the rule's message, the engine's refusal and docs/guides/harnesses.md
// to both.
const (
	openCodeModelRefusal = "Invalid model reference"
	openCodeModelListing = "opencode models --standalone"
)

// The messages of the rules over a provider's harness and its sign-in.
const (
	msgHarnessKey   = "apiKeySecretRef is the key auth mode APIKey hands the harness"
	msgHarnessRunIn = "runIn places the provider's executor runs, and Crewlet launches an executor run " +
		"only on ClaudeCode or OpenCode"
	msgHarnessToken       = "auth.tokenSecretRef is a headless token, which Crewlet hands only to ClaudeCode"
	msgKeyHarness         = "auth mode APIKey takes a harness Crewlet hands a key to"
	msgHarnessTwoLogins   = "tokenSecretRef and credentialBundleSecretRef are two logins"
	msgOpenCodeModel      = "an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash"
	msgHarnessNoLogin     = "a Subscription login needs auth.tokenSecretRef or auth.credentialBundleSecretRef"
	msgHarnessLoginFields = "tokenSecretRef and credentialBundleSecretRef sign in to a subscription"
	msgHarnessOwnVariable = "env may not set the harness's own token or key variable"
	msgHarnessIsolation   = "env may not set a variable Crewlet sets on every call of the harness to keep it inside its " +
		"seat's own home"
	msgHarnessSubscriptionEnv = "under auth mode Subscription, env may not set a variable Crewlet sets there to sign " +
		"the harness in with the login the seat's home holds"
	msgQwenKeyOnly     = "QwenCode signs in with its key alone, under auth mode APIKey"
	msgHarnessZone     = "env may not set TZ: Crewlet hands every harness it starts the engine's own zone"
	msgEnvHarness      = "auth mode Env takes a harness Crewlet signs in from its env"
	msgHarnessEnvUnset = "auth mode Env signs the harness in with env alone, and env sets nothing that signs it in"
	msgHarnessEnvOnly  = "env may set a variable that signs the harness in from env"
	// atEnv and atAuth are how the API server reports a rule on spec that
	// names env, or auth, as the field to change.
	atEnv  = "spec.env: Invalid value"
	atAuth = "spec.auth: Invalid value"
)

// TestLLMProviderSignInValidation covers the rules over a provider's harness and
// how it signs in, each of which Crewlet's admission holds too (the CRD
// suite's engine tests hold the lists they name to it).
func TestLLMProviderSignInValidation(t *testing.T) {
	// tokenProvider is Claude Code on a headless token.
	tokenProvider := func(namespace, name string) *v1.LLMProvider {
		p := llmProvider(namespace, name)
		p.Spec.Model = "sonnet"
		p.Spec.Auth = v1.HarnessAuth{TokenSecretRef: secretKey("claude", "oauthToken")}
		return p
	}
	inE2B := func(p *v1.LLMProvider) { p.Spec.RunIn = v1.SandboxPlacementE2B }
	// metered signs the provider in with auth mode APIKey on one key.
	metered := v1.HarnessAuth{Mode: v1.HarnessAuthAPIKey, APIKeySecretRef: secretKey(providerName, apiKeyKey)}
	// keyInEnv signs harness in to a subscription with no login of its own,
	// its model provider's key in env: the route Crewlet's Hermes, Pi and
	// OpenCode profiles document, as they name no key variable for the
	// engine to fill.
	keyInEnv := func(harness v1.Harness, key string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Harness = harness
			p.Spec.Auth = v1.HarnessAuth{Mode: v1.HarnessAuthSubscription}
			p.Spec.Env = []v1.SecretEnvVar{{Name: key, ValueFrom: fromSecret("model-keys", "key")}}
		}
	}

	cases := []createCase[*v1.LLMProvider]{
		{name: "Claude Code on a headless token is accepted"},
		{
			name: "every setting is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.RunIn = v1.SandboxPlacementE2B
				p.Spec.Auth.Mode = v1.HarnessAuthSubscription
				p.Spec.Env = []v1.SecretEnvVar{{Name: envProxy, ValueFrom: fromSecret("claude", "proxy")}}
				p.Spec.TimeoutSeconds = 900
				p.Spec.MaxConcurrent = 2
				p.Spec.LoginName = "claude"
			},
		},
		{
			name: "Codex on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Harness = v1.HarnessCodex
				p.Spec.Auth = v1.HarnessAuth{CredentialBundleSecretRef: secretKey("codex", "bundle")}
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
				p.Spec.Harness = v1.HarnessCodex
				p.Spec.Auth = metered
			},
		},
		{
			// Crewlet hands the run the model as the provider's text
			// calls pass it, and exports a key in env into the remote
			// sandbox, where it signs OpenCode in.
			name: "OpenCode in E2B on its model provider's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessOpenCode, envAnthropicKey)(p)
				inE2B(p)
			},
		},
		{
			name: "OpenCode run Direct through a gateway's key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessOpenCode, "OPENROUTER_API_KEY")(p)
				p.Spec.RunIn = v1.SandboxPlacementDirect
				p.Spec.Model = modelViaOpenRouter
			},
		},
		{
			// Where a login can follow a run is the engine's to judge at
			// each launch, on the seat the run is for, whose own
			// spec.sandbox may sign the harness in: the operator reports a
			// seat whose executor it cannot sign in on the Seat's Ready
			// (SandboxLoginNotPortable), and admits the provider.
			name: "OpenCode in E2B on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Model = modelViaAnthropic
				p.Spec.Harness = v1.HarnessOpenCode
				p.Spec.Auth = v1.HarnessAuth{CredentialBundleSecretRef: secretKey("opencode", "bundle")}
			},
		},
		{
			name: "Claude Code in E2B on an exported login is accepted",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Auth = v1.HarnessAuth{CredentialBundleSecretRef: secretKey("claude", "bundle")}
			},
		},
		{
			name: "OpenCode in E2B on a proxy alone in env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessOpenCode, envProxy)(p)
				inE2B(p)
			},
			want: msgHarnessNoLogin,
		},
		{
			// Hermes signs in through env too, and has no in-box runner.
			name: "Hermes with a runIn is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessHermes, "OPENROUTER_API_KEY")(p)
				inE2B(p)
			},
			want: msgHarnessRunIn,
		},
		{
			name: "a headless token for a harness other than Claude Code is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Harness = v1.HarnessCodex
			},
			want: msgHarnessToken,
		},
		{
			// Crewlet's cursor-agent profile names CURSOR_API_KEY, a
			// Cursor account's key.
			name: "CursorAgent on a metered key is accepted",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Model = "auto"
				p.Spec.Harness = v1.HarnessCursorAgent
				p.Spec.Auth = metered
			},
		},
		{
			name: "auth mode APIKey for a harness with no key variable is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Harness = v1.HarnessHermes
				p.Spec.Auth = metered
			},
			want: msgKeyHarness,
		},
		{
			name: "a headless token beside an exported login is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("claude", "bundle")
			},
			want: msgHarnessTwoLogins,
		},
		{
			name: "a key outside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgHarnessKey,
		},
		{
			// Crewlet would read the harness's key variable from its secret
			// store and the engine's environment, which are not this
			// object's to name.
			name:   "auth mode APIKey without a key is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.HarnessAuth{Mode: v1.HarnessAuthAPIKey} },
			want:   msgHarnessKey,
		},
		{
			name: "a runIn on a harness with no in-box runner is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.RunIn = v1.SandboxPlacementDirect
				p.Spec.Harness = v1.HarnessCodex
				p.Spec.Auth = metered
			},
			want: msgHarnessRunIn,
		},
		{
			name: "a runIn refusal is reported at runIn",
			mutate: func(p *v1.LLMProvider) {
				inE2B(p)
				p.Spec.Harness = v1.HarnessCodex
				p.Spec.Auth = metered
			},
			want: "spec.runIn: Invalid value",
		},
		{
			name:   "a subscription with no login is refused",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.HarnessAuth{Mode: v1.HarnessAuthSubscription} },
			want:   msgHarnessNoLogin,
		},
		{
			// The rule sits on spec, which sees the harness and env, and
			// reports the field a person has to change.
			name:   "a subscription with no login is reported at auth",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.HarnessAuth{Mode: v1.HarnessAuthSubscription} },
			want:   atAuth,
		},
		{
			// Crewlet compares a name's parts ignoring case.
			name:   "Pi on a key in env under a lower-case name is accepted",
			mutate: keyInEnv(v1.HarnessPi, "openrouter_api_key"),
		},
		{
			// A name whose parts are KEY or TOKEN only inside a longer
			// word holds no credential to Crewlet, which reports such a
			// harness signed in to nothing.
			name:   "Hermes on a count of model tokens in env is refused",
			mutate: keyInEnv(v1.HarnessHermes, "HERMES_MAX_TOKENS"),
			want:   msgHarnessNoLogin,
		},
		{
			// Two sign-ins, which Hermes settles itself: a variable the
			// bundle's .env also sets takes the file's value, and a model
			// provider's key in env wins over an OAuth login in the
			// bundle's auth.json. Neither is refused.
			name: "Hermes on an exported login beside a key in env is accepted",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("hermes", "bundle")
			},
		},
		{
			name: "Hermes on a subscription with neither a login nor env is refused",
			mutate: func(p *v1.LLMProvider) {
				keyInEnv(v1.HarnessHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.Env = nil
			},
			want: msgHarnessNoLogin,
		},
		{
			name: "a subscription token beside auth mode APIKey is refused",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Auth.Mode = v1.HarnessAuthAPIKey
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgHarnessLoginFields,
		},
		{
			// A credential in the engine's environment is one a ${NAME}
			// in many fields of the company document can have the engine
			// send anywhere, and the harness's own children can read.
			name:   "a login inherited from the engine's environment is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.Auth = v1.HarnessAuth{Mode: "InheritEnv"} },
			want:   `Unsupported value: "InheritEnv"`,
		},
		{
			name:   "Crewlet's custom profile is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.Harness = "Custom" },
			want:   `Unsupported value: "Custom"`,
		},
		{
			// An engine pod has no container runtime to run one in.
			name:   "a container run is not offered",
			mutate: func(p *v1.LLMProvider) { p.Spec.RunIn = "Container" },
			want:   `Unsupported value: "Container"`,
		},
	}
	// The exception is the env sign-in harnesses' alone, so every other harness the
	// API offers is refused a key in env, read from the installed CRD's enum
	// so a harness added later is held to the rule without being listed here. A
	// harness whose profile names a key variable (Codex's CODEX_API_KEY) has it
	// dropped under Subscription, so the key would never reach it; one whose
	// profile names none (KimiCode) reads a key only from its own files.
	for _, harness := range llmProviderEnum(t, "spec", "harness") {
		if slices.Contains(envSignIn, v1.Harness(harness)) {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   harness + " on its model provider's key in env is accepted",
					mutate: keyInEnv(v1.Harness(harness), "OPENROUTER_API_KEY"),
				},
				createCase[*v1.LLMProvider]{
					// A proxy is configuration, and signs the harness in
					// to nothing.
					name:   harness + " on a proxy alone in env is refused",
					mutate: keyInEnv(v1.Harness(harness), envProxy),
					want:   msgHarnessNoLogin,
				})
			continue
		}
		cases = append(cases, createCase[*v1.LLMProvider]{
			name:   harness + " on a key in env is refused",
			mutate: keyInEnv(v1.Harness(harness), "OPENROUTER_API_KEY"),
			want:   msgHarnessNoLogin,
		})
	}
	// Crewlet's auth owns the variables a profile names for a token and a
	// key, and refuses each in env, in every auth mode: whether a value
	// there reached the harness would turn on the mode and on what else holds a
	// credential. Each harness is signed in some other way the API admits, a
	// bundle beside a token variable and its own key beside a key variable,
	// so the variable is the only fault.
	bundle := v1.HarnessAuth{CredentialBundleSecretRef: secretKey("harness", "bundle")}
	for _, harness := range slices.Sorted(maps.Keys(harnessVariables)) {
		own := harnessVariables[harness]
		for _, variable := range []string{own.headless, own.key} {
			if variable == "" {
				continue
			}
			cases = append(cases, createCase[*v1.LLMProvider]{
				name: fmt.Sprintf("%s with its own %s in env is refused", harness, variable),
				mutate: func(p *v1.LLMProvider) {
					p.Spec.Harness = harness
					p.Spec.Auth = bundle
					if variable == own.key {
						p.Spec.Auth = metered
					}
					p.Spec.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("harness", "value")}}
				},
				want: msgHarnessOwnVariable,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			// The rule sits on spec, and reports the field to change.
			name: "a harness's own variable in env is reported at env",
			mutate: func(p *v1.LLMProvider) {
				p.Spec.Env = []v1.SecretEnvVar{{Name: envAnthropicKey, ValueFrom: fromSecret("harness", "value")}}
			},
			want: atEnv,
		},
		createCase[*v1.LLMProvider]{
			// Another harness's variable is that harness's business: OpenCode
			// reads ANTHROPIC_API_KEY as its model provider's key.
			name:   "another harness's key variable in env is accepted",
			mutate: keyInEnv(v1.HarnessOpenCode, envAnthropicKey),
		},
	)
	// Crewlet sets HOME, the XDG directories and TMPDIR on every call of
	// every harness, and each profile's own configuration directory or switch,
	// to keep the harness inside its seat's home and its call's working
	// directory; env is layered over them, so Crewlet refuses each in env.
	// Each harness is signed in with a bundle, which the API admits for every
	// one, so the variable is the only fault.
	inEnvBesideBundle := func(harness v1.Harness, variable string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Harness = harness
			p.Spec.Auth = bundle
			p.Spec.Env = []v1.SecretEnvVar{{Name: variable, ValueFrom: fromSecret("harness", "value")}}
		}
	}
	for _, harness := range llmProviderEnum(t, "spec", "harness") {
		name := v1.Harness(harness)
		for _, variable := range append(slices.Clone(homeIsolation), harnessIsolation[name]...) {
			cases = append(cases, createCase[*v1.LLMProvider]{
				name:   fmt.Sprintf("%s with %s in env is refused", harness, variable),
				mutate: inEnvBesideBundle(name, variable),
				want:   msgHarnessIsolation,
			})
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			// The rule sits on spec, and reports the field to change.
			name:   "an isolation variable in env is reported at env",
			mutate: inEnvBesideBundle(v1.HarnessClaudeCode, "HOME"),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			// A profile's own directory is its harness's alone: OpenCode
			// does not read Codex's.
			name:   "another harness's configuration directory in env is accepted",
			mutate: inEnvBesideBundle(v1.HarnessOpenCode, "CODEX_HOME"),
		},
		createCase[*v1.LLMProvider]{
			// Crewlet matches a variable's name exactly, as the
			// environment does.
			name:   "an isolation variable's name in lower case is accepted",
			mutate: inEnvBesideBundle(v1.HarnessClaudeCode, "home"),
		},
		createCase[*v1.LLMProvider]{
			name:   "TZ in env is reported at env",
			mutate: inEnvBesideBundle(v1.HarnessClaudeCode, "TZ"),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			name:   "a zone's name in lower case is accepted",
			mutate: inEnvBesideBundle(v1.HarnessClaudeCode, "tz"),
		},
	)
	// Crewlet hands every harness it starts the engine's own zone, and reads its
	// limit resets in it, so it refuses TZ in env for every harness.
	for _, harness := range llmProviderEnum(t, "spec", "harness") {
		cases = append(cases, createCase[*v1.LLMProvider]{
			name:   harness + " with TZ in env is refused",
			mutate: inEnvBesideBundle(v1.Harness(harness), "TZ"),
			want:   msgHarnessZone,
		})
	}
	// Qwen Code keeps no login of its own in the builds Crewlet supports, so
	// under Subscription it would sign in to nothing, whatever it is handed.
	qwen := func(auth v1.HarnessAuth) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = "qwen3-coder-plus"
			p.Spec.Harness = v1.HarnessQwenCode
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
	// Under Subscription Crewlet sets, after env, what points a harness that picks
	// its sign-in from its environment at the login its seat's home holds
	// (GeminiCLI's GOOGLE_GENAI_USE_GCA), so it refuses that variable in env
	// there; under APIKey it sets nothing, and the variable is the operator's.
	for harness, variables := range subscriptionEnv {
		for _, variable := range variables {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s with %s in env under Subscription is refused", harness, variable),
					mutate: inEnvBesideBundle(harness, variable),
					want:   msgHarnessSubscriptionEnv,
				},
				createCase[*v1.LLMProvider]{
					name: fmt.Sprintf("%s with %s in env under APIKey is accepted", harness, variable),
					mutate: func(p *v1.LLMProvider) {
						inEnvBesideBundle(harness, variable)(p)
						p.Spec.Auth = metered
					},
				},
				createCase[*v1.LLMProvider]{
					// Another harness's variable is that harness's business.
					name:   fmt.Sprintf("another harness with %s in env under Subscription is accepted", variable),
					mutate: inEnvBesideBundle(v1.HarnessClaudeCode, variable),
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s in env under Subscription is reported at env", variable),
					mutate: inEnvBesideBundle(harness, variable),
					want:   atEnv,
				})
		}
	}
	// Under auth mode Env the provider's env alone signs its harness in: Crewlet
	// hands it neither its token nor its key variable and seeds no login.
	// So env must set what signs the harness in, one of the names its profile
	// lists (sign_in_env) or, for an env sign-in harness, a credential-named
	// one, and a harness whose profile has neither cannot take the mode. A name
	// of sign_in_env is refused under every other mode, which hands the harness a
	// credential of its own beside it.
	envMode := func(harness v1.Harness, names ...string) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			p.Spec.Model = modelViaAnthropic
			p.Spec.Harness = harness
			p.Spec.Auth = v1.HarnessAuth{Mode: v1.HarnessAuthEnv}
			p.Spec.Env = nil
			for _, name := range names {
				p.Spec.Env = append(p.Spec.Env, v1.SecretEnvVar{Name: name, ValueFrom: fromSecret("cloud", name)})
			}
		}
	}
	cases = append(cases,
		createCase[*v1.LLMProvider]{
			name: "Claude Code on Bedrock under auth mode Env is accepted",
			mutate: envMode(v1.HarnessClaudeCode,
				"CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "AWS_BEARER_TOKEN_BEDROCK"),
		},
		createCase[*v1.LLMProvider]{
			name:   "Claude Code through a gateway's token under auth mode Env is accepted",
			mutate: envMode(v1.HarnessClaudeCode, envBaseURL, "ANTHROPIC_AUTH_TOKEN"),
		},
		createCase[*v1.LLMProvider]{
			name: "Claude Code in E2B under auth mode Env is accepted",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.HarnessClaudeCode, "CLAUDE_CODE_USE_VERTEX", "CLOUD_ML_REGION")(p)
				inE2B(p)
			},
		},
		createCase[*v1.LLMProvider]{
			// A gateway's address says where the calls go, not whom
			// they are made as.
			name:   "Claude Code on a gateway's address alone under auth mode Env is refused",
			mutate: envMode(v1.HarnessClaudeCode, envBaseURL),
			want:   msgHarnessEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name:   "Claude Code under auth mode Env with no env is refused",
			mutate: envMode(v1.HarnessClaudeCode),
			want:   msgHarnessEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name:   "an env that signs nothing in under auth mode Env is reported at env",
			mutate: envMode(v1.HarnessClaudeCode, envBaseURL),
			want:   atEnv,
		},
		createCase[*v1.LLMProvider]{
			// Another harness's key signs Claude Code in to nothing.
			name:   "Claude Code on a model provider's key under auth mode Env is refused",
			mutate: envMode(v1.HarnessClaudeCode, "OPENROUTER_API_KEY"),
			want:   msgHarnessEnvUnset,
		},
		createCase[*v1.LLMProvider]{
			name: "a headless token under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.HarnessClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.TokenSecretRef = secretKey("claude", "oauthToken")
			},
			want: msgHarnessLoginFields,
		},
		createCase[*v1.LLMProvider]{
			name: "a bundle under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.HarnessClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.CredentialBundleSecretRef = secretKey("claude", "bundle")
			},
			want: msgHarnessLoginFields,
		},
		createCase[*v1.LLMProvider]{
			name: "a key under auth mode Env is refused",
			mutate: func(p *v1.LLMProvider) {
				envMode(v1.HarnessClaudeCode, "ANTHROPIC_AUTH_TOKEN")(p)
				p.Spec.Auth.APIKeySecretRef = secretKey(providerName, apiKeyKey)
			},
			want: msgHarnessKey,
		},
		createCase[*v1.LLMProvider]{
			name:   "the harness's own key variable in env under auth mode Env is refused",
			mutate: envMode(v1.HarnessClaudeCode, "ANTHROPIC_AUTH_TOKEN", envAnthropicKey),
			want:   msgHarnessOwnVariable,
		},
	)
	for _, harness := range llmProviderEnum(t, "spec", "harness") {
		name := v1.Harness(harness)
		switch {
		case slices.Contains(envSignIn, name):
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   harness + " on its model provider's key under auth mode Env is accepted",
					mutate: envMode(name, "OPENROUTER_API_KEY"),
				},
				createCase[*v1.LLMProvider]{
					name:   harness + " on a proxy alone under auth mode Env is refused",
					mutate: envMode(name, envProxy),
					want:   msgHarnessEnvUnset,
				},
				createCase[*v1.LLMProvider]{
					// A cloud switch is no credential, and signs only
					// Claude Code in.
					name:   harness + " on Claude Code's cloud switch under auth mode Env is refused",
					mutate: envMode(name, "CLAUDE_CODE_USE_BEDROCK"),
					want:   msgHarnessEnvUnset,
				})
		case len(signInEnv[name]) > 0:
			// Each of its names is tried below, under every mode.
		default:
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   harness + " under auth mode Env is refused",
					mutate: envMode(name, "OPENROUTER_API_KEY", "ANTHROPIC_AUTH_TOKEN"),
					want:   msgEnvHarness,
				},
				createCase[*v1.LLMProvider]{
					name:   harness + " under auth mode Env is reported at auth",
					mutate: envMode(name, "OPENROUTER_API_KEY"),
					want:   atAuth,
				})
		}
	}
	for harness, names := range signInEnv {
		for _, variable := range names {
			cases = append(cases,
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s on %s under auth mode Env is accepted", harness, variable),
					mutate: envMode(harness, variable),
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s with %s in env under Subscription is refused", harness, variable),
					mutate: inEnvBesideBundle(harness, variable),
					want:   msgHarnessEnvOnly,
				},
				createCase[*v1.LLMProvider]{
					name: fmt.Sprintf("%s with %s in env under APIKey is refused", harness, variable),
					mutate: func(p *v1.LLMProvider) {
						inEnvBesideBundle(harness, variable)(p)
						p.Spec.Auth = metered
					},
					want: msgHarnessEnvOnly,
				},
				createCase[*v1.LLMProvider]{
					name:   fmt.Sprintf("%s in env under Subscription is reported at env", variable),
					mutate: inEnvBesideBundle(harness, variable),
					want:   atEnv,
				},
				createCase[*v1.LLMProvider]{
					// The name signs only its own harness in: to OpenCode it
					// is a variable like any other.
					name:   fmt.Sprintf("another harness with %s in env under Subscription is accepted", variable),
					mutate: inEnvBesideBundle(v1.HarnessOpenCode, variable),
				})
		}
	}
	// OpenCode splits its model at the first slash into a provider and a
	// model id, and Crewlet refuses a model with nothing but white space on
	// either side of it, white space as Go's strings.TrimSpace reads it: the
	// Unicode spaces beyond ASCII's among it.
	openCodeModel := func(model string, runIn v1.SandboxPlacement) func(*v1.LLMProvider) {
		return func(p *v1.LLMProvider) {
			keyInEnv(v1.HarnessOpenCode, "OPENROUTER_API_KEY")(p)
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
				keyInEnv(v1.HarnessHermes, "OPENROUTER_API_KEY")(p)
				p.Spec.Model = claudeModel
			},
		},
	)
	runCreateCases(t, tokenProvider, cases)
}

// TestLLMProviderEnvSignInEmptyEnv proves an explicit empty env signs a harness in
// to nothing where env is its sign-in: an env sign-in harness's under
// Subscription, and every harness's that takes auth mode Env under it. has() is
// true of a list written as [], which only an unstructured object can send,
// since the typed client drops an empty list.
func TestLLMProviderEnvSignInEmptyEnv(t *testing.T) {
	namespace := newNamespace(t)
	type probe struct {
		harness v1.Harness
		mode    v1.HarnessAuthMode
		want    string
	}
	probes := make([]probe, 0, 2*len(envSignIn)+len(signInEnv))
	for _, harness := range envSignIn {
		probes = append(probes,
			probe{harness: harness, mode: v1.HarnessAuthSubscription, want: msgHarnessNoLogin},
			probe{harness: harness, mode: v1.HarnessAuthEnv, want: msgHarnessEnvUnset})
	}
	for _, harness := range slices.Sorted(maps.Keys(signInEnv)) {
		probes = append(probes, probe{harness: harness, mode: v1.HarnessAuthEnv, want: msgHarnessEnvUnset})
	}
	for _, pr := range probes {
		t.Run(string(pr.harness)+" "+string(pr.mode), func(t *testing.T) {
			p := llmProvider(namespace, strings.ToLower(string(pr.harness)))
			p.Spec.Harness = pr.harness
			p.Spec.Model = modelViaAnthropic
			p.Spec.Auth = v1.HarnessAuth{Mode: pr.mode}
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
