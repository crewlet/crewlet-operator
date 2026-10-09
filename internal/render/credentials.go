package render

// The credential positions of Crewlet's two documents, and what fills each.
//
// Crewlet marks every position that holds a credential "x-crewlet-secret":
// true in the JSON Schema `crewlet schema company` and `crewlet schema
// bootstrap` print, from the same declaration that drives its redaction.
// This table is the operator's answer to each mark: the crewlet.ai field that
// fills the position with a reference to a Secret, the Secret the operator
// makes itself, or why v1alpha1 offers no way to fill it. A test runs the
// pinned engine's schema and holds the table to its marks in both directions,
// so a credential position Crewlet adds fails the build until it is answered
// here, and one it stops marking fails until its row goes; the same test
// holds docs/concepts/credentials.md to the table, and the CRDs to every
// field it names. A hand-kept list was what this replaced, and nothing told
// it when the engine moved.

// CredentialTier is the document a credential position is in.
type CredentialTier string

// The two documents.
const (
	// TierCompany is the company document (Crewlet's Tier B), rendered by
	// Company and applied through the engine's API.
	TierCompany CredentialTier = "company"
	// TierBootstrap is a data pod's crewlet.yaml (Crewlet's Tier A),
	// rendered by Bootstrap and kept in a Secret.
	TierBootstrap CredentialTier = "bootstrap"
)

// CredentialPosition is one position Crewlet marks as a credential.
type CredentialPosition struct {
	Tier CredentialTier
	// Path is the position as Crewlet's schema places it: keys joined by
	// dots, `[]` for every element of a list and `*` for every value of a
	// map. A role's positions are written from `roles[]` wherever the org
	// chart puts the role, `units[].roles[]` included, since a Seat fills
	// them the same way in either place.
	Path string
	// Sources fill the position; more than one when it is filled in more
	// than one way. Empty when Unsupported says why nothing does.
	Sources []CredentialSource
	// Unsupported says why v1alpha1 offers no way to fill the position.
	Unsupported string
}

// CredentialSource is one way a position is filled.
type CredentialSource struct {
	// Field is the crewlet.ai field that references the Secret, as
	// Kind.path with `[]` for every element of a list; empty for a Secret
	// the operator makes itself.
	Field string
	// Reference says which Secret, and which of its keys, holds the value.
	Reference string
	// When qualifies the source, for a position filled in more than one
	// way.
	When string
}

// Why the integrations v1alpha1 does not offer have no field.
const (
	notOfferedGitLab     = "The GitLab integration is not offered yet."
	notOfferedMattermost = "The Mattermost integration is not offered yet."
	notOfferedJira       = "The Jira integration is not offered yet; so the tracker backend is Native or None."
	notOfferedConfluence = "The Confluence integration is not offered yet; so the knowledge backend is Native or None."
	notOfferedDatadog    = "The Datadog integration is not offered yet."
	notOfferedAtlassian  = "The Atlassian organization integration is not offered yet."
	notOfferedCLIAgent   = "The `cli-agent` provider type is not offered: the engine image ships no coding CLI, " +
		"and a subscription login belongs to one machine, which a pod that can be rescheduled at any time is not."
	notOfferedStreamToken = "The data pod runs Crewlet's embedded broker, which needs no token; an external NATS " +
		"cluster (`stream.type: nats`), where Crewlet reads one, is not offered."
)

// The references a covered position's sources take.
const (
	perKey       = "one `SecretKeyReference` per key"
	perFile      = "one `SecretKeyReference` per file"
	perVariable  = "one `SecretKeyReference` per variable"
	perHeader    = "one `SecretKeyReference` per header"
	perEnvOrHead = "one `SecretKeyReference` per variable or header"
	oneKey       = "`SecretKeyReference`"
)

// CredentialPositions are every credential position of both documents, in
// the order docs/concepts/credentials.md lists them.
var CredentialPositions = []CredentialPosition{
	{Tier: TierCompany, Path: "providers.llm.*.api_keys[]", Sources: []CredentialSource{
		{Field: "LLMProvider.spec.apiKeySecretRefs", Reference: perKey},
		{
			Field: "EngineClass.spec.platformLLMProviders[].apiKeySecretRefs", Reference: "one `NamespacedSecretKeyReference` per key",
			When: "for a model the platform provides",
		},
	}},
	{Tier: TierCompany, Path: "providers.embeddings.api_key", Sources: []CredentialSource{
		{Field: "Company.spec.embeddings.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: TierCompany, Path: "providers.sandbox.e2b.api_key", Sources: []CredentialSource{
		{Field: "Company.spec.sandbox.e2b.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: TierCompany, Path: "providers.sandbox.setup[].files.*", Sources: []CredentialSource{
		{Field: "Company.spec.sandbox.setup[].files[].valueFrom.secretKeyRef", Reference: perFile},
	}},
	{Tier: TierCompany, Path: "providers.sandbox.setup[].env.*", Sources: []CredentialSource{
		{Field: "Company.spec.sandbox.setup[].env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: TierCompany, Path: "mcp_servers[].env.*", Sources: []CredentialSource{
		{Field: "MCPServer.spec.env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: TierCompany, Path: "mcp_servers[].headers.*", Sources: []CredentialSource{
		{Field: "MCPServer.spec.headers[].valueFrom.secretKeyRef", Reference: perHeader},
	}},
	{Tier: TierCompany, Path: "units[].mcp_env.*.*", Sources: []CredentialSource{
		{Field: "Unit.spec.mcp[].env[].valueFrom.secretKeyRef", Reference: perEnvOrHead},
	}},
	{Tier: TierCompany, Path: "roles[].mcp_env.*.*", Sources: []CredentialSource{
		{Field: "Seat.spec.mcp[].env[].valueFrom.secretKeyRef", Reference: perEnvOrHead},
	}},
	{Tier: TierCompany, Path: "roles[].sandbox.env.*", Sources: []CredentialSource{
		{Field: "Seat.spec.sandbox.env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: TierCompany, Path: "roles[].sandbox.setup[].files.*", Sources: []CredentialSource{
		{Field: "Seat.spec.sandbox.setup[].files[].valueFrom.secretKeyRef", Reference: perFile},
	}},
	{Tier: TierCompany, Path: "roles[].sandbox.setup[].env.*", Sources: []CredentialSource{
		{Field: "Seat.spec.sandbox.setup[].env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: TierCompany, Path: "roles[].integrations.slack.bot_token", Sources: []CredentialSource{
		{Field: "Seat.spec.integrations.slack.credentialsSecretRef", Reference: "`SecretReference`, key `botToken` (`SlackBotTokenKey`)"},
	}},
	{Tier: TierCompany, Path: "roles[].integrations.slack.signing_secret", Sources: []CredentialSource{
		{
			Field:     "Seat.spec.integrations.slack.credentialsSecretRef",
			Reference: "`SecretReference`, key `signingSecret` (`SlackSigningSecretKey`)",
		},
	}},
	{Tier: TierCompany, Path: "roles[].integrations.github.private_key", Sources: []CredentialSource{
		{
			Field:     "Seat.spec.integrations.github.app.credentialsSecretRef",
			Reference: "`SecretReference`, key `privateKey` (`GitHubAppPrivateKeyKey`)",
		},
	}},
	{Tier: TierCompany, Path: "roles[].integrations.github.webhook_secret", Sources: []CredentialSource{
		{
			Field:     "Seat.spec.integrations.github.app.credentialsSecretRef",
			Reference: "`SecretReference`, key `webhookSecret` (`GitHubAppWebhookSecretKey`)",
		},
	}},
	{Tier: TierCompany, Path: "integrations.github.webhook_secret", Sources: []CredentialSource{
		{Field: "Integration.spec.github.webhookSecretRef", Reference: oneKey},
	}},
	{Tier: TierCompany, Path: "integrations.github.token", Sources: []CredentialSource{
		{Field: "Integration.spec.github.tokenSecretRef", Reference: oneKey},
	}},

	{Tier: TierCompany, Path: "integrations.gitlab.signing_secret", Unsupported: notOfferedGitLab},
	{Tier: TierCompany, Path: "integrations.gitlab.token", Unsupported: notOfferedGitLab},
	{Tier: TierCompany, Path: "integrations.gitlab.provisioning.admin_token", Unsupported: notOfferedGitLab},
	{Tier: TierCompany, Path: "integrations.mattermost.provisioning.admin_token", Unsupported: notOfferedMattermost},
	{Tier: TierCompany, Path: "roles[].integrations.mattermost.bot_token", Unsupported: notOfferedMattermost},
	{Tier: TierCompany, Path: "integrations.jira.token", Unsupported: notOfferedJira},
	{Tier: TierCompany, Path: "integrations.jira.webhook_secret", Unsupported: notOfferedJira},
	{Tier: TierCompany, Path: "integrations.confluence.token", Unsupported: notOfferedConfluence},
	{Tier: TierCompany, Path: "integrations.confluence.webhook_secret", Unsupported: notOfferedConfluence},
	{Tier: TierCompany, Path: "integrations.confluence.webhook_token", Unsupported: notOfferedConfluence},
	{Tier: TierCompany, Path: "integrations.datadog.webhook_token", Unsupported: notOfferedDatadog},
	{Tier: TierCompany, Path: "integrations.datadog.provisioning.api_key", Unsupported: notOfferedDatadog},
	{Tier: TierCompany, Path: "integrations.datadog.provisioning.app_key", Unsupported: notOfferedDatadog},
	{Tier: TierCompany, Path: "integrations.atlassian.api_key", Unsupported: notOfferedAtlassian},
	{Tier: TierCompany, Path: "providers.llm.*.cli.auth.token", Unsupported: notOfferedCLIAgent},
	{Tier: TierCompany, Path: "providers.llm.*.cli.auth.credential_bundle", Unsupported: notOfferedCLIAgent},
	{Tier: TierCompany, Path: "providers.llm.*.cli.env.*", Unsupported: notOfferedCLIAgent},

	{Tier: TierBootstrap, Path: "api.auth.tokens[].token", Sources: []CredentialSource{
		{Reference: "Secret `<company>-api-tokens`, which the operator generates once (or a platform supplies): " +
			"key `operator` for the operator's own token, `access` for the one published in `status.apiAccessSecretRef`"},
	}},
	{Tier: TierBootstrap, Path: "secrets.keys[].material", Sources: []CredentialSource{
		{Reference: "Secret `<company>-keyring`, which the operator generates once (or a platform supplies): " +
			"one key per keyring key, the annotation `crewlet.ai/active-key-id` naming the one that seals"},
	}},
	{Tier: TierBootstrap, Path: "store.objects.s3.secret_access_key", Sources: []CredentialSource{
		{
			Field:     "Company.spec.objects.credentialsSecretRef",
			Reference: "`SecretReference`, key `secretAccessKey` (`S3SecretAccessKeyKey`); unset, the pod's own identity",
		},
	}},
	{Tier: TierBootstrap, Path: "stream.token", Unsupported: notOfferedStreamToken},
}
