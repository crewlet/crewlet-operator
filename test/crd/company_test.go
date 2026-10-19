package crd_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

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
		{"topology.defaultSeatPool", s.Topology.DefaultSeatPool, v1.DataPool},
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
	c.Spec.Knowledge = v1.KnowledgeSettings{
		Backend:         v1.KnowledgeNative,
		Scope:           []string{containerKey, "ENG2"},
		SkillsContainer: new(""),
		RootSpace:       "HQ",
		Vectors:         new(true),
	}
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
	// One object and one scalar: a value may be any JSON value.
	c.Spec.EngineConfig = []v1.EngineConfigEntry{
		{Name: "turn_engine", Value: &v1.EngineConfigValue{Raw: []byte(`{"extension_enabled":true}`)}},
		{Name: "notification_rate_limit", Value: &v1.EngineConfigValue{Raw: []byte(`5`)}},
	}
	c.Spec.Exposure.PublicBaseURL = "https://hooks.nimbus.example"
	c.Spec.Runtime = v1.CompanyRuntime{
		Version: "v1.2.4",
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
	dashboard := func(address string) func(*v1.Company) {
		return func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = address }
	}
	public := func(address string) func(*v1.Company) {
		return func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = address }
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
			// v1alpha1 runs one data pod; 3 and 5 are not admitted until the
			// operator can run them.
			name:   "3 data replicas are refused",
			mutate: func(c *v1.Company) { c.Spec.Topology.Data.Replicas = 3 },
			want:   msgThreeReplicas,
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
			name: "OpenAICompatible embeddings without a key are accepted",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{
					Type: v1.EmbeddingProviderOpenAICompatible, Model: "nomic-embed-text", BaseURL: "http://ollama:11434/v1",
				}
			},
		},
		{
			// Crewlet trims the embedding model and refuses an empty one,
			// white space as Go's strings.TrimSpace reads it.
			name: "an embeddings model of white space alone is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{Model: " \t ", APIKeySecretRef: secretKey("openai", apiKeyKey)}
			},
			want: msgBlankEmbeddingModel,
		},
		{
			name: "an embeddings model of Unicode white space alone is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{
					Type: v1.EmbeddingProviderOpenAICompatible, Model: "\u3000\u00a0", BaseURL: "http://ollama:11434/v1",
				}
			},
			want: msgBlankEmbeddingModel,
		},
		{
			// A format character is not white space to Go, so the engine
			// takes it as a model like any other.
			name: "an embeddings model Go does not count as white space is accepted",
			mutate: func(c *v1.Company) {
				c.Spec.Embeddings = v1.EmbeddingSettings{Model: "\u200b", APIKeySecretRef: secretKey("openai", apiKeyKey)}
			},
		},
		{
			name:   "OpenAI embeddings without a key are refused",
			mutate: func(c *v1.Company) { c.Spec.Embeddings = v1.EmbeddingSettings{Model: "text-embedding-3-small"} },
			want:   "apiKeySecretRef is required for type OpenAI",
		},
		{
			name:   "vectors without embeddings are refused",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.Vectors = new(true) },
			want:   "knowledge.vectors needs spec.embeddings",
		},
		{
			name:   "vectors turned off without embeddings are accepted",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.Vectors = new(false) },
		},
		{
			name:   "a scope that is not a container key is refused",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.Scope = []string{"docs"} },
			want:   msgContainerKey,
		},
		{
			name:   "a container key of 11 characters is refused",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.RootSpace = "ABCDEFGHIJK" },
			want:   "spec.knowledge.rootSpace",
		},
		{
			name:   "a one-letter rootSpace is refused",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.RootSpace = "H" },
			want:   "spec.knowledge.rootSpace",
		},
		{
			name:   "a skillsContainer that is a container key is accepted",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.SkillsContainer = new("TOOLS") },
		},
		{
			name:   "a skillsContainer that is not a container key is refused",
			mutate: func(c *v1.Company) { c.Spec.Knowledge.SkillsContainer = new("tools") },
			want:   "skillsContainer must be empty or a container key",
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
		{
			// Crewlet reads the scheme as written and refuses HTTPS://, so
			// the API does too, rather than crash-loop the data pod.
			name:   "a publicBaseURL with an upper-case scheme is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "HTTPS://hooks.nimbus.example" },
			want:   msgPublicBaseURL,
		},
		{
			// An internal network's dashboard may be plain HTTP; Crewlet
			// takes either scheme.
			name:   "an http dashboardBaseURL is accepted",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "http://crewlet.nimbus.internal:8080/" },
		},
		{
			name:   "a dashboardBaseURL without a scheme is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "crewlet.nimbus.example" },
			want:   msgDashboardBaseURL,
		},
		{
			name:   "a dashboardBaseURL of another scheme is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "ftp://crewlet.nimbus.example" },
			want:   msgDashboardBaseURL,
		},
		{
			name:   "a dashboardBaseURL with an upper-case scheme is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "Https://crewlet.nimbus.example" },
			want:   msgDashboardBaseURL,
		},
		{
			// A path is a base a path-routing proxy serves the engine
			// under, which Crewlet takes.
			name:   "a dashboardBaseURL with a path is accepted",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "https://crewlet.nimbus.example/nimbus/" },
		},
		{
			name: "a dashboardBaseURL with a query is refused",
			mutate: func(c *v1.Company) {
				c.Spec.Exposure.DashboardBaseURL = "https://crewlet.nimbus.example/?tenant=nimbus"
			},
			want: msgDashboardBaseURLQuery,
		},
		{
			name:   "a dashboardBaseURL with a fragment is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.DashboardBaseURL = "https://crewlet.nimbus.example/#/work" },
			want:   msgDashboardBaseURLQuery,
		},
		// Crewlet refuses to start on a browser's address whose host a
		// browser rewrites before it sends it, and on any port outside 1 to
		// 65535, so the API refuses each; their neighbours it takes.
		{
			name:   "a dashboardBaseURL on port 0 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:0"),
			want:   msgDashboardBaseURLPort,
		},
		{
			name:   "a dashboardBaseURL on port 65536 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:65536"),
			want:   msgDashboardBaseURLPort,
		},
		{
			// A port no int holds is refused by the rule's message, not by
			// an evaluation error.
			name:   "a dashboardBaseURL on a port of fifty digits is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:" + strings.Repeat("9", 50)),
			want:   msgDashboardBaseURLPort,
		},
		// One port beyond 65535 for each branch of the rule's pattern, and
		// leading zeros before one and before the largest that is not.
		{
			name:   "a dashboardBaseURL on port 65540 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:65540"),
			want:   msgDashboardBaseURLPort,
		},
		{
			name:   "a dashboardBaseURL on port 65600 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:65600"),
			want:   msgDashboardBaseURLPort,
		},
		{
			name:   "a dashboardBaseURL on port 66000 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:66000"),
			want:   msgDashboardBaseURLPort,
		},
		{
			name:   "a dashboardBaseURL on port 70000 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:70000"),
			want:   msgDashboardBaseURLPort,
		},
		{
			name:   "a dashboardBaseURL on port 065536 is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:065536"),
			want:   msgDashboardBaseURLPort,
		},
		{name: "a dashboardBaseURL on port 65535 is accepted", mutate: dashboard("http://crewlet.nimbus.internal:65535")},
		{name: "a dashboardBaseURL on port 065535 is accepted", mutate: dashboard("http://crewlet.nimbus.internal:065535")},
		{
			name:   "a dashboardBaseURL on a port with leading zeros is accepted",
			mutate: dashboard("http://crewlet.nimbus.internal:08080"),
		},
		// A second ':' or a bracket outside an IPv6 address: refused by
		// Crewlet's URL parser and by the API server's on Kubernetes 1.36
		// and later, while the older Go net/url of 1.35 takes a second ':'
		// and a bracket after a name, and that of 1.34 brackets around
		// anything too, so the rule states the host's shape itself.
		{
			name:   "a dashboardBaseURL with two ports is refused",
			mutate: dashboard("http://crewlet.nimbus.internal:80:80"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL of an IPv6 address without brackets is refused",
			mutate: dashboard("http://::1:8080"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL whose IPv6 address does not start the host is refused",
			mutate: dashboard("http://x[::1]:8080"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL of an IPv4 address in brackets is refused",
			mutate: dashboard("http://[1.2.3.4]"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL of an IPv6 address with a group of five digits is refused",
			mutate: dashboard("http://[12345::1]:8080"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL of an IPv6 address with two :: is refused",
			mutate: dashboard("http://[1::2::3]"),
			want:   msgDashboardBaseURLHost,
		},
		{
			name:   "a dashboardBaseURL of an IPv4-mapped IPv6 address is accepted",
			mutate: dashboard("http://[::ffff:10.0.0.1]:8080"),
		},
		{
			name:   "a dashboardBaseURL of a full IPv6 address is accepted",
			mutate: dashboard("https://[2001:db8:0:0:0:0:0:1]"),
		},
		{
			name:   "a dashboardBaseURL whose host is outside ASCII is refused",
			mutate: dashboard("https://bücher.example"),
			want:   msgDashboardBaseURLASCII,
		},
		{name: "a dashboardBaseURL whose host is punycode is accepted", mutate: dashboard("https://xn--bcher-kva.example")},
		{
			name:   "a dashboardBaseURL whose host is percent-encoded is refused",
			mutate: dashboard("https://b%C3%BCcher.example"),
			want:   msgDashboardBaseURLPercent,
		},
		{
			name:   "a dashboardBaseURL with an IPv6 zone is refused",
			mutate: dashboard("http://[fe80::1%25eth0]:8080"),
			want:   msgDashboardBaseURLPercent,
		},
		{name: "a dashboardBaseURL of an IPv6 address is accepted", mutate: dashboard("http://[::1]:8080")},
		{name: "a dashboardBaseURL on localhost is accepted", mutate: dashboard("http://localhost:8080")},
		{name: "a dashboardBaseURL of four decimal octets is accepted", mutate: dashboard("http://127.0.0.1:8080")},
		// An octet of each branch of the rule's pattern: one and two
		// digits, 100 to 199, 200 to 249 and 250 to 255.
		{name: "a dashboardBaseURL of octets of one and two digits is accepted", mutate: dashboard("http://10.20.3.40:8080")},
		{name: "a dashboardBaseURL of octets of 100 to 249 is accepted", mutate: dashboard("http://199.200.249.100")},
		{name: "a dashboardBaseURL of octets of 250 to 255 is accepted", mutate: dashboard("http://255.250.255.251")},
		{
			name:   "a dashboardBaseURL of an octet of 256 is refused",
			mutate: dashboard("http://10.0.0.256"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL of an IPv4 short form is refused",
			mutate: dashboard("http://127.1:8080"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL of an IPv4 address as one number is refused",
			mutate: dashboard("http://2130706433"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL of a hexadecimal IPv4 address is refused",
			mutate: dashboard("http://0x7f.0.0.1"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			// A browser reads a leading zero as octal.
			name:   "a dashboardBaseURL of an IPv4 address with a leading zero is refused",
			mutate: dashboard("http://192.168.010.1"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL of an IPv4 address with a trailing dot is refused",
			mutate: dashboard("http://1.2.3.4./"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL whose host name ends in a number is refused",
			mutate: dashboard("https://crewlet.nimbus.123"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL whose host name ends in a hexadecimal number is refused",
			mutate: dashboard("https://crewlet.nimbus.0x1F:8443"),
			want:   msgDashboardBaseURLNumber,
		},
		{
			name:   "a dashboardBaseURL whose last label holds a letter is accepted",
			mutate: dashboard("https://crewlet.nimbus.1x"),
		},
		{
			name:   "a publicBaseURL on port 0 is refused",
			mutate: public("https://hooks.nimbus.example:0"),
			want:   msgPublicBaseURLPort,
		},
		{
			name:   "a publicBaseURL on port 99999 is refused",
			mutate: public("https://hooks.nimbus.example:99999/hooks"),
			want:   msgPublicBaseURLPort,
		},
		{name: "a publicBaseURL on port 8443 is accepted", mutate: public("https://hooks.nimbus.example:8443")},
		{
			name:   "a publicBaseURL with two ports is refused",
			mutate: public("https://hooks.nimbus.example:443:443"),
			want:   msgPublicBaseURLHost,
		},
		{
			name:   "a publicBaseURL of an IPv6 address without brackets is refused",
			mutate: public("https://2001:db8::1:8443"),
			want:   msgPublicBaseURLHost,
		},
		{
			name:   "a publicBaseURL with an empty IPv6 zone is refused",
			mutate: public("https://[fe80::1%25]:8443"),
			want:   msgPublicBaseURLHost,
		},
		{
			// No browser reaches the public address, and Crewlet takes a
			// zone there.
			name:   "a publicBaseURL with an IPv6 zone is accepted",
			mutate: public("https://[fe80::1%25eth0]:8443"),
		},
		{
			// No browser compares an origin with the public address, so
			// Crewlet takes a host there that it refuses as a browser's.
			name:   "a publicBaseURL of an IPv4 short form is accepted",
			mutate: public("https://127.1"),
		},
		{
			name:   "a publicBaseURL with a path is accepted",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "https://relay.nimbus.example/nimbus/" },
		},
		{
			name:   "a publicBaseURL with a query is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "https://relay.nimbus.example/?tenant=nimbus" },
			want:   msgPublicBaseURLQuery,
		},
		{
			name:   "a publicBaseURL with a fragment is refused",
			mutate: func(c *v1.Company) { c.Spec.Exposure.PublicBaseURL = "https://relay.nimbus.example/hooks#nimbus" },
			want:   msgPublicBaseURLQuery,
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
		{
			name:   "ANTHROPIC_API_KEY is refused",
			mutate: env(v1.EnvVar{Name: "ANTHROPIC_API_KEY", ValueFrom: platformURL}),
			want:   msgProviderKeyEnv,
		},
		{
			name:   "OPENAI_API_KEY is refused",
			mutate: env(v1.EnvVar{Name: "OPENAI_API_KEY", Value: new("sk-literal")}),
			want:   msgProviderKeyEnv,
		},
	})
}

// TestCompanyEngineConfigReservedKeys checks that engineConfig cannot set a
// block another field or object owns. Those blocks are the only ones that hold
// credentials, so the API server refusing them is what keeps a literal
// credential from ever being stored in a Company.
// TestCompanySeatPoolDefaults: a pool that says nothing else is a Shared
// pool of one pod.
func TestCompanySeatPoolDefaults(t *testing.T) {
	c := company(newNamespace(t), companyName)
	c.Spec.Topology.SeatPools = []v1.SeatPool{{Name: poolName}}
	if err := k8sClient.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got := &v1.Company{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(c), got); err != nil {
		t.Fatal(err)
	}
	pool := got.Spec.Topology.SeatPools[0]
	if pool.Mode != v1.SeatPoolShared || pool.Replicas == nil || *pool.Replicas != 1 {
		t.Errorf("a pool that sets nothing defaulted to mode %q, replicas %v; want Shared and 1", pool.Mode, pool.Replicas)
	}
}

func TestCompanyTopology(t *testing.T) {
	pool := func(name string, mode v1.SeatPoolMode, replicas int32) v1.SeatPool {
		return v1.SeatPool{Name: name, Mode: mode, Replicas: new(replicas)}
	}
	topology := func(defaultPool string, pools ...v1.SeatPool) func(*v1.Company) {
		return func(c *v1.Company) {
			c.Spec.Topology.DefaultSeatPool = defaultPool
			c.Spec.Topology.SeatPools = pools
		}
	}
	runCreateCases(t, company, []createCase[*v1.Company]{
		{
			name: "every kind of pool, with a Shared default, is accepted",
			mutate: func(c *v1.Company) {
				c.Spec.Topology.DefaultSeatPool = poolName
				c.Spec.Topology.SeatPools = []v1.SeatPool{
					{
						Name: poolName, Mode: v1.SeatPoolShared, Replicas: new(int32(3)), MaxConcurrentTurns: 4,
						TerminationGracePeriodSeconds: 600,
					},
					pool("isolated", v1.SeatPoolPerSeat, 2),
					pool("parked", v1.SeatPoolShared, 0),
				}
			},
		},
		{name: "the data pool as the default with no pools is accepted", mutate: topology(v1.DataPool)},
		{
			name:   "a pool named data is refused",
			mutate: topology(v1.DataPool, pool(v1.DataPool, v1.SeatPoolShared, 1)),
			want:   msgDataPoolName,
		},
		{
			name:   "a pool name that is not a DNS label is refused",
			mutate: topology(v1.DataPool, pool("General", v1.SeatPoolShared, 1)),
			want:   msgPoolName,
		},
		{
			name:   "a pool name of 21 characters is refused",
			mutate: topology(v1.DataPool, pool(strings.Repeat("p", 21), v1.SeatPoolShared, 1)),
			want:   "may not be more than 20 bytes",
		},
		{
			name:   "two pools of one name are refused",
			mutate: topology(v1.DataPool, pool(poolName, v1.SeatPoolShared, 1), pool(poolName, v1.SeatPoolPerSeat, 1)),
			want:   msgDuplicate,
		},
		{
			name: "nine pools are refused",
			mutate: func(c *v1.Company) {
				for i := range 9 {
					c.Spec.Topology.SeatPools = append(c.Spec.Topology.SeatPools, pool(fmt.Sprintf("p%d", i), v1.SeatPoolShared, 1))
				}
			},
			want: "Too many: 9: must have at most 8 items",
		},
		{
			name:   "a Shared pool of 101 pods is refused",
			mutate: topology(v1.DataPool, pool(poolName, v1.SeatPoolShared, 101)),
			want:   "should be less than or equal to 100",
		},
		{
			name:   "a PerSeat pool of 3 pods per seat is refused",
			mutate: topology(v1.DataPool, pool("isolated", v1.SeatPoolPerSeat, 3)),
			want:   msgPerSeatReplicas,
		},
		{
			name:   "a PerSeat pool of no pods per seat is refused",
			mutate: topology(v1.DataPool, pool("isolated", v1.SeatPoolPerSeat, 0)),
			want:   msgPerSeatReplicas,
		},
		{
			name:   "a default naming no pool is refused",
			mutate: topology(poolName, pool("other", v1.SeatPoolShared, 1)),
			want:   msgDefaultSeatPool,
		},
		{
			name:   "a default naming a pool with no pools at all is refused",
			mutate: topology(poolName),
			want:   msgDefaultSeatPool,
		},
		{
			name:   "a default naming a PerSeat pool is refused",
			mutate: topology("isolated", pool("isolated", v1.SeatPoolPerSeat, 1)),
			want:   msgDefaultSeatPool,
		},
	})
}

func TestCompanyEngineConfigReservedKeys(t *testing.T) {
	reserved := []string{
		"name", "mission", "vision", "policies", "timezone", "tracker", "knowledge",
		"token_budget", "providers", "integrations", "mcp_servers", "roles", "units",
	}
	const msgReservedKey = "engineConfig may not set name, mission"
	cases := make([]createCase[*v1.Company], 0, len(reserved)+4)
	cases = append(cases, createCase[*v1.Company]{
		name: "an unreserved key is accepted", mutate: engineConfig("learning", `{"enabled":false}`),
	})
	for _, key := range reserved {
		cases = append(cases, createCase[*v1.Company]{
			name:   "the key " + key + " is refused",
			mutate: engineConfig(key, `{"llm":{"a":{"api_keys":["sk-literal"]}}}`),
			want:   msgReservedKey,
		})
	}
	twice := append(engineConfigOf("learning"), engineConfigOf("learning")...)
	cases = append(cases,
		createCase[*v1.Company]{
			name:   "a scalar under a reserved key is refused too",
			mutate: engineConfig("name", `"Nimbus"`),
			want:   msgReservedKey,
		},
		createCase[*v1.Company]{
			name:   "a key twice is refused",
			mutate: func(c *v1.Company) { c.Spec.EngineConfig = twice },
			want:   msgDuplicate,
		},
		createCase[*v1.Company]{
			name:   "a key Crewlet cannot spell is refused",
			mutate: engineConfig("Turn-Engine", `{}`),
			want:   "spec.engineConfig[0].name",
		},
	)
	runCreateCases(t, company, cases)
}

func engineConfigOf(key string) []v1.EngineConfigEntry {
	return []v1.EngineConfigEntry{{Name: key, Value: &v1.EngineConfigValue{Raw: []byte(`{}`)}}}
}

// engineConfig sets engineConfig to the one key given, with the JSON value
// given.
func engineConfig(key, value string) func(*v1.Company) {
	return func(c *v1.Company) {
		c.Spec.EngineConfig = []v1.EngineConfigEntry{{Name: key, Value: &v1.EngineConfigValue{Raw: []byte(value)}}}
	}
}

// TestCompanyEngineConfigRoundTrips checks that a value comes back as it was
// written, whatever its JSON type.
func TestCompanyEngineConfigRoundTrips(t *testing.T) {
	c := company(newNamespace(t), companyName)
	c.Spec.EngineConfig = []v1.EngineConfigEntry{
		{Name: "turn_engine", Value: &v1.EngineConfigValue{Raw: []byte(`{"extension_enabled":true}`)}},
		{Name: "notification_rate_limit", Value: &v1.EngineConfigValue{Raw: []byte(`5`)}},
		{Name: "skill_variables", Value: &v1.EngineConfigValue{Raw: []byte(`{"region":"eu"}`)}},
		{Name: "a_list", Value: &v1.EngineConfigValue{Raw: []byte(`["a","b"]`)}},
		{Name: "a_string", Value: &v1.EngineConfigValue{Raw: []byte(`"hourly"`)}},
		{Name: "a_bool", Value: &v1.EngineConfigValue{Raw: []byte(`false`)}},
	}
	if err := k8sClient.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got := &v1.Company{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(c), got); err != nil {
		t.Fatal(err)
	}
	if len(got.Spec.EngineConfig) != len(c.Spec.EngineConfig) {
		t.Fatalf("engineConfig came back with %d entries, want %d: %v",
			len(got.Spec.EngineConfig), len(c.Spec.EngineConfig), got.Spec.EngineConfig)
	}
	for i, want := range c.Spec.EngineConfig {
		e := got.Spec.EngineConfig[i]
		if e.Name != want.Name || !bytes.Equal(e.Value.Raw, want.Value.Raw) {
			t.Errorf("engineConfig[%d] came back as %s: %s, want %s: %s", i, e.Name, e.Value.Raw, want.Name, want.Value.Raw)
		}
	}
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
			name:   "topology.data.replicas cannot become 3",
			mutate: func(c *v1.Company) { c.Spec.Topology.Data.Replicas = 3 },
			want:   msgThreeReplicas,
		},
	})
}
