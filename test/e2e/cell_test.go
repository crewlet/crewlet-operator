//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/utils"
)

// A hosted cell's tenant: its namespace, labelled as the platform's
// provisioner labels one, its company, and the tier egress proxy it leaves
// the cluster through.
const (
	cellNamespace  = "t-e2ecell"
	cellTenant     = "e2ecell"
	cellCompany    = "cell"
	cellClass      = "e2e-cell"
	cellTier       = "t1"
	proxyRelease   = "e2e"
	proxySigning   = "egress-signing-key"
	proxyPort      = 3128
	outsideWorld   = "https://github.com"
	proxyURLKey    = "egress.proxyURL"
	cellDataLabels = "data"
)

var (
	cellBootstrap = cellCompany + "-bootstrap"
	cellKey       = types.NamespacedName{Namespace: cellNamespace, Name: cellCompany}
)

var _ = Describe("A hosted cell's tenant", Ordered, func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	BeforeAll(func() {
		By("running the tier egress proxy from the chart, on the manager image")
		apply(&corev1.Secret{Namespace: namespace, Name: proxySigning, StringData: map[string]string{
			"signing-key": "e2e signing key, not a secret",
		}})
		repository, tag, _ := strings.Cut(managerImage, ":")
		render := exec.Command("helm", "template", proxyRelease, "dist/chart", "--namespace", namespace,
			"--set", "egressProxy.enabled=true",
			"--set", "egressProxy.signingKeySecret.name="+proxySigning,
			"--set", "manager.image.repository="+repository,
			"--set", "manager.image.tag="+tag,
			"--set", "egressProxy.tiers[0].name="+cellTier,
			"--set", "egressProxy.tiers[0].trustTiers[0]="+cellTier,
			"--set", "egressProxy.tiers[0].replicas=1",
			"--show-only", "templates/egress-proxy/rbac.yaml",
			"--show-only", "templates/egress-proxy/tiers.yaml")
		rendered, err := utils.Run(render)
		Expect(err).NotTo(HaveOccurred(), "rendering the egress proxy")
		kubectl := exec.Command("kubectl", "apply", "-f", "-")
		kubectl.Stdin = strings.NewReader(rendered)
		_, err = utils.Run(kubectl)
		Expect(err).NotTo(HaveOccurred(), "applying the egress proxy")
		wait := exec.Command("kubectl", "rollout", "status", "-n", namespace, "--timeout=3m",
			"deploy/"+proxyName())
		_, err = utils.Run(wait)
		Expect(err).NotTo(HaveOccurred(), "the egress proxy never became ready")

		By("creating the tenant's namespace as the provisioner labels one")
		ns := restrictedNamespace(cellNamespace)
		ns.Labels["crewlet.ai/tenant"] = cellTenant
		ns.Labels["crewlet.ai/trust-tier"] = cellTier
		apply(ns)

		By("creating a class that sends every connection out through the proxy")
		inRelease := &metav1.LabelSelector{MatchLabels: map[string]string{corev1.LabelMetadataName: namespace}}
		apply(&v1.EngineClass{
			Name: cellClass,
			Spec: v1.EngineClassSpec{
				AllowedNamespaces: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key: "crewlet.ai/tenant", Operator: metav1.LabelSelectorOpExists,
				}}},
				Engine: v1.EngineSettings{Image: crewletImage, ImagePullPolicy: corev1.PullNever},
				Data: v1.DataPodSettings{
					Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					}},
					Storage: v1.DataStorage{Size: new(resource.MustParse("16Gi"))},
				},
				Exposure: v1.ExposureSettings{Webhooks: v1.WebhookExposure{Mode: v1.WebhookExposureNone}},
				NetworkPolicy: v1.NetworkPolicySettings{
					Mode: v1.NetworkPolicyIsolated,
					Egress: v1.NetworkPolicyEgress{Proxy: v1.EgressProxy{
						Host: fmt.Sprintf("%s.%s.svc", proxyName(), namespace),
						Port: proxyPort,
						Peers: []v1.NetworkPolicyPeer{{
							NamespaceSelector: inRelease,
							PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								"crewlet.ai/egress-tier": cellTier,
							}},
						}},
						SigningKeySecretRef: v1.NamespacedSecretKeyReference{
							Namespace: namespace, Name: proxySigning, Key: "signing-key",
						},
					}},
				},
				Retention: v1.RetentionSettings{BackupOwner: "e2e"},
			},
		})
		apply(&v1.Company{
			Namespace: cellNamespace, Name: cellCompany,
			Spec: v1.CompanySpec{EngineClassName: cellClass, CompanyName: "Cell"},
		})
	})

	AfterAll(func() {
		// A probe a failed spec left behind would hold the Company's deletion.
		for _, probe := range []string{"direct", "cluster", "proxied", "quarantined", "released"} {
			left := &corev1.Pod{Namespace: cellNamespace, Name: "egress-" + probe}
			_ = k8sClient.Delete(ctx, left, client.GracePeriodSeconds(0))
		}
		_ = k8sClient.Delete(ctx, &v1.Company{Namespace: cellNamespace, Name: cellCompany})
		Eventually(func() bool {
			err := k8sClient.Get(ctx, cellKey, &v1.Company{})
			return apierrors.IsNotFound(err)
		}).WithTimeout(engineReady).Should(BeTrue())
		_ = k8sClient.Delete(ctx, &v1.EngineClass{Name: cellClass})
		_ = k8sClient.Delete(ctx, &corev1.Namespace{Name: cellNamespace})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpNamespace(cellNamespace, cellCompany+"-data-0")
			dump("logs", "deploy/"+proxyName(), "-n", namespace, "--tail=200")
		}
	})

	It("becomes Ready with no route outside the cluster", func() {
		company := &v1.Company{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, cellKey, company)).To(Succeed())
			ready := meta.FindStatusCondition(company.Status.Conditions, v1.ConditionReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue),
				"Ready=%s %s: %s", ready.Status, ready.Reason, ready.Message)
		}).WithTimeout(engineReady).Should(Succeed())
		Expect(company.Status.Endpoints.PublicBaseURL).To(BeEmpty(), "a cell's engine has no public address")
		Expect(company.Status.Endpoints.API).To(HaveSuffix("." + cellNamespace + ".svc:8080"))

		ingresses := &networkingv1.IngressList{}
		Expect(k8sClient.List(ctx, ingresses, client.InNamespace(cellNamespace))).To(Succeed())
		Expect(ingresses.Items).To(BeEmpty())
		services := &corev1.ServiceList{}
		Expect(k8sClient.List(ctx, services, client.InNamespace(cellNamespace))).To(Succeed())
		for _, s := range services.Items {
			Expect(s.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP),
				"Service %s is reachable from outside the cluster", s.Name)
		}
	})

	It("reaches the internet through the proxy alone, and no other namespace", func() {
		Expect(egressProbe("direct", `curl -sS --max-time 8 -o /dev/null `+outsideWorld)).
			To(Equal(corev1.PodFailed), "an engine pod connected out without the proxy")
		Expect(egressProbe("cluster", `curl -sSk --max-time 8 -o /dev/null https://kubernetes.default.svc`)).
			To(Equal(corev1.PodFailed), "an engine pod reached the Kubernetes API")
		Expect(egressProbe("proxied", `curl -sSf --max-time 20 -o /dev/null -x "$PROXY" `+outsideWorld)).
			To(Equal(corev1.PodSucceeded), "an engine pod could not connect out through the proxy")
	})

	It("is cut off at once by a quarantine, its pods kept", func() {
		dataPodUID := podUID(cellCompany + "-data-0")
		setQuarantine(v1.QuarantineEgress)
		Eventually(func(g Gomega) {
			company := &v1.Company{}
			g.Expect(k8sClient.Get(ctx, cellKey, company)).To(Succeed())
			g.Expect(conditionStatus(company.Status.Conditions, v1.ConditionQuarantined)).To(Equal(metav1.ConditionTrue))
		}).Should(Succeed())
		Expect(egressProbe("quarantined", `curl -sSf --max-time 20 -o /dev/null -x "$PROXY" `+outsideWorld)).
			To(Equal(corev1.PodFailed), "a quarantined engine pod still connected out")
		Expect(podUID(cellCompany+"-data-0")).To(Equal(dataPodUID), "the quarantine restarted the data pod")

		setQuarantine("")
		Eventually(func() corev1.PodPhase {
			return egressProbe("released", `curl -sSf --max-time 20 -o /dev/null -x "$PROXY" `+outsideWorld)
		}).Should(Equal(corev1.PodSucceeded), "lifting the quarantine did not restore the proxy")
	})
})

// proxyName is the tier proxy's Deployment and Service.
func proxyName() string {
	return proxyRelease + "-crewlet-operator-egress-proxy-" + cellTier
}

// egressProbe runs a curl pod named after what it probes, with the labels of
// the company's data pods, so that the operator's policy around them holds
// it, and PROXY set to the proxy address the data pods are given; it returns
// how the pod ended. StatefulSets adopt only pods named after their ordinals,
// so the data StatefulSet leaves it alone.
func egressProbe(name, script string) corev1.PodPhase {
	GinkgoHelper()
	name = "egress-" + name
	_ = k8sClient.Delete(ctx, &corev1.Pod{Namespace: cellNamespace, Name: name}, client.GracePeriodSeconds(0))
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: cellNamespace, Name: name}, &corev1.Pod{})
		return apierrors.IsNotFound(err)
	}).Should(BeTrue())
	// A CNI programs a new pod's policies after the pod starts, and lets its
	// first packets through until then; the wait keeps the probe out of that
	// window, so it tests the policy rather than the CNI's latency.
	pod := curlPodWith(cellNamespace, name, cellBootstrap, "sleep 15; "+script)
	pod.Labels = map[string]string{
		"app.kubernetes.io/name": "crewlet", "app.kubernetes.io/instance": cellCompany,
		"app.kubernetes.io/component": cellDataLabels, v1.CompanyLabel: cellCompany,
	}
	pod.Spec.Containers[0].Env = []corev1.EnvVar{secretEnv("PROXY", cellBootstrap, proxyURLKey)}
	apply(pod)
	phase := curlPhase(cellNamespace, name)
	// Gone again at once: the operator stops a deleted Company only once no
	// pod with its data pods' labels is left.
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)))).To(Succeed())
	return phase
}

// setQuarantine sets the tenant namespace's quarantine label, or removes it
// when value is empty, as the platform does on an abuse case.
func setQuarantine(value string) {
	GinkgoHelper()
	ns := &corev1.Namespace{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cellNamespace}, ns)).To(Succeed())
	base := ns.DeepCopy()
	if value == "" {
		delete(ns.Labels, v1.QuarantineLabel)
	} else {
		ns.Labels[v1.QuarantineLabel] = value
	}
	Expect(k8sClient.Patch(ctx, ns, client.MergeFrom(base))).To(Succeed())
}

// podUID is the UID of a pod of the tenant's namespace.
func podUID(name string) types.UID {
	GinkgoHelper()
	pod := &corev1.Pod{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: cellNamespace, Name: name}, pod)).To(Succeed())
	return pod.UID
}
