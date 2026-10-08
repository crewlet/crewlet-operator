package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// CompanyReconciler reconciles Company objects. It decides whether a Company
// is one the operator can act on (Accepted), and for an accepted one makes the
// engine exist and be reachable: the keyring and API tokens, the bootstrap
// document, the data StatefulSet, the Services, the routes and the network
// policy (RuntimeReady, Exposed). It also owns the Ready summary.
//
// The company document is not its concern: that is the config reconciler's,
// which owns the SecretsSynced and ConfigApplied conditions and
// status.config. Each writes only the status fields it owns, each with an
// optimistic lock, so neither overwrites the other's.
type CompanyReconciler struct {
	client.Client

	// APIReader reads from the API server directly. Secrets and Pods are
	// read through it and never through the cache: the manager caches
	// Secrets as metadata only, so that it does not hold every Secret of the
	// cluster in memory, and does not cache Pods at all.
	APIReader client.Reader

	// GatewayAPI is whether the cluster serves Gateway API HTTPRoutes,
	// detected once when the manager starts (GatewayAPIInstalled).
	GatewayAPI bool

	// OperatorNamespace is the namespace the operator runs in, admitted to
	// every engine's port under networkPolicy Isolated so that the operator
	// can reach the engines it configures. It is empty when the operator
	// runs outside the cluster.
	OperatorNamespace string
}

// Writes use server-side apply or patch, never update, so no rule grants
// update except on companies/finalizers, which is how the API server lets an
// owner reference block its owner's deletion.
//
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/status,verbs=get;patch
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/finalizers,verbs=update
// +kubebuilder:rbac:groups=crewlet.ai,resources=engineclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=seats;mcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;configmaps;services;serviceaccounts,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=list
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=list;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses;networkpolicies,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get

// Reconcile brings a Company's engine to what the Company and its class ask
// for and records what it found in the Company's status; for a deleted
// Company, it stops the engine and releases the Company.
func (r *CompanyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	company := &crewletaiv1alpha1.Company{}
	if err := r.Get(ctx, req.NamespacedName, company); err != nil {
		// A request can outlive its object: the key of a Company deleted
		// since it was queued is not an error.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !company.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, company)
	}
	if !controllerutil.ContainsFinalizer(company, crewletaiv1alpha1.CompanyFinalizer) {
		base := company.DeepCopy()
		controllerutil.AddFinalizer(company, crewletaiv1alpha1.CompanyFinalizer)
		if err := r.Patch(ctx, company, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding the finalizer to Company %s: %w", req.NamespacedName, err)
		}
	}

	base := company.DeepCopy()
	class, accepted, err := r.accept(ctx, company)
	if err != nil {
		return ctrl.Result{}, err
	}
	accepted.Type = crewletaiv1alpha1.ConditionAccepted
	accepted.ObservedGeneration = company.Generation
	meta.SetStatusCondition(&company.Status.Conditions, accepted)

	// A Company that is no longer accepted (its class was deleted, say)
	// keeps the engine it has: the runtime conditions keep the generation
	// they describe, and Ready says why nothing changes.
	if accepted.Status == metav1.ConditionTrue {
		if err := r.reconcileRuntime(ctx, company, class); err != nil {
			return ctrl.Result{}, err
		}
	}
	ready := summarize(company)
	ready.ObservedGeneration = company.Generation
	meta.SetStatusCondition(&company.Status.Conditions, ready)
	company.Status.ObservedGeneration = company.Generation

	// The optimistic lock makes a status write fail, and the reconcile
	// retry, rather than drop a condition the config reconciler wrote since
	// the Company was read: a merge patch replaces the whole list.
	if err := r.Status().Patch(ctx, company, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating the status of Company %s: %w", req.NamespacedName, err)
	}
	logf.FromContext(ctx).V(1).Info("Reconciled the Company", "ready", ready.Status, "reason", ready.Reason)
	return ctrl.Result{}, nil
}

// accept returns the EngineClass of a Company and its Accepted condition,
// without the condition's type and generation; the class is nil when it does
// not resolve. An error is one the reconcile should be retried for (the API
// server did not answer); everything about the Company itself is a condition.
func (r *CompanyReconciler) accept(
	ctx context.Context, company *crewletaiv1alpha1.Company,
) (*crewletaiv1alpha1.EngineClass, metav1.Condition, error) {
	class, refused, err := r.engineClass(ctx, company)
	if err != nil {
		return nil, metav1.Condition{}, err
	}
	if refused != nil {
		return nil, *refused, nil
	}
	condition, err := r.withinLimits(ctx, company, class)
	return class, condition, err
}

// withinLimits returns the Accepted condition of a Company whose class
// resolves: whether its Seats and MCPServers stay within the class's limits.
func (r *CompanyReconciler) withinLimits(
	ctx context.Context, company *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (metav1.Condition, error) {

	limits := class.Spec.Limits
	if limits.MaxSeats != nil {
		seats := &crewletaiv1alpha1.SeatList{}
		if err := r.List(ctx, seats, client.InNamespace(company.Namespace)); err != nil {
			return metav1.Condition{}, fmt.Errorf("listing the Seats of Company %s/%s: %w", company.Namespace, company.Name, err)
		}
		n := countOf(seats.Items, company.Name, func(s crewletaiv1alpha1.Seat) string { return s.Spec.CompanyRef.Name })
		if n > *limits.MaxSeats {
			return limitExceeded("Seats", n, class.Name, "maxSeats", *limits.MaxSeats), nil
		}
	}
	if limits.MaxMCPServers != nil {
		servers := &crewletaiv1alpha1.MCPServerList{}
		if err := r.List(ctx, servers, client.InNamespace(company.Namespace)); err != nil {
			return metav1.Condition{}, fmt.Errorf("listing the MCPServers of Company %s/%s: %w", company.Namespace, company.Name, err)
		}
		n := countOf(servers.Items, company.Name, func(s crewletaiv1alpha1.MCPServer) string { return s.Spec.CompanyRef.Name })
		if n > *limits.MaxMCPServers {
			return limitExceeded("MCPServers", n, class.Name, "maxMCPServers", *limits.MaxMCPServers), nil
		}
	}

	return metav1.Condition{
		Status:  metav1.ConditionTrue,
		Reason:  crewletaiv1alpha1.ReasonValid,
		Message: fmt.Sprintf("The Company runs with EngineClass %s and is within its limits.", class.Name),
	}, nil
}

// engineClass resolves the EngineClass a Company runs with: the one it names,
// or else the one class annotated as the default. When it cannot, it returns
// the Accepted=False condition that says why instead.
func (r *CompanyReconciler) engineClass(
	ctx context.Context, company *crewletaiv1alpha1.Company,
) (*crewletaiv1alpha1.EngineClass, *metav1.Condition, error) {
	if name := company.Spec.EngineClassName; name != "" {
		class := &crewletaiv1alpha1.EngineClass{}
		err := r.Get(ctx, types.NamespacedName{Name: name}, class)
		if apierrors.IsNotFound(err) {
			return nil, classNotFound(fmt.Sprintf("EngineClass %s does not exist.", name)), nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("reading EngineClass %s: %w", name, err)
		}
		return class, nil, nil
	}

	classes := &crewletaiv1alpha1.EngineClassList{}
	if err := r.List(ctx, classes); err != nil {
		return nil, nil, fmt.Errorf("listing EngineClasses: %w", err)
	}
	var defaults []*crewletaiv1alpha1.EngineClass
	for i := range classes.Items {
		if classes.Items[i].Annotations[crewletaiv1alpha1.DefaultEngineClassAnnotation] == "true" {
			defaults = append(defaults, &classes.Items[i])
		}
	}
	switch len(defaults) {
	case 1:
		return defaults[0], nil, nil
	case 0:
		return nil, classNotFound(fmt.Sprintf(
			"spec.engineClassName is unset and no EngineClass is annotated %s=true.",
			crewletaiv1alpha1.DefaultEngineClassAnnotation)), nil
	default:
		// Picking one would make which class a Company gets depend on list
		// order; the platform has to settle it.
		names := make([]string, 0, len(defaults))
		for _, c := range defaults {
			names = append(names, c.Name)
		}
		slices.Sort(names)
		return nil, classNotFound(fmt.Sprintf(
			"spec.engineClassName is unset and %d EngineClasses are annotated %s=true (%s): name one, or leave one default.",
			len(defaults), crewletaiv1alpha1.DefaultEngineClassAnnotation, strings.Join(names, ", "))), nil
	}
}

func classNotFound(message string) *metav1.Condition {
	return &metav1.Condition{
		Status:  metav1.ConditionFalse,
		Reason:  crewletaiv1alpha1.ReasonEngineClassNotFound,
		Message: message,
	}
}

func limitExceeded(kind string, n int32, class, limit string, max int32) metav1.Condition {
	return metav1.Condition{
		Status: metav1.ConditionFalse,
		Reason: crewletaiv1alpha1.ReasonLimitExceeded,
		Message: fmt.Sprintf("The Company has %d %s; EngineClass %s allows %d (spec.limits.%s).",
			n, kind, class, max, limit),
	}
}

// countOf counts the items that belong to the company named.
func countOf[T any](items []T, company string, companyOf func(T) string) int32 {
	var n int32
	for _, item := range items {
		if companyOf(item) == company {
			n++
		}
	}
	return n
}

// SetupWithManager sets up the controller with the Manager.
//
// A Company is re-run by its own spec changes, by a change of the
// ConfigApplied condition the config reconciler writes (Ready summarises it),
// and by changes to what it owns. Its acceptance and its engine also depend on
// objects it does not own: the EngineClasses (one may appear, disappear,
// change, or become the default), the Seats and MCPServers its limits count,
// the platform's Secrets its class copies into its namespace, and a keyring
// or tokens Secret another controller supplies. Each of those enqueues the
// Companies it can affect.
//
// Everything owned except the StatefulSet is watched as metadata only: the
// reconciler writes those objects whole with server-side apply and reads the
// status it needs from the apply's answer, so a full cache of every Secret,
// ConfigMap and Service in the cluster would buy nothing.
func (r *CompanyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&crewletaiv1alpha1.Company{}, builder.WithPredicates(predicate.Or[client.Object](
			predicate.GenerationChangedPredicate{}, configAppliedChanged()))).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}, builder.OnlyMetadata).
		Owns(&corev1.ConfigMap{}, builder.OnlyMetadata).
		Owns(&corev1.ServiceAccount{}, builder.OnlyMetadata).
		Owns(&corev1.Secret{}, builder.OnlyMetadata).
		Owns(&networkingv1.Ingress{}, builder.OnlyMetadata).
		Owns(&networkingv1.NetworkPolicy{}, builder.OnlyMetadata).
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.companiesOfSecret)).
		Watches(&crewletaiv1alpha1.EngineClass{}, handler.EnqueueRequestsFromMapFunc(r.companiesOfClass)).
		Watches(&crewletaiv1alpha1.Seat{}, handler.EnqueueRequestsFromMapFunc(companyOfChild)).
		Watches(&crewletaiv1alpha1.MCPServer{}, handler.EnqueueRequestsFromMapFunc(companyOfChild))
	// Without the Gateway API's CRDs a watch on HTTPRoutes would fail the
	// manager's start; GatewayAPI is false there, and nothing reads them.
	if r.GatewayAPI {
		b = b.Owns(&gatewayv1.HTTPRoute{}, builder.OnlyMetadata)
	}
	return b.Named("company").Complete(r)
}

// configAppliedChanged passes a Company update that changes its
// ConfigApplied condition, which the config reconciler writes and the Ready
// summary depends on. The reconciler's own status writes do not pass it.
func configAppliedChanged() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old, ok := e.ObjectOld.(*crewletaiv1alpha1.Company)
			if !ok {
				return false
			}
			updated, ok := e.ObjectNew.(*crewletaiv1alpha1.Company)
			if !ok {
				return false
			}
			before := meta.FindStatusCondition(old.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied)
			after := meta.FindStatusCondition(updated.Status.Conditions, crewletaiv1alpha1.ConditionConfigApplied)
			switch {
			case before == nil || after == nil:
				return (before == nil) != (after == nil)
			default:
				return before.Status != after.Status || before.Reason != after.Reason || before.Message != after.Message
			}
		},
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// companiesOfSecret maps a Secret to the Companies it can affect beyond the
// one that owns it (Owns covers that):
//
//   - the Companies whose class copies a value from it into their namespace
//     (platformValues), so that rotating a platform credential reaches every
//     engine that uses it;
//   - the Company whose keyring or API tokens it is by name, which matters
//     when the Secret is supplied by another controller and so not owned.
func (r *CompanyReconciler) companiesOfSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request
	for _, suffix := range []string{"-keyring", "-api-tokens"} {
		if company, ok := strings.CutSuffix(obj.GetName(), suffix); ok && company != "" {
			requests = append(requests, reconcile.Request{Namespace: obj.GetNamespace(), Name: company})
		}
	}
	classes := &crewletaiv1alpha1.EngineClassList{}
	if err := r.List(ctx, classes); err != nil {
		// See companiesOfClass.
		logf.FromContext(ctx).Error(err, "Failed to list the EngineClasses a Secret change affects", "secret", client.ObjectKeyFromObject(obj))
		return requests
	}
	for i := range classes.Items {
		class := &classes.Items[i]
		if !referencesPlatformSecret(class, obj.GetNamespace(), obj.GetName()) {
			continue
		}
		requests = append(requests, r.companiesOfClass(ctx, class)...)
	}
	return requests
}

// referencesPlatformSecret reports whether a class copies a value from the
// Secret named into its companies' namespaces. Company-level overrides do not
// matter here: a spurious reconcile is cheap, a missed one is not.
func referencesPlatformSecret(class *crewletaiv1alpha1.EngineClass, namespace, name string) bool {
	is := func(ns, n string) bool { return ns == namespace && n == name }
	if ref := class.Spec.Objects.S3.CredentialsSecretRef; is(ref.Namespace, ref.Name) {
		return true
	}
	if ref := class.Spec.Telemetry.HeadersSecretRef; is(ref.Namespace, ref.Name) {
		return true
	}
	return slices.ContainsFunc(class.Spec.PodTemplate.Spec.Env, func(e crewletaiv1alpha1.ClassEnvVar) bool {
		return is(e.ValueFrom.SecretKeyRef.Namespace, e.ValueFrom.SecretKeyRef.Name)
	})
}

// companiesOfClass maps an EngineClass to every Company that names it, and to
// every Company that names no class, since a change of the default annotation
// can change the class those run with.
func (r *CompanyReconciler) companiesOfClass(ctx context.Context, obj client.Object) []reconcile.Request {
	companies := &crewletaiv1alpha1.CompanyList{}
	if err := r.List(ctx, companies); err != nil {
		// The map function cannot return an error. The cache answers this
		// list from memory, so it fails only while the cache is not synced,
		// before any event can be delivered; log it rather than drop it
		// silently.
		logf.FromContext(ctx).Error(err, "Failed to list the Companies an EngineClass change affects", "engineClass", obj.GetName())
		return nil
	}
	var requests []reconcile.Request
	for i := range companies.Items {
		c := &companies.Items[i]
		if c.Spec.EngineClassName == "" || c.Spec.EngineClassName == obj.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(c)})
		}
	}
	return requests
}

// companyOfChild maps a Seat or an MCPServer to the Company it names.
func companyOfChild(_ context.Context, obj client.Object) []reconcile.Request {
	var company string
	switch child := obj.(type) {
	case *crewletaiv1alpha1.Seat:
		company = child.Spec.CompanyRef.Name
	case *crewletaiv1alpha1.MCPServer:
		company = child.Spec.CompanyRef.Name
	default:
		return nil
	}
	return []reconcile.Request{{Namespace: obj.GetNamespace(), Name: company}}
}
