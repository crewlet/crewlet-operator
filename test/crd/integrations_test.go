package crd_test

import (
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The messages of the rules over the vendors the engine can provision.
const (
	msgEngineVendors    = "provisioning Engine applies to gitlab, mattermost, datadog and atlassian"
	msgEngineRequired   = "datadog and atlassian need provisioning Engine"
	msgGitLabEngine     = "gitlab.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgMattermostEng    = "mattermost.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgOneSite          = "url and cloudID are two ways to name one site: give one"
	msgRouteTo          = "give exactly one of seatRef and dismiss: true"
	msgSeatSource       = "exactly one of secretKeyRef and provisioned must be set"
	msgProvisionedField = "the engine mints a GitLab account's Token"
	msgURLScheme        = "url must be an absolute URL starting with http:// or https://"
	msgURLReference     = "url may not hold a ${NAME} reference"
	msgSiteURL          = "siteURL must be an absolute URL starting with http:// or https://"
)

// nameUpperCaseURL names the case that offers a vendor's url with its scheme
// in upper case: Crewlet reads a vendor's scheme as written and refuses
// HTTPS://, so the API does too, rather than admit an Integration the engine
// will not apply.
const nameUpperCaseURL = "a url with an upper-case scheme is refused"

// The names the vendor fixtures share.
const (
	gitlabGroup  = "nimbus-platform"
	gitlabServer = "gitlab"
	gitlabToken  = "GITLAB_TOKEN"
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
					name:   "provisioning Engine is refused",
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
					name:   "provisioning External is refused",
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
					name:   "provisioning Engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Provisioning = v1.IntegrationProvisioningEngine },
					want:   msgEngineVendors,
				},
				{
					name:   "a second vendor beside it is refused",
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
			name: "GitLab cannot become Mattermost",
			mutate: func(i *v1.Integration) {
				i.Spec.GitLab = v1.GitLabIntegration{}
				mattermostMember(&i.Spec)
			},
			want: "an Integration's vendor cannot change",
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
						{Name: "JIRA_API_TOKEN", ValueFrom: provisioned(v1.ProvisionedAtlassian, v1.ProvisionedToken)},
						{Name: "JIRA_USERNAME", ValueFrom: provisioned(v1.ProvisionedAtlassian, v1.ProvisionedEmail)},
						{Name: "DD_APP_KEY", ValueFrom: provisioned(v1.ProvisionedDatadog, v1.ProvisionedAppKey)},
						{Name: "MATTERMOST_TOKEN", ValueFrom: provisioned(v1.ProvisionedMattermost, v1.ProvisionedBotToken)},
					}}
				},
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
					Confluence: v1.ConfluenceSeatIntegration{Enabled: on, SpaceRole: spaceRoleID, Spaces: []string{"ENG", "~ana"}},
					Datadog:    v1.DatadogSeatIntegration{Enabled: on, Role: datadogRoleID},
					Mattermost: v1.MattermostSeatIntegration{
						Enabled:  on,
						Roles:    []v1.MattermostRole{v1.MattermostSystemPostAllPublic, v1.MattermostSystemUser},
						Channels: []string{"town-square", "incidents"},
						BotToken: fromSecret("ana-mattermost", "botToken"),
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
					Jira:       v1.JiraSeatIntegration{Enabled: on},
					Confluence: v1.ConfluenceSeatIntegration{Enabled: on},
					Datadog:    v1.DatadogSeatIntegration{Enabled: on},
					Mattermost: v1.MattermostSeatIntegration{Enabled: on},
				}
			},
			want: "a seat that is on GitLab needs its role",
		},
		{
			name:   "a seat on Datadog without a role is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.Datadog = v1.DatadogSeatIntegration{Enabled: on} },
			want:   "a seat that is on Datadog needs the role its service account holds",
		},
		{
			name:   "a seat on Jira without a project role is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.Jira = v1.JiraSeatIntegration{Enabled: on} },
			want:   "a seat that is on Jira needs the project role it holds",
		},
		{
			name:   "a seat on Confluence without its access is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.Confluence = v1.ConfluenceSeatIntegration{Enabled: on} },
			want:   "a seat that is on Confluence needs its access",
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
					Enabled: on, Operations: []string{"read:space", "create:page"}, Spaces: []string{"ENG"},
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
