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

// engineLagGrace is how long a Crewlet node may be behind an epoch before it
// counts the lag as its own failure rather than propagation: three of its
// 15-second reconcile intervals (crewlet's configplane.LagGrace). Past it, a
// node whose apply has not ended reads shed when a peer has applied the epoch.
const engineLagGrace = 45 * time.Second

// applyDeadline is how long after a write the operator waits for an engine
// whose posture still reads wait, or after it first finds the engine running
// a revision of the rendered document it did not write, which it adopts
// (inSync). A node reports wait for the whole of an
// apply, and turns serve when it succeeds or a diverged posture when an
// attempt fails, so wait past this is one of two things the engine itself
// never concludes: it is not polling its activation pointer at all, or an
// apply hangs on a node no peer has applied the epoch beside (the engine
// reads that silence as no evidence, and keeps reporting wait). One poll
// covers a lost nudge and a second the apply itself (one that starts the
// native runtime or MCP servers takes seconds); four is past any apply that
// ends.
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
// records and has not diverged since, Applying while its posture reads wait,
// and ApplyFailed when its posture reads diverged, or still wait
// applyDeadline after the write, or after the operator found the revision it
// adopted (status.config.appliedAt). It runs on every look at the engine, the
// periodic resync included, so an engine that diverges after applying the
// operator's revision, on one someone activates later, is reported too.
// health and healthErr are what readHealth read; result is what the
// reconcile would return otherwise.
//
// The posture decides, because the engine's posture is its own conclusion:
// an attempt counts, and its failure turns the posture diverged, only once
// the attempt has ended, so a diverged posture is never an apply still
// running. Its record in /fleet is read only for what the posture cannot
// carry: the failed attempt's epoch and its account.
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

	switch {
	case health.Posture.Diverged():
		run.applyFailed(config.RevisionID, run.failureAccount(ctx, eng, health, target))
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	case health.AppliedEpoch >= target:
		run.settled(key, target, config.RevisionID)
		return result
	case config.AppliedAt != nil && run.clock().Sub(config.AppliedAt.Time) >= applyDeadline:
		run.applyFailed(config.RevisionID, fmt.Sprintf("The engine has not applied revision %s (epoch %d) %s after the "+
			"operator activated it or found it activated, and has not concluded that it failed: it reports status %q and "+
			"posture %q at applied epoch %d, and keeps running that. It is not polling its activation pointer, or an apply "+
			"hangs; its log says which.",
			config.RevisionID, target, applyDeadline, health.Status, health.Posture, health.AppliedEpoch))
		return ctrl.Result{RequeueAfter: engineRetryInterval}
	}
	run.applying(key, target, config.RevisionID, health)
	return ctrl.Result{RequeueAfter: applyPollInterval}
}

// failureAccount is ApplyFailed's message for an engine whose posture reads
// diverged: the failed attempt's epoch and the engine's own account of it,
// from its record in /fleet, when that record names an attempt at the
// operator's epoch or a later one that ended in failure. A diverged posture
// with no such record is an apply that has run past engineLagGrace while a
// peer applied the epoch, or a failure the engine has not yet written to
// the record it keeps for its peers (it updates its own posture first); the
// next look, engineRetryInterval later, finds the account of the second.
//
// What the node runs meanwhile is the revision its record names it serving,
// and what it starts on if it restarts the one its record names it booting
// on, which is the same revision but while the engine retries moving what it
// starts on; the epoch it reports applied says nothing of either after a
// restart, when it is 0 until the node applies one.
func (run *configRun) failureAccount(ctx context.Context, eng *engine.Client, health engine.Health, target int64) string {
	runs := fmt.Sprintf(" It reports status %q and posture %q, and keeps running epoch %d, the last it applied.",
		health.Status, health.Posture, health.AppliedEpoch)
	fleet, err := run.fleetOf(ctx, eng)
	if err != nil {
		return ended(fmt.Sprintf("The engine reports that it did not apply the activated epoch, and its record of the attempt "+
			"could not be read: %v", err)) + runs
	}
	record := fleet.LastApply()
	if record.Serving != "" {
		runs = fmt.Sprintf(" It reports status %q and posture %q, and keeps running revision %s, the last it applied or "+
			"started on, %s.", health.Status, health.Posture, record.Serving, startsOn(&record))
	}
	if record.Status.Failed() && record.Epoch >= target && record.Epoch > health.AppliedEpoch {
		account := record.Error
		if account == "" {
			account = "the engine gave no account of the failure"
		}
		return ended(fmt.Sprintf("The engine failed to apply epoch %d (revision %s): %s", record.Epoch, record.RevisionID, account)) + runs
	}
	epoch := max(fleet.TargetEpoch, target)
	return fmt.Sprintf("The engine reports that it did not apply epoch %d and records no failed attempt at it: its apply "+
		"has run longer than the engine's %s grace while a peer applied it, or the account of the failure has not reached "+
		"its record yet.", epoch, engineLagGrace) + runs
}

// startsOn says, for failureAccount, what a node whose record names it
// serving a revision starts on if it restarts.
func startsOn(record *engine.ApplyRecord) string {
	switch record.Boots {
	case record.Serving:
		return "which it starts on again if it restarts"
	case "":
		return "and starts on no revision if it restarts, and tries the fleet's again"
	}
	return fmt.Sprintf("and starts on revision %s if it restarts, which its store names until the engine has moved "+
		"what it starts on to the revision it runs", record.Boots)
}

// settled reports the verdict of an epoch the engine has applied: the write's
// own, when this reconciler remembers it, and an Applied event when the
// epoch is this reconcile's write or one the Company was waiting for.
func (run *configRun) settled(key types.NamespacedName, target int64, revision string) {
	if remembered, ok := run.pending.settle(key, target); ok {
		run.configApplied = &remembered
	}
	// The engine runs this document now, and no earlier one: its pins
	// alone are what the pods must serve. A document of the engine's own
	// is narrowed by narrowPlacement, from the engine's record of applying
	// it: the epoch the operator recorded says nothing about it.
	if run.running != nil && run.runningRevision == "" {
		run.recordPlacement(*run.running, true)
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
