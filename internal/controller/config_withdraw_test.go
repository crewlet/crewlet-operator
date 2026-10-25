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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"

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

// storeEngine answers the engine's /health, /config, /config/revisions,
// /fleet and /secrets routes from a revision history, the last of which is
// the fleet's, and a store, as the operator's client reads them. A document
// it is to refuse is refused as the engine refuses an invalid one.
// beforeReload, afterReload and afterApply, when set, run once, under its
// lock, just before a reload is answered and just after a reload or an apply
// is: somebody else's write landing between two of the operator's.
//
// A write into its store (PUT /secrets, PUT /secrets/{name}), and a removal
// of a value (DELETE /secrets/{name}), re-activates the fleet's revision when its
// document reads one of the names, as the engine does, answering 200 with
// what that did; reloadAnswer, when set, is what it answers instead,
// activating nothing (nil for the engine's null, else a failed
// re-activation's object), and afterSecretsReload, when set, runs once just
// after the re-activation.
//
// Its fleet is one node, storeNode, running the fleet's revision, unless
// serving names what each node runs instead (another revision for one that
// refused the fleet's, "" for one that has recorded no attempt and so not
// said); runsNone names nodes beside those that refused the fleet's revision
// and run no company, as a seat pod that started on nothing and refused it
// does; fleetDown makes GET /fleet unanswerable, and missing names the
// revisions GET /config/revisions does not find. configDown, secretsDown and
// reloadDown make it answer a read of the fleet's document, a write to the
// store, and a reload of the fleet's revision, as an engine that cannot decide
// who is asking does; unreadable, when set, gives
// the body it answers a read of the fleet's document with, as an engine whose
// keyring does not open that revision does (config_unreadable_test.go).
//
// Each node's row says what it starts on as the engine's does: a data node
// (dataNode) the revision it runs, and a seat node, whose store is scratch,
// and a node that runs no company, none; boots names what a node starts on
// in place of that, as a data node whose record of it the engine has not
// moved yet to the revision it runs, "" for none.
type storeEngine struct {
	mu                        sync.Mutex
	revisions                 []storeRevision
	store                     map[string]string
	refuse                    bool
	configDown, secretsDown   bool
	reloadDown                bool
	beforeReload, afterReload func(e *storeEngine)
	afterApply                func(e *storeEngine)
	reloadAnswer              *map[string]any
	afterSecretsReload        func(e *storeEngine)
	serving                   map[string]string
	boots                     map[string]string
	runsNone                  map[string]bool
	fleetDown                 bool
	missing                   map[string]bool
	unreadable                func(revision string) string
}

// dataNode reports whether node is a pod of the Company's data StatefulSet,
// as the storeEngine's fleet names them: one whose store outlives a restart.
func dataNode(node string) bool {
	return strings.HasPrefix(node, "acme-data-")
}

// storeNode is the node of a storeEngine's fleet unless serving says
// otherwise.
const storeNode = "acme-data-0"

// down reports whether r is a request configDown, secretsDown or reloadDown
// makes the engine refuse: a read of the fleet's document, a write to the
// store, one name's or a batch's, or a reload of the fleet's revision.
func (e *storeEngine) down(r *http.Request) bool {
	if r.Method == http.MethodGet {
		return r.URL.Path == configRoute && e.configDown
	}
	if r.URL.Path == reloadRoute {
		return e.reloadDown
	}
	return (r.URL.Path == secretsRoute || strings.HasPrefix(r.URL.Path, secretsRoute+"/")) && e.secretsDown
}

// cannotDecide is the engine's answer to a request it cannot decide who is
// asking: its request guard's, which cannot read who presented the credential
// (crewlet's internal/api/auth, Caller). The authority table's own unknown
// never meets the operator's tokens on /config or /secrets, since they hold
// admin and admin is decided before the chart.
func cannotDecide(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "2")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"identity_unavailable",` +
		`"message":"Can't check who you are right now. Try again in a moment."}`))
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
	answer := func(status int, v any) { answerJSON(w, status, v) }
	if r.Method == http.MethodPost && r.URL.Path == reloadRoute && e.beforeReload != nil {
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
		answer(http.StatusCreated, map[string]any{revisionIDField: revision.id, "epoch": len(e.revisions)})
	}
	if e.down(r) {
		cannotDecide(w)
		return
	}
	if r.Method == http.MethodGet && e.serveServing(w, r.URL.Path, active) {
		return
	}
	if r.Method != http.MethodGet && e.serveSecretWrite(w, r, body, active, activate) {
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		answer(http.StatusOK, map[string]any{"status": "ok", "configured": true, "posture": engine.PostureServe,
			"applied_epoch": len(e.revisions)})
	case r.Method == http.MethodGet && r.URL.Path == configRoute:
		e.readActive(w, active)
	case r.Method == http.MethodPut && r.URL.Path == configRoute && e.refuse:
		answer(http.StatusBadRequest, map[string]any{"error": "validation_error", "detail": "refused",
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
	case r.Method == http.MethodPost && r.URL.Path == reloadRoute:
		reloaded := activate(active.document)
		if e.afterReload != nil {
			hook := e.afterReload
			e.afterReload = nil
			hook(e)
		}
		activated(reloaded)
	case r.Method == http.MethodGet && r.URL.Path == secretsRoute:
		listing := make([]map[string]string, 0, len(e.store))
		for _, name := range slices.Sorted(maps.Keys(e.store)) {
			listing = append(listing, map[string]string{"name": name, "updated_by": operatorLogin, "operator_id": operatorLogin})
		}
		answer(http.StatusOK, map[string]any{"secrets": listing})
	default:
		http.NotFound(w, r)
	}
}

// serveSecretWrite answers a write into the store or a removal from it,
// PUT /secrets, PUT /secrets/{name} and DELETE /secrets/{name}, and reports
// whether r is one of them. active is the fleet's revision and activate
// stores and activates a document; the caller holds e's lock.
func (e *storeEngine) serveSecretWrite(
	w http.ResponseWriter, r *http.Request, body []byte, active storeRevision, activate func([]byte) storeRevision,
) bool {
	reads := func(name string) bool { return strings.Contains(string(active.document), "${"+name+"}") }
	name, isSecret := strings.CutPrefix(r.URL.Path, "/secrets/")
	switch {
	case r.Method == http.MethodPut && r.URL.Path == secretsRoute:
		var batch struct {
			Values map[string]string `json:"values"`
		}
		if err := json.Unmarshal(body, &batch); err != nil || len(batch.Values) == 0 || len(batch.Values) > engine.MaxSecretBatch {
			answerJSON(w, http.StatusBadRequest, refusal("invalid_body", "not a batch"))
			return true
		}
		names := slices.Sorted(maps.Keys(batch.Values))
		readBy := map[string][]string{}
		var read []string
		for _, name := range names {
			e.store[name] = batch.Values[name]
			readBy[name] = []string{}
			if reads(name) {
				readBy[name] = []string{readPath}
				read = append(read, name)
			}
		}
		var reload any
		if len(read) > 0 {
			reload = e.reactivate(activate, active, "reload: secrets "+strings.Join(read, ", ")+" written")
		}
		answerJSON(w, http.StatusOK, map[string]any{"names": names, readByField: readBy, reloadField: reload})
	case r.Method == http.MethodPut && isSecret:
		e.store[name] = string(body)
		readBy, reload := []string{}, any(nil)
		if reads(name) {
			readBy = []string{readPath}
			reload = e.reactivate(activate, active, "reload: secret "+name+" written")
		}
		answerJSON(w, http.StatusOK, map[string]any{"name": name, readByField: readBy, reloadField: reload})
	case r.Method == http.MethodDelete && isSecret:
		_, ok := e.store[name]
		delete(e.store, name)
		readBy, reload := []string{}, any(nil)
		if reads(name) {
			readBy = []string{readPath}
			if ok {
				reload = e.reactivate(activate, active, "reload: secret "+name+" removed")
			}
		}
		answerJSON(w, http.StatusOK, map[string]any{"removed": ok, readByField: readBy, reloadField: reload})
	default:
		return false
	}
	return true
}

// readPath is the path of a document the storeEngine answers reads a name,
// and readByField, reloadField and revisionIDField the keys of the engine's
// answers that carry such paths, a re-activation and a revision.
const (
	readPath        = "mcp.servers.env"
	readByField     = "read_by"
	reloadField     = "reload"
	revisionIDField = "revision_id"
)

// reactivate is the engine's re-activation of active at a write or a
// removal of a value its document reads, with summary, as the answer's
// `reload` says it: reloadAnswer where it is set, activating nothing. The
// caller holds e's lock.
func (e *storeEngine) reactivate(activate func([]byte) storeRevision, active storeRevision, summary string) any {
	if e.reloadAnswer != nil {
		if *e.reloadAnswer == nil {
			return nil
		}
		return *e.reloadAnswer
	}
	reloaded := activate(active.document)
	reloaded.summary = summary
	e.revisions[len(e.revisions)-1] = reloaded
	if e.afterSecretsReload != nil {
		hook := e.afterSecretsReload
		e.afterSecretsReload = nil
		hook(e)
	}
	return map[string]any{revisionIDField: reloaded.id, "epoch": len(e.revisions)}
}

// serveServing answers what the engine says of what its nodes run, the GET
// routes /fleet and /config/revisions/{id} at path, and reports whether path
// is one of them. active is the fleet's revision; the caller holds e's lock.
func (e *storeEngine) serveServing(w http.ResponseWriter, path string, active storeRevision) bool {
	revisionID, isRevision := strings.CutPrefix(path, "/config/revisions/")
	switch {
	case path == fleetRoute && e.fleetDown:
		w.Header().Set("Retry-After", "1")
		answerJSON(w, http.StatusServiceUnavailable, refusal("unavailable", "coordination is unreachable"))
	case path == fleetRoute:
		serving := e.serving
		if serving == nil {
			serving = map[string]string{storeNode: active.id}
		}
		// The rows as the engine writes them: an attempt at the fleet's
		// epoch, which a node serving any other revision, or none, refused;
		// and the zero record of a node that has recorded no attempt. What
		// a node starts on is left out where it starts on none, as the
		// engine leaves it out.
		row := func(node string, epoch int, attempted string, status engine.ApplyStatus, revision string) map[string]any {
			roles, boots := []string{seatsRole}, ""
			if dataNode(node) {
				roles, boots = []string{dataRole}, revision
			}
			if b, ok := e.boots[node]; ok {
				boots = b
			}
			r := map[string]any{"id": node, "roles": roles, "config_epoch": epoch,
				"config_revision_id": attempted, "config_serving_revision_id": revision, "config_status": string(status)}
			if boots != "" && status != "" {
				r["config_boot_revision_id"] = boots
			}
			return r
		}
		nodes := make([]map[string]any, 0, len(serving)+len(e.runsNone))
		for _, node := range slices.Sorted(maps.Keys(serving)) {
			switch revision := serving[node]; revision {
			case "":
				nodes = append(nodes, row(node, 0, "", "", ""))
			case active.id:
				nodes = append(nodes, row(node, len(e.revisions), active.id, engine.ApplyOK, revision))
			default:
				nodes = append(nodes, row(node, len(e.revisions), active.id, engine.ApplyError, revision))
			}
		}
		for _, node := range slices.Sorted(maps.Keys(e.runsNone)) {
			nodes = append(nodes, row(node, len(e.revisions), active.id, engine.ApplyError, ""))
		}
		answerJSON(w, http.StatusOK, map[string]any{"this_node": storeNode, "nodes": nodes, "target_epoch": len(e.revisions)})
	case isRevision:
		i := slices.IndexFunc(e.revisions, func(rev storeRevision) bool { return rev.id == revisionID })
		if i < 0 || e.missing[revisionID] {
			answerJSON(w, http.StatusNotFound, refusal("not_found", "no revision "+revisionID))
			return true
		}
		payload, err := yaml.YAMLToJSON(e.revisions[i].document)
		if err != nil {
			answerJSON(w, http.StatusInternalServerError, refusal("internal", err.Error()))
			return true
		}
		answerJSON(w, http.StatusOK, map[string]any{revisionIDField: revisionID, "is_active": i == len(e.revisions)-1,
			"payload": json.RawMessage(payload)})
	default:
		return false
	}
	return true
}

// answerJSON writes v as the JSON answer, with status.
func answerJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// refusal is the engine's error envelope: its code and its detail.
func refusal(code, detail string) map[string]any {
	return map[string]any{"error": code, "detail": detail}
}

// readActive answers a read of the fleet's document, active: the document and
// its ETag, or, where unreadable is set, the engine's refusal of a revision
// its keyring does not open.
func (e *storeEngine) readActive(w http.ResponseWriter, active storeRevision) {
	if e.unreadable != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(e.unreadable(active.id)))
		return
	}
	w.Header().Set("ETag", `"`+active.id+`"`)
	_, _ = w.Write(active.document)
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

// configRoute is the engine's route of the company document, reloadRoute the
// one that activates it again, fleetRoute that of its fleet, secretsRoute
// that of its secret store, and dataRole and seatsRole the roles of a data
// pod's node and a seat pod's there.
const (
	configRoute  = "/config"
	reloadRoute  = "/config/reload"
	fleetRoute   = "/fleet"
	secretsRoute = "/secrets"
	dataRole     = "data"
	seatsRole    = "seats"
)

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
	// Pushed before r1 activated, so that r1 read them: nothing is
	// re-activated for them.
	fe.reloadAnswer = new(map[string]any(nil))
	state, _, err = secretsync.Sync(ctx, eng, present, state, operatorLogin)
	if err != nil {
		t.Fatal(err)
	}
	fe.reloadAnswer = nil
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
	_, err := w.enforced(t)
	return err
}

// enforced is enforce, with when the reconcile asks to run again.
func (w *withdrawal) enforced(t *testing.T) (ctrl.Result, error) {
	t.Helper()
	ctx := context.Background()
	if blocked, err := w.run.resolveSecrets(ctx); blocked || err != nil {
		t.Fatalf("the document is held: %t, %v", blocked, err)
	}
	active, err := activeConfig(ctx, w.client)
	if err != nil {
		t.Fatal(err)
	}
	return w.run.enforce(ctx, w.client, *w.run.desired, active)
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
// renders, it goes before the apply, and the engine activates the running
// revision again at its removal, which reads it: that is the document the
// operator renders, so it is the one activation it takes, recorded as the
// operator's apply, and nothing is dry-run that the engine could refuse.
func TestTheNextDocumentLetsGoWhatTheRunningRevisionRequired(t *testing.T) {
	for _, tc := range []struct {
		running
		refused bool
		// revisions are the engine's revisions the reconcile ends with;
		// the last lets the registry go when it is gone from the store.
		revisions []string
		kept      bool
		// byEngine is the engine's re-activation at the removal, the
		// document being the one the engine runs, taken for the apply
		// before anything is dry-run.
		byEngine bool
	}{
		{running: runningRequires, revisions: []string{"r1", "r2", "r3"}},
		{running: runningRequires, refused: true, revisions: []string{"r1"}, kept: true},
		{running: runningOther, revisions: []string{"r1", "r2", "r3"}},
		{running: runningOther, refused: true, revisions: []string{"r1"}, kept: true},
		{running: runningLetsGo, revisions: []string{"r1", "r2"}},
		{running: runningLetsGo, refused: true, revisions: []string{"r1", "r2"}, byEngine: true},
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
			reactivated := len(tc.revisions) == 3 || tc.refused && !tc.byEngine
			if told(recorded, "Reactivated") != reactivated {
				t.Errorf("events %q: Reactivated told %t, want %t", recorded, !reactivated, reactivated)
			}
			if reactivated && !strings.Contains(w.engine.summaryOf(), w.registry) {
				t.Errorf("revision %s is summarised %q, which does not name the registry", last, w.engine.summaryOf())
			}
			if (!tc.refused || tc.byEngine) &&
				(w.run.config == nil || w.run.config.RevisionID != last || w.run.applied.revision != last) {
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
// value and activates the revision again, unless the engine did so at a
// removal that was made.
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
			// The engine activates r2 again itself as the registry, which
			// r2 reads, is removed: a record of the deletion lost is one of
			// that activation lost too.
			first := []string{"r1", "r2"}
			if tc.fails == 3 {
				first = append(first, "r3")
			}
			if got, deleted := w.engine.ids(), !w.engine.holds(w.registry); !slices.Equal(got, first) ||
				deleted != (tc.fails == 3) {
				t.Fatalf("the revisions are %v and the registry is deleted: %t, want %v, and deleted: %t",
					got, deleted, first, tc.fails == 3)
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
			// Activated again without it: by the engine at the removal, or,
			// where the record of that was lost, at the operator's request,
			// where it finds the engine running the document it renders, and
			// so knows that revision lets the value go. A held document is
			// not that one: the revision the engine activated at the removal
			// is one whose reading the operator does not know, which needs no
			// activation for a value it already went without.
			want := []string{"r1", "r2", "r3"}
			if tc.fails == 3 && !tc.held {
				want = append(want, "r4")
			}
			last := want[len(want)-1]
			if got := w.engine.ids(); !slices.Equal(got, want) || w.engine.holds(w.registry) {
				t.Fatalf("the revisions are %v and the store holds the registry: %t, want %v, the last activating r2's "+
					"document again without it", got, w.engine.holds(w.registry), want)
			}
			state := w.state(t)
			if len(state.Withdrawn()) != 0 {
				t.Errorf("the state owes %v, want nothing", state.Withdrawn())
			}
			if letsGo := tc.fails != 3 || !tc.held; letsGo != slices.Equal(state.LetGo(last), []string{w.registry}) {
				t.Errorf("the state lets %v go for %s, want the registry: %t", state.LetGo(last), last, letsGo)
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
// own, and a Warning says so. So with the engine's own re-activation at the
// removal, where another revision overtook it; the operator's request is
// made where the engine's failed.
func TestAReloadIsRecordedOnlyForTheDocumentItRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before bool
		// byEngine is the engine's own re-activation at the removal, which
		// otherwise fails.
		byEngine bool
	}{
		{name: "another document activated before the request", before: true},
		{name: "the same document activated again after it"},
		{name: "the same document activated again after the engine's own re-activation", byEngine: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			reload := "r3"
			switch {
			case tc.byEngine:
				reload = "r2"
				w.engine.afterSecretsReload = func(e *storeEngine) { e.activateOther(e.revisions[len(e.revisions)-1].document) }
			case tc.before:
				w.engine.beforeReload = func(e *storeEngine) { e.activateOther(w.requiring) }
			default:
				reload = "r2"
				w.engine.afterReload = func(e *storeEngine) { e.activateOther(e.revisions[len(e.revisions)-1].document) }
			}
			if !tc.byEngine {
				w.engine.reloadAnswer = failedReload("unavailable", "the coordination store did not answer")
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
// settled. Where the engine activated the running revision again itself, at
// the removal of a value it reads, that is the activation, and the operator
// asks for none.
func TestReactivateRepeatsOnlyARevisionThatLetsTheValueGo(t *testing.T) {
	for _, tc := range []struct {
		running
		again bool
	}{
		{running: runningLetsGo, again: true},
		{running: runningRequires},
		{running: runningOther},
	} {
		for _, byEngine := range []bool{false, true} {
			name := tc.name
			if byEngine {
				name += ", and the engine activated it again at the removal"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				w := newWithdrawal(t, tc.running)
				if !byEngine {
					w.engine.reloadAnswer = new(map[string]any(nil))
				}
				active, err := activeConfig(ctx, w.client)
				if err != nil {
					t.Fatal(err)
				}
				// Removed by a sync that kept nothing for the running revision.
				state, res, err := secretsync.Sync(ctx, w.client, w.resolve(t), w.state(t), operatorLogin)
				if err != nil || !slices.Equal(state.Withdrawn(), []string{w.registry}) {
					t.Fatalf("the sync withdrew %v (%v), want the registry", state.Withdrawn(), err)
				}
				if res.Reload.Landed() != byEngine {
					t.Fatalf("the sync reports the engine's re-activation %+v", res.Reload)
				}
				next, err := w.run.reactivate(ctx, w.client, active, res.Reload, res.ReloadAction, state)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"r1"}
				if tc.again || byEngine {
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
				if !tc.again && next.Records("r2") {
					t.Errorf("the state records r2, which activated a revision that does not let the registry go")
				}
				if recorded := drained(w.events); tc.again && !told(recorded, "Reactivated") {
					t.Errorf("events %q, want Reactivated", recorded)
				}
			})
		}
	}
}

// actionRecorder records the action of every event, which events.FakeRecorder
// drops.
type actionRecorder struct{ actions []string }

func (r *actionRecorder) Eventf(_ runtime.Object, _ runtime.Object, _, reason, action, _ string, _ ...any) {
	r.actions = append(r.actions, reason+" "+action)
}

// TestAReactivationByTheEngineNamesTheRequestThatMadeIt: the engine
// activates its revision again at a write of a value it reads as much as at
// a removal, and the Reactivated event names the engine call whose request
// did it, the secret sync's (secretsync.Result.ReloadAction). Every one was
// labelled DeleteSecret, the write that re-activated in the push before an
// apply the engine refused included.
func TestAReactivationByTheEngineNamesTheRequestThatMadeIt(t *testing.T) {
	for _, action := range []string{secretsync.ActionPutSecrets, secretsync.ActionWriteSecret,
		secretsync.ActionDeleteSecret} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			active, err := activeConfig(ctx, w.client)
			if err != nil {
				t.Fatal(err)
			}
			state, res, err := secretsync.Sync(ctx, w.client, w.resolve(t), w.state(t), operatorLogin)
			if err != nil || !res.Reload.Landed() {
				t.Fatalf("the sync = (%+v, %v), want the engine's re-activation", res.Reload, err)
			}
			recorder := &actionRecorder{}
			w.run.Recorder = recorder
			if _, err := w.run.reactivate(ctx, w.client, active, res.Reload, action, state); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(recorder.actions, "Reactivated "+action) {
				t.Errorf("events %q, want Reactivated by %s", recorder.actions, action)
			}
		})
	}
}

// TestARefusedDocumentComesBackWhenItsWithdrawalReportSays holds a document
// the engine refused, after which the activation owed could not be made, to
// the retry the SecretWithdrawalFailed event promises: here the engine's own
// two seconds. It came back ten seconds on whatever the event said, and a
// revision the engine cannot open, which the event says is looked at again
// at the resync interval, ten seconds on as well.
func TestARefusedDocumentComesBackWhenItsWithdrawalReportSays(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// r1 lets the registry go, and an earlier sync withdrew it: an
	// activation without it is owed.
	read := w.resolve(t)
	read.Keep("r1", w.registry)
	ref := types.NamespacedName{Namespace: w.run.company.Namespace, Name: secretSyncStateName(w.run.company)}
	owed, _, err := secretsync.Sync(ctx, w.client, read, w.state(t), operatorLogin)
	if err != nil {
		t.Fatal(err)
	}
	if err := secretsync.SaveState(ctx, w.cluster, ref, w.run.company, owed); err != nil {
		t.Fatal(err)
	}
	// The dry run is refused, and the activation owed then meets an engine
	// that cannot say who is asking, for two seconds.
	w.engine.refuse = true
	w.engine.reloadDown = true
	result, err := w.run.converge(ctx, w.client)
	if err != nil {
		t.Fatalf("converge() = %v, want the refusal reported", err)
	}
	recorded := drained(w.events)
	i := slices.IndexFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Warning SecretWithdrawalFailed ") })
	if i < 0 || !strings.Contains(recorded[i], "tries again in 2s") || result.RequeueAfter != 2*time.Second {
		t.Errorf("the next look is %s on and events %q; want SecretWithdrawalFailed promising 2s, and 2s",
			result.RequeueAfter, recorded)
	}
}
