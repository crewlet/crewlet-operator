package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// sandboxHead is the field of a Seat that names the head of its sandbox
// chain.
const sandboxHead = "spec.llm.sandbox[0].name"

// codeWorkRun is a reconcile of one accepted Company whose one agent Seat,
// ana, reported its code work unrunnable at generation 1 and is now at
// generation 2.
func codeWorkRun() (*configRun, *part, *events.FakeRecorder) {
	company := &crewletaiv1alpha1.Company{Namespace: "ns", Name: fleetCompany}
	meta.SetStatusCondition(&company.Status.Conditions, metav1.Condition{
		Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid,
	})
	seat := &crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatAna, Generation: 2, Spec: crewletaiv1alpha1.SeatSpec{
		Sandbox: &crewletaiv1alpha1.SeatSandbox{},
	}}
	seat.Status.Conditions = []metav1.Condition{{
		Type: crewletaiv1alpha1.ConditionCodeWorkRunnable, Status: metav1.ConditionFalse,
		Reason: crewletaiv1alpha1.ReasonSecretValueEmpty, ObservedGeneration: 1,
	}}
	p := newPart(seat)
	recorder := events.NewFakeRecorder(10)
	run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{Recorder: recorder}, company: company, parts: []*part{p}}
	return run, p, recorder
}

// TestCodeWorkRunnableFollowsTheRender holds the Seat's CodeWorkRunnable to
// what the render found, and to nothing else: a reconcile that did not render
// (its Company not accepted, its class refused) leaves the last report
// standing rather than removing a condition nobody re-checked, while one that
// did sets it at the Seat's generation, or removes it from a Seat Crewlet
// offers no code work.
func TestCodeWorkRunnableFollowsTheRender(t *testing.T) {
	codeWork := func(p *part) *metav1.Condition {
		return meta.FindStatusCondition(p.obj.(*crewletaiv1alpha1.Seat).Status.Conditions, crewletaiv1alpha1.ConditionCodeWorkRunnable)
	}

	t.Run("no render", func(t *testing.T) {
		run, p, _ := codeWorkRun()
		run.decide(p, nil, nil)
		if c := codeWork(p); c == nil || c.Reason != crewletaiv1alpha1.ReasonSecretValueEmpty || c.ObservedGeneration != 1 {
			t.Errorf("CodeWorkRunnable is %+v, want the last report left as it was", c)
		}
	})
	t.Run("runnable", func(t *testing.T) {
		run, p, _ := codeWorkRun()
		run.objectsChecked = true
		run.checkCodeWork(map[string]render.CodeWork{seatAna: {Field: sandboxHead, Message: "runs it"}})
		run.decide(p, nil, nil)
		c := codeWork(p)
		if c == nil || c.Status != metav1.ConditionTrue || c.Reason != crewletaiv1alpha1.ReasonCodeWorkSupported ||
			c.ObservedGeneration != 2 || c.Message != sandboxHead+": runs it." {
			t.Errorf("CodeWorkRunnable is %+v, want True %s at generation 2", c, crewletaiv1alpha1.ReasonCodeWorkSupported)
		}
	})
	t.Run("no code work", func(t *testing.T) {
		run, p, _ := codeWorkRun()
		run.objectsChecked = true
		run.checkCodeWork(map[string]render.CodeWork{})
		run.decide(p, nil, nil)
		if c := codeWork(p); c != nil {
			t.Errorf("CodeWorkRunnable is %+v on a Seat Crewlet offers no code work", c)
		}
	})
}

// TestUnrunnableCodeWorkIsAnnouncedOnce emits one Warning event on the Seat
// when its code work turns unrunnable, which nothing on the Company says when
// the document applies, and none on the reconciles after that find it so
// again.
func TestUnrunnableCodeWorkIsAnnouncedOnce(t *testing.T) {
	login := render.CodeWork{Field: sandboxHead, Reason: crewletaiv1alpha1.ReasonSandboxLoginNotPortable, Message: "cannot sign in"}
	run, _, recorder := codeWorkRun()
	run.checkCodeWork(map[string]render.CodeWork{seatAna: login})
	select {
	case e := <-recorder.Events:
		if want := "Warning " + crewletaiv1alpha1.ReasonSandboxLoginNotPortable + " " + sandboxHead + ": cannot sign in."; e != want {
			t.Errorf("the event is %q, want %q", e, want)
		}
	default:
		t.Error("no event announces the code work that turned unrunnable")
	}

	unchanged := render.CodeWork{Field: sandboxHead, Reason: crewletaiv1alpha1.ReasonSecretValueEmpty, Message: "holds nothing"}
	run, _, recorder = codeWorkRun()
	run.checkCodeWork(map[string]render.CodeWork{seatAna: unchanged})
	select {
	case e := <-recorder.Events:
		t.Errorf("the Seat already reported %s, and is announced again: %q", unchanged.Reason, e)
	default:
	}
}
