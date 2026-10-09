package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// Moving a seat to another pool is make-before-break. The engine runs a seat
// only on a pod whose labels match its pin, so a revision that pins a seat to
// a pool whose pods have not started leaves it held by nobody for as long as
// they take: scheduling, an image pull, the start. Seat pods are created only
// once the data pods are ready, and a new one claims nothing until a data
// node reports its copy current. So the config reconciler keeps a moving
// seat pinned where the revision the engine runs pins it, and moves the pin
// with the first revision written after the StatefulSet of its new pool has a
// ready pod. The pods it leaves stay as long as that revision pins it to them
// (status.config.placement), and the engine moves the seat when the new
// revision is applied: one release and one claim, a turn boundary apart.

// placementHoldInterval is how soon the config reconciler looks again while
// it holds a seat's pin back for its new pool's pods. A seat pod is ready as
// soon as its container runs, so the hold lasts as long as the pod takes to
// start; five seconds is the engine's own claim sweep, so a look this often
// adds at most one sweep to the move.
const placementHoldInterval = 5 * time.Second

// holdMoves keeps each agent seat that moves to a pool with no ready pod
// pinned where the engine runs it, re-rendering the document with those pins
// when there is any. active is the engine's document, nil when it has none:
// a seat the engine does not run yet has nowhere to stay.
func (run *configRun) holdMoves(ctx context.Context, active *engine.ActiveConfig) error {
	if active == nil {
		return nil
	}
	running, err := render.RolePlacements(active.Document)
	if err != nil {
		return err
	}
	next, err := render.RolePlacements(run.doc.YAML)
	if err != nil {
		return err
	}
	held := map[string]map[string]string{}
	for _, p := range run.parts {
		seat, ok := p.obj.(*crewletaiv1alpha1.Seat)
		if !ok || seat.Spec.Kind == crewletaiv1alpha1.SeatKindHuman || p.handleTaken {
			continue
		}
		handle := render.SeatHandle(seat)
		was, runs := running[handle]
		if !runs || maps.Equal(was, next[handle]) {
			continue
		}
		pool := render.SeatPool(run.company, seat)
		workload := seatWorkloadName(run.company, pool, handle)
		ready, err := run.workloadReady(ctx, workload)
		if err != nil {
			return err
		}
		if ready {
			continue
		}
		held[handle] = was
		p.moving = fmt.Sprintf("The seat moves to pool %s, whose StatefulSet %s has no ready pod yet: it keeps running "+
			"where the revision the engine runs pins it (%s), and a revision that moves it is applied once a pod of %s is "+
			"ready, so that it is never pinned to pods that are not there.", pool, workload, labelsText(was), workload)
	}
	if len(held) == 0 {
		return nil
	}
	in := run.input()
	in.HeldPlacements = held
	doc, err := render.Company(in)
	if err != nil {
		// The same objects rendered a moment ago; only pins differ.
		return fmt.Errorf("rendering the document of Company %s/%s with its moving seats held: %w",
			run.company.Namespace, run.company.Name, err)
	}
	rendered, err := revisionPlacement(doc.YAML)
	if err != nil {
		return err
	}
	run.doc, run.rendered, run.holding = doc, &rendered, true
	logf.FromContext(ctx).V(1).Info("Holding moving seats where they run until their new pool has a ready pod",
		"seats", slices.Sorted(maps.Keys(held)))
	return nil
}

// workloadReady reports whether the Company's StatefulSet of that name has a
// ready pod.
func (run *configRun) workloadReady(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	sts := &appsv1.StatefulSet{}
	err := run.Get(ctx, client.ObjectKey{Namespace: run.company.Namespace, Name: name}, sts)
	switch {
	case apierrors.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("reading StatefulSet %s/%s: %w", run.company.Namespace, name, err)
	}
	return metav1.IsControlledBy(sts, run.company) && sts.Status.ReadyReplicas > 0, nil
}

// labelsText is a pin's labels, for a message.
func labelsText(labels map[string]string) string {
	pairs := make([]string, 0, len(labels))
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		pairs = append(pairs, k+"="+labels[k])
	}
	return strings.Join(pairs, ", ")
}

// holdResync looks again soon while a seat's pin is held back.
func (run *configRun) holdResync(result ctrl.Result) ctrl.Result {
	if run.holding && (result.RequeueAfter == 0 || result.RequeueAfter > placementHoldInterval) {
		result.RequeueAfter = placementHoldInterval
	}
	return result
}
