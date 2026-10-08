//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/utils"
)

// The company the suite runs: a namespace of its own, as a platform gives
// each tenant, under the restricted Pod Security Standard the engine is
// built to run under.
const (
	tenantNamespace   = "e2e-acme"
	consumerNamespace = "e2e-consumer"
	className         = "e2e"
	companyName       = "acme"
	providerSecret    = "model-gateway"

	// fieldManager is the suite's field manager. It writes the objects the
	// way a control plane does: server-side apply of typed objects.
	fieldManager = "e2e-control-plane"

	// engineReady bounds how long an engine may take to start, be given
	// its first document and become ready, image pull excluded (the image
	// is loaded into kind).
	engineReady = 5 * time.Minute
)

// Objects of the company the operator creates, by the names the docs give.
var (
	dataPod       = companyName + "-data-0"
	dataClaim     = "data-" + companyName + "-data-0"
	statefulSet   = companyName + "-data"
	keyringSecret = companyName + "-keyring"
	tokensSecret  = companyName + "-api-tokens"
	accessSecret  = companyName + "-api-access"
	engineService = companyName + "-engine"

	// engineURL is the engine API's in-cluster address.
	engineURL = fmt.Sprintf("http://%s.%s.svc:8080", engineService, tenantNamespace)
)

var _ = Describe("Company", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	BeforeAll(func() {
		By("creating the tenant's namespace, enforcing the restricted Pod Security Standard")
		apply(&corev1.Namespace{
			Name:   tenantNamespace,
			Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
		})

		By("creating an EngineClass that runs the engine image built from source")
		apply(&v1.EngineClass{
			Name: className,
			Spec: v1.EngineClassSpec{
				Engine: v1.EngineSettings{Image: crewletImage},
				Data: v1.DataPodSettings{
					Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					}},
					// The smallest volume the API admits. kind's
					// provisioner does not enforce it; the engine's
					// event stream is sized from it all the same.
					Storage: v1.DataStorage{Size: new(resource.MustParse("7Gi"))},
				},
				Exposure: v1.ExposureSettings{Webhooks: v1.WebhookExposure{Mode: v1.WebhookExposureNone}},
			},
		})
	})

	AfterAll(func() {
		By("deleting the tenant's objects while the operator runs to finalize them")
		_ = k8sClient.Delete(ctx, &v1.Company{Namespace: tenantNamespace, Name: companyName})
		for _, ns := range []string{tenantNamespace, consumerNamespace} {
			_ = k8sClient.Delete(ctx, &corev1.Namespace{Name: ns})
		}
		Eventually(func(g Gomega) {
			for _, ns := range []string{tenantNamespace, consumerNamespace} {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ns}, &corev1.Namespace{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "namespace %s is still there", ns)
			}
		}).WithTimeout(engineReady).Should(Succeed())
		_ = k8sClient.Delete(ctx, &v1.EngineClass{Name: className})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpTenant()
		}
	})

	It("runs a company: the engine is ready and runs the rendered document", func() {
		By("writing the company's objects and the Secret its model provider reads")
		applyCompany(v1.DeletionPolicyRetain)

		By("waiting for every condition of the Company to be True")
		company := waitForCompanyReady()
		Expect(company.Status.Config.RevisionID).NotTo(BeEmpty(), "ConfigApplied=True names no revision")
		Expect(company.Status.Config.Objects).To(Equal(v1.AppliedObjectCounts{
			Units: new(int32(1)), Seats: new(int32(2)), LLMProviders: new(int32(1)),
			MCPServers: new(int32(0)), Integrations: new(int32(0)),
		}))
		Expect(company.Status.Endpoints.API).To(Equal(engineURL))
		Expect(company.Status.APIAccessSecretRef.Name).To(Equal(accessSecret))

		By("finding the data pod Ready")
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: dataPod}, pod)).To(Succeed())
		Expect(podReady(pod)).To(BeTrue(), "the data pod is not Ready")

		By("reading each Seat's identity in the engine and the revision it is in")
		for _, name := range []string{"lead", "dev"} {
			seat := &v1.Seat{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: name}, seat)).To(Succeed())
			Expect(seat.Status.Handle).To(Equal(name))
			Expect(seat.Status.SeatID).NotTo(BeEmpty(), "Seat %s reports no seatID", name)
			applied := meta.FindStatusCondition(seat.Status.Conditions, v1.ConditionApplied)
			Expect(applied).NotTo(BeNil())
			Expect(applied.Status).To(Equal(metav1.ConditionTrue), "Seat %s: %s", name, applied.Message)
			Expect(applied.Message).To(ContainSubstring(company.Status.Config.RevisionID))
		}
	})

	It("lets a consumer read the engine's API with the published token", func() {
		By("copying the API access Secret into a namespace labelled as an API consumer")
		access := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: accessSecret}, access)).To(Succeed())
		Expect(access.Data).To(HaveKey(v1.APIAccessTokenKey))
		Expect(string(access.Data[v1.APIAccessURLKey])).To(Equal(engineURL))
		apply(&corev1.Namespace{
			Name: consumerNamespace,
			Labels: map[string]string{
				v1.APIConsumerNamespaceLabel:         "true",
				"pod-security.kubernetes.io/enforce": "restricted",
			},
		})
		apply(&corev1.Secret{Namespace: consumerNamespace, Name: accessSecret, Data: access.Data})

		By("reading the active company document with the token")
		// The token reaches curl from the Secret, through the environment:
		// never as a literal in the pod.
		script := `curl -fsS -H "Authorization: Bearer ${TOKEN}" "${URL}/config"`
		apply(&corev1.Pod{
			Namespace: consumerNamespace, Name: "read-config",
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot:   new(true),
					RunAsUser:      new(int64(1000)),
					SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				},
				Containers: []corev1.Container{{
					Name:    "curl",
					Image:   curlImage,
					Command: []string{"/bin/sh", "-c", script},
					Env: []corev1.EnvVar{
						secretEnv("TOKEN", accessSecret, v1.APIAccessTokenKey),
						secretEnv("URL", accessSecret, v1.APIAccessURLKey),
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: new(false),
						ReadOnlyRootFilesystem:   new(true),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					},
				}},
			},
		})
		Eventually(func(g Gomega) {
			pod := &corev1.Pod{}
			key := types.NamespacedName{Namespace: consumerNamespace, Name: "read-config"}
			g.Expect(k8sClient.Get(ctx, key, pod)).To(Succeed())
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodSucceeded), "the consumer's request did not succeed")
		}).Should(Succeed())
		out, err := utils.Run(exec.Command("kubectl", "logs", "read-config", "-n", consumerNamespace))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("Acme"), "the engine's active document is not the company's")
	})

	It("applies an edited Seat as a new revision", func() {
		before := getCompany().Status.Config.RevisionID

		By("changing the goal of Seat lead")
		apply(leadSeat("Lead engineering, and ship every week."))

		By("waiting for a new revision that holds the Seat's new generation")
		seat := &v1.Seat{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: "lead"}, seat)).To(Succeed())
		Expect(seat.Generation).To(BeNumerically(">", 1))
		Eventually(func(g Gomega) {
			company := getCompany()
			g.Expect(company.Status.Config.RevisionID).NotTo(Equal(before), "no new revision was applied")
			g.Expect(conditionStatus(company.Status.Conditions, v1.ConditionConfigApplied)).To(Equal(metav1.ConditionTrue))

			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(seat), seat)).To(Succeed())
			applied := meta.FindStatusCondition(seat.Status.Conditions, v1.ConditionApplied)
			g.Expect(applied).NotTo(BeNil())
			g.Expect(applied.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(applied.ObservedGeneration).To(Equal(seat.Generation))
			g.Expect(applied.Message).To(ContainSubstring(company.Status.Config.RevisionID))
		}).Should(Succeed())
	})

	It("keeps the volume and the keyring when a Retain company is deleted, and adopts them again", func() {
		claim := getObject(&corev1.PersistentVolumeClaim{}, dataClaim)
		keyring := getObject(&corev1.Secret{}, keyringSecret)

		By("deleting the Company")
		Expect(k8sClient.Delete(ctx, getCompany())).To(Succeed())
		waitForCompanyGone()

		By("finding the engine stopped and its owned objects gone")
		Eventually(func(g Gomega) {
			g.Expect(gone(&appsv1.StatefulSet{}, statefulSet)).To(BeTrue(), "the StatefulSet is still there")
			g.Expect(gone(&corev1.Pod{}, dataPod)).To(BeTrue(), "the data pod is still there")
			g.Expect(gone(&corev1.Secret{}, tokensSecret)).To(BeTrue(), "the API tokens Secret is still there")
			g.Expect(gone(&corev1.Service{}, engineService)).To(BeTrue(), "the engine Service is still there")
		}).Should(Succeed())

		By("finding the volume claim and the keyring kept, the keyring released from the Company")
		Expect(getObject(&corev1.PersistentVolumeClaim{}, dataClaim).GetUID()).To(Equal(claim.GetUID()))
		kept := getObject(&corev1.Secret{}, keyringSecret)
		Expect(kept.GetUID()).To(Equal(keyring.GetUID()))
		Expect(kept.GetOwnerReferences()).To(BeEmpty())

		By("creating the Company again, now with deletionPolicy Delete")
		applyCompany(v1.DeletionPolicyDelete)
		company := waitForCompanyReady()
		Expect(company.Status.Config.RevisionID).NotTo(BeEmpty())
		Expect(getObject(&corev1.PersistentVolumeClaim{}, dataClaim).GetUID()).To(Equal(claim.GetUID()),
			"the new Company does not run on the kept volume")
		adopted := getObject(&corev1.Secret{}, keyringSecret)
		Expect(adopted.GetUID()).To(Equal(keyring.GetUID()), "the new Company does not use the kept keyring")
		Expect(metav1.IsControlledBy(adopted, company)).To(BeTrue(), "the kept keyring was not adopted")
	})

	It("deletes the volume and the keyring with a Delete company", func() {
		By("deleting the Company")
		Expect(k8sClient.Delete(ctx, getCompany())).To(Succeed())
		waitForCompanyGone()

		By("finding everything the operator created for it gone")
		Eventually(func(g Gomega) {
			g.Expect(gone(&appsv1.StatefulSet{}, statefulSet)).To(BeTrue(), "the StatefulSet is still there")
			g.Expect(gone(&corev1.PersistentVolumeClaim{}, dataClaim)).To(BeTrue(), "the data volume claim is still there")
			g.Expect(gone(&corev1.Secret{}, keyringSecret)).To(BeTrue(), "the keyring Secret is still there")
			g.Expect(gone(&corev1.Secret{}, tokensSecret)).To(BeTrue(), "the API tokens Secret is still there")
			g.Expect(gone(&corev1.Secret{}, accessSecret)).To(BeTrue(), "the API access Secret is still there")
			g.Expect(gone(&corev1.Service{}, engineService)).To(BeTrue(), "the engine Service is still there")
		}).Should(Succeed())
	})
})

// applyCompany writes the company the suite runs: the Company, one Unit, two
// Seats in it, and an OpenAI-compatible model provider whose key is in a
// Secret. The provider's address is never called: the engine validates and
// applies a document without reaching its model providers.
func applyCompany(deletion v1.DeletionPolicy) {
	GinkgoHelper()
	apply(&corev1.Secret{
		Namespace: tenantNamespace, Name: providerSecret,
		//nolint:gosec // G101: a dummy key; the engine stores it and never sends it, since nothing calls the provider.
		StringData: map[string]string{"apiKey": "e2e-not-a-real-key"},
	})
	apply(&v1.Company{
		Namespace: tenantNamespace, Name: companyName,
		Spec: v1.CompanySpec{
			EngineClassName: className,
			CompanyName:     "Acme",
			Mission:         "Prove the operator end to end.",
			DeletionPolicy:  deletion,
		},
	})
	apply(&v1.LLMProvider{
		Namespace: tenantNamespace, Name: "gateway",
		Spec: v1.LLMProviderSpec{
			CompanyRef: v1.CompanyReference{Name: companyName},
			LLMProviderSettings: v1.LLMProviderSettings{
				Type:    v1.LLMProviderOpenAICompatible,
				Model:   "e2e-model",
				BaseURL: fmt.Sprintf("http://model-gateway.%s.svc:8080/v1", tenantNamespace),
			},
			APIKeySecretRefs: []v1.SecretKeyReference{{Name: providerSecret, Key: "apiKey"}},
		},
	})
	apply(&v1.Unit{
		Namespace: tenantNamespace, Name: "engineering",
		Spec: v1.UnitSpec{
			CompanyRef:  v1.CompanyReference{Name: companyName},
			DisplayName: "Engineering",
			Type:        v1.UnitTypeDepartment,
			Purpose:     "Build what Acme sells.",
		},
	})
	apply(leadSeat("Lead engineering."))
	apply(seat("dev", "Developer", "Write the code."))
}

func leadSeat(goal string) *v1.Seat { return seat("lead", "Engineering Lead", goal) }

func seat(name, displayName, goal string) *v1.Seat {
	return &v1.Seat{
		Namespace: tenantNamespace, Name: name,
		Spec: v1.SeatSpec{
			CompanyRef:  v1.CompanyReference{Name: companyName},
			DisplayName: displayName,
			UnitRef:     v1.LocalObjectReference{Name: "engineering"},
			Goal:        goal,
			LLM:         v1.SeatLLM{Default: []v1.LLMProviderReference{{Name: "gateway"}}},
		},
	}
}

// apply server-side applies a typed object under the suite's field manager.
// The object is encoded with encoding/json, which honours the API types'
// omitempty and omitzero, so the request holds exactly the fields set here:
// the fields this manager owns.
func apply(obj client.Object) {
	GinkgoHelper()
	gvk, err := apiutil.GVKForObject(obj, k8sClient.Scheme())
	Expect(err).NotTo(HaveOccurred())
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	data, err := json.Marshal(obj)
	Expect(err).NotTo(HaveOccurred())
	u := &unstructured.Unstructured{}
	Expect(u.UnmarshalJSON(data)).To(Succeed())
	Expect(k8sClient.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
		client.FieldOwner(fieldManager), client.ForceOwnership)).To(Succeed(), "applying %s %s", gvk.Kind, obj.GetName())
}

func getCompany() *v1.Company {
	GinkgoHelper()
	company := &v1.Company{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: companyName}, company)).To(Succeed())
	return company
}

// waitForCompanyReady waits until every condition of the Company is True for
// its current generation, and returns it.
func waitForCompanyReady() *v1.Company {
	GinkgoHelper()
	company := &v1.Company{}
	key := types.NamespacedName{Namespace: tenantNamespace, Name: companyName}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, key, company)).To(Succeed())
		for _, t := range []string{
			v1.ConditionAccepted, v1.ConditionRuntimeReady, v1.ConditionExposed,
			v1.ConditionSecretsSynced, v1.ConditionConfigApplied, v1.ConditionReady,
		} {
			c := meta.FindStatusCondition(company.Status.Conditions, t)
			g.Expect(c).NotTo(BeNil(), "the Company reports no %s", t)
			g.Expect(c.Status).To(Equal(metav1.ConditionTrue), "%s=%s %s: %s", t, c.Status, c.Reason, c.Message)
			g.Expect(c.ObservedGeneration).To(Equal(company.Generation), "%s is for an older generation", t)
		}
	}).WithTimeout(engineReady).Should(Succeed())
	return company
}

// waitForCompanyGone waits for the operator's finalizer to stop the engine
// and let the Company go: the data pod drains first.
func waitForCompanyGone() {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: companyName}, &v1.Company{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the Company is still there")
	}).WithTimeout(engineReady).Should(Succeed())
}

func conditionStatus(conditions []metav1.Condition, t string) metav1.ConditionStatus {
	if c := meta.FindStatusCondition(conditions, t); c != nil {
		return c.Status
	}
	return ""
}

func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getObject reads a named object in the tenant's namespace into obj.
func getObject[T client.Object](obj T, name string) T {
	GinkgoHelper()
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: name}, obj)).To(Succeed())
	return obj
}

// gone reports whether a named object in the tenant's namespace no longer
// exists. An object waiting on a finalizer still exists.
func gone(obj client.Object, name string) bool {
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: name}, obj)
	Expect(client.IgnoreNotFound(err)).To(Succeed())
	return apierrors.IsNotFound(err)
}

func secretEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
		Name: secret, Key: key,
	}}}
}

// ctx is the context of every API call. Each wait is bounded by its
// Eventually instead, so that a slow call fails the step it belongs to.
var ctx = context.Background()

// crewletKinds are the namespaced crewlet.ai kinds, for kubectl.
const crewletKinds = "companies.crewlet.ai,units.crewlet.ai,seats.crewlet.ai,llmproviders.crewlet.ai," +
	"mcpservers.crewlet.ai,integrations.crewlet.ai"

// dumpTenant prints what a failed spec needs to be understood: the tenant's
// objects and their status, the data pod's description and the engine's and
// the operator's logs.
func dumpTenant() {
	commands := [][]string{
		// The kinds by name, not by their category: kubectl may still hold
		// a discovery cache from before the CRDs were installed.
		{"get", crewletKinds + ",statefulsets,pods,services,pvc,secrets,configmaps,networkpolicies", "-o", "wide"},
		{"get", crewletKinds, "-o", "yaml"},
		{"events"},
		{"describe", "pod", dataPod},
		{"logs", dataPod, "--tail=200"},
	}
	for _, args := range commands {
		dump(append(args, "-n", tenantNamespace)...)
	}
	dump("logs", "deploy/crewlet-operator-controller-manager", "-n", namespace, "--tail=300")
}

// dump prints what a kubectl command prints, or why it failed.
func dump(args ...string) {
	out, err := utils.Run(exec.Command("kubectl", args...))
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "kubectl %s: %v\n", strings.Join(args, " "), err)
		return
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "kubectl %s:\n%s\n", strings.Join(args, " "), out)
}
