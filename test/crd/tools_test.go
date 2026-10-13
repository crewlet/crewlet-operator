package crd_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// httpServer turns an MCPServer into an HTTP one.
func httpServer(s *v1.MCPServer) {
	s.Spec.Transport = v1.MCPTransportHTTP
	s.Spec.Command = ""
	s.Spec.URL = githubServer
}

func TestMCPServerDefaults(t *testing.T) {
	s := mcpServer(newNamespace(t), "tavily")
	if err := k8sClient.Create(ctx, s); err != nil {
		t.Fatal(err)
	}
	got := &v1.MCPServer{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(s), got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Transport != v1.MCPTransportStdio {
		t.Errorf("transport defaulted to %q, want Stdio", got.Spec.Transport)
	}
	if got.Spec.Shared == nil || !*got.Spec.Shared {
		t.Errorf("shared defaulted to %v, want true", got.Spec.Shared)
	}
}

func TestMCPServerValidation(t *testing.T) {
	tavilyKey := fromSecret("tavily", apiKeyKey)
	secretEnv := []v1.SecretEnvVar{{Name: "TAVILY_API_KEY", ValueFrom: tavilyKey}}
	secretHeaders := []v1.SecretHeader{{Name: "X-Api-Key", ValueFrom: tavilyKey}}
	asHTTP := func(change func(*v1.MCPServer)) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			httpServer(s)
			change(s)
		}
	}

	runCreateCases(t, mcpServer, []createCase[*v1.MCPServer]{
		{name: "minimal Stdio server is accepted"},
		{
			name: "a Stdio server with every field is accepted",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.Args = []string{"mcp-server-mattermost==0.6.1"}
				s.Spec.Env = secretEnv
				s.Spec.ToolPrefix = "mattermost_"
				s.Spec.ToolAnnotations = []v1.MCPToolAnnotations{
					{Tool: "post_message", ReadOnly: new(false), OpenWorld: new(true)},
				}
				s.Spec.StartupTimeoutSeconds = 120
				s.Spec.RequestTimeoutSeconds = 60
			},
		},
		{
			name:   "an HTTP server with headers is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.Headers = secretHeaders }),
		},
		{
			name:   "a Stdio server without command is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Command = "" },
			want:   msgStdioCommand,
		},
		{
			name:   "an HTTP server with a command is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.Command = "uvx" }),
			want:   msgStdioCommand,
		},
		{
			name:   "args on an HTTP server are refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.Args = []string{"--verbose"} }),
			want:   msgStdioOnly,
		},
		{
			name:   "env on an HTTP server is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.Env = secretEnv }),
			want:   msgStdioOnly,
		},
		{
			name:   "an HTTP server without url is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "" }),
			want:   msgHTTPURL,
		},
		{
			name:   "a url on a Stdio server is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.URL = "https://example.com/mcp" },
			want:   msgHTTPURL,
		},
		{
			name:   "headers on a Stdio server are refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Headers = secretHeaders },
			want:   "headers apply to transport HTTP only",
		},
		{
			name:   "a url that is not a URL is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "api.githubcopilot.com/mcp" }),
			want:   "url must be an absolute http or https URL",
		},
		{
			name: "two headers differing only in case are refused",
			mutate: asHTTP(func(s *v1.MCPServer) {
				s.Spec.Headers = []v1.SecretHeader{secretHeaders[0], {Name: "x-api-key", ValueFrom: tavilyKey}}
			}),
			want: msgHeaderCase,
		},
		{
			name:   "the name crewlet is refused",
			mutate: func(s *v1.MCPServer) { s.Name = "crewlet" },
			want:   "the name crewlet is reserved",
		},
	})
}

func TestIntegrationValidation(t *testing.T) {
	// asGitHub replaces Slack with GitHub, then applies change.
	asGitHub := func(change func(*v1.GitHubIntegration)) func(*v1.Integration) {
		return func(i *v1.Integration) {
			gh := githubMember()
			change(&gh)
			i.Spec.Slack = nil
			i.Spec.GitHub = gh
		}
	}

	runCreateCases(t, slackIntegration, []createCase[*v1.Integration]{
		{name: "Slack with defaults is accepted"},
		{
			name: "Slack with settings is accepted",
			mutate: func(i *v1.Integration) {
				i.Spec.Slack = &v1.SlackIntegration{
					TypingStatus:  v1.WorkingStatusAddressed,
					StatusPhrases: v1.StatusPhrases{Execute: []string{"is on it..."}},
				}
			},
		},
		{
			name: "GitHub is accepted",
			mutate: asGitHub(func(gh *v1.GitHubIntegration) {
				gh.URL = "https://github.example.com"
				gh.TokenSecretRef = secretKey(githubKeys, "orgToken")
				gh.Provisioning = v1.GitHubProvisioning{
					Org:        "nimbus-hq",
					Repos:      []string{"nimbus-hq/nimbuscore"},
					OrgWebhook: v1.ContainerWebhookAuto,
				}
			}),
		},
		{
			name:   "no vendor is refused",
			mutate: func(i *v1.Integration) { i.Spec.Slack = nil },
			want:   msgOneVendor,
		},
		{
			name:   "two vendors are refused",
			mutate: func(i *v1.Integration) { i.Spec.GitHub = githubMember() },
			want:   msgOneVendor,
		},
		{
			name: "GitHub provisioning without an organization token is refused",
			mutate: asGitHub(func(gh *v1.GitHubIntegration) {
				gh.Provisioning = v1.GitHubProvisioning{Org: "nimbus-hq"}
			}),
			want: "provisioning needs tokenSecretRef",
		},
		{
			name:   "an http GitHub url is refused",
			mutate: asGitHub(func(gh *v1.GitHubIntegration) { gh.URL = "http://github.example.com" }),
			want:   "url must be an absolute https URL",
		},
		{
			name:   "an org webhook mode outside Auto, Require and Never is refused",
			mutate: asGitHub(func(gh *v1.GitHubIntegration) { gh.Provisioning.OrgWebhook = "true" }),
			want:   `Unsupported value: "true"`,
		},
	})
}

func TestIntegrationVendorIsImmutable(t *testing.T) {
	runUpdateCases(t, slackIntegration, []updateCase[*v1.Integration]{
		{
			name:   "the vendor's settings can change",
			mutate: func(i *v1.Integration) { i.Spec.Slack.TypingStatus = v1.WorkingStatusAlways },
		},
		{
			name: "the vendor cannot change",
			mutate: func(i *v1.Integration) {
				i.Spec.Slack = nil
				i.Spec.GitHub = githubMember()
			},
			want: "an Integration's vendor cannot change",
		},
	})
}

// TestCompanyRefIsImmutable runs the one rule every child kind shares: an
// object cannot move to another company, because the operator would have to
// remove it from one document and add it to another as a single change.
func TestCompanyRefIsImmutable(t *testing.T) {
	const caseName = "companyRef cannot change"
	moveTo := func(ref *v1.CompanyReference) { ref.Name = "other" }

	t.Run("Unit", func(t *testing.T) {
		runUpdateCases(t, unit, []updateCase[*v1.Unit]{{
			name: caseName, mutate: func(u *v1.Unit) { moveTo(&u.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
	t.Run("Seat", func(t *testing.T) {
		runUpdateCases(t, seat, []updateCase[*v1.Seat]{{
			name: caseName, mutate: func(s *v1.Seat) { moveTo(&s.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
	t.Run("LLMProvider", func(t *testing.T) {
		runUpdateCases(t, llmProvider, []updateCase[*v1.LLMProvider]{{
			name: caseName, mutate: func(p *v1.LLMProvider) { moveTo(&p.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
	t.Run("MCPServer", func(t *testing.T) {
		runUpdateCases(t, mcpServer, []updateCase[*v1.MCPServer]{{
			name: caseName, mutate: func(s *v1.MCPServer) { moveTo(&s.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
	t.Run("Integration", func(t *testing.T) {
		runUpdateCases(t, slackIntegration, []updateCase[*v1.Integration]{{
			name: caseName, mutate: func(i *v1.Integration) { moveTo(&i.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
	t.Run("Schedule", func(t *testing.T) {
		runUpdateCases(t, unitSchedule, []updateCase[*v1.Schedule]{{
			name: caseName, mutate: func(s *v1.Schedule) { moveTo(&s.Spec.CompanyRef) }, want: msgCompanyRefFrozen,
		}})
	})
}
