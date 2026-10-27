package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	policyv1ac "k8s.io/client-go/applyconfigurations/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// A company's seat pools run as StatefulSets of stateless engine pods, beside
// the data StatefulSet: one per Shared pool (<c>-seats-<pool>) and one per
// seat of a PerSeat pool (<c>-seat-<handle>). Each pod runs Crewlet with the
// seats role alone, joins the company's event stream as a leaf of the data
// pods, keeps on an emptyDir a scratch store, which the engine discards at
// every start, a container's restart included, and serves nothing but its
// probes. A node without the ingress role binds api.port for /health and
// /ready alone, and its /ready answers whether it is
// doing its work: linked to the data pods' broker, running the company
// document, holding its presence lease and admitted by its latest placement
// pass to claim seats. So a seat pod is ready once it has joined the fleet
// and may take seats, and that is what a rollout waits on before it replaces
// the next pod; minReadySeconds adds two placement passes on top
// (seatPodMinReadySeconds).
//
// A StatefulSet rather than a Deployment, though the pods hold nothing: the
// engine wants a node id that is distinct and stable across restarts, and a
// leaf needs a NATS server name; the pod's name gives both, and with no
// volume claim template the StatefulSet costs nothing more.
//
// The seat pods follow the data pods and never lead them. They are created,
// and a pool gains pods, only while every data pod is ready and runs the
// current template, since a leaf with no data pod to join gives up within a
// minute; they take a new template only then too, every seat StatefulSet in
// the same reconcile; and a suspended company stops them before its data
// pods, because a stopping seat pod hands its last records to a data pod.
const (
	// leafPortName names the data pods' leaf listener on the pods and the
	// headless data Service.
	leafPortName = "leaf"
	// seatPodMinReadySeconds is how long a seat pod must be ready before
	// it counts as available, and a rollout replaces the next one: two of
	// the engine's five-second placement passes. /ready turns 200 in the
	// pass that admits the pod to claim; its peers, which now count it
	// live, give back what is past their share of the pool's seats, and
	// the pod claims those in the passes after. Without the pause the
	// next pod would drain while the seats it holds still had one fewer
	// place to go.
	seatPodMinReadySeconds = 10
	// seatPodBootSeconds is every bound the engine puts on a seat pod's
	// boot but its object store's, laid end to end, each step waiting
	// after the one before it on the boot's one goroutine, read from
	// Crewlet's source (the mirrors in internal/render/mirror_test.go name
	// each declaration):
	//   - 60: its connection to its own leaf broker, a 30 s handshake and
	//     a 30 s flush that reads the broker's max_payload (listenTimeout);
	//   - 61: the leaf link to a data pod and the fleet's JetStream
	//     answering across it (clusterReadyTimeout, 60 s, whose deadline is
	//     checked before an ask of a second, clusterReadyAsk);
	//   - 300: the event stream's streams, looked up or created across the
	//     link, which counts as clustered (clusterSequenceBudget);
	//   - 60: a second connection, for coordination (listenTimeout twice);
	//   - 300: the coordination buckets, a window of their own
	//     (clusterSequenceBudget);
	//   - 90: the fleet's record of its object store (AgreeObjectBackend):
	//     eight rounds of a write and a read at the client's 5 s default and
	//     a read after them, begun again within 5 s; a boot meets more than
	//     three of those requests only if something removes the record
	//     while it runs, but the rounds are the engine's bound;
	//   - 61: the lock on the store's native library, which a container
	//     killed while extracting it leaves on the scratch volume a restart
	//     keeps (tursoPrepareTimeout, a minute, polled every 25 ms);
	//   - 20: two registrations of what the node answers, 10 s each
	//     (flushBudget).
	// Its object store's step is S3's probe (s3ProbeSeconds) or the one
	// below. Nothing on a seat pod's boot waits on what the company
	// configures or grows with what it holds: the engine lists the
	// company's secrets after /health answers 200.
	seatPodBootSeconds = 60 + 61 + 300 + 60 + 300 + 90 + 61 + 20
	// seatPodNATSObjectsSeconds is the object store's step on the default
	// backend: the broker's limit read to size the bucket (LookupBudget,
	// 30 s), then the bucket looked up (30 s), created (2 min, Budget),
	// read back (5 s, ReadBack) and bound (5 s), inside a window of five
	// minutes that never binds them (natsobj.Open). A bucket given its
	// ceiling by store.objects.nats_max_bytes would trade the limit read
	// for a 2 min update, but the render never sets it.
	seatPodNATSObjectsSeconds = 30 + 30 + 120 + 5 + 5
)

// seatPodStartupSeconds is how long a seat pod's engine may take to answer
// /health 200, which it does once it has recovered what it holds (503
// before, from the moment it binds): every bound the engine puts on a leaf's
// boot laid end to end, its object store's by the class's backend. Past any
// of them the engine exits by itself (or, past the lock, the listing or the
// limit read, carries on without them), so the kubelet never ends a boot the
// engine would finish. Most of it waits on a data pod: a seat pod started
// while the data pod is down or recovering waits the whole of it.
func seatPodStartupSeconds(class *crewletaiv1alpha1.EngineClass) int32 {
	if class.Spec.Objects.Backend == crewletaiv1alpha1.ObjectBackendS3 {
		return seatPodBootSeconds + s3ProbeSeconds
	}
	return seatPodBootSeconds + seatPodNATSObjectsSeconds
}

// seatWorkload is one StatefulSet of seat pods.
type seatWorkload struct {
	name string
	// pool is the pool's name, and seat the handle of a PerSeat pool's
	// seat (empty for a Shared pool).
	pool, seat string
	// plan is how the pool runs its pods: as the Company's spec plans it,
	// for a StatefulSet the plan wants; as it was planned when the operator
	// last wrote the StatefulSet (SeatPoolAnnotation), for one kept only
	// because the running revision pins a seat to it.
	plan *render.PoolPlan
	// kept is a StatefulSet the plan does not want, which the running
	// revision pins a seat to.
	kept bool
	// existing is the StatefulSet as it is, or nil.
	existing *appsv1.StatefulSet
	// status is the StatefulSet's status after the reconcile: as the apply
	// answered, or as it was.
	status appsv1.StatefulSetStatus
	// blocked is an existing StatefulSet that keeps an older template while
	// the data pods roll; pending one not created yet, while they are not
	// ready or while the Company is refused for its pools.
	blocked, pending bool
}

// replicas is how many pods the StatefulSet runs, and whether it may be
// created: what the plan sizes it at, unless the Company's pools are frozen
// (refused for passing the class's maxPods) or it is kept, when it stays at
// the size it is, as it was before a suspension stopped it.
func (w *seatWorkload) replicas(frozen bool) (int32, bool) {
	if !w.kept && !frozen {
		return w.plan.Replicas, true
	}
	if w.existing == nil {
		return 0, false
	}
	return restoredReplicas(w.existing), true
}

// secretName is the Secret the workload's pods start from: one per pool and
// mode, since a PerSeat pool's seats share their document and a Shared pool
// of the same name, which a revision can still pin a seat to after the pool
// changed mode, does not.
func (w *seatWorkload) secretName(c *crewletaiv1alpha1.Company) string {
	return poolBootstrapName(c, w.pool, w.plan.Mode)
}

// seatPlan is what a Company's seat pools should be, against what they are.
type seatPlan struct {
	// topology is the plan render made.
	topology *render.Topology
	// allowed is whether the class allows seat pools: while it does not,
	// the operator starts no seat pod, and keeps only those the revision the
	// engine runs pins a seat to, for which the data pods keep their
	// listener open (render.LeafOpen).
	allowed bool
	// refusal is the Company's own refusal (its Shared pools pass maxPods),
	// which freezes every pool at the size it is; nil when there is none.
	refusal *render.RefError
	// refusedSeats counts the Seats refused a pool on their own.
	refusedSeats int
	// undeclared are the agent Seats placed in a pool the Company does not
	// declare, as "seat (pool)", sorted: the document pins each to a label
	// no pod carries, which holds nothing else back.
	undeclared []string
	// wanted are the StatefulSets the plan runs, in the order of the pools
	// and of their seats; kept those it does not, which the running revision
	// pins a seat to; stale every other StatefulSet of seat pods.
	wanted, kept []*seatWorkload
	stale        []*appsv1.StatefulSet
}

// running reports whether the company runs, or keeps, any seat pods.
func (p *seatPlan) running() bool {
	return len(p.wanted)+len(p.kept) > 0
}

// planSeatPools works out which seat StatefulSets a Company runs: those its
// plan places a seat in, sized by the plan, and those the revision the engine
// runs still pins a seat to (status.config.placement), kept at the size they
// are. The rest are stale. A class that allows no seat pools plans none, and
// keeps the pinned ones all the same: the engine keeps running the revision
// that pins seats to them while the Company's next one is refused for its
// pools, and the data pods keep their listener open for them for as long
// (render.LeafOpen). A Seat refused a pool takes no pod; a Company whose
// Shared pools alone pass the class's maxPods keeps every pool at the size
// it is (refusal).
func (r *CompanyReconciler) planSeatPools(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (*seatPlan, error) {
	existing, err := r.seatStatefulSets(ctx, c)
	if err != nil {
		return nil, err
	}
	seats := &crewletaiv1alpha1.SeatList{}
	if err := r.List(ctx, seats, client.InNamespace(c.Namespace)); err != nil {
		return nil, fmt.Errorf("listing the Seats of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	ours := slices.DeleteFunc(seats.Items, func(s crewletaiv1alpha1.Seat) bool { return s.Spec.CompanyRef.Name != c.Name })
	topology, err := render.PlanTopology(c, class, ours)
	plan := &seatPlan{topology: topology, allowed: render.SeatPoolsAllowed(class)}
	for i := range ours {
		s := &ours[i]
		if pool := render.SeatPool(c, s); s.Spec.Kind != crewletaiv1alpha1.SeatKindHuman && !declaredPool(c, pool) {
			plan.undeclared = append(plan.undeclared, fmt.Sprintf("%s (%s)", s.Name, pool))
		}
	}
	slices.Sort(plan.undeclared)
	for _, refusal := range render.RefErrors(err) {
		switch {
		case refusal.Kind != render.KindCompany:
			plan.refusedSeats++
		case refusal.Reason == crewletaiv1alpha1.ReasonPoolCapacityExceeded:
			plan.refusal = refusal
		}
	}

	if t := plan.topology; plan.allowed {
		for i := range t.Pools {
			pool := &t.Pools[i]
			if pool.Mode != crewletaiv1alpha1.SeatPoolPerSeat {
				plan.wanted = append(plan.wanted, &seatWorkload{name: sharedPoolStatefulSetName(c, pool.Name), pool: pool.Name, plan: pool})
				continue
			}
			for _, handle := range pool.Seats {
				plan.wanted = append(plan.wanted, &seatWorkload{
					name: perSeatStatefulSetName(c, handle), pool: pool.Name, seat: handle, plan: pool,
				})
			}
		}
	}
	for _, w := range plan.wanted {
		w.existing = existing[w.name]
		delete(existing, w.name)
	}
	pins := c.Status.Config.Placement
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		sts := existing[name]
		pool, seat := sts.Labels[render.PoolLabel], sts.Labels[render.SeatLabel]
		pinned := slices.Contains(pins.Pools, pool)
		if seat != "" {
			pinned = slices.Contains(pins.Seats, seat)
		}
		if !pinned {
			plan.stale = append(plan.stale, sts)
			continue
		}
		kept := keptPlan(sts, pool, seat, class)
		plan.kept = append(plan.kept, &seatWorkload{
			name: name, pool: pool, seat: seat, plan: &kept, kept: true, existing: sts, status: sts.Status,
		})
	}
	return plan, nil
}

// SeatPoolAnnotation records, on a seat StatefulSet, the pool spec it was
// last written from (a spec.topology.seatPools entry, as JSON). The Company may
// drop or change the pool while the revision the engine runs still pins a
// seat to its pods; the StatefulSet is then kept, and keeps following the
// data pods (a new image, a new keyring), which takes a template: the grace
// period and turns recorded here, and the class's current
// seatPools.defaults.resources and volume limits, which alone size a seat
// pod.
const SeatPoolAnnotation = "crewlet.ai/seat-pool"

// keptPlan is how a kept seat StatefulSet runs: its SeatPoolAnnotation's pool,
// resolved against the class's defaults as they are now. A StatefulSet
// without a readable one (its annotation was edited away) runs its pool's
// mode on the class's defaults alone.
func keptPlan(sts *appsv1.StatefulSet, pool, seat string, class *crewletaiv1alpha1.EngineClass) render.PoolPlan {
	spec := crewletaiv1alpha1.SeatPool{Name: pool, Mode: crewletaiv1alpha1.SeatPoolShared}
	if seat != "" {
		spec.Mode = crewletaiv1alpha1.SeatPoolPerSeat
	}
	var recorded crewletaiv1alpha1.SeatPool
	if raw, ok := sts.Annotations[SeatPoolAnnotation]; ok && json.Unmarshal([]byte(raw), &recorded) == nil &&
		recorded.Name == pool && (recorded.Mode == crewletaiv1alpha1.SeatPoolPerSeat) == (seat != "") {
		spec = recorded
	}
	return render.PoolPlanOf(&spec, class.Spec.SeatPools.Defaults)
}

// seatPoolSpec is SeatPoolAnnotation's value for a workload: the Company's
// spec of its pool, or for a kept workload the one it recorded.
func seatPoolSpec(c *crewletaiv1alpha1.Company, w *seatWorkload) (string, error) {
	spec := crewletaiv1alpha1.SeatPool{Name: w.pool, Mode: w.plan.Mode}
	if w.kept {
		if raw, ok := w.existing.Annotations[SeatPoolAnnotation]; ok {
			return raw, nil
		}
	} else {
		for i := range c.Spec.Topology.SeatPools {
			if c.Spec.Topology.SeatPools[i].Name == w.pool {
				spec = c.Spec.Topology.SeatPools[i]
			}
		}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("encoding seat pool %s: %w", w.pool, err)
	}
	return string(raw), nil
}

// SuspendedReplicasAnnotation records, on a seat StatefulSet a suspension
// scaled to zero, how many pods it ran. A StatefulSet the plan sizes takes
// its size from the plan when the Company resumes; one kept for the running
// revision, or frozen while the Company is refused for its pools, has nothing
// else to take it from.
const SuspendedReplicasAnnotation = "crewlet.ai/suspended-replicas"

// restoredReplicas is how many pods an existing seat StatefulSet runs once
// the Company runs: what it ran before a suspension, or what it runs.
func restoredReplicas(sts *appsv1.StatefulSet) int32 {
	if raw, ok := sts.Annotations[SuspendedReplicasAnnotation]; ok {
		if n, err := strconv.ParseInt(raw, 10, 32); err == nil && n >= 0 {
			return int32(n)
		}
	}
	return value(sts.Spec.Replicas)
}

// seatStatefulSets are the StatefulSets of seat pods the Company controls, by
// name. The cache holds every StatefulSet the operator manages.
func (r *CompanyReconciler) seatStatefulSets(ctx context.Context, c *crewletaiv1alpha1.Company) (map[string]*appsv1.StatefulSet, error) {
	list := &appsv1.StatefulSetList{}
	if err := r.List(ctx, list, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentSeats))); err != nil {
		return nil, fmt.Errorf("listing the seat StatefulSets of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	out := map[string]*appsv1.StatefulSet{}
	for i := range list.Items {
		if sts := &list.Items[i]; metav1.IsControlledBy(sts, c) {
			out[sts.Name] = sts
		}
	}
	return out, nil
}

// stopSeatPods scales every seat StatefulSet of the Company to zero and
// reports whether any seat pod is still there, stopping or not. It needs
// nothing rendered, so a suspended Company stops whatever else is wrong. The
// operator does not watch Pods, so a caller that waits for them polls.
//
// Each StatefulSet keeps how many pods it ran (SuspendedReplicasAnnotation),
// for one the Company's plan no longer sizes to run as many again when the
// Company resumes.
func (r *CompanyReconciler) stopSeatPods(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	existing, err := r.seatStatefulSets(ctx, c)
	if err != nil {
		return false, err
	}
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		sts := existing[name]
		if value(sts.Spec.Replicas) == 0 {
			continue
		}
		base := sts.DeepCopy()
		if sts.Annotations == nil {
			sts.Annotations = map[string]string{}
		}
		sts.Annotations[SuspendedReplicasAnnotation] = strconv.Itoa(int(value(sts.Spec.Replicas)))
		sts.Spec.Replicas = new(int32(0))
		if err := r.Patch(ctx, sts, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
			return false, fmt.Errorf("scaling StatefulSet %s/%s to zero: %w", sts.Namespace, sts.Name, err)
		}
	}
	return r.seatPodsLeft(ctx, c)
}

// seatPodsLeft reports whether any seat pod of the Company exists.
func (r *CompanyReconciler) seatPodsLeft(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	pods := &metav1.PartialObjectMetadataList{}
	pods.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
	if err := r.APIReader.List(ctx, pods, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentSeats)),
		client.Limit(1)); err != nil {
		return false, fmt.Errorf("listing the seat pods of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	return len(pods.Items) > 0, nil
}

// seatInputs is what the seat pods are started from: the data pods' own
// inputs, and how the data pods stand after this reconcile.
type seatInputs struct {
	keyring  *corev1.Secret
	objects  *render.S3Credentials
	platform map[string][]byte
	// image is the engine image of the data pods' template.
	image string
	// dataReady is every data pod ready on the current template.
	dataReady bool
}

// reconcileSeatPools writes the seat pools' objects and records what they
// report: the pools' Secrets and StatefulSets, their disruption budgets, the
// headless Service and NetworkPolicy of the seat pods, and the deletion of
// what no pool and no running revision needs. With in nil (the data pods are
// held back) it writes nothing and only reports.
func (r *CompanyReconciler) reconcileSeatPools(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, plan *seatPlan, in *seatInputs,
) error {
	for _, w := range plan.wanted {
		if w.existing != nil {
			w.status = w.existing.Status
		}
	}
	if in != nil {
		if err := r.writeSeatPools(ctx, c, class, plan, in); err != nil {
			return err
		}
	}
	reportSeatPools(c, class, plan)
	return nil
}

// writeSeatPools is the writing half of reconcileSeatPools.
//
// Every StatefulSet that stays, wanted or kept, follows the data pods: it
// takes their image and keyring once they are ready on their template, and
// its pool's Secret is written for it. What the plan cannot change while the
// Company is refused for its pools (frozen) is their size: none is created,
// and each keeps the pods it runs.
func (r *CompanyReconciler) writeSeatPools(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, plan *seatPlan, in *seatInputs,
) error {
	for _, sts := range plan.stale {
		if err := r.deleteIfControlled(ctx, c, sts); err != nil {
			return err
		}
		if err := r.deleteControlled(ctx, c, podDisruptionBudgetGVK, sts.Name); err != nil {
			return err
		}
	}
	// The Service governs every seat StatefulSet, so it is there before
	// the first and goes after the last.
	if plan.running() {
		if err := r.applySeatsService(ctx, c); err != nil {
			return err
		}
	} else if err := r.deleteControlled(ctx, c, serviceGVK, seatsServiceName(c)); err != nil {
		return err
	}
	if err := r.reconcileSeatsPolicy(ctx, c, class, plan.running()); err != nil {
		return err
	}

	// One Secret per pool and mode, written once for the StatefulSets that
	// start from it, the wanted ones' first.
	var order []string
	bySecret := map[string][]*seatWorkload{}
	for _, w := range slices.Concat(plan.wanted, plan.kept) {
		name := w.secretName(c)
		if bySecret[name] == nil {
			order = append(order, name)
		}
		bySecret[name] = append(bySecret[name], w)
	}
	keep := map[string]bool{}
	for _, name := range order {
		keep[name] = true
	}
	if suspended(c) {
		// Stopped by stopSeatPods; what starts them again stays.
		return r.deleteSeatSecrets(ctx, c, keep)
	}
	for _, name := range order {
		if err := r.writeSeatPool(ctx, c, class, name, bySecret[name], in, plan.refusal != nil); err != nil {
			return err
		}
	}
	return r.deleteSeatSecrets(ctx, c, keep)
}

// writeSeatPool writes one Secret of a pool and the StatefulSets that start
// from it: every one while the data pods are ready on the current template,
// and otherwise none, only taking away pods a pool no longer runs.
func (r *CompanyReconciler) writeSeatPool(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	secret string, workloads []*seatWorkload, in *seatInputs, frozen bool,
) error {
	// The document is the first workload's pool's: a wanted one's, as the
	// Company plans it, whenever the pool is still planned.
	pool := workloads[0].plan
	members := make([]string, c.Spec.Topology.Data.Replicas)
	for i := range members {
		members[i] = fmt.Sprintf("%s-%d.%s.%s.svc", dataStatefulSetName(c), i, dataServiceName(c), c.Namespace)
	}
	var data map[string][]byte
	var hash string
	written := false
	for _, w := range workloads {
		doc, err := render.SeatPodBootstrap(&render.SeatPodInput{
			Company: c, Class: class, Pool: pool, Seat: w.seat,
			Keyring: keyringOf(in.keyring), Objects: in.objects, DataMembers: members,
		})
		if err != nil {
			// The data pods were started from the same keyring, which
			// render accepted for them.
			return fmt.Errorf("rendering the bootstrap document of seat pool %s: %w", w.pool, err)
		}
		if data == nil {
			// One document serves the Secret: a PerSeat pool's seats
			// differ only in the environment.
			data = maps.Clone(in.platform)
			data[configFile] = doc.YAML
			hash = hashOf(data)
		}
		pod, err := seatPodTemplate(c, class, w, secret, in.image, doc, hash)
		if err != nil {
			return err
		}
		templateHash, err := podTemplateHash(pod)
		if err != nil {
			return err
		}
		replicas, create := w.replicas(frozen)
		if !in.dataReady || !create {
			if err := r.holdSeatWorkload(ctx, w, templateHash, frozen); err != nil {
				return err
			}
			continue
		}
		if !written {
			if err := r.applySeatSecret(ctx, c, secret, w.pool, data); err != nil {
				return err
			}
			written = true
		}
		spec, err := seatPoolSpec(c, w)
		if err != nil {
			return err
		}
		sts, err := r.applySeatStatefulSet(ctx, c, w, replicas, pod, map[string]string{
			PodTemplateHashAnnotation: templateHash, SeatPoolAnnotation: spec,
		})
		if err != nil {
			return err
		}
		if sts.Status != nil {
			w.status = statefulSetStatusOf(sts.Status)
		}
		if err := r.resumed(ctx, w); err != nil {
			return err
		}
		if err := r.reconcilePodDisruptionBudget(ctx, c, w, replicas); err != nil {
			return err
		}
	}
	return nil
}

// PodTemplateHashAnnotation records, on a seat StatefulSet, a hash of the pod
// template the operator last wrote into it: whether a StatefulSet held back
// while the data pods roll keeps an older template is whether the template it
// would be given now hashes otherwise.
const PodTemplateHashAnnotation = "crewlet.ai/pod-template-hash"

// podTemplateHash is PodTemplateHashAnnotation's value for a template.
func podTemplateHash(pod *corev1ac.PodTemplateSpecApplyConfiguration) (string, error) {
	raw, err := json.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("encoding a seat pod template: %w", err)
	}
	return hashOf(map[string][]byte{"template": raw}), nil
}

// resumed drops SuspendedReplicasAnnotation from a StatefulSet that runs
// again, so that a later scale of it is not undone at the next suspension.
func (r *CompanyReconciler) resumed(ctx context.Context, w *seatWorkload) error {
	if w.existing == nil {
		return nil
	}
	if _, ok := w.existing.Annotations[SuspendedReplicasAnnotation]; !ok {
		return nil
	}
	base := w.existing.DeepCopy()
	delete(w.existing.Annotations, SuspendedReplicasAnnotation)
	if err := r.Patch(ctx, w.existing, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
		return fmt.Errorf("recording that StatefulSet %s/%s runs again: %w", w.existing.Namespace, w.existing.Name, err)
	}
	return nil
}

// holdSeatWorkload leaves a seat StatefulSet on its template while the data
// pods are not ready on theirs, and creates none: it only takes away the
// pods a pool the plan sizes no longer runs. It notes whether the StatefulSet
// waits for a template (blocked) or to be created (pending).
func (r *CompanyReconciler) holdSeatWorkload(ctx context.Context, w *seatWorkload, templateHash string, frozen bool) error {
	if w.existing == nil {
		w.pending = true
		return nil
	}
	w.blocked = w.existing.Annotations[PodTemplateHashAnnotation] != templateHash
	if w.kept || frozen {
		return nil
	}
	if current := value(w.existing.Spec.Replicas); w.plan.Replicas < current {
		base := w.existing.DeepCopy()
		w.existing.Spec.Replicas = new(w.plan.Replicas)
		if err := r.Patch(ctx, w.existing, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
			return fmt.Errorf("scaling StatefulSet %s/%s down: %w", w.existing.Namespace, w.existing.Name, err)
		}
	}
	return nil
}

// applySeatSecret writes the Secret a seat pool's pods start from.
func (r *CompanyReconciler) applySeatSecret(ctx context.Context, c *crewletaiv1alpha1.Company, name, pool string, data map[string][]byte) error {
	labels := labelsFor(c, componentSeats)
	labels[render.PoolLabel] = pool
	secret := corev1ac.Secret(name, c.Namespace).
		WithLabels(labels).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	return r.apply(ctx, c, secretGVK, name, secret)
}

// deleteSeatSecrets deletes the seat pools' Secrets of the Company but those
// named. The cache holds Secrets as metadata, which is all this reads.
func (r *CompanyReconciler) deleteSeatSecrets(ctx context.Context, c *crewletaiv1alpha1.Company, keep map[string]bool) error {
	secrets := &metav1.PartialObjectMetadataList{}
	secrets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	if err := r.List(ctx, secrets, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentSeats))); err != nil {
		return fmt.Errorf("listing the seat pools' Secrets of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		if keep[secret.Name] {
			continue
		}
		if err := r.deleteIfControlled(ctx, c, secret); err != nil {
			return err
		}
	}
	return nil
}

// applySeatsService writes the headless Service that governs every seat
// StatefulSet. A StatefulSet needs one; nothing connects to a seat pod, so
// it has no port.
func (r *CompanyReconciler) applySeatsService(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	svc := corev1ac.Service(seatsServiceName(c), c.Namespace).
		WithLabels(labelsFor(c, componentSeats)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(corev1ac.ServiceSpec().
			WithClusterIP(corev1.ClusterIPNone).
			WithSelector(selectorFor(c, componentSeats)))
	return r.apply(ctx, c, serviceGVK, seatsServiceName(c), svc)
}

// seatWorkloadLabels are the labels of a seat StatefulSet and of its pods,
// and seatWorkloadSelector the selector that picks out its pods: a Shared
// pool's pods by the pool, a PerSeat seat's by the seat. A Shared pool's
// selector also requires the seat label to be absent, so that it never
// claims a PerSeat seat's pods when a pool changes mode under its name.
func seatWorkloadLabels(c *crewletaiv1alpha1.Company, w *seatWorkload) map[string]string {
	labels := labelsFor(c, componentSeats)
	labels[render.PoolLabel] = w.pool
	if w.seat != "" {
		labels[render.SeatLabel] = w.seat
	}
	return labels
}

func seatWorkloadSelector(c *crewletaiv1alpha1.Company, w *seatWorkload) *metav1ac.LabelSelectorApplyConfiguration {
	match := selectorFor(c, componentSeats)
	if w.seat != "" {
		match[render.SeatLabel] = w.seat
		return metav1ac.LabelSelector().WithMatchLabels(match)
	}
	match[render.PoolLabel] = w.pool
	return metav1ac.LabelSelector().WithMatchLabels(match).
		WithMatchExpressions(metav1ac.LabelSelectorRequirement().
			WithKey(render.SeatLabel).
			WithOperator(metav1.LabelSelectorOpDoesNotExist))
}

// applySeatStatefulSet writes one seat StatefulSet, with the pod template
// given, and returns it as the API server answered, status included.
func (r *CompanyReconciler) applySeatStatefulSet(
	ctx context.Context, c *crewletaiv1alpha1.Company, w *seatWorkload, replicas int32,
	pod *corev1ac.PodTemplateSpecApplyConfiguration, annotations map[string]string,
) (*appsv1ac.StatefulSetApplyConfiguration, error) {
	sts := appsv1ac.StatefulSet(w.name, c.Namespace).
		WithLabels(seatWorkloadLabels(c, w)).
		WithAnnotations(annotations).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithReplicas(replicas).
			WithServiceName(seatsServiceName(c)).
			WithSelector(seatWorkloadSelector(c, w)).
			// Parallel: no seat pod waits on another to start or stop.
			WithPodManagementPolicy(appsv1.ParallelPodManagement).
			WithMinReadySeconds(seatPodMinReadySeconds).
			WithUpdateStrategy(appsv1ac.StatefulSetUpdateStrategy().
				WithType(appsv1.RollingUpdateStatefulSetStrategyType).
				WithRollingUpdate(appsv1ac.RollingUpdateStatefulSetStrategy().
					WithMaxUnavailable(intstr.FromInt32(1)))).
			WithTemplate(pod))
	if err := r.apply(ctx, c, statefulSetGVK, w.name, sts); err != nil {
		return nil, err
	}
	return sts, nil
}

// seatPodTemplate is a seat pod: the engine container with the seats role
// alone, its scratch and sandboxes volumes and its Tier A, and what the
// class adds.
//
// Its probes are the data pod's, on the same port, where the engine serves a
// node without the ingress role nothing else. /ready is what makes them worth
// having: it is 503 while the pod is draining, cannot reach the data pods'
// broker (broker_unlinked), has no company document yet (unconfigured), cannot
// apply the revision its peers run (shed, stuck), does not hold its presence
// lease (no_presence), or was not admitted by its latest placement pass to
// claim seats (admission_withheld: no data pod has answered that its copy of
// the company's records admits a seat, or the pod cannot serve the seats it
// holds). The operator runs every engine in the normal mode, never a
// maintenance mode, so a seat pod is always judged on its admission; one the
// data pods cannot admit is one that runs no seat, and reads so.
//
// Nothing dials a seat pod: the kubelet probes it from its own node, which a
// NetworkPolicy does not filter, so the seats policy that admits no other pod
// leaves the probes alone, and the headless Service that governs the pods
// publishes no port. The executor run of one of its seats calls the data
// pods' tool bridge at the Company's public address, a box on the pod's own
// host included, since the pod serves no route of the bridge's to dial on
// loopback, and the data pod a call reaches relays it here over the broker
// connection the pod dials out on (engineEnv).
func seatPodTemplate(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	w *seatWorkload, secret, image string, doc *render.BootstrapDocument, hash string,
) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
	env, err := engineEnv(c, class, &engineEnvironment{
		bootstrap:   doc.Env,
		secret:      secret,
		statefulSet: w.name,
		// On the scratch volume: the pod starts with an empty one anyway.
		tursoCache: render.DataDir + "/cache",
	})
	if err != nil {
		return nil, err
	}
	container := withProbes(engineContainerBase(class, image, env), seatPodStartupSeconds(class)).
		WithVolumeMounts(
			corev1ac.VolumeMount().WithName(scratchVolume).WithMountPath(render.DataDir),
			corev1ac.VolumeMount().WithName(configVolumeName).WithMountPath(configDir).WithReadOnly(true),
		).
		// The same volume, so that the pool's scratch limit bounds what the
		// processes the engine runs write as well. /tmp is also where the
		// engine makes the directory of the sockets a CLI it runs on the pod
		// reaches its seat's tools through, so it must stay writable for the
		// pod's seats to have tools.
		WithVolumeMounts(scratchMounts()...).
		WithVolumeMounts(sandboxesMount())
	if res := w.plan.Resources; res != nil {
		resources, err := applyConfigOf[corev1ac.ResourceRequirementsApplyConfiguration](res)
		if err != nil {
			return nil, err
		}
		container.WithResources(resources)
	}
	spec, err := enginePodSpec(c, class, w.plan.TerminationGracePeriodSeconds)
	if err != nil {
		return nil, err
	}
	spec.WithContainers(container).
		WithVolumes(
			configVolume(secret),
			scratchVolumeOf(w.plan.ScratchSizeLimit),
			sandboxesVolumeOf(w.plan.SandboxSizeLimit),
		)
	labels, annotations := enginePodMetadata(class, seatWorkloadLabels(c, w), hash)
	return corev1ac.PodTemplateSpec().
		WithLabels(labels).
		WithAnnotations(annotations).
		WithSpec(spec), nil
}

// reconcilePodDisruptionBudget lets a voluntary disruption (a node drain)
// take one of a seat StatefulSet's pods at a time, where losing two would
// leave its seats with no pod: a Shared pool's, and a PerSeat seat's with a
// standby. A PerSeat seat with one pod has nothing to keep and gets none.
//
// Unready pods may always be evicted (AlwaysAllow). An unready seat pod is
// not doing its work (it cannot reach the data pods, is not admitted to
// claim seats, or cannot start at all), though it may still hold seats: one
// its placement pass withholds keeps what it holds, and one cut off from
// the fleet keeps its leases until they lapse. An eviction drains it like
// any stop, which hands those back, while a budget that counted it would
// hold up a node drain for as long as the pod stays unready: for ever, for
// one that cannot start.
func (r *CompanyReconciler) reconcilePodDisruptionBudget(
	ctx context.Context, c *crewletaiv1alpha1.Company, w *seatWorkload, replicas int32,
) error {
	if w.plan.Mode == crewletaiv1alpha1.SeatPoolPerSeat && replicas < 2 {
		return r.deleteControlled(ctx, c, podDisruptionBudgetGVK, w.name)
	}
	pdb := policyv1ac.PodDisruptionBudget(w.name, c.Namespace).
		WithLabels(seatWorkloadLabels(c, w)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(policyv1ac.PodDisruptionBudgetSpec().
			WithMaxUnavailable(intstr.FromInt32(1)).
			WithSelector(seatWorkloadSelector(c, w)).
			WithUnhealthyPodEvictionPolicy(policyv1.AlwaysAllow))
	return r.apply(ctx, c, podDisruptionBudgetGVK, w.name, pdb)
}

// statefulSetStatusOf is a StatefulSet status from its apply configuration, for the
// fields the report reads.
func statefulSetStatusOf(s *appsv1ac.StatefulSetStatusApplyConfiguration) appsv1.StatefulSetStatus {
	return appsv1.StatefulSetStatus{
		Replicas:        value(s.Replicas),
		ReadyReplicas:   value(s.ReadyReplicas),
		UpdatedReplicas: value(s.UpdatedReplicas),
	}
}

// reportSeatPools records the seat pools in the Company's
// status.topology.seatPools and its SeatsPlaced condition.
func reportSeatPools(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, plan *seatPlan) {
	statuses := make([]crewletaiv1alpha1.SeatPoolStatus, 0, len(c.Spec.Topology.SeatPools))
	for i := range c.Spec.Topology.SeatPools {
		spec := &c.Spec.Topology.SeatPools[i]
		status := crewletaiv1alpha1.SeatPoolStatus{
			Name: spec.Name, Mode: spec.Mode,
			Replicas: new(int32(0)), ReadyReplicas: new(int32(0)), UpdatedReplicas: new(int32(0)), Seats: new(int32(0)),
		}
		if p := plan.topology.Pool(spec.Name); p != nil {
			for range p.Seats {
				*status.Seats++
			}
		}
		for _, w := range plan.wanted {
			if w.pool == spec.Name {
				*status.Replicas += w.status.Replicas
				*status.ReadyReplicas += w.status.ReadyReplicas
				*status.UpdatedReplicas += w.status.UpdatedReplicas
			}
		}
		statuses = append(statuses, status)
	}
	c.Status.Topology.SeatPools = nil
	if len(statuses) > 0 {
		// Never an empty list: an empty topology is no topology, and the
		// API refuses one written as {}.
		c.Status.Topology.SeatPools = statuses
	}

	condition := seatsPlaced(c, class, plan)
	condition.Type = crewletaiv1alpha1.ConditionSeatsPlaced
	condition.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, condition)
}

// seatsPlaced is the SeatsPlaced condition, without its type and generation.
func seatsPlaced(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, plan *seatPlan) metav1.Condition {
	notPlaced := func(reason, message string) metav1.Condition {
		return metav1.Condition{Status: metav1.ConditionFalse, Reason: reason, Message: message + plan.notes()}
	}
	switch {
	case suspended(c):
		return notPlaced(crewletaiv1alpha1.ReasonSuspended, "The Company is suspended: no pod runs its seats.")
	case !plan.allowed && len(c.Spec.Topology.SeatPools) > 0:
		return notPlaced(crewletaiv1alpha1.ReasonSeatPoolsNotAllowed, fmt.Sprintf(
			"EngineClass %s does not allow seat pools (spec.seatPools.policy), so the operator starts no seat pod and the "+
				"Company's next revision is refused. The pods the revision the engine runs pins seats to keep running, and the "+
				"data pods keep their listener open for them, until a revision that pins no seat to a seat pool runs.", class.Name))
	case plan.refusal != nil:
		return notPlaced(crewletaiv1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
			"%s: %s. The operator keeps each seat pool at the size it is and starts no other; their pods still follow the "+
				"data pods' image and keyring.", plan.refusal.Field, plan.refusal.Message))
	case len(plan.undeclared) == 1:
		return notPlaced(crewletaiv1alpha1.ReasonPoolNotFound, fmt.Sprintf(
			"Seat %s is placed in a pool the Company does not declare (spec.topology.seatPools), whose pods never start: "+
				"the seat does not run in it until the Company declares the pool or the Seat names another. If it ran "+
				"elsewhere it keeps running there (its Applied is PlacementPending); otherwise no pod runs it, and its "+
				"messages wait.", plan.undeclared[0]))
	case len(plan.undeclared) > 1:
		return notPlaced(crewletaiv1alpha1.ReasonPoolNotFound, fmt.Sprintf(
			"Seats %s are placed in pools the Company does not declare (spec.topology.seatPools), whose pods never start: "+
				"none runs in its pool until the Company declares it or the Seat names another. A seat that ran elsewhere "+
				"keeps running there (its Applied is PlacementPending); no pod runs any other, and its messages wait.",
			strings.Join(plan.undeclared, ", ")))
	}

	// Every pool with a seat, data included, by what serves it.
	var empty, waiting, blocked []string
	if len(plan.topology.DataSeats) > 0 && value(c.Status.Topology.Data.ReadyReplicas) == 0 {
		empty = append(empty, crewletaiv1alpha1.DataPool)
	}
	if len(c.Spec.Topology.SeatPools) == 0 && len(empty) > 0 {
		// No pool of its own: every seat runs on the data pods.
		return notPlaced(crewletaiv1alpha1.ReasonPoolEmpty, "No data pod is ready, and every agent seat runs on the data pods: "+
			"their messages wait for one.")
	}
	for _, w := range plan.wanted {
		what := w.pool
		if w.seat != "" {
			what = fmt.Sprintf("%s (seat %s)", w.pool, w.seat)
		}
		switch {
		case w.plan.Mode == crewletaiv1alpha1.SeatPoolShared && len(w.plan.Seats) == 0:
			// A pool no seat is placed in has nothing to serve.
		case w.pending:
			waiting = append(waiting, what)
		case w.status.ReadyReplicas == 0:
			empty = append(empty, what)
		}
		if w.blocked {
			blocked = append(blocked, w.name)
		}
	}
	switch {
	case len(empty)+len(waiting) > 0:
		message := ""
		if len(empty) > 0 {
			message = "No ready pod runs the seats of " + strings.Join(empty, ", ") + ": their messages wait for one."
		}
		if len(waiting) > 0 {
			message = strings.TrimSpace(message + " The pods of " + strings.Join(waiting, ", ") +
				" are created once every data pod is ready on the current template.")
		}
		return notPlaced(crewletaiv1alpha1.ReasonPoolEmpty, message)
	case len(blocked) > 0:
		return notPlaced(crewletaiv1alpha1.ReasonRolloutBlocked, "StatefulSets "+strings.Join(blocked, ", ")+
			" keep their pods' template until every data pod is ready on the current one: seat pods follow the data pods.")
	}
	return metav1.Condition{
		Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonAllPoolsServing,
		Message: "Every pool an agent seat is placed in has a ready pod." + plan.notes(),
	}
}

// notes says what else is true of a plan: the pools kept for the running
// revision, and the seats refused a pool.
func (p *seatPlan) notes() string {
	var notes string
	if len(p.kept) > 0 {
		names := make([]string, 0, len(p.kept))
		for _, w := range p.kept {
			names = append(names, w.name)
		}
		notes += " StatefulSets " + strings.Join(names, ", ") + " keep running, as they are, while the revision the engine runs " +
			"pins a seat to them (status.config.placement)."
	}
	switch {
	case p.refusedSeats == 1:
		notes += " 1 Seat is refused the pool it names, and runs in none; its conditions say why."
	case p.refusedSeats > 1:
		notes += fmt.Sprintf(" %d Seats are refused the pool they name, and run in none; their conditions say why.", p.refusedSeats)
	}
	return notes
}

// deleteSeatPools deletes every seat StatefulSet of a Company being deleted
// and reports whether any seat pod is left: the data pods stop only after
// them, as when the Company is suspended.
func (r *CompanyReconciler) deleteSeatPools(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	existing, err := r.seatStatefulSets(ctx, c)
	if err != nil {
		return false, err
	}
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		if err := r.deleteIfControlled(ctx, c, existing[name]); err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	if len(existing) > 0 {
		return true, nil
	}
	return r.seatPodsLeft(ctx, c)
}
