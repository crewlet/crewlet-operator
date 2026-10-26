package crd_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// setupFilePaths are the paths TestSetupFilePathsFollowTheEngine offers as a
// sandbox setup step's file:
//   - ones the seat's E2B box writes: relative to its home, under ~/,
//     absolute inside the home or not, one that climbs out of the home with
//     .., and one with a backslash after the ~, which the box's tilde check
//     takes as it takes a / but which stays in the file's name (~\.npmrc is
//     the file \.npmrc in /home/user), and a zero-width space, which is no
//     white space to Crewlet or the API;
//   - the home and each directory above it, however .. and repeated
//     separators spell them;
//   - another account's home in its shapes: a name, a second ~, a character
//     outside ASCII after the ~;
//   - white space alone, which names no file.
var setupFilePaths = []string{
	".npmrc", "./.npmrc", "a/../.npmrc", ".config/gh/hosts.yml", "~/.npmrc", "~//.npmrc", `~\.npmrc`,
	"/home/user/.npmrc", "/home/user//.npmrc", "/home/user/../user/.npmrc", "/tmp/x", "/etc/x", "/root/.npmrc",
	"/home/x", "../x", "~/../x", "\u200b",
	"~", "~/", "~//", ".", "./", "..", "../..", "a/..", "/", "//", "/home", "/home/", "/home/user",
	"/home/user/", "/home//user", "/home/user/.", "/home/user/x/..", "/home/../home/user", "~/..", "~/../..",
	"~/a/../..",
	"~root/.npmrc", "~root", "~~", "~.npmrc", "~é/x",
	" ", "\t", " \u3000 ",
}

// namesTheHome are the shapes of setupFilePaths that name the box's home or a
// directory above it, which the API admits and Crewlet refuses: whether a
// path names one depends on how .. and repeated separators resolve, which no
// rule the API server runs can follow. Crewlet's refusal is reported instead:
// on the Company's ConfigApplied for a step of the Company's own, as every one
// this test offers is, and on the Seat's Applied for a step of a Seat's.
var namesTheHome = []string{
	"~", "~/", "~//", ".", "./", "..", "../..", "a/..", "/", "//", "/home", "/home/", "/home/user",
	"/home/user/", "/home//user", "/home/user/.", "/home/user/x/..", "/home/../home/user", "~/..", "~/../..",
	"~/a/../..",
}

// TestSetupFilePathsFollowTheEngine holds the API's verdict on a sandbox
// setup step's file path to Crewlet's, both ways where that is honest: every
// path the API refuses Crewlet refuses too, so no Company is refused a file
// its seats' boxes would write; and every path Crewlet refuses the API
// refuses too, but for namesTheHome, each of which Crewlet must still
// refuse. Every box the operator renders is an E2B one (default_run_in: e2b,
// and a box-mode CLI runs in E2B too), so Crewlet judges each path as such
// a box writes it.
func TestSetupFilePathsFollowTheEngine(t *testing.T) {
	namespace := newNamespace(t)
	steps := make([]v1.SandboxSetupStep, len(setupFilePaths))
	for i, path := range setupFilePaths {
		steps[i] = setupStepWritingFile(fmt.Sprintf("s%02d", i), path)
	}
	refused := engineRefusesSetupSteps(t, steps, func(i int, rest []engine.Segment) bool {
		// At the step's files, or at the file's own entry under them.
		return len(rest) >= 1 && rest[0].Key == "files" &&
			(len(rest) == 1 || (len(rest) == 2 && rest[1].Key == setupFilePaths[i]))
	})
	apiRefused := 0
	for i, path := range setupFilePaths {
		c := company(namespace, fmt.Sprintf("f%02d", i))
		c.Spec.Sandbox = v1.SandboxSettings{
			E2B:   v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
			Setup: []v1.SandboxSetupStep{setupStepWritingFile("files", path)},
		}
		apiAdmits := admitted(t, k8sClient, c)
		crewletRefuses := refused[i]
		home := slices.Contains(namesTheHome, path)
		if !apiAdmits {
			apiRefused++
		}
		switch {
		case !apiAdmits && !crewletRefuses:
			t.Errorf("the API refuses the setup file %q, and crewlet validate takes it", path)
		case !apiAdmits && home:
			t.Errorf("%q is listed as one the API admits for naming the home, and the API refuses it", path)
		case apiAdmits && crewletRefuses && !home:
			t.Errorf("the API admits the setup file %q, and crewlet validate refuses it, so the Company is "+
				"applied nowhere", path)
		case !crewletRefuses && home:
			t.Errorf("%q is listed as naming the box's home, and crewlet validate takes it", path)
		}
	}
	if apiRefused == 0 {
		t.Error("the API refuses none of the setup files: its rules on a path are tried nowhere")
	}
}

// setupStepWritingFile is a setup step named name that writes one file, at
// path, from a Secret.
func setupStepWritingFile(name, path string) v1.SandboxSetupStep {
	return v1.SandboxSetupStep{
		Name:  name,
		Files: []v1.SandboxFile{{Path: path, ValueFrom: fromSecret("company-sandbox", "file")}},
	}
}

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
	in := sandboxCompany(steps, &v1.SeatSandbox{})
	refused := map[int]bool{}
	for _, p := range validateProblems(t, in) {
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

// sandboxCompany is the render input of a company whose sandbox runs steps,
// with an agent seat, coder, whose sandbox is sandbox.
func sandboxCompany(steps []v1.SandboxSetupStep, sandbox *v1.SeatSandbox) *render.Input {
	in := probeCompany()
	in.Company.Spec.Sandbox = v1.SandboxSettings{
		E2B:   v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
		Setup: steps,
	}
	in.Seats[0].Spec.Sandbox = sandbox
	return in
}

// probeCompany is the render input of a company the engine has nothing to
// say about: an agent seat, coder, on one provider, and a person on a seat of
// their own.
func probeCompany() *render.Input {
	in := &render.Input{
		Company:         company("probe", companyName),
		Class:           engineClass("", "basic"),
		LLMProviders:    []v1.LLMProvider{*llmProvider("probe", providerName)},
		StoreName:       secretsync.StoreName,
		ProvisionedName: secretsync.ProvisionedName,
	}
	in.Company.UID = types.UID("00000000-0000-4000-8000-000000000002")
	in.Company.Spec.EngineClassName = "basic"
	coder := seat("probe", "coder")
	coder.Spec.Goal = "Write the code."
	coder.Spec.LLM.Default = providers(providerName)
	// A person on a seat of their own, as askEngine's, so that the engine
	// has nothing to say about the chart.
	founder := seat("probe", "founder")
	founder.Spec.Kind = v1.SeatKindHuman
	founder.Spec.Contact = v1.SeatContact{GitHubLogin: "probe-founder"}
	in.Seats = []v1.Seat{*coder, *founder}
	return in
}

// validateProblem is one problem `crewlet validate` finds in a document.
type validateProblem struct {
	Segments []engine.Segment `json:"segments"`
	Message  string           `json:"message"`
}

// validateProblems renders in and hands the document to `crewlet validate`,
// with every Secret key it references set to a value, as the secret sync sets
// it, and every provisioned credential minted, and returns what it refuses.
func validateProblems(t *testing.T, in *render.Input) []validateProblem {
	t.Helper()
	doc, err := render.Company(in)
	if err != nil {
		t.Fatalf("rendering the document: %v", err)
	}
	return validateDocument(t, doc, doc.YAML)
}

// validateDocument hands document, doc's YAML or an edit of it, to `crewlet
// validate`, with every Secret key doc references set to a value and every
// provisioned credential minted, and returns what it refuses.
func validateDocument(t *testing.T, doc *render.Document, document []byte) []validateProblem {
	t.Helper()
	problems, _ := validateVerdict(t, doc, document)
	return problems
}

// validateVerdict is [validateDocument]'s whole verdict: what `crewlet
// validate` refuses, and what it warns about.
func validateVerdict(t *testing.T, doc *render.Document, document []byte) (problems, warnings []validateProblem) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "company.yaml")
	if err := os.WriteFile(file, document, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), enginetest.Binary(t), "validate", "-json", "-tier", "company", file)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}
	for _, k := range doc.Refs {
		cmd.Env = append(cmd.Env, secretsync.StoreName(k)+"=probe-"+k.Name+"-"+k.Key)
	}
	for _, name := range doc.Provisioned {
		cmd.Env = append(cmd.Env, name+"=minted-"+name)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exit := (*exec.ExitError)(nil); !errors.As(err, &exit) {
			t.Fatalf("running crewlet validate: %v", err)
		}
	}
	var verdict struct {
		Problems []validateProblem `json:"problems"`
		Warnings []validateProblem `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &verdict); err != nil {
		t.Fatalf("crewlet validate printed no verdict (%v):\n%s\n%s", err, stdout.String(), stderr.String())
	}
	return verdict.Problems, verdict.Warnings
}
