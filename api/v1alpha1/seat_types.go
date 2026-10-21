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
	// default is the chain of every phase not set below, and of the seat's
	// executor, which does its work. Crewlet runs the executor as the
	// agentic run of the chain's first member that drives ClaudeCode or
	// OpenCode and is not sitting out its spent plan on the node, in that
	// member's cell (its runIn, else the Company's spec.sandbox.defaultRunIn),
	// and a launch falls back along the chain to the next such member. A
	// member of any other CLI never runs the executor and still serves every
	// phase below that is unset. Crewlet refuses a chain with no ClaudeCode
	// or OpenCode member, which the operator refuses as NoExecutorCLI, and
	// applies one that mixes them with an advisory that its executor never
	// falls back to the others. Crewlet's `default`.
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
}

// SeatSandbox is what a seat's executor box reads beyond the Company's
// spec.sandbox: its own provisioning and environment, the tool servers it is
// given, and the bounds of one run. It maps onto Crewlet's role `sandbox`.
// Nothing here places the box or picks its CLI: every agent seat's executor
// is its coding CLI's own run, placed by the provider it launches on, so a
// seat with no spec.sandbox runs on the Company's settings alone. An empty
// one says nothing, and the API refuses it.
// +kubebuilder:validation:MinProperties=1
// +kubebuilder:validation:XValidation:rule="!has(self.optionalKeys) || self.optionalKeys.all(k, has(self.env) && self.env.exists(e, e.name == k))",message="optionalKeys may name only a variable of env: an entry naming anything else leaves nothing out",fieldPath=".optionalKeys"
type SeatSandbox struct {
	// pauseTTLSeconds is how long the seat's box stays paused while its run
	// waits on a person. Unset takes the Company's default; 0 never pauses.
	// Crewlet's `pause_ttl_seconds`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	PauseTTLSeconds *int32 `json:"pauseTTLSeconds,omitempty"`

	// maxTurns caps the agentic rounds of one of the seat's executor runs,
	// the only bound on its turn. Unset takes the Company's
	// spec.sandbox.defaultMaxTurns, else Crewlet's default of 96; 0 is
	// uncapped. Crewlet's `max_turns`.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxTurns *int32 `json:"maxTurns,omitempty"`

	// mcpServerRefs name the MCPServers the seat's executor box is given,
	// beside its tool bridge, which every executor box is given. Crewlet's
	// mcp.servers.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	MCPServerRefs []LocalObjectReference `json:"mcpServerRefs,omitempty"`

	// env is the environment of the seat's executor runs, for the external
	// tokens its work needs: from Secrets, or provisioned by the engine,
	// such as the GitLab token a provisioning Integration mints for the
	// seat's account, which the engine gives its gitlab tool server too, so
	// both are one token. A run gets
	// its provider's whole env beneath this, so a variable named here wins
	// over the provider's of that name. A ClaudeCode sign-in here
	// (CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN or a
	// CLAUDE_CODE_USE_* toggle) keeps the provider's own sign-in out of the
	// box, its token or key and every sign-in in its env, a gateway's
	// bearer token or a cloud switch, while the rest of its env, such as a
	// gateway's address, still reaches the run: a seat meant to bypass the
	// gateway names ANTHROPIC_BASE_URL here itself. A run that goes without
	// the value is given the provider's sign-in as one with no sign-in here
	// is. Crewlet's `env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Env []SeatEnvVar `json:"env,omitempty"`

	// optionalKeys names variables of env whose value may go missing. An
	// executor run starts without a value that does not resolve, a Secret
	// that does not exist, holds no such key or holds an empty value, or a
	// provisioned value nothing has minted yet, whether or not it is listed
	// here: the run is the seat's turn itself, and refusing it would fail
	// every message the seat could answer meanwhile. Unlisted, the engine
	// logs each such value as a gap of the run (sandbox_env_unresolved), and
	// the operator reports an empty one on the Seat; listed, the variable is
	// left out quietly, as an absence that is expected, but for a value an
	// Integration mints for the seat, which is logged as awaited whatever
	// this says. A missing Secret or key under a listed variable is then no
	// failure of the Seat's references and holds nothing back. Under the
	// Enforce policy the operator also takes the value it pushed for it out
	// of the engine's secret store, and has the engine read the store again
	// whether or not the document can be applied, so no run starts with a
	// deleted Secret's value, but in a revision the engine runs that reads
	// the key as required (one from before the key was listed) or whose
	// reading the operator does not know (another document somebody else
	// activated), which keeps the value until the next document runs, and
	// that one is activated once with it and at once again without it; a
	// Secret key that is also read where it is required holds the document
	// while it is missing, and keeps the value it had in a revision the
	// engine runs that requires it too until the document applies. env wins
	// over a setup step's variable of the same name, and so does this list
	// over the step's optionalKeys for it. Crewlet's `optional_keys`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=128
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z_][A-Za-z0-9_]*$`
	OptionalKeys []string `json:"optionalKeys,omitempty"`

	// setup is the seat's own provisioning of its executor box, applied
	// after the Company's. Crewlet's `setup`.
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
// Integration. Slack gives a bot no role: what the app may do is the bot token
// scopes of its manifest, and where it may do it is the channels it is
// invited to, so the block holds no access of its own.
type SlackSeatIntegration struct {
	// enabled is whether the seat is on Slack. False keeps the block, and
	// the engine revokes the app's bot token rather than deleting the app;
	// turning it back on needs a person to approve the install again.
	// Crewlet's `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// credentialsSecretRef is a Secret holding the app's credentials under
	// the keys botToken (the bot token) and signingSecret (which verifies
	// the app's webhooks). Crewlet's `bot_token` and `signing_secret`, both
	// credential positions. Rewriting the Secret rotates them without a
	// restart. The Secret is the seat's alone: Crewlet refuses one another
	// seat or an Integration names too.
	// +required
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef,omitzero"`
}

// GitHubPermission is how much of one GitHub App permission a seat holds. It
// maps onto the level GitHub's API takes for it (Read is `read`, Write is
// `write`), and the dashboard shows it under GitHub's names, Read-only and
// Read and write. A permission left unset is GitHub's No access.
// +kubebuilder:validation:Enum=Read;Write
type GitHubPermission string

// The GitHub permission levels.
const (
	GitHubPermissionRead  GitHubPermission = "Read"
	GitHubPermissionWrite GitHubPermission = "Write"
)

// GitHubPermissions are the GitHub App permissions a seat holds, one field per
// permission GitHub defines for an app, each under GitHub's own key. They map
// onto Crewlet's role integrations.github.permissions. A permission left unset
// is No access; Metadata, which GitHub grants every app read-only, is not
// listed. The engine registers the seat's app with these permissions and
// narrows each installation token to them, so lowering one applies to the next
// token, and raising one past what the app was registered with waits for an
// owner of the organization to accept the app's new permissions.
// +kubebuilder:validation:MinProperties=1
type GitHubPermissions struct {
	// contents is the repository's code, branches and releases. Crewlet's
	// `contents`.
	// +optional
	Contents GitHubPermission `json:"contents,omitempty"`

	// issues is the repository's issues and their comments. Crewlet's
	// `issues`.
	// +optional
	Issues GitHubPermission `json:"issues,omitempty"`

	// pullRequests is the repository's pull requests, their reviews and
	// comments. Crewlet's `pull_requests`.
	// +optional
	PullRequests GitHubPermission `json:"pullRequests,omitempty"`

	// checks is the check runs and suites on the repository's commits.
	// Crewlet's `checks`.
	// +optional
	Checks GitHubPermission `json:"checks,omitempty"`

	// actions is the repository's workflow runs and artifacts. Crewlet's
	// `actions`.
	// +optional
	Actions GitHubPermission `json:"actions,omitempty"`

	// deployments is the repository's deployments and their statuses.
	// Crewlet's `deployments`.
	// +optional
	Deployments GitHubPermission `json:"deployments,omitempty"`

	// workflows lets the seat push a commit that changes a file under
	// .github/workflows, which GitHub refuses otherwise. GitHub offers it as
	// Read and write alone, and a push needs contents Write too. The same
	// grant lets
	// the seat's branches run a workflow it rewrote with the secrets the
	// repository hands its runs, so pair it with branch protection and
	// environment-scoped secrets. Crewlet's `workflows`.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == 'Write'",message="workflows is offered by GitHub as Write alone"
	Workflows GitHubPermission `json:"workflows,omitempty"`
}

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
	// and `webhook_secret`, both credential positions. The Secret is the
	// seat's alone: Crewlet refuses one another seat or an Integration names
	// too.
	// +required
	CredentialsSecretRef SecretReference `json:"credentialsSecretRef,omitzero"`
}

// GitHubSeatIntegration is a seat's identity and access on GitHub. It maps
// onto Crewlet's role integrations.github and needs the company's GitHub
// Integration.
type GitHubSeatIntegration struct {
	// enabled is whether the seat is on GitHub. False keeps the block, and
	// the engine suspends the app's installation rather than uninstalling
	// it, so turning it back on needs no person. Crewlet's `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// permissions are the GitHub App permissions the seat holds. Unset is
	// No access on every permission but Metadata. Crewlet's `permissions`.
	// +optional
	Permissions GitHubPermissions `json:"permissions,omitzero"`

	// repos are the repositories the seat's tokens are narrowed to, as
	// owner/name. Unset is every repository the app's installation covers,
	// which is the organization's choice at GitHub. Where the Integration
	// lists provisioning repos, each must be one of them: Crewlet refuses
	// another, which the engine's dry run reports as Invalid on the Seat.
	// Crewlet's `repos`.
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

// GitLabAccessLevel is a GitLab role, the level of a seat's membership in the
// group and its projects, under GitLab's own name. It maps onto Crewlet's
// access_level, the integer GitLab's Members API takes (Guest is 10, Planner
// 15, Reporter 20, Developer 30, Maintainer 40, Owner 50). Security Manager,
// a beta GitLab leaves off by default, and Minimal Access, which grants an
// agent nothing, are not offered. The token the engine mints follows the
// role: below Developer it can read the API and the repositories but not
// push.
// +kubebuilder:validation:Enum=Guest;Planner;Reporter;Developer;Maintainer;Owner
type GitLabAccessLevel string

// The GitLab roles, least first.
const (
	GitLabGuest      GitLabAccessLevel = "Guest"
	GitLabPlanner    GitLabAccessLevel = "Planner"
	GitLabReporter   GitLabAccessLevel = "Reporter"
	GitLabDeveloper  GitLabAccessLevel = "Developer"
	GitLabMaintainer GitLabAccessLevel = "Maintainer"
	GitLabOwner      GitLabAccessLevel = "Owner"
)

// GitLabSeatIntegration is a seat's account and access on GitLab. It maps onto
// Crewlet's role integrations.gitlab and needs the company's GitLab
// Integration. Under its provisioning Engine the engine creates the seat's
// service account, gives it the role in the projects, and mints its token.
// Under External the account is made outside the engine, which changes
// nothing at GitLab: enabled then turns the seat's own GitLab tools on and
// off, and the engine reports the seat's access as managed outside Crewlet.
// +kubebuilder:validation:XValidation:rule="!(has(self.accessLevel) && has(self.memberRoleID))",message="accessLevel and memberRoleID are two ways to name the seat's role: give one"
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.accessLevel) || has(self.memberRoleID)",message="a seat that is on GitLab needs its role: give accessLevel, or memberRoleID for a custom role"
type GitLabSeatIntegration struct {
	// enabled is whether the seat is on GitLab. False keeps the account,
	// which the engine blocks and whose tokens it revokes, so its history
	// keeps its author and turning it back on needs no person. Crewlet's
	// `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// accessLevel is the seat's GitLab role. One of it and memberRoleID is
	// required while the seat is on. Crewlet's `access_level`.
	// +optional
	AccessLevel GitLabAccessLevel `json:"accessLevel,omitempty"`

	// memberRoleID is a custom role of the group, by its id, in place of
	// accessLevel: GitLab Ultimate only. The engine checks it against the
	// group's roles on every pass, and reports one that no longer exists
	// rather than falling back to another. Crewlet's `member_role_id`.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MemberRoleID int64 `json:"memberRoleID,omitempty"`

	// projects are the projects, by path, the seat is a member of. Unset is
	// the whole group. Where the Integration lists engineProvisioning
	// projects, each must be one of them, so a seat cannot widen the
	// company's reach: Crewlet refuses another, which the engine's dry run
	// reports as Invalid on the Seat. Crewlet's `projects`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=255
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9_.][A-Za-z0-9_./-]*$`
	Projects []string `json:"projects,omitempty"`
}

// MattermostRole is a role or permission of a Mattermost account, under the
// identifier Mattermost's API takes (SystemUser is `system_user`, the Member
// role; SystemPostAllPublic is `system_post_all_public`, post:channels;
// SystemPostAll is `system_post_all`, post:all; SystemAdmin is
// `system_admin`, System Admin).
// +kubebuilder:validation:Enum=SystemUser;SystemPostAllPublic;SystemPostAll;SystemAdmin
type MattermostRole string

// The Mattermost roles.
const (
	// MattermostSystemUser is Member, the role every account holds.
	MattermostSystemUser MattermostRole = "SystemUser"
	// MattermostSystemPostAllPublic is post:channels: post in every public
	// channel, a member or not.
	MattermostSystemPostAllPublic MattermostRole = "SystemPostAllPublic"
	// MattermostSystemPostAll is post:all: post in every channel, private
	// ones and direct messages included.
	MattermostSystemPostAll MattermostRole = "SystemPostAll"
	// MattermostSystemAdmin is System Admin, which grants every other
	// administration privilege of the server too.
	MattermostSystemAdmin MattermostRole = "SystemAdmin"
)

// MattermostSeatIntegration is a seat's own Mattermost bot. It maps onto
// Crewlet's role integrations.mattermost and needs the company's Mattermost
// Integration. One credential covers everything the bot does: the websocket
// the engine listens on, its REST calls, and the Mattermost tool server.
// Under the Integration's provisioning Engine the engine creates the bot,
// gives it the roles and the channels, and mints its token; under External
// the bot is made outside the engine, and the engine reports its access as
// managed outside Crewlet.
// +kubebuilder:validation:XValidation:rule="!has(self.roles) || ('SystemUser' in self.roles && size(self.roles) <= 2)",message="roles is one of Mattermost's sets for a bot: SystemUser alone, or SystemUser with one of SystemPostAllPublic, SystemPostAll and SystemAdmin",fieldPath=".roles"
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.roles)",message="a seat that is on Mattermost needs the roles its bot holds: give roles, such as [SystemUser]",fieldPath=".roles"
type MattermostSeatIntegration struct {
	// enabled is whether the seat is on Mattermost. False keeps the bot,
	// which the engine deactivates and whose tokens it revokes. Crewlet's
	// `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// roles are the bot account's roles: Member (SystemUser) alone, or
	// Member with one of post:channels, post:all and System Admin. Required
	// while the seat is on. Crewlet's `roles`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	Roles []MattermostRole `json:"roles,omitempty"`

	// channels are the channels, by name (the URL slug), the bot is a
	// member of: it hears only the channels it is in. Where the Integration
	// lists engineProvisioning channels, each must be one of them: Crewlet
	// refuses another, which the engine's dry run reports as Invalid on the
	// Seat. Crewlet's `channels`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=64
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9][a-z0-9_-]*$`
	Channels []string `json:"channels,omitempty"`

	// botToken is the Secret key holding the personal access token of a bot
	// made outside the engine, under a Mattermost Integration with
	// provisioning External. Under provisioning Engine leave it unset: the
	// engine creates the bot and mints its token, which no document names,
	// and refuses one given here. The Secret key is the seat's alone:
	// Crewlet refuses one another seat or a company block names too.
	// Crewlet's `bot_token`, a credential position.
	// +optional
	BotToken SecretValueSource `json:"botToken,omitzero"`

	// username is the bot's username, for a bot that exists under a name
	// other than the seat's handle (with the Integration's
	// engineProvisioning.usernamePrefix before it). Crewlet's `username`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9._-]*$`
	Username string `json:"username,omitempty"`
}

// JiraSeatIntegration is a seat's access on Jira. It maps onto Crewlet's role
// integrations.jira and needs the company's Jira Integration. With the
// company's Atlassian Integration the engine creates the seat's account,
// grants it Jira and gives it the project role in the projects; without one
// (a Data Center instance) the account is made outside the engine, and the
// engine reports the seat's access as managed outside Crewlet.
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.projectRole) || !has(self.projects) || size(self.projects) == 0",message="a seat that is on Jira in projects needs the project role it holds there: give projectRole, or leave projects out for product access only",fieldPath=".projectRole"
type JiraSeatIntegration struct {
	// enabled is whether the seat is on Jira. False takes the account out of
	// its project roles and narrows its token, and keeps the account and its
	// Jira product access. Crewlet's `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// projectRole is the project role the seat holds in each of projects,
	// by its id on the site (GET /rest/api/3/role), such as 10101. What a
	// role may do is each project's permission scheme. The engine checks it
	// against the site's roles on every pass, and reports one that no
	// longer exists rather than falling back to another. Required while
	// the seat is on in projects; with neither, the seat holds Jira product
	// access only. Crewlet's `project_role`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern=`^[0-9]+$`
	ProjectRole string `json:"projectRole,omitempty"`

	// projects are the projects, by key, the seat holds projectRole in.
	// Each must be a project the company's document names, as a Unit's or
	// a Seat's spec.project, so a seat cannot widen the company's reach:
	// Crewlet refuses another, which the engine's dry run reports as
	// Invalid on the Seat. Crewlet's `projects`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=255
	// +kubebuilder:validation:items:Pattern=`^[A-Z][A-Z0-9_]*$`
	Projects []string `json:"projects,omitempty"`
}

// ConfluenceSeatIntegration is a seat's access on Confluence. It maps onto
// Crewlet's role integrations.confluence and needs the company's Confluence
// Integration. With the company's Atlassian Integration the engine creates
// the seat's account, grants it Confluence and gives it the access in the
// spaces; without one the account is made outside the engine, and the engine
// reports the seat's access as managed outside Crewlet. A site with
// Confluence's role-based access grants by spaceRole; a site without it
// grants space permissions by operation.
// +kubebuilder:validation:XValidation:rule="!(has(self.spaceRole) && has(self.operations) && size(self.operations) > 0)",message="spaceRole and operations are two ways to grant the seat's access: give spaceRole on a site with role-based access, and operations on one without"
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.spaceRole) || (has(self.operations) && size(self.operations) > 0) || !has(self.spaces) || size(self.spaces) == 0",message="a seat that is on Confluence in spaces needs its access there: give spaceRole, or operations where the site has no space roles, or leave spaces out for product access only"
type ConfluenceSeatIntegration struct {
	// enabled is whether the seat is on Confluence. False takes away the
	// account's access in its spaces and narrows its token, and keeps the
	// account and its Confluence product access. Crewlet's `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// spaceRole is the space role the seat holds in each of spaces, by its
	// id on the site (Confluence's role-based access): Viewer, Collaborator,
	// Manager, Admin, or a custom role. The engine checks it against the
	// site's roles on every pass, and reports one that no longer exists
	// rather than falling back to another. It or operations is required
	// while the seat is on in spaces; with neither and no spaces, the seat
	// holds Confluence product access only. Crewlet's `space_role`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9_:-]*$`
	SpaceRole string `json:"spaceRole,omitempty"`

	// operations are Confluence's space permissions, as
	// <operation>:<target> under Confluence's own names, such as read:space
	// or create:page, the seat holds in each of spaces, for a site whose
	// role-based access is off. Crewlet's `operations`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=3
	// +kubebuilder:validation:items:MaxLength=64
	// +kubebuilder:validation:items:Pattern=`^[a-z_]+:[a-z_]+$`
	Operations []string `json:"operations,omitempty"`

	// spaces are the spaces, by key, the seat holds spaceRole or
	// operations in. Each must be a space the company's document names, as
	// a Unit's or a Seat's spec.space: Crewlet refuses another, which the
	// engine's dry run reports as Invalid on the Seat. Crewlet's `spaces`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=255
	// +kubebuilder:validation:items:Pattern=`^~?[A-Za-z0-9]+$`
	Spaces []string `json:"spaces,omitempty"`
}

// DatadogSeatIntegration is a seat's service account on Datadog. It maps onto
// Crewlet's role integrations.datadog and needs the company's Datadog
// Integration, under which the engine creates the account with the role and
// mints its application key.
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.role)",message="a seat that is on Datadog needs the role its service account holds: give role, a role id as GET /api/v2/roles lists it",fieldPath=".role"
type DatadogSeatIntegration struct {
	// enabled is whether the seat is on Datadog. False disables the service
	// account and deletes its key; Datadog never erases a user, so its
	// history keeps its author. Crewlet's `enabled`.
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	// role is the Datadog role the account holds, by its id
	// (GET /api/v2/roles): a managed role, such as Datadog Read Only Role,
	// or a custom one. The engine checks it against the organization's
	// roles on every pass, and reports one that no longer exists rather than
	// falling back to another. Required while the seat is on. Crewlet's
	// `role`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9-]*$`
	Role string `json:"role,omitempty"`
}

// SeatIntegrations is a seat's own identity and access on the company's
// integrations, one block per vendor: whether the seat is on it, what it may
// do there in the vendor's own terms, and, where the vendor has one, the scope
// it may do it in. A vendor without a block is off for the seat. The
// credential the engine mints for a seat's account is sealed under a name the
// engine gives it for the seat, and the engine hands it to the vendor's tool
// server itself, so spec.mcp holds none for these vendors.
// +kubebuilder:validation:MinProperties=1
type SeatIntegrations struct {
	// slack is the seat's own Slack app.
	// +optional
	Slack SlackSeatIntegration `json:"slack,omitzero"`

	// github is the seat's identity and access on GitHub.
	// +optional
	GitHub GitHubSeatIntegration `json:"github,omitzero"`

	// gitlab is the seat's account and access on GitLab.
	// +optional
	GitLab GitLabSeatIntegration `json:"gitlab,omitzero"`

	// mattermost is the seat's own Mattermost bot.
	// +optional
	Mattermost MattermostSeatIntegration `json:"mattermost,omitzero"`

	// jira is the seat's access on Jira.
	// +optional
	Jira JiraSeatIntegration `json:"jira,omitzero"`

	// confluence is the seat's access on Confluence.
	// +optional
	Confluence ConfluenceSeatIntegration `json:"confluence,omitzero"`

	// datadog is the seat's service account on Datadog.
	// +optional
	Datadog DatadogSeatIntegration `json:"datadog,omitzero"`
}

// SeatContact is how agents reach a person: their accounts on the company's
// surfaces, which tie what the person does there to their seat. It maps onto
// Crewlet's role `contact`, one field for each of its keys but one (below).
//
// Crewlet refuses two seats that claim one account, so a Seat claiming an
// account another Seat of the company already claims is reported as a
// Conflict. Each field is compared as Crewlet compares it: githubLogin,
// gitlabUsername and mattermostUsername without case, as GitHub, GitLab and
// Mattermost compare them; slackUserID and atlassianAccount exactly.
// +kubebuilder:validation:MinProperties=1
type SeatContact struct {
	// slackUserID is the person's Slack member id, such as U0123ABCD.
	// Crewlet's `slack_user_id`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Z0-9]+$`
	SlackUserID string `json:"slackUserID,omitempty"`

	// mattermostUsername is the person's username on the company's
	// Mattermost, such as jane.doe, written without the @: the name an
	// agent mentions them by and opens a direct message with, and the
	// sender an inbound post is attributed by. Crewlet's
	// `mattermost_user_id`, which holds the username despite its name, and
	// never the 26-character user id. Mattermost's own rule for a username
	// holds it: lowercase letters, digits, '.', '-' and '_', at most 64
	// characters, and none of the names Mattermost reserves (all, channel,
	// matterbot, system) nor here: a mention of all, channel or here
	// notifies the whole channel rather than the person.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[a-z0-9._-]+$`
	// +kubebuilder:validation:XValidation:rule="!(self in ['all', 'channel', 'here', 'matterbot', 'system'])",message="mattermostUsername may not be all, channel, here, matterbot or system: Mattermost reserves all, channel, matterbot and system, and a mention of all, channel or here notifies the whole channel rather than the person"
	MattermostUsername string `json:"mattermostUsername,omitempty"`

	// atlassianAccount is the person's identity on the company's Jira and
	// Confluence, one value for both: their account id on Atlassian Cloud,
	// such as 557058:f58131cb-b67d-43c7-b30d-6b58d40bd077 and never their
	// email address, and their username on a Data Center instance, such as
	// jane.doe. Webhooks name the person by it, so it attributes what they do
	// there to their seat, and agents assign and mention them by it.
	// Crewlet's `atlassian_account_id`, which holds the username on Data
	// Center despite its name. Any value up to 255 characters without
	// whitespace is taken, since a Data Center username may hold almost any
	// character. Where the company reaches Jira or Confluence on Atlassian
	// Cloud, a value outside the form of an account id (letters, digits,
	// '_', '|', ':' and '-', at most 128 characters) is applied and reported
	// on the Seat as ResolvedRefs=False with the reason NotAnAccountID.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^\S+$`
	// +kubebuilder:validation:XValidation:rule="!self.contains('${')",message="atlassianAccount may not hold ${: the engine reads a ${NAME} there as a reference, resolved from its own secret store and environment, which hold credentials that are not this object's to name"
	AtlassianAccount string `json:"atlassianAccount,omitempty"`

	// githubLogin is the person's GitHub login. Crewlet's `github_login`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	GitHubLogin string `json:"githubLogin,omitempty"`

	// gitlabUsername is the person's GitLab username, written without the
	// @: assignments, review requests, mentions, and the sender an inbound
	// webhook is attributed by. Crewlet's `gitlab_username`. GitLab's own
	// rule for a username's form holds it: letters, digits, '_', '-' and
	// '.', at most 255 characters, not starting with '-' and not ending
	// with '.'.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^([A-Za-z0-9_.][A-Za-z0-9_.-]*[A-Za-z0-9_-]|[A-Za-z0-9_])$`
	GitLabUsername string `json:"gitlabUsername,omitempty"`
}

// SeatSpec is one seat of a company: an AI agent or a person. It renders to
// one entry of Crewlet's roles, nested under its unit's.
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
	// (Platform Engineer)". Unset is the handle. Crewlet's `name`. It is for
	// reading only: one seat names another, a unit its lead and a seat what
	// it manages, by the handle, so two Seats may share a displayName.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	DisplayName string `json:"displayName,omitempty"`

	// email is the seat's email address. Crewlet's `email`. Each seat has its
	// own: the engine finds a seat by its address, compared lower-cased and
	// with any plus tag dropped, so a Seat whose address another Seat's
	// matches that way is reported as a Conflict.
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
	// for every seat in it and below it. Crewlet's `manages`, which names a
	// Seat by its handle and a Unit by its metadata.name, the Unit's key; a
	// key that is also a seat's handle reads as the seat, so a Unit named so
	// is refused here as a Conflict.
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
	// the Company's caps. Crewlet refuses a window below
	// turn_engine.executor_min_budget_tokens (25000 unless the Company's
	// spec.engineConfig sets turn_engine) on an Agent seat: an executor
	// launch needs that much of the window left, so the seat would never
	// launch one. The refusal comes from the engine's dry run, Invalid on
	// the Seat. Crewlet's `token_budget`.
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
	// itself on each server, and its own settings, such as the channel cache
	// of slack-mcp-server, which no other seat may share. They are merged
	// over its unit's, per variable. One entry per server, keyed by its
	// name. A credential of a vendor the engine provisions for the company
	// (GitLab or Mattermost under provisioning Engine, Datadog, an Atlassian
	// organization) is never given here: the engine hands the seat's own to
	// that vendor's server itself, and refuses one given here, which the
	// engine's dry run reports as Invalid on the Seat. A credential given
	// here is the seat's alone: Crewlet refuses a Secret key another seat or
	// an Integration names too. Crewlet's `mcp_env`, a credential position.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	MCP []MCPCredentials `json:"mcp,omitempty"`

	// sandbox is the seat's own settings for its executor's box, which runs
	// whether or not it is set. Unset takes the Company's spec.sandbox alone.
	// Agent seats only. Crewlet's `sandbox`.
	// +optional
	Sandbox SeatSandbox `json:"sandbox,omitzero"`

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

	// stopRunning ends the seat's running work when it is paused, rather
	// than letting it finish: the turn it is on at its next round, and its
	// executor run, launching or running, at once, its tool bridge closed,
	// what it spent charged and its box reclaimed. What was stopped is not
	// run again, and the engine records the stop as the operator's. A run
	// parked on a person's question stays parked behind the pause. Only with
	// paused true. Setting it on a seat already paused stops its running
	// work then; unsetting it changes nothing, since a stopped turn stays
	// stopped. Crewlet's pause `stop_running`.
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

	// contact is how agents reach the person in a Human seat on a vendor's
	// chat surface or tool, and how what the person does there reaches the
	// engine as theirs. It applies to Human seats only, and is optional: a
	// person who works only through the engine's dashboard has no account to
	// name, and signs in on the seat through the engine's own identity
	// directory. On a company that talks on Slack or Mattermost (an
	// Integration of either, or a seat's Slack app) the engine warns of a
	// Human seat with no account there, a slackUserID or a
	// mattermostUsername, since nothing addressed to it there reaches
	// anybody; a GitHub, GitLab or Atlassian account does not count, and the
	// operator records the warning as an EngineWarnings event on the
	// Company. On Chatlet, which a company with neither talks on, the person
	// reads the rooms on the dashboard as their seat, and nothing is warned.
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
	// pausedBy is who paused the seat, as Crewlet records the author of a
	// write: the seat of the person who paused it, or token:<id> for a
	// token no seat is bound to. A pause the operator took reads
	// token:operator.
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
	// handle, the email or a contact account; Conflict names the other),
	// ResolvedRefs (the unit, the providers, the tool servers and every
	// Secret key exist), Applied (the seat is in the revision the engine
	// runs), and on an Agent seat Ready and, once the engine runs it, Paused
	// (whether the engine holds it paused, and whether that is what
	// spec.paused asks). Ready is True, LeaseHeld, while an engine node holds
	// the seat's lease and runs it, naming the provider its executor runs on
	// and where; False while none does (Unplaced, or PoolNotFound), and
	// False whoever holds it while its executor can run no turn as its spec
	// places it: NotExposed or SandboxLoginNotPortable.
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
