package crd_test

import (
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Names the fixtures share. The Secrets they name never exist: the API server
// validates a reference's shape, and whether the Secret is there is the
// operator's question, reported in an object's status.
const (
	companyName   = "nimbus"
	claudeModel   = "claude-sonnet-5"
	providerName  = "anthropic"
	unitName      = "platform"
	seatHandle    = "ana"
	apiKeyKey     = "apiKey"
	webhookHost   = "{{uid}}.hooks.example.com"
	engineImage   = "ghcr.io/crewlet/crewlet:v0.1.0"
	githubKeys    = "nimbus-github"
	githubServer  = "https://api.githubcopilot.com/mcp/"
	platformKeyNS = "crewlet-system"
	containerKey  = "PLAT"
)

// The kinds the tests that apply manifests treat specially.
const (
	kindEngineClass = "EngineClass"
	kindCompany     = "Company"
)

// The messages of the rules more than one case checks. Each is the message
// declared on the rule, or the start of it.
const (
	msgGatewayMode                 = "gateway is required with mode Gateway and not allowed otherwise"
	msgIngressMode                 = "ingress is required with mode Ingress and not allowed otherwise"
	msgIngressControllerNamespaces = "networkPolicy Isolated with an Ingress needs ingress.controllerNamespaces"
	msgHostnameMode                = "hostname is required with modes Gateway and Ingress and not allowed otherwise"
	msgHostnameVars                = "hostname may use only the variables"
	msgHostnameUnique              = "hostname must contain {{uid}}, or {{name}} and {{namespace}} joined by a '.'"
	msgHostnameDNS                 = "hostname must be a lowercase DNS name once its variables are replaced"
	msgPrefixUnique                = "prefix must contain {{uid}}, or {{namespace}} and {{name}} joined by a '/' or '.'"
	msgPrefixSlash                 = "prefix must end with '/'"
	msgS3Backend                   = "s3 is required with backend S3 and not allowed otherwise"
	msgReservedLabels              = "labels under crewlet.ai/ and app.kubernetes.io/ are the operator's own"
	msgKeysRequired                = "apiKeySecretRefs needs at least one key for types Anthropic and OpenAI"
	msgBaseURLRequired             = "baseURL is required for type OpenAICompatible"
	msgClaudeModel                 = "claudeModel applies to type Anthropic only"
	msgReasoning                   = "reasoning applies to type OpenAI only"
	msgBudgetTokens                = "reasoningBudgetTokens applies to type Anthropic only"
	msgBaseURLShape                = "baseURL must be an absolute http or https URL"
	msgCompanyName                 = "a Company's name must be a DNS label of at most 47 characters"
	msgEnvExactlyOne               = "exactly one of value and valueFrom must be set"
	msgEnvReserved                 = "variables starting with CREWLET_ or OTEL_ are set by the operator"
	msgPublicBaseURL               = "publicBaseURL must be an absolute https URL"
	msgUnitID                      = "a Unit's name is its Crewlet unit id"
	msgDuplicate                   = "Duplicate value"
	msgThreeReplicas               = "Unsupported value: 3"
	msgStorageFloor                = "size must be at least 7Gi"
	msgHeaderCase                  = "names must differ in more than case: HTTP header names are case-insensitive"
	msgContainerKey                = `should match '^[A-Z][A-Z0-9]{1,9}$'`
	msgProviderKeyEnv              = "ANTHROPIC_API_KEY and OPENAI_API_KEY are model provider keys"
	msgReasoningEffort             = "reasoningEffort is sent to an OpenAI model only while reasoning is true"
	msgSeatName                    = "without spec.handle the Seat's name is its handle"
	msgHandleImmutable             = "the seat's handle is immutable"
	msgHumanContact                = "a Human seat needs contact"
	msgAgentContact                = "contact and availability apply to Human seats only"
	msgHumanForbidden              = "a Human seat is never run by the engine"
	msgStdioCommand                = "command is required with transport Stdio and not allowed with HTTP"
	msgStdioOnly                   = "args and env apply to transport Stdio only"
	msgHTTPURL                     = "url is required with transport HTTP and not allowed with Stdio"
	msgOneVendor                   = "exactly one vendor (slack or github) must be set"
	msgCompanyRefFrozen            = "companyRef is immutable"
)

func companyRef() v1.CompanyReference { return v1.CompanyReference{Name: companyName} }

func secretKey(name, key string) v1.SecretKeyReference {
	return v1.SecretKeyReference{Name: name, Key: key}
}

func fromSecret(name, key string) v1.SecretValueSource {
	return v1.SecretValueSource{SecretKeyRef: secretKey(name, key)}
}

// engineClass is the smallest valid EngineClass. It is cluster-scoped, so the
// namespace argument is unused; the name carries the test's prefix instead.
func engineClass(_, name string) *v1.EngineClass {
	return &v1.EngineClass{
		Name: name,
		Spec: v1.EngineClassSpec{
			Engine: v1.EngineSettings{Image: engineImage},
			Data: v1.DataPodSettings{
				Storage: v1.DataStorage{Size: new(resource.MustParse("10Gi"))},
			},
		},
	}
}

// company is the smallest valid Company.
func company(namespace, name string) *v1.Company {
	return &v1.Company{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.CompanySpec{CompanyName: "Nimbus"},
	}
}

// llmProvider is the smallest valid LLMProvider.
func llmProvider(namespace, name string) *v1.LLMProvider {
	return &v1.LLMProvider{
		Namespace: namespace,
		Name:      name,
		Spec: v1.LLMProviderSpec{
			CompanyRef:          companyRef(),
			LLMProviderSettings: v1.LLMProviderSettings{Type: v1.LLMProviderAnthropic, Model: claudeModel},
			APIKeySecretRefs:    []v1.SecretKeyReference{secretKey(providerName, apiKeyKey)},
		},
	}
}

// unit is the smallest valid Unit.
func unit(namespace, name string) *v1.Unit {
	return &v1.Unit{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.UnitSpec{CompanyRef: companyRef()},
	}
}

// seat is the smallest valid Seat: an agent.
func seat(namespace, name string) *v1.Seat {
	return &v1.Seat{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.SeatSpec{CompanyRef: companyRef()},
	}
}

// mcpServer is the smallest valid MCPServer: a Stdio server.
func mcpServer(namespace, name string) *v1.MCPServer {
	return &v1.MCPServer{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.MCPServerSpec{CompanyRef: companyRef(), Command: "uvx"},
	}
}

// slackIntegration is the smallest valid Integration: Slack with defaults.
func slackIntegration(namespace, name string) *v1.Integration {
	return &v1.Integration{
		Namespace: namespace,
		Name:      name,
		Spec:      v1.IntegrationSpec{CompanyRef: companyRef(), Slack: &v1.SlackIntegration{}},
	}
}

func githubMember() v1.GitHubIntegration {
	return v1.GitHubIntegration{WebhookSecretRef: secretKey(githubKeys, "webhookSecret")}
}

// mcpCredentials gives a seat or a unit its own Authorization header for a
// server.
func mcpCredentials(server string) v1.MCPCredentials {
	return v1.MCPCredentials{
		Name: server,
		Env: []v1.MCPCredentialValue{{
			Name:      "Authorization",
			ValueFrom: fromSecret(seatHandle+"-"+server, "authorization"),
		}},
	}
}

func providers(names ...string) []v1.LLMProviderReference {
	refs := make([]v1.LLMProviderReference, 0, len(names))
	for _, name := range names {
		refs = append(refs, v1.LLMProviderReference{Name: name})
	}
	return refs
}
