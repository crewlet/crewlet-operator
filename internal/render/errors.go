package render

import (
	"cmp"
	"errors"
	"slices"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// RefError is a reference from one object to another that cannot be rendered
// into the company document: the object named does not exist, the chain of
// parents it starts loops, it names something Crewlet would read as a
// different object or something that cannot be there (a person where the
// engine runs a turn, a provisioned credential nothing provisions), or it
// places a seat in a pool the company's EngineClass does not let it run, or
// whose pods cannot keep the login of a provider it runs on, or runs a seat's
// code work on a CLIAgent provider of another CLI than its coding agent. The
// controller reports it on the object that holds the reference (Kind and
// Name), as ResolvedRefs=False with Reason, as Accepted=False for a Conflict,
// or as Applied=False for a pool the class does not admit (PerSeatNotAllowed,
// PoolCapacityExceeded) or whose pods cannot keep the seat's CLI login
// (CLIAuthNotPortable), and for code work on a CLIAgent provider of another
// CLI (CodingAgentMismatch); a refusal of the Company itself is reported on
// its ConfigApplied.
//
// The company document is applied whole or not at all, so one RefError
// blocks the whole render.
type RefError struct {
	// Kind and Name are the object holding the reference.
	Kind string
	Name string
	// Field is the path of the reference within that object, such as
	// spec.unitRef.name.
	Field string
	// Reason is the condition reason the refusal is reported with:
	// v1alpha1.ReasonRefNotFound, v1alpha1.ReasonCycle,
	// v1alpha1.ReasonConflict, v1alpha1.ReasonReferenceNotAllowed,
	// v1alpha1.ReasonHumanSeat, v1alpha1.ReasonNoProvisioner, or for a
	// placement v1alpha1.ReasonPoolNotFound, v1alpha1.ReasonPerSeatNotAllowed,
	// v1alpha1.ReasonPoolCapacityExceeded, v1alpha1.ReasonSeatPoolsNotAllowed
	// or v1alpha1.ReasonCLIAuthNotPortable, or for a seat's code work
	// v1alpha1.ReasonCodingAgentMismatch.
	Reason string
	// Message says what is wrong and what to do, for a condition message.
	Message string
}

func (e *RefError) Error() string {
	return e.Kind + "/" + e.Name + " " + e.Field + ": " + e.Message
}

// refErrors collects the refusals of one render. They are sorted before they
// are returned, so the same objects always produce the same error.
type refErrors []*RefError

func (r *refErrors) add(kind, name, field, reason, message string) {
	*r = append(*r, &RefError{Kind: kind, Name: name, Field: field, Reason: reason, Message: message})
}

func (r *refErrors) notFound(kind, name, field, message string) {
	r.add(kind, name, field, v1alpha1.ReasonRefNotFound, message)
}

// err joins every refusal into one error, or returns nil when there is none.
// errors.As finds the first *RefError; RefErrors lists them all.
func (r refErrors) err() error {
	if len(r) == 0 {
		return nil
	}
	slices.SortFunc(r, func(a, b *RefError) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Field, b.Field))
	})
	errs := make([]error, len(r))
	for i, e := range r {
		errs[i] = e
	}
	return errors.Join(errs...)
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
