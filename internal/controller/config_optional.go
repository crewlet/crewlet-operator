package controller

import (
	"fmt"
	"slices"
	"strings"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// What a Secret read for a tool server or a coding run costs when it holds
// nothing.
//
// Crewlet starts a tool server, and a run_sandbox coding run, only with every
// value it is started with resolved to something: a reference that resolves
// to nothing, or to an empty value, keeps it from starting, unless the
// optionalKeys list that governs the value names its key, when the key is
// left out instead (crewlet#293). The operator reads every Secret before the
// engine does, and says what it read, of every value the engine starts
// something with (render.StartValue.Started): one another value replaces
// wherever it would be read costs nothing, whatever it holds.
//
//   - A Secret or key that does not exist under an optional key holds
//     nothing back, whether or not a Secret another part requires holds the
//     document: the part that gives the value reports it in its
//     ResolvedRefs=True message, as a value it goes without, and so does an
//     empty value there, which the engine leaves out alike. The Company's own
//     setup steps have no ResolvedRefs, so their SecretsSynced message says
//     it. Where the revision the engine runs reads the key as required, or is
//     one the operator does not know the reading of, the engine's store keeps
//     the value it held until a revision that lets it go runs (see
//     config_withdraw.go), and the line says that too.
//   - One under a key that is not optional is a missing reference like any
//     other (SecretNotFound), and holds the document, but only on the parts
//     that read it there: a part that reads the key under an optional key
//     alone does not need it.
//   - An empty value under a key that is not optional is applied, since the
//     engine takes the document and only the server or the run goes without
//     its start, and reported on the part that gives it, ResolvedRefs=False
//     SecretValueEmpty, with a Warning event when it turns so; the Company's
//     own steps in the SecretsSynced message. Without that, the server
//     failing to start would show only on the engine's own dashboard and
//     log: neither GET /fleet, which the operator reads, nor anything else it
//     reads reports a tool server. A Seat whose coding runs it refuses says
//     so on CodeWorkRunnable too (render.CodeWorkOf).

// requires reports whether p reads k somewhere it does not let go without its
// value: one of the references in p's spec that name k, or k's whole Secret,
// that the render did not read as an optional start value of p's (starts).
func (p *part) requires(k secretref.Key, starts []render.StartValue) bool {
	return requiresKey(p.ref, p.refs, k, starts)
}

// requiresKey reports whether refs, the references of the object at owner,
// name k, or k's whole Secret, more often than starts holds optional start
// values of owner's under k: one of them is read where the engine needs it.
func requiresKey(owner render.ObjectRef, refs []secretref.Key, k secretref.Key, starts []render.StartValue) bool {
	n := 0
	for _, ref := range refs {
		if references(ref, k) {
			n++
		}
	}
	for i := range starts {
		if v := &starts[i]; v.Optional && v.Object == owner && v.Key == k {
			n--
		}
	}
	return n > 0
}

// checkSecretValues records on each part what the reconcile read of the
// values the document starts a tool server or a coding run with: the
// optional ones whose Secret or key does not exist (run.missing), or whose
// value is empty (desired.Empty), which the engine leaves out, and the
// required ones whose value is empty, which keep a server or a run from
// starting. The Company's own are recorded for the SecretsSynced message.
func (run *configRun) checkSecretValues(desired secretsync.Desired) {
	missing := map[secretref.Key]*secretsync.MissingError{}
	for _, m := range run.missing {
		missing[m.Key] = m
	}
	empty := desired.Empty()
	run.companyEmpty, run.companyLeftOut = nil, nil
	for i := range run.doc.StartValues {
		v := &run.doc.StartValues[i]
		if !v.Started {
			continue
		}
		p := run.part(v.Object)
		m, gone := missing[v.Key]
		blank := slices.Contains(empty, v.Key)
		switch {
		case v.Optional && (gone || blank):
			what := fmt.Sprintf("Secret %s/%s holds an empty value under %q", v.Key.Namespace, v.Key.Name, v.Key.Key)
			if gone {
				what = absence(m)
			}
			value := leftOutValue{key: v.Key, line: fmt.Sprintf("%s (%s): %s, and %s lists %s", v.Field, v.Name, what,
				run.listOf(v), v.Name)}
			if p == nil {
				run.companyLeftOut = append(run.companyLeftOut, value)
				continue
			}
			p.leftOut = append(p.leftOut, value)
		case !v.Optional && blank:
			finding := v.Field + ": " + run.emptyValue(v)
			if p == nil {
				run.companyEmpty = append(run.companyEmpty, finding)
				continue
			}
			if p.reportedReason == "" {
				p.reportedReason = crewletaiv1alpha1.ReasonSecretValueEmpty
			}
			p.reported = append(p.reported, finding)
		}
	}
}

// absence says which part of a reference does not exist.
func absence(m *secretsync.MissingError) string {
	if m.Reason == secretsync.SecretNotFound {
		return fmt.Sprintf("Secret %s/%s does not exist", m.Key.Namespace, m.Key.Name)
	}
	return fmt.Sprintf("Secret %s/%s has no key %q", m.Key.Namespace, m.Key.Name, m.Key.Key)
}

// listOf names the optionalKeys list that governs v, as the object v is
// written in reads it.
func (run *configRun) listOf(v *render.StartValue) string {
	if v.List == v.Object {
		return "its " + v.ListField
	}
	return fmt.Sprintf("%s %s's %s", v.List.Kind, v.List.Name, v.ListField)
}

// emptyValue is the report of v, a required start value whose Secret key
// holds an empty value: what does not start, and what to do. Leaving the key
// out is offered where Crewlet offers it, and never for a Seat's or a Unit's
// value for a tool server, which is the identity the seat's instance acts as.
func (run *configRun) emptyValue(v *render.StartValue) string {
	var held string
	switch {
	case v.Server == "":
		held = "Crewlet refuses each run_sandbox coding run whose environment it reaches, before a box exists, " +
			"and a box-mode run of a seat's executor starts without it"
	case v.Object.Kind == render.KindSeat:
		held = fmt.Sprintf("the engine starts the seat's instance of MCPServer %s with no value that resolves to nothing, "+
			"so it does not start it", v.Server)
	case v.Object.Kind == render.KindUnit:
		held = fmt.Sprintf("the engine starts the instance of MCPServer %s of no member of the Unit that does not give "+
			"its own value for %s", v.Server, v.Name)
	case run.serverShared(v.Server):
		held = fmt.Sprintf("the engine starts no tool server with a value that resolves to nothing, so it does not "+
			"start MCPServer %s", v.Server)
	default:
		held = fmt.Sprintf("the engine starts the instance of MCPServer %s of no seat that does not give its own "+
			"value for %s", v.Server, v.Name)
	}
	remedy := "Give the key a value"
	if !v.Seat() {
		remedy += fmt.Sprintf(", or list %s in %s to have it left out", v.Name, run.listOf(v))
	}
	return fmt.Sprintf("Secret %s/%s holds an empty value under %q, given as %s: %s. %s",
		v.Key.Namespace, v.Key.Name, v.Key.Key, v.Name, held, remedy)
}

// serverShared reports whether the MCPServer named runs one instance for the
// whole company, as Crewlet's default has it.
func (run *configRun) serverShared(name string) bool {
	if p := run.part(render.ObjectRef{Kind: render.KindMCPServer, Name: name}); p != nil {
		if s, ok := p.obj.(*crewletaiv1alpha1.MCPServer); ok && s.Spec.Shared != nil {
			return *s.Spec.Shared
		}
	}
	return true
}

// leftOutValue is a value a part, or the Company's own setup steps, gives a
// tool server or a coding run under a key an optionalKeys list lets go
// without it, whose Secret or key does not exist or holds nothing: line says
// so, and key is the Secret key.
type leftOutValue struct {
	key  secretref.Key
	line string
}

// leftOutLines are the lines of values, each saying too where the engine's
// store keeps the value it held for a revision an engine pod runs or starts
// on (run.retained).
func (run *configRun) leftOutLines(values []leftOutValue) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.line
		switch {
		case !slices.Contains(run.retained, v.key):
		case run.keptPlainly():
			out[i] += "; the engine keeps the value it held for the revision it runs, which " +
				strings.TrimPrefix(run.keptBecause(false, "the key"), "the revision the engine runs ") +
				", until a revision that lets it go runs"
		default:
			out[i] += "; the engine keeps the value it held, as " + run.keptBecause(false, "the key") +
				", until no engine pod runs a revision that requires it"
		}
	}
	return out
}

// leftOutMessage is a part's ResolvedRefs=True message when it gives values
// it lets go without whose Secret or key does not exist or holds nothing.
func leftOutMessage(leftOut []string) string {
	return "Every object it references exists, and every Secret key but these, each a value it gives under a key " +
		"an optionalKeys list lets go without it: " + strings.Join(leftOut, "; ") + "."
}
