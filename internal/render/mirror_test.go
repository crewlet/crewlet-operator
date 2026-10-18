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
// of its API; and which Atlassian deployment the company's Jira and
// Confluence are on, and the identity each names a person by there, which
// it reaches only when it connects to them and when a webhook arrives, and
// against which the render reports a person's Atlassian account
// (atlassianAccounts); and how a name says it holds a credential, which the
// tool-server API's refusal of a literal value copies and the engine never
// applies to a tool server's values, taking any literal there; and whether
// the address a data pod's engine is told is on loopback, which the render
// holds a data pod back on and the engine shows only for the addresses a test
// tries. A contract test cannot hold those to the engine, so the operator
// holds the code it read: the digest of each function as it was when it was
// read against the operator files that copy it.
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

	contactGo       = "internal/render/contact.go"
	integrationsMd  = "docs/guides/integrations.md"
	jiraClient      = "internal/jira/client.go"
	confluenceGo    = "internal/confluence/client.go"
	atlassianHostGo = "internal/atlassianhost/atlassianhost.go"
	integrationsGo  = "internal/config/integrations.go"
	atlassianRead   = "v0.1.1-0.20261012064104-c5d26af1f2b1"
	cloudRuleRead   = "v0.1.1-0.20261012225007-e3313b767056"
	addressRead     = "v0.1.1-0.20261012231821-14c93cab2ee6"

	credentialNameRead = "v0.1.1-0.20261012231821-14c93cab2ee6"

	// identityRead is crewlet#195's engine, which every mirror it changed
	// was read again at: the run_sandbox launch and the agent-mode launch
	// find the seat's sandbox by handle and take their setup through
	// boxSetup, which writes a file whose body is one whole ${VAR} as that
	// value, byte for byte (every setup file the render writes is one); the
	// agent-mode launch refuses a surface bound to no seat, a fault of its
	// own assembly no configuration reaches; sandboxEnv takes the org's
	// sandbox type; and the tool bridge takes the keyring's material and
	// still builds nothing without CREWLET_MCP_BRIDGE_URL.
	identityRead = "v0.1.1-0.20261016092541-a677e638f19b"

	// bootBindRead is crewlet#257's engine, which binds api.port, and
	// api.public where the node serves one, before its boot: whether a node
	// has a tool bridge is now one function (mcpbridge.Configured) that
	// crewlet run asks before the engine exists, and which node binds
	// api.public is another (api.PublicListener). A node given no
	// CREWLET_MCP_BRIDGE_URL still builds no bridge, and one without the
	// ingress role still binds api.public only for a bridge it serves.
	bootBindRead = "v0.1.1-0.20261018071555-ae6e5c16db2e"
	bootstrapGo  = "internal/render/bootstrap.go"
	mcpbridgeGo  = "internal/api/mcpbridge/build.go"
	bootGo       = "internal/api/boot.go"

	// signInRead is the engine the loopback judgement was first read at.
	signInRead  = "v0.1.1-0.20261018171039-c28489c9c9ca"
	apiConfigGo = "internal/config/api.go"
)

// signInPosture is every operator file that copies where Crewlet lets a
// second factor be optional without the acknowledgement: the render, which
// holds a data pod back on it, the EngineClass API's spec.signIn, which says
// so, and the guide that tells a platform when to acknowledge it.
var signInPosture = []string{bootstrapGo, "api/v1alpha1/engineclass_types.go", "docs/guides/people.md"}

// atlassianAccount is every operator file that copies which deployment the
// company's Jira and Confluence are on, and what names a person there: the
// render's report on a Seat's atlassianAccount, and the guide that tells a
// person which value to give.
var atlassianAccount = []string{contactGo, "api/v1alpha1/seat_types.go", integrationsMd}

// cloudIDGateway is every operator file that copies which gateway a cloud_id
// is read through: those of atlassianAccount, to which a product named by
// one is on Cloud, and the Integration API, which says whose gateway it is.
var cloudIDGateway = append([]string{"api/v1alpha1/integration_types.go"}, atlassianAccount...)

// credentialNames is every operator file that copies how the engine reads a
// credential's name: the API's rules that read one (the LLMProvider API's over
// a CLI's sign-in, and the tool-server API's refusal of a literal value), the
// render's copy of their pattern, and the guide and the page that state the
// rule of a literal value.
var credentialNames = []string{
	"api/v1alpha1/llmprovider_types.go", "api/v1alpha1/mcpserver_types.go", "api/v1alpha1/unit_types.go",
	codeWorkGo, "docs/guides/tool-servers.md", "docs/concepts/credentials.md",
}

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
// TestCLIAgentSignInFollowsTheEngine), the deployment its validation reads a
// Jira or Confluence address on (TestContractAtlassianDeploymentIsTheEngines),
// and the lists TestCodeWorkFollowsTheEngineSource,
// TestAtlassianDeploymentFollowsTheEngineSource and
// TestCredentialNameFollowsTheEngineSource read.
//
// Most of them are the launch's own code. Two are config's write-time twins
// of a launch decision, read because the launch's own is spread through a
// registry or a manager: (*Company).sandboxProvider, which crewlet's
// TestBothPathsToTheSandboxEntryAgree holds to the head the phase registry
// gives a launch, and (*Company).codingAgent, which nothing in crewlet holds
// to the launch's order, so that order is read too (Launch, NewManager,
// buildSandbox).
//
// The Atlassian ones are the chain from a company's Jira or Confluence block
// to the deployment the engine's clients for it run on, which it shows only
// when it connects: the address a client reads (jiraBaseURL,
// confluenceBaseURL, ResolvedBaseURL, BaseURL, atlassianGatewayURL, Gateway),
// the company's declaration (Declared) and the rule (DeploymentOf, Of and
// what Of calls). Which declaration each of the engine's many constructions
// of a client is handed is not read here: Crewlet's
// TestEveryAtlassianClientIsGivenTheCompanysDeclaration requires every one to
// name it, and its validation, which
// TestContractAtlassianDeploymentIsTheEngines holds, reads an address by the
// same rule with the same declaration. Nor is startConfluence, which reads
// BaseURL over a copy of the block it resolves itself rather than through
// confluenceBaseURL, since everything else it does would make every change to
// its wiring a read.
//
// The last two, externalLoopback and the ExternalBase it reads, are the one
// decision here the engine shows without a run: `crewlet validate` refuses
// totp optional off loopback, and TestContractSignInPosture holds the render's
// verdict to the engine's at every address it tries. The operator makes that
// verdict on whatever address a Company or its class's route gives the
// engine, before the engine sees it, and holds the data pod back on it, so a
// change to the judgement that moves an address no case tries would have the
// operator hold back a company the engine runs, or start one that refuses to
// boot, with every contract test green. The rule that asks it
// (validatePasswords) is held by the contract test alone, whose cases take
// each of its branches: Optional with and without the acknowledgement, on
// loopback and off it.
var mirrored = []mirror{
	{
		file: sandboxGo, function: "(*launcher).Launch",
		against: []string{codeWorkGo},
		decides: "a run_sandbox launch runs the head of the seat's sandbox phase with role.sandbox.coding_agent or " +
			"the catalogue's default, and refuses before a box exists what newCodingRun and sandboxCredentials refuse",
		digest: "b9fbe0dbb82fa41563957f23e33a5aa990db356b41b8bbd7505950438d52cb00",
		read:   identityRead,
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
		digest: "d6aff5239a57ba7f269dcc08e693b71f762f54c3eee83a92d06c4cedf906aeea",
		read:   identityRead,
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
		digest: "833a43cf285e977637f99849e7b2d4402d4ac7f6e7c3ac51c00d73a28e63e5e6",
		read:   identityRead,
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
		file: jiraClient, function: "DeploymentOf",
		against: atlassianAccount,
		decides: "Jira is on Atlassian Cloud exactly where atlassianhost.Of answers Cloud for the address the engine " +
			"reads it at and the deployment the company declared, and on Data Center otherwise",
		digest: "9858f45393fbdab8d5148123b227c00ce57138335c7d96c680559c6835a4b9f8",
		read:   cloudRuleRead,
	},
	{
		file: confluenceGo, function: "DeploymentOf",
		against: atlassianAccount,
		decides: "Confluence is on Atlassian Cloud by the same rule as Jira, atlassianhost.Of, and on Data Center " +
			"otherwise",
		digest: "9858f45393fbdab8d5148123b227c00ce57138335c7d96c680559c6835a4b9f8",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "Of",
		against: atlassianAccount,
		decides: "an address whose written host is Atlassian Cloud's own (isCloudHost) is Cloud whatever was " +
			"declared, any other is the deployment the company declared, and one that declared none is Data Center",
		digest: "dcdecbbc5c58e3d21efa7bbb922c25e98648791aa4247fdd858776eeec891943",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "isCloudHost",
		against: atlassianAccount,
		decides: "a host is Atlassian Cloud's own when it is a site of one of its environments or one of their " +
			"gateways",
		digest: "91bd18d44e65e07521f8ffca43e6dac018e83c40115a75b693036339544a7fd3",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "isGatewayHost",
		against: atlassianAccount,
		decides: "a gateway is the host of one of the environments' gateways exactly, never a host under it",
		digest:  "e8f3b84b85bfd1a5657b4919e1df1b9620660b0b9046ec8f4f203076e82171ac",
		read:    cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "environmentOfSite",
		against: atlassianAccount,
		decides: "a site is a host under one of an environment's site domains, the environments table " +
			"TestAtlassianDeploymentFollowsTheEngineSource holds",
		digest: "c5436ff0b1545a290a0fb137f50a67b7a279c57b920e670cda64594f991b4882",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "hostOf",
		against: atlassianAccount,
		decides: "an address holding no ${NAME}, as every url the API admits, has its host written, and is judged " +
			"by that host",
		digest: "da86eccfe7137e20f4234dfd73d909964f75f592a8a287c9bccf878eb4be9639",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "host",
		against: atlassianAccount,
		decides: "a host compares without case and without an absolute name's trailing dot, and an address that " +
			"does not parse has none",
		digest: "6d24b643eb1f8e30fd37f6d5ccb9d2b7e08fcdc02905a38167b965efde3c8b76",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, function: "Gateway",
		against: cloudIDGateway,
		decides: "a cloud id is read through the gateway of its site's environment, Government Cloud's for a site " +
			"under atlassian-us-gov-mod.net and the commercial Cloud's for any other, so always through one of " +
			"the environments' gateways, and a product named by a cloud_id, given or discovered, is on Cloud",
		digest: "bab4934fed37904531e5b3b8da88f62845d677bd2479d4ab11fb73bcfa507daf",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, function: "atlassianGatewayURL",
		against: cloudIDGateway,
		decides: "a cloud_id's address is on the host of the gateway Gateway names for its site",
		digest:  "45027e6bb99e10f82f436c16f02b6bbca826dae57f16214143c191ec997b5210",
		read:    cloudRuleRead,
	},
	{
		file: integrationsGo, function: "(*Jira).BaseURL",
		against: cloudIDGateway,
		decides: "Jira named by a cloud_id is read at its gateway address (atlassianGatewayURL), and by a url at " +
			"the url",
		digest: "3519295ac5e07dd3f0acf201cd9fab0bcef0aeb3769aa39e2de41f6538121b66",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, function: "(*Confluence).BaseURL",
		against: cloudIDGateway,
		decides: "Confluence named by a cloud_id is read at its gateway address (atlassianGatewayURL), and by a " +
			"url at the url",
		digest: "9fe72f7427e9d3f6a889b230bc8842287ae0e02670a417f1d2dcbc3384c720a2",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, function: "(*Jira).ResolvedBaseURL",
		against: atlassianAccount,
		decides: "Jira is read at BaseURL of its url, cloud_id and site_url, each with its ${NAME} references " +
			"resolved and its spaces trimmed, which leaves the url the operator renders, holding none, as written",
		digest: "3fe27899fa6491dbbaffb5cab77690c6fa62b8652f5243a94028255449c89db4",
		read:   addressRead,
	},
	{
		file: integrationsGo, function: "(*Confluence).ResolvedBaseURL",
		against: atlassianAccount,
		decides: "Confluence is read at BaseURL of its url, cloud_id and site_url, each with its ${NAME} " +
			"references resolved and its spaces trimmed, which leaves the url the operator renders, holding none, " +
			"as written",
		digest: "cffc562ba4852ccb7345e3f8acba365220049656bd6347a2146c84121555c544",
		read:   addressRead,
	},
	{
		file: "internal/engine/jira.go", function: "jiraBaseURL",
		against: atlassianAccount,
		decides: "the engine derives Jira's deployment and builds its clients (startJira, rewireJira and the " +
			"setup pass) at ResolvedBaseURL of the company's jira block",
		digest: "f1c2e48505b4224df1b3ee4c3229e88491533e85bec1630c246938734cff4454",
		read:   addressRead,
	},
	{
		file: "internal/engine/integrations.go", function: "confluenceBaseURL",
		against: atlassianAccount,
		decides: "the setup pass, which registers Confluence's webhooks as its deployment has them, builds its " +
			"client at ResolvedBaseURL of the company's confluence block",
		digest: "699e7b7fcfebc0fe31da4309ff0151693c125c7802a70422e394301b7488b83f",
		read:   addressRead,
	},
	{
		file: integrationsGo, function: "(*Atlassian).Declared",
		against: atlassianAccount,
		decides: "a company whose integrations.atlassian names no deployment, as every document the operator " +
			"renders, declares none (atlassianhost.Unknown), never the form's default of cloud",
		digest: "c17c2425fd965b5664acc2bb1cf785dadededc0b80011c4b8579784d58b236fa",
		read:   cloudRuleRead,
	},
	{
		file: "internal/jira/payload.go", function: "personID",
		against: atlassianAccount,
		decides: "a Jira webhook names a person by accountId on Cloud and by name, the username, on Data Center, " +
			"which is what a person's atlassian_account_id is matched against",
		digest: "f3e78974748a84706859ffe74740d238a17a72477bda196106b7837d7e2f105f",
		read:   atlassianRead,
	},
	{
		file: "internal/confluence/parse.go", function: "(*Parser).base",
		against: atlassianAccount,
		decides: "a Confluence webhook names its actor by accountId on Cloud and by username, else the user key, on " +
			"Data Center, which is what a person's atlassian_account_id is matched against",
		digest: "f93e753e2226757e3cd1e14f55610c86f3f384efb77980612c69d8458b1e9ab6",
		read:   atlassianRead,
	},
	{
		file: profileFile, function: "IsCredentialName",
		against: credentialNames,
		decides: "a name says it holds a credential when one of its parts, cut at every character that is not a " +
			"letter or a digit and read without case, is one of credentialWords (which " +
			"TestCredentialNameFollowsTheEngineSource holds)",
		digest: "c1e074261881108c0b2fc7785714d31c2547d6498d870d22afeb892b59a7321d",
		read:   credentialNameRead,
	},
	{
		file: mcpbridgeGo, function: "Build",
		against: []string{executorGo},
		decides: "a node given no CREWLET_MCP_BRIDGE_URL builds no tool bridge (the test is Configured's)",
		digest:  "108e637e5417c3bd768bbb6785ede3313232fb828e519f32c1a833125c37e58b",
		read:    bootBindRead,
	},
	{
		file: mcpbridgeGo, function: "Configured",
		against: []string{executorGo, bootstrapGo},
		decides: "a node has a tool bridge only when CREWLET_MCP_BRIDGE_URL is set to more than spaces, which the " +
			"operator gives the data pods alone",
		digest: "8cb9750e7647eb66a8474b39608e911a2975c99bbc36c1362e88ee30431b6950",
		read:   bootBindRead,
	},
	{
		file: bootGo, function: "PublicListener",
		against: []string{bootstrapGo},
		decides: "a node without the ingress role binds api.public only where it serves its tool bridge, so a seat " +
			"pod, given no bridge address, is given no api.public block",
		digest: "a83eb8f588e04b36bb5f222173246f63bb564bb521f5a0d9eddf34ba6a1b4b11",
		read:   bootBindRead,
	},
	{
		file: bootGo, function: "servesBridge",
		against: []string{bootstrapGo, executorGo},
		decides: "a node without the ingress role serves a tool bridge only when it runs seats and has a bridge address",
		digest:  "53129c12fa32a9938afa3d793112e1c700d3ae679f3683f9957cb522b5effa8c",
		read:    bootBindRead,
	},
	{
		file: apiConfigGo, function: "API.externalLoopback",
		against: signInPosture,
		decides: "a node is reached on loopback when the host of its api.external_url is localhost, case aside, or a " +
			"loopback IP, where totp optional needs no accept_insecure, and on no other address, where the node " +
			"refuses to start without it",
		digest: "fa3825b5e04b0790d1336150a10d421dfbfbbda3a21c962868a8c0795ff6134f",
		read:   signInRead,
	},
	{
		file: apiConfigGo, function: "API.ExternalBase",
		against: signInPosture,
		decides: "the address externalLoopback judges is api.external_url with its surrounding spaces and trailing " +
			"'/' removed",
		digest: "783e12fe587aeebcb7817c63074c01025e278b1885608e55a927740aed6f5d6b",
		read:   signInRead,
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
