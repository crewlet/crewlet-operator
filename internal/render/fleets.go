package render

import (
	"cmp"
	"fmt"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// Fleets: a seat's chain or the assistant's model may name one of the
// Company's spec.llm.fleets alone, rendered fleet:<name>, and Crewlet picks the
// member with room for each piece of work. Every verdict the operator draws
// from a chain (the executor's, a seat pod's sign-ins) therefore reads a fleet
// as its members, in list order, since any of them may take the work.

// fleetRefPrefix starts a reference to a fleet where Crewlet accepts one
// providers.llm key (config.FleetRefPrefix); a provider key holds no ':'.
const fleetRefPrefix = "fleet:"

// fleet is the Company's fleet name, and whether it has one: default-fleet
// always exists, with no members until spec.llm.fleets lists it, as Crewlet
// synthesises it.
func (r *renderer) fleet(name string) (*v1alpha1.LLMFleet, bool) {
	fleets := r.in.Company.Spec.LLM.Fleets
	for i := range fleets {
		if fleets[i].Name == name {
			return &fleets[i], true
		}
	}
	if name == v1alpha1.DefaultLLMFleet {
		return &v1alpha1.LLMFleet{Name: name}, true
	}
	return nil, false
}

// resolves reports whether ref names a provider the document holds.
func (r *renderer) resolves(ref v1alpha1.LLMProviderReference) bool {
	switch cmp.Or(ref.Kind, v1alpha1.LLMProviderKindLLMProvider) {
	case v1alpha1.LLMProviderKindLLMProvider:
		return r.providers[ref.Name]
	case v1alpha1.LLMProviderKindPlatform:
		return r.usesPlatformProviders() && r.platform[ref.Name] != nil
	case v1alpha1.LLMProviderKindFleet:
		// A fleet is no provider: links expands it.
	}
	return false
}

// link is one provider a chain runs on: a provider it names, or a member of
// the fleet it names.
type link struct {
	ref v1alpha1.LLMProviderReference
	// field is the field of the referring object that names the provider or
	// its fleet.
	field string
	// fleet is the fleet it is a member of, or "".
	fleet string
}

// links is the providers a written chain at field runs on that the document
// holds, in order: a fleet stands for its members.
func (r *renderer) links(refs []v1alpha1.LLMProviderReference, field string) []link {
	var out []link
	for i, ref := range refs {
		at := fmt.Sprintf("%s[%d].name", field, i)
		if ref.Kind != v1alpha1.LLMProviderKindFleet {
			if r.resolves(ref) {
				out = append(out, link{ref: ref, field: at})
			}
			continue
		}
		out = append(out, r.fleetLinks(ref.Name, at)...)
	}
	return out
}

// fleetLinks is fleet name's members the document holds, each at field.
func (r *renderer) fleetLinks(name, field string) []link {
	f, ok := r.fleet(name)
	if !ok {
		return nil
	}
	var out []link
	for _, m := range f.Members {
		if r.resolves(m) {
			out = append(out, link{ref: m, field: field, fleet: name})
		}
	}
	return out
}

// defaultFleetLinks is what a seat with no chain of its own runs on ahead of
// the document's llm_order: default-fleet's members, once it has one
// (Crewlet's phase.Resolve, SourceFleet).
func (r *renderer) defaultFleetLinks() []link {
	return r.fleetLinks(v1alpha1.DefaultLLMFleet, fieldLLM)
}

// llmFleets renders the Company's spec.llm.fleets. default-fleet is written
// only when listed: Crewlet synthesises it otherwise and never serves it, so
// the document matches the engine's GET /config.
func (r *renderer) llmFleets() map[string]llmFleetDoc {
	fleets := r.in.Company.Spec.LLM.Fleets
	if len(fleets) == 0 {
		return nil
	}
	out := make(map[string]llmFleetDoc, len(fleets))
	for i := range fleets {
		f := &fleets[i]
		doc := llmFleetDoc{
			Name:            cmp.Or(f.DisplayName, f.Name),
			Strategy:        llmFleetStrategy(f.Strategy),
			ThresholdPoints: f.ThresholdPoints,
		}
		for j, m := range f.Members {
			// Crewlet refuses a member that is not a providers.llm key.
			field := fmt.Sprintf("spec.llm.fleets[%d].members[%d]", i, j)
			if key, ok := r.provider(KindCompany, r.in.Company.Name, field, m, absentRefused); ok {
				doc.Members = append(doc.Members, key)
			}
		}
		out[f.Name] = doc
		r.source(Path{keyProviders, "llm_fleets", f.Name}, r.owner)
	}
	return out
}
