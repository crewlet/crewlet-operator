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
		v1alpha1.CloudflareLevelRead: levelRead, v1alpha1.CloudflareLevelOperate: levelOperate,
		v1alpha1.CloudflareLevelConfigure: "configure", "": "",
	} {
		if got := cloudflareLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY DIGITALOCEAN LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as read.
func TestEveryDigitalOceanLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.DigitalOceanLevel]string{
		v1alpha1.DigitalOceanLevelRead: levelRead, v1alpha1.DigitalOceanLevelOperate: levelOperate,
		v1alpha1.DigitalOceanLevelAdminister: levelAdminister, "": "",
	} {
		if got := digitaloceanLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY AWS LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryAWSLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.AWSLevel]string{
		v1alpha1.AWSLevelRead: levelRead, v1alpha1.AWSLevelOperate: levelOperate,
		v1alpha1.AWSLevelAdminister: levelAdminister, "": "",
	} {
		if got := awsLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}
