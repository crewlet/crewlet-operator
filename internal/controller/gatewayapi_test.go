package controller

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/yaml"
)

// gatewayAPICRDs holds the Gateway API CRDs the suite installs, copied by
// `make gateway-api-crds`.
var gatewayAPICRDs = filepath.Join("testdata", "gateway-api")

// TestGatewayAPICRDsMatchTheGoModule holds the vendored CRDs to the release of
// sigs.k8s.io/gateway-api the operator is built with: a suite that ran
// against other CRDs than the types it writes would pass on fields a cluster
// of that release refuses, or the other way around.
func TestGatewayAPICRDsMatchTheGoModule(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("the test binary carries no build information to read the gateway-api version from")
	}
	var want string
	for _, dep := range info.Deps {
		if dep.Path == "sigs.k8s.io/gateway-api" {
			want = dep.Version
		}
	}
	if want == "" {
		t.Fatal("sigs.k8s.io/gateway-api is not a dependency of the test binary")
	}

	crds := os.DirFS(gatewayAPICRDs)
	files, err := fs.Glob(crds, "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no CRDs in %s: run make gateway-api-crds", gatewayAPICRDs)
	}
	for _, file := range files {
		raw, err := fs.ReadFile(crds, file)
		if err != nil {
			t.Fatal(err)
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(raw, crd); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if got := crd.Annotations["gateway.networking.k8s.io/bundle-version"]; got != want {
			t.Errorf("%s is from Gateway API %s, and go.mod requires %s: run make gateway-api-crds", file, got, want)
		}
	}
}

// TestGatewayAPIInstalledWithoutTheKind is the cluster without the Gateway
// API: the operator must start there and leave HTTPRoutes alone. (The suite's
// envtest serves the kind; the Ginkgo specs cover the other answer.)
func TestGatewayAPIInstalledWithoutTheKind(t *testing.T) {
	installed, err := GatewayAPIInstalled(meta.NewDefaultRESTMapper(nil))
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Fatal("GatewayAPIInstalled reported the Gateway API on a cluster that serves no HTTPRoute kind")
	}
}
