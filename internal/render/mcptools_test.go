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
	tavily := renderedTavily(t, in)
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
	if tools, _ := renderedTavily(t, in)["tools"].(map[string]any); tools["default"] != levelApprove {
		t.Errorf("a default of Approve writes tools %v, want default approve", tools)
	}

	toolServer(in, optionalTavily).Spec.Tools = v1alpha1.MCPTools{}
	if tools, ok := renderedTavily(t, in)["tools"]; ok {
		t.Errorf("a server that sets no level writes tools %v, want none", tools)
	}
}

// TestToolChangesAreAcceptedAutomaticallyOnlyWhereSet holds
// spec.tools.autoAcceptChanges to Crewlet's tools.auto_accept_changes: true is
// written beside the levels or alone, and false, like unset, writes nothing,
// Crewlet's own default of holding each change for a review, so a server that
// sets nothing else writes no tools block.
func TestToolChangesAreAcceptedAutomaticallyOnlyWhereSet(t *testing.T) {
	const autoAccept = "auto_accept_changes"
	for _, c := range []struct {
		name  string
		tools v1alpha1.MCPTools
		// want is the entry's tools block, nil for none.
		want map[string]any
	}{
		{"set beside the levels", v1alpha1.MCPTools{
			Default: v1alpha1.MCPToolLevelBlocked, AutoAcceptChanges: new(true),
			Levels: []v1alpha1.MCPToolLevelEntry{{Tool: toolCrawl, Level: v1alpha1.MCPToolLevelApprove}},
		}, map[string]any{
			keyDefault: levelBlocked, autoAccept: true, "levels": map[string]any{toolCrawl: levelApprove},
		}},
		{"set alone", v1alpha1.MCPTools{AutoAcceptChanges: new(true)}, map[string]any{autoAccept: true}},
		{"false beside a default", v1alpha1.MCPTools{Default: v1alpha1.MCPToolLevelAllow, AutoAcceptChanges: new(false)},
			map[string]any{keyDefault: levelAllow}},
		{"false alone", v1alpha1.MCPTools{AutoAcceptChanges: new(false)}, nil},
		{"unset beside a default", v1alpha1.MCPTools{Default: v1alpha1.MCPToolLevelApprove},
			map[string]any{keyDefault: levelApprove}},
		{"unset with nothing else", v1alpha1.MCPTools{}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := optionalInput(t)
			toolServer(in, optionalTavily).Spec.Tools = c.tools
			tools, ok := renderedTavily(t, in)["tools"]
			if c.want == nil {
				if ok {
					t.Errorf("the entry writes tools %v, want none", tools)
				}
				return
			}
			if !reflect.DeepEqual(tools, c.want) {
				t.Errorf("the entry writes tools %v, want %v", tools, c.want)
			}
		})
	}
}

// renderedTavily is the tavily mcp_servers entry of the document in renders.
func renderedTavily(t *testing.T, in *Input) map[string]any {
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
		if entry := server.(map[string]any); entry[keyName] == optionalTavily {
			return entry
		}
	}
	t.Fatalf("the document has no tool server %s", optionalTavily)
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
