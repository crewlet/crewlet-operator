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
	// waiting: a direct message, a mention, or a thread the seat follows.
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
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https'",message="url must be an absolute https URL"
	// +kubebuilder:validation:XValidation:rule="!self.matches('[$][{][A-Za-z_][A-Za-z0-9_]*[}]')",message="url may not hold a ${NAME} reference: the engine would resolve it from its own secret store and environment, which hold credentials that are not this object's to name"
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

// IntegrationSpec connects a company to one vendor. Exactly one vendor member
// is set; the Integration's kind of vendor cannot change, and a company has at
// most one Integration per vendor (a second is reported as a conflict).
//
// For both vendors, every per-seat credential (a seat's Slack app, its GitHub
// App) comes from a Secret its Seat references, whoever created the app: the
// engine mints no credential for Slack or GitHub. So an Integration has no
// switch between credentials the platform provides and credentials the engine
// provisions; one arrives with the vendors whose apps the engine can create
// (GitLab, Mattermost).
//
// v1alpha1 has the members slack and github. Crewlet's other integrations
// arrive as further members of this union, each with a reference field per
// credential. Until then their credential positions cannot be set at all:
// integrations.gitlab (signing_secret, token, provisioning.admin_token),
// integrations.mattermost.provisioning.admin_token and a seat's
// integrations.mattermost.bot_token, integrations.jira (token,
// webhook_secret), integrations.confluence (token, webhook_secret,
// webhook_token), integrations.datadog (webhook_token, provisioning.api_key,
// provisioning.app_key) and integrations.atlassian.api_key.
// +kubebuilder:validation:XValidation:rule="[has(self.slack), has(self.github)].filter(x, x).size() == 1",message="exactly one vendor (slack or github) must be set"
// +kubebuilder:validation:XValidation:rule="has(self.slack) == has(oldSelf.slack) && has(self.github) == has(oldSelf.github)",message="an Integration's vendor cannot change; create another Integration"
type IntegrationSpec struct {
	// companyRef names the Company this integration belongs to. Immutable.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="companyRef is immutable"
	CompanyRef CompanyReference `json:"companyRef,omitzero"`

	// slack connects Slack.
	// +optional
	Slack *SlackIntegration `json:"slack,omitempty"`

	// github connects GitHub.
	// +optional
	GitHub GitHubIntegration `json:"github,omitzero"`
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
	// when the company is exposed and the vendor has one. Per-seat URLs are
	// in each Seat's status.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	WebhookURL string `json:"webhookURL,omitempty"`
}

// Integration connects a company to a vendor (Slack, GitHub): the company-wide
// settings and the credentials that verify the vendor's webhooks. Credentials
// that belong to one seat, such as its own Slack app, are on the Seat.
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
