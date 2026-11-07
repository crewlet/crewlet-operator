package render

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// A company's opt-in reaches its document, and the engine's validator reads it.
// Here rather than as a company case, because every case is also applied to a
// running engine (TestDiffContract), and an engine under test must never have the
// switch on: its binary is Crewlet's main, which may report.
func TestAnOptInToIssueReportsReachesTheDocument(t *testing.T) {
	c := companyCaseNamed(t, "org-chart")
	silent, err := Company(c.input(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(silent.YAML), "issue_reports") {
		t.Error("a company that says nothing renders the block")
	}

	on := true
	in := c.input(t)
	in.Company.Spec.Diagnostics.IssueReports.Enabled = &on
	doc, err := Company(in)
	if err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Diagnostics struct {
			IssueReports struct {
				Enabled bool `json:"enabled"`
			} `json:"issue_reports"`
		} `json:"diagnostics"`
	}
	if err := yaml.Unmarshal(doc.YAML, &tree); err != nil {
		t.Fatal(err)
	}
	if !tree.Diagnostics.IssueReports.Enabled {
		t.Errorf("the opt-in did not reach the document:\n%s", doc.YAML)
	}

	tierA, err := Bootstrap(bootstrapCases()[0].input(t))
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
	validate(t, env, c.advisories, "-config", write(t, "crewlet.yaml", tierA.YAML), "-company", write(t, "company.yaml", doc.YAML))
}
