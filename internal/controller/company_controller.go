package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// CompanyReconciler reconciles Company objects.
type CompanyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=crewlet.ai,resources=companies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/finalizers,verbs=update

// Reconcile moves the cluster toward what a Company declares. The API
// is defined and its rules are enforced by the API server, but the controller
// does not act on it yet: it reports success without touching the cluster.
func (r *CompanyReconciler) Reconcile(_ context.Context, _ ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CompanyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crewletaiv1alpha1.Company{}).
		Named("company").
		Complete(r)
}
