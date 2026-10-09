package render

import (
	"strconv"
	"strings"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// Path is a location in a rendered document, in the form Crewlet reports a
// validation problem's location (its "segments"): object keys as strings and
// list indexes as ints. A map key can hold a dot, so a path is kept as
// segments rather than as the dotted string Crewlet also prints.
type Path []any

// String renders the path the way Crewlet's messages print it: keys joined
// with dots, indexes as [i].
func (p Path) String() string {
	var b strings.Builder
	for _, segment := range p {
		switch s := segment.(type) {
		case int:
			b.WriteString("[" + strconv.Itoa(s) + "]")
		case string:
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s)
		}
	}
	return b.String()
}

// HasPrefix reports whether prefix is the first segments of p.
func (p Path) HasPrefix(prefix Path) bool {
	if len(prefix) > len(p) {
		return false
	}
	for i, segment := range prefix {
		if p[i] != segment {
			return false
		}
	}
	return true
}

// PathOf is the Path of a problem the engine reported, from its segments,
// so a refusal of an applied document can be attributed with Attribute.
//
// The engine client has already taken the segments apart; reading them from
// the client's type rather than from the engine's JSON keeps one decoder of
// that format, where a second one would have to agree with it on every
// edge, such as an index arriving as a JSON number.
func PathOf(segments []engine.Segment) Path {
	if segments == nil {
		return nil
	}
	p := make(Path, len(segments))
	for i, s := range segments {
		if s.IsIndex {
			p[i] = s.Index
		} else {
			p[i] = s.Key
		}
	}
	return p
}

// ObjectRef names the custom resource a part of a document was rendered
// from. Every kind but EngineClass is in the Company's namespace.
type ObjectRef struct {
	// Kind is the resource kind, such as Seat.
	Kind string
	// Name is the object's metadata.name.
	Name string
}

func (o ObjectRef) String() string { return o.Kind + "/" + o.Name }

// The kinds a document is rendered from.
const (
	KindCompany     = "Company"
	KindEngineClass = "EngineClass"
	KindUnit        = "Unit"
	KindSeat        = "Seat"
	KindLLMProvider = "LLMProvider"
	KindMCPServer   = "MCPServer"
	KindIntegration = "Integration"
	KindSchedule    = "Schedule"
)

// Source says that the part of a document at Path, and everything below it
// that no longer Source claims, was rendered from Object.
type Source struct {
	Path   Path
	Object ObjectRef
}

// Attribute returns the object that produced the part of the document at p:
// the Source with the longest path that p starts with. A Unit's Source covers
// its whole subtree, and the Seats and child Units inside it have longer
// Sources of their own, so a problem is attributed to the innermost object.
// It reports false for a path no object produced, such as the empty path of
// a problem Crewlet could not locate.
func (d *Document) Attribute(p Path) (ObjectRef, bool) {
	best := -1
	for i, s := range d.Sources {
		if p.HasPrefix(s.Path) && (best < 0 || len(s.Path) > len(d.Sources[best].Path)) {
			best = i
		}
	}
	if best < 0 {
		return ObjectRef{}, false
	}
	return d.Sources[best].Object, true
}
