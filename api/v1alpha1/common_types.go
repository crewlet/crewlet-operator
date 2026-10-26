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
// such as a sandbox setup step's token or a coding CLI's model key. Crewlet
// treats every value in the positions that use this type as a credential, so
// a literal value is not accepted: the value always comes from a Secret.
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

// NamespacedSecretEnvVar is a named credential an EngineClass gives, delivered
// as an environment variable whose value is read from a Secret in a namespace
// the platform controls, such as a platform model provider's key. Like
// SecretEnvVar it takes no literal value, and like NamespacedSecretKeyReference
// it is used only by the cluster-scoped EngineClass.
type NamespacedSecretEnvVar struct {
	// name is the variable's name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	Name string `json:"name,omitempty"`

	// valueFrom is the Secret key the value is read from.
	// +required
	ValueFrom NamespacedSecretValueSource `json:"valueFrom,omitzero"`
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
	// On an agent Seat, Ready reports whether the seat runs: LeaseHeld while
	// an engine node holds its lease, as the engine's GET /fleet reports,
	// and Unplaced while none does, or PoolNotFound when none can, as the
	// Company does not declare the seat's pool. A seat whose executor's chain is headed
	// by a CLI in mode Box is not Ready either while the Company has no public
	// address for its tool bridge, whoever holds it (NotExposed): the engine
	// applies it and refuses every one of its turns.
	// CodeWorkRunnable does not count towards it: a seat whose code work
	// cannot run still runs its turns, and a health check gating on Ready
	// would otherwise hold a rollout over a seat that works.
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

	// ConditionIdentityClaimed reports whether anybody has joined the
	// company: whether the engine's identity directory holds a person, who
	// signs in to its dashboard as themselves on a Human seat. A company
	// starts with nobody, and its first person is invited or added with the
	// access token the operator publishes. It does not count towards Ready:
	// a company nobody has joined runs its agent seats all the same.
	ConditionIdentityClaimed = "IdentityClaimed"

	// ConditionExposed reports whether the routes the EngineClass asks for
	// exist and were accepted.
	ConditionExposed = "Exposed"

	// ConditionAccepted reports whether an object's spec is one the
	// operator can act on (for a child: whether its Company exists).
	ConditionAccepted = "Accepted"

	// ConditionResolvedRefs reports whether every object and Secret a child
	// references exists and has the keys it needs, and is what it must be
	// (an Agent seat where the engine runs a turn, an Integration that
	// provisions a provisioned credential). False holds the company's
	// document, and the part reports it on Applied too, except for the
	// findings the operator applies and reports only, because only the part
	// at fault suffers them: a Seat's PoolNotFound, NoProvisioner and
	// NotAnAccountID, a Human seat named in a GitLab Integration's
	// accessLevels (HumanSeat), and a value a tool server or a coding run is
	// started with that a Secret holds empty (SecretValueEmpty). Applied says
	// which. True names, in its message, every value the child gives a tool
	// server or a coding run under a key an optionalKeys list lets go
	// without it whose Secret or key does not exist, or holds nothing.
	ConditionResolvedRefs = "ResolvedRefs"

	// ConditionApplied reports whether a child is part of the revision the
	// engine runs.
	ConditionApplied = "Applied"

	// ConditionPaused reports whether the engine holds an agent seat
	// paused: True while it does, whoever paused it.
	ConditionPaused = "Paused"

	// ConditionCodeWorkRunnable reports whether an agent Seat's coding agent
	// can run on the provider at the head of its sandbox chain, or on the
	// sign-in the seat's sandbox environment gives it, with every value its
	// runs are given, so that a run_sandbox launch of the seat can start. It
	// is set on an Agent seat with spec.sandbox whose executor does not run in
	// box mode, in a Company with spec.sandbox (the seats Crewlet offers
	// run_sandbox), and is computed from the spec, and from the Secrets once
	// they are read, under every management policy: under Observe or Seed it
	// judges the Seat's objects, which may not be the document the engine
	// runs (Applied says whether they are). Only CodingAgentMismatch also
	// holds the document (Applied=False), as Crewlet refuses it on every
	// write; with SandboxLoginNotPortable, SecretValueEmpty or NoProvisioner
	// the seat is applied and runs its turns, and Crewlet refuses each of its
	// coding runs.
	ConditionCodeWorkRunnable = "CodeWorkRunnable"

	// ConditionBoxModeRunnable reports whether an LLMProvider in mode
	// Box can run a seat's executor in the sandbox at all: its runs
	// happen in the Company's remote sandbox and call the seat's tools back
	// through the engine's public address (status.endpoints.publicBaseURL),
	// so it is False (NotExposed) while the Company has none. It is set on
	// such a provider only, and does not hold the document: the engine
	// applies the provider, which still runs in the engine's pod behind a
	// chain's head or in another phase, and refuses each turn of a seat whose
	// executor's chain it heads (the Seat's Ready says which seats).
	ConditionBoxModeRunnable = "BoxModeRunnable"

	// ConditionHealthy reports what the engine says about an integration:
	// whether its credentials work and its webhooks are registered.
	ConditionHealthy = "Healthy"

	// ConditionSeatsPlaced reports whether every pool a Company's agent
	// seats are placed in, data included, exists and has a ready pod to run
	// them, and whether the seat pools run what the Company and its class ask
	// for. It does not count towards Ready: a Shared pool scaled to zero
	// parks its seats on purpose.
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

	// ReasonInsecureSignInNotAcknowledged is RuntimeReady=False when the
	// Company's EngineClass lets people sign in with a password alone
	// (spec.signIn.secondFactor Optional) and the engine is told people
	// reach it at an address that is not on loopback, without the class's
	// acknowledgement (spec.signIn.acceptInsecure): Crewlet refuses to start
	// on that, so the operator holds the pods back as they are rather than
	// start an engine that cannot boot. The message names the address and
	// the field.
	ReasonInsecureSignInNotAcknowledged = "InsecureSignInNotAcknowledged"

	// ReasonAddressRefused is RuntimeReady=False when an address the
	// operator would tell the engine it is reached at (api.external_url,
	// api.public.external_url, api.artifacts.external_url or an
	// api.auth.allowed_origins entry of the data pod's crewlet.yaml) is one
	// Crewlet refuses to start on. The API refuses such an address where a
	// Company or its class states it, so what reaches this is in practice an
	// artifact origin that meets the dashboard's, or one the operator derives: the host of
	// the class's API route, expanded for a Company whose namespace makes
	// its last label a number (4711, 0x1f), or whose namespace or name does
	// with the template's own characters beside it (0x{{name}} for a Company
	// named cafe), which a browser reads as an IPv4 address. The operator
	// holds the pods back as they are rather than start an engine that
	// cannot boot; the message names the address, where it comes from and
	// what to change.
	ReasonAddressRefused = "AddressRefused"

	// ReasonNoAlarms is EngineHealthy=True: the engine raises no alarm.
	ReasonNoAlarms = "NoAlarms"

	// ReasonAlarms is EngineHealthy=False: the engine raises alarms; the
	// message says how many and names the one standing longest.
	ReasonAlarms = "Alarms"

	// ReasonAlarmsNotReported is EngineHealthy=Unknown: the engine has not
	// evaluated its alarms yet, or could not be asked.
	ReasonAlarmsNotReported = "AlarmsNotReported"

	// ReasonPersonEnrolled is IdentityClaimed=True: a person is in the
	// company's identity directory. They may not have chosen a password yet.
	ReasonPersonEnrolled = "PersonEnrolled"

	// ReasonUnclaimed is IdentityClaimed=False: nobody has joined the company
	// yet. The message names its Human seats and how to bring the first
	// person in.
	ReasonUnclaimed = "Unclaimed"

	// ReasonNoHumanSeat is IdentityClaimed=False: nobody has joined the
	// company, and nobody can, because the company the engine runs has no
	// Human seat to put them on.
	ReasonNoHumanSeat = "NoHumanSeat"

	// ReasonIdentityNotReported is IdentityClaimed=Unknown: the engine's
	// health could not be read, the engine could not read its identity
	// directory, or it did not say.
	ReasonIdentityNotReported = "IdentityNotReported"

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
	// started, does not answer, asks to be asked again later, or does not
	// know the operator's token. The message carries what failed. It is
	// also Ready=Unknown on an agent Seat when the engine's fleet could not
	// be read: whether a node holds the seat is not known then.
	ReasonEngineUnavailable = "EngineUnavailable"

	// ReasonWriteRefused is ConfigApplied=False when the engine refuses the
	// operator's token a change to the company document: the engine's Tier A
	// (api.auth.company_writers) names other tokens as the document's
	// writers. The operator renders that list itself, naming its own token
	// under Enforce, so the engine runs a crewlet.yaml the operator did not
	// write. The message names the writers the engine reported.
	ReasonWriteRefused = "WriteRefused"

	// ReasonNotGranted is ConfigApplied=False when the engine knows the
	// operator's token and will not let it make a request the operator
	// needs: the token holds none of the grants the request needs, or the
	// engine's identity directory binds it to a seat the company no longer
	// has. The operator renders its token with admin, which every request it
	// makes needs, so the first is an engine running a crewlet.yaml the
	// operator did not write; the second is undone by an admin of the
	// company. The message names the grants the engine asked for, or the
	// binding.
	ReasonNotGranted = "NotGranted"

	// ReasonSeatHeld is ConfigApplied=False when the engine refuses the
	// document because it takes a human seat out of the company, or makes
	// it an agent's, while a person, a service account or an open
	// invitation in the engine's identity directory holds it. The message
	// names each such seat and what holds it, never who; an admin of the
	// company frees the seat first. It is also Applied=False on a Seat that
	// still exists and is held: one whose kind turned from Human to Agent.
	ReasonSeatHeld = "SeatHeld"

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
	// all: webhooks None or External, the API ClusterInternal, and the
	// artifacts None. There is
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

	// ReasonRouteNotHTTPS is Exposed=False when the artifacts route's Gateways
	// have no HTTPS listener for it: the data pod opens no artifact origin.
	ReasonRouteNotHTTPS = "RouteNotHTTPS"

	// ReasonPublicBaseURLMissing is Exposed=False when the class's
	// webhooks.mode is External and the Company does not state its address
	// in spec.exposure.publicBaseURL.
	ReasonPublicBaseURLMissing = "PublicBaseURLMissing"

	// ReasonNotExposed is Healthy=False on an integration that needs inbound
	// delivery when the company has no public address
	// (status.endpoints.publicBaseURL), and BoxModeRunnable=False on an
	// LLMProvider in mode Box then: its runs happen in a
	// remote sandbox, which calls the seat's tools back through the engine's
	// public address. It is Ready=False on an agent Seat whose executor's
	// chain such a provider heads, in whatever pool it is placed: the engine
	// applies the seat and refuses each of its turns.
	ReasonNotExposed = "NotExposed"

	// ReasonNoProvisioner is ResolvedRefs=False on a Seat that names a
	// provisioned credential while no Integration of its Company has
	// provisioning Engine: nothing creates the value, so the engine starts
	// none of the seat's tool servers that take it and refuses each of its
	// run_sandbox coding runs whose environment holds it, unless the key the
	// value is written under is optional, when each starts without it; its
	// chat app, and a box-mode run of its executor, run without it. It is
	// CodeWorkRunnable=False on such a Seat whose coding runs it refuses. The
	// seat is applied, with the reference the engine fills once an
	// Integration provisions.
	ReasonNoProvisioner = "NoProvisioner"

	// ReasonNotAnAccountID is ResolvedRefs=False on a Seat whose
	// spec.contact.atlassianAccount is not in the form of an Atlassian
	// account id while its Company reaches Jira or Confluence on Atlassian
	// Cloud, which names a person by account id alone: an email address, or
	// a Data Center username. Nothing the person does there reaches the seat
	// as theirs, and an agent's mention of them names nobody. The seat is
	// applied with the value as written.
	ReasonNotAnAccountID = "NotAnAccountID"

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
	// when a referenced Secret or Secret key does not exist, but for a value
	// a tool server or a coding run is started with under a key an
	// optionalKeys list lets go without it, which holds nothing back; and
	// RuntimeReady=False when a Secret the engine's pods are given values
	// from does not: one the EngineClass references (telemetry.headersSecretRef,
	// a podTemplate.spec.env valueFrom) or the Company's
	// spec.objects.credentialsSecretRef. On an EngineClass it is
	// Accepted=False when a Secret or key the class references does not
	// exist.
	ReasonSecretNotFound = "SecretNotFound"

	// ReasonSecretValueEmpty is ResolvedRefs=False on an object that gives a
	// tool server or a coding run a value from a Secret key that holds
	// nothing, under a key no optionalKeys list lets go without it, where the
	// value is the one the engine starts with: Crewlet starts no tool server,
	// and no run_sandbox coding run, with a value that resolves to nothing, so
	// the server it is given to does not start, for a seat or for the
	// company, and the seat's coding runs are refused, which is
	// CodeWorkRunnable=False with this reason on the Seat. The object is
	// applied, and the value reaches the engine once the Secret holds one.
	// Listing the key in the optionalKeys list that governs it has it left
	// out instead: the MCPServer's spec.optionalKeys for a value a tool
	// server is started with, the server's own or a Seat's or a Unit's, though
	// a seat's own credential, the identity its instance acts as, is not one
	// to start without; a Seat's spec.sandbox.optionalKeys, or the step's own,
	// for a coding run's.
	ReasonSecretValueEmpty = "SecretValueEmpty"

	// ReasonOptionalKeyUnused is ResolvedRefs=False on an MCPServer whose
	// spec.optionalKeys names a key no value read from a Secret, or
	// provisioned, is written under: not by its own env or headers, nor by
	// any Seat's or Unit's spec.mcp for it. Nothing is left out for the
	// entry, and the key it was meant for stays one the server is not
	// started without, so Crewlet refuses the document, which is held.
	ReasonOptionalKeyUnused = "OptionalKeyUnused"

	// ReasonConflict is Accepted=False when the object collides with
	// another of its company in a way Crewlet refuses: two Integrations for
	// one vendor (Jira, Confluence and Atlassian are three vendors); two
	// Seats with one handle, one displayName or one contact account (any
	// field of spec.contact); two Units that answer to one key (a
	// displayName or a name, compared without case); or an LLMProvider
	// whose name is also the name of one of the EngineClass's
	// platformLLMProviders the company uses, since both are keys of
	// Crewlet's one providers.llm mapping; or a Seat whose spec.mcp gives an
	// HTTP server a header its Unit's spec.mcp gives the server in another
	// case, since Crewlet lays a unit's values under its members' by the
	// name as written and would send either one. The message names the
	// other object.
	ReasonConflict = "Conflict"

	// ReasonReferenceNotAllowed is ResolvedRefs=False on an object (or
	// ConfigApplied=False on a Company, for its own fields and engineConfig)
	// that holds a ${NAME} reference in a field the operator renders into the
	// company document. Crewlet resolves one from the engine's own secret
	// store and environment in many fields that hold no credential (an MCP
	// server's url, a provider's base_url), so it would name values that are
	// not the object's. The operator refuses one in every string it
	// renders, a string Crewlet passes on as written (a sandbox setup
	// command) included, except the references it writes itself, so a
	// field Crewlet starts expanding later names none either. A credential
	// is given with a Secret reference.
	ReasonReferenceNotAllowed = "ReferenceNotAllowed"

	// ReasonCycle is ResolvedRefs=False on a Unit whose parentRef chain
	// leads back to itself.
	ReasonCycle = "Cycle"

	// ReasonHumanSeat is ResolvedRefs=False on a Schedule whose
	// target.seatRef names a Human seat: the engine runs no turn for a
	// person, so the schedule could never fire, and refuses it. Target an
	// Agent seat, or the person's unit with fanOut Each. It is also
	// ResolvedRefs=False on an Integration whose Datadog routeTo.seatRef
	// names a Human seat, which the engine refuses as well, and on one whose
	// GitLab accessLevels entry does: the engine creates no GitLab account
	// for a person, so that entry grants nothing, and is applied and
	// reported only.
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
	// Company's spec.topology.seatPools. The seat is applied, pinned to the
	// label crewlet.ai/undeclared-pool, which no pod carries: it is
	// Ready=False with the reason while no engine node holds it, a seat that
	// ran elsewhere keeps running there (Applied=False, PlacementPending),
	// and the Company reports SeatsPlaced=False with the reason.
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

	// ReasonCLIAuthNotPortable is Applied=False on a Seat placed in a seat
	// pool that runs on an LLMProvider signed in with a credential
	// bundle. A seat pod keeps the CLI's login on its scratch volume, so the
	// bundle is restored at every start, and every pod refreshes the one
	// login on its own: a vendor that rotates refresh tokens then logs the
	// whole company out.
	ReasonCLIAuthNotPortable = "CLIAuthNotPortable"

	// ReasonCodeWorkSupported is CodeWorkRunnable=True on a Seat whose
	// coding agent can run on the provider at the head of its sandbox chain,
	// or on the sign-in the seat's sandbox environment gives it; the message
	// says which.
	ReasonCodeWorkSupported = "CodeWorkSupported"

	// ReasonToolBridgeServed is BoxModeRunnable=True on an LLMProvider
	// whose box-mode runs can reach the tool bridge at the Company's
	// public address.
	ReasonToolBridgeServed = "ToolBridgeServed"

	// ReasonCodingAgentMismatch is Applied=False and CodeWorkRunnable=False
	// on a Seat whose code work (spec.sandbox) runs on a model provider, an
	// LLMProvider or a platform provider, driving another CLI than the seat's
	// coding agent (spec.sandbox.codingAgent, else the Company's
	// spec.sandbox.defaultCodingAgent, else ClaudeCode): the head of
	// spec.llm.sandbox, else of spec.llm.default, else of the Company's model
	// order. The provider's model is written in its own CLI's grammar and its
	// sign-in is that CLI's, so the coding agent can read neither, and
	// Crewlet refuses the pairing on every write. It is also Applied=False
	// alone on a Seat whose executor runs in box mode (an LLMProvider in
	// mode Box heads its executor's chain) and whose spec.sandbox.codingAgent
	// names another CLI than that provider's: Crewlet never offers such a seat
	// run_sandbox, as it codes in its executor's run, and refuses on every
	// write a coding agent that would drive nothing.
	ReasonCodingAgentMismatch = "CodingAgentMismatch"

	// ReasonSandboxLoginNotPortable is CodeWorkRunnable=False on a Seat
	// whose code work runs on an LLMProvider of its coding agent's CLI whose
	// login cannot follow a run into the Company's remote sandbox: a
	// ClaudeCode signed in by a credential bundle with neither
	// CLAUDE_CODE_OAUTH_TOKEN nor ANTHROPIC_API_KEY in the seat's sandbox
	// environment, or an OpenCode with no credential-named variable in its
	// env or the seat's sandbox environment. A bundle stays on the
	// engine's data volume: Crewlet applies the seat, which runs its turns,
	// and refuses each run_sandbox launch of it.
	ReasonSandboxLoginNotPortable = "SandboxLoginNotPortable"

	// ReasonPlacementPending is Applied=False on an agent Seat placed in a
	// pool other than the one the revision the engine runs pins it to,
	// while that pool's StatefulSet has no ready pod, or the Company does
	// not declare the pool: the operator keeps the seat pinned where it
	// runs, so that it is never pinned to pods that are not there, and
	// moves the pin with the first revision after one is.
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
