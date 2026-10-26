package enginetest_test

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// Every testing.TB must be a T, or a plain Go test could not start an engine.
var _ enginetest.T = testing.TB(nil)

// TestWithCompanyStartsReady pins the seeded start: Start returns once the
// seed is the engine's running company, so a test can read it at once.
func TestWithCompanyStartsReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t, enginetest.WithCompany([]byte(enginetest.MinimalCompany))).Client()
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready() right after a seeded Start = %v", err)
	}
	active, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(active.Document), "name: Acme") {
		t.Fatalf("the active document is not the seed:\n%s", active.Document)
	}
}

// TestEnginesDoNotShareState runs two engines at once, as parallel tests do,
// and checks that each has its own port, tokens and store.
func TestEnginesDoNotShareState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, b := enginetest.Start(t), enginetest.Start(t)
	if a.URL == b.URL || a.OperatorToken == b.OperatorToken || a.Dir == b.Dir {
		t.Fatalf("two engines share a port, a token or a directory: %s %s", a.URL, b.URL)
	}
	if err := a.Client().PutSecret(ctx, "ONLY_IN_A", []byte("v")); err != nil {
		t.Fatal(err)
	}
	listed, err := b.Client().ListSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("engine b lists %+v, want nothing written to engine a", listed)
	}
}

// fatal is the panic fatalT's Fatalf raises, standing for the end of the
// test that testing.T's Fatalf brings.
type fatal string

// fatalT is a T whose Fatalf ends the call it is in, so a test can see that
// Binary fails a test rather than skip it.
type fatalT struct{ testing.TB }

func (f fatalT) Fatalf(format string, args ...any) { panic(fatal(fmt.Sprintf(format, args...))) }

// TestBinaryFailsWithoutTheEngine pins ADR-0002: a suite with no engine
// binary fails, with the command that builds one, rather than skipping its
// contract tests and passing.
func TestBinaryFailsWithoutTheEngine(t *testing.T) {
	t.Setenv(enginetest.BinaryEnv, filepath.Join(t.TempDir(), "no-crewlet-here"))
	defer func() {
		msg, ok := recover().(fatal)
		if !ok {
			t.Fatal("Binary returned without failing the test")
		}
		if !strings.Contains(string(msg), "unset it to use bin/crewlet") {
			t.Errorf("Binary failed with %q, which does not say how to get the engine", msg)
		}
	}()
	enginetest.Binary(fatalT{t})
}

// TestCompanyWritersRefuseWhatWouldIgnoreThem: WithCompanyWriters makes the
// document managed only on enginetest's own Tier A, and a managed engine
// ignores a -company seed. Combined with either, the engine would run
// unmanaged or unseeded while the test believed otherwise, so Start fails
// the test before it runs anything.
func TestCompanyWritersRefuseWhatWouldIgnoreThem(t *testing.T) {
	writers := enginetest.WithCompanyWriters(enginetest.OperatorTokenID)
	for name, other := range map[string]enginetest.Option{
		"WithCompany": enginetest.WithCompany([]byte(enginetest.MinimalCompany)),
		"WithTierA": enginetest.WithTierA("operator-token", "access-token", func(int, string) []byte {
			panic(fatal("Start rendered the caller's Tier A, which ignores the writers"))
		}),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				msg, ok := recover().(fatal)
				if !ok {
					t.Fatalf("Start ran an engine with %s and WithCompanyWriters", name)
				}
				if !strings.Contains(string(msg), name+" and WithCompanyWriters do not combine") {
					t.Errorf("Start failed with %q, which does not say the two options do not combine", msg)
				}
			}()
			enginetest.Start(fatalT{t}, writers, other)
		})
	}
}

// TestNodeRefusesACallersTierA: WithNode writes the node block of
// enginetest's own Tier A; with WithTierA the engine would run the caller's
// node instead while the test believed otherwise, so Start fails the test
// before it runs anything.
func TestNodeRefusesACallersTierA(t *testing.T) {
	defer func() {
		msg, ok := recover().(fatal)
		if !ok {
			t.Fatal("Start ran an engine with WithTierA and WithNode")
		}
		if !strings.Contains(string(msg), "WithTierA and WithNode do not combine") {
			t.Errorf("Start failed with %q, which does not say the two options do not combine", msg)
		}
	}()
	enginetest.Start(fatalT{t}, enginetest.WithNode("n", nil), enginetest.WithTierA("operator-token", "access-token", func(int, string) []byte {
		panic(fatal("Start rendered the caller's Tier A, which ignores the node"))
	}))
}

// TestModuleVersionRefusesASourceNoVersionIs holds Source to the code the
// engine binary runs: a binary that is not a module version of crewlet, and
// one built from a checkout with uncommitted changes, whose stamped version
// the module proxy serves as the commit without them, name no source, and a
// version that names one is returned as it is.
func TestModuleVersionRefusesASourceNoVersionIs(t *testing.T) {
	const commit = "v0.0.0-20261023214803-bbedf7ecebd7"
	for _, c := range []struct {
		name, path, version string
		// says is what the refusal must say, empty for none.
		says string
	}{
		{name: "a commit of crewlet", path: enginetest.Module, version: commit},
		{name: "a release of crewlet", path: enginetest.Module, version: "v0.2.0"},
		{
			name: "a checkout with uncommitted changes", path: enginetest.Module, version: commit + "+dirty",
			says: "built from a checkout with uncommitted changes",
		},
		{name: "a build with no version", path: enginetest.Module, version: "(devel)", says: "install it with `make crewlet`"},
		{name: "another module", path: "example.com/crewlet", version: commit, says: "not a module version of"},
	} {
		t.Run(c.name, func(t *testing.T) {
			info := &buildinfo.BuildInfo{Main: debug.Module{Path: c.path, Version: c.version}}
			got, err := enginetest.ModuleVersion(info)
			switch {
			case c.says == "" && (err != nil || got != c.version):
				t.Errorf("ModuleVersion = %q, %v; want %q", got, err, c.version)
			case c.says != "" && (err == nil || !strings.Contains(err.Error(), c.says)):
				t.Errorf("ModuleVersion = %q, %v; want a refusal saying %q", got, err, c.says)
			}
		})
	}
}

// TestRestartKeepsTheEnginesState pins Restart: the engine comes back at the
// same address, with the same tokens, on what its directory kept, the secret
// store included, as a pod's container restarted on its volume does.
func TestRestartKeepsTheEnginesState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t)
	url := e.URL
	if err := e.Client().PutSecret(ctx, "KEPT_ACROSS_A_RESTART", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := e.Restart(t); err != nil {
		t.Fatal(err)
	}
	listed, err := e.Client().ListSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if e.URL != url || len(listed) != 1 || listed[0].Name != "KEPT_ACROSS_A_RESTART" {
		t.Fatalf("after Restart the engine is at %s (was %s) and lists %+v, want the same address and the kept secret",
			e.URL, url, listed)
	}
}
