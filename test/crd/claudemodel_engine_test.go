package crd_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// unlistedClaudeModel is a claudeModel no table lists, which the engine
// refuses with the ids it takes.
const unlistedClaudeModel = "claude-unlisted-0"

// wantOneOf reads the ids from the engine's refusal of a claude_model its
// table does not list: "... (want one of a, b, c)".
var wantOneOf = regexp.MustCompile(`\(want one of ([^)]*)\)`)

// TestClaudeModelFollowsTheEngine holds the LLMProvider API's claudeModel
// enum to the model table of the engine the operator is built against, which
// refuses on every write a claude_model it does not list exactly as written.
// The table's ids are read from the pinned crewlet's own refusal of one it
// does not list; the enum must be exactly those. Every id of the enum is then
// given to an Anthropic provider whose model is a gateway alias, the one place
// Crewlet takes a claudeModel, and the engine must admit each. A bump of the
// engine that adds, renames or drops a row of its table fails here until the
// API follows it.
func TestClaudeModelFollowsTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	enum := enumOf(t, "llmproviders.crewlet.ai", "spec", "claudeModel")

	provider := func(name, claudeModel string) v1.LLMProvider {
		p := llmProvider(namespace, name)
		p.Spec.Model = "house-alias"
		p.Spec.ClaudeModel = v1.ClaudeModelID(claudeModel)
		return *p
	}
	providers := make([]v1.LLMProvider, 0, 1+len(enum))
	providers = append(providers, provider("unlisted", unlistedClaudeModel))
	if refusal := apiVerdict(t, &providers[0]); !strings.Contains(refusal, msgClaudeModelUnlisted) {
		t.Errorf("the API admits claudeModel %q, which no table lists: %q", unlistedClaudeModel, refusal)
	}
	for _, id := range enum {
		p := provider("listed-"+id, id)
		if refusal := apiVerdict(t, &p); refusal != "" {
			t.Errorf("the API refuses claudeModel %q of its own enum: %s", id, refusal)
		}
		providers = append(providers, p)
	}

	report := askEngine(t, providers, false)
	var table []string
	for _, problem := range report.problems["unlisted"] {
		if m := wantOneOf.FindStringSubmatch(problem); m != nil {
			table = strings.Split(m[1], ", ")
		}
	}
	if table == nil {
		t.Fatalf("the engine did not refuse claudeModel %q with the ids it takes: %q",
			unlistedClaudeModel, report.problems["unlisted"])
	}
	slices.Sort(table)
	if got := slices.Sorted(slices.Values(enum)); !slices.Equal(got, table) {
		t.Errorf("the API's claudeModel enum is %v, the engine's table %v", got, table)
	}
	for _, p := range providers[1:] {
		if problems := report.problems[p.Name]; len(problems) > 0 {
			t.Errorf("the engine refuses claudeModel %q beside a gateway alias: %q", p.Spec.ClaudeModel, problems)
		}
	}
}
