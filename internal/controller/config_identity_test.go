package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestIdentityClaimedFollowsTheEngine pins each answer IdentityClaimed gives
// for what the engine's /health says about its identity directory: a company
// somebody has joined, one nobody has, told apart from one nobody can join
// for want of a Human seat, and every answer that is no answer, which is
// never read as nobody.
func TestIdentityClaimedFollowsTheEngine(t *testing.T) {
	const access = "acme-api-access"
	seven := []string{seatAna, "bo", "cy", "dee", "eve", "fay", "gus"}
	for _, c := range []struct {
		name    string
		health  engine.Health
		err     error
		seats   []string
		status  metav1.ConditionStatus
		reason  string
		says    []string
		saysNot []string
	}{
		{
			name: "somebody joined", health: engine.Health{Identity: engine.IdentityReady}, seats: []string{seatFounder},
			status: metav1.ConditionTrue, reason: crewletaiv1alpha1.ReasonPersonEnrolled,
		},
		{
			name: "nobody yet", health: engine.Health{Identity: engine.IdentityUnclaimed}, seats: []string{seatFounder},
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonUnclaimed,
			says: []string{"Its Human seats are founder.", "Secret " + access, "status.apiAccessSecretRef",
				"crewlet iam invite <email> -seat <handle> -grants admin"},
		},
		{
			name: "nobody yet, and many seats", health: engine.Health{Identity: engine.IdentityUnclaimed}, seats: seven,
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonUnclaimed,
			says: []string{"ana, bo, cy, dee, eve and 2 more"}, saysNot: []string{"fay", "gus"},
		},
		{
			name: "nobody can", health: engine.Health{Identity: engine.IdentityUnclaimed},
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonNoHumanSeat,
			says: []string{"no Human seat"}, saysNot: []string{access},
		},
		{
			name: "a directory the engine could not read", health: engine.Health{Identity: engine.IdentityUnknown},
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonIdentityNotReported,
		},
		{
			name: "an engine that did not say", seats: []string{seatFounder},
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonIdentityNotReported,
		},
		{
			name: "health not read", err: errors.New("dial tcp: connection refused"),
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonIdentityNotReported,
			says: []string{"connection refused"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := identityClaimed(c.health, c.err, c.seats, access)
			if got.Type != crewletaiv1alpha1.ConditionIdentityClaimed || got.Status != c.status || got.Reason != c.reason {
				t.Fatalf("identityClaimed() = %s %s %s, want %s %s", got.Type, got.Status, got.Reason, c.status, c.reason)
			}
			for _, s := range c.says {
				if !strings.Contains(got.Message, s) {
					t.Errorf("the message does not say %q: %s", s, got.Message)
				}
			}
			for _, s := range c.saysNot {
				if strings.Contains(got.Message, s) {
					t.Errorf("the message says %q: %s", s, got.Message)
				}
			}
		})
	}
}

// TestHumanSeatsAreTheRunningDocuments holds IdentityClaimed to the seats of
// the company the engine runs once the reconcile is done: the rendered
// document when the reconcile activated it, so a Human seat it just added is
// named at once; else the document the engine ran when the reconcile read
// it, which a refusal or another policy keeps from being the rendered one.
func TestHumanSeatsAreTheRunningDocuments(t *testing.T) {
	const (
		rendered = "roles:\n  - name: Founder\n    handle: founder\n    kind: human\n"
		running  = "roles:\n  - name: Ana\n    handle: ana\n    kind: human\n"
	)
	for _, c := range []struct {
		name   string
		active string
		wrote  bool
		want   []string
	}{
		{name: "the engine had no document", wrote: true, want: []string{seatFounder}},
		{name: "the reconcile activated the rendered one", active: running, wrote: true, want: []string{seatFounder}},
		{name: "the engine runs another", active: running, want: []string{seatAna}},
	} {
		t.Run(c.name, func(t *testing.T) {
			run := &configRun{doc: &render.Document{YAML: []byte(rendered)}, wrote: c.wrote}
			if c.active != "" {
				run.active = []byte(c.active)
			}
			if got := run.humanSeats(context.Background()); !slices.Equal(got, c.want) {
				t.Fatalf("humanSeats() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestIdentityResyncWaitsLittleForTheFirstPerson holds the reconciler to
// looking at the engine again within identityPollInterval while nobody has
// joined, since the first person joins where no watch can see it, and to
// leaving every other look as it was decided.
func TestIdentityResyncWaitsLittleForTheFirstPerson(t *testing.T) {
	condition := func(reason string) *metav1.Condition {
		return &metav1.Condition{Type: crewletaiv1alpha1.ConditionIdentityClaimed, Reason: reason}
	}
	for _, c := range []struct {
		name     string
		claimed  *metav1.Condition
		decided  time.Duration
		expected time.Duration
	}{
		{"nobody yet, at the drift check", condition(crewletaiv1alpha1.ReasonUnclaimed), driftCheckInterval, identityPollInterval},
		{"nobody yet, and no look decided", condition(crewletaiv1alpha1.ReasonUnclaimed), 0, identityPollInterval},
		{"nobody yet, and a sooner look decided", condition(crewletaiv1alpha1.ReasonUnclaimed), applyPollInterval, applyPollInterval},
		{"somebody joined", condition(crewletaiv1alpha1.ReasonPersonEnrolled), driftCheckInterval, driftCheckInterval},
		{"nobody can join", condition(crewletaiv1alpha1.ReasonNoHumanSeat), driftCheckInterval, driftCheckInterval},
		{"health not read", nil, driftCheckInterval, driftCheckInterval},
	} {
		t.Run(c.name, func(t *testing.T) {
			run := &configRun{identityClaimed: c.claimed}
			if got := run.identityResync(ctrl.Result{RequeueAfter: c.decided}).RequeueAfter; got != c.expected {
				t.Fatalf("identityResync() looks again after %s, want %s", got, c.expected)
			}
		})
	}
}
