package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// testKeyMaterial is a keyring key: base64 of 32 bytes, which is what
// Crewlet checks the material is. Its value is irrelevant.
const testKeyMaterial = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

var update = flag.Bool("update", false, "rewrite the golden files under testdata from the current render")

// testNamespace is the namespace every namespaced test object is in, and
// testUID the UID of every test Company: the samples carry neither, and the
// API server would give them both.
const (
	testNamespace = "tenant-nimbus"
	testUID       = types.UID("3f2a9c1e-6b1d-4e7a-9a52-0c8d5f1e2b47")
)

// The names of the Nimbus sample objects the tests break and look up.
const (
	sampleCompany   = "nimbus"
	seatAna         = "ana"
	seatCTO         = "cto"
	seatJane        = "jane"
	unitEngineering = "engineering"
	unitPlatform    = "platform"
	// sampleGitHub names both the GitHub MCPServer and the GitHub
	// Integration.
	sampleGitHub = "github"
	// sampleToolsets is a setting of the GitHub MCPServer's, a header
	// naming the toolsets it offers.
	sampleToolsets = "X-MCP-Toolsets"
	// sampleGitLab names both the GitLab MCPServer and the GitLab
	// Integration.
	sampleGitLab = "gitlab"
	// samplePlatformProvider is the class's platform model provider, which
	// the seat ana's auxiliary chain names.
	samplePlatformProvider = "platform-claude"

	schedulePlatformStandup = "platform-standup"
	scheduleCIHealth        = "ci-health"
)

// caseSeatPools is the company case that places seats in every kind of pool.
const caseSeatPools = "seat-pools"

// caseOptionalKeys is the company case whose blocks list keys that may go
// without their value.
const caseOptionalKeys = "optional-keys"

// caseAtlassian is the company case on Atlassian Cloud, whose Jira and
// Confluence Integrations are named jira and confluence, and seatLena its
// person, tied to her Atlassian account.
const (
	caseAtlassian        = "atlassian"
	caseAtlassianJira    = "jira"
	caseAtlassianWiki    = "confluence"
	seatLena             = "lena"
	lenaAtlassianCloudID = "557058:0d3b7c6e-2a41-4c8f-9e1d-5b6a7c8d9e0f"
)

// keyName is the key an entry of the company document is named by: a role's,
// a unit's, an engineConfig block's.
const keyName = "name"

// keyLLM is the key of a model chain in the company document: a seat's, and
// the assistant's.
const keyLLM = "llm"

// objects is what a set of manifests holds, sorted into the slices an Input
// is made of.
type objects struct {
	class   *v1alpha1.EngineClass
	company *v1alpha1.Company
	input   Input
}

// load decodes the multi-document manifests in files. Decoding is strict, so
// a field the API does not have fails the test instead of vanishing from the
// render. Manifests that define no EngineClass render with
// testdata/class.yaml, the smallest class the API admits.
func load(t *testing.T, files ...string) objects {
	t.Helper()
	var o objects
	decodeFile := func(file string) {
		data, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatal(err)
		}
		for _, manifest := range regexp.MustCompile(`(?m)^---\s*$`).Split(string(data), -1) {
			var meta metav1.TypeMeta
			if err := yaml.Unmarshal([]byte(manifest), &meta); err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			decode := func(into any) {
				if err := yaml.UnmarshalStrict([]byte(manifest), into); err != nil {
					t.Fatalf("%s: %s: %v", file, meta.Kind, err)
				}
			}
			switch meta.Kind {
			case "":
				// A manifest of comments only.
			case KindEngineClass:
				o.class = &v1alpha1.EngineClass{}
				decode(o.class)
			case KindCompany:
				o.company = &v1alpha1.Company{}
				decode(o.company)
				o.company.Namespace = testNamespace
				o.company.UID = testUID
			case KindUnit:
				var u v1alpha1.Unit
				decode(&u)
				o.input.Units = append(o.input.Units, u)
			case KindSeat:
				var s v1alpha1.Seat
				decode(&s)
				o.input.Seats = append(o.input.Seats, s)
			case KindLLMProvider:
				var p v1alpha1.LLMProvider
				decode(&p)
				o.input.LLMProviders = append(o.input.LLMProviders, p)
			case KindMCPServer:
				var m v1alpha1.MCPServer
				decode(&m)
				o.input.MCPServers = append(o.input.MCPServers, m)
			case KindIntegration:
				var i v1alpha1.Integration
				decode(&i)
				o.input.Integrations = append(o.input.Integrations, i)
			case KindSchedule:
				var s v1alpha1.Schedule
				decode(&s)
				o.input.Schedules = append(o.input.Schedules, s)
			default:
				t.Fatalf("%s: a manifest of kind %s", file, meta.Kind)
			}
		}
	}
	for _, file := range files {
		decodeFile(file)
	}
	if o.class == nil {
		decodeFile(filepath.Join("testdata", "class.yaml"))
	}
	if o.company == nil {
		t.Fatalf("%v: want a Company", files)
	}
	o.input.Company = o.company
	o.input.Class = o.class
	// The secret sync's own naming rather than a stand-in, so the contract
	// tests hand the engine the names a cluster would.
	o.input.StoreName = secretsync.StoreName
	o.input.ProvisionedName = secretsync.ProvisionedName
	return o
}

// golden compares got with the golden file, or rewrites the file under
// -update.
func golden(t *testing.T, file string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		t.Fatalf("%v; run go test ./internal/render -update to write it", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the render differs from %s; if the change is intended, run go test ./internal/render -update and review the diff.\n--- got\n%s", file, got)
	}
}
