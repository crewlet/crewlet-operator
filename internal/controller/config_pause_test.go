package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// pauseEngine stands in for an engine's roster (GET /agents) and its pause
// tools, whose answers a running engine cannot be made to give at will: a
// roster that fails or lies, and a tool that refuses.
type pauseEngine struct {
	mu sync.Mutex
	// roster is what GET /agents answers, by handle; rosterDown makes it
	// fail.
	roster     map[string]bool
	rosterDown bool
	// record is whether the coordination store holds each seat paused, which
	// the tools answer from and change; refuse makes them refuse.
	record map[string]bool
	refuse bool
	calls  []string
}

func (f *pauseEngine) client(t *testing.T) *engine.Client {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	tool := func(name string, paused bool) {
		server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				var args struct {
					Handle string `json:"handle"`
				}
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				f.calls = append(f.calls, name+" "+args.Handle)
				if f.refuse {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{
						Text: "lost the record to other writers four times in a row",
					}}}, nil
				}
				changed := f.record[args.Handle] != paused
				f.record[args.Handle] = paused
				answer := map[string]any{"handle": args.Handle, "outcome": "applied", "paused": paused, "changed": changed}
				if paused {
					answer["paused_by"], answer["paused_at"] = engine.TokenLogin(render.TokenOperator), "2026-10-09T08:00:00Z"
				}
				text, err := json.Marshal(answer)
				if err != nil {
					return nil, err
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil
			})
	}
	tool("pause_seat", true)
	tool("resume_seat", false)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle(engine.OperatorMCPPath, handler)
	mux.HandleFunc("GET /agents", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.rosterDown {
			http.Error(w, `{"error":"unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		rows := []map[string]any{}
		for handle, paused := range f.roster {
			row := map[string]any{"handle": handle}
			if paused {
				row["paused"] = map[string]any{"by": "jane", "at": "2026-10-09T07:00:00Z"}
			}
			rows = append(rows, row)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return engine.New(srv.URL, "test-bearer-value-0123456789")
}

// pauseRun is a reconcile of one Company under Enforce, whose one agent Seat,
// ana, sets spec.paused to want.
func pauseRun(want *bool) (*configRun, *part) {
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany, Spec: crewletaiv1alpha1.CompanySpec{
		Config: crewletaiv1alpha1.ConfigSettings{ManagementPolicy: crewletaiv1alpha1.ManagementPolicyEnforce},
	}}
	seat := &crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatAna, Generation: 2, Spec: crewletaiv1alpha1.SeatSpec{Paused: want}}
	p := newPart(seat)
	recorder := events.NewFakeRecorder(10)
	return &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Recorder: recorder}, company: company, parts: []*part{p}}, p
}

// TestAnOwnedPauseFollowsTheRecordNotTheRoster holds an owned pause to the
// coordination store's record, which the tools answer from: a roster that
// says the seat is paused when the record says it runs (a node that missed
// the resume) must not stop the operator from pausing it.
func TestAnOwnedPauseFollowsTheRecordNotTheRoster(t *testing.T) {
	f := &pauseEngine{roster: map[string]bool{seatAna: true}, record: map[string]bool{seatAna: false}}
	run, p := pauseRun(new(true))
	run.syncPauses(context.Background(), f.client(t), ctrl.Result{})
	if !f.record[seatAna] {
		t.Fatalf("the seat runs after the reconcile (calls %v): a stale roster held back the brake", f.calls)
	}
	if p.pause == nil || p.pause.status != metav1.ConditionTrue || p.pause.pause.By != engine.TokenLogin(render.TokenOperator) {
		t.Errorf("the verdict is %+v, want the record's pause, by the operator", p.pause)
	}
}

// TestAPauseThatCannotBeMadeIsAskedForAgainSoon holds a brake asked for to a
// quick retry: when the roster cannot be read, the pause is still taken, and
// a look comes again soon; when the engine refuses the call (contention), it
// is asked for again soon, not at the next resync.
func TestAPauseThatCannotBeMadeIsAskedForAgainSoon(t *testing.T) {
	t.Run("the roster cannot be read", func(t *testing.T) {
		f := &pauseEngine{rosterDown: true, record: map[string]bool{seatAna: false}}
		run, p := pauseRun(new(true))
		result := run.syncPauses(context.Background(), f.client(t), ctrl.Result{RequeueAfter: 5 * time.Minute})
		if !f.record[seatAna] {
			t.Errorf("the seat runs: the pause waited on the roster (calls %v)", f.calls)
		}
		if p.pause == nil || p.pause.status != metav1.ConditionTrue {
			t.Errorf("the verdict is %+v, want the pause the engine took", p.pause)
		}
		if result.RequeueAfter > engineRetryInterval {
			t.Errorf("the next look is in %s, want %s at most", result.RequeueAfter, engineRetryInterval)
		}
	})
	t.Run("the engine refuses the call", func(t *testing.T) {
		f := &pauseEngine{roster: map[string]bool{seatAna: false}, record: map[string]bool{seatAna: false}, refuse: true}
		run, p := pauseRun(new(true))
		result := run.syncPauses(context.Background(), f.client(t), ctrl.Result{RequeueAfter: 5 * time.Minute})
		if p.pause == nil || p.pause.reason != crewletaiv1alpha1.ReasonPauseRefused {
			t.Errorf("the verdict is %+v, want %s", p.pause, crewletaiv1alpha1.ReasonPauseRefused)
		}
		if result.RequeueAfter > engineRetryInterval {
			t.Errorf("the next look is in %s, want %s at most", result.RequeueAfter, engineRetryInterval)
		}
	})
}

// TestASeatTheEngineDoesNotRunHasNoPause takes the Paused condition off a
// Seat the engine runs no seat of, rather than leaving the last report of a
// seat that is gone.
func TestASeatTheEngineDoesNotRunHasNoPause(t *testing.T) {
	f := &pauseEngine{roster: map[string]bool{}, record: map[string]bool{}}
	run, p := pauseRun(nil)
	seat := p.obj.(*crewletaiv1alpha1.Seat)
	seat.Status.Conditions = []metav1.Condition{{Type: crewletaiv1alpha1.ConditionPaused, Status: metav1.ConditionTrue,
		Reason: crewletaiv1alpha1.ReasonSeatPaused, ObservedGeneration: 2}}
	seat.Status.Pause = crewletaiv1alpha1.SeatPauseStatus{PausedBy: "jane"}
	run.syncPauses(context.Background(), f.client(t), ctrl.Result{})
	decidePause(p, seat)
	if meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionPaused) != nil || seat.Status.Pause.PausedBy != "" {
		t.Errorf("the Seat still reports a pause: %+v %+v", seat.Status.Conditions, seat.Status.Pause)
	}
	if len(f.calls) > 0 {
		t.Errorf("the operator called %v for a seat the engine does not run", f.calls)
	}
}
