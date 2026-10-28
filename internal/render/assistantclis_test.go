package render

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// assistantCLIsSentence is where docs/guides/cli-agents.md names the CLIs the
// Dashboard Assistant runs on: the code spans from "Today that is" to the
// colon that ends the list.
var assistantCLIsSentence = regexp.MustCompile(`(?s)Today that is\s+(.*?):`)

// assistantCLIsStated reads the CLIs the guide says the assistant runs on.
func assistantCLIsStated(t *testing.T) []v1alpha1.CLIAgentName {
	t.Helper()
	const guide = "docs/guides/cli-agents.md"
	data, err := os.ReadFile(filepath.Join("..", "..", guide))
	if err != nil {
		t.Fatal(err)
	}
	m := assistantCLIsSentence.FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s does not say which CLIs the assistant runs on (%q)", guide, assistantCLIsSentence)
	}
	spans := regexp.MustCompile("`([^`]+)`").FindAllSubmatch(m[1], -1)
	out := make([]v1alpha1.CLIAgentName, 0, len(spans))
	for _, span := range spans {
		out = append(out, v1alpha1.CLIAgentName(span[1]))
	}
	slices.Sort(out)
	return out
}

// TestContractAssistantCLIsFollowTheEngine holds the guide's list of the CLIs
// the Dashboard Assistant runs on to the engine's own verdict, CLI by CLI: for
// each CLI the API admits webTools Denied for, a Company whose assistant chain
// names one provider of that CLI, with webTools Denied, is handed
// to `crewlet validate`. The engine refuses a member whose run is not shown to
// leave the model with no tool of its own (a shell or a file reader on the
// engine host reads what no request of the person's returned), naming
// assistant.llm[0], and takes every other; the guide must name exactly the
// CLIs it takes. The API cannot refuse such a chain itself, since the rule
// turns on another object's spec, so the dry run's refusal on the Company is
// the only one a tenant meets, and the guide is what tells them beforehand.
func TestContractAssistantCLIsFollowTheEngine(t *testing.T) {
	// assistantProbe is the one provider each case's chain names.
	const assistantProbe = "probe"
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	on := true

	var takes []v1alpha1.CLIAgentName
	for _, agent := range webDeniable(t, "crewlet.ai_llmproviders.yaml") {
		in := companyCaseNamed(t, "minimal").input(t)
		in.LLMProviders = []v1alpha1.LLMProvider{{Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Agent: agent, Model: "anthropic/model", WebTools: v1alpha1.CLIAgentWebToolsDenied,
			},
		}}}
		in.LLMProviders[0].Name = assistantProbe
		in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{Enabled: &on,
			LLM: []v1alpha1.LLMProviderReference{{Kind: v1alpha1.LLMProviderKindLLMProvider, Name: assistantProbe}}}
		doc, err := Company(in)
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		env := bootstrapEnv(tierA)
		for _, k := range doc.Refs {
			env[secretsync.StoreName(k)] = dummyCredential(k)
		}
		// The verdict read here is the chain member's alone: whether any
		// problem names assistant.llm[0].
		got, stdout, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		refused := strings.Contains(stdout, "assistant.llm[0]")
		if !refused {
			takes = append(takes, agent)
		}
		if got.Valid && refused {
			t.Errorf("%s: the verdict is valid and names assistant.llm[0]:\n%s", agent, stdout)
		}
	}
	slices.Sort(takes)
	if stated := assistantCLIsStated(t); !slices.Equal(takes, stated) {
		t.Errorf("the engine runs the assistant on %v; docs/guides/cli-agents.md says %v", takes, stated)
	}
}

// deepWorkRunsOn is where docs/guides/cli-agents.md names the CLI deep work
// runs on: "Deep work runs on a provider driving" followed by it.
var deepWorkRunsOn = regexp.MustCompile("[Dd]eep work runs on a provider driving `([A-Za-z]+)`")

// TestContractDeepWorkFollowsTheEngine holds what the API and the guide say
// deep work runs on to the engine's own verdict: for each CLI that runs a
// seat's executor, and one that does not, a Company whose
// spec.assistant.deepWork names one LLMProvider of that CLI is handed to
// `crewlet validate`, and the engine must take exactly the CLI the guide
// names, refusing every other at assistant.deep_work.llm; the API cannot
// refuse such a provider itself, since the rule turns on another object's
// spec, so the dry run's refusal on the Company is the only one a tenant
// meets. A platform provider of that CLI is taken too, which the API admits.
// And a provider placed in E2B by its runIn, in a Company that configures
// none, is refused by the render on the LLMProvider and by the engine at its
// run_in, while one placed Direct is taken: deep work runs where its
// provider names, else in E2B where the Company configures it, else confined
// on the engine pod.
func TestContractDeepWorkFollowsTheEngine(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	const deepProbe = "deep-probe"

	const guide = "docs/guides/cli-agents.md"
	data, err := os.ReadFile(filepath.Join("..", "..", guide))
	if err != nil {
		t.Fatal(err)
	}
	stated := deepWorkRunsOn.FindSubmatch(data)
	if stated == nil {
		t.Fatalf("%s does not say what deep work runs on (%q)", guide, deepWorkRunsOn)
	}
	want := v1alpha1.CLIAgentName(stated[1])

	key := v1alpha1.SecretKeyReference{Name: "model", Key: keyAPIKey}
	settings := map[v1alpha1.CLIAgentName]func(*v1alpha1.LLMProviderSpec){
		v1alpha1.CLIAgentClaudeCode: func(s *v1alpha1.LLMProviderSpec) {
			s.Model = modelSonnet
			s.Auth = v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthAPIKey, APIKeySecretRef: key}
		},
		v1alpha1.CLIAgentOpenCode: func(s *v1alpha1.LLMProviderSpec) {
			s.Model = modelOpenCode
			s.Auth = v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthSubscription}
			s.Env = []v1alpha1.SecretEnvVar{{Name: envGatewayKey,
				ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: key}}}
		},
		v1alpha1.CLIAgentCodex: func(s *v1alpha1.LLMProviderSpec) {
			s.Model = modelCodex
			s.Auth = v1alpha1.CLIAgentAuth{Mode: v1alpha1.CLIAgentAuthAPIKey, APIKeySecretRef: key}
		},
	}
	// deepInput is a Company whose deep work runs on deepProbe, of agent,
	// placed in cell, in a Company whose spec.sandbox runs Direct and, with
	// e2b, configures E2B.
	deepInput := func(agent v1alpha1.CLIAgentName, cell v1alpha1.SandboxPlacement, e2b bool) *Input {
		in := companyCaseNamed(t, "minimal").input(t)
		in.PublicBaseURL = "https://minimal.hooks.example.com"
		spec := v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
			Agent:      agent,
			RunIn:      cell,
		}
		settings[agent](&spec)
		in.LLMProviders = []v1alpha1.LLMProvider{{Spec: spec}}
		in.LLMProviders[0].Name = deepProbe
		in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{DefaultRunIn: v1alpha1.SandboxPlacementDirect}
		if e2b {
			withE2B(in)
		}
		in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{DeepWork: v1alpha1.CompanyAssistantDeepWork{
			LLM: v1alpha1.LLMProviderReference{Kind: v1alpha1.LLMProviderKindLLMProvider, Name: deepProbe},
		}}
		return in
	}
	// check hands doc to the engine's validator, and reports whether the
	// engine refuses it at a path holding at.
	check := func(doc []byte, at string) (refused bool, stdout string) {
		env := bootstrapEnv(tierA)
		for _, k := range secretRefsOf(t, doc) {
			env[k] = "dummy"
		}
		got, stdout, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc))
		refused = strings.Contains(stdout, at)
		if got.Valid && refused {
			t.Errorf("the verdict is valid and names %s:\n%s", at, stdout)
		}
		return refused, stdout
	}

	var takes []v1alpha1.CLIAgentName
	for _, agent := range slices.Sorted(maps.Keys(settings)) {
		doc, err := Company(deepInput(agent, "", true))
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		// The verdict read here is deep work's alone: whether any problem
		// names assistant.deep_work.llm.
		if refused, _ := check(doc.YAML, "assistant.deep_work.llm"); !refused {
			takes = append(takes, agent)
		}
	}
	if !slices.Equal(takes, []v1alpha1.CLIAgentName{want}) {
		t.Errorf("the engine runs deep work on %v; %s says %s alone", takes, guide, want)
	}

	t.Run("a platform provider", func(t *testing.T) {
		in := deepInput(want, "", true)
		in.LLMProviders = nil
		in.Company.Spec.LLM.UsePlatformProviders = new(true)
		in.Class.Spec.PlatformLLMProviders = []v1alpha1.PlatformLLMProvider{{
			Name: deepProbe, Agent: want, Model: modelSonnet,
			Auth: v1alpha1.PlatformLLMProviderAuth{
				Mode: v1alpha1.CLIAgentAuthAPIKey,
				APIKeySecretRef: v1alpha1.NamespacedSecretKeyReference{
					Namespace: "crewlet-system", Name: deepProbe, Key: keyAPIKey,
				},
			},
		}}
		in.Company.Spec.Assistant.DeepWork.LLM = v1alpha1.LLMProviderReference{Kind: v1alpha1.LLMProviderKindPlatform, Name: deepProbe}
		doc, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		if refused, stdout := check(doc.YAML, "assistant.deep_work"); refused {
			t.Errorf("the engine refuses deep work on a platform provider:\n%s", stdout)
		}
	})

	t.Run("Direct", func(t *testing.T) {
		doc, err := Company(deepInput(want, v1alpha1.SandboxPlacementDirect, false))
		if err != nil {
			t.Fatal(err)
		}
		if refused, stdout := check(doc.YAML, deepProbe+".run_in"); refused {
			t.Errorf("the engine refuses deep work placed Direct:\n%s", stdout)
		}
	})

	t.Run("E2B the Company does not configure", func(t *testing.T) {
		in := deepInput(want, v1alpha1.SandboxPlacementE2B, false)
		_, err := Company(in)
		if got := refusalsOf(err); !slices.Equal(got, []string{"LLMProvider/" + deepProbe + " spec.runIn " +
			v1alpha1.ReasonSandboxPlacementNotOffered}) {
			t.Fatalf("the render refuses %v, want the provider's runIn", got)
		}
		// The document the operator would write without the refusal: the
		// same, with the Company's E2B sandbox taken out of it.
		doc, err := Company(deepInput(want, v1alpha1.SandboxPlacementE2B, true))
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err := yaml.Unmarshal(doc.YAML, &parsed); err != nil {
			t.Fatal(err)
		}
		providers, _ := parsed[keyProviders].(map[string]any)
		sandbox, _ := providers["sandbox"].(map[string]any)
		delete(sandbox, "e2b")
		written, err := yaml.Marshal(parsed)
		if err != nil {
			t.Fatal(err)
		}
		if refused, stdout := check(written, deepProbe+".run_in"); !refused {
			t.Errorf("the engine takes deep work placed in an E2B sandbox nothing configures:\n%s", stdout)
		}
	})
}

// secretRefsOf is the store names a document's ${NAME} references name.
func secretRefsOf(t *testing.T, doc []byte) []string {
	t.Helper()
	matches := pointer.FindAllStringSubmatch(string(doc), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}
