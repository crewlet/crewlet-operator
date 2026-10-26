package controller

import (
	"testing"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestASeatPodIsGivenTheFleetsBridgeAddress holds a seat pod to the tool
// bridge address the data pods are given, and to none when they have none.
// Crewlet's bridge address is the fleet's: a box's call reaches a data pod,
// which relays it over the broker to the pod holding the run's seat, a seat
// pod's included — and the seat pod mints its runs' endpoints on that same
// address. Given none, it builds no bridge, and refuses box mode on its seats.
func TestASeatPodIsGivenTheFleetsBridgeAddress(t *testing.T) {
	c := &crewletaiv1alpha1.Company{Namespace: "tenant", Name: companyName}
	class := &crewletaiv1alpha1.EngineClass{Name: "standard"}
	w := &seatWorkload{name: companyName + "-seats-" + poolGeneral, pool: poolGeneral, plan: &render.PoolPlan{}}
	doc := &render.BootstrapDocument{}
	for _, want := range []string{"", "https://nimbus.example.com"} {
		pod, err := seatPodTemplate(c, class, w, w.name, "engine:main", doc, "hash", want)
		if err != nil {
			t.Fatalf("seatPodTemplate: %v", err)
		}
		var got []string
		for _, env := range pod.Spec.Containers[0].Env {
			if *env.Name == envMCPBridgeURL {
				got = append(got, *env.Value)
			}
		}
		switch {
		case want == "" && len(got) != 0:
			t.Errorf("a seat pod of a company with no bridge address was given %v", got)
		case want != "" && (len(got) != 1 || got[0] != want):
			t.Errorf("a seat pod was given %s %v, want the data pods' %q", envMCPBridgeURL, got, want)
		}
	}
}
