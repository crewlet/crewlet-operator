package controller

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1ac "sigs.k8s.io/gateway-api/applyconfiguration/apis/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// routedPath is the one path prefix a route sends to the engine: every path,
// to the one port the route is for.
//
// The webhooks' route reaches the engine's public listener (render.PublicPort)
// and nothing else, and that listener is where the line between public and
// private is drawn: Crewlet serves there only the routes outside parties call
// without an operator credential (/webhooks/, /otlp/{token}, /mcp/{token}),
// decided from one list of its own, and answers any other path as one
// nothing serves. Repeating those prefixes in the route would add no
// boundary the listener does not already hold, since the admin API is not on
// that port to be reached by any path; it would add a second list, which a
// Crewlet release that serves a new public route outgrows silently, its
// callers refused at the proxy as if they had the wrong address. The API's
// route reaches the API port, which serves no public route.
const routedPath = "/"

// The kinds of the objects the operator writes for a Company.
var (
	secretGVK         = corev1.SchemeGroupVersion.WithKind("Secret")
	serviceGVK        = corev1.SchemeGroupVersion.WithKind("Service")
	serviceAccountGVK = corev1.SchemeGroupVersion.WithKind("ServiceAccount")
	statefulSetGVK    = appsv1.SchemeGroupVersion.WithKind("StatefulSet")
	ingressGVK        = networkingv1.SchemeGroupVersion.WithKind("Ingress")
	networkPolicyGVK  = networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy")
	// The seat pools' disruption budgets.
	podDisruptionBudgetGVK = policyv1.SchemeGroupVersion.WithKind("PodDisruptionBudget")
	httpRouteGVK           = schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version, Kind: "HTTPRoute"}
)

// GatewayAPIInstalled reports whether the cluster serves Gateway API
// HTTPRoutes at v1. The operator runs on clusters without the Gateway API;
// there it neither watches nor writes HTTPRoutes, and a class that routes
// through Gateways is reported as not exposed.
func GatewayAPIInstalled(mapper meta.RESTMapper) (bool, error) {
	_, err := mapper.RESTMapping(httpRouteGVK.GroupKind(), httpRouteGVK.Version)
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("looking up the Gateway API's HTTPRoute kind: %w", err)
	}
	return true, nil
}

// route is one route a class asks for: the webhooks', the API's or the
// artifacts'.
type route struct {
	// what is "webhooks", "API" or "artifacts", for messages.
	what      string
	name      string
	component string
	// port is the engine Service's port the route sends to.
	port    int32
	gateway *crewletaiv1alpha1.GatewayExposure
	ingress *crewletaiv1alpha1.IngressExposure
	// hostname is the expanded host name template, or the wildcard under it
	// for an origin per artifact.
	hostname string
}

// routeState is what a route's controller reported about it.
type routeState struct {
	accepted bool
	reason   string
	message  string
}

// engineAddresses is where the engine is reached, as the data pod's
// crewlet.yaml tells it: Crewlet's api.external_url, api.public.external_url,
// api.artifacts.external_url and api.auth.allowed_origins.
type engineAddresses struct {
	// external is where a browser reaches the engine: the Company's
	// status.endpoints.dashboardBaseURL.
	external string
	// public is where the public listener is reached: the Company's
	// status.endpoints.publicBaseURL, or the listener's address inside the
	// cluster.
	public string
	// origins are the browser origins beside external's own.
	origins []string
	// artifacts is the Company's status.endpoints.artifactsBaseURL; empty
	// opens no artifact listener.
	artifacts string
}

// reconcileExposure writes the routes the class asks for, deletes those it no
// longer asks for, sets the Company's public, dashboard and artifacts base
// URLs and exposed API endpoint, and returns the Exposed condition and the
// addresses the engine is told it is reached at.
//
// Crewlet requires a node that serves the API to know where a browser reaches
// it (api.external_url) and, while its public listener is open, where that is
// reached (api.public.external_url); it reads both only at start, so a change
// of either restarts the data pod. ADR-0006 records how each is chosen when
// nothing names one, and ADR-0007 the artifact origin's address.
func (r *CompanyReconciler) reconcileExposure(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (metav1.Condition, engineAddresses, error) {
	exposure := class.Spec.Exposure
	webhooks, api, artifacts := webhooksRouteOf(c, &exposure.Webhooks), apiRouteOf(c, &exposure.API),
		artifactsRouteOf(c, &exposure.Artifacts)

	// The public base URL does not wait for the route to be accepted: it is
	// fixed by the class and the Company alone, and a platform reads it to
	// configure vendor apps before anything is reachable. Under mode None
	// nothing is public, and a publicBaseURL the Company states is unused.
	// A trailing '/' is dropped, as Crewlet drops it from the base it builds
	// its own webhook addresses on: the addresses published from this one
	// must be the engine's, without a '//' before webhooks/.
	stated := strings.TrimRight(c.Spec.Exposure.PublicBaseURL, "/")
	switch exposure.Webhooks.Mode {
	case crewletaiv1alpha1.WebhookExposureGateway, crewletaiv1alpha1.WebhookExposureIngress:
		c.Status.Endpoints.PublicBaseURL = cmp.Or(stated, "https://"+webhooks.hostname)
	case crewletaiv1alpha1.WebhookExposureExternal:
		c.Status.Endpoints.PublicBaseURL = stated
	case crewletaiv1alpha1.WebhookExposureNone:
		c.Status.Endpoints.PublicBaseURL = ""
	}

	webhooksState, err := r.reconcileRoute(ctx, c, &webhooks)
	if err != nil {
		return metav1.Condition{}, engineAddresses{}, err
	}
	apiState, err := r.reconcileRoute(ctx, c, &api)
	if err != nil {
		return metav1.Condition{}, engineAddresses{}, err
	}
	artifactsState, err := r.reconcileRoute(ctx, c, &artifacts)
	if err != nil {
		return metav1.Condition{}, engineAddresses{}, err
	}
	// The API's scheme, like the public base URL, is fixed by the class
	// (and the Gateways it names) and not by whether the route has been
	// accepted, so the dashboard's address does not wait for acceptance
	// either: it is the engine's api.external_url, which the engine reads
	// only at start, and an address that followed acceptance would make
	// every flip of it (a Gateway controller re-evaluating its routes, an
	// Ingress's published address briefly cleared) a restart of the data
	// pod, and would never be known behind an Ingress controller that
	// publishes no address. exposedAPI keeps waiting: it says the route
	// works.
	apiScheme, err := r.routeScheme(ctx, c, &api)
	if err != nil {
		return metav1.Condition{}, engineAddresses{}, err
	}
	c.Status.Endpoints.ExposedAPI = ""
	var routed string
	if apiScheme != "" {
		routed = apiScheme + "://" + api.hostname
		if apiState != nil && apiState.accepted {
			c.Status.Endpoints.ExposedAPI = routed
		}
	}
	artifactsScheme, err := r.routeScheme(ctx, c, &artifacts)
	if err != nil {
		return metav1.Condition{}, engineAddresses{}, err
	}
	addresses := engineAddressesOf(c, routed, artifactsAddressOf(c, &exposure.Artifacts, artifactsScheme))
	c.Status.Endpoints.DashboardBaseURL = addresses.external
	c.Status.Endpoints.ArtifactsBaseURL = addresses.artifacts
	var states []routeState
	for _, state := range []*routeState{webhooksState, apiState, artifactsState} {
		if state != nil {
			states = append(states, *state)
		}
	}

	exposed := metav1.Condition{Type: crewletaiv1alpha1.ConditionExposed}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureExternal && c.Spec.Exposure.PublicBaseURL == "" {
		exposed.Status = metav1.ConditionFalse
		exposed.Reason = crewletaiv1alpha1.ReasonPublicBaseURLMissing
		exposed.Message = fmt.Sprintf("EngineClass %s has webhooks forwarded from outside the operator (webhooks.mode External): "+
			"set spec.exposure.publicBaseURL to the address vendors deliver to.", class.Name)
		return exposed, addresses, nil
	}
	for _, state := range states {
		if !state.accepted {
			exposed.Status, exposed.Reason, exposed.Message = metav1.ConditionFalse, state.reason, state.message
			return exposed, addresses, nil
		}
	}
	if artifactsScheme != "" && artifactsScheme != schemeHTTPS && c.Spec.Exposure.ArtifactsBaseURL == "" {
		exposed.Status, exposed.Reason = metav1.ConditionFalse, crewletaiv1alpha1.ReasonRouteNotHTTPS
		exposed.Message = fmt.Sprintf("EngineClass %s routes the artifacts through Gateways, and no listener HTTPRoute %s attaches "+
			"to is HTTPS: Crewlet refuses an artifact origin reached over plain http on a routable host, so the data pod opens "+
			"no artifact origin, and the dashboard opens no HTML page or design, until one is. Give the Gateway an HTTPS "+
			"listener for %s.", class.Name, artifacts.name, artifacts.hostname)
		return exposed, addresses, nil
	}
	exposed.Status = metav1.ConditionTrue
	messages := make([]string, 0, len(states)+1)
	if len(states) == 0 {
		exposed.Reason = crewletaiv1alpha1.ReasonNoRoutes
		messages = append(messages, fmt.Sprintf("EngineClass %s routes nothing to the engine from outside the cluster.", class.Name))
	} else {
		exposed.Reason = crewletaiv1alpha1.ReasonRouteAccepted
		for _, state := range states {
			messages = append(messages, state.message)
		}
	}
	if routed == "" && c.Spec.Exposure.DashboardBaseURL == "" {
		messages = append(messages, fmt.Sprintf("The class routes no API and the Company states no spec.exposure.dashboardBaseURL, "+
			"so the engine is told people reach it at %s, the address kubectl port-forward serves it at: every link it gives a "+
			"person, an invitation's included, leads there.", addresses.external))
	}
	if addresses.artifacts == "" {
		messages = append(messages, "The class routes no artifact origin and the Company states no "+
			"spec.exposure.artifactsBaseURL, so the data pod opens none, and the dashboard opens no HTML page or design: "+
			"route the class's spec.exposure.artifacts, or state the address, http://localhost:8082 for a browser that "+
			"port-forwards the engine Service's artifacts port.")
	}
	exposed.Message = strings.Join(messages, " ")
	return exposed, addresses, nil
}

// webhooksRouteOf is the route a class's webhooks exposure asks for.
func webhooksRouteOf(c *crewletaiv1alpha1.Company, w *crewletaiv1alpha1.WebhookExposure) route {
	rt := route{what: "webhooks", name: webhooksRouteName(c), component: componentWebhooks, port: render.PublicPort}
	switch w.Mode {
	case crewletaiv1alpha1.WebhookExposureGateway:
		rt.gateway = &w.Gateway
	case crewletaiv1alpha1.WebhookExposureIngress:
		rt.ingress = w.Ingress
	case crewletaiv1alpha1.WebhookExposureExternal, crewletaiv1alpha1.WebhookExposureNone:
	}
	if w.Hostname != "" {
		rt.hostname = render.ExpandTemplate(w.Hostname, c)
	}
	return rt
}

// apiRouteOf is the route a class's API exposure asks for.
func apiRouteOf(c *crewletaiv1alpha1.Company, a *crewletaiv1alpha1.APIExposure) route {
	rt := route{what: "API", name: apiRouteName(c), component: componentAPI, port: render.APIPort}
	switch a.Mode {
	case crewletaiv1alpha1.APIExposureGateway:
		rt.gateway = &a.Gateway
	case crewletaiv1alpha1.APIExposureIngress:
		rt.ingress = a.Ingress
	case crewletaiv1alpha1.APIExposureClusterInternal:
	}
	if a.Hostname != "" {
		rt.hostname = render.ExpandTemplate(a.Hostname, c)
	}
	return rt
}

// artifactsRouteOf is the route a class's artifacts exposure asks for: under
// an origin per artifact, the wildcard that matches every artifact's host.
func artifactsRouteOf(c *crewletaiv1alpha1.Company, a *crewletaiv1alpha1.ArtifactExposure) route {
	rt := route{what: "artifacts", name: artifactsRouteName(c), component: componentArtifacts, port: render.ArtifactsPort}
	switch a.Mode {
	case crewletaiv1alpha1.ArtifactExposureGateway:
		rt.gateway = &a.Gateway
	case crewletaiv1alpha1.ArtifactExposureIngress:
		rt.ingress = a.Ingress
	case crewletaiv1alpha1.ArtifactExposureNone:
	}
	if a.Hostname != "" {
		rt.hostname = render.ExpandTemplate(a.Hostname, c)
		if perArtifact(a) {
			rt.hostname = "*." + rt.hostname
		}
	}
	return rt
}

// artifactsAddressOf is the artifact origin's address through the class's
// route, empty unless the route serves https, which Crewlet requires.
func artifactsAddressOf(c *crewletaiv1alpha1.Company, a *crewletaiv1alpha1.ArtifactExposure, scheme string) string {
	if scheme != schemeHTTPS || a.Hostname == "" {
		return ""
	}
	host := render.ExpandTemplate(a.Hostname, c)
	if perArtifact(a) {
		host = render.ArtifactPlaceholder + "." + host
	}
	return schemeHTTPS + "://" + host
}

// The schemes a route serves its host on (routeScheme).
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// portForwardURL is where a browser reaches the engine through
// `kubectl port-forward svc/<company>-engine <port>`, the one address every
// company has: the engine's API port on localhost. Crewlet suggests the same
// form when it refuses a node without api.external_url, reads localhost as
// loopback, so neither its plain-http advisory nor its insecure-posture rules
// fire, and accepts a port-forwarded browser's writes, whose origin it is.
func portForwardURL() string {
	return fmt.Sprintf("http://localhost:%d", render.APIPort)
}

// perArtifact reports whether every artifact gets a host of its own, origins'
// default.
func perArtifact(a *crewletaiv1alpha1.ArtifactExposure) bool {
	return a.Origins != crewletaiv1alpha1.ArtifactOriginsShared
}

// publicListenerURL is the address of the engine's public listener inside the
// cluster: the engine Service's public port.
func publicListenerURL(c *crewletaiv1alpha1.Company) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", engineServiceName(c), c.Namespace, render.PublicPort)
}

// engineAddressesOf is where the engine is reached, given the address of the
// class's API route (scheme and host, or empty when it routes none) and of its
// artifacts route.
//
// A browser reaches it at the Company's spec.exposure.dashboardBaseURL, else
// at the route, else through a port-forward. The public listener is reached
// at the Company's status.endpoints.publicBaseURL, else at its address inside
// the cluster: Crewlet requires an address while the listener is open, and
// the data pod opens it whatever the class routes, so the one address it
// truly has is the in-cluster one, which an in-cluster forwarder can use and
// no vendor can. status.endpoints.publicBaseURL stays unset then, since it is
// the address a vendor is given.
//
// When the Company's address is not the route's, the route is still an
// address the engine is reached on: Crewlet accepts a browser's write only
// from api.external_url's origin or an api.auth.allowed_origins entry, so the
// route's origin is allowed, or a person signing in through the route would
// be refused every write.
//
// The artifact origin is the Company's spec.exposure.artifactsBaseURL, else
// the artifacts route, else none (ADR-0007).
func engineAddressesOf(c *crewletaiv1alpha1.Company, routed, artifactsRouted string) engineAddresses {
	stated := strings.TrimRight(c.Spec.Exposure.DashboardBaseURL, "/")
	a := engineAddresses{
		external:  cmp.Or(stated, routed, portForwardURL()),
		public:    cmp.Or(c.Status.Endpoints.PublicBaseURL, publicListenerURL(c)),
		artifacts: cmp.Or(strings.TrimRight(c.Spec.Exposure.ArtifactsBaseURL, "/"), artifactsRouted),
	}
	if stated != "" && routed != "" && !strings.EqualFold(originOf(stated), routed) {
		a.origins = []string{routed}
	}
	return a
}

// addressRefusalMessage is RuntimeReady's message when Crewlet would refuse to
// start on an address engineAddressesOf chose, or one boxURLs hands the
// sandboxes: which it is, where it came from and what to change. The API
// refuses the addresses a Company states that Crewlet would refuse, so the
// one that reaches here is in practice a route's host, which the class's
// template makes of the Company's namespace and name: the API route's, or the
// webhooks route's where the engine hands it to sandboxes. A Company's own
// address reaches here only when the API admitted it before it had the rule
// that refuses it, or on an API server whose URL parser takes a shape
// Crewlet's refuses that no rule covers.
func addressRefusalMessage(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, refused *render.AddressError) string {
	// Where the address came from, as engineAddressesOf chose it. The
	// addresses the operator chooses for an engine nothing routes to
	// (localhost, the public listener's in-cluster name) are ones Crewlet
	// takes, so they need no sentence; and so is the webhooks route's as
	// api.public.external_url, https:// and a host name the class's rules
	// hold to a DNS name, with no port, query or fragment, which is all
	// Crewlet asks of that key. Handed to sandboxes, it is held to their URL
	// parser too, which a host whose last label is a number fails.
	box := refused.Field == render.EnvBridgeURL || refused.Field == render.EnvReceiverURL
	var where string
	switch {
	case box && c.Spec.Exposure.PublicBaseURL != "":
		where = " It is the Company's spec.exposure.publicBaseURL, which the engine hands its sandboxes: correct it."
	case box && class.Spec.Exposure.Webhooks.Hostname != "":
		where = fmt.Sprintf(" It is the address of the webhooks route of EngineClass %s, its spec.exposure.webhooks.hostname "+
			"%q expanded for this Company, which the engine hands its sandboxes. A template whose last label holds "+
			"{{namespace}} or {{name}} ends in a number for some Companies: one whose namespace is a number (4711, or 0x "+
			"and hex digits such as 0x1f), or whose namespace or name makes one with the template's own characters beside "+
			"it (0x{{name}} for a Company named cafe). End the template in a domain of your own, such as "+
			"{{uid}}.hooks.example.com, or run the Company in a namespace of another name.",
			class.Name, class.Spec.Exposure.Webhooks.Hostname)
	case refused.Field == render.FieldArtifactsURL && c.Spec.Exposure.ArtifactsBaseURL != "":
		where = " It is the Company's spec.exposure.artifactsBaseURL: correct it, giving artifacts a host of their own."
	case refused.Field == render.FieldArtifactsURL && class.Spec.Exposure.Artifacts.Hostname != "":
		where = fmt.Sprintf(" It is the address of the artifacts route of EngineClass %s, its spec.exposure.artifacts.hostname "+
			"%q expanded for this Company, and it meets the dashboard's address or the public one this Company states: "+
			"give artifacts a domain of their own.", class.Name, class.Spec.Exposure.Artifacts.Hostname)
	case refused.Field == render.FieldPublicURL && c.Spec.Exposure.PublicBaseURL != "":
		where = " It is the Company's spec.exposure.publicBaseURL: correct it."
	case refused.Field == render.FieldExternalURL && c.Spec.Exposure.DashboardBaseURL != "":
		where = " It is the Company's spec.exposure.dashboardBaseURL: correct it."
	case !box && refused.Field != render.FieldPublicURL && class.Spec.Exposure.API.Hostname != "":
		where = fmt.Sprintf(" It is the address of the API route of EngineClass %s, its spec.exposure.api.hostname %q "+
			"expanded for this Company. A template whose last label holds {{namespace}} or {{name}} ends in a number "+
			"for some Companies: one whose namespace is a number (4711, or 0x and hex digits such as 0x1f), or whose "+
			"namespace or name makes one with the template's own characters beside it (0x{{name}} for a Company named "+
			"cafe). End the template in a domain of your own, such as {{name}}.{{namespace}}.crewlet.example.com, or run "+
			"the Company in a namespace of another name.", class.Name, class.Spec.Exposure.API.Hostname)
	}
	return fmt.Sprintf("Crewlet refuses to start with %s set to %s, which %s.%s The pods are held as they are.",
		refused.Field, refused.Address, refused.Reason, where)
}

// originOf is a URL's origin, scheme://host[:port], as a browser sends it, or
// the URL itself when it does not parse (the API refused such a URL).
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host
}

// reconcileRoute writes one route as an HTTPRoute or an Ingress and deletes
// the other kind. It returns nil when the class asks for no such route.
func (r *CompanyReconciler) reconcileRoute(ctx context.Context, c *crewletaiv1alpha1.Company, rt *route) (*routeState, error) {
	if rt.ingress == nil {
		if err := r.deleteControlled(ctx, c, ingressGVK, rt.name); err != nil {
			return nil, err
		}
	}
	if rt.gateway == nil && r.GatewayAPI {
		if err := r.deleteControlled(ctx, c, httpRouteGVK, rt.name); err != nil {
			return nil, err
		}
	}
	var state *routeState
	var err error
	switch {
	case rt.gateway != nil:
		if !r.GatewayAPI {
			return &routeState{
				reason: crewletaiv1alpha1.ReasonGatewayAPINotInstalled,
				message: fmt.Sprintf("The class routes the %s through Gateways, and this cluster has no Gateway API HTTPRoute kind: "+
					"install the Gateway API and restart the operator.", rt.what),
			}, nil
		}
		state, err = r.applyHTTPRoute(ctx, c, rt)
	case rt.ingress != nil:
		state, err = r.applyIngress(ctx, c, rt)
	default:
		return nil, nil
	}
	// A route someone else owns is a fact about the routes, so it is
	// reported as the Company not being exposed, and the engine still runs.
	if conflict, ok := errors.AsType[*nameConflictError](err); ok {
		return &routeState{reason: crewletaiv1alpha1.ReasonNameConflict, message: conflict.message()}, nil
	}
	return state, err
}

// applyHTTPRoute writes a route as a Gateway API HTTPRoute and reads whether
// every Gateway it names accepted it.
func (r *CompanyReconciler) applyHTTPRoute(ctx context.Context, c *crewletaiv1alpha1.Company, rt *route) (*routeState, error) {
	rule := gatewayv1ac.HTTPRouteRule().
		WithBackendRefs(gatewayv1ac.HTTPBackendRef().
			WithName(gatewayv1.ObjectName(engineServiceName(c))).
			WithPort(rt.port)).
		WithMatches(gatewayv1ac.HTTPRouteMatch().
			WithPath(gatewayv1ac.HTTPPathMatch().WithType(gatewayv1.PathMatchPathPrefix).WithValue(routedPath)))
	spec := gatewayv1ac.HTTPRouteSpec().
		WithHostnames(gatewayv1.Hostname(rt.hostname)).
		WithRules(rule)
	for _, p := range rt.gateway.ParentRefs {
		ref := gatewayv1ac.ParentReference().WithName(gatewayv1.ObjectName(p.Name))
		if p.Namespace != "" {
			ref.WithNamespace(gatewayv1.Namespace(p.Namespace))
		}
		if p.SectionName != "" {
			ref.WithSectionName(gatewayv1.SectionName(p.SectionName))
		}
		spec.WithParentRefs(ref)
	}
	httpRoute := gatewayv1ac.HTTPRoute(rt.name, c.Namespace).
		WithLabels(labelsFor(c, rt.component)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(spec)
	if err := r.apply(ctx, c, httpRouteGVK, rt.name, httpRoute); err != nil {
		return nil, err
	}

	for _, p := range rt.gateway.ParentRefs {
		gateway := types.NamespacedName{Namespace: cmp.Or(p.Namespace, c.Namespace), Name: p.Name}
		accepted, message := parentAccepted(httpRoute, gateway, p.SectionName)
		if !accepted {
			return &routeState{
				reason: crewletaiv1alpha1.ReasonRouteNotAccepted,
				message: fmt.Sprintf("Gateway %s has not accepted HTTPRoute %s for the %s yet%s.",
					gateway, rt.name, rt.what, message),
			}, nil
		}
	}
	return &routeState{
		accepted: true,
		message:  fmt.Sprintf("HTTPRoute %s routes the %s from %s.", rt.name, rt.what, rt.hostname),
	}, nil
}

// parentAccepted reads whether the Gateway (or its listener) accepted a
// route, from the route's status as the apply returned it. A Gateway's
// controller adds an entry per parent it handles; the message carries its
// reason when it refused.
func parentAccepted(route *gatewayv1ac.HTTPRouteApplyConfiguration, gateway types.NamespacedName, section string) (bool, string) {
	if route.Status == nil {
		return false, ""
	}
	for _, parent := range route.Status.Parents {
		ref := parent.ParentRef
		if ref == nil || value(ref.Name) != gatewayv1.ObjectName(gateway.Name) ||
			string(cmp.Or(value(ref.Namespace), gatewayv1.Namespace(*route.Namespace))) != gateway.Namespace ||
			string(value(ref.SectionName)) != section {
			continue
		}
		for _, condition := range parent.Conditions {
			if value(condition.Type) != string(gatewayv1.RouteConditionAccepted) {
				continue
			}
			if value(condition.Status) == metav1.ConditionTrue {
				return true, ""
			}
			return false, fmt.Sprintf(": %s: %s", value(condition.Reason), value(condition.Message))
		}
	}
	return false, ""
}

// routeScheme is the scheme a route serves its host name on: https when an
// Ingress terminates TLS (tls.secretName) or a Gateway listener the route can
// attach to is HTTPS, and http otherwise. It is empty when the class asks
// for no such route, or for one through Gateways on a cluster without the
// Gateway API, where the route cannot be written at all.
func (r *CompanyReconciler) routeScheme(ctx context.Context, c *crewletaiv1alpha1.Company, rt *route) (string, error) {
	switch {
	case rt.ingress != nil:
		if rt.ingress.TLS.SecretName != "" {
			return schemeHTTPS, nil
		}
		return schemeHTTP, nil
	case rt.gateway != nil && r.GatewayAPI:
		https, err := r.gatewaysServeHTTPS(ctx, c, rt.gateway)
		if err != nil || !https {
			return schemeHTTP, err
		}
		return schemeHTTPS, nil
	default:
		return "", nil
	}
}

// gatewaysServeHTTPS reports whether a route's Gateways terminate TLS for
// it: whether a listener the route can attach to is HTTPS. A Gateway that
// does not exist yet terminates nothing. It reads the Gateways uncached: the
// operator's cache holds Gateways as metadata only (for the EngineClass
// reconciler), and only a class that routes the API through Gateways has
// their listeners read, once per reconcile of each of its Companies.
func (r *CompanyReconciler) gatewaysServeHTTPS(
	ctx context.Context, c *crewletaiv1alpha1.Company, exposure *crewletaiv1alpha1.GatewayExposure,
) (bool, error) {
	for _, p := range exposure.ParentRefs {
		gateway := &gatewayv1.Gateway{}
		key := types.NamespacedName{Namespace: cmp.Or(p.Namespace, c.Namespace), Name: p.Name}
		if err := r.APIReader.Get(ctx, key, gateway); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, fmt.Errorf("reading Gateway %s: %w", key, err)
		}
		for _, l := range gateway.Spec.Listeners {
			if (p.SectionName == "" || string(l.Name) == p.SectionName) && l.Protocol == gatewayv1.HTTPSProtocolType {
				return true, nil
			}
		}
	}
	return false, nil
}

// applyIngress writes a route as an Ingress. An Ingress has no acceptance of
// its own; it counts as accepted once its controller published an address
// for it.
func (r *CompanyReconciler) applyIngress(ctx context.Context, c *crewletaiv1alpha1.Company, rt *route) (*routeState, error) {
	paths := networkingv1ac.HTTPIngressRuleValue().
		WithPaths(networkingv1ac.HTTPIngressPath().
			WithPath(routedPath).
			WithPathType(networkingv1.PathTypePrefix).
			WithBackend(networkingv1ac.IngressBackend().
				WithService(networkingv1ac.IngressServiceBackend().
					WithName(engineServiceName(c)).
					WithPort(networkingv1ac.ServiceBackendPort().WithNumber(rt.port)))))
	spec := networkingv1ac.IngressSpec().
		WithRules(networkingv1ac.IngressRule().
			WithHost(rt.hostname).
			WithHTTP(paths))
	if rt.ingress.ClassName != "" {
		spec.WithIngressClassName(rt.ingress.ClassName)
	}
	if rt.ingress.TLS.SecretName != "" {
		spec.WithTLS(networkingv1ac.IngressTLS().
			WithHosts(rt.hostname).
			WithSecretName(render.ExpandTemplate(rt.ingress.TLS.SecretName, c)))
	}
	ingress := networkingv1ac.Ingress(rt.name, c.Namespace).
		WithLabels(labelsFor(c, rt.component)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(spec)
	if len(rt.ingress.Annotations) > 0 {
		ingress.WithAnnotations(rt.ingress.Annotations)
	}
	if err := r.apply(ctx, c, ingressGVK, rt.name, ingress); err != nil {
		return nil, err
	}
	if ingress.Status == nil || ingress.Status.LoadBalancer == nil || len(ingress.Status.LoadBalancer.Ingress) == 0 {
		return &routeState{
			reason: crewletaiv1alpha1.ReasonRouteNotAccepted,
			message: fmt.Sprintf("The Ingress controller has not published an address for Ingress %s (the %s) yet.",
				rt.name, rt.what),
		}, nil
	}
	return &routeState{
		accepted: true,
		message:  fmt.Sprintf("Ingress %s routes the %s from %s.", rt.name, rt.what, rt.hostname),
	}, nil
}

// reconcileNetworkPolicy writes the NetworkPolicies around the company's data
// pods: the class's isolation (<c>-engine) under networkPolicy Isolated, and
// the broker's (<c>-broker) whenever the data pods open a port of the broker.
// Each is deleted while it is not called for.
func (r *CompanyReconciler) reconcileNetworkPolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	if err := r.reconcileEnginePolicy(ctx, c, class); err != nil {
		return err
	}
	return r.reconcileBrokerPolicy(ctx, c, class)
}

// reconcileEnginePolicy writes the NetworkPolicy around the company's data
// pods under the class's networkPolicy Isolated, and deletes it otherwise.
//
// It selects the data pods only, never the seat pods: NetworkPolicies are
// additive, so one that selected the seat pods too would open them to what
// the data pods admit, and <c>-seats could no longer deny them every
// connection.
//
// Ingress is three ports, each from its own peers: the API port from the
// operator's pods, the Gateway proxies or Ingress controller of the class's
// API route, and the class's apiConsumers; the public port from where the
// webhooks' route arrives from alone (its Gateway proxies or Ingress
// controller, or the class's external forwarders); the artifact port from the
// artifacts' route alone. None admits another's peers: a webhook's proxy
// reaches no admin route, and an API consumer has no business on the port
// published to the internet. A rule
// with no peer is left out rather than written empty, which would admit
// every source. Egress is engineEgress.
func (r *CompanyReconciler) reconcileEnginePolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	settings := class.Spec.NetworkPolicy
	if settings.Mode != crewletaiv1alpha1.NetworkPolicyIsolated {
		return r.deleteControlled(ctx, c, networkPolicyGVK, networkPolicyName(c))
	}
	namespaceIn := func(names ...string) *networkingv1ac.NetworkPolicyPeerApplyConfiguration {
		return networkingv1ac.NetworkPolicyPeer().WithNamespaceSelector(metav1ac.LabelSelector().
			WithMatchExpressions(metav1ac.LabelSelectorRequirement().
				WithKey(corev1.LabelMetadataName).
				WithOperator(metav1.LabelSelectorOpIn).
				WithValues(names...)))
	}
	exposure := &class.Spec.Exposure
	var api []*networkingv1ac.NetworkPolicyPeerApplyConfiguration
	// Outside the cluster (make run) the operator has no namespace, and its
	// requests do not come from a pod a policy could admit anyway. Its pods
	// only, and not its whole namespace: other workloads may share it.
	if r.OperatorNamespace != "" {
		api = append(api, namespaceIn(r.OperatorNamespace).
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(r.OperatorPodLabels)))
	}
	if routed := apiRouteNamespaces(c, &exposure.API); len(routed) > 0 {
		api = append(api, namespaceIn(routed...))
	}
	for i := range settings.APIConsumers {
		api = append(api, peerOf(&settings.APIConsumers[i]))
	}
	var public []*networkingv1ac.NetworkPolicyPeerApplyConfiguration
	if routed := webhookRouteNamespaces(c, &exposure.Webhooks); len(routed) > 0 {
		public = append(public, namespaceIn(routed...))
	}
	var artifacts []*networkingv1ac.NetworkPolicyPeerApplyConfiguration
	if routed := artifactRouteNamespaces(c, &exposure.Artifacts); len(routed) > 0 {
		artifacts = append(artifacts, namespaceIn(routed...))
	}
	for i := range exposure.Webhooks.External.Forwarders {
		public = append(public, peerOf(&exposure.Webhooks.External.Forwarders[i]))
	}

	spec := networkingv1ac.NetworkPolicySpec().
		WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))).
		WithPolicyTypes(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress).
		WithEgress(engineEgress(&settings)...)
	for _, rule := range []struct {
		port  int32
		peers []*networkingv1ac.NetworkPolicyPeerApplyConfiguration
	}{{render.APIPort, api}, {render.PublicPort, public}, {render.ArtifactsPort, artifacts}} {
		if len(rule.peers) > 0 {
			spec.WithIngress(networkingv1ac.NetworkPolicyIngressRule().WithPorts(tcpPort(rule.port)).WithFrom(rule.peers...))
		}
	}
	policy := networkingv1ac.NetworkPolicy(networkPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(spec)
	return r.apply(ctx, c, networkPolicyGVK, networkPolicyName(c), policy)
}

// engineEgress is where an engine pod may connect under networkPolicy
// Isolated: DNS, the pods of the Company's namespace, every address but the
// cloud metadata endpoints and the class's exceptCIDRs, and the class's
// egress.to peers. The engine runs code the tenant supplies, and the metadata
// endpoint hands out the node's credentials to whatever asks.
func engineEgress(settings *crewletaiv1alpha1.NetworkPolicySettings) []*networkingv1ac.NetworkPolicyEgressRuleApplyConfiguration {
	egress := []*networkingv1ac.NetworkPolicyEgressRuleApplyConfiguration{
		// DNS, wherever the cluster's resolver runs (a kube-system
		// Service, or a node-local cache on a link-local address).
		networkingv1ac.NetworkPolicyEgressRule().WithPorts(
			networkingv1ac.NetworkPolicyPort().WithProtocol(corev1.ProtocolUDP).WithPort(intstr.FromInt32(dnsPort)),
			tcpPort(dnsPort)),
		// The tenant's own namespace: what it runs beside the engine
		// (an MCP server, a database) is the tenant's to reach.
		networkingv1ac.NetworkPolicyEgressRule().WithTo(
			networkingv1ac.NetworkPolicyPeer().WithPodSelector(metav1ac.LabelSelector())),
		networkingv1ac.NetworkPolicyEgressRule().WithTo(
			networkingv1ac.NetworkPolicyPeer().WithIPBlock(networkingv1ac.IPBlock().
				WithCIDR(anyIPv4).WithExcept(exceptCIDRs(settings.Egress.ExceptCIDRs, false)...)),
			networkingv1ac.NetworkPolicyPeer().WithIPBlock(networkingv1ac.IPBlock().
				WithCIDR(anyIPv6).WithExcept(exceptCIDRs(settings.Egress.ExceptCIDRs, true)...))),
	}
	if len(settings.Egress.To) > 0 {
		to := make([]*networkingv1ac.NetworkPolicyPeerApplyConfiguration, 0, len(settings.Egress.To))
		for i := range settings.Egress.To {
			to = append(to, peerOf(&settings.Egress.To[i]))
		}
		egress = append(egress, networkingv1ac.NetworkPolicyEgressRule().WithTo(to...))
	}
	return egress
}

// reconcileBrokerPolicy writes <c>-broker while the data pods open the leaf
// listener, whatever the class's networkPolicy says, and deletes it
// otherwise. "While" covers both the pods the next template makes
// (render.LeafOpen) and the pods that run now (dataPodsOpenLeaf): a template
// that closes the listener reaches the pods only as the data StatefulSet
// rolls, which takes a drain at least and lasts as long as the rollout is
// held back, and a pod that still listens meanwhile must stay behind the
// policy. Crewlet's broker accepts any connection on
// that port, with no authentication and no TLS, and whoever joins it reads
// and writes the company's whole event stream; with companies sharing a
// cluster, and sometimes a namespace, only a policy keeps another's pods off
// it. It admits the leaf port from this company's seat pods alone, selected
// by the company's label.
//
// It selects the data pods for ingress, and a pod any policy selects for
// ingress is denied every connection no policy admits: so without the class's
// isolation (<c>-engine, under Isolated) it admits the API port, the public
// port and the artifact port from anywhere as well, which leaves the data pods
// as open as they were and closes only the broker.
func (r *CompanyReconciler) reconcileBrokerPolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	open := render.LeafOpen(c, class)
	if !open {
		running, err := r.dataPodsOpenLeaf(ctx, c)
		if err != nil {
			return err
		}
		open = running
	}
	if !open {
		return r.deleteControlled(ctx, c, networkPolicyGVK, brokerPolicyName(c))
	}
	rules := []*networkingv1ac.NetworkPolicyIngressRuleApplyConfiguration{
		networkingv1ac.NetworkPolicyIngressRule().
			WithPorts(tcpPort(render.LeafPort)).
			WithFrom(networkingv1ac.NetworkPolicyPeer().
				WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentSeats)))),
	}
	if class.Spec.NetworkPolicy.Mode != crewletaiv1alpha1.NetworkPolicyIsolated {
		rules = append(rules, networkingv1ac.NetworkPolicyIngressRule().
			WithPorts(tcpPort(render.APIPort), tcpPort(render.PublicPort), tcpPort(render.ArtifactsPort)))
	}
	policy := networkingv1ac.NetworkPolicy(brokerPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(networkingv1ac.NetworkPolicySpec().
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))).
			WithPolicyTypes(networkingv1.PolicyTypeIngress).
			WithIngress(rules...))
	return r.apply(ctx, c, networkPolicyGVK, brokerPolicyName(c), policy)
}

// dataPodsOpenLeaf reports whether any data pod of the Company, running or
// stopping, opens the leaf listener: carries LeafListenerAnnotation. The
// operator does not watch Pods; the data StatefulSet's status changes as its
// pods are replaced, which brings the Company back here once the last one
// that listened is gone.
func (r *CompanyReconciler) dataPodsOpenLeaf(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	pods := &metav1.PartialObjectMetadataList{}
	pods.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
	if err := r.APIReader.List(ctx, pods, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return false, fmt.Errorf("listing the data pods of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	for i := range pods.Items {
		if pods.Items[i].Annotations[LeafListenerAnnotation] == leafListenerOpen {
			return true, nil
		}
	}
	return false, nil
}

// reconcileSeatsPolicy writes <c>-seats while the company runs seat pods, and
// deletes it otherwise. A seat pod serves no other pod (a node without the
// ingress role answers only its probes, which the kubelet asks from the pod's
// own node, which no policy polices), so it admits no connection at all.
// Under the class's networkPolicy Isolated it also holds the seat pods to the
// data pods' egress, and names the leaf listener they join through on its
// own, so that narrowing the namespace's egress never cuts them off.
//
// The isolation holds only while no other policy selects the seat pods,
// which is why <c>-engine and <c>-broker select the data pods alone.
func (r *CompanyReconciler) reconcileSeatsPolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, running bool,
) error {
	if !running {
		return r.deleteControlled(ctx, c, networkPolicyGVK, seatsPolicyName(c))
	}
	spec := networkingv1ac.NetworkPolicySpec().
		WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentSeats))).
		WithPolicyTypes(networkingv1.PolicyTypeIngress)
	if settings := &class.Spec.NetworkPolicy; settings.Mode == crewletaiv1alpha1.NetworkPolicyIsolated {
		leaf := networkingv1ac.NetworkPolicyEgressRule().
			WithPorts(tcpPort(render.LeafPort)).
			WithTo(networkingv1ac.NetworkPolicyPeer().
				WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))))
		spec.WithPolicyTypes(networkingv1.PolicyTypeEgress).
			WithEgress(append([]*networkingv1ac.NetworkPolicyEgressRuleApplyConfiguration{leaf}, engineEgress(settings)...)...)
	}
	policy := networkingv1ac.NetworkPolicy(seatsPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentSeats)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(spec)
	return r.apply(ctx, c, networkPolicyGVK, seatsPolicyName(c), policy)
}

// tcpPort is a NetworkPolicy port of TCP.
func tcpPort(port int32) *networkingv1ac.NetworkPolicyPortApplyConfiguration {
	return networkingv1ac.NetworkPolicyPort().WithProtocol(corev1.ProtocolTCP).WithPort(intstr.FromInt32(port))
}

// The address space an engine may reach, and the cloud metadata endpoints
// it may not: AWS, GCP and Azure serve theirs at 169.254.169.254, and AWS
// also at fd00:ec2::254 on IPv6.
const (
	dnsPort      = 53
	anyIPv4      = "0.0.0.0/0"
	anyIPv6      = "::/0"
	metadataIPv4 = "169.254.169.254/32"
	metadataIPv6 = "fd00:ec2::254/128"
)

// exceptCIDRs are the blocks of one address family the engine may not
// reach: the metadata endpoint and the class's exceptCIDRs of that family.
// The API server has checked that each is a CIDR.
func exceptCIDRs(class []string, ipv6 bool) []string {
	except := []string{metadataIPv4}
	if ipv6 {
		except = []string{metadataIPv6}
	}
	for _, block := range class {
		if prefix, err := netip.ParsePrefix(block); err == nil && prefix.Addr().Is6() == ipv6 {
			except = append(except, block)
		}
	}
	return except
}

// peerOf is a class's peer as a NetworkPolicy peer.
func peerOf(p *crewletaiv1alpha1.NetworkPolicyPeer) *networkingv1ac.NetworkPolicyPeerApplyConfiguration {
	peer := networkingv1ac.NetworkPolicyPeer().WithNamespaceSelector(selectorOf(p.NamespaceSelector))
	if p.PodSelector != nil {
		peer = peer.WithPodSelector(selectorOf(p.PodSelector))
	}
	return peer
}

// selectorOf is a label selector as an apply configuration. A nil one is
// empty, which selects everything.
func selectorOf(s *metav1.LabelSelector) *metav1ac.LabelSelectorApplyConfiguration {
	out := metav1ac.LabelSelector()
	if s == nil {
		return out
	}
	if len(s.MatchLabels) > 0 {
		out = out.WithMatchLabels(s.MatchLabels)
	}
	for _, e := range s.MatchExpressions {
		out = out.WithMatchExpressions(metav1ac.LabelSelectorRequirement().
			WithKey(e.Key).WithOperator(e.Operator).WithValues(e.Values...))
	}
	return out
}

// webhookRouteNamespaces are the namespaces the webhooks' route reaches the
// data pods' public port from; see routeNamespaces.
func webhookRouteNamespaces(c *crewletaiv1alpha1.Company, w *crewletaiv1alpha1.WebhookExposure) []string {
	switch w.Mode {
	case crewletaiv1alpha1.WebhookExposureGateway:
		return routeNamespaces(c, &w.Gateway, nil)
	case crewletaiv1alpha1.WebhookExposureIngress:
		return routeNamespaces(c, nil, w.Ingress)
	case crewletaiv1alpha1.WebhookExposureExternal, crewletaiv1alpha1.WebhookExposureNone:
		// No route: an External forwarder is admitted as a peer of its own
		// (external.forwarders), and under None nothing arrives.
	}
	return nil
}

// apiRouteNamespaces are the namespaces the API's route reaches the data
// pods' API port from; see routeNamespaces.
func apiRouteNamespaces(c *crewletaiv1alpha1.Company, a *crewletaiv1alpha1.APIExposure) []string {
	switch a.Mode {
	case crewletaiv1alpha1.APIExposureGateway:
		return routeNamespaces(c, &a.Gateway, nil)
	case crewletaiv1alpha1.APIExposureIngress:
		return routeNamespaces(c, nil, a.Ingress)
	case crewletaiv1alpha1.APIExposureClusterInternal:
		// No route: the API's callers are the operator and apiConsumers.
	}
	return nil
}

// artifactRouteNamespaces are the namespaces the artifacts' route reaches the
// data pods' artifact port from; see routeNamespaces.
func artifactRouteNamespaces(c *crewletaiv1alpha1.Company, a *crewletaiv1alpha1.ArtifactExposure) []string {
	switch a.Mode {
	case crewletaiv1alpha1.ArtifactExposureGateway:
		return routeNamespaces(c, &a.Gateway, nil)
	case crewletaiv1alpha1.ArtifactExposureIngress:
		return routeNamespaces(c, nil, a.Ingress)
	case crewletaiv1alpha1.ArtifactExposureNone:
		// No route.
	}
	return nil
}

// routeNamespaces are the namespaces one route's traffic reaches the data
// pods from: a Gateway route's proxyNamespaces (or its Gateways'
// namespaces), or an Ingress route's controllerNamespaces. Sorted and
// without repetition, so the policy does not change with the order they are
// listed in.
func routeNamespaces(
	c *crewletaiv1alpha1.Company, gateway *crewletaiv1alpha1.GatewayExposure, ingress *crewletaiv1alpha1.IngressExposure,
) []string {
	var names []string
	switch {
	case gateway != nil && len(gateway.ProxyNamespaces) > 0:
		names = append(names, gateway.ProxyNamespaces...)
	case gateway != nil:
		for _, p := range gateway.ParentRefs {
			names = append(names, cmp.Or(p.Namespace, c.Namespace))
		}
	case ingress != nil:
		names = append(names, ingress.ControllerNamespaces...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// deleteControlled deletes the object of a kind and name when the Company
// controls it. An object of that name that the Company does not control is
// someone else's and is left alone. It reads the object's metadata only,
// through the metadata informer its kind is watched with.
func (r *CompanyReconciler) deleteControlled(
	ctx context.Context, c *crewletaiv1alpha1.Company, gvk schema.GroupVersionKind, name string,
) error {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(gvk)
	if err := r.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("reading %s %s/%s: %w", gvk.Kind, c.Namespace, name, err)
	}
	return r.deleteIfControlled(ctx, c, obj)
}

// deleteIfControlled deletes an object the Company controls. The UID
// precondition keeps the delete from reaching an object that replaced this
// one since it was read.
func (r *CompanyReconciler) deleteIfControlled(
	ctx context.Context, c *crewletaiv1alpha1.Company, obj client.Object, opts ...client.DeleteOption,
) error {
	if !metav1.IsControlledBy(obj, c) {
		return nil
	}
	uid := obj.GetUID()
	opts = append(opts, client.Preconditions{UID: &uid})
	if err := r.Delete(ctx, obj, opts...); err != nil &&
		!apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}
