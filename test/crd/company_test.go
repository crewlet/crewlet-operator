package crd_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// platformURLVar is a variable a platform sets on the engine.
const platformURLVar = "PLATFORM_URL"

func TestCompanyDefaults(t *testing.T) {
	c := company(newNamespace(t), companyName)
	if err := k8sClient.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got := &v1.Company{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(c), got); err != nil {
		t.Fatal(err)
	}
	s := got.Spec
	for _, d := range []struct {
		field     string
		got, want any
	}{
		{"config.managementPolicy", s.Config.ManagementPolicy, v1.ManagementPolicyEnforce},
		{"topology.data.replicas", s.Topology.Data.Replicas, int32(1)},
		{"deletionPolicy", s.DeletionPolicy, v1.DeletionPolicyRetain},
	} {
		if !equal(d.got, d.want) {
			t.Errorf("%s defaulted to %v, want %v", d.field, d.got, d.want)
		}
	}
}

// fullCompany sets every field of the Company spec to a valid value.
func fullCompany(c *v1.Company) {
	c.Spec.EngineClassName = "standard"
	c.Spec.Mission = "Ship a reliable cloud storage product."
	c.Spec.TimeZone = "America/Argentina/Buenos_Aires"
	c.Spec.Tracker = v1.TrackerSettings{
		Backend: v1.TrackerNative,
		Native:  v1.NativeTrackerSettings{InboxRetentionDays: 90},
	}
	c.Spec.Knowledge = v1.KnowledgeSettings{Backend: v1.KnowledgeNative, SkillsContainer: new("")}
	c.Spec.TokenBudget = v1.TokenBudget{Day: 2_000_000}
	c.Spec.LLM.Order = []v1.LLMProviderReference{
		{Name: providerName},
		{Kind: v1.LLMProviderKindPlatform, Name: providerName},
	}
	c.Spec.Embeddings = v1.EmbeddingSettings{
		Model:           "text-embedding-3-large",
		APIKeySecretRef: secretKey("openai", apiKeyKey),
	}
	c.Spec.Sandbox = v1.SandboxSettings{
		E2B:                    v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
		DefaultPauseTTLSeconds: new(int32(0)),
	}
	c.Spec.EngineConfig = &runtime.RawExtension{
		Raw: []byte(`{"turn_engine":{"extension_enabled":true},"notification_rate_limit":5}`),
	}
	c.Spec.Exposure.PublicBaseURL = "https://hooks.nimbus.example"
	c.Spec.Runtime = v1.CompanyRuntime{
		Version: "v0.14.3",
		Env:     []v1.EnvVar{{Name: platformURLVar, Value: new("https://platform.example")}},
	}
	c.Spec.Suspend = new(true)
	c.Spec.DeletionPolicy = v1.DeletionPolicyDelete
}

func TestCompanyValidation(t *testing.T) {
	env := func(e v1.EnvVar) func(*v1.Company) {
		return func(c *v1.Company) { c.Spec.Runtime.Env = []v1.EnvVar{e} }
	}
	platformURL := fromSecret("platform", "url")
	rename := func(name string) func(*v1.Company) {
		return func(c *v1.Company) { c.Name = name }
	}

	runCreateCases(t, company, []createCase[*v1.Company]{
		{name: "minimal company is accepted"},
		{name: "a full company is accepted", mutate: fullCompany},

		// The name is part of the names of the objects the operator creates.
		{name: "a name that starts with a digit is refused", mutate: rename("9nimbus"), want: msgCompanyName},
		{name: "a name with a dot is refused", mutate: rename("nimbus.labs"), want: msgCompanyName},
		{name: "a name of 48 characters is refused", mutate: rename("n" + strings.Repeat("a", 47)), want: msgCompanyName},
		{name: "a name of 47 characters is accepted", mutate: rename("n" + strings.Repeat("a", 46))},

		{
			name:   "data replicas outside 1, 3 and 5 are refused",
			mutate: func(c *v1.Company) { c.Spec.Topology.Data.Replicas = 2 },
			want:   "Unsupported value: 2",
		},
		{
			name: "a native tracker block on backend None is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Tracker = v1.TrackerSettings{
					Backend: v1.TrackerNone,
					Native:  v1.NativeTrackerSettings{InboxRetentionDays: 90},
				}
			},
			want: "native applies to backend Native only",
		},
		{
			// The second reference names the same provider: kind defaults
			// to LLMProvider before the rule runs.
			name: "a provider named twice in the order is refused",
			mutate: func(c *v1.Company) {
				c.Spec.LLM.Order = []v1.LLMProviderReference{
					{Name: providerName},
					{Kind: v1.LLMProviderKindLLMProvider, Name: providerName},
				}
			},
			want: "order must name each provider once",
		},
		{
			name: "OpenAICompatible embeddings without baseURL are refused",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{
					Type: v1.EmbeddingProviderOpenAICompatible, Model: "nomic-embed-text", Dimensions: 768,
				}
			},
			want: msgBaseURLRequired,
		},
		{
			name: "an embeddings baseURL that is not a URL is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{
					Type: v1.EmbeddingProviderOpenAICompatible, Model: "m", BaseURL: "embeddings.local",
				}
			},
			want: msgBaseURLShape,
		},
		{
			name:   "an http publicBaseURL is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "http://hooks.nimbus.example" },
			want:   msgPublicBaseURL,
		},
		{
			name:   "a relative publicBaseURL is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "/webhooks" },
			want:   msgPublicBaseURL,
		},
		{name: "an env var from a Secret is accepted", mutate: env(v1.EnvVar{Name: platformURLVar, ValueFrom: platformURL})},
		{name: "an empty env value is accepted", mutate: env(v1.EnvVar{Name: "EMPTY", Value: new("")})},
		{
			name:   "an env var with both value and valueFrom is refused",
			mutate: env(v1.EnvVar{Name: platformURLVar, Value: new("x"), ValueFrom: platformURL}),
			want:   msgEnvExactlyOne,
		},
		{
			name:   "an env var with neither value nor valueFrom is refused",
			mutate: env(v1.EnvVar{Name: platformURLVar}),
			want:   msgEnvExactlyOne,
		},
		{
			name:   "a CREWLET_ variable is refused",
			mutate: env(v1.EnvVar{Name: "CREWLET_NODE_ID", Value: new("x")}),
			want:   msgEnvReserved,
		},
		{
			name:   "an OTEL_ variable is refused",
			mutate: env(v1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: new("x")}),
			want:   msgEnvReserved,
		},
	})
}

func TestCompanyImmutability(t *testing.T) {
	runUpdateCases(t, company, []updateCase[*v1.Company]{
		{
			name: "the company's other fields can change",
			mutate: func(c *v1.Company) {
				c.Spec.Mission = "A new mission."
				c.Spec.EngineClassName = "pro"
			},
		},
		{
			name:   "companyName cannot change",
			mutate: func(c *v1.Company) { c.Spec.CompanyName = "Nimbus Labs" },
			want:   "companyName is immutable",
		},
		{
			name:   "topology.data.replicas cannot change",
			mutate: func(c *v1.Company) { c.Spec.Topology.Data.Replicas = 3 },
			want:   "topology.data.replicas is immutable",
		},
	})
}
