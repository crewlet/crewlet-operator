package controller

import (
	"slices"
	"strings"
	"testing"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// portForwarded is where a browser reaches an engine nothing routes to:
// kubectl port-forward's address of the engine's API port.
const portForwarded = "http://localhost:8080"

// TestEngineAddressesFollowWhereTheEngineIsReached pins what the data pod's
// crewlet.yaml tells the engine about where it is reached, for each source of
// each address: a browser at the Company's stated address, else at the class's
// API route, else through a port-forward; the public listener at the
// Company's public address, else inside the cluster; the route's origin
// allowed when the stated address is somewhere else; and the artifact origin
// at the Company's address, else the artifacts route's. routed is what
// routeScheme and the class's hostname make of the API's route: https behind
// a Gateway listener that terminates TLS, http behind an Ingress without it.
func TestEngineAddressesFollowWhereTheEngineIsReached(t *testing.T) {
	const (
		inCluster       = "http://nimbus-engine.tenant-nimbus.svc:8081"
		routedAPI       = "https://nimbus.api.example.com"
		routedArtifacts = "https://{artifact}.nimbus.crewletusercontent.example"
	)
	for _, c := range []struct {
		name                string
		stated, public      string
		routed              string
		external, publicURL string
		origins             []string
		// statedArtifacts, artifactsRouted and artifacts are the Company's,
		// the route's and the engine's artifact origin.
		statedArtifacts, artifactsRouted, artifacts string
	}{
		{
			name:     "a class that routes nothing",
			external: portForwarded, publicURL: inCluster,
		},
		{
			name:   "an API Gateway whose listener terminates TLS",
			routed: routedAPI, external: routedAPI, publicURL: inCluster,
		},
		{
			name:   "an API Ingress without TLS",
			routed: "http://nimbus.api.example.com", external: "http://nimbus.api.example.com", publicURL: inCluster,
		},
		{
			name:   "a stated address beside the route",
			stated: "https://crewlet.sso.example.com/nimbus/", routed: routedAPI,
			external: "https://crewlet.sso.example.com/nimbus", publicURL: inCluster,
			origins: []string{routedAPI},
		},
		{
			name:   "a stated address on the route's own origin",
			stated: "https://NIMBUS.api.example.com/crewlet", routed: routedAPI,
			external: "https://NIMBUS.api.example.com/crewlet", publicURL: inCluster,
		},
		{
			name:   "a stated address and no route",
			stated: "https://crewlet.sso.example.com", external: "https://crewlet.sso.example.com", publicURL: inCluster,
		},
		{
			name:   "webhooks routed through a Gateway",
			public: "https://nimbus.hooks.example.com", external: portForwarded,
			publicURL: "https://nimbus.hooks.example.com",
		},
		{
			name:     "artifacts routed over https",
			external: portForwarded, publicURL: inCluster,
			artifactsRouted: routedArtifacts, artifacts: routedArtifacts,
		},
		{
			name:     "a stated artifact origin ahead of the route",
			external: portForwarded, publicURL: inCluster,
			statedArtifacts: "https://artifacts.tunnel.example/", artifactsRouted: routedArtifacts,
			artifacts: "https://artifacts.tunnel.example",
		},
		{
			name:     "a stated port-forward artifact origin",
			external: portForwarded, publicURL: inCluster,
			statedArtifacts: "http://localhost:8082", artifacts: "http://localhost:8082",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			company := &crewletaiv1alpha1.Company{}
			company.Name, company.Namespace = "nimbus", "tenant-nimbus"
			company.Spec.Exposure.DashboardBaseURL = c.stated
			// What reconcileExposure has set by then: the vendors'
			// address, unset under webhooks None and under External
			// before the Company states one.
			company.Status.Endpoints.PublicBaseURL = c.public
			company.Spec.Exposure.ArtifactsBaseURL = c.statedArtifacts
			got := engineAddressesOf(company, c.routed, c.artifactsRouted)
			if got.external != c.external || got.public != c.publicURL || !slices.Equal(got.origins, c.origins) ||
				got.artifacts != c.artifacts {
				t.Fatalf("engineAddressesOf() = %+v, want external %s, public %s, origins %v, artifacts %q",
					got, c.external, c.publicURL, c.origins, c.artifacts)
			}
		})
	}
}

// hostnameWord appears in a refusal only where it names a class's template.
const hostnameWord = "hostname"

// routeOfNamespace is an API host name template whose last label a
// Company's namespace fills, and statedDashboard an address a Company states
// beside its class's route.
const (
	routeOfNamespace = "{{name}}.{{namespace}}"
	statedDashboard  = "https://crewlet.example.com"
)

// TestAddressRefusalNamesWhereTheAddressCameFrom pins RuntimeReady's message
// for an address Crewlet would refuse to start on: it names the Tier A key,
// the address and the engine's reason, and sends the reader to where
// engineAddressesOf took it from, the Company's own field or the class's API
// host name template; and names no source for an address the operator chose
// itself for an engine nothing routes to, nor for the webhooks route's.
func TestAddressRefusalNamesWhereTheAddressCameFrom(t *testing.T) {
	const reason = "has a host a browser reads as an IPv4 address"
	for _, c := range []struct {
		name                  string
		field                 string
		dashboard, public     string
		artifacts             string
		apiHost, webhooksHost string
		artifactsHost         string
		want                  []string
		wantNot               []string
	}{
		{
			name: "the Company's dashboard address", field: render.FieldExternalURL,
			dashboard: "http://127.1", apiHost: routeOfNamespace,
			want: []string{"spec.exposure.dashboardBaseURL: correct it"}, wantNot: []string{hostnameWord},
		},
		{
			name: "the API route's address", field: render.FieldExternalURL, apiHost: routeOfNamespace,
			want: []string{`the API route of EngineClass standard, its spec.exposure.api.hostname "` + routeOfNamespace + `"`},
		},
		{
			name: "the API route's origin", field: render.FieldAllowedOrigins,
			dashboard: statedDashboard, apiHost: routeOfNamespace,
			want: []string{`the API route of EngineClass standard, its spec.exposure.api.hostname`},
		},
		{
			name: "the Company's public address", field: render.FieldPublicURL,
			public: "https://hooks.example.com:0", webhooksHost: webhooksHostname,
			want: []string{"spec.exposure.publicBaseURL: correct it"}, wantNot: []string{hostnameWord},
		},
		{
			// No refusal names it, since Crewlet takes every address the
			// route's rules let through; nor is it sent to the API route.
			name: "the webhooks route's address", field: render.FieldPublicURL,
			apiHost: routeOfNamespace, webhooksHost: webhooksHostname,
			wantNot: []string{"It is"},
		},
		{
			name: "an address the operator chose", field: render.FieldExternalURL,
			wantNot: []string{"It is"},
		},
		{
			name: "the Company's artifact origin", field: render.FieldArtifactsURL,
			artifacts: "https://crewlet.example.com", apiHost: routeOfNamespace, artifactsHost: "{{uid}}.usercontent.example",
			want: []string{"spec.exposure.artifactsBaseURL: correct it"}, wantNot: []string{hostnameWord},
		},
		{
			// It is never sent to the API route, though the API route's
			// address is the one it meets.
			name: "the artifacts route's address", field: render.FieldArtifactsURL,
			apiHost: routeOfNamespace, artifactsHost: "{{uid}}.usercontent.example",
			want:    []string{`the artifacts route of EngineClass standard, its spec.exposure.artifacts.hostname "{{uid}}.usercontent.example"`},
			wantNot: []string{"api.hostname"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			company := &crewletaiv1alpha1.Company{}
			company.Name, company.Namespace = companyName, "4711"
			company.Spec.Exposure.DashboardBaseURL, company.Spec.Exposure.PublicBaseURL = c.dashboard, c.public
			company.Spec.Exposure.ArtifactsBaseURL = c.artifacts
			class := &crewletaiv1alpha1.EngineClass{}
			class.Name = "standard"
			class.Spec.Exposure.API.Hostname, class.Spec.Exposure.Webhooks.Hostname = c.apiHost, c.webhooksHost
			class.Spec.Exposure.Artifacts.Hostname = c.artifactsHost
			const address = "http://nimbus.4711"
			got := addressRefusalMessage(company, class, &render.AddressError{Field: c.field, Address: address, Reason: reason})
			for _, want := range append([]string{c.field + " set to " + address + ", which " + reason, "The pods are held as they are."}, c.want...) {
				if !strings.Contains(got, want) {
					t.Errorf("the message %q does not say %q", got, want)
				}
			}
			for _, not := range c.wantNot {
				if strings.Contains(got, not) {
					t.Errorf("the message %q says %q", got, not)
				}
			}
		})
	}
}
