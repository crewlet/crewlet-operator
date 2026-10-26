package render

import (
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

// webToolsRule finds the CEL rule that holds webTools Denied to the CLIs
// Crewlet can turn the web off for, and captures its list of them.
var webToolsRule = regexp.MustCompile(`self\.webTools != 'Denied' \|\| self\.agent in \[([^\]]*)\]`)

// webDeniable reads the CLIs the rule admits webTools Denied for out of one
// generated CRD, as the API server will judge them: every CEL rule in the
// document is read, and exactly one must be the webTools rule.
func webDeniable(t *testing.T, crd string) []v1alpha1.CLIAgentName {
	t.Helper()
	//nolint:gosec // G304: crd is one of the two CRD file names this file passes, under the module's own config.
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", crd))
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var rules []string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if rule, ok := v["rule"].(string); ok {
				rules = append(rules, rule)
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	var found [][]string
	for _, rule := range rules {
		if m := webToolsRule.FindStringSubmatch(rule); m != nil {
			found = append(found, m)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s carries %d webTools rules, want one", crd, len(found))
	}
	out := make([]v1alpha1.CLIAgentName, 0, strings.Count(found[0][1], ",")+1)
	for quoted := range strings.SplitSeq(found[0][1], ",") {
		out = append(out, v1alpha1.CLIAgentName(strings.Trim(strings.TrimSpace(quoted), "'")))
	}
	slices.Sort(out)
	return out
}

// TestContractWebToolsDeniedFollowsTheEngine holds the API's list of CLIs
// webTools Denied is admitted for to the engine's own verdict, CLI by CLI: a
// provider of each CLI the API knows is rendered with webTools Denied, the
// API's own rule set aside, and handed to `crewlet validate`. The engine
// refuses the stance on a CLI whose profile carries no web denial, naming
// web_denial, and takes it on every other; the rule must admit exactly the
// CLIs the engine takes, on an LLMProvider and on a platform provider alike.
// A CLI whose profile gains a measured denial in Crewlet fails here until the
// rule admits it, and one the rule admits that the engine refuses fails here
// before a tenant's apply does.
func TestContractWebToolsDeniedFollowsTheEngine(t *testing.T) {
	admitted := webDeniable(t, "crewlet.ai_llmproviders.yaml")
	if platform := webDeniable(t, "crewlet.ai_engineclasses.yaml"); !slices.Equal(platform, admitted) {
		t.Errorf("a platform provider admits webTools Denied for %v, an LLMProvider for %v", platform, admitted)
	}
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)

	var takes []v1alpha1.CLIAgentName
	for _, agent := range []v1alpha1.CLIAgentName{
		v1alpha1.CLIAgentClaudeCode, v1alpha1.CLIAgentCodex, v1alpha1.CLIAgentGeminiCLI, v1alpha1.CLIAgentQwenCode,
		v1alpha1.CLIAgentOpenCode, v1alpha1.CLIAgentCursorAgent, v1alpha1.CLIAgentCopilot, v1alpha1.CLIAgentGrok,
		v1alpha1.CLIAgentMuseCode, v1alpha1.CLIAgentKimiCode, v1alpha1.CLIAgentHermes, v1alpha1.CLIAgentPi,
	} {
		in := companyCaseNamed(t, "minimal").input(t)
		in.LLMProviders = []v1alpha1.LLMProvider{{Spec: v1alpha1.LLMProviderSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{
				Agent: agent, Model: "anthropic/model", WebTools: v1alpha1.CLIAgentWebToolsDenied,
			},
		}}}
		in.LLMProviders[0].Name = "probe"
		doc, err := Company(in)
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		env := bootstrapEnv(tierA)
		for _, k := range doc.Refs {
			env[secretsync.StoreName(k)] = dummyCredential(k)
		}
		// A provider with no sign-in is refused as well, so the verdict
		// read here is the web stance's alone: whether any problem
		// names web_denial.
		got, stdout, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		refused := strings.Contains(stdout, "web_denial")
		if !refused {
			takes = append(takes, agent)
		}
		if got.Valid && refused {
			t.Errorf("%s: the verdict is valid and names web_denial:\n%s", agent, stdout)
		}
	}
	slices.Sort(takes)
	if !slices.Equal(takes, admitted) {
		t.Errorf("the engine takes webTools Denied for %v; the API admits it for %v", takes, admitted)
	}
}
