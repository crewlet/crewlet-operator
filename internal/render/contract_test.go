package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	if !got.Valid || len(got.Problems) > 0 || err != nil {
		t.Errorf("crewlet validate refused the document (exit %v):\n%s\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "config_reference_unresolved") {
		t.Errorf("a reference resolved to nothing:\n%s", stderr.String())
	}
	// A warning is a document Crewlet runs but doubts: a lead or a manages
	// entry naming nobody, a backup nobody owns. Every rendered document
	// is meant to be one Crewlet has nothing to say about.
	for _, w := range got.Warnings {
		t.Errorf("crewlet validate warns: %s", w)
	}
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

// testKeyMaterial is a keyring key: base64 of 32 bytes, which is what
// Crewlet checks the material is. Its value is irrelevant.
const testKeyMaterial = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

// bootstrapEnv answers every variable a Tier A's pod is given, with a
// dummy of the right shape.
func bootstrapEnv(doc *BootstrapDocument) map[string]string {
	env := map[string]string{}
	for _, e := range doc.Env {
		switch {
		case e.Name == EnvNodeID:
			env[e.Name] = "nimbus-data-0"
		case strings.HasPrefix(e.Name, envSecretKeyPrefix):
			env[e.Name] = testKeyMaterial
		default:
			env[e.Name] = "dummy-" + strings.ToLower(e.Name)
		}
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
