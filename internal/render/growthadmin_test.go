package render

import (
	"reflect"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// TestGooglePlayAndAnalyticsRenderTheirAdminClientWhereNamed holds each
// Integration's optional OAuth client, which an admin signs in as to add
// agents' identities, to its oauth block: absent where unset, and where set
// the client ID and its secret as one store variable both blocks share, the
// Secret key pushed once.
func TestGooglePlayAndAnalyticsRenderTheirAdminClientWhereNamed(t *testing.T) {
	in := companyCaseNamed(t, sampleCompany).input(t)
	withGooglePlay(in)
	withGoogleAnalytics(in)
	// BOTH CONNECT GOOGLE CLOUD, which a company connects once.
	seen := map[string]bool{}
	in.Integrations = slices.DeleteFunc(in.Integrations, func(i v1alpha1.Integration) bool {
		dup := seen[i.Name]
		seen[i.Name] = true
		return dup
	})
	integrations, _ := renderTree(t, in)[keyIntegrations].(map[string]any)
	for _, vendor := range []string{VendorGooglePlay, VendorGoogleAnalytics} {
		if _, named := integrations[vendor].(map[string]any)["oauth"]; named {
			t.Errorf("%s names an OAuth client the Integration does not", vendor)
		}
	}

	client := v1alpha1.GoogleOAuthClient{ClientID: adMobClientID,
		ClientSecretRef: v1alpha1.SecretKeyReference{Name: adMobOAuthName, Key: adMobOAuthKey}}
	for i := range in.Integrations {
		spec := &in.Integrations[i].Spec
		if spec.GooglePlay != nil {
			spec.GooglePlay.OAuth = client
		}
		if spec.GoogleAnalytics != nil {
			spec.GoogleAnalytics.OAuth = client
		}
	}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var refs []secretref.Key
	for _, k := range doc.Refs {
		if k.Name == adMobOAuthName && k.Key == adMobOAuthKey {
			refs = append(refs, k)
		}
	}
	if len(refs) != 1 {
		t.Fatalf("the client's secret is %d of the document's Refs, want one: %v", len(refs), doc.Refs)
	}
	want := map[string]any{keyOAuthClientID: adMobClientID,
		keyOAuthClientSecret: "${" + secretsync.StoreName(refs[0]) + "}"}
	integrations, _ = renderTree(t, in)[keyIntegrations].(map[string]any)
	for _, vendor := range []string{VendorGooglePlay, VendorGoogleAnalytics} {
		if got := integrations[vendor].(map[string]any)["oauth"]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s's oauth is %v, want %v", vendor, got, want)
		}
	}
}
