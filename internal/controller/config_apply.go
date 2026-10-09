package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
)

// A write to the engine's /config activates a revision and returns its
// epoch; the engine applies it afterwards, on its reconcile loop, and keeps
// running the previous revision if the apply fails. So a revision is running
// only once the engine's /health reports that epoch applied, and the config
// reconciler reports ConfigApplied=True only then.

// applyPollInterval is how soon the config reconciler looks again while the
// engine applies a revision it activated. The write nudges the engine's
// reconcile loop, which then applies the revision within milliseconds, so the
// first look after this usually finds it applied; a lost nudge leaves the
// revision to the engine's next poll, at most enginePollInterval later.
const applyPollInterval = 2 * time.Second

// enginePollInterval is the longest a Crewlet engine goes between two reads
// of its activation pointer: 15 seconds with 20% jitter (crewlet's
// configplane.ReconcileInterval and ReconcileJitter). Each read that finds an
// epoch it has not applied makes an attempt at it, up to three per epoch.
const enginePollInterval = 18 * time.Second

// applyDeadline is how long after a write the operator waits for the engine
// to report the write's epoch applied when the engine records no failed
// attempt either. One poll covers a lost nudge and a second the apply itself
// (one that starts the native runtime or MCP servers takes seconds); an
// engine that has neither applied nor failed the epoch in four is not
// polling its pointer at all, which no later look will change by itself.
const applyDeadline = 4 * enginePollInterval

// pendingApplies remembers, per Company, the ConfigApplied condition a write
// reports once the engine has applied it, while the Company says Applying:
// the write's own verdict (Applied, DriftCorrected, Seeded) and message,
// which nothing on the engine can tell afterwards. It lives in memory only,
// so an operator restarted in the seconds an apply takes reports the plain
// Applied the engine running the rendered document always justifies; the
// write's events keep the rest.
type pendingApplies struct {
	mu      sync.Mutex
	byOwner map[types.NamespacedName]pendingApply
}

// pendingApply is the condition a write of epoch will report.
type pendingApply struct {
	epoch     int64
	condition metav1.Condition
}

func (p *pendingApplies) remember(company types.NamespacedName, epoch int64, condition *metav1.Condition) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byOwner == nil {
		p.byOwner = map[types.NamespacedName]pendingApply{}
	}
	if current, ok := p.byOwner[company]; ok && current.epoch == epoch {
		// The first verdict for an epoch is its write's; a later look at
		// the same epoch only finds the document in sync.
		return
	}
	p.byOwner[company] = pendingApply{epoch: epoch, condition: *condition}
}

// settle returns, and forgets, the condition remembered for the company's
// write of epoch.
func (p *pendingApplies) settle(company types.NamespacedName, epoch int64) (metav1.Condition, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pending, ok := p.byOwner[company]
	delete(p.byOwner, company)
	if !ok || pending.epoch != epoch {
		return metav1.Condition{}, false
	}
	return pending.condition, true
}

func (p *pendingApplies) forget(company types.NamespacedName) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byOwner, company)
}

func (r *CompanyConfigReconciler) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// verifyApplied turns a ConfigApplied=True verdict into what the engine
// reports about it: True once the engine has applied the epoch status.config
// records and has not diverged since, Applying until then, and ApplyFailed
// when the engine recorded a failed attempt at that epoch or a later one, or
// when applyDeadline passes without either. It runs on every look at the
// engine, the periodic resync included, so an engine that diverges after
// applying the operator's revision is reported too. health and healthErr are
// what readHealth read; result is what the reconcile would return otherwise.
func (run *configRun) verifyApplied(
	ctx context.Context, eng *engine.Client, health engine.Health, healthErr error, result ctrl.Result,
) ctrl.Result {
	if run.configApplied == nil || run.configApplied.Status != metav1.ConditionTrue {
		return result
	}
	key := types.NamespacedName{Namespace: run.company.Namespace, Name: run.company.Name}
	config := run.company.Status.Config
	if run.config != nil {
		config = *run.config
	}
	var target int64
	if config.Epoch != nil {
		target = *config.Epoch
	}
	if healthErr != nil {
		run.unavailable(healthErr)
		run.applied = appliedVerdict{}
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	}

	if health.AppliedEpoch >= target && !health.Posture.Diverged() {
		run.settled(key, target, config.RevisionID)
		return result
	}
	// Behind the epoch, or diverged from a later one. A diverged posture
	// alone does not say the apply failed: the engine counts an attempt
	// before it makes it, so its posture reads diverged for as long as an
	// apply that will succeed takes. Only the record of an attempt that
	// ended does.
	record, err := eng.LastApply(ctx)
	if err != nil {
		run.unavailable(err)
		run.applied = appliedVerdict{}
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	}
	switch {
	case record.Status.Failed() && record.Epoch >= target && record.Epoch > health.AppliedEpoch:
		account := record.Error
		if account == "" {
			account = "the engine gave no account of the failure"
		}
		run.applyFailed(config.RevisionID, fmt.Sprintf("The engine failed to apply epoch %d (revision %s): %s. "+
			"It reports status %q and posture %q, and keeps running epoch %d, the last it applied.",
			record.Epoch, record.RevisionID, account, health.Status, health.Posture, health.AppliedEpoch))
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	case health.AppliedEpoch >= target:
		// An apply of a later epoch, in progress: the epoch the
		// operator's revision has is applied, and still runs.
		run.settled(key, target, config.RevisionID)
		return result
	case config.AppliedAt != nil && run.clock().Sub(config.AppliedAt.Time) >= applyDeadline:
		run.applyFailed(config.RevisionID, fmt.Sprintf("The engine has not applied revision %s (epoch %d) %s after the "+
			"operator activated it, and records no failed attempt: it reports status %q and posture %q at applied epoch %d, "+
			"and keeps running that. Its log says why.",
			config.RevisionID, target, applyDeadline, health.Status, health.Posture, health.AppliedEpoch))
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	}
	run.applying(key, target, config.RevisionID, health)
	return ctrl.Result{RequeueAfter: applyPollInterval}
}

// settled reports the verdict of an epoch the engine has applied: the write's
// own, when this reconciler remembers it, and an Applied event when the
// epoch is this reconcile's write or one the Company was waiting for.
func (run *configRun) settled(key types.NamespacedName, target int64, revision string) {
	if remembered, ok := run.pending.settle(key, target); ok {
		run.configApplied = &remembered
	}
	current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied)
	if run.wrote || (current != nil &&
		(current.Reason == crewletaiv1alpha1.ReasonApplying || current.Reason == crewletaiv1alpha1.ReasonApplyFailed)) {
		run.event(corev1.EventTypeNormal, crewletaiv1alpha1.ReasonApplied, "ApplyConfig",
			"The engine applied revision %s (epoch %d).", revision, target)
	}
}

// applying records a revision the engine has not applied yet.
func (run *configRun) applying(key types.NamespacedName, target int64, revision string, health engine.Health) {
	run.pending.remember(key, target, run.configApplied)
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonApplying,
		Message: fmt.Sprintf("Revision %s is activated as epoch %d and the engine is applying it; it runs epoch %d meanwhile "+
			"(status %q, posture %q).", revision, target, health.AppliedEpoch, health.Status, health.Posture),
	}
	if run.applied.kind == appliedInRevision {
		run.applied = appliedVerdict{kind: appliedPending, revision: revision}
	}
}

// applyFailed records a revision the engine did not apply, with an event
// when the Company did not say so already.
func (run *configRun) applyFailed(revision, message string) {
	if current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied); current == nil ||
		current.Reason != crewletaiv1alpha1.ReasonApplyFailed {
		run.event(corev1.EventTypeWarning, crewletaiv1alpha1.ReasonApplyFailed, "ApplyConfig", "%s", message)
	}
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonApplyFailed,
		Message: message,
	}
	if run.applied.kind == appliedInRevision {
		run.applied = appliedVerdict{kind: appliedFailed, revision: revision, message: fmt.Sprintf(
			"In revision %s, which the engine did not apply (see ConfigApplied on Company %s).", revision, run.company.Name)}
	}
}
