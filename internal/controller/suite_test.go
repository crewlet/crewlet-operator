package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/envtestbin"
	// +kubebuilder:scaffold:imports
)

// The suite runs the controllers' tests against a real kube-apiserver and
// etcd started by envtest, with the CRDs from config/crd/bases installed, so
// the schemas and CEL rules the API server enforces are the ones under test,
// and with the Gateway API's standard CRDs from testdata/gateway-api, so the
// HTTPRoutes the controller writes are validated as a cluster would.
//
// envtest runs no controllers: no StatefulSet controller, no garbage
// collector, no kubelet. A test that needs what they would do (a StatefulSet
// reporting ready pods, a Gateway accepting a route) writes that status
// itself, and says so.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	var err error
	err = crewletaiv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	Expect(gatewayv1.Install(scheme.Scheme)).To(Succeed())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			gatewayAPICRDs,
		},
		ErrorIfCRDPathMissing: true,
	}

	// Outside make (an IDE, a bare go test) KUBEBUILDER_ASSETS is unset; fall
	// back to the binaries `make setup-envtest` downloaded into bin/k8s.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		testEnv.BinaryAssetsDirectory, err = envtestbin.Dir(filepath.Join("..", "..", "bin", "k8s"))
		Expect(err).NotTo(HaveOccurred())
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// updateLive writes mutate's change to obj as the API server holds it,
// reading it again and retrying when the write conflicts. A spec that runs a
// manager shares its objects with the controllers, which write their status
// and metadata at moments of their own choosing; a Get followed by a plain
// Update loses that race now and then with a 409 Conflict, which says
// nothing about the code under test. RetryOnConflict rather than Eventually,
// so that any other error still fails the spec at once.
func updateLive[T client.Object](obj T, mutate func(T)) {
	GinkgoHelper()
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		mutate(obj)
		return k8sClient.Update(ctx, obj)
	})).To(Succeed())
}

// updateStatusLive is updateLive for the status subresource.
func updateStatusLive[T client.Object](obj T, mutate func(T)) {
	GinkgoHelper()
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		mutate(obj)
		return k8sClient.Status().Update(ctx, obj)
	})).To(Succeed())
}
