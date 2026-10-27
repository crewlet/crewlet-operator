package render

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The keys the providers the executor tests add are signed in under, and
// the variable a CLI signed in through env reads its model provider's key
// from.
const (
	keyAPIKey     = "apiKey"
	keyBundle     = "bundle"
	envGatewayKey = "OPENROUTER_API_KEY"
)

// The models the executor tests' providers run, each in its CLI's own
// grammar: OpenCode's names the provider it routes through.
const (
	modelSonnet   = "sonnet"
	modelCodex    = "gpt-5-codex"
	modelOpenCode = "openrouter/anthropic/claude-sonnet-5"
)

// The providers the executor tests add to the seat-pools case, beside its
// own ClaudeCode provider claude.
const (
	providerCodex    = "codex"
	providerOpenCode = "opencode"
	platformHeadName = "class-claude"
)

// codexProvider is a provider of Codex on a key, which serves a seat's other
// phases and never its executor.
func codexProvider() v1alpha1.LLMProvider {
	return v1alpha1.LLMProvider{
		Namespace: testNamespace, Name: providerCodex,
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: sampleCompany},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Agent: v1alpha1.CLIAgentCodex, Model: modelCodex},
			Auth: v1alpha1.CLIAgentAuth{
				Mode: v1alpha1.CLIAgentAuthAPIKey, APIKeySecretRef: v1alpha1.SecretKeyReference{Name: providerCodex, Key: keyAPIKey},
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
				Agent: v1alpha1.CLIAgentOpenCode, Model: modelOpenCode,
			},
			RunIn: v1alpha1.SandboxPlacementE2B,
			Auth:  v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthSubscription},
			Env: []v1alpha1.SecretEnvVar{{
				Name:      envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "openrouter", Key: keyAPIKey}},
			}},
		},
	}
}

// bundled is the provider provider makes, signed in by a credential bundle
// alone, whose login cannot follow a run into an E2B box.
func bundled(provider func() v1alpha1.LLMProvider) v1alpha1.LLMProvider {
	p := provider()
	p.Spec.Env = nil
	p.Spec.Auth = v1alpha1.CLIAgentAuth{CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle}}
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
// a Direct box on a data pod dials the pod's own listener on loopback, and on
// a seat pod, which serves no route, the Company's public address; an E2B box
// dials the public address, and runs on a login that follows it as a
// variable, a provider's token or key or the seat's own sign-in. The verdict
// judges the first member of the chain a launch can run, and names the
// members a launch falls back to and those it never runs. None of it keeps
// the document from rendering: the engine applies such a seat and fails its
// turns alone.
func TestExecutor(t *testing.T) {
	const (
		public = "https://nimbus.example.com"
		// runnable is the verdict on a seat whose turns can run.
		runnable = "runnable"
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
			says: []string{"on the data pod that holds the seat", "on the pod's own loopback listener"},
		},
		{
			name: "Direct on a seat pod", public: public,
			want: everySeat(fieldLLM, runnable), saysOf: seatBo,
			says: []string{"on the pod of seat pool general", public + "/mcp/", "a data pod relays each call to it"},
		},
		{
			// A data pod needs no address; a seat pod serves no route of
			// its own, and dials the public one.
			name: "Direct with no public address",
			want: runnableBut(
				seatAna+" "+fieldLLM+" "+v1alpha1.ReasonBridgeUnreachable, seatBo+" "+fieldLLM+" "+v1alpha1.ReasonBridgeUnreachable,
				seatDee+" "+fieldLLM+" "+v1alpha1.ReasonBridgeUnreachable,
			),
			saysOf: seatBo,
			says:   []string{"seat pool general", "Company nimbus has no public address", "place the seat in data"},
		},
		{
			name: "E2B by the provider's runIn", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				ownProvider(in, "claude").Spec.RunIn = v1alpha1.SandboxPlacementE2B
			},
			want: everySeat(fieldLLM, runnable),
			says: []string{"in the Company's E2B sandbox", public + "/mcp/", "relays each call to the engine pod holding the seat"},
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
			// A seat in a pool needs a public address Direct too: running
			// it Direct also takes placing it in data.
			name: "E2B with no public address, a seat in a pool",
			mutate: func(in *Input) {
				withE2B(in)
				ownProvider(in, "claude").Spec.RunIn = v1alpha1.SandboxPlacementE2B
			},
			want:   everySeat(fieldLLM, v1alpha1.ReasonNotExposed),
			saysOf: seatBo,
			says:   []string{"set LLMProvider claude's spec.runIn to Direct, and place the seat in data"},
		},
		{
			name: "a bundled ClaudeCode login in E2B", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(cliProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCLI)
			},
			want:   runnableBut(cyOwn(v1alpha1.ReasonSandboxLoginNotPortable)),
			saysOf: seatCy,
			says:   []string{"ClaudeCode login cannot follow it", "auth.tokenSecretRef", "CLAUDE_CODE_OAUTH_TOKEN", "to Direct"},
		},
		{
			// The box is seeded with the login on the engine pod.
			name: "a bundled ClaudeCode login Direct", public: public,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, bundled(cliProvider))
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCLI)
			},
			want: runnableBut(cyOwn(runnable)),
		},
		{
			name: "a bundled ClaudeCode login in E2B, signed in by the seat", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(cliProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerCLI)
				cy.Spec.Sandbox = v1alpha1.SeatSandbox{Env: []v1alpha1.SeatEnvVar{seatSecret("CLAUDE_CODE_OAUTH_TOKEN")}}
			},
			want:   runnableBut(cyOwn(runnable)),
			saysOf: seatCy,
			says:   []string{"signed in by CLAUDE_CODE_OAUTH_TOKEN from the seat's spec.sandbox"},
		},
		{
			// A run goes without a value that holds nothing, listed or not,
			// so the seat's sign-in does not count then.
			name: "a bundled ClaudeCode login in E2B, the seat's sign-in empty", public: public,
			mutate: func(in *Input) {
				withE2B(in)
				p := bundled(cliProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				cy := seatNamed(in, seatCy)
				cy.Spec.LLM.Default = chainOfNames(providerCLI)
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
			says:   []string{"OpenCode login cannot follow it", envGatewayKey},
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
			// A launch passes over a CLI that cannot run the executor; the
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
			// Refused on its own (NoExecutorCLI): there is no executor to
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
					Name: platformHeadName, Agent: v1alpha1.CLIAgentClaudeCode, Model: "claude-sonnet-5",
					Auth: v1alpha1.PlatformLLMProviderAuth{
						Mode: v1alpha1.CLIAgentAuthAPIKey,
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
			SecretKeyRef: v1alpha1.SecretKeyReference{Name: "cy-token", Key: name},
		},
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

// TestRunnerCLIsAreTheRunIn holds the CLIs the render runs a seat's executor
// on to the ones the LLMProvider API admits a runIn on, both of which say
// what Crewlet launches a run on: the CRD's rule names them in its order.
func TestRunnerCLIsAreTheRunIn(t *testing.T) {
	quoted := make([]string, len(RunnerCLIs))
	for i, cli := range RunnerCLIs {
		quoted[i] = "'" + string(cli) + "'"
	}
	want := "!has(self.runIn) || self.agent in [" + strings.Join(quoted, ", ") + "]"
	raw := crdOf(t, "llmproviders")
	if !slices.Contains(rules(raw), want) {
		t.Errorf("the LLMProvider CRD has no rule %q: a runIn is admitted on other CLIs than the render runs an executor on", want)
	}
}

// TestContractExecutorIsAppliedWhereItCannotRun has the engine validate the
// documents the render writes for a seat whose executor it reports rather
// than refuses: one placed in E2B in a Company with no public address, one
// in E2B on a login that cannot follow it there, and one Direct on a seat
// pod in a Company with no public address. The engine accepts each, and
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
				in := companyCaseNamed(t, "cli-agent").input(t)
				in.PublicBaseURL = ""
				return in
			},
			seat:       "builder",
			reason:     v1alpha1.ReasonNotExposed,
			advisories: companyCaseNamed(t, "cli-agent").advisories,
		},
		{
			name: "E2B on a login that cannot follow it",
			input: func(t *testing.T) *Input {
				in := companyCaseNamed(t, caseSeatPools).input(t)
				in.PublicBaseURL = "https://nimbus.example.com"
				withE2B(in)
				p := bundled(cliProvider)
				p.Spec.RunIn = v1alpha1.SandboxPlacementE2B
				in.LLMProviders = append(in.LLMProviders, p)
				seatNamed(in, seatCy).Spec.LLM.Default = chainOfNames(providerCLI)
				return in
			},
			seat:   seatCy,
			reason: v1alpha1.ReasonSandboxLoginNotPortable,
		},
		{
			name: "Direct on a seat pod of a company with no public address",
			input: func(t *testing.T) *Input {
				return companyCaseNamed(t, caseSeatPools).input(t)
			},
			seat:   seatBo,
			reason: v1alpha1.ReasonBridgeUnreachable,
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
