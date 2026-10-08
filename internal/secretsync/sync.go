package secretsync

import (
	"context"
	"crypto/hmac"
	"fmt"
	"maps"
	"slices"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// Source is the provenance the operator records on every entry it writes
// (PUT /secrets/{name}?source=operator). An owned entry the engine lists
// with any other source was last written by someone else.
const Source = "operator"

// EngineSecrets is the part of the engine's API Sync needs: the /secrets
// routes. *engine.Client implements it; Sync takes the interface so its
// tests can script an engine that fails between two writes, which the real
// one cannot be made to do on cue.
type EngineSecrets interface {
	// ListSecrets lists every entry in the store (GET /secrets).
	ListSecrets(ctx context.Context) ([]engine.SecretInfo, error)
	// PutSecret stores or replaces one value (PUT /secrets/{name}).
	PutSecret(ctx context.Context, name string, value []byte, source string) error
	// DeleteSecret removes one entry, reporting whether it existed
	// (DELETE /secrets/{name}). Removing an absent entry is not an error.
	DeleteSecret(ctx context.Context, name string) (removed bool, err error)
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
}

// Sync makes the engine's secret store hold desired, given prev, what the
// operator last pushed into it, and returns what the store now holds.
//
// It writes a desired name when its value differs from the one prev
// recorded, when the engine does not list it (the engine's data was lost or
// someone deleted the entry), or when the engine lists it with a source other
// than Source (someone else wrote over it; Kubernetes is the source of truth
// for every owned name). Then it deletes every name the engine lists that
// Owns accepts and desired neither pushes nor keeps. Writes come first, in
// name order, and deletes after them, so an engine never misses a name the
// next document references because a delete failed first; a failed write
// stops the sync before any delete.
//
// Every step is checked against the engine's listing rather than prev alone,
// so Sync is idempotent: a second Sync with the same arguments writes and
// deletes nothing. When a step fails, Sync returns the state the store is
// known to be in up to that point, with the error, and the caller saves that
// state: a retry from it repeats only what is not known to have happened.
// A name whose write failed has no fingerprint in it, because the engine may
// or may not have stored the value.
func Sync(ctx context.Context, eng EngineSecrets, desired Desired, prev State) (State, Result, error) {
	var res Result
	listing, err := eng.ListSecrets(ctx)
	if err != nil {
		return prev, res, fmt.Errorf("listing the engine's secret store: %w", err)
	}
	listed := make(map[string]string, len(listing))
	for _, info := range listing {
		listed[info.Name] = info.Source
	}

	next := State{key: prev.key, fingerprints: make(map[string][]byte, len(desired.values))}
	if len(next.key) == 0 {
		next.key = newFingerprintKey()
	}
	// A fingerprint is a record of what the engine holds, so one for a name
	// the engine no longer lists records nothing.
	for name, fp := range prev.fingerprints {
		if _, ok := listed[name]; ok {
			next.fingerprints[name] = fp
		}
	}

	for _, name := range desired.Names() {
		value := desired.values[name]
		fp := next.fingerprint(name, value)
		if source, ok := listed[name]; ok && source == Source && hmac.Equal(next.fingerprints[name], fp) {
			continue
		}
		delete(next.fingerprints, name)
		if err := eng.PutSecret(ctx, name, value, Source); err != nil {
			return next, res, fmt.Errorf("writing %s into the engine's secret store: %w", name, err)
		}
		next.fingerprints[name] = fp
		res.Put = append(res.Put, name)
	}

	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !Owns(name) {
			continue
		}
		if _, ok := desired.values[name]; ok {
			continue
		}
		if _, ok := desired.kept[name]; ok {
			continue
		}
		// Whether the entry still existed does not matter: either way it is
		// gone now, which is all the sync needs.
		if _, err := eng.DeleteSecret(ctx, name); err != nil {
			return next, res, fmt.Errorf("deleting %s, which nothing references, from the engine's secret store: %w", name, err)
		}
		delete(next.fingerprints, name)
		res.Deleted = append(res.Deleted, name)
	}
	return next, res, nil
}
