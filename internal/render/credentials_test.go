package render

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	celvalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel"
	"k8s.io/apimachinery/pkg/util/validation/field"
	celconfig "k8s.io/apiserver/pkg/apis/cel"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// secretMark is the annotation Crewlet's schemas put on a credential
// position.
const secretMark = "x-crewlet-secret"

// engineSchema runs the engine's `crewlet schema <tier>` and returns
// the JSON Schema it prints.
func engineSchema(t *testing.T, tier credentialTier) map[string]any {
	t.Helper()
	//nolint:gosec // G204: the binary is the engine under test, chosen by whoever runs the tests.
	cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), "schema", string(tier))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("crewlet schema %s: %v\n%s", tier, err, stderr.String())
	}
	var schema map[string]any
	if err := json.Unmarshal(out, &schema); err != nil {
		t.Fatalf("crewlet schema %s printed no JSON: %v", tier, err)
	}
	return schema
}

// markedLocations returns the location of every node of schema that carries
// secretMark, as segments: a property's key, `[]` for a list's elements and
// `*` for a map's values. Sorted, each once.
//
// A location is where a value sits in the document, so the walk follows what
// places a value (properties, patternProperties, items, prefixItems,
// additionalProperties) and what describes the value already placed there
// (a $ref, the branches of allOf, anyOf and oneOf, and an if's then and
// else). It does not enter `if` or `not`: those are what a value is tested
// against, never a place one sits, so a mark there marks nothing.
func markedLocations(t *testing.T, schema map[string]any) [][]string {
	t.Helper()
	defs, _ := schema["$defs"].(map[string]any)
	marks := map[string][]string{}
	var walk func(node any, at []string, inside map[string]bool)
	walk = func(node any, at []string, inside map[string]bool) {
		n, ok := node.(map[string]any)
		if !ok {
			return
		}
		if mark, ok := n[secretMark]; ok {
			if mark != true {
				t.Errorf("%s: %s is %v; Crewlet documents true as its only value", strings.Join(at, "."), secretMark, mark)
			}
			marks[strings.Join(at, "\x00")] = slices.Clone(at)
		}
		if ref, ok := n["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			def, ok := defs[name]
			if !ok {
				t.Fatalf("%s refers to %s, which the schema does not define", strings.Join(at, "."), ref)
			}
			// A definition met again inside itself is the recursion of the
			// org chart (a unit's children are units); its positions were
			// collected where it was first entered, and orgPosition writes
			// a child's from there.
			if !inside[name] {
				walk(def, at, withName(inside, name))
			}
		}
		for _, combinator := range []string{"allOf", "anyOf", "oneOf"} {
			subs, _ := n[combinator].([]any)
			for _, sub := range subs {
				walk(sub, at, inside)
			}
		}
		for _, branch := range []string{"then", "else"} {
			walk(n[branch], at, inside)
		}
		if properties, ok := n["properties"].(map[string]any); ok {
			for key, sub := range properties {
				walk(sub, append(slices.Clone(at), key), inside)
			}
		}
		if patterns, ok := n["patternProperties"].(map[string]any); ok {
			for _, sub := range patterns {
				walk(sub, append(slices.Clone(at), "*"), inside)
			}
		}
		prefix, _ := n["prefixItems"].([]any)
		for _, sub := range prefix {
			walk(sub, append(slices.Clone(at), "[]"), inside)
		}
		walk(n["items"], append(slices.Clone(at), "[]"), inside)
		walk(n["additionalProperties"], append(slices.Clone(at), "*"), inside)
	}
	walk(schema, nil, map[string]bool{})

	locations := slices.Collect(maps.Values(marks))
	slices.SortFunc(locations, slices.Compare)
	return locations
}

// schemaMarks returns every position the engine's schema of tier
// marks as a credential, written as credentialPosition.Path is.
func schemaMarks(t *testing.T, tier credentialTier) []string {
	t.Helper()
	locations := markedLocations(t, engineSchema(t, tier))
	paths := make([]string, 0, len(locations))
	for _, at := range locations {
		paths = append(paths, positionPath(at))
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

func withName(set map[string]bool, name string) map[string]bool {
	out := make(map[string]bool, len(set)+1)
	for k := range set {
		out[k] = true
	}
	out[name] = true
	return out
}

// orgPosition writes a location in the org chart from the entry it belongs
// to, since a Seat or a Unit fills its positions the same way at any depth:
// from the last `roles[]` on, and otherwise from the unit, written `units[]`,
// however deep among `children[]` it sits.
func orgPosition(at []string) []string {
	for i := len(at) - 2; i >= 0; i-- {
		if at[i] == keyRoles && at[i+1] == "[]" {
			return at[i:]
		}
	}
	if len(at) < 2 || at[0] != keyUnits || at[1] != "[]" {
		return at
	}
	i := 2
	for i+1 < len(at) && at[i] == keyChildren && at[i+1] == "[]" {
		i += 2
	}
	return append([]string{keyUnits, "[]"}, at[i:]...)
}

// positionPath writes a schema location as credentialPosition.Path.
func positionPath(at []string) string {
	var b strings.Builder
	for _, segment := range orgPosition(at) {
		if segment != "[]" && b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(segment)
	}
	return b.String()
}

// TestMarkedLocationsFollowsEveryPlacingConstruct holds the walk to a
// schema that puts a mark under each construct it follows, and one under
// each it must not: the engine's generator emits only some of them today, so
// without this a construct the walk mishandled would pass unseen until
// Crewlet first marked a position under it, and then miss it.
func TestMarkedLocationsFollowsEveryPlacingConstruct(t *testing.T) {
	mark := map[string]any{secretMark: true}
	object := func(properties map[string]any) map[string]any { return map[string]any{"properties": properties} }
	unit := map[string]any{"items": map[string]any{"$ref": "#/$defs/Unit"}}
	const branches = "branches"
	schema := object(map[string]any{
		"plain": mark,
		"all":   map[string]any{"allOf": []any{map[string]any{}, mark}},
		"any":   map[string]any{"anyOf": []any{mark}},
		"one":   map[string]any{"oneOf": []any{mark}},
		branches: map[string]any{
			"if":   object(map[string]any{"tested": mark}),
			"then": mark,
			"else": object(map[string]any{"other": mark}),
		},
		"negated":  map[string]any{"not": mark},
		"list":     map[string]any{"items": mark},
		"tuple":    map[string]any{"prefixItems": []any{map[string]any{}, mark}},
		"map":      map[string]any{"additionalProperties": mark},
		"patterns": map[string]any{"patternProperties": map[string]any{"^x-": mark}},
		keyUnits:   unit,
	})
	schema["$defs"] = map[string]any{"Unit": object(map[string]any{"token": mark, keyChildren: unit})}
	want := [][]string{
		{"all"},
		{"any"},
		{branches},
		{branches, "other"},
		{"list", "[]"},
		{"map", "*"},
		{"one"},
		{"patterns", "*"},
		{"plain"},
		{"tuple", "[]"},
		{keyUnits, "[]", "token"},
	}
	if got := markedLocations(t, schema); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("the walk found the marks at\n%v\nwant\n%v", got, want)
	}
}

// TestOrgPositionsAreWrittenFromTheirEntry: a role's and a unit's positions
// are written the same at every depth of the org chart, so one row answers
// each wherever the chart puts it.
func TestOrgPositionsAreWrittenFromTheirEntry(t *testing.T) {
	// A credential of a role's sandbox, and of a unit's MCP server.
	roleEnv := Path{"sandbox", "env", "TOKEN"}
	unitEnv := Path{"mcp_env", "linear", "TOKEN"}
	const rolePosition, unitPosition = "roles[].sandbox.env.TOKEN", "units[].mcp_env.linear.TOKEN"
	at := slices.Concat[Path]
	for _, c := range []struct {
		at   Path
		want string
	}{
		{at(Path{keyRoles, 0}, roleEnv), rolePosition},
		{at(Path{keyUnits, 0, keyRoles, 1}, roleEnv), rolePosition},
		{at(Path{keyUnits, 0, keyChildren, 2, keyChildren, 0, keyRoles, 1}, roleEnv), rolePosition},
		{at(Path{keyUnits, 0}, unitEnv), unitPosition},
		{at(Path{keyUnits, 0, keyChildren, 0}, unitEnv), unitPosition},
		{at(Path{keyUnits, 1, keyChildren, 0, keyChildren, 3}, unitEnv), unitPosition},
		{Path{keyProviders, "llm", "a", keyEnv, envGatewayKey}, "providers.llm.a.env." + envGatewayKey},
		// children only nests units: elsewhere it is an ordinary key.
		{Path{keyMCPServers, 0, keyChildren, 0, "env", "X"}, "mcp_servers[].children[].env.X"},
	} {
		if got := positionPath(segments(c.at)); got != c.want {
			t.Errorf("%s is written %s, want %s", c.at, got, c.want)
		}
	}
	if _, ok := positionOf(tierCompany, at(Path{keyUnits, 0, keyChildren, 0}, unitEnv)); !ok {
		t.Error("a child unit's MCP credential is not the position units[].mcp_env.*.*")
	}
}

// TestCredentialPositionsAreTheEnginesMarks holds credentialPositions to the
// positions the engine marks as credentials, in both directions: a
// position Crewlet adds must be answered (a field, or why there is none), and
// a row for a position Crewlet no longer marks is a stale claim.
func TestCredentialPositionsAreTheEnginesMarks(t *testing.T) {
	for _, tier := range []credentialTier{tierCompany, tierBootstrap} {
		t.Run(string(tier), func(t *testing.T) {
			marked := schemaMarks(t, tier)
			if len(marked) == 0 {
				t.Fatalf("crewlet schema %s marks no position, so this test would prove nothing", tier)
			}
			var listed []string
			for _, p := range credentialPositions {
				if p.Tier == tier {
					listed = append(listed, p.Path)
				}
			}
			for _, p := range marked {
				if !slices.Contains(listed, p) {
					t.Errorf("Crewlet marks %s as a credential, and credentialPositions does not list it: "+
						"name the field that fills it, or why v1alpha1 offers none", p)
				}
			}
			for _, p := range listed {
				if !slices.Contains(marked, p) {
					t.Errorf("credentialPositions lists %s, which Crewlet does not mark as a credential: remove the row", p)
				}
			}
		})
	}
}

// engineConfigRefusal is the start of the message of the CEL rule on
// Company.spec.engineConfig that keeps it off the blocks that hold
// credentials.
const engineConfigRefusal = "engineConfig may not set"

// engineConfigValidator runs the CEL rules of Company.spec.engineConfig as
// the generated CRD states them, with the validator the API server runs.
func engineConfigValidator(t *testing.T) func(entries ...any) field.ErrorList {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_companies.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	var node *apiextensionsv1.JSONSchemaProps
	for _, v := range crd.Spec.Versions {
		if v.Name == "v1alpha1" {
			spec := v.Schema.OpenAPIV3Schema.Properties["spec"]
			engineConfig, ok := spec.Properties["engineConfig"]
			if ok {
				node = &engineConfig
			}
		}
	}
	if node == nil {
		t.Fatal("the Company CRD has no v1alpha1 spec.engineConfig")
	}
	var internal apiextensions.JSONSchemaProps
	if err := apiextensionsv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(node, &internal, nil); err != nil {
		t.Fatal(err)
	}
	structural, err := structuralschema.NewStructural(&internal)
	if err != nil {
		t.Fatalf("spec.engineConfig is not a structural schema: %v", err)
	}
	validator := celvalidation.NewValidator(structural, false, celconfig.PerCallLimit)
	if validator == nil {
		t.Fatal("spec.engineConfig carries no CEL rule")
	}
	return func(entries ...any) field.ErrorList {
		errs, _ := validator.Validate(t.Context(), field.NewPath("spec", "engineConfig"), structural, entries, nil,
			celconfig.RuntimeCELCostBudget)
		return errs
	}
}

// TestEngineConfigCannotNameACredentialBlock: engineConfig passes a
// top-level block of the company document through as written, so it may
// never name a block Crewlet holds a credential under, or a literal could
// reach the document past every reference this API makes. The CRD's rule
// keeps a list of the blocks it refuses; this holds that list to the
// top-level key of every position the engine's schema marks, so a
// credential Crewlet adds under another block fails here until the rule
// refuses that block too.
func TestEngineConfigCannotNameACredentialBlock(t *testing.T) {
	validate := engineConfigValidator(t)
	var blocks []string
	for _, at := range markedLocations(t, engineSchema(t, tierCompany)) {
		blocks = append(blocks, at[0])
	}
	slices.Sort(blocks)
	blocks = slices.Compact(blocks)
	if len(blocks) == 0 {
		t.Fatal("crewlet schema company marks no position, so this test would prove nothing")
	}
	entry := func(block string) map[string]any { return map[string]any{keyName: block, "value": map[string]any{}} }
	for _, block := range blocks {
		errs := validate(entry(block))
		if !slices.ContainsFunc(errs, func(e *field.Error) bool { return strings.HasPrefix(e.Detail, engineConfigRefusal) }) {
			t.Errorf("Crewlet holds a credential under %s, and the CRD lets engineConfig set it (%v): "+
				"add it to the rule on Company.spec.engineConfig", block, errs)
		}
	}
	// A block that holds none is let through, so the refusals above are
	// the rule's and not a validator that refuses everything.
	if errs := validate(entry("turn_engine")); len(errs) > 0 {
		t.Errorf("engineConfig may not set turn_engine, which holds no credential: %v", errs)
	}
}

// TestCredentialPositionsAreEachAnsweredOnce: every row either names what
// fills it or says why nothing does, never both, and names its position once;
// of the sources of one position, all but one say when they apply.
func TestCredentialPositionsAreEachAnsweredOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range credentialPositions {
		key := string(p.Tier) + " " + p.Path
		if seen[key] {
			t.Errorf("%s is listed twice", key)
		}
		seen[key] = true
		if (len(p.Sources) == 0) == (p.Unsupported == "") {
			t.Errorf("%s: a position has sources or a reason it is not offered, exactly one of the two", key)
		}
		unqualified := 0
		for _, s := range p.Sources {
			if s.Reference == "" {
				t.Errorf("%s: source %+v says not which Secret holds the value", key, s)
			}
			if s.When == "" {
				unqualified++
			}
		}
		if unqualified > 1 {
			t.Errorf("%s: %d sources say not when they apply; every source but one must", key, unqualified)
		}
	}
}

// TestCredentialFieldsAreTheAPIs holds every field credentialPositions names
// to the generated CRDs, so a renamed or removed field fails here rather than
// leaving the table, and the page made from it, pointing at nothing.
func TestCredentialFieldsAreTheAPIs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the CRDs in config/crd/bases: %v", err)
	}
	schemas := map[string]*apiextensionsv1.JSONSchemaProps{}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(data, &crd); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, v := range crd.Spec.Versions {
			if v.Name == "v1alpha1" {
				schemas[crd.Spec.Names.Kind] = v.Schema.OpenAPIV3Schema
			}
		}
	}

	for _, p := range credentialPositions {
		for _, s := range p.Sources {
			if s.Field == "" {
				continue
			}
			kind, path, _ := strings.Cut(s.Field, ".")
			node, ok := schemas[kind]
			if !ok {
				t.Errorf("%s: %s names kind %s, which no CRD defines", p.Path, s.Field, kind)
				continue
			}
			for segment := range strings.SplitSeq(path, ".") {
				name, list := strings.CutSuffix(segment, "[]")
				child, ok := node.Properties[name]
				if !ok {
					node = nil
					break
				}
				node = &child
				if list {
					if node.Items == nil || node.Items.Schema == nil {
						node = nil
						break
					}
					node = node.Items.Schema
				}
			}
			if node == nil {
				t.Errorf("%s: the API has no field %s", p.Path, s.Field)
			}
		}
	}
}

// credentialsDoc is the page that lists the positions, for people.
var credentialsDoc = filepath.Join("..", "..", "docs", "concepts", "credentials.md")

// The page's sections, each holding one table made from credentialPositions.
const (
	sectionCovered    = "## Covered in v1alpha1"
	sectionNotOffered = "## Not offered in v1alpha1"
	sectionTierA      = "## In the data pod's `crewlet.yaml`"
)

// credentialTables are the tables the page lists the positions in, by the
// heading of the section each is in.
func credentialTables() map[string]string {
	covered := []string{"| Crewlet position | Field | Reference |", "|---|---|---|"}
	notOffered := []string{"| Crewlet position | Why it is not offered |", "|---|---|"}
	tierA := []string{"| Crewlet position | Filled from |", "|---|---|"}
	var groupReason string
	var group []string
	flush := func() {
		if len(group) > 0 {
			notOffered = append(notOffered, "| "+strings.Join(group, ", ")+" | "+groupReason+" |")
		}
		group = nil
	}
	for _, p := range credentialPositions {
		position := "`" + p.Path + "`"
		switch {
		case p.Tier == tierBootstrap && p.Unsupported != "":
			tierA = append(tierA, "| "+position+" | Not offered. "+p.Unsupported+" |")
		case p.Tier == tierBootstrap:
			for _, s := range p.Sources {
				from := s.Reference
				if s.Field != "" {
					from = "`" + s.Field + "`: " + s.Reference
				}
				tierA = append(tierA, "| "+position+" | "+from+" |")
			}
		case p.Unsupported != "":
			// Positions not offered for one reason share a row, as long as
			// they are listed together.
			if p.Unsupported != groupReason {
				flush()
				groupReason = p.Unsupported
			}
			group = append(group, position)
		default:
			for _, s := range p.Sources {
				cell := position
				if s.When != "" {
					cell += ", " + s.When
				}
				covered = append(covered, "| "+cell+" | `"+s.Field+"` | "+s.Reference+" |")
			}
		}
	}
	flush()
	tables := map[string]string{
		sectionCovered: strings.Join(covered, "\n"),
		sectionTierA:   strings.Join(tierA, "\n"),
	}
	// With every company position covered there is no table of the ones
	// that are not, and the page has no such section.
	if len(notOffered) > 2 {
		tables[sectionNotOffered] = strings.Join(notOffered, "\n")
	}
	return tables
}

// TestCredentialsDocListsThePositions holds docs/concepts/credentials.md to
// credentialPositions: each of its three tables is exactly the rows the table
// makes, so the page a user reads lists what the engine marks and what the
// operator fills it from, and nothing else.
func TestCredentialsDocListsThePositions(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(credentialsDoc))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	tables := credentialTables()
	if _, ok := tables[sectionNotOffered]; !ok && slices.Contains(lines, sectionNotOffered) {
		t.Errorf("%s has a section %q, and credentialPositions leaves no company position uncovered: remove it",
			credentialsDoc, sectionNotOffered)
	}
	for heading, want := range tables {
		start := slices.Index(lines, heading)
		if start < 0 {
			t.Errorf("%s has no section %q; it should hold this table:\n\n%s", credentialsDoc, heading, want)
			continue
		}
		var table []string
		for _, line := range lines[start+1:] {
			if strings.HasPrefix(line, "## ") {
				break
			}
			if strings.HasPrefix(line, "|") {
				table = append(table, line)
			} else if len(table) > 0 {
				break
			}
		}
		if got := strings.Join(table, "\n"); got != want {
			t.Errorf("the table under %q in %s is not the one credentialPositions makes; it should read:\n\n%s\n\nIt reads:\n\n%s",
				heading, credentialsDoc, want, got)
		}
	}
}

// segments writes a place in a rendered document as a schema location:
// `[]` for every index.
func segments(p Path) []string {
	out := make([]string, len(p))
	for i, segment := range p {
		switch s := segment.(type) {
		case int:
			out[i] = "[]"
		case string:
			out[i] = s
		}
	}
	return out
}

// matchesPosition reports whether the place p in a rendered document is the
// position pattern (a credentialPosition.Path).
func matchesPosition(pattern string, p Path) bool {
	var want []string
	for part := range strings.SplitSeq(pattern, ".") {
		name, list := strings.CutSuffix(part, "[]")
		want = append(want, name)
		if list {
			want = append(want, "[]")
		}
	}
	return slices.EqualFunc(want, orgPosition(segments(p)), func(want, got string) bool {
		if want == "*" {
			return got != "[]"
		}
		return want == got
	})
}

// visitLeaves calls visit with every scalar of a document and its place.
func visitLeaves(t *testing.T, document []byte, visit func(Path, any)) {
	t.Helper()
	var tree any
	if err := yaml.Unmarshal(document, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(at Path, v any)
	walk = func(at Path, v any) {
		switch v := v.(type) {
		case map[string]any:
			for key, child := range v {
				walk(append(slices.Clone(at), key), child)
			}
		case []any:
			for i, child := range v {
				walk(append(slices.Clone(at), i), child)
			}
		default:
			visit(at, v)
		}
	}
	walk(nil, tree)
}

// positionOf is the credential position of tier that p is, if it is one.
func positionOf(tier credentialTier, p Path) (credentialPosition, bool) {
	for _, position := range credentialPositions {
		if position.Tier == tier && matchesPosition(position.Path, p) {
			return position, true
		}
	}
	return credentialPosition{}, false
}

// TestBootstrapCredentialsSitOnlyAtMarkedPositions uses the engine's own
// marks on Tier A to check where the operator puts the data pod's
// credentials. Crewlet suggests a ${VAR} at a marked Tier A position; the
// operator writes the values themselves into crewlet.yaml, kept in a Secret,
// because a Tier A reference resolves from the process environment, which
// the engine also expands a company document's ${NAME} from, in fields that
// are not credentials (see Bootstrap). What it holds to instead: every
// position the rendered Tier A fills that Crewlet marks is one the table says
// the operator fills, its value is one of the credentials it was given, and
// every such credential is at a marked position and nowhere else, so none
// reaches a place the engine logs or serves unmasked.
func TestBootstrapCredentialsSitOnlyAtMarkedPositions(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			in := c.input(t)
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			credentials := []string{in.Tokens.Operator, in.Tokens.Access}
			for _, material := range in.Keyring.Keys {
				credentials = append(credentials, material)
			}
			if in.Objects != nil {
				credentials = append(credentials, in.Objects.SecretAccessKey)
			}
			placed := 0
			visitLeaves(t, doc.YAML, func(p Path, v any) {
				value, _ := v.(string)
				position, marked := positionOf(tierBootstrap, p)
				switch {
				case marked && position.Unsupported != "":
					t.Errorf("%s is filled, and credentialPositions says v1alpha1 offers nothing to fill it with", p)
				case marked && !slices.Contains(credentials, value):
					t.Errorf("%s, a credential position, holds something other than a credential the operator was given", p)
				case marked:
					placed++
				case slices.ContainsFunc(credentials, func(c string) bool { return strings.Contains(value, c) }):
					t.Errorf("%s holds a credential, and Crewlet does not mark it as a credential position", p)
				}
			})
			if placed != len(credentials) {
				t.Errorf("the document places %d credentials at marked positions, and was given %d", placed, len(credentials))
			}
		})
	}
}
