package engine_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// fakeCatalogue serves a pause_seat tool that answers text, as the engine's
// operator catalogue does, and records the bearer of every request.
func fakeCatalogue(t *testing.T, answer string, failed bool) (*engine.Client, func() []string) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "pause_seat", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{IsError: failed, Content: []mcp.Content{&mcp.TextContent{Text: answer}}}, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	var mu sync.Mutex
	var bearers []string
	mux := http.NewServeMux()
	mux.Handle(engine.OperatorMCPPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		bearers = append(bearers, r.Header.Get("Authorization"))
		mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return engine.New(srv.URL, testAuth), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bearers...)
	}
}

// TestPauseSeatSendsTheBearerOnEveryRequestOfTheSession pins the session's
// authentication: the engine guards /operator/mcp, so a request of the
// session without the token (the initialize, the call, the close) is one the
// engine refuses.
func TestPauseSeatSendsTheBearerOnEveryRequestOfTheSession(t *testing.T) {
	client, bearers := fakeCatalogue(t,
		`{"handle":"ana","outcome":"applied","paused":true,"changed":true,"paused_by":"token:ops-bot",`+
			`"paused_by_kind":"operator","operator_id":"token:ops-bot",`+
			`"paused_at":"2026-06-08T07:30:02Z","reason":"","stop_running":false}`, false)
	got, err := client.PauseSeat(context.Background(), "ana", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Paused || !got.Changed || got.By != engine.TokenLogin("ops-bot") {
		t.Fatalf("PauseSeat() = %+v, want a changed pause by token:ops-bot", got)
	}
	seen := bearers()
	if len(seen) < 2 {
		t.Fatalf("the session made %d requests, want at least the initialize and the call", len(seen))
	}
	for i, b := range seen {
		if b != "Bearer "+testAuth {
			t.Errorf("request %d carried Authorization %q", i, b)
		}
	}
}

// TestPauseSeatNamesThePauserAsCrewletRecordsThem pins who a pause is by,
// read from the answer Crewlet's pause_seat gives (its pauseAnswer): the
// author it records, paused_by, which is a Tier A token's login and a
// person's seat. The credential beside it (operator_id) is not who paused
// the seat: for a person it is their session, which the roster never names.
func TestPauseSeatNamesThePauserAsCrewletRecordsThem(t *testing.T) {
	for _, c := range []struct {
		name, answer, by string
	}{
		{
			name: "the operator's own token",
			answer: `{"handle":"ana","outcome":"applied","paused":true,"changed":true,"paused_by":"token:operator",` +
				`"paused_by_kind":"operator","operator_id":"token:operator","paused_at":"2026-06-08T07:30:02Z",` +
				`"reason":"","stop_running":false}`,
			by: engine.TokenLogin("operator"),
		},
		{
			name: "a person on their seat",
			answer: `{"handle":"ana","outcome":"applied","paused":true,"changed":false,"paused_by":"founder",` +
				`"paused_by_kind":"human","operator_id":"session:0199aa00-0000-7000-8000-000000000001",` +
				`"paused_at":"2026-06-08T07:30:02Z","reason":"Rollout","stop_running":true}`,
			by: "founder",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, _ := fakeCatalogue(t, c.answer, false)
			got, err := client.PauseSeat(context.Background(), "ana", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Paused || got.By != c.by {
				t.Fatalf("PauseSeat() = %+v, want a pause by %s", got, c.by)
			}
		})
	}
}

// TestPauseSeatReadsTheEnginesAnswers pins every answer the reconciler acts on
// that a running engine cannot be made to give at will: an outcome the store
// may or may not have taken, a refusal, and an outcome this client does not
// know, which must not read as a pause taken.
func TestPauseSeatReadsTheEnginesAnswers(t *testing.T) {
	for _, c := range []struct {
		name    string
		answer  string
		failed  bool
		unknown bool
		refused string
		other   string
	}{
		{
			name:    "an unknown outcome",
			answer:  `{"handle":"ana","outcome":"unknown","detail":"the write may or may not have landed"}`,
			unknown: true,
		},
		{
			name:    "a refusal",
			answer:  `No agent seat has the handle "ana", so there is nothing to pause.`,
			failed:  true,
			refused: `No agent seat has the handle "ana"`,
		},
		{
			name:   "an outcome this client does not know",
			answer: `{"handle":"ana","outcome":"pending","paused":true,"changed":true}`,
			other:  `the outcome "pending"`,
		},
		{
			name:   "a pause with no instant",
			answer: `{"handle":"ana","outcome":"applied","paused":true,"changed":true,"paused_at":""}`,
			other:  "paused_at",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, _ := fakeCatalogue(t, c.answer, c.failed)
			got, err := client.PauseSeat(context.Background(), "ana", "", false)
			var refused *engine.ToolError
			switch {
			case err == nil:
				t.Fatalf("PauseSeat() = %+v, want an error", got)
			case c.unknown != errors.Is(err, engine.ErrOutcomeUnknown):
				t.Fatalf("PauseSeat() = %v; ErrOutcomeUnknown is %v, want %v", err, errors.Is(err, engine.ErrOutcomeUnknown), c.unknown)
			case c.refused != "" && (!errors.As(err, &refused) || !strings.Contains(refused.Message, c.refused)):
				t.Fatalf("PauseSeat() = %v, want a *ToolError with %q", err, c.refused)
			case c.other != "" && !strings.Contains(err.Error(), c.other):
				t.Fatalf("PauseSeat() = %v, want an error naming %q", err, c.other)
			}
		})
	}
}
