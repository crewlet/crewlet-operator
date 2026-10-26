package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// Where the engine runs each agent seat is the engine's to decide: every node
// that runs seats claims, from the leases in the coordination store, the
// seats whose role.placement its labels match, a few every five seconds, and
// a node that stops releases them or lets their leases lapse. The operator
// only places the pods and pins each seat to its pool's labels. What it
// reports on a Seat (status.placement.node, and Ready) is therefore what the
// engine's GET /fleet says, read on each of the config reconciler's looks at
// the engine: every node answers it the same, from the lease table.

// placementPollInterval is how soon the config reconciler looks at a
// Company's fleet again while an agent seat of the revision the engine runs
// is not held by a pod of its own StatefulSet although that StatefulSet has a
// ready pod: the engine is placing the seat. A node that runs seats sweeps
// every 5 seconds and claims at most 4 seats a sweep; a node that has just
// joined claims nothing until a data node reports its copy level, and a seat
// whose holder was killed waits out its 45-second lease. Fifteen seconds is
// three sweeps, so the Seat's status follows a move within a few looks, each
// costing the reconciler's usual handful of reads of the engine.
const placementPollInterval = 15 * time.Second

// placementSettleWindow bounds how long the config reconciler looks that
// often. A seat the engine has not placed on a ready pod of its StatefulSet
// in three minutes, four times the longest lease, is not being placed but is
// stuck (a pod that runs and never joins the fleet: a seat pod whose leaf
// cannot reach the data pods gives up and restarts within a minute, over
// and over). The Seat's Ready condition says so, and the reconciler goes back
// to its resync interval rather than polling a fault.
const placementSettleWindow = 3 * time.Minute

// placementSettling remembers, per Company and per seat, since when each of
// its seats has been settling (see placementPollInterval): one seat stuck
// past the window must not cost a seat that starts settling later its quick
// looks. In memory only: an operator that restarts polls one more window,
// which costs nothing.
type placementSettling struct {
	mu    sync.Mutex
	since map[types.NamespacedName]map[string]time.Time
}

// recent records which of the company's seats are settling now, forgetting
// those that no longer are, and reports whether any started settling less
// than placementSettleWindow ago.
func (p *placementSettling) recent(company types.NamespacedName, seats []string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.since == nil {
		p.since = map[types.NamespacedName]map[string]time.Time{}
	}
	before := p.since[company]
	current := make(map[string]time.Time, len(seats))
	recent := false
	for _, seat := range seats {
		since, ok := before[seat]
		if !ok {
			since = now
		}
		current[seat] = since
		recent = recent || now.Sub(since) < placementSettleWindow
	}
	p.since[company] = current
	return recent
}

func (p *placementSettling) forget(company types.NamespacedName) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.since, company)
}

// fleetOf is the engine's fleet, read at most once a reconcile: the apply's
// verification and the seats' placement read the same answer.
func (run *configRun) fleetOf(ctx context.Context, eng *engine.Client) (*engine.Fleet, error) {
	if run.fleet == nil && run.fleetErr == nil {
		fleet, err := eng.Fleet(ctx)
		if err != nil {
			logf.FromContext(ctx).V(1).Info("Failed to read the engine's fleet", "error", err.Error())
			run.fleetErr = err
		} else {
			run.fleet = &fleet
		}
	}
	return run.fleet, run.fleetErr
}

// lookAtFleet reads the fleet of a Company whose next document this
// reconcile did not get to apply (it refers to something missing, or holds a
// Secret key that does not exist): the engine keeps running its last good
// revision, and places that revision's seats whatever is wrong with the next.
// A Company that runs no engine has no fleet to read.
func (run *configRun) lookAtFleet(ctx context.Context) {
	c := run.company
	if suspended(c) || meta.FindStatusCondition(c.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady) == nil {
		return
	}
	eng, err := run.Engines.Engine(ctx, c)
	if err != nil {
		run.fleetErr = err
		return
	}
	_, _ = run.fleetOf(ctx, eng)
}

// leaseOf is the lease of the seat with handle in the fleet the reconcile
// read, found by its handle or by the id the engine derives for it, which a
// node that does not run the seat names the lease by (engine.Fleet.Holder).
// run.fleet must be set.
func (run *configRun) leaseOf(handle string) (engine.SeatLease, bool) {
	return run.fleet.Holder(handle, render.SeatID(run.company.Spec.CompanyName, handle))
}

// decideReady sets an agent Seat's Ready condition and status.placement.node
// from the engine's fleet: True (LeaseHeld) while a node holds the seat's
// lease, False (Unplaced) while none does. Neither changes when this
// reconcile did not read the fleet, which it reads whenever the Company runs
// an engine; Unknown is an engine whose fleet could not be read. A seat whose
// executor cannot run a turn on the node its spec places it on is False with
// that reason whoever holds it: the engine applies the seat and refuses
// every one of its turns, so a lease says nothing about whether it works.
// The verdict is the spec's, also while a move holds the seat's pin where it
// ran (PlacementPending): that lasts until a pod of its new pool starts.
func (run *configRun) decideReady(p *part, s *crewletaiv1alpha1.Seat) {
	gen := s.Generation
	conditions := p.conditions()
	handle := s.Status.Handle
	workload := s.Status.Placement.Workload
	switch {
	case suspended(run.company):
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, crewletaiv1alpha1.ReasonSuspended,
			fmt.Sprintf("Company %s is suspended: no engine node runs its seats, and their messages wait.", run.company.Name))
	case p.executor != nil && p.executor.Reason != "":
		message := p.executor.Field + ": " + p.executor.Message + "."
		lease, held := engine.SeatLease{}, false
		if run.fleet != nil {
			lease, held = run.leaseOf(handle)
		}
		switch {
		case held:
			s.Status.Placement.Node = lease.Node
			message += fmt.Sprintf(" Engine node %s holds the seat.", lease.Node)
		case run.fleet != nil:
			message += " No engine node holds the seat."
		default:
			s.Status.Placement.Node = p.base.(*crewletaiv1alpha1.Seat).Status.Placement.Node
		}
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, p.executor.Reason, message)
	case run.fleet != nil:
		lease, held := run.leaseOf(handle)
		if held {
			s.Status.Placement.Node = lease.Node
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, true, crewletaiv1alpha1.ReasonLeaseHeld,
				heldMessage(lease, workload))
			return
		}
		if pool := s.Status.Placement.Pool; !declaredPool(run.company, pool) {
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, crewletaiv1alpha1.ReasonPoolNotFound, fmt.Sprintf(
				"No engine node holds the seat: it is placed in pool %s, which Company %s does not declare "+
					"(spec.topology.seatPools), so no pod carries the label it is pinned to. Its messages wait for the "+
					"Company to declare the pool, or for the Seat to name another.", pool, run.company.Name))
			return
		}
		setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, crewletaiv1alpha1.ReasonUnplaced,
			run.unplacedMessage(p, handle, workload))
	case run.fleetErr != nil:
		s.Status.Placement.Node = p.base.(*crewletaiv1alpha1.Seat).Status.Placement.Node
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionUnknown, Reason: crewletaiv1alpha1.ReasonEngineUnavailable,
			Message:            bounded("The engine's fleet, which says which node holds the seat, could not be read: " + run.fleetErr.Error()),
			ObservedGeneration: gen,
		})
	default:
		// Not looked at: what the engine last reported stands, but not a
		// verdict of the spec's that this render no longer finds.
		s.Status.Placement.Node = p.base.(*crewletaiv1alpha1.Seat).Status.Placement.Node
		ready := meta.FindStatusCondition(*conditions, crewletaiv1alpha1.ConditionReady)
		switch {
		case ready == nil:
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, crewletaiv1alpha1.ReasonUnplaced,
				"No engine has reported holding the seat yet.")
		case run.objectsChecked && executorReason(ready.Reason):
			setCondition(conditions, gen, crewletaiv1alpha1.ConditionReady, false, crewletaiv1alpha1.ReasonUnplaced, fmt.Sprintf(
				"Its turns can run now (it was %s), and the engine has not been asked since which node holds the seat.",
				ready.Reason))
		}
	}
}

// executorReason reports whether reason is Ready's for a seat whose executor
// cannot run a turn, a verdict of the seat's spec rather than of the fleet.
func executorReason(reason string) bool {
	return reason == crewletaiv1alpha1.ReasonBoxModeBridgeUnsupported || reason == crewletaiv1alpha1.ReasonNotExposed
}

// declaredPool reports whether pool is one a seat of the Company can run in:
// data, or one of its spec.topology.seatPools.
func declaredPool(c *crewletaiv1alpha1.Company, pool string) bool {
	return pool == crewletaiv1alpha1.DataPool ||
		slices.ContainsFunc(c.Spec.Topology.SeatPools, func(p crewletaiv1alpha1.SeatPool) bool { return p.Name == pool })
}

// heldMessage is Ready's message for a seat a node holds.
func heldMessage(lease engine.SeatLease, workload string) string {
	message := fmt.Sprintf("Engine node %s holds the seat (lease epoch %d)", lease.Node, lease.Epoch)
	if !lease.AcquiredAt.IsZero() {
		message += " since " + lease.AcquiredAt.UTC().Format(time.RFC3339)
	}
	message += "."
	if workload != "" && !podOf(lease.Node, workload) {
		message += fmt.Sprintf(" It is not a pod of StatefulSet %s, which runs the seat in the revision being applied; "+
			"the node releases the seat once it applies that revision, and a pod of %s takes it.", workload, workload)
	}
	return message
}

// unplacedMessage is Ready's message for an agent seat no node holds, by
// what keeps it unplaced.
func (run *configRun) unplacedMessage(p *part, handle, workload string) string {
	switch {
	case !p.inRevision():
		return "No engine node holds the seat: it is not in the revision the engine runs (see Applied)."
	case workload == "":
		return "No engine node holds the seat: no pod runs its pool (see SeatsPlaced on Company " + run.company.Name + ")."
	case slices.Contains(run.fleet.Unplaceable, handle):
		return fmt.Sprintf("No engine node holds the seat: no running engine node carries the label its placement "+
			"asks for, which only the pods of StatefulSet %s carry. Its messages wait for one (see SeatsPlaced on Company %s).",
			workload, run.company.Name)
	}
	return fmt.Sprintf("No engine node holds the seat yet: a pod of StatefulSet %s runs, and the engine claims a few seats "+
		"every five seconds; a node that has just started claims none before a data node reports its copy current.", workload)
}

// podOf reports whether an engine node, named after its pod, is a pod of the
// StatefulSet workload: the StatefulSet's name, '-', and an ordinal.
func podOf(node, workload string) bool {
	ordinal, ok := strings.CutPrefix(node, workload+"-")
	if !ok || ordinal == "" {
		return false
	}
	for _, r := range ordinal {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// placementResync decides when to look at the Company's fleet again, once
// this reconcile has read it: soon while seats are settling onto a ready pod
// (placementPollInterval, for at most placementSettleWindow), and otherwise
// at the resync interval at the latest, including after a reconcile that
// applied nothing, so that the Seats' Ready follows the engine while the
// next document is held back.
func (run *configRun) placementResync(ctx context.Context, result ctrl.Result) (ctrl.Result, error) {
	if run.fleet == nil {
		return result, nil
	}
	key := client.ObjectKeyFromObject(run.company)
	settling, err := run.settlingSeats(ctx)
	if err != nil {
		return result, err
	}
	if result.RequeueAfter == 0 {
		result.RequeueAfter = run.resyncInterval()
	}
	if len(settling) == 0 {
		run.settling.forget(key)
		return result, nil
	}
	if run.settling.recent(key, settling, run.clock()) {
		result.RequeueAfter = min(result.RequeueAfter, placementPollInterval)
		logf.FromContext(ctx).V(1).Info("Seats are being placed; looking at the engine's fleet again soon",
			"seats", settling, "after", result.RequeueAfter)
	}
	return result, nil
}

// settlingSeats are the handles of the agent seats of the revision the
// engine runs that the engine is still placing on the pods of their
// StatefulSet, while that StatefulSet has a ready pod: those no pod of it
// holds (the engine is placing them, or moving them there), and those a pod
// of it holds past its share (it is shedding them onto another pod of it).
//
// The second kind is a move between two pods of one StatefulSet, which the
// first cannot see: when a pool gains a pod, the engine bounds each pod by
// its share of the seats pinned to the pool, ceil(seats / live pods), and a
// pod past it gives seats back for the new pod to claim, a few seconds of
// sweeps later. Without it the Seats would name the pod they left until the
// next resync.
func (run *configRun) settlingSeats(ctx context.Context) ([]string, error) {
	list := &appsv1.StatefulSetList{}
	if err := run.List(ctx, list, client.InNamespace(run.company.Namespace),
		client.MatchingLabels{crewletaiv1alpha1.CompanyLabel: run.company.Name}); err != nil {
		return nil, fmt.Errorf("listing the StatefulSets of Company %s/%s: %w", run.company.Namespace, run.company.Name, err)
	}
	// The company's StatefulSets that have a ready pod.
	ready := map[string]bool{}
	for i := range list.Items {
		if sts := &list.Items[i]; metav1.IsControlledBy(sts, run.company) && sts.Status.ReadyReplicas > 0 {
			ready[sts.Name] = true
		}
	}
	var out []string
	// The seats each pod of a ready StatefulSet holds, and how many seats
	// that StatefulSet is placed, by StatefulSet.
	held := map[string]map[string][]string{}
	placed := map[string]int{}
	for _, p := range run.parts {
		s, ok := p.obj.(*crewletaiv1alpha1.Seat)
		if !ok || s.Spec.Kind == crewletaiv1alpha1.SeatKindHuman || p.handleTaken || !p.inRevision() {
			continue
		}
		workload := s.Status.Placement.Workload
		if workload == "" || !ready[workload] {
			continue
		}
		placed[workload]++
		lease, isHeld := run.leaseOf(s.Status.Handle)
		if !isHeld || !podOf(lease.Node, workload) {
			out = append(out, s.Status.Handle)
			continue
		}
		if held[workload] == nil {
			held[workload] = map[string][]string{}
		}
		held[workload][lease.Node] = append(held[workload][lease.Node], s.Status.Handle)
	}
	for _, workload := range slices.Sorted(maps.Keys(held)) {
		// The pods the engine shares the seats among: those it counts live,
		// one at least, since a pod of the StatefulSet holds seats. A seat
		// pod's /ready requires its presence lease, so its ready pods say
		// nothing the live ones do not.
		var live int
		for i := range run.fleet.Nodes {
			if podOf(run.fleet.Nodes[i].ID, workload) {
				live++
			}
		}
		live = max(live, 1)
		share := (placed[workload] + live - 1) / live
		for _, node := range slices.Sorted(maps.Keys(held[workload])) {
			if seats := held[workload][node]; len(seats) > share {
				out = append(out, seats...)
			}
		}
	}
	return out, nil
}
