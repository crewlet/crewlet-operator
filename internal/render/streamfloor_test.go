package render

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"
)

// What a fresh node of Crewlet reserves on its event stream before it boots
// (its engine.BrokerFloorFor): its eight state logs' ceilings at their
// floors, the code host's log among them, which every node reserves whether
// or not the company hosts code, and, with the objects in the stream's own
// bucket, that bucket beside them at a quarter of the stream.
// TestContractStreamFloor holds both to the figure `crewlet validate` names,
// and TestStreamFloorFiguresAreTheEngines holds every hand-written comment
// and page that quotes one to these.
const (
	freshNodeLogsFloor   int64 = 8053063680
	freshNodeBucketFloor int64 = 10737418239
)

// The EngineClass rules that floor the data volume, as controller-gen writes
// them into the CRD: the size's own rule, the least any class boots on, and
// the class's rule for a class whose objects are in the stream's bucket.
var (
	sizeFloorRule = regexp.MustCompile(`^!quantity\(string\(self\)\)\.isLessThan\(quantity\('(\d+Gi)'\)\)$`)
	natsFloorRule = regexp.MustCompile(`^\(has\(self\.objects\) && has\(self\.objects\.backend\) && ` +
		`self\.objects\.backend == 'S3'\) \|\| !has\(self\.data\.storage\.size\) \|\| ` +
		`!quantity\(string\(self\.data\.storage\.size\)\)\.isLessThan\(quantity\('(\d+Gi)'\)\)$`)
)

// TestContractStreamFloor holds the EngineClass's floors on data.storage.size,
// read from the generated CRD rather than restated, to the least stream a
// fresh node of Crewlet boots on, for each place a class keeps its files:
// the smallest volume the API admits renders a stream `crewlet validate` has
// nothing to say about, and a volume a GiB smaller one it says a fresh node
// refuses to boot on, naming the bytes it would reserve. Crewlet says that
// as an advisory at stream.store_max_bytes rather than a refusal: the setting
// is valid (above config.StoreMaxBytesFloor), and what refuses is the broker,
// when the node reserves its logs and its object bucket on an empty stream
// directory.
func TestContractStreamFloor(t *testing.T) {
	s3Floor, natsFloor := engineClassFloors(t)
	for _, c := range []struct {
		bootstrap string
		floor     resource.Quantity
		reserves  int64
	}{
		{bootstrap: caseNATS, floor: natsFloor, reserves: freshNodeBucketFloor},
		{bootstrap: "s3", floor: s3Floor, reserves: freshNodeLogsFloor},
	} {
		below := c.floor.DeepCopy()
		below.Sub(resource.MustParse("1Gi"))
		for _, v := range []struct {
			size  resource.Quantity
			warns []advisory
		}{
			{size: c.floor},
			{size: below, warns: []advisory{{
				path: "stream.store_max_bytes",
				says: fmt.Sprintf("refuses to boot. Raise stream.store_max_bytes to at least %d,", c.reserves),
			}}},
		} {
			t.Run(c.bootstrap+"/"+v.size.String(), func(t *testing.T) {
				in := bootstrapCaseNamed(t, c.bootstrap).input(t)
				in.VolumeSize = v.size
				doc, err := Bootstrap(in)
				if err != nil {
					t.Fatal(err)
				}
				validate(t, bootstrapEnv(doc), v.warns, "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
			})
		}
	}
}

// engineClassFloors reads the two floors on an EngineClass's data volume out
// of the generated CRD: the size's own rule, which every class is held to
// (the least a class whose files are in S3 boots on), and the class's rule
// for files in the stream's own bucket. Each must be there exactly once, in
// the shape this test reads, or the test fails rather than read nothing.
func engineClassFloors(t *testing.T) (s3, nats resource.Quantity) {
	t.Helper()
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
	size := spec.Properties["data"].Properties["storage"].Properties["size"]
	floor := func(rules apiextensionsv1.ValidationRules, pattern *regexp.Regexp, where string) resource.Quantity {
		var found []string
		for _, r := range rules {
			if m := pattern.FindStringSubmatch(r.Rule); m != nil {
				found = append(found, m[1])
			}
		}
		if len(found) != 1 {
			t.Fatalf("%s: want exactly one rule of the shape %s at %s, found %q", path, pattern, where, found)
		}
		return resource.MustParse(found[0])
	}
	return floor(size.XValidations, sizeFloorRule, "spec.data.storage.size"),
		floor(spec.XValidations, natsFloorRule, "spec")
}

// bootstrapCaseNamed is the bootstrap case of that name.
func bootstrapCaseNamed(t *testing.T, name string) *bootstrapCase {
	t.Helper()
	cases := bootstrapCases()
	i := slices.IndexFunc(cases, func(c bootstrapCase) bool { return c.name == name })
	if i < 0 {
		t.Fatalf("no bootstrap case %q", name)
	}
	return &cases[i]
}

// streamFloorQuotes are the hand-written files that quote what a fresh node
// reserves, each with the figures it must quote: nothing generates them from
// the engine, so this is what keeps them the engine's.
var streamFloorQuotes = map[string][]int64{
	engineClassGo:                              {freshNodeLogsFloor, freshNodeBucketFloor},
	"docs/concepts/company-document.md":        {freshNodeLogsFloor, freshNodeBucketFloor},
	troubleshootingMd:                          {freshNodeLogsFloor, freshNodeBucketFloor},
	"internal/engine/enginetest/enginetest.go": {freshNodeBucketFloor},
}

// bytesFigure is a figure of ten digits or more, which these files write
// only to quote a number of bytes.
var bytesFigure = regexp.MustCompile(`\b\d{10,}\b`)

// TestStreamFloorFiguresAreTheEngines holds every file that quotes what a
// fresh node reserves to the figures TestContractStreamFloor holds to the
// engine: each quotes the figures it is listed with, and no other figure of
// that size, so an engine change that moves the floor fails here rather than
// leaving a stale copy beside a corrected one.
func TestStreamFloorFiguresAreTheEngines(t *testing.T) {
	for file, want := range streamFloorQuotes {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		quoted := map[int64]bool{}
		for _, figure := range bytesFigure.FindAllString(string(data), -1) {
			n, err := strconv.ParseInt(figure, 10, 64)
			if err != nil || !slices.Contains(want, n) {
				t.Errorf("%s quotes %s, which is none of the figures it is listed with, %v", file, figure, want)
				continue
			}
			quoted[n] = true
		}
		for _, n := range want {
			if !quoted[n] {
				t.Errorf("%s does not quote %d, which it is listed with", file, n)
			}
		}
	}
}
