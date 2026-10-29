// Package engine is a typed client for the API of a Crewlet engine: its
// probes, the company document and its history under /config, the encrypted
// secret store under
// /secrets, the integration status under /integrations, the fleet's nodes and
// seat leases under /fleet, and its seats' pauses, read from its roster and
// changed through the pause tools of its operator catalogue over MCP (see
// SeatPauses).
//
// It speaks the engine's API as the engine documents it
// (docs/reference/api-endpoints.md in github.com/crewlet/crewlet) and maps
// every refusal onto a typed error: a refused document becomes a
// *ValidationError whose problems carry their place in the document, a write
// that takes a held human seat away a *SeatHeldError, a lost compare-and-set
// matches ErrRevisionAdvanced, a refused token ErrUnauthorized, a token the
// engine will not let do this ErrForbidden, a write a managed document
// refuses this token ErrConfigManaged, a request the engine asks to be sent
// again ErrUnavailable, a draining or unconfigured engine ErrNotReady, and
// anything else an *APIError with the engine's code and message.
//
// No error this package returns carries a secret value: the engine never
// echoes one, and a secret written with WriteSecret or PutSecrets travels
// only in the request body, which no error, log line or wrap here includes.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultTimeout bounds one request, connection to last byte, when the caller's
// context sets no earlier deadline.
//
// Every call here is small and answered from the engine's own state: the
// largest is a whole company document, which the engine caps at 4 MiB and
// validates in well under a second. Thirty seconds is the engine's own bound on
// reading a request body, so a request that takes longer is one the engine
// itself has given up on; waiting past it would only hold a reconcile worker.
const DefaultTimeout = 30 * time.Second

// maxResponseBytes bounds how much of an answer is read. The largest answer
// this client asks for is the company document, which the engine refuses to
// store above 4 MiB; the margin covers its JSON or YAML rendering and the
// derived hierarchy beside a refusal.
const maxResponseBytes = 16 << 20

// headerContentType is the header that says what a request's body is.
const headerContentType = "Content-Type"

// secretName is the grammar the engine keys its secret store by: an
// environment-variable name, because that is what a ${NAME} in the company
// document resolves through.
var secretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Client talks to one engine's API. It is safe for concurrent use.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient makes the Client send its requests through hc instead of its
// own, for a caller that needs its own transport (TLS, a test server's client).
// hc's Timeout then bounds each request in place of DefaultTimeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.http = hc }
}

// New returns a client for the engine whose API is served at baseURL (such as
// http://nimbus-engine.tenant.svc:8080), authenticating with token, one of the
// bearer tokens in the engine's Tier A api.auth.tokens.
//
// A baseURL that does not parse is reported by the first call, so a caller
// building clients from objects under reconciliation reports it there, against
// the object, rather than at construction.
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    NewHTTPClient(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewHTTPClient returns the HTTP client a Client uses unless WithHTTPClient
// gives it another: net/http's default transport with every phase of a
// connection bounded, under DefaultTimeout for the whole request.
//
// A caller that builds a Client per request, as a reconciler does for each
// engine it reaches, passes one NewHTTPClient to all of them with
// WithHTTPClient, so that they share one pool of connections instead of
// each leaving its own idle connections behind.
func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = DefaultTimeout
	// One engine per Client, so a reconcile that makes several calls in a
	// row reuses one connection rather than opening one per call.
	transport.MaxIdleConnsPerHost = 4
	return &http.Client{Transport: transport, Timeout: DefaultTimeout, CheckRedirect: refuseRedirects}
}

// refuseRedirects makes a redirect the answer, which the call then reports.
//
// The engine never redirects its API, and what answers at an engine's
// address is not to be trusted to: a data pod's address is a name behind a
// headless Service that a tenant's own pod can answer at. Following a 307 or
// 308 would send the request on to wherever it pointed, with the request's
// body (a company document, or a credential for the engine's secret store),
// and net/http keeps the Authorization header on a redirect to the same host.
func refuseRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// Ready reports whether the engine is ready for work: nil when its /ready
// answers 200, a *NotReadyError (matching ErrNotReady) naming the engine's
// reason when it answers 503 — starting, draining, unconfigured, shed, stuck
// or estate_behind among them.
//
// An engine with no company yet is not ready ("unconfigured"), yet it serves
// /config so that the first company can be applied: a caller about to apply
// the first document waits for the engine to answer, not for Ready.
//
// An engine that cannot be reached returns the transport's error, which does
// not match ErrNotReady: "not up" and "up, and not taking work" call for
// different responses.
func (c *Client) Ready(ctx context.Context) error {
	resp, err := c.roundTrip(ctx, request{method: http.MethodGet, path: "/ready"})
	if err != nil {
		return err
	}
	switch resp.status {
	case http.StatusOK:
		return nil
	case http.StatusServiceUnavailable:
		var body struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(resp.body, &body) == nil && body.Reason != "" {
			return &NotReadyError{Reason: body.Reason}
		}
	}
	return resp.refusal()
}

// Health returns what the engine's /health says about the answering node:
// its status and config posture, the activation epoch it has applied, and
// its standing alarms.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var answer struct {
		Status       string `json:"status"`
		Configured   bool   `json:"configured"`
		Posture      string `json:"posture"`
		AppliedEpoch int64  `json:"applied_epoch"`
		Alarms       *struct {
			Count int    `json:"count"`
			Worst string `json:"worst"`
		} `json:"alarms"`
		Identity string `json:"identity"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/health"}, http.StatusOK, &answer); err != nil {
		return Health{}, err
	}
	h := Health{
		Status: answer.Status, Configured: answer.Configured, Posture: Posture(answer.Posture),
		AppliedEpoch: answer.AppliedEpoch, Identity: answer.Identity,
	}
	if a := answer.Alarms; a != nil {
		h.Alarms = &Alarms{Count: a.Count, Worst: a.Worst}
	}
	return h, nil
}

// Fleet returns the engine's view of its fleet, GET /fleet: every live node
// with its roles, labels, last apply, the revision it runs and the one it
// would start on, which node holds each seat's lease, and the agent seats no
// live node may run.
//
// Every node answers the same, read from the coordination store's lease
// table rather than from the node that served the request, so the answer
// describes the whole company whichever data pod the operator reaches. A
// lease table the engine cannot read is its 503, an *APIError, never an
// empty Fleet: "no node is live" is a claim, and a store blip is not
// evidence for it.
func (c *Client) Fleet(ctx context.Context) (Fleet, error) {
	var answer struct {
		ThisNode string `json:"this_node"`
		Nodes    []struct {
			ID         string            `json:"id"`
			Roles      []string          `json:"roles"`
			Broker     string            `json:"broker"`
			Labels     map[string]string `json:"labels"`
			Seats      int               `json:"seats"`
			Epoch      int64             `json:"config_epoch"`
			RevisionID string            `json:"config_revision_id"`
			Serving    string            `json:"config_serving_revision_id"`
			Boots      string            `json:"config_boot_revision_id"`
			Status     string            `json:"config_status"`
			Error      string            `json:"config_error"`
		} `json:"nodes"`
		Seats []struct {
			Handle     string `json:"handle"`
			Node       string `json:"node"`
			Epoch      int64  `json:"epoch"`
			AcquiredAt string `json:"acquired_at"`
		} `json:"seats"`
		// Only the handle of an unplaceable seat is read: the engine
		// renders its placement for a person, and the operator knows
		// where it placed the seat.
		Unplaceable []struct {
			Handle string `json:"handle"`
		} `json:"unplaceable"`
		UnmannedRoles []string `json:"unmanned_roles"`
		TargetEpoch   int64    `json:"target_epoch"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/fleet"}, http.StatusOK, &answer); err != nil {
		return Fleet{}, err
	}
	f := Fleet{ThisNode: answer.ThisNode, UnmannedRoles: answer.UnmannedRoles, TargetEpoch: answer.TargetEpoch}
	for i := range answer.Nodes {
		n := &answer.Nodes[i]
		f.Nodes = append(f.Nodes, FleetNode{
			ID: n.ID, Roles: n.Roles, Broker: BrokerKind(n.Broker), Labels: n.Labels, Seats: n.Seats,
			Apply: ApplyRecord{
				Epoch: n.Epoch, RevisionID: n.RevisionID, Status: ApplyStatus(n.Status), Error: n.Error, Serving: n.Serving,
				Boots: n.Boots,
			},
		})
	}
	for _, s := range answer.Seats {
		lease := SeatLease{Handle: s.Handle, Node: s.Node, Epoch: s.Epoch}
		if s.AcquiredAt != "" {
			at, err := time.Parse(time.RFC3339Nano, s.AcquiredAt)
			if err != nil {
				return Fleet{}, fmt.Errorf("GET /fleet: seat %q: acquired_at %q is not an RFC 3339 time: %w", s.Handle, s.AcquiredAt, err)
			}
			lease.AcquiredAt = at
		}
		f.Seats = append(f.Seats, lease)
	}
	for _, u := range answer.Unplaceable {
		f.Unplaceable = append(f.Unplaceable, u.Handle)
	}
	return f, nil
}

// ActiveConfig returns the fleet's company document, GET /config, redacted,
// with its revision: the revision the fleet's activation pointer names, which
// every node answers the same whatever it runs itself. What each node runs is
// FleetNode.Apply.Serving, and what it would start on FleetNode.Apply.Boots:
// a node applies the fleet's revision asynchronously, and one that refuses it
// goes on running the revision it ran before, and starts on the revision its
// own store names where that store outlives a restart, as a data pod's does
// (a seat pod's does not: it starts on nothing and applies the fleet's
// revision). Before the first apply it fails with ErrNoActiveRevision.
//
// An engine that cannot read the activation pointer from its coordination
// store answers 503 with a Retry-After, which is ErrUnavailable: "the fleet
// has no company" would send the next write to replace the one it runs, so
// the engine does not guess, and neither does a caller. Ask again.
func (c *Client) ActiveConfig(ctx context.Context) (ActiveConfig, error) {
	// THE EPOCH FIRST: see ActiveConfig.Epoch for why the order matters.
	health, err := c.Health(ctx)
	if err != nil {
		return ActiveConfig{}, err
	}
	resp, err := c.roundTrip(ctx, request{
		method: http.MethodGet, path: "/config",
		// YAML, the form ApplyRequest.Document takes, so a caller can
		// compare what it rendered with what is running.
		query: url.Values{"format": {"yaml"}},
	})
	if err != nil {
		return ActiveConfig{}, err
	}
	if resp.status != http.StatusOK {
		return ActiveConfig{}, resp.refusal()
	}
	// The engine names the revision only in the ETag: the document is the
	// body, whole, so that it can be sent back as it is.
	revision := revisionOfETag(resp.header.Get("ETag"))
	if revision == "" {
		return ActiveConfig{}, fmt.Errorf("GET /config: the engine answered without an ETag naming the active revision; "+
			"is %s a Crewlet engine, and not a proxy that strips headers?", c.baseURL)
	}
	return ActiveConfig{RevisionID: revision, Epoch: health.AppliedEpoch, Document: resp.body}, nil
}

// Revision returns one revision of the company document,
// GET /config/revisions/{id}, redacted as ActiveConfig's document is: every
// reference written as a whole ${NAME} stays as it is written. The answering
// node reads it from its own history, which holds every revision the fleet's
// pointer named while the node ran on its present store, and which the
// node's retention sweep prunes by that node's marks alone: it keeps a
// revision created or last activated within the engine's revision retention
// (over a year), and, whatever their age, the revision the answering node
// boots on, the fleet's revision as that node last saw it, and the ancestors
// of each as far as their parent links reach. Whether another node runs or
// boots on a revision does not enter into it, so a revision another node
// runs that is off those chains and older than the retention, or one from
// before the answering node's store began, is not there: an *APIError with
// status 404, as is every revision the node does not hold. The operator's
// secret sync reads that as a revision it cannot read, and keeps every value
// in the store for the nodes that run it or start on it
// (secretsync.Desired.KeepUnread).
//
// It is how a caller reads a revision some node runs or would start on that
// is no longer the fleet's (FleetNode.Apply.Serving, FleetNode.Apply.Boots):
// the document of a node that refused the fleet's revision, which it goes on
// running, and the one a node starts on where its store outlives a restart,
// as a data pod's does.
func (c *Client) Revision(ctx context.Context, id string) (Revision, error) {
	if id == "" || strings.ContainsAny(id, "/?#%") {
		// The id becomes a path segment; one that is not a single segment
		// would address another route.
		return Revision{}, fmt.Errorf("revision id %q is not one the engine mints", id)
	}
	var answer struct {
		RevisionID string          `json:"revision_id"`
		Payload    json.RawMessage `json:"payload"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/config/revisions/" + id}, http.StatusOK, &answer); err != nil {
		return Revision{}, err
	}
	if answer.RevisionID != id || len(answer.Payload) == 0 || bytes.Equal(answer.Payload, []byte("null")) {
		return Revision{}, fmt.Errorf("GET /config/revisions/%s: the engine answered revision %q with no document; "+
			"is %s a Crewlet engine?", id, answer.RevisionID, c.baseURL)
	}
	return Revision{ID: answer.RevisionID, Document: answer.Payload}, nil
}

// ApplyConfig replaces the engine's company document with req.Document, or,
// with req.DryRun, checks that it would.
//
// A document the engine refuses fails with a *ValidationError locating each
// problem; a stale req.IfMatch, or a write that lost a race with another
// writer, with ErrRevisionAdvanced; req.CreateOnly on a configured engine with
// ErrAlreadyConfigured.
func (c *Client) ApplyConfig(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	if req.IfMatch != "" && req.CreateOnly {
		return ApplyResult{}, errors.New("apply the company document: IfMatch and CreateOnly are mutually exclusive: " +
			"a write cannot require both a particular active revision and none at all")
	}
	header := http.Header{headerContentType: {"application/yaml"}}
	if req.Summary != "" {
		header.Set("X-Summary", req.Summary)
	}
	switch {
	case req.IfMatch != "":
		// A strong entity tag, the form the engine's ETag takes.
		header.Set("If-Match", `"`+req.IfMatch+`"`)
	case req.CreateOnly:
		header.Set("If-None-Match", "*")
	}
	r := request{method: http.MethodPut, path: "/config", header: header, body: req.Document}
	if req.DryRun {
		r.query = url.Values{"dry_run": {"true"}}
		var checked struct {
			BaseRevisionID string    `json:"base_revision_id"`
			Warnings       []warning `json:"warnings"`
		}
		if err := c.call(ctx, r, http.StatusOK, &checked); err != nil {
			return ApplyResult{}, err
		}
		return ApplyResult{BaseRevisionID: checked.BaseRevisionID, Warnings: messages(checked.Warnings)}, nil
	}
	var applied struct {
		RevisionID string    `json:"revision_id"`
		Epoch      int64     `json:"epoch"`
		Warnings   []warning `json:"warnings"`
	}
	if err := c.call(ctx, r, http.StatusCreated, &applied); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{RevisionID: applied.RevisionID, Epoch: applied.Epoch, Warnings: messages(applied.Warnings)}, nil
}

// ReloadConfig activates the fleet's document again, unchanged, as a new
// revision (POST /config/reload), with summary as its summary when it is not
// empty.
//
// It is the engine's own gesture for a value its secret store holds that the
// running document references: the engine reads the store only when a
// revision activates, so a value written or removed since changes nothing it
// runs until one does. Nothing is sent but the request, so nothing the
// operator renders reaches the engine through it, and the caller names no
// revision: the engine stores again whichever revision the fleet's activation
// pointer names when the request lands, built on it, and activates it with a
// compare-and-set against that pointer, so a write landing inside the request
// fails it with ErrRevisionAdvanced, as any write that lost a race does. An
// engine with no active document fails with ErrNoActiveRevision.
func (c *Client) ReloadConfig(ctx context.Context, summary string) (ApplyResult, error) {
	header := http.Header{}
	if summary != "" {
		header.Set("X-Summary", summary)
	}
	var reloaded struct {
		RevisionID string    `json:"revision_id"`
		Epoch      int64     `json:"epoch"`
		Warnings   []warning `json:"warnings"`
	}
	if err := c.call(ctx, request{method: http.MethodPost, path: "/config/reload", header: header}, http.StatusCreated, &reloaded); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{RevisionID: reloaded.RevisionID, Epoch: reloaded.Epoch, Warnings: messages(reloaded.Warnings)}, nil
}

// warning is the part of an engine warning this client reports.
type warning struct {
	Message string `json:"message"`
}

func messages(warnings []warning) []string {
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, w.Message)
	}
	return out
}

// ListSecrets lists the values in the engine's encrypted secret store, by name.
// The engine never returns a value here.
func (c *Client) ListSecrets(ctx context.Context) ([]SecretInfo, error) {
	var listing struct {
		Secrets []struct {
			Name       string    `json:"name"`
			KeyID      string    `json:"key_id"`
			UpdatedAt  time.Time `json:"updated_at"`
			UpdatedBy  string    `json:"updated_by"`
			OperatorID string    `json:"operator_id"`
			Source     string    `json:"source"`
		} `json:"secrets"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/secrets"}, http.StatusOK, &listing); err != nil {
		return nil, err
	}
	out := make([]SecretInfo, 0, len(listing.Secrets))
	for _, s := range listing.Secrets {
		out = append(out, SecretInfo{
			Name: s.Name, KeyID: s.KeyID, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy,
			OperatorID: s.OperatorID, Source: s.Source,
		})
	}
	return out, nil
}

// PutSecret stores or rotates the value of name in the engine's encrypted
// secret store: WriteSecret, for a caller that needs no more than whether it
// was stored.
func (c *Client) PutSecret(ctx context.Context, name string, value []byte) error {
	_, err := c.WriteSecret(ctx, name, value)
	return err
}

// WriteSecret stores or rotates the value of name in the engine's encrypted
// secret store (PUT /secrets/{name}). When the active revision reads name,
// the engine re-activates it in the same request, so that a ${name} in the
// running document resolves to the new value from then on, and answers 200
// whatever that re-activation did: SecretsWritten.Reload says. The engine
// records who wrote it from the credential this client authenticates with,
// never from anything the request says: see SecretInfo.OperatorID.
//
// The value is the request body, sent as raw bytes: the engine stores exactly
// what it receives, and an encoding step in between is a credential the
// vendor then refuses. So this is the write for a value that is not UTF-8,
// which PutSecrets cannot carry. Nothing this returns contains it.
func (c *Client) WriteSecret(ctx context.Context, name string, value []byte) (SecretsWritten, error) {
	if err := checkSecretName(name); err != nil {
		return SecretsWritten{}, err
	}
	var answer struct {
		Name   string        `json:"name"`
		ReadBy []string      `json:"read_by"`
		Reload *reloadAnswer `json:"reload"`
	}
	if err := c.call(ctx, request{
		method: http.MethodPut, path: "/secrets/" + name,
		header: http.Header{headerContentType: {"application/octet-stream"}},
		body:   value,
	}, http.StatusOK, &answer); err != nil {
		return SecretsWritten{}, err
	}
	return SecretsWritten{
		Names: []string{name}, ReadBy: map[string][]string{name: answer.ReadBy}, Reload: answer.Reload.reload(),
	}, nil
}

// MaxSecretBatch is the most values one PutSecrets writes: the engine
// refuses a `PUT /secrets` body naming more.
const MaxSecretBatch = 64

// PutSecrets stores or rotates every value of values in the engine's
// encrypted secret store in one request (PUT /secrets), and re-activates the
// active revision once if it reads any of them. A rotation of several
// credentials is then one activation, never a series that runs a mix of the
// old and the new values. The engine answers 200 whatever the
// re-activation did: SecretsWritten.Reload says, and is nil when nothing
// reads any of the names.
//
// It writes 1 to MaxSecretBatch values, each named in the store's grammar
// and valid UTF-8, since the body is JSON and a JSON string cannot carry
// other bytes unchanged (WriteSecret can); anything else is refused before a
// request is sent. The engine refuses the whole body when it refuses any
// value, so a failed batch may have sealed none of them, or, for an error
// inside the engine, some. Nothing this returns contains a value.
func (c *Client) PutSecrets(ctx context.Context, values map[string][]byte) (SecretsWritten, error) {
	if len(values) == 0 || len(values) > MaxSecretBatch {
		return SecretsWritten{}, fmt.Errorf("write %d secrets in one request: the engine takes 1 to %d",
			len(values), MaxSecretBatch)
	}
	body := struct {
		Values map[string]string `json:"values"`
	}{Values: make(map[string]string, len(values))}
	for name, value := range values {
		if err := checkSecretName(name); err != nil {
			return SecretsWritten{}, err
		}
		if !utf8.Valid(value) {
			return SecretsWritten{}, fmt.Errorf("the value of secret %s is not UTF-8, which a JSON body cannot carry "+
				"unchanged: write it alone, as raw bytes", name)
		}
		body.Values[name] = string(value)
	}
	// A map of strings encodes without fail.
	encoded, _ := json.Marshal(body)
	var answer struct {
		Names  []string            `json:"names"`
		ReadBy map[string][]string `json:"read_by"`
		Reload *reloadAnswer       `json:"reload"`
	}
	if err := c.call(ctx, request{
		method: http.MethodPut, path: "/secrets",
		header: http.Header{headerContentType: {"application/json"}},
		body:   encoded,
	}, http.StatusOK, &answer); err != nil {
		return SecretsWritten{}, err
	}
	return SecretsWritten{Names: answer.Names, ReadBy: answer.ReadBy, Reload: answer.Reload.reload()}, nil
}

// reloadAnswer is the engine's `reload` in the answer of a write through
// /secrets, as it writes it.
type reloadAnswer struct {
	RevisionID       string `json:"revision_id"`
	Epoch            int64  `json:"epoch"`
	Error            string `json:"error"`
	Detail           string `json:"detail"`
	StoredRevisionID string `json:"stored_revision_id"`
	Node             string `json:"node"`
	Deferred         bool   `json:"deferred"`
}

// reload is the Reload r says, nil for the engine's null.
func (r *reloadAnswer) reload() *Reload {
	if r == nil {
		return nil
	}
	return &Reload{
		RevisionID: r.RevisionID, Epoch: r.Epoch, Error: r.Error, Detail: r.Detail,
		StoredRevisionID: r.StoredRevisionID, Node: r.Node, Deferred: r.Deferred,
	}
}

// DeleteSecret removes name from the engine's secret store, and reports
// whether there was a value to remove and what that did to the running
// company. Removing a name that is not there is not an error.
//
// When the active revision reads name and a value was removed, the engine
// re-activates that revision in the same request, so that it runs without
// the value from then on, and answers 200 whatever the re-activation did:
// SecretRemoved.Reload says.
func (c *Client) DeleteSecret(ctx context.Context, name string) (SecretRemoved, error) {
	if err := checkSecretName(name); err != nil {
		return SecretRemoved{}, err
	}
	var answer struct {
		Removed bool          `json:"removed"`
		ReadBy  []string      `json:"read_by"`
		Reload  *reloadAnswer `json:"reload"`
	}
	if err := c.call(ctx, request{method: http.MethodDelete, path: "/secrets/" + name}, http.StatusOK, &answer); err != nil {
		return SecretRemoved{}, err
	}
	return SecretRemoved{Removed: answer.Removed, ReadBy: answer.ReadBy, Reload: answer.Reload.reload()}, nil
}

// checkSecretName refuses a name the engine's store cannot hold before it
// reaches a URL. The name becomes a path segment, and one such as "../config"
// would otherwise address another route with the value as its body.
func checkSecretName(name string) error {
	if !secretName.MatchString(name) {
		return fmt.Errorf("secret name %q is not an environment-variable name (letters, digits and underscores, "+
			"not starting with a digit), which is the only kind the engine's secret store holds", name)
	}
	return nil
}

// Integrations returns the engine's verdict on every tool it can integrate
// with, each with the surfaces of it the company document turns on. A tool the
// company does not use is reported as IntegrationNotInUse; an engine with no
// company yet reports no tools at all.
func (c *Client) Integrations(ctx context.Context) ([]IntegrationStatus, error) {
	var answer struct {
		Integrations []struct {
			Key           string   `json:"key"`
			Enabled       bool     `json:"enabled"`
			SecretPresent *bool    `json:"secret_present"`
			SecretUsable  *bool    `json:"secret_usable"`
			Routes        *bool    `json:"routes"`
			InboundPath   *string  `json:"inbound_path"`
			Seats         []string `json:"seats"`
		} `json:"integrations"`
		Tools []struct {
			Key      string           `json:"key"`
			Surfaces []string         `json:"surfaces"`
			State    IntegrationState `json:"state"`
			Label    string           `json:"label"`
			Reason   string           `json:"reason"`
			Surface  string           `json:"surface"`
		} `json:"tools"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/integrations"}, http.StatusOK, &answer); err != nil {
		return nil, err
	}
	surfaces := make(map[string]IntegrationSurface, len(answer.Integrations))
	for _, row := range answer.Integrations {
		s := IntegrationSurface{
			Key: row.Key, Enabled: row.Enabled, SecretPresent: row.SecretPresent,
			SecretUsable: row.SecretUsable, Routes: row.Routes, Seats: row.Seats,
		}
		if row.InboundPath != nil {
			s.InboundPath = *row.InboundPath
		}
		surfaces[row.Key] = s
	}
	out := make([]IntegrationStatus, 0, len(answer.Tools))
	for _, tool := range answer.Tools {
		status := IntegrationStatus{
			Key: tool.Key, State: tool.State, Label: tool.Label, Reason: tool.Reason, Surface: tool.Surface,
		}
		// In the tool's own order of its surfaces, which is the order the
		// engine weighs them in.
		for _, key := range tool.Surfaces {
			if s, ok := surfaces[key]; ok {
				status.Surfaces = append(status.Surfaces, s)
			}
		}
		out = append(out, status)
	}
	return out, nil
}

// request is one call to the engine.
type request struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   []byte
}

// response is an engine's answer, read whole.
type response struct {
	method string
	path   string
	status int
	header http.Header
	body   []byte
}

// refusal is the error for an answer the caller did not expect, prefixed with
// the request it answered.
func (r *response) refusal() error {
	if r.status >= 300 && r.status < 400 {
		// The client follows no redirect (refuseRedirects), and the engine
		// sends none, so a 3xx is something else answering at its address.
		return fmt.Errorf("%s %s: %w", r.method, r.path, &APIError{
			StatusCode: r.status,
			Message: fmt.Sprintf("%s to %q, which this client does not follow, since the engine never redirects",
				http.StatusText(r.status), r.header.Get("Location")),
		})
	}
	return fmt.Errorf("%s %s: %w", r.method, r.path, errorFromResponse(r.status, r.header, r.body))
}

// call sends r and decodes a `want` answer's JSON body into out (unless out is
// nil); any other status is the engine's refusal.
func (c *Client) call(ctx context.Context, r request, want int, out any) error {
	resp, err := c.roundTrip(ctx, r)
	if err != nil {
		return err
	}
	if resp.status != want {
		return resp.refusal()
	}
	if out != nil {
		if err := json.Unmarshal(resp.body, out); err != nil {
			return fmt.Errorf("%s %s: the engine's %d answer is not the JSON this client reads: %w",
				r.method, r.path, resp.status, err)
		}
	}
	return nil
}

// roundTrip sends r and reads the answer, whatever its status.
//
// Every error it returns names the method and path and never the body, which
// for WriteSecret and PutSecrets is a credential.
func (c *Client) roundTrip(ctx context.Context, r request) (*response, error) {
	target, err := url.Parse(c.baseURL + r.path)
	if err != nil {
		return nil, fmt.Errorf("engine URL %q: %w", c.baseURL, err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("engine URL %q: want an http or https URL", c.baseURL)
	}
	target.RawQuery = r.query.Encode()
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", r.method, r.path, err)
	}
	maps.Copy(req.Header, r.header)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crewlet-operator")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error names the method and the URL, which hold no secret: a
		// secret's name is in the path, its value only in the body.
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s %s: reading the engine's %d answer: %w", r.method, r.path, resp.StatusCode, err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%s %s: the engine's %d answer is larger than %d bytes, more than any answer "+
			"this client asks for", r.method, r.path, resp.StatusCode, maxResponseBytes)
	}
	return &response{method: r.method, path: r.path, status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// revisionOfETag reads the revision id out of the engine's ETag, a strong
// entity tag holding it.
func revisionOfETag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "W/")
	return strings.Trim(tag, `"`)
}
