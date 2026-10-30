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
	"github.com/crewlet/crewlet-operator/internal/secretref"
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
	eng.entries[nameA] = fakeEntry{"a1", self}
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
	if loaded.key != state.key || !maps.EqualFunc(loaded.fingerprints, state.fingerprints, bytes.Equal) {
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

// TestSaveStateKeepsTheWithdrawnNames: a withdrawal survives a save and a
// load, so a revision that failed to activate after it is activated by a
// later reconcile, of this operator or one restarted; a line that names no
// entry the operator owns, which only a hand edit writes, is dropped.
func TestSaveStateKeepsTheWithdrawnNames(t *testing.T) {
	ctx := context.Background()
	c := newStateClient(t)
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), State{})
	held := Desired{absent: map[string]*MissingError{nameB: {Key: keyB}}}
	state, _, err := Withdraw(ctx, eng, held, state)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	secret := stateSecret(t, c)
	secret.Data[withdrawnField] = append(secret.Data[withdrawnField], []byte("not-a-store-name\n")...)
	if err := c.Update(ctx, secret); err != nil {
		t.Fatalf("editing the state: %v", err)
	}
	loaded, err := LoadState(ctx, c, stateRef, company())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !slices.Equal(loaded.Withdrawn(), []string{nameB}) {
		t.Fatalf("Withdrawn() = %v after a load, want [%s]", loaded.Withdrawn(), nameB)
	}

	if err := SaveState(ctx, c, stateRef, company(), loaded.Reactivated()); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if got := stateSecret(t, c).Data[withdrawnField]; len(got) != 0 {
		t.Errorf("the state Secret keeps the withdrawn names %q after the revision activated", got)
	}
}

// TestSaveStateKeepsTheWrittenNames: the names written since a revision
// last activated after them survive a save and a load, each with the epoch
// it waits past or none, so a write of the document that fails, or an
// operator that restarts, leaves them owed exactly as they were; a line that
// names no entry the operator owns, which only a hand edit writes, is
// dropped, one whose epoch is no number no lower than zero waits for a read
// as one no read followed does, and an activation empties the list.
func TestSaveStateKeepsTheWrittenNames(t *testing.T) {
	ctx := context.Background()
	c := newStateClient(t)
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), State{})
	state, _ = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), state.Observed(3))
	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	secret := stateSecret(t, c)
	if want := nameA + " 3\n" + nameB + "\n"; string(secret.Data[writtenField]) != want {
		t.Fatalf("written-names = %q, want %q", secret.Data[writtenField], want)
	}
	secret.Data[writtenField] = append(secret.Data[writtenField], []byte("not-a-store-name 4\n"+nameC+" -1\n")...)
	if err := c.Update(ctx, secret); err != nil {
		t.Fatalf("editing the state: %v", err)
	}
	loaded, err := LoadState(ctx, c, stateRef, company())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !slices.Equal(loaded.Written(), []string{nameA, nameB, nameC}) {
		t.Fatalf("Written() after a load = %v, want [%s %s %s]", loaded.Written(), nameA, nameB, nameC)
	}
	if got := loaded.Ran(4).Written(); !slices.Equal(got, []string{nameB, nameC}) {
		t.Fatalf("Written() after a load and an activation at epoch 4 = %v, want [%s %s]", got, nameB, nameC)
	}

	if err := SaveState(ctx, c, stateRef, company(), loaded.Activated(revision1, nil)); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if got := stateSecret(t, c).Data[writtenField]; len(got) != 0 {
		t.Errorf("the state Secret keeps the written names %q after a revision activated", got)
	}
}

// TestActivationIsRecordedForItsRevisionAlone: what Activated records of the
// operator's last activation answers for that revision and for no other, an
// empty one included, the Secret keys it lets go as well as their names,
// survives a save and a load and an activation of nothing new
// (Reactivated), and is replaced by the next activation; a line that names
// no entry the operator owns, or no Secret key, which only a hand edit
// writes, is dropped, and so are the record and the retained names once the
// State no longer holds them.
func TestActivationIsRecordedForItsRevisionAlone(t *testing.T) {
	ctx := context.Background()
	c := newStateClient(t)
	state := State{key: newFingerprintKey()}.Activated(revision1, []secretref.Key{keyB, keyA})
	state.retained = nameSet([]string{nameC})
	state.withdraw(nameC)
	for _, revision := range []string{revision2, ""} {
		if got := state.LetGo(revision); got != nil {
			t.Errorf("LetGo(%q) = %v, want none: only the recorded revision lets a name go", revision, got)
		}
		if got := state.LetGoKeys(revision); got != nil || state.Records(revision) {
			t.Errorf("LetGoKeys(%q) = %v and Records = %t, want none and false", revision, got, state.Records(revision))
		}
	}
	if got := (State{}).LetGo(""); got != nil || (State{}).Records("") {
		t.Errorf("the zero State lets %v go", got)
	}

	if err := SaveState(ctx, c, stateRef, company(), state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	secret := stateSecret(t, c)
	if got, want := string(secret.Data[letGoField]), "ns/a[token]\nns/b[token]\n"; got != want {
		t.Errorf("the state Secret holds %s = %q, want %q", letGoField, got, want)
	}
	for _, line := range []string{"not-a-key", "ns/b", "ns/b[]", "/b[token]", "ns/[token]", "ns/b[to]ken]", "ns/x/b[token]", "ns/b[token"} {
		secret.Data[letGoField] = append(secret.Data[letGoField], []byte(line+"\n")...)
	}
	secret.Data[retainedField] = append(secret.Data[retainedField], []byte("not-a-store-name\n")...)
	if err := c.Update(ctx, secret); err != nil {
		t.Fatalf("editing the state: %v", err)
	}
	loaded, err := LoadState(ctx, c, stateRef, company())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if want := slices.Sorted(slices.Values([]string{nameA, nameB})); !slices.Equal(loaded.LetGo(revision1), want) {
		t.Fatalf("LetGo(%s) = %v after a load, want %v", revision1, loaded.LetGo(revision1), want)
	}
	if want := []secretref.Key{keyA, keyB}; !slices.Equal(loaded.LetGoKeys(revision1), want) {
		t.Fatalf("LetGoKeys(%s) = %v after a load, want %v", revision1, loaded.LetGoKeys(revision1), want)
	}
	if !slices.Equal(loaded.Retained(), []string{nameC}) {
		t.Fatalf("Retained() = %v after a load, want [%s]", loaded.Retained(), nameC)
	}

	settled := loaded.Reactivated()
	if len(settled.Withdrawn()) != 0 || !slices.Equal(settled.LetGo(revision1), loaded.LetGo(revision1)) {
		t.Errorf("Reactivated() = withdrawn %v and LetGo %v, want none and the record kept",
			settled.Withdrawn(), settled.LetGo(revision1))
	}
	next := settled.Activated(revision2, []secretref.Key{keyC})
	if next.LetGo(revision1) != nil || !slices.Equal(next.LetGo(revision2), []string{nameC}) {
		t.Errorf("after the next activation LetGo(%s) = %v and LetGo(%s) = %v, want none and [%s]",
			revision1, next.LetGo(revision1), revision2, next.LetGo(revision2), nameC)
	}

	if err := SaveState(ctx, c, stateRef, company(), State{key: next.key}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	for _, field := range []string{revisionField, letGoField, retainedField} {
		if got, ok := stateSecret(t, c).Data[field]; !ok || len(got) != 0 {
			t.Errorf("the state Secret holds %s = %q (present: %t), want it empty, and owned", field, got, ok)
		}
	}
}

// TestAdoptionRecordsARevisionWithoutSettlingAWithdrawal: a revision the
// operator found running the document it renders is recorded as Activated
// records one, so LetGo answers for it alone, but a withdrawal stays owed:
// the operator did not activate that revision after it, so the engine may
// have read the store with the value.
func TestAdoptionRecordsARevisionWithoutSettlingAWithdrawal(t *testing.T) {
	state := State{}.Activated(revision1, []secretref.Key{keyA})
	state.withdraw(nameB)
	adopted := state.Adopted(revision2, []secretref.Key{keyB})
	if !slices.Equal(adopted.Withdrawn(), []string{nameB}) {
		t.Errorf("Withdrawn() = %v after an adoption, want [%s]", adopted.Withdrawn(), nameB)
	}
	if adopted.LetGo(revision1) != nil || !slices.Equal(adopted.LetGo(revision2), []string{nameB}) || !adopted.Records(revision2) {
		t.Errorf("after the adoption LetGo(%s) = %v and LetGo(%s) = %v, want none and [%s]",
			revision1, adopted.LetGo(revision1), revision2, adopted.LetGo(revision2), nameB)
	}
	if !slices.Equal(state.LetGo(revision1), []string{nameA}) {
		t.Errorf("the adoption changed the State it was made from: LetGo(%s) = %v", revision1, state.LetGo(revision1))
	}
	if adopted.Recorded() != revision2 || state.Recorded() != revision1 || (State{}).Recorded() != "" {
		t.Errorf("Recorded() = %q, %q and %q, want %s, %s and none", adopted.Recorded(), state.Recorded(), (State{}).Recorded(),
			revision2, revision1)
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
	want := slices.Sorted(slices.Values([]string{nameA, fingerprintKeyField, withdrawnField, writtenField, revisionField, letGoField, retainedField}))
	if !slices.Equal(got, want) {
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
	if other.key == state.key {
		t.Fatal("two states share a fingerprint key")
	}
	if bytes.Equal(other.fingerprints[nameA], state.fingerprints[nameA]) {
		t.Error("one value has the same fingerprint under two keys")
	}

	// Pin the construction, so a change to it is a decision: an HMAC of the
	// name, a NUL and the value, under the state's key.
	mac := hmac.New(sha256.New, []byte(state.key))
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
