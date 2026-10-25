package render

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

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
	var out []v1alpha1.CLIAgentName
	for _, span := range regexp.MustCompile("`([^`]+)`").FindAllSubmatch(m[1], -1) {
		out = append(out, v1alpha1.CLIAgentName(span[1]))
	}
	slices.Sort(out)
	return out
}

// TestContractAssistantCLIsFollowTheEngine holds the guide's list of the CLIs
// the Dashboard Assistant runs on to the engine's own verdict, CLI by CLI: for
// each CLI the API admits webTools Denied for, a Company whose assistant chain
// names one provider of that CLI, in mode Text with webTools Denied, is handed
// to `crewlet validate`. The engine refuses a member whose run is not shown to
// leave the model with no tool of its own (a shell or a file reader on the
// engine host reads what no request of the person's returned), naming
// assistant.llm[0], and takes every other; the guide must name exactly the
// CLIs it takes. The API cannot refuse such a chain itself, since the rule
// turns on another object's spec, so the dry run's refusal on the Company is
// the only one a tenant meets, and the guide is what tells them beforehand.
func TestContractAssistantCLIsFollowTheEngine(t *testing.T) {
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
		in.LLMProviders[0].Name = "probe"
		in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{Enabled: &on,
			LLM: []v1alpha1.LLMProviderReference{{Kind: v1alpha1.LLMProviderKindLLMProvider, Name: "probe"}}}
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
