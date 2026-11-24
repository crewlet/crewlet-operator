package controller

import (
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// nowhere is a name no object or pool of the specs' companies has.
const nowhere = "nowhere"

// harnessModel is the model of the specs' providers, which no harness is ever asked
// for.
const harnessModel = "sonnet"

// TestExecutorIsNamedOnReady holds an agent Seat's Ready, True LeaseHeld while
// an engine node holds its lease, to what the render states of its executor:
// the message says where the executor runs. A seat the render states no
// executor for, in a Company whose document it refuses, is Ready as its lease
// says all the same.
//
// Mutation: drop the executor's message from Ready's, and this fails.
func TestExecutorIsNamedOnReady(t *testing.T) {
	held := &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: fleetWorkload + "-0", Epoch: 2}}}
	for _, c := range []struct {
		name     string
		executor *render.Executor
		says     []string
	}{
		{
			name:     "an executor",
			executor: &render.Executor{Message: "runs the seat's executor as ClaudeCode, in a box on the data pod"},
			says:     []string{"holds the seat", "It runs the seat's executor as ClaudeCode, in a box on the data pod."},
		},
		{name: "no executor", says: []string{"holds the seat"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, p := seatRun("")
			run.fleet = held
			p.executor = c.executor
			run.decideReady(p, p.obj.(*crewletaiv1alpha1.Seat))
			r := meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.Seat).Status.Conditions, crewletaiv1alpha1.ConditionReady)
			if r == nil || r.Status != metav1.ConditionTrue || r.Reason != crewletaiv1alpha1.ReasonLeaseHeld {
				t.Fatalf("Ready is %+v, want True %s", r, crewletaiv1alpha1.ReasonLeaseHeld)
			}
			for _, phrase := range c.says {
				if !strings.Contains(r.Message, phrase) {
					t.Errorf("Ready's message does not say %q: %s", phrase, r.Message)
				}
			}
			if got := p.obj.(*crewletaiv1alpha1.Seat).Status.Placement.Node; got != fleetWorkload+"-0" {
				t.Errorf("status.placement.node is %q, want %q", got, fleetWorkload+"-0")
			}
		})
	}
}

// reconcileStatus stands in for a reconcile's status write of one part:
// decided and announced as writeStatus does, then taken as the status the
// part has, which the next reconcile starts from.
func reconcileStatus(run *configRun, p *part) {
	run.decide(p, nil, nil)
	run.announce(p)
	p.base = p.obj.DeepCopyObject().(client.Object)
}

// drained is every event recorder holds now.
func drained(recorder *events.FakeRecorder) []string {
	var out []string
	for {
		select {
		case e := <-recorder.Events:
			out = append(out, e)
		default:
			return out
		}
	}
}

// TestReportedFindingsHoldNothing holds what the render found wrong with a
// part's references that holds nothing (render.ReportedOf) to ResolvedRefs
// alone: False with the finding's reason, the part no problem of the
// document's, announced by one Warning event and not again while the reason
// stands. Beside a finding that holds the document, which ResolvedRefs shows
// first, it is announced once that one is gone, not on every reconcile.
func TestReportedFindingsHoldNothing(t *testing.T) {
	pool := &render.RefError{
		Kind: render.KindSeat, Name: seatAna, Field: "spec.placement.pool", Reason: crewletaiv1alpha1.ReasonPoolNotFound,
		Message: "names pool nowhere", Basis: render.Reported,
	}
	const announced = "Warning PoolNotFound Spec.placement.pool: names pool nowhere."
	run, p := seatRun("")
	recorder := events.NewFakeRecorder(10)
	run.Recorder = recorder
	run.objectsChecked = true
	run.checkReported([]*render.RefError{pool})
	if p.hasProblem() {
		t.Error("a finding that holds nothing holds the document")
	}
	reconcileStatus(run, p)
	c := meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.Seat).Status.Conditions, crewletaiv1alpha1.ConditionResolvedRefs)
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != crewletaiv1alpha1.ReasonPoolNotFound ||
		c.Message != "Spec.placement.pool: names pool nowhere." {
		t.Errorf("ResolvedRefs is %+v, want False %s", c, crewletaiv1alpha1.ReasonPoolNotFound)
	}
	for range 2 {
		p.reported, p.reportedReason = nil, ""
		run.checkReported([]*render.RefError{pool})
		reconcileStatus(run, p)
	}
	if got := drained(recorder); !slices.Equal(got, []string{announced}) {
		t.Errorf("three reconciles announced %q, want %q once", got, announced)
	}

	t.Run("beside a finding that holds the document", func(t *testing.T) {
		run, p := seatRun("")
		recorder := events.NewFakeRecorder(10)
		run.Recorder = recorder
		run.objectsChecked = true
		for range 3 {
			p.reported, p.reportedReason = nil, ""
			p.unresolved, p.unresolvedReason = []string{"spec.unitRef.name: names Unit gone"}, crewletaiv1alpha1.ReasonRefNotFound
			run.checkReported([]*render.RefError{pool})
			reconcileStatus(run, p)
		}
		if got := drained(recorder); len(got) > 0 {
			t.Errorf("the reported finding, which ResolvedRefs does not show, is announced: %q", got)
		}
		for range 2 {
			p.reported, p.reportedReason = nil, ""
			p.unresolved, p.unresolvedReason = nil, ""
			run.checkReported([]*render.RefError{pool})
			reconcileStatus(run, p)
		}
		if got := drained(recorder); !slices.Equal(got, []string{announced}) {
			t.Errorf("two reconciles after the held finding went announced %q, want %q once", got, announced)
		}
	})
}

// TestReadyNamesAnUndeclaredPool holds an agent Seat no node holds, placed in
// a pool its Company does not declare, to Ready=False PoolNotFound rather
// than Unplaced: no pod will ever carry its pin, which no wait fixes.
func TestReadyNamesAnUndeclaredPool(t *testing.T) {
	run, p := seatRun("")
	run.fleet = &engine.Fleet{}
	declared := run.company.Spec.Topology.SeatPools
	run.company.Spec.Topology.SeatPools = nil
	seat := p.obj.(*crewletaiv1alpha1.Seat)
	run.decideReady(p, seat)
	if r := meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionReady); r == nil ||
		r.Reason != crewletaiv1alpha1.ReasonPoolNotFound || !strings.Contains(r.Message, "pool general") {
		t.Errorf("Ready is %+v, want PoolNotFound for a pool the Company does not declare", r)
	}
	run.company.Spec.Topology.SeatPools = declared
	run.decideReady(p, seat)
	if r := meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionReady); r == nil ||
		r.Reason != crewletaiv1alpha1.ReasonUnplaced {
		t.Errorf("Ready is %+v, want Unplaced once the Company declares the pool", r)
	}
}
