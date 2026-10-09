package secretsync

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

var stateRef = types.NamespacedName{Namespace: nimbusNS, Name: "nimbus-secret-sync"}

func newStateClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := crewletaiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func company() *crewletaiv1alpha1.Company {
	c := &crewletaiv1alpha1.Company{}
	c.Namespace, c.Name, c.UID = nimbusNS, nimbusNS, "6f1c1a5e-0000-4000-8000-000000000001"
	return c
}

// controllerRef is a controller reference to the Company nimbus of the UID
// given.
func controllerRef(uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: crewletaiv1alpha1.GroupVersion.String(), Kind: "Company", Name: nimbusNS, UID: uid, Controller: new(true),
	}
}

func stateSecret(t *testing.T, c client.Client) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	if err := c.Get(context.Background(), stateRef, s); err != nil {
		t.Fatalf("reading the state Secret: %v", err)
	}
	return s
}

// TestLoadStateWithoutASecretIsEmpty: a company's first sync has no state,
// and must push every name.
func TestLoadStateWithoutASecretIsEmpty(t *testing.T) {
	state, err := LoadState(context.Background(), newStateClient(t), stateRef, company())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	eng := newFakeEngine()
	eng.entries[nameA] = fakeEntry{"a1", Source}
	_, res := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), state)
	if !slices.Equal(res.Put, []string{nameA}) {
		t.Fatalf("Put = %v: an empty state must not vouch for any entry", res.Put)
	}
}

// TestSaveStateRoundTrips: what SaveState writes, LoadState reads back, and
// a sync from it is a no-op. The Secret is the Company's, labelled as the
// operator's, so it goes when the Company does.
func TestSaveStateRoundTrips(t *testing.T) {
	ctx := context.Background()
	c := newStateClient(t)
	owner := company()
	eng := newFakeEngine()
	desired := desiredOf(map[string]string{nameA: "a1", nameB: "b1"})
	state, _ := mustSync(t, eng, desired, State{})

	if err := SaveState(ctx, c, stateRef, owner, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	loaded, err := LoadState(ctx, c, stateRef, owner)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !bytes.Equal(loaded.key, state.key) || !maps.EqualFunc(loaded.fingerprints, state.fingerprints, bytes.Equal) {
		t.Fatalf("LoadState read %v back, want %v", loaded, state)
	}
	eng.takeCalls()
	mustSync(t, eng, desired, loaded)
	assertCalls(t, eng, "list")

	secret := stateSecret(t, c)
	controller := metav1.GetControllerOf(secret)
	if controller == nil || controller.UID != owner.UID || controller.Kind != "Company" ||
		controller.APIVersion != crewletaiv1alpha1.GroupVersion.String() || !*controller.BlockOwnerDeletion {
		t.Errorf("owner = %+v, want the Company as controller", secret.OwnerReferences)
	}
	if secret.Labels["app.kubernetes.io/managed-by"] != "crewlet-operator" ||
		secret.Labels["app.kubernetes.io/instance"] != owner.Name {
		t.Errorf("labels = %v", secret.Labels)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Errorf("type = %q", secret.Type)
	}
}

// TestSaveStateForgetsWhatTheStateDropped: an entry the state no longer has
// must leave the Secret too, or a later load would vouch for a name the
// store no longer holds.
func TestSaveStateForgetsWhatTheStateDropped(t *testing.T) {
	ctx := context.Background()
	c := newStateClient(t)
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), State{})
	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	state, _ = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), state)
	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got := slices.Sorted(maps.Keys(stateSecret(t, c).Data))
	if want := []string{nameA, fingerprintKeyField}; !slices.Equal(got, want) {
		t.Fatalf("state Secret keys = %v, want %v", got, want)
	}
}

// TestLoadStateStartsOverFromAnEditedSecret: a fingerprint is only as good
// as its key and its shape. Without a well-formed key nothing can be
// verified, so the state is empty; an entry that is not a fingerprint of an
// owned name is dropped. Either way the cost is a push, never a skipped one.
func TestLoadStateStartsOverFromAnEditedSecret(t *testing.T) {
	ctx := context.Background()
	fp := bytes.Repeat([]byte{1}, sha256.Size)
	edited := func(data map[string][]byte) *corev1.Secret {
		s := &corev1.Secret{Data: data}
		s.Namespace, s.Name = stateRef.Namespace, stateRef.Name
		s.OwnerReferences = []metav1.OwnerReference{controllerRef(company().UID)}
		return s
	}

	for name, key := range map[string][]byte{"missing": nil, "short": []byte("k"), "long": bytes.Repeat([]byte{1}, 64)} {
		t.Run("key "+name, func(t *testing.T) {
			data := map[string][]byte{nameA: fp}
			if key != nil {
				data[fingerprintKeyField] = key
			}
			state, err := LoadState(ctx, newStateClient(t, edited(data)), stateRef, company())
			if err != nil {
				t.Fatalf("LoadState: %v", err)
			}
			if len(state.key) != 0 || len(state.fingerprints) != 0 {
				t.Fatalf("LoadState() = %v, want the empty state", state)
			}
		})
	}

	t.Run("entries", func(t *testing.T) {
		data := map[string][]byte{
			fingerprintKeyField: bytes.Repeat([]byte{2}, fingerprintKeyLen),
			nameA:               fp,
			nameB:               fp[:10],
			foreignName:         fp,
		}
		state, err := LoadState(ctx, newStateClient(t, edited(data)), stateRef, company())
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if got := slices.Sorted(maps.Keys(state.fingerprints)); !slices.Equal(got, []string{nameA}) {
			t.Fatalf("LoadState kept %v, want only the well-formed %s", got, nameA)
		}
	})
}

// TestFingerprintsRevealNothing: the state Secret may be read by anyone who
// can read Secrets in the namespace, and copied anywhere. It must not hold a
// value, a plain hash of one (which confirms a guess), or the same
// fingerprint for two names holding the same value; and two companies'
// fingerprints of one value must differ, because each has its own key.
func TestFingerprintsRevealNothing(t *testing.T) {
	ctx := context.Background()
	const value = "xoxb-1234567890-shared-by-two-names"
	desired := desiredOf(map[string]string{nameA: value, nameB: value})

	c := newStateClient(t)
	state, _ := mustSync(t, newFakeEngine(), desired, State{})
	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	plain := sha256.Sum256([]byte(value))
	for name, stored := range stateSecret(t, c).Data {
		if bytes.Contains(stored, []byte(value)) || bytes.Equal(stored, plain[:]) {
			t.Errorf("state entry %s holds the value or its plain hash", name)
		}
	}

	if bytes.Equal(state.fingerprints[nameA], state.fingerprints[nameB]) {
		t.Error("two names holding one value have the same fingerprint")
	}

	other, _ := mustSync(t, newFakeEngine(), desired, State{})
	if bytes.Equal(other.key, state.key) {
		t.Fatal("two states share a fingerprint key")
	}
	if bytes.Equal(other.fingerprints[nameA], state.fingerprints[nameA]) {
		t.Error("one value has the same fingerprint under two keys")
	}

	// Pin the construction, so a change to it is a decision: an HMAC of the
	// name, a NUL and the value, under the state's key.
	mac := hmac.New(sha256.New, state.key)
	mac.Write([]byte(nameA + "\x00" + value))
	if !bytes.Equal(state.fingerprints[nameA], mac.Sum(nil)) {
		t.Error("the fingerprint is not HMAC-SHA256(key, name NUL value)")
	}

	// Exactly the names, under every verb, so the fingerprint key is in no
	// log line a State reaches.
	want := "[" + nameA + " " + nameB + "]"
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if got := fmt.Sprintf(verb, state); got != want {
			t.Errorf("Sprintf(%q, state) = %q, want %q", verb, got, want)
		}
	}
}

// TestDigestFollowsTheValues: the digest a caller stores beside an applied
// document must change when a value it references is rotated, and only then,
// or a rotation would never reach the engine, or every reconcile would apply.
func TestDigestFollowsTheValues(t *testing.T) {
	a, b := nameA, nameB
	eng := newFakeEngine()
	values := map[string]string{a: "first-a", b: "first-b"}
	first, _ := mustSync(t, eng, desiredOf(values), State{})
	names := []string{a, b}

	same, _ := mustSync(t, eng, desiredOf(values), first)
	if first.Digest(names) != same.Digest(names) {
		t.Error("the digest changed although no value did")
	}
	rotated, _ := mustSync(t, eng, desiredOf(map[string]string{a: "first-a", b: "second-b"}), first)
	if first.Digest(names) == rotated.Digest(names) {
		t.Error("the digest did not change when a value was rotated")
	}
	if first.Digest(names) == first.Digest([]string{a}) {
		t.Error("the digest of fewer names is the digest of all of them")
	}
}

// TestLoadStateRefusesASecretTheCompanyDoesNotControl: a Secret of the
// state's name that someone else created, or that an earlier Company of the
// name controlled, is neither read nor overwritten; the error names its
// controller so the caller can tell the two apart.
func TestLoadStateRefusesASecretTheCompanyDoesNotControl(t *testing.T) {
	for _, tc := range []struct {
		name       string
		owners     []metav1.OwnerReference
		controller bool
	}{
		{name: "a Secret with no controller"},
		{name: "an earlier Company's", owners: []metav1.OwnerReference{controllerRef("6f1c1a5e-0000-4000-8000-0000000000ff")}, controller: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &corev1.Secret{Data: map[string][]byte{fingerprintKeyField: bytes.Repeat([]byte{1}, fingerprintKeyLen)}}
			s.Namespace, s.Name, s.OwnerReferences = stateRef.Namespace, stateRef.Name, tc.owners
			_, err := LoadState(context.Background(), newStateClient(t, s), stateRef, company())
			notOwned, ok := errors.AsType[*NotOwnedError](err)
			if !ok {
				t.Fatalf("LoadState() error = %v, want a *NotOwnedError", err)
			}
			if (notOwned.Controller != nil) != tc.controller {
				t.Errorf("Controller = %+v", notOwned.Controller)
			}
		})
	}
}
