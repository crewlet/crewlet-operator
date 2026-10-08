package controller

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
// routed publicly. A path prefix matches whole path segments in both the
// Gateway API and Ingress, so /webhooks/ does not match /webhooksx.
const (
	webhooksPath = "/webhooks/"
	apiPath      = "/"
)

// The kinds the operator may have to delete when the class stops asking for
// them.
var (
	secretGVK        = corev1.SchemeGroupVersion.WithKind("Secret")
	ingressGVK       = networkingv1.SchemeGroupVersion.WithKind("Ingress")
	networkPolicyGVK = networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy")
	httpRouteGVK     = schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version, Kind: "HTTPRoute"}
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
	path      string
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
// endpoint, and returns the Exposed condition.
func (r *CompanyReconciler) reconcileExposure(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (metav1.Condition, error) {
	exposure := class.Spec.Exposure
	webhooks := route{what: "webhooks", name: webhooksRouteName(c), component: componentWebhooks, path: webhooksPath}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureGateway {
		webhooks.gateway = &exposure.Webhooks.Gateway
	}
	if exposure.Webhooks.Mode == crewletaiv1alpha1.WebhookExposureIngress {
		webhooks.ingress = exposure.Webhooks.Ingress
	}
	if exposure.Webhooks.Hostname != "" {
		webhooks.hostname = render.ExpandTemplate(exposure.Webhooks.Hostname, c)
	}
	api := route{what: "API", name: apiRouteName(c), component: componentAPI, path: apiPath}
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
	switch exposure.Webhooks.Mode {
	case crewletaiv1alpha1.WebhookExposureGateway, crewletaiv1alpha1.WebhookExposureIngress:
		c.Status.Endpoints.PublicBaseURL = cmp.Or(c.Spec.Exposure.PublicBaseURL, "https://"+webhooks.hostname)
	case crewletaiv1alpha1.WebhookExposureExternal:
		c.Status.Endpoints.PublicBaseURL = c.Spec.Exposure.PublicBaseURL
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
	switch {
	case rt.gateway != nil:
		if !r.GatewayAPI {
			return &routeState{
				reason: crewletaiv1alpha1.ReasonGatewayAPINotInstalled,
				message: fmt.Sprintf("The class routes the %s through Gateways, and this cluster has no Gateway API HTTPRoute kind: "+
					"install the Gateway API and restart the operator.", rt.what),
			}, nil
		}
		return r.applyHTTPRoute(ctx, c, rt)
	case rt.ingress != nil:
		return r.applyIngress(ctx, c, rt)
	default:
		return nil, nil
	}
}

// applyHTTPRoute writes a route as a Gateway API HTTPRoute and reads whether
// every Gateway it names accepted it.
func (r *CompanyReconciler) applyHTTPRoute(ctx context.Context, c *crewletaiv1alpha1.Company, rt *route) (*routeState, error) {
	spec := gatewayv1ac.HTTPRouteSpec().
		WithHostnames(gatewayv1.Hostname(rt.hostname)).
		WithRules(gatewayv1ac.HTTPRouteRule().
			WithMatches(gatewayv1ac.HTTPRouteMatch().
				WithPath(gatewayv1ac.HTTPPathMatch().WithType(gatewayv1.PathMatchPathPrefix).WithValue(rt.path))).
			WithBackendRefs(gatewayv1ac.HTTPBackendRef().
				WithName(gatewayv1.ObjectName(engineServiceName(c))).
				WithPort(render.APIPort)))
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
	if err := r.apply(ctx, httpRoute, "HTTPRoute", rt.name, c.Namespace); err != nil {
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
// Gateways uncached: the operator does not watch Gateways, and reads them
// only once a route was accepted.
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
	spec := networkingv1ac.IngressSpec().
		WithRules(networkingv1ac.IngressRule().
			WithHost(rt.hostname).
			WithHTTP(networkingv1ac.HTTPIngressRuleValue().
				WithPaths(networkingv1ac.HTTPIngressPath().
					WithPath(rt.path).
					WithPathType(networkingv1.PathTypePrefix).
					WithBackend(networkingv1ac.IngressBackend().
						WithService(networkingv1ac.IngressServiceBackend().
							WithName(engineServiceName(c)).
							WithPort(networkingv1ac.ServiceBackendPort().WithNumber(render.APIPort)))))))
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
	if err := r.apply(ctx, ingress, "Ingress", rt.name, c.Namespace); err != nil {
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

// reconcileNetworkPolicy writes the NetworkPolicy that isolates the data
// pods under networkPolicy Isolated, and deletes it otherwise.
//
// It selects the data pods only, so that a policy for other pods of the
// company can be added beside it: NetworkPolicies are additive, and one that
// selected every pod would open them all to what the data pods admit. It
// restricts ingress only; the engine reaches vendors and model providers
// outward on its own.
func (r *CompanyReconciler) reconcileNetworkPolicy(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	if class.Spec.NetworkPolicy != crewletaiv1alpha1.NetworkPolicyIsolated {
		return r.deleteControlled(ctx, c, networkPolicyGVK, networkPolicyName(c))
	}
	namespaceIn := func(names ...string) *networkingv1ac.NetworkPolicyPeerApplyConfiguration {
		return networkingv1ac.NetworkPolicyPeer().WithNamespaceSelector(metav1ac.LabelSelector().
			WithMatchExpressions(metav1ac.LabelSelectorRequirement().
				WithKey(corev1.LabelMetadataName).
				WithOperator(metav1.LabelSelectorOpIn).
				WithValues(names...)))
	}
	peers := []*networkingv1ac.NetworkPolicyPeerApplyConfiguration{
		networkingv1ac.NetworkPolicyPeer().WithNamespaceSelector(metav1ac.LabelSelector().
			WithMatchLabels(map[string]string{crewletaiv1alpha1.APIConsumerNamespaceLabel: "true"})),
	}
	// Outside the cluster (make run) the operator has no namespace, and its
	// requests do not come from a pod a policy could admit anyway.
	if r.OperatorNamespace != "" {
		peers = append(peers, namespaceIn(r.OperatorNamespace))
	}
	if routed := routeNamespaces(c, class); len(routed) > 0 {
		peers = append(peers, namespaceIn(routed...))
	}
	policy := networkingv1ac.NetworkPolicy(networkPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(networkingv1ac.NetworkPolicySpec().
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))).
			WithPolicyTypes(networkingv1.PolicyTypeIngress).
			WithIngress(networkingv1ac.NetworkPolicyIngressRule().
				WithPorts(networkingv1ac.NetworkPolicyPort().
					WithProtocol(corev1.ProtocolTCP).
					WithPort(intstr.FromInt32(render.APIPort))).
				WithFrom(peers...)))
	return r.apply(ctx, policy, "NetworkPolicy", networkPolicyName(c), c.Namespace)
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
func (r *CompanyReconciler) deleteIfControlled(ctx context.Context, c *crewletaiv1alpha1.Company, obj client.Object) error {
	if !metav1.IsControlledBy(obj, c) {
		return nil
	}
	uid := obj.GetUID()
	if err := r.Delete(ctx, obj, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil &&
		!apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}
