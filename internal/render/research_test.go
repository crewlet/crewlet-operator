package render

import (
	"slices"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// spec.assistant.research renders as Crewlet's assistant.research, hosts and
// all; unset, the document carries no research block.
func TestAssistantResearchRendersItsHosts(t *testing.T) {
	t.Parallel()
	read := func(in *Input) map[string]any {
		t.Helper()
		doc, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Assistant map[string]any `json:"assistant"`
		}
		if err := yaml.Unmarshal(doc.YAML, &out); err != nil {
			t.Fatal(err)
		}
		return out.Assistant
	}
	in := companyCaseNamed(t, "minimal").input(t)
	if _, set := read(in)["research"]; set {
		t.Error("a Company with no research renders a research block")
	}
	in.Company.Spec.Assistant.Research = v1alpha1.CompanyAssistantResearch{
		Hosts: []string{"developers.cloudflare.com", "docs.github.com"}}
	research, _ := read(in)["research"].(map[string]any)
	hosts, _ := research["hosts"].([]any)
	got := make([]string, 0, len(hosts))
	for _, h := range hosts {
		got = append(got, h.(string))
	}
	if !slices.Equal(got, []string{"developers.cloudflare.com", "docs.github.com"}) {
		t.Errorf("research rendered as %v", read(in)["research"])
	}
}
