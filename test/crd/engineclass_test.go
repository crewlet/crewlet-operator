package crd_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

func gatewayExposure() v1.GatewayExposure {
	return v1.GatewayExposure{ParentRefs: []v1.GatewayParentReference{{Namespace: "gateway-system", Name: "public"}}}
}

// ingressClass is the IngressClass of the Ingress exposures.
const ingressClass = "nginx"

// ingressWithTLS is an Ingress exposure whose TLS Secret is named by the
// template given, with the controller namespace the default networkPolicy
// Isolated needs.
func ingressWithTLS(secretTemplate string) *v1.IngressExposure {
	ingress := &v1.IngressExposure{ClassName: ingressClass, ControllerNamespaces: []string{"ingress-nginx"}}
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
		{"networkPolicy.mode", s.NetworkPolicy.Mode, v1.NetworkPolicyIsolated},
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
			name:   "allowedReplicas of 3 is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.AllowedReplicas = []int32{3} },
			want:   msgThreeReplicas,
		},
		{
			name:   "a storage size of zero is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("0")) },
			want:   msgStorageFloor,
		},
		{
			name:   "a storage size just under the engine's floor is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("7167Mi")) },
			want:   msgStorageFloor,
		},
		{
			name:   "a storage size of exactly the floor is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("7Gi")) },
		},
		{
			name:   "a storage size in bytes is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("10737418240")) },
		},
		{
			name:   "a version override with an image by tag is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Engine.AllowVersionOverride = new(true) },
		},
		{
			name: "a version override with an image by digest is refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Engine.Image = engineDigestImage
				c.Spec.Engine.AllowVersionOverride = new(true)
			},
			want: "allowVersionOverride needs an image by tag",
		},
		{
			name:   "an image by digest without a version override is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Engine.Image = engineDigestImage },
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
		// Names and namespaces hold '-': team-a/b-web and team-a-b/web would
		// both be team-a-b-web.
		{
			name:   "a prefix joining namespace and name by '-' is refused",
			mutate: s3("{{namespace}}-{{name}}/"),
			want:   msgPrefixUnique,
		},
		{
			name:   "a prefix joining name and namespace by '-' is refused",
			mutate: s3("{{name}}-{{namespace}}/"),
			want:   msgPrefixUnique,
		},
		{name: "a prefix joining namespace and name by '.' is accepted", mutate: s3("tenants/{{namespace}}.{{name}}/")},
		{name: "a prefix joining name and namespace by '/' is accepted", mutate: s3("{{name}}/{{namespace}}/")},
		{name: "a prefix by UID is accepted", mutate: s3("{{uid}}/")},
		// Without the '/', nimbus/web's prefix starts nimbus/web-2's.
		{
			name:   "a prefix that does not end with '/' is refused",
			mutate: s3("{{namespace}}/{{name}}"),
			want:   msgPrefixSlash,
		},
		{name: "a UID prefix that does not end with '/' is refused", mutate: s3("crewlet-{{uid}}"), want: msgPrefixSlash},
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

// engineDigestImage is the engine image pinned by digest.
const engineDigestImage = "ghcr.io/crewlet/crewlet@sha256:" +
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

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
			name:   "a hostname joining namespace and name by '-' is refused",
			mutate: gatewayHost("{{namespace}}-{{name}}.hooks.example.com"),
			want:   msgHostnameUnique,
		},
		{
			name:   "a hostname joining name and namespace by '-' is refused",
			mutate: gatewayHost("{{name}}-{{namespace}}.hooks.example.com"),
			want:   msgHostnameUnique,
		},
		{
			name:   "a hostname joining name and namespace by '.' is accepted",
			mutate: gatewayHost("{{name}}.{{namespace}}.hooks.example.com"),
		},
		{
			name:   "a hostname joining namespace and name by '.' is accepted",
			mutate: gatewayHost("{{namespace}}.{{name}}.hooks.example.com"),
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
			name:   "an API hostname joining namespace and name by '-' is refused",
			mutate: apiGatewayHost("{{namespace}}-{{name}}.api.example.com"),
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

		// Under networkPolicy Isolated the operator has to know where an
		// Ingress controller runs, or its policy would refuse everything the
		// Ingress forwards.
		{
			name: "Isolated Ingress webhooks without controllerNamespaces are refused",
			mutate: webhooks(v1.WebhookExposure{
				Mode:     v1.WebhookExposureIngress,
				Ingress:  &v1.IngressExposure{ClassName: ingressClass},
				Hostname: webhookHost,
			}),
			want: msgIngressControllerNamespaces,
		},
		{
			name: "an Isolated Ingress API without controllerNamespaces is refused",
			mutate: api(v1.APIExposure{
				Mode:     v1.APIExposureIngress,
				Ingress:  &v1.IngressExposure{ClassName: "internal"},
				Hostname: apiHost,
			}),
			want: msgIngressControllerNamespaces,
		},
		{
			name: "Ingress webhooks without controllerNamespaces and no network policy are accepted",
			mutate: func(c *v1.EngineClass) {
				c.Spec.NetworkPolicy = v1.NetworkPolicySettings{Mode: v1.NetworkPolicyNone}
				c.Spec.Exposure.Webhooks = v1.WebhookExposure{
					Mode:     v1.WebhookExposureIngress,
					Ingress:  &v1.IngressExposure{ClassName: ingressClass},
					Hostname: webhookHost,
				}
			},
		},
		{
			name: "Gateway proxy namespaces are accepted",
			mutate: func(c *v1.EngineClass) {
				gatewayHost(webhookHost)(c)
				c.Spec.Exposure.Webhooks.Gateway.ProxyNamespaces = []string{"envoy-gateway-system"}
			},
		},
		{
			name: "a proxy namespace that is no namespace name is refused",
			mutate: func(c *v1.EngineClass) {
				gatewayHost(webhookHost)(c)
				c.Spec.Exposure.Webhooks.Gateway.ProxyNamespaces = []string{"Envoy_Gateway"}
			},
			want: "spec.exposure.webhooks.gateway.proxyNamespaces[0]",
		},
	})
}

func TestEngineClassNetworkPolicy(t *testing.T) {
	platform := &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "platform-system"}}
	policy := func(p v1.NetworkPolicySettings) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.NetworkPolicy = p }
	}
	except := func(cidrs ...string) func(*v1.EngineClass) {
		return policy(v1.NetworkPolicySettings{Egress: v1.NetworkPolicyEgress{ExceptCIDRs: cidrs}})
	}
	const msgCIDR = "exceptCIDRs must be canonical CIDRs"
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{
			name: "API consumers and egress peers are accepted",
			mutate: policy(v1.NetworkPolicySettings{
				APIConsumers: []v1.NetworkPolicyPeer{{
					NamespaceSelector: platform,
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "control-plane"}},
				}},
				Egress: v1.NetworkPolicyEgress{To: []v1.NetworkPolicyPeer{{NamespaceSelector: platform}}},
			}),
		},
		{
			name:   "an API consumer without a namespace selector is refused",
			mutate: policy(v1.NetworkPolicySettings{APIConsumers: []v1.NetworkPolicyPeer{{PodSelector: platform}}}),
			want:   "spec.networkPolicy.apiConsumers[0].namespaceSelector: Required value",
		},
		{
			name: "API consumers with mode None are refused",
			mutate: policy(v1.NetworkPolicySettings{
				Mode:         v1.NetworkPolicyNone,
				APIConsumers: []v1.NetworkPolicyPeer{{NamespaceSelector: platform}},
			}),
			want: "apiConsumers and egress apply to mode Isolated only",
		},
		{
			name: "egress with mode None is refused",
			mutate: policy(v1.NetworkPolicySettings{
				Mode:   v1.NetworkPolicyNone,
				Egress: v1.NetworkPolicyEgress{ExceptCIDRs: []string{"10.0.0.0/8"}},
			}),
			want: "apiConsumers and egress apply to mode Isolated only",
		},
		{name: "IPv4 and IPv6 blocks are accepted", mutate: except("10.0.0.0/8", "fd00:10::/48", "192.168.1.7/32")},
		{name: "an address that is no CIDR is refused", mutate: except("10.0.0.1"), want: msgCIDR},
		{name: "a CIDR with host bits set is refused", mutate: except("10.0.0.1/8"), want: msgCIDR},
		// The whole address space as an exception would leave the engine
		// nothing, and the API server refuses an ipBlock whose except is its
		// cidr.
		{name: "the whole IPv4 space is refused", mutate: except("0.0.0.0/0"), want: msgCIDR},
		{name: "the whole IPv6 space is refused", mutate: except("::/0"), want: msgCIDR},
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
		{
			name: "service account annotations are accepted",
			mutate: func(c *v1.EngineClass) {
				c.Spec.ServiceAccount.Annotations = map[string]string{
					"eks.amazonaws.com/role-arn": "arn:aws:iam::111122223333:role/crewlet-engine",
				}
			},
		},
		{
			name: "env from a literal and from a platform Secret is accepted",
			mutate: classEnv(
				v1.ClassEnvVar{Name: "PLATFORM_URL", Value: new("https://platform.example")},
				v1.ClassEnvVar{Name: platformKeyVar, ValueFrom: platformKey},
			),
		},
		{
			name:   "an env var with both value and valueFrom is refused",
			mutate: classEnv(v1.ClassEnvVar{Name: platformKeyVar, Value: new("x"), ValueFrom: platformKey}),
			want:   msgEnvExactlyOne,
		},
		{
			name:   "an env var with neither value nor valueFrom is refused",
			mutate: classEnv(v1.ClassEnvVar{Name: platformKeyVar}),
			want:   msgEnvExactlyOne,
		},
		{
			name:   "a CREWLET_ variable is refused",
			mutate: classEnv(v1.ClassEnvVar{Name: "CREWLET_NODE_ID", Value: new("x")}),
			want:   msgEnvReserved,
		},
		{
			name:   "OPENAI_API_KEY is refused",
			mutate: classEnv(v1.ClassEnvVar{Name: "OPENAI_API_KEY", ValueFrom: platformKey}),
			want:   msgProviderKeyEnv,
		},
	})
}

// platformKeyVar is a variable a platform sets on every engine of a class,
// from platformKey, a Secret in the platform's namespace.
const platformKeyVar = "PLATFORM_KEY"

var platformKey = v1.NamespacedSecretValueSource{
	SecretKeyRef: v1.NamespacedSecretKeyReference{Namespace: platformKeyNS, Name: "platform-wiring", Key: "machineKey"},
}

func classEnv(env ...v1.ClassEnvVar) func(*v1.EngineClass) {
	return func(c *v1.EngineClass) { c.Spec.PodTemplate.Spec.Env = env }
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
		{
			name: "reasoningEffort on a platform OpenAI provider with reasoning is accepted",
			mutate: platform(with(openAI, func(s *v1.LLMProviderSettings) {
				s.Reasoning = new(true)
				s.ReasoningEffort = v1.ReasoningEffortHigh
			}), true),
		},
		{
			name:   "reasoningEffort on a platform OpenAI provider without reasoning is refused",
			mutate: platform(with(openAI, func(s *v1.LLMProviderSettings) { s.ReasoningEffort = v1.ReasoningEffortHigh }), true),
			want:   msgReasoningEffort,
		},
		{
			name: "reasoningEffort on a platform OpenAICompatible provider is refused",
			mutate: platform(v1.LLMProviderSettings{
				Type: v1.LLMProviderOpenAICompatible, Model: "llama", BaseURL: "http://vllm:8000/v1",
				ReasoningEffort: v1.ReasoningEffortHigh,
			}, false),
			want: msgReasoningEffort,
		},
	})
}
