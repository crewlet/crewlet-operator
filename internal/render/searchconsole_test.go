package render

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// caseSearchConsole is the company case on Search Console, and the name of
// the Integration that connects it, in that case and in the sample company
// withSearchConsole connects.
const caseSearchConsole = "search-console"

// scSampleSite is the property the sample company's Search Console
// Integration lists.
const scSampleSite = "sc-domain:nimbus.example"

// scApproverAt is where the engine refuses a Search Console approver that is
// no person's seat; it says gaApproverSays there, as for every growth product.
const scApproverAt = "search_console.approvers[0]"

// withSearchConsole connects the sample company to Google Cloud and to Search
// Console, routing its agents' held acts to the Seats named.
func withSearchConsole(in *Input, approvers ...string) {
	withGoogleCloud(in)
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseSearchConsole,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			SearchConsole: &v1alpha1.SearchConsoleIntegration{
				Sites: []string{scSampleSite}, ApproverRefs: localRefs(approvers...),
			},
		},
	})
}

// searchConsoleApprovedBy writes into a sample company's document the Google
// Cloud and Search Console blocks the render writes for withSearchConsole,
// with handle as the one approver, as the operator would write a reference
// it did not refuse.
func searchConsoleApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withSearchConsole(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		for _, vendor := range []string{VendorGoogleCloud, VendorSearchConsole} {
			block, ok := from[vendor].(map[string]any)
			if !ok {
				t.Fatalf("the render writes no %s block", vendor)
			}
			into[vendor] = block
		}
		into[VendorSearchConsole].(map[string]any)[keyApprovers] = []any{handle}
	}
}

// TestSearchConsoleRendersItsTwoBlocksAlone holds the Search Console
// Integration and a Seat's searchConsole to the two blocks they map onto:
// with both added, the sample company's document is the one it was with
// neither, plus integrations.search_console and the seat's own
// search_console block, each as Crewlet writes it.
func TestSearchConsoleRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGoogleCloud(in)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withSearchConsole(in, seatJane)
	seat(in, seatAna).Spec.Integrations.SearchConsole = v1alpha1.SearchConsoleSeatIntegration{
		Enabled: new(true), Level: v1alpha1.SearchConsoleLevelOperate, Sites: []string{scSampleSite},
	}
	after := renderTree(t, in)

	integrations, _ := after[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorSearchConsole], map[string]any{
		keyEnabled: true, keySites: []any{scSampleSite}, keyApprovers: []any{seatJane},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.search_console is %v, want %v", got, want)
	}
	delete(integrations, VendorSearchConsole)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorSearchConsole], map[string]any{
		keyEnabled: true, keyLevel: levelOperate, keySites: []any{scSampleSite},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's search_console is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorSearchConsole)
	if len(seatBlocks) == 0 {
		delete(role, keyIntegrations)
	}

	if !reflect.DeepEqual(after, before) {
		got, err := yaml.Marshal(after)
		if err != nil {
			t.Fatal(err)
		}
		want, err := yaml.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("past its two blocks the document changed:\n%s\nwas\n%s", got, want)
	}
}
