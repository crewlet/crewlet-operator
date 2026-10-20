package controller

import (
	"context"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// EngineClassReconciler writes an EngineClass's status: its Accepted
// condition and observedGeneration. The class's schema checks each field on
// its own; what it cannot check is whether the objects the class names in
// other namespaces exist, which matters before any Company runs with the
// class, since a Company would then fail on it in every tenant namespace at
// once. So Accepted is False when
//
//   - a Secret the class references (a platform model provider's API key,
//     the telemetry headers, a pod environment value) does not exist, or
//     lacks the key referenced: SecretNotFound. Only names and keys are read
//     into the condition, never a value;
//   - the class routes through Gateways and the cluster has no Gateway API:
//     GatewayAPINotInstalled;
//   - a Gateway a route attaches to, named with its namespace, does not
//     exist: GatewayNotFound. A parentRef without a namespace is in each
//     Company's own namespace, which has no one answer for the class.
//
// The engine image is not checked: its grammar is the schema's, and whether
// a registry serves it to the data pods only the kubelet that pulls it can
// tell. Accepted is advisory: Companies of a class that is not accepted still
// run, and report what the problem does to them on their own conditions.
type EngineClassReconciler struct {
	client.Client

	// APIReader reads the referenced Secrets, to see their keys: the
	// manager's cache holds Secrets as metadata only.
	APIReader client.Reader

	// GatewayAPI is whether the cluster serves the Gateway API, found once
	// when the operator starts (GatewayAPIInstalled).
	GatewayAPI bool
}

// The class's status is patched; the Secrets it references are read with get,
// and watched as metadata, as the other reconcilers already do; Gateways are
// watched as metadata.
//
// +kubebuilder:rbac:groups=crewlet.ai,resources=engineclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=engineclasses/status,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch

// Reconcile checks an EngineClass and records the verdict on its status.
func (r *EngineClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	class := &crewletaiv1alpha1.EngineClass{}
	if err := r.Get(ctx, req.NamespacedName, class); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reason, findings, err := r.check(ctx, class)
	if err != nil {
		return ctrl.Result{}, err
	}
	accepted := metav1.Condition{
		Type: crewletaiv1alpha1.ConditionAccepted, Status: metav1.ConditionTrue, Reason: crewletaiv1alpha1.ReasonValid,
		Message: "Every Secret and Gateway the class names exists.", ObservedGeneration: class.Generation,
	}
	if len(findings) > 0 {
		accepted.Status, accepted.Reason, accepted.Message = metav1.ConditionFalse, reason, bounded(sentences(findings))
	}

	base := class.DeepCopy()
	meta.SetStatusCondition(&class.Status.Conditions, accepted)
	class.Status.ObservedGeneration = class.Generation
	if err := r.Status().Patch(ctx, class, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating the status of EngineClass %s: %w", class.Name, err)
	}
	return ctrl.Result{}, nil
}

// check returns what is wrong with a class: the reason of the first finding,
// and every finding as a sentence. An error is the API server's.
func (r *EngineClassReconciler) check(ctx context.Context, class *crewletaiv1alpha1.EngineClass) (string, []string, error) {
	var reason string
	var findings []string
	found := func(why, finding string) {
		if reason == "" {
			reason = why
		}
		findings = append(findings, finding)
	}

	// Each Secret is read once, however many keys of it the class names,
	// and reported missing once.
	read := map[types.NamespacedName]*corev1.Secret{}
	for _, k := range secretRefsOf(class) {
		name := types.NamespacedName{Namespace: k.Namespace, Name: k.Name}
		secret, seen := read[name]
		if !seen {
			secret = &corev1.Secret{}
			err := r.APIReader.Get(ctx, name, secret)
			switch {
			case apierrors.IsNotFound(err):
				secret = nil
				found(crewletaiv1alpha1.ReasonSecretNotFound, fmt.Sprintf("Secret %s does not exist", name))
			case err != nil:
				return "", nil, fmt.Errorf("reading Secret %s: %w", name, err)
			}
			read[name] = secret
		}
		if secret == nil || k.Key == "" {
			continue
		}
		if _, ok := secret.Data[k.Key]; !ok {
			found(crewletaiv1alpha1.ReasonSecretNotFound, fmt.Sprintf("Secret %s has no key %q", name, k.Key))
		}
	}

	parents := gatewayParents(class)
	if len(parents) > 0 && !r.GatewayAPI {
		found(crewletaiv1alpha1.ReasonGatewayAPINotInstalled,
			"The class routes through Gateways, and the cluster had no Gateway API when the operator started")
		return reason, findings, nil
	}
	for _, p := range parents {
		if p.Namespace == "" {
			continue
		}
		key := types.NamespacedName{Namespace: p.Namespace, Name: p.Name}
		gateway := &metav1.PartialObjectMetadata{}
		gateway.SetGroupVersionKind(gatewayGVK)
		err := r.Get(ctx, key, gateway)
		switch {
		case apierrors.IsNotFound(err):
			found(crewletaiv1alpha1.ReasonGatewayNotFound, fmt.Sprintf("Gateway %s does not exist", key))
		case err != nil:
			return "", nil, fmt.Errorf("reading Gateway %s: %w", key, err)
		}
	}
	return reason, findings, nil
}

// gatewayGVK is the kind the class's routes attach to, read as metadata.
var gatewayGVK = schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version, Kind: "Gateway"}

// gatewayParents are the Gateways a class's routes attach to: the webhooks',
// the API's and the artifacts', each once.
func gatewayParents(class *crewletaiv1alpha1.EngineClass) []crewletaiv1alpha1.GatewayParentReference {
	var out []crewletaiv1alpha1.GatewayParentReference
	exposure := class.Spec.Exposure
	for _, g := range []*crewletaiv1alpha1.GatewayExposure{
		&exposure.Webhooks.Gateway, &exposure.API.Gateway, &exposure.Artifacts.Gateway,
	} {
		for _, p := range g.ParentRefs {
			p.SectionName = ""
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
//
// A class is checked again when its spec changes, and when a Secret or a
// Gateway it names is created, changed or deleted. Its own status writes
// change no generation and so run nothing.
func (r *EngineClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&crewletaiv1alpha1.EngineClass{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.classesOf(func(class *crewletaiv1alpha1.EngineClass, obj client.Object) bool {
			return slices.ContainsFunc(secretRefsOf(class), func(k secretref.Key) bool {
				return k.Namespace == obj.GetNamespace() && k.Name == obj.GetName()
			})
		})))
	if r.GatewayAPI {
		b = b.WatchesMetadata(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.classesOf(
			func(class *crewletaiv1alpha1.EngineClass, obj client.Object) bool {
				return slices.ContainsFunc(gatewayParents(class), func(p crewletaiv1alpha1.GatewayParentReference) bool {
					return p.Namespace == obj.GetNamespace() && p.Name == obj.GetName()
				})
			})))
	}
	return b.Named("engineclass").Complete(r)
}

// classesOf maps an object to the classes names reports as naming it. A
// cluster has a handful of classes, so they are all looked at rather than
// indexed.
func (r *EngineClassReconciler) classesOf(
	names func(*crewletaiv1alpha1.EngineClass, client.Object) bool,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		classes := &crewletaiv1alpha1.EngineClassList{}
		if err := r.List(ctx, classes); err != nil {
			// The map function cannot return an error. The cache answers
			// this list from memory, so it fails only while the cache is
			// not synced, before any event is delivered; log it rather
			// than drop it silently.
			logf.FromContext(ctx).Error(err, "Failed to list the EngineClasses an object change affects",
				"object", client.ObjectKeyFromObject(obj))
			return nil
		}
		var requests []reconcile.Request
		for i := range classes.Items {
			if names(&classes.Items[i], obj) {
				requests = append(requests, reconcile.Request{Name: classes.Items[i].Name})
			}
		}
		return requests
	}
}
