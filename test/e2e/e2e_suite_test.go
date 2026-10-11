//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/utils"
)

// managerImage is the image the suite builds and loads into kind. It is
// never pushed, so its registry only has to be one nothing pulls from.
const managerImage = "example.com/crewlet-operator:v0.0.1"

var (
	// crewletImage is the engine image the suite runs every Company on.
	// `make test-e2e` passes it as CREWLET_IMG, crewlet's main as GHCR
	// publishes it unless the caller names another, and the suite pulls it
	// with `make crewlet-image`, which prints the crewlet commit it runs.
	crewletImage string

	// k8sClient talks to the kind cluster as the suite's own control plane
	// would: typed objects from the API module, server-side applied.
	k8sClient client.Client
)

// TestE2E deploys the operator to a kind cluster and checks it from the
// outside. `make test-e2e` creates the cluster and puts the pinned kind and
// kubectl first on PATH. The operator has no admission webhooks, so unlike
// kubebuilder's default suite this one installs no cert-manager.
//
// kubectl's kuberc is disabled unless KUBECTL_KUBERC=true, so a local kubectl
// configuration cannot change what the suite sees.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting crewlet-operator e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

// The operator is deployed once, for every spec: the Company specs need it
// running as much as the Manager specs do, and Ginkgo runs top-level
// containers in a random order.
var _ = BeforeSuite(func() {
	crewletImage = os.Getenv("CREWLET_IMG")
	Expect(crewletImage).NotTo(BeEmpty(),
		"CREWLET_IMG names the engine image to pull and run: run the suite with make test-e2e, which sets it")

	By("building the manager image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager image")

	By("loading the manager image on Kind")
	err = utils.LoadImageToKindClusterWithName(managerImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager image into Kind")

	By("pulling the Crewlet engine image")
	cmd = exec.Command("make", "crewlet-image", fmt.Sprintf("CREWLET_IMG=%s", crewletImage))
	pulled, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to pull the Crewlet engine image")
	// The line naming the engine's commit, in the suite's own output, so a
	// failed run says which engine it failed on.
	_, _ = fmt.Fprint(GinkgoWriter, pulled)

	// Loaded rather than left for the node to pull: the node runs the image
	// this suite pulled, of the commit printed above, and never pulls one,
	// since every class here sets the pull policy Never. IfNotPresent, the
	// default for a tag other than latest, would pull main again should the
	// node's image garbage collection remove the loaded one, and run another
	// commit than the one printed with no error; Never fails the pod
	// instead (ErrImageNeverPull). So main moving on while the suite runs
	// changes nothing.
	By("loading the Crewlet engine image on Kind")
	err = utils.LoadImageToKindClusterWithName(crewletImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the Crewlet engine image into Kind")

	configureKubectlKubeRC()

	By("creating manager namespace")
	cmd = exec.Command("kubectl", "create", "ns", namespace)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

	By("labeling the namespace to enforce the restricted security policy")
	cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
		"pod-security.kubernetes.io/enforce=restricted")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

	By("installing CRDs")
	cmd = exec.Command("make", "install")
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

	By("deploying the controller-manager")
	cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

	k8sClient = newClient()
})

// AfterSuite removes what BeforeSuite deployed. The specs delete their own
// objects first, while the operator still runs to remove its finalizers.
var _ = AfterSuite(func() {
	By("undeploying the controller-manager")
	cmd := exec.Command("make", "undeploy")
	_, _ = utils.Run(cmd)

	By("uninstalling CRDs")
	cmd = exec.Command("make", "uninstall")
	_, _ = utils.Run(cmd)

	By("removing manager namespace")
	cmd = exec.Command("kubectl", "delete", "ns", namespace)
	_, _ = utils.Run(cmd)
})

// newClient returns a client for the cluster of the current kubeconfig
// context, which kind set to the suite's cluster when it created it.
func newClient() client.Client {
	logf.SetLogger(GinkgoLogr)
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(crewletv1alpha1.AddToScheme(scheme))
	cfg, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to read the kubeconfig")
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred(), "Failed to create a client")
	return c
}

// Disable kubectl kuberc by default for test isolation.
// This prevents local kubectl configurations from affecting test behavior.
// To enable kuberc, set: KUBECTL_KUBERC=true
func configureKubectlKubeRC() {
	if os.Getenv("KUBECTL_KUBERC") != "true" {
		By("disabling kubectl kuberc for test isolation")
		err := os.Setenv("KUBECTL_KUBERC", "false")
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to disable kubectl kuberc")
		_, _ = fmt.Fprintf(GinkgoWriter,
			"kubectl kuberc disabled for consistent test behavior (override with KUBECTL_KUBERC=true)\n")
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "kubectl kuberc enabled (KUBECTL_KUBERC=true)\n")
	}
}
