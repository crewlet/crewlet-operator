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

// researchHost is a host the research cases list.
const researchHost = "docs.example.com"

// research.search renders as Crewlet's research.search.mcp_server, naming the
// MCPServer by its name.
func TestAssistantResearchRendersItsSearchServer(t *testing.T) {
	t.Parallel()
	in := optionalInput(t)
	in.Company.Spec.Assistant.Research = v1alpha1.CompanyAssistantResearch{
		Hosts:  []string{researchHost},
		Search: v1alpha1.CompanyAssistantResearchSearch{MCPServerRef: v1alpha1.LocalObjectReference{Name: optionalTavily}}}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Assistant struct {
			Research struct {
				Search struct {
					MCPServer string `json:"mcp_server"`
				} `json:"search"`
			} `json:"research"`
		} `json:"assistant"`
	}
	if err := yaml.Unmarshal(doc.YAML, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Assistant.Research.Search.MCPServer; got != optionalTavily {
		t.Errorf("research.search.mcp_server rendered as %q, want %s", got, optionalTavily)
	}
}
