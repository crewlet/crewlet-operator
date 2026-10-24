package crd_test

import (
	"testing"

	"github.com/crewlet/crewlet-operator/internal/render"
)

// charterKeys are the company document's keys a charter rule is about.
var charterKeys = map[string]bool{"mission": true, "vision": true, "policies": true}

// TestCharterBoundsFollowTheEngine holds the Company API's charter rules to
// the engine every suite runs (Crewlet's main): every case of charterCases is
// refused by the API exactly when the engine faults the charter it renders
// to. The engine never refuses a charter a document arrives with, since a
// file that ran under an earlier build must still import and boot, so its
// verdict here is `crewlet validate`'s warning at mission, vision or
// policies; the API is where a new fault is stopped before the operator
// writes it. A Crewlet change that moves a bound, a unit, the trimming or
// the line breaks fails here, on the next run, until the API follows it.
func TestCharterBoundsFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	for _, c := range charterCases() {
		t.Run(c.name, func(t *testing.T) {
			probe := company(namespace, "charter")
			c.withCharter(probe)
			refused := apiVerdict(t, probe) != ""

			in := probeCompany()
			c.withCharter(in.Company)
			doc, err := render.Company(in)
			if err != nil {
				t.Fatalf("rendering the charter: %v", err)
			}
			problems, warnings := validateVerdict(t, doc, doc.YAML)
			if len(problems) > 0 {
				t.Fatalf("crewlet validate refused the probe: %v", problems)
			}
			var faults []string
			for _, w := range warnings {
				if len(w.Segments) > 0 && charterKeys[w.Segments[0].Key] {
					faults = append(faults, render.PathOf(w.Segments).String()+": "+w.Message)
				}
			}
			switch {
			case refused && len(faults) == 0:
				t.Errorf("the API refuses a charter the engine takes")
			case !refused && len(faults) > 0:
				t.Errorf("the API admits a charter the engine faults: %v", faults)
			}
		})
	}
}
