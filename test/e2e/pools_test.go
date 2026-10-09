//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
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

// A company that runs its seats in pools: a Shared pool of stateless seat
// pods beside the data pod, and a PerSeat pool that gives one seat a pod of
// its own. What the engine does with them is read from its own GET /fleet,
// from inside the cluster, with the token the operator publishes.
const (
	poolsNamespace         = "e2e-pools"
	poolsConsumerNamespace = "e2e-pools-consumer"
	poolsClassName         = "e2e-pools"
	poolsCompany           = "pools"
	sharedPool             = "general"
	perSeatPool            = "solo"

	// fleetReader is the consumer's pod the specs read the fleet through.
	fleetReader = "fleet-reader"

	// placed bounds how long the engine may take to place its seats once
	// their pods run: a new node claims nothing before a data node reports
	// its copy current, then a few seats every five seconds; a seat whose
	// holder went away without releasing it waits out its 45-second lease.
	placed = 3 * time.Minute
)

// scopedName is the name of an object of one of the company's pools or seats:
// the company, the kind, the pool or handle, and eight hexadecimal digits of
// a SHA-256 of the four joined by '/' (no salt for a StatefulSet).
func scopedName(kind, value string) string {
	sum := sha256.Sum256([]byte(poolsCompany + "/" + kind + "/" + value + "/"))
	return poolsCompany + "-" + kind + "-" + value + "-" + hex.EncodeToString(sum[:4])
}

// The company's objects, by the names the docs give.
var (
	poolsDataPod      = poolsCompany + "-data-0"
	sharedStatefulSet = scopedName("seats", sharedPool)
	soloStatefulSet   = scopedName("seat", "ed")
	soloPod           = soloStatefulSet + "-0"
	poolsAccessSecret = poolsCompany + "-api-access"

	// The seats and where the Company places them: ana on the data pods,
	// three in the Shared pool, ed in the PerSeat pool.
	dataSeats   = []string{"ana"}
	sharedSeats = []string{"bo", "cy", "di"}
	soloSeats   = []string{"ed"}
)

// fleetAnswer is the part of the engine's GET /fleet the specs read.
type fleetAnswer struct {
	Nodes []struct {
		ID     string            `json:"id"`
		Roles  []string          `json:"roles"`
		Broker string            `json:"broker"`
		Labels map[string]string `json:"labels"`
	} `json:"nodes"`
	Seats []struct {
		Handle string `json:"handle"`
		Node   string `json:"node"`
		Epoch  int64  `json:"epoch"`
	} `json:"seats"`
}

// holder is the node that holds a seat's lease and the lease's epoch, or ""
// when no node does.
func (f fleetAnswer) holder(handle string) (string, int64) {
	for _, s := range f.Seats {
		if s.Handle == handle {
			return s.Node, s.Epoch
		}
	}
	return "", 0
}

var _ = Describe("Seat pools", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	BeforeAll(func() {
		By("creating the tenant's namespace, enforcing the restricted Pod Security Standard")
		apply(restrictedNamespace(poolsNamespace))

		By("creating an EngineClass that allows seat pools, PerSeat ones included")
		small := &corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		}}
		apply(&v1.EngineClass{
			Name: poolsClassName,
			Spec: v1.EngineClassSpec{
				Engine: v1.EngineSettings{Image: crewletImage},
				Data: v1.DataPodSettings{
					Resources: small,
					Storage:   v1.DataStorage{Size: new(resource.MustParse("7Gi"))},
				},
				SeatPools: v1.SeatPoolSettings{
					Policy:       v1.SeatPoolsAllowed,
					AllowPerSeat: new(true),
					MaxPods:      5,
					// A pod with no turn to finish drains in a second; the
					// specs delete pods and scale pools.
					Defaults: v1.SeatPoolDefaults{Resources: small, TerminationGracePeriodSeconds: 30},
				},
				Exposure: v1.ExposureSettings{Webhooks: v1.WebhookExposure{Mode: v1.WebhookExposureNone}},
				NetworkPolicy: v1.NetworkPolicySettings{
					Mode: v1.NetworkPolicyIsolated,
					APIConsumers: []v1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{corev1.LabelMetadataName: poolsConsumerNamespace},
					}}},
				},
			},
		})

		By("writing the company: two Shared pods, and seats on the data pod, in the Shared pool and in a PerSeat pool")
		applyPoolsCompany(2)
	})

	AfterAll(func() {
		By("deleting the tenant's objects while the operator runs to finalize them")
		_ = k8sClient.Delete(ctx, &v1.Company{Namespace: poolsNamespace, Name: poolsCompany})
		for _, ns := range []string{poolsNamespace, poolsConsumerNamespace} {
			_ = k8sClient.Delete(ctx, &corev1.Namespace{Name: ns})
		}
		Eventually(func(g Gomega) {
			for _, ns := range []string{poolsNamespace, poolsConsumerNamespace} {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ns}, &corev1.Namespace{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "namespace %s is still there", ns)
			}
		}).WithTimeout(engineReady).Should(Succeed())
		_ = k8sClient.Delete(ctx, &v1.EngineClass{Name: poolsClassName})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpPools()
		}
	})

	It("runs the data pod and every seat pod, and the engine places each seat in its own pool", func() {
		By("waiting for the Company to be Ready with every pool serving")
		Eventually(func(g Gomega) {
			company := &v1.Company{}
			key := types.NamespacedName{Namespace: poolsNamespace, Name: poolsCompany}
			g.Expect(k8sClient.Get(ctx, key, company)).To(Succeed())
			for _, t := range []string{v1.ConditionReady, v1.ConditionSeatsPlaced} {
				c := meta.FindStatusCondition(company.Status.Conditions, t)
				g.Expect(c).NotTo(BeNil(), "the Company reports no %s", t)
				g.Expect(c.Status).To(Equal(metav1.ConditionTrue), "%s=%s %s: %s", t, c.Status, c.Reason, c.Message)
			}
			g.Expect(poolStatus(company, sharedPool)).To(Equal([3]int32{2, 2, int32(len(sharedSeats))}))
			g.Expect(poolStatus(company, perSeatPool)).To(Equal([3]int32{1, 1, int32(len(soloSeats))}))
		}).WithTimeout(engineReady).Should(Succeed())

		By("finding the data pod Ready, and each seat pod running and available past its minReadySeconds")
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: poolsNamespace, Name: poolsDataPod}, pod)).To(Succeed())
		Expect(podReady(pod)).To(BeTrue(), "the data pod is not Ready")
		for name, replicas := range map[string]int32{sharedStatefulSet: 2, soloStatefulSet: 1} {
			Eventually(func(g Gomega) {
				sts := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: poolsNamespace, Name: name}, sts)).To(Succeed())
				g.Expect(sts.Status.AvailableReplicas).To(Equal(replicas), "StatefulSet %s", name)
			}).Should(Succeed())
		}

		By("reading the fleet from inside the cluster with the published token")
		startFleetReader()
		Eventually(func(g Gomega) {
			fleet := readFleet(g)
			for _, n := range fleet.Nodes {
				switch {
				case n.ID == poolsDataPod:
					g.Expect(n.Broker).To(Equal("member"), "the data pod's broker")
				case strings.HasPrefix(n.ID, poolsCompany+"-seat"):
					g.Expect(n.Broker).To(Equal("leaf"), "seat pod %s's broker", n.ID)
					g.Expect(n.Roles).To(Equal([]string{"seats"}), "seat pod %s's roles", n.ID)
				}
			}
			for _, handle := range dataSeats {
				node, _ := fleet.holder(handle)
				g.Expect(node).To(Equal(poolsDataPod), "seat %s", handle)
			}
			for _, handle := range sharedSeats {
				node, _ := fleet.holder(handle)
				g.Expect(node).To(HavePrefix(sharedStatefulSet+"-"), "seat %s", handle)
			}
			node, _ := fleet.holder("ed")
			g.Expect(node).To(Equal(soloPod), "seat ed is not held by its own pod")
		}).WithTimeout(placed).Should(Succeed())

		By("reporting on each Seat the node the engine says holds it")
		expectSeatsFollowTheFleet()
	})

	// The data pod's leaf listener takes any connection, with no
	// authentication: <company>-broker is what keeps it the company's.
	It("admits the company's own seat pods to the data pod's leaf listener", func() {
		Expect(leafProbe(poolsNamespace, "leaf-own", seatLabels(poolsCompany), leafAddress)).To(Equal(corev1.PodSucceeded),
			"a pod labelled as the company's seat pod did not join the leaf listener: the probe proves nothing")
	})

	// NetworkPolicy at work, which needs a CNI that enforces it (see the
	// company specs' "refuses a pod of a namespace the class does not
	// admit").
	It("does not admit another pod to the leaf listener, under Isolated or None, nor a connection to a seat pod", func() {
		refused := func() {
			GinkgoHelper()
			for name, probe := range map[string]struct {
				namespace string
				labels    map[string]string
			}{
				"leaf-other-namespace": {poolsConsumerNamespace, seatLabels(poolsCompany)},
				"leaf-unlabelled":      {poolsNamespace, nil},
				"leaf-another-company": {poolsNamespace, seatLabels("another")},
			} {
				Expect(leafProbe(probe.namespace, name, probe.labels, leafAddress)).To(Equal(corev1.PodFailed),
					"pod %s/%s joined the company's leaf listener", probe.namespace, name)
			}
		}
		By("refusing every other pod under Isolated")
		refused()

		By("refusing a connection to a seat pod, from the company's namespace and from another")
		// A seat pod binds no port, so a refused connection to one says
		// nothing. A pod labelled as the company's seat pod that does
		// listen can be refused by the seats policy alone; the same
		// listener without the labels, reached, is the control.
		plain := listenerPod("seat-listener-plain", nil)
		labelled := listenerPod("seat-listener", seatLabels(poolsCompany))
		Expect(leafProbe(poolsNamespace, "seat-listener-control", nil, plain)).To(Equal(corev1.PodSucceeded),
			"the listener without seat labels was not reached: the probe proves nothing")
		for name, namespace := range map[string]string{
			"seat-listener-same-namespace":  poolsNamespace,
			"seat-listener-other-namespace": poolsConsumerNamespace,
		} {
			Expect(leafProbe(namespace, name, nil, labelled)).To(Equal(corev1.PodFailed),
				"pod %s/%s reached a pod labelled as the company's seat pod", namespace, name)
		}

		By("refusing every other pod under None, which keeps the broker's policy")
		setPoolsNetworkPolicy(v1.NetworkPolicyNone)
		DeferCleanup(setPoolsNetworkPolicy, v1.NetworkPolicyIsolated)
		refused()
		Expect(leafProbe(poolsNamespace, "leaf-own-none", seatLabels(poolsCompany), leafAddress)).
			To(Equal(corev1.PodSucceeded), "the company's own seat pod was refused under None")
	})

	It("rebalances the Shared pool's seats onto a pod it gains", func() {
		By("scaling the Shared pool to three pods")
		applyPoolsCompany(3)

		By("finding each of the three pods holding one of its three seats")
		Eventually(func(g Gomega) {
			fleet := readFleet(g)
			held := map[string]int{}
			for _, handle := range sharedSeats {
				node, _ := fleet.holder(handle)
				g.Expect(node).To(HavePrefix(sharedStatefulSet+"-"), "seat %s", handle)
				held[node]++
			}
			g.Expect(held).To(Equal(map[string]int{
				sharedStatefulSet + "-0": 1, sharedStatefulSet + "-1": 1, sharedStatefulSet + "-2": 1,
			}))
		}).WithTimeout(placed).Should(Succeed())
		expectSeatsFollowTheFleet()
	})

	It("returns a PerSeat seat to its own pod when the pod is replaced", func() {
		var before int64
		Eventually(func(g Gomega) {
			var node string
			node, before = readFleet(g).holder("ed")
			g.Expect(node).To(Equal(soloPod))
		}).Should(Succeed())

		By("deleting the seat's pod")
		_, err := utils.Run(exec.Command("kubectl", "delete", "pod", soloPod, "-n", poolsNamespace, "--wait=false"))
		Expect(err).NotTo(HaveOccurred())

		By("finding the seat taken again by the pod that replaced it, under a new lease")
		Eventually(func(g Gomega) {
			node, epoch := readFleet(g).holder("ed")
			g.Expect(node).To(Equal(soloPod))
			g.Expect(epoch).To(BeNumerically(">", before), "the seat's lease has not changed hands")
		}).WithTimeout(placed).Should(Succeed())
		expectSeatsFollowTheFleet()
	})
})

// applyPoolsCompany writes the company of the Seat pools specs, its Shared
// pool at shared pods.
func applyPoolsCompany(shared int32) {
	GinkgoHelper()
	apply(&v1.Company{
		Namespace: poolsNamespace, Name: poolsCompany,
		Spec: v1.CompanySpec{
			EngineClassName: poolsClassName,
			CompanyName:     "Pools",
			Mission:         "Prove seat pools end to end.",
			DeletionPolicy:  v1.DeletionPolicyDelete,
			Topology: v1.CompanyTopology{SeatPools: []v1.SeatPool{
				{Name: sharedPool, Mode: v1.SeatPoolShared, Replicas: new(shared)},
				{Name: perSeatPool, Mode: v1.SeatPoolPerSeat, Replicas: new(int32(1))},
			}},
		},
	})
	applyProvider(poolsNamespace, poolsCompany)
	placements := map[string][]string{v1.DataPool: dataSeats, sharedPool: sharedSeats, perSeatPool: soloSeats}
	for pool, handles := range placements {
		for _, handle := range handles {
			apply(&v1.Seat{
				Namespace: poolsNamespace, Name: handle,
				Spec: v1.SeatSpec{
					CompanyRef: v1.CompanyReference{Name: poolsCompany},
					Goal:       "Answer what " + handle + " is asked.",
					LLM:        v1.SeatLLM{Default: []v1.LLMProviderReference{{Name: providerName}}},
					Placement:  v1.SeatPlacement{Pool: pool},
				},
			})
		}
	}
}

// poolStatus is a pool's replicas, ready replicas and seats in the Company's
// status.topology.seatPools.
func poolStatus(company *v1.Company, name string) [3]int32 {
	for _, p := range company.Status.Topology.SeatPools {
		if p.Name == name {
			return [3]int32{value(p.Replicas), value(p.ReadyReplicas), value(p.Seats)}
		}
	}
	return [3]int32{-1, -1, -1}
}

func value(p *int32) int32 {
	if p == nil {
		return -1
	}
	return *p
}

// startFleetReader creates the consumer namespace the class admits, with a
// copy of the Company's API access Secret, and a pod there that the specs
// run curl in.
func startFleetReader() {
	GinkgoHelper()
	access := &corev1.Secret{}
	key := types.NamespacedName{Namespace: poolsNamespace, Name: poolsAccessSecret}
	Expect(k8sClient.Get(ctx, key, access)).To(Succeed())
	apply(restrictedNamespace(poolsConsumerNamespace))
	apply(&corev1.Secret{Namespace: poolsConsumerNamespace, Name: poolsAccessSecret, Data: access.Data})
	apply(curlPodWith(poolsConsumerNamespace, fleetReader, poolsAccessSecret, "sleep 3600"))
	Eventually(func(g Gomega) {
		pod := &corev1.Pod{}
		key := types.NamespacedName{Namespace: poolsConsumerNamespace, Name: fleetReader}
		g.Expect(k8sClient.Get(ctx, key, pod)).To(Succeed())
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	}).Should(Succeed())
}

// readFleet reads the engine's GET /fleet through the fleet reader, as the
// published token.
func readFleet(g Gomega) fleetAnswer {
	out, err := utils.Run(exec.Command("kubectl", "exec", fleetReader, "-n", poolsConsumerNamespace, "--",
		"/bin/sh", "-c", `curl -fsS --max-time 10 -H "Authorization: Bearer ${TOKEN}" "${URL}/fleet"`))
	g.Expect(err).NotTo(HaveOccurred(), "reading /fleet")
	var fleet fleetAnswer
	g.Expect(json.Unmarshal([]byte(out), &fleet)).To(Succeed(), "/fleet answered %s", out)
	return fleet
}

// expectSeatsFollowTheFleet waits until every agent Seat's status names the
// node the engine's fleet says holds it, with Ready=True.
func expectSeatsFollowTheFleet() {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		fleet := readFleet(g)
		for _, handle := range append(append(append([]string{}, dataSeats...), sharedSeats...), soloSeats...) {
			seat := &v1.Seat{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: poolsNamespace, Name: handle}, seat)).To(Succeed())
			node, _ := fleet.holder(handle)
			g.Expect(node).NotTo(BeEmpty(), "no node holds seat %s", handle)
			g.Expect(seat.Status.Placement.Node).To(Equal(node), "Seat %s", handle)
			ready := meta.FindStatusCondition(seat.Status.Conditions, v1.ConditionReady)
			g.Expect(ready).NotTo(BeNil(), "Seat %s has no Ready", handle)
			g.Expect(ready.Reason).To(Equal(v1.ReasonLeaseHeld), "Seat %s: %s", handle, ready.Message)
		}
	}).WithTimeout(placed).Should(Succeed())
}

// dumpPools prints what a failed spec of the pools needs to be understood.
func dumpPools() {
	dumpNamespace(poolsNamespace, poolsDataPod, sharedStatefulSet+"-0", soloPod)
}

// leafAddress is the data pod's leaf listener, as a seat pod dials it.
var leafAddress = fmt.Sprintf("%s.%s-data.%s.svc:%d", poolsDataPod, poolsCompany, poolsNamespace, 7422)

// seatLabels are the labels the operator gives company's seat pods, which
// <company>-broker admits by.
func seatLabels(company string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "crewlet", "app.kubernetes.io/instance": company,
		"app.kubernetes.io/component": "seats", v1.CompanyLabel: company,
	}
}

// leafProbe runs a pod named name, with labels, in namespace, that opens a TCP
// connection to target (host:port) and succeeds only if a NATS server
// answers on it with its INFO line, as the leaf listener does at once; it
// returns how the pod ended. A refused or dropped connection fails it.
func leafProbe(namespace, name string, labels map[string]string, target string) corev1.PodPhase {
	GinkgoHelper()
	_ = k8sClient.Delete(ctx, &corev1.Pod{Namespace: namespace, Name: name}, client.GracePeriodSeconds(0))
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &corev1.Pod{}))
	}).Should(BeTrue())
	// curl's telnet:// opens a bare TCP connection and prints what the
	// server sends; the sleep keeps its input open while the INFO line
	// arrives.
	script := `out=$( (sleep 3) | curl -sS --max-time 8 "telnet://` + target + `" 2>&1 ); echo "$out"; ` +
		`case "$out" in *INFO*) exit 0;; *) exit 1;; esac`
	pod := curlPodWith(namespace, name, poolsAccessSecret, script)
	pod.Labels = labels
	pod.Spec.Containers[0].Env = nil
	apply(pod)
	return curlPhase(namespace, name)
}

// listenerPod runs a pod named name, with labels, in the pools namespace that
// answers every TCP connection on port 7422 with an INFO line, as the leaf
// listener does, and returns its address (host:port) once it runs. The pod
// is deleted when the spec ends.
func listenerPod(name string, labels map[string]string) string {
	GinkgoHelper()
	pod := curlPodWith(poolsNamespace, name, poolsAccessSecret, "")
	pod.Labels = labels
	pod.Spec.Containers[0].Env = nil
	pod.Spec.Containers[0].Command = []string{"nc", "-lk", "-p", "7422", "-e", "echo", "INFO"}
	apply(pod)
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod, client.GracePeriodSeconds(0)))).To(Succeed())
	})
	var address string
	Eventually(func(g Gomega) {
		running := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), running)).To(Succeed())
		g.Expect(running.Status.Phase).To(Equal(corev1.PodRunning))
		g.Expect(running.Status.PodIP).NotTo(BeEmpty())
		address = fmt.Sprintf("%s:7422", running.Status.PodIP)
	}).Should(Succeed())
	return address
}

// setPoolsNetworkPolicy sets the pools class's networkPolicy.mode and waits
// for the operator to have acted on it: <company>-engine exists under
// Isolated alone.
func setPoolsNetworkPolicy(mode v1.NetworkPolicyMode) {
	GinkgoHelper()
	class := &v1.EngineClass{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: poolsClassName}, class)).To(Succeed())
	class.Spec.NetworkPolicy.Mode = mode
	Expect(k8sClient.Update(ctx, class)).To(Succeed())
	Eventually(func(g Gomega) {
		key := types.NamespacedName{Namespace: poolsNamespace, Name: poolsCompany + "-engine"}
		err := k8sClient.Get(ctx, key, &networkingv1.NetworkPolicy{})
		if mode == v1.NetworkPolicyIsolated {
			g.Expect(err).NotTo(HaveOccurred())
			return
		}
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the engine's NetworkPolicy is still there under %s", mode)
	}).Should(Succeed())
}
