package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// What the company's engine pods run and start on, which the secret sync
// keeps.
//
// The engine's GET /config answers the fleet's revision, the one its
// activation pointer names, and every pod answers the same. Each pod applies
// that revision on its own loop, and a pod that refuses it, or has not
// applied it yet, runs the revision it ran before. So the store keeps,
// beside what the fleet's revision references, what every other revision a
// pod RUNS or STARTS ON references (secretsync.Desired.KeepServed), for as
// long as a pod does, for two reasons. A pod resolves the revision it runs
// from the store: every attempt to apply a revision, refused or not, reads
// the store again into the snapshot the running revision resolves its
// ${VAR} references from, so a value gone from the store is gone from the
// pod at its next attempt, and what it starts afterwards, a seat's tool
// server on a later claim, say, finds nothing under that name. And a pod
// starts on the revision its own store names, which the engine reports
// apart from the one it runs (engine.ApplyRecord.Boots), because it moves
// it only once an apply has succeeded, with a local write it retries until
// it lands: a revision whose document requires a value the store no longer
// holds does not start, and a data pod that does not start is not one the
// operator can put the value back through.
//
// Only a data pod starts on a revision, its store being on its persistent
// volume. A seat pod's Tier A marks its store scratch
// (render.SeatPodBootstrap), which the engine reads nothing of at boot and
// deletes as it opens it, so a seat pod starts on nothing, even after a
// container restart that leaves its emptyDir holding what it wrote, and
// applies the fleet's revision, whose names Keep holds. A pod that runs no
// company and starts on none, which its row in the fleet says with an
// attempt recorded and neither revision named (a seat pod that refused the
// fleet's revision, a data pod that joined on an empty volume and refused
// it), needs nothing more of the store.
//
// What each pod runs and starts on is its row in the engine's GET /fleet,
// and the document of each revision is GET /config/revisions/{id}, which the
// node the operator reaches answers only while its own history holds it
// (engine.Client.Revision). The fleet lists a node only while it holds its
// presence lease, which a restarting pod has given up and a pod cut off
// from the coordination store loses, and nothing the operator saw of a pod
// before stands for what it runs or starts on once the fleet does not list
// it: a pod applies the fleet's revision on its own loop, and a data pod
// moves the revision its volume starts it on after every apply that
// succeeds, so one that applied a revision after the fleet last listed it
// and then went down (a rolling restart takes each pod down in turn) or was
// cut off runs, or starts on, a revision no listing named.
// So the operator remembers nothing of a pod, and judges one the fleet does
// not list by what it can be running, which its Pod object says. A data pod
// keeps every value (secretsync.Desired.KeepUnread) until the fleet lists it
// again, whether its engine container runs or not: running, it may run any
// revision the fleet named since it last listed the pod; not running
// (Pending, waiting, restarting, or not created yet), it runs nothing, and
// starts on whichever revision it applied last. A seat pool's pod keeps
// every value while its engine container runs, and nothing while it does
// not, when it runs nothing and starts on nothing.
//
// Every value is kept too for a node the fleet lists that has recorded no
// attempt yet, whose zero record says nothing of what it runs or starts on,
// for a revision the engine cannot answer, and for every pod while the fleet
// cannot be read; a pod whose revisions are unknown is named once, as such.
// That costs a deleted Secret's value lingering in the store while a pod is
// in doubt, which a pod that cannot start, or runs a revision without a value
// it requires, would cost far more than. So every delete waits for as long
// as a data pod is down, Pending or cut off from the fleet, which in
// v1alpha1 rarely shows: the one data pod is the node the operator reaches,
// so a fleet that answers at all lists it, and one that does not keeps every
// value anyway. A seat pod's Pod on a node whose kubelet stopped reporting
// stays Running, and holds every delete back until it is removed. And a seat
// pod that can be running nothing never costs it, so that one that never
// starts (no node has room for it, say) holds no delete back for as long as
// it waits.

// heldPollInterval is how soon the config reconciler looks at a Company's
// engine again while its secret store keeps a value only for a revision a
// pod runs or starts on other than the fleet's, or for a pod whose revisions
// it does not know: enginePollInterval, the longest a node goes before it
// reads the activation pointer again and applies the fleet's revision, after
// which a pod that can run it does, and starts on it, and the value goes.
// Nothing the operator watches says when that happens, nor when a pod the
// fleet does not list is listed again. A pod that refuses the fleet's
// revision for good keeps the value as long as it runs, and is looked at this
// often, as an engine that refused the operator's revision is
// (engineRetryInterval).
const heldPollInterval = enginePollInterval

// servingView is what keepServing found the company's engine pods run and
// start on: each revision other than the fleet's kept for a pod, with the
// pods that keep it, and unread, the pods whose revisions it does not know
// or could not read, each with why.
type servingView struct {
	revisions map[string]revisionPods
	unread    []string
}

// revisionPods are the engine pods a revision is kept for: those that run
// it, and those that start on it and run another revision or none, each
// sorted.
type revisionPods struct {
	runs, boots []string
}

// phrase says what the pods do with the revision, for a sentence that names
// it: "engine pod a still runs", "engine pods a, b boot on", or the two
// joined with "and".
func (p revisionPods) phrase() string {
	var parts []string
	if len(p.runs) > 0 {
		parts = append(parts, podsPhrase(p.runs)+" still "+plural(len(p.runs), "runs", "run"))
	}
	if len(p.boots) > 0 {
		parts = append(parts, podsPhrase(p.boots)+" "+plural(len(p.boots), "boots on", "boot on"))
	}
	return strings.Join(parts, " and ")
}

// enginePod is a pod of one of the Company's StatefulSets. scratch is a seat
// pool's pod, whose store the operator renders scratch
// (render.SeatPodBootstrap), so that it starts on nothing whatever it ran.
type enginePod struct {
	scratch bool
}

// keepServing keeps in desired, beside the fleet's revision that Keep named
// there, every other revision an engine pod of the company runs or starts
// on, as the fleet lists each; a pod that runs no company and starts on none
// keeps nothing of its own. A pod whose revisions it does not know, a pod the
// fleet does not list that may need a value among them (judgeUnlisted), and
// one whose revision the engine cannot answer, keep everything, as does a
// fleet it cannot read. Its error is the API server's.
func (run *configRun) keepServing(ctx context.Context, eng *engine.Client, desired *secretsync.Desired) error {
	pods, err := run.enginePods(ctx)
	if err != nil {
		return err
	}
	view := &servingView{revisions: map[string]revisionPods{}}
	run.serving = view
	// Read afresh rather than through fleetOf: what the pods run moves
	// while the reconcile applies a revision, and a later read is never
	// one that keeps less than the pods need. The seats' placement reads
	// the same answer after it.
	fleet, err := eng.Fleet(ctx)
	run.fleet, run.fleetErr = nil, err
	if err != nil {
		// No error: a fleet it cannot read keeps every value, as a pod
		// whose revisions are unknown does, and SecretsSynced says why.
		view.unread = []string{fleetUnread(err)}
		desired.KeepUnread(view.unread...)
		return nil
	}
	run.fleet = &fleet

	// records are what each node the fleet lists says it runs and starts
	// on: all that is kept for a pod, beside every value for a pod the
	// reconcile does not know of, which view.unread names.
	records := map[string]engine.ApplyRecord{}
	listed := map[string]bool{}
	var unsaid []string
	for i := range fleet.Nodes {
		node := &fleet.Nodes[i]
		listed[node.ID] = true
		if node.Apply.Status == "" {
			// The zero record: the node has recorded no attempt, so it
			// has not said what it runs or starts on, which a node just
			// started may be anything its volume names, and one whose
			// record nobody renews any longer anything it applied since.
			unsaid = append(unsaid, node.ID)
			continue
		}
		records[node.ID] = node.Apply
	}
	for _, node := range slices.Sorted(slices.Values(unsaid)) {
		view.unread = append(view.unread, node+" (it has not said which revision it runs or boots on)")
	}
	if err := run.judgeUnlisted(ctx, pods, listed, view); err != nil {
		return err
	}

	byRevision := map[string]revisionPods{}
	for _, node := range slices.Sorted(maps.Keys(records)) {
		// Empty is none: a node with both empty runs no company and
		// starts on none, and keeps nothing.
		record := records[node]
		if record.Serving != "" {
			p := byRevision[record.Serving]
			p.runs = append(p.runs, node)
			byRevision[record.Serving] = p
		}
		if record.Boots != "" && record.Boots != record.Serving {
			p := byRevision[record.Boots]
			p.boots = append(p.boots, node)
			byRevision[record.Boots] = p
		}
	}
	for _, revision := range slices.Sorted(maps.Keys(byRevision)) {
		if revision == desired.Running() {
			continue
		}
		holders := byRevision[revision]
		document, err := eng.Revision(ctx, revision)
		if err != nil {
			for _, node := range holders.runs {
				view.unread = append(view.unread, fmt.Sprintf("%s (it runs revision %s, which the engine could not answer: %v)",
					node, revision, err))
			}
			for _, node := range holders.boots {
				view.unread = append(view.unread, fmt.Sprintf("%s (it boots on revision %s, which the engine could not answer: %v)",
					node, revision, err))
			}
			continue
		}
		desired.KeepServed(revision, storeNamesIn(document.Document)...)
		view.revisions[revision] = holders
	}
	desired.KeepUnread(view.unread...)
	return nil
}

// judgeUnlisted names in view.unread each engine pod the fleet does not list
// (listed) that may need a value of the store, nothing the operator saw of it
// before standing for what it runs or starts on now: one whose engine
// container runs, which may run any revision, and a data pod whose engine
// container does not, which starts on whichever revision it applied last. A
// seat pool's pod whose engine container does not run needs nothing: it runs
// nothing and starts on nothing. Its error is the API server's.
func (run *configRun) judgeUnlisted(
	ctx context.Context, pods map[string]enginePod, listed map[string]bool, view *servingView,
) error {
	var unlisted []string
	for pod := range pods {
		if !listed[pod] {
			unlisted = append(unlisted, pod)
		}
	}
	if len(unlisted) == 0 {
		return nil
	}
	running, err := run.runningPods(ctx)
	if err != nil {
		return err
	}
	for _, pod := range slices.Sorted(slices.Values(unlisted)) {
		switch {
		case running[pod]:
			// Cut off from the fleet, or up again since a restart no
			// reconcile saw: it may run any revision the fleet named since
			// it last listed the pod, which no listing names.
			view.unread = append(view.unread, pod+" (it runs while the engine's fleet does not list it)")
		case pods[pod].scratch:
			// A seat pool's pod, down: it runs nothing, and starts on
			// nothing, its store scratch, whatever it ran before.
		default:
			// A data pod, down: it runs nothing, and starts on the
			// revision it applied last, which it may have applied after
			// any listing the operator saw, as a pod a rolling restart
			// takes down right after it applies the fleet's revision has.
			// It keeps every value as a running one does; the two are
			// told apart for whoever reads SecretsSynced, a pod that is
			// down and one cut off from the fleet being fixed apart.
			view.unread = append(view.unread,
				pod+" (it is not running, and the engine's fleet does not say which revision its volume starts it on)")
		}
	}
	return nil
}

// fleetUnread is what keepServing says of every engine pod while the engine's
// GET /fleet cannot be read, err being why.
func fleetUnread(err error) string {
	return "any engine pod (the engine's GET /fleet could not be read: " + err.Error() + ")"
}

// enginePods are the pods of the Company's StatefulSets, its data pods' and
// its seat pools', by the name the engine knows each node by: the pod's
// name. A pod counts from the moment its StatefulSet asks for it, created or
// not, to the moment it is scaled away, so one that restarts, and one that
// has yet to start, are among them.
func (run *configRun) enginePods(ctx context.Context) (map[string]enginePod, error) {
	list := &appsv1.StatefulSetList{}
	if err := run.List(ctx, list, client.InNamespace(run.company.Namespace),
		client.MatchingLabels{crewletaiv1alpha1.CompanyLabel: run.company.Name}); err != nil {
		return nil, fmt.Errorf("listing the StatefulSets of Company %s/%s: %w", run.company.Namespace, run.company.Name, err)
	}
	pods := map[string]enginePod{}
	for i := range list.Items {
		sts := &list.Items[i]
		if !metav1.IsControlledBy(sts, run.company) {
			continue
		}
		// A seat pool's StatefulSet is the one labelled as the seats'
		// component (seatWorkloadLabels); any other is taken for the
		// data pods', whose store outlives a restart, so that a pod of
		// one it cannot place is never let go of what it may start on.
		pod := enginePod{scratch: sts.Labels["app.kubernetes.io/component"] == componentSeats}
		// What the spec asks for and what the controller has made, the
		// larger: a pod being scaled away runs until it is gone.
		replicas := sts.Status.Replicas
		if sts.Spec.Replicas != nil {
			replicas = max(replicas, *sts.Spec.Replicas)
		}
		var start int32
		if sts.Spec.Ordinals != nil {
			start = sts.Spec.Ordinals.Start
		}
		for ordinal := start; ordinal < start+replicas; ordinal++ {
			pods[sts.Name+"-"+strconv.Itoa(int(ordinal))] = pod
		}
	}
	return pods, nil
}

// runningPods are the names of the Company's pods whose engine container
// runs, read from the API server rather than a cache, since the operator
// watches no Pod: a pod that is Pending, whose container waits or restarts,
// or that is not there at all, is not among them. A container that runs is
// one the kubelet last reported running, which a pod on a node that stopped
// reporting stays: kept as running, it keeps every value, whatever it may be
// running, until it is removed.
func (run *configRun) runningPods(ctx context.Context) (map[string]bool, error) {
	list := &corev1.PodList{}
	if err := run.APIReader.List(ctx, list, client.InNamespace(run.company.Namespace),
		client.MatchingLabels{crewletaiv1alpha1.CompanyLabel: run.company.Name}); err != nil {
		return nil, fmt.Errorf("listing the pods of Company %s/%s: %w", run.company.Namespace, run.company.Name, err)
	}
	running := map[string]bool{}
	for i := range list.Items {
		pod := &list.Items[i]
		for j := range pod.Status.ContainerStatuses {
			if status := &pod.Status.ContainerStatuses[j]; status.Name == engineContainer && status.State.Running != nil {
				running[pod.Name] = true
			}
		}
	}
	return running, nil
}

// heldResync is result, looking at the engine again within heldPollInterval
// while the last secret sync kept a value only for a revision a pod runs or
// starts on other than the fleet's, or for a pod whose revisions it does not
// know.
func (run *configRun) heldResync(result ctrl.Result) ctrl.Result {
	if len(run.held) == 0 {
		return result
	}
	if result.RequeueAfter == 0 || result.RequeueAfter > heldPollInterval {
		result.RequeueAfter = heldPollInterval
	}
	return result
}

// heldSentence is what the Company's SecretsSynced message says of the
// values the last secret sync kept only for a revision other than the
// fleet's, or for a pod whose revisions it does not know: nothing when there
// are none. The value of a deleted Secret among them is left to
// retainedSentence, which says which revision keeps it and why, and names its
// Secret key rather than its store name.
func (run *configRun) heldSentence() string {
	if run.serving == nil {
		return ""
	}
	held := slices.DeleteFunc(slices.Clone(run.held), func(name string) bool {
		return slices.ContainsFunc(run.retained, func(k secretref.Key) bool { return secretsync.StoreName(k) == name })
	})
	if len(held) == 0 {
		return ""
	}
	var why []string
	for _, revision := range slices.Sorted(maps.Keys(run.serving.revisions)) {
		// Runs, for a pod that resolves the revision's references from
		// what its every apply attempt reads of the store; boots on, for
		// one that does not start without them, a data pod alone, since a
		// seat pod starts on nothing whatever it ran. A pod that does both
		// is named as running it.
		why = append(why, fmt.Sprintf("revision %s, which %s", revision, run.serving.revisions[revision].phrase()))
	}
	if len(run.serving.unread) > 0 {
		why = append(why, "a revision the operator does not know of "+strings.Join(run.serving.unread, ", "))
	}
	return fmt.Sprintf(" It keeps %d %s the revision the engine is to run no longer needs, for %s, until no pod runs or "+
		"boots on a revision that does: %s.", len(held), plural(len(held), "value", "values"), strings.Join(why, "; and "),
		strings.Join(held, ", "))
}
