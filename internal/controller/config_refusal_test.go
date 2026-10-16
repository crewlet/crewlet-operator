package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/events"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// seatFounder is the seat the refusal tests find held: a human seat in the
// engine, which the document makes an agent's.
const seatFounder = "founder"

// refusalRun is a reconcile of Company acme whose Seats are founder, an agent
// seat now, and bo.
func refusalRun() (*configRun, *part, *part, *events.FakeRecorder) {
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany}
	founder := newPart(&crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatFounder})
	bo := newPart(&crewletaiv1alpha1.Seat{Namespace: "ns", Name: "bo"})
	recorder := events.NewFakeRecorder(10)
	run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Recorder: recorder}, company: company,
		parts: []*part{founder, bo}}
	return run, founder, bo, recorder
}

// asReturned is an engine's refusal as a Client method returns it.
func asReturned(err error) error { return fmt.Errorf("PUT /config: %w", err) }

// TestNotGrantedSaysWhatTheEngineAskedFor holds the operator to telling a
// refusal of its token's authority from every other refusal, and to saying
// what undoes it: a grant missing from the crewlet.yaml it renders, or a
// binding only an admin of the company can remove. A refusal of the document
// itself, and a 403 from something in front of the engine, are not it.
func TestNotGrantedSaysWhatTheEngineAskedFor(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		says []string
	}{
		{
			name: "a grant the token lacks",
			err: &engine.APIError{StatusCode: http.StatusForbidden, Code: "unauthorized", Reason: "no_grant",
				Grants: []string{"admin"}},
			says: []string{"(unauthorized, no_grant)", "needs the grant admin", "Secret acme-bootstrap"},
		},
		{
			name: "a seat the company dropped",
			err: &engine.APIError{StatusCode: http.StatusForbidden, Code: "seat_unavailable",
				Message: "Your seat, founder, is not in the org chart."},
			says: []string{engine.TokenLogin(render.TokenOperator), "Your seat, founder, is not in the org chart", "crewlet iam unbind"},
		},
		{
			name: "a managed document",
			err:  &engine.APIError{StatusCode: http.StatusForbidden, Code: "config_managed", ManagedBy: []string{"ops"}},
		},
		{name: "a proxy's 403", err: &engine.APIError{StatusCode: http.StatusForbidden}},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, _, _, recorder := refusalRun()
			got := run.notGranted(asReturned(c.err))
			if got != (c.says != nil) {
				t.Fatalf("notGranted() = %v, want %v", got, c.says != nil)
			}
			if !got {
				return
			}
			if run.configApplied.Reason != crewletaiv1alpha1.ReasonNotGranted {
				t.Fatalf("ConfigApplied reason %s, want NotGranted", run.configApplied.Reason)
			}
			for _, s := range c.says {
				if !strings.Contains(run.configApplied.Message, s) {
					t.Errorf("ConfigApplied does not say %q: %s", s, run.configApplied.Message)
				}
			}
			if len(recorder.Events) != 1 {
				t.Errorf("recorded %d events, want the one Warning", len(recorder.Events))
			}
		})
	}
}

// TestSeatHeldNamesEachSeatAndItsRemedy holds the SeatHeld report to what
// the engine says and no more: every seat and the kind of thing that holds
// it, the remedy for each kind present and none for a kind absent, the
// report on a Seat that still exists and is held and on no other, and one
// event however many reconciles meet the same refusal.
func TestSeatHeldNamesEachSeatAndItsRemedy(t *testing.T) {
	held := asReturned(&engine.SeatHeldError{Held: map[string]engine.SeatHolder{
		seatFounder: {Kind: engine.HolderPerson, Stage: "active"},
		seatAna:     {Kind: engine.HolderInvitation},
	}})
	run, founder, bo, recorder := refusalRun()
	if !run.seatHeld(held) {
		t.Fatal("seatHeld() = false for a *SeatHeldError")
	}
	got := run.configApplied
	if got.Reason != crewletaiv1alpha1.ReasonSeatHeld {
		t.Fatalf("ConfigApplied reason %s, want SeatHeld", got.Reason)
	}
	for _, s := range []string{"ana (held by an open invitation); founder (held by a person, active)",
		"a person is moved to another human seat or removed; an open invitation is cancelled", "crewlet iam seats"} {
		if !strings.Contains(got.Message, s) {
			t.Errorf("ConfigApplied does not say %q: %s", s, got.Message)
		}
	}
	if strings.Contains(got.Message, "service account") {
		t.Errorf("ConfigApplied names a remedy for a service account, which holds no seat here: %s", got.Message)
	}
	if !strings.Contains(founder.held, "a person, active") || bo.held != "" {
		t.Errorf("founder's report %q, bo's %q; want founder's naming its holder and none on bo", founder.held, bo.held)
	}
	if len(recorder.Events) != 1 {
		t.Fatalf("recorded %d events, want the one Warning", len(recorder.Events))
	}

	// The next reconcile meets the same refusal with the condition already
	// recorded: it is not told again.
	again, _, _, recorder := refusalRun()
	meta.SetStatusCondition(&again.company.Status.Conditions, *got)
	again.seatHeld(held)
	if len(recorder.Events) != 0 {
		t.Errorf("recorded %d events for a refusal ConfigApplied already reports, want none", len(recorder.Events))
	}

	if run, _, _, _ := refusalRun(); run.seatHeld(asReturned(&engine.APIError{StatusCode: http.StatusConflict, Code: "seat_held"})) {
		t.Error("seatHeld() = true for a seat_held answer without its holders, which is no *SeatHeldError")
	}
}

// TestRetryAfterFollowsTheEngineWithinTheInterval holds the reconcile's next
// look at an engine to when the engine asked for it, never later than
// engineRetryInterval.
func TestRetryAfterFollowsTheEngineWithinTheInterval(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want time.Duration
	}{
		{"seconds", &engine.APIError{StatusCode: http.StatusServiceUnavailable, RetryAfter: 2 * time.Second}, 2 * time.Second},
		{"longer than the interval", &engine.APIError{StatusCode: http.StatusServiceUnavailable, RetryAfter: time.Minute}, engineRetryInterval},
		{"no Retry-After", &engine.APIError{StatusCode: http.StatusServiceUnavailable}, engineRetryInterval},
		{"not a 503", &engine.APIError{StatusCode: http.StatusBadGateway, RetryAfter: 2 * time.Second}, engineRetryInterval},
		{"unreachable", fmt.Errorf("dial tcp: connection refused"), engineRetryInterval},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := retryAfter(asReturned(c.err)); got != c.want {
				t.Fatalf("retryAfter() = %s, want %s", got, c.want)
			}
		})
	}
}
