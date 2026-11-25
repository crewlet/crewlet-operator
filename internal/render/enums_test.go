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
		v1alpha1.CloudflareLevelConfigure: "configure", v1alpha1.CloudflareLevelSecure: "secure", "": "",
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

// EVERY GOOGLE ANALYTICS LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and
// unset onto nothing, which the engine reads as read.
func TestEveryGoogleAnalyticsLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.GoogleAnalyticsLevel]string{
		v1alpha1.GoogleAnalyticsLevelRead: levelRead, v1alpha1.GoogleAnalyticsLevelOperate: levelOperate,
		v1alpha1.GoogleAnalyticsLevelAdminister: levelAdminister, "": "",
	} {
		if got := googleAnalyticsLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY GOOGLE ANALYTICS ROLE A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as Operate's own, Editor's.
func TestEveryGoogleAnalyticsRoleMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for role, want := range map[v1alpha1.GoogleAnalyticsRole]string{
		v1alpha1.GoogleAnalyticsRoleMarketer: "marketer", "": "",
	} {
		if got := googleAnalyticsRole(role); got != want {
			t.Errorf("role %q renders %q, want %q", role, got, want)
		}
	}
}

// EVERY ADMOB LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryAdMobLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.AdMobLevel]string{
		v1alpha1.AdMobLevelRead: levelRead, v1alpha1.AdMobLevelOperate: levelOperate,
		v1alpha1.AdMobLevelAdminister: levelAdminister, "": "",
	} {
		if got := adMobLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY ADSENSE LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryAdSenseLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.AdSenseLevel]string{
		v1alpha1.AdSenseLevelRead: levelRead, v1alpha1.AdSenseLevelOperate: levelOperate,
		v1alpha1.AdSenseLevelAdminister: levelAdminister, "": "",
	} {
		if got := adSenseLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY APP STORE CONNECT LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and
// unset onto nothing, which the engine reads as read.
func TestEveryAppStoreConnectLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.AppStoreConnectLevel]string{
		v1alpha1.AppStoreConnectLevelRead: levelRead, v1alpha1.AppStoreConnectLevelOperate: levelOperate,
		v1alpha1.AppStoreConnectLevelAdminister: levelAdminister, "": "",
	} {
		if got := appStoreConnectLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY APPLE ADS LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryAppleAdsLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.AppleAdsLevel]string{
		v1alpha1.AppleAdsLevelRead: levelRead, v1alpha1.AppleAdsLevelOperate: levelOperate,
		v1alpha1.AppleAdsLevelAdminister: levelAdminister, "": "",
	} {
		if got := appleAdsLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY META ADS LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset onto
// nothing, which the engine reads as read.
func TestEveryMetaAdsLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.MetaAdsLevel]string{
		v1alpha1.MetaAdsLevelRead: levelRead, v1alpha1.MetaAdsLevelOperate: levelOperate,
		v1alpha1.MetaAdsLevelAdminister: levelAdminister, "": "",
	} {
		if got := metaAdsLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY META ADS IDENTITY A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as company.
func TestEveryMetaAdsIdentityMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for identity, want := range map[v1alpha1.MetaAdsIdentity]string{
		v1alpha1.MetaAdsIdentityOwn: "own", v1alpha1.MetaAdsIdentityCompany: "company", "": "",
	} {
		if got := metaAdsIdentity(identity); got != want {
			t.Errorf("Meta Ads identity %q renders %q, want %q", identity, got, want)
		}
	}
}

// EVERY GOOGLE ADS LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as read.
func TestEveryGoogleAdsLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.GoogleAdsLevel]string{
		v1alpha1.GoogleAdsLevelRead: levelRead, v1alpha1.GoogleAdsLevelOperate: levelOperate,
		v1alpha1.GoogleAdsLevelAdminister: levelAdminister, "": "",
	} {
		if got := googleAdsLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY SEARCH CONSOLE LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and
// unset onto nothing, which the engine reads as read.
func TestEverySearchConsoleLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.SearchConsoleLevel]string{
		v1alpha1.SearchConsoleLevelRead: levelRead, v1alpha1.SearchConsoleLevelOperate: levelOperate,
		v1alpha1.SearchConsoleLevelAdminister: levelAdminister, "": "",
	} {
		if got := searchConsoleLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}

// EVERY GOOGLE PLAY LEVEL A SEAT MAY NAME MAPS ONTO THE ENGINE'S, and unset
// onto nothing, which the engine reads as read.
func TestEveryGooglePlayLevelMapsOntoTheEngines(t *testing.T) {
	t.Parallel()
	for level, want := range map[v1alpha1.GooglePlayLevel]string{
		v1alpha1.GooglePlayLevelRead: levelRead, v1alpha1.GooglePlayLevelOperate: levelOperate,
		v1alpha1.GooglePlayLevelAdminister: levelAdminister, "": "",
	} {
		if got := googlePlayLevel(level); got != want {
			t.Errorf("level %q renders %q, want %q", level, got, want)
		}
	}
}
