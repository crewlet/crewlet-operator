package controller

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// summarize returns a Company's Ready condition, without its generation:
// True when it is Accepted, RuntimeReady, Exposed and ConfigApplied. When it
// is not, its reason is the reason of the first of those that is not True, so
// a health check that reads Ready alone still learns why; a suspended Company
// says Suspended, which is the one cause a user chose.
//
// ConfigApplied is the config reconciler's. Until that reconciler has
// reported it for the Company's current generation, the Company is not
// Ready: an engine without its document runs nothing, and one that runs an
// older document has not taken the change.
func summarize(c *crewletaiv1alpha1.Company) metav1.Condition {
	ready := metav1.Condition{Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionFalse}
	if suspended(c) {
		// Only RuntimeReady=Suspended for this generation says the
		// StatefulSet was scaled to zero; a suspension the operator has not
		// carried out yet must not read as one it has.
		ready.Reason = crewletaiv1alpha1.ReasonSuspended
		runtime := meta.FindStatusCondition(c.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady)
		if runtime != nil && runtime.Reason == crewletaiv1alpha1.ReasonSuspended && runtime.ObservedGeneration == c.Generation {
			ready.Message = "The Company is suspended: its engine is stopped and its data volumes kept."
		} else {
			ready.Message = "The Company is suspended, and the operator has not stopped its engine yet."
		}
		return ready
	}
	for _, t := range []string{
		crewletaiv1alpha1.ConditionAccepted,
		crewletaiv1alpha1.ConditionRuntimeReady,
		crewletaiv1alpha1.ConditionExposed,
		crewletaiv1alpha1.ConditionConfigApplied,
	} {
		condition := meta.FindStatusCondition(c.Status.Conditions, t)
		if condition == nil {
			// Only ConfigApplied can be missing here: Accepted is always
			// set, and the runtime conditions with it once it is True.
			ready.Reason = crewletaiv1alpha1.ReasonConfigPending
			ready.Message = "The company document has not been applied to the engine yet."
			return ready
		}
		if t == crewletaiv1alpha1.ConditionConfigApplied && condition.ObservedGeneration < c.Generation {
			// The runtime reconciler usually runs first after a spec
			// change: a ConfigApplied of the previous generation says
			// nothing about the document of this one, and Ready=True
			// stamped with this generation would tell a client waiting
			// for observedGeneration that the change is done.
			ready.Reason = crewletaiv1alpha1.ReasonConfigPending
			ready.Message = fmt.Sprintf("The company document of generation %d has not been applied to the engine yet "+
				"(ConfigApplied describes generation %d).", c.Generation, condition.ObservedGeneration)
			return ready
		}
		if condition.Status != metav1.ConditionTrue {
			ready.Reason = condition.Reason
			ready.Message = t + ": " + condition.Message
			return ready
		}
	}
	ready.Status = metav1.ConditionTrue
	ready.Reason = crewletaiv1alpha1.ReasonAvailable
	ready.Message = "The engine runs the company's document and is reachable as its class asks."
	return ready
}
