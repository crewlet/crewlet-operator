package controller

import (
	"slices"
	"testing"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// portForwarded is where a browser reaches an engine nothing routes to:
// kubectl port-forward's address of the engine's API port.
const portForwarded = "http://localhost:8080"

// TestEngineAddressesFollowWhereTheEngineIsReached pins what the data pod's
// crewlet.yaml tells the engine about where it is reached, for each source of
// each address: a browser at the Company's stated address, else at the class's
// API route, else through a port-forward; the public listener at the
// Company's public address, else inside the cluster; and the route's origin
// allowed when the stated address is somewhere else. routed is what
// routeScheme and the class's hostname make of the API's route: https behind
// a Gateway listener that terminates TLS, http behind an Ingress without it.
func TestEngineAddressesFollowWhereTheEngineIsReached(t *testing.T) {
	const (
		inCluster = "http://nimbus-engine.tenant-nimbus.svc:8081"
		routedAPI = "https://nimbus.api.example.com"
	)
	for _, c := range []struct {
		name                string
		stated, public      string
		routed              string
		external, publicURL string
		origins             []string
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
	} {
		t.Run(c.name, func(t *testing.T) {
			company := &crewletaiv1alpha1.Company{}
			company.Name, company.Namespace = "nimbus", "tenant-nimbus"
			company.Spec.Exposure.DashboardBaseURL = c.stated
			// What reconcileExposure has set by then: the vendors'
			// address, unset under webhooks None and under External
			// before the Company states one.
			company.Status.Endpoints.PublicBaseURL = c.public
			got := engineAddressesOf(company, c.routed)
			if got.external != c.external || got.public != c.publicURL || !slices.Equal(got.origins, c.origins) {
				t.Fatalf("engineAddressesOf() = %+v, want external %s, public %s, origins %v", got, c.external, c.publicURL, c.origins)
			}
		})
	}
}
