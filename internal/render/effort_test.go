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

// The provider a contract test renders and hands the engine.
const (
	probeName  = "probe"
	probeModel = "anthropic/model"
)

// effortClause is one harness clause of the effort rule: the harness or
// harnesses it names, and the levels it admits for them.
var effortClause = regexp.MustCompile(`self\.harness (?:== '([A-Za-z]+)'|in \[([^\]]*)\]) && self\.effort in \[([^\]]*)\]`)

// quotedList reads a CEL list of quoted names.
func quotedList(list string) []string {
	var out []string
	for quoted := range strings.SplitSeq(list, ",") {
		out = append(out, strings.Trim(strings.TrimSpace(quoted), "'"))
	}
	return out
}

// effortLevels reads the levels the effort rule admits per harness out of one
// generated CRD, as the API server will judge them.
func effortLevels(t *testing.T, crd string) map[v1alpha1.Harness][]string {
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
	var rule string
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if r, ok := v["rule"].(string); ok && strings.Contains(r, "self.effort in") {
				rule = r
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
	out := map[v1alpha1.Harness][]string{}
	for _, m := range effortClause.FindAllStringSubmatch(rule, -1) {
		harnesses := []string{m[1]}
		if m[1] == "" {
			harnesses = quotedList(m[2])
		}
		for _, h := range harnesses {
			out[v1alpha1.Harness(h)] = quotedList(m[3])
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no effort rule", crd)
	}
	return out
}

// TestContractEffortFollowsTheEngine holds the levels the API admits for effort
// to the engine's own verdict, harness by harness and level by level: a
// provider is rendered at each candidate level and handed to `crewlet validate`,
// which refuses a level its profile does not take, naming effort.
func TestContractEffortFollowsTheEngine(t *testing.T) {
	admitted := effortLevels(t, "crewlet.ai_llmproviders.yaml")
	if platform := effortLevels(t, "crewlet.ai_engineclasses.yaml"); !maps.EqualFunc(platform, admitted, slices.Equal) {
		t.Errorf("a platform provider admits effort %v, an LLMProvider %v", platform, admitted)
	}
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	candidates := []string{"none", "off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent"}
	for _, harness := range []v1alpha1.Harness{
		v1alpha1.HarnessClaudeCode, v1alpha1.HarnessCodex, v1alpha1.HarnessGeminiCLI, v1alpha1.HarnessQwenCode,
		v1alpha1.HarnessOpenCode, v1alpha1.HarnessCursorAgent, v1alpha1.HarnessCopilot, v1alpha1.HarnessGrok,
		v1alpha1.HarnessMuseCode, v1alpha1.HarnessKimiCode, v1alpha1.HarnessHermes, v1alpha1.HarnessPi,
	} {
		var takes []string
		for _, level := range candidates {
			in := companyCaseNamed(t, "minimal").input(t)
			in.LLMProviders = []v1alpha1.LLMProvider{{Spec: v1alpha1.LLMProviderSpec{
				CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
				LLMProviderSettings: v1alpha1.LLMProviderSettings{
					Harness: harness, Model: probeModel, Effort: level,
				},
			}}}
			in.LLMProviders[0].Name = probeName
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("%s: %v", harness, err)
			}
			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			// A provider with no sign-in is refused as well, so the verdict
			// read here is the effort's alone: whether a problem names it.
			_, stdout, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
			if !strings.Contains(stdout, "providers.llm.probe.effort") {
				takes = append(takes, level)
			}
		}
		if want := admitted[harness]; !slices.Equal(takes, want) {
			t.Errorf("%s: the engine takes effort %v; the API admits %v", harness, takes, want)
		}
	}
}
