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
	// registry.example.com/crewlet:v1.2.3. There is no default: the platform
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
	// version with spec.runtime.version. Off by default: the platform
	// usually decides which version a class's tenants run.
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
	// Three quarters of it is the engine's event stream, Crewlet's
	// stream.store_max_bytes: the stream reserves what a node keeps there up
	// front, and a fresh node refuses to boot on a share that cannot hold it
	// (Crewlet's engine.BrokerFloor, and its deployment guide's "minimum room
	// for a fresh node"). Its seven state logs' ceilings at their smallest
	// come to 6979321856 bytes, so a volume must be at least 9Gi, whose share
	// of 6.75Gi holds them. With objects.backend NATS, the default, the object
	// store's bucket reserves a quarter of the share beside them, 9305762474
	// bytes in all, so such a class must give at least 12Gi, whose share is
	// 9Gi. That is the least that boots: the mailboxes and coordination
	// buckets grow into what is left, so give a company that runs for long
	// more. The stream's ceiling is 64Ti (config.StoreMaxBytesCeiling, a
	// guard against a unit mistake), so a volume larger than about 85Ti gives
	// the stream 64Ti and the rest to the store and its snapshots.
	//
	// It applies to the data volumes of companies created after it is set.
	// An existing company's volumes keep the size they were created with:
	// they are claimed through the StatefulSet's volume claim templates,
	// which Kubernetes does not let change, and growing a volume is a
	// decision about one company's storage class and data, not the class's.
	// Its owner expands the company's claim instead, and once the expansion
	// lands the operator sizes the stream from what the claim then has.
	// +required
	// +kubebuilder:validation:XValidation:rule="!quantity(string(self)).isLessThan(quantity('9Gi'))",message="size must be at least 9Gi: three quarters of the volume is the engine's event stream, and a fresh node of Crewlet reserves at least 6979321856 bytes of it for its state logs"
	Size *resource.Quantity `json:"size,omitempty"`
}

// DataPodSettings sizes the pods that hold a company's estate. In v1alpha1 a
// company runs exactly one data pod, which also serves the API, runs the
// background workers and runs every seat placed in the pool data.
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

	// startupSeconds is how long a data pod's engine may take, from its
	// start, to answer /health 200: the time it takes to recover what it holds
	// on its volume (the store, the event stream, the replicated estate) and
	// bring its broker up. Crewlet waits that recovery out rather than
	// bounding it, because it grows with the data, so the startup probe is
	// what bounds it: it allows at least this long from the container's
	// start, and less than ten seconds more. A boot that runs past it is
	// restarted and begins its recovery again, so err long: a window that is
	// too long costs nothing on a healthy boot, only the time to restart one
	// that hung.
	//
	// It is at least every bound Crewlet puts on a data pod's boot, laid end
	// to end, which a window may not cut short: 1636 seconds, or 1776 with
	// objects.backend S3, whose bucket every boot probes. Those are its waits
	// for its own broker and store (provisioning the event stream, the
	// coordination buckets, the object store and the state logs, two minutes
	// a window on the broker of one member a data pod runs, and the requests
	// around them), never for a peer or a seat pod. One of them grows with
	// what the node holds and is left to the recovery's share, as the
	// recovery is: the claim of each batch of a seat pod's events the node
	// kept and could not settle, 15 s a batch. Unset derives it from the
	// data volume: that floor plus six seconds per GiB of the volume. That is
	// four times the second and a half per GiB Crewlet measured recovering
	// after a crash on a fast local disk, for volumes that are
	// network-attached; set this for disks slower than that. Nothing the
	// company configures counts: the engine answers /health once it has
	// recovered, and only then reads the company's secrets, starts its tool
	// servers and restores its CLIs' logins.
	// Changing it changes the data pods' template, which restarts them.
	// +optional
	// +kubebuilder:validation:Minimum=1636
	// +kubebuilder:validation:Maximum=86400
	StartupSeconds int32 `json:"startupSeconds,omitempty"`

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

// SeatPoolPolicy is whether a class's companies may run seats on pods of
// their own, outside the data pods.
// +kubebuilder:validation:Enum=Forbidden;Allowed
type SeatPoolPolicy string

// The seat pool policies.
const (
	// SeatPoolsForbidden runs every seat on the data pods. A Company of the
	// class may declare no spec.topology.seatPools.
	SeatPoolsForbidden SeatPoolPolicy = "Forbidden"
	// SeatPoolsAllowed lets a Company declare seat pools, and opens the
	// leaf listener its seat pods join the company's event stream through
	// on every data pod of the class, from the data pod's first start, so
	// that a company adding its first pool restarts nothing.
	SeatPoolsAllowed SeatPoolPolicy = "Allowed"
)

// SeatPoolDefaults sizes the pods of a seat pool that does not size its own,
// and the compute of every seat pod, which the class alone sets: a
// Company's pools choose how many pods they run, within maxPods, and the
// class what each of them may take, as it does for the data pods.
// +kubebuilder:validation:MinProperties=1
type SeatPoolDefaults struct {
	// maxConcurrentTurns is how many turns one seat pod runs at once: Crewlet's
	// node.max_concurrent. Unset takes the engine's default, 32. A seat runs
	// one turn at a time, so a pod never runs more turns at once than it
	// holds seats.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxConcurrentTurns int32 `json:"maxConcurrentTurns,omitempty"`

	// resources are the compute resources of every seat pod's engine
	// container. Unset sets none.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// terminationGracePeriodSeconds is how long a seat pod may take to stop.
	// A seat pod finishes its running turns before it exits, with no bound
	// of its own, so unset is five minutes, as for a data pod, rather than
	// the Kubernetes default of thirty seconds.
	// +optional
	// +kubebuilder:validation:Minimum=30
	// +kubebuilder:validation:Maximum=3600
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`

	// scratchSizeLimit caps the emptyDir a seat pod keeps its scratch store
	// in: a store the engine deletes at every start, its native library
	// (about 20 MB, extracted at every start) and the files of the
	// processes it runs (stdio MCP servers, git). A pod that writes more is
	// evicted. Unset is 2Gi.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!quantity(string(self)).isLessThan(quantity('256Mi'))",message="scratchSizeLimit must be at least 256Mi: the engine extracts its native library of about 20 MB there at every start, beside its scratch store and the files of the processes it runs"
	ScratchSizeLimit *resource.Quantity `json:"scratchSizeLimit,omitempty"`
}

// SeatPoolSettings is whether, and how far, a class's companies may run seats
// on stateless pods of their own (a Company's spec.topology.seatPools). A seat
// pod holds no data: it joins the company's event stream as a NATS leaf of the
// data pods, keeps a scratch store it deletes at every start, and runs the
// seats placed in its pool.
// +kubebuilder:validation:XValidation:rule="(has(self.policy) && self.policy == 'Allowed') || (!has(self.allowPerSeat) && !has(self.maxPods) && !has(self.defaults))",message="allowPerSeat, maxPods and defaults apply to policy Allowed only"
// +kubebuilder:validation:XValidation:rule="!has(self.policy) || self.policy != 'Allowed' || has(self.maxPods)",message="a class that allows seat pools caps them: set maxPods, the most seat pods one company may run"
// +kubebuilder:validation:MinProperties=1
type SeatPoolSettings struct {
	// policy is whether the class's companies may declare seat pools.
	// Changing it restarts every data pod of the class, which opens or
	// closes the leaf listener.
	// +optional
	// +default="Forbidden"
	Policy SeatPoolPolicy `json:"policy,omitempty"`

	// allowPerSeat lets a Company declare a PerSeat pool, which runs one
	// pod (or two, a warm standby) for each seat placed in it. Each costs a
	// pod, and every pod's work grows with the number of seats of the
	// company, so it is off by default and meant for the few seats that
	// need a pod to themselves. It isolates a seat's compute and its
	// failures, not its credentials: every pod of a company can read the
	// company's whole secret store.
	// +optional
	AllowPerSeat *bool `json:"allowPerSeat,omitempty"`

	// maxPods is the most seat pods a Company of the class may run, all of
	// its pools together: each Shared pool's replicas, and each PerSeat
	// pool's replicas for every seat placed in it. The data pods are not
	// counted. Required with policy Allowed: a class's companies size their
	// own pools, and only this bounds how many seat pods a company of the
	// class runs.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxPods int32 `json:"maxPods,omitempty"`

	// defaults size the pods of a pool that does not size its own.
	// +optional
	Defaults SeatPoolDefaults `json:"defaults,omitzero"`
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
//
// The bucket's credentials are each Company's own (its
// spec.objects.credentialsSecretRef), not the class's: whatever reaches a
// company's engine pod is readable by code the tenant runs there (a stdio
// MCP server, a coding CLI), so a key the class shared would let every
// tenant read and write every other tenant's files.
//
// Crewlet reaches the bucket when it starts (a HeadBucket request, on the data
// pods and the seat pods alike), and refuses to start when it cannot. Some
// settings fail that request before it is sent, through the S3 client Crewlet
// is built with (the AWS SDK for Go), however the bucket is set up, so the API
// refuses them where they are written rather than every pod crash-looping on
// them: each rule below names one.
// +kubebuilder:validation:XValidation:rule="!has(self.region) || (has(self.pathStyle) && self.pathStyle) || !self.region.contains('.')",message="region may not hold a '.' unless pathStyle is true: Crewlet's S3 client names the bucket's host after the region, and refuses at start a region holding a '.' wherever it addresses the bucket by host name; name the region without one, such as eu-central-1, or set pathStyle to true"
// +kubebuilder:validation:XValidation:rule="!has(self.region) || !has(self.endpoint) || !(self.region.contains('fips-') || self.region.contains('-fips'))",message="region names a FIPS endpoint (fips- or -fips in it), which Crewlet's S3 client refuses at start beside a custom endpoint: leave the endpoint out to reach Amazon S3's FIPS endpoint for the region, or name the region without fips"
// +kubebuilder:validation:XValidation:rule="!has(self.region) || has(self.endpoint) || !(self.region.contains('fips-') || self.region.contains('-fips')) || !self.region.trim().replace('-fips-', '-').replace('fips-', \"\").replace('-fips', \"\").matches('^cn-[0-9A-Za-z_]+-[0-9]+$')",message="region names a FIPS endpoint in Amazon's China regions, which have none, and Crewlet's S3 client refuses it at start: name the region without fips, such as cn-north-1"
type S3ObjectSettings struct {
	// endpoint is the S3 API's base URL. Unset is Amazon S3's own endpoint
	// for the region. Crewlet's S3 client refuses at start an endpoint with a
	// query, one whose host is not a name, an IPv4 address or an IPv6
	// address in brackets (with no percent-encoding in its zone) followed by
	// at most one port, and no connection is made on a port outside 1 to
	// 65535, so the API refuses each.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="endpoint must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="endpoint may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^#]*[?][^#]')",message="endpoint may not have a query: Crewlet's S3 client refuses at start a custom endpoint with one, before it sends a request"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://') || self.matches('^[^:/?#]+://([^/?#]*@)?([[](([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){1,7}:|:(:[0-9A-Fa-f]{1,4}){1,7}|::|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){1,6}|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){1,5}|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){1,4}|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){1,3}|([0-9A-Fa-f]{1,4}:){5}(:[0-9A-Fa-f]{1,4}){1,2}|([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4})|(([0-9A-Fa-f]{1,4}:){6}|:(:[0-9A-Fa-f]{1,4}){0,5}:|([0-9A-Fa-f]{1,4}:){5}:|([0-9A-Fa-f]{1,4}:)(:[0-9A-Fa-f]{1,4}){0,4}:|([0-9A-Fa-f]{1,4}:){2}(:[0-9A-Fa-f]{1,4}){0,3}:|([0-9A-Fa-f]{1,4}:){3}(:[0-9A-Fa-f]{1,4}){0,2}:|([0-9A-Fa-f]{1,4}:){4}(:[0-9A-Fa-f]{1,4}){0,1}:)(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])([.](25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])){3})(%25[^]%/?#]+)?[]]|[^/?#:[]*)(:[0-9]*)?([/?#]|$)')",message="endpoint's host must be a name, an IPv4 address or an IPv6 address in brackets, with no percent-encoding in its zone, and at most one ':' and a port may follow it: Crewlet's S3 client refuses any other at start (write http://[::1]:9000, not http://::1:9000)"
	// +kubebuilder:validation:XValidation:rule="!isURL(self) || !self.matches('^[^:/?#]+://[^/?#]*:(0+|0*([1-9][0-9]{5,}|[7-9][0-9]{4}|6[6-9][0-9]{3}|65[6-9][0-9]{2}|655[4-9][0-9]|6553[6-9]))([/?#]|$)')",message="endpoint's port must be 1 to 65535: no connection is made on another, and Crewlet refuses to start without its bucket; leave the port out for the scheme's own"
	Endpoint string `json:"endpoint,omitempty"`

	// region is the bucket's region. Every request is signed for one, even
	// where the provider ignores it (Cloudflare R2: auto, MinIO: us-east-1).
	// Crewlet's S3 client puts it into a host name, and refuses at start a
	// region that is not one: letters, digits and '-' in labels of at most 63
	// joined by '.', none starting or ending with '-'. Crewlet trims the white
	// space around it first, and refuses one of white space alone.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="region may not be white space alone: Crewlet signs every request for a region, and refuses to start without one"
	// +kubebuilder:validation:XValidation:rule="self.trim().size() == 0 || self.trim().matches('^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?([.][A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$')",message="region must be letters, digits and '-', in labels of at most 63 joined by '.' and neither starting nor ending with '-', such as eu-central-1: Crewlet's S3 client puts it into a host name, and refuses any other at start, before it sends a request"
	Region string `json:"region,omitempty"`

	// bucket is the bucket's name: a general purpose bucket's. Crewlet's S3
	// client reads a name that begins with arn: as an access point's ARN, and
	// one that ends with --x-s3, --xa-s3 or --op-s3 as an S3 Express One Zone
	// directory bucket or an Outposts access point alias, each of which it
	// addresses in a way of its own and refuses at start where it cannot, so
	// the API refuses those names. Crewlet trims the white space around it,
	// and refuses one of white space alone.
	// +required
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self.trim().size() > 0",message="bucket may not be white space alone: Crewlet keeps the company's files in it, and refuses to start without one"
	// +kubebuilder:validation:XValidation:rule="!(self.trim().startsWith('arn:') || self.trim().endsWith('--x-s3') || self.trim().endsWith('--xa-s3') || self.trim().endsWith('--op-s3'))",message="bucket must name a general purpose bucket: Crewlet's S3 client reads a name that begins with arn: as an access point's ARN, and one that ends with --x-s3, --xa-s3 or --op-s3 as an S3 Express One Zone directory bucket or an Outposts access point alias, and refuses at start each it cannot resolve as one; name the bucket itself"
	Bucket string `json:"bucket,omitempty"`

	// prefix is Crewlet's store.objects.s3.prefix, prepended to every key as
	// it is, so that one bucket holds many companies. It is a template:
	// {{namespace}}, {{name}} and {{uid}} are replaced by the Company's
	// namespace, name and UID, and the result must differ between
	// companies, so it must contain {{uid}}, or {{namespace}} and {{name}}
	// joined by a '/' or '.', which neither can hold (with a '-' between
	// them, team-a/b-web and team-a-b/web would share keys); and it ends
	// with '/', so that no company's prefix starts another's. The default
	// keys files by namespace and name, which is also what the data volumes
	// are named by: a Company re-created under its old name after a Retain
	// deletion finds both its estate and its files again.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +default="{{namespace}}/{{name}}/"
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="prefix may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || ['/', '.'].exists(sep, self.contains('{{name}}' + sep + '{{namespace}}') || self.contains('{{namespace}}' + sep + '{{name}}'))",message="prefix must contain {{uid}}, or {{namespace}} and {{name}} joined by a '/' or '.', so that companies sharing the bucket do not share keys: names and namespaces hold '-', so {{namespace}}-{{name}} is team-a-b-web for both team-a/b-web and team-a-b/web"
	// +kubebuilder:validation:XValidation:rule="self.endsWith('/')",message="prefix must end with '/', so that no company's prefix is the start of another's and a listing of one company's keys never returns another's"
	Prefix string `json:"prefix,omitempty"`

	// pathStyle addresses the bucket in the URL path rather than in the host
	// name; MinIO and most self-hosted gateways need it.
	// +optional
	PathStyle *bool `json:"pathStyle,omitempty"`
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

	// proxyNamespaces are the namespaces the pods that carry the Gateways'
	// traffic run in. Under networkPolicy Isolated they are admitted to the
	// one port of the engine the route reaches: the public listener for the
	// webhooks' route, the API port for the API's, the artifact listener for
	// the artifacts'. Unset is the namespaces
	// of parentRefs, which is right for
	// implementations that run a proxy beside each Gateway (Istio, for
	// example); implementations with a shared data plane run it elsewhere
	// (Envoy Gateway in envoy-gateway-system, for example) and need it named
	// here.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ProxyNamespaces []string `json:"proxyNamespaces,omitempty"`
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

	// controllerNamespaces are the namespaces the Ingress controller's pods
	// run in. Under networkPolicy Isolated they are admitted to the one port
	// of the engine the Ingress reaches (the public listener for the
	// webhooks' Ingress, the API port for the API's, the artifact listener
	// for the artifacts'), and they are required
	// there: an Ingress does not say which pods
	// serve it, and without them the policy would refuse every request the
	// Ingress forwards.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ControllerNamespaces []string `json:"controllerNamespaces,omitempty"`
}

// WebhookExposureMode is how vendors' webhooks reach a company's engine.
// +kubebuilder:validation:Enum=Gateway;Ingress;External;None
type WebhookExposureMode string

// The webhook exposure modes.
const (
	// WebhookExposureGateway routes the public hostname to the engine's
	// public listener with an HTTPRoute.
	WebhookExposureGateway WebhookExposureMode = "Gateway"
	// WebhookExposureIngress does the same with an Ingress.
	WebhookExposureIngress WebhookExposureMode = "Ingress"
	// WebhookExposureExternal creates nothing public: something outside the
	// operator (a relay, an API gateway) forwards to the public port of the
	// engine's Service, and each Company states the address it is reached
	// on in spec.exposure.publicBaseURL.
	WebhookExposureExternal WebhookExposureMode = "External"
	// WebhookExposureNone exposes nothing; integrations that need inbound
	// delivery report that they are not exposed.
	WebhookExposureNone WebhookExposureMode = "None"
)

// WebhookExposure is how vendors' webhooks, and the calls a remote sandbox
// makes back to the engine, reach a company's engine. Whatever the mode, they
// reach the engine's public listener (Crewlet's api.public), a port of its
// own that serves only the routes outside parties call without an operator
// credential (/webhooks/, /otlp/{token}, /mcp/{token}) and answers every
// other path as one nothing serves; the admin API, the dashboard and the
// probes are on the API port, which this never reaches.
// +kubebuilder:validation:XValidation:rule="self.mode == 'Gateway' ? has(self.gateway) : !has(self.gateway)",message="gateway is required with mode Gateway and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode == 'Ingress' ? has(self.ingress) : !has(self.ingress)",message="ingress is required with mode Ingress and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode == 'External' || !has(self.external)",message="external is allowed with mode External only"
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

	// external is where the forwarder runs, for mode External.
	// +optional
	External ExternalExposure `json:"external,omitzero"`

	// hostname is the public host name of each company, as a template:
	// {{namespace}}, {{name}} and {{uid}} are replaced by the Company's
	// namespace, name and UID. The result is the company's public base URL,
	// https://<hostname>, which every vendor is told to deliver to. Prefer
	// {{uid}}: the host name is public, and a UID says nothing about the
	// tenant where a name can; and a UID is never reused, so a vendor still
	// configured for a deleted company can never reach a company created
	// later under the same name. The template must contain {{uid}}, or
	// {{name}} and {{namespace}} joined by a '.', which neither can hold, so
	// that no two companies share a host name: with a '-' between them,
	// team-a/b-web and team-a-b/web would.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="hostname may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || self.contains('{{name}}.{{namespace}}') || self.contains('{{namespace}}.{{name}}')",message="hostname must contain {{uid}}, or {{name}} and {{namespace}} joined by a '.', so that no two companies share a host name: names and namespaces hold '-', so {{namespace}}-{{name}} is team-a-b-web for both team-a/b-web and team-a-b/web"
	// +kubebuilder:validation:XValidation:rule="self.replace('{{namespace}}', 'n').replace('{{name}}', 'n').replace('{{uid}}', 'u').matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="hostname must be a lowercase DNS name once its variables are replaced"
	Hostname string `json:"hostname,omitempty"`
}

// ExternalExposure is where the forwarder of a class's webhooks runs, when
// something outside the operator forwards them (webhooks.mode External).
// +kubebuilder:validation:MinProperties=1
type ExternalExposure struct {
	// forwarders are the pods that forward webhooks to the engine's public
	// port: a tunnel's or a relay's pods in the cluster. They are required
	// under networkPolicy Isolated, which admits them to that port and to
	// nothing else of the engine, and refused under any other mode, which
	// has no policy to admit them to. A forwarder outside the pod network (a
	// load balancer of your own that sends traffic from the nodes or from
	// outside the cluster) cannot be admitted by namespace: isolate those
	// engines with your CNI's own policy and networkPolicy.mode None.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	Forwarders []NetworkPolicyPeer `json:"forwarders,omitempty"`
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

// APIExposure is how a company's engine API is reached. Every request needs
// a credential, but for the probes, the vendors' webhooks and a sandbox's
// per-run endpoints: a person's session, signed in at the dashboard, or a
// bearer token, such as the one the operator generates per company and
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

	// hostname is the API's host name, as a template with the variables and
	// rules of webhooks.hostname, and one rule more.
	//
	// It is also where a browser reaches the engine (the Company's
	// status.endpoints.dashboardBaseURL, unless the Company states its own),
	// and an origin the engine accepts a person's write from, and Crewlet
	// refuses to start on a browser's address whose host ends in a label
	// that is a number: a browser reads such a host as an IPv4 address and
	// rewrites it before it sends it. So its last label may not be one
	// ({{uid}}.123, {{uid}}.0x1f), where webhooks.hostname's may. A last
	// label a variable fills is a number only for some Companies: one whose
	// namespace is a number (4711, 0x1f) under {{name}}.{{namespace}}, or
	// whose namespace or name makes one with the template's own characters
	// beside it ({{uid}}.0x{{name}} for a Company named cafe; a Company's
	// name begins with a letter, so it is never a number alone). That
	// Company's engine is held back, with RuntimeReady=False AddressRefused,
	// rather than started to refuse. Ending the template in a domain of your
	// own, as in {{name}}.{{namespace}}.crewlet.example.com, leaves it to no
	// Company.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="hostname may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || self.contains('{{name}}.{{namespace}}') || self.contains('{{namespace}}.{{name}}')",message="hostname must contain {{uid}}, or {{name}} and {{namespace}} joined by a '.', so that no two companies share a host name: names and namespaces hold '-', so {{namespace}}-{{name}} is team-a-b-web for both team-a/b-web and team-a-b/web"
	// +kubebuilder:validation:XValidation:rule="self.replace('{{namespace}}', 'n').replace('{{name}}', 'n').replace('{{uid}}', 'u').matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="hostname must be a lowercase DNS name once its variables are replaced"
	// +kubebuilder:validation:XValidation:rule="!self.matches('(^|[.])([0-9]+|0x[0-9a-f]*)$')",message="hostname may not end in a label that is a number, such as {{uid}}.123 or {{uid}}.0x1f: a browser reads the host as an IPv4 address and rewrites it before it sends it, and Crewlet refuses to start on such an address; end it in a label that is not one, such as a domain of your own"
	Hostname string `json:"hostname,omitempty"`
}

// ArtifactExposureMode is how browsers reach a company's artifact origin.
// +kubebuilder:validation:Enum=None;Gateway;Ingress
type ArtifactExposureMode string

// The artifact exposure modes.
const (
	// ArtifactExposureNone routes nothing; the Company may state an address
	// (spec.exposure.artifactsBaseURL).
	ArtifactExposureNone ArtifactExposureMode = "None"
	// ArtifactExposureGateway routes the artifact hosts with an HTTPRoute.
	ArtifactExposureGateway ArtifactExposureMode = "Gateway"
	// ArtifactExposureIngress does the same with an Ingress.
	ArtifactExposureIngress ArtifactExposureMode = "Ingress"
)

// ArtifactOrigins is how artifacts are kept apart in a browser.
// +kubebuilder:validation:Enum=PerArtifact;Shared
type ArtifactOrigins string

// The artifact origin shapes.
const (
	// ArtifactOriginsPerArtifact gives each artifact a host under the route's
	// host name: its own origin, which needs a wildcard DNS name and certificate.
	ArtifactOriginsPerArtifact ArtifactOrigins = "PerArtifact"
	// ArtifactOriginsShared serves every artifact from one host, each with an
	// opaque origin and no browser storage.
	ArtifactOriginsShared ArtifactOrigins = "Shared"
)

// ArtifactExposure is how browsers reach the engine's artifact listener
// (Crewlet's api.artifacts), which serves artifacts and nothing else. Crewlet
// refuses it over plain http on a routable host, so a route serves https only.
// +kubebuilder:validation:XValidation:rule="self.mode == 'Gateway' ? has(self.gateway) : !has(self.gateway)",message="gateway is required with mode Gateway and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode == 'Ingress' ? has(self.ingress) : !has(self.ingress)",message="ingress is required with mode Ingress and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode in ['Gateway', 'Ingress'] ? has(self.hostname) : !has(self.hostname)",message="hostname is required with modes Gateway and Ingress and not allowed otherwise"
// +kubebuilder:validation:XValidation:rule="self.mode in ['Gateway', 'Ingress'] || !has(self.origins)",message="origins is allowed with modes Gateway and Ingress only"
// +kubebuilder:validation:XValidation:rule="self.mode != 'Ingress' || (has(self.ingress) && has(self.ingress.tls))",message="an Ingress for artifacts needs ingress.tls: Crewlet refuses to start on an artifact origin reached over plain http on a routable host, since every byte a page loads would travel in the clear"
// +kubebuilder:validation:MinProperties=1
type ArtifactExposure struct {
	// mode is how the artifact origin is reached.
	// +optional
	// +default="None"
	Mode ArtifactExposureMode `json:"mode,omitempty"`

	// gateway is the Gateways to attach to, for mode Gateway; a listener the
	// route attaches to must be HTTPS.
	// +optional
	Gateway GatewayExposure `json:"gateway,omitzero"`

	// ingress is the Ingress settings, for mode Ingress; tls is required.
	// +optional
	Ingress *IngressExposure `json:"ingress,omitempty"`

	// hostname is the artifact origin's host name, a template with the
	// variables and rules of api.hostname. Use a registrable domain of its
	// own: on the dashboard's site Crewlet serves every artifact opaque.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=200
	// +kubebuilder:validation:XValidation:rule="!self.replace('{{namespace}}', '').replace('{{name}}', '').replace('{{uid}}', '').contains('{' + '{')",message="hostname may use only the variables {{namespace}}, {{name}} and {{uid}}"
	// +kubebuilder:validation:XValidation:rule="self.contains('{{uid}}') || self.contains('{{name}}.{{namespace}}') || self.contains('{{namespace}}.{{name}}')",message="hostname must contain {{uid}}, or {{name}} and {{namespace}} joined by a '.', so that no two companies share a host name: names and namespaces hold '-', so {{namespace}}-{{name}} is team-a-b-web for both team-a/b-web and team-a-b/web"
	// +kubebuilder:validation:XValidation:rule="self.replace('{{namespace}}', 'n').replace('{{name}}', 'n').replace('{{uid}}', 'u').matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="hostname must be a lowercase DNS name once its variables are replaced"
	// +kubebuilder:validation:XValidation:rule="!self.matches('(^|[.])([0-9]+|0x[0-9a-f]*)$')",message="hostname may not end in a label that is a number, such as {{uid}}.123 or {{uid}}.0x1f: a browser reads the host as an IPv4 address and rewrites it before it sends it, and Crewlet refuses to start on such an address; end it in a label that is not one, such as a domain of your own"
	Hostname string `json:"hostname,omitempty"`

	// origins is PerArtifact (unset), where the route takes *.<hostname>, or
	// Shared, where it takes <hostname>.
	// +optional
	Origins ArtifactOrigins `json:"origins,omitempty"`
}

// ExposureSettings is how a class's companies are reached.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="!has(self.webhooks) || !has(self.api) || !has(self.webhooks.hostname) || !has(self.api.hostname) || self.webhooks.hostname != self.api.hostname",message="webhooks.hostname and api.hostname must differ: both routes take every path of their host, and the public host must reach the public listener alone"
// +kubebuilder:validation:XValidation:rule="!has(self.artifacts) || !has(self.artifacts.hostname) || ((!has(self.api) || !has(self.api.hostname) || (self.api.hostname != self.artifacts.hostname && !self.api.hostname.endsWith('.' + self.artifacts.hostname))) && (!has(self.webhooks) || !has(self.webhooks.hostname) || (self.webhooks.hostname != self.artifacts.hostname && !self.webhooks.hostname.endsWith('.' + self.artifacts.hostname))))",message="artifacts.hostname must differ from api.hostname and webhooks.hostname, and neither may be under it: an artifact must never be served where the API's session runs, and Crewlet refuses an artifact origin whose host is the API's or a parent of it"
type ExposureSettings struct {
	// webhooks is how vendors' webhooks reach the engine.
	// +optional
	// +default={"mode": "None"}
	Webhooks WebhookExposure `json:"webhooks,omitzero"`

	// api is how the engine's API is reached.
	// +optional
	// +default={"mode": "ClusterInternal"}
	API APIExposure `json:"api,omitzero"`

	// artifacts is how browsers reach the artifact origin.
	// +optional
	// +default={"mode": "None"}
	Artifacts ArtifactExposure `json:"artifacts,omitzero"`

	// trustedProxies are the address blocks of what forwards requests to
	// the engine: the Gateway's proxies or the Ingress controller of the
	// class's routes, and whatever balances in front of them. Crewlet takes
	// a request's client address from its X-Forwarded-For header only when
	// the request arrives from one of these, and otherwise uses the address
	// it arrives from, which behind a proxy is the proxy's for every
	// caller: the sign-in throttle then counts everyone's attempts at one
	// login together, so a stranger guessing at a person's login holds up
	// that person's own sign-in, and the audit trail names the proxy. Each
	// is a canonical IPv4 or IPv6 CIDR narrower than the whole address
	// space; a single host is a /32 or /128. Unset, no forwarded header is
	// believed. Crewlet's api.trusted_proxies.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=43
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c) && cidr(c).prefixLength() > 0 && string(cidr(c).masked()) == c)",message="trustedProxies must be canonical CIDRs such as 10.0.0.0/8 or fd00::/8, narrower than /0: a /0 believes the forwarded address any caller sends"
	TrustedProxies []string `json:"trustedProxies,omitempty"`
}

// NetworkPolicySettings is the NetworkPolicy the operator puts around each
// company's engine pods.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="self.mode == 'Isolated' || (!has(self.apiConsumers) && !has(self.egress))",message="apiConsumers and egress apply to mode Isolated only"
type NetworkPolicySettings struct {
	// mode is whether the operator restricts the engine's traffic.
	// +optional
	// +default="Isolated"
	Mode NetworkPolicyMode `json:"mode,omitempty"`

	// apiConsumers are the pods admitted to the engine's API port besides
	// the operator and the Gateway proxies or Ingress controller of the
	// class's API route, and to nothing else of the engine (the public
	// port, which the webhooks reach, admits only where they arrive from):
	// the platform's own components (a control plane, a dashboard) that
	// call the engines of this class's companies. Each holds an access
	// token, which the engine still requires.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	APIConsumers []NetworkPolicyPeer `json:"apiConsumers,omitempty"`

	// egress narrows where the engine may connect to. The engine runs code
	// the tenant supplies in its pod (stdio MCP servers, coding CLIs, and
	// what its agents have them do), so under Isolated its pods may reach
	// DNS, the pods of their own namespace, and every address outside the
	// cluster except the cloud metadata endpoints (169.254.169.254,
	// fd00:ec2::254), which hand out the node's credentials. What else is in
	// the cluster is unreachable only if listed in exceptCIDRs. A seat's
	// code work and a box-mode executor's run, the sandbox's setup
	// commands included, run in the Company's E2B sandbox, which this policy
	// does not reach.
	// +optional
	Egress NetworkPolicyEgress `json:"egress,omitzero"`
}

// NetworkPolicyEgress is where the engine may connect to, beyond DNS and
// its own namespace.
// +kubebuilder:validation:MinProperties=1
type NetworkPolicyEgress struct {
	// exceptCIDRs are address blocks the engine may not reach, besides the
	// cloud metadata endpoints. List the cluster's pod, Service and node
	// networks here: a NetworkPolicy has no other way to tell an address in
	// the cluster from one outside it, and without them an engine reaches
	// other tenants' pods, the Kubernetes API server and the nodes. Each is
	// a canonical IPv4 or IPv6 CIDR, narrower than the whole address space.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MaxLength=43
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c) && cidr(c).prefixLength() > 0 && string(cidr(c).masked()) == c)",message="exceptCIDRs must be canonical CIDRs such as 10.0.0.0/8 or fd00::/8, narrower than /0"
	ExceptCIDRs []string `json:"exceptCIDRs,omitempty"`

	// to are pods in the cluster the engine may reach although
	// exceptCIDRs covers them: an in-cluster model gateway, the telemetry
	// collector, an object store.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	To []NetworkPolicyPeer `json:"to,omitempty"`
}

// NetworkPolicyPeer is the pods its podSelector selects in the namespaces
// its namespaceSelector selects, as a NetworkPolicy peer that sets both.
type NetworkPolicyPeer struct {
	// namespaceSelector selects namespaces by their labels; an empty one
	// selects every namespace. Select a namespace by name with the label
	// kubernetes.io/metadata.name, which the API server sets on every
	// namespace.
	// +required
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// podSelector selects pods in those namespaces by their labels. Unset
	// selects every pod in them.
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`
}

// NetworkPolicyMode is whether the operator restricts traffic to the engine.
// +kubebuilder:validation:Enum=Isolated;None
type NetworkPolicyMode string

// The network policy modes.
const (
	// NetworkPolicyIsolated admits traffic to the engine's API port only
	// from the operator's pods, from the namespaces the Gateway proxies or
	// Ingress controller of the class's API route run in
	// (gateway.proxyNamespaces, ingress.controllerNamespaces), and from the
	// class's apiConsumers; to its public port only from where the webhooks'
	// route arrives from (the same fields of exposure.webhooks, or
	// webhooks.external.forwarders); and
	// lets the engine connect only as networkPolicy.egress says. It needs a
	// CNI that enforces NetworkPolicy, egress included.
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
	// them, such as the address of the platform's own API. Every value,
	// one from a Secret included, reaches every tenant of the class: code
	// the tenant runs in the engine's pod can read its environment. They are
	// the engine's environment, not that of the processes it starts: a Stdio
	// tool server is handed only the part Crewlet's allowlist names, such as
	// PATH and the TLS and proxy variables, a coding CLI that part and the
	// variables its Crewlet profile passes through, such as GeminiCLI's
	// GOOGLE_CLOUD_PROJECT and GOOGLE_CLOUD_LOCATION, and a sandbox run none
	// of it. So a variable a tool server reads goes in its MCPServer's env
	// (docs/guides/tool-servers.md), and any other a coding CLI reads in its
	// LLMProvider's spec.env. A Company's spec.runtime.env is merged over
	// this env by name. The same names are refused as there: those
	// starting with CREWLET_ or OTEL_, and ANTHROPIC_API_KEY and
	// OPENAI_API_KEY.
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

// PlatformLLMProvider is a model the platform offers to every Company of a
// class that sets spec.llm.usePlatformProviders: a coding CLI, as a tenant's
// own LLMProvider is, signed in with a key whose Secret lives in a namespace
// the platform controls and is pushed into the secret store of each opted-in
// company's engine.
//
// That store is the tenant's to read: the engine's API reveals any value in
// it to whoever holds the company's access token (status.apiAccessSecretRef
// of the Company), and code the tenant runs in the engine's pod can open it
// with the keyring the pod holds. So every opted-in tenant can read these
// keys. Offer keys you would give each of them: one per tenant, or the
// per-tenant virtual keys of an LLM gateway, never the platform's own.
//
// It signs its CLI in with a key and nothing else: auth mode APIKey for a CLI
// Crewlet hands a key to, or, for Hermes, Pi and OpenCode, which read their
// model provider's key from that provider's own variable, the key in env. A
// subscription's headless token or login is one account's, which every tenant
// of the class would share: it belongs on a tenant's own LLMProvider. It runs
// in mode Host, in the engine's pod that holds each seat, and keeps its
// per-seat homes on the data volume under
// /var/lib/crewlet/llm-cli-platform/<name>, apart from every tenant's
// loginName. Its rules are an LLMProvider's that apply to such a provider,
// each with the same message's start, and hold to the same engine.
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || (has(self.auth.mode) && self.auth.mode == 'APIKey') || (self.agent in ['Hermes', 'Pi', 'OpenCode'] && has(self.env) && self.env.exists(e, e.name.matches('(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)')))",message="a platform provider signs its CLI in with a key the class gives each tenant: auth mode APIKey with auth.apiKeySecretRef, for a CLI Crewlet hands a key to, or for Hermes, Pi and OpenCode their model provider's key in env, under a name Crewlet reads as a credential, one of whose parts between underscores is, in any case, KEY, KEYS, APIKEY, ACCESSKEY, SECRETKEY, TOKEN, SECRET, PASSWORD, PASSWD, CREDENTIAL, CREDENTIALS, AUTH, OAUTH or PAT (such as OPENROUTER_API_KEY). A subscription's token or login is one account's, which every tenant of the class would share: it belongs on a tenant's own LLMProvider",fieldPath=".auth"
// +kubebuilder:validation:XValidation:rule="!has(self.auth) || !has(self.auth.mode) || self.auth.mode != 'APIKey' || self.agent in ['ClaudeCode', 'Codex', 'GeminiCLI', 'QwenCode', 'CursorAgent', 'Copilot', 'Grok', 'MuseCode']",message="auth mode APIKey takes a CLI Crewlet hands a key to: ClaudeCode, Codex, GeminiCLI, QwenCode, CursorAgent, Copilot, Grok or MuseCode. Crewlet's profiles for OpenCode, KimiCode, Hermes and Pi name no key variable: give Hermes, Pi or OpenCode its model provider's key in env under mode Subscription; KimiCode reads a key only from its own files, which a platform provider does not give it"
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(e, (self.agent == 'ClaudeCode' && e.name in ['CLAUDE_CODE_OAUTH_TOKEN', 'ANTHROPIC_API_KEY']) || (self.agent == 'Codex' && e.name == 'CODEX_API_KEY') || (self.agent == 'GeminiCLI' && e.name == 'GEMINI_API_KEY') || (self.agent == 'QwenCode' && e.name == 'OPENAI_API_KEY') || (self.agent == 'CursorAgent' && e.name == 'CURSOR_API_KEY') || (self.agent == 'Copilot' && e.name == 'COPILOT_GITHUB_TOKEN') || (self.agent == 'Grok' && e.name == 'XAI_API_KEY') || (self.agent == 'MuseCode' && e.name == 'META_API_KEY'))",message="env may not set the CLI's own token or key variable (ClaudeCode's CLAUDE_CODE_OAUTH_TOKEN and ANTHROPIC_API_KEY, Codex's CODEX_API_KEY, GeminiCLI's GEMINI_API_KEY, QwenCode's OPENAI_API_KEY, CursorAgent's CURSOR_API_KEY, Copilot's COPILOT_GITHUB_TOKEN, Grok's XAI_API_KEY, MuseCode's META_API_KEY): Crewlet's auth owns it, and whether a value in env reaches the CLI would turn on the auth mode. Give the key as auth.apiKeySecretRef with auth mode APIKey",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(e, e.name in ['HOME', 'XDG_CONFIG_HOME', 'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'TMPDIR'] || (self.agent == 'ClaudeCode' && e.name in ['CLAUDE_CONFIG_DIR', 'CLAUDE_CODE_DISABLE_CLAUDE_MDS', 'CLAUDE_CODE_DISABLE_AUTO_MEMORY']) || (self.agent == 'Codex' && e.name in ['CODEX_HOME', 'CODEX_SQLITE_HOME']) || (self.agent == 'OpenCode' && e.name in ['OPENCODE_CONFIG_DIR', 'OPENCODE_CONFIG_PROJECT_DISABLE']) || (self.agent == 'CursorAgent' && e.name == 'CURSOR_CONFIG_DIR') || (self.agent == 'KimiCode' && e.name == 'KIMI_CODE_HOME') || (self.agent == 'Hermes' && e.name == 'HERMES_HOME') || (self.agent == 'Pi' && e.name == 'PI_CODING_AGENT_DIR'))",message="env may not set a variable Crewlet sets on every call of the CLI to keep it inside its seat's own home and its call's working directory: HOME, XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME, XDG_CACHE_HOME or TMPDIR for any CLI, and ClaudeCode's CLAUDE_CONFIG_DIR, CLAUDE_CODE_DISABLE_CLAUDE_MDS and CLAUDE_CODE_DISABLE_AUTO_MEMORY, Codex's CODEX_HOME and CODEX_SQLITE_HOME, OpenCode's OPENCODE_CONFIG_DIR and OPENCODE_CONFIG_PROJECT_DISABLE, CursorAgent's CURSOR_CONFIG_DIR, KimiCode's KIMI_CODE_HOME, Hermes's HERMES_HOME or Pi's PI_CODING_AGENT_DIR. A value there would replace that isolation for every seat on the provider, so Crewlet refuses the document: drop it",fieldPath=".env"
// +kubebuilder:validation:XValidation:rule="self.agent != 'OpenCode' || !has(self.model) || (self.model.indexOf('/') >= 0 && self.model.substring(0, self.model.indexOf('/')).trim().size() > 0 && self.model.substring(self.model.indexOf('/') + 1).trim().size() > 0)",message="an OpenCode model is <provider>/<model>, which OpenCode splits at its first slash: a model with no slash, or with nothing but white space before or after it, names no provider or no model, and every call the provider makes would fail inside OpenCode, which OpenCode 2 refuses as Invalid model reference. Name the provider as OpenCode spells it (opencode models --standalone lists them), such as anthropic/claude-sonnet-5 or openrouter/anthropic/claude-sonnet-5, with that provider's key in env",fieldPath=".model"
type PlatformLLMProvider struct {
	// name is the provider's name. Seats and the Company's model order refer
	// to it with kind PlatformLLMProvider. It may not be default, which
	// Crewlet would run every chainless seat of every opted-in company on,
	// whatever the company's own order says.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self != 'default'",message="a platform provider may not be named default: Crewlet runs every seat that names no chain on the provider keyed default, ahead of spec.llm.order"
	Name string `json:"name,omitempty"`

	LLMProviderSettings `json:",inline"`

	// auth is how the CLI signs in. Crewlet's `auth`.
	// +required
	Auth PlatformLLMProviderAuth `json:"auth,omitzero"`

	// env is extra environment of the CLI, as an LLMProvider's spec.env is,
	// each value read from a Secret in a namespace the platform controls:
	// the model provider's key Hermes, Pi or OpenCode signs in with, or the
	// address of the platform's gateway a CLI reads from its environment
	// (ClaudeCode's ANTHROPIC_BASE_URL). A credential-named value of an
	// OpenCode provider travels into each box of a seat's code work on it,
	// in the tenant's own E2B account. Crewlet's `env`, a credential
	// position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	Env []NamespacedSecretEnvVar `json:"env,omitempty"`
}

// PlatformLLMProviderAuth is how a platform provider signs its CLI in. It maps
// onto the `auth` of a Crewlet providers.llm entry, as an LLMProvider's
// CLIAgentAuth does, without the subscription's headless token and credential
// bundle, which are one account's.
// +kubebuilder:validation:XValidation:rule="has(self.apiKeySecretRef) == (has(self.mode) && self.mode == 'APIKey')",message="apiKeySecretRef is the key auth mode APIKey hands the CLI: it is required with mode APIKey and not allowed otherwise, as Crewlet reads a key under no other mode"
// +kubebuilder:validation:MinProperties=1
type PlatformLLMProviderAuth struct {
	// mode is how the CLI signs in: APIKey, with apiKeySecretRef, or
	// Subscription, which signs Hermes, Pi or OpenCode in with its model
	// provider's key in env. Unset is Subscription. Crewlet's `mode`.
	// +optional
	Mode CLIAgentAuthMode `json:"mode,omitempty"`

	// apiKeySecretRef is the Secret key holding the API key auth mode
	// APIKey hands the CLI, in the key variable Crewlet's profile for it
	// names. Crewlet's `api_key`, a credential position.
	// +optional
	APIKeySecretRef NamespacedSecretKeyReference `json:"apiKeySecretRef,omitzero"`
}

// EngineLimits caps what a Company of a class may declare. The operator
// refuses to apply a company that exceeds a limit and reports it on the
// Company. A control plane should refuse such a request before it writes the
// Company; this is the cluster's defence in depth.
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

// TelemetrySettings sends the engine's traces and metrics to an OpenTelemetry
// collector. The engine reads its exporter configuration from the standard
// OTEL_* environment variables, which the operator sets from these fields;
// with an endpoint set, Crewlet exports metrics as well as traces, and the
// two cannot be turned off separately (OTEL_ names are the operator's, so
// OTEL_METRICS_EXPORTER cannot be set in env). While a Company has a public
// address (status.endpoints.publicBaseURL), the operator also wires Crewlet's
// sandbox telemetry receiver there (CREWLET_SANDBOX_OTEL_RECEIVER_URL): code
// a remote sandbox runs exports to the engine's public listener with a token
// signed for its one run, and the engine forwards it to this collector with
// the headers below, which never enter the sandbox.
type TelemetrySettings struct {
	// otlpEndpoint is the collector's OTLP/HTTP endpoint
	// (OTEL_EXPORTER_OTLP_ENDPOINT).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() in ['http', 'https']",message="otlpEndpoint must be an absolute http or https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="otlpEndpoint may not hold a user name or password: a credential is named through a Secret, never written into an address"
	OTLPEndpoint string `json:"otlpEndpoint,omitempty"`

	// headersSecretRef is a Secret key holding the headers sent to the
	// collector, in the OTEL_EXPORTER_OTLP_HEADERS format
	// (key1=value1,key2=value2), usually to authenticate. The operator copies
	// the value into a Secret it owns in each Company's namespace and
	// references it from the pod; the value never appears in the pod
	// template. It is in the engine's environment, which code the tenant
	// runs in the pod can read, so it reaches every tenant of the class: use
	// a credential that may only write telemetry, scoped per tenant where
	// the collector allows it.
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
	// the engine reaches an S3 bucket when its Company names no
	// spec.objects.credentialsSecretRef. The same annotations go on every
	// company's ServiceAccount, so the identity is every tenant's: scope it
	// per tenant (a role per company, or a session policy limited to the
	// company's prefix), or give each Company keys of its own instead.
	// +optional
	// +kubebuilder:validation:MaxProperties=16
	Annotations map[string]string `json:"annotations,omitempty"`
}

// EngineService is what a class adds to the Service <company>-engine the
// operator creates in front of each Company's data pods, which the class's
// routes send to. It has no Crewlet field: the engine never sees its
// Service.
// +kubebuilder:validation:MinProperties=1
type EngineService struct {
	// annotations are added to the Service, for a load balancer controller
	// that reads a backend's settings from it. On GKE an Ingress takes its
	// health check from cloud.google.com/backend-config, naming a
	// BackendConfig in each company's namespace: point its check at port
	// 8080, path /ready, since the public port the webhooks' route sends to
	// answers 404 on every path it does not serve, / included.
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

// SecondFactor is whether a person signing in to a company's dashboard with a
// password must also prove a second factor. It maps onto Crewlet's
// api.auth.totp (Required is `required`, Optional is `optional`).
// +kubebuilder:validation:Enum=Required;Optional
type SecondFactor string

// The second-factor postures.
const (
	// SecondFactorRequired has everyone who signs in with a password also
	// give a code from an authenticator app, or one of their recovery
	// codes, and has a person without one set it up at their first sign-in,
	// before anything else. It is Crewlet's default.
	SecondFactorRequired SecondFactor = "Required"
	// SecondFactorOptional lets a person sign in with a password alone. A
	// person may still set up an authenticator app, and is then asked for
	// its code like everyone under Required; nobody is made to.
	SecondFactorOptional SecondFactor = "Optional"
)

// SignInSettings is how people prove who they are when they sign in to the
// dashboards of a class's companies.
//
// It is the class's, not the Company's, because it weakens how people prove
// who they are, which is the platform's decision: a tenant could otherwise
// lower it for every person of its company, and the people who sign in are
// often not the people who write the Company. A platform offers it the way it
// offers exposure.trustedProxies, on a class it writes for the companies that
// may run with it (a test rig, a demo tier).
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="!has(self.acceptInsecure) || !self.acceptInsecure || (has(self.secondFactor) && self.secondFactor == 'Optional')",message="acceptInsecure applies to secondFactor Optional only: it acknowledges password-only sign-in and lifts nothing else, and set alone it has the engine warn of an insecure posture at every start about nothing"
type SignInSettings struct {
	// secondFactor is whether a person who signs in with a password must
	// also give a code from an authenticator app (Required), or may sign
	// in with the password alone (Optional). Crewlet's api.auth.totp.
	//
	// Optional is refused by Crewlet on an address a browser reaches over
	// the network unless acceptInsecure acknowledges it, and the engine
	// judges that by where it is told people reach it, the Company's
	// status.endpoints.dashboardBaseURL: a host of localhost (in any case)
	// or a loopback IP, such as the http://localhost:8080 of a company
	// reached through kubectl port-forward, needs no acknowledgement, and
	// any other host does. A company of the class whose address needs one
	// it does not have keeps its engine's pods as they are, with
	// RuntimeReady=False InsecureSignInNotAcknowledged, rather than start
	// an engine that refuses to boot. The engine reads it only at start, so
	// a change to Optional or back restarts each company's data pod.
	// +optional
	// +default="Required"
	SecondFactor SecondFactor `json:"secondFactor,omitempty"`

	// acceptInsecure acknowledges that a company of the class lets people
	// sign in with a password alone on an address a browser reaches over
	// the network: it is what lets secondFactor Optional run there, and
	// applies to secondFactor Optional only. It lifts nothing else, plain
	// http on such an address included, which the engine warns of either
	// way. While it is set the engine logs a config_advisory line at WARN
	// at every start, saying the insecure sign-in settings are allowed at
	// its address, for as long as the class sets it. Crewlet's
	// api.auth.accept_insecure.
	// +optional
	AcceptInsecure *bool `json:"acceptInsecure,omitempty"`
}

// EngineClassSpec is the defaults and guard rails a platform sets once for
// every Company that names the class, the way a StorageClass does for volumes.
// The data volume's floor and the data pods' startup window follow where the
// class keeps files (see DataStorage.size and DataPodSettings.startupSeconds):
// a company's object store cannot move under them, since Crewlet refuses to
// start a node that names another store than the one it recorded, so the
// backend a volume and a window were sized for is the one it runs with.
// +kubebuilder:validation:XValidation:rule="(has(self.objects) && has(self.objects.backend) && self.objects.backend == 'S3') || !has(self.data.storage.size) || !quantity(string(self.data.storage.size)).isLessThan(quantity('12Gi'))",message="data.storage.size must be at least 12Gi with objects.backend NATS: three quarters of the volume is the engine's event stream, and a fresh node of Crewlet reserves at least 9305762474 bytes of it for its state logs and the object store's bucket"
// +kubebuilder:validation:XValidation:rule="!has(self.data.startupSeconds) || !has(self.objects) || !has(self.objects.backend) || self.objects.backend != 'S3' || self.data.startupSeconds >= 1776",message="data.startupSeconds must be at least 1776 with objects.backend S3: every boot of a data pod probes the bucket, and Crewlet's bounds on the boot then add up to 1776 seconds, which a shorter window can cut short"
// +kubebuilder:validation:XValidation:rule="self.networkPolicy.mode != 'Isolated' || ((!has(self.exposure.webhooks.ingress) || has(self.exposure.webhooks.ingress.controllerNamespaces)) && (!has(self.exposure.api.ingress) || has(self.exposure.api.ingress.controllerNamespaces)) && (!has(self.exposure.artifacts) || !has(self.exposure.artifacts.ingress) || has(self.exposure.artifacts.ingress.controllerNamespaces)))",message="networkPolicy Isolated with an Ingress needs ingress.controllerNamespaces: without them the policy refuses everything the Ingress forwards"
// +kubebuilder:validation:XValidation:rule="!has(self.networkPolicy) || !has(self.networkPolicy.mode) || self.networkPolicy.mode != 'Isolated' || !has(self.exposure) || !has(self.exposure.webhooks) || !has(self.exposure.webhooks.mode) || self.exposure.webhooks.mode != 'External' || has(self.exposure.webhooks.external)",message="networkPolicy Isolated with External webhooks needs webhooks.external.forwarders: without them the policy refuses everything forwarded to the public port"
// +kubebuilder:validation:XValidation:rule="!has(self.exposure) || !has(self.exposure.webhooks) || !has(self.exposure.webhooks.external) || (has(self.networkPolicy) && has(self.networkPolicy.mode) && self.networkPolicy.mode == 'Isolated')",message="webhooks.external.forwarders apply to networkPolicy mode Isolated only: no other mode has a policy to admit them to"
type EngineClassSpec struct {
	// allowedNamespaces selects, by their labels, the namespaces whose
	// Companies may run with this class, as a Gateway's
	// allowedRoutes.namespaces.selector does; a Company in any other
	// namespace is not accepted (Accepted=False, ClassNotAllowed). Unset, or
	// an empty selector, admits every namespace.
	//
	// Running with a class is a privilege, and any subject that may write a
	// Company may name any class: the class carries the platform's Secrets
	// it references (platform model keys, telemetry headers, env values)
	// into each of its companies' namespaces and engines, its workload
	// identity onto the engine ServiceAccount, and its scheduling onto the
	// platform's nodes. A class with any of those should name the
	// namespaces it is for. To select namespaces by name, match the label
	// kubernetes.io/metadata.name, which the API server sets on every
	// namespace.
	// +optional
	AllowedNamespaces *metav1.LabelSelector `json:"allowedNamespaces,omitempty"`

	// engine is the engine image.
	// +required
	Engine EngineSettings `json:"engine,omitzero"`

	// data sizes the pods that hold each company's estate.
	// +required
	Data DataPodSettings `json:"data,omitzero"`

	// seatPools is whether each company may run seats on stateless pods of
	// its own, and how many.
	// +optional
	// +default={"policy": "Forbidden"}
	SeatPools SeatPoolSettings `json:"seatPools,omitzero"`

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
	// +default={"webhooks": {"mode": "None"}, "api": {"mode": "ClusterInternal"}, "artifacts": {"mode": "None"}}
	Exposure ExposureSettings `json:"exposure,omitzero"`

	// networkPolicy is the NetworkPolicy the operator puts around each
	// company's engine pods.
	// +optional
	// +default={"mode": "Isolated"}
	NetworkPolicy NetworkPolicySettings `json:"networkPolicy,omitzero"`

	// podTemplate is what is added to every engine pod.
	// +optional
	PodTemplate EnginePodTemplate `json:"podTemplate,omitzero"`

	// serviceAccount is what is added to each company's engine
	// ServiceAccount.
	// +optional
	ServiceAccount EngineServiceAccount `json:"serviceAccount,omitzero"`

	// service is what is added to each company's engine Service, which the
	// class's routes send to.
	// +optional
	Service EngineService `json:"service,omitzero"`

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

	// signIn is how people prove who they are when they sign in to each
	// company's dashboard. Unset, everyone gives a second factor, as
	// Crewlet requires by default.
	// +optional
	SignIn SignInSettings `json:"signIn,omitzero"`
}

// EngineClassStatus is what the operator observed about an EngineClass.
// +kubebuilder:validation:MinProperties=1
type EngineClassStatus struct {
	// conditions has Accepted: whether the objects the class names in other
	// namespaces exist, which its schema cannot check. It is False with
	// SecretNotFound when a Secret or key the class references does not
	// exist, GatewayAPINotInstalled when it routes through Gateways on a
	// cluster without the Gateway API, and GatewayNotFound when a Gateway
	// a route names with its namespace does not exist. Companies of a class
	// that is not accepted still run, and report what the problem does to
	// them on their own conditions.
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
// and limits. A Company names one in spec.engineClassName; a platform that
// hosts many tenants usually writes one class for each service tier it
// offers them.
//
// Anyone who may write a Company may name any class; a class admits the
// namespaces its spec.allowedNamespaces selects, and no others.
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
