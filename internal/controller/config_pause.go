package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// A seat's pause is not in the company document: Crewlet keeps it in its
// coordination store, and a person takes and lifts it from the engine's
// dashboard. So the config reconciler reconciles it apart from the document,
// on every look at the engine, whatever became of the document: a pause is a
// brake, and holding it back because another object keeps the next revision
// from being applied would leave a seat running that its Seat asks to stop.
//
// Who owns a pause. A Seat that sets spec.paused owns its seat's pause under
// the Company's Enforce policy: a pause or resume made in the dashboard is
// undone on the operator's next look, as an edit of the document is. A Seat
// that leaves spec.paused unset owns nothing, under any policy: the operator
// only reports the pause. Reading unset as "not paused" would make the
// operator lift, within minutes, every pause a person takes in the dashboard
// of an operator-run company, which is the one control a person reaches for
// when an agent misbehaves, and the one the engine deliberately keeps out of
// the document. Observe and Seed change no pause, as they write no document.
// This is ADR-0003 (adr/0003-own-a-seats-pause-only-where-the-seat-asks.md).

// pauseVerdict is what a reconcile found about one seat's pause: its Paused
// condition and status.pause.
type pauseVerdict struct {
	status  metav1.ConditionStatus
	reason  string
	message string
	// pause is the pause the engine holds; zero when it holds none, or when
	// the reconcile could not tell.
	pause engine.SeatPause
}

// syncPauses reads which of the company's seats the engine holds paused and,
// under Enforce, pauses or resumes each Seat whose spec.paused says otherwise.
// It records a verdict on every agent Seat the engine runs and returns result,
// sooner when a call should be made again. An engine that cannot be read
// leaves every Paused condition as it was: the document's own conditions say
// why the engine did not answer.
func (run *configRun) syncPauses(ctx context.Context, eng *engine.Client, result ctrl.Result) ctrl.Result {
	pauses, err := eng.SeatPauses(ctx)
	if err != nil {
		logf.FromContext(ctx).V(1).Info("Failed to read the engine's seat pauses", "error", err.Error())
		return result
	}
	var retry time.Duration
	for _, p := range run.parts {
		seat, ok := p.obj.(*crewletaiv1alpha1.Seat)
		if !ok || seat.Spec.Kind == crewletaiv1alpha1.SeatKindHuman || p.handleTaken {
			continue
		}
		state, listed := pauses[render.SeatHandle(seat)]
		if !listed {
			// Not in the revision the engine runs yet, or no longer: there
			// is no seat of the engine's to pause.
			continue
		}
		verdict, again := run.syncPause(ctx, eng, seat, state)
		p.pause = &verdict
		if again > 0 && (retry == 0 || again < retry) {
			retry = again
		}
	}
	if retry > 0 && (result.RequeueAfter == 0 || retry < result.RequeueAfter) {
		result.RequeueAfter = retry
	}
	return result
}

// syncPause decides one seat's pause from what the engine holds, calling the
// engine when Enforce has it differ from the Seat; again is how soon to look
// once more, zero for the reconcile's own interval.
func (run *configRun) syncPause(
	ctx context.Context, eng *engine.Client, seat *crewletaiv1alpha1.Seat, state engine.SeatPause,
) (verdict pauseVerdict, again time.Duration) {
	want := seat.Spec.Paused
	handle := render.SeatHandle(seat)
	stop := seat.Spec.StopRunning != nil && *seat.Spec.StopRunning
	differs := want != nil && (*want != state.Paused || (*want && stop && !state.StopRunning))
	switch {
	case !differs:
		return reported(state), 0
	case run.policy() != crewletaiv1alpha1.ManagementPolicyEnforce:
		v := reported(state)
		v.reason = crewletaiv1alpha1.ReasonPauseNotManaged
		v.message += fmt.Sprintf(" spec.paused asks for it to be %s; under the Company's %s policy the operator changes no pause.",
			pausedWord(*want, stop), run.policy())
		return v, 0
	}

	var answer engine.PauseResult
	var err error
	if *want {
		answer, err = eng.PauseSeat(ctx, handle, seat.Spec.PauseReason, stop)
	} else {
		answer, err = eng.ResumeSeat(ctx, handle)
	}
	var refused *engine.ToolError
	switch {
	case errors.Is(err, engine.ErrOutcomeUnknown):
		return pauseVerdict{
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonPauseUnknown,
			message: fmt.Sprintf("The engine may or may not have recorded the operator's %s of the seat; the operator asks again in %s: %s.",
				changeWord(*want), engineRetryInterval, err),
		}, engineRetryInterval
	case errors.As(err, &refused):
		return pauseVerdict{
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonPauseRefused,
			message: fmt.Sprintf("The engine refused the operator's %s of the seat: %s", changeWord(*want), refused.Message),
		}, 0
	case err != nil:
		// The engine did not answer: the call is made again on the next
		// look, which an engine that cannot be reached brings soon.
		logf.FromContext(ctx).V(1).Info("Failed to change a seat's pause", "seat", seat.Name, "error", err.Error())
		return pauseVerdict{
			status: metav1.ConditionUnknown, reason: crewletaiv1alpha1.ReasonPauseUnknown,
			message: fmt.Sprintf("The operator's %s of the seat did not reach the engine; it tries again in %s: %s.",
				changeWord(*want), engineRetryInterval, err),
		}, engineRetryInterval
	}
	if answer.Changed {
		run.recordPause(seat, state, answer.SeatPause, *want)
	}
	return reported(answer.SeatPause), 0
}

// recordPause records the operator's change of a seat's pause as an event on
// the Seat: a Warning when it undid a change somebody made in the engine
// since the Seat last reported its pause as spec.paused asks, which is drift.
func (run *configRun) recordPause(seat *crewletaiv1alpha1.Seat, before, after engine.SeatPause, paused bool) {
	action := "Resumed"
	if paused {
		action = "Paused"
	}
	previous := meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionPaused)
	wantedBefore := metav1.ConditionFalse
	if paused {
		wantedBefore = metav1.ConditionTrue
	}
	drifted := previous != nil && previous.ObservedGeneration == seat.Generation && previous.Status == wantedBefore &&
		previous.Reason != crewletaiv1alpha1.ReasonPauseNotManaged
	if drifted {
		somebody := "somebody"
		if before.By != "" {
			somebody = before.By
		}
		run.Recorder.Eventf(seat, nil, corev1.EventTypeWarning, crewletaiv1alpha1.ReasonDrifted, "SyncPause",
			"%s changed the seat's pause in the engine, and spec.paused asks for it to be %s: under the Company's Enforce "+
				"policy the operator %s it again.", somebody, pausedWord(paused, after.StopRunning), strings.ToLower(action))
		return
	}
	run.Recorder.Eventf(seat, nil, corev1.EventTypeNormal, "Seat"+action, "SyncPause", "%s the seat in the engine, as spec.paused asks.", action)
}

// reported is the verdict of a seat whose pause is what the engine holds.
func reported(state engine.SeatPause) pauseVerdict {
	if !state.Paused {
		return pauseVerdict{
			status: metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonSeatRunning,
			message: "The engine runs the seat: nobody has it paused.",
		}
	}
	message := fmt.Sprintf("The engine holds the seat paused since %s, paused by %s", state.At.Format(time.RFC3339), state.By)
	if state.Reason != "" {
		message += ": " + state.Reason
	}
	message += ". It starts no new turn and its mail waits on its inbox"
	if state.StopRunning {
		message += "; the turn it was on was stopped"
	}
	return pauseVerdict{
		status: metav1.ConditionTrue, reason: crewletaiv1alpha1.ReasonSeatPaused, message: message + ".", pause: state,
	}
}

func pausedWord(paused, stop bool) string {
	switch {
	case paused && stop:
		return "paused, its running turn stopped"
	case paused:
		return "paused"
	}
	return "running"
}

func changeWord(paused bool) string {
	if paused {
		return "pause"
	}
	return "resume"
}

// decidePause sets a Seat's Paused condition and status.pause from the
// reconcile's verdict. A Human seat has neither: the engine takes no work for
// a person that a pause could hold. A Seat with no verdict keeps what it had.
func decidePause(p *part, seat *crewletaiv1alpha1.Seat) {
	if seat.Spec.Kind == crewletaiv1alpha1.SeatKindHuman {
		meta.RemoveStatusCondition(&seat.Status.Conditions, crewletaiv1alpha1.ConditionPaused)
		seat.Status.Pause = crewletaiv1alpha1.SeatPauseStatus{}
		return
	}
	v := p.pause
	if v == nil {
		return
	}
	meta.SetStatusCondition(&seat.Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionPaused, Status: v.status, Reason: v.reason, Message: bounded(v.message),
		ObservedGeneration: seat.Generation,
	})
	if v.status == metav1.ConditionUnknown {
		// What the engine holds is not known: the last report stands.
		return
	}
	seat.Status.Pause = crewletaiv1alpha1.SeatPauseStatus{}
	if v.pause.Paused {
		at := metav1.NewTime(v.pause.At)
		seat.Status.Pause = crewletaiv1alpha1.SeatPauseStatus{
			PausedBy: v.pause.By, PausedAt: &at, Reason: v.pause.Reason, StopRunning: new(v.pause.StopRunning),
		}
	}
}
