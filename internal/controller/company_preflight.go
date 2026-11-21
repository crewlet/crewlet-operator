package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	batchv1ac "k8s.io/client-go/applyconfigurations/batch/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The preflight's names and bounds.
const (
	componentPreflight = "preflight"
	// preflightDocument is the key, and the file name, of the company
	// document the preflight validates beside crewlet.yaml.
	preflightDocument  = "company.yaml"
	preflightDir       = "/etc/crewlet-preflight"
	preflightContainer = "preflight"
	// preflightDeadlineSeconds bounds a run: crewlet validate reaches
	// nothing, so only pulling the image takes long.
	preflightDeadlineSeconds = 600
	// preflightBackoffLimit allows a pod lost for a reason the pod failure
	// policy does not name (an eviction without a disruption condition).
	preflightBackoffLimit = 2
	// preflightTTLSeconds lets the cluster collect a finished Job the
	// operator did not delete itself (one the Company outlived).
	preflightTTLSeconds = 86400
	// preflightMessageLimit keeps a refusal readable in a condition.
	preflightMessageLimit = 2048
)

// preflightSecretName is the Secret that holds what a Company's preflight
// validates: the crewlet.yaml the data pods would move to, and the company
// document the engine ran when the preflight started.
func preflightSecretName(c *crewletaiv1alpha1.Company) string {
	return c.Name + "-preflight"
}

// preflightPolicyName is the NetworkPolicy that cuts the preflight pods off.
func preflightPolicyName(c *crewletaiv1alpha1.Company) string {
	return c.Name + "-preflight"
}

// preflightGate is why the data pods may not move to image yet, or nil: under
// the class's engine.preflight Required, a running Company moves to another
// image only once that image has accepted the crewlet.yaml the moved pods
// would read (config, rendered but not yet applied) and the company
// document. The caller holds the pods back, StatefulSet and bootstrap Secret
// alike, while it is not nil: holding the image alone would roll the old
// pods onto a crewlet.yaml written for the new one. A first start has no
// image to keep, and an image already refused (refused) is not run at all.
// With no move to make, what earlier preflights left is deleted.
func (r *CompanyReconciler) preflightGate(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	existing *appsv1.StatefulSet, image string, refused *imageRefusal, config []byte,
) (*imageRefusal, error) {
	from := ""
	if existing != nil {
		from = runningImage(existing)
	}
	if refused != nil || from == "" || from == image || class.Spec.Engine.Preflight != crewletaiv1alpha1.PreflightRequired {
		return nil, r.clearPreflights(ctx, c)
	}
	return r.preflight(ctx, c, class, image, config)
}

// preflight returns why the data pods wait for image, or nil once image has
// passed.
//
// The image is run as a Job of the namespace, under the engine's own
// ServiceAccount with no token, on the class's nodes, with crewlet validate
// given the crewlet.yaml the moved pods would read and the company document
// the engine runs. validate reads no environment and dials nothing, so the
// Job needs no network and no credential, and a NetworkPolicy gives it
// none: the document is the engine's redacted one, and crewlet.yaml is what
// the bootstrap Secret beside it holds. The Job is named after the image and
// the crewlet.yaml, never the live document, so an engine that answers only
// now and then cannot have every reconcile replace the Job; the document is
// read once, when the Job starts. Jobs of earlier attempts are deleted.
//
// Only crewlet validate's refusal (a non-zero exit, which the Job's pod
// failure policy fails it on) holds the pods for good; a Job that ran out of
// time or lost its pod without an exit is started again.
func (r *CompanyReconciler) preflight(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, image string, config []byte,
) (*imageRefusal, error) {
	sum := sha256.Sum256(append([]byte(image+"\n"), config...))
	name := fmt.Sprintf("%s-preflight-%s", c.Name, hex.EncodeToString(sum[:])[:10])
	if err := r.deletePreflights(ctx, c, name); err != nil {
		return nil, err
	}

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, job)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.startPreflight(ctx, c, class, name, image, config); err != nil {
			return nil, err
		}
		return preflightRunning(image, ""), nil
	case err != nil:
		return nil, fmt.Errorf("reading Job %s/%s: %w", c.Namespace, name, err)
	}
	if !job.DeletionTimestamp.IsZero() {
		// A retry waits for the Job it replaces to go.
		return preflightRunning(image, ""), nil
	}
	if jobConditionTrue(job, batchv1.JobComplete) {
		return nil, nil
	}
	failed := jobCondition(job, batchv1.JobFailed)
	if failed == nil {
		return preflightRunning(image, ""), nil
	}
	if failed.Reason != batchv1.JobReasonPodFailurePolicy {
		if err := r.deleteIfControlled(ctx, c, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			return nil, err
		}
		return preflightRunning(image, fmt.Sprintf(" Its last attempt ended without a verdict (%s), and it runs again.", failed.Reason)), nil
	}
	said, err := r.preflightSaid(ctx, c, name)
	if err != nil {
		return nil, err
	}
	return &imageRefusal{
		reason: crewletaiv1alpha1.ReasonPreflightFailed,
		message: fmt.Sprintf("The engine image %s refused the company's configuration in its preflight (Job %s): %s "+
			"Correct the configuration, or move to an image that accepts it.", image, name, said),
	}, nil
}

// jobCondition is a Job's True condition of that type, or nil.
func jobCondition(job *batchv1.Job, t batchv1.JobConditionType) *batchv1.JobCondition {
	for i := range job.Status.Conditions {
		if job.Status.Conditions[i].Type == t && job.Status.Conditions[i].Status == corev1.ConditionTrue {
			return &job.Status.Conditions[i]
		}
	}
	return nil
}

// jobConditionTrue reports whether a Job's condition of that type is True.
func jobConditionTrue(job *batchv1.Job, t batchv1.JobConditionType) bool {
	return jobCondition(job, t) != nil
}

// preflightRunning is the refusal while the preflight of image runs; more
// says why it runs again, when it does.
func preflightRunning(image, more string) *imageRefusal {
	return &imageRefusal{
		reason: crewletaiv1alpha1.ReasonPreflightRunning,
		message: fmt.Sprintf("The engine image %s is validating the company's configuration before any data pod moves to it "+
			"(the class's engine.preflight is Required); the pods are held as they are meanwhile.%s", image, more),
	}
}

// activeDocument is the company document the engine runs, redacted, or ""
// when there is none to read: no engine to ask (Engines unset), an engine
// that cannot be reached, or one that runs no document yet. The preflight
// then validates crewlet.yaml alone, since holding an upgrade on an engine
// that cannot answer would keep a broken engine from the image that fixes it.
func (r *CompanyReconciler) activeDocument(ctx context.Context, c *crewletaiv1alpha1.Company) string {
	if r.Engines == nil {
		return ""
	}
	log := logf.FromContext(ctx)
	e, err := r.Engines.Engine(ctx, c)
	if err != nil {
		log.Info("Preflight validates crewlet.yaml alone: the engine cannot be reached", "reason", err.Error())
		return ""
	}
	active, err := e.ActiveConfig(ctx)
	if err != nil {
		log.Info("Preflight validates crewlet.yaml alone: the engine's document cannot be read", "reason", err.Error())
		return ""
	}
	return string(active.Document)
}

// startPreflight writes what the Job validates and the policy that cuts its
// pods off, and creates the Job.
//
// The Secret holds the crewlet.yaml the bootstrap Secret beside it will hold
// once the move is made, so nothing reaches the namespace that it would not
// hold anyway, and the engine's redacted document, read now and kept with
// the Job it was read for.
func (r *CompanyReconciler) startPreflight(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, name, image string, config []byte,
) error {
	if err := r.applyPreflightPolicy(ctx, c); err != nil {
		return err
	}
	data := map[string][]byte{configFile: config}
	args := []string{"validate", configFlag, preflightDir + "/" + configFile}
	if document := r.activeDocument(ctx, c); document != "" {
		data[preflightDocument] = []byte(document)
		args = append(args, "-company", preflightDir+"/"+preflightDocument)
	}
	secret := corev1ac.Secret(preflightSecretName(c), c.Namespace).
		WithLabels(labelsFor(c, componentPreflight)).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	if err := r.apply(ctx, c, secretGVK, preflightSecretName(c), secret); err != nil {
		return err
	}

	spec, err := enginePodSpec(c, class, 0)
	if err != nil {
		return err
	}
	spec.WithRestartPolicy(corev1.RestartPolicyNever).
		WithVolumes(corev1ac.Volume().WithName(componentPreflight).WithSecret(corev1ac.SecretVolumeSource().
			WithSecretName(preflightSecretName(c)).
			WithDefaultMode(configFileMode)))
	container := engineContainerBase(class, image, nil).
		WithName(preflightContainer).
		WithVolumeMounts(corev1ac.VolumeMount().WithName(componentPreflight).WithMountPath(preflightDir).WithReadOnly(true)).
		// What validate prints on a refusal is the condition's message.
		WithTerminationMessagePolicy(corev1.TerminationMessageFallbackToLogsOnError)
	// Replaced, not appended to: the base's arguments start the engine.
	container.Args = args
	spec.WithContainers(container)

	job := batchv1ac.Job(name, c.Namespace).
		WithLabels(labelsFor(c, componentPreflight)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(batchv1ac.JobSpec().
			WithBackoffLimit(preflightBackoffLimit).
			WithActiveDeadlineSeconds(preflightDeadlineSeconds).
			WithTTLSecondsAfterFinished(preflightTTLSeconds).
			// validate's exit is the verdict: a non-zero one fails the Job
			// at once, and a pod the cluster took away is not counted.
			WithPodFailurePolicy(batchv1ac.PodFailurePolicy().WithRules(
				batchv1ac.PodFailurePolicyRule().
					WithAction(batchv1.PodFailurePolicyActionIgnore).
					WithOnPodConditions(batchv1ac.PodFailurePolicyOnPodConditionsPattern().
						WithType(corev1.DisruptionTarget).WithStatus(corev1.ConditionTrue)),
				batchv1ac.PodFailurePolicyRule().
					WithAction(batchv1.PodFailurePolicyActionFailJob).
					WithOnExitCodes(batchv1ac.PodFailurePolicyOnExitCodesRequirement().
						WithContainerName(preflightContainer).
						WithOperator(batchv1.PodFailurePolicyOnExitCodesOpNotIn).
						WithValues(0)))).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(labelsFor(c, componentPreflight)).
				WithSpec(spec)))
	return r.apply(ctx, c, jobGVK, name, job)
}

// applyPreflightPolicy writes <c>-preflight, which admits no connection to or
// from the Company's preflight pods: validate dials nothing, and the new
// image has not yet earned the engine's network.
func (r *CompanyReconciler) applyPreflightPolicy(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	policy := networkingv1ac.NetworkPolicy(preflightPolicyName(c), c.Namespace).
		WithLabels(labelsFor(c, componentPreflight)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(networkingv1ac.NetworkPolicySpec().
			WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentPreflight))).
			WithPolicyTypes(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
	return r.apply(ctx, c, networkPolicyGVK, preflightPolicyName(c), policy)
}

// clearPreflights deletes what the Company's preflights left: their Jobs, the
// Secret they read and their policy. It runs whenever no move waits.
func (r *CompanyReconciler) clearPreflights(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	if err := r.deletePreflights(ctx, c, ""); err != nil {
		return err
	}
	if err := r.deleteControlled(ctx, c, secretGVK, preflightSecretName(c)); err != nil {
		return err
	}
	return r.deleteControlled(ctx, c, networkPolicyGVK, preflightPolicyName(c))
}

// preflightSaid is what the failed preflight's container said: its
// termination message, which is the end of its log when it wrote none.
func (r *CompanyReconciler) preflightSaid(ctx context.Context, c *crewletaiv1alpha1.Company, job string) (string, error) {
	pods := &corev1.PodList{}
	if err := r.APIReader.List(ctx, pods, client.InNamespace(c.Namespace),
		client.MatchingLabels{batchv1.JobNameLabel: job}); err != nil {
		return "", fmt.Errorf("listing the pods of Job %s/%s: %w", c.Namespace, job, err)
	}
	for i := range pods.Items {
		statuses := pods.Items[i].Status.ContainerStatuses
		for j := range statuses {
			// The refusing pod: pods the cluster took away end without
			// an exit of validate's own.
			if t := statuses[j].State.Terminated; t != nil && t.ExitCode != 0 && strings.TrimSpace(t.Message) != "" {
				said := strings.TrimSpace(t.Message)
				if len(said) > preflightMessageLimit {
					said = "..." + said[len(said)-preflightMessageLimit:]
				}
				return said, nil
			}
		}
	}
	return "it ended without saying why (its pod is gone, or it ran out of time).", nil
}

// deletePreflights deletes the Company's preflight Jobs but the one named
// keep, with their pods: each validated something no longer asked for.
func (r *CompanyReconciler) deletePreflights(ctx context.Context, c *crewletaiv1alpha1.Company, keep string) error {
	jobs := &metav1.PartialObjectMetadataList{}
	jobs.SetGroupVersionKind(batchv1.SchemeGroupVersion.WithKind("JobList"))
	if err := r.List(ctx, jobs, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentPreflight))); err != nil {
		return fmt.Errorf("listing the preflight Jobs of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	for i := range jobs.Items {
		if jobs.Items[i].Name == keep {
			continue
		}
		if err := r.deleteIfControlled(ctx, c, &jobs.Items[i], client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			return err
		}
	}
	return nil
}
