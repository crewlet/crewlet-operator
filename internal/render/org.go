package render

import (
	"cmp"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// The org chart: Units become Crewlet's units, nested under their parents
// as children, and Seats become roles, inside their Unit's entry or at the
// top level when they have none. Siblings are ordered by metadata.name and
// seats by handle, so the document never depends on the order objects were
// listed in.

// keyChildren is the key a unit lists its child units under.
const keyChildren = "children"

// keySchedules is the key a role or a unit lists its schedules under.
const keySchedules = "schedules"

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
			r.errs.notFound(HarmsBeyond, KindUnit, u.Name, "spec.parentRef.name", fmt.Sprintf(
				"names Unit %s as the parent, which does not exist in namespace %s", parent, r.namespace()))
		case r.inCycle(u):
			r.errs.add(HarmsBeyond, KindUnit, u.Name, "spec.parentRef.name", v1alpha1.ReasonCycle, fmt.Sprintf(
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
			r.errs.notFound(HarmsBeyond, KindSeat, s.Name, "spec.unitRef.name", fmt.Sprintf(
				"names Unit %s, which does not exist in namespace %s", unit, r.namespace()))
		default:
			members[unit] = append(members[unit], s)
		}
	}

	r.duplicateSeats()
	r.headerSpellings()
	r.atlassianAccounts()
	r.scheduleTargets()
	t := tree{children: children, members: members}
	return r.roles(Path{keyRoles}, rootSeats), r.unitList(Path{keyUnits}, roots, t)
}

// duplicateSeats refuses Seats that would share an identity in the engine,
// on every Seat involved, as Crewlet's own validation would refuse the
// document: a handle, an email address, or a contact account two Seats
// claim. Each is compared as Crewlet compares it. A display name is not an
// identity: Crewlet holds none unique, and names a seat by its handle
// wherever one seat refers to another, so two Seats may share one.
func (r *renderer) duplicateSeats() {
	seats := slices.SortedFunc(maps.Values(r.seats), byName)
	for _, g := range groupBy(seats, SeatHandle) {
		conflict(&r.errs, KindSeat, g, "spec.handle", func(others string) string {
			return fmt.Sprintf("shares the handle %q with %s. The handle is the seat's identity in the engine "+
				"(its inbox, its agent id, its accounts), so give each Seat its own spec.handle", g.key, others)
		})
	}
	// Crewlet's party registry keys a seat by its address in the form
	// iam.NormalizeEmail gives it, and the first seat to claim one wins, so
	// mail for one person would reach another; Crewlet refuses two seats
	// declaring one address in that form (validateSeatEmails).
	for _, g := range groupBy(seats, func(s *v1alpha1.Seat) string { return normalizeEmail(s.Spec.Email) }) {
		conflict(&r.errs, KindSeat, g, "spec.email", func(others string) string {
			return fmt.Sprintf("declares the email %q, as %s does (compared lower-cased, with any plus tag dropped). "+
				"The engine finds a seat by its address and only the first of these would be found, so give each Seat "+
				"its own spec.email", g.key, others)
		})
	}
	for _, a := range contactAccounts {
		for _, g := range groupBy(seats, func(s *v1alpha1.Seat) string { return a.claim(&s.Spec.Contact) }) {
			conflict(&r.errs, KindSeat, g, "spec.contact."+a.field, func(others string) string {
				return fmt.Sprintf("claims the account %q, as %s does. An inbound message reaches only one of the seats "+
					"that claim an account, so give each Seat its own", g.key, others)
			})
		}
	}
}

// group is the objects that share one key.
type group[T any] struct {
	key     string
	members []T
}

// groupBy groups items by key, in the order the keys are first met, and
// returns the groups of more than one; an empty key is no claim.
func groupBy[T any](items []T, key func(T) string) []group[T] {
	index := map[string]int{}
	var groups []group[T]
	for _, item := range items {
		k := key(item)
		if k == "" {
			continue
		}
		i, seen := index[k]
		if !seen {
			i = len(groups)
			index[k] = i
			groups = append(groups, group[T]{key: k})
		}
		groups[i].members = append(groups[i].members, item)
	}
	return slices.DeleteFunc(groups, func(g group[T]) bool { return len(g.members) < 2 })
}

// conflict refuses every member of g, each with a message naming the others.
func conflict[T interface{ GetName() string }](
	errs *refErrors, kind string, g group[T], field string, message func(others string) string,
) {
	for _, member := range g.members {
		var others []string
		for _, o := range g.members {
			if o.GetName() != member.GetName() {
				others = append(others, o.GetName())
			}
		}
		names := kind + " " + others[0]
		if len(others) > 1 {
			names = kind + "s " + strings.Join(others, ", ")
		}
		errs.add(EngineRefuses, kind, member.GetName(), field, v1alpha1.ReasonConflict, message(names))
	}
}

// byName orders objects by metadata.name.
func byName[T interface{ GetName() string }](a, b T) int {
	return strings.Compare(a.GetName(), b.GetName())
}

// normalizeEmail is an address in the form Crewlet compares it in
// (iam.NormalizeEmail): trimmed, lower-cased, and with the local part cut at
// its first '+', so ada+ops@example.com and Ada@example.com are one address.
// An empty address is no claim.
func normalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return email
	}
	local, _, _ = strings.Cut(local, "+")
	return local + "@" + domain
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
			// Crewlet names a unit's lead by the seat's handle, and
			// refuses anything not shaped like one.
			doc.Lead = SeatHandle(s)
		} else {
			r.errs.notFound(HarmsBeyond, KindUnit, u.Name, "spec.leadRef.name", fmt.Sprintf(
				"names Seat %s as the lead, which does not exist in namespace %s", lead, r.namespace()))
		}
	}
	doc.Schedules = r.schedules(at, r.unitSchedules[u.Name])
	doc.Roles = r.roles(append(slices.Clone(at), keyRoles), t.members[u.Name])
	doc.Children = r.unitList(append(slices.Clone(at), keyChildren), t.children[u.Name], t)
	return doc
}

func (r *renderer) roles(at Path, seats []*v1alpha1.Seat) []roleDoc {
	slices.SortFunc(seats, func(a, b *v1alpha1.Seat) int {
		return cmp.Or(strings.Compare(SeatHandle(a), SeatHandle(b)), strings.Compare(a.Name, b.Name))
	})
	out := make([]roleDoc, 0, len(seats))
	for i, s := range seats {
		seatAt := append(slices.Clone(at), i)
		r.source(seatAt, ObjectRef{Kind: KindSeat, Name: s.Name})
		out = append(out, r.role(seatAt, s))
	}
	return out
}

func (r *renderer) role(at Path, s *v1alpha1.Seat) roleDoc {
	spec := &s.Spec
	doc := roleDoc{
		Name:                 seatName(s),
		Handle:               SeatHandle(s),
		Kind:                 seatKind(spec.Kind),
		Email:                spec.Email,
		Contact:              contact(&spec.Contact),
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
	if doc.Kind == "human" {
		// The engine never runs a person, so a Human seat has no
		// placement; the API refuses every agent-only field on it.
		return doc
	}

	// Every agent seat is pinned, those on the data pods included: an
	// unpinned seat may land on any pod that runs seats, a seat pool's as
	// well as a data pod.
	pool := SeatPool(r.in.Company, s)
	doc.Placement = &placementDoc{Labels: r.topology.Placement(pool, doc.Handle)}
	if held, ok := r.in.HeldPlacements[doc.Handle]; ok {
		doc.Placement = &placementDoc{Labels: held}
	}
	if r.topology.Places(pool, doc.Handle) {
		r.seatPodProviders(s, pool)
	}

	doc.LLM = phaseLLMDoc{
		Default:   r.chain(s, "default", spec.LLM.Default),
		Review:    r.chain(s, "review", spec.LLM.Review),
		Subagent:  r.chain(s, "subagent", spec.LLM.Subagent),
		Auxiliary: r.chain(s, "auxiliary", spec.LLM.Auxiliary),
		Judge:     r.chain(s, "judge", spec.LLM.Judge),
	}
	r.noExecutor(s)
	if sb := &spec.Sandbox; seatSandboxSet(sb) {
		doc.Sandbox = &roleSandboxDoc{
			PauseTTLSeconds: sb.PauseTTLSeconds,
			MaxTurns:        sb.MaxTurns,
			Env:             r.seatEnv(s, sb),
			OptionalKeys:    sortedSet(sb.OptionalKeys),
			Setup:           r.setup(ObjectRef{Kind: KindSeat, Name: s.Name}, sb.Setup),
		}
		for i, ref := range sb.MCPServerRefs {
			if r.mcp[ref.Name] == nil {
				r.errs.notFound(HarmsBeyond, KindSeat, s.Name, fmt.Sprintf("spec.sandbox.mcpServerRefs[%d].name", i), fmt.Sprintf(
					"names MCPServer %s, which does not exist in namespace %s", ref.Name, r.namespace()))
				continue
			}
			if doc.Sandbox.MCP == nil {
				doc.Sandbox.MCP = &roleSandboxMCP{}
			}
			doc.Sandbox.MCP.Servers = append(doc.Sandbox.MCP.Servers, ref.Name)
		}
	}
	doc.Integrations = r.roleIntegrations(s)
	doc.Schedules = r.schedules(at, r.seatSchedules[s.Name])
	return doc
}

// scheduleTargets sorts the Schedules by the Seat or Unit they target, and
// refuses those whose target cannot carry them: one that does not exist, and
// a Human seat, which Crewlet refuses any schedule on because it runs no
// turn for a person. That refusal is the Schedule's to report: the engine
// would report it at the seat's schedules, which is the Seat's place in the
// document.
func (r *renderer) scheduleTargets() {
	r.seatSchedules = map[string][]*v1alpha1.Schedule{}
	r.unitSchedules = map[string][]*v1alpha1.Schedule{}
	for _, sched := range sortedByName(r.in.Schedules) {
		target := &sched.Spec.Target
		if name := target.SeatRef.Name; name != "" {
			seat := r.seats[name]
			switch {
			case seat == nil:
				r.errs.notFound(EngineRefuses, KindSchedule, sched.Name, "spec.target.seatRef.name", fmt.Sprintf(
					"names Seat %s, which does not exist in namespace %s", name, r.namespace()))
			case seat.Spec.Kind == v1alpha1.SeatKindHuman:
				r.errs.add(EngineRefuses, KindSchedule, sched.Name, "spec.target.seatRef.name", v1alpha1.ReasonHumanSeat, fmt.Sprintf(
					"names Seat %s, a Human seat, and the engine runs no turn for a person: target an Agent seat, "+
						"or a unit with fanOut Each", name))
			default:
				r.seatSchedules[name] = append(r.seatSchedules[name], sched)
			}
			continue
		}
		name := target.UnitRef.Name
		if r.units[name] == nil {
			r.errs.notFound(EngineRefuses, KindSchedule, sched.Name, "spec.target.unitRef.name", fmt.Sprintf(
				"names Unit %s, which does not exist in namespace %s", name, r.namespace()))
			continue
		}
		r.unitSchedules[name] = append(r.unitSchedules[name], sched)
	}
}

// schedules renders the schedules of the role or unit at owner, in the
// order scheduleTargets sorted them, by metadata.name.
func (r *renderer) schedules(owner Path, schedules []*v1alpha1.Schedule) []scheduleDoc {
	if len(schedules) == 0 {
		return nil
	}
	out := make([]scheduleDoc, len(schedules))
	for i, sched := range schedules {
		r.source(append(slices.Clone(owner), keySchedules, i), ObjectRef{Kind: KindSchedule, Name: sched.Name})
		spec := &sched.Spec
		out[i] = scheduleDoc{
			// The name is half the identity the engine claims each fire
			// under, and a metadata.name never changes, so a fire is never
			// claimed twice because a Schedule was edited.
			Name:           sched.Name,
			Cron:           spec.Schedule,
			Task:           spec.Prompt,
			Timezone:       spec.TimeZone,
			Target:         scheduleFanOut(spec.Target.FanOut),
			TimeoutSeconds: spec.TimeoutSeconds,
			Catchup:        spec.CatchUp,
		}
		if spec.Suspend != nil && *spec.Suspend {
			out[i].Enabled = new(false)
		}
	}
	return out
}

// seatEnv renders the environment of a seat's executor runs, sb's env, each
// variable from a Secret or provisioned.
func (r *renderer) seatEnv(s *v1alpha1.Seat, sb *v1alpha1.SeatSandbox) map[string]string {
	if len(sb.Env) == 0 {
		return nil
	}
	out := make(map[string]string, len(sb.Env))
	for i, e := range sb.Env {
		field := fmt.Sprintf("spec.sandbox.env[%d].valueFrom", i)
		out[e.Name] = r.seatCredential(s, field, e.ValueFrom, &startUse{
			object: ObjectRef{Kind: KindSeat, Name: s.Name}, field: field, name: e.Name,
			optional: slices.Contains(sb.OptionalKeys, e.Name),
			list:     ObjectRef{Kind: KindSeat, Name: s.Name}, listField: "spec.sandbox.optionalKeys",
		})
	}
	return out
}

// roleIntegrations renders a Seat's blocks of Crewlet's role integrations.
// A block is written whenever the Seat sets it, off ones included, since
// enabled is the stored setting the engine acts on: an absent block is a seat
// the vendor has never been turned on for, and an off one keeps its account at
// the vendor and its access for when it is turned back on.
func (r *renderer) roleIntegrations(s *v1alpha1.Seat) roleIntegrationsDoc {
	in := &s.Spec.Integrations
	var doc roleIntegrationsDoc
	if sl := &in.Slack; sl.Enabled != nil {
		doc.Slack = &roleSlackDoc{
			Enabled:       *sl.Enabled,
			BotToken:      r.secret(sl.CredentialsSecretRef.Name, v1alpha1.SlackBotTokenKey),
			SigningSecret: r.secret(sl.CredentialsSecretRef.Name, v1alpha1.SlackSigningSecretKey),
		}
	}
	if g := &in.GitHub; g.Enabled != nil {
		doc.GitHub = &roleGitHubDoc{
			Enabled:     *g.Enabled,
			Permissions: gitHubPermissions(&g.Permissions),
			Repos:       g.Repos,
		}
		if app := g.App; app.ID != 0 {
			doc.GitHub.AppID = app.ID
			doc.GitHub.AppSlug = app.Slug
			doc.GitHub.InstallationID = app.InstallationID
			doc.GitHub.PrivateKey = r.secret(app.CredentialsSecretRef.Name, v1alpha1.GitHubAppPrivateKeyKey)
			doc.GitHub.WebhookSecret = r.secret(app.CredentialsSecretRef.Name, v1alpha1.GitHubAppWebhookSecretKey)
		}
	}
	if g := &in.GitLab; g.Enabled != nil {
		doc.GitLab = &roleGitLabDoc{
			Enabled:      *g.Enabled,
			AccessLevel:  gitLabAccessLevel(g.AccessLevel),
			MemberRoleID: g.MemberRoleID,
			Projects:     g.Projects,
		}
	}
	if m := &in.Mattermost; m.Enabled != nil {
		doc.Mattermost = &roleMattermostDoc{
			Enabled:  *m.Enabled,
			Roles:    mattermostRoles(m.Roles),
			Channels: m.Channels,
			BotToken: r.secretRef(m.BotToken.SecretKeyRef),
			Username: m.Username,
		}
	}
	if j := &in.Jira; j.Enabled != nil {
		doc.Jira = &roleJiraDoc{Enabled: *j.Enabled, ProjectRole: j.ProjectRole, Projects: j.Projects}
	}
	if c := &in.Confluence; c.Enabled != nil {
		doc.Confluence = &roleConfluenceDoc{
			Enabled: *c.Enabled, SpaceRole: c.SpaceRole, Operations: c.Operations, Spaces: c.Spaces,
		}
	}
	if d := &in.Datadog; d.Enabled != nil {
		doc.Datadog = &roleDatadogDoc{Enabled: *d.Enabled, Role: d.Role}
	}
	return doc
}

// gitHubPermissions renders a seat's GitHub App permissions as the map of
// GitHub's permission keys Crewlet stores, leaving out every permission the
// seat holds none of, which is GitHub's No access.
func gitHubPermissions(p *v1alpha1.GitHubPermissions) map[string]string {
	out := map[string]string{}
	for key, level := range map[string]v1alpha1.GitHubPermission{
		"contents":      p.Contents,
		"issues":        p.Issues,
		"pull_requests": p.PullRequests,
		"checks":        p.Checks,
		"actions":       p.Actions,
		"workflows":     p.Workflows,
	} {
		if level != "" {
			out[key] = gitHubPermission(level)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mattermostRoles renders a bot's roles under Mattermost's identifiers, in
// the order Mattermost writes the set (System Admin, then Member, then a post
// permission), which is the order Crewlet compares a set in, so the same roles
// always render the same list whatever order the Seat lists them in.
func mattermostRoles(roles []v1alpha1.MattermostRole) []string {
	if len(roles) == 0 {
		return nil
	}
	order := []v1alpha1.MattermostRole{
		v1alpha1.MattermostSystemAdmin, v1alpha1.MattermostSystemUser,
		v1alpha1.MattermostSystemPostAllPublic, v1alpha1.MattermostSystemPostAll,
	}
	out := make([]string, 0, len(roles))
	for _, role := range order {
		if slices.Contains(roles, role) {
			out = append(out, mattermostRole(role))
		}
	}
	return out
}

// chain resolves one phase of a Seat's model providers to providers.llm
// keys.
func (r *renderer) chain(s *v1alpha1.Seat, phase string, refs []v1alpha1.LLMProviderReference) []string {
	var out []string
	for i, ref := range refs {
		// Crewlet refuses a seat's chain naming no provider.
		if key, ok := r.provider(KindSeat, s.Name, fmt.Sprintf("spec.llm.%s[%d]", phase, i), ref, absentRefused); ok {
			out = append(out, key)
		}
	}
	return out
}

// manages renders a Seat's manages entries. Crewlet names a seat there by its
// handle and a unit by its key, which is the Unit's metadata.name, and reads
// a token that is both a seat's handle and a unit's key as the seat
// (org.managesIndex); a reference to such a Unit is refused rather than
// rendered as a reference to the Seat.
func (r *renderer) manages(s *v1alpha1.Seat) []string {
	var out []string
	for i, ref := range s.Spec.Manages {
		field := fmt.Sprintf("spec.manages[%d].name", i)
		if ref.Kind == v1alpha1.ManagedKindUnit {
			u := r.units[ref.Name]
			if u == nil {
				r.errs.notFound(HarmsBeyond, KindSeat, s.Name, field, fmt.Sprintf(
					"names Unit %s, which does not exist in namespace %s", ref.Name, r.namespace()))
				continue
			}
			if other := r.seatWithHandle(u.Name); other != nil {
				r.errs.add(HarmsBeyond, KindSeat, s.Name, field, v1alpha1.ReasonConflict, fmt.Sprintf(
					"Unit %s's key %q is also Seat %s's handle, which Crewlet reads as the seat: this would manage the "+
						"seat rather than the unit. Give the Seat another spec.handle, or the Unit another name",
					u.Name, u.Name, other.Name))
				continue
			}
			out = append(out, u.Name)
			continue
		}
		// An unset kind is the API's default, Seat.
		managed := r.seats[ref.Name]
		if managed == nil {
			r.errs.notFound(HarmsBeyond, KindSeat, s.Name, field, r.noSeat(ref.Name))
			continue
		}
		out = append(out, SeatHandle(managed))
	}
	return out
}

// seatWithHandle returns a Seat whose handle is handle, the one that sorts
// first by metadata.name so the answer does not depend on map order.
func (r *renderer) seatWithHandle(handle string) *v1alpha1.Seat {
	var found *v1alpha1.Seat
	for _, s := range r.seats {
		if SeatHandle(s) == handle && (found == nil || s.Name < found.Name) {
			found = s
		}
	}
	return found
}

// SeatHandle is a Seat's handle: spec.handle, or its metadata.name. With the
// Company's spec.companyName it is what SeatID derives the seat's identity
// from.
func SeatHandle(s *v1alpha1.Seat) string {
	return cmp.Or(s.Spec.Handle, s.Name)
}

// seatName is a Seat's Crewlet name: spec.displayName, or its handle.
func seatName(s *v1alpha1.Seat) string {
	return cmp.Or(s.Spec.DisplayName, SeatHandle(s))
}

// unitName is a Unit's Crewlet name: spec.displayName, or its metadata.name,
// which is also its Crewlet id.
func unitName(u *v1alpha1.Unit) string {
	return cmp.Or(u.Spec.DisplayName, u.Name)
}

// seatSandboxSet reports whether a Seat sets anything of its spec.sandbox,
// which the role's sandbox is written for. The API refuses one that sets
// nothing ({}), as every agent seat's executor runs in a box whether or not
// the seat says anything of it; unset, the role has no sandbox and its box
// runs on the company's settings alone.
func seatSandboxSet(sb *v1alpha1.SeatSandbox) bool {
	return !reflect.ValueOf(*sb).IsZero()
}
