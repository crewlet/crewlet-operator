package crd_test

import (
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

func TestUnitValidation(t *testing.T) {
	rename := func(name string) func(*v1.Unit) {
		return func(u *v1.Unit) { u.Name = name }
	}
	runCreateCases(t, unit, []createCase[*v1.Unit]{
		{name: "minimal unit is accepted"},
		{
			name: "a unit with every field is accepted",
			mutate: func(u *v1.Unit) {
				u.Spec.DisplayName = "Platform Engineering"
				u.Spec.Type = v1.UnitTypeSquad
				u.Spec.ParentRef = v1.LocalObjectReference{Name: "engineering"}
				u.Spec.LeadRef = v1.LocalObjectReference{Name: seatHandle}
				u.Spec.Goals = []string{"p99 under 200ms"}
				u.Spec.Channel = "#platform"
				u.Spec.Project = containerKey
				u.Spec.Space = containerKey
				u.Spec.MCP = []v1.MCPCredentials{mcpCredentials("github"), mcpCredentials("datadog")}
			},
		},
		// The name is the Crewlet unit id, less the '_' no Kubernetes name
		// can hold: ^[a-z][a-z0-9-]{0,63}$.
		{name: "a name that starts with a digit is refused", mutate: rename("1platform"), want: msgUnitID},
		{name: "a name with a dot is refused", mutate: rename("platform.eu"), want: msgUnitID},
		{name: "a name of 65 characters is refused", mutate: rename("p" + strings.Repeat("a", 64)), want: msgUnitID},
		{name: "a name of 64 characters is accepted", mutate: rename("p" + strings.Repeat("a", 63))},
		{name: "a name with an underscore is refused", mutate: rename("platform_eu"), want: msgUnitID},
		{
			name: "credentials for one server twice are refused",
			mutate: func(u *v1.Unit) {
				u.Spec.MCP = []v1.MCPCredentials{mcpCredentials("github"), mcpCredentials("github")}
			},
			want: msgDuplicate,
		},
		{
			name:   "a project that is not a container key is refused",
			mutate: func(u *v1.Unit) { u.Spec.Project = "platform-engineering" },
			want:   "spec.project",
		},
		{
			name:   "a space that is not a container key is refused",
			mutate: func(u *v1.Unit) { u.Spec.Space = "Plat" },
			want:   msgContainerKey,
		},
		{
			name: "two credentials differing only in case are refused",
			mutate: func(u *v1.Unit) {
				c := mcpCredentials("github")
				c.Env = append(c.Env, v1.MCPCredentialValue{
					Name: "authorization", ValueFrom: seatFromSecret("platform-github", "authorization"),
				})
				u.Spec.MCP = []v1.MCPCredentials{c}
			},
			want: msgHeaderCase,
		},
		{
			name:   "an unknown unit type is refused",
			mutate: func(u *v1.Unit) { u.Spec.Type = "Tribe" },
			want:   `Unsupported value: "Tribe"`,
		},
		{
			name: "a setting as a literal value beside a credential is accepted",
			mutate: func(u *v1.Unit) {
				c := mcpCredentials("gitlab")
				c.Env = append(c.Env, v1.MCPCredentialValue{Name: "GITLAB_HOST", Value: new("gitlab.example.com")})
				u.Spec.MCP = []v1.MCPCredentials{c}
			},
		},
		{
			name: "a literal value under a credential's name is refused",
			mutate: func(u *v1.Unit) {
				u.Spec.MCP = []v1.MCPCredentials{{Name: "gitlab", Env: []v1.MCPCredentialValue{
					{Name: "GITLAB_TOKEN", Value: new("glpat-literal")},
				}}}
			},
			want: msgLiteralCredential,
		},
	})
}

// fullAgent sets every field an agent seat may have to a valid value.
func fullAgent(s *v1.Seat) {
	s.Spec.Handle = seatHandle
	s.Spec.DisplayName = "Ana (Platform Engineer)"
	s.Spec.Email = "ana@nimbus.example"
	s.Spec.UnitRef = v1.LocalObjectReference{Name: unitName}
	s.Spec.Goal = "Keep CI green."
	s.Spec.Manages = []v1.ManagedReference{{Name: "bob"}, {Kind: v1.ManagedKindUnit, Name: "bob"}}
	s.Spec.Workers = []string{"researcher"}
	s.Spec.TokenBudget = v1.TokenBudget{Day: 300_000, Week: 1_000_000}
	s.Spec.LLM = v1.SeatLLM{
		Default: append(providers(providerName),
			v1.LLMProviderReference{Kind: v1.LLMProviderKindPlatform, Name: providerName}),
		Review: providers(providerName),
	}
	s.Spec.LearningEnabled = new(false)
	s.Spec.MCP = []v1.MCPCredentials{mcpCredentials("github")}
	s.Spec.Sandbox = &v1.SeatSandbox{
		CodingAgent:   v1.CodingAgentClaudeCode,
		MaxTurns:      new(int32(0)),
		MCPServerRefs: []v1.LocalObjectReference{{Name: "github"}},
	}
	s.Spec.Placement = v1.SeatPlacement{Pool: poolName}
	s.Spec.Paused = new(true)
	s.Spec.PauseReason = "Deploy freeze until the incident review"
	s.Spec.StopRunning = new(true)
	s.Spec.Integrations = v1.SeatIntegrations{
		Slack: v1.SlackSeatIntegration{CredentialsSecretRef: v1.SecretReference{Name: "ana-slack"}},
		GitHub: v1.GitHubSeatIntegration{
			Tier:  v1.GitHubAccessReview,
			Repos: []string{"nimbus-hq/nimbuscore"},
			App: v1.GitHubApp{
				ID:                   1234567,
				Slug:                 "nimbus-ana",
				InstallationID:       89012345,
				CredentialsSecretRef: v1.SecretReference{Name: "ana-github-app"},
			},
		},
	}
}

// contactOnly makes c the seat's whole contact.
func contactOnly(c *v1.SeatContact) func(*v1.Seat) {
	return func(s *v1.Seat) { s.Spec.Contact = *c }
}

func TestSeatValidation(t *testing.T) {
	rename := func(name string) func(*v1.Seat) {
		return func(s *v1.Seat) { s.Name = name }
	}
	// asHuman makes the seat a valid human seat, then applies change.
	asHuman := func(change func(*v1.Seat)) func(*v1.Seat) {
		return func(s *v1.Seat) {
			s.Spec.Kind = v1.SeatKindHuman
			s.Spec.Contact = v1.SeatContact{GitHubLogin: "jane"}
			change(s)
		}
	}
	nothing := func(*v1.Seat) {}

	runCreateCases(t, seat, []createCase[*v1.Seat]{
		{name: "minimal agent seat is accepted"},
		{name: "an agent seat with every field is accepted", mutate: fullAgent},

		// Without spec.handle the name is the handle.
		{name: "a name with a dot and no handle is refused", mutate: rename("ana.b"), want: msgSeatName},
		{
			name:   "a name of 64 characters and no handle is refused",
			mutate: rename("a" + strings.Repeat("n", 63)),
			want:   msgSeatName,
		},
		{
			name: "a name that is no handle is accepted with a handle",
			mutate: func(s *v1.Seat) {
				s.Name = "ana.b"
				s.Spec.Handle = "ana-b"
			},
		},
		{
			name:   "a handle outside the grammar is refused",
			mutate: func(s *v1.Seat) { s.Spec.Handle = "-ana" },
			want:   "spec.handle",
		},

		// Kinds.
		{name: "a human seat with contact is accepted", mutate: asHuman(nothing)},
		{
			// A person who works only through the dashboard has no chat
			// account to name; Crewlet advises rather than refuses.
			name:   "a human seat without contact is accepted",
			mutate: func(s *v1.Seat) { s.Spec.Kind = v1.SeatKindHuman },
		},
		{
			name:   "contact on an agent seat is refused",
			mutate: func(s *v1.Seat) { s.Spec.Contact = v1.SeatContact{SlackUserID: "U0ANA"} },
			want:   msgAgentContact,
		},
		{
			name:   "availability on an agent seat is refused",
			mutate: func(s *v1.Seat) { s.Spec.Availability = "CET business hours" },
			want:   msgAgentContact,
		},

		// Contact accounts, each in its platform's own form.
		{
			name: "a human seat with every contact account is accepted",
			mutate: asHuman(func(s *v1.Seat) {
				s.Spec.Contact = v1.SeatContact{
					SlackUserID:        "U0FOUNDER",
					MattermostUsername: "jane.doe_2-x",
					AtlassianAccount:   "557058:f58131cb-b67d-43c7-b30d-6b58d40bd077",
					GitHubLogin:        "Jane-Founder",
					GitLabUsername:     "Jane.Doe_2-x",
				}
			}),
		},
		{
			name:   "a human seat reached on Mattermost alone is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "jane"})),
		},
		{
			name:   "a Slack member id in lowercase is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{SlackUserID: "u0founder"})),
			want:   msgSlackUserID,
		},
		{
			name:   "a GitHub login with an underscore is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitHubLogin: "jane_founder"})),
			want:   msgGitHubLogin,
		},
		{
			name:   "a GitHub login of 40 characters is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitHubLogin: strings.Repeat("j", 40)})),
			want:   "spec.contact.githubLogin: Too long",
		},
		{
			// Mattermost lowercases every username, and Crewlet compares
			// this one as written: a capital would match nobody's posts.
			name:   "a Mattermost username with a capital is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "Jane"})),
			want:   msgMattermostUsername,
		},
		{
			name:   "a Mattermost username written as a mention is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "@jane"})),
			want:   msgMattermostUsername,
		},
		{
			name:   "a Mattermost username of 64 characters is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: strings.Repeat("j", 64)})),
		},
		{
			name:   "a Mattermost username of 65 characters is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: strings.Repeat("j", 65)})),
			want:   "spec.contact.mattermostUsername: Too long",
		},
		{
			name:   "a Mattermost username the platform reserves is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "channel"})),
			want:   msgMattermostReserved,
		},
		{
			// The server takes the name, but a mention of it is @here,
			// which notifies everybody in the channel who is online.
			name:   "a Mattermost username whose mention notifies the channel is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "here"})),
			want:   msgMattermostReserved,
		},
		{
			name:   "a Mattermost username that only contains a reserved one is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{MattermostUsername: "channel-ops"})),
		},
		{
			name:   "an Atlassian Cloud account id of 24 hexadecimal digits is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "5b10ac8d82e05b22cc7d4ef5"})),
		},
		{
			// A Data Center username: the render, not the API, knows
			// whether the company's Jira and Confluence are on Cloud.
			name:   "an Atlassian Data Center username with a dot is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "jane.doe"})),
		},
		{
			name:   "an Atlassian Data Center username that is an address is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "jane.doe@acme.example"})),
		},
		{
			name:   "an Atlassian account with a space is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "jane doe"})),
			want:   msgAtlassianAccount,
		},
		{
			name:   "an Atlassian account written as a reference is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "${JANE_ATLASSIAN}"})),
			want:   msgAtlassianReference,
		},
		{
			name:   "an Atlassian account holding a reference is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "jane-${SUFFIX}"})),
			want:   msgAtlassianReference,
		},
		{
			name:   "an Atlassian account with a dollar sign alone is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: "svc$jira"})),
		},
		{
			name:   "an Atlassian account of 255 characters is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: strings.Repeat("a", 255)})),
		},
		{
			name:   "an Atlassian account of 256 characters is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{AtlassianAccount: strings.Repeat("a", 256)})),
			want:   "spec.contact.atlassianAccount: Too long",
		},
		{
			name:   "a GitLab username of one character is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: "j"})),
		},
		{
			name:   "a GitLab username starting with a dot is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: ".jane"})),
		},
		{
			name:   "a GitLab username starting with a hyphen is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: "-jane"})),
			want:   msgGitLabUsername,
		},
		{
			name:   "a GitLab username ending with a dot is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: "jane."})),
			want:   msgGitLabUsername,
		},
		{
			name:   "a GitLab username written as a mention is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: "@jane"})),
			want:   msgGitLabUsername,
		},
		{
			name:   "a GitLab username of 255 characters is accepted",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: strings.Repeat("j", 255)})),
		},
		{
			name:   "a GitLab username of 256 characters is refused",
			mutate: asHuman(contactOnly(&v1.SeatContact{GitLabUsername: strings.Repeat("j", 256)})),
			want:   "spec.contact.gitlabUsername: Too long",
		},
		{
			name:   "llm on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.LLM = v1.SeatLLM{Default: providers(providerName)} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "a token budget on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.TokenBudget = v1.TokenBudget{Day: 1} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "workers on a human seat are refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Workers = []string{"researcher"} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "learningEnabled on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.LearningEnabled = new(true) }),
			want:   msgHumanForbidden,
		},
		{
			name:   "mcp on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.MCP = []v1.MCPCredentials{mcpCredentials("github")} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "a sandbox on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Sandbox = &v1.SeatSandbox{} }),
			want:   msgHumanForbidden,
		},
		{
			name: "integrations on a human seat are refused",
			mutate: asHuman(func(s *v1.Seat) {
				s.Spec.Integrations.Slack.CredentialsSecretRef = v1.SecretReference{Name: "jane-slack"}
			}),
			want: msgHumanForbidden,
		},
		{
			name:   "behavioral guidelines on a human seat are refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.BehavioralGuidelines = []string{"Be kind."} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "a project on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Project = "LEAD" }),
			want:   msgHumanForbidden,
		},
		{
			name:   "a space on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Space = "LEAD" }),
			want:   msgHumanForbidden,
		},
		{
			name:   "placement on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Placement = v1.SeatPlacement{Pool: v1.DataPool} }),
			want:   msgHumanForbidden,
		},
		{
			name:   "paused on a human seat is refused",
			mutate: asHuman(func(s *v1.Seat) { s.Spec.Paused = new(false) }),
			want:   msgHumanForbidden,
		},

		// Pauses.
		{name: "a seat asked to run is accepted", mutate: func(s *v1.Seat) { s.Spec.Paused = new(false) }},
		{
			name:   "a pause reason without a pause is refused",
			mutate: func(s *v1.Seat) { s.Spec.PauseReason = "Deploy freeze" },
			want:   msgPauseDetails,
		},
		{
			name: "a pause reason on a seat asked to run is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Paused = new(false)
				s.Spec.PauseReason = "Deploy freeze"
			},
			want: msgPauseDetails,
		},
		{
			name:   "stopRunning without a pause is refused",
			mutate: func(s *v1.Seat) { s.Spec.StopRunning = new(false) },
			want:   msgPauseDetails,
		},
		{
			name: "a pause reason of the engine's bound in characters is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Paused = new(true)
				s.Spec.PauseReason = strings.Repeat("é", 500)
			},
		},
		{
			name: "a pause reason past the engine's bound is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Paused = new(true)
				s.Spec.PauseReason = strings.Repeat("é", 501)
			},
			want: "spec.pauseReason: Too long",
		},

		// Placement, models, credentials.
		{
			// Whether the Company declares the pool is the render's to
			// say; the API server sees one object at a time.
			name:   "the data pool is accepted",
			mutate: func(s *v1.Seat) { s.Spec.Placement = v1.SeatPlacement{Pool: v1.DataPool} },
		},
		{
			name:   "a pool that is not a DNS label is refused",
			mutate: func(s *v1.Seat) { s.Spec.Placement = v1.SeatPlacement{Pool: "General_Pool"} },
			want:   msgPoolName,
		},
		{
			name:   "a pool name of 21 characters is refused",
			mutate: func(s *v1.Seat) { s.Spec.Placement = v1.SeatPlacement{Pool: strings.Repeat("p", 21)} },
			want:   "may not be more than 20 bytes",
		},
		{
			name:   "a provider twice in one chain is refused",
			mutate: func(s *v1.Seat) { s.Spec.LLM = v1.SeatLLM{Judge: providers("haiku", "haiku")} },
			want:   "a provider may appear only once in a chain",
		},
		{
			name: "credentials for one server twice are refused",
			mutate: func(s *v1.Seat) {
				s.Spec.MCP = []v1.MCPCredentials{mcpCredentials("github"), mcpCredentials("github")}
			},
			want: msgDuplicate,
		},
		{
			name:   "a worker that is no template name is refused",
			mutate: func(s *v1.Seat) { s.Spec.Workers = []string{"Not A Worker Key!"} },
			want:   "spec.workers[0]",
		},
		{
			name:   "a project that is not a container key is refused",
			mutate: func(s *v1.Seat) { s.Spec.Project = "lead" },
			want:   msgContainerKey,
		},
		{
			name:   "a space of 11 characters is refused",
			mutate: func(s *v1.Seat) { s.Spec.Space = "LEADERSHIP1" },
			want:   "spec.space",
		},
		{
			name: "an MCP credential that is no header or variable name is refused",
			mutate: func(s *v1.Seat) {
				c := mcpCredentials("github")
				c.Env[0].Name = "Bad Header"
				s.Spec.MCP = []v1.MCPCredentials{c}
			},
			want: "spec.mcp[0].env[0].name",
		},
		{
			name: "a seat's own setting as a literal value beside its credential is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.MCP = []v1.MCPCredentials{{Name: slackServer, Env: []v1.MCPCredentialValue{
					{Name: envSlackBot, ValueFrom: seatFromSecret("ana-slack", v1.SlackBotTokenKey)},
					slackChannelsCache(seatHandle),
				}}}
			},
		},
		{
			name: "a literal value under a credential's name is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.MCP = []v1.MCPCredentials{{Name: slackServer, Env: []v1.MCPCredentialValue{
					{Name: envSlackBot, Value: new("xoxb-literal")},
				}}}
			},
			want: msgLiteralCredential,
		},
		{
			name: "a literal Authorization header is refused",
			mutate: func(s *v1.Seat) {
				c := mcpCredentials("github")
				c.Env[0] = v1.MCPCredentialValue{Name: "Authorization", Value: new("Bearer literal")}
				s.Spec.MCP = []v1.MCPCredentials{c}
			},
			want: msgLiteralCredential,
		},
		{
			name: "a value and a provisioned valueFrom together are refused",
			mutate: func(s *v1.Seat) {
				cache := slackChannelsCache(seatHandle)
				cache.ValueFrom = v1.SeatCredentialSource{Provisioned: v1.ProvisionedCredential{Name: "slack-cache"}}
				s.Spec.MCP = []v1.MCPCredentials{{Name: slackServer, Env: []v1.MCPCredentialValue{cache}}}
			},
			want: msgEnvExactlyOne,
		},
		{
			name:   "a repository that is not owner/name is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.GitHub.Repos = []string{"nimbuscore"} },
			want:   "spec.integrations.github.repos[0]",
		},
		{
			name: "workflows on a FullAccess seat is accepted",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitHub.Tier = v1.GitHubAccessFullAccess
				s.Spec.Integrations.GitHub.Workflows = new(true)
			},
		},
		{
			name:   "workflows set false on a Review seat is accepted",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.GitHub.Workflows = new(false) },
		},
		{
			name:   "workflows on a Review seat is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.GitHub.Workflows = new(true) },
			want:   msgWorkflowsFullAccess,
		},
		{
			name: "workflows on a seat with no tier is refused",
			mutate: func(s *v1.Seat) {
				s.Spec.Integrations.GitHub.Tier = ""
				s.Spec.Integrations.GitHub.Workflows = new(true)
			},
			want: msgWorkflowsFullAccess,
		},
	})
}

// msgWorkflowsFullAccess is the refusal of workflows on a seat below
// FullAccess, which Crewlet refuses too.
const msgWorkflowsFullAccess = "workflows applies to a FullAccess seat only"

func TestSeatHandleIsImmutable(t *testing.T) {
	withHandle := func(s *v1.Seat) { s.Spec.Handle = seatHandle }
	runUpdateCases(t, seat, []updateCase[*v1.Seat]{
		{
			name: "the seat's other fields can change",
			mutate: func(s *v1.Seat) {
				s.Spec.Goal = "A new goal."
				s.Spec.UnitRef = v1.LocalObjectReference{Name: unitName}
			},
		},
		{
			name:   "a handle equal to the name can be written down",
			mutate: func(s *v1.Seat) { s.Spec.Handle = s.Name },
		},
		{
			name:   "a handle other than the name cannot be added",
			mutate: func(s *v1.Seat) { s.Spec.Handle = "someone-else" },
			want:   msgHandleImmutable,
		},
		{
			name:   "a handle cannot change",
			before: withHandle,
			mutate: func(s *v1.Seat) { s.Spec.Handle = "ana2" },
			want:   msgHandleImmutable,
		},
		{
			name:   "a handle other than the name cannot be removed",
			before: withHandle,
			mutate: func(s *v1.Seat) { s.Spec.Handle = "" },
			want:   msgHandleImmutable,
		},
	})
}
