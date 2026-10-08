// Package enginetest runs a real Crewlet engine for a test: the pinned crewlet
// binary, on a free loopback port, with a throwaway Tier A of its own in the
// test's temporary directory.
//
// The binary is $CREWLET_BIN, or bin/crewlet under the module root, which
// `make crewlet` builds at the version the Makefile pins (CREWLET_VERSION). A
// test that needs it fails, rather than skips, when it is missing: a skipped
// contract test is indistinguishable from a passing one in a CI summary, and
// this suite is what tells the operator and the engine apart when they drift.
package enginetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// BinaryEnv names the environment variable that overrides where the crewlet
// binary is found.
const BinaryEnv = "CREWLET_BIN"

// Token ids the engine records as the author of each revision a token writes.
const (
	// OperatorTokenID is the id of the token the operator holds.
	OperatorTokenID = "crewlet-operator"

	// AccessTokenID is the id of a second token, standing for a person or a
	// control plane that reaches the same engine: a test of attribution or
	// of concurrent writers needs two credentials the engine tells apart.
	AccessTokenID = "access"
)

// MinimalCompany is the smallest company document the engine accepts and runs
// in full: a name, one agent seat, and one LLM provider. The provider is
// OpenAI-compatible and points at port 9 (discard) on the loopback interface,
// which nothing serves, so a seat that ever tried to take a turn would fail
// at once rather than reach a real model; its key is a ${VAR} reference that
// nothing sets, which the engine accepts and resolves only when a turn runs.
const MinimalCompany = `name: Acme
providers:
  llm:
    default:
      type: openai-compatible
      model: enginetest-model
      base_url: http://127.0.0.1:9/v1
      api_keys:
        - "${ENGINETEST_LLM_API_KEY}"
roles:
  - name: Ana
    goal: "Answer every question the company is asked"
`

// startTimeout bounds how long an engine may take to answer after it is
// started. A fresh engine answers in about a second; the margin is for a
// loaded CI runner, where a -race test binary competes for the same CPUs.
const startTimeout = 60 * time.Second

// stopTimeout bounds the engine's drain after SIGTERM. An engine running no
// turns drains in well under a second; one that has not stopped by then is a
// bug in the engine or in the test, and is killed and reported.
const stopTimeout = 30 * time.Second

// portAttempts is how many times Start picks a new port when the one it chose
// was taken between choosing it and the engine binding it.
const portAttempts = 3

// T is the part of testing.TB that Start and Binary use. It is an interface
// of its own, rather than testing.TB, so that a Ginkgo suite can start an
// engine with GinkgoT(): testing.TB has an unexported method that only the
// testing package's own types implement. Every testing.TB is a T.
type T interface {
	Helper()
	TempDir() string
	Cleanup(func())
	Failed() bool
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// Engine is a running engine.
type Engine struct {
	// URL is the base URL of the engine's API, http://127.0.0.1:<port>.
	URL string

	// OperatorToken authenticates as OperatorTokenID.
	OperatorToken string

	// AccessToken authenticates as AccessTokenID.
	AccessToken string

	// Dir is the engine's working directory: its Tier A, store and stream.
	Dir string

	// LogPath is the file the engine's stdout and stderr go to. Start prints
	// its tail when the test fails.
	LogPath string
}

// Client returns an API client for the engine, authenticated with the
// operator token.
func (e *Engine) Client() *engine.Client {
	return engine.New(e.URL, e.OperatorToken)
}

// Option configures Start.
type Option func(*options)

type options struct {
	company []byte
}

// WithCompany seeds the engine with document as its first company revision
// (`crewlet run -company`), and makes Start wait until the engine is ready,
// which is when that revision has been applied. Without it the engine starts
// with no company, and Start waits only until its API answers.
func WithCompany(document []byte) Option {
	return func(o *options) { o.company = document }
}

// Start runs an engine for the rest of the test and stops it at cleanup.
func Start(t T, opts ...Option) *Engine {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	bin := Binary(t)
	dir := t.TempDir()
	e := &Engine{
		OperatorToken: randomToken(t),
		AccessToken:   randomToken(t),
		Dir:           dir,
		LogPath:       filepath.Join(dir, "crewlet.log"),
	}
	args := []string{"run", "-config", filepath.Join(dir, "crewlet.yaml")}
	if o.company != nil {
		company := filepath.Join(dir, "seed.company.yaml")
		writeFile(t, company, o.company)
		args = append(args, "-company", company)
	}
	env := []string{
		// Nothing of the caller's environment but PATH: a developer's own
		// CREWLET_* or provider keys must not change what the engine runs.
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"ENGINETEST_OPERATOR_TOKEN=" + e.OperatorToken,
		"ENGINETEST_ACCESS_TOKEN=" + e.AccessToken,
		"ENGINETEST_SECRET_KEY=" + randomKey(t),
	}

	for attempt := 1; ; attempt++ {
		port := freePort(t)
		writeFile(t, filepath.Join(dir, "crewlet.yaml"), tierA(port))
		e.URL = "http://127.0.0.1:" + strconv.Itoa(port)
		err := e.run(t, bin, args, env, o.company != nil)
		if err == nil {
			return e
		}
		// The port was free when it was chosen; another process can take it
		// before the engine binds it, and that alone is worth another try.
		if !errors.Is(err, errPortTaken) || attempt == portAttempts {
			t.Fatalf("starting the crewlet engine (%s): %v\n%s", bin, err, tail(e.LogPath))
		}
	}
}

// errPortTaken is an engine that exited because its port was in use.
var errPortTaken = errors.New("the engine's port was taken")

// run starts the engine and waits until it answers. On success the engine is
// stopped at the test's cleanup; on failure it is not running.
func (e *Engine) run(t T, bin string, args, env []string, seeded bool) error {
	t.Helper()
	// TRUNCATED on every attempt, so the port-taken check below reads this
	// attempt's log and not the one before it.
	logFile, err := os.Create(e.LogPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = e.Dir
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		_ = logFile.Close()
	}()

	if stillRunning, err := e.waitUntilAnswering(exited, seeded); err != nil {
		if stillRunning {
			_ = cmd.Process.Kill()
			<-exited
		}
		if bytes.Contains(readFile(e.LogPath), []byte("address already in use")) {
			return fmt.Errorf("%w: %w", errPortTaken, err)
		}
		return err
	}

	t.Cleanup(func() {
		e.stop(t, cmd, exited)
		if t.Failed() {
			t.Logf("crewlet engine log (%s):\n%s", e.LogPath, tail(e.LogPath))
		}
	})
	return nil
}

// waitUntilAnswering polls the engine until it is up: answering, or, for a
// seeded engine, ready, which it is once the seed has been applied. An engine
// with no company is never ready (/ready answers 503 "unconfigured") yet
// serves /config, which is what a test of the first apply needs.
//
// "Answering" is an authenticated read of /secrets, not /health, because the
// answer has to come from THIS engine: when another test's engine took the
// port first, it answers /health for everyone, while only this engine accepts
// this engine's token. The read is served before any company exists.
//
// stillRunning reports, on an error, whether the engine has yet to exit.
func (e *Engine) waitUntilAnswering(exited <-chan error, seeded bool) (stillRunning bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	client := e.Client()
	probe := func() error {
		if _, err := client.ListSecrets(ctx); err != nil {
			return err
		}
		if seeded {
			return client.Ready(ctx)
		}
		return nil
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var last error
	for {
		select {
		case err := <-exited:
			return false, fmt.Errorf("the engine exited before it answered: %w", err)
		case <-ctx.Done():
			return true, fmt.Errorf("the engine did not answer within %s; last probe: %w", startTimeout, last)
		case <-tick.C:
			if last = probe(); last == nil {
				return false, nil
			}
		}
	}
}

// stop sends SIGTERM, the engine's graceful shutdown, and kills it if it has not
// stopped within stopTimeout.
func (e *Engine) stop(t T, cmd *exec.Cmd, exited <-chan error) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("stopping the crewlet engine: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(stopTimeout):
		_ = cmd.Process.Kill()
		<-exited
		t.Errorf("the crewlet engine did not stop within %s of SIGTERM and was killed; its log is %s", stopTimeout, e.LogPath)
	}
}

// streamMaxBytes is the embedded broker's declared limit: Crewlet's floor,
// 5 GiB, which its four state logs fit inside. A suite runs many engines at
// once on one filesystem, and an engine with no declared limit takes three
// quarters of the free space it finds at boot, so on a machine with a few
// gibibytes free, or busy writing a build cache, the engine that boots last
// refuses its own logs ("insufficient storage resources available") and
// never applies a document. A declared limit is a reservation the broker
// keeps count of, not a check against the disk, and a test's streams hold
// kilobytes.
const streamMaxBytes = 5 << 30

// tierA is the engine's bootstrap: the embedded broker with its own store, the
// local store and coordination of a single node, an encrypted secret store,
// and the API on the loopback interface with two tokens. Every credential is a
// ${VAR} the engine resolves from the environment Start gives it, so none is
// written to disk.
func tierA(port int) []byte {
	return fmt.Appendf(nil, `logging:
  level: info
  format: json
stream:
  type: embedded
  store_dir: ./data/stream
  store_max_bytes: %d
store:
  path: ./data/company.db
coordination:
  type: local
api:
  host: 127.0.0.1
  port: %d
  auth:
    tokens:
      - id: %s
        token: "${ENGINETEST_OPERATOR_TOKEN}"
      - id: %s
        token: "${ENGINETEST_ACCESS_TOKEN}"
secrets:
  active_key_id: enginetest
  keys:
    - id: enginetest
      material: "${ENGINETEST_SECRET_KEY}"
retention:
  backup_owner: enginetest
`, streamMaxBytes, port, OperatorTokenID, AccessTokenID)
}

// Binary returns the crewlet binary a test runs: the one BinaryEnv names, or
// bin/crewlet at the root of the module, which `make crewlet` builds at the
// pinned version. It fails the test, with what to do, when there is none:
// never a skip, because a suite against the engine that did not run has not
// passed. Start uses it; a suite that runs another crewlet subcommand, such
// as `crewlet validate`, calls it directly so both find the same engine.
func Binary(t T) string {
	t.Helper()
	if bin := os.Getenv(BinaryEnv); bin != "" {
		// LookPath rather than Stat: it also refuses a file that is not
		// executable, which would otherwise fail later as a start error.
		path, err := exec.LookPath(bin)
		if err != nil {
			t.Fatalf("%s=%s: %v; unset it to use bin/crewlet, or point it at a crewlet binary", BinaryEnv, bin, err)
		}
		return path
	}
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("finding bin/crewlet: %v; set %s to a crewlet binary", err, BinaryEnv)
	}
	bin, err := exec.LookPath(filepath.Join(root, "bin", "crewlet"))
	if err != nil {
		t.Fatalf("no crewlet engine binary (%v): run `make crewlet` to build the pinned version, or set %s", err, BinaryEnv)
	}
	return bin
}

// moduleRoot is the nearest directory at or above the working directory that
// holds a go.mod: a test's working directory is its package's, inside the
// module.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the working directory")
		}
		dir = parent
	}
}

// freePort returns a loopback port nothing was listening on a moment ago.
func freePort(t T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	return port
}

func randomToken(t T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating an API token: %v", err)
	}
	return hex.EncodeToString(b)
}

// randomKey is a keyring key in the form `crewlet secrets keygen` prints:
// 32 random bytes, base64.
func randomKey(t T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generating a keyring key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func writeFile(t T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func readFile(path string) []byte {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Appendf(nil, "(reading %s: %v)", path, err)
	}
	return data
}

// tailBytes is how much of the engine's log a failure prints: the end, where
// the reason is, without burying the test's own output under a boot log.
const tailBytes = 32 << 10

func tail(path string) string {
	data := readFile(path)
	if len(data) <= tailBytes {
		return string(data)
	}
	data = data[len(data)-tailBytes:]
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	return "...\n" + strings.TrimLeft(string(data), "\n")
}
