package secretsync

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

const (
	// FieldOwner is the server-side apply field manager of the state Secret.
	FieldOwner = "crewlet-operator"

	// fingerprintKeyField is the data key of the state Secret that holds the
	// fingerprint key. Every other data key is a store name, which Owns
	// accepts and this, with its dash, never is.
	fingerprintKeyField = "fingerprint-key"

	// fingerprintKeyLen is the fingerprint key's length: as long as
	// SHA-256's output, so the key is never the weaker part of the HMAC.
	fingerprintKeyLen = sha256.Size

	// withdrawnField is the data key of the state Secret that lists the
	// withdrawn names (State.Withdrawn), one per line. Like
	// fingerprintKeyField it has a dash, so Owns never accepts it, and so
	// have the three after it.
	withdrawnField = "withdrawn-names"

	// revisionField and letGoField are the data keys of the state Secret
	// that hold the revision whose reading of the store the State records
	// and the Secret keys that revision's document lets go without, one
	// per line as secretref.Key prints them (State.Activated, State.Adopted);
	// retainedField lists the retained names (State.Retained), one per line.
	revisionField = "activated-revision"
	letGoField    = "optional-keys"
	retainedField = "retained-names"
)

// State is what the operator last pushed into one engine's secret store: a
// fingerprint of the value of every name it pushed, and the key the
// fingerprints are computed under; the names it withdrew (Withdrawn); one
// revision of the engine's document whose reading of the store it knows, with
// the Secret keys that revision lets go without (Activated, Adopted, LetGo);
// and the names it last kept for the running revision although their Secret
// or key is gone (Retained).
//
// It records nothing of what an engine node runs or starts on, which the
// caller reads from the engine's fleet alone: a node applies the fleet's
// revision on its own loop, and a data node moves the revision its store
// starts it on after every apply that succeeds, whether or not the caller is
// looking, so nothing the operator saw of a node before says what it runs or
// starts on now. For a node the fleet does not list that may need a value,
// the caller keeps every value instead (Desired.KeepUnread).
//
// The zero State knows of nothing pushed, so Sync pushes every name it is
// given, and of no revision, so it reads every name the running document
// references as required; it is what LoadState returns for a company that has
// no state yet. A State formats as its names only, because it holds the
// fingerprint key.
type State struct {
	// key is the fingerprint key, a string so that it stays as small to
	// copy as every other field, and immutable in every copy: a State is a
	// value its methods return changed copies of, and each copy shares it.
	key          string
	fingerprints map[string][]byte
	withdrawn    map[string]struct{}
	// revision is the revision whose reading the State records, empty for
	// none, and letGo the Secret keys its document lets go without, by
	// store name.
	revision string
	letGo    map[string]secretref.Key
	retained map[string]struct{}
}

// Withdrawn are the names Sync or Withdraw removed from the store because the
// optional reference each stands for has no Secret or key any more, since a
// revision last activated, sorted.
//
// The engine reads its store when a revision activates, and starts every tool
// server and executor run with what it read then: removing a name changes
// nothing it runs until the next activation, which may be minutes away, or
// none at all while the next document cannot be applied. Until then it would
// go on starting things with the value of a Secret Kubernetes no longer
// holds. So the caller activates a revision once a name is withdrawn, the
// next document or the running one again, and then records it with
// Activated, or with Reactivated where it activated none because the
// revision the engine runs needs no activation for it. Kept in the saved
// State, a withdrawal survives a failed activation and a restarted operator.
func (s State) Withdrawn() []string {
	return slices.Sorted(maps.Keys(s.withdrawn))
}

// Reactivated is s with every withdrawal it records settled: a revision has
// activated since, so the engine has read the store without them, or the
// revision the engine runs is one no activation is owed for (see LetGo).
// The revision it records stays recorded.
func (s State) Reactivated() State {
	s.withdrawn = nil
	return s
}

// Activated is s once the operator has activated revision, whose document
// lets go without the Secret keys optional: every withdrawal is settled, as
// Reactivated, and LetGo answers optional for revision from now on, and
// nothing for any other.
//
// The caller records every activation it makes and knows the document of: an
// apply, with render.Document.Optional of the document it applied, and an
// activation of the running revision again, unchanged, with LetGoKeys of the
// revision it read, once it has confirmed that the revision activated again
// is that one.
func (s State) Activated(revision string, optional []secretref.Key) State {
	return s.Reactivated().Adopted(revision, optional)
}

// Adopted is s recording revision, whose document lets go without the Secret
// keys optional, without settling a withdrawal: the caller found revision
// running a document it renders, render.Document.Optional of which is
// optional, but did not activate it after the withdrawals it owes, so the
// engine may have read the store with them.
func (s State) Adopted(revision string, optional []secretref.Key) State {
	s.revision = revision
	s.letGo = make(map[string]secretref.Key, len(optional))
	for _, k := range optional {
		s.letGo[StoreName(k)] = k
	}
	return s
}

// Recorded is the revision whose reading s records, empty for none.
func (s State) Recorded() string {
	return s.revision
}

// Records reports whether s records how revision reads the store: whether
// LetGo answers for it.
func (s State) Records(revision string) bool {
	return revision != "" && revision == s.revision
}

// LetGo are the store names the document of revision lets go without, every
// reference to each being under a key its block declares optional, sorted:
// those Activated or Adopted recorded, when revision is the one recorded, and
// none for any other revision, an empty one included.
//
// A revision whose reading the State does not record reads every name its
// document references as required, as far as the secret sync is concerned:
// the operator does not know how that document reads it, and a name removed
// from the store that the running revision requires stops what it starts
// with the value at the next activation of that revision, however long the
// next document is held. The revision ids the engine mints are never reused
// (a revert and a reload each write a new one), so a matching id is that very
// revision.
func (s State) LetGo(revision string) []string {
	if !s.Records(revision) {
		return nil
	}
	return slices.Sorted(maps.Keys(s.letGo))
}

// LetGoKeys are the Secret keys LetGo names the store entries of, sorted as
// secretref.Compare sorts them: those whose Secret or key is gone are the
// values the revision goes without, whatever the next document reads of
// them (Desired.Release).
func (s State) LetGoKeys(revision string) []secretref.Key {
	if !s.Records(revision) {
		return nil
	}
	return slices.SortedFunc(maps.Values(s.letGo), secretref.Compare)
}

// letsGo reports whether the document of revision lets go without name, as
// LetGo says.
func (s State) letsGo(revision, name string) bool {
	_, ok := s.letGo[name]
	return ok && s.Records(revision)
}

// Retained are the names the last Sync or Withdraw left in the store,
// although the optional reference each stands for has no Secret or key any
// more, because the revision the engine runs reads them as required, or is
// one whose reading the State does not record (Result.Retained), sorted. The caller compares them with the Retained of
// the State it started from, to tell a name it newly keeps from one it has
// already said it keeps.
func (s State) Retained() []string {
	return slices.Sorted(maps.Keys(s.retained))
}

// withdraw records name as withdrawn in s, which it changes in place.
func (s *State) withdraw(name string) {
	if s.withdrawn == nil {
		s.withdrawn = map[string]struct{}{}
	}
	s.withdrawn[name] = struct{}{}
}

// carried is a State that starts from s: its key, its record of withdrawals
// and of the activated revision, and what it last retained, in maps of its
// own where a Sync or a Withdraw changes them in place, and fingerprints.
func (s State) carried(fingerprints map[string][]byte) State {
	return State{
		key: s.key, fingerprints: fingerprints, withdrawn: maps.Clone(s.withdrawn),
		revision: s.revision, letGo: s.letGo, retained: s.retained,
	}
}

// nameSet is names as a set, nil for none.
func nameSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

// fingerprint returns the HMAC-SHA256 of the name and the value under the
// State's key. The name is part of the input so two names holding the same
// value do not show it by having the same fingerprint.
func (s State) fingerprint(name string, value []byte) []byte {
	mac := hmac.New(sha256.New, []byte(s.key))
	mac.Write([]byte(name))
	mac.Write([]byte{0})
	mac.Write(value)
	return mac.Sum(nil)
}

// Digest returns a hex SHA-256 over names and the fingerprint the State
// holds for each, in the order given. It changes when the value pushed under
// one of the names changes, and reveals no more about the values than the
// fingerprints do, which is nothing without the State's key: a caller can
// store it beside the document it applied, to tell later whether a value
// that document references has been rotated since.
//
// A name the State holds no fingerprint for contributes its name alone.
func (s State) Digest(names []string) string {
	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(s.fingerprints[name])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// String returns the names the State holds a fingerprint for.
func (s State) String() string {
	return "[" + strings.Join(slices.Sorted(maps.Keys(s.fingerprints)), " ") + "]"
}

// Format makes every fmt verb print what String prints: without it, %v and
// %#v would print the fingerprint key.
func (s State) Format(f fmt.State, _ rune) {
	formatNames(f, s.String())
}

// MarshalLog makes logr print the names only.
func (s State) MarshalLog() any {
	return slices.Sorted(maps.Keys(s.fingerprints))
}

// formatNames writes a String result for a Formatter. A Formatter cannot
// return an error, and fmt.State writes into fmt's own buffer, which does not
// fail, so the write's result carries nothing to act on.
func formatNames(f fmt.State, s string) {
	_, _ = io.WriteString(f, s)
}

// newFingerprintKey returns a random fingerprint key. crypto/rand.Read does
// not return an error: it crashes the program instead, because a process
// without randomness cannot do anything safely.
func newFingerprintKey() string {
	key := make([]byte, fingerprintKeyLen)
	_, _ = rand.Read(key)
	return string(key)
}

// LoadState reads the State saved in the Secret ref, with r, which must be
// an uncached reader (see Resolve).
//
// A Secret that does not exist yet is the zero State. So is one whose
// fingerprint key is missing or malformed, which only a hand edit makes: its
// fingerprints cannot be checked without the key, and the cost of starting
// over is one push of each value, which leaves the store as it was. For the
// same reason an entry that is not a well-formed fingerprint of a name the
// operator owns is dropped, and its name pushed again.
//
// A Secret of that name that owner does not control is not the operator's to
// read or to overwrite: LoadState returns a *NotOwnedError, which names its
// controller, and SaveState must not be called.
func LoadState(ctx context.Context, r client.Reader, ref types.NamespacedName, owner client.Object) (State, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, ref, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("reading the secret sync state %s: %w", ref, err)
	}
	if !metav1.IsControlledBy(secret, owner) {
		return State{}, &NotOwnedError{Ref: ref, Controller: metav1.GetControllerOf(secret)}
	}
	key := secret.Data[fingerprintKeyField]
	if len(key) != fingerprintKeyLen {
		return State{}, nil
	}
	s := State{key: string(key), fingerprints: make(map[string][]byte, len(secret.Data)-1)}
	for name, fp := range secret.Data {
		if Owns(name) && len(fp) == sha256.Size {
			s.fingerprints[name] = fp
		}
	}
	// A line that is no name the operator owns, or no Secret key, is
	// dropped, as a malformed fingerprint is: only a hand edit writes one,
	// and no entry of it can be the operator's to have withdrawn, let go or
	// retained.
	s.withdrawn = ownedLines(secret.Data[withdrawnField])
	s.revision = string(secret.Data[revisionField])
	s.letGo = keyLines(secret.Data[letGoField])
	s.retained = ownedLines(secret.Data[retainedField])
	return s, nil
}

// keyLines are the lines of data that are Secret keys as secretref.Key prints
// them, by store name, nil for none.
func keyLines(data []byte) map[string]secretref.Key {
	var keys map[string]secretref.Key
	for line := range strings.Lines(string(data)) {
		k, ok := parseKey(strings.TrimSuffix(line, "\n"))
		if !ok {
			continue
		}
		if keys == nil {
			keys = map[string]secretref.Key{}
		}
		keys[StoreName(k)] = k
	}
	return keys
}

// parseKey reads a Secret key as secretref.Key prints it, namespace/name[key],
// and reports whether line is one: a namespace and a Secret name hold no "/",
// "[" or "]", and neither does a key.
func parseKey(line string) (secretref.Key, bool) {
	namespace, rest, ok := strings.Cut(line, "/")
	if !ok {
		return secretref.Key{}, false
	}
	name, rest, ok := strings.Cut(rest, "[")
	if !ok {
		return secretref.Key{}, false
	}
	key, ok := strings.CutSuffix(rest, "]")
	k := secretref.Key{Namespace: namespace, Name: name, Key: key}
	if !ok || namespace == "" || name == "" || key == "" || strings.ContainsAny(namespace+name+key, "/[]") {
		return secretref.Key{}, false
	}
	return k, true
}

// ownedLines are the lines of data that are names Owns accepts, as a set,
// nil for none.
func ownedLines(data []byte) map[string]struct{} {
	var names []string
	for name := range strings.Lines(string(data)) {
		if name = strings.TrimSuffix(name, "\n"); Owns(name) {
			names = append(names, name)
		}
	}
	return nameSet(names)
}

// lines is names, one per line, never nil.
func lines(names []string) []byte {
	out := []byte{}
	for _, name := range names {
		out = append(out, name+"\n"...)
	}
	return out
}

// NotOwnedError is a state Secret that exists and that the owner LoadState
// was given does not control: someone else's Secret of the name the operator
// keeps its state under.
type NotOwnedError struct {
	Ref types.NamespacedName
	// Controller is the Secret's controller, nil when it has none.
	Controller *metav1.OwnerReference
}

func (e *NotOwnedError) Error() string {
	controller := "no controller"
	if e.Controller != nil {
		controller = "the controller " + e.Controller.Kind + " " + e.Controller.Name
	}
	return fmt.Sprintf("Secret %s, where the secret sync keeps its state, exists with %s: "+
		"the operator neither reads nor overwrites a Secret it did not create; rename or delete it", e.Ref, controller)
}

// SaveState writes s into the Secret ref, creating it if needed, owned by
// owner (the Company), so the Secret goes when the Company does.
//
// It applies the whole Secret with server-side apply, which needs no read
// first: the manager's cache does not hold Secret data, and an entry s no
// longer has is removed because this field manager owned it.
func SaveState(ctx context.Context, c client.Client, ref types.NamespacedName, owner client.Object, s State) error {
	gvk, err := apiutil.GVKForObject(owner, c.Scheme())
	if err != nil {
		return fmt.Errorf("finding the kind of the owner of the secret sync state %s: %w", ref, err)
	}
	data := make(map[string][]byte, len(s.fingerprints)+5)
	maps.Copy(data, s.fingerprints)
	if len(s.key) > 0 {
		data[fingerprintKeyField] = []byte(s.key)
	}
	// Written when they are empty too, so this field manager owns them: a
	// list it left out would stay where a hand edit put it, and every
	// reconcile would activate a revision for names it withdrew long ago,
	// or take a revision whose reading it does not know for one that lets
	// a name go.
	data[withdrawnField] = lines(s.Withdrawn())
	data[revisionField] = []byte(s.revision)
	keys := slices.SortedFunc(maps.Values(s.letGo), secretref.Compare)
	printed := make([]string, len(keys))
	for i, k := range keys {
		printed[i] = k.String()
	}
	data[letGoField] = lines(printed)
	data[retainedField] = lines(s.Retained())
	secret := corev1ac.Secret(ref.Name, ref.Namespace).
		WithLabels(map[string]string{
			"app.kubernetes.io/name":       "crewlet",
			"app.kubernetes.io/instance":   owner.GetName(),
			"app.kubernetes.io/component":  "secret-sync-state",
			"app.kubernetes.io/managed-by": "crewlet-operator",
		}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(owner.GetName()).
			WithUID(owner.GetUID()).
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	if err := c.Apply(ctx, secret, client.FieldOwner(FieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("saving the secret sync state %s: %w", ref, err)
	}
	return nil
}
