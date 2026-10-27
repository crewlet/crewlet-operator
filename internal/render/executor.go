package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Where an agent seat's executor runs, and whether it can.
//
// Every agent seat's executor is its coding CLI's own agentic run, detached
// from the turn that launched it, in a box (Crewlet's agentLauncher). A launch
// walks the seat's executor chain, spec.llm.default, else the head of the
// document's llm_order, and runs on the first member that drives a CLI with
// an in-box runner, ClaudeCode or OpenCode (RunnerCLIs), and is not sitting
// out its spent plan on the node: a member of any other CLI is passed over
// silently, as it still serves the seat's other phases. The run goes to the
// picked member's cell, its runIn, else the Company's
// spec.sandbox.defaultRunIn (agentPlacement):
//
//   - Direct is a process tree of the engine pod that holds the seat. It
//     calls the seat's tools back through the tool bridge on that pod's own
//     listener, on loopback, where the pod serves the route: a node with the
//     ingress and seats roles that binds a listener, the public one, which is
//     a data pod. A seat pod serves no route of its own, so its boxes dial
//     the fleet's address, CREWLET_MCP_BRIDGE_URL, which the operator gives
//     every engine pod while the Company has a public address; without one
//     Crewlet refuses every launch there (BridgeUnreachable). A Direct box is
//     seeded with its provider's login, so any sign-in runs there.
//   - E2B is the Company's E2B sandbox. It dials the fleet's address, the
//     Company's public one, which any data pod answers and relays to the pod
//     holding the seat over the broker (ADR-0056 in Crewlet): without one
//     Crewlet refuses every launch (NotExposed). It is given only the
//     variables that sign its CLI in (signin.go), so a provider whose login a
//     credential bundle restores cannot run there unless the seat's own
//     environment signs its CLI in (SandboxLoginNotPortable).
//
// These two cells are the ones the operator offers, Crewlet's container cell
// not among them, and a Direct box is made on the pod's sandboxes volume
// (SandboxDir): ADR-0008 records why, with the bridge's address every engine
// pod is given.
//
// The verdict judges the first runner member alone, because that is the one
// every launch picks while it can run: a launch the box's login refuses
// fails, it does not move on down the chain. The later runner members are
// named as the fallbacks a launch takes while the members before them sit
// out their plans.
//
// Crewlet applies a document holding a seat its executor cannot run, and the
// turns of every other seat run. So none of the three is refused here:
// refusing it would hold the whole company on its last revision over a seat
// the engine would apply and fail alone (ADR-0005). It is reported on the
// seat's Ready instead. What Crewlet refuses on every write is refused here,
// on the object to change: a seat whose chain has no runner member
// (NoExecutorCLI), a company with an agent seat and a model provider and no
// spec.sandbox (SandboxRequired), and a provider placed in E2B a seat or deep
// work can launch on while the Company configures no e2b
// (SandboxPlacementNotOffered).
//
// Crewlet decides the verdicts at a launch, where no validation shows them,
// so TestMirroredEngineDeclarationsAreUnchanged holds the engine functions
// this copies to the ones it was read against, and fails when one changes.

// RunnerCLIs are the CLIs Crewlet runs a seat's executor on, the ones it has
// an in-box runner for (codingagent.Names, config.CodingAgents), to which a
// test holds this copy. Every other CLI serves a seat's other phases alone.
var RunnerCLIs = []v1alpha1.CLIAgentName{v1alpha1.CLIAgentClaudeCode, v1alpha1.CLIAgentOpenCode}

// Executor is what the operator can tell from the objects about an agent
// seat's executor: the member every launch runs on while it can, where its
// run goes, and whether it can reach the seat's tools from there.
type Executor struct {
	// Field is the field of the Seat that names the member, or spec.llm
	// for a seat with no chain of its own, which runs on the head of the
	// document's llm_order: the Company's, which the message names.
	Field string
	// Member is the provider's name, Platform whether it is one of the
	// EngineClass's platform providers, and CLI the CLI it drives.
	Member   string
	Platform bool
	CLI      v1alpha1.CLIAgentName
	// Placement is the cell the member's runs go to.
	Placement v1alpha1.SandboxPlacement
	// Reason is empty when the seat's turns can run, and otherwise
	// v1alpha1.ReasonNotExposed, v1alpha1.ReasonSandboxLoginNotPortable or
	// v1alpha1.ReasonBridgeUnreachable.
	Reason string
	// Message says what the executor runs on and where and, when it cannot,
	// what to change, for a condition message: a clause, after Field when
	// Reason is set, after "It " when it is not.
	Message string
}

// ExecutorOf is the executor of every Agent seat whose executor chain has a
// member that can run it, keyed by the Seat's name, in a Company whose
// spec.sandbox places its runs: without one Crewlet refuses the document
// (SandboxRequired), as it does a seat whose chain has no such member
// (NoExecutorCLI). Like ReportedOf it is a statement about each Seat's spec,
// which holds while another object's refusal keeps Company from returning a
// document.
func ExecutorOf(in *Input) map[string]Executor {
	r := newRenderer(in)
	out := map[string]Executor{}
	if in.Company.Spec.Sandbox.DefaultRunIn == "" {
		return out
	}
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
	// cli is what the verdicts read of the provider's CLI.
	cli cliEntry
}

func (m *member) describe() string {
	if m.platform {
		return "the platform model provider " + m.name
	}
	return "LLMProvider " + m.name
}

// runs reports whether the member can run a seat's executor.
func (m *member) runs() bool { return slices.Contains(RunnerCLIs, m.cli.agent) }

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
	return member{name: name, field: field, cli: ownEntry(r.own[name])}
}

// platformMember is the member that is the EngineClass's platform provider
// name.
func (r *renderer) platformMember(name, field string) member {
	return member{name: name, field: field, platform: true, cli: platformEntry(r.platform[name])}
}

// runnersOf is the members of chain that can run a seat's executor, in chain
// order: the ones a launch picks among (Crewlet's executorRunners).
func runnersOf(chain []member) []member {
	return slices.DeleteFunc(slices.Clone(chain), func(m member) bool { return !m.runs() })
}

// launches reports whether s is an agent seat whose executor a launch can
// run: one with a runner member in a Company whose spec.sandbox places its
// runs.
func (r *renderer) launches(s *v1alpha1.Seat) bool {
	if s.Spec.Kind == v1alpha1.SeatKindHuman || r.in.Company.Spec.Sandbox.DefaultRunIn == "" {
		return false
	}
	chain, ok := r.executorChain(s)
	return ok && len(runnersOf(chain)) > 0
}

// cellOf is the cell a run on m goes to: its runIn, else the Company's
// spec.sandbox.defaultRunIn (Crewlet's agentPlacement). A platform provider
// names none.
func (r *renderer) cellOf(m *member) v1alpha1.SandboxPlacement {
	return cmp.Or(m.cli.runIn, r.in.Company.Spec.Sandbox.DefaultRunIn)
}

// executor is the verdict on the agent seat s; false when its chain resolves
// to nothing or holds no member that can run it, each refused on its own.
func (r *renderer) executor(s *v1alpha1.Seat) (Executor, bool) {
	chain, ok := r.executorChain(s)
	if !ok {
		return Executor{}, false
	}
	runners := runnersOf(chain)
	if len(runners) == 0 {
		return Executor{}, false
	}
	env, leftOut := r.runEnvNames(s)
	head := &runners[0]
	e := Executor{
		Field: head.field, Member: head.name, Platform: head.platform, CLI: head.cli.agent,
		Placement: r.cellOf(head),
	}
	e.Reason, e.Message = r.verdict(s, head, env, leftOut)
	e.Message += r.fallbacks(s, chain, runners, env, leftOut)
	return e, true
}

// verdict is the reason and the message of a launch on m, as the comment at
// the top of this file says.
func (r *renderer) verdict(s *v1alpha1.Seat, m *member, env, leftOut envNames) (reason, message string) {
	company := r.in.Company.Name
	public := r.in.PublicBaseURL
	pool := SeatPool(r.in.Company, s)
	runs := fmt.Sprintf("runs the seat's executor as %s on %s%s", m.cli.agent, m.describe(), r.heads(m))
	noAddress := fmt.Sprintf("Company %s has no public address (status.endpoints.publicBaseURL)", company)
	exposeIt := "Give the Company's EngineClass a webhooks route, or the Company spec.exposure.publicBaseURL"
	switch r.cellOf(m) {
	case v1alpha1.SandboxPlacementDirect:
		switch {
		case pool == v1alpha1.DataPool:
			return "", runs + ", Direct, on the data pod that holds the seat, whose box calls the seat's tools back " +
				"through the tool bridge on the pod's own loopback listener"
		case public == "":
			return v1alpha1.ReasonBridgeUnreachable, fmt.Sprintf("%s, Direct, on a pod of seat pool %s, and %s: a seat "+
				"pod serves no tool bridge of its own, so its boxes call the seat's tools back at the Company's public "+
				"address, and Crewlet refuses each launch of the seat's executor there without one. %s, or place the "+
				"seat in data, whose pods serve the bridge to their own boxes", runs, pool, noAddress, exposeIt)
		}
		return "", fmt.Sprintf("%s, Direct, on the pod of seat pool %s that holds the seat, whose box calls the seat's "+
			"tools back at %s/mcp/, where a data pod relays each call to it", runs, pool, public)
	default:
		if public == "" {
			return v1alpha1.ReasonNotExposed, fmt.Sprintf("%s, in the Company's E2B sandbox, and %s for the box to call "+
				"the seat's tools back at, so Crewlet refuses each launch of the seat's executor. %s, or %s",
				runs, noAddress, exposeIt, r.directRemedy(m, pool))
		}
		own, seat := m.cli.remoteSignIn(env)
		bridged := fmt.Sprintf("%s, in the Company's E2B sandbox, which calls the seat's tools back at %s/mcp/, where "+
			"a data pod relays each call to the engine pod holding the seat", runs, public)
		switch {
		case len(seat) > 0 && m.cli.agent == v1alpha1.CLIAgentClaudeCode:
			return "", fmt.Sprintf("%s, signed in by %s from the Company's or the seat's spec.sandbox, which Crewlet "+
				"hands the run in place of the provider's own sign-in", bridged, strings.Join(seat, ", "))
		case own:
			return "", bridged
		case len(seat) > 0:
			return "", fmt.Sprintf("%s, signed in by %s from the Company's or the seat's spec.sandbox, as the "+
				"provider's own %s login cannot follow the run there", bridged, strings.Join(seat, ", "), m.cli.agent)
		}
		_, lost := m.cli.remoteSignIn(leftOut)
		return v1alpha1.ReasonSandboxLoginNotPortable, fmt.Sprintf("%s, in the Company's E2B sandbox, where %s's %s login "+
			"cannot follow it: a credential bundle stays on the engine's data volume, as it carries a refresh token "+
			"the engine keeps rotating, and nothing in the run's environment signs %s in, so Crewlet refuses each "+
			"launch of the seat's executor. To fix it, %s, or %s%s", runs, m.describe(), m.cli.agent, m.cli.agent,
			signInRemedy(m), r.directRemedy(m, pool), leftOutNote(lost))
	}
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

// signInRemedy is how to sign m's CLI in for a run in E2B.
func signInRemedy(m *member) string {
	if m.cli.agent == v1alpha1.CLIAgentOpenCode {
		return "put its model provider's key in the provider's env, or in the seat's spec.sandbox.env, under a " +
			"name Crewlet reads as a credential, such as OPENROUTER_API_KEY"
	}
	return fmt.Sprintf("sign the provider in with auth.tokenSecretRef or auth mode APIKey, or sign ClaudeCode in "+
		"for the seat in spec.sandbox.env (%s)", strings.Join(claudeCodeSignIn, ", "))
}

// directRemedy is how to run m's executor runs Direct, which reach the seat's
// tools on a data pod without a public address and are seeded with the
// provider's login.
func (r *renderer) directRemedy(m *member, pool string) string {
	var how string
	switch {
	case m.platform:
		how = "set the Company's spec.sandbox.defaultRunIn to Direct, as a platform provider names no runIn,"
	case m.cli.runIn != "":
		how = fmt.Sprintf("set LLMProvider %s's spec.runIn to Direct,", m.name)
	default:
		how = fmt.Sprintf("set LLMProvider %s's spec.runIn, or the Company's spec.sandbox.defaultRunIn, to Direct,", m.name)
	}
	if pool != v1alpha1.DataPool {
		how += " and place the seat in data,"
	}
	return how + " to run it Direct on the data pod that holds the seat, seeded with the provider's login"
}

// fallbacks is the end of a seat's message naming the runner members a
// launch falls back to, each with what a run there would meet, and the
// members that never run the executor, or nothing when there are none.
func (r *renderer) fallbacks(s *v1alpha1.Seat, chain, runners []member, env, leftOut envNames) string {
	var out string
	if len(runners) > 1 {
		steps := make([]string, 0, len(runners)-1)
		for i := 1; i < len(runners); i++ {
			m := &runners[i]
			step := fmt.Sprintf("%s as %s, %s", m.describe(), m.cli.agent, cellWords(r.cellOf(m)))
			if reason, _ := r.verdict(s, m, env, leftOut); reason != "" {
				step += fmt.Sprintf(", where Crewlet would refuse it too (%s)", reason)
			}
			steps = append(steps, step)
		}
		out += fmt.Sprintf(". While %s sits out its spent plan on the node, a launch falls back to %s",
			runners[0].describe(), strings.Join(steps, ", then to "))
	}
	var never []string
	for i := range chain {
		if m := &chain[i]; !m.runs() {
			never = append(never, fmt.Sprintf("%s (%s)", m.describe(), m.cli.agent))
		}
	}
	if len(never) > 0 {
		verb := "never runs"
		if len(never) > 1 {
			verb = "never run"
		}
		out += fmt.Sprintf(". %s %s it, as Crewlet runs a seat's executor only on ClaudeCode or OpenCode: it serves "+
			"the seat's other phases", strings.Join(never, " and "), verb)
	}
	return out
}

// cellWords names a cell in a sentence.
func cellWords(p v1alpha1.SandboxPlacement) string {
	if p == v1alpha1.SandboxPlacementDirect {
		return "Direct"
	}
	return "in the Company's E2B sandbox"
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
		r.errs.add(EngineRefuses, KindSeat, s.Name, fieldLLM, v1alpha1.ReasonNoExecutorCLI, fmt.Sprintf(
			"names no spec.llm.default, so the seat's executor runs on %s%s, and it drives %s: Crewlet runs a "+
				"seat's executor, its CLI's own run, only on ClaudeCode or OpenCode, and refuses a seat that could "+
				"run no turn. Name a provider driving one of them in spec.llm.default, or put one first in the "+
				"Company's spec.llm.order", head.describe(), r.heads(head), head.cli.agent))
		return
	}
	named := make([]string, len(chain))
	for i := range chain {
		named[i] = fmt.Sprintf("%s (%s)", chain[i].describe(), chain[i].cli.agent)
	}
	r.errs.add(EngineRefuses, KindSeat, s.Name, fieldLLMDefault, v1alpha1.ReasonNoExecutorCLI, fmt.Sprintf(
		"is %s, and none of them drives ClaudeCode or OpenCode: Crewlet runs a seat's executor, its CLI's own run, "+
			"only on those, and refuses a seat that could run no turn. Name a provider driving one of them in "+
			"spec.llm.default, and keep these for the seat's other phases with spec.llm.review, subagent, "+
			"auxiliary or judge, or move one of them to ClaudeCode or OpenCode", strings.Join(named, ", ")))
}

// sandboxRequired refuses the Company when it has an agent seat and a model
// provider and no spec.sandbox, as Crewlet refuses providers.sandbox missing
// (config's validateSandboxPlacement): every such seat's executor runs in a
// box, and the company would configure none. models is whether the document
// holds a provider. A company with neither an agent seat nor a provider runs,
// and holds its seats' work until a revision gives it both.
func (r *renderer) sandboxRequired(models bool) {
	if !models || r.in.Company.Spec.Sandbox.DefaultRunIn != "" {
		return
	}
	var agents []string
	for _, s := range r.seats {
		if s.Spec.Kind != v1alpha1.SeatKindHuman {
			agents = append(agents, SeatHandle(s))
		}
	}
	if len(agents) == 0 {
		return
	}
	slices.Sort(agents)
	r.errs.add(EngineRefuses, KindCompany, r.in.Company.Name, "spec.sandbox", v1alpha1.ReasonSandboxRequired, fmt.Sprintf(
		"is unset, and every agent seat's executor runs as its coding CLI's own run, in a box spec.sandbox "+
			"configures: %s, so Crewlet refuses the document. Set spec.sandbox.defaultRunIn to Direct to run the "+
			"executors on the engine pods that hold the seats, or to E2B with spec.sandbox.e2b to run them in E2B",
		seatsSentence(agents)))
}

// seatsSentence names the first of seats and how many more, for a refusal
// that one edit fixes for every one of them.
func seatsSentence(seats []string) string {
	out := fmt.Sprintf("seat %s has one", seats[0])
	switch more := len(seats) - 1; {
	case more == 1:
		out += ", and 1 more seat does too"
	case more > 1:
		out += fmt.Sprintf(", and %d more seats do too", more)
	}
	return out
}

// unofferedPlacements refuses every LLMProvider with runIn E2B that a launch
// can land on while the Company's spec.sandbox configures no e2b, as Crewlet
// refuses each at its run_in on every write: a ClaudeCode or OpenCode member
// of an agent seat's executor chain (config's validateSandboxPlacement, over
// every runner member, the fallbacks included), and the provider deep work
// runs on (config's deepWorkProblems). Without spec.sandbox the document is
// refused for that instead (sandboxRequired), as Crewlet asks nothing of a
// cell then.
func (r *renderer) unofferedPlacements() {
	sb := &r.in.Company.Spec.Sandbox
	if sb.DefaultRunIn == "" || sb.E2B.APIKeySecretRef.Name != "" {
		return
	}
	seats := map[string][]string{}
	for _, s := range sortedByName(r.in.Seats) {
		if s.Spec.Kind == v1alpha1.SeatKindHuman {
			continue
		}
		chain, ok := r.executorChain(s)
		if !ok {
			continue
		}
		for _, m := range runnersOf(chain) {
			if !m.platform && m.cli.runIn == v1alpha1.SandboxPlacementE2B && !slices.Contains(seats[m.name], SeatHandle(s)) {
				seats[m.name] = append(seats[m.name], SeatHandle(s))
			}
		}
	}
	var deep string
	if d := r.in.Company.Spec.Assistant.DeepWork.LLM; cmp.Or(d.Kind, v1alpha1.LLMProviderKindLLMProvider) ==
		v1alpha1.LLMProviderKindLLMProvider && r.own[d.Name] != nil && r.own[d.Name].RunIn == v1alpha1.SandboxPlacementE2B {
		deep = d.Name
	}
	for _, p := range sortedByName(r.in.LLMProviders) {
		var whose []string
		if handles := seats[p.Name]; len(handles) > 0 {
			slices.Sort(handles)
			named := "seat " + handles[0]
			switch more := len(handles) - 1; {
			case more == 1:
				named += " and 1 more seat"
			case more > 1:
				named += fmt.Sprintf(" and %d more seats", more)
			}
			whose = append(whose, fmt.Sprintf("the executor of %s can launch on it", named))
		}
		if p.Name == deep {
			whose = append(whose, "the Company's deep work runs on it (spec.assistant.deepWork.llm)")
		}
		if len(whose) == 0 {
			continue
		}
		r.errs.add(EngineRefuses, KindLLMProvider, p.Name, "spec.runIn", v1alpha1.ReasonSandboxPlacementNotOffered, fmt.Sprintf(
			"is E2B, and Company %s's spec.sandbox configures no e2b, while %s: Crewlet refuses a run placed in a "+
				"cell nothing configures. Configure the Company's spec.sandbox.e2b, or set spec.runIn to Direct, or "+
				"remove it to take the Company's spec.sandbox.defaultRunIn", r.in.Company.Name, strings.Join(whose, ", and ")))
	}
}

// offersDirect reports whether a run can be placed Direct: the Company's
// default is, or one of its LLMProviders names it. The document configures
// the direct cell only then, so it says which cells the Company offers.
func (r *renderer) offersDirect() bool {
	if r.in.Company.Spec.Sandbox.DefaultRunIn == v1alpha1.SandboxPlacementDirect {
		return true
	}
	return slices.ContainsFunc(r.in.LLMProviders, func(p v1alpha1.LLMProvider) bool {
		return p.Spec.RunIn == v1alpha1.SandboxPlacementDirect
	})
}
