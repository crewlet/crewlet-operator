package controller

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// executorField is the field of a Seat that names the member its executor
// runs on.
const executorField = "spec.llm.default[0].name"

// nowhere is a name no object or pool of the specs' companies has, and
// noBridge the message of an executor the render finds unrunnable.
const (
	nowhere  = "nowhere"
	noBridge = "no bridge"
)

// cliModel is the model of the specs' providers, which no CLI is ever asked
// for.
const cliModel = "sonnet"

// TestExecutorVerdictFollowsTheRender holds an agent Seat's Ready to what the
// render found about its executor before what the fleet says: a node holding
// the lease of a seat the engine fails every turn of is no sign that it runs,
// so the seat is not Ready, whichever of the three reasons the render gives,
// and the message still names the node. A seat whose executor can run is
// Ready as its lease says, and the message says where its executor runs.
func TestExecutorVerdictFollowsTheRender(t *testing.T) {
	ready := func(p *part) *metav1.Condition {
		return meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.Seat).Status.Conditions, crewletaiv1alpha1.ConditionReady)
	}
	held := &engine.Fleet{Seats: []engine.SeatLease{{Handle: seatAna, Node: fleetWorkload + "-0", Epoch: 2}}}
	for _, c := range []struct {
		name     string
		executor *render.Executor
		fleet    *engine.Fleet
		// lastNode is the node status.placement.node named before, and
		// wantNode the one it names after.
		lastNode, wantNode string
		want               string
		says               []string
	}{
		{
			name:     "not exposed, held",
			executor: &render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonNotExposed, Message: noBridge},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonNotExposed,
			says:     []string{executorField + ": no bridge.", "Engine node " + fleetWorkload + "-0 holds the seat."},
		},
		{
			// The fleet was not read: the node last reported stands.
			name:     "not exposed, fleet not read",
			executor: &render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonNotExposed, Message: noBridge},
			lastNode: fleetWorkload + "-1",
			wantNode: fleetWorkload + "-1",
			want:     crewletaiv1alpha1.ReasonNotExposed,
			says:     []string{executorField + ": no bridge."},
		},
		{
			name:     "not exposed, held by nobody",
			executor: &render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonNotExposed, Message: "no address"},
			fleet:    &engine.Fleet{},
			want:     crewletaiv1alpha1.ReasonNotExposed,
			says:     []string{"No engine node holds the seat."},
		},
		{
			name: "a login that cannot follow the run, held",
			executor: &render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonSandboxLoginNotPortable,
				Message: "no login"},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonSandboxLoginNotPortable,
			says:     []string{executorField + ": no login.", "Engine node " + fleetWorkload + "-0 holds the seat."},
		},
		{
			name: "a Direct run on a seat pod with no address, held",
			executor: &render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonBridgeUnreachable,
				Message: noBridge},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonBridgeUnreachable,
			says:     []string{executorField + ": no bridge.", "Engine node " + fleetWorkload + "-0 holds the seat."},
		},
		{
			name:     "runnable, held",
			executor: &render.Executor{Field: executorField, Message: "runs the seat's executor as ClaudeCode, Direct"},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonLeaseHeld,
			says:     []string{"holds the seat", "It runs the seat's executor as ClaudeCode, Direct."},
		},
		{
			// A seat the render has no executor for, in a Company whose
			// document it refuses, is Ready as its lease says.
			name:     "no executor, held",
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonLeaseHeld,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, p := seatRun(c.lastNode)
			run.fleet = c.fleet
			p.executor = c.executor
			run.decideReady(p, p.obj.(*crewletaiv1alpha1.Seat))
			r := ready(p)
			if r == nil || r.Reason != c.want {
				t.Fatalf("Ready is %+v, want %s", r, c.want)
			}
			for _, phrase := range c.says {
				if !strings.Contains(r.Message, phrase) {
					t.Errorf("Ready's message does not say %q: %s", phrase, r.Message)
				}
			}
			if got := p.obj.(*crewletaiv1alpha1.Seat).Status.Placement.Node; got != c.wantNode {
				t.Errorf("status.placement.node is %q, want %q", got, c.wantNode)
			}
		})
	}
}

// TestFixedExecutorVerdictIsNotKept holds an agent Seat's Ready to the render
// when the fleet was not read: a verdict of the spec's that the render no
// longer finds is not kept as what the engine last reported, whichever of the
// three it was, while one a reconcile that did not render cannot judge stands.
func TestFixedExecutorVerdictIsNotKept(t *testing.T) {
	for _, before := range []string{
		crewletaiv1alpha1.ReasonNotExposed, crewletaiv1alpha1.ReasonSandboxLoginNotPortable,
		crewletaiv1alpha1.ReasonBridgeUnreachable,
	} {
		for _, c := range []struct {
			name     string
			rendered bool
			want     string
		}{
			{name: "rendered", rendered: true, want: crewletaiv1alpha1.ReasonUnplaced},
			{name: "not rendered", want: before},
		} {
			t.Run(before+" "+c.name, func(t *testing.T) {
				run, p := seatRun("")
				seat := p.obj.(*crewletaiv1alpha1.Seat)
				meta.SetStatusCondition(&seat.Status.Conditions, metav1.Condition{
					Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: before,
					ObservedGeneration: 2,
				})
				run.objectsChecked = c.rendered
				if c.rendered {
					p.executor = &render.Executor{Field: executorField, Message: "runs"}
				}
				run.decideReady(p, seat)
				r := meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionReady)
				if r == nil || r.Reason != c.want {
					t.Fatalf("Ready is %+v, want %s", r, c.want)
				}
				if c.rendered && (r.ObservedGeneration != seat.Generation || !strings.Contains(r.Message, "it was "+before)) {
					t.Errorf("Ready is %+v, want the fixed verdict named at generation %d", r, seat.Generation)
				}
			})
		}
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

// TestUnrunnableExecutorIsAnnouncedOnce emits one Warning event on a Seat
// whose Ready turns to its executor's reason, which nothing on the Company
// says when the document applies, and none on the reconciles after that find
// it so again. While the Company is suspended the Seat's Ready says so instead, and
// the executor is announced once the Company resumes, not on every reconcile
// before.
func TestUnrunnableExecutorIsAnnouncedOnce(t *testing.T) {
	bridge := render.Executor{Field: executorField, Reason: crewletaiv1alpha1.ReasonNotExposed, Message: noBridge}
	announced := "Warning " + bridge.Reason + " " + executorField + ": no bridge."
	t.Run("seat", func(t *testing.T) {
		run, p := seatRun("")
		recorder := events.NewFakeRecorder(10)
		run.Recorder = recorder
		for range 3 {
			run.checkExecutors(map[string]render.Executor{seatAna: bridge})
			reconcileStatus(run, p)
		}
		if got := drained(recorder); !slices.Equal(got, []string{announced}) {
			t.Errorf("three reconciles announced %q, want %q once", got, announced)
		}
	})
	t.Run("seat of a suspended company", func(t *testing.T) {
		run, p := seatRun("")
		recorder := events.NewFakeRecorder(10)
		run.Recorder = recorder
		run.company.Spec.Suspend = new(true)
		for range 2 {
			run.checkExecutors(map[string]render.Executor{seatAna: bridge})
			reconcileStatus(run, p)
		}
		if got := drained(recorder); len(got) > 0 {
			t.Errorf("a suspended Company's seat, whose Ready says Suspended, is announced: %q", got)
		}
		run.company.Spec.Suspend = nil
		for range 2 {
			run.checkExecutors(map[string]render.Executor{seatAna: bridge})
			reconcileStatus(run, p)
		}
		if got := drained(recorder); !slices.Equal(got, []string{announced}) {
			t.Errorf("two reconciles after the Company resumed announced %q, want %q once", got, announced)
		}
	})
}

// TestExecutorFollowsTheEngineSource holds the operator's name for the tool
// bridge's address, the one variable whose absence makes the render report a
// seat's turns unrunnable, to the engine's (mcpbridge.BaseURLVar): a pod
// given another name would build no bridge for any box but a data pod's
// Direct ones, and every E2B run, and every Direct run on a seat pod, would
// fail while reported runnable.
func TestExecutorFollowsTheEngineSource(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	const file = "internal/api/mcpbridge/build.go"
	src, err := source.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found string
	ast.Inspect(parsed, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "BaseURLVar" || len(spec.Values) != 1 {
			return true
		}
		if lit, ok := spec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			found, _ = strconv.Unquote(lit.Value)
		}
		return false
	})
	if found == "" {
		t.Fatalf("%s declares no string constant BaseURLVar", file)
	}
	if found != envMCPBridgeURL {
		t.Errorf("the engine reads its tool bridge's address from %s, the operator sets %s", found, envMCPBridgeURL)
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
