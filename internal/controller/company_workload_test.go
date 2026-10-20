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
		{"the smallest volume", classWith(0), "12Gi", 300 + 12*6},
		{"a volume of whole GiB", classWith(0), "100Gi", 300 + 100*6},
		{"a part of a GiB counts as one", classWith(0), "10737418241", 300 + 11*6},
		{"a volume in decimal units", classWith(0), "500G", 300 + 466*6},
		{"a volume whose window would pass a day", classWith(0), "20Ti", maxStartupSeconds},
		{"the largest volume whose window is a day", classWith(0), "14350Gi", maxStartupSeconds},
		{"a GiB less", classWith(0), "14349Gi", maxStartupSeconds - 6},
		{"a volume whose Value saturates", classWith(0), "16Ei", maxStartupSeconds},
		{"a volume whose Value wraps below zero", classWith(0), "9223372036854775808", maxStartupSeconds},
		{"a volume whose Value reads zero", classWith(0), "1e30", maxStartupSeconds},
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

// TestStartupFailureThreshold holds the startup probe's failures to the window
// they make up, counted as the kubelet counts it: from a first probe that may
// land at once, threshold-1 periods, never shorter than the window and short
// of a period more.
func TestStartupFailureThreshold(t *testing.T) {
	t.Parallel()
	for _, seconds := range []int32{300, 301, 304, 305, 306, 360, 396, 1200, maxStartupSeconds - 1, maxStartupSeconds} {
		threshold := startupFailureThreshold(seconds)
		if window := (threshold - 1) * probePeriodSeconds; window < seconds || window >= seconds+probePeriodSeconds {
			t.Errorf("startupFailureThreshold(%d) = %d, a window of %ds after the first probe", seconds, threshold, window)
		}
	}
}
