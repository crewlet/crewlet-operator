package controller

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

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
		want     string
		says     []string
	}{
		{
			name:     "bridge unsupported, held",
			executor: &render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported, Message: "no bridge"},
			fleet:    held,
			want:     crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported,
			says:     []string{executorHead + ": no bridge.", "Engine node " + fleetWorkload + "-0 holds the seat."},
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
			want:     crewletaiv1alpha1.ReasonLeaseHeld,
		},
		{
			name:  "no agent-mode executor, held",
			fleet: held,
			want:  crewletaiv1alpha1.ReasonLeaseHeld,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, p := seatRun("")
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
			if c.fleet == held && p.obj.(*crewletaiv1alpha1.Seat).Status.Placement.Node != fleetWorkload+"-0" {
				t.Errorf("status.placement.node is %q, want the node that holds the seat", p.obj.(*crewletaiv1alpha1.Seat).Status.Placement.Node)
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

// TestUnrunnableExecutorIsAnnouncedOnce emits one Warning event on a Seat
// whose turns turn unrunnable, and on an LLMProvider whose agent mode does,
// which nothing on the Company says when the document applies, and none on
// the reconciles after that find them so again.
func TestUnrunnableExecutorIsAnnouncedOnce(t *testing.T) {
	bridge := render.Executor{Field: executorHead, Reason: crewletaiv1alpha1.ReasonAgentModeBridgeUnsupported, Message: "no bridge"}
	run, p := seatRun("")
	recorder := events.NewFakeRecorder(10)
	run.Recorder = recorder
	run.checkExecutors(map[string]render.Executor{seatAna: bridge}, nil)
	select {
	case e := <-recorder.Events:
		if want := "Warning " + bridge.Reason + " " + executorHead + ": no bridge."; e != want {
			t.Errorf("the event is %q, want %q", e, want)
		}
	default:
		t.Error("no event announces the seat whose turns turned unrunnable")
	}
	meta.SetStatusCondition(&p.base.(*crewletaiv1alpha1.Seat).Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionReady, Status: metav1.ConditionFalse, Reason: bridge.Reason,
	})
	run.checkExecutors(map[string]render.Executor{seatAna: bridge}, nil)
	select {
	case e := <-recorder.Events:
		t.Errorf("the Seat already reported %s, and is announced again: %q", bridge.Reason, e)
	default:
	}

	exposed := render.AgentMode{Reason: crewletaiv1alpha1.ReasonNotExposed, Message: "No address"}
	run, _, recorder = agentModeRun()
	run.checkExecutors(nil, map[string]render.AgentMode{agentProvider: exposed})
	select {
	case e := <-recorder.Events:
		if want := "Warning " + exposed.Reason + " No address."; e != want {
			t.Errorf("the event is %q, want %q", e, want)
		}
	default:
		t.Error("no event announces the provider whose agent mode turned unrunnable")
	}
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
