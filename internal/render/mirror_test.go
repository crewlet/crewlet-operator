package render

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// mirror is a declaration of the engine whose decision the operator copies
// where no engine test can show the engine making it: the verdict on a seat's
// executor (ExecutorOf), the member a launch runs on, the cell it places the
// run in, whether the box can reach the seat's tools from there and whether
// its login follows it, and what of its provider and of the pod a box is
// given, which Crewlet reaches only when a seat launches a run, never on a
// write or in an answer of its API; and which Atlassian deployment the company's Jira and
// Confluence are on, and the identity each names a person by there, which
// it reaches only when it connects to them and when a webhook arrives, and
// against which the render reports a person's Atlassian account
// (atlassianAccounts); and how a name says it holds a credential, which the
// tool-server API's refusal of a literal value copies and the engine never
// applies to a tool server's values, taking any literal there; and whether
// the address a data pod's engine is told is on loopback, and whether it is
// one the engine refuses to start on at all, which the render holds a data
// pod back on (and the API refuses where a Company or its class states it)
// and the engine shows only for the addresses a test tries; and which engine
// pod's boxes dial its tool bridge on loopback and which dial the address the
// controller gives every engine pod, which Crewlet decides from the listeners
// a node binds when it starts, and where every executor run, whatever its
// cell, exports its traces, which it decides at each launch. A contract test
// cannot hold those to the engine, so the operator
// holds the code it read: the digest of each declaration as it was when it
// was read against the operator files that copy it.
type mirror struct {
	// file and name are the declaration's file in the crewlet module and
	// its name, as enginetest.Fingerprint takes it: a function's, a
	// method's or a constant's.
	file, name string
	// against is every operator file that copies the decision, each of
	// which a change to the declaration sends its reader to.
	against []string
	// decides is what the operator copies from it, for the reader the
	// test sends to it.
	decides string
	// digest is the declaration's enginetest.Fingerprint as it was read, at
	// the crewlet version read.
	digest, read string
}

// The operator files that copy a decision of the engine's, this table's own
// file, the engine files most of it is read from, and the crewlet version
// every mirror below was first read at.
const (
	signinGo    = "internal/render/signin.go"
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

	// optionalKeysRead is crewlet#293's engine, which every mirror it changed
	// was read again at, as crewlet#291's had been before it. A value that
	// resolves to nothing is left out where the layer that wins lists its key
	// in optional_keys, and holds a tool server back otherwise; a value a
	// provisioning pass mints for the seat is waited for whatever the
	// declaration says (refGap.hold). The render follows it: the
	// NoProvisioner report says which gestures go without the value.
	optionalKeysRead = "v0.1.1-0.20261021054645-37bd6f2e0c50"
	// deepWorkRead is the engine that runs the Dashboard Assistant's deep work
	// in a box of its own, read on top of crewlet's
	// feat/dashboard-assistant-final at this version, before that change
	// merged: whether a run's environment signs its CLI in is entrySignsIn,
	// which a seat's run and deep work are both judged by.
	deepWorkRead = "v0.1.1-0.20261026010616-d93875db794b"
	// toolCredentialsGo is where the engine decides what a value that
	// resolves to nothing does to a tool server or an executor run.
	toolCredentialsGo = "internal/engine/toolcredentials.go"
	// renderCompanyGo is the render's NoProvisioner report.
	renderCompanyGo = "internal/render/company.go"
	// renderOptionalGo is the render's start values and its check of an
	// MCPServer's optionalKeys against the company.
	renderOptionalGo = "internal/render/optional.go"
	// configOptionalGo is the config reconciler's report of a start value
	// whose Secret holds nothing: left out under an optional key, and
	// otherwise an empty value that keeps a server from starting and that a
	// run starts without.
	configOptionalGo = "internal/controller/config_optional.go"
	// seatTypesGo is the Seat API.
	seatTypesGo = "api/v1alpha1/seat_types.go"
	// mcpServerTypesGo is the MCPServer API.
	mcpServerTypesGo = "api/v1alpha1/mcpserver_types.go"
	// toolServersMd and cliAgentsMd are the guides that state what a tool
	// server and an executor run are started with.
	toolServersMd = "docs/guides/tool-servers.md"
	cliAgentsMd   = "docs/guides/cli-agents.md"
	// runtimeMd is the page that states what an engine pod is made of and
	// where its boxes live.
	runtimeMd = "docs/concepts/runtime.md"
	// engineMCPGo, mcpConfigGo, orgGo and fileRulesGo are where the engine
	// decides which instances of a tool server it starts and with which
	// values, and which spellings of a header it refuses.
	engineMCPGo = "internal/engine/mcp.go"
	mcpConfigGo = "internal/config/mcp.go"
	orgGo       = "internal/org/organization.go"
	fileRulesGo = "internal/config/filerules.go"

	// bootstrapGo is the render of both Tier A documents, mcpbridgeGo where the
	// engine decides whether a node has a tool bridge, and bootGo which node
	// binds a public listener.
	bootstrapGo = "internal/render/bootstrap.go"
	mcpbridgeGo = "internal/api/mcpbridge/build.go"
	bootGo      = "internal/api/boot.go"

	// signInRead is the engine the loopback judgement was first read at.
	signInRead  = "v0.1.1-0.20261018171039-c28489c9c9ca"
	apiConfigGo = "internal/config/api.go"

	// addressShapeRead is the engine the judgement of an address's shape was
	// first read at.
	addressShapeRead = "v0.1.1-0.20261019003935-7cf6955f22ea"
	addressGo        = "internal/render/address.go"
	// engineClassGo is the EngineClass API, which several decisions copied
	// from the engine are written into.
	engineClassGo = "api/v1alpha1/engineclass_types.go"
	normalizeGo   = "internal/config/normalize.go"

	// e2bGo is the engine's E2B backend, and e2bCreateRead the engine its
	// Create was first read at.
	e2bGo         = "internal/sandbox/e2b.go"
	e2bCreateRead = "v0.1.1-0.20261019061838-6c1b4361a915"

	// fleetBridgeRead is the engine whose tool bridge address is the fleet's:
	// the node a box's call reaches relays it over the broker to the node
	// holding the run's seat, so a seat pod is given the data pods' address,
	// and a node without the ingress role serves no route of the bridge's. A
	// deep-work session, which no seat holds, is relayed to the pod that
	// opened it, so it needs nothing more here.
	fleetBridgeRead = "v0.1.1-0.20261026090300-7a96723be5ad"
	bridgeFleetGo   = "internal/engine/bridgefleet.go"
	seatPoolsGo     = "internal/controller/company_pools.go"

	// executorRead is crewlet#388's engine, read at the head of its branch
	// before that change merged, where every agent seat's executor is its
	// coding CLI's own run in a box. A launch picks the first member of the
	// seat's executor chain with an in-box runner that is not sitting out its
	// plan, and places the run by that member's run_in, else the catalogue's
	// default_run_in: the one run is the seat's turn, and every second agent
	// a seat once ran its code in is gone. A box on its node's own host, the direct
	// cell, dials the bridge on that node's own listener, on loopback, where
	// the node serves the route: one with the ingress and seats roles that
	// binds a listener, the public one where it binds one, which is a data
	// pod. Every other box, a seat pod's direct ones included, dials
	// CREWLET_MCP_BRIDGE_URL, and a node with neither builds no bridge and
	// refuses those launches. So the operator gives every engine pod the
	// Company's public address, whatever cells the company's runs are placed
	// in. A direct box lives under providers.sandbox.local.state_dir, is
	// seeded with its member's login, and is given of the pod's environment
	// the allowlisted part alone; a run starts without a value of its
	// environment that resolves to nothing.
	executorRead      = "v0.1.1-0.20261026201916-b7ebd490a7d1"
	localGo           = "internal/sandbox/local.go"
	assistantGo       = "internal/config/assistant.go"
	llmProviderTypes  = "api/v1alpha1/llmprovider_types.go"
	listenersGo       = "cmd/crewlet/listeners.go"
	bridgeGo          = "internal/api/mcpbridge/mcpbridge.go"
	companyWorkloadGo = "internal/controller/company_workload.go"

	// bootRevisionRead is crewlet#391's engine, read at the head of its branch
	// before that change merged. A node starts on the revision its own store
	// names, which the engine moves only once an apply has succeeded, and a
	// node whose store is scratch starts on nothing: decideFile reads no
	// store for it, and OpenNode deletes the file before it opens it. A seat
	// pod's Tier A sets that store (SeatPodBootstrap), so the secret sync
	// keeps nothing for a seat pod the fleet does not list whose engine is not
	// running (judgeUnlisted). No engine test can show it: a node starts on a
	// scratch store only without the data role, and such a node needs a data
	// node's broker to start, where enginetest starts one data node alone.
	bootRevisionRead = "v0.1.1-0.20261026181807-528aee549682"
	seedGo           = "cmd/crewlet/seed.go"
	storeGo          = "internal/store/store.go"
	configServingGo  = "internal/controller/config_serving.go"

	// bootBoundsRead is the engine every bound on a data pod's and a seat
	// pod's boot the controller adds up was read at.
	bootBoundsRead = "v0.1.1-0.20261019181514-fb36f0c31127"
	// assistantLogRead is the engine whose data node runs an eighth state
	// log, the Dashboard Assistant's, read at the head of crewlet's
	// feat/dashboard-assistant-final, before that change merged. startCore
	// opens its two sides over the running log (openAssistant, which reads
	// nothing over the network) and runs a person's erasure from it on a
	// goroutine of its own, off the boot. The
	// log is one more in the register every data node runs, so each of the
	// boot's steps that reads the logs one at a time reads one more: the
	// join reads the fleet's trim floors once for the generations and once
	// for each log's floor, each listing reading back one row a log, and
	// each log's stats (statelog.go's replayable, trimFloor and
	// fleetGenerations), and the admission reads each log's maintenance
	// window (maintenance_mode.go's admit). The steps that read them
	// together or under one window (the ceilings, the streams, the
	// consumers) do not grow. dataPodBootSeconds follows it: 1646 seconds
	// before the object store, where seven logs took 1536.
	assistantLogRead = "v0.1.1-0.20261026010616-d93875db794b"

	// bootRecoveryRead is crewlet#311's engine, where the join was read
	// again against the windows, and the boot's read of the company's
	// secrets first read. A join now keeps an installed artefact's pin on
	// the trim until the node's own position counts it, renewed in the
	// background as often as startStateLog sets, and comes up on its own
	// rows when a fault of the node's stops it before anything is
	// installed. None of it adds a read or a write to a boot: the pin is the
	// one put it was. The secrets are read by bootSecrets, past the recovery
	// boundary, which no window counts. Setting that renewal is all that
	// changed in startStateLog since it was read for its refusal of a broker
	// that cannot host a state log, which names Worklet and Doclet, the
	// engine's own tracker and pages; it moves no window either.
	bootRecoveryRead = "v0.1.1-0.20261025183322-23ffa9027d77"

	// The engine files most of the bounds on a boot are read from, and the
	// guide that quotes the windows they add up to.
	jsprovisionGo     = "internal/jsprovision/jsprovision.go"
	embeddedGo        = "internal/queue/jetstream/embedded.go"
	queueGo           = "internal/queue/jetstream/queue.go"
	stateLogGo        = "internal/engine/statelog.go"
	backendsGo        = "internal/engine/backends.go"
	walkGo            = "internal/coord/kv/walk.go"
	positionsGo       = "internal/coord/kv/positions.go"
	natsObjGo         = "internal/objstore/natsobj/natsobj.go"
	troubleshootingMd = "docs/guides/troubleshooting.md"

	// artifactOriginRead is the engine the artifact origin was first read at.
	artifactOriginRead = "v0.1.1-0.20261020171236-91f9788515e6"
	artifactsConfigGo  = "internal/config/artifacts.go"
	exposureMd         = "docs/guides/exposure.md"
	companyTypesGo     = "api/v1alpha1/company_types.go"
	companyExposureGo  = "internal/controller/company_exposure.go"

	// brandKindRead is the engine that adds the brand kind
	// (crewlet/crewlet#328), which the dashboard renders natively, so the
	// kinds framed from the artifact origin are still HTML pages and designs
	// alone.
	brandKindRead = "v0.1.1-0.20261025061312-a6fd4b115be7"
)

// adr0008Md is the record of where a seat's executor runs, and
// credentialsConceptMd the page that says what reaches an engine pod and a
// box of the platform's and the tenant's credentials: each copies several of
// the engine's decisions below.
const (
	adr0008Md            = "adr/0008-run-a-seats-executor-in-its-own-engine-pod-or-e2b.md"
	credentialsConceptMd = "docs/concepts/credentials.md" //nolint:gosec // G101: a page's path, which holds no credential.
)

// artifactOriginCopies is every operator file that copies which artifact
// origins Crewlet refuses to start on.
var artifactOriginCopies = []string{addressGo, companyTypesGo, engineClassGo, companyExposureGo, exposureMd}

// framedKinds is every operator file that states which artifacts are framed.
var framedKinds = []string{companyTypesGo, engineClassGo, companyExposureGo, exposureMd}

// addressCopies is every operator file that copies which addresses Crewlet
// refuses to start on: the render, which holds a data pod back on one the
// operator derives, the API's rules on the addresses a Company states
// (dashboardBaseURL, publicBaseURL) and on the API route's host name template
// of a class, the message the controller holds the pods back with, which says
// which namespaces and names make a number of a template's last label, and
// the guides that tell people what to write and what that message means.
var addressCopies = []string{addressGo, companyTypesGo, engineClassGo, companyExposureGo, exposureMd, troubleshootingMd}

// signInPosture is every operator file that copies where Crewlet lets a
// second factor be optional without the acknowledgement: the render, which
// holds a data pod back on it, the EngineClass API's spec.signIn, which says
// so, and the guide that tells a platform when to acknowledge it.
var signInPosture = []string{bootstrapGo, engineClassGo, "docs/guides/people.md"}

// atlassianAccount is every operator file that copies which deployment the
// company's Jira and Confluence are on, and what names a person there: the
// render's report on a Seat's atlassianAccount, and the guide that tells a
// person which value to give.
var atlassianAccount = []string{contactGo, seatTypesGo, integrationsMd}

// cloudIDGateway is every operator file that copies which gateway a cloud_id
// is read through: those of atlassianAccount, to which a product named by
// one is on Cloud, and the Integration API, which says whose gateway it is.
var cloudIDGateway = append([]string{"api/v1alpha1/integration_types.go"}, atlassianAccount...)

// credentialNames is every operator file that copies how the engine reads a
// credential's name: the API's rules that read one (the LLMProvider and
// platform provider APIs' over a CLI's sign-in, and the tool-server API's
// refusal of a literal value), the render's copy of their pattern, and the
// guide and the page that state the rule of a literal value.
var credentialNames = []string{
	"api/v1alpha1/llmprovider_types.go", engineClassGo, mcpServerTypesGo, "api/v1alpha1/unit_types.go",
	signinGo, "docs/guides/tool-servers.md", credentialsConceptMd,
}

// executorLogin is every operator file that copies how an executor run's CLI
// signs in where it runs, and when Crewlet refuses it for signing in to
// nothing: the render's executor verdict and its sign-in rules, the
// LLMProvider API's docs and the guide that state it, and the CRD suite's
// reading of the engine's sign-in verdicts (its holdVerdicts).
var executorLogin = []string{
	signinGo, executorGo, "api/v1alpha1/llmprovider_types.go", "docs/guides/cli-agents.md",
	"test/crd/cliagent_engine_test.go",
}

// executorBox is every operator page that states what an executor's box
// carries of the provider its run is on, its token, key or credential-named
// variables of its env and, off the engine pod, never its credential bundle:
// the credentials guide's "What a coding box carries" and the credentials
// concept page.
var executorBox = []string{"docs/guides/credentials.md", credentialsConceptMd}

// bridgeVerdict is every operator file that copies which engine pod's direct
// boxes reach the tool bridge on loopback and which only at the Company's
// public address: the render, which writes the pods' api.public and reports a
// Direct seat in a seat pool with no public address as BridgeUnreachable
// (ExecutorOf), the API's reason, and the pages and the record that state the
// verdict.
var bridgeVerdict = []string{
	bootstrapGo, executorGo, "api/v1alpha1/common_types.go", cliAgentsMd, troubleshootingMd,
	"docs/guides/topology.md", "docs/concepts/applying.md", runtimeMd, exposureMd,
	adr0008Md,
}

// directBox is every operator file that copies where a Direct box lives and
// what of the pod it is given: the render's local.state_dir and SandboxDir,
// the volume the controller mounts there on every engine pod, and the API
// docs and pages that say what a Direct run gets of the pod's environment.
var directBox = []string{
	renderCompanyGo, bootstrapGo, companyWorkloadGo, seatPoolsGo, companyTypesGo, engineClassGo,
	runtimeMd, cliAgentsMd,
}

// runTelemetry is every operator file that states where a seat's executor run
// exports its traces: to the receiver at the Company's public address, Direct
// as well as in E2B. The controller gives every engine pod that address, the
// API docs of the address, of the route that carries it and of the class's
// telemetry say who calls it, and the pages and the security policy say so.
var runTelemetry = []string{
	companyWorkloadGo, companyTypesGo, engineClassGo, exposureMd, runtimeMd,
	"docs/concepts/overview.md", credentialsConceptMd, "SECURITY.md", "README.md",
	adr0008Md,
}

// bootWindows is every operator file that copies the bounds the engine puts
// on its own boot: the controller's data pod and seat pod startup windows,
// which add them up, the test that pins their sums, the EngineClass API's
// floor on data.startupSeconds, and the pages that quote them.
var bootWindows = []string{
	"internal/controller/company_workload.go", "internal/controller/company_pools.go",
	"internal/controller/company_workload_test.go", engineClassGo,
	runtimeMd, troubleshootingMd,
}

// mirrored is every engine declaration the operator's run-time verdicts copy
// a decision of, and no other: a declaration changed here sends somebody to
// read it, so one that copies nothing would only cost a read. What the engine
// shows without a run is held by a contract test instead, never here: the
// head of a seat's chain (TestContractChainlessSeatRunsOnTheFirstProviderAlone),
// what it refuses on a write of a seat's executor, its chain, its catalogue
// and its cells (TestContractEngineRefusesWhatItHolds), what it applies and
// fails at a launch alone (TestContractExecutorIsAppliedWhereItCannotRun),
// the sign-in a provider's settings give it and the CLIs it admits a run_in
// on (test/crd's TestCLIAgentSignInFollowsTheEngine and
// TestCLIAgentRunInFollowsTheEngine), the deployment its validation reads a
// Jira or Confluence address on (TestContractAtlassianDeploymentIsTheEngines),
// and the lists TestExecutorSignInFollowsTheEngineSource,
// TestRunnerCLIsFollowTheEngineSource,
// TestAtlassianDeploymentFollowsTheEngineSource and
// TestCredentialNameFollowsTheEngineSource read.
//
// Two decide what a value that resolves to nothing costs a seat:
// runEnvironment, which lays a run's environment and leaves out a key the
// layer that wins declares optional, and refGap.hold, which waits for a value
// a provisioning pass mints for the seat whatever the declaration says. The
// executor verdict and the render's NoProvisioner report copy them; what a
// tool server is started with is shown by a running engine instead
// (TestContractAnOptionalKeyIsLeftOut). Which values each instance of a tool
// server is started with, which the render's StartValue.Started copies and the
// config reconciler reports an empty value by, is decided by five more:
// startSharedServers, seatSpecs and Grants (which instances run, the company's
// and the seats'), inheritMCPEnv (a unit's block under its members') and
// sandboxMCP (an executor's box). And validateHeaderSpellings, with
// spelledUnlike, refuses a header a seat spells unlike its unit, which the
// render refuses too (headerSpellings).
//
// Most of them are the launch's own code: the member it runs on (member,
// executorRunners), the cell it places the run in (agentPlacement,
// buildSandbox and its round cap, defaultSandboxMaxTurns), the address the
// box dials (LaunchExecutor, with the bridge's own Build, Reaches and the
// node's bridgeHostURL and loopbackURL), and the login it hands the box
// (newCodingRun, entrySignIn, sandboxCredentials and OnEngineHost). Some are
// a sandbox backend's: what an E2B box is created from, (*E2BProvider).Create,
// since the launch hands a run's credential files to whatever box it is placed
// in and each backend decides what to do with them; where a direct box lives
// (NewLocal, DefaultBoxRoot, localOptions) and what of the pod's environment it
// is given (childEnv, PassthroughEnv). And two are deep work's, which no seat
// launches: where it runs (DeepWorkPlacement) and the address it dials
// ((*assistantDeep).target).
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
// externalLoopback and the ExternalBase it reads are a decision the engine
// shows without a run: `crewlet validate` refuses totp optional off loopback,
// and TestContractSignInPosture holds the render's verdict to the engine's at
// every address it tries. The operator makes that
// verdict on whatever address a Company or its class's route gives the
// engine, before the engine sees it, and holds the data pod back on it, so a
// change to the judgement that moves an address no case tries would have the
// operator hold back a company the engine runs, or start one that refuses to
// boot, with every contract test green. The rule that asks it
// (validatePasswords) is held by the contract test alone, whose cases take
// each of its branches: Optional with and without the acknowledgement, on
// loopback and off it.
//
// The address shapes after them are the same kind of decision: `crewlet
// validate` refuses each, and TestContractAddressShapes holds the render's
// verdict to the engine's, and test/crd's TestAddressShapesFollowTheEngine
// the API's, at every address they try. The operator makes the verdict
// before the engine sees the address, at admission for one a Company or its
// class states and in the render for one it derives (the API route's host,
// from the Company's namespace and name), so a change that refuses an
// address no case tries would crash-loop a data pod the API admitted, with
// every test green.
// Which addresses each is asked of is the wiring that calls them
// (API.validate, APIAuth.validate), which the contract tests hold, since
// their every shape reaches the engine through it.
//
// The boot's bounds after them are what the controller's startup windows
// add up (dataPodBootSeconds, seatPodBootSeconds and the object stores'
// steps): every constant a wait is bounded by, and every function that
// arranges such waits on the boot of a data pod or a seat pod. The engine
// shows none of them without a boot that stalls at each, so a bound that grew,
// or a step that waits twice where it waited once, would have the kubelet
// restart a boot the engine would finish, with every test green. Which steps
// a boot runs, and in what order, is engine.New's, which is not read here: it
// changes with most of the engine's work, and a read of it at each change
// would be a read of everything else it does. One of them is a step the
// windows leave out, past the recovery boundary: (*Engine).bootSecrets, the
// boot's read of the company's secrets, which waits out a store that does not
// answer for as long as the boot lives, as the pages that say what a boot
// does after /health answers 200 state.
var mirrored = []mirror{
	{
		file: bootGo, name: "ArtifactsListener",
		against: []string{bootstrapGo},
		decides: "only an ingress node with an api.port binds api.artifacts, so a seat pod is given none",
		digest:  "383ced277b7ee737b539b49fd1ae9c0336235275cbda2d7733f49140f13719ea",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "(*APIArtifacts).validate",
		against: artifactOriginCopies,
		decides: "which api.artifacts.external_url Crewlet refuses to start on",
		digest:  "42d77db202b845f630e41b3e688675ff1f9c7a091ecfd89fafe8daf66daacb94",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "checkArtifactTemplate",
		against: artifactOriginCopies,
		decides: "where {artifact} may stand in an artifact origin",
		digest:  "a915329e877dd0f1bceef5997a1e7be769ddaad24de0b4f1c43d4d7090558015",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "(*APIArtifacts).checkCollisions",
		against: artifactOriginCopies,
		decides: "which artifact origins meet the dashboard's or the public listener's",
		digest:  "faca0faed45645d25dafb3b9072f0e9cf7f51ec04cbe7a890b0d2226549e4d01",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "artifactHostShape",
		against: artifactOriginCopies,
		decides: "which hosts a template gives an artifact",
		digest:  "c309e00278dfcd12fbb92d3774db0c96cffcb9da5634a2d0b9d278d72e5e5803",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "loopbackHost",
		against: artifactOriginCopies,
		decides: "where an artifact origin may be plain http",
		digest:  "76966b0c99f8dbf36aba2e771fbd549851109be987a7884430adf043f2ac89bb",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "APIArtifacts.ExternalBase",
		against: artifactOriginCopies,
		decides: "an artifact origin is judged trimmed of space and a trailing '/'",
		digest:  "9158e653553093b67e644a4b58e8621ed0ad6ab7b289b7b60cb9a75ec894a367",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "APIArtifacts.rendered",
		against: artifactOriginCopies,
		decides: "a template is judged with {artifact} filled by a label",
		digest:  "918a47aa3972d93aae0f1c976c84c31a927904dc9d40dac42bde10d2233d977e",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "APIArtifacts.apex",
		against: artifactOriginCopies,
		decides: "the host every artifact host shares",
		digest:  "a1d1cc61930dfe853e620efe7846f81da270d6daa9ea355cf79b0546640ccae5",
		read:    artifactOriginRead,
	},
	{
		file: artifactsConfigGo, name: "ArtifactPlaceholder",
		against: artifactOriginCopies,
		decides: "the placeholder an artifact origin names a label with",
		digest:  "207300ebf3e0ac1da5df8ad140c03178e321a2bafa82938fd8aa680efd931fac",
		read:    artifactOriginRead,
	},
	{
		file: "internal/artifacts/types.go", name: "LabelLen",
		against: artifactOriginCopies,
		decides: "an artifact's host label is 26 characters",
		digest:  "fc2d5f83b48b92a4a9864d4da1b087a5cd1cdb58fbfc609f83c9c480207bd8e0",
		read:    artifactOriginRead,
	},
	{
		file: "internal/artifacts/kind.go", name: "kindSpecs",
		against: framedKinds,
		decides: "HTML pages and designs are the kinds framed from the artifact origin",
		digest:  "3ce61727d1fefd666a7f64890b74d4df9ba8fe6ae404ab9b6fa919e68764ef24",
		read:    brandKindRead,
	},
	{
		file: sandboxGo, name: "buildSandbox",
		against: []string{renderCompanyGo, companyTypesGo, seatTypesGo},
		decides: "the catalogue every launch reads is built from providers.sandbox, over the cells the company reaches " +
			"alone, with its default_run_in as the cell of a run whose entry names none, its default_max_turns, else " +
			"96, as every run's round cap, and its setup steps as the setup every box is given; a company with no " +
			"sandbox has none, and runs no executor",
		digest: "c50896e3b0dff8bdcbcf4462422ce3b87e586c1a206f66192c989d7b3f32280d",
		read:   executorRead,
	},
	{
		file: "internal/config/sandbox.go", name: "defaultSandboxMaxTurns",
		against: []string{companyTypesGo, seatTypesGo, cliAgentsMd},
		decides: "an executor run on a seat and a catalogue that name no round cap takes 96 rounds at most, the only " +
			"bound on a seat's turn",
		digest: "bb2dfa06eb8b449af4c6df4ea5183fcd4b7a237f9e8fd8620a4e0d55a16ba5f7",
		read:   executorRead,
	},
	{
		file: sandboxGo, name: "newCodingRun",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a run on the member a launch picked is handed that member's model as its runs in the engine's pod " +
			"pass it, its sign-in (entrySignIn) beneath the seat's environment, and its login, the CLI's credential " +
			"files or the document OpenCode 2's export printed, as the run's credential files, for the box to seed " +
			"or to ignore ((*E2BProvider).Create)",
		digest: "1527131dde11c15181a40fa3ce3061b492b31a11054d197e6c29ece72774a2a5",
		read:   executorRead,
	},
	{
		file: e2bGo, name: "(*E2BProvider).Create",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a box in the Company's E2B sandbox is created from its template, the run's timeout and the run's " +
			"environment alone, never from the run's credential files, so the member's login (its credential " +
			"files, or the document OpenCode 2's export printed) never reaches it, and only a variable of the " +
			"run's environment signs its CLI in there",
		digest: "65305f3ecc87afac5544b7bddb91062389e5a0e0b76a63481f6f191e778e2a03",
		read:   e2bCreateRead,
	},
	{
		file: sandboxGo, name: "entrySignIn",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a ClaudeCode run whose seat's environment signs ClaudeCode in (codingagent.ClaudeCodeCredentialEnv, " +
			"which TestExecutorSignInFollowsTheEngineSource holds) is handed none of its member's sign-in, and every " +
			"other run its member's own (SandboxEnv), beneath the seat's environment",
		digest: "0057bb67a4aa5b34269adc0b3f86eccf82bbb4ddc0edd4cccec029a16078de71",
		read:   executorRead,
	},
	{
		file: sandboxGo, name: "signsIn",
		against: []string{signinGo},
		decides: "a variable of the seat's environment signs ClaudeCode in when it holds a value",
		digest:  "d67892b7c02c1053a5a38c588fe26d93a39b9525b01879a0d6dc3ef923280798",
		read:    firstRead,
	},
	{
		file: sandboxGo, name: "sandboxCredentials",
		against: append([]string{"api/v1alpha1/common_types.go", troubleshootingMd}, executorLogin...),
		decides: "the login of the member a launch picked is refused only in a box off the engine host (OnEngineHost: " +
			"a Direct box is seeded with it), and there unless the run's environment signs its CLI in " +
			"(entrySignsIn): SandboxLoginNotPortable, a refusal that fails the launch rather than moving it down " +
			"the chain",
		digest: "9cfb8b3288a8fa60fa231e82b51c9ab89b980974c955967d5c16ce1db7d53e44",
		read:   executorRead,
	},
	{
		file: sandboxGo, name: "entrySignsIn",
		against: append(append([]string{cliAgentsMd}, executorLogin...), executorBox...),
		decides: "a run's environment signs its CLI in, in a box off the engine host, when it holds the CLI's " +
			"token or key variable, for ClaudeCode any variable that signs it in, and for an env sign-in CLI any " +
			"credential-named one: the rule a seat's E2B run and the assistant's deep work, whose environment " +
			"is its provider's own sign-in alone, are both held to before a box exists",
		digest: "46c439e9448b247366d6d3852f1fdf0cf98cc40f927178a6395653138ecfb018",
		read:   deepWorkRead,
	},
	{
		file: sandboxGo, name: "(*Engine).runEnvironment",
		against: append([]string{
			signinGo, renderCompanyGo, renderOptionalGo, configOptionalGo, seatTypesGo, companyTypesGo, cliAgentsMd,
		}, executorBox...),
		decides: "a run's environment is the env of the catalogue's and the seat's setup steps, then the seat's " +
			"role.sandbox.env, each later layer winning a name, and a value that resolves to nothing is left out, " +
			"quietly where the optional_keys beside the value that wins lists its key (a setup file's path " +
			"likewise), and as the run's gap otherwise, which LaunchExecutor logs and starts the run without",
		digest: "b60e4dd5e0352f843987be692813b523adfefe41221f497170d245d38e92faf2",
		read:   executorRead,
	},
	{
		file: toolCredentialsGo, name: "resolveServer",
		against: []string{renderOptionalGo, configOptionalGo, mcpServerTypesGo, toolServersMd},
		decides: "a tool server starts with its env (stdio) or headers (http), a seat's mcp_env for it over them key " +
			"by key, merged by the key the server reads (a header in any case) before anything resolves, so a seat's " +
			"key replaces the server's whole; a value that resolves to nothing, or to an empty value, is left out " +
			"where the server's optional_keys lists its key, whichever layer wrote it, and holds the server back " +
			"otherwise; and a url that resolves to nothing holds it back whatever optional_keys says",
		digest: "2883d7f46a0787adfe1194f21693365ab5729bb46466bbdaeeef8522faff7041",
		read:   optionalKeysRead,
	},
	{
		file: toolCredentialsGo, name: "(*refGap).hold",
		against: []string{renderCompanyGo, signinGo, mcpServerTypesGo, seatTypesGo},
		decides: "a value a provisioning pass mints for the seat holds a tool server back, and is logged as awaited " +
			"where it is missing from an executor run, whatever optional_keys says, and a declared key's value " +
			"that nothing mints is left out",
		digest: "0b885181d18a78a921a37fae2c37e6f1e40a508f1a55c59a89cfb9de528dd14c",
		read:   optionalKeysRead,
	},
	{
		file: engineMCPGo, name: "(*Engine).startSharedServers",
		against: []string{renderOptionalGo, configOptionalGo, toolServersMd},
		decides: "a shared server is started once for the company, with its own values alone (no seat's block)",
		digest:  "f88ef4e48075a9adde25d5a5296cd9c82919d2792fff3e65f5d0404a5525f7d5",
		read:    optionalKeysRead,
	},
	{
		file: engineMCPGo, name: "seatSpecs",
		against: []string{renderOptionalGo, configOptionalGo, toolServersMd},
		decides: "a seat's instance is started of every per-seat server it is granted, with the seat's block for it",
		digest:  "f173aa5f383a5c87f0897455a9545d3ef6e70c05d42b4a7a1f7b3b710e37f3a5",
		read:    optionalKeysRead,
	},
	{
		file: mcpConfigGo, name: "(*MCPServer).Grants",
		against: []string{renderOptionalGo, configOptionalGo},
		decides: "a per-seat server is granted to every agent seat with a block of values for it, its own or its " +
			"unit's, and a shared one to every agent seat",
		digest: "68e24eca130819612cd092985ec133b3f1a494fd4a5ee4d5c73801daf2cc5a99",
		read:   optionalKeysRead,
	},
	{
		file: orgGo, name: "(*Organization).inheritMCPEnv",
		against: []string{renderOptionalGo, configOptionalGo, toolServersMd},
		decides: "a unit's block for a server is laid under each of its direct agent members' own, which wins a key " +
			"as written",
		digest: "dacbba6d65ca753f5b914c2804aa7ca95f2a17a405fcce1e829b93957eba440c",
		read:   optionalKeysRead,
	},
	{
		file: sandboxGo, name: "sandboxMCP",
		against: []string{renderOptionalGo, configOptionalGo},
		decides: "an executor's box is given every server its seat's sandbox scopes, shared or not, with the seat's " +
			"block for it",
		digest: "bc5673425d1a4038333777f676a20ecfcd77ffe6535f373f6602be6394c8f99b",
		read:   executorRead,
	},
	{
		file: fileRulesGo, name: "(*Company).validateHeaderSpellings",
		against: []string{renderOptionalGo, toolServersMd},
		decides: "a seat's value for an http server under a header its unit's block writes in another case is " +
			"refused on every write, and a stdio server's keys are compared as written",
		digest: "5ee6ced04f6db8e78a19c3a5b0f983ad4588acfa061181f24ef401436e0e782f",
		read:   optionalKeysRead,
	},
	{
		file: fileRulesGo, name: "spelledUnlike",
		against: []string{renderOptionalGo},
		decides: "a seat's key is refused where its unit's block writes the same header under another spelling",
		digest:  "5727b98d86c5bf29f1799e6bfbbd9fdae77f5c7c78d6ffebfc9aeeb7604dc064",
		read:    optionalKeysRead,
	},
	{
		file: doctorGo, name: "(*Provider).CredentialEnvNames",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "the variables that sign a CLI in inside a box: its token and key variables, and for an env " +
			"sign-in CLI the credential-named variables of its env",
		digest: "091501e8498192be36fc8619232c358a2276c32bae8f412e7197fcc0a8b89667",
		read:   firstRead,
	},
	{
		file: doctorGo, name: "(*Provider).SandboxEnv",
		against: append(append([]string{engineClassGo, "docs/guides/multi-tenancy.md"}, executorLogin...), executorBox...),
		decides: "a provider's own sign-in follows an executor run into its box, Direct or E2B, when it is one of " +
			"those variables, and nothing else of the provider's env does: a gateway's address or a cloud switch " +
			"in env reaches no executor run",
		digest: "f81f2bc4b7fca996eaac300c1ab530d2e357500bd4b76145551412788cc9ddb9",
		read:   firstRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).LaunchExecutor",
		against: append([]string{renderCompanyGo, configOptionalGo}, executorLogin...),
		decides: "a launch, behind the turn's fence on a node with a sandbox runtime and a bridge something reaches, " +
			"picks its member (member), places the run in the member's cell (agentPlacement, else the catalogue's " +
			"default), refuses a cell whose box cannot dial the bridge (Reaches: a Direct box dials its node's own " +
			"listener where the node serves one, every other the fleet's address; BridgeUnreachable, NotExposed), " +
			"starts the run without a value of its environment that resolves to nothing, and refuses the picked " +
			"member's login only off the engine host (sandboxCredentials: SandboxLoginNotPortable)",
		digest: "e513b05ddd1d7323f89dede43d5712f40082184b390e261ea6ff174d8b4cde0d",
		read:   executorRead,
	},
	{
		file: "internal/sandbox/launch.go", name: "withTelemetry",
		against: runTelemetry,
		decides: "every executor launch, whatever its cell, is given the run's OTel environment, an endpoint under " +
			"the one receiver address the node was given (CREWLET_SANDBOX_OTEL_RECEIVER_URL) with a token signed " +
			"for the run, so a Direct box on an engine pod exports its traces to the Company's public address as " +
			"an E2B box does",
		digest: "0d16b5ee99d997bfe0a31ca979204f9519fad0f1cb16776982957893e0353830",
		read:   executorRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).member",
		against: []string{executorGo, headsGo, seatTypesGo, llmProviderTypes, cliAgentsMd},
		decides: "a launch runs on the first member of the seat's executor chain with an in-box runner that is not " +
			"sitting out its spent plan on the node, passing over every other CLI silently, and when every runner " +
			"member is sitting out runs on none",
		digest: "863b716f4c7243731fb8ac34e18664aed54fee0dc77c427277efdf05f1528210",
		read:   executorRead,
	},
	{
		file: agentRunGo, name: "executorRunners",
		against: []string{executorGo, headsGo, seatTypesGo, llmProviderTypes, cliAgentsMd},
		decides: "the members a launch picks among are the executor chain's members, in order, whose CLI has an " +
			"in-box runner (codingagent.Names, held by TestRunnerCLIsFollowTheEngineSource), and a chain with none " +
			"launches nothing",
		digest: "11df5269c5b19f733513c92b8069dc548f0ec7f988b3110e98f35e2cda39f95b",
		read:   executorRead,
	},
	{
		file: agentRunGo, name: "agentPlacement",
		against: []string{executorGo, renderCompanyGo, llmProviderTypes},
		decides: "a run is placed in its member's own run_in, and with none in the catalogue's default_run_in, so a " +
			"platform provider, which names none, runs in the Company's default",
		digest: "dac767f83dbf6edf929344249a1f3e89c013b208d9ee021f4716d39efca85644",
		read:   executorRead,
	},
	{
		file: "internal/sandbox/placement.go", name: "Placement.OnEngineHost",
		against: []string{executorGo, signinGo},
		decides: "a direct box runs on the engine's own host, where its login is seeded, and an e2b box does not",
		digest:  "55a2329f3b2e286f3aa4a0e096b5982cae3443e5c0fa6dc8375a661286009b2d",
		read:    executorRead,
	},
	{
		file: localGo, name: "NewLocal",
		against: directBox,
		decides: "a direct box is made under providers.sandbox.local.state_dir, made absolute, and only with none " +
			"under DefaultBoxRoot, so the state_dir the render writes puts every box on the pod's sandboxes volume",
		digest: "7196f5bbbc3de78eb9f93dd245fa7cace5888fe8699e0e20507e63b9e053e1d2",
		read:   executorRead,
	},
	{
		file: localGo, name: "DefaultBoxRoot",
		against: directBox,
		decides: "with no state_dir a direct box would be made under CREWLET_SANDBOX_LOCAL_HOME, else the engine " +
			"user's ~/.crewlet/sandboxes, which on a seat pod is its scratch volume",
		digest: "b9396f0a8dd34eab0baa36e6332036133d1d850d27f3a91b7cf2567af87ec872",
		read:   executorRead,
	},
	{
		file: sandboxGo, name: "localOptions",
		against: directBox,
		decides: "the local backend is handed the catalogue's local.state_dir as written, for NewLocal to place its " +
			"boxes under",
		digest: "78d67b2c7b8e029f2ee2fa5bb2e80f6fd8b128f9daaf64a1f98151f805dd1642",
		read:   executorRead,
	},
	{
		file: "internal/sandbox/local_box.go", name: "(*directBox).childEnv",
		against: directBox,
		decides: "a direct box's processes are given the engine's environment through hostbox.Inherit alone, the " +
			"box's home, XDG and temporary directories, and the run's environment, so of the pod's environment only " +
			"what PassthroughEnv names reaches a Direct run",
		digest: "2ba9f4a8443ffe0d5fbbc823b3179c8194e7b837e74032f125f799d84aa45afa",
		read:   executorRead,
	},
	{
		file: "internal/hostbox/hostbox.go", name: "PassthroughEnv",
		against: directBox,
		decides: "of the engine's environment a child is given PATH, the locale, TERM, TZ, the TLS trust and the " +
			"proxy variables alone",
		digest: "8d6d9651cbfe5473746bf48ae12b5348f0bf439c84b98c8302b300c1f86ec3a7",
		read:   executorRead,
	},
	{
		file: assistantGo, name: "(*Company).DeepWorkPlacement",
		against: []string{companyTypesGo, executorGo, cliAgentsMd, runtimeMd,
			adr0008Md},
		decides: "deep work runs in its provider's run_in, else in E2B where the company configures it, else in a " +
			"container with an image, else on the engine host under the engine's own confinement, never by " +
			"default_run_in",
		digest: "5c900a1eb6c7e0484c2f60c289bdec274be0aca2d2f7bee1154b12312140b225",
		read:   executorRead,
	},
	{
		file: "internal/engine/assistantdeep.go", name: "(*assistantDeep).boxRoot",
		against: []string{companyTypesGo, bootstrapGo, cliAgentsMd, runtimeMd,
			adr0008Md},
		decides: "a deep-work run placed on the engine host keeps its confined box beside the node's store, under " +
			"the directory of the store path the render writes (/var/lib/crewlet/store/assistant-boxes on the " +
			"data volume), never under providers.sandbox.local.state_dir, so no sandboxSizeLimit bounds it",
		digest: "cd3f5182fffdbe1e0cead0b77f3ee6ece5b470f25d23d6f20e780409e339a16d",
		read:   executorRead,
	},
	{
		file: "internal/engine/assistantdeep.go", name: "(*assistantDeep).target",
		against: []string{companyTypesGo, engineClassGo, cliAgentsMd, exposureMd, runtimeMd, "SECURITY.md",
			adr0008Md},
		decides: "deep work reaches Crewlet at the fleet's bridge address in whichever cell it runs, never at its " +
			"node's own listener, so a Company with no public address runs no deep work, and a run placed on the " +
			"engine host needs the host's confinement",
		digest: "00a568fc5466cf94401190d850322014d35eae81771a6d37eae2adc8ab93931f",
		read:   executorRead,
	},
	{
		file: jiraClient, name: "DeploymentOf",
		against: atlassianAccount,
		decides: "Jira is on Atlassian Cloud exactly where atlassianhost.Of answers Cloud for the address the engine " +
			"reads it at and the deployment the company declared, and on Data Center otherwise",
		digest: "9858f45393fbdab8d5148123b227c00ce57138335c7d96c680559c6835a4b9f8",
		read:   cloudRuleRead,
	},
	{
		file: confluenceGo, name: "DeploymentOf",
		against: atlassianAccount,
		decides: "Confluence is on Atlassian Cloud by the same rule as Jira, atlassianhost.Of, and on Data Center " +
			"otherwise",
		digest: "9858f45393fbdab8d5148123b227c00ce57138335c7d96c680559c6835a4b9f8",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "Of",
		against: atlassianAccount,
		decides: "an address whose written host is Atlassian Cloud's own (isCloudHost) is Cloud whatever was " +
			"declared, any other is the deployment the company declared, and one that declared none is Data Center",
		digest: "dcdecbbc5c58e3d21efa7bbb922c25e98648791aa4247fdd858776eeec891943",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "isCloudHost",
		against: atlassianAccount,
		decides: "a host is Atlassian Cloud's own when it is a site of one of its environments or one of their " +
			"gateways",
		digest: "91bd18d44e65e07521f8ffca43e6dac018e83c40115a75b693036339544a7fd3",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "isGatewayHost",
		against: atlassianAccount,
		decides: "a gateway is the host of one of the environments' gateways exactly, never a host under it",
		digest:  "e8f3b84b85bfd1a5657b4919e1df1b9620660b0b9046ec8f4f203076e82171ac",
		read:    cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "environmentOfSite",
		against: atlassianAccount,
		decides: "a site is a host under one of an environment's site domains, the environments table " +
			"TestAtlassianDeploymentFollowsTheEngineSource holds",
		digest: "c5436ff0b1545a290a0fb137f50a67b7a279c57b920e670cda64594f991b4882",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "hostOf",
		against: atlassianAccount,
		decides: "an address holding no ${NAME}, as every url the API admits, has its host written, and is judged " +
			"by that host",
		digest: "da86eccfe7137e20f4234dfd73d909964f75f592a8a287c9bccf878eb4be9639",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "host",
		against: atlassianAccount,
		decides: "a host compares without case and without an absolute name's trailing dot, and an address that " +
			"does not parse has none",
		digest: "6d24b643eb1f8e30fd37f6d5ccb9d2b7e08fcdc02905a38167b965efde3c8b76",
		read:   cloudRuleRead,
	},
	{
		file: atlassianHostGo, name: "Gateway",
		against: cloudIDGateway,
		decides: "a cloud id is read through the gateway of its site's environment, Government Cloud's for a site " +
			"under atlassian-us-gov-mod.net and the commercial Cloud's for any other, so always through one of " +
			"the environments' gateways, and a product named by a cloud_id, given or discovered, is on Cloud",
		digest: "bab4934fed37904531e5b3b8da88f62845d677bd2479d4ab11fb73bcfa507daf",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, name: "atlassianGatewayURL",
		against: cloudIDGateway,
		decides: "a cloud_id's address is on the host of the gateway Gateway names for its site",
		digest:  "45027e6bb99e10f82f436c16f02b6bbca826dae57f16214143c191ec997b5210",
		read:    cloudRuleRead,
	},
	{
		file: integrationsGo, name: "(*Jira).BaseURL",
		against: cloudIDGateway,
		decides: "Jira named by a cloud_id is read at its gateway address (atlassianGatewayURL), and by a url at " +
			"the url",
		digest: "3519295ac5e07dd3f0acf201cd9fab0bcef0aeb3769aa39e2de41f6538121b66",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, name: "(*Confluence).BaseURL",
		against: cloudIDGateway,
		decides: "Confluence named by a cloud_id is read at its gateway address (atlassianGatewayURL), and by a " +
			"url at the url",
		digest: "9fe72f7427e9d3f6a889b230bc8842287ae0e02670a417f1d2dcbc3384c720a2",
		read:   cloudRuleRead,
	},
	{
		file: integrationsGo, name: "(*Jira).ResolvedBaseURL",
		against: atlassianAccount,
		decides: "Jira is read at BaseURL of its url, cloud_id and site_url, each with its ${NAME} references " +
			"resolved and its spaces trimmed, which leaves the url the operator renders, holding none, as written",
		digest: "3fe27899fa6491dbbaffb5cab77690c6fa62b8652f5243a94028255449c89db4",
		read:   addressRead,
	},
	{
		file: integrationsGo, name: "(*Confluence).ResolvedBaseURL",
		against: atlassianAccount,
		decides: "Confluence is read at BaseURL of its url, cloud_id and site_url, each with its ${NAME} " +
			"references resolved and its spaces trimmed, which leaves the url the operator renders, holding none, " +
			"as written",
		digest: "cffc562ba4852ccb7345e3f8acba365220049656bd6347a2146c84121555c544",
		read:   addressRead,
	},
	{
		file: "internal/engine/jira.go", name: "jiraBaseURL",
		against: atlassianAccount,
		decides: "the engine derives Jira's deployment and builds its clients (startJira, rewireJira and the " +
			"setup pass) at ResolvedBaseURL of the company's jira block",
		digest: "f1c2e48505b4224df1b3ee4c3229e88491533e85bec1630c246938734cff4454",
		read:   addressRead,
	},
	{
		file: "internal/engine/integrations.go", name: "confluenceBaseURL",
		against: atlassianAccount,
		decides: "the setup pass, which registers Confluence's webhooks as its deployment has them, builds its " +
			"client at ResolvedBaseURL of the company's confluence block",
		digest: "699e7b7fcfebc0fe31da4309ff0151693c125c7802a70422e394301b7488b83f",
		read:   addressRead,
	},
	{
		file: integrationsGo, name: "(*Atlassian).Declared",
		against: atlassianAccount,
		decides: "a company whose integrations.atlassian names no deployment, as every document the operator " +
			"renders, declares none (atlassianhost.Unknown), never the form's default of cloud",
		digest: "c17c2425fd965b5664acc2bb1cf785dadededc0b80011c4b8579784d58b236fa",
		read:   cloudRuleRead,
	},
	{
		file: "internal/jira/payload.go", name: "personID",
		against: atlassianAccount,
		decides: "a Jira webhook names a person by accountId on Cloud and by name, the username, on Data Center, " +
			"which is what a person's atlassian_account_id is matched against",
		digest: "f3e78974748a84706859ffe74740d238a17a72477bda196106b7837d7e2f105f",
		read:   atlassianRead,
	},
	{
		file: "internal/confluence/parse.go", name: "(*Parser).base",
		against: atlassianAccount,
		decides: "a Confluence webhook names its actor by accountId on Cloud and by username, else the user key, on " +
			"Data Center, which is what a person's atlassian_account_id is matched against",
		digest: "f93e753e2226757e3cd1e14f55610c86f3f384efb77980612c69d8458b1e9ab6",
		read:   atlassianRead,
	},
	{
		file: profileFile, name: "IsCredentialName",
		against: credentialNames,
		decides: "a name says it holds a credential when one of its parts, cut at every character that is not a " +
			"letter or a digit and read without case, is one of credentialWords (which " +
			"TestCredentialNameFollowsTheEngineSource holds)",
		digest: "c1e074261881108c0b2fc7785714d31c2547d6498d870d22afeb892b59a7321d",
		read:   credentialNameRead,
	},
	{
		file: mcpbridgeGo, name: "Build",
		against: []string{executorGo, companyWorkloadGo},
		decides: "a node builds its tool bridge from CREWLET_MCP_BRIDGE_URL, from the loopback address of its own " +
			"listener, or from both, and given neither (a variable of spaces alone being none) builds none, so every " +
			"launch of a box that would dial it is refused",
		digest: "904155dfd22fec3dbf6518ab5ccc6df7d14b0e8ec0180bc4bc477df57ef672ca",
		read:   executorRead,
	},
	{
		file: listenersGo, name: "(*nodeListeners).bridgeHostURL",
		against: append([]string{companyWorkloadGo, seatPoolsGo}, bridgeVerdict...),
		decides: "only a node that runs both the ingress and the seats roles and binds a listener has a loopback " +
			"address of its own bridge, at api.public where it binds one: a data pod, never a seat pod, whose direct " +
			"boxes dial CREWLET_MCP_BRIDGE_URL like a remote sandbox's, so the operator gives every engine pod the " +
			"Company's public address, and ExecutorOf reports a Direct seat on a data pod as runnable with no " +
			"address, and one in a seat pool as BridgeUnreachable while the Company has none",
		digest: "bccfb6220c20a79cd5aa212ffb9adb41735772aaa37eff24395e9708b6a82efe",
		read:   executorRead,
	},
	{
		file: listenersGo, name: "loopbackURL",
		against: append([]string{companyWorkloadGo}, bridgeVerdict...),
		decides: "a listener bound on every interface, as a data pod's api.public is, is dialled at 127.0.0.1 and its " +
			"port over plain http, so a data pod's direct boxes reach its bridge with no address of the operator's, " +
			"and ExecutorOf reports a Direct seat on a data pod as runnable whether or not the Company has one",
		digest: "3858556fc7840e1e4d62f1a9b73a8888c265c299aa8c766886017d23db43dc0b",
		read:   executorRead,
	},
	{
		file: bridgeGo, name: "(*Bridge).Reaches",
		against: []string{companyWorkloadGo, executorGo},
		decides: "a box on its node's own host dials the node's own listener wherever the node has one, ahead of " +
			"CREWLET_MCP_BRIDGE_URL, and every other box the variable alone, so the public address a data pod is " +
			"given moves none of its direct boxes off loopback",
		digest: "c0df6ef2a0204972856c3580b014a6bccc40e3485104c9151de5a08585a1476a",
		read:   executorRead,
	},
	{
		file: bridgeFleetGo, name: "(*Engine).joinBridgeFleet",
		against: []string{executorGo, seatPoolsGo, bootstrapGo, companyWorkloadGo},
		decides: "every node with a bridge joins the fleet's relay, a seat pod included, so a box's call to the data " +
			"pods reaches a seat a seat pod holds and the operator gives every engine pod the one address; only an " +
			"ingress node at api.port 0 stays out",
		digest: "6cb403b397c5b3e76772569894f2052b1b4b75572dfbda6cefec1304d5889610",
		read:   fleetBridgeRead,
	},
	{
		file: seedGo, name: "decideFile",
		against: []string{configServingGo, bootstrapGo},
		decides: "a node whose store is scratch reads nothing of it at boot and starts on its seed alone, which the " +
			"operator never gives a seat pod, so a seat pod whose engine is not running starts on no revision and " +
			"needs no value kept for it",
		digest: "712f6a1328c920bade21f954015f268e8afead6cd200b75df7dc6215a085390e",
		read:   bootRevisionRead,
	},
	{
		file: storeGo, name: "OpenNode",
		against: []string{configServingGo, bootstrapGo},
		decides: "a scratch store's file is deleted before the node opens it, so a seat pod's emptyDir holds no " +
			"revision across a restart of its container",
		digest: "e6bccb9596966fc6cd3bba04a8622830b5be4173996ef0cf56dc034f86da3302",
		read:   bootRevisionRead,
	},
	{
		file: bootGo, name: "PublicListener",
		against: []string{bootstrapGo},
		decides: "a node without the ingress role never binds api.public, so a seat pod is given no api.public block",
		digest:  "83a3c86ffc1eed9410a71fd6d1298c907c1e444406e1dedf6170a7ab02128b8b",
		read:    fleetBridgeRead,
	},
	{
		file: apiConfigGo, name: "API.externalLoopback",
		against: signInPosture,
		decides: "a node is reached on loopback when the host of its api.external_url is localhost, case aside, or a " +
			"loopback IP, where totp optional needs no accept_insecure, and on no other address, where the node " +
			"refuses to start without it",
		digest: "fa3825b5e04b0790d1336150a10d421dfbfbbda3a21c962868a8c0795ff6134f",
		read:   signInRead,
	},
	{
		file: apiConfigGo, name: "API.ExternalBase",
		against: signInPosture,
		decides: "the address externalLoopback judges is api.external_url with its surrounding spaces and trailing " +
			"'/' removed",
		digest: "783e12fe587aeebcb7817c63074c01025e278b1885608e55a927740aed6f5d6b",
		read:   signInRead,
	},
	{
		file: normalizeGo, name: "(*Bootstrap).normalize",
		against: addressCopies,
		decides: "every string of a Tier A is judged and used with the white space around it removed, an " +
			"api.auth.allowed_origins entry included",
		digest: "5a9cb62c15c8dcbadadd8f8723365499b907bf074ea072a57b2b9e67c4574a9e",
		read:   addressShapeRead,
	},
	{
		file: apiConfigGo, name: "(*API).validateExternalURL",
		against: addressCopies,
		decides: "api.external_url is judged with its surrounding spaces removed, by checkExternalURL and then, as " +
			"a browser's address, by checkBrowserHost",
		digest: "9ae3a0bc910a331cf4c7e823c65a4c4343fa497b911f1f9670687004909588d9",
		read:   addressShapeRead,
	},
	{
		file: apiConfigGo, name: "(*APIPublic).validate",
		against: addressCopies,
		decides: "api.public.external_url is judged with its surrounding spaces removed by checkExternalURL alone, " +
			"since no browser compares an origin with it",
		digest: "06e5094cc09c9a382f12536b7483c70b34d6ca93efc26feb485fb3cae5c708c9",
		read:   addressShapeRead,
	},
	{
		file: apiConfigGo, name: "checkExternalURL",
		against: addressCopies,
		decides: "an address a node is reached at starts with http:// or https:// in lower case, parses as a URL, " +
			"has a host, a port checkPort takes, and no query or fragment; a path is taken",
		digest: "60a7a52edb8851b986b55a57ccbe60e49652ac1669a357b4607c3a2d9fec9635",
		read:   addressShapeRead,
	},
	{
		file: integrationsGo, name: "hasHTTPScheme",
		against: addressCopies,
		decides: "the scheme checkExternalURL takes is http:// or https:// spelled in lower case",
		digest:  "a76612d926d38783c3ef5be235aa48a1f5dadd645271cc1ea44e70edb2b77130",
		read:    addressShapeRead,
	},
	{
		file: apiConfigGo, name: "checkPort",
		against: addressCopies,
		decides: "a URL's port, when it has one, is a number from 1 to 65535",
		digest:  "4dfe1cf753019e1c41f3e579509d5a5e790c28fa4bdc953170954e1e8aff4858",
		read:    addressShapeRead,
	},
	{
		file: apiConfigGo, name: "checkBrowserHost",
		against: addressCopies,
		decides: "a browser's address has a host in ASCII, holding no '%', and one whose last label is a number " +
			"(endsInNumber) only as an IPv4 address of four decimal octets",
		digest: "ab83e9286e1d46a7ec8a9fbde1ad32e031f1f1a408d1659425d5c3be39a81fca",
		read:   addressShapeRead,
	},
	{
		file: apiConfigGo, name: "endsInNumber",
		against: addressCopies,
		decides: "a host's last label, one trailing dot aside, is a number when it is all digits or 0x and hex " +
			"digits, in either case",
		digest: "9dac04214bdf618b1a3663e98c90e93c66ad5e8a52839b2c22a69da014ccb6c0",
		read:   addressShapeRead,
	},
	{
		file: apiConfigGo, name: "checkOrigin",
		against: addressCopies,
		decides: "an api.auth.allowed_origins entry, as written, is scheme://host[:port] and nothing more, on http " +
			"or https, with a port checkPort takes and a host checkBrowserHost takes",
		digest: "b1847a33a8f613437ac8c52830fe6abbf7840d4dd9f0ce79f0550b2c8702f8cc",
		read:   addressShapeRead,
	},
	{
		file: "internal/coord/kv/objects.go", name: "(*FleetStore).AgreeObjectBackend",
		against: bootWindows,
		decides: "the object store's fleet record is created, or read back, in requests bounded only by the " +
			"client's 5 s default",
		digest: "df14c1a0c958506e2c63dc1d02480705ef8a34a0d6c0cd6e24e2406eec4f29a8",
		read:   bootBoundsRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).Floors",
		against: bootWindows,
		decides: "the fleet's trim floors are one certified listing",
		digest:  "bfa507886b1a36731fb7ef23a0c0025ebd6e64a7eaebcad7d9664789cfdcd09b",
		read:    bootBoundsRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).Maintenance",
		against: bootWindows,
		decides: "a log's maintenance window is one leader read at the client's 5 s default",
		digest:  "2d64c1ab6c4955dc784ad67efb803ed1847947257f81d1aa09b1c6d7ea808e69",
		read:    bootBoundsRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).Positions",
		against: bootWindows,
		decides: "the fleet's positions are one certified listing",
		digest:  "138413da5b12c93bcc094b3e9a44623b5395b25f4c1f29d9038b0b3cd54d20b5",
		read:    bootBoundsRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).PutAdmission",
		against: bootWindows,
		decides: "an admission is one write at the client's 5 s default",
		digest:  "3ce67a6c90426322796c9c4cd723235bccef5221a9479c0c53aaf1cb1712ee9d",
		read:    bootBoundsRead,
	},
	{
		file: walkGo, name: "beginPass",
		against: bootWindows,
		decides: "a pass reads the key index beside the creation of its watcher",
		digest:  "1bade1490d991bab472776aff1e3fe15872c3ec771b62a2f2c068ec16e79dd0f",
		read:    bootBoundsRead,
	},
	{
		file: walkGo, name: "listUnder",
		against: bootWindows,
		decides: "a certified listing begins a pass, walks it to its end, stops its watcher and certifies it",
		digest:  "826316a80dc0d32207d7580e1c37d4f3d0e3bec11277cdd503ea80bc58acea74",
		read:    bootBoundsRead,
	},
	{
		file: walkGo, name: "passIdle",
		against: bootWindows,
		decides: "a certified listing ends a pass that delivered nothing for 5 s",
		digest:  "a4e20590cf172727d11914965721b0568320621fdb22673a56eac5a14eeb237f",
		read:    bootBoundsRead,
	},
	{
		file: walkGo, name: "passOver",
		against: bootWindows,
		decides: "a pass ends on its end marker, or once it has gone passIdle without a delivery after the index " +
			"answered",
		digest: "c35d03701efa142d2d79312757ee4923e8b028ce5b336ab3cafa30777b125f95",
		read:   bootBoundsRead,
	},
	{
		file: backendsGo, name: "attachCoordination",
		against: bootWindows,
		decides: "the coordination buckets are opened under one sequence ceiling of their own",
		digest:  "f5e65acfd07add41b5831c2e593e55405c36745610b0d7110f66d48013c32bd4",
		read:    bootBoundsRead,
	},
	{
		file: backendsGo, name: "clusteredStream",
		against: bootWindows,
		decides: "a node that is not a broker member, or one with a cluster, provisions coordination and objects " +
			"on the clustered budgets",
		digest: "be2b4afb51da5337abf7c792b01b2ad2ccdbf2ce42b7380ecfd3f9d79d3b03b7",
		read:   bootBoundsRead,
	},
	{
		file: backendsGo, name: "objectsReservation",
		against: bootWindows,
		decides: "sizing the nats bucket reads the broker's limit under LookupBudget, and carries on from the " +
			"volume's free space past it",
		digest: "7ba9b5772807a60484d6bbe24850daf0e6b09617a7bd9c95a28722822c38aa45",
		read:   bootBoundsRead,
	},
	{
		file: backendsGo, name: "openObjects",
		against: bootWindows,
		decides: "the object store agrees the fleet's backend, then opens the nats bucket under a sequence ceiling" +
			" of its own or probes the S3 bucket",
		digest: "19bd69c70076d83dff808d97561d6408d2bde7f1f1d48819f82b1ac874f28af1",
		read:   bootBoundsRead,
	},
	{
		file: "internal/engine/core.go", name: "(*Engine).startCore",
		against: bootWindows,
		decides: "a data node starts its state logs before it reports itself recovered",
		digest:  "7d56777f94aa8119842badd13a388b3bb8eb9fad27a966c0a5e87bc01112cef3",
		read:    assistantLogRead,
	},
	{
		file: "internal/engine/evicted.go", name: "(*stateLog).fleetGenerations",
		against: bootWindows,
		decides: "the join's generations are one listing of the fleet's floors",
		digest:  "c7b4b8ded7d5a90a16ccdf3879fa9b6e74d8741208d43f845f0d5f732908ee45",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/fleetbroker.go", name: "(*Engine).serveFleetBroker",
		against: bootWindows,
		decides: "a broker member registers its fleet broker answerer, one flush",
		digest:  "acd95e184e9549186af2396272a788ee488cf511b1ce3357fbe96d506dd28d99",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/history.go", name: "(*Engine).armHistory",
		against: bootWindows,
		decides: "a data node registers its history answerer, one flush",
		digest:  "a9ece00e3267829897fa33ad1a020dae4cc28592402905630304cc9f44dd97a6",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/inboxrelay.go", name: "(*Engine).armInboxRelay",
		against: bootWindows,
		decides: "a data node registers its inbox relay, one flush",
		digest:  "673a18d6bcdf74dad77ace48bd70a663d2879b58bd0e5590de638434cb204d53",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/maintenance_mode.go", name: "(*Engine).admit",
		against: bootWindows,
		decides: "a data node's admission writes its record and reads each log's maintenance window",
		digest:  "053e5edea1b55b86e8eb47fda76435d074ff5c1ae42b15c542d177293b116d20",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/presence.go", name: "(*Engine).armPresence",
		against: bootWindows,
		decides: "every node registers its presence answerer, one flush",
		digest:  "4c0d3b84ff37218350fee94835fede14371d3dd70a6fc0e967fafba1f1b8bf71",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/remote.go", name: "(*Engine).serveEstate",
		against: bootWindows,
		decides: "a data node registers its estate service, one flush",
		digest:  "d6a146c8e4b638098a187a7b279dd60361f25c6c251590a3243bb1b1a66c720a",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/remote.go", name: "(*Engine).takeCustody",
		against: bootWindows,
		decides: "a data node attaches its custody keeper to the custody group",
		digest:  "bf26445c8ed37c6bf4aee6c4805b6bac4410ecf8cb058477d09c99433a4cebc8",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/secrets.go", name: "(*Engine).bootSecrets",
		against: bootWindows,
		decides: "a boot's read of the company's secrets has no bound: it asks a store that did not answer again " +
			"for as long as the boot lives, and fails only on a value the node's keyring cannot open",
		digest: "6c3dd24b5cfa2bd48562316fb4fab85a4489bea1c6a983fa9e8ab35440f573d9",
		read:   bootRecoveryRead,
	},
	{
		file: stateLogGo, name: "(*Engine).join",
		against: bootWindows,
		decides: "a node's join reads whether its rows can replay, and asks for a donor only when they cannot",
		digest:  "51ba54deb4eb2abdc3365a04ca263e2e9c218ac809eba67b43ae5c00d35eb166",
		read:    bootRecoveryRead,
	},
	{
		file: stateLogGo, name: "(*Engine).startStateLog",
		against: bootWindows,
		decides: "the state logs read their ceilings, are provisioned under one sequence ceiling, joined, and " +
			"started under another",
		digest: "e23f2f866e38084ca2d6ed5034923b4f3ad38703397b23ed6c48f09ad3ec0984",
		read:   bootRecoveryRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).provisionAll",
		against: bootWindows,
		decides: "every registered domain's log is provisioned in turn",
		digest:  "1cb3ef19075fc9b3537ad040be545f6e995da3862bf96d78855d0e697bd0bc34",
		read:    bootBoundsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).replayable",
		against: bootWindows,
		decides: "the join lists the fleet's positions and floors, then reads each log's stats and trim floor",
		digest:  "ea1c92e0718f299efaab001075f05a54cf8525842627b3f323f7191fc1b6dd0f",
		read:    bootBoundsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).start",
		against: bootWindows,
		decides: "a log's start reads its stats, attaches its consumer and verifies its checkpoint",
		digest:  "db62104d27fe1aba85ff011579ef018f65c2862a9b20930cf31eeed5771bcceb",
		read:    bootBoundsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).startLog",
		against: bootWindows,
		decides: "each log is started in turn, under the consumers' sequence ceiling",
		digest:  "501906b3addfef47df47b6da4a96209f955b9da2788e709d41f9d3c34a748efe",
		read:    bootBoundsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).trimFloor",
		against: bootWindows,
		decides: "a log's trim floor is one listing of the fleet's floors",
		digest:  "579fb7d3962be4896893e798a1b511f22be5d4d789580da7c70ebf962ee52dc9",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/statelogceilings.go", name: "ceilingsFor",
		against: bootWindows,
		decides: "the state logs' ceilings are read together, then the broker's limit once",
		digest:  "2a5d1f293d143df9260721484834d6fa2ed5858e7c578d59eefb035e4106da8e",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/statelogceilings.go", name: "readHeld",
		against: bootWindows,
		decides: "every log's ceiling is read concurrently, each under LookupBudget",
		digest:  "f55d995c036ee5fb4a72a67308b8e55fcc75889795a2af49de0d9358438db8b8",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/steer.go", name: "(*Engine).armSteer",
		against: bootWindows,
		decides: "every node registers its steer answerer, one flush",
		digest:  "4ee600edf7605e6d684024ab1fb004dad231aab572c6f0dae31e80ad18fb07f1",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "LookupBudget",
		against: bootWindows,
		decides: "a lookup of a stream or bucket, and the broker's limit read, are bounded at 30 s",
		digest:  "fa87cb9b864068dc2bf52d7f9d17a79b008f788acbf791b1f3737e298adf9de0",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "ReadBack",
		against: bootWindows,
		decides: "a create's read-back is bounded at 5 s",
		digest:  "0a5a56836bb851f95d3e62cf9eb7aadfe894766f2899274f59f46ee66daabe59",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "ReadTerm",
		against: bootWindows,
		decides: "a read of a stream's existence is given a second",
		digest:  "9b1814c736031af0d54ea98aad986deea84fdf3beb58a962ae0e4bf8bcf36ac9",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "clusterBudget",
		against: bootWindows,
		decides: "one clustered create is bounded at two minutes",
		digest:  "44ca09aac04b2b49fca3652db639bd8dad3c1203e22740bbaa2b6f5ed8f652a7",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "clusterSequenceBudget",
		against: bootWindows,
		decides: "a clustered bring-up of many creates is bounded at five minutes",
		digest:  "06979cf6f3c68a0eebdea814a96ac2cb8bfa9a09acfa49e70ad2eb3bec3f749e",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "soloBudget",
		against: bootWindows,
		decides: "one create on a lone member is bounded at 30 s",
		digest:  "714fc4a3fac977cf4ce24b797a61b4ed5ac4abdb6e7cceb1c357f57bfebbb360",
		read:    bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "soloSequenceBudget",
		against: bootWindows,
		decides: "a lone member's bring-up of many creates is bounded at two minutes",
		digest:  "b1ea5d569d918231b9f10db39bd61597d725f546f6a0880dbdc94285f8ad960d",
		read:    bootBoundsRead,
	},
	{
		file: "internal/objstore/backend.go", name: "MiBPace",
		against: bootWindows,
		decides: "a body is given 30 s a started mebibyte",
		digest:  "5a7270aaad55629662d0fafb7703ad817c56817ff6984bb48b1e89842ec274e7",
		read:    bootBoundsRead,
	},
	{
		file: natsObjGo, name: "Open",
		against: bootWindows,
		decides: "the nats bucket is looked up, created, read back and bound, each under its own budget",
		digest:  "9e93e8da5662ef36648f50c6eeb77004b986bd479534d398a0be174d55a7c47e",
		read:    bootBoundsRead,
	},
	{
		file: "internal/objstore/s3obj/probe.go", name: "(*Backend).probe",
		against: bootWindows,
		decides: "the S3 probe makes a put, a delete, three presigned puts, a conditional delete and a cleanup " +
			"delete",
		digest: "0a32835a0c0716b17e34546b8b6ef8bc10e9fe6a2dc1440a933559687fd50a5e",
		read:   bootBoundsRead,
	},
	{
		file: "internal/objstore/s3obj/s3obj.go", name: "callBudget",
		against: bootWindows,
		decides: "an S3 request is bounded at 30 s, plus MiBPace for a body",
		digest:  "84d7159868e348fb81b3350f140d50d338cf34adc68c7788c7dc5ffde6002f24",
		read:    bootBoundsRead,
	},
	{
		file: "internal/observe/custody.go", name: "(*Keeper).Start",
		against: bootWindows,
		decides: "the custody keeper reconciles what it holds, then attaches to its group",
		digest:  "1fa472fde8f947abeb6decd310f3ef13b878eb2fd283dc54e23e8a69b3f6ddb3",
		read:    bootBoundsRead,
	},
	{
		file: "internal/queue/jetstream/attach.go", name: "(*Queue).attach",
		against: bootWindows,
		decides: "attaching a durable group looks up, creates and reads back its consumer, then looks it up and " +
			"reads it back again",
		digest: "c10a5e667bc9202dd55d2322b9a559a79adfde4884020720c8f933c7e196e118",
		read:   bootBoundsRead,
	},
	{
		file: embeddedGo, name: "(*embeddedServer).awaitLeafReady",
		against: bootWindows,
		decides: "a leaf asks for the fleet's JetStream until clusterReadyTimeout, checking the deadline only " +
			"before each ask",
		digest: "18c689dfdc9356ecd3d8d273914a0c19d393047d2a9c69234146979fa92fadd3",
		read:   bootBoundsRead,
	},
	{
		file: embeddedGo, name: "(*embeddedServer).connect",
		against: bootWindows,
		decides: "a connection to the node's own broker is a handshake bounded by listenTimeout, then held to the " +
			"contract",
		digest: "879f96f323d34070554be24a1ff2aede52334a0cb153d33147d42a1bb4c072a3",
		read:   bootBoundsRead,
	},
	{
		file: embeddedGo, name: "awaitBoot",
		against: bootWindows,
		decides: "a member's listeners are waited for under listenTimeout once the broker has started, and a " +
			"leaf's pass at once",
		digest: "034f931f369cf60c59727138f592c3abb4a017386adf9250a8a8b7dbf13faba3",
		read:   bootBoundsRead,
	},
	{
		file: embeddedGo, name: "clusterReadyAsk",
		against: bootWindows,
		decides: "each ask of a leaf's readiness wait is a ReadTerm, begun before its deadline is checked again",
		digest:  "47d1f25a48ce5a2d0b6d22c23ed1fd2385abd80cc2738b213966e72e5e39038b",
		read:    bootBoundsRead,
	},
	{
		file: embeddedGo, name: "clusterReadyTimeout",
		against: bootWindows,
		decides: "a leaf waits 60 s for its link to a member and the fleet's JetStream to answer across it",
		digest:  "e113ad46469a3410dbdfb88eb60d1797073b03de61d9248f4ad4264bc23d45b2",
		read:    bootBoundsRead,
	},
	{
		file: embeddedGo, name: "listenTimeout",
		against: bootWindows,
		decides: "a connection to the node's own broker has 30 s for its handshake and 30 s for the flush that " +
			"reads max_payload, and a member's listeners 30 s to be ready",
		digest: "c86fc21135cf320e770660373a8825aeb93af3f01077e8041adc80e107d477a1",
		read:   bootBoundsRead,
	},
	{
		file: "internal/queue/jetstream/payload.go", name: "announcedMaxPayload",
		against: bootWindows,
		decides: "holding a connection to the contract flushes it once, under the connection's own timeout",
		digest:  "68b9138e708c957990f490b4e67734758eb6968950ac8198524ef08a39356b86",
		read:    bootBoundsRead,
	},
	{
		file: "internal/queue/jetstream/payload.go", name: "heldToTheContract",
		against: bootWindows,
		decides: "every connection to the node's own broker reads the broker's max_payload before it is used",
		digest:  "3763a4087ba9c572f2b5ce5296ad09c660bb52822269b9a1fa6bb54ab67fdab7",
		read:    bootBoundsRead,
	},
	{
		file: queueGo, name: "(*Queue).DialWatched",
		against: bootWindows,
		decides: "the coordination connection is a second connection to the node's own broker",
		digest:  "2f2801f816363be34f7e0e54a083e0ecce03a2c9a341de51f2d18f4eb7d84dd7",
		read:    bootBoundsRead,
	},
	{
		file: queueGo, name: "(*Queue).ensureStreams",
		against: bootWindows,
		decides: "the engine's streams are provisioned under one sequence ceiling",
		digest:  "7a6b1d446cebce85a83447482dae05456724529c94eaf9405202e5a8915ffb16",
		read:    bootBoundsRead,
	},
	{
		file: queueGo, name: "Config.Clustered",
		against: bootWindows,
		decides: "a leaf, and a member with a cluster or a URL, provisions on the clustered budgets, and a lone " +
			"member on the solo ones",
		digest: "18ddd1c5ce31decde14d4b90b5447baff44efcc20777bd54b549bf225b4a1558",
		read:   bootBoundsRead,
	},
	{
		file: queueGo, name: "newQueueOn",
		against: bootWindows,
		decides: "the queue's boot connects, waits for a leaf's link and a member's cluster, then provisions the " +
			"streams, in that order",
		digest: "050788e956291f25654a7097134e229a46c6e4cbc8d8c86d76ff23432a9a6d48",
		read:   bootBoundsRead,
	},
	{
		file: "internal/queue/jetstream/scatter.go", name: "flushBudget",
		against: bootWindows,
		decides: "registering what a node answers flushes its subscription within 10 s",
		digest:  "d30349eb1ca8de06f83cefe4a40a5f7a04a530d0c795602242581534196ac1e6",
		read:    bootBoundsRead,
	},
	{
		file: "internal/statelog/apply.go", name: "(*Runner).VerifyCheckpoint",
		against: bootWindows,
		decides: "verifying a log's checkpoint reads the stream's info and one message",
		digest:  "da09d1e9aa0b0bb83183b96b050fdebfe9d0cc61817751163153caca768b2277",
		read:    bootBoundsRead,
	},
	{
		file: "internal/statelog/transfer.go", name: "OfferWindow",
		against: bootWindows,
		decides: "a node behind its logs waits 5 s for a donor's offer",
		digest:  "cc939f4812fb244a16ecd0c6a1d5104a92c132f0d7eb0c697d5bead6a2c427e3",
		read:    bootBoundsRead,
	},
	{
		file: "internal/store/turso.go", name: "acquireTursoLock",
		against: bootWindows,
		decides: "the native library's extraction lock is polled until its deadline",
		digest:  "0b94f18ab1dd1f9d9d0a1e4a96ba52b81550992149cc965f48fc2258133d83bd",
		read:    bootBoundsRead,
	},
	{
		file: "internal/store/turso.go", name: "tursoPrepareTimeout",
		against: bootWindows,
		decides: "the native library's extraction lock is waited for a minute, then the store carries on without " +
			"it",
		digest: "642997e5d66a77954f7a4dcc1a90d12572440f35fa5e750cd2fb0283c01c5807",
		read:   bootBoundsRead,
	},
	{
		file: walkGo, name: "certify",
		against: bootWindows,
		decides: "a certified listing reads back from the leader, one at a time, every key its index names that the " +
			"pass did not deliver live",
		digest: "6d82be18f819632018c6a01071a9b1d2b5dd8971038bf0afe8d8e367cc5d5013",
		read:   bootBoundsRead,
	},
	{
		file: "internal/jsapi/leader.go", name: "(*Leader).get",
		against: bootWindows,
		decides: "a leader read on a context with no deadline is bounded by the client's DefaultTimeout",
		digest:  "2f238e2e50c4fd88d34c368d06b29acb40575b09da6c5e30a6cc0441ad372fe1",
		read:    bootBoundsRead,
	},
	{
		file: "internal/statelog/transfer.go", name: "collectOffers",
		against: bootWindows,
		decides: "a donor's offers are collected for the window, and the collection then waits for the listing it " +
			"started to stop",
		digest: "f6d5e70fdf06b6a53652075f759399e7290302067bc534e9a57f59102a6c23a5",
		read:   bootBoundsRead,
	},
	{
		file: "internal/coord/kv/race.go", name: "(*FleetStore).retryRaces",
		against: bootWindows,
		decides: "a fleet write that loses a race is attempted again for as long as the client's 5 s default since" +
			" the first",
		digest: "5ab491aab57c57858b1b8b719c64a820599a4b5ae4a3bf1c474ba5980a5cc4b2",
		read:   bootBoundsRead,
	},
	{
		file: "internal/coord/kv/markers.go", name: "createRounds",
		against: bootWindows,
		decides: "a create-only write goes on for at most eight rounds over removal markers",
		digest:  "cd96404addaa827263e86bb4eb59cb3faa86b3fc2f0fca0c562541ea65946431",
		read:    bootBoundsRead,
	},
	{
		file: "internal/coord/kv/markers.go", name: "createKey",
		against: bootWindows,
		decides: "each round of a create-only write is a settled update and a leader read, each at the client's 5 " +
			"s default",
		digest: "d52d5def68b8d33f4b3f0e48d94386f5632a0f24fd0c5e470bd98032c32ce6c0",
		read:   bootBoundsRead,
	},
	{
		file: natsObjGo, name: "open",
		against: bootWindows,
		decides: "the nats bucket is looked up, created and read back, then bound, under the window it is handed",
		digest:  "19f98c44b5091f2ee1e02867c3c43c2fdb58d7725922de6e02fad24b4660b5b1",
		read:    bootBoundsRead,
	},
	{
		file: natsObjGo, name: "lookupBucket",
		against: bootWindows,
		decides: "the bucket's lookup is bounded by the timing's Lookup",
		digest:  "5f6d37bb7d8d6d15f34240329eb52abdaf6fadfcbadc5c71ca953f790d6eca98",
		read:    bootBoundsRead,
	},
	{
		file: natsObjGo, name: "createBucket",
		against: bootWindows,
		decides: "the bucket's create is bounded by the timing's Budget, and read back under ReadBack",
		digest:  "9b6c501a360dc5fa65d799d42ee922e15fe8569074a9fa0c0171b7be007cc36b",
		read:    bootBoundsRead,
	},
	{
		file: natsObjGo, name: "boundBucket",
		against: bootWindows,
		decides: "a bucket with no ceiling is given one only when the ceiling is named explicitly",
		digest:  "07f0675b5fe636195f957377fe60140f3158d85b2fdabfe55a2a4bdc2f6cb79c",
		read:    bootBoundsRead,
	},
}

// TestMirroredEngineDeclarationsAreUnchanged holds every declaration in
// mirrored to the engine the suites run: one whose digest moved is a
// declaration somebody reads again against the operator file named, and one
// that is gone is a decision somebody finds again. The test fails until its new digest is
// recorded, so the read is made before the change lands, not when a seat
// first meets the difference.
func TestMirroredEngineDeclarationsAreUnchanged(t *testing.T) {
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
// nothing when the declaration there is the one that was read.
func (m *mirror) check(source *os.Root, version string) string {
	at := fmt.Sprintf("%s %s, in crewlet %s,", m.file, m.name, version)
	copies := strings.Join(m.against, ", ")
	src, err := source.ReadFile(m.file)
	if err == nil {
		var digest string
		if digest, err = enginetest.Fingerprint(m.file, src, m.name); err == nil {
			if digest == m.digest {
				return ""
			}
			return fmt.Sprintf("%s is not the declaration the operator's copies of it were read against, which copy "+
				"from it that %s. `git log -p %s..%s -- %s` in a crewlet checkout shows what changed since. Read it "+
				"against each of %s, make the operator follow it where it no longer does, then record digest %q "+
				"and read %q for it in %s",
				at, m.decides, gitRev(m.read), gitRev(version), m.file, copies, digest, version, mirrorTable)
		}
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, enginetest.ErrNoDeclaration) {
		return fmt.Sprintf("%s is gone (%v), and the operator copies from it that %s. Find where the engine "+
			"decides that now, read it against each of %s, and name that declaration in %s in place of this one, "+
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

// TestAMirroredDeclarationThatMovedFails holds check to its job, on a module
// source of its own: the function as read passes, and a digest that differs,
// a function the file no longer declares and a file that is gone each fail,
// naming the function, the operator file to read it against, and what to
// record.
func TestAMirroredDeclarationThatMovedFails(t *testing.T) {
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
	read := mirror{file: file, name: "pick", against: []string{signinGo, executorGo}, decides: decides,
		digest: digest, read: "v0.1.1-0.20261010000000-ba9876543210"}
	if problem := read.check(source, version); problem != "" {
		t.Fatalf("the function as read fails: %s", problem)
	}

	moved := read
	moved.digest = strings.Repeat("0", len(digest))
	gone := read
	gone.name = "choose"
	elsewhere := read
	elsewhere.file = "internal/engine/launch.go"
	for name, c := range map[string]struct {
		m    mirror
		want []string
	}{
		"a digest that differs": {moved, []string{
			file, "pick", "crewlet " + version, signinGo, executorGo, decides,
			"git log -p ba9876543210..0123456789ab -- " + file,
			fmt.Sprintf("record digest %q and read %q", digest, version), mirrorTable,
		}},
		"a function that is gone": {gone, []string{
			file, "choose", "is gone", signinGo, executorGo, decides, mirrorTable,
			"the digest and version to record",
		}},
		"a file that is gone": {elsewhere, []string{
			"internal/engine/launch.go", "is gone", signinGo, executorGo, mirrorTable,
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
