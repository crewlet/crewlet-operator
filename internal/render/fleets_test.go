package render

import (
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The fleets of the llm-fleets case, its providers and its seats.
const (
	fleetOpus   = "claude-opus"
	fleetSpare  = "spare"
	fleetSonnet = "sonnet"
	seatLead    = "lead"
	seatSupport = "support"
	memberMax   = "max"
	memberTeam  = "team"
	memberAPI   = "api"
	// atDefaultHead is the field of a seat's executor chain's first entry.
	atDefaultHead = fieldLLMDefault + "[0].name"
)

// fleetChain is a chain naming fleet name alone.
func fleetChain(name string) []v1alpha1.LLMProviderReference {
	return []v1alpha1.LLMProviderReference{{Kind: v1alpha1.LLMProviderKindFleet, Name: name}}
}

// fleetsCase is the llm-fleets case's input with mutate applied.
func fleetsCase(t *testing.T, mutate func(*Input)) *Input {
	t.Helper()
	in := companyCaseNamed(t, caseLLMFleets).input(t)
	if mutate != nil {
		mutate(in)
	}
	return in
}

// withoutDefaultFleet leaves default-fleet out of the Company's fleets, so
// Crewlet synthesises it with no members.
func withoutDefaultFleet(in *Input) {
	in.Company.Spec.LLM.Fleets = slices.DeleteFunc(in.Company.Spec.LLM.Fleets, func(f v1alpha1.LLMFleet) bool {
		return f.Name == v1alpha1.DefaultLLMFleet
	})
}

// renderedProviders is the providers block of in's document, decoded.
func renderedProviders(t *testing.T, in *Input) map[string]any {
	t.Helper()
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Providers map[string]any `json:"providers"`
	}
	if err := yaml.Unmarshal(doc.YAML, &out); err != nil {
		t.Fatal(err)
	}
	return out.Providers
}

// TestLLMFleetsRenderAsTheEngineServesThem: default-fleet is written only when
// the Company lists it, as Crewlet synthesises it otherwise and GET /config
// never serves it, so drift stays clean; a reference to it needs no listing.
func TestLLMFleetsRenderAsTheEngineServesThem(t *testing.T) {
	t.Parallel()
	in := fleetsCase(t, withoutDefaultFleet)
	fleets, _ := renderedProviders(t, in)["llm_fleets"].(map[string]any)
	if _, ok := fleets[v1alpha1.DefaultLLMFleet]; ok {
		t.Errorf("llm_fleets holds default-fleet, which the Company does not list: %v", fleets)
	}
	if got := len(fleets); got != 2 {
		t.Errorf("llm_fleets holds %d fleets, want 2: %v", got, fleets)
	}

	in = fleetsCase(t, func(in *Input) {
		in.Company.Spec.LLM.Fleets = nil
		seat(in, seatLead).Spec.LLM = v1alpha1.SeatLLM{}
		in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{}
	})
	if fleets, ok := renderedProviders(t, in)["llm_fleets"]; ok {
		t.Errorf("a Company with no fleets renders llm_fleets %v, want none", fleets)
	}
}

// TestLLMFleetReferencesThatDoNotResolveAreRefused: Crewlet refuses a chain
// or the assistant naming a fleet it does not have, and a member that is not
// a providers.llm key, so each is refused on the object that names it.
func TestLLMFleetReferencesThatDoNotResolveAreRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   []string
	}{
		{
			name:   "a seat's chain names a fleet the Company does not list",
			mutate: func(in *Input) { seat(in, seatLead).Spec.LLM.Default = fleetChain("haiku") },
			want:   []string{"Seat/lead " + atDefaultHead + " RefNotFound"},
		},
		{
			name: "the assistant names a fleet the Company does not list",
			mutate: func(in *Input) {
				in.Company.Spec.Assistant.Model = v1alpha1.LLMProviderReference{Kind: v1alpha1.LLMProviderKindFleet, Name: "haiku"}
			},
			want: []string{"Company/fleet spec.assistant.model.name RefNotFound"},
		},
		{
			name: "a fleet's member is no LLMProvider",
			mutate: func(in *Input) {
				in.Company.Spec.LLM.Fleets[0].Members[1].Name = "enterprise"
			},
			want: []string{"Company/fleet spec.llm.fleets[0].members[1].name RefNotFound"},
		},
		{
			// default-fleet exists unlisted, as Crewlet synthesises it.
			name: "a seat's chain names default-fleet, unlisted",
			mutate: func(in *Input) {
				withoutDefaultFleet(in)
				seat(in, seatSupport).Spec.LLM.Default = fleetChain(v1alpha1.DefaultLLMFleet)
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := Company(fleetsCase(t, c.mutate))
			got := refusalsOf(err)
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Errorf("refused %v (%v), want %v", got, err, c.want)
			}
		})
	}
}

// TestExecutorReadsAFleetAsItsMembers: any member of a fleet may take a seat's
// work, so the executor's verdict reads the fleet's members in order; a seat
// with no chain runs on default-fleet once it has a member; a fleet with no
// members runs nothing and refuses nothing, as its seats wait until one joins.
func TestExecutorReadsAFleetAsItsMembers(t *testing.T) {
	t.Parallel()
	type want struct{ member, says string }
	for _, c := range []struct {
		name   string
		mutate func(*Input)
		want   map[string]want
	}{
		{
			name: "a seat on a fleet, and one on default-fleet",
			want: map[string]want{
				seatLead:    {memberMax, "LLMProvider max, a member of fleet " + fleetOpus},
				seatSupport: {memberAPI, "a member of fleet default-fleet, which a seat whose spec.llm.default is empty runs on"},
			},
		},
		{
			name: "default-fleet has no members: the head of the model order",
			mutate: func(in *Input) {
				withoutDefaultFleet(in)
				in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{{Name: memberTeam}}
				in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{}
			},
			want: map[string]want{
				seatLead:    {memberMax, "a member of fleet claude-opus"},
				seatSupport: {memberTeam, "the head of Company fleet's spec.llm.order"},
			},
		},
		{
			name: "a seat on a fleet with no members",
			mutate: func(in *Input) {
				seat(in, seatLead).Spec.LLM.Default = fleetChain(fleetSpare)
			},
			want: map[string]want{
				seatSupport: {memberAPI, "default-fleet"},
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := fleetsCase(t, c.mutate)
			if _, err := Company(in); err != nil {
				t.Fatalf("Company() refused the document: %v", err)
			}
			got := ExecutorOf(in)
			if len(got) != len(c.want) {
				t.Errorf("ExecutorOf() has %d seats, want %d: %v", len(got), len(c.want), got)
			}
			for seat, w := range c.want {
				e, ok := got[seat]
				switch {
				case !ok:
					t.Errorf("seat %s has no executor", seat)
				case e.Member != w.member:
					t.Errorf("seat %s runs on %s, want %s", seat, e.Member, w.member)
				case !strings.Contains(e.Message, w.says):
					t.Errorf("seat %s's executor says %q, want it to say %q", seat, e.Message, w.says)
				}
			}
		})
	}
}

// TestNoExecutorHarnessNamesTheFleet: a fleet's members share one harness, and
// Crewlet refuses a seat whose executor could run on none of them.
func TestNoExecutorHarnessNamesTheFleet(t *testing.T) {
	t.Parallel()
	in := fleetsCase(t, func(in *Input) {
		for i := range in.LLMProviders {
			if p := &in.LLMProviders[i]; p.Name == memberMax || p.Name == memberTeam {
				p.Spec.Harness, p.Spec.Model = v1alpha1.HarnessCursorAgent, "gpt-5"
				p.Spec.Effort = ""
			}
		}
	})
	_, err := Company(in)
	refusals := RefErrors(err)
	if len(refusals) != 1 {
		t.Fatalf("refusals %v, want one", err)
	}
	r := refusals[0]
	if r.Kind != KindSeat || r.Name != seatLead || r.Field != fieldLLMDefault || r.Reason != v1alpha1.ReasonNoExecutorHarness {
		t.Errorf("refused %s/%s %s %s, want Seat/lead %s NoExecutorHarness", r.Kind, r.Name, r.Field, r.Reason, fieldLLMDefault)
	}
	if !strings.Contains(r.Message, "a member of fleet claude-opus") {
		t.Errorf("the refusal does not name the fleet: %q", r.Message)
	}
}
