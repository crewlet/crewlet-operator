package render

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// testSecrets are the operator-owned Secrets every bootstrap case reads.
var testSecrets = BootstrapSecrets{Keyring: "nimbus-keyring", APITokens: "nimbus-api-tokens", Objects: "nimbus-objects"}

// The keyring of the rotation case: rotatedKey seals, and staleKey, an id
// YAML would read as a date unless it is quoted, still opens what it sealed.
const (
	rotatedKey = "2026-10.b"
	staleKey   = "2026-01-01"
)

type bootstrapCase struct {
	name    string
	keyring Keyring
}

func bootstrapCases() []bootstrapCase {
	return []bootstrapCase{
		{name: "nats", keyring: Keyring{ActiveKeyID: "k1", KeyIDs: []string{"k1"}}},
		{name: "s3", keyring: Keyring{ActiveKeyID: rotatedKey, KeyIDs: []string{rotatedKey, staleKey}}},
		{name: "s3-workload-identity", keyring: Keyring{ActiveKeyID: "k1", KeyIDs: []string{"k1"}}},
	}
}

func (c bootstrapCase) input(t *testing.T) *BootstrapInput {
	t.Helper()
	o := load(t, filepath.Join("testdata", "bootstrap", c.name+".input.yaml"))
	return &BootstrapInput{
		Company: o.company, Class: o.class, Keyring: c.keyring, Secrets: testSecrets,
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
				if e.Value != "" || e.ValueFrom == nil {
					t.Errorf("%s has a literal value: every variable must come from a Secret or the pod", e.Name)
				}
			}
			if !slices.Equal(referenced, given) {
				t.Errorf("the document refers to %v, the pod is given %v", referenced, given)
			}
		})
	}
}

// TestBootstrapEnvReadsTheOperatorSecrets pins where each credential comes
// from: the runtime reconciler creates these Secrets, under these keys.
func TestBootstrapEnvReadsTheOperatorSecrets(t *testing.T) {
	doc, err := Bootstrap(bootstrapCases()[1].input(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"CREWLET_SECRET_KEY_2026_01_01": {testSecrets.Keyring, staleKey},
		"CREWLET_SECRET_KEY_2026_10_B":  {testSecrets.Keyring, rotatedKey},
		EnvTokenOperator:                {testSecrets.APITokens, TokenOperator},
		EnvTokenAccess:                  {testSecrets.APITokens, TokenAccess},
		EnvS3AccessKeyID:                {testSecrets.Objects, "accessKeyID"},
		EnvS3SecretAccessKey:            {testSecrets.Objects, "secretAccessKey"},
	}
	for _, e := range doc.Env {
		if e.Name == EnvNodeID {
			if e.ValueFrom.FieldRef == nil || e.ValueFrom.FieldRef.FieldPath != "metadata.name" {
				t.Errorf("%s = %+v, want the pod's name", e.Name, e.ValueFrom)
			}
			continue
		}
		ref := e.ValueFrom.SecretKeyRef
		if ref == nil || [2]string{ref.Name, ref.Key} != want[e.Name] {
			t.Errorf("%s reads %+v, want Secret %v", e.Name, ref, want[e.Name])
		}
		delete(want, e.Name)
	}
	if len(want) > 0 {
		t.Errorf("the pod is not given %v", want)
	}
}

// TestBootstrapRefusesAKeyringItCannotReference covers the keyring Secret's
// ids, which come from a Secret anyone allowed to edit it can change.
func TestBootstrapRefusesAKeyringItCannotReference(t *testing.T) {
	for _, c := range []struct {
		name    string
		keyring Keyring
		want    string
	}{
		{"no keys", Keyring{}, "holds no key"},
		{"active key missing", Keyring{ActiveKeyID: "k2", KeyIDs: []string{"k1"}}, `no key "k2"`},
		{"not a key id", Keyring{ActiveKeyID: "k1", KeyIDs: []string{"k1", "k:2"}}, `"k:2"`},
		{"one variable for two keys", Keyring{ActiveKeyID: "k-1", KeyIDs: []string{"k-1", "k.1"}}, "CREWLET_SECRET_KEY_K_1"},
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
	if got := flatten(t, doc.YAML)["node.labels."+PoolLabel]; got != DataPool {
		t.Fatalf("node.labels[%s] = %v, want %s", PoolLabel, got, DataPool)
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
