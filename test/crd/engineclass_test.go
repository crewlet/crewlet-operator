package crd_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
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
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("14335Mi")) },
			want:   msgStorageFloorNATS,
		},
		{
			name:   "a 10Gi volume with files in the stream's bucket, the default, is refused",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("10Gi")) },
			want:   msgStorageFloorNATS,
		},
		{
			name: "a 10Gi volume with files in the stream's bucket, named, is refused",
			mutate: func(c *v1.EngineClass) {
				c.Spec.Data.Storage.Size = new(resource.MustParse("10Gi"))
				c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendNATS}
			},
			want: msgStorageFloorNATS,
		},
		{
			name:   "a storage size of exactly the floor with files in the stream's bucket is accepted",
			mutate: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("14Gi")) },
		},
		{
			name: "a storage size just under the floor with files in S3 is refused",
			mutate: func(c *v1.EngineClass) {
				s3("")(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("10239Mi"))
			},
			want: msgStorageFloor,
		},
		{
			name: "a storage size of exactly the floor with files in S3 is accepted",
			mutate: func(c *v1.EngineClass) {
				s3("")(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("10Gi"))
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
		// The webhooks' host is no browser's address, but the engine hands
		// it to sandboxes, whose URL parser reads a host whose last label is
		// a number as an IPv4 address.
		{
			name:   "a webhooks hostname ending in a number is refused",
			mutate: gatewayHost("{{uid}}.hooks.123"),
			want:   msgHostnameNumber,
		},
		{
			name:   "a webhooks hostname ending in a hexadecimal number is refused",
			mutate: gatewayHost("{{uid}}.hooks.0x1f"),
			want:   msgHostnameNumber,
		},
		{name: "a webhooks hostname ending in a label with a letter is accepted", mutate: gatewayHost("{{uid}}.hooks.123x")},
		{
			// Whether {{namespace}} is a number is the Company's to say, so
			// the operator decides it per Company (AddressRefused).
			name:   "a webhooks hostname ending in a variable is accepted",
			mutate: gatewayHost("{{uid}}.{{name}}.{{namespace}}"),
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

// msgPlatformSignIn is the start of the platform provider's rule over how its
// CLI signs in, which takes a key alone.
const msgPlatformSignIn = "a platform provider signs its CLI in with a key the class gives each tenant"

// TestPlatformLLMProviders checks the copy of the LLMProvider rules that
// PlatformLLMProvider declares for itself, and its own rule that a platform
// provider signs in with a key and never with a subscription's login.
func TestPlatformLLMProviders(t *testing.T) {
	key := v1.NamespacedSecretKeyReference{Namespace: platformKeyNS, Name: "platform-llm", Key: apiKeyKey}
	envFrom := func(name string) v1.NamespacedSecretEnvVar {
		return v1.NamespacedSecretEnvVar{Name: name, ValueFrom: v1.NamespacedSecretValueSource{SecretKeyRef: key}}
	}
	// platform gives the class one platform provider: Claude Code on a key,
	// changed by change.
	platform := func(change func(*v1.PlatformLLMProvider)) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) {
			p := v1.PlatformLLMProvider{
				Name: "platform", Agent: v1.CLIAgentClaudeCode, Model: claudeModel,
				Auth: v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: key},
			}
			if change != nil {
				change(&p)
			}
			c.Spec.PlatformLLMProviders = []v1.PlatformLLMProvider{p}
		}
	}
	// keyInEnv signs agent in with its model provider's key in env, as
	// Hermes, Pi and OpenCode sign in.
	keyInEnv := func(agent v1.CLIAgentName, name string) func(*v1.PlatformLLMProvider) {
		return func(p *v1.PlatformLLMProvider) {
			p.Agent = agent
			p.Model = modelViaOpenRouter
			p.Auth = v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthSubscription}
			p.Env = []v1.NamespacedSecretEnvVar{envFrom(name)}
		}
	}

	runCreateCases(t, engineClass, []createCase[*v1.EngineClass]{
		{name: "a platform provider is accepted", mutate: platform(nil)},
		{
			// A gateway Claude Code reads from its environment, on the
			// tenant's virtual key.
			name: "a platform provider through a gateway is accepted",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Env = []v1.NamespacedSecretEnvVar{envFrom(envBaseURL)}
				p.TimeoutSeconds = 120
				p.MaxConcurrent = 8
			}),
		},
		{
			name:   "a platform OpenCode provider on its model provider's key in env is accepted",
			mutate: platform(keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")),
		},
		{
			name:   "a platform Codex provider on a key is accepted",
			mutate: platform(func(p *v1.PlatformLLMProvider) { p.Agent = v1.CLIAgentCodex; p.Model = "gpt-5" }),
		},
		{
			name:   "a platform provider named default is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) { p.Name = "default" }),
			want:   "a platform provider may not be named default",
		},
		{
			name:   "a platform provider with a model of white space alone is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) { p.Model = "\u2028 " }),
			want:   msgBlankModel,
		},
		{
			name:   "a platform provider with no auth is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) { p.Auth = v1.PlatformLLMProviderAuth{} }),
			want:   "spec.platformLLMProviders[0].auth: Required value",
		},
		{
			// A subscription is one account's, which every tenant of the
			// class would share.
			name: "a platform provider on a subscription is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Auth = v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthSubscription}
			}),
			want: msgPlatformSignIn,
		},
		{
			name: "a platform provider on a subscription is reported at auth",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Auth = v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthSubscription}
			}),
			want: "spec.platformLLMProviders[0].auth: Invalid value",
		},
		{
			// A proxy is configuration, and signs the CLI in to nothing.
			name:   "a platform Hermes provider on a proxy alone in env is refused",
			mutate: platform(keyInEnv(v1.CLIAgentHermes, envProxy)),
			want:   msgPlatformSignIn,
		},
		{
			// Codex reads its key from its own variable, which auth owns.
			name:   "a platform Codex provider on a key in env is refused",
			mutate: platform(keyInEnv(v1.CLIAgentCodex, "OPENROUTER_API_KEY")),
			want:   msgPlatformSignIn,
		},
		{
			name: "auth mode APIKey for a platform Hermes provider is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				keyInEnv(v1.CLIAgentHermes, "OPENROUTER_API_KEY")(p)
				p.Auth = v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthAPIKey, APIKeySecretRef: key}
			}),
			want: msgCLIKeyAgent,
		},
		{
			name: "auth mode APIKey without a key is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Auth = v1.PlatformLLMProviderAuth{Mode: v1.CLIAgentAuthAPIKey}
			}),
			want: msgCLIKey,
		},
		{
			name: "a key outside auth mode APIKey is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
				p.Auth.APIKeySecretRef = key
			}),
			want: msgCLIKey,
		},
		{
			name: "a platform provider with its CLI's own key variable in env is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Env = []v1.NamespacedSecretEnvVar{envFrom(envAnthropicKey)}
			}),
			want: msgCLIOwnVariable,
		},
		{
			name: "a platform provider's own variable in env is reported at env",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Env = []v1.NamespacedSecretEnvVar{envFrom(envAnthropicKey)}
			}),
			want: "spec.platformLLMProviders[0].env: Invalid value",
		},
		{
			name: "a platform provider with an isolation variable in env is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				p.Env = []v1.NamespacedSecretEnvVar{envFrom("CLAUDE_CONFIG_DIR")}
			}),
			want: msgCLIIsolation,
		},
		{
			name: "a platform OpenCode model with no provider is refused",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
				p.Model = claudeModel
			}),
			want: msgOpenCodeModel,
		},
		{
			name: "a platform OpenCode model with no provider is reported at model",
			mutate: platform(func(p *v1.PlatformLLMProvider) {
				keyInEnv(v1.CLIAgentOpenCode, "OPENROUTER_API_KEY")(p)
				p.Model = "anthropic/ "
			}),
			want: "spec.platformLLMProviders[0].model: Invalid value",
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
		{name: "the boot's own bounds are accepted", mutate: window(1216)},
		{name: "a day is accepted", mutate: window(86400)},
		{
			name: "under the boot's own bounds is refused", mutate: window(1215),
			want: "should be greater than or equal to 1216",
		},
		{name: "over a day is refused", mutate: window(86401), want: "should be less than or equal to 86400"},
		{name: "the boot's own bounds with objects in S3 are accepted", mutate: inS3(1356)},
		{name: "under them with objects in S3 is refused", mutate: inS3(1355), want: msgStartupFloorS3},
		{name: "objects in S3 and no window is accepted", mutate: inS3(0)},
	})
}

// TestEngineClassStartupWindowFollowsTheObjects covers the floor on a class's
// startup window across a change of where it keeps files: a class at the
// stream's floor may not move its files to S3, whose floor is higher.
func TestEngineClassStartupWindowFollowsTheObjects(t *testing.T) {
	runUpdateCases(t, engineClass, []updateCase[*v1.EngineClass]{
		{
			name:   "a class at 1216 seconds moving its files to S3 is refused",
			before: func(c *v1.EngineClass) { c.Spec.Data.StartupSeconds = 1216 },
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
			name: "a 10Gi class moving its files from S3 into the stream's bucket is refused",
			before: func(c *v1.EngineClass) {
				inS3(c)
				c.Spec.Data.Storage.Size = new(resource.MustParse("10Gi"))
			},
			mutate: func(c *v1.EngineClass) { c.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendNATS} },
			want:   msgStorageFloorNATS,
		},
		{
			name:   "a 14Gi class moving its files from the stream's bucket to S3 is accepted",
			before: func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = new(resource.MustParse("14Gi")) },
			mutate: inS3,
		},
	})
}

// TestEngineClassStoredBelowTheFloor is what the API server does with a class
// stored below the floor on its data volume, as one written before the floor
// last rose is (Crewlet's code host's log, the eighth state log a fresh node
// reserves, moved it from 12Gi to 14Gi with the files in the stream's bucket
// and from 9Gi to 10Gi in S3): it keeps the class as it is, and holds a
// change to it to the rules as they are now, sparing what the change leaves
// as it was (the API server's validation ratcheting). With the files in the
// stream's bucket the floor is a rule on the whole spec, so any change to the
// spec is refused until the size reaches the floor, and one to the metadata
// alone is not; in S3 it is a rule on the size alone, so only a change of the
// size that leaves it below the floor is refused. DataStorage.size's doc and
// the installing and troubleshooting guides, which tell a platform to raise
// such a class, say so from this test.
func TestEngineClassStoredBelowTheFloor(t *testing.T) {
	s3Floor, natsFloor := installedVolumeFloors(t)
	under := func(floor resource.Quantity, by string) *resource.Quantity {
		q := floor.DeepCopy()
		q.Sub(resource.MustParse(by))
		return &q
	}
	prefix := strings.ToLower(namespacePrefix(newNamespace(t)))
	nats := engineClass("", prefix+"-stored-nats")
	nats.Spec.Data.Storage.Size = under(natsFloor, "1Gi")
	inS3 := engineClass("", prefix+"-stored-s3")
	inS3.Spec.Objects = v1.ObjectSettings{
		Backend: v1.ObjectBackendS3,
		S3:      v1.S3ObjectSettings{Region: tenantsRegion, Bucket: tenantsBucket},
	}
	inS3.Spec.Data.Storage.Size = under(s3Floor, "1Gi")
	storeUnderLowerFloors(t, nats, inS3)

	window := func(c *v1.EngineClass) { c.Spec.Data.StartupSeconds = 3600 }
	sized := func(size *resource.Quantity) func(*v1.EngineClass) {
		return func(c *v1.EngineClass) { c.Spec.Data.Storage.Size = size }
	}
	for _, c := range []struct {
		name   string
		class  *v1.EngineClass
		mutate func(*v1.EngineClass)
		want   string
	}{
		{
			name: "in the stream's bucket, a change to the spec is refused", class: nats,
			mutate: window, want: msgStorageFloorNATS,
		},
		{
			name: "in the stream's bucket, a change to the labels alone is accepted", class: nats,
			mutate: func(c *v1.EngineClass) { c.Labels = map[string]string{"example.com/tier": "team"} },
		},
		{name: "in the stream's bucket, the size raised to the floor is accepted", class: nats, mutate: sized(&natsFloor)},
		{name: "in S3, a change to the rest of the spec is accepted", class: inS3, mutate: window},
		{
			name: "in S3, a size still below the floor is refused", class: inS3,
			mutate: sized(under(s3Floor, "512Mi")), want: msgStorageFloor,
		},
		{name: "in S3, the size raised to the floor is accepted", class: inS3, mutate: sized(&s3Floor)},
	} {
		t.Run(c.name, func(t *testing.T) {
			stored := &v1.EngineClass{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(c.class), stored); err != nil {
				t.Fatal(err)
			}
			c.mutate(stored)
			// A dry run, so every case meets the class as it was stored.
			expect(t, k8sClient.Update(ctx, stored, client.DryRunAll), c.want)
		})
	}
}

// The EngineClass rules that floor the data volume, as controller-gen writes
// them into the CRD (internal/render's TestContractStreamFloor reads them the
// same way): the size's own rule, and the class's rule for a class whose
// objects are in the stream's bucket.
var (
	sizeFloorRule = regexp.MustCompile(`^!quantity\(string\(self\)\)\.isLessThan\(quantity\('(\d+Gi)'\)\)$`)
	natsFloorRule = regexp.MustCompile(`^\(has\(self\.objects\) && has\(self\.objects\.backend\) && ` +
		`self\.objects\.backend == 'S3'\) \|\| !has\(self\.data\.storage\.size\) \|\| ` +
		`!quantity\(string\(self\.data\.storage\.size\)\)\.isLessThan\(quantity\('(\d+Gi)'\)\)$`)
)

// engineClassCRD is the name of the EngineClass CRD.
const engineClassCRD = "engineclasses.crewlet.ai"

// volumeFloorRules are the two rules of an EngineClass CRD's schema that
// floor the data volume: the size's own, and the class's for the files in the
// stream's bucket, each with the floor it names. It fails the test unless
// each is there exactly once, in the shape the patterns read.
func volumeFloorRules(
	t *testing.T, schema *apiextensionsv1.JSONSchemaProps,
) (size, nats *apiextensionsv1.ValidationRule, s3Floor, natsFloor string) {
	t.Helper()
	spec := schema.Properties["spec"]
	sizeProps := spec.Properties["data"].Properties["storage"].Properties["size"]
	find := func(
		rules apiextensionsv1.ValidationRules, pattern *regexp.Regexp, where string,
	) (*apiextensionsv1.ValidationRule, string) {
		var found *apiextensionsv1.ValidationRule
		var floor string
		for i := range rules {
			if m := pattern.FindStringSubmatch(rules[i].Rule); m != nil {
				if found != nil {
					t.Fatalf("%s: two rules of the shape %s at %s", engineClassCRD, pattern, where)
				}
				found, floor = &rules[i], m[1]
			}
		}
		if found == nil {
			t.Fatalf("%s: no rule of the shape %s at %s", engineClassCRD, pattern, where)
		}
		return found, floor
	}
	size, s3Floor = find(sizeProps.XValidations, sizeFloorRule, "spec.data.storage.size")
	nats, natsFloor = find(spec.XValidations, natsFloorRule, "spec")
	return size, nats, s3Floor, natsFloor
}

// installedVolumeFloors are the floors on an EngineClass's data volume as the
// CRD envtest installed holds them: the size's own, which a class whose files
// are in S3 is held to, and the one for the files in the stream's bucket.
func installedVolumeFloors(t *testing.T) (s3, nats resource.Quantity) {
	t.Helper()
	for _, crd := range installedCRDs {
		if crd.Name == engineClassCRD {
			_, _, s3Floor, natsFloor := volumeFloorRules(t, crd.Spec.Versions[0].Schema.OpenAPIV3Schema)
			return resource.MustParse(s3Floor), resource.MustParse(natsFloor)
		}
	}
	t.Fatalf("no CRD %s was installed", engineClassCRD)
	return resource.Quantity{}, resource.Quantity{}
}

// storeUnderLowerFloors stores classes, each below a floor on the data
// volume, as an API server whose CRD had lower floors did: it installs the
// EngineClass CRD with both floors at 1Gi, creates the classes once the API
// server holds them to that, and installs the CRD's own rules again,
// returning once the API server holds them to those. No other test runs
// meanwhile, since none of this package runs in parallel with another.
func storeUnderLowerFloors(t *testing.T, classes ...*v1.EngineClass) {
	t.Helper()
	key := client.ObjectKey{Name: engineClassCRD}
	installed := &apiextensionsv1.CustomResourceDefinition{}
	if err := k8sClient.Get(ctx, key, installed); err != nil {
		t.Fatal(err)
	}
	own := installed.Spec.DeepCopy()
	lowered := installed.Spec.DeepCopy()
	size, nats, s3Floor, natsFloor := volumeFloorRules(t, lowered.Versions[0].Schema.OpenAPIV3Schema)
	size.Rule = strings.Replace(size.Rule, "quantity('"+s3Floor+"')", "quantity('1Gi')", 1)
	nats.Rule = strings.Replace(nats.Rule, "quantity('"+natsFloor+"')", "quantity('1Gi')", 1)
	install := func(spec *apiextensionsv1.CustomResourceDefinitionSpec) error {
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := k8sClient.Get(ctx, key, crd); err != nil {
				return err
			}
			crd.Spec = *spec.DeepCopy()
			return k8sClient.Update(ctx, crd)
		})
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			if err := install(own); err != nil {
				t.Errorf("installing the EngineClass CRD's own rules again: %v", err)
			}
		}
	})
	if err := install(lowered); err != nil {
		t.Fatalf("installing the EngineClass CRD with lower floors: %v", err)
	}
	// Asked of a name no class has, so that its create is judged by the
	// rules alone, never refused as one that exists.
	probe := classes[0].DeepCopy()
	probe.Name += "-probe"
	awaitAdmission(t, probe, true)
	for _, c := range classes {
		if err := k8sClient.Create(ctx, c); err != nil {
			t.Fatalf("storing EngineClass %s under the lower floors: %v", c.Name, err)
		}
	}
	if err := install(own); err != nil {
		t.Fatalf("installing the EngineClass CRD's own rules again: %v", err)
	}
	restored = true
	awaitAdmission(t, probe, false)
}

// awaitAdmission waits, for up to half a minute, until the API server's
// verdict on creating class is admitted, asking with dry runs: a CRD whose
// rules changed is held to them once the API server has rebuilt its
// validation, not when the update returns.
func awaitAdmission(t *testing.T, class *v1.EngineClass, admitted bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := k8sClient.Create(ctx, class.DeepCopy(), client.DryRunAll)
		if err != nil && !apierrors.IsInvalid(err) {
			t.Fatalf("a dry run of EngineClass %s: %v", class.Name, err)
		}
		if (err == nil) == admitted {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the API server's verdict on EngineClass %s is still %v after the CRD changed, want admitted %v",
				class.Name, err, admitted)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
