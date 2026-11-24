package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// TestExecutorHarnessesFollowTheEngineSource holds the harnesses the render
// runs a seat's executor on (ExecutorHarnesses) to the engine's one list of
// them, in its order: the built-in profiles in profilesYAML that carry an
// executor block, in the file's order (harnessprofile.ExecutorRunners), which
// the engine holds its registered runners to and derives
// config.ExecutorHarnesses from, refusing a chain without one of them. A
// harness either gains moves the member every launch on a chain runs on, and
// NoExecutorHarness.
//
// Mutation: drop HarnessCopilot from ExecutorHarnesses, and this fails.
func TestExecutorHarnessesFollowTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	raw, err := source.ReadFile(profilesYAML)
	if err != nil {
		t.Fatal(err)
	}
	var doc yamlv3.Node
	if err := yamlv3.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yamlv3.MappingNode {
		t.Fatalf("%s is not a mapping of profiles", profilesYAML)
	}
	var got []string
	table := doc.Content[0].Content
	for i := 0; i+1 < len(table); i += 2 {
		var profile struct {
			Executor *yamlv3.Node `yaml:"executor"`
		}
		if err := table[i+1].Decode(&profile); err != nil {
			t.Fatalf("%s: %s: %v", profilesYAML, table[i].Value, err)
		}
		if profile.Executor != nil {
			got = append(got, table[i].Value)
		}
	}
	want := make([]string, len(ExecutorHarnesses))
	for i, harness := range ExecutorHarnesses {
		want[i] = harnessName(harness)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the engine runs a seat's executor on %v (the executor blocks of %s), the render on %v",
			got, profilesYAML, want)
	}
}

// profileFile is the engine's file of harness profiles, which declares how it
// reads a credential's name (IsCredentialName and its credentialWords).
const profileFile = "internal/providers/llm/harness/harnessprofile/profile.go"

// profilesYAML is the engine's table of built-in harness profiles.
const profilesYAML = "internal/providers/llm/harness/harnessprofile/profiles.yaml"

// credentialNamePattern is the names Crewlet reads as holding a credential,
// which sign an env sign-in harness in: one of their parts, cut at every
// character that is not a letter or a digit, is, in any case, one of the words
// below (harnessprofile.IsCredentialName). It is the pattern of every rule of
// the API that reads a credential's name, which
// TestCredentialNameFollowsTheEngineSource holds to the engine: the LLMProvider
// and platform provider API's rules over env, which the CRD suite holds to the
// engine's sign-in, and the tool-server API's refusal of a literal value under
// such a name, which the CRD suite holds to the names the engine reads a seat's
// tool credential under.
//
//nolint:gosec // G101: the pattern a credential's name matches, which holds no credential.
const credentialNamePattern = `(?i)(^|[^A-Za-z0-9])(KEY|KEYS|APIKEY|ACCESSKEY|SECRETKEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|CREDENTIALS|AUTH|OAUTH|PAT)([^A-Za-z0-9]|$)`

// credentialNameRules are the kinds whose CRDs read a credential's name, and
// how many of their rules do (every other kind's, none): the LLMProvider
// API's two over a harness's sign-in in env, under Subscription and under Env,
// and the platform provider's two, and
// the tool-server API's refusal of a literal value under such a name, on each
// type that takes one (an MCPServer's env and headers, and the values of a
// Seat's or a Unit's spec.mcp).
var credentialNameRules = map[string]int{
	"engineclasses": 2,
	"llmproviders":  2,
	"mcpservers":    2,
	"seats":         1,
	"units":         1,
}

// TestCredentialNameFollowsTheEngineSource holds the one pattern the operator
// reads a credential's name with to the engine's source, and every rule of
// the API that reads one to that pattern. The words are Crewlet's
// credentialWords, in its order; how a name is cut into parts is
// IsCredentialName's, which TestMirroredEngineDeclarationsAreUnchanged holds, as
// it holds the function and not the list. The LLMProvider API's rules are
// also held to the engine's sign-in by the CRD suite, and the tool-server
// API's to the names the engine reads a seat's tool credentials under.
func TestCredentialNameFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	src, err := source.ReadFile(profileFile)
	if err != nil {
		t.Fatal(err)
	}
	words := stringSliceVar(t, profileFile, src, "credentialWords")
	const head, tail = `(?i)(^|[^A-Za-z0-9])(`, `)([^A-Za-z0-9]|$)`
	if want := head + strings.Join(words, "|") + tail; credentialNamePattern != want {
		t.Errorf("the engine reads a credential's name as one whose part is one of %v, which is the pattern %q; "+
			"the operator's is %q", words, want, credentialNamePattern)
	}

	files, err := filepath.Glob(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the CRDs in config/crd/bases: %v", err)
	}
	for _, file := range files {
		kind := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "crewlet.ai_"), ".yaml")
		want := credentialNameRules[kind]
		found := 0
		for _, rule := range rules(crdOf(t, kind)) {
			// A rule reads a credential's name where it names the words.
			if !strings.Contains(rule, "|APIKEY|") {
				continue
			}
			found++
			_, rest, ok := strings.Cut(rule, "name.matches('")
			pattern, _, _ := strings.Cut(rest, "')")
			if !ok || pattern != credentialNamePattern {
				t.Errorf("a rule of the %s CRD reads a credential's name other than as %q: %s",
					kind, credentialNamePattern, rule)
			}
		}
		if found != want {
			t.Errorf("%d rules of the %s CRD read a credential's name, want %d", found, kind, want)
		}
	}
}

// crdOf is the decoded CRD of kind, its plural, from config/crd/bases.
func crdOf(t *testing.T, kind string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_"+kind+".yaml")))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := yaml.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

// stringSliceVar is the string literals of the []string var name declares in
// the Go source src of file.
func stringSliceVar(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	return stringLiterals(t, file, name, declared(t, file, src, name))
}

// stringLiterals is the string literals of e, a literal of a []string of
// name's in file.
func stringLiterals(t *testing.T, file, name string, e ast.Expr) []string {
	t.Helper()
	list, ok := e.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s in %s is no literal", name, file)
	}
	out := make([]string, 0, len(list.Elts))
	for _, elt := range list.Elts {
		out = append(out, stringLiteral(t, file, name, elt))
	}
	return out
}

// stringLiteral is the value of e, a string literal of name's in file.
func stringLiteral(t *testing.T, file, name string, e ast.Expr) string {
	t.Helper()
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s in %s holds a value that is no string literal", name, file)
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// declared is the value a var of name alone is declared with in the Go source
// src of file.
func declared(t *testing.T, file string, src []byte, name string) ast.Expr {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == name && len(value.Values) == 1 {
				return value.Values[0]
			}
		}
	}
	t.Fatalf("%s declares no var %s", file, name)
	return nil
}

// rules is every x-kubernetes-validations rule in a decoded CRD.
func rules(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		if validations, ok := n["x-kubernetes-validations"].([]any); ok {
			for _, v := range validations {
				if rule, ok := v.(map[string]any)["rule"].(string); ok {
					out = append(out, rule)
				}
			}
		}
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	case []any:
		for _, child := range n {
			out = append(out, rules(child)...)
		}
	}
	return out
}
