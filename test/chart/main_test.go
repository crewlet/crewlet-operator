// Package chart_test renders the Helm chart with the helm binary the
// Makefile pins, installs its RBAC and admission policies into a real
// kube-apiserver (envtest), and checks what they let a subject do by acting
// as that subject. It is where the policies that RBAC cannot express are
// held to their word: the manager's writes, and the tenant provisioner's.
package chart_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/envtestbin"
)

// The release the chart is rendered as, which names the manager's
// ServiceAccount, and the tenant provisioner's ServiceAccount.
const (
	release            = "crewlet-operator"
	releaseNamespace   = "crewlet-operator-system"
	managerUser        = "system:serviceaccount:" + releaseNamespace + ":crewlet-operator-controller-manager"
	provisionerNS      = "platform"
	provisionerName    = "control-plane"
	provisionerUser    = "system:serviceaccount:" + provisionerNS + ":" + provisionerName
	tenantAccessRole   = release + "-tenant-access"
	tenantLabel        = "crewlet.ai/tenant"
	podSecurityEnforce = "pod-security.kubernetes.io/enforce"
)

var (
	ctx    = context.Background()
	cfg    *rest.Config
	scheme = runtime.NewScheme()
	admin  client.Client
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run starts the API server, installs what the chart renders, runs the tests
// and stops the API server again.
func run(m *testing.M) int {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		dir, err := envtestbin.Dir(filepath.Join("..", "..", "bin", "k8s"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		testEnv.BinaryAssetsDirectory = dir
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "stopping envtest:", err)
		}
	}()
	var err error
	cfg, err = testEnv.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting envtest:", err)
		return 1
	}
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, crewletv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	admin, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := install(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// helmBinary is the helm the Makefile installs, or HELM.
func helmBinary() string {
	if path := os.Getenv("HELM"); path != "" {
		return path
	}
	return filepath.Join("..", "..", "bin", "helm")
}

// install renders the chart, with the tenant provisioner on, and creates the
// cluster-scoped RBAC and admission objects it holds.
func install() error {
	helm := helmBinary()
	cmd := exec.CommandContext(ctx, helm, "template", release, filepath.Join("..", "..", "dist", "chart"),
		"--namespace", releaseNamespace,
		"--set", "tenantProvisioner.enabled=true",
		"--set", "tenantProvisioner.subjects[0].kind=ServiceAccount",
		"--set", "tenantProvisioner.subjects[0].name="+provisionerName,
		"--set", "tenantProvisioner.subjects[0].namespace="+provisionerNS,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// A missing helm fails the suite rather than skipping it: these
		// policies are tested here and nowhere else.
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no helm binary at %s: run `make helm`, or set HELM", helm)
		}
		return fmt.Errorf("helm template: %w\n%s", err, stderr.String())
	}
	wanted := map[string]bool{
		"ClusterRole": true, "ClusterRoleBinding": true,
		"ValidatingAdmissionPolicy": true, "ValidatingAdmissionPolicyBinding": true,
	}
	decoder := utilyaml.NewYAMLOrJSONDecoder(&stdout, 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading what helm rendered: %w", err)
		}
		if !wanted[obj.GetKind()] {
			continue
		}
		if err := admin.Create(ctx, obj); err != nil {
			return fmt.Errorf("creating %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
}

// as is a client that acts as the user named, as the API server would see a
// request from that ServiceAccount.
func as(t *testing.T, user string) client.Client {
	t.Helper()
	impersonated := rest.CopyConfig(cfg)
	impersonated.Impersonate = rest.ImpersonationConfig{
		UserName: user,
		Groups:   []string{"system:serviceaccounts", "system:authenticated"},
	}
	c, err := client.New(impersonated, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
