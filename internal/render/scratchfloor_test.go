package render

import (
	"regexp"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

// scratchFloorRule is an EngineClass's floor on a scratchSizeLimit, as
// controller-gen writes it into the CRD.
var scratchFloorRule = regexp.MustCompile(`^!quantity\(string\(self\)\)\.isLessThan\(quantity\('(\d+Mi)'\)\)$`)

// TestTheScratchFloorHoldsTheEnginesSpoolRoom holds the EngineClass's floors on
// a scratch volume, a data pod's and a seat pod's, read from the generated CRD
// rather than restated, to MinScratchSizeLimit, and that to the least whole
// MiB that holds the engine's native library, the one room every session spool
// of its process shares (engineSpoolRoomBytes, which the mirror table holds to
// the engine's spoolRoomBytes) and scratchRestBytes beside them: a floor under
// it is a volume the spools alone can fill to an eviction, and one over it
// refuses a limit the engine runs on.
func TestTheScratchFloorHoldsTheEnginesSpoolRoom(t *testing.T) {
	floor := resource.MustParse(MinScratchSizeLimit)
	holds := int64(engineLibraryBytes + engineSpoolRoomBytes + scratchRestBytes)
	if least := (holds + 1<<20 - 1) >> 20 << 20; floor.Value() != least {
		t.Errorf("MinScratchSizeLimit is %s, %d bytes; the least whole MiB that holds the library (%d), the "+
			"spool room (%d) and the rest (%d) is %d", MinScratchSizeLimit, floor.Value(), engineLibraryBytes,
			engineSpoolRoomBytes, scratchRestBytes, least)
	}

	var limits []any
	collectProperty(crdOf(t, "engineclasses"), "scratchSizeLimit", &limits)
	if len(limits) != 2 {
		t.Fatalf("the EngineClass CRD has %d scratchSizeLimit fields, want 2, a data pod's and a seat pod's", len(limits))
	}
	for _, limit := range limits {
		var floors []string
		for _, rule := range rules(limit) {
			if m := scratchFloorRule.FindStringSubmatch(rule); m != nil {
				floors = append(floors, m[1])
			}
		}
		if len(floors) != 1 || !floor.Equal(resource.MustParse(floors[0])) {
			t.Errorf("a scratchSizeLimit of the EngineClass CRD is floored at %v, want one floor of %s "+
				"(MinScratchSizeLimit)", floors, MinScratchSizeLimit)
		}
	}
}

// collectProperty appends to found the schema of every property named name,
// at any depth of the decoded schema node.
func collectProperty(node any, name string, found *[]any) {
	switch n := node.(type) {
	case map[string]any:
		if properties, ok := n["properties"].(map[string]any); ok {
			if property, ok := properties[name]; ok {
				*found = append(*found, property)
			}
		}
		for _, child := range n {
			collectProperty(child, name, found)
		}
	case []any:
		for _, child := range n {
			collectProperty(child, name, found)
		}
	}
}
