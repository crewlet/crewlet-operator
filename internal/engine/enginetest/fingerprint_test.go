package enginetest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// fingerprinted is a source file shaped like the engine's: a function, a
// method on a pointer and one on a value receiver of the same name, a
// function that joins a value into a message and declares an alias, a
// neighbour, a constant of its own, constants in a block, one of them iota's
// repetition, and a variable.
const fingerprinted = `package engine

import "fmt"

// pick is the decision a test holds.
func pick(env map[string]string, names []string) (string, error) {
	// The first name that has a value.
	for _, name := range names {
		if env[name] != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("none of %v is set, "+
		"so nothing signs the run in", names)
}

type launcher struct{}

func (l *launcher) Launch(n int) int { return n + 1 }

type value struct{}

func (v value) Launch(n int) int { return n + 2 }

func why(name string) error {
	type reason = string
	return errors.New(reason("the variable " + name + " is blank, " +
		"so nothing signs the run in"))
}

func neighbour() int { return 1 }

// budget bounds a boot.
const budget = 30 * time.Second

const (
	// soloBudget bounds one create.
	soloBudget, clusterBudget = 30 * time.Second, 2 * time.Minute
	first                     = iota
	second
)

var poll = 100 * time.Millisecond
`

// fingerprint is the digest of the declaration name in src.
func fingerprint(t *testing.T, src, name string) string {
	t.Helper()
	digest, err := enginetest.Fingerprint("engine.go", []byte(src), name)
	if err != nil {
		t.Fatalf("Fingerprint(%s) = %v", name, err)
	}
	return digest
}

// edit is fingerprinted with old replaced by new, which it must hold.
func edit(t *testing.T, old, replacement string) string {
	t.Helper()
	if !strings.Contains(fingerprinted, old) {
		t.Fatalf("the source holds no %q", old)
	}
	return strings.Replace(fingerprinted, old, replacement, 1)
}

// TestFingerprintIgnoresCommentsAndLayout: what the engine's maintainers
// edit most, comments and the wrapping gofmt leaves to them, must not make a
// test ask somebody to read a function that does what it did.
func TestFingerprintIgnoresCommentsAndLayout(t *testing.T) {
	want := fingerprint(t, fingerprinted, "pick")
	for name, src := range map[string]string{
		"the doc comment rewritten": edit(t, "// pick is the decision a test holds.",
			"// pick is the decision a test holds,\n// now said in two lines."),
		"a comment in the body removed": edit(t, "\t// The first name that has a value.\n", ""),
		"a comment in the body added": edit(t, "\t\t\treturn name, nil",
			"\t\t\t// Found.\n\n\t\t\treturn name, nil"),
		"the message rewrapped": edit(t, `"none of %v is set, "+
		"so nothing signs the run in"`, `"none of %v is set, so nothing "+"signs "+
		"the run in"`),
		"the message in one literal": edit(t, `"none of %v is set, "+
		"so nothing signs the run in"`, `"none of %v is set, so nothing signs the run in"`),
		"the message in back quotes": edit(t, `"none of %v is set, "+
		"so nothing signs the run in"`, "`none of %v is set, so nothing signs the run in`"),
		"the signature split over lines": edit(t, "func pick(env map[string]string, names []string) (string, error) {",
			"func pick(\n\tenv map[string]string,\n\tnames []string,\n) (string, error) {"),
		"a neighbour changed": edit(t, "func neighbour() int { return 1 }", "func neighbour() int { return 3 }"),
	} {
		if got := fingerprint(t, src, "pick"); got != want {
			t.Errorf("%s moved the digest of pick", name)
		}
	}

	// A spread is the same spread on a line of its own.
	if fingerprint(t, spread(t, false), "pick") != fingerprint(t, spread(t, true), "pick") {
		t.Error("a spread split over lines moved the digest of pick")
	}

	// A message that joins a value in the middle, rewrapped on either side
	// of it, is the message it was.
	was := fingerprint(t, fingerprinted, "why")
	for name, src := range map[string]string{
		"the tail rewrapped": edit(t, `" is blank, " +
		"so nothing signs the run in"`, `" is blank, so nothing " + "signs the run in"`),
		"the tail in one literal": edit(t, `" is blank, " +
		"so nothing signs the run in"`, `" is blank, so nothing signs the run in"`),
		"the head split": edit(t, `"the variable " + name`, `"the " + "variable " + name`),
	} {
		if got := fingerprint(t, src, "why"); got != was {
			t.Errorf("%s moved the digest of why", name)
		}
	}
}

// spread is fingerprinted with pick's names spread into its call, on the
// call's last line or, as gofmt leaves a long call, on a line of its own.
func spread(t *testing.T, ownLine bool) string {
	t.Helper()
	if ownLine {
		return edit(t, "names)\n}", "\n\t\tnames...,\n\t)\n}")
	}
	return edit(t, "names)\n}", "names...)\n}")
}

// TestFingerprintMovesWithTheCode is the other half: every change to what
// the function does is a function somebody has to read again.
func TestFingerprintMovesWithTheCode(t *testing.T) {
	was := fingerprint(t, fingerprinted, "pick")
	for name, src := range map[string]string{
		"a condition":         edit(t, `env[name] != ""`, `env[name] == ""`),
		"a statement removed": edit(t, "\t\t\treturn name, nil\n", ""),
		"a literal's text":    edit(t, `"so nothing signs the run in"`, `"so nothing signs it in"`),
		"a name":              edit(t, "return name, nil", "return names[0], nil"),
		"a range":             edit(t, "for _, name := range names", "for _, name := range names[1:]"),
		"the signature":       edit(t, "(string, error) {", "(string, bool, error) {"),
		"a call":              edit(t, "fmt.Errorf(", "fmt.Sprintf("),
		"a statement added":   edit(t, "\treturn \"\", fmt", "\tnames = nil\n\treturn \"\", fmt"),
		"an operand":          edit(t, "names)\n}", "env)\n}"),
		// One argument where there were as many as names holds.
		"a spread": spread(t, false),
	} {
		if got := fingerprint(t, src, "pick"); got == was {
			t.Errorf("%s left the digest of pick as it was", name)
		}
	}

	was = fingerprint(t, fingerprinted, "why")
	for name, src := range map[string]string{
		"the text after the value": edit(t, `" is blank, "`, `" is set, "`),
		"the value":                edit(t, `+ name +`, `+ "name" +`),
		// type reason string is a type of its own, not string by another
		// name.
		"an alias made a type": edit(t, "type reason = string", "type reason string"),
	} {
		if got := fingerprint(t, src, "why"); got == was {
			t.Errorf("%s left the digest of why as it was", name)
		}
	}
}

// TestFingerprintNamesAMethodByItsReceiver: two methods of one name are two
// decisions, and a method moved to the other kind of receiver is not the
// method the test recorded.
func TestFingerprintNamesAMethodByItsReceiver(t *testing.T) {
	pointer := fingerprint(t, fingerprinted, "(*launcher).Launch")
	value := fingerprint(t, fingerprinted, "value.Launch")
	if pointer == value {
		t.Error("(*launcher).Launch and value.Launch have one digest")
	}
	for _, name := range []string{"launcher.Launch", "(*value).Launch", "Launch"} {
		if _, err := enginetest.Fingerprint("engine.go", []byte(fingerprinted), name); !errors.Is(err, enginetest.ErrNoDeclaration) {
			t.Errorf("Fingerprint(%s) = %v, want ErrNoDeclaration", name, err)
		}
	}
}

// TestFingerprintRefusesWhatIsNotThere: a declaration the engine removed or
// renamed fails as absent, and a name declared twice, a constant that only
// repeats the one before it and a file that does not parse fail as
// themselves, so none of them reads as a declaration that did not change.
func TestFingerprintRefusesWhatIsNotThere(t *testing.T) {
	for _, name := range []string{"choose", "timeout"} {
		if _, err := enginetest.Fingerprint("engine.go", []byte(fingerprinted), name); !errors.Is(err, enginetest.ErrNoDeclaration) {
			t.Errorf("Fingerprint of %s, which the file does not declare, = %v, want ErrNoDeclaration", name, err)
		}
	}
	twice := fingerprinted + "\nfunc init() {}\n\nfunc init() {}\n"
	if _, err := enginetest.Fingerprint("engine.go", []byte(twice), "init"); err == nil || errors.Is(err, enginetest.ErrNoDeclaration) {
		t.Errorf("Fingerprint of a function declared twice = %v, want a refusal", err)
	}
	if _, err := enginetest.Fingerprint("engine.go", []byte(fingerprinted), "second"); err == nil ||
		errors.Is(err, enginetest.ErrNoDeclaration) || !strings.Contains(err.Error(), "repeats") {
		t.Errorf("Fingerprint of a constant that repeats the one before it = %v, want a refusal saying so", err)
	}
	broken := strings.Replace(fingerprinted, "func neighbour() int {", "func neighbour() int", 1)
	if _, err := enginetest.Fingerprint("engine.go", []byte(broken), "pick"); err == nil || errors.Is(err, enginetest.ErrNoDeclaration) {
		t.Errorf("Fingerprint of a file that does not parse = %v, want the parse error", err)
	}
}

// TestFingerprintNamesAConstantOrAVariable: a constant is held by the spec
// that declares it, so its value moves its digest and its comment, its
// neighbours in a block and the block itself do not; and a constant made a
// variable is not the declaration that was read.
func TestFingerprintNamesAConstantOrAVariable(t *testing.T) {
	// The constants the source declares on their own and in a block.
	const alone, inBlock = "budget", "clusterBudget"
	budget := fingerprint(t, fingerprinted, alone)
	cluster := fingerprint(t, fingerprinted, inBlock)
	if solo := fingerprint(t, fingerprinted, "soloBudget"); solo != cluster {
		t.Error("two constants of one spec have two digests")
	}
	fingerprint(t, fingerprinted, "first")
	poll := fingerprint(t, fingerprinted, "poll")
	if budget == cluster || budget == poll {
		t.Error("two declarations share a digest")
	}
	for name, c := range map[string]struct {
		src, of, want string
	}{
		"the doc comment rewritten": {
			edit(t, "// budget bounds a boot.", "// budget bounds\n// a boot."), alone, budget,
		},
		"moved into a block": {
			edit(t, "const budget = 30 * time.Second", "const (\n\tbudget = 30 * time.Second\n)"), alone, budget,
		},
		"its value spelled over lines": {
			edit(t, "const budget = 30 * time.Second", "const budget = 30 *\n\ttime.Second"), alone, budget,
		},
		"a neighbour in its block changed": {edit(t, "first                     = iota", "first = 1"), inBlock, cluster},
		"a constant after it added": {
			edit(t, "\tsecond\n)", "\tsecond\n\tthird = 3\n)"), inBlock, cluster,
		},
	} {
		if got := fingerprint(t, c.src, c.of); got != c.want {
			t.Errorf("%s moved the digest of %s", name, c.of)
		}
	}
	for name, c := range map[string]struct {
		src, of, was string
	}{
		"its value":              {edit(t, "const budget = 30 * time.Second", "const budget = 60 * time.Second"), alone, budget},
		"its unit":               {edit(t, "const budget = 30 * time.Second", "const budget = 30 * time.Minute"), alone, budget},
		"its type":               {edit(t, "const budget = 30 * time.Second", "const budget time.Duration = 30 * time.Second"), alone, budget},
		"made a variable":        {edit(t, "const budget = 30 * time.Second", "var budget = 30 * time.Second"), alone, budget},
		"its spec's other value": {edit(t, "30 * time.Second, 2 * time.Minute", "30 * time.Second, 5 * time.Minute"), inBlock, cluster},
		"a variable's value":     {edit(t, "var poll = 100 * time.Millisecond", "var poll = time.Second"), "poll", poll},
	} {
		if got := fingerprint(t, c.src, c.of); got == c.was {
			t.Errorf("%s left the digest of %s as it was", name, c.of)
		}
	}
}
