package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// readyCompany is a Company at generation 2 whose every condition but
// ConfigApplied is True for that generation.
func readyCompany(configApplied *metav1.Condition) *crewletaiv1alpha1.Company {
	c := &crewletaiv1alpha1.Company{}
	c.Generation = 2
	for _, t := range []string{crewletaiv1alpha1.ConditionAccepted, crewletaiv1alpha1.ConditionRuntimeReady, crewletaiv1alpha1.ConditionExposed} {
		c.Status.Conditions = append(c.Status.Conditions, metav1.Condition{
			Type: t, Status: metav1.ConditionTrue, Reason: "R", ObservedGeneration: 2,
		})
	}
	configApplied.Type = crewletaiv1alpha1.ConditionConfigApplied
	c.Status.Conditions = append(c.Status.Conditions, *configApplied)
	return c
}

func TestReadyWaitsForTheDocumentOfTheCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured metav1.Condition
		status     metav1.ConditionStatus
		reason     string
	}{
		{
			name:       "applied for this generation",
			configured: metav1.Condition{Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied, ObservedGeneration: 2},
			status:     metav1.ConditionTrue, reason: crewletaiv1alpha1.ReasonAvailable,
		},
		{
			name:       "applied for the previous generation",
			configured: metav1.Condition{Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied, ObservedGeneration: 1},
			status:     metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonConfigPending,
		},
		{
			name:       "refused for the previous generation",
			configured: metav1.Condition{Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonInvalid, ObservedGeneration: 1},
			status:     metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonConfigPending,
		},
		{
			name:       "refused for this generation",
			configured: metav1.Condition{Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonInvalid, ObservedGeneration: 2},
			status:     metav1.ConditionFalse, reason: crewletaiv1alpha1.ReasonInvalid,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready := summarize(readyCompany(&tc.configured))
			if ready.Status != tc.status || ready.Reason != tc.reason {
				t.Errorf("Ready is %s %s (%s), want %s %s", ready.Status, ready.Reason, ready.Message, tc.status, tc.reason)
			}
		})
	}
}

func TestConfigReportedSeesANewGeneration(t *testing.T) {
	changed := configReported()
	at := func(generation int64) *crewletaiv1alpha1.Company {
		return readyCompany(&metav1.Condition{
			Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonApplied, Message: "m", ObservedGeneration: generation,
		})
	}
	if !changed.Update(event.UpdateEvent{ObjectOld: at(1), ObjectNew: at(2)}) {
		t.Error("a ConfigApplied re-stamped with a new generation does not re-run the Company, whose Ready waits for it")
	}
	if changed.Update(event.UpdateEvent{ObjectOld: at(2), ObjectNew: at(2)}) {
		t.Error("an unchanged ConfigApplied re-runs the Company")
	}
	moved := at(2)
	moved.Status.Config.Placement = crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool}}
	if !changed.Update(event.UpdateEvent{ObjectOld: at(2), ObjectNew: moved}) {
		t.Error("a revision that pins seats elsewhere does not re-run the Company, whose seat pods it decides")
	}
}
