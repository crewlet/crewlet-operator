package engine

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The sentinels a caller branches on. Each is matched with errors.Is against
// what a Client method returns; the *APIError, *ValidationError,
// *SeatHeldError or *NotReadyError behind it carries the engine's own words.
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
	// engine refuses this credential: its Tier A api.auth.company_writers
	// names the tokens that alone may change the document, and this one is
	// not among them (403 config_managed). Reads, a reload and the secret
	// store stay open to a holder of admin. The *APIError behind it lists
	// the writers in ManagedBy.
	ErrConfigManaged = errors.New("the engine's company document is managed by another token")

	// ErrForbidden is a credential the engine knows and will not let do this
	// (403 unauthorized, token_refused or seat_unavailable): it holds none of
	// the grants the request needs, the request needs a person signed in
	// rather than a token, or the identity directory binds the token to a
	// seat the company no longer has. The *APIError behind it carries the
	// rule's Reason and the Grants that would have admitted it. Unlike
	// ErrUnauthorized the engine accepted the token, so a retry with it
	// changes nothing.
	ErrForbidden = errors.New("the engine does not let the API token do this")

	// ErrUnavailable is a request the engine could not answer now and asks
	// to be sent again (503): it could not decide who is asking, or could
	// not read what the request needs, such as who holds a seat a write
	// takes away. The *APIError behind it carries RetryAfter when the
	// engine said when.
	ErrUnavailable = errors.New("the engine could not answer now")

	// ErrRevisionUnreadable is a revision of the company document that the
	// engine's keyring does not open (409 unreadable_revision or
	// unsealed_revision): sealed under a key the keyring no longer holds, or
	// stored without a seal, which no node reads. The engine answers it to a
	// read of the document and to a write, which reads the revision it
	// replaces, and sends no Retry-After: no wait opens the revision, and
	// nothing the caller sends does, since what does is given to the engine
	// (a key put back in its keyring, which it reads when it starts). The
	// *APIError behind it carries the revision in RevisionID, the engine's
	// account as its Message and the remedy as its Hint.
	ErrRevisionUnreadable = errors.New("the engine's keyring does not open a revision of its company document")

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
	codeSeatHeld          = "seat_held"
)

// codesForbidden are the refusals of a credential the engine accepted: the
// authority table's own (unauthorized for a grant the token lacks,
// token_refused for a verb that needs a person present) and the request
// path's for a token the identity directory binds to a seat the company no
// longer has (seat_unavailable). config_managed is a 403 too, and is not
// here: it refuses a write onto the document whatever the token holds, and
// has its own sentinel.
var codesForbidden = []string{"unauthorized", "token_refused", "seat_unavailable"}

// codesRevisionUnreadable are the refusals of a revision the engine's keyring
// does not open, one per cause because the remedies share nothing: a key put
// back in the keyring (unreadable_revision), or the document brought back
// sealed (unsealed_revision).
var codesRevisionUnreadable = []string{"unreadable_revision", "unsealed_revision"}

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
// It matches ErrUnauthorized, ErrForbidden, ErrRevisionAdvanced,
// ErrAlreadyConfigured, ErrNoActiveRevision, ErrConfigManaged,
// ErrRevisionUnreadable and ErrUnavailable with errors.Is where its status
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

	// Message is the engine's `detail`, what it says about this request;
	// else its `message`, the sentence it writes for the code whatever the
	// request; else the HTTP status text. The engine writes `message` on
	// every refusal and `detail` only where it has more to say.
	Message string

	// Hint is the engine's `hint`: what to do about it, when it said.
	Hint string

	// CurrentRevisionID is the revision that is active now, when the engine
	// named one (revision_advanced, already_configured).
	CurrentRevisionID string

	// StoredRevisionID is the revision a write stored, when the engine kept
	// one and could not activate it: a write that lost the race, which left
	// it inert, or one whose activation the engine sent and could not
	// confirm (UnconfirmedActivation), which it may run or may not.
	StoredRevisionID string

	// RevisionID is the revision a refusal is about, when the engine named
	// one: the revision its keyring does not open (ErrRevisionUnreadable).
	RevisionID string

	// ManagedBy are the token ids that may change the company document,
	// when the engine refused this write as config_managed.
	ManagedBy []string

	// Reason is the rule that refused the credential, on a refusal of its
	// authority (ErrForbidden): no_grant, token_refused and the others of
	// the engine's authority table.
	Reason string

	// Grants are the permissions any one of which would have admitted the
	// credential, on a refusal of its authority, such as [admin]. Empty
	// when no grant would, because what is missing is not a permission.
	Grants []string

	// RetryAfter is how long the engine asked to be left before the
	// request is sent again, from a 503's Retry-After; zero when it did
	// not say.
	RetryAfter time.Duration
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
	case ErrForbidden:
		// By code and status together, as config_managed: a proxy's 403
		// says nothing about what the engine lets the token do.
		return e.StatusCode == http.StatusForbidden && slices.Contains(codesForbidden, e.Code)
	case ErrUnavailable:
		// By status alone: a 503 is "ask again later" whoever wrote it,
		// and the engine writes it under several codes (unavailable,
		// identity_unavailable and the stores' own), each retried the
		// same way. A draining engine's 503 is a *NotReadyError instead.
		return e.StatusCode == http.StatusServiceUnavailable
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
	case ErrRevisionUnreadable:
		// By code alone: only the engine writes these codes, so a 409
		// without one (a proxy's) says nothing about its keyring.
		return slices.Contains(codesRevisionUnreadable, e.Code)
	}
	return false
}

// UnconfirmedActivation is the revision a config write stored whose activation
// the engine sent to its fleet and could not confirm, and true, when err is
// that answer: a 503 naming the revision it stored (ErrUnavailable with a
// StoredRevisionID). A lost race names a stored revision too, but it is no
// 503, and the engine knows that one did not activate.
//
// The fleet may run the revision or may not, and only a read of the fleet's
// activation (Fleet.Activation) says which: a caller that wrote it keeps it
// until a read does, and sends the write again only once one says it did
// not land.
func UnconfirmedActivation(err error) (string, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable || apiErr.StoredRevisionID == "" {
		return "", false
	}
	return apiErr.StoredRevisionID, true
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

// SeatHeldError is a write the engine refused because it takes a human seat
// out of the company, or turns it into an agent's, while something holds it
// (409 seat_held): a person, a service account bound to it, or an open
// invitation onto it. The document is valid; what has to change first is
// the engine's identity directory, which an admin of the company changes.
//
// The engine's detail is not kept: it names each holder by login, for
// SeatHolder's reason not to.
type SeatHeldError struct {
	// Held is what holds each seat the write would take away, by the
	// seat's handle.
	Held map[string]SeatHolder

	// Hint is the engine's remedy, naming the routes that free a seat.
	Hint string
}

// SeatHolder is what holds a human seat, as much of it as a status may say:
// whether it is a person, a service account or an invitation, and how far
// through enrolment. The engine's answer also names the holder's login and
// id, or the invitation's, which this type does not keep: a login is often
// a person's email address, and nothing the operator writes about a Company
// is the place for it.
type SeatHolder struct {
	// Kind is what holds the seat: HolderPerson, HolderServiceAccount or
	// HolderInvitation.
	Kind string

	// Stage is how far a person or a service account is through
	// enrolment: invited, enrolling, active or suspended. Empty for an
	// invitation.
	Stage string
}

// The kinds of SeatHolder. The first two are the engine's own words for a
// principal's kind (person, machine); an invitation is the engine's holder
// with no principal at all, named here rather than left as an empty kind, so
// that a caller cannot read a holder the engine sent a kind this package does
// not know for as an invitation.
const (
	HolderPerson         = "person"
	HolderServiceAccount = "machine"
	HolderInvitation     = "invitation"
)

func (e *SeatHeldError) Error() string {
	seats := make([]string, 0, len(e.Held))
	for _, handle := range slices.Sorted(maps.Keys(e.Held)) {
		seats = append(seats, handle+" (held by "+e.Held[handle].String()+")")
	}
	message := "the engine refuses to take human seats that are held out of the company: " + strings.Join(seats, "; ")
	if e.Hint != "" {
		message += " (" + e.Hint + ")"
	}
	return message
}

// String names the holder as a status message does, without its login.
func (h SeatHolder) String() string {
	var who string
	switch h.Kind {
	case HolderPerson:
		who = "a person"
	case HolderServiceAccount:
		who = "a service account"
	case HolderInvitation:
		return "an open invitation"
	default:
		who = "a principal of kind " + strconv.Quote(h.Kind)
	}
	if h.Stage != "" {
		who += ", " + h.Stage
	}
	return who
}

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
	Error             string                `json:"error"`
	Message           string                `json:"message"`
	Detail            string                `json:"detail"`
	Hint              string                `json:"hint"`
	Reason            string                `json:"reason"`
	Grants            []string              `json:"grants"`
	Problems          []Problem             `json:"problems"`
	CurrentRevisionID string                `json:"current_revision_id"`
	StoredRevisionID  string                `json:"stored_revision_id"`
	RevisionID        string                `json:"revision_id"`
	ManagedBy         []string              `json:"managed_by"`
	Held              map[string]heldAnswer `json:"held"`
}

// heldAnswer is one holder of a seat as the engine's seat_held refusal sends
// it. Login and the ids are left undecoded: see SeatHolder.
type heldAnswer struct {
	Kind       string `json:"kind"`
	Stage      string `json:"stage"`
	Invitation string `json:"invitation"`
}

func (h heldAnswer) holder() SeatHolder {
	if h.Invitation != "" {
		// The engine names an invitation only where no principal is
		// bound to the seat, and sends it with no kind.
		return SeatHolder{Kind: HolderInvitation}
	}
	return SeatHolder{Kind: h.Kind, Stage: h.Stage}
}

// errorFromResponse turns a non-2xx answer into the package's error for it.
//
// The body is the engine's answer, never the request's, so it cannot carry a
// secret the caller sent: the engine's refusals name a secret by its name and
// never echo a value.
func errorFromResponse(status int, header http.Header, body []byte) error {
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
	if status == http.StatusConflict && r.Error == codeSeatHeld && len(r.Held) > 0 {
		held := make(map[string]SeatHolder, len(r.Held))
		for handle, h := range r.Held {
			held[handle] = h.holder()
		}
		return &SeatHeldError{Held: held, Hint: r.Hint}
	}
	return &APIError{
		StatusCode:        status,
		Code:              r.Error,
		Message:           cmp.Or(r.Detail, r.Message, http.StatusText(status)),
		Hint:              r.Hint,
		CurrentRevisionID: r.CurrentRevisionID,
		StoredRevisionID:  r.StoredRevisionID,
		RevisionID:        r.RevisionID,
		ManagedBy:         r.ManagedBy,
		Reason:            r.Reason,
		Grants:            r.Grants,
		RetryAfter:        retryAfter(status, header),
	}
}

// retryAfter is a 503's Retry-After as a duration, zero when there is none.
// Only the delay-seconds form is read: the engine writes whole seconds, and
// an HTTP date is something else's, whose clock this one does not share.
func retryAfter(status int, header http.Header) time.Duration {
	if status != http.StatusServiceUnavailable {
		return 0
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(header.Get("Retry-After")))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
