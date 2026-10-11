package enginetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"strconv"
)

// ErrNoFunction is a source that declares no function of the name asked for.
var ErrNoFunction = errors.New("no such function")

// Fingerprint is the SHA-256, in hex, of the declaration of the function name
// in the Go source src of file: a function by its name (newCodingRun), a
// method by its receiver's type and its name, written as Go writes a method
// expression ((*launcher).Launch, or T.M on a value receiver).
//
// It is how a test holds a decision the operator copies from the engine's
// code, where no validation or API answer shows the engine making it, to the
// engine the suites run: the test records the digest of the function the
// operator was last read against, and a digest that differs is a function
// somebody has to read again. So the digest moves with what the function does
// and with nothing else. It is taken over the declaration as go/printer lays
// it out afresh: parsed without its comments, every position cleared so that
// none of the source's line breaks survives, each string literal written in
// double quotes and each run of them joined by + folded into one, beside a
// value it is joined to or not. A comment rewritten, a call split over several
// lines or a long message rewrapped leaves it as it was; a statement, a
// condition, a name, a spread (f(xs...)), an alias's = or the text of a
// literal changed moves it. A digest of the file would move with every edit
// to its other functions, and one of the function's bytes with every comment
// in it.
func Fingerprint(file string, src []byte, name string) (string, error) {
	// No comments, and no object resolution, whose objects point back into
	// the tree and would send the walk below round in a cycle.
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		return "", err
	}
	var found *ast.FuncDecl
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || funcName(fn) != name {
			continue
		}
		if found != nil {
			// Only init may be declared twice, and it is no decision of
			// anybody's to mirror.
			return "", fmt.Errorf("%s declares %s more than once", file, name)
		}
		found = fn
	}
	if found == nil {
		return "", fmt.Errorf("%s declares no %s: %w", file, name, ErrNoFunction)
	}
	canonical(reflect.ValueOf(found))
	var out bytes.Buffer
	if err := printer.Fprint(&out, fset, found); err != nil {
		return "", fmt.Errorf("printing %s in %s: %w", name, file, err)
	}
	sum := sha256.Sum256(out.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// funcName is how Fingerprint names a declaration: name, T.name or
// (*T).name, with a generic receiver's type parameters left out.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return fn.Name.Name
	}
	recv, pointer := fn.Recv.List[0].Type, false
	if star, ok := recv.(*ast.StarExpr); ok {
		recv, pointer = star.X, true
	}
	switch generic := recv.(type) {
	case *ast.IndexExpr:
		recv = generic.X
	case *ast.IndexListExpr:
		recv = generic.X
	}
	ident, ok := recv.(*ast.Ident)
	if !ok {
		return fn.Name.Name
	}
	if pointer {
		return "(*" + ident.Name + ")." + fn.Name.Name
	}
	return ident.Name + "." + fn.Name.Name
}

var (
	posType  = reflect.TypeFor[token.Pos]()
	exprType = reflect.TypeFor[ast.Expr]()
)

// syntax names, by the node that holds it, each position go/printer prints
// from whether it is valid rather than from where it is: the ... of a call
// that spreads its last argument (CallExpr.Ellipsis) and the = of a type
// alias (TypeSpec.Assign). Cleared like the rest, f(xs...) would print as
// f(xs) and type A = B as type A B, which do something else. They are the
// two go/printer's nodes.go asks IsValid of for what to print; the third it
// asks, a declaration's parenthesis (GenDecl.Lparen), is layout.
var syntax = map[reflect.Type]string{
	reflect.TypeFor[ast.CallExpr](): "Ellipsis",
	reflect.TypeFor[ast.TypeSpec](): "Assign",
}

// present is the position a syntax position keeps when it is valid: the
// first of the file, the same wherever the source put it.
const present = token.Pos(1)

// canonical clears every position in the tree under v but what syntax names,
// so that go/printer lays the declaration out from its structure alone,
// writes every string literal quoted, and folds each concatenation of string
// literals into one, after its operands, so that a chain of them folds from
// the left into a single literal. Parsed without comments, the tree holds no
// comment to print, and the declaration's Doc is nil.
func canonical(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			canonical(v.Elem())
		}
	case reflect.Struct:
		if lit, ok := reflect.TypeAssert[*ast.BasicLit](v.Addr()); ok {
			if s, ok := stringLit(lit); ok {
				lit.Value = strconv.Quote(s)
			}
		}
		for f, field := range v.Fields() {
			if field.Type() == posType {
				pos := token.NoPos
				if syntax[v.Type()] == f.Name && token.Pos(field.Int()).IsValid() {
					pos = present
				}
				field.SetInt(int64(pos))
				continue
			}
			canonical(field)
			foldStrings(field)
		}
	case reflect.Slice:
		for i := range v.Len() {
			canonical(v.Index(i))
			foldStrings(v.Index(i))
		}
	default:
	}
}

// foldStrings folds the expression v holds when it adds a string literal to
// another: to one literal when both operands are, and when the left operand
// is itself a sum ending in a literal, as in "a" + v + "b " + "c", into that
// sum with its last literal extended ("a" + v + "b c"). + is left-associative,
// so the chain's earlier links are folded before it, and a literal operand
// makes every sum of the chain a string's, where joining two of its literals
// is what Go does.
func foldStrings(v reflect.Value) {
	if v.Type() != exprType || v.IsNil() {
		return
	}
	sum, ok := reflect.TypeAssert[*ast.BinaryExpr](v)
	if !ok || sum.Op != token.ADD {
		return
	}
	y, ok := stringLit(sum.Y)
	if !ok {
		return
	}
	if x, ok := stringLit(sum.X); ok {
		v.Set(reflect.ValueOf(quoted(x + y)))
		return
	}
	if left, ok := sum.X.(*ast.BinaryExpr); ok && left.Op == token.ADD {
		if x, ok := stringLit(left.Y); ok {
			left.Y = quoted(x + y)
			v.Set(reflect.ValueOf(left))
		}
	}
}

// quoted is the string literal of s.
func quoted(s string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

// stringLit is the value of a string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
