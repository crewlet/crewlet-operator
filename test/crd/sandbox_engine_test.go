package crd_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// stepShape is a setup step TestSetupStepShapesFollowTheEngine offers.
type stepShape struct {
	name string
	step v1.SandboxSetupStep
}

// setupStepShapes are the steps TestSetupStepShapesFollowTheEngine offers:
// names of white space alone and their neighbours, a step that does nothing,
// and a step that does one thing of each kind Crewlet counts.
func setupStepShapes() []stepShape {
	file := []v1.SandboxFile{{Path: ".npmrc", ValueFrom: fromSecret("company-sandbox", "file")}}
	env := []v1.SecretEnvVar{{Name: "NPM_TOKEN", ValueFrom: fromSecret("company-sandbox", "token")}}
	return []stepShape{
		{"a name of a space", v1.SandboxSetupStep{Name: " ", Brief: "b"}},
		{"a name of tabs and a wide space", v1.SandboxSetupStep{Name: "\t\u3000\t", Brief: "b"}},
		{"a name of a zero-width space", v1.SandboxSetupStep{Name: "\u200b", Brief: "b"}},
		{"a name padded with spaces", v1.SandboxSetupStep{Name: " login ", Brief: "b"}},
		{"a step that does nothing", v1.SandboxSetupStep{Name: "nothing"}},
		{"a step with only a brief", v1.SandboxSetupStep{Name: "brief", Brief: "b"}},
		{"a step with only a brief of a space", v1.SandboxSetupStep{Name: "blank-brief", Brief: " "}},
		{"a step with only a command", v1.SandboxSetupStep{Name: "command", Commands: []string{"true"}}},
		{"a step with only an env variable", v1.SandboxSetupStep{Name: "env", Env: env}},
		{"a step with only a file", v1.SandboxSetupStep{Name: "file", Files: file}},
	}
}

// TestSetupStepShapesFollowTheEngine holds the API's verdict on a setup
// step's name, and on whether it does anything, to Crewlet's, both ways: the
// API refuses a step exactly when crewlet validate refuses it. Crewlet's
// rules here hold on every step whatever box it reaches, so nothing is left
// to Crewlet alone.
func TestSetupStepShapesFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	shapes := setupStepShapes()
	steps := make([]v1.SandboxSetupStep, len(shapes))
	for i := range shapes {
		steps[i] = shapes[i].step
		// Crewlet keys nothing on a step's name, but the API does: a
		// Company's steps are a map by name.
		if steps[i].Name == "" {
			t.Fatalf("shape %q has no name", shapes[i].name)
		}
	}
	refused := engineRefusesSetupSteps(t, steps, func(_ int, rest []engine.Segment) bool {
		// At the step itself (it does nothing), or at its name.
		return len(rest) == 0 || (len(rest) == 1 && rest[0].Key == "name")
	})
	apiRefused := 0
	for i, shape := range shapes {
		c := company(namespace, fmt.Sprintf("s%02d", i))
		c.Spec.Sandbox = v1.SandboxSettings{
			E2B:   v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
			Setup: []v1.SandboxSetupStep{shape.step},
		}
		apiAdmits := admitted(t, k8sClient, c)
		if !apiAdmits {
			apiRefused++
		}
		switch {
		case apiAdmits && refused[i]:
			t.Errorf("%s: the API admits it, and crewlet validate refuses it, so the Company is applied nowhere",
				shape.name)
		case !apiAdmits && !refused[i]:
			t.Errorf("%s: the API refuses it, and crewlet validate takes it", shape.name)
		}
	}
	if apiRefused == 0 {
		t.Error("the API refuses none of the setup steps: its rules on a step are tried nowhere")
	}
}

// engineRefusesSetupSteps renders one company document whose sandbox runs
// steps, and an agent seat whose box is given every one, and reports which
// steps `crewlet validate` refuses, by index. at says whether a refusal of
// step i at rest (the place below providers.sandbox.setup[i]) is one the
// caller expects. Every reference resolves, and a refusal anywhere else fails
// the test, so a step is refused for itself alone.
func engineRefusesSetupSteps(
	t *testing.T, steps []v1.SandboxSetupStep, at func(i int, rest []engine.Segment) bool,
) map[int]bool {
	t.Helper()
	in := &render.Input{
		Company:         company("probe", companyName),
		Class:           engineClass("", "basic"),
		LLMProviders:    []v1.LLMProvider{*llmProvider("probe", providerName)},
		StoreName:       secretsync.StoreName,
		ProvisionedName: secretsync.ProvisionedName,
	}
	in.Company.UID = types.UID("00000000-0000-4000-8000-000000000002")
	in.Company.Spec.EngineClassName = "basic"
	in.Company.Spec.Sandbox = v1.SandboxSettings{
		E2B:   v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
		Setup: steps,
	}
	coder := seat("probe", "coder")
	coder.Spec.Goal = "Write the code."
	coder.Spec.LLM.Default = providers(providerName)
	coder.Spec.Sandbox = &v1.SeatSandbox{}
	// A person on a seat of their own, as askEngine's, so that the engine
	// has nothing to say about the chart.
	founder := seat("probe", "founder")
	founder.Spec.Kind = v1.SeatKindHuman
	founder.Spec.Contact = v1.SeatContact{GitHubLogin: "probe-founder"}
	in.Seats = []v1.Seat{*coder, *founder}
	doc, err := render.Company(in)
	if err != nil {
		t.Fatalf("rendering the setup steps: %v", err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "company.yaml")
	if err := os.WriteFile(file, doc.YAML, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), "validate", "-json", "-tier", "company", file)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for _, k := range doc.Refs {
		cmd.Env = append(cmd.Env, secretsync.StoreName(k)+"=probe-"+k.Name+"-"+k.Key)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exit := (*exec.ExitError)(nil); !errors.As(err, &exit) {
			t.Fatalf("running crewlet validate: %v", err)
		}
	}
	var verdict struct {
		Problems []struct {
			Segments []engine.Segment `json:"segments"`
			Message  string           `json:"message"`
		} `json:"problems"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &verdict); err != nil {
		t.Fatalf("crewlet validate printed no verdict (%v):\n%s\n%s", err, stdout.String(), stderr.String())
	}
	refused := map[int]bool{}
	for _, p := range verdict.Problems {
		s := p.Segments
		// providers.sandbox.setup[i], and what is under it.
		step := len(s) >= 4 && s[0].Key == "providers" && s[1].Key == "sandbox" && s[2].Key == "setup" &&
			s[3].IsIndex && s[3].Index < len(steps)
		if !step || !at(s[3].Index, s[4:]) {
			t.Fatalf("crewlet validate refused the document elsewhere than this test expects (%s): %s",
				render.PathOf(s), p.Message)
		}
		refused[s[3].Index] = true
	}
	return refused
}
