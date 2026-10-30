package render

import (
	"cmp"
	"errors"
	"slices"
	"strconv"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// RefError is a finding about a reference from one object to another, or
// about what the reference makes of the object: the object named does not
// exist, the chain of parents it starts loops, it names something Crewlet
// would read as a different object or something that cannot be there (a
// person where the engine runs a turn, a provisioned credential nothing
// provisions), or it places a seat in a pool the company does not declare or
// its EngineClass does not let it run, or whose pods cannot keep the login of
// a provider it runs on, or gives a seat an executor chain with no CLI that
// can run its executor, or places a seat's executor runs where the Company
// offers no box (a Company with agent seats and no spec.sandbox, a provider
// placed in an E2B sandbox the Company does not configure), or names a
// person's Atlassian account in a form the company's Jira or Confluence on
// Atlassian Cloud names nobody by.
//
// Its Basis says whether it holds the company's document, and why
// (ADR-0005). A finding that holds is a refusal: the document is applied
// whole or not at all, so one refusal blocks the whole render, and the
// controller reports it on the object that holds the reference (Kind and
// Name), as ResolvedRefs=False with Reason, as Accepted=False for a
// Conflict, or as Applied=False for a pool the class does not admit
// (PerSeatNotAllowed, PoolCapacityExceeded) or whose pods cannot keep the
// seat's CLI login (CLIAuthNotPortable), for a seat whose executor chain has no
// CLI that runs it (NoExecutorCLI), and for a provider placed in a cell the
// Company does not configure (SandboxPlacementNotOffered); a refusal of the
// Company itself is reported on its ConfigApplied (SandboxRequired among them). A Reported finding holds nothing:
// the object is rendered as its spec says, ReportedOf lists the finding, and
// the controller reports it as ResolvedRefs=False with Reason while the
// object is applied.
type RefError struct {
	// Kind and Name are the object holding the reference.
	Kind string
	Name string
	// Field is the path of the reference within that object, such as
	// spec.unitRef.name.
	Field string
	// Reason is the condition reason the finding is reported with:
	// v1alpha1.ReasonRefNotFound, v1alpha1.ReasonCycle,
	// v1alpha1.ReasonConflict, v1alpha1.ReasonReferenceNotAllowed,
	// v1alpha1.ReasonHumanSeat, v1alpha1.ReasonNoProvisioner, or for a
	// placement v1alpha1.ReasonPoolNotFound, v1alpha1.ReasonPerSeatNotAllowed,
	// v1alpha1.ReasonPoolCapacityExceeded, v1alpha1.ReasonSeatPoolsNotAllowed
	// or v1alpha1.ReasonCLIAuthNotPortable, or for a seat's executor
	// v1alpha1.ReasonNoExecutorCLI, v1alpha1.ReasonSandboxRequired or
	// v1alpha1.ReasonSandboxPlacementNotOffered, or for a person's Atlassian
	// account v1alpha1.ReasonNotAnAccountID.
	Reason string
	// Message says what is wrong and what to do, for a condition message.
	Message string
	// Basis is why the finding holds the document, or Reported.
	Basis Basis
}

// Basis is why a finding holds the company's document, or that it does not
// (ADR-0005). One reason can hold on one basis in one place and on another,
// or not at all, in another (an object that does not exist is refused by
// the engine as Datadog's route, and is harm beyond the object as a Unit's
// parent), so each place that records a finding names its own: a
// compile error if it does not, and TestEveryFindingHasABasis holds every
// place to the table of them.
type Basis int

const (
	// EngineRefuses holds a document the engine cannot take as it
	// is: it refuses the document on a write, or the document has no place
	// to write the object (a Schedule of a seat or unit that does not
	// exist). Applying it is impossible, so refusing it here only says so
	// on the object at fault before the engine is asked.
	// TestContractEngineRefusesWhatItHolds holds each place to the engine.
	EngineRefuses Basis = iota + 1
	// HarmsBeyond holds a document that, applied, would harm more than the
	// object at fault: other objects' identity, authority, credentials or
	// work (a unit dropped with its subtree, a reference that resolves
	// another object's value, a login every seat pod refreshes on its own).
	HarmsBeyond
	// ClassPolicy holds a document the EngineClass's administrator forbids:
	// seat pools, PerSeat pools, a seat-pod budget, the platform providers
	// offered.
	ClassPolicy
	// Reported holds nothing: none of the three is true, so the object is
	// rendered as its spec says and the finding is reported on it, since
	// only the object at fault cannot do (part of) its job.
	Reported
)

// Holds reports whether a finding of this basis holds the document.
func (b Basis) Holds() bool {
	return b == EngineRefuses || b == HarmsBeyond || b == ClassPolicy
}

func (b Basis) String() string {
	switch b {
	case EngineRefuses:
		return "EngineRefuses"
	case HarmsBeyond:
		return "HarmsBeyond"
	case ClassPolicy:
		return "ClassPolicy"
	case Reported:
		return "Reported"
	}
	return "Basis(" + strconv.Itoa(int(b)) + ")"
}

func (e *RefError) Error() string {
	return e.Kind + "/" + e.Name + " " + e.Field + ": " + e.Message
}

// refErrors collects the findings of one render. They are sorted before they
// are returned, so the same objects always produce the same error.
type refErrors []*RefError

func (r *refErrors) add(basis Basis, kind, name, field, reason, message string) {
	*r = append(*r, &RefError{Kind: kind, Name: name, Field: field, Reason: reason, Message: message, Basis: basis})
}

func (r *refErrors) notFound(basis Basis, kind, name, field, message string) {
	r.add(basis, kind, name, field, v1alpha1.ReasonRefNotFound, message)
}

// err joins every finding that holds the document into one error, or returns
// nil when there is none. errors.As finds the first *RefError; RefErrors
// lists them all.
func (r refErrors) err() error {
	held := r.sorted(Basis.Holds)
	if len(held) == 0 {
		return nil
	}
	errs := make([]error, len(held))
	for i, e := range held {
		errs[i] = e
	}
	return errors.Join(errs...)
}

// reported is every finding that holds nothing, sorted.
func (r refErrors) reported() []*RefError {
	return r.sorted(func(b Basis) bool { return !b.Holds() })
}

// sorted is the findings whose basis keep accepts, by object and field.
func (r refErrors) sorted(keep func(Basis) bool) []*RefError {
	var out []*RefError
	for _, e := range r {
		if keep(e.Basis) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b *RefError) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Field, b.Field))
	})
	return out
}

// RefErrors lists every *RefError in an error Company returned, so a
// controller can report each one on its own object rather than only the
// first.
func RefErrors(err error) []*RefError {
	parts := []error{err}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		parts = joined.Unwrap()
	}
	var out []*RefError
	for _, part := range parts {
		if ref, ok := errors.AsType[*RefError](part); ok {
			out = append(out, ref)
		}
	}
	return out
}
