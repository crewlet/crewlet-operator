package secretsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// fakeEngine is the engine's /secrets surface as its documentation describes
// it: names in the environment-variable grammar, values up to 64 KiB, a
// listing without values that names the credential each entry was last
// written with, an idempotent delete, and a batch write of up to 64 values
// that re-activates the active revision once when it reads any of them
// (reads), as a delete of a value it reads does, answering 200 whatever the
// re-activation did (reload). Every
// write it takes is Sync's, so it records self as the writer; a test plays
// anybody else by writing an entry directly. It records every call, a batch
// as "put" and its names, a value written alone as "put-raw" and its name,
// and fails the ones a test names: a batch fails when any of its names does.
type fakeEngine struct {
	entries map[string]fakeEntry
	calls   []string

	// reads are the names the active revision reads, and reload what a
	// batch that writes one answers about its re-activation; a nil reload
	// is one that lands as the next revision of reloaded.
	reads    map[string]bool
	reload   *engine.Reload
	reloaded []string

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

func (e *fakeEngine) PutSecrets(_ context.Context, values map[string][]byte) (engine.SecretsWritten, error) {
	names := slices.Sorted(maps.Keys(values))
	e.calls = append(e.calls, "put "+strings.Join(names, " "))
	if len(values) == 0 || len(values) > engine.MaxSecretBatch {
		return engine.SecretsWritten{}, fmt.Errorf("invalid_body: %d values", len(values))
	}
	for _, name := range names {
		if err := e.failPut[name]; err != nil {
			return engine.SecretsWritten{}, err
		}
		if !storeGrammar.MatchString(name) || len(values[name]) > MaxValueBytes || !utf8.Valid(values[name]) {
			return engine.SecretsWritten{}, fmt.Errorf("invalid_body: %s", name)
		}
	}
	written := engine.SecretsWritten{Names: names, ReadBy: map[string][]string{}}
	for _, name := range names {
		e.entries[name] = fakeEntry{value: string(values[name]), by: self}
		written.ReadBy[name] = []string{}
		if e.reads[name] {
			written.ReadBy[name] = []string{"llm.api_key"}
		}
	}
	if slices.ContainsFunc(names, func(name string) bool { return e.reads[name] }) {
		written.Reload = e.reactivate()
	}
	return written, nil
}

func (e *fakeEngine) PutSecret(_ context.Context, name string, value []byte) error {
	e.calls = append(e.calls, "put-raw "+name)
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

func (e *fakeEngine) DeleteSecret(_ context.Context, name string) (engine.SecretRemoved, error) {
	e.calls = append(e.calls, "delete "+name)
	if err := e.failDelete[name]; err != nil {
		return engine.SecretRemoved{}, err
	}
	_, ok := e.entries[name]
	delete(e.entries, name)
	removed := engine.SecretRemoved{Removed: ok, ReadBy: []string{}}
	if e.reads[name] {
		removed.ReadBy = []string{"mcp.servers.env"}
		if ok {
			removed.Reload = e.reactivate()
		}
	}
	return removed, nil
}

// reactivate is the engine's re-activation at a write of a value the active
// revision reads: reload, or the next revision of reloaded.
func (e *fakeEngine) reactivate() *engine.Reload {
	if e.reload != nil {
		return e.reload
	}
	e.reloaded = append(e.reloaded, fmt.Sprintf("reloaded-%d", len(e.reloaded)+1))
	return &engine.Reload{RevisionID: e.reloaded[len(e.reloaded)-1], Epoch: int64(len(e.reloaded))}
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
	assertCalls(t, eng, "list", "put "+nameA+" "+nameB+" "+nameC)
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
	if !maps.EqualFunc(again.fingerprints, state.fingerprints, slices.Equal) || again.key != state.key {
		t.Fatal("the second Sync changed the state")
	}
}

// TestSyncWritesItsValuesAsOneBatch: the engine re-activates its active
// revision at every write of a value it reads, so a sync writes every value
// in one request, which is one activation, and reports what that
// re-activation did; a sync that writes nothing the revision reads reports
// none, and one whose re-activation failed says why.
func TestSyncWritesItsValuesAsOneBatch(t *testing.T) {
	eng := newFakeEngine()
	eng.reads = map[string]bool{nameA: true, nameC: true}
	desired := desiredOf(map[string]string{nameA: "a1", nameB: "b1", nameC: "c1"})

	state, res := mustSync(t, eng, desired, State{})
	assertCalls(t, eng, "list", "put "+nameA+" "+nameB+" "+nameC)
	if !slices.Equal(eng.reloaded, []string{"reloaded-1"}) {
		t.Fatalf("the engine re-activated %q, want one revision for the sync", eng.reloaded)
	}
	if !res.Reload.Landed() || res.Reload.RevisionID != "reloaded-1" || res.Reload.Epoch != 1 {
		t.Fatalf("Reload = %+v, want the engine's re-activation", res.Reload)
	}

	// Only a value nothing reads changes: nothing is re-activated.
	state, res = mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b2", nameC: "c1"}), state)
	assertCalls(t, eng, "list", "put "+nameB)
	if res.Reload != nil {
		t.Fatalf("Reload = %+v, want none for a value no revision reads", res.Reload)
	}

	// A re-activation that failed is the engine's 200 with its reason.
	eng.reload = &engine.Reload{Error: "revision_advanced", Detail: "another configuration write activated first"}
	_, res = mustSync(t, eng, desiredOf(map[string]string{nameA: "a2", nameB: "b2", nameC: "c1"}), state)
	assertCalls(t, eng, "list", "put "+nameA)
	if res.Reload == nil || res.Reload.Landed() || res.Reload.Error != "revision_advanced" {
		t.Fatalf("Reload = %+v, want the engine's failed re-activation", res.Reload)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a2", self}, nameB: {"b2", self}, nameC: {"c1", self}})
}

// TestSyncReportsTheLastReactivation: the engine re-activates its revision
// at a removal of a value it reads too, so what a sync reports is the
// re-activation at its last request that changed a value the revision reads:
// a removal of one comes after the write, and a removal of a value nothing
// reads changes nothing.
func TestSyncReportsTheLastReactivation(t *testing.T) {
	eng := newFakeEngine()
	eng.reads = map[string]bool{nameA: true, nameB: true}
	state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "b1", nameC: "c1"}), State{})
	eng.takeCalls()

	// C, read by nothing, goes after A's write: A's re-activation stands.
	state, res := mustSync(t, eng, desiredOf(map[string]string{nameA: "a2", nameB: "b1"}), state)
	assertCalls(t, eng, "list", "put "+nameA, "delete "+nameC)
	if want := eng.reloaded[len(eng.reloaded)-1]; res.Reload.RevisionID != want {
		t.Fatalf("Reload = %+v, want the write's, %s", res.Reload, want)
	}

	// B, which the revision reads, goes after A's write: its own stands.
	_, res = mustSync(t, eng, desiredOf(map[string]string{nameA: "a3"}), state)
	assertCalls(t, eng, "list", "put "+nameA, "delete "+nameB)
	if len(eng.reloaded) != 4 || res.Reload.RevisionID != eng.reloaded[3] {
		t.Fatalf("Reload = %+v after %v, want the removal's", res.Reload, eng.reloaded)
	}
}

// TestSyncSplitsALargeSetIntoBatches: the engine takes at most
// engine.MaxSecretBatch values a request, so a larger set goes in requests
// of that many, in name order, and the re-activation reported is the last
// one's, made with every value in the store. A batch that fails stops the
// sync with those before it recorded.
func TestSyncSplitsALargeSetIntoBatches(t *testing.T) {
	values := map[string]string{}
	for i := range 2*engine.MaxSecretBatch + 2 {
		values[StoreName(secretref.Key{Namespace: "ns", Name: fmt.Sprintf("s%03d", i), Key: tokenKey})] = fmt.Sprintf("v%d", i)
	}
	names := slices.Sorted(maps.Keys(values))
	first, second, third := names[:engine.MaxSecretBatch], names[engine.MaxSecretBatch:2*engine.MaxSecretBatch],
		names[2*engine.MaxSecretBatch:]

	eng := newFakeEngine()
	eng.reads = map[string]bool{}
	for _, name := range names {
		eng.reads[name] = true
	}
	boom := errors.New("engine unavailable")
	eng.failPut[second[0]] = boom
	partial, res, err := Sync(context.Background(), eng, desiredOf(values), State{}, self)
	if !errors.Is(err, boom) {
		t.Fatalf("Sync() error = %v, want the engine's", err)
	}
	assertCalls(t, eng, "list", "put "+strings.Join(first, " "), "put "+strings.Join(second, " "))
	if !slices.Equal(res.Put, first) {
		t.Fatalf("Put = %v, want the first batch alone", res.Put)
	}
	if got := slices.Sorted(maps.Keys(partial.fingerprints)); !slices.Equal(got, first) {
		t.Fatalf("the state records %d names, want the %d of the first batch", len(got), len(first))
	}

	delete(eng.failPut, second[0])
	_, res = mustSync(t, eng, desiredOf(values), partial)
	assertCalls(t, eng, "list", "put "+strings.Join(second, " "), "put "+strings.Join(third, " "))
	if !slices.Equal(res.Put, slices.Concat(second, third)) {
		t.Fatalf("Put = %v", res.Put)
	}
	if want := eng.reloaded[len(eng.reloaded)-1]; res.Reload.RevisionID != want {
		t.Fatalf("Reload = %+v, want the last batch's, %s", res.Reload, want)
	}
}

// TestSyncWritesAValueThatIsNotUTF8Alone: a JSON body cannot carry such a
// value unchanged, so it is written as raw bytes on its own, before the
// batch, whose re-activation, made with it in the store, is the one
// reported.
func TestSyncWritesAValueThatIsNotUTF8Alone(t *testing.T) {
	eng := newFakeEngine()
	eng.reads = map[string]bool{nameA: true, nameB: true}
	_, res := mustSync(t, eng, desiredOf(map[string]string{nameA: "a1", nameB: "\xff\xfe", nameC: "c1"}), State{})
	assertCalls(t, eng, "list", "put-raw "+nameB, "put "+nameA+" "+nameC)
	if !slices.Equal(res.Put, []string{nameA, nameB, nameC}) {
		t.Fatalf("Put = %v, want every name, sorted", res.Put)
	}
	if !res.Reload.Landed() {
		t.Fatalf("Reload = %+v, want the batch's re-activation", res.Reload)
	}
	assertStore(t, eng, map[string]fakeEntry{nameA: {"a1", self}, nameB: {"\xff\xfe", self}, nameC: {"c1", self}})

	// Written alone and last, its re-activation is not read: none reported.
	eng = newFakeEngine()
	_, res = mustSync(t, eng, desiredOf(map[string]string{nameB: "\xff"}), State{})
	assertCalls(t, eng, "list", "put-raw "+nameB)
	if res.Reload != nil {
		t.Fatalf("Reload = %+v, want none", res.Reload)
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
	assertCalls(t, eng, "list", "put "+nameA+" "+nameB)
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
	assertCalls(t, eng, "list", "put "+nameB+" "+nameC, "delete "+nameA)
}

// TestSyncReturnsWhatItAchieved: after a failure, the state returned records
// exactly what is known to be in the store, so saving it and retrying
// writes only what is missing, and nothing is deleted while a write is
// outstanding. A refused batch is refused whole, and one that failed inside
// the engine may have sealed any of its values, so none of them is claimed.
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
	assertCalls(t, eng, "list", "put "+nameA+" "+nameB)
	if len(res.Put) != 0 || len(res.Deleted) != 0 || res.Reload != nil {
		t.Fatalf("Result = %+v, want nothing written", res)
	}
	// The engine may or may not hold b2 now, so the state must not claim b1.
	for _, name := range []string{nameA, nameB} {
		if _, ok := partial.fingerprints[name]; ok {
			t.Errorf("the state records a fingerprint for %s, whose batch failed", name)
		}
	}

	delete(eng.failPut, nameB)
	eng.failDelete[nameC] = boom
	partial, res, err = Sync(context.Background(), eng, desired, partial, self)
	if !errors.Is(err, boom) {
		t.Fatalf("Sync() error = %v, want the engine's", err)
	}
	assertCalls(t, eng, "list", "put "+nameA+" "+nameB, "delete "+nameC)
	if !slices.Equal(res.Put, []string{nameA, nameB}) || len(res.Deleted) != 0 {
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

// revision3 is a third revision, which an engine node runs beside the
// fleet's.
const revision3 = "0191d4a6-5c1e-7000-8000-000000000003"

// dataNode is an engine node, named as the operator names each one: its
// pod's name.
const dataNode = "nimbus-data-1"

// TestSyncKeepsWhatEveryNodeRuns: the store serves every revision an engine
// node runs, not only the fleet's. A node that refused the fleet's revision
// runs the one before it, resolving its references from what every apply
// attempt reads of the store, and a data node starts on the revision its
// store names when it restarts, which is that one but while the engine
// retries moving it; each is kept the same way. So a name that revision
// references stays (KeepServed), and an absent one
// it reads as required is retained, and both are Held, the names the store
// keeps for it alone; what no revision kept references goes, as before. A
// node whose revision the operator does not know (KeepUnread) keeps
// everything. Without the served revision kept, the same sync deletes what
// it needs.
func TestSyncKeepsWhatEveryNodeRuns(t *testing.T) {
	// revision2 is the fleet's: it references nameA. revision1 is what a
	// node that refused revision2 runs: it references nameB and nameC,
	// whose Secret is gone, and lets neither go as far as the state knows.
	start := func(t *testing.T) (*fakeEngine, State, Desired) {
		t.Helper()
		eng := newFakeEngine()
		eng.entries[foreignName] = fakeEntry{value: foreignValue, by: access}
		state, _ := mustSync(t, eng, desiredOf(map[string]string{nameA: "a", nameB: "b", nameC: "c"}), State{})
		eng.takeCalls()
		next := desiredOf(map[string]string{nameA: "a"})
		next.absent = map[string]*MissingError{nameC: {Key: keyC, Reason: SecretNotFound}}
		next.Keep(revision2, nameA)
		return eng, state, next
	}
	everything := map[string]fakeEntry{
		nameA: {value: "a", by: self}, nameB: {value: "b", by: self}, nameC: {value: "c", by: self},
		foreignName: {value: foreignValue, by: access},
	}

	t.Run("a node runs the revision before the fleet's", func(t *testing.T) {
		eng, state, next := start(t)
		next.KeepServed(revision1, nameB, nameC)
		after, res := mustSync(t, eng, next, state)
		assertCalls(t, eng, "list")
		assertStore(t, eng, everything)
		if len(res.Deleted) != 0 || !slices.Equal(res.Retained, []string{nameC}) || !slices.Equal(res.Held, []string{nameB, nameC}) {
			t.Errorf("Deleted = %v, Retained = %v and Held = %v, want none, [%s] and [%s %s]",
				res.Deleted, res.Retained, res.Held, nameC, nameB, nameC)
		}
		if len(after.Withdrawn()) != 0 {
			t.Errorf("Withdrawn() = %v, want none: nothing was removed", after.Withdrawn())
		}
		if got := next.Holders(nameC, state); !slices.Equal(got, []string{revision1}) {
			t.Errorf("Holders(%s) = %v, want [%s]", nameC, got, revision1)
		}
		if got := next.Served(); !slices.Equal(got, []string{revision1}) || next.Running() != revision2 {
			t.Errorf("Served() = %v and Running() = %s, want [%s] and %s", got, next.Running(), revision1, revision2)
		}
	})

	t.Run("no node runs it any more", func(t *testing.T) {
		eng, state, next := start(t)
		_, res := mustSync(t, eng, next, state)
		assertCalls(t, eng, "list", "delete "+nameB, "delete "+nameC)
		if !slices.Equal(res.Deleted, []string{nameB, nameC}) || len(res.Held) != 0 {
			t.Errorf("Deleted = %v and Held = %v, want [%s %s] and none", res.Deleted, res.Held, nameB, nameC)
		}
	})

	t.Run("the revision a node runs lets the absent name go", func(t *testing.T) {
		eng, state, next := start(t)
		state = state.Adopted(revision1, []secretref.Key{keyC})
		next.KeepServed(revision1, nameB, nameC)
		after, res := mustSync(t, eng, next, state)
		assertCalls(t, eng, "list", "delete "+nameC)
		if !slices.Equal(res.Held, []string{nameB}) || !slices.Equal(after.Withdrawn(), []string{nameC}) {
			t.Errorf("Held = %v and Withdrawn() = %v, want [%s] and [%s]", res.Held, after.Withdrawn(), nameB, nameC)
		}
	})

	t.Run("a released name another revision requires", func(t *testing.T) {
		// The fleet's revision lets nameC go, as the state recorded when
		// the operator activated it; the node that refused it runs a
		// revision that requires it.
		eng, state, next := start(t)
		state = state.Activated(revision2, []secretref.Key{keyC})
		next.absent = nil
		next.Keep(revision2, nameA, nameC)
		next.Release(&MissingError{Key: keyC, Reason: SecretNotFound})
		served := next
		served.KeepServed(revision1, nameB, nameC)
		_, res := mustSync(t, eng, served, state)
		assertCalls(t, eng, "list")
		if !slices.Equal(res.Retained, []string{nameC}) || !slices.Equal(res.Held, []string{nameB, nameC}) {
			t.Errorf("Retained = %v and Held = %v, want [%s] and [%s %s]", res.Retained, res.Held, nameC, nameB, nameC)
		}
	})

	t.Run("a node whose revision is unknown", func(t *testing.T) {
		eng, state, next := start(t)
		next.KeepUnread(dataNode)
		_, res := mustSync(t, eng, next, state)
		assertCalls(t, eng, "list")
		assertStore(t, eng, everything)
		if !slices.Equal(res.Retained, []string{nameC}) || !slices.Equal(res.Held, []string{nameB, nameC}) ||
			!slices.Equal(next.Unread(), []string{dataNode}) {
			t.Errorf("Retained = %v, Held = %v and Unread() = %v", res.Retained, res.Held, next.Unread())
		}
	})

	t.Run("withdrawn while the next document is held", func(t *testing.T) {
		eng, state, _ := start(t)
		held := Desired{partial: true, absent: map[string]*MissingError{nameC: {Key: keyC, Reason: SecretNotFound}}}
		held.Keep(revision2, nameA)
		served := held
		served.KeepServed(revision1, nameB, nameC)
		_, res, err := Withdraw(context.Background(), eng, served, state)
		if err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		assertCalls(t, eng, "list")
		if len(res.Deleted) != 0 || !slices.Equal(res.Retained, []string{nameC}) || !slices.Equal(res.Held, []string{nameC}) {
			t.Errorf("Deleted = %v, Retained = %v and Held = %v, want none, [%s] and [%s]", res.Deleted, res.Retained, res.Held, nameC, nameC)
		}
		_, res, err = Withdraw(context.Background(), eng, held, state)
		if err != nil || !slices.Equal(res.Deleted, []string{nameC}) {
			t.Errorf("Withdraw without the served revision deleted %v (%v), want [%s]", res.Deleted, err, nameC)
		}
	})

	t.Run("a third revision", func(t *testing.T) {
		// Two nodes on two revisions before the fleet's, each keeping its
		// own.
		eng, state, next := start(t)
		next.KeepServed(revision1, nameB)
		next.KeepServed(revision3, nameC)
		_, res := mustSync(t, eng, next, state)
		assertCalls(t, eng, "list")
		if got := next.Served(); !slices.Equal(got, []string{revision1, revision3}) || !slices.Equal(res.Held, []string{nameB, nameC}) {
			t.Errorf("Served() = %v and Held = %v", got, res.Held)
		}
	})
}
