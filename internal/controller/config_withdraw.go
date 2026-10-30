package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// A value no engine starts anything with once its Secret is gone, but the
// last good revision keeps.
//
// Kubernetes hands a container started after an optional secretKeyRef's
// Secret is deleted no value for it, never the one it had. The operator holds
// the engine to the same for every reference a document lets go without
// (Document.Optional), under the Enforce policy, which is the one under which
// it keeps the engine's store in step with the Secrets; under Seed and
// Observe the store is the engine's own. Two steps make it so, and neither is
// enough alone:
//
//   - The store entry goes. The secret sync removes it before the next
//     document is applied, and Withdraw removes it, and no other entry,
//     while that document cannot be.
//   - A revision activates after it went. The engine reads its store only
//     then, and starts every tool server and executor run with what it read,
//     the server's restarts included, so a removal reaches nothing until the
//     next activation. The next document's apply is one; while that
//     document is held, or when it activated before the entry went, the
//     running one is activated again, unchanged. The engine does that
//     itself, in the same request, when it removes a value the revision it
//     runs reads, as it does when it is written one (Crewlet's ADR-0070):
//     its answer says what that did (engine.SecretRemoved.Reload), and the
//     operator takes a re-activation that landed for its own. Where none
//     did (the revision does not read the value, or the engine's attempt
//     failed), the operator asks for one (engine.Client.ReloadConfig),
//     which is the engine's own gesture for a value its store changed under
//     it.
//
// The same holds for a value written: the engine re-activates the revision
// that reads it at the write, so the secret sync writes every value it
// pushes in one request (engine.Client.PutSecrets), one activation for all of
// them, and a rotation of a value the running document reads needs no apply
// of the same document from the operator, who records the engine's
// re-activation as its own (syncActivation).
//
// The store serves every revision by that revision's own reading of a key,
// though, and the revision the engine runs is the last good one, which keeps
// running whole while the next document is held: a key it reads as required,
// in a server or a run it starts only with every such value, keeps its value
// until the next document is applied, as it does when a Secret the next
// document requires is deleted. And a key it lets go goes, whatever the next
// document reads of it. So what the running revision reads is what decides:
//
//   - The secret sync's state records the reading of one revision
//     (secretsync.State): one the operator activated, with the Secret keys
//     its document lets go without (render.Document.Optional of the document
//     it applied, carried to each activation of it again that the operator
//     confirms activated that document), or one it finds running the very
//     document it renders, whoever activated it (adopt), whose reading is
//     the rendered one's. Any other revision, somebody else's edit or a
//     revert to another document, reads every name its document references
//     as required.
//   - The entry of a key whose Secret is gone goes where the running
//     revision does not reference it or lets it go: a key the next document
//     lets go without, and every key the running revision lets go, the next
//     document requiring it or not referencing it at all (Desired.Release).
//     A next document that requires it is held for it anyway, and is applied
//     only once the Secret exists again, when its value is pushed.
//   - A key the running revision requires keeps its value, which the
//     Company's SecretsSynced and the part's ResolvedRefs line report
//     wherever the reconcile read the Secrets the document references, and a
//     SecretKept event, told once, when the reconcile ends with the value
//     kept, the one report on a document the render holds. It goes once a
//     document that lets it go is running: the sync after the apply removes
//     it, deciding by the revision the engine runs as read after the apply,
//     and the revision just applied is activated again at once. Between the
//     two the new revision has started what it starts with the deleted
//     Secret's value: the price of keeping the last good revision whole,
//     paid where the running revision reads the key as required, or is one
//     whose reading the state does not record, when the key's Secret is
//     deleted.
//
// What the second step owes is kept in the secret sync's state
// (secretsync.State.Withdrawn), so an activation that fails is made by a
// later reconcile, of this operator or one restarted; the activation an
// apply makes is recorded before anything is deleted after it, so a deletion
// whose own record a failed write lost is withdrawn again by a later
// reconcile, which knows the revision lets the key go. And it is settled by
// any activation the engine is read running after the deletion, whoever made
// it, by the fleet's activation epoch the deletion was marked with
// (settleRead, markChanges): a reload that failed, or lost its race to
// another writer, is read back rather than taken for a failed withdrawal.
//
// The engine offers no conditional reload and no conditional delete:
// whichever revision is active when the request lands is the one activated
// again, by a reload or by the engine at a write or a removal, and a revision
// somebody activates between the operator's read of the running one and its
// deletion of a key that one lets go may require the key. The operator
// records an activation again, its own or the engine's, only once it has read
// back that the revision activated is the document it read (reactivate,
// syncActivation), and otherwise reads the revision activated as one whose
// reading it does not know, and says so.

// withdrawHeld does, for a document this reconcile could not apply, what the
// secret sync does before an apply that is not coming: it removes from the
// engine's store the entry of every reference that has no Secret or key any
// more and that the revision the engine runs goes without, a reference of
// read, which it read of the Secrets, that the document lets go without, or
// one that revision lets go itself, and activates the running revision
// again if it references one, and says what the store keeps for it. It does
// nothing but under the Enforce policy, on an engine that runs, and nothing
// where no such reference is absent, no activation is owed, and the store
// keeps nothing it has said it keeps. Over a revision the engine stores
// without a seal it removes nothing and activates nothing: nobody can read
// what that revision reads, and the write that replaces it once the document
// is no longer held settles what is owed (KeepListed, reactivate).
//
// An error is the API server's, for the reconcile to retry. One of the
// engine's, or one reaching it, is reported (withdrawalFailed) and tried
// again when engineRetry says: the document's conditions already say why it
// is held.
func (run *configRun) withdrawHeld(ctx context.Context, read secretsync.Desired) (ctrl.Result, error) {
	c := run.company
	if run.policy() != crewletaiv1alpha1.ManagementPolicyEnforce || suspended(c) ||
		meta.FindStatusCondition(c.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady) == nil {
		return ctrl.Result{}, nil
	}
	stateRef := types.NamespacedName{Namespace: c.Namespace, Name: secretSyncStateName(c)}
	state, err := secretsync.LoadState(ctx, run.APIReader, stateRef, c)
	if _, ok := errors.AsType[*secretsync.NotOwnedError](err); ok {
		// Somebody else's Secret: the operator writes no state there, and
		// so nothing into the store either, which enforce reports once the
		// document gets that far.
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	run.told = state.Retained()
	// What the revision the state records goes without: whether that one
	// runs, the engine says.
	released, err := secretsync.Absent(ctx, run.APIReader, state.LetGoKeys(state.Recorded()))
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(read.Missing()) == 0 && len(released) == 0 && len(state.Withdrawn()) == 0 && len(state.Written()) == 0 &&
		len(state.Retained()) == 0 && len(run.recordedConfig().Unconfirmed) == 0 {
		// No value to take out of the store or to say it keeps, no
		// activation owed, nothing said kept that has come back, and no
		// write of the operator's that may run, which may let go what the
		// revision the state records does not (settlePending).
		return ctrl.Result{}, nil
	}
	eng, err := run.Engines.Engine(ctx, c)
	if err != nil {
		return run.withdrawalFailed(ctx, err), nil
	}
	active, err := activeConfig(ctx, eng)
	if revision, ok := engine.Unsealed(err); ok {
		// A revision stored without a seal, which the rendered document
		// replaces once nothing holds it (converge): until then nobody can
		// read which values it reads and the nodes run, and boot again, the
		// revision before it, so the store keeps everything it lists, as
		// enforce keeps it before that write, and no activation can be made
		// over it (reactivate).
		run.unsealed, err = revision, nil
		read.KeepListed(revision)
	}
	if err != nil {
		return run.withdrawalFailed(ctx, err), nil
	}
	// The revision that runs may be a write of the operator's the engine
	// could not confirm activating, whose reading the state recorded before
	// it was sent, and whose document, where the render has moved on, lets
	// go of what the one the state records does not.
	settled := run.settlePending(state, active)
	if settled.Recorded() != state.Recorded() {
		// What the revision the state now records goes without.
		if released, err = secretsync.Absent(ctx, run.APIReader, settled.LetGoKeys(settled.Recorded())); err != nil {
			return ctrl.Result{}, err
		}
	}
	state = settled
	if active != nil {
		if run.doc != nil {
			// Held for a Secret, not refused by the render: there is a
			// rendered document the running one may be.
			diffs, err := diffWith(run.doc, active)
			if err != nil {
				return ctrl.Result{}, err
			}
			state = run.adopt(state, active, diffs)
		}
		read.Keep(active.RevisionID, storeNamesIn(active.Document)...)
		// A key another revision lets go stays: Withdraw removes only what
		// the state records the running one as letting go.
		read.Release(released...)
	}
	// And whatever every other revision an engine pod runs or starts on
	// reads as required stays for it, as in a sync.
	if err := run.keepServing(ctx, eng, &read); err != nil {
		return ctrl.Result{}, err
	}
	// What the fleet keepServing read settles or marks is saved before
	// anything is asked of the engine: the reload below may land and the save
	// after it be lost, and a mark the read lowered that only this reconcile
	// knows of would be loaded unread again, which no activation's epoch is
	// above (markChanges).
	state, moved := run.settleRead(ctx, eng, active, state)
	if moved {
		if err := secretsync.SaveState(ctx, run.Client, stateRef, c, state); err != nil {
			return ctrl.Result{}, err
		}
	}
	next, res, withdrawErr := secretsync.Withdraw(ctx, eng, read, state)
	if len(res.Deleted) > 0 {
		run.event(corev1.EventTypeNormal, "SecretsSynced", "SyncSecrets",
			"Deleted %d values from the engine's secret store whose Secret or key no longer exists, which the revision the "+
				"engine runs goes without, while the next document is held: %s", len(res.Deleted), strings.Join(res.Deleted, ", "))
	}
	run.keep(read, next, res.Retained)
	run.held = res.Held
	run.tellKept()
	if run.secretsSynced != nil {
		// The Secrets another part requires, which hold the document.
		run.secretsSynced.Message += run.retainedSentence() + run.heldSentence()
	}
	var reactivateErr error
	if withdrawErr == nil {
		next, reactivateErr = run.reactivate(ctx, eng, active, res.Reload, res.ReloadAction, next, stateRef)
	}
	if err := secretsync.SaveState(ctx, run.Client, stateRef, c, next); err != nil {
		return ctrl.Result{}, err
	}
	if withdrawErr != nil {
		return run.withdrawalFailed(ctx, withdrawErr), nil
	}
	if reactivateErr != nil {
		return run.activationFailed(ctx, reactivateErr, next), nil
	}
	return run.heldResync(ctrl.Result{}), nil
}

// keep records the absent names of read (Missing) that the secret sync left
// in the engine's store, retained, because a revision read keeps names for
// (Desired.Keep, Desired.KeepServed) reads them as required, or is one next
// does not record the reading of, or a pod's revisions are not known
// (Desired.KeepUnread): run.retained, which the conditions name, the
// revisions and pods that keep them (run.keptFor, run.keptUnread), and
// run.kept, those the state the reconcile loaded did not record as retained
// (run.told), for tellKept to tell once rather than at every reconcile; next
// records them.
func (run *configRun) keep(read secretsync.Desired, next secretsync.State, retained []string) {
	run.retained = nil
	holders := map[string]bool{}
	for _, m := range read.Missing() {
		name := secretsync.StoreName(m.Key)
		if !slices.Contains(retained, name) {
			continue
		}
		run.retained = append(run.retained, m.Key)
		for _, revision := range read.Holders(name, next) {
			holders[revision] = true
		}
	}
	run.keptFor, run.keptUnread = nil, nil
	if len(run.retained) > 0 {
		// The fleet's revision first, which is the one the conditions
		// have always named, then those pods run beside it.
		for _, revision := range slices.Sorted(maps.Keys(holders)) {
			k := keptRevision{revision: revision, fleets: revision == read.Running(), unknown: !next.Records(revision),
				unsealed: run.unsealed != "" && revision == run.unsealed}
			if run.serving != nil {
				k.pods = run.serving.revisions[revision]
			}
			run.keptFor = append(run.keptFor, k)
		}
		slices.SortStableFunc(run.keptFor, func(a, b keptRevision) int {
			switch {
			case a.fleets == b.fleets:
				return 0
			case a.fleets:
				return -1
			}
			return 1
		})
		if run.serving != nil {
			run.keptUnread = run.serving.unread
		}
	}
	run.kept = slices.DeleteFunc(next.Retained(), func(name string) bool { return slices.Contains(run.told, name) })
}

// keptRevision is a revision that keeps a value whose Secret is gone: the
// fleet's, or one engine pods run or start on beside it (pods), which reads
// it as required, or is one the operator does not know the reading of
// (unknown), or the fleet's that the engine stores without a seal, which
// nobody can read and its nodes do not run (unsealed).
type keptRevision struct {
	revision string
	fleets   bool
	pods     revisionPods
	unknown  bool
	unsealed bool
}

// tellKept records a Normal SecretKept event naming the names keep found the
// store newly keeps, once, and forgets them.
func (run *configRun) tellKept() {
	if len(run.kept) == 0 {
		return
	}
	run.event(corev1.EventTypeNormal, "SecretKept", "SyncSecrets",
		"Kept %d values in the engine's secret store whose Secret or key no longer exists, which the next document lets go "+
			"without, because %s: each goes once a revision that lets it go runs: %s",
		len(run.kept), run.keptBecause(true, "each"), strings.Join(run.kept, ", "))
	run.kept = nil
}

// keptPlainly reports whether what keeps run.retained is the fleet's
// revision alone, which every pod runs: the case the conditions name it in
// as "the revision the engine runs".
func (run *configRun) keptPlainly() bool {
	return len(run.keptUnread) == 0 && len(run.keptFor) == 1 && run.keptFor[0].fleets
}

// keptBecause says why the store keeps what run.retained names: each
// revision that keeps it, the fleet's by its id when byID and as "the
// revision the engine runs" otherwise, reads them as required, or is one the
// operator does not know the reading of and takes to; and a pod whose
// revisions the operator does not know may run or start on one that does.
// The fleet's revision, where the engine stores it without a seal, is "the
// engine's active revision" and never the one it runs: nobody can read it,
// and its nodes run the revision before it. them names what they read.
func (run *configRun) keptBecause(byID bool, them string) string {
	var reasons []string
	for _, k := range run.keptFor {
		subject := "the revision the engine runs"
		switch {
		case !k.fleets:
			subject = fmt.Sprintf("revision %s, which %s,", k.revision, k.pods.phrase())
		case k.unsealed && byID:
			subject = fmt.Sprintf("revision %s, the engine's active revision,", k.revision)
		case k.unsealed:
			subject = "the engine's active revision"
		case byID:
			subject = fmt.Sprintf("revision %s, which the engine runs,", k.revision)
		}
		reading := "reads " + them + " as required"
		switch {
		case k.unsealed:
			reading = "is stored without a seal and nobody can read it, while the engine's nodes run the revision they " +
				"applied before it, which reads the store again whenever a node restarts, so the operator takes it to " +
				"read " + them + " as required"
		case k.unknown:
			reading = "is one the operator has no record of activating and does not render, so it takes it to read " + them +
				" as required"
		}
		reasons = append(reasons, subject+" "+reading)
	}
	if len(run.keptUnread) > 0 {
		reasons = append(reasons, fmt.Sprintf("the operator does not know what %s %s or %s on, and takes it to read %s as required",
			strings.Join(run.keptUnread, ", "), plural(len(run.keptUnread), "runs", "run"),
			plural(len(run.keptUnread), "boots", "boot"), them))
	}
	return strings.Join(reasons, "; and ")
}

// podsPhrase names engine pods for a sentence: "engine pod a" or "engine
// pods a, b".
func podsPhrase(pods []string) string {
	return plural(len(pods), "engine pod ", "engine pods ") + strings.Join(pods, ", ")
}

// plural is one for n of 1, and many for any other n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// retainedSentence is what the Company's SecretsSynced message says of
// run.retained: nothing when it is empty.
func (run *configRun) retainedSentence() string {
	if len(run.retained) == 0 {
		return ""
	}
	keys := make([]string, len(run.retained))
	for i, k := range run.retained {
		keys[i] = k.String()
	}
	return fmt.Sprintf(" The engine's secret store keeps the value it held for the %d the document lets go without whose "+
		"Secret or key no longer exists (%s): %s, and each goes once a revision that lets it go runs.",
		len(run.retained), strings.Join(keys, ", "), run.keptBecause(false, "each"))
}

// settle makes the activation the secret sync's state owes (reactivate) and
// saves the state it ends with: in a reconcile of enforce that applies
// nothing, after a document the engine refused, and after an apply, whose
// sync may have withdrawn a name the revision just applied started with.
// active is the engine's document as the reconcile last read it, before the
// sync, nil to read it; reload is the engine's own re-activation at the
// sync's last write or removal (secretsync.Result.Reload), which reactivate
// takes for the activation where it landed. Its error is the API server's; an
// activation that failed is reported (activationFailed), and retry is when
// that report says the operator tries again, zero where nothing failed.
func (run *configRun) settle(
	ctx context.Context, eng *engine.Client, active *engine.ActiveConfig, reload *engine.Reload, reloadAction string,
	state secretsync.State, ref types.NamespacedName,
) (retry time.Duration, err error) {
	if len(state.Withdrawn()) == 0 && len(state.Written()) == 0 {
		return 0, nil
	}
	next, reactivateErr := run.reactivate(ctx, eng, active, reload, reloadAction, state, ref)
	if err := secretsync.SaveState(ctx, run.Client, ref, run.company, next); err != nil {
		return 0, err
	}
	if reactivateErr != nil {
		return run.activationFailed(ctx, reactivateErr, next).RequeueAfter, nil
	}
	return 0, nil
}

// settleRefused is settle for a document the engine refused, as the
// reconcile ends there: it comes back when the report of an activation that
// failed says it does, and otherwise when an object changes.
func (run *configRun) settleRefused(
	ctx context.Context, eng *engine.Client, active *engine.ActiveConfig, reload *engine.Reload, reloadAction string,
	state secretsync.State, ref types.NamespacedName,
) (ctrl.Result, error) {
	retry, err := run.settle(ctx, eng, active, reload, reloadAction, state, ref)
	return run.heldResync(ctrl.Result{RequeueAfter: retry}), err
}

// withdrawalFailed reports err, which kept the engine from reading its store
// without a value whose Secret no longer exists: until the operator gets
// through, the engine may still start a tool server or an executor run with it.
// It returns when to try again, as engineRetry says for err.
func (run *configRun) withdrawalFailed(ctx context.Context, err error) ctrl.Result {
	retry := run.engineRetry(err)
	logf.FromContext(ctx).Error(err, "Failed to keep the value of a deleted Secret from the engine")
	again := fmt.Sprintf("the operator tries again in %s", retry)
	if engine.Waitless(err) {
		again = fmt.Sprintf("the engine answered that waiting won't clear what it met, so somebody has to clear it in "+
			"the engine, and the operator asks again every %s until then", retry)
	}
	run.event(corev1.EventTypeWarning, "SecretWithdrawalFailed", "ReloadConfig", "%s", ended(fmt.Sprintf(
		"The engine may still start a tool server or an executor run with the value of a Secret that no longer exists, "+
			"which the revision it runs lets go without; %s: %s", again, err)))
	return ctrl.Result{RequeueAfter: retry}
}

// activationFailed reports err, which kept the engine from reading the store
// changes state still owes an activation for: a withdrawal where it owes one,
// as withdrawalFailed reports it; otherwise only values written, which the
// revision the engine runs read before they were written, so it may go on
// starting tool servers and executor runs with the values the Secrets held
// before, a credential somebody rotated included (SecretRefreshFailed). It
// returns when to try again, as engineRetry says for err.
func (run *configRun) activationFailed(ctx context.Context, err error, state secretsync.State) ctrl.Result {
	if len(state.Withdrawn()) > 0 || len(state.Written()) == 0 {
		return run.withdrawalFailed(ctx, err)
	}
	retry := run.engineRetry(err)
	logf.FromContext(ctx).Error(err, "Failed to have the engine read the values the operator wrote into its secret store")
	run.event(corev1.EventTypeWarning, "SecretRefreshFailed", "ReloadConfig", "%s", ended(fmt.Sprintf(
		"The engine may still start a tool server or an executor run with a value a Secret held before the operator "+
			"wrote its current one into the engine's secret store (%s), which the revision it runs read before; the "+
			"operator tries again in %s: %s", strings.Join(state.Written(), ", "), retry, err)))
	return ctrl.Result{RequeueAfter: retry}
}

// storeChanges are the changes of the engine's secret store that the revision
// the engine runs has not read and reads once it activates again: names
// withdrawn that it lets go without, and names written that it references.
// A withdrawn name it references as required, or one its reading the operator
// does not know, it keeps until the next revision runs, and a written name it
// does not reference changes nothing it runs.
type storeChanges struct {
	withdrawn, written []string
}

// owedBy is what an activation of active again would read of the store
// changes state records.
func owedBy(state secretsync.State, active *engine.ActiveConfig) storeChanges {
	letGo := state.LetGo(active.RevisionID)
	references := storeNamesIn(active.Document)
	return storeChanges{
		withdrawn: slices.DeleteFunc(state.Withdrawn(), func(name string) bool { return !slices.Contains(letGo, name) }),
		written:   slices.DeleteFunc(state.Written(), func(name string) bool { return !slices.Contains(references, name) }),
	}
}

func (c storeChanges) empty() bool { return len(c.withdrawn) == 0 && len(c.written) == 0 }

// owedIn reports whether state still owes an activation for any of c.
func (c storeChanges) owedIn(state secretsync.State) bool {
	withdrawn, written := state.Withdrawn(), state.Written()
	return slices.ContainsFunc(c.withdrawn, func(name string) bool { return slices.Contains(withdrawn, name) }) ||
		slices.ContainsFunc(c.written, func(name string) bool { return slices.Contains(written, name) })
}

// reads is how the engine reads its store once a revision activates after c:
// without what was withdrawn, with what was written.
func (c storeChanges) reads() string {
	var parts []string
	if len(c.withdrawn) > 0 {
		parts = append(parts, "without "+strings.Join(c.withdrawn, ", "))
	}
	if len(c.written) > 0 {
		parts = append(parts, "with the values now written under "+strings.Join(c.written, ", "))
	}
	return strings.Join(parts, " and ")
}

// why is reads with the reason for each change, which revision, the one
// activated again, lets go without or references.
func (c storeChanges) why(revision string) string {
	var parts []string
	if len(c.withdrawn) > 0 {
		parts = append(parts, fmt.Sprintf("without %s, whose Secret or key no longer exists and which %s lets go "+
			"without", strings.Join(c.withdrawn, ", "), revision))
	}
	if len(c.written) > 0 {
		parts = append(parts, fmt.Sprintf("with the values now written under %s, which %s references and read "+
			"before they were written", strings.Join(c.written, ", "), revision))
	}
	return strings.Join(parts, ", and ")
}

// made is what the operator did to the store.
func (c storeChanges) made() string {
	switch {
	case len(c.written) == 0:
		return fmt.Sprintf("removed %s from the engine's secret store", strings.Join(c.withdrawn, ", "))
	case len(c.withdrawn) == 0:
		return fmt.Sprintf("wrote %s into the engine's secret store", strings.Join(c.written, ", "))
	}
	return fmt.Sprintf("removed %s from and wrote %s into the engine's secret store", strings.Join(c.withdrawn, ", "),
		strings.Join(c.written, ", "))
}

// reactivate activates the engine's running revision again when state records
// a change of the store it reads once it does (owedBy): a withdrawn name it
// lets go without (State.LetGo), or a value written under a name it
// references, which it read before the write: a rotated credential, or a
// value the sync wrote back. It returns state with every withdrawal settled:
// the engine has read its store without them, runs nothing that references
// them, or runs a revision that reads them as required, or one whose reading
// state does not record, which keeps what it read until the next one runs. A
// value written under a name the running revision does not reference stays
// owed, for the next write of the document to read. active is the engine's
// document as this reconcile read it, nil to read it here, and nil with
// run.unsealed set where the engine runs a revision stored without a seal,
// over which nothing is activated and state is returned as it is.
//
// It is the activation for every store change no write of the document is
// coming to settle: a document the engine refused, or one held for a Secret.
// A value rotated while the document was refused went unread by the revision
// the engine runs for as long as it was refused, while every node that
// restarted read the new one.
//
// The engine activates its revision again itself when a write or a removal
// changes a value that revision reads (Crewlet's ADR-0070), and reload is
// what the last such request answered (secretsync.Result.Reload), made by
// the engine call reloadAction (secretsync.Result.ReloadAction): where it
// landed, it came after every change the sync made, and it is the
// activation; asking for another would activate the same document twice.
// Where it is nil or failed, the operator asks for one (POST /config/reload).
//
// Neither names a revision: the engine stores again whichever revision the
// fleet's activation pointer names when the request lands, built on it, and
// a write landing inside the request fails it as a lost race. So the
// revision it activates again is read back, and recorded as letting go what
// the one read lets go only where it is that document; otherwise the state
// goes on recording the one read, which is no longer the fleet's, and a
// Warning says so. Before the operator's request the changes are marked by
// the fleet's activation epoch, and state saved in ref (markChanges): a
// request that fails, or loses its race to another writer, is read back, and a revision
// the fleet runs above that epoch came after every change, whoever activated
// it, and settles them (reloadFailed).
func (run *configRun) reactivate(
	ctx context.Context, eng *engine.Client, active *engine.ActiveConfig, reload *engine.Reload, reloadAction string,
	state secretsync.State, ref types.NamespacedName,
) (secretsync.State, error) {
	if len(state.Withdrawn()) == 0 && len(state.Written()) == 0 {
		return state, nil
	}
	if active == nil && run.unsealed != "" {
		// The engine runs a revision it stores without a seal, which it
		// neither shows nor activates again: no activation can be made over
		// it, and asking for one is refused every time. The withdrawals stay
		// owed, and the write that replaces it settles them, its activation
		// reading the store without them (State.Activated).
		return state, nil
	}
	if active == nil {
		var err error
		if active, err = activeConfig(ctx, eng); err != nil {
			return state, err
		}
	}
	if active == nil {
		return state.Reactivated(), nil
	}
	owed := owedBy(state, active)
	if owed.empty() {
		return state.Reactivated(), nil
	}
	// WHO ACTIVATED IT: the engine, at the request of the sync that made
	// reload (a write as much as a removal), or the operator, asking for it.
	by := ""
	if reload.Landed() {
		by = reloadAction
	}
	reloaded := engine.ApplyResult{}
	if by != "" {
		reloaded.RevisionID, reloaded.Epoch = reload.RevisionID, reload.Epoch
	} else {
		// How the revision the request may store reads every name, should
		// the engine not confirm activating it and a later read find it
		// running (config_unconfirmed.go): as the one it activates again
		// does.
		own, ok := run.ownWrite(active)
		ahead := ok && !state.Pends(own.AppliedHash)
		if ahead {
			state = state.Pending(own.AppliedHash, state.LetGoKeys(active.RevisionID))
		}
		var err error
		if state, err = run.markChanges(ctx, eng, state, ref, ahead); err != nil {
			return state, err
		}
		if reloaded, err = eng.ReloadConfig(ctx, reactivationSummary(owed)); err != nil {
			return run.reloadFailed(ctx, eng, active, state, owed,
				fmt.Errorf("activating revision %s again, so that the engine reads its secret store %s: %w",
					active.RevisionID, owed.reads(), err))
		}
	}
	now, err := activeConfig(ctx, eng)
	switch {
	case err != nil:
		run.reactivatedUnconfirmed(active, reloaded, by, owed, "the operator could not read it back: "+err.Error())
	case now == nil || now.RevisionID != reloaded.RevisionID:
		run.reactivatedUnconfirmed(active, reloaded, by, owed, "the engine runs another revision since")
	case !bytes.Equal(now.Document, active.Document):
		run.reactivatedUnconfirmed(active, reloaded, by, owed,
			"its document is not that one: somebody activated another revision before the request landed")
	default:
		run.recordReactivation(active, reloaded, by, owed, nil)
		return state.Activated(reloaded.RevisionID, state.LetGoKeys(active.RevisionID)), nil
	}
	// Whatever its document, the revision the request activated read the
	// store after every write and withdrawal the state records.
	return state.Reloaded(), nil
}

// reloadFailed is state once a request to activate active again, so that the
// engine reads the store changes owed, failed with failed: settled as far as
// the fleet's revision, read afresh, activated after the changes, since
// nothing read before the request saw what activated since, and failed where
// any of owed is still owed. A revision the fleet's activation names above
// the epoch the changes were marked by (markChanges) came after every one of
// them, whoever activated it: the request itself, where it landed and its
// answer did not, or another writer, who won the race with it. Where its
// document is active's, the fleet runs that document again reading the store
// as owed says, as a reactivation the operator confirmed does, and it is
// recorded as one (recordReactivation, whose event says the request failed);
// otherwise it is a revision whose reading the operator does not know, which
// a Warning says, and the state goes on recording the one read, as
// reactivatedUnconfirmed leaves it. A request that failed outright leaves the
// fleet on active, which settles nothing.
//
// One whose revision the engine stored and could not confirm activating may
// yet land. That is decided from the answer, before anything is read back,
// since a read that fails, or finds the revision running with the fleet's
// pointer unreadable, leaves nothing else to say it: where active is the
// operator's, the revision is recorded as its own write of the same document,
// for the next read to settle as it settles one (reloadUnconfirmed), and the
// changes it was asked for after await it, so that finding it running
// settles them (State.Awaiting) however the pointer reads. Recorded only
// where the read back still found active, it was taken, once it landed, for a
// revision somebody else activated.
func (run *configRun) reloadFailed(
	ctx context.Context, eng *engine.Client, active *engine.ActiveConfig, state secretsync.State, owed storeChanges,
	failed error,
) (secretsync.State, error) {
	if stored, ok := engine.UnconfirmedActivation(failed); ok {
		run.reloadUnconfirmed(active, stored)
		state = state.Awaiting(stored)
	}
	now, err := activeConfig(ctx, eng)
	if err != nil || now == nil || now.RevisionID == active.RevisionID {
		return state, failed
	}
	fleet, err := eng.Fleet(ctx)
	if err != nil || fleet.Activation == nil || fleet.Activation.RevisionID != now.RevisionID {
		return state, failed
	}
	settled := state.Ran(fleet.Activation.Epoch)
	if owed.owedIn(settled) {
		return state, failed
	}
	activation := engine.ApplyResult{RevisionID: now.RevisionID, Epoch: fleet.Activation.Epoch}
	if bytes.Equal(now.Document, active.Document) {
		run.recordReactivation(active, activation, "", owed, failed)
		return settled.Adopted(now.RevisionID, state.LetGoKeys(active.RevisionID)), nil
	}
	run.event(corev1.EventTypeWarning, "Reactivated", "ReloadConfig", "%s", ended(fmt.Sprintf(
		"Did not activate revision %s again: revision %s, another document, activated as epoch %d after the operator "+
			"%s, and read the store %s; the operator takes it to read every value as required, as it does any revision "+
			"it has no record of activating, and the request failed: %s",
		active.RevisionID, now.RevisionID, fleet.Activation.Epoch, owed.made(), owed.reads(), failed)))
	return settled, nil
}

// reactivationSummary is the summary of the revision that activates the
// running document again to read the store changes owed.
func reactivationSummary(owed storeChanges) string {
	const shown = 4
	list := func(names []string) string {
		text := strings.Join(names[:min(len(names), shown)], ", ")
		if len(names) > shown {
			text += fmt.Sprintf(" and %d more", len(names)-shown)
		}
		return text
	}
	var parts []string
	if len(owed.withdrawn) > 0 {
		parts = append(parts, "without "+list(owed.withdrawn)+", whose Secret or key no longer exists")
	}
	if len(owed.written) > 0 {
		parts = append(parts, "to read the values now written under "+list(owed.written))
	}
	return "crewlet-operator: activated again " + strings.Join(parts, ", and ")
}

// recordReactivation records the running document activated again as
// reloaded, reading the store changes owed, by the engine at the request of
// the sync's engine call by (WriteSecret, PutSecrets or DeleteSecret: a write
// or a removal of a value the revision reads), or at the operator's request,
// by empty. A revision the operator recorded
// as its own is replaced by the one that activates it again, with the same
// content, so that the next one it applies does not take it for an edit
// somebody else made; so is a write of the operator's the engine could not
// confirm activating and runs, whose record (status.config.unconfirmed) moves
// to the revision that activates it again, for the next reconcile that
// applies a document to confirm and report (config_unconfirmed.go); one it
// did not write stays somebody else's. failed is the error the operator's
// request answered where the engine was read back running the document again
// anyway (reloadFailed), nil where it answered the activation.
//
// reloaded is recorded once: a record status.config.unconfirmed already holds
// of it, a reload the engine stored and could not confirm activating
// (reloadUnconfirmed), goes, as the read back settled it. Kept, it was a
// second entry of one revision, which the API server refuses, or a revision
// the next reconcile confirmed a second time.
func (run *configRun) recordReactivation(
	active *engine.ActiveConfig, reloaded engine.ApplyResult, by string, owed storeChanges, failed error,
) {
	if failed == nil {
		who, action := reactivatedBy(by)
		run.event(corev1.EventTypeNormal, "Reactivated", action,
			"%s revision %s again, unchanged, as revision %s (epoch %d), so that the engine reads its secret store "+
				"%s.", who, active.RevisionID, reloaded.RevisionID, reloaded.Epoch, owed.why("the revision"))
	} else {
		run.event(corev1.EventTypeNormal, "Reactivated", "ReloadConfig", "%s", ended(fmt.Sprintf(
			"The engine runs revision %s again, unchanged, as revision %s (epoch %d), activated after the operator %s, "+
				"so it reads the store %s; the operator's request to activate it again failed: %s",
			active.RevisionID, reloaded.RevisionID, reloaded.Epoch, owed.made(), owed.why("the revision"), failed)))
	}
	recorded := run.recordedConfig()
	config := recorded.DeepCopy()
	if i := unconfirmedIndex(config.Unconfirmed, reloaded.RevisionID); i >= 0 {
		config.Unconfirmed = slices.Delete(config.Unconfirmed, i, i+1)
		run.config = config
	}
	now := metav1.NewTime(run.clock())
	switch i := unconfirmedIndex(config.Unconfirmed, active.RevisionID); {
	case recorded.RevisionID == active.RevisionID:
		config.RevisionID, config.Epoch, config.AppliedAt = reloaded.RevisionID, new(reloaded.Epoch), &now
		if run.applied.kind == appliedInRevision && run.applied.revision == active.RevisionID {
			run.applied.revision = reloaded.RevisionID
		}
	case i >= 0:
		// Confirmed only in the secret sync's state (settlePending), by a
		// reconcile that holds its document: the record goes on to the
		// revision that runs it now, which is the write's as much as the
		// one it activated again was, rather than staying with one the
		// next read finds overtaken.
		config.Unconfirmed[i].RevisionID, config.Unconfirmed[i].WrittenAt = reloaded.RevisionID, &now
	default:
		return
	}
	run.config = config
}

// reactivatedUnconfirmed reports an activation of active again, reading the
// store changes owed, by the engine at the sync's engine call by or at the
// operator's request (by empty), that activated reloaded, which the operator
// could not confirm is active's document, for why: it takes reloaded to read every
// name as required, and, where owed withdrew a name, a revision somebody else
// activated may have needed a value it removed for active, which let it go.
func (run *configRun) reactivatedUnconfirmed(
	active *engine.ActiveConfig, reloaded engine.ApplyResult, by string, owed storeChanges, why string,
) {
	who, action := reactivatedBy(by)
	// why ends with what the read back met, which may be the engine's own
	// sentence: the sentence that quotes it is ended by ended.
	activated := ended(fmt.Sprintf("%s the engine's document again as revision %s (epoch %d), so that the engine "+
		"reads its secret store %s; but the operator cannot confirm that revision %s is revision %s's document, as %s",
		who, reloaded.RevisionID, reloaded.Epoch, owed.why("revision "+active.RevisionID), reloaded.RevisionID,
		active.RevisionID, why))
	takes := fmt.Sprintf("It takes revision %s to read every value as required", reloaded.RevisionID)
	if len(owed.withdrawn) > 0 {
		takes += ", and one that reads a value it removed so goes without what it starts with it until the Secret " +
			"exists again"
	}
	run.event(corev1.EventTypeWarning, "Reactivated", action, "%s %s.", activated, takes)
}

// reactivatedBy is who an activation again is said to be by, and the action
// its event names: the engine, at the sync's engine call by, or the operator
// asking for it (ReloadConfig) where by is empty.
func reactivatedBy(by string) (who, action string) {
	if by == "" {
		return "Activated", "ReloadConfig"
	}
	return "The engine activated", by
}

// readOptional reads, while the render refuses the document, the Secret keys
// the objects let go without their value: those every reference to which, in
// the specs of the Company, its class and its parts, is a value a tool server
// or an executor run is started with under a key its block declares optional.
// A reference the render did not reach, in an object it refused, counts as
// required, so what it reads is never a value a revision may need. A value
// too large for the store is there, so it is no concern here.
func (run *configRun) readOptional(ctx context.Context) (secretsync.Desired, error) {
	starts := render.StartValuesOf(run.input())
	var optional []secretref.Key
	for i := range starts {
		k := starts[i].Key
		if starts[i].Optional && !slices.Contains(optional, k) && !run.required(k, starts) {
			optional = append(optional, k)
		}
	}
	if len(optional) == 0 {
		return secretsync.Desired{}, nil
	}
	read, err := secretsync.Resolve(ctx, run.APIReader, optional, optional)
	for _, e := range joined(err) {
		var tooLarge *secretsync.TooLargeError
		if e != nil && !errors.As(e, &tooLarge) {
			return secretsync.Desired{}, err
		}
	}
	return read, nil
}

// required reports whether the Company, its class or one of its parts reads k
// somewhere starts does not count as an optional start value of its own.
func (run *configRun) required(k secretref.Key, starts []render.StartValue) bool {
	if slices.ContainsFunc(run.parts, func(p *part) bool { return p.requires(k, starts) }) {
		return true
	}
	owner := render.ObjectRef{Kind: render.KindCompany, Name: run.company.Name}
	if requiresKey(owner, secretRefsOf(run.company), k, starts) {
		return true
	}
	return run.class != nil && slices.ContainsFunc(secretRefsOf(run.class), func(ref secretref.Key) bool { return references(ref, k) })
}
