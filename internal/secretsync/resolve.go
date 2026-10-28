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
// value per store name, the names to keep without pushing (see Keep and
// KeepServed), and the names that are to hold no value: those of optional
// references whose Secret or key does not exist (see Resolve), each removed
// from the store unless a revision kept reads it as required, and those the
// revision the engine runs lets go without whose Secret or key does not
// exist (see Release).
//
// It formats as its names only, under every verb and through logr, so a
// Desired that ends up in a log line or an error by mistake leaks nothing.
type Desired struct {
	values map[string][]byte
	// kept are the names each revision kept references, by revision: the
	// fleet's revision, running, which Keep names, and every other revision
	// an engine node runs or starts on (KeepServed).
	kept    map[string]map[string]struct{}
	running string
	// unread are the engine nodes the operator does not know the revisions
	// of, or whose revision it could not read (KeepUnread), as a set: while
	// there is one, no name is deleted. A set rather than a sorted slice
	// keeps a Desired, a value every reconcile copies, as small to copy as
	// it was before it named any node.
	unread map[string]struct{}
	// absent are the optional references whose Secret or key does not
	// exist, by store name, each as the *MissingError it would be were it
	// required; empty are the references whose Secret key holds an empty
	// value, which is pushed as it is, as a set.
	absent map[string]*MissingError
	empty  map[secretref.Key]struct{}
	// released are the references the running revision lets go without
	// whose Secret or key does not exist, by store name (Release).
	released map[string]*MissingError
	// partial is a Desired Resolve returned beside the references it could
	// not read: it holds what they say about the others (Missing, Empty),
	// and no value, so Sync refuses it rather than delete every entry it
	// does not push.
	partial bool
}

// Names returns the names Desired pushes, sorted.
func (d Desired) Names() []string {
	return slices.Sorted(maps.Keys(d.values))
}

// Keep says what of the store the revision the engine runs needs: revision is
// its id, the fleet's revision (engine.ActiveConfig), and names the store
// names its document references, which Sync and Withdraw do not delete
// although Desired pushes no value for them. It is the revision Running
// names, whose reading of a name State.LetGo answers, and whose released
// names Release takes; a second call names another one, and keeps the names
// the first kept as well.
//
// It is for the window between two documents. Sync runs before the next
// document is applied, so that every name the next document references
// exists when the engine resolves it; but until it is applied the running
// document still references the previous names, and deleting one would make
// the engine resolve it to an empty credential at its next activation, or
// for as long as the next document fails to apply. Pass the running
// revision's id and names here. Names Owns does not accept are never deleted
// anyway, and are ignored.
//
// An absent name (Missing) among names is kept too, unless the State Sync or
// Withdraw is given records revision as letting the name go without
// (State.LetGo): the store serves every revision by that revision's own
// reading of a name, and the running one, which the operator has to keep
// whole until the next document is applied, may read as required a name the
// next document lets go without. Kept, its entry holds the value the Secret
// had, until a revision that lets it go runs (Result.Retained); removed, the
// engine resolves the name to nothing at its next activation, and leaves the
// key out.
func (d *Desired) Keep(revision string, names ...string) {
	d.running = revision
	d.keep(revision, names)
}

// KeepServed says what of the store another revision needs that an engine
// node runs or starts on, as Keep does for the fleet's. A node that refused
// the fleet's revision goes on running the revision it ran before
// (engine.ApplyRecord.Serving), and needs its values for as long as it does,
// since every attempt to apply a revision reads the store again into the
// snapshot the running revision resolves its references from. A node whose
// store outlives a restart, as a data pod's does, starts on the revision that
// store names (engine.ApplyRecord.Boots), the one it runs but while the
// engine retries moving it, and cannot start once a value that revision
// requires is gone from the store. Each such revision is passed here, by the
// node that runs it and by the node that starts on it. A node that runs no
// company and starts on none has no revision to pass, and needs nothing
// beyond what Keep names. Its names are kept, and its absent names retained
// unless the State records revision as letting them go, as the running
// revision's are; it does not change the revision Keep named.
func (d *Desired) KeepServed(revision string, names ...string) {
	d.keep(revision, names)
}

// KeepUnread says that the engine nodes named may run or start on a revision
// whose references the operator does not know: one it could not read, or a
// node whose revisions it has not learned. Every name the store lists is kept
// then, and every absent name retained, since any may be one such a revision
// requires, until a later Desired names what they run and start on.
func (d *Desired) KeepUnread(nodes ...string) {
	if len(nodes) == 0 {
		return
	}
	// In a set of d's own, as keep's: a copy that names more nodes must
	// not change the Desired it was copied from.
	unread := make(map[string]struct{}, len(d.unread)+len(nodes))
	maps.Copy(unread, d.unread)
	for _, node := range nodes {
		unread[node] = struct{}{}
	}
	d.unread = unread
}

// keep adds names to what revision keeps, in maps of d's own: a Desired is a
// value, and a copy of one that keeps more (a reconcile keeps the running
// revision's names on a copy of what it read) must not change the original.
func (d *Desired) keep(revision string, names []string) {
	kept := make(map[string]map[string]struct{}, len(d.kept)+1)
	maps.Copy(kept, d.kept)
	set := maps.Clone(kept[revision])
	if set == nil {
		set = make(map[string]struct{}, len(names))
	}
	for _, name := range names {
		set[name] = struct{}{}
	}
	kept[revision] = set
	d.kept = kept
}

// Running is the revision Keep named, empty for none.
func (d Desired) Running() string {
	return d.running
}

// Served are the revisions KeepServed named other than the one Keep named,
// sorted.
func (d Desired) Served() []string {
	return slices.Sorted(func(yield func(string) bool) {
		for revision := range d.kept {
			if revision != d.running && !yield(revision) {
				return
			}
		}
	})
}

// Unread are the engine nodes KeepUnread named, sorted.
func (d Desired) Unread() []string {
	return slices.Sorted(maps.Keys(d.unread))
}

// Holders are the revisions kept (Keep, KeepServed) that reference name and
// read it as required, as prev records their reading (State.LetGo), sorted:
// those that keep an absent name in the store.
func (d Desired) Holders(name string, prev State) []string {
	var out []string
	for revision, names := range d.kept {
		if _, ok := names[name]; ok && !prev.letsGo(revision, name) {
			out = append(out, revision)
		}
	}
	slices.Sort(out)
	return out
}

// Release names the references the revision the engine runs lets go without
// whose Secret or key does not exist, as Absent reports them of
// State.LetGoKeys of that revision: Sync and Withdraw remove the entry of
// each that the State they are given records the revision Keep names as
// letting go, whatever the next document reads of it, and record it as
// withdrawn.
//
// That is the other half of serving every revision by its own reading. The
// next document may reference such a key where it is required, or not at
// all, so its absent names (Missing) never name it; but the revision the
// engine runs lets it go, and would go on starting what it starts with the
// deleted Secret's value for as long as the next document is held, or the
// engine refuses it. Removing it costs the next document nothing: one that
// requires it cannot be applied until the Secret exists again, and Sync
// pushes the value then.
func (d *Desired) Release(absent ...*MissingError) {
	if d.released == nil {
		d.released = make(map[string]*MissingError, len(absent))
	}
	for _, m := range absent {
		d.released[StoreName(m.Key)] = m
	}
}

// retains reports whether name, absent, stays in the store: a revision kept
// references it (Keep, KeepServed) and reads it as required, which it does
// unless prev records it as letting the name go without (State.LetGo), or a
// node runs a revision the operator does not know (KeepUnread).
func (d Desired) retains(name string, prev State) bool {
	return len(d.unread) > 0 || len(d.Holders(name, prev)) > 0
}

// keeps reports whether a revision kept references name, or a node runs a
// revision the operator does not know: either way the store keeps its entry.
func (d Desired) keeps(name string) bool {
	if len(d.unread) > 0 {
		return true
	}
	for _, names := range d.kept {
		if _, ok := names[name]; ok {
			return true
		}
	}
	return false
}

// letGo reports whether a revision kept references name and, as prev
// records its reading, lets it go: what makes a released name one to remove,
// where no other revision kept retains it.
func (d Desired) letGo(name string, prev State) bool {
	for revision, names := range d.kept {
		if _, ok := names[name]; ok && prev.letsGo(revision, name) {
			return true
		}
	}
	return false
}

// gone reports whether name stands for a reference whose Secret or key does
// not exist, absent from the next document (Missing) or released by a
// revision kept that lets it go (Release), and whether its entry is to leave
// the store: it is unless a revision kept reads it as required (retains). A
// name Desired pushes a value for is neither, whatever else says it is gone:
// its Secret was read after.
func (d Desired) gone(name string, prev State) (absent, removed bool) {
	if _, ok := d.values[name]; ok {
		return false, false
	}
	if _, ok := d.absent[name]; ok {
		return true, !d.retains(name, prev)
	}
	if _, ok := d.released[name]; ok && d.letGo(name, prev) {
		return true, !d.retains(name, prev)
	}
	return false, false
}

// deletes reports whether Sync deletes name, an owned name the store lists
// that Desired pushes no value for: one gone and removed, or one neither gone
// nor kept.
func (d Desired) deletes(name string, prev State) bool {
	absent, removed := d.gone(name, prev)
	if absent {
		return removed
	}
	return !d.keeps(name)
}

// fleetsAlone is d keeping only what Keep named, the fleet's revision: what
// the store would hold were every node running it, against which Sync and
// Withdraw tell what they keep for another revision (Result.Held).
func (d Desired) fleetsAlone() Desired {
	alone := d
	alone.kept, alone.unread = nil, nil
	if names, ok := d.kept[d.running]; ok {
		alone.kept = map[string]map[string]struct{}{d.running: names}
	}
	return alone
}

// goneNames are the store names of absent and released, sorted.
func (d Desired) goneNames() []string {
	names := slices.Collect(maps.Keys(d.absent))
	for name := range d.released {
		if _, ok := d.absent[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Missing are the optional references whose Secret or key does not exist,
// which hold no value in the store but where a revision kept reads them as
// required (see Keep), sorted as Resolve sorts its errors.
func (d Desired) Missing() []*MissingError {
	return slices.SortedFunc(maps.Values(d.absent), func(a, b *MissingError) int { return secretref.Compare(a.Key, b.Key) })
}

// Empty are the references whose Secret key holds an empty value, sorted. The
// value is pushed as it is: the engine starts no tool server with a value
// that resolves to nothing and starts an executor run without it, leaves the
// key out where it is written under an optional key, and reads it as written
// anywhere else.
func (d Desired) Empty() []secretref.Key {
	return slices.SortedFunc(maps.Keys(d.empty), secretref.Compare)
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
// it; Desired then pushes nothing, because a company document is applied
// whole or not at all, and Sync refuses it. What it still says is what was
// read of the other references, Missing and Empty, which hold whatever became
// of the rest: Withdraw takes it. Any other error is the API server's,
// returned as soon as it happens.
//
// A reference of optional, the keys every reference to which the document
// lets go without its value (render.Document.Optional), is the exception, as
// a missing optional secretKeyRef is to Kubernetes: a Secret or key that does
// not exist there is no error but an absent value (Missing), whose store
// entry Sync and Withdraw remove wherever the revision the engine runs lets
// it go too (see Keep), so the engine resolves the reference to nothing and
// leaves its key out rather than start with a value the Secret no longer
// holds. A value over MaxValueBytes is refused there too: it is there.
func Resolve(ctx context.Context, r client.Reader, keys, optional []secretref.Key) (Desired, error) {
	bySecret := map[types.NamespacedName][]secretref.Key{}
	for _, k := range keys {
		ref := types.NamespacedName{Namespace: k.Namespace, Name: k.Name}
		if !slices.Contains(bySecret[ref], k) {
			bySecret[ref] = append(bySecret[ref], k)
		}
	}

	values := make(map[string][]byte, len(keys))
	absent := map[string]*MissingError{}
	empty := map[secretref.Key]struct{}{}
	var problems []error
	missing := func(k secretref.Key, reason MissingReason) {
		if slices.Contains(optional, k) {
			absent[StoreName(k)] = &MissingError{Key: k, Reason: reason}
			return
		}
		problems = append(problems, &MissingError{Key: k, Reason: reason})
	}
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
				missing(k, SecretNotFound)
			}
			continue
		}
		for _, k := range refKeys {
			value, ok := secret.Data[k.Key]
			switch {
			case !ok:
				missing(k, KeyNotFound)
			case len(value) > MaxValueBytes:
				problems = append(problems, &TooLargeError{Key: k, Size: len(value)})
			default:
				values[StoreName(k)] = value
				if len(value) == 0 {
					empty[k] = struct{}{}
				}
			}
		}
	}
	if len(problems) > 0 {
		return Desired{absent: absent, empty: empty, partial: true}, errors.Join(problems...)
	}
	return Desired{values: values, absent: absent, empty: empty}, nil
}

// Absent reads keys, with r, an uncached reader as Resolve takes, and returns
// those whose Secret or key does not exist, sorted as Resolve sorts its
// errors: for Desired.Release, of the keys the running revision lets go
// without (State.LetGoKeys). A value over MaxValueBytes is there, so it is no
// concern here. Any other error is the API server's.
func Absent(ctx context.Context, r client.Reader, keys []secretref.Key) ([]*MissingError, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	read, err := Resolve(ctx, r, keys, keys)
	if err != nil && !read.partial {
		// The API server's. One beside a partial read is about the
		// references, and with every one of them optional it can only be a
		// value too large, which is there.
		return nil, err
	}
	return read.Missing(), nil
}

func compareRefs(a, b types.NamespacedName) int {
	return secretref.Compare(
		secretref.Key{Namespace: a.Namespace, Name: a.Name},
		secretref.Key{Namespace: b.Namespace, Name: b.Name},
	)
}
