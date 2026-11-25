package crd_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The messages of the rules over the vendors the engine can provision.
const (
	msgEngineVendors = "provisioning Engine applies to gitlab, mattermost, datadog, cloudflare, expo, " +
		"digitalocean, scaleway, aws, googleCloud and atlassian"
	msgEngineRequired = "datadog, cloudflare, expo, digitalocean, scaleway, aws, googleCloud and atlassian " +
		"need provisioning " +
		"Engine"
	msgGitLabEngine     = "gitlab.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgMattermostEng    = "mattermost.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgOneSite          = "url and cloudID are two ways to name one site: give one"
	msgRouteTo          = "give exactly one of seatRef and dismiss: true"
	msgSeatSource       = "exactly one of secretKeyRef and provisioned must be set"
	msgProvisionedField = "the engine mints a GitLab account's Token"
	msgURLScheme        = "url must be an absolute URL starting with http:// or https://"
	msgURLReference     = "url may not hold a ${NAME} reference"
	msgSiteURL          = "siteURL must be an absolute URL starting with http:// or https://"
	// msgEngineCalls ends the provisioning Engine rule's message, for the
	// vendors where the engine makes every call itself.
	msgEngineCalls = "gives no seat one at App Store Connect, Apple Ads or Meta Ads, where it makes every call itself"
	// msgEngineNowhere is the end of msgEngineVendors' rule: every vendor at
	// which the engine creates no account.
	msgEngineNowhere = "the engine creates no account and mints no credential at Slack, GitHub, Jira, Confluence, " +
		"Google Analytics, Google Ads, Search Console, Google Play, AdMob or AdSense"
)

// The spend cap values the growth cases repeat, and the name of the case
// that lists an approver twice.
const (
	capPerChange        = "50.00"
	currencyUSD         = "USD"
	currencyEUR         = "EUR"
	approverListedTwice = "an approver listed twice is refused"
)

// The cases every growth Integration repeats: provisioning External, which is
// its only provisioning, and an approver named twice, beside secondApprover,
// the other approver its fullest case names.
const (
	nameExternalAccepted = "provisioning External is accepted"
	nameApproverTwice    = "an approver named twice is refused"
	secondApprover       = "omar"
	// scApprover is the person the Search Console cases name first.
	scApprover = "ines"
)

// nameUpperCaseURL names the case that offers a vendor's url with its scheme
// in upper case: Crewlet reads a vendor's scheme as written and refuses
// HTTPS://, so the API does too, rather than admit an Integration the engine
// will not apply.
const nameUpperCaseURL = "a url with an upper-case scheme is refused"

// The case name, the refusals and the zone the Cloudflare, Expo, DigitalOcean
// and AWS cases repeat.
const (
	externalRefused = "provisioning External is refused"
	engineRefused   = "provisioning Engine is refused"
	// secondVendorRefused names the case that sets another vendor beside
	// the one a vendor's cases are about.
	secondVendorRefused = "a second vendor beside it is refused"
	// engineOperate is the engine's spelling of an Operate level, which the
	// API refuses.
	engineOperate = "operate"
	// engineOwn is the engine's spelling of an Own identity, which the API
	// refuses.
	engineOwn            = "own"
	expoAccountRefused   = "spec.expo.accountName: Invalid value"
	doTeamRefused        = "spec.digitalocean.team: Invalid value"
	nimbusZone           = "nimbus.example"
	awsSlugRefused       = "spec.aws.slug: Invalid value"
	awsConnectionRefused = "spec.aws.connection: Invalid value"
	awsHomeRegion        = "eu-west-1"
)

// awsConnection is a CrewletConnection output of the nimbus stack in
// eu-west-1, as the stack prints it.
const awsConnection = "arn:aws:rolesanywhere:eu-west-1:123456789012:" +
	"trust-anchor/0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0," +
	"arn:aws:rolesanywhere:eu-west-1:123456789012:profile/1a2b3c4d-5e6f-7081-92a3-b4c5d6e7f809," +
	"arn:aws:iam::123456789012:role/crewlet/nimbus/crewlet-nimbus-provisioner," +
	"arn:aws:rolesanywhere:eu-west-1:123456789012:profile/2b3c4d5e-6f70-8192-a3b4-c5d6e7f8091a," +
	"arn:aws:iam::123456789012:role/crewlet/nimbus/crewlet-nimbus-broker"

// awsAtlasOutput is what the same stack's output ends with once it holds the
// atlas's role: its profile and the role.
const awsAtlasOutput = ",arn:aws:rolesanywhere:eu-west-1:123456789012:profile/3c4d5e6f-7081-92a3-b4c5-d6e7f8091a2b" +
	",arn:aws:iam::123456789012:role/crewlet/nimbus/crewlet-nimbus-atlas"

// The refusals, the project and the pool the Google Cloud cases repeat.
const (
	googleIdentityRefused = "spec.googleCloud.identityProject: Invalid value"
	googleProviderRefused = "spec.googleCloud.provider: Invalid value"
	googleProjectsRefused = "spec.integrations.googleCloud.projects[0]: Invalid value"
	googleProject         = "nimbus-prod"
	// googleProviderPool is the workload identity pool the fixture's
	// provider is in: a pool alone, which names no provider.
	googleProviderPool = "projects/123456789012/locations/global/workloadIdentityPools/crewlet"
)

// The refusals and the property the Google Analytics cases repeat.
const (
	gaPropertyRefused      = "spec.googleAnalytics.properties[0]: Invalid value"
	gaSeatPropertyRefused  = "spec.integrations.googleAnalytics.properties[0]: Invalid value"
	gaProperty             = "123456789"
	gaApprover             = "maya"
	msgGAPropertiesWhileOn = "properties is required while the seat is on: a Google Analytics agent with no " +
		"property can reach nothing"
)

// The refusals, the account, the OAuth client and the approver the AdMob
// cases repeat.
const (
	adMobAccountRefused     = "spec.admob.accounts[0]: Invalid value"
	adMobSeatAccountRefused = "spec.integrations.admob.accounts[0]: Invalid value"
	adMobClientIDRefused    = "spec.admob.oauth.clientID: Invalid value"
	adMobAccount            = "pub-1234567890123456"
	adMobClientID           = "123456789012-abc123def456.apps.googleusercontent.com"
	clientIDNoProject       = "abc123def456.apps.googleusercontent.com"
	adMobApprover           = "ines"
	msgAdMobAccountsWhileOn = "accounts is required while the seat is on: an AdMob agent with no account can " +
		"reach nothing"
	adMobSecondAccount = "pub-6543210987654321"
	// upperPublisher is a publisher ID in upper case, which Google never
	// writes.
	upperPublisher = "PUB-1234567890123456"
)

// The refusals the AdSense cases repeat. Its publisher IDs and OAuth client
// have AdMob's shapes, so the cases name AdMob's values.
const (
	adSenseAccountRefused     = "spec.adsense.accounts[0]: Invalid value"
	adSenseSeatAccountRefused = "spec.integrations.adsense.accounts[0]: Invalid value"
	adSenseClientIDRefused    = "spec.adsense.oauth.clientID: Invalid value"
	msgAdSenseAccountsWhileOn = "accounts is required while the seat is on: an AdSense agent with no account can " +
		"reach nothing"
)

// The accounts, approver and messages the Google Ads cases repeat.
const (
	adsCustomer        = "1234567890"
	adsManager         = "9876543210"
	adsApprover        = "noah"
	adsCustomerRefused = "spec.googleAds.customers[0].id: Invalid value"
	msgAdsSpendOutside = "spend.accounts may cap only an account customers lists"
	msgAdsSpendID      = "spend.accounts caps a Google Ads account by its 10-digit customer ID"
	msgAdsCustomersOn  = "customers is required while the seat is on: a Google Ads agent with no account can " +
		"reach nothing"
	msgAdsManagerSelf = "manager is the other account the identity is added to, never the account itself"
)

// adsCustomers is n distinct customer IDs.
func adsCustomers(n int) []v1.GoogleAdsCustomer {
	out := make([]v1.GoogleAdsCustomer, n)
	for i := range out {
		out[i] = v1.GoogleAdsCustomer{ID: fmt.Sprintf("1%09d", i)}
	}
	return out
}

// adsCap is both caps of the account id, in euros.
func adsCap(id string) v1.GrowthSpendCap {
	return v1.GrowthSpendCap{Account: id, Currency: currencyEUR, PerChange: capPerChange, Daily: "200"}
}

// The sites and refusals the Search Console cases repeat.
const (
	scPrefix       = "https://www.example.com/"
	scDomain       = "sc-domain:example.com"
	scSiteRefused  = "spec.searchConsole.sites[0]: Invalid value"
	scSeatRefused  = "spec.integrations.searchConsole.sites[0]: Invalid value"
	msgSCSitesOn   = "sites is required while the seat is on: a Search Console agent with no property can reach nothing"
	scSecondPrefix = "https://shop.example.com/de/"
)

// scSites is n distinct Domain properties.
func scSites(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("sc-domain:site%d.example.com", i)
	}
	return out
}

// The apps, accounts, buckets and refusals the Google Play cases repeat.
const (
	playApp            = "com.example.app"
	playSecondApp      = "com.example.reader_beta"
	playAccount        = "5732191048837265001"
	playBucket         = "pubsite_prod_rev_5732191048837265001"
	playApprover       = "ines"
	playAppRefused     = "spec.googlePlay.apps[0]: Invalid value"
	playSeatAppRefused = "spec.integrations.googlePlay.apps[0]: Invalid value"
	msgPlayAppsOn      = "apps is required while the seat is on: a Google Play agent with no app can reach nothing"
)

// playIDs is n distinct values of format, which takes the index.
func playIDs(n int, format string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf(format, i)
	}
	return out
}

// The names the vendor fixtures share.
const (
	gitlabGroup  = "nimbus-platform"
	gitlabServer = "gitlab"
	gitlabToken  = "GITLAB_TOKEN"
	// The two variables the scw CLI reads a Scaleway key's values from.
	scwSecretKey = "SCW_SECRET_KEY" //nolint:gosec // G101: a variable's name, which holds no credential.
	scwAccessKey = "SCW_ACCESS_KEY"
	siteCloudID  = "4f3e2d1c-0b9a"
)

// vendorIntegration is the smallest valid Integration of one vendor, set by
// member, with provisioning Engine when engine is true.
func vendorIntegration(member func(*v1.IntegrationSpec), engine bool) func(namespace, name string) *v1.Integration {
	return func(namespace, name string) *v1.Integration {
		i := slackIntegration(namespace, name)
		i.Spec.Slack = nil
		member(&i.Spec)
		if engine {
			i.Spec.Provisioning = v1.IntegrationProvisioningEngine
		}
		return i
	}
}

func gitlabMember(s *v1.IntegrationSpec) {
	s.GitLab = v1.GitLabIntegration{
		URL:              "https://gitlab.example.com",
		SigningSecretRef: secretKey(gitlabServer, "signingSecret"),
	}
}

func gitlabEngine(s *v1.IntegrationSpec) {
	gitlabMember(s)
	s.GitLab.EngineProvisioning = v1.GitLabEngineProvisioning{
		Group:               gitlabGroup,
		AdminTokenSecretRef: secretKey("gitlab-admin", "token"),
	}
}

func mattermostMember(s *v1.IntegrationSpec) {
	s.Mattermost = v1.MattermostIntegration{URL: "https://chat.example.com", Team: companyName}
}

func jiraMember(s *v1.IntegrationSpec) {
	s.Jira = v1.JiraIntegration{URL: "https://jira.example.com", TokenSecretRef: secretKey("jira", "token")}
}

func confluenceMember(s *v1.IntegrationSpec) {
	s.Confluence = v1.ConfluenceIntegration{CloudID: siteCloudID, TokenSecretRef: secretKey("confluence", "token")}
}

func datadogMember(s *v1.IntegrationSpec) {
	s.Datadog = v1.DatadogIntegration{
		WebhookTokenSecretRef: secretKey("datadog", "webhookToken"),
		RouteTo:               v1.DatadogRouteTo{SeatRef: v1.LocalObjectReference{Name: seatHandle}},
		Site:                  v1.DatadogSiteEU1,
		APIKeySecretRef:       secretKey("datadog", "apiKey"),
		AppKeySecretRef:       secretKey("datadog", "appKey"),
	}
}

func cloudflareMember(s *v1.IntegrationSpec) {
	s.Cloudflare = v1.CloudflareIntegration{
		AccountID: "0123456789abcdef0123456789abcdef", APITokenSecretRef: secretKey("cloudflare", "apiToken"),
	}
}

func expoMember(s *v1.IntegrationSpec) {
	s.Expo = v1.ExpoIntegration{AccountName: companyName}
}

func digitaloceanMember(s *v1.IntegrationSpec) {
	s.DigitalOcean = v1.DigitalOceanIntegration{Team: "Nimbus Production"}
}

func scalewayMember(s *v1.IntegrationSpec) {
	s.Scaleway = v1.ScalewayIntegration{
		OrganizationID: scalewayOrg, AccessKey: "SCWPROVISIONERKEY001",
		SecretKeySecretRef: secretKey("scaleway", "secretKey"),
	}
}

// The Scaleway Organization and a project of it the Scaleway cases name.
const (
	scalewayOrg     = "11111111-2222-4333-8444-555555555555"
	scalewayProject = "aaaaaaaa-0000-4000-8000-000000000001"
)

func awsMember(s *v1.IntegrationSpec) {
	s.AWS = v1.AWSIntegration{
		Slug: companyName, HomeRegion: awsHomeRegion,
		CertificateAuthoritySecretRef: secretKey("aws", "certificateAuthority"),
	}
}

func googleCloudMember(s *v1.IntegrationSpec) {
	s.GoogleCloud = v1.GoogleCloudIntegration{
		IdentityProject:        "nimbus-crewlet-id",
		Provider:               googleProviderPool + "/providers/crewlet-engine",
		FederationKeySecretRef: secretKey("google-cloud", "federationKey"),
	}
}

func googleAnalyticsMember(s *v1.IntegrationSpec) {
	s.GoogleAnalytics = &v1.GoogleAnalyticsIntegration{}
}

// gaProperties is n distinct property IDs.
func gaProperties(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(100000000 + i)
	}
	return out
}

func adMobMember(s *v1.IntegrationSpec) {
	s.AdMob = v1.AdMobIntegration{OAuth: v1.GoogleOAuthClient{
		ClientID: adMobClientID, ClientSecretRef: secretKey("google-oauth", "clientSecret"),
	}}
}

// adSenseMember signs in as adMobMember's OAuth client, which the two
// products may share.
func adSenseMember(s *v1.IntegrationSpec) {
	s.AdSense = v1.AdSenseIntegration{OAuth: v1.GoogleOAuthClient{
		ClientID: adMobClientID, ClientSecretRef: secretKey("google-oauth", "clientSecret"),
	}}
}

// adMobAccounts is n distinct publisher IDs.
func adMobAccounts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "pub-" + strconv.Itoa(1000000000000000+i)
	}
	return out
}

// The App Store Connect team and app the cases repeat.
const (
	ascTeam    = "main"
	ascIssuer  = "57246542-96fe-1a63-e053-0824d011072a"
	ascApp     = "1234567890"
	ascRefused = "spec.appStoreConnect.connections[0]."
	msgASCApps = "apps is required while the seat is on: an App Store Connect agent given no app can reach nothing"
)

func appStoreConnectMember(s *v1.IntegrationSpec) {
	s.AppStoreConnect = v1.AppStoreConnectIntegration{Connections: []v1.AppStoreConnectConnection{{
		Name: ascTeam, IssuerID: ascIssuer,
	}}}
}

// The Apple Ads API user, ad account and key the cases repeat.
const (
	aaConnection  = "main"
	aaClientID    = "SEARCHADS.aeb3ef5f-0c5a-4f2a-99c8-fca83f25a9e1"
	aaKeyID       = "a273d0d3-4d9e-458c-a173-0db8619ca7d7"
	aaAccount     = "123456789"
	aaAccount2    = "123456790"
	aaNotDigits   = "acct-1"
	aaDaily       = "300.00"
	aaRefused     = "spec.appleAds.connections[0]."
	aaCapRefused  = "spec.appleAds.spend.accounts[0]."
	msgAAAccounts = "adAccounts is required while the seat is on: an Apple Ads agent given no ad account can reach nothing"
	msgAAClients  = "each connection is its own API user: a clientID is in two connections"
	msgAACapShape = "spend.accounts caps an Apple Ads ad account by its ID, its digits"
	msgAACapReach = "spend.accounts may cap only an ad account adAccounts lists"
)

// aaConnectionOf is an API user under the connection id given, whose client
// and team IDs end in suffix, its private key in a Secret of its own name.
func aaConnectionOf(name, suffix string) v1.AppleAdsConnection {
	return v1.AppleAdsConnection{
		Name: name, ClientID: "SEARCHADS." + suffix, TeamID: "SEARCHADS." + suffix, KeyID: aaKeyID,
		PrivateKeySecretRef: secretKey("apple-ads-"+name, "privateKey"),
	}
}

func appleAdsMember(s *v1.IntegrationSpec) {
	s.AppleAds = v1.AppleAdsIntegration{Connections: []v1.AppleAdsConnection{
		aaConnectionOf(aaConnection, strings.TrimPrefix(aaClientID, "SEARCHADS.")),
	}}
}

// aaCap is the caps of the ad account given, in US dollars.
func aaCap(account string) v1.GrowthSpendCap {
	return v1.GrowthSpendCap{Account: account, Currency: currencyUSD, PerChange: capPerChange, Daily: aaDaily}
}

// The Meta app, ad accounts and caps the cases repeat.
const (
	metaAppID       = "987654321012345"
	metaAccount     = "act_1112223333"
	metaAccount2    = "act_2223334444"
	metaDigits      = "1112223333"
	metaNotAccount  = "acct-1"
	metaDaily       = "200.00"
	metaCapRefused  = "spec.metaAds.spend.accounts[0]."
	msgMetaAccounts = "adAccounts is required while the seat is on: a Meta Ads agent given no ad account can reach nothing"
	msgMetaTwice    = "adAccounts names one ad account twice: its digits alone are act_ and them"
	msgMetaCapShape = "spend.accounts caps a Meta ad account by its ID as Meta writes it: act_ and its digits"
	msgMetaCapReach = "spend.accounts may cap only an ad account adAccounts lists"
)

func metaAdsMember(s *v1.IntegrationSpec) {
	s.MetaAds = v1.MetaAdsIntegration{
		AppID:              metaAppID,
		AppSecretSecretRef: secretKey("meta-ads", "appSecret"),
		ReadTokenSecretRef: secretKey("meta-ads", "readToken"),
	}
}

// metaCap is the caps of the ad account given, in euros.
func metaCap(account string) v1.GrowthSpendCap {
	return v1.GrowthSpendCap{Account: account, Currency: currencyEUR, PerChange: capPerChange, Daily: metaDaily}
}

func googleAdsMember(s *v1.IntegrationSpec) {
	s.GoogleAds = &v1.GoogleAdsIntegration{}
}

func searchConsoleMember(s *v1.IntegrationSpec) {
	s.SearchConsole = &v1.SearchConsoleIntegration{}
}

func googlePlayMember(s *v1.IntegrationSpec) {
	s.GooglePlay = &v1.GooglePlayIntegration{}
}

func atlassianMember(s *v1.IntegrationSpec) {
	s.Atlassian = v1.AtlassianIntegration{OrgID: "1a2b3c4d-5e6f", APIKeySecretRef: secretKey("atlassian", "apiKey")}
}

func TestIntegrationVendors(t *testing.T) {
	type vendor struct {
		name   string
		member func(*v1.IntegrationSpec)
		engine bool
		cases  []createCase[*v1.Integration]
	}
	for _, v := range []vendor{
		{
			name: "GitLab", member: gitlabMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with a read token and a hook name",
					mutate: func(i *v1.Integration) {
						i.Spec.GitLab.TokenSecretRef = secretKey("gitlab", "readToken")
						i.Spec.GitLab.WebhookName = "nimbus-staging"
					},
				},
				{
					name:   "an engineProvisioning without provisioning Engine is refused",
					mutate: func(i *v1.Integration) { gitlabEngine(&i.Spec) },
					want:   msgGitLabEngine,
				},
				{
					name: "provisioning Engine without engineProvisioning is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.Provisioning = v1.IntegrationProvisioningEngine
					},
					want: msgGitLabEngine,
				},
				{
					name:   "a url that is no URL is refused",
					mutate: func(i *v1.Integration) { i.Spec.GitLab.URL = "gitlab.example.com" },
					want:   msgURLScheme,
				},
				{
					name:   nameUpperCaseURL,
					mutate: func(i *v1.Integration) { i.Spec.GitLab.URL = "HTTPS://gitlab.example.com" },
					want:   msgURLScheme,
				},
				{
					name:   "a url naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.GitLab.URL = "https://${GITLAB_HOST}" },
					want:   msgURLReference,
				},
				{
					name:   "no signing secret is refused",
					mutate: func(i *v1.Integration) { i.Spec.GitLab.SigningSecretRef = v1.SecretKeyReference{} },
					want:   "spec.gitlab.signingSecretRef: Required value",
				},
				{
					name:   "a hook name naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.GitLab.WebhookName = "nimbus-${HOOK}" },
					want:   "webhookName" + msgReference,
				},
			},
		},
		{
			name: "GitLab provisioned by the engine", member: gitlabEngine, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name: "with every setting",
					mutate: func(i *v1.Integration) {
						p := &i.Spec.GitLab.EngineProvisioning
						p.AccountOwner = v1.GitLabAccountOwnerInstance
						p.UsernamePrefix = "agent-"
						p.Projects = []string{gitlabGroup + "/api"}
						p.GroupWebhook = v1.ContainerWebhookNever
					},
				},
				{
					name: "no administrator token is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GitLab.EngineProvisioning.AdminTokenSecretRef = v1.SecretKeyReference{}
					},
					want: "adminTokenSecretRef: Required value",
				},
			},
		},
		{
			name: "Mattermost", member: mattermostMember,
			cases: []createCase[*v1.Integration]{
				{
					name:   "with a typing status",
					mutate: func(i *v1.Integration) { i.Spec.Mattermost.TypingStatus = v1.WorkingStatusAddressed },
				},
				{
					name: "provisioned by the engine",
					mutate: func(i *v1.Integration) {
						i.Spec.Provisioning = v1.IntegrationProvisioningEngine
						i.Spec.Mattermost.EngineProvisioning = v1.MattermostEngineProvisioning{
							AdminTokenSecretRef: secretKey("mattermost-admin", "token"),
							UsernamePrefix:      "agent-",
							Channels:            []string{"town-square", "ops"},
							DisplayNameSuffix:   " (AI)",
						}
					},
				},
				{
					name: "an administrator token without provisioning Engine is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.Mattermost.EngineProvisioning = v1.MattermostEngineProvisioning{
							AdminTokenSecretRef: secretKey("mattermost-admin", "token"),
						}
					},
					want: msgMattermostEng,
				},
				{
					name:   "provisioning Engine without engineProvisioning is refused",
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgMattermostEng,
				},
				{
					name: "a display name suffix naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.Provisioning = v1.IntegrationProvisioningEngine
						i.Spec.Mattermost.EngineProvisioning = v1.MattermostEngineProvisioning{
							AdminTokenSecretRef: secretKey("mattermost-admin", "token"),
							DisplayNameSuffix:   " (${SUFFIX})",
						}
					},
					want: "displayNameSuffix" + msgReference,
				},
				{
					name:   "a team that is no slug is refused",
					mutate: func(i *v1.Integration) { i.Spec.Mattermost.Team = "Nimbus HQ" },
					want:   "spec.mattermost.team: Invalid value",
				},
				{
					name:   nameUpperCaseURL,
					mutate: func(i *v1.Integration) { i.Spec.Mattermost.URL = "Https://chat.example.com" },
					want:   msgURLScheme,
				},
			},
		},
		{
			name: "Jira", member: jiraMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "on Cloud by its id, with every setting",
					mutate: func(i *v1.Integration) {
						i.Spec.Jira.URL = ""
						i.Spec.Jira.CloudID = siteCloudID
						i.Spec.Jira.SiteURL = "https://nimbus.atlassian.net"
						i.Spec.Jira.Email = "admin@nimbus.example"
						i.Spec.Jira.WebhookSecretRef = secretKey("jira", "webhookSecret")
						i.Spec.Jira.WebhookName = companyName
					},
				},
				{
					name:   "a site the Atlassian organization discovers",
					mutate: func(i *v1.Integration) { i.Spec.Jira.URL = "" },
				},
				{
					name:   "a url and a cloudID are refused",
					mutate: func(i *v1.Integration) { i.Spec.Jira.CloudID = siteCloudID },
					want:   msgOneSite,
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineVendors,
				},
				{
					name:   "a siteURL that is no URL is refused",
					mutate: func(i *v1.Integration) { i.Spec.Jira.SiteURL = "nimbus.atlassian.net" },
					want:   msgSiteURL,
				},
				{
					name:   nameUpperCaseURL,
					mutate: func(i *v1.Integration) { i.Spec.Jira.URL = "HTTP://jira.example.com" },
					want:   msgURLScheme,
				},
				{
					name:   "a siteURL with an upper-case scheme is refused",
					mutate: func(i *v1.Integration) { i.Spec.Jira.SiteURL = "HTTPS://nimbus.atlassian.net" },
					want:   msgSiteURL,
				},
				{
					name:   "a siteURL naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Jira.SiteURL = "https://${JIRA_HOST}" },
					want:   "siteURL" + msgReference,
				},
				{
					name:   "a webhook name naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Jira.WebhookName = "nimbus-${HOOK}" },
					want:   "webhookName" + msgReference,
				},
			},
		},
		{
			name: "Confluence", member: confluenceMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its webhook credentials",
					mutate: func(i *v1.Integration) {
						i.Spec.Confluence.WebhookSecretRef = secretKey("confluence", "webhookSecret")
						i.Spec.Confluence.WebhookTokenSecretRef = secretKey("confluence", "webhookToken")
					},
				},
				{
					name:   "a url and a cloudID are refused",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.URL = "https://wiki.example.com" },
					want:   msgOneSite,
				},
				{
					name:   "no token is refused",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.TokenSecretRef = v1.SecretKeyReference{} },
					want:   "spec.confluence.tokenSecretRef: Required value",
				},
				{
					name:   "a siteURL is accepted",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.SiteURL = "https://nimbus.atlassian.net" },
				},
				{
					name:   "a siteURL that is no URL is refused",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.SiteURL = "nimbus.atlassian.net" },
					want:   msgSiteURL,
				},
				{
					name:   "a siteURL with an upper-case scheme is refused",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.SiteURL = "HTTPS://nimbus.atlassian.net" },
					want:   msgSiteURL,
				},
				{
					name: nameUpperCaseURL,
					mutate: func(i *v1.Integration) {
						i.Spec.Confluence.CloudID = ""
						i.Spec.Confluence.URL = "HTTPS://wiki.example.com"
					},
					want: msgURLScheme,
				},
				{
					name:   "a siteURL naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Confluence.SiteURL = "https://${WIKI_HOST}" },
					want:   "siteURL" + msgReference,
				},
			},
		},
		{
			name: "Datadog", member: datadogMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name: "dismissing alerts that name no seat, with every setting",
					mutate: func(i *v1.Integration) {
						d := &i.Spec.Datadog
						d.RouteTo = v1.DatadogRouteTo{Dismiss: new(true)}
						d.EmailDomain = "agents.nimbus.example"
						d.WebhookName = "nimbus"
						d.HandleTag = "owner"
					},
				},
				{
					name:   "a route to a seat and dismissal at once are refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.RouteTo.Dismiss = new(true) },
					want:   msgRouteTo,
				},
				{
					name:   "a route to nobody is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.RouteTo = v1.DatadogRouteTo{Dismiss: new(false)} },
					want:   msgRouteTo,
				},
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name:   "a region Datadog does not serve is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.Site = "datadoghq.com" },
					want:   `Unsupported value: "datadoghq.com"`,
				},
				{
					name:   "a tag key with a colon is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.HandleTag = "team:owner" },
					want:   "spec.datadog.handleTag: Invalid value",
				},
				{
					name:   "a webhook name with an @ is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.WebhookName = "@webhook-crewlet" },
					want:   "spec.datadog.webhookName: Invalid value",
				},
				{
					name:   "a webhook name naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.WebhookName = "crewlet-${HOOK}" },
					want:   "webhookName" + msgReference,
				},
			},
		},
		{
			name: "Cloudflare", member: cloudflareMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   "scoped to zones",
					mutate: func(i *v1.Integration) { i.Spec.Cloudflare.Zones = []string{nimbusZone, "status.nimbus.example"} },
				},
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name:   "an account ID that is no Cloudflare account ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.Cloudflare.AccountID = "0123456789ABCDEF0123456789ABCDEF" },
					want:   "spec.cloudflare.accountID: Invalid value",
				},
				{
					name:   "a zone with a scheme is refused",
					mutate: func(i *v1.Integration) { i.Spec.Cloudflare.Zones = []string{"https://nimbus.example"} },
					want:   "spec.cloudflare.zones[0]: Invalid value",
				},
				{
					name:   "a zone listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.Cloudflare.Zones = []string{nimbusZone, nimbusZone} },
					want:   "spec.cloudflare.zones[1]: Duplicate value",
				},
				{
					name:   "no provisioner token is refused",
					mutate: func(i *v1.Integration) { i.Spec.Cloudflare.APITokenSecretRef = v1.SecretKeyReference{} },
					want:   "spec.cloudflare.apiTokenSecretRef: Required value",
				},
			},
		},
		{
			name: "Expo", member: expoMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name:   "an account name with an @ is refused",
					mutate: func(i *v1.Integration) { i.Spec.Expo.AccountName = "@nimbus" },
					want:   expoAccountRefused,
				},
				{
					name:   "an account name with a slash is refused",
					mutate: func(i *v1.Integration) { i.Spec.Expo.AccountName = "accounts/nimbus" },
					want:   expoAccountRefused,
				},
				{
					name:   "an account name naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Expo.AccountName = "${EXPO_ACCOUNT}" },
					want:   expoAccountRefused,
				},
				{
					name: "a token for the atlas is accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.Expo.AtlasTokenSecretRef = secretKey("expo", "atlasToken")
					},
				},
			},
		},
		{
			name: "DigitalOcean", member: digitaloceanMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name: "a token for the atlas is accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.DigitalOcean.AtlasTokenSecretRef = secretKey("digitalocean", "atlasToken")
					},
				},
				{
					name:   "a team with a leading space is refused",
					mutate: func(i *v1.Integration) { i.Spec.DigitalOcean.Team = " Nimbus" },
					want:   doTeamRefused,
				},
				{
					name:   "a team on two lines is refused",
					mutate: func(i *v1.Integration) { i.Spec.DigitalOcean.Team = "Nimbus\nProduction" },
					want:   doTeamRefused,
				},
				{
					name:   "a team naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.DigitalOcean.Team = "${DO_TEAM}" },
					want:   doTeamRefused,
				},
			},
		},
		{
			name: "Scaleway", member: scalewayMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name:   "an Organization ID that is no UUID is refused",
					mutate: func(i *v1.Integration) { i.Spec.Scaleway.OrganizationID = companyName },
					want:   "spec.scaleway.organizationID: Invalid value",
				},
				{
					name: "an Organization ID in capitals is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.Scaleway.OrganizationID = "AAAAAAAA-2222-4333-8444-555555555555"
					},
					want: "spec.scaleway.organizationID: Invalid value",
				},
				{
					name:   "an access key that is no Scaleway access key is refused",
					mutate: func(i *v1.Integration) { i.Spec.Scaleway.AccessKey = "scwprovisionerkey001" },
					want:   "spec.scaleway.accessKey: Invalid value",
				},
				{
					name:   "the atlas application switched on is accepted",
					mutate: func(i *v1.Integration) { i.Spec.Scaleway.Atlas = new(true) },
				},
				{
					name:   "no provisioner secret key is refused",
					mutate: func(i *v1.Integration) { i.Spec.Scaleway.SecretKeySecretRef = v1.SecretKeyReference{} },
					want:   "spec.scaleway.secretKeySecretRef: Required value",
				},
			},
		},
		{
			name: "AWS", member: awsMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its Regions, IAM read and the stack's output",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.Regions = []string{awsHomeRegion, "us-east-1", "us-gov-west-1"}
						i.Spec.AWS.IAMRead = new(true)
						i.Spec.AWS.Connection = awsConnection
					},
				},
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name:   "a slug with an upper-case letter is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Slug = "NimbusHQ" },
					want:   awsSlugRefused,
				},
				{
					name:   "a slug ending in a hyphen is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Slug = "nimbus-" },
					want:   awsSlugRefused,
				},
				{
					name:   "a slug longer than 20 characters is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Slug = "nimbus-platform-prod1" },
					want:   "spec.aws.slug: Too long",
				},
				{
					name:   "a home Region that is no Region code is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.HomeRegion = "Ireland" },
					want:   "spec.aws.homeRegion: Invalid value",
				},
				{
					name:   "a Region listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Regions = []string{awsHomeRegion, awsHomeRegion} },
					want:   "spec.aws.regions[1]: Duplicate value",
				},
				{
					name:   "a Region that is an availability zone is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Regions = []string{"eu-west-1a"} },
					want:   "spec.aws.regions[0]: Invalid value",
				},
				{
					name: "with the atlas's role and the output of a stack that holds it",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.AtlasRead = new(true)
						i.Spec.AWS.Connection = awsConnection + awsAtlasOutput
					},
				},
				{
					name: "a stack output with the atlas's profile and no role is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.Connection = awsConnection + awsAtlasOutput[:strings.LastIndex(awsAtlasOutput, ",")]
					},
					want: awsConnectionRefused,
				},
				{
					name: "a stack output whose seventh ARN is not the atlas's role is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.Connection = awsConnection + strings.Replace(awsAtlasOutput, "-atlas", "-broker", 1)
					},
					want: awsConnectionRefused,
				},
				{
					name: "no certificate authority is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.CertificateAuthoritySecretRef = v1.SecretKeyReference{}
					},
					want: "spec.aws.certificateAuthoritySecretRef: Required value",
				},
				{
					name: "a stack output of four ARNs is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.Connection = awsConnection[:strings.LastIndex(awsConnection, ",")]
					},
					want: awsConnectionRefused,
				},
				{
					name: "a stack output spaced after its commas is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AWS.Connection = strings.ReplaceAll(awsConnection, ",", ", ")
					},
					want: awsConnectionRefused,
				},
				{
					name:   "a stack output naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.AWS.Connection = "${AWS_CONNECTION}" },
					want:   awsConnectionRefused,
				},
			},
		},
		{
			name: "GoogleCloud", member: googleCloudMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   "without a provider, before it exists, is accepted",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.Provider = "" },
				},
				{
					name:   "with the atlas's reader asked for",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.AtlasRead = new(true) },
				},
				{
					name:   externalRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
					want:   msgEngineRequired,
				},
				{
					name: "the provisioner's email in place of its project is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleCloud.IdentityProject = "crewlet-provisioner@nimbus-crewlet-id.iam.gserviceaccount.com"
					},
					want: "spec.googleCloud.identityProject: Too long",
				},
				{
					name:   "an email short enough to be a project ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "ops@nimbus-crewlet-id" },
					want:   googleIdentityRefused,
				},
				{
					name:   "an identity project named with capitals and spaces is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "Nimbus Crewlet ID" },
					want:   googleIdentityRefused,
				},
				{
					name:   "a domain-scoped identity project, which no service account's email names, is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "example.com:acme" },
					want:   googleIdentityRefused,
				},
				{
					name:   "an identity project shorter than six characters is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "nimbo" },
					want:   googleIdentityRefused,
				},
				{
					name:   "an identity project ending in a hyphen is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "nimbus-crewlet-" },
					want:   googleIdentityRefused,
				},
				{
					name:   "an identity project naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleCloud.IdentityProject = "${GOOGLE_PROJECT}" },
					want:   googleIdentityRefused,
				},
				{
					name: "a provider that is only a pool is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleCloud.Provider = googleProviderPool
					},
					want: googleProviderRefused,
				},
				{
					name: "a provider named by its project ID rather than its number is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleCloud.Provider = "projects/nimbus-crewlet-id/locations/global/" +
							"workloadIdentityPools/crewlet/providers/crewlet-engine"
					},
					want: googleProviderRefused,
				},
				{
					name: "no federation key is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleCloud.FederationKeySecretRef = v1.SecretKeyReference{}
					},
					want: "spec.googleCloud.federationKeySecretRef: Required value",
				},
			},
		},
		{
			name: "GoogleAnalytics", member: googleAnalyticsMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its properties and approvers",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAnalytics.Properties = []string{gaProperty, "987654321"}
						i.Spec.GoogleAnalytics.ApproverRefs = []v1.LocalObjectReference{{Name: gaApprover}, {Name: secondApprover}}
					},
				},
				{
					name: "with an admin sign-in's OAuth client",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAnalytics.OAuth = v1.GoogleOAuthClient{ClientID: adMobClientID,
							ClientSecretRef: secretKey("google-oauth", "clientSecret")}
					},
				},
				{
					name: "an admin sign-in's OAuth client with no project number is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAnalytics.OAuth = v1.GoogleOAuthClient{
							ClientID:        clientIDNoProject,
							ClientSecretRef: secretKey("google-oauth", "clientSecret")}
					},
					want: "spec.googleAnalytics.oauth.clientID: Invalid value",
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name:   "a property named by its resource name is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = []string{"properties/" + gaProperty} },
					want:   gaPropertyRefused,
				},
				{
					name:   "a Universal Analytics tracking ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = []string{"UA-12345-1"} },
					want:   gaPropertyRefused,
				},
				{
					name:   "a property ID longer than 20 digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = []string{strings.Repeat("1", 21)} },
					want:   "spec.googleAnalytics.properties[0]: Too long",
				},
				{
					name:   "a property listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = []string{gaProperty, gaProperty} },
					want:   "spec.googleAnalytics.properties[1]: Duplicate value",
				},
				{
					name:   "200 properties are accepted",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = gaProperties(200) },
				},
				{
					name:   "201 properties are refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = gaProperties(201) },
					want:   "spec.googleAnalytics.properties: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAnalytics.ApproverRefs = []v1.LocalObjectReference{{Name: gaApprover}, {Name: gaApprover}}
					},
					want: "spec.googleAnalytics.approverRefs[1]: Duplicate value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { googleCloudMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "GoogleAds", member: googleAdsMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its accounts, a manager, approvers and caps",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{
							{ID: adsCustomer, Manager: adsManager}, {ID: "1112223334"},
						}
						i.Spec.GoogleAds.ApproverRefs = []v1.LocalObjectReference{{Name: adsApprover}, {Name: secondApprover}}
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{
							adsCap(adsCustomer), {Account: "1112223334", PerChange: "0.5"},
						}
					},
				},
				{
					name: "caps on an account with no customers listed are accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{adsCap(adsCustomer)}
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name: "a customer ID written with hyphens is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: "123-456-7890"}}
					},
					want: "spec.googleAds.customers[0].id: Too long",
				},
				{
					name: "a customer ID of nine digits is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: "123456789"}}
					},
					want: "spec.googleAds.customers[0].id: Invalid value",
				},
				{
					name: "a customer ID with a letter is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: "12345678ab"}}
					},
					want: adsCustomerRefused,
				},
				{
					name: "a customer listed twice is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: adsCustomer}, {ID: adsCustomer}}
					},
					want: "spec.googleAds.customers[1]: Duplicate value",
				},
				{
					name: "an account that is its own manager is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: adsCustomer, Manager: adsCustomer}}
					},
					want: msgAdsManagerSelf,
				},
				{
					name: "a manager that is no customer ID is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: adsCustomer, Manager: "manager-01"}}
					},
					want: "spec.googleAds.customers[0].manager: Invalid value",
				},
				{
					name:   "200 customers are accepted",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAds.Customers = adsCustomers(200) },
				},
				{
					name:   "201 customers are refused",
					mutate: func(i *v1.Integration) { i.Spec.GoogleAds.Customers = adsCustomers(201) },
					want:   "spec.googleAds.customers: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.ApproverRefs = []v1.LocalObjectReference{{Name: adsApprover}, {Name: adsApprover}}
					},
					want: "spec.googleAds.approverRefs[1]: Duplicate value",
				},
				{
					name: "caps on an account customers does not list are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: adsCustomer}}
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{adsCap(adsManager)}
					},
					want: msgAdsSpendOutside,
				},
				{
					name: "caps on an account that is no customer ID are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{adsCap("act_1234567890")}
					},
					want: msgAdsSpendID,
				},
				{
					name: "an account capped twice is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{adsCap(adsCustomer), adsCap(adsCustomer)}
					},
					want: "spec.googleAds.spend.accounts[1]: Duplicate value",
				},
				{
					name: "a currency in lower case is refused",
					mutate: func(i *v1.Integration) {
						c := adsCap(adsCustomer)
						c.Currency = "eur"
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: "spec.googleAds.spend.accounts[0].currency: Invalid value",
				},
				{
					name: "an amount with a comma is refused",
					mutate: func(i *v1.Integration) {
						c := adsCap(adsCustomer)
						c.Daily = "1,000"
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: "spec.googleAds.spend.accounts[0].daily: Invalid value",
				},
				{
					name: "an amount with seven decimals is refused",
					mutate: func(i *v1.Integration) {
						c := adsCap(adsCustomer)
						c.PerChange = "0.0000001"
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: "spec.googleAds.spend.accounts[0].perChange: Invalid value",
				},
				{
					name: "a negative amount is refused",
					mutate: func(i *v1.Integration) {
						c := adsCap(adsCustomer)
						c.Daily = "-5"
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: "spec.googleAds.spend.accounts[0].daily: Invalid value",
				},
				{
					name: "a cap without an account is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{{Daily: "10"}}
					},
					want: "spec.googleAds.spend.accounts[0].account: Required value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { googleCloudMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "SearchConsole", member: searchConsoleMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with URL-prefix and domain properties and approvers",
					mutate: func(i *v1.Integration) {
						i.Spec.SearchConsole.Sites = []string{
							scPrefix, scDomain, scSecondPrefix, "http://example.org:8080/", "sc-domain:xn--bcher-kva.example",
						}
						i.Spec.SearchConsole.ApproverRefs = []v1.LocalObjectReference{{Name: scApprover}, {Name: secondApprover}}
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name:   "a URL prefix without its trailing slash is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"https://www.example.com"} },
					want:   scSiteRefused,
				},
				{
					name:   "a URL prefix with a query is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"https://www.example.com/?a=1"} },
					want:   scSiteRefused,
				},
				{
					name:   "a URL prefix with a fragment is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"https://www.example.com/#top"} },
					want:   scSiteRefused,
				},
				{
					name:   "a URL prefix with an upper-case host is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"https://WWW.example.com/"} },
					want:   scSiteRefused,
				},
				{
					name:   "a URL prefix on another scheme is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"ftp://example.com/"} },
					want:   scSiteRefused,
				},
				{
					name:   "a URL prefix with a user name is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"https://user@example.com/"} },
					want:   scSiteRefused,
				},
				{
					name:   "a domain property with a path is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"sc-domain:example.com/blog"} },
					want:   scSiteRefused,
				},
				{
					name:   "a domain property with no dot is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"sc-domain:intranet"} },
					want:   scSiteRefused,
				},
				{
					name:   "a bare host is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{"www.example.com"} },
					want:   scSiteRefused,
				},
				{
					name:   "a site listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{scPrefix, scPrefix} },
					want:   "spec.searchConsole.sites[1]: Duplicate value",
				},
				{
					name:   "1000 sites are accepted",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = scSites(1000) },
				},
				{
					name:   "1001 sites are refused",
					mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = scSites(1001) },
					want:   "spec.searchConsole.sites: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.SearchConsole.ApproverRefs = []v1.LocalObjectReference{{Name: scApprover}, {Name: scApprover}}
					},
					want: "spec.searchConsole.approverRefs[1]: Duplicate value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { googleCloudMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "GooglePlay", member: googlePlayMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its apps, developer accounts, reports buckets and approvers",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.Apps = []string{playApp, playSecondApp, "Com.Example.A1"}
						i.Spec.GooglePlay.DeveloperAccounts = []string{playAccount, "1"}
						i.Spec.GooglePlay.ReportsBuckets = []string{playBucket, "pubsite_prod_0123456789"}
						i.Spec.GooglePlay.ApproverRefs = []v1.LocalObjectReference{{Name: playApprover}, {Name: secondApprover}}
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name: "with an admin sign-in's OAuth client",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.OAuth = v1.GoogleOAuthClient{ClientID: adMobClientID,
							ClientSecretRef: secretKey("google-oauth", "clientSecret")}
					},
				},
				{
					name: "an admin sign-in's OAuth client without its secret is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.OAuth = v1.GoogleOAuthClient{ClientID: adMobClientID}
					},
					want: "spec.googlePlay.oauth.clientSecretRef: Required value",
				},
				{
					name:   "a package name of one part is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = []string{"example"} },
					want:   playAppRefused,
				},
				{
					name:   "a package name with a part that starts with a digit is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = []string{"com.1example.app"} },
					want:   playAppRefused,
				},
				{
					name:   "a package name with a hyphen is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = []string{"com.example-app.app"} },
					want:   playAppRefused,
				},
				{
					name:   "a package name with an empty part is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = []string{"com..app"} },
					want:   playAppRefused,
				},
				{
					name: "a package name of 150 characters is accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.Apps = []string{"com." + strings.Repeat("a", 146)}
					},
				},
				{
					name: "a package name of 151 characters is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.Apps = []string{"com." + strings.Repeat("a", 147)}
					},
					want: "spec.googlePlay.apps[0]: Too long",
				},
				{
					name:   "an app listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = []string{playApp, playApp} },
					want:   "spec.googlePlay.apps[1]: Duplicate value",
				},
				{
					name:   "200 apps are accepted",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = playIDs(200, "com.example.app%d") },
				},
				{
					name:   "201 apps are refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.Apps = playIDs(201, "com.example.app%d") },
					want:   "spec.googlePlay.apps: Too many",
				},
				{
					name:   "a developer account that is not digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.DeveloperAccounts = []string{"acme-studio"} },
					want:   "spec.googlePlay.developerAccounts[0]: Invalid value",
				},
				{
					name: "a developer account of 26 digits is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.DeveloperAccounts = []string{strings.Repeat("1", 26)}
					},
					want: "spec.googlePlay.developerAccounts[0]: Too long",
				},
				{
					name:   "a developer account listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.DeveloperAccounts = []string{playAccount, playAccount} },
					want:   "spec.googlePlay.developerAccounts[1]: Duplicate value",
				},
				{
					name:   "20 developer accounts are accepted",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.DeveloperAccounts = playIDs(20, "%d") },
				},
				{
					name:   "21 developer accounts are refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.DeveloperAccounts = playIDs(21, "%d") },
					want:   "spec.googlePlay.developerAccounts: Too many",
				},
				{
					name:   "a reports bucket with gs:// is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.ReportsBuckets = []string{"gs://" + playBucket} },
					want:   "spec.googlePlay.reportsBuckets[0]: Invalid value",
				},
				{
					name:   "a bucket that is no reports bucket is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.ReportsBuckets = []string{"acme-play-reports"} },
					want:   "spec.googlePlay.reportsBuckets[0]: Invalid value",
				},
				{
					name:   "a reports bucket listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.ReportsBuckets = []string{playBucket, playBucket} },
					want:   "spec.googlePlay.reportsBuckets[1]: Duplicate value",
				},
				{
					name:   "20 reports buckets are accepted",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.ReportsBuckets = playIDs(20, "pubsite_prod_rev_%d") },
				},
				{
					name:   "21 reports buckets are refused",
					mutate: func(i *v1.Integration) { i.Spec.GooglePlay.ReportsBuckets = playIDs(21, "pubsite_prod_rev_%d") },
					want:   "spec.googlePlay.reportsBuckets: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.GooglePlay.ApproverRefs = []v1.LocalObjectReference{{Name: playApprover}, {Name: playApprover}}
					},
					want: "spec.googlePlay.approverRefs[1]: Duplicate value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { searchConsoleMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "AdMob", member: adMobMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its accounts and approvers",
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.Accounts = []string{adMobAccount, adMobSecondAccount}
						i.Spec.AdMob.ApproverRefs = []v1.LocalObjectReference{{Name: adMobApprover}, {Name: secondApprover}}
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name: "a block without its OAuth client is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.OAuth = v1.GoogleOAuthClient{}
						i.Spec.AdMob.Accounts = []string{adMobAccount}
					},
					want: "spec.admob.oauth: Required value",
				},
				{
					name:   "an OAuth client without its secret is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.OAuth.ClientSecretRef = v1.SecretKeyReference{} },
					want:   "spec.admob.oauth.clientSecretRef: Required value",
				},
				{
					name:   "an OAuth client without its ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.OAuth.ClientID = "" },
					want:   "spec.admob.oauth.clientID: Required value",
				},
				{
					name: "an OAuth client ID with no project number is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.OAuth.ClientID = clientIDNoProject
					},
					want: adMobClientIDRefused,
				},
				{
					name: "an OAuth client ID in upper case is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.OAuth.ClientID = "123456789012-ABC123DEF456.apps.googleusercontent.com"
					},
					want: adMobClientIDRefused,
				},
				{
					name: "an OAuth client ID of another domain is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.OAuth.ClientID = "123456789012-abc123def456.apps.example.com"
					},
					want: adMobClientIDRefused,
				},
				{
					name:   "an ad client ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = []string{"ca-" + adMobAccount} },
					want:   "spec.admob.accounts[0]: Too long",
				},
				{
					name:   "a publisher ID of 15 digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = []string{"pub-123456789012345"} },
					want:   "should be at least 20 chars long",
				},
				{
					name:   "a publisher ID in upper case is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = []string{upperPublisher} },
					want:   adMobAccountRefused,
				},
				{
					name:   "a publisher ID with a letter among its digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = []string{"pub-123456789012345a"} },
					want:   adMobAccountRefused,
				},
				{
					name:   "an account listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = []string{adMobAccount, adMobAccount} },
					want:   "spec.admob.accounts[1]: Duplicate value",
				},
				{
					name:   "50 accounts are accepted",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = adMobAccounts(50) },
				},
				{
					name:   "51 accounts are refused",
					mutate: func(i *v1.Integration) { i.Spec.AdMob.Accounts = adMobAccounts(51) },
					want:   "spec.admob.accounts: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.AdMob.ApproverRefs = []v1.LocalObjectReference{{Name: adMobApprover}, {Name: adMobApprover}}
					},
					want: "spec.admob.approverRefs[1]: Duplicate value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { googleAnalyticsMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "AdSense", member: adSenseMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with its accounts and approvers",
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.Accounts = []string{adMobAccount, adMobSecondAccount}
						i.Spec.AdSense.ApproverRefs = []v1.LocalObjectReference{{Name: adMobApprover}, {Name: secondApprover}}
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineNowhere,
				},
				{
					name: "a block without its OAuth client is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.OAuth = v1.GoogleOAuthClient{}
						i.Spec.AdSense.Accounts = []string{adMobAccount}
					},
					want: "spec.adsense.oauth: Required value",
				},
				{
					name:   "an OAuth client without its secret is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.OAuth.ClientSecretRef = v1.SecretKeyReference{} },
					want:   "spec.adsense.oauth.clientSecretRef: Required value",
				},
				{
					name:   "an OAuth client without its ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.OAuth.ClientID = "" },
					want:   "spec.adsense.oauth.clientID: Required value",
				},
				{
					name: "an OAuth client ID with no project number is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.OAuth.ClientID = clientIDNoProject
					},
					want: adSenseClientIDRefused,
				},
				{
					name: "an OAuth client ID in upper case is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.OAuth.ClientID = "123456789012-ABC123DEF456.apps.googleusercontent.com"
					},
					want: adSenseClientIDRefused,
				},
				{
					name: "an OAuth client ID of another domain is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.OAuth.ClientID = "123456789012-abc123def456.apps.example.com"
					},
					want: adSenseClientIDRefused,
				},
				{
					name:   "an ad client ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{"ca-" + adMobAccount} },
					want:   "spec.adsense.accounts[0]: Too long",
				},
				{
					name:   "an account's resource name is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{"accounts/" + adMobAccount} },
					want:   "spec.adsense.accounts[0]: Too long",
				},
				{
					name:   "a publisher ID of 15 digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{"pub-123456789012345"} },
					want:   "should be at least 20 chars long",
				},
				{
					name:   "a publisher ID in upper case is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{upperPublisher} },
					want:   adSenseAccountRefused,
				},
				{
					name:   "a publisher ID with a letter among its digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{"pub-123456789012345a"} },
					want:   adSenseAccountRefused,
				},
				{
					name:   "an account listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = []string{adMobAccount, adMobAccount} },
					want:   "spec.adsense.accounts[1]: Duplicate value",
				},
				{
					name:   "50 accounts are accepted",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = adMobAccounts(50) },
				},
				{
					name:   "51 accounts are refused",
					mutate: func(i *v1.Integration) { i.Spec.AdSense.Accounts = adMobAccounts(51) },
					want:   "spec.adsense.accounts: Too many",
				},
				{
					name: nameApproverTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.AdSense.ApproverRefs = []v1.LocalObjectReference{{Name: adMobApprover}, {Name: adMobApprover}}
					},
					want: "spec.adsense.approverRefs[1]: Duplicate value",
				},
				{
					// One OAuth client may serve both products, each still
					// its own Integration.
					name:   "AdMob beside it is refused",
					mutate: func(i *v1.Integration) { adMobMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "AppStoreConnect", member: appStoreConnectMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with approvers and two teams is accepted",
					mutate: func(i *v1.Integration) {
						a := &i.Spec.AppStoreConnect
						a.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}}
						a.Connections = append(a.Connections, v1.AppStoreConnectConnection{
							Name: "agency-2", IssuerID: "6A1B2C3D-4E5F-4A6B-8C7D-9E0F1A2B3C4D",
						})
					},
				},
				{
					name:   nameExternalAccepted,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineCalls,
				},
				{
					name: "no team is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppStoreConnect.Connections = nil
						i.Spec.AppStoreConnect.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}}
					},
					want: "spec.appStoreConnect.connections: Required value",
				},
				{
					name: "two teams of one name are refused",
					mutate: func(i *v1.Integration) {
						a := &i.Spec.AppStoreConnect
						a.Connections = append(a.Connections, a.Connections[0])
					},
					want: "spec.appStoreConnect.connections[1]: Duplicate value",
				},
				{
					name:   "a team name with a capital letter is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppStoreConnect.Connections[0].Name = "Main" },
					want:   ascRefused + "name: Invalid value",
				},
				{
					name:   "a team name starting with a digit is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppStoreConnect.Connections[0].Name = "2nd" },
					want:   ascRefused + "name: Invalid value",
				},
				{
					name: "a team name longer than 32 characters is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppStoreConnect.Connections[0].Name = "a" + strings.Repeat("b", 32)
					},
					want: ascRefused + "name: Too long",
				},
				{
					name:   "an issuer ID that is no UUID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppStoreConnect.Connections[0].IssuerID = "57246542-96fe" },
					want:   ascRefused + "issuerID: Invalid value",
				},
				{
					name: "an issuer ID naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppStoreConnect.Connections[0].IssuerID = "${ISSUER_ID_OF_THE_TEAM_00000000000}"
					},
					want: ascRefused + "issuerID: Invalid value",
				},
				{
					name: approverListedTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.AppStoreConnect.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}, {Name: seatHandle}}
					},
					want: "spec.appStoreConnect.approverRefs[1]: Duplicate value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { jiraMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "AppleAds", member: appleAdsMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with approvers, ad accounts, two API users, an organization and caps is accepted",
					mutate: func(i *v1.Integration) {
						a := &i.Spec.AppleAds
						a.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}}
						a.AdAccounts = []string{aaAccount, aaAccount2}
						a.Connections[0].OrgID = "987654321"
						a.Connections = append(a.Connections, aaConnectionOf("agency-2", "7c1d2e3f-4a5b-4c6d-8e7f"))
						a.Spend.Accounts = []v1.GrowthSpendCap{
							aaCap(aaAccount), {Account: aaAccount2, Currency: currencyEUR, PerChange: "25", Daily: "100.5"},
						}
					},
				},
				{
					name: "caps on an ad account with no list of ad accounts, one with neither cap, are accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{aaCap(aaAccount), {Account: aaAccount2}}
					},
				},
				{
					name:   "provisioning External is accepted",
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineCalls,
				},
				{
					name: "no API user is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Connections = nil
						i.Spec.AppleAds.AdAccounts = []string{aaAccount}
					},
					want: "spec.appleAds.connections: Required value",
				},
				{
					name: "two connections of one name are refused",
					mutate: func(i *v1.Integration) {
						a := &i.Spec.AppleAds
						a.Connections = append(a.Connections, aaConnectionOf(aaConnection, "7c1d2e3f-4a5b-4c6d-8e7f"))
					},
					want: "spec.appleAds.connections[1]: Duplicate value",
				},
				{
					name: "one API user under two connections is refused",
					mutate: func(i *v1.Integration) {
						a := &i.Spec.AppleAds
						other := a.Connections[0]
						other.Name, other.PrivateKeySecretRef = "agency", secretKey("apple-ads-agency", "privateKey")
						a.Connections = append(a.Connections, other)
					},
					want: msgAAClients,
				},
				{
					name:   "a connection name with a capital letter is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].Name = "Main" },
					want:   aaRefused + "name: Invalid value",
				},
				{
					name:   "an organization ID that is not digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].OrgID = "org-987654321" },
					want:   aaRefused + "orgID: Invalid value",
				},
				{
					name:   "a client ID without Apple's SEARCHADS. prefix is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].ClientID = "aeb3ef5f-0c5a-4f2a-99c8" },
					want:   aaRefused + "clientID: Invalid value",
				},
				{
					name: "a client ID naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Connections[0].ClientID = "${APPLE_ADS_CLIENT_ID}"
					},
					want: aaRefused + "clientID: Invalid value",
				},
				{
					name: "a client ID longer than SEARCHADS. and 64 characters is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Connections[0].ClientID = "SEARCHADS." + strings.Repeat("a", 65)
					},
					want: aaRefused + "clientID: Too long",
				},
				{
					name:   "a team ID with a character outside an identifier is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].TeamID = "SEARCHADS.team_1" },
					want:   aaRefused + "teamID: Invalid value",
				},
				{
					name:   "a connection without its team ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].TeamID = "" },
					want:   aaRefused + "teamID: Required value",
				},
				{
					name:   "a key ID of two words is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].KeyID = "a273d0d3 4d9e" },
					want:   aaRefused + "keyID: Invalid value",
				},
				{
					name:   "a key ID naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.Connections[0].KeyID = "${APPLE_ADS_KEY_ID}" },
					want:   "keyID" + msgReference,
				},
				{
					name: "a key ID longer than 128 characters is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Connections[0].KeyID = strings.Repeat("a", 129)
					},
					want: aaRefused + "keyID: Too long",
				},
				{
					name: "a connection without its private key is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Connections[0].PrivateKeySecretRef = v1.SecretKeyReference{}
					},
					want: aaRefused + "privateKeySecretRef: Required value",
				},
				{
					name:   "an ad account that is not digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.AdAccounts = []string{aaNotDigits} },
					want:   "spec.appleAds.adAccounts[0]: Invalid value",
				},
				{
					name:   "an ad account listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.AppleAds.AdAccounts = []string{aaAccount, aaAccount} },
					want:   "spec.appleAds.adAccounts[1]: Duplicate value",
				},
				{
					name: approverListedTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}, {Name: seatHandle}}
					},
					want: "spec.appleAds.approverRefs[1]: Duplicate value",
				},
				{
					name: "caps on one ad account twice are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{aaCap(aaAccount), aaCap(aaAccount)}
					},
					want: "spec.appleAds.spend.accounts[1]: Duplicate value",
				},
				{
					name: "caps on an ad account named other than by its digits are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{aaCap(aaNotDigits)}
					},
					want: msgAACapShape,
				},
				{
					name: "caps on an ad account adAccounts does not list are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.AdAccounts = []string{aaAccount2}
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{aaCap(aaAccount)}
					},
					want: msgAACapReach,
				},
				{
					name: "caps naming no ad account are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{{Currency: currencyUSD, Daily: aaDaily}}
					},
					want: aaCapRefused + "account: Required value",
				},
				{
					name: "a currency that is no ISO 4217 code is refused",
					mutate: func(i *v1.Integration) {
						c := aaCap(aaAccount)
						c.Currency = "usd"
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: aaCapRefused + "currency: Invalid value",
				},
				{
					name: "a cap with more than six decimals is refused",
					mutate: func(i *v1.Integration) {
						c := aaCap(aaAccount)
						c.PerChange = "50.0000001"
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: aaCapRefused + "perChange: Invalid value",
				},
				{
					name: "a cap written with a decimal comma is refused",
					mutate: func(i *v1.Integration) {
						c := aaCap(aaAccount)
						c.Daily = "300,00"
						i.Spec.AppleAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: aaCapRefused + "daily: Invalid value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { jiraMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "MetaAds", member: metaAdsMember,
			cases: []createCase[*v1.Integration]{
				{
					name: "with approvers, every token, a business, ad accounts in both spellings and caps is accepted",
					mutate: func(i *v1.Integration) {
						m := &i.Spec.MetaAds
						m.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}}
						m.OperateTokenSecretRef = secretKey("meta-ads", "operateToken")
						m.AdminTokenSecretRef = secretKey("meta-ads", "adminToken")
						m.AdminSystemUserTokenSecretRef = secretKey("meta-ads", "adminSystemUserToken")
						m.BusinessID = "1234567890"
						m.AdAccounts = []string{metaAccount, strings.TrimPrefix(metaAccount2, "act_")}
						m.Spend.Accounts = []v1.GrowthSpendCap{
							metaCap(metaAccount), {Account: metaAccount2, Currency: currencyUSD, PerChange: "25", Daily: "100.5"},
						}
					},
				},
				{
					name: "caps on an ad account with no list of ad accounts, one with neither cap, are accepted",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{metaCap(metaAccount), {Account: metaAccount2}}
					},
				},
				{
					name:   "provisioning External is accepted",
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningExternal },
				},
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineCalls,
				},
				{
					name:   "no app ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AppID = "" },
					want:   "spec.metaAds.appID: Required value",
				},
				{
					name:   "an app ID that is not digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AppID = "app-987654321" },
					want:   "spec.metaAds.appID: Invalid value",
				},
				{
					name:   "an app ID naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AppID = "${META_APP_ID}" },
					want:   "spec.metaAds.appID: Invalid value",
				},
				{
					name:   "an app ID longer than 20 digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AppID = strings.Repeat("9", 21) },
					want:   "spec.metaAds.appID: Too long",
				},
				{
					name:   "no app secret is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AppSecretSecretRef = v1.SecretKeyReference{} },
					want:   "spec.metaAds.appSecretSecretRef: Required value",
				},
				{
					name:   "no read token is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.ReadTokenSecretRef = v1.SecretKeyReference{} },
					want:   "spec.metaAds.readTokenSecretRef: Required value",
				},
				{
					name:   "a business ID that is not digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.BusinessID = "bp-1234567890" },
					want:   "spec.metaAds.businessID: Invalid value",
				},
				{
					name: "an admin system user token with no business ID is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.AdminSystemUserTokenSecretRef = secretKey("meta-ads", "adminSystemUserToken")
						i.Spec.MetaAds.BusinessID = ""
					},
					want: "adminSystemUserTokenSecretRef needs businessID",
				},
				{
					name:   "an ad account that is no ad account ID is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AdAccounts = []string{metaNotAccount} },
					want:   "spec.metaAds.adAccounts[0]: Invalid value",
				},
				{
					name:   "an ad account of more than 20 digits is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AdAccounts = []string{strings.Repeat("1", 21)} },
					want:   "spec.metaAds.adAccounts[0]: Invalid value",
				},
				{
					name:   "an ad account listed twice is refused",
					mutate: func(i *v1.Integration) { i.Spec.MetaAds.AdAccounts = []string{metaAccount, metaAccount} },
					want:   "spec.metaAds.adAccounts[1]: Duplicate value",
				},
				{
					name: approverListedTwice,
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.ApproverRefs = []v1.LocalObjectReference{{Name: seatHandle}, {Name: seatHandle}}
					},
					want: "spec.metaAds.approverRefs[1]: Duplicate value",
				},
				{
					name: "caps on one ad account twice are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{metaCap(metaAccount), metaCap(metaAccount)}
					},
					want: "spec.metaAds.spend.accounts[1]: Duplicate value",
				},
				{
					name: "caps on an ad account named by its digits alone are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{metaCap(metaDigits)}
					},
					want: msgMetaCapShape,
				},
				{
					name: "caps on an ad account adAccounts does not list are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.AdAccounts = []string{metaAccount2}
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{metaCap(metaAccount)}
					},
					want: msgMetaCapReach,
				},
				{
					name: "caps naming no ad account are refused",
					mutate: func(i *v1.Integration) {
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{{Currency: currencyEUR, Daily: metaDaily}}
					},
					want: metaCapRefused + "account: Required value",
				},
				{
					name: "a currency that is no ISO 4217 code is refused",
					mutate: func(i *v1.Integration) {
						c := metaCap(metaAccount)
						c.Currency = "eur"
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: metaCapRefused + "currency: Invalid value",
				},
				{
					name: "a cap with more than six decimals is refused",
					mutate: func(i *v1.Integration) {
						c := metaCap(metaAccount)
						c.PerChange = "50.0000001"
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: metaCapRefused + "perChange: Invalid value",
				},
				{
					name: "a cap written with a decimal comma is refused",
					mutate: func(i *v1.Integration) {
						c := metaCap(metaAccount)
						c.Daily = "200,00"
						i.Spec.MetaAds.Spend.Accounts = []v1.GrowthSpendCap{c}
					},
					want: metaCapRefused + "daily: Invalid value",
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { jiraMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
		{
			name: "Atlassian", member: atlassianMember, engine: true,
			cases: []createCase[*v1.Integration]{
				{
					name:   "unset provisioning is refused",
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = "" },
					want:   msgEngineRequired,
				},
				{
					name:   "no organization key is refused",
					mutate: func(i *v1.Integration) { i.Spec.Atlassian.APIKeySecretRef = v1.SecretKeyReference{} },
					want:   "spec.atlassian.apiKeySecretRef: Required value",
				},
			},
		},
		{
			name: "Slack", member: func(s *v1.IntegrationSpec) { s.Slack = &v1.SlackIntegration{} },
			cases: []createCase[*v1.Integration]{
				{
					name:   engineRefused,
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineVendors,
				},
				{
					name:   secondVendorRefused,
					mutate: func(i *v1.Integration) { jiraMember(&i.Spec) },
					want:   msgOneVendor,
				},
			},
		},
	} {
		t.Run(v.name, func(t *testing.T) {
			// The first case is the base, unchanged: it must be accepted,
			// or every refusal below would pass for the wrong reason.
			runCreateCases(t, vendorIntegration(v.member, v.engine),
				append([]createCase[*v1.Integration]{{name: "the smallest one is accepted"}}, v.cases...))
		})
	}
}

func TestIntegrationVendorsAreImmutable(t *testing.T) {
	runUpdateCases(t, vendorIntegration(gitlabMember, false), []updateCase[*v1.Integration]{
		{
			name:   "the vendor's settings can change",
			mutate: func(i *v1.Integration) { i.Spec.GitLab.WebhookName = "nimbus" },
		},
		{
			name: "the provisioning mode can change",
			mutate: func(i *v1.Integration) {
				gitlabEngine(&i.Spec)
				i.Spec.Provisioning = v1.IntegrationProvisioningEngine
			},
		},
		{
			name: "GitLab cannot become App Store Connect",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				appStoreConnectMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Apple Ads",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				appleAdsMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Meta Ads",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				metaAdsMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Mattermost",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				mattermostMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Google Analytics",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				googleAnalyticsMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Google Ads",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				googleAdsMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Google Play",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				googlePlayMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become Search Console",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				searchConsoleMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become AdMob",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				adMobMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
		{
			name: "GitLab cannot become AdSense",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				adSenseMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(googleAnalyticsMember, false), []updateCase[*v1.Integration]{
		{
			name:   "its properties can change",
			mutate: func(i *v1.Integration) { i.Spec.GoogleAnalytics.Properties = []string{gaProperty} },
		},
		{
			name: "Google Analytics cannot become Google Cloud",
			mutate: func(i *v1.Integration) {
				i.Spec.GoogleAnalytics = nil
				i.Spec.Provisioning = v1.IntegrationProvisioningEngine
				googleCloudMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(googleAdsMember, false), []updateCase[*v1.Integration]{
		{
			name: "its accounts and caps can change",
			mutate: func(i *v1.Integration) {
				i.Spec.GoogleAds.Customers = []v1.GoogleAdsCustomer{{ID: adsCustomer}}
				i.Spec.GoogleAds.Spend.Accounts = []v1.GrowthSpendCap{adsCap(adsCustomer)}
			},
		},
		{
			name: "Google Ads cannot become Google Cloud",
			mutate: func(i *v1.Integration) {
				i.Spec.GoogleAds = nil
				i.Spec.Provisioning = v1.IntegrationProvisioningEngine
				googleCloudMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(searchConsoleMember, false), []updateCase[*v1.Integration]{
		{
			name:   "its sites can change",
			mutate: func(i *v1.Integration) { i.Spec.SearchConsole.Sites = []string{scDomain} },
		},
		{
			name: "Search Console cannot become Google Cloud",
			mutate: func(i *v1.Integration) {
				i.Spec.SearchConsole = nil
				i.Spec.Provisioning = v1.IntegrationProvisioningEngine
				googleCloudMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(googlePlayMember, false), []updateCase[*v1.Integration]{
		{
			name: "its apps, accounts and buckets can change",
			mutate: func(i *v1.Integration) {
				i.Spec.GooglePlay.Apps = []string{playApp}
				i.Spec.GooglePlay.DeveloperAccounts = []string{playAccount}
				i.Spec.GooglePlay.ReportsBuckets = []string{playBucket}
			},
		},
		{
			name: "Google Play cannot become Search Console",
			mutate: func(i *v1.Integration) {
				i.Spec.GooglePlay = nil
				searchConsoleMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(adMobMember, false), []updateCase[*v1.Integration]{
		{
			name: "its accounts and OAuth client can change",
			mutate: func(i *v1.Integration) {
				i.Spec.AdMob.Accounts = []string{adMobAccount}
				i.Spec.AdMob.OAuth.ClientSecretRef = secretKey("google-oauth-next", "clientSecret")
			},
		},
		{
			name: "AdMob cannot become Google Analytics",
			mutate: func(i *v1.Integration) {
				i.Spec.AdMob = v1.AdMobIntegration{}
				googleAnalyticsMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
	runUpdateCases(t, vendorIntegration(adSenseMember, false), []updateCase[*v1.Integration]{
		{
			name: "its accounts and OAuth client can change",
			mutate: func(i *v1.Integration) {
				i.Spec.AdSense.Accounts = []string{adMobAccount}
				i.Spec.AdSense.OAuth.ClientSecretRef = secretKey("google-oauth-next", "clientSecret")
			},
		},
		{
			name: "AdSense cannot become AdMob",
			mutate: func(i *v1.Integration) {
				i.Spec.AdSense = v1.AdSenseIntegration{}
				adMobMember(&i.Spec)
			},
			want: msgVendorFrozen,
		},
	})
}

func TestProvisionedCredentials(t *testing.T) {
	provisioned := func(vendor v1.ProvisionedVendor, field v1.ProvisionedField) v1.SeatCredentialSource {
		return v1.SeatCredentialSource{Provisioned: v1.ProvisionedCredential{Vendor: vendor, Field: field}}
	}
	gitlabMinted := provisioned(v1.ProvisionedGitLab, v1.ProvisionedToken)
	memberRole := []v1.MattermostRole{v1.MattermostSystemUser}
	t.Run("Seat", func(t *testing.T) {
		runCreateCases(t, seat, []createCase[*v1.Seat]{
			{name: "a seat with no credentials is accepted"},
			{
				name: "a provisioned sandbox credential is accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: gitlabToken, ValueFrom: gitlabMinted},
						{Name: "NPM_TOKEN", ValueFrom: seatFromSecret("ana-npm", "token")},
					}}
				},
			},
			{
				name: "a Mattermost bot token from a Secret is accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
						Enabled: new(true), Roles: memberRole, BotToken: fromSecret("ana-mattermost", "botToken"),
						Username: "agent-ana", Channels: []string{"deploys"},
					}
				},
			},
			{
				// Under provisioning Engine the engine mints the bot's token,
				// which no document names.
				name: "a Mattermost bot without a token is accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
						Enabled: new(true), Roles: memberRole, Channels: []string{"deploys"},
					}
				},
			},
			{
				name: "a credential both from a Secret and provisioned is refused",
				mutate: func(s *v1.Seat) {
					source := gitlabMinted
					source.SecretKeyRef = secretKey("ana-gitlab", "token")
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{{Name: gitlabToken, ValueFrom: source}}}
				},
				want: msgSeatSource,
			},
			{
				name: "every credential the engine mints is accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: gitlabToken, ValueFrom: gitlabMinted},
						{Name: envJiraToken, ValueFrom: provisioned(v1.ProvisionedAtlassian, v1.ProvisionedToken)},
						{Name: "JIRA_USERNAME", ValueFrom: provisioned(v1.ProvisionedAtlassian, v1.ProvisionedEmail)},
						{Name: "DD_APP_KEY", ValueFrom: provisioned(v1.ProvisionedDatadog, v1.ProvisionedAppKey)},
						{Name: "MATTERMOST_TOKEN", ValueFrom: provisioned(v1.ProvisionedMattermost, v1.ProvisionedBotToken)},
						{Name: "CLOUDFLARE_API_TOKEN", ValueFrom: provisioned(v1.ProvisionedCloudflare, v1.ProvisionedAPIToken)},
						{Name: "EXPO_TOKEN", ValueFrom: provisioned(v1.ProvisionedExpo, v1.ProvisionedToken)},
						{Name: "DIGITALOCEAN_ACCESS_TOKEN", ValueFrom: provisioned(v1.ProvisionedDigitalOcean, v1.ProvisionedToken)},
						{Name: scwSecretKey, ValueFrom: provisioned(v1.ProvisionedScaleway, v1.ProvisionedSecretKey)},
						{Name: scwAccessKey, ValueFrom: provisioned(v1.ProvisionedScaleway, v1.ProvisionedAccessKey)},
					}}
				},
			},
			{
				name: "a Scaleway credential other than its key's two values is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: scwSecretKey, ValueFrom: provisioned(v1.ProvisionedScaleway, v1.ProvisionedToken)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "a key's secret claimed of another vendor is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: scwSecretKey, ValueFrom: provisioned(v1.ProvisionedCloudflare, v1.ProvisionedSecretKey)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "a Cloudflare credential other than its API token is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: "CLOUDFLARE_API_TOKEN", ValueFrom: provisioned(v1.ProvisionedCloudflare, v1.ProvisionedToken)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "a DigitalOcean credential other than its token is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: "DIGITALOCEAN_ACCESS_TOKEN", ValueFrom: provisioned(v1.ProvisionedDigitalOcean, v1.ProvisionedAPIToken)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "an Expo credential other than its token is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: "EXPO_TOKEN", ValueFrom: provisioned(v1.ProvisionedExpo, v1.ProvisionedAPIToken)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "a credential the engine does not mint for the vendor is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: gitlabToken, ValueFrom: provisioned(v1.ProvisionedGitLab, v1.ProvisionedEmail)},
					}}
				},
				want: msgProvisionedField,
			},
			{
				name: "a vendor the engine provisions no account at is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: "SLACK_BOT_TOKEN", ValueFrom: provisioned("Slack", v1.ProvisionedBotToken)},
					}}
				},
				want: `provisioned.vendor: Unsupported value: "Slack"`,
			},
			{
				name: "a Mattermost username outside the server's grammar is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
						Enabled: new(true), Roles: memberRole, Username: "Ana",
					}
				},
				want: "spec.integrations.mattermost.username: Invalid value",
			},
		})
	})
	t.Run("Unit", func(t *testing.T) {
		runCreateCases(t, unit, []createCase[*v1.Unit]{
			{
				name: "a unit's credentials from Secrets are accepted",
				mutate: func(u *v1.Unit) {
					u.Spec.MCP = []v1.MCPCredentials{mcpCredentials(linearServer)}
				},
			},
			{
				name: "a unit's setting as a literal value is accepted",
				mutate: func(u *v1.Unit) {
					u.Spec.MCP = []v1.MCPCredentials{{Name: gitlabServer, Env: []v1.MCPCredentialValue{
						{Name: "GITLAB_HOST", Value: new("gitlab.example.com")},
					}}}
				},
			},
		})
	})
}

// TestSeatIntegrationAccess holds each vendor block of a Seat's
// spec.integrations to the access the vendor offers, in the vendor's own
// terms, as Crewlet's document rules hold roles[].integrations.
func TestSeatIntegrationAccess(t *testing.T) {
	on := new(true)
	runCreateCases(t, seat, []createCase[*v1.Seat]{
		{
			name: "every vendor's access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations = v1.SeatIntegrations{
					GitLab: v1.GitLabSeatIntegration{
						Enabled: on, AccessLevel: v1.GitLabReporter, Projects: []string{"nimbus-hq/infra"},
					},
					Jira:       v1.JiraSeatIntegration{Enabled: on, ProjectRole: "10101", Projects: []string{"PLAT"}},
					Confluence: v1.ConfluenceSeatIntegration{Enabled: on, SpaceRole: spaceRoleID, Spaces: []string{spaceENG, "~ana"}},
					Datadog:    v1.DatadogSeatIntegration{Enabled: on, Role: datadogRoleID},
					Mattermost: v1.MattermostSeatIntegration{
						Enabled:  on,
						Roles:    []v1.MattermostRole{v1.MattermostSystemPostAllPublic, v1.MattermostSystemUser},
						Channels: []string{"town-square", "incidents"},
						BotToken: fromSecret("ana-mattermost", "botToken"),
					},
					Cloudflare:   v1.CloudflareSeatIntegration{Enabled: on, Level: v1.CloudflareLevelOperate},
					Expo:         v1.ExpoSeatIntegration{Enabled: on, Level: v1.ExpoLevelReleaseManager},
					DigitalOcean: v1.DigitalOceanSeatIntegration{Enabled: on, Level: v1.DigitalOceanLevelAdminister},
					Scaleway: v1.ScalewaySeatIntegration{
						Enabled: on, Level: v1.ScalewayLevelAdminister, Projects: []string{scalewayProject},
					},
					AWS: v1.AWSSeatIntegration{Enabled: on, Level: v1.AWSLevelAdminister},
					GoogleCloud: v1.GoogleCloudSeatIntegration{
						Enabled: on, Level: v1.GoogleCloudLevelAdminister,
						Projects: []string{googleProject, "example.com:nimbus-legacy"},
					},
					GoogleAnalytics: v1.GoogleAnalyticsSeatIntegration{
						Enabled: on, Level: v1.GoogleAnalyticsLevelAdminister, Properties: []string{gaProperty, "987654321"},
					},
					AdMob: v1.AdMobSeatIntegration{
						Enabled: on, Level: v1.AdMobLevelAdminister, Accounts: []string{adMobAccount, adMobSecondAccount},
					},
					AdSense: v1.AdSenseSeatIntegration{
						Enabled: on, Level: v1.AdSenseLevelAdminister, Accounts: []string{adMobAccount, adMobSecondAccount},
					},
					AppStoreConnect: v1.AppStoreConnectSeatIntegration{
						Enabled: on, Level: v1.AppStoreConnectLevelAdminister, Apps: []string{ascApp, "987654321"},
						TeamResources: on,
					},
					AppleAds: v1.AppleAdsSeatIntegration{
						Enabled: on, Level: v1.AppleAdsLevelAdminister, AdAccounts: []string{aaAccount, aaAccount2},
					},
					MetaAds: v1.MetaAdsSeatIntegration{
						Enabled: on, Level: v1.MetaAdsLevelAdminister,
						AdAccounts: []string{metaAccount, strings.TrimPrefix(metaAccount2, "act_")}, BusinessResources: on,
						Identity: v1.MetaAdsIdentityOwn,
					},
					GoogleAds: v1.GoogleAdsSeatIntegration{
						Enabled: on, Level: v1.GoogleAdsLevelAdminister, Customers: []string{adsCustomer, adsManager},
					},
					SearchConsole: v1.SearchConsoleSeatIntegration{
						Enabled: on, Level: v1.SearchConsoleLevelAdminister, Sites: []string{scPrefix, scDomain},
					},
					GooglePlay: v1.GooglePlaySeatIntegration{
						Enabled: on, Level: v1.GooglePlayLevelAdminister, Apps: []string{playApp, playSecondApp},
					},
				}
			},
		},
		{
			name: "every vendor turned off with its access kept is accepted",
			mutate: func(s *v1.Seat) {
				off := new(false)
				s.Spec.Integrations = v1.SeatIntegrations{
					GitLab:     v1.GitLabSeatIntegration{Enabled: off, AccessLevel: v1.GitLabOwner},
					Jira:       v1.JiraSeatIntegration{Enabled: off},
					Confluence: v1.ConfluenceSeatIntegration{Enabled: off},
					Datadog:    v1.DatadogSeatIntegration{Enabled: off, Role: datadogRoleID},
				}
			},
		},
		{
			name: "a seat on a vendor without the access it holds there is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations = v1.SeatIntegrations{
					GitLab:     v1.GitLabSeatIntegration{Enabled: on},
					Datadog:    v1.DatadogSeatIntegration{Enabled: on},
					Mattermost: v1.MattermostSeatIntegration{Enabled: on},
				}
			},
			want: "a seat that is on GitLab needs its role",
		},
		{
			name: "a seat on Cloudflare, Expo, DigitalOcean and AWS at the vendors' least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Cloudflare = v1.CloudflareSeatIntegration{Enabled: on}
				s.Spec.Integrations.Expo = v1.ExpoSeatIntegration{Enabled: on}
				s.Spec.Integrations.DigitalOcean = v1.DigitalOceanSeatIntegration{Enabled: on}
				s.Spec.Integrations.AWS = v1.AWSSeatIntegration{Enabled: on}
			},
		},
		{
			name: "a DigitalOcean level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.DigitalOcean = v1.DigitalOceanSeatIntegration{Enabled: on, Level: engineOperate}
			},
			want: `spec.integrations.digitalocean.level: Unsupported value: "operate"`,
		},
		{
			name: "an AWS level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AWS = v1.AWSSeatIntegration{Enabled: on, Level: engineOperate}
			},
			want: `spec.integrations.aws.level: Unsupported value: "operate"`,
		},
		{
			name:   "an AWS block without enabled is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.AWS = v1.AWSSeatIntegration{Level: v1.AWSLevelRead} },
			want:   "spec.integrations.aws.enabled: Required value",
		},
		{
			name: "a Cloudflare level the engine does not offer is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Cloudflare = v1.CloudflareSeatIntegration{Enabled: on, Level: "Admin"}
			},
			want: `spec.integrations.cloudflare.level: Unsupported value: "Admin"`,
		},
		{
			name: "an Expo level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Expo = v1.ExpoSeatIntegration{Enabled: on, Level: "release_manager"}
			},
			want: `spec.integrations.expo.level: Unsupported value: "release_manager"`,
		},
		{
			name: "a seat on Scaleway in no project is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Scaleway = v1.ScalewaySeatIntegration{Enabled: on}
			},
			want: "projects is required while the seat is on",
		},
		{
			name: "a seat off Scaleway names no project",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Scaleway = v1.ScalewaySeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a Scaleway project that is no project ID is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Scaleway = v1.ScalewaySeatIntegration{Enabled: on, Projects: []string{"web"}}
			},
			want: "spec.integrations.scaleway.projects[0]: Invalid value",
		},
		{
			name: "a Scaleway project listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Scaleway = v1.ScalewaySeatIntegration{
					Enabled: on, Projects: []string{scalewayProject, scalewayProject},
				}
			},
			want: "spec.integrations.scaleway.projects[1]: Duplicate value",
		},
		{
			name: "a Scaleway level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Scaleway = v1.ScalewaySeatIntegration{
					Enabled: on, Level: "administer", Projects: []string{scalewayProject},
				}
			},
			want: `spec.integrations.scaleway.level: Unsupported value: "administer"`,
		},
		{
			name: "a seat on Google Cloud at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Projects: []string{googleProject},
				}
			},
		},
		{
			name: "a seat turned off at Google Cloud with no project is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Google Cloud in no project is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Level: v1.GoogleCloudLevelOperator,
				}
			},
			want: "projects is required while the seat is on: a Google Cloud agent in no project can reach nothing",
		},
		{
			name: "a Google Cloud level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Level: "operator", Projects: []string{googleProject},
				}
			},
			want: `spec.integrations.googleCloud.level: Unsupported value: "operator"`,
		},
		{
			name: "a Google Cloud project named by its number is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Projects: []string{"123456789012"},
				}
			},
			want: googleProjectsRefused,
		},
		{
			name: "a Google Cloud project ID with a capital letter is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Projects: []string{"Nimbus-prod"},
				}
			},
			want: googleProjectsRefused,
		},
		{
			name: "a Google Cloud project listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleCloud = v1.GoogleCloudSeatIntegration{
					Enabled: on, Projects: []string{googleProject, googleProject},
				}
			},
			want: "spec.integrations.googleCloud.projects[1]: Duplicate value",
		},
		{
			name: "a seat on Google Analytics at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Properties: []string{gaProperty},
				}
			},
		},
		{
			name: "a seat on App Store Connect at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{
					Enabled: on, Apps: []string{ascApp},
				}
			},
		},
		{
			name: "a seat turned off at Google Analytics with no property is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Google Analytics with no property is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Level: v1.GoogleAnalyticsLevelOperate,
				}
			},
			want: msgGAPropertiesWhileOn,
		},
		{
			name: "a Google Analytics level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Level: engineOperate, Properties: []string{gaProperty},
				}
			},
			want: `spec.integrations.googleAnalytics.level: Unsupported value: "operate"`,
		},
		{
			name: "a Google Analytics property named by its resource name is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Properties: []string{"properties/" + gaProperty},
				}
			},
			want: gaSeatPropertyRefused,
		},
		{
			name: "a Google Analytics property listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Properties: []string{gaProperty, gaProperty},
				}
			},
			want: "spec.integrations.googleAnalytics.properties[1]: Duplicate value",
		},
		{
			name: "more than 200 Google Analytics properties are refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{
					Enabled: on, Properties: gaProperties(201),
				}
			},
			want: "spec.integrations.googleAnalytics.properties: Too many",
		},
		{
			name: "a Google Analytics block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAnalytics = v1.GoogleAnalyticsSeatIntegration{Properties: []string{gaProperty}}
			},
			want: "spec.integrations.googleAnalytics.enabled: Required value",
		},
		{
			name: "a seat on AdMob at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{Enabled: on, Accounts: []string{adMobAccount}}
			},
		},
		{
			name: "a seat turned off at AdMob with no account is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on AdMob with no account is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{Enabled: on, Level: v1.AdMobLevelOperate}
			},
			want: msgAdMobAccountsWhileOn,
		},
		{
			name: "an AdMob level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{
					Enabled: on, Level: engineOperate, Accounts: []string{adMobAccount},
				}
			},
			want: `spec.integrations.admob.level: Unsupported value: "operate"`,
		},
		{
			name: "an AdMob publisher ID in upper case is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{
					Enabled: on, Accounts: []string{upperPublisher},
				}
			},
			want: adMobSeatAccountRefused,
		},
		{
			name: "an AdMob account listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{
					Enabled: on, Accounts: []string{adMobAccount, adMobAccount},
				}
			},
			want: "spec.integrations.admob.accounts[1]: Duplicate value",
		},
		{
			name: "more than 50 AdMob accounts are refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{Enabled: on, Accounts: adMobAccounts(51)}
			},
			want: "spec.integrations.admob.accounts: Too many",
		},
		{
			name: "an AdMob block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdMob = v1.AdMobSeatIntegration{Accounts: []string{adMobAccount}}
			},
			want: "spec.integrations.admob.enabled: Required value",
		},
		{
			name: "a seat on AdSense at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{Enabled: on, Accounts: []string{adMobAccount}}
			},
		},
		{
			name: "a seat turned off at AdSense with no account is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on AdSense with no account is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{Enabled: on, Level: v1.AdSenseLevelOperate}
			},
			want: msgAdSenseAccountsWhileOn,
		},
		{
			name: "an AdSense level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{
					Enabled: on, Level: engineOperate, Accounts: []string{adMobAccount},
				}
			},
			want: `spec.integrations.adsense.level: Unsupported value: "operate"`,
		},
		{
			name: "an AdSense publisher ID in upper case is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{
					Enabled: on, Accounts: []string{upperPublisher},
				}
			},
			want: adSenseSeatAccountRefused,
		},
		{
			name: "an AdSense account listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{
					Enabled: on, Accounts: []string{adMobAccount, adMobAccount},
				}
			},
			want: "spec.integrations.adsense.accounts[1]: Duplicate value",
		},
		{
			name: "more than 50 AdSense accounts are refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{Enabled: on, Accounts: adMobAccounts(51)}
			},
			want: "spec.integrations.adsense.accounts: Too many",
		},
		{
			name: "an AdSense block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AdSense = v1.AdSenseSeatIntegration{Accounts: []string{adMobAccount}}
			},
			want: "spec.integrations.adsense.enabled: Required value",
		},
		{
			name: "a seat turned off at App Store Connect with no app is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on App Store Connect given no app is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{
					Enabled: on, Level: v1.AppStoreConnectLevelOperate,
				}
			},
			want: msgASCApps,
		},
		{
			name: "an App Store Connect block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{Apps: []string{ascApp}}
			},
			want: "spec.integrations.appStoreConnect.enabled: Required value",
		},
		{
			name: "an App Store Connect level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{
					Enabled: on, Level: engineOperate, Apps: []string{ascApp},
				}
			},
			want: `spec.integrations.appStoreConnect.level: Unsupported value: "operate"`,
		},
		{
			name: "an App Store Connect app that is no Apple ID is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{
					Enabled: on, Apps: []string{playApp},
				}
			},
			want: "spec.integrations.appStoreConnect.apps[0]: Invalid value",
		},
		{
			name: "an App Store Connect app listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppStoreConnect = v1.AppStoreConnectSeatIntegration{
					Enabled: on, Apps: []string{ascApp, ascApp},
				}
			},
			want: "spec.integrations.appStoreConnect.apps[1]: Duplicate value",
		},
		{
			name: "a seat on Apple Ads at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{Enabled: on, AdAccounts: []string{aaAccount}}
			},
		},
		{
			name: "a seat turned off at Apple Ads with no ad account is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Apple Ads given no ad account is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{Enabled: on, Level: v1.AppleAdsLevelOperate}
			},
			want: msgAAAccounts,
		},
		{
			name: "an Apple Ads block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{AdAccounts: []string{aaAccount}}
			},
			want: "spec.integrations.appleAds.enabled: Required value",
		},
		{
			name: "an Apple Ads level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{
					Enabled: on, Level: engineOperate, AdAccounts: []string{aaAccount},
				}
			},
			want: `spec.integrations.appleAds.level: Unsupported value: "operate"`,
		},
		{
			name: "an Apple Ads ad account that is not digits is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{Enabled: on, AdAccounts: []string{aaNotDigits}}
			},
			want: "spec.integrations.appleAds.adAccounts[0]: Invalid value",
		},
		{
			name: "an Apple Ads ad account listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.AppleAds = v1.AppleAdsSeatIntegration{
					Enabled: on, AdAccounts: []string{aaAccount, aaAccount},
				}
			},
			want: "spec.integrations.appleAds.adAccounts[1]: Duplicate value",
		},
		{
			name: "a seat on Meta Ads at its least access, by an ad account's digits alone, is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{Enabled: on, AdAccounts: []string{metaDigits}}
			},
		},
		{
			name: "a seat turned off at Meta Ads with no ad account is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Meta Ads given no ad account is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{Enabled: on, Level: v1.MetaAdsLevelOperate}
			},
			want: msgMetaAccounts,
		},
		{
			name: "a Meta Ads block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{AdAccounts: []string{metaAccount}}
			},
			want: "spec.integrations.metaAds.enabled: Required value",
		},
		{
			name: "a Meta Ads level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{
					Enabled: on, Level: engineOperate, AdAccounts: []string{metaAccount},
				}
			},
			want: `spec.integrations.metaAds.level: Unsupported value: "operate"`,
		},
		{
			name: "a Meta Ads seat as the company's system user is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{
					Enabled: on, AdAccounts: []string{metaAccount}, Identity: v1.MetaAdsIdentityCompany,
				}
			},
		},
		{
			name: "a Meta Ads identity in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{
					Enabled: on, AdAccounts: []string{metaAccount}, Identity: engineOwn,
				}
			},
			want: `spec.integrations.metaAds.identity: Unsupported value: "own"`,
		},
		{
			name: "a Meta Ads ad account that is no ad account ID is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{Enabled: on, AdAccounts: []string{metaNotAccount}}
			},
			want: "spec.integrations.metaAds.adAccounts[0]: Invalid value",
		},
		{
			name: "a Meta Ads ad account listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{
					Enabled: on, AdAccounts: []string{metaAccount, metaAccount},
				}
			},
			want: "spec.integrations.metaAds.adAccounts[1]: Duplicate value",
		},
		{
			name: "a Meta Ads ad account listed in both spellings is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.MetaAds = v1.MetaAdsSeatIntegration{
					Enabled: on, AdAccounts: []string{metaDigits, metaAccount},
				}
			},
			want: msgMetaTwice,
		},
		{
			name: "a seat on Google Ads at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{Enabled: on, Customers: []string{adsCustomer}}
			},
		},
		{
			name: "a seat turned off at Google Ads with no account is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Google Ads with no account is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{Enabled: on, Level: v1.GoogleAdsLevelOperate}
			},
			want: msgAdsCustomersOn,
		},
		{
			name: "a Google Ads level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{
					Enabled: on, Level: engineOperate, Customers: []string{adsCustomer},
				}
			},
			want: `spec.integrations.googleAds.level: Unsupported value: "operate"`,
		},
		{
			name: "a Google Ads customer ID with hyphens is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{Enabled: on, Customers: []string{"123-456-78"}}
			},
			want: "spec.integrations.googleAds.customers[0]: Invalid value",
		},
		{
			name: "a Google Ads account listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{
					Enabled: on, Customers: []string{adsCustomer, adsCustomer},
				}
			},
			want: "spec.integrations.googleAds.customers[1]: Duplicate value",
		},
		{
			name: "a Google Ads block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GoogleAds = v1.GoogleAdsSeatIntegration{Customers: []string{adsCustomer}}
			},
			want: "spec.integrations.googleAds.enabled: Required value",
		},
		{
			name: "a seat on Search Console at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{Enabled: on, Sites: []string{scDomain}}
			},
		},
		{
			name: "a seat turned off at Search Console with no site is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Search Console with no site is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{
					Enabled: on, Level: v1.SearchConsoleLevelOperate,
				}
			},
			want: msgSCSitesOn,
		},
		{
			name: "a Search Console level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{
					Enabled: on, Level: engineOperate, Sites: []string{scDomain},
				}
			},
			want: `spec.integrations.searchConsole.level: Unsupported value: "operate"`,
		},
		{
			name: "a Search Console site without its trailing slash is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{
					Enabled: on, Sites: []string{"https://www.example.com"},
				}
			},
			want: scSeatRefused,
		},
		{
			name: "a Search Console site listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{
					Enabled: on, Sites: []string{scDomain, scDomain},
				}
			},
			want: "spec.integrations.searchConsole.sites[1]: Duplicate value",
		},
		{
			name: "a Search Console block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.SearchConsole = v1.SearchConsoleSeatIntegration{Sites: []string{scDomain}}
			},
			want: "spec.integrations.searchConsole.enabled: Required value",
		},
		{
			name: "a seat on Google Play at its least access is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Enabled: on, Apps: []string{playApp}}
			},
		},
		{
			name: "a seat turned off at Google Play with no app is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Enabled: new(false)}
			},
		},
		{
			name: "a seat on Google Play with no app is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Enabled: on, Level: v1.GooglePlayLevelOperate}
			},
			want: msgPlayAppsOn,
		},
		{
			name: "a Google Play level in the engine's spelling is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{
					Enabled: on, Level: engineOperate, Apps: []string{playApp},
				}
			},
			want: `spec.integrations.googlePlay.level: Unsupported value: "operate"`,
		},
		{
			name: "a Google Play app that is no package name is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Enabled: on, Apps: []string{"Example App"}}
			},
			want: playSeatAppRefused,
		},
		{
			name: "a Google Play app listed twice is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Enabled: on, Apps: []string{playApp, playApp}}
			},
			want: "spec.integrations.googlePlay.apps[1]: Duplicate value",
		},
		{
			name: "a seat on 201 Google Play apps is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{
					Enabled: on, Apps: playIDs(201, "com.example.app%d"),
				}
			},
			want: "spec.integrations.googlePlay.apps: Too many",
		},
		{
			name: "a Google Play block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GooglePlay = v1.GooglePlaySeatIntegration{Apps: []string{playApp}}
			},
			want: "spec.integrations.googlePlay.enabled: Required value",
		},
		{
			name:   "a seat on Datadog without a role is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.Datadog = v1.DatadogSeatIntegration{Enabled: on} },
			want:   "a seat that is on Datadog needs the role its service account holds",
		},
		{
			name: "a seat on Jira and Confluence with product access only is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Jira = v1.JiraSeatIntegration{Enabled: on}
				s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{Enabled: on}
			},
		},
		{
			name: "a seat on Jira in projects without a project role is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Jira = v1.JiraSeatIntegration{Enabled: on, Projects: []string{"PLAT"}}
			},
			want: "a seat that is on Jira in projects needs the project role it holds there",
		},
		{
			name: "a seat on Confluence in spaces without its access is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{Enabled: on, Spaces: []string{spaceENG}}
			},
			want: "a seat that is on Confluence in spaces needs its access there",
		},
		{
			name:   "a seat on Mattermost without roles is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{Enabled: on} },
			want:   "a seat that is on Mattermost needs the roles its bot holds",
		},
		{
			name:   "a GitLab block without enabled is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.GitLab = v1.GitLabSeatIntegration{AccessLevel: v1.GitLabGuest} },
			want:   "spec.integrations.gitlab.enabled: Required value",
		},
		{
			name: "a GitLab custom role is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitLab = v1.GitLabSeatIntegration{Enabled: on, MemberRoleID: 1007}
			},
		},
		{
			name: "a GitLab default role beside a custom one is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitLab = v1.GitLabSeatIntegration{
					Enabled: on, AccessLevel: v1.GitLabDeveloper, MemberRoleID: 1007,
				}
			},
			want: "accessLevel and memberRoleID are two ways to name the seat's role",
		},
		{
			name: "GitLab's Minimal Access, which grants an agent nothing, is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitLab = v1.GitLabSeatIntegration{Enabled: on, AccessLevel: "MinimalAccess"}
			},
			want: `spec.integrations.gitlab.accessLevel: Unsupported value: "MinimalAccess"`,
		},
		{
			name: "a GitLab project that is no path is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitLab = v1.GitLabSeatIntegration{Enabled: on, Projects: []string{"-infra"}}
			},
			want: "spec.integrations.gitlab.projects[0]: Invalid value",
		},
		{
			name: "a Datadog role naming a variable of the engine is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Datadog = v1.DatadogSeatIntegration{Enabled: on, Role: "${DATADOG_ROLE}"}
			},
			want: "spec.integrations.datadog.role: Invalid value",
		},
		{
			name: "a Jira project role that is no id is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Jira = v1.JiraSeatIntegration{Enabled: on, ProjectRole: "Developers"}
			},
			want: "spec.integrations.jira.projectRole: Invalid value",
		},
		{
			name: "a Jira project that is no key is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Jira = v1.JiraSeatIntegration{Enabled: on, Projects: []string{"plat"}}
			},
			want: "spec.integrations.jira.projects[0]: Invalid value",
		},
		{
			name: "Confluence space permissions by operation are accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{
					Enabled: on, Operations: []string{"read:space", "create:page"}, Spaces: []string{spaceENG},
				}
			},
		},
		{
			name: "a Confluence space role beside operations is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{
					Enabled: on, SpaceRole: spaceRoleID, Operations: []string{"read:space"},
				}
			},
			want: "spaceRole and operations are two ways to grant the seat's access",
		},
		{
			name: "a Confluence operation without its target is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{Enabled: on, Operations: []string{"read"}}
			},
			want: "spec.integrations.confluence.operations[0]: Invalid value",
		},
		{
			name: "Mattermost post:all without Member is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
					Enabled: on, Roles: []v1.MattermostRole{v1.MattermostSystemPostAll},
					BotToken: fromSecret("ana-mattermost", "botToken"),
				}
			},
			want: msgMattermostRoles,
		},
		{
			name: "Mattermost roles beyond one set are refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
					Enabled: on,
					Roles: []v1.MattermostRole{
						v1.MattermostSystemUser, v1.MattermostSystemPostAll, v1.MattermostSystemAdmin,
					},
					BotToken: fromSecret("ana-mattermost", "botToken"),
				}
			},
			want: msgMattermostRoles,
		},
		{
			name: "a Mattermost role the server does not define is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
					Enabled: on, Roles: []v1.MattermostRole{v1.MattermostSystemUser, "SystemManager"},
					BotToken: fromSecret("ana-mattermost", "botToken"),
				}
			},
			want: `Unsupported value: "SystemManager"`,
		},
		{
			name: "a Slack block without enabled is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.Slack = v1.SlackSeatIntegration{CredentialsSecretRef: v1.SecretReference{Name: "ana-slack"}}
			},
			want: "spec.integrations.slack.enabled: Required value",
		},
	})
}

const (
	// envJiraToken is the variable mcp-atlassian reads a Jira token from,
	// and spaceENG a Confluence space's key.
	envJiraToken = "JIRA_API_TOKEN" //nolint:gosec // G101: a variable's name, which holds no credential.
	spaceENG     = "ENG"
	// datadogRoleID is a role id as Datadog's GET /api/v2/roles returns one.
	datadogRoleID = "4f0e2b3c-1a2b-11ef-9c2b-da7ad0900002"
	// spaceRoleID is a space role id as Confluence's space roles return one.
	spaceRoleID = "b3f1c2d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	// msgMattermostRoles is the refusal of roles outside Mattermost's sets
	// for a bot.
	msgMattermostRoles = "roles is one of Mattermost's sets for a bot"
)

// TestTrackerAndKnowledgeBackends: Jira and Confluence are a company's
// tracker and knowledge base once their Integrations exist.
func TestTrackerAndKnowledgeBackends(t *testing.T) {
	runCreateCases(t, company, []createCase[*v1.Company]{
		{name: "the default backends are accepted"},
		{
			name: "Jira and Confluence are accepted",
			mutate: func(c *v1.Company) {
				c.Spec.Tracker.Backend = v1.TrackerJira
				c.Spec.Knowledge.Backend = v1.KnowledgeConfluence
			},
		},
		{
			name:   "a tracker the engine does not run is refused",
			mutate: func(c *v1.Company) { c.Spec.Tracker.Backend = "Linear" },
			want:   `Unsupported value: "Linear"`,
		},
	})
}
