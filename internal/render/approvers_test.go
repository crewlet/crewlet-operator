package render

import (
	"reflect"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// growthApprovers is each growth Integration's member and how a test gives
// the sample company one whose approverRefs name the Seats named.
var growthApprovers = []struct {
	member, vendor, integration string
	with                        func(*Input, ...string)
}{
	{"googleAnalytics", VendorGoogleAnalytics, caseGoogleAnalytics, withGoogleAnalytics},
	{"googleAds", VendorGoogleAds, caseGoogleAds, withGoogleAds},
}

// TestEveryGrowthIntegrationResolvesApproversAlike holds every growth
// Integration's approverRefs to one reading: each Human Seat by its handle,
// in the order named, and a Seat that does not exist or an Agent seat refused
// at the member's own field, as Crewlet refuses an approver that is no
// person's seat.
func TestEveryGrowthIntegrationResolvesApproversAlike(t *testing.T) {
	for _, g := range growthApprovers {
		t.Run(g.member, func(t *testing.T) {
			in := companyCaseNamed(t, sampleCompany).input(t)
			lead := seat(in, seatJane).DeepCopy()
			lead.Name, lead.Spec.Handle, lead.Spec.DisplayName = "lead", "growth-lead", "Growth Lead"
			lead.Spec.Contact, lead.Spec.Manages, lead.Spec.Availability = v1alpha1.SeatContact{}, nil, ""
			in.Seats = append(in.Seats, *lead)
			g.with(in, lead.Name, seatJane)
			integrations, _ := renderTree(t, in)[keyIntegrations].(map[string]any)
			block, _ := integrations[g.vendor].(map[string]any)
			if got, want := block["approvers"], []any{"growth-lead", seatJane}; !reflect.DeepEqual(got, want) {
				t.Errorf("approvers are %v, want the Human seats' handles in the order named, %v", got, want)
			}

			for _, c := range []struct {
				name, reason string
			}{{"growth", v1alpha1.ReasonRefNotFound}, {seatAna, v1alpha1.ReasonAgentSeat}} {
				in := companyCaseNamed(t, sampleCompany).input(t)
				g.with(in, seatJane, c.name)
				_, err := Company(in)
				field := "spec." + g.member + ".approverRefs[1].name"
				if !slices.ContainsFunc(RefErrors(err), func(r *RefError) bool {
					return r.Kind == KindIntegration && r.Name == g.integration && r.Field == field && r.Reason == c.reason
				}) {
					t.Errorf("an approver naming %s is not refused at %s with %s: %v", c.name, field, c.reason, err)
				}
			}
		})
	}
}
