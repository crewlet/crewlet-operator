package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A seat's pause is not part of the company document: Crewlet keeps it as a
// record of the fleet's coordination store, written by a person through the
// operator catalogue's pause_seat and resume_seat tools, and holds a paused
// seat's mail on its inbox until somebody resumes it. This file reads which
// seats are paused (the engine's roster, GET /agents) and calls the two
// tools.
//
// The tools are called over MCP (POST /operator/mcp) rather than the engine's
// other transport for them, /operator/act. Act names each gesture with a
// UUIDv7 Idempotency-Key and answers a retry under the same key as the one
// gesture; for that to hold across a crash of the operator, the key would
// have to be persisted before the call and read back after it. A pause or a
// resume needs none of that: one repeated by value finds the seat in the
// state it asks for and changes nothing. The client is the official MCP
// SDK's, because the transport is streamable HTTP, whose answers may arrive
// as a server-sent event stream and whose session has a negotiated protocol
// version: a hand-rolled client would have to agree with the engine's server,
// which is the same SDK, on both, for every release of either.

// OperatorMCPPath is where the engine serves the operator catalogue over MCP.
const OperatorMCPPath = "/operator/mcp"

// The operator catalogue's pause tools.
const (
	toolPauseSeat  = "pause_seat"
	toolResumeSeat = "resume_seat"
)

// MaxPauseReasonRunes is the longest reason the engine keeps on a pause, in
// characters (Crewlet's builtin.MaxPauseReasonRunes): one line on the seat's
// profile and in its feed.
const MaxPauseReasonRunes = 500

// SeatPause is what the engine reports about one agent seat's pause.
type SeatPause struct {
	// Paused is whether a person (or the operator) has the seat paused: it
	// starts no new turn and its mail waits on its inbox.
	Paused bool
	// By is who paused it, as Crewlet records the author of a write: the
	// seat of the person who paused it, or, for a Tier A token, its login
	// TokenLogin(id), since no seat is bound to one; the operator's own
	// pause reads "token:operator". Empty when the seat is not paused.
	By string
	// At is when the pause was taken. Zero when the seat is not paused.
	At time.Time
	// Reason is why, in the pauser's words; empty when none was given.
	Reason string
	// StopRunning is whether the pause also ended the turn the seat was on
	// rather than letting it finish.
	StopRunning bool
}

// PauseResult is the engine's answer to a pause or a resume: the seat's pause
// as the fleet's record holds it once the call has been made, and whether the
// call changed it. A pause of a paused seat and a resume of a free one change
// nothing and announce nothing.
type PauseResult struct {
	SeatPause
	Changed bool
}

// ErrOutcomeUnknown is a pause or resume the engine may or may not have
// recorded: its write to the fleet's record got no answer. Calling again is
// safe, since a call that finds the seat in the state it asks for changes
// nothing.
var ErrOutcomeUnknown = errors.New("the engine may or may not have recorded the change")

// ToolError is a call the engine's operator catalogue refused, with the
// engine's own account of why: a handle that names no agent seat of the
// company it runs, a reason past its bound, a call that lost the record to
// other writers four times in a row. The MCP transport carries the account
// as text and no machine-readable class, so this one type stands for every
// refusal.
type ToolError struct {
	// Tool is the tool that refused, such as pause_seat.
	Tool string
	// Message is the engine's account.
	Message string
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("%s %s: the engine refused the call: %s", OperatorMCPPath, e.Tool, e.Message)
}

// SeatPauses reads the pause of every agent seat of the company the engine
// runs, by handle, from its roster (GET /agents): the account the engine's
// dashboard draws, seeded from the fleet's record when the engine starts and
// moved by the announcement of each change. A Human seat takes no work a pause
// could hold and is not listed; neither is a seat of a revision the engine does
// not run yet.
func (c *Client) SeatPauses(ctx context.Context) (map[string]SeatPause, error) {
	var rows []struct {
		Handle string `json:"handle"`
		Paused *struct {
			By          string `json:"by"`
			At          string `json:"at"`
			Reason      string `json:"reason"`
			StopRunning bool   `json:"stop_running"`
		} `json:"paused"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/agents"}, http.StatusOK, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]SeatPause, len(rows))
	for _, row := range rows {
		if row.Handle == "" {
			continue
		}
		p := SeatPause{}
		if row.Paused != nil {
			at, err := parseInstant(row.Paused.At)
			if err != nil {
				return nil, fmt.Errorf("GET /agents: seat %s: paused.at: %w", row.Handle, err)
			}
			p = SeatPause{Paused: true, By: row.Paused.By, At: at, Reason: row.Paused.Reason, StopRunning: row.Paused.StopRunning}
		}
		out[row.Handle] = p
	}
	return out, nil
}

// PauseSeat pauses the agent seat handle: it starts no new turn until it is
// resumed, its mail waits on its inbox in order, and its scheduled runs are
// skipped. stopRunning also ends its running work: the turn it is on at its
// next round, and its executor run, launching or running, at once, its box
// reclaimed and what it spent charged; a run parked on a person's question
// stays parked behind the pause. reason is shown on the seat and in its feed,
// at most MaxPauseReasonRunes characters. The pause is recorded as the
// operator token's.
//
// Pausing a paused seat changes nothing, unless stopRunning adds a stop the
// pause in place did not ask for; the reason a pause was taken with stays.
// A refusal is a *ToolError; a write that got no answer, ErrOutcomeUnknown.
func (c *Client) PauseSeat(ctx context.Context, handle, reason string, stopRunning bool) (PauseResult, error) {
	args := map[string]any{"handle": handle}
	if reason != "" {
		args["reason"] = reason
	}
	if stopRunning {
		args["stop_running"] = true
	}
	return c.pauseTool(ctx, toolPauseSeat, args)
}

// ResumeSeat lifts the pause of the agent seat handle: it takes work again,
// starting with what waited on its inbox. Resuming a seat that is not paused
// changes nothing. Errors as PauseSeat's.
func (c *Client) ResumeSeat(ctx context.Context, handle string) (PauseResult, error) {
	return c.pauseTool(ctx, toolResumeSeat, map[string]any{"handle": handle})
}

// pauseAnswer is what pause_seat and resume_seat answer. paused_by is the
// author Crewlet records for the caller, which is already the seat of a
// person and the login of a token, so it is the whole of who paused it; the
// answer's paused_by_kind and operator_id (the credential it came through)
// are not read, since the roster names a pauser by the author alone.
type pauseAnswer struct {
	Handle      string `json:"handle"`
	Outcome     string `json:"outcome"`
	Paused      bool   `json:"paused"`
	Changed     bool   `json:"changed"`
	PausedBy    string `json:"paused_by"`
	PausedAt    string `json:"paused_at"`
	Reason      string `json:"reason"`
	StopRunning bool   `json:"stop_running"`
	Detail      string `json:"detail"`
}

// The outcomes a pause tool answers with (Crewlet's statelog outcomes).
const (
	outcomeApplied = "applied"
	outcomeUnknown = "unknown"
)

func (c *Client) pauseTool(ctx context.Context, tool string, args map[string]any) (PauseResult, error) {
	text, err := c.callTool(ctx, tool, args)
	if err != nil {
		return PauseResult{}, err
	}
	var answer pauseAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return PauseResult{}, fmt.Errorf("%s %s: the engine's answer is not the JSON this client reads: %w", OperatorMCPPath, tool, err)
	}
	switch answer.Outcome {
	case outcomeApplied:
	case outcomeUnknown:
		return PauseResult{}, fmt.Errorf("%s %s: %w: %s", OperatorMCPPath, tool, ErrOutcomeUnknown, answer.Detail)
	default:
		return PauseResult{}, fmt.Errorf("%s %s: the engine answered the outcome %q, which this client does not know",
			OperatorMCPPath, tool, answer.Outcome)
	}
	result := PauseResult{Changed: answer.Changed, Paused: answer.Paused}
	if answer.Paused {
		at, err := parseInstant(answer.PausedAt)
		if err != nil {
			return PauseResult{}, fmt.Errorf("%s %s: paused_at: %w", OperatorMCPPath, tool, err)
		}
		result.SeatPause = SeatPause{
			Paused: true, By: answer.PausedBy, At: at, Reason: answer.Reason, StopRunning: answer.StopRunning,
		}
	}
	return result, nil
}

// callTool calls one tool of the operator catalogue, in a session of its own,
// and returns its text answer, or a *ToolError when the engine refused it.
//
// A session per call, rather than one kept per engine: a reconcile calls a
// tool rarely (only when a seat's pause is not what its Seat asks), and a
// session held between reconciles would be state the engine can lose on any
// restart, which the client would then have to notice and re-establish.
func (c *Client) callTool(ctx context.Context, tool string, args map[string]any) (string, error) {
	endpoint, err := url.Parse(c.baseURL + OperatorMCPPath)
	if err != nil {
		return "", fmt.Errorf("engine URL %q: %w", c.baseURL, err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return "", fmt.Errorf("engine URL %q: want an http or https URL", c.baseURL)
	}
	hc := *c.http
	hc.Transport = bearer{token: c.token, next: transportOf(c.http)}
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint.String(), HTTPClient: &hc,
		// Request and answer only: the operator listens for nothing the
		// engine could announce, and a stream held open for it would
		// outlive the call.
		DisableStandaloneSSE: true,
		// The reconcile that made the call is retried as a whole, with
		// the backoff of its own queue; a transport retrying inside it
		// would hold a worker for that long first.
		MaxRetries: -1,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "crewlet-operator", Version: clientVersion()}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", fmt.Errorf("%s: connecting: %w", OperatorMCPPath, err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", OperatorMCPPath, tool, err)
	}
	var text []string
	for _, content := range result.Content {
		if t, ok := content.(*mcp.TextContent); ok {
			text = append(text, t.Text)
		}
	}
	answer := strings.Join(text, "\n")
	if result.IsError {
		return "", &ToolError{Tool: tool, Message: answer}
	}
	return answer, nil
}

// bearer authenticates every request of an MCP session with the client's
// token, as roundTrip does each of the client's own.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("User-Agent", "crewlet-operator")
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.next.RoundTrip(r)
}

// transportOf is hc's transport, or net/http's default one, which is what an
// http.Client with none uses.
func transportOf(hc *http.Client) http.RoundTripper {
	if hc.Transport != nil {
		return hc.Transport
	}
	return http.DefaultTransport
}

// clientVersion is the version the operator names itself by in an MCP
// session: its module's, as the build recorded it ("(devel)" for a build from
// a working tree).
func clientVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// parseInstant reads an RFC 3339 instant the engine wrote, to the second.
//
// To the second because that is all the pause tools answer with, while the
// roster carries the record's own fraction: read at full precision, one pause
// would read as two instants depending on which of the two reported it.
func parseInstant(s string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not an RFC 3339 instant: %w", s, err)
	}
	return at.UTC().Truncate(time.Second), nil
}
