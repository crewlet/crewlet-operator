package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Where an agent seat's executor runs.
//
// Every agent seat's executor is its harness's own agentic run, detached
// from the turn that launched it, in a box (Crewlet's agentLauncher). A launch
// walks the seat's executor chain, spec.llm.default, else the head of the
// document's llm_order, and runs on the first member that drives a harness with
// an in-box runner (ExecutorHarnesses), and is not
// sitting out its spent plan on the node: a member of any other harness is passed
// over silently, as it still serves the seat's other phases. The run goes to
// Crewlet's direct cell, as no provider names a run_in and the document's
// default_run_in is direct (memberCell): a process tree of the engine pod that
// holds the seat, in a box on the pod's sandboxes volume (SandboxDir). It calls
// the seat's tools back through the tool bridge on that pod's own listener, on
// loopback, which every engine pod serves its own boxes: a data pod on its
// public listener, and a seat pod, which has no ingress role, on api.port beside
// its probes, to loopback alone, as the render binds it on every interface
// (Crewlet's boxHostURL). So a run needs no address of the operator's, in any
// pool, and a box is seeded with its provider's login where it signs in with
// one and handed its key or its env where it does not, so any sign-in runs
// there. ADR-0008 records why every run is placed so.
//
// So the executor of a seat is a statement, never a refusal: the member every
// launch runs on while it can, the pod it runs in, and the later runner members
// a launch falls back to while the members before them sit out their plans,
// which the Seat's Ready names. What Crewlet refuses on every write is refused
// here, on the object to change: a seat whose chain has no runner member
// (NoExecutorHarness).
//
// Crewlet decides the launch where no validation shows it, so
// TestMirroredEngineDeclarationsAreUnchanged holds the engine functions
// this copies to the ones it was read against, and fails when one changes.

// ExecutorHarnesses are the harnesses Crewlet runs a seat's executor on, the
// ones it has an in-box runner for: its built-in harness profiles that carry
// an executor block, in their order, to which a test holds this copy. Every
// other harness serves a seat's other phases alone.
var ExecutorHarnesses = []v1alpha1.Harness{
	v1alpha1.HarnessClaudeCode, v1alpha1.HarnessCodex, v1alpha1.HarnessGeminiCLI, v1alpha1.HarnessQwenCode,
	v1alpha1.HarnessOpenCode, v1alpha1.HarnessCopilot, v1alpha1.HarnessGrok, v1alpha1.HarnessMuseCode,
	v1alpha1.HarnessKimiCode, v1alpha1.HarnessHermes, v1alpha1.HarnessPi,
}

// executorHarnessWords names ExecutorHarnesses in a sentence, the last two
// joined by "or", so a message names the harnesses a test holds to the engine.
func executorHarnessWords() string {
	names := make([]string, len(ExecutorHarnesses))
	for i, h := range ExecutorHarnesses {
		names[i] = string(h)
	}
	if n := len(names); n > 1 {
		return strings.Join(names[:n-1], ", ") + " or " + names[n-1]
	}
	return strings.Join(names, "")
}

// Executor is what the operator can tell from the objects about an agent
// seat's executor: the member every launch runs on while it can, and where.
type Executor struct {
	// Member is the provider's name, Platform whether it is one of the
	// EngineClass's platform providers, and Harness the harness it drives.
	Member   string
	Platform bool
	Harness  v1alpha1.Harness
	// Message says what the executor runs on and where, and what a launch
	// falls back to, for a condition message: a clause, after "It ".
	Message string
}

// ExecutorOf is the executor of every Agent seat whose executor chain has a
// member that can run it, keyed by the Seat's name: Crewlet refuses a seat
// whose chain has no such member (NoExecutorHarness). Like ReportedOf it is a
// statement about each Seat's spec, which holds while another object's
// refusal keeps Company from returning a document.
func ExecutorOf(in *Input) map[string]Executor {
	r := newRenderer(in)
	out := map[string]Executor{}
	for i := range in.Seats {
		s := &in.Seats[i]
		if s.Spec.Kind == v1alpha1.SeatKindHuman {
			continue
		}
		if e, ok := r.executor(s); ok {
			out[s.Name] = e
		}
	}
	return out
}

// member is one provider of a seat's executor chain.
type member struct {
	name string
	// field is the field of the Seat that names it, or fieldLLM for the
	// head of the document's llm_order.
	field string
	// platform is true for one of the EngineClass's platform providers.
	platform bool
	// harness is the harness the provider drives.
	harness v1alpha1.Harness
}

func (m *member) describe() string {
	if m.platform {
		return "the platform model provider " + m.name
	}
	return "LLMProvider " + m.name
}

// runs reports whether the member can run a seat's executor.
func (m *member) runs() bool { return slices.Contains(ExecutorHarnesses, m.harness) }

// executorChain resolves a seat's executor chain as Crewlet's phase.Resolve
// does: the providers of spec.llm.default that resolve, in order, or with none
// named the head of the document's llm_order. ok is false when the chain
// names providers none of which resolves, each refused on its own, or when
// the company has no provider at all.
func (r *renderer) executorChain(s *v1alpha1.Seat) ([]member, bool) {
	refs := s.Spec.LLM.Default
	if len(refs) == 0 {
		head, ok := r.documentHead()
		if !ok {
			return nil, false
		}
		return []member{head}, true
	}
	var chain []member
	for i, ref := range refs {
		field := fmt.Sprintf("%s[%d].name", fieldLLMDefault, i)
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				chain = append(chain, r.ownMember(ref.Name, field))
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] != nil {
				chain = append(chain, r.platformMember(ref.Name, field))
			}
		}
	}
	return chain, len(chain) > 0
}

// documentHead is the head of the document's llm_order, as providersDoc writes
// it: the first of the Company's spec.llm.order that resolves, then its own
// providers by name, then the platform's by name. A seat that names no chain
// runs on it alone, since the API refuses a provider named default, which
// Crewlet would run such a seat on ahead of the order.
func (r *renderer) documentHead() (member, bool) {
	for _, ref := range r.in.Company.Spec.LLM.Order {
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				return r.ownMember(ref.Name, fieldLLM), true
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] != nil {
				return r.platformMember(ref.Name, fieldLLM), true
			}
		}
	}
	if own := sortedByName(r.in.LLMProviders); len(own) > 0 {
		return r.ownMember(own[0].Name, fieldLLM), true
	}
	if !r.usesPlatformProviders() {
		return member{}, false
	}
	providers := r.in.Class.Spec.PlatformLLMProviders
	names := make([]string, 0, len(providers))
	for i := range providers {
		names = append(names, providers[i].Name)
	}
	if len(names) == 0 {
		return member{}, false
	}
	return r.platformMember(slices.Min(names), fieldLLM), true
}

// ownMember is the member that is the Company's own LLMProvider name.
func (r *renderer) ownMember(name, field string) member {
	return member{name: name, field: field, harness: r.own[name].Harness}
}

// platformMember is the member that is the EngineClass's platform provider
// name.
func (r *renderer) platformMember(name, field string) member {
	return member{name: name, field: field, platform: true, harness: r.platform[name].Harness}
}

// runnersOf is the members of chain that can run a seat's executor, in chain
// order: the ones a launch picks among (Crewlet's executorRunners).
func runnersOf(chain []member) []member {
	return slices.DeleteFunc(slices.Clone(chain), func(m member) bool { return !m.runs() })
}

// launches reports whether s is an agent seat whose executor a launch can
// run: one with a runner member.
func (r *renderer) launches(s *v1alpha1.Seat) bool {
	if s.Spec.Kind == v1alpha1.SeatKindHuman {
		return false
	}
	chain, ok := r.executorChain(s)
	return ok && len(runnersOf(chain)) > 0
}

// executor is the statement on the agent seat s; false when its chain
// resolves to nothing or holds no member that can run it, each refused on its
// own.
func (r *renderer) executor(s *v1alpha1.Seat) (Executor, bool) {
	chain, ok := r.executorChain(s)
	if !ok {
		return Executor{}, false
	}
	runners := runnersOf(chain)
	if len(runners) == 0 {
		return Executor{}, false
	}
	head := &runners[0]
	return Executor{
		Member: head.name, Platform: head.platform, Harness: head.harness,
		Message: r.runsOn(s, head) + fallbacks(chain, runners),
	}, true
}

// runsOn is what the message says of a launch on m: the pod the run's box is
// made on, which serves it the seat's tools, as the comment at the top of this
// file says.
func (r *renderer) runsOn(s *v1alpha1.Seat, m *member) string {
	where := "the data pod that holds the seat"
	if pool := SeatPool(r.in.Company, s); pool != v1alpha1.DataPool {
		where = fmt.Sprintf("the pod of seat pool %s that holds the seat", pool)
	}
	return fmt.Sprintf("runs the seat's executor as %s on %s%s, in a box on %s, which calls the seat's tools back "+
		"through the tool bridge on the pod's own listener, on loopback", m.harness, m.describe(), r.heads(m), where)
}

// heads is what the message says of a member a seat with no chain of its
// own runs on, which its own spec.llm does not name: the object that does.
func (r *renderer) heads(m *member) string {
	if m.field != fieldLLM {
		return ""
	}
	company := r.in.Company.Name
	if slices.ContainsFunc(r.in.Company.Spec.LLM.Order, func(ref v1alpha1.LLMProviderReference) bool {
		return ref.Name == m.name && (ref.Kind == v1alpha1.LLMProviderKindPlatform) == m.platform
	}) {
		return fmt.Sprintf(", the head of Company %s's spec.llm.order, which a seat whose spec.llm.default is "+
			"empty runs on", company)
	}
	first := fmt.Sprintf("Company %s's first LLMProvider by name", company)
	if m.platform {
		first = fmt.Sprintf("EngineClass %s's first platform model provider by name", r.in.Class.Name)
	}
	return fmt.Sprintf(", %s, which a seat whose spec.llm.default is empty runs on while Company %s's "+
		"spec.llm.order names none", first, company)
}

// fallbacks is the end of a seat's message naming the runner members a
// launch falls back to, and the members that never run the executor, or
// nothing when there are none.
func fallbacks(chain, runners []member) string {
	var out string
	if len(runners) > 1 {
		steps := make([]string, 0, len(runners)-1)
		for i := 1; i < len(runners); i++ {
			steps = append(steps, fmt.Sprintf("%s as %s", runners[i].describe(), runners[i].harness))
		}
		out += fmt.Sprintf(". While %s sits out its spent plan on the node, a launch falls back to %s",
			runners[0].describe(), strings.Join(steps, ", then to "))
	}
	var never []string
	for i := range chain {
		if m := &chain[i]; !m.runs() {
			never = append(never, fmt.Sprintf("%s (%s)", m.describe(), m.harness))
		}
	}
	if len(never) > 0 {
		verb := "never runs"
		if len(never) > 1 {
			verb = "never run"
		}
		out += fmt.Sprintf(". %s %s it, as Crewlet runs a seat's executor only on %s: it serves "+
			"the seat's other phases", strings.Join(never, " and "), verb, executorHarnessWords())
	}
	return out
}

// noExecutor refuses the agent seat s when its executor chain holds no member
// that can run it, as Crewlet refuses it on every write (config's
// validateExecutors): the seat would take its work and fail every turn at the
// launch. A chain read off the seat's own spec.llm.default is refused there; a
// seat that names none, at spec.llm, as it runs on the head of the Company's
// model order, which Crewlet refuses at that provider for every such seat at
// once. A chain naming only providers that do not resolve is refused for
// each of them instead, as Crewlet does.
func (r *renderer) noExecutor(s *v1alpha1.Seat) {
	chain, ok := r.executorChain(s)
	if !ok || len(runnersOf(chain)) > 0 {
		return
	}
	if len(s.Spec.LLM.Default) == 0 {
		head := &chain[0]
		r.errs.add(EngineRefuses, KindSeat, s.Name, fieldLLM, v1alpha1.ReasonNoExecutorHarness, fmt.Sprintf(
			"names no spec.llm.default, so the seat's executor runs on %s%s, and it drives %s: Crewlet runs a "+
				"seat's executor, its harness's own run, only on %s, and refuses a seat that could "+
				"run no turn. Name a provider driving one of them in spec.llm.default, or put one first in the "+
				"Company's spec.llm.order", head.describe(), r.heads(head), head.harness, executorHarnessWords()))
		return
	}
	named := make([]string, len(chain))
	for i := range chain {
		named[i] = fmt.Sprintf("%s (%s)", chain[i].describe(), chain[i].harness)
	}
	r.errs.add(EngineRefuses, KindSeat, s.Name, fieldLLMDefault, v1alpha1.ReasonNoExecutorHarness, fmt.Sprintf(
		"is %s, and none of them drives %s: Crewlet runs a seat's executor, its harness's own run, "+
			"only on those, and refuses a seat that could run no turn. Name a provider driving one of them in "+
			"spec.llm.default, and keep these for the seat's other phases with spec.llm.review, subagent, "+
			"auxiliary or judge, or move one of them to %[2]s", strings.Join(named, ", "), executorHarnessWords()))
}
