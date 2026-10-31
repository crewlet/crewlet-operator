package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
)

// These tests drive verifyApplied with a fake engine that answers /health and
// /fleet as the engine writes them, because the states that matter here — an
// apply that fails, an engine that never applies — are ones a valid document
// never puts the real engine in. The real engine's ordinary path, an apply
// that reaches ConfigApplied=True, is the envtest suite's (config_test.go).

// fakeEngine answers /health and /fleet with the bodies it holds, /fleet
// with fleetStatus where it is set, and counts the /fleet reads.
type fakeEngine struct {
	health, fleet string
	fleetStatus   int
	fleetReads    int
}

func (f *fakeEngine) client(t *testing.T) *engine.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(f.health))
		case fleetRoute:
			f.fleetReads++
			if f.fleetStatus != 0 {
				w.WriteHeader(f.fleetStatus)
			}
			_, _ = w.Write([]byte(f.fleet))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return engine.New(server.URL, "token")
}

// health is a /health body; fleet a /fleet body whose one node recorded the
// attempt given.
func health(status, posture string, applied int) string {
	return `{"status":"` + status + `","configured":true,"posture":"` + posture + `","applied_epoch":` + strconv.Itoa(applied) + `}`
}

func fleet(epoch int, revision, status, account string) string {
	return fleetServing(epoch, revision, status, account, "", "")
}

// fleetServing is fleet whose node runs serving and starts on boots, as a
// node that refused revision runs the one it ran before, and starts on it but
// while the engine retries moving what it starts on; an empty boots is left
// out, as the engine leaves it out of the row of a node that starts on none.
func fleetServing(epoch int, revision, status, account, serving, boots string) string {
	bootField := ""
	if boots != "" {
		bootField = `,"config_boot_revision_id":"` + boots + `"`
	}
	return `{"this_node":"node-0","nodes":[{"id":"node-0","config_epoch":` + strconv.Itoa(epoch) +
		`,"config_revision_id":"` + revision + `","config_status":"` + status + `","config_error":"` + account +
		`","config_serving_revision_id":"` + serving + `"` + bootField + `}]}`
}

// verifyRun is a reconcile that wrote revision r2 as epoch 2 at writtenAt,
// and would report verdict were the engine running it; the Company's
// ConfigApplied says current (nil for none).
func verifyRun(now, writtenAt time.Time, verdict string, current *metav1.Condition) (*configRun, *eventLog) {
	log := &eventLog{}
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: "acme"}
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{
		RevisionID: "r2", Epoch: new(int64(2)), AppliedAt: &metav1.Time{Time: writtenAt},
	}
	if current != nil {
		company.Status.Conditions = []metav1.Condition{*current}
	}
	run := &configRun{
		CompanyConfigReconciler: &CompanyConfigReconciler{Recorder: log, now: func() time.Time { return now }},
		company:                 company,
		configApplied: &metav1.Condition{
			Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: verdict,
			Message: verdict + " revision r2.",
		},
		applied: appliedVerdict{kind: appliedInRevision, revision: "r2"},
	}
	return run, log
}

func condition(reason string) *metav1.Condition {
	return &metav1.Condition{Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse, Reason: reason}
}

// TestConfigAppliedWaitsForTheEngineToApplyTheEpoch pins that a revision the
// engine has activated and not applied is Applying, its parts with it, and
// that once the engine reports the epoch applied the Company reports the
// write's own verdict, which the Applying look remembered, with an Applied
// event.
func TestConfigAppliedWaitsForTheEngineToApplyTheEpoch(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	eng := &fakeEngine{health: health("ok", "wait", 1), fleet: fleet(1, "r1", "ok", "")}
	cl := eng.client(t)

	run, log := verifyRun(written.Add(time.Second), written, crewletaiv1alpha1.ReasonDriftCorrected, nil)
	h, err := cl.Health(context.Background())
	result := run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{RequeueAfter: applyPollInterval})
	if got := run.configApplied; got.Status != metav1.ConditionFalse || got.Reason != crewletaiv1alpha1.ReasonApplying ||
		!strings.Contains(got.Message, "epoch 2") {
		t.Fatalf("ConfigApplied while the engine runs epoch 1 = %+v, want False, Applying, naming epoch 2", got)
	}
	if run.applied.kind != appliedPending {
		t.Errorf("parts' verdict = %v, want appliedPending", run.applied.kind)
	}
	if result.RequeueAfter != applyPollInterval {
		t.Errorf("RequeueAfter = %s, want %s", result.RequeueAfter, applyPollInterval)
	}

	// The next look finds the document in sync and would say Applied; the
	// engine has applied epoch 2 by then.
	eng.health = health("ok", "serve", 2)
	next, _ := verifyRun(written.Add(3*time.Second), written, crewletaiv1alpha1.ReasonApplied, condition(crewletaiv1alpha1.ReasonApplying))
	next.CompanyConfigReconciler = run.CompanyConfigReconciler
	next.company.Status.Conditions = []metav1.Condition{*condition(crewletaiv1alpha1.ReasonApplying)}
	reads := eng.fleetReads
	h, err = cl.Health(context.Background())
	next.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
	if got := next.configApplied; got.Status != metav1.ConditionTrue || got.Reason != crewletaiv1alpha1.ReasonDriftCorrected {
		t.Fatalf("ConfigApplied once the engine applied epoch 2 = %+v, want True with the write's DriftCorrected", got)
	}
	if next.applied.kind != appliedInRevision {
		t.Errorf("parts' verdict = %v, want appliedInRevision", next.applied.kind)
	}
	if eng.fleetReads != reads {
		t.Errorf("read /fleet for an engine that applied the epoch and has not diverged")
	}
	if events := log.of("acme"); len(events) != 1 || !strings.HasPrefix(events[0], "Normal Applied The engine applied revision r2 (epoch 2)") {
		t.Errorf("events = %q, want one Normal Applied naming revision r2 and epoch 2", events)
	}
}

// TestConfigAppliedTakesAWaitPostureForAnApplyInProgress pins that the
// verdict follows the engine's posture. A node reads wait for the whole of an
// apply and turns diverged only once an attempt has ended in failure, so wait
// is Applying whatever the record in /fleet says, and costs no read of it: a
// record of a failed attempt beside a wait posture is one the engine does not
// count against the epoch it is applying.
func TestConfigAppliedTakesAWaitPostureForAnApplyInProgress(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	eng := &fakeEngine{health: health("ok", "wait", 1), fleet: fleet(2, "r2", "error", "engine: apply: no sandbox cell")}
	cl := eng.client(t)
	run, log := verifyRun(written.Add(time.Second), written, crewletaiv1alpha1.ReasonApplied, nil)
	h, err := cl.Health(context.Background())
	run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
	if got := run.configApplied; got.Reason != crewletaiv1alpha1.ReasonApplying {
		t.Fatalf("ConfigApplied while the engine reads wait = %+v, want Applying", got)
	}
	if eng.fleetReads != 0 {
		t.Errorf("read /fleet %d times for an engine whose posture says it is still applying", eng.fleetReads)
	}
	if events := log.of("acme"); len(events) != 0 {
		t.Errorf("events = %q, want none", events)
	}
}

// TestConfigAppliedTakesADivergedPostureForAFailure pins that a diverged
// posture is ApplyFailed at once, with or without an account in /fleet: the
// engine has concluded, and its record either lags its posture by a moment
// (the account arrives on the next look) or has nothing to say (an apply past
// the engine's grace while a peer applied the epoch, on the operator's epoch
// or on one activated after it). Waiting for the record would only delay a
// failure the engine has already reported.
func TestConfigAppliedTakesADivergedPostureForAFailure(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name, health, fleet, want string
	}{
		{"before the account reaches the record", health("isolated", "isolated", 1), fleet(1, "r1", "ok", ""),
			"records no failed attempt"},
		{"past the grace beside a peer", health("shed", "shed", 1), fleet(1, "r1", "ok", ""), "45s grace"},
		{"on an epoch activated after the operator's", health("shed", "shed", 2), `{"this_node":"node-0","target_epoch":3,` +
			`"nodes":[{"id":"node-0","config_epoch":2,"config_revision_id":"r2","config_status":"ok"}]}`, "apply epoch 3"},
		{"with no record to read", health("isolated", "isolated", 1), `not json`, "could not be read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := (&fakeEngine{health: c.health, fleet: c.fleet}).client(t)
			run, log := verifyRun(written.Add(time.Second), written, crewletaiv1alpha1.ReasonApplied, nil)
			h, err := cl.Health(context.Background())
			result := run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
			got := run.configApplied
			if got.Status != metav1.ConditionFalse || got.Reason != crewletaiv1alpha1.ReasonApplyFailed {
				t.Fatalf("ConfigApplied = %+v, want False, ApplyFailed", got)
			}
			for _, want := range []string{c.want, `posture "` + string(h.Posture) + `"`} {
				if !strings.Contains(got.Message, want) {
					t.Errorf("message %q does not carry %q", got.Message, want)
				}
			}
			if run.applied.kind != appliedFailed {
				t.Errorf("parts' verdict = %v, want appliedFailed", run.applied.kind)
			}
			if events := log.of("acme"); len(events) != 1 || !strings.HasPrefix(events[0], "Warning ApplyFailed") {
				t.Errorf("events = %q, want one Warning ApplyFailed", events)
			}
			if result.RequeueAfter != engineRetryInterval {
				t.Errorf("RequeueAfter = %s, want %s", result.RequeueAfter, engineRetryInterval)
			}
		})
	}
}

// TestConfigAppliedReportsAFailedApply pins ApplyFailed for an engine that
// recorded a failed attempt at the epoch, or at a later one after it had
// applied the operator's (a resync), with the engine's status, posture and
// account in the message, a Warning event, and the parts not applied.
func TestConfigAppliedReportsAFailedApply(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name, health, fleet string
		// runs is what the message says the engine runs meanwhile.
		runs string
	}{
		{"at the epoch", health("isolated", "isolated", 1), fleet(2, "r2", "error", "engine: apply: no sandbox cell"),
			"keeps running epoch 1"},
		{"at a later epoch", health("isolated", "isolated", 2), fleet(3, "r3", "error", "engine: apply: no sandbox cell"),
			"keeps running epoch 2"},
		{"degraded", health("shed", "shed", 1), fleet(2, "r2", "degraded", "engine: apply: no sandbox cell"),
			"keeps running epoch 1"},
		// Restarted onto the revision it ran, the node reports no epoch
		// applied: its record names the revision, and the one it starts on.
		{"after a restart", health("isolated", "isolated", 0),
			fleetServing(2, "r2", "error", "engine: apply: no sandbox cell", "r1", "r1"),
			"keeps running revision r1, the last it applied or started on, which it starts on again if it restarts."},
		// Its record of what it starts on not moved yet to the revision it
		// runs, it starts on the one before if it restarts.
		{"starting on another revision", health("isolated", "isolated", 0),
			fleetServing(3, "r3", "error", "engine: apply: no sandbox cell", "r2", "r1"),
			"keeps running revision r2, the last it applied or started on, and starts on revision r1 if it restarts"},
		{"starting on none", health("isolated", "isolated", 0),
			fleetServing(2, "r2", "error", "engine: apply: no sandbox cell", "r1", ""),
			"keeps running revision r1, the last it applied or started on, and starts on no revision if it restarts"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := (&fakeEngine{health: c.health, fleet: c.fleet}).client(t)
			run, log := verifyRun(written.Add(time.Hour), written, crewletaiv1alpha1.ReasonApplied, nil)
			h, err := cl.Health(context.Background())
			result := run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{RequeueAfter: driftCheckInterval})
			got := run.configApplied
			if got.Status != metav1.ConditionFalse || got.Reason != crewletaiv1alpha1.ReasonApplyFailed {
				t.Fatalf("ConfigApplied = %+v, want False, ApplyFailed", got)
			}
			for _, want := range []string{"engine: apply: no sandbox cell", `status "` + h.Status, `posture "` + string(h.Posture), c.runs} {
				if !strings.Contains(got.Message, want) {
					t.Errorf("message %q does not carry %q", got.Message, want)
				}
			}
			if run.applied.kind != appliedFailed {
				t.Errorf("parts' verdict = %v, want appliedFailed", run.applied.kind)
			}
			if events := log.of("acme"); len(events) != 1 || !strings.HasPrefix(events[0], "Warning ApplyFailed") {
				t.Errorf("events = %q, want one Warning ApplyFailed", events)
			}
			if result.RequeueAfter != engineRetryInterval {
				t.Errorf("RequeueAfter = %s, want %s", result.RequeueAfter, engineRetryInterval)
			}

			// Said once: the next look at the same failure records no
			// second event.
			again, againLog := verifyRun(written.Add(time.Hour), written, crewletaiv1alpha1.ReasonApplied, got)
			again.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
			if events := againLog.of("acme"); len(events) != 0 {
				t.Errorf("events on a second look = %q, want none", events)
			}
		})
	}
}

// TestConfigAppliedGivesUpOnAnEpochThatNeverArrives pins the deadline: an
// engine that neither applies the epoch nor records a failed attempt is
// Applying until applyDeadline after the write, and ApplyFailed from then,
// with its status and posture in the message.
func TestConfigAppliedGivesUpOnAnEpochThatNeverArrives(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	cl := (&fakeEngine{health: health("ok", "wait", 1), fleet: fleet(1, "r1", "ok", "")}).client(t)
	for _, c := range []struct {
		after time.Duration
		want  string
	}{
		{applyDeadline - time.Second, crewletaiv1alpha1.ReasonApplying},
		{applyDeadline, crewletaiv1alpha1.ReasonApplyFailed},
	} {
		run, log := verifyRun(written.Add(c.after), written, crewletaiv1alpha1.ReasonApplied, nil)
		h, err := cl.Health(context.Background())
		run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
		if got := run.configApplied.Reason; got != c.want {
			t.Fatalf("ConfigApplied %s after the write = %s, want %s", c.after, got, c.want)
		}
		if c.want == crewletaiv1alpha1.ReasonApplyFailed {
			if msg := run.configApplied.Message; !strings.Contains(msg, `posture "wait"`) || !strings.Contains(msg, "applied epoch 1") {
				t.Errorf("message %q does not say the engine's posture and applied epoch", msg)
			}
			if events := log.of("acme"); len(events) != 1 || !strings.HasPrefix(events[0], "Warning ApplyFailed") {
				t.Errorf("events = %q, want one Warning ApplyFailed", events)
			}
		}
	}
}

// TestConfigAppliedNeedsTheEnginesHealth pins that an engine whose /health
// cannot be read is not reported as running anything.
func TestConfigAppliedNeedsTheEnginesHealth(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	run, _ := verifyRun(written, written, crewletaiv1alpha1.ReasonApplied, nil)
	result := run.verifyApplied(context.Background(), nil, engine.Health{}, context.DeadlineExceeded, ctrl.Result{})
	if got := run.configApplied; got.Status != metav1.ConditionFalse || got.Reason != crewletaiv1alpha1.ReasonEngineUnavailable {
		t.Fatalf("ConfigApplied = %+v, want False, EngineUnavailable", got)
	}
	if result.RequeueAfter != engineRetryInterval || run.applied.kind != appliedUnknown {
		t.Errorf("result %+v and parts' verdict %v, want a retry and the parts left as they were", result, run.applied.kind)
	}
	if meta.IsStatusConditionTrue([]metav1.Condition{*run.configApplied}, crewletaiv1alpha1.ConditionConfigApplied) {
		t.Error("ConfigApplied is True")
	}
}
