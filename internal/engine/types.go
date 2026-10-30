package engine

import "time"

// tokenLoginPrefix is the class of a Tier A token's login.
const tokenLoginPrefix = "token:"

// TokenLogin is the login a Tier A token (one of the engine's
// api.auth.tokens) acts under, "token:<id>" (Crewlet's iam.TokenLogin). It is
// what Crewlet records as the author of every write made with the token: a
// revision's created_by, a stored secret's updated_by, a seat pause's
// paused_by. A caller compares an author with it rather than spelling the
// prefix, so the one place that knows the engine's grammar is here.
func TokenLogin(id string) string { return tokenLoginPrefix + id }

// ActiveConfig is the fleet's company document: the revision the fleet's
// activation pointer names, which every node answers the same. A node runs it
// once it has applied it, and a node that refused it runs the revision it ran
// before (FleetNode.Apply.Serving).
type ActiveConfig struct {
	// RevisionID is the fleet's revision, the id an ApplyRequest.IfMatch
	// names to make the next write conditional on it.
	RevisionID string

	// Epoch is the activation epoch the answering engine has applied, read
	// from its /health. The engine serves no epoch beside the document, so
	// this is read first, before the document: a write that lands between
	// the two reads leaves Epoch behind RevisionID's activation, never ahead
	// of it. A caller comparing it with an ApplyResult.Epoch therefore never
	// concludes that a revision is running before it is.
	Epoch int64

	// Document is the fleet's company document as YAML, REDACTED: every
	// credential the engine holds is "__redacted__", except one written as a
	// whole ${VAR} reference, which names a credential without carrying it.
	// It is a document to compare and show, not one to apply back.
	Document []byte
}

// Revision is one stored revision of the company document (Client.Revision).
type Revision struct {
	// ID is the revision's id.
	ID string

	// Document is the revision's company document as JSON, which is YAML,
	// REDACTED as ActiveConfig.Document is: every reference written as a
	// whole ${VAR} stays as it is written.
	Document []byte
}

// Health is what an engine's /health says about the node that answered.
type Health struct {
	// Status is the engine's one-word verdict on the node. Until the node
	// serves it is "starting" (with "shutting_down" for one stopped before
	// then), answered 200 once the node has recovered what it holds and its
	// company's part of the boot is under way, and 503, which this client
	// reports as an *APIError, before that. Once the node serves it is, in
	// its order of precedence, "shutting_down", a diverged posture ("shed",
	// "stuck" or "isolated"), "unconfigured", or "ok". A newer engine may
	// add one.
	Status string

	// Configured is whether a company revision is active on the node.
	Configured bool

	// Posture is what the node concluded about the activation epoch it
	// has applied against the fleet's, empty from an engine that has not
	// decided yet.
	Posture Posture

	// AppliedEpoch is the activation epoch the node has applied. The engine
	// applies a revision asynchronously after the write that activates it,
	// so it runs a revision written at epoch N once this has reached N.
	AppliedEpoch int64

	// Alarms is the node's standing alarms, nil before the engine has
	// first evaluated them or on a node that keeps no state log: neither
	// has looked, which is not the same as no alarm.
	Alarms *Alarms

	// Identity is whether the company has its first person:
	// IdentityReady once anybody is enrolled, IdentityUnclaimed while
	// nobody is, IdentityUnknown when the node could not read its identity
	// directory. Empty from an engine that did not say. A service account
	// enrolls nobody: it signs no one in with a password.
	Identity string
}

// The answers Health.Identity carries (Crewlet's internal/api, the
// Identity* constants of /health). The engine never reads a directory it
// could not read as empty, so unknown is never unclaimed.
const (
	IdentityReady     = "ready"
	IdentityUnclaimed = "unclaimed"
	IdentityUnknown   = "unknown"
)

// Posture is an engine node's config posture: what it does about an
// activation epoch it has not applied (crewlet's docs/concepts/control-plane.md,
// "Posture: what a lagging node does").
type Posture string

// The postures the engine reports.
const (
	// PostureServe is a node that has applied the current epoch.
	PostureServe Posture = "serve"

	// PostureWait is a node behind the current epoch for an ordinary
	// reason: it is applying the epoch, it has not polled the activation
	// pointer yet, or it has been behind longer than the engine's grace
	// with no peer reporting either way and no attempt of its own that
	// failed, which the engine takes as no evidence.
	PostureWait Posture = "wait"

	// PostureShed is a node that cannot apply an epoch its peers have, and
	// refuses new work.
	PostureShed Posture = "shed"

	// PostureIsolated is a node that failed to apply the epoch while no
	// peer has applied it, and keeps serving the epoch it applied last. A
	// single-node engine that cannot apply a revision ends here.
	PostureIsolated Posture = "isolated"

	// PostureStuck is a node that cannot apply an epoch its peers have and
	// has exhausted its retries: it needs a corrected revision.
	PostureStuck Posture = "stuck"
)

// Diverged reports whether the posture says the node has failed to apply
// the current epoch rather than only not reached it yet. The engine concludes
// that only from an attempt at the epoch that ended in failure, or from a lag
// on it past its grace while a peer has applied it: an apply still running
// reads wait.
func (p Posture) Diverged() bool {
	return p == PostureShed || p == PostureIsolated || p == PostureStuck
}

// ApplyStatus is how a node's last attempt to apply an activation epoch
// ended, as it recorded it for its peers.
type ApplyStatus string

// The outcomes the engine records.
const (
	// ApplyOK is an epoch applied cleanly.
	ApplyOK ApplyStatus = "ok"

	// ApplyError is an epoch the node refused: it changed nothing, and
	// keeps serving the epoch it applied before.
	ApplyError ApplyStatus = "error"

	// ApplyDegraded is an apply that failed after a subsystem it cannot
	// undo had changed; the node needs a restart.
	ApplyDegraded ApplyStatus = "degraded"
)

// Failed reports whether the attempt did not apply the epoch.
func (s ApplyStatus) Failed() bool { return s == ApplyError || s == ApplyDegraded }

// ApplyRecord is what a node recorded about its last attempt to apply an
// activation epoch, the revision it runs, and the one it would start on: the
// config_* fields of its row in the engine's /fleet. The zero value is a node
// that has recorded no attempt.
type ApplyRecord struct {
	// Epoch is the activation epoch the attempt was for.
	Epoch int64

	// RevisionID is the revision that epoch activated: the one the node last
	// attempted, which it runs only where the attempt succeeded (Serving).
	RevisionID string

	// Status is how the attempt ended.
	Status ApplyStatus

	// Error is the engine's own account of a failed attempt, empty for
	// one that succeeded. The engine shows it on its dashboard and caps it
	// at 2000 bytes; it names configuration, never a credential's value,
	// which the engine resolves only where a provider is constructed.
	Error string

	// Serving is the revision the node RUNS (config_serving_revision_id):
	// the one its last successful apply installed, or the one it booted on;
	// empty while it runs none. It differs from RevisionID exactly when the
	// node refused the revision it last attempted, and that node goes on
	// running Serving, resolving its references from the secret snapshot
	// every attempt reads from the store again, so every value Serving's
	// document references has to stay in the secret store for as long as
	// the node runs it. It says nothing of what the node starts on, which
	// is Boots.
	Serving string

	// Boots is the revision the node would START ON if it restarted now
	// (config_boot_revision_id). On a node whose store outlives a restart,
	// as a data pod's does on its volume, that is the revision the boot row
	// in its store names, which the engine moves only once an apply has
	// succeeded, as a local write it retries on every tick until it lands:
	// so on such a node it is Serving except while that write is being
	// retried, when the node runs a revision it would not start on, and a
	// value only the revision it starts on references is what it needs to
	// start at all.
	//
	// Empty beside an attempt (Status) is a node that starts on nothing and
	// applies the fleet's revision from scratch, whatever it runs: one whose
	// store is scratch, as every seat pod's is (render.SeatPodBootstrap),
	// which the engine reports empty whatever the boot row it writes there
	// holds, since that store is discarded at every start and no start reads
	// the row; or a data node on a fresh volume, whose boot row names no
	// revision until its first write lands, its first apply refused or that
	// write still being retried. The engine reads the boot row before the
	// node records anything, and a read of it that fails later leaves its
	// last answer standing, so empty is never a row the engine could not
	// read. Empty in the zero record says nothing: that node has not
	// reported. With Serving empty too, beside an attempt, it is a node that
	// runs no company and starts on none: it started on nothing and refused
	// the fleet's revision, and needs nothing of the secret store beyond
	// that revision.
	Boots string
}

// Alarms is how many of a node's alarms are firing, and which has stood
// longest, such as backup_age or trim_blocked.
type Alarms struct {
	// Count is how many are firing; zero is a real zero.
	Count int
	// Worst names the one firing longest; empty when none is.
	Worst string
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

	// UpdatedBy is who wrote it, as Crewlet records the author of a write:
	// TokenLogin(id) for a Tier A token, the operator's included, and the
	// seat of a person.
	UpdatedBy string

	// OperatorID is the credential the write authenticated with, which
	// Crewlet takes from the request's authentication and never from the
	// request: TokenLogin(id) for a Tier A token, "session:…" for a signed-in
	// person, "pat:…" for a personal token. Unlike UpdatedBy, it stays the
	// token's login when the token is bound to a seat, so it is what tells
	// the operator's own writes from anyone else's.
	OperatorID string

	// Source is the surface the value arrived by, as the engine names it:
	// "api" for every write through PUT /secrets, "setup" or "provision" for
	// the engine's own, and "cli", "llm-login" or "provision" for a crewlet
	// command that wrote the store directly while its node's engine was
	// stopped. It says how, not who.
	Source string
}

// SecretsWritten is what one write into the engine's secret store
// (Client.PutSecrets, Client.WriteSecret) did: the values it sealed, and what
// that did to the running company.
type SecretsWritten struct {
	// Names are the names written, sorted.
	Names []string

	// ReadBy are, by name, the paths of the active revision's document that
	// read it, an empty list for a name nothing reads.
	ReadBy map[string][]string

	// Reload is the re-activation the write made of the active revision
	// because that revision reads one of the names: nil when it reads none,
	// so that nothing was re-activated.
	Reload *Reload
}

// SecretRemoved is what one removal from the engine's secret store
// (Client.DeleteSecret) did.
type SecretRemoved struct {
	// Removed is whether there was a value to remove.
	Removed bool

	// ReadBy are the paths of the active revision's document that read the
	// name, empty when nothing reads it.
	ReadBy []string

	// Reload is the re-activation the removal made of the active revision
	// because that revision reads the name: nil when it reads it not, or
	// nothing was removed, so that nothing was re-activated.
	Reload *Reload
}

// Reload is the engine's answer on the re-activation a write through
// /secrets made of the active revision, after it had sealed every value
// (Crewlet's ADR-0070). The engine answers 200 whatever the re-activation
// did, so a write that sealed its values and could not re-activate says so
// here rather than in an error.
type Reload struct {
	// RevisionID and Epoch are the revision the re-activation stored and
	// activated, and its activation epoch, set when it landed.
	RevisionID string
	Epoch      int64

	// Error is the engine's code for a re-activation that did not land:
	// revision_advanced, validation_error, unavailable, no_active_revision
	// or internal_error. Detail is its sentence.
	Error  string
	Detail string

	// StoredRevisionID and Node name the revision a re-activation stored
	// whose activation was lost or went unanswered, and the node whose
	// history alone holds it.
	StoredRevisionID string
	Node             string

	// Deferred is a write that asked the engine to re-activate nothing
	// (defer_reload, Client.PutSecrets and Client.WriteSecret with
	// deferReload): the values are sealed and not active yet.
	Deferred bool
}

// Landed reports whether r is a re-activation that stored and activated a
// revision. A nil Reload is none.
func (r *Reload) Landed() bool {
	return r != nil && r.RevisionID != "" && r.Error == ""
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

// Fleet is the engine's view of its fleet: the live nodes, the seat leases and
// the agent seats no live node may run, read from the coordination store's
// lease table, so every node of the fleet answers the same.
type Fleet struct {
	// ThisNode is the id of the node that answered.
	ThisNode string

	// Nodes are the live nodes, by id.
	Nodes []FleetNode

	// Seats are the agent seats some live node holds the lease of, by
	// handle. A seat that is not here is held by no node: its messages wait
	// for one.
	Seats []SeatLease

	// Unplaceable are the handles of the agent seats whose placement no
	// live node that runs seats matches. An agent seat that is neither held
	// nor unplaceable is one a node may take and has not yet: the engine
	// claims a few seats every five seconds.
	Unplaceable []string

	// UnmannedRoles are the node roles (data, ingress, seats, workers) no
	// live node runs.
	UnmannedRoles []string

	// TargetEpoch is the activation epoch the fleet converges on, zero when
	// nothing has been activated or the engine could not read it.
	TargetEpoch int64
}

// Holder returns the lease of the seat with handle and the id the engine
// derives for it, and whether a node holds it.
//
// Crewlet names a seat's lease by the seat's id and resolves it to the handle
// through the company the answering node runs; a node that runs no company,
// or one that does not have the seat yet (it has not applied the revision that
// added it), names the lease by the id instead (Crewlet's
// internal/api/queries, seatHandleOf). Matching the handle alone would read
// a held seat as held by nobody for as long as that lasts.
func (f *Fleet) Holder(handle, id string) (SeatLease, bool) {
	for i := range f.Seats {
		if named := f.Seats[i].Handle; named == handle || named == id {
			return f.Seats[i], true
		}
	}
	return SeatLease{}, false
}

// Node returns the live node with id, and whether there is one.
func (f *Fleet) Node(id string) (FleetNode, bool) {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return f.Nodes[i], true
		}
	}
	return FleetNode{}, false
}

// LastApply is what the answering node recorded about its last attempt to
// apply an activation epoch: the epoch, its revision and how the attempt
// ended, with the engine's own account of a failure. /health's posture says
// whether the node has concluded that an apply failed; this record is the
// only place that says which epoch failed and why. A node writes it once
// the attempt has ended, just after it moves its own posture, and a node
// that has recorded no attempt, or that the fleet does not list yet, is the
// zero ApplyRecord.
func (f *Fleet) LastApply() ApplyRecord {
	n, _ := f.Node(f.ThisNode)
	return n.Apply
}

// FleetNode is one live node of a fleet, as its presence lease describes it.
type FleetNode struct {
	// ID is the node's id: Tier A's node.id, which the operator sets to
	// the pod's name.
	ID string

	// Roles are the node's roles: data, ingress, seats, workers.
	Roles []string

	// Broker is how the node's broker takes part in the fleet's.
	Broker BrokerKind

	// Labels are the node's Tier A node.labels, which a seat's
	// role.placement matches.
	Labels map[string]string

	// Seats is how many seats the node holds.
	Seats int

	// StateLogs is how many state logs the node runs, one replication loop
	// each (Crewlet's appliers_total): every domain of the engine's register
	// on a data node, and zero on a node without data, which runs none. The
	// node reports it from its heartbeat, so a node that has not beaten
	// since its logs started reports zero too.
	StateLogs int

	// Apply is the node's record of its last attempt to apply an epoch.
	Apply ApplyRecord
}

// BrokerKind is how a node's broker takes part in the fleet's.
type BrokerKind string

// The broker kinds the engine reports. A newer engine may report one this
// client does not know, which the engine itself calls "unknown".
const (
	// BrokerMember is an embedded broker that holds the streams and votes
	// in the JetStream cluster: a data pod.
	BrokerMember BrokerKind = "member"

	// BrokerLeaf is an embedded broker that joins the members as a leaf
	// and holds nothing: a seat pod.
	BrokerLeaf BrokerKind = "leaf"

	// BrokerClient is a node on an external NATS cluster.
	BrokerClient BrokerKind = "client"
)

// SeatLease is which node holds a seat's lease, and so runs the seat.
type SeatLease struct {
	// Handle is the seat's handle, or the seat's id when the answering node
	// does not have the seat in the company it runs (see Fleet.Holder).
	Handle string

	// Node is the id of the node that holds the lease.
	Node string

	// Epoch is the lease's fencing epoch, which moves when the seat
	// changes hands.
	Epoch int64

	// AcquiredAt is when the node took the seat, on the coordination
	// store's clock: it moves with Epoch, never with a renewal. Zero when
	// the engine did not say.
	AcquiredAt time.Time
}
