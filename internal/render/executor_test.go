package render

import (
	"cmp"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The keys the providers the executor tests add are signed in under, and
// the variable a harness signed in through env reads its model provider's key
// from.
const (
	keyAPIKey     = "apiKey"
	keyBundle     = "bundle"
	envGatewayKey = "OPENROUTER_API_KEY"
)

// cySecret is the Secret seat cy's own values are read from,
// loginStaysBehind what the report says of a login that cannot follow a run
// into an E2B box, and toDirect the move to Direct its advice ends on.
const (
	cySecret         = "cy-token"
	loginStaysBehind = "OpenCode login cannot follow it"
	toDirect         = "to Direct"
)

// The models the executor tests' providers run, each in its harness's own
// grammar: OpenCode's names the provider it routes through.
const (
	modelSonnet   = "sonnet"
	modelCodex    = "gpt-5-codex"
	modelOpenCode = "openrouter/anthropic/claude-sonnet-5"
	modelCopilot  = "claude-sonnet-4.5"
	modelGemini   = "gemini-2.5-pro"
	modelKimi     = "kimi-k2-turbo-preview"
	modelMuse     = "muse-1"
)

// The providers the executor tests add to the seat-pools case, beside its
// own ClaudeCode provider claude.
const (
	providerCodex    = "codex"
	providerOpenCode = "opencode"
	providerCopilot  = "copilot"
	providerGemini   = "gemini"
	providerKimi     = "kimi"
	providerMuse     = "muse"
	providerQwenCode = "qwen"
	platformHeadName = "class-claude"
)

// codexProvider is a provider of Codex on a key, which serves a seat's other
// phases and never its executor.
func codexProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerCodex,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Harness: v1alpha1.HarnessCodex, Model: modelCodex},
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerCodex, Key: keyAPIKey},
			},
		},
	}
}

// geminiProvider is a provider of GeminiCLI signed in by its key, placed in
// E2B by its runIn.
func geminiProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerGemini,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Harness: v1alpha1.HarnessGeminiCLI, Model: modelGemini,
			},
			RunIn: v1alpha1.SandboxPlacementE2B,
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerGemini, Key: keyAPIKey},
			},
		},
	}
}

// museProvider is a provider of MuseCode signed in by its key, placed in E2B
// by its runIn.
func museProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerMuse,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Harness: v1alpha1.HarnessMuseCode, Model: modelMuse,
			},
			RunIn: v1alpha1.SandboxPlacementE2B,
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerMuse, Key: keyAPIKey},
			},
		},
	}
}

// openCodeProvider is a provider of OpenCode signed in by its model
// provider's key in env, placed in E2B by its runIn.
func openCodeProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerOpenCode,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Harness: v1alpha1.HarnessOpenCode, Model: modelOpenCode,
			},
			RunIn: v1alpha1.SandboxPlacementE2B,
			Auth:  v1alpha1.HarnessAuth{Mode: v1alpha1.HarnessAuthSubscription},
			Env: []v1alpha1.SecretEnvVar{{
				Name:      envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "openrouter", Key: keyAPIKey}},
			}},
		},
	}
}

// copilotProvider is a provider of Copilot on a GitHub token under auth mode
// APIKey, which Crewlet hands the harness as COPILOT_GITHUB_TOKEN, placed in
// E2B by its runIn.
func copilotProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerCopilot,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Harness: v1alpha1.HarnessCopilot, Model: modelCopilot},
			RunIn:               v1alpha1.SandboxPlacementE2B,
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerCopilot, Key: keyAPIKey},
			},
		},
	}
}

// kimiProvider is a provider of KimiCode on a Moonshot key under auth mode
// APIKey, which Crewlet hands the harness as KIMI_MODEL_API_KEY, placed in E2B
// by its runIn.
func kimiProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerKimi,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Harness: v1alpha1.HarnessKimiCode, Model: modelKimi},
			RunIn:               v1alpha1.SandboxPlacementE2B,
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerKimi, Key: keyAPIKey},
			},
		},
	}
}

// qwenCodeProvider is a provider of Qwen Code on its key, placed in E2B,
// which E2B prebuilds no template for.
func qwenCodeProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerQwenCode,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Harness: v1alpha1.HarnessQwenCode, Model: qwenModel,
			},
			RunIn: v1alpha1.SandboxPlacementE2B,
			Auth: v1alpha1.HarnessAuth{
				Mode: v1alpha1.HarnessAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerQwenCode, Key: keyAPIKey},
			},
		},
	}
}

// withQwenTemplate gives the Company its E2B sandbox with a template of its
// own, which an E2B run of Qwen Code needs.
func withQwenTemplate(in *Input) {
	withE2B(in)
	in.Company.Spec.Sandbox.E2B.Template = "qwen-code"
}

// withMuseTemplate gives the Company its E2B sandbox with a template of its
// own, which an E2B run of Muse Code needs.
func withMuseTemplate(in *Input) {
	withE2B(in)
	in.Company.Spec.Sandbox.E2B.Template = "muse-code"
}

// bundled is the provider provider makes, signed in by a credential bundle
// alone, whose login cannot follow a run into an E2B box.
func bundled(provider func() v1alpha1.LLMProvider) v1alpha1.LLMProvider {
	p := provider()
	p.Spec.Env = nil
	p.Spec.Auth = v1alpha1.HarnessAuth{CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle}}
	return p
}

// gatewayed is the provider provider makes, signed in under auth mode Env by
// its env alone, which sets names, each from a Secret of its own.
func gatewayed(provider func() v1alpha1.LLMProvider, names ...string) v1alpha1.LLMProvider {
	p := provider()
	p.Spec.Auth = v1alpha1.HarnessAuth{Mode: v1alpha1.HarnessAuthEnv}
	p.Spec.Env = nil
	for _, name := range names {
		p.Spec.Env = append(p.Spec.Env, v1alpha1.SecretEnvVar{
			Name:      name,
			ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "cloud", Key: name}},
		})
	}
	return p
}

// withE2B gives the Company its E2B sandbox.
func withE2B(in *Input) {
	in.Company.Spec.Sandbox.E2B.APIKeySecretRef = v1alpha1.SecretKeyReference{Name: "e2b", Key: keyAPIKey}
}

// chainOfNames is a chain of the Company's own LLMProviders.
func chainOfNames(names ...string) []v1alpha1.LLMProviderReference {
	out := make([]v1alpha1.LLMProviderReference, len(names))
	for i, name := range names {
		out[i] = v1alpha1.LLMProviderReference{Name: name}
	}
	return out
}

// TestExecutor holds the report on every agent seat's executor to where
// Crewlet runs it and whether its box can reach the seat's tools from there:
// a Direct box dials its engine pod's own listener on loopback, a data pod's
// and a seat pod's alike, with or without a public address; an E2B box dials
// the public address, and runs on a login that follows it as a variable, a
// provider's token or key or the seat's own sign-in. The verdict
// judges the first member of the chain a launch can run, and names the
// members a launch falls back to and those it never runs. None of it keeps
// the document from rendering: the engine applies such a seat and fails its
// turns alone.
//
// Mutation: read Copilot's sign-in as OpenCode's: a seat's NPM_TOKEN passes.
func TestExecutor(t *testing.T) {
	const (
		public = "https://nimbus.example.com"
		// runnable is the verdict on a seat whose turns can run.
		runnable = "runnable"
		// ownListener is where a Direct box calls the seat's tools back,
		// on a data pod and a seat pod alike.
		ownListener = "on the pod's own listener, on loopback"
	)
	everySeat := func(field, verdict string) []string {
		return []string{
			"ana " + field + " " + verdict, "bo " + field + " " + verdict,
			"cy " + field + " " + verdict, "dee " + field + " " + verdict,
		}
	}
	// runnableBut is every seat runnable on the head of the order, but each
	// of verdicts, "seat field reason", in its seat's place.
	runnableBut := func(verdicts ...string) []string {
		out := everySeat(fieldLLM, runnable)
		for _, v := range verdicts {
			seat, _, _ := strings.Cut(v, " ")
			for i := range out {
				if strings.HasPrefix(out[i], seat+" ") {
					out[i] = v
				}
			}
		}
		return out
	}
	// cyOwn is cy's verdict on the head of a chain of its own.
	cyOwn := func(verdict string) string { return seatCy + " " + fieldLLMDefault + "[0].name " + verdict }
	for _, c := range []struct {
		name   string
		public string
		mutate func(*Input)
		// want is each reported seat, as "seat field reason", the reason
		// "runnable" for one whose turns can run.
		want []string
		// says are phrases every report says; of the seat named in
		// saysOf alone when it is set.
		says   []string
		saysOf string
	}{
		{
			// Every seat names no chain and runs on claude, the only
			// provider, in the Company's default cell, Direct.
			name: "Direct with a public address", public: public,
			want: everySeat(fieldLLM, runnable),
			says: []string{"as ClaudeCode on LLMProvider claude", "Company nimbus's first LLMProvider by name", "Direct"},
		},
		{
			name: "Direct on the data pods", public: public,
			want: everySeat(fieldLLM, runnable), saysOf: seatCy,
			says: []string{"on the data pod that holds the seat", ownListener},
		},
		{
			name: "Direct on a seat pod", public: public,
			want: everySeat(fieldLLM, runnable), saysOf: seatBo,
			says: []string{"on the pod of seat pool general that holds the seat", ownListener},
		},
		{
			// A seat pod serves its own boxes the bridge on loopback, as a
			// data pod does: no seat's Direct run needs an address.
			name: "Direct with no public address",
			want: everySeat(fieldLLM, runnable), saysOf: seatBo,
			says: []string{"on the pod of seat pool general that holds the seat", ownListener},
		},
		{
			name: "E2B by the provider's runIn", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				ownProvider(in, "claude").Spec.RunIn = v1alpha1.SandboxPlacementE2B
			},
			want: everySeat(fieldLLM, runnable),
			says: []string{inE2B, public + "/mcp/", "relays each call to the engine pod holding the seat"},
		},
		{
			name: "E2B by the Company's default, with no public address",
			mutate: func(in *Input) {
				withE2B(in)
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementE2B
			},
			want:   everySeat(fieldLLM, v1alpha1.ReasonNotExposed),
			saysOf: seatCy,
			says: []string{
				"Company nimbus has no public address", "spec.exposure.publicBaseURL",
				"set LLMProvider claude's spec.runIn, or the Company's spec.sandbox.defaultRunIn, to Direct",
			},
		},
		{
			// Running it Direct is the remedy in a pool too: a seat pod
			// serves its own boxes the bridge.
			name: "E2B with no public address, a seat in a pool",
			mutate: func(in *Input) {
				withE2B(in)
				ownProvider(in, "claude").Spec.RunIn = v1alpha1.SandboxPlacementE2B
			},
			want:   everySeat(fieldLLM, v1alpha1.ReasonNotExposed),
			saysOf: seatBo,
			says:   []string{"set LLMProvider claude's spec.runIn to Direct, to run it Direct on the engine pod that holds the seat"},
		},
		{
			name: "a bundled ClaudeCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"ClaudeCode login cannot follow it", "auth.tokenSecretRef", "CLAUDE_CODE_OAUTH_TOKEN", toDirect},
		},
		{
			// Under auth mode Env the provider's env is its sign-in, and
			// it follows the run into the box, so a gateway's bearer token
			// there signs the run in.
			name: "a ClaudeCode provider under auth mode Env in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := gatewayed(claudeCodeProvider, "ANTHROPIC_AUTH_TOKEN")
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{inE2B, public + "/mcp/"},
		},
		{
			// So does a cloud provider's switch, beside its credentials.
			name: "a ClaudeCode provider on Bedrock under auth mode Env in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := gatewayed(claudeCodeProvider, "CLAUDE_CODE_USE_BEDROCK", "AWS_REGION", "AWS_BEARER_TOKEN_BEDROCK")
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want: runnableBut(cyOwn(runnable)),
		},
		{
			// A gateway's address says where the calls go, not whom they
			// are made as: beside a bundle, nothing signs the run in.
			name: "a bundled ClaudeCode login in E2B beside a gateway's address", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				p.Spec.Env = gatewayed(claudeCodeProvider, "ANTHROPIC_BASE_URL").Spec.Env
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"ClaudeCode login cannot follow it", "auth mode Env", "ANTHROPIC_AUTH_TOKEN"},
		},
		{
			// QwenCode's key follows the run, under auth mode APIKey.
			name: "a QwenCode key in E2B", public: public,
			mutate: func(in *Input) {
				withQwenTemplate(in)
				in.LLMProviders = append(in.LLMProviders, qwenCodeProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as QwenCode on LLMProvider qwen", inE2B},
		},
		{
			// Its Qwen subscription login is a bundle, which stays on the
			// engine's data volume.
			name: "a bundled QwenCode login in E2B", public: public,
			mutate: func(in *Input) {
				withQwenTemplate(in)
				in.LLMProviders = append(in.LLMProviders, bundled(qwenCodeProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"QwenCode login cannot follow it", "auth mode APIKey", "OPENAI_API_KEY", "to Direct"},
		},
		{
			name: "a bundled QwenCode login in E2B, signed in by the seat", public: public,
			mutate: func(in *Input) {
				withQwenTemplate(in)
				in.LLMProviders = append(in.LLMProviders, bundled(qwenCodeProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerQwenCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("OPENAI_API_KEY")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"signed in by OPENAI_API_KEY from the Company's or the seat's spec.sandbox"},
		},
		{
			// A credential-named variable that is not its key signs it in
			// to nothing: Qwen Code reads OPENAI_API_KEY alone.
			name: "a bundled QwenCode login in E2B beside another key of the seat's", public: public,
			mutate: func(in *Input) {
				withQwenTemplate(in)
				in.LLMProviders = append(in.LLMProviders, bundled(qwenCodeProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerQwenCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("OPENROUTER_API_KEY")}}
			},
			want: runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
		},
		{
			// The box is seeded with the login on the engine pod.
			name: "a bundled ClaudeCode login Direct", public: public,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, bundled(claudeCodeProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
			},
			want: runnableBut(cyOwn(runnable)),
		},
		{
			name: "a bundled ClaudeCode login in E2B, signed in by the seat", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerClaudeCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("CLAUDE_CODE_OAUTH_TOKEN")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"signed in by CLAUDE_CODE_OAUTH_TOKEN from the Company's or the seat's spec.sandbox"},
		},
		{
			// The run's environment is the Company's setup steps' env, then
			// the seat's, then its spec.sandbox.env: a sign-in in any of
			// them signs the run in.
			name: "a bundled ClaudeCode login in E2B, signed in by a Company setup step", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
				in.Company.Spec.Sandbox.Setup = []v1alpha1.SandboxSetupStep{setupSignIn("CLAUDE_CODE_OAUTH_TOKEN")}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"signed in by CLAUDE_CODE_OAUTH_TOKEN from the Company's or the seat's spec.sandbox"},
		},
		{
			name: "a bundled ClaudeCode login in E2B, signed in by a seat's setup step", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerClaudeCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Setup: []v1alpha1.SandboxSetupStep{setupSignIn("ANTHROPIC_API_KEY")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"signed in by ANTHROPIC_API_KEY from the Company's or the seat's spec.sandbox"},
		},
		{
			// A run goes without a value that holds nothing, listed or not,
			// so the seat's sign-in does not count then.
			name: "a bundled ClaudeCode login in E2B, the seat's sign-in empty", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerClaudeCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("CLAUDE_CODE_OAUTH_TOKEN")}}
				in.Empty = []secretref.Key{{Namespace: testNamespace, Name: "cy-token", Key: "CLAUDE_CODE_OAUTH_TOKEN"}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"gives CLAUDE_CODE_OAUTH_TOKEN, which every run goes without"},
		},
		{
			name: "a bundled OpenCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(openCodeProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerOpenCode)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{loginStaysBehind, envGatewayKey},
		},
		{
			// The seat's own DigitalOcean token, handed to doctl, is never
			// the run's sign-in, although OpenCode reads its name as a model
			// provider's key: Crewlet judges the sign-in without it.
			name: "a seat's DigitalOcean token beside a bundled OpenCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(openCodeProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerOpenCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{{
					Name: "DIGITALOCEAN_ACCESS_TOKEN",
					ValueFrom: v1alpha1.SeatCredentialSource{Provisioned: v1alpha1.ProvisionedCredential{
						Vendor: v1alpha1.ProvisionedDigitalOcean, Field: v1alpha1.ProvisionedToken,
					}},
				}}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{loginStaysBehind},
		},
		{
			// Nor is a credential the seat gives its GitLab server from its
			// own Secret, handed to glab under the same key.
			name: "a seat's own GitLab token beside a bundled OpenCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(openCodeProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerOpenCode)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("GITLAB_TOKEN")}}
				cy.Spec.MCP = []v1alpha1.MCPCredentials{{Name: VendorGitLab, Env: []v1alpha1.MCPCredentialValue{{
					Name: "GITLAB_PERSONAL_ACCESS_TOKEN",
					ValueFrom: v1alpha1.SecretValueSource{
						SecretKeyRef: v1alpha1.SecretKeyReference{Name: cySecret, Key: "GITLAB_TOKEN"},
					},
				}}}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{loginStaysBehind},
		},
		{
			name: "OpenCode on its key in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, openCodeProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerOpenCode)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as OpenCode on LLMProvider opencode, in the Company's E2B sandbox"},
		},
		{
			name: "GeminiCLI on its key in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, geminiProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerGemini)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as GeminiCLI on LLMProvider gemini, in the Company's E2B sandbox"},
		},
		{
			// A GeminiCLI login is files, which stay on the engine's pod, and
			// GeminiCLI reads no other credential's name, unlike OpenCode: a
			// seat's model provider key signs nothing in.
			name: "a bundled GeminiCLI login in E2B, beside a seat's other key", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(geminiProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerGemini)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret(envGatewayKey)}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"GeminiCLI login cannot follow it", "spec.sandbox.env (GEMINI_API_KEY, GOOGLE_GENAI_USE_VERTEXAI)"},
		},
		{
			name: "GeminiCLI on Vertex AI in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, gatewayed(geminiProvider, "GOOGLE_GENAI_USE_VERTEXAI"))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerGemini)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as GeminiCLI on LLMProvider gemini, in the Company's E2B sandbox"},
		},
		{
			name: "a bundled GeminiCLI login in E2B, the seat's key signing it in", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(geminiProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerGemini)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("GEMINI_API_KEY")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says: []string{"signed in by GEMINI_API_KEY from the Company's or the seat's spec.sandbox, which Crewlet " +
				"hands the run in place of the provider's own sign-in"},
		},
		{
			name: "Copilot on its GitHub token in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, copilotProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCopilot)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as Copilot on LLMProvider copilot, in the Company's E2B sandbox", public + "/mcp/"},
		},
		{
			name: "a bundled Copilot login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(copilotProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCopilot)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says: []string{
				"Copilot login cannot follow it",
				"sign the provider in with auth mode APIKey or with auth mode Env and one of GH_TOKEN, GITHUB_TOKEN in env",
				"sign Copilot in for the seat in spec.sandbox.env (COPILOT_GITHUB_TOKEN, GH_TOKEN, GITHUB_TOKEN)",
				toDirect,
			},
		},
		{
			// Copilot's profile names the variables that sign it in, its
			// api_key_env and sign_in_env: any other name read as a
			// credential is none, as Copilot reads no model provider's key
			// from its environment.
			name: "a seat's NPM_TOKEN beside a bundled Copilot login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(copilotProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerCopilot)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("NPM_TOKEN")}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"Copilot login cannot follow it"},
		},
		{
			// GITHUB_TOKEN is one of Copilot's sign_in_env: a seat's own,
			// from a Secret, signs the run in in place of the provider's.
			name: "a bundled Copilot login in E2B, signed in by the seat's GITHUB_TOKEN", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(copilotProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerCopilot)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("GITHUB_TOKEN")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says: []string{
				"signed in by GITHUB_TOKEN from the Company's or the seat's spec.sandbox, which Crewlet hands the " +
					"run in place of the provider's own sign-in",
			},
		},
		{
			// Under auth mode Env the provider's env is its sign-in, and a
			// GitHub token there follows the run into the box.
			name: "a Copilot provider under auth mode Env in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, gatewayed(copilotProvider, "GH_TOKEN"))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCopilot)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as Copilot on LLMProvider copilot, in the Company's E2B sandbox", public + "/mcp/"},
		},
		{
			name: "a bundled Copilot login in E2B, signed in by the seat", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(copilotProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerCopilot)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("COPILOT_GITHUB_TOKEN")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says: []string{
				"signed in by COPILOT_GITHUB_TOKEN from the Company's or the seat's spec.sandbox, which Crewlet " +
					"hands the run in place of the provider's own sign-in",
			},
		},
		{
			// The box is seeded with the login on the engine pod.
			name: "a bundled Copilot login Direct", public: public,
			mutate: func(in *Input) {
				p := bundled(copilotProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementDirect
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCopilot)
			},
			want: runnableBut(cyOwn(runnable)),
		},
		{
			// Copilot is a runner a launch takes past a Codex, which the
			// report names among the harnesses that run the executor.
			name: "a Codex ahead of a Copilot", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, codexProvider(), copilotProvider())
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerCodex, providerCopilot)
			},
			want:   runnableBut(seatBo + " " + fieldLLMDefault + "[1].name " + runnable),
			saysOf: seatBo,
			says: []string{
				"as Copilot on LLMProvider copilot",
				"LLMProvider codex (Codex) never runs it, as Crewlet runs a seat's executor only on ClaudeCode, " +
					"GeminiCLI, QwenCode, OpenCode, Copilot, MuseCode or KimiCode",
			},
		},
		{
			name: "KimiCode on its key in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, kimiProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerKimi)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as KimiCode on LLMProvider kimi, in the Company's E2B sandbox", public + "/mcp/"},
		},
		{
			// KimiCode's profile names its key alone (api_key_env), and no
			// variable that signs it in from env: a login cannot follow a run
			// into E2B, and only the key can sign it in there.
			name: "a bundled KimiCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(kimiProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerKimi)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says: []string{
				"KimiCode login cannot follow it",
				"sign the provider in with auth mode APIKey, or sign KimiCode in for the seat in spec.sandbox.env " +
					"(KIMI_MODEL_API_KEY)",
				toDirect,
			},
		},
		{
			// The seat's own key signs the run in where the login cannot go.
			name: "a bundled KimiCode login in E2B, signed in by the seat's KIMI_MODEL_API_KEY", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(kimiProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerKimi)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("KIMI_MODEL_API_KEY")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says: []string{
				"signed in by KIMI_MODEL_API_KEY from the Company's or the seat's spec.sandbox, as the provider's " +
					"own KimiCode login cannot follow the run there",
			},
		},
		{
			name: "MuseCode on its key in E2B", public: public,
			mutate: func(in *Input) {
				withMuseTemplate(in)
				in.LLMProviders = append(in.LLMProviders, museProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerMuse)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as MuseCode on LLMProvider muse, in the Company's E2B sandbox"},
		},
		{
			// A MuseCode login is a file, which stays on the engine's pod, and
			// MuseCode reads no other credential's name, unlike OpenCode: a
			// seat's model provider key signs nothing in.
			name: "a bundled MuseCode login in E2B, beside a seat's other key", public: public,
			mutate: func(in *Input) {
				withMuseTemplate(in)
				in.LLMProviders = append(in.LLMProviders, bundled(museProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerMuse)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret(envGatewayKey)}}
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"MuseCode login cannot follow it", "spec.sandbox.env (META_API_KEY)"},
		},
		{
			name: "a bundled MuseCode login in E2B, the seat's key signing it in", public: public,
			mutate: func(in *Input) {
				withMuseTemplate(in)
				in.LLMProviders = append(in.LLMProviders, bundled(museProvider))
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerMuse)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("META_API_KEY")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"as MuseCode on LLMProvider muse, in the Company's E2B sandbox"},
		},
		{
			// A launch passes over a harness that cannot run the executor; the
			// verdict is the first runner's, and the report says the other
			// serves the seat's other phases.
			name: "a Codex ahead of a ClaudeCode", public: public,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, codexProvider())
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerCodex, "claude")
			},
			want:   runnableBut(seatBo + " " + fieldLLMDefault + "[1].name " + runnable),
			saysOf: seatBo,
			says:   []string{"LLMProvider codex (Codex) never runs it", "serves the seat's other phases"},
		},
		{
			// Each fallback is placed in its own cell.
			name: "a fallback in another cell", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, openCodeProvider())
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames("claude", providerOpenCode)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says: []string{
				"While LLMProvider claude sits out its spent plan on the node, a launch falls back to " +
					"LLMProvider opencode as OpenCode, in the Company's E2B sandbox",
			},
		},
		{
			// The verdict is the first runner's alone: a launch the box's
			// login refuses fails, it does not move down the chain.
			name: "a fallback that could not run", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.LLMProviders = append(in.LLMProviders, bundled(openCodeProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames("claude", providerOpenCode)
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"where Crewlet would refuse it too (SandboxLoginNotPortable)"},
		},
		{
			// Refused on its own (NoExecutorHarness): there is no executor to
			// report on.
			name: "a seat whose chain no member of runs it", public: public,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, codexProvider())
				seatNamed(in, seatBo).Spec.LLM.Default = chainOfNames(providerCodex)
			},
			want: slices.DeleteFunc(everySeat(fieldLLM, runnable), func(v string) bool { return strings.HasPrefix(v, seatBo+" ") }),
		},
		{
			// A platform provider names no runIn: its runs take the
			// Company's default, and it signs in with a key.
			name: "a platform provider at the head of the order", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementE2B
				in.Company.Spec.LLM.UsePlatformProviders = new(true)
				in.Class.Spec.PlatformLLMProviders = []v1alpha1.PlatformLLMProvider{{
					Name: platformHeadName, Harness: v1alpha1.HarnessClaudeCode, Model: "claude-sonnet-5",
					Auth: v1alpha1.PlatformLLMProviderAuth{
						Mode: v1alpha1.HarnessAuthAPIKey,
						APIKeySecretRef: v1alpha1.NamespacedSecretKeyReference{
							Namespace: "crewlet-system", Name: platformHeadName, Key: keyAPIKey,
						},
					},
				}}
				in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{
					{Kind: v1alpha1.LLMProviderKindPlatform, Name: platformHeadName},
				}
			},
			want: everySeat(fieldLLM, runnable),
			says: []string{"the platform model provider class-claude", "the head of Company nimbus's spec.llm.order", "E2B"},
		},
		{
			// Without spec.sandbox Crewlet refuses the document: there is
			// no executor to report on.
			name: "a Company with no spec.sandbox", public: public,
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			in.PublicBaseURL = c.public
			if c.mutate != nil {
				c.mutate(in)
			}
			executors := ExecutorOf(in)
			got := make([]string, 0, len(executors))
			for _, seat := range slices.Sorted(maps.Keys(executors)) {
				e := executors[seat]
				got = append(got, seat+" "+e.Field+" "+cmp.Or(e.Reason, runnable))
				if c.saysOf != "" && seat != c.saysOf {
					continue
				}
				for _, phrase := range c.says {
					if !strings.Contains(e.Message, phrase) {
						t.Errorf("the report on %s does not say %q: %s", seat, phrase, e.Message)
					}
				}
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("reported %v, want %v", got, c.want)
			}
		})
	}
}

// seatSecret is a variable of a seat's spec.sandbox.env read from the Secret
// cy-token under its own name.
func seatSecret(name string) v1alpha1.SeatEnvVar {
	return v1alpha1.SeatEnvVar{
		Name: name,
		ValueFrom: v1alpha1.SeatCredentialSource{
			SecretKeyRef: v1alpha1.SecretKeyReference{Name: cySecret, Key: name},
		},
	}
}

// setupSignIn is a setup step whose env gives the variable named, read from
// the Secret claude-token under its own name.
func setupSignIn(name string) v1alpha1.SandboxSetupStep {
	return v1alpha1.SandboxSetupStep{
		Name: "sign-in",
		Env: []v1alpha1.SandboxEnvVar{{
			Name:      name,
			ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "claude-token", Key: name}},
		}},
	}
}

// ownProvider is the Company's own LLMProvider of that name.
func ownProvider(in *Input, name string) *v1alpha1.LLMProvider {
	for i := range in.LLMProviders {
		if in.LLMProviders[i].Name == name {
			return &in.LLMProviders[i]
		}
	}
	panic("no LLMProvider " + name)
}

// TestAnE2BRunOfAHarnessWithNoPrebuiltTemplateNeedsTheCompanys holds the
// refusal of a Company whose seat's executor can launch in E2B on QwenCode,
// which E2B prebuilds no template for, while spec.sandbox.e2b names none: on
// the Company, naming the provider, as a head or as a fallback, of its own or
// the class's; and nothing once the Company names a template, or with every
// QwenCode run Direct.
//
// Mutations, each red: drop the call in Company; judge the head alone (the
// fallback goes unrefused); refuse whatever the template; refuse a Direct run.
func TestAnE2BRunOfAHarnessWithNoPrebuiltTemplateNeedsTheCompanys(t *testing.T) {
	const field = "Company/" + sampleCompany + " spec.sandbox.e2b.template"
	for _, c := range []struct {
		name    string
		mutate  func(*Input)
		refused bool
	}{
		{name: "the head of a seat's chain in E2B", refused: true, mutate: func(in *Input) {
			withE2B(in)
			in.LLMProviders = append(in.LLMProviders, qwenCodeProvider())
			seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
		}},
		{name: "a fallback in E2B", refused: true, mutate: func(in *Input) {
			withE2B(in)
			in.LLMProviders = append(in.LLMProviders, qwenCodeProvider())
			seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames("claude", providerQwenCode)
		}},
		{name: "in E2B by the Company's default", refused: true, mutate: func(in *Input) {
			withE2B(in)
			in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementE2B
			p := qwenCodeProvider()
			p.Spec.RunIn = ""
			in.LLMProviders = append(in.LLMProviders, p)
			seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
		}},
		{name: "with the Company's own template", mutate: func(in *Input) {
			withQwenTemplate(in)
			in.LLMProviders = append(in.LLMProviders, qwenCodeProvider())
			seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
		}},
		{name: "Direct", mutate: func(in *Input) {
			withE2B(in)
			p := qwenCodeProvider()
			p.Spec.RunIn = v1alpha1.SandboxPlacementDirect
			in.LLMProviders = append(in.LLMProviders, p)
			seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerQwenCode)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			c.mutate(in)
			_, err := Company(in)
			at := slices.IndexFunc(RefErrors(err), func(r *RefError) bool { return r.Kind+"/"+r.Name+" "+r.Field == field })
			switch {
			case c.refused && at < 0:
				t.Fatalf("the render does not refuse %s: %v", field, err)
			case !c.refused && at >= 0:
				t.Fatalf("the render refuses %s: %v", field, RefErrors(err)[at])
			case c.refused:
				r := RefErrors(err)[at]
				if r.Reason != v1alpha1.ReasonSandboxTemplateRequired || r.Basis != EngineRefuses ||
					!strings.Contains(r.Message, "LLMProvider qwen (QwenCode)") ||
					!strings.Contains(r.Message, "@qwen-code/qwen-code@0.28.4") {
					t.Errorf("the refusal reads %s on %v: %s", r.Reason, r.Basis, r.Message)
				}
			}
		})
	}
}

// TestExecutorHarnessesAreTheRunIn holds the harnesses the render runs a
// seat's executor on to the ones the LLMProvider API admits a runIn on, both
// of which say what Crewlet launches a run on: the CRD's rule names them in
// its order.
//
// Mutation: drop 'Copilot' from the runIn rule, and this fails.
func TestExecutorHarnessesAreTheRunIn(t *testing.T) {
	quoted := make([]string, len(ExecutorHarnesses))
	for i, harness := range ExecutorHarnesses {
		quoted[i] = "'" + string(harness) + "'"
	}
	want := "!has(self.runIn) || self.harness in [" + strings.Join(quoted, ", ") + "]"
	raw := crdOf(t, "llmproviders")
	if !slices.Contains(rules(raw), want) {
		t.Errorf("the LLMProvider CRD has no rule %q: a runIn is admitted on other harnesses than the render runs an executor on", want)
	}
}

// TestContractExecutorIsAppliedWhereItCannotRun has the engine validate the
// documents the render writes for a seat whose executor it reports rather
// than refuses: one placed in E2B in a Company with no public address, and
// one in E2B on a login that cannot follow it there. The engine accepts each, and
// fails the seat's turns at the launch alone, which is what makes reporting
// it right. When this fails, the engine has started refusing one of them on
// a write, which holds the company on its last revision anyway: refuse it in
// the render then, where the Seat is told why.
func TestContractExecutorIsAppliedWhereItCannotRun(t *testing.T) {
	for _, c := range []struct {
		name       string
		input      func(*testing.T) *Input
		seat       string
		reason     string
		advisories []advisory
	}{
		{
			name: "E2B in a company with no public address",
			input: func(t *testing.T) *Input {
				in := companyCaseNamed(t, "harnesses").input(t)
				in.PublicBaseURL = ""
				return in
			},
			seat:       "builder",
			reason:     v1alpha1.ReasonNotExposed,
			advisories: companyCaseNamed(t, "harnesses").advisories,
		},
		{
			name: "E2B on a login that cannot follow it",
			input: func(t *testing.T) *Input {
				in := companyCaseNamed(t, caseSeatPools).input(t)
				in.PublicBaseURL = "https://nimbus.example.com"
				withE2B(in)
				p := bundled(claudeCodeProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerClaudeCode)
				return in
			},
			seat:       seatCy,
			reason:     v1alpha1.ReasonSandboxLoginNotPortable,
			advisories: companyCaseNamed(t, caseSeatPools).advisories,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := c.input(t)
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("the render refuses the document: %v", err)
			}
			if got := ExecutorOf(in)[c.seat].Reason; got != c.reason {
				t.Fatalf("the render reports %q on Seat %s, want %q", got, c.seat, c.reason)
			}
			tierA, err := Bootstrap(bootstrapCases()[0].input(t))
			if err != nil {
				t.Fatal(err)
			}
			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			validate(t, env, c.advisories, "-config", write(t, "crewlet.yaml", tierA.YAML), "-company", write(t, "company.yaml", doc.YAML))
		})
	}
}

// TestContractDirectCell holds the direct cell a document configures to the
// volume the operator keeps a Direct run's boxes on and to what the engine
// takes. A run can be placed Direct by the Company's
// spec.sandbox.defaultRunIn or by a provider's runIn, and wherever one can,
// the document configures Crewlet's local backend with its boxes under
// SandboxDir, where every engine pod mounts its sandboxes volume, and nothing
// else of it, the rest being the container cell's; where none can, it
// configures no local backend, so the cells a document configures are the
// ones the Company offers. The engine takes each, the direct cell alone and
// beside E2B. And every document that configures the direct cell is handed
// to the engine again without it, which the engine must refuse where the run
// is placed Direct: the block is what lets a run go Direct, not one the
// engine would make up for, so a case it took either way would hold nothing.
func TestContractDirectCell(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	const (
		catalogue     = keyProviders + ".sandbox"
		defaultRunIn  = catalogue + ".default_run_in"
		providerRunIn = keyProviders + ".llm.claude.run_in"
	)
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		// directAt is where the engine refuses the document without its
		// local backend: the field that places a run Direct, or the
		// catalogue, when that was its one cell; empty when no run is
		// placed Direct and the document configures none.
		directAt string
	}{
		{name: "Direct alone", mutate: func(*Input) {}, directAt: catalogue},
		{name: "Direct beside E2B", mutate: withE2B, directAt: defaultRunIn},
		{
			name: "E2B by default and one provider Direct",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementE2B
				withE2B(in)
				in.LLMProviders[0].Spec.RunIn = v1alpha1.SandboxPlacementDirect
			},
			directAt: providerRunIn,
		},
		{
			name: "E2B alone",
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementE2B
				withE2B(in)
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			// One ClaudeCode provider, claude, on a key, and one agent seat
			// with no chain of its own, whose executor runs on it.
			const base = "minimal-agent"
			in := companyCaseNamed(t, base).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("the render refuses the document: %v", err)
			}
			var tree struct {
				Providers struct {
					Sandbox map[string]any `json:"sandbox"`
				} `json:"providers"`
			}
			if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
				t.Fatal(err)
			}
			local, configured := tree.Providers.Sandbox["local"]
			switch {
			case c.directAt == "" && configured:
				t.Errorf("no run is placed Direct, and the document configures the direct cell: %v", local)
			case c.directAt != "" && !configured:
				t.Errorf("a run is placed Direct, and the document configures no direct cell:\n%s", doc.YAML)
			case c.directAt != "":
				want := map[string]any{"state_dir": SandboxDir}
				if got, ok := local.(map[string]any); !ok || !maps.Equal(got, want) {
					t.Errorf("the direct cell is %v, want %v: its boxes on the sandboxes volume, and nothing of the container cell", local, want)
				}
			}

			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			validate(t, env, companyCaseNamed(t, base).advisories,
				"-config", config, "-company", write(t, "company.yaml", doc.YAML))
			if c.directAt == "" {
				return
			}

			var without map[string]any
			if err := yaml.Unmarshal(doc.YAML, &without); err != nil {
				t.Fatal(err)
			}
			delete(without[keyProviders].(map[string]any)["sandbox"].(map[string]any), "local")
			data, err := yaml.Marshal(without)
			if err != nil {
				t.Fatal(err)
			}
			got, stdout, stderr, _ := verdict(t, env, "-config", config, "-company", write(t, "without-local.yaml", data))
			refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
				var p struct{ Path string }
				return json.Unmarshal(raw, &p) == nil && p.Path == c.directAt
			})
			if got.Valid || !refused {
				t.Errorf("crewlet validate took a run placed Direct with no local backend, or refused it elsewhere than at %s:\n%s\n%s",
					c.directAt, stdout, stderr)
			}
		})
	}
}

// TestSeatCredentialKeysAreTheOnesItsIntegrationsRender holds
// seatCredentialKeys, which the sign-in view leaves out, to every Secret key
// roleIntegrations renders a seat's own credential from, in both directions,
// so a credential position added there cannot count as a seat's sign-in here.
func TestSeatCredentialKeysAreTheOnesItsIntegrationsRender(t *testing.T) {
	in := companyCaseNamed(t, caseSeatPools).input(t)
	cy := seatNamed(in, seatCy)
	cy.Spec.Integrations.Slack.Enabled = new(true)
	cy.Spec.Integrations.Slack.CredentialsSecretRef = v1alpha1.SecretReference{Name: "cy-slack"}
	cy.Spec.Integrations.GitHub.Enabled = new(true)
	cy.Spec.Integrations.GitHub.App.ID = 7
	cy.Spec.Integrations.GitHub.App.CredentialsSecretRef = v1alpha1.SecretReference{Name: "cy-github"}
	cy.Spec.Integrations.Mattermost.Enabled = new(true)
	cy.Spec.Integrations.Mattermost.BotToken = v1alpha1.SecretValueSource{
		SecretKeyRef: v1alpha1.SecretKeyReference{Name: "cy-mattermost", Key: "bot-token"},
	}

	r := newRenderer(in)
	raw, err := json.Marshal(r.roleIntegrations(cy))
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`).FindAllStringSubmatch(string(raw), -1)
	rendered := make([]string, 0, len(refs))
	for _, m := range refs {
		rendered = append(rendered, m[1])
	}
	keys := r.seatCredentialKeys(cy)
	kept := make([]string, 0, len(keys))
	for k := range keys {
		kept = append(kept, in.StoreName(k))
	}
	slices.Sort(rendered)
	slices.Sort(kept)
	if len(rendered) == 0 || !slices.Equal(rendered, kept) {
		t.Errorf("roleIntegrations renders %v and seatCredentialKeys keeps %v", rendered, kept)
	}
}
