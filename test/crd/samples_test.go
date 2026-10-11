package crd_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// TestSamplesAreAccepted applies every object in config/samples, the Nimbus
// example company, into one namespace. A sample the API server refuses is a
// sample nobody can follow.
func TestSamplesAreAccepted(t *testing.T) {
	// The directory is read through an fs.FS, which confines every read to
	// it whatever a kustomization entry says.
	samples := os.DirFS(filepath.Join("..", "..", "config", "samples"))
	var kustomization struct {
		Resources []string `json:"resources"`
	}
	data, err := fs.ReadFile(samples, "kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &kustomization); err != nil {
		t.Fatal(err)
	}

	// Every file in the directory must be listed, or kustomize users would
	// silently miss a kind.
	files, err := fs.Glob(samples, "v1alpha1_*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(kustomization.Resources) {
		t.Fatalf("config/samples holds %d sample files and its kustomization lists %d",
			len(files), len(kustomization.Resources))
	}

	namespace := newNamespace(t)
	kinds := map[string]int{}
	for _, resource := range kustomization.Resources {
		for _, obj := range readObjects(t, samples, resource) {
			if obj.GetKind() != kindEngineClass {
				obj.SetNamespace(namespace)
			} else {
				checkSampleEngineImage(t, resource, obj)
				// Cluster-scoped: keep it from colliding with another test's.
				obj.SetName(namespacePrefix(namespace) + "-" + obj.GetName())
			}
			if err := k8sClient.Create(ctx, obj); err != nil {
				t.Errorf("%s: %s %s refused: %v", resource, obj.GetKind(), obj.GetName(), err)
			}
			kinds[obj.GetKind()]++
		}
	}
	for _, kind := range []string{
		kindEngineClass, kindCompany, "Unit", "Seat", "LLMProvider", "MCPServer", "Integration", "Schedule",
	} {
		if kinds[kind] == 0 {
			t.Errorf("config/samples has no %s", kind)
		}
	}
}

// checkSampleEngineImage holds a sample class's engine image to the image
// every suite runs (the Makefile's CREWLET_MAIN_IMG, CREWLET_IMG's default),
// so a reader who applies the samples runs the engine the suites ran, and a
// sample that names another image, or a Makefile that moves to one, fails
// here.
func checkSampleEngineImage(t *testing.T, resource string, class *unstructured.Unstructured) {
	t.Helper()
	image, _, err := unstructured.NestedString(class.Object, "spec", "engine", "image")
	if err != nil {
		t.Fatalf("%s: %v", resource, err)
	}
	if want := enginetest.Image(t); image != want {
		t.Errorf("%s: EngineClass %s runs %q, want %s, the engine image every suite runs (CREWLET_MAIN_IMG)",
			resource, class.GetName(), image, want)
	}
}

// readObjects decodes every YAML document of a file.
func readObjects(t *testing.T, dir fs.FS, path string) []*unstructured.Unstructured {
	t.Helper()
	data, err := fs.ReadFile(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objects []*unstructured.Unstructured
	for {
		obj := &unstructured.Unstructured{}
		err := decoder.Decode(&obj.Object)
		if errors.Is(err, io.EOF) {
			return objects
		}
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(obj.Object) > 0 {
			objects = append(objects, obj)
		}
	}
}
