package controller

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
// the caller writes, and in retry when it has to run again for a change
// nothing it watches announces.
//
// An error is one to retry (the API server did not answer, a write
// conflicted); everything about the Company or its class is a condition.
func (r *CompanyReconciler) reconcileRuntime(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, retry *requeue,
) error {
	err := r.reconcileRuntimeObjects(ctx, c, class, retry)
	conflict, ok := errors.AsType[*nameConflictError](err)
	if !ok {
		return err
	}
	// An object the Company does not own stops the runtime where it was
	// met; the pods are held back as they are, and the condition names the
	// object.
	existing, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return err
	}
	runtimeReady, err := r.holdBack(ctx, c, existing, crewletaiv1alpha1.ReasonNameConflict, conflict.message())
	if err != nil {
		return err
	}
	runtimeReady.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, runtimeReady)
	return nil
}

// reconcileRuntimeObjects writes the objects of an accepted Company's
// runtime, in order; see reconcileRuntime.
func (r *CompanyReconciler) reconcileRuntimeObjects(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, retry *requeue,
) error {
	keyring, keyringHeld, err := r.ensureKeyring(ctx, c)
	if err != nil {
		return err
	}
	tokens, tokensHeld, err := r.ensureAPITokens(ctx, c)
	if err != nil {
		return err
	}
	if tokens != nil && tokensHeld == nil {
		tokensHeld = checkTokens(tokens)
		if tokensHeld == nil {
			if err := r.applyAPIAccess(ctx, c, tokens); err != nil {
				return err
			}
		}
	}
	hold := cmp.Or(keyringHeld, tokensHeld)
	c.Status.APIAccessSecretRef = crewletaiv1alpha1.SecretReference{Name: apiAccessSecretName(c)}
	c.Status.Endpoints.API = apiURL(c)

	platform, platformMissing, err := r.readPlatformValues(ctx, c, class)
	if err != nil {
		return err
	}
	missing := platformMissing
	objects, objectsMissing, err := r.readObjectCredentials(ctx, c, class)
	if err != nil {
		return err
	}
	missing = append(missing, objectsMissing...)
	if err := r.applyServiceAccount(ctx, c, class); err != nil {
		return err
	}
	if err := r.applyServices(ctx, c, class); err != nil {
		return err
	}
	exposed, addresses, err := r.reconcileExposure(ctx, c, class)
	if err != nil {
		return err
	}
	exposed.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, exposed)
	if err := r.reconcileNetworkPolicy(ctx, c, class); err != nil {
		return err
	}

	bridge, err := r.toolBridge(ctx, c)
	if err != nil {
		return err
	}
	var bridgeURL string
	if bridge {
		bridgeURL = c.Status.Endpoints.PublicBaseURL
	}
	runtimeReady, err := r.reconcileWorkload(ctx, c, class,
		&workloadInputs{keyring, tokens, objects, platform, missing, hold, bridgeURL, addresses, retry})
	if err != nil {
		return err
	}
	runtimeReady.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, runtimeReady)
	return nil
}

// workloadInputs is what the data pods are started from, as
// reconcileRuntimeObjects read it.
type workloadInputs struct {
	keyring, tokens *corev1.Secret
	// objects are the S3 keys of the Company's own, or nil.
	objects *render.S3Credentials
	// platform are the class's values for the engine's environment, keyed
	// as the bootstrap Secret holds them.
	platform map[string][]byte
	// missing are the values that could not be read; hold is why the pods
	// are held back otherwise.
	missing []string
	hold    *held
	// bridgeURL is where the engine's tool bridge is reached from a
	// sandbox, or empty when the engine serves none.
	bridgeURL string
	// addresses are where the engine is reached, which its crewlet.yaml
	// tells it.
	addresses engineAddresses
	// retry collects when the reconcile has to run again.
	retry *requeue
}

// requeue is the soonest a reconcile has to run again for a change that no
// object the operator watches announces, such as a pod staying unready for
// long enough; zero is never.
type requeue struct{ after time.Duration }

// within asks for the reconcile to run again within d, unless something
// already asked for sooner; a d of zero asks for nothing.
func (q *requeue) within(d time.Duration) {
	if d > 0 && (q.after == 0 || d < q.after) {
		q.after = d
	}
}

// reconcileWorkload writes the bootstrap Secret and the data StatefulSet and
// returns the RuntimeReady condition.
//
// What cannot be rendered (a keyring the engine could not open or that is
// gone, a platform Secret that is missing, a sign-in the engine would refuse
// at the address it is reached at) holds the pods back: the StatefulSet keeps running
// what it runs, and is only scaled to zero if the Company is suspended
// meanwhile. An engine image that is refused does not hold back anything else:
// the pods keep the image they run, and every other change still applies.
func (r *CompanyReconciler) reconcileWorkload(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	in *workloadInputs,
) (metav1.Condition, error) {
	existing, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return metav1.Condition{}, err
	}
	pools, err := r.planSeatPools(ctx, c, class)
	if err != nil {
		return metav1.Condition{}, err
	}
	// What is held back holds the seat pods back with the data pods: they
	// keep running what they run.
	holdBack := func(reason, message string) (metav1.Condition, error) {
		recordDataStatus(c, existing)
		if err := r.reconcileSeatPools(ctx, c, class, pools, nil); err != nil {
			return metav1.Condition{}, err
		}
		return r.holdBack(ctx, c, existing, reason, message)
	}
	if in.hold != nil {
		return holdBack(in.hold.reason, in.hold.message)
	}
	if len(in.missing) > 0 {
		return holdBack(crewletaiv1alpha1.ReasonSecretNotFound,
			"The engine's pods cannot be given the values they read from Secrets: "+strings.Join(in.missing, "; ")+".")
	}
	return r.runWorkload(ctx, c, class, in, existing, pools)
}

// runWorkload is reconcileWorkload for a Company whose pods are not held
// back: it writes the data pods' bootstrap Secret and StatefulSet, then the
// seat pools after them.
func (r *CompanyReconciler) runWorkload(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	in *workloadInputs, existing *appsv1.StatefulSet, pools *seatPlan,
) (metav1.Condition, error) {
	keyring, tokens, platform := in.keyring, in.tokens, in.platform
	// A suspended company stops its seat pods first, and its data pods
	// only once they are gone.
	seatPodsLeft := false
	if suspended(c) {
		left, err := r.stopSeatPods(ctx, c)
		if err != nil {
			return metav1.Condition{}, err
		}
		seatPodsLeft = left
	}
	holdBack := func(reason, message string) (metav1.Condition, error) {
		recordDataStatus(c, existing)
		if err := r.reconcileSeatPools(ctx, c, class, pools, nil); err != nil {
			return metav1.Condition{}, err
		}
		return r.holdBack(ctx, c, existing, reason, message)
	}
	bootstrap, err := render.Bootstrap(&render.BootstrapInput{
		Company:        c,
		Class:          class,
		Keyring:        keyringOf(keyring),
		Tokens:         tokensOf(tokens),
		Objects:        in.objects,
		VolumeSize:     dataVolumeSize(existing, class),
		ExternalURL:    in.addresses.external,
		PublicURL:      in.addresses.public,
		AllowedOrigins: in.addresses.origins,
	})
	if errors.Is(err, render.ErrNoAddress) {
		// engineAddressesOf always names both, so this is the operator's
		// own fault, not the keyring's.
		return metav1.Condition{}, err
	}
	if errors.Is(err, render.ErrSignInNotAcknowledged) {
		// The engine would refuse this crewlet.yaml at every start, so the
		// pods keep the one they run, which requires a second factor or
		// was given at an address that needed no acknowledgement.
		return holdBack(crewletaiv1alpha1.ReasonInsecureSignInNotAcknowledged, fmt.Sprintf(
			"EngineClass %s lets people sign in with a password alone (spec.signIn.secondFactor Optional), and the engine "+
				"is told people reach it at %s, which is not a loopback address: Crewlet refuses to start so without the "+
				"class's acknowledgement. Set the class's spec.signIn.acceptInsecure to true to allow password-only sign-in "+
				"there, or its spec.signIn.secondFactor to Required. The pods are held as they are.",
			class.Name, in.addresses.external))
	}
	if err != nil {
		return holdBack(crewletaiv1alpha1.ReasonKeyringInvalid,
			fmt.Sprintf("Secret %s/%s is not a keyring the engine can use: %s. The operator never rewrites a keyring, since the "+
				"engine's sealed data opens only with its keys: repair the Secret, setting the annotation %s to the id of the key "+
				"that seals new writes.", keyring.Namespace, keyring.Name, err, KeyringActiveKeyAnnotation))
	}

	claims, err := r.cachedDataClaims(ctx, c)
	if err != nil {
		return metav1.Condition{}, err
	}
	image, refusal := engineImage(c, class, versionFloor(c, claims))
	if refusal != nil {
		if existing == nil {
			recordDataStatus(c, existing)
			if err := r.reconcileSeatPools(ctx, c, class, pools, nil); err != nil {
				return metav1.Condition{}, err
			}
			if suspended(c) {
				return suspendedCondition("Once it resumes, its pods are held back: " + refusal.message), nil
			}
			return runtimeCondition(metav1.ConditionFalse, refusal.reason, refusal.message), nil
		}
		image = runningImage(existing)
	}

	bootstrapHash, err := r.applyBootstrap(ctx, c, bootstrap, platform)
	if err != nil {
		return metav1.Condition{}, err
	}
	replicas := dataReplicas(c)
	if seatPodsLeft && existing != nil {
		replicas = value(existing.Spec.Replicas)
	}
	sts, err := r.applyStatefulSet(ctx, c, class, existing, replicas, &workload{
		image:          image,
		bootstrap:      bootstrap,
		bootstrapHash:  bootstrapHash,
		bridgeURL:      in.bridgeURL,
		startupSeconds: dataPodStartupSeconds(class, dataVolumeSize(existing, class)),
	})
	if err != nil {
		return metav1.Condition{}, err
	}
	progress := rolloutOf(sts)
	recordRollout(c, sts, progress.ready)
	if err := r.recordVersionOnClaims(ctx, c, claims); err != nil {
		return metav1.Condition{}, err
	}
	if err := r.reconcileSeatPools(ctx, c, class, pools, &seatInputs{
		keyring:   keyring,
		objects:   in.objects,
		platform:  platform,
		image:     *sts.Spec.Template.Spec.Containers[0].Image,
		dataReady: progress.ready && *sts.Spec.Replicas > 0,
		retry:     in.retry,
	}); err != nil {
		return metav1.Condition{}, err
	}

	switch {
	case seatPodsLeft:
		return suspendingCondition(), nil
	case suspended(c):
		// The StatefulSet was just applied with no replicas, whatever was
		// refused.
		if refusal != nil {
			return suspendedCondition("Once it resumes, its pods run " + image + ": " + refusal.message), nil
		}
		return suspendedCondition(""), nil
	case refusal != nil:
		return runtimeCondition(metav1.ConditionFalse, refusal.reason, refusal.message+" The data pods keep running "+image+"."), nil
	case progress.ready:
		return runtimeCondition(metav1.ConditionTrue, crewletaiv1alpha1.ReasonPodsReady, progress.message), nil
	default:
		return runtimeCondition(metav1.ConditionFalse, crewletaiv1alpha1.ReasonPodsNotReady, progress.message), nil
	}
}

// holdBack leaves the StatefulSets as they are, except that a suspended
// Company's are scaled to zero, its seat pods' first: suspending is how a
// platform stops an engine, and it must work while something else is wrong.
// The scale is the one change that needs nothing rendered. A suspended
// Company reports Suspended once its pods are stopped (Suspending until
// then), and says what will hold them back once it resumes.
func (r *CompanyReconciler) holdBack(
	ctx context.Context, c *crewletaiv1alpha1.Company, existing *appsv1.StatefulSet, reason, message string,
) (metav1.Condition, error) {
	if suspended(c) {
		stopped, err := r.stopEngine(ctx, c, existing)
		if err != nil || !stopped {
			return suspendingCondition(), err
		}
		return suspendedCondition("Once it resumes, its pods are held back: " + message), nil
	}
	return runtimeCondition(metav1.ConditionFalse, reason, message), nil
}

// stop stops a suspended Company's pods when nothing else of its runtime can
// be reconciled (it is not accepted), and records RuntimeReady=Suspended once
// they are, which is then true: Ready reads its claim that the engine is
// stopped from that condition.
func (r *CompanyReconciler) stop(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	existing, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return err
	}
	stopped, err := r.stopEngine(ctx, c, existing)
	if err != nil {
		return err
	}
	condition := suspendedCondition("")
	if !stopped {
		condition = suspendingCondition()
	}
	condition.ObservedGeneration = c.Generation
	meta.SetStatusCondition(&c.Status.Conditions, condition)
	return nil
}

// stopEngine scales a Company's seat StatefulSets to zero and, once no seat
// pod is left, its data StatefulSet: a stopping seat pod hands its last
// records to a data pod, the only kind that keeps them, so the data pods
// outlive the seat pods' drain. It reports whether the data pods were
// stopped.
func (r *CompanyReconciler) stopEngine(ctx context.Context, c *crewletaiv1alpha1.Company, data *appsv1.StatefulSet) (bool, error) {
	left, err := r.stopSeatPods(ctx, c)
	if err != nil || left {
		return false, err
	}
	return true, r.scaleToZero(ctx, c, data)
}

// scaleToZero sets the replicas of a data StatefulSet the Company controls to
// zero, with a patch of that one field: it needs nothing rendered, so it works
// whatever else about the Company is wrong.
func (r *CompanyReconciler) scaleToZero(ctx context.Context, c *crewletaiv1alpha1.Company, sts *appsv1.StatefulSet) error {
	if sts == nil || !metav1.IsControlledBy(sts, c) || (sts.Spec.Replicas != nil && *sts.Spec.Replicas == 0) {
		return nil
	}
	base := sts.DeepCopy()
	sts.Spec.Replicas = new(int32(0))
	if err := r.Patch(ctx, sts, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
		return fmt.Errorf("scaling StatefulSet %s/%s to zero: %w", sts.Namespace, sts.Name, err)
	}
	return nil
}

// suspendingCondition is RuntimeReady for a suspended Company whose seat pods
// are still stopping, before its data pods stop.
func suspendingCondition() metav1.Condition {
	return runtimeCondition(metav1.ConditionFalse, crewletaiv1alpha1.ReasonSuspending,
		"The Company is suspended: its seat pods are stopping, and its data pods stop once they have.")
}

// suspendedCondition is RuntimeReady for a suspended Company whose data
// StatefulSet is scaled to zero, or that has none; more adds to its message.
func suspendedCondition(more string) metav1.Condition {
	message := "The Company is suspended: its data pods are stopped and its data volumes kept."
	if more != "" {
		message += " " + more
	}
	return runtimeCondition(metav1.ConditionFalse, crewletaiv1alpha1.ReasonSuspended, message)
}

// capitalize makes an error message, which starts in lower case, the start of
// a sentence.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// apply writes an object of the Company's with server-side apply, as the
// operator's field manager, taking over any field another manager set on it.
// The response, status included, is decoded into obj.
//
// It first makes sure the object is the Company's to write: one of that name
// that the Company does not control is someone else's (a user's Service, a
// namespace's default-deny NetworkPolicy), and taking it over would rewrite
// it and, through the owner reference the apply adds, have the garbage
// collector delete it with the Company. The operator would then be a
// confused deputy for anyone allowed to write Companies but not those
// objects. It is left alone and reported (*nameConflictError), as the
// Deployment and ReplicaSet controllers refuse to adopt an object another
// controller owns. The one object of another owner taken over is one an
// earlier Company of the same name controlled: it was deleted, the garbage
// collector is about to delete the object, and the apply replaces the owner
// reference with this Company's.
func (r *CompanyReconciler) apply(
	ctx context.Context, c *crewletaiv1alpha1.Company, gvk schema.GroupVersionKind, name string, obj runtime.ApplyConfiguration,
) error {
	if err := r.mayWrite(ctx, c, gvk, name); err != nil {
		return err
	}
	if err := r.Apply(ctx, obj, client.FieldOwner(FieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("applying %s %s/%s: %w", gvk.Kind, c.Namespace, name, err)
	}
	return nil
}

// mayWrite returns a *nameConflictError unless the object of that kind and
// name does not exist, or is the Company's (see apply). It reads metadata
// only, from the informer the kind is watched with; the StatefulSet, the one
// kind cached whole, is read as itself, so that no second informer is started
// for it. An object an earlier Company of the name controlled is released
// from it first: the apply adds this Company as the controller, and an object
// may have only one.
func (r *CompanyReconciler) mayWrite(ctx context.Context, c *crewletaiv1alpha1.Company, gvk schema.GroupVersionKind, name string) error {
	var obj client.Object
	if gvk == statefulSetGVK {
		sts, err := r.statefulSetNamed(ctx, c.Namespace, name)
		if err != nil || sts == nil {
			return err
		}
		obj = sts
	} else {
		m := &metav1.PartialObjectMetadata{}
		m.SetGroupVersionKind(gvk)
		if err := r.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, m); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("reading %s %s/%s: %w", gvk.Kind, c.Namespace, name, err)
		}
		obj = m
	}
	if metav1.IsControlledBy(obj, c) {
		return nil
	}
	owner := metav1.GetControllerOf(obj)
	if owner == nil || !isPreviousCompany(owner, c) {
		controller := "no controller"
		if owner != nil {
			controller = "the controller " + owner.Kind + " " + owner.Name
		}
		return &nameConflictError{kind: gvk.Kind, namespace: c.Namespace, name: name, company: c.Name, controller: controller}
	}
	release := &metav1.PartialObjectMetadata{}
	release.SetGroupVersionKind(gvk)
	release.Namespace, release.Name = obj.GetNamespace(), obj.GetName()
	release.ResourceVersion = obj.GetResourceVersion()
	release.OwnerReferences = obj.GetOwnerReferences()
	base := release.DeepCopy()
	release.OwnerReferences = slices.DeleteFunc(slices.Clone(release.OwnerReferences),
		func(o metav1.OwnerReference) bool { return o.UID == owner.UID })
	// A merge patch replaces the whole list; the optimistic lock makes it
	// fail rather than drop a reference added since the read.
	if err := r.Patch(ctx, release, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("releasing %s %s/%s from an earlier Company %s: %w", gvk.Kind, c.Namespace, name, c.Name, err)
	}
	return nil
}

// nameConflictError is an object the operator would write for a Company that
// exists and is not the Company's.
type nameConflictError struct {
	kind, namespace, name, company, controller string
}

func (e *nameConflictError) Error() string { return e.message() }

// message is the condition message: what exists, and what to do.
func (e *nameConflictError) message() string {
	return fmt.Sprintf("%s %s/%s exists with %s, and the operator writes an object of that name for Company %s: "+
		"it changes or adopts no object it did not create. Rename or delete that %s, or give the Company another name.",
		e.kind, e.namespace, e.name, e.controller, e.company, e.kind)
}

// finalize stops a deleted Company's engine and settles its data volumes,
// then lets the deletion finish.
//
//  0. Every seat StatefulSet is deleted, and the operator waits until every
//     seat pod is gone: a seat pod drains on SIGTERM, handing its last
//     records to a data pod, so the data pods must still run.
//  1. The data StatefulSet is deleted and the operator waits until it and
//     every data pod are gone. Each pod drains on SIGTERM: running turns
//     finish, waiting ones are handed back. The Company's other objects stay
//     until then, so a draining engine keeps its Services, Secrets and routes.
//  2. With deletionPolicy Delete the data volume claims are deleted. With
//     Retain they are left, and so is the keyring Secret, which is released
//     from the Company: the volumes cannot be read without it.
//  3. The finalizer is removed; the garbage collector deletes everything else
//     the Company owns.
//
// Under Retain the StatefulSet is deleted with the Orphan propagation
// policy, and its pods are deleted by the operator once it is gone. A claim
// the StatefulSet controller made while the policy was Delete carries an
// owner reference to the StatefulSet, which that controller removes only on
// its next sync after the policy changes; a background deletion would let the
// garbage collector delete the claim, and the data with it, whatever Retain
// says. Orphaning has the garbage collector remove the references from every
// dependent before the StatefulSet goes, so no claim can be collected with
// it.
func (r *CompanyReconciler) finalize(ctx context.Context, c *crewletaiv1alpha1.Company) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(c, crewletaiv1alpha1.CompanyFinalizer) {
		return ctrl.Result{}, nil
	}
	log := logf.FromContext(ctx)
	retain := c.Spec.DeletionPolicy == crewletaiv1alpha1.DeletionPolicyRetain

	// The seat pods go first: each drains into a data pod, which keeps
	// what it hands over, so the data pods outlive them.
	left, err := r.deleteSeatPools(ctx, c)
	if err != nil {
		return ctrl.Result{}, err
	}
	if left {
		log.V(1).Info("Waiting for the seat pods to stop")
		return ctrl.Result{RequeueAfter: drainPollInterval}, nil
	}

	sts, err := r.existingStatefulSet(ctx, c)
	if err != nil {
		return ctrl.Result{}, err
	}
	if sts != nil && metav1.IsControlledBy(sts, c) {
		propagation := metav1.DeletePropagationBackground
		if retain {
			propagation = metav1.DeletePropagationOrphan
		}
		if err := r.deleteIfControlled(ctx, c, sts, client.PropagationPolicy(propagation)); err != nil {
			return ctrl.Result{}, err
		}
		log.V(1).Info("Waiting for the data StatefulSet to be deleted")
		return ctrl.Result{RequeueAfter: drainPollInterval}, nil
	}
	pods := &corev1.PodList{}
	if err := r.APIReader.List(ctx, pods, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing the data pods of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	if len(pods.Items) > 0 {
		if retain {
			// Orphaned with the StatefulSet: nothing else deletes them.
			for i := range pods.Items {
				pod := &pods.Items[i]
				if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, fmt.Errorf("deleting data pod %s/%s: %w", pod.Namespace, pod.Name, err)
				}
			}
		}
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
	claims, err := r.dataClaims(ctx, c)
	if err != nil {
		return err
	}
	for i := range claims {
		claim := &claims[i]
		if err := r.Delete(ctx, claim); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PersistentVolumeClaim %s/%s: %w", claim.Namespace, claim.Name, err)
		}
	}
	return nil
}

// toolBridge reports whether the company's engine serves its tool bridge: it
// does while an LLMProvider of the company runs a CLI in mode Agent, whose
// runs happen in a remote sandbox and call the seat's tools back through it.
// Without one the engine is given no CREWLET_MCP_BRIDGE_URL and mounts no
// bridge, so its public listener answers /mcp/ as a path nothing serves:
// no company serves a route it does not use.
func (r *CompanyReconciler) toolBridge(ctx context.Context, c *crewletaiv1alpha1.Company) (bool, error) {
	var providers crewletaiv1alpha1.LLMProviderList
	if err := r.List(ctx, &providers, client.InNamespace(c.Namespace)); err != nil {
		return false, fmt.Errorf("listing the LLMProviders of namespace %s: %w", c.Namespace, err)
	}
	for i := range providers.Items {
		p := &providers.Items[i]
		if p.Spec.CompanyRef.Name == c.Name && p.Spec.CLI.Mode == crewletaiv1alpha1.CLIAgentModeAgent {
			return true, nil
		}
	}
	return false, nil
}
