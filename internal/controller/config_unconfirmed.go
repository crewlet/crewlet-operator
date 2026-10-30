package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// An activation the engine could not confirm.
//
// A write of the company document stores a revision, then asks the fleet to
// activate it by a compare-and-set on its activation pointer, conditioned on
// the revision the write was built on; so does a reload, which activates the
// running revision again, conditioned on that one. When the answer to the
// compare-and-set is lost, the engine answers 503 naming the revision it
// stored (engine.UnconfirmedActivation): the pointer may name it or may not.
// Nothing the engine says afterwards says which write a revision was, so
// without a record of it a write that did land read on the next reconcile as
// a revision somebody else activated: under Enforce, where its document is
// the rendered one, a revision the operator adopts as one it did not write,
// and where the render had moved on since, an edit somebody else made, which
// the next write reported as drift it corrected; under Seed, a document the
// engine had before the operator seeded one.
//
// So each such write is recorded (status.config.unconfirmed) with what a
// confirmed write records, and settled by the next read of the revision the
// engine runs, before anything is written. The engine runs one of them: that
// write landed, at the epoch the fleet's activation names, and it is recorded
// as the operator's, as a confirmed write is. The engine runs the revision
// one was built on: that one has not landed yet, and still may, so it stays
// and the write is sent again on the same condition, which the engine refuses
// once one of them lands (and the reconcile then reads it, and records it).
// Every other one can no longer land, since it was conditioned on a revision
// the fleet has moved past, and goes; but for one written into an engine that
// ran no document, which under Seed is the seed whatever revision runs now,
// since the engine stored it while it had none, so either it landed and was
// replaced since, or another writer's create reached the engine first, which
// nothing the engine says tells apart (status.config.overtaken, overtakenSeed).
// It stays recorded until a reconcile decides with it, since one that fails
// before it gets that far must not lose it.
//
// What the write's activation read of the secret store is settled two ways.
// The changes it read were marked with the fleet's activation epoch before it
// was sent (markChanges), and the next read of a revision the engine runs
// above that epoch settles them, whoever activated it (settleRead). A change
// no read of the pointer followed, which it cannot have where the pointer was
// unreadable, and that is when the engine is likeliest to lose an answer,
// awaits the revision the engine stored instead (secretsync.State.Awaiting,
// saved as the answer comes back), and finding that revision running settles
// it (secretsync.State.Found). What a write needs beside that is how its
// document reads every name, which the state records under the write's
// appliedHash before it is sent (secretsync.State.Pending), since nothing
// else says it once the render has moved on; the revision found running a
// write of the operator's records that reading (settlePending). A reconcile
// whose document is held settles the state the same way, and leaves
// status.config, and the report of the write, to the reconcile that applies
// one: its conditions say why it holds.

// maxUnconfirmed bounds status.config.unconfirmed and status.config.overtaken
// (their MaxItems): writes whose answers are all lost come ten seconds apart
// while the engine cannot reach its coordination store, and an activation
// that has neither landed nor been refused sixteen writes later is not one a
// later read will see land.
const maxUnconfirmed = 16

// recordedConfig is status.config as this reconcile leaves it: what it decided
// to write over the Company's record, where it decided anything.
func (run *configRun) recordedConfig() crewletaiv1alpha1.ConfigStatus {
	if run.config != nil {
		return *run.config
	}
	return run.company.Status.Config
}

// boundedWrites is writes without the oldest past maxUnconfirmed.
func boundedWrites(writes []crewletaiv1alpha1.UnconfirmedRevision) []crewletaiv1alpha1.UnconfirmedRevision {
	if extra := len(writes) - maxUnconfirmed; extra > 0 {
		return writes[extra:]
	}
	return writes
}

// unconfirmedIndex is the index in writes of the write of revision, -1 for
// none.
func unconfirmedIndex(writes []crewletaiv1alpha1.UnconfirmedRevision, revision string) int {
	if revision == "" {
		return -1
	}
	return slices.IndexFunc(writes, func(write crewletaiv1alpha1.UnconfirmedRevision) bool {
		return write.RevisionID == revision
	})
}

// ownWrite is the record that makes active, the revision the engine runs as
// this reconcile read it, a write of the operator's, as
// status.config.unconfirmed holds one: status.config where it records that
// revision, or a write the engine could not confirm activating that is it
// and runs its document (runsItsDocument). ok is false for a revision the
// operator did not write, for one it recorded without the hash of what it
// runs, and for none.
func (run *configRun) ownWrite(active *engine.ActiveConfig) (write crewletaiv1alpha1.UnconfirmedRevision, ok bool) {
	if active == nil {
		return crewletaiv1alpha1.UnconfirmedRevision{}, false
	}
	recorded := run.recordedConfig()
	if recorded.RevisionID == active.RevisionID && recorded.AppliedHash != "" {
		return crewletaiv1alpha1.UnconfirmedRevision{
			RevisionID: active.RevisionID, AppliedHash: recorded.AppliedHash, Objects: recorded.Objects,
		}, true
	}
	if i := unconfirmedIndex(recorded.Unconfirmed, active.RevisionID); i >= 0 &&
		runsItsDocument(&recorded.Unconfirmed[i], active) {
		return recorded.Unconfirmed[i], true
	}
	return crewletaiv1alpha1.UnconfirmedRevision{}, false
}

// runsItsDocument reports whether active, the revision write records, runs
// the document write was recorded with: a reload's (documentSHA256), since
// the engine activates again whichever revision is active when the request
// lands, so the revision it stored runs somebody else's document where they
// activated one in between. A write of the rendered document runs what the
// engine stored for it, and its record names no document.
func runsItsDocument(write *crewletaiv1alpha1.UnconfirmedRevision, active *engine.ActiveConfig) bool {
	return write.DocumentSHA256 == "" || active != nil && write.DocumentSHA256 == documentDigest(active.Document)
}

// documentDigest is documentSHA256 of document: its SHA-256, in hex.
func documentDigest(document []byte) string {
	sum := sha256.Sum256(document)
	return hex.EncodeToString(sum[:])
}

// unconfirmed records write, whose activation the engine could not confirm,
// as revision stored, reports it on ConfigApplied, and returns when the next
// reconcile settles it (settleUnconfirmed): as soon as the engine's
// Retry-After says, the scale of one batch on its coordination store, and ten
// seconds on where it sends none (engineRetry).
func (run *configRun) unconfirmed(stored string, write writeAttempt, err error) time.Duration {
	retry := run.engineRetry(err)
	recorded := run.recordedConfig()
	config := recorded.DeepCopy()
	now := metav1.NewTime(run.clock())
	config.Unconfirmed = boundedWrites(append(config.Unconfirmed, crewletaiv1alpha1.UnconfirmedRevision{
		RevisionID: stored, BaseRevisionID: write.base, AppliedHash: write.hash, WrittenAt: &now,
		Objects: run.objectCounts(), Replaced: write.replaced, DriftedAt: write.driftedAt,
	}))
	run.config = config
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse,
		Reason: crewletaiv1alpha1.ReasonEngineUnavailable,
		Message: ended(fmt.Sprintf("The engine stored the rendered document as revision %s and could not confirm that it "+
			"activated it, so it may run it or may not. The operator reads which revision the engine runs before it writes "+
			"again, in %s: %s", stored, retry, err)),
	}
	return retry
}

// reloadUnconfirmed records stored, the revision a request to activate active
// again stored and could not confirm activating, as a write of the operator's
// of active's document, where active is the operator's (ownWrite): it was
// conditioned on active, and may land yet, and once a read finds it running it
// is recorded and reported as that write would be (settleUnconfirmed), rather
// than as a revision somebody else activated. A revision the operator did not
// write stays somebody else's when activated again.
//
// It is recorded with the digest of active's document (documentSHA256), since
// the request names no revision: where somebody activated another revision
// before it landed, the engine stored that one's document, which a read then
// finds running under stored, and that revision is not the operator's
// (runsItsDocument). And it replaced nothing of its own: its base is the
// operator's write, and what that write replaced, where it is recorded here
// too, is reported once, whichever of the two is confirmed
// (confirmUnconfirmed); copied onto this record, it named the operator's own
// write as one somebody else made, or as one stored without a seal.
func (run *configRun) reloadUnconfirmed(active *engine.ActiveConfig, stored string) {
	own, ok := run.ownWrite(active)
	recorded := run.recordedConfig()
	if !ok || unconfirmedIndex(recorded.Unconfirmed, stored) >= 0 {
		return
	}
	config := recorded.DeepCopy()
	now := metav1.NewTime(run.clock())
	own.RevisionID, own.BaseRevisionID, own.WrittenAt = stored, active.RevisionID, &now
	own.Replaced, own.DriftedAt, own.DocumentSHA256 = "", "", documentDigest(active.Document)
	config.Unconfirmed = boundedWrites(append(config.Unconfirmed, own))
	run.config = config
}

// overtakenBy splits writes, of status.config.unconfirmed, by runs, a revision
// none of them is that the engine runs: still are the ones built on runs,
// which may land yet, and overtaken the others written into an engine that ran
// no document, which Seed takes for the seed (overtakenSeed). Every other one
// can no longer land, and nothing reads it again.
func overtakenBy(
	writes []crewletaiv1alpha1.UnconfirmedRevision, runs string,
) (still, overtaken []crewletaiv1alpha1.UnconfirmedRevision) {
	for i := range writes {
		switch writes[i].BaseRevisionID {
		case runs:
			still = append(still, writes[i])
		case "":
			overtaken = append(overtaken, writes[i])
		}
	}
	return still, overtaken
}

// settleUnconfirmed settles the writes status.config.unconfirmed records
// against active, the revision the engine runs as this reconcile read it. One
// of them is active, with the document it was recorded with
// (runsItsDocument): it is recorded as the operator's write
// (confirmUnconfirmed). Otherwise the ones built on active stay, and every
// other one goes, to status.config.overtaken where it was written into an
// engine that ran no document; a reload found running another document was
// built on a revision somebody else activated, and goes too. Under Observe
// nothing is settled: the operator writes nothing there, and Observe records
// the revision it finds in sync.
func (run *configRun) settleUnconfirmed(ctx context.Context, eng *engine.Client, active *engine.ActiveConfig) error {
	recorded := run.recordedConfig()
	if len(recorded.Unconfirmed) == 0 || run.policy() == crewletaiv1alpha1.ManagementPolicyObserve {
		return nil
	}
	runs := ""
	if active != nil {
		runs = active.RevisionID
	}
	if i := unconfirmedIndex(recorded.Unconfirmed, runs); i >= 0 && runsItsDocument(&recorded.Unconfirmed[i], active) {
		return run.confirmUnconfirmed(ctx, eng, &recorded, i)
	}
	still, overtaken := overtakenBy(recorded.Unconfirmed, runs)
	if len(still) < len(recorded.Unconfirmed) {
		config := recorded.DeepCopy()
		config.Unconfirmed = still
		config.Overtaken = boundedWrites(append(config.Overtaken, overtaken...))
		run.config = config
	}
	return nil
}

// confirmUnconfirmed records recorded.Unconfirmed[i], a revision the engine
// runs, as the operator's write, and reports it, as recordApply records and
// reports one the engine confirmed: at the epoch the fleet's activation names,
// which only a read of it gives, since the engine answered none; from now,
// since nothing says when the activation landed, and the engine is held to
// applying its epoch applyDeadline after the operator found it running, as an
// adopted revision is; and saying what it replaced (reportReplaced), which the
// record kept, or where it is a reload of a write recorded here too, what
// that write replaced, which only it records (reloadUnconfirmed), and which
// then goes with it: the revision it activated again landed, since the reload
// runs. A write built on it stays unconfirmed, and every other one is
// overtaken by it, and so is every write status.config.overtaken keeps: none
// can land now, nor be the seed, since a write of the operator's runs.
//
// It fails where the fleet cannot be read, or answers without its activation,
// and with an *activationAhead, a lost race, where the activation names
// another revision: converge reads again from the start, and waits for the
// node it reads where the second read finds the same.
func (run *configRun) confirmUnconfirmed(
	ctx context.Context, eng *engine.Client, recorded *crewletaiv1alpha1.ConfigStatus, i int,
) error {
	write := recorded.Unconfirmed[i]
	fleet, err := run.fleetOf(ctx, eng)
	if err != nil {
		return fmt.Errorf("the engine runs revision %s, which the operator wrote and the engine could not confirm "+
			"activating, and its fleet, which says as what epoch, could not be read: %w", write.RevisionID, err)
	}
	switch {
	case fleet.Activation == nil:
		// The read of the document named the revision, so it is active:
		// the engine answered its fleet without reading the pointer.
		return fmt.Errorf("the engine runs revision %s, which the operator wrote and the engine could not confirm "+
			"activating, and its fleet answered without its activation, which says as what epoch", write.RevisionID)
	case fleet.Activation.RevisionID != write.RevisionID:
		return &activationAhead{read: write.RevisionID, activation: *fleet.Activation, unconfirmed: true}
	}
	epoch := fleet.Activation.Epoch
	// The write a reload activated again, where that is recorded here too,
	// and so on down: the first of them says what the rendered document
	// replaced.
	first, landed := write, []string{write.RevisionID}
	for {
		j := unconfirmedIndex(recorded.Unconfirmed, first.BaseRevisionID)
		if j < 0 || slices.Contains(landed, recorded.Unconfirmed[j].RevisionID) {
			break
		}
		first = recorded.Unconfirmed[j]
		landed = append(landed, first.RevisionID)
	}
	still, _ := overtakenBy(slices.DeleteFunc(slices.Clone(recorded.Unconfirmed),
		func(w crewletaiv1alpha1.UnconfirmedRevision) bool { return slices.Contains(landed, w.RevisionID) }),
		write.RevisionID)
	found := metav1.NewTime(run.clock())
	run.confirmed = write.RevisionID
	run.config = &crewletaiv1alpha1.ConfigStatus{
		RevisionID: write.RevisionID, Epoch: new(epoch), AppliedHash: write.AppliedHash, AppliedAt: &found,
		Objects: write.Objects, Unconfirmed: still,
	}
	condition := metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied,
		Message: fmt.Sprintf("Applied the rendered document as revision %s.", write.RevisionID),
	}
	run.reportReplaced(&condition, write.RevisionID, first.BaseRevisionID, first.Replaced, first.DriftedAt)
	run.configApplied = &condition
	run.applied = appliedVerdict{kind: appliedInRevision, revision: write.RevisionID}
	run.event(corev1.EventTypeNormal, "Activated", "ApplyConfig",
		"The engine runs revision %s as epoch %d: the operator activated it, and the engine could not confirm the "+
			"activation when it was made.", write.RevisionID, epoch)
	return nil
}

// settlePending is state recording the reading of active, the revision the
// engine runs as this reconcile read it, where that is a write of the
// operator's whose reading state does not record (ownWrite): the write's
// activation went unconfirmed, or the save of its record after it was lost,
// and the reading state recorded under the write's appliedHash before it was
// sent says how its document reads every name (secretsync.State.Confirmed).
// The readings of the writes status.config.unconfirmed no longer names go: no
// revision the operator has not recorded can be one of them. A revision state
// already records keeps its reading, and so does one the operator did not
// write, or none, which the caller adopts where its document is the rendered
// one.
func (run *configRun) settlePending(state secretsync.State, active *engine.ActiveConfig) secretsync.State {
	if active != nil && !state.Records(active.RevisionID) {
		if own, ok := run.ownWrite(active); ok {
			if confirmed, ok := state.Confirmed(active.RevisionID, own.AppliedHash); ok {
				state = confirmed
			}
		}
	}
	recorded := run.recordedConfig()
	keep := make([]string, len(recorded.Unconfirmed))
	for i := range recorded.Unconfirmed {
		keep[i] = recorded.Unconfirmed[i].AppliedHash
	}
	return state.KeepPending(keep)
}

// overtakenSeed is the write into an engine with no document, of
// status.config.overtaken, that seeded the engine under Seed, nil for none: the
// engine had no document when it was written, and runs one now, so that write
// landed, and the revision that runs replaced it, or another write reached
// the engine first in the moment between the reconcile's read and that write,
// which nothing the engine says tells apart, and which the operator takes for
// the first, whatever revision runs now.
func (run *configRun) overtakenSeed() *crewletaiv1alpha1.UnconfirmedRevision {
	recorded := run.recordedConfig()
	for i := range recorded.Overtaken {
		if write := &recorded.Overtaken[i]; write.BaseRevisionID == "" {
			return write
		}
	}
	return nil
}
