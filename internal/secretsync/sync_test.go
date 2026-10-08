package secretsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// fakeEngine is the engine's /secrets surface as its documentation describes
// it: names in the environment-variable grammar, values up to 64 KiB, a
// listing without values, an idempotent delete. It records every call, and
// fails the ones a test names.
type fakeEngine struct {
	entries map[string]fakeEntry
	calls   []string

	failList   error
	failPut    map[string]error
	failDelete map[string]error
}

type fakeEntry struct {
	value  string
	source string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{entries: map[string]fakeEntry{}, failPut: map[string]error{}, failDelete: map[string]error{}}
}

func (e *fakeEngine) ListSecrets(context.Context) ([]SecretInfo, error) {
	e.calls = append(e.calls, "list")
	if e.failList != nil {
		return nil, e.failList
	}
	var out []SecretInfo
	for _, name := range slices.Sorted(maps.Keys(e.entries)) {
		out = append(out, SecretInfo{Name: name, Source: e.entries[name].source})
	}
	return out, nil
}

func (e *fakeEngine) PutSecret(_ context.Context, name string, value []byte, source string) error {
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
	e.entries[name] = fakeEntry{value: string(value), source: source}
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
	// foreignName is an entry somebody other than the operator wrote.
	foreignName = "GITLAB_TOKEN_SWE"
)

var (
	nameA = StoreName(secretref.Key{Namespace: "ns", Name: "a", Key: tokenKey})
	nameB = StoreName(secretref.Key{Namespace: "ns", Name: "b", Key: tokenKey})
	nameC = StoreName(secretref.Key{Namespace: "ns", Name: "c", Key: tokenKey})
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
	next, res, err := Sync(context.Background(), eng, desired, prev)
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

// TestSyncIsIdempotent: the first sync writes every name, in name order,
// with the operator's provenance; a second sync of the same set, from the
// state the first returned, only lists.
func TestSyncIsIdempotent(t *testing.T) {
	eng := newFakeEngine()
	desired := desiredOf(map[string]string{nameC: "c1", nameA: "a1", nameB: "b1"})

	state, res := mustSync(t, eng, desired, State{})
	assertCalls(t, eng, "list", "put "+nameA, "put "+nameB, "put "+nameC)
	if !slices.Equal(res.Put, []string{nameA, nameB, nameC}) || len(res.Deleted) != 0 {
		t.Fatalf("Result = %+v", res)
	}
	assertStore(t, eng, map[string]fakeEntry{
		nameA: {"a1", Source}, nameB: {"b1", Source}, nameC: {"c1", Source},
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
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", Source}, nameB: {"b2", Source}})
}

// TestSyncRepairsTheStore: the state says what the operator pushed, not
// what the store holds now. An entry the engine lost, or one someone else
// wrote over, is written again although its value did not change.
func TestSyncRepairsTheStore(t *testing.T) {
	eng := newFakeEngine()
	desired := desiredOf(map[string]string{nameA: "a1", nameB: "b1", nameC: "c1"})
	state, _ := mustSync(t, eng, desired, State{})
	eng.takeCalls()

	delete(eng.entries, nameA)
	eng.entries[nameB] = fakeEntry{value: "typed-by-hand", source: "cli"}

	state, res := mustSync(t, eng, desired, state)
	assertCalls(t, eng, "list", "put "+nameA, "put "+nameB)
	if !slices.Equal(res.Put, []string{nameA, nameB}) {
		t.Fatalf("Put = %v", res.Put)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", Source}, nameB: {"b1", Source}, nameC: {"c1", Source}})

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
// that starts with the prefix.
func TestSyncCollectsOnlyTheOperatorsNames(t *testing.T) {
	eng := newFakeEngine()
	foreign := map[string]fakeEntry{
		foreignName:         {"glpat", "provision"},
		"K8S_MY_OWN_TOKEN":  {"mine", "cli"},
		"ANTHROPIC_API_KEY": {"sk", "api"},
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
	want[nameA] = fakeEntry{"a1", Source}
	assertStore(t, eng, want)
	if _, ok := next.fingerprints[nameB]; ok {
		t.Fatal("the state still records the deleted name")
	}

	// An owned name the state never recorded (left by a state that was
	// lost) is collected too: ownership is the name's shape, not the state.
	eng.entries[nameC] = fakeEntry{"c-orphan", Source}
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
	next.Keep(nameB)
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
	partial, res, err := Sync(context.Background(), eng, desired, state)
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
	partial, res, err = Sync(context.Background(), eng, desired, partial)
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
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", Source}, nameB: {"b2", Source}})
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
	got, res, err := Sync(context.Background(), eng, desiredOf(map[string]string{nameB: "b1"}), state)
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
