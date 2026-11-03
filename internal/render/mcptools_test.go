package render

import (
	"maps"
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The level words Crewlet's tools block is written in, and a tool of the
// Tavily server the level cases block.
const (
	levelAllow   = "allow"
	levelBlocked = "blocked"
	levelApprove = "approve"
	toolCrawl    = "tavily_crawl"
	toolExtract  = "tavily_extract"
)

// TestToolLevelsAreWrittenByTheServersOwnNames holds spec.tools to Crewlet's
// tools block: the default and each level in Crewlet's words, each level keyed
// by the tool's own name, and no tools block at all for a server that sets
// none, which Crewlet reads as each tool at the level its hints give it.
func TestToolLevelsAreWrittenByTheServersOwnNames(t *testing.T) {
	in := optionalInput(t)
	toolServer(in, optionalTavily).Spec.Tools = v1alpha1.MCPTools{
		Default: v1alpha1.MCPToolLevelBlocked,
		Levels: []v1alpha1.MCPToolLevelEntry{
			{Tool: "tavily_search", Level: v1alpha1.MCPToolLevelAllow},
			{Tool: toolExtract, Level: v1alpha1.MCPToolLevelApprove},
			{Tool: toolCrawl, Level: v1alpha1.MCPToolLevelBlocked},
		},
	}
	tavily := renderedServer(t, in, optionalTavily)
	tools, _ := tavily["tools"].(map[string]any)
	if tools["default"] != levelBlocked {
		t.Errorf("tools.default is %v, want blocked", tools["default"])
	}
	levels, _ := tools["levels"].(map[string]any)
	want := map[string]any{"tavily_search": levelAllow, toolExtract: levelApprove, toolCrawl: levelBlocked}
	if !maps.Equal(levels, want) {
		t.Errorf("tools.levels is %v, want %v", levels, want)
	}

	toolServer(in, optionalTavily).Spec.Tools = v1alpha1.MCPTools{Default: v1alpha1.MCPToolLevelApprove}
	if tools, _ := renderedServer(t, in, optionalTavily)["tools"].(map[string]any); tools["default"] != levelApprove {
		t.Errorf("a default of Approve writes tools %v, want default approve", tools)
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

// TestLimitsAreWrittenByTheServersOwnNames holds a Seat's and a Unit's
// spec.mcpLimits to Crewlet's mcp_limits: keyed by the server's name, a server
// turned off as enabled false, each tool held back in Crewlet's words by the tool's own name, and
// no mcp_limits at all where none is set.
func TestLimitsAreWrittenByTheServersOwnNames(t *testing.T) {
	in := optionalInput(t)
	seat(in, optionalSeat).Spec.MCPLimits = []v1alpha1.MCPLimit{{Name: optionalTavily, Tools: []v1alpha1.MCPToolLimit{
		{Tool: toolCrawl, Level: v1alpha1.MCPLimitLevelBlocked},
		{Tool: toolExtract, Level: v1alpha1.MCPLimitLevelApprove},
	}}}
	home := unitHolding(t, renderedTree(t, in), optionalSeat)["id"].(string)
	unit(in, home).Spec.MCPLimits = []v1alpha1.MCPLimit{{Name: optionalTavily, Enabled: new(false)}}
	tree := renderedTree(t, in)

	want := map[string]any{optionalTavily: map[string]any{
		"tools": map[string]any{toolCrawl: levelBlocked, toolExtract: levelApprove}}}
	if got := roleIn(tree, optionalSeat)["mcp_limits"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's mcp_limits are %v, want %v", got, want)
	}
	want = map[string]any{optionalTavily: map[string]any{"enabled": false}}
	if got := unitHolding(t, tree, optionalSeat)["mcp_limits"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the unit's mcp_limits are %v, want %v", got, want)
	}

	seat(in, optionalSeat).Spec.MCPLimits = nil
	if got, ok := roleIn(renderedTree(t, in), optionalSeat)["mcp_limits"]; ok {
		t.Errorf("a seat with no limits writes mcp_limits %v, want none", got)
	}
}

// renderedTree is the company document in renders, decoded.
func renderedTree(t *testing.T, in *Input) map[string]any {
	t.Helper()
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	return tree
}
