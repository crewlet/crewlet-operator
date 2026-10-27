package crd_test

import (
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The messages of the rules on an optionalKeys list.
const (
	msgOptionalHeaderCase = "optionalKeys names each header once: HTTP header names are case-insensitive"
	msgOptionalSeatEnv    = "optionalKeys may name only a variable of env"
	msgOptionalStep       = "optionalKeys may name only a variable of this step's env or a path of its files"
	msgOptionalHeaderName = `should match '^[A-Za-z0-9!#$%&'*+.^_|~-]+$'`
	msgOptionalEnvName    = `should match '^[A-Za-z_][A-Za-z0-9_]*$'`
)

// The keys the optionalKeys cases write and list.
const (
	fileNPMRC           = ".npmrc"
	envNPMToken         = "NPM_TOKEN"
	envDatabaseURL      = "DATABASE_URL"
	headerAuthorization = "Authorization"
	caseListedTwice     = "a key listed twice is refused"
)

// TestMCPServerOptionalKeysValidation covers the rules an MCPServer's
// optionalKeys is held to on its own: a key in the grammar a Seat's or a
// Unit's spec.mcp writes, and a header once in any case. Anything that turns
// on what the Seats and Units write is the render's to check, as Crewlet
// checks it against the whole document: a key the server does not write, or
// writes as a literal, is taken here, since a Seat or a Unit may write it
// from a Secret, and one that variable grammar does not admit too, since a
// Seat or a Unit may give a Stdio server one.
func TestMCPServerOptionalKeysValidation(t *testing.T) {
	registry := v1.MCPEnvVar{Name: "NPM_CONFIG_REGISTRY", ValueFrom: fromSecret("registry", "url")}
	auth := v1.MCPHeader{Name: headerAuthorization, ValueFrom: fromSecret("github", "header")}
	stdio := func(keys ...string) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			s.Spec.Env = []v1.MCPEnvVar{registry, literal(envSlackChannels)}
			s.Spec.OptionalKeys = keys
		}
	}
	overHTTP := func(keys ...string) func(*v1.MCPServer) {
		return func(s *v1.MCPServer) {
			httpServer(s)
			s.Spec.Headers = []v1.MCPHeader{auth, {Name: headerToolsets, Value: new("repos")}}
			s.Spec.OptionalKeys = keys
		}
	}
	runCreateCases(t, mcpServer, []createCase[*v1.MCPServer]{
		{name: "a Stdio server's variable read from a Secret is accepted", mutate: stdio("NPM_CONFIG_REGISTRY")},
		{name: "a variable only a Seat writes is accepted", mutate: stdio("SLACK_MCP_XOXB_TOKEN")},
		{
			name:   "a variable the server writes as a literal, which a Seat may write from a Secret, is accepted",
			mutate: stdio(envSlackChannels),
		},
		{
			name:   "a key a Seat may give a Stdio server outside a variable's grammar is accepted",
			mutate: stdio("npm-config-proxy"),
		},
		{name: "a key starting with a digit on a Stdio server is accepted", mutate: stdio("1PASSWORD_TOKEN")},
		{
			name:   "one variable in two cases on a Stdio server is accepted",
			mutate: stdio("NPM_CONFIG_REGISTRY", "npm_config_registry"),
		},
		{name: "a header read from a Secret is accepted", mutate: overHTTP(headerAuthorization)},
		{name: "a header in another case is accepted", mutate: overHTTP("authorization")},
		{name: "a header only a Seat writes is accepted", mutate: overHTTP("X-Api-Key")},
		{
			name:   "a header the server writes as a literal, which a Seat may write from a Secret, is accepted",
			mutate: overHTTP("x-mcp-toolsets"),
		},
		{
			name:   "one header listed in two cases is refused",
			mutate: overHTTP(headerAuthorization, "authorization"),
			want:   msgOptionalHeaderCase,
		},
		{
			name:   "a key outside a header name's grammar is refused",
			mutate: overHTTP("X Api"),
			want:   msgOptionalHeaderName,
		},
		{
			name:   "a key outside a header name's grammar is refused on a Stdio server too",
			mutate: stdio("NPM CONFIG"),
			want:   msgOptionalHeaderName,
		},
		{name: "an empty key is refused", mutate: stdio(""), want: "should be at least 1 chars long"},
		{name: caseListedTwice, mutate: stdio("NPM_CONFIG_REGISTRY", "NPM_CONFIG_REGISTRY"), want: msgDuplicate},
	})
}

// TestSeatSandboxOptionalKeysValidation covers a Seat's spec.sandbox
// optionalKeys: a variable of its env, read from a Secret or provisioned, and
// nothing else, which would leave nothing out.
func TestSeatSandboxOptionalKeysValidation(t *testing.T) {
	sandbox := func(keys ...string) func(*v1.Seat) {
		return func(s *v1.Seat) {
			s.Spec.Sandbox = v1.SeatSandbox{
				Env: []v1.SeatEnvVar{
					{Name: envDatabaseURL, ValueFrom: seatFromSecret("ana-sandbox", "database")},
					{
						Name:      gitlabToken,
						ValueFrom: v1.SeatCredentialSource{Provisioned: v1.ProvisionedCredential{Name: "gitlab-token"}},
					},
				},
				OptionalKeys: keys,
			}
		}
	}
	runCreateCases(t, seat, []createCase[*v1.Seat]{
		{name: "a variable read from a Secret is accepted", mutate: sandbox(envDatabaseURL)},
		{name: "a provisioned variable is accepted", mutate: sandbox(gitlabToken)},
		{name: "a variable env does not hold is refused", mutate: sandbox("PROXY_URL"), want: msgOptionalSeatEnv},
		{
			name:   "an optional key with no env is refused",
			mutate: func(s *v1.Seat) { s.Spec.Sandbox = v1.SeatSandbox{OptionalKeys: []string{envDatabaseURL}} },
			want:   msgOptionalSeatEnv,
		},
		{
			name:   "a key outside a variable name's grammar is refused",
			mutate: sandbox("database-url"),
			want:   msgOptionalEnvName,
		},
		{name: caseListedTwice, mutate: sandbox(envDatabaseURL, envDatabaseURL), want: msgDuplicate},
	})
}

// TestSetupStepOptionalKeysValidation covers a setup step's optionalKeys, on
// a Company's steps and a Seat's: a variable of the step's env or a path of
// its files, and nothing else, which would leave nothing out.
func TestSetupStepOptionalKeysValidation(t *testing.T) {
	step := func(keys ...string) v1.SandboxSetupStep {
		return v1.SandboxSetupStep{
			Name:         "registry",
			Files:        []v1.SandboxFile{{Path: fileNPMRC, ValueFrom: fromSecret("company-sandbox", "npmrc")}},
			Env:          []v1.SecretEnvVar{{Name: envNPMToken, ValueFrom: fromSecret("company-sandbox", "token")}},
			OptionalKeys: keys,
		}
	}
	onCompany := func(keys ...string) func(*v1.Company) {
		return func(c *v1.Company) {
			c.Spec.Sandbox = v1.SandboxSettings{
				DefaultRunIn: v1.SandboxPlacementE2B,
				E2B:          v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
				Setup:        []v1.SandboxSetupStep{step(keys...)},
			}
		}
	}
	runCreateCases(t, company, []createCase[*v1.Company]{
		{name: "a variable of the step's env is accepted", mutate: onCompany(envNPMToken)},
		{name: "a path of the step's files is accepted", mutate: onCompany(fileNPMRC)},
		{name: "both are accepted", mutate: onCompany(envNPMToken, fileNPMRC)},
		{
			name:   "a variable the step does not hold is refused",
			mutate: onCompany("NPM_CONFIG_REGISTRY"),
			want:   msgOptionalStep,
		},
		{name: "a path the step does not write is refused", mutate: onCompany("~/.npmrc"), want: msgOptionalStep},
		{name: caseListedTwice, mutate: onCompany(envNPMToken, envNPMToken), want: msgDuplicate},
	})
	runCreateCases(t, seat, []createCase[*v1.Seat]{
		{
			name:   "a seat's step naming its file is accepted",
			mutate: func(s *v1.Seat) { s.Spec.Sandbox = v1.SeatSandbox{Setup: []v1.SandboxSetupStep{step(fileNPMRC)}} },
		},
		{
			name: "a seat's step naming another step's variable is refused",
			mutate: func(s *v1.Seat) {
				other := step()
				other.Name = "other"
				other.Env[0].Name = "OTHER_TOKEN"
				s.Spec.Sandbox = v1.SeatSandbox{Setup: []v1.SandboxSetupStep{other, step("OTHER_TOKEN")}}
			},
			want: msgOptionalStep,
		},
	})
}
