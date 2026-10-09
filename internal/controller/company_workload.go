package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// What the engine container looks like from the inside, beyond what render
// shares (render.DataDir, render.APIPort).
const (
	// engineContainer is the name of the engine's container in a data pod.
	engineContainer = "engine"
	// configDir is where the bootstrap ConfigMap is mounted.
	configDir = "/etc/crewlet"
	// configFile is the key of the bootstrap ConfigMap that holds Tier A,
	// and the file name it is mounted under.
	configFile = "crewlet.yaml"
	// homeDir is the engine user's home in Crewlet's image, where the CLI
	// agents and the git the sandbox recipes drive keep their state.
	homeDir = "/home/crewlet"
	// engineUID is the user Crewlet's image runs as.
	engineUID int64 = 10001
	// dataVolume is the name of the data volume claim template, and so the
	// first part of each data volume claim's name (data-<sts>-<ordinal>).
	dataVolume = "data"
	// portName names the API port on the pods and the Services.
	portName = "http"
)

// The pod template annotations that roll the data pods when what they read
// at start changes.
//
//nolint:gosec // G101 reads "secrets" in a name as a credential; the annotation holds resourceVersions, never a value.
const (
	// BootstrapHashAnnotation is the hash of the data pods' crewlet.yaml.
	BootstrapHashAnnotation = "crewlet.ai/bootstrap-hash"
	// SecretsVersionAnnotation names the resourceVersions of the Secrets the
	// engine's environment reads.
	SecretsVersionAnnotation = "crewlet.ai/secrets-version"
)

// The environment variables the operator sets on the engine beyond those
// render.Bootstrap lists.
const (
	envHome            = "HOME"
	envTursoCache      = "TURSO_GO_CACHE_DIR"
	envOTLPEndpoint    = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envOTLPProtocol    = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envOTLPHeaders     = "OTEL_EXPORTER_OTLP_HEADERS"
	envOTELResourceAtt = "OTEL_RESOURCE_ATTRIBUTES"
)

// preStopSleepSeconds is how long a stopping data pod keeps serving before
// the engine is told to drain. Endpoint changes reach kube-proxy and the
// Gateways a few seconds after the pod is marked terminating; without the
// pause a webhook could still be routed to a pod that already turned it
// away.
const preStopSleepSeconds = 5

// workload is everything a data pod's template is built from.
type workload struct {
	image     string
	bootstrap *render.BootstrapDocument
	// secretsVersion is the SecretsVersionAnnotation's value.
	secretsVersion string
}

// applyBootstrap writes the ConfigMap that holds the data pods' crewlet.yaml.
// It holds ${VAR} references only, so it is a ConfigMap and not a Secret.
func (r *CompanyReconciler) applyBootstrap(ctx context.Context, c *crewletaiv1alpha1.Company, doc *render.BootstrapDocument) error {
	cm := corev1ac.ConfigMap(bootstrapConfigName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithData(map[string]string{configFile: string(doc.YAML)})
	return r.apply(ctx, c, configMapGVK, bootstrapConfigName(c), cm)
}

// applyServiceAccount writes the engine's ServiceAccount. The engine does not
// call the Kubernetes API, so the account exists only to carry a workload
// identity (the class's annotations) and its token is never mounted.
func (r *CompanyReconciler) applyServiceAccount(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) error {
	sa := corev1ac.ServiceAccount(serviceAccountName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithAutomountServiceAccountToken(false)
	if a := class.Spec.ServiceAccount.Annotations; len(a) > 0 {
		sa.WithAnnotations(a)
	}
	return r.apply(ctx, c, serviceAccountGVK, serviceAccountName(c), sa)
}

// applyServices writes the company's two Services. <c>-engine routes to
// ready data pods and backs the routes and status.endpoints.api. <c>-data is
// the StatefulSet's headless governing Service: it gives each data pod a
// stable DNS name, published before the pod is ready, because an engine with
// no document yet answers /ready with 503 and the operator must reach it to
// apply the first one.
func (r *CompanyReconciler) applyServices(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	port := corev1ac.ServicePort().
		WithName(portName).
		WithProtocol(corev1.ProtocolTCP).
		WithPort(render.APIPort).
		WithTargetPort(intstr.FromString(portName))
	engine := corev1ac.Service(engineServiceName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(corev1ac.ServiceSpec().
			WithType(corev1.ServiceTypeClusterIP).
			WithSelector(selectorFor(c, componentData)).
			WithPorts(port))
	if err := r.apply(ctx, c, serviceGVK, engineServiceName(c), engine); err != nil {
		return err
	}
	data := corev1ac.Service(dataServiceName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(corev1ac.ServiceSpec().
			WithClusterIP(corev1.ClusterIPNone).
			WithPublishNotReadyAddresses(true).
			WithSelector(selectorFor(c, componentData)).
			WithPorts(port))
	return r.apply(ctx, c, serviceGVK, dataServiceName(c), data)
}

// applyStatefulSet writes the data StatefulSet and returns it as the API
// server answered, status included.
func (r *CompanyReconciler) applyStatefulSet(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	existing *appsv1.StatefulSet, w *workload,
) (*appsv1ac.StatefulSetApplyConfiguration, error) {
	pod, err := podTemplate(c, class, w)
	if err != nil {
		return nil, err
	}
	whenDeleted := appsv1.RetainPersistentVolumeClaimRetentionPolicyType
	if c.Spec.DeletionPolicy == crewletaiv1alpha1.DeletionPolicyDelete {
		whenDeleted = appsv1.DeletePersistentVolumeClaimRetentionPolicyType
	}
	spec := appsv1ac.StatefulSetSpec().
		WithReplicas(dataReplicas(c)).
		WithServiceName(dataServiceName(c)).
		WithSelector(metav1ac.LabelSelector().WithMatchLabels(selectorFor(c, componentData))).
		// Parallel: the data pods hold no ordinal-ordered state between
		// them, so none waits on another to start or stop, and a pod that
		// is not ready never holds back another's replacement.
		WithPodManagementPolicy(appsv1.ParallelPodManagement).
		WithUpdateStrategy(appsv1ac.StatefulSetUpdateStrategy().
			WithType(appsv1.RollingUpdateStatefulSetStrategyType).
			WithRollingUpdate(appsv1ac.RollingUpdateStatefulSetStrategy().
				WithMaxUnavailable(intstr.FromInt32(1)))).
		WithPersistentVolumeClaimRetentionPolicy(appsv1ac.StatefulSetPersistentVolumeClaimRetentionPolicy().
			WithWhenDeleted(whenDeleted).
			// Suspending scales to zero and must never cost the data.
			WithWhenScaled(appsv1.RetainPersistentVolumeClaimRetentionPolicyType)).
		WithTemplate(pod)

	// A StatefulSet's volume claim templates cannot change once it exists,
	// and a class's storage applies to companies created after it changes
	// (see DataStorage.size), so an existing StatefulSet keeps its own.
	if existing != nil {
		for i := range existing.Spec.VolumeClaimTemplates {
			claim, err := applyConfigOf[corev1ac.PersistentVolumeClaimApplyConfiguration](&existing.Spec.VolumeClaimTemplates[i])
			if err != nil {
				return nil, err
			}
			claim.Status = nil
			spec.WithVolumeClaimTemplates(claim)
		}
	} else {
		spec.WithVolumeClaimTemplates(dataClaim(c, class))
	}

	sts := appsv1ac.StatefulSet(dataStatefulSetName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(spec)
	if err := r.apply(ctx, c, statefulSetGVK, dataStatefulSetName(c), sts); err != nil {
		return nil, err
	}
	return sts, nil
}

// dataReplicas is the data pods' count: none while the Company is suspended.
func dataReplicas(c *crewletaiv1alpha1.Company) int32 {
	if suspended(c) {
		return 0
	}
	return c.Spec.Topology.Data.Replicas
}

func suspended(c *crewletaiv1alpha1.Company) bool {
	return c.Spec.Suspend != nil && *c.Spec.Suspend
}

// dataClaim is the volume claim template of a new data StatefulSet. Its
// labels select the claims of a Company when it is deleted with
// deletionPolicy Delete.
func dataClaim(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) *corev1ac.PersistentVolumeClaimApplyConfiguration {
	storage := class.Spec.Data.Storage
	spec := corev1ac.PersistentVolumeClaimSpec().
		WithAccessModes(corev1.ReadWriteOnce).
		WithResources(corev1ac.VolumeResourceRequirements().
			WithRequests(corev1.ResourceList{corev1.ResourceStorage: *storage.Size}))
	if storage.StorageClassName != "" {
		spec.WithStorageClassName(storage.StorageClassName)
	}
	return corev1ac.PersistentVolumeClaim(dataVolume, "").
		WithLabels(labelsFor(c, componentData)).
		WithSpec(spec)
}

// podTemplate is a data pod: the engine container, its volumes, probes and
// security context, and what the class adds.
func podTemplate(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, w *workload,
) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
	overlay := class.Spec.PodTemplate

	labels := labelsFor(c, componentData)
	maps.Copy(labels, overlay.Metadata.Labels)
	annotations := maps.Clone(overlay.Metadata.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[BootstrapHashAnnotation] = w.bootstrap.Hash
	annotations[SecretsVersionAnnotation] = w.secretsVersion

	container, err := engineContainerOf(c, class, w)
	if err != nil {
		return nil, err
	}
	spec := corev1ac.PodSpec().
		WithServiceAccountName(serviceAccountName(c)).
		WithAutomountServiceAccountToken(false).
		// Service links would put an environment variable for every
		// Service of the namespace into the engine's environment, which
		// is read as configuration (${VAR}) and should hold only what the
		// operator and the class put there.
		WithEnableServiceLinks(false).
		WithTerminationGracePeriodSeconds(class.Spec.Data.TerminationGracePeriodSeconds).
		WithSecurityContext(corev1ac.PodSecurityContext().
			WithRunAsNonRoot(true).
			WithRunAsUser(engineUID).
			WithRunAsGroup(engineUID).
			// The data volume is written by the engine user; fsGroup makes
			// a fresh volume writable, and OnRootMismatch skips the
			// recursive walk of a large estate on every start.
			WithFSGroup(engineUID).
			WithFSGroupChangePolicy(corev1.FSGroupChangeOnRootMismatch).
			WithSeccompProfile(corev1ac.SeccompProfile().WithType(corev1.SeccompProfileTypeRuntimeDefault))).
		WithContainers(container).
		WithVolumes(
			corev1ac.Volume().WithName("config").WithConfigMap(corev1ac.ConfigMapVolumeSource().
				WithName(bootstrapConfigName(c))),
			// The root filesystem is read-only; the engine and what it
			// spawns (git, stdio MCP servers, CLI agents) write their
			// scratch files under /tmp and their state under HOME.
			corev1ac.Volume().WithName("tmp").WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
			corev1ac.Volume().WithName("home").WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
		)

	for _, s := range class.Spec.Engine.ImagePullSecrets {
		spec.WithImagePullSecrets(corev1ac.LocalObjectReference().WithName(s.Name))
	}
	ps := overlay.Spec
	if len(ps.NodeSelector) > 0 {
		spec.WithNodeSelector(ps.NodeSelector)
	}
	if ps.RuntimeClassName != "" {
		spec.WithRuntimeClassName(ps.RuntimeClassName)
	}
	if ps.PriorityClassName != "" {
		spec.WithPriorityClassName(ps.PriorityClassName)
	}
	if ps.Affinity != nil {
		affinity, err := applyConfigOf[corev1ac.AffinityApplyConfiguration](ps.Affinity)
		if err != nil {
			return nil, err
		}
		spec.WithAffinity(affinity)
	}
	for i := range ps.Tolerations {
		toleration, err := applyConfigOf[corev1ac.TolerationApplyConfiguration](&ps.Tolerations[i])
		if err != nil {
			return nil, err
		}
		spec.WithTolerations(toleration)
	}
	for i := range ps.TopologySpreadConstraints {
		constraint, err := applyConfigOf[corev1ac.TopologySpreadConstraintApplyConfiguration](&ps.TopologySpreadConstraints[i])
		if err != nil {
			return nil, err
		}
		spec.WithTopologySpreadConstraints(constraint)
	}

	return corev1ac.PodTemplateSpec().
		WithLabels(labels).
		WithAnnotations(annotations).
		WithSpec(spec), nil
}

// engineContainerOf is the engine container of a data pod.
func engineContainerOf(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, w *workload,
) (*corev1ac.ContainerApplyConfiguration, error) {
	env, err := engineEnv(c, class, w)
	if err != nil {
		return nil, err
	}
	health := func(path string) *corev1ac.HTTPGetActionApplyConfiguration {
		return corev1ac.HTTPGetAction().WithPath(path).WithPort(intstr.FromString(portName))
	}
	container := corev1ac.Container().
		WithName(engineContainer).
		WithImage(w.image).
		// The image's entrypoint is the crewlet binary under an init
		// process; these are its arguments.
		WithArgs("run", "-config", configDir+"/"+configFile).
		WithPorts(corev1ac.ContainerPort().
			WithName(portName).
			WithContainerPort(render.APIPort).
			WithProtocol(corev1.ProtocolTCP)).
		WithEnv(env...).
		WithVolumeMounts(
			corev1ac.VolumeMount().WithName(dataVolume).WithMountPath(render.DataDir),
			corev1ac.VolumeMount().WithName("config").WithMountPath(configDir).WithReadOnly(true),
			corev1ac.VolumeMount().WithName("tmp").WithMountPath("/tmp"),
			corev1ac.VolumeMount().WithName("home").WithMountPath(homeDir),
		).
		// The first start extracts the store's native library and migrates
		// the store; the startup probe gives it five minutes before the
		// liveness probe may restart it.
		WithStartupProbe(corev1ac.Probe().
			WithHTTPGet(health("/health")).
			WithPeriodSeconds(5).
			WithFailureThreshold(60)).
		// /health answers 200 for as long as the process lives, through a
		// drain too, so liveness never kills a pod that is finishing its
		// turns.
		WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(health("/health")).
			WithPeriodSeconds(10).
			WithTimeoutSeconds(5)).
		// /ready answers 503 while the engine has no document, sheds load,
		// or drains, which takes the pod out of the engine Service.
		WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(health("/ready")).
			WithPeriodSeconds(5).
			WithTimeoutSeconds(5)).
		WithLifecycle(corev1ac.Lifecycle().
			WithPreStop(corev1ac.LifecycleHandler().
				WithSleep(corev1ac.SleepAction().WithSeconds(preStopSleepSeconds)))).
		WithSecurityContext(corev1ac.SecurityContext().
			WithRunAsNonRoot(true).
			WithAllowPrivilegeEscalation(false).
			WithReadOnlyRootFilesystem(true).
			WithCapabilities(corev1ac.Capabilities().WithDrop(corev1.Capability("ALL"))))
	if p := class.Spec.Engine.ImagePullPolicy; p != "" {
		container.WithImagePullPolicy(p)
	}
	if r := class.Spec.Data.Resources; r != nil {
		resources, err := applyConfigOf[corev1ac.ResourceRequirementsApplyConfiguration](r)
		if err != nil {
			return nil, err
		}
		container.WithResources(resources)
	}
	return container, nil
}

// engineEnv is the engine container's environment: what the bootstrap
// document references, then the operator's own settings, then the class's
// and the Company's variables. Kubernetes expands $(VAR) only from variables
// listed earlier, which the telemetry attributes rely on.
func engineEnv(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, w *workload,
) ([]*corev1ac.EnvVarApplyConfiguration, error) {
	var env []*corev1ac.EnvVarApplyConfiguration
	for i := range w.bootstrap.Env {
		v, err := applyConfigOf[corev1ac.EnvVarApplyConfiguration](&w.bootstrap.Env[i])
		if err != nil {
			return nil, err
		}
		env = append(env, v)
	}
	literal := func(name, value string) *corev1ac.EnvVarApplyConfiguration {
		return corev1ac.EnvVar().WithName(name).WithValue(value)
	}
	fromSecret := func(name, secret, key string) *corev1ac.EnvVarApplyConfiguration {
		return corev1ac.EnvVar().WithName(name).WithValueFrom(corev1ac.EnvVarSource().
			WithSecretKeyRef(corev1ac.SecretKeySelector().WithName(secret).WithKey(key)))
	}

	env = append(env,
		// Set rather than left to the image's user database, which an
		// image built on another base may not have for the engine's uid.
		literal(envHome, homeDir),
		// On the data volume, so a restart does not extract the store's
		// 20 MB native library again.
		literal(envTursoCache, render.DataDir+"/cache"),
	)

	if t := class.Spec.Telemetry; t.OTLPEndpoint != "" {
		env = append(env,
			literal(envOTLPEndpoint, t.OTLPEndpoint),
			literal(envOTLPProtocol, "http/protobuf"),
			literal(envOTELResourceAtt, fmt.Sprintf("k8s.namespace.name=%s,k8s.statefulset.name=%s,k8s.pod.name=$(%s)",
				c.Namespace, dataStatefulSetName(c), render.EnvNodeID)))
		if t.HeadersSecretRef.Name != "" {
			env = append(env, fromSecret(envOTLPHeaders, platformSecretName(c), platformKeyOTLPHeaders))
		}
	}

	for _, v := range class.Spec.PodTemplate.Spec.Env {
		switch {
		case overridden(c, v.Name):
		case v.Value != nil:
			env = append(env, literal(v.Name, *v.Value))
		default:
			env = append(env, fromSecret(v.Name, platformSecretName(c), platformKeyEnvPrefix+v.Name))
		}
	}
	for _, v := range c.Spec.Runtime.Env {
		if v.Value != nil {
			env = append(env, literal(v.Name, *v.Value))
			continue
		}
		ref := v.ValueFrom.SecretKeyRef
		env = append(env, fromSecret(v.Name, ref.Name, ref.Key))
	}
	return env, nil
}

// applyConfigOf converts a value of a Kubernetes type into its apply
// configuration. The two share their JSON form by construction (the apply
// configurations are generated from the types), so a round trip through JSON
// carries exactly the fields that are set.
func applyConfigOf[T any](v any) (*T, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding a %T: %w", v, err)
	}
	out := new(T)
	if err := json.Unmarshal(raw, out); err != nil {
		return nil, fmt.Errorf("decoding a %T as %T: %w", v, out, err)
	}
	return out, nil
}

// existingStatefulSet returns the data StatefulSet, or nil when there is none.
// The cache holds only the StatefulSets the operator manages (CacheOptions),
// so when it has none the API server is asked: a StatefulSet of that name
// someone else created must be found, to be left alone.
func (r *CompanyReconciler) existingStatefulSet(ctx context.Context, c *crewletaiv1alpha1.Company) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	key := types.NamespacedName{Namespace: c.Namespace, Name: dataStatefulSetName(c)}
	err := r.Get(ctx, key, sts)
	if apierrors.IsNotFound(err) {
		err = r.APIReader.Get(ctx, key, sts)
	}
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading StatefulSet %s/%s: %w", c.Namespace, dataStatefulSetName(c), err)
	}
	return sts, nil
}

// dataVolumeSize is the capacity the data pods' volume claims request: the
// existing StatefulSet's own claim template, which a change of the class's
// storage does not move, or the class's for a StatefulSet not created yet.
// The engine's event stream is sized from it, so it has to be the volume
// the pod actually gets.
func dataVolumeSize(existing *appsv1.StatefulSet, class *crewletaiv1alpha1.EngineClass) resource.Quantity {
	if existing != nil {
		for i := range existing.Spec.VolumeClaimTemplates {
			if claim := &existing.Spec.VolumeClaimTemplates[i]; claim.Name == dataVolume {
				return *claim.Spec.Resources.Requests.Storage()
			}
		}
	}
	return *class.Spec.Data.Storage.Size
}

// runningImage is the engine image in a StatefulSet's pod template.
func runningImage(sts *appsv1.StatefulSet) string {
	for i := range sts.Spec.Template.Spec.Containers {
		if c := &sts.Spec.Template.Spec.Containers[i]; c.Name == engineContainer {
			return c.Image
		}
	}
	return ""
}

// imageRefusal is why the engine image a Company asks for is not run.
type imageRefusal struct {
	reason, message string
}

// engineImage is the image a Company's data pods should run: the class's,
// with the tag replaced by spec.runtime.version when the class allows it.
// It refuses an override the class does not allow, and a version older than
// floor, the newest the company's data was run ready on (versionFloor), since
// the engine migrates its data forward only.
func engineImage(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, floor string) (string, *imageRefusal) {
	image := class.Spec.Engine.Image
	if version := c.Spec.Runtime.Version; version != "" {
		if a := class.Spec.Engine.AllowVersionOverride; a == nil || !*a {
			return "", &imageRefusal{
				reason: crewletaiv1alpha1.ReasonVersionOverrideNotAllowed,
				message: fmt.Sprintf("spec.runtime.version is %s, and EngineClass %s does not set engine.allowVersionOverride: "+
					"remove spec.runtime.version, or allow the override on the class.", version, class.Name),
			}
		}
		image = withTag(image, version)
	}

	if floor == "" {
		return image, nil
	}
	want, err := utilversion.ParseSemantic(tagOf(image))
	if err != nil {
		// A tag that is no version (latest, a branch) cannot be ordered;
		// whoever names one has taken the ordering on themselves.
		return image, nil
	}
	ran, err := utilversion.ParseSemantic(floor)
	if err != nil {
		return image, nil
	}
	if want.LessThan(ran) {
		return "", &imageRefusal{
			reason: crewletaiv1alpha1.ReasonDowngradeRefused,
			message: fmt.Sprintf("The engine image %s is older than %s, which the company's data was last run on: the engine migrates its "+
				"data forward only. Run %s or later; to go back, restore the data volume from a backup taken before the upgrade.",
				image, floor, floor),
		}
	}
	return image, nil
}

// EngineVersionAnnotation is set on each data volume claim to the newest
// engine version its data pod ran ready on. It is the downgrade floor that
// survives with the data: status.engine.version holds the same, but a status
// is lost when a Company is created again over Retained volumes, or restored
// from a backup that drops it (Velero does not restore a custom resource's
// status by default), and a version older than the one that migrated the data
// would then be run against it.
const EngineVersionAnnotation = "crewlet.ai/engine-version"

// versionFloor is the newest of status.engine.version and the claims'
// EngineVersionAnnotation, by semantic version; values that are not one are
// ignored, as engineImage ignores them.
func versionFloor(c *crewletaiv1alpha1.Company, claims []metav1.PartialObjectMetadata) string {
	floor := c.Status.Engine.Version
	newest := semver(floor)
	for i := range claims {
		v := claims[i].Annotations[EngineVersionAnnotation]
		if parsed := semver(v); parsed != nil && (newest == nil || newest.LessThan(parsed)) {
			floor, newest = v, parsed
		}
	}
	return floor
}

// semver parses a semantic version, or returns nil for anything else.
func semver(s string) *utilversion.Version {
	v, err := utilversion.ParseSemantic(s)
	if err != nil {
		return nil
	}
	return v
}

// cachedDataClaims lists the metadata of a Company's data volume claims from
// the cache (CacheOptions keeps the operator's).
func (r *CompanyReconciler) cachedDataClaims(ctx context.Context, c *crewletaiv1alpha1.Company) ([]metav1.PartialObjectMetadata, error) {
	claims := &metav1.PartialObjectMetadataList{}
	claims.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaimList"))
	if err := r.List(ctx, claims, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return nil, fmt.Errorf("listing the data volume claims of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	return claims.Items, nil
}

// recordVersionOnClaims sets EngineVersionAnnotation on every data volume
// claim to status.engine.version, once that is newer than what the claim
// says: only a version the pods ran ready on is recorded, as in the status.
func (r *CompanyReconciler) recordVersionOnClaims(
	ctx context.Context, c *crewletaiv1alpha1.Company, claims []metav1.PartialObjectMetadata,
) error {
	// A tag that is no version cannot be ordered (see engineImage), so it
	// sets no floor.
	recorded := semver(c.Status.Engine.Version)
	if recorded == nil {
		return nil
	}
	for i := range claims {
		claim := &claims[i]
		if on := semver(claim.Annotations[EngineVersionAnnotation]); on != nil && !on.LessThan(recorded) {
			continue
		}
		base := claim.DeepCopy()
		if claim.Annotations == nil {
			claim.Annotations = map[string]string{}
		}
		claim.Annotations[EngineVersionAnnotation] = c.Status.Engine.Version
		if err := r.Patch(ctx, claim, client.MergeFrom(base), client.FieldOwner(FieldOwner)); err != nil {
			return fmt.Errorf("recording the engine version on PersistentVolumeClaim %s/%s: %w", claim.Namespace, claim.Name, err)
		}
	}
	return nil
}

// withTag replaces the tag of an image reference by tag. The API server has
// refused allowVersionOverride on an image pinned by digest.
func withTag(image, tag string) string {
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		image = image[:colon]
	}
	return image + ":" + tag
}

// tagOf is the tag of an image reference, or "" for an image by digest only.
func tagOf(image string) string {
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		return image[colon+1:]
	}
	return ""
}

// rollout is what a data StatefulSet's status says about its pods.
type rollout struct {
	ready   bool
	message string
}

// rolloutOf reads a StatefulSet's status: ready when the controller has seen
// the current spec, every replica runs the current template and is ready,
// and no pod of an older template is left.
func rolloutOf(sts *appsv1ac.StatefulSetApplyConfiguration) rollout {
	want := *sts.Spec.Replicas
	status := sts.Status
	if status == nil {
		status = appsv1ac.StatefulSetStatus()
	}
	replicas, ready, updated := value(status.Replicas), value(status.ReadyReplicas), value(status.UpdatedReplicas)
	switch {
	case value(status.ObservedGeneration) < value(sts.Generation):
		return rollout{message: "The StatefulSet controller has not yet seen the current data StatefulSet."}
	case replicas != want || ready < want || updated < want || value(status.CurrentRevision) != value(status.UpdateRevision):
		return rollout{message: fmt.Sprintf("%d of %d data pods are ready and run the current template.", min(ready, updated), want)}
	default:
		return rollout{ready: true, message: fmt.Sprintf("%d of %d data pods are ready.", ready, want)}
	}
}

// value is what an optional field of a status holds; unset is the zero
// value, which the API server omits.
func value[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// recordRollout sets the Company's status.topology.data and, once every data
// pod runs the template and is ready, status.engine.
func recordRollout(c *crewletaiv1alpha1.Company, sts *appsv1ac.StatefulSetApplyConfiguration, done bool) {
	if s := sts.Status; s != nil {
		c.Status.Topology.Data = crewletaiv1alpha1.DataTopologyStatus{
			Replicas:        new(value(s.Replicas)),
			ReadyReplicas:   new(value(s.ReadyReplicas)),
			UpdatedReplicas: new(value(s.UpdatedReplicas)),
		}
	}
	if !done || *sts.Spec.Replicas == 0 {
		return
	}
	image := *sts.Spec.Template.Spec.Containers[0].Image
	c.Status.Engine = crewletaiv1alpha1.EngineStatus{Image: image, Version: tagOf(image)}
}

// runtimeCondition is a RuntimeReady condition.
func runtimeCondition(status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: crewletaiv1alpha1.ConditionRuntimeReady, Status: status, Reason: reason, Message: message}
}
