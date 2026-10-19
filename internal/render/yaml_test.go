package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// yamlSpecials are the runes a YAML encoder treats apart: every line break
// YAML 1.1 or 1.2 knows, the white space and control characters it escapes,
// the indicators that end a plain scalar or open another node, and a few
// ordinary ones to stand between them.
var yamlSpecials = []rune{
	'\n', '\r', '\t', ' ', 0x85, 0x2028, 0x2029, 0xFEFF, 0x80, 0x9F, 0x7F, 0, 0x1B, 0xA0, 0x3000, 0xFFFE, 0xFFFF,
	0x1F600, '#', ':', '-', '|', '>', '"', '\'', '\\', '%', '@', '`', '!', '&', '*', '{', '}', '[', ']', ',', '?',
	'~', '$', 'a', '0', '.',
}

// yamlRunes are the runes TestYAMLWritesEveryStringAsGiven tries one by one:
// ASCII, the C1 controls (NEL among them) and Latin-1; the general
// punctuation block (LS, PS, the other spaces and the invisible format
// characters); the edges of the private use area, the noncharacters, the BOM
// and the specials block; and the first, last and an emoji of the four-byte
// runes, which the encoder treats as one class. Every rune is more than the
// race detector, under which the suite runs, can afford in each shape: a run
// of all of them, 6.7 million strings in these shapes and 400,000 random ones
// of the specials, read back by gopkg.in/yaml.v3 v3.0.1, the parser Crewlet
// reads with, found none written otherwise.
func yamlRunes() []rune {
	var out []rune
	for _, block := range [][2]rune{
		{0, 0x2FF}, {0x2000, 0x206F}, {0xD7F0, 0xD7FF}, {0xE000, 0xE00F}, {0xF8F0, 0xF8FF}, {0xFDD0, 0xFDEF},
		{0xFEFF, 0xFEFF}, {0xFFF0, 0xFFFF},
	} {
		for r := block[0]; r <= block[1]; r++ {
			out = append(out, r)
		}
	}
	return append(out, 0x3000, 0x10000, 0x1F600, 0xE0001, utf8.MaxRune)
}

// yamlStrings is every rune of yamlRunes in the shapes a string takes around
// one (alone, inside a word, after and before a space, before and after a
// line break), and every pair of yamlSpecials in the same shapes.
func yamlStrings() []string {
	var out []string
	for _, r := range yamlRunes() {
		s := string(r)
		out = append(out, s, "a"+s+"b", " "+s, s+" ", s+"\n", "\n"+s)
	}
	for _, a := range yamlSpecials {
		for _, b := range yamlSpecials {
			s := string([]rune{a, b})
			out = append(out, s, "x"+s+"y", "\t"+s, s+"\n", "  "+s+"\n"+s)
		}
	}
	return append(out, "", "line one\nline two\n", "\t\n\t\n", "a\r\nb", "yes", "no", "on", "off", "y", "n", "~",
		"null", "true", "False", "1e3", "0x1f", "0o17", "1:20", ".inf", "-.nan", "- a", "? b", "a: b", "a #b",
		"${CREWLET_NODE_ID}", "'q'", `"q"`)
}

// TestYAMLWritesEveryStringAsGiven holds yamlOf to the one property the
// engine needs of it: every string, as a value, a list's item and a key,
// reads back from the YAML exactly as it was given. It reads the YAML with
// the encoder's own family's parser, the maintained continuation of the one
// Crewlet reads with (gopkg.in/yaml.v3); TestContractYAMLReachesTheEngineAsWritten
// asks the engine itself.
//
// It fails on the encoder this replaced (a NEL read back as a space, every
// other C1 control character not written at all), and on yamlOf letting the
// encoder write a string holding a line break as a literal block (one that
// opens with a tab before its first break does not parse).
func TestYAMLWritesEveryStringAsGiven(t *testing.T) {
	for _, s := range yamlStrings() {
		// A document of its own for each: the parser's check for a
		// mapping's duplicate keys is quadratic in its keys, and one
		// document of every string takes minutes to read.
		in := map[string]any{"value": s, "seq": []string{s, s}, "keys": map[string]string{s: "v"}}
		encoded, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		out, err := yamlOf(encoded)
		if err != nil {
			t.Errorf("yamlOf(%q): %v", s, err)
			continue
		}
		var got struct {
			Value string            `yaml:"value"`
			List  []string          `yaml:"seq"`
			Keys  map[string]string `yaml:"keys"`
		}
		if err := yaml.Unmarshal(out, &got); err != nil {
			t.Errorf("the YAML written for %q does not parse (%v):\n%s", s, err, out)
			continue
		}
		if got.Value != s || !reflect.DeepEqual(got.List, []string{s, s}) || !reflect.DeepEqual(got.Keys, map[string]string{s: "v"}) {
			t.Errorf("%q reads back as value %q, list %q and keys %q from\n%s", s, got.Value, got.List, got.Keys, out)
		}
	}
}

// TestYAMLKeepsEveryValueOfTheJSON holds yamlOf to the JSON's other values:
// numbers keep their value, and the type the engine decodes them as (an
// integer as an integer, a fraction as a float), booleans and null stay what
// they are, and a mapping's keys are written sorted.
func TestYAMLKeepsEveryValueOfTheJSON(t *testing.T) {
	encoded := []byte(`{"z":1,"a":{"int":-9223372036854775808,"uint":18446744073709551615,` +
		`"exp":1e3,"frac":0.5,"tiny":5e-324,"bool":true,"null":null,"array":[false,0,"0"]},"m":"x"}`)
	out, err := yamlOf(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"z": 1, "m": "x",
		"a": map[string]any{
			"int": math.MinInt64, "uint": uint64(math.MaxUint64), "exp": 1000, "frac": 0.5, "tiny": 5e-324,
			"bool": true, "null": nil, "array": []any{false, 0, "0"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("yamlOf wrote\n%s\nwhich reads back as %#v, want %#v", out, got, want)
	}
	if a, m, z := bytes.Index(out, []byte("a:")), bytes.Index(out, []byte("m:")), bytes.Index(out, []byte("z:")); a > m || m > z {
		t.Errorf("yamlOf wrote the keys unsorted:\n%s", out)
	}
}

// TestContractYAMLReachesTheEngineAsWritten asks the engine itself what it
// reads from the operator's YAML: one data pod's crewlet.yaml whose
// api.auth.allowed_origins holds every string of yamlStrings twice, as it is
// and between two letters. Each between letters is no origin at all and
// whole after the engine's trim, so `crewlet validate` refuses it and quotes
// it as it decoded it, which must be the string the render wrote, rune for
// rune. Each as it is is there for what the YAML holds around it: one the
// engine's parser cannot read refuses the whole document, and one it reads
// and quotes must be the string as the engine trims it.
//
// TestYAMLWritesEveryStringAsGiven reads with a parser of the same family,
// but a later release than the engine's (gopkg.in/yaml.v3 v3.0.1, which the
// operator does not build with): only the engine can say what the engine
// reads.
func TestContractYAMLReachesTheEngineAsWritten(t *testing.T) {
	var origins, want []string
	for _, s := range yamlStrings() {
		// A reference is resolved from the environment before the
		// document is read, so the engine would quote its value.
		if !strings.Contains(s, "${") {
			origins = append(origins, "zz"+s+"zz", s)
			want = append(want, "zz"+s+"zz", strings.TrimSpace(s))
		}
	}
	doc, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	var tierA map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
		t.Fatal(err)
	}
	tierA["api"].(map[string]any)["auth"].(map[string]any)["allowed_origins"] = origins
	encoded, err := json.Marshal(tierA)
	if err != nil {
		t.Fatal(err)
	}
	written, err := yamlOf(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", written))
	read := make([]*string, len(origins))
	for _, raw := range got.Problems {
		var p struct{ Path, Message string }
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		var i int
		if _, err := fmt.Sscanf(p.Path, "api.auth.allowed_origins[%d]", &i); err != nil || i < 0 || i >= len(origins) {
			t.Fatalf("crewlet validate refused the Tier A at %s, not at an allowed_origins entry: %s\n%s", p.Path, p.Message, stderr)
		}
		if _, quoted, ok := strings.Cut(p.Message, "wrong shape: "); ok {
			if prefix, err := strconv.QuotedPrefix(quoted); err == nil {
				value, _ := strconv.Unquote(prefix)
				read[i] = &value
			}
		}
	}
	if len(got.Problems) == 0 {
		t.Fatalf("crewlet validate refused none of the entries:\n%.2000s\n%s", stdout, stderr)
	}
	for i, origin := range origins {
		switch between := i%2 == 0; {
		case read[i] == nil && between:
			t.Errorf("crewlet validate quoted no value for the entry %q", origin)
		case read[i] != nil && *read[i] != want[i]:
			t.Errorf("the render wrote %q, and the engine read %q", origin, *read[i])
		}
	}
}
