package render

import (
	"bytes"
	"context"
	"slices"
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
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
    main: {type: anthropic, model: m, api_keys: ["__redacted__"]}
  llm_order: [main]
turn_engine: {extension_enabled: true}
roles:
- {name: Ana, handle: ana, kind: agent, goal: Answer questions., llm: {default: main, review: main}}
- {name: Bo, handle: bo, kind: agent, llm: main}
units:
- {id: eng, name: Engineering, roles: [{name: Cy, handle: cy, kind: agent, llm: [main, backup]}]}
`,
			want: []string{"providers.llm.main.api_keys[0]"},
		},
		{
			name: "an engineConfig setting the Company sets, changed, and its block otherwise defaulted",
			active: `name: Acme
providers:
  llm:
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
    main: {type: anthropic, model: m, api_keys: ["${K8S_MAIN_KEY_AAAAAAAAAAAA}"]}
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
