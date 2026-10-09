package crd_test

import (
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The messages of the rules over the vendors the engine can provision.
const (
	msgEngineVendors   = "provisioning Engine applies to gitlab, mattermost, datadog and atlassian"
	msgEngineRequired  = "datadog and atlassian need provisioning Engine"
	msgGitLabEngine    = "gitlab.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgMattermostEng   = "mattermost.engineProvisioning is required with provisioning Engine and not allowed otherwise"
	msgOneSite         = "url and cloudID are two ways to name one site: give one"
	msgRouteTo         = "give exactly one of seatRef and dismiss: true"
	msgSeatSource      = "exactly one of secretKeyRef and provisioned must be set"
	msgUnitProvisioned = "a unit's credentials come from Secrets"
	msgURLScheme       = "url must be an absolute http or https URL"
	msgURLReference    = "url may not hold a ${NAME} reference"
	msgSiteURL         = "siteURL must be an absolute http or https URL"
)

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
						p.AccessLevel = v1.GitLabDeveloper
						p.AccessLevels = []v1.GitLabSeatAccess{{Name: seatHandle, AccessLevel: v1.GitLabMaintainer}}
						p.UsernamePrefix = "agent-"
						p.Projects = []string{gitlabGroup + "/api"}
						p.GroupWebhook = v1.ContainerWebhookNever
						p.TokenScopes = []string{"api", "read_repository"}
					},
				},
				{
					name: "no administrator token is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GitLab.EngineProvisioning.AdminTokenSecretRef = v1.SecretKeyReference{}
					},
					want: "adminTokenSecretRef: Required value",
				},
				{
					name: "one seat given two access levels is refused",
					mutate: func(i *v1.Integration) {
						i.Spec.GitLab.EngineProvisioning.AccessLevels = []v1.GitLabSeatAccess{
							{Name: seatHandle, AccessLevel: v1.GitLabMaintainer}, {Name: seatHandle, AccessLevel: v1.GitLabDeveloper},
						}
					},
					want: msgDuplicate,
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
						d.Role = "Datadog Standard Role"
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
				{
					name:   "a role naming a variable of the engine is refused",
					mutate: func(i *v1.Integration) { i.Spec.Datadog.Role = "${DATADOG_ROLE}" },
					want:   "role" + msgReference,
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
	provisioned := func(name string) v1.SeatCredentialSource {
		return v1.SeatCredentialSource{Provisioned: v1.ProvisionedCredential{Name: name}}
	}
	t.Run("Seat", func(t *testing.T) {
		runCreateCases(t, seat, []createCase[*v1.Seat]{
			{name: "a seat with no credentials is accepted"},
			{
				name: "provisioned tool, sandbox and Mattermost credentials are accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.MCP = []v1.MCPCredentials{{Name: gitlabServer, Env: []v1.MCPCredentialValue{
						{Name: gitlabToken, ValueFrom: provisioned("gitlab-token")},
					}}}
					s.Spec.Sandbox = &v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: gitlabToken, ValueFrom: provisioned("gitlab-token")},
						{Name: "NPM_TOKEN", ValueFrom: seatFromSecret("ana-npm", "token")},
					}}
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
						BotToken: provisioned("mattermost"), Username: "agent-ana", Channel: "deploys",
					}
				},
			},
			{
				name: "a Mattermost bot token from a Secret is accepted",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{
						BotToken: seatFromSecret("ana-mattermost", "botToken"),
					}
				},
			},
			{
				name: "a credential both from a Secret and provisioned is refused",
				mutate: func(s *v1.Seat) {
					source := provisioned("gitlab-token")
					source.SecretKeyRef = secretKey("ana-gitlab", "token")
					s.Spec.MCP = []v1.MCPCredentials{{Name: gitlabServer, Env: []v1.MCPCredentialValue{
						{Name: gitlabToken, ValueFrom: source},
					}}}
				},
				want: msgSeatSource,
			},
			{
				name: "a Mattermost bot without a token is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{Channel: "deploys"}
				},
				want: "botToken: Required value",
			},
			{
				name: "a provisioned name that is no slug is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Sandbox = &v1.SeatSandbox{Env: []v1.SeatEnvVar{
						{Name: gitlabToken, ValueFrom: provisioned("GitLab Token")},
					}}
				},
				want: "provisioned.name: Invalid value",
			},
			{
				name: "a Mattermost username outside the server's grammar is refused",
				mutate: func(s *v1.Seat) {
					s.Spec.Integrations.Mattermost = v1.MattermostSeatIntegration{BotToken: provisioned("bot"), Username: "Ana"}
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
					u.Spec.MCP = []v1.MCPCredentials{mcpCredentials(gitlabServer)}
				},
			},
			{
				name: "a provisioned unit credential is refused",
				mutate: func(u *v1.Unit) {
					u.Spec.MCP = []v1.MCPCredentials{{Name: gitlabServer, Env: []v1.MCPCredentialValue{
						{Name: gitlabToken, ValueFrom: provisioned("gitlab-token")},
					}}}
				},
				want: msgUnitProvisioned,
			},
		})
	})
}

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
