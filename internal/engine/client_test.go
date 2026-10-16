package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// The fake engine below answers with JSON written out as the engine writes it,
// so each test reads as the exchange it pins.

const (
	testAuth   = "test-bearer-value-0123456789"
	storedPath = "/secrets/K8S_A"
	storedName = "K8S_A"

	// unconfigured is the reason an engine with no company gives for not
	// being ready.
	unconfigured = "unconfigured"

	baseRevision = "rev-1"

	// poolLabel and dataPool are the placement label the operator gives a
	// data pod, and its value.
	poolLabel = "crewlet.ai/pool"
	dataPool  = "data"
)

// rolesOneLLM is the path of the second seat's llm, as segments.
var rolesOneLLM = []engine.Segment{{Key: "roles"}, {Index: 1, IsIndex: true}, {Key: "llm"}}

// fakeEngine is an httptest server that answers each "METHOD /path" with the
// handler registered for it and records every request it received.
type fakeEngine struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	seen     []seenRequest
}

type seenRequest struct {
	method, path, query string
	header              http.Header
	body                []byte
}

func newFakeEngine(t *testing.T) *fakeEngine {
	t.Helper()
	f := &fakeEngine{t: t, handlers: map[string]http.HandlerFunc{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeEngine) handle(route string, h http.HandlerFunc) { f.handlers[route] = h }

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("reading the request body: %v", err)
	}
	f.mu.Lock()
	f.seen = append(f.seen, seenRequest{
		method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body,
	})
	f.mu.Unlock()
	h, ok := f.handlers[r.Method+" "+r.URL.Path]
	if !ok {
		answer(http.StatusNotFound, `{"error":"no_route"}`)(w, r)
		return
	}
	h(w, r)
}

// only returns the one request the fake received, failing the test otherwise.
func (f *fakeEngine) only() seenRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) != 1 {
		f.t.Fatalf("the engine received %d requests, want 1", len(f.seen))
	}
	return f.seen[0]
}

func (f *fakeEngine) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func (f *fakeEngine) client() *engine.Client { return engine.New(f.server.URL+"/", testAuth) }

// answer writes body, JSON, with status.
func answer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// page writes body as a proxy in front of the engine would: not JSON.
func page(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "<html>bad gateway</html>")
	}
}

func apply(f *fakeEngine, req engine.ApplyRequest) (engine.ApplyResult, error) {
	return f.client().ApplyConfig(context.Background(), req)
}

// write is an ApplyRequest whose content no test here looks at.
var write = engine.ApplyRequest{Document: []byte("name: x\n"), Summary: "s"}

// TestEveryRequestCarriesTheBearerToken pins the one credential the engine's
// guard reads: without it every /config, /secrets and /integrations call is a
// 401.
func TestEveryRequestCarriesTheBearerToken(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /secrets", answer(http.StatusOK, `{"secrets":[]}`))
	if _, err := f.client().ListSecrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.only().header.Get("Authorization"); got != "Bearer "+testAuth {
		t.Fatalf("Authorization = %q, want the bearer token", got)
	}
}

func TestReady(t *testing.T) {
	cases := []struct {
		name       string
		handler    http.HandlerFunc
		wantReason string // empty: ready, or not an engine's not-ready answer
		wantStatus int    // for an answer that is neither
	}{
		{name: "ready", handler: answer(http.StatusOK, `{"ready":true}`)},
		{
			name:       "unconfigured",
			handler:    answer(http.StatusServiceUnavailable, `{"ready":false,"reason":"unconfigured"}`),
			wantReason: unconfigured,
		},
		{
			name:       "draining",
			handler:    answer(http.StatusServiceUnavailable, `{"ready":false,"reason":"draining"}`),
			wantReason: "draining",
		},
		{name: "a proxy's 502", handler: page(http.StatusBadGateway), wantStatus: http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("GET /ready", tc.handler)
			err := f.client().Ready(context.Background())
			switch {
			case tc.wantReason == "" && tc.wantStatus == 0:
				if err != nil {
					t.Fatalf("Ready() = %v, want nil", err)
				}
			case tc.wantReason != "":
				var notReady *engine.NotReadyError
				if !errors.Is(err, engine.ErrNotReady) || !errors.As(err, &notReady) || notReady.Reason != tc.wantReason {
					t.Fatalf("Ready() = %v, want ErrNotReady with reason %q", err, tc.wantReason)
				}
			default:
				var apiErr *engine.APIError
				if errors.Is(err, engine.ErrNotReady) || !errors.As(err, &apiErr) || apiErr.StatusCode != tc.wantStatus {
					t.Fatalf("Ready() = %v, want an *APIError with status %d that is not ErrNotReady", err, tc.wantStatus)
				}
			}
		})
	}
}

// TestReadyUnreachableIsNotNotReady keeps "not up" apart from "up and not
// taking work": a caller waits out the second and reports the first.
func TestReadyUnreachableIsNotNotReady(t *testing.T) {
	f := newFakeEngine(t)
	url := f.server.URL
	f.server.Close()
	err := engine.New(url, testAuth).Ready(context.Background())
	if err == nil || errors.Is(err, engine.ErrNotReady) {
		t.Fatalf("Ready() against a closed server = %v, want a transport error that is not ErrNotReady", err)
	}
}

func TestActiveConfig(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /health", answer(http.StatusOK, `{"status":"ok","applied_epoch":7}`))
	document := "name: Acme\nproviders:\n  llm: {}\n"
	f.handle("GET /config", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "yaml" {
			t.Errorf("GET /config?%s, want format=yaml", r.URL.RawQuery)
		}
		w.Header().Set("ETag", `"rev-7"`)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = io.WriteString(w, document)
	})
	got, err := f.client().ActiveConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.RevisionID != "rev-7" || got.Epoch != 7 || string(got.Document) != document {
		t.Fatalf("ActiveConfig() = {%q, %d, %q}, want {rev-7, 7, the document}", got.RevisionID, got.Epoch, got.Document)
	}
}

// TestActiveConfigReadsTheEpochBeforeTheDocument pins the order that makes
// ActiveConfig.Epoch err only towards "not applied yet".
func TestActiveConfigReadsTheEpochBeforeTheDocument(t *testing.T) {
	f := newFakeEngine(t)
	var order []string
	f.handle("GET /health", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "health")
		answer(http.StatusOK, `{"applied_epoch":1}`)(w, r)
	})
	f.handle("GET /config", func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "config")
		w.Header().Set("ETag", `"r"`)
	})
	if _, err := f.client().ActiveConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"health", "config"}) {
		t.Fatalf("requests in order %v, want health before config", order)
	}
}

func TestActiveConfigBeforeTheFirstApply(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /health", answer(http.StatusOK, `{"applied_epoch":0}`))
	f.handle("GET /config", answer(http.StatusNotFound, `{"error":"no_active_revision"}`))
	_, err := f.client().ActiveConfig(context.Background())
	if !errors.Is(err, engine.ErrNoActiveRevision) {
		t.Fatalf("ActiveConfig() = %v, want ErrNoActiveRevision", err)
	}
}

func TestActiveConfigWithoutAnETagIsAnError(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /health", answer(http.StatusOK, `{"applied_epoch":1}`))
	f.handle("GET /config", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "name: x\n") })
	if _, err := f.client().ActiveConfig(context.Background()); err == nil || !strings.Contains(err.Error(), "ETag") {
		t.Fatalf("ActiveConfig() = %v, want an error naming the missing ETag", err)
	}
}

func TestApplyConfigSendsTheDocumentAndItsPreconditions(t *testing.T) {
	cases := []struct {
		name            string
		ifMatch         string
		createOnly      bool
		wantIfMatch     string
		wantIfNoneMatch string
	}{
		{name: "unconditional"},
		{name: "if-match", ifMatch: baseRevision, wantIfMatch: `"` + baseRevision + `"`},
		{name: "create-only", createOnly: true, wantIfNoneMatch: "*"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("PUT /config", answer(http.StatusCreated, `{
				"revision_id": "rev-2", "epoch": 3, "derived": {"seats": [], "units": null},
				"warnings": [{"kind": "dangling_reference", "path": "roles[0].manages[0]",
					"message": "seat \"CEO\" manages \"Ghost\""}]}`))
			req := write
			req.IfMatch, req.CreateOnly = tc.ifMatch, tc.createOnly
			got, err := apply(f, req)
			if err != nil {
				t.Fatal(err)
			}
			if got.RevisionID != "rev-2" || got.Epoch != 3 || !slices.Equal(got.Warnings, []string{`seat "CEO" manages "Ghost"`}) {
				t.Fatalf("ApplyConfig() = %+v, want rev-2 at epoch 3 with the warning's message", got)
			}
			sent := f.only()
			if !bytes.Equal(sent.body, write.Document) {
				t.Errorf("body = %q, want the document unchanged", sent.body)
			}
			if sent.query != "" {
				t.Errorf("query = %q, want none on a write", sent.query)
			}
			for header, want := range map[string]string{
				"X-Summary":     write.Summary,
				"Content-Type":  "application/yaml",
				"If-Match":      tc.wantIfMatch,
				"If-None-Match": tc.wantIfNoneMatch,
			} {
				if got := sent.header.Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
		})
	}
}

func TestApplyConfigDryRun(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusOK, `{"valid":true,"base_revision_id":"rev-1","warnings":[]}`))
	got, err := apply(f, engine.ApplyRequest{Document: write.Document, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.RevisionID != "" || got.BaseRevisionID != baseRevision || got.Warnings == nil {
		t.Fatalf("ApplyConfig(dry run) = %+v, want no revision, base rev-1 and an empty, non-nil warning list", got)
	}
	if q := f.only().query; q != "dry_run=true" {
		t.Fatalf("query = %q, want dry_run=true", q)
	}
}

func TestApplyConfigRefusesIfMatchWithCreateOnly(t *testing.T) {
	f := newFakeEngine(t)
	req := write
	req.IfMatch, req.CreateOnly = baseRevision, true
	if _, err := apply(f, req); err == nil {
		t.Fatal("ApplyConfig() with IfMatch and CreateOnly succeeded, want an error")
	}
	if n := f.requests(); n != 0 {
		t.Fatalf("the engine received %d requests, want none for a request the client refuses", n)
	}
}

// TestApplyConfigValidationErrorLocatesEveryProblem pins what attribution
// needs: each problem's path, its segments with indexes as numbers, and the
// seat or unit it is about.
func TestApplyConfigValidationErrorLocatesEveryProblem(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusBadRequest, `{
		"error": "validation_error",
		"detail": "roles[1].llm: value not in the allowed set\nunits[0].lead: missing",
		"hint": "the WHOLE document a write produces is validated",
		"problems": [
			{"path": "roles[1].llm", "segments": ["roles", 1, "llm"], "kind": "unknown_value",
			 "message": "roles[1].llm: value not in the allowed set", "seat": "bob"},
			{"path": "units[0].lead", "segments": ["units", 0, "lead"], "kind": "missing",
			 "message": "units[0].lead: missing", "unit": "Platform", "line": 12}
		],
		"derived": {"seats": [], "units": []}}`))
	_, err := apply(f, engine.ApplyRequest{Document: write.Document, DryRun: true})
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("ApplyConfig() = %v, want a *ValidationError", err)
	}
	if invalid.Code != "validation_error" || len(invalid.Problems) != 2 {
		t.Fatalf("ValidationError = %+v, want validation_error with 2 problems", invalid)
	}
	first, second := invalid.Problems[0], invalid.Problems[1]
	if first.Path != "roles[1].llm" || !slices.Equal(first.Segments, rolesOneLLM) || first.Seat != "bob" ||
		first.Kind != "unknown_value" {
		t.Errorf("first problem = %+v", first)
	}
	if second.Unit != "Platform" || second.Line != 12 {
		t.Errorf("second problem = %+v, want unit Platform at line 12", second)
	}
	for _, p := range invalid.Problems {
		if !strings.Contains(err.Error(), p.Message) {
			t.Errorf("error %q does not name problem %q", err, p.Message)
		}
	}
}

// TestUnlocatedDocumentRefusalIsStillAValidationError keeps a refusal without
// problems attributable to the document as a whole rather than lost.
func TestUnlocatedDocumentRefusalIsStillAValidationError(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusBadRequest, `{"error":"invalid_body","detail":"not yaml"}`))
	_, err := apply(f, write)
	var invalid *engine.ValidationError
	if !errors.As(err, &invalid) || len(invalid.Problems) != 1 || invalid.Problems[0].Path != "" ||
		invalid.Problems[0].Message != "not yaml" {
		t.Fatalf("ApplyConfig() = %#v, want one unlocated problem carrying the detail", err)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		handler    http.HandlerFunc
		sentinel   error
		wantCode   string
		wantStatus int
	}{
		{
			name: "stale if-match",
			handler: answer(http.StatusConflict,
				`{"error":"revision_advanced","current_revision_id":"rev-9","your_base":"\"rev-1\""}`),
			sentinel: engine.ErrRevisionAdvanced, wantCode: "revision_advanced", wantStatus: http.StatusConflict,
		},
		{
			name:     "create-only on a configured engine",
			handler:  answer(http.StatusPreconditionFailed, `{"error":"already_configured","current_revision_id":"rev-9"}`),
			sentinel: engine.ErrAlreadyConfigured, wantCode: "already_configured", wantStatus: http.StatusPreconditionFailed,
		},
		{
			name:     "if-match on an unconfigured engine",
			handler:  answer(http.StatusPreconditionFailed, `{"error":"no_active_revision"}`),
			sentinel: engine.ErrNoActiveRevision, wantCode: "no_active_revision", wantStatus: http.StatusPreconditionFailed,
		},
		{
			name:     "wrong token",
			handler:  answer(http.StatusUnauthorized, `{"error":"invalid_token"}`),
			sentinel: engine.ErrUnauthorized, wantCode: "invalid_token", wantStatus: http.StatusUnauthorized,
		},
		{
			name: "a managed document",
			handler: answer(http.StatusForbidden,
				`{"error":"config_managed","managed_by":["operator"],"detail":"the company document is managed by operator"}`),
			sentinel: engine.ErrConfigManaged, wantCode: "config_managed", wantStatus: http.StatusForbidden,
		},
		{
			name: "a token without the grant",
			handler: answer(http.StatusForbidden, `{"error":"unauthorized","grants":["admin"],`+
				`"message":"You don't have the access this needs (Admin, Tracker or Knowledge). Ask an admin for it.",`+
				`"reason":"no_grant"}`),
			sentinel: engine.ErrForbidden, wantCode: "unauthorized", wantStatus: http.StatusForbidden,
		},
		{
			name: "a verb that needs a person",
			handler: answer(http.StatusForbidden, `{"error":"token_refused","grants":["admin"],`+
				`"message":"Access tokens can't do this. Sign in and try again.","reason":"token_refused"}`),
			sentinel: engine.ErrForbidden, wantCode: "token_refused", wantStatus: http.StatusForbidden,
		},
		{
			name: "a token bound to a seat the company dropped",
			handler: answer(http.StatusForbidden, `{"detail":"Your seat, founder, is not in the org chart.",`+
				`"error":"seat_unavailable",`+
				`"message":"Your seat is no longer in the org chart. Ask an admin to move you to another seat."}`),
			sentinel: engine.ErrForbidden, wantCode: "seat_unavailable", wantStatus: http.StatusForbidden,
		},
		{
			name:       "a proxy's 403",
			handler:    page(http.StatusForbidden),
			wantStatus: http.StatusForbidden,
		},
		{
			name: "an engine that could not decide",
			handler: answer(http.StatusServiceUnavailable, `{"error":"unavailable",`+
				`"message":"Can't answer right now. Try again in a moment, or ask another node."}`),
			sentinel: engine.ErrUnavailable, wantCode: "unavailable", wantStatus: http.StatusServiceUnavailable,
		},
		{
			// A 503 asks for the request again whoever wrote it.
			name: "a proxy's 503", handler: page(http.StatusServiceUnavailable),
			sentinel: engine.ErrUnavailable, wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:     "missing summary",
			handler:  answer(http.StatusBadRequest, `{"error":"summary_required","hint":"PUT /config needs an audit summary"}`),
			wantCode: "summary_required", wantStatus: http.StatusBadRequest,
		},
		{name: "a proxy's page", handler: page(http.StatusBadGateway), wantStatus: http.StatusBadGateway},
	}
	sentinels := []error{
		engine.ErrRevisionAdvanced, engine.ErrAlreadyConfigured, engine.ErrNoActiveRevision,
		engine.ErrUnauthorized, engine.ErrNotReady, engine.ErrConfigManaged, engine.ErrForbidden,
		engine.ErrUnavailable,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("PUT /config", tc.handler)
			_, err := apply(f, write)
			var apiErr *engine.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("ApplyConfig() = %v, want an *APIError", err)
			}
			if apiErr.StatusCode != tc.wantStatus || apiErr.Code != tc.wantCode {
				t.Fatalf("APIError = {%d %q}, want {%d %q}", apiErr.StatusCode, apiErr.Code, tc.wantStatus, tc.wantCode)
			}
			// Each refusal matches its own sentinel and no other, so a
			// caller branching on one never mistakes another for it.
			for _, s := range sentinels {
				if got, want := errors.Is(err, s), errors.Is(s, tc.sentinel); got != want {
					t.Errorf("errors.Is(err, %v) = %v, want %v", s, got, want)
				}
			}
		})
	}
}

func TestRevisionAdvancedNamesTheCurrentRevision(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusConflict,
		`{"error":"revision_advanced","current_revision_id":"rev-9","stored_revision_id":"rev-10"}`))
	_, err := apply(f, write)
	var apiErr *engine.APIError
	if !errors.As(err, &apiErr) || apiErr.CurrentRevisionID != "rev-9" || apiErr.StoredRevisionID != "rev-10" {
		t.Fatalf("ApplyConfig() = %v, want the current and the stored revision named", err)
	}
}

// TestRefusalMessageIsWhatTheEngineSaid pins which of the engine's sentences
// a refusal reads as its message: the detail, what the engine says about this
// request; else the message, the sentence the engine writes for the code on
// every refusal; and only an answer with neither, a proxy's, the status text.
func TestRefusalMessageIsWhatTheEngineSaid(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
	}{
		{
			name: "detail and message",
			body: `{"detail":"Your seat, founder, is not in the org chart.","error":"seat_unavailable",` +
				`"message":"Your seat is no longer in the org chart. Ask an admin to move you to another seat."}`,
			want: "Your seat, founder, is not in the org chart.",
		},
		{
			name: "a message only",
			body: `{"error":"token_refused","grants":["admin"],` +
				`"message":"Access tokens can't do this. Sign in and try again.","reason":"token_refused"}`,
			want: "Access tokens can't do this. Sign in and try again.",
		},
		{name: "neither", body: `{"error":"token_refused"}`, want: "Forbidden"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("PUT /config", answer(http.StatusForbidden, c.body))
			_, err := apply(f, write)
			var apiErr *engine.APIError
			if !errors.As(err, &apiErr) || apiErr.Message != c.want {
				t.Fatalf("ApplyConfig() = %v, want an *APIError whose message is %q", err, c.want)
			}
		})
	}
}

// TestAuthorityRefusalNamesItsGrants reads the reason and the grants of a
// refusal on authority, as the engine's authority table writes them: what a
// status message names when the operator's token lacks a grant.
func TestAuthorityRefusalNamesItsGrants(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusForbidden, `{"error":"unauthorized","grants":["admin"],`+
		`"message":"You don't have the access this needs (Admin, Tracker or Knowledge). Ask an admin for it.",`+
		`"reason":"no_grant"}`))
	_, err := apply(f, write)
	var apiErr *engine.APIError
	if !errors.As(err, &apiErr) || !errors.Is(err, engine.ErrForbidden) {
		t.Fatalf("ApplyConfig() = %v, want an *APIError matching ErrForbidden", err)
	}
	if apiErr.Reason != "no_grant" || !slices.Equal(apiErr.Grants, []string{"admin"}) {
		t.Fatalf("APIError reason %q, grants %v, want no_grant and [admin]", apiErr.Reason, apiErr.Grants)
	}
}

// TestUnavailableCarriesTheEnginesRetryAfter reads when the engine asked to
// be sent a request again: a 503's Retry-After in whole seconds, which is
// what the engine writes. A date is not read, nor is the header on anything
// but a 503.
func TestUnavailableCarriesTheEnginesRetryAfter(t *testing.T) {
	const identityUnavailable = `{"error":"identity_unavailable",` +
		`"hint":"This write removes a human seat, and this node could not read the identity directory to see ` +
		`whether anybody holds it; nothing was written.",` +
		`"message":"Can't check who you are right now. Try again in a moment.","seats":["founder"]}`
	for _, c := range []struct {
		name   string
		status int
		header string
		want   time.Duration
	}{
		{name: "seconds", status: http.StatusServiceUnavailable, header: "2", want: 2 * time.Second},
		{name: "none", status: http.StatusServiceUnavailable},
		{name: "a date", status: http.StatusServiceUnavailable, header: "Fri, 16 Oct 2026 23:59:00 GMT"},
		{name: "not a 503", status: http.StatusConflict, header: "2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("PUT /config", func(w http.ResponseWriter, r *http.Request) {
				if c.header != "" {
					w.Header().Set("Retry-After", c.header)
				}
				answer(c.status, identityUnavailable)(w, r)
			})
			_, err := apply(f, write)
			var apiErr *engine.APIError
			if !errors.As(err, &apiErr) || apiErr.RetryAfter != c.want {
				t.Fatalf("ApplyConfig() = %v, want an *APIError with RetryAfter %s", err, c.want)
			}
			if got, want := errors.Is(err, engine.ErrUnavailable), c.status == http.StatusServiceUnavailable; got != want {
				t.Fatalf("errors.Is(err, ErrUnavailable) = %v, want %v", got, want)
			}
		})
	}
}

// seatFounder is the human seat the seat_held tests find a person on, and
// stageActive the stage of a person or a service account who has finished
// enrolling, both as the engine names them.
const (
	seatFounder = "founder"
	stageActive = "active"
)

// TestSeatHeldNamesEachHolderButNotTheirLogin decodes the engine's refusal of
// a write that takes held human seats away: what holds each seat, a person by
// kind and stage and an invitation as one, and nothing a status must not
// carry: neither the person's login, an email address here, nor any id.
func TestSeatHeldNamesEachHolderButNotTheirLogin(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusConflict, `{"detail":"This change removes human seats that `+
		`people still hold: ana (held by an open invitation (inv-1)); founder (held by jane.doe@nimbus.example); `+
		`release (held by service account svc:release).",`+
		`"error":"seat_held",`+
		`"held":{"ana":{"invitation":"inv-1","expires_at":"2026-10-23T09:00:00Z"},`+
		`"founder":{"person":"p-1","kind":"person","login":"jane.doe@nimbus.example","stage":"active"},`+
		`"release":{"person":"p-2","kind":"machine","login":"svc:release","stage":"active"}},`+
		`"hint":"Move each person to another human seat (PATCH /iam/people/{person} with seat) or remove them, `+
		`unbind a service account (PATCH /iam/people/{person} with no seat), cancel an open invitation `+
		`(DELETE /iam/invitations/{id}) — each an admin's gesture — then send the write again.",`+
		`"message":"This change removes a seat someone holds or is invited to. Move or remove them, or cancel `+
		`the invitation, first. The detail says who."}`))
	_, err := apply(f, write)
	var held *engine.SeatHeldError
	if !errors.As(err, &held) {
		t.Fatalf("ApplyConfig() = %v, want a *SeatHeldError", err)
	}
	want := map[string]engine.SeatHolder{
		"ana":       {Kind: engine.HolderInvitation},
		seatFounder: {Kind: engine.HolderPerson, Stage: stageActive},
		"release":   {Kind: engine.HolderServiceAccount, Stage: stageActive},
	}
	if !maps.Equal(held.Held, want) {
		t.Fatalf("Held = %v, want %v", held.Held, want)
	}
	if !strings.HasPrefix(held.Hint, "Move each person") {
		t.Fatalf("Hint = %q, want the engine's", held.Hint)
	}
	for _, leak := range []string{"jane.doe", "svc:release", "p-1", "inv-1"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("the error says %q: %v", leak, err)
		}
	}
	for _, said := range []string{"founder (held by a person, active)", "ana (held by an open invitation)",
		"release (held by a service account, active)"} {
		if !strings.Contains(err.Error(), said) {
			t.Fatalf("the error does not say %q: %v", said, err)
		}
	}
}

// TestDrainingWriteIsNotReady maps the 503 every write meets during a drain
// onto ErrNotReady, the condition a caller retries later.
func TestDrainingWriteIsNotReady(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT /config", answer(http.StatusServiceUnavailable, `{"error":"draining","detail":"this node is draining"}`))
	if _, err := apply(f, write); !errors.Is(err, engine.ErrNotReady) {
		t.Fatalf("ApplyConfig() during a drain = %v, want ErrNotReady", err)
	}
}

func TestListSecrets(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /secrets", answer(http.StatusOK, `{"secrets":[{"name":"K8S_A","key_id":"k1",
		"updated_at":"2026-10-08T19:31:10.31209515Z","updated_by":"token:crewlet-operator","source":"operator"}]}`))
	got, err := f.client().ListSecrets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := engine.SecretInfo{
		Name: storedName, KeyID: "k1", UpdatedAt: time.Date(2026, 10, 8, 19, 31, 10, 312095150, time.UTC),
		UpdatedBy: "token:crewlet-operator", Source: "operator",
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ListSecrets() = %+v, want [%+v]", got, want)
	}
}

func TestPutSecretSendsTheRawValue(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("PUT "+storedPath, answer(http.StatusOK, `{"name":"K8S_A","bytes":9}`))
	value := []byte("line1\n\"x\"")
	if err := f.client().PutSecret(context.Background(), storedName, value, "operator"); err != nil {
		t.Fatal(err)
	}
	sent := f.only()
	if !bytes.Equal(sent.body, value) {
		t.Errorf("body = %q, want the value byte for byte", sent.body)
	}
	if sent.query != "source=operator" {
		t.Errorf("query = %q, want source=operator", sent.query)
	}
}

// TestPutSecretErrorsNeverContainTheValue holds the package's promise on every
// way a write can fail: a refusal, an error answer, a proxy's page and a
// transport failure.
func TestPutSecretErrorsNeverContainTheValue(t *testing.T) {
	const value = "sk-live-do-not-leak"
	cases := map[string]http.HandlerFunc{
		"refused": answer(http.StatusServiceUnavailable,
			`{"error":"no_keyring","detail":"this node has no secrets.keys"}`),
		"internal error": answer(http.StatusInternalServerError, `{"error":"internal_error"}`),
		"a proxy's page": page(http.StatusBadGateway),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("PUT "+storedPath, h)
			err := f.client().PutSecret(context.Background(), storedName, []byte(value), "")
			if err == nil {
				t.Fatal("PutSecret() succeeded, want an error")
			}
			if strings.Contains(err.Error(), value) {
				t.Fatalf("PutSecret() error %q contains the secret value", err)
			}
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		f := newFakeEngine(t)
		url := f.server.URL
		f.server.Close()
		err := engine.New(url, testAuth).PutSecret(context.Background(), storedName, []byte(value), "")
		if err == nil || strings.Contains(err.Error(), value) {
			t.Fatalf("PutSecret() = %v, want an error without the value", err)
		}
	})
}

// TestSecretNamesOutsideTheGrammarNeverReachTheEngine guards the path: a name
// is a URL segment, and "../config" would address another route.
func TestSecretNamesOutsideTheGrammarNeverReachTheEngine(t *testing.T) {
	for _, name := range []string{"", "../config", "a-b", "1A", "A B", "A/B"} {
		f := newFakeEngine(t)
		if err := f.client().PutSecret(context.Background(), name, []byte("v"), ""); err == nil {
			t.Errorf("PutSecret(%q) succeeded, want an error", name)
		}
		if _, err := f.client().DeleteSecret(context.Background(), name); err == nil {
			t.Errorf("DeleteSecret(%q) succeeded, want an error", name)
		}
		if n := f.requests(); n != 0 {
			t.Errorf("name %q: the engine received %d requests, want none", name, n)
		}
	}
}

func TestDeleteSecret(t *testing.T) {
	for _, removed := range []bool{true, false} {
		f := newFakeEngine(t)
		body, err := json.Marshal(map[string]any{"name": storedName, "removed": removed})
		if err != nil {
			t.Fatal(err)
		}
		f.handle("DELETE "+storedPath, answer(http.StatusOK, string(body)))
		got, err := f.client().DeleteSecret(context.Background(), storedName)
		if err != nil || got != removed {
			t.Fatalf("DeleteSecret() = %v, %v, want %v, nil", got, err, removed)
		}
	}
}

func TestIntegrationsGroupsSurfacesUnderTheirTool(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /integrations", answer(http.StatusOK, `{
		"traffic_known": true,
		"integrations": [
			{"key": "jira", "configured": true, "enabled": true, "secret_present": true, "secret_usable": false,
			 "routes": true, "inbound_kind": "webhook", "inbound_path": "/webhooks/jira", "seats": ["ana"]},
			{"key": "atlassian", "configured": true, "enabled": true, "secret_present": null, "secret_usable": null,
			 "routes": null, "inbound_kind": null, "inbound_path": null, "seats": []}
		],
		"tools": [
			{"key": "atlassian", "surfaces": ["atlassian", "confluence", "jira", "forge"], "state": "attention",
			 "label": "Secret unresolved", "reason": "the webhook secret did not resolve", "surface": "jira"},
			{"key": "slack", "surfaces": ["slack"], "state": "not_in_use", "label": "Not in use", "reason": ""}
		]}`))
	got, err := f.client().Integrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Integrations() = %+v, want two tools", got)
	}
	suite, slack := got[0], got[1]
	if suite.Key != "atlassian" || suite.State != engine.IntegrationAttention || suite.Surface != "jira" ||
		suite.Reason == "" {
		t.Errorf("atlassian = %+v", suite)
	}
	// In the tool's order of its surfaces, not the rows' order.
	keys := make([]string, 0, len(suite.Surfaces))
	for _, s := range suite.Surfaces {
		keys = append(keys, s.Key)
	}
	if !slices.Equal(keys, []string{"atlassian", "jira"}) {
		t.Fatalf("atlassian surfaces = %v, want the organization, then jira", keys)
	}
	org, jira := suite.Surfaces[0], suite.Surfaces[1]
	if org.SecretPresent != nil || org.Routes != nil || org.InboundPath != "" {
		t.Errorf("organization surface = %+v, want its three-valued answers nil", org)
	}
	if jira.SecretPresent == nil || !*jira.SecretPresent || jira.SecretUsable == nil || *jira.SecretUsable ||
		jira.InboundPath != "/webhooks/jira" || !slices.Equal(jira.Seats, []string{"ana"}) {
		t.Errorf("jira surface = %+v", jira)
	}
	if slack.State != engine.IntegrationNotInUse || len(slack.Surfaces) != 0 {
		t.Errorf("slack = %+v, want not in use with no surfaces", slack)
	}
}

func TestIntegrationsOfAnUnconfiguredEngine(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /integrations", answer(http.StatusOK, `{"integrations":[]}`))
	got, err := f.client().Integrations(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("Integrations() = %+v, %v, want no tools and no error", got, err)
	}
}

func TestBadBaseURLIsReportedByTheCall(t *testing.T) {
	for _, base := range []string{"engine:8080", "://nope", "ftp://engine"} {
		if err := engine.New(base, testAuth).Ready(context.Background()); err == nil {
			t.Errorf("Ready() against %q succeeded, want an error", base)
		}
	}
}

func TestOversizedAnswerIsRefused(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /integrations", func(w http.ResponseWriter, _ *http.Request) {
		chunk := strings.Repeat("x", 1<<20)
		for range 17 {
			_, _ = io.WriteString(w, chunk)
		}
	})
	if _, err := f.client().Integrations(context.Background()); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Integrations() = %v, want the oversized answer refused", err)
	}
}

func TestContextCancellationStopsTheCall(t *testing.T) {
	f := newFakeEngine(t)
	release := make(chan struct{})
	f.handle("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.client().Ready(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Ready() = %v, want the context's deadline", err)
	}
}

func TestSegmentRefusesAnythingButAKeyOrAnIndex(t *testing.T) {
	var s engine.Segment
	if err := json.Unmarshal([]byte(`{"a":1}`), &s); err == nil {
		t.Fatal("an object decoded as a path segment, want an error")
	}
	if err := json.Unmarshal([]byte(`0`), &s); err != nil || !s.IsIndex || s.Index != 0 || s.String() != "[0]" {
		t.Fatalf("index 0 decoded as %+v, %v", s, err)
	}
	if err := json.Unmarshal([]byte(`"roles"`), &s); err != nil || s.IsIndex || s.String() != "roles" {
		t.Fatalf(`"roles" decoded as %+v, %v`, s, err)
	}
}

// TestRedirectsAreNotFollowed pins that a redirect from whatever answers at
// an engine's address is the answer: the request, its token and its body (a
// credential, for PutSecret) never reach where it points.
func TestRedirectsAreNotFollowed(t *testing.T) {
	elsewhere := newFakeEngine(t)
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFakeEngine(t)
			redirect := func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.server.URL+"/elsewhere", status)
			}
			f.handle("PUT /secrets/GITHUB_TOKEN", redirect)
			f.handle("PUT /config", redirect)
			f.handle("GET /ready", redirect)

			calls := map[string]error{
				"PutSecret": f.client().PutSecret(context.Background(), "GITHUB_TOKEN", []byte("ghp_value"), ""),
				"Ready":     f.client().Ready(context.Background()),
			}
			_, calls["ApplyConfig"] = apply(f, write)
			for name, err := range calls {
				if apiErr, ok := errors.AsType[*engine.APIError](err); !ok || apiErr.StatusCode != status {
					t.Errorf("%s: got %v, want an *APIError with status %d", name, err, status)
				} else if !strings.Contains(err.Error(), "does not follow") {
					t.Errorf("%s: %q does not say the redirect was not followed", name, err)
				}
			}
			if n := elsewhere.requests(); n != 0 {
				t.Fatalf("the redirect's target received %d requests, want none", n)
			}
		})
	}
}

// TestHealthReadsTheAlarms pins the alarm table the operator reports on the
// Company: absent until the engine has evaluated it, and a real zero after.
func TestHealthReadsTheAlarms(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       *engine.Alarms
	}{
		{name: "not evaluated yet", body: `{"status":"ok","applied_epoch":2}`},
		{name: "none firing", body: `{"applied_epoch":2,"alarms":{"count":0}}`, want: &engine.Alarms{}},
		{
			name: "firing", body: `{"applied_epoch":2,"alarms":{"count":3,"worst":"backup_age"}}`,
			want: &engine.Alarms{Count: 3, Worst: "backup_age"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("GET /health", answer(http.StatusOK, c.body))
			got, err := f.client().Health(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.AppliedEpoch != 2 {
				t.Errorf("AppliedEpoch = %d, want 2", got.AppliedEpoch)
			}
			if (got.Alarms == nil) != (c.want == nil) || (c.want != nil && *got.Alarms != *c.want) {
				t.Errorf("Alarms = %+v, want %+v", got.Alarms, c.want)
			}
		})
	}
}

// TestHealthReadsWhetherAnybodyHasJoined pins the read of /health's identity:
// the engine's word as it wrote it, and empty from an engine that left it out.
func TestHealthReadsWhetherAnybodyHasJoined(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"enrolled", `{"status":"ok","identity":"ready"}`, engine.IdentityReady},
		{"nobody joined", `{"status":"ok","identity":"unclaimed"}`, engine.IdentityUnclaimed},
		{"unreadable", `{"status":"ok","identity":"unknown"}`, engine.IdentityUnknown},
		{"not said", `{"status":"ok"}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("GET /health", answer(http.StatusOK, c.body))
			got, err := f.client().Health(context.Background())
			if err != nil || got.Identity != c.want {
				t.Fatalf("Health() = %+v, %v; want identity %q", got, err, c.want)
			}
		})
	}
}

// TestHealthReadsThePosture pins the fields the config reconciler decides
// ConfigApplied from: the status, whether a revision is active, the
// posture, and the applied epoch, and which postures say the node failed to
// apply the current epoch rather than not reached it yet.
func TestHealthReadsThePosture(t *testing.T) {
	for _, c := range []struct {
		posture  engine.Posture
		diverged bool
	}{
		{engine.PostureServe, false},
		{engine.PostureWait, false},
		{engine.PostureShed, true},
		{engine.PostureIsolated, true},
		{engine.PostureStuck, true},
		{"", false},
	} {
		t.Run(string(c.posture), func(t *testing.T) {
			f := newFakeEngine(t)
			f.handle("GET /health", answer(http.StatusOK,
				fmt.Sprintf(`{"status":"isolated","configured":true,"posture":%q,"applied_epoch":7}`, c.posture)))
			got, err := f.client().Health(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := engine.Health{Status: "isolated", Configured: true, Posture: c.posture, AppliedEpoch: 7}
			if got != want {
				t.Errorf("Health() = %+v, want %+v", got, want)
			}
			if got.Posture.Diverged() != c.diverged {
				t.Errorf("Posture(%q).Diverged() = %t, want %t", c.posture, got.Posture.Diverged(), c.diverged)
			}
		})
	}
}

// TestFleetReadsNodesAndSeatLeases pins the read of /fleet as the engine
// writes it: each node's roles, broker, labels and apply record, which node
// holds each seat since when, and the handles of the seats no node may run.
func TestFleetReadsNodesAndSeatLeases(t *testing.T) {
	// seatsNode is the node that answers, a pod of a seat pool, and holds
	// two of the leases the answer names.
	const seatsNode = "nimbus-seats-general-0"
	const fleet = `{
  "nodes": [
    {"id": "nimbus-data-0", "roles": ["data", "ingress", "seats", "workers"], "broker": "member",
     "labels": {"crewlet.ai/pool": "data"}, "owner": "nimbus-data-0:8f2a", "protocol": 4, "seats": 1, "expires_in": 41,
     "config_epoch": 7, "config_status": "ok", "config_error": "", "config_revision_id": "r7",
     "config_reported_at": "2026-09-23T08:02:11.482Z", "in_flight": 0, "draining": false},
    {"id": "nimbus-seats-general-0", "roles": ["seats"], "broker": "leaf",
     "labels": {"crewlet.ai/pool": "general"}, "owner": "nimbus-seats-general-0:11aa", "protocol": 4, "seats": 1,
     "expires_in": 40, "config_epoch": 7, "config_status": "error", "config_error": "engine: apply: no such provider",
     "config_revision_id": "r7", "config_reported_at": ""}
  ],
  "seats": [
    {"handle": "ana", "node": "nimbus-data-0", "owner": "nimbus-data-0:8f2a", "epoch": 4, "expires_in": 41,
     "acquired_at": "2026-09-23T08:02:11.482Z"},
    {"handle": "bo", "node": "nimbus-seats-general-0", "owner": "nimbus-seats-general-0:11aa", "epoch": 2,
     "expires_in": 40, "acquired_at": ""},
    {"handle": "0f6d6a43-5d3c-5a8e-9d8b-6a1f0c2e4b7d", "node": "nimbus-seats-general-0",
     "owner": "nimbus-seats-general-0:11aa", "epoch": 1, "expires_in": 40, "acquired_at": ""}
  ],
  "duties": [{"duty": "maintenance", "node": "nimbus-data-0", "expires_in": 41}],
  "unplaceable": [{"handle": "cy", "placement": {"Node": "", "Labels": {"crewlet.ai/seat": "cy"}}}],
  "unmanned_roles": [],
  "this_node": "nimbus-seats-general-0",
  "target_epoch": 7,
  "activation": {"epoch": 7, "revision_id": "r7", "at": "2026-09-23T08:00:00Z", "summary": "crewlet-operator"}
}`
	f := newFakeEngine(t)
	f.handle("GET /fleet", answer(http.StatusOK, fleet))
	got, err := f.client().Fleet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ThisNode != seatsNode || got.TargetEpoch != 7 || len(got.UnmannedRoles) != 0 {
		t.Errorf("Fleet() = this node %q, target %d, unmanned %v", got.ThisNode, got.TargetEpoch, got.UnmannedRoles)
	}
	data, ok := got.Node("nimbus-data-0")
	if !ok || data.Broker != engine.BrokerMember || data.Seats != 1 || data.Labels[poolLabel] != dataPool ||
		!slices.Equal(data.Roles, []string{dataPool, "ingress", "seats", "workers"}) ||
		data.Apply != (engine.ApplyRecord{Epoch: 7, RevisionID: "r7", Status: engine.ApplyOK}) {
		t.Errorf("Node(nimbus-data-0) = %+v, %t", data, ok)
	}
	if _, ok := got.Node("nimbus-data-1"); ok {
		t.Error("Node(nimbus-data-1) found a node the fleet does not list")
	}
	ana, ok := got.Holder("ana", "")
	if want := time.Date(2026, 9, 23, 8, 2, 11, 482_000_000, time.UTC); !ok || ana.Node != "nimbus-data-0" || ana.Epoch != 4 ||
		!ana.AcquiredAt.Equal(want) {
		t.Errorf("Holder(ana) = %+v, %t, want nimbus-data-0 at epoch 4 since %s", ana, ok, want)
	}
	if bo, ok := got.Holder("bo", ""); !ok || bo.Node != seatsNode || !bo.AcquiredAt.IsZero() {
		t.Errorf("Holder(bo) = %+v, %t, want nimbus-seats-general-0 with no tenure", bo, ok)
	}
	// A lease the answering node names by the seat's id, as a node that does
	// not have the seat in the company it runs does: found by the id, and
	// not by the handle alone.
	const deeID = "0f6d6a43-5d3c-5a8e-9d8b-6a1f0c2e4b7d"
	if dee, ok := got.Holder("dee", deeID); !ok || dee.Node != seatsNode || dee.Epoch != 1 {
		t.Errorf("Holder(dee, %s) = %+v, %t, want nimbus-seats-general-0 at epoch 1", deeID, dee, ok)
	}
	if _, ok := got.Holder("dee", ""); ok {
		t.Error("Holder(dee) without its id found a lease named by an id")
	}
	if _, ok := got.Holder("cy", ""); ok || !slices.Equal(got.Unplaceable, []string{"cy"}) {
		t.Errorf("cy: held %t, unplaceable %v; want held by nobody and unplaceable", ok, got.Unplaceable)
	}
	// The answering node's own record, failed.
	record := got.LastApply()
	if want := (engine.ApplyRecord{Epoch: 7, RevisionID: "r7", Status: engine.ApplyError, Error: "engine: apply: no such provider"}); record != want ||
		!record.Status.Failed() {
		t.Errorf("LastApply() = %+v, want %+v, failed", record, want)
	}
}

// TestFleetLastApplyOfAnUnlistedNodeIsNone pins that a node the fleet does
// not list yet has recorded nothing, rather than another node's record.
func TestFleetLastApplyOfAnUnlistedNodeIsNone(t *testing.T) {
	f := newFakeEngine(t)
	f.handle("GET /fleet", answer(http.StatusOK, `{"this_node":"node-2","nodes":[{"id":"node-0","config_status":"error"}]}`))
	got, err := f.client().Fleet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record := got.LastApply(); record != (engine.ApplyRecord{}) || record.Status.Failed() {
		t.Fatalf("LastApply() of an unlisted node = %+v, want the zero record", record)
	}
}

// TestFleetRefusals pins that a fleet the engine cannot read, or one it
// describes with a tenure that is not a time, is an error and never an
// empty fleet: an empty one would read as every seat unplaced.
func TestFleetRefusals(t *testing.T) {
	unavailable := newFakeEngine(t)
	unavailable.handle("GET /fleet", answer(http.StatusServiceUnavailable, `{"error":"unavailable","message":"the lease table could not be read"}`))
	if got, err := unavailable.client().Fleet(context.Background()); err == nil {
		t.Errorf("Fleet() of an engine answering 503 = %+v, want an error", got)
	} else if apiErr, ok := errors.AsType[*engine.APIError](err); !ok || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Fleet() of an engine answering 503 = %v, want an *APIError with status 503", err)
	}

	malformed := newFakeEngine(t)
	malformed.handle("GET /fleet", answer(http.StatusOK, `{"this_node":"n","seats":[{"handle":"ana","node":"n","acquired_at":"yesterday"}]}`))
	if got, err := malformed.client().Fleet(context.Background()); err == nil || !strings.Contains(err.Error(), "acquired_at") {
		t.Errorf("Fleet() with a malformed acquired_at = %+v, %v, want an error naming acquired_at", got, err)
	}
}
