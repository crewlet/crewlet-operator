package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
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
// environment (and PATH and HOME), and fails the test on any problem or
// warning, and on any reference the environment did not answer: an unresolved reference is
// only a warning to Crewlet, and a missing variable would be a pod that
// boots with an empty token.
func validate(t *testing.T, env map[string]string, args ...string) {
	t.Helper()
	got, stdout, stderr, err := verdict(t, env, args...)
	if !got.Valid || len(got.Problems) > 0 || err != nil {
		t.Errorf("crewlet validate refused the document (exit %v):\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "config_reference_unresolved") {
		t.Errorf("a reference resolved to nothing:\n%s", stderr)
	}
	// A warning is a document Crewlet runs but doubts: a lead or a manages
	// entry naming nobody, a backup nobody owns. Every rendered document
	// is meant to be one Crewlet has nothing to say about.
	for _, w := range got.Warnings {
		t.Errorf("crewlet validate warns: %s", w)
	}
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

// bootstrapEnv answers every variable a Tier A's pod is given: its name.
func bootstrapEnv(doc *BootstrapDocument) map[string]string {
	env := map[string]string{}
	for _, e := range doc.Env {
		env[e.Name] = "nimbus-data-0"
	}
	return env
}

// TestContractBootstrap validates every Tier A case on its own.
func TestContractBootstrap(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			doc, err := Bootstrap(c.input(t))
			if err != nil {
				t.Fatal(err)
			}
			validate(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML))
		})
	}
}

// TestContractStreamFloor ties the API's floor on data.storage.size to
// Crewlet's on stream.store_max_bytes: the smallest volume an EngineClass
// admits renders a stream Crewlet accepts, and a volume a gibibyte smaller
// one it refuses, so the floor is the engine's and not a guess.
func TestContractStreamFloor(t *testing.T) {
	for _, c := range []struct {
		size   string
		accept bool
	}{
		{size: "7Gi", accept: true},
		{size: "6Gi", accept: false},
	} {
		t.Run(c.size, func(t *testing.T) {
			in := bootstrapCases()[0].input(t)
			in.VolumeSize = resource.MustParse(c.size)
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"-tier", "bootstrap", write(t, "crewlet.yaml", doc.YAML)}
			if c.accept {
				validate(t, bootstrapEnv(doc), args...)
				return
			}
			got, stdout, stderr, _ := verdict(t, bootstrapEnv(doc), args...)
			if got.Valid || !strings.Contains(stdout+stderr, "store_max_bytes") {
				t.Errorf("crewlet validate did not refuse stream.store_max_bytes for a %s volume:\n%s\n%s", c.size, stdout, stderr)
			}
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
			validate(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		})
	}
}

// dummyCredential is a stand-in value for a referenced Secret key.
func dummyCredential(k secretref.Key) string {
	return "dummy-" + k.Name + "-" + k.Key
}

// TestContractNoCredentialLeavesThroughAReference runs the engine as the
// operator configures a data pod (the rendered Tier A, and only the pod's
// name in its environment) and applies a company document that names the
// variables the operator once put the credentials in, in a field that is no
// credential: an MCP server's URL, which the engine expands from its secret
// store and then from its environment before it connects. The engine must
// reach the server with none of its credentials in the request.
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
	tierA, err := Bootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(tierA.Env) != 1 || tierA.Env[0].Name != EnvNodeID {
		t.Fatalf("the pod's environment is %v; this test gives the engine its name alone", tierA.Env)
	}
	e := enginetest.Start(t, enginetest.WithTierA(tokens.Operator, tokens.Access, func(port int, dir string) []byte {
		// The data pod's paths and address, moved into the test's
		// directory and onto the loopback interface.
		var doc map[string]any
		if err := yaml.Unmarshal(tierA.YAML, &doc); err != nil {
			t.Fatal(err)
		}
		api := doc["api"].(map[string]any)
		api["host"], api["port"] = "127.0.0.1", port
		doc["store"].(map[string]any)["path"] = filepath.Join(dir, "store", "crewlet.db")
		doc["stream"].(map[string]any)["store_dir"] = filepath.Join(dir, "stream")
		out, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}, EnvNodeID+"=nimbus-data-0"))

	// Every variable a credential has ever been given to the engine in.
	url := listener.URL + "/mcp/${CREWLET_TOKEN_OPERATOR}/${CREWLET_TOKEN_ACCESS}/${CREWLET_SECRET_KEY_K1}/" +
		"${CREWLET_S3_ACCESS_KEY_ID}/${CREWLET_S3_SECRET_ACCESS_KEY}"
	company := fmt.Appendf(nil, "name: Nimbus\nmcp_servers:\n  - name: probe\n    transport: http\n    url: %q\n", url)
	if _, err := e.Client().ApplyConfig(t.Context(), engine.ApplyRequest{Document: company, CreateOnly: true, Summary: "a probe for leaked credentials"}); err != nil {
		t.Fatalf("applying the company document: %v", err)
	}

	select {
	case got := <-requests:
		seen := []request{got}
		for drained := false; !drained; {
			select {
			case r := <-requests:
				seen = append(seen, r)
			case <-time.After(time.Second):
				drained = true
			}
		}
		for _, r := range seen {
			for _, credential := range []string{tokens.Operator, tokens.Access, testKeyMaterial} {
				if strings.Contains(r.url+r.header+r.body, credential) {
					t.Errorf("the engine sent a credential to the MCP server's URL: %+v", r)
				}
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the engine never reached the MCP server, so the test proves nothing")
	}
}
