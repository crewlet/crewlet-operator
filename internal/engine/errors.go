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
	// between this write's read and its activation. The engine answers it
	// 412 to a write whose If-Match named a revision, as the operator's
	// writes over a running document do, and 409 to one that named none, as
	// its activation of the running revision again (ReloadConfig) does; a
	// create-only write that loses its race is ErrAlreadyConfigured instead.
	// Re-read the active configuration and render again; the engine may have
	// kept this write's revision as inert history.
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

	// ErrUnavailable is a request the engine could not answer now (503): it
	// could not decide who is asking, or could not read what the request
	// needs, such as who holds a seat a write takes away or a store of its
	// fleet's that did not answer. The *APIError behind it carries
	// RetryAfter where the engine said when to ask again. Where it said
	// nothing, no wait clears what it met, and its message says so: a
	// condition of the node, such as a record it can't read yet, that
	// somebody clears in the engine, whom the detail names where it can.
	//
	// A config write can be one too after the engine has stored its
	// revision: the activation was sent to the fleet's coordination store,
	// whose answer never came back, so the engine may run the revision or
	// may not. That 503 carries the Retry-After of an undecided answer, when
	// a read may tell, and names the revision in StoredRevisionID and the
	// node that stored it in its hint: UnconfirmedActivation tells it from
	// every other 503. What settles it is a read of the fleet's
	// activation (Fleet.Activation), before the write is sent again: once
	// the first did activate, the same write from the same base loses the
	// race to it and is refused ErrRevisionAdvanced, and a write built on
	// the revision it stored activates the same document a second time.
	ErrUnavailable = errors.New("the engine could not answer now")

	// ErrRevisionUnreadable is a revision of the company document that the
	// engine cannot open (409): sealed under a key the keyring no longer
	// holds (unreadable_revision), or, met through the fleet's activation
	// pointer, under a key this node was never given
	// (unreadable_fleet_revision); sealed in a form this build of the engine
	// does not read, most likely by a newer build, in either place
	// (unknown_seal_revision); or stored without a seal, which no node reads
	// (unsealed_revision, unsealed_fleet_revision). The engine answers it to
	// a read of the document and to every write built from it, and sends no
	// Retry-After: no wait opens the revision. A sealed one is somebody's
	// document, which no write replaces, since that would discard it unread:
	// what opens it is given to the engine, a key put in its keyring, which
	// it reads when it starts, or the build that sealed it. One without a
	// seal no key opens, and the one write that goes on over it is a whole
	// document (ApplyConfig), which reads nothing from it: Unsealed reports
	// it. The *APIError behind it carries the revision in RevisionID, the
	// engine's account as its Message and the remedy as its Hint.
	ErrRevisionUnreadable = errors.New("the engine cannot open a revision of its company document")

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

// codesRevisionUnreadable are the refusals of a revision the engine cannot
// open, one per cause and origin because the remedies share nothing: a key
// put back in the keyring (unreadable_revision), a key the fleet seals under
// given to this node (unreadable_fleet_revision), the build that sealed it
// (unknown_seal_revision, one code for either origin, since that remedy is
// the same for both), or a whole document replacing it (codesUnsealed).
var codesRevisionUnreadable = append([]string{
	"unreadable_revision", "unreadable_fleet_revision", "unknown_seal_revision",
}, codesUnsealed...)

// codesUnsealed are the refusals of a revision stored without a seal, this
// node's copy (unsealed_revision) or the fleet's (unsealed_fleet_revision):
// the one cause a write replaces (Unsealed).
var codesUnsealed = []string{"unsealed_revision", "unsealed_fleet_revision"}

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
	// one and did not confirm activating it: inert history beside a lost
	// race (ErrRevisionAdvanced, ErrAlreadyConfigured), or, on a 503
	// (ErrUnavailable, read by UnconfirmedActivation), the revision whose
	// activation the engine sent and could not confirm, which the fleet may
	// run or may not.
	StoredRevisionID string

	// RevisionID is the revision a refusal is about, when the engine named
	// one: the revision it cannot open (ErrRevisionUnreadable).
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

	// Outcome is what the engine knows of a write a 503 answers: "unknown"
	// for one it may or may not have landed, empty for every other answer.
	Outcome string
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
		// By status alone: a 503 is "not now" whoever wrote it, and the
		// engine writes it under several codes (unavailable,
		// identity_unavailable, seat_holders_unavailable and the stores'
		// own), each retried the same way, whether it said when or that
		// no wait clears it: nothing else tells the caller that somebody
		// has cleared it. A draining engine's 503 is a *NotReadyError
		// instead.
		return e.StatusCode == http.StatusServiceUnavailable
	case ErrRevisionAdvanced:
		// By code alone: the engine writes it 412 for a write that named
		// the revision it was built on and 409 for one that named none,
		// and either way what moved is the company's revision.
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

// Unsealed is the engine's active revision, one it stores without a seal, and
// true, when err is the engine's refusal to open it (ErrRevisionUnreadable
// under unsealed_revision or unsealed_fleet_revision, naming the revision).
// The fleet's pointer names it, and no node runs it: each runs the revision it
// applied before.
//
// It is the one revision the engine cannot open that a write replaces: a
// whole document sent with ApplyConfig goes on over it, reading nothing from
// it, so no masked value is restored from it and no field carried, and the
// engine judges the write as one replacing nothing. The revision is still the
// write's base: a dry run names it as BaseRevisionID, and IfMatch is judged
// against it. Every other write is built from the active document and is
// refused over it as a read is. A revision sealed in any way the engine cannot
// open (unreadable_revision, unreadable_fleet_revision,
// unknown_seal_revision) is refused for every write, ApplyConfig included,
// since replacing it would discard a document somebody sealed, and is not
// reported here. Nor is a refusal that names no revision, on which no write
// can be conditioned; the engine always names one.
func Unsealed(err error) (string, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !slices.Contains(codesUnsealed, apiErr.Code) || apiErr.RevisionID == "" {
		return "", false
	}
	return apiErr.RevisionID, true
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

// Waitless reports whether err is the engine's 503 for a condition no wait
// clears: one under a code of its own with no Retry-After, which the engine
// sends only when it knows waiting changes nothing (a record a node can't
// read yet, a node the fleet evicted), and words so ("This can't be done
// here, and waiting won't change that."). Somebody clears it in the engine,
// whom its detail names where it can tell. A write whose outcome the engine
// does not know is not one, though it carries no Retry-After either: it may
// have landed, and the engine's answer says to try it again or ask another
// node (UnconfirmedActivation reads the one a config write gets). Nor is a
// 503 that names no code, which something in front of the engine wrote.
func Waitless(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusServiceUnavailable && apiErr.Code != "" &&
		apiErr.RetryAfter == 0 && apiErr.Outcome != outcomeUnknown
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
	Outcome           string                `json:"outcome"`
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
		Outcome:           r.Outcome,
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
