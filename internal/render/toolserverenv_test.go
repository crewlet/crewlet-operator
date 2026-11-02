package render

import (
	"bufio"
	"bytes"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// toolServerShell is the stdio tool server the environment contract starts:
// a shell that writes its environment to the file its one argument names, by
// a rename so that the file is never read half written, and then reads its
// input until the engine closes it. It never answers MCP, so the engine gives
// it up and starts it again later, and each start writes the same file.
var toolServerShell = []string{"-c", `env > "$0.part" && mv "$0.part" "$0"; exec cat >/dev/null`}

// The variables the environment contract's servers declare, and which of the
// two sets a value: the server's own env or the seat's.
const (
	envHTTPSProxy  = "HTTPS_PROXY"
	envProbeServer = "PROBE_SERVER"
	envProbeSeat   = "PROBE_SEAT"
	envProbeBoth   = "PROBE_BOTH"
	setByServer    = "server"
	setBySeat      = "seat"
)

// shellOwnEnv is the names /bin/sh sets in its environment by itself, which
// no engine handed it: dash adds PWD, and a bash in sh mode SHLVL and _ too.
// It is measured, by starting the shell with an empty environment, rather
// than listed, so that a machine whose sh is another shell does not fail the
// contract over the shell's own variables.
func shellOwnEnv(t *testing.T) map[string]bool {
	t.Helper()
	file := filepath.Join(t.TempDir(), "env")
	//nolint:gosec // G204: the arguments are this file's own constant script and a temporary path.
	cmd := exec.CommandContext(t.Context(), "/bin/sh", append(slices.Clone(toolServerShell), file)...)
	cmd.Env = []string{}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running /bin/sh with an empty environment: %v\n%s", err, out)
	}
	own := map[string]bool{}
	for name := range envFile(t, file) {
		own[name] = true
	}
	return own
}

// envFile reads what the shell of toolServerShell wrote: one NAME=VALUE per
// line. None of the values this test sets holds a newline.
func envFile(t *testing.T, file string) map[string]string {
	t.Helper()
	//nolint:gosec // G304: the file is one this test's own server wrote in its temporary directory.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		if name, value, ok := strings.Cut(lines.Text(), "="); ok {
			env[name] = value
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return env
}

// TestContractAToolServerGetsTheAllowlistAndItsEnvAlone holds what the
// tool-servers guide (docs/guides/tool-servers.md, Stdio and HTTP), the
// MCPServer API's env and the class's and the Company's env say a stdio tool
// server is handed to the engine that hands it. It runs the engine as the
// operator configures a data pod, with variables in its environment that the
// class's podTemplate.spec.env or the Company's runtime.env would set: proxy,
// TLS and locale variables, which Crewlet's allowlist names, and two it does
// not name, GLAB_CHECK_UPDATE, which the guide once set with the image's ENV,
// and an address of the platform's own. The company it applies has a shared
// server and a per-seat one, each declaring an env of its own that sets one
// of the allowlisted variables again, and the seat gives the per-seat one a
// value its server sets too. Each server, as the engine starts it, holds
// PATH, HOME and every allowlisted variable as the engine has them, its own
// env over them and the seat's over that, and nothing else of the engine's
// environment: not the two the list does not name, and not the pod's own
// CREWLET_NODE_ID either.
//
// The engine starts a shared server at the apply that first declares it (a
// later apply restarts it only when it changes it), and a per-seat one once
// it holds the seat, so a running engine shows the decision: the test holds
// the engine rather than a copy of its code.
func TestContractAToolServerGetsTheAllowlistAndItsEnvAlone(t *testing.T) {
	const (
		engineProxy = "http://proxy.engine.invalid:3128"
		serverProxy = "http://proxy.server.invalid:3128"
	)
	// What the engine's environment holds besides PATH, HOME and the pod's
	// own variables: listed, the variables Crewlet's allowlist names, which a
	// tool server is handed, and unlisted, two it does not.
	listed := map[string]string{
		"HTTP_PROXY":          engineProxy,
		envHTTPSProxy:         engineProxy,
		"https_proxy":         engineProxy,
		"NO_PROXY":            "127.0.0.1,localhost",
		"SSL_CERT_DIR":        t.TempDir(),
		"REQUESTS_CA_BUNDLE":  "/etc/engine-ca/bundle.pem",
		"NODE_EXTRA_CA_CERTS": "/etc/engine-ca/extra.pem",
		"LANG":                "C.UTF-8",
	}
	unlisted := map[string]string{
		"GLAB_CHECK_UPDATE": "false",
		"PLATFORM_API_URL":  "https://platform.example.com",
	}
	env := make([]string, 0, len(listed)+len(unlisted))
	for name, value := range listed {
		env = append(env, name+"="+value)
	}
	for name, value := range unlisted {
		env = append(env, name+"="+value)
	}

	dir := t.TempDir()
	sharedFile, seatFile := filepath.Join(dir, "shared.env"), filepath.Join(dir, "seat.env")
	in := companyCaseNamed(t, "minimal-agent").input(t)
	if len(in.Seats) != 1 {
		t.Fatalf("the minimal-agent case has %d seats; this test gives its one seat a server's values", len(in.Seats))
	}
	server := func(name string, shared bool, file string) v1alpha1.MCPServer {
		return v1alpha1.MCPServer{
			Name:      name,
			Namespace: in.Company.Namespace,
			Spec: v1alpha1.MCPServerSpec{
				CompanyRef: v1alpha1.CompanyReference{Name: in.Company.Name},
				Transport:  v1alpha1.MCPTransportStdio,
				Shared:     new(shared),
				Command:    "/bin/sh",
				Args:       append(slices.Clone(toolServerShell), file),
				Env: []v1alpha1.MCPEnvVar{
					{Name: envProbeServer, Value: new(name)},
					{Name: envProbeBoth, Value: new(setByServer)},
					{Name: envHTTPSProxy, Value: new(serverProxy)},
				},
				// It never answers MCP: the engine gives it up at once
				// rather than after Crewlet's default of 120 seconds.
				StartupTimeoutSeconds: 1,
			},
		}
	}
	in.MCPServers = append(in.MCPServers, server("probe-shared", true, sharedFile), server("probe-seat", false, seatFile))
	in.Seats[0].Spec.MCP = append(in.Seats[0].Spec.MCP, v1alpha1.MCPCredentials{
		Name: "probe-seat",
		Env: []v1alpha1.MCPCredentialValue{
			{Name: envProbeSeat, Value: new(setBySeat)},
			{Name: envProbeBoth, Value: new(setBySeat)},
		},
	})
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}

	e, _ := startDataPod(t, bootstrapCases()[0].input(t), env...)
	client := e.Client()
	for _, k := range doc.Refs {
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(dummyCredential(k))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, CreateOnly: true, Summary: "a tool server's environment"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}

	// The engine's PATH and HOME are enginetest's: the test's own PATH, and
	// the engine's directory.
	handed := map[string]string{"PATH": os.Getenv("PATH"), "HOME": e.Dir}
	maps.Copy(handed, listed)
	want := map[string]map[string]string{sharedFile: maps.Clone(handed), seatFile: maps.Clone(handed)}
	maps.Copy(want[sharedFile], map[string]string{envProbeServer: "probe-shared", envProbeBoth: setByServer, envHTTPSProxy: serverProxy})
	maps.Copy(want[seatFile], map[string]string{
		envProbeServer: "probe-seat", envProbeSeat: setBySeat, envProbeBoth: setBySeat, envHTTPSProxy: serverProxy,
	})

	own := shellOwnEnv(t)
	for _, file := range []string{sharedFile, seatFile} {
		// A seat's server starts once the engine holds the seat, a moment
		// after the apply; a shared one during it.
		for deadline := time.Now().Add(2 * time.Minute); ; {
			if _, err := os.Stat(file); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("the engine never started the server that writes %s", file)
			}
			time.Sleep(200 * time.Millisecond)
		}
		got := envFile(t, file)
		maps.DeleteFunc(got, func(name, _ string) bool { return own[name] && !has(want[file], name) })
		for _, name := range slices.Sorted(maps.Keys(want[file])) {
			if value, ok := got[name]; !ok {
				t.Errorf("%s: the server was not handed %s", file, name)
			} else if value != want[file][name] {
				t.Errorf("%s: the server was handed %s=%q, want %q", file, name, value, want[file][name])
			}
		}
		for _, name := range slices.Sorted(maps.Keys(got)) {
			if !has(want[file], name) {
				t.Errorf("%s: the server was handed %s=%q, which is neither on Crewlet's allowlist nor declared", file, name, got[name])
			}
		}
	}
}

// has reports whether m holds the key name.
func has(m map[string]string, name string) bool {
	_, ok := m[name]
	return ok
}

// TestContractAnOptionalKeyIsLeftOut holds what the MCPServer API says of
// optionalKeys to the engine that starts the server: a key whose value is
// read from a Secret the secret sync pushed nothing for, as it pushes nothing
// for an optional key whose Secret or key does not exist, or from one that
// holds an empty value, is left out of the server's environment, whole, and
// the server starts without it; the server's own literal and the values that
// are there reach it as written. It holds a shared server's own env and a
// per-seat server's values a Seat gives it alike, as the engine starts each
// with the rendered document.
func TestContractAnOptionalKeyIsLeftOut(t *testing.T) {
	const (
		present = "PROBE_PRESENT"
		missing = "PROBE_MISSING"
		empty   = "PROBE_EMPTY"
		setting = "PROBE_SETTING"
		seatOwn = "PROBE_SEAT_TOKEN"
		seatGap = "PROBE_SEAT_MISSING"
		secret  = "probe"
	)
	dir := t.TempDir()
	sharedFile, seatFile := filepath.Join(dir, "shared.env"), filepath.Join(dir, "seat.env")
	in := companyCaseNamed(t, "minimal-agent").input(t)
	fromSecret := func(key string) v1alpha1.SecretValueSource {
		return v1alpha1.SecretValueSource{SecretKeyRef: v1alpha1.SecretKeyReference{Name: secret, Key: key}}
	}
	server := func(name string, shared bool, file string) v1alpha1.MCPServer {
		return v1alpha1.MCPServer{
			Name:      name,
			Namespace: in.Company.Namespace,
			Spec: v1alpha1.MCPServerSpec{
				CompanyRef:            v1alpha1.CompanyReference{Name: in.Company.Name},
				Transport:             v1alpha1.MCPTransportStdio,
				Shared:                new(shared),
				Command:               "/bin/sh",
				Args:                  append(slices.Clone(toolServerShell), file),
				Env:                   []v1alpha1.MCPEnvVar{{Name: setting, Value: new(name)}},
				StartupTimeoutSeconds: 1,
			},
		}
	}
	shared := server("probe-shared", true, sharedFile)
	shared.Spec.Env = append(shared.Spec.Env,
		v1alpha1.MCPEnvVar{Name: present, ValueFrom: fromSecret("present")},
		v1alpha1.MCPEnvVar{Name: missing, ValueFrom: fromSecret("missing")},
		v1alpha1.MCPEnvVar{Name: empty, ValueFrom: fromSecret("empty")})
	shared.Spec.OptionalKeys = []string{present, missing, empty}
	perSeat := server("probe-seat", false, seatFile)
	perSeat.Spec.OptionalKeys = []string{seatGap}
	in.MCPServers = append(in.MCPServers, shared, perSeat)
	in.Seats[0].Spec.MCP = append(in.Seats[0].Spec.MCP, v1alpha1.MCPCredentials{
		Name: perSeat.Name,
		Env: []v1alpha1.MCPCredentialValue{
			{Name: seatOwn, ValueFrom: fromSecret("seat-token")},
			{Name: seatGap, ValueFrom: fromSecret("seat-missing")},
		},
	})
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}

	e, _ := startDataPod(t, bootstrapCases()[0].input(t))
	client := e.Client()
	// What the secret sync leaves in the store: nothing for a Secret key
	// that does not exist, under an optional key, and the empty value a
	// Secret key holds as it is.
	values := map[string]string{"empty": ""}
	for _, k := range doc.Refs {
		value, set := values[k.Key]
		if !set {
			value = dummyCredential(k)
		}
		if k.Name == secret && (k.Key == "missing" || k.Key == "seat-missing") {
			continue
		}
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, CreateOnly: true, Summary: "optional keys"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}

	probe := func(key string) string {
		return dummyCredential(secretref.Key{Namespace: testNamespace, Name: secret, Key: key})
	}
	want := map[string]map[string]string{
		sharedFile: {setting: shared.Name, present: probe("present")},
		seatFile:   {setting: perSeat.Name, seatOwn: probe("seat-token")},
	}
	gone := map[string][]string{sharedFile: {missing, empty}, seatFile: {seatGap}}
	for _, file := range []string{sharedFile, seatFile} {
		for deadline := time.Now().Add(2 * time.Minute); ; {
			if _, err := os.Stat(file); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatalf("the engine never started the server that writes %s: an optional key held it back", file)
			}
			time.Sleep(200 * time.Millisecond)
		}
		got := envFile(t, file)
		for name, value := range want[file] {
			if got[name] != value {
				t.Errorf("%s: the server was handed %s=%q, want %q", file, name, got[name], value)
			}
		}
		for _, name := range gone[file] {
			if value, ok := got[name]; ok {
				t.Errorf("%s: the server was handed %s=%q, which its optionalKeys leaves out without a value", file, name, value)
			}
		}
	}
}
