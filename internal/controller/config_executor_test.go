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

// executorHead is the field of a Seat that names the head of its executor's
// chain, and agentProvider the LLMProvider in mode Agent there.
const (
	executorHead  = "spec.llm.default[0].name"
	agentProvider = "claude-agent"
)

// nowhere is a name no object or pool of the specs' companies has, and
// noBridge the message of an executor the render finds unrunnable.
const (
	nowhere  = "nowhere"
	noBridge = "no bridge"
)

// cliModel is the model of the specs' CLIAgent providers, which no CLI is
// ever asked for.
const cliModel = "sonnet"

// TestExecutorVerdictFollowsTheRender holds an agent Seat's Ready to what the
// render found about its executor before what the fleet says: a node holding
// the lease of a seat the engine refuses every turn of is no sign that it
// runs, so the seat is not Ready, and the message still names the node. A
// seat whose executor can run is Ready as its lease says.
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
			name:     "bridge unsupported, held",
			executor: &render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported, Message: noBridge},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported,
			says:     []string{executorHead + ": no bridge.", "Engine node " + fleetWorkload + "-0 holds the seat."},
		},
		{
			// The fleet was not read: the node last reported stands.
			name:     "bridge unsupported, fleet not read",
			executor: &render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported, Message: noBridge},
			lastNode: fleetWorkload + "-1",
			wantNode: fleetWorkload + "-1",
			want:     crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported,
			says:     []string{executorHead + ": no bridge."},
		},
		{
			name:     "not exposed, held by nobody",
			executor: &render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonNotExposed, Message: "no address"},
			fleet:    &engine.Fleet{},
			want:     crewletaiv1alpha1.ReasonNotExposed,
			says:     []string{"No engine node holds the seat."},
		},
		{
			name:     "runnable, held",
			executor: &render.Executor{Field: executorHead, Message: "runs"},
			fleet:    held,
			wantNode: fleetWorkload + "-0",
			want:     crewletaiv1alpha1.ReasonLeaseHeld,
		},
		{
			name:     "no agent-mode executor, held",
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
// longer finds is not kept as what the engine last reported, while one a
// reconcile that did not render cannot judge stands.
func TestFixedExecutorVerdictIsNotKept(t *testing.T) {
	for _, c := range []struct {
		name     string
		rendered bool
		want     string
	}{
		{name: "rendered", rendered: true, want: crewletaiv1alpha1.ReasonUnplaced},
		{name: "not rendered", want: crewletaiv1alpha1.ReasonNotExposed},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, p := seatRun("")
			seat := p.obj.(*crewletaiv1alpha1.Seat)
			meta.SetStatusCondition(&seat.Status.Conditions, metav1.Condition{
				Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: crewletaiv1alpha1.ReasonNotExposed,
				ObservedGeneration: 2,
			})
			run.objectsChecked = c.rendered
			if c.rendered {
				p.executor = &render.Executor{Field: executorHead, Message: "runs"}
			}
			run.decideReady(p, seat)
			r := meta.FindStatusCondition(seat.Status.Conditions, crewletaiv1alpha1.ConditionReady)
			if r == nil || r.Reason != c.want {
				t.Fatalf("Ready is %+v, want %s", r, c.want)
			}
			if c.rendered && (r.ObservedGeneration != seat.Generation || !strings.Contains(r.Message, "it was NotExposed")) {
				t.Errorf("Ready is %+v, want the fixed verdict named at generation %d", r, seat.Generation)
			}
		})
	}
}

// agentModeRun is a reconcile of one accepted Company whose one LLMProvider,
// agentProvider, reported its agent mode runnable at generation 1 and is now
// at generation 2.
func agentModeRun() (*configRun, *part, *events.FakeRecorder) {
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany}
	meta.SetStatusCondition(&company.Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid,
	})
	provider := &crewletaiv1alpha1.LLMProvider{Namespace: "ns", Name: agentProvider, Generation: 2}
	provider.Status.Conditions = []metav1.Condition{{
		Type: crewletaiv1alpha1.ConditionAgentModeRunnable, Status: metav1.ConditionTrue,
		Reason: crewletaiv1alpha1.ReasonToolBridgeServed, ObservedGeneration: 1,
	}}
	p := newPart(provider)
	recorder := events.NewFakeRecorder(10)
	run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Recorder: recorder}, company: company, parts: []*part{p}}
	return run, p, recorder
}

// TestAgentModeRunnableFollowsTheRender holds an LLMProvider's
// AgentModeRunnable to what the render found, set at the provider's
// generation by a reconcile that rendered, left as it was by one that did
// not, and removed from a provider that is no CLI in mode Agent.
func TestAgentModeRunnableFollowsTheRender(t *testing.T) {
	agentMode := func(p *part) *metav1.Condition {
		return meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.LLMProvider).Status.Conditions,
			crewletaiv1alpha1.ConditionAgentModeRunnable)
	}
	t.Run("no render", func(t *testing.T) {
		run, p, _ := agentModeRun()
		run.decide(p, nil, nil)
		if c := agentMode(p); c == nil || c.ObservedGeneration != 1 {
			t.Errorf("AgentModeRunnable is %+v, want the last report left as it was", c)
		}
	})
	t.Run("not exposed", func(t *testing.T) {
		run, p, _ := agentModeRun()
		run.objectsChecked = true
		run.checkExecutors(nil, map[string]render.AgentMode{agentProvider: {Reason: crewletaiv1alpha1.ReasonNotExposed, Message: "No address"}})
		run.decide(p, nil, nil)
		c := agentMode(p)
		if c == nil || c.Status != metav1.ConditionFalse || c.Reason != crewletaiv1alpha1.ReasonNotExposed ||
			c.ObservedGeneration != 2 || c.Message != "No address." {
			t.Errorf("AgentModeRunnable is %+v, want False %s at generation 2", c, crewletaiv1alpha1.ReasonNotExposed)
		}
		applied := meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.LLMProvider).Status.Conditions, crewletaiv1alpha1.ConditionApplied)
		if p.hasProblem() || (applied != nil && applied.Status == metav1.ConditionFalse) {
			t.Errorf("the provider holds the document: Applied is %+v", applied)
		}
	})
	t.Run("no agent mode", func(t *testing.T) {
		run, p, _ := agentModeRun()
		run.objectsChecked = true
		run.checkExecutors(nil, map[string]render.AgentMode{})
		run.decide(p, nil, nil)
		if c := agentMode(p); c != nil {
			t.Errorf("AgentModeRunnable is %+v on a provider in mode Text", c)
		}
	})
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
// whose Ready turns to its executor's reason, and on an LLMProvider whose
// AgentModeRunnable turns False, which nothing on the Company says when the
// document applies, and none on the reconciles after that find them so
// again. While the Company is suspended the Seat's Ready says so instead, and
// the executor is announced once the Company resumes, not on every reconcile
// before.
func TestUnrunnableExecutorIsAnnouncedOnce(t *testing.T) {
	bridge := render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported, Message: noBridge}
	announced := "Warning " + bridge.Reason + " " + executorHead + ": no bridge."
	t.Run("seat", func(t *testing.T) {
		run, p := seatRun("")
		recorder := events.NewFakeRecorder(10)
		run.Recorder = recorder
		for range 3 {
			run.checkExecutors(map[string]render.Executor{seatAna: bridge}, nil)
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
			run.checkExecutors(map[string]render.Executor{seatAna: bridge}, nil)
			reconcileStatus(run, p)
		}
		if got := drained(recorder); len(got) > 0 {
			t.Errorf("a suspended Company's seat, whose Ready says Suspended, is announced: %q", got)
		}
		run.company.Spec.Suspend = nil
		for range 2 {
			run.checkExecutors(map[string]render.Executor{seatAna: bridge}, nil)
			reconcileStatus(run, p)
		}
		if got := drained(recorder); !slices.Equal(got, []string{announced}) {
			t.Errorf("two reconciles after the Company resumed announced %q, want %q once", got, announced)
		}
	})
	t.Run("provider", func(t *testing.T) {
		exposed := render.AgentMode{Reason: crewletaiv1alpha1.ReasonNotExposed, Message: "No address"}
		run, p, recorder := agentModeRun()
		run.objectsChecked = true
		for range 3 {
			run.checkExecutors(nil, map[string]render.AgentMode{agentProvider: exposed})
			reconcileStatus(run, p)
		}
		if want := "Warning " + exposed.Reason + " No address."; !slices.Equal(drained(recorder), []string{want}) {
			t.Errorf("three reconciles did not announce %q once", want)
		}
	})
}

// TestExecutorFollowsTheEngineSource holds the operator's name for the tool
// bridge's address, the one variable whose absence makes the render report a
// seat's turns unrunnable, to the pinned engine's (mcpbridge.BaseURLVar): a
// pod given another name would serve no bridge either, and every agent-mode
// seat on the data pods would fail while reported runnable.
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
