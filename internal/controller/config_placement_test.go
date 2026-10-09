package controller

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The pools and a seat the placement specs name.
const (
	poolGeneral  = "general"
	poolIsolated = "isolated"
	poolBatch    = "batch"
	seatAna      = "ana"
)

// TestPlacementRecordKeepsThePreviousPinsUntilApplied holds the rule the
// runtime's pool removal depends on: a revision activated but not yet
// applied adds its pins to what the record held, since the engine keeps
// running the previous revision until it applies the new one (and for good if
// the apply fails); once applied, the record is that revision's pins alone.
func TestPlacementRecordKeepsThePreviousPinsUntilApplied(t *testing.T) {
	previous := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{seatAna}}
	next := crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolBatch, crewletaiv1alpha1.DataPool}}
	company := &crewletaiv1alpha1.Company{}
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{RevisionID: "r1", Placement: previous}

	run := &configRun{company: company}
	run.recordPlacement(next, false)
	want := crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolBatch, crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{seatAna}}
	if run.config == nil || !equality.Semantic.DeepEqual(run.config.Placement, want) {
		t.Fatalf("while the revision is applied the record is %+v, want %+v", run.config, want)
	}
	if run.config.RevisionID != "r1" {
		t.Errorf("the record lost its revision: %+v", run.config)
	}

	run.recordPlacement(*run.running, true)
	if !equality.Semantic.DeepEqual(run.config.Placement, next) {
		t.Errorf("once the revision is applied the record is %+v, want %+v", run.config.Placement, next)
	}

	unchanged := &configRun{company: company}
	unchanged.recordPlacement(previous, true)
	if unchanged.config != nil {
		t.Errorf("a record that already says it was rewritten: %+v", unchanged.config)
	}
}

// TestPlacementRecordHoldsOnlyWhatNamesAPod keeps out of the record a value
// no pool or seat of the operator's could have (a document the engine owns
// may pin anything), and bounds it to what the status holds.
func TestPlacementRecordHoldsOnlyWhatNamesAPod(t *testing.T) {
	got := boundedPlacement(
		[]string{poolGeneral, "Not-A-Pool", "a-pool-name-longer-than-twenty", poolGeneral, crewletaiv1alpha1.DataPool},
		[]string{seatAna, "ana_b", "-x"},
	)
	want := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{seatAna}}
	if !equality.Semantic.DeepEqual(got, want) {
		t.Errorf("recorded %+v, want %+v", got, want)
	}

	seats := make([]string, 0, maxPlacedSeats+5)
	for i := range maxPlacedSeats + 5 {
		seats = append(seats, fmt.Sprintf("seat-%04d", i))
	}
	got = boundedPlacement(nil, seats)
	if len(got.Seats) != maxPlacedSeats || got.Seats[0] != "seat-0000" || got.Pools != nil {
		t.Errorf("recorded %d seats from %s and pools %v, want the first %d", len(got.Seats), got.Seats[0], got.Pools, maxPlacedSeats)
	}
}
