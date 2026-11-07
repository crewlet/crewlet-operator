package enginetest_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// Spelled in a document (`issue_reports`), in an object (`issueReports`) and in Go.
var (
	issueReportsOnInYAML = regexp.MustCompile(`(?m)^\s*(issueReports|issue_reports):\s*\n\s*enabled:\s*true\b`)
	issueReportsOnInline = regexp.MustCompile(`(issueReports|issue_reports)"?:\s*\{\s*"?enabled"?:\s*true`)
	issueReportsOnInGo   = regexp.MustCompile(`IssueReports(\.Enabled\s*=[^=]|:\s*\S*\{\s*Enabled:)`)
)

// outsideAnEngine are the test files that may turn the switch on, because what
// they build reaches no running engine.
var outsideAnEngine = map[string]string{
	"test/crd/company_test.go":             "the API server's admission alone, in envtest",
	"internal/render/issuereports_test.go": "a render and `crewlet validate`, which starts nothing",
}

func setsIssueReportsOn(path, text string) bool {
	if strings.HasSuffix(path, ".go") {
		return issueReportsOnInGo.MatchString(text)
	}
	return issueReportsOnInYAML.MatchString(text) || issueReportsOnInline.MatchString(text)
}

// TestNoFixtureTurnsIssueReportsOn: the suites run Crewlet's main, which may report,
// and where a report goes cannot be redirected from here. So no fixture, sample or
// manifest anything could hand to an engine sets the switch, and neither does a test
// beyond the files named above. Being inert was the only thing stopping test traffic
// from filing public issues, before main reported.
func TestNoFixtureTurnsIssueReportsOn(t *testing.T) {
	t.Parallel()
	root := os.DirFS(filepath.Join("..", "..", ".."))
	checked := 0
	err := fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "dist":
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml", ".md", ".json":
		case ".go":
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
		default:
			return nil
		}
		if _, allowed := outsideAnEngine[path]; allowed || path == "internal/engine/enginetest/issuereports_test.go" {
			return nil
		}
		text, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		checked++
		if setsIssueReportsOn(path, string(text)) {
			t.Errorf("%s turns issue reports on: an engine under test files real issues", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Errorf("read %d files, which is not this tree", checked)
	}
	for path := range outsideAnEngine {
		if _, err := fs.Stat(root, path); err != nil {
			t.Errorf("outsideAnEngine names %s: %v", path, err)
		}
	}
}

// The scan and the refusal can each fail.
func TestEverySpellingOfTheSwitchIsSeen(t *testing.T) {
	t.Parallel()
	for path, text := range map[string]string{
		"an-object.yaml":     "spec:\n  diagnostics:\n    issueReports:\n      enabled: true\n",
		"a-document.yaml":    "diagnostics:\n  issue_reports:\n    enabled: true\n",
		"an-inline.yaml":     "diagnostics: {issue_reports: {enabled: true}}\n",
		"a-document.json":    `{"diagnostics":{"issue_reports":{"enabled":true}}}`,
		"a-manifest.md":      "```yaml\n  issueReports:\n    enabled: true\n```\n",
		"a-field_test.go":    "c.Spec.Diagnostics.IssueReports.Enabled = &enabled\n",
		"a-literal_test.go":  "IssueReports: v1.IssueReportsSettings{Enabled: &on}\n",
		"a-raw-yaml_test.go": "IssueReports.Enabled = ptr(true)\n",
	} {
		if !setsIssueReportsOn(path, text) {
			t.Errorf("%s was not seen: %q", path, text)
		}
	}
	for path, text := range map[string]string{
		"off.yaml":     "diagnostics:\n  issue_reports:\n    enabled: false\n",
		"absent.yaml":  "diagnostics: {}\n",
		"schema.yaml":  "issueReports:\n  description: whether\n  properties:\n    enabled:\n      type: boolean\n",
		"read_test.go": "if spec.Diagnostics.IssueReports.Enabled == nil {\n",
	} {
		if setsIssueReportsOn(path, text) {
			t.Errorf("%s was taken for the switch: %q", path, text)
		}
	}
	for text, want := range map[string]bool{
		"diagnostics:\n  issue_reports:\n    enabled: true\n":  true,
		"diagnostics:\n  issue_reports:\n    enabled: false\n": false,
		"name: acme\n": false,
		"":             false,
	} {
		if got := enginetest.ReportsIssues([]byte(text)); got != want {
			t.Errorf("ReportsIssues(%q) = %v, want %v", text, got, want)
		}
	}
}

// TestStartRefusesACompanyThatReports: the refusal comes before anything runs.
func TestStartRefusesACompanyThatReports(t *testing.T) {
	t.Parallel()
	defer func() {
		msg, ok := recover().(fatal)
		if !ok {
			t.Fatal("Start ran an engine with issue reports on")
		}
		if !strings.Contains(string(msg), "issue reports on") {
			t.Errorf("Start failed with %q, which does not say why", msg)
		}
	}()
	enginetest.Start(fatalT{t}, enginetest.WithCompany([]byte("diagnostics:\n  issue_reports:\n    enabled: true\n")))
}
