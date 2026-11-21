package render

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// caseGoogleAds is the company case on Google Ads, and the name of the
// Integration that connects it, in that case and in the sample company
// withGoogleAds connects.
const caseGoogleAds = "google-ads"

// adsSampleCustomer is the account the sample company's Google Ads
// Integration lists and caps.
const adsSampleCustomer = "1234567890"

// adsApproverAt is where the engine refuses a Google Ads approver that is no
// person's seat; it says gaApproverSays there, as for every growth product.
const adsApproverAt = "google_ads.approvers[0]"

// withGoogleAds connects the sample company to Google Cloud and to Google
// Ads, capping its one account and routing its agents' held acts to the
// Seats named.
func withGoogleAds(in *Input, approvers ...string) {
	withGoogleCloud(in)
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseGoogleAds,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			GoogleAds: &v1alpha1.GoogleAdsIntegration{
				Customers:    []v1alpha1.GoogleAdsCustomer{{ID: adsSampleCustomer, Manager: "9876543210"}},
				ApproverRefs: localRefs(approvers...),
				Spend: v1alpha1.GrowthSpend{Accounts: []v1alpha1.GrowthSpendCap{
					{Account: adsSampleCustomer, Currency: "EUR", PerChange: "50.00", Daily: "200.00"},
				}},
			},
		},
	})
}

// googleAdsApprovedBy writes into a sample company's document the Google
// Cloud and Google Ads blocks the render writes for withGoogleAds, with
// handle as the one approver, as the operator would write a reference it did
// not refuse.
func googleAdsApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withGoogleAds(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		for _, vendor := range []string{VendorGoogleCloud, VendorGoogleAds} {
			block, ok := from[vendor].(map[string]any)
			if !ok {
				t.Fatalf("the render writes no %s block", vendor)
			}
			into[vendor] = block
		}
		into[VendorGoogleAds].(map[string]any)["approvers"] = []any{handle}
	}
}

// TestGoogleAdsRendersItsTwoBlocksAlone holds the Google Ads Integration and
// a Seat's googleAds to the two blocks they map onto: with both added, the
// sample company's document is the one it was with neither, plus
// integrations.google_ads and the seat's own google_ads block, each as
// Crewlet writes it, its caps included.
func TestGoogleAdsRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGoogleCloud(in)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withGoogleAds(in, seatJane)
	seat(in, seatAna).Spec.Integrations.GoogleAds = v1alpha1.GoogleAdsSeatIntegration{
		Enabled: new(true), Level: v1alpha1.GoogleAdsLevelAdminister, Customers: []string{adsSampleCustomer},
	}
	after := renderTree(t, in)

	integrations, _ := after[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorGoogleAds], map[string]any{
		keyEnabled:  true,
		"customers": []any{map[string]any{"id": adsSampleCustomer, "manager": "9876543210"}},
		"approvers": []any{seatJane},
		"spend": map[string]any{"accounts": []any{map[string]any{
			"account": adsSampleCustomer, "currency": "EUR", "per_change": "50.00", "daily": "200.00",
		}}},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.google_ads is %v, want %v", got, want)
	}
	delete(integrations, VendorGoogleAds)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorGoogleAds], map[string]any{
		keyEnabled: true, "level": levelAdminister, "customers": []any{adsSampleCustomer},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's google_ads is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorGoogleAds)
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
