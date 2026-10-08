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
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

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
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "nimbus-data"}, sts)).To(Succeed())
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
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKeyFromObject(company), &crewletaiv1alpha1.Company{})
		}).WithTimeout(settle).Should(Satisfy(apierrors.IsNotFound))
	})
})
