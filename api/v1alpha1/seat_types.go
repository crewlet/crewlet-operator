package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SeatKind is whether a seat is an AI agent or a person. It maps onto
// Crewlet's role `kind` (Agent is `agent`, Human is `human`).
// +kubebuilder:validation:Enum=Agent;Human
type SeatKind string

// The seat kinds.
const (
	// SeatKindAgent is an AI agent: the engine runs it, with its own inbox,
	// models and tools.
	SeatKindAgent SeatKind = "Agent"
	// SeatKindHuman is a person: part of the chart (a lead, an escalation
	// target, a colleague to mention) whom the engine never runs.
	SeatKindHuman SeatKind = "Human"
)

// ManagedKind is the kind of object a seat manages.
// +kubebuilder:validation:Enum=Seat;Unit
type ManagedKind string

// The managed kinds.
const (
	// ManagedKindSeat is a single Seat.
	ManagedKindSeat ManagedKind = "Seat"
	// ManagedKindUnit is every seat of a Unit and of the units below it.
	ManagedKindUnit ManagedKind = "Unit"
)

// ManagedReference names a Seat or a Unit, in the referring Seat's namespace,
// that a seat manages.
type ManagedReference struct {
	// kind is the kind of object named.
	// +optional
	// +default="Seat"
	Kind ManagedKind `json:"kind,omitempty"`

	// name is the object's metadata.name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// SeatLLM is the model providers a seat runs on, one fallback chain per phase
// of its work: when every key of the first provider is cooling down, the next
// is tried. It maps onto Crewlet's per-phase `llm` mapping. A phase left unset
// uses the default chain; a seat with no default chain runs on the first
// provider of the Company's spec.llm.order alone, with no fallback.
// +kubebuilder:validation:MinProperties=1
type SeatLLM struct {
	// default is the chain of every phase not set below, including the
	// executor that does the seat's work. Crewlet's `default`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Default []LLMProviderReference `json:"default,omitempty"`

	// review is the chain of the reviewer that judges the executor's work.
	// Crewlet's `review`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Review []LLMProviderReference `json:"review,omitempty"`

	// subagent is the chain of the short-lived workers the seat delegates
	// to. Crewlet's `subagent`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Subagent []LLMProviderReference `json:"subagent,omitempty"`

	// auxiliary is the chain of the cheap model used for reflection and
	// summaries. Crewlet's `auxiliary`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Auxiliary []LLMProviderReference `json:"auxiliary,omitempty"`

	// judge is the chain of the model that decides whether a turn that ran
	// out of rounds may have more. Crewlet's `judge`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Judge []LLMProviderReference `json:"judge,omitempty"`

	// sandbox is the chain the coding agent in the seat's sandbox runs on.
	// Crewlet's `sandbox`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:validation:XValidation:rule="self.all(a, self.exists_one(b, b.kind == a.kind && b.name == a.name))",message="a provider may appear only once in a chain"
	Sandbox []LLMProviderReference `json:"sandbox,omitempty"`
}

// SeatSandbox gives a seat a sandbox to run code in, on the Company's
// spec.sandbox. It maps onto Crewlet's role `sandbox`; its presence is
// Crewlet's `enabled: true`.
type SeatSandbox struct {
	// codingAgent is the coding agent the seat's sandbox runs. Unset takes
	// the Company's spec.sandbox.defaultCodingAgent. Crewlet's
	// `coding_agent`.
	// +optional
	CodingAgent CodingAgent `json:"codingAgent,omitempty"`

	// pauseTTLSeconds is how long the seat's sandbox stays paused while it
	// waits on a person. Unset takes the Company's default; 0 never pauses.
	// Crewlet's `pause_ttl_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	PauseTTLSeconds *int32 `json:"pauseTTLSeconds,omitempty"`

	// maxTurns caps the agentic rounds of the seat's coding runs. Unset
	// takes the Company's default; 0 is uncapped. Crewlet's `max_turns`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxTurns *int32 `json:"maxTurns,omitempty"`

	// mcpServerRefs name the MCPServers the coding agent may use. Crewlet's
	// mcp.servers.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	MCPServerRefs []LocalObjectReference `json:"mcpServerRefs,omitempty"`

	// env is the environment of the seat's sandbox runs, for the external
	// tokens its code work needs: from Secrets, or provisioned by the
	// engine, such as the GitLab token a provisioning Integration mints for
	// the seat, named as its tool server's is so both are one token.
	// Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SeatEnvVar `json:"env,omitempty"`

	// setup is the seat's own provisioning, applied after the Company's.
	// Crewlet's `setup`.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	Setup []SandboxSetupStep `json:"setup,omitempty"`
}

// SeatPlacement is where a seat runs.
type SeatPlacement struct {
	// pool is the pool of pods that runs the seat: data, the company's data
	// pods, or the name of one of the Company's spec.topology.seatPools.
	// The seat is pinned to the pool with Crewlet's role.placement, which
	// the engine never widens: while the pool runs no pod, the seat runs
	// nowhere and its messages wait for one.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Pool string `json:"pool,omitempty"`
}

// SlackSeatIntegration is a seat's own Slack app, so the seat posts as itself.
// It maps onto Crewlet's role integrations.slack and needs the company's Slack
// Integration.
type SlackSeatIntegration struct {
	// credentialsSecretRef is a Secret holding the app's credentials under
	// the keys botToken (the bot token) and signingSecret (which verifies
	// the app's webhooks). Crewlet's `bot_token` and `signing_secret`, both
	// credential positions. Rewriting the Secret rotates them without a
	// restart.
	// +required
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef,omitzero"`
}

// GitHubAccessTier is how much a seat may do in a repository. It maps onto
// Crewlet's role integrations.github.tier (ReadOnly is `read_only`, Review is
// `review`, FullAccess is `full_access`).
// +kubebuilder:validation:Enum=ReadOnly;Review;FullAccess
type GitHubAccessTier string

// The GitHub access tiers, least first.
const (
	GitHubAccessReadOnly   GitHubAccessTier = "ReadOnly"
	GitHubAccessReview     GitHubAccessTier = "Review"
	GitHubAccessFullAccess GitHubAccessTier = "FullAccess"
)

// GitHubApp is the GitHub App a seat acts as. An app carries exactly one bot
// identity, so every agent seat that works on GitHub has its own.
type GitHubApp struct {
	// id is the app's numeric id. Crewlet's `app_id`.
	// +required
	// +kubebuilder:validation:Minimum=1
	ID int64 `json:"id,omitempty"`

	// slug is the app's slug; the bot's login, and so how the seat is
	// mentioned, derives from it. Crewlet's `app_slug`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Slug string `json:"slug,omitempty"`

	// installationID is the app's installation on the organization. Unset
	// while the app is created but not yet installed. Crewlet's
	// `installation_id`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	InstallationID int64 `json:"installationID,omitempty"`

	// credentialsSecretRef is a Secret holding the app's credentials under
	// the keys privateKey (the app's PEM key) and webhookSecret (which
	// verifies the app's own webhook deliveries). Crewlet's `private_key`
	// and `webhook_secret`, both credential positions.
	// +required
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef,omitzero"`
}

// GitHubSeatIntegration is a seat's identity on GitHub. It maps onto
// Crewlet's role integrations.github and needs the company's GitHub
// Integration.
// +kubebuilder:validation:MinProperties=1
type GitHubSeatIntegration struct {
	// tier is how much the seat may do in a repository. Unset is ReadOnly.
	// Crewlet's `tier`.
	// +optional
	Tier GitHubAccessTier `json:"tier,omitempty"`

	// repos are the repositories the seat works in, as owner/name. Unset is
	// every repository the app's installation covers. Crewlet's `repos`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=201
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`
	Repos []string `json:"repos,omitempty"`

	// app is the GitHub App the seat acts as.
	// +optional
	App GitHubApp `json:"app,omitzero"`
}

// MattermostSeatIntegration is a seat's own Mattermost bot. It maps onto
// Crewlet's role integrations.mattermost and needs the company's Mattermost
// Integration. One credential covers everything the bot does: the websocket
// the engine listens on, its REST calls, and the Mattermost tool server, whose
// credentials (in spec.mcp) name the same token.
type MattermostSeatIntegration struct {
	// botToken is the bot account's personal access token: from a Secret,
	// or provisioned by the engine under a Mattermost Integration with
	// provisioning Engine, which creates the bot and mints the token.
	// Crewlet's `bot_token`, a credential position.
	// +required
	BotToken SeatCredentialSource `json:"botToken,omitzero"`

	// username is the bot's username, for a bot that exists under a name
	// other than the seat's handle (with the Integration's
	// engineProvisioning.usernamePrefix before it). Crewlet's `username`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9._-]*$`
	Username string `json:"username,omitempty"`

	// channel is a channel, by its name (the URL slug), the engine adds the
	// bot to when it provisions it, on top of the Integration's
	// engineProvisioning.channels. Crewlet's `channel`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9_-]*$`
	Channel string `json:"channel,omitempty"`
}

// SeatIntegrations is a seat's own identity on the company's integrations:
// the chat app it speaks as, the code-host app it acts as. Its tool
// credentials, the GitLab, Jira, Confluence and Datadog ones included, are in
// spec.mcp instead, which is where Crewlet reads them.
// +kubebuilder:validation:MinProperties=1
type SeatIntegrations struct {
	// slack is the seat's own Slack app.
	// +optional
	Slack SlackSeatIntegration `json:"slack,omitzero"`

	// github is the seat's identity on GitHub.
	// +optional
	GitHub GitHubSeatIntegration `json:"github,omitzero"`

	// mattermost is the seat's own Mattermost bot.
	// +optional
	Mattermost MattermostSeatIntegration `json:"mattermost,omitzero"`
}

// SeatContact is how agents reach a person: their accounts on the company's
// surfaces. It maps onto Crewlet's role `contact`. Crewlet refuses two seats
// that claim one account, so a Seat whose slackUserID or githubLogin (in any
// case: GitHub logins are compared without it) another Seat of the company
// already claims is reported as a Conflict.
// +kubebuilder:validation:MinProperties=1
type SeatContact struct {
	// slackUserID is the person's Slack member id, such as U0123ABCD.
	// Crewlet's `slack_user_id`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Z0-9]+$`
	SlackUserID string `json:"slackUserID,omitempty"`

	// githubLogin is the person's GitHub login. Crewlet's `github_login`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	GitHubLogin string `json:"githubLogin,omitempty"`
}

// SeatSpec is one seat of a company: an AI agent or a person. It renders to
// one entry of Crewlet's roles, nested under its unit's.
// +kubebuilder:validation:XValidation:rule="self.kind != 'Human' || has(self.contact)",message="a Human seat needs contact: how agents reach the person"
// +kubebuilder:validation:XValidation:rule="self.kind != 'Agent' || (!has(self.contact) && !has(self.availability))",message="contact and availability apply to Human seats only"
// +kubebuilder:validation:XValidation:rule="self.kind != 'Human' || !(has(self.llm) || has(self.tokenBudget) || has(self.workers) || has(self.learningEnabled) || has(self.mcp) || has(self.sandbox) || has(self.integrations) || has(self.behavioralGuidelines) || has(self.project) || has(self.space) || has(self.placement) || has(self.paused))",message="a Human seat is never run by the engine: llm, tokenBudget, workers, learningEnabled, mcp, sandbox, integrations, behavioralGuidelines, project, space, placement and paused apply to Agent seats only"
// +kubebuilder:validation:XValidation:rule="(!has(self.pauseReason) && !has(self.stopRunning)) || (has(self.paused) && self.paused)",message="pauseReason and stopRunning describe a pause: they apply only while paused is true"
type SeatSpec struct {
	// companyRef names the Company this seat belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// handle is the seat's canonical slug. Unset is the Seat's
	// metadata.name. Crewlet derives the seat's identity, and with it the
	// seat's memory, from the company name and the handle, so the handle
	// cannot be changed; a Seat deleted and created again with the same
	// handle gets its memory back. Crewlet's `handle`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Handle string `json:"handle,omitempty"`

	// kind is whether the seat is an AI agent or a person. Crewlet's `kind`.
	// +optional
	// +default="Agent"
	Kind SeatKind `json:"kind,omitempty"`

	// displayName is the seat's name as people read it, such as "Ana
	// (Platform Engineer)". Unset is the handle. Crewlet's `name`. Crewlet
	// refuses two seats with one name, so a Seat whose displayName (or, unset,
	// handle) another Seat of the company already uses is reported as a
	// Conflict.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	DisplayName string `json:"displayName,omitempty"`

	// email is the seat's email address. Crewlet's `email`.
	// +optional
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=254
	// +kubebuilder:validation:Pattern=`^[^@\s]+@[^@\s]+$`
	Email string `json:"email,omitempty"`

	// unitRef names the Unit, in this namespace, the seat belongs to. Unset
	// is a seat outside every unit, such as a chief executive.
	// +optional
	UnitRef LocalObjectReference `json:"unitRef,omitzero"`

	// goal is what the seat is for; it reaches the seat's prompt. Crewlet's
	// `goal`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	Goal string `json:"goal,omitempty"`

	// backstory is who the seat is; it reaches the seat's prompt. Crewlet's
	// `backstory`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=8192
	Backstory string `json:"backstory,omitempty"`

	// responsibilities are what the seat owns. Crewlet's
	// `responsibilities`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	Responsibilities []string `json:"responsibilities,omitempty"`

	// behavioralGuidelines are how the seat should work. Crewlet's
	// `behavioral_guidelines`.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=1024
	BehavioralGuidelines []string `json:"behavioralGuidelines,omitempty"`

	// manages names the seats and units this seat manages; a unit stands
	// for every seat in it and below it. Crewlet's `manages`.
	// +optional
	// +listType=map
	// +listMapKey=kind
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=64
	Manages []ManagedReference `json:"manages,omitempty"`

	// workers narrows which of the company's delegate templates (the
	// workers block of the Company's engineConfig) the seat may use, by the
	// templates' names: a lowercase letter or digit, then lowercase letters,
	// digits, '_' and '-'. Unset is every one. Crewlet's `workers`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=128
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9][a-z0-9_-]*$`
	Workers []string `json:"workers,omitempty"`

	// tokenBudget caps the tokens this seat may spend per window, on top of
	// the Company's caps. Crewlet's `token_budget`.
	// +optional
	TokenBudget TokenBudget `json:"tokenBudget,omitzero"`

	// llm is the model providers the seat runs on.
	// +optional
	LLM SeatLLM `json:"llm,omitzero"`

	// learningEnabled overrides the company's learning setting for this
	// seat: false stops reflection after the seat's turns. Unset inherits.
	// Crewlet's `learning_enabled`.
	// +optional
	LearningEnabled *bool `json:"learningEnabled,omitempty"`

	// mcp are the seat's own tool-server credentials, which make it act as
	// itself on each server. They are merged over its unit's, per variable.
	// One entry per server, keyed by its name. Crewlet's `mcp_env`, a
	// credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	MCP []MCPCredentials `json:"mcp,omitempty"`

	// sandbox gives the seat a sandbox to run code in. Unset gives it none.
	// +optional
	Sandbox *SeatSandbox `json:"sandbox,omitempty"`

	// placement is where the seat runs. Unset runs it in the Company's
	// spec.topology.defaultSeatPool.
	// +optional
	Placement SeatPlacement `json:"placement,omitzero"`

	// paused says whether the seat may take work. True pauses it: it starts
	// no new turn, the mail it is sent waits on its inbox in order, and its
	// schedules' fires are skipped rather than queued; the turn it is on
	// finishes first unless stopRunning is set. False resumes it, and what
	// waited is delivered first. Unset leaves the seat's pause to the people
	// who run the company in the engine's dashboard: the operator reports a
	// pause they take, and never lifts it.
	//
	// A pause is not part of the company document. Crewlet keeps it in its
	// coordination store, changed through its operator catalogue's
	// pause_seat and resume_seat tools, which the operator calls under the
	// Company's Enforce policy whenever the seat's pause differs from this
	// field; under Observe and Seed it only reports the difference.
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// pauseReason is why the seat is paused, shown on the seat and in its
	// feed in the engine. Only with paused true. The engine keeps the
	// reason a pause was taken with, so a reason changed while the seat
	// stays paused shows from the seat's next pause on. Crewlet's pause
	// `reason`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=500
	PauseReason string `json:"pauseReason,omitempty"`

	// stopRunning ends the turn the seat is on at its next round when it is
	// paused, rather than letting the turn finish; the stopped turn is not
	// run again. Only with paused true. Setting it on a seat already paused
	// stops the turn it is on then; unsetting it changes nothing, since a
	// stopped turn stays stopped. Crewlet's pause `stop_running`.
	// +optional
	StopRunning *bool `json:"stopRunning,omitempty"`

	// integrations is the seat's own identity on the company's
	// integrations.
	// +optional
	Integrations SeatIntegrations `json:"integrations,omitzero"`

	// project is the tracker project a seat outside every unit owns, by its
	// key; a seat in a unit takes its unit's. Crewlet requires a container
	// key to be an upper-case letter followed by 1 to 9 upper-case letters or
	// digits, such as LEAD. Crewlet's `project`.
	// +optional
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	Project string `json:"project,omitempty"`

	// space is the knowledge container a seat outside every unit owns, by
	// its key, which follows the same rule as project; a seat in a unit
	// takes its unit's. Crewlet's `space`.
	// +optional
	// +kubebuilder:validation:MinLength=2
	// +kubebuilder:validation:MaxLength=10
	// +kubebuilder:validation:Pattern=`^[A-Z][A-Z0-9]{1,9}$`
	Space string `json:"space,omitempty"`

	// contact is how agents reach the person in a Human seat. Required for,
	// and only for, kind Human.
	// +optional
	Contact SeatContact `json:"contact,omitzero"`

	// availability tells colleagues of a Human seat when to expect an
	// answer, such as "CET business hours". Crewlet's `availability`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Availability string `json:"availability,omitempty"`
}

// SlackSeatStatus is the Slack side of a seat, as the operator computed it.
// +kubebuilder:validation:MinProperties=1
type SlackSeatStatus struct {
	// requestURL is the URL the seat's Slack app must deliver events to: its
	// Event Subscriptions request URL, <publicBaseURL>/webhooks/slack/<handle>,
	// where publicBaseURL is the Company's status.endpoints.publicBaseURL.
	//
	// It is set whenever the Company has a Slack Integration and a public
	// base URL, whether or not this Seat sets spec.integrations.slack: a
	// platform that creates the seat's Slack app needs the URL for the app's
	// manifest before the app, and so its credentials, exist. Derived from
	// the handle, which never changes, it never changes for a seat either.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	RequestURL string `json:"requestURL,omitempty"`
}

// GitHubSeatStatus is the GitHub side of a seat, as the operator computed it.
// +kubebuilder:validation:MinProperties=1
type GitHubSeatStatus struct {
	// webhookURL is the URL the seat's GitHub App must deliver webhooks to,
	// <publicBaseURL>/webhooks/github/<handle>. Like the Slack requestURL it
	// is set whenever the Company has a GitHub Integration and a public base
	// URL, before the seat's app exists, and it never changes for a seat.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	WebhookURL string `json:"webhookURL,omitempty"`
}

// SeatPlacementStatus is where the engine runs a seat.
// +kubebuilder:validation:MinProperties=1
type SeatPlacementStatus struct {
	// pool is the pool of pods that runs the seat: spec.placement.pool, or
	// the Company's spec.topology.defaultSeatPool. The seat is rendered with
	// role.placement.labels {crewlet.ai/pool: <pool>}, which only the pool's
	// pods carry, or, in a PerSeat pool, {crewlet.ai/seat: <handle>}, which
	// only the seat's own pods carry.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=20
	Pool string `json:"pool,omitempty"`

	// workload is the name of the StatefulSet whose pods may run the seat:
	// the data pods', the Shared pool's, or the seat's own in a PerSeat
	// pool.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Workload string `json:"workload,omitempty"`

	// node is the engine node, a pod's name, that holds the seat's lease
	// and runs it, as the engine's GET /fleet last reported. Unset while no
	// node holds it.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Node string `json:"node,omitempty"`
}

// SeatPauseStatus is a seat's pause, as the engine reports it.
// +kubebuilder:validation:MinProperties=1
type SeatPauseStatus struct {
	// pausedBy is who paused the seat: the seat of the person whose engine
	// token paused it, or the token's id when no seat is bound to it. A
	// pause the operator took is its token's, operator.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	PausedBy string `json:"pausedBy,omitempty"`

	// pausedAt is when the seat was paused, to the second.
	// +optional
	PausedAt *metav1.Time `json:"pausedAt,omitempty"`

	// reason is why, in the pauser's words.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=500
	Reason string `json:"reason,omitempty"`

	// stopRunning is whether the pause also ended the turn the seat was on.
	// +optional
	StopRunning *bool `json:"stopRunning,omitempty"`
}

// SeatIntegrationsStatus is the addresses a seat's vendor apps must use.
// +kubebuilder:validation:MinProperties=1
type SeatIntegrationsStatus struct {
	// slack is the Slack side of the seat.
	// +optional
	Slack SlackSeatStatus `json:"slack,omitzero"`

	// github is the GitHub side of the seat.
	// +optional
	GitHub GitHubSeatStatus `json:"github,omitzero"`
}

// SeatStatus is what the operator observed about a Seat.
// +kubebuilder:validation:MinProperties=1
type SeatStatus struct {
	// conditions are Accepted (the Company exists, and no other Seat has the
	// handle, the name or a contact account; Conflict names the other),
	// ResolvedRefs (the unit, the providers, the tool servers and
	// every Secret key exist), Applied (the seat is in the revision the
	// engine runs) and, on an Agent seat, Ready (an engine node holds the
	// seat's lease and runs it: LeaseHeld, or Unplaced) and, once the
	// engine runs it, Paused (whether the engine holds it paused, and
	// whether that is what spec.paused asks).
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// handle is the seat's handle: spec.handle, or the metadata.name.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Handle string `json:"handle,omitempty"`

	// seatID is the seat's identity in the engine, derived from the company
	// name and the handle, so that callers need not derive it themselves.
	// Unset for a Human seat, which the engine does not run and gives no
	// identity, and for a seat whose handle another Seat derives too.
	// +optional
	// +kubebuilder:validation:MinLength=36
	// +kubebuilder:validation:MaxLength=36
	SeatID string `json:"seatID,omitempty"`

	// placement is where the engine runs the seat. Unset for a Human seat,
	// which the engine does not run.
	// +optional
	Placement SeatPlacementStatus `json:"placement,omitzero"`

	// integrations is the addresses the seat's vendor apps must use. Unset
	// for a Human seat, which has no vendor apps of its own.
	// +optional
	Integrations SeatIntegrationsStatus `json:"integrations,omitzero"`

	// pause is the seat's pause while the engine holds it paused, whoever
	// paused it: the operator, for spec.paused, or a person in the engine's
	// dashboard. Unset while the seat takes work.
	// +optional
	Pause SeatPauseStatus `json:"pause,omitzero"`
}

// Seat is one seat of a company's org chart: an AI agent the engine runs, or a
// person agents work with.
//
// spec.paused pauses an agent seat while keeping it in the chart; Company's
// spec.suspend stops the whole engine instead.
//
// Unless spec.handle is set, the Seat's metadata.name is its handle and must
// follow the handle's rules: lowercase letters, digits and '-', starting with
// a letter or digit, at most 63 characters. The handle cannot change.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=st,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=`.spec.kind`
// +kubebuilder:printcolumn:name="Unit",type=string,JSONPath=`.spec.unitRef.name`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Paused",type=string,JSONPath=`.status.conditions[?(@.type=="Paused")].status`
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.status.placement.pool`,priority=1
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.placement.node`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="has(self.spec.handle) || self.metadata.name.matches('^[a-z0-9][a-z0-9-]{0,62}$')",message="without spec.handle the Seat's name is its handle: lowercase letters, digits and '-', starting with a letter or digit, at most 63 characters",fieldPath=".spec.handle"
// +kubebuilder:validation:XValidation:rule="(has(self.spec.handle) ? self.spec.handle : self.metadata.name) == (has(oldSelf.spec.handle) ? oldSelf.spec.handle : oldSelf.metadata.name)",message="the seat's handle is immutable: the seat's identity and memory are derived from it",fieldPath=".spec.handle"
type Seat struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the seat's desired state.
	// +required
	Spec SeatSpec `json:"spec,omitzero"`

	// status is the seat's observed state.
	// +optional
	Status SeatStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SeatList contains a list of Seat.
type SeatList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Seat `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Seat{}, &SeatList{})
		return nil
	})
}
