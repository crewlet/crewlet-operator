package crd_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

func gatewayExposure() v1.GatewayExposure {
	return v1.GatewayExposure{ParentRefs: []v1.GatewayParentReference{{Namespace: "gateway-system", Name: "public"}}}
}

// ingressWithTLS is an Ingress exposure whose TLS Secret is named by the
// template given.
func ingressWithTLS(secretTemplate string) *v1.IngressExposure {
	ingress := &v1.IngressExposure{ClassName: "nginx"}
	ingress.TLS.SecretName = secretTemplate
	return ingress
}

func TestEngineClassDefaults(t *testing.T) {
	class := engineClass("", namespacePrefix(newNamespace(t))+"-defaults")
	if err := k8sClient.Create(ctx, class); err != nil {
		t.Fatal(err)
	}
	got := &v1.EngineClass{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(class), got); err != nil {
		t.Fatal(err)
	}
	s := got.Spec
	for _, c := range []struct {
		field     string
		got, want any
	}{
		{"data.allowedReplicas", s.Data.AllowedReplicas, []int32{1}},
		{"data.terminationGracePeriodSeconds", s.Data.TerminationGracePeriodSeconds, int64(300)},
		{"stream.mode", s.Stream.Mode, v1.StreamModeEmbedded},
		{"objects.backend", s.Objects.Backend, v1.ObjectBackendNATS},
		{"exposure.webhooks.mode", s.Exposure.Webhooks.Mode, v1.WebhookExposureNone},
		{"exposure.api.mode", s.Exposure.API.Mode, v1.APIExposureClusterInternal},
		{"networkPolicy", s.NetworkPolicy, v1.NetworkPolicyIsolated},
	} {
		if !equal(c.got, c.want) {
			t.Errorf("%s defaulted to %v, want %v", c.field, c.got, c.want)
		}
	}
}

func TestEngineClassObjects(t *testing.T) {
	s3 := func(prefix string) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) {
			c.Spec.Objects = v1.ObjectSettings{
				Backend: v1.ObjectBackendS3,
				S3:      v1.S3ObjectSettings{Region: "eu-central-1", Bucket: "crewlet-tenants", Prefix: prefix},
			}
		}
	}
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "minimal class is accepted"},
		{
			name:   "allowedReplicas outside 1, 3 and 5 is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.AllowedReplicas = []int32{2} },
			want:   "Unsupported value: 2",
		},
		{name: "S3 with a per-company prefix is accepted", mutate: s3("{{uid}}/")},
		{name: "S3 with the default prefix is accepted", mutate: s3("")},
		{
			name:   "S3 backend without s3 is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendS3} },
			want:   msgS3Backend,
		},
		{
			name: "s3 with the NATS backend is refused",
			mutate: func(c *v1.EngineClass) {
				s3("{{uid}}/")(c)
				c.Spec.Objects.Backend = v1.ObjectBackendNATS
			},
			want: msgS3Backend,
		},
		{
			name:   "a prefix with an unknown variable is refused",
			mutate: s3("{{uid}}/{{tenant}}/"),
			want:   "prefix may use only the variables",
		},
		{name: "a prefix every company shares is refused", mutate: s3("crewlet/"), want: msgPrefixUnique},
		{name: "a prefix naming only the namespace is refused", mutate: s3("{{namespace}}/"), want: msgPrefixUnique},
		{
			name: "an S3 endpoint that is not a URL is refused",
			mutate: func(c *v1.EngineClass) {
				s3("{{uid}}/")(c)
				c.Spec.Objects.S3.Endpoint = "minio.storage:9000"
			},
			want: "endpoint must be an absolute http or https URL",
		},
	})
}

func TestEngineClassExposure(t *testing.T) {
	webhooks := func(w v1.WebhookExposure) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Exposure.Webhooks = w }
	}
	gatewayHost := func(hostname string) func(*v1.EngineClass) {
		return webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureGateway, Gateway: gatewayExposure(), Hostname: hostname})
	}
	api := func(a v1.APIExposure) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Exposure.API = a }
	}
	apiGatewayHost := func(hostname string) func(*v1.EngineClass) {
		return api(v1.APIExposure{Mode: v1.APIExposureGateway, Gateway: gatewayExposure(), Hostname: hostname})
	}
	const apiHost = "{{uid}}.api.internal.example.com"

	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "Gateway webhooks are accepted", mutate: gatewayHost(webhookHost)},
		{
			name:   "a hostname from namespace and name is accepted",
			mutate: gatewayHost("{{name}}.{{namespace}}.hooks.example.com"),
		},
		{
			name: "Ingress webhooks with TLS are accepted",
			mutate: webhooks(v1.WebhookExposure{
				Mode:     v1.WebhookExposureIngress,
				Ingress:  ingressWithTLS("{{name}}-webhooks-tls"),
				Hostname: webhookHost,
			}),
		},
		{
			name:   "External webhooks need nothing more",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureExternal}),
		},
		{
			name:   "Gateway without gateway is refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureGateway, Hostname: webhookHost}),
			want:   msgGatewayMode,
		},
		{
			name:   "gateway with mode External is refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureExternal, Gateway: gatewayExposure()}),
			want:   msgGatewayMode,
		},
		{
			name:   "Ingress without ingress is refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureIngress, Hostname: webhookHost}),
			want:   msgIngressMode,
		},
		{
			name:   "Gateway without a hostname is refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureGateway, Gateway: gatewayExposure()}),
			want:   msgHostnameMode,
		},
		{
			name:   "a hostname with mode None is refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureNone, Hostname: webhookHost}),
			want:   msgHostnameMode,
		},
		{
			name:   "a hostname with an unknown variable is refused",
			mutate: gatewayHost("{{tenant}}.hooks.example.com"),
			want:   msgHostnameVars,
		},
		{
			name:   "a hostname every company shares is refused",
			mutate: gatewayHost("hooks.example.com"),
			want:   msgHostnameUnique,
		},
		{
			name:   "a hostname that is no DNS name is refused",
			mutate: gatewayHost("{{uid}}.Hooks_Example.com"),
			want:   msgHostnameDNS,
		},
		{
			name: "a TLS Secret name with an unknown variable is refused",
			mutate: webhooks(v1.WebhookExposure{
				Mode:     v1.WebhookExposureIngress,
				Ingress:  ingressWithTLS("{{tenant}}-tls"),
				Hostname: webhookHost,
			}),
			want: "secretName may use only the variables",
		},

		// The API's exposure follows the same rules as the webhooks'.
		{name: "a Gateway API route is accepted", mutate: apiGatewayHost(apiHost)},
		{
			name:   "an API Gateway without gateway is refused",
			mutate: api(v1.APIExposure{Mode: v1.APIExposureGateway, Hostname: apiHost}),
			want:   msgGatewayMode,
		},
		{
			name:   "an API Ingress without ingress is refused",
			mutate: api(v1.APIExposure{Mode: v1.APIExposureIngress, Hostname: apiHost}),
			want:   msgIngressMode,
		},
		{
			name:   "a ClusterInternal API with a hostname is refused",
			mutate: api(v1.APIExposure{Mode: v1.APIExposureClusterInternal, Hostname: apiHost}),
			want:   msgHostnameMode,
		},
		{
			name:   "an API hostname every company shares is refused",
			mutate: apiGatewayHost("api.example.com"),
			want:   msgHostnameUnique,
		},
		{
			name:   "an API hostname with an unknown variable is refused",
			mutate: apiGatewayHost("{{x}}.{{uid}}.example.com"),
			want:   msgHostnameVars,
		},
		{
			name:   "an API hostname that is no DNS name is refused",
			mutate: apiGatewayHost("{{uid}}..example.com"),
			want:   msgHostnameDNS,
		},
	})
}

func TestEngineClassPods(t *testing.T) {
	labels := func(l map[string]string) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.PodTemplate.Metadata.Labels = l }
	}
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "extra pod labels are accepted", mutate: labels(map[string]string{"team": "platform"})},
		{
			name:   "a pod label under crewlet.ai/ is refused",
			mutate: labels(map[string]string{"crewlet.ai/pool": "x"}),
			want:   msgReservedLabels,
		},
		{
			name:   "a pod label under app.kubernetes.io/ is refused",
			mutate: labels(map[string]string{"app.kubernetes.io/name": "x"}),
			want:   msgReservedLabels,
		},
		{
			name:   "an OTLP endpoint that is not a URL is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Telemetry = v1.TelemetrySettings{OTLPEndpoint: "otel-collector:4318"} },
			want:   "otlpEndpoint must be an absolute http or https URL",
		},
	})
}

// TestPlatformLLMProviders checks the copy of the LLMProvider rules that
// PlatformLLMProvider declares for itself.
func TestPlatformLLMProviders(t *testing.T) {
	keys := []v1.NamespacedSecretKeyReference{{Namespace: platformKeyNS, Name: "platform-llm", Key: apiKeyKey}}
	platform := func(settings v1.LLMProviderSettings, withKeys bool) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) {
			p := v1.PlatformLLMProvider{Name: "platform", LLMProviderSettings: settings}
			if withKeys {
				p.APIKeySecretRefs = keys
			}
			c.Spec.PlatformLLMProviders = []v1.PlatformLLMProvider{p}
		}
	}
	anthropic := v1.LLMProviderSettings{Type: v1.LLMProviderAnthropic, Model: claudeModel}
	openAI := v1.LLMProviderSettings{Type: v1.LLMProviderOpenAI, Model: "gpt-5"}
	with := func(s v1.LLMProviderSettings, change func(*v1.LLMProviderSettings)) v1.LLMProviderSettings {
		change(&s)
		return s
	}

	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "a platform provider is accepted", mutate: platform(anthropic, true)},
		{
			name:   "a platform Anthropic provider without keys is refused",
			mutate: platform(anthropic, false),
			want:   msgKeysRequired,
		},
		{
			name:   "a platform OpenAICompatible provider without baseURL is refused",
			mutate: platform(v1.LLMProviderSettings{Type: v1.LLMProviderOpenAICompatible, Model: "llama"}, false),
			want:   msgBaseURLRequired,
		},
		{
			name:   "claudeModel on a platform OpenAI provider is refused",
			mutate: platform(with(openAI, func(s *v1.LLMProviderSettings) { s.ClaudeModel = claudeModel }), true),
			want:   msgClaudeModel,
		},
		{
			name:   "reasoning on a platform Anthropic provider is refused",
			mutate: platform(with(anthropic, func(s *v1.LLMProviderSettings) { s.Reasoning = new(true) }), true),
			want:   msgReasoning,
		},
		{
			name:   "a thinking budget on a platform OpenAI provider is refused",
			mutate: platform(with(openAI, func(s *v1.LLMProviderSettings) { s.ReasoningBudgetTokens = 2048 }), true),
			want:   msgBudgetTokens,
		},
	})
}
