package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkingStatus is when a seat shows that it is working on a chat message. It
// maps onto Crewlet's typing_status (Always is `always`, Addressed is
// `addressed`).
// +kubebuilder:validation:Enum=Always;Addressed
type WorkingStatus string

// The working-status modes.
const (
	// WorkingStatusAlways shows it on every turn a chat message started.
	WorkingStatusAlways WorkingStatus = "Always"
	// WorkingStatusAddressed shows it only when a person is plausibly
	// waiting on the seat: a direct message, a mention of it by name, and
	// on Slack or Mattermost a thread it is in because it was named there,
	// on Chatlet a reply in a thread it started or a person's message in a
	// unit's room that reached nobody else and so falls to it as the unit's
	// lead. Never a message it reached through a room-wide address, or only
	// by having spoken in the thread before.
	WorkingStatusAddressed WorkingStatus = "Addressed"
)

// StatusPhrases replaces the words a seat's working indicator shows. Each line
// completes the sentence "<seat> is …", and one line is picked per phase.
// Each list maps onto the same-named list of Crewlet's status_phrases.
// +kubebuilder:validation:MinProperties=1
type StatusPhrases struct {
	// onboarding is shown during a seat's first-turn onboarding pass.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	Onboarding []string `json:"onboarding,omitempty"`

	// execute is shown while the seat works.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	Execute []string `json:"execute,omitempty"`

	// review is shown while the reviewer judges the seat's work.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	Review []string `json:"review,omitempty"`

	// default is shown in any phase without lines of its own.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	Default []string `json:"default,omitempty"`
}

// SlackIntegration turns Slack on for a company. It maps onto Crewlet's
// integrations.slack. Slack has no company-wide credential: every agent seat
// that speaks on Slack has its own app, in its Seat's spec.integrations.slack.
type SlackIntegration struct {
	// typingStatus is when a seat shows that it is working. Unset takes
	// Crewlet's default, Always. Crewlet's `typing_status`.
	// +optional
	TypingStatus WorkingStatus `json:"typingStatus,omitempty"`

	// statusPhrases replaces the words the working indicator shows.
	// Crewlet's `status_phrases`.
	// +optional
	StatusPhrases StatusPhrases `json:"statusPhrases,omitzero"`
}

// ContainerWebhookMode is how webhooks are registered at GitHub: once on the
// organization, or once per repository. It maps onto Crewlet's org_webhook
// (Auto is `auto`, Require is `true`, Never is `false`).
// +kubebuilder:validation:Enum=Auto;Require;Never
type ContainerWebhookMode string

// The container webhook modes.
const (
	// ContainerWebhookAuto uses one organization hook where the token may
	// create one, and per-repository hooks otherwise.
	ContainerWebhookAuto ContainerWebhookMode = "Auto"
	// ContainerWebhookRequire demands an organization hook.
	ContainerWebhookRequire ContainerWebhookMode = "Require"
	// ContainerWebhookNever always registers per-repository hooks.
	ContainerWebhookNever ContainerWebhookMode = "Never"
)

// GitHubProvisioning is where the engine registers the company's webhooks at
// GitHub. It maps onto Crewlet's integrations.github.provisioning. The engine
// registers them with the organization token (tokenSecretRef); without one it
// registers nothing and each seat's own app delivers its own webhooks.
// +kubebuilder:validation:MinProperties=1
type GitHubProvisioning struct {
	// org is the GitHub organization holding the repositories. Crewlet's
	// `org`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	Org string `json:"org,omitempty"`

	// repos are repositories, as owner/name, to register a webhook on one by
	// one. Crewlet's `repos`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:items:MaxLength=201
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`
	Repos []string `json:"repos,omitempty"`

	// orgWebhook is whether one organization hook replaces the
	// per-repository ones. Unset is Auto. Crewlet's `org_webhook`.
	// +optional
	OrgWebhook ContainerWebhookMode `json:"orgWebhook,omitempty"`
}

// GitHubIntegration turns GitHub on for a company: webhooks from github.com or
// a GitHub Enterprise Server wake the seats they concern. It maps onto
// Crewlet's integrations.github. Each agent seat's own GitHub App is in its
// Seat's spec.integrations.github; the GitHub tools are an MCPServer.
// +kubebuilder:validation:XValidation:rule="!has(self.provisioning) || has(self.tokenSecretRef)",message="provisioning needs tokenSecretRef: without an organization token the engine registers no webhooks"
type GitHubIntegration struct {
	// url is the base URL of a GitHub Enterprise Server, such as
	// https://github.example.com. Unset is github.com. Crewlet's `url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && self.startsWith('https://')",message="url must be an absolute URL starting with https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// webhookSecretRef is the Secret key holding the secret that verifies
	// webhooks delivered to the company-wide route. Crewlet's
	// `webhook_secret`, a credential position.
	// +required
	WebhookSecretRef SecretKeyReference `json:"webhookSecretRef,omitzero"`

	// tokenSecretRef is the Secret key holding an organization token with
	// the admin:org_hook scope, which the engine registers the company's
	// webhooks with. Unset registers none. Crewlet's `token`, a credential
	// position.
	// +optional
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef,omitzero"`

	// provisioning is where the webhooks are registered. It requires
	// tokenSecretRef, without which the engine registers nothing.
	// +optional
	Provisioning GitHubProvisioning `json:"provisioning,omitzero"`
}

// IntegrationProvisioning is who creates the seats' accounts at a vendor, and
// so where their credentials come from.
// +kubebuilder:validation:Enum=External;Engine
type IntegrationProvisioning string

// The provisioning modes.
const (
	// IntegrationProvisioningExternal leaves the seats' accounts to someone
	// outside the engine (a person, a platform): each seat's credentials are
	// Secrets its Seat references, and the engine is given no administrator
	// credential, so it creates, changes and removes nothing at the vendor.
	IntegrationProvisioningExternal IntegrationProvisioning = "External"
	// IntegrationProvisioningEngine has the engine create each agent seat's
	// account with an administrator credential the Integration references,
	// mint the account's credential into the engine's secret store, register
	// the company's webhooks, and heal all of it on a schedule. Seats name
	// the minted credentials as provisioned values.
	IntegrationProvisioningEngine IntegrationProvisioning = "Engine"
)

// GitLabAccountOwner is where the engine creates the seats' GitLab service
// accounts. It maps onto Crewlet's provisioning mode (Group is `group`,
// Instance is `instance`).
// +kubebuilder:validation:Enum=Group;Instance
type GitLabAccountOwner string

// The GitLab account owners.
const (
	// GitLabAccountOwnerGroup creates them under the group, which is all
	// GitLab.com offers.
	GitLabAccountOwnerGroup GitLabAccountOwner = "Group"
	// GitLabAccountOwnerInstance creates them on a self-managed instance
	// itself, with an instance administrator's token.
	GitLabAccountOwnerInstance GitLabAccountOwner = "Instance"
)

// GitLabEngineProvisioning is how the engine provisions the seats' GitLab
// accounts under provisioning Engine. It maps onto Crewlet's
// integrations.gitlab.provisioning: for each agent Seat whose
// spec.integrations.gitlab is enabled, the engine creates a service account,
// makes it a member of the group, or of the seat's projects, at the seat's
// role, mints its token into an entry of its secret store it names for the
// seat, and registers the webhook. Each seat's role and projects are on its
// Seat.
type GitLabEngineProvisioning struct {
	// group is the top-level group, by its path, the service accounts join
	// and the webhook is registered on. Crewlet's `group`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.][A-Za-z0-9_./-]*$`
	Group string `json:"group,omitempty"`

	// adminTokenSecretRef is the Secret key holding a personal access token
	// with the api scope of an Owner of the group (of an instance
	// administrator, for accountOwner Instance), which creates and removes
	// the service accounts. Crewlet's `admin_token`, a credential position.
	// +required
	AdminTokenSecretRef SecretKeyReference `json:"adminTokenSecretRef,omitzero"`

	// accountOwner is where the service accounts are created. Unset is
	// Group. Crewlet's `mode`.
	// +optional
	AccountOwner GitLabAccountOwner `json:"accountOwner,omitempty"`

	// usernamePrefix is put before each seat's handle to form its account's
	// username, for a group shared with people. Crewlet's
	// `username_prefix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+$`
	UsernamePrefix string `json:"usernamePrefix,omitempty"`

	// projects are the projects, by path, a seat's spec.integrations.gitlab
	// projects may name, and the ones the webhook is registered on one by
	// one when no group hook is. Crewlet's `projects`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=255
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9_.][A-Za-z0-9_./-]*$`
	Projects []string `json:"projects,omitempty"`

	// groupWebhook is whether one group hook replaces the per-project ones.
	// Unset is Auto. Crewlet's `group_webhook`.
	// +optional
	GroupWebhook ContainerWebhookMode `json:"groupWebhook,omitempty"`
}

// GitLabIntegration turns GitLab on for a company: webhooks from a GitLab
// instance wake the seats they concern. It maps onto Crewlet's
// integrations.gitlab. Each seat's GitLab account and access are in its
// Seat's spec.integrations.gitlab.
type GitLabIntegration struct {
	// url is the instance's base URL, such as https://gitlab.com. Crewlet's
	// `url`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="url must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// signingSecretRef is the Secret key holding the signing token every
	// webhook delivery is verified against: whsec_ followed by the standard
	// base64 of 32 bytes, the only form GitLab signs with (its "Generate
	// signing token" button makes one). Crewlet's `signing_secret`, a
	// credential position.
	// +required
	SigningSecretRef SecretKeyReference `json:"signingSecretRef,omitzero"`

	// tokenSecretRef is the Secret key holding a personal access token with
	// the read_api scope, which the engine reads an issue's or merge
	// request's participants with, so that everyone in a thread hears its
	// activity. Unset routes to the people a delivery names only. Crewlet's
	// `token`, a credential position.
	// +optional
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef,omitzero"`

	// webhookName is the name of the hooks the engine registers, which is
	// how it tells its own from others on the instance: two deployments
	// watching one instance need two names. Unset is crewlet. Crewlet's
	// `webhook_name`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="webhookName may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	WebhookName string `json:"webhookName,omitempty"`

	// engineProvisioning is how the engine creates the seats' accounts.
	// Required with provisioning Engine, and not allowed with External.
	// +optional
	EngineProvisioning GitLabEngineProvisioning `json:"engineProvisioning,omitzero"`
}

// MattermostEngineProvisioning is how the engine provisions the seats'
// Mattermost bots under provisioning Engine. It maps onto Crewlet's
// integrations.mattermost.provisioning: for each agent seat whose
// spec.integrations.mattermost.botToken is provisioned, the engine creates a
// bot, adds it to the team and the channels, and mints its token into that
// value.
type MattermostEngineProvisioning struct {
	// adminTokenSecretRef is the Secret key holding a system
	// administrator's personal access token, which creates and disables the
	// bots. Crewlet's `admin_token`, a credential position.
	// +required
	AdminTokenSecretRef SecretKeyReference `json:"adminTokenSecretRef,omitzero"`

	// usernamePrefix is put before each seat's handle to form its bot's
	// username, for a server shared with people. Crewlet's
	// `username_prefix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern=`^[a-z0-9._-]+$`
	UsernamePrefix string `json:"usernamePrefix,omitempty"`

	// channels are channels, by name (the URL slug), every bot joins: a bot
	// hears only the channels it is in. Crewlet's `channels`.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=64
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9][a-z0-9_-]*$`
	Channels []string `json:"channels,omitempty"`

	// displayNameSuffix is put after each bot's display name, such as
	// " (AI)", to tell agents from people at a glance. Crewlet's
	// `display_name_suffix`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="displayNameSuffix may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	DisplayNameSuffix string `json:"displayNameSuffix,omitempty"`
}

// MattermostIntegration turns Mattermost on for a company. It maps onto
// Crewlet's integrations.mattermost. Mattermost delivers no webhook the engine
// can use: the engine holds a websocket per seat's bot instead, so the
// integration needs no public address. Each seat's bot is in its Seat's
// spec.integrations.mattermost.
type MattermostIntegration struct {
	// url is the address the engine reaches the server at, such as
	// https://chat.example.com: each seat's websocket, the REST calls and
	// the bots' provisioning go to it. It need not be the server's Site URL,
	// which people browse and from which Mattermost builds its links; the
	// engine builds none from url. Any route to the server serves, such as
	// its in-cluster Service: the engine's websockets carry no Origin
	// header, so Mattermost's origin check, which holds a browser to the
	// Site URL, never applies to them. The server has to answer its API at
	// url without a redirect, which neither the engine nor the seats'
	// Mattermost tool server, given the same address as its MATTERMOST_URL,
	// follows: on the scheme that address is served on, and at the Site
	// URL's path, outside which Mattermost redirects every request.
	// Crewlet's `url`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="url must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// team is the team, by name (the URL slug), the bots belong to.
	// Crewlet's `team`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9_-]*$`
	Team string `json:"team,omitempty"`

	// typingStatus is when a seat shows that it is typing. Unset takes
	// Crewlet's default, Always; each indicator costs a request every few
	// seconds while the seat works. Crewlet's `typing_status`.
	// +optional
	TypingStatus WorkingStatus `json:"typingStatus,omitempty"`

	// engineProvisioning is how the engine creates the seats' bots.
	// Required with provisioning Engine, and not allowed with External.
	// +optional
	EngineProvisioning MattermostEngineProvisioning `json:"engineProvisioning,omitzero"`
}

// JiraIntegration turns Jira on for a company: webhooks from a Jira site wake
// the seats they concern, and the company may run Jira as its tracker (the
// Company's spec.tracker.backend Jira). It maps onto Crewlet's
// integrations.jira. Each seat's Jira identity is its own token, a credential
// of the MCPServer named atlassian (or jira) in its Seat's spec.mcp.
// +kubebuilder:validation:XValidation:rule="!(has(self.url) && has(self.cloudID))",message="url and cloudID are two ways to name one site: give one"
type JiraIntegration struct {
	// url is a Data Center instance's or a Cloud site's base URL. Unset
	// with cloudID unset, the company's Atlassian Integration discovers the
	// site. Crewlet's `url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="url must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// cloudID is an Atlassian Cloud site's id; requests go through the API
	// gateway of the Cloud siteURL is on: Atlassian Government Cloud's,
	// api.atlassian-us-gov-mod.com, for a site under
	// atlassian-us-gov-mod.net, and api.atlassian.com otherwise. Crewlet's
	// `cloud_id`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	CloudID string `json:"cloudID,omitempty"`

	// siteURL is the site's address as a browser opens it, for the links
	// the engine hands people; needed with cloudID, whose gateway address
	// opens nothing in a browser, and which is read through the gateway of
	// this site's Cloud. Unset is url. Crewlet's `site_url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="siteURL must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="siteURL may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="siteURL may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	SiteURL string `json:"siteURL,omitempty"`

	// email is the account's address, for Jira Cloud's basic
	// authentication. Unset sends the token as a bearer token, as a service
	// account and a Data Center personal access token want. Crewlet's
	// `email`.
	// +optional
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=254
	// +kubebuilder:validation:Pattern=`^[^@\s]+@[^@\s]+$`
	Email string `json:"email,omitempty"`

	// tokenSecretRef is the Secret key holding the API token (Cloud) or
	// personal access token (Data Center) of an administrator account. The
	// engine reads an issue's watchers with it, which no webhook carries,
	// and registers the company's webhook. Crewlet's `token`, a credential
	// position.
	// +required
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef,omitzero"`

	// webhookSecretRef is the Secret key holding the secret webhook
	// deliveries are signed with. Required for a Data Center instance, whose
	// route answers none without it; on Cloud, webhooks the engine
	// registers are signed with it, and the Forge relay needs none.
	// Crewlet's `webhook_secret`, a credential position.
	// +optional
	WebhookSecretRef SecretKeyReference `json:"webhookSecretRef,omitzero"`

	// webhookName is the name of the webhook the engine registers, which is
	// how it tells its own from others on the site: two deployments
	// watching one site need two names. Unset is crewlet. Crewlet's
	// `webhook_name`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="webhookName may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	WebhookName string `json:"webhookName,omitempty"`
}

// ConfluenceIntegration turns Confluence on for a company: page changes wake
// the seats they concern, and the company may run Confluence as its knowledge
// base (the Company's spec.knowledge.backend Confluence). It maps onto
// Crewlet's integrations.confluence. Each seat's Confluence identity is its
// own token, a credential of the MCPServer named atlassian in its Seat's
// spec.mcp.
// +kubebuilder:validation:XValidation:rule="!(has(self.url) && has(self.cloudID))",message="url and cloudID are two ways to name one site: give one"
type ConfluenceIntegration struct {
	// url is a Data Center instance's or a Cloud site's base URL. Unset
	// with cloudID unset, the company's Atlassian Integration discovers the
	// site. Crewlet's `url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="url must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="url may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	URL string `json:"url,omitempty"`

	// cloudID is an Atlassian Cloud site's id; requests go through the API
	// gateway of the Cloud siteURL is on: Atlassian Government Cloud's,
	// api.atlassian-us-gov-mod.com, for a site under
	// atlassian-us-gov-mod.net, and api.atlassian.com otherwise. Crewlet's
	// `cloud_id`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	CloudID string `json:"cloudID,omitempty"`

	// siteURL is the site's address as a browser opens it, for the links
	// the engine hands people; needed with cloudID, which is read through
	// the gateway of this site's Cloud. Unset is url. Crewlet's
	// `site_url`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (self.startsWith('http://') || self.startsWith('https://'))",message="siteURL must be an absolute URL starting with http:// or https://"
	// +kubebuilder:validation:XValidation:rule="!self.matches('^[^:/?#]+://[^/?#]*@')",message="siteURL may not hold a user name or password: a credential is named through a Secret, never written into an address"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="siteURL may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	SiteURL string `json:"siteURL,omitempty"`

	// email is the account's address, for Confluence Cloud's basic
	// authentication. Unset sends the token as a bearer token. Crewlet's
	// `email`.
	// +optional
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=254
	// +kubebuilder:validation:Pattern=`^[^@\s]+@[^@\s]+$`
	Email string `json:"email,omitempty"`

	// tokenSecretRef is the Secret key holding the API token (Cloud) or
	// personal access token (Data Center) of the account a seat without a
	// Confluence credential of its own searches as, which also reads the
	// tool-skill pages. Crewlet's `token`, a credential position.
	// +required
	TokenSecretRef SecretKeyReference `json:"tokenSecretRef,omitzero"`

	// webhookSecretRef is the Secret key holding the secret a Data Center
	// instance signs webhook deliveries with. Required for Data Center.
	// Crewlet's `webhook_secret`, a credential position.
	// +optional
	WebhookSecretRef SecretKeyReference `json:"webhookSecretRef,omitzero"`

	// webhookTokenSecretRef is the Secret key holding the token a Confluence
	// Cloud webhook carries in its delivery URL, Cloud signing nothing: at
	// least 26 characters, random. Crewlet's `webhook_token`, a credential
	// position.
	// +optional
	WebhookTokenSecretRef SecretKeyReference `json:"webhookTokenSecretRef,omitzero"`
}

// DatadogSite is the Datadog region a company's organization is in. It maps
// onto Crewlet's provisioning.site, the region's host name (US1 is
// datadoghq.com, US3 us3.datadoghq.com, US5 us5.datadoghq.com, EU1
// datadoghq.eu, AP1 ap1.datadoghq.com, AP2 ap2.datadoghq.com, US1FED
// ddog-gov.com).
// +kubebuilder:validation:Enum=US1;US3;US5;EU1;AP1;AP2;US1FED
type DatadogSite string

// The Datadog sites.
const (
	DatadogSiteUS1    DatadogSite = "US1"
	DatadogSiteUS3    DatadogSite = "US3"
	DatadogSiteUS5    DatadogSite = "US5"
	DatadogSiteEU1    DatadogSite = "EU1"
	DatadogSiteAP1    DatadogSite = "AP1"
	DatadogSiteAP2    DatadogSite = "AP2"
	DatadogSiteUS1FED DatadogSite = "US1FED"
)

// DatadogRouteTo is where an alert goes whose monitor names no seat.
// +kubebuilder:validation:XValidation:rule="has(self.seatRef) != (has(self.dismiss) && self.dismiss)",message="give exactly one of seatRef and dismiss: true"
// +kubebuilder:validation:MinProperties=1
type DatadogRouteTo struct {
	// seatRef names the agent Seat, in this namespace, the alert wakes.
	// Crewlet's `route_to`, as the seat's handle.
	// +optional
	SeatRef LocalObjectReference `json:"seatRef,omitzero"`

	// dismiss wakes nobody: only monitors that name a seat reach an agent,
	// and the rest stay with whatever Datadog already does about them.
	// Crewlet's `route_to: none`.
	// +optional
	Dismiss *bool `json:"dismiss,omitempty"`
}

// DatadogIntegration turns Datadog on for a company: a firing monitor wakes
// the seat its tags name. It maps onto Crewlet's integrations.datadog. The
// engine registers the webhook at Datadog itself and creates a service
// account for each agent Seat whose spec.integrations.datadog is enabled,
// with the role that block names, so the Integration's provisioning is
// Engine.
type DatadogIntegration struct {
	// webhookTokenSecretRef is the Secret key holding the token every
	// delivery carries in its X-Crewlet-Token header: at least 26
	// characters, random. Datadog signs nothing, so this token is the whole
	// of the check. Crewlet's `webhook_token`, a credential position.
	// +required
	WebhookTokenSecretRef SecretKeyReference `json:"webhookTokenSecretRef,omitzero"`

	// routeTo is where an alert goes whose monitor names no seat.
	// Crewlet's `route_to`.
	// +required
	RouteTo DatadogRouteTo `json:"routeTo,omitzero"`

	// site is the region the organization, and so its keys, are in.
	// Crewlet's `provisioning.site`.
	// +required
	Site DatadogSite `json:"site,omitempty"`

	// apiKeySecretRef is the Secret key holding an API key of the
	// organization, which says which organization a call is for. Crewlet's
	// `provisioning.api_key`, a credential position.
	// +required
	APIKeySecretRef SecretKeyReference `json:"apiKeySecretRef,omitzero"`

	// appKeySecretRef is the Secret key holding an application key, which
	// says which user acts: one allowed to manage webhooks and service
	// accounts. Crewlet's `provisioning.app_key`, a credential position.
	// +required
	AppKeySecretRef SecretKeyReference `json:"appKeySecretRef,omitzero"`

	// emailDomain is the domain the service accounts' addresses are built
	// under; Datadog requires an address and never writes to it. Unset
	// takes Crewlet's default, agents.crewlet.invalid. Crewlet's
	// `provisioning.email_domain`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`
	EmailDomain string `json:"emailDomain,omitempty"`

	// webhookName is the name of the webhook the engine keeps at Datadog,
	// and so the handle monitors write: @webhook-<name>. Unset is crewlet.
	// Crewlet's `webhook_name`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[^ @,]+$`
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="webhookName may not hold a ${NAME} reference: the operator refuses every one it did not write, since in many fields the engine resolves one from its own secret store and environment, which hold credentials that are not this object's to name"
	WebhookName string `json:"webhookName,omitempty"`

	// handleTag is the monitor tag key whose value names the seat an alert
	// wakes, such as crewlet:sre-lead. Unset is crewlet. Crewlet's
	// `handle_tag`.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9_./-]*$`
	HandleTag string `json:"handleTag,omitempty"`
}

// AtlassianIntegration connects a company's Atlassian Cloud organization, in
// which the engine creates a service account for each agent seat, grants it
// the company's Jira and Confluence sites, and mints its API token. It maps
// onto Crewlet's integrations.atlassian. Each seat names the minted token and
// the account's address as provisioned values of its MCPServer named
// atlassian, so the Integration's provisioning is Engine. A Data Center
// deployment has no organization; its accounts are made by hand and each
// seat's token comes from a Secret.
type AtlassianIntegration struct {
	// orgID is the organization's id, from admin.atlassian.com. Crewlet's
	// `org_id`.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	OrgID string `json:"orgID,omitempty"`

	// apiKeySecretRef is the Secret key holding an organization API key
	// created without scopes: Atlassian refuses a scoped key the call that
	// creates an account. Crewlet's `api_key`, a credential position.
	// +required
	APIKeySecretRef SecretKeyReference `json:"apiKeySecretRef,omitzero"`
}

// IntegrationSpec connects a company to one vendor. Exactly one vendor member
// is set; the Integration's kind of vendor cannot change, and a company has at
// most one Integration per vendor (a second is reported as a conflict).
//
// provisioning says who creates the seats' accounts at the vendor. Under
// External (the default) someone else does, and every per-seat credential
// comes from a Secret its Seat references; the Integration then holds no
// administrator credential. Under Engine the engine creates them, from the
// administrator credential the vendor member references, and seats name the
// credentials it mints as provisioned values. The engine can create accounts
// at GitLab, Mattermost, Datadog and Atlassian; Datadog and Atlassian are
// always provisioned by the engine, and Slack, GitHub, Jira and Confluence
// never are (the engine mints no credential there).
// +kubebuilder:validation:XValidation:rule="[has(self.slack), has(self.github), has(self.gitlab), has(self.mattermost), has(self.jira), has(self.confluence), has(self.datadog), has(self.atlassian)].filter(x, x).size() == 1",message="exactly one vendor (slack, github, gitlab, mattermost, jira, confluence, datadog or atlassian) must be set"
// +kubebuilder:validation:XValidation:rule="has(self.slack) == has(oldSelf.slack) && has(self.github) == has(oldSelf.github) && has(self.gitlab) == has(oldSelf.gitlab) && has(self.mattermost) == has(oldSelf.mattermost) && has(self.jira) == has(oldSelf.jira) && has(self.confluence) == has(oldSelf.confluence) && has(self.datadog) == has(oldSelf.datadog) && has(self.atlassian) == has(oldSelf.atlassian)",message="an Integration's vendor cannot change; create another Integration"
// +kubebuilder:validation:XValidation:rule="!has(self.provisioning) || self.provisioning != 'Engine' || has(self.gitlab) || has(self.mattermost) || has(self.datadog) || has(self.atlassian)",message="provisioning Engine applies to gitlab, mattermost, datadog and atlassian: the engine creates no account and mints no credential at Slack, GitHub, Jira or Confluence"
// +kubebuilder:validation:XValidation:rule="!(has(self.datadog) || has(self.atlassian)) || (has(self.provisioning) && self.provisioning == 'Engine')",message="datadog and atlassian need provisioning Engine: the engine creates the seats' accounts with the keys they reference whenever it holds them"
// +kubebuilder:validation:XValidation:rule="!has(self.gitlab) || has(self.gitlab.engineProvisioning) == (has(self.provisioning) && self.provisioning == 'Engine')",message="gitlab.engineProvisioning is required with provisioning Engine and not allowed otherwise: it holds the administrator token the engine creates the seats' accounts with"
// +kubebuilder:validation:XValidation:rule="!has(self.mattermost) || has(self.mattermost.engineProvisioning) == (has(self.provisioning) && self.provisioning == 'Engine')",message="mattermost.engineProvisioning is required with provisioning Engine and not allowed otherwise: it holds the administrator token the engine creates the seats' bots with"
type IntegrationSpec struct {
	// companyRef names the Company this integration belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// provisioning is who creates the seats' accounts at the vendor. Unset
	// is External.
	// +optional
	Provisioning IntegrationProvisioning `json:"provisioning,omitempty"`

	// slack connects Slack.
	// +optional
	Slack *SlackIntegration `json:"slack,omitempty"`

	// github connects GitHub.
	// +optional
	GitHub GitHubIntegration `json:"github,omitzero"`

	// gitlab connects GitLab.
	// +optional
	GitLab GitLabIntegration `json:"gitlab,omitzero"`

	// mattermost connects Mattermost.
	// +optional
	Mattermost MattermostIntegration `json:"mattermost,omitzero"`

	// jira connects Jira.
	// +optional
	Jira JiraIntegration `json:"jira,omitzero"`

	// confluence connects Confluence.
	// +optional
	Confluence ConfluenceIntegration `json:"confluence,omitzero"`

	// datadog connects Datadog.
	// +optional
	Datadog DatadogIntegration `json:"datadog,omitzero"`

	// atlassian connects an Atlassian Cloud organization.
	// +optional
	Atlassian AtlassianIntegration `json:"atlassian,omitzero"`
}

// IntegrationStatus is what the operator observed about an Integration.
// +kubebuilder:validation:MinProperties=1
type IntegrationStatus struct {
	// conditions are Accepted (the Company exists and has no other
	// Integration for the vendor), ResolvedRefs (every Secret key exists),
	// Applied (the integration is in the revision the engine runs) and
	// Healthy (the engine's own check of the vendor passes).
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the metadata.generation this status describes.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// webhookURL is the company-wide URL the vendor delivers webhooks to,
	// when the company is exposed and the vendor has one:
	// <publicBaseURL>/webhooks/<vendor> for GitHub, GitLab, Jira,
	// Confluence (a Data Center instance's; Confluence Cloud delivers to
	// <webhookURL>/<event>, a hook the engine registers per event) and
	// Datadog. Slack, Mattermost and Atlassian have none; Slack's and
	// GitHub's per-seat URLs are in each Seat's status.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	WebhookURL string `json:"webhookURL,omitempty"`
}

// Integration connects a company to a vendor (Slack, GitHub, GitLab,
// Mattermost, Jira, Confluence, Datadog or an Atlassian organization): the
// company-wide settings, the credentials that verify the vendor's webhooks,
// and, where the engine creates the seats' accounts, the administrator
// credential it creates them with. Credentials that belong to one seat, such
// as its own Slack app or its GitLab token, are on the Seat.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=intg,categories=crewlet
// +kubebuilder:printcolumn:name="Company",type=string,JSONPath=`.spec.companyRef.name`
// +kubebuilder:printcolumn:name="Applied",type=string,JSONPath=`.status.conditions[?(@.type=="Applied")].status`
// +kubebuilder:printcolumn:name="Healthy",type=string,JSONPath=`.status.conditions[?(@.type=="Healthy")].status`
// +kubebuilder:printcolumn:name="Webhook",type=string,JSONPath=`.status.webhookURL`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Integration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the integration's desired state.
	// +required
	Spec IntegrationSpec `json:"spec,omitzero"`

	// status is the integration's observed state.
	// +optional
	Status IntegrationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// IntegrationList contains a list of Integration.
type IntegrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Integration `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Integration{}, &IntegrationList{})
		return nil
	})
}
