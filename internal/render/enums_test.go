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

// EVERY SCALEWAY LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read; a Scaleway key's two values map
// onto the fields the engine seals them under.
func TestEveryScalewayLevelAndFieldMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.ScalewayLevel]string{
		v1alpha1.ScalewayLevelRead: "read", v1alpha1.ScalewayLevelOperate: "operate",
		v1alpha1.ScalewayLevelAdminister: levelAdminister, "": "",
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

// EVERY GOOGLE CLOUD LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as viewer.
func TestEveryGoogleCloudLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.GoogleCloudLevel]string{
		v1alpha1.GoogleCloudLevelViewer: "viewer", v1alpha1.GoogleCloudLevelOperator: "operator",
		v1alpha1.GoogleCloudLevelAdminister: levelAdminister, "": "",
	} {
		if got := googleCloudLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// A SERVER NAMED FOR A SEAT CREDENTIAL KIND CARRIES THE SEAT'S OWN CREDENTIAL,
// by the name Crewlet gives it: google-cloud for Google Cloud, whose kind has
// an underscore no server name can hold, and jira or confluence for
// Atlassian's. A server named for nothing the engine binds carries none.
func TestAServerNamedForASeatCredentialKindCarriesTheSeats(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		VendorAtlassian: true, VendorCloudflare: true, VendorDatadog: true, VendorDigitalOcean: true,
		VendorExpo: true, VendorGitHub: true, VendorGitLab: true, "google-cloud": true, VendorMattermost: true,
		VendorScaleway: true, VendorSlack: true, VendorJira: true, VendorConfluence: true,
		"google": false, "google-cloud-monitoring": false, "linear": false, "": false,
	} {
		if got := seatCredentialServer(name); got != want {
			t.Errorf("a server named %q carries a seat credential: %v, want %v", name, got, want)
		}
	}
}
