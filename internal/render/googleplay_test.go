package render

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// caseGooglePlay is the company case on Google Play, and the name of the
// Integration that connects it, in that case and in the sample company
// withGooglePlay connects.
const caseGooglePlay = "google-play"

// The app, developer account and reports bucket the sample company's Google
// Play Integration lists.
const (
	playSampleApp     = "com.nimbus.app"
	playSampleAccount = "5732191048837265001"
	playSampleBucket  = "pubsite_prod_rev_5732191048837265001"
)

// The keys of a Google Play block in the company document.
const (
	keyApps              = "apps"
	keyDeveloperAccounts = "developer_accounts"
	keyReportsBuckets    = "reports_buckets"
)

// playApproverAt is where the engine refuses a Google Play approver that is no
// person's seat; it says gaApproverSays there, as for every growth product.
const playApproverAt = "google_play.approvers[0]"

// withGooglePlay connects the sample company to Google Cloud and to Google
// Play, routing its agents' held acts to the Seats named.
func withGooglePlay(in *Input, approvers ...string) {
	withGoogleCloud(in)
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseGooglePlay,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			GooglePlay: &v1alpha1.GooglePlayIntegration{
				Apps: []string{playSampleApp}, DeveloperAccounts: []string{playSampleAccount},
				ReportsBuckets: []string{playSampleBucket}, ApproverRefs: localRefs(approvers...),
			},
		},
	})
}

// googlePlayApprovedBy writes into a sample company's document the Google
// Cloud and Google Play blocks the render writes for withGooglePlay, with
// handle as the one approver, as the operator would write a reference it did
// not refuse.
func googlePlayApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withGooglePlay(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		for _, vendor := range []string{VendorGoogleCloud, VendorGooglePlay} {
			block, ok := from[vendor].(map[string]any)
			if !ok {
				t.Fatalf("the render writes no %s block", vendor)
			}
			into[vendor] = block
		}
		into[VendorGooglePlay].(map[string]any)[keyApprovers] = []any{handle}
	}
}

// TestGooglePlayRendersItsTwoBlocksAlone holds the Google Play Integration and
// a Seat's googlePlay to the two blocks they map onto: with both added, the
// sample company's document is the one it was with neither, plus
// integrations.google_play and the seat's own google_play block, each as
// Crewlet writes it.
func TestGooglePlayRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGoogleCloud(in)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withGooglePlay(in, seatJane)
	seat(in, seatAna).Spec.Integrations.GooglePlay = v1alpha1.GooglePlaySeatIntegration{
		Enabled: new(true), Level: v1alpha1.GooglePlayLevelAdminister, Apps: []string{playSampleApp},
	}
	after := renderTree(t, in)

	integrations, _ := after[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorGooglePlay], map[string]any{
		keyEnabled: true, keyApps: []any{playSampleApp}, keyDeveloperAccounts: []any{playSampleAccount},
		keyReportsBuckets: []any{playSampleBucket}, keyApprovers: []any{seatJane},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.google_play is %v, want %v", got, want)
	}
	delete(integrations, VendorGooglePlay)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorGooglePlay], map[string]any{
		keyEnabled: true, keyLevel: levelAdminister, keyApps: []any{playSampleApp},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's google_play is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorGooglePlay)
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

// TestAnEmptyGooglePlayBlockConnectsEveryApp holds the empty Integration, {},
// to an enabled block with no lists: every app an agent's identity is invited
// to, which a nil pointer could not say.
func TestAnEmptyGooglePlayBlockConnectsEveryApp(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGooglePlay(in)
	in.Integrations[len(in.Integrations)-1].Spec.GooglePlay = &v1alpha1.GooglePlayIntegration{}
	integrations, _ := renderTree(t, in)[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorGooglePlay], map[string]any{keyEnabled: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("an empty googlePlay renders %v, want %v", got, want)
	}
}
