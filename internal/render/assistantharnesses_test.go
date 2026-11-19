package render

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// assistantHarnessesSentence is where docs/guides/harnesses.md names the
// harnesses Ask Crewlet's model may drive: the code spans from "Today that is"
// to the colon that ends the list.
var assistantHarnessesSentence = regexp.MustCompile(`(?s)Today that is\s+(.*?):`)

// assistantHarnessesStated reads the harnesses the guide says Ask Crewlet's
// model may drive.
func assistantHarnessesStated(t *testing.T) []v1alpha1.Harness {
	t.Helper()
	const guide = "docs/guides/harnesses.md"
	data, err := os.ReadFile(filepath.Join("..", "..", guide))
	if err != nil {
		t.Fatal(err)
	}
	m := assistantHarnessesSentence.FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s does not say which harnesses Ask Crewlet's model may drive (%q)", guide, assistantHarnessesSentence)
	}
	spans := regexp.MustCompile("`([^`]+)`").FindAllSubmatch(m[1], -1)
	out := make([]v1alpha1.Harness, 0, len(spans))
	for _, span := range spans {
		out = append(out, v1alpha1.Harness(span[1]))
	}
	slices.Sort(out)
	return out
}

// offeredHarnesses are the harnesses an LLMProvider may drive, read from the
// generated CRD's enum, so a harness the API gains is tried with the rest.
func offeredHarnesses(t *testing.T) []v1alpha1.Harness {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_llmproviders.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd struct {
		Spec struct {
			Versions []struct {
				Schema struct {
					OpenAPIV3Schema struct {
						Properties struct {
							Spec struct {
								Properties struct {
									Harness struct {
										Enum []v1alpha1.Harness `json:"enum"`
									} `json:"harness"`
								} `json:"properties"`
							} `json:"spec"`
						} `json:"properties"`
					} `json:"openAPIV3Schema"`
				} `json:"schema"`
			} `json:"versions"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("the LLMProvider CRD serves %d versions, want one", len(crd.Spec.Versions))
	}
	out := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties.Spec.Properties.Harness.Enum
	if len(out) == 0 {
		t.Fatal("the LLMProvider CRD lists no harness")
	}
	return out
}

// TestContractAssistantHarnessesFollowTheEngine holds the guide's list of the
// harnesses Ask Crewlet's model may drive to the engine's own verdict, harness
// by harness: for each harness the API offers, a Company whose
// spec.assistant.model names one provider of that harness, its webTools
// unset, is handed to `crewlet validate`. The engine runs that provider for
// Ask Crewlet with its web tools off whatever the provider says, so it
// refuses only a harness whose run is not shown to leave the model with no
// tool of its own (a shell or a file reader on the engine host reads what no
// request of the person's returned), naming assistant.model, and takes every
// other; the guide must name exactly the harnesses it takes. The API cannot
// refuse such a model itself, since the rule turns on another object's spec,
// so the dry run's refusal on the Company is the only one a tenant meets, and
// the guide is what tells them beforehand.
func TestContractAssistantHarnessesFollowTheEngine(t *testing.T) {
	// assistantProbe is the provider each case's model names.
	const assistantProbe = "probe"
	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
	if err != nil {
		t.Fatal(err)
	}
	config := write(t, "crewlet.yaml", tierA.YAML)

	var takes []v1alpha1.Harness
	for _, harness := range offeredHarnesses(t) {
		in := companyCaseNamed(t, "minimal").input(t)
		in.LLMProviders = []v1alpha1.LLMProvider{{Spec: v1alpha1.LLMProviderSpec{
			CompanyRef:          v1alpha1.CompanyReference{Name: in.Company.Name},
			LLMProviderSettings: v1alpha1.LLMProviderSettings{Harness: harness, Model: probeModel},
		}}}
		in.LLMProviders[0].Name = assistantProbe
		in.Company.Spec.Assistant = v1alpha1.CompanyAssistant{
			Model: v1alpha1.LLMProviderReference{Kind: v1alpha1.LLMProviderKindLLMProvider, Name: assistantProbe}}
		doc, err := Company(in)
		if err != nil {
			t.Fatalf("%s: %v", harness, err)
		}
		env := bootstrapEnv(tierA)
		for _, k := range doc.Refs {
			env[secretsync.StoreName(k)] = dummyCredential(k)
		}
		// A provider with no sign-in is refused as well, so the verdict
		// read here is the model's alone: whether any problem names
		// assistant.model.
		got, stdout, _, _ := verdict(t, env, "-config", config, "-company", write(t, "company.yaml", doc.YAML))
		refused := strings.Contains(stdout, "assistant.model")
		if !refused {
			takes = append(takes, harness)
		}
		if got.Valid && refused {
			t.Errorf("%s: the verdict is valid and names assistant.model:\n%s", harness, stdout)
		}
	}
	slices.Sort(takes)
	if stated := assistantHarnessesStated(t); !slices.Equal(takes, stated) {
		t.Errorf("the engine runs Ask Crewlet on %v; docs/guides/harnesses.md says %v", takes, stated)
	}
}
