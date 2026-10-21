package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// Keys that may go without their value.
//
// Crewlet starts a tool server only once every value it is started with
// resolves to something, and refuses a run_sandbox coding run whose
// environment or files hold one that does not: a ${NAME} nothing answers for,
// or one set to an empty value (its toolcredentials.go, resolveServer and
// runEnvironment). A key the block holding the value lists in its
// optional_keys is left out instead, whole, and what it would have been given
// to starts without it. The operator mirrors the field on the three blocks
// that have it (an MCPServer, a Seat's spec.sandbox, a setup step) and follows
// the rule in what it reads of a Secret:
//
//   - A value read from a Secret under an optional key is a StartValue with
//     Optional set, and a Secret key every reference to which is one is in
//     Document.Optional. The secret sync takes a missing Secret or key there
//     for what Kubernetes takes a missing optional secretKeyRef for, a value
//     that is not there: it removes the store entry it pushed for it before,
//     and the controller has a revision activate after, the running one
//     again while the next is held, so the engine resolves the reference to
//     nothing and leaves the key out rather than starting with a deleted
//     Secret's value.
//   - A required StartValue the engine starts something with (Started) whose
//     Secret key holds nothing keeps the server or the run from starting,
//     which the controller reports on the object that gives it
//     (ReasonSecretValueEmpty), and CodeWorkOf on the Seat whose runs it
//     refuses: the engine says so only in its own log and dashboard.
//   - An MCPServer's optionalKeys entry under which no value read from a
//     Secret, or provisioned, is written, by the server or by a Seat's or a
//     Unit's spec.mcp for it, every value there a literal or none at all, is
//     refused here (ReasonOptionalKeyUnused), as Crewlet refuses it on every
//     write (validateOptionalKeys): the key it was meant for stays required,
//     so nothing is left out. The API cannot: a Seat or a Unit may read the
//     key from a Secret where the server writes a literal, and give a Stdio
//     server a key outside a variable's grammar. It refuses what one object
//     shows alone: an HTTP server's entry listed twice in two cases, and an
//     entry of a sandbox block or a step naming nothing its own env or files
//     hold. Every value a sandbox block or a step holds is a reference, so no
//     entry of theirs naming one can be one whose values reference nothing;
//     and no entry can be a variable's name where its key belongs: a store
//     name is the operator's, never a key a user writes.
//   - A Seat's header for an HTTP server spelled unlike its Unit's for the
//     same server is refused (headerSpellings), as Crewlet refuses it
//     (validateHeaderSpellings): it lays a unit's values under a member's by
//     the name as written, and the server would be sent either.
//
// A seat's or a unit's value for a key replaces its server's whole before
// anything is resolved, and an HTTP server's keys are headers, one header in
// any case (Crewlet's MCPServer.ValueKey): optionalKey compares them so.

// StartValue is one Secret key the document gives a tool server or a coding
// run under a key: a variable of a stdio server's environment or of a run's,
// an HTTP server's header, or a setup step's file.
type StartValue struct {
	// Key is the Secret key the value is read from.
	Key secretref.Key
	// Object is the object whose field holds the reference, and Field that
	// field, such as spec.env[0].valueFrom.
	Object ObjectRef
	Field  string
	// Name is the key the value is written under: a variable, a header or a
	// file's path.
	Name string
	// Server is the MCPServer the value is given to; empty for a value of a
	// coding run.
	Server string
	// Optional is whether the optionalKeys list that governs the value lists
	// Name, so that the engine leaves the key out when the value resolves to
	// nothing.
	Optional bool
	// List is the object whose optionalKeys governs the value, and
	// ListField that list's field: the MCPServer's spec.optionalKeys for a
	// value it is started with, its own or a Seat's or a Unit's, a Seat's
	// spec.sandbox.optionalKeys for its env, a setup step's own for its env
	// and files.
	List      ObjectRef
	ListField string
	// Started is whether the engine starts something with the value: a
	// server instance, the company's or a seat's, or a seat's coding runs,
	// for which it is the value that wins its key. A value another one
	// replaces wherever it would be read starts nothing, whatever it holds.
	Started bool
}

// Seat reports whether the value is one of a Seat's or a Unit's spec.mcp for
// a tool server: the identity the seat's instance acts as, which Crewlet
// never offers to leave out (the server's optionalKeys still governs it).
func (v *StartValue) Seat() bool {
	return v.Server != "" && (v.Object.Kind == KindSeat || v.Object.Kind == KindUnit)
}

// StartValuesOf is the StartValues of the objects of in, as Document lists
// them, also while another object's refusal keeps Company from returning a
// document: like ReportedOf it is a statement about the objects' specs. An
// object the render refuses may be one it never reaches, such as a Seat in a
// Unit that does not exist, and none of its references is then a StartValue.
func StartValuesOf(in *Input) []StartValue {
	r := newRenderer(in)
	r.company()
	return r.markStarted()
}

// startUse is where a value the engine starts a tool server or a coding run
// with is written, before it is known to come from a Secret.
type startUse struct {
	object    ObjectRef
	field     string
	name      string
	server    string
	optional  bool
	list      ObjectRef
	listField string
}

// startSecret records ref, read under a start position, and returns the
// pointer the document holds in its place. A reference under a key that is
// not optional is required, as every other reference is.
func (r *renderer) startSecret(use *startUse, ref v1alpha1.SecretKeyReference) string {
	k := secretref.Key{Namespace: r.namespace(), Name: ref.Name, Key: ref.Key}
	r.starts = append(r.starts, StartValue{
		Key: k, Object: use.object, Field: use.field, Name: use.name, Server: use.server, Optional: use.optional,
		List: use.list, ListField: use.listField,
	})
	r.refs[k] = struct{}{}
	if !use.optional {
		r.required[k] = struct{}{}
	}
	return "${" + r.in.StoreName(k) + "}"
}

// headerSpellings refuses a Seat's spec.mcp value for an HTTP server under a
// header its Unit's spec.mcp writes for that server in another case, as
// Crewlet refuses it on every write (its validateHeaderSpellings): Crewlet
// lays a unit's block under each member's by the key as written, so both
// values would be kept, and the seat's instance would send either one on any
// request. One header written twice in one block is the API's to refuse
// (MCPCredentials.env takes a name once in any case), and a Stdio server's
// keys are variables, which an environment tells apart by case.
func (r *renderer) headerSpellings() {
	for _, seat := range sortedByName(r.in.Seats) {
		u := r.units[seat.Spec.UnitRef.Name]
		if u == nil {
			continue
		}
		for i, c := range seat.Spec.MCP {
			server := r.mcp[c.Name]
			if server == nil || server.Spec.Transport != v1alpha1.MCPTransportHTTP {
				continue
			}
			unit := blockValues(ObjectRef{Kind: KindUnit, Name: u.Name}, u.Spec.MCP, c.Name)
			for j, v := range c.Env {
				for _, theirs := range unit.values {
					if theirs.key == v.Name || !strings.EqualFold(theirs.key, v.Name) {
						continue
					}
					r.errs.add(EngineRefuses, KindSeat, seat.Name, fmt.Sprintf("spec.mcp[%d].env[%d].name", i, j),
						v1alpha1.ReasonConflict, fmt.Sprintf("names the header Unit %s's spec.mcp gives MCPServer %s as %s: "+
							"Crewlet lays a seat's value over its unit's only where the two are spelled alike, so the "+
							"seat's instance would send either one, and it refuses the document. Spell them the same",
							u.Name, c.Name, theirs.key))
				}
			}
		}
	}
}

// fieldOptionalKeys is an MCPServer's optionalKeys.
const fieldOptionalKeys = "spec.optionalKeys"

// markStarted sets Started on every StartValue the engine starts something
// with, laid out as Crewlet's resolveServer and runEnvironment lay out what
// each start is given, and returns the StartValues.
//
// A tool server is started as the company's instance, when it is shared,
// with its own values alone; as a seat's instance, for every agent seat a
// per-seat server is granted to by a block of values for it, the seat's own
// or its unit's (Crewlet's MCPServer.Grants); and in a coding run's box, for
// every agent seat whose spec.sandbox.mcpServerRefs names it, shared or not.
// A seat's instance and box are given the server's own values with its
// unit's block laid over them and its own over that, a key compared as the
// server compares its keys (sameKey): Crewlet lays a unit's block under the
// direct agent members' (inheritMCPEnv), and a seat's whole block over the
// server's (resolveServer).
//
// A coding run, the run_sandbox code work of a seat Crewlet offers it to or
// an agent-mode executor's run, is given the Company's setup steps, then the
// seat's, then its spec.sandbox.env, a later variable of the same name
// winning (runEnvironment), and every file of every step.
func (r *renderer) markStarted() []StartValue {
	started := map[ObjectRef]map[string]bool{}
	start := func(o ObjectRef, field string) {
		if started[o] == nil {
			started[o] = map[string]bool{}
		}
		started[o][field] = true
	}
	for _, s := range sortedByName(r.in.MCPServers) {
		shared := s.Spec.Shared == nil || *s.Spec.Shared
		own := ownValues(s)
		if shared {
			startWinners(s, start, own)
		}
		for _, seat := range sortedByName(r.in.Seats) {
			if seat.Spec.Kind == v1alpha1.SeatKindHuman {
				continue
			}
			var unit layer
			if u := r.units[seat.Spec.UnitRef.Name]; u != nil {
				unit = blockValues(ObjectRef{Kind: KindUnit, Name: u.Name}, u.Spec.MCP, s.Name)
			}
			block := blockValues(ObjectRef{Kind: KindSeat, Name: seat.Name}, seat.Spec.MCP, s.Name)
			granted := !shared && (unit.declared || block.declared)
			boxed := seat.Spec.Sandbox != nil && slices.ContainsFunc(seat.Spec.Sandbox.MCPServerRefs,
				func(ref v1alpha1.LocalObjectReference) bool { return ref.Name == s.Name })
			if granted || boxed {
				startWinners(s, start, own, unit, block)
			}
		}
	}
	for _, seat := range sortedByName(r.in.Seats) {
		if _, _, agentMode := r.agentModeExecutor(seat); seat.Spec.Kind == v1alpha1.SeatKindHuman ||
			!r.offersCodeWork(seat) && !agentMode {
			continue
		}
		type at struct {
			object ObjectRef
			field  string
		}
		won := map[string]at{}
		steps := func(owner ObjectRef, setup []v1alpha1.SandboxSetupStep) {
			for i := range setup {
				for j, e := range setup[i].Env {
					won[e.Name] = at{owner, fmt.Sprintf("spec.sandbox.setup[%d].env[%d].valueFrom", i, j)}
				}
				for j := range setup[i].Files {
					start(owner, fmt.Sprintf("spec.sandbox.setup[%d].files[%d].valueFrom", i, j))
				}
			}
		}
		if r.in.Company.Spec.Sandbox.E2B.APIKeySecretRef.Name != "" {
			steps(r.owner, r.in.Company.Spec.Sandbox.Setup)
		}
		owner := ObjectRef{Kind: KindSeat, Name: seat.Name}
		if sb := seat.Spec.Sandbox; sb != nil {
			steps(owner, sb.Setup)
			for j, e := range sb.Env {
				won[e.Name] = at{owner, fmt.Sprintf("spec.sandbox.env[%d].valueFrom", j)}
			}
		}
		for _, w := range won {
			start(w.object, w.field)
		}
	}
	for i := range r.starts {
		v := &r.starts[i]
		v.Started = started[v.Object][v.Field]
	}
	return r.starts
}

// layer is one block of values a tool server is started with: its own, a
// unit's or a seat's, each value by its key and the field it is written at.
// declared is whether the object writes a block for the server at all.
type layer struct {
	object   ObjectRef
	values   []layerValue
	declared bool
}

type layerValue struct {
	key, field string
}

// ownValues is s's own layer, its env or its headers.
func ownValues(s *v1alpha1.MCPServer) layer {
	l := layer{object: ObjectRef{Kind: KindMCPServer, Name: s.Name}, declared: true}
	for j, e := range s.Spec.Env {
		l.values = append(l.values, layerValue{e.Name, fmt.Sprintf("spec.env[%d].valueFrom", j)})
	}
	for j, h := range s.Spec.Headers {
		l.values = append(l.values, layerValue{h.Name, fmt.Sprintf("spec.headers[%d].valueFrom", j)})
	}
	return l
}

// blockValues is the layer of creds, a Seat's or a Unit's spec.mcp at object,
// for the server named.
func blockValues(object ObjectRef, creds []v1alpha1.MCPCredentials, server string) layer {
	l := layer{object: object}
	for i, c := range creds {
		if c.Name != server {
			continue
		}
		l.declared = true
		for j, v := range c.Env {
			l.values = append(l.values, layerValue{v.Name, fmt.Sprintf("spec.mcp[%d].env[%d].valueFrom", i, j)})
		}
	}
	return l
}

// startWinners starts, of layers laid over each other in order, the value
// that wins each of s's keys.
func startWinners(s *v1alpha1.MCPServer, start func(ObjectRef, string), layers ...layer) {
	type won struct {
		object ObjectRef
		field  string
	}
	winners := map[string]won{}
	for _, l := range layers {
		for _, v := range l.values {
			id := v.key
			if s.Spec.Transport == v1alpha1.MCPTransportHTTP {
				id = strings.ToLower(id)
			}
			winners[id] = won{l.object, v.field}
		}
	}
	for _, w := range winners {
		start(w.object, w.field)
	}
}

// optionalKey reports whether s lists key in its optionalKeys, compared as
// Crewlet compares the server's keys: an HTTP server's are headers, which
// differ in more than case or not at all (the API admits only a header
// name's grammar, in which that is ASCII case), and a Stdio server's are as
// written.
func optionalKey(s *v1alpha1.MCPServer, key string) bool {
	return slices.ContainsFunc(s.Spec.OptionalKeys, func(listed string) bool { return sameKey(s, listed, key) })
}

// sameKey reports whether a and b name one of s's keys.
func sameKey(s *v1alpha1.MCPServer, a, b string) bool {
	if s.Spec.Transport == v1alpha1.MCPTransportHTTP {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// mcpOptionalKeys is s's optionalKeys as the document writes them, sorted, and
// refuses each entry no value a Secret or the engine supplies is written
// under: not by s's own env or headers, nor by any Seat's or Unit's spec.mcp
// for s. Crewlet refuses such an entry on every write, since it leaves
// nothing out and the key it was meant for stays required.
func (r *renderer) mcpOptionalKeys(s *v1alpha1.MCPServer) []string {
	if len(s.Spec.OptionalKeys) == 0 {
		return nil
	}
	// referenced are, by listed key, whether a value read from a Secret or
	// provisioned is written under it; literal, who writes a literal under
	// it; and written, every key written, as written, for a Stdio key the
	// list spells in another case.
	referenced, literal := map[string]bool{}, map[string][]string{}
	var written []string
	carry := func(who, name string, isLiteral bool) {
		if !slices.Contains(written, name) {
			written = append(written, name)
		}
		for _, listed := range s.Spec.OptionalKeys {
			if !sameKey(s, listed, name) {
				continue
			}
			if !isLiteral {
				referenced[listed] = true
			} else if !slices.Contains(literal[listed], who) {
				literal[listed] = append(literal[listed], who)
			}
		}
	}
	for _, e := range s.Spec.Env {
		carry("this server's env", e.Name, e.Value != nil)
	}
	for _, h := range s.Spec.Headers {
		carry("this server's headers", h.Name, h.Value != nil)
	}
	blocks := func(who string, creds []v1alpha1.MCPCredentials) {
		for _, c := range creds {
			if c.Name != s.Name {
				continue
			}
			for _, v := range c.Env {
				carry(who, v.Name, v.Value != nil)
			}
		}
	}
	for _, u := range sortedByName(r.in.Units) {
		blocks("Unit "+u.Name, u.Spec.MCP)
	}
	for _, seat := range sortedByName(r.in.Seats) {
		blocks("Seat "+seat.Name, seat.Spec.MCP)
	}

	where := "this server's env"
	if s.Spec.Transport == v1alpha1.MCPTransportHTTP {
		where = "this server's headers"
	}
	for i, key := range s.Spec.OptionalKeys {
		if referenced[key] {
			continue
		}
		message := fmt.Sprintf("names %s, which no value is written under: not %s, nor any Seat's or Unit's spec.mcp "+
			"entry for this server. Nothing is left out for it, and the key it was meant for stays one the server "+
			"is not started without, so Crewlet refuses the document. Name the key a value is read from a Secret "+
			"under, or remove the entry", key, where)
		if writers := literal[key]; len(writers) > 0 {
			message = fmt.Sprintf("names %s, under which every value is a literal (%s): a literal is never missing, so "+
				"nothing is left out for it, and Crewlet refuses the document. Read one of those values from a "+
				"Secret, or remove the entry", key, strings.Join(writers, ", "))
		} else if i := slices.IndexFunc(written, func(w string) bool { return strings.EqualFold(w, key) }); i >= 0 {
			// Only a Stdio server's keys can differ in case alone and be two:
			// an HTTP server's are headers, compared without it.
			message += fmt.Sprintf(" (a Stdio server's keys are variables, which an environment tells apart by "+
				"case: the one written is %s)", written[i])
		}
		r.errs.add(EngineRefuses, KindMCPServer, s.Name, fmt.Sprintf("spec.optionalKeys[%d]", i),
			v1alpha1.ReasonOptionalKeyUnused, message)
	}
	return sortedSet(s.Spec.OptionalKeys)
}

// sortedSet is keys sorted, nil when there are none: an optionalKeys list,
// which the API holds as a set, in an order that does not depend on how it
// was written.
func sortedSet(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	return slices.Sorted(slices.Values(keys))
}
