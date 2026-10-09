package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// configRun is one reconcile of one Company's document: what it read, and
// the verdict it reached, which writeStatus records.
type configRun struct {
	*CompanyConfigReconciler

	company *crewletaiv1alpha1.Company
	class   *crewletaiv1alpha1.EngineClass
	parts   []*part
	doc     *render.Document
	// desired is the store entries the document calls for, once read.
	desired *secretsync.Desired

	// The Company's conditions this reconcile decided, nil where it left
	// one as it was, and status.config when it changed it.
	secretsSynced *metav1.Condition
	configApplied *metav1.Condition
	engineHealthy *metav1.Condition
	config        *crewletaiv1alpha1.ConfigStatus
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
// the engine. An error is one to retry with backoff: the API server did not
// answer. Everything about the objects, and an engine that cannot be reached,
// is a condition.
func (run *configRun) reconcile(ctx context.Context) (ctrl.Result, error) {
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
	if err != nil {
		refusals := render.RefErrors(err)
		if len(refusals) == 0 {
			return ctrl.Result{}, fmt.Errorf("rendering the document of Company %s/%s: %w", c.Namespace, c.Name, err)
		}
		run.refused(refusals)
		return ctrl.Result{}, nil
	}
	run.doc = doc

	// Read before the engine is reached, so that a missing Secret is
	// reported while the engine is still starting. Under Seed the values
	// are read only for the one write into an empty engine: once the engine
	// owns the document, the Secrets are no longer the operator's concern.
	// Under Observe nothing is pushed, so they are never read.
	if run.policy() == crewletaiv1alpha1.ManagementPolicyEnforce {
		if blocked, err := run.resolveSecrets(ctx); blocked || err != nil {
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
	result, err := run.converge(ctx, eng)
	if err != nil {
		if isAPIServerError(err) {
			return ctrl.Result{}, err
		}
		if !run.writeRefused(err) {
			run.unavailable(err)
		}
		return ctrl.Result{RequeueAfter: engineRetryInterval}, nil
	}
	health, healthErr := run.readHealth(ctx, eng)
	return run.verifyApplied(ctx, eng, health, healthErr, result), nil
}

// resolveSecrets reads the values of every Secret key the document
// references into run.desired. blocked reports keys that are missing or too
// large, which it records on the parts that reference them.
func (run *configRun) resolveSecrets(ctx context.Context) (blocked bool, err error) {
	desired, err := secretsync.Resolve(ctx, run.APIReader, run.doc.Refs)
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
		Company:       run.company,
		Class:         run.class,
		StoreName:     secretsync.StoreName,
		PublicBaseURL: run.company.Status.Endpoints.PublicBaseURL,
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
		var result ctrl.Result
		switch policy := run.policy(); {
		case policy == crewletaiv1alpha1.ManagementPolicyObserve:
			return run.observe(ctx, eng, active)
		case policy == crewletaiv1alpha1.ManagementPolicySeed && active != nil:
			run.seeded(active)
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
		pushing.Keep(storeNamesIn(active.Document)...)
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
	if active != nil && len(diffs) == 0 && hash == recorded.AppliedHash {
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
	next, result, syncErr := secretsync.Sync(ctx, eng, desired, state)
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
		run.configApplied = &metav1.Condition{
			Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonInSync,
			Message: fmt.Sprintf("The engine runs the rendered document as revision %s.", active.RevisionID),
		}
		run.applied = appliedVerdict{kind: appliedObserved, revision: active.RevisionID}
		return ctrl.Result{RequeueAfter: run.resyncInterval()}, nil
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
func (run *configRun) seeded(active *engine.ActiveConfig) {
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
		default:
			if target.unresolvedReason == "" {
				target.unresolvedReason = ref.Reason
			}
			target.unresolved = append(target.unresolved, finding)
		}
	}
	run.block(refusals[0].Reason, "The document cannot be rendered: "+sentences(refusalLines(refusals)))
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
	for _, p := range run.parts {
		switch o := p.obj.(type) {
		case *crewletaiv1alpha1.Seat:
			if problem.Seat != "" && render.SeatHandle(o) == problem.Seat {
				return p
			}
		case *crewletaiv1alpha1.Unit:
			if problem.Unit != "" && (o.Name == problem.Unit || o.Spec.DisplayName == problem.Unit) {
				return p
			}
		}
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
		Message: fmt.Sprintf("The operator's request to the company's engine failed, and it tries again in %s: %s.", engineRetryInterval, err),
	}
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
	message := fmt.Sprintf("The engine refuses the operator's token (id %q) a change to the company document: its "+
		"api.auth.company_writers names %s. The operator renders that list into the data pod's crewlet.yaml (Secret %s), "+
		"naming its own token under Enforce, so the engine runs a crewlet.yaml the operator did not write: check that "+
		"Secret and that the data pod has restarted since it changed.", render.TokenOperator, writers, bootstrapSecretName(run.company))
	if current := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied); current == nil ||
		current.Reason != crewletaiv1alpha1.ReasonWriteRefused {
		run.event(corev1.EventTypeWarning, crewletaiv1alpha1.ReasonWriteRefused, "ApplyConfig", "%s", message)
	}
	run.block(crewletaiv1alpha1.ReasonWriteRefused, message)
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
	var units, seats, providers, servers, integrations int32
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
		}
	}
	if use := run.company.Spec.LLM.UsePlatformProviders; use != nil && *use {
		for range run.class.Spec.PlatformLLMProviders {
			providers++
		}
	}
	return crewletaiv1alpha1.AppliedObjectCounts{
		Units: new(units), Seats: new(seats), LLMProviders: new(providers), MCPServers: new(servers), Integrations: new(integrations),
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
