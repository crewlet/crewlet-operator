package render

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// caseGoogleAnalytics is the company case on Google Analytics 4, and the name
// of the Integration that connects it, in that case and in the sample company
// withGoogleAnalytics connects.
const caseGoogleAnalytics = "google-analytics"

// gaSampleProperty is the property the sample company's Google Analytics
// Integration lists.
const gaSampleProperty = "123456789"

// The keys of a growth block in the company document.
const (
	keyEnabled    = "enabled"
	keyProperties = "properties"
	keySites      = "sites"
)

// Where the engine refuses a Google Analytics approver that is no person's
// seat, and what it says there.
const (
	gaApproverAt   = "google_analytics.approvers[0]"
	gaApproverSays = "is not a person's seat"
)

// withGoogleCloud connects the sample company to Google Cloud, whose identity
// Google Analytics signs in through.
func withGoogleCloud(in *Input) {
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      "nimbus-cloud",
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef:   v1alpha1.CompanyReference{Name: sampleCompany},
			Provisioning: v1alpha1.IntegrationProvisioningEngine,
			GoogleCloud: v1alpha1.GoogleCloudIntegration{
				Provisioner:            "crewlet-provisioner@nimbus-crewlet-id.iam.gserviceaccount.com",
				FederationKeySecretRef: v1alpha1.SecretKeyReference{Name: "nimbus-cloud", Key: "federationKey"},
			},
		},
	})
}

// withGoogleAnalytics connects the sample company to Google Cloud and to
// Google Analytics, routing its agents' held acts to the Seats named.
func withGoogleAnalytics(in *Input, approvers ...string) {
	withGoogleCloud(in)
	in.Integrations = append(in.Integrations, v1alpha1.Integration{
		Namespace: testNamespace,
		Name:      caseGoogleAnalytics,
		Spec: v1alpha1.IntegrationSpec{
			CompanyRef: v1alpha1.CompanyReference{Name: sampleCompany},
			GoogleAnalytics: &v1alpha1.GoogleAnalyticsIntegration{
				Properties: []string{gaSampleProperty}, ApproverRefs: localRefs(approvers...),
			},
		},
	})
}

// googleAnalyticsApprovedBy writes into a sample company's document the
// Google Cloud and Google Analytics blocks the render writes for
// withGoogleAnalytics, with handle as the one approver, as the operator would
// write a reference it did not refuse.
func googleAnalyticsApprovedBy(handle string) func(*testing.T, map[string]any) {
	return func(t *testing.T, doc map[string]any) {
		t.Helper()
		in := companyCaseNamed(t, sampleCompany).input(t)
		withDatadog(in)
		withGoogleAnalytics(in, seatJane)
		rendered := renderTree(t, in)
		from, _ := rendered[keyIntegrations].(map[string]any)
		into, _ := doc[keyIntegrations].(map[string]any)
		for _, vendor := range []string{VendorGoogleCloud, VendorGoogleAnalytics} {
			block, ok := from[vendor].(map[string]any)
			if !ok {
				t.Fatalf("the render writes no %s block", vendor)
			}
			into[vendor] = block
		}
		into[VendorGoogleAnalytics].(map[string]any)[keyApprovers] = []any{handle}
	}
}

// renderTree is the document in renders, decoded.
func renderTree(t *testing.T, in *Input) map[string]any {
	t.Helper()
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	return tree
}

// TestGoogleAnalyticsRendersItsTwoBlocksAlone holds the Google Analytics
// Integration and a Seat's googleAnalytics to the two blocks they map onto:
// with both added, the sample company's document is the one it was with
// neither, plus integrations.google_analytics and the seat's own
// google_analytics block, each as Crewlet writes it.
func TestGoogleAnalyticsRendersItsTwoBlocksAlone(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGoogleCloud(in)
	before := renderTree(t, in)

	in = companyCaseNamed(t, sampleCompany).input(t)
	withGoogleAnalytics(in, seatJane)
	seat(in, seatAna).Spec.Integrations.GoogleAnalytics = v1alpha1.GoogleAnalyticsSeatIntegration{
		Enabled: new(true), Level: v1alpha1.GoogleAnalyticsLevelOperate, Properties: []string{gaSampleProperty},
	}
	after := renderTree(t, in)

	integrations, _ := after[keyIntegrations].(map[string]any)
	if got, want := integrations[VendorGoogleAnalytics], map[string]any{
		keyEnabled: true, keyProperties: []any{gaSampleProperty}, keyApprovers: []any{seatJane},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.google_analytics is %v, want %v", got, want)
	}
	delete(integrations, VendorGoogleAnalytics)

	role := roleIn(after, seatAna)
	seatBlocks, _ := role[keyIntegrations].(map[string]any)
	if got, want := seatBlocks[VendorGoogleAnalytics], map[string]any{
		keyEnabled: true, keyLevel: levelOperate, keyProperties: []any{gaSampleProperty},
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("the seat's google_analytics is %v, want %v", got, want)
	}
	delete(seatBlocks, VendorGoogleAnalytics)
	if len(seatBlocks) == 0 {
		delete(role, keyIntegrations)
	}

	if !reflect.DeepEqual(after, before) {
		got, want := renderYAML(t, after), renderYAML(t, before)
		t.Errorf("past its two blocks the document changed:\n%s\nwas\n%s", got, want)
	}
}

// renderYAML is tree as YAML, for a failure's message.
func renderYAML(t *testing.T, tree map[string]any) string {
	t.Helper()
	out, err := yaml.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
