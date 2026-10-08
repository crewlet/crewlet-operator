package engine

import "time"

// ActiveConfig is the company document an engine is running.
type ActiveConfig struct {
	// RevisionID is the active revision, the id an ApplyRequest.IfMatch
	// names to make the next write conditional on it.
	RevisionID string

	// Epoch is the activation epoch the answering engine has applied, read
	// from its /health. The engine serves no epoch beside the document, so
	// this is read first, before the document: a write that lands between
	// the two reads leaves Epoch behind RevisionID's activation, never ahead
	// of it. A caller comparing it with an ApplyResult.Epoch therefore never
	// concludes that a revision is running before it is.
	Epoch int64

	// Document is the active company document as YAML, REDACTED: every
	// credential the engine holds is "__redacted__", except one written as a
	// whole ${VAR} reference, which names a credential without carrying it.
	// It is a document to compare and show, not one to apply back.
	Document []byte
}

// ApplyRequest is one write of the whole company document.
type ApplyRequest struct {
	// Document is the company document, YAML (or JSON, which is YAML). It
	// replaces the active document whole.
	Document []byte

	// Summary is the audit sentence the engine records on the revision.
	// Required unless DryRun: the engine refuses a write without one.
	Summary string

	// IfMatch makes the write conditional on the active revision being this
	// one; a stale one fails with ErrRevisionAdvanced before anything is
	// stored. Empty is unconditional. Mutually exclusive with CreateOnly.
	IfMatch string

	// CreateOnly makes the write land only on an engine, and a fleet, with no
	// company yet; otherwise it fails with ErrAlreadyConfigured. Mutually
	// exclusive with IfMatch.
	CreateOnly bool

	// DryRun validates the write exactly as the engine would perform it, and
	// stores and activates nothing.
	DryRun bool
}

// ApplyResult is what a write produced.
type ApplyResult struct {
	// RevisionID is the revision the write stored and activated. Empty for a
	// dry run, which stores nothing.
	RevisionID string

	// Epoch is the fleet's activation epoch for RevisionID: every engine has
	// applied the revision once its applied epoch reaches this. Zero for a
	// dry run.
	Epoch int64

	// BaseRevisionID is, for a dry run, the revision the check was built on,
	// empty when nothing was active. A caller that dry-runs and then applies
	// with IfMatch set to it applies exactly what it checked.
	BaseRevisionID string

	// Warnings are what the engine will run but a person should know about,
	// such as a reference that resolves to nothing, each as the engine's own
	// sentence. Never nil.
	Warnings []string
}

// SecretInfo describes one value in the engine's encrypted secret store. The
// engine never returns the value itself.
type SecretInfo struct {
	// Name is the environment-variable name a ${NAME} in the company
	// document resolves through.
	Name string

	// KeyID is the keyring key the value is sealed under.
	KeyID string

	// UpdatedAt is when the value was last written.
	UpdatedAt time.Time

	// UpdatedBy is the API token id that wrote it.
	UpdatedBy string

	// Source is the provenance the writer recorded, such as "api" or
	// "operator".
	Source string
}

// IntegrationState is one tool's standing as the engine judges it.
type IntegrationState string

// The states the engine reports. They are the engine's own vocabulary; a newer
// engine may add one, which a caller should treat as not working.
const (
	// IntegrationConnected is a tool whose every configured surface works.
	IntegrationConnected IntegrationState = "connected"

	// IntegrationAttention is a tool a person has to act on: a credential
	// that is missing or expiring, or a surface nothing can deliver to.
	IntegrationAttention IntegrationState = "attention"

	// IntegrationNotConnected is a configured tool that does not work yet
	// and that nobody has to act on, or whose status the engine cannot read.
	IntegrationNotConnected IntegrationState = "not_connected"

	// IntegrationNotInUse is a tool the company does not use: no block, or
	// every block switched off.
	IntegrationNotInUse IntegrationState = "not_in_use"
)

// IntegrationStatus is the engine's verdict on one tool (Slack, GitHub, the
// Atlassian suite, ...), with the surfaces of it the company document turns on.
type IntegrationStatus struct {
	// Key names the tool: slack, mattermost, atlassian, github, gitlab or
	// datadog.
	Key string

	// State is the engine's verdict.
	State IntegrationState

	// Label is the state in a reader's words.
	Label string

	// Reason is one sentence on why, empty for a tool that works or is not in
	// use.
	Reason string

	// Surface names the surface the state was taken from, empty for a tool
	// with nothing configured.
	Surface string

	// Surfaces are the tool's surfaces the company document turns on, in the
	// order the engine lists them. Empty for a tool not in use.
	Surfaces []IntegrationSurface
}

// IntegrationSurface is how one inbound surface of a tool is wired.
//
// The three pointers are the engine's three-valued answers: nil means the
// question does not apply to the surface, or the engine cannot say.
type IntegrationSurface struct {
	// Key names the surface, such as slack, jira or forge.
	Key string

	// Enabled is false for a block the document keeps switched off.
	Enabled bool

	// SecretPresent says whether the document names the secret a delivery is
	// verified with.
	SecretPresent *bool

	// SecretUsable says whether that secret resolved to a usable value in the
	// engine: present and unusable is a route refusing every delivery.
	SecretUsable *bool

	// Routes says whether a verified delivery would reach a seat.
	Routes *bool

	// InboundPath is the engine path the vendor delivers to, empty for a
	// surface that receives nothing over HTTP.
	InboundPath string

	// Seats are the handles of the seats holding their own identity on this
	// surface.
	Seats []string
}
