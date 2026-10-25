package render

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// Input is everything a company document is rendered from: a Company, its
// EngineClass, and the objects whose spec.companyRef names the Company. The
// slices may be in any order; the document does not depend on it.
type Input struct {
	Company      *v1alpha1.Company
	Class        *v1alpha1.EngineClass
	Units        []v1alpha1.Unit
	Seats        []v1alpha1.Seat
	LLMProviders []v1alpha1.LLMProvider
	MCPServers   []v1alpha1.MCPServer
	Integrations []v1alpha1.Integration
	Schedules    []v1alpha1.Schedule

	// StoreName is the name of the engine secret-store entry that holds a
	// referenced Secret key's value. The document refers to the value as
	// ${StoreName(key)}. It must be a pure function of the key, so that a
	// render depends on nothing but its input.
	StoreName func(secretref.Key) string

	// ProvisionedName is the name of the engine secret-store entry that
	// holds the credential the engine provisions for the seat with the
	// handle under the provisioned name. The document refers to it as
	// ${ProvisionedName(handle, name)}. Like StoreName it must be a pure
	// function of its arguments.
	ProvisionedName func(handle, name string) string

	// PublicBaseURL is the address vendors deliver webhooks to: the
	// Company's spec.exposure.publicBaseURL, or the one the class's
	// webhooks.hostname derives. Empty when the company is not exposed. It
	// is not part of the document: Crewlet reads where its public listener
	// is reached from the data pod's crewlet.yaml (api.public.external_url,
	// BootstrapInput.PublicURL). It decides what this render reports, the
	// verdicts of ExecutorOf and AgentModeOf on a run that calls the seat's
	// tools back through it, and the webhook addresses a Seat publishes.
	PublicBaseURL string

	// Unset are the Secret keys the reconcile read and found holding no
	// value: a Secret or a key that does not exist, or an empty value. A
	// value read from one under a key its block declares optional is left
	// out of what the engine starts, which CodeWorkOf follows. Before the
	// Secrets are read it is empty, and every key reads as holding a value.
	Unset []secretref.Key
	// Empty are the keys of Unset that exist and hold an empty value, which
	// the engine reads as nothing too: under a key not declared optional,
	// what it is given to does not start, where a missing one holds the
	// document instead, and CodeWorkOf says so of a seat's coding runs.
	Empty []secretref.Key

	// HeldPlacements are the role.placement labels to render for the agent
	// seats, by handle, that keep running where the revision the engine runs
	// pins them while the pods of the pool they move to start: the pin
	// moves with the first revision after those pods are up, so that the
	// seat is never pinned to pods that are not there. A seat not named is
	// pinned where it is placed.
	HeldPlacements map[string]map[string]string
}

// Document is a rendered company document.
type Document struct {
	// YAML is the document, with keys sorted and lists in a stable order.
	YAML []byte
	// Hash is the hex-encoded SHA-256 of YAML.
	Hash string
	// Refs is every Secret key the document refers to, sorted with
	// secretref.Compare and without duplicates.
	Refs []secretref.Key
	// Optional are the keys of Refs every reference to which is a
	// StartValue under a key its block declares optional, sorted: the engine
	// leaves such a key out when its value resolves to nothing, so a Secret
	// or key that does not exist holds nothing back, and the secret sync
	// keeps its entry out of the engine's store.
	Optional []secretref.Key
	// StartValues are the references to Secret keys the document gives a
	// tool server or a coding run under a key, in the order rendered.
	StartValues []StartValue
	// Provisioned is every store entry the document refers to that the
	// engine's provisioning fills rather than the operator, sorted and
	// without duplicates: the names of the seats' provisioned credentials.
	Provisioned []string
	// Sources map each part of the document to the object it was rendered
	// from; see Attribute.
	Sources []Source
}

// Company renders the company document of in.Company.
//
// It fails with *RefError values, joined (see RefErrors): one for each
// finding about a reference between objects that holds the document (its
// Basis), and one for each ${NAME} reference an object holds itself
// (checkReferences). Nothing is rendered then, because a document is applied
// whole or not at all. A finding that holds nothing is rendered past, and
// ReportedOf lists it. The only other failure is an engineConfig value that is
// not JSON, which the API server never stores.
func Company(in *Input) (*Document, error) {
	r, merged, err := renderJSON(in, in.StoreName, in.ProvisionedName)
	if err != nil {
		return nil, err
	}
	out, err := yamlOf(merged)
	if err != nil {
		return nil, fmt.Errorf("converting the company document to YAML: %w", err)
	}

	sum := sha256.Sum256(out)
	slices.SortFunc(r.sources, func(a, b Source) int { return strings.Compare(a.Path.String(), b.Path.String()) })
	rendered := &Document{
		YAML:        out,
		Hash:        hex.EncodeToString(sum[:]),
		Refs:        sortedKeys(r.refs),
		Optional:    r.optional(),
		StartValues: r.markStarted(),
		Provisioned: slices.Sorted(maps.Keys(r.provisioned)),
		Sources:     r.sources,
	}

	// The same render with every store name changed tells the references
	// the renderer wrote from strings an object holds: only the former
	// differ between the two.
	_, probe, err := renderJSON(in,
		func(k secretref.Key) string { return "PROBE_" + in.StoreName(k) },
		func(handle, name string) string { return "PROBE_" + in.ProvisionedName(handle, name) })
	if err != nil {
		return nil, err
	}
	if err := checkReferences(rendered, r.owner, merged, probe); err != nil {
		return nil, err
	}
	return rendered, nil
}

// ReportedOf is every finding about the objects of in that holds nothing
// (Basis Reported), sorted by object and field: each such object is rendered
// as its spec says, and the finding is the controller's to report on it. Like
// CodeWorkOf it is a statement about the objects' specs, which holds while
// another object's refusal keeps Company from returning a document.
func ReportedOf(in *Input) []*RefError {
	r := newRenderer(in)
	r.company()
	return r.errs.reported()
}

// renderJSON renders the document as JSON, its map keys sorted, with
// storeName naming each referenced Secret key's entry in the engine's store
// and provisionedName each provisioned credential's.
func renderJSON(
	in *Input, storeName func(secretref.Key) string, provisionedName func(handle, name string) string,
) (*renderer, []byte, error) {
	in = new(*in)
	in.StoreName = storeName
	in.ProvisionedName = provisionedName
	r := newRenderer(in)
	doc := r.company()
	if err := r.errs.err(); err != nil {
		return nil, nil, err
	}

	// The engineConfig entries are top-level keys of the document. The API
	// refuses every key a typed field renders, so merging them into the
	// encoded object cannot replace anything rendered here.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding the company document: %w", err)
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &top); err != nil {
		return nil, nil, fmt.Errorf("decoding the company document: %w", err)
	}
	for _, entry := range in.Company.Spec.EngineConfig {
		top[entry.Name] = entry.Value.Raw
	}
	// Every top-level key but the five the children's blocks live in is
	// the Company's own: its typed fields and its engineConfig entries.
	for key := range top {
		if !slices.Contains(childBlocks, key) {
			r.source(Path{key}, r.owner)
		}
	}
	// encoding/json writes map keys sorted, and yamlOf keeps them so.
	merged, err := json.Marshal(top)
	if err != nil {
		return nil, nil, fmt.Errorf("encoding the company document: %w", err)
	}
	return r, merged, nil
}

// reference is Crewlet's grammar of a ${NAME} reference (its envref
// package): the only form it substitutes. A shell parameter expansion such
// as ${1:-x} is not one.
var reference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// checkReferences refuses every ${NAME} in the document but the ones the
// renderer wrote for a referenced Secret key or a provisioned credential: the
// strings that differ from the same place in probe, a render with every store
// name changed.
//
// Crewlet resolves a ${NAME} in the company document from its secret store
// and then from its process environment, and not only where a credential
// goes: an MCP server's url, an embeddings provider's base_url, a model's
// name, a skill variable. A reference anywhere else would let whoever writes
// the object name any value the engine can resolve (the class's platform
// model keys in its store, its environment) and have the engine send it where
// they chose. Crewlet passes
// other strings on as written, such as a sandbox setup command, which the
// sandbox's shell expands from the run's own environment; a reference is
// refused there too, so that a field Crewlet starts expanding names nothing
// the day it does. The API refuses one in many typed fields; this check
// covers every string, engineConfig's included. A string that merely equals
// a reference the renderer wrote elsewhere is refused too: it names that
// value where no credential goes.
func checkReferences(doc *Document, company ObjectRef, encoded, probe []byte) error {
	var tree, probed any
	if err := json.Unmarshal(encoded, &tree); err != nil {
		return fmt.Errorf("decoding the company document: %w", err)
	}
	if err := json.Unmarshal(probe, &probed); err != nil {
		return fmt.Errorf("decoding the company document: %w", err)
	}
	var errs refErrors
	refuse := func(at Path, s string) {
		owner, ok := doc.Attribute(at)
		if !ok {
			owner = company
		}
		errs.add(HarmsBeyond, owner.Kind, owner.Name, "document "+at.String(), v1alpha1.ReasonReferenceNotAllowed, fmt.Sprintf(
			"holds the reference %s, and the operator refuses every one it did not write, since in many fields the engine "+
				"resolves one from its own secret store and environment: name a credential only with a Secret reference, "+
				"which the operator turns into a reference of its own",
			reference.FindString(s)))
	}
	var walk func(at Path, v, p any)
	walk = func(at Path, v, p any) {
		switch v := v.(type) {
		case string:
			if written, _ := p.(string); written != v {
				return
			}
			if reference.MatchString(v) {
				refuse(at, v)
			}
		case []any:
			items, _ := p.([]any)
			for i, item := range v {
				var other any
				if i < len(items) {
					other = items[i]
				}
				walk(append(slices.Clone(at), i), item, other)
			}
		case map[string]any:
			fields, _ := p.(map[string]any)
			for _, key := range slices.Sorted(maps.Keys(v)) {
				if reference.MatchString(key) {
					refuse(append(slices.Clone(at), key), key)
				}
				walk(append(slices.Clone(at), key), v[key], fields[key])
			}
		}
	}
	walk(Path{}, tree, probed)
	return errs.err()
}

// The top-level keys of the company document that hold the blocks rendered
// from the Company's children. Every other top-level key is the Company's.
const (
	keyProviders    = "providers"
	keyIntegrations = "integrations"
	keyMCPServers   = "mcp_servers"
	keyRoles        = "roles"
	keyUnits        = "units"
)

var childBlocks = []string{keyProviders, keyIntegrations, keyMCPServers, keyRoles, keyUnits}

// renderer holds the state of one render: the objects indexed by name, the
// Secret keys referenced so far, the sources recorded and the refusals.
type renderer struct {
	in    *Input
	owner ObjectRef

	units     map[string]*v1alpha1.Unit
	seats     map[string]*v1alpha1.Seat
	mcp       map[string]*v1alpha1.MCPServer
	providers map[string]bool
	// own are the Company's LLMProviders' specs, and platform the
	// EngineClass's platform providers when the Company uses them, by name.
	own      map[string]*v1alpha1.LLMProviderSpec
	platform map[string]*v1alpha1.PlatformLLMProvider

	// seatSchedules and unitSchedules are the Schedules whose target
	// resolves, by the metadata.name of the Seat or Unit they target.
	seatSchedules map[string][]*v1alpha1.Schedule
	unitSchedules map[string][]*v1alpha1.Schedule

	// topology is where each agent seat runs, which its role.placement
	// pins it to.
	topology *Topology

	// unset and empty are Input.Unset's and Input.Empty's keys.
	unset, empty map[secretref.Key]bool

	refs map[secretref.Key]struct{}
	// required are the keys of refs read somewhere the engine does not
	// leave out when the value is missing: everywhere but a StartValue
	// under an optional key.
	required    map[secretref.Key]struct{}
	starts      []StartValue
	provisioned map[string]struct{}
	sources     []Source
	errs        refErrors
}

func newRenderer(in *Input) *renderer {
	r := &renderer{
		in:          in,
		owner:       ObjectRef{Kind: KindCompany, Name: in.Company.Name},
		units:       map[string]*v1alpha1.Unit{},
		seats:       map[string]*v1alpha1.Seat{},
		mcp:         map[string]*v1alpha1.MCPServer{},
		providers:   map[string]bool{},
		own:         map[string]*v1alpha1.LLMProviderSpec{},
		platform:    map[string]*v1alpha1.PlatformLLMProvider{},
		refs:        map[secretref.Key]struct{}{},
		required:    map[secretref.Key]struct{}{},
		provisioned: map[string]struct{}{},
		unset:       map[secretref.Key]bool{},
		empty:       map[secretref.Key]bool{},
	}
	for _, k := range in.Unset {
		r.unset[k] = true
	}
	for _, k := range in.Empty {
		r.unset[k], r.empty[k] = true, true
	}
	for i := range in.Units {
		r.units[in.Units[i].Name] = &in.Units[i]
	}
	for i := range in.Seats {
		r.seats[in.Seats[i].Name] = &in.Seats[i]
	}
	for i := range in.MCPServers {
		r.mcp[in.MCPServers[i].Name] = &in.MCPServers[i]
	}
	for i := range in.LLMProviders {
		p := &in.LLMProviders[i]
		r.providers[p.Name] = true
		r.own[p.Name] = &p.Spec
	}
	if r.usesPlatformProviders() {
		for i := range in.Class.Spec.PlatformLLMProviders {
			p := &in.Class.Spec.PlatformLLMProviders[i]
			r.platform[p.Name] = p
		}
	}
	r.topology = planTopology(in.Company, in.Class, in.Seats, &r.errs)
	return r
}

func (r *renderer) usesPlatformProviders() bool {
	use := r.in.Company.Spec.LLM.UsePlatformProviders
	return use != nil && *use
}

func (r *renderer) namespace() string { return r.in.Company.Namespace }

// secret records a reference to a Secret key in the Company's namespace and
// returns the pointer the document holds in its place.
func (r *renderer) secret(name, key string) string {
	return r.secretKey(secretref.Key{Namespace: r.namespace(), Name: name, Key: key})
}

func (r *renderer) secretKey(k secretref.Key) string {
	r.refs[k] = struct{}{}
	r.required[k] = struct{}{}
	return "${" + r.in.StoreName(k) + "}"
}

// optional is the keys of refs no reference requires, sorted.
func (r *renderer) optional() []secretref.Key {
	var out []secretref.Key
	for k := range r.refs {
		if _, ok := r.required[k]; !ok {
			out = append(out, k)
		}
	}
	slices.SortFunc(out, secretref.Compare)
	return out
}

// seatCredential returns the pointer the document holds for one of a seat's
// own credentials: a Secret key's store entry, which the secret sync fills,
// or a provisioned credential's, which the engine's provisioning fills. use
// is the tool server or coding run the value is started with, nil for a
// value of any other position. A provisioned credential in a company where no
// Integration provisions is reported at field, as nothing fills it until one
// does; it is rendered all the same, since Crewlet applies the reference and
// only the seat goes without its value (withoutProvisioner).
func (r *renderer) seatCredential(s *v1alpha1.Seat, field string, src v1alpha1.SeatCredentialSource, use *startUse) string {
	if ref := src.SecretKeyRef; ref.Name != "" {
		if use != nil {
			return r.startSecret(use, ref)
		}
		return r.secret(ref.Name, ref.Key)
	}
	if !r.engineProvisions() {
		r.errs.add(Reported, KindSeat, s.Name, field, v1alpha1.ReasonNoProvisioner, fmt.Sprintf(
			"names the provisioned credential %s, and no Integration of Company %s has provisioning Engine, "+
				"so nothing creates it: %s. Give the value from a Secret, or provision the vendor with the engine",
			src.Provisioned.Name, r.in.Company.Name, withoutProvisioner(use)))
	}
	name := r.in.ProvisionedName(SeatHandle(s), src.Provisioned.Name)
	r.provisioned[name] = struct{}{}
	return "${" + name + "}"
}

// withoutProvisioner is what becomes of a seat whose provisioned credential,
// written at use, nothing creates. Crewlet holds a value that does not
// resolve to the rule it holds a missing Secret's to: a tool server is not
// started without one, nor a run_sandbox coding run, unless the value's key
// is optional, when it is left out; an agent-mode executor's run starts
// without it, since refusing it would fail the turn.
func withoutProvisioner(use *startUse) string {
	switch {
	case use == nil:
		return "the seat's chat app runs without it"
	case use.server != "" && use.optional:
		return fmt.Sprintf("the engine starts the seat's instance of MCPServer %s without it, as the server's "+
			"optionalKeys lets it", use.server)
	case use.server != "":
		return fmt.Sprintf("the engine does not start the seat's instance of MCPServer %s, which takes it", use.server)
	case use.optional:
		return "the seat's coding runs start without it, as the optionalKeys beside it let them"
	default:
		return "Crewlet refuses each of the seat's run_sandbox coding runs, whose environment holds it, and an " +
			"agent-mode run of its executor starts without it"
	}
}

// engineProvisions reports whether an Integration of the company has the
// engine create the seats' accounts, and so fill provisioned credentials.
func (r *renderer) engineProvisions() bool {
	for i := range r.in.Integrations {
		if r.in.Integrations[i].Spec.Provisioning == v1alpha1.IntegrationProvisioningEngine {
			return true
		}
	}
	return false
}

func (r *renderer) source(p Path, o ObjectRef) {
	r.sources = append(r.sources, Source{Path: p, Object: o})
}

func (r *renderer) company() companyDoc {
	spec := &r.in.Company.Spec
	doc := companyDoc{
		Name:     spec.CompanyName,
		Mission:  spec.Mission,
		Vision:   spec.Vision,
		Policies: spec.Policies,
		Timezone: spec.TimeZone,
		Tracker: trackerDoc{
			Backend: trackerBackend(spec.Tracker.Backend),
		},
		Knowledge: knowledgeDoc{
			Backend:         knowledgeBackend(spec.Knowledge.Backend),
			Scope:           spec.Knowledge.Scope,
			SkillsContainer: spec.Knowledge.SkillsContainer,
			RootSpace:       spec.Knowledge.RootSpace,
			Vectors:         spec.Knowledge.Vectors,
		},
		TokenBudget: tokenBudget(spec.TokenBudget),
	}
	if days := spec.Tracker.Native.InboxRetentionDays; days != 0 {
		doc.Tracker.Native = &trackerNativeDoc{InboxRetentionDays: days}
	}
	doc.Providers = r.providersDoc()
	doc.Assistant = r.assistant()
	doc.Integrations = r.integrations()
	doc.MCPServers = r.mcpServers()
	doc.Roles, doc.Units = r.org()
	return doc
}

func tokenBudget(b v1alpha1.TokenBudget) tokenBudgetDoc {
	return tokenBudgetDoc{Day: b.Day, Week: b.Week, Month: b.Month}
}

// assistant renders the Company's spec.assistant. Its chain is resolved as a
// Seat's is, and a provider it names that the document does not hold is
// refused as the engine refuses it (EngineRefuses): an assistant chain naming
// no provider is refused on every write, where an llm_order key naming none
// is dropped.
func (r *renderer) assistant() assistantDoc {
	a := &r.in.Company.Spec.Assistant
	doc := assistantDoc{
		Enabled:            a.Enabled,
		MaxSteps:           a.MaxSteps,
		TurnTimeoutSeconds: a.TurnTimeoutSeconds,
		ProposalTTLSeconds: a.ProposalTTLSeconds,
		RetentionDays:      a.RetentionDays,
		TokenBudget:        tokenBudget(a.TokenBudget),
	}
	for i, ref := range a.LLM {
		field := fmt.Sprintf("spec.assistant.llm[%d]", i)
		if key, ok := r.provider(KindCompany, r.in.Company.Name, field, ref, absentRefused); ok {
			doc.LLM = append(doc.LLM, key)
		}
	}
	return doc
}

// ---- providers ----------------------------------------------------------- //

func (r *renderer) providersDoc() providersDoc {
	spec := &r.in.Company.Spec
	doc := providersDoc{LLM: map[string]llmProviderDoc{}}

	own := sortedByName(r.in.LLMProviders)
	for _, p := range own {
		doc.LLM[p.Name] = r.llmProvider(p)
		r.source(Path{keyProviders, "llm", p.Name}, ObjectRef{Kind: KindLLMProvider, Name: p.Name})
	}

	var platform []string
	if r.usesPlatformProviders() {
		class := ObjectRef{Kind: KindEngineClass, Name: r.in.Class.Name}
		for i := range r.in.Class.Spec.PlatformLLMProviders {
			p := &r.in.Class.Spec.PlatformLLMProviders[i]
			// Both kinds of provider are keys of Crewlet's one providers.llm
			// mapping, so a tenant's provider named like a platform one
			// would silently replace it. The tenant's object is the one
			// that can be renamed.
			if r.providers[p.Name] {
				r.errs.add(HarmsBeyond, KindLLMProvider, p.Name, "metadata.name", v1alpha1.ReasonConflict,
					fmt.Sprintf("EngineClass %s offers a platform model provider named %s, and both are keys of the company's one providers.llm mapping: rename this LLMProvider", r.in.Class.Name, p.Name))
				continue
			}
			doc.LLM[p.Name] = r.platformProvider(p)
			r.source(Path{keyProviders, "llm", p.Name}, class)
			platform = append(platform, p.Name)
		}
		slices.Sort(platform)
	}

	// llm_order is written out in full rather than left for Crewlet to
	// complete, so the document alone says which provider a seat without a
	// chain runs on: the first of the Company's spec.llm.order, then its own
	// providers by name, then the platform's by name, and that one alone.
	// The API refuses a provider named default, which Crewlet would put
	// ahead of the order.
	named := map[string]bool{}
	for i, ref := range spec.LLM.Order {
		// Crewlet drops a key of llm_order that names no provider, so
		// every seat with no chain of its own would run on whichever
		// provider it then puts first.
		key, ok := r.provider(KindCompany, r.in.Company.Name, fmt.Sprintf("spec.llm.order[%d]", i), ref, absentDropped)
		if ok {
			doc.LLMOrder = append(doc.LLMOrder, key)
			named[key] = true
		}
	}
	for _, p := range own {
		if !named[p.Name] {
			doc.LLMOrder = append(doc.LLMOrder, p.Name)
		}
	}
	for _, name := range platform {
		if !named[name] {
			doc.LLMOrder = append(doc.LLMOrder, name)
		}
	}
	if len(doc.LLMOrder) > 0 {
		r.source(Path{keyProviders, "llm_order"}, r.owner)
	} else {
		// No provider at all: leave the block out rather than write an
		// empty mapping. Crewlet runs such a company and holds its seats'
		// work until a provider arrives.
		doc.LLM = nil
	}

	if e := spec.Embeddings; e.Model != "" {
		doc.Embeddings = &embeddingsDoc{
			Type:           embeddingProviderType(e.Type),
			Model:          e.Model,
			BaseURL:        e.BaseURL,
			Dimensions:     e.Dimensions,
			MaxInputTokens: e.MaxInputTokens,
			MaxBatchInputs: e.MaxBatchInputs,
			MaxBatchTokens: e.MaxBatchTokens,
		}
		if e.APIKeySecretRef.Name != "" {
			doc.Embeddings.APIKey = r.secret(e.APIKeySecretRef.Name, e.APIKeySecretRef.Key)
		}
		r.source(Path{keyProviders, "embeddings"}, r.owner)
	}

	if s := spec.Sandbox; s.E2B.APIKeySecretRef.Name != "" {
		doc.Sandbox = &sandboxDoc{
			E2B: e2bDoc{
				APIKey:   r.secret(s.E2B.APIKeySecretRef.Name, s.E2B.APIKeySecretRef.Key),
				Domain:   s.E2B.Domain,
				Template: s.E2B.Template,
			},
			// The remote sandbox is the only cell v1alpha1 offers; naming
			// it keeps the document valid without relying on Crewlet's
			// rule that a catalogue of one cell needs no default.
			DefaultRunIn:           "e2b",
			DefaultCodingAgent:     codingAgent(s.DefaultCodingAgent),
			DefaultTimeoutSeconds:  s.DefaultTimeoutSeconds,
			DefaultPauseTTLSeconds: s.DefaultPauseTTLSeconds,
			DefaultMaxTurns:        s.DefaultMaxTurns,
			Setup:                  r.setup(r.owner, s.Setup),
		}
		r.source(Path{keyProviders, "sandbox"}, r.owner)
	}
	return doc
}

// CLIStateRoot is the directory on a data pod's volume under which each
// LLMProvider keeps its login and its per-seat homes, one directory per
// loginName: on the volume, so they outlive the pod.
const CLIStateRoot = DataDir + "/llm-cli"

// CLIPlatformStateRoot is the directory on a data pod's volume under which
// each platform provider keeps its per-seat homes, one directory per name.
//
// A root of its own rather than CLIStateRoot, because a tenant chooses its
// providers' loginName freely: under one root, a loginName equal to a
// platform provider's name would point the two entries at one directory.
// Crewlet keeps each CLI's files in a directory of its own under it
// (<state_dir>/<agent>), so entries of different CLIs would share the
// directory and nothing in it; entries of the same CLI would share its login
// and seat homes, mixing the tenant's login into the directory of a provider
// every tenant of the class is given.
const CLIPlatformStateRoot = DataDir + "/llm-cli-platform"

// llmEntry renders what an LLMProvider and a platform provider share, with
// the entry's state directory.
//
// The web stance is written only when it is Denied: Allowed is the profile's
// own default, and an override that restates it would still be an override,
// which Crewlet merges into the profile and the document then carries for
// nothing.
func llmEntry(s *v1alpha1.LLMProviderSettings, stateDir string) llmProviderDoc {
	doc := llmProviderDoc{
		Agent:          cliAgentName(s.Agent),
		Model:          s.Model,
		StateDir:       stateDir,
		TimeoutSeconds: s.TimeoutSeconds,
		MaxConcurrent:  s.MaxConcurrent,
	}
	if s.WebTools == v1alpha1.CLIAgentWebToolsDenied {
		doc.Overrides = &llmOverridesDoc{WebTools: cliAgentWebTools(s.WebTools)}
	}
	return doc
}

// llmProvider renders one of the Company's LLMProviders. In mode Agent its
// runs happen in a remote sandbox and call the seat's tools back through the
// engine's public address; a company with none is applied all the same, and
// AgentModeOf reports it, since Crewlet refuses only the turns that need the
// address.
func (r *renderer) llmProvider(p *v1alpha1.LLMProvider) llmProviderDoc {
	s := &p.Spec
	doc := llmEntry(&s.LLMProviderSettings, CLIStateRoot+"/"+cmp.Or(s.LoginName, p.Name))
	doc.Mode = cliAgentMode(s.Mode)
	doc.RunIn = cliAgentRunIn(s.RunIn)
	doc.Env = r.secretEnv(s.Env)
	doc.Auth = llmAuthDoc{Mode: cliAgentAuthMode(s.Auth.Mode)}
	if k := s.Auth.TokenSecretRef; k.Name != "" {
		doc.Auth.Token = r.secret(k.Name, k.Key)
	}
	if k := s.Auth.APIKeySecretRef; k.Name != "" {
		doc.Auth.APIKey = r.secret(k.Name, k.Key)
	}
	if k := s.Auth.CredentialBundleSecretRef; k.Name != "" {
		doc.Auth.CredentialBundle = r.secret(k.Name, k.Key)
	}
	return doc
}

// platformProvider renders one of the EngineClass's platform providers, whose
// key and env are read from Secrets in the namespaces the class names.
func (r *renderer) platformProvider(p *v1alpha1.PlatformLLMProvider) llmProviderDoc {
	doc := llmEntry(&p.LLMProviderSettings, CLIPlatformStateRoot+"/"+p.Name)
	if len(p.Env) > 0 {
		doc.Env = make(map[string]string, len(p.Env))
		for _, e := range p.Env {
			ref := e.ValueFrom.SecretKeyRef
			doc.Env[e.Name] = r.secretKey(secretref.Key{Namespace: ref.Namespace, Name: ref.Name, Key: ref.Key})
		}
	}
	doc.Auth = llmAuthDoc{Mode: cliAgentAuthMode(p.Auth.Mode)}
	if k := p.Auth.APIKeySecretRef; k.Name != "" {
		doc.Auth.APIKey = r.secretKey(secretref.Key{Namespace: k.Namespace, Name: k.Name, Key: k.Key})
	}
	return doc
}

// absentKey is what Crewlet does with a providers.llm key the document does
// not hold, where the reference to it is written: the caller's to say, since
// one reference type is read in places Crewlet treats differently.
type absentKey int

const (
	// absentDropped: Crewlet drops the key (llm_order), and whatever then
	// comes first takes its place.
	absentDropped absentKey = iota + 1
	// absentRefused: Crewlet refuses the document (a seat's chain, the
	// assistant's chain).
	absentRefused
)

// provider resolves a reference to a model provider to its providers.llm
// key, or records why it cannot be resolved.
func (r *renderer) provider(kind, name, field string, ref v1alpha1.LLMProviderReference, absence absentKey) (string, bool) {
	field += ".name"
	var absent string
	switch ref.Kind {
	case v1alpha1.LLMProviderKindPlatform:
		switch {
		case !r.usesPlatformProviders():
			// Not the class's policy: the setting is the Company's own,
			// and the document holds none of the class's providers for a
			// Company that does not set it, so the key is absent as a
			// missing LLMProvider's is.
			absent = fmt.Sprintf("names the platform model provider %s, but Company %s does not set spec.llm.usePlatformProviders",
				ref.Name, r.in.Company.Name)
		case r.platform[ref.Name] == nil:
			r.errs.notFound(ClassPolicy, kind, name, field, fmt.Sprintf(
				"EngineClass %s offers no platform model provider named %s", r.in.Class.Name, ref.Name))
			return "", false
		}
	case v1alpha1.LLMProviderKindLLMProvider, "":
		// An unset kind is the API's default, LLMProvider.
		if !r.providers[ref.Name] {
			absent = fmt.Sprintf("names LLMProvider %s, which does not exist in namespace %s", ref.Name, r.namespace())
		}
	}
	if absent == "" {
		return ref.Name, true
	}
	// A key providers.llm does not hold is held for what Crewlet does with
	// it where it is written, whichever reason left it out.
	switch absence {
	case absentDropped:
		// A dropped key moves every reader of the list onto another
		// provider: harm beyond the object at fault.
		r.errs.notFound(HarmsBeyond, kind, name, field, absent)
	case absentRefused:
		r.errs.notFound(EngineRefuses, kind, name, field, absent)
	}
	return "", false
}

// setup renders owner's sandbox setup steps, the Company's or a Seat's, each
// at spec.sandbox.setup of owner.
func (r *renderer) setup(owner ObjectRef, steps []v1alpha1.SandboxSetupStep) []setupStepDoc {
	if len(steps) == 0 {
		return nil
	}
	out := make([]setupStepDoc, len(steps))
	for i := range steps {
		s := &steps[i]
		at := fmt.Sprintf("spec.sandbox.setup[%d]", i)
		use := func(field, name string) *startUse {
			return &startUse{
				object: owner, field: at + field, name: name, optional: slices.Contains(s.OptionalKeys, name),
				list: owner, listField: at + ".optionalKeys",
			}
		}
		out[i] = setupStepDoc{
			Name:           s.Name,
			Commands:       s.Commands,
			OptionalKeys:   sortedSet(s.OptionalKeys),
			Brief:          s.Brief,
			TimeoutSeconds: s.TimeoutSeconds,
		}
		if len(s.Env) > 0 {
			out[i].Env = make(map[string]string, len(s.Env))
			for j, e := range s.Env {
				out[i].Env[e.Name] = r.startSecret(use(fmt.Sprintf(".env[%d].valueFrom", j), e.Name), e.ValueFrom.SecretKeyRef)
			}
		}
		if len(s.Files) > 0 {
			out[i].Files = make(map[string]string, len(s.Files))
			for j, f := range s.Files {
				out[i].Files[f.Path] = r.startSecret(use(fmt.Sprintf(".files[%d].valueFrom", j), f.Path), f.ValueFrom.SecretKeyRef)
			}
		}
	}
	return out
}

func (r *renderer) secretEnv(env []v1alpha1.SecretEnvVar) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for _, e := range env {
		out[e.Name] = r.secret(e.ValueFrom.SecretKeyRef.Name, e.ValueFrom.SecretKeyRef.Key)
	}
	return out
}

// ---- integrations -------------------------------------------------------- //

func (r *renderer) integrations() integrationsDoc {
	doc := integrationsDoc{
		CheckIntervalSeconds: r.in.Company.Spec.Integrations.CheckIntervalSeconds,
	}
	if doc.CheckIntervalSeconds != 0 {
		r.source(Path{keyIntegrations, "check_interval_seconds"}, r.owner)
	}

	claimed := map[string]string{}
	for _, in := range sortedByName(r.in.Integrations) {
		vendor := Vendor(in)
		// Each vendor is one block of Crewlet's integrations, so a second
		// Integration for it would replace the first. The one that sorts
		// first by name keeps the block.
		if other, taken := claimed[vendor]; taken {
			r.errs.add(HarmsBeyond, KindIntegration, in.Name, "spec."+vendor, v1alpha1.ReasonConflict, fmt.Sprintf(
				"Integration %s already connects %s for Company %s: a company has one Integration per vendor", other, vendor, r.in.Company.Name))
			continue
		}
		claimed[vendor] = in.Name
		r.source(Path{keyIntegrations, vendor}, ObjectRef{Kind: KindIntegration, Name: in.Name})
		r.integration(&doc, in)
	}
	return doc
}

// The vendors an Integration connects, by the key of Crewlet's integrations
// block each is.
const (
	VendorSlack      = "slack"
	VendorGitHub     = "github"
	VendorGitLab     = "gitlab"
	VendorMattermost = "mattermost"
	VendorJira       = "jira"
	VendorConfluence = "confluence"
	VendorDatadog    = "datadog"
	VendorAtlassian  = "atlassian"
)

// Vendor is the vendor an Integration connects: the key of the block of
// Crewlet's integrations it renders. The API admits exactly one member.
func Vendor(in *v1alpha1.Integration) string {
	spec := &in.Spec
	switch {
	case spec.Slack != nil:
		return VendorSlack
	case spec.GitLab.URL != "":
		return VendorGitLab
	case spec.Mattermost.URL != "":
		return VendorMattermost
	case spec.Jira.TokenSecretRef.Name != "":
		return VendorJira
	case spec.Confluence.TokenSecretRef.Name != "":
		return VendorConfluence
	case spec.Datadog.WebhookTokenSecretRef.Name != "":
		return VendorDatadog
	case spec.Atlassian.OrgID != "":
		return VendorAtlassian
	}
	return VendorGitHub
}

// integration renders one Integration's block into doc.
func (r *renderer) integration(doc *integrationsDoc, in *v1alpha1.Integration) {
	spec := &in.Spec
	ref := func(k v1alpha1.SecretKeyReference) string {
		if k.Name == "" {
			return ""
		}
		return r.secret(k.Name, k.Key)
	}
	switch Vendor(in) {
	case VendorSlack:
		s := spec.Slack
		doc.Slack = &slackDoc{
			TypingStatus: workingStatus(s.TypingStatus),
			StatusPhrases: statusPhrasesDoc{
				Onboarding: s.StatusPhrases.Onboarding,
				Execute:    s.StatusPhrases.Execute,
				Review:     s.StatusPhrases.Review,
				Default:    s.StatusPhrases.Default,
			},
		}
	case VendorGitHub:
		g := &spec.GitHub
		doc.GitHub = &githubDoc{
			Enabled:       true,
			URL:           g.URL,
			WebhookSecret: ref(g.WebhookSecretRef),
			Token:         ref(g.TokenSecretRef),
		}
		if p := g.Provisioning; p.Org != "" || len(p.Repos) > 0 || p.OrgWebhook != "" {
			doc.GitHub.Provisioning = &githubProvisioningDoc{
				Org:        p.Org,
				Repos:      p.Repos,
				OrgWebhook: containerWebhookMode(p.OrgWebhook),
			}
		}
	case VendorGitLab:
		g := &spec.GitLab
		doc.GitLab = &gitlabDoc{
			Enabled:       true,
			URL:           g.URL,
			SigningSecret: ref(g.SigningSecretRef),
			Token:         ref(g.TokenSecretRef),
			WebhookName:   g.WebhookName,
		}
		if p := &g.EngineProvisioning; p.Group != "" {
			doc.GitLab.Provisioning = &gitlabProvisioningDoc{
				Group:          p.Group,
				AdminToken:     ref(p.AdminTokenSecretRef),
				Mode:           gitLabAccountOwner(p.AccountOwner),
				AccessLevel:    gitLabAccessLevel(p.AccessLevel),
				AccessLevels:   r.gitLabAccessLevels(in, p.AccessLevels),
				UsernamePrefix: p.UsernamePrefix,
				Projects:       p.Projects,
				GroupWebhook:   containerWebhookMode(p.GroupWebhook),
				TokenScopes:    p.TokenScopes,
			}
		}
	case VendorMattermost:
		m := &spec.Mattermost
		doc.Mattermost = &mattermostDoc{
			Enabled:      true,
			URL:          m.URL,
			Team:         m.Team,
			TypingStatus: workingStatus(m.TypingStatus),
		}
		if p := &m.EngineProvisioning; p.AdminTokenSecretRef.Name != "" {
			doc.Mattermost.Provisioning = &mattermostProvisioningDoc{
				AdminToken:        ref(p.AdminTokenSecretRef),
				UsernamePrefix:    p.UsernamePrefix,
				Channels:          p.Channels,
				DisplayNameSuffix: p.DisplayNameSuffix,
			}
		}
	case VendorJira:
		j := &spec.Jira
		doc.Jira = &jiraDoc{
			URL:           j.URL,
			CloudID:       j.CloudID,
			SiteURL:       j.SiteURL,
			Email:         j.Email,
			Token:         ref(j.TokenSecretRef),
			WebhookSecret: ref(j.WebhookSecretRef),
			WebhookName:   j.WebhookName,
		}
	case VendorConfluence:
		c := &spec.Confluence
		doc.Confluence = &confluenceDoc{
			URL:           c.URL,
			CloudID:       c.CloudID,
			SiteURL:       c.SiteURL,
			Email:         c.Email,
			Token:         ref(c.TokenSecretRef),
			WebhookSecret: ref(c.WebhookSecretRef),
			WebhookToken:  ref(c.WebhookTokenSecretRef),
		}
	case VendorDatadog:
		d := &spec.Datadog
		doc.Datadog = &datadogDoc{
			Enabled:      true,
			WebhookToken: ref(d.WebhookTokenSecretRef),
			RouteTo:      r.datadogRouteTo(in),
			WebhookName:  d.WebhookName,
			HandleTag:    d.HandleTag,
			Provisioning: datadogProvisioningDoc{
				Site:        datadogSite(d.Site),
				APIKey:      ref(d.APIKeySecretRef),
				AppKey:      ref(d.AppKeySecretRef),
				Role:        d.Role,
				EmailDomain: d.EmailDomain,
			},
		}
	case VendorAtlassian:
		a := &spec.Atlassian
		doc.Atlassian = &atlassianDoc{OrgID: a.OrgID, APIKey: ref(a.APIKeySecretRef)}
	}
}

// gitLabAccessLevels renders a GitLab Integration's per-seat access levels,
// which Crewlet keys by the seat's handle. A Seat that does not exist is
// refused: written by its name, the level would go to whichever seat takes
// that handle later, which nobody granted it to. A Human seat is rendered and
// reported: the engine creates no GitLab account for a person, so its entry
// grants nothing until the Seat becomes an Agent seat, which is what it says.
func (r *renderer) gitLabAccessLevels(in *v1alpha1.Integration, levels []v1alpha1.GitLabSeatAccess) map[string]string {
	if len(levels) == 0 {
		return nil
	}
	out := make(map[string]string, len(levels))
	for i, l := range levels {
		field := fmt.Sprintf("spec.gitlab.engineProvisioning.accessLevels[%d].name", i)
		s := r.seats[l.Name]
		switch {
		case s == nil:
			r.errs.notFound(HarmsBeyond, KindIntegration, in.Name, field, r.noSeat(l.Name))
			continue
		case s.Spec.Kind == v1alpha1.SeatKindHuman:
			r.errs.add(Reported, KindIntegration, in.Name, field, v1alpha1.ReasonHumanSeat, fmt.Sprintf(
				"names Seat %s, a Human seat, which has no GitLab account: the engine provisions agent seats only, so "+
					"the entry grants nothing", l.Name))
		}
		out[SeatHandle(s)] = gitLabAccessLevel(l.AccessLevel)
	}
	return out
}

// datadogRouteTo renders a Datadog Integration's route_to: the handle of the
// seat an alert naming no seat wakes, or Crewlet's `none`. Crewlet refuses a
// route_to naming no seat, or a person, on every write.
func (r *renderer) datadogRouteTo(in *v1alpha1.Integration) string {
	route := in.Spec.Datadog.RouteTo
	if route.Dismiss != nil && *route.Dismiss {
		return crewletNone
	}
	const field = "spec.datadog.routeTo.seatRef.name"
	name := route.SeatRef.Name
	s := r.seats[name]
	switch {
	case s == nil:
		r.errs.notFound(EngineRefuses, KindIntegration, in.Name, field, r.noSeat(name))
		return ""
	case s.Spec.Kind == v1alpha1.SeatKindHuman:
		r.errs.add(EngineRefuses, KindIntegration, in.Name, field, v1alpha1.ReasonHumanSeat, fmt.Sprintf(
			"names Seat %s, a Human seat, and the engine runs no turn for a person: route the alert to an Agent seat", name))
		return ""
	}
	return SeatHandle(s)
}

// noSeat is the message of a reference to a Seat that does not exist.
func (r *renderer) noSeat(name string) string {
	return fmt.Sprintf("names Seat %s, which does not exist in namespace %s", name, r.namespace())
}

// ---- tool servers ------------------------------------------------------- //

func (r *renderer) mcpServers() []mcpServerDoc {
	sorted := sortedByName(r.in.MCPServers)
	out := make([]mcpServerDoc, 0, len(sorted))
	for i, s := range sorted {
		spec := &s.Spec
		doc := mcpServerDoc{
			Name:                  s.Name,
			Transport:             mcpTransport(spec.Transport),
			Shared:                spec.Shared,
			Command:               spec.Command,
			Args:                  spec.Args,
			URL:                   spec.URL,
			OptionalKeys:          r.mcpOptionalKeys(s),
			ToolPrefix:            spec.ToolPrefix,
			StartupTimeoutSeconds: spec.StartupTimeoutSeconds,
			RequestTimeoutSeconds: spec.RequestTimeoutSeconds,
		}
		if len(spec.Env) > 0 {
			doc.Env = make(map[string]string, len(spec.Env))
			for j, e := range spec.Env {
				doc.Env[e.Name] = r.serverValue(s, fmt.Sprintf("spec.env[%d].valueFrom", j), e.Name, e.Value, e.ValueFrom)
			}
		}
		if len(spec.Headers) > 0 {
			doc.Headers = make(map[string]string, len(spec.Headers))
			for j, h := range spec.Headers {
				doc.Headers[h.Name] = r.serverValue(s, fmt.Sprintf("spec.headers[%d].valueFrom", j), h.Name, h.Value, h.ValueFrom)
			}
		}
		if len(spec.ToolAnnotations) > 0 {
			doc.ToolAnnotations = make(map[string]toolAnnotationsDoc, len(spec.ToolAnnotations))
			for _, a := range spec.ToolAnnotations {
				doc.ToolAnnotations[a.Tool] = toolAnnotationsDoc{
					ReadOnly:    a.ReadOnly,
					Destructive: a.Destructive,
					Idempotent:  a.Idempotent,
					OpenWorld:   a.OpenWorld,
				}
			}
		}
		out = append(out, doc)
		r.source(Path{keyMCPServers, i}, ObjectRef{Kind: KindMCPServer, Name: s.Name})
	}
	return out
}

// serverValue renders one of s's own values, of its env or its headers, under
// name: a literal as it is written, which the API admits only under a name
// that holds no credential, and otherwise the Secret key it is read from,
// written at field.
func (r *renderer) serverValue(s *v1alpha1.MCPServer, field, name string, literal *string, from v1alpha1.SecretValueSource) string {
	if literal != nil {
		return *literal
	}
	return r.startSecret(&startUse{
		object: ObjectRef{Kind: KindMCPServer, Name: s.Name}, field: field, name: name, server: s.Name,
		optional: optionalKey(s, name), list: ObjectRef{Kind: KindMCPServer, Name: s.Name}, listField: fieldOptionalKeys,
	}, from.SecretKeyRef)
}

// mcpEnv renders a Unit's or a Seat's per-server values as Crewlet's
// mcp_env, a mapping from server name to variable (or header) name to value:
// a setting as it is written, and a credential by the function value, which
// renders one at its field of the object, started with the server as use
// says.
func (r *renderer) mcpEnv(
	kind, name string, creds []v1alpha1.MCPCredentials,
	value func(field string, src v1alpha1.SeatCredentialSource, use *startUse) string,
) map[string]map[string]string {
	if len(creds) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(creds))
	for i, c := range creds {
		if r.mcp[c.Name] == nil {
			r.errs.notFound(HarmsBeyond, kind, name, fmt.Sprintf("spec.mcp[%d].name", i), fmt.Sprintf(
				"names MCPServer %s, which does not exist in namespace %s", c.Name, r.namespace()))
			continue
		}
		server := r.mcp[c.Name]
		values := make(map[string]string, len(c.Env))
		for j, v := range c.Env {
			if v.Value != nil {
				values[v.Name] = *v.Value
				continue
			}
			field := fmt.Sprintf("spec.mcp[%d].env[%d].valueFrom", i, j)
			values[v.Name] = value(field, v.ValueFrom, &startUse{
				object: ObjectRef{Kind: kind, Name: name}, field: field, name: v.Name, server: c.Name,
				optional: optionalKey(server, v.Name), list: ObjectRef{Kind: KindMCPServer, Name: c.Name},
				listField: fieldOptionalKeys,
			})
		}
		out[c.Name] = values
	}
	return out
}

// sortedByName returns pointers to the objects in items, ordered by
// metadata.name, leaving items as it is.
func sortedByName[T any, P interface {
	*T
	GetName() string
}](items []T) []P {
	out := make([]P, len(items))
	for i := range items {
		out[i] = &items[i]
	}
	slices.SortFunc(out, func(a, b P) int { return strings.Compare(a.GetName(), b.GetName()) })
	return out
}

func sortedKeys(set map[secretref.Key]struct{}) []secretref.Key {
	return slices.SortedFunc(maps.Keys(set), secretref.Compare)
}
