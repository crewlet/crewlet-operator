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

// ProvisionedCredential is a seat's credential the engine creates: an
// Integration with provisioning Engine (GitLab, Mattermost, Datadog,
// Atlassian) makes the seat's account at the vendor, mints the account's
// credential, and seals it in the engine's own secret store. The company
// document refers to it by a name the operator derives from the seat's handle
// and this name, and nothing else ever writes that store entry: the operator
// pushes no value into it and deletes none it finds there.
type ProvisionedCredential struct {
	// name names the value among the seat's provisioned credentials. Every
	// place a seat names the same provisioned name refers to one value, so
	// a token given to a tool server's environment and to the seat's
	// sandbox is one token; different names are different values, such as
	// an account's token and its email address.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Name string `json:"name,omitempty"`
}

// SeatCredentialSource says where one of a seat's own credentials comes from:
// a Secret key, or the engine, which provisions it (see
// ProvisionedCredential).
// +kubebuilder:validation:XValidation:rule="has(self.secretKeyRef) != has(self.provisioned)",message="exactly one of secretKeyRef and provisioned must be set"
// +kubebuilder:validation:MinProperties=1
type SeatCredentialSource struct {
	// secretKeyRef selects the Secret key that holds the value.
	// +optional
	SecretKeyRef SecretKeyReference `json:"secretKeyRef,omitzero"`

	// provisioned is a value the engine creates and keeps, under an
	// Integration with provisioning Engine.
	// +optional
	Provisioned ProvisionedCredential `json:"provisioned,omitzero"`
}

// SeatEnvVar is a named credential of a seat delivered as an environment
// variable: from a Secret, or provisioned by the engine.
type SeatEnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// valueFrom is where the value comes from.
	// +required
	ValueFrom SeatCredentialSource `json:"valueFrom,omitzero"`
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
	//
	// On an agent Seat, Ready reports whether an engine node holds the
	// seat's lease and so runs it: LeaseHeld, or Unplaced while no node
	// does, as the engine's GET /fleet reports.
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

	// ConditionEngineHealthy reports whether the engine raises any alarm
	// (its /health alarm table): backup_age and trim_blocked, which say the
	// engine has no recent verified backup and so trims none of its logs,
	// are the ones an operator-run company meets first. It does not count
	// towards Ready: an engine with an alarm still runs the company, but
	// one whose logs reach their ceiling refuses ordinary writes.
	ConditionEngineHealthy = "EngineHealthy"

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

	// ConditionPaused reports whether the engine holds an agent seat
	// paused: True while it does, whoever paused it.
	ConditionPaused = "Paused"

	// ConditionHealthy reports what the engine says about an integration:
	// whether its credentials work and its webhooks are registered.
	ConditionHealthy = "Healthy"

	// ConditionSeatsPlaced reports whether every pool a Company's agent
	// seats are placed in, data included, has a ready pod to run them, and
	// whether the seat pools run what the Company and its class ask for. It
	// does not count towards Ready: a Shared pool scaled to zero parks its
	// seats on purpose.
	ConditionSeatsPlaced = "SeatsPlaced"
)

// Condition reasons. A reason is a CamelCase word or phrase that a program can
// switch on; the condition's message is for people.
const (
	// ReasonAvailable is Ready=True on a Company whose engine runs the
	// current revision.
	ReasonAvailable = "Available"

	// ReasonSuspended is Ready=False, RuntimeReady=False and
	// SeatsPlaced=False on a Company with spec.suspend set, once its pods
	// are stopped (RuntimeReady) or are being stopped (Ready, SeatsPlaced),
	// and Ready=False on each of its agent Seats.
	ReasonSuspended = "Suspended"

	// ReasonSuspending is RuntimeReady=False on a suspended Company whose
	// seat pods are still stopping. Its data pods stop once they have: a
	// stopping seat pod hands its last records to a data pod, the only kind
	// that keeps them.
	ReasonSuspending = "Suspending"

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

	// ReasonNoAlarms is EngineHealthy=True: the engine raises no alarm.
	ReasonNoAlarms = "NoAlarms"

	// ReasonAlarms is EngineHealthy=False: the engine raises alarms; the
	// message says how many and names the one standing longest.
	ReasonAlarms = "Alarms"

	// ReasonAlarmsNotReported is EngineHealthy=Unknown: the engine has not
	// evaluated its alarms yet, or could not be asked.
	ReasonAlarmsNotReported = "AlarmsNotReported"

	// ReasonAPITokensInvalid is RuntimeReady=False when the company's API
	// tokens Secret, as another controller supplies it, lacks a token or
	// holds one shorter than 43 characters (32 random bytes in base64url)
	// or with characters outside base64, base64url and hex. The operator
	// never rewrites the Secret; the message says what to repair.
	ReasonAPITokensInvalid = "APITokensInvalid"

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

	// ReasonClassNotAllowed is Accepted=False on a Company in a namespace
	// its EngineClass's spec.allowedNamespaces does not select.
	ReasonClassNotAllowed = "ClassNotAllowed"

	// ReasonLimitExceeded is Accepted=False on a Company whose children
	// exceed a limit of its EngineClass.
	ReasonLimitExceeded = "LimitExceeded"

	// ReasonSynced is SecretsSynced=True.
	ReasonSynced = "Synced"

	// ReasonApplied is ConfigApplied=True: the engine reports the epoch of
	// the revision the operator activated applied.
	ReasonApplied = "Applied"

	// ReasonApplying is ConfigApplied=False after the operator activated a
	// revision and before the engine reports it applied: the engine applies
	// a revision asynchronously, and runs the previous one meanwhile. It is
	// also Applied=False on each child whose current generation is in that
	// revision and not in the one the engine ran before.
	ReasonApplying = "Applying"

	// ReasonApplyFailed is ConfigApplied=False when the engine did not apply
	// the revision it was given: it recorded the attempt as failed, or did
	// not report the revision's epoch applied within a minute and twelve
	// seconds. The engine keeps running the revision it applied last; the
	// message carries its status, posture and own account of the failure,
	// and an ApplyFailed event is recorded. It is also Applied=False on
	// each child whose current generation is not in the revision the
	// engine runs.
	ReasonApplyFailed = "ApplyFailed"

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
	// carries what failed. It is also Ready=Unknown on an agent Seat when the
	// engine's fleet could not be read: whether a node holds the seat is not
	// known then.
	ReasonEngineUnavailable = "EngineUnavailable"

	// ReasonWriteRefused is ConfigApplied=False when the engine refuses the
	// operator's token a change to the company document: the engine's Tier A
	// (api.auth.company_writers) names other tokens as the document's
	// writers. The operator renders that list itself, naming its own token
	// under Enforce, so the engine runs a crewlet.yaml the operator did not
	// write. The message names the writers the engine reported.
	ReasonWriteRefused = "WriteRefused"

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

	// ReasonGatewayAPINotInstalled is Exposed=False, and Accepted=False on
	// the EngineClass, when the class routes through Gateways and the
	// cluster has no Gateway API (no HTTPRoute kind). The operator checks
	// once, when it starts: restart it after installing the Gateway API.
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
	// delivery when the company has no public address
	// (status.endpoints.publicBaseURL), and ResolvedRefs=False on an
	// LLMProvider of type CLIAgent in mode Agent then: its runs happen in a
	// remote sandbox, which calls the seat's tools back through the engine's
	// public address.
	ReasonNotExposed = "NotExposed"

	// ReasonNoProvisioner is ResolvedRefs=False on a Seat that names a
	// provisioned credential while no Integration of its Company has
	// provisioning Engine: nothing would create the value, and the seat's
	// tool or chat app would start without it.
	ReasonNoProvisioner = "NoProvisioner"

	// ReasonValid is Accepted=True.
	ReasonValid = "Valid"

	// ReasonGatewayNotFound is Accepted=False on an EngineClass whose
	// Gateway route names a Gateway, with its namespace, that does not
	// exist.
	ReasonGatewayNotFound = "GatewayNotFound"

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
	// a podTemplate.spec.env valueFrom) or the Company's
	// spec.objects.credentialsSecretRef. On an EngineClass it is
	// Accepted=False when a Secret or key the class references does not
	// exist.
	ReasonSecretNotFound = "SecretNotFound"

	// ReasonConflict is Accepted=False when the object collides with
	// another of its company in a way Crewlet refuses: two Integrations for
	// one vendor (Jira, Confluence and Atlassian are three vendors); two Seats with one handle, one displayName or one contact
	// account (contact.slackUserID or contact.githubLogin); two Units with
	// one displayName; or an LLMProvider whose name is also the name of one
	// of the EngineClass's platformLLMProviders the company uses, since both
	// are keys of Crewlet's one providers.llm mapping. The message names the
	// other object.
	ReasonConflict = "Conflict"

	// ReasonReferenceNotAllowed is ResolvedRefs=False on an object (or
	// ConfigApplied=False on a Company, for its own fields and engineConfig)
	// that holds a ${NAME} reference in a field the operator renders into the
	// company document. Crewlet resolves one from the engine's own secret
	// store and environment in many fields that hold no credential (an MCP
	// server's url, a sandbox setup command), so it would name values that
	// are not the object's. A credential is given with a Secret reference.
	ReasonReferenceNotAllowed = "ReferenceNotAllowed"

	// ReasonCycle is ResolvedRefs=False on a Unit whose parentRef chain
	// leads back to itself.
	ReasonCycle = "Cycle"

	// ReasonHumanSeat is ResolvedRefs=False on a Schedule whose
	// target.seatRef names a Human seat: the engine runs no turn for a
	// person, so the schedule could never fire. Target an Agent seat, or the
	// person's unit with fanOut Each. It is also ResolvedRefs=False on an
	// Integration whose Datadog routeTo.seatRef or GitLab accessLevels entry
	// names a Human seat, for whom the engine runs no turn and creates no
	// account.
	ReasonHumanSeat = "HumanSeat"

	// ReasonSeatPaused is Paused=True: the engine holds the seat paused, as
	// spec.paused asks or, with spec.paused unset, because a person paused
	// it. The message and status.pause say who paused it, when and why.
	ReasonSeatPaused = "SeatPaused"

	// ReasonSeatRunning is Paused=False: the engine runs the seat, as
	// spec.paused asks or, with it unset, because nobody has it paused.
	ReasonSeatRunning = "SeatRunning"

	// ReasonPauseNotManaged is Paused=True or False when the seat's pause
	// differs from spec.paused and the Company's management policy is
	// Observe or Seed, under which the operator changes no pause; the
	// condition's status is the engine's.
	ReasonPauseNotManaged = "PauseNotManaged"

	// ReasonPauseUnknown is Paused=Unknown after the engine answered that it
	// may or may not have recorded the operator's pause or resume. The
	// operator asks again shortly; a second call changes nothing if the
	// first was recorded.
	ReasonPauseUnknown = "PauseUnknown"

	// ReasonPauseRefused is Paused=Unknown when the engine refused the
	// operator's pause or resume; the message is the engine's.
	ReasonPauseRefused = "PauseRefused"

	// ReasonCheckPassed is Healthy=True.
	ReasonCheckPassed = "CheckPassed"

	// ReasonCheckFailed is Healthy=False; the message carries what the
	// engine's integration check found.
	ReasonCheckFailed = "CheckFailed"

	// ReasonAllPoolsServing is SeatsPlaced=True: every pool an agent seat
	// is placed in has a ready pod.
	ReasonAllPoolsServing = "AllPoolsServing"

	// ReasonPoolEmpty is SeatsPlaced=False while a pool an agent seat is
	// placed in has no ready pod: a Shared pool at zero replicas, or a
	// PerSeat seat's pods down. Its seats run nowhere meanwhile; their
	// messages wait. The message names the pools.
	ReasonPoolEmpty = "PoolEmpty"

	// ReasonRolloutBlocked is SeatsPlaced=False while the seat pools keep
	// their previous pod template because the data pods have not finished
	// rolling to the current one: seat pods follow the data pods, never
	// lead them.
	ReasonRolloutBlocked = "RolloutBlocked"

	// ReasonUnplaced is Ready=False on an agent Seat that no engine node
	// holds: its pool has no pod running the engine, the seat is not in the
	// revision the engine runs, or the engine has not placed it yet. Its
	// messages wait for a node.
	ReasonUnplaced = "Unplaced"

	// ReasonLeaseHeld is Ready=True on an agent Seat whose lease an engine
	// node holds, which therefore runs it; status.placement.node names the
	// node, a pod's name.
	ReasonLeaseHeld = "LeaseHeld"

	// ReasonPoolNotFound is ResolvedRefs=False on a Seat whose
	// spec.placement.pool is neither data nor the name of one of its
	// Company's spec.topology.seatPools.
	ReasonPoolNotFound = "PoolNotFound"

	// ReasonSeatPoolsNotAllowed is ConfigApplied=False and
	// SeatsPlaced=False on a Company that declares spec.topology.seatPools
	// while its EngineClass's spec.seatPools.policy is not Allowed: the
	// class's data pods open no listener for seat pods to join through, so
	// the operator runs none.
	ReasonSeatPoolsNotAllowed = "SeatPoolsNotAllowed"

	// ReasonPerSeatNotAllowed is Applied=False on a Seat placed in a
	// PerSeat pool while its EngineClass does not set
	// spec.seatPools.allowPerSeat.
	ReasonPerSeatNotAllowed = "PerSeatNotAllowed"

	// ReasonPoolCapacityExceeded is Applied=False on each Seat placed in a
	// PerSeat pool that the company's seat pods cannot admit within its
	// EngineClass's spec.seatPools.maxPods (the seats already running in
	// pods of their own are admitted first, then the older ones), and
	// ConfigApplied=False and SeatsPlaced=False on a Company whose Shared
	// pools alone run more: the operator then keeps the pools' pods as
	// they are and starts no other.
	ReasonPoolCapacityExceeded = "PoolCapacityExceeded"

	// ReasonAgentModeBridgeUnsupported is Applied=False on a Seat placed in
	// a seat pool whose executor runs on a CLIAgent provider in mode Agent:
	// one at the head of its executor's chain, the only entry Crewlet runs
	// as an agentic run (a fallback behind it is a text call).
	// An agent-mode run calls the seat's tools back through the tool bridge
	// of the engine node that holds the seat, at the address that node was
	// given (CREWLET_MCP_BRIDGE_URL). The engine would serve the bridge on a
	// seat pod's own listener, but the operator publishes no address of a
	// seat pod a remote sandbox could dial (the company's public address
	// reaches the data pods only), so it gives a seat pod none, and the
	// engine refuses agent mode on a node with no bridge URL.
	ReasonAgentModeBridgeUnsupported = "AgentModeBridgeUnsupported"

	// ReasonCLIAuthNotPortable is Applied=False on a Seat placed in a seat
	// pool that runs on a CLIAgent provider signed in with a credential
	// bundle. A seat pod keeps the CLI's login on its scratch volume, so the
	// bundle is restored at every start, and every pod refreshes the one
	// login on its own: a vendor that rotates refresh tokens then logs the
	// whole company out.
	ReasonCLIAuthNotPortable = "CLIAuthNotPortable"

	// ReasonCodingAgentMismatch is Applied=False on a Seat whose code work
	// (spec.sandbox) runs on a CLIAgent provider driving another CLI than
	// the seat's coding agent (spec.sandbox.codingAgent, else the Company's
	// spec.sandbox.defaultCodingAgent, else ClaudeCode): the head of
	// spec.llm.sandbox, else of spec.llm.default, else of the Company's
	// model order. The provider's model is written in its own CLI's grammar
	// and its sign-in is that CLI's, so the coding agent can read neither,
	// and Crewlet refuses the pairing on every write.
	ReasonCodingAgentMismatch = "CodingAgentMismatch"

	// ReasonSandboxModelUnsupported is Applied=False on a Seat whose code
	// work runs the ClaudeCode coding agent on a provider of type OpenAI or
	// OpenAICompatible, an LLMProvider or a platform provider, while the
	// seat's sandbox environment (spec.sandbox.env, its setup steps' and
	// the Company's spec.sandbox.setup steps') names no variable that signs
	// ClaudeCode in. ClaudeCode speaks the Anthropic API only, and Crewlet
	// refuses every run_sandbox launch of such a seat.
	ReasonSandboxModelUnsupported = "SandboxModelUnsupported"

	// ReasonSandboxLoginNotPortable is Applied=False on a Seat whose code
	// work runs on a CLIAgent provider of its coding agent's CLI whose login
	// cannot follow a run into the Company's remote sandbox: a ClaudeCode
	// signed in by a credential bundle with neither CLAUDE_CODE_OAUTH_TOKEN
	// nor ANTHROPIC_API_KEY in the seat's sandbox environment, or an
	// OpenCode with no credential-named variable in its cli.env or the
	// seat's sandbox environment. A bundle stays on the engine's data
	// volume, and Crewlet refuses every run_sandbox launch of such a seat.
	ReasonSandboxLoginNotPortable = "SandboxLoginNotPortable"

	// ReasonPlacementPending is Applied=False on an agent Seat placed in a
	// pool other than the one the revision the engine runs pins it to,
	// while that pool's StatefulSet has no ready pod: the operator keeps the
	// seat pinned where it runs, so that it is never pinned to pods that are
	// not there, and moves the pin with the first revision after one is.
	ReasonPlacementPending = "PlacementPending"
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
