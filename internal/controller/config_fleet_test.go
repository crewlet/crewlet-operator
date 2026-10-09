package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
)

// TestPodOf pins how a node is told to be a pod of a StatefulSet: the engine
// node is named after its pod, and only the StatefulSet's name, '-' and an
// ordinal make one of its pods, so that a pool whose name extends another's
// is not taken for it.
// The Company, its data pod and the StatefulSet of its Shared pool general,
// in the tests of this file.
const (
	fleetCompany  = "acme"
	fleetDataPod  = "acme-data-0"
	fleetWorkload = "acme-seats-general"
)

func TestPodOf(t *testing.T) {
	for _, c := range []struct {
		node, workload string
		want           bool
	}{
		{fleetDataPod, "acme-data", true},
		{fleetWorkload + "-12", fleetWorkload, true},
		{fleetWorkload + "-2-0", fleetWorkload, false},
		{fleetWorkload + "-", fleetWorkload, false},
		{fleetWorkload, fleetWorkload, false},
		{fleetDataPod, fleetWorkload, false},
		{"acme-seat-ana-x", "acme-seat-ana", false},
	} {
		if got := podOf(c.node, c.workload); got != c.want {
			t.Errorf("podOf(%q, %q) = %t, want %t", c.node, c.workload, got, c.want)
		}
	}
}

// seatRun is a reconcile of one Company with one agent Seat, ana, placed on
// StatefulSet fleetWorkload and in the revision the engine runs; the Seat
// last reported node lastNode.
func seatRun(lastNode string) (*configRun, *part) {
	const workload = fleetWorkload
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany, UID: "company-uid"}
	seat := &crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatAna, Generation: 3}
	seat.Status.Handle = seatAna
	seat.Status.Placement = crewletaiv1alpha1.SeatPlacementStatus{Pool: "general", Node: lastNode}
	seat.Status.Conditions = []metav1.Condition{{
		Type: crewletaiv1alpha1.ConditionApplied, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonInRevision,
		ObservedGeneration: 3,
	}}
	p := newPart(seat)
	// decide clears the placement and sets the workload before Ready.
	seat.Status.Placement = crewletaiv1alpha1.SeatPlacementStatus{Pool: "general", Workload: workload}
	return &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{}, company: company, parts: []*part{p}}, p
}

// fleetStatefulSet is a StatefulSet named name that the seatRun Company
// controls, with ready pods.
func fleetStatefulSet(name string, ready int32) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{}
	sts.Namespace, sts.Name = "ns", name
	sts.Labels = map[string]string{crewletaiv1alpha1.CompanyLabel: fleetCompany}
	sts.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: crewletaiv1alpha1.GroupVersion.String(), Kind: "Company", Name: fleetCompany, UID: "company-uid", Controller: new(true),
	}}
	sts.Status.ReadyReplicas = ready
	return sts
}

func readyOf(p *part) metav1.Condition {
	c := meta.FindStatusCondition(*p.conditions(), crewletaiv1alpha1.ConditionReady)
	if c == nil {
		return metav1.Condition{}
	}
	return *c
}

// TestSeatReadyFollowsTheFleet pins each answer Ready gives an agent seat:
// the node the engine says holds it, and why none does.
func TestSeatReadyFollowsTheFleet(t *testing.T) {
	const workload = fleetWorkload
	acquired := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	t.Run("held by a pod of its StatefulSet", func(t *testing.T) {
		run, p := seatRun("")
		run.fleet = &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: workload + "-1", Epoch: 4, AcquiredAt: acquired}}}
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		run.decideReady(p, seat)
		ready := readyOf(p)
		if ready.Status != metav1.ConditionTrue || ready.Reason != crewletaiv1alpha1.ReasonLeaseHeld || ready.ObservedGeneration != 3 ||
			seat.Status.Placement.Node != workload+"-1" || strings.Contains(ready.Message, "not a pod") ||
			!strings.Contains(ready.Message, "2026-10-09T08:00:00Z") {
			t.Errorf("Ready = %+v, node %q; want True, LeaseHeld, at generation 3, held by %s-1 since its tenure",
				ready, seat.Status.Placement.Node, workload)
		}
	})

	t.Run("held by a pod outside its StatefulSet", func(t *testing.T) {
		run, p := seatRun("")
		run.fleet = &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: fleetDataPod, Epoch: 1}}}
		run.decideReady(p, p.obj.(*crewletaiv1alpha1.Seat))
		if ready := readyOf(p); ready.Status != metav1.ConditionTrue || !strings.Contains(ready.Message, "not a pod of StatefulSet "+workload) {
			t.Errorf("Ready = %+v, want True and naming the StatefulSet the seat moves to", ready)
		}
	})

	t.Run("in a pool no engine node runs", func(t *testing.T) {
		run, p := seatRun("acme-seats-general-0")
		run.fleet = &engine.Fleet{Unplaceable: []string{seatAna}}
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		run.decideReady(p, seat)
		ready := readyOf(p)
		if ready.Status != metav1.ConditionFalse || ready.Reason != crewletaiv1alpha1.ReasonUnplaced ||
			!strings.Contains(ready.Message, "no running engine node carries the label") || seat.Status.Placement.Node != "" {
			t.Errorf("Ready = %+v, node %q; want False, Unplaced, no node carrying its label, and no node", ready, seat.Status.Placement.Node)
		}
	})

	t.Run("placeable and not placed yet", func(t *testing.T) {
		run, p := seatRun("")
		run.fleet = &engine.Fleet{}
		run.decideReady(p, p.obj.(*crewletaiv1alpha1.Seat))
		if ready := readyOf(p); ready.Reason != crewletaiv1alpha1.ReasonUnplaced || !strings.Contains(ready.Message, "yet") {
			t.Errorf("Ready = %+v, want Unplaced, not yet", ready)
		}
	})

	t.Run("not in the revision the engine runs", func(t *testing.T) {
		run, p := seatRun("")
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		seat.Generation = 4
		run.fleet = &engine.Fleet{Unplaceable: []string{seatAna}}
		run.decideReady(p, seat)
		if ready := readyOf(p); ready.Reason != crewletaiv1alpha1.ReasonUnplaced || !strings.Contains(ready.Message, "not in the revision") {
			t.Errorf("Ready = %+v, want Unplaced, not in the revision", ready)
		}
	})

	t.Run("a fleet that could not be read", func(t *testing.T) {
		run, p := seatRun(workload + "-0")
		run.fleetErr = errors.New("GET /fleet: 503 unavailable")
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		run.decideReady(p, seat)
		ready := readyOf(p)
		if ready.Status != metav1.ConditionUnknown || ready.Reason != crewletaiv1alpha1.ReasonEngineUnavailable ||
			!strings.Contains(ready.Message, "503") || seat.Status.Placement.Node != workload+"-0" {
			t.Errorf("Ready = %+v, node %q; want Unknown, EngineUnavailable, the error, and the node last reported", ready, seat.Status.Placement.Node)
		}
	})

	t.Run("a fleet not read", func(t *testing.T) {
		run, p := seatRun(workload + "-0")
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		run.decideReady(p, seat)
		if ready := readyOf(p); ready.Reason != crewletaiv1alpha1.ReasonUnplaced || seat.Status.Placement.Node != workload+"-0" {
			t.Errorf("first Ready = %+v, node %q; want Unplaced, and the node last reported kept", ready, seat.Status.Placement.Node)
		}
		held := metav1.Condition{Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonLeaseHeld, ObservedGeneration: 2}
		meta.SetStatusCondition(&seat.Status.Conditions, held)
		run.decideReady(p, seat)
		if ready := readyOf(p); ready.Reason != crewletaiv1alpha1.ReasonLeaseHeld || ready.ObservedGeneration != 2 {
			t.Errorf("Ready = %+v, want the condition as it was", ready)
		}
	})

	t.Run("a suspended company", func(t *testing.T) {
		run, p := seatRun(workload + "-0")
		run.company.Spec.Suspend = new(true)
		seat := p.obj.(*crewletaiv1alpha1.Seat)
		run.decideReady(p, seat)
		if ready := readyOf(p); ready.Reason != crewletaiv1alpha1.ReasonSuspended || seat.Status.Placement.Node != "" {
			t.Errorf("Ready = %+v, node %q; want Suspended and no node", ready, seat.Status.Placement.Node)
		}
	})
}

// TestPlacementResyncPollsOnlyWhileSeatsSettle pins the one fast look the
// config reconciler takes: while a seat of the running revision is not held
// by a pod of its StatefulSet and that StatefulSet has a ready pod, for at
// most placementSettleWindow; and the resync interval otherwise, even after a
// reconcile that would not have looked again.
func TestPlacementResyncPollsOnlyWhileSeatsSettle(t *testing.T) {
	const workload = fleetWorkload
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := crewletaiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	statefulSet := func(ready int32) *appsv1.StatefulSet { return fleetStatefulSet(workload, ready) }
	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	reconciler := &CompanyConfigReconciler{ResyncInterval: time.Minute}
	look := func(at time.Time, ready int32, fleet *engine.Fleet, result ctrl.Result) time.Duration {
		t.Helper()
		reconciler.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(statefulSet(ready)).Build()
		reconciler.now = func() time.Time { return at }
		run, _ := seatRun("")
		run.CompanyConfigReconciler = reconciler
		run.fleet = fleet
		got, err := run.placementResync(context.Background(), result)
		if err != nil {
			t.Fatal(err)
		}
		return got.RequeueAfter
	}
	unplaced := &engine.Fleet{}
	held := &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: workload + "-0"}}}

	if got := look(start, 1, unplaced, ctrl.Result{}); got != placementPollInterval {
		t.Errorf("a seat not placed on a ready pool: RequeueAfter = %s, want %s", got, placementPollInterval)
	}
	if got := look(start.Add(time.Minute), 1, unplaced, ctrl.Result{RequeueAfter: 2 * time.Second}); got != 2*time.Second {
		t.Errorf("a sooner look asked for: RequeueAfter = %s, want it kept", got)
	}
	if got := look(start.Add(placementSettleWindow), 1, unplaced, ctrl.Result{}); got != time.Minute {
		t.Errorf("past the settle window: RequeueAfter = %s, want the resync interval", got)
	}
	if got := look(start.Add(placementSettleWindow+time.Second), 1, held, ctrl.Result{}); got != time.Minute {
		t.Errorf("a placed seat: RequeueAfter = %s, want the resync interval", got)
	}
	// Placed, so the next seat to settle starts a window of its own: here
	// one held by a data pod while its pool's pod is ready, which the engine
	// moves.
	later := start.Add(time.Hour)
	elsewhere := &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: fleetDataPod}}}
	if got := look(later, 1, elsewhere, ctrl.Result{}); got != placementPollInterval {
		t.Errorf("a seat held outside its ready pool, after the seats were placed: RequeueAfter = %s, want %s", got, placementPollInterval)
	}
	if got := look(later, 0, unplaced, ctrl.Result{}); got != time.Minute {
		t.Errorf("a pool with no ready pod: RequeueAfter = %s, want the resync interval", got)
	}
	run, _ := seatRun("")
	run.CompanyConfigReconciler = reconciler
	if got, err := run.placementResync(context.Background(), ctrl.Result{}); err != nil || got.RequeueAfter != 0 {
		t.Errorf("a reconcile that did not read the fleet: %+v, %v, want the result unchanged", got, err)
	}
}

// TestPlacementSettlingIsPerSeat keeps one seat stuck past the settle window
// from costing a seat that starts settling later its quick looks, and starts
// a seat's window again once it settled and starts over.
func TestPlacementSettlingIsPerSeat(t *testing.T) {
	var settling placementSettling
	company := types.NamespacedName{Namespace: "ns", Name: fleetCompany}
	start := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	if !settling.recent(company, []string{seatAna}, start) {
		t.Fatal("a seat that starts settling gets no quick looks")
	}
	stuck := start.Add(placementSettleWindow + time.Minute)
	if settling.recent(company, []string{seatAna}, stuck) {
		t.Error("a seat stuck past the window still gets quick looks")
	}
	if !settling.recent(company, []string{seatAna, "bo"}, stuck) {
		t.Error("a seat that starts settling beside a stuck one gets no quick looks")
	}
	settling.recent(company, nil, stuck)
	if !settling.recent(company, []string{seatAna}, stuck.Add(time.Second)) {
		t.Error("a seat that settled and starts over gets no quick looks")
	}
}

// TestSeatsPastAPodsShareAreSettling holds the quick looks to a move the
// engine makes between two pods of one StatefulSet: when a pool gains a pod,
// a pod holding more than its share of the pool's seats, ceil(seats / live
// pods), sheds them for the new pod to claim, and the Seats must follow
// before the next resync. Live is the engine's count, not the StatefulSet's
// ready pods.
func TestSeatsPastAPodsShareAreSettling(t *testing.T) {
	const (
		workload = fleetWorkload
		seatBo   = "bo"
	)
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	nodes := func(n int) []engine.FleetNode {
		var out []engine.FleetNode
		for i := range n {
			out = append(out, engine.FleetNode{ID: fmt.Sprintf("%s-%d", workload, i)})
		}
		return append(out, engine.FleetNode{ID: fleetDataPod})
	}
	leases := func(ana, bo string) []engine.SeatLease {
		return []engine.SeatLease{{Handle: seatAna, Node: workload + "-" + ana}, {Handle: seatBo, Node: workload + "-" + bo}}
	}
	for _, c := range []struct {
		name  string
		ready int32
		fleet *engine.Fleet
		want  []string
	}{
		{"both seats on one of two pods", 2, &engine.Fleet{Nodes: nodes(2), Seats: leases("0", "0")}, []string{seatAna, seatBo}},
		{"one seat on each of two pods", 2, &engine.Fleet{Nodes: nodes(2), Seats: leases("0", "1")}, nil},
		{"both seats on the one pod", 1, &engine.Fleet{Nodes: nodes(1), Seats: leases("0", "0")}, nil},
		// A seat pod's /ready requires its presence lease, so a ready pod
		// the fleet does not list live is a moment between two reads; the
		// engine shares the seats among the pods it counts live, and the
		// one it counts holds its whole share.
		{"both seats on one of two ready pods, one of them live", 2, &engine.Fleet{Nodes: nodes(1), Seats: leases("0", "0")}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, ana := seatRun("")
			bo := ana.obj.(*crewletaiv1alpha1.Seat).DeepCopy()
			bo.Name, bo.Status.Handle = seatBo, seatBo
			run.parts = append(run.parts, newPart(bo))
			bo.Status.Placement = ana.obj.(*crewletaiv1alpha1.Seat).Status.Placement
			run.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(fleetStatefulSet(workload, c.ready)).Build()
			run.fleet = c.fleet
			got, err := run.settlingSeats(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("settling %v, want %v", got, c.want)
			}
		})
	}
}
