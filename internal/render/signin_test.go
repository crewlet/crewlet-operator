package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

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
	raw, err := source.ReadFile("internal/providers/llm/cliagent/cliprofile/profiles.yaml")
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
	claudeCode := cliAgentName(v1alpha1.CLIAgentClaudeCode)
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
			t.Errorf("the engine's %s profile signs its CLI in from env by %v (sign_in_env), which the render and "+
				"the API's rules over auth mode Env read for ClaudeCode alone", name, p.SignInEnv)
		}
	}
}

// TestRunnerCLIsFollowTheEngineSource holds the CLIs the render runs a seat's
// executor on (RunnerCLIs) to the engine's two lists of them, in their order:
// the coding agents it has an in-box runner for (codingagent.Names), and the
// CLIs config lets run an executor (config.CodingAgents), which it refuses a
// chain without and a runIn on any other CLI by. A CLI either gains moves
// every verdict the render reaches on a chain, and the API's runIn rule
// (TestRunnerCLIsAreTheRunIn).
func TestRunnerCLIsFollowTheEngineSource(t *testing.T) {
	source := openSource(t, enginetest.Source(t))
	want := make([]string, len(RunnerCLIs))
	for i, cli := range RunnerCLIs {
		want[i] = cliAgentName(cli)
	}

	const names = "internal/sandbox/codingagent/names.go"
	src, err := source.ReadFile(names)
	if err != nil {
		t.Fatal(err)
	}
	runners := constsOf(t, source, path.Dir(names))
	idents := returnedIdents(t, names, src, "Names")
	got := make([]string, 0, len(idents))
	for _, ident := range idents {
		got = append(got, runners[ident])
	}
	if !slices.Equal(got, want) {
		t.Errorf("the engine has an in-box runner for %v, the render runs an executor on %v", got, want)
	}

	const providers = "internal/config/providers.go"
	if src, err = source.ReadFile(providers); err != nil {
		t.Fatal(err)
	}
	config := constsOf(t, source, path.Dir(providers))
	list, ok := declared(t, providers, src, token.VAR, "CodingAgents").(*ast.CompositeLit)
	if !ok {
		t.Fatalf("CodingAgents in %s is no literal", providers)
	}
	got = make([]string, 0, len(list.Elts))
	for _, elt := range list.Elts {
		ident, ok := elt.(*ast.Ident)
		if !ok {
			t.Fatalf("CodingAgents in %s holds %T, not a constant's name", providers, elt)
		}
		got = append(got, config[ident.Name])
	}
	if !slices.Equal(got, want) {
		t.Errorf("Crewlet's config runs an executor on %v, the render on %v", got, want)
	}
}

// returnedIdents is the names of the constants the function name returns as
// one []string literal in the Go source src of file.
func returnedIdents(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Recv != nil || fn.Body == nil || len(fn.Body.List) != 1 {
			continue
		}
		ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			break
		}
		list, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			break
		}
		var out []string
		for _, elt := range list.Elts {
			ident, ok := elt.(*ast.Ident)
			if !ok {
				t.Fatalf("%s's %s returns %T, not a constant's name", file, name, elt)
			}
			out = append(out, ident.Name)
		}
		return out
	}
	t.Fatalf("%s declares no %s returning one []string literal", file, name)
	return nil
}

// constsOf is every string constant the Go package in dir of source
// declares with a literal, by name, its tests left out.
func constsOf(t *testing.T, source *os.Root, dir string) map[string]string {
	t.Helper()
	entries, err := fsReadDir(source, dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry, ".go") || strings.HasSuffix(entry, "_test.go") {
			continue
		}
		file := path.Join(dir, entry)
		src, err := source.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != len(value.Values) {
					continue
				}
				for i, n := range value.Names {
					if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							out[n.Name] = s
						}
					}
				}
			}
		}
	}
	return out
}

// fsReadDir is the names of the entries of dir in source.
func fsReadDir(source *os.Root, dir string) ([]string, error) {
	d, err := source.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	return d.Readdirnames(-1)
}

// profileFile is the engine's file of CLI profiles, which declares how it
// reads a credential's name (IsCredentialName and its credentialWords).
const profileFile = "internal/providers/llm/cliagent/cliprofile/profile.go"

// credentialNameRules are the kinds whose CRDs read a credential's name, and
// how many of their rules do (every other kind's, none): the LLMProvider
// API's two over a CLI's sign-in in env, under Subscription and under Env,
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
	return stringLiterals(t, file, name, declared(t, file, src, token.VAR, name))
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

// declared is the value a var or const (tok) of name alone is declared with
// in the Go source src of file.
func declared(t *testing.T, file string, src []byte, tok token.Token, name string) ast.Expr {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != tok {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if ok && len(value.Names) == 1 && value.Names[0].Name == name && len(value.Values) == 1 {
				return value.Values[0]
			}
		}
	}
	t.Fatalf("%s declares no %s %s", file, tok, name)
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
