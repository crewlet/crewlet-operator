package crd_test

import (
	"regexp"
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

// envName is the grammar of an environment variable's name, which an
// MCPServer's env takes.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// literal is a setting of name written out as a literal value.
func literal(name string) v1.MCPEnvVar { return v1.MCPEnvVar{Name: name, Value: new("literal")} }

// holdValue offers entry in every position of the tool-server API that takes
// a value, by a dry run, and checks the API server's answer is want in each:
// a Seat's and a Unit's spec.mcp, an MCPServer's headers and, where the name
// is a variable's, an MCPServer's env. Each CRD carries its own generated
// copy of a rule its types share, so a rule is held in every position rather
// than in the one a kind's table happens to name. entry is an MCPServer's
// variable, whose valueFrom is a Secret key alone: a provisioned value is a
// Seat's, and its cases are the Seat's own.
func holdValue(t *testing.T, namespace string, entry v1.MCPEnvVar, want string) {
	t.Helper()
	s := seat(namespace, "probe")
	s.Spec.MCP = []v1.MCPCredentials{{Name: "probe", Env: []v1.MCPCredentialValue{{
		Name: entry.Name, Value: entry.Value,
		ValueFrom: v1.SeatCredentialSource{SecretKeyRef: entry.ValueFrom.SecretKeyRef},
	}}}}
	u := unit(namespace, "probe")
	u.Spec.MCP = s.Spec.MCP
	headers := mcpServer(namespace, "probe")
	httpServer(headers)
	headers.Spec.Headers = []v1.MCPHeader{{Name: entry.Name, Value: entry.Value, ValueFrom: entry.ValueFrom}}
	objects := map[string]client.Object{
		"a Seat's spec.mcp": s, "a Unit's spec.mcp": u, "an MCPServer's headers": headers,
	}
	if envName.MatchString(entry.Name) {
		env := mcpServer(namespace, "probe")
		env.Spec.Env = []v1.MCPEnvVar{entry}
		objects["an MCPServer's env"] = env
	}
	for position, obj := range objects {
		t.Run(position, func(t *testing.T) { expect(t, k8sClient.Create(ctx, obj, client.DryRunAll), want) })
	}
}

// TestToolServerValuesTakeOneSource holds every value of a tool server, an
// MCPServer's and the ones a Seat or a Unit gives it, to exactly one of a
// literal value and a Secret key.
func TestToolServerValuesTakeOneSource(t *testing.T) {
	namespace := newNamespace(t)
	t.Run("a setting with neither value nor valueFrom is refused", func(t *testing.T) {
		holdValue(t, namespace, v1.MCPEnvVar{Name: envSlackChannels}, msgEnvExactlyOne)
	})
	t.Run("a setting with both value and valueFrom is refused", func(t *testing.T) {
		both := literal(envSlackChannels)
		both.ValueFrom = fromSecret("slack", "channels-cache")
		holdValue(t, namespace, both, msgEnvExactlyOne)
	})
	t.Run("a setting with a value alone is accepted", func(t *testing.T) {
		holdValue(t, namespace, literal(envSlackChannels), "")
	})
	t.Run("a setting with a valueFrom alone is accepted", func(t *testing.T) {
		holdValue(t, namespace, v1.MCPEnvVar{Name: envSlackChannels, ValueFrom: fromSecret("slack", "channels-cache")}, "")
	})
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
	secretEnv := []v1.MCPEnvVar{{Name: "TAVILY_API_KEY", ValueFrom: tavilyKey}}
	secretHeaders := []v1.MCPHeader{{Name: "X-Api-Key", ValueFrom: tavilyKey}}
	asHTTP := func(change func(*v1.MCPServer)) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			httpServer(s)
			change(s)
		}
	}
	withEnv := func(env ...v1.MCPEnvVar) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) { s.Spec.Env = env }
	}
	withHeaders := func(headers ...v1.MCPHeader) func(*v1.MCPServer) {
		return asHTTP(func(s *v1.MCPServer) { s.Spec.Headers = headers })
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
				s.Spec.Headers = []v1.MCPHeader{secretHeaders[0], {Name: "x-api-key", ValueFrom: tavilyKey}}
			}),
			want: msgHeaderCase,
		},
		{
			name: "settings in env as literal values are accepted",
			mutate: withEnv(
				v1.MCPEnvVar{Name: "SLACK_MCP_ADD_MESSAGE_TOOL", Value: new("true")},
				v1.MCPEnvVar{Name: envJiraURL, Value: new("https://jira.example.com")},
				v1.MCPEnvVar{Name: "GITLAB_HOST", Value: new("")},
				secretEnv[0],
			),
		},
		{
			name:   "a setting in headers as a literal value is accepted",
			mutate: withHeaders(v1.MCPHeader{Name: headerToolsets, Value: new("repos,issues")}, secretHeaders[0]),
		},
		{
			name:   "a literal value under a credential's name in env is refused",
			mutate: withEnv(v1.MCPEnvVar{Name: "TAVILY_API_KEY", Value: new("tvly-literal")}),
			want:   msgLiteralCredential,
		},
		{
			name:   "a literal value under a credential's name in headers is refused",
			mutate: withHeaders(v1.MCPHeader{Name: "X-Api-Key", Value: new("literal")}),
			want:   msgLiteralCredential,
		},
		{
			name:   "a literal Authorization header is refused",
			mutate: withHeaders(v1.MCPHeader{Name: "authorization", Value: new("Bearer literal")}),
			want:   msgLiteralCredential,
		},
		{
			name:   "a literal Proxy-Authorization header is refused",
			mutate: withHeaders(v1.MCPHeader{Name: "Proxy-Authorization", Value: new("Basic literal")}),
			want:   msgLiteralCredential,
		},
		{
			name:   "a literal Cookie header is refused",
			mutate: withHeaders(v1.MCPHeader{Name: "Cookie", Value: new("session=literal")}),
			want:   msgLiteralCredential,
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
