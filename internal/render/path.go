package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

// UnmarshalJSON reads the segments array of a Crewlet problem. Decoded into
// a plain []any an index would arrive as a float64, which would never equal
// the int segment a Source holds, so every number is read back as an int.
func (p *Path) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("a document path is an array of keys and indexes: %w", err)
	}
	if raw == nil {
		*p = nil
		return nil
	}
	out := make(Path, len(raw))
	for i, element := range raw {
		element = bytes.TrimSpace(element)
		if len(element) > 0 && element[0] == '"' {
			var key string
			if err := json.Unmarshal(element, &key); err != nil {
				return fmt.Errorf("path segment %d: %w", i, err)
			}
			out[i] = key
			continue
		}
		index, err := strconv.Atoi(string(element))
		if err != nil {
			return fmt.Errorf("path segment %d is %s: want a key or a list index", i, element)
		}
		out[i] = index
	}
	*p = out
	return nil
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
