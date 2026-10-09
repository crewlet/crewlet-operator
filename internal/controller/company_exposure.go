package controller

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/netip"
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

// The paths a route sends to the engine. Vendors need /webhooks/ only; the
// engine's admin API (/config, /secrets, ...) shares the port and is never
// routed publicly. The tool bridge, /mcp/, is routed beside the webhooks
// while the company has an Agent-mode CLI provider: its runs happen in a
// remote sandbox, which calls the seat's tools back there, admitted by a
// token signed for the one run in the path itself, as a webhook is admitted
// by its signature. A path prefix matches whole path segments in both the
// Gateway API and Ingress, so /webhooks/ does not match /webhooksx.
const (
	webhooksPath = "/webhooks/"
	bridgePath   = "/mcp/"
	apiPath      = "/"
)

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

// route is one route a class asks for: the webhooks' or the API's.
type route struct {
	// what is "webhooks" or "API", for messages.
	what      string
	name      string
	component string
	paths     []string
	gateway   *crewletaiv1alpha1.GatewayExposure
	ingress   *crewletaiv1alpha1.IngressExposure
	// hostname is the expanded host name template.
	hostname string
}

// routeState is what a route's controller reported about it.
type routeState struct {
	accepted bool
	reason   string
	message  string
	// https is whether the route terminates TLS.
	https bool
}

// reconcileExposure writes the routes the class asks for, deletes those it no
// longer asks for, sets the Company's public base URL and exposed API
// endpoint, and returns the Exposed condition. With bridge the webhooks'
// route sends the tool bridge's path to the engine too.
func (r *CompanyReconciler) reconcileExposure(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, bridge bool,
) (metav1.Condition, error) {
	exposure := class.Spec.Exposure
	webhooks := route{what: "webhooks", name: webhooksRouteName(c), component: componentWebhooks, paths: []string{webhooksPath}}
	if bridge {
		webhooks.paths = append(webhooks.paths, bridgePath)
	}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureGateway {
		webhooks.gateway = &exposure.Webhooks.Gateway
	}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureIngress {
		webhooks.ingress = exposure.Webhooks.Ingress
	}
	if exposure.Webhooks.Hostname != "" {
		webhooks.hostname = render.ExpandTemplate(exposure.Webhooks.Hostname, c)
	}
	api := route{what: "API", name: apiRouteName(c), component: componentAPI, paths: []string{apiPath}}
	if exposure.API.Mode == crewletaiv1alpha1.APIExposureGateway {
		api.gateway = &exposure.API.Gateway
	}
	if exposure.API.Mode == crewletaiv1alpha1.APIExposureIngress {
		api.ingress = exposure.API.Ingress
	}
	if exposure.API.Hostname != "" {
		api.hostname = render.ExpandTemplate(exposure.API.Hostname, c)
	}

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
		return metav1.Condition{}, err
	}
	apiState, err := r.reconcileRoute(ctx, c, &api)
	if err != nil {
		return metav1.Condition{}, err
	}
	c.Status.Endpoints.ExposedAPI = ""
	if apiState != nil && apiState.accepted {
		scheme := "http"
		if apiState.https {
			scheme = "https"
		}
		c.Status.Endpoints.ExposedAPI = scheme + "://" + api.hostname
	}
	var states []routeState
	for _, state := range []*routeState{webhooksState, apiState} {
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
		return exposed, nil
	}
	for _, state := range states {
		if !state.accepted {
			exposed.Status, exposed.Reason, exposed.Message = metav1.ConditionFalse, state.reason, state.message
			return exposed, nil
		}
	}
	exposed.Status = metav1.ConditionTrue
	if len(states) == 0 {
		exposed.Reason = crewletaiv1alpha1.ReasonNoRoutes
		exposed.Message = fmt.Sprintf("EngineClass %s routes nothing to the engine from outside the cluster.", class.Name)
		return exposed, nil
	}
	messages := make([]string, 0, len(states))
	for _, state := range states {
		messages = append(messages, state.message)
	}
	exposed.Reason = crewletaiv1alpha1.ReasonRouteAccepted
	exposed.Message = strings.Join(messages, " ")
	return exposed, nil
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
			WithPort(render.APIPort))
	for _, path := range rt.paths {
		rule.WithMatches(gatewayv1ac.HTTPRouteMatch().
			WithPath(gatewayv1ac.HTTPPathMatch().WithType(gatewayv1.PathMatchPathPrefix).WithValue(path)))
	}
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
	https, err := r.gatewaysServeHTTPS(ctx, c, rt.gateway)
	if err != nil {
		return nil, err
	}
	return &routeState{
		accepted: true,
		https:    https,
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

// gatewaysServeHTTPS reports whether a route's Gateways terminate TLS for
// it: whether a listener the route can attach to is HTTPS. It reads the
// Gateways uncached: the operator's cache holds Gateways as metadata only
// (for the EngineClass reconciler), and their listeners are read only once a
// route was accepted.
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
	paths := networkingv1ac.HTTPIngressRuleValue()
	for _, path := range rt.paths {
		paths.WithPaths(networkingv1ac.HTTPIngressPath().
			WithPath(path).
			WithPathType(networkingv1.PathTypePrefix).
			WithBackend(networkingv1ac.IngressBackend().
				WithService(networkingv1ac.IngressServiceBackend().
					WithName(engineServiceName(c)).
					WithPort(networkingv1ac.ServiceBackendPort().WithNumber(render.APIPort)))))
	}
	spec := networkingv1ac.IngressSpec().
		WithRules(networkingv1ac.IngressRule().
			WithHost(rt.hostname).
			WithHTTP(paths))
	if rt.ingress.ClassName != "" {
		spec.WithIngressClassName(rt.ingress.ClassName)
	}
	https := rt.ingress.TLS.SecretName != ""
	if https {
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
		https:    https,
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
// Ingress is the engine's API port only, from the operator's pods, the
// class's Gateway proxies or Ingress controller, and the class's
// apiConsumers. Egress is engineEgress.
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
	var from []*networkingv1ac.NetworkPolicyPeerApplyConfiguration
	// Outside the cluster (make run) the operator has no namespace, and its
	// requests do not come from a pod a policy could admit anyway. Its pods
	// only, and not its whole namespace: other workloads may share it.
	if r.OperatorNamespace != "" {
		from = append(from, namespaceIn(r.OperatorNamespace).
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(r.OperatorPodLabels)))
	}
	if routed := routeNamespaces(c, class); len(routed) > 0 {
		from = append(from, namespaceIn(routed...))
	}
	for i := range settings.APIConsumers {
		from = append(from, peerOf(&settings.APIConsumers[i]))
	}

	policy := networkingv1ac.NetworkPolicy(networkPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(networkingv1ac.NetworkPolicySpec().
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))).
			WithPolicyTypes(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress).
			WithIngress(networkingv1ac.NetworkPolicyIngressRule().
				WithPorts(tcpPort(render.APIPort)).
				WithFrom(from...)).
			WithEgress(engineEgress(&settings)...))
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
// listener (the class allows seat pools), whatever the class's networkPolicy
// says, and deletes it otherwise. Crewlet's broker accepts any connection on
// that port, with no authentication and no TLS, and whoever joins it reads
// and writes the company's whole event stream; with companies sharing a
// cluster, and sometimes a namespace, only a policy keeps another's pods off
// it. It admits the leaf port from this company's seat pods alone, selected
// by the company's label.
//
// It selects the data pods for ingress, and a pod any policy selects for
// ingress is denied every connection no policy admits: so without the class's
// isolation (<c>-engine, under Isolated) it admits the API port from anywhere
// as well, which leaves the data pods as open as they were and closes only the
// broker.
func (r *CompanyReconciler) reconcileBrokerPolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	if !leafOpen(class) {
		return r.deleteControlled(ctx, c, networkPolicyGVK, brokerPolicyName(c))
	}
	rules := []*networkingv1ac.NetworkPolicyIngressRuleApplyConfiguration{
		networkingv1ac.NetworkPolicyIngressRule().
			WithPorts(tcpPort(render.LeafPort)).
			WithFrom(networkingv1ac.NetworkPolicyPeer().
				WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentSeats)))),
	}
	if class.Spec.NetworkPolicy.Mode != crewletaiv1alpha1.NetworkPolicyIsolated {
		rules = append(rules, networkingv1ac.NetworkPolicyIngressRule().WithPorts(tcpPort(render.APIPort)))
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

// reconcileSeatsPolicy writes <c>-seats while the company runs seat pods, and
// deletes it otherwise. A seat pod serves nothing (the engine binds no port
// on a node without the ingress role), so it admits no connection at all.
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

// routeNamespaces are the namespaces routed traffic reaches the data pods
// from: each Gateway route's proxyNamespaces (or its Gateways' namespaces),
// and each Ingress route's controllerNamespaces. Sorted and without
// repetition, so the policy does not change with the order they are listed in.
func routeNamespaces(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) []string {
	var names []string
	gateway := func(g *crewletaiv1alpha1.GatewayExposure) {
		if len(g.ProxyNamespaces) > 0 {
			names = append(names, g.ProxyNamespaces...)
			return
		}
		for _, p := range g.ParentRefs {
			names = append(names, cmp.Or(p.Namespace, c.Namespace))
		}
	}
	exposure := class.Spec.Exposure
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureGateway {
		gateway(&exposure.Webhooks.Gateway)
	}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureIngress {
		names = append(names, exposure.Webhooks.Ingress.ControllerNamespaces...)
	}
	if exposure.API.Mode == crewletaiv1alpha1.APIExposureGateway {
		gateway(&exposure.API.Gateway)
	}
	if exposure.API.Mode == crewletaiv1alpha1.APIExposureIngress {
		names = append(names, exposure.API.Ingress.ControllerNamespaces...)
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
