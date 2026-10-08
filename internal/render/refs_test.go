package render

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestCompanyRefusesWhatItCannotRender covers every reference between
// objects the API server cannot check, each broken on its own in an
// otherwise valid company. Each refusal must name the object and the field
// that hold the reference, with the reason the controller reports it with.
func TestCompanyRefusesWhatItCannotRender(t *testing.T) {
	type refusal struct{ kind, name, field, reason string }
	const (
		fieldParent  = "spec.parentRef.name"
		fieldManages = "spec.manages[0].name"
	)
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   []refusal
	}{
		{
			name:   "a seat's unit is missing",
			mutate: func(in *Input) { seat(in, seatAna).Spec.UnitRef.Name = "sre" },
			want:   []refusal{{KindSeat, seatAna, "spec.unitRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a unit's parent is missing",
			mutate: func(in *Input) { unit(in, unitPlatform).Spec.ParentRef.Name = "infra" },
			want:   []refusal{{KindUnit, unitPlatform, fieldParent, v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "two units are each other's parent",
			mutate: func(in *Input) {
				unit(in, unitEngineering).Spec.ParentRef.Name = unitPlatform
				// A unit below the cycle is unreachable, but its own
				// reference resolves: only the cycle is reported.
				in.Units = append(in.Units, v1alpha1.Unit{
					ObjectMeta: unit(in, unitPlatform).ObjectMeta,
					Spec:       v1alpha1.UnitSpec{ParentRef: v1alpha1.LocalObjectReference{Name: unitPlatform}},
				})
				in.Units[len(in.Units)-1].Name = "sre"
			},
			want: []refusal{
				{KindUnit, unitEngineering, fieldParent, v1alpha1.ReasonCycle},
				{KindUnit, unitPlatform, fieldParent, v1alpha1.ReasonCycle},
			},
		},
		{
			name:   "a unit is its own parent",
			mutate: func(in *Input) { unit(in, unitEngineering).Spec.ParentRef.Name = unitEngineering },
			want:   []refusal{{KindUnit, unitEngineering, fieldParent, v1alpha1.ReasonCycle}},
		},
		{
			name:   "a unit's lead is missing",
			mutate: func(in *Input) { unit(in, unitPlatform).Spec.LeadRef.Name = "bob" },
			want:   []refusal{{KindUnit, unitPlatform, "spec.leadRef.name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a managed seat is missing",
			mutate: func(in *Input) { seat(in, seatJane).Spec.Manages[0].Name = "coo" },
			want:   []refusal{{KindSeat, seatJane, fieldManages, v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a managed unit is missing",
			mutate: func(in *Input) { seat(in, seatCTO).Spec.Manages[0].Name = "sales" },
			want:   []refusal{{KindSeat, seatCTO, fieldManages, v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a managed unit is named like a seat",
			mutate: func(in *Input) {
				unit(in, unitPlatform).Spec.DisplayName = "Agent CTO"
			},
			want: []refusal{{KindSeat, seatCTO, fieldManages, v1alpha1.ReasonConflict}},
		},
		{
			name: "a seat's tool server is missing",
			mutate: func(in *Input) {
				seat(in, seatAna).Spec.MCP[0].Name = "gitlab"
				seat(in, seatAna).Spec.Sandbox.MCPServerRefs[0].Name = "gitlab"
			},
			want: []refusal{
				{KindSeat, seatAna, "spec.mcp[0].name", v1alpha1.ReasonRefNotFound},
				{KindSeat, seatAna, "spec.sandbox.mcpServerRefs[0].name", v1alpha1.ReasonRefNotFound},
			},
		},
		{
			name: "a unit's tool server is missing",
			mutate: func(in *Input) {
				unit(in, unitPlatform).Spec.MCP = []v1alpha1.MCPCredentials{{Name: "datadog", Env: []v1alpha1.MCPCredentialValue{{
					Name: "DD_API_KEY", ValueFrom: v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "dd", Key: "apiKey"}},
				}}}}
			},
			want: []refusal{{KindUnit, unitPlatform, "spec.mcp[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a seat's model provider is missing",
			mutate: func(in *Input) { seat(in, seatCTO).Spec.LLM.Default[0].Name = "openai" },
			want:   []refusal{{KindSeat, seatCTO, "spec.llm.default[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "the company's model order names a missing provider",
			mutate: func(in *Input) { in.Company.Spec.LLM.Order[0].Name = "openai" },
			want:   []refusal{{KindCompany, sampleCompany, "spec.llm.order[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a seat names a platform provider the company did not opt into",
			mutate: func(in *Input) {
				off := false
				in.Company.Spec.LLM.UsePlatformProviders = &off
			},
			want: []refusal{{KindSeat, seatAna, "spec.llm.auxiliary[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name:   "a seat names a platform provider the class does not offer",
			mutate: func(in *Input) { seat(in, seatAna).Spec.LLM.Auxiliary[0].Name = "platform-gpt" },
			want:   []refusal{{KindSeat, seatAna, "spec.llm.auxiliary[0].name", v1alpha1.ReasonRefNotFound}},
		},
		{
			name: "a provider is named like a platform provider",
			mutate: func(in *Input) {
				p := in.LLMProviders[0]
				p.Name = "platform-claude"
				in.LLMProviders = append(in.LLMProviders, p)
			},
			want: []refusal{{KindLLMProvider, "platform-claude", "metadata.name", v1alpha1.ReasonConflict}},
		},
		{
			name: "two integrations connect one vendor",
			mutate: func(in *Input) {
				i := *integration(in, "slack")
				i.Name = "slack-2"
				in.Integrations = append(in.Integrations, i)
			},
			want: []refusal{{KindIntegration, "slack-2", "spec.slack", v1alpha1.ReasonConflict}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, sampleCompany).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if doc != nil {
				t.Errorf("a document was rendered despite the refusal")
			}
			refusals := RefErrors(err)
			got := make([]refusal, 0, len(refusals))
			for _, e := range refusals {
				got = append(got, refusal{e.Kind, e.Name, e.Field, e.Reason})
				if e.Message == "" || !strings.Contains(err.Error(), e.Message) {
					t.Errorf("%v: the message is not in the error: %q", e, err)
				}
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("refusals %v, want %v (err %v)", got, c.want, err)
			}
			if _, ok := errors.AsType[*RefError](err); !ok {
				t.Fatalf("errors.AsType finds no *RefError in %v", err)
			}
		})
	}
}

func seat(in *Input, name string) *v1alpha1.Seat {
	for i := range in.Seats {
		if in.Seats[i].Name == name {
			return &in.Seats[i]
		}
	}
	panic("no Seat " + name)
}

func unit(in *Input, name string) *v1alpha1.Unit {
	for i := range in.Units {
		if in.Units[i].Name == name {
			return &in.Units[i]
		}
	}
	panic("no Unit " + name)
}

func integration(in *Input, name string) *v1alpha1.Integration {
	for i := range in.Integrations {
		if in.Integrations[i].Name == name {
			return &in.Integrations[i]
		}
	}
	panic("no Integration " + name)
}

// TestAttributeFindsTheInnermostObject maps the paths of problems the
// engine's dry run could report on the Nimbus document, as the engine prints
// their segments, back to the objects that produced them.
func TestAttributeFindsTheInnermostObject(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, sampleCompany).input(t))
	if err != nil {
		t.Fatal(err)
	}
	company := ObjectRef{KindCompany, sampleCompany}
	for _, c := range []struct {
		segments string
		want     ObjectRef
	}{
		{`["name"]`, company},
		{`["turn_engine","extension_enabled"]`, company},
		{`["providers","embeddings","model"]`, company},
		{`["providers","llm_order",1]`, company},
		{`["integrations","public_base_url"]`, company},
		{`["providers","llm","anthropic","api_keys",0]`, ObjectRef{KindLLMProvider, "anthropic"}},
		{`["providers","llm","platform-claude","model"]`, ObjectRef{KindEngineClass, "standard"}},
		{`["mcp_servers",0,"url"]`, ObjectRef{KindMCPServer, sampleGitHub}},
		{`["integrations","github","webhook_secret"]`, ObjectRef{KindIntegration, sampleGitHub}},
		{`["integrations","slack","typing_status"]`, ObjectRef{KindIntegration, "slack"}},
		{`["roles",0,"contact"]`, ObjectRef{KindSeat, seatJane}},
		{`["units",0,"lead"]`, ObjectRef{KindUnit, unitEngineering}},
		{`["units",0,"roles",0,"llm"]`, ObjectRef{KindSeat, seatCTO}},
		{`["units",0,"children",0,"space"]`, ObjectRef{KindUnit, unitPlatform}},
		{`["units",0,"children",0,"roles",0,"sandbox","env"]`, ObjectRef{KindSeat, seatAna}},
	} {
		var p Path
		if err := p.UnmarshalJSON([]byte(c.segments)); err != nil {
			t.Fatal(err)
		}
		got, ok := doc.Attribute(p)
		if !ok || got != c.want {
			t.Errorf("Attribute(%s) = %v, %v; want %v", p, got, ok, c.want)
		}
	}
	for _, segments := range []string{`null`, `["units",1]`, `["workers"]`} {
		var p Path
		if err := p.UnmarshalJSON([]byte(segments)); err != nil {
			t.Fatal(err)
		}
		if got, ok := doc.Attribute(p); ok {
			t.Errorf("Attribute(%s) = %v; want no object for a path nothing rendered", segments, got)
		}
	}
}

// TestPathReadsCrewletSegments decodes the segments of a problem as
// `crewlet validate -json` and the dry run print them.
func TestPathReadsCrewletSegments(t *testing.T) {
	var p Path
	if err := p.UnmarshalJSON([]byte(` [ "a.b", 0, "c", 12 ] `)); err != nil {
		t.Fatal(err)
	}
	want := Path{"a.b", 0, "c", 12}
	if !slices.Equal(p, want) || p.String() != "a.b[0].c[12]" {
		t.Fatalf("decoded %#v (%s), want %#v", p, p, want)
	}
	if err := p.UnmarshalJSON([]byte(`null`)); err != nil || p != nil {
		t.Fatalf("null decoded to %#v, %v", p, err)
	}
	for _, bad := range []string{`[true]`, `[1.5]`, `{"a": 1}`} {
		if err := p.UnmarshalJSON([]byte(bad)); err == nil {
			t.Errorf("%s decoded to %#v; want an error", bad, p)
		}
	}
}
