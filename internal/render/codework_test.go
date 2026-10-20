package render

import (
	"cmp"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The objects of the opencode-agent case the code-work tests break, and the
// names their providers and Secrets use.
const (
	codeWorkSeat  = "fixer"
	codeWorkModel = "openrouter/anthropic/claude-sonnet-5"
	keyAPIKey     = "apiKey"
	keyBundle     = "bundle"
	keyValue      = "value"
	envGatewayKey = "OPENROUTER_API_KEY"
	platformGPT   = "shared-gpt"
)

// codeWorkProvider is a provider of kind for the opencode-agent case, named
// head- and its kind or CLI, signed in as the API admits: an API provider on a key (an
// OpenAICompatible one on its endpoint alone), a CLIAgent provider in mode
// Text on its headless token, its key, its model provider's key in env, or a
// bundle, the first its CLI takes.
func codeWorkProvider(kind v1alpha1.LLMProviderType, agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
	key := func(name string) v1alpha1.SecretKeyReference {
		return v1alpha1.SecretKeyReference{Name: name, Key: keyAPIKey}
	}
	p := v1alpha1.LLMProvider{
		Namespace: testNamespace,
		Name:      "head-" + strings.ToLower(string(kind)),
		Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: "harbor"},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Type: kind, Model: codeWorkModel},
		},
	}
	switch kind {
	case v1alpha1.LLMProviderAnthropic, v1alpha1.LLMProviderOpenAI:
		p.Spec.APIKeySecretRefs = []v1alpha1.SecretKeyReference{key(p.Name)}
	case v1alpha1.LLMProviderOpenAICompatible:
		p.Spec.BaseURL = "https://gateway.example.com/v1"
	case v1alpha1.LLMProviderCLIAgent:
		p.Name = "head-" + strings.ToLower(string(agent))
		p.Spec.CLI = v1alpha1.CLIAgentSettings{Agent: agent}
		switch agent {
		case v1alpha1.CLIAgentClaudeCode:
			p.Spec.CLI.Auth.TokenSecretRef = key(p.Name)
		case v1alpha1.CLIAgentCodex, v1alpha1.CLIAgentGeminiCLI, v1alpha1.CLIAgentQwenCode,
			v1alpha1.CLIAgentCursorAgent, v1alpha1.CLIAgentCopilot, v1alpha1.CLIAgentGrok, v1alpha1.CLIAgentMuseCode:
			p.Spec.CLI.Auth.Mode = v1alpha1.CLIAgentAuthAPIKey
			p.Spec.APIKeySecretRefs = []v1alpha1.SecretKeyReference{key(p.Name)}
		case v1alpha1.CLIAgentOpenCode, v1alpha1.CLIAgentHermes, v1alpha1.CLIAgentPi:
			p.Spec.CLI.Auth.Mode = v1alpha1.CLIAgentAuthSubscription
			p.Spec.CLI.Env = []v1alpha1.SecretEnvVar{{
				Name:      envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: key("openrouter")},
			}}
		case v1alpha1.CLIAgentKimiCode:
			p.Spec.CLI.Auth.CredentialBundleSecretRef = v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle}
		}
	}
	return p
}

// codeWorkBundled is a CLIAgent provider of agent for the opencode-agent case,
// named bundle- and its CLI, signed in by a credential bundle alone, whose
// login cannot follow a run into a remote sandbox.
func codeWorkBundled(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
	p := codeWorkProvider(v1alpha1.LLMProviderCLIAgent, agent)
	p.Name = "bundle-" + strings.ToLower(string(agent))
	p.Spec.APIKeySecretRefs = nil
	p.Spec.CLI.Env = nil
	p.Spec.CLI.Auth = v1alpha1.CLIAgentAuth{
		CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle},
	}
	return p
}

// codeWorkInput is the opencode-agent case with seat fixer's code work on
// head, a new provider, driven by runner.
func codeWorkInput(t *testing.T, head *v1alpha1.LLMProvider, runner v1alpha1.CodingAgent) *Input {
	t.Helper()
	in := companyCaseNamed(t, "opencode-agent").input(t)
	in.LLMProviders = append(in.LLMProviders, *head)
	s := seat(in, codeWorkSeat)
	s.Spec.LLM.Sandbox = []v1alpha1.LLMProviderReference{{Name: head.Name}}
	s.Spec.Sandbox.CodingAgent = runner
	return in
}

// TestCodeWork covers every seat whose code work no run_sandbox launch could
// run: a CLIAgent provider driving another CLI than its coding agent, which
// the render refuses as Crewlet does on every write, and ClaudeCode on a
// provider that does not speak Anthropic's wire or a CLIAgent login a remote
// sandbox cannot carry, which Crewlet applies and the render reports without
// refusing the document. Each is reported at the field that names the
// provider, every way out of each the engine takes is reported runnable, and
// a seat Crewlet offers no code work is reported nothing.
func TestCodeWork(t *testing.T) {
	cli := func(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
		return codeWorkProvider(v1alpha1.LLMProviderCLIAgent, agent)
	}
	bundled := codeWorkBundled
	api := func(kind v1alpha1.LLMProviderType) v1alpha1.LLMProvider { return codeWorkProvider(kind, "") }
	seatEnv := func(name string) func(*Input) {
		return func(in *Input) {
			seat(in, codeWorkSeat).Spec.Sandbox.Env = []v1alpha1.SeatEnvVar{{
				Name:      name,
				ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "seat", Key: keyValue}},
			}}
		}
	}
	setupEnv := func(company bool, name string) func(*Input) {
		return func(in *Input) {
			step := v1alpha1.SandboxSetupStep{Name: "login", Env: []v1alpha1.SecretEnvVar{{
				Name:      name,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "setup", Key: keyValue}},
			}}}
			if company {
				in.Company.Spec.Sandbox.Setup = append(in.Company.Spec.Sandbox.Setup, step)
				return
			}
			s := seat(in, codeWorkSeat)
			s.Spec.Sandbox.Setup = append(s.Spec.Sandbox.Setup, step)
		}
	}
	const (
		atSandbox = "spec.llm.sandbox[0].name "
		runnable  = atSandbox + "runnable"
		mismatch  = atSandbox + v1alpha1.ReasonCodingAgentMismatch
		foreign   = atSandbox + v1alpha1.ReasonSandboxModelUnsupported
		stranded  = atSandbox + v1alpha1.ReasonSandboxLoginNotPortable
		// refusal is the render's refusal of a mismatch.
		refusal = "Seat/fixer " + mismatch
	)
	for _, c := range []struct {
		name   string
		head   v1alpha1.LLMProvider
		runner v1alpha1.CodingAgent
		mutate func(*Input)
		// refused is the render's refusals of the document.
		refused []string
		// reported is CodeWorkOf's entry for the seat, its field and its
		// reason, or "runnable"; empty for no entry.
		reported string
		// says is what the entry's message must say.
		says []string
	}{
		{
			name: "ClaudeCode on an Anthropic provider", head: api(v1alpha1.LLMProviderAnthropic), runner: v1alpha1.CodingAgentClaudeCode,
			reported: runnable, says: []string{"with the ClaudeCode coding agent on LLMProvider head-anthropic"},
		},
		{
			name: "OpenCode on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentOpenCode,
			reported: runnable, says: []string{"with the OpenCode coding agent on LLMProvider head-openai"},
		},
		{
			name: "ClaudeCode on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentClaudeCode,
			reported: foreign,
			says: []string{
				"on LLMProvider head-openai, of type OpenAI", "spec.sandbox.codingAgent: OpenCode", "CLAUDE_CODE_USE_FOUNDRY",
				"Crewlet refuses each run_sandbox launch of the seat.",
			},
		},
		{
			name: "ClaudeCode on an OpenAICompatible provider", head: api(v1alpha1.LLMProviderOpenAICompatible),
			runner: v1alpha1.CodingAgentClaudeCode, reported: foreign,
		},
		{
			// The Company's default coding agent is OpenCode; unset,
			// ClaudeCode.
			name: "the default coding agent on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI),
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultCodingAgent = ""
				seat(in, "porter").Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentOpenCode
			},
			reported: foreign,
		},
		{
			name: "the Company's default coding agent, OpenCode, on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI),
			reported: runnable,
		},
		{
			name: "ClaudeCode on an OpenAI provider, signed in by the seat", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("CLAUDE_CODE_OAUTH_TOKEN"), reported: runnable,
			says: []string{
				"on the sign-in the seat's sandbox environment gives it (CLAUDE_CODE_OAUTH_TOKEN), under its own model choice",
				"neither the model nor the key of LLMProvider head-openai",
			},
		},
		{
			name: "ClaudeCode on an OpenAI provider, moved onto Bedrock by the seat's setup", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(false, "CLAUDE_CODE_USE_BEDROCK"), reported: runnable,
			says: []string{"gives it (CLAUDE_CODE_USE_BEDROCK), under its own model choice"},
		},
		{
			name: "ClaudeCode on an OpenAI provider, signed in by the Company's setup", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(true, "ANTHROPIC_AUTH_TOKEN"), reported: runnable,
			says: []string{"gives it (ANTHROPIC_AUTH_TOKEN)"},
		},
		{
			name: "ClaudeCode on an OpenAI provider beside a key no Claude Code reads", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("OPENAI_API_KEY"), reported: foreign,
		},
		{
			name: "ClaudeCode on a Codex provider", head: cli(v1alpha1.CLIAgentCodex), runner: v1alpha1.CodingAgentClaudeCode,
			refused: []string{refusal}, reported: mismatch,
			says: []string{"on LLMProvider head-codex, a CLIAgent provider driving Codex", "Name a provider of type Anthropic, or"},
		},
		{
			name: "OpenCode on a ClaudeCode provider", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentOpenCode,
			refused: []string{refusal}, reported: mismatch,
			says: []string{"Anthropic, OpenAI or OpenAICompatible", "or set spec.sandbox.codingAgent: ClaudeCode"},
		},
		{
			name: "ClaudeCode on an OpenCode provider", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentClaudeCode,
			refused: []string{refusal}, reported: mismatch, says: []string{"or set spec.sandbox.codingAgent: OpenCode"},
		},
		{
			name: "ClaudeCode on its headless token", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			reported: runnable, says: []string{"on LLMProvider head-claudecode, whose model and sign-in it can use"},
		},
		{
			// The provider's own sign-in reaches the box, whatever the
			// seat adds beside it.
			name: "ClaudeCode on its headless token beside the seat's key", head: cli(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_API_KEY"), reported: runnable,
			says: []string{"whose model and sign-in it can use"},
		},
		{
			name: "OpenCode on its key in env", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode,
			reported: runnable, says: []string{"on LLMProvider head-opencode, whose model and sign-in it can use"},
		},
		{
			name: "ClaudeCode on a bundle", head: bundled(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			reported: stranded,
			says: []string{
				"cli.auth.tokenSecretRef", "CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY",
				"Crewlet refuses each run_sandbox launch of the seat.",
			},
		},
		{
			name: "ClaudeCode on a bundle, signed in by the seat's key", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_API_KEY"), reported: runnable,
			says: []string{
				"on LLMProvider bundle-claudecode, whose model it uses, signed in by ANTHROPIC_API_KEY from the seat's sandbox environment",
			},
		},
		{
			// A bearer token is no variable ClaudeCode's profile reads
			// as its login.
			name: "ClaudeCode on a bundle beside a bearer token", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_AUTH_TOKEN"), reported: stranded,
		},
		{
			name: "OpenCode on a bundle", head: bundled(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode,
			reported: stranded, says: []string{envGatewayKey},
		},
		{
			// Any variable named as a credential passes, as it passes
			// Crewlet's guard.
			name: "OpenCode on a bundle beside a credential in the Company's setup", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: setupEnv(true, "GH_TOKEN"), reported: runnable,
			says: []string{"signed in by GH_TOKEN from the seat's sandbox environment"},
		},
		{
			name: "OpenCode on a bundle beside a proxy", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: seatEnv("HTTPS_PROXY"), reported: stranded,
		},
		{
			// The sandbox chain is the code work's, ahead of the seat's
			// own chain.
			name: "a Codex executor whose code work runs on Anthropic", head: api(v1alpha1.LLMProviderAnthropic),
			runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, cli(v1alpha1.CLIAgentCodex))
				seat(in, codeWorkSeat).Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: "head-codex"}}
			},
			reported: runnable,
		},
		{
			name: "a Codex executor that runs the code work too", head: api(v1alpha1.LLMProviderAnthropic),
			runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, cli(v1alpha1.CLIAgentCodex))
				s := seat(in, codeWorkSeat)
				s.Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: "head-codex"}}
				s.Spec.LLM.Sandbox = nil
			},
			refused:  []string{"Seat/fixer spec.llm.default[0].name " + v1alpha1.ReasonCodingAgentMismatch},
			reported: "spec.llm.default[0].name " + v1alpha1.ReasonCodingAgentMismatch,
		},
		{
			// A seat with no chain runs on the head of the document's
			// order, a platform provider among them.
			name: "a chainless seat on a platform OpenAI provider", head: api(v1alpha1.LLMProviderAnthropic),
			runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.Company.Spec.LLM.UsePlatformProviders = new(true)
				in.Class.Spec.PlatformLLMProviders = []v1alpha1.PlatformLLMProvider{{
					Name: platformGPT, Type: v1alpha1.LLMProviderOpenAI, Model: "gpt-5",
					APIKeySecretRefs: []v1alpha1.NamespacedSecretKeyReference{
						{Namespace: "crewlet-system", Name: platformGPT, Key: keyAPIKey},
					},
				}}
				in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{
					{Kind: v1alpha1.LLMProviderKindPlatform, Name: platformGPT},
				}
				s := seat(in, codeWorkSeat)
				s.Spec.LLM = v1alpha1.SeatLLM{}
			},
			reported: "spec.llm " + v1alpha1.ReasonSandboxModelUnsupported,
			says:     []string{"on the platform model provider shared-gpt, of type OpenAI"},
		},
		{
			// Crewlet offers no run_sandbox without the Company's
			// sandbox, and refuses the seat's gate itself.
			name: "a company with no sandbox", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{}
				in.LLMProviders = slices.DeleteFunc(in.LLMProviders, func(p v1alpha1.LLMProvider) bool {
					return p.Spec.CLI.Mode == v1alpha1.CLIAgentModeAgent
				})
				seat(in, "porter").Spec.LLM.Default = nil
			},
		},
		{
			name: "a seat with no sandbox", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentClaudeCode,
			mutate: func(in *Input) { seat(in, codeWorkSeat).Spec.Sandbox = nil },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := codeWorkInput(t, &c.head, c.runner)
			if c.mutate != nil {
				c.mutate(in)
			}
			_, err := Company(in)
			if got := refusalsOf(err); !slices.Equal(got, c.refused) {
				t.Fatalf("refused %v (%v), want %v", got, err, c.refused)
			}
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			got := ""
			if ok {
				got = w.Field + " " + cmp.Or(w.Reason, "runnable")
			}
			if got != c.reported {
				t.Fatalf("reported %q (%s), want %q", got, w.Message, c.reported)
			}
			for _, phrase := range c.says {
				if !strings.Contains(w.Message, phrase) {
					t.Errorf("the report does not say %q: %s", phrase, w.Message)
				}
			}
			if refusal := slices.IndexFunc(RefErrors(err), func(r *RefError) bool {
				return r.Kind == KindSeat && r.Name == codeWorkSeat
			}); refusal >= 0 && RefErrors(err)[refusal].Message != w.Message {
				t.Errorf("the refusal says %q, the report %q", RefErrors(err)[refusal].Message, w.Message)
			}
		})
	}
}

// TestContractCodeWorkOnWrite holds the render's verdict on a seat's code work
// to the engine's on a write. Every coding agent the API offers is
// paired with every kind of provider at the head of the seat's sandbox chain,
// every CLI the API offers among them, signed in as codeWorkProvider signs it
// and, for each coding agent's own CLI, by a credential bundle alone: where
// the render refuses the seat (CodingAgentMismatch), `crewlet validate`
// refuses the document at the seat's llm_sandbox, and where it only reports
// the seat (SandboxModelUnsupported, SandboxLoginNotPortable) the engine
// accepts the document the render writes, which is what makes reporting
// rather than refusing them right: an engine that refused them on a write
// would hold the company on its last revision over them anyway. A refused
// seat is handed to the engine in the document the render writes without its
// sandbox, with the block put back as the render writes it.
func TestContractCodeWorkOnWrite(t *testing.T) {
	runners := []v1alpha1.CodingAgent{v1alpha1.CodingAgentClaudeCode, v1alpha1.CodingAgentOpenCode}
	agents := cliAgentNames(t)
	heads := make([]v1alpha1.LLMProvider, 0, 5+len(agents))
	for _, kind := range []v1alpha1.LLMProviderType{
		v1alpha1.LLMProviderAnthropic, v1alpha1.LLMProviderOpenAI, v1alpha1.LLMProviderOpenAICompatible,
	} {
		heads = append(heads, codeWorkProvider(kind, ""))
	}
	for _, agent := range agents {
		heads = append(heads, codeWorkProvider(v1alpha1.LLMProviderCLIAgent, agent))
	}
	heads = append(heads, codeWorkBundled(v1alpha1.CLIAgentClaudeCode), codeWorkBundled(v1alpha1.CLIAgentOpenCode))
	reasons := map[string]bool{}
	for _, runner := range runners {
		for _, head := range heads {
			in := codeWorkInput(t, &head, runner)
			w, ok := CodeWorkOf(in)[codeWorkSeat]
			if !ok {
				t.Fatalf("%s on %s: the render reports no code work", runner, head.Name)
			}
			reasons[w.Reason] = true
			t.Run(string(runner)+" on "+head.Name, func(t *testing.T) {
				refused := w.Reason == v1alpha1.ReasonCodingAgentMismatch
				doc, err := Company(in)
				if refused != (err != nil) {
					t.Fatalf("the render reports %q and refuses the document with %v", w.Reason, err)
				}
				var document []byte
				if refused {
					in = codeWorkInput(t, &head, runner)
					seat(in, codeWorkSeat).Spec.Sandbox = nil
					if doc, err = Company(in); err != nil {
						t.Fatalf("rendering the seat without its sandbox: %v", err)
					}
					gate, err := json.Marshal(roleSandboxDoc{Enabled: true, CodingAgent: codingAgent(runner)})
					if err != nil {
						t.Fatal(err)
					}
					document = withRoleSandbox(t, doc.YAML, codeWorkSeat, gate)
				} else {
					document = doc.YAML
				}
				env := map[string]string{}
				for _, k := range doc.Refs {
					env[secretsync.StoreName(k)] = dummyCredential(k)
				}
				got, _, _, _ := verdict(t, env, "-tier", "company", write(t, "company.yaml", document))
				engineRefuses := false
				for _, raw := range got.Problems {
					var p struct {
						Segments []engine.Segment `json:"segments"`
						Message  string           `json:"message"`
					}
					if err := json.Unmarshal(raw, &p); err != nil {
						t.Fatal(err)
					}
					path := PathOf(p.Segments)
					if len(path) > 0 && path[len(path)-1] == "llm_sandbox" {
						engineRefuses = true
						continue
					}
					t.Errorf("crewlet validate refuses the document elsewhere: %s", raw)
				}
				if refused != engineRefuses {
					t.Errorf("the render refuses the pairing (%q): %v; the engine refuses it: %v", w.Reason, refused, engineRefuses)
				}
			})
		}
	}
	// The matrix sends the engine every verdict the render reaches, so that
	// a change to the heads cannot quietly stop asking about one.
	for _, reason := range []string{
		"", v1alpha1.ReasonCodingAgentMismatch, v1alpha1.ReasonSandboxModelUnsupported, v1alpha1.ReasonSandboxLoginNotPortable,
	} {
		if !reasons[reason] {
			t.Errorf("no pairing reaches %q", reason)
		}
	}
}

// withRoleSandbox is document with the role of handle given gate as its
// sandbox block.
func withRoleSandbox(t *testing.T, document []byte, handle string, gate []byte) []byte {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(document, &doc); err != nil {
		t.Fatal(err)
	}
	var block map[string]any
	if err := json.Unmarshal(gate, &block); err != nil {
		t.Fatal(err)
	}
	roles, _ := doc["roles"].([]any)
	for _, r := range roles {
		if role, ok := r.(map[string]any); ok && role["handle"] == handle {
			role["sandbox"] = block
			out, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	t.Fatalf("the document has no role %s", handle)
	return nil
}

// cliAgentNames is every CLI the API offers: the enum of the installed
// LLMProvider CRD's spec.cli.agent, so a CLI added later is paired too.
func cliAgentNames(t *testing.T) []v1alpha1.CLIAgentName {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_llmproviders.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties struct {
							Spec struct {
								Properties struct {
									CLI struct {
										Properties struct {
											Agent struct {
												Enum []v1alpha1.CLIAgentName `json:"enum"`
											} `json:"agent"`
										} `json:"properties"`
									} `json:"cli"`
								} `json:"properties"`
							} `json:"spec"`
						} `json:"properties"`
					} `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &crd); err != nil || len(crd.Spec.Versions) != 1 {
		t.Fatalf("reading the LLMProvider CRD's versions (%v)", err)
	}
	agents := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.Properties.CLI.Properties.Agent.Enum
	if len(agents) == 0 {
		t.Fatal("the LLMProvider CRD offers no spec.cli.agent")
	}
	return agents
}

// TestCodeWorkFollowsTheEngineSource holds the render's copies of what a
// launch runs and signs in, which no validation shows, to the engine's
// source: the coding agent of a seat and a catalogue that name none
// (sandbox.DefaultCodingAgent), the variables that sign ClaudeCode in from a
// seat's own environment (codingagent.ClaudeCodeCredentialEnv), and
// ClaudeCode's token and key variables (its profile's token_env and
// api_key_env). The pattern of a credential's name is held by
// TestCredentialNameFollowsTheEngineSource.
func TestCodeWorkFollowsTheEngineSource(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	const manager = "internal/sandbox/manager.go"
	src, err := source.ReadFile(manager)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stringConst(t, manager, src, "DefaultCodingAgent"), codingAgent(defaultCodingAgent); got != want {
		t.Errorf("the engine runs %s for a seat and a catalogue that name no coding agent, the render %s", got, want)
	}

	const file = "internal/sandbox/codingagent/claudecode.go"
	if src, err = source.ReadFile(file); err != nil {
		t.Fatal(err)
	}
	if got := stringSliceVar(t, file, src, "ClaudeCodeCredentialEnv"); !slices.Equal(got, claudeCodeSignIn) {
		t.Errorf("the engine signs ClaudeCode in from a seat's %v, the render from %v", got, claudeCodeSignIn)
	}

	raw, err := source.ReadFile("internal/providers/llm/cliagent/cliprofile/profiles.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]struct {
		TokenEnv  string `json:"token_env"`
		APIKeyEnv string `json:"api_key_env"`
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		t.Fatal(err)
	}
	claude, ok := profiles[cliAgentName(v1alpha1.CLIAgentClaudeCode)]
	if want := []string{claude.TokenEnv, claude.APIKeyEnv}; !ok || !slices.Equal(claudeCodeLogin, want) {
		t.Errorf("the engine's claude-code profile reads its login from %v, the render from %v", want, claudeCodeLogin)
	}
}

// profileFile is the engine's file of CLI profiles, which declares how it
// reads a credential's name (IsCredentialName and its credentialWords).
const profileFile = "internal/providers/llm/cliagent/cliprofile/profile.go"

// credentialNameRules are the kinds whose CRDs read a credential's name, and
// how many of their rules do (every other kind's, none): the LLMProvider
// API's two over a CLI's sign-in
// in cli.env, and the tool-server API's refusal of a literal value under such
// a name, on each type that takes one (an MCPServer's env and headers, and the
// values of a Seat's or a Unit's spec.mcp).
var credentialNameRules = map[string]int{
	"llmproviders": 2,
	"mcpservers":   2,
	"seats":        1,
	"units":        1,
}

// TestCredentialNameFollowsTheEngineSource holds the one pattern the operator
// reads a credential's name with to the engine's source, and every rule of
// the API that reads one to that pattern. The words are Crewlet's
// credentialWords, in its order; how a name is cut into parts is
// IsCredentialName's, which TestMirroredEngineDeclarationsAreUnchanged holds, as
// it holds the function and not the list. The LLMProvider API's rules are
// also held to the engine's sign-in by the CRD suite, and the tool-server
// API's to the names the engine reads a seat's tool credentials under.
func TestCredentialNameFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	src, err := source.ReadFile(profileFile)
	if err != nil {
		t.Fatal(err)
	}
	words := stringSliceVar(t, profileFile, src, "credentialWords")
	const head, tail = `(?i)(^|[^A-Za-z0-9])(`, `)([^A-Za-z0-9]|$)`
	if want := head + strings.Join(words, "|") + tail; credentialNamePattern != want {
		t.Errorf("the engine reads a credential's name as one whose part is one of %v, which is the pattern %q; "+
			"the operator's is %q", words, want, credentialNamePattern)
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the CRDs in config/crd/bases: %v", err)
	}
	for _, file := range files {
		kind := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "crewlet.ai_"), ".yaml")
		want := credentialNameRules[kind]
		crd, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		var schema any
		if err := yaml.Unmarshal(crd, &schema); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, rule := range rules(schema) {
			// A rule reads a credential's name where it names the words.
			if !strings.Contains(rule, "|APIKEY|") {
				continue
			}
			found++
			_, rest, ok := strings.Cut(rule, "name.matches('")
			pattern, _, _ := strings.Cut(rest, "')")
			if !ok || pattern != credentialNamePattern {
				t.Errorf("a rule of the %s CRD reads a credential's name other than as %q: %s",
					kind, credentialNamePattern, rule)
			}
		}
		if found != want {
			t.Errorf("%d rules of the %s CRD read a credential's name, want %d", found, kind, want)
		}
	}
}

// stringSliceVar is the string literals of the []string var name declares in
// the Go source src of file.
func stringSliceVar(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	return stringLiterals(t, file, name, declared(t, file, src, token.VAR, name))
}

// stringLiterals is the string literals of e, a literal of a []string of
// name's in file.
func stringLiterals(t *testing.T, file, name string, e ast.Expr) []string {
	t.Helper()
	list, ok := e.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s in %s is no literal", name, file)
	}
	out := make([]string, 0, len(list.Elts))
	for _, elt := range list.Elts {
		out = append(out, stringLiteral(t, file, name, elt))
	}
	return out
}

// stringConst is the string literal the const name declares in the Go source
// src of file.
func stringConst(t *testing.T, file string, src []byte, name string) string {
	t.Helper()
	return stringLiteral(t, file, name, declared(t, file, src, token.CONST, name))
}

// stringLiteral is the value of e, a string literal of name's in file.
func stringLiteral(t *testing.T, file, name string, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s in %s holds a value that is no string literal", name, file)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// declared is the value a var or const (tok) of name alone is declared with
// in the Go source src of file.
func declared(t *testing.T, file string, src []byte, tok token.Token, name string) ast.Expr {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != tok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == name && len(value.Values) == 1 {
				return value.Values[0]
			}
		}
	}
	t.Fatalf("%s declares no %s %s", file, tok, name)
	return nil
}

// rules is every x-kubernetes-validations rule in a decoded CRD.
func rules(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if validations, ok := n["x-kubernetes-validations"].([]any); ok {
			for _, v := range validations {
				if rule, ok := v.(map[string]any)["rule"].(string); ok {
					out = append(out, rule)
				}
			}
		}
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	case []any:
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	}
	return out
}
