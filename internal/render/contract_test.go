package render

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	for _, c := range []struct {
		name, from, to string
	}{
		{"public on the API port", fmt.Sprintf("public:\n    port: %d", PublicPort), fmt.Sprintf("public:\n    port: %d", APIPort)},
		{"public on the leaf port", fmt.Sprintf("public:\n    port: %d", PublicPort), fmt.Sprintf("public:\n    port: %d", LeafPort)},
		{"public beside no API port", fmt.Sprintf("  port: %d\n  public:", APIPort), "  port: 0\n  public:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(string(doc.YAML), c.from) {
				t.Fatalf("the rendered Tier A holds no %q:\n%s", c.from, doc.YAML)
			}
			mutated := strings.Replace(string(doc.YAML), c.from, c.to, 1)
			got, stdout, _, _ := verdict(t, bootstrapEnv(doc), "-tier", "bootstrap", write(t, "crewlet.yaml", []byte(mutated)))
			if got.Valid || !strings.Contains(stdout, "api.public.port") {
				t.Fatalf("crewlet validate did not refuse api.public.port:\n%s\n%s", mutated, stdout)
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
				validate(t, bootstrapEnv(doc), nil, args...)
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
		if err := client.PutSecret(t.Context(), secretsync.StoreName(k), []byte(dummyCredential(k)), ""); err != nil {
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
	// The guide's company is run by its agents alone.
	validate(t, env, []advisory{advisoryNoHumanSeat},
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
// the mistakes of a CLIAgent provider no API rule can see, because each turns
// on another object, and checks that the document attributes every problem to
// the LLMProvider, which is where the controller reports it.
func TestContractCLIAgentRefusalsReachTheProvider(t *testing.T) {
	const (
		providerAgent = "claude-agent"
		providerCodex = "codex"
	)
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
		{
			// One login is one CLI's credential files.
			name: "two providers of different CLIs share one login",
			mutate: func(in *Input) {
				var login string
				for _, p := range in.LLMProviders {
					if p.Name == providerAgent {
						login = p.Spec.CLI.LoginName
					}
				}
				for i := range in.LLMProviders {
					if in.LLMProviders[i].Name == providerCodex {
						in.LLMProviders[i].Spec.CLI.LoginName = login
					}
				}
			},
			provider: providerCodex,
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
