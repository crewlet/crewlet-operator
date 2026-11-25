package render

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// caseGoogleSignIn is the company case whose Google products sign in as the
// Google Cloud Integration's signInClient, the names of its Integrations, and
// the client's ID; its secret is in adMobOAuthName's Secret.
const (
	caseGoogleSignIn  = "google-sign-in"
	signInGoogleCloud = "google-cloud"
	signInAdSense     = "adsense"
	signInClientID    = "123456789012-harbor.apps.googleusercontent.com"
)

// TestTheSignInClientIsRenderedBesideTheProvisioning holds the Google Cloud
// Integration's signInClient to google_cloud.sign_in_client, its secret
// written as the store name of its Secret key, and an AdMob Integration that
// names no client to an admob block with no oauth, which signs in as it.
func TestTheSignInClientIsRenderedBesideTheProvisioning(t *testing.T) {
	in := companyCaseNamed(t, caseGoogleSignIn).input(t)
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(doc.Refs, func(k secretref.Key) bool {
		return k.Name == adMobOAuthName && k.Key == adMobOAuthKey
	})
	if i < 0 {
		t.Fatalf("the document's Refs %v do not carry the sign-in client's secret", doc.Refs)
	}
	integrations, _ := renderTree(t, in)[keyIntegrations].(map[string]any)
	cloud, _ := integrations[VendorGoogleCloud].(map[string]any)
	if got, want := cloud["sign_in_client"], map[string]any{
		keyOAuthClientID: signInClientID, keyOAuthClientSecret: "${" + secretsync.StoreName(doc.Refs[i]) + "}",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("integrations.google_cloud.sign_in_client is %v, want %v", got, want)
	}
	adMob, _ := integrations[VendorAdMob].(map[string]any)
	if oauth, ok := adMob["oauth"]; ok {
		t.Errorf("integrations.admob names oauth %v, want none: it signs in as the sign-in client", oauth)
	}
}

// TestContractAGoogleProductSignsInAsTheSignInClient has the engine's dry run
// admit AdMob and the Google Analytics admin on the Google Cloud
// Integration's signInClient alone, and AdSense on its own client beside it,
// and refuse a product with neither, each problem on that product's
// Integration, where the operator reports it Invalid.
func TestContractAGoogleProductSignsInAsTheSignInClient(t *testing.T) {
	client := enginetest.Start(t).Client()
	for _, c := range []struct {
		name    string
		mutate  func(*Input)
		refused string
	}{
		{name: "AdMob on the sign-in client, AdSense on its own", mutate: func(*Input) {}},
		{
			name: "AdMob with neither client",
			mutate: func(in *Input) {
				integration(in, signInGoogleCloud).Spec.GoogleCloud.SignInClient = v1alpha1.GoogleOAuthClient{}
			},
			refused: caseAdMob,
		},
		{
			name: "AdSense with neither client",
			mutate: func(in *Input) {
				cloud := &integration(in, signInGoogleCloud).Spec.GoogleCloud
				integration(in, caseAdMob).Spec.AdMob.OAuth = cloud.SignInClient
				cloud.SignInClient = v1alpha1.GoogleOAuthClient{}
				integration(in, signInAdSense).Spec.AdSense.OAuth = v1alpha1.GoogleOAuthClient{}
			},
			refused: signInAdSense,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, caseGoogleSignIn).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
			if c.refused == "" {
				if err != nil {
					t.Fatalf("the dry run refused the document: %v", err)
				}
				return
			}
			verr, ok := errors.AsType[*engine.ValidationError](err)
			if !ok || len(verr.Problems) == 0 {
				t.Fatalf("the dry run answered %v, want a refusal", err)
			}
			want := ObjectRef{Kind: KindIntegration, Name: c.refused}
			for _, p := range verr.Problems {
				if got, ok := doc.Attribute(PathOf(p.Segments)); !ok || got != want {
					t.Errorf("the engine's problem at %q (%s) is attributed to %v, %v; want %v",
						p.Path, p.Message, got, ok, want)
				}
			}
		})
	}
}
