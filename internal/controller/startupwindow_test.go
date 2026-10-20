package controller

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// s3StartupRule is the EngineClass rule that floors data.startupSeconds for a
// class whose objects are in S3, as controller-gen writes it into the CRD.
var s3StartupRule = regexp.MustCompile(`^!has\(self\.data\.startupSeconds\) \|\| !has\(self\.objects\) \|\| ` +
	`!has\(self\.objects\.backend\) \|\| self\.objects\.backend != 'S3' \|\| ` +
	`self\.data\.startupSeconds >= (\d+)$`)

// TestStartupSecondsFloorIsTheBootWindow holds the EngineClass's floors on
// data.startupSeconds, read from the generated CRD rather than restated, to
// the window the controller derives for a data pod's boot alone: a class may
// name no window shorter than the engine's own bounds on that boot, for each
// place it keeps its files, and the controller never derives one shorter.
func TestStartupSecondsFloorIsTheBootWindow(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_engineclasses.yaml")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(crd.Spec.Versions) != 1 || crd.Spec.Versions[0].Schema == nil {
		t.Fatalf("%s: want one version with a schema", path)
	}
	spec := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	field := spec.Properties["data"].Properties["startupSeconds"]
	if field.Minimum == nil {
		t.Fatalf("%s: spec.data.startupSeconds has no minimum", path)
	}
	var s3 []string
	for _, r := range spec.XValidations {
		if m := s3StartupRule.FindStringSubmatch(r.Rule); m != nil {
			s3 = append(s3, m[1])
		}
	}
	if len(s3) != 1 {
		t.Fatalf("%s: want exactly one rule of the shape %s at spec, found %q", path, s3StartupRule, s3)
	}
	s3Floor, err := strconv.ParseInt(s3[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for backend, floor := range map[crewletaiv1alpha1.ObjectBackend]int64{
		crewletaiv1alpha1.ObjectBackendNATS: int64(*field.Minimum),
		crewletaiv1alpha1.ObjectBackendS3:   s3Floor,
	} {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Objects.Backend = backend
		if want := int64(dataPodBootWindow(class)); floor != want {
			t.Errorf("the API floors data.startupSeconds at %d with objects.backend %s, and a data pod's boot "+
				"window there is %d", floor, backend, want)
		}
	}
}

// TestStartupWindowFiguresAreTheControllers holds every hand-written comment
// and page that quotes a startup window the controller derives to the window
// it derives: nothing generates them from the constants, so a window the
// engine moves, and the constant with it, leaves each of them quoting the
// old figure until this fails.
func TestStartupWindowFiguresAreTheControllers(t *testing.T) {
	t.Parallel()
	classOn := func(backend crewletaiv1alpha1.ObjectBackend) *crewletaiv1alpha1.EngineClass {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Objects.Backend = backend
		return class
	}
	nats, s3 := classOn(crewletaiv1alpha1.ObjectBackendNATS), classOn(crewletaiv1alpha1.ObjectBackendS3)
	data := []int64{int64(dataPodBootWindow(nats)), int64(dataPodBootWindow(s3))}
	seats := []int64{int64(seatPodStartupSeconds(nats)), int64(seatPodStartupSeconds(s3))}
	for file, figures := range map[string][]int64{
		"api/v1alpha1/engineclass_types.go": data,
		"docs/concepts/runtime.md":          append(append([]int64{}, data...), seats...),
		"docs/guides/troubleshooting.md":    data,
	} {
		src, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(src), ",", "")
		for _, figure := range figures {
			if !regexp.MustCompile(`\b` + strconv.FormatInt(figure, 10) + `\b`).MatchString(text) {
				t.Errorf("%s does not quote %d seconds, a startup window the controller derives", file, figure)
			}
		}
	}
}
