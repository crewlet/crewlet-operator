package secretsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// MaxValueBytes is the largest value the engine's secret store accepts
// (PUT /secrets/{name} refuses a larger body). Resolve refuses a larger value
// itself, so the reference that holds it is reported on the object that made
// it rather than as an engine error halfway through a sync.
const MaxValueBytes = 64 << 10

// MissingReason says which part of a reference does not exist.
type MissingReason string

const (
	// SecretNotFound means the referenced Secret does not exist.
	SecretNotFound MissingReason = "SecretNotFound"
	// KeyNotFound means the Secret exists but holds no such key.
	KeyNotFound MissingReason = "KeyNotFound"
)

// MissingError reports a reference whose Secret or key does not exist. It is
// a fault of the object that made the reference, to be reported on it, not a
// reason to retry: the Secret's watch brings the reconcile back once someone
// creates it.
type MissingError struct {
	Key    secretref.Key
	Reason MissingReason
}

func (e *MissingError) Error() string {
	if e.Reason == SecretNotFound {
		return fmt.Sprintf("Secret %s/%s does not exist: create it, or reference one that does",
			e.Key.Namespace, e.Key.Name)
	}
	return fmt.Sprintf("Secret %s/%s has no key %q: add the key, or reference one the Secret holds",
		e.Key.Namespace, e.Key.Name, e.Key.Key)
}

// TooLargeError reports a referenced value larger than MaxValueBytes.
type TooLargeError struct {
	Key  secretref.Key
	Size int
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("key %q of Secret %s/%s holds %d bytes, more than the %d the engine's secret store accepts: "+
		"a credential that large is usually a file referenced by mistake", e.Key.Key, e.Key.Namespace, e.Key.Name,
		e.Size, MaxValueBytes)
}

// Desired is the set of store entries a company's references call for: a
// value per store name, and the names to keep without pushing (see Keep).
//
// It formats as its names only, under every verb and through logr, so a
// Desired that ends up in a log line or an error by mistake leaks nothing.
type Desired struct {
	values map[string][]byte
	kept   map[string]struct{}
}

// Names returns the names Desired pushes, sorted.
func (d Desired) Names() []string {
	return slices.Sorted(maps.Keys(d.values))
}

// Keep adds names that Sync must not delete although Desired pushes no value
// for them.
//
// It is for the window between two documents. Sync runs before the next
// document is applied, so that every name the next document references
// exists when the engine resolves it; but until it is applied the running
// document still references the previous names, and deleting one would make
// the engine resolve it to an empty credential at its next activation, or
// for as long as the next document fails to apply. Pass the running
// document's names here, and drop them once the next document is running.
// Names Owns does not accept are never deleted anyway, and are ignored.
func (d *Desired) Keep(names ...string) {
	if d.kept == nil {
		d.kept = make(map[string]struct{}, len(names))
	}
	for _, name := range names {
		d.kept[name] = struct{}{}
	}
}

// String returns the names Desired pushes, never a value.
func (d Desired) String() string {
	return "[" + strings.Join(d.Names(), " ") + "]"
}

// Format makes every fmt verb print what String prints: without it, %v and
// %#v would print the values.
func (d Desired) Format(f fmt.State, _ rune) {
	formatNames(f, d.String())
}

// MarshalLog makes logr print the names only.
func (d Desired) MarshalLog() any {
	return d.Names()
}

// Resolve reads the value of every key in keys, and returns the store entries
// they call for, named by StoreName.
//
// r must be an uncached reader: the operator watches Secrets as metadata only,
// so that it does not hold every Secret in the cluster in memory, and reads a
// value only when a reconcile needs it. Each Secret is read once however many
// of its keys are referenced, and a key referenced twice is one entry.
//
// Every reference whose Secret or key does not exist is reported, as a
// *MissingError, and every value over MaxValueBytes as a *TooLargeError,
// joined into one error so the caller can report each on the object that made
// it; Desired is then empty, because a company document is applied whole or
// not at all. Any other error is the API server's, returned as soon as it
// happens.
func Resolve(ctx context.Context, r client.Reader, keys []secretref.Key) (Desired, error) {
	bySecret := map[types.NamespacedName][]secretref.Key{}
	for _, k := range keys {
		ref := types.NamespacedName{Namespace: k.Namespace, Name: k.Name}
		if !slices.Contains(bySecret[ref], k) {
			bySecret[ref] = append(bySecret[ref], k)
		}
	}

	values := make(map[string][]byte, len(keys))
	var problems []error
	// Sorted, so the joined error reads the same on every reconcile and a
	// condition built from it does not flap.
	for _, ref := range slices.SortedFunc(maps.Keys(bySecret), compareRefs) {
		refKeys := bySecret[ref]
		slices.SortFunc(refKeys, secretref.Compare)

		secret := &corev1.Secret{}
		if err := r.Get(ctx, ref, secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return Desired{}, fmt.Errorf("reading Secret %s: %w", ref, err)
			}
			for _, k := range refKeys {
				problems = append(problems, &MissingError{Key: k, Reason: SecretNotFound})
			}
			continue
		}
		for _, k := range refKeys {
			value, ok := secret.Data[k.Key]
			switch {
			case !ok:
				problems = append(problems, &MissingError{Key: k, Reason: KeyNotFound})
			case len(value) > MaxValueBytes:
				problems = append(problems, &TooLargeError{Key: k, Size: len(value)})
			default:
				values[StoreName(k)] = value
			}
		}
	}
	if len(problems) > 0 {
		return Desired{}, errors.Join(problems...)
	}
	return Desired{values: values}, nil
}

func compareRefs(a, b types.NamespacedName) int {
	return secretref.Compare(
		secretref.Key{Namespace: a.Namespace, Name: a.Name},
		secretref.Key{Namespace: b.Namespace, Name: b.Name},
	)
}
