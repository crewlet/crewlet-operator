package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// TestDataPodStartupSeconds holds the data pod's startup window to the
// recovery it has to cover: the boot's own bounds plus six seconds for every
// GiB the volume may hold, rounded up, unless the class names a window, and
// never past the API's day.
func TestDataPodStartupSeconds(t *testing.T) {
	t.Parallel()
	classWith := func(seconds int32) *crewletaiv1alpha1.EngineClass {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Data.StartupSeconds = seconds
		return class
	}
	for _, c := range []struct {
		name    string
		class   *crewletaiv1alpha1.EngineClass
		volume  string
		seconds int32
	}{
		{"a volume of no size", classWith(0), "0", 300},
		{"a quantity below zero, which no claim has", classWith(0), "-2Gi", 300},
		{"the smallest volume", classWith(0), "8Gi", 300 + 8*6},
		{"a volume of whole GiB", classWith(0), "100Gi", 300 + 100*6},
		{"a part of a GiB counts as one", classWith(0), "10737418241", 300 + 11*6},
		{"a volume in decimal units", classWith(0), "500G", 300 + 466*6},
		{"a volume whose window would pass a day", classWith(0), "20Ti", maxStartupSeconds},
		{"the class's window, whatever the volume", classWith(1200), "1Ti", 1200},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := dataPodStartupSeconds(c.class, resource.MustParse(c.volume)); got != c.seconds {
				t.Errorf("dataPodStartupSeconds(%s) = %d, want %d", c.volume, got, c.seconds)
			}
		})
	}
}
