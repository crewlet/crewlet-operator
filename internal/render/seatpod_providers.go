package render

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// What a seat pod cannot do that a data pod does: run a CLIAgent provider
// whose runs call back into the node that holds the seat, or keep a CLI
// login it was given as a credential bundle.
//
// An agent-mode run calls the seat's tools through the tool bridge of the
// very node that opened its session (Crewlet's
// docs/concepts/code-sandbox.md, "The tool bridge"), at the address the node
// was given as CREWLET_MCP_BRIDGE_URL. The engine serves that bridge on a
// node without the ingress role too, on its api.port beside the probes (or
// on api.public's listener when one is set), and a seat pod binds api.port
// for its probes. What a seat pod lacks is an address: the operator routes
// the company's public address to the data pods alone, and publishes none
// for a seat pod that a remote sandbox could dial, so it gives a seat pod no
// CREWLET_MCP_BRIDGE_URL, and the engine refuses agent mode for a seat there
// at run time, after the revision is applied and while the Seat reports
// Applied. A bridge for a seat pod needs a route and a public address of the
// pod's own, which its session's runs reach and no other pod's; until the
// operator gives it one, the seat is refused here, where the refusal is
// reported before anything runs.
//
// A CLI's login lives under its state directory, which on a data pod is the
// data volume and on a seat pod the scratch volume it loses at every start.
// A credential bundle is restored only into an empty login, so every seat pod
// would restore it at each start and then refresh it on its own, beside the
// data pod: a vendor that rotates refresh tokens retires the token the
// others hold at each refresh, and logs the company out. A headless token
// (tokenSecretRef), an API key, or the model provider's key a Hermes, Pi or
// OpenCode reads from cli.env never refreshes, and is portable: a ${NAME}
// every pod resolves from the fleet's encrypted store.

// fieldLLMDefault is a Seat's executor chain, the one every phase without a
// chain of its own runs on.
const fieldLLMDefault = "spec.llm.default"

// seatPodProviders refuses the providers an agent seat placed in seat pool
// pool runs on that a seat pod cannot serve.
func (r *renderer) seatPodProviders(s *v1alpha1.Seat, pool string) {
	llm := &s.Spec.LLM
	// Crewlet decides a turn's runtime from the head of the executor's chain
	// alone (Registry.Head): an agent-mode CLI there runs the turn as its own
	// agentic run. Anywhere else, behind the head or in another phase, it is
	// reached as a text model, which a seat pod serves like any other.
	if head, ok := r.executorHead(llm.Default, fieldLLMDefault); ok {
		if cli := r.cliProviders[head.provider]; cli != nil && cli.Mode == v1alpha1.CLIAgentModeAgent {
			r.errs.add(KindSeat, s.Name, head.field, v1alpha1.ReasonAgentModeBridgeUnsupported, fmt.Sprintf(
				"runs the seat's work on LLMProvider %s, a CLI in mode Agent at the head of its executor's chain, and "+
					"places the seat in seat pool %s: an agent-mode run calls the seat's tools back through the engine "+
					"pod that holds the seat, and a seat pod serves no tool bridge. Place the seat in data, or put a "+
					"provider in mode Text at the head of the chain",
				head.provider, pool))
		}
	}
	phases := []struct {
		refs  []v1alpha1.LLMProviderReference
		field string
	}{
		{llm.Default, fieldLLMDefault},
		{llm.Review, "spec.llm.review"},
		{llm.Subagent, "spec.llm.subagent"},
		{llm.Auxiliary, "spec.llm.auxiliary"},
		{llm.Judge, "spec.llm.judge"},
		{llm.Sandbox, "spec.llm.sandbox"},
	}
	var refused []string
	for _, phase := range phases {
		refs, field := phase.refs, phase.field
		if len(refs) == 0 {
			// A phase left unset runs on the default chain.
			refs, field = llm.Default, fieldLLMDefault
		}
		for _, link := range r.chainOf(refs, field) {
			cli := r.cliProviders[link.provider]
			if cli == nil || cli.Auth.CredentialBundleSecretRef.Name == "" || slices.Contains(refused, link.provider) {
				continue
			}
			refused = append(refused, link.provider)
			r.errs.add(KindSeat, s.Name, link.field, v1alpha1.ReasonCLIAuthNotPortable, fmt.Sprintf(
				"runs on LLMProvider %s, whose CLI signs in with a credential bundle, and places the seat in seat pool %s: "+
					"a seat pod keeps the login on a volume it loses at every start, restores the bundle each time and "+
					"refreshes it on its own, which logs every pod out once the vendor rotates the refresh token. Sign "+
					"the CLI in with cli.auth.tokenSecretRef (ClaudeCode), auth mode APIKey, or for Hermes, Pi or OpenCode "+
					"its model provider's key in cli.env, in place of the bundle, or place the seat in data",
				link.provider, pool))
		}
	}
}

// chainLink is one provider of a seat's chain, and the field that names it.
type chainLink struct {
	provider string
	field    string
}

// chainOf is the LLMProviders a chain names, with the field of each; an empty
// chain is the one provider a seat with no chain runs on, the head of the
// document's llm_order. Platform providers are left out: none drives a CLI.
func (r *renderer) chainOf(refs []v1alpha1.LLMProviderReference, field string) []chainLink {
	if len(refs) == 0 {
		head, ok := r.orderHead()
		if !ok {
			return nil
		}
		return []chainLink{{provider: head, field: "spec.llm"}}
	}
	var out []chainLink
	for i, ref := range refs {
		if cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) == v1alpha1.LLMProviderKindLLMProvider && r.providers[ref.Name] {
			out = append(out, chainLink{provider: ref.Name, field: fmt.Sprintf("%s[%d].name", field, i)})
		}
	}
	return out
}

// executorHead is the LLMProvider at the head of a seat's executor chain, the
// one Crewlet may run as an agentic run, with the field that names it: the
// first entry of refs that resolves, or with no refs the head of the
// document's llm_order. ok is false when the head is a platform provider,
// which drives no CLI, or when nothing resolves (each such entry is refused
// on its own).
func (r *renderer) executorHead(refs []v1alpha1.LLMProviderReference, field string) (chainLink, bool) {
	if len(refs) == 0 {
		head, ok := r.orderHead()
		return chainLink{provider: head, field: "spec.llm"}, ok
	}
	for i, ref := range refs {
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				return chainLink{provider: ref.Name, field: fmt.Sprintf("%s[%d].name", field, i)}, true
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] {
				return chainLink{}, false
			}
		}
	}
	return chainLink{}, false
}

// orderHead is the LLMProvider a seat with no chain runs on: the head of the
// document's llm_order, as providersDoc writes it (the first of the Company's
// spec.llm.order that resolves, else its own providers by name), when it is
// one of the Company's own. ok is false when it is a platform provider, or
// when there is none.
func (r *renderer) orderHead() (string, bool) {
	for _, ref := range r.in.Company.Spec.LLM.Order {
		switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
		case v1alpha1.LLMProviderKindLLMProvider:
			if r.providers[ref.Name] {
				return ref.Name, true
			}
		case v1alpha1.LLMProviderKindPlatform:
			if r.usesPlatformProviders() && r.platform[ref.Name] {
				return "", false
			}
		}
	}
	own := sortedByName(r.in.LLMProviders)
	if len(own) == 0 {
		return "", false
	}
	return own[0].Name, true
}
