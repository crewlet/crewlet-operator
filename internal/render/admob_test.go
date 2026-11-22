package render

import (
	"reflect"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// caseAdMob is the company case on AdMob, and the name of the Integration
// that connects it, in that case and in the sample company withAdMob connects.
const caseAdMob = "admob"

// The account and the OAuth client the sample company's AdMob Integration
// names, and the keys of a growth block's accounts, approvers, level and
// OAuth client ID.
const (
	adMobSampleAccount = "pub-1234567890123456"
	adMobClientID      = "123456789012-abc123def456.apps.googleusercontent.com"
	adMobOAuthName     = "google-oauth"
	adMobOAuthKey      = "clientSecret"
	keyAccounts        = "accounts"
	keyApprovers       = "approvers"
	keyLevel           = "level"
	keyOAuthClientID   = "client_id"
)

// Where the engine refuses an AdMob approver that is no person's seat.
const adMobApproverAt = "admob.approvers[0]"

// withAdMob connects the sample company to AdMob, routing its agents' held
// acts to the Seats named.
func withAdMob(in *Input, approvers ...string) {
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseAdMob,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			AdMob: v1alpha1.AdMobIntegration{
				OAuth: v1alpha1.GoogleOAuthClient{
					ClientID:        adMobClientID,
					ClientSecretRef: v1alpha1.SecretKeyReference{Name: adMobOAuthName, Key: adMobOAuthKey},
				},
				Accounts:     []string{adMobSampleAccount},
				ApproverRefs: localRefs(approvers...),
			},
		},
	})
}

// adMobApprovedBy writes into a sample company's document the AdMob block the
// render writes for withAdMob, with handle as the one approver, as the
// operator would write a reference it did not refuse.
func adMobApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withAdMob(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		block, ok := from[VendorAdMob].(map[string]any)
		if !ok {
			t.Fatalf("the render writes no %s block", VendorAdMob)
		}
		block[keyApprovers] = []any{handle}
		into[VendorAdMob] = block
	}
}

// TestAdMobRendersItsTwoBlocksAlone holds the AdMob Integration and a Seat's
// admob to the two blocks they map onto: with both added, the sample
// company's document is the one it was with neither, plus integrations.admob
// and the seat's own admob block, each as Crewlet writes it. The OAuth
// client's secret is written as the store name of its Secret key, which the
// document's Refs carry, so it is pushed to the engine's store as every
// referenced Secret is.
func TestAdMobRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withAdMob(in, seatJane)
	seat(in, seatAna).Spec.Integrations.AdMob = v1alpha1.AdMobSeatIntegration{
		Enabled: new(true), Level: v1alpha1.AdMobLevelOperate, Accounts: []string{adMobSampleAccount},
	}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	after := renderTree(t, in)

	i := slices.IndexFunc(doc.Refs, func(k secretref.Key) bool {
		return k.Name == adMobOAuthName && k.Key == adMobOAuthKey
	})
	if i < 0 {
		t.Fatalf("the document's Refs %v do not carry the OAuth client's secret", doc.Refs)
	}
	integrations, _ := after[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorAdMob], map[string]any{
		keyEnabled: true,
		"oauth": map[string]any{
			keyOAuthClientID: adMobClientID, keyOAuthClientSecret: "${" + secretsync.StoreName(doc.Refs[i]) + "}",
		},
		keyAccounts: []any{adMobSampleAccount}, keyApprovers: []any{seatJane},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.admob is %v, want %v", got, want)
	}
	delete(integrations, VendorAdMob)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorAdMob], map[string]any{
		keyEnabled: true, keyLevel: levelOperate, keyAccounts: []any{adMobSampleAccount},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's admob is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorAdMob)
	if len(seatBlocks) == 0 {
		delete(role, keyIntegrations)
	}

	if !reflect.DeepEqual(after, before) {
		got, want := renderYAML(t, after), renderYAML(t, before)
		t.Errorf("past its two blocks the document changed:\n%s\nwas\n%s", got, want)
	}
}
