package render

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
	"github.com/crewlet/crewlet-operator/test/docs"
)

// The contract tests hand every rendered document to the engine's own
// validator, `crewlet validate`, from the engine build the operator is
// tested against. They are what keeps the renderer and Crewlet from drifting
// apart: a field Crewlet renamed or a rule it added fails here, not in a
// cluster.

// validation is what `crewlet validate -json` prints.
type validation struct {
	Valid    bool              `json:"valid"`
	Problems []json.RawMessage `json:"problems"`
	Warnings []json.RawMessage `json:"warnings"`
}

// validate runs `crewlet validate -json args...` with exactly env as its
// environment (and PATH and HOME), and fails the test on any problem, on any
// reference the environment did not answer, and on any warning but the
// advisories the case expects, each of which must be given: an unresolved
// reference is only a warning to Crewlet, and a missing variable would be a
// pod that boots with an empty token.
func validate(t *testing.T, env map[string]string, want []advisory, args ...string) {
	t.Helper()
	got, stdout, stderr, err := verdict(t, env, args...)
	if !got.Valid || len(got.Problems) > 0 || err != nil {
		t.Errorf("crewlet validate refused the document (exit %v):\n%s\n%s", err, stdout, stderr)
	}
	// A CLI's env is resolved where the engine builds the provider, and
	// one that resolves to nothing is only logged there too: for a Hermes
	// signed in through its env, that is a CLI with no key.
	for _, unresolved := range []string{"config_reference_unresolved", "cli_agent_env_unresolved"} {
		if strings.Contains(stderr, unresolved) {
			t.Errorf("a reference resolved to nothing:\n%s", stderr)
		}
	}
	unexpected, missing := advisoriesOf(got.Warnings, want)
	for _, w := range unexpected {
		t.Errorf("crewlet validate warns: %s", w)
	}
	for _, a := range missing {
		t.Errorf("crewlet validate gave no advisory at %s saying %q; it warned %s", a.path, a.says, got.Warnings)
	}
}

// advisory is a warning Crewlet's validator is expected to give a document:
// an advisory (its kind for a setting that is valid and has a consequence),
// at path, whose sentence says says.
//
// A warning is a document Crewlet runs but doubts: a lead or a manages entry
// naming nobody, a backup nobody owns. Every rendered document is meant to
// be one Crewlet has nothing to say about, except for the advisories it
// gives about a choice that is the tenant's or the platform's to make, which
// no render can avoid. A case names each it expects, and the gate holds the
// engine to exactly those, both ways: an advisory no case expected is a
// document Crewlet doubts, and an expected one it stopped giving is a rule
// that moved under the operator's docs, which say what each means.
type advisory struct{ path, says string }

// advisoryNoHumanSeat is Crewlet's advisory on a company that declares no
// `kind: human` seat (config.peoplelessWarnings): nobody can be invited into
// it or created in it. Crewlet calls it "a warning and not a rule … a
// company its agents run alone", which is what an agent-only chart of a
// tenant's is.
var advisoryNoHumanSeat = advisory{path: keyRoles, says: "no `kind: human` seats"}

// advisoriesOf sorts what the validator warned against what a case expects:
// the warnings no expected advisory accounts for, and the expected advisories
// no warning gave. An expected advisory accounts for one warning of kind
// advisory, at its path, whose message says what it says.
func advisoriesOf(warnings []json.RawMessage, want []advisory) (unexpected []string, missing []advisory) {
	missing = slices.Clone(want)
	for _, raw := range warnings {
		var w struct{ Kind, Path, Message string }
		if json.Unmarshal(raw, &w) != nil {
			unexpected = append(unexpected, string(raw))
			continue
		}
		i := slices.IndexFunc(missing, func(a advisory) bool {
			return w.Kind == "advisory" && w.Path == a.path && strings.Contains(w.Message, a.says)
		})
		if i < 0 {
			unexpected = append(unexpected, string(raw))
			continue
		}
		missing = slices.Delete(missing, i, i+1)
	}
	return unexpected, missing
}

// verdict runs `crewlet validate -json args...` as validate describes and
// returns what it printed, its exit error included, for a test that
// expects a refusal.
func verdict(t *testing.T, env map[string]string, args ...string) (validation, string, string, error) {
	t.Helper()
	//nolint:gosec // G204: the binary is the engine under test, chosen by whoever runs the tests.
	cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), append([]string{"validate", "-json"}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + cmd.Dir}
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("running crewlet validate: %v", err)
	}

	var got validation
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("crewlet validate printed no verdict (%v, exit %v):\n%s\n%s", jsonErr, err, stdout.String(), stderr.String())
	}
	return got, stdout.String(), stderr.String(), err
}

// write puts data in a file of the test's own and returns its path.
func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// bootstrapEnv answers every variable a Tier A's pod is given: the value the
// pod is given, or for its name, which the Downward API gives it,
// nimbus-data-0.
func bootstrapEnv(doc *BootstrapDocument) map[string]string {
	env := map[string]string{}
	for _, e := range doc.Env {
		env[e.Name] = cmp.Or(e.Value, "nimbus-data-0")
	}
	return env
}

// startDataPod runs the engine as the operator configures a data pod: on the
// crewlet.yaml it renders, with the test's PATH and a HOME in the engine's
// directory (enginetest's), the environment the pod gives it (bootstrapEnv)
// and env, and nothing else. The pod's paths move into the engine's
// directory and its API onto the loopback interface. The public listener is
// dropped: its port is fixed, an engine another package's suite runs
// meanwhile may hold it, and nothing a contract test asks reaches it.
func startDataPod(t *testing.T, in *BootstrapInput, env ...string) (*enginetest.Engine, *BootstrapDocument) {
	t.Helper()
	tierA, err := Bootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	pod := make([]string, 0, len(tierA.Env)+len(env))
	for name, value := range bootstrapEnv(tierA) {
		pod = append(pod, name+"="+value)
	}
	e := enginetest.Start(t, enginetest.WithTierA(in.Tokens.Operator, in.Tokens.Access, func(port int, dir string) []byte {
		var doc map[string]any
		if err := yaml.Unmarshal(tierA.YAML, &doc); err != nil {
			t.Fatal(err)
		}
		api := doc["api"].(map[string]any)
		api["host"], api["port"] = "127.0.0.1", port
		// Fixed ports every case would share.
		delete(api, "public")
		delete(api, "artifacts")
		doc["store"].(map[string]any)["path"] = filepath.Join(dir, "store", "crewlet.db")
		doc["stream"].(map[string]any)["store_dir"] = filepath.Join(dir, "stream")
		out, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}, append(pod, env...)...))
	return e, tierA
}

// TestContractBootstrap validates every Tier A case on its own.
func TestContractBootstrap(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			validate(t, bootstrapEnv(doc), c.advisories, "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
		})
	}
}

// TestContractSeatPod validates the Tier A of every kind of seat pod on its
// own: a Shared pool's, a PerSeat seat's (its seat label a reference the
// pod's environment answers), and one naming an S3 object store. The engine
// refuses a node without the data role whose store is not scratch, that
// keeps a stream of its own, that is a member rather than a leaf, or that
// runs ingress or workers.
func TestContractSeatPod(t *testing.T) {
	for _, c := range seatPodCases() {
		t.Run(c.name, func(t *testing.T) {
			in, _ := c.input(t)
			doc, err := SeatPodBootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			validate(t, bootstrapEnv(doc), nil, "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
		})
	}
}

// TestContractRefusesListenersThatCannotOpen proves the gate that keeps the
// data pod's listeners apart is live: a Tier A whose public listener shares
// a port with the API or the leaf listener, or that opens it beside no API
// port on an ingress node, is refused by the engine, so a change of
// PublicPort, APIPort or LeafPort that let two meet would fail
// TestContractBootstrap rather than a pod at boot. No object the operator
// reads sets any of the three, which is why nothing is checked at render.
func TestContractRefusesListenersThatCannotOpen(t *testing.T) {
	// The case whose class allows seat pools, whose data pod opens the
	// leaf listener too.
	var in *BootstrapInput
	for _, c := range bootstrapCases() {
		if candidate := c.input(t); SeatPoolsAllowed(candidate.Class) {
			in = candidate
		}
	}
	if in == nil {
		t.Fatal("no bootstrap case allows seat pools")
	}
	doc, err := Bootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.YAML), "leaf:") {
		t.Fatalf("the seat-pools case opens no leaf listener:\n%s", doc.YAML)
	}
	// Each case moves one port in the parsed document rather than in its
	// text, which no longer holds a listener's port at a fixed place once
	// the block carries an address beside it.
	public := func(api map[string]any) map[string]any { return api["public"].(map[string]any) }
	for _, c := range []struct {
		name   string
		mutate func(api map[string]any)
	}{
		{"public on the API port", func(api map[string]any) { public(api)["port"] = APIPort }},
		{"public on the leaf port", func(api map[string]any) { public(api)["port"] = LeafPort }},
		{"public beside no API port", func(api map[string]any) { api["port"] = 0 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var tierA map[string]any
			if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
				t.Fatal(err)
			}
			api, ok := tierA["api"].(map[string]any)
			if _, open := api["public"].(map[string]any); !ok || !open {
				t.Fatalf("the rendered Tier A opens no public listener:\n%s", doc.YAML)
			}
			c.mutate(api)
			mutated, err := yaml.Marshal(tierA)
			if err != nil {
				t.Fatal(err)
			}
			got, stdout, _, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", mutated))
			if got.Valid || !strings.Contains(stdout, "api.public.port") {
				t.Fatalf("crewlet validate did not refuse api.public.port:\n%s\n%s", mutated, stdout)
			}
		})
	}
}

// addressShapes are the addresses TestContractAddressShapes tries in every
// place a data pod's Tier A names one: the shapes Crewlet refuses at start
// (a host a browser rewrites before it sends it: IPv4 short forms, decimal,
// hexadecimal and octal, leading zeros, a trailing dot, a name whose last
// label is a number; a host outside ASCII, written as it is or
// percent-encoded, a NEL or a C1 control character among them; an IPv6
// zone; a port outside 1 to 65535; a second ':' or a bracket outside an
// IPv6 address; a scheme in upper case; a query or a fragment, with or
// without a path; no host) and their neighbours it takes (four decimal
// octets, a name with digits beside letters, punycode, IPv6 with and without
// brackets' port, a port with leading zeros, a path, a NEL or a C1 control
// character in it, a trailing slash, white space around the address, a user,
// localhost in any case). test/crd's TestAddressShapesFollowTheEngine holds
// the API's rules to the engine at shapes of its own.
var addressShapes = []string{
	"http://127.1:8080", "http://127.1", "http://2130706433", "http://0x7f.0.0.1", "http://0177.0.0.1",
	"http://192.168.010.1", "http://1.2.3.4.", "http://01.2.3.4", "http://1.2.3.04", "http://256.1.1.1",
	"http://1.2.3", "http://1.2.3.4", "http://0.0.0.0", "http://255.255.255.255", "http://127.0.0.1:8080",
	"http://foo.123", "http://foo.123.", "http://foo.123..", "http://foo.0x1f", "http://foo.0X1F", "http://foo.0x",
	"http://0x.", "http://123.", "http://.1", "http://foo123", "http://123foo", "http://foo.1x", "http://1e1.example",
	"http://example.com.", "http://" + strings.Repeat("a", 2030) + ".1",
	"https://bücher.example", "https://xn--bcher-kva.example", "http://b%C3%BCcher.example", "http://a%C3.example",
	"http://%31.2.3.4", "http://a%25b.example",
	"http://[::1]", "http://[::1]:8080", "http://[fe80::1]", "http://[::ffff:127.0.0.1]",
	"http://[fe80::1%25eth0]:8080", "http://[fe80::1%eth0]", "http://::1:8080", "http://[1.2.3.4]",
	"http://h.example:80:80", "http://x[::1]:8080", "http://[v1.x]", "http://[12345::1]",
	"http://h.example:0", "http://h.example:00000", "http://h.example:1", "http://h.example:080",
	"http://h.example:0000000000000000080", "http://h.example:65535", "http://h.example:65535/",
	"http://h.example:65536", "http://h.example:99999", "http://h.example:123456789012345678901234567890",
	"http://h.example:" + strings.Repeat("9", 2000), "http://h.example:",
	"HTTP://h.example", "Https://h.example", "ftp://h.example", "h.example", "http://", "http://:8080",
	"http://h.example/?tenant=nimbus", "http://h.example/#/worklet", "http://h.example/?", "http://h.example?",
	"http://h.example?a=b", "http://h.example#x", "http://h.example#", "http://u:p@h.example", "http://u@h.example",
	" http://h.example", "http://h.example ", "  ", "http://a b.example",
	"http://h.example/a\u0080b", "http://h.example/a\u0085b", "http://h.ex\u0085ample", "http://\u0085h.example",
	"http://h.ex\u0080ample",
	"http://h.example/", "http://h.example/nimbus/path", "http://h.example/nimbus/", "http://h.example/ü",
	"http://h.example:8080/a%2Fb", "http://localhost:8080", "http://LOCALHOST:8080", "https://crewlet.example.com",
	"*",
}

// TestContractAddressShapes holds the render's verdict on the addresses a
// data pod's engine is told it is reached at to Crewlet's, both ways, at
// every shape of addressShapes in each place a Tier A names one: as
// api.external_url, as api.public.external_url and as an
// api.auth.allowed_origins entry. Bootstrap refuses a shape there with an
// AddressError naming the place exactly where `crewlet validate` refuses a
// Tier A holding the shape there, at that path, and renders it wherever the
// engine takes it; so the render holds back no data pod the engine would
// run, and starts none it would refuse to boot.
func TestContractAddressShapes(t *testing.T) {
	base := func(t *testing.T) *BootstrapInput {
		t.Helper()
		return bootstrapCases()[0].input(t)
	}
	doc, err := Bootstrap(base(t))
	if err != nil {
		t.Fatal(err)
	}
	// engineRefuses is whether the engine refuses the rendered Tier A once
	// set puts a shape in it, at path; and fails the test where it refuses
	// it for anything else.
	engineRefuses := func(t *testing.T, set func(api map[string]any), paths ...string) map[string]bool {
		t.Helper()
		var tierA map[string]any
		if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
			t.Fatal(err)
		}
		set(tierA["api"].(map[string]any))
		// Written as the render writes, so that a shape reaches the engine
		// as the render would hand it over.
		encoded, err := json.Marshal(tierA)
		if err != nil {
			t.Fatal(err)
		}
		mutated, err := yamlOf(encoded)
		if err != nil {
			t.Fatal(err)
		}
		got, stdout, _, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", mutated))
		refused := map[string]bool{}
		for _, raw := range got.Problems {
			var p struct{ Path string }
			if err := json.Unmarshal(raw, &p); err != nil || !slices.Contains(paths, p.Path) {
				t.Fatalf("crewlet validate refused the Tier A elsewhere than at %v:\n%s", paths, stdout)
			}
			refused[p.Path] = true
		}
		if got.Valid == (len(refused) > 0) {
			t.Fatalf("crewlet validate is valid %v with problems at %v:\n%s", got.Valid, refused, stdout)
		}
		return refused
	}
	// renderRefuses is whether Bootstrap refuses a shape at field, and fails
	// the test where it refuses it for anything else.
	renderRefuses := func(t *testing.T, in *BootstrapInput, field, shape string) bool {
		t.Helper()
		_, err := Bootstrap(in)
		refused, ok := errors.AsType[*AddressError](err)
		switch {
		case err == nil:
			return false
		case !ok || refused.Field != field || refused.Address != shape:
			t.Fatalf("Bootstrap refused %s %q with %v", field, shape, err)
		}
		return true
	}
	for _, place := range []struct {
		field string
		set   func(in *BootstrapInput, shape string)
		patch func(api map[string]any, shape string)
	}{
		{
			field: FieldExternalURL,
			set:   func(in *BootstrapInput, shape string) { in.ExternalURL = shape },
			patch: func(api map[string]any, shape string) { api["external_url"] = shape },
		},
		{
			field: FieldPublicURL,
			set:   func(in *BootstrapInput, shape string) { in.PublicURL = shape },
			patch: func(api map[string]any, shape string) { api["public"].(map[string]any)["external_url"] = shape },
		},
	} {
		t.Run(place.field, func(t *testing.T) {
			for _, shape := range addressShapes {
				t.Run(fmt.Sprintf("%.40q", shape), func(t *testing.T) {
					t.Parallel()
					engine := engineRefuses(t, func(api map[string]any) { place.patch(api, shape) }, place.field)[place.field]
					in := base(t)
					place.set(in, shape)
					if render := renderRefuses(t, in, place.field, shape); render != engine {
						t.Errorf("the render refuses %s %q: %v; crewlet validate refuses it: %v", place.field, shape, render, engine)
					}
				})
			}
		})
	}
	t.Run(FieldAllowedOrigins, func(t *testing.T) {
		// One Tier A holds every shape, each at its own index.
		paths := make([]string, len(addressShapes))
		for i := range addressShapes {
			paths[i] = fmt.Sprintf("%s[%d]", FieldAllowedOrigins, i)
		}
		engine := engineRefuses(t, func(api map[string]any) {
			api["auth"].(map[string]any)["allowed_origins"] = addressShapes
		}, paths...)
		for i, shape := range addressShapes {
			in := base(t)
			in.AllowedOrigins = []string{shape}
			if render := renderRefuses(t, in, FieldAllowedOrigins, shape); render != engine[paths[i]] {
				t.Errorf("the render refuses %s %q: %v; crewlet validate refuses it: %v", FieldAllowedOrigins, shape, render, engine[paths[i]])
			}
		}
	})
}

// TestContractBoxURLShapes holds the render's verdict on an address handed to
// sandboxes (BootstrapInput.BoxURLs) to Crewlet's, both ways, at every shape
// of boxURLTaken, boxURLRefused and addressShapes, in each variable: Bootstrap
// refuses a shape there with an AddressError naming the variable exactly
// where the engine refuses to boot on it, which `crewlet llm doctor` reports
// as its boot would read it (enginetest.BoxURLVerdicts), and renders it
// wherever the engine takes it; so the render holds back no data pod the
// engine would run, and starts none it would refuse to boot.
func TestContractBoxURLShapes(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, "minimal-agent").input(t))
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for _, k := range doc.Refs {
		env[secretsync.StoreName(k)] = dummyCredential(k)
	}
	shapes := slices.Concat(boxURLTaken, boxURLRefused, addressShapes)
	refusedSomewhere := make([]bool, len(shapes))
	t.Run("shapes", func(t *testing.T) {
		for i, shape := range shapes {
			t.Run(fmt.Sprintf("%.40q", shape), func(t *testing.T) {
				t.Parallel()
				withShape := maps.Clone(env)
				withShape[EnvBridgeURL], withShape[EnvReceiverURL] = shape, shape
				engine := enginetest.BoxURLVerdicts(t, doc.YAML, withShape, EnvBridgeURL, EnvReceiverURL)
				refusedSomewhere[i] = engine[EnvBridgeURL] || engine[EnvReceiverURL]
				for _, variable := range []string{EnvBridgeURL, EnvReceiverURL} {
					in := bootstrapCases()[0].input(t)
					in.BoxURLs = []BoxURL{{Variable: variable, URL: shape}}
					_, err := Bootstrap(in)
					refused, ok := errors.AsType[*AddressError](err)
					switch {
					case err != nil && (!ok || refused.Field != variable || refused.Address != shape):
						t.Fatalf("Bootstrap refused %s %q with %v", variable, shape, err)
					case (err != nil) != engine[variable]:
						t.Errorf("the render refuses %s %q: %v; Crewlet refuses to boot on it: %v",
							variable, shape, err != nil, engine[variable])
					}
				}
			})
		}
	})
	// A doctor whose report this test misread would read every shape as one
	// the engine takes, and the render's refusals as the only ones.
	if !slices.Contains(refusedSomewhere, true) {
		t.Error("crewlet llm doctor refused no box URL: its report did not name the variables as this test reads it")
	}
}

// artifactShapes are artifact origins either side of each line Crewlet draws,
// read beside a dashboard at nimbusAPI and a public listener at nimbusHooks.
var artifactShapes = []string{
	nimbusArtifacts, "https://artifacts.crewletusercontent.example", "https://artifacts.crewletusercontent.example/",
	" https://artifacts.crewletusercontent.example ", "   ",
	"http://localhost:8082", "http://{artifact}.localhost:8082", "http://127.0.0.1:8082", "http://[::1]:8082",
	"http://artifacts.crewletusercontent.example", "ftp://artifacts.crewletusercontent.example",
	"https://{artifact}--acme.crewletusercontent.example", "https://{artifact}-" + strings.Repeat("a", 36) + ".crewletusercontent.example",
	"https://{artifact}-" + strings.Repeat("a", 37) + ".crewletusercontent.example",
	"https://{artifact}{artifact}.crewletusercontent.example", "https://x{artifact}.crewletusercontent.example",
	"https://{artifact}", "https://{artifact}:8443", "https://{artifact}/x", "https://{artifact}_x.crewletusercontent.example",
	"https://{artifact}X.crewletusercontent.example",
	"https://artifacts.crewletusercontent.example/pages", "https://artifacts.crewletusercontent.example?x=1",
	"https://artifacts.crewletusercontent.example#x", "https://artifacts.crewletusercontent.example:0",
	"https://artifacts.crewletusercontent.example:65536", "https://artifacts.crewletusercontent.example:8443",
	nimbusAPI, nimbusHooks, nimbusAPI + ":443", "https://api.example.com", "https://example.com",
	"https://{artifact}.api.example.com", "https://{artifact}.example.com",
}

// TestContractArtifactAddressShapes holds the render's refusal of an artifact
// origin to crewlet validate's, shape by shape.
func TestContractArtifactAddressShapes(t *testing.T) {
	base := func(t *testing.T) *BootstrapInput {
		t.Helper()
		in := bootstrapCases()[0].input(t)
		in.ExternalURL, in.PublicURL, in.ArtifactsURL = nimbusAPI, nimbusHooks, nimbusArtifacts
		return in
	}
	// engineRefuses is whether crewlet validate refuses shape as in's artifact
	// origin.
	engineRefuses := func(t *testing.T, in *BootstrapInput, shape string) bool {
		t.Helper()
		doc, err := Bootstrap(in)
		if err != nil {
			t.Fatal(err)
		}
		var tierA map[string]any
		if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
			t.Fatal(err)
		}
		tierA["api"].(map[string]any)["artifacts"].(map[string]any)["external_url"] = shape
		encoded, err := json.Marshal(tierA)
		if err != nil {
			t.Fatal(err)
		}
		mutated, err := yamlOf(encoded)
		if err != nil {
			t.Fatal(err)
		}
		got, stdout, _, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", mutated))
		for _, raw := range got.Problems {
			var p struct{ Path string }
			if err := json.Unmarshal(raw, &p); err != nil || p.Path != FieldArtifactsURL {
				t.Fatalf("crewlet validate refused the Tier A elsewhere than at %s:\n%s", FieldArtifactsURL, stdout)
			}
		}
		if got.Valid == (len(got.Problems) > 0) {
			t.Fatalf("crewlet validate is valid %v with problems %s", got.Valid, stdout)
		}
		return !got.Valid
	}
	// renderRefuses is whether Bootstrap refuses shape as in's artifact origin.
	renderRefuses := func(t *testing.T, in *BootstrapInput, shape string) bool {
		t.Helper()
		in.ArtifactsURL = shape
		_, err := Bootstrap(in)
		refused, ok := errors.AsType[*AddressError](err)
		switch {
		case err == nil:
			return false
		case !ok || refused.Field != FieldArtifactsURL || refused.Address != shape:
			t.Fatalf("Bootstrap refused %s %q with %v", FieldArtifactsURL, shape, err)
		}
		return true
	}
	for _, shape := range artifactShapes {
		t.Run(fmt.Sprintf("%.60q", shape), func(t *testing.T) {
			t.Parallel()
			engine, render := engineRefuses(t, base(t), shape), renderRefuses(t, base(t), shape)
			if render != engine {
				t.Errorf("the render refuses %s %q: %v; crewlet validate refuses it: %v", FieldArtifactsURL, shape, render, engine)
			}
		})
	}
	for _, c := range []struct{ name, external, shape string }{
		{
			// The dashboard's first label is an artifact label's length.
			name:     "an origin per artifact that would give one the dashboard's host",
			external: "https://" + strings.Repeat("d", 26) + ".apps.example.com",
			shape:    "https://{artifact}.apps.example.com",
		},
		{
			name:     "an origin per artifact beside a dashboard of another label length",
			external: "https://" + strings.Repeat("d", 25) + ".apps.example.com",
			shape:    "https://{artifact}.apps.example.com",
		},
		{
			name:     "a template with a suffix that would give one the dashboard's host",
			external: "https://" + strings.Repeat("d", 26) + "--acme.apps.example.com",
			shape:    "https://{artifact}--acme.apps.example.com",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := func() *BootstrapInput {
				in := base(t)
				in.ExternalURL = c.external
				return in
			}
			engine, render := engineRefuses(t, in(), c.shape), renderRefuses(t, in(), c.shape)
			if render != engine {
				t.Errorf("beside %s, the render refuses %q: %v; crewlet validate refuses it: %v", c.external, c.shape, render, engine)
			}
		})
	}
	t.Run("an allowed origin", func(t *testing.T) {
		in := func() *BootstrapInput {
			in := base(t)
			in.ExternalURL, in.AllowedOrigins = "https://crewlet.sso.example.com/nimbus", []string{nimbusAPI}
			return in
		}
		for _, shape := range []string{nimbusAPI, "https://{artifact}.nimbus.api.example.com"} {
			engine, render := engineRefuses(t, in(), shape), renderRefuses(t, in(), shape)
			if render != engine {
				t.Errorf("beside the allowed origin %s, the render refuses %q: %v; crewlet validate refuses it: %v",
					nimbusAPI, shape, render, engine)
			}
		}
	})
}

// TestContractTrustedProxies ties the API's rule on an EngineClass's
// spec.exposure.trustedProxies, which becomes api.trusted_proxies, to
// Crewlet's (config.checkTrustedProxy): an address that is not a block, a
// block of the whole address space and one with host bits set are each
// refused by the engine, so the API refusing them on the class refuses
// nothing the data pod would start with.
func TestContractTrustedProxies(t *testing.T) {
	for _, block := range []string{"10.0.0.7", "0.0.0.0/0", "10.0.0.7/8"} {
		t.Run(block, func(t *testing.T) {
			c := bootstrapCases()[0]
			c.proxies = []string{block}
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
			refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
				var p struct{ Path string }
				return json.Unmarshal(raw, &p) == nil && p.Path == "api.trusted_proxies[0]"
			})
			if got.Valid || !refused {
				t.Errorf("crewlet validate did not refuse api.trusted_proxies[0] %q:\n%s\n%s", block, stdout, stderr)
			}
		})
	}
}

// TestContractS3BucketAndRegion ties the API's rules on an EngineClass's
// spec.objects.s3 region and bucket, which become store.objects.s3's, to
// Crewlet's (config.StoreObjects.validate, over the trim of every Tier A
// string, config.Bootstrap.normalize): a region or a bucket of white space
// alone is refused, so the API refusing it on the class refuses nothing a
// data or seat pod would start with; and one with white space around it is
// taken, so the API, which takes it too, crash-loops no pod on it.
func TestContractS3BucketAndRegion(t *testing.T) {
	for _, c := range []struct {
		name, region, bucket, refused string
	}{
		{name: "a region of white space", region: " \t", bucket: "crewlet", refused: "store.objects.s3.region"},
		{name: "a bucket of white space", region: "eu-central-1", bucket: "   ", refused: "store.objects.s3.bucket"},
		{name: "white space around both", region: " eu-central-1 ", bucket: " crewlet "},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := bootstrapCases()[1].input(t)
			in.Class.Spec.Objects.S3.Region, in.Class.Spec.Objects.S3.Bucket = c.region, c.bucket
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
			refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
				var p struct{ Path string }
				return json.Unmarshal(raw, &p) == nil && p.Path == c.refused
			})
			switch {
			case c.refused == "" && (!got.Valid || len(got.Problems) > 0):
				t.Errorf("crewlet validate refused the Tier A:\n%s\n%s", stdout, stderr)
			case c.refused != "" && (got.Valid || !refused):
				t.Errorf("crewlet validate did not refuse %s:\n%s\n%s", c.refused, stdout, stderr)
			}
		})
	}
}

// TestContractSignInPosture ties the render's verdict on a class's sign-in to
// Crewlet's (config.APIAuth.validatePasswords, over config.API.
// externalLoopback), at every address of loopbackCases. A second factor
// Optional without the class's acknowledgement renders exactly where the
// engine accepts it, and is refused by the render (ErrSignInNotAcknowledged)
// exactly where the engine refuses to start on it, so the operator neither
// holds back a company the engine would run nor starts one it refuses.
// Acknowledged, it renders at every address and the engine accepts it, with
// its warning that the insecure sign-in settings are allowed.
func TestContractSignInPosture(t *testing.T) {
	optional := v1alpha1.SignInSettings{SecondFactor: v1alpha1.SecondFactorOptional}
	acknowledged := v1alpha1.SignInSettings{SecondFactor: v1alpha1.SecondFactorOptional, AcceptInsecure: new(true)}
	for _, c := range loopbackCases {
		t.Run(c.address, func(t *testing.T) {
			render := func(signIn v1alpha1.SignInSettings) (*BootstrapDocument, error) {
				in := bootstrapCases()[0].input(t)
				in.ExternalURL, in.Class.Spec.SignIn = c.address, signIn
				return Bootstrap(in)
			}
			// What Crewlet says of the address whatever the sign-in: an
			// https address trusts no proxy here, and plain http off
			// loopback sends passwords in the clear.
			var advisories []advisory
			switch {
			case strings.HasPrefix(c.address, "https://"):
				advisories = append(advisories, advisoryNoTrustedProxy)
			case !c.loopback:
				advisories = append(advisories, advisoryPlainHTTP)
			}

			doc, err := render(acknowledged)
			if err != nil {
				t.Fatal(err)
			}
			validate(t, bootstrapEnv(doc), append(slices.Clone(advisories), advisoryInsecureSignIn),
				"-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))

			if _, err := render(optional); c.loopback {
				if err != nil {
					t.Fatalf("Bootstrap() = %v, want the crewlet.yaml of a class whose second factor is optional on loopback", err)
				}
			} else if !errors.Is(err, ErrSignInNotAcknowledged) {
				t.Fatalf("Bootstrap() = %v, want ErrSignInNotAcknowledged", err)
			}
			// The acknowledged document without its acknowledgement, which
			// is the document the render refuses off loopback, and renders
			// on it.
			var tierA map[string]any
			if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
				t.Fatal(err)
			}
			delete(tierA["api"].(map[string]any)["auth"].(map[string]any), "accept_insecure")
			unacknowledged, err := yaml.Marshal(tierA)
			if err != nil {
				t.Fatal(err)
			}
			file := write(t, "crewlet.yaml", unacknowledged)
			if c.loopback {
				validate(t, bootstrapEnv(doc), advisories, "-tier", "bootstrap", file)
				return
			}
			got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", file)
			refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
				var p struct{ Path, Message string }
				return json.Unmarshal(raw, &p) == nil && p.Path == "api.auth.totp" && strings.Contains(p.Message, "password-only sign-in")
			})
			if got.Valid || !refused {
				t.Errorf("crewlet validate did not refuse api.auth.totp optional at %s, which the render refuses:\n%s\n%s", c.address, stdout, stderr)
			}
		})
	}
}

// TestContractSecondFactorReachesTheEngine runs the engine on a data pod's
// rendered crewlet.yaml and asks it what a sign-in page asks before anybody
// signs in (GET /auth/config): the crewlet.yaml of a class that says nothing
// about sign-in, which leaves api.auth.totp out, requires a second factor, and
// that of a class whose secondFactor is Optional does not. No validation can
// say so: an unset totp is valid whichever way the engine reads it, and only
// the running engine says which.
func TestContractSecondFactorReachesTheEngine(t *testing.T) {
	for _, c := range []struct {
		signIn v1alpha1.SignInSettings
		want   string
	}{
		{want: "required"},
		{signIn: v1alpha1.SignInSettings{SecondFactor: v1alpha1.SecondFactorOptional}, want: totpOptional},
	} {
		t.Run(c.want, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.Class.Spec.SignIn = c.signIn
			e, tierA := startDataPod(t, in)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.URL+"/auth/config", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			var posture struct {
				SecondFactor string `json:"second_factor"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&posture); err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("GET /auth/config: %d, %v", resp.StatusCode, err)
			}
			if posture.SecondFactor != c.want {
				t.Errorf("the engine reports second_factor %q for the crewlet.yaml\n%s\nwant %q", posture.SecondFactor, tierA.YAML, c.want)
			}
		})
	}
}

// TestContractVendorURLsAreReadAsWritten ties the API's rules on the vendor
// addresses an Integration names to Crewlet's (config.hasHTTPScheme), which
// reads a scheme as written: each address refused with its scheme in upper
// case, so the API refusing HTTPS:// on the Integration refuses nothing the
// engine would apply.
func TestContractVendorURLsAreReadAsWritten(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	// The case names each Integration after its vendor.
	for _, c := range []struct {
		integration, path string
		set               func(*v1alpha1.IntegrationSpec)
	}{
		{VendorGitHub, "integrations.github.url", func(s *v1alpha1.IntegrationSpec) { s.GitHub.URL = "HTTPS://github.example.com" }},
		{VendorGitLab, "integrations.gitlab.url", func(s *v1alpha1.IntegrationSpec) { s.GitLab.URL = "HTTPS://gitlab.example.com" }},
		{VendorMattermost, "integrations.mattermost.url", func(s *v1alpha1.IntegrationSpec) { s.Mattermost.URL = "Https://chat.example.com" }},
		{VendorJira, "integrations.jira.url", func(s *v1alpha1.IntegrationSpec) { s.Jira.URL = "HTTP://jira.example.com" }},
		{VendorJira, "integrations.jira.site_url", func(s *v1alpha1.IntegrationSpec) { s.Jira.SiteURL = "HTTPS://jira.example.com" }},
		{VendorConfluence, "integrations.confluence.url", func(s *v1alpha1.IntegrationSpec) { s.Confluence.URL = "HTTPS://wiki.example.com" }},
		{VendorConfluence, "integrations.confluence.site_url", func(s *v1alpha1.IntegrationSpec) { s.Confluence.SiteURL = "HTTPS://wiki.example.com" }},
	} {
		t.Run(c.path, func(t *testing.T) {
			in := companyCaseNamed(t, "secret-positions").input(t)
			i := slices.IndexFunc(in.Integrations, func(i v1alpha1.Integration) bool { return i.Name == c.integration })
			if i < 0 {
				t.Fatalf("the case has no Integration named %s", c.integration)
			}
			c.set(&in.Integrations[i].Spec)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			for _, name := range doc.Provisioned {
				env[name] = "minted-" + name
			}
			got, stdout, stderr, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
			refused := slices.ContainsFunc(got.Problems, func(raw json.RawMessage) bool {
				var p struct{ Path, Message string }
				return json.Unmarshal(raw, &p) == nil && p.Path == c.path && strings.Contains(p.Message, "must start with http:// or https://")
			})
			if got.Valid || !refused {
				t.Errorf("crewlet validate did not refuse %s for its upper-case scheme:\n%s\n%s", c.path, stdout, stderr)
			}
		})
	}
}

// TestContractSeatPoolsCompany validates the document of a company that
// places seats in every kind of pool with the Tier A of its data pod, which
// listens for the seat pods: the pins are labels the engine accepts beside
// the node that serves the company.
func TestContractSeatPoolsCompany(t *testing.T) {
	in := companyCaseNamed(t, caseSeatPools).input(t)
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	boot := bootstrapCases()[0].input(t)
	boot.Company, boot.Class = in.Company, in.Class
	tierA, err := Bootstrap(boot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tierA.YAML), "leaf:") {
		t.Fatalf("the data pod of a class that allows pools opens no leaf listener:\n%s", tierA.YAML)
	}
	env := bootstrapEnv(tierA)
	for _, k := range doc.Refs {
		env[secretsync.StoreName(k)] = dummyCredential(k)
	}
	validate(t, env, companyCaseNamed(t, caseSeatPools).advisories,
		"-config", write(t, "crewlet.yaml", tierA.YAML), "-company", write(t, "company.yaml", doc.YAML))
}

// TestContractStreamCeiling ties the render's stream share of a large data
// volume to Crewlet's ceiling on stream.store_max_bytes
// (config.StoreMaxBytesCeiling): the ceiling is taken and a byte more is
// refused, so storeMaxBytesCeiling is the engine's and not a guess; and every
// volume the API admits past what three quarters of fits under it, up to
// sizes no int64 holds, renders a stream Crewlet accepts.
func TestContractStreamCeiling(t *testing.T) {
	for _, c := range []struct {
		name   string
		bytes  int64
		accept bool
	}{
		{name: "the ceiling", bytes: storeMaxBytesCeiling, accept: true},
		{name: "a byte more", bytes: storeMaxBytesCeiling + 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(bootstrapCases()[0].input(t))
			if err != nil {
				t.Fatal(err)
			}
			var tierA map[string]any
			if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
				t.Fatal(err)
			}
			tierA["stream"].(map[string]any)["store_max_bytes"] = c.bytes
			encoded, err := json.Marshal(tierA)
			if err != nil {
				t.Fatal(err)
			}
			written, err := yamlOf(encoded)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-tier", "bootstrap", write(t, "crewlet.yaml", written)}
			if c.accept {
				validate(t, bootstrapEnv(doc), nil, args...)
				return
			}
			got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), args...)
			if got.Valid || !strings.Contains(stdout+stderr, "store_max_bytes") {
				t.Errorf("crewlet validate did not refuse stream.store_max_bytes %d:\n%s\n%s", c.bytes, stdout, stderr)
			}
		})
	}
	for _, size := range []string{"85Ti", "86Ti", "1Pi", "1Ei", "16Ei", "9223372036854775808", "1e30"} {
		t.Run(size, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.VolumeSize = resource.MustParse(size)
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			validate(t, bootstrapEnv(doc), nil, "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
		})
	}
}

// TestContractCompany validates every company case together with the Tier
// A of the pod that runs it, so the rules that need both tiers are checked
// too. Every secret-store reference resolves to a dummy value, as the
// operator's secret sync would make it resolve to the real one.
func TestContractCompany(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	for _, c := range companyCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Company(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			env := bootstrapEnv(tierA)
			for _, k := range doc.Refs {
				env[secretsync.StoreName(k)] = dummyCredential(k)
			}
			// What the engine's provisioning mints, once it has.
			for _, name := range doc.Provisioned {
				env[name] = "minted-" + name
			}
			validate(t, env, c.advisories, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		})
	}
}

// TestContractAdvisoriesAreHeldBothWays proves the advisory gate validate
// applies can fail in each direction, against the engine's own verdicts: an
// advisory the engine gives that the case did not expect is reported, and so
// is an expected one the engine did not give.
func TestContractAdvisoriesAreHeldBothWays(t *testing.T) {
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	warnings := func(name string) []json.RawMessage {
		doc, err := Company(companyCaseNamed(t, name).input(t))
		if err != nil {
			t.Fatal(err)
		}
		env := bootstrapEnv(tierA)
		for _, k := range doc.Refs {
			env[secretsync.StoreName(k)] = dummyCredential(k)
		}
		got, _, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		return got.Warnings
	}
	agentsAlone, withPeople := warnings("minimal-agent"), warnings("org-chart")

	if unexpected, _ := advisoriesOf(agentsAlone, nil); len(unexpected) != 1 ||
		!strings.Contains(unexpected[0], advisoryNoHumanSeat.says) {
		t.Errorf("a chart of agents alone, expecting nothing: unexpected %v, want the engine's no-human-seat advisory", unexpected)
	}
	never := advisory{path: keyRoles, says: "a sentence the engine never says"}
	if _, missing := advisoriesOf(agentsAlone, []advisory{advisoryNoHumanSeat, never}); !slices.Equal(missing, []advisory{never}) {
		t.Errorf("expecting an advisory the engine never gives: missing %v, want [%v]", missing, never)
	}
	if _, missing := advisoriesOf(withPeople, []advisory{advisoryNoHumanSeat}); !slices.Equal(missing, []advisory{advisoryNoHumanSeat}) {
		t.Errorf("a chart with a person, expecting the no-human-seat advisory: missing %v, want it", missing)
	}
	// The engine's words at another place are another advisory.
	elsewhere := advisory{path: "units", says: advisoryNoHumanSeat.says}
	if _, missing := advisoriesOf(agentsAlone, []advisory{elsewhere}); !slices.Equal(missing, []advisory{elsewhere}) {
		t.Errorf("expecting the no-human-seat advisory at %s: missing %v, want it", elsewhere.path, missing)
	}
}

// dummyCredential is a stand-in value for a referenced Secret key.
func dummyCredential(k secretref.Key) string {
	return "dummy-" + k.Name + "-" + k.Key
}

// TestContractNoCredentialLeavesThroughAReference runs the engine as the
// operator configures a data pod (startDataPod) and applies a company
// document that names the variables the operator once put the credentials
// in, in a field that is no credential: an MCP server's URL, which the
// engine expands from its secret store and then from its environment before
// it connects. None of them may resolve. Crewlet dials no server whose url
// holds a reference that resolves to nothing, so the engine must hold the
// server back and name every one of them as set nowhere: a variable the
// operator gave the engine would be missing from that list, and would be
// sent to the server the moment the others were set. A control server with
// no reference, at the same address, is dialled, so a listener that hears
// nothing from the probe is one the engine could reach, and no request it
// hears may carry a credential.
func TestContractNoCredentialLeavesThroughAReference(t *testing.T) {
	type request struct{ url, header, body string }
	requests := make(chan request, 64)
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{url: r.URL.String(), header: fmt.Sprint(r.Header), body: string(body)}
		http.Error(w, "not an MCP server", http.StatusNotFound)
	}))
	t.Cleanup(listener.Close)

	tokens := APITokens{Operator: "operator-" + strings.Repeat("o", 40), Access: "access-" + strings.Repeat("a", 40)}
	in := bootstrapCases()[0].input(t)
	in.Tokens = tokens
	e, _ := startDataPod(t, in)

	// Every variable a credential has ever been given to the engine in.
	variables := []string{
		"CREWLET_TOKEN_OPERATOR", "CREWLET_TOKEN_ACCESS", "CREWLET_SECRET_KEY_K1",
		"CREWLET_S3_ACCESS_KEY_ID", "CREWLET_S3_SECRET_ACCESS_KEY",
	}
	url := listener.URL + "/mcp"
	for _, name := range variables {
		url += "/${" + name + "}"
	}
	company := fmt.Appendf(nil, "name: Nimbus\nmcp_servers:\n  - name: probe\n    transport: http\n    url: %q\n"+
		"  - name: control\n    transport: http\n    url: %q\n", url, listener.URL+"/control")
	if _, err := e.Client().ApplyConfig(t.Context(), engine.ApplyRequest{Document: company, CreateOnly: true, Summary: "a probe for leaked credentials"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}

	var seen []request
	for dialled := false; !dialled; {
		select {
		case r := <-requests:
			seen = append(seen, r)
			dialled = strings.HasPrefix(r.url, "/control")
		case <-time.After(30 * time.Second):
			t.Fatal("the engine never reached the control server, so the test proves nothing")
		}
	}

	held := heldBack(t, e, "probe")
	if !strings.Contains(held, "not set") {
		t.Errorf("the engine holds the probe server back for another reason than variables set nowhere: %s", held)
	}
	for _, name := range variables {
		if !strings.Contains(held, "${"+name+"}") {
			t.Errorf("the engine does not name %s among the variables set nowhere, so it holds a value under it: %s", name, held)
		}
	}

	for drained := false; !drained; {
		select {
		case r := <-requests:
			seen = append(seen, r)
		case <-time.After(time.Second):
			drained = true
		}
	}
	for _, r := range seen {
		if strings.HasPrefix(r.url, "/mcp") {
			t.Errorf("the engine dialled the probe server's url: %+v", r)
		}
		for _, credential := range []string{tokens.Operator, tokens.Access, testKeyMaterial} {
			if strings.Contains(r.url+r.header+r.body, credential) {
				t.Errorf("the engine sent a credential to the MCP server's URL: %+v", r)
			}
		}
	}
}

// heldBack is why e holds the tool server named server back, as its GET
// /mcp-servers reports a failed instance of it (the reason of its
// mcp_server_credential_missing line). The report rides each node's
// heartbeat, so it is waited for.
func heldBack(t *testing.T, e *enginetest.Engine, server string) string {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.URL+"/mcp-servers", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+e.OperatorToken)
		var status struct {
			Servers []struct {
				Name  string `json:"name"`
				Nodes []struct {
					Error string `json:"error"`
				} `json:"nodes"`
			} `json:"servers"`
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			err = json.NewDecoder(resp.Body).Decode(&status)
			_ = resp.Body.Close()
		}
		for _, row := range status.Servers {
			for _, node := range row.Nodes {
				if row.Name == server && node.Error != "" {
					return node.Error
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine reports no failed instance of %s after a minute (%v): %+v", server, err, status)
		}
		time.Sleep(time.Second)
	}
}

// TestContractChainlessSeatRunsOnTheFirstProviderAlone ties the docs of
// spec.llm.order to the engine: a seat that names no chain runs on the first
// provider of the rendered llm_order and on no other, as the engine's own
// org projection resolves it, which is how a turn resolves it. A provider
// named default would take its place; the API refuses that name.
func TestContractChainlessSeatRunsOnTheFirstProviderAlone(t *testing.T) {
	in := companyCaseNamed(t, "minimal-agent").input(t)
	second := in.LLMProviders[0].DeepCopy()
	second.Name = "aardvark"
	in.LLMProviders = append(in.LLMProviders, *second)
	// The order names the provider that sorts last, so that the first
	// provider of llm_order is the order's and not the first by name.
	in.Company.Spec.LLM.Order = []v1alpha1.LLMProviderReference{{Kind: v1alpha1.LLMProviderKindLLMProvider, Name: "claude"}}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}

	e := enginetest.Start(t)
	client := e.Client()
	for _, k := range doc.Refs {
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(dummyCredential(k))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, CreateOnly: true, Summary: "chains"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}

	// The engine activates a revision after it answers the apply; it is
	// ready once its first company runs.
	for deadline := time.Now().Add(time.Minute); client.Ready(t.Context()) != nil; {
		if time.Now().After(deadline) {
			t.Fatalf("the engine never became ready: %v", client.Ready(t.Context()))
		}
		time.Sleep(200 * time.Millisecond)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, e.URL+"/org", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.OperatorToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var org struct {
		Roles []struct {
			Handle string              `json:"handle"`
			LLM    map[string][]string `json:"llm"`
		} `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&org); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /org: %d, %v", resp.StatusCode, err)
	}
	if len(org.Roles) != 1 || len(org.Roles[0].LLM) == 0 {
		t.Fatalf("GET /org shows no seat with its chains: %+v", org.Roles)
	}
	for phase, chain := range org.Roles[0].LLM {
		if !slices.Equal(chain, []string{"claude"}) {
			t.Errorf("seat %s runs phase %s on %v, want [claude] alone", org.Roles[0].Handle, phase, chain)
		}
	}
}

// TestContractGettingStarted renders the company the getting-started guide
// applies, as the guide applies it, with the Tier A its class gives a data
// pod, and has the engine validate both: the first page a reader follows
// must run on the engine the operator is tested against.
func TestContractGettingStarted(t *testing.T) {
	manifests, err := docs.Manifests(filepath.Join("..", "..", "docs", "getting-started.md"))
	if err != nil {
		t.Fatal(err)
	}
	in := load(t, write(t, "guide.yaml", []byte(strings.Join(manifests, "\n---\n")))).input
	if in.Class.Name != in.Company.Spec.EngineClassName {
		t.Fatalf("the guide's Company names EngineClass %q and the guide creates %q", in.Company.Spec.EngineClassName, in.Class.Name)
	}
	doc, err := Company(&in)
	if err != nil {
		t.Fatal(err)
	}

	boot := bootstrapCases()[0].input(t)
	boot.Company, boot.Class = in.Company, in.Class
	boot.VolumeSize = *in.Class.Spec.Data.Storage.Size
	tierA, err := Bootstrap(boot)
	if err != nil {
		t.Fatal(err)
	}
	env := bootstrapEnv(tierA)
	for _, k := range doc.Refs {
		env[secretsync.StoreName(k)] = dummyCredential(k)
	}
	// The guide's company has a Human seat for its reader, who joins it in
	// the dashboard and is reached nowhere else: Crewlet says that seat
	// has no chat contact, and nothing else.
	founderUnreachable := advisory{path: "roles[0].contact", says: "founder is a human seat with no chat contact"}
	validate(t, env, []advisory{founderUnreachable},
		"-config", write(t, "crewlet.yaml", tierA.YAML), "-company", write(t, "company.yaml", doc.YAML))
}

// TestContractScheduleRefusalsReachTheSchedule has the engine's dry run
// refuse each of a schedule's mistakes no API rule can see, and checks that
// the document attributes every problem to the Schedule, which is what the
// controller reports it on: the engine's place for each is inside the
// schedule's entry of a role's or unit's schedules.
func TestContractScheduleRefusalsReachTheSchedule(t *testing.T) {
	const (
		scheduleInventory = "inventory"
		scheduleReport    = "weekly-report"
	)
	client := enginetest.Start(t).Client()
	for _, c := range []struct {
		name     string
		mutate   func(*Input)
		schedule string
	}{
		{
			name:     "a cron field out of range",
			mutate:   func(in *Input) { schedule(in, scheduleInventory).Spec.Schedule = "61 * * * *" },
			schedule: scheduleInventory,
		},
		{
			name:     "a day name the grammar does not have",
			mutate:   func(in *Input) { schedule(in, scheduleInventory).Spec.Schedule = "0 9 * * MON-FRY" },
			schedule: scheduleInventory,
		},
		{
			name:     "a time zone the engine cannot load",
			mutate:   func(in *Input) { schedule(in, scheduleInventory).Spec.TimeZone = "Mars/Olympus_Mons" },
			schedule: scheduleInventory,
		},
		{
			name:     "the host's own clock",
			mutate:   func(in *Input) { schedule(in, scheduleInventory).Spec.TimeZone = "Local" },
			schedule: scheduleInventory,
		},
		{
			name:     "each, on a unit with no agent seat directly in it",
			mutate:   func(in *Input) { schedule(in, scheduleInventory).Spec.Target.UnitRef.Name = "anvils" },
			schedule: scheduleInventory,
		},
		{
			name:     "lead, on a unit whose inherited lead is a person",
			mutate:   func(in *Input) { unit(in, "rnd").Spec.LeadRef.Name = "wile" },
			schedule: scheduleReport,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "org-chart").input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
			verr, ok := errors.AsType[*engine.ValidationError](err)
			if !ok {
				t.Fatalf("the dry run answered %v, want a refusal", err)
			}
			if len(verr.Problems) == 0 {
				t.Fatalf("the refusal names no problem: %v", verr)
			}
			want := ObjectRef{Kind: KindSchedule, Name: c.schedule}
			for _, p := range verr.Problems {
				if got, ok := doc.Attribute(PathOf(p.Segments)); !ok || got != want {
					t.Errorf("the engine's problem at %q (%s) is attributed to %v, %v; want %v", p.Path, p.Message, got, ok, want)
				}
			}
		})
	}
}

// TestContractCLIAgentRefusalsReachTheProvider has the engine's dry run refuse
// the mistakes of a provider's CLI no API rule can see, because each turns
// on another object, and checks that the document attributes every problem to
// the LLMProvider, which is where the controller reports it.
func TestContractCLIAgentRefusalsReachTheProvider(t *testing.T) {
	const providerAgent = "claude-agent"
	client := enginetest.Start(t).Client()
	for _, c := range []struct {
		name     string
		mutate   func(*Input)
		provider string
	}{
		{
			// The run is a sandbox run, and the company has no sandbox to
			// run it in.
			name:     "agent mode in a company with no sandbox",
			mutate:   func(in *Input) { in.Company.Spec.Sandbox = v1alpha1.SandboxSettings{} },
			provider: providerAgent,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := companyCaseNamed(t, "cli-agent").input(t)
			c.mutate(in)
			doc, err := Company(in)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
			verr, ok := errors.AsType[*engine.ValidationError](err)
			if !ok {
				t.Fatalf("the dry run answered %v, want a refusal", err)
			}
			if len(verr.Problems) == 0 {
				t.Fatalf("the refusal names no problem: %v", verr)
			}
			want := ObjectRef{Kind: KindLLMProvider, Name: c.provider}
			for _, p := range verr.Problems {
				if got, ok := doc.Attribute(PathOf(p.Segments)); !ok || got != want {
					t.Errorf("the engine's problem at %q (%s) is attributed to %v, %v; want %v", p.Path, p.Message, got, ok, want)
				}
			}
		})
	}
}

// TestContractProvidersOfTwoCLIsShareALoginName has the engine run two
// providers of different CLIs under one loginName, as the loginName field and
// the cli-agents guide say it does: the render gives both the loginName's
// state_dir, and Crewlet keeps each CLI's files in a directory of its own
// under it (<state_dir>/<agent>), so they share the directory and nothing in
// it.
//
// The engine once refused the pair twice over: its validation refused two
// CLIs on one state_dir, and a workspace registry that lives as long as the
// process held each directory under the first CLI to open it and refused
// every other. A dry run reaches only the first. The engine builds a
// revision's providers, and opens their workspaces, when a node applies the
// revision, after the write has been answered: a node that cannot build it
// keeps the revision before, and only its own account says so. So each case
// applies its revisions to a running engine and waits for that account
// (awaitApplied).
//
// The first case names the pair in the company's first revision. The second
// runs the case as rendered, then moves the reviewer's provider to another
// CLI under claude-agent's loginName: the registry, holding that directory
// under Claude Code since the first revision, refused the move on every node
// that had run it, and applied it only on a node restarted since. The guide
// says the move applies with no restart, so it is applied to the engine that
// runs the first revision, which enginetest never restarts.
func TestContractProvidersOfTwoCLIsShareALoginName(t *testing.T) {
	const (
		providerClaude = "claude-agent"
		providerCodex  = "codex"
	)

	t.Run("named in the first revision", func(t *testing.T) {
		in := companyCaseNamed(t, "cli-agent").input(t)
		login := loginNameOf(modelProvider(in, providerClaude))
		modelProvider(in, providerCodex).Spec.LoginName = login
		doc, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		requireOneLoginTwoCLIs(t, "the rendered document", doc.YAML, login, providerClaude, providerCodex)

		client := enginetest.Start(t).Client()
		putCredentials(t, client, doc)
		applied, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{
			Document: doc.YAML, CreateOnly: true, Summary: "two CLIs under one loginName",
		})
		if err != nil {
			t.Fatalf("the engine refused two CLIs under one loginName: %v", err)
		}
		awaitApplied(t, client, applied)
	})

	t.Run("moved onto it in a live revision", func(t *testing.T) {
		in := companyCaseNamed(t, "cli-agent").input(t)
		first, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		client := enginetest.Start(t).Client()
		putCredentials(t, client, first)
		running, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{
			Document: first.YAML, CreateOnly: true, Summary: "the cli-agent company",
		})
		if err != nil {
			t.Fatalf("applying the cli-agent company: %v", err)
		}
		awaitApplied(t, client, running)

		// The reviewer's provider moves from Codex to OpenCode, which reads
		// the same OpenAI model as <provider>/<model>, and onto the login
		// name of claude-agent, a Claude Code provider: OpenCode opens a
		// login of its own beside Claude Code's in the directory Claude
		// Code has held since the first revision.
		login := loginNameOf(modelProvider(in, providerClaude))
		codex := modelProvider(in, providerCodex)
		codex.Spec.Agent = v1alpha1.CLIAgentOpenCode
		codex.Spec.Model = "openai/" + codex.Spec.Model
		codex.Spec.LoginName = login
		moved, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		want := requireOneLoginTwoCLIs(t, "the rendered move", moved.YAML, login, providerClaude, providerCodex)

		putCredentials(t, client, moved)
		applied, err := client.ApplyConfig(t.Context(), engine.ApplyRequest{
			Document: moved.YAML, IfMatch: running.RevisionID,
			Summary: "codex moves to OpenCode under claude-agent's loginName",
		})
		if err != nil {
			t.Fatalf("the engine refused a provider moved to another CLI under another provider's loginName: %v", err)
		}
		active := awaitApplied(t, client, applied)
		got := loginEntries(t, active.Document)
		for _, name := range []string{providerClaude, providerCodex} {
			if got[name] != want[name] {
				t.Errorf("the engine runs providers.llm.%s as %+v, want %+v, as the move renders it", name, got[name], want[name])
			}
		}
	})
}

// loginNameOf is the loginName p's state_dir is rendered from: its own, else
// its metadata.name.
func loginNameOf(p *v1alpha1.LLMProvider) string { return cmp.Or(p.Spec.LoginName, p.Name) }

// putCredentials puts a stand-in value in the engine's secret store under
// every name doc refers to, as the secret sync does before the operator
// applies a document.
func putCredentials(t *testing.T, client *engine.Client, doc *Document) {
	t.Helper()
	for _, k := range doc.Refs {
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(dummyCredential(k))); err != nil {
			t.Fatal(err)
		}
	}
}

// loginEntry is what a providers.llm entry says about where its CLI keeps its
// files.
type loginEntry struct {
	Agent    string `json:"agent"`
	StateDir string `json:"state_dir"`
}

// loginEntries reads every providers.llm entry of a company document under the
// keys Crewlet reads, so a check of them holds whatever the YAML's layout,
// order or quoting.
func loginEntries(t *testing.T, document []byte) map[string]loginEntry {
	t.Helper()
	var doc struct {
		Providers struct {
			LLM map[string]loginEntry `json:"llm"`
		} `json:"providers"`
	}
	if err := yaml.Unmarshal(document, &doc); err != nil {
		t.Fatalf("reading the company document: %v\n%s", err, document)
	}
	return doc.Providers.LLM
}

// requireOneLoginTwoCLIs fails the test unless document, described as what,
// gives each of the providers the state_dir of loginName login, and drives
// no two of them on one CLI. It returns the document's entries.
func requireOneLoginTwoCLIs(t *testing.T, what string, document []byte, login string, providers ...string) map[string]loginEntry {
	t.Helper()
	entries := loginEntries(t, document)
	dir := CLIStateRoot + "/" + login
	byAgent := map[string]string{}
	for _, name := range providers {
		e, ok := entries[name]
		switch {
		case !ok:
			t.Fatalf("%s has no providers.llm.%s:\n%s", what, name, document)
		case e.StateDir != dir:
			t.Fatalf("%s gives providers.llm.%s the state_dir %q, want loginName %s's, %q:\n%s", what, name, e.StateDir, login, dir, document)
		case byAgent[e.Agent] != "":
			t.Fatalf("%s drives providers.llm.%s and %s on one CLI, %q, want two CLIs:\n%s", what, byAgent[e.Agent], name, e.Agent, document)
		}
		byAgent[e.Agent] = name
	}
	return entries
}

// awaitApplied waits until the engine runs the revision applied wrote, by the
// engine's own account, and returns its active configuration.
//
// The write's answer says only that the revision was stored and activated:
// the node builds it afterwards, its providers and their workspaces included.
// A node that cannot build it keeps serving the revision before, its posture
// turns diverged (isolated, on an engine alone) and its record in /fleet
// names the failed epoch with the engine's account of why, which this fails
// on at once. Applied is all of: /health has applied the write's epoch and
// serves it (status ok, posture serve); the node's record names that epoch
// and revision, applied ok rather than degraded, which is a node that needs a
// restart; /ready answers; and GET /config names the revision as the one
// active.
func awaitApplied(t *testing.T, client *engine.Client, applied engine.ApplyResult) engine.ActiveConfig {
	t.Helper()
	want := engine.ApplyRecord{Epoch: applied.Epoch, RevisionID: applied.RevisionID, Status: engine.ApplyOK}
	var last string
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the engine has not applied revision %s (epoch %d) after a minute: %s", applied.RevisionID, applied.Epoch, last)
		}
		health, err := client.Health(t.Context())
		if err != nil {
			last = fmt.Sprintf("GET /health: %v", err)
			continue
		}
		fleet, err := client.Fleet(t.Context())
		if err != nil {
			last = fmt.Sprintf("GET /fleet: %v", err)
			continue
		}
		record := fleet.LastApply()
		if health.Posture.Diverged() || (record.Status.Failed() && record.Epoch >= applied.Epoch) {
			t.Fatalf("the engine did not apply revision %s (epoch %d): it reports status %q and posture %q at applied epoch %d, "+
				"and its record of its last apply is %+v", applied.RevisionID, applied.Epoch, health.Status, health.Posture,
				health.AppliedEpoch, record)
		}
		if health.AppliedEpoch < applied.Epoch || record != want ||
			health.Status != "ok" || !health.Configured || health.Posture != engine.PostureServe {
			last = fmt.Sprintf("it reports status %q, configured %t, posture %q at applied epoch %d, and the record %+v; "+
				"want status ok, configured, posture serve at epoch %d, and the record %+v",
				health.Status, health.Configured, health.Posture, health.AppliedEpoch, record, applied.Epoch, want)
			continue
		}
		if err := client.Ready(t.Context()); err != nil {
			last = fmt.Sprintf("GET /ready: %v", err)
			continue
		}
		active, err := client.ActiveConfig(t.Context())
		if err != nil {
			last = fmt.Sprintf("GET /config: %v", err)
			continue
		}
		if active.RevisionID != applied.RevisionID {
			t.Fatalf("the engine applied epoch %d, and its active revision is %s, want %s", applied.Epoch, active.RevisionID, applied.RevisionID)
		}
		return active
	}
}
