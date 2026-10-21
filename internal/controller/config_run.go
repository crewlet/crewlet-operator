package controller

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// configRun is one reconcile of one Company's document: what it read, and
// the verdict it reached, which writeStatus records.
type configRun struct {
	*CompanyConfigReconciler

	company *crewletaiv1alpha1.Company
	class   *crewletaiv1alpha1.EngineClass
	parts   []*part
	// eng is the client of the Company's engine, once resolved.
	eng *engine.Client
	doc *render.Document
	// desired is the store entries the document calls for, once read.
	desired *secretsync.Desired

	// The Company's conditions this reconcile decided, nil where it left
	// one as it was, and status.config when it changed it.
	secretsSynced *metav1.Condition
	configApplied *metav1.Condition
	engineHealthy *metav1.Condition
	// identityClaimed is IdentityClaimed, decided wherever the engine's
	// health was read.
	identityClaimed *metav1.Condition
	config          *crewletaiv1alpha1.ConfigStatus
	// wrote is whether this reconcile activated a revision.
	wrote bool

	// objectsChecked and secretsChecked are whether the reconcile got as
	// far as resolving the parts' references to other objects, and to
	// Secret keys, so that their ResolvedRefs condition says something.
	objectsChecked, secretsChecked bool
	// applied is what the reconcile found about the parts' Applied
	// condition.
	applied appliedVerdict
	// health is the engine's verdict on each tool, by key; nil when the
	// reconcile did not read it.
	health map[string]engine.IntegrationStatus

	// fleet is the engine's GET /fleet once this reconcile has read it, and
	// fleetErr why it could not; both nil when it did not try (fleetOf).
	fleet    *engine.Fleet
	fleetErr error

	// rendered is where the rendered document pins its seats, and running
	// where the document the engine runs or is applying does, once this
	// reconcile has found out; see recordPlacement. runningRevision is the
	// revision of running when it is the engine's own document (Observe,
	// Seed), whose apply only the engine's record of it tells, and empty for
	// a document the operator wrote or found in sync with the rendered one,
	// whose apply verifyApplied settles.
	rendered, running *crewletaiv1alpha1.RevisionPlacement
	runningRevision   string
	// active is the document the engine ran when converge last read it,
	// nil before it did or while the engine had none.
	active []byte
	// placement is what status.config.placement becomes once this reconcile
	// writes the status; nil leaves it as it is.
	placement *placementUpdate
	// holding is whether doc keeps a moving seat pinned where it runs (see
	// holdMoves).
	holding bool
}

// appliedKind is what a reconcile found out about whether the parts are in
// the revision the engine runs.
type appliedKind int

const (
	// appliedUnknown leaves every part's Applied condition as it was: the
	// reconcile did not get as far as the engine.
	appliedUnknown appliedKind = iota
	// appliedInRevision is every part in revision.
	appliedInRevision
	// appliedBlocked is a document that could not be applied: the parts
	// with a problem of their own report it, the others are blocked by
	// them, unless their current generation is already running.
	appliedBlocked
	// appliedObserved is a document compared, not applied (Observe): the
	// parts that differ from the engine's document have drifted, the
	// others are in revision.
	appliedObserved
	// appliedNotManaged is a document the engine owns (Seed, once seeded).
	appliedNotManaged
	// appliedPending is a revision activated and not applied yet: the
	// parts whose current generation the engine runs already stay in
	// revision, the others are applying.
	appliedPending
	// appliedFailed is a revision the engine did not apply: the parts
	// whose current generation the engine runs already stay in revision,
	// the others are not applied.
	appliedFailed
)

type appliedVerdict struct {
	kind     appliedKind
	revision string
	// message says why, for appliedBlocked, appliedNotManaged and
	// appliedFailed.
	message string
}

// reconcile decides the Company's and its parts' conditions and, as the
// management policy allows, writes the document and its Secret values into
// the engine, and its seats' pauses. An error is one to retry with backoff:
// the API server did not answer. Everything about the objects, and an engine
// that cannot be reached, is a condition.
func (run *configRun) reconcile(ctx context.Context) (ctrl.Result, error) {
	result, err := run.reconcileDocument(ctx)
	if err != nil || !run.engineRuns() {
		return result, err
	}
	// Whatever became of the document: a pause is not in it (see
	// syncPauses).
	eng := run.eng
	if eng == nil {
		// The document did not get as far as the engine (an object of it
		// is refused, or a Secret is missing), and its conditions say so.
		var resolveErr error
		if eng, resolveErr = run.Engines.Engine(ctx, run.company); resolveErr != nil {
			logf.FromContext(ctx).V(1).Info("No engine to read the seats' pauses from", "error", resolveErr.Error())
			return result, nil
		}
	}
	return run.syncPauses(ctx, eng, result), nil
}

// engineRuns reports whether the Company has an engine to talk to: it is
// accepted, with a class, not suspended, and its runtime is set up.
func (run *configRun) engineRuns() bool {
	c := run.company
	return run.class != nil && meta.IsStatusConditionTrue(c.Status.Conditions, crewletaiv1alpha1.ConditionAccepted) &&
		!suspended(c) && meta.FindStatusCondition(c.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady) != nil
}

// reconcileDocument is reconcile's part for the company document.
func (run *configRun) reconcileDocument(ctx context.Context) (ctrl.Result, error) {
	c := run.company
	if !meta.IsStatusConditionTrue(c.Status.Conditions, crewletaiv1alpha1.ConditionAccepted) {
		// The runtime reconciler says why; its status write brings the
		// Company back here once it is accepted.
		return ctrl.Result{}, nil
	}
	class, refused, err := engineClassOf(ctx, run.Client, c)
	if err != nil {
		return ctrl.Result{}, err
	}
	if refused != nil {
		// Accepted is about to turn False; the runtime reconciler reports it.
		return ctrl.Result{}, nil
	}
	run.class = class

	doc, err := render.Company(run.input())
	// render reports every reference that does not resolve at once, so
	// the parts it does not name have none.
	run.objectsChecked = true
	// Whatever became of the document: whether a seat's code work can run,
	// and whether its turns can, are facts about its own spec.
	run.checkCodeWork(render.CodeWorkOf(run.input()))
	run.checkExecutors(render.ExecutorOf(run.input()), render.AgentModeOf(run.input()))
	run.checkReported(render.ReportedOf(run.input()))
	if err != nil {
		refusals := render.RefErrors(err)
		if len(refusals) == 0 {
			return ctrl.Result{}, fmt.Errorf("rendering the document of Company %s/%s: %w", c.Namespace, c.Name, err)
		}
		run.refused(refusals)
		run.lookAtFleet(ctx)
		return ctrl.Result{}, nil
	}
	run.doc = doc
	rendered, err := revisionPlacement(doc.YAML)
	if err != nil {
		return ctrl.Result{}, err
	}
	run.rendered = &rendered

	// Read before the engine is reached, so that a missing Secret is
	// reported while the engine is still starting. Under Seed the values
	// are read only for the one write into an empty engine: once the engine
	// owns the document, the Secrets are no longer the operator's concern.
	// Under Observe nothing is pushed, so they are never read.
	if run.policy() == crewletaiv1alpha1.ManagementPolicyEnforce {
		if blocked, err := run.resolveSecrets(ctx); blocked || err != nil {
			if blocked {
				run.lookAtFleet(ctx)
			}
			return ctrl.Result{}, err
		}
	}

	if suspended(c) || meta.FindStatusCondition(c.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady) == nil {
		// No engine runs, or the runtime has not been set up yet: a
		// change of spec.suspend or of RuntimeReady brings the Company
		// back here.
		return ctrl.Result{}, nil
	}
	eng, err := run.Engines.Engine(ctx, c)
	if err != nil {
		run.unavailable(err)
		return ctrl.Result{RequeueAfter: engineRetryInterval}, nil
	}
	run.eng = eng
	result, err := run.converge(ctx, eng)
	if err != nil {
		if isAPIServerError(err) {
			return ctrl.Result{}, err
		}
		if !run.writeRefused(err) && !run.notGranted(err) && !run.seatHeld(err) {
			run.unavailable(err)
		}
		_, _ = run.fleetOf(ctx, eng)
		return ctrl.Result{RequeueAfter: retryAfter(err)}, nil
	}
	health, healthErr := run.readHealth(ctx, eng)
	result = run.verifyApplied(ctx, eng, health, healthErr, result)
	run.narrowPlacement(ctx, eng, health, healthErr)
	result = run.holdResync(result)
	result = run.identityResync(result)
	// For the seats' placement, if the verification did not need it.
	_, _ = run.fleetOf(ctx, eng)
	return result, nil
}

// resolveSecrets reads the values of every Secret key the document
// references into run.desired. blocked reports keys that are missing or too
// large, which it records on the parts that reference them.
func (run *configRun) resolveSecrets(ctx context.Context) (blocked bool, err error) {
	desired, err := secretsync.Resolve(ctx, run.APIReader, run.doc.Refs, nil)
	if err != nil {
		if !run.secretsMissing(err) {
			return false, err
		}
		run.secretsChecked = true
		return true, nil
	}
	run.secretsChecked = true
	run.desired = &desired
	return false, nil
}

// isAPIServerError reports whether err came from the Kubernetes API server
// rather than from the engine.
func isAPIServerError(err error) bool {
	var status apierrors.APIStatus
	return errors.As(err, &status)
}

func (run *configRun) input() *render.Input {
	in := &render.Input{
		Company:         run.company,
		Class:           run.class,
		StoreName:       secretsync.StoreName,
		ProvisionedName: secretsync.ProvisionedName,
		PublicBaseURL:   run.company.Status.Endpoints.PublicBaseURL,
	}
	for _, p := range run.parts {
		switch o := p.obj.(type) {
		case *crewletaiv1alpha1.Unit:
			in.Units = append(in.Units, *o)
		case *crewletaiv1alpha1.Seat:
			in.Seats = append(in.Seats, *o)
		case *crewletaiv1alpha1.LLMProvider:
			in.LLMProviders = append(in.LLMProviders, *o)
		case *crewletaiv1alpha1.MCPServer:
			in.MCPServers = append(in.MCPServers, *o)
		case *crewletaiv1alpha1.Integration:
			in.Integrations = append(in.Integrations, *o)
		case *crewletaiv1alpha1.Schedule:
			in.Schedules = append(in.Schedules, *o)
		}
	}
	return in
}

// policy is the Company's management policy.
func (run *configRun) policy() crewletaiv1alpha1.ManagementPolicy {
	return render.ManagementPolicyOf(run.company)
}

// converge does what the management policy says with the engine's document.
// A revision someone wrote between its read and its write makes it start
// over once, from a fresh read; a second one in the same reconcile is an
// error, and the reconcile is retried.
func (run *configRun) converge(ctx context.Context, eng *engine.Client) (ctrl.Result, error) {
	for attempt := 1; ; attempt++ {
		active, err := activeConfig(ctx, eng)
		if err != nil {
			return ctrl.Result{}, err
		}
		if active != nil {
			run.active = active.Document
		}
		var result ctrl.Result
		switch policy := run.policy(); {
		case policy == crewletaiv1alpha1.ManagementPolicyObserve:
			return run.observe(ctx, eng, active)
		case policy == crewletaiv1alpha1.ManagementPolicySeed && active != nil:
			if err := run.seeded(active); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
		default:
			if run.desired == nil {
				blocked, err := run.resolveSecrets(ctx)
				if blocked || err != nil {
					return ctrl.Result{}, err
				}
			}
			result, err = run.enforce(ctx, eng, *run.desired, active)
		}
		lost := errors.Is(err, engine.ErrRevisionAdvanced) || errors.Is(err, engine.ErrAlreadyConfigured)
		if lost && attempt == 1 {
			logf.FromContext(ctx).V(1).Info("Another writer changed the engine's document during the apply; reading it again")
			continue
		}
		return result, err
	}
}

// activeConfig is the engine's active configuration, or nil when it has none
// yet.
func activeConfig(ctx context.Context, eng *engine.Client) (*engine.ActiveConfig, error) {
	active, err := eng.ActiveConfig(ctx)
	if errors.Is(err, engine.ErrNoActiveRevision) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &active, nil
}

// enforce makes the engine run the rendered document: under Enforce always,
// and under Seed for the one write into an engine with no document (active
// is nil then).
func (run *configRun) enforce(
	ctx context.Context, eng *engine.Client, desired secretsync.Desired, active *engine.ActiveConfig,
) (ctrl.Result, error) {
	c := run.company
	if err := run.holdMoves(ctx, active); err != nil {
		return ctrl.Result{}, err
	}
	stateRef := types.NamespacedName{Namespace: c.Namespace, Name: secretSyncStateName(c)}
	state, err := secretsync.LoadState(ctx, run.APIReader, stateRef, c)
	if notOwned, ok := errors.AsType[*secretsync.NotOwnedError](err); ok {
		// The state of an earlier Company of this name, which the garbage
		// collector is about to delete, describes another engine's store:
		// start over, and take the Secret over with the next save. Anyone
		// else's Secret is left alone, and nothing is applied.
		if notOwned.Controller == nil || !isPreviousCompany(notOwned.Controller, c) {
			run.secretsSynced = &metav1.Condition{
				Type: crewletaiv1alpha1.ConditionSecretsSynced, Status: metav1.ConditionFalse,
				Reason: crewletaiv1alpha1.ReasonNameConflict, Message: capitalize(notOwned.Error()) + ".",
			}
			run.block(crewletaiv1alpha1.ReasonNameConflict, capitalize(notOwned.Error())+".")
			return ctrl.Result{}, nil
		}
		state, err = secretsync.State{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	// The running document keeps its names until the next one replaces
	// it: deleting one now would leave the engine resolving it to nothing
	// for as long as the new document fails to apply.
	pushing := desired
	if active != nil {
		pushing.Keep(active.RevisionID, storeNamesIn(active.Document)...)
	}
	state, pushed, err := run.sync(ctx, eng, pushing, state, stateRef)
	if err != nil {
		return ctrl.Result{}, err
	}

	hash := appliedHash(run.doc, state, desired.Names())
	diffs, err := diffWith(run.doc, active)
	if err != nil {
		return ctrl.Result{}, err
	}
	recorded := c.Status.Config
	// A value written back although no Secret changed (one somebody else
	// overwrote, or one the store lost) leaves the hash where it was, but the
	// engine reads its store only when a revision activates: since the
	// overwrite, a reload may have handed it the other value, which it would
	// keep running. So the same document is applied again, which is how a
	// rotated credential reaches a running engine anyway.
	if active != nil && len(diffs) == 0 && hash == recorded.AppliedHash && len(pushed.Put) == 0 {
		run.inSync(active)
		return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
	}
	// A revision the operator did not write, with other content: an edit
	// made through the engine's API or dashboard, or a document the engine
	// had before the operator took it over.
	drifted := active != nil && len(diffs) > 0 && active.RevisionID != recorded.RevisionID

	checked, err := eng.ApplyConfig(ctx, engine.ApplyRequest{Document: run.doc.YAML, DryRun: true})
	if err != nil {
		if run.invalid(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// The pins go into the record before the write that activates them, not
	// with the status written after it: between the two the runtime
	// reconciler may see a Company that no longer asks for a pool the new
	// revision pins a seat to, and a record without it, and delete the pods
	// the engine is about to need.
	if err := run.widenPlacement(ctx, *run.rendered); err != nil {
		return ctrl.Result{}, err
	}
	req := engine.ApplyRequest{Document: run.doc.YAML, Summary: run.summary(active, diffs, pushed.Put)}
	if checked.BaseRevisionID == "" {
		req.CreateOnly = true
	} else {
		// What the dry run checked against: a write in between is lost
		// rather than overwritten unseen.
		req.IfMatch = checked.BaseRevisionID
	}
	if run.policy() == crewletaiv1alpha1.ManagementPolicySeed {
		req.IfMatch, req.CreateOnly = "", true
	}
	applied, err := eng.ApplyConfig(ctx, req)
	if err != nil {
		if run.invalid(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	run.recordApply(applied, hash, drifted, active, diffs, req.Summary)

	// The engine runs the new document's names now; the old ones go.
	if _, _, err := run.sync(ctx, eng, desired, state, stateRef); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: applyPollInterval}, nil
}

// sync pushes the desired values into the engine's secret store, deletes
// what nothing references, and saves what it did, whether or not it got
// through.
func (run *configRun) sync(
	ctx context.Context, eng *engine.Client, desired secretsync.Desired, state secretsync.State, ref types.NamespacedName,
) (secretsync.State, secretsync.Result, error) {
	next, result, syncErr := secretsync.Sync(ctx, eng, desired, state, engine.TokenLogin(render.TokenOperator))
	if err := secretsync.SaveState(ctx, run.Client, ref, run.company, next); err != nil {
		return next, result, errors.Join(syncErr, err)
	}
	if syncErr != nil {
		run.secretsSynced = &metav1.Condition{
			Type: crewletaiv1alpha1.ConditionSecretsSynced, Status: metav1.ConditionFalse,
			Reason: crewletaiv1alpha1.ReasonEngineUnavailable, Message: capitalize(syncErr.Error()) + ".",
		}
		return next, result, syncErr
	}
	if len(result.Put) > 0 || len(result.Deleted) > 0 {
		// Names only: a store name says which Secret key it holds, never
		// the value.
		run.event(corev1.EventTypeNormal, "SecretsSynced", "SyncSecrets", "Wrote %d and deleted %d values in the engine's secret store: %s",
			len(result.Put), len(result.Deleted), strings.Join(slices.Concat(result.Put, result.Deleted), ", "))
	}
	if len(result.Overwritten) > 0 {
		// A Warning, because somebody holding a credential with the
		// operator's grant replaced a value Kubernetes is the source of, and
		// whoever did should hear that it was put back rather than find out
		// when their value stops working.
		by := make([]string, 0, len(result.Overwritten))
		for _, o := range result.Overwritten {
			by = append(by, fmt.Sprintf("%s (written by %s, with %s)", o.Name, o.By, o.Credential))
		}
		run.event(corev1.EventTypeWarning, "SecretOverwritten", "SyncSecrets",
			"Wrote back %d values somebody else had replaced in the engine's secret store; the Kubernetes Secret each refers to is their source: %s",
			len(result.Overwritten), strings.Join(by, ", "))
	}
	run.secretsSynced = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionSecretsSynced, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonSynced,
		Message: fmt.Sprintf("The engine's secret store holds the current value of each of the %d Secret keys the document references.",
			len(desired.Names())),
	}
	return next, result, nil
}

// secretSyncStateName is the Secret that holds what the operator last pushed
// into a Company's engine's secret store (see secretsync.State).
func secretSyncStateName(c *crewletaiv1alpha1.Company) string { return c.Name + "-secret-sync" }

// storeNamePattern finds the ${NAME} references in a served document.
var storeNamePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// storeNamesIn returns the secret-store names the operator owns that a
// document served by the engine references. The engine serves every
// reference written as a whole ${NAME}, which is how the operator writes
// them all.
func storeNamesIn(document []byte) []string {
	var names []string
	for _, m := range storeNamePattern.FindAllSubmatch(document, -1) {
		if name := string(m[1]); secretsync.Owns(name) && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// appliedHash is status.config.appliedHash: the rendered document's hash and
// the fingerprints of the values it references, so that a rotated value
// changes it while the document does not.
func appliedHash(doc *render.Document, state secretsync.State, names []string) string {
	sum := sha256.Sum256([]byte(doc.Hash + "\n" + state.Digest(names)))
	return hex.EncodeToString(sum[:])
}

// diffWith compares the rendered document with the engine's; no active
// document is a difference everywhere, which the empty path stands for.
func diffWith(doc *render.Document, active *engine.ActiveConfig) ([]render.Path, error) {
	if active == nil {
		return []render.Path{{}}, nil
	}
	return render.Diff(doc.YAML, active.Document)
}

// inSync records an engine that runs the rendered document, with every
// value it references, without a write: possibly as a revision the operator
// did not write itself, such as a reload of the same document, which it
// adopts.
func (run *configRun) inSync(active *engine.ActiveConfig) {
	recorded := run.company.Status.Config
	if recorded.RevisionID != active.RevisionID {
		config := recorded.DeepCopy()
		config.RevisionID = active.RevisionID
		config.Objects = run.objectCounts()
		run.config = config
	}
	run.recordPlacement(*run.rendered, false)
	// The reason of the last write (Applied, DriftCorrected) stays until
	// the next one: it is what that write did.
	if current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied); current != nil &&
		current.Status == metav1.ConditionTrue {
		kept := *current
		run.configApplied = &kept
	} else {
		run.configApplied = &metav1.Condition{
			Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied,
			Message: fmt.Sprintf("The engine runs the rendered document as revision %s.", active.RevisionID),
		}
	}
	run.applied = appliedVerdict{kind: appliedInRevision, revision: active.RevisionID}
}

// recordApply records a revision the operator wrote.
func (run *configRun) recordApply(
	applied engine.ApplyResult, hash string, drifted bool, active *engine.ActiveConfig, diffs []render.Path, summary string,
) {
	now := metav1.Now()
	run.wrote = true
	run.config = &crewletaiv1alpha1.ConfigStatus{
		RevisionID:  applied.RevisionID,
		Epoch:       new(applied.Epoch),
		AppliedHash: hash,
		AppliedAt:   &now,
		Objects:     run.objectCounts(),
	}
	run.recordPlacement(*run.rendered, false)
	condition := metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied,
		Message: fmt.Sprintf("Applied the rendered document as revision %s.", applied.RevisionID),
	}
	run.applied = appliedVerdict{kind: appliedInRevision, revision: applied.RevisionID}
	switch {
	case run.policy() == crewletaiv1alpha1.ManagementPolicySeed:
		condition.Reason = crewletaiv1alpha1.ReasonSeeded
		condition.Message = fmt.Sprintf("Seeded the engine with the rendered document as revision %s; "+
			"under the Seed policy the engine owns the document from now on.", applied.RevisionID)
		run.applied = appliedVerdict{kind: appliedNotManaged, revision: applied.RevisionID, message: fmt.Sprintf(
			"Seeded into revision %s; under the Company's Seed policy the engine owns the document since, and changes to this object are not applied.",
			applied.RevisionID)}
	case drifted:
		condition.Reason = crewletaiv1alpha1.ReasonDriftCorrected
		condition.Message = fmt.Sprintf("Revision %s, which the operator did not write, differed from the rendered document at %s; "+
			"revision %s replaced it.", active.RevisionID, placesOf(diffs), applied.RevisionID)
		run.event(corev1.EventTypeWarning, crewletaiv1alpha1.ReasonDrifted, "DetectDrift",
			"The engine ran revision %s, which the operator did not write and which differed from the rendered document at %s; "+
				"under the Enforce policy revision %s replaced it.", active.RevisionID, placesOf(diffs), applied.RevisionID)
	}
	run.configApplied = &condition
	// Activated, not applied: the engine applies it asynchronously, and
	// verifyApplied records the Applied event once it has.
	run.event(corev1.EventTypeNormal, "Activated", "ApplyConfig", "Activated revision %s as epoch %d (%s).",
		applied.RevisionID, applied.Epoch, summary)
	if len(applied.Warnings) > 0 {
		run.event(corev1.EventTypeWarning, "EngineWarnings", "ApplyConfig", "The engine runs revision %s with warnings: %s",
			applied.RevisionID, sentences(applied.Warnings))
	}
}

// observe compares the rendered document with the engine's and reports the
// difference, writing nothing: no revision, and no Secret value.
func (run *configRun) observe(ctx context.Context, eng *engine.Client, active *engine.ActiveConfig) (ctrl.Result, error) {
	run.secretsSynced = notManaged(crewletaiv1alpha1.ConditionSecretsSynced,
		"Under the Company's Observe policy the operator does not write the engine's secret store.")
	if _, err := eng.ApplyConfig(ctx, engine.ApplyRequest{Document: run.doc.YAML, DryRun: true}); err != nil {
		if run.invalid(err) {
			return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
		}
		return ctrl.Result{}, err
	}
	diffs, err := diffWith(run.doc, active)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(diffs) == 0 {
		if run.company.Status.Config.RevisionID != active.RevisionID {
			run.config = &crewletaiv1alpha1.ConfigStatus{RevisionID: active.RevisionID, Objects: run.objectCounts()}
		}
		run.recordPlacement(*run.rendered, false)
		run.runningRevision = active.RevisionID
		run.configApplied = &metav1.Condition{
			Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonInSync,
			Message: fmt.Sprintf("The engine runs the rendered document as revision %s.", active.RevisionID),
		}
		run.applied = appliedVerdict{kind: appliedObserved, revision: active.RevisionID}
		return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
	}

	// The engine's own document is what runs, once the engine has applied
	// it, and nothing the operator does moves it: its pins join the record
	// until narrowPlacement finds it applied. No document runs nothing.
	if active == nil {
		run.recordPlacement(crewletaiv1alpha1.RevisionPlacement{}, true)
	} else {
		running, err := revisionPlacement(active.Document)
		if err != nil {
			return ctrl.Result{}, err
		}
		run.recordPlacement(running, false)
		run.runningRevision = active.RevisionID
	}
	message := "The engine runs no company document; under the Company's Observe policy the operator does not write one."
	revision := ""
	if active != nil {
		revision = active.RevisionID
		message = fmt.Sprintf("The engine runs revision %s, which differs from the rendered document at %s; "+
			"under the Company's Observe policy the operator does not overwrite it.", active.RevisionID, placesOf(diffs))
		for _, p := range diffs {
			if target := run.partAt(p); target != nil {
				target.drifted = append(target.drifted, p.String())
			}
		}
	} else {
		for _, p := range run.parts {
			p.drifted = append(p.drifted, "the engine runs no company document")
		}
	}
	condition := metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonDrifted,
		Message: message,
	}
	if current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied); current == nil ||
		current.Reason != condition.Reason || current.Message != bounded(condition.Message) {
		run.event(corev1.EventTypeWarning, crewletaiv1alpha1.ReasonDrifted, "DetectDrift", "%s", message)
	}
	run.configApplied = &condition
	run.applied = appliedVerdict{kind: appliedObserved, revision: revision}
	return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
}

// seeded records a Seed company whose engine has a document: from now on
// the engine owns it.
func (run *configRun) seeded(active *engine.ActiveConfig) error {
	running, err := revisionPlacement(active.Document)
	if err != nil {
		return err
	}
	run.recordPlacement(running, false)
	run.runningRevision = active.RevisionID
	recorded := run.company.Status.Config.RevisionID
	message := fmt.Sprintf("The engine had a company document (revision %s) before the operator seeded one; "+
		"under the Company's Seed policy the operator leaves it to the engine.", active.RevisionID)
	if recorded != "" {
		message = fmt.Sprintf("The engine was seeded with the rendered document as revision %s and owns the document since; "+
			"it runs revision %s.", recorded, active.RevisionID)
	}
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonSeeded,
		Message: message,
	}
	run.secretsSynced = notManaged(crewletaiv1alpha1.ConditionSecretsSynced,
		"The Company's Seed policy handed the document, and the secret store, to the engine once it was seeded.")
	run.applied = appliedVerdict{kind: appliedNotManaged, message: "Under the Company's Seed policy the engine owns the document, " +
		"so changes to this object are not applied."}
	return nil
}

func notManaged(conditionType, message string) *metav1.Condition {
	return &metav1.Condition{
		Type: conditionType, Status: metav1.ConditionUnknown, Reason: crewletaiv1alpha1.ReasonNotManaged, Message: message,
	}
}

// refused records the references render could not resolve, each on the part
// that holds it, and blocks the document.
func (run *configRun) refused(refusals []*render.RefError) {
	for _, ref := range refusals {
		finding := ref.Field + ": " + ref.Message
		// A refusal of the Company's own (spec.llm.order) is in the
		// message below and on no part.
		target := run.part(render.ObjectRef{Kind: ref.Kind, Name: ref.Name})
		switch {
		case target == nil:
		case ref.Reason == crewletaiv1alpha1.ReasonConflict:
			target.conflicts = append(target.conflicts, finding)
			target.handleTaken = target.handleTaken || (ref.Kind == render.KindSeat && ref.Field == "spec.handle")
		case ref.Reason == crewletaiv1alpha1.ReasonPerSeatNotAllowed || ref.Reason == crewletaiv1alpha1.ReasonPoolCapacityExceeded:
			if target.unplaceableReason == "" {
				target.unplaceableReason = ref.Reason
			}
			target.unplaceable = append(target.unplaceable, finding)
		case unrunnable(ref.Reason):
			if target.unrunnableReason == "" {
				target.unrunnableReason = ref.Reason
			}
			target.unrunnable = append(target.unrunnable, finding)
		default:
			if target.unresolvedReason == "" {
				target.unresolvedReason = ref.Reason
			}
			target.unresolved = append(target.unresolved, finding)
		}
	}
	run.block(refusals[0].Reason, "The document cannot be rendered: "+sentences(refusalLines(refusals)))
}

// unrunnable reports whether reason refuses a seat whose CLI login its pods
// cannot keep, or whose code work runs on another CLI's provider, which the
// seat reports as Applied=False rather than as an unresolved reference.
func unrunnable(reason string) bool {
	switch reason {
	case crewletaiv1alpha1.ReasonCLIAuthNotPortable, crewletaiv1alpha1.ReasonCodingAgentMismatch:
		return true
	}
	return false
}

// checkCodeWork records each Seat's code work on its part, and emits a
// Warning event on a Seat whose code work turns unrunnable or changes its
// reason, a new problem to announce: with SandboxModelUnsupported or
// SandboxLoginNotPortable the document applies and nothing on the Company
// says so.
func (run *configRun) checkCodeWork(codeWork map[string]render.CodeWork) {
	for _, p := range run.parts {
		seat, ok := p.obj.(*crewletaiv1alpha1.Seat)
		if !ok {
			continue
		}
		w, ok := codeWork[seat.Name]
		if !ok {
			continue
		}
		p.codeWork = &w
		if w.Reason == "" {
			continue
		}
		before := meta.FindStatusCondition(p.base.(*crewletaiv1alpha1.Seat).Status.Conditions,
			crewletaiv1alpha1.ConditionCodeWorkRunnable)
		if before == nil || before.Reason != w.Reason {
			run.Recorder.Eventf(seat, nil, corev1.EventTypeWarning, w.Reason, "CheckCodeWork", "%s: %s.", w.Field, w.Message)
		}
	}
}

// checkExecutors records each agent Seat's executor and each LLMProvider's
// agent mode on its part, for decide to report and announce.
func (run *configRun) checkExecutors(executors map[string]render.Executor, agentModes map[string]render.AgentMode) {
	for _, p := range run.parts {
		switch o := p.obj.(type) {
		case *crewletaiv1alpha1.Seat:
			if e, ok := executors[o.Name]; ok {
				p.executor = &e
			}
		case *crewletaiv1alpha1.LLMProvider:
			if m, ok := agentModes[o.Name]; ok {
				p.agentMode = &m
			}
		}
	}
}

// checkReported records on each part what the render found wrong with its
// references that holds nothing, for decide to report and announce.
func (run *configRun) checkReported(reported []*render.RefError) {
	for _, ref := range reported {
		p := run.part(render.ObjectRef{Kind: ref.Kind, Name: ref.Name})
		if p == nil {
			continue
		}
		if p.reportedReason == "" {
			p.reportedReason = ref.Reason
		}
		p.reported = append(p.reported, ref.Field+": "+ref.Message)
	}
}

// announce emits a Warning event on a part whose decided status now shows a
// finding that holds nothing: a Seat whose Ready turns to its executor's
// reason, an LLMProvider whose AgentModeRunnable turns False, a part whose
// ResolvedRefs turns False for a reported finding. The document applies with
// each, and nothing on the Company says so. The event follows the condition
// that shows the finding, from the status the part had to the one decided,
// rather than the finding alone: a Seat's Ready says Suspended while its
// Company is, and a part's ResolvedRefs a finding that holds the document
// before a reported one, and a finding compared with a condition showing
// another reason would be announced again on every reconcile.
func (run *configRun) announce(p *part) {
	turned := func(condition string, reason string) *metav1.Condition {
		now := meta.FindStatusCondition(*conditionsOf(p.obj), condition)
		if reason == "" || now == nil || now.Status != metav1.ConditionFalse || now.Reason != reason {
			return nil
		}
		before := meta.FindStatusCondition(*conditionsOf(p.base), condition)
		if before != nil && before.Status == now.Status && before.Reason == now.Reason {
			return nil
		}
		return now
	}
	if p.executor != nil {
		if c := turned(crewletaiv1alpha1.ConditionReady, p.executor.Reason); c != nil {
			run.Recorder.Eventf(p.obj, nil, corev1.EventTypeWarning, c.Reason, "CheckExecutor", "%s: %s.",
				p.executor.Field, p.executor.Message)
		}
	}
	if p.agentMode != nil {
		if c := turned(crewletaiv1alpha1.ConditionAgentModeRunnable, p.agentMode.Reason); c != nil {
			run.Recorder.Eventf(p.obj, nil, corev1.EventTypeWarning, c.Reason, "CheckAgentMode", "%s", p.agentMode.Message+".")
		}
	}
	if len(p.unresolved) == 0 {
		if c := turned(crewletaiv1alpha1.ConditionResolvedRefs, p.reportedReason); c != nil {
			run.Recorder.Eventf(p.obj, nil, corev1.EventTypeWarning, c.Reason, "CheckReferences", "%s", sentences(p.reported))
		}
	}
}

func refusalLines(refusals []*render.RefError) []string {
	out := make([]string, len(refusals))
	for i, r := range refusals {
		out[i] = r.Error()
	}
	return out
}

// secretsMissing records the referenced Secret keys that are missing or too
// large on the parts that reference them, and blocks the document. It
// reports false for an error that is not about the references.
func (run *configRun) secretsMissing(err error) bool {
	var findings []string
	reason := ""
	for _, e := range joined(err) {
		var key secretref.Key
		var r string
		var missing *secretsync.MissingError
		var tooLarge *secretsync.TooLargeError
		switch {
		case errors.As(e, &missing):
			key, r = missing.Key, crewletaiv1alpha1.ReasonSecretNotFound
		case errors.As(e, &tooLarge):
			key, r = tooLarge.Key, crewletaiv1alpha1.ReasonSecretTooLarge
		default:
			return false
		}
		if reason == "" {
			reason = r
		}
		findings = append(findings, e.Error())
		for _, p := range run.parts {
			if slices.ContainsFunc(p.refs, func(ref secretref.Key) bool { return references(ref, key) }) {
				if p.unresolvedReason == "" {
					p.unresolvedReason = r
				}
				p.unresolved = append(p.unresolved, e.Error())
			}
		}
	}
	message := sentences(findings)
	run.secretsSynced = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionSecretsSynced, Status: metav1.ConditionFalse, Reason: reason, Message: message,
	}
	run.block(reason, "The document is not applied until the Secrets it references can be read: "+message)
	return true
}

// joined returns the errors err joins, or err alone.
func joined(err error) []error {
	var j interface{ Unwrap() []error }
	if errors.As(err, &j) {
		return j.Unwrap()
	}
	return []error{err}
}

// invalid records the engine's refusal of the rendered document, each
// problem on the part that rendered its place in the document, and blocks
// the document. It reports false for an error that is not a refusal.
func (run *configRun) invalid(err error) bool {
	var verr *engine.ValidationError
	if !errors.As(err, &verr) {
		return false
	}
	for i := range verr.Problems {
		problem := &verr.Problems[i]
		// A problem of the Company's own is in the message below and on
		// no part.
		if target := run.problemPart(problem); target != nil {
			target.invalid = append(target.invalid, problem.String())
		}
	}
	run.block(crewletaiv1alpha1.ReasonInvalid, "The engine refused the rendered document: "+sentences(problemLines(verr)))
	run.event(corev1.EventTypeWarning, crewletaiv1alpha1.ReasonInvalid, "DryRunConfig",
		"The engine refused the rendered document; the revision it runs stays. %s", sentences(problemLines(verr)))
	return true
}

func problemLines(verr *engine.ValidationError) []string {
	out := make([]string, len(verr.Problems))
	for i, p := range verr.Problems {
		out[i] = p.String()
	}
	return out
}

// problemPart is the part an engine's problem is about: the one that rendered
// its place in the document, or else the seat or unit the engine names. Nil
// for a problem of the Company's own (or of its class's platform providers).
func (run *configRun) problemPart(problem *engine.Problem) *part {
	if obj, ok := run.doc.Attribute(render.PathOf(problem.Segments)); ok {
		return run.part(obj)
	}
	// The engine names the unit a problem is about by its name, the Unit's
	// displayName or, unset, its metadata.name. Crewlet holds no name
	// unique, so two Units may carry one, and the problem is then the Unit's
	// only when one does: blaming the first would report another Unit's
	// problem on it.
	var named []*part
	for _, p := range run.parts {
		switch o := p.obj.(type) {
		case *crewletaiv1alpha1.Seat:
			if problem.Seat != "" && render.SeatHandle(o) == problem.Seat {
				return p
			}
		case *crewletaiv1alpha1.Unit:
			if problem.Unit != "" && cmp.Or(o.Spec.DisplayName, o.Name) == problem.Unit {
				named = append(named, p)
			}
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return nil
}

// block records a document that cannot be applied: ConfigApplied False with
// reason, and the parts blocked.
func (run *configRun) block(reason, message string) {
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse, Reason: reason,
		Message: message + " The engine keeps running the revision it runs.",
	}
	var blockers []string
	for _, p := range run.parts {
		if p.hasProblem() {
			blockers = append(blockers, p.ref.String())
		}
	}
	why := "the Company itself"
	if len(blockers) > 0 {
		why = strings.Join(blockers, ", ")
	}
	run.applied = appliedVerdict{kind: appliedBlocked, message: fmt.Sprintf(
		"Not applied: the document is applied whole, and %s keeps it from being applied (see ConfigApplied on Company %s).",
		why, run.company.Name)}
}

// unavailable records an engine that could not be reached, or that failed a
// request for a reason of its own.
func (run *configRun) unavailable(err error) {
	run.configApplied = &metav1.Condition{
		Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionFalse,
		Reason:  crewletaiv1alpha1.ReasonEngineUnavailable,
		Message: fmt.Sprintf("The operator's request to the company's engine failed, and it tries again in %s: %s.", retryAfter(err), err),
	}
}

// retryAfter is how soon the reconcile asks a Company's engine again after
// err: as soon as the engine asked to be asked again (engine.ErrUnavailable
// with a Retry-After), and never later than engineRetryInterval. The engine
// asks for seconds, and waiting the full interval would leave the status
// that much longer behind it; a longer wait than the interval would hold
// back a status for whatever a header says.
func retryAfter(err error) time.Duration {
	var apiErr *engine.APIError
	if errors.Is(err, engine.ErrUnavailable) && errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return min(apiErr.RetryAfter, engineRetryInterval)
	}
	return engineRetryInterval
}

// notGranted records an engine that knows the operator's token and will not
// let it make a request the operator needs (engine.ErrForbidden), and reports
// false for any other error. No retry with the token changes that, so it is
// reported as what blocks the document rather than as an engine that is
// down.
//
// The operator renders its own token with admin, the one grant every route it
// calls needs, so a token that lacks it is an engine started from a
// crewlet.yaml the operator did not write. A token refused for the seat it is
// bound to is a binding an admin of the company made in the engine's
// identity directory, which only they can undo: the operator's token acts as
// no seat.
func (run *configRun) notGranted(err error) bool {
	var apiErr *engine.APIError
	if !errors.Is(err, engine.ErrForbidden) || !errors.As(err, &apiErr) {
		return false
	}
	var message string
	if apiErr.Code == "seat_unavailable" {
		message = fmt.Sprintf("The engine refuses the operator's token every request: its identity directory binds "+
			"the token, which it knows as %s, to a seat the company no longer has (%s). An admin of the company "+
			"unbinds that service account, in the engine's dashboard or with `crewlet iam unbind`; the operator's "+
			"token needs no seat.", engine.TokenLogin(render.TokenOperator), strings.TrimSuffix(apiErr.Message, "."))
	} else {
		needs := "no grant the engine named: what it refused is not a permission"
		if len(apiErr.Grants) > 0 {
			needs = "the grant " + strings.Join(apiErr.Grants, " or ")
		}
		message = fmt.Sprintf("The engine refuses the operator's token a request the operator needs (%s, %s): "+
			"the request needs %s. The crewlet.yaml the operator renders for the data pod (Secret %s) grants its "+
			"token, %q, admin, so the engine runs a crewlet.yaml the operator did not write: check that Secret and "+
			"that the data pod has restarted since it changed.",
			apiErr.Code, cmp.Or(apiErr.Reason, "no reason given"), needs, bootstrapSecretName(run.company), render.TokenOperator)
	}
	run.blockAndTell(crewletaiv1alpha1.ReasonNotGranted, "ApplyConfig", message)
	return true
}

// seatHeld records an engine that refuses the document because it takes a
// human seat out of the company, or makes it an agent's, while something in
// the engine's identity directory holds the seat (engine.SeatHeldError), and
// reports false for any other error. The document is valid and stays
// unapplied until an admin of the company frees each seat, which the
// operator neither can nor should do: a seat's holder is a person, or what a
// person set up. The message names each seat and what kind of thing holds
// it, never who: the engine names a holder by login, often an email address,
// which a status is no place for.
func (run *configRun) seatHeld(err error) bool {
	var held *engine.SeatHeldError
	if !errors.As(err, &held) {
		return false
	}
	var seats []string
	kinds := map[string]bool{}
	for _, handle := range slices.Sorted(maps.Keys(held.Held)) {
		holder := held.Held[handle]
		seats = append(seats, fmt.Sprintf("%s (held by %s)", handle, holder))
		kinds[holder.Kind] = true
	}
	// A Seat that is still there and is held is one the document makes an
	// agent's: the engine names only the seats the document takes away.
	for _, p := range run.parts {
		seat, ok := p.obj.(*crewletaiv1alpha1.Seat)
		if !ok {
			continue
		}
		if holder, ok := held.Held[render.SeatHandle(seat)]; ok {
			p.held = fmt.Sprintf("The engine's identity directory has %s on this seat, so the engine refuses a document "+
				"that makes it an agent's or leaves it out until an admin of the company frees it (see ConfigApplied on "+
				"Company %s).", holder, run.company.Name)
		}
	}
	// The remedy for each kind of holder present, and only those: a person
	// holds a human seat for as long as they are in the company, so they are
	// moved or removed, never unbound.
	var remedies []string
	if kinds[engine.HolderPerson] {
		remedies = append(remedies, "a person is moved to another human seat or removed")
	}
	if kinds[engine.HolderServiceAccount] {
		remedies = append(remedies, "a service account is unbound")
	}
	if kinds[engine.HolderInvitation] {
		remedies = append(remedies, "an open invitation is cancelled")
	}
	remedy := ""
	if len(remedies) > 0 {
		remedy = ": " + strings.Join(remedies, "; ")
	}
	message := fmt.Sprintf("The engine refuses the rendered document: it takes human seats out of the company, or makes "+
		"them agents', while its identity directory holds them: %s. An admin of the company frees each seat first, in "+
		"the engine's dashboard or with `crewlet iam` (`crewlet iam seats` lists what holds each seat)%s. Or keep "+
		"each seat as a Human Seat.", strings.Join(seats, "; "), remedy)
	run.blockAndTell(crewletaiv1alpha1.ReasonSeatHeld, "DryRunConfig", message)
	return true
}

// blockAndTell blocks the document for reason and records a Warning event
// with the same message when ConfigApplied did not already carry that
// reason, so that a refusal every retry meets again is told once rather than
// at every retry.
func (run *configRun) blockAndTell(reason, action, message string) {
	if current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied); current == nil ||
		current.Reason != reason {
		run.event(corev1.EventTypeWarning, reason, action, "%s", message)
	}
	run.block(reason, message)
}

// writeRefused records an engine that refuses the operator's token a change
// to the company document (engine.ErrConfigManaged), and reports false for any
// other error. The operator renders the engine's api.auth.company_writers
// itself and names its own token under Enforce, so this is an engine started
// from a crewlet.yaml the operator did not write, which no retry of the write
// changes: it is reported as that rather than as an engine that is down.
func (run *configRun) writeRefused(err error) bool {
	var apiErr *engine.APIError
	if !errors.Is(err, engine.ErrConfigManaged) || !errors.As(err, &apiErr) {
		return false
	}
	writers := "no token the engine named"
	if len(apiErr.ManagedBy) > 0 {
		writers = strings.Join(apiErr.ManagedBy, ", ")
	}
	// The engine names the writers and not the id it read the refused token
	// as, so the message says which id the operator renders for its token
	// rather than claiming the engine saw that one.
	message := fmt.Sprintf("The engine refuses the operator's token a change to the company document: its "+
		"api.auth.company_writers names %s. The crewlet.yaml the operator renders for the data pod (Secret %s) sets "+
		"that list only under Enforce, and then to its own token's id, %q, so the engine runs a crewlet.yaml the "+
		"operator did not write: check that Secret and that the data pod has restarted since it changed.",
		writers, bootstrapSecretName(run.company), render.TokenOperator)
	run.blockAndTell(crewletaiv1alpha1.ReasonWriteRefused, "ApplyConfig", message)
	return true
}

// part returns the part ref names, or nil.
func (run *configRun) part(ref render.ObjectRef) *part {
	for _, p := range run.parts {
		if p.ref == ref {
			return p
		}
	}
	return nil
}

// partAt returns the part that rendered the place p in the document, or nil
// when the Company (or its class) did.
func (run *configRun) partAt(p render.Path) *part {
	obj, ok := run.doc.Attribute(p)
	if !ok {
		return nil
	}
	return run.part(obj)
}

// placesOf lists the first few places of a diff, for a message.
func placesOf(diffs []render.Path) string {
	const shown = 5
	out := make([]string, 0, shown)
	for i, p := range diffs {
		if i == shown {
			break
		}
		if s := p.String(); s != "" {
			out = append(out, s)
		} else {
			out = append(out, "the whole document")
		}
	}
	if len(diffs) > shown {
		return fmt.Sprintf("%s and %d more places", strings.Join(out, ", "), len(diffs)-shown)
	}
	return strings.Join(out, ", ")
}

// summary is the X-Summary of a write: "crewlet-operator: " and the objects
// whose part of the document changed, or the Secrets whose rotated values
// the write makes the engine read.
func (run *configRun) summary(active *engine.ActiveConfig, diffs []render.Path, put []string) string {
	var objects []string
	add := func(s string) {
		if !slices.Contains(objects, s) {
			objects = append(objects, s)
		}
	}
	companyRef := render.ObjectRef{Kind: render.KindCompany, Name: run.company.Name}.String()
	if active == nil {
		add(companyRef)
		for _, s := range run.doc.Sources {
			add(s.Object.String())
		}
	} else {
		for _, p := range diffs {
			if obj, ok := run.doc.Attribute(p); ok {
				add(obj.String())
			} else {
				add(companyRef)
			}
		}
	}
	for _, k := range run.doc.Refs {
		if slices.Contains(put, secretsync.StoreName(k)) && active != nil && len(diffs) == 0 {
			add("Secret " + k.Namespace + "/" + k.Name)
		}
	}
	if len(objects) == 0 {
		// Only the record of what was applied is gone (a status write
		// that failed after the apply); the document is the same.
		add(companyRef)
	}
	const shown = 8
	text := strings.Join(objects[:min(len(objects), shown)], ", ")
	if len(objects) > shown {
		text += fmt.Sprintf(" and %d more", len(objects)-shown)
	}
	return "crewlet-operator: " + text
}

// objectCounts is status.config.objects for the rendered document.
func (run *configRun) objectCounts() crewletaiv1alpha1.AppliedObjectCounts {
	var units, seats, providers, servers, integrations, schedules int32
	for _, p := range run.parts {
		switch p.ref.Kind {
		case render.KindUnit:
			units++
		case render.KindSeat:
			seats++
		case render.KindLLMProvider:
			providers++
		case render.KindMCPServer:
			servers++
		case render.KindIntegration:
			integrations++
		case render.KindSchedule:
			schedules++
		}
	}
	if use := run.company.Spec.LLM.UsePlatformProviders; use != nil && *use {
		for range run.class.Spec.PlatformLLMProviders {
			providers++
		}
	}
	return crewletaiv1alpha1.AppliedObjectCounts{
		Units: new(units), Seats: new(seats), LLMProviders: new(providers), MCPServers: new(servers), Integrations: new(integrations),
		Schedules: new(schedules),
	}
}

// readHealth reads the engine's /health, for the EngineHealthy condition and
// for verifyApplied, and its verdict on each tool, for the Integrations'
// Healthy condition. A failure to read the tools leaves Healthy as it was:
// it is the engine's view, and the next look reads it again.
func (run *configRun) readHealth(ctx context.Context, eng *engine.Client) (engine.Health, error) {
	health, healthErr := eng.Health(ctx)
	if healthErr != nil {
		logf.FromContext(ctx).V(1).Info("Failed to read the engine's health", "error", healthErr.Error())
	}
	run.engineHealthy = engineHealthy(health.Alarms, healthErr)
	run.identityClaimed = identityClaimed(health, healthErr, run.humanSeats(ctx), apiAccessSecretName(run.company),
		dataStatefulSetName(run.company)+"-0")

	tools, err := eng.Integrations(ctx)
	if err != nil {
		logf.FromContext(ctx).V(1).Info("Failed to read the engine's integration status", "error", err.Error())
		return health, healthErr
	}
	run.health = make(map[string]engine.IntegrationStatus, len(tools))
	for _, t := range tools {
		run.health[t.Key] = t
	}
	return health, healthErr
}

// engineHealthy is the EngineHealthy condition for the alarms an engine
// reports, or for the error that kept it from reporting them.
func engineHealthy(alarms *engine.Alarms, err error) *metav1.Condition {
	c := &metav1.Condition{Type: crewletaiv1alpha1.ConditionEngineHealthy}
	switch {
	case err != nil:
		c.Status, c.Reason = metav1.ConditionUnknown, crewletaiv1alpha1.ReasonAlarmsNotReported
		c.Message = "The engine's health could not be read: " + err.Error()
	case alarms == nil:
		c.Status, c.Reason = metav1.ConditionUnknown, crewletaiv1alpha1.ReasonAlarmsNotReported
		c.Message = "The engine has not evaluated its alarms yet."
	case alarms.Count == 0:
		c.Status, c.Reason = metav1.ConditionTrue, crewletaiv1alpha1.ReasonNoAlarms
		c.Message = "The engine raises no alarm."
	default:
		c.Status, c.Reason = metav1.ConditionFalse, crewletaiv1alpha1.ReasonAlarms
		c.Message = fmt.Sprintf("The engine raises %d alarm(s); %s has stood longest.", alarms.Count, alarms.Worst)
		if alarms.Worst == alarmBackupAge || alarms.Worst == alarmTrimBlocked {
			c.Message += " The engine trims its logs only behind a recent verified backup, and refuses " +
				"ordinary writes once a log reaches its ceiling: take one with `crewlet backup` in the data pod " +
				"(docs/concepts/runtime.md, \"Backups\")."
		}
	}
	return c
}

// humanSeats are the handles of the human seats of the company the engine
// runs, which are the seats a person can join on: those of the rendered
// document when this reconcile activated it, else those of the document the
// engine ran when the reconcile read it, which under Observe and Seed may
// not be the rendered one, and under Enforce is not while a refusal holds
// the rendered one back.
func (run *configRun) humanSeats(ctx context.Context) []string {
	document := run.doc.YAML
	if run.active != nil && !run.wrote {
		document = run.active
	}
	seats, err := render.HumanSeatsOf(document)
	if err != nil {
		// The engine served it, and the render wrote the other: neither
		// fails to parse. Said rather than hidden, and read as no seat.
		logf.FromContext(ctx).Error(err, "Failed to read the human seats of the company document")
	}
	return seats
}

// maxNamedSeats is how many of a company's Human seats IdentityClaimed names:
// enough to pick one from, and a bound on a message a company of hundreds of
// people would otherwise fill.
const maxNamedSeats = 5

// identityPollInterval is how soon the config reconciler looks at a
// Company's engine again while nobody has joined it (IdentityClaimed=False
// Unclaimed). The first person joins in the engine's dashboard or with
// `crewlet iam`, which nothing the operator watches announces, so the drift
// check's five minutes would leave the Company saying nobody has joined for
// that long after somebody has, while the person who just joined waits on
// it. Thirty seconds keeps that wait inside a minute and costs a company
// nobody has joined yet the few reads of one look twice a minute; once
// somebody has, the drift check is enough, and a company with no Human seat
// changes only through its objects, which the reconciler watches.
const identityPollInterval = 30 * time.Second

// identityResync looks again soon while nobody has joined the company.
func (run *configRun) identityResync(result ctrl.Result) ctrl.Result {
	if c := run.identityClaimed; c != nil && c.Reason == crewletaiv1alpha1.ReasonUnclaimed &&
		(result.RequeueAfter == 0 || result.RequeueAfter > identityPollInterval) {
		result.RequeueAfter = identityPollInterval
	}
	return result
}

// identityClaimed is the IdentityClaimed condition for what an engine's
// /health says about its identity directory, or for the error that kept it
// from saying. humanSeats are the handles of the company's human seats, and
// accessSecret the Secret that holds the access token the operator publishes,
// with which the first person is brought in, and dataPod the pod `crewlet
// iam` runs in: it reads the node to reach from a crewlet.yaml, which only
// the engine's pods hold.
//
// A company that has never had anybody join is the state of every new
// company, and nothing else tells whoever created it that the next step is
// theirs: the engine signs nobody in until a person is invited or added,
// and only a holder of an admin credential can do that.
func identityClaimed(health engine.Health, err error, humanSeats []string, accessSecret, dataPod string) *metav1.Condition {
	c := &metav1.Condition{Type: crewletaiv1alpha1.ConditionIdentityClaimed}
	switch {
	case err != nil:
		c.Status, c.Reason = metav1.ConditionUnknown, crewletaiv1alpha1.ReasonIdentityNotReported
		c.Message = "The engine's health could not be read: " + err.Error()
	case health.Identity == engine.IdentityReady:
		c.Status, c.Reason = metav1.ConditionTrue, crewletaiv1alpha1.ReasonPersonEnrolled
		c.Message = "A person has joined the company: the engine's identity directory holds someone on a Human seat, " +
			"who signs in to its dashboard as themselves once they have chosen a password."
	case health.Identity == engine.IdentityUnclaimed && len(humanSeats) == 0:
		c.Status, c.Reason = metav1.ConditionFalse, crewletaiv1alpha1.ReasonNoHumanSeat
		c.Message = "Nobody has joined the company, and nobody can: it has no Human seat to put a person on. " +
			"Add a Seat of kind Human for each person who signs in to the engine's dashboard."
	case health.Identity == engine.IdentityUnclaimed:
		c.Status, c.Reason = metav1.ConditionFalse, crewletaiv1alpha1.ReasonUnclaimed
		named := humanSeats[:min(len(humanSeats), maxNamedSeats)]
		seats := strings.Join(named, ", ")
		if more := len(humanSeats) - len(named); more > 0 {
			seats += fmt.Sprintf(" and %d more", more)
		}
		c.Message = fmt.Sprintf("Nobody has joined the company yet. Its Human seats are %s. Bring the first person in "+
			"with the access token in Secret %s (status.apiAccessSecretRef): sign in to the dashboard with it and "+
			"invite or add yourself onto your seat, or run `crewlet iam invite <email> -seat <handle> -grants admin "+
			"-config %s/%s -api http://127.0.0.1:%d` in pod %s with it in CREWLET_API_TOKEN.",
			seats, accessSecret, configDir, configFile, render.APIPort, dataPod)
	case health.Identity == "":
		c.Status, c.Reason = metav1.ConditionUnknown, crewletaiv1alpha1.ReasonIdentityNotReported
		c.Message = "The engine did not say whether anybody has joined the company."
	default:
		c.Status, c.Reason = metav1.ConditionUnknown, crewletaiv1alpha1.ReasonIdentityNotReported
		c.Message = fmt.Sprintf("The engine could not say whether anybody has joined the company (%s): it could not "+
			"read its identity directory, and a directory it cannot read is not one nobody is in.", health.Identity)
	}
	return c
}

// The engine's alarms about backups, which every operator-run company
// raises until a backup is taken.
const (
	alarmBackupAge   = "backup_age"
	alarmTrimBlocked = "trim_blocked"
)

// event records an event on the Company. Its note never carries a Secret
// value: every caller passes names, revisions and the engine's own words,
// which never repeat a value.
func (run *configRun) event(eventType, reason, action, note string, args ...any) {
	run.Recorder.Eventf(run.company, nil, eventType, reason, action, note, args...)
}

// The most pins status.config.placement holds of each kind: the MaxItems of
// RevisionPlacement's lists.
const (
	maxPlacedPools = 64
	maxPlacedSeats = 1024
)

// The values a pin must have to name one of a company's pods: a seat pool's
// name, and a seat's handle, as the API admits them.
var (
	poolNamePattern   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	seatHandlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// revisionPlacement reads where a company document pins its seats, as
// status.config.placement records it: only a value that can name one of the
// company's pods, and at most as many as the status holds.
func revisionPlacement(document []byte) (crewletaiv1alpha1.RevisionPlacement, error) {
	p, err := render.PlacementOf(document)
	if err != nil {
		return crewletaiv1alpha1.RevisionPlacement{}, err
	}
	return boundedPlacement(p.Pools, p.Seats), nil
}

// boundedPlacement keeps the pools and seats that can name a company's pods,
// sorted, the first in that order where there are more than the status
// holds. Under Enforce no document pins more: a company's pools are at most
// eight, and a seat beyond the thousandth in a PerSeat pool would be the
// thousandth pod. Under Observe or Seed the engine's own document may say
// anything; a pod pinned only by a seat beyond the bound is then kept for as
// long as the Company places the seat in it.
func boundedPlacement(pools, seats []string) crewletaiv1alpha1.RevisionPlacement {
	keep := func(values []string, pattern *regexp.Regexp, maxLength, maxItems int) []string {
		var out []string
		for _, v := range values {
			if len(v) <= maxLength && pattern.MatchString(v) {
				out = append(out, v)
			}
		}
		slices.Sort(out)
		out = slices.Compact(out)
		return out[:min(len(out), maxItems)]
	}
	placement := crewletaiv1alpha1.RevisionPlacement{
		Pools: keep(pools, poolNamePattern, 20, maxPlacedPools),
		Seats: keep(seats, seatHandlePattern, 63, maxPlacedSeats),
	}
	if len(placement.Pools) == 0 {
		placement.Pools = nil
	}
	if len(placement.Seats) == 0 {
		placement.Seats = nil
	}
	return placement
}

// recordPlacement records where the document the engine runs pins its seats.
//
// A revision the operator activates, or finds activated, runs only once the
// engine has applied it, and the engine keeps running the previous one if
// the apply fails. Until it is applied (exact is false) the record is
// therefore the union of what it already holds and the new document's pins,
// so that no pod the previous revision still needs goes; settled and
// narrowPlacement narrow it to the document alone (exact) once the engine
// reports it applied. The union is taken when the status is written, against
// the record as that write finds it, so that a pin an earlier write recorded
// and the cached Company does not show yet is kept.
func (run *configRun) recordPlacement(placement crewletaiv1alpha1.RevisionPlacement, exact bool) {
	if !exact {
		running := placement
		run.running = &running
	}
	run.placement = &placementUpdate{pins: placement, exact: exact}
}

// placementUpdate is a change to status.config.placement: pins that replace
// the record (exact) or join it.
type placementUpdate struct {
	pins  crewletaiv1alpha1.RevisionPlacement
	exact bool
}

// of is the record recorded becomes.
func (u *placementUpdate) of(recorded crewletaiv1alpha1.RevisionPlacement) crewletaiv1alpha1.RevisionPlacement {
	if u.exact {
		return boundedPlacement(u.pins.Pools, u.pins.Seats)
	}
	return boundedPlacement(slices.Concat(recorded.Pools, u.pins.Pools), slices.Concat(recorded.Seats, u.pins.Seats))
}

// widenPlacement adds pins to the Company's status.config.placement, as the
// API server holds it, ahead of the write that activates the revision that
// pins them.
func (run *configRun) widenPlacement(ctx context.Context, pins crewletaiv1alpha1.RevisionPlacement) error {
	update := placementUpdate{pins: pins}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &crewletaiv1alpha1.Company{}
		if err := run.APIReader.Get(ctx, client.ObjectKeyFromObject(run.company), fresh); err != nil {
			return err
		}
		widened := update.of(fresh.Status.Config.Placement)
		if equality.Semantic.DeepEqual(widened, fresh.Status.Config.Placement) {
			return nil
		}
		base := fresh.DeepCopy()
		fresh.Status.Config.Placement = widened
		if err := run.Status().Patch(ctx, fresh, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) {
				return err
			}
			return fmt.Errorf("recording where the next revision of Company %s/%s pins its seats: %w",
				run.company.Namespace, run.company.Name, err)
		}
		return nil
	})
}

// narrowPlacement narrows the record to the pins of the engine's own document
// (Observe, Seed) once the engine has applied that document: its record of
// its last apply names the document's revision, ended well, and is no later
// than the epoch it runs. Until then the engine may still be running an
// earlier revision, whose pins the record keeps.
func (run *configRun) narrowPlacement(ctx context.Context, eng *engine.Client, health engine.Health, healthErr error) {
	if run.running == nil || run.runningRevision == "" || healthErr != nil {
		return
	}
	fleet, err := run.fleetOf(ctx, eng)
	if err != nil {
		return
	}
	record := fleet.LastApply()
	if record.RevisionID == run.runningRevision && record.Status == engine.ApplyOK && health.AppliedEpoch >= record.Epoch {
		run.recordPlacement(*run.running, true)
	}
}
