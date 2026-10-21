package crd_test

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
		{"exposure.artifacts.mode", s.Exposure.Artifacts.Mode, v1.ArtifactExposureNone},
		{"networkPolicy.mode", s.NetworkPolicy.Mode, v1.NetworkPolicyIsolated},
		{"seatPools", s.SeatPools, v1.SeatPoolSettings{Policy: v1.SeatPoolsForbidden}},
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
				S3:      v1.S3ObjectSettings{Region: tenantsRegion, Bucket: tenantsBucket, Prefix: prefix},
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
			name:   "a storage size just under the floor with files in the stream's bucket is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("12287Mi")) },
			want:   msgStorageFloorNATS,
		},
		{
			name:   "a 9Gi volume with files in the stream's bucket, the default, is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("9Gi")) },
			want:   msgStorageFloorNATS,
		},
		{
			name: "a 9Gi volume with files in the stream's bucket, named, is refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Data.Storage.Size = new(resource.MustParse("9Gi"))
				c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendNATS}
			},
			want: msgStorageFloorNATS,
		},
		{
			name:   "a storage size of exactly the floor with files in the stream's bucket is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("12Gi")) },
		},
		{
			name: "a storage size just under the floor with files in S3 is refused",
			mutate: func(c *v1.EngineClass) {
				s3("")(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("9215Mi"))
			},
			want: msgStorageFloor,
		},
		{
			name: "a storage size of exactly the floor with files in S3 is accepted",
			mutate: func(c *v1.EngineClass) {
				s3("")(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("9Gi"))
			},
		},
		{
			name:   "a storage size in bytes is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("17179869184")) },
		},
		// A volume of any size past the floor is admitted: the render takes
		// no more of it than Crewlet's ceiling on the stream (64 TiB), and
		// computes what it takes exactly for a size no int64 holds
		// (internal/render's TestContractStreamCeiling).
		{
			name:   "a storage size past what the engine's stream ceiling is three quarters of is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("1Pi")) },
		},
		{
			name:   "a storage size no int64 holds is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("1e30")) },
		},
		{
			name:   "a storage size of 16Ei, which no int64 holds, is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("16Ei")) },
		},
		{
			name:   "a storage size one past the largest int64 is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("9223372036854775808")) },
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
		{
			name: "an S3 region of white space alone is refused",
			mutate: func(c *v1.EngineClass) {
				s3("{{uid}}/")(c)
				c.Spec.Objects.S3.Region = " \t"
			},
			want: msgS3RegionBlank,
		},
		{
			name: "an S3 bucket of white space alone is refused",
			mutate: func(c *v1.EngineClass) {
				s3("{{uid}}/")(c)
				c.Spec.Objects.S3.Bucket = "\t \t"
			},
			want: msgS3BucketBlank,
		},
		{
			// Crewlet trims every string of its Tier A before it reads one.
			name: "an S3 region and bucket with white space around them are accepted",
			mutate: func(c *v1.EngineClass) {
				s3("{{uid}}/")(c)
				c.Spec.Objects.S3.Region, c.Spec.Objects.S3.Bucket = " eu-central-1 ", " crewlet-tenants "
			},
		},
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
		// What Crewlet's S3 client refuses at start, before it sends a
		// request, whatever the bucket; test/crd's
		// TestS3SettingsFollowTheEngine holds each to an engine's start.
		{name: "an S3 region with a space is refused", mutate: s3Set(s3Region("eu central 1")), want: msgS3Region},
		{name: "an S3 region with an underscore is refused", mutate: s3Set(s3Region("eu_central_1")), want: msgS3Region},
		{name: "an S3 region ending in '.' is refused", mutate: s3Set(s3Region("eu-central-1.")), want: msgS3Region},
		{name: "an S3 region starting with '-' is refused", mutate: s3Set(s3Region("-eu")), want: msgS3Region},
		{name: "an S3 region ending in '-' is refused", mutate: s3Set(s3Region("eu-")), want: msgS3Region},
		{
			name:   "an S3 region of a label of 64 is refused",
			mutate: s3Set(s3Region(strings.Repeat("a", 64))),
			want:   msgS3Region,
		},
		{name: "an S3 region outside ASCII is refused", mutate: s3Set(s3Region("eü-central-1")), want: msgS3Region},
		{name: "an S3 region in upper case is accepted", mutate: s3Set(s3Region("EU-CENTRAL-1"))},
		{name: "an S3 region of a label of 63 is accepted", mutate: s3Set(s3Region(strings.Repeat("a", 63)))},
		{name: "R2's S3 region auto is accepted", mutate: s3Set(s3Region("auto"))},
		{
			name:   "an S3 region holding a '.' without path style is refused",
			mutate: s3Set(s3Region("eu.central")),
			want:   msgS3RegionDot,
		},
		{
			name:   "an S3 region holding a '.' with path style is accepted",
			mutate: s3Set(s3Region("eu.central"), func(s *v1.S3ObjectSettings) { s.PathStyle = new(true) }),
		},
		{
			name:   "an S3 FIPS region beside an endpoint is refused",
			mutate: s3Set(s3Region("fips-us-east-1"), s3Endpoint("https://minio.storage.example")),
			want:   msgS3RegionFIPSEndpoint,
		},
		{
			name:   "an S3 region holding -fips beside an endpoint is refused",
			mutate: s3Set(s3Region("us-fipsy-1"), s3Endpoint("https://minio.storage.example")),
			want:   msgS3RegionFIPSEndpoint,
		},
		{name: "an S3 FIPS region on Amazon's endpoint is accepted", mutate: s3Set(s3Region("us-east-1-fips"))},
		{name: "an S3 FIPS region of AWS GovCloud is accepted", mutate: s3Set(s3Region("fips-us-gov-west-1"))},
		{
			name:   "an S3 FIPS region in China is refused",
			mutate: s3Set(s3Region("fips-cn-north-1")),
			want:   msgS3RegionFIPSChina,
		},
		{name: "an S3 region in China is accepted", mutate: s3Set(s3Region("cn-north-1"))},
		{name: "an S3 bucket named by an ARN is refused", mutate: s3Set(s3Bucket("arn:aws:s3:::crewlet")), want: msgS3Bucket},
		{name: "an S3 directory bucket is refused", mutate: s3Set(s3Bucket("crewlet--use1-az4--x-s3")), want: msgS3Bucket},
		{
			name:   "an S3 Local Zone directory bucket is refused",
			mutate: s3Set(s3Bucket("crewlet--use1-az4--xa-s3")),
			want:   msgS3Bucket,
		},
		{name: "an S3 Outposts alias is refused", mutate: s3Set(s3Bucket("crewlet--op-s3")), want: msgS3Bucket},
		{name: "an S3 bucket holding '.' is accepted", mutate: s3Set(s3Bucket("crewlet.tenants"))},
		{
			name:   "an S3 endpoint with a query is refused",
			mutate: s3Set(s3Endpoint("https://minio.storage.example/?tenant=nimbus")),
			want:   msgS3EndpointQuery,
		},
		{name: "an S3 endpoint with an empty query is accepted", mutate: s3Set(s3Endpoint("https://minio.storage.example?"))},
		{
			name:   "an S3 endpoint on port 0 is refused",
			mutate: s3Set(s3Endpoint("http://minio.storage:0")),
			want:   msgS3EndpointPort,
		},
		{
			name:   "an S3 endpoint on port 65536 is refused",
			mutate: s3Set(s3Endpoint("http://minio.storage:65536")),
			want:   msgS3EndpointPort,
		},
		{name: "an S3 endpoint on port 9000 is accepted", mutate: s3Set(s3Endpoint("http://minio.storage:9000"))},
		{
			name:   "an S3 endpoint with two ports is refused",
			mutate: s3Set(s3Endpoint("http://minio.storage:9000:9000")),
			want:   msgS3EndpointHost,
		},
		{
			// Crewlet's S3 client decodes the zone and parses the address
			// again, which fails on what the decoding left.
			name:   "an S3 endpoint whose IPv6 zone is percent-encoded is refused",
			mutate: s3Set(s3Endpoint("https://[fe80::1%25e%20th0]:9000")),
			want:   msgS3EndpointHost,
		},
		{name: "an S3 endpoint with an IPv6 zone is accepted", mutate: s3Set(s3Endpoint("https://[fe80::1%25eth0]:9000"))},
		{
			name:   "an S3 endpoint with an upper-case scheme is accepted",
			mutate: s3Set(s3Endpoint("HTTPS://minio.storage.example")),
		},
	})
}

// s3Set is an EngineClass mutation to S3, of region eu-west-1 and bucket
// crewlet-files, with each of set applied to its settings.
func s3Set(set ...func(*v1.S3ObjectSettings)) func(*v1.EngineClass) {
	return func(c *v1.EngineClass) {
		s3 := v1.S3ObjectSettings{Region: "eu-west-1", Bucket: "crewlet-files"}
		for _, f := range set {
			f(&s3)
		}
		c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendS3, S3: s3}
	}
}

// s3Region, s3Bucket and s3Endpoint set one of an S3ObjectSettings' fields.
func s3Region(r string) func(*v1.S3ObjectSettings) {
	return func(s *v1.S3ObjectSettings) { s.Region = r }
}
func s3Bucket(b string) func(*v1.S3ObjectSettings) {
	return func(s *v1.S3ObjectSettings) { s.Bucket = b }
}
func s3Endpoint(e string) func(*v1.S3ObjectSettings) {
	return func(s *v1.S3ObjectSettings) { s.Endpoint = e }
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
	proxies := func(blocks ...string) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Exposure.TrustedProxies = blocks }
	}
	artifacts := func(a v1.ArtifactExposure) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Exposure.Artifacts = a }
	}
	artifactsGatewayHost := func(hostname string) func(*v1.EngineClass) {
		return artifacts(v1.ArtifactExposure{
			Mode: v1.ArtifactExposureGateway, Gateway: gatewayExposure(), Hostname: hostname,
		})
	}
	const (
		apiHost           = "{{uid}}.api.internal.example.com"
		artifactsHost     = "{{uid}}.crewletusercontent.example"
		msgTrustedProxies = "trustedProxies must be canonical CIDRs"
		msgArtifactsHost  = "artifacts.hostname must differ from api.hostname and webhooks.hostname, " +
			"and neither may be under it"
	)

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
			name: "External webhooks without forwarders and no network policy are accepted",
			mutate: func(c *v1.EngineClass) {
				c.Spec.NetworkPolicy = v1.NetworkPolicySettings{Mode: v1.NetworkPolicyNone}
				c.Spec.Exposure.Webhooks = v1.WebhookExposure{Mode: v1.WebhookExposureExternal}
			},
		},
		{
			// Under Isolated the public port admits only where webhooks
			// arrive from; with no forwarder named it admits nobody.
			name:   "Isolated External webhooks without forwarders are refused",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureExternal}),
			want:   "networkPolicy Isolated with External webhooks needs webhooks.external.forwarders",
		},
		{
			name: "forwarders with no network policy are refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.NetworkPolicy = v1.NetworkPolicySettings{Mode: v1.NetworkPolicyNone}
				c.Spec.Exposure.Webhooks = v1.WebhookExposure{Mode: v1.WebhookExposureExternal, External: v1.ExternalExposure{
					Forwarders: []v1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}},
				}}
			},
			want: "webhooks.external.forwarders apply to networkPolicy mode Isolated only",
		},
		{
			name: "External webhooks with their forwarders are accepted",
			mutate: webhooks(v1.WebhookExposure{Mode: v1.WebhookExposureExternal, External: v1.ExternalExposure{
				Forwarders: []v1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/metadata.name": "tunnel"},
				}}},
			}}),
		},
		{
			name: "external with mode Gateway is refused",
			mutate: webhooks(v1.WebhookExposure{
				Mode: v1.WebhookExposureGateway, Gateway: gatewayExposure(), Hostname: webhookHost,
				External: v1.ExternalExposure{Forwarders: []v1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}}},
			}),
			want: "external is allowed with mode External only",
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
			name: "annotations for the engine Service are accepted",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Service.Annotations = map[string]string{
					"cloud.google.com/backend-config": `{"ports": {"public": "crewlet-public"}}`,
				}
			},
		},
		{
			name: "a Gateway API route beside the webhooks' on another host is accepted",
			mutate: func(c *v1.EngineClass) {
				gatewayHost(webhookHost)(c)
				apiGatewayHost(apiHost)(c)
			},
		},
		{
			// Both routes take every path of their host: on one host,
			// which listener answers would be the Gateway's choice.
			name: "a Gateway API route on the webhooks' host is refused",
			mutate: func(c *v1.EngineClass) {
				gatewayHost(webhookHost)(c)
				apiGatewayHost(webhookHost)(c)
			},
			want: "webhooks.hostname and api.hostname must differ",
		},
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
		// The API's host is a browser's address, which Crewlet refuses to
		// start on when its last label is a number.
		{
			name:   "an API hostname ending in a number is refused",
			mutate: apiGatewayHost("{{uid}}.123"),
			want:   msgHostnameNumber,
		},
		{
			name:   "an API hostname ending in a hexadecimal number is refused",
			mutate: apiGatewayHost("{{uid}}.0x1f"),
			want:   msgHostnameNumber,
		},
		{name: "an API hostname ending in 0x is refused", mutate: apiGatewayHost("{{uid}}.0x"), want: msgHostnameNumber},
		{
			name:   "an API hostname ending in an IPv4 address is refused",
			mutate: apiGatewayHost("{{uid}}.10.0.0.1"),
			want:   msgHostnameNumber,
		},
		{name: "an API hostname ending in a label with a letter is accepted", mutate: apiGatewayHost("{{uid}}.123x")},
		{name: "an API hostname ending in a hexadecimal word is accepted", mutate: apiGatewayHost("{{uid}}.0xg")},
		{
			// Whether {{namespace}} is a number is the Company's to say,
			// so the operator decides it per Company (AddressRefused).
			name:   "an API hostname ending in a variable is accepted",
			mutate: apiGatewayHost("{{uid}}.{{name}}.{{namespace}}"),
		},
		{
			// The webhooks' host is no browser's address.
			name:   "a webhooks hostname ending in a number is accepted",
			mutate: gatewayHost("{{uid}}.hooks.123"),
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
		// The artifacts' route, which serves https alone.
		{name: "a Gateway artifacts route is accepted", mutate: artifactsGatewayHost(artifactsHost)},
		{
			name: "an artifacts route of one shared host is accepted",
			mutate: func(c *v1.EngineClass) {
				artifactsGatewayHost(artifactsHost)(c)
				c.Spec.Exposure.Artifacts.Origins = v1.ArtifactOriginsShared
			},
		},
		{
			name: "an Ingress artifacts route with TLS is accepted",
			mutate: artifacts(v1.ArtifactExposure{
				Mode: v1.ArtifactExposureIngress, Ingress: ingressWithTLS("{{name}}-artifacts-tls"), Hostname: artifactsHost,
			}),
		},
		{
			// Crewlet refuses an artifact origin over plain http on a
			// routable host.
			name: "an Ingress artifacts route without TLS is refused",
			mutate: artifacts(v1.ArtifactExposure{
				Mode: v1.ArtifactExposureIngress, Hostname: artifactsHost,
				Ingress: &v1.IngressExposure{ClassName: ingressClass, ControllerNamespaces: []string{"ingress-nginx"}},
			}),
			want: "an Ingress for artifacts needs ingress.tls",
		},
		{
			name: "an Isolated Ingress artifacts route without controllerNamespaces is refused",
			mutate: func(c *v1.EngineClass) {
				ingress := ingressWithTLS("{{name}}-artifacts-tls")
				ingress.ControllerNamespaces = nil
				c.Spec.Exposure.Artifacts = v1.ArtifactExposure{
					Mode: v1.ArtifactExposureIngress, Ingress: ingress, Hostname: artifactsHost,
				}
			},
			want: msgIngressControllerNamespaces,
		},
		{
			name:   "an artifacts Gateway without gateway is refused",
			mutate: artifacts(v1.ArtifactExposure{Mode: v1.ArtifactExposureGateway, Hostname: artifactsHost}),
			want:   msgGatewayMode,
		},
		{
			name:   "an artifacts hostname with mode None is refused",
			mutate: artifacts(v1.ArtifactExposure{Mode: v1.ArtifactExposureNone, Hostname: artifactsHost}),
			want:   msgHostnameMode,
		},
		{
			name:   "artifacts origins with mode None are refused",
			mutate: artifacts(v1.ArtifactExposure{Mode: v1.ArtifactExposureNone, Origins: v1.ArtifactOriginsShared}),
			want:   "origins is allowed with modes Gateway and Ingress only",
		},
		{
			name:   "an artifacts hostname ending in a number is refused",
			mutate: artifactsGatewayHost("{{uid}}.123"),
			want:   "hostname may not end in a label that is a number",
		},
		{
			name:   "an artifacts hostname shared by two companies is refused",
			mutate: artifactsGatewayHost("{{name}}-{{namespace}}.crewletusercontent.example"),
			want:   "hostname must contain {{uid}}",
		},
		{
			name: "an artifacts route on the API's host is refused",
			mutate: func(c *v1.EngineClass) {
				apiGatewayHost(apiHost)(c)
				artifactsGatewayHost(apiHost)(c)
			},
			want: msgArtifactsHost,
		},
		{
			// A cookie scoped to the artifacts' host would reach the API.
			name: "an API host under the artifacts' host is refused",
			mutate: func(c *v1.EngineClass) {
				apiGatewayHost("api." + artifactsHost)(c)
				artifactsGatewayHost(artifactsHost)(c)
			},
			want: msgArtifactsHost,
		},
		{
			name: "a webhooks host under the artifacts' host is refused",
			mutate: func(c *v1.EngineClass) {
				gatewayHost("hooks." + artifactsHost)(c)
				artifactsGatewayHost(artifactsHost)(c)
			},
			want: msgArtifactsHost,
		},
		{
			name: "artifacts on a domain of their own beside the API and the webhooks are accepted",
			mutate: func(c *v1.EngineClass) {
				gatewayHost(webhookHost)(c)
				apiGatewayHost(apiHost)(c)
				artifactsGatewayHost(artifactsHost)(c)
			},
		},
		{name: "trusted proxies are accepted", mutate: proxies("10.244.0.0/16", "192.0.2.7/32", "fd00:10:244::/56")},
		{
			// Crewlet reads a bare address as a host or as a typo for a
			// block, and guesses neither.
			name:   "a trusted proxy that is an address, not a block, is refused",
			mutate: proxies("10.0.0.7"),
			want:   msgTrustedProxies,
		},
		{name: "a trusted proxy of the whole address space is refused", mutate: proxies("::/0"), want: msgTrustedProxies},
		{name: "a trusted proxy with host bits set is refused", mutate: proxies("10.0.0.7/8"), want: msgTrustedProxies},
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

func TestEngineClassSeatPools(t *testing.T) {
	pools := func(p v1.SeatPoolSettings) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.SeatPools = p }
	}
	scratch := func(size string) func(*v1.EngineClass) {
		return pools(v1.SeatPoolSettings{
			Policy: v1.SeatPoolsAllowed, MaxPods: 6,
			Defaults: v1.SeatPoolDefaults{ScratchSizeLimit: new(resource.MustParse(size))},
		})
	}
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{
			name: "every setting under Allowed is accepted",
			mutate: pools(v1.SeatPoolSettings{
				Policy:       v1.SeatPoolsAllowed,
				AllowPerSeat: new(true),
				MaxPods:      6,
				Defaults: v1.SeatPoolDefaults{
					MaxConcurrentTurns: 4,
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
					},
					TerminationGracePeriodSeconds: 600,
					ScratchSizeLimit:              new(resource.MustParse("4Gi")),
				},
			}),
		},
		{name: "Forbidden alone is accepted", mutate: pools(v1.SeatPoolSettings{Policy: v1.SeatPoolsForbidden})},
		{
			name:   "allowPerSeat under Forbidden is refused",
			mutate: pools(v1.SeatPoolSettings{Policy: v1.SeatPoolsForbidden, AllowPerSeat: new(true)}),
			want:   msgPoolsAllowedOnly,
		},
		{
			name:   "maxPods under Forbidden is refused",
			mutate: pools(v1.SeatPoolSettings{Policy: v1.SeatPoolsForbidden, MaxPods: 6}),
			want:   msgPoolsAllowedOnly,
		},
		{
			// The policy defaults to Forbidden, so a class that sets only
			// a limit has pools it does not allow.
			name:   "defaults with no policy are refused",
			mutate: pools(v1.SeatPoolSettings{Defaults: v1.SeatPoolDefaults{MaxConcurrentTurns: 4}}),
			want:   msgPoolsAllowedOnly,
		},
		{
			name:   "a negative maxPods is refused",
			mutate: pools(v1.SeatPoolSettings{Policy: v1.SeatPoolsAllowed, MaxPods: -1}),
			want:   "should be greater than or equal to 1",
		},
		{
			name:   "Allowed without maxPods is refused",
			mutate: pools(v1.SeatPoolSettings{Policy: v1.SeatPoolsAllowed, AllowPerSeat: new(true)}),
			want:   msgPoolsCapped,
		},
		{name: "a scratch limit of 256Mi is accepted", mutate: scratch("256Mi")},
		{name: "a scratch limit under 256Mi is refused", mutate: scratch("255Mi"), want: msgScratchFloor},
		{
			name: "a grace period under 30 seconds is refused",
			mutate: pools(v1.SeatPoolSettings{
				Policy: v1.SeatPoolsAllowed, MaxPods: 6,
				Defaults: v1.SeatPoolDefaults{TerminationGracePeriodSeconds: 10},
			}),
			want: "should be greater than or equal to 30",
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
			name: "a platform provider named default is refused",
			mutate: func(c *v1.EngineClass) {
				platform(anthropic, true)(c)
				c.Spec.PlatformLLMProviders[0].Name = "default"
			},
			want: "a platform provider may not be named default",
		},
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
			name:   "a platform provider with a model of white space alone is refused",
			mutate: platform(with(anthropic, func(s *v1.LLMProviderSettings) { s.Model = "\u2028 " }), true),
			want:   msgBlankModel,
		},
		{
			name:   "a platform CLIAgent provider is refused",
			mutate: platform(v1.LLMProviderSettings{Type: v1.LLMProviderCLIAgent, Model: "sonnet"}, true),
			want:   "a platform model provider speaks a model API",
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

// TestEngineClassStartupWindow: a class may name its data pods' startup window,
// from every bound Crewlet puts on a data pod's boot laid end to end, which
// is longer when the class keeps files in S3, whose bucket every boot probes,
// up to a day, past which a recovery is a disk to look at.
func TestEngineClassStartupWindow(t *testing.T) {
	window := func(seconds int32) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Data.StartupSeconds = seconds }
	}
	inS3 := func(seconds int32) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) {
			c.Spec.Objects = v1.ObjectSettings{
				Backend: v1.ObjectBackendS3,
				S3:      v1.S3ObjectSettings{Region: tenantsRegion, Bucket: tenantsBucket},
			}
			c.Spec.Data.StartupSeconds = seconds
		}
	}
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "the boot's own bounds are accepted", mutate: window(1656)},
		{name: "a day is accepted", mutate: window(86400)},
		{
			name: "under the boot's own bounds is refused", mutate: window(1655),
			want: "should be greater than or equal to 1656",
		},
		{name: "over a day is refused", mutate: window(86401), want: "should be less than or equal to 86400"},
		{name: "the boot's own bounds with objects in S3 are accepted", mutate: inS3(1796)},
		{name: "under them with objects in S3 is refused", mutate: inS3(1795), want: msgStartupFloorS3},
		{name: "objects in S3 and no window is accepted", mutate: inS3(0)},
	})
}

// TestEngineClassStartupWindowFollowsTheObjects covers the floor on a class's
// startup window across a change of where it keeps files: a class at the
// stream's floor may not move its files to S3, whose floor is higher.
func TestEngineClassStartupWindowFollowsTheObjects(t *testing.T) {
	runUpdateCases(t, engineClass, []updateCase[*v1.EngineClass]{
		{
			name:   "a class at 1656 seconds moving its files to S3 is refused",
			before: func(c *v1.EngineClass) { c.Spec.Data.StartupSeconds = 1656 },
			mutate: func(c *v1.EngineClass) {
				c.Spec.Objects = v1.ObjectSettings{
					Backend: v1.ObjectBackendS3,
					S3:      v1.S3ObjectSettings{Region: tenantsRegion, Bucket: tenantsBucket},
				}
			},
			want: msgStartupFloorS3,
		},
	})
}

// TestEngineClassSignIn: a class may let people sign in with a password alone,
// and acknowledge it, but the acknowledgement applies to an optional second
// factor only: it lifts nothing else, and set alone it has the engine warn of
// an insecure posture at every start about nothing. secondFactor takes the
// API's PascalCase values, never Crewlet's own spelling.
func TestEngineClassSignIn(t *testing.T) {
	signIn := func(s v1.SignInSettings) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.SignIn = s }
	}
	const msgAcceptInsecure = "acceptInsecure applies to secondFactor Optional only"
	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "a class that says nothing about sign-in is accepted"},
		{name: "Required is accepted", mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorRequired})},
		{name: "Optional is accepted", mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorOptional})},
		{
			name:   "Optional acknowledged is accepted",
			mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorOptional, AcceptInsecure: new(true)}),
		},
		{
			name:   "Optional with the acknowledgement false is accepted",
			mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorOptional, AcceptInsecure: new(false)}),
		},
		{
			name:   "Required with the acknowledgement false is accepted",
			mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorRequired, AcceptInsecure: new(false)}),
		},
		{
			name:   "Required acknowledged is refused",
			mutate: signIn(v1.SignInSettings{SecondFactor: v1.SecondFactorRequired, AcceptInsecure: new(true)}),
			want:   msgAcceptInsecure,
		},
		{
			// secondFactor defaults to Required, which the rule then reads.
			name:   "the acknowledgement alone is refused",
			mutate: signIn(v1.SignInSettings{AcceptInsecure: new(true)}),
			want:   msgAcceptInsecure,
		},
		{
			name:   "Crewlet's own spelling is refused",
			mutate: signIn(v1.SignInSettings{SecondFactor: "optional"}),
			want:   `spec.signIn.secondFactor: Unsupported value: "optional"`,
		},
	})
}

// TestEngineClassSignInDefaults: a signIn that names no secondFactor requires
// one, and says so, so a reader of the class sees what its companies require.
func TestEngineClassSignInDefaults(t *testing.T) {
	class := engineClass("", namespacePrefix(newNamespace(t))+"-sign-in")
	class.Spec.SignIn = v1.SignInSettings{AcceptInsecure: new(false)}
	if err := k8sClient.Create(ctx, class); err != nil {
		t.Fatal(err)
	}
	got := &v1.EngineClass{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(class), got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.SignIn.SecondFactor != v1.SecondFactorRequired {
		t.Errorf("signIn.secondFactor defaulted to %q, want %q", got.Spec.SignIn.SecondFactor, v1.SecondFactorRequired)
	}
}

// TestEngineClassFloorFollowsTheObjects covers the floor on a class's data
// volume across a change of where it keeps files: a class at the S3 floor
// may not move its files into the stream's bucket, whose floor is higher,
// and one at that floor may move them to S3.
func TestEngineClassFloorFollowsTheObjects(t *testing.T) {
	inS3 := func(c *v1.EngineClass) {
		c.Spec.Objects = v1.ObjectSettings{
			Backend: v1.ObjectBackendS3,
			S3:      v1.S3ObjectSettings{Region: tenantsRegion, Bucket: tenantsBucket},
		}
	}
	runUpdateCases(t, engineClass, []updateCase[*v1.EngineClass]{
		{
			name: "a 9Gi class moving its files from S3 into the stream's bucket is refused",
			before: func(c *v1.EngineClass) {
				inS3(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("9Gi"))
			},
			mutate: func(c *v1.EngineClass) { c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendNATS} },
			want:   msgStorageFloorNATS,
		},
		{
			name:   "a 12Gi class moving its files from the stream's bucket to S3 is accepted",
			before: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("12Gi")) },
			mutate: inS3,
		},
	})
}
