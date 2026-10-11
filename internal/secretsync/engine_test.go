package secretsync

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// TestSyncAgainstTheEngine runs Sync through *engine.Client against the real
// pinned engine, so the fake the other tests script cannot drift from it:
// the engine must accept every name StoreName produces, record the
// operator's provenance, list it back, and leave entries the operator does
// not own alone.
func TestSyncAgainstTheEngine(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	eng := enginetest.Start(t).Client()

	// Names from references shaped like real ones: a dashed Secret name, a
	// camelCase key, and one long enough to be truncated.
	slack := StoreName(secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey})
	signing := StoreName(secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: signingSecretKey})
	stale := StoreName(secretref.Key{
		Namespace: nimbusNS,
		Name:      "a-secret-whose-name-is-longer-than-the-readable-part-allows",
		Key:       apiKeyKey,
	})

	// What a previous sync left, and what somebody else wrote.
	if err := eng.PutSecret(ctx, stale, []byte("old"), Source); err != nil {
		t.Fatalf("seeding %s: %v", stale, err)
	}
	if err := eng.PutSecret(ctx, foreignName, []byte("glpat"), "provision"); err != nil {
		t.Fatalf("seeding %s: %v", foreignName, err)
	}

	desired := desiredOf(map[string]string{slack: "xoxb", signing: "sig"})
	state, res, err := Sync(ctx, eng, desired, State{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if want := slices.Sorted(slices.Values([]string{slack, signing})); !slices.Equal(res.Put, want) ||
		!slices.Equal(res.Deleted, []string{stale}) {
		t.Fatalf("Result = %+v, want Put %q and Deleted [%s]", res, want, stale)
	}

	listing, err := eng.ListSecrets(ctx)
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	sources := map[string]string{}
	for _, info := range listing {
		sources[info.Name] = info.Source
	}
	want := map[string]string{slack: Source, signing: Source, foreignName: "provision"}
	if !maps.Equal(sources, want) {
		t.Fatalf("the engine lists %v, want %v", sources, want)
	}

	// The engine's listing is all a second Sync has to go on besides the
	// state, so this is where a mismatch in how the client reports the
	// provenance would show up as a rewrite on every reconcile.
	_, res, err = Sync(ctx, eng, desired, state)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(res.Put) != 0 || len(res.Deleted) != 0 {
		t.Fatalf("a second Sync of the same set changed the store: %+v", res)
	}
}

// TestContractStoreRefusesWhatResolveRefuses holds MaxValueBytes to the
// pinned engine's store: a value of exactly that size is stored, and one a
// byte larger is refused. Resolve refuses the larger value before the engine
// is asked, which holds the company's document (SecretTooLarge) on the
// engine's word (ADR-0005), so a store that took more would leave the
// operator holding a company the engine would run.
func TestContractStoreRefusesWhatResolveRefuses(t *testing.T) {
	t.Parallel()
	eng := enginetest.Start(t).Client()
	name := StoreName(secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey})
	if err := eng.PutSecret(t.Context(), name, bytes.Repeat([]byte("a"), MaxValueBytes), Source); err != nil {
		t.Errorf("the store refuses a value of MaxValueBytes: %v", err)
	}
	if err := eng.PutSecret(t.Context(), name, bytes.Repeat([]byte("a"), MaxValueBytes+1), Source); err == nil {
		t.Error("the store takes a value a byte over MaxValueBytes, which Resolve refuses")
	}
}
