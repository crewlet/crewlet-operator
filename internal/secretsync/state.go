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
)

// State is what the operator last pushed into one engine's secret store: a
// fingerprint of the value of every name it pushed, and the key the
// fingerprints are computed under.
//
// The zero State knows of nothing pushed, so Sync pushes every name it is
// given; it is what LoadState returns for a company that has no state yet.
// A State formats as its names only, because it holds the fingerprint key.
type State struct {
	key          []byte
	fingerprints map[string][]byte
}

// fingerprint returns the HMAC-SHA256 of the name and the value under the
// State's key. The name is part of the input so two names holding the same
// value do not show it by having the same fingerprint.
func (s State) fingerprint(name string, value []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
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
func newFingerprintKey() []byte {
	key := make([]byte, fingerprintKeyLen)
	_, _ = rand.Read(key)
	return key
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
	s := State{key: key, fingerprints: make(map[string][]byte, len(secret.Data)-1)}
	for name, fp := range secret.Data {
		if Owns(name) && len(fp) == sha256.Size {
			s.fingerprints[name] = fp
		}
	}
	return s, nil
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
	data := make(map[string][]byte, len(s.fingerprints)+1)
	maps.Copy(data, s.fingerprints)
	if len(s.key) > 0 {
		data[fingerprintKeyField] = s.key
	}
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
