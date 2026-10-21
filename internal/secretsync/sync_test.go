package secretsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// fakeEngine is the engine's /secrets surface as its documentation describes
// it: names in the environment-variable grammar, values up to 64 KiB, a
// listing without values that names the credential each entry was last
// written with, an idempotent delete. Every write it takes is Sync's, so it
// records self as the writer; a test plays anybody else by writing an entry
// directly. It records every call, and fails the ones a test names.
type fakeEngine struct {
	entries map[string]fakeEntry
	calls   []string

	failList   error
	failPut    map[string]error
	failDelete map[string]error
}

type fakeEntry struct {
	value string
	// by is the credential that last wrote the value, as the engine lists
	// it in operator_id.
	by string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{entries: map[string]fakeEntry{}, failPut: map[string]error{}, failDelete: map[string]error{}}
}

func (e *fakeEngine) ListSecrets(context.Context) ([]engine.SecretInfo, error) {
	e.calls = append(e.calls, "list")
	if e.failList != nil {
		return nil, e.failList
	}
	var out []engine.SecretInfo
	for _, name := range slices.Sorted(maps.Keys(e.entries)) {
		entry := e.entries[name]
		out = append(out, engine.SecretInfo{Name: name, UpdatedBy: entry.by, OperatorID: entry.by})
	}
	return out, nil
}

func (e *fakeEngine) PutSecret(_ context.Context, name string, value []byte) error {
	e.calls = append(e.calls, "put "+name)
	if err := e.failPut[name]; err != nil {
		return err
	}
	if !storeGrammar.MatchString(name) {
		return fmt.Errorf("invalid_name %q", name)
	}
	if len(value) > MaxValueBytes {
		return errors.New("value_too_large")
	}
	e.entries[name] = fakeEntry{value: string(value), by: self}
	return nil
}

func (e *fakeEngine) DeleteSecret(_ context.Context, name string) (bool, error) {
	e.calls = append(e.calls, "delete "+name)
	if err := e.failDelete[name]; err != nil {
		return false, err
	}
	_, ok := e.entries[name]
	delete(e.entries, name)
	return ok, nil
}

// holds reports whether the store holds name.
func (e *fakeEngine) holds(name string) bool {
	_, ok := e.entries[name]
	return ok
}

// takeCalls returns the calls since the last takeCalls.
func (e *fakeEngine) takeCalls() []string {
	calls := e.calls
	e.calls = nil
	return calls
}

// Literals the tests share.
const (
	nimbusNS         = "nimbus"
	slackSecret      = "ana-slack"
	slackName        = "slack"
	botTokenKey      = "botToken"
	signingSecretKey = "signingSecret"
	apiKeyKey        = "apiKey"
	tokenKey         = "token"
	// foreignName is an entry somebody other than the operator wrote, and
	// foreignValue its value.
	foreignName  = "GITLAB_TOKEN_SWE"
	foreignValue = "theirs"
	// access is the credential of somebody else with the same grant as the
	// operator: the company's published access token.
	access = "token:access"
)

// self is the credential the engine records for the operator's writes: its
// own Tier A token's login.
var self = engine.TokenLogin("operator")

var (
	keyA  = secretref.Key{Namespace: "ns", Name: "a", Key: tokenKey}
	keyB  = secretref.Key{Namespace: "ns", Name: "b", Key: tokenKey}
	keyC  = secretref.Key{Namespace: "ns", Name: "c", Key: tokenKey}
	nameA = StoreName(keyA)
	nameB = StoreName(keyB)
	nameC = StoreName(keyC)
)

// revision1 and revision2 are two revisions of the engine's document.
const (
	revision1 = "0191d4a6-5c1e-7000-8000-000000000001"
	revision2 = "0191d4a6-5c1e-7000-8000-000000000002"
)

func desiredOf(values map[string]string) Desired {
	d := Desired{values: map[string][]byte{}}
	for name, v := range values {
		d.values[name] = []byte(v)
	}
	return d
}

func mustSync(t *testing.T, eng *fakeEngine, desired Desired, prev State) (State, Result) {
	t.Helper()
	next, res, err := Sync(context.Background(), eng, desired, prev, self)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return next, res
}

func assertStore(t *testing.T, eng *fakeEngine, want map[string]fakeEntry) {
	t.Helper()
	if !maps.Equal(eng.entries, want) {
		t.Fatalf("store = %v, want %v", eng.entries, want)
	}
}

func assertCalls(t *testing.T, eng *fakeEngine, want ...string) {
	t.Helper()
	if got := eng.takeCalls(); !slices.Equal(got, want) {
		t.Fatalf("engine calls = %q, want %q", got, want)
	}
}

// TestSyncIsIdempotent: the first sync writes every name, in name order, as
// the operator; a second sync of the same set, from the state the first
// returned, only lists.
func TestSyncIsIdempotent(t *testing.T) {
	eng := newFakeEngine()
	desired := desiredOf(map[string]string{nameC: "c1", nameA: "a1", nameB: "b1"})

	state, res := mustSync(t, eng, desired, State{})
	assertCalls(t, eng, "list", "put "+nameA, "put "+nameB, "put "+nameC)
	if !slices.Equal(res.Put, []string{nameA, nameB, nameC}) || len(res.Deleted) != 0 {
		t.Fatalf("Result = %+v", res)
	}
	assertStore(t, eng, map[string]fakeEntry{
		nameA: {"a1", self}, nameB: {"b1", self}, nameC: {"c1", self},
	})

	again, res := mustSync(t, eng, desired, state)
	assertCalls(t, eng, "list")
	if len(res.Put) != 0 || len(res.Deleted) != 0 {
		t.Fatalf("the second Sync changed the store: %+v", res)
	}
	if !maps.EqualFunc(again.fingerprints, state.fingerprints, slices.Equal) || !slices.Equal(again.key, state.key) {
		t.Fatal("the second Sync changed the state")
	}
}

// TestSyncWritesOnlyWhatChanged: a rotated Secret rewrites its entry and no
// other.
func TestSyncWritesOnlyWhatChanged(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), State{})
	eng.takeCalls()

	_, res := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b2"}), state)
	assertCalls(t, eng, "list", "put "+nameB)
	if !slices.Equal(res.Put, []string{nameB}) {
		t.Fatalf("Put = %v, want only %s", res.Put, nameB)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", self}, nameB: {"b2", self}})
}

// TestSyncRepairsTheStore: the state says what the operator pushed, not
// what the store holds now. An entry the engine lost, or one someone else
// wrote over, is written again although its value did not change, and the
// one someone else wrote is reported with who wrote it.
func TestSyncRepairsTheStore(t *testing.T) {
	eng := newFakeEngine()
	desired := desiredOf(map[string]string{nameA: "a1", nameB: "b1", nameC: "c1"})
	state, _ := mustSync(t, eng, desired, State{})
	eng.takeCalls()

	delete(eng.entries, nameA)
	eng.entries[nameB] = fakeEntry{value: "typed-by-hand", by: access}

	state, res := mustSync(t, eng, desired, state)
	assertCalls(t, eng, "list", "put "+nameA, "put "+nameB)
	if !slices.Equal(res.Put, []string{nameA, nameB}) {
		t.Fatalf("Put = %v", res.Put)
	}
	// The lost entry is no overwrite: nobody else wrote it.
	if want := []Overwrite{{Name: nameB, By: access, Credential: access}}; !slices.Equal(res.Overwritten, want) {
		t.Fatalf("Overwritten = %+v, want %+v", res.Overwritten, want)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", self}, nameB: {"b1", self}, nameC: {"c1", self}})

	// A name gone from the store and from desired leaves the state too,
	// rather than being vouched for until the Company is deleted.
	delete(eng.entries, nameC)
	state, _ = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), state)
	if _, ok := state.fingerprints[nameC]; ok {
		t.Fatalf("the state still records %s, which the store no longer holds", nameC)
	}
}

// TestSyncCollectsOnlyTheOperatorsNames: an owned name nothing references
// is deleted; a name the operator does not own is never touched, even one
// that starts with the prefix or that the operator's own credential wrote.
func TestSyncCollectsOnlyTheOperatorsNames(t *testing.T) {
	eng := newFakeEngine()
	foreign := map[string]fakeEntry{
		foreignName:         {"glpat", "reconcile loop"},
		"K8S_MY_OWN_TOKEN":  {"mine", access},
		"ANTHROPIC_API_KEY": {"sk", self},
	}
	maps.Copy(eng.entries, foreign)
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), State{})
	eng.takeCalls()

	next, res := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), state)
	assertCalls(t, eng, "list", "delete "+nameB)
	if !slices.Equal(res.Deleted, []string{nameB}) {
		t.Fatalf("Deleted = %v", res.Deleted)
	}
	want := maps.Clone(foreign)
	want[nameA] = fakeEntry{"a1", self}
	assertStore(t, eng, want)
	if _, ok := next.fingerprints[nameB]; ok {
		t.Fatal("the state still records the deleted name")
	}

	// An owned name the state never recorded (left by a state that was
	// lost) is collected too: ownership is the name's shape, not the state.
	eng.entries[nameC] = fakeEntry{"c-orphan", self}
	_, res = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), next)
	if !slices.Equal(res.Deleted, []string{nameC}) {
		t.Fatalf("Deleted = %v, want the orphaned %s", res.Deleted, nameC)
	}
}

// TestSyncKeepsWhatTheRunningDocumentReferences: a kept name survives the
// sync, with its fingerprint, until it is no longer kept.
func TestSyncKeepsWhatTheRunningDocumentReferences(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1"}), State{})
	eng.takeCalls()

	next := desiredOf(map[string]string{nameA: "a1", nameC: "c1"})
	next.Keep(revision1, nameB)
	state, res := mustSync(t, eng, next, state)
	assertCalls(t, eng, "list", "put "+nameC)
	if len(res.Deleted) != 0 {
		t.Fatalf("Deleted = %v, want the kept %s left alone", res.Deleted, nameB)
	}
	if _, ok := state.fingerprints[nameB]; !ok {
		t.Fatal("the state forgot the kept name")
	}

	_, res = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameC: "c1"}), state)
	if !slices.Equal(res.Deleted, []string{nameB}) {
		t.Fatalf("Deleted = %v, want %s once it is no longer kept", res.Deleted, nameB)
	}
}

// TestSyncWritesBeforeItDeletes: a document switch must find every name it
// references written before anything it no longer references is removed.
func TestSyncWritesBeforeItDeletes(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), State{})
	eng.takeCalls()

	mustSync(t, eng, desiredOf(map[string]string{nameB: "b1", nameC: "c1"}), state)
	assertCalls(t, eng, "list", "put "+nameB, "put "+nameC, "delete "+nameA)
}

// TestSyncReturnsWhatItAchieved: after a failure, the state returned records
// exactly what is known to be in the store, so saving it and retrying
// writes only what is missing, and nothing is deleted while a write is
// outstanding.
func TestSyncReturnsWhatItAchieved(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameB: "b1", nameC: "stale"}), State{})
	eng.takeCalls()

	desired := desiredOf(map[string]string{nameA: "a1", nameB: "b2"})
	boom := errors.New("engine unavailable")
	eng.failPut[nameB] = boom
	partial, res, err := Sync(context.Background(), eng, desired, state, self)
	if !errors.Is(err, boom) {
		t.Fatalf("Sync() error = %v, want the engine's", err)
	}
	assertCalls(t, eng, "list", "put "+nameA, "put "+nameB)
	if !slices.Equal(res.Put, []string{nameA}) || len(res.Deleted) != 0 {
		t.Fatalf("Result = %+v, want only %s written", res, nameA)
	}
	if _, ok := partial.fingerprints[nameA]; !ok {
		t.Error("the state does not record the write that succeeded")
	}
	// The engine may or may not hold b2 now, so the state must not claim b1.
	if _, ok := partial.fingerprints[nameB]; ok {
		t.Error("the state records a fingerprint for the write that failed")
	}

	delete(eng.failPut, nameB)
	eng.failDelete[nameC] = boom
	partial, res, err = Sync(context.Background(), eng, desired, partial, self)
	if !errors.Is(err, boom) {
		t.Fatalf("Sync() error = %v, want the engine's", err)
	}
	assertCalls(t, eng, "list", "put "+nameB, "delete "+nameC)
	if !slices.Equal(res.Put, []string{nameB}) || len(res.Deleted) != 0 {
		t.Fatalf("Result = %+v", res)
	}

	delete(eng.failDelete, nameC)
	final, res := mustSync(t, eng, desired, partial)
	assertCalls(t, eng, "list", "delete "+nameC)
	if len(res.Put) != 0 || !slices.Equal(res.Deleted, []string{nameC}) {
		t.Fatalf("Result = %+v", res)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", self}, nameB: {"b2", self}})
	if got := slices.Sorted(maps.Keys(final.fingerprints)); !slices.Equal(got, []string{nameA, nameB}) {
		t.Fatalf("final state names %v", got)
	}
}

// TestSyncChangesNothingWhenTheListingFails: without the listing, Sync
// cannot tell what to write or delete, so it does neither and keeps the
// previous state.
func TestSyncChangesNothingWhenTheListingFails(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1"}), State{})
	eng.takeCalls()

	eng.failList = errors.New("connection refused")
	got, res, err := Sync(context.Background(), eng, desiredOf(map[string]string{nameB: "b1"}), state, self)
	if !errors.Is(err, eng.failList) {
		t.Fatalf("Sync() error = %v", err)
	}
	assertCalls(t, eng, "list")
	if len(res.Put)+len(res.Deleted) != 0 {
		t.Fatalf("Result = %+v", res)
	}
	if !maps.EqualFunc(got.fingerprints, state.fingerprints, slices.Equal) {
		t.Fatal("the state changed")
	}
}

// TestSyncRemovesAnAbsentValueOnlyWhereTheRunningRevisionLetsItGo: a name
// whose optional reference has no Secret or key any more is removed from the
// store, so the engine resolves it to nothing and leaves its key out rather
// than starting with the deleted Secret's value, where the running revision
// does not reference it or lets it go too, as the state recorded when the
// operator activated it; where that revision reads it as required, or is one
// the state records nothing of, the value stays for it (Retained), and is
// not withdrawn. A kept name with no such reference is left as Keep
// promises, and a name somebody else wrote is never touched.
func TestSyncRemovesAnAbsentValueOnlyWhereTheRunningRevisionLetsItGo(t *testing.T) {
	letsGoB := State{}.Activated(revision1, []secretref.Key{keyB})
	for _, tc := range []struct {
		name string
		// recorded is what the state records of the operator's last
		// activation, running the revision the engine runs, and kept
		// whether its document references nameB.
		recorded State
		running  string
		kept     bool
		removed  bool
	}{
		{name: "the running revision lets it go", recorded: letsGoB, running: revision1, kept: true, removed: true},
		{name: "the running revision does not reference it", recorded: State{}, running: revision1, removed: true},
		{name: "the running revision requires it", recorded: State{}.Activated(revision1, []secretref.Key{keyC}), running: revision1, kept: true},
		{name: "somebody else activated the running revision", recorded: letsGoB, running: revision2, kept: true},
		{name: "the state records no activation", recorded: State{}, running: revision1, kept: true},
		{name: "no revision runs", recorded: letsGoB, removed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := newFakeEngine()
			eng.entries[foreignName] = fakeEntry{value: foreignValue, by: access}
			state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), tc.recorded)
			eng.takeCalls()

			next := desiredOf(map[string]string{nameA: "a"})
			next.absent = map[string]*MissingError{nameB: {Key: keyB, Reason: SecretNotFound}}
			if tc.running != "" {
				names := []string{nameC}
				if tc.kept {
					names = append(names, nameB)
				}
				next.Keep(tc.running, names...)
			}
			after, res := mustSync(t, eng, next, state)
			want := map[string]fakeEntry{
				nameA: {value: "a", by: self}, nameC: {value: "c", by: self}, foreignName: {value: foreignValue, by: access},
			}
			if !tc.removed {
				want[nameB] = fakeEntry{value: "b", by: self}
			}
			if tc.running == "" {
				// Nothing keeps nameC either.
				delete(want, nameC)
			}
			assertStore(t, eng, want)
			removed, retained, withdrawn := []string(nil), []string{nameB}, []string(nil)
			if tc.removed {
				removed, retained, withdrawn = []string{nameB}, nil, []string{nameB}
			}
			if tc.running == "" {
				removed = []string{nameB, nameC}
			}
			if !slices.Equal(res.Deleted, removed) || !slices.Equal(res.Retained, retained) {
				t.Errorf("Deleted = %v and Retained = %v, want %v and %v", res.Deleted, res.Retained, removed, retained)
			}
			if !slices.Equal(after.Withdrawn(), withdrawn) {
				t.Errorf("Withdrawn() = %v, want %v: the engine runs the value it read until a revision activates",
					after.Withdrawn(), withdrawn)
			}
			if !slices.Equal(after.Retained(), retained) {
				t.Errorf("State.Retained() = %v, want %v", after.Retained(), retained)
			}
			if !slices.Equal(after.LetGo(revision1), tc.recorded.LetGo(revision1)) {
				t.Errorf("the sync changed the recorded activation: LetGo = %v, want %v",
					after.LetGo(revision1), tc.recorded.LetGo(revision1))
			}
		})
	}

	t.Run("withdrawn until a revision activates", func(t *testing.T) {
		// Whatever the syncs in between: pushed again, the engine still
		// runs what it read before.
		eng := newFakeEngine()
		state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b"}), letsGoB)
		gone := desiredOf(map[string]string{nameA: "a"})
		gone.absent = map[string]*MissingError{nameB: {Key: keyB, Reason: SecretNotFound}}
		gone.Keep(revision1, nameA, nameB)
		after, _ := mustSync(t, eng, gone, state)
		again := desiredOf(map[string]string{nameA: "a", nameB: "b2"})
		again.Keep(revision1, nameA, nameB)
		after, _ = mustSync(t, eng, again, after)
		if !slices.Equal(after.Withdrawn(), []string{nameB}) || len(after.Retained()) != 0 {
			t.Errorf("Withdrawn() = %v and Retained() = %v after the value came back, want [%s] and none",
				after.Withdrawn(), after.Retained(), nameB)
		}
		if got := after.Reactivated().Withdrawn(); len(got) != 0 {
			t.Errorf("Reactivated().Withdrawn() = %v, want none", got)
		}
	})

	t.Run("lost by the engine while the running revision requires it", func(t *testing.T) {
		// A value the store lost owes no activation where the running
		// revision requires it: activated again, it would start without
		// it what it has been starting with it.
		eng := newFakeEngine()
		state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b"}), State{}.Activated(revision1, nil))
		delete(eng.entries, nameB)
		gone := desiredOf(map[string]string{nameA: "a"})
		gone.absent = map[string]*MissingError{nameB: {Key: keyB, Reason: SecretNotFound}}
		gone.Keep(revision1, nameA, nameB)
		after, res := mustSync(t, eng, gone, state)
		if len(after.Withdrawn()) != 0 || len(res.Retained) != 0 {
			t.Errorf("Withdrawn() = %v and Retained = %v, want none: nothing was removed, and nothing is held", after.Withdrawn(), res.Retained)
		}
		held := Desired{partial: true, absent: gone.absent}
		held.Keep(revision1, nameA, nameB)
		after, res, err := Withdraw(context.Background(), eng, held, state)
		if err != nil || len(after.Withdrawn()) != 0 || len(res.Deleted)+len(res.Retained) != 0 {
			t.Errorf("Withdraw: Withdrawn() = %v and %+v (%v), want nothing", after.Withdrawn(), res, err)
		}
	})

	t.Run("retained, then let go", func(t *testing.T) {
		// A name kept for a revision that requires it goes once the
		// operator has activated one that lets it go.
		eng := newFakeEngine()
		state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b"}), State{}.Activated(revision1, nil))
		gone := desiredOf(map[string]string{nameA: "a"})
		gone.absent = map[string]*MissingError{nameB: {Key: keyB, Reason: SecretNotFound}}
		kept := gone
		kept.Keep(revision1, nameA, nameB)
		state, res := mustSync(t, eng, kept, state)
		if !slices.Equal(res.Retained, []string{nameB}) || len(state.Withdrawn()) != 0 {
			t.Fatalf("Retained = %v and Withdrawn() = %v, want [%s] and none", res.Retained, state.Withdrawn(), nameB)
		}
		eng.takeCalls()
		state, res = mustSync(t, eng, gone, state.Activated(revision2, []secretref.Key{keyB}))
		assertCalls(t, eng, "list", "delete "+nameB)
		if !slices.Equal(res.Deleted, []string{nameB}) || !slices.Equal(state.Withdrawn(), []string{nameB}) ||
			len(state.Retained()) != 0 {
			t.Errorf("Deleted = %v, Withdrawn() = %v and Retained() = %v, want [%s], [%s] and none",
				res.Deleted, state.Withdrawn(), state.Retained(), nameB, nameB)
		}
	})
}

// TestSyncRefusesAPartialDesired: a Desired Resolve returned with an error
// pushes nothing, and synced it would delete every entry the operator owns.
func TestSyncRefusesAPartialDesired(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a"}), State{})
	eng.takeCalls()
	next, _, err := Sync(context.Background(), eng, Desired{partial: true}, state, self)
	if err == nil {
		t.Fatal("Sync took a partial Desired")
	}
	assertCalls(t, eng)
	assertStore(t, eng, map[string]fakeEntry{nameA: {value: "a", by: self}})
	if !maps.EqualFunc(next.fingerprints, state.fingerprints, bytes.Equal) {
		t.Errorf("the state changed: %v, want %v", next, state)
	}
}

// TestWithdrawRemovesOnlyAbsentValues: while a document cannot be applied,
// Withdraw removes the entry of every optional reference whose Secret or key
// is gone and records it as withdrawn, and leaves every other entry as the
// running revision needs it: what it requires, what a partial Desired does
// not push, what somebody else wrote. An absent name the operator pushed and
// the engine no longer lists was removed by a run that stopped before saving
// it, and is withdrawn too.
func TestWithdrawRemovesOnlyAbsentValues(t *testing.T) {
	eng := newFakeEngine()
	eng.entries[foreignName] = fakeEntry{value: foreignValue, by: access}
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), State{})
	delete(eng.entries, nameC)
	eng.takeCalls()

	held := Desired{partial: true, absent: map[string]*MissingError{
		nameB: {Key: keyB, Reason: SecretNotFound}, nameC: {Key: keyC, Reason: KeyNotFound},
	}}
	next, res, err := Withdraw(context.Background(), eng, held, state)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	assertCalls(t, eng, "list", "delete "+nameB)
	if !slices.Equal(res.Deleted, []string{nameB}) || len(res.Retained) != 0 {
		t.Errorf("Deleted = %v and Retained = %v, want [%s] and none", res.Deleted, res.Retained, nameB)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {value: "a", by: self}, foreignName: {value: foreignValue, by: access}})
	if want := []string{nameB, nameC}; !slices.Equal(next.Withdrawn(), slices.Sorted(slices.Values(want))) {
		t.Errorf("Withdrawn() = %v, want %v", next.Withdrawn(), want)
	}
	if _, ok := next.fingerprints[nameA]; !ok || len(next.fingerprints) != 1 {
		t.Errorf("fingerprints = %v, want %s's alone", next, nameA)
	}

	// A second Withdraw has nothing to remove and keeps the record.
	eng.takeCalls()
	again, res, err := Withdraw(context.Background(), eng, held, next)
	if err != nil || len(res.Deleted) != 0 || !slices.Equal(again.Withdrawn(), next.Withdrawn()) {
		t.Fatalf("Withdraw again = %v, %v, %v", again.Withdrawn(), res.Deleted, err)
	}
	assertCalls(t, eng, "list")

	// Nothing absent: nothing is read or removed.
	if _, res, err := Withdraw(context.Background(), eng, Desired{partial: true}, next); err != nil || res.Deleted != nil {
		t.Fatalf("Withdraw of nothing = %v, %v", res.Deleted, err)
	}
	assertCalls(t, eng)
}

// TestWithdrawKeepsWhatTheRunningRevisionRequires: while the next document
// is held, the running revision keeps every value it reads as required,
// that of a Secret deleted since included, and only a name it lets go, as
// the state recorded when the operator activated it, or does not reference
// at all, is removed. What it keeps is Retained, in the Result and in the
// State, until nothing is absent.
func TestWithdrawKeepsWhatTheRunningRevisionRequires(t *testing.T) {
	eng := newFakeEngine()
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}),
		State{}.Activated(revision1, []secretref.Key{keyC}))
	eng.takeCalls()

	held := Desired{partial: true, absent: map[string]*MissingError{
		nameA: {Key: keyA, Reason: SecretNotFound},
		nameB: {Key: keyB, Reason: SecretNotFound},
		nameC: {Key: keyC, Reason: KeyNotFound},
	}}
	// The running revision references nameB, which it requires, and
	// nameC, which it lets go; nameA it does not reference.
	held.Keep(revision1, nameB, nameC)
	next, res, err := Withdraw(context.Background(), eng, held, state)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	assertCalls(t, eng, "list", "delete "+nameA, "delete "+nameC)
	assertStore(t, eng, map[string]fakeEntry{nameB: {value: "b", by: self}})
	if !slices.Equal(res.Deleted, []string{nameA, nameC}) || !slices.Equal(res.Retained, []string{nameB}) {
		t.Errorf("Deleted = %v and Retained = %v, want [%s %s] and [%s]", res.Deleted, res.Retained, nameA, nameC, nameB)
	}
	if !slices.Equal(next.Withdrawn(), []string{nameA, nameC}) || !slices.Equal(next.Retained(), []string{nameB}) {
		t.Errorf("Withdrawn() = %v and Retained() = %v", next.Withdrawn(), next.Retained())
	}
	if _, ok := next.fingerprints[nameB]; !ok {
		t.Error("the state forgot the value it keeps")
	}

	// Run by somebody else, the same document lets nothing go.
	eng = newFakeEngine()
	state, _ = mustSync(t, eng, desiredOf(map[string]string{nameB: "b", nameC: "c"}), State{}.Activated(revision1, []secretref.Key{keyC}))
	eng.takeCalls()
	other := held
	other.Keep(revision2, nameB, nameC)
	_, res, err = Withdraw(context.Background(), eng, other, state)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	assertCalls(t, eng, "list")
	if len(res.Deleted) != 0 || !slices.Equal(res.Retained, []string{nameB, nameC}) {
		t.Errorf("Deleted = %v and Retained = %v under a revision the operator did not activate, want none and both",
			res.Deleted, res.Retained)
	}

	// Nothing absent any more: nothing is retained either.
	cleared, res, err := Withdraw(context.Background(), eng, Desired{partial: true}, next)
	if err != nil || len(res.Retained) != 0 || len(cleared.Retained()) != 0 {
		t.Fatalf("Withdraw of nothing = %v, %v, %v", res.Retained, cleared.Retained(), err)
	}
}

// TestARevisionThatLetsANameGoGoesWithoutItWhateverTheNextDocumentReads:
// the store serves every revision by its own reading, so a name the running
// revision lets go whose Secret is gone (Desired.Release) leaves the store
// and is withdrawn, by Sync and by Withdraw alike, whether the next document
// reads it where it is required (and is held for it) or does not reference it
// at all; while a revision the State does not record as letting it go keeps
// it, and a value the next document pushes is pushed. A pushed name the
// engine no longer lists was removed by a run that did not save it, and is
// withdrawn too.
func TestARevisionThatLetsANameGoGoesWithoutItWhateverTheNextDocumentReads(t *testing.T) {
	ctx := context.Background()
	letsGoB := State{}.Activated(revision1, []secretref.Key{keyB})
	goneB := []*MissingError{{Key: keyB, Reason: SecretNotFound}}
	for _, tc := range []struct {
		name     string
		recorded State
		running  string
		removed  bool
	}{
		{name: "the running revision lets it go", recorded: letsGoB, running: revision1, removed: true},
		{name: "the running revision requires it", recorded: State{}.Activated(revision1, []secretref.Key{keyC}), running: revision1},
		{name: "somebody else activated the running revision", recorded: letsGoB, running: revision2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The next document requires nameB, whose Secret is gone, and
			// is held for it: Withdraw.
			eng := newFakeEngine()
			state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), tc.recorded)
			eng.takeCalls()
			held := Desired{partial: true}
			held.Keep(tc.running, nameA, nameB, nameC)
			held.Release(goneB...)
			next, res, err := Withdraw(ctx, eng, held, state)
			if err != nil {
				t.Fatalf("Withdraw: %v", err)
			}
			if tc.removed {
				assertCalls(t, eng, "list", "delete "+nameB)
			} else {
				assertCalls(t, eng, "list")
			}
			if eng.holds(nameB) == tc.removed || len(res.Retained) != 0 || slices.Contains(next.Withdrawn(), nameB) != tc.removed {
				t.Errorf("Withdraw: the store holds %s: %t, Retained = %v, Withdrawn() = %v; want it removed: %t, "+
					"nothing retained", nameB, eng.holds(nameB), res.Retained, next.Withdrawn(), tc.removed)
			}

			// The next document does not reference nameB: Sync before the
			// apply.
			eng = newFakeEngine()
			state, _ = mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), tc.recorded)
			eng.takeCalls()
			unreferenced := desiredOf(map[string]string{nameA: "a", nameC: "c"})
			unreferenced.Keep(tc.running, nameA, nameB, nameC)
			unreferenced.Release(goneB...)
			next, res = mustSync(t, eng, unreferenced, state)
			if eng.holds(nameB) == tc.removed || len(res.Retained) != 0 || slices.Contains(next.Withdrawn(), nameB) != tc.removed {
				t.Errorf("Sync: the store holds %s: %t, Retained = %v, Withdrawn() = %v; want it removed: %t, "+
					"nothing retained", nameB, eng.holds(nameB), res.Retained, next.Withdrawn(), tc.removed)
			}

			// Removed by a run that did not save it.
			eng = newFakeEngine()
			state, _ = mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), tc.recorded)
			delete(eng.entries, nameB)
			next, _, err = Withdraw(ctx, eng, held, state)
			if err != nil || slices.Contains(next.Withdrawn(), nameB) != tc.removed {
				t.Errorf("Withdraw of a name the engine no longer lists: Withdrawn() = %v (%v), want it withdrawn: %t",
					next.Withdrawn(), err, tc.removed)
			}
			next, _ = mustSync(t, eng, unreferenced, state)
			if slices.Contains(next.Withdrawn(), nameB) != tc.removed {
				t.Errorf("Sync of a name the engine no longer lists: Withdrawn() = %v, want it withdrawn: %t", next.Withdrawn(), tc.removed)
			}
		})
	}

	t.Run("read again since", func(t *testing.T) {
		// The Secret came back between the two reads: its value is pushed,
		// never removed, and nothing is withdrawn, the store having lost
		// it in between or not.
		eng := newFakeEngine()
		state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b"}), letsGoB)
		delete(eng.entries, nameB)
		back := desiredOf(map[string]string{nameA: "a", nameB: "b2"})
		back.Keep(revision1, nameA, nameB)
		back.Release(goneB...)
		next, res := mustSync(t, eng, back, state)
		if !eng.holds(nameB) || len(res.Deleted) != 0 || len(next.Withdrawn()) != 0 {
			t.Errorf("the store holds %s: %t, Deleted = %v, Withdrawn() = %v; want it pushed and nothing removed",
				nameB, eng.holds(nameB), res.Deleted, next.Withdrawn())
		}
	})
}
