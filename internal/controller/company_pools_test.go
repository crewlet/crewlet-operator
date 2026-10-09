package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// The objects the operator makes for the seat pools of the Company the seat
// pool specs run: Nimbus, with a Shared pool general of two pods, a Shared
// pool batch at zero, and a PerSeat pool isolated.
const (
	poolsCompany      = "nimbus"
	seatsService      = "nimbus-seats"
	generalSet        = "nimbus-seats-general"
	batchSet          = "nimbus-seats-batch"
	cySet             = "nimbus-seat-cy"
	generalBootstrap  = "nimbus-bootstrap-general"
	isolatedBootstrap = "nimbus-bootstrap-isolated"
	brokerPolicy      = "nimbus-broker"
	seatsPolicy       = "nimbus-seats"
	seatImageV030     = "ghcr.io/crewlet/crewlet:v0.3.0"
)

func TestBoundedNameKeepsAStatefulSetsPodsLabelled(t *testing.T) {
	company := &crewletaiv1alpha1.Company{Name: strings.Repeat("c", 47)}
	pool := strings.Repeat("p", 20)
	handle := strings.Repeat("h", 63)
	names := map[string]string{
		"a Shared pool":                    sharedPoolStatefulSetName(company, pool),
		"a PerSeat seat":                   perSeatStatefulSetName(company, handle),
		"another pool of the same start":   sharedPoolStatefulSetName(company, pool[:19]+"q"),
		"a short one":                      sharedPoolStatefulSetName(&crewletaiv1alpha1.Company{Name: "nimbus"}, "general"),
		"a cut that would end with a dash": boundedName(strings.Repeat("a", 42) + "-" + strings.Repeat("b", 20)),
	}
	seen := map[string]string{}
	for what, name := range names {
		if len(name) > maxStatefulSetName {
			t.Errorf("%s's StatefulSet %s has %d characters, over %d", what, name, len(name), maxStatefulSetName)
		}
		if strings.Contains(name, "--") || strings.HasSuffix(name, "-") {
			t.Errorf("%s's StatefulSet %s is not a DNS label", what, name)
		}
		if other, ok := seen[name]; ok {
			t.Errorf("%s and %s share the StatefulSet %s", what, other, name)
		}
		seen[name] = what
	}
	if got := names["a short one"]; got != "nimbus-seats-general" {
		t.Errorf("a name that fits is %s, want it unchanged", got)
	}
}

var _ = Describe("Company seat pools", func() {
	var namespace string

	BeforeEach(func() {
		ns := &corev1.Namespace{GenerateName: "pools-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name
	})

	// newClass creates an EngineClass that allows seat pools, PerSeat ones
	// included, named after the spec's namespace.
	newClass := func(mutate ...func(*crewletaiv1alpha1.EngineClass)) *crewletaiv1alpha1.EngineClass {
		GinkgoHelper()
		class := &crewletaiv1alpha1.EngineClass{
			Name: namespace,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
				Retention: crewletaiv1alpha1.RetentionSettings{BackupOwner: "platform-oncall"},
				SeatPools: crewletaiv1alpha1.SeatPoolSettings{Policy: crewletaiv1alpha1.SeatPoolsAllowed, AllowPerSeat: new(true)},
			},
		}
		for _, m := range mutate {
			m(class)
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, class))).To(Succeed()) })
		return class
	}
	// newCompany creates Nimbus with three pools and a seat in each kind of
	// place: ana on the data pods, bo in general, cy alone in isolated, dee
	// in batch, and jane, a person, nowhere.
	newCompany := func(class string, mutate ...func(*crewletaiv1alpha1.Company)) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace,
			Name:      poolsCompany,
			Spec: crewletaiv1alpha1.CompanySpec{
				CompanyName: companyDisplayName, EngineClassName: class,
				Topology: crewletaiv1alpha1.CompanyTopology{SeatPools: []crewletaiv1alpha1.SeatPool{
					{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared, Replicas: new(int32(2))},
					{Name: poolIsolated, Mode: crewletaiv1alpha1.SeatPoolPerSeat, Replicas: new(int32(2))},
					{Name: poolBatch, Mode: crewletaiv1alpha1.SeatPoolShared, Replicas: new(int32(0))},
				}},
			},
		}
		for _, m := range mutate {
			m(company)
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		for handle, pool := range map[string]string{seatAna: "", "bo": poolGeneral, "cy": poolIsolated, "dee": poolBatch} {
			Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Seat{
				Namespace: namespace, Name: handle,
				Spec: crewletaiv1alpha1.SeatSpec{
					CompanyRef: crewletaiv1alpha1.CompanyReference{Name: poolsCompany},
					Placement:  crewletaiv1alpha1.SeatPlacement{Pool: pool},
				},
			})).To(Succeed())
		}
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Seat{
			Namespace: namespace, Name: "jane",
			Spec: crewletaiv1alpha1.SeatSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: poolsCompany},
				Kind:       crewletaiv1alpha1.SeatKindHuman,
				Contact:    crewletaiv1alpha1.SeatContact{GitHubLogin: "jane"},
			},
		})).To(Succeed())
		return company
	}
	reconcileOnce := func(company *crewletaiv1alpha1.Company) ctrl.Result {
		GinkgoHelper()
		result, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(company)})
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	reconciled := func(company *crewletaiv1alpha1.Company) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		reconcileOnce(company)
		got := &crewletaiv1alpha1.Company{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
		return got
	}
	updateCompany := func(company *crewletaiv1alpha1.Company, mutate func(*crewletaiv1alpha1.Company)) {
		GinkgoHelper()
		updateLive(company, mutate)
	}
	condition := func(company *crewletaiv1alpha1.Company, t string) metav1.Condition {
		GinkgoHelper()
		c := meta.FindStatusCondition(company.Status.Conditions, t)
		Expect(c).NotTo(BeNil(), "the Company has no %s condition", t)
		Expect(c.ObservedGeneration).To(Equal(company.Generation), "%s describes an older generation", t)
		return *c
	}
	get := func(obj client.Object, name string) {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)).To(Succeed())
	}
	absent := func(obj client.Object, name string) {
		GinkgoHelper()
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, obj)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%T %s exists (err=%v)", obj, name, err)
	}
	statefulSet := func(name string) *appsv1.StatefulSet {
		GinkgoHelper()
		sts := &appsv1.StatefulSet{}
		get(sts, name)
		return sts
	}
	// rollOut stands in for the StatefulSet controller and the kubelet: it
	// reports every replica of a StatefulSet's current template ready.
	rollOut := func(name string) {
		GinkgoHelper()
		sts := statefulSet(name)
		n := *sts.Spec.Replicas
		sts.Status = appsv1.StatefulSetStatus{
			ObservedGeneration: sts.Generation,
			Replicas:           n, ReadyReplicas: n, AvailableReplicas: n, UpdatedReplicas: n, CurrentReplicas: n,
			CurrentRevision: fmt.Sprintf("%s-%d", name, sts.Generation),
			UpdateRevision:  fmt.Sprintf("%s-%d", name, sts.Generation),
		}
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
	}
	seatSets := func() []string {
		GinkgoHelper()
		list := &appsv1.StatefulSetList{}
		Expect(k8sClient.List(ctx, list, client.InNamespace(namespace),
			client.MatchingLabels{"app.kubernetes.io/component": componentSeats})).To(Succeed())
		var names []string
		for i := range list.Items {
			if list.Items[i].DeletionTimestamp.IsZero() {
				names = append(names, list.Items[i].Name)
			}
		}
		return names
	}
	// running starts the company: its data pod ready, then its seat pools.
	running := func(class string, mutate ...func(*crewletaiv1alpha1.Company)) *crewletaiv1alpha1.Company {
		GinkgoHelper()
		company := reconciled(newCompany(class, mutate...))
		rollOut(dataName)
		return reconciled(company)
	}
	// pinned records, as the config reconciler would, where the revision the
	// engine runs pins its seats.
	pinned := func(company *crewletaiv1alpha1.Company, placement crewletaiv1alpha1.RevisionPlacement) {
		GinkgoHelper()
		updateStatusLive(company, func(c *crewletaiv1alpha1.Company) { c.Status.Config.Placement = placement })
	}
	// seatPod stands in for a pod of a seat StatefulSet.
	seatPod := func(name string) *corev1.Pod {
		GinkgoHelper()
		pod := &corev1.Pod{
			Namespace: namespace, Name: name,
			Labels: selectorFor(&crewletaiv1alpha1.Company{Name: poolsCompany}, componentSeats),
			Spec: corev1.PodSpec{
				ServiceAccountName:           engineName,
				AutomountServiceAccountToken: new(false),
				Containers:                   []corev1.Container{{Name: "engine", Image: engineImageV020}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		return pod
	}
	gone := func(obj client.Object) {
		GinkgoHelper()
		Expect(k8sClient.Delete(ctx, obj, client.GracePeriodSeconds(0))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		}).Should(BeTrue())
	}

	It("starts the seat pools once the data pods are ready, and makes every object they run with", func() {
		class := newClass()
		company := reconciled(newCompany(class.Name))

		By("opening the data pods' leaf listener, which the class allows")
		data := statefulSet(dataName)
		Expect(data.Spec.Template.Spec.Containers[0].Ports).To(ContainElement(corev1.ContainerPort{
			Name: "leaf", ContainerPort: 7422, Protocol: corev1.ProtocolTCP,
		}))
		dataSvc := &corev1.Service{}
		get(dataSvc, dataName)
		Expect(dataSvc.Spec.Ports).To(ContainElement(corev1.ServicePort{
			Name: "leaf", Port: 7422, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("leaf"),
		}))
		engineSvc := &corev1.Service{}
		get(engineSvc, engineName)
		Expect(engineSvc.Spec.Ports).To(HaveLen(1), "the engine Service routes the API alone")

		By("creating no seat pod while the data pod is not ready")
		Expect(seatSets()).To(BeEmpty())
		placed := condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Status).To(Equal(metav1.ConditionFalse))
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonPoolEmpty))
		Expect(placed.Message).To(ContainSubstring("are created once every data pod is ready"))

		rollOut(dataName)
		company = reconciled(company)

		By("running a StatefulSet for each Shared pool and for each seat of a PerSeat pool")
		Expect(seatSets()).To(ConsistOf(generalSet, batchSet, cySet))

		general := statefulSet(generalSet)
		Expect(metav1.GetControllerOf(general).UID).To(Equal(company.UID))
		Expect(*general.Spec.Replicas).To(Equal(int32(2)))
		Expect(general.Spec.ServiceName).To(Equal(seatsService))
		Expect(general.Spec.PodManagementPolicy).To(Equal(appsv1.ParallelPodManagement))
		Expect(general.Spec.MinReadySeconds).To(Equal(int32(60)))
		Expect(general.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable).To(Equal(new(intstr.FromInt32(1))))
		Expect(general.Spec.VolumeClaimTemplates).To(BeEmpty())
		Expect(general.Spec.Selector.MatchLabels).To(HaveKeyWithValue(render.PoolLabel, poolGeneral))
		Expect(general.Spec.Selector.MatchExpressions).To(ConsistOf(metav1.LabelSelectorRequirement{
			Key: render.SeatLabel, Operator: metav1.LabelSelectorOpDoesNotExist,
		}))
		Expect(*statefulSet(batchSet).Spec.Replicas).To(BeZero())

		By("rendering each pool's Tier A into a Secret its pods mount")
		keyring := &corev1.Secret{}
		get(keyring, keyringName)
		plan, err := render.PlanTopology(company, class, nil)
		Expect(err).NotTo(HaveOccurred())
		want, err := render.SeatPodBootstrap(&render.SeatPodInput{
			Company: company, Class: class, Pool: plan.Pool(poolGeneral), Keyring: keyringOf(keyring),
			DataMembers: []string{"nimbus-data-0.nimbus-data." + namespace + ".svc"},
		})
		Expect(err).NotTo(HaveOccurred())
		bootstrap := &corev1.Secret{}
		get(bootstrap, generalBootstrap)
		Expect(metav1.GetControllerOf(bootstrap).UID).To(Equal(company.UID))
		Expect(bootstrap.Data).To(Equal(map[string][]byte{configFile: want.YAML}))
		get(&corev1.Secret{}, isolatedBootstrap)

		pod := general.Spec.Template
		Expect(pod.Annotations).To(HaveKeyWithValue(BootstrapHashAnnotation, hashOf(bootstrap.Data)))
		Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", componentSeats))
		Expect(pod.Labels).To(HaveKeyWithValue(render.PoolLabel, poolGeneral))
		Expect(pod.Spec.ServiceAccountName).To(Equal(engineName))
		Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		Expect(*pod.Spec.EnableServiceLinks).To(BeFalse())
		Expect(*pod.Spec.TerminationGracePeriodSeconds).To(Equal(int64(300)))
		Expect(*pod.Spec.SecurityContext.RunAsNonRoot).To(BeTrue())
		Expect(pod.Spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		Expect(pod.Spec.Volumes).To(ConsistOf(
			corev1.Volume{Name: configVolumeName, Secret: &corev1.SecretVolumeSource{
				SecretName: generalBootstrap, Items: []corev1.KeyToPath{{Key: configFile, Path: configFile}}, DefaultMode: new(int32(0o440)),
			}},
			corev1.Volume{Name: scratchVolume, EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: new(resource.MustParse(render.DefaultSeatPodScratchSizeLimit)),
			}},
		))
		container := pod.Spec.Containers[0]
		Expect(container.Image).To(Equal(engineImageV020), "the seat pods run the data pods' image")
		Expect(container.Args).To(Equal([]string{"run", "-config", "/etc/crewlet/crewlet.yaml"}))
		Expect(container.Ports).To(BeEmpty(), "nothing listens on a seat pod")
		Expect(container.LivenessProbe).To(BeNil())
		Expect(container.ReadinessProbe).To(BeNil())
		Expect(container.StartupProbe).To(BeNil())
		Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
		Expect(container.Env[:len(want.Env)]).To(Equal(want.Env))
		Expect(container.Env).To(ContainElement(corev1.EnvVar{Name: "TURSO_GO_CACHE_DIR", Value: "/var/lib/crewlet/cache"}))
		Expect(container.VolumeMounts).To(ConsistOf(
			corev1.VolumeMount{Name: scratchVolume, MountPath: "/var/lib/crewlet"},
			corev1.VolumeMount{Name: configVolumeName, MountPath: "/etc/crewlet", ReadOnly: true},
			corev1.VolumeMount{Name: scratchVolume, MountPath: "/tmp", SubPath: "tmp"},
			corev1.VolumeMount{Name: scratchVolume, MountPath: "/home/crewlet", SubPath: "home"},
		))

		By("giving a PerSeat seat's pods their seat, and no other pool's selector")
		cy := statefulSet(cySet)
		Expect(cy.Spec.Selector.MatchLabels).To(HaveKeyWithValue(render.SeatLabel, "cy"))
		Expect(cy.Spec.Template.Spec.Containers[0].Env).To(ContainElement(corev1.EnvVar{Name: render.EnvSeat, Value: "cy"}))
		Expect(cy.Spec.Template.Spec.Volumes[0].Secret.SecretName).To(Equal(isolatedBootstrap))
		for _, name := range []string{generalSet, batchSet} {
			selector, err := metav1.LabelSelectorAsSelector(statefulSet(name).Spec.Selector)
			Expect(err).NotTo(HaveOccurred())
			Expect(selector.Matches(labels.Set(cy.Spec.Template.Labels))).To(BeFalse(), "%s selects cy's pods", name)
		}

		By("governing them with a headless Service of no port")
		svc := &corev1.Service{}
		get(svc, seatsService)
		Expect(svc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
		Expect(svc.Spec.Ports).To(BeEmpty())

		By("budgeting disruptions one pod at a time where a pool has a pod to keep")
		for _, name := range []string{generalSet, batchSet, cySet} {
			pdb := &policyv1.PodDisruptionBudget{}
			get(pdb, name)
			Expect(pdb.Spec.MaxUnavailable).To(Equal(new(intstr.FromInt32(1))))
			Expect(pdb.Spec.Selector).To(Equal(statefulSet(name).Spec.Selector))
			Expect(*pdb.Spec.UnhealthyPodEvictionPolicy).To(Equal(policyv1.AlwaysAllow))
		}

		By("admitting only the company's seat pods to the leaf listener, and nothing to a seat pod")
		broker := &networkingv1.NetworkPolicy{}
		get(broker, brokerPolicy)
		Expect(broker.Spec.PodSelector.MatchLabels).To(Equal(selectorFor(company, componentData)))
		Expect(broker.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
		Expect(broker.Spec.Ingress).To(HaveLen(1), "under Isolated, the engine policy admits the API port")
		Expect(broker.Spec.Ingress[0].Ports[0].Port.IntValue()).To(Equal(7422))
		Expect(broker.Spec.Ingress[0].From).To(Equal([]networkingv1.NetworkPolicyPeer{{
			PodSelector: &metav1.LabelSelector{MatchLabels: selectorFor(company, componentSeats)},
		}}))
		seats := &networkingv1.NetworkPolicy{}
		get(seats, seatsPolicy)
		Expect(seats.Spec.PodSelector.MatchLabels).To(Equal(selectorFor(company, componentSeats)))
		Expect(seats.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
		Expect(seats.Spec.Ingress).To(BeEmpty())
		Expect(egressRules(seats)).To(ConsistOf(
			"ports TCP/7422",
			"to pods {"+metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: selectorFor(company, componentData)})+"} in the policy's namespace",
			"ports UDP/53 TCP/53",
			"to pods {any} in the policy's namespace",
			"to "+anyIPv4+" except [169.254.169.254/32]",
			"to "+anyIPv6+" except [fd00:ec2::254/128]",
		))
		By("selecting the seat pods with no policy but their own, which NetworkPolicies being additive needs")
		for _, name := range []string{engineName, brokerPolicy} {
			policy := &networkingv1.NetworkPolicy{}
			get(policy, name)
			selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
			Expect(err).NotTo(HaveOccurred())
			Expect(selector.Matches(labels.Set(pod.Labels))).To(BeFalse(), "%s selects the seat pods", name)
			Expect(selector.Matches(labels.Set(data.Spec.Template.Labels))).To(BeTrue(), "%s does not select the data pods", name)
		}

		By("reporting each pool")
		Expect(company.Status.Topology.SeatPools).To(Equal([]crewletaiv1alpha1.SeatPoolStatus{
			{Name: poolGeneral, Mode: crewletaiv1alpha1.SeatPoolShared, Replicas: new(int32(0)), ReadyReplicas: new(int32(0)), UpdatedReplicas: new(int32(0)), Seats: new(int32(1))},
			{Name: poolIsolated, Mode: crewletaiv1alpha1.SeatPoolPerSeat, Replicas: new(int32(0)), ReadyReplicas: new(int32(0)), UpdatedReplicas: new(int32(0)), Seats: new(int32(1))},
			{Name: poolBatch, Mode: crewletaiv1alpha1.SeatPoolShared, Replicas: new(int32(0)), ReadyReplicas: new(int32(0)), UpdatedReplicas: new(int32(0)), Seats: new(int32(1))},
		}))
		placed = condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonPoolEmpty))
		Expect(placed.Message).To(ContainSubstring("general, isolated (seat cy), batch"))

		rollOut(generalSet)
		rollOut(cySet)
		company = reconciled(company)
		Expect(company.Status.Topology.SeatPools[0].ReadyReplicas).To(Equal(new(int32(2))))
		placed = condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonPoolEmpty), "batch runs no pod and holds dee")
		Expect(placed.Message).To(ContainSubstring("seats of batch:"))

		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Topology.SeatPools[2].Replicas = new(int32(1)) })
		company = reconciled(company)
		rollOut(batchSet)
		company = reconciled(company)
		placed = condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Status).To(Equal(metav1.ConditionTrue))
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonAllPoolsServing))
		ready := condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Reason).NotTo(Equal(crewletaiv1alpha1.ReasonPoolEmpty), "SeatsPlaced does not count towards Ready")
	})

	It("runs pods the restricted Pod Security Standard admits", func() {
		updateLive(&corev1.Namespace{Name: namespace}, func(ns *corev1.Namespace) {
			ns.Labels = map[string]string{"pod-security.kubernetes.io/enforce": "restricted"}
		})
		class := newClass()
		running(class.Name)
		admitted := func(name string, template corev1.PodTemplateSpec) error {
			pod := &corev1.Pod{Namespace: namespace, Name: name + "-0", Labels: template.Labels, Spec: template.Spec}
			return k8sClient.Create(ctx, pod, client.DryRunAll)
		}
		// The StatefulSet controller adds the data pod's volume from its
		// claim template.
		data := statefulSet(dataName).Spec.Template
		data.Spec.Volumes = append(data.Spec.Volumes, corev1.Volume{
			Name: dataVolume, PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: dataClaimName},
		})
		Expect(admitted(dataName, data)).To(Succeed(), "the namespace refuses the data pods")
		for _, name := range []string{generalSet, cySet} {
			Expect(admitted(name, statefulSet(name).Spec.Template)).To(Succeed(), "the namespace refuses %s's pods", name)
		}
		// The control: the namespace does enforce the standard.
		escalating := statefulSet(generalSet).Spec.Template
		escalating.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation = new(true)
		Expect(admitted("escalating", escalating)).To(MatchError(ContainSubstring("restricted")))
	})

	It("changes nothing on a second reconcile", func() {
		class := newClass()
		company := running(class.Name)
		versions := func() map[string]string {
			GinkgoHelper()
			out := map[string]string{}
			for _, o := range []struct {
				name string
				obj  client.Object
			}{
				{generalSet, &appsv1.StatefulSet{}}, {cySet, &appsv1.StatefulSet{}}, {batchSet, &appsv1.StatefulSet{}},
				{generalBootstrap, &corev1.Secret{}}, {isolatedBootstrap, &corev1.Secret{}},
				{seatsService, &corev1.Service{}}, {dataName, &corev1.Service{}},
				{brokerPolicy, &networkingv1.NetworkPolicy{}}, {seatsPolicy, &networkingv1.NetworkPolicy{}},
				{generalSet, &policyv1.PodDisruptionBudget{}}, {cySet, &policyv1.PodDisruptionBudget{}},
				{poolsCompany, &crewletaiv1alpha1.Company{}},
			} {
				get(o.obj, o.name)
				out[fmt.Sprintf("%T/%s", o.obj, o.name)] = o.obj.GetResourceVersion()
			}
			return out
		}
		before := versions()
		reconciled(company)
		Expect(versions()).To(Equal(before))
	})

	It("names the pools' StatefulSets within what their pods' labels hold", func() {
		class := newClass()
		long := strings.Repeat("n", 47)
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace, Name: long,
			Spec: crewletaiv1alpha1.CompanySpec{
				CompanyName: companyDisplayName, EngineClassName: class.Name,
				Topology: crewletaiv1alpha1.CompanyTopology{SeatPools: []crewletaiv1alpha1.SeatPool{
					{Name: strings.Repeat("p", 20)},
					{Name: "solo", Mode: crewletaiv1alpha1.SeatPoolPerSeat},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())
		handle := strings.Repeat("h", 63)
		Expect(k8sClient.Create(ctx, &crewletaiv1alpha1.Seat{
			Namespace: namespace, Name: handle,
			Spec: crewletaiv1alpha1.SeatSpec{
				CompanyRef: crewletaiv1alpha1.CompanyReference{Name: long},
				Placement:  crewletaiv1alpha1.SeatPlacement{Pool: "solo"},
			},
		})).To(Succeed())
		reconcileOnce(company)
		rollOut(long + "-data")
		reconcileOnce(company)

		list := &appsv1.StatefulSetList{}
		Expect(k8sClient.List(ctx, list, client.InNamespace(namespace), client.MatchingLabels{crewletaiv1alpha1.CompanyLabel: long})).To(Succeed())
		names := make([]string, 0, len(list.Items))
		for i := range list.Items {
			names = append(names, list.Items[i].Name)
			Expect(len(list.Items[i].Name)).To(BeNumerically("<=", 52), "%s overflows its pods' controller-revision-hash label", list.Items[i].Name)
		}
		Expect(names).To(ConsistOf(long+"-data", sharedPoolStatefulSetName(company, strings.Repeat("p", 20)), perSeatStatefulSetName(company, handle)))
	})

	It("rolls the seat pods only once the data pods run the new template, and all of them in one reconcile", func() {
		class := newClass()
		// A pod in every pool, so that nothing but the rollout keeps a
		// seat from running.
		company := running(class.Name, func(c *crewletaiv1alpha1.Company) { c.Spec.Topology.SeatPools[2].Replicas = new(int32(1)) })
		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.Engine.Image = seatImageV030 })
		company = reconciled(company)

		By("rolling the data pods first")
		Expect(statefulSet(dataName).Spec.Template.Spec.Containers[0].Image).To(Equal(seatImageV030))
		for _, name := range []string{generalSet, batchSet, cySet} {
			Expect(statefulSet(name).Spec.Template.Spec.Containers[0].Image).To(Equal(engineImageV020), "%s rolled before the data pods", name)
		}
		placed := condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonPoolEmpty), "no seat pod is ready yet")

		By("holding the seat pods back while the data pods roll, and saying so once nothing else is wrong")
		for _, name := range []string{generalSet, batchSet, cySet} {
			rollOut(name)
		}
		company = reconciled(company)
		placed = condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonRolloutBlocked))
		Expect(placed.Message).To(ContainSubstring(generalSet))

		By("rolling every seat StatefulSet once the data pods are ready on the new template")
		rollOut(dataName)
		reconciled(company)
		for _, name := range []string{generalSet, batchSet, cySet} {
			Expect(statefulSet(name).Spec.Template.Spec.Containers[0].Image).To(Equal(seatImageV030), "%s did not roll", name)
		}
	})

	It("adds pods to a pool only while the data pods are ready, and takes them away at once", func() {
		class := newClass()
		company := running(class.Name)
		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.Engine.Image = seatImageV030 })
		updateCompany(company, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Topology.SeatPools[0].Replicas = new(int32(1))
			c.Spec.Topology.SeatPools[2].Replicas = new(int32(3))
		})
		reconciled(company)
		Expect(*statefulSet(generalSet).Spec.Replicas).To(Equal(int32(1)))
		Expect(*statefulSet(batchSet).Spec.Replicas).To(BeZero(), "batch gained pods while the data pods rolled")
		rollOut(dataName)
		reconciled(company)
		Expect(*statefulSet(batchSet).Spec.Replicas).To(Equal(int32(3)))
	})

	It("stops the seat pods before the data pods when suspended, and starts them after when resumed", func() {
		class := newClass()
		company := running(class.Name)
		pod := seatPod("nimbus-seats-general-0")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })

		By("scaling every seat pool to zero and keeping the data pods while a seat pod stops")
		result := reconcileOnce(company)
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)), "nothing looks again for the seat pods to stop")
		for _, name := range []string{generalSet, batchSet, cySet} {
			Expect(*statefulSet(name).Spec.Replicas).To(BeZero(), "%s still runs", name)
		}
		Expect(*statefulSet(dataName).Spec.Replicas).To(Equal(int32(1)), "the data pods stopped before the seat pods")
		get(company, poolsCompany)
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspending))
		ready := condition(company, crewletaiv1alpha1.ConditionReady)
		Expect(ready.Message).NotTo(ContainSubstring("is stopped"))
		Expect(condition(company, crewletaiv1alpha1.ConditionSeatsPlaced).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))

		By("stopping the data pods once no seat pod is left")
		gone(pod)
		company = reconciled(company)
		Expect(*statefulSet(dataName).Spec.Replicas).To(BeZero())
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))

		By("starting the data pods first on resume, and the seat pods once they are ready")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(false) })
		reconciled(company)
		Expect(*statefulSet(dataName).Spec.Replicas).To(Equal(int32(1)))
		Expect(*statefulSet(generalSet).Spec.Replicas).To(BeZero(), "the seat pods started before the data pods were ready")
		rollOut(dataName)
		reconciled(company)
		Expect(*statefulSet(generalSet).Spec.Replicas).To(Equal(int32(2)))
		Expect(*statefulSet(cySet).Spec.Replicas).To(Equal(int32(2)))
	})

	It("stops the seat pods first when a Company that is not accepted is suspended", func() {
		class := newClass()
		company := running(class.Name)
		pod := seatPod("nimbus-seat-cy-0")
		Expect(k8sClient.Delete(ctx, class)).To(Succeed())
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Suspend = new(true) })
		company = reconciled(company)
		Expect(condition(company, crewletaiv1alpha1.ConditionAccepted).Reason).To(Equal(crewletaiv1alpha1.ReasonEngineClassNotFound))
		Expect(*statefulSet(cySet).Spec.Replicas).To(BeZero())
		Expect(*statefulSet(dataName).Spec.Replicas).To(Equal(int32(1)))
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspending))
		gone(pod)
		company = reconciled(company)
		Expect(*statefulSet(dataName).Spec.Replicas).To(BeZero())
		Expect(condition(company, crewletaiv1alpha1.ConditionRuntimeReady).Reason).To(Equal(crewletaiv1alpha1.ReasonSuspended))
	})

	It("keeps a removed pool's pods while the running revision pins a seat to them, and deletes them after", func() {
		class := newClass()
		company := running(class.Name)
		pinned(company, crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolGeneral, poolBatch}, Seats: []string{"cy"}})
		general := statefulSet(generalSet)

		By("keeping general, as it is, while the revision that runs pins bo to it")
		updateCompany(company, func(c *crewletaiv1alpha1.Company) {
			c.Spec.Topology.SeatPools = c.Spec.Topology.SeatPools[1:]
			c.Spec.Topology.SeatPools[0].Replicas = new(int32(1))
		})
		company = reconciled(company)
		Expect(statefulSet(generalSet).ResourceVersion).To(Equal(general.ResourceVersion))
		get(&corev1.Secret{}, generalBootstrap)
		get(&policyv1.PodDisruptionBudget{}, generalSet)
		Expect(condition(company, crewletaiv1alpha1.ConditionSeatsPlaced).Message).To(ContainSubstring(generalSet + " keep running"))
		Expect(company.Status.Topology.SeatPools).To(HaveLen(2), "a removed pool is not reported as one of the Company's")

		By("keeping a seat's own pods while the revision that runs pins the seat to them")
		Expect(*statefulSet(cySet).Spec.Replicas).To(Equal(int32(1)))
		updateLive(&crewletaiv1alpha1.Seat{Namespace: namespace, Name: "cy"}, func(s *crewletaiv1alpha1.Seat) {
			s.Spec.Placement.Pool = poolBatch
		})
		reconciled(company)
		get(&appsv1.StatefulSet{}, cySet)
		get(&corev1.Secret{}, isolatedBootstrap)

		By("deleting both once a revision that pins nothing to them runs")
		pinned(company, crewletaiv1alpha1.RevisionPlacement{Pools: []string{crewletaiv1alpha1.DataPool, poolBatch}})
		reconciled(company)
		Expect(seatSets()).To(ConsistOf(batchSet))
		absent(&policyv1.PodDisruptionBudget{}, generalSet)
		absent(&policyv1.PodDisruptionBudget{}, cySet)
		absent(&corev1.Secret{}, generalBootstrap)
		absent(&corev1.Secret{}, isolatedBootstrap)
	})

	It("deletes at once the pods of a pool no running revision pins anything to", func() {
		class := newClass()
		company := running(class.Name)
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Topology.SeatPools = c.Spec.Topology.SeatPools[1:] })
		reconciled(company)
		Expect(seatSets()).To(ConsistOf(batchSet, cySet))
	})

	It("runs no seat pod, pinned or not, and opens no broker port under a class that forbids seat pools", func() {
		class := newClass()
		company := running(class.Name)
		pinned(company, crewletaiv1alpha1.RevisionPlacement{Pools: []string{poolGeneral}, Seats: []string{"cy"}})
		hash := statefulSet(dataName).Spec.Template.Annotations[BootstrapHashAnnotation]
		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.SeatPools = crewletaiv1alpha1.SeatPoolSettings{} })
		company = reconciled(company)

		Expect(seatSets()).To(BeEmpty())
		absent(&corev1.Service{}, seatsService)
		absent(&networkingv1.NetworkPolicy{}, brokerPolicy)
		absent(&networkingv1.NetworkPolicy{}, seatsPolicy)
		absent(&corev1.Secret{}, generalBootstrap)
		data := statefulSet(dataName)
		Expect(data.Spec.Template.Annotations[BootstrapHashAnnotation]).NotTo(Equal(hash), "closing the leaf listener does not roll the data pods")
		Expect(data.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(1))
		dataSvc := &corev1.Service{}
		get(dataSvc, dataName)
		Expect(dataSvc.Spec.Ports).To(HaveLen(1))
		placed := condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonSeatPoolsNotAllowed))
	})

	It("keeps the pools as they are, and starts no pod, while the Shared pools pass the class's maxPods", func() {
		class := newClass(func(c *crewletaiv1alpha1.EngineClass) { c.Spec.SeatPools.MaxPods = 1 })
		company := running(class.Name)
		Expect(seatSets()).To(BeEmpty())
		placed := condition(company, crewletaiv1alpha1.ConditionSeatsPlaced)
		Expect(placed.Reason).To(Equal(crewletaiv1alpha1.ReasonPoolCapacityExceeded))
		Expect(placed.Message).To(ContainSubstring("spec.seatPools.maxPods"))

		By("leaving what already runs as it is")
		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.SeatPools.MaxPods = 0 })
		reconciled(company)
		Expect(seatSets()).To(ConsistOf(generalSet, batchSet, cySet))
		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) { c.Spec.SeatPools.MaxPods = 1 })
		updateCompany(company, func(c *crewletaiv1alpha1.Company) { c.Spec.Topology.SeatPools[0].Replicas = new(int32(5)) })
		reconciled(company)
		Expect(*statefulSet(generalSet).Spec.Replicas).To(Equal(int32(2)))
	})

	It("runs no pod for a seat its class refuses a PerSeat pool, nor one past maxPods", func() {
		class := newClass(func(c *crewletaiv1alpha1.EngineClass) { c.Spec.SeatPools.AllowPerSeat = nil })
		running(class.Name)
		Expect(seatSets()).To(ConsistOf(generalSet, batchSet))

		updateLive(class, func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.SeatPools.AllowPerSeat = new(true)
			c.Spec.SeatPools.MaxPods = 3
		})
		company := reconciled(&crewletaiv1alpha1.Company{Namespace: namespace, Name: poolsCompany})
		Expect(seatSets()).To(ConsistOf(generalSet, batchSet), "cy's two pods would make four")
		Expect(condition(company, crewletaiv1alpha1.ConditionSeatsPlaced).Message).To(ContainSubstring("1 Seat is refused"))
	})

	It("gives a PerSeat seat with a single pod no disruption budget", func() {
		class := newClass()
		running(class.Name, func(c *crewletaiv1alpha1.Company) { c.Spec.Topology.SeatPools[1].Replicas = new(int32(1)) })
		absent(&policyv1.PodDisruptionBudget{}, cySet)
		get(&policyv1.PodDisruptionBudget{}, generalSet)
	})

	It("admits the API port beside the broker's when the class does not isolate the engine", func() {
		class := newClass(func(c *crewletaiv1alpha1.EngineClass) {
			c.Spec.NetworkPolicy = crewletaiv1alpha1.NetworkPolicySettings{Mode: crewletaiv1alpha1.NetworkPolicyNone}
		})
		running(class.Name)
		absent(&networkingv1.NetworkPolicy{}, engineName)
		broker := &networkingv1.NetworkPolicy{}
		get(broker, brokerPolicy)
		Expect(broker.Spec.Ingress).To(HaveLen(2))
		Expect(broker.Spec.Ingress[1].Ports[0].Port.IntValue()).To(Equal(8080))
		Expect(broker.Spec.Ingress[1].From).To(BeEmpty(), "the API port is as open as it was without the policy")
		seats := &networkingv1.NetworkPolicy{}
		get(seats, seatsPolicy)
		Expect(seats.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
		Expect(seats.Spec.Ingress).To(BeEmpty())
	})

	It("deletes the seat pods before the data pods when the Company is deleted", func() {
		class := newClass()
		company := running(class.Name)
		pod := seatPod("nimbus-seats-general-0")
		Expect(k8sClient.Delete(ctx, company)).To(Succeed())

		result := reconcileOnce(company)
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))
		Expect(seatSets()).To(BeEmpty())
		data := statefulSet(dataName)
		Expect(data.DeletionTimestamp).To(BeNil(), "the data pods were stopped before the seat pods")

		result = reconcileOnce(company)
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)), "the data pods were stopped while a seat pod ran")
		Expect(statefulSet(dataName).DeletionTimestamp).To(BeNil())

		gone(pod)
		reconcileOnce(company)
		Eventually(func() bool {
			sts := &appsv1.StatefulSet{}
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dataName}, sts)
			return apierrors.IsNotFound(err) || !sts.DeletionTimestamp.IsZero()
		}).Should(BeTrue(), "the data StatefulSet is not being deleted once the seat pods are gone")
	})
})
