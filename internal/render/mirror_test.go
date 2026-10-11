package render

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// mirror is a function of the engine whose decision the operator copies
// where no engine test can show the engine making it: the verdicts on a
// seat's code work (CodeWorkOf) and on an agent-mode executor (ExecutorOf),
// and the LLMProvider API's rule on an Agent-mode CLI's login, which Crewlet
// reaches only when a seat launches a run, never on a write or in an answer
// of its API. A contract test cannot hold those to the engine, so the
// operator holds the code it read: the digest of each function as it was
// when it was read against the operator files that copy it.
type mirror struct {
	// file and function are the function's file in the crewlet module and
	// its name, as enginetest.Fingerprint takes it.
	file, function string
	// against is every operator file that copies the decision, each of
	// which a change to the function sends its reader to.
	against []string
	// decides is what the operator copies from it, for the reader the
	// test sends to it.
	decides string
	// digest is the function's enginetest.Fingerprint as it was read, at
	// the crewlet version read.
	digest, read string
}

// The operator files that copy a decision of the engine's, this table's own
// file, the engine files most of it is read from, and the crewlet version
// every mirror below was first read at.
const (
	codeWorkGo  = "internal/render/codework.go"
	executorGo  = "internal/render/executor.go"
	headsGo     = "internal/render/seatpod_providers.go"
	mirrorTable = "internal/render/mirror_test.go"
	sandboxGo   = "internal/engine/sandbox.go"
	agentRunGo  = "internal/engine/agentrun.go"
	doctorGo    = "internal/providers/llm/cliagent/doctor.go"
	companyGo   = "internal/config/company.go"
	firstRead   = "v0.1.1-0.20261010171839-4e45c6b992a2"
)

// agentLogin is every operator file that copies how an Agent-mode run's CLI
// signs in inside the remote sandbox, and is refused when nothing does: the
// LLMProvider API's rule on spec.cli of mode Agent, the guide that states
// it, and the CRD suite's reading of the engine's verdicts on it (its
// holdVerdicts). The renderer's code-work verdict copies the same functions
// for a run_sandbox launch.
var agentLogin = []string{
	"api/v1alpha1/llmprovider_types.go", "docs/guides/cli-agents.md", "test/crd/cliagent_engine_test.go",
}

// mirrored is every engine function the operator's run-time verdicts copy a
// decision of, and no other: a function changed here sends somebody to read
// it, so one that copies nothing would only cost a read. What the engine
// shows without a run is held by a contract test instead, never here: the
// head of a seat's chain (TestContractChainlessSeatRunsOnTheFirstProviderAlone),
// the CodingAgentMismatch it refuses on a write (TestContractCodeWorkOnWrite),
// the sign-in a CLIAgent provider's settings give it (test/crd's
// TestCLIAgentSignInFollowsTheEngine), and the lists TestCodeWorkFollowsTheEngineSource
// reads.
//
// Most of them are the launch's own code. Two are config's write-time twins
// of a launch decision, read because the launch's own is spread through a
// registry or a manager: (*Company).sandboxProvider, which crewlet's
// TestBothPathsToTheSandboxEntryAgree holds to the head the phase registry
// gives a launch, and (*Company).codingAgent, which nothing in crewlet holds
// to the launch's order, so that order is read too (Launch, NewManager,
// buildSandbox).
var mirrored = []mirror{
	{
		file: sandboxGo, function: "(*launcher).Launch",
		against: []string{codeWorkGo},
		decides: "a run_sandbox launch runs the head of the seat's sandbox phase with role.sandbox.coding_agent or " +
			"the catalogue's default, and refuses before a box exists what newCodingRun and sandboxCredentials refuse",
		digest: "53107d935c906582978a09c5690b861b69e761ced6d63dbba559e211fdaca62f",
		read:   firstRead,
	},
	{
		file: sandboxGo, function: "buildSandbox",
		against: []string{codeWorkGo},
		decides: "the catalogue a launch reads is built from providers.sandbox, with its default_coding_agent as the " +
			"default coding agent and its setup steps as the setup every run is given, and a company with no " +
			"sandbox has none, so offers no seat run_sandbox",
		digest: "73a2a28ba0a87e759b935cb74009831d5c1c1e89a2d9c1eb008e574287b3f687",
		read:   firstRead,
	},
	{
		file: "internal/sandbox/manager.go", function: "NewManager",
		against: []string{codeWorkGo},
		decides: "a catalogue that names no default coding agent runs DefaultCodingAgent, Claude Code, whose value " +
			"TestCodeWorkFollowsTheEngineSource holds",
		digest: "124cf3ef185dfc577ccf34d74b499d71c99dfb42414c5fecf4cd1802f6a38d89",
		read:   firstRead,
	},
	{
		file: sandboxGo, function: "newCodingRun",
		against: append([]string{codeWorkGo}, agentLogin...),
		decides: "a CLIAgent head another CLI's coding agent cannot run (CodingAgentMismatch), ClaudeCode on a " +
			"head that is no Anthropic provider (SandboxModelUnsupported), and a CLIAgent head's run, a " +
			"run_sandbox launch or an agent-mode run alike, carrying its provider's own sign-in (SandboxEnv) " +
			"under what the seat's environment sets",
		digest: "baeb63889217d2f01b87cce93be5b9e5d4a1682935599f38dbb94d3080b524dc",
		read:   firstRead,
	},
	{
		file: sandboxGo, function: "claudeCodeOnForeignEntry",
		against: []string{codeWorkGo},
		decides: "ClaudeCode on a head that is no Anthropic provider runs on the seat's own sign-in when it has one, " +
			"and is refused otherwise (SandboxModelUnsupported)",
		digest: "9240ccaa5ccdff89f42c0beb0b40e847f5035bd4a7cd8315f4e9fe8c67358670",
		read:   firstRead,
	},
	{
		file: sandboxGo, function: "signsIn",
		against: []string{codeWorkGo},
		decides: "a variable of the seat's environment signs ClaudeCode in when it holds a value",
		digest:  "d67892b7c02c1053a5a38c588fe26d93a39b9525b01879a0d6dc3ef923280798",
		read:    firstRead,
	},
	{
		file: sandboxGo, function: "sandboxCredentials",
		against: append([]string{codeWorkGo}, agentLogin...),
		decides: "a CLIAgent head whose login cannot follow the run into a remote sandbox is refused unless the run's " +
			"environment signs its CLI in: a run_sandbox launch (SandboxLoginNotPortable), and at phase Execute an " +
			"agent-mode run, which the LLMProvider API refuses on a Subscription login no variable carries",
		digest: "898c033d6df0b834eea9f102df1095424243de2578e288fe00220e572a3a6686",
		read:   firstRead,
	},
	{
		file: sandboxGo, function: "(*Engine).sandboxEnv",
		against: []string{codeWorkGo},
		decides: "a run's environment is the env of the catalogue's and the seat's setup steps, then the seat's " +
			"role.sandbox.env",
		digest: "47a6af92c5c2fd83aab61a8c6c6012ca63b572b35f801edc2313c71222ea183c",
		read:   firstRead,
	},
	{
		file: doctorGo, function: "(*Provider).CredentialEnvNames",
		against: append([]string{codeWorkGo}, agentLogin...),
		decides: "the variables that sign a CLI in inside a box: its token and key variables, and for an env " +
			"sign-in CLI the credential-named variables of its cli.env",
		digest: "091501e8498192be36fc8619232c358a2276c32bae8f412e7197fcc0a8b89667",
		read:   firstRead,
	},
	{
		file: doctorGo, function: "(*Provider).SandboxEnv",
		against: append([]string{codeWorkGo}, agentLogin...),
		decides: "a CLIAgent provider's own sign-in follows the run into the box when it is one of those variables",
		digest:  "f81f2bc4b7fca996eaac300c1ab530d2e357500bd4b76145551412788cc9ddb9",
		read:    firstRead,
	},
	{
		file: companyGo, function: "(*Company).sandboxProvider",
		against: []string{codeWorkGo},
		decides: "config's twin of the head the phase registry gives a seat's code work, which crewlet's " +
			"TestBothPathsToTheSandboxEntryAgree holds to it: the first configured provider of its llm_sandbox, " +
			"else of its llm, else the company's fallback",
		digest: "e0d59baf916e2892840471b03a0461b7d2e3149c76ab07975606b6f8bec57f04",
		read:   firstRead,
	},
	{
		file: companyGo, function: "(*Company).codingAgent",
		against: []string{codeWorkGo},
		decides: "config's twin, on a write, of the coding agent a launch runs, by which it refuses " +
			"CodingAgentMismatch: the seat's own coding_agent, else the catalogue's default, else Claude Code; " +
			"nothing in crewlet holds it to the launch's (Launch, NewManager)",
		digest: "ea194ad5e34803abb6f736efecb3fea988605855d66d9e7a5dfc2daecf3e9873",
		read:   firstRead,
	},
	{
		file: agentRunGo, function: "(*Company).executorRuntime",
		against: []string{executorGo, headsGo},
		decides: "a seat's executor runs as an agentic run of a CLI when the head of its executor's chain alone is a " +
			"CLIAgent provider in agent mode",
		digest: "b03aa59b6706b0dcad0a4c2fb167e5cf22243dffa6a2034ddb6b10a3a640a8dd",
		read:   firstRead,
	},
	{
		file: agentRunGo, function: "(*agentLauncher).LaunchExecutor",
		against: append([]string{executorGo}, agentLogin...),
		decides: "an agent-mode turn on a node that serves no tool bridge is refused, never run on the engine's own " +
			"tool loop (AgentModeBridgeUnsupported, NotExposed), and so is one whose CLI login cannot follow it " +
			"into the remote sandbox (sandboxCredentials at phase Execute)",
		digest: "918a3f2ddb7e5327f9b05f2dbea7454f97ade87ebee8ba5a5c946b470aff56d9",
		read:   firstRead,
	},
	{
		file: agentRunGo, function: "(*agentLauncher).executorRun",
		against: append([]string{executorGo}, agentLogin...),
		decides: "an agent-mode run is the executor itself: it runs on the head of the seat's executor chain " +
			"(phase Execute), never on its llm_sandbox, so that head's sign-in is the one that must reach the box",
		digest: "49679ff36a054ab3dea460a4a5c850c4fafea190b74e37c3a6e72637b25df448",
		read:   firstRead,
	},
	{
		file: "internal/api/mcpbridge/build.go", function: "Build",
		against: []string{executorGo},
		decides: "a node given no CREWLET_MCP_BRIDGE_URL builds no tool bridge",
		digest:  "99b4eabe6c10792f110b1391e9c768d802b18f077c07ce01477c4055f98e5b54",
		read:    firstRead,
	},
}

// TestMirroredEngineFunctionsAreUnchanged holds every function in mirrored to
// the engine the suites run: one whose digest moved is a function somebody
// reads again against the operator file named, and one that is gone is a
// decision somebody finds again. The test fails until its new digest is
// recorded, so the read is made before the change lands, not when a seat
// first meets the difference.
func TestMirroredEngineFunctionsAreUnchanged(t *testing.T) {
	source, version := openSource(t, enginetest.Source(t)), enginetest.Version(t)
	for i := range mirrored {
		if problem := mirrored[i].check(source, version); problem != "" {
			t.Error(problem)
		}
	}
}

// openSource is the module source at dir, every read confined to it.
func openSource(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// check is what is wrong with m in the crewlet module source at version, or
// nothing when the function there is the one that was read.
func (m *mirror) check(source *os.Root, version string) string {
	at := fmt.Sprintf("%s %s, in crewlet %s,", m.file, m.function, version)
	copies := strings.Join(m.against, ", ")
	src, err := source.ReadFile(m.file)
	if err == nil {
		var digest string
		if digest, err = enginetest.Fingerprint(m.file, src, m.function); err == nil {
			if digest == m.digest {
				return ""
			}
			return fmt.Sprintf("%s is not the function the operator's copies of it were read against, which copy "+
				"from it that %s. `git log -p %s..%s -- %s` in a crewlet checkout shows what changed since. Read it "+
				"against each of %s, make the operator follow it where it no longer does, then record digest %q "+
				"and read %q for it in %s",
				at, m.decides, gitRev(m.read), gitRev(version), m.file, copies, digest, version, mirrorTable)
		}
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, enginetest.ErrNoFunction) {
		return fmt.Sprintf("%s is gone (%v), and the operator copies from it that %s. Find where the engine "+
			"decides that now, read it against each of %s, and name that function in %s in place of this one, "+
			"with any digest at first: the next run fails with the digest and version to record for it",
			at, err, m.decides, copies, mirrorTable)
	}
	return fmt.Sprintf("%s cannot be read: %v", at, err)
}

// pseudoRev is the commit a pseudo-version names: its last twelve hex digits,
// after the fourteen of its time, which every form of one ends in
// (v0.0.0-<time>-<commit>, v0.1.1-0.<time>-<commit> after a release,
// v0.2.0-rc.1.0.<time>-<commit> after a pre-release).
var pseudoRev = regexp.MustCompile(`[-.]\d{14}-([0-9a-f]{12})$`)

// gitRev is a crewlet module version as git names it: the commit of a
// pseudo-version, or the tag a release is.
func gitRev(version string) string {
	if m := pseudoRev.FindStringSubmatch(version); m != nil {
		return m[1]
	}
	return strings.TrimSuffix(version, "+incompatible")
}

// TestAMirroredFunctionThatMovedFails holds check to its job, on a module
// source of its own: the function as read passes, and a digest that differs,
// a function the file no longer declares and a file that is gone each fail,
// naming the function, the operator file to read it against, and what to
// record.
func TestAMirroredFunctionThatMovedFails(t *testing.T) {
	dir := t.TempDir()
	const file, decides = sandboxGo, "a pick"
	src := []byte("package engine\n\nfunc pick(n int) int { return n }\n")
	if err := os.MkdirAll(filepath.Join(dir, "internal", "engine"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), src, 0o600); err != nil {
		t.Fatal(err)
	}
	source := openSource(t, dir)
	digest, err := enginetest.Fingerprint(file, src, "pick")
	if err != nil {
		t.Fatal(err)
	}
	const version = "v0.1.1-0.20261011000000-0123456789ab"
	read := mirror{file: file, function: "pick", against: []string{codeWorkGo, executorGo}, decides: decides,
		digest: digest, read: "v0.1.1-0.20261010000000-ba9876543210"}
	if problem := read.check(source, version); problem != "" {
		t.Fatalf("the function as read fails: %s", problem)
	}

	moved := read
	moved.digest = strings.Repeat("0", len(digest))
	gone := read
	gone.function = "choose"
	elsewhere := read
	elsewhere.file = "internal/engine/launch.go"
	for name, c := range map[string]struct {
		m    mirror
		want []string
	}{
		"a digest that differs": {moved, []string{
			file, "pick", "crewlet " + version, codeWorkGo, executorGo, decides,
			"git log -p ba9876543210..0123456789ab -- " + file,
			fmt.Sprintf("record digest %q and read %q", digest, version), mirrorTable,
		}},
		"a function that is gone": {gone, []string{
			file, "choose", "is gone", codeWorkGo, executorGo, decides, mirrorTable,
			"the digest and version to record",
		}},
		"a file that is gone": {elsewhere, []string{
			"internal/engine/launch.go", "is gone", codeWorkGo, executorGo, mirrorTable,
		}},
	} {
		problem := c.m.check(source, version)
		if problem == "" {
			t.Errorf("%s passes", name)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(problem, want) {
				t.Errorf("%s fails with %q, which does not say %q", name, problem, want)
			}
		}
	}

	// Every form of pseudo-version names its commit, and a release its tag.
	const release, commit = "v0.2.0", "c298682e6417"
	for version, want := range map[string]string{
		release:                                      release,
		release + "+incompatible":                    release,
		release + "-rc.1":                            release + "-rc.1",
		"v0.0.0-20261011151308-" + commit:            commit,
		"v0.1.1-0.20261011151308-" + commit:          commit,
		release + "-rc.1.0.20261011151308-" + commit: commit,
	} {
		if got := gitRev(version); got != want {
			t.Errorf("gitRev(%s) = %s, want %s", version, got, want)
		}
	}
}
