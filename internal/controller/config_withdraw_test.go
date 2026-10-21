package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// These tests hold what the operator takes out of the engine's secret store,
// and which revision it activates again, to the revision the engine runs: a
// fake engine records its revisions and its store, since what matters is the
// sequence of writes inside one reconcile, which the real engine's suite
// (config_test.go) sees only as where it ends up.

// storeEngine answers the engine's /health, /config and /secrets routes from
// a revision history, the last of which is active, and a store, as the
// operator's client reads them. A document it is to refuse is refused as the
// engine refuses an invalid one. beforeReload, afterReload and afterApply,
// when set, run once, under its lock, just before a reload is answered and
// just after a reload or an apply is: somebody else's write landing between
// two of the operator's.
type storeEngine struct {
	mu                        sync.Mutex
	revisions                 []storeRevision
	store                     map[string]string
	refuse                    bool
	beforeReload, afterReload func(e *storeEngine)
	afterApply                func(e *storeEngine)
}

type storeRevision struct {
	id, summary string
	document    []byte
}

// newStoreEngine runs the engine with document as its active revision r1.
func newStoreEngine(t *testing.T, document []byte) (*storeEngine, *engine.Client) {
	t.Helper()
	e := &storeEngine{revisions: []storeRevision{{id: "r1", document: document}}, store: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(server.Close)
	return e, engine.New(server.URL, "token")
}

func (e *storeEngine) serve(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	if r.Method == http.MethodPost && r.URL.Path == "/config/reload" && e.beforeReload != nil {
		hook := e.beforeReload
		e.beforeReload = nil
		hook(e)
	}
	active := e.revisions[len(e.revisions)-1]
	activate := func(document []byte) storeRevision {
		revision := storeRevision{id: fmt.Sprintf("r%d", len(e.revisions)+1), document: document, summary: r.Header.Get("X-Summary")}
		e.revisions = append(e.revisions, revision)
		return revision
	}
	activated := func(revision storeRevision) {
		answer(http.StatusCreated, map[string]any{"revision_id": revision.id, "epoch": len(e.revisions)})
	}
	name, isSecret := strings.CutPrefix(r.URL.Path, "/secrets/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		answer(http.StatusOK, map[string]any{"status": "ok", "configured": true, "posture": "serving", "applied_epoch": len(e.revisions)})
	case r.Method == http.MethodGet && r.URL.Path == configRoute:
		w.Header().Set("ETag", `"`+active.id+`"`)
		_, _ = w.Write(active.document)
	case r.Method == http.MethodPut && r.URL.Path == configRoute && e.refuse:
		answer(http.StatusUnprocessableEntity, map[string]any{"error": "validation_error", "detail": "refused",
			"problems": []map[string]string{{"path": "roles", "kind": "invalid", "message": "refused"}}})
	case r.Method == http.MethodPut && r.URL.Path == configRoute && r.URL.Query().Get("dry_run") == "true":
		answer(http.StatusOK, map[string]any{"base_revision_id": active.id})
	case r.Method == http.MethodPut && r.URL.Path == configRoute:
		applied := activate(body)
		if e.afterApply != nil {
			hook := e.afterApply
			e.afterApply = nil
			hook(e)
		}
		activated(applied)
	case r.Method == http.MethodPost && r.URL.Path == "/config/reload":
		reloaded := activate(active.document)
		if e.afterReload != nil {
			hook := e.afterReload
			e.afterReload = nil
			hook(e)
		}
		activated(reloaded)
	case r.Method == http.MethodGet && r.URL.Path == "/secrets":
		listing := make([]map[string]string, 0, len(e.store))
		for _, name := range slices.Sorted(maps.Keys(e.store)) {
			listing = append(listing, map[string]string{"name": name, "updated_by": operatorLogin, "operator_id": operatorLogin})
		}
		answer(http.StatusOK, map[string]any{"secrets": listing})
	case r.Method == http.MethodPut && isSecret:
		e.store[name] = string(body)
		answer(http.StatusOK, map[string]any{})
	case r.Method == http.MethodDelete && isSecret:
		_, ok := e.store[name]
		delete(e.store, name)
		answer(http.StatusOK, map[string]any{"removed": ok})
	default:
		http.NotFound(w, r)
	}
}

// ids are the engine's revisions, oldest first.
func (e *storeEngine) ids() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.revisions))
	for i, r := range e.revisions {
		out[i] = r.id
	}
	return out
}

// summaryOf is the summary of the active revision.
func (e *storeEngine) summaryOf() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.revisions[len(e.revisions)-1].summary
}

func (e *storeEngine) holds(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.store[name]
	return ok
}

// activateOther activates document as somebody else would, a revert or an
// edit through the engine's own API: the caller holds e's lock.
func (e *storeEngine) activateOther(document []byte) {
	e.revisions = append(e.revisions, storeRevision{
		id: fmt.Sprintf("r%d", len(e.revisions)+1), document: document, summary: "somebody else's",
	})
}

// configRoute is the engine's route of the company document.
const configRoute = "/config"

// running is the revision the engine runs when a withdrawal's reconcile
// starts: r1, with both values in its store as the operator pushed them.
type running struct {
	name string
	// requires is whether r1's server requires the registry; it lets it go
	// otherwise.
	requires bool
	// recorded is the revision the state records the operator's last
	// activation of, with the keys r1's document lets go without, "" for
	// none.
	recorded string
	// other is whether r1's document is not the one the reconcile renders,
	// which an edit somebody made through the engine stands for here, and
	// extra a block of it the operator does not render, which Diff leaves
	// out as an engine default.
	other bool
	extra string
}

// The cases of the running revision these tests share.
var (
	runningLetsGo   = running{name: "the running revision lets it go", recorded: "r1"}
	runningRequires = running{name: "the running revision requires it", requires: true, recorded: "r1"}
	runningOther    = running{name: "somebody else activated another document", recorded: "r0", other: true}
	runningNoRecord = running{name: "the state records no activation of another document", other: true}
	runningAdopted  = running{name: "somebody else activated the rendered document", recorded: "r0"}
	// runningReads is the rendered document with one more reference to the
	// registry, where it is required, in a block Diff does not compare.
	runningReads = running{name: "somebody else activated the rendered document reading it in a block of its own", recorded: "r0",
		extra: "tuning:\n  registry: ${%s}\n"}
)

// operatorLogin is the credential the engine records for the operator's
// writes.
var operatorLogin = engine.TokenLogin(render.TokenOperator)

// fixedEngine resolves every Company to one engine.
type fixedEngine struct{ client *engine.Client }

func (f fixedEngine) Engine(context.Context, *crewletaiv1alpha1.Company) (*engine.Client, error) {
	return f.client, nil
}

// withdrawal is a reconcile under Enforce of optionalRun's Company, whose
// server lets the registry go, while the engine runs revision r1 as running
// says; and then the registry's Secret is deleted.
type withdrawal struct {
	run      *configRun
	engine   *storeEngine
	client   *engine.Client
	cluster  client.Client
	events   *events.FakeRecorder
	registry string
	// requiring is a document whose server requires the registry.
	requiring []byte
	// stateWrites counts the writes of the secret sync's state since the
	// reconcile began, and failWrite, when it is not 0, is the one that
	// fails, the API server not answering.
	stateWrites, failWrite atomic.Int32
}

// errStateWrite is the API server's answer to the write failWrite names.
var errStateWrite = errors.New("the API server did not answer")

func newWithdrawal(t *testing.T, r running) *withdrawal {
	t.Helper()
	ctx := context.Background()
	w := &withdrawal{}
	tavily := valueSecret(requiredSecret, map[string]string{requiredKey: tavilyValue})
	npm := valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})

	requiring, server, _ := optionalRun(t)
	server.obj.(*crewletaiv1alpha1.MCPServer).Spec.OptionalKeys = nil
	requiringDoc, err := render.Company(requiring.input())
	if err != nil {
		t.Fatal(err)
	}
	w.requiring = requiringDoc.YAML
	run1, server, _ := optionalRun(t)
	if r.requires {
		server.obj.(*crewletaiv1alpha1.MCPServer).Spec.OptionalKeys = nil
	}
	if r.other {
		run1.company.Spec.Mission = "Edited through the engine."
	}
	runningDoc, err := render.Company(run1.input())
	if err != nil {
		t.Fatal(err)
	}
	served := runningDoc.YAML
	if r.extra != "" {
		registry := secretsync.StoreName(secretref.Key{Namespace: "ns", Name: optionalSecret, Key: optionalKey})
		served = append(slices.Clone(served), fmt.Sprintf(r.extra, registry)...)
	}
	fe, eng := newStoreEngine(t, served)

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := crewletaiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	company := run1.company.DeepCopy()
	company.Spec.Mission = ""
	company.UID = "6f1c1a5e-0000-4000-8000-000000000002"
	company.Status.Config.RevisionID = "r1"
	meta.SetStatusCondition(&company.Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionRuntimeReady, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid,
	})
	cluster := interceptor.NewClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(company, tavily, npm).WithStatusSubresource(company).Build(),
		interceptor.Funcs{Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			if n := w.stateWrites.Add(1); n == w.failWrite.Load() {
				return errStateWrite
			}
			return c.Apply(ctx, obj, opts...)
		}},
	)

	// The operator pushed both values and activated r1, or found r1
	// activated by somebody else.
	present, err := secretsync.Resolve(ctx, cluster, runningDoc.Refs, runningDoc.Optional)
	if err != nil {
		t.Fatal(err)
	}
	var state secretsync.State
	if r.recorded != "" {
		state = state.Activated(r.recorded, runningDoc.Optional)
	}
	state, _, err = secretsync.Sync(ctx, eng, present, state, operatorLogin)
	if err != nil {
		t.Fatal(err)
	}
	stateRef := types.NamespacedName{Namespace: company.Namespace, Name: secretSyncStateName(company)}
	if err := secretsync.SaveState(ctx, cluster, stateRef, company, state); err != nil {
		t.Fatal(err)
	}
	if err := cluster.Delete(ctx, npm); err != nil {
		t.Fatal(err)
	}
	w.stateWrites.Store(0)

	w.engine, w.client, w.cluster = fe, eng, cluster
	w.registry = secretsync.StoreName(secretref.Key{Namespace: "ns", Name: optionalSecret, Key: optionalKey})
	w.again(t, company)
	if !fe.holds(w.registry) {
		t.Fatal("the engine's store does not hold the registry the operator pushed")
	}
	return w
}

// again makes w.run a new reconcile of company, as a reconcile after the last
// one sees it, rendering optionalRun's document.
func (w *withdrawal) again(t *testing.T, company *crewletaiv1alpha1.Company) {
	t.Helper()
	run, _, _ := optionalRun(t)
	w.events = events.NewFakeRecorder(50)
	run.CompanyConfigReconciler = &CompanyConfigReconciler{
		Client: w.cluster, APIReader: w.cluster, Engines: fixedEngine{w.client}, Recorder: w.events,
	}
	run.company = company
	rendered, err := revisionPlacement(run.doc.YAML)
	if err != nil {
		t.Fatal(err)
	}
	run.rendered = &rendered
	w.run = run
}

// statusWritten is the Company as the reconcile's status write leaves it:
// the revision it recorded, if any.
func (w *withdrawal) statusWritten() *crewletaiv1alpha1.Company {
	company := w.run.company.DeepCopy()
	if w.run.config != nil {
		company.Status.Config = *w.run.config
	}
	return company
}

// state is the secret sync's state as the reconcile saved it.
func (w *withdrawal) state(t *testing.T) secretsync.State {
	t.Helper()
	ref := types.NamespacedName{Namespace: w.run.company.Namespace, Name: secretSyncStateName(w.run.company)}
	state, err := secretsync.LoadState(context.Background(), w.cluster, ref, w.run.company)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// resolve is what the reconcile reads of the Secrets its document
// references, which none holds back.
func (w *withdrawal) resolve(t *testing.T) secretsync.Desired {
	t.Helper()
	read, err := secretsync.Resolve(context.Background(), w.cluster, w.run.doc.Refs, w.run.doc.Optional)
	if err != nil {
		t.Fatal(err)
	}
	return read
}

// enforce runs the reconcile's enforce against the document the engine runs,
// as converge does once nothing holds the document.
func (w *withdrawal) enforce(t *testing.T) error {
	t.Helper()
	ctx := context.Background()
	if blocked, err := w.run.resolveSecrets(ctx); blocked || err != nil {
		t.Fatalf("the document is held: %t, %v", blocked, err)
	}
	active, err := activeConfig(ctx, w.client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.run.enforce(ctx, w.client, *w.run.desired, active)
	return err
}

// told reports whether the reconcile recorded a Normal event of reason, and
// warned whether a Warning one.
func told(recorded []string, reason string) bool {
	return slices.ContainsFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Normal "+reason+" ") })
}

func warned(recorded []string, reason string) bool {
	return slices.ContainsFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Warning "+reason+" ") })
}

// keptReading is what SecretKept and the conditions say of the revision that
// keeps a value: that it reads it as required, or that the operator takes it
// to.
func keptReading(r running) string {
	if r.other || r.recorded == "" || r.extra != "" {
		return "has no record of activating and does not render"
	}
	return "reads each as required"
}

// TestAHeldDocumentTakesOnlyWhatTheRunningRevisionLetsGo holds a reconcile
// whose next document cannot be applied to the revision the engine runs: the
// value of a deleted Secret goes from the store, and that revision activates
// again without it, only where the operator activated it and recorded it as
// letting the value go, or finds it running the document it renders; one
// that requires it, another document somebody else activated and a state
// that records no activation of it all keep it, activate nothing, and say so
// once, for as long as it is kept: a Secret that comes back and goes again
// is said again.
func TestAHeldDocumentTakesOnlyWhatTheRunningRevisionLetsGo(t *testing.T) {
	for _, tc := range []struct {
		running
		removed bool
	}{
		{running: runningLetsGo, removed: true},
		{running: runningAdopted, removed: true},
		{running: runningRequires},
		{running: runningOther},
		{running: runningNoRecord},
		{running: runningReads},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, tc.running)
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			recorded := drained(w.events)
			state := w.state(t)
			if tc.removed {
				if w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r1", "r2"}) ||
					!strings.Contains(w.engine.summaryOf(), w.registry) {
					t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it gone and r1 activated again "+
						"as r2", w.engine.holds(w.registry), w.engine.ids())
				}
				if !slices.Equal(state.LetGo("r2"), []string{w.registry}) || len(state.Withdrawn()) != 0 {
					t.Errorf("the state lets %v go for r2 and owes %v, want the registry and nothing", state.LetGo("r2"), state.Withdrawn())
				}
				if !told(recorded, "Reactivated") || told(recorded, "SecretKept") {
					t.Errorf("events %q, want Reactivated and no SecretKept", recorded)
				}
				return
			}
			if !w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r1"}) {
				t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it kept and nothing activated",
					w.engine.holds(w.registry), w.engine.ids())
			}
			if !slices.Equal(state.Retained(), []string{w.registry}) || len(state.Withdrawn()) != 0 {
				t.Errorf("the state retains %v and owes %v, want the registry and nothing", state.Retained(), state.Withdrawn())
			}
			if !told(recorded, "SecretKept") || told(recorded, "Reactivated") {
				t.Errorf("events %q, want SecretKept and no Reactivated", recorded)
			}
			if !slices.ContainsFunc(recorded, func(e string) bool { return strings.Contains(e, keptReading(tc.running)) }) {
				t.Errorf("events %q do not say the running revision %s", recorded, keptReading(tc.running))
			}

			// Said once: the next reconcile keeps it as quietly.
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			if again := drained(w.events); told(again, "SecretKept") {
				t.Errorf("the second reconcile tells again: %q", again)
			}

			// The Secret comes back while the document is held: nothing is
			// kept any more, and a deletion after is told again.
			npm := valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})
			if err := w.cluster.Create(ctx, npm); err != nil {
				t.Fatal(err)
			}
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			if got := w.state(t).Retained(); len(got) != 0 {
				t.Errorf("the state retains %v once the Secret is back, want nothing", got)
			}
			if err := w.cluster.Delete(ctx, npm); err != nil {
				t.Fatal(err)
			}
			drained(w.events)
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			if again := drained(w.events); !told(again, "SecretKept") {
				t.Errorf("the Secret was deleted again, and SecretKept is not told: %q", again)
			}
		})
	}
}

// TestAHeldDocumentSaysWhatTheStoreKeepsOfItself holds the Company's
// SecretsSynced message, which says why the document is held, to saying
// what the store keeps in a sentence of its own.
func TestAHeldDocumentSaysWhatTheStoreKeepsOfItself(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningRequires)
	if err := w.cluster.Delete(ctx, valueSecret(requiredSecret, nil)); err != nil {
		t.Fatal(err)
	}
	blocked, err := w.run.resolveSecrets(ctx)
	if err != nil || !blocked {
		t.Fatalf("the document is held: %t, %v; want it held for the tavily Secret", blocked, err)
	}
	if _, err := w.run.withdrawHeld(ctx, *w.run.read); err != nil {
		t.Fatal(err)
	}
	msg := w.run.secretsSynced.Message
	if !strings.HasPrefix(msg, "Secret ns/"+requiredSecret+" does not exist") ||
		!strings.Contains(msg, ". The engine's secret store keeps the value it held for the 1 the document lets go without") {
		t.Errorf("SecretsSynced says %q, want why the document is held, and then what the store keeps", msg)
	}
}

// TestARevisionThatLetsAValueGoGoesWithoutItWhateverTheNextDocumentReads:
// the revision the engine runs, which the operator activated and recorded as
// letting the registry go, goes without it once its Secret is deleted,
// whether the next document requires it, and is held for it, is refused by
// the render, so that nothing of it is read, or does not reference it and is
// refused by the engine; and is activated again without it. A revision that
// requires it keeps it.
func TestARevisionThatLetsAValueGoGoesWithoutItWhateverTheNextDocumentReads(t *testing.T) {
	// What the next document makes of the registry.
	const (
		requires   = "requires it"
		renderless = "is refused by the render"
		unread     = "does not reference it and is refused by the engine"
	)
	for _, tc := range []struct {
		next string
		running
		removed bool
	}{
		{next: requires, running: runningLetsGo, removed: true},
		{next: requires, running: runningRequires},
		{next: renderless, running: runningLetsGo, removed: true},
		{next: renderless, running: runningRequires},
		{next: unread, running: runningLetsGo, removed: true},
		{next: unread, running: runningRequires},
	} {
		t.Run("the next document "+tc.next+", "+tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, tc.running)
			p := w.run.part(render.ObjectRef{Kind: render.KindMCPServer, Name: optionalServer})
			server := p.obj.(*crewletaiv1alpha1.MCPServer)
			switch tc.next {
			case requires:
				server.Spec.OptionalKeys = nil
				doc, err := render.Company(w.run.input())
				if err != nil {
					t.Fatal(err)
				}
				w.run.doc = doc
				blocked, err := w.run.resolveSecrets(ctx)
				if err != nil || !blocked {
					t.Fatalf("the document is held: %t, %v; want it held for the registry", blocked, err)
				}
				if _, err := w.run.withdrawHeld(ctx, *w.run.read); err != nil {
					t.Fatal(err)
				}
			case renderless:
				w.run.doc = nil
				if _, err := w.run.withdrawHeld(ctx, secretsync.Desired{}); err != nil {
					t.Fatal(err)
				}
			default:
				server.Spec.Env = server.Spec.Env[:1]
				server.Spec.OptionalKeys = nil
				doc, err := render.Company(w.run.input())
				if err != nil {
					t.Fatal(err)
				}
				w.run.doc = doc
				w.engine.refuse = true
				if err := w.enforce(t); err != nil {
					t.Fatal(err)
				}
			}
			recorded := drained(w.events)
			if !tc.removed {
				if !w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r1"}) {
					t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it kept and nothing activated",
						w.engine.holds(w.registry), w.engine.ids())
				}
				return
			}
			if w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r1", "r2"}) {
				t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it gone and r1 activated again "+
					"as r2", w.engine.holds(w.registry), w.engine.ids())
			}
			if state := w.state(t); !slices.Equal(state.LetGo("r2"), []string{w.registry}) || len(state.Withdrawn()) != 0 {
				t.Errorf("the state lets %v go for r2 and owes %v, want the registry and nothing", state.LetGo("r2"), state.Withdrawn())
			}
			if !told(recorded, "Reactivated") || told(recorded, "SecretKept") {
				t.Errorf("events %q, want Reactivated and no SecretKept", recorded)
			}
		})
	}
}

// TestTheNextDocumentLetsGoWhatTheRunningRevisionRequired holds the apply of
// a document that lets go a value whose Secret is deleted. Where the running
// revision requires it, or is another document somebody else activated, the
// value stays in the store for that revision until the next one is
// activated, and goes then, and that one is activated again at once, in the
// same reconcile, so that the engine reads its store without it; a document
// the engine refuses activates nothing, the value stays, and SecretKept says
// so, while one that runs ends the reconcile with nothing kept to say. Where
// the running revision lets it go too, the operator's own or the document it
// renders, it goes before the apply, which is then the one activation it
// takes, or, the document refused, the running revision is activated again
// without it.
func TestTheNextDocumentLetsGoWhatTheRunningRevisionRequired(t *testing.T) {
	for _, tc := range []struct {
		running
		refused bool
		// revisions are the engine's revisions the reconcile ends with;
		// the last lets the registry go when it is gone from the store.
		revisions []string
		kept      bool
	}{
		{running: runningRequires, revisions: []string{"r1", "r2", "r3"}},
		{running: runningRequires, refused: true, revisions: []string{"r1"}, kept: true},
		{running: runningOther, revisions: []string{"r1", "r2", "r3"}},
		{running: runningOther, refused: true, revisions: []string{"r1"}, kept: true},
		{running: runningLetsGo, revisions: []string{"r1", "r2"}},
		{running: runningLetsGo, refused: true, revisions: []string{"r1", "r2"}},
		{running: runningAdopted, revisions: []string{"r1", "r2"}},
	} {
		name := tc.name
		if tc.refused {
			name += ", and the engine refuses the next"
		}
		t.Run(name, func(t *testing.T) {
			w := newWithdrawal(t, tc.running)
			w.engine.refuse = tc.refused
			if err := w.enforce(t); err != nil {
				t.Fatal(err)
			}
			recorded := drained(w.events)
			state := w.state(t)
			if got := w.engine.ids(); !slices.Equal(got, tc.revisions) || w.engine.holds(w.registry) != tc.kept {
				t.Fatalf("the revisions are %v and the store holds the registry: %t, want %v and %t", got,
					w.engine.holds(w.registry), tc.revisions, tc.kept)
			}
			if told(recorded, "SecretKept") != tc.kept {
				t.Errorf("events %q: SecretKept told %t, want %t", recorded, !tc.kept, tc.kept)
			}
			if tc.kept {
				if !slices.Equal(state.Retained(), []string{w.registry}) || len(state.Withdrawn()) != 0 {
					t.Errorf("the state retains %v and owes %v, want the registry and nothing", state.Retained(), state.Withdrawn())
				}
				msg := w.run.secretsSynced.Message
				if !strings.Contains(msg, "The engine's secret store keeps the value it held for the 1") ||
					!strings.Contains(msg, strings.TrimPrefix(keptReading(tc.running), "reads each")) {
					t.Errorf("SecretsSynced says %q, not what the store keeps and why", msg)
				}
				if strings.Contains(msg, "holds none for") {
					t.Errorf("SecretsSynced says %q: it holds none of what it keeps", msg)
				}
				return
			}
			last := tc.revisions[len(tc.revisions)-1]
			if !slices.Equal(state.LetGo(last), []string{w.registry}) || len(state.Withdrawn()) != 0 || len(state.Retained()) != 0 {
				t.Errorf("the state lets %v go for %s, owes %v and retains %v, want the registry, nothing and nothing",
					state.LetGo(last), last, state.Withdrawn(), state.Retained())
			}
			reactivated := len(tc.revisions) == 3 || tc.refused
			if told(recorded, "Reactivated") != reactivated {
				t.Errorf("events %q: Reactivated told %t, want %t", recorded, !reactivated, reactivated)
			}
			if reactivated && !strings.Contains(w.engine.summaryOf(), w.registry) {
				t.Errorf("revision %s is summarised %q, which does not name the registry", last, w.engine.summaryOf())
			}
			if !tc.refused && (w.run.config == nil || w.run.config.RevisionID != last || w.run.applied.revision != last) {
				t.Errorf("the reconcile records %+v and parts in %q, want %s", w.run.config, w.run.applied.revision, last)
			}
			if msg := w.run.secretsSynced.Message; !strings.Contains(msg, "It holds none for the 1") {
				t.Errorf("SecretsSynced says %q, not that the store holds none of the registry", msg)
			}
		})
	}
}

// TestAnApplyIsRecordedBeforeAnythingIsDeletedAfterIt: the apply of a
// document that lets go a value the running revision required activates it
// with the value in the store; the record of that activation is written
// before the value is deleted, so a write of the state that fails at either
// step leaves the next reconcile, which finds the revision running and the
// document applied, knowing that it lets the value go, and it deletes the
// value and activates the revision again.
func TestAnApplyIsRecordedBeforeAnythingIsDeletedAfterIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		// fails is the write of the state in the first reconcile that
		// fails: 1 before the apply, 2 its record, 3 after the deletion.
		fails int32
		// held is whether the next reconcile's document is another one,
		// held for a Secret, so that only the record says how the revision
		// the engine runs reads the value.
		held bool
	}{
		{name: "the record of the apply is lost", fails: 2},
		{name: "the record of the deletion is lost", fails: 3},
		{name: "the record of the deletion is lost, and the next document is held", fails: 3, held: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningRequires)
			w.failWrite.Store(tc.fails)
			if err := w.enforce(t); !errors.Is(err, errStateWrite) {
				t.Fatalf("the reconcile ends with %v, want the state write that failed", err)
			}
			if got, deleted := w.engine.ids(), !w.engine.holds(w.registry); !slices.Equal(got, []string{"r1", "r2"}) ||
				deleted != (tc.fails == 3) {
				t.Fatalf("the revisions are %v and the registry is deleted: %t, want r2 applied, and deleted: %t",
					got, deleted, tc.fails == 3)
			}

			w.failWrite.Store(0)
			w.again(t, w.statusWritten())
			if tc.held {
				w.run.company.Spec.Mission = "Held."
				doc, err := render.Company(w.run.input())
				if err != nil {
					t.Fatal(err)
				}
				w.run.doc = doc
				if err := w.cluster.Delete(ctx, valueSecret(requiredSecret, nil)); err != nil {
					t.Fatal(err)
				}
				if blocked, err := w.run.resolveSecrets(ctx); err != nil || !blocked {
					t.Fatalf("the document is held: %t, %v; want it held for the tavily Secret", blocked, err)
				}
				if _, err := w.run.withdrawHeld(ctx, *w.run.read); err != nil {
					t.Fatal(err)
				}
			} else if err := w.enforce(t); err != nil {
				t.Fatal(err)
			}
			if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2", "r3"}) || w.engine.holds(w.registry) {
				t.Fatalf("the revisions are %v and the store holds the registry: %t, want r2 activated again without it as r3",
					got, w.engine.holds(w.registry))
			}
			if state := w.state(t); !slices.Equal(state.LetGo("r3"), []string{w.registry}) || len(state.Withdrawn()) != 0 {
				t.Errorf("the state lets %v go for r3 and owes %v, want the registry and nothing", state.LetGo("r3"), state.Withdrawn())
			}
		})
	}
}

// TestAReloadIsRecordedOnlyForTheDocumentItRead: the engine activates again
// whichever revision is active when the request lands. Where somebody else
// activated another document between the operator's read and its request,
// or activated another revision after it, so that the operator cannot read
// back what its request activated, the revision activated again is not
// recorded as letting go what the one read lets go, nor as the operator's
// own, and a Warning says so.
func TestAReloadIsRecordedOnlyForTheDocumentItRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before bool
	}{
		{name: "another document activated before the request", before: true},
		{name: "the same document activated again after it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			reload := "r3"
			if tc.before {
				w.engine.beforeReload = func(e *storeEngine) { e.activateOther(w.requiring) }
			} else {
				reload = "r2"
				w.engine.afterReload = func(e *storeEngine) { e.activateOther(e.revisions[len(e.revisions)-1].document) }
			}
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			recorded := drained(w.events)
			if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2", "r3"}) {
				t.Fatalf("the revisions are %v, want r1, r2 and r3, the reload %s", got, reload)
			}
			state := w.state(t)
			if state.Records(reload) || len(state.LetGo(reload)) != 0 || len(state.Withdrawn()) != 0 {
				t.Errorf("the state records %s: %t, letting %v go, and owes %v; want %s unknown and nothing owed",
					reload, state.Records(reload), state.LetGo(reload), state.Withdrawn(), reload)
			}
			if !warned(recorded, "Reactivated") || told(recorded, "Reactivated") {
				t.Errorf("events %q, want a Warning Reactivated", recorded)
			}
			if w.run.config != nil && w.run.config.RevisionID == reload {
				t.Errorf("the reconcile records %s, which it could not confirm is its document, as its own", reload)
			}
		})
	}
}

// TestTheSyncAfterAnApplyDecidesByTheRevisionThatRuns: somebody activates
// another revision between the operator's apply and its sync after it, which
// keeps every value it references, that of a Secret deleted since included,
// and is not activated again.
func TestTheSyncAfterAnApplyDecidesByTheRevisionThatRuns(t *testing.T) {
	w := newWithdrawal(t, runningRequires)
	w.engine.afterApply = func(e *storeEngine) { e.activateOther(w.requiring) }
	if err := w.enforce(t); err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2", "r3"}) || !w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want r3 somebody else's, and kept for it",
			got, w.engine.holds(w.registry))
	}
	if state := w.state(t); state.Records("r3") || !slices.Equal(state.Retained(), []string{w.registry}) {
		t.Errorf("the state records r3: %t and retains %v, want r3 unknown and the registry kept", state.Records("r3"), state.Retained())
	}
	if recorded := drained(w.events); !told(recorded, "SecretKept") {
		t.Errorf("events %q: the reconcile ends with the value kept, and SecretKept is not told", recorded)
	}
}

// TestReactivateRepeatsOnlyARevisionThatLetsTheValueGo holds what the
// operator activates again for a value it removed: the running revision,
// where the operator activated it and recorded it as letting the value go,
// recorded as the revision that activates it again; and nothing where the
// running revision requires the value, or is one the operator does not know
// the reading of, which keeps what it read. Either way the removal is
// settled.
func TestReactivateRepeatsOnlyARevisionThatLetsTheValueGo(t *testing.T) {
	for _, tc := range []struct {
		running
		again bool
	}{
		{running: runningLetsGo, again: true},
		{running: runningRequires},
		{running: runningOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, tc.running)
			// Removed by a sync that kept nothing for the running revision.
			state, _, err := secretsync.Sync(ctx, w.client, w.resolve(t), w.state(t), operatorLogin)
			if err != nil || !slices.Equal(state.Withdrawn(), []string{w.registry}) {
				t.Fatalf("the sync withdrew %v (%v), want the registry", state.Withdrawn(), err)
			}
			next, err := w.run.reactivate(ctx, w.client, nil, state)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"r1"}
			if tc.again {
				want = append(want, "r2")
			}
			if got := w.engine.ids(); !slices.Equal(got, want) {
				t.Fatalf("the revisions are %v, want %v", got, want)
			}
			if len(next.Withdrawn()) != 0 {
				t.Errorf("the removal is owed still: %v", next.Withdrawn())
			}
			if tc.again && !slices.Equal(next.LetGo("r2"), []string{w.registry}) {
				t.Errorf("the revision that activated r1 again lets %v go, want the registry", next.LetGo("r2"))
			}
		})
	}
}
