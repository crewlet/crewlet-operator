package crd_test

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/crewlet/crewlet-operator/test/docs"
)

// TestGettingStartedIsAccepted applies every object the getting-started
// guide hands to kubectl, as the guide applies them. A page a reader cannot
// follow is caught here rather than by the reader.
func TestGettingStartedIsAccepted(t *testing.T) {
	manifests, err := docs.Manifests(filepath.Join("..", "..", "docs", "getting-started.md"))
	if err != nil {
		t.Fatal(err)
	}
	namespace := newNamespace(t)
	// Cluster-scoped, so renamed to keep it from colliding with another
	// test's, and the Company follows it.
	class := namespacePrefix(namespace) + "-local"
	kinds := map[string]int{}
	var guideClass string
	for _, manifest := range manifests {
		decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)
		for {
			obj := &unstructured.Unstructured{}
			if err := decoder.Decode(&obj.Object); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatal(err)
			}
			switch obj.GetKind() {
			case kindEngineClass:
				guideClass = obj.GetName()
				if _, isDefault := obj.GetAnnotations()["crewlet.ai/is-default-class"]; isDefault {
					t.Errorf("the guide's EngineClass is a default class: the full example's samples bring another, " +
						"and two defaults leave every Company that names none unaccepted")
				}
				obj.SetName(class)
			case kindCompany:
				// Naming the class keeps the Company running when the full
				// example brings the samples' default class.
				if named, _, _ := unstructured.NestedString(obj.Object, "spec", "engineClassName"); named != guideClass {
					t.Errorf("Company %s names EngineClass %q, not the guide's %q", obj.GetName(), named, guideClass)
				}
				if err := unstructured.SetNestedField(obj.Object, class, "spec", "engineClassName"); err != nil {
					t.Fatal(err)
				}
				obj.SetNamespace(namespace)
			default:
				obj.SetNamespace(namespace)
			}
			if err := k8sClient.Create(ctx, obj); err != nil {
				t.Errorf("%s %s refused: %v", obj.GetKind(), obj.GetName(), err)
			}
			kinds[obj.GetKind()]++
		}
	}
	for _, kind := range []string{kindEngineClass, kindCompany, "LLMProvider", "Unit", "Seat"} {
		if kinds[kind] == 0 {
			t.Errorf("the guide applies no %s; the test reads it wrong, or the guide changed shape", kind)
		}
	}
}
