package render

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// typedKeys are the top-level keys of a company document that fields and
// objects of the API render, the same list the API refuses in a Company's
// engineConfig. Every other top-level key is an engineConfig entry.
var typedKeys = []string{
	"name", "mission", "vision", "policies", "timezone", "tracker", "knowledge", "token_budget",
	keyProviders, keyIntegrations, keyMCPServers, keyRoles, keyUnits,
}

// identityKeys are the keys that name an element of a list of mappings in a
// company document: a role's handle, a unit's id, a tool server's or a setup
// step's name. Diff matches such lists element by element on them, so a
// seat added or removed in the middle of a list is one difference rather
// than a difference at every position after it.
var identityKeys = []string{"handle", "id", "name"}

// Diff compares a rendered company document with the active document an
// engine serves (engine.ActiveConfig.Document) and returns the places where
// they differ, sorted and without duplicates. An empty result means the
// engine runs the rendered document.
//
// The engine does not serve back the bytes it was given: it serves its own
// encoding of the company it decoded, which differs from the rendered one in
// two ways that carry no meaning, and Diff disregards both:
//
//   - A seat's model chain is spelled in its shortest form: a chain of one
//     provider as the provider's name, and an llm mapping that sets only the
//     default phase as that phase's chain.
//   - The engine's tuning blocks, which the Company sets through
//     engineConfig, are served with every setting the engine defaults filled
//     in, and the blocks the Company does not set are served whole. So an
//     engineConfig entry is compared only in what the Company sets: a setting
//     someone changes there is found, while one only added is
//     indistinguishable from a default and is not.
//
// Every block the API's fields and objects render is compared exactly: a
// value changed, added or removed there is a difference. A credential the
// engine holds as anything but a whole ${VAR} reference is served redacted,
// which never equals the rendered reference, so a literal credential written
// over a reference is found too.
//
// Each path is in the rendered document's terms where the place exists
// there, so Document.Attribute names the object it belongs to. A place only
// the active document has (a seat someone added) is reported as the path of
// the nearest enclosing mapping or list the rendered document has.
func Diff(rendered, active []byte) ([]Path, error) {
	var r, a map[string]any
	if err := yaml.Unmarshal(rendered, &r); err != nil {
		return nil, fmt.Errorf("reading the rendered company document: %w", err)
	}
	if err := yaml.Unmarshal(active, &a); err != nil {
		return nil, fmt.Errorf("reading the engine's active company document: %w", err)
	}
	normalizeRoles(r)
	normalizeRoles(a)

	d := differ{}
	for _, key := range unionKeys(r, a) {
		rv, inR := r[key]
		av, inA := a[key]
		typed := slices.Contains(typedKeys, key)
		switch {
		case !inR && !typed:
			// A tuning block the Company leaves to the engine's defaults.
		case !inR || !inA:
			d.add(Path{key})
		default:
			d.compare(Path{key}, rv, av, !typed)
		}
	}
	slices.SortFunc(d.paths, func(x, y Path) int { return strings.Compare(x.String(), y.String()) })
	return slices.CompactFunc(d.paths, func(x, y Path) bool { return x.String() == y.String() }), nil
}

type differ struct {
	paths []Path
}

func (d *differ) add(p Path) {
	d.paths = append(d.paths, slices.Clone(p))
}

// compare records where r and a differ below p. With subset, a mapping in a
// may hold keys r does not: they are engine defaults (see Diff).
func (d *differ) compare(p Path, r, a any, subset bool) {
	switch rv := r.(type) {
	case map[string]any:
		av, ok := a.(map[string]any)
		if !ok {
			d.add(p)
			return
		}
		for _, key := range unionKeys(rv, av) {
			x, inR := rv[key]
			y, inA := av[key]
			switch {
			case !inR && subset:
			case !inR || !inA:
				d.add(append(p, key))
			default:
				d.compare(append(p, key), x, y, subset)
			}
		}
	case []any:
		av, ok := a.([]any)
		if !ok {
			d.add(p)
			return
		}
		d.compareList(p, rv, av, subset)
	default:
		if !reflect.DeepEqual(r, a) {
			d.add(p)
		}
	}
}

// compareList compares two lists, element by element on their identity key
// when both are lists of mappings that have one, by position otherwise.
func (d *differ) compareList(p Path, r, a []any, subset bool) {
	key, ok := identityKey(r, a)
	if !ok {
		if len(r) != len(a) {
			d.add(p)
		}
		for i := range min(len(r), len(a)) {
			d.compare(append(p, i), r[i], a[i], subset)
		}
		return
	}
	inA := make(map[any]any, len(a))
	for _, element := range a {
		inA[element.(map[string]any)[key]] = element
	}
	matched := 0
	for i, element := range r {
		other, found := inA[element.(map[string]any)[key]]
		if !found {
			d.add(append(p, i))
			continue
		}
		matched++
		d.compare(append(p, i), element, other, subset)
	}
	if matched != len(a) {
		// An element only the active document has has no place in the
		// rendered one; the list it is in is the nearest that does.
		d.add(p)
	}
}

// identityKey returns the identity key every element of both lists is a
// mapping with, holding a value that is unique within each list.
func identityKey(r, a []any) (string, bool) {
	if len(r) == 0 || len(a) == 0 {
		return "", false
	}
	for _, key := range identityKeys {
		if uniquelyKeyed(r, key) && uniquelyKeyed(a, key) {
			return key, true
		}
	}
	return "", false
}

func uniquelyKeyed(list []any, key string) bool {
	seen := make(map[string]bool, len(list))
	for _, element := range list {
		m, ok := element.(map[string]any)
		if !ok {
			return false
		}
		id, ok := m[key].(string)
		if !ok || id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// normalizeRoles rewrites the llm field of every role in the document, at
// the top level and in every unit, into the per-phase mapping form with a
// list for every phase, which is how the operator renders it; the engine
// serves the shortest equivalent spelling (see Diff).
func normalizeRoles(doc map[string]any) {
	roles(doc[keyRoles])
	units(doc[keyUnits])
}

func units(v any) {
	list, _ := v.([]any)
	for _, element := range list {
		unit, ok := element.(map[string]any)
		if !ok {
			continue
		}
		roles(unit[keyRoles])
		units(unit["children"])
	}
}

func roles(v any) {
	list, _ := v.([]any)
	for _, element := range list {
		role, ok := element.(map[string]any)
		if !ok {
			continue
		}
		llm, ok := role["llm"]
		if !ok {
			continue
		}
		switch chain := llm.(type) {
		case string, []any:
			role["llm"] = map[string]any{"default": asList(chain)}
		case map[string]any:
			for phase, providers := range chain {
				chain[phase] = asList(providers)
			}
		}
	}
}

// asList is a chain of providers as a list: a single name is a chain of one.
func asList(v any) any {
	if name, ok := v.(string); ok {
		return []any{name}
	}
	return v
}

// unionKeys returns the keys of both mappings, sorted.
func unionKeys(a, b map[string]any) []string {
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
