package secretsync

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// TestSyncAgainstTheEngine runs Sync through *engine.Client against the real
// engine, so the fake the other tests script cannot drift from it: the
// engine must accept every name StoreName produces, record the credential
// each write authenticated with, list it back, and leave entries the operator
// does not own alone. The engine is the one judge of who wrote an entry, so
// this is also where a write by another credential with the same grant — the
// company's access token — has to be seen for what it is.
func TestSyncAgainstTheEngine(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	e := enginetest.Start(t)
	eng := e.Client()
	other := engine.New(e.URL, e.AccessToken)
	self := engine.TokenLogin(enginetest.OperatorTokenID)

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
	if err := eng.PutSecret(ctx, stale, []byte("old")); err != nil {
		t.Fatalf("seeding %s: %v", stale, err)
	}
	if err := other.PutSecret(ctx, foreignName, []byte("glpat")); err != nil {
		t.Fatalf("seeding %s: %v", foreignName, err)
	}

	desired := desiredOf(map[string]string{slack: "xoxb", signing: "sig"})
	state, res, err := Sync(ctx, eng, desired, State{}, self)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if want := slices.Sorted(slices.Values([]string{slack, signing})); !slices.Equal(res.Put, want) ||
		!slices.Equal(res.Deleted, []string{stale}) || len(res.Overwritten) != 0 {
		t.Fatalf("Result = %+v, want Put %q and Deleted [%s]", res, want, stale)
	}

	writers := func() map[string]string {
		t.Helper()
		listing, err := eng.ListSecrets(ctx)
		if err != nil {
			t.Fatalf("ListSecrets: %v", err)
		}
		out := map[string]string{}
		for _, info := range listing {
			out[info.Name] = info.OperatorID
		}
		return out
	}
	access := engine.TokenLogin(enginetest.AccessTokenID)
	if got, want := writers(), map[string]string{slack: self, signing: self, foreignName: access}; !maps.Equal(got, want) {
		t.Fatalf("the engine lists writers %v, want %v", got, want)
	}

	// The engine's listing is all a second Sync has to go on besides the
	// state, so this is where a mismatch in how the client reports the
	// writer would show up as a rewrite on every reconcile.
	state, res, err = Sync(ctx, eng, desired, state, self)
	if err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if len(res.Put) != 0 || len(res.Deleted) != 0 {
		t.Fatalf("a second Sync of the same set changed the store: %+v", res)
	}

	// Somebody with the same grant writes over an entry the operator owns.
	// Nothing in the request can make the engine record the operator as its
	// writer, so the next Sync puts the operator's value back and says who
	// had replaced it.
	if err := other.PutSecret(ctx, slack, []byte("typed-by-hand")); err != nil {
		t.Fatalf("overwriting %s: %v", slack, err)
	}
	_, res, err = Sync(ctx, eng, desired, state, self)
	if err != nil {
		t.Fatalf("third Sync: %v", err)
	}
	if want := []Overwrite{{Name: slack, By: access, Credential: access}}; !slices.Equal(res.Put, []string{slack}) ||
		!slices.Equal(res.Overwritten, want) {
		t.Fatalf("Result = %+v, want %s written back and reported as overwritten by %s", res, slack, access)
	}
	if got := writers()[slack]; got != self {
		t.Fatalf("%s is listed as written by %s after the Sync, want %s", slack, got, self)
	}
}

// TestContractStoreRefusesWhatResolveRefuses holds MaxValueBytes to the
// engine's store: a value of exactly that size is stored, and one a
// byte larger is refused. Resolve refuses the larger value before the engine
// is asked, which holds the company's document (SecretTooLarge) on the
// engine's word (ADR-0005), so a store that took more would leave the
// operator holding a company the engine would run.
func TestContractStoreRefusesWhatResolveRefuses(t *testing.T) {
	t.Parallel()
	eng := enginetest.Start(t).Client()
	name := StoreName(secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey})
	if err := eng.PutSecret(t.Context(), name, bytes.Repeat([]byte("a"), MaxValueBytes)); err != nil {
		t.Errorf("the store refuses a value of MaxValueBytes: %v", err)
	}
	if err := eng.PutSecret(t.Context(), name, bytes.Repeat([]byte("a"), MaxValueBytes+1)); err == nil {
		t.Error("the store takes a value a byte over MaxValueBytes, which Resolve refuses")
	}
}
