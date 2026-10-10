package render

import (
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

// TestCodeWorkRefusals covers every seat whose code work no run_sandbox launch
// could run: a CLIAgent provider driving another CLI than its coding agent,
// ClaudeCode on a provider that does not speak Anthropic's wire, and a
// CLIAgent login a remote sandbox cannot carry. Each is refused on the Seat,
// at the field that names the provider, and every way out of each the engine
// takes is admitted.
func TestCodeWorkRefusals(t *testing.T) {
	cli := func(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
		return codeWorkProvider(v1alpha1.LLMProviderCLIAgent, agent)
	}
	bundled := func(agent v1alpha1.CLIAgentName) v1alpha1.LLMProvider {
		p := cli(agent)
		p.Spec.APIKeySecretRefs = nil
		p.Spec.CLI.Env = nil
		p.Spec.CLI.Auth = v1alpha1.CLIAgentAuth{
			CredentialBundleSecretRef: v1alpha1.SecretKeyReference{Name: p.Name, Key: keyBundle},
		}
		return p
	}
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
		atSandbox = "Seat/fixer spec.llm.sandbox[0].name "
		mismatch  = atSandbox + v1alpha1.ReasonCodingAgentMismatch
		foreign   = atSandbox + v1alpha1.ReasonSandboxModelUnsupported
		stranded  = atSandbox + v1alpha1.ReasonSandboxLoginNotPortable
	)
	for _, c := range []struct {
		name   string
		head   v1alpha1.LLMProvider
		runner v1alpha1.CodingAgent
		mutate func(*Input)
		want   []string
		// says is what the refusal's message must say.
		says []string
	}{
		{name: "ClaudeCode on an Anthropic provider", head: api(v1alpha1.LLMProviderAnthropic), runner: v1alpha1.CodingAgentClaudeCode},
		{name: "OpenCode on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentOpenCode},
		{
			name: "ClaudeCode on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI), runner: v1alpha1.CodingAgentClaudeCode,
			want: []string{foreign},
			says: []string{"on LLMProvider head-openai, of type OpenAI", "spec.sandbox.codingAgent: OpenCode", "CLAUDE_CODE_USE_FOUNDRY"},
		},
		{
			name: "ClaudeCode on an OpenAICompatible provider", head: api(v1alpha1.LLMProviderOpenAICompatible),
			runner: v1alpha1.CodingAgentClaudeCode, want: []string{foreign},
		},
		{
			// The Company's default coding agent is OpenCode; unset,
			// ClaudeCode.
			name: "the default coding agent on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI),
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultCodingAgent = ""
				seat(in, "porter").Spec.Sandbox.CodingAgent = v1alpha1.CodingAgentOpenCode
			},
			want: []string{foreign},
		},
		{
			name: "the Company's default coding agent, OpenCode, on an OpenAI provider", head: api(v1alpha1.LLMProviderOpenAI),
		},
		{
			name: "ClaudeCode on an OpenAI provider, signed in by the seat", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("CLAUDE_CODE_OAUTH_TOKEN"),
		},
		{
			name: "ClaudeCode on an OpenAI provider, moved onto Bedrock by the seat's setup", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(false, "CLAUDE_CODE_USE_BEDROCK"),
		},
		{
			name: "ClaudeCode on an OpenAI provider, signed in by the Company's setup", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: setupEnv(true, "ANTHROPIC_AUTH_TOKEN"),
		},
		{
			name: "ClaudeCode on an OpenAI provider beside a key no Claude Code reads", head: api(v1alpha1.LLMProviderOpenAI),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("OPENAI_API_KEY"), want: []string{foreign},
		},
		{
			name: "ClaudeCode on a Codex provider", head: cli(v1alpha1.CLIAgentCodex), runner: v1alpha1.CodingAgentClaudeCode,
			want: []string{mismatch},
			says: []string{"on LLMProvider head-codex, a CLIAgent provider driving Codex", "Name a provider of type Anthropic, or"},
		},
		{
			name: "OpenCode on a ClaudeCode provider", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentOpenCode,
			want: []string{mismatch},
			says: []string{"Anthropic, OpenAI or OpenAICompatible", "or set spec.sandbox.codingAgent: ClaudeCode"},
		},
		{
			name: "ClaudeCode on an OpenCode provider", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentClaudeCode,
			want: []string{mismatch}, says: []string{"or set spec.sandbox.codingAgent: OpenCode"},
		},
		{name: "ClaudeCode on its headless token", head: cli(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode},
		{name: "OpenCode on its key in env", head: cli(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode},
		{
			name: "ClaudeCode on a bundle", head: bundled(v1alpha1.CLIAgentClaudeCode), runner: v1alpha1.CodingAgentClaudeCode,
			want: []string{stranded}, says: []string{"cli.auth.tokenSecretRef", "CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY"},
		},
		{
			name: "ClaudeCode on a bundle, signed in by the seat's key", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_API_KEY"),
		},
		{
			// A bearer token is no variable ClaudeCode's profile reads
			// as its login.
			name: "ClaudeCode on a bundle beside a bearer token", head: bundled(v1alpha1.CLIAgentClaudeCode),
			runner: v1alpha1.CodingAgentClaudeCode, mutate: seatEnv("ANTHROPIC_AUTH_TOKEN"), want: []string{stranded},
		},
		{
			name: "OpenCode on a bundle", head: bundled(v1alpha1.CLIAgentOpenCode), runner: v1alpha1.CodingAgentOpenCode,
			want: []string{stranded}, says: []string{envGatewayKey},
		},
		{
			// Any variable named as a credential passes, as it passes
			// Crewlet's guard.
			name: "OpenCode on a bundle beside a credential in the Company's setup", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: setupEnv(true, "GH_TOKEN"),
		},
		{
			name: "OpenCode on a bundle beside a proxy", head: bundled(v1alpha1.CLIAgentOpenCode),
			runner: v1alpha1.CodingAgentOpenCode, mutate: seatEnv("HTTPS_PROXY"), want: []string{stranded},
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
			want: []string{"Seat/fixer spec.llm.default[0].name " + v1alpha1.ReasonCodingAgentMismatch},
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
			want: []string{"Seat/fixer spec.llm " + v1alpha1.ReasonSandboxModelUnsupported},
			says: []string{"on the platform model provider shared-gpt, of type OpenAI"},
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
	} {
		t.Run(c.name, func(t *testing.T) {
			in := codeWorkInput(t, &c.head, c.runner)
			if c.mutate != nil {
				c.mutate(in)
			}
			_, err := Company(in)
			got := refusalsOf(err)
			if !slices.Equal(got, c.want) {
				t.Fatalf("refused %v (%v), want %v", got, err, c.want)
			}
			for _, phrase := range c.says {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("the refusal does not say %q: %v", phrase, err)
				}
			}
		})
	}
}

// TestContractCodingAgentMismatch holds the render's refusal of a seat whose
// code work hands a CLIAgent provider to another CLI's coding agent to the
// pinned engine, which refuses the pairing on every write. Every coding agent
// the API offers is paired with every kind of provider at the head of the
// seat's sandbox chain, every CLI the API offers among them: the render
// refuses the seat with CodingAgentMismatch exactly where `crewlet validate`
// refuses the document at the seat's llm_sandbox. The engine is handed the
// document the render writes with the seat's sandbox block put back, as the
// render writes it, since the render refuses the seat it is asked about.
func TestContractCodingAgentMismatch(t *testing.T) {
	runners := []v1alpha1.CodingAgent{v1alpha1.CodingAgentClaudeCode, v1alpha1.CodingAgentOpenCode}
	agents := cliAgentNames(t)
	heads := make([]v1alpha1.LLMProvider, 0, 3+len(agents))
	for _, kind := range []v1alpha1.LLMProviderType{
		v1alpha1.LLMProviderAnthropic, v1alpha1.LLMProviderOpenAI, v1alpha1.LLMProviderOpenAICompatible,
	} {
		heads = append(heads, codeWorkProvider(kind, ""))
	}
	for _, agent := range agents {
		heads = append(heads, codeWorkProvider(v1alpha1.LLMProviderCLIAgent, agent))
	}
	for _, runner := range runners {
		for _, head := range heads {
			t.Run(string(runner)+" on "+head.Name, func(t *testing.T) {
				in := codeWorkInput(t, &head, runner)
				_, err := Company(in)
				refused := slices.ContainsFunc(RefErrors(err), func(r *RefError) bool {
					return r.Reason == v1alpha1.ReasonCodingAgentMismatch
				})
				in = codeWorkInput(t, &head, runner)
				seat(in, codeWorkSeat).Spec.Sandbox = nil
				doc, err := Company(in)
				if err != nil {
					t.Fatalf("rendering the seat without its sandbox: %v", err)
				}
				gate, err := json.Marshal(roleSandboxDoc{Enabled: true, CodingAgent: codingAgent(runner)})
				if err != nil {
					t.Fatal(err)
				}
				document := withRoleSandbox(t, doc.YAML, codeWorkSeat, gate)
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
					t.Errorf("the render refuses the pairing: %v; the engine refuses it: %v (%v)", refused, engineRefuses, err)
				}
			})
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

// TestCodeWorkFollowsTheEngineSource holds the render's copies of what signs
// a coding agent in at a launch, which no validation shows, to the pinned
// engine's source: the variables that sign ClaudeCode in from a seat's own
// environment (codingagent.ClaudeCodeCredentialEnv), and ClaudeCode's token
// and key variables (its profile's token_env and api_key_env). The pattern of
// a credential's name is the LLMProvider API's, which the CRD suite holds to
// the engine: it is held to the installed CRD's rules.
func TestCodeWorkFollowsTheEngineSource(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })

	const file = "internal/sandbox/codingagent/claudecode.go"
	src, err := source.ReadFile(file)
	if err != nil {
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

	crd, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_llmproviders.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := yaml.Unmarshal(crd, &schema); err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, rule := range rules(schema) {
		_, rest, found := strings.Cut(rule, "e.name.matches('")
		if !found {
			continue
		}
		matches++
		if pattern, _, _ := strings.Cut(rest, "')"); pattern != signInNamePattern {
			t.Errorf("the LLMProvider API reads %q as a credential's name, the render %q", pattern, signInNamePattern)
		}
	}
	if matches == 0 {
		t.Error("no rule of the LLMProvider CRD matches a credential's name")
	}
}

// stringSliceVar is the string literals of the []string var name declares in
// the Go source src of file.
func stringSliceVar(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != name || len(value.Values) != 1 {
				continue
			}
			list, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s in %s is no literal", name, file)
			}
			var out []string
			for _, elt := range list.Elts {
				lit, ok := elt.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("%s in %s holds a value that is no string literal", name, file)
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
			return out
		}
	}
	t.Fatalf("%s declares no var %s", file, name)
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
