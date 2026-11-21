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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	batchv1ac "k8s.io/client-go/applyconfigurations/batch/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
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
	// preflightMessageLimit keeps a refusal readable in a condition.
	preflightMessageLimit = 2048
)

// preflightSecretName is the Secret that holds the company document a
// Company's preflight validates.
func preflightSecretName(c *crewletaiv1alpha1.Company) string {
	return c.Name + "-preflight"
}

// preflightGate is the image the data pods run and why it is not image: a
// move to another image waits, under the class's engine.preflight Required,
// for that image to accept the configuration, and the pods keep the image
// they run meanwhile. A first start has no image to keep, and an image
// already refused (refused) is not run at all.
func (r *CompanyReconciler) preflightGate(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	existing *appsv1.StatefulSet, image string, refused *imageRefusal, bootstrapHash string,
) (string, *imageRefusal, error) {
	if refused != nil || existing == nil || class.Spec.Engine.Preflight != crewletaiv1alpha1.PreflightRequired {
		return image, refused, nil
	}
	from := runningImage(existing)
	if from == "" || from == image {
		return image, nil, nil
	}
	held, err := r.preflight(ctx, c, class, image, bootstrapHash)
	if err != nil || held == nil {
		return image, nil, err
	}
	return from, held, nil
}

// preflight holds a Company's data pods on the image they run until image has
// validated their configuration, under the class's engine.preflight
// Required: it returns the refusal that keeps them there, or nil once the
// image has passed.
//
// The image is run as a Job of the namespace, under the engine's own
// ServiceAccount with no token, on the class's nodes, with crewlet validate
// given the pods' crewlet.yaml (the bootstrap Secret the new pods would read)
// and the company document the engine runs now. validate reads no
// environment and dials nothing, so the Job needs no network and no
// credential: the document is the engine's redacted one. Each Job is named
// after what it validates, so a changed image, crewlet.yaml or document runs
// a new one, and the Jobs of earlier attempts are deleted.
func (r *CompanyReconciler) preflight(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, image, bootstrapHash string,
) (*imageRefusal, error) {
	document := r.activeDocument(ctx, c)
	sum := sha256.Sum256([]byte(image + "\n" + bootstrapHash + "\n" + document))
	name := fmt.Sprintf("%s-preflight-%s", c.Name, hex.EncodeToString(sum[:])[:10])
	if err := r.deletePreflights(ctx, c, name); err != nil {
		return nil, err
	}

	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, job)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.startPreflight(ctx, c, class, name, image, document); err != nil {
			return nil, err
		}
		return preflightRunning(image), nil
	case err != nil:
		return nil, fmt.Errorf("reading Job %s/%s: %w", c.Namespace, name, err)
	}
	if jobConditionTrue(job, batchv1.JobComplete) {
		return nil, nil
	}
	if jobConditionTrue(job, batchv1.JobFailed) {
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
	return preflightRunning(image), nil
}

// jobConditionTrue reports whether a Job's condition of that type is True.
func jobConditionTrue(job *batchv1.Job, t batchv1.JobConditionType) bool {
	for i := range job.Status.Conditions {
		if job.Status.Conditions[i].Type == t && job.Status.Conditions[i].Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// preflightRunning is the refusal while the preflight of image runs.
func preflightRunning(image string) *imageRefusal {
	return &imageRefusal{
		reason: crewletaiv1alpha1.ReasonPreflightRunning,
		message: fmt.Sprintf("The engine image %s is validating the company's configuration before any data pod moves to it "+
			"(the class's engine.preflight is Required).", image),
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

// startPreflight writes the document's Secret and creates the Job.
func (r *CompanyReconciler) startPreflight(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, name, image, document string,
) error {
	args := []string{"validate", configFlag, configDir + "/" + configFile}
	spec, err := enginePodSpec(c, class, 0)
	if err != nil {
		return err
	}
	spec.WithRestartPolicy(corev1.RestartPolicyNever).
		WithVolumes(configVolume(bootstrapSecretName(c)))
	mounts := []*corev1ac.VolumeMountApplyConfiguration{
		corev1ac.VolumeMount().WithName(configVolumeName).WithMountPath(configDir).WithReadOnly(true),
	}
	if document != "" {
		secret := corev1ac.Secret(preflightSecretName(c), c.Namespace).
			WithLabels(labelsFor(c, componentPreflight)).
			WithOwnerReferences(ownerReference(c)).
			WithType(corev1.SecretTypeOpaque).
			WithData(map[string][]byte{preflightDocument: []byte(document)})
		if err := r.apply(ctx, c, secretGVK, preflightSecretName(c), secret); err != nil {
			return err
		}
		spec.WithVolumes(corev1ac.Volume().WithName(componentPreflight).WithSecret(corev1ac.SecretVolumeSource().
			WithSecretName(preflightSecretName(c)).
			WithDefaultMode(configFileMode)))
		mounts = append(mounts, corev1ac.VolumeMount().WithName(componentPreflight).WithMountPath(preflightDir).WithReadOnly(true))
		args = append(args, "-company", preflightDir+"/"+preflightDocument)
	}
	container := engineContainerBase(class, image, nil).
		WithName(preflightContainer).
		WithVolumeMounts(mounts...).
		// What validate prints on a refusal is the condition's message.
		WithTerminationMessagePolicy(corev1.TerminationMessageFallbackToLogsOnError)
	// Replaced, not appended to: the base's arguments start the engine.
	container.Args = args
	spec.WithContainers(container)

	job := batchv1ac.Job(name, c.Namespace).
		WithLabels(labelsFor(c, componentPreflight)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(batchv1ac.JobSpec().
			WithBackoffLimit(0).
			WithActiveDeadlineSeconds(preflightDeadlineSeconds).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(labelsFor(c, componentPreflight)).
				WithSpec(spec)))
	return r.apply(ctx, c, jobGVK, name, job)
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
			if t := statuses[j].State.Terminated; t != nil && strings.TrimSpace(t.Message) != "" {
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
