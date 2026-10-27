package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/test/docs"
)

// The functions of the render that record a finding, by name: named by the
// function itself, so that a rename fails to compile rather than leaving a
// row of the table that no place answers.
var (
	fnCheckReferences    = nameOf(checkReferences)
	fnProvidersDoc       = nameOf((*renderer).providersDoc)
	fnProvider           = nameOf((*renderer).provider)
	fnIntegrations       = nameOf((*renderer).integrations)
	fnGitLabAccessLevels = nameOf((*renderer).gitLabAccessLevels)
	fnDatadogRouteTo     = nameOf((*renderer).datadogRouteTo)
	fnMcpEnv             = nameOf((*renderer).mcpEnv)
	fnMcpOptionalKeys    = nameOf((*renderer).mcpOptionalKeys)
	fnHeaderSpellings    = nameOf((*renderer).headerSpellings)
	fnSeatCredential     = nameOf((*renderer).seatCredential)
	fnPlanTopology       = nameOf(planTopology)
	fnCheckCapacity      = nameOf(checkCapacity)
	fnOrg                = nameOf((*renderer).org)
	fnConflict           = nameOf(conflict[*v1alpha1.Seat])
	fnUnit               = nameOf((*renderer).unit)
	fnRole               = nameOf((*renderer).role)
	fnScheduleTargets    = nameOf((*renderer).scheduleTargets)
	fnManages            = nameOf((*renderer).manages)
	fnSeatPodProviders   = nameOf((*renderer).seatPodProviders)
	fnAtlassianAccounts  = nameOf((*renderer).atlassianAccounts)
	fnNoExecutor         = nameOf((*renderer).noExecutor)
	fnSandboxRequired    = nameOf((*renderer).sandboxRequired)
	fnUnoffered          = nameOf((*renderer).unofferedPlacements)
)

// nameOf is the name a function of the package is declared under: the last
// element of its symbol, without a generic function's type arguments.
func nameOf(f any) string {
	symbol := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	symbol, _, _ = strings.Cut(symbol, "[")
	return symbol[strings.LastIndexByte(symbol, '.')+1:]
}

// The methods of refErrors that record a finding, and the package of the
// reasons.
const (
	methodAdd      = "add"
	methodNotFound = "notFound"
	apiPackage     = "v1alpha1"
)

// site is one place the render records a finding: the function it is in, the
// basis it records it on, and the reason.
type site struct {
	fn     string
	basis  Basis
	reason string
}

func (s site) String() string { return s.fn + " " + s.basis.String() + " " + s.reason }

// findings is every place the render records a finding, classified as
// ADR-0005 asks, each with why. It is the one list of what holds a company's
// document: a place added without a row, or a row whose place is gone, fails
// TestEveryFindingHasABasis, so a new finding is classified in review rather
// than held by default.
var findings = []struct {
	site
	why string
}{
	{site{fnCheckReferences, HarmsBeyond, v1alpha1.ReasonReferenceNotAllowed},
		"the engine resolves a ${NAME} from its own store and environment: another object's credential"},
	{site{fnProvidersDoc, HarmsBeyond, v1alpha1.ReasonConflict},
		"one key of providers.llm: the tenant's provider would replace the class's for every seat on it"},
	{site{fnProvider, ClassPolicy, v1alpha1.ReasonRefNotFound},
		"the class offers no platform provider of that name"},
	{site{fnProvider, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"the engine drops an llm_order key naming no provider (no such LLMProvider, or a platform provider the Company " +
			"does not use): every chainless seat runs on whatever comes first then"},
	{site{fnProvider, EngineRefuses, v1alpha1.ReasonRefNotFound},
		"a seat's chain, the assistant's, or the provider its deep work runs on, naming no provider: no such " +
			"LLMProvider, or a platform provider the Company does not use, which is the tenant's setting and not the " +
			"class's policy"},
	{site{fnIntegrations, HarmsBeyond, v1alpha1.ReasonConflict},
		"one block per vendor: a new Integration would displace the running one"},
	{site{fnGitLabAccessLevels, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"written by name, the level goes to whichever seat takes the handle later"},
	{site{fnGitLabAccessLevels, Reported, v1alpha1.ReasonHumanSeat},
		"the engine creates no account for a person: the entry grants nothing"},
	{site{fnDatadogRouteTo, EngineRefuses, v1alpha1.ReasonRefNotFound}, "route_to naming no seat"},
	{site{fnDatadogRouteTo, EngineRefuses, v1alpha1.ReasonHumanSeat}, "route_to naming a person"},
	{site{fnMcpEnv, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"the credentials would go to whichever server takes the name later"},
	{site{fnMcpOptionalKeys, EngineRefuses, v1alpha1.ReasonOptionalKeyUnused},
		"an optional key no value read from a Secret or provisioned is written under leaves nothing out"},
	{site{fnHeaderSpellings, EngineRefuses, v1alpha1.ReasonConflict},
		"a seat's header spelled unlike its unit's: the seat's instance would send either value"},
	{site{fnSeatCredential, Reported, v1alpha1.ReasonNoProvisioner},
		"the engine applies the reference; only the seat's own tool goes without it"},
	{site{fnPlanTopology, ClassPolicy, v1alpha1.ReasonSeatPoolsNotAllowed}, "the class forbids seat pools"},
	{site{fnPlanTopology, Reported, v1alpha1.ReasonPoolNotFound},
		"the engine accepts any pin; only the seat runs nowhere"},
	{site{fnPlanTopology, ClassPolicy, v1alpha1.ReasonPerSeatNotAllowed}, "the class forbids PerSeat pools"},
	{site{fnCheckCapacity, ClassPolicy, v1alpha1.ReasonPoolCapacityExceeded}, "the Shared pools pass the class's maxPods"},
	{site{fnCheckCapacity, ClassPolicy, v1alpha1.ReasonPoolCapacityExceeded}, "the first PerSeat seat past maxPods"},
	{site{fnCheckCapacity, ClassPolicy, v1alpha1.ReasonPoolCapacityExceeded}, "every PerSeat seat after it"},
	{site{fnOrg, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"a Unit with no parent to sit under: left out, its whole subtree goes"},
	{site{fnOrg, HarmsBeyond, v1alpha1.ReasonCycle}, "a loop of Units: none of them can be placed"},
	{site{fnOrg, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"a Seat with no unit: moved or left out, its unit's members and lead change and a running seat retires"},
	{site{fnConflict, EngineRefuses, v1alpha1.ReasonConflict},
		"two seats with one identity: a handle, an email address or a contact account"},
	{site{fnAtlassianAccounts, Reported, v1alpha1.ReasonNotAnAccountID},
		"the engine registers any value; only the person goes unrecognized on Jira and Confluence Cloud"},
	{site{fnUnit, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"the engine runs the unit and every descendant inheriting its lead with no lead"},
	{site{fnRole, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"the sandbox would reach whichever server takes the name later"},
	{site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonRefNotFound},
		"a schedule lives inside its seat's entry, which does not exist"},
	{site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonHumanSeat}, "a schedule on a person"},
	{site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonRefNotFound},
		"a schedule lives inside its unit's entry, which does not exist"},
	{site{fnManages, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"written by key, the authority goes to whichever unit takes the key later"},
	{site{fnManages, HarmsBeyond, v1alpha1.ReasonConflict},
		"the unit's key is a seat's handle: the engine would read the Unit as the Seat, and grant that"},
	{site{fnManages, HarmsBeyond, v1alpha1.ReasonRefNotFound},
		"written by handle, the authority goes to whichever seat takes the handle later"},
	{site{fnSeatPodProviders, HarmsBeyond, v1alpha1.ReasonCLIAuthNotPortable},
		"every seat pod refreshes the one login on its own and logs every seat on it out"},
	{site{fnNoExecutor, EngineRefuses, v1alpha1.ReasonNoExecutorCLI},
		"a seat that names no chain, on the head of the order, a CLI that runs no executor"},
	{site{fnNoExecutor, EngineRefuses, v1alpha1.ReasonNoExecutorCLI},
		"a seat's own chain with no CLI that runs an executor"},
	{site{fnSandboxRequired, EngineRefuses, v1alpha1.ReasonSandboxRequired},
		"agent seats and a model, and no box for any executor to run in"},
	{site{fnUnoffered, EngineRefuses, v1alpha1.ReasonSandboxPlacementNotOffered},
		"a launch or deep work placed in an E2B sandbox the company does not configure"},
}

// reportedElsewhere are the reasons a statement about an object's spec
// reports beside the render (ExecutorOf), always holding nothing: no place
// may record one as a finding.
var reportedElsewhere = []string{
	v1alpha1.ReasonSandboxLoginNotPortable, v1alpha1.ReasonNotExposed, v1alpha1.ReasonBridgeUnreachable,
}

// findingSites reads every place the package's source records a finding: a
// call of refErrors' add or notFound, whose basis must be one of the four
// constants and whose reason one of v1alpha1's, so that the table above can
// be held to them.
func findingSites(t *testing.T) []site {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []site
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || (fn.Recv != nil && receiverIs(fn, "refErrors")) {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != methodAdd && sel.Sel.Name != methodNotFound) {
					return true
				}
				// diff's add takes a path: refErrors' take a basis first.
				if want := map[string]int{methodAdd: 6, methodNotFound: 5}[sel.Sel.Name]; len(call.Args) != want {
					return true
				}
				at := fset.Position(call.Pos())
				basis, ok := call.Args[0].(*ast.Ident)
				if !ok || !slices.Contains([]string{"EngineRefuses", "HarmsBeyond", "ClassPolicy", "Reported"}, basis.Name) {
					t.Errorf("%s: a finding whose basis is not one of the constants: %s", at, source(call.Args[0]))
					return true
				}
				s := site{fn: fn.Name.Name, basis: basisNamed(basis.Name), reason: v1alpha1.ReasonRefNotFound}
				if sel.Sel.Name == methodAdd {
					reason, ok := call.Args[4].(*ast.SelectorExpr)
					if !ok || source(reason.X) != apiPackage {
						t.Errorf("%s: a finding whose reason is not a v1alpha1 constant: %s", at, source(call.Args[4]))
						return true
					}
					s.reason = reasonValue(t, reason.Sel.Name)
				}
				out = append(out, s)
				return true
			})
		}
	}
	return out
}

// receiverIs reports whether fn is a method of the named type.
func receiverIs(fn *ast.FuncDecl, name string) bool {
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// source is an expression's source, for a message.
func source(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return source(e.X) + "." + e.Sel.Name
	}
	return fmt.Sprintf("%T", e)
}

func basisNamed(name string) Basis {
	for _, b := range []Basis{EngineRefuses, HarmsBeyond, ClassPolicy, Reported} {
		if b.String() == name {
			return b
		}
	}
	return 0
}

// reasonValue is the value of the v1alpha1 constant named, which the API's
// source declares.
func reasonValue(t *testing.T, name string) string {
	t.Helper()
	values := reasonConstants(t)
	value, ok := values[name]
	if !ok {
		t.Fatalf("v1alpha1 declares no reason %s", name)
	}
	return value
}

// reasonConstants is every Reason… constant of the API's common_types.go, by
// its name.
func reasonConstants(t *testing.T) map[string]string {
	t.Helper()
	file := filepath.Join("..", "..", "api", "v1alpha1", "common_types.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 || !strings.HasPrefix(spec.Names[0].Name, "Reason") {
			return true
		}
		if lit, ok := spec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			out[spec.Names[0].Name] = strings.Trim(lit.Value, `"`)
		}
		return true
	})
	return out
}

// TestEveryFindingHasABasis holds every place the render records a finding
// to the table of them, in both directions: a new place is classified there,
// with why, before it holds or reports anything; and a place whose basis
// changes changes the table, in the same review. A reason a statement about
// a spec reports beside the render is never a finding.
func TestEveryFindingHasABasis(t *testing.T) {
	render := func(sites []site) []string {
		out := make([]string, 0, len(sites))
		for _, s := range sites {
			out = append(out, s.String())
			if slices.Contains(reportedElsewhere, s.reason) {
				t.Errorf("%s: %s is reported beside the render, never recorded as a finding", s, s.reason)
			}
		}
		slices.Sort(out)
		return out
	}
	want := make([]site, 0, len(findings))
	for _, f := range findings {
		want = append(want, f.site)
	}
	got, table := render(findingSites(t)), render(want)
	if !slices.Equal(got, table) {
		t.Errorf("the render records findings at\n%s\nand the table classifies\n%s",
			strings.Join(got, "\n"), strings.Join(table, "\n"))
	}
}

// engineCase is a finding the render holds the document for on the basis
// EngineRefuses, and the document the operator would write without it: the
// object's spec, whole, with the reference as it is.
type engineCase struct {
	name string
	site site
	// input is the render case broken, mutate breaks it, and refused is the
	// refusal the render records then, as "Kind/name field".
	input   string
	mutate  func(*Input)
	refused string
	// write puts the same reference into the document the unbroken input
	// renders, as the operator would write it; the engine must refuse that
	// document at a path ending in at.
	write func(t *testing.T, doc map[string]any)
	at    string
	// says, when set, is in the message of the engine's problem there.
	says string
	// noPlace, set instead of write, says the document has no place to
	// write the object, which the engine's schema of the document shows.
	noPlace bool
}

// The keys of a providers.llm entry and of providers.sandbox, and the cell,
// that the engine cases on a run's placement write.
const (
	keyRunIn    = "run_in"
	keyStateDir = "state_dir"
	cellE2B     = "e2b"
)

// engineCases are the findings of the basis EngineRefuses, each held to the
// engine.
func engineCases() []engineCase {
	const (
		missing = "missing"
		janeTwo = "Jane Two"
		// sharedEmail is the CTO's address (cto@nimbus.example) as
		// another seat might spell it.
		sharedEmail = "CTO+ops@nimbus.example"
	)
	role := func(t *testing.T, doc map[string]any, handle string) map[string]any {
		t.Helper()
		found := roleIn(doc, handle)
		if found == nil {
			t.Fatalf("the document has no role %s", handle)
		}
		return found
	}
	// another is a copy of jane's role, the top-level person, with edit
	// applied, appended to the top-level roles.
	another := func(edit func(map[string]any)) func(*testing.T, map[string]any) {
		return func(t *testing.T, doc map[string]any) {
			t.Helper()
			copied := map[string]any{}
			maps.Copy(copied, role(t, doc, seatJane))
			delete(copied, "contact")
			delete(copied, "manages")
			edit(copied)
			doc["roles"] = append(doc["roles"].([]any), copied)
		}
	}
	// twin is a copy of Seat jane named jane-2 with edit applied and no
	// contact account of jane's.
	twin := func(edit func(*v1alpha1.Seat)) func(*Input) {
		return func(in *Input) {
			s := seat(in, seatJane).DeepCopy()
			s.Name = seatJane + "-2"
			s.Spec.Contact = v1alpha1.SeatContact{}
			s.Spec.Manages = nil
			s.Spec.DisplayName = janeTwo
			edit(s)
			in.Seats = append(in.Seats, *s)
		}
	}
	// Every account of a contact, claimed by jane and her twin.
	accounts := make([]engineCase, 0, len(contactAccounts))
	for _, a := range contactAccounts {
		const account = "jane-founder"
		accounts = append(accounts, engineCase{
			name: "two seats take one account: " + a.field, site: site{fnConflict, EngineRefuses, v1alpha1.ReasonConflict},
			input: sampleCompany,
			mutate: func(in *Input) {
				setContact(&seat(in, seatJane).Spec.Contact, a.field, account)
				twin(func(s *v1alpha1.Seat) { setContact(&s.Spec.Contact, a.field, account) })(in)
			},
			refused: "Seat/jane-2 spec.contact." + a.field,
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				contact, _ := role(t, doc, seatJane)["contact"].(map[string]any)
				contact[a.key] = account
				another(func(r map[string]any) {
					r["handle"], r[keyName] = seatJane+"-2", janeTwo
					r["contact"] = map[string]any{a.key: account}
				})(t, doc)
			},
			at: keyName, says: a.key,
		})
	}
	// optionalKeysAt is where the engine refuses the first optional key.
	const optionalKeysAt = "optional_keys[0]"
	// optionalKey writes key as the optional_keys of the tool server named.
	optionalKey := func(name, key string) func(*testing.T, map[string]any) {
		return func(t *testing.T, doc map[string]any) {
			t.Helper()
			for _, server := range doc[keyMCPServers].([]any) {
				if entry := server.(map[string]any); entry[keyName] == name {
					entry["optional_keys"] = []any{key}
					return
				}
			}
			t.Fatalf("the document has no tool server %s", name)
		}
	}
	// directWithoutE2B places the document's runs Direct by default, on the
	// sandboxes volume, with no e2b backend, as the render writes a Company
	// whose spec.sandbox says so, and is its providers.llm.
	directWithoutE2B := func(t *testing.T, doc map[string]any) map[string]any {
		t.Helper()
		providers, _ := doc[keyProviders].(map[string]any)
		sandbox, _ := providers["sandbox"].(map[string]any)
		delete(sandbox, cellE2B)
		sandbox["default_run_in"] = "direct"
		sandbox["local"] = map[string]any{keyStateDir: SandboxDir}
		llm, ok := providers["llm"].(map[string]any)
		if !ok {
			t.Fatal("the document has no providers.llm")
		}
		return llm
	}
	// codexEntry adds a Codex provider beside the sample's anthropic, on the
	// same key, as the render writes codexProvider.
	codexEntry := func(t *testing.T, doc map[string]any) {
		t.Helper()
		llm, _ := doc[keyProviders].(map[string]any)["llm"].(map[string]any)
		anthropic, ok := llm["anthropic"].(map[string]any)
		if !ok {
			t.Fatal("the document has no provider anthropic")
		}
		entry := maps.Clone(anthropic)
		entry["agent"], entry["model"] = "codex", modelCodex
		llm[providerCodex] = entry
	}
	return append([]engineCase{
		{
			name: "a seat's own executor chain with no CLI that runs it", site: site{fnNoExecutor, EngineRefuses, v1alpha1.ReasonNoExecutorCLI},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, codexProvider())
				seat(in, seatCTO).Spec.LLM.Default = chainOfNames(providerCodex)
			},
			refused: "Seat/cto spec.llm.default",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				codexEntry(t, doc)
				role(t, doc, seatCTO)[keyLLM].(map[string]any)["default"] = []any{providerCodex}
			},
			at: keyLLM, says: "no member of it can run the executor",
		},
		{
			// Crewlet refuses it once, at the entry every such seat falls
			// to; the render on each seat, whose spec.llm is what changes.
			name: "a seat with no chain on a head of the order that runs no executor", site: site{fnNoExecutor, EngineRefuses, v1alpha1.ReasonNoExecutorCLI},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.LLMProviders = append(in.LLMProviders, codexProvider())
				seat(in, seatCTO).Spec.LLM = v1alpha1.SeatLLM{}
				in.Company.Spec.LLM.Order = append(chainOfNames(providerCodex), in.Company.Spec.LLM.Order...)
			},
			refused: "Seat/cto spec.llm",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				codexEntry(t, doc)
				delete(role(t, doc, seatCTO), keyLLM)
				providers, _ := doc[keyProviders].(map[string]any)
				order, _ := providers["llm_order"].([]any)
				providers["llm_order"] = append([]any{providerCodex}, order...)
			},
			at: providerCodex + ".agent", says: "cannot run an executor",
		},
		{
			name: "agent seats and a model and no spec.sandbox", site: site{fnSandboxRequired, EngineRefuses, v1alpha1.ReasonSandboxRequired},
			input:   sampleCompany,
			mutate:  func(in *Input) { in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{} },
			refused: "Company/nimbus spec.sandbox",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				providers, _ := doc[keyProviders].(map[string]any)
				delete(providers, "sandbox")
			},
			at: "providers.sandbox", says: "configures none",
		},
		{
			name:  "a provider placed in an E2B sandbox the company does not configure",
			site:  site{fnUnoffered, EngineRefuses, v1alpha1.ReasonSandboxPlacementNotOffered},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementDirect
				in.Company.Spec.Sandbox.E2B = v1alpha1.E2BSandbox{}
				ownProvider(in, "anthropic").Spec.RunIn = v1alpha1.SandboxPlacementE2B
			},
			refused: "LLMProvider/anthropic spec.runIn",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				directWithoutE2B(t, doc)["anthropic"].(map[string]any)[keyRunIn] = cellE2B
			},
			at: "anthropic.run_in", says: "needs the `e2b:` backend",
		},
		{
			// A fallback a launch moves to once the head sits out its plan
			// is placed as the head is: the CTO's chain runs Direct on
			// anthropic first, and on an OpenCode in E2B after it.
			name:  "a fallback provider placed in an E2B sandbox the company does not configure",
			site:  site{fnUnoffered, EngineRefuses, v1alpha1.ReasonSandboxPlacementNotOffered},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.Company.Spec.Sandbox.DefaultRunIn = v1alpha1.SandboxPlacementDirect
				in.Company.Spec.Sandbox.E2B = v1alpha1.E2BSandbox{}
				in.LLMProviders = append(in.LLMProviders, openCodeProvider())
				seat(in, seatCTO).Spec.LLM.Default = chainOfNames("anthropic", providerOpenCode)
			},
			refused: "LLMProvider/" + providerOpenCode + " spec.runIn",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				directWithoutE2B(t, doc)[providerOpenCode] = map[string]any{
					"agent": "opencode", "model": modelOpenCode, keyRunIn: cellE2B,
					"auth":      map[string]any{"mode": "subscription"},
					keyEnv:      map[string]any{envGatewayKey: "${OPENROUTER_API_KEY}"},
					keyStateDir: "/var/lib/crewlet/llm-cli/" + providerOpenCode,
				}
				role(t, doc, seatCTO)[keyLLM] = map[string]any{"default": []any{"anthropic", providerOpenCode}}
			},
			at: providerOpenCode + ".run_in", says: "needs the `e2b:` backend",
		},
		{
			name: "an optional key nothing writes", site: site{fnMcpOptionalKeys, EngineRefuses, v1alpha1.ReasonOptionalKeyUnused},
			input:   sampleCompany,
			mutate:  func(in *Input) { toolServer(in, sampleGitHub).Spec.OptionalKeys = []string{"X-Api-Key"} },
			refused: "MCPServer/github spec.optionalKeys[0]",
			write:   optionalKey(sampleGitHub, "X-Api-Key"),
			at:      optionalKeysAt, says: "is not a key of",
		},
		{
			// A Stdio server's keys are variables, whose names an
			// environment tells apart by case.
			name: "a Stdio server's optional key in another case", site: site{fnMcpOptionalKeys, EngineRefuses, v1alpha1.ReasonOptionalKeyUnused},
			input:   sampleCompany,
			mutate:  func(in *Input) { toolServer(in, sampleGitLab).Spec.OptionalKeys = []string{"gitlab_token"} },
			refused: "MCPServer/gitlab spec.optionalKeys[0]",
			write:   optionalKey(sampleGitLab, "gitlab_token"),
			at:      optionalKeysAt, says: "is not a key of",
		},
		{
			// Ana's own Authorization, under a header her unit writes as
			// authorization.
			name: "a seat's header spelled unlike its unit's", site: site{fnHeaderSpellings, EngineRefuses, v1alpha1.ReasonConflict},
			input: sampleCompany,
			mutate: func(in *Input) {
				unit(in, unitPlatform).Spec.MCP = []v1alpha1.MCPCredentials{{Name: sampleGitHub, Env: []v1alpha1.MCPCredentialValue{{
					Name: headerLower, ValueFrom: v1alpha1.SeatCredentialSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: "platform-github", Key: headerLower}},
				}}}}
			},
			refused: "Seat/ana spec.mcp[0].env[0].name",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				platform := unitHolding(t, doc, seatAna)
				platform["mcp_env"] = map[string]any{sampleGitHub: map[string]any{headerLower: "${PLATFORM_GITHUB}"}}
			},
			at: headerAuthorization, says: "names the header its unit's block writes as",
		},
		{
			name: "an optional key only literals are written under", site: site{fnMcpOptionalKeys, EngineRefuses, v1alpha1.ReasonOptionalKeyUnused},
			input: sampleCompany,
			mutate: func(in *Input) {
				toolServer(in, sampleGitHub).Spec.OptionalKeys = []string{sampleToolsets}
				seat(in, seatAna).Spec.MCP[0].Env = append(seat(in, seatAna).Spec.MCP[0].Env,
					v1alpha1.MCPCredentialValue{Name: sampleToolsets, Value: new("repos")})
			},
			refused: "MCPServer/github spec.optionalKeys[0]",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				optionalKey(sampleGitHub, sampleToolsets)(t, doc)
				env := role(t, doc, seatAna)["mcp_env"].(map[string]any)[sampleGitHub].(map[string]any)
				env[sampleToolsets] = "repos"
			},
			at: optionalKeysAt, says: "carries a ${VAR} reference",
		},
		{
			name: "a seat's chain names no provider", site: site{fnProvider, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input:   sampleCompany,
			mutate:  func(in *Input) { seat(in, seatCTO).Spec.LLM.Default = []v1alpha1.LLMProviderReference{{Name: missing}} },
			refused: "Seat/cto spec.llm.default[0].name",
			write: func(t *testing.T, doc map[string]any) {
				role(t, doc, seatCTO)["llm"] = map[string]any{"default": []any{missing}}
			},
			at: keyLLM,
		},
		{
			// The assistant's chain is refused as a seat's is, where
			// llm_order would only drop the key.
			name: "the assistant's chain names no provider", site: site{fnProvider, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{
					Enabled: new(true), LLM: []v1alpha1.LLMProviderReference{{Name: missing}},
				}
			},
			refused: "Company/" + sampleCompany + " spec.assistant.llm[0].name",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				doc["assistant"] = map[string]any{"enabled": true, keyLLM: []any{missing}}
			},
			at: "assistant.llm[0]", says: `"missing"`,
		},
		{
			// So is the provider its deep work runs on, which Crewlet
			// refuses whether or not the assistant is on.
			name: "the assistant's deep work names no provider", site: site{fnProvider, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input: sampleCompany,
			mutate: func(in *Input) {
				in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{
					DeepWork: v1alpha1.CompanyAssistantDeepWork{LLM: v1alpha1.LLMProviderReference{Name: missing}},
				}
			},
			refused: "Company/" + sampleCompany + " spec.assistant.deepWork.llm.name",
			write: func(t *testing.T, doc map[string]any) {
				t.Helper()
				doc["assistant"] = map[string]any{"deep_work": map[string]any{keyLLM: missing}}
			},
			at: "assistant.deep_work.llm", says: `"missing"`,
		},
		{
			// The class offers the provider; the Company's own setting
			// leaves it out of the document, as a missing LLMProvider is.
			name:    "a seat's chain names a platform provider the Company does not use",
			site:    site{fnProvider, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input:   sampleCompany,
			mutate:  func(in *Input) { in.Company.Spec.LLM.UsePlatformProviders = nil },
			refused: "Seat/ana spec.llm.auxiliary[0].name",
			// The document of a Company that does not use the class's
			// providers holds none of them; ana's chain names one as it is.
			write: func(t *testing.T, doc map[string]any) {
				providers, _ := doc[keyProviders].(map[string]any)
				llm, _ := providers["llm"].(map[string]any)
				if _, ok := llm[samplePlatformProvider]; !ok {
					t.Fatalf("the document has no provider %s", samplePlatformProvider)
				}
				delete(llm, samplePlatformProvider)
				order, _ := providers["llm_order"].([]any)
				providers["llm_order"] = slices.DeleteFunc(order, func(key any) bool { return key == samplePlatformProvider })
			},
			at: "llm.auxiliary", says: samplePlatformProvider,
		},
		{
			name: "two seats take one handle", site: site{fnConflict, EngineRefuses, v1alpha1.ReasonConflict},
			input:   sampleCompany,
			mutate:  twin(func(s *v1alpha1.Seat) { s.Spec.Handle = seatJane }),
			refused: "Seat/jane-2 spec.handle",
			write:   another(func(r map[string]any) { r[keyName] = janeTwo }),
			at:      "handle",
		},
		{
			// Compared as Crewlet compares it (iam.NormalizeEmail):
			// lower-cased, with the plus tag dropped.
			name: "two seats take one email", site: site{fnConflict, EngineRefuses, v1alpha1.ReasonConflict},
			input:   sampleCompany,
			mutate:  twin(func(s *v1alpha1.Seat) { s.Spec.Email = sharedEmail }),
			refused: "Seat/jane-2 spec.email",
			write: another(func(r map[string]any) {
				r["handle"], r[keyName], r["email"] = seatJane+"-2", janeTwo, sharedEmail
			}),
			at: "email", says: "seats declare the email",
		},
		{
			name: "datadog routes alerts to no seat", site: site{fnDatadogRouteTo, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input:   sampleCompany,
			mutate:  func(in *Input) { routeDatadog(in, missing) },
			refused: "Integration/datadog spec.datadog.routeTo.seatRef.name",
			write:   func(t *testing.T, doc map[string]any) { datadogIn(t, doc)["route_to"] = missing },
			at:      "route_to",
		},
		{
			name: "datadog routes alerts to a person", site: site{fnDatadogRouteTo, EngineRefuses, v1alpha1.ReasonHumanSeat},
			input:   sampleCompany,
			mutate:  func(in *Input) { routeDatadog(in, seatJane) },
			refused: "Integration/datadog spec.datadog.routeTo.seatRef.name",
			write:   func(t *testing.T, doc map[string]any) { datadogIn(t, doc)["route_to"] = seatJane },
			at:      "route_to",
		},
		{
			name: "a schedule of a person", site: site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonHumanSeat},
			input:   sampleCompany,
			mutate:  func(in *Input) { schedule(in, scheduleCIHealth).Spec.Target.SeatRef.Name = seatJane },
			refused: "Schedule/ci-health spec.target.seatRef.name",
			write: func(t *testing.T, doc map[string]any) {
				owner := role(t, doc, seatAna)
				entry := owner["schedules"].([]any)[0]
				role(t, doc, seatJane)["schedules"] = []any{entry}
			},
			at: "schedules",
		},
		{
			name: "a schedule of a seat that does not exist", site: site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input:   sampleCompany,
			mutate:  func(in *Input) { schedule(in, scheduleCIHealth).Spec.Target.SeatRef.Name = missing },
			refused: "Schedule/ci-health spec.target.seatRef.name",
			noPlace: true,
		},
		{
			name: "a schedule of a unit that does not exist", site: site{fnScheduleTargets, EngineRefuses, v1alpha1.ReasonRefNotFound},
			input:   sampleCompany,
			mutate:  func(in *Input) { schedule(in, schedulePlatformStandup).Spec.Target.UnitRef.Name = missing },
			refused: "Schedule/platform-standup spec.target.unitRef.name",
			noPlace: true,
		},
	}, accounts...)
}

// TestTheDocsStateTheBases holds the docs' account of what holds a company's
// document, docs/concepts/company-document.md "What blocks a document", to
// the table above, which ADR-0005 makes the authority: the reasons its table
// holds on each basis are those the render holds on it, in both directions,
// and every reason that holds nothing is in the list of what the operator
// applies and reports instead. A basis changed in the code and not in the
// docs would tell a reader an object holds the company when it does not, or
// the other way round.
func TestTheDocsStateTheBases(t *testing.T) {
	const page = "docs/concepts/company-document.md"
	path := filepath.Join("..", "..", page)
	rows, err := docs.Table(path, "On")
	if err != nil {
		t.Fatal(err)
	}
	columns := map[string]Basis{"Engine": EngineRefuses, "Harm": HarmsBeyond, "Class": ClassPolicy}
	var documented []string
	for _, row := range rows {
		basis, ok := columns[row[3]]
		if !ok {
			t.Errorf("%s: the row %q holds for %q, which is none of %v", page, row[1], row[3], slices.Sorted(maps.Keys(columns)))
			continue
		}
		// The reasons are the cell's code spans, less what a parenthesis
		// says of the condition they are on.
		reasons, _, _ := strings.Cut(row[2], " (")
		for _, reason := range codeSpans(reasons) {
			documented = append(documented, reason+" "+basis.String())
		}
	}
	var held, reported []string
	for _, f := range findings {
		if f.basis == Reported {
			reported = append(reported, f.reason)
			continue
		}
		held = append(held, f.reason+" "+f.basis.String())
	}
	slices.Sort(documented)
	slices.Sort(held)
	documented, held = slices.Compact(documented), slices.Compact(held)
	if !slices.Equal(documented, held) {
		t.Errorf("%s holds the document for\n%s\nand the render holds it for\n%s",
			page, strings.Join(documented, "\n"), strings.Join(held, "\n"))
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	const from, to = "What the operator applies and reports instead", "Each is applied"
	_, list, _ := strings.Cut(string(data), from)
	list, _, found := strings.Cut(list, to)
	if !found {
		t.Fatalf("%s has no list from %q to %q", page, from, to)
	}
	listed := codeSpans(list)
	for _, reason := range slices.Concat(reported, reportedElsewhere) {
		if !slices.Contains(listed, reason) {
			t.Errorf("%s does not list %s among what the operator applies and reports instead", page, reason)
		}
	}
}

// codeSpans are the Markdown code spans of text, without their backticks.
func codeSpans(text string) []string {
	var out []string
	for {
		_, rest, ok := strings.Cut(text, "`")
		if !ok {
			return out
		}
		span, after, ok := strings.Cut(rest, "`")
		if !ok {
			return out
		}
		out = append(out, span)
		text = after
	}
}

// TestContractEngineRefusesWhatItHolds holds every finding of the basis
// EngineRefuses to the engine: the render refuses the broken input on
// that basis, and the engine refuses the document the operator would write
// without the refusal, the object's spec whole, at the same place; or the
// engine's schema of the document has no place for the object at all. An
// engine that starts accepting one fails here, rather than leaving the
// operator holding a company the engine would run. Every place of the basis
// must have a case.
func TestContractEngineRefusesWhatItHolds(t *testing.T) {
	client := enginetest.Start(t).Client()
	covered := map[site]bool{}
	for _, c := range engineCases() {
		covered[c.site] = true
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, c.input).input(t)
			withDatadog(in)
			base, err := Company(in)
			if err != nil {
				t.Fatalf("the unbroken input is refused: %v", err)
			}
			c.mutate(in)
			_, err = Company(in)
			refusal := slices.IndexFunc(RefErrors(err), func(r *RefError) bool {
				return r.Kind+"/"+r.Name+" "+r.Field == c.refused
			})
			if refusal < 0 {
				t.Fatalf("the render does not refuse %s: %v", c.refused, err)
			}
			if r := RefErrors(err)[refusal]; r.Basis != c.site.basis || r.Reason != c.site.reason {
				t.Fatalf("the render refuses %s on %v with %s, want %v with %s", c.refused, r.Basis, r.Reason, c.site.basis, c.site.reason)
			}
			if c.noPlace {
				scheduleHasNoPlace(t)
				return
			}
			var doc map[string]any
			if err := yaml.Unmarshal(base.YAML, &doc); err != nil {
				t.Fatal(err)
			}
			c.write(t, doc)
			written, err := yaml.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: written, DryRun: true})
			verr, ok := errors.AsType[*engine.ValidationError](err)
			if !ok {
				t.Fatalf("the engine's dry run answered %v: it takes the document the render refuses", err)
			}
			if !slices.ContainsFunc(verr.Problems, func(p engine.Problem) bool {
				return strings.HasSuffix(p.Path, c.at) && strings.Contains(p.Message, c.says)
			}) {
				t.Errorf("the engine refuses the document elsewhere than at …%s: %v", c.at, verr)
			}
		})
	}
	for _, f := range findings {
		if f.basis == EngineRefuses && !covered[f.site] {
			t.Errorf("%s holds on the engine's word, and no case holds it to the engine", f.site)
		}
	}
}

// TestContractReportedIsApplied holds every finding of the basis Reported to
// the engine the other way: the engine takes the document the render
// writes with it, so holding the company for it would be the operator's own
// strictness over one object. An engine that starts refusing one fails here.
// Every place of the basis must have a case.
func TestContractReportedIsApplied(t *testing.T) {
	client := enginetest.Start(t).Client()
	covered := map[string]bool{}
	for _, c := range reportedCases() {
		covered[c.fn+" "+c.want[0].reason] = true
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, c.input).input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatalf("the document is refused: %v", err)
			}
			if _, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true}); err != nil {
				t.Errorf("the engine refuses the document the render writes: %v", err)
			}
		})
	}
	for _, f := range findings {
		if f.basis == Reported && !covered[f.fn+" "+f.reason] {
			t.Errorf("%s is reported, and no case holds it to the engine", f.site)
		}
	}
}

// scheduleHasNoPlace checks the engine's schema of the company
// document: a schedule is written inside the entry of the role or unit that
// runs it, and nowhere else, and names neither, so a schedule of a seat or a
// unit that does not exist cannot be written.
func scheduleHasNoPlace(t *testing.T) {
	t.Helper()
	//nolint:gosec // G204: the binary is the engine under test, chosen by whoever runs the tests.
	out, err := exec.CommandContext(t.Context(), enginetest.Binary(t), "schema", "company").Output()
	if err != nil {
		t.Fatalf("crewlet schema company: %v", err)
	}
	var schema struct {
		Properties map[string]any `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &schema); err != nil {
		t.Fatal(err)
	}
	var holders []string
	for name, def := range schema.Defs {
		if _, ok := def.Properties["schedules"]; ok {
			holders = append(holders, name)
		}
	}
	if _, ok := schema.Properties["schedules"]; ok {
		holders = append(holders, "the document")
	}
	slices.Sort(holders)
	if !slices.Equal(holders, []string{"Role", "Unit"}) {
		t.Errorf("schedules are written in %v, not only in a role's or a unit's entry", holders)
	}
	for field := range schema.Defs["Schedule"].Properties {
		if slices.Contains([]string{"role", "unit", "seat", "handle", "owner"}, field) {
			t.Errorf("a schedule names its %s: one of a seat or unit that does not exist could be written", field)
		}
	}
}

// withDatadog gives the sample company Datadog, routing alerts to the CTO.
func withDatadog(in *Input) {
	if !slices.ContainsFunc(in.Integrations, func(i v1alpha1.Integration) bool { return i.Name == VendorDatadog }) {
		in.Integrations = append(in.Integrations, datadogIntegration(seatCTO))
	}
}

// routeDatadog routes the sample company's Datadog alerts to the Seat named.
func routeDatadog(in *Input, name string) {
	withDatadog(in)
	integration(in, VendorDatadog).Spec.Datadog.RouteTo.SeatRef.Name = name
}

// roleIn is the role entry of handle in a decoded document, at the top level
// or in a unit nested to any depth, or nil.
func roleIn(doc map[string]any, handle string) map[string]any {
	roles, _ := doc["roles"].([]any)
	for _, r := range roles {
		if role, ok := r.(map[string]any); ok && role["handle"] == handle {
			return role
		}
	}
	for _, key := range []string{keyUnits, keyChildren} {
		units, _ := doc[key].([]any)
		for _, u := range units {
			if unit, ok := u.(map[string]any); ok {
				if found := roleIn(unit, handle); found != nil {
					return found
				}
			}
		}
	}
	return nil
}

// unitHolding is the entry of the unit whose own roles hold handle, at any
// depth of a decoded document.
func unitHolding(t *testing.T, doc map[string]any, handle string) map[string]any {
	t.Helper()
	var find func(map[string]any) map[string]any
	find = func(m map[string]any) map[string]any {
		for _, key := range []string{keyUnits, keyChildren} {
			units, _ := m[key].([]any)
			for _, u := range units {
				unit, ok := u.(map[string]any)
				if !ok {
					continue
				}
				roles, _ := unit["roles"].([]any)
				for _, r := range roles {
					if role, ok := r.(map[string]any); ok && role["handle"] == handle {
						return unit
					}
				}
				if found := find(unit); found != nil {
					return found
				}
			}
		}
		return nil
	}
	unit := find(doc)
	if unit == nil {
		t.Fatalf("no unit of the document holds %s", handle)
	}
	return unit
}

// datadogIn is the datadog block of a decoded document.
func datadogIn(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	integrations, _ := doc[keyIntegrations].(map[string]any)
	block, ok := integrations[VendorDatadog].(map[string]any)
	if !ok {
		t.Fatal("the document has no datadog block")
	}
	return block
}

// TestTheTestEngineNamesTheTokensAsTheRenderDoes: the engine every suite runs
// gives the two tokens the ids the render gives them. The operator recognises
// its own writes by the login the engine records for its token, so a test
// engine that named it otherwise would read every write the operator makes as
// somebody else's, and a suite would test a controller that production never
// runs. enginetest cannot import this package (these tests import it), so the
// ids are copied there and held here.
func TestTheTestEngineNamesTheTokensAsTheRenderDoes(t *testing.T) {
	t.Parallel()
	if enginetest.OperatorTokenID != TokenOperator || enginetest.AccessTokenID != TokenAccess {
		t.Fatalf("enginetest names the tokens %q and %q, the render %q and %q",
			enginetest.OperatorTokenID, enginetest.AccessTokenID, TokenOperator, TokenAccess)
	}
}
