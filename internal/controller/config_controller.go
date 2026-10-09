package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// driftCheckInterval is how often the config reconciler looks at a Company's
// engine when nothing it watches has changed. The engine's document can be
// edited through its own API and dashboard, which no watch announces; this
// bounds how long such an edit runs before Enforce overwrites it, and how
// stale an Integration's Healthy condition gets.
const driftCheckInterval = 5 * time.Minute

// engineRetryInterval is how soon the config reconciler tries a Company's
// engine again after it could not reach it. A data pod that is starting is
// the usual cause, and nothing the operator watches says when it answers:
// the pod is not ready, and does not become ready, until it has a document.
const engineRetryInterval = 10 * time.Second

// applyPollInterval is how soon the config reconciler looks again after it
// applied a revision. The engine activates a revision when the write returns
// and applies it asynchronously, and the integrations' state it reports is
// the applied revision's, so the first look right after a write would read
// the previous one.
const applyPollInterval = 2 * time.Second

// CompanyConfigReconciler keeps the company document a Company's engine runs
// equal to what the Company and its parts (Units, Seats, LLMProviders,
// MCPServers, Integrations) declare, as the Company's
// spec.config.managementPolicy says.
//
// It renders the whole document, pushes the Secret values it references
// into the engine's secret store, has the engine dry-run it, and applies it
// as one revision, or reports on the object at fault why it cannot. It owns
// the Company's SecretsSynced and ConfigApplied conditions and status.config,
// and the whole status of every part; the Company's other status fields are
// the CompanyReconciler's. Both write the Company's status with a merge patch
// under an optimistic lock, re-reading it on a conflict, so neither
// overwrites what the other wrote.
type CompanyConfigReconciler struct {
	client.Client

	// APIReader reads from the API server directly: the values of the
	// Secrets a company references, which the manager's cache holds as
	// metadata only, and the Company itself after a conflicting status
	// write, when the cache may not have caught up yet.
	APIReader client.Reader

	// Engines finds each Company's engine.
	Engines EngineResolver

	// Recorder records the events the reconciler emits on Companies.
	Recorder events.EventRecorder

	// ResyncInterval is how often a Company whose engine answered is looked
	// at again with nothing having changed. Zero is driftCheckInterval.
	ResyncInterval time.Duration

	changes changeClock
}

// Writes are patches, never updates. Secrets are read with get (values,
// through the APIReader) and watched as metadata; the reconciler writes one
// Secret per Company, the secret sync's state, with server-side apply.
//
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=companies/status,verbs=get;patch
// +kubebuilder:rbac:groups=crewlet.ai,resources=engineclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=units;seats;llmproviders;mcpservers;integrations,verbs=get;list;watch
// +kubebuilder:rbac:groups=crewlet.ai,resources=units/status;seats/status;llmproviders/status;mcpservers/status;integrations/status,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile brings a Company's document in its engine to what its objects
// declare, and records the outcome on the Company and on each of its parts.
func (r *CompanyConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	company := &crewletaiv1alpha1.Company{}
	if err := r.Get(ctx, req.NamespacedName, company); err != nil {
		if apierrors.IsNotFound(err) {
			r.changes.forget(req.NamespacedName)
			return ctrl.Result{}, r.orphan(ctx, req.NamespacedName)
		}
		return ctrl.Result{}, fmt.Errorf("reading Company %s: %w", req.NamespacedName, err)
	}
	if !company.DeletionTimestamp.IsZero() {
		// The runtime reconciler stops the engine; there is no document
		// left to keep.
		r.changes.forget(req.NamespacedName)
		return ctrl.Result{}, nil
	}
	// Before anything is rendered: a burst is often an object and what it
	// references, created in some order, and reporting the reference as
	// missing for the instant between the two would be noise.
	if wait := r.changes.unsettled(req.NamespacedName); wait > 0 {
		return ctrl.Result{RequeueAfter: wait}, nil
	}

	parts, err := r.partsOf(ctx, company)
	if err != nil {
		return ctrl.Result{}, err
	}
	run := &configRun{CompanyConfigReconciler: r, company: company, parts: parts}
	result, err := run.reconcile(ctx)
	if statusErr := run.writeStatus(ctx); statusErr != nil {
		if err == nil {
			err = statusErr
		} else {
			err = fmt.Errorf("%w; and %w", err, statusErr)
		}
	}
	return result, err
}

func (r *CompanyConfigReconciler) resyncInterval() time.Duration {
	if r.ResyncInterval > 0 {
		return r.ResyncInterval
	}
	return driftCheckInterval
}

// partsOf lists the parts of a company from the cache, by companyRefIndex.
func (r *CompanyConfigReconciler) partsOf(ctx context.Context, company *crewletaiv1alpha1.Company) ([]*part, error) {
	var parts []*part
	for _, list := range partLists() {
		if err := r.List(ctx, list, client.InNamespace(company.Namespace),
			client.MatchingFields{companyRefIndex: company.Name}); err != nil {
			return nil, fmt.Errorf("listing the parts of Company %s/%s: %w", company.Namespace, company.Name, err)
		}
		for _, obj := range itemsOf(list) {
			parts = append(parts, newPart(obj))
		}
	}
	return parts, nil
}

// orphan reports, on every part that names a Company that does not exist,
// that it is not accepted.
func (r *CompanyConfigReconciler) orphan(ctx context.Context, company types.NamespacedName) error {
	missing := &crewletaiv1alpha1.Company{}
	missing.Namespace, missing.Name = company.Namespace, company.Name
	parts, err := r.partsOf(ctx, missing)
	if err != nil {
		return err
	}
	for _, p := range parts {
		setCondition(p.conditions(), p.obj.GetGeneration(), crewletaiv1alpha1.ConditionAccepted, false,
			crewletaiv1alpha1.ReasonCompanyNotFound,
			fmt.Sprintf("Company %s does not exist in namespace %s.", company.Name, company.Namespace))
		p.setObservedGeneration()
		if err := r.patchPartStatus(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
//
// A Company is re-run by a change of its spec; by a change of what the
// runtime reconciler found that decides whether there is an engine to talk
// to (Accepted, RuntimeReady, the public base URL); by any change to one of
// its parts or to a Secret it references, through the indexes; and by a
// change to its EngineClass's platform model providers. Status writes to the
// parts, which are this reconciler's own, change no generation and so run
// nothing.
func (r *CompanyConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	indexer := mgr.GetFieldIndexer()
	for _, list := range partLists() {
		obj := itemsOfKind(list)
		if err := indexer.IndexField(ctx, obj, companyRefIndex, indexCompanyRef); err != nil {
			return fmt.Errorf("indexing %T by %s: %w", obj, companyRefIndex, err)
		}
		if err := indexer.IndexField(ctx, obj, refIndex, indexSecretRefs); err != nil {
			return fmt.Errorf("indexing %T by %s: %w", obj, refIndex, err)
		}
	}
	for _, obj := range []client.Object{&crewletaiv1alpha1.Company{}, &crewletaiv1alpha1.EngineClass{}} {
		if err := indexer.IndexField(ctx, obj, refIndex, indexSecretRefs); err != nil {
			return fmt.Errorf("indexing %T by %s: %w", obj, refIndex, err)
		}
	}

	parts := handler.EnqueueRequestsFromMapFunc(r.companyOfPart)
	specChanged := builder.WithPredicates(predicate.GenerationChangedPredicate{})
	b := ctrl.NewControllerManagedBy(mgr).
		For(&crewletaiv1alpha1.Company{}, builder.WithPredicates(predicate.Or[client.Object](
			predicate.GenerationChangedPredicate{}, engineStateChanged())))
	for _, list := range partLists() {
		b = b.Watches(itemsOfKind(list), parts, specChanged)
	}
	return b.
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.companiesOfSecret)).
		// Moving the default-class annotation changes no generation, and
		// changes the class of every Company that names none.
		Watches(&crewletaiv1alpha1.EngineClass{}, handler.EnqueueRequestsFromMapFunc(r.companiesOfClass),
			builder.WithPredicates(predicate.Or[client.Object](predicate.GenerationChangedPredicate{}, defaultClassChanged()))).
		Named("companyconfig").
		Complete(r)
}

// defaultClassChanged passes an EngineClass update that sets, removes or
// changes the DefaultEngineClassAnnotation.
func defaultClassChanged() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetAnnotations()[crewletaiv1alpha1.DefaultEngineClassAnnotation] !=
				e.ObjectNew.GetAnnotations()[crewletaiv1alpha1.DefaultEngineClassAnnotation]
		},
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// itemsOfKind returns an empty object of the kind a list partLists returned
// holds.
func itemsOfKind(list client.ObjectList) client.Object {
	switch list.(type) {
	case *crewletaiv1alpha1.UnitList:
		return &crewletaiv1alpha1.Unit{}
	case *crewletaiv1alpha1.SeatList:
		return &crewletaiv1alpha1.Seat{}
	case *crewletaiv1alpha1.LLMProviderList:
		return &crewletaiv1alpha1.LLMProvider{}
	case *crewletaiv1alpha1.MCPServerList:
		return &crewletaiv1alpha1.MCPServer{}
	default:
		return &crewletaiv1alpha1.Integration{}
	}
}

// engineStateChanged passes a Company update that changes what decides
// whether, and where, its document can be applied: whether it is Accepted,
// the state of its engine's pods (RuntimeReady), and the public base URL the
// document carries. These are status fields the runtime reconciler writes;
// the config reconciler's own status writes change none of them.
func engineStateChanged() predicate.Funcs {
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
			if old.Status.Endpoints.PublicBaseURL != updated.Status.Endpoints.PublicBaseURL {
				return true
			}
			if meta.IsStatusConditionTrue(old.Status.Conditions, crewletaiv1alpha1.ConditionAccepted) !=
				meta.IsStatusConditionTrue(updated.Status.Conditions, crewletaiv1alpha1.ConditionAccepted) {
				return true
			}
			before := meta.FindStatusCondition(old.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady)
			after := meta.FindStatusCondition(updated.Status.Conditions, crewletaiv1alpha1.ConditionRuntimeReady)
			if before == nil || after == nil {
				return (before == nil) != (after == nil)
			}
			return before.Status != after.Status || before.Reason != after.Reason
		},
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}
