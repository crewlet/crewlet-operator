package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The EngineClass reconciler runs in a manager, so that what re-checks a
// class is what would in a cluster: a Secret or a Gateway it names appearing
// or changing. Its cache sees the platform namespace the classes name.
var _ = Describe("EngineClass controller in a manager", Ordered, func() {
	var platform string
	const settle = 30 * time.Second
	// value is the API key the Secret holds: it must never reach the
	// condition.
	const value = "sk-platform-value-never-shown"

	BeforeAll(func() {
		ns := &corev1.Namespace{GenerateName: "platform-"}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		platform = ns.Name

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Cache:                  cacheIn(platform),
			Controller:             config.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())
		gatewayAPI, err := GatewayAPIInstalled(mgr.GetRESTMapper())
		Expect(err).NotTo(HaveOccurred())
		Expect(gatewayAPI).To(BeTrue(), "the suite installs the Gateway API")
		Expect((&EngineClassReconciler{
			Client: mgr.GetClient(), APIReader: mgr.GetAPIReader(), GatewayAPI: gatewayAPI,
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

	// classNaming creates a class whose platform model provider's key is
	// apiKey of the Secret platform/llm, and whose webhooks route through
	// the Gateway platform/<publicGateway>.
	classNaming := func() *crewletaiv1alpha1.EngineClass {
		GinkgoHelper()
		class := &crewletaiv1alpha1.EngineClass{
			GenerateName: "checked-",
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("16Gi"))},
				},
				Exposure: crewletaiv1alpha1.ExposureSettings{Webhooks: crewletaiv1alpha1.WebhookExposure{
					Mode:     crewletaiv1alpha1.WebhookExposureGateway,
					Hostname: "{{uid}}.hooks.example.com",
					Gateway: crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{
						{Namespace: platform, Name: publicGateway},
					}},
				}},
				PlatformLLMProviders: []crewletaiv1alpha1.PlatformLLMProvider{{
					Name: "platform-claude", Type: crewletaiv1alpha1.LLMProviderAnthropic, Model: "claude-sonnet-5",
					APIKeySecretRefs: []crewletaiv1alpha1.NamespacedSecretKeyReference{{Namespace: platform, Name: "llm", Key: "apiKey"}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })
		return class
	}
	accepted := func(class *crewletaiv1alpha1.EngineClass) func(Gomega) metav1.Condition {
		return func(g Gomega) metav1.Condition {
			got := &crewletaiv1alpha1.EngineClass{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), got)).To(Succeed())
			g.Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
			c := meta.FindStatusCondition(got.Status.Conditions, crewletaiv1alpha1.ConditionAccepted)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.ObservedGeneration).To(Equal(got.Generation))
			return *c
		}
	}
	reason := func(class *crewletaiv1alpha1.EngineClass) func(Gomega) string {
		return func(g Gomega) string { return accepted(class)(g).Reason }
	}

	It("accepts a class once every Secret key and Gateway it names exists, and never shows a value", func() {
		class := classNaming()

		By("naming a Secret that does not exist")
		Eventually(reason(class)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSecretNotFound))
		c := accepted(class)(Default)
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Message).To(ContainSubstring("Secret " + platform + "/llm does not exist"))
		Expect(c.Message).To(ContainSubstring("Gateway "+platform+"/"+publicGateway+" does not exist"),
			"every finding is reported, not only the first")

		By("creating the Secret without the key")
		secret := &corev1.Secret{Namespace: platform, Name: "llm", StringData: map[string]string{"unrelated": value}}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		Eventually(func(g Gomega) string { return accepted(class)(g).Message }).WithTimeout(settle).
			Should(ContainSubstring(`Secret ` + platform + `/llm has no key "apiKey"`))

		By("adding the key")
		updateLive(secret, func(s *corev1.Secret) { s.Data = map[string][]byte{"apiKey": []byte(value)} })
		Eventually(reason(class)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonGatewayNotFound))

		By("creating the Gateway")
		gateway := &gatewayv1.Gateway{
			Namespace: platform, Name: publicGateway,
			Spec: gatewayv1.GatewaySpec{
				GatewayClassName: "example",
				Listeners:        []gatewayv1.Listener{{Name: "http", Port: 80, Protocol: gatewayv1.HTTPProtocolType}},
			},
		}
		Expect(k8sClient.Create(ctx, gateway)).To(Succeed())
		Eventually(reason(class)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonValid))
		Expect(accepted(class)(Default).Status).To(Equal(metav1.ConditionTrue))

		By("deleting the Secret again")
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		Eventually(reason(class)).WithTimeout(settle).Should(Equal(crewletaiv1alpha1.ReasonSecretNotFound))

		Expect(k8sClient.Delete(ctx, gateway)).To(Succeed())
		got := &crewletaiv1alpha1.EngineClass{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), got)).To(Succeed())
		for _, condition := range got.Status.Conditions {
			Expect(condition.Message).NotTo(ContainSubstring(value))
		}
	})
})

var _ = Describe("EngineClass controller", func() {
	It("refuses a class that routes through Gateways on a cluster without the Gateway API", func() {
		class := &crewletaiv1alpha1.EngineClass{
			GenerateName: "no-gateway-api-",
			Spec: crewletaiv1alpha1.EngineClassSpec{
				Engine: crewletaiv1alpha1.EngineSettings{Image: engineImageV020},
				Data: crewletaiv1alpha1.DataPodSettings{
					Storage: crewletaiv1alpha1.DataStorage{Size: new(resource.MustParse("16Gi"))},
				},
				Exposure: crewletaiv1alpha1.ExposureSettings{Webhooks: crewletaiv1alpha1.WebhookExposure{
					Mode:     crewletaiv1alpha1.WebhookExposureGateway,
					Hostname: "{{uid}}.hooks.example.com",
					Gateway: crewletaiv1alpha1.GatewayExposure{ParentRefs: []crewletaiv1alpha1.GatewayParentReference{
						{Name: "in-each-company-namespace"},
					}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, class)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, class)).To(Succeed()) })

		r := &EngineClassReconciler{Client: k8sClient, APIReader: k8sClient, GatewayAPI: false}
		_, err := r.Reconcile(ctx, ctrl.Request{Name: class.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), class)).To(Succeed())
		c := meta.FindStatusCondition(class.Status.Conditions, crewletaiv1alpha1.ConditionAccepted)
		Expect(c).NotTo(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionFalse))
		Expect(c.Reason).To(Equal(crewletaiv1alpha1.ReasonGatewayAPINotInstalled))

		By("accepting it on a cluster that has it: a parentRef without a namespace is in each Company's own")
		r.GatewayAPI = true
		_, err = r.Reconcile(ctx, ctrl.Request{Name: class.Name})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(class), class)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(class.Status.Conditions, crewletaiv1alpha1.ConditionAccepted)).To(BeTrue())
	})

	It("reconciles a class that no longer exists", func() {
		r := &EngineClassReconciler{Client: k8sClient, APIReader: k8sClient}
		_, err := r.Reconcile(ctx, ctrl.Request{Name: "gone"})
		Expect(err).NotTo(HaveOccurred())
	})
})
