package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The sentinels a caller branches on. Each is matched with errors.Is against
// what a Client method returns; the *APIError, *ValidationError or
// *NotReadyError behind it carries the engine's own words.
var (
	// ErrUnauthorized is a 401: the engine accepts none of the bearer tokens
	// it was sent, or none at all. The token is wrong, revoked, or missing
	// from the engine's Tier A api.auth.tokens.
	ErrUnauthorized = errors.New("the engine refused the API token")

	// ErrRevisionAdvanced is a compare-and-set the caller lost: the active
	// revision is not the one an If-Match named, or another write activated
	// between this write's read and its activation. Re-read the active
	// configuration and render again; the engine may have kept this write's
	// revision as inert history.
	ErrRevisionAdvanced = errors.New("the engine's active configuration revision has advanced")

	// ErrAlreadyConfigured is a create-only write (ApplyRequest.CreateOnly)
	// that found a company already configured, on this engine or anywhere in
	// its fleet.
	ErrAlreadyConfigured = errors.New("the engine already runs a company")

	// ErrNoActiveRevision is an engine with no company configured yet: no
	// revision has ever been activated. ActiveConfig answers it before the
	// first apply.
	ErrNoActiveRevision = errors.New("the engine has no active configuration revision")

	// ErrConfigManaged is a write onto the company document that the
	// engine refuses this token: its Tier A api.auth.company_writers names
	// the tokens that alone may change the document, and this one is not
	// among them (403 config_managed). Reads, a reload and the secret store
	// stay open to it. The *APIError behind it lists the writers in
	// ManagedBy.
	ErrConfigManaged = errors.New("the engine's company document is managed by another token")

	// ErrNotReady is an engine that is up but should not be sent work: it
	// is draining for a shutdown, has no company yet, or its posture is shed
	// or stuck. The *NotReadyError behind it names which.
	ErrNotReady = errors.New("the engine is not ready")
)

// The engine's error codes this package maps onto a sentinel. Every refusal the
// engine writes is JSON with an `error` code; see the engine's API reference,
// "Every refusal carries a code".
const (
	codeInvalidToken      = "invalid_token"
	codeRevisionAdvanced  = "revision_advanced"
	codeAlreadyConfigured = "already_configured"
	codeNoActiveRevision  = "no_active_revision"
	codeDraining          = "draining"
	codeConfigManaged     = "config_managed"
)

// codesWithProblems are the refusals of a document: the engine located each
// failure in it, so they become a *ValidationError.
var codesWithProblems = map[string]struct{}{
	"validation_error": {},
	"invalid_body":     {},
	"invalid_patch":    {},
}

// APIError is a request the engine (or something in front of it) refused, for
// every refusal that is not a document's (*ValidationError) or a not-ready
// engine's (*NotReadyError).
//
// It matches ErrUnauthorized, ErrRevisionAdvanced, ErrAlreadyConfigured,
// ErrNoActiveRevision and ErrConfigManaged with errors.Is where its status
// and code say so, so a caller can branch on the sentinel and still read the
// engine's message.
type APIError struct {
	// StatusCode is the HTTP status.
	StatusCode int

	// Code is the engine's machine-readable `error`, such as
	// revision_advanced or invalid_name. Empty when the answer carried no
	// code, which the engine never writes: such an answer came from a proxy
	// in front of it, and says nothing about what the engine did.
	Code string

	// Message is the engine's `detail`, or the HTTP status text when it gave
	// none.
	Message string

	// Hint is the engine's `hint`: what to do about it, when it said.
	Hint string

	// CurrentRevisionID is the revision that is active now, when the engine
	// named one (revision_advanced, already_configured).
	CurrentRevisionID string

	// StoredRevisionID is the revision a write that lost the race stored and
	// left inert, when the engine kept one.
	StoredRevisionID string

	// ManagedBy are the token ids that may change the company document,
	// when the engine refused this write as config_managed.
	ManagedBy []string
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("engine answered ")
	b.WriteString(strconv.Itoa(e.StatusCode))
	if e.Code != "" {
		b.WriteString(" " + e.Code)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Hint != "" {
		b.WriteString(" (" + e.Hint + ")")
	}
	if e.Code == "" {
		// The engine always names a code, so an answer without one is not
		// the engine's; saying so spares the reader from looking for the
		// refusal in the engine's log.
		b.WriteString(" (no error code: the answer came from something in front of the engine)")
	}
	return b.String()
}

// Is reports whether the refusal is one of the package's sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrRevisionAdvanced:
		return e.Code == codeRevisionAdvanced
	case ErrAlreadyConfigured:
		return e.Code == codeAlreadyConfigured
	case ErrNoActiveRevision:
		// A 412 no_active_revision is an If-Match sent to an engine with
		// nothing active, and a 409 one a patch or reload; both mean there is
		// no company yet.
		return e.Code == codeNoActiveRevision
	case ErrConfigManaged:
		// Matched by code and status together: a 403 without the code is
		// a proxy's, and says nothing about the document.
		return e.StatusCode == http.StatusForbidden && e.Code == codeConfigManaged
	}
	return false
}

// NotReadyError is an engine that answered but will not take work now.
type NotReadyError struct {
	// Reason is the engine's own word for why: draining, unconfigured, shed
	// or stuck.
	Reason string
}

func (e *NotReadyError) Error() string {
	return "the engine is not ready: " + e.Reason
}

// Is makes a *NotReadyError match ErrNotReady.
func (e *NotReadyError) Is(target error) bool { return target == ErrNotReady }

// ValidationError is a company document the engine refused, with every failure
// it found located in the document, so a caller can attribute each to the
// object it rendered that part of the document from.
//
// The engine never repeats a credential in a problem's message: it says what
// rule a value breaks, never the value.
type ValidationError struct {
	// Code is the engine's refusal: validation_error (the document parsed and
	// is not a valid company), invalid_body (it is not a company document at
	// all) or invalid_patch.
	Code string

	// Detail is the engine's whole refusal text, one line per failure.
	Detail string

	// Problems are the failures, located. Never empty: a refusal the engine
	// could not locate is one Problem with an empty Path.
	Problems []Problem
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "the engine refused the company document (%s)", e.Code)
	for i, p := range e.Problems {
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString("; ")
		}
		b.WriteString(p.String())
	}
	return b.String()
}

// Problem is one failure in a refused document.
type Problem struct {
	// Path is the failure's place in the document as the engine spells it,
	// such as roles[1].llm or providers.llm.default.base_url. Empty for a
	// failure that belongs to no place in it (a document that is not YAML).
	Path string `json:"path"`

	// Segments is Path taken apart. Read these rather than splitting Path: a
	// map key can contain a dot.
	Segments []Segment `json:"segments"`

	// Kind classifies the failure: missing, unknown_value, out_of_range,
	// conflict, unknown_field, shape, or invalid.
	Kind string `json:"kind"`

	// Message is the failure's whole line, as it appears in the refusal's
	// detail.
	Message string `json:"message"`

	// Seat is the handle of the seat the failure is about, when it is about
	// one.
	Seat string `json:"seat,omitempty"`

	// Unit is the name of the unit the failure is about, when it is about
	// one.
	Unit string `json:"unit,omitempty"`

	// Line is the 1-based line of the submitted document, for a failure the
	// engine's parser found; 0 otherwise.
	Line int `json:"line,omitempty"`
}

// String renders the problem as the engine's own line, which already starts
// with its path.
func (p Problem) String() string {
	if p.Message != "" {
		return p.Message
	}
	return p.Path + ": " + p.Kind
}

// Segment is one step of a Problem's path: a map key or a list index.
type Segment struct {
	// Key is the map key, when the step is one.
	Key string

	// Index is the list index, when IsIndex.
	Index int

	// IsIndex says the step is a list index rather than a key. Index 0 and
	// the key "" are both real steps, so neither can stand for "unset".
	IsIndex bool
}

// UnmarshalJSON reads a step as the engine writes it: a string for a key, a
// number for an index.
func (s *Segment) UnmarshalJSON(data []byte) error {
	var key string
	if err := json.Unmarshal(data, &key); err == nil {
		*s = Segment{Key: key}
		return nil
	}
	var index int
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("a path segment is a string key or an integer index, not %s", data)
	}
	*s = Segment{Index: index, IsIndex: true}
	return nil
}

// String renders the step as it appears in a path.
func (s Segment) String() string {
	if s.IsIndex {
		return "[" + strconv.Itoa(s.Index) + "]"
	}
	return s.Key
}

// refusal is the body of every non-2xx answer the engine writes.
type refusal struct {
	Error             string    `json:"error"`
	Detail            string    `json:"detail"`
	Hint              string    `json:"hint"`
	Reason            string    `json:"reason"`
	Problems          []Problem `json:"problems"`
	CurrentRevisionID string    `json:"current_revision_id"`
	StoredRevisionID  string    `json:"stored_revision_id"`
	ManagedBy         []string  `json:"managed_by"`
}

// errorFromResponse turns a non-2xx answer into the package's error for it.
//
// The body is the engine's answer, never the request's, so it cannot carry a
// secret the caller sent: the engine's refusals name a secret by its name and
// never echo a value.
func errorFromResponse(status int, body []byte) error {
	var r refusal
	if json.Unmarshal(body, &r) != nil {
		// Not JSON, so not the engine's: a proxy's error page. The body is
		// not quoted, because a page of HTML says nothing a caller can act
		// on and would bury the status.
		r = refusal{}
	}
	if _, ok := codesWithProblems[r.Error]; ok {
		problems := r.Problems
		if len(problems) == 0 {
			// The engine locates every document failure; one it did not is
			// still a failure, and the attribution falls back to the
			// document as a whole.
			problems = []Problem{{Kind: "invalid", Message: r.Detail}}
		}
		return &ValidationError{Code: r.Error, Detail: r.Detail, Problems: problems}
	}
	if status == http.StatusServiceUnavailable && r.Error == codeDraining {
		return &NotReadyError{Reason: codeDraining}
	}
	message := r.Detail
	if message == "" {
		message = http.StatusText(status)
	}
	return &APIError{
		StatusCode:        status,
		Code:              r.Error,
		Message:           message,
		Hint:              r.Hint,
		CurrentRevisionID: r.CurrentRevisionID,
		StoredRevisionID:  r.StoredRevisionID,
		ManagedBy:         r.ManagedBy,
	}
}
