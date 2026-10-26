//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
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
	outsiderNamespace = "e2e-outsider"
	className         = "e2e"
	companyName       = "acme"
	providerSecret    = "model-key"

	// engineeringUnit is the company's one Unit, which its agent seats sit
	// in and its founder manages.
	engineeringUnit = "engineering"

	// founderSeat is the company's Human seat, which the suite puts the
	// company's first person on, and founderEmail that person's address.
	founderSeat  = "founder"
	founderEmail = "founder@acme.example"

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
	// publicURL is the in-cluster address of the engine's public listener,
	// the port the webhooks' route reaches.
	publicURL = fmt.Sprintf("http://%s.%s.svc:8081", engineService, tenantNamespace)
)

var _ = Describe("Company", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	BeforeAll(func() {
		By("creating the tenant's namespace, enforcing the restricted Pod Security Standard")
		apply(restrictedNamespace(tenantNamespace))

		By("creating an EngineClass that runs the engine image the suite loaded")
		apply(&v1.EngineClass{
			Name: className,
			Spec: v1.EngineClassSpec{
				// Never: the node runs the image the suite loaded, or fails
				// to start it, and never pulls whatever main is by then.
				Engine: v1.EngineSettings{Image: crewletImage, ImagePullPolicy: corev1.PullNever},
				Data: v1.DataPodSettings{
					Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					}},
					// The smallest volume the API admits. kind's
					// provisioner does not enforce it; the engine's
					// event stream is sized from it all the same.
					Storage: v1.DataStorage{Size: new(resource.MustParse("14Gi"))},
				},
				Exposure: v1.ExposureSettings{Webhooks: v1.WebhookExposure{Mode: v1.WebhookExposureNone}},
				NetworkPolicy: v1.NetworkPolicySettings{
					Mode: v1.NetworkPolicyIsolated,
					APIConsumers: []v1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{corev1.LabelMetadataName: consumerNamespace},
					}}},
				},
			},
		})
	})

	AfterAll(func() {
		By("deleting the tenant's objects while the operator runs to finalize them")
		_ = k8sClient.Delete(ctx, &v1.Company{Namespace: tenantNamespace, Name: companyName})
		for _, ns := range []string{tenantNamespace, consumerNamespace, outsiderNamespace} {
			_ = k8sClient.Delete(ctx, &corev1.Namespace{Name: ns})
		}
		Eventually(func(g Gomega) {
			for _, ns := range []string{tenantNamespace, consumerNamespace, outsiderNamespace} {
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
			Units: new(int32(1)), Seats: new(int32(3)), LLMProviders: new(int32(1)),
			MCPServers: new(int32(0)), Integrations: new(int32(0)), Schedules: new(int32(0)),
		}))
		Expect(company.Status.Endpoints.API).To(Equal(engineURL))
		Expect(company.Status.APIAccessSecretRef.Name).To(Equal(accessSecret))
		// What the getting-started guide shows a reader at this step.
		for _, c := range company.Status.Conditions {
			GinkgoWriter.Printf("condition %s=%s %s\n", c.Type, c.Status, c.Reason)
		}

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

	It("lets a consumer the class admits read the engine's API with the published token", func() {
		By("copying the API access Secret into the namespace the class admits as an API consumer")
		access := copyAccessSecret(consumerNamespace)
		Expect(access.Data).To(HaveKey(v1.APIAccessTokenKey))
		Expect(string(access.Data[v1.APIAccessURLKey])).To(Equal(engineURL))

		By("reading the active company document with the token")
		apply(curlPod(consumerNamespace, curlPodName, readConfig))
		Expect(curlPhase(consumerNamespace, curlPodName)).To(Equal(corev1.PodSucceeded),
			"the consumer's request did not succeed")
		out, err := utils.Run(exec.Command("kubectl", "logs", curlPodName, "-n", consumerNamespace))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("Acme"), "the engine's active document is not the company's")

		By("being sent to the public listener for a webhook, which the API port does not serve")
		const webhookPod = "webhook-on-api"
		apply(curlPod(consumerNamespace, webhookPod,
			`curl -sS --max-time 20 -X POST -w "\nstatus=%{http_code}\n" "${URL}/webhooks/gitlab"`))
		Expect(curlPhase(consumerNamespace, webhookPod)).To(Equal(corev1.PodSucceeded),
			"the consumer's request did not complete")
		out, err = utils.Run(exec.Command("kubectl", "logs", webhookPod, "-n", consumerNamespace))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("status=404"), "the API port served a webhook")
		Expect(out).To(ContainSubstring("api.public"), "the engine runs no public listener to send the vendor to")
	})

	It("serves the public listener the routes outside parties call, and nothing else", func() {
		// Through the kubelet into the pod's own network namespace, which
		// no NetworkPolicy polices: this is the engine's split, not the
		// policy's.
		local := freePort()
		forward := exec.CommandContext(ctx, "kubectl", "port-forward", "-n", tenantNamespace,
			"pod/"+dataPod, fmt.Sprintf("%d:8081", local))
		Expect(forward.Start()).To(Succeed())
		DeferCleanup(func() {
			_ = forward.Process.Kill()
			_ = forward.Wait()
		})
		ask := func(g Gomega, method, path string) (int, string) {
			req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", local, path), nil)
			g.Expect(err).NotTo(HaveOccurred())
			resp, err := http.DefaultClient.Do(req)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			g.Expect(err).NotTo(HaveOccurred())
			return resp.StatusCode, string(body)
		}
		Eventually(func(g Gomega) {
			// The admin API is a path nothing serves, and nothing says a
			// port is behind it.
			status, body := ask(g, http.MethodGet, "/config")
			g.Expect(status).To(Equal(http.StatusNotFound))
			g.Expect(body).To(ContainSubstring("no_route"))
			g.Expect(body).NotTo(ContainSubstring("api.public"))
			// No probe is public.
			status, _ = ask(g, http.MethodGet, "/health")
			g.Expect(status).To(Equal(http.StatusNotFound))
			// A webhook route is served: it refuses a delivery for a
			// vendor the company has not connected, rather than not being
			// there.
			status, body = ask(g, http.MethodPost, "/webhooks/gitlab")
			g.Expect(status).NotTo(Equal(http.StatusNotFound), "the public listener does not serve the webhooks: %s", body)
		}).WithTimeout(time.Minute).Should(Succeed())
	})

	// The engine's NetworkPolicy at work, as below: a consumer of the API is
	// admitted to the API port alone, and the public port admits only where
	// the class's webhooks arrive from, which under webhooks None is nowhere.
	It("does not admit an API consumer to the public listener", func() {
		const publicPod = "public-from-consumer"
		apply(curlPod(consumerNamespace, publicPod, `curl -sS --max-time 10 -o /dev/null "`+publicURL+`/webhooks/gitlab"`))
		Expect(curlPhase(consumerNamespace, publicPod)).To(Equal(corev1.PodFailed),
			"an API consumer reached the engine's public listener")
	})

	It("refuses the published token a change to the document the operator enforces, and keeps the document", func() {
		before := getCompany().Status.Config.RevisionID

		By("replacing the company document with the token, as a person in the engine's dashboard would")
		const editPod = "edit-config"
		apply(curlPod(consumerNamespace, editPod, editConfig))
		Expect(curlPhase(consumerNamespace, editPod)).To(Equal(corev1.PodSucceeded),
			"the consumer's requests did not complete")
		out, err := utils.Run(exec.Command("kubectl", "logs", editPod, "-n", consumerNamespace))
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("status=403"), "the engine did not refuse the edit")
		Expect(out).To(ContainSubstring(`"config_managed"`))
		Expect(out).To(ContainSubstring("Acme"), "the document read after the refusal is not the company's")
		Expect(out).NotTo(ContainSubstring("Edited by hand"))

		By("finding the operator's revision still the one the engine runs")
		Consistently(func(g Gomega) {
			company := getCompany()
			g.Expect(company.Status.Config.RevisionID).To(Equal(before))
			g.Expect(conditionStatus(company.Status.Conditions, v1.ConditionConfigApplied)).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})

	It("reports nobody in the company until its first person joins, and refuses to take that person's seat", func() {
		identity := func(g Gomega) *metav1.Condition {
			c := meta.FindStatusCondition(getCompany().Status.Conditions, v1.ConditionIdentityClaimed)
			g.Expect(c).NotTo(BeNil(), "the Company reports no IdentityClaimed")
			return c
		}

		By("finding the company unclaimed, naming its Human seat")
		Eventually(func(g Gomega) {
			c := identity(g)
			g.Expect(c.Reason).To(Equal(v1.ReasonUnclaimed), c.Message)
			g.Expect(c.Message).To(ContainSubstring("Its Human seats are " + founderSeat + "."))
		}).Should(Succeed())

		By("adding the first person onto the Human seat with the published token, as a control plane would")
		key, err := uuid.NewV7()
		Expect(err).NotTo(HaveOccurred())
		const addPod = "add-founder"
		apply(curlPod(consumerNamespace, addPod, addPerson(key.String())))
		phase := curlPhase(consumerNamespace, addPod)
		out, err := utils.Run(exec.Command("kubectl", "logs", addPod, "-n", consumerNamespace))
		Expect(err).NotTo(HaveOccurred())
		Expect(phase).To(Equal(corev1.PodSucceeded), "the engine did not add the person: %s", out)

		By("finding the company's first person enrolled")
		Eventually(func(g Gomega) {
			c := identity(g)
			g.Expect(c.Status).To(Equal(metav1.ConditionTrue), "%s: %s", c.Reason, c.Message)
			g.Expect(c.Reason).To(Equal(v1.ReasonPersonEnrolled))
		}).Should(Succeed())

		By("deleting the person's Human Seat, which the engine refuses while they hold it")
		before := getCompany().Status.Config.RevisionID
		Expect(k8sClient.Delete(ctx, &v1.Seat{Namespace: tenantNamespace, Name: founderSeat})).To(Succeed())
		Eventually(func(g Gomega) {
			c := meta.FindStatusCondition(getCompany().Status.Conditions, v1.ConditionConfigApplied)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Reason).To(Equal(v1.ReasonSeatHeld), c.Message)
			g.Expect(c.Message).To(ContainSubstring(founderSeat + " (held by a person, active)"))
			g.Expect(c.Message).NotTo(ContainSubstring(founderEmail))
		}).Should(Succeed())
		Expect(getCompany().Status.Config.RevisionID).To(Equal(before), "the engine runs another revision")

		By("restoring the Seat, which the engine runs again with nothing to apply")
		apply(founder())
		Eventually(func(g Gomega) {
			company := getCompany()
			g.Expect(conditionStatus(company.Status.Conditions, v1.ConditionConfigApplied)).To(Equal(metav1.ConditionTrue))
			g.Expect(company.Status.Config.RevisionID).To(Equal(before))
		}).Should(Succeed())

		By("recording the data pod's peak memory, as its cgroup counts it")
		peak, err := utils.Run(exec.Command("kubectl", "exec", "-n", tenantNamespace, dataPod, "--",
			"cat", "/sys/fs/cgroup/memory.peak"))
		if err != nil {
			// A measurement, not a guard: a node whose cgroup keeps no peak
			// says so in the report rather than failing the suite.
			peak = "not read: " + err.Error()
		}
		AddReportEntry("data pod peak memory (bytes)", strings.TrimSpace(peak))
	})

	// The engine's NetworkPolicy at work, which needs a CNI that enforces
	// NetworkPolicy: kind's kindnet does, on a kernel with nftables' queue
	// support (CONFIG_NFT_QUEUE).
	It("refuses a pod of a namespace the class does not admit, token or not", func() {
		copyAccessSecret(outsiderNamespace)
		apply(curlPod(outsiderNamespace, curlPodName, readConfig))
		Expect(curlPhase(outsiderNamespace, curlPodName)).To(Equal(corev1.PodFailed),
			"a pod in a namespace the engine's NetworkPolicy does not admit reached its API")
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
	apply(&v1.Company{
		Namespace: tenantNamespace, Name: companyName,
		Spec: v1.CompanySpec{
			EngineClassName: className,
			CompanyName:     "Acme",
			Mission:         "Prove the operator end to end.",
			DeletionPolicy:  deletion,
		},
	})
	applyProvider(tenantNamespace, companyName)
	apply(&v1.Unit{
		Namespace: tenantNamespace, Name: engineeringUnit,
		Spec: v1.UnitSpec{
			CompanyRef:  v1.CompanyReference{Name: companyName},
			DisplayName: "Engineering",
			Type:        v1.UnitTypeDepartment,
			Purpose:     "Build what Acme sells.",
		},
	})
	apply(leadSeat("Lead engineering."))
	apply(seat("dev", "Developer", "Write the code."))
	apply(founder())
}

// founder is the company's Human seat, for its first person. It names no
// chat contact: the person works through the dashboard.
func founder() *v1.Seat {
	return &v1.Seat{
		Namespace: tenantNamespace, Name: founderSeat,
		Spec: v1.SeatSpec{
			CompanyRef:  v1.CompanyReference{Name: companyName},
			Kind:        v1.SeatKindHuman,
			DisplayName: "Founder",
			Manages:     []v1.ManagedReference{{Kind: v1.ManagedKindUnit, Name: engineeringUnit}},
		},
	}
}

func leadSeat(goal string) *v1.Seat { return seat("lead", "Engineering Lead", goal) }

func seat(name, displayName, goal string) *v1.Seat {
	return &v1.Seat{
		Namespace: tenantNamespace, Name: name,
		Spec: v1.SeatSpec{
			CompanyRef:  v1.CompanyReference{Name: companyName},
			DisplayName: displayName,
			UnitRef:     v1.LocalObjectReference{Name: engineeringUnit},
			Goal:        goal,
			LLM:         v1.SeatLLM{Default: []v1.LLMProviderReference{{Name: providerName}}},
		},
	}
}

// providerName is the model provider of the suite's companies.
const providerName = "gateway"

// applyProvider writes a company's model provider, Claude Code on a metered
// key, and the Secret holding the key. The provider is never called: the
// engine validates and applies a document without running its model's CLI,
// which the engine image the suite runs does not carry.
func applyProvider(namespace, company string) {
	GinkgoHelper()
	const keyField = "apiKey"
	// A dummy key: the engine stores it and never sends it, since nothing
	// calls the provider.
	apply(&corev1.Secret{
		Namespace: namespace, Name: providerSecret,
		StringData: map[string]string{keyField: "e2e-not-a-real-key"},
	})
	apply(&v1.LLMProvider{
		Namespace: namespace, Name: providerName,
		Spec: v1.LLMProviderSpec{
			CompanyRef: v1.CompanyReference{Name: company},
			LLMProviderSettings: v1.LLMProviderSettings{
				Agent: v1.CLIAgentClaudeCode,
				Model: "sonnet",
			},
			Auth: v1.CLIAgentAuth{
				Mode:            v1.CLIAgentAuthAPIKey,
				APIKeySecretRef: v1.SecretKeyReference{Name: providerSecret, Key: keyField},
			},
		},
	})
}

// restrictedNamespace is a namespace, as a platform gives each tenant, that
// enforces the restricted Pod Security Standard the engine is built to run
// under.
func restrictedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		Name:   name,
		Labels: map[string]string{"pod-security.kubernetes.io/enforce": "restricted"},
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
	"mcpservers.crewlet.ai,integrations.crewlet.ai,schedules.crewlet.ai"

// dumpTenant prints what a failed spec needs to be understood: the tenant's
// objects and their status, the data pod's description and the engine's and
// the operator's logs.
func dumpTenant() { dumpNamespace(tenantNamespace, dataPod) }

// dumpNamespace prints a namespace's crewlet.ai objects and the objects the
// operator makes, its events, the description and log of each pod named, and
// the operator's log.
func dumpNamespace(ns string, pods ...string) {
	const get, logs = "get", "logs"
	commands := make([][]string, 0, 3+2*len(pods))
	commands = append(commands,
		// The kinds by name, not by their category: kubectl may still hold
		// a discovery cache from before the CRDs were installed.
		[]string{get, crewletKinds + ",statefulsets,pods,services,pvc,secrets,configmaps,networkpolicies," +
			"poddisruptionbudgets", "-o", "wide"},
		[]string{get, crewletKinds, "-o", "yaml"},
		[]string{"events"},
	)
	for _, pod := range pods {
		commands = append(commands, []string{"describe", "pod", pod}, []string{logs, pod, "--tail=200"})
	}
	for _, args := range commands {
		dump(append(args, "-n", ns)...)
	}
	dump(logs, "deploy/crewlet-operator-controller-manager", "-n", namespace, "--tail=300")
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

// readConfig reads the engine's active company document with the token and
// the URL of the API access Secret. The token reaches curl from the Secret,
// through the environment: never as a literal in the pod.
const readConfig = `curl -fsS --max-time 20 -H "Authorization: Bearer ${TOKEN}" "${URL}/config"`

// copyAccessSecret creates namespace, enforcing the restricted Pod Security
// Standard, with a copy of the Company's API access Secret, as a consumer's
// platform would provide it, and returns the Secret.
func copyAccessSecret(namespace string) *corev1.Secret {
	GinkgoHelper()
	access := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: accessSecret}, access)).To(Succeed())
	apply(restrictedNamespace(namespace))
	apply(&corev1.Secret{Namespace: namespace, Name: accessSecret, Data: access.Data})
	return access
}

// freePort is a TCP port of this host nothing listens on, for a
// port-forward.
func freePort() int {
	GinkgoHelper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	Expect(ok).To(BeTrue())
	return addr.Port
}

// curlPodName is the name of the pod that reads the company document.
const curlPodName = "read-config"

// editConfig, run as the consumer, tries to replace the company document
// with the published token and then reads it back. The engine refuses the
// change before it reads the body, so the body is one line; curl prints the
// answer and its status, and the document read afterwards.
const editConfig = `curl -sS --max-time 20 -X PUT -H "Authorization: Bearer ${TOKEN}" ` +
	`-H "X-Summary: an edit the operator would overwrite" -H "Content-Type: application/yaml" ` +
	`--data-binary "name: Edited by hand" -w "\nstatus=%{http_code}\n" "${URL}/config" && ` + readConfig

// addPerson, run as the consumer, adds the company's first person onto the
// founder seat with the published token, under the Idempotency-Key key, so
// that a retry of the same request would be the same operation. The engine's
// answer carries the person's first password link, a credential, so only its
// status is printed; the whole answer is printed only when it is a refusal,
// which carries none.
func addPerson(key string) string {
	body := `{"kind":"person","email":"` + founderEmail + `","seat":"` + founderSeat + `","grants":["admin"]}`
	return `out=$(curl -sS --max-time 20 -X POST -H "Authorization: Bearer ${TOKEN}" ` +
		`-H "Content-Type: application/json" -H "Idempotency-Key: ` + key + `" ` +
		`--data '` + body + `' -w '\nstatus=%{http_code}' "${URL}/iam/people") || exit 1; ` +
		`status=$(printf '%s\n' "$out" | tail -n 1); ` +
		`case "$status" in status=201|status=202) echo "$status" ;; *) printf '%s\n' "$out"; exit 1 ;; esac`
}

// curlPod is a pod named name in namespace that runs script with curl, with
// the API access Secret's token and URL in its environment as TOKEN and URL.
func curlPod(namespace, name, script string) *corev1.Pod {
	return curlPodWith(namespace, name, accessSecret, script)
}

// curlPodWith is a curlPod that reads the token and URL from the API access
// Secret named secret.
func curlPodWith(namespace, name, secret, script string) *corev1.Pod {
	return &corev1.Pod{
		Namespace: namespace, Name: name,
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
					secretEnv("TOKEN", secret, v1.APIAccessTokenKey),
					secretEnv("URL", secret, v1.APIAccessURLKey),
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					ReadOnlyRootFilesystem:   new(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
}

// curlPhase waits for the curlPod named name in namespace to finish and
// returns how.
func curlPhase(namespace, name string) corev1.PodPhase {
	GinkgoHelper()
	pod := &corev1.Pod{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod)).To(Succeed())
		g.Expect(pod.Status.Phase).To(BeElementOf(corev1.PodSucceeded, corev1.PodFailed), "the pod has not finished")
	}).Should(Succeed())
	return pod.Status.Phase
}
