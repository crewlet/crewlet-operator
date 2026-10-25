package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
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
	// configDir is where the bootstrap Secret's crewlet.yaml is mounted,
	// from the volume configVolumeName.
	configDir        = "/etc/crewlet"
	configVolumeName = "config"
	// configFile is the key of the bootstrap Secret that holds Tier A, and
	// the file name it is mounted under.
	configFile = "crewlet.yaml"
	// configFileMode lets the engine user read crewlet.yaml through the
	// pod's fsGroup and nobody else, since it holds credentials.
	configFileMode int32 = 0o440
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
	// publicPortName names the public port (render.PublicPort) on the data
	// pods and the engine Service.
	publicPortName = "public"
	// artifactsPortName names the artifact port (render.ArtifactsPort).
	artifactsPortName = "artifacts"
)

// BootstrapHashAnnotation is the pod template annotation that rolls the
// data pods when what they read at start changes: the hash of the bootstrap
// Secret's data, crewlet.yaml and the values the environment reads from it.
// Kubernetes reads a Secret into an environment variable, and the engine its
// crewlet.yaml, only when the container starts.
//
// It hashes the values rather than naming the Secrets' resourceVersions, so
// that a write that changes nothing the pods read (a label, a policy
// engine's annotation) does not restart the engine. A hash of the data says
// nothing about a value: the data holds the keyring's keys, so it cannot be
// guessed at without them.
const BootstrapHashAnnotation = "crewlet.ai/bootstrap-hash"

// LeafListenerAnnotation marks a data pod whose engine opens the leaf
// listener (render.LeafPort), with the value leafListenerOpen. It is how the
// operator knows what the pods that run open, rather than what their next
// template will: the listener accepts any connection, with no
// authentication, so the NetworkPolicy that guards it (<c>-broker) stays
// until no running data pod opens it, however long a rollout that closes it
// takes or is held back.
const LeafListenerAnnotation = "crewlet.ai/leaf-listener"

// leafListenerOpen is LeafListenerAnnotation's value.
const leafListenerOpen = "open"

// The environment variables the operator sets on the engine beyond those
// render.Bootstrap lists.
const (
	envHome            = "HOME"
	envTursoCache      = "TURSO_GO_CACHE_DIR"
	envOTLPEndpoint    = "OTEL_EXPORTER_OTLP_ENDPOINT"
	envOTLPProtocol    = "OTEL_EXPORTER_OTLP_PROTOCOL"
	envOTLPHeaders     = "OTEL_EXPORTER_OTLP_HEADERS"
	envOTELResourceAtt = "OTEL_RESOURCE_ATTRIBUTES"
	envMCPBridgeURL    = "CREWLET_MCP_BRIDGE_URL"
	envOTelReceiverURL = "CREWLET_SANDBOX_OTEL_RECEIVER_URL"
)

// preStopSleepSeconds is how long a stopping data pod keeps serving before
// the engine is told to drain. Endpoint changes reach kube-proxy and the
// Gateways a few seconds after the pod is marked terminating; without the
// pause a webhook could still be routed to a pod that already turned it
// away.
const preStopSleepSeconds = 5

// livenessFailureThreshold is how many failed /health probes, ten seconds
// apart, restart an engine container: a minute, longer than the 45 seconds
// in which the engine's watchdog ends a wedged process itself.
const livenessFailureThreshold = 6

// probePeriodSeconds is how often an engine's startup and readiness probes
// ask: the period of the engine's own placement pass, so readiness follows a
// seat pod's admission within one pass.
const probePeriodSeconds = 5

// startupFailureThreshold is how many failed startup probes, one every
// probePeriodSeconds, give a container at least a startup window of seconds
// from its start, and less than two periods more.
//
// The kubelet does not count the window from the container's start. Its
// startup prober ticks from when the pod's probe worker began, and only
// readiness is ever triggered early, so a container's first probe lands
// anywhere in the period after the container runs. The engine answers each
// probe at once (connection refused, then 503 while it recovers), and the
// kubelet restarts the container on the threshold-th failure in a row: at
// that first probe plus threshold-1 periods, which can be all but a whole
// period short of threshold periods. So the threshold covers the window with
// threshold-1 periods, rounded up. A threshold of the window over the period
// truncated, as it was, cut a window that is no multiple of the period (a
// class's startupSeconds of 1657, the 1752 seconds a 16Gi volume derives) by
// up to four seconds more on top of that period.
func startupFailureThreshold(seconds int32) int32 {
	return (seconds+probePeriodSeconds-1)/probePeriodSeconds + 1
}

// The data pod's startup window when the class sets none: every bound the
// engine puts on its own boot, laid end to end, and the recovery of what the
// volume holds, which the engine waits out unbounded because it grows with
// the data. See DataPodSettings.StartupSeconds for the reasoning.
const (
	// dataPodBootSeconds is every bound the engine puts on a data pod's
	// boot but its object store's, each step waiting after the one before
	// it on the boot's one goroutine, read from Crewlet's source (the
	// mirrors in internal/render/mirror_test.go name each declaration). A
	// data pod is a lone broker member, so every create is a local one, on
	// the solo budgets: a minute each and two for a whole bring-up.
	//   - 60: the lock on the store's native library, which a container
	//     killed while extracting it leaves on the volume
	//     (tursoPrepareTimeout);
	//   - 30: its broker's listeners ready (listenTimeout);
	//   - 60: its connection to its own broker, a 30 s handshake and a 30 s
	//     flush that reads the broker's max_payload (listenTimeout twice);
	//   - 120: the event stream's own streams (soloSequenceBudget);
	//   - 60: a second connection, for coordination;
	//   - 120: the coordination buckets, a window of their own;
	//   - 90: the fleet's record of its object store, as on a seat pod
	//     (seatPodBootSeconds);
	//   - 40: four registrations of what the node answers (flushBudget);
	//   - 40: its admission, a write and seven reads at 5 s (admit);
	//   - 20: the listing of the company's secrets (refreshSecrets): the key
	//     index and the watch, a 5 s idle pass and the 5 s delete of the
	//     watch's consumer;
	//   - 10: the estate service's registration;
	//   - 100: the custody keeper's consumer, looked up, created and read
	//     back, then looked up and read back again to attach;
	//   - 10: the fleet broker's registration;
	//   - 35: the state logs' ceilings, read together under LookupBudget,
	//     then the broker's limit at 5 s;
	//   - 120: the state logs' streams (soloSequenceBudget);
	//   - 500: the join's reads of where the fleet stands: a listing of the
	//     nodes' positions (25 s: 20 for a listing, and 5 to read back its
	//     one row), eight of the logs' trim floors (55 s each: 20, and 5 for
	//     each of the seven floors), and each log's stats at 5 s;
	//   - 10: the window for a donor's offer, when the node is behind, and
	//     the 5 s delete of the consumer a listing in it leaves;
	//   - 130: each state log's consumer (soloSequenceBudget) and the two 5 s
	//     reads that verify the last one's checkpoint past that window;
	//   - 1: the 25 ms polls of the library lock and the listeners.
	// Those counts are the topology's: one data node, seven logs. What
	// grows with what the company holds is not counted, as its recovery is
	// not: 5 s for each secret its store holds (10 for one deleted in the
	// last three hours), which each listing of them may read back one at a
	// time, 15 s for each batch of a seat pod's events a crash left
	// unsettled, which the custody keeper claims before it attaches, and
	// 10 s for each CLI whose credential bundle is restored, behind a lock a
	// `crewlet llm` command in the container may hold.
	dataPodBootSeconds = 60 + 30 + 60 + 120 + 60 + 120 + 90 + 40 + 40 + 20 + 10 + 100 + 10 + 35 + 120 + 500 + 10 + 130 + 1
	// dataPodNATSObjectsSeconds is the object store's step on the default
	// backend: the broker's limit read (LookupBudget, 30 s), then the bucket
	// looked up (30 s), created (30 s, soloBudget), read back and bound
	// (5 s each), inside a window of soloSequenceBudget that never binds
	// them (natsobj.Open).
	dataPodNATSObjectsSeconds = 30 + 30 + 30 + 5 + 5
	// s3ProbeSeconds is the object store's step on an S3 backend, a data
	// pod's or a seat pod's: the probe of the bucket every boot makes
	// (s3obj's probe), a put of a probe object (60 s, callBudget and
	// objstore.MiBPace for its body), its delete, three presigned puts, the
	// delete of a probe a provider stored that it should have refused, and
	// the delete of the probe however the probe ends (30 s each,
	// callBudget).
	s3ProbeSeconds = 60 + 30 + 3*30 + 30 + 30
	// dataPodRecoverySecondsPerGiB is four times the 1.5 s/GiB the engine
	// measured recovering after a crash on a fast local disk: a network-
	// attached volume reads slower, and a window too short restarts a
	// recovery that would have finished, which then starts over.
	dataPodRecoverySecondsPerGiB = 6
)

// dataPodBootWindow is every bound the engine puts on a data pod's boot, its
// object store's by the class's backend: the least window a data pod is
// given, whatever its volume.
func dataPodBootWindow(class *crewletaiv1alpha1.EngineClass) int32 {
	if class.Spec.Objects.Backend == crewletaiv1alpha1.ObjectBackendS3 {
		return dataPodBootSeconds + s3ProbeSeconds
	}
	return dataPodBootSeconds + dataPodNATSObjectsSeconds
}

// dataPodStartupSeconds is how long a data pod's engine may take to answer
// /health 200, which it does once it has recovered what its volume holds (503
// before, from the moment it binds): the class's spec.data.startupSeconds,
// which the API holds to dataPodBootWindow at least, or, unset, the boot's
// own bounds plus the recovery of a full volume. The volume is
// dataVolumeSize's, which moves under a running company only when its owner
// grows its claim, and the window grows with what it then holds.
func dataPodStartupSeconds(class *crewletaiv1alpha1.EngineClass, volume resource.Quantity) int32 {
	if s := class.Spec.Data.StartupSeconds; s > 0 {
		return s
	}
	const gibibyte = 1 << 30
	// A volume too large for the sum below to be taken in an int64 takes the
	// longest window, as every volume over about 14 TiB does. The comparison
	// is exact for a quantity of any size, where Value is not: for a size at
	// or past the largest int64, which the API admits, it saturates there,
	// wraps below zero or reads 0.
	if volume.CmpInt64(math.MaxInt64-gibibyte) > 0 {
		return maxStartupSeconds
	}
	boot := dataPodBootWindow(class)
	recovery := (volume.Value() + gibibyte - 1) / gibibyte * dataPodRecoverySecondsPerGiB
	// Never under the boot's own window: a quantity below zero, which no
	// claim has, would otherwise take a recovery's seconds away from it.
	if recovery <= 0 {
		return boot
	}
	// Bounded by the day before it is converted, the recovery converts
	// checked rather than assumed, and the sum cannot overflow.
	if recovery >= maxStartupSeconds {
		return maxStartupSeconds
	}
	return min(boot+int32(recovery), maxStartupSeconds)
}

// maxStartupSeconds is the longest startup window the API admits, a day: past
// it a recovery is a disk to look at, not a boot to wait for.
const maxStartupSeconds = 86400

// workload is everything a data pod's template is built from.
type workload struct {
	image     string
	bootstrap *render.BootstrapDocument
	// bootstrapHash is the BootstrapHashAnnotation's value.
	bootstrapHash string
	// bridgeURL is the base URL a sandbox reaches the engine's tool bridge
	// at, or empty for an engine that serves none.
	bridgeURL string
	// startupSeconds is the data pod's startup window
	// (dataPodStartupSeconds).
	startupSeconds int32
	// artifacts is render.ArtifactsOpen.
	artifacts bool
}

// applyBootstrap writes the Secret the data pods start from: their
// crewlet.yaml, which holds the keyring, the API tokens and the object
// store's keys, and the class's values the engine's environment reads
// (platform). It returns the hash of its data, for BootstrapHashAnnotation.
func (r *CompanyReconciler) applyBootstrap(
	ctx context.Context, c *crewletaiv1alpha1.Company, doc *render.BootstrapDocument, platform map[string][]byte,
) (string, error) {
	data := maps.Clone(platform)
	data[configFile] = doc.YAML
	secret := corev1ac.Secret(bootstrapSecretName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	if err := r.apply(ctx, c, secretGVK, bootstrapSecretName(c), secret); err != nil {
		return "", err
	}
	return hashOf(data), nil
}

// hashOf is the hex-encoded SHA-256 of a Secret's data, its keys in order.
func hashOf(data map[string][]byte) string {
	h := sha256.New()
	for _, key := range slices.Sorted(maps.Keys(data)) {
		// Lengths first, so that no two different maps hash alike.
		h.Write(fmt.Appendf(nil, "%d:%s%d:", len(key), key, len(data[key])))
		h.Write(data[key])
	}
	return hex.EncodeToString(h.Sum(nil))
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
// ready data pods and backs the routes and status.endpoints.api: its port
// http is the engine's API, which the API's route reaches, its port public
// the engine's public listener, which the webhooks' route reaches, and its
// port artifacts the artifact listener, named even while closed so that the
// artifacts' route resolves. It
// carries the class's service annotations, for a load balancer controller
// that reads a backend's health check there. <c>-data is the StatefulSet's headless governing Service: it gives each
// data pod a stable DNS name, published before the pod is ready, because an
// engine with no document yet answers /ready with 503 and the operator must
// reach it to apply the first one. The seat pods join the data pods' leaf listener by the
// same names, which is why <c>-data also names the leaf port while the class
// opens it: a seat pod that starts while a data pod is not ready yet waits
// for it rather than failing to resolve it.
func (r *CompanyReconciler) applyServices(ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) error {
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
			WithPorts(port, corev1ac.ServicePort().
				WithName(publicPortName).
				WithProtocol(corev1.ProtocolTCP).
				WithPort(render.PublicPort).
				WithTargetPort(intstr.FromString(publicPortName)),
				corev1ac.ServicePort().
					WithName(artifactsPortName).
					WithProtocol(corev1.ProtocolTCP).
					WithPort(render.ArtifactsPort).
					WithTargetPort(intstr.FromString(artifactsPortName))))
	if annotations := class.Spec.Service.Annotations; len(annotations) > 0 {
		engine.WithAnnotations(annotations)
	}
	if err := r.apply(ctx, c, serviceGVK, engineServiceName(c), engine); err != nil {
		return err
	}
	ports := []*corev1ac.ServicePortApplyConfiguration{port}
	if render.LeafOpen(c, class) {
		ports = append(ports, corev1ac.ServicePort().
			WithName(leafPortName).
			WithProtocol(corev1.ProtocolTCP).
			WithPort(render.LeafPort).
			WithTargetPort(intstr.FromString(leafPortName)))
	}
	data := corev1ac.Service(dataServiceName(c), c.Namespace).
		WithLabels(labelsFor(c, componentData)).
		WithOwnerReferences(ownerReference(c)).
		WithSpec(corev1ac.ServiceSpec().
			WithClusterIP(corev1.ClusterIPNone).
			WithPublishNotReadyAddresses(true).
			WithSelector(selectorFor(c, componentData)).
			WithPorts(ports...))
	return r.apply(ctx, c, serviceGVK, dataServiceName(c), data)
}

// applyStatefulSet writes the data StatefulSet and returns it as the API
// server answered, status included.
func (r *CompanyReconciler) applyStatefulSet(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
	existing *appsv1.StatefulSet, replicas int32, w *workload,
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
		WithReplicas(replicas).
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
	container, err := engineContainerOf(c, class, w)
	if err != nil {
		return nil, err
	}
	spec, err := enginePodSpec(c, class, class.Spec.Data.TerminationGracePeriodSeconds)
	if err != nil {
		return nil, err
	}
	spec.WithContainers(container).
		WithVolumes(
			configVolume(bootstrapSecretName(c)),
			// The root filesystem is read-only; the engine and what it
			// spawns (git, stdio MCP servers, CLI agents) write their
			// scratch files under /tmp and their state under HOME.
			corev1ac.Volume().WithName("tmp").WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
			corev1ac.Volume().WithName("home").WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
		)
	labels, annotations := enginePodMetadata(class, labelsFor(c, componentData), w.bootstrapHash)
	if render.LeafOpen(c, class) {
		annotations[LeafListenerAnnotation] = leafListenerOpen
	}
	return corev1ac.PodTemplateSpec().
		WithLabels(labels).
		WithAnnotations(annotations).
		WithSpec(spec), nil
}

// enginePodMetadata are the labels and annotations of an engine pod: the
// class's, then the operator's own over them, and the bootstrap hash that
// rolls the pods when what they read at start changes. The class may not use
// the operator's label prefixes, so its labels never change a selector's.
func enginePodMetadata(class *crewletaiv1alpha1.EngineClass, own map[string]string, hash string) (labels, annotations map[string]string) {
	overlay := class.Spec.PodTemplate.Metadata
	labels = maps.Clone(overlay.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, own)
	annotations = maps.Clone(overlay.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[BootstrapHashAnnotation] = hash
	return labels, annotations
}

// configVolume mounts a bootstrap Secret's crewlet.yaml, readable by the
// engine user alone, since it holds credentials.
func configVolume(secret string) *corev1ac.VolumeApplyConfiguration {
	return corev1ac.Volume().WithName(configVolumeName).WithSecret(corev1ac.SecretVolumeSource().
		WithSecretName(secret).
		WithItems(corev1ac.KeyToPath().WithKey(configFile).WithPath(configFile)).
		WithDefaultMode(configFileMode))
}

// enginePodSpec is what every engine pod, data or seat, has in common: the
// engine's ServiceAccount, with no token mounted, its security context, and
// where the class schedules it. The caller adds the containers and volumes.
func enginePodSpec(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, gracePeriodSeconds int64,
) (*corev1ac.PodSpecApplyConfiguration, error) {
	spec := corev1ac.PodSpec().
		WithServiceAccountName(serviceAccountName(c)).
		WithAutomountServiceAccountToken(false).
		// Service links would put an environment variable for every
		// Service of the namespace into the engine's environment, which
		// is read as configuration (${VAR}) and should hold only what the
		// operator and the class put there.
		WithEnableServiceLinks(false).
		WithTerminationGracePeriodSeconds(gracePeriodSeconds).
		WithSecurityContext(corev1ac.PodSecurityContext().
			WithRunAsNonRoot(true).
			WithRunAsUser(engineUID).
			WithRunAsGroup(engineUID).
			// The data volume is written by the engine user; fsGroup makes
			// a fresh volume writable, and OnRootMismatch skips the
			// recursive walk of a large estate on every start.
			WithFSGroup(engineUID).
			WithFSGroupChangePolicy(corev1.FSGroupChangeOnRootMismatch).
			WithSeccompProfile(corev1ac.SeccompProfile().WithType(corev1.SeccompProfileTypeRuntimeDefault)))

	for _, s := range class.Spec.Engine.ImagePullSecrets {
		spec.WithImagePullSecrets(corev1ac.LocalObjectReference().WithName(s.Name))
	}
	ps := class.Spec.PodTemplate.Spec
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
	return spec, nil
}

// engineContainerOf is the engine container of a data pod.
func engineContainerOf(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, w *workload,
) (*corev1ac.ContainerApplyConfiguration, error) {
	env, err := engineEnv(c, class, &engineEnvironment{
		bootstrap:   w.bootstrap.Env,
		secret:      bootstrapSecretName(c),
		statefulSet: dataStatefulSetName(c),
		// On the data volume, so a restart does not extract the store's
		// 20 MB native library again.
		tursoCache: render.DataDir + "/cache",
		// Crewlet requires the URL to address the node that holds each
		// bridged session. The company's public address reaches the one
		// data pod, and a seat pod is given no URL, so it opens no bridged
		// session: the engine refuses the agent-mode turns of a seat placed
		// there, which the Seat reports (render's ExecutorOf). Every bridged
		// session is the data pod's.
		bridgeURL: w.bridgeURL,
	})
	if err != nil {
		return nil, err
	}
	// The startup probe gives the engine its whole recovery before the
	// liveness probe may restart it (dataPodStartupSeconds). /ready answers
	// 503 while the engine starts, has no document, sheds load, or drains,
	// which takes the pod out of the engine Service.
	container := withProbes(engineContainerBase(class, w.image, env), w.startupSeconds).
		WithVolumeMounts(
			corev1ac.VolumeMount().WithName(dataVolume).WithMountPath(render.DataDir),
			corev1ac.VolumeMount().WithName(configVolumeName).WithMountPath(configDir).WithReadOnly(true),
			corev1ac.VolumeMount().WithName("tmp").WithMountPath("/tmp"),
			corev1ac.VolumeMount().WithName("home").WithMountPath(homeDir),
		).
		WithLifecycle(corev1ac.Lifecycle().
			WithPreStop(corev1ac.LifecycleHandler().
				WithSleep(corev1ac.SleepAction().WithSeconds(preStopSleepSeconds))))
	// The public listener, which every data pod's bootstrap document opens,
	// the artifact listener while the Company has an artifact origin, and the
	// leaf listener the seat pods join through, which it opens when
	// render.LeafOpen says so.
	container.WithPorts(corev1ac.ContainerPort().
		WithName(publicPortName).
		WithContainerPort(render.PublicPort).
		WithProtocol(corev1.ProtocolTCP))
	if w.artifacts {
		container.WithPorts(corev1ac.ContainerPort().
			WithName(artifactsPortName).
			WithContainerPort(render.ArtifactsPort).
			WithProtocol(corev1.ProtocolTCP))
	}
	if render.LeafOpen(c, class) {
		container.WithPorts(corev1ac.ContainerPort().
			WithName(leafPortName).
			WithContainerPort(render.LeafPort).
			WithProtocol(corev1.ProtocolTCP))
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

// withProbes gives an engine container its HTTP port, render.APIPort, and
// probes it there: /health for startup, for as long as startupSeconds, and
// for liveness, and /ready for readiness. Every engine pod binds the port,
// whatever its roles, and answers both probes on it; what /ready means
// follows from its roles (see seatPodTemplate).
func withProbes(container *corev1ac.ContainerApplyConfiguration, startupSeconds int32) *corev1ac.ContainerApplyConfiguration {
	get := func(path string) *corev1ac.HTTPGetActionApplyConfiguration {
		return corev1ac.HTTPGetAction().WithPath(path).WithPort(intstr.FromString(portName))
	}
	return container.
		WithPorts(corev1ac.ContainerPort().
			WithName(portName).
			WithContainerPort(render.APIPort).
			WithProtocol(corev1.ProtocolTCP)).
		// Five seconds to answer, as liveness and readiness have: /health
		// reads the coordination plane under a budget of its own (about two
		// seconds), and the Kubernetes default of one would count a slow
		// plane as a failed start.
		WithStartupProbe(corev1ac.Probe().
			WithHTTPGet(get("/health")).
			WithPeriodSeconds(probePeriodSeconds).
			WithTimeoutSeconds(5).
			WithFailureThreshold(startupFailureThreshold(startupSeconds))).
		// /health answers 200 from the end of the recovery for as long as
		// the process lives, through the company's part of the boot and a
		// drain too, so liveness never kills a pod that is starting its
		// company's tool servers or finishing its turns. A minute of
		// failures before a restart, as the engine's own deployment guide
		// sets: its watchdog ends a process whose watched duty stalls for a
		// seat lease's TTL (45 seconds) by itself, so liveness is the
		// backstop for a process too wedged to do even that, and the
		// Kubernetes default of three failures (30 seconds) would restart
		// one the watchdog was about to judge.
		WithLivenessProbe(corev1ac.Probe().
			WithHTTPGet(get("/health")).
			WithPeriodSeconds(10).
			WithTimeoutSeconds(5).
			WithFailureThreshold(livenessFailureThreshold)).
		WithReadinessProbe(corev1ac.Probe().
			WithHTTPGet(get("/ready")).
			WithPeriodSeconds(probePeriodSeconds).
			WithTimeoutSeconds(5))
}

// engineContainerBase is what every engine container has in common: the
// image, the command line, the environment and a security context that
// leaves the process nothing to escalate to.
func engineContainerBase(
	class *crewletaiv1alpha1.EngineClass, image string, env []*corev1ac.EnvVarApplyConfiguration,
) *corev1ac.ContainerApplyConfiguration {
	container := corev1ac.Container().
		WithName(engineContainer).
		WithImage(image).
		// The image's entrypoint is the crewlet binary under an init
		// process; these are its arguments.
		WithArgs("run", "-config", configDir+"/"+configFile).
		WithEnv(env...).
		WithSecurityContext(corev1ac.SecurityContext().
			WithRunAsNonRoot(true).
			WithAllowPrivilegeEscalation(false).
			WithReadOnlyRootFilesystem(true).
			WithCapabilities(corev1ac.Capabilities().WithDrop(corev1.Capability("ALL"))))
	if p := class.Spec.Engine.ImagePullPolicy; p != "" {
		container.WithImagePullPolicy(p)
	}
	return container
}

// engineEnvironment is what an engine container's environment is made of
// beyond the class and the Company.
type engineEnvironment struct {
	// bootstrap is what the pod's bootstrap document refers to.
	bootstrap []corev1.EnvVar
	// secret is the bootstrap Secret that holds the class's values for the
	// environment (platformValues).
	secret string
	// statefulSet names the pod's StatefulSet in the telemetry attributes.
	statefulSet string
	// tursoCache is where the store extracts its native library.
	tursoCache string
	// bridgeURL is CREWLET_MCP_BRIDGE_URL, or empty for a pod that serves
	// no tool bridge.
	bridgeURL string
}

// engineEnv is an engine container's environment: what the bootstrap
// document references, then the operator's own settings, then the class's
// and the Company's variables. Kubernetes expands $(VAR) only from variables
// listed earlier, which the telemetry attributes rely on.
func engineEnv(
	c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass, e *engineEnvironment,
) ([]*corev1ac.EnvVarApplyConfiguration, error) {
	var env []*corev1ac.EnvVarApplyConfiguration
	for i := range e.bootstrap {
		v, err := applyConfigOf[corev1ac.EnvVarApplyConfiguration](&e.bootstrap[i])
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
		literal(envTursoCache, e.tursoCache),
	)

	if e.bridgeURL != "" {
		env = append(env, literal(envMCPBridgeURL, e.bridgeURL))
	}

	if t := class.Spec.Telemetry; t.OTLPEndpoint != "" {
		env = append(env,
			literal(envOTLPEndpoint, t.OTLPEndpoint),
			literal(envOTLPProtocol, "http/protobuf"),
			literal(envOTELResourceAtt, fmt.Sprintf("k8s.namespace.name=%s,k8s.statefulset.name=%s,k8s.pod.name=$(%s)",
				c.Namespace, e.statefulSet, render.EnvNodeID)))
		if t.HeadersSecretRef.Name != "" {
			env = append(env, fromSecret(envOTLPHeaders, e.secret, platformKeyOTLPHeaders))
		}
		// Crewlet's sandbox telemetry receiver: a remote sandbox exports to
		// the engine's public listener, the one port it can reach, with a
		// token signed for its run, and the engine forwards to the
		// collector above with its headers, which never enter the box.
		// Every pod is given it, a seat pod's too: the pod that launches a
		// run mints its endpoint, under the company's address, and the data
		// pod that address reaches verifies it with the keyring every pod
		// shares. Without a collector there is nothing to forward to, and
		// without a public address nothing a sandbox could reach.
		if public := c.Status.Endpoints.PublicBaseURL; public != "" {
			env = append(env, literal(envOTelReceiverURL, public))
		}
	}

	for _, v := range class.Spec.PodTemplate.Spec.Env {
		switch {
		case overridden(c, v.Name):
		case v.Value != nil:
			env = append(env, literal(v.Name, *v.Value))
		default:
			env = append(env, fromSecret(v.Name, e.secret, platformKeyEnvPrefix+v.Name))
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
	return r.statefulSetNamed(ctx, c.Namespace, dataStatefulSetName(c))
}

// statefulSetNamed returns the StatefulSet of that name, or nil when there is
// none; see existingStatefulSet.
func (r *CompanyReconciler) statefulSetNamed(ctx context.Context, namespace, name string) (*appsv1.StatefulSet, error) {
	sts := &appsv1.StatefulSet{}
	key := types.NamespacedName{Namespace: namespace, Name: name}
	err := r.Get(ctx, key, sts)
	if apierrors.IsNotFound(err) {
		err = r.APIReader.Get(ctx, key, sts)
	}
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading StatefulSet %s/%s: %w", namespace, name, err)
	}
	return sts, nil
}

// dataVolumeSize is the capacity of the smallest data volume the company's
// pods have. The engine's event stream is sized from it (three quarters of
// it), and the startup window too, so it has to be a volume the pods
// actually get: every data pod shares one bootstrap, so the stream fits the
// smallest.
//
// A volume is first the size of the claim template it was made from: the
// existing StatefulSet's own, which a change of the class's storage does not
// move, or the class's for a StatefulSet not created yet. A claim its owner
// has since expanded counts at the size it now has, the smaller of what it
// asks for and what it was given, so an expansion counts once it has landed.
// A claim is never counted below its template: a provisioner that rounded a
// claim up when it made it moves nothing (the claim still asks for the
// template's size), and one that gave less than asked leaves the stream
// where it was sized. Without this, a company whose volume was too small for
// what its engine reserves (made under an older floor, or before an engine
// that keeps more on its stream) had no way out: the template cannot change,
// and an expanded claim left the stream as small as it was.
func dataVolumeSize(
	existing *appsv1.StatefulSet, class *crewletaiv1alpha1.EngineClass, claims []corev1.PersistentVolumeClaim,
) resource.Quantity {
	template := *class.Spec.Data.Storage.Size
	if existing != nil {
		for i := range existing.Spec.VolumeClaimTemplates {
			if claim := &existing.Spec.VolumeClaimTemplates[i]; claim.Name == dataVolume {
				template = *claim.Spec.Resources.Requests.Storage()
			}
		}
	}
	var smallest *resource.Quantity
	for i := range claims {
		claim := &claims[i]
		if !claim.DeletionTimestamp.IsZero() {
			// Its data goes with it, and its pod gets a new claim from
			// the template.
			continue
		}
		size := template
		requested, asked := claim.Spec.Resources.Requests[corev1.ResourceStorage]
		granted, bound := claim.Status.Capacity[corev1.ResourceStorage]
		if asked && bound {
			grown := requested
			if granted.Cmp(grown) < 0 {
				grown = granted
			}
			if grown.Cmp(size) > 0 {
				size = grown
			}
		}
		if smallest == nil || size.Cmp(*smallest) < 0 {
			smallest = &size
		}
	}
	if smallest == nil {
		return template
	}
	return *smallest
}

// cachedDataClaims lists a Company's data volume claims from the cache
// (CacheOptions keeps the operator's): the engine version recorded on each
// (versionFloor) and the size each has (dataVolumeSize), whose change queues
// the Company (claimResized).
func (r *CompanyReconciler) cachedDataClaims(
	ctx context.Context, c *crewletaiv1alpha1.Company,
) ([]corev1.PersistentVolumeClaim, error) {
	claims := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, claims, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return nil, fmt.Errorf("listing the data volume claims of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	return claims.Items, nil
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
				"data forward only. Run %s or later; to go back, restore the data volume from a backup taken before %s first ran on it.",
				image, floor, floor, floor),
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
func versionFloor(c *crewletaiv1alpha1.Company, claims []corev1.PersistentVolumeClaim) string {
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

// recordVersionOnClaims sets EngineVersionAnnotation on every data volume
// claim to status.engine.version, once that is newer than what the claim
// says: only a version the pods ran ready on is recorded, as in the status.
func (r *CompanyReconciler) recordVersionOnClaims(
	ctx context.Context, c *crewletaiv1alpha1.Company, claims []corev1.PersistentVolumeClaim,
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

// recordDataStatus records the data StatefulSet's pods in
// status.topology.data as they are, for a reconcile that holds them back and
// so writes nothing that would answer with them: the seat pools' report reads
// whether a data pod is ready from there. No StatefulSet is no pod.
func recordDataStatus(c *crewletaiv1alpha1.Company, sts *appsv1.StatefulSet) {
	var s appsv1.StatefulSetStatus
	if sts != nil {
		s = sts.Status
	}
	c.Status.Topology.Data = crewletaiv1alpha1.DataTopologyStatus{
		Replicas: new(s.Replicas), ReadyReplicas: new(s.ReadyReplicas), UpdatedReplicas: new(s.UpdatedReplicas),
	}
}

// runtimeCondition is a RuntimeReady condition.
func runtimeCondition(status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{Type: crewletaiv1alpha1.ConditionRuntimeReady, Status: status, Reason: reason, Message: message}
}
