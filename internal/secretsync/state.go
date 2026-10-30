package secretsync

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
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
	// withdrawn names (State.Withdrawn), and writtenField the one that lists
	// the written names (State.Written): one per line, each followed, after
	// a space, by the activation epoch it waits for an activation above,
	// where a read of the fleet's activation pointer has followed it
	// (State.Observed). Like fingerprintKeyField each has a dash, so Owns
	// never accepts it, and so have the five after them.
	withdrawnField = "withdrawn-names"
	writtenField   = "written-names"

	// revisionField and letGoField are the data keys of the state Secret
	// that hold the revision whose reading of the store the State records
	// and the Secret keys that revision's document lets go without, one
	// per line as secretref.Key prints them (State.Activated, State.Adopted);
	// retainedField lists the retained names (State.Retained), one per line.
	revisionField = "activated-revision"
	letGoField    = "optional-keys"
	retainedField = "retained-names"

	// pendingField is the data key of the state Secret that holds the
	// readings of the documents written whose activation a read is still to
	// confirm (State.Pending), as one JSON object keyed by the hash the
	// caller gave each.
	pendingField = "pending-activations"

	// awaitField is the data key of the state Secret that lists the
	// revisions the changes of the store await (State.Awaiting): one change
	// and one revision per line, the name, a space and the revision.
	awaitField = "awaited-revisions"
)

// State is what the operator last pushed into one engine's secret store: a
// fingerprint of the value of every name it pushed, and the key the
// fingerprints are computed under; the names it withdrew (Withdrawn), and
// the ones it wrote (Written), that no activation is known to have read the
// store after, with the activation epoch each waits past (Observed, Ran); one
// revision of the engine's document whose reading of the store it knows, with
// the Secret keys that revision lets go without (Activated, Adopted, LetGo);
// the names it last kept for the running revision although their Secret or
// key is gone (Retained); the reading of each document the caller wrote
// whose revision a later read may find running without the caller having
// recorded it (Pending, Confirmed); and the revisions the engine stored for
// activations asked for after a change and could not confirm, any of which
// found running settles it (Awaiting, Found).
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
	// withdrawn and written are the store changes no activation is known to
	// have read, by name: each the epoch of the fleet's activation pointer
	// read first after it, which an activation the engine runs at a greater
	// epoch came after, and unread where no read has followed it yet.
	withdrawn map[string]int64
	written   map[string]int64
	// awaits are the revisions Awaiting recorded for those changes, by
	// name: each a revision the engine stored for an activation asked for
	// after the change, sorted. An entry for a name the State records no
	// change of is none: a change settled otherwise leaves nothing to await.
	awaits map[string][]string
	// readings is how the documents the State knows of read the store, nil
	// for none (readingRecord).
	readings *readingRecord
	retained map[string]struct{}
}

// readingRecord is how the documents a State knows of read the store: the
// revision whose reading it records, and the documents the caller wrote whose
// revision a read may yet find running (Pending), whose reading becomes the
// revision's once one does (Confirmed). Behind one pointer, so that a State,
// which callers pass by value, stays inside the size a value parameter is
// held to; and never changed once made: a State that changes it makes another
// (withReadings).
type readingRecord struct {
	// revision is the revision whose reading the State records, empty for
	// none, and letGo the Secret keys its document lets go without, by
	// store name.
	revision string
	letGo    map[string]secretref.Key
	// pending are the readings Pending recorded: the Secret keys each
	// document lets go without, by store name, by the hash the caller gave
	// the document.
	pending map[string]map[string]secretref.Key
}

// reading is what s records of how documents read the store, the zero record
// for nothing.
func (s State) reading() readingRecord {
	if s.readings == nil {
		return readingRecord{}
	}
	return *s.readings
}

// withReadings is s recording r, which it does not change afterwards.
func (s State) withReadings(r readingRecord) State {
	s.readings = nil
	if r.revision != "" || len(r.letGo) > 0 || len(r.pending) > 0 {
		s.readings = &r
	}
	return s
}

// pending is what s records under Pending, nil for nothing.
func (s State) pending() map[string]map[string]secretref.Key {
	return s.reading().pending
}

// Withdrawn are the names Sync or Withdraw removed from the store because the
// optional reference each stands for has no Secret or key any more, since a
// revision activated after the removal, sorted.
//
// The engine reads its store when a revision activates, and starts every tool
// server and executor run with what it read then: removing a name changes
// nothing it runs until the next activation, which may be minutes away, or
// none at all while the next document cannot be applied. Until then it would
// go on starting things with the value of a Secret Kubernetes no longer
// holds. So the caller activates a revision once a name is withdrawn, the
// next document or the running one again, and then records it with
// Activated, or with Reactivated where it activated none because the
// revision the engine runs needs no activation for it; and any activation the
// engine is read running above the epoch the withdrawal waits past settles it
// too (Ran), whoever made it. Kept in the saved State, a withdrawal survives a
// failed activation, a lost save after it and a restarted operator.
func (s State) Withdrawn() []string {
	return slices.Sorted(maps.Keys(s.withdrawn))
}

// Written are the names Sync wrote a value under that no activation is known
// to have read the store after, sorted: every value it pushed, whether new or
// written back over one somebody else wrote or the store lost.
//
// A written value changes nothing the engine runs until a revision activates,
// as a withdrawal does not, and a value written back leaves the fingerprints,
// and so the caller's hash of what it applied, where they were: since the
// value was replaced, a revision may have activated reading the other one,
// which it goes on running. So a revision of the caller's document is one it
// runs with every value only once a revision has activated after the last
// write (Activated, Reloaded, Ran). Sync saves the mark before the value is
// written, so a write survives a write of the document that fails or loses
// its race, a save lost after the value was written, and a restarted
// operator, which the caller's own record of what one reconcile wrote did not.
func (s State) Written() []string {
	return slices.Sorted(maps.Keys(s.written))
}

// Reloaded is s once a revision has activated after every write and
// withdrawal it records, a revision whose document the caller does not know
// included: the engine read the store with each value written and without
// each withdrawn, so both are settled. The revision it records stays
// recorded.
func (s State) Reloaded() State {
	s.withdrawn, s.written, s.awaits = nil, nil, nil
	return s
}

// Reactivated is s with every withdrawal it records settled: a revision has
// activated since, so the engine has read the store without them, or the
// revision the engine runs is one no activation is owed for (see LetGo).
// The revision it records stays recorded.
func (s State) Reactivated() State {
	s.withdrawn = nil
	s.awaits = s.awaited()
	return s
}

// unread is the mark of a change of the store no read of the fleet's
// activation pointer has followed yet: no activation is above it, so none
// settles it but one the caller makes, and the first read after it lowers it
// to the epoch read (Observed).
const unread = math.MaxInt64

// Observed is s once the caller has read the fleet's activation pointer at
// epoch after every change of the store s records: each change no read has
// followed yet waits for an activation above epoch, which the pointer can
// only name once a write after this read moved it, so after the change; one
// a read followed already keeps the earlier epoch. Zero is a pointer that
// names nothing.
//
// The epoch is read after the changes, never before: one read before a change
// would settle it by an activation the pointer named before it, which read
// the store without it. And it is read before the activation the caller asks
// for, and saved with s before that is sent, so that a save lost after the
// activation leaves a change that activation settles (Ran) rather than one it
// waits past.
func (s State) Observed(epoch int64) State {
	s.withdrawn = observed(s.withdrawn, epoch)
	s.written = observed(s.written, epoch)
	return s
}

// Ran is s once the caller has read the fleet's activation pointer at epoch,
// naming a revision it has seen opened (the one GET /config answered): every
// change that waits for an activation above a lower epoch is settled, whoever
// made that activation and whatever its document, since it came after the
// change, and every node reads the store at its attempt to apply it, a node
// that refuses it included, whose running revision resolves its references
// from what that attempt read. A revision no node can open (one sealed under
// a key the keyring lost, or stored without a seal) is not one they read the
// store for, however the pointer names it, and settles nothing.
func (s State) Ran(epoch int64) State {
	s.withdrawn = ran(s.withdrawn, epoch)
	s.written = ran(s.written, epoch)
	s.awaits = s.awaited()
	return s
}

// Awaiting is s once the caller has asked the engine for an activation after
// every change of the store s records, and the engine stored revision for it
// and could not confirm activating it: should a read find revision running,
// it activated after every one of those changes and every node read the
// store when it applied it, so it settles them (Found), whatever the fleet's
// activation pointer says. A change a later read of the pointer has followed
// is settled by any activation above it (Ran), and needs no such record; one
// no read has followed (Observed) needs it, since no epoch the pointer names
// later can settle it, and the pointer is likeliest to be unreadable when the
// engine loses an activation's answer. A later change of a name waits for an
// activation of its own.
func (s State) Awaiting(revision string) State {
	if revision == "" {
		return s
	}
	awaits := s.awaited()
	for _, marks := range []map[string]int64{s.withdrawn, s.written} {
		for name := range marks {
			if revisions := awaits[name]; !slices.Contains(revisions, revision) {
				if awaits == nil {
					awaits = map[string][]string{}
				}
				awaits[name] = slices.Sorted(slices.Values(append(slices.Clone(revisions), revision)))
			}
		}
	}
	s.awaits = awaits
	return s
}

// Found is s once the caller has read the engine running revision, as a
// revision its nodes can open: every change that awaits it (Awaiting) is
// settled, as Ran settles one an activation above its epoch came after.
func (s State) Found(revision string) State {
	awaits := s.awaits
	if revision == "" || len(awaits) == 0 {
		return s
	}
	read := func(name string, _ int64) bool { return slices.Contains(awaits[name], revision) }
	s.withdrawn = without(s.withdrawn, read)
	s.written = without(s.written, read)
	s.awaits = s.awaited()
	return s
}

// SameMarks reports whether s and o record the same changes of the store,
// each with the same mark and awaiting the same revisions: whether a save of
// one leaves what a save of the other did.
func (s State) SameMarks(o State) bool {
	return maps.Equal(s.withdrawn, o.withdrawn) && maps.Equal(s.written, o.written) &&
		maps.EqualFunc(s.awaited(), o.awaited(), slices.Equal)
}

// awaited is what s awaits (Awaiting) for the changes it records, in a map of
// its own, nil for none: an entry for a name settled otherwise is dropped.
func (s State) awaited() map[string][]string {
	var awaits map[string][]string
	for name, revisions := range s.awaits {
		_, withdrawn := s.withdrawn[name]
		_, written := s.written[name]
		if len(revisions) == 0 || !withdrawn && !written {
			continue
		}
		if awaits == nil {
			awaits = map[string][]string{}
		}
		awaits[name] = revisions
	}
	return awaits
}

// Unobserved reports whether s records a change of the store no read of the
// fleet's activation pointer has followed yet (Observed).
func (s State) Unobserved() bool {
	for _, marks := range []map[string]int64{s.withdrawn, s.written} {
		for _, epoch := range marks {
			if epoch == unread {
				return true
			}
		}
	}
	return false
}

// observed is marks with every one above epoch lowered to it, in a map of its
// own: the State's maps are values other States share.
func observed(marks map[string]int64, epoch int64) map[string]int64 {
	if marks == nil {
		return nil
	}
	next := make(map[string]int64, len(marks))
	for name, mark := range marks {
		next[name] = min(mark, epoch)
	}
	return next
}

// ran is marks less every one an activation at epoch is above, in a map of
// its own, nil for none.
func ran(marks map[string]int64, epoch int64) map[string]int64 {
	return without(marks, func(_ string, mark int64) bool { return epoch > mark })
}

// without is marks less every one settled reports, in a map of its own, nil
// for none.
func without(marks map[string]int64, settled func(name string, mark int64) bool) map[string]int64 {
	var left map[string]int64
	for name, mark := range marks {
		if settled(name, mark) {
			continue
		}
		if left == nil {
			left = map[string]int64{}
		}
		left[name] = mark
	}
	return left
}

// Activated is s once the operator has activated revision, whose document
// lets go without the Secret keys optional: every withdrawal and every write
// is settled, as Reloaded, and LetGo answers optional for revision from now
// on, and nothing for any other.
//
// The caller records every activation it makes and knows the document of: an
// apply, with render.Document.Optional of the document it applied, and an
// activation of the running revision again, unchanged, with LetGoKeys of the
// revision it read, once it has confirmed that the revision activated again
// is that one.
func (s State) Activated(revision string, optional []secretref.Key) State {
	return s.Reloaded().Adopted(revision, optional)
}

// Adopted is s recording revision, whose document lets go without the Secret
// keys optional, without settling a withdrawal: the caller found revision
// running a document it renders, render.Document.Optional of which is
// optional, but did not activate it after the withdrawals it owes, so the
// engine may have read the store with them.
func (s State) Adopted(revision string, optional []secretref.Key) State {
	r := s.reading()
	r.revision = revision
	r.letGo = make(map[string]secretref.Key, len(optional))
	for _, k := range optional {
		r.letGo[StoreName(k)] = k
	}
	return s.withReadings(r)
}

// Pending is s recording the reading of a document the caller is about to
// write, which lets go without the Secret keys optional, by hash, the hash the
// caller gives that document, which must tell it from every other document it
// writes: should a read find the revision the engine stored for the write
// running before the caller has recorded its activation (its answer lost, or
// the caller's record of it), Confirmed records that revision's reading from
// it. The caller records it, and saves s, before the write is sent, since a
// write whose answer is lost leaves nothing else to say how its document reads
// the store.
func (s State) Pending(hash string, optional []secretref.Key) State {
	letGo := make(map[string]secretref.Key, len(optional))
	for _, k := range optional {
		letGo[StoreName(k)] = k
	}
	pending := maps.Clone(s.pending())
	if pending == nil {
		pending = map[string]map[string]secretref.Key{}
	}
	pending[hash] = letGo
	r := s.reading()
	r.pending = pending
	return s.withReadings(r)
}

// Pends reports whether s records the reading of the document hash names
// (Pending).
func (s State) Pends(hash string) bool {
	_, ok := s.pending()[hash]
	return ok
}

// Confirmed is s recording revision, which the caller found running the
// document whose reading Pending recorded under hash, as reading the store
// that way, as Adopted records a revision of a document the caller renders:
// without settling a withdrawal or a write, which only a read of the epoch the
// revision activated as settles (Ran). ok is false where s records no reading
// under hash, and s is returned unchanged.
func (s State) Confirmed(revision, hash string) (next State, ok bool) {
	letGo, ok := s.pending()[hash]
	if !ok || revision == "" {
		return s, false
	}
	r := s.reading()
	r.revision, r.letGo = revision, letGo
	return s.withReadings(r), true
}

// KeepPending is s recording only the readings Pending recorded under hashes:
// the caller's record of the rest says no revision it has not recorded can be
// that write.
func (s State) KeepPending(hashes []string) State {
	var kept map[string]map[string]secretref.Key
	for _, hash := range hashes {
		if letGo, ok := s.pending()[hash]; ok {
			if kept == nil {
				kept = map[string]map[string]secretref.Key{}
			}
			kept[hash] = letGo
		}
	}
	r := s.reading()
	r.pending = kept
	return s.withReadings(r)
}

// Recorded is the revision whose reading s records, empty for none.
func (s State) Recorded() string {
	return s.reading().revision
}

// Records reports whether s records how revision reads the store: whether
// LetGo answers for it.
func (s State) Records(revision string) bool {
	return revision != "" && revision == s.reading().revision
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
	return slices.Sorted(maps.Keys(s.reading().letGo))
}

// LetGoKeys are the Secret keys LetGo names the store entries of, sorted as
// secretref.Compare sorts them: those whose Secret or key is gone are the
// values the revision goes without, whatever the next document reads of
// them (Desired.Release).
func (s State) LetGoKeys(revision string) []secretref.Key {
	if !s.Records(revision) {
		return nil
	}
	return slices.SortedFunc(maps.Values(s.reading().letGo), secretref.Compare)
}

// letsGo reports whether the document of revision lets go without name, as
// LetGo says.
func (s State) letsGo(revision, name string) bool {
	_, ok := s.reading().letGo[name]
	return ok && s.Records(revision)
}

// Retained are the names the last Sync or Withdraw left in the store,
// although the optional reference each stands for has no Secret or key any
// more, because the revision the engine runs reads them as required, or is
// one whose reading the State does not record (Result.Retained), sorted. The
// caller compares them with the Retained of the State it started from, to
// tell a name it newly keeps from one it has already said it keeps.
func (s State) Retained() []string {
	return slices.Sorted(maps.Keys(s.retained))
}

// withdraw records name as withdrawn in s, which it changes in place: a
// change no read has followed, whatever an earlier withdrawal of it waited
// past.
func (s *State) withdraw(name string) {
	if s.withdrawn == nil {
		s.withdrawn = map[string]int64{}
	}
	s.withdrawn[name] = unread
	s.changedAgain(name)
}

// write records a value written under name in s, which it changes in place,
// as withdraw records a withdrawal.
func (s *State) write(name string) {
	if s.written == nil {
		s.written = map[string]int64{}
	}
	s.written[name] = unread
	s.changedAgain(name)
}

// changedAgain records in s, which it changes in place, that name changed
// after every activation Awaiting recorded for it: none of them read it now.
func (s *State) changedAgain(name string) {
	if _, ok := s.awaits[name]; ok {
		awaits := maps.Clone(s.awaits)
		delete(awaits, name)
		if len(awaits) == 0 {
			awaits = nil
		}
		s.awaits = awaits
	}
}

// deleted records name removed from the store in s, which it changes in
// place: what was written under it is no value any revision reads now, and
// a withdrawal, where it is one, is what an activation owes for it.
func (s *State) deleted(name string) {
	delete(s.fingerprints, name)
	delete(s.written, name)
}

// carried is a State that starts from s: its key, its record of withdrawals,
// of writes and the revisions they await, and of how documents read the
// store, and what it last retained, in maps of its own where a Sync or a
// Withdraw changes them in place, and fingerprints.
func (s State) carried(fingerprints map[string][]byte) State {
	return State{
		key: s.key, fingerprints: fingerprints, withdrawn: maps.Clone(s.withdrawn), written: maps.Clone(s.written),
		awaits: s.awaits, readings: s.readings, retained: s.retained,
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
	// and no entry of it can be the operator's to have withdrawn, written,
	// let go or retained.
	s.withdrawn = markLines(secret.Data[withdrawnField])
	s.written = markLines(secret.Data[writtenField])
	s.awaits = awaitLines(secret.Data[awaitField])
	s.awaits = s.awaited()
	s.retained = ownedLines(secret.Data[retainedField])
	return s.withReadings(readingRecord{
		revision: string(secret.Data[revisionField]), letGo: keyLines(secret.Data[letGoField]),
		pending: pendingOf(secret.Data[pendingField]),
	}), nil
}

// pendingRecord is one reading State.Pending recorded, as the state Secret
// holds it: Secret keys as secretref.Key prints them.
type pendingRecord struct {
	Optional []string `json:"optional"`
}

// pendingOf reads the readings State.Pending recorded from data, nil for none.
// What does not decode is dropped, as a malformed line of the other fields
// is: only a hand edit writes it, and the cost is the one State.Confirmed
// would have saved, a revision read as requiring every name it references.
func pendingOf(data []byte) map[string]map[string]secretref.Key {
	var records map[string]pendingRecord
	if json.Unmarshal(data, &records) != nil {
		return nil
	}
	var pending map[string]map[string]secretref.Key
	for hash, r := range records {
		if hash == "" {
			continue
		}
		letGo := map[string]secretref.Key{}
		for _, line := range r.Optional {
			if k, ok := parseKey(line); ok {
				letGo[StoreName(k)] = k
			}
		}
		if pending == nil {
			pending = map[string]map[string]secretref.Key{}
		}
		pending[hash] = letGo
	}
	return pending
}

// pendingData is s's pending readings as the state Secret holds them, keyed
// by hash, and nothing for none, as the other lists are; encoding/json writes
// the keys sorted, so a State saves the same bytes whatever order its maps
// hold them in.
func (s State) pendingData() []byte {
	if len(s.pending()) == 0 {
		return []byte{}
	}
	records := make(map[string]pendingRecord, len(s.pending()))
	for hash, letGo := range s.pending() {
		keys := slices.SortedFunc(maps.Values(letGo), secretref.Compare)
		optional := make([]string, len(keys))
		for i, k := range keys {
			optional[i] = k.String()
		}
		records[hash] = pendingRecord{Optional: optional}
	}
	data, err := json.Marshal(records)
	if err != nil {
		// A map of strings to a struct of strings always encodes; were it
		// ever not to, nothing pending is the State's zero, which costs a
		// revision read as requiring every name and nothing else.
		return []byte{}
	}
	return data
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

// markLines are the lines of data that are names Owns accepts, each with the
// epoch after it, nil for none. A name with no epoch after it, or one that is
// not a number no lower than zero, is unread: only a hand edit writes the
// second, and reading it as an epoch could settle a change no activation
// read.
func markLines(data []byte) map[string]int64 {
	var marks map[string]int64
	for line := range strings.Lines(string(data)) {
		name, epoch, read := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		if !Owns(name) {
			continue
		}
		mark := int64(unread)
		if n, err := strconv.ParseInt(epoch, 10, 64); read && err == nil && n >= 0 {
			mark = n
		}
		if marks == nil {
			marks = map[string]int64{}
		}
		marks[name] = mark
	}
	return marks
}

// marked is marks one per line, sorted by name, each followed by its epoch
// where a read has followed it, never nil.
func marked(marks map[string]int64) []byte {
	out := []byte{}
	for _, name := range slices.Sorted(maps.Keys(marks)) {
		out = append(out, name...)
		if mark := marks[name]; mark != unread {
			out = append(out, ' ')
			out = strconv.AppendInt(out, mark, 10)
		}
		out = append(out, '\n')
	}
	return out
}

// awaitLines are the lines of data that are a name Owns accepts and a
// revision, the revisions of each name sorted, nil for none. A line that is
// not one is dropped: only a hand edit writes it, and the cost is the one a
// lost record of an unconfirmed activation costs, an activation asked for
// again.
func awaitLines(data []byte) map[string][]string {
	var awaits map[string][]string
	for line := range strings.Lines(string(data)) {
		name, revision, ok := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		if !ok || !Owns(name) || revision == "" || strings.ContainsAny(revision, " \t") ||
			slices.Contains(awaits[name], revision) {
			continue
		}
		if awaits == nil {
			awaits = map[string][]string{}
		}
		awaits[name] = append(awaits[name], revision)
	}
	for name := range awaits {
		slices.Sort(awaits[name])
	}
	return awaits
}

// awaitedLines is awaits one change and one revision per line, sorted by
// name and then revision, never nil.
func awaitedLines(awaits map[string][]string) []byte {
	out := []byte{}
	for _, name := range slices.Sorted(maps.Keys(awaits)) {
		for _, revision := range awaits[name] {
			out = append(out, name+" "+revision+"\n"...)
		}
	}
	return out
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
	data := make(map[string][]byte, len(s.fingerprints)+7)
	maps.Copy(data, s.fingerprints)
	if len(s.key) > 0 {
		data[fingerprintKeyField] = []byte(s.key)
	}
	// Written when they are empty too, so this field manager owns them: a
	// list it left out would stay where a hand edit put it, and every
	// reconcile would activate a revision for names it withdrew or wrote
	// long ago, or take a revision whose reading it does not know for one
	// that lets a name go.
	data[withdrawnField] = marked(s.withdrawn)
	data[writtenField] = marked(s.written)
	data[awaitField] = awaitedLines(s.awaited())
	reading := s.reading()
	data[revisionField] = []byte(reading.revision)
	keys := slices.SortedFunc(maps.Values(reading.letGo), secretref.Compare)
	printed := make([]string, len(keys))
	for i, k := range keys {
		printed[i] = k.String()
	}
	data[letGoField] = lines(printed)
	data[retainedField] = lines(s.Retained())
	data[pendingField] = s.pendingData()
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
