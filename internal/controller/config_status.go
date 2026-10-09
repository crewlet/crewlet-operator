package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// The engine's keys of the tools an Integration connects.
const (
	toolSlack  = "slack"
	toolGitHub = "github"
)

// writeStatus records the reconcile's verdict: every part's status, then the
// Company's conditions and status.config.
func (run *configRun) writeStatus(ctx context.Context) error {
	vendors := run.vendors()
	units := unitTree(run.parts)
	var errs []error
	for _, p := range run.parts {
		run.decide(p, vendors, units)
		if err := run.patchPartStatus(ctx, p); err != nil {
			errs = append(errs, err)
		}
	}
	if err := run.patchCompanyStatus(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// decide sets a part's status from what the reconcile found.
func (run *configRun) decide(p *part, vendors map[string]bool, units map[string]unitFacts) {
	gen := p.obj.GetGeneration()
	conditions := p.conditions()

	companyAccepted := meta.FindStatusCondition(run.company.Status.Conditions, crewletaiv1alpha1.ConditionAccepted)
	switch {
	case companyAccepted == nil || companyAccepted.Status != metav1.ConditionTrue:
		// Nothing of the company is applied while the Company itself is
		// refused (its class is gone, it is over its limits), and a part
		// reported as accepted would say otherwise.
		message := fmt.Sprintf("Company %s has not been accepted yet.", run.company.Name)
		if companyAccepted != nil {
			message = fmt.Sprintf("Company %s is not accepted (%s): %s", run.company.Name, companyAccepted.Reason, companyAccepted.Message)
		}
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionAccepted, false, crewletaiv1alpha1.ReasonCompanyNotAccepted, message)
	case len(p.conflicts) > 0:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionAccepted, false, crewletaiv1alpha1.ReasonConflict, sentences(p.conflicts))
	default:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionAccepted, true, crewletaiv1alpha1.ReasonValid,
			fmt.Sprintf("Part of Company %s.", run.company.Name))
	}
	switch {
	case len(p.unresolved) > 0:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionResolvedRefs, false, p.unresolvedReason, sentences(p.unresolved))
	case run.secretsChecked:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionResolvedRefs, true, crewletaiv1alpha1.ReasonResolved,
			"Every object and Secret key it references exists.")
	case run.objectsChecked:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionResolvedRefs, true, crewletaiv1alpha1.ReasonResolved,
			"Every object it references exists.")
	}
	run.decideApplied(p)

	base := run.company.Status.Endpoints.PublicBaseURL
	switch o := p.obj.(type) {
	case *crewletaiv1alpha1.Seat:
		handle := render.SeatHandle(o)
		o.Status.Handle = handle
		o.Status.SeatID = ""
		o.Status.Placement = crewletaiv1alpha1.SeatPlacementStatus{}
		o.Status.Integrations = crewletaiv1alpha1.SeatIntegrationsStatus{}
		// Crewlet derives an agent id for an Agent seat only (a Human seat
		// has none, not a zero one), and only an Agent seat may have
		// vendor apps of its own. A handle two Seats derive is neither
		// seat's identity.
		if o.Spec.Kind == crewletaiv1alpha1.SeatKindHuman || p.handleTaken {
			break
		}
		o.Status.SeatID = render.SeatID(run.company.Spec.CompanyName, handle)
		o.Status.Placement.Pool = render.DataPool
		if o.Spec.Placement != nil && o.Spec.Placement.Pool != "" {
			o.Status.Placement.Pool = o.Spec.Placement.Pool
		}
		if base != "" && vendors[toolSlack] {
			o.Status.Integrations.Slack.RequestURL = base + "/webhooks/slack/" + handle
		}
		if base != "" && vendors[toolGitHub] {
			o.Status.Integrations.GitHub.WebhookURL = base + "/webhooks/github/" + handle
		}
	case *crewletaiv1alpha1.Unit:
		facts := units[o.Name]
		o.Status.Depth = facts.depth
		o.Status.Seats = new(facts.seats)
	case *crewletaiv1alpha1.Integration:
		vendor := vendorOf(o)
		o.Status.WebhookURL = ""
		if base != "" && vendor == toolGitHub {
			// Slack has no company-wide address: each seat's app
			// delivers to its own, in the Seat's status.
			o.Status.WebhookURL = base + "/webhooks/github"
		}
		run.decideHealthy(p, vendor, base)
	}
	p.setObservedGeneration()
}

// decideApplied sets a part's Applied condition from the reconcile's verdict.
func (run *configRun) decideApplied(p *part) {
	gen := p.obj.GetGeneration()
	conditions := p.conditions()
	verdict := run.applied
	switch verdict.kind {
	case appliedUnknown:
	case appliedInRevision:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, true, crewletaiv1alpha1.ReasonInRevision,
			fmt.Sprintf("In revision %s, which the engine runs.", verdict.revision))
	case appliedBlocked:
		switch {
		case len(p.conflicts) > 0:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonConflict, sentences(p.conflicts))
		case len(p.unresolved) > 0:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, p.unresolvedReason, sentences(p.unresolved))
		case len(p.invalid) > 0:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonInvalid, sentences(p.invalid))
		case p.inRevision():
			// This generation runs already; only what changed elsewhere
			// is held back.
		default:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonBlocked, verdict.message)
		}
	case appliedObserved:
		switch {
		case len(p.drifted) > 0:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonDrifted,
				"The engine's document differs from this object's part of the rendered one at: "+strings.Join(p.drifted, ", ")+".")
		default:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, true, crewletaiv1alpha1.ReasonInRevision,
				fmt.Sprintf("In revision %s, which the engine runs.", verdict.revision))
		}
	case appliedNotManaged:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonNotManaged, verdict.message)
	case appliedPending:
		if !p.inRevision() {
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonApplying,
				fmt.Sprintf("In revision %s, which the engine is applying.", verdict.revision))
		}
	case appliedFailed:
		if !p.inRevision() {
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionApplied, false, crewletaiv1alpha1.ReasonApplyFailed, verdict.message)
		}
	}
}

// decideHealthy sets an Integration's Healthy condition from the engine's
// verdict on its tool. With no public address no webhook can arrive, which
// is the verdict whatever the engine says; without a verdict read, the
// condition stays as it was.
func (run *configRun) decideHealthy(p *part, vendor, base string) {
	gen := p.obj.GetGeneration()
	conditions := p.conditions()
	if base == "" {
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionHealthy, false, crewletaiv1alpha1.ReasonNotExposed, fmt.Sprintf(
			"Company %s has no public address (status.endpoints.publicBaseURL), so %s cannot deliver webhooks to its engine.",
			run.company.Name, vendor))
		return
	}
	if run.health == nil {
		return
	}
	tool, ok := run.health[vendor]
	switch {
	case !ok:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionHealthy, false, crewletaiv1alpha1.ReasonCheckFailed,
			fmt.Sprintf("The engine reports nothing about %s.", vendor))
	case tool.State == engine.IntegrationConnected:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionHealthy, true, crewletaiv1alpha1.ReasonCheckPassed,
			fmt.Sprintf("The engine reports %s %s.", vendor, tool.Label))
	case tool.State == engine.IntegrationNotInUse:
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionHealthy, false, crewletaiv1alpha1.ReasonCheckFailed,
			fmt.Sprintf("The engine does not use %s: the revision it runs has no %s block.", vendor, vendor))
	default:
		// attention, not_connected, and any state a newer engine adds,
		// which the engine's client says to read as not working.
		message := fmt.Sprintf("The engine reports %s %s.", vendor, tool.Label)
		if tool.Reason != "" {
			message = fmt.Sprintf("The engine reports %s %s: %s", vendor, tool.Label, tool.Reason)
		}
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionHealthy, false, crewletaiv1alpha1.ReasonCheckFailed, message)
	}
}

// vendors are the tools the company's Integrations connect.
func (run *configRun) vendors() map[string]bool {
	out := map[string]bool{}
	for _, p := range run.parts {
		if in, ok := p.obj.(*crewletaiv1alpha1.Integration); ok {
			out[vendorOf(in)] = true
		}
	}
	return out
}

// vendorOf is the engine's key of the tool an Integration connects.
func vendorOf(in *crewletaiv1alpha1.Integration) string {
	if in.Spec.Slack != nil {
		return toolSlack
	}
	return toolGitHub
}

// unitFacts is what a Unit's status says about its place in the chart.
type unitFacts struct {
	// depth is nil for a Unit that is not in the chart: its parent chain
	// leads to a Unit that does not exist, or loops.
	depth *int32
	seats int32
}

// unitTree computes each Unit's depth and its number of member Seats.
func unitTree(parts []*part) map[string]unitFacts {
	units := map[string]*crewletaiv1alpha1.Unit{}
	facts := map[string]unitFacts{}
	for _, p := range parts {
		if u, ok := p.obj.(*crewletaiv1alpha1.Unit); ok {
			units[u.Name] = u
		}
	}
	for _, p := range parts {
		if s, ok := p.obj.(*crewletaiv1alpha1.Seat); ok && s.Spec.UnitRef.Name != "" {
			f := facts[s.Spec.UnitRef.Name]
			f.seats++
			facts[s.Spec.UnitRef.Name] = f
		}
	}
	for name, u := range units {
		var depth int32
		seen := map[string]bool{name: true}
		for u != nil && u.Spec.ParentRef.Name != "" {
			parent := u.Spec.ParentRef.Name
			if seen[parent] {
				u = nil
				break
			}
			seen[parent] = true
			u = units[parent]
			depth++
		}
		f := facts[name]
		if u != nil {
			f.depth = new(depth)
		}
		facts[name] = f
	}
	return facts
}

// patchCompanyStatus writes the Company's conditions and status.config this
// reconcile decided. A conflict means someone wrote the Company since it was
// read: the runtime reconciler's status, or a spec change. The Company is
// read again from the API server and the same decisions are written onto it,
// which is safe because the two reconcilers never write the same fields.
// They are stamped with the generation this reconcile read and rendered, not
// the fresh object's: after a spec change the document of the new generation
// has not been rendered, and claiming it was would let Ready report it
// applied. The generation change queues the Company again.
func (run *configRun) patchCompanyStatus(ctx context.Context) error {
	if run.secretsSynced == nil && run.configApplied == nil && run.engineHealthy == nil && run.config == nil {
		return nil
	}
	company := run.company
	generation := company.Generation
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		base := company.DeepCopy()
		for _, c := range []*metav1.Condition{run.secretsSynced, run.configApplied, run.engineHealthy} {
			if c == nil {
				continue
			}
			condition := *c
			condition.ObservedGeneration = generation
			condition.Message = bounded(condition.Message)
			meta.SetStatusCondition(&company.Status.Conditions, condition)
		}
		if run.config != nil {
			company.Status.Config = *run.config
		}
		if equality.Semantic.DeepEqual(base.Status, company.Status) {
			return nil
		}
		err := run.Status().Patch(ctx, company, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		if apierrors.IsConflict(err) {
			fresh := &crewletaiv1alpha1.Company{}
			if getErr := run.APIReader.Get(ctx, client.ObjectKeyFromObject(company), fresh); getErr != nil {
				return getErr
			}
			company = fresh
		}
		if err != nil {
			return fmt.Errorf("updating the status of Company %s/%s: %w", company.Namespace, company.Name, err)
		}
		return nil
	})
}
