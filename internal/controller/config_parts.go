package controller

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// part is one part of a company (a Unit, Seat, LLMProvider, MCPServer,
// Integration or Schedule) in one reconcile: the object as read, and what the reconcile
// found wrong with it. Its status is computed from those findings and written
// at the end of the reconcile.
type part struct {
	ref  render.ObjectRef
	obj  client.Object
	base client.Object
	refs []secretref.Key

	// conflicts are why the part is not Accepted.
	conflicts []string
	// handleTaken is a Seat whose handle another Seat derives too, which
	// therefore has no identity of its own in the engine.
	handleTaken bool
	// unresolved are why its references do not resolve, with the reason
	// of the first.
	unresolved       []string
	unresolvedReason string
	// unplaceable are why a Seat cannot run in the pool it is placed in,
	// which its class does not let the company run it in, with the reason
	// of the first. Its references resolve; the document is refused all
	// the same.
	unplaceable       []string
	unplaceableReason string
	// unrunnable are why a Seat cannot run on the pods of the pool it is
	// placed in, which its class admits, because the pods cannot serve what
	// it runs on, or why its code work runs on a CLIAgent provider of
	// another CLI. Its references resolve; the document is refused all the
	// same.
	unrunnable       []string
	unrunnableReason string
	// invalid are the engine's refusals of its part of the document.
	invalid []string
	// drifted are the places its part of the document differs from what
	// the engine runs (Observe).
	drifted []string
	// moving says why a Seat stays pinned where the revision the engine
	// runs pins it, while the pods of the pool it moves to start (see
	// holdMoves); empty when it is pinned where it is placed.
	moving string
	// pause is what the reconcile found about a Seat's pause, nil when it
	// did not read it (see syncPauses); pauseGone is a Seat the engine runs
	// no seat of, whose Paused condition goes.
	pause     *pauseVerdict
	pauseGone bool
	// codeWork is what the render found about a Seat's code work, nil when
	// Crewlet offers the seat none or none of its sandbox chain resolves;
	// read only once the reconcile rendered (objectsChecked).
	codeWork *render.CodeWork
}

func newPart(obj client.Object) *part {
	return &part{
		ref:  render.ObjectRef{Kind: kindOf(obj), Name: obj.GetName()},
		obj:  obj,
		base: obj.DeepCopyObject().(client.Object),
		refs: secretRefsOf(obj),
	}
}

// kindOf is the kind of a part, as render names it.
func kindOf(obj client.Object) string {
	switch obj.(type) {
	case *crewletaiv1alpha1.Unit:
		return render.KindUnit
	case *crewletaiv1alpha1.Seat:
		return render.KindSeat
	case *crewletaiv1alpha1.LLMProvider:
		return render.KindLLMProvider
	case *crewletaiv1alpha1.MCPServer:
		return render.KindMCPServer
	case *crewletaiv1alpha1.Schedule:
		return render.KindSchedule
	default:
		return render.KindIntegration
	}
}

// conditions returns the part's status conditions, to be set in place.
func (p *part) conditions() *[]metav1.Condition {
	switch o := p.obj.(type) {
	case *crewletaiv1alpha1.Unit:
		return &o.Status.Conditions
	case *crewletaiv1alpha1.Seat:
		return &o.Status.Conditions
	case *crewletaiv1alpha1.LLMProvider:
		return &o.Status.Conditions
	case *crewletaiv1alpha1.MCPServer:
		return &o.Status.Conditions
	case *crewletaiv1alpha1.Integration:
		return &o.Status.Conditions
	case *crewletaiv1alpha1.Schedule:
		return &o.Status.Conditions
	}
	return nil
}

func (p *part) setObservedGeneration() {
	gen := p.obj.GetGeneration()
	switch o := p.obj.(type) {
	case *crewletaiv1alpha1.Unit:
		o.Status.ObservedGeneration = gen
	case *crewletaiv1alpha1.Seat:
		o.Status.ObservedGeneration = gen
	case *crewletaiv1alpha1.LLMProvider:
		o.Status.ObservedGeneration = gen
	case *crewletaiv1alpha1.MCPServer:
		o.Status.ObservedGeneration = gen
	case *crewletaiv1alpha1.Integration:
		o.Status.ObservedGeneration = gen
	case *crewletaiv1alpha1.Schedule:
		o.Status.ObservedGeneration = gen
	}
}

// statusOf returns the status of a part, for comparison.
func statusOf(obj client.Object) any {
	switch o := obj.(type) {
	case *crewletaiv1alpha1.Unit:
		return o.Status
	case *crewletaiv1alpha1.Seat:
		return o.Status
	case *crewletaiv1alpha1.LLMProvider:
		return o.Status
	case *crewletaiv1alpha1.MCPServer:
		return o.Status
	case *crewletaiv1alpha1.Integration:
		return o.Status
	case *crewletaiv1alpha1.Schedule:
		return o.Status
	}
	return nil
}

// hasProblem reports whether the part itself keeps the document from being
// applied, as opposed to being blocked by another one.
func (p *part) hasProblem() bool {
	return len(p.conflicts) > 0 || len(p.unresolved) > 0 || len(p.unplaceable) > 0 || len(p.unrunnable) > 0 ||
		len(p.invalid) > 0
}

// inRevision reports whether the part's current generation is in the
// revision the engine runs: its Applied condition is True and describes
// this generation.
func (p *part) inRevision() bool {
	applied := meta.FindStatusCondition(*p.conditions(), crewletaiv1alpha1.ConditionApplied)
	return applied != nil && applied.Status == metav1.ConditionTrue && applied.ObservedGeneration == p.obj.GetGeneration()
}

// patchPartStatus writes a part's status when the reconcile changed it. The
// reconciler is the only writer of a part's status, so a plain merge patch
// cannot lose anyone else's write.
func (r *CompanyConfigReconciler) patchPartStatus(ctx context.Context, p *part) error {
	if equality.Semantic.DeepEqual(statusOf(p.base), statusOf(p.obj)) {
		return nil
	}
	if err := r.Status().Patch(ctx, p.obj, client.MergeFrom(p.base)); err != nil {
		return fmt.Errorf("updating the status of %s %s/%s: %w", p.ref.Kind, p.obj.GetNamespace(), p.obj.GetName(), err)
	}
	return nil
}

// setCondition sets a condition of the given type, keeping its last
// transition time when its status does not change.
func setCondition(conditions *[]metav1.Condition, generation int64, conditionType string, status bool, reason, message string) {
	s := metav1.ConditionFalse
	if status {
		s = metav1.ConditionTrue
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type: conditionType, Status: s, Reason: reason, Message: bounded(message), ObservedGeneration: generation,
	})
}

// maxConditionMessage bounds a condition's message. The API allows 32 KiB;
// a message is for a person reading `kubectl describe`, and an engine's
// refusal of a large document can list hundreds of problems.
const maxConditionMessage = 4096

// bounded cuts a message to maxConditionMessage bytes, at a rune boundary.
func bounded(message string) string {
	if len(message) <= maxConditionMessage {
		return message
	}
	const ellipsis = " …"
	cut := maxConditionMessage - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + ellipsis
}

// sentences joins findings into one message, each ending with a period.
func sentences(findings []string) string {
	out := make([]string, len(findings))
	for i, f := range findings {
		f = capitalize(strings.TrimSpace(f))
		if !strings.HasSuffix(f, ".") {
			f += "."
		}
		out[i] = f
	}
	return strings.Join(out, " ")
}
