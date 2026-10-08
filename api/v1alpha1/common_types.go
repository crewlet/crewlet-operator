package v1alpha1

// This file holds the small types every kind shares: references to other
// objects and to Secrets, environment variables, and the condition vocabulary.
//
// References are always typed structs, never bare strings, so that a later
// field (a kind, a namespace) can be added without changing the shape of every
// object that already uses the reference.

// CompanyReference names the Company a namespaced object belongs to. The
// Company is always in the referring object's own namespace: an object can be
// part of exactly one company, and a reference across namespaces would let one
// tenant attach objects to another's company.
type CompanyReference struct {
	// name is the metadata.name of a Company in this object's namespace.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// LocalObjectReference names another object of the kind the referring field
// documents, in the referring object's own namespace.
type LocalObjectReference struct {
	// name is the metadata.name of the referenced object.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// SecretKeyReference selects one key of a Secret in the referring object's
// namespace. The operator reads the value when it renders the company and
// pushes it into the engine's encrypted secret store; the value itself never
// appears in the company document, in a pod template or in any status.
type SecretKeyReference struct {
	// name is the metadata.name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// key is the key within the Secret's data.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	Key string `json:"key,omitempty"`
}

// SecretReference names a whole Secret in the referring object's namespace.
// It is used where one credential is several values that are always issued
// together (a Slack app's bot token and signing secret, for example); the
// field that holds the reference documents the keys the Secret must carry.
type SecretReference struct {
	// name is the metadata.name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// NamespacedSecretKeyReference selects one key of a Secret in a named
// namespace. Only the cluster-scoped EngineClass uses it: a platform-owned
// credential (a managed LLM key, an object-store key) lives in a namespace the
// platform controls, never in a tenant's.
type NamespacedSecretKeyReference struct {
	// namespace is the namespace the Secret is in.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`

	// name is the metadata.name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`

	// key is the key within the Secret's data.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+$`
	Key string `json:"key,omitempty"`
}

// NamespacedSecretReference names a whole Secret in a named namespace. Like
// NamespacedSecretKeyReference it is used only by the cluster-scoped
// EngineClass; the field that holds it documents the keys the Secret must
// carry.
type NamespacedSecretReference struct {
	// namespace is the namespace the Secret is in.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`

	// name is the metadata.name of the Secret.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// SecretValueSource says where a credential's value comes from. It has one
// member today; it is a struct so that a second source can be added without
// reshaping every list that carries credentials.
type SecretValueSource struct {
	// secretKeyRef selects the Secret key that holds the value.
	// +required
	SecretKeyRef SecretKeyReference `json:"secretKeyRef,omitzero"`
}

// SecretEnvVar is a named credential delivered as an environment variable,
// such as a tool server's API token. Crewlet treats every value in the
// positions that use this type as a credential, so a literal value is not
// accepted: the value always comes from a Secret.
type SecretEnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// valueFrom is the Secret key the value is read from.
	// +required
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// SecretHeader is a named credential sent as an HTTP request header, such as
// the Authorization header of a remote tool server. The Secret key holds the
// whole header value, scheme included (for example "Bearer <token>").
type SecretHeader struct {
	// name is the header's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9!#$%&'*+.^_|~-]+$`
	Name string `json:"name,omitempty"`

	// valueFrom is the Secret key the value is read from.
	// +required
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// EnvVar is an environment variable set on the engine's pods. Unlike
// SecretEnvVar it may carry a literal value, because it is not a Crewlet
// credential position but platform wiring (a feature flag, an endpoint); a
// sensitive value is referenced from a Secret with valueFrom, which Kubernetes
// resolves when it starts the container, so the value never appears in the pod
// template.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value and valueFrom must be set"
type EnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// value is the variable's literal value. An empty string is a valid
	// value, which is why the field is a pointer.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	Value *string `json:"value,omitempty"`

	// valueFrom reads the value from a Secret key in the Company's namespace.
	// +optional
	ValueFrom SecretValueSource `json:"valueFrom,omitzero"`
}

// NamespacedSecretValueSource says where a value set by the cluster-scoped
// EngineClass comes from: a Secret in a namespace the platform controls.
type NamespacedSecretValueSource struct {
	// secretKeyRef selects the Secret key that holds the value.
	// +required
	SecretKeyRef NamespacedSecretKeyReference `json:"secretKeyRef,omitzero"`
}

// ClassEnvVar is an environment variable an EngineClass sets on the engine
// pods of every Company of the class: platform wiring such as the address of
// the platform's own API. A value from a Secret is copied by the operator into
// a Secret it owns in each Company's namespace and referenced from the pod, as
// telemetry headers are, so the value never appears in the pod template and the
// platform's Secret never has to be readable from a tenant namespace.
// +kubebuilder:validation:XValidation:rule="has(self.value) != has(self.valueFrom)",message="exactly one of value and valueFrom must be set"
type ClassEnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// value is the variable's literal value. An empty string is a valid
	// value, which is why the field is a pointer.
	// +optional
	// +kubebuilder:validation:MaxLength=4096
	Value *string `json:"value,omitempty"`

	// valueFrom reads the value from a Secret key in a namespace the
	// platform controls.
	// +optional
	ValueFrom NamespacedSecretValueSource `json:"valueFrom,omitzero"`
}

// TokenBudget caps how many LLM tokens may be spent per calendar window on the
// company's clock (spec.timeZone of the Company). Every window is optional and
// independent; an absent window is uncapped. It maps onto Crewlet's
// token_budget block.
// +kubebuilder:validation:MinProperties=1
type TokenBudget struct {
	// day is the most tokens one local day, midnight to midnight, may spend.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Day int64 `json:"day,omitempty"`

	// week is the most tokens one ISO week, starting on Monday, may spend.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Week int64 `json:"week,omitempty"`

	// month is the most tokens one calendar month may spend.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Month int64 `json:"month,omitempty"`
}

// Condition types. Every kind reports a subset; each kind's status documents
// which.
const (
	// ConditionReady summarises a Company: True when everything it asks for
	// is in effect. Generic health checks (kstatus, Argo CD, Flux) read this
	// one.
	ConditionReady = "Ready"

	// ConditionRuntimeReady reports whether a Company's engine pods exist,
	// run the requested version and pass their readiness checks.
	ConditionRuntimeReady = "RuntimeReady"

	// ConditionSecretsSynced reports whether every Secret value the company
	// references has been pushed into the engine's secret store.
	ConditionSecretsSynced = "SecretsSynced"

	// ConditionConfigApplied reports whether the engine runs the company
	// document rendered from the Company and its children.
	ConditionConfigApplied = "ConfigApplied"

	// ConditionExposed reports whether the routes the EngineClass asks for
	// exist and were accepted.
	ConditionExposed = "Exposed"

	// ConditionAccepted reports whether an object's spec is one the
	// operator can act on (for a child: whether its Company exists).
	ConditionAccepted = "Accepted"

	// ConditionResolvedRefs reports whether every object and Secret a child
	// references exists and has the keys it needs.
	ConditionResolvedRefs = "ResolvedRefs"

	// ConditionApplied reports whether a child is part of the revision the
	// engine runs.
	ConditionApplied = "Applied"

	// ConditionHealthy reports what the engine says about an integration:
	// whether its credentials work and its webhooks are registered.
	ConditionHealthy = "Healthy"
)

// Condition reasons. A reason is a CamelCase word or phrase that a program can
// switch on; the condition's message is for people.
const (
	// ReasonAvailable is Ready=True on a Company whose engine runs the
	// current revision.
	ReasonAvailable = "Available"

	// ReasonSuspended is Ready=False on a Company with spec.suspend set.
	ReasonSuspended = "Suspended"

	// ReasonPodsReady is RuntimeReady=True.
	ReasonPodsReady = "PodsReady"

	// ReasonPodsNotReady is RuntimeReady=False while pods are missing,
	// starting or failing their readiness checks.
	ReasonPodsNotReady = "PodsNotReady"

	// ReasonDowngradeRefused is RuntimeReady=False when the requested engine
	// version is older than the one the data was last written by.
	ReasonDowngradeRefused = "DowngradeRefused"

	// ReasonVersionOverrideNotAllowed is RuntimeReady=False when a Company
	// sets spec.runtime.version and its EngineClass does not allow it.
	ReasonVersionOverrideNotAllowed = "VersionOverrideNotAllowed"

	// ReasonEngineClassNotFound is Accepted=False on a Company whose class
	// does not exist, or that names none while no class is the default.
	ReasonEngineClassNotFound = "EngineClassNotFound"

	// ReasonReplicasNotAllowed is Accepted=False on a Company whose
	// spec.topology.data.replicas is not in its class's allowedReplicas.
	ReasonReplicasNotAllowed = "ReplicasNotAllowed"

	// ReasonLimitExceeded is Accepted=False on a Company whose children
	// exceed a limit of its EngineClass.
	ReasonLimitExceeded = "LimitExceeded"

	// ReasonSynced is SecretsSynced=True.
	ReasonSynced = "Synced"

	// ReasonApplied is ConfigApplied=True.
	ReasonApplied = "Applied"

	// ReasonDriftCorrected is ConfigApplied=True after the operator
	// overwrote a revision someone else made under the Enforce policy.
	ReasonDriftCorrected = "DriftCorrected"

	// ReasonDrifted is ConfigApplied=False under the Observe policy when the
	// engine runs a document other than the rendered one.
	ReasonDrifted = "Drifted"

	// ReasonSeeded is ConfigApplied=True under the Seed policy once the
	// document has been handed to the engine, and Applied=False on every
	// child afterwards: the engine owns the document from then on.
	ReasonSeeded = "Seeded"

	// ReasonInvalid is Applied=False (or ConfigApplied=False) when the
	// engine's dry run refuses the rendered document; the message carries
	// the engine's own words.
	ReasonInvalid = "Invalid"

	// ReasonBlocked is Applied=False on a child that is valid but cannot be
	// applied because another object of its company is not: a document is
	// applied whole or not at all.
	ReasonBlocked = "Blocked"

	// ReasonInRevision is Applied=True; the message names the revision.
	ReasonInRevision = "InRevision"

	// ReasonRouteAccepted is Exposed=True.
	ReasonRouteAccepted = "RouteAccepted"

	// ReasonNotExposed is Exposed=False (and Healthy=False on an integration
	// that needs inbound delivery) when the class exposes no webhooks.
	ReasonNotExposed = "NotExposed"

	// ReasonValid is Accepted=True.
	ReasonValid = "Valid"

	// ReasonCompanyNotFound is Accepted=False on a child whose
	// spec.companyRef names no Company.
	ReasonCompanyNotFound = "CompanyNotFound"

	// ReasonRefNotFound is ResolvedRefs=False when a referenced object does
	// not exist.
	ReasonRefNotFound = "RefNotFound"

	// ReasonSecretNotFound is ResolvedRefs=False (or SecretsSynced=False)
	// when a referenced Secret or Secret key does not exist.
	ReasonSecretNotFound = "SecretNotFound"

	// ReasonConflict is Accepted=False when the object collides with
	// another of its company in a way Crewlet refuses: two Integrations for
	// one vendor; two Seats with one handle, one displayName or one contact
	// account (contact.slackUserID or contact.githubLogin); two Units with
	// one displayName; or an LLMProvider whose name is also the name of one
	// of the EngineClass's platformLLMProviders the company uses, since both
	// are keys of Crewlet's one providers.llm mapping. The message names the
	// other object.
	ReasonConflict = "Conflict"

	// ReasonCycle is ResolvedRefs=False on a Unit whose parentRef chain
	// leads back to itself.
	ReasonCycle = "Cycle"

	// ReasonCheckPassed is Healthy=True.
	ReasonCheckPassed = "CheckPassed"

	// ReasonCheckFailed is Healthy=False; the message carries what the
	// engine's integration check found.
	ReasonCheckFailed = "CheckFailed"
)

// The keys a Secret must carry where a field references the whole Secret
// (a SecretReference or a NamespacedSecretReference), and the keys of the
// Secrets the operator writes. A program that writes or reads these Secrets
// uses the constants rather than repeating the strings.
const (
	// SlackBotTokenKey holds a seat's Slack bot token, in the Secret named by
	// a Seat's spec.integrations.slack.credentialsSecretRef.
	SlackBotTokenKey = "botToken"
	// SlackSigningSecretKey holds the signing secret of a seat's Slack app,
	// in the same Secret.
	SlackSigningSecretKey = "signingSecret"

	// GitHubAppPrivateKeyKey holds a seat's GitHub App private key (PEM), in
	// the Secret named by a Seat's
	// spec.integrations.github.app.credentialsSecretRef.
	GitHubAppPrivateKeyKey = "privateKey"
	// GitHubAppWebhookSecretKey holds the secret that verifies the app's own
	// webhook deliveries, in the same Secret.
	GitHubAppWebhookSecretKey = "webhookSecret"

	// S3AccessKeyIDKey holds the access key id, in the Secret named by an
	// EngineClass's spec.objects.s3.credentialsSecretRef.
	S3AccessKeyIDKey = "accessKeyID"
	// S3SecretAccessKeyKey holds the secret access key, in the same Secret.
	S3SecretAccessKeyKey = "secretAccessKey"

	// APIAccessTokenKey holds the bearer token for a company's engine API,
	// in the Secret the operator names in a Company's
	// status.apiAccessSecretRef.
	APIAccessTokenKey = "token"
)

// APIConsumerNamespaceLabel is the namespace label that, with the value
// "true", admits the namespace's pods to the engine's port under the
// EngineClass's networkPolicy Isolated: it is how a platform lets its own
// components (a control plane, a dashboard) reach the engine API of every
// company.
const APIConsumerNamespaceLabel = "crewlet.ai/api-consumer"
