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
// here, and one it stops marking fails until its row goes; the same tests
// hold docs/concepts/credentials.md to the table, and the CRDs to every
// field it names. A hand-kept list was what this replaced, and nothing told
// it when the engine moved.
//
// It is test code because only tests and the page read it: the renderer
// fills each position from its own field, and a table in the manager that
// nothing in the manager reads would be dead weight in every binary.

// credentialTier is the document a credential position is in.
type credentialTier string

// The two documents.
const (
	// tierCompany is the company document (Crewlet's Tier B), rendered by
	// Company and applied through the engine's API.
	tierCompany credentialTier = "company"
	// tierBootstrap is a data pod's crewlet.yaml (Crewlet's Tier A),
	// rendered by Bootstrap and kept in a Secret.
	tierBootstrap credentialTier = "bootstrap"
)

// credentialPosition is one position Crewlet marks as a credential.
type credentialPosition struct {
	Tier credentialTier
	// Path is the position as Crewlet's schema places it: keys joined by
	// dots, `[]` for every element of a list and `*` for every value of a
	// map. The org chart nests, so its positions are written from the entry
	// they belong to, since a Seat or a Unit fills them the same way at any
	// depth: a role's from `roles[]`, `units[].roles[]` and
	// `units[].children[].roles[]` included, and a unit's from `units[]`,
	// a child unit's `units[].children[]` (at any depth) included.
	Path string
	// Sources fill the position; more than one when it is filled in more
	// than one way. Empty when Unsupported says why nothing does.
	Sources []credentialSource
	// Unsupported says why v1alpha1 offers no way to fill the position.
	Unsupported string
}

// credentialSource is one way a position is filled.
type credentialSource struct {
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

// Why the positions v1alpha1 does not offer have no field.
const (
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
	// provisioned is a seat's credential the engine mints into its own
	// store, which no Secret holds.
	provisioned = "`ProvisionedCredential`: no Secret; the engine mints the value into its own store"
)

// githubWebhookSecret is the position of the GitHub organization
// integration's webhook secret, which the refs and render tests also name as
// the place in the document it is rendered at; it has no index to differ by.
const githubWebhookSecret = "integrations.github.webhook_secret"

// credentialPositions are every credential position of both documents, in
// the order docs/concepts/credentials.md lists them.
var credentialPositions = []credentialPosition{
	{Tier: tierCompany, Path: "providers.llm.*.api_keys[]", Sources: []credentialSource{
		{Field: "LLMProvider.spec.apiKeySecretRefs", Reference: perKey},
		{
			Field: "EngineClass.spec.platformLLMProviders[].apiKeySecretRefs", Reference: "one `NamespacedSecretKeyReference` per key",
			When: "for a model the platform provides",
		},
	}},
	{Tier: tierCompany, Path: "providers.embeddings.api_key", Sources: []credentialSource{
		{Field: "Company.spec.embeddings.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "providers.sandbox.e2b.api_key", Sources: []credentialSource{
		{Field: "Company.spec.sandbox.e2b.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "providers.sandbox.setup[].files.*", Sources: []credentialSource{
		{Field: "Company.spec.sandbox.setup[].files[].valueFrom.secretKeyRef", Reference: perFile},
	}},
	{Tier: tierCompany, Path: "providers.sandbox.setup[].env.*", Sources: []credentialSource{
		{Field: "Company.spec.sandbox.setup[].env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: tierCompany, Path: "mcp_servers[].env.*", Sources: []credentialSource{
		{Field: "MCPServer.spec.env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: tierCompany, Path: "mcp_servers[].headers.*", Sources: []credentialSource{
		{Field: "MCPServer.spec.headers[].valueFrom.secretKeyRef", Reference: perHeader},
	}},
	{Tier: tierCompany, Path: "units[].mcp_env.*.*", Sources: []credentialSource{
		{Field: "Unit.spec.mcp[].env[].valueFrom.secretKeyRef", Reference: perEnvOrHead},
	}},
	{Tier: tierCompany, Path: "roles[].mcp_env.*.*", Sources: []credentialSource{
		{Field: "Seat.spec.mcp[].env[].valueFrom.secretKeyRef", Reference: perEnvOrHead},
		{
			Field: "Seat.spec.mcp[].env[].valueFrom.provisioned", Reference: provisioned,
			When: "under an Integration with provisioning Engine (GitLab, Mattermost, Datadog, Atlassian)",
		},
	}},
	{Tier: tierCompany, Path: "roles[].sandbox.env.*", Sources: []credentialSource{
		{Field: "Seat.spec.sandbox.env[].valueFrom.secretKeyRef", Reference: perVariable},
		{
			Field: "Seat.spec.sandbox.env[].valueFrom.provisioned", Reference: provisioned,
			When: "under an Integration with provisioning Engine",
		},
	}},
	{Tier: tierCompany, Path: "roles[].sandbox.setup[].files.*", Sources: []credentialSource{
		{Field: "Seat.spec.sandbox.setup[].files[].valueFrom.secretKeyRef", Reference: perFile},
	}},
	{Tier: tierCompany, Path: "roles[].sandbox.setup[].env.*", Sources: []credentialSource{
		{Field: "Seat.spec.sandbox.setup[].env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},
	{Tier: tierCompany, Path: "roles[].integrations.slack.bot_token", Sources: []credentialSource{
		{Field: "Seat.spec.integrations.slack.credentialsSecretRef", Reference: "`SecretReference`, key `botToken` (`SlackBotTokenKey`)"},
	}},
	{Tier: tierCompany, Path: "roles[].integrations.slack.signing_secret", Sources: []credentialSource{
		{
			Field:     "Seat.spec.integrations.slack.credentialsSecretRef",
			Reference: "`SecretReference`, key `signingSecret` (`SlackSigningSecretKey`)",
		},
	}},
	{Tier: tierCompany, Path: "roles[].integrations.github.private_key", Sources: []credentialSource{
		{
			Field:     "Seat.spec.integrations.github.app.credentialsSecretRef",
			Reference: "`SecretReference`, key `privateKey` (`GitHubAppPrivateKeyKey`)",
		},
	}},
	{Tier: tierCompany, Path: "roles[].integrations.github.webhook_secret", Sources: []credentialSource{
		{
			Field:     "Seat.spec.integrations.github.app.credentialsSecretRef",
			Reference: "`SecretReference`, key `webhookSecret` (`GitHubAppWebhookSecretKey`)",
		},
	}},
	{Tier: tierCompany, Path: githubWebhookSecret, Sources: []credentialSource{
		{Field: "Integration.spec.github.webhookSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.github.token", Sources: []credentialSource{
		{Field: "Integration.spec.github.tokenSecretRef", Reference: oneKey},
	}},

	{Tier: tierCompany, Path: "integrations.gitlab.signing_secret", Sources: []credentialSource{
		{Field: "Integration.spec.gitlab.signingSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.gitlab.token", Sources: []credentialSource{
		{Field: "Integration.spec.gitlab.tokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.gitlab.provisioning.admin_token", Sources: []credentialSource{
		{Field: "Integration.spec.gitlab.engineProvisioning.adminTokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.mattermost.provisioning.admin_token", Sources: []credentialSource{
		{Field: "Integration.spec.mattermost.engineProvisioning.adminTokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "roles[].integrations.mattermost.bot_token", Sources: []credentialSource{
		{Field: "Seat.spec.integrations.mattermost.botToken.secretKeyRef", Reference: oneKey},
		{
			Field: "Seat.spec.integrations.mattermost.botToken.provisioned", Reference: provisioned,
			When: "under a Mattermost Integration with provisioning Engine",
		},
	}},
	{Tier: tierCompany, Path: "integrations.jira.token", Sources: []credentialSource{
		{Field: "Integration.spec.jira.tokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.jira.webhook_secret", Sources: []credentialSource{
		{Field: "Integration.spec.jira.webhookSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.confluence.token", Sources: []credentialSource{
		{Field: "Integration.spec.confluence.tokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.confluence.webhook_secret", Sources: []credentialSource{
		{Field: "Integration.spec.confluence.webhookSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.confluence.webhook_token", Sources: []credentialSource{
		{Field: "Integration.spec.confluence.webhookTokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.datadog.webhook_token", Sources: []credentialSource{
		{Field: "Integration.spec.datadog.webhookTokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.datadog.provisioning.api_key", Sources: []credentialSource{
		{Field: "Integration.spec.datadog.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.datadog.provisioning.app_key", Sources: []credentialSource{
		{Field: "Integration.spec.datadog.appKeySecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "integrations.atlassian.api_key", Sources: []credentialSource{
		{Field: "Integration.spec.atlassian.apiKeySecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "providers.llm.*.cli.auth.token", Sources: []credentialSource{
		{Field: "LLMProvider.spec.cli.auth.tokenSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "providers.llm.*.cli.auth.credential_bundle", Sources: []credentialSource{
		{Field: "LLMProvider.spec.cli.auth.credentialBundleSecretRef", Reference: oneKey},
	}},
	{Tier: tierCompany, Path: "providers.llm.*.cli.env.*", Sources: []credentialSource{
		{Field: "LLMProvider.spec.cli.env[].valueFrom.secretKeyRef", Reference: perVariable},
	}},

	{Tier: tierBootstrap, Path: "api.auth.tokens[].token", Sources: []credentialSource{
		{Reference: "Secret `<company>-api-tokens`, which the operator generates once (or a platform supplies): " +
			"key `operator` for the operator's own token, `access` for the one published in `status.apiAccessSecretRef`"},
	}},
	{Tier: tierBootstrap, Path: "secrets.keys[].material", Sources: []credentialSource{
		{Reference: "Secret `<company>-keyring`, which the operator generates once (or a platform supplies): " +
			"one key per keyring key, the annotation `crewlet.ai/active-key-id` naming the one that seals"},
	}},
	{Tier: tierBootstrap, Path: "store.objects.s3.secret_access_key", Sources: []credentialSource{
		{
			Field:     "Company.spec.objects.credentialsSecretRef",
			Reference: "`SecretReference`, key `secretAccessKey` (`S3SecretAccessKeyKey`); unset, the pod's own identity",
		},
	}},
	{Tier: tierBootstrap, Path: "stream.token", Unsupported: notOfferedStreamToken},
}
