package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	yamlv3 "go.yaml.in/yaml/v3"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// TestExecutorSignInFollowsTheEngineSource holds the render's copies of what
// signs a seat's executor run in, which no validation shows, to the engine's
// profiles: the variables that sign ClaudeCode in from a run's environment
// are its profile's token_env and api_key_env, which carry a provider's own
// sign-in into the run, then its sign_in_env, the names that sign it in from
// a provider's env (a gateway's token, a cloud switch), as Crewlet's
// CredentialEnvNames reads them, in that order. No other profile may name
// sign_in_env: the render, and the API's rules over auth mode Env, read such
// names for ClaudeCode alone. The pattern of a credential's name is held by
// TestCredentialNameFollowsTheEngineSource.
func TestExecutorSignInFollowsTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	raw, err := source.ReadFile(profilesYAML)
	if err != nil {
		t.Fatal(err)
	}
	var profiles map[string]struct {
		TokenEnv  string   `json:"token_env"`
		APIKeyEnv string   `json:"api_key_env"`
		SignInEnv []string `json:"sign_in_env"`
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		t.Fatal(err)
	}
	claudeCode := harnessName(v1alpha1.HarnessClaudeCode)
	claude, ok := profiles[claudeCode]
	if !ok {
		t.Fatalf("the engine has no %s profile", claudeCode)
	}
	if !slices.Equal(claude.SignInEnv, claudeCodeEnvSignIn) {
		t.Errorf("the engine's %s profile signs it in from env by %v (sign_in_env), the render by %v",
			claudeCode, claude.SignInEnv, claudeCodeEnvSignIn)
	}
	if want := slices.Concat([]string{claude.TokenEnv, claude.APIKeyEnv}, claude.SignInEnv); !slices.Equal(
		want, claudeCodeSignIn) {
		t.Errorf("the engine signs ClaudeCode in from a run's %v (token_env, api_key_env, sign_in_env), the render "+
			"from %v", want, claudeCodeSignIn)
	}
	for _, name := range slices.Sorted(maps.Keys(profiles)) {
		if p := profiles[name]; name != claudeCode && len(p.SignInEnv) > 0 {
			t.Errorf("the engine's %s profile signs its harness in from env by %v (sign_in_env), which the render and "+
				"the API's rules over auth mode Env read for ClaudeCode alone", name, p.SignInEnv)
		}
	}
}

// TestExecutorHarnessesFollowTheEngineSource holds the harnesses the render
// runs a seat's executor on (ExecutorHarnesses) to the engine's one list of
// them, in its order: the built-in profiles in profilesYAML that carry an
// executor block, in the file's order (harnessprofile.ExecutorRunners), which
// the engine holds its registered runners to and derives
// config.ExecutorHarnesses from, refusing a chain without one of them and a
// runIn on any other harness. A harness either gains moves every verdict the
// render reaches on a chain, and the API's runIn rule
// (TestExecutorHarnessesAreTheRunIn). Those whose block names no
// e2b_template, which E2B prebuilds none for, are the render's unprebuilt,
// whose E2B runs need the Company's own template.
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
	var got, bare []string
	table := doc.Content[0].Content
	for i := 0; i+1 < len(table); i += 2 {
		var profile struct {
			Executor *struct {
				E2BTemplate string `yaml:"e2b_template"`
			} `yaml:"executor"`
		}
		if err := table[i+1].Decode(&profile); err != nil {
			t.Fatalf("%s: %s: %v", profilesYAML, table[i].Value, err)
		}
		if profile.Executor != nil {
			got = append(got, table[i].Value)
			if profile.Executor.E2BTemplate == "" {
				bare = append(bare, table[i].Value)
			}
		}
	}
	var unbuilt []string
	for harness := range unprebuilt {
		unbuilt = append(unbuilt, harnessName(harness))
	}
	slices.Sort(bare)
	slices.Sort(unbuilt)
	if !slices.Equal(bare, unbuilt) {
		t.Errorf("E2B prebuilds no template for %v (the executor blocks of %s naming no e2b_template), the render "+
			"for %v", bare, profilesYAML, unbuilt)
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
