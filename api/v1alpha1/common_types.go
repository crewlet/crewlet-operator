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
// value (a managed LLM key, telemetry headers) lives in a namespace the
// platform controls rather than in each tenant's. The value itself still
// reaches every tenant that runs with it (see the field that references
// it); the namespace keeps tenants from the platform's Secret, not from the
// value.
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
// platform's Secret never has to be readable from a tenant namespace. The
// value is in every tenant's engine pod all the same, where code the tenant
// runs can read it: put nothing here a tenant must not hold.
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
	// ConditionReady summarises a Company: True when it is Accepted, its
	// engine runs (RuntimeReady), its routes are in place (Exposed) and the
	// engine runs its document (ConfigApplied). When it is False its reason
	// is the reason of the first of those that is not True, in that order,
	// except that a suspended Company reports Suspended. Generic health
	// checks (kstatus, Argo CD, Flux) read this one.
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

	// ReasonSuspended is Ready=False and RuntimeReady=False on a Company
	// with spec.suspend set.
	ReasonSuspended = "Suspended"

	// ReasonConfigPending is Ready=False on a Company whose engine runs
	// and is exposed while ConfigApplied has not been reported for the
	// Company's current generation yet.
	ReasonConfigPending = "ConfigPending"

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

	// ReasonKeyringInvalid is RuntimeReady=False when the company's keyring
	// Secret cannot be read as a keyring: it holds no key, or its active key
	// is not one of them. The operator never rewrites a keyring, because
	// every value the engine has sealed depends on it; the message says
	// what to repair.
	ReasonKeyringInvalid = "KeyringInvalid"

	// ReasonKeyringMissing is RuntimeReady=False when the company's keyring
	// Secret does not exist while its data StatefulSet or data volume claims
	// do. A new keyring could not open the secrets sealed on the volumes, so
	// the operator generates none and holds the pods back until the Secret
	// is restored from a backup, or the claims are deleted to discard the
	// data.
	ReasonKeyringMissing = "KeyringMissing"

	// ReasonPreviousCompanyObjects is RuntimeReady=False while the keyring
	// or API tokens Secret of an earlier Company of the same name, deleted
	// with deletionPolicy Delete, still exists: the garbage collector is
	// about to delete it, and the pods wait until it has rather than start
	// on keys that are going away.
	ReasonPreviousCompanyObjects = "PreviousCompanyObjects"

	// ReasonEngineClassNotFound is Accepted=False on a Company whose class
	// does not exist, or that names none while no class, or more than one,
	// is the default.
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
	// engine runs a document other than the rendered one, and Applied=False
	// on each child whose part of the document differs; the messages name
	// the places that differ.
	ReasonDrifted = "Drifted"

	// ReasonInSync is ConfigApplied=True under the Observe policy when the
	// engine runs the rendered document, although the operator did not write
	// it.
	ReasonInSync = "InSync"

	// ReasonSeeded is ConfigApplied=True under the Seed policy once the
	// engine has a document, whether the operator wrote it or found one
	// there: the engine owns the document from then on.
	ReasonSeeded = "Seeded"

	// ReasonNotManaged is Applied=False on every child of a Company whose
	// document the engine owns (the Seed policy, once the engine has a
	// document), and SecretsSynced=Unknown on a Company whose policy keeps
	// the operator out of the engine's secret store (Observe, and Seed once
	// seeded): the operator does not write what the condition is about.
	ReasonNotManaged = "NotManaged"

	// ReasonEngineUnavailable is ConfigApplied=False while the operator
	// cannot reach the company's engine or its token: the data pod has not
	// started, does not answer, or refuses the operator's token. The message
	// carries what failed.
	ReasonEngineUnavailable = "EngineUnavailable"

	// ReasonInvalid is Applied=False (or ConfigApplied=False) when the
	// engine's dry run refuses the rendered document; the message carries
	// the engine's own words.
	ReasonInvalid = "Invalid"

	// ReasonBlocked is Applied=False on a child that is valid but cannot be
	// applied because another object of its company is not, or because a
	// Secret the company references is missing: a document is applied whole
	// or not at all. A child whose current generation is already in the
	// revision the engine runs keeps Applied=True meanwhile.
	ReasonBlocked = "Blocked"

	// ReasonInRevision is Applied=True; the message names the revision, and
	// the condition's observedGeneration is the child's generation that
	// revision was rendered from.
	ReasonInRevision = "InRevision"

	// ReasonRouteAccepted is Exposed=True when every route the class asks
	// for exists and was accepted by its Gateway or Ingress controller.
	ReasonRouteAccepted = "RouteAccepted"

	// ReasonNoRoutes is Exposed=True when the class asks for no route at
	// all: webhooks None or External, and the API ClusterInternal. There is
	// nothing to wait for, so it does not hold Ready back.
	ReasonNoRoutes = "NoRoutes"

	// ReasonRouteNotAccepted is Exposed=False while a route the class asks
	// for has not been accepted: its Gateway has not attached it, or its
	// Ingress controller has not published an address for it. The message
	// carries what the controller reported.
	ReasonRouteNotAccepted = "RouteNotAccepted"

	// ReasonGatewayAPINotInstalled is Exposed=False when the class routes
	// through Gateways and the cluster has no Gateway API (no HTTPRoute
	// kind). The operator checks once, when it starts: restart it after
	// installing the Gateway API.
	ReasonGatewayAPINotInstalled = "GatewayAPINotInstalled"

	// ReasonNameConflict is RuntimeReady=False (or Exposed=False, for a
	// route) when an object the operator would write for a Company exists
	// and is not the Company's: a Service, Secret, ConfigMap, ServiceAccount,
	// StatefulSet, NetworkPolicy, HTTPRoute or Ingress of the derived name
	// that someone else created. The operator neither changes nor adopts it;
	// the message names it. Rename or delete it, or give the Company another
	// name. It is also SecretsSynced=False and ConfigApplied=False when the
	// Secret <company>-secret-sync, where the operator keeps what it pushed
	// into the engine's secret store, is someone else's.
	ReasonNameConflict = "NameConflict"

	// ReasonPublicBaseURLMissing is Exposed=False when the class's
	// webhooks.mode is External and the Company does not state its address
	// in spec.exposure.publicBaseURL.
	ReasonPublicBaseURLMissing = "PublicBaseURLMissing"

	// ReasonNotExposed is Healthy=False on an integration that needs inbound
	// delivery when the class's webhooks.mode is None.
	ReasonNotExposed = "NotExposed"

	// ReasonValid is Accepted=True.
	ReasonValid = "Valid"

	// ReasonResolved is ResolvedRefs=True.
	ReasonResolved = "Resolved"

	// ReasonCompanyNotFound is Accepted=False on a child whose
	// spec.companyRef names no Company.
	ReasonCompanyNotFound = "CompanyNotFound"

	// ReasonCompanyNotAccepted is Accepted=False on a child whose Company
	// exists and is not accepted (its class is gone, or it is over its
	// class's limits): nothing of the company is applied meanwhile. The
	// message carries the Company's own Accepted reason and message.
	ReasonCompanyNotAccepted = "CompanyNotAccepted"

	// ReasonRefNotFound is ResolvedRefs=False when a referenced object does
	// not exist.
	ReasonRefNotFound = "RefNotFound"

	// ReasonSecretTooLarge is ResolvedRefs=False (and SecretsSynced=False)
	// when a referenced Secret key holds more than the 64 KiB the engine's
	// secret store accepts.
	ReasonSecretTooLarge = "SecretTooLarge"

	// ReasonSecretNotFound is ResolvedRefs=False (or SecretsSynced=False)
	// when a referenced Secret or Secret key does not exist, and
	// RuntimeReady=False when a Secret the engine's pods are given values
	// from does not: one the EngineClass references (telemetry.headersSecretRef,
	// a podTemplate.spec.env valueFrom), the Company's
	// spec.objects.credentialsSecretRef, or the company's API tokens Secret
	// when it lacks a token.
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

	// S3AccessKeyIDKey holds the access key id, in the Secret named by a
	// Company's spec.objects.credentialsSecretRef.
	S3AccessKeyIDKey = "accessKeyID"
	// S3SecretAccessKeyKey holds the secret access key, in the same Secret.
	S3SecretAccessKeyKey = "secretAccessKey"

	// APIAccessTokenKey holds the bearer token for a company's engine API,
	// in the Secret the operator names in a Company's
	// status.apiAccessSecretRef.
	APIAccessTokenKey = "token"
	// APIAccessURLKey holds the in-cluster base URL of the company's engine
	// API, the same as its status.endpoints.api, in the same Secret, so a
	// consumer that mounts the Secret needs nothing else.
	APIAccessURLKey = "url"
)

// CompanyLabel is the label, with the Company's name as its value, on every
// object the operator creates for a Company. Several companies may share a
// namespace, so selectors (the Services', the NetworkPolicy's) match on it
// rather than on the namespace.
const CompanyLabel = "crewlet.ai/company"

// CompanyFinalizer is the finalizer the operator puts on every Company. It
// holds the Company's deletion until the engine has drained and stopped and
// the data volumes have been kept or deleted as spec.deletionPolicy says.
const CompanyFinalizer = "crewlet.ai/company"

// APIConsumerNamespaceLabel is the namespace label that, with the value
// "true", admits the namespace's pods to the engine's port under the
// EngineClass's networkPolicy Isolated: it is how a platform lets its own
// components (a control plane, a dashboard) reach the engine API of every
// company.
const APIConsumerNamespaceLabel = "crewlet.ai/api-consumer"
