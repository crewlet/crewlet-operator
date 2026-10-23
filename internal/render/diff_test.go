package render

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// paths renders a Diff result for a failure message and a comparison.
func paths(ps []Path) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

// TestDiff pins what Diff finds and what it disregards, on documents spelled
// the way the engine serves them.
func TestDiff(t *testing.T) {
	const rendered = `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine:
  extension_enabled: true
roles:
- name: Ana
  handle: ana
  kind: agent
  goal: Answer questions.
  llm: {default: [main], review: [main]}
- name: Bo
  handle: bo
  kind: agent
  llm: {default: [main]}
units:
- id: eng
  name: Engineering
  roles:
  - {name: Cy, handle: cy, kind: agent, llm: {default: [main, backup]}}
`
	cases := []struct {
		name   string
		active string
		want   []string
	}{
		{
			name: "the engine's own spelling of the same company",
			active: `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine:
  extension_enabled: true
  max_iterations: 3
scheduling: {tick_seconds: 10}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Answer questions., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- id: eng
  name: Engineering
  roles:
  - {name: Cy, handle: cy, kind: agent, llm: [main, backup]}
`,
		},
		{
			name: "a seat's field changed",
			active: `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine: {extension_enabled: true}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Something else., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [main, backup]}]}
`,
			want: []string{"roles[0].goal"},
		},
		{
			name: "a field added to a typed block, a seat added and a seat removed",
			active: `name: Acme
mission: Added in the dashboard.
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine: {extension_enabled: true}
roles:
- {name: Bo, handle: bo, kind: agent, llm: main}
- {name: Di, handle: di, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [main, backup]}]}
`,
			want: []string{"mission", "roles", "roles[0]"},
		},
		{
			name: "a literal credential written over a reference, as the engine serves it",
			active: `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "__redacted__"}}
  llm_order: [main]
turn_engine: {extension_enabled: true}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Answer questions., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [main, backup]}]}
`,
			want: []string{"providers.llm.main.auth.api_key"},
		},
		{
			name: "an engineConfig setting the Company sets, changed, and its block otherwise defaulted",
			active: `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine: {extension_enabled: false, max_iterations: 7}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Answer questions., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [main, backup]}]}
`,
			want: []string{"turn_engine.extension_enabled"},
		},
		{
			name: "a model chain reordered in a unit's seat, and a typed block the render does not have",
			active: `name: Acme
providers:
  llm:
    main: {model: m, auth: {mode: api-key, api_key: "${K8S_MAIN_KEY_AAAAAAAAAAAA}"}}
  llm_order: [main]
turn_engine: {extension_enabled: true}
tracker: {backend: none}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Answer questions., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [backup, main]}]}
`,
			want: []string{"tracker", "units[0].roles[0].llm.default[0]", "units[0].roles[0].llm.default[1]"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Diff([]byte(rendered), []byte(c.active))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(paths(got), c.want) {
				t.Errorf("Diff() = %q, want %q", paths(got), c.want)
			}
		})
	}
}

// TestDiffLeavesOutASettingTheEngineRedacts pins what Diff makes of a tool
// server's setting, which the render writes out where the engine holds
// credentials and serves every literal redacted: the redacted setting is no
// difference, while a credential served redacted, a setting served as a
// reference, and a redacted value anywhere else are.
func TestDiffLeavesOutASettingTheEngineRedacts(t *testing.T) {
	const rendered = `name: Acme
mission: Ship it.
mcp_servers:
- name: slack
  transport: stdio
  command: slack-mcp-server
  env: {SLACK_MCP_ADD_MESSAGE_TOOL: "true", SLACK_MCP_LOG: ""}
- name: github
  transport: http
  url: https://api.githubcopilot.com/mcp/
  headers: {X-MCP-Toolsets: repos}
roles:
- name: Ana
  handle: ana
  kind: agent
  mcp_env:
    slack:
      SLACK_MCP_XOXB_TOKEN: ${K8S_ANA_SLACK_BOT_TOKEN_AAAAAAAAAAAA}
      SLACK_MCP_CHANNELS_CACHE: /home/crewlet/.cache/slack-mcp-server/ana-channels.json
units:
- id: eng
  name: Engineering
  mcp_env:
    gitlab: {GITLAB_HOST: gitlab.example.com}
`
	served := func(change func(string) string) string {
		return change(`name: Acme
mission: Ship it.
mcp_servers:
- {name: slack, transport: stdio, command: slack-mcp-server,
   env: {SLACK_MCP_ADD_MESSAGE_TOOL: __redacted__, SLACK_MCP_LOG: ""}}
- {name: github, transport: http, url: "https://api.githubcopilot.com/mcp/", headers: {X-MCP-Toolsets: __redacted__}}
roles:
- name: Ana
  handle: ana
  kind: agent
  mcp_env:
    slack:
      SLACK_MCP_XOXB_TOKEN: ${K8S_ANA_SLACK_BOT_TOKEN_AAAAAAAAAAAA}
      SLACK_MCP_CHANNELS_CACHE: __redacted__
units:
- {id: eng, name: Engineering, mcp_env: {gitlab: {GITLAB_HOST: __redacted__}}}
`)
	}
	same := func(doc string) string { return doc }
	for _, c := range []struct {
		name   string
		active string
		want   []string
	}{
		{name: "every setting served redacted", active: served(same)},
		{
			name: "a literal credential written over a seat's reference",
			active: served(func(doc string) string {
				return strings.Replace(doc, "${K8S_ANA_SLACK_BOT_TOKEN_AAAAAAAAAAAA}", "__redacted__", 1)
			}),
			want: []string{"roles[0].mcp_env.slack.SLACK_MCP_XOXB_TOKEN"},
		},
		{
			name: "a setting replaced by a reference",
			active: served(func(doc string) string {
				return strings.Replace(doc, "SLACK_MCP_CHANNELS_CACHE: __redacted__", "SLACK_MCP_CHANNELS_CACHE: ${CACHE}", 1)
			}),
			want: []string{"roles[0].mcp_env.slack.SLACK_MCP_CHANNELS_CACHE"},
		},
		{
			name: "a redacted value where no setting is rendered",
			active: served(func(doc string) string {
				return strings.Replace(doc, "mission: Ship it.", "mission: __redacted__", 1)
			}),
			want: []string{"mission"},
		},
		{
			name: "a setting removed",
			active: served(func(doc string) string {
				return strings.Replace(doc, "{GITLAB_HOST: __redacted__}", "{}", 1)
			}),
			want: []string{"units[0].mcp_env.gitlab.GITLAB_HOST"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Diff([]byte(rendered), []byte(c.active))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(paths(got), c.want) {
				t.Errorf("Diff() = %q, want %q", paths(got), c.want)
			}
		})
	}
}

// TestDiffContract hands every rendered document to the real engine and
// diffs it with the document the engine then serves: the engine's own
// encoding of a document it was given must never read as a difference, or
// the operator would report drift, and under Enforce rewrite the document,
// on every reconcile.
func TestDiffContract(t *testing.T) {
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc, err := Company(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			client := enginetest.Start(t).Client()
			active := applyAndRead(t, client, engine.ApplyRequest{Document: doc.YAML, Summary: "contract test", CreateOnly: true})
			got, err := Diff(doc.YAML, active.Document)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) > 0 {
				t.Errorf("Diff() of a document and the engine's encoding of it = %q, want nothing\n--- rendered\n%s\n--- served\n%s",
					paths(got), doc.YAML, active.Document)
			}
		})
	}
}

// TestDiffFindsAnEditMadeThroughTheEngine edits a seat the way a person in
// the dashboard would, through the engine's API, and checks that Diff finds
// the edit and that the document attributes it to the Seat.
func TestDiffFindsAnEditMadeThroughTheEngine(t *testing.T) {
	t.Parallel()
	doc, err := Company(companyCaseNamed(t, sampleCompany).input(t))
	if err != nil {
		t.Fatal(err)
	}
	client := enginetest.Start(t).Client()
	first := applyAndRead(t, client, engine.ApplyRequest{Document: doc.YAML, Summary: "contract test", CreateOnly: true})

	edited := bytes.Replace(first.Document, []byte("goal: Keep CI green and deploys boring."), []byte("goal: Ship faster."), 1)
	if bytes.Equal(edited, first.Document) {
		t.Fatalf("the served document has no goal to edit:\n%s", first.Document)
	}
	second := applyAndRead(t, client, engine.ApplyRequest{Document: edited, Summary: "a person's edit", IfMatch: first.RevisionID})

	got, err := Diff(doc.YAML, second.Document)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Diff() = %q, want the edited goal alone", paths(got))
	}
	if obj, ok := doc.Attribute(got[0]); !ok || obj != (ObjectRef{Kind: KindSeat, Name: seatAna}) {
		t.Errorf("Attribute(%s) = %v, %v; want Seat/%s", got[0], obj, ok, seatAna)
	}
}

// applyAndRead applies req and returns the active configuration after it.
func applyAndRead(t *testing.T, client *engine.Client, req engine.ApplyRequest) engine.ActiveConfig {
	t.Helper()
	ctx := context.Background()
	if _, err := client.ApplyConfig(ctx, req); err != nil {
		t.Fatalf("applying the document: %v", err)
	}
	active, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return active
}
