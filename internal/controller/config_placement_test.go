package controller

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
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

	run := &configRun{company: &crewletaiv1alpha1.Company{}}
	run.recordPlacement(next, false)
	want := crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolBatch, crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{seatAna}}
	if got := run.placement.of(previous); !equality.Semantic.DeepEqual(got, want) {
		t.Fatalf("while the revision is applied the record is %+v, want %+v", got, want)
	}

	run.recordPlacement(*run.running, true)
	want = crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolBatch, crewletaiv1alpha1.DataPool}}
	if got := run.placement.of(previous); !equality.Semantic.DeepEqual(got, want) {
		t.Errorf("once the revision is applied the record is %+v, want %+v", got, want)
	}
}

// placementCompany is a Company whose status.config.placement is recorded,
// on a client that serves its status subresource.
func placementCompany(t *testing.T, recorded crewletaiv1alpha1.RevisionPlacement) (client.Client, *crewletaiv1alpha1.Company) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := crewletaiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany}
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{RevisionID: "r1", Placement: recorded}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(company).WithStatusSubresource(company).Build()
	got := &crewletaiv1alpha1.Company{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(company), got); err != nil {
		t.Fatal(err)
	}
	return c, got
}

// TestPlacementRecordKeepsAPinItsCacheDoesNotShow holds the union to the
// record the status write finds, not to the Company the reconcile read: a pin
// the previous reconcile wrote, which the cache did not show this one yet,
// stays when the write conflicts and is made again.
func TestPlacementRecordKeepsAPinItsCacheDoesNotShow(t *testing.T) {
	data := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool}}
	c, stale := placementCompany(t, data)

	// The previous reconcile's write, which the stale copy predates.
	written := stale.DeepCopy()
	written.Status.Config.Placement.Pools = append(written.Status.Config.Placement.Pools, poolGeneral)
	if err := c.Status().Update(context.Background(), written); err != nil {
		t.Fatal(err)
	}

	run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Client: c, APIReader: c}, company: stale}
	run.recordPlacement(crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolBatch}}, false)
	if err := run.patchCompanyStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := &crewletaiv1alpha1.Company{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(stale), got); err != nil {
		t.Fatal(err)
	}
	want := crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolBatch, crewletaiv1alpha1.DataPool, poolGeneral}}
	if !equality.Semantic.DeepEqual(got.Status.Config.Placement, want) {
		t.Errorf("the record is %+v, want %+v: a pin the cache did not show was dropped", got.Status.Config.Placement, want)
	}
}

// TestPlacementIsRecordedBeforeTheWriteThatActivatesIt holds the record ahead
// of an activation: the next revision's pins join what the API server holds,
// so that the runtime never sees a Company that has stopped asking for a pool
// beside a record that does not pin it yet.
func TestPlacementIsRecordedBeforeTheWriteThatActivatesIt(t *testing.T) {
	recorded := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}}
	c, company := placementCompany(t, recorded)
	stale := company.DeepCopy()
	stale.Status.Config.Placement = crewletaiv1alpha1.RevisionPlacement{}

	run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Client: c, APIReader: c}, company: stale}
	if err := run.widenPlacement(context.Background(), crewletaiv1alpha1.RevisionPlacement{Seats: []string{seatAna}}); err != nil {
		t.Fatal(err)
	}
	got := &crewletaiv1alpha1.Company{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(company), got); err != nil {
		t.Fatal(err)
	}
	want := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}, Seats: []string{seatAna}}
	if !equality.Semantic.DeepEqual(got.Status.Config.Placement, want) {
		t.Errorf("the record is %+v, want %+v", got.Status.Config.Placement, want)
	}
}

// TestPlacementOfTheEnginesOwnDocumentNarrowsOnceItIsApplied holds the record
// of a document the engine owns (Observe, Seed) to the union until the
// engine's own record says it applied that revision: the epoch the operator
// recorded at its last write says nothing about a document it did not write.
func TestPlacementOfTheEnginesOwnDocumentNarrowsOnceItIsApplied(t *testing.T) {
	running := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool}}
	recorded := crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral}}
	fleetApplied := func(revision string, status engine.ApplyStatus, epoch int64) *engine.Fleet {
		return &engine.Fleet{ThisNode: fleetDataPod, Nodes: []engine.FleetNode{{
			ID: fleetDataPod, Apply: engine.ApplyRecord{Epoch: epoch, RevisionID: revision, Status: status},
		}}}
	}
	for _, c := range []struct {
		name   string
		fleet  *engine.Fleet
		health engine.Health
		want   crewletaiv1alpha1.RevisionPlacement
	}{
		{"the engine still applies an earlier revision", fleetApplied("r1", engine.ApplyOK, 4), engine.Health{AppliedEpoch: 4}, recorded},
		{"the engine failed to apply it", fleetApplied("r2", engine.ApplyError, 5), engine.Health{AppliedEpoch: 4}, recorded},
		{"the engine applied it", fleetApplied("r2", engine.ApplyOK, 5), engine.Health{AppliedEpoch: 5}, running},
	} {
		t.Run(c.name, func(t *testing.T) {
			run := &configRun{company: &crewletaiv1alpha1.Company{}, fleet: c.fleet}
			run.recordPlacement(running, false)
			run.runningRevision = "r2"
			run.narrowPlacement(context.Background(), nil, c.health, nil)
			if got := run.placement.of(recorded); !equality.Semantic.DeepEqual(got, c.want) {
				t.Errorf("the record is %+v, want %+v", got, c.want)
			}
		})
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
