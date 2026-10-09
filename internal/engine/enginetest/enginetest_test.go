package enginetest_test

import (
	"context"
	"fmt"
	"path/filepath"
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
	if err := a.Client().PutSecret(ctx, "ONLY_IN_A", []byte("v"), ""); err != nil {
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
