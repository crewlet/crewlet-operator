package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// CompanyReconciler reconciles Company objects. Today it decides whether a
// Company is one the operator can act on, and reports that as the Accepted
// condition: its EngineClass resolves, and its children stay within the class's
// limits.
type CompanyReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=crewlet.ai,resources=companies,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=crewlet.ai,resources=engineclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=seats;mcpservers,verbs=get;list;watch

// Reconcile sets a Company's Accepted condition and observedGeneration.
func (r *CompanyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	company := &crewletaiv1alpha1.Company{}
	if err := r.Get(ctx, req.NamespacedName, company); err != nil {
		// A request can outlive its object: the key of a Company deleted
		// since it was queued is not an error.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	accepted, err := r.accept(ctx, company)
	if err != nil {
		return ctrl.Result{}, err
	}
	accepted.Type = crewletaiv1alpha1.ConditionAccepted
	accepted.ObservedGeneration = company.Generation

	base := company.DeepCopy()
	meta.SetStatusCondition(&company.Status.Conditions, accepted)
	company.Status.ObservedGeneration = company.Generation
	if err := r.Status().Patch(ctx, company, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating the status of Company %s: %w", req.NamespacedName, err)
	}
	logf.FromContext(ctx).V(1).Info("Reconciled the Company", "accepted", accepted.Status, "reason", accepted.Reason)
	return ctrl.Result{}, nil
}

// accept returns the Accepted condition of a Company, without its type and
// generation. An error is one the reconcile should be retried for (the API
// server did not answer); everything about the Company itself is a condition.
func (r *CompanyReconciler) accept(ctx context.Context, company *crewletaiv1alpha1.Company) (metav1.Condition, error) {
	class, refused, err := r.engineClass(ctx, company)
	if err != nil {
		return metav1.Condition{}, err
	}
	if refused != nil {
		return *refused, nil
	}

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
// A Company's acceptance also depends on objects it does not own: the
// EngineClasses (one may appear, disappear, change its limits or become the
// default) and the Seats and MCPServers its limits count. Each of those
// enqueues the Companies it can affect.
func (r *CompanyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Only a spec change, never the controller's own status writes,
		// re-runs a Company.
		For(&crewletaiv1alpha1.Company{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&crewletaiv1alpha1.EngineClass{}, handler.EnqueueRequestsFromMapFunc(r.companiesOfClass)).
		Watches(&crewletaiv1alpha1.Seat{}, handler.EnqueueRequestsFromMapFunc(companyOfChild)).
		Watches(&crewletaiv1alpha1.MCPServer{}, handler.EnqueueRequestsFromMapFunc(companyOfChild)).
		Named("company").
		Complete(r)
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
