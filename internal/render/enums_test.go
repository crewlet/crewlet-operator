package render

import (
	"testing"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// EVERY CLOUDFLARE LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryCloudflareLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.CloudflareLevel]string{
		v1alpha1.CloudflareLevelRead: "read", v1alpha1.CloudflareLevelOperate: "operate",
		v1alpha1.CloudflareLevelConfigure: "configure", "": "",
	} {
		if got := cloudflareLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}
