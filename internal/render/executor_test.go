package render

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// TestExecutor holds the report on a seat whose executor runs a CLI in mode
// Box to where the engine serves the tool bridge its turns call back
// through: the data pods, while the Company has a public address. Only the
// head of the executor's chain runs in the sandbox; a fallback behind it, or
// a provider of another phase, runs in the pod that holds the seat, as in
// mode Host, which every pod serves. None of it
// keeps the document from rendering: the engine applies such a seat and
// refuses its turns alone.
func TestExecutor(t *testing.T) {
	const (
		public           = "https://nimbus.example.com"
		platformHeadName = "class-claude"
	)
	box := func(in *Input) {
		in.LLMProviders = append(in.LLMProviders, cliProvider(v1alpha1.CLIAgentModeBox))
	}
	chain := func(names ...string) []v1alpha1.LLMProviderReference {
		out := make([]v1alpha1.LLMProviderReference, len(names))
		for i, name := range names {
			out[i] = v1alpha1.LLMProviderReference{Name: name}
		}
		return out
	}
	for _, c := range []struct {
		name   string
		public string
		mutate func(*Input)
		// want is each reported seat, as "seat field reason", the reason
		// "runnable" for one whose turns can run.
		want []string
		says []string
	}{
		{
			name: "a box-mode executor in a Shared pool", public: public,
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"bo spec.llm.default[0].name BoxModeBridgeUnsupported"},
			says: []string{"seat pool general", "Place the seat in data"},
		},
		{
			name: "a box-mode executor ahead of another provider in a PerSeat pool", public: public,
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatAna).Spec.LLM.Default = chain(providerCLI, "claude")
			},
			want: []string{"ana spec.llm.default[0].name BoxModeBridgeUnsupported"},
		},
		{
			// Crewlet runs only the head of the executor's chain in the
			// sandbox; a fallback behind it runs in the pod, as in mode Host.
			name: "a box-mode CLI behind another provider in a PerSeat pool", public: public,
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatAna).Spec.LLM.Default = chain("claude", providerCLI)
			},
		},
		{
			name: "a box-mode CLI in another phase", public: public,
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatBo).Spec.LLM.Review = chain(providerCLI)
			},
		},
		{
			name: "a box-mode executor on the data pods", public: public,
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatCy).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"cy spec.llm.default[0].name runnable"},
			says: []string{public + "/mcp/"},
		},
		{
			name: "a box-mode executor on the data pods of a company with no public address",
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatCy).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"cy spec.llm.default[0].name NotExposed"},
			says: []string{"Company nimbus has no public address", "spec.exposure.publicBaseURL"},
		},
		{
			// The pool is the first thing to change, and the message says
			// what the data pods would still need.
			name: "a box-mode executor in a pool of a company with no public address",
			mutate: func(in *Input) {
				box(in)
				seatNamed(in, seatBo).Spec.LLM.Default = chain(providerCLI)
			},
			want: []string{"bo spec.llm.default[0].name BoxModeBridgeUnsupported"},
			says: []string{"seat pool general", "in data it needs Company nimbus to have a public address too"},
		},
		{
			// Every seat with no chain of its own runs on the head of the
			// company's order.
			name: "a box-mode CLI at the head of the company's order", public: public,
			mutate: func(in *Input) {
				box(in)
				in.Company.Spec.LLM.Order = chain(providerCLI)
			},
			want: []string{
				"ana spec.llm BoxModeBridgeUnsupported",
				"bo spec.llm BoxModeBridgeUnsupported",
				"cy spec.llm runnable",
				"dee spec.llm BoxModeBridgeUnsupported",
			},
			// The Seat's own spec.llm is empty: the message names the
			// Company's order, and what the Seat can set instead.
			says: []string{"at the head of Company nimbus's spec.llm.order", "spec.llm.default"},
		},
		{
			name: "a box-mode CLI first by name, with no order", public: public,
			mutate: func(in *Input) {
				box(in)
				in.LLMProviders[len(in.LLMProviders)-1].Name = "a-" + providerCLI
				in.Company.Spec.LLM.Order = nil
			},
			want: []string{
				"ana spec.llm BoxModeBridgeUnsupported",
				"bo spec.llm BoxModeBridgeUnsupported",
				"cy spec.llm runnable",
				"dee spec.llm BoxModeBridgeUnsupported",
			},
			says: []string{"Company nimbus's first LLMProvider by name"},
		},
		{
			// A platform provider runs in mode Host alone.
			name: "a platform provider at the head of the company's order", public: public,
			mutate: func(in *Input) {
				box(in)
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
					{Name: providerCLI},
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			in.PublicBaseURL = c.public
			c.mutate(in)
			if _, err := Company(in); err != nil {
				t.Fatalf("the document is refused: %v", err)
			}
			executors := ExecutorOf(in)
			got := make([]string, 0, len(executors))
			for _, seat := range slices.Sorted(maps.Keys(executors)) {
				e := executors[seat]
				got = append(got, seat+" "+e.Field+" "+cmp.Or(e.Reason, "runnable"))
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

// TestBoxMode holds the report on an LLMProvider in mode Box to whether
// its runs can call a seat's tools back at all: through the Company's public
// address, which the document then has. A provider in mode Host, or of
// another type, has no box mode to report on.
func TestBoxMode(t *testing.T) {
	const public = "https://nimbus.example.com"
	for _, c := range []struct {
		name   string
		public string
		want   map[string]string
	}{
		{name: "a company with a public address", public: public, want: map[string]string{providerCLI: "runnable"}},
		{name: "a company with none", want: map[string]string{providerCLI: v1alpha1.ReasonNotExposed}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseSeatPools).input(t)
			in.PublicBaseURL = c.public
			in.LLMProviders = append(in.LLMProviders, cliProvider(v1alpha1.CLIAgentModeBox))
			host := cliProvider(v1alpha1.CLIAgentModeHost)
			host.Name = "claude-host"
			in.LLMProviders = append(in.LLMProviders, host)
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("the document is refused: %v", err)
			}
			if !strings.Contains(string(doc.YAML), "mode: box") {
				t.Errorf("the document does not run the provider in box mode:\n%s", doc.YAML)
			}
			got := map[string]string{}
			for name, m := range BoxModeOf(in) {
				got[name] = cmp.Or(m.Reason, "runnable")
				if c.public != "" && !strings.Contains(m.Message, c.public+"/mcp/") {
					t.Errorf("the report on %s does not name the bridge: %s", name, m.Message)
				}
			}
			if !maps.Equal(got, c.want) {
				t.Errorf("reported %v, want %v", got, c.want)
			}
		})
	}
}

// TestContractBoxModeIsAppliedWhereItCannotRun has the engine
// validate the documents the render writes for a box-mode executor it
// reports rather than refuses: a seat placed in a seat pool, and a Company
// with no public address. The engine accepts both, and refuses the seat's
// turns at run time alone, which is what makes reporting them right. When
// this fails, the engine has started refusing one of them on a write,
// which holds the company on its last revision anyway: refuse it in the
// render then, where the Seat or the LLMProvider is told why.
func TestContractBoxModeIsAppliedWhereItCannotRun(t *testing.T) {
	for _, c := range []struct {
		name       string
		input      func(*testing.T) *Input
		seat       string
		reason     string
		advisories []advisory
	}{
		{
			name: "a box-mode executor in a seat pool",
			input: func(t *testing.T) *Input {
				in := companyCaseNamed(t, caseSeatPools).input(t)
				in.PublicBaseURL = "https://nimbus.example.com"
				in.LLMProviders = append(in.LLMProviders, cliProvider(v1alpha1.CLIAgentModeBox))
				in.Company.Spec.Sandbox.E2B.APIKeySecretRef = v1alpha1.SecretKeyReference{Name: "e2b", Key: "apiKey"}
				seatNamed(in, seatBo).Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: providerCLI}}
				return in
			},
			seat:   seatBo,
			reason: v1alpha1.ReasonBoxModeBridgeUnsupported,
		},
		{
			name: "a box-mode executor in a company with no public address",
			input: func(t *testing.T) *Input {
				in := companyCaseNamed(t, "cli-agent").input(t)
				in.PublicBaseURL = ""
				return in
			},
			seat:       "builder",
			reason:     v1alpha1.ReasonNotExposed,
			advisories: companyCaseNamed(t, "cli-agent").advisories,
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
			if c.reason == v1alpha1.ReasonNotExposed {
				for name, m := range BoxModeOf(in) {
					if m.Reason != v1alpha1.ReasonNotExposed {
						t.Errorf("the render reports %q on LLMProvider %s, want NotExposed", m.Reason, name)
					}
				}
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
