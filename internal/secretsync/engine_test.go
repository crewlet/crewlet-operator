package secretsync

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

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

// TestSyncKeepsWhatARefusedRevisionRuns runs the case KeepServed exists for
// against the real engine: the fleet's revision drops a value the revision
// before it references and is refused, so the node runs that revision still,
// says it would start on it (Boots), and boots on it when it restarts. A sync
// for the fleet's revision that keeps the served one's names leaves the value,
// and the node restarts onto it; one that keeps only the fleet's deletes it,
// and the node no longer starts.
func TestSyncKeepsWhatARefusedRevisionRuns(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	e := enginetest.Start(t)
	eng := e.Client()
	self := engine.TokenLogin(enginetest.OperatorTokenID)
	served := StoreName(secretref.Key{Namespace: nimbusNS, Name: "nimbus-e2b", Key: apiKeyKey})

	state, _, err := Sync(ctx, eng, desiredOf(map[string]string{served: enginetest.SandboxKeyValue}), State{}, self)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	p, err := eng.ApplyConfig(ctx, engine.ApplyRequest{
		Document: enginetest.SandboxCompany(served), Summary: "secretsync: the revision a node runs", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the node applying the first revision", func() error { return eng.Ready(ctx) })
	// The fleet's next revision references no value the operator owns, and
	// one nothing sets, which the apply refuses.
	r, err := eng.ApplyConfig(ctx, engine.ApplyRequest{
		Document: enginetest.SandboxCompany("SECRETSYNC_E2B_NEVER_SET"), Summary: "secretsync: the fleet's revision",
		IfMatch: p.RevisionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	servingP := func() error {
		fleet, err := eng.Fleet(ctx)
		if err != nil {
			return err
		}
		if record := fleet.LastApply(); record.RevisionID != r.RevisionID || !record.Status.Failed() ||
			record.Serving != p.RevisionID || record.Boots != p.RevisionID {
			return fmt.Errorf("LastApply() = %+v, want %s refused and %s served and booted on", record, r.RevisionID, p.RevisionID)
		}
		return nil
	}
	waitFor(t, "the node refusing the fleet's revision", servingP)
	running, err := eng.Revision(ctx, p.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(running.Document, []byte("${"+served+"}")) {
		t.Fatalf("revision %s's document does not reference ${%s}:\n%s", p.RevisionID, served, running.Document)
	}

	desired := desiredOf(nil)
	desired.Keep(r.RevisionID)
	keeping := desired
	keeping.KeepServed(p.RevisionID, served)
	state, res, err := Sync(ctx, eng, keeping, state, self)
	if err != nil {
		t.Fatalf("Sync keeping the served revision: %v", err)
	}
	if len(res.Deleted) != 0 || !slices.Equal(res.Held, []string{served}) {
		t.Fatalf("Result = %+v, want nothing deleted and %s held for revision %s", res, served, p.RevisionID)
	}
	if err := e.Restart(t); err != nil {
		t.Fatalf("the node does not start on the revision it ran, with the value it references kept: %v", err)
	}
	waitFor(t, "the restarted node ready on the revision it ran", func() error {
		if err := eng.Ready(ctx); err != nil {
			return err
		}
		return servingP()
	})

	if _, res, err = Sync(ctx, eng, desired, state, self); err != nil || !slices.Equal(res.Deleted, []string{served}) {
		t.Fatalf("Sync for the fleet's revision alone = %+v, %v; want %s deleted", res, err, served)
	}
	if err := e.Restart(t); err == nil {
		t.Fatalf("the node started on revision %s without %s, which its document references", p.RevisionID, served)
	}
}

// waitFor polls check until it returns nil, for at most 30 seconds: what an
// engine does after a write, it does on its own loop.
func waitFor(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: still failing after 30s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
