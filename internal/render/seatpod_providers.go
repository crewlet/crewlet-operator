package render

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// What a seat pod cannot do that a data pod does: keep a CLI login it was
// given as a credential bundle.
//
// A CLI's login lives under its state directory, which on a data pod is the
// data volume and on a seat pod the scratch volume it loses at every start.
// A credential bundle is restored only into an empty login, so every seat pod
// would restore it at each start and then refresh it on its own, beside the
// data pod: a vendor that rotates refresh tokens retires the token the
// others hold at each refresh, and logs the company out. A headless token
// (tokenSecretRef), an API key, or the model provider's key a Hermes, Pi or
// OpenCode reads from cli.env never refreshes, and is portable: a ${NAME}
// every pod resolves from the fleet's encrypted store. Crewlet applies a
// document that places such a seat in a seat pool, so the refusal is the
// operator's own: applied, it would log out every seat on that CLI login, the
// seats on the data pods included, which is harm beyond the seat at fault.
//
// A seat pod cannot serve a CLI in mode Agent at the head of a seat's
// executor's chain either, as it serves no tool bridge; that seat is applied
// and reported on its Ready (ExecutorOf), since only its own turns fail.

// fieldLLMDefault is a Seat's executor chain, the one every phase without a
// chain of its own runs on.
const fieldLLMDefault = "spec.llm.default"

// fieldLLM stands for the chain of a Seat whose spec.llm.default is empty:
// the head of the document's llm_order, which the Company decides.
const fieldLLM = "spec.llm"

// seatPodProviders refuses the providers an agent seat placed in seat pool
// pool runs on whose login a seat pod cannot keep.
func (r *renderer) seatPodProviders(s *v1alpha1.Seat, pool string) {
	llm := &s.Spec.LLM
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
			r.errs.add(HarmsBeyond, KindSeat, s.Name, link.field, v1alpha1.ReasonCLIAuthNotPortable, fmt.Sprintf(
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
		return []chainLink{{provider: head, field: fieldLLM}}
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
		return chainLink{provider: head, field: fieldLLM}, ok
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
