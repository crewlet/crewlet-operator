package render

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// instructionsFields are the members of an assistant.instructions block that
// carry a preset, each the API's enum and the engine's.
var instructionsFields = []string{"length", "format"}

// crdInstructions is the Company CRD's schema of spec.assistant.instructions.
func crdInstructions(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_companies.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd map[string]any
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	node := any(crd)
	for _, key := range []string{"spec", "versions", "0", "schema", "openAPIV3Schema", keyProperties, "spec",
		keyProperties, "assistant", keyProperties, "instructions", keyProperties} {
		switch n := node.(type) {
		case map[string]any:
			node = n[key]
		case []any:
			node = n[0]
		}
	}
	out, _ := node.(map[string]any)
	if out == nil {
		t.Fatal("the Company CRD has no spec.assistant.instructions")
	}
	return out
}

// engineInstructions is `crewlet schema company`'s AssistantInstructions.
func engineInstructions(t *testing.T) map[string]any {
	t.Helper()
	defs, _ := engineSchema(t, tierCompany)["$defs"].(map[string]any)
	def, _ := defs["AssistantInstructions"].(map[string]any)
	props, _ := def[keyProperties].(map[string]any)
	if props == nil {
		t.Fatal("the engine's company schema declares no AssistantInstructions")
	}
	return props
}

// presets is the non-empty enum values a schema property lists.
func presets(property any) []string {
	p, _ := property.(map[string]any)
	enum, _ := p["enum"].([]any)
	var out []string
	for _, v := range enum {
		if s, _ := v.(string); s != "" {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// TestContractAssistantInstructionsFollowTheEngine holds
// spec.assistant.instructions to the engine's assistant.instructions both
// ways: the lengths and formats the Company CRD offers render as exactly the
// ones `crewlet schema company` lists, the CRD bounds the text where the
// engine does, and a Company naming each preset, with a text at the bound in
// two-byte characters, is one `crewlet validate` admits.
func TestContractAssistantInstructionsFollowTheEngine(t *testing.T) {
	crd, eng := crdInstructions(t), engineInstructions(t)
	rendered := map[string]func(string) string{
		"length": func(v string) string { return answerLength(v1alpha1.AnswerLength(v)) },
		"format": func(v string) string { return answerFormat(v1alpha1.AnswerFormat(v)) },
	}
	offered := map[string][]string{}
	for _, field := range instructionsFields {
		offered[field] = presets(crd[field])
		var got []string
		for _, v := range offered[field] {
			got = append(got, rendered[field](v))
		}
		slices.Sort(got)
		if want := presets(eng[field]); len(want) == 0 || !slices.Equal(got, want) {
			t.Errorf("the API's %ss render as %v; the engine takes %v", field, got, want)
		}
	}
	crdText, _ := crd["text"].(map[string]any)
	engText, _ := eng["text"].(map[string]any)
	bound, _ := engText["maxLength"].(float64)
	if crdBound, _ := crdText["maxLength"].(float64); bound == 0 || crdBound != bound {
		t.Fatalf("the CRD bounds the text at %v characters, the engine at %v", crdText["maxLength"], engText["maxLength"])
	}

	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)
	for i, length := range offered["length"] {
		in := companyCaseNamed(t, "assistant").input(t)
		in.Company.Spec.Assistant.Instructions = v1alpha1.AssistantInstructions{
			Length: v1alpha1.AnswerLength(length),
			Format: v1alpha1.AnswerFormat(offered["format"][i%len(offered["format"])]),
		}
		if i == 0 {
			in.Company.Spec.Assistant.Instructions.Text = strings.Repeat("é", int(bound))
		}
		doc, err := Company(in)
		if err != nil {
			t.Fatal(err)
		}
		env := bootstrapEnv(tierA)
		for _, k := range doc.Refs {
			env[secretsync.StoreName(k)] = dummyCredential(k)
		}
		validate(t, env, companyCaseNamed(t, "assistant").advisories,
			"-config", config, "-company", write(t, "company.yaml", doc.YAML))
	}
}

// TestContractACredentialInInstructionsIsRefusedOnTheCompany has the engine's
// dry run refuse a company's instructions whose text holds what looks like a
// credential, which no API rule can tell apart, each problem on the Company,
// where the operator reports it, and none repeating the credential into the
// Company's conditions.
func TestContractACredentialInInstructionsIsRefusedOnTheCompany(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a1B2", 9)
	in := companyCaseNamed(t, "assistant").input(t)
	in.Company.Spec.Assistant.Instructions = v1alpha1.AssistantInstructions{Text: "Sign every answer with " + secret}
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	client := enginetest.Start(t).Client()
	_, err = client.ApplyConfig(t.Context(), engine.ApplyRequest{Document: doc.YAML, DryRun: true})
	verr, ok := errors.AsType[*engine.ValidationError](err)
	if !ok || len(verr.Problems) == 0 {
		t.Fatalf("the dry run answered %v, want a refusal", err)
	}
	want := ObjectRef{Kind: KindCompany, Name: in.Company.Name}
	for _, p := range verr.Problems {
		if got, ok := doc.Attribute(PathOf(p.Segments)); !ok || got != want {
			t.Errorf("the engine's problem at %q (%s) is attributed to %v, %v; want %v", p.Path, p.Message, got, ok, want)
		}
		if p.Path != "assistant.instructions.text" {
			t.Errorf("the engine's problem is at %q, want assistant.instructions.text", p.Path)
		}
		if strings.Contains(p.Message, secret) {
			t.Errorf("the engine's problem repeats the credential: %s", p.Message)
		}
	}
}
