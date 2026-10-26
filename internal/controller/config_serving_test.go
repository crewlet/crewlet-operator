package controller

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// These tests hold the secret sync to every revision an engine pod runs or
// starts on, not only the fleet's: a pod that refused the fleet's revision
// runs the one before it, resolving its references from what every apply
// attempt reads of the store, and a data pod starts on the revision its
// store names, which it does not once a value that revision requires is gone
// from the store; while a pod that runs no company and starts on none needs
// only the fleet's revision. A pod the fleet does not list keeps every value
// wherever it may need one, nothing the operator saw of it before standing
// for what it runs or starts on now: a data pod always, a seat pod while its
// engine runs (config_serving.go).

// secondDataPod and thirdDataPod are the Company's data pods beside
// storeNode, and seatPod a pod of its Shared pool general, whose store is
// scratch (render.SeatPodBootstrap), so it starts on nothing every time.
const (
	secondDataPod = "acme-data-1"
	thirdDataPod  = "acme-data-2"
	seatPod       = "acme-seats-general-0"
)

// servingStatefulSet is a StatefulSet of the withdrawal's Company named name,
// asking for replicas pods, which the Company controls unless foreign; one
// named for a seat pool carries the seats' component label, as
// seatWorkloadLabels writes it.
func servingStatefulSet(w *withdrawal, name string, replicas int32, foreign bool) *appsv1.StatefulSet {
	sts := &appsv1.StatefulSet{}
	sts.Namespace, sts.Name = w.run.company.Namespace, name
	component := componentData
	if strings.HasPrefix(name, w.run.company.Name+"-seats-") {
		component = componentSeats
	}
	sts.Labels = labelsFor(w.run.company, component)
	owner := metav1.NewControllerRef(w.run.company, crewletaiv1alpha1.GroupVersion.WithKind(render.KindCompany))
	if foreign {
		owner.UID = "somebody-else"
	}
	sts.OwnerReferences = []metav1.OwnerReference{*owner}
	sts.Spec.Replicas = &replicas
	return sts
}

// The states keepServing reads an engine container in: running, and waiting
// to be started again after it exited, as a pod in CrashLoopBackOff does.
var (
	engineRunning      = &corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	engineCrashLooping = &corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
)

// sidecar is the container an engine pod of these tests runs beside the
// engine's, whose state says nothing of the engine's.
const sidecar = "sidecar"

// enginePodObject is the Pod name of the withdrawal's Company, whose engine
// container is in state, beside a sidecar that runs whatever the engine does;
// with state nil it is Pending, scheduled nowhere, and reports no container.
func enginePodObject(w *withdrawal, name string, state *corev1.ContainerState) *corev1.Pod {
	pod := &corev1.Pod{}
	pod.Namespace, pod.Name = w.run.company.Namespace, name
	pod.Labels = labelsFor(w.run.company, componentData)
	if strings.HasPrefix(name, w.run.company.Name+"-seats-") {
		pod.Labels = labelsFor(w.run.company, componentSeats)
	}
	pod.Spec.Containers = []corev1.Container{{Name: engineContainer, Image: "crewlet"}, {Name: sidecar, Image: sidecar}}
	pod.Status.Phase = corev1.PodPending
	if state != nil {
		pod.Status.Phase = corev1.PodRunning
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: sidecar, State: *engineRunning},
			{Name: engineContainer, State: *state},
		}
	}
	return pod
}

// companyPods makes the withdrawal's Company's StatefulSets, its data pods'
// asking for dataPods pods, 1 when 0, and its Shared pool general's asking
// for one, and the Pods pods names, each with its engine container in that
// state, nil for a Pending pod.
func (w *withdrawal) companyPods(t *testing.T, dataPods int32, pods map[string]*corev1.ContainerState) {
	t.Helper()
	ctx := context.Background()
	for _, sts := range []*appsv1.StatefulSet{
		servingStatefulSet(w, "acme-data", max(dataPods, 1), false),
		servingStatefulSet(w, "acme-seats-general", 1, false),
	} {
		if err := w.cluster.Create(ctx, sts); err != nil {
			t.Fatal(err)
		}
	}
	for name, state := range pods {
		if err := w.cluster.Create(ctx, enginePodObject(w, name, state)); err != nil {
			t.Fatal(err)
		}
	}
}

// setPod puts the withdrawal's Company's Pod name in state, nil for a Pending
// pod, making it where it is not there, as its StatefulSet makes a deleted
// pod again; or deletes it, where gone.
func (w *withdrawal) setPod(t *testing.T, name string, state *corev1.ContainerState, gone bool) {
	t.Helper()
	ctx := context.Background()
	pod := &corev1.Pod{}
	err := w.cluster.Get(ctx, types.NamespacedName{Namespace: w.run.company.Namespace, Name: name}, pod)
	switch {
	case apierrors.IsNotFound(err) && gone:
		return
	case apierrors.IsNotFound(err):
		err = w.cluster.Create(ctx, enginePodObject(w, name, state))
	case err != nil:
	case gone:
		err = w.cluster.Delete(ctx, pod)
	default:
		pod.Status = enginePodObject(w, name, state).Status
		err = w.cluster.Status().Update(ctx, pod)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// What SecretsSynced says of an engine pod the fleet does not list that may
// need a value: one whose engine runs, and a data pod whose engine does not.
const (
	runsUnlisted = " (it runs while the engine's fleet does not list it)"
	downUnlisted = " (it is not running, and the engine's fleet does not say which revision its volume starts it on)"
)

// TestAPodThatRefusedTheFleetsRevisionKeepsWhatItRuns applies a document that
// lets go a value whose Secret is deleted, which the running revision r1
// requires, and has the engine's one pod refuse the new revision r2: it runs
// r1 still, and would not start again on it without the value, so the value
// stays in the store, the Company says why, and the reconcile looks again
// within heldPollInterval, as the one after it does while the pod runs r1
// still. Once the pod runs the fleet's revision, the next
// reconcile takes the value out, and activates the revision again so that the
// engine reads its store without it.
func TestAPodThatRefusedTheFleetsRevisionKeepsWhatItRuns(t *testing.T) {
	w := newWithdrawal(t, runningRequires)
	w.engine.afterApply = func(e *storeEngine) { e.serving = map[string]string{storeNode: "r1"} }
	result, err := w.enforced(t)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) || !w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want r2 applied and the registry kept for r1",
			got, w.engine.holds(w.registry))
	}
	if !slices.Equal(w.run.held, []string{w.registry}) {
		t.Errorf("the reconcile holds %v for a revision other than the fleet's, want the registry", w.run.held)
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > heldPollInterval {
		t.Errorf("the reconcile runs again after %v, want within %v while the store keeps a value for r1",
			result.RequeueAfter, heldPollInterval)
	}
	msg := w.run.secretsSynced.Message
	if !strings.Contains(msg, "revision r1, which engine pod "+storeNode+" still runs,") ||
		!strings.Contains(msg, "keeps the value it held for the 1") {
		t.Errorf("SecretsSynced says %q, not that the store keeps the registry for r1, which %s runs", msg, storeNode)
	}
	if recorded := drained(w.events); !told(recorded, "SecretKept") {
		t.Errorf("events %q: the reconcile ends with the value kept, and SecretKept is not told", recorded)
	}

	// The next reconcile finds the document applied, and the pod on r1
	// still: it changes nothing, and looks again within heldPollInterval
	// rather than at the drift check.
	w.again(t, w.statusWritten())
	if result, err = w.enforced(t); err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) || !w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want nothing changed while the pod runs r1",
			got, w.engine.holds(w.registry))
	}
	if w.run.resyncInterval() <= heldPollInterval || result.RequeueAfter <= 0 || result.RequeueAfter > heldPollInterval {
		t.Errorf("the reconcile in sync runs again after %v (its resync %v), want within %v while the pod runs r1",
			result.RequeueAfter, w.run.resyncInterval(), heldPollInterval)
	}

	// The pod applies the fleet's revision.
	w.engine.mu.Lock()
	w.engine.serving = nil
	w.engine.mu.Unlock()
	w.again(t, w.statusWritten())
	result, err = w.enforced(t)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2", "r3"}) || w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want it gone and r2 activated again as r3",
			got, w.engine.holds(w.registry))
	}
	if len(w.run.held) != 0 || result.RequeueAfter == heldPollInterval && heldPollInterval < w.run.resyncInterval() {
		t.Errorf("the reconcile holds %v and runs again after %v, want nothing held and no held poll", w.run.held, result.RequeueAfter)
	}
	if msg := w.run.secretsSynced.Message; strings.Contains(msg, "which engine pod "+storeNode+" still runs") {
		t.Errorf("SecretsSynced says %q, which keeps nothing for another revision", msg)
	}
}

// TestAPodKeepsWhatItBootsOn applies the same document where the engine's
// one pod applies the new revision r2 and its record of what it starts on
// still names r1, as the engine's does while it retries moving it: a restart
// then would start the pod on r1, which does not start without the value, so
// the value stays, and the Company says the pod boots on r1. Once the
// engine has moved what the pod starts on to r2, the next reconcile takes the
// value out and activates r2 again without it.
func TestAPodKeepsWhatItBootsOn(t *testing.T) {
	w := newWithdrawal(t, runningRequires)
	w.engine.afterApply = func(e *storeEngine) {
		e.serving = map[string]string{storeNode: "r2"}
		e.boots = map[string]string{storeNode: "r1"}
	}
	result, err := w.enforced(t)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) || !w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want r2 applied and the registry kept for r1, "+
			"which %s starts on", got, w.engine.holds(w.registry), storeNode)
	}
	if !slices.Equal(w.run.held, []string{w.registry}) || result.RequeueAfter <= 0 || result.RequeueAfter > heldPollInterval {
		t.Errorf("the reconcile holds %v and runs again after %v, want the registry within %v", w.run.held, result.RequeueAfter,
			heldPollInterval)
	}
	if msg := w.run.secretsSynced.Message; !strings.Contains(msg, "revision r1, which engine pod "+storeNode+" boots on,") {
		t.Errorf("SecretsSynced says %q, not that the store keeps the registry for r1, which %s boots on", msg, storeNode)
	}

	// The engine moves what the pod starts on.
	w.engine.mu.Lock()
	w.engine.boots = nil
	w.engine.mu.Unlock()
	w.again(t, w.statusWritten())
	if _, err := w.enforced(t); err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2", "r3"}) || w.engine.holds(w.registry) {
		t.Fatalf("the revisions are %v and the store holds the registry: %t, want it gone and r2 activated again as r3",
			got, w.engine.holds(w.registry))
	}
}

// TestAPodWhoseRevisionIsNotKnownKeepsEverything applies the same document
// where the operator cannot tell what a pod runs or starts on once the new
// revision is written: a seat pod or a data pod whose engine runs while the
// fleet does not list it, a data pod the fleet does not list whose engine is
// down, which starts on whichever revision it applied last, a node that has
// not said, a revision a pod runs or starts on that the engine cannot answer,
// and a fleet that cannot be read. Any revision may require the value then,
// and it stays; the Company says which pod keeps it, and names it once, as
// one whose revision it does not know.
func TestAPodWhoseRevisionIsNotKnownKeepsEverything(t *testing.T) {
	// unsaidNode is a node the fleet lists with the zero record.
	const unsaidNode = thirdDataPod
	for _, tc := range []struct {
		name  string
		after func(e *storeEngine)
		// dataPods and pods are the Company's data pods and Pods
		// (companyPods), made before the reconcile; before is what the
		// fleet says before the apply, where the default, one node on r1,
		// is not it; why is the reason SecretsSynced gives.
		dataPods int32
		pods     map[string]*corev1.ContainerState
		before   func(e *storeEngine)
		why      string
	}{
		{
			name:  "a seat pod that runs while the fleet does not list it",
			after: func(e *storeEngine) { e.serving = map[string]string{storeNode: "r2"} },
			pods:  map[string]*corev1.ContainerState{seatPod: engineRunning},
			why:   seatPod + runsUnlisted,
		},
		{
			name:     "a data pod that runs while the fleet does not list it",
			after:    func(e *storeEngine) { e.serving = map[string]string{storeNode: "r2"} },
			dataPods: 2,
			pods:     map[string]*corev1.ContainerState{secondDataPod: engineRunning},
			why:      secondDataPod + runsUnlisted,
		},
		{
			name:     "a data pod the fleet does not list that is down",
			after:    func(e *storeEngine) { e.serving = map[string]string{storeNode: "r2"} },
			dataPods: 2,
			pods:     map[string]*corev1.ContainerState{secondDataPod: engineCrashLooping},
			why:      secondDataPod + downUnlisted,
		},
		{
			name:   "a node that has not said which revision it runs",
			after:  func(e *storeEngine) { e.serving = map[string]string{storeNode: "r2", unsaidNode: ""} },
			before: func(e *storeEngine) { e.serving = map[string]string{storeNode: "r1", unsaidNode: ""} },
			why:    unsaidNode + " (it has not said which revision it runs or boots on)",
		},
		{
			name: "a revision a pod runs that the engine cannot answer",
			after: func(e *storeEngine) {
				e.serving = map[string]string{storeNode: "r1"}
				e.missing = map[string]bool{"r1": true}
			},
			why: storeNode + " (it runs revision r1, which the engine could not answer",
		},
		{
			name: "a revision a pod boots on that the engine cannot answer",
			after: func(e *storeEngine) {
				e.serving = map[string]string{storeNode: "r2"}
				e.boots = map[string]string{storeNode: "r1"}
				e.missing = map[string]bool{"r1": true}
			},
			why: storeNode + " (it boots on revision r1, which the engine could not answer",
		},
		{
			name:  "a fleet that cannot be read",
			after: func(e *storeEngine) { e.fleetDown = true },
			why:   "any engine pod (the engine's GET /fleet could not be read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWithdrawal(t, runningRequires)
			w.companyPods(t, tc.dataPods, tc.pods)
			if tc.before != nil {
				w.engine.mu.Lock()
				tc.before(w.engine)
				w.engine.mu.Unlock()
			}
			w.engine.afterApply = tc.after
			result, err := w.enforced(t)
			if err != nil {
				t.Fatal(err)
			}
			if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) || !w.engine.holds(w.registry) {
				t.Fatalf("the revisions are %v and the store holds the registry: %t, want r2 applied and the registry kept",
					got, w.engine.holds(w.registry))
			}
			if !slices.Equal(w.run.held, []string{w.registry}) || result.RequeueAfter > heldPollInterval {
				t.Errorf("the reconcile holds %v and runs again after %v, want the registry within %v",
					w.run.held, result.RequeueAfter, heldPollInterval)
			}
			msg := w.run.secretsSynced.Message
			if !strings.Contains(msg, tc.why) {
				t.Errorf("SecretsSynced says %q, not %q", msg, tc.why)
			}
			if pod, _, _ := strings.Cut(tc.why, " ("); strings.Count(msg, pod) != 1 {
				t.Errorf("SecretsSynced says %q, which names %s %d times, want once, as a pod whose revision is not known",
					msg, pod, strings.Count(msg, pod))
			}
		})
	}
}

// TestAPodThatCannotBeRunningHoldsNothingBack applies a document that lets go
// a value whose Secret is deleted, which the running revision r1 requires,
// beside a seat pod the fleet does not list whose engine is not running: one
// that never started, its pod Pending where no node has room for it, one
// whose pod is not there yet, and one whose engine waits to start again,
// whatever it ran before. It runs nothing, and starts on nothing, its store
// scratch, so the value goes in the same reconcile, as it does where every
// pod runs r2, and r2 activates again without it; nothing is held for the
// seat pod.
func TestAPodThatCannotBeRunningHoldsNothingBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		// pods are the Company's Pods: seatPod's, or none.
		pods map[string]*corev1.ContainerState
	}{
		{name: "a pod that never started", pods: map[string]*corev1.ContainerState{seatPod: nil}},
		{name: "a pod not created yet"},
		{name: "a pod that waits to start again", pods: map[string]*corev1.ContainerState{seatPod: engineCrashLooping}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWithdrawal(t, runningRequires)
			w.companyPods(t, 1, tc.pods)
			w.engine.afterApply = func(e *storeEngine) { e.serving = map[string]string{storeNode: "r2"} }
			if err := w.enforce(t); err != nil {
				t.Fatal(err)
			}
			if got := w.engine.ids(); w.engine.holds(w.registry) || !slices.Equal(got, []string{"r1", "r2", "r3"}) {
				t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it gone and r2 activated again "+
					"as r3: %s runs nothing and starts on nothing", w.engine.holds(w.registry), got, seatPod)
			}
			if len(w.run.held) != 0 || len(w.run.serving.unread) != 0 {
				t.Errorf("the reconcile holds %v, and does not know what %v run, want nothing: %s runs nothing",
					w.run.held, w.run.serving.unread, seatPod)
			}
			if msg := w.run.secretsSynced.Message; strings.Contains(msg, seatPod) {
				t.Errorf("SecretsSynced says %q, which keeps nothing for %s", msg, seatPod)
			}
		})
	}
}

// TestNothingTheOperatorSawStandsForWhatAnUnlistedPodNeeds follows a pod of
// the Company over the reconciles that find the fleet not listing it, the
// engine running r1, a revision that requires a value that r0, the revision
// before it, does not reference. The reconcile that applied r1 found the pod
// listed on r0, running it and starting on it, as the fleet lists a pod right
// after an apply that the pod has yet to poll for; or found it down, Pending
// where no node has room for it. The pod then applied r1, which moves what a
// data pod's volume starts it on to r1, and left the fleet: cut off from the
// coordination store while its container stays Running, taken down by a
// rolling restart, or crashing. Every later reconcile finds it unlisted, the
// first of them applying r3, which lets the value go, its Secret deleted. The
// pod may run r1, or start on it, which nothing the operator saw of it names,
// so at each the value stays, the Company names the pod once, as one whose
// revisions the operator does not know, and the reconcile looks again within
// heldPollInterval.
func TestNothingTheOperatorSawStandsForWhatAnUnlistedPodNeeds(t *testing.T) {
	// podAt is the pod at one later reconcile: its engine's state, nil for a
	// Pending pod, or gone, its Pod deleted and not made again yet; why is
	// what SecretsSynced says of it.
	type podAt struct {
		engine *corev1.ContainerState
		gone   bool
		why    string
	}
	cutOff := podAt{engine: engineRunning, why: runsUnlisted}
	crashing := podAt{engine: engineCrashLooping, why: downUnlisted}
	for _, tc := range []struct {
		name string
		// pod is the pod followed, and first its engine at the reconcile
		// that applied r1, nil for a Pending pod, the fleet listing it on
		// r0 where listed; later is the pod at each reconcile after.
		pod    string
		first  *corev1.ContainerState
		listed bool
		later  []podAt
	}{
		{name: "a seat pod listed on r0, then cut off", pod: seatPod, first: engineRunning, listed: true, later: []podAt{cutOff}},
		{name: "a data pod listed on r0, then cut off", pod: secondDataPod, first: engineRunning, listed: true, later: []podAt{cutOff}},
		{name: "a seat pod that was down, then cut off", pod: seatPod, later: []podAt{cutOff}},
		{
			// The engine stops a node whose connection to the coordination
			// store closes for good, and its pod waits to start again,
			// unlisted still.
			name: "a data pod listed on r0, cut off, then waiting to start again", pod: secondDataPod,
			first: engineRunning, listed: true, later: []podAt{cutOff, crashing},
		},
		{
			// Deleted, then made again where no node has room for it yet.
			name: "a data pod listed on r0, then taken down by a rolling restart", pod: secondDataPod,
			first: engineRunning, listed: true, later: []podAt{{gone: true, why: downUnlisted}, {why: downUnlisted}},
		},
		{
			name: "a data pod listed on r0 that crashes after applying r1", pod: secondDataPod,
			first: engineRunning, listed: true, later: []podAt{crashing, crashing},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningRequires)
			dataPods := int32(1)
			if tc.pod == secondDataPod {
				dataPods = 2
			}
			w.companyPods(t, dataPods, map[string]*corev1.ContainerState{tc.pod: tc.first})
			// r0 references neither value: what is kept for it keeps
			// nothing of the registry's.
			w.engine.mu.Lock()
			w.engine.revisions = append([]storeRevision{{id: "r0", document: []byte("company:\n  name: Acme\n")}},
				w.engine.revisions...)
			w.engine.serving = map[string]string{storeNode: "r1"}
			if tc.listed {
				w.engine.serving[tc.pod] = "r0"
			}
			running := w.engine.revisions[1].document
			w.engine.mu.Unlock()

			// The reconcile that applied r1, the document it renders, in
			// its look after the apply, which keeps r1's names: it knows
			// what every pod needs.
			desired := w.resolve(t)
			desired.Keep("r1", storeNamesIn(running)...)
			if err := w.run.keepServing(ctx, w.client, &desired); err != nil {
				t.Fatal(err)
			}
			if unread := desired.Unread(); len(unread) != 0 {
				t.Fatalf("the reconcile that applied r1 does not know what %q run, want every pod known", unread)
			}

			// The pod applies r1, and leaves the fleet.
			w.engine.mu.Lock()
			w.engine.serving = map[string]string{storeNode: "r1"}
			w.engine.mu.Unlock()
			w.engine.afterApply = func(e *storeEngine) { e.serving = map[string]string{storeNode: "r3"} }
			for i, at := range tc.later {
				w.setPod(t, tc.pod, at.engine, at.gone)
				w.again(t, w.statusWritten())
				result, err := w.enforced(t)
				if err != nil {
					t.Fatal(err)
				}
				if got := w.engine.ids(); !slices.Equal(got, []string{"r0", "r1", "r3"}) || !w.engine.holds(w.registry) {
					t.Fatalf("after reconcile %d the revisions are %v and the store holds the registry: %t, want r3 applied "+
						"and the registry kept: %s may run or start on r1", i+1, got, w.engine.holds(w.registry), tc.pod)
				}
				if !slices.Equal(w.run.held, []string{w.registry}) || result.RequeueAfter <= 0 || result.RequeueAfter > heldPollInterval {
					t.Errorf("reconcile %d holds %v and runs again after %v, want the registry within %v",
						i+1, w.run.held, result.RequeueAfter, heldPollInterval)
				}
				if msg := w.run.secretsSynced.Message; !strings.Contains(msg, tc.pod+at.why) || strings.Count(msg, tc.pod) != 1 {
					t.Errorf("reconcile %d: SecretsSynced says %q, which does not name %s once, as %q",
						i+1, msg, tc.pod, tc.pod+at.why)
				}
			}
		})
	}
}

// TestKeepServingJudgesAPodByWhatItCanBeRunning holds keepServing to what it
// keeps for each pod: from a node's row in the fleet, what it runs and what
// it starts on; for a pod the fleet does not list, every value wherever it may
// need one, which its Pod and its StatefulSet say. A pod whose engine runs
// keeps every value, and so does a data pod whose engine does not, Pending,
// waiting to start again or not created yet: it runs nothing, and starts on
// whichever revision it applied last. A seat pod whose engine does not run
// keeps nothing: it runs nothing and starts on nothing. (Nothing the fleet
// listed of a pod before changes that: see
// TestNothingTheOperatorSawStandsForWhatAnUnlistedPodNeeds.) A node with the
// zero record keeps every value; one that runs no company and starts on none
// keeps nothing; and a pod of a StatefulSet the Company does not control,
// which no fleet lists here, is none of its pods, and keeps nothing.
//
// The fleet's revision is r2, which the pod storeNode runs and starts on
// unless boots says otherwise, and r1 the revision before it.
func TestKeepServingJudgesAPodByWhatItCanBeRunning(t *testing.T) {
	for _, tc := range []struct {
		name string
		// dataPods and pods are the Company's data pods and Pods
		// (companyPods).
		dataPods int32
		pods     map[string]*corev1.ContainerState
		// serving, boots and runsNone are the fleet's rows, as the
		// storeEngine's fields of those names; serving defaults to
		// storeNode on r2.
		serving  map[string]string
		boots    map[string]string
		runsNone map[string]bool
		// served are the revisions other than r2 keepServing keeps,
		// holders the pods r1 is kept for, and unread the pods it does not
		// know of.
		served  []string
		holders revisionPods
		unread  []string
	}{
		{name: "a seat pod that never started", pods: map[string]*corev1.ContainerState{seatPod: nil}},
		{name: "a seat pod not created yet"},
		{name: "a seat pod that waits to start again", pods: map[string]*corev1.ContainerState{seatPod: engineCrashLooping}},
		{
			name:   "a seat pod that runs cut off from the fleet",
			pods:   map[string]*corev1.ContainerState{seatPod: engineRunning},
			unread: []string{seatPod},
		},
		{
			name:     "a data pod that runs cut off from the fleet",
			dataPods: 2,
			pods:     map[string]*corev1.ContainerState{secondDataPod: engineRunning},
			unread:   []string{secondDataPod},
		},
		{
			name:     "a data pod that never started",
			dataPods: 2,
			pods:     map[string]*corev1.ContainerState{secondDataPod: nil},
			unread:   []string{secondDataPod},
		},
		{
			name:     "a data pod that waits to start again",
			dataPods: 2,
			pods:     map[string]*corev1.ContainerState{secondDataPod: engineCrashLooping},
			unread:   []string{secondDataPod},
		},
		{
			name:     "a data pod not created yet",
			dataPods: 2,
			unread:   []string{secondDataPod},
		},
		{
			name:     "a node that runs the revision before the fleet's",
			dataPods: 2,
			serving:  map[string]string{storeNode: "r2", secondDataPod: "r1"},
			served:   []string{"r1"},
			holders:  revisionPods{runs: []string{secondDataPod}},
		},
		{
			name:    "a node that runs the fleet's revision and starts on the one before",
			boots:   map[string]string{storeNode: "r1"},
			served:  []string{"r1"},
			holders: revisionPods{boots: []string{storeNode}},
		},
		{
			name:     "a node that runs no company and starts on none",
			dataPods: 2,
			runsNone: map[string]bool{secondDataPod: true},
		},
		{
			name:     "a node that has not said",
			dataPods: 2,
			serving:  map[string]string{storeNode: "r2", secondDataPod: ""},
			unread:   []string{secondDataPod},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningRequires)
			w.companyPods(t, tc.dataPods, tc.pods)
			if err := w.cluster.Create(ctx, servingStatefulSet(w, "other-data", 1, true)); err != nil {
				t.Fatal(err)
			}
			w.engine.mu.Lock()
			w.engine.activateOther(w.run.doc.YAML)
			w.engine.serving = tc.serving
			if w.engine.serving == nil {
				w.engine.serving = map[string]string{storeNode: "r2"}
			}
			w.engine.boots, w.engine.runsNone = tc.boots, tc.runsNone
			w.engine.mu.Unlock()

			desired := w.resolve(t)
			desired.Keep("r2", storeNamesIn(w.run.doc.YAML)...)
			if err := w.run.keepServing(ctx, w.client, &desired); err != nil {
				t.Fatal(err)
			}
			if served := desired.Served(); !slices.Equal(served, tc.served) {
				t.Errorf("keepServing keeps the revisions %v beside the fleet's, want %v", served, tc.served)
			}
			if holders := w.run.serving.revisions["r1"]; !reflect.DeepEqual(holders, tc.holders) {
				t.Errorf("keepServing keeps r1 for %+v, want %+v", holders, tc.holders)
			}
			unread := make([]string, 0, len(desired.Unread()))
			for _, why := range desired.Unread() {
				pod, _, _ := strings.Cut(why, " ")
				unread = append(unread, pod)
			}
			if !slices.Equal(unread, tc.unread) {
				t.Errorf("keepServing does not know what %v run, want %v: %q", unread, tc.unread, desired.Unread())
			}
		})
	}
}

// TestAPodThatRunsNoCompanyHoldsNothingBack applies a document that lets go a
// value whose Secret is deleted, which the running revision r1 requires,
// beside a seat pod that refuses the new revision r2 and runs no company: the
// value goes in the same reconcile, as it does where every pod runs r2, and
// r2 activates again without it; nothing is held for the seat pod.
func TestAPodThatRunsNoCompanyHoldsNothingBack(t *testing.T) {
	w := newWithdrawal(t, runningRequires)
	if err := w.cluster.Create(context.Background(), servingStatefulSet(w, "acme-seats-general", 1, false)); err != nil {
		t.Fatal(err)
	}
	w.engine.afterApply = func(e *storeEngine) {
		e.serving = map[string]string{storeNode: "r2"}
		e.runsNone = map[string]bool{seatPod: true}
	}
	if err := w.enforce(t); err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); w.engine.holds(w.registry) || !slices.Equal(got, []string{"r1", "r2", "r3"}) {
		t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it gone and r2 activated again as r3: "+
			"%s runs no company and needs no revision but the fleet's", w.engine.holds(w.registry), got, seatPod)
	}
	if len(w.run.held) != 0 {
		t.Errorf("the reconcile holds %v for %s, which runs no company, want nothing", w.run.held, seatPod)
	}
	if msg := w.run.secretsSynced.Message; strings.Contains(msg, seatPod) {
		t.Errorf("SecretsSynced says %q, which keeps nothing for %s", msg, seatPod)
	}
}

// TestEnginePodsAreThePodsTheStatefulSetsAskFor holds enginePods to the pods
// of the Company's own StatefulSets, from the first ordinal through the larger
// of the replicas asked for and made: a pod being scaled away runs until it
// is gone; and to which of them start on nothing, a seat pool's.
func TestEnginePodsAreThePodsTheStatefulSetsAskFor(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningRequires)
	seats := servingStatefulSet(w, "acme-seats-general", 1, false)
	seats.Spec.Ordinals = &appsv1.StatefulSetOrdinals{Start: 3}
	seats.Status.Replicas = 2
	data := servingStatefulSet(w, "acme-data", 1, false)
	for _, sts := range []*appsv1.StatefulSet{seats, data, servingStatefulSet(w, "other-data", 1, true)} {
		if err := w.cluster.Create(ctx, sts); err != nil {
			t.Fatal(err)
		}
	}
	pods, err := w.run.enginePods(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]enginePod{
		storeNode: {}, "acme-seats-general-3": {scratch: true}, "acme-seats-general-4": {scratch: true},
	}
	if !maps.Equal(pods, want) {
		t.Errorf("the engine pods are %v, want %v", pods, want)
	}
}

// TestHeldSentenceSaysWhatOnlyAnotherRevisionKeeps holds what SecretsSynced
// says of the values a secret sync kept only for a revision other than the
// fleet's: each, and the pods that run it or boot on it, except the value of
// a deleted Secret, which the sentence of what is retained already gives with
// its reason; and nothing at all when nothing is held.
func TestHeldSentenceSaysWhatOnlyAnotherRevisionKeeps(t *testing.T) {
	deleted := secretref.Key{Namespace: "ns", Name: optionalSecret, Key: optionalKey}
	const dropped = "K8S_DROPPED"
	runs := map[string]revisionPods{"r1": {runs: []string{storeNode, secondDataPod}}}
	for _, tc := range []struct {
		name      string
		revisions map[string]revisionPods
		held      []string
		retained  []secretref.Key
		want      string
	}{
		{name: "nothing held", revisions: runs},
		{
			name: "a value the fleet's revision no longer references", revisions: runs, held: []string{dropped},
			want: " It keeps 1 value the revision the engine is to run no longer needs, for revision r1, which engine pods " +
				"acme-data-0, acme-data-1 still run, until no pod runs or boots on a revision that does: K8S_DROPPED.",
		},
		{
			name: "a revision pods run and another boots on", held: []string{dropped},
			revisions: map[string]revisionPods{"r1": {runs: []string{seatPod}, boots: []string{storeNode}}},
			want: " It keeps 1 value the revision the engine is to run no longer needs, for revision r1, which engine pod " +
				"acme-seats-general-0 still runs and engine pod acme-data-0 boots on, until no pod runs or boots on a " +
				"revision that does: K8S_DROPPED.",
		},
		{
			name: "the value of a deleted Secret beside it", revisions: runs,
			held: []string{dropped, secretsync.StoreName(deleted)}, retained: []secretref.Key{deleted},
			want: " It keeps 1 value the revision the engine is to run no longer needs, for revision r1, which engine pods " +
				"acme-data-0, acme-data-1 still run, until no pod runs or boots on a revision that does: K8S_DROPPED.",
		},
		{
			name: "only the value of a deleted Secret", revisions: runs, held: []string{secretsync.StoreName(deleted)},
			retained: []secretref.Key{deleted},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := &configRun{serving: &servingView{revisions: tc.revisions}, held: tc.held, retained: tc.retained}
			if got := run.heldSentence(); got != tc.want {
				t.Errorf("heldSentence says %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAHeldDocumentKeepsWhatAnotherPodRuns holds a reconcile whose next
// document cannot be applied, where the fleet's revision r1 lets go a value
// whose Secret is deleted, to a pod still running r0, which requires it: the
// value stays, nothing is activated, and the reconcile looks again within
// heldPollInterval; once the pod runs r1, and starts on it, the value goes and
// r1 is activated again without it.
func TestAHeldDocumentKeepsWhatAnotherPodRuns(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	w.engine.mu.Lock()
	w.engine.revisions = append([]storeRevision{{id: "r0", document: w.requiring}}, w.engine.revisions...)
	w.engine.serving = map[string]string{storeNode: "r1", secondDataPod: "r0"}
	w.engine.mu.Unlock()

	result, err := w.run.withdrawHeld(ctx, w.resolve(t))
	if err != nil {
		t.Fatal(err)
	}
	if !w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r0", "r1"}) {
		t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it kept for r0 and nothing activated",
			w.engine.holds(w.registry), w.engine.ids())
	}
	if !slices.Equal(w.run.held, []string{w.registry}) || result.RequeueAfter <= 0 || result.RequeueAfter > heldPollInterval {
		t.Errorf("the reconcile holds %v and runs again after %v, want the registry within %v", w.run.held, result.RequeueAfter,
			heldPollInterval)
	}
	if recorded := drained(w.events); !told(recorded, "SecretKept") || !slices.ContainsFunc(recorded, func(e string) bool {
		return strings.Contains(e, "revision r0, which engine pod acme-data-1 still runs,")
	}) {
		t.Errorf("events %q do not tell that acme-data-1 keeps the registry for r0", recorded)
	}

	w.engine.mu.Lock()
	w.engine.serving = map[string]string{storeNode: "r1", secondDataPod: "r1"}
	w.engine.mu.Unlock()
	if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
		t.Fatal(err)
	}
	if w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r0", "r1", "r3"}) {
		t.Fatalf("the store holds the registry: %t, and the revisions are %v, want it gone and r1 activated again",
			w.engine.holds(w.registry), w.engine.ids())
	}
	if len(w.run.held) != 0 {
		t.Errorf("the reconcile holds %v once every pod runs r1, want nothing", w.run.held)
	}
}
