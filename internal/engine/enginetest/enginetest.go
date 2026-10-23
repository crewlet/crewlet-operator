// Package enginetest runs a real Crewlet engine for a test: the crewlet binary
// every suite runs, on a free loopback port, with a throwaway Tier A of its
// own in the test's temporary directory. It also reads that engine's module
// source, for a test that holds a copy of the engine's code to it.
//
// The binary is $CREWLET_BIN, or bin/crewlet under the module root, which
// `make crewlet` installs from the crewlet module at the commit the engine
// image every suite runs (the Makefile's CREWLET_IMG, crewlet's main as
// ghcr.io publishes it) was built from, or at CREWLET_VERSION when that names
// another. A test that needs it fails, rather than skips, when it is missing:
// a skipped contract test is indistinguishable from a passing one in a CI
// summary, and this suite is what tells the operator and the engine apart
// when they drift. ADR-0002
// (adr/0002-test-against-crewlet-main-and-fail-without-it.md) records the
// decision for every package that tests against the engine.
package enginetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
)

// BinaryEnv names the environment variable that overrides where the crewlet
// binary is found.
const BinaryEnv = "CREWLET_BIN"

// The ids of the engine's two tokens, the ones the operator renders into
// every engine's Tier A. Crewlet records each write made with one under its
// login, engine.TokenLogin(id): a revision's created_by, a stored secret's
// updated_by and operator_id, a seat pause's paused_by.
const (
	// OperatorTokenID is the id of the token the operator holds. It is the
	// id the operator gives its own token in production (render's
	// TokenOperator, which this package cannot import: render's own tests
	// import this one, and render holds the two equal), because the
	// controllers recognise their own writes by it: a test engine that named
	// it otherwise would have every write the operator makes read as
	// somebody else's.
	OperatorTokenID = "operator"

	// AccessTokenID is the id of a second token, standing for a person or a
	// control plane that reaches the same engine: a test of attribution or
	// of concurrent writers needs two credentials the engine tells apart.
	// It is render's TokenAccess, the token the operator publishes.
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

// AddPerson puts a person with the address email on the human seat seat of
// the company the engine runs, as an admin of the company does in its
// dashboard or with `crewlet iam create`: POST /iam/people with the operator
// token, which this engine grants admin. The engine refuses it until it runs
// a company that has seat as a human seat nobody holds, so a test applies
// one and waits for Ready first. The person holds no grant: what a test of a
// held seat needs is somebody on it, not somebody who can do anything.
//
// It returns the person's id once the engine's identity directory answers
// for them, which is when a write that takes the seat away is refused. The
// engine's answer to the create also carries the person's first password
// link, a credential, which is neither kept nor printed.
func (e *Engine) AddPerson(t T, email, seat string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"kind": "person", "email": email, "seat": seat, "grants": []string{}})
	if err != nil {
		t.Fatalf("encoding a person: %v", err)
	}
	status, answer, err := e.call(http.MethodPost, "/iam/people", body)
	if err != nil {
		t.Fatalf("POST /iam/people: %v", err)
	}
	// 202 is a create the engine has made durable and not yet applied
	// here, which the read below waits out. Any other answer is a
	// refusal, which carries no credential and is printed whole.
	if status != http.StatusCreated && status != http.StatusAccepted {
		t.Fatalf("POST /iam/people answered %d: %s", status, answer)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answer, &created); err != nil || created.ID == "" {
		t.Fatalf("POST /iam/people answered %d with no person id", status)
	}
	deadline := time.Now().Add(startTimeout)
	for {
		status, answer, err := e.call(http.MethodGet, "/iam/people/"+created.ID, nil)
		if err == nil && status == http.StatusOK {
			return created.ID
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine's directory does not answer for person %s after %s: %d %s %v",
				created.ID, startTimeout, status, answer, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// call sends one request to the engine with the operator token and returns
// its status and body.
func (e *Engine) call(method, path string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), engine.DefaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, e.URL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.OperatorToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	return resp.StatusCode, answer, err
}

// Option configures Start.
type Option func(*options)

type options struct {
	company []byte
	tierA   *tierAOption
	writers []string
	node    *nodeOption
}

// nodeOption is the node block of enginetest's own Tier A (WithNode).
type nodeOption struct {
	id     string
	labels map[string]string
}

// tierAOption is a Tier A of the caller's (WithTierA).
type tierAOption struct {
	operator, access string
	render           func(port int, dir string) []byte
	env              []string
}

// WithTierA runs the engine on a Tier A of the caller's instead of
// enginetest's own: render returns it for the port Start chose and the
// engine's working directory, and it must serve the API on 127.0.0.1 at that
// port with operator and access as the tokens of OperatorTokenID and
// AccessTokenID. env is the engine's environment beyond PATH and HOME,
// exactly; none of enginetest's own variables is set. It is how a test runs
// the engine as the operator configures a data pod.
func WithTierA(operator, access string, render func(port int, dir string) []byte, env ...string) Option {
	return func(o *options) {
		o.tierA = &tierAOption{operator: operator, access: access, render: render, env: env}
	}
}

// WithCompanyWriters makes the company document managed: enginetest's Tier A
// names ids as api.auth.company_writers, the token ids that alone may change
// the document, and the engine refuses every other credential a change with
// 403 config_managed. Its reads, a reload and the secret store stay open to
// both tokens, which hold admin. The engine ignores a WithCompany seed when
// the document is managed, so the two do not combine; nor does WithTierA,
// whose Tier A is the caller's to write, list included.
func WithCompanyWriters(ids ...string) Option {
	return func(o *options) { o.writers = ids }
}

// WithNode names the engine's node and gives it labels, the node block of
// enginetest's own Tier A, so that a test can run the engine as the node a
// rendered document places its seats on: a seat whose role.placement asks for
// a label the node does not carry is one the engine never runs. Without it the
// node has the engine's default id and no label. It does not combine with
// WithTierA, whose Tier A, node block included, is the caller's.
func WithNode(id string, labels map[string]string) Option {
	return func(o *options) { o.node = &nodeOption{id: id, labels: labels} }
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

	if o.company != nil && len(o.writers) > 0 {
		t.Fatalf("enginetest: WithCompany and WithCompanyWriters do not combine: a managed engine ignores its -company seed")
	}
	if o.tierA != nil && len(o.writers) > 0 {
		t.Fatalf("enginetest: WithTierA and WithCompanyWriters do not combine: " +
			"the engine runs the caller's Tier A, so the writers go in it as api.auth.company_writers")
	}
	if o.tierA != nil && o.node != nil {
		t.Fatalf("enginetest: WithTierA and WithNode do not combine: " +
			"the engine runs the caller's Tier A, so the node goes in it as its node block")
	}
	render := func(port int) []byte { return tierA(port, o.writers, o.node) }
	if custom := o.tierA; custom != nil {
		e.OperatorToken, e.AccessToken = custom.operator, custom.access
		env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}, custom.env...)
		render = func(port int) []byte { return custom.render(port, dir) }
	}

	for attempt := 1; ; attempt++ {
		port := freePort(t)
		writeFile(t, filepath.Join(dir, "crewlet.yaml"), render(port))
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

// streamMaxBytes is the embedded broker's declared limit: 9 GiB, the share
// the render gives the smallest data volume an EngineClass that keeps its
// files in the stream's bucket admits (three quarters of 12Gi), as every
// engine here does. A fresh node reserves at least 9305762474 bytes of it
// whatever it runs (Crewlet's engine.BrokerFloor: its seven state logs'
// ceilings at their smallest and the object store's bucket, a quarter of the
// limit) and refuses to boot on less, and what is left holds the mailboxes
// and the coordination buckets. A suite runs many engines at
// once on one filesystem, and an engine with no declared limit takes three
// quarters of the free space it finds at boot, so on a machine with a few
// gibibytes free, or busy writing a build cache, the engine that boots last
// refuses its own logs ("insufficient storage resources available") and
// never applies a document. A declared limit is a reservation the broker
// keeps count of, not a check against the disk, and a test's streams hold
// kilobytes.
const streamMaxBytes = 9 << 30

// tierA is the engine's bootstrap: the embedded broker with its own store, the
// local store and coordination of a single node, an encrypted secret store,
// and the API on the loopback interface with two tokens. Every credential is a
// ${VAR} the engine resolves from the environment Start gives it, so none is
// written to disk. writers, when there are any, are api.auth.company_writers.
//
// Crewlet asks a node that serves the API three things beyond its port, and
// refuses to start without them. api.external_url is where a browser reaches
// the engine: the loopback address the API binds, which is also where a test
// reaches it, and which Crewlet reads as loopback, so neither its plain-http
// advisory nor its trusted-proxies one fires. api.auth.admin is serve, since
// the operator's own calls are admin's. Each token states its grants: the
// operator's token is admin because every route the operator calls (the
// company document, the secret store, the fleet, the integrations and the
// pause tools) is admin's in Crewlet's authority table, and admin cannot be
// split; the access token stands for the token the operator publishes to a
// company's consumers, which the operator renders with admin too, and a test
// of the managed document needs it to reach the refusal it tests
// (config_managed) rather than be refused at the route for lack of a grant.
func tierA(port int, writers []string, node *nodeOption) []byte {
	var companyWriters string
	if len(writers) > 0 {
		companyWriters = "    company_writers: [" + strings.Join(writers, ", ") + "]\n"
	}
	var nodeBlock strings.Builder
	if node != nil {
		// Quoted, as JSON strings are YAML strings: a label value such as
		// "true" or "1" stays a string.
		fmt.Fprintf(&nodeBlock, "node:\n  id: %q\n", node.id)
		if len(node.labels) > 0 {
			nodeBlock.WriteString("  labels:\n")
			for _, k := range slices.Sorted(maps.Keys(node.labels)) {
				fmt.Fprintf(&nodeBlock, "    %q: %q\n", k, node.labels[k])
			}
		}
	}
	return fmt.Appendf(nil, `%slogging:
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
  port: %[3]d
  external_url: http://127.0.0.1:%[3]d
  auth:
    admin: serve
    tokens:
      - id: %[4]s
        token: "${ENGINETEST_OPERATOR_TOKEN}"
        grants: [admin]
      - id: %[5]s
        token: "${ENGINETEST_ACCESS_TOKEN}"
        grants: [admin]
%[6]ssecrets:
  active_key_id: enginetest
  keys:
    - id: enginetest
      material: "${ENGINETEST_SECRET_KEY}"
retention:
  backup_owner: enginetest
`, nodeBlock.String(), streamMaxBytes, port, OperatorTokenID, AccessTokenID, companyWriters)
}

// Binary returns the crewlet binary a test runs: the one BinaryEnv names, or
// bin/crewlet at the root of the module, which `make crewlet` installs. It
// fails the test, with what to do, when there is none:
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
		t.Fatalf("no crewlet engine binary (%v): run `make crewlet` to install the engine every suite runs, or set %s",
			err, BinaryEnv)
	}
	return bin
}

// Module is the Go module the crewlet binary is built from.
const Module = "github.com/crewlet/crewlet"

// Source returns the directory of the crewlet module source the binary Binary
// returns was built from: the module zip the Go command downloads, and checks
// against the checksum database, for the version the binary's build info
// names, which `make crewlet` has already put in the module cache. A test
// reads it for what the engine decides where no validation shows it, such as
// what signs a coding agent in at a launch. A binary built from a checkout
// names no version, and fails the test, as a source that cannot be had does.
func Source(t T) string {
	t.Helper()
	version := Version(t)
	cmd := exec.Command("go", "mod", "download", "-json", Module+"@"+version)
	// Outside any module, so that no go.mod or go.work of the caller's
	// decides what is downloaded.
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var module struct{ Dir, Error string }
	if err := json.Unmarshal(stdout.Bytes(), &module); err != nil || module.Dir == "" {
		t.Fatalf("go mod download %s@%s gave no source directory (%v, %v, %s): %s%s",
			Module, version, runErr, err, module.Error, stdout.String(), stderr.String())
	}
	return module.Dir
}

// Version returns the version of the crewlet module the binary Binary returns
// was built from, as its build info names it: for the binary `make crewlet`
// installs, the pseudo-version of the crewlet commit it installed, which `go
// install` stamps. A binary whose source no module version is fails the test
// (ModuleVersion).
func Version(t T) string {
	t.Helper()
	bin := Binary(t)
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading the build info of %s: %v", bin, err)
	}
	version, err := ModuleVersion(info)
	if err != nil {
		t.Fatalf("%s: %v", bin, err)
	}
	return version
}

// ModuleVersion is the version of the crewlet module whose source a binary of
// build info info was built from, or why no version is.
//
// A binary built in a crewlet checkout names a version too, since the go
// command stamps one from the checkout's commit, and one with uncommitted
// changes carries "+dirty" after it. The module proxy serves that version as
// the commit alone, without the changes: Source would hand a test the code
// the binary does not run, and a test holding the operator's copies to the
// engine's source would pass, or fail, against the wrong engine. So such a
// version is refused, as a build with no version at all is.
func ModuleVersion(info *buildinfo.BuildInfo) (string, error) {
	version := info.Main.Version
	switch {
	case info.Main.Path != Module || version == "" || version == "(devel)":
		return "", fmt.Errorf("it is %s %s, not a module version of %s: install it with `make crewlet`",
			info.Main.Path, version, Module)
	case strings.HasSuffix(version, "+dirty"):
		return "", fmt.Errorf("it is %s %s, built from a checkout with uncommitted changes, whose source is "+
			"no module version: commit them and push the commit, then run `make crewlet CREWLET_VERSION=<commit>`",
			Module, version)
	}
	return version, nil
}

// Image is the engine image every suite runs unless CREWLET_IMG names
// another: the Makefile's CREWLET_MAIN_IMG, crewlet's main as ghcr.io
// publishes it, which is CREWLET_IMG's default. A sample or a guide that
// names an engine image names this one, so a reader runs the engine the
// suites ran, and a test of either holds it here. It is read from the
// Makefile, so the image the suites run is named in one place.
func Image(t T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("finding the Makefile: %v", err)
	}
	module, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("opening the module root: %v", err)
	}
	defer func() { _ = module.Close() }()
	makefile, err := module.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	m := makefileImage.FindSubmatch(makefile)
	if m == nil {
		t.Fatalf("the Makefile names no CREWLET_MAIN_IMG (a line `CREWLET_MAIN_IMG := <image>`)")
	}
	return string(m[1])
}

// makefileImage is the Makefile's line that names crewlet's main image.
var makefileImage = regexp.MustCompile(`(?m)^CREWLET_MAIN_IMG := (\S+)$`)

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
