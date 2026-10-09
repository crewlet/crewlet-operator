package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
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
// pods, keeps a scratch store on an emptyDir it loses at every start, and
// serves nothing: the pinned engine binds no port on a node without the
// ingress role, so there is nothing for a probe to ask. A seat pod is
// therefore ready as soon as its container runs, and minReadySeconds is what
// paces a rollout instead: a pod counts as available, and the next is
// replaced, only once it has run that long.
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
	// scratchVolume is a seat pod's one writable volume: the scratch store
	// and the native library under render.DataDir, /tmp and HOME, all
	// under the pool's scratchSizeLimit.
	scratchVolume = "scratch"
	// seatPodMinReadySeconds is how long a seat pod must run before it
	// counts as available. With no probe to say otherwise, it is the time
	// a new pod takes to join the stream, adopt the company document and
	// claim its first seats (a few seconds) with room to spare, so that a
	// rollout replaces the next pod only once the last one has stayed up.
	seatPodMinReadySeconds = 60
)

// leafOpen reports whether a class's data pods open the leaf listener: when
// it allows seat pools at all, whether or not a company has one yet, so that
// a company's first pool restarts nothing.
func leafOpen(class *crewletaiv1alpha1.EngineClass) bool {
	return class.Spec.SeatPools.Policy == crewletaiv1alpha1.SeatPoolsAllowed
}

// seatWorkload is one StatefulSet of seat pods.
type seatWorkload struct {
	name string
	// pool is the pool's name, and seat the handle of a PerSeat pool's
	// seat (empty for a Shared pool).
	pool, seat string
	// plan is the pool as planned; nil for a StatefulSet kept only because
	// the running revision pins a seat to it.
	plan *render.PoolPlan
	// existing is the StatefulSet as it is, or nil.
	existing *appsv1.StatefulSet
	// status is the StatefulSet's status after the reconcile: as the apply
	// answered, or as it was.
	status appsv1.StatefulSetStatus
	// blocked is an existing StatefulSet that keeps an older template while
	// the data pods roll; pending one not created yet while they are not
	// ready.
	blocked, pending bool
}

// seatPlan is what a Company's seat pools should be, against what they are.
type seatPlan struct {
	// topology is the plan render made.
	topology *render.Topology
	// allowed is whether the class allows seat pools: while it does not,
	// the data pods open no listener to join, and no seat pod runs.
	allowed bool
	// refusal is the Company's own refusal (its Shared pools pass maxPods),
	// which keeps every pool as it is; nil when there is none.
	refusal *render.RefError
	// refusedSeats counts the Seats refused a pool on their own.
	refusedSeats int
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
// runs still pins a seat to (status.config.placement), kept as they are. The
// rest are stale. A class that allows no seat pools runs none, pinned or not:
// its data pods have no listener for them to join. A Seat refused a pool
// takes no pod; a Company whose Shared pools alone pass the class's maxPods
// keeps every pool as it is (refusal).
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
	plan := &seatPlan{topology: topology, allowed: leafOpen(class)}
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
		if !plan.allowed || !pinned {
			plan.stale = append(plan.stale, sts)
			continue
		}
		plan.kept = append(plan.kept, &seatWorkload{name: name, pool: pool, seat: seat, existing: sts, status: sts.Status})
	}
	return plan, nil
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
func (r *CompanyReconciler) stopSeatPods(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	existing, err := r.seatStatefulSets(ctx, c)
	if err != nil {
		return false, err
	}
	for _, name := range slices.Sorted(maps.Keys(existing)) {
		if err := r.scaleToZero(ctx, c, existing[name]); err != nil {
			return false, err
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

	// The pools whose Secret stays: every pool that runs a StatefulSet,
	// and the pool of each one kept, whose pods may restart from it.
	pools := map[string]bool{}
	for _, w := range plan.kept {
		pools[w.pool] = true
	}
	if plan.refusal != nil || suspended(c) {
		// Over the class's maxPods, or stopped: what runs stays as it is.
		for _, w := range plan.wanted {
			pools[w.pool] = true
		}
		return r.deleteSeatSecrets(ctx, c, pools)
	}

	byPool := map[string][]*seatWorkload{}
	var order []string
	for _, w := range plan.wanted {
		if byPool[w.pool] == nil {
			order = append(order, w.pool)
		}
		byPool[w.pool] = append(byPool[w.pool], w)
	}
	for _, pool := range order {
		pools[pool] = true
		if err := r.writeSeatPool(ctx, c, class, byPool[pool], in); err != nil {
			return err
		}
	}
	return r.deleteSeatSecrets(ctx, c, pools)
}

// writeSeatPool writes one pool's Secret and StatefulSets: every one while
// the data pods are ready on the current template, and otherwise none, only
// taking away pods the pool no longer runs.
func (r *CompanyReconciler) writeSeatPool(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	workloads []*seatWorkload, in *seatInputs,
) error {
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
			return fmt.Errorf("rendering the bootstrap document of seat pool %s: %w", pool.Name, err)
		}
		if data == nil {
			// One document serves the pool: a PerSeat pool's seats
			// differ only in the environment.
			data = maps.Clone(in.platform)
			data[configFile] = doc.YAML
			hash = hashOf(data)
		}
		if !in.dataReady {
			if err := r.holdSeatWorkload(ctx, w, in.image, hash); err != nil {
				return err
			}
			continue
		}
		if !written {
			if err := r.applySeatSecret(ctx, c, pool.Name, data); err != nil {
				return err
			}
			written = true
		}
		sts, err := r.applySeatStatefulSet(ctx, c, class, w, in.image, doc, hash)
		if err != nil {
			return err
		}
		if sts.Status != nil {
			w.status = statefulSetStatusOf(sts.Status)
		}
		if err := r.reconcilePodDisruptionBudget(ctx, c, w); err != nil {
			return err
		}
	}
	return nil
}

// holdSeatWorkload leaves a seat StatefulSet on its template while the data
// pods are not ready on theirs, and creates none: it only takes away the
// pods a pool no longer runs. It notes whether the StatefulSet waits for a
// template (blocked) or to be created (pending).
func (r *CompanyReconciler) holdSeatWorkload(ctx context.Context, w *seatWorkload, image, hash string) error {
	if w.existing == nil {
		w.pending = true
		return nil
	}
	template := &w.existing.Spec.Template
	w.blocked = runningImage(w.existing) != image || template.Annotations[BootstrapHashAnnotation] != hash
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
func (r *CompanyReconciler) applySeatSecret(ctx context.Context, c *crewletaiv1alpha1.Company, pool string, data map[string][]byte) error {
	labels := labelsFor(c, componentSeats)
	labels[render.PoolLabel] = pool
	secret := corev1ac.Secret(poolBootstrapName(c, pool), c.Namespace).
		WithLabels(labels).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	return r.apply(ctx, c, secretGVK, poolBootstrapName(c, pool), secret)
}

// deleteSeatSecrets deletes the seat pools' Secrets of every pool but those
// given. The cache holds Secrets as metadata, which is all this reads.
func (r *CompanyReconciler) deleteSeatSecrets(ctx context.Context, c *crewletaiv1alpha1.Company, keep map[string]bool) error {
	secrets := &metav1.PartialObjectMetadataList{}
	secrets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	if err := r.List(ctx, secrets, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentSeats))); err != nil {
		return fmt.Errorf("listing the seat pools' Secrets of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		if keep[secret.Labels[render.PoolLabel]] {
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

// applySeatStatefulSet writes one seat StatefulSet and returns it as the API
// server answered, status included.
func (r *CompanyReconciler) applySeatStatefulSet(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	w *seatWorkload, image string, doc *render.BootstrapDocument, hash string,
) (*appsv1ac.StatefulSetApplyConfiguration, error) {
	pod, err := seatPodTemplate(c, class, w, image, doc, hash)
	if err != nil {
		return nil, err
	}
	sts := appsv1ac.StatefulSet(w.name, c.Namespace).
		WithLabels(seatWorkloadLabels(c, w)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithReplicas(w.plan.Replicas).
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
// alone, its scratch volume and its Tier A, and what the class adds. It has
// no port and no probe, since the engine serves nothing on a node without
// the ingress role.
func seatPodTemplate(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	w *seatWorkload, image string, doc *render.BootstrapDocument, hash string,
) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
	env, err := engineEnv(c, class, &engineEnvironment{
		bootstrap:   doc.Env,
		secret:      poolBootstrapName(c, w.pool),
		statefulSet: w.name,
		// On the scratch volume: the pod starts with an empty one anyway.
		tursoCache: render.DataDir + "/cache",
	})
	if err != nil {
		return nil, err
	}
	container := engineContainerBase(class, image, env).
		WithVolumeMounts(
			corev1ac.VolumeMount().WithName(scratchVolume).WithMountPath(render.DataDir),
			corev1ac.VolumeMount().WithName(configVolumeName).WithMountPath(configDir).WithReadOnly(true),
			// One volume, so that the pool's scratch limit bounds what
			// the processes the engine runs write as well.
			corev1ac.VolumeMount().WithName(scratchVolume).WithMountPath("/tmp").WithSubPath("tmp"),
			corev1ac.VolumeMount().WithName(scratchVolume).WithMountPath(homeDir).WithSubPath("home"),
		)
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
			configVolume(poolBootstrapName(c, w.pool)),
			corev1ac.Volume().WithName(scratchVolume).WithEmptyDir(corev1ac.EmptyDirVolumeSource().
				WithSizeLimit(w.plan.ScratchSizeLimit)),
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
// Crash-looping pods may always be evicted (AlwaysAllow): with no probe,
// a seat pod is unhealthy only while its engine cannot start, and a budget
// that held such a pod would hold up the drain for nothing.
func (r *CompanyReconciler) reconcilePodDisruptionBudget(ctx context.Context, c *crewletaiv1alpha1.Company, w *seatWorkload) error {
	if w.plan.Mode == crewletaiv1alpha1.SeatPoolPerSeat && w.plan.Replicas < 2 {
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
			"EngineClass %s does not allow seat pools (spec.seatPools.policy), so the data pods open no listener for seat pods "+
				"to join and the operator runs none: the seats placed in the Company's pools run nowhere.", class.Name))
	case plan.refusal != nil:
		return notPlaced(crewletaiv1alpha1.ReasonPoolCapacityExceeded, fmt.Sprintf(
			"%s: %s. The operator keeps the seat pools' pods as they are and starts no other.", plan.refusal.Field, plan.refusal.Message))
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
