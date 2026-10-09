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
					Name: "authorization", ValueFrom: fromSecret("platform-github", "authorization"),
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
			name:   "a human seat without contact is refused",
			mutate: func(s *v1.Seat) { s.Spec.Kind = v1.SeatKindHuman },
			want:   msgHumanContact,
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
			name:   "a repository that is not owner/name is refused",
			mutate: func(s *v1.Seat) { s.Spec.Integrations.GitHub.Repos = []string{"nimbuscore"} },
			want:   "spec.integrations.github.repos[0]",
		},
	})
}

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
