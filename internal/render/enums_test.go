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

// EVERY DIGITALOCEAN LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as read.
func TestEveryDigitalOceanLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.DigitalOceanLevel]string{
		v1alpha1.DigitalOceanLevelRead: levelRead, v1alpha1.DigitalOceanLevelOperate: levelOperate,
		v1alpha1.DigitalOceanLevelAdminister: "administer", "": "",
	} {
		if got := digitaloceanLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY SCALEWAY LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read; a Scaleway key's two values map
// onto the fields the engine seals them under.
func TestEveryScalewayLevelAndFieldMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.ScalewayLevel]string{
		v1alpha1.ScalewayLevelRead: "read", v1alpha1.ScalewayLevelOperate: "operate",
		v1alpha1.ScalewayLevelAdminister: "administer", "": "",
	} {
		if got := scalewayLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
	for field, want := range map[v1alpha1.ProvisionedField]string{
		v1alpha1.ProvisionedSecretKey: "SECRET_KEY", v1alpha1.ProvisionedAccessKey: "ACCESS_KEY",
	} {
		if got := provisionedField(field); got != want {
			t.Errorf("provisioned field %q renders %q, want %q", field, got, want)
		}
	}
	for field, want := range map[v1alpha1.SeatCredentialField]string{
		v1alpha1.SeatCredentialSecretKey: "SECRET_KEY", v1alpha1.SeatCredentialAccessKey: "ACCESS_KEY",
	} {
		if got := seatCredentialField(field); got != want {
			t.Errorf("seat credential field %q renders %q, want %q", field, got, want)
		}
	}
}
