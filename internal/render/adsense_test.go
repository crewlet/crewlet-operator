package render

import (
	"reflect"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// caseAdSense is the company case on AdSense, and the name of the Integration
// that connects it, in that case and in the sample company withAdSense
// connects.
const caseAdSense = "adsense"

// The account the sample company's AdSense Integration names. It signs in as
// AdMob's OAuth client, which the two products may share.
const adSenseSampleAccount = "pub-2345678901234567"

// Where the engine refuses an AdSense approver that is no person's seat.
const adSenseApproverAt = "adsense.approvers[0]"

// withAdSense connects the sample company to AdSense as the OAuth client
// withAdMob names, routing its agents' held acts to the Seats named.
func withAdSense(in *Input, approvers ...string) {
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseAdSense,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			AdSense: &v1alpha1.AdSenseIntegration{
				OAuth: v1alpha1.GoogleOAuthClient{
					ClientID:        adMobClientID,
					ClientSecretRef: v1alpha1.SecretKeyReference{Name: adMobOAuthName, Key: adMobOAuthKey},
				},
				Accounts:     []string{adSenseSampleAccount},
				ApproverRefs: localRefs(approvers...),
			},
		},
	})
}

// adSenseApprovedBy writes into a sample company's document the AdSense block
// the render writes for withAdSense, with handle as the one approver, as the
// operator would write a reference it did not refuse.
func adSenseApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withAdSense(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		block, ok := from[VendorAdSense].(map[string]any)
		if !ok {
			t.Fatalf("the render writes no %s block", VendorAdSense)
		}
		block[keyApprovers] = []any{handle}
		into[VendorAdSense] = block
	}
}

// TestAdSenseRendersItsTwoBlocksAlone holds the Integration and a Seat's
// adsense to integrations.adsense and the seat's own block, past which the
// document is unchanged; the OAuth client's secret is a store name Refs carry.
func TestAdSenseRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withAdSense(in, seatJane)
	seat(in, seatAna).Spec.Integrations.AdSense = v1alpha1.AdSenseSeatIntegration{
		Enabled: new(true), Level: v1alpha1.AdSenseLevelOperate, Accounts: []string{adSenseSampleAccount},
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
	if got, want := integrations[VendorAdSense], map[string]any{
		keyEnabled: true,
		"oauth": map[string]any{
			keyOAuthClientID: adMobClientID, keyOAuthClientSecret: "${" + secretsync.StoreName(doc.Refs[i]) + "}",
		},
		keyAccounts: []any{adSenseSampleAccount}, keyApprovers: []any{seatJane},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.adsense is %v, want %v", got, want)
	}
	delete(integrations, VendorAdSense)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorAdSense], map[string]any{
		keyEnabled: true, keyLevel: levelOperate, keyAccounts: []any{adSenseSampleAccount},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's adsense is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorAdSense)
	if len(seatBlocks) == 0 {
		delete(role, keyIntegrations)
	}

	if !reflect.DeepEqual(after, before) {
		got, want := renderYAML(t, after), renderYAML(t, before)
		t.Errorf("past its two blocks the document changed:\n%s\nwas\n%s", got, want)
	}
}

// TestAdSenseAndAdMobShareOneOAuthClient holds both blocks to one store
// variable for a shared Secret key, pushed once; the adsense company case has
// the engine admit the sharing.
func TestAdSenseAndAdMobShareOneOAuthClient(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withAdMob(in)
	withAdSense(in)
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var shared []secretref.Key
	for _, k := range doc.Refs {
		if k.Name == adMobOAuthName && k.Key == adMobOAuthKey {
			shared = append(shared, k)
		}
	}
	if len(shared) != 1 {
		t.Fatalf("the document's Refs carry the shared OAuth client's secret %d times, want once: %v", len(shared), doc.Refs)
	}
	want := "${" + secretsync.StoreName(shared[0]) + "}"
	leaves := flatten(t, doc.YAML)
	for _, vendor := range []string{VendorAdMob, VendorAdSense} {
		if got := leaves["integrations."+vendor+".oauth.client_secret"]; got != want {
			t.Errorf("integrations.%s.oauth.client_secret = %v, want %s", vendor, got, want)
		}
	}
}
