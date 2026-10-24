package render

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Whether a seat's executor can run as an agentic run of a CLI in mode
// Agent, and whether such a provider can run one at all.
//
// An agent-mode run happens in the Company's remote sandbox and calls the
// seat's tools back through the tool bridge of the engine node that holds the
// seat, at the address the node was given as CREWLET_MCP_BRIDGE_URL
// (Crewlet's docs/concepts/code-sandbox.md, "The tool bridge"). The operator
// gives that address, the Company's public address, to the data pods alone,
// and only while the Company has one (status.endpoints.publicBaseURL): the
// public address routes to the data pods, and nothing a remote sandbox could
// dial reaches a seat pod. A node with no bridge address builds no bridge,
// and Crewlet refuses every agent-mode turn there (its agentLauncher): the
// turn fails, it does not fall back to the engine's own tool loop.
//
// Crewlet applies a document holding such a seat or provider, and the turns
// of every other seat run. So neither is refused here: refusing them would
// hold the whole company on its last revision over a seat the engine would
// apply and refuse alone (ADR-0005). They are reported instead: a seat that
// can run no turn on its Ready, and a provider whose agent mode cannot run on
// its AgentModeRunnable, since the same provider still serves a text call
// behind a chain's head or in another phase.
//
// Crewlet decides it at a turn, where no validation shows it, so
// TestMirroredEngineDeclarationsAreUnchanged holds the engine functions this
// copies to the ones it was read against, and fails when one changes.

// Executor is what the operator can tell from the objects of a seat whose
// executor's chain is headed by a CLI in mode Agent: whether the node that
// holds the seat serves the tool bridge its every turn calls back through.
type Executor struct {
	// Field is the field of the Seat that names the head, or spec.llm for
	// a seat with no chain of its own, which runs on the head of the
	// document's llm_order: the Company's, which the message names.
	Field string
	// Reason is empty when the seat's turns can run, and otherwise
	// v1alpha1.ReasonAgentModeBridgeUnsupported (the seat is placed in a
	// seat pool) or v1alpha1.ReasonNotExposed (the Company has no public
	// address).
	Reason string
	// Message says what the seat runs on and, when it cannot, what to
	// change, for a condition message after Field.
	Message string
}

// ExecutorOf is the executor of every Agent seat whose executor's chain is
// headed by an LLMProvider in mode Agent, keyed by the
// Seat's name. Crewlet decides a turn's runtime from the head of that chain
// alone (its Registry.Head): an agent-mode CLI anywhere else, behind the head
// or in another phase, is reached as a text model, which every node serves.
// Like CodeWorkOf it is a statement about each Seat's spec, which holds
// while another object's refusal keeps Company from returning a document.
func ExecutorOf(in *Input) map[string]Executor {
	r := newRenderer(in)
	out := map[string]Executor{}
	for i := range in.Seats {
		s := &in.Seats[i]
		if head, _, ok := r.agentModeExecutor(s); ok {
			out[s.Name] = r.executor(s, head)
		}
	}
	return out
}

// agentModeExecutor is the head of an Agent seat's executor chain, with what
// the verdicts read of its CLI, when it is an LLMProvider in mode Agent: the
// seat's every turn is that CLI's own agentic run. Crewlet never offers such
// a seat run_sandbox (its Runner.offersSandbox), so the seat has no code work
// of its own: what it codes, it codes in that run, on that CLI.
func (r *renderer) agentModeExecutor(s *v1alpha1.Seat) (chainLink, cliEntry, bool) {
	if s.Spec.Kind == v1alpha1.SeatKindHuman {
		return chainLink{}, cliEntry{}, false
	}
	head, ok := r.executorHead(s.Spec.LLM.Default, fieldLLMDefault)
	if !ok {
		return chainLink{}, cliEntry{}, false
	}
	spec := r.own[head.provider]
	if spec == nil || spec.Mode != v1alpha1.CLIAgentModeAgent {
		return chainLink{}, cliEntry{}, false
	}
	return head, ownEntry(spec), true
}

// executor is the verdict on a seat whose executor's head is the agent-mode
// CLI head.
func (r *renderer) executor(s *v1alpha1.Seat, head chainLink) Executor {
	e := Executor{Field: head.field}
	pool := SeatPool(r.in.Company, s)
	// A seat with no chain of its own runs on the head of the Company's
	// order, which its own spec.llm does not name: the message names the
	// object that does.
	heads, text := "at the head of its executor's chain", "put a provider in mode Text at the head of the chain"
	if head.field == fieldLLM {
		heads = fmt.Sprintf("that is Company %s's first LLMProvider by name, which a seat whose spec.llm.default is "+
			"empty runs on while the Company's spec.llm.order names none of its own", r.in.Company.Name)
		if slices.ContainsFunc(r.in.Company.Spec.LLM.Order, func(ref v1alpha1.LLMProviderReference) bool {
			return cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) == v1alpha1.LLMProviderKindLLMProvider && ref.Name == head.provider
		}) {
			heads = fmt.Sprintf("at the head of Company %s's spec.llm.order, which a seat whose spec.llm.default is empty "+
				"runs on", r.in.Company.Name)
		}
		text = "name a provider in mode Text first in the Seat's spec.llm.default"
	}
	runs := fmt.Sprintf("runs every turn of the seat as an agentic run of LLMProvider %s, a CLI in mode Agent %s, which "+
		"calls the seat's tools back through the tool bridge of the engine pod that holds the seat", head.provider, heads)
	switch {
	case pool != v1alpha1.DataPool:
		e.Reason = v1alpha1.ReasonAgentModeBridgeUnsupported
		e.Message = fmt.Sprintf("%s, and places the seat in seat pool %s: the operator publishes no address of a seat pod "+
			"that a remote sandbox could dial, so a seat pod serves no tool bridge, and Crewlet refuses each of the seat's "+
			"turns. Place the seat in data, or %s", runs, pool, text)
		if r.in.PublicBaseURL == "" {
			e.Message += fmt.Sprintf("; in data it needs Company %s to have a public address too "+
				"(status.endpoints.publicBaseURL), which it has not", r.in.Company.Name)
		}
	case r.in.PublicBaseURL == "":
		e.Reason = v1alpha1.ReasonNotExposed
		e.Message = fmt.Sprintf("%s, and Company %s has no public address (status.endpoints.publicBaseURL) for the "+
			"sandbox to reach it at, so Crewlet refuses each of the seat's turns. Give its EngineClass a webhooks route, "+
			"or the Company spec.exposure.publicBaseURL, or %s", runs, r.in.Company.Name, text)
	default:
		e.Message = fmt.Sprintf("%s, at %s/mcp/ on the data pods", runs, r.in.PublicBaseURL)
	}
	return e
}

// AgentMode is what the operator can tell from the objects of an LLMProvider
// in mode Agent: whether a run of it can call a seat's tools back at all.
type AgentMode struct {
	// Reason is empty when its runs can reach the tool bridge, and
	// otherwise v1alpha1.ReasonNotExposed.
	Reason string
	// Message says where its runs reach the bridge or, when they cannot,
	// what to change, for a condition message.
	Message string
}

// AgentModeOf is the agent mode of every LLMProvider in mode Agent, keyed by
// the provider's name. Whether a given seat's turns can
// run on it is ExecutorOf's: a seat pod serves no bridge even when the
// Company has a public address.
func AgentModeOf(in *Input) map[string]AgentMode {
	out := map[string]AgentMode{}
	for i := range in.LLMProviders {
		p := &in.LLMProviders[i]
		if p.Spec.Mode != v1alpha1.CLIAgentModeAgent {
			continue
		}
		if in.PublicBaseURL == "" {
			out[p.Name] = AgentMode{Reason: v1alpha1.ReasonNotExposed, Message: fmt.Sprintf(
				"Its runs happen in the Company's sandbox and call the seat's tools back through the engine's public "+
					"address, and Company %s has none (status.endpoints.publicBaseURL), so Crewlet refuses every turn of "+
					"a seat whose executor's chain it heads. Give the Company's EngineClass a webhooks route, or the "+
					"Company spec.exposure.publicBaseURL, or set spec.mode to Text", in.Company.Name)}
			continue
		}
		out[p.Name] = AgentMode{Message: fmt.Sprintf(
			"Its runs happen in the Company's sandbox and call the seat's tools back at %s/mcp/, which the data pods "+
				"serve: a seat placed in a seat pool cannot run on it (see the Seat's Ready)", in.PublicBaseURL)}
	}
	return out
}
