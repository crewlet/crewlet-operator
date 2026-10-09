package render

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// secretMark is the annotation Crewlet's schemas put on a credential
// position.
const secretMark = "x-crewlet-secret"

// schemaMarks runs the pinned engine's `crewlet schema <tier>` and returns
// every position it marks as a credential, written as CredentialPosition.Path
// is.
func schemaMarks(t *testing.T, tier CredentialTier) []string {
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
	defs, _ := schema["$defs"].(map[string]any)

	marks := map[string]bool{}
	var walk func(node any, at []string, inside map[string]bool)
	walk = func(node any, at []string, inside map[string]bool) {
		n, ok := node.(map[string]any)
		if !ok {
			return
		}
		if mark, ok := n[secretMark]; ok {
			if mark != true {
				t.Errorf("%s: %s is %v; Crewlet documents true as its only value", positionPath(at), secretMark, mark)
			}
			marks[positionPath(at)] = true
		}
		if ref, ok := n["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			def, ok := defs[name]
			if !ok {
				t.Fatalf("crewlet schema %s: %s refers to %s, which it does not define", tier, positionPath(at), ref)
			}
			// A definition met again inside itself is the recursion of the
			// org chart (a unit's children are units); its positions were
			// collected where it was first entered.
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
		walk(n["items"], append(slices.Clone(at), "[]"), inside)
		walk(n["additionalProperties"], append(slices.Clone(at), "*"), inside)
	}
	walk(schema, nil, map[string]bool{})

	paths := make([]string, 0, len(marks))
	for p := range marks {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths
}

func withName(set map[string]bool, name string) map[string]bool {
	out := make(map[string]bool, len(set)+1)
	for k := range set {
		out[k] = true
	}
	out[name] = true
	return out
}

// positionPath writes a schema location as CredentialPosition.Path: from the
// last `roles[]` on, since a role's positions are the same wherever the org
// chart places it.
func positionPath(at []string) string {
	for i := len(at) - 2; i >= 0; i-- {
		if at[i] == keyRoles && at[i+1] == "[]" {
			at = at[i:]
			break
		}
	}
	var b strings.Builder
	for _, segment := range at {
		if segment != "[]" && b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(segment)
	}
	return b.String()
}

// TestCredentialPositionsAreTheEnginesMarks holds CredentialPositions to the
// positions the pinned engine marks as credentials, in both directions: a
// position Crewlet adds must be answered (a field, or why there is none), and
// a row for a position Crewlet no longer marks is a stale claim.
func TestCredentialPositionsAreTheEnginesMarks(t *testing.T) {
	for _, tier := range []CredentialTier{TierCompany, TierBootstrap} {
		t.Run(string(tier), func(t *testing.T) {
			marked := schemaMarks(t, tier)
			if len(marked) == 0 {
				t.Fatalf("crewlet schema %s marks no position, so this test would prove nothing", tier)
			}
			var listed []string
			for _, p := range CredentialPositions {
				if p.Tier == tier {
					listed = append(listed, p.Path)
				}
			}
			for _, p := range marked {
				if !slices.Contains(listed, p) {
					t.Errorf("Crewlet marks %s as a credential, and CredentialPositions does not list it: "+
						"name the field that fills it, or why v1alpha1 offers none", p)
				}
			}
			for _, p := range listed {
				if !slices.Contains(marked, p) {
					t.Errorf("CredentialPositions lists %s, which Crewlet does not mark as a credential: remove the row", p)
				}
			}
		})
	}
}

// TestCredentialPositionsAreEachAnsweredOnce: every row either names what
// fills it or says why nothing does, never both, and names its position once;
// of the sources of one position, all but one say when they apply.
func TestCredentialPositionsAreEachAnsweredOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range CredentialPositions {
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

// TestCredentialFieldsAreTheAPIs holds every field CredentialPositions names
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

	for _, p := range CredentialPositions {
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

// The page's sections, each holding one table made from CredentialPositions.
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
	for _, p := range CredentialPositions {
		position := "`" + p.Path + "`"
		switch {
		case p.Tier == TierBootstrap && p.Unsupported != "":
			tierA = append(tierA, "| "+position+" | Not offered. "+p.Unsupported+" |")
		case p.Tier == TierBootstrap:
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
	return map[string]string{
		sectionCovered:    strings.Join(covered, "\n"),
		sectionNotOffered: strings.Join(notOffered, "\n"),
		sectionTierA:      strings.Join(tierA, "\n"),
	}
}

// TestCredentialsDocListsThePositions holds docs/concepts/credentials.md to
// CredentialPositions: each of its three tables is exactly the rows the table
// makes, so the page a user reads lists what the engine marks and what the
// operator fills it from, and nothing else.
func TestCredentialsDocListsThePositions(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(credentialsDoc))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for heading, want := range credentialTables() {
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
			t.Errorf("the table under %q in %s is not the one CredentialPositions makes; it should read:\n\n%s\n\nIt reads:\n\n%s",
				heading, credentialsDoc, want, got)
		}
	}
}

// matchesPosition reports whether the place p in a rendered document is the
// position pattern (a CredentialPosition.Path).
func matchesPosition(pattern string, p Path) bool {
	for i := len(p) - 2; i >= 0; i-- {
		if _, index := p[i+1].(int); index && p[i] == keyRoles {
			p = p[i:]
			break
		}
	}
	var want []string
	for part := range strings.SplitSeq(pattern, ".") {
		name, list := strings.CutSuffix(part, "[]")
		want = append(want, name)
		if list {
			want = append(want, "[]")
		}
	}
	if len(want) != len(p) {
		return false
	}
	for i, segment := range p {
		switch s := segment.(type) {
		case int:
			if want[i] != "[]" {
				return false
			}
		case string:
			if want[i] == "[]" || (want[i] != "*" && want[i] != s) {
				return false
			}
		}
	}
	return true
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
func positionOf(tier CredentialTier, p Path) (CredentialPosition, bool) {
	for _, position := range CredentialPositions {
		if position.Tier == tier && matchesPosition(position.Path, p) {
			return position, true
		}
	}
	return CredentialPosition{}, false
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
				position, marked := positionOf(TierBootstrap, p)
				switch {
				case marked && position.Unsupported != "":
					t.Errorf("%s is filled, and CredentialPositions says v1alpha1 offers nothing to fill it with", p)
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
