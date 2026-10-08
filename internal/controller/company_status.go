package controller

import (
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
// reported it, the Company is not Ready: an engine without its document runs
// nothing.
func summarize(c *crewletaiv1alpha1.Company) metav1.Condition {
	ready := metav1.Condition{Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionFalse}
	if suspended(c) {
		ready.Reason = crewletaiv1alpha1.ReasonSuspended
		ready.Message = "The Company is suspended: its engine is stopped and its data volumes kept."
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
