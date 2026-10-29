package secretsync

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// EngineSecrets is the part of the engine's API Sync needs: the /secrets
// routes. *engine.Client implements it; Sync takes the interface so its
// tests can script an engine that fails between two writes, which the real
// one cannot be made to do on cue.
type EngineSecrets interface {
	// ListSecrets lists every entry in the store (GET /secrets).
	ListSecrets(ctx context.Context) ([]engine.SecretInfo, error)
	// PutSecrets stores or replaces up to engine.MaxSecretBatch values, each
	// UTF-8, in one request (PUT /secrets), and the engine re-activates the
	// active revision once if it reads any of them, unless deferReload asks
	// it to re-activate nothing. The engine records the credential that
	// wrote them (SecretInfo.OperatorID).
	PutSecrets(ctx context.Context, values map[string][]byte, deferReload bool) (engine.SecretsWritten, error)
	// WriteSecret stores or replaces one value as raw bytes
	// (PUT /secrets/{name}), for a value that is not UTF-8, which a batch's
	// JSON body cannot carry, and the engine re-activates the active
	// revision if it reads the name, unless deferReload asks it not to.
	WriteSecret(ctx context.Context, name string, value []byte, deferReload bool) (engine.SecretsWritten, error)
	// DeleteSecret removes one entry, reporting whether it existed
	// (DELETE /secrets/{name}), and the engine re-activates the active
	// revision if it reads the name. Removing an absent entry is not an
	// error.
	DeleteSecret(ctx context.Context, name string) (engine.SecretRemoved, error)
}

// The controller hands Sync the same *engine.Client it applies documents
// with, so the interface must not drift away from the client's methods.
var _ EngineSecrets = (*engine.Client)(nil)

// Result names what one Sync changed in the store, each list sorted.
type Result struct {
	// Put are the names whose value Sync wrote.
	Put []string
	// Deleted are the names Sync removed.
	Deleted []string
	// Overwritten are the names in Put the engine listed as last written by
	// a credential other than the operator's: somebody replaced a value the
	// operator owns, and Sync wrote the operator's back.
	Overwritten []Overwrite
	// Retained are the absent names (Desired.Missing) the engine lists that
	// were left in the store, holding the value their Secret had, because
	// a revision kept reads them as required, or is one whose reading the
	// State does not record (Desired.Keep, Desired.KeepServed), or a node
	// runs a revision the operator does not know (Desired.KeepUnread).
	Retained []string
	// Held are the names the engine lists that were left in the store only
	// for a revision some engine node runs or starts on other than the
	// fleet's (Desired.KeepServed), or for a node whose revisions the
	// operator does not know (Desired.KeepUnread): were every node running
	// the fleet's revision and starting on it or on none, each would have
	// been deleted. Each goes once no node runs or starts on a revision that
	// needs it, which only a later Sync or Withdraw can find.
	Held []string
	// Reload is what the engine's re-activation of its active revision did
	// at the last request of this Sync, or Withdraw, that wrote or removed
	// a value that revision reads (engine.Reload.Landed says whether it
	// activated a revision): the engine re-activates the revision whenever
	// a request changes a value it reads, so that one read the store as the
	// Sync left it, for every value the revision reads. A request that
	// changed only values the revision does not read made none, and changes
	// nothing here. Nil when no request changed a value the revision reads:
	// the caller then activates the revision itself where it has to. A
	// deferred answer (engine.Reload.Deferred) and a null one never replace
	// a re-activation an earlier request of the same Sync made.
	Reload *engine.Reload
	// ReloadAction is the engine call whose request made Reload:
	// ActionWriteSecret, ActionPutSecrets or ActionDeleteSecret, which is
	// how the operator says what activated the revision. Empty with Reload
	// nil.
	ReloadAction string
}

// The engine calls a Sync or Withdraw makes that re-activate the revision
// (Result.ReloadAction), named as the client's methods are.
const (
	ActionWriteSecret  = "WriteSecret"
	ActionPutSecrets   = "PutSecrets"
	ActionDeleteSecret = "DeleteSecret"
)

// Overwrite is one owned entry somebody other than the operator wrote.
type Overwrite struct {
	// Name is the store name.
	Name string
	// By is who wrote it, as the engine records the author
	// (SecretInfo.UpdatedBy).
	By string
	// Credential is what that write authenticated with
	// (SecretInfo.OperatorID).
	Credential string
}

// Sync makes the engine's secret store hold desired, given prev, what the
// operator last pushed into it, and returns what the store now holds. self is
// the credential the engine records for this operator's writes, as it lists
// it in SecretInfo.OperatorID: engine.TokenLogin of the operator's own token.
//
// It writes a desired name when its value differs from the one prev
// recorded, when the engine does not list it (the engine's data was lost or
// someone deleted the entry), or when the engine lists it as written by a
// credential other than self (someone else wrote over it; Kubernetes is the
// source of truth for every owned name). Who wrote an entry is the engine's
// record of the authenticated credential, never a word the writer chose:
// any caller can say anything about itself, and the operator would then take
// a value somebody else put there for its own and never notice. Then it
// deletes every name the engine lists that Owns accepts and desired neither
// pushes nor keeps, every absent name (Desired.Missing) but one a revision
// kept reads as required (Desired.Keep, Desired.KeepServed), which it leaves
// (Result.Retained, State.Retained), and every name a revision kept lets go
// whose Secret or key is gone (Desired.Release) that no other revision kept
// reads as required; it records each absent or released name it deleted as
// withdrawn (State.Withdrawn). While a node may run or start on a revision the
// operator does not know (Desired.KeepUnread) it deletes nothing, and what it
// keeps for any revision but the fleet's it reports (Result.Held). Writes
// come first and deletes after them, so an engine never misses a name the
// next document references because a delete failed first; a failed write
// stops the sync before any delete.
//
// The values are written together, in requests of up to
// engine.MaxSecretBatch names in name order (PutSecrets), because the engine
// re-activates its active revision at every write of a value it reads: one
// request per value would be one activation per value, the company running a
// mix of the old and the new values between them. A value that is not UTF-8
// is written alone, before them (WriteSecret), since a JSON body cannot carry
// it unchanged. Every write request but the last asks the engine to
// re-activate nothing (deferReload), and the last re-activates the revision
// once for every value the sync wrote, which Result.Reload reports: a sync is
// one activation however many requests carry it. Where the last request
// re-activates nothing, because the revision reads none of its names, or its
// re-activation fails, Result.Reload says so and the caller activates the
// revision itself. A value written deferred is recorded as written (a
// fingerprint in the State) only once the last write request has been
// answered: a sync that fails, or stops, before that leaves it to be written
// again by the next, whose last request activates it. The engine re-activates
// the revision at the removal of a value it reads too, one removal at a time,
// which is what Result.Reload reports where one comes last.
//
// A Desired that Resolve returned with an error pushes nothing, and Sync
// refuses it rather than delete every entry.
//
// Every step is checked against the engine's listing rather than prev alone,
// so Sync is idempotent: a second Sync with the same arguments writes and
// deletes nothing. When a step fails, Sync returns the state the store is
// known to be in up to that point, with the error, and the caller saves that
// state: a retry from it repeats only what is not known to have happened.
// A name whose write failed has no fingerprint in it, because the engine may
// or may not have stored the value.
func Sync(ctx context.Context, eng EngineSecrets, desired Desired, prev State, self string) (State, Result, error) {
	var res Result
	if desired.partial {
		return prev, res, errors.New("syncing the engine's secret store: the values were not all read, " +
			"and a sync of the rest would delete every entry it does not push")
	}
	listing, err := eng.ListSecrets(ctx)
	if err != nil {
		return prev, res, fmt.Errorf("listing the engine's secret store: %w", err)
	}
	listed := make(map[string]engine.SecretInfo, len(listing))
	for _, info := range listing {
		listed[info.Name] = info
	}

	// Withdrawn until a revision activates, whatever this sync pushes: the
	// engine runs what it read before the withdrawal, a value pushed again
	// under the name included.
	next := prev.carried(make(map[string][]byte, len(desired.values)))
	if len(next.key) == 0 {
		next.key = newFingerprintKey()
	}
	// An absent name the operator pushed and the engine no longer lists was
	// removed by a run that stopped before it could save that it had.
	for _, name := range desired.goneNames() {
		_, pushed := prev.fingerprints[name]
		_, ok := listed[name]
		if _, removed := desired.gone(name, prev); pushed && removed && !ok {
			next.withdraw(name)
		}
	}
	// A fingerprint is a record of what the engine holds, so one for a name
	// the engine no longer lists records nothing.
	for name, fp := range prev.fingerprints {
		if _, ok := listed[name]; ok {
			next.fingerprints[name] = fp
		}
	}

	if err := push(ctx, eng, desired, listed, &next, self, &res); err != nil {
		return next, res, err
	}

	var retained []string
	alone := desired.fleetsAlone()
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !Owns(name) {
			continue
		}
		if _, ok := desired.values[name]; ok {
			continue
		}
		absent, _ := desired.gone(name, prev)
		if !desired.deletes(name, prev) {
			if absent {
				retained = append(retained, name)
			}
			if alone.deletes(name, prev) {
				res.Held = append(res.Held, name)
			}
			continue
		}
		// Whether the entry still existed does not matter: either way it is
		// gone now, which is all the sync needs.
		removed, err := eng.DeleteSecret(ctx, name)
		if err != nil {
			what := "which nothing references"
			if absent {
				what = "whose Secret or key does not exist"
			}
			return next, res, fmt.Errorf("deleting %s, %s, from the engine's secret store: %w", name, what, err)
		}
		res.removed(removed)
		delete(next.fingerprints, name)
		res.Deleted = append(res.Deleted, name)
		if absent {
			next.withdraw(name)
		}
	}
	res.Retained, next.retained = retained, nameSet(retained)
	return next, res, nil
}

// push writes every value of desired that the engine does not hold as the
// operator last wrote it, as Sync says, recording each written in next and
// in res, with the engine's last re-activation; a failed write stops it.
func push(
	ctx context.Context, eng EngineSecrets, desired Desired, listed map[string]engine.SecretInfo, next *State,
	self string, res *Result,
) error {
	var single, batched []string
	fingerprints := map[string][]byte{}
	for _, name := range desired.Names() {
		value := desired.values[name]
		fp := next.fingerprint(name, value)
		info, ok := listed[name]
		if ok && info.OperatorID == self && hmac.Equal(next.fingerprints[name], fp) {
			continue
		}
		// Until the write is answered the engine may or may not hold it.
		delete(next.fingerprints, name)
		fingerprints[name] = fp
		if utf8.Valid(value) {
			batched = append(batched, name)
		} else {
			single = append(single, name)
		}
	}
	wrote := func(name string) {
		next.fingerprints[name] = fingerprints[name]
		res.Put = append(res.Put, name)
		if info, ok := listed[name]; ok && info.OperatorID != self {
			res.Overwritten = append(res.Overwritten, Overwrite{Name: name, By: info.UpdatedBy, Credential: info.OperatorID})
		}
	}
	type writeRequest struct {
		names []string
		alone bool
	}
	var requests []writeRequest
	for _, name := range single {
		requests = append(requests, writeRequest{names: []string{name}, alone: true})
	}
	for batch := range slices.Chunk(batched, engine.MaxSecretBatch) {
		requests = append(requests, writeRequest{names: batch})
	}
	// WRITTEN DEFERRED, recorded once the last request is answered: until
	// then nothing activates them, and a sync that stops leaves them for
	// the next to write again.
	var deferred []string
	for i, request := range requests {
		last := i == len(requests)-1
		var written engine.SecretsWritten
		var err error
		action := ActionPutSecrets
		if request.alone {
			action = ActionWriteSecret
			name := request.names[0]
			written, err = eng.WriteSecret(ctx, name, desired.values[name], !last)
		} else {
			values := make(map[string][]byte, len(request.names))
			for _, name := range request.names {
				values[name] = desired.values[name]
			}
			written, err = eng.PutSecrets(ctx, values, !last)
		}
		if err != nil {
			sortWritten(res)
			return fmt.Errorf("writing %s into the engine's secret store: %w", strings.Join(request.names, ", "), err)
		}
		if !last {
			deferred = append(deferred, request.names...)
			continue
		}
		for _, name := range append(deferred, request.names...) {
			wrote(name)
		}
		res.written(written, action)
	}
	sortWritten(res)
	return nil
}

// removed records in res the engine's answer to a removal: its
// re-activation, where it made one (adopt).
func (res *Result) removed(answer engine.SecretRemoved) {
	res.adopt(answer.Reload, ActionDeleteSecret)
}

// written records in res the engine's answer to a write, which action made:
// its re-activation, where it made one (adopt).
func (res *Result) written(answer engine.SecretsWritten, action string) {
	res.adopt(answer.Reload, action)
}

// adopt takes reload as the sync's last re-activation, made by action, where
// the engine made one: never a null answer (a request that re-activated
// nothing, or whose re-activation the engine did not say) or a deferred one,
// either of which would wipe what an earlier request of the same sync
// activated and cost the company an activation it already had.
func (res *Result) adopt(reload *engine.Reload, action string) {
	if reload == nil || reload.Deferred {
		return
	}
	res.Reload, res.ReloadAction = reload, action
}

// sortWritten sorts what res says was written by name, as Result promises:
// a value written alone comes before the batches.
func sortWritten(res *Result) {
	slices.Sort(res.Put)
	slices.SortFunc(res.Overwritten, func(a, b Overwrite) int { return strings.Compare(a.Name, b.Name) })
}

// Withdraw removes from the engine's store the entry of every absent name of
// desired (Desired.Missing) that it lists, but one a revision kept reads as
// required (Desired.Keep, Desired.KeepServed), and of every name a revision
// kept lets go whose Secret or key is gone (Desired.Release) that no other
// revision kept reads as required, and records each it removed as withdrawn
// (State.Withdrawn) in the State it returns, with what it did:
// Result.Deleted, the names it removed, Result.Retained, the absent names it
// left, and Result.Held, those of them it left only for a revision other
// than the fleet's, each sorted, and Result.Reload, the engine's
// re-activation at the last removal of a value the active revision reads.
// It is the part of Sync that holds while
// nothing else may be pushed or deleted: a document that cannot be applied
// leaves the store as the running revision needs it, every name it requires
// included, but the value of a reference whose Secret is gone that the
// running revision lets go is one it goes without, and one no engine may
// start anything with. desired may be one Resolve returned with an error.
//
// A step that fails returns the State known up to it, with the error, for
// the caller to save, as Sync does.
func Withdraw(ctx context.Context, eng EngineSecrets, desired Desired, prev State) (State, Result, error) {
	var res Result
	gone := desired.goneNames()
	if len(gone) == 0 {
		// Nothing absent, so nothing to retain either.
		next := prev.carried(prev.fingerprints)
		next.retained = nil
		return next, res, nil
	}
	listing, err := eng.ListSecrets(ctx)
	if err != nil {
		return prev, res, fmt.Errorf("listing the engine's secret store: %w", err)
	}
	next := prev.carried(maps.Clone(prev.fingerprints))
	listed := make(map[string]bool, len(listing))
	var removing []string
	alone := desired.fleetsAlone()
	for _, info := range listing {
		listed[info.Name] = true
		absent, removed := desired.gone(info.Name, prev)
		switch {
		case !absent || !Owns(info.Name):
		case removed:
			removing = append(removing, info.Name)
		default:
			res.Retained = append(res.Retained, info.Name)
			if _, alsoRemoved := alone.gone(info.Name, prev); alsoRemoved {
				res.Held = append(res.Held, info.Name)
			}
		}
	}
	slices.Sort(res.Retained)
	slices.Sort(res.Held)
	slices.Sort(removing)
	next.retained = nameSet(res.Retained)
	// As in Sync: an absent name the operator pushed and the engine no
	// longer lists was removed by a run that stopped before saving it.
	for _, name := range gone {
		_, pushed := prev.fingerprints[name]
		if _, removed := desired.gone(name, prev); pushed && removed && !listed[name] {
			delete(next.fingerprints, name)
			next.withdraw(name)
		}
	}
	for _, name := range removing {
		removed, err := eng.DeleteSecret(ctx, name)
		if err != nil {
			return next, res, fmt.Errorf("deleting %s, whose Secret or key does not exist, from the engine's secret store: %w",
				name, err)
		}
		res.removed(removed)
		delete(next.fingerprints, name)
		next.withdraw(name)
		res.Deleted = append(res.Deleted, name)
	}
	return next, res, nil
}
