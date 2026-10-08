// Package engine is a typed client for the REST API of a Crewlet engine: its
// probes, the company document under /config, the encrypted secret store under
// /secrets, and the integration status under /integrations.
//
// It speaks the engine's API as the engine documents it
// (docs/reference/api-endpoints.md in github.com/crewlet/crewlet) and maps
// every refusal onto a typed error: a refused document becomes a
// *ValidationError whose problems carry their place in the document, a lost
// compare-and-set matches ErrRevisionAdvanced, a refused token ErrUnauthorized,
// a draining or unconfigured engine ErrNotReady, and anything else an
// *APIError with the engine's code and message.
//
// No error this package returns carries a secret value: the engine never
// echoes one, and a secret written with PutSecret travels only in the request
// body, which no error, log line or wrap here includes.
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
	return &http.Client{Transport: transport, Timeout: DefaultTimeout}
}

// Ready reports whether the engine is ready for work: nil when its /ready
// answers 200, a *NotReadyError (matching ErrNotReady) naming the engine's
// reason when it answers 503 — draining, unconfigured, shed or stuck.
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

// ActiveConfig returns the company document the engine runs, redacted, with
// its revision. Before the first apply it fails with ErrNoActiveRevision.
func (c *Client) ActiveConfig(ctx context.Context) (ActiveConfig, error) {
	// THE EPOCH FIRST: see ActiveConfig.Epoch for why the order matters.
	var health struct {
		AppliedEpoch int64 `json:"applied_epoch"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/health"}, http.StatusOK, &health); err != nil {
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
	header := http.Header{"Content-Type": {"application/yaml"}}
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
			Name      string    `json:"name"`
			KeyID     string    `json:"key_id"`
			UpdatedAt time.Time `json:"updated_at"`
			UpdatedBy string    `json:"updated_by"`
			Source    string    `json:"source"`
		} `json:"secrets"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/secrets"}, http.StatusOK, &listing); err != nil {
		return nil, err
	}
	out := make([]SecretInfo, 0, len(listing.Secrets))
	for _, s := range listing.Secrets {
		out = append(out, SecretInfo{
			Name: s.Name, KeyID: s.KeyID, UpdatedAt: s.UpdatedAt, UpdatedBy: s.UpdatedBy, Source: s.Source,
		})
	}
	return out, nil
}

// PutSecret stores or rotates the value of name in the engine's encrypted
// secret store, recording source as its provenance ("api" when empty). A
// ${name} in the company document resolves to it at the next apply.
//
// The value is the request body, sent as raw bytes: the engine stores exactly
// what it receives, and an encoding step in between is a credential the
// vendor then refuses. Nothing this returns contains it.
func (c *Client) PutSecret(ctx context.Context, name string, value []byte, source string) error {
	if err := checkSecretName(name); err != nil {
		return err
	}
	r := request{
		method: http.MethodPut, path: "/secrets/" + name,
		header: http.Header{"Content-Type": {"application/octet-stream"}},
		body:   value,
	}
	if source != "" {
		r.query = url.Values{"source": {source}}
	}
	return c.call(ctx, r, http.StatusOK, nil)
}

// DeleteSecret removes name from the engine's secret store, and reports
// whether there was a value to remove. Removing a name that is not there is
// not an error.
func (c *Client) DeleteSecret(ctx context.Context, name string) (removed bool, err error) {
	if err := checkSecretName(name); err != nil {
		return false, err
	}
	var answer struct {
		Removed bool `json:"removed"`
	}
	if err := c.call(ctx, request{method: http.MethodDelete, path: "/secrets/" + name}, http.StatusOK, &answer); err != nil {
		return false, err
	}
	return answer.Removed, nil
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
	return fmt.Errorf("%s %s: %w", r.method, r.path, errorFromResponse(r.status, r.body))
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
// for PutSecret is a credential.
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
