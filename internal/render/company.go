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

	"sigs.k8s.io/yaml"

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

	// PublicBaseURL is the address vendors deliver webhooks to, rendered as
	// integrations.public_base_url: the Company's
	// spec.exposure.publicBaseURL, or the one the class's webhooks.hostname
	// derives. Empty when the company is not exposed.
	PublicBaseURL string
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
// reference between objects that cannot be rendered, and one for each
// ${NAME} reference an object holds itself (checkReferences). Nothing is
// rendered then, because a document is applied whole or not at all. The only
// other failure is an engineConfig value that is not JSON, which the API
// server never stores.
func Company(in *Input) (*Document, error) {
	r, merged, err := renderJSON(in, in.StoreName, in.ProvisionedName)
	if err != nil {
		return nil, err
	}
	out, err := yaml.JSONToYAML(merged)
	if err != nil {
		return nil, fmt.Errorf("converting the company document to YAML: %w", err)
	}

	sum := sha256.Sum256(out)
	slices.SortFunc(r.sources, func(a, b Source) int { return strings.Compare(a.Path.String(), b.Path.String()) })
	rendered := &Document{
		YAML:        out,
		Hash:        hex.EncodeToString(sum[:]),
		Refs:        sortedKeys(r.refs),
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
	// encoding/json writes map keys sorted, and JSONToYAML keeps them so.
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
// goes: an MCP server's url, a provider's base_url, a sandbox setup command,
// a skill variable. A reference anywhere else would let whoever writes the
// object name any value the engine can resolve (the class's platform model
// keys in its store, its environment) and have the engine send it where they
// chose. The API refuses one in the typed fields the engine is known to
// expand; this check covers every string, engineConfig's included, and
// whatever field Crewlet expands next. A string that merely equals a
// reference the renderer wrote elsewhere is refused too: it names that
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
		errs.add(owner.Kind, owner.Name, "document "+at.String(), v1alpha1.ReasonReferenceNotAllowed, fmt.Sprintf(
			"holds the reference %s, which the engine would resolve from its own secret store and environment: "+
				"name a credential only with a Secret reference, which the operator turns into a reference of its own",
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
	platform  map[string]bool
	// cli are the CLIAgent providers' cli blocks, by name.
	cliProviders map[string]*v1alpha1.CLIAgentSettings

	// seatSchedules and unitSchedules are the Schedules whose target
	// resolves, by the metadata.name of the Seat or Unit they target.
	seatSchedules map[string][]*v1alpha1.Schedule
	unitSchedules map[string][]*v1alpha1.Schedule

	// topology is where each agent seat runs, which its role.placement
	// pins it to.
	topology *Topology

	refs        map[secretref.Key]struct{}
	provisioned map[string]struct{}
	sources     []Source
	errs        refErrors
}

func newRenderer(in *Input) *renderer {
	r := &renderer{
		in:           in,
		owner:        ObjectRef{Kind: KindCompany, Name: in.Company.Name},
		units:        map[string]*v1alpha1.Unit{},
		seats:        map[string]*v1alpha1.Seat{},
		mcp:          map[string]*v1alpha1.MCPServer{},
		providers:    map[string]bool{},
		platform:     map[string]bool{},
		cliProviders: map[string]*v1alpha1.CLIAgentSettings{},
		refs:         map[secretref.Key]struct{}{},
		provisioned:  map[string]struct{}{},
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
		if p.Spec.Type == v1alpha1.LLMProviderCLIAgent {
			r.cliProviders[p.Name] = &p.Spec.CLI
		}
	}
	if r.usesPlatformProviders() {
		for i := range in.Class.Spec.PlatformLLMProviders {
			r.platform[in.Class.Spec.PlatformLLMProviders[i].Name] = true
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
	return "${" + r.in.StoreName(k) + "}"
}

// seatCredential returns the pointer the document holds for one of a seat's
// own credentials: a Secret key's store entry, which the secret sync fills,
// or a provisioned credential's, which the engine's provisioning fills. A
// provisioned credential in a company where no Integration provisions is
// refused at field: nothing would ever fill it.
func (r *renderer) seatCredential(s *v1alpha1.Seat, field string, src v1alpha1.SeatCredentialSource) string {
	if ref := src.SecretKeyRef; ref.Name != "" {
		return r.secret(ref.Name, ref.Key)
	}
	if !r.engineProvisions() {
		r.errs.add(KindSeat, s.Name, field, v1alpha1.ReasonNoProvisioner, fmt.Sprintf(
			"names the provisioned credential %s, and no Integration of Company %s has provisioning Engine, "+
				"so nothing would create it: give the value from a Secret, or provision the vendor with the engine",
			src.Provisioned.Name, r.in.Company.Name))
		return ""
	}
	name := r.in.ProvisionedName(SeatHandle(s), src.Provisioned.Name)
	r.provisioned[name] = struct{}{}
	return "${" + name + "}"
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
	doc.Integrations = r.integrations()
	doc.MCPServers = r.mcpServers()
	doc.Roles, doc.Units = r.org()
	return doc
}

func tokenBudget(b v1alpha1.TokenBudget) tokenBudgetDoc {
	return tokenBudgetDoc{Day: b.Day, Week: b.Week, Month: b.Month}
}

// ---- providers ----------------------------------------------------------- //

func (r *renderer) providersDoc() providersDoc {
	spec := &r.in.Company.Spec
	doc := providersDoc{LLM: map[string]llmProviderDoc{}}

	own := sortedByName(r.in.LLMProviders)
	for _, p := range own {
		keys := make([]string, len(p.Spec.APIKeySecretRefs))
		for i, ref := range p.Spec.APIKeySecretRefs {
			keys[i] = r.secret(ref.Name, ref.Key)
		}
		entry := llmProvider(&p.Spec.LLMProviderSettings, keys)
		if p.Spec.Type == v1alpha1.LLMProviderCLIAgent {
			entry.CLI = r.cli(p)
		}
		doc.LLM[p.Name] = entry
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
				r.errs.add(KindLLMProvider, p.Name, "metadata.name", v1alpha1.ReasonConflict,
					fmt.Sprintf("EngineClass %s offers a platform model provider named %s, and both are keys of the company's one providers.llm mapping: rename this LLMProvider", r.in.Class.Name, p.Name))
				continue
			}
			keys := make([]string, len(p.APIKeySecretRefs))
			for i, ref := range p.APIKeySecretRefs {
				keys[i] = r.secretKey(secretref.Key{Namespace: ref.Namespace, Name: ref.Name, Key: ref.Key})
			}
			doc.LLM[p.Name] = llmProvider(&p.LLMProviderSettings, keys)
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
		key, ok := r.provider(KindCompany, r.in.Company.Name, fmt.Sprintf("spec.llm.order[%d]", i), ref)
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
			Setup:                  r.setup(s.Setup),
		}
		r.source(Path{keyProviders, "sandbox"}, r.owner)
	}
	return doc
}

func llmProvider(s *v1alpha1.LLMProviderSettings, keys []string) llmProviderDoc {
	doc := llmProviderDoc{
		Type:                  llmProviderType(s.Type),
		Model:                 s.Model,
		ClaudeModel:           s.ClaudeModel,
		APIKeys:               keys,
		BaseURL:               s.BaseURL,
		Reasoning:             s.Reasoning,
		ReasoningEffort:       reasoningEffort(s.ReasoningEffort),
		ReasoningBudgetTokens: s.ReasoningBudgetTokens,
		TimeoutSeconds:        s.TimeoutSeconds,
	}
	if c := s.Cooldowns; c != (v1alpha1.LLMCredentialCooldowns{}) {
		doc.Cooldowns = &cooldownsDoc{RateLimitSeconds: c.RateLimitSeconds, AuthSeconds: c.AuthSeconds}
	}
	return doc
}

// CLIStateRoot is the directory on a data pod's volume under which each
// CLIAgent provider keeps its login and its per-seat homes, one directory per
// loginName: on the volume, so they outlive the pod.
const CLIStateRoot = DataDir + "/llm-cli"

// cli renders the cli block of a CLIAgent provider, or records why it cannot
// run: in mode Agent its runs happen in a remote sandbox and call the seat's
// tools back through the engine's public address, which the company must have.
func (r *renderer) cli(p *v1alpha1.LLMProvider) *cliDoc {
	c := &p.Spec.CLI
	if c.Mode == v1alpha1.CLIAgentModeAgent && r.in.PublicBaseURL == "" {
		r.errs.add(KindLLMProvider, p.Name, "spec.cli.mode", v1alpha1.ReasonNotExposed, fmt.Sprintf(
			"is Agent, whose runs happen in a sandbox that calls the seat's tools back through the engine's public address, "+
				"and Company %s has none (status.endpoints.publicBaseURL): give its EngineClass a webhooks route, or the "+
				"Company spec.exposure.publicBaseURL, or use mode Text", r.in.Company.Name))
	}
	login := cmp.Or(c.LoginName, p.Name)
	doc := &cliDoc{
		Agent:          cliAgentName(c.Agent),
		Mode:           cliAgentMode(c.Mode),
		RunIn:          cliAgentRunIn(c.RunIn),
		StateDir:       CLIStateRoot + "/" + login,
		TimeoutSeconds: c.TimeoutSeconds,
		MaxConcurrent:  c.MaxConcurrent,
		Env:            r.secretEnv(c.Env),
		Auth:           cliAuthDoc{Mode: cliAgentAuthMode(c.Auth.Mode)},
	}
	if k := c.Auth.TokenSecretRef; k.Name != "" {
		doc.Auth.Token = r.secret(k.Name, k.Key)
	}
	if k := c.Auth.CredentialBundleSecretRef; k.Name != "" {
		doc.Auth.CredentialBundle = r.secret(k.Name, k.Key)
	}
	return doc
}

// provider resolves a reference to a model provider to its providers.llm
// key, or records why it cannot be resolved.
func (r *renderer) provider(kind, name, field string, ref v1alpha1.LLMProviderReference) (string, bool) {
	field += ".name"
	switch ref.Kind {
	case v1alpha1.LLMProviderKindPlatform:
		switch {
		case !r.usesPlatformProviders():
			r.errs.notFound(kind, name, field, fmt.Sprintf(
				"names the platform model provider %s, but Company %s does not set spec.llm.usePlatformProviders", ref.Name, r.in.Company.Name))
			return "", false
		case !r.platform[ref.Name]:
			r.errs.notFound(kind, name, field, fmt.Sprintf(
				"EngineClass %s offers no platform model provider named %s", r.in.Class.Name, ref.Name))
			return "", false
		}
	case v1alpha1.LLMProviderKindLLMProvider, "":
		// An unset kind is the API's default, LLMProvider.
		if !r.providers[ref.Name] {
			r.errs.notFound(kind, name, field, fmt.Sprintf(
				"names LLMProvider %s, which does not exist in namespace %s", ref.Name, r.namespace()))
			return "", false
		}
	}
	return ref.Name, true
}

func (r *renderer) setup(steps []v1alpha1.SandboxSetupStep) []setupStepDoc {
	if len(steps) == 0 {
		return nil
	}
	out := make([]setupStepDoc, len(steps))
	for i, s := range steps {
		out[i] = setupStepDoc{
			Name:           s.Name,
			Commands:       s.Commands,
			Env:            r.secretEnv(s.Env),
			Brief:          s.Brief,
			TimeoutSeconds: s.TimeoutSeconds,
		}
		if len(s.Files) > 0 {
			out[i].Files = map[string]string{}
			for _, f := range s.Files {
				out[i].Files[f.Path] = r.secret(f.ValueFrom.SecretKeyRef.Name, f.ValueFrom.SecretKeyRef.Key)
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
		PublicBaseURL:        r.in.PublicBaseURL,
		CheckIntervalSeconds: r.in.Company.Spec.Integrations.CheckIntervalSeconds,
	}
	if doc.PublicBaseURL != "" {
		r.source(Path{keyIntegrations, "public_base_url"}, r.owner)
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
			r.errs.add(KindIntegration, in.Name, "spec."+vendor, v1alpha1.ReasonConflict, fmt.Sprintf(
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
// which Crewlet keys by the seat's handle, or records why a Seat named there
// cannot carry one.
func (r *renderer) gitLabAccessLevels(in *v1alpha1.Integration, levels []v1alpha1.GitLabSeatAccess) map[string]string {
	if len(levels) == 0 {
		return nil
	}
	out := make(map[string]string, len(levels))
	for i, l := range levels {
		if handle, ok := r.agentSeat(in, fmt.Sprintf("spec.gitlab.engineProvisioning.accessLevels[%d].name", i), l.Name,
			"which has no GitLab account: the engine provisions agent seats only"); ok {
			out[handle] = gitLabAccessLevel(l.AccessLevel)
		}
	}
	return out
}

// datadogRouteTo renders a Datadog Integration's route_to: the handle of the
// seat an alert naming no seat wakes, or Crewlet's `none`.
func (r *renderer) datadogRouteTo(in *v1alpha1.Integration) string {
	route := in.Spec.Datadog.RouteTo
	if route.Dismiss != nil && *route.Dismiss {
		return crewletNone
	}
	handle, _ := r.agentSeat(in, "spec.datadog.routeTo.seatRef.name", route.SeatRef.Name,
		"and the engine runs no turn for a person: route the alert to an Agent seat")
	return handle
}

// agentSeat resolves the Seat an Integration names at field to its handle,
// or records why it cannot be named there: it does not exist, or it is a
// Human seat, which humanReason says the consequence of.
func (r *renderer) agentSeat(in *v1alpha1.Integration, field, name, humanReason string) (string, bool) {
	s := r.seats[name]
	switch {
	case s == nil:
		r.errs.notFound(KindIntegration, in.Name, field, fmt.Sprintf(
			"names Seat %s, which does not exist in namespace %s", name, r.namespace()))
		return "", false
	case s.Spec.Kind == v1alpha1.SeatKindHuman:
		r.errs.add(KindIntegration, in.Name, field, v1alpha1.ReasonHumanSeat, fmt.Sprintf(
			"names Seat %s, a Human seat, %s", name, humanReason))
		return "", false
	}
	return SeatHandle(s), true
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
			Env:                   r.secretEnv(spec.Env),
			URL:                   spec.URL,
			ToolPrefix:            spec.ToolPrefix,
			StartupTimeoutSeconds: spec.StartupTimeoutSeconds,
			RequestTimeoutSeconds: spec.RequestTimeoutSeconds,
		}
		if len(spec.Headers) > 0 {
			doc.Headers = make(map[string]string, len(spec.Headers))
			for _, h := range spec.Headers {
				doc.Headers[h.Name] = r.secret(h.ValueFrom.SecretKeyRef.Name, h.ValueFrom.SecretKeyRef.Key)
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

// mcpEnv renders a Unit's or a Seat's per-server credentials as Crewlet's
// mcp_env, a mapping from server name to variable (or header) name to value.
// value renders one credential, at its field of the object.
func (r *renderer) mcpEnv(
	kind, name string, creds []v1alpha1.MCPCredentials, value func(field string, src v1alpha1.SeatCredentialSource) string,
) map[string]map[string]string {
	if len(creds) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(creds))
	for i, c := range creds {
		if r.mcp[c.Name] == nil {
			r.errs.notFound(kind, name, fmt.Sprintf("spec.mcp[%d].name", i), fmt.Sprintf(
				"names MCPServer %s, which does not exist in namespace %s", c.Name, r.namespace()))
			continue
		}
		values := make(map[string]string, len(c.Env))
		for j, v := range c.Env {
			values[v.Name] = value(fmt.Sprintf("spec.mcp[%d].env[%d].valueFrom", i, j), v.ValueFrom)
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
