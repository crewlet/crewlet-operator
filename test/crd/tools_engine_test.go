package crd_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// engineCredentialLists are the files of the engine's source whose
// CredentialKeys list the names it reads a seat's tool credential under, in
// its mcp_env: GitLab's token, GitHub's, and Datadog's application key, each
// where the engine's provisioning and setup look for it.
var engineCredentialLists = []string{
	"internal/gitlab/provision.go", "internal/github/provision.go", "internal/datadog/provision.go",
}

// The engine's function that names where it reads a seat's Atlassian token,
// and its file that declares how it reads a credential's name.
const (
	atlassianCredentials = "internal/atlassian/seatcred.go"
	atlassianTokenKeys   = "tokenKeys"
	cliProfile           = "internal/providers/llm/cliagent/cliprofile/profile.go"
)

// literalSettings are names of settings that are no credential, which the
// tool-server API takes as a literal value: slack-mcp-server's own, an
// address, a proxy, names that hold a credential's word only inside a longer
// part, and the headers GitHub's remote server reads its toolsets from.
var literalSettings = []string{
	envSlackChannels, "SLACK_MCP_ADD_MESSAGE_TOOL", envJiraURL, envProxy, "HERMES_MAX_TOKENS", "MY_KEY2",
	"MONKEY", "KEYRING_PATH", "AUTHOR", headerToolsets, "X-MCP-Readonly",
}

// TestToolServerLiteralsFollowTheEngine holds the tool-server API's refusal
// of a literal value to how the engine reads a credential's name, which the
// engine itself never applies to a tool server's values: it takes any literal
// there and masks it as a credential. Every name the engine reads a seat's
// tool credential under (GitLab's, GitHub's and Datadog's CredentialKeys, and
// the Atlassian token keys of tokenKeys), and every word of its
// credentialWords, alone, as a part of a variable's name and as a part of a
// header's, is refused a literal value on a Seat's and a Unit's spec.mcp and,
// where its grammar fits, an MCPServer's env and headers; the settings of
// literalSettings are taken. An engine change that reads a credential under a
// name the rule lets through fails here until the API follows it.
func TestToolServerLiteralsFollowTheEngine(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	read := func(file string) []byte {
		t.Helper()
		src, err := source.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		return src
	}

	var credentials []string
	for _, file := range engineCredentialLists {
		credentials = append(credentials, stringSlice(t, file, read(file), "CredentialKeys")...)
	}
	atlassian := stringsOf(t, atlassianCredentials, read(atlassianCredentials), atlassianTokenKeys)
	credentials = append(credentials, atlassian...)
	words := stringSlice(t, cliProfile, read(cliProfile), "credentialWords")
	if len(words) == 0 {
		t.Fatalf("%s declares no credential words", cliProfile)
	}
	for _, word := range words {
		header := strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		credentials = append(credentials, word, "VENDOR_"+word, strings.ToLower(word)+"_value", "X-"+header+"-Value")
	}

	// The engine's lists name some of the same names (Authorization, the
	// Atlassian tokens under each product): each is offered once.
	slices.Sort(credentials)
	credentials = slices.Compact(credentials)

	namespace := newNamespace(t)
	for _, name := range credentials {
		t.Run("refused "+name, func(t *testing.T) { holdValue(t, namespace, literal(name), msgLiteralCredential) })
	}
	for _, name := range literalSettings {
		t.Run("taken "+name, func(t *testing.T) { holdValue(t, namespace, literal(name), "") })
	}
}

// stringSlice is the string literals of the []string var name declares in
// the Go source src of file.
func stringSlice(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	parsed := parseGo(t, file, src)
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != name || len(value.Values) != 1 {
				continue
			}
			list, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s in %s is no literal", name, file)
			}
			out := make([]string, 0, len(list.Elts))
			for _, elt := range list.Elts {
				out = append(out, stringValue(t, file, name, elt, nil))
			}
			return out
		}
	}
	t.Fatalf("%s declares no var %s", file, name)
	return nil
}

// stringsOf is every string the []string literals of the function (or
// method) name in the Go source src of file hold: string literals, and the
// string constants of the file they name.
func stringsOf(t *testing.T, file string, src []byte, name string) []string {
	t.Helper()
	parsed := parseGo(t, file, src)
	consts := map[string]ast.Expr{}
	var body *ast.BlockStmt
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				value := spec.(*ast.ValueSpec)
				for i, ident := range value.Names {
					if i < len(value.Values) {
						consts[ident.Name] = value.Values[i]
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.Name == name {
				body = d.Body
			}
		}
	}
	if body == nil {
		t.Fatalf("%s declares no function %s", file, name)
	}
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		list, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if array, ok := list.Type.(*ast.ArrayType); ok {
			if elem, ok := array.Elt.(*ast.Ident); ok && elem.Name == "string" {
				for _, elt := range list.Elts {
					out = append(out, stringValue(t, file, name, elt, consts))
				}
			}
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("%s in %s names no string", name, file)
	}
	return out
}

// stringValue is the value of e, a string literal in name of file, or the
// name of one of consts.
func stringValue(t *testing.T, file, name string, e ast.Expr, consts map[string]ast.Expr) string {
	t.Helper()
	if ident, ok := e.(*ast.Ident); ok && consts[ident.Name] != nil {
		e = consts[ident.Name]
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		t.Fatalf("%s in %s holds a value that is no string literal: %s", name, file, fmt.Sprint(e))
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// parseGo parses the Go source src of file.
func parseGo(t *testing.T, file string, src []byte) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
