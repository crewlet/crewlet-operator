package render

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// testTokens are the API tokens every bootstrap case is rendered with, and
// testObjects the S3 keys of the cases whose class names none of its own.
// Each token is 43 characters, the length of the tokens the operator
// generates: Crewlet refuses a token under 26 (secrets.CheckOperatorToken).
var (
	testTokens = APITokens{
		Operator: "test-operator-token-0123456789abcdefghijklm",
		Access:   "test-access-token-0123456789abcdefghijklmno",
	}
	testObjects = S3Credentials{AccessKeyID: "test-access-key-id", SecretAccessKey: "test-secret-access-key"}
)

// The keyring of the rotation case: rotatedKey seals, and staleKey, an id
// YAML would read as a date unless it is quoted, still opens what it sealed.
const (
	rotatedKey = "2026-10.b"
	staleKey   = "2026-01-01"
)

// otherKeyMaterial is a second key's material, so the rotation case's keys
// differ.
const otherKeyMaterial = "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="

type bootstrapCase struct {
	name string
	// from names the input the case is rendered from, when it is not the
	// case's own: a case that differs only in the addresses below.
	from    string
	keyring Keyring
	objects *S3Credentials
	// external, public and origins are the addresses the engine is
	// reached at, as the controller derives them: unset, the port-forward
	// address of a class that routes no API, and the public listener's
	// address inside the cluster.
	external, public string
	origins          []string
	// advisories are the advisories Crewlet's validator gives the Tier A,
	// about a choice of the platform's that the render carries through.
	advisories []advisory
}

// advisoryNoTrustedProxy is Crewlet's advisory on an https api.external_url
// with no api.trusted_proxies: something in front of the engine terminates
// TLS, and unless it is named every caller shows its address. Only the
// platform knows its proxies' addresses, and no field of the operator's
// offers them, so a routed engine reached over https draws it.
var advisoryNoTrustedProxy = advisory{path: "api.trusted_proxies", says: "trusts no proxy"}

// caseNATS is the bootstrap case of the defaults: a class whose object store
// is the company's own event stream, and a Company under Enforce. The cases
// that differ from it only in where the engine is reached are rendered from
// its input.
const caseNATS = "nats"

func bootstrapCases() []bootstrapCase {
	return []bootstrapCase{
		{name: caseNATS, keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
		{
			name:    "s3",
			keyring: Keyring{ActiveKeyID: rotatedKey, Keys: map[string]string{rotatedKey: testKeyMaterial, staleKey: otherKeyMaterial}},
			objects: &testObjects,
		},
		{name: "s3-workload-identity", keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
		// The nats case's company leaves managementPolicy to its default,
		// Enforce; these two set the others.
		{name: "observe", keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
		{name: "seed", keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
		// A class that allows seat pools: the data pod opens its leaf
		// listener.
		{name: caseSeatPools, keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
		// A class that routes the API and the webhooks over HTTPS.
		{
			name: "https-route", from: caseNATS, keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}},
			external: "https://nimbus.api.example.com", public: "https://nimbus.hooks.example.com",
			advisories: []advisory{advisoryNoTrustedProxy},
		},
		// A Company that states where people reach the dashboard, ahead of
		// the class's API route, which stays an origin it is reached on.
		{
			name: "route-origin", from: caseNATS, keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}},
			external: "https://crewlet.sso.example.com/nimbus", origins: []string{"https://nimbus.api.example.com"},
			advisories: []advisory{advisoryNoTrustedProxy},
		},
	}
}

func (c *bootstrapCase) input(t *testing.T) *BootstrapInput {
	t.Helper()
	o := load(t, filepath.Join("testdata", "bootstrap", cmp.Or(c.from, c.name)+".input.yaml"))
	in := reachedAtDefaults(&BootstrapInput{
		Company: o.company, Class: o.class, Keyring: c.keyring, Tokens: testTokens, Objects: c.objects,
		VolumeSize: *o.class.Spec.Data.Storage.Size, AllowedOrigins: c.origins,
	})
	in.ExternalURL = cmp.Or(c.external, in.ExternalURL)
	in.PublicURL = cmp.Or(c.public, in.PublicURL)
	return in
}

// reachedAtDefaults gives in the addresses the controller gives the data pod
// of a Company that states none and whose class routes nothing: where
// kubectl port-forward serves the API, and the public listener inside the
// cluster.
func reachedAtDefaults(in *BootstrapInput) *BootstrapInput {
	in.ExternalURL = fmt.Sprintf("http://localhost:%d", APIPort)
	in.PublicURL = fmt.Sprintf("http://%s-engine.%s.svc:%d", in.Company.Name, in.Company.Namespace, PublicPort)
	return in
}

// TestBootstrapGolden pins each Tier A and the environment it needs.
func TestBootstrapGolden(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "bootstrap", c.name+".yaml"), doc.YAML)
			env, err := yaml.Marshal(doc.Env)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "bootstrap", c.name+".env.yaml"), env)
		})
	}
}

var envPointer = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// TestBootstrapEnvIsExactlyTheReferences holds Env to the document: the pod
// is given every variable the document refers to, so each reference
// resolves, and nothing else, so nothing reaches the engine's environment
// that its configuration does not name.
func TestBootstrapEnvIsExactlyTheReferences(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			var referenced []string
			for _, m := range envPointer.FindAllStringSubmatch(string(doc.YAML), -1) {
				referenced = append(referenced, m[1])
			}
			slices.Sort(referenced)
			referenced = slices.Compact(referenced)
			given := make([]string, 0, len(doc.Env))
			for _, e := range doc.Env {
				given = append(given, e.Name)
			}
			if !slices.Equal(referenced, given) {
				t.Errorf("the document refers to %v, the pod is given %v", referenced, given)
			}
		})
	}
}

// TestBootstrapKeepsEveryCredentialOutOfTheEnvironment: the engine resolves
// a ${NAME} in its company document from its environment, so a credential
// in the environment is one a writer of that document can have the engine
// send anywhere. Every credential is in the document, which the operator
// keeps in a Secret, and the environment holds the pod's name alone.
func TestBootstrapKeepsEveryCredentialOutOfTheEnvironment(t *testing.T) {
	c := bootstrapCases()[1]
	doc, err := Bootstrap(c.input(t))
	if err != nil {
		t.Fatal(err)
	}
	credentials := slices.Concat(
		[]string{testTokens.Operator, testTokens.Access, testObjects.AccessKeyID, testObjects.SecretAccessKey},
		slices.Collect(maps.Values(c.keyring.Keys)))
	for _, credential := range credentials {
		if !strings.Contains(string(doc.YAML), credential) {
			t.Errorf("the document does not hold the credential %q", credential)
		}
	}
	for _, e := range doc.Env {
		if e.Name != EnvNodeID || e.Value != "" || e.ValueFrom == nil || e.ValueFrom.FieldRef == nil ||
			e.ValueFrom.FieldRef.FieldPath != "metadata.name" {
			t.Errorf("the pod's environment holds %+v; it must hold the pod's name alone", e)
		}
	}
}

// TestBootstrapRefusesAKeyringItCannotReference covers the keyring Secret's
// ids and material, which come from a Secret anyone allowed to edit it can
// change.
func TestBootstrapRefusesAKeyringItCannotReference(t *testing.T) {
	for _, c := range []struct {
		name    string
		keyring Keyring
		want    string
	}{
		{"no keys", Keyring{}, "holds no key"},
		{"active key missing", Keyring{ActiveKeyID: "k2", Keys: map[string]string{"k1": testKeyMaterial}}, `no key "k2"`},
		{"not a key id", Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial, "k:2": testKeyMaterial}}, `"k:2"`},
		{"not base64", Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": "material"}}, `"k1" is not base64 of 32 bytes`},
		{"a short key", Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": "c2hvcnQ="}}, `"k1" is not base64 of 32 bytes`},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.Keyring = c.keyring
			_, err := Bootstrap(in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one naming %s", err, c.want)
			}
		})
	}
}

// TestBootstrapPlacesTheDataPodInTheDataPool ties the two documents
// together: every agent seat is pinned to the data pool, so the data pod
// must carry that label or no seat would ever run.
func TestBootstrapPlacesTheDataPodInTheDataPool(t *testing.T) {
	doc, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := flatten(t, doc.YAML)["node.labels."+PoolLabel]; got != v1alpha1.DataPool {
		t.Fatalf("node.labels[%s] = %v, want %s", PoolLabel, got, v1alpha1.DataPool)
	}
}

// TestBootstrapServesOutsidePartiesOnTheirOwnListener pins the data pod's
// two listeners: the admin API on APIPort and the routes outside parties
// call on PublicPort, opened on every data pod whatever its class routes,
// so that a class's exposure never decides which routes the admin port
// serves; and a seat pod, which serves no public route, opens none.
func TestBootstrapServesOutsidePartiesOnTheirOwnListener(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			leaves := flatten(t, doc.YAML)
			if got := leaves["api.port"]; fmt.Sprint(got) != fmt.Sprint(APIPort) {
				t.Errorf("api.port = %v, want %d", got, APIPort)
			}
			if got := leaves["api.public.port"]; fmt.Sprint(got) != fmt.Sprint(PublicPort) {
				t.Errorf("api.public.port = %v, want %d: the webhooks would share the admin API's listener", got, PublicPort)
			}
		})
	}
	for _, c := range seatPodCases() {
		t.Run("seat pod "+c.name, func(t *testing.T) {
			in, _ := c.input(t)
			doc, err := SeatPodBootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			for key := range flatten(t, doc.YAML) {
				if strings.HasPrefix(key, "api.public") {
					t.Errorf("a seat pod's Tier A sets %s: it serves no public route", key)
				}
			}
		})
	}
}

// TestBootstrapNamesTheOperatorTheOnlyWriterUnderEnforce pins
// api.auth.company_writers per management policy: under Enforce the
// operator's own token alone may change the company document, so the engine
// refuses an edit the operator would overwrite; under Observe and Seed the
// engine's own users own the document, and no list stands in their way.
func TestBootstrapNamesTheOperatorTheOnlyWriterUnderEnforce(t *testing.T) {
	for _, c := range []struct {
		policy v1alpha1.ManagementPolicy
		want   []string
	}{
		{policy: "", want: []string{TokenOperator}},
		{policy: v1alpha1.ManagementPolicyEnforce, want: []string{TokenOperator}},
		{policy: v1alpha1.ManagementPolicyObserve},
		{policy: v1alpha1.ManagementPolicySeed},
	} {
		t.Run(cmp.Or(string(c.policy), "unset"), func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.Company.Spec.Config.ManagementPolicy = c.policy
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				API struct {
					Auth map[string]any `json:"auth"`
				} `json:"api"`
			}
			if err := yaml.Unmarshal(doc.YAML, &got); err != nil {
				t.Fatal(err)
			}
			writers, present := got.API.Auth["company_writers"]
			if c.want == nil {
				if present {
					t.Fatalf("api.auth.company_writers = %v under %q, want it absent", writers, c.policy)
				}
				return
			}
			if fmt.Sprint(writers) != fmt.Sprint(c.want) {
				t.Fatalf("api.auth.company_writers = %v under %q, want %v", writers, c.policy, c.want)
			}
		})
	}
}

// TestBootstrapServesAdminToBothTokens pins the data pod's authority: it
// serves admin, and each token holds exactly admin. The operator's own calls
// are all admin's in Crewlet's authority table, so a narrower grant is an
// operator the engine refuses its own document; the published token holds
// what the operator does. A grant the engine does not need would widen what
// a leaked token opens, and a posture of refuse with an admin token is one
// the engine refuses to start on.
func TestBootstrapServesAdminToBothTokens(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				API struct {
					Auth struct {
						Admin  string `json:"admin"`
						Tokens []struct {
							ID     string   `json:"id"`
							Grants []string `json:"grants"`
						} `json:"tokens"`
					} `json:"auth"`
				} `json:"api"`
			}
			if err := yaml.Unmarshal(doc.YAML, &got); err != nil {
				t.Fatal(err)
			}
			if got.API.Auth.Admin != "serve" {
				t.Errorf("api.auth.admin = %q, want serve", got.API.Auth.Admin)
			}
			ids := make([]string, 0, len(got.API.Auth.Tokens))
			for _, token := range got.API.Auth.Tokens {
				ids = append(ids, token.ID)
				if !slices.Equal(token.Grants, []string{"admin"}) {
					t.Errorf("token %s holds %v, want [admin]", token.ID, token.Grants)
				}
			}
			if !slices.Equal(ids, []string{TokenOperator, TokenAccess}) {
				t.Errorf("api.auth.tokens are %v, want [%s %s]", ids, TokenOperator, TokenAccess)
			}
		})
	}
}

// TestBootstrapNeedsTheEnginesAddresses: Crewlet refuses to start a node
// that serves the API without api.external_url, or that opens its public
// listener without api.public.external_url, so the render refuses a Tier A
// without either rather than a pod that crash-loops.
func TestBootstrapNeedsTheEnginesAddresses(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*BootstrapInput)
	}{
		{"no external address", func(in *BootstrapInput) { in.ExternalURL = "" }},
		{"no public address", func(in *BootstrapInput) { in.PublicURL = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			c.mutate(in)
			if _, err := Bootstrap(in); !errors.Is(err, ErrNoAddress) {
				t.Fatalf("Bootstrap() = %v, want ErrNoAddress", err)
			}
		})
	}
}

// TestBootstrapBoundsTheStreamByTheVolume pins the event stream's share of
// the volume: three quarters of the claim the pod runs on, whatever the
// class says now, since the claim is what the pod was given.
func TestBootstrapBoundsTheStreamByTheVolume(t *testing.T) {
	in := bootstrapCases()[0].input(t)
	in.VolumeSize = resource.MustParse("8Gi")
	doc, err := Bootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stream struct {
			StoreMaxBytes int64 `json:"store_max_bytes"`
		} `json:"stream"`
	}
	if err := yaml.Unmarshal(doc.YAML, &got); err != nil {
		t.Fatal(err)
	}
	if want := int64(6 << 30); got.Stream.StoreMaxBytes != want {
		t.Fatalf("stream.store_max_bytes = %d for an 8Gi volume, want %d", got.Stream.StoreMaxBytes, want)
	}
}

// TestDataPodsKeepTheirLeafListenerForPinnedSeats holds the data pods' leaf
// listener open while the revision the engine runs pins a seat to a seat pod,
// whatever the class says now: the engine keeps running that revision while
// the next one is refused for its pools, and closing the listener would leave
// the seats it pins held by nobody.
func TestDataPodsKeepTheirLeafListenerForPinnedSeats(t *testing.T) {
	for _, c := range []struct {
		name    string
		policy  v1alpha1.SeatPoolPolicy
		pinned  v1alpha1.RevisionPlacement
		want    bool
		because string
	}{
		{name: "pools allowed", policy: v1alpha1.SeatPoolsAllowed, want: true, because: "the class allows seat pools"},
		{name: "pools forbidden", policy: v1alpha1.SeatPoolsForbidden, because: "nothing joins it"},
		{
			name: "pools forbidden, every seat on the data pods", policy: v1alpha1.SeatPoolsForbidden,
			pinned: v1alpha1.RevisionPlacement{Pools: []string{v1alpha1.DataPool}}, because: "no seat runs on a seat pod",
		},
		{
			name: "pools forbidden, a seat in a Shared pool", policy: v1alpha1.SeatPoolsForbidden,
			pinned: v1alpha1.RevisionPlacement{Pools: []string{v1alpha1.DataPool, "general"}}, want: true,
			because: "the running revision pins a seat to a Shared pool's pods",
		},
		{
			name: "pools forbidden, a seat of its own", policy: v1alpha1.SeatPoolsForbidden,
			pinned: v1alpha1.RevisionPlacement{Seats: []string{"cy"}}, want: true,
			because: "the running revision pins a seat to its own pods",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.Class.Spec.SeatPools = v1alpha1.SeatPoolSettings{Policy: c.policy}
			in.Company.Status.Config.Placement = c.pinned
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			var tierA struct {
				Stream struct {
					Leaf *struct {
						Port int `json:"port"`
					} `json:"leaf"`
				} `json:"stream"`
			}
			if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
				t.Fatal(err)
			}
			if got := tierA.Stream.Leaf != nil && tierA.Stream.Leaf.Port == LeafPort; got != c.want {
				t.Errorf("the data pods open the leaf listener: %v, want %v: %s", got, c.want, c.because)
			}
			if got := LeafOpen(in.Company, in.Class); got != c.want {
				t.Errorf("LeafOpen = %v, want %v: %s", got, c.want, c.because)
			}
		})
	}
}
