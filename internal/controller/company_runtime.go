package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// drainPollInterval is how often a Company being deleted checks whether its
// data pods have stopped. Nothing the operator watches announces it: the
// StatefulSet is already gone, and the operator does not watch Pods.
const drainPollInterval = 5 * time.Second

// reconcileRuntime makes an accepted Company's engine exist and be reachable:
// its Secrets, bootstrap document, ServiceAccount, Services, routes, network
// policy and data StatefulSet. It records what it finds in the Company's
// status (RuntimeReady, Exposed, the endpoints, the engine and topology), which
// the caller writes.
//
// An error is one to retry (the API server did not answer, a write
// conflicted); everything about the Company or its class is a condition.
func (r *CompanyReconciler) reconcileRuntime(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	keyring, err := r.ensureKeyring(ctx, c)
	if err != nil {
		return err
	}
	tokens, err := r.ensureAPITokens(ctx, c)
	if err != nil {
		return err
	}
	// A tokens Secret the platform supplies (see ensureGenerated) may lack
	// a token; the pods could not start without it, and consumers must not
	// be handed an empty one.
	missing := missingTokens(tokens)
	if len(missing) == 0 {
		if err := r.applyAPIAccess(ctx, c, tokens); err != nil {
			return err
		}
	}
	c.Status.APIAccessSecretRef = crewletaiv1alpha1.SecretReference{Name: apiAccessSecretName(c)}
	c.Status.Endpoints.API = apiURL(c)

	platformVersion, platformMissing, err := r.applyPlatformSecret(ctx, c, class)
	if err != nil {
		return err
	}
	missing = append(missing, platformMissing...)
	if err := r.applyServiceAccount(ctx, c, class); err != nil {
		return err
	}
	if err := r.applyServices(ctx, c); err != nil {
		return err
	}
	exposed, err := r.reconcileExposure(ctx, c, class)
	if err != nil {
		return err
	}
	exposed.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, exposed)
	if err := r.reconcileNetworkPolicy(ctx, c, class); err != nil {
		return err
	}

	runtimeReady, err := r.reconcileWorkload(ctx, c, class, keyring, tokens, platformVersion, missing)
	if err != nil {
		return err
	}
	runtimeReady.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, runtimeReady)
	return nil
}

// reconcileWorkload writes the bootstrap ConfigMap and the data StatefulSet
// and returns the RuntimeReady condition.
//
// What cannot be rendered (a keyring the engine could not open, a platform
// Secret that is missing) holds the pods back: the StatefulSet keeps running
// what it runs, and is only scaled to zero if the Company is suspended
// meanwhile. An engine image that is refused does not hold back anything else:
// the pods keep the image they run, and every other change still applies.
func (r *CompanyReconciler) reconcileWorkload(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	keyring, tokens *corev1.Secret, platformVersion string, missing []string,
) (metav1.Condition, error) {
	existing, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return metav1.Condition{}, err
	}

	if len(missing) > 0 {
		return r.holdBack(ctx, c, existing, crewletaiv1alpha1.ReasonSecretNotFound,
			"The engine's pods cannot be given the values they read from Secrets: "+strings.Join(missing, "; ")+".")
	}
	bootstrap, err := render.Bootstrap(&render.BootstrapInput{
		Company: c,
		Class:   class,
		Keyring: keyringOf(keyring),
		Secrets: render.BootstrapSecrets{
			Keyring:   keyring.Name,
			APITokens: tokens.Name,
			Objects:   platformSecretName(c),
		},
		VolumeSize: dataVolumeSize(existing, class),
	})
	if err != nil {
		return r.holdBack(ctx, c, existing, crewletaiv1alpha1.ReasonKeyringInvalid,
			fmt.Sprintf("%s. The operator never rewrites a keyring, since the engine's sealed data opens only with its keys: "+
				"repair the Secret, setting the annotation %s to the id of the key that seals new writes.",
				capitalize(err.Error()), KeyringActiveKeyAnnotation))
	}

	image, refusal := engineImage(c, class)
	if refusal != nil {
		if existing == nil {
			return runtimeCondition(metav1.ConditionFalse, refusal.reason, refusal.message), nil
		}
		image = runningImage(existing)
	}

	if err := r.applyBootstrap(ctx, c, bootstrap); err != nil {
		return metav1.Condition{}, err
	}
	sts, err := r.applyStatefulSet(ctx, c, class, existing, &workload{
		image:          image,
		bootstrap:      bootstrap,
		secretsVersion: secretsVersion(keyring, tokens, platformVersion),
	})
	if err != nil {
		return metav1.Condition{}, err
	}
	progress := rolloutOf(sts)
	recordRollout(c, sts, progress.ready)

	switch {
	case refusal != nil:
		return runtimeCondition(metav1.ConditionFalse, refusal.reason, refusal.message+" The data pods keep running "+image+"."), nil
	case suspended(c):
		return runtimeCondition(metav1.ConditionFalse, crewletaiv1alpha1.ReasonSuspended,
			"The Company is suspended: its data pods are stopped and its data volumes kept."), nil
	case progress.ready:
		return runtimeCondition(metav1.ConditionTrue, crewletaiv1alpha1.ReasonPodsReady, progress.message), nil
	default:
		return runtimeCondition(metav1.ConditionFalse, crewletaiv1alpha1.ReasonPodsNotReady, progress.message), nil
	}
}

// holdBack leaves the data StatefulSet as it is, except that a suspended
// Company's is scaled to zero: suspending is how a platform stops an engine,
// and it must work while something else is wrong. The scale is the one change
// that needs nothing rendered.
func (r *CompanyReconciler) holdBack(
	ctx context.Context, c *crewletaiv1alpha1.Company, existing *appsv1.StatefulSet, reason, message string,
) (metav1.Condition, error) {
	if existing != nil && suspended(c) && (existing.Spec.Replicas == nil || *existing.Spec.Replicas != 0) {
		base := existing.DeepCopy()
		existing.Spec.Replicas = new(int32(0))
		if err := r.Patch(ctx, existing, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
			return metav1.Condition{}, fmt.Errorf("scaling StatefulSet %s/%s to zero: %w", existing.Namespace, existing.Name, err)
		}
	}
	return runtimeCondition(metav1.ConditionFalse, reason, message), nil
}

// capitalize makes an error message, which starts in lower case, the start of
// a sentence.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// apply writes an object with server-side apply, as the operator's field
// manager, taking over any field another manager set on it. The response,
// status included, is decoded into obj.
func (r *CompanyReconciler) apply(ctx context.Context, obj runtime.ApplyConfiguration, kind, name, namespace string) error {
	if err := r.Apply(ctx, obj, client.FieldOwner(FieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying %s %s/%s: %w", kind, namespace, name, err)
	}
	return nil
}

// finalize stops a deleted Company's engine and settles its data volumes,
// then lets the deletion finish.
//
//  1. The data StatefulSet is deleted and the operator waits until no data
//     pod is left. Each pod drains on SIGTERM: running turns finish,
//     waiting ones are handed back. The Company's other objects stay until
//     then, so a draining engine keeps its Services, Secrets and routes.
//  2. With deletionPolicy Delete the data volume claims are deleted. With
//     Retain they are left, and so is the keyring Secret, which is released
//     from the Company: the volumes cannot be read without it.
//  3. The finalizer is removed; the garbage collector deletes everything else
//     the Company owns.
func (r *CompanyReconciler) finalize(ctx context.Context, c *crewletaiv1alpha1.Company) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(c, crewletaiv1alpha1.CompanyFinalizer) {
		return ctrl.Result{}, nil
	}
	log := logf.FromContext(ctx)

	sts, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return ctrl.Result{}, err
	}
	if sts != nil {
		if err := r.deleteIfControlled(ctx, c, sts); err != nil {
			return ctrl.Result{}, err
		}
	}
	pods := &corev1.PodList{}
	if err := r.APIReader.List(ctx, pods, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing the data pods of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	if len(pods.Items) > 0 {
		log.V(1).Info("Waiting for the data pods to stop", "pods", len(pods.Items))
		return ctrl.Result{RequeueAfter: drainPollInterval}, nil
	}

	switch c.Spec.DeletionPolicy {
	case crewletaiv1alpha1.DeletionPolicyDelete:
		if err := r.deleteDataVolumes(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
	case crewletaiv1alpha1.DeletionPolicyRetain:
		if err := r.orphanKeyring(ctx, c); err != nil {
			return ctrl.Result{}, err
		}
	}

	base := c.DeepCopy()
	controllerutil.RemoveFinalizer(c, crewletaiv1alpha1.CompanyFinalizer)
	if err := r.Patch(ctx, c, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing the finalizer of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	log.Info("Released the deleted Company", "deletionPolicy", c.Spec.DeletionPolicy)
	return ctrl.Result{}, nil
}

// deleteDataVolumes deletes a Company's data volume claims. The
// StatefulSet's own retention policy says the same, but it acts through the
// garbage collector after the StatefulSet is gone and only on claims it has
// already marked, which a deletionPolicy changed just before the deletion
// has not reached; deleting them here makes the policy hold whenever it was
// set.
func (r *CompanyReconciler) deleteDataVolumes(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	claims := &corev1.PersistentVolumeClaimList{}
	if err := r.APIReader.List(ctx, claims, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return fmt.Errorf("listing the data volume claims of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if err := r.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PersistentVolumeClaim %s/%s: %w", claim.Namespace, claim.Name, err)
		}
	}
	return nil
}
