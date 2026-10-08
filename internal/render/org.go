package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The org chart: Units become Crewlet's units, nested under their parents
// as children, and Seats become roles, inside their Unit's entry or at the
// top level when they have none. Siblings are ordered by metadata.name and
// seats by handle, so the document never depends on the order objects were
// listed in.

// org renders the top-level roles and units.
func (r *renderer) org() ([]roleDoc, []unitDoc) {
	children := map[string][]*v1alpha1.Unit{}
	var roots []*v1alpha1.Unit
	for _, u := range r.units {
		parent := u.Spec.ParentRef.Name
		switch {
		case parent == "":
			roots = append(roots, u)
		case r.units[parent] == nil:
			r.errs.notFound(KindUnit, u.Name, "spec.parentRef.name", fmt.Sprintf(
				"names Unit %s as the parent, which does not exist in namespace %s", parent, r.namespace()))
		case r.inCycle(u):
			r.errs.add(KindUnit, u.Name, "spec.parentRef.name", v1alpha1.ReasonCycle, fmt.Sprintf(
				"its chain of parents, through Unit %s, leads back to it: point spec.parentRef of one Unit in the chain elsewhere, or remove it", parent))
		default:
			children[parent] = append(children[parent], u)
		}
	}

	members := map[string][]*v1alpha1.Seat{}
	var rootSeats []*v1alpha1.Seat
	for _, s := range r.seats {
		unit := s.Spec.UnitRef.Name
		switch {
		case unit == "":
			rootSeats = append(rootSeats, s)
		case r.units[unit] == nil:
			r.errs.notFound(KindSeat, s.Name, "spec.unitRef.name", fmt.Sprintf(
				"names Unit %s, which does not exist in namespace %s", unit, r.namespace()))
		default:
			members[unit] = append(members[unit], s)
		}
	}

	t := tree{children: children, members: members}
	return r.roles(Path{"roles"}, rootSeats), r.unitList(Path{"units"}, roots, t)
}

// tree is the org chart indexed by parent: each Unit's child Units and its
// member Seats, by the parent's metadata.name.
type tree struct {
	children map[string][]*v1alpha1.Unit
	members  map[string][]*v1alpha1.Seat
}

// inCycle reports whether u's chain of parents leads back to u. A Unit whose
// chain only runs into a cycle further up is not in one: it is unreachable
// from the top of the chart, and the Units of the cycle carry the refusal.
func (r *renderer) inCycle(u *v1alpha1.Unit) bool {
	seen := map[string]bool{}
	for p := r.units[u.Spec.ParentRef.Name]; p != nil && !seen[p.Name]; p = r.units[p.Spec.ParentRef.Name] {
		if p.Name == u.Name {
			return true
		}
		seen[p.Name] = true
	}
	return false
}

func (r *renderer) unitList(at Path, units []*v1alpha1.Unit, t tree) []unitDoc {
	slices.SortFunc(units, func(a, b *v1alpha1.Unit) int { return strings.Compare(a.Name, b.Name) })
	out := make([]unitDoc, 0, len(units))
	for i, u := range units {
		out = append(out, r.unit(append(slices.Clone(at), i), u, t))
	}
	return out
}

func (r *renderer) unit(at Path, u *v1alpha1.Unit, t tree) unitDoc {
	r.source(at, ObjectRef{Kind: KindUnit, Name: u.Name})
	spec := &u.Spec
	doc := unitDoc{
		Name:      unitName(u),
		ID:        u.Name,
		Type:      unitType(spec.Type),
		Purpose:   spec.Purpose,
		Goals:     spec.Goals,
		Channel:   spec.Channel,
		Knowledge: spec.Knowledge,
		MCPEnv:    r.mcpEnv(KindUnit, u.Name, spec.MCP),
		Project:   spec.Project,
		Space:     spec.Space,
	}
	if lead := spec.LeadRef.Name; lead != "" {
		if s := r.seats[lead]; s != nil {
			// Crewlet names a unit's lead by the seat's name, not its
			// handle.
			doc.Lead = seatName(s)
		} else {
			r.errs.notFound(KindUnit, u.Name, "spec.leadRef.name", fmt.Sprintf(
				"names Seat %s as the lead, which does not exist in namespace %s", lead, r.namespace()))
		}
	}
	doc.Roles = r.roles(append(slices.Clone(at), "roles"), t.members[u.Name])
	doc.Children = r.unitList(append(slices.Clone(at), "children"), t.children[u.Name], t)
	return doc
}

func (r *renderer) roles(at Path, seats []*v1alpha1.Seat) []roleDoc {
	slices.SortFunc(seats, func(a, b *v1alpha1.Seat) int {
		return cmp.Or(strings.Compare(seatHandle(a), seatHandle(b)), strings.Compare(a.Name, b.Name))
	})
	out := make([]roleDoc, 0, len(seats))
	for i, s := range seats {
		r.source(append(slices.Clone(at), i), ObjectRef{Kind: KindSeat, Name: s.Name})
		out = append(out, r.role(s))
	}
	return out
}

func (r *renderer) role(s *v1alpha1.Seat) roleDoc {
	spec := &s.Spec
	doc := roleDoc{
		Name:                 seatName(s),
		Handle:               seatHandle(s),
		Kind:                 seatKind(spec.Kind),
		Email:                spec.Email,
		Availability:         spec.Availability,
		Goal:                 spec.Goal,
		Backstory:            spec.Backstory,
		Responsibilities:     spec.Responsibilities,
		BehavioralGuidelines: spec.BehavioralGuidelines,
		Manages:              r.manages(s),
		Workers:              spec.Workers,
		TokenBudget:          tokenBudget(spec.TokenBudget),
		LearningEnabled:      spec.LearningEnabled,
		MCPEnv:               r.mcpEnv(KindSeat, s.Name, spec.MCP),
		Project:              spec.Project,
		Space:                spec.Space,
	}
	if c := spec.Contact; c != (v1alpha1.SeatContact{}) {
		doc.Contact = &contactDoc{SlackUserID: c.SlackUserID, GitHubLogin: c.GitHubLogin}
	}
	if doc.Kind == "human" {
		// The engine never runs a person, so a Human seat has no
		// placement; the API refuses every agent-only field on it.
		return doc
	}

	pool := DataPool
	if spec.Placement != nil && spec.Placement.Pool != "" {
		pool = spec.Placement.Pool
	}
	// Every agent seat is pinned, even while every seat runs on the data
	// pods: an unpinned seat may land on any pod that runs seats, and the
	// pin is what keeps it where its pool says once there is more than
	// one kind of pod.
	doc.Placement = &placementDoc{Labels: map[string]string{PoolLabel: pool}}

	doc.LLM = phaseLLMDoc{
		Default:   r.chain(s, "default", spec.LLM.Default),
		Review:    r.chain(s, "review", spec.LLM.Review),
		Subagent:  r.chain(s, "subagent", spec.LLM.Subagent),
		Auxiliary: r.chain(s, "auxiliary", spec.LLM.Auxiliary),
		Judge:     r.chain(s, "judge", spec.LLM.Judge),
		Sandbox:   r.chain(s, "sandbox", spec.LLM.Sandbox),
	}
	if sb := spec.Sandbox; sb != nil {
		doc.Sandbox = &roleSandboxDoc{
			Enabled:         true,
			CodingAgent:     codingAgent(sb.CodingAgent),
			PauseTTLSeconds: sb.PauseTTLSeconds,
			MaxTurns:        sb.MaxTurns,
			Env:             r.secretEnv(sb.Env),
			Setup:           r.setup(sb.Setup),
		}
		for i, ref := range sb.MCPServerRefs {
			if r.mcp[ref.Name] == nil {
				r.errs.notFound(KindSeat, s.Name, fmt.Sprintf("spec.sandbox.mcpServerRefs[%d].name", i), fmt.Sprintf(
					"names MCPServer %s, which does not exist in namespace %s", ref.Name, r.namespace()))
				continue
			}
			if doc.Sandbox.MCP == nil {
				doc.Sandbox.MCP = &roleSandboxMCP{}
			}
			doc.Sandbox.MCP.Servers = append(doc.Sandbox.MCP.Servers, ref.Name)
		}
	}
	doc.Integrations = r.roleIntegrations(&spec.Integrations)
	return doc
}

func (r *renderer) roleIntegrations(in *v1alpha1.SeatIntegrations) roleIntegrationsDoc {
	var doc roleIntegrationsDoc
	if creds := in.Slack.CredentialsSecretRef.Name; creds != "" {
		doc.Slack = &roleSlackDoc{
			BotToken:      r.secret(creds, v1alpha1.SlackBotTokenKey),
			SigningSecret: r.secret(creds, v1alpha1.SlackSigningSecretKey),
		}
	}
	if g := in.GitHub; g.Tier != "" || len(g.Repos) > 0 || g.App.ID != 0 {
		doc.GitHub = &roleGitHubDoc{
			Tier:  gitHubAccessTier(g.Tier),
			Repos: g.Repos,
		}
		if app := g.App; app.ID != 0 {
			doc.GitHub.AppID = app.ID
			doc.GitHub.AppSlug = app.Slug
			doc.GitHub.InstallationID = app.InstallationID
			doc.GitHub.PrivateKey = r.secret(app.CredentialsSecretRef.Name, v1alpha1.GitHubAppPrivateKeyKey)
			doc.GitHub.WebhookSecret = r.secret(app.CredentialsSecretRef.Name, v1alpha1.GitHubAppWebhookSecretKey)
		}
	}
	return doc
}

// chain resolves one phase of a Seat's model providers to providers.llm
// keys.
func (r *renderer) chain(s *v1alpha1.Seat, phase string, refs []v1alpha1.LLMProviderReference) []string {
	var out []string
	for i, ref := range refs {
		if key, ok := r.provider(KindSeat, s.Name, fmt.Sprintf("spec.llm.%s[%d]", phase, i), ref); ok {
			out = append(out, key)
		}
	}
	return out
}

// manages renders a Seat's manages entries. Crewlet names both seats and
// units there by their names, and reads a name that is both a seat's and a
// unit's as the seat; a reference to such a Unit is refused rather than
// rendered as a reference to the Seat.
func (r *renderer) manages(s *v1alpha1.Seat) []string {
	var out []string
	for i, ref := range s.Spec.Manages {
		field := fmt.Sprintf("spec.manages[%d].name", i)
		if ref.Kind == v1alpha1.ManagedKindUnit {
			u := r.units[ref.Name]
			if u == nil {
				r.errs.notFound(KindSeat, s.Name, field, fmt.Sprintf(
					"names Unit %s, which does not exist in namespace %s", ref.Name, r.namespace()))
				continue
			}
			if other := r.seatNamed(unitName(u)); other != nil {
				r.errs.add(KindSeat, s.Name, field, v1alpha1.ReasonConflict, fmt.Sprintf(
					"Unit %s is named %q, as Seat %s is, and Crewlet would read this as managing the seat: give the Unit or the Seat another displayName",
					u.Name, unitName(u), other.Name))
				continue
			}
			out = append(out, unitName(u))
			continue
		}
		// An unset kind is the API's default, Seat.
		managed := r.seats[ref.Name]
		if managed == nil {
			r.errs.notFound(KindSeat, s.Name, field, fmt.Sprintf(
				"names Seat %s, which does not exist in namespace %s", ref.Name, r.namespace()))
			continue
		}
		out = append(out, seatName(managed))
	}
	return out
}

// seatNamed returns a Seat whose rendered name is name, the one that sorts
// first by metadata.name so the answer does not depend on map order.
func (r *renderer) seatNamed(name string) *v1alpha1.Seat {
	var found *v1alpha1.Seat
	for _, s := range r.seats {
		if seatName(s) == name && (found == nil || s.Name < found.Name) {
			found = s
		}
	}
	return found
}

// seatHandle is a Seat's handle: spec.handle, or its metadata.name.
func seatHandle(s *v1alpha1.Seat) string {
	return cmp.Or(s.Spec.Handle, s.Name)
}

// seatName is a Seat's Crewlet name: spec.displayName, or its handle.
func seatName(s *v1alpha1.Seat) string {
	return cmp.Or(s.Spec.DisplayName, seatHandle(s))
}

// unitName is a Unit's Crewlet name: spec.displayName, or its metadata.name,
// which is also its Crewlet id.
func unitName(u *v1alpha1.Unit) string {
	return cmp.Or(u.Spec.DisplayName, u.Name)
}
