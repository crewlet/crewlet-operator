package render

import (
	"cmp"
	"errors"
	"slices"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// RefError is a reference from one object to another that cannot be rendered
// into the company document: the object named does not exist, the chain of
// parents it starts loops, or it names something Crewlet would read as a
// different object. The controller reports it on the object that holds the
// reference (Kind and Name), as ResolvedRefs=False with Reason, or
// Accepted=False for a Conflict.
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
	// v1alpha1.ReasonRefNotFound, v1alpha1.ReasonCycle or
	// v1alpha1.ReasonConflict.
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
