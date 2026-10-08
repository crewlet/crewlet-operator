package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DefaultEngineClassAnnotation marks the EngineClass a Company that names no
// class uses. Exactly one class should carry it with the value "true"; with
// none or several, a Company without spec.engineClassName is not accepted.
const DefaultEngineClassAnnotation = "crewlet.ai/is-default-class"

// EngineSettings is the engine image a class runs.
// +kubebuilder:validation:XValidation:rule="!has(self.allowVersionOverride) || !self.allowVersionOverride || !self.image.contains('@')",message="allowVersionOverride needs an image by tag: a Company's spec.runtime.version replaces the tag, and an image pinned by digest has none"
type EngineSettings struct {
	// image is the Crewlet engine image, by tag or by digest, for example
	// ghcr.io/crewlet/crewlet:v0.14.2. There is no default: the platform
	// decides which build its tenants run. A Company's spec.runtime.version
	// replaces the tag, so an image pinned by digest cannot be combined with
	// allowVersionOverride.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image,omitempty"`

	// imagePullPolicy is the pull policy of the engine container. Unset
	// takes the Kubernetes default for the image reference.
	// +optional
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// imagePullSecrets name Secrets in each Company's namespace that pull
	// the image. They are referenced, not copied: the platform puts them
	// into the tenant namespaces it creates.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=8
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// allowVersionOverride lets a Company of this class choose its own engine
	// version with spec.runtime.version. Off by default: the class usually
	// decides which version its tenants run.
	// +optional
	AllowVersionOverride *bool `json:"allowVersionOverride,omitempty"`
}

// DataStorage is the volume each data pod keeps the company's estate on.
type DataStorage struct {
	// storageClassName is the StorageClass of the data volumes. Unset uses
	// the cluster's default class.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	StorageClassName string `json:"storageClassName,omitempty"`

	// size is the capacity requested for each data volume. It holds the
	// store, the event stream and the store's cache. There is no default: how
	// much a tenant may keep is a platform decision.
	//
	// It applies to the data volumes of companies created after it is set.
	// An existing company's volumes keep the size they were created with:
	// they are claimed through the StatefulSet's volume claim templates,
	// which Kubernetes does not let change, and resizing a volume is a
	// decision about one company's storage class and data, not the class's.
	// +required
	// +kubebuilder:validation:XValidation:rule="quantity(string(self)).isGreaterThan(quantity('0'))",message="size must be greater than zero"
	Size *resource.Quantity `json:"size,omitempty"`
}

// DataPodSettings sizes the pods that hold a company's estate. In v1alpha1 a
// company runs exactly one data pod, which also serves the API, runs every
// seat and runs the background workers.
type DataPodSettings struct {
	// allowedReplicas is the set of data-pod counts a Company of this class
	// may choose in spec.topology.data.replicas. v1alpha1 runs exactly one
	// data pod, so the only value admitted is 1; 3 and 5, a highly available
	// estate, are added when the operator can run them.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:items:Enum=1
	// +default=[1]
	AllowedReplicas []int32 `json:"allowedReplicas,omitempty"`

	// resources are the compute resources of the engine container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// storage is the data volume each data pod gets.
	// +required
	Storage DataStorage `json:"storage,omitzero"`

	// terminationGracePeriodSeconds is how long a data pod may take to stop.
	// The engine finishes running turns before it exits and puts no bound on
	// that itself, so the default is five minutes, which covers a long turn,
	// rather than the Kubernetes default of thirty seconds, which does not.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:validation:Maximum=3600
	// +default=300
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// StreamMode is where a company's event stream runs.
// +kubebuilder:validation:Enum=Embedded
type StreamMode string

// The stream modes.
const (
	// StreamModeEmbedded runs NATS JetStream inside the engine process, with
	// its data on the data volume. An external NATS cluster is not offered in
	// v1alpha1.
	StreamModeEmbedded StreamMode = "Embedded"
)

// StreamSettings is how a company's event stream runs.
// +kubebuilder:validation:MinProperties=1
type StreamSettings struct {
	// mode is where the stream runs. Embedded is the only mode in v1alpha1.
	// +optional
	// +default="Embedded"
	Mode StreamMode `json:"mode,omitempty"`
}

// ObjectBackend is where a company's files are kept. It maps onto Crewlet's
// store.objects.backend (NATS is `nats`, S3 is `s3`).
// +kubebuilder:validation:Enum=NATS;S3
type ObjectBackend string

// The object backends.
const (
	// ObjectBackendNATS keeps files in a bucket on the company's own event
	// stream, on the data volume.
	ObjectBackendNATS ObjectBackend = "NATS"
	// ObjectBackendS3 keeps files in an S3-compatible bucket.
	ObjectBackendS3 ObjectBackend = "S3"
)

// S3ObjectSettings is the S3-compatible bucket a class's companies keep their
// files in. Each field maps onto the same-named field of Crewlet's
// store.objects.s3.
type S3ObjectSettings struct {
	// endpoint is the S3 API's base URL. Unset is Amazon S3's own endpoint
	// for the region.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="endpoint must be an absolute http or https URL"
	Endpoint string `json:"endpoint,omitempty"`

	// region is the bucket's region. Every request is signed for one, even
	// where the provider ignores it (Cloudflare R2: auto, MinIO: us-east-1).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Region string `json:"region,omitempty"`

	// bucket is the bucket's name.
	// +required
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	Bucket string `json:"bucket,omitempty"`

	// prefix is prepended to every key, so that one bucket holds many
	// companies. It is a template: {{namespace}}, {{name}} and {{uid}} are
	// replaced by the Company's namespace, name and UID, and the result must
	// differ between companies, so it must contain {{uid}}, or both
	// {{namespace}} and {{name}}. The default keys files by namespace and
	// name, which is also what the data volumes are named by: a Company
	// re-created under its old name after a Retain deletion finds both its
	// estate and its files again.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +default="{{namespace}}/{{name}}/"
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="prefix may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || (self.contains('{{namespace}}') && self.contains('{{name}}'))",message="prefix must contain {{uid}}, or both {{namespace}} and {{name}}, so that companies sharing the bucket do not share keys"
	Prefix string `json:"prefix,omitempty"`

	// pathStyle addresses the bucket in the URL path rather than in the host
	// name; MinIO and most self-hosted gateways need it.
	// +optional
	PathStyle *bool `json:"pathStyle,omitempty"`

	// credentialsSecretRef is a Secret holding the keys accessKeyID and
	// secretAccessKey (S3AccessKeyIDKey, S3SecretAccessKeyKey). Unset uses
	// the pod's own credentials through the SDK's default chain: a workload
	// identity (EKS Pod Identity, or IRSA and GKE Workload Identity with the
	// class's spec.serviceAccount.annotations) or the node's role. The engine reads object-store credentials
	// from its environment, so the operator copies the two values into a
	// Secret it owns in each Company's namespace and references them from the
	// pod; the values never appear in the pod template.
	// +optional
	CredentialsSecretRef NamespacedSecretReference `json:"credentialsSecretRef,omitzero"`
}

// ObjectSettings is where a class's companies keep their files.
// +kubebuilder:validation:XValidation:rule="self.backend == 'S3' ? has(self.s3) : !has(self.s3)",message="s3 is required with backend S3 and not allowed otherwise"
// +kubebuilder:validation:MinProperties=1
type ObjectSettings struct {
	// backend is where files are kept. Changing it does not move existing
	// files.
	// +optional
	// +default="NATS"
	Backend ObjectBackend `json:"backend,omitempty"`

	// s3 is the bucket, for backend S3.
	// +optional
	S3 S3ObjectSettings `json:"s3,omitzero"`
}

// GatewayParentReference names a Gateway API Gateway (or one listener of it)
// that the operator attaches a company's HTTPRoute to.
type GatewayParentReference struct {
	// namespace is the Gateway's namespace. Unset is the Company's own
	// namespace.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`

	// name is the Gateway's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// sectionName is the name of one listener of the Gateway. Unset attaches
	// to every listener that accepts the route.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	SectionName string `json:"sectionName,omitempty"`
}

// GatewayExposure attaches a company's routes to Gateways.
type GatewayExposure struct {
	// parentRefs are the Gateways the HTTPRoute attaches to.
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	ParentRefs []GatewayParentReference `json:"parentRefs,omitempty"`
}

// IngressTLS terminates TLS on a company's Ingress.
type IngressTLS struct {
	// secretName is the TLS Secret, in the Company's namespace, the Ingress
	// presents. It is a template like the hostname: {{namespace}}, {{name}}
	// and {{uid}} are replaced. A certificate controller that issues
	// certificates from Ingress annotations creates it; set those annotations
	// under ingress.annotations.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="secretName may use only the variables {{namespace}}, {{name}} and {{uid}}"
	SecretName string `json:"secretName,omitempty"`
}

// IngressExposure puts a company's routes on an Ingress.
type IngressExposure struct {
	// className is the IngressClass. Unset uses the cluster's default class.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	ClassName string `json:"className,omitempty"`

	// annotations are added to the Ingress, for the controller that serves it
	// or a certificate controller.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	Annotations map[string]string `json:"annotations,omitempty"`

	// tls terminates TLS on the Ingress. Unset serves plain HTTP, which is
	// right only behind something else that terminates TLS: vendors deliver
	// webhooks over HTTPS only.
	// +optional
	TLS IngressTLS `json:"tls,omitzero"`
}

// WebhookExposureMode is how vendors' webhooks reach a company's engine.
// +kubebuilder:validation:Enum=Gateway;Ingress;External;None
type WebhookExposureMode string

// The webhook exposure modes.
const (
	// WebhookExposureGateway routes the public hostname's /webhooks/ path to
	// the engine with an HTTPRoute.
	WebhookExposureGateway WebhookExposureMode = "Gateway"
	// WebhookExposureIngress does the same with an Ingress.
	WebhookExposureIngress WebhookExposureMode = "Ingress"
	// WebhookExposureExternal creates nothing public: something outside the
	// operator (a relay, an API gateway) forwards to the engine's Service,
	// and each Company states the address it is reached on in
	// spec.exposure.publicBaseURL.
	WebhookExposureExternal WebhookExposureMode = "External"
	// WebhookExposureNone exposes nothing; integrations that need inbound
	// delivery report that they are not exposed.
	WebhookExposureNone WebhookExposureMode = "None"
)

// WebhookExposure is how vendors' webhooks reach a company's engine. Only the
// path prefix /webhooks/ is ever exposed by it: the engine's admin API shares
// its port and must not be public.
// +kubebuilder:validation:XValidation:rule="self.mode == 'Gateway' ? has(self.gateway) : !has(self.gateway)",message="gateway is required with mode Gateway and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode == 'Ingress' ? has(self.ingress) : !has(self.ingress)",message="ingress is required with mode Ingress and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode in ['Gateway', 'Ingress'] ? has(self.hostname) : !has(self.hostname)",message="hostname is required with modes Gateway and Ingress and not allowed otherwise"
// +kubebuilder:validation:MinProperties=1
type WebhookExposure struct {
	// mode is how webhooks reach the engine.
	// +optional
	// +default="None"
	Mode WebhookExposureMode `json:"mode,omitempty"`

	// gateway is the Gateways to attach to, for mode Gateway.
	// +optional
	Gateway GatewayExposure `json:"gateway,omitzero"`

	// ingress is the Ingress settings, for mode Ingress.
	// +optional
	Ingress *IngressExposure `json:"ingress,omitempty"`

	// hostname is the public host name of each company, as a template:
	// {{namespace}}, {{name}} and {{uid}} are replaced by the Company's
	// namespace, name and UID. The result is the company's public base URL,
	// https://<hostname>, which every vendor is told to deliver to. Prefer
	// {{uid}}: the host name is public, and a UID says nothing about the
	// tenant where a name can; and a UID is never reused, so a vendor still
	// configured for a deleted company can never reach a company created
	// later under the same name. The template must contain {{uid}}, or both
	// {{namespace}} and {{name}}, so that no two companies share a host name.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="hostname may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || (self.contains('{{namespace}}') && self.contains('{{name}}'))",message="hostname must contain {{uid}}, or both {{namespace}} and {{name}}, so that no two companies share a host name"
	// +kubebuilder:validation:XValidation:rule="self.replace('{{namespace}}', 'n').replace('{{name}}', 'n').replace('{{uid}}', 'u').matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="hostname must be a lowercase DNS name once its variables are replaced"
	Hostname string `json:"hostname,omitempty"`
}

// APIExposureMode is how a company's engine API is reached from outside its
// namespace.
// +kubebuilder:validation:Enum=ClusterInternal;Gateway;Ingress
type APIExposureMode string

// The API exposure modes.
const (
	// APIExposureClusterInternal reaches the API only through the company's
	// Service, from inside the cluster.
	APIExposureClusterInternal APIExposureMode = "ClusterInternal"
	// APIExposureGateway also routes a host name to the API with an
	// HTTPRoute. Attach it to an internal Gateway: the API includes the
	// company's configuration and secret store.
	APIExposureGateway APIExposureMode = "Gateway"
	// APIExposureIngress does the same with an Ingress.
	APIExposureIngress APIExposureMode = "Ingress"
)

// APIExposure is how a company's engine API is reached. The API always
// requires a bearer token, which the operator generates per company and
// publishes in a Secret named by the Company's status.apiAccessSecretRef.
// +kubebuilder:validation:XValidation:rule="self.mode == 'Gateway' ? has(self.gateway) : !has(self.gateway)",message="gateway is required with mode Gateway and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode == 'Ingress' ? has(self.ingress) : !has(self.ingress)",message="ingress is required with mode Ingress and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode in ['Gateway', 'Ingress'] ? has(self.hostname) : !has(self.hostname)",message="hostname is required with modes Gateway and Ingress and not allowed otherwise"
// +kubebuilder:validation:MinProperties=1
type APIExposure struct {
	// mode is how the API is reached.
	// +optional
	// +default="ClusterInternal"
	Mode APIExposureMode `json:"mode,omitempty"`

	// gateway is the Gateways to attach to, for mode Gateway.
	// +optional
	Gateway GatewayExposure `json:"gateway,omitzero"`

	// ingress is the Ingress settings, for mode Ingress.
	// +optional
	Ingress *IngressExposure `json:"ingress,omitempty"`

	// hostname is the API's host name, as a template with the same
	// variables and rules as webhooks.hostname.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="hostname may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || (self.contains('{{namespace}}') && self.contains('{{name}}'))",message="hostname must contain {{uid}}, or both {{namespace}} and {{name}}, so that no two companies share a host name"
	// +kubebuilder:validation:XValidation:rule="self.replace('{{namespace}}', 'n').replace('{{name}}', 'n').replace('{{uid}}', 'u').matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="hostname must be a lowercase DNS name once its variables are replaced"
	Hostname string `json:"hostname,omitempty"`
}

// ExposureSettings is how a class's companies are reached.
// +kubebuilder:validation:MinProperties=1
type ExposureSettings struct {
	// webhooks is how vendors' webhooks reach the engine.
	// +optional
	// +default={"mode": "None"}
	Webhooks WebhookExposure `json:"webhooks,omitzero"`

	// api is how the engine's API is reached.
	// +optional
	// +default={"mode": "ClusterInternal"}
	API APIExposure `json:"api,omitzero"`
}

// NetworkPolicyMode is whether the operator restricts traffic to the engine.
// +kubebuilder:validation:Enum=Isolated;None
type NetworkPolicyMode string

// The network policy modes.
const (
	// NetworkPolicyIsolated admits traffic to the engine's port only from
	// the operator, from the namespaces of the Gateways or Ingress
	// controller the class routes through, and from namespaces labelled
	// crewlet.ai/api-consumer=true (APIConsumerNamespaceLabel). It needs a
	// CNI that enforces NetworkPolicy.
	NetworkPolicyIsolated NetworkPolicyMode = "Isolated"
	// NetworkPolicyNone creates no NetworkPolicy.
	NetworkPolicyNone NetworkPolicyMode = "None"
)

// EnginePodMetadata is metadata added to every engine pod.
// +kubebuilder:validation:XValidation:rule="!has(self.labels) || self.labels.all(k, !k.startsWith('crewlet.ai/') && !k.startsWith('app.kubernetes.io/'))",message="labels under crewlet.ai/ and app.kubernetes.io/ are the operator's own"
// +kubebuilder:validation:MinProperties=1
type EnginePodMetadata struct {
	// labels are added to every engine pod.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	Labels map[string]string `json:"labels,omitempty"`

	// annotations are added to every engine pod.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	Annotations map[string]string `json:"annotations,omitempty"`
}

// EnginePodSpec is the part of a pod's spec a class may set on engine pods.
// It is a closed list rather than a whole PodSpec: the operator owns the
// containers, volumes, probes and security context, because the engine's data
// safety depends on them.
// +kubebuilder:validation:MinProperties=1
type EnginePodSpec struct {
	// nodeSelector constrains engine pods to nodes with these labels.
	// +optional
	// +kubebuilder:validation:MaxProperties=32
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// tolerations let engine pods run on tainted nodes.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// affinity is the engine pods' scheduling affinity.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// topologySpreadConstraints spread engine pods across topology domains.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=8
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// runtimeClassName is the RuntimeClass engine pods run with, for example
	// a sandboxed runtime such as gVisor.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	RuntimeClassName string `json:"runtimeClassName,omitempty"`

	// priorityClassName is the PriorityClass of engine pods.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// env adds environment variables to the engine container of every
	// Company of the class: platform wiring that is the same for all of
	// them, such as the address of the platform's own API. A Company's
	// spec.runtime.env is merged over it by name. The same names are
	// refused as there: those starting with CREWLET_ or OTEL_, and
	// ANTHROPIC_API_KEY and OPENAI_API_KEY.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(e, !e.name.startsWith('CREWLET_') && !e.name.startsWith('OTEL_'))",message="variables starting with CREWLET_ or OTEL_ are set by the operator"
	// +kubebuilder:validation:XValidation:rule="self.all(e, !(e.name in ['ANTHROPIC_API_KEY', 'OPENAI_API_KEY']))",message="ANTHROPIC_API_KEY and OPENAI_API_KEY are model provider keys: give a provider its key with a Secret reference instead"
	Env []ClassEnvVar `json:"env,omitempty"`
}

// EnginePodTemplate is what a class adds to every engine pod.
// +kubebuilder:validation:MinProperties=1
type EnginePodTemplate struct {
	// metadata is added to every engine pod's metadata.
	// +optional
	Metadata EnginePodMetadata `json:"metadata,omitzero"`

	// spec is merged into every engine pod's spec.
	// +optional
	Spec EnginePodSpec `json:"spec,omitzero"`
}

// PlatformLLMProvider is a model provider the platform offers to every
// Company of a class that sets spec.llm.usePlatformProviders. Its keys live in
// a namespace the platform controls and are pushed straight into each opted-in
// engine's secret store, so a tenant never holds them.
// +kubebuilder:validation:XValidation:rule="!has(self.claudeModel) || self.type == 'Anthropic'",message="claudeModel applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoning) || self.type == 'OpenAI'",message="reasoning applies to type OpenAI only; an Anthropic model's thinking depth is reasoningEffort"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningEffort) || self.type == 'Anthropic' || (self.type == 'OpenAI' && has(self.reasoning) && self.reasoning)",message="reasoningEffort is sent to an OpenAI model only while reasoning is true, and never to an OpenAICompatible one"
// +kubebuilder:validation:XValidation:rule="!has(self.reasoningBudgetTokens) || self.type == 'Anthropic'",message="reasoningBudgetTokens applies to type Anthropic only"
// +kubebuilder:validation:XValidation:rule="self.type != 'OpenAICompatible' || has(self.baseURL)",message="baseURL is required for type OpenAICompatible, which has no default endpoint"
// +kubebuilder:validation:XValidation:rule="self.type == 'OpenAICompatible' || has(self.apiKeySecretRefs)",message="apiKeySecretRefs needs at least one key for types Anthropic and OpenAI"
type PlatformLLMProvider struct {
	// name is the provider's name. Seats and the Company's model order refer
	// to it with kind PlatformLLMProvider.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name,omitempty"`

	LLMProviderSettings `json:",inline"`

	// apiKeySecretRefs are the provider's API keys, rotated as an
	// LLMProvider's are.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	APIKeySecretRefs []NamespacedSecretKeyReference `json:"apiKeySecretRefs,omitempty"`
}

// EngineLimits caps what a Company of a class may declare. The operator
// refuses to apply a company that exceeds a limit and reports it on the
// Company. A control plane should refuse it before it writes the Company; this
// is the cluster's defence in depth.
// +kubebuilder:validation:MinProperties=1
type EngineLimits struct {
	// maxSeats is the most Seats a Company may have. Unset is unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxSeats *int32 `json:"maxSeats,omitempty"`

	// maxMCPServers is the most MCPServers a Company may have. Unset is
	// unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxMCPServers *int32 `json:"maxMCPServers,omitempty"`
}

// TelemetrySettings sends the engine's traces to an OpenTelemetry collector.
// The engine reads its exporter configuration from the standard OTEL_*
// environment variables, which the operator sets from these fields.
type TelemetrySettings struct {
	// otlpEndpoint is the collector's OTLP/HTTP endpoint
	// (OTEL_EXPORTER_OTLP_ENDPOINT).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="otlpEndpoint must be an absolute http or https URL"
	OTLPEndpoint string `json:"otlpEndpoint,omitempty"`

	// headersSecretRef is a Secret key holding the headers sent to the
	// collector, in the OTEL_EXPORTER_OTLP_HEADERS format
	// (key1=value1,key2=value2), usually to authenticate. The operator copies
	// the value into a Secret it owns in each Company's namespace and
	// references it from the pod; the value never appears in the pod
	// template.
	// +optional
	HeadersSecretRef NamespacedSecretKeyReference `json:"headersSecretRef,omitzero"`
}

// EngineServiceAccount is what a class adds to the ServiceAccount the operator
// creates for each Company's engine pods.
// +kubebuilder:validation:MinProperties=1
type EngineServiceAccount struct {
	// annotations are added to the ServiceAccount, for a workload identity
	// that is bound through one: eks.amazonaws.com/role-arn for IRSA,
	// iam.gke.io/gcp-service-account for GKE Workload Identity. That is how
	// the engine reaches an S3 bucket without credentialsSecretRef.
	// +optional
	// +kubebuilder:validation:MaxProperties=16
	Annotations map[string]string `json:"annotations,omitempty"`
}

// RetentionSettings is who answers for a class's companies' backups.
type RetentionSettings struct {
	// backupOwner names whoever is responsible for backing up the companies'
	// data volumes; the engine warns while none is named. Crewlet's
	// retention.backup_owner.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	BackupOwner string `json:"backupOwner,omitempty"`
}

// EngineClassSpec is the defaults and guard rails a platform sets once for
// every Company that names the class, the way a StorageClass does for volumes.
type EngineClassSpec struct {
	// engine is the engine image.
	// +required
	Engine EngineSettings `json:"engine,omitzero"`

	// data sizes the pods that hold each company's estate.
	// +required
	Data DataPodSettings `json:"data,omitzero"`

	// stream is how each company's event stream runs.
	// +optional
	// +default={"mode": "Embedded"}
	Stream StreamSettings `json:"stream,omitzero"`

	// objects is where each company's files are kept.
	// +optional
	// +default={"backend": "NATS"}
	Objects ObjectSettings `json:"objects,omitzero"`

	// exposure is how each company is reached from outside its namespace.
	// +optional
	// +default={"webhooks": {"mode": "None"}, "api": {"mode": "ClusterInternal"}}
	Exposure ExposureSettings `json:"exposure,omitzero"`

	// networkPolicy is whether the operator restricts traffic to the engine.
	// +optional
	// +default="Isolated"
	NetworkPolicy NetworkPolicyMode `json:"networkPolicy,omitempty"`

	// podTemplate is what is added to every engine pod.
	// +optional
	PodTemplate EnginePodTemplate `json:"podTemplate,omitzero"`

	// serviceAccount is what is added to each company's engine
	// ServiceAccount.
	// +optional
	ServiceAccount EngineServiceAccount `json:"serviceAccount,omitzero"`

	// platformLLMProviders are model providers the platform offers to
	// companies that opt in.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	PlatformLLMProviders []PlatformLLMProvider `json:"platformLLMProviders,omitempty"`

	// limits caps what a company may declare.
	// +optional
	Limits EngineLimits `json:"limits,omitzero"`

	// telemetry sends the engine's traces to a collector. Unset exports
	// nothing.
	// +optional
	Telemetry TelemetrySettings `json:"telemetry,omitzero"`

	// retention is who answers for backups.
	// +optional
	Retention RetentionSettings `json:"retention,omitzero"`
}

// EngineClassStatus is what the operator observed about an EngineClass.
// +kubebuilder:validation:MinProperties=1
type EngineClassStatus struct {
	// conditions has Accepted: whether the class is one the operator can run
	// companies with.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// EngineClass is a platform's defaults and guard rails for running Crewlet
// engines: the image, the size of the data pods, how companies are exposed,
// and limits. A Company names one in spec.engineClassName; in a multi-tenant platform, a service tier
// usually maps to a class.
//
// Like a StorageClass, a class is not access-controlled: anyone who may write
// a Company may name any class (see the Company's spec.engineClassName).
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ec,categories=crewlet
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.engine.image`
// +kubebuilder:printcolumn:name="Webhooks",type=string,JSONPath=`.spec.exposure.webhooks.mode`
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type EngineClass struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the class's desired state.
	// +required
	Spec EngineClassSpec `json:"spec,omitzero"`

	// status is the class's observed state.
	// +optional
	Status EngineClassStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EngineClassList contains a list of EngineClass.
type EngineClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EngineClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &EngineClass{}, &EngineClassList{})
		return nil
	})
}
