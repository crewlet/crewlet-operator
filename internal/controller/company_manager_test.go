package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// collectOrphaned stands in for the garbage collector, which envtest does
// not run, for a Company deleted with deletionPolicy Retain: the operator
// deletes its data StatefulSet with the Orphan propagation policy, and the
// collector's part is to drop the orphan finalizer once it has released the
// dependents.
func collectOrphaned(namespace string, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		sts := &appsv1.StatefulSet{}
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dataName}, sts)
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(sts.DeletionTimestamp).NotTo(BeNil(), "the StatefulSet is not being deleted yet")
		g.Expect(sts.Finalizers).To(ContainElement(metav1.FinalizerOrphanDependents))
		sts.Finalizers = nil
		g.Expect(k8sClient.Update(ctx, sts)).To(Succeed())
	}).WithTimeout(timeout).Should(Succeed())
}

// The other specs call Reconcile directly; this one runs the controller in a
// manager, as cmd/main.go does, so that its watches are what drives it: a
// Company's creation, the status of the StatefulSet it owns, and the
// ConfigApplied condition the config reconciler writes. Its cache sees one
// namespace only, so it leaves the other specs' Companies alone.
var _ = Describe("Company controller in a manager", Ordered, func() {
	var namespace string

	// The manager starts its informers before the first event arrives.
	const settle = 30 * time.Second

	BeforeAll(func() {
		ns := &corev1.Namespace{GenerateName: "manager-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		})
		Expect(err).NotTo(HaveOccurred())
		gatewayAPI, err := GatewayAPIInstalled(mgr.GetRESTMapper())
		Expect(err).NotTo(HaveOccurred())
		Expect((&CompanyReconciler{
			Client:            mgr.GetClient(),
			APIReader:         mgr.GetAPIReader(),
			GatewayAPI:        gatewayAPI,
			OperatorNamespace: operatorNamespace,
		}).SetupWithManager(mgr)).To(Succeed())

		managerCtx, stop := context.WithCancel(ctx)
		done := make(chan error)
		go func() {
			defer GinkgoRecover()
			done <- mgr.Start(managerCtx)
		}()
		DeferCleanup(func() {
			stop()
			Expect(<-done).To(Succeed())
		})
	})

	It("runs a Company's engine and follows its pods and its document", func() {
		class := &crewletaiv1alpha1.EngineClass{
			Name: namespace,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace,
			Name:      companyName,
			Spec:      crewletaiv1alpha1.CompanySpec{CompanyName: companyDisplayName, EngineClassName: class.Name},
		}
		Expect(k8sClient.Create(ctx, company)).To(Succeed())

		conditionOf := func(t string) func(Gomega) string {
			return func(g Gomega) string {
				got := &crewletaiv1alpha1.Company{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
				c := meta.FindStatusCondition(got.Status.Conditions, t)
				g.Expect(c).NotTo(BeNil())
				return c.Reason
			}
		}
		Eventually(conditionOf(crewletaiv1alpha1.ConditionRuntimeReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPodsNotReady))

		By("following the StatefulSet's status, as its controller would report it")
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dataName}, sts)).To(Succeed())
		sts.Status = appsv1.StatefulSetStatus{
			ObservedGeneration: sts.Generation, Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
			UpdatedReplicas: 1, CurrentReplicas: 1, CurrentRevision: "r1", UpdateRevision: "r1",
		}
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
		Eventually(conditionOf(crewletaiv1alpha1.ConditionRuntimeReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonPodsReady))
		Expect(conditionOf(crewletaiv1alpha1.ConditionReady)(Default)).To(Equal(crewletaiv1alpha1.ReasonConfigPending))

		By("following the ConfigApplied condition, as the config reconciler would write it")
		Eventually(func(g Gomega) {
			got := &crewletaiv1alpha1.Company{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
			base := got.DeepCopy()
			meta.SetStatusCondition(&got.Status.Conditions, metav1.Condition{
				Type: crewletaiv1alpha1.ConditionConfigApplied, Status: metav1.ConditionTrue,
				Reason: crewletaiv1alpha1.ReasonApplied, Message: "Revision r1 is active.", ObservedGeneration: got.Generation,
			})
			g.Expect(k8sClient.Status().Patch(ctx, got, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))).To(Succeed())
		}).WithTimeout(settle).Should(Succeed())
		Eventually(conditionOf(crewletaiv1alpha1.ConditionReady)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonAvailable))

		By("releasing the Company when it is deleted")
		Expect(k8sClient.Delete(ctx, company)).To(Succeed())
		collectOrphaned(namespace, settle)
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(company), &crewletaiv1alpha1.Company{})
		}).WithTimeout(settle).Should(Satisfy(apierrors.IsNotFound))
	})
})

// Both reconcilers in one manager, as cmd/main.go runs them, with a real
// engine standing in for the data pod: the specs above stand in for one
// reconciler each, and what this one checks lives between the two, in the
// status they share and the predicates each passes for the other's writes.
var _ = Describe("Company and config controllers in one manager", Ordered, func() {
	var (
		namespace string
		engines   = &testEngines{}
	)
	const settle = 60 * time.Second

	BeforeAll(func() {
		ns := &corev1.Namespace{GenerateName: "both-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		namespace = ns.Name

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
			// The other specs register controllers of the same names in
			// managers of their own.
			Controller: config.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())
		gatewayAPI, err := GatewayAPIInstalled(mgr.GetRESTMapper())
		Expect(err).NotTo(HaveOccurred())
		Expect((&CompanyReconciler{
			Client:            mgr.GetClient(),
			APIReader:         mgr.GetAPIReader(),
			GatewayAPI:        gatewayAPI,
			OperatorNamespace: operatorNamespace,
		}).SetupWithManager(mgr)).To(Succeed())
		Expect((&CompanyConfigReconciler{
			Client:    mgr.GetClient(),
			APIReader: mgr.GetAPIReader(),
			Engines:   engines,
			Recorder:  &eventLog{},
		}).SetupWithManager(mgr)).To(Succeed())

		managerCtx, stop := context.WithCancel(ctx)
		done := make(chan error)
		go func() {
			defer GinkgoRecover()
			done <- mgr.Start(managerCtx)
		}()
		DeferCleanup(func() {
			stop()
			Expect(<-done).To(Succeed())
		})
	})

	It("reports Ready for a generation only once that generation's document is applied", func() {
		class := &crewletaiv1alpha1.EngineClass{
			Name: namespace,
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("10Gi"))},
				},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })
		company := &crewletaiv1alpha1.Company{
			Namespace: namespace,
			Name:      companyName,
			Spec:      crewletaiv1alpha1.CompanySpec{CompanyName: companyDisplayName, EngineClassName: class.Name},
		}
		engines.set(client.ObjectKeyFromObject(company), enginetest.Start(GinkgoT()))
		Expect(k8sClient.Create(ctx, company)).To(Succeed())

		// readyAt waits until Ready is True for the generation given and
		// checks, on every observation, the invariant: Ready never claims a
		// generation whose document ConfigApplied does not report.
		readyAt := func(generation int64) {
			GinkgoHelper()
			Eventually(func(g Gomega) {
				got := &crewletaiv1alpha1.Company{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), got)).To(Succeed())
				ready := meta.FindStatusCondition(got.Status.Conditions, crewletaiv1alpha1.ConditionReady)
				applied := meta.FindStatusCondition(got.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied)
				if ready != nil && ready.Status == metav1.ConditionTrue {
					Expect(applied).NotTo(BeNil())
					Expect(applied.ObservedGeneration).To(BeNumerically(">=", ready.ObservedGeneration),
						"Ready is True for generation %d while ConfigApplied describes generation %d", ready.ObservedGeneration, applied.ObservedGeneration)
				}
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.ObservedGeneration).To(Equal(generation))
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue), "%s: %s", ready.Reason, ready.Message)
			}).WithTimeout(settle).WithPolling(50 * time.Millisecond).Should(Succeed())
		}

		By("running the engine, as the StatefulSet controller would report it")
		sts := &appsv1.StatefulSet{}
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: dataStatefulSetName(company)}, sts)
		}).WithTimeout(settle).Should(Succeed())
		sts.Status = appsv1.StatefulSetStatus{
			ObservedGeneration: sts.Generation, Replicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
			UpdatedReplicas: 1, CurrentReplicas: 1, CurrentRevision: "r1", UpdateRevision: "r1",
		}
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), company)).To(Succeed())
		readyAt(company.Generation)

		By("changing the spec, which the engine takes as a new revision")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(company), company)).To(Succeed())
		company.Spec.Mission = "Answer every customer within a day."
		Expect(k8sClient.Update(ctx, company)).To(Succeed())
		readyAt(company.Generation)

		By("releasing the Company when it is deleted")
		Expect(k8sClient.Delete(ctx, company)).To(Succeed())
		collectOrphaned(namespace, settle)
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(company), &crewletaiv1alpha1.Company{})
		}).WithTimeout(settle).Should(Satisfy(apierrors.IsNotFound))
	})
})
