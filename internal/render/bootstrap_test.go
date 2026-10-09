package render

import (
	"cmp"
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
var (
	testTokens  = APITokens{Operator: "test-operator-token", Access: "test-access-token"}
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
	name    string
	keyring Keyring
	objects *S3Credentials
}

func bootstrapCases() []bootstrapCase {
	return []bootstrapCase{
		{name: "nats", keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
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
		{name: "seat-pools", keyring: Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}},
	}
}

func (c bootstrapCase) input(t *testing.T) *BootstrapInput {
	t.Helper()
	o := load(t, filepath.Join("testdata", "bootstrap", c.name+".input.yaml"))
	return &BootstrapInput{
		Company: o.company, Class: o.class, Keyring: c.keyring, Tokens: testTokens, Objects: c.objects,
		VolumeSize: *o.class.Spec.Data.Storage.Size,
	}
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
