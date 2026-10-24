package crd_test

import (
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// A credential is named through a Secret, in every position, and never
// written as a value: a user name and password in an address is one written
// as a value. Go's HTTP client sends it as Basic authentication to whatever
// the address names, and the engine builds every link it hands a person, and
// every webhook address it registers with a vendor, on the Company's own two.
// Every address the API takes refuses one, so each case offers one there.

// withUser is an address that holds a user name and password.
const withUser = "https://crewlet:hunter2@"

// msgUserInfo is the start of every rule's message, after the field's name.
const msgUserInfo = " may not hold a user name or password"

// fieldURL and fieldSiteURL are the names the vendors' address fields share.
const (
	fieldURL     = "url"
	fieldSiteURL = "siteURL"
)

func TestAddressesInCompanies(t *testing.T) {
	runCreateCases(t, company, []createCase[*v1.Company]{
		{
			name: "a user in the embeddings base URL is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{
					Type: v1.EmbeddingProviderOpenAICompatible, Model: "bge-m3", BaseURL: withUser + "embed.example",
				}
			},
			want: "baseURL" + msgUserInfo,
		},
		{
			name:   "a user in publicBaseURL is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = withUser + "hooks.nimbus.example" },
			want:   "publicBaseURL" + msgUserInfo,
		},
		{
			name:   "a user in dashboardBaseURL is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = withUser + "crewlet.nimbus.example" },
			want:   "dashboardBaseURL" + msgUserInfo,
		},
		{
			// An '@' in a path is no user: the rule reads the authority
			// alone.
			name:   "an '@' in the path is accepted",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "https://crewlet.nimbus.example/@nimbus" },
		},
	})
}

func TestAddressesInEngineClasses(t *testing.T) {
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{
			name: "a user in an S3 endpoint is refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Objects = v1.ObjectSettings{
					Backend: v1.ObjectBackendS3,
					S3: v1.S3ObjectSettings{
						Region: tenantsRegion, Bucket: tenantsBucket, Prefix: "{{uid}}/", Endpoint: withUser + "minio.storage:9000",
					},
				}
			},
			want: "endpoint" + msgUserInfo,
		},
		{
			name: "a user in the OTLP endpoint is refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Telemetry = v1.TelemetrySettings{OTLPEndpoint: withUser + "otel-collector:4318"}
			},
			want: "otlpEndpoint" + msgUserInfo,
		},
	})
}

func TestAddressesInMCPServers(t *testing.T) {
	runCreateCases(t, mcpServer, []createCase[*v1.MCPServer]{
		{
			name: "a user in an HTTP server's URL is refused",
			mutate: func(s *v1.MCPServer) {
				httpServer(s)
				s.Spec.URL = withUser + "mcp.example/mcp"
			},
			want: fieldURL + msgUserInfo,
		},
	})
}

// githubOnly is an Integration of GitHub alone.
func githubOnly(s *v1.IntegrationSpec) { s.GitHub = githubMember() }

func TestAddressesInIntegrations(t *testing.T) {
	for _, c := range []struct {
		name   string
		member func(*v1.IntegrationSpec)
		field  string
		set    func(*v1.IntegrationSpec)
	}{
		{"GitHub url", githubOnly, fieldURL, func(s *v1.IntegrationSpec) {
			s.GitHub.URL = withUser + "github.example.com"
		}},
		{"GitLab url", gitlabMember, fieldURL, func(s *v1.IntegrationSpec) {
			s.GitLab.URL = withUser + "gitlab.example.com"
		}},
		{"Mattermost url", mattermostMember, fieldURL, func(s *v1.IntegrationSpec) {
			s.Mattermost.URL = withUser + "chat.example.com"
		}},
		{"Jira url", jiraMember, fieldURL, func(s *v1.IntegrationSpec) {
			s.Jira.URL = withUser + "jira.example.com"
		}},
		{"Jira siteURL", jiraMember, fieldSiteURL, func(s *v1.IntegrationSpec) {
			s.Jira.SiteURL = withUser + "nimbus.atlassian.net"
		}},
		{"Confluence url", confluenceMember, fieldURL, func(s *v1.IntegrationSpec) {
			s.Confluence.CloudID = ""
			s.Confluence.URL = withUser + "wiki.example.com"
		}},
		{"Confluence siteURL", confluenceMember, fieldSiteURL, func(s *v1.IntegrationSpec) {
			s.Confluence.SiteURL = withUser + "nimbus.atlassian.net"
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			runCreateCases(t, vendorIntegration(c.member, false), []createCase[*v1.Integration]{{
				name:   "a user is refused",
				mutate: func(i *v1.Integration) { c.set(&i.Spec) },
				want:   c.field + msgUserInfo,
			}})
		})
	}
}
