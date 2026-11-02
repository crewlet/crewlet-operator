package render

import (
	"maps"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The level words Crewlet's tools block is written in, and a tool of the
// Tavily server the level cases block.
const (
	levelAllow   = "allow"
	levelBlocked = "blocked"
	toolCrawl    = "tavily_crawl"
)

// TestToolLevelsAreWrittenByTheServersOwnNames holds spec.tools to Crewlet's
// tools block: the default and each level in Crewlet's words, each level keyed
// by the tool's own name, and no tools block at all for a server that sets
// none, which Crewlet reads as every tool allowed.
func TestToolLevelsAreWrittenByTheServersOwnNames(t *testing.T) {
	in := optionalInput(t)
	toolServer(in, optionalTavily).Spec.Tools = v1alpha1.MCPTools{
		Default: v1alpha1.MCPToolLevelBlocked,
		Levels: []v1alpha1.MCPToolLevelEntry{
			{Tool: "tavily_search", Level: v1alpha1.MCPToolLevelAllow},
			{Tool: toolCrawl, Level: v1alpha1.MCPToolLevelBlocked},
		},
	}
	tavily := renderedServer(t, in, optionalTavily)
	tools, _ := tavily["tools"].(map[string]any)
	if tools["default"] != levelBlocked {
		t.Errorf("tools.default is %v, want blocked", tools["default"])
	}
	levels, _ := tools["levels"].(map[string]any)
	if want := map[string]any{"tavily_search": levelAllow, toolCrawl: levelBlocked}; !maps.Equal(levels, want) {
		t.Errorf("tools.levels is %v, want %v", levels, want)
	}

	toolServer(in, optionalTavily).Spec.Tools = v1alpha1.MCPTools{}
	if tools, ok := renderedServer(t, in, optionalTavily)["tools"]; ok {
		t.Errorf("a server that sets no level writes tools %v, want none", tools)
	}
}

// renderedServer is the mcp_servers entry named in the document in renders.
func renderedServer(t *testing.T, in *Input, name string) map[string]any {
	t.Helper()
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	for _, server := range tree[keyMCPServers].([]any) {
		if entry := server.(map[string]any); entry[keyName] == name {
			return entry
		}
	}
	t.Fatalf("the document has no tool server %s", name)
	return nil
}
