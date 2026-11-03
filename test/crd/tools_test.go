package crd_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
// variable, whose valueFrom is a Secret key, as every position's here is.
func holdValue(t *testing.T, namespace string, entry v1.MCPEnvVar, want string) {
	t.Helper()
	s := seat(namespace, "probe")
	s.Spec.MCP = []v1.MCPCredentials{{Name: "probe", Env: []v1.MCPCredentialValue{{
		Name: entry.Name, Value: entry.Value,
		ValueFrom: entry.ValueFrom,
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
	if got.Spec.Enabled == nil || !*got.Spec.Enabled {
		t.Errorf("enabled defaulted to %v, want true", got.Spec.Enabled)
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
			name: "a server turned off, with a description, is accepted",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Enabled = new(false)
				s.Spec.Description = "Searches the public web; " + strings.Repeat("é", 175)
			},
		},
		{
			name:   "a description over 200 characters is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Description = strings.Repeat("a", 201) },
			want:   "spec.description: Too long",
		},
		{
			name:   "a description over two lines is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Description = "Searches the web\nand reads pages" },
			want:   msgDescriptionLine,
		},
		{
			name:   "a description with a tab is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Description = "Searches\tthe web" },
			want:   msgDescriptionLine,
		},
		{
			// A LINE SEPARATOR AND A PARAGRAPH SEPARATOR break a line too,
			// though neither is a control character, and Crewlet refuses
			// both.
			name:   "a description with a line separator is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Description = "Searches the web\u2028and reads pages" },
			want:   msgDescriptionLine,
		},
		{
			name:   "a description with a paragraph separator is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.Description = "Searches the web\u2029and reads pages" },
			want:   msgDescriptionLine,
		},
		{
			name:   "an HTTP server with headers is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.Headers = secretHeaders }),
		},
		{
			name: "a per-seat server's seat credentials are accepted",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{
					{Kind: v1.SeatCredentialAtlassian, Field: v1.SeatCredentialToken, Key: envJiraToken},
					{Kind: v1.SeatCredentialAtlassian, Field: v1.SeatCredentialEmail, Key: "JIRA_USERNAME"},
				}
			},
		},
		{
			name: "an HTTP server's seat credential with a prefix is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{
					{Kind: v1.SeatCredentialGitHub, Key: headerAuthorization, Prefix: "Bearer "},
				}
			}),
		},
		{
			name: "a server granted to units and seats, with seats left out, is accepted",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Access = &v1.MCPServerAccess{
					UnitRefs:       []v1.LocalObjectReference{{Name: accessUnit}, {Name: "design"}},
					SeatRefs:       []v1.LocalObjectReference{{Name: "pm"}},
					ExceptSeatRefs: []v1.LocalObjectReference{{Name: accessExcepted}},
				}
			},
		},
		{
			// Crewlet reads `access: {}` as reaching no seat, and warns.
			name:   "a server granted to nobody is accepted",
			mutate: func(s *v1.MCPServer) { s.Spec.Access = &v1.MCPServerAccess{} },
		},
		{
			name: "a per-seat server's grant without seat credentials is accepted",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.Access = &v1.MCPServerAccess{SeatRefs: []v1.LocalObjectReference{{Name: "pm"}}}
			},
		},
		{
			name: "a grant on a server reading a vendor's seat credential is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{{Kind: v1.SeatCredentialGitLab, Key: gitlabToken}}
				s.Spec.Access = &v1.MCPServerAccess{SeatRefs: []v1.LocalObjectReference{{Name: "pm"}}}
			},
			want: msgAccessSeatCredentials,
		},
		{
			// An empty grant is a grant too: the integration's lane is the
			// one place this server's is made.
			name: "an empty grant on a server reading a vendor's seat credential is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{{Kind: v1.SeatCredentialSlack, Key: "SLACK_MCP_XOXB_TOKEN"}}
				s.Spec.Access = &v1.MCPServerAccess{}
			},
			want: msgAccessSeatCredentials,
		},
		{
			name: "a seat both granted and left out is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Access = &v1.MCPServerAccess{
					UnitRefs:       []v1.LocalObjectReference{{Name: accessUnit}},
					SeatRefs:       []v1.LocalObjectReference{{Name: "pm"}, {Name: accessExcepted}},
					ExceptSeatRefs: []v1.LocalObjectReference{{Name: accessExcepted}},
				}
			},
			want: msgAccessSeatBoth,
		},
		{
			name: "a unit named twice is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Access = &v1.MCPServerAccess{
					UnitRefs: []v1.LocalObjectReference{{Name: accessUnit}, {Name: accessUnit}},
				}
			},
			want: msgDuplicate,
		},
		{
			name: "a seat left out twice is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Access = &v1.MCPServerAccess{
					UnitRefs:       []v1.LocalObjectReference{{Name: accessUnit}},
					ExceptSeatRefs: []v1.LocalObjectReference{{Name: accessExcepted}, {Name: accessExcepted}},
				}
			},
			want: msgDuplicate,
		},
		{
			name: "a grant naming an empty name is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Access = &v1.MCPServerAccess{SeatRefs: []v1.LocalObjectReference{{Name: ""}}}
			},
			want: "spec.access.seatRefs[0].name",
		},
		{
			name: "seat credentials on a shared server are refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{{Kind: v1.SeatCredentialGitLab, Key: gitlabToken}}
			},
			want: "seatCredentials applies to a server with shared false",
		},
		{
			name: "a field of an integration other than Atlassian is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{
					{Kind: v1.SeatCredentialGitLab, Field: v1.SeatCredentialToken, Key: gitlabToken},
				}
			},
			want: "field applies to Atlassian",
		},
		{
			name: "an integration with no seat credential is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{{Kind: "Jira", Key: envJiraToken}}
			},
			want: `seatCredentials[0].kind: Unsupported value: "Jira"`,
		},
		{
			name: "one header read twice in two cases is refused",
			mutate: asHTTP(func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{
					{Kind: v1.SeatCredentialSlack, Key: headerAuthorization},
					{Kind: v1.SeatCredentialGitHub, Key: headerLower},
				}
			}),
			want: "seatCredentials names each header once",
		},
		{
			name: "a prefix naming a variable of the engine is refused",
			mutate: func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
				s.Spec.SeatCredentials = []v1.MCPSeatCredential{
					{Kind: v1.SeatCredentialSlack, Key: "SLACK_MCP_XOXB_TOKEN", Prefix: "${PREFIX}"},
				}
			},
			want: "prefix may not hold a ${NAME} reference",
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
		// THE ENGINE'S ENTRY RULES (crewlet internal/config/mcpnames.go):
		// a server's name, its toolPrefix and its url, refused here as
		// Crewlet refuses them, rather than as the company document every
		// other object of the Company is applied with.
		{
			name:   "a name of 32 characters is accepted",
			mutate: func(s *v1.MCPServer) { s.Name = strings.Repeat("a", 32) },
		},
		{
			name:   "a name of 33 characters is refused",
			mutate: func(s *v1.MCPServer) { s.Name = strings.Repeat("b", 33) },
			want:   msgServerName,
		},
		{
			name:   "a server name holding a dot is refused",
			mutate: func(s *v1.MCPServer) { s.Name = "tools.github" },
			want:   msgServerName,
		},
		{
			name:   "a toolPrefix of 49 characters, starting with _, is accepted",
			mutate: func(s *v1.MCPServer) { s.Spec.ToolPrefix = "_" + strings.Repeat("p", 48) },
		},
		{
			name:   "a toolPrefix of 50 characters is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.ToolPrefix = strings.Repeat("p", 50) },
			want:   "may not be more than 49",
		},
		{
			name:   "a toolPrefix with a dot is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.ToolPrefix = "gh." },
			want:   "should match",
		},
		{
			name:   "plain http to a public host is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://mcp.example.com/mcp" }),
			want:   msgPlainHTTP,
		},
		{
			name:   "plain http to a shared address space host is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://100.127.255.254/mcp" }),
		},
		{
			name:   "plain http just past the shared address space is refused",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://100.128.0.1/mcp" }),
			want:   msgPlainHTTP,
		},
		{
			name:   "plain http to a Service in the cluster is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://github-mcp.tools.svc.cluster.local:8080/mcp" }),
		},
		{
			name:   "plain http to a Service by its own name is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://github-mcp:8080/mcp" }),
		},
		{
			name:   "plain http to a private address is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://10.42.0.7:8080/mcp" }),
		},
		{
			name:   "plain http to the loopback address is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://[::1]:8080/mcp" }),
		},
		{
			name:   "plain http to localhost is accepted",
			mutate: asHTTP(func(s *v1.MCPServer) { s.Spec.URL = "http://localhost:3000/mcp" }),
		},
		{
			name: "tool levels by the server's own names are accepted",
			mutate: withTools(v1.MCPTools{Default: v1.MCPToolLevelBlocked, Levels: []v1.MCPToolLevelEntry{
				{Tool: "list_invoices", Level: v1.MCPToolLevelAllow},
				{Tool: "get-invoice.v2", Level: v1.MCPToolLevelAllow},
				{Tool: "void_invoice", Level: v1.MCPToolLevelApprove},
			}}),
		},
		{
			name:   "a default of Approve is accepted",
			mutate: withTools(v1.MCPTools{Default: v1.MCPToolLevelApprove}),
		},
		{
			name:   "a default outside Allow, Approve and Blocked is refused",
			mutate: withTools(v1.MCPTools{Default: "Ask"}),
			want:   "spec.tools.default: Unsupported value",
		},
		{
			name: "a level outside Allow, Approve and Blocked is refused",
			mutate: withTools(v1.MCPTools{Levels: []v1.MCPToolLevelEntry{
				{Tool: toolDeleteInvoice, Level: "approve"},
			}}),
			want: "spec.tools.levels[0].level: Unsupported value",
		},
		{
			name: "a tool named with a space is refused",
			mutate: withTools(v1.MCPTools{Levels: []v1.MCPToolLevelEntry{
				{Tool: "delete invoice", Level: v1.MCPToolLevelBlocked},
			}}),
			want: msgToolName,
		},
		{
			// A NO-BREAK SPACE is a space to Crewlet, though not to \s.
			name: "a tool named with a no-break space is refused",
			mutate: withTools(v1.MCPTools{Levels: []v1.MCPToolLevelEntry{
				{Tool: "delete invoice", Level: v1.MCPToolLevelBlocked},
			}}),
			want: msgToolName,
		},
		{
			name: "a tool named with a control character is refused",
			mutate: withTools(v1.MCPTools{Levels: []v1.MCPToolLevelEntry{
				{Tool: "delete\x7finvoice", Level: v1.MCPToolLevelBlocked},
			}}),
			want: msgToolName,
		},
		{
			name: "one tool given two levels is refused",
			mutate: withTools(v1.MCPTools{Levels: []v1.MCPToolLevelEntry{
				{Tool: toolDeleteInvoice, Level: v1.MCPToolLevelBlocked},
				{Tool: toolDeleteInvoice, Level: v1.MCPToolLevelAllow},
			}}),
			want: "spec.tools.levels[1]: Duplicate value",
		},
	})
}

// TestMCPServerToolsBlockNamesALevel holds an MCPServer's tools block to set a
// level: an empty block is refused rather than read as none. A typed client
// never sends one (the empty value is omitted), so it is written as a manifest
// would write it.
func TestMCPServerToolsBlockNamesALevel(t *testing.T) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(mcpServer(newNamespace(t), "empty-tools"))
	if err != nil {
		t.Fatal(err)
	}
	empty := &unstructured.Unstructured{Object: content}
	empty.SetGroupVersionKind(v1.GroupVersion.WithKind("MCPServer"))
	if err := unstructured.SetNestedMap(empty.Object, map[string]any{}, "spec", "tools"); err != nil {
		t.Fatal(err)
	}
	expect(t, k8sClient.Create(ctx, empty), "spec.tools")
}

// toolDeleteInvoice is a tool the level cases block, and msgToolName the API
// server's refusal of a tool name outside the pattern.
const (
	toolDeleteInvoice = "delete_invoice"
	msgToolName       = "spec.tools.levels[0].tool in body should match"
)

// withTools sets an MCPServer's tool levels.
func withTools(tools v1.MCPTools) func(*v1.MCPServer) {
	return func(s *v1.MCPServer) { s.Spec.Tools = tools }
}

// TestMCPServerOAuthValidation holds an MCPServer's spec.oauth to what
// Crewlet's mcp_servers[].oauth takes (its internal/config/mcpoauth.go): a
// registration naming what it needs and nothing else, scopes an authorization
// request can carry, and a sign-in that is the company's own, for a remote
// server, which sends the Authorization header itself.
func TestMCPServerOAuthValidation(t *testing.T) {
	oauth := func(o v1.MCPOAuth) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			httpServer(s)
			s.Spec.OAuth = o
		}
	}
	// signedIn is oauth with more changed after it.
	signedIn := func(o v1.MCPOAuth, more func(*v1.MCPServer)) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			oauth(o)(s)
			more(s)
		}
	}
	cimd := v1.MCPOAuth{Client: v1.MCPOAuthClientCIMD}
	const clientID = "crewlet-acme"
	own := v1.MCPOAuth{Client: v1.MCPOAuthClientOwn, ClientID: clientID}
	withScopes := func(scopes ...string) func(*v1.MCPServer) {
		return oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientDCR, Scopes: scopes})
	}
	fifty := make([]string, 50)
	for i := range fifty {
		fifty[i] = fmt.Sprintf("s%d", i)
	}
	runCreateCases(t, mcpServer, []createCase[*v1.MCPServer]{
		{name: "a sign-in by the engine's client metadata document is accepted", mutate: oauth(cimd)},
		{name: "a sign-in by a registration is accepted", mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientDCR})},
		{name: "the company's own public client is accepted", mutate: oauth(own)},
		{
			name: "the company's own confidential client is accepted",
			mutate: oauth(v1.MCPOAuth{
				Client: v1.MCPOAuthClientOwn, ClientID: "1234 5678.apps.example.com",
				ClientSecret: fromSecret("linear-oauth", "clientSecret"),
			}),
		},
		{
			// Every printable ASCII character a scope token may hold,
			// and fifty of them, Crewlet's bound.
			name:   "scopes of every character a scope token holds are accepted",
			mutate: withScopes("read", "write:issues", "https://example.com/auth/read", "!#$%&'()*+,-./:;<=>?@[]^_`{|}~"),
		},
		{name: "fifty scopes are accepted", mutate: withScopes(fifty...)},
		{
			name: "another header beside the sign-in is accepted",
			mutate: signedIn(cimd, func(s *v1.MCPServer) {
				s.Spec.Headers = []v1.MCPHeader{{Name: headerToolsets, Value: new("issues")}}
			}),
		},
		{
			name:   "a sign-in naming no client is refused",
			mutate: oauth(v1.MCPOAuth{Scopes: []string{"read"}}),
			want:   oauthRefusal("client", msgOAuthClient),
		},
		{
			name:   "an unknown client is refused",
			mutate: oauth(v1.MCPOAuth{Client: "Device"}),
			want:   `spec.oauth.client: Unsupported value: "Device"`,
		},
		{
			name:   "the company's own client with no id is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientOwn}),
			want:   oauthRefusal("clientID", msgOAuthClientIDRequired),
		},
		{
			name:   "a client id beside the engine's client metadata document is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientCIMD, ClientID: clientID}),
			want:   oauthRefusal("clientID", msgOAuthClientIDNotAllowed),
		},
		{
			name:   "a client id beside a registration is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientDCR, ClientID: clientID}),
			want:   oauthRefusal("clientID", msgOAuthClientIDNotAllowed),
		},
		{
			name:   "a client secret beside a registration is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientDCR, ClientSecret: fromSecret("linear-oauth", "clientSecret")}),
			want:   oauthRefusal("clientSecret", msgOAuthClientSecret),
		},
		{
			name: "a client secret beside the engine's client metadata document is refused",
			mutate: oauth(v1.MCPOAuth{
				Client: v1.MCPOAuthClientCIMD, ClientSecret: fromSecret("linear-oauth", "clientSecret"),
			}),
			want: oauthRefusal("clientSecret", msgOAuthClientSecret),
		},
		{
			name:   "a client id of white space alone is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientOwn, ClientID: whiteSpace}),
			want:   msgOAuthClientIDShape,
		},
		{
			name:   "a client id with a space at its end is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientOwn, ClientID: clientID + " "}),
			want:   msgOAuthClientIDShape,
		},
		{
			name:   "a client id naming a variable of the engine is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientOwn, ClientID: "${CLIENT_ID}"}),
			want:   "clientID may not hold a ${NAME} reference",
		},
		{name: "a scope with a space is refused", mutate: withScopes("read write"), want: msgOAuthScope},
		{name: "a scope with a double quote is refused", mutate: withScopes(`read"`), want: msgOAuthScope},
		{name: "a scope with a backslash is refused", mutate: withScopes(`read\all`), want: msgOAuthScope},
		{name: "a scope outside ASCII is refused", mutate: withScopes("l\u00e9cture"), want: msgOAuthScope},
		{
			name:   "a scope listed twice is refused",
			mutate: withScopes("read", "read"),
			want:   `spec.oauth.scopes[1]: Duplicate value: "read"`,
		},
		{
			name:   "fifty-one scopes are refused",
			mutate: withScopes(append(slices.Clone(fifty), "s50")...),
			want:   "spec.oauth.scopes: Too many",
		},
		{
			name:   "a scope naming a variable of the engine is refused",
			mutate: withScopes("${SCOPE}"),
			want:   "scopes may not hold a ${NAME} reference",
		},
		{
			name:   "a sign-in on a Stdio server is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.OAuth = cimd },
			want:   "oauth signs the company in to a remote server and applies to transport HTTP only",
		},
		{
			name: "a sign-in on a per-seat server is refused",
			mutate: signedIn(cimd, func(s *v1.MCPServer) {
				s.Spec.Shared = new(false)
			}),
			want: "oauth is one sign-in for the whole company",
		},
		{
			name: "an Authorization header beside the sign-in is refused",
			mutate: signedIn(cimd, func(s *v1.MCPServer) {
				s.Spec.Headers = []v1.MCPHeader{{Name: headerLower, ValueFrom: fromSecret("linear", "token")}}
			}),
			want: headersRefusal(msgOAuthAuthorization),
		},
	})
}

// TestMCPServerOAuthGrantValidation holds an MCPServer's spec.oauth.grant to
// what Crewlet's mcp_servers[].oauth.grant takes (validateServiceIdentity and
// validateSignIn in its internal/config/mcpoauth.go): a service identity
// signs in as the company's own client, with its id and its secret and no
// registration, a person's sign-in names how the engine registers whether or
// not the grant is written, and every rule of a sign-in holds for both. No
// header beside a sign-in may take the name the engine stores the client's
// secret under, in any case.
func TestMCPServerOAuthGrantValidation(t *testing.T) {
	const (
		clientID       = "crewlet-agents"
		storeKeyHeader = "oauth.client_secret"
		vendorOAuth    = "vendor-oauth"
		vendorKey      = "clientSecret"
		scopeReadOnly  = "read"
	)
	oauth := func(o v1.MCPOAuth, more ...func(*v1.MCPServer)) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			httpServer(s)
			s.Spec.OAuth = o
			for _, m := range more {
				m(s)
			}
		}
	}
	service := v1.MCPOAuth{
		Grant: v1.MCPOAuthGrantClientCredentials, ClientID: clientID, ClientSecret: fromSecret(vendorOAuth, vendorKey),
	}
	// serviceWith is the service identity changed by change.
	serviceWith := func(change func(*v1.MCPOAuth)) v1.MCPOAuth {
		o := service
		change(&o)
		return o
	}
	header := func(name string) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			s.Spec.Headers = []v1.MCPHeader{{Name: name, ValueFrom: fromSecret("vendor", "token")}}
		}
	}
	runCreateCases(t, mcpServer, []createCase[*v1.MCPServer]{
		{name: "a service identity is accepted", mutate: oauth(service)},
		{
			name:   "a service identity asking for scopes is accepted",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.Scopes = []string{scopeReadOnly, "write:issues"} })),
		},
		{
			name: "a person's sign-in that names its grant is accepted",
			mutate: oauth(v1.MCPOAuth{
				Grant: v1.MCPOAuthGrantAuthorizationCode, Client: v1.MCPOAuthClientOwn, ClientID: clientID,
				ClientSecret: fromSecret(vendorOAuth, vendorKey),
			}),
		},
		{
			name:   "another header beside a service identity is accepted",
			mutate: oauth(service, header(headerToolsets)),
		},
		{
			// The name is the engine's own key beside a sign-in alone.
			name:   "a header named as the client secret's key on a server with no sign-in is accepted",
			mutate: func(s *v1.MCPServer) { httpServer(s); header(storeKeyHeader)(s) },
		},
		{
			name:   "an unknown grant is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.Grant = "DeviceCode" })),
			want:   `spec.oauth.grant: Unsupported value: "DeviceCode"`,
		},
		{
			name:   "a service identity naming a registration is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.Client = v1.MCPOAuthClientDCR })),
			want:   oauthRefusal("client", msgOAuthClientRefused),
		},
		{
			name:   "a service identity naming the company's own client is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.Client = v1.MCPOAuthClientOwn })),
			want:   oauthRefusal("client", msgOAuthClientRefused),
		},
		{
			name:   "a service identity with no client id is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.ClientID = "" })),
			want:   oauthRefusal("clientID", msgOAuthClientIDRequired),
		},
		{
			name:   "a service identity with no client secret is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.ClientSecret = v1.SecretValueSource{} })),
			want:   oauthRefusal("clientSecret", msgOAuthClientSecretRequired),
		},
		{
			name:   "a service identity whose client id is white space alone is refused",
			mutate: oauth(serviceWith(func(o *v1.MCPOAuth) { o.ClientID = whiteSpace })),
			want:   msgOAuthClientIDShape,
		},
		{
			name:   "a person's sign-in that names its grant and no client is refused",
			mutate: oauth(v1.MCPOAuth{Grant: v1.MCPOAuthGrantAuthorizationCode}),
			want:   oauthRefusal("client", msgOAuthClient),
		},
		{
			name:   "a service identity on a Stdio server is refused",
			mutate: func(s *v1.MCPServer) { s.Spec.OAuth = service },
			want:   "oauth signs the company in to a remote server and applies to transport HTTP only",
		},
		{
			name:   "a service identity on a per-seat server is refused",
			mutate: oauth(service, func(s *v1.MCPServer) { s.Spec.Shared = new(false) }),
			want:   "oauth is one sign-in for the whole company",
		},
		{
			name:   "an Authorization header beside a service identity is refused",
			mutate: oauth(service, header("Authorization")),
			want:   headersRefusal(msgOAuthAuthorization),
		},
		{
			name:   "a header named as the client secret's key beside a service identity is refused",
			mutate: oauth(service, header(storeKeyHeader)),
			want:   headersRefusal(msgOAuthStoreKeyHeader),
		},
		{
			name:   "a header named as the client secret's key in another case beside a person's sign-in is refused",
			mutate: oauth(v1.MCPOAuth{Client: v1.MCPOAuthClientCIMD}, header("OAuth.Client_Secret")),
			want:   headersRefusal(msgOAuthStoreKeyHeader),
		},
	})

	// AN EMPTY BLOCK says nothing of how to sign in, and is refused rather
	// than read as a person's sign-in with no client. A typed client never
	// sends one (the empty struct is omitted), so it is written as a
	// manifest would write it.
	server := mcpServer(newNamespace(t), "empty-oauth")
	httpServer(server)
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(server)
	if err != nil {
		t.Fatal(err)
	}
	empty := &unstructured.Unstructured{Object: content}
	empty.SetGroupVersionKind(v1.GroupVersion.WithKind("MCPServer"))
	if err := unstructured.SetNestedMap(empty.Object, map[string]any{}, "spec", "oauth"); err != nil {
		t.Fatal(err)
	}
	expect(t, k8sClient.Create(ctx, empty), "spec.oauth in body should have at least 1 properties")
}

// The messages of the rules an MCPServer's spec.oauth is held to.
const (
	msgOAuthClient             = "client is required for a person's sign-in (grant AuthorizationCode, the default)"
	msgOAuthClientRefused      = "client is not allowed with grant ClientCredentials"
	msgOAuthClientIDRequired   = "clientID is required with grant ClientCredentials and with client Own"
	msgOAuthClientIDNotAllowed = "clientID applies to client Own and to grant ClientCredentials"
	msgOAuthClientIDShape      = "clientID is printable ASCII characters and spaces"
	msgOAuthClientSecret       = "clientSecret applies to client Own, a client registered at the vendor as " +
		"confidential, and to grant ClientCredentials"
	msgOAuthClientSecretRequired = "clientSecret is required with grant ClientCredentials"
	msgOAuthScope                = "each scope is one scope token as RFC 6749 spells it"
	msgOAuthAuthorization        = "headers may not hold Authorization beside oauth"
	msgOAuthStoreKeyHeader       = "headers may not hold a header named oauth.client_secret beside oauth"
)

// oauthRefusal is the API server's refusal of an MCPServer's spec.oauth by a
// rule over the whole block, given at its field, saying msg.
func oauthRefusal(field, msg string) string {
	return "spec.oauth." + field + ": Invalid value: " + msg
}

// headersRefusal is the API server's refusal of an MCPServer's spec.headers
// beside its spec.oauth, given at the headers, saying msg.
func headersRefusal(msg string) string {
	return "spec.headers: Invalid value: " + msg
}

// msgServerName and msgPlainHTTP are the messages of the rules an MCPServer's
// name and url are held to as Crewlet holds them.
const (
	msgServerName = "an MCPServer's name is its Crewlet server name"
	msgPlainHTTP  = "url may be plain http only to a host on no public network"
	// accessUnit and accessExcepted are the Unit and the Seat an MCPServer's
	// access names in the access cases.
	accessUnit     = "engineering"
	accessExcepted = "intern"
)

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
			want:   "url must be an absolute URL starting with https://",
		},
		{
			// Crewlet reads the scheme as written and refuses HTTPS://.
			name:   "a GitHub url with an upper-case scheme is refused",
			mutate: asGitHub(func(gh *v1.GitHubIntegration) { gh.URL = "HTTPS://github.example.com" }),
			want:   "url must be an absolute URL starting with https://",
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
