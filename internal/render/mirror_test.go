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
// one the engine refuses to start on at all, or to boot on where a pod hands
// it to sandboxes, which the render holds a data pod back on (and the API
// refuses where a Company or its class states it) and the engine shows only
// for the addresses a test tries; and which engine pod's boxes dial its tool
// bridge on loopback and which dial the address the controller gives every
// engine pod, which Crewlet decides from the listeners a node binds when it
// starts, and where every executor run, whatever its cell, exports its
// traces, which it decides at each launch. A contract test
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
	doctorGo    = "internal/providers/llm/harness/doctor.go"
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
	// seatAppTokenRead is crewlet#395's engine, read at the head of its branch
	// before that change merged, which every mirror it moved was read again
	// at. A seat whose GitHub App is installed is issued its installation
	// token at use, as ${GITHUB_APP_TOKEN_<HANDLE>}, and only the seat's own
	// resolution answers it: runEnvironment, sandboxMCP and seatSpecs read the
	// seat's values through that resolution, resolveServer and refGap.hold
	// take what is minted or issued for the seat as one set, and a value
	// naming the token waits for the app's installation as a minted one waits
	// for its seal. Every layer, every merge order and every optional_keys
	// rule is as it was. The operator follows it by writing nothing: the API
	// refuses every ${NAME} the operator did not write, and the render writes
	// no ${GITHUB_APP_TOKEN_<HANDLE>}, so no value it renders is one the
	// engine issues, and each copy below holds for every document it renders.
	seatAppTokenRead = "v0.1.1-0.20261026223946-d58ab9ddf7af"
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
	// mcpServerTypesGo is the MCPServer API, and unitTypesGo the Unit API,
	// whose MCPCredentialValue a Seat's spec.mcp shares.
	mcpServerTypesGo = "api/v1alpha1/mcpserver_types.go"
	unitTypesGo      = "api/v1alpha1/unit_types.go"
	// toolServersMd and harnessesMd are the guides that state what a tool
	// server and an executor run are started with.
	toolServersMd = "docs/guides/tool-servers.md"
	harnessesMd   = "docs/guides/harnesses.md"
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
	// mcpNamesGo is where the engine holds a tool server's name, tool
	// prefix, value keys and address to what every client can use.
	mcpNamesGo = "internal/config/mcpnames.go"
	// mcpToolsGo is where the engine sets each tool of a server at a level,
	// and refuses a sandbox listing of a server that holds a tool back.
	mcpToolsGo = "internal/config/mcptools.go"
	// renderOrgGo is the render of the org chart, a Seat's sandbox included.
	renderOrgGo = "internal/render/org.go"

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
	// boxURLGo is where the engine judges an address a sandbox dials back to
	// it, and engineBoxURLGo where its boot reads every such address and
	// refuses one that is set and unusable, both first read at codeHostRead.
	boxURLGo       = "internal/sandbox/boxurl.go"
	engineBoxURLGo = "internal/engine/boxurl.go"
	// boxGitURLRead is crewlet/crewlet#298's engine whose boot reads a third
	// address a box dials back to the engine, read at the head of its branch:
	// the code host's base a coding box clones and pushes through
	// (CREWLET_BOX_GIT_URL, sandbox.BoxGitURLVar), through ParseBoxURL as the
	// tool bridge's and the telemetry receiver's are, its boot refused on a
	// value that is set and unusable. The operator hands its pods the
	// bridge's and the receiver's alone, which the render judges: it does not
	// offer the code host, so it sets no code host's base, and no pod of its
	// carries one another way, since the API refuses every CREWLET_ variable
	// a Company or a class gives the engine's pods. So no boot of its meets
	// the third address, and no verdict moves.
	boxGitURLRead = "v0.1.1-0.20261030022059-2887e830ccb5"
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
	// harness's own run in a box. A launch picks the first member of the
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
	executorRead = "v0.1.1-0.20261026201916-b7ebd490a7d1"
	// commitAttributionRead is crewlet#504's engine, read at the head of its
	// branch before that change merged, where the catalogue also carries
	// providers.sandbox.commit_attribution: on unless false, every run's own
	// commits credit Crewlet as co-author, which the Company's
	// spec.sandbox.commitAttribution renders.
	commitAttributionRead = "v0.1.1-0.20261101000021-5df52121cb3d"
	// uncappedRead is crewlet#546's engine: an executor run is uncapped unless
	// the Company or the Seat sets a round cap. providers.sandbox's
	// default_max_turns left out or 0 caps nothing, and the constant that
	// held the shipped 96 is gone; (*SandboxProvider).MaxTurns is where the
	// engine says it now, which the Company's spec.sandbox.defaultMaxTurns
	// and the Seat's spec.sandbox.maxTurns render.
	uncappedRead = "v0.1.1-0.20261101204309-4f58b2b8380e"
	// executorFinalRead is crewlet#388's engine again, read at the head of
	// its branch once crewlet's main to 7cd6b68b80 was merged into it,
	// before that change merged. A node without the ingress role that runs
	// seats now serves its own direct boxes the tool bridge and the
	// telemetry receiver on api.port, to loopback alone, where loopback
	// reaches the listener (boxHostURL, reachedOnLoopback), so a seat pod's
	// Direct boxes need no address of the operator's and nothing on a seat
	// pod dials the public route; a direct box exports its traces to its
	// own node's listener where the node forwards them (BuildOtelReceiver,
	// withTelemetry). A box is handed its entry's whole env (entryEnv,
	// SandboxEnv); a direct job lost with its engine is carried on, never
	// collected (jobEnd, carryOnLost); a deep-work box on the host lives under
	// the company's box root (boxRootOf, hostCell, BoxRoot); a boot floors
	// each seat's fencing epoch within the coordination window
	// (attachCoordination); and runEnvironment and sandboxMCP renamed a
	// parameter. Each mirror that moved was read again at it.
	executorFinalRead = "v0.1.1-0.20261028203606-c6ba342a52ad"
	// harnessRead is crewlet/crewlet#527's engine, read on crewlet's main
	// where it merged (986af5348e): the coding CLI is a harness throughout
	// the engine's Go, its packages move (cliagent to llm/harness, cliprofile
	// to harness/harnessprofile, sandbox/codingagent to sandbox/executor) and
	// its words with them. Every mirror it moved was read again at it, and
	// each moved by its names and its messages alone: a package qualifier, a
	// method (Provider.Agent is Harness), a field (a launch's CodingAgent is
	// Harness), and a refusal that says "harness" where it said "CLI". No
	// decision any of them copies moved.
	harnessRead = "v0.1.1-0.20261101141833-986af5348ea5"
	// envSignInRead is crewlet#452's engine, read on crewlet's main where it
	// merged (e0328e5d9d), which adds auth.mode env: a providers.llm entry
	// whose env alone signs its harness in. The names
	// that sign a harness in beyond its token and key variables moved from a list
	// of the Claude Code runner's (executor.ClaudeCodeCredentialEnv, now
	// gone) into the harness's profile (sign_in_env, which only claude-code's
	// names), so entryEnv, entrySignInName and entrySignsIn ask
	// CredentialEnvNames, which reads it, where they asked the runner's list.
	// For every profile the engine ships the names are the ones they were, in
	// their order, so what a run is handed and whether it signs in are as at
	// executorFinalRead, and the render's copy follows the profile instead.
	// A box of an entry that signs in with no login (api-key, env) is seeded
	// with none (SandboxCredentials), which the render's verdict never
	// counted on: such an entry signs a box in by a variable. And SandboxEnv
	// carries the engine's zone into every executor run's box, E2B included:
	// the zone it reads a reset the harness names without one in, the engine's
	// own TZ where it names a zone the system knows, and UTC otherwise
	// (childZone), its name harnessprofile.ZoneVariable, which an entry's env
	// may not set.
	envSignInRead = "v0.1.1-0.20261029083537-e0328e5d9daf"
	otelGo        = "internal/sandbox/otel.go"
	// topologyGo and assistantDeepGo are the render's pod sizes and the
	// engine's deep-work backend, which several rows name.
	topologyGo        = "internal/render/topology.go"
	assistantDeepGo   = "internal/engine/assistantdeep.go"
	localGo           = "internal/sandbox/local.go"
	localBoxGo        = "internal/sandbox/local_box.go"
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

	// codeHostRead is crewlet/crewlet#298's engine, the code host's, read at
	// the head of its branch, where every bound the windows were read from
	// that it moved was read again. The code host's log is an eighth state
	// log, so a data pod's admission reads eight maintenance windows (admit
	// reads one per registered domain, and the register grew). The join
	// reads every log's trim floor once, each by its key and all at once
	// (readFloors, readFloorsOf, (*FleetStore).Floor), where it listed the
	// fleet's floors once for the join and once more for each log
	// ((*FleetStore).Floors, gone, and trimFloor, which only a write's fence
	// reads now, so no boot waits on it). The object store's fleet record is
	// agreed in casRetry's attempts, bounded by their number, not by time,
	// where retryRaces (gone) asked again within the client's 5 s default. A
	// log's stats and its record at a sequence are read through ReadWith on
	// their caller's context, asked again while the broker answers none,
	// where each was one request at the client's default. What else moved
	// in the declarations read here moved no bound: the queue's
	// registrations name how many answers they run at once, the
	// provisioning reads hand their value back rather than set it from a
	// closure, the nats bucket's uploads share a window sized for the link
	// they cross, the S3 client counts into the node's metrics, and a log's
	// start words one refusal anew. A bound moved outside them: the change
	// that gave the registrations their answer counts also gave the estate
	// service a second lane, for a push's proposals, which a data node
	// registers beside the shared one, one more flush on its boot. That
	// registration is made below serveEstate, whose own code did not change,
	// so no declaration read here showed it, and the windows missed it until
	// gitLaneRead.
	codeHostRead = "v0.1.1-0.20261025200628-15c2f1a96275"

	// gitLaneRead is crewlet/crewlet#298's engine with the estate service's
	// git lane, on which a node with the git role answers the git reads of
	// its repositories, read where serveEstate was read again against the
	// windows. The estate service registers its lanes one after another,
	// each one flush, and every data node registers two, the shared lane and
	// the proposals', which the windows counted as one: the proposals' lane
	// came in on the branch below serveEstate, whose own code did not change,
	// so the service's Serve and its lanes are read now too, and the queue's
	// Serve and flush, which make each registration one flush. The git lanes
	// are registered only beside a git half, which serveEstate hands the
	// service only on a node with the git role, and no pod the operator runs
	// has that role. Every other registration the windows count was read
	// down to the queue there too: the steer answerer and the fleet broker
	// register in their step's own code (armSteer, serveFleetBroker), and
	// the history answerer, the inbox relay and the presence answerer each
	// register one subject, in a function one call below their step
	// (eventfan's Serve, (*inboxRelay).serve, presence's (*Hub).Start),
	// which are read now as well, so a registration added in any of them
	// moves a digest, as one added to the estate's lanes does.
	gitLaneRead = "v0.1.1-0.20261026025520-adba06740326"

	// hedgeCeilingRead is crewlet/crewlet#298's engine where a leader read is
	// asked again while nobody answers it and every read asked that way has
	// a ceiling, read where the bounds it moved were read again against the
	// windows. A leader read is asked as a log's reads were, each request
	// presumed lost after its term and the next sent beside it, and one on a
	// context with no deadline is held to the client's DefaultTimeout before
	// it is asked ((*Leader).get, ask, askTimed, NewLeader, ReadBoundedWith),
	// where get applied that bound itself: the windows' leader reads keep
	// their 5 s, as does the broker's limit read, which is asked the same
	// way now ((*Queue).budget). A read asked that way from the start, a
	// state log's state and its record at a sequence among them, ends at its
	// timing's Lookup where its caller set no deadline (ReadWith, with the
	// queue's timing: (*Queue).provisioning, Clustered.Timing), where it was
	// asked for as long as its caller's context lived: the windows count a
	// boot's reads of a state log at that ceiling now, where they counted
	// each at the term of one request for want of a bound. A certified
	// listing's key index is a listing of the bucket's subjects from its
	// leader, one page for a list as short as the nodes' positions
	// (keysUnder, (*Leader).Subjects, page, sorted), where it was two
	// requests, which takes 5 s off the join's listing of them. The nats
	// bucket's lookup is held to Lookup by ReadWith rather than by a context
	// of its own, its open hands the piece reads of a bucket across a link a
	// longer term, and a log's read of a record across a link is given its
	// crossing beside the read term, none of which moves a bound.
	hedgeCeilingRead = "v0.1.1-0.20261026010533-a5bf068300dd"

	// clockSkewRead is crewlet/crewlet#298's engine that measures each node's
	// clock against its broker's (clock_skew), read where the fleet's
	// positions listing was read again against the windows. Each row the
	// listing answers now carries the instant the broker stored it, which the
	// walk of the register's node class hands on from the entry it holds
	// already ((*FleetStore).eachPositionKey) and Positions sets on the row:
	// the listing asks nothing it did not, so the join's listing of the
	// positions keeps its 20 s. That walk was read by no row, though, and nor
	// was the way from it to the certified listing it is, a fleet bucket's
	// filtered walk with the leader reader it addresses
	// ((*FleetStore).eachUnder, eachEntryUnder, newLeaderReader), or the
	// steps of the pass between listUnder and the functions that bound them
	// ((*pass).walk, stop, certify and close), or the read back of a key the
	// pass did not deliver live ((*leaderReader).last, (*Leader).Last). A
	// request added in any of them would have moved no digest, so they are
	// read here too, and each makes no more requests than the windows count
	// for a listing. No row read the way the other reads of one key the
	// windows count reach that same leader read either (a log's trim floor
	// and its maintenance window, the object store's fleet record and the
	// rounds of its create): the store's read of a key's live value
	// ((*FleetStore).get, getLatest, (*leaderReader).live) and its create
	// ((*FleetStore).create). Nor did any row read what bounds the write each
	// of those rounds makes before its read: a settled update, which waits
	// out the leader's answer that another write to the key is still in
	// flight, no longer than the client's DefaultTimeout where its caller set
	// no deadline (settled, settledBucket.Update, settledBucket.settle and
	// jsinflight's Decide). Each read is one leader read of the key, and each
	// round's write one settled update, at the client's 5 s, and they are
	// read here as well.
	clockSkewRead = "v0.1.1-0.20261026011712-cbc031701107"

	// brokerAuthRead is crewlet/crewlet#298's engine whose embedded broker
	// admits only the fleet's keys (crewlet/crewlet@a7a499203), read at the
	// head of its branch, where a node's connections to its own broker and a
	// leaf's wait for the fleet were read again against the windows. Every
	// connection the engine opens to its own broker is made in process, a
	// clustered member's too (crewlet/crewlet@acde63711), and presents a key
	// the process mints at each start ((*embeddedServer).connect). A data
	// pod is a lone member and a seat pod a leaf, whose connections were in
	// process already: each is still a handshake bounded by listenTimeout
	// and a flush, so a pod's boot counts each connection at the 60 s it
	// did. A leaf asks for the fleet's JetStream as it did, until
	// clusterReadyTimeout; only its refusal moved, which now names the key
	// the leaf presents, because a member that refuses that key looks from
	// the leaf like one that is not there (awaitLeafReady). So a seat pod
	// that every data pod refuses exits after the minute a seat pod that
	// reaches none does, and no window moves. The decision that moved is
	// who may link: the leaf listener admits a link only on a key derived
	// from the company's keyring, which the render writes whole into every
	// pod, data and seat alike, so no render changes. What it changes is
	// the operator's text on what guards the leaf listener. An edit of the
	// keyring that adds a key and makes it active strands no seat pod, since
	// the seat pods take a new keyring only once every data pod is ready on
	// it (writeSeatPool); one that removes the key they present has them
	// refused between the two rolls, as the credentials guide says.
	brokerAuthRead = "v0.1.1-0.20261030230815-ab6d97e67b58"

	// bootStanceRead is crewlet/crewlet#298's engine at the pushed head of
	// its branch, where every row of this table was compared again: 195 of
	// the 198 hold there, three of them boot rows whose digests had moved
	// since they were read, recorded at this version. The other three,
	// (*Engine).bootSecrets, (*Engine).join and (*Engine).startStateLog, stay
	// at bootRecoveryRead, crewlet#311 on crewlet's main, which #298's branch
	// has not merged: at its head bootSecrets is not declared, and join and
	// startStateLog are the branch's own, so the three rows fail there. They
	// hold the operator to the engine #298 lands as, which carries #311, and
	// are read against bootWindows at the head of the branch once it has
	// taken main in, as is every other row, this version's included. None of
	// the three recorded here asks the broker anything it did not ask before,
	// so no window moves and their decision texts stand. A log's start hands
	// the reading of the log's end it took to the runner paired with where
	// the runner stands, its checkpoint and the barriers its loop reached
	// past it ((*statelog.Runner).Stance, read under the runner's own lock),
	// where it paired the reading with the checkpoint alone
	// (crewlet/crewlet@19aa26ea1): a value in memory, so the start still
	// reads its stats, attaches its consumer and verifies its checkpoint, as
	// (*stateLog).start's row says. The estate service gives the engine
	// writes a git node answers for its peers a bound of their own, a slot
	// each until it is answered (crewlet/crewlet@7468263a7), which estate's
	// Serve makes as a channel when it builds the one server every lane
	// answers through: its lanes are registered as they were, one after the
	// other through the queue's Serve, the two git lanes only beside a git
	// half, which no pod the operator runs is handed. And a log's state read
	// notes how many members store the log as the broker reports it
	// ((*DomainLog).stateInfo, crewlet/crewlet@09db07e30), which decides what
	// a barrier that did not commit is refused as, and is still one read
	// through ReadWith. The registered domains, which every loop of the boot
	// over them multiplies and no digest sees, are the eight the windows
	// count.
	bootStanceRead = "v0.1.1-0.20261101062317-46be603c9402"

	// tenLogsRead is crewlet/crewlet#298's engine at the pushed head of its
	// branch once the branch had taken crewlet's main in
	// (crewlet/crewlet@ec453fd43), where each of the 116 rows read against
	// bootWindows was compared again: 104 hold there, and the twelve that
	// had moved, two of them gone, are read again and recorded at this
	// version, two of bootStanceRead's three deferred rows among them (the
	// third, bootSecrets, holds). The registered domains are ten there:
	// main brought the chat log and Ask Crewlet's to the branch's eight. No
	// digest sees that, but two loops of the boot that the windows count read
	// once for each domain, the admission's read of each log's maintenance
	// window (admit, over maintenanceStreams) and the join's read of each
	// log's stats ((*stateLog).replayable), so a data pod's window counts
	// ten of each. None of the twelve asks the broker for more than its
	// decision text now says:
	//   - (*Engine).join and (*Engine).startStateLog are crewlet#311's, read
	//     at bootRecoveryRead, which the branch now carries: the join words
	//     the domains it failed on in a list (words.And), and the state log
	//     hands its logs a nudge of the seat-claim gate (e.nudgeAdmission),
	//     a signal on a channel. And (*stateLog).start hands each log's runner
	//     a hook for its first drain and one for when its loop comes level,
	//     both run by the loop, never by the boot (crewlet/crewlet@420a7b0fa,
	//     @9da72601f).
	//   - attachCoordination floors each seat's fencing epoch above the
	//     epochs its sandbox runs carry, one listing of the runs and a floor
	//     for each seat (floorSeatEpochs, crewlet/crewlet@e30315557), under
	//     the same sequence ceiling as the buckets, so the step keeps its
	//     window and its row says what the window holds.
	//   - The inbox relay is the live relay (crewlet/crewlet@da95cd841):
	//     armLiveRelay hands one relay every domain whose applier announces
	//     (liveDomains) and the check of which of them this node's copy holds
	//     out of service, and the relay hands each batch to its domain's
	//     listener (deliverLive). Neither is a request on the boot: it serves
	//     every domain on its node's one subject, registered once through
	//     (*liveRelay).serve, so a data pod's boot counts it as the one flush
	//     the inbox relay was, and a node without data registers none. The
	//     two rows name the relay's declarations in liverelay.go, and
	//     inboxLiftRead, at which armInboxRelay was read last, goes.
	//   - (*FleetStore).eachPositionKey hands its one class to
	//     eachPositionPrefix, which walks the positions register under a
	//     prefix of key segments, since an operator's acknowledgement is
	//     filed under its log as well (crewlet/crewlet@9aed87eaa). The walk
	//     moved there whole, still one filtered walk, so eachPositionPrefix is
	//     read now too, and the join's listing of the positions keeps its
	//     20 s.
	//   - startCore opens Ask Crewlet's reader and writer (openAssistant),
	//     which ask the broker nothing, and starts its erasure of a removed
	//     person past the boot (crewlet/crewlet@f2431f16c); armHistory builds
	//     no reader on a node without data, and a data node registers its
	//     answerer as before (crewlet/crewlet@d6b1ec595); armPresence hands
	//     the presence hub the node's objective meter
	//     (crewlet/crewlet@16fb72df3), and the hub registers its one subject
	//     under the name a chat room's presence shares
	//     (crewlet/crewlet@c434ea10e); and createKey tells a create the broker
	//     did not answer from one it refused (crewlet/crewlet@387ed35c9).
	tenLogsRead = "v0.1.1-0.20261102031859-ec453fd43cc8"

	// bridgeBaseRead is crewlet/crewlet#298's engine at the same head as
	// tenLogsRead, once this branch had taken this repository's main in
	// through #52, where every row outside the boot's windows was compared
	// again: each holds there as main read it but the tool bridge's Build.
	// The branch's boot reads every address a box dials back to the engine
	// through ParseBoxURL first (ReadBoxURLs, refusing to start on a value
	// that is set and unusable) and hands Build the base it read, where Build
	// read CREWLET_MCP_BRIDGE_URL itself and trimmed it; beside it Build
	// still takes the loopback address of the node's own listener, and builds
	// no bridge given neither. So a node's bridge is built from the same
	// values as main read it, and the render's address verdicts, which judge
	// every address the controller hands a pod by ParseBoxURL's grammar,
	// already hold a pod back on one the boot would refuse.
	bridgeBaseRead = "v0.1.1-0.20261102031859-ec453fd43cc8"

	// elevenLogsRead is crewlet/crewlet#298's engine once its branch had
	// taken Crewlet's main in up to #590, read at that merge as it landed
	// (crewlet/crewlet@c57e9a4146), once this branch had taken this
	// repository's main in through #73, where all 253 rows were compared
	// again: 247 hold there, and the six that moved, one of them renamed,
	// are read again and recorded at this version, three of them read once
	// more at landingRead. The registered domains
	// are eleven there, main's executor log the tenth and the code host's
	// the eleventh: no digest sees that, but a data pod's window counts
	// eleven of each loop tenLogsRead names. None of the six moves a verdict
	// the operator copies:
	//   - (*Engine).startStateLog checks the state logs' signer from the
	//     package the record seal moved to (recordseal.NewSigner, where it
	//     was statelog.NewSigner): the same check, at the same step.
	//   - newCodingRun is newExecutorRun, main's executor rename, its body
	//     unchanged but for the type it builds.
	//   - (*agentLauncher).launch, last read at crewlet's main 2f52194d02,
	//     where crewlet/crewlet#549 and #552 had the run hand its harness the
	//     skills its seat holds, names the run's model on its bridge session
	//     before the session opens, and hands the run the code holds of its
	//     turn (launchHolds), the checkouts a carried round keeps
	//     (carriedGrants) and the box's git (launchBoxGit): none of them on a
	//     node that serves no code host, and no pod the operator runs serves
	//     one. Its one new refusal is a pending run the store cannot read,
	//     which is no verdict on a document. The member it picks, the cell it
	//     places the run in, the bridge it refuses, the environment it starts
	//     the run with and the login it refuses off the engine host are as at
	//     executorFinalRead.
	//   - BuildOtelReceiver reads CREWLET_SANDBOX_OTEL_RECEIVER_URL through
	//     ParseBoxURL, refusing to boot on a value that is set and unusable,
	//     as the boot reads the bridge's base (bridgeBaseRead). The render
	//     already holds every address it hands a pod to that grammar
	//     (refusedAddress), and a node builds its receiver from the same
	//     values as before.
	//   - (*Coordinator).carryOnLost, last read at crewlet's main 11badf3141
	//     after crewlet/crewlet#525 (where a lost job that had asked is parked
	//     on the question its row holds, crewlet#447, and its record is
	//     published by publishCollected, crewlet#513), reads a lost job's
	//     pushes off the code host's record whether or not its box answered
	//     (readPushes), and carries them in the run's outcome and its report.
	//     A company the operator runs hosts no code, so that record holds no
	//     push of its jobs, and a lost job is carried on, never collected, as
	//     before.
	//   - boxHostRefusal's refusals name the box's harness where they named
	//     its coding agent; each states the rule it stated.
	elevenLogsRead = "v0.1.1-0.20261103033313-c57e9a41469b"

	// landingRead is crewlet/crewlet#298's engine as it lands: its branch's
	// last merge of Crewlet's main, up to #627 (crewlet/crewlet@e62c21e71c),
	// once this branch had taken this repository's main in through #76,
	// where all 266 rows were compared again. Every row holds there as one
	// side last read it but the three this branch and main had each read
	// again, which are read once more and recorded at this version:
	//   - (*agentLauncher).launch is main's, as boxDefaultsRead read it
	//     (crewlet/crewlet#612 and #627: an integration's box settings handed
	//     through runEnvironment to the run, and the picked member's login
	//     judged without them) and sessionContinueRead before it
	//     (crewlet/crewlet#581: the member picked first, the run placed in
	//     memberCell, the job before's session continued where its box can
	//     hold it), with this branch's beside it, as elevenLogsRead read it
	//     (the run's model on its session, launchHolds, carriedGrants and
	//     launchBoxGit, none of them on a node that serves no code host).
	//     The member it picks, the cell it places the run in, the bridge it
	//     refuses, the environment it starts the run with and the login it
	//     refuses off the engine host are as each of those reads says.
	//   - newExecutorRun is this branch's name for main's newCodingRun, and
	//     its body is main's at boxDefaultsRead but for the type it builds,
	//     so it holds as main read it there: the box settings lowest, none
	//     under one of the harness's sign-in names, and the run's sign-in
	//     judged without them.
	//   - (*Coordinator).carryOnLost is this branch's as elevenLogsRead read
	//     it: main's had moved since collectedRead by one word of a comment,
	//     which this branch's carries too. What main's reading of it added,
	//     the lost job's own session continued in the kept box, is decided by
	//     the relaunch (crewlet#581's continuesInItsBox and ContinuedCapture),
	//     not by carryOnLost, and holds here as main's copies say it.
	//
	// Two rows main read with the Cloudflare and Expo integrations, whose
	// two pull requests were made on sessionKeepRead and read before either
	// merged, are read again here, where both are merged, and recorded at
	// this version: seataccess's Lanes and SeatCredentialKinds. Each holds
	// as main read it, and its copies follow it: the nine lanes and the
	// kinds seat_access_migrated names, and the eight seat credential kinds.
	// The third, ExpoLevels, main read again at expoHandoffRead, once a
	// person set up each agent's Expo robot (crewlet/crewlet#610), and it
	// holds here as it does there: Expo's four levels, viewer when unset.
	//
	// And six rows are read for the first time here, for the executor
	// sessions Crewlet keeps (crewlet/crewlet#568 and #581), which wait on
	// an engine pod's scratch volume (sessionSpools): their bound
	// (SessionMaxBytes, SealedMaxBytes), where they are spooled
	// (spoolDirectory), and why a seat can hold several at once, an upload
	// run off its read (keepLater) that a relaunch does not wait out
	// (ContinuedCapture, relaunchCaptureWait). A digest of those that moves
	// is the engine bounding them otherwise, which the volume's docs follow.
	landingRead = "v0.1.1-0.20261103153337-e62c21e71c9c"

	// chatLogRead is crewlet's main at abd826b859, where crewlet#142 merged
	// Chatlet, the engine's own chat, as a ninth state log every data node
	// runs, and crewlet#478 merged per-seat integration access. The chat log
	// is one more in the register, so each step of the boot that reads the
	// logs one at a time reads one more, as with the Dashboard Assistant's
	// log before it: the join's listings of the trim floors, one more of them
	// and one more row in each, and one more log's stats, and the admission's
	// read of one more maintenance window. dataPodBootSeconds followed it,
	// counted from dataPodStateLogs, to 1766 seconds before the object store,
	// where eight logs had taken 1646. Every other boot step it moved reads
	// what it read: the inbox relay became the live relay, one registration
	// for every domain's announcements where it was one for the inbox's;
	// armHistory builds no reader on a node without data, which registered
	// nothing before either; armPresence times its moves; the join's refusal
	// names its logs in words; startStateLog and a log's start hand the
	// seat-claim gate a hook the apply loop calls; createKey marks a write
	// that went out unanswered; and a boot's handshake, which replaced the
	// call to admit, is admit alone in the normal mode every engine pod runs
	// in. A launch now asks the turn's fence again of every refusal it meets,
	// and classes a refusal of the configuration's, both after every decision
	// the render copies; a Confluence webhook carries its body's source and
	// author beside the actor it names as before; and seatCredentialFields
	// reads one per-kind table that also holds the values only the engine
	// reads, keeping the same fields a tool server may read.
	chatLogRead = "v0.1.1-0.20261030170958-abd826b8591e"

	// executorLogRead is crewlet#536's engine, read at its merge commit on
	// main (59b9bd3f04), whose data node runs a tenth
	// state log, the executor log, which names each seat's executor sessions.
	// startCore opens its two sides over the running log (openExecutor, which
	// builds a reader and a writer and reads nothing over the network), as it
	// has opened the Dashboard Assistant's since that eighth log joined
	// (openAssistant, which reads nothing over the network either, and whose
	// erasure of a removed person runs on a goroutine of its own, off the
	// boot). Each such log is one more in the register every data node runs,
	// so each of the boot's steps that reads the logs one at a time reads one
	// more: the join reads the fleet's trim floors once for the generations
	// and once for each log's floor, each listing reading back one row a log,
	// and each log's stats (statelog.go's replayable, trimFloor and
	// fleetGenerations), and the admission reads each log's maintenance
	// window (maintenance_mode.go's admit). The steps that read them together
	// or under one window (the ceilings, the streams, the consumers) do not
	// grow: ceilingsFor and readHeld, whose comments alone it changed, read as
	// they did, and no other mirrored declaration moved. dataPodBootSeconds
	// follows it, counted from dataPodStateLogs: 1896 seconds before the
	// object store, where nine logs took 1766, eight 1646 and seven 1536.
	executorLogRead = "v0.1.1-0.20261101193927-59b9bd3f041e"

	// The engine files most of the bounds on a boot are read from, and the
	// guide that quotes the windows they add up to.
	jsprovisionGo     = "internal/jsprovision/jsprovision.go"
	hedgeGo           = "internal/jsprovision/hedge.go"
	jsapiLeaderGo     = "internal/jsapi/leader.go"
	jsapiListingGo    = "internal/jsapi/listing.go"
	embeddedGo        = "internal/queue/jetstream/embedded.go"
	queueGo           = "internal/queue/jetstream/queue.go"
	domainLogGo       = "internal/queue/jetstream/domainlog.go"
	stateLogGo        = "internal/engine/statelog.go"
	backendsGo        = "internal/engine/backends.go"
	walkGo            = "internal/coord/kv/walk.go"
	positionsGo       = "internal/coord/kv/positions.go"
	fleetGo           = "internal/coord/kv/fleet.go"
	casLineGo         = "internal/coord/kv/casline.go"
	settleGo          = "internal/coord/kv/settle.go"
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

	// seatAccessRead is the engine of per-seat integration access, which
	// stores each seat's access at each vendor in the vendor's own terms
	// and refuses a level outside the vendor's fixed set.
	seatAccessRead = "v0.1.1-0.20261030031001-cb160d62f89c"
	// seatAccessMergedRead is the engine with every part of per-seat access
	// merged, which adds Deployments to a seat's GitHub App permissions and
	// the seat_access_migrated gate on each kind.
	seatAccessMergedRead = "v0.1.1-0.20261030052257-53bc8aa979f2"
	// entryRulesRead is crewlet#534's engine, read on crewlet's main where it
	// merged (1ecc48d5ca), which holds an MCP server's name, tool prefix, url
	// and value keys to what every client can use. sandboxMCP gives no box a
	// server whose url, once resolved, Crewlet would not dial: plain http to
	// a host on a public network, or a user name or password in it. The
	// render's copy holds as it was: the API refuses such a url, and any
	// ${NAME} in one, so a rendered server's url is the literal Crewlet
	// dials, and every box that scopes the server is given it.
	entryRulesRead = "v0.1.1-0.20261101172218-1ecc48d5ca08"
	// serverHealthRead is crewlet/crewlet#554's engine, read on crewlet's main
	// where it merged (2f52194d02), which keeps each MCP server's health.
	// startSharedServers now stamps when each shared server's outcome began
	// before it replaces the outcomes, so a server's page can say how long it
	// has been serving or down. Which servers it starts, and with which
	// values, is as it was. It also exports privateHost as MCPPrivateHost, the
	// one reading a server's page shares with MCPAddressProblem, which now
	// calls it by that name; neither rule moved, so the render's copies hold.
	serverHealthRead = "v0.1.1-0.20261102002330-2f52194d0262"
	// mcpAccessRead is crewlet/crewlet#586's engine, read on crewlet's main
	// where it merged (60028e53a8), where an MCP server reaches an agent
	// seat through one grant on its entry (ADR-0076). mcp_servers[].access
	// absent reaches every agent seat, and written reaches the seats it names,
	// a unit standing for every agent seat beneath it at any depth
	// (placeSeats); a server reading a vendor's per-seat credential reaches
	// the seats on at that integration's lane instead (markAccessLanes), and
	// refuses access (MCPServer.Reach). Grants is Reach on a server that is on,
	// so a per-seat server starts a child for every agent seat it reaches,
	// whether or not the seat holds an mcp_env block for it (seatSpecs, which
	// asks Grants, is unchanged), and sandboxMCP gives a box only a server its
	// seat is granted. The render follows it: markStarted starts a seat's
	// instance and box by reaches, and the API's access renders the grant.
	mcpAccessRead = "v0.1.1-0.20261102112847-60028e53a804"
	// mcpAccessGo is where the engine decides whom a tool server reaches.
	mcpAccessGo = "internal/config/mcpaccess.go"
	// inboxLaneRead is crewlet's main at 65fce4b904, read for
	// (*Queue).attach, which crewlet/crewlet#563 moved: an agent's inbox gains
	// a priority lane, a second consumer attach opens beside the topic's own
	// (queue.HasLane), and both are opened by openConsumer, the lookup and
	// read-back attach made inline before. The custody keeper's topic is no
	// inbox, so its attach still looks its one consumer up and reads it back,
	// and the data pod's boot window that counts it holds.
	inboxLaneRead = "v0.1.1-0.20261102131138-65fce4b904ff"
	// mcpOAuthRead is crewlet/crewlet#588's engine, read on crewlet's main
	// where it merged (e4e51c1213), which signs Crewlet in to a remote MCP
	// server with OAuth (mcp_servers[].oauth). A server signed in to with
	// OAuth is started with the bearer the engine holds for it, or held back
	// until somebody signs in (startSharedServers), its client secret is one
	// of the values it waits for (resolveServer), and sandboxMCP gives no box
	// such a server. The operator renders no oauth block yet, so no rendered
	// server is signed in to with OAuth, and the render's copies hold as they
	// were.
	mcpOAuthRead = "v0.1.1-0.20261102154049-e4e51c1213ee"
	// toolApprovalRead is crewlet/crewlet#591's engine, read on crewlet's main
	// where it merged (7b43d84822), which adds the approve level (a call to such a tool waits for a
	// person's answer) and starts a tool nothing sets at the level its hints
	// give it (allow for a tool its server marks read-only, approve for
	// every other). A server whose entry sets no default of allow may then
	// hold any tool back, so the engine refuses a sandbox listing of it and
	// sandboxMCP gives no box one. The render follows it: it writes Approve,
	// refuses such a listing on the Seat, and starts no value a box would
	// be given such a server.
	toolApprovalRead = "v0.1.1-0.20261102170345-7b43d84822ae"
	// serviceIdentityRead is crewlet/crewlet#589's engine, read on crewlet's
	// main at 7b43d84822, which signs Crewlet in to a server as its own
	// service identity (OAuth client credentials). startSharedServers starts
	// a server signed in to with OAuth under a token source that is either the
	// company's grant or that identity; the operator renders no oauth block,
	// so no rendered server is signed in to, and the render's copy holds.
	serviceIdentityRead = "v0.1.1-0.20261102170345-7b43d84822ae"
	// mcpLimitsRead is crewlet/crewlet#594's engine, read on crewlet's main
	// where it merged (bb2df11096), which lets a unit's lead narrow an MCP
	// server for their own teams (mcp_limits on a seat or a unit, a server
	// turned off with enabled: false): each agent seat is held to the
	// strictest of its own limits and every enclosing unit's
	// (resolveMCPLimits). Grants reads a limit that turns a server
	// off, so a per-seat server starts no child for such a seat, and
	// sandboxMCP gives a box no server a seat's limits hold a tool of back, a
	// listing the engine refuses (validateMCPLimits); MCPToolPolicy.validate's
	// tool key rule moved into toolKey, unchanged, which the limits share.
	// The render follows it: markStarted starts nothing of a server a limit
	// turns off for the seat and gives its box no server a limit holds back,
	// the API renders spec.mcpLimits, and the Seat's sandbox listing of such
	// a server is refused.
	mcpLimitsRead = "v0.1.1-0.20261102203033-bb2df1109698"
	// sessionKeepRead is crewlet/crewlet#568's engine, read on crewlet's main
	// where it merged (bf46fbdfdd): every executor launch's harness session
	// is kept, scrubbed of the credentials the launch handed its box. Three mirrors moved, each for that bookkeeping
	// alone, so the render's copies hold.
	//
	// runEnvironment now also collects, as the run's secrets, what each
	// layer's references resolved to and a setup file whose whole content is
	// one reference. The environment a run starts with is as at
	// executorFinalRead, where it was last read.
	//
	// sandboxMCP notes what each server's url's references resolved to. The
	// servers it gives, withholds and refuses are as at mcpLimitsRead, where
	// it was last read.
	//
	// (*agentLauncher).launch hands both to the launch's request, and tells a
	// stop decided on the run from the turn's own, noting a launch the turn's
	// stop ended after its run was recorded. It was last read at 2f52194d02
	// for crewlet#549 and #552, where the run came to hand its harness the
	// skills its seat holds (the bridge's list and the box's files); before
	// that at crewlet#528 (0b2730bb44), where the harness came to be named on
	// the wire and a sandbox's spec input with it (its CodingAgent field is
	// Harness); at harnessRead, which moved its names alone; and at
	// crewlet#453 (e0328e5d9d), where an executor launch came to stamp the
	// picked member's sign-in identity on the run's row (the launch request's
	// SignInID), which the run's collection compares to relaunch, once, a run
	// its vendor refused on a sign-in replaced while it ran. The member a
	// launch picks, the cell it places the run in, the bridge it refuses, the
	// environment it starts the run with and the login it refuses off the
	// engine host are as at executorFinalRead.
	sessionKeepRead = "v0.1.1-0.20261103022057-bf46fbdfddd4"
	// sessionContinueRead is crewlet/crewlet#581's engine, read on crewlet's
	// main where it merged (d8bf4ed905): a relaunch within a turn continues the
	// turn's kept session in whichever box it gets. (*agentLauncher).launch
	// moved for that alone. It picks its member first, before the seat's
	// headroom is checked, because the floor it is checked against is the
	// continued session's context and the brief that continues it, which only
	// a member of the harness that wrote the session takes up; it announces the
	// pick's hand-offs once that check passes; it hands the launch's request
	// the session it continues with its briefs; and it reads the box's word on
	// the session the box started off the run's first request to its bridge.
	// It takes the run's cell from memberCell, which holds the rule
	// agentPlacement's entry records — the member's own run_in, else the
	// catalogue's default_run_in — and which the cell a relaunch target names
	// reads too, so memberCell is mirrored beside it. The member it picks, the
	// cell it places the run in, the bridge it refuses, the environment it
	// starts the run with and the login it refuses off the engine host are as
	// at sessionKeepRead.
	sessionContinueRead = "v0.1.1-0.20261103062224-d8bf4ed90508"
	// cloudflareConfigureRead is crewlet/crewlet#608's engine, read on crewlet's
	// main where it merged (8a57e4eca8): Cloudflare's configure level.
	cloudflareConfigureRead = "v0.1.1-0.20261103084712-8a57e4eca88a"
	// expoHandoffRead is crewlet/crewlet#610's engine, read on crewlet's main
	// where it merged (d5caf82907): a person sets up each agent's Expo robot,
	// and the levels name the Expo role it must hold. ExpoLevels kept its digest.
	expoHandoffRead = "v0.1.1-0.20261103143303-d5caf82907bd"
	// boxDefaultsRead is crewlet/crewlet#627's engine, read on crewlet's main
	// where it merged (cc72b17f3b): an integration's box settings, #612's, are
	// returned apart from the run's own env, laid lowest of the whole box env
	// and never judged as a sign-in.
	boxDefaultsRead = "v0.1.1-0.20261103152428-cc72b17f3b2c"
	// orgLimitsGo is where the engine resolves what the limits on the way
	// down to a seat hold each server to, and mcpLimitsGo where it refuses
	// a limit, and a sandbox listing a limit holds back.
	orgLimitsGo = "internal/org/limits.go"
	mcpLimitsGo = "internal/config/mcplimits.go"
	// mcpRiskGo is where the engine reads a tool's hints as a risk, and the
	// level a tool nothing sets starts at.
	mcpRiskGo = "internal/mcp/risk.go"
	// roleAccessGo is where the engine states each vendor's fixed set of
	// what a seat may hold.
	roleAccessGo = "internal/config/roleaccess.go"
	// renderEnumsGo is the render's mapping of the API's values onto
	// Crewlet's.
	renderEnumsGo = "internal/render/enums.go"
	// commonTypesGo is the API's shared types and condition reasons.
	commonTypesGo = "api/v1alpha1/common_types.go"
)

// seatAccess is every operator file that copies a vendor's fixed set of what
// a seat may hold: the Seat API, which offers and refuses it, the render,
// which writes it in the vendor's identifiers, and the guide that lists it.
var seatAccess = []string{seatTypesGo, "internal/render/org.go", renderEnumsGo, integrationsMd}

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

// boxURLCopies is every operator file that copies which addresses handed to
// sandboxes Crewlet refuses to boot on: those of addressCopies, the render
// holding a data pod back on one, the API refusing one a Company states as
// its publicBaseURL or a class's webhooks host name template makes, and the
// controller's message naming the variable and where its address came from;
// the controller's choice of which variables a pod is handed, which the
// render judges (boxURLs and receiverURL); and enginetest's BoxURLVerdicts,
// which names the addresses the boot reads for the suites that ask it.
var boxURLCopies = append([]string{"internal/controller/company_workload.go", "internal/engine/enginetest/enginetest.go"},
	addressCopies...)

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
// platform provider APIs' over a harness's sign-in, and the tool-server API's
// refusal of a literal value), the render's copy of their pattern, and the
// guide and the page that state the rule of a literal value.
var credentialNames = []string{
	"api/v1alpha1/llmprovider_types.go", engineClassGo, mcpServerTypesGo, unitTypesGo,
	signinGo, "docs/guides/tool-servers.md", credentialsConceptMd,
}

// executorLogin is every operator file that copies how an executor run's harness
// signs in where it runs, and when Crewlet refuses it for signing in to
// nothing: the render's executor verdict and its sign-in rules, the
// LLMProvider API's docs and the guide that state it, and the CRD suite's
// reading of the engine's sign-in verdicts (its holdVerdicts).
var executorLogin = []string{
	signinGo, executorGo, "api/v1alpha1/llmprovider_types.go", "docs/guides/harnesses.md",
	"test/crd/harness_engine_test.go",
}

// executorBox is every operator page that states what an executor's box
// carries of the provider its run is on, its token, key or credential-named
// variables of its env and, off the engine pod, never its credential bundle:
// the credentials guide's "What a coding box carries" and the credentials
// concept page.
var executorBox = []string{"docs/guides/credentials.md", credentialsConceptMd}

// bridgeVerdict is every operator file that copies which engine pod's direct
// boxes reach the tool bridge on loopback, every one's, and which runs reach
// it only at the Company's public address, those in E2B and deep work: the
// render, which binds the pods' listeners and reports a Direct seat as
// runnable in any pool (ExecutorOf), the API's Ready reasons, and the pages
// and the record that state the verdict.
var bridgeVerdict = []string{
	bootstrapGo, executorGo, commonTypesGo, harnessesMd, troubleshootingMd,
	"docs/guides/topology.md", "docs/concepts/applying.md", runtimeMd, exposureMd,
	adr0008Md,
}

// directBox is every operator file that copies where a Direct box lives and
// what of the pod it is given: the render's local.state_dir and SandboxDir,
// the volume the controller mounts there on every engine pod, and the API
// docs and pages that say what a Direct run gets of the pod's environment.
var directBox = []string{
	renderCompanyGo, bootstrapGo, companyWorkloadGo, seatPoolsGo, companyTypesGo, engineClassGo,
	runtimeMd, harnessesMd,
}

// runTelemetry is every operator file that states where a seat's executor run
// exports its traces: in E2B to the receiver at the Company's public address,
// and Direct to its own pod's listener, on loopback, which every pod given
// the class's collector serves. The controller gives every engine pod that
// address and the collector, and lets the data pods alone reach the public
// route's proxies, the API docs of the address, of the route that carries it
// and of the class's telemetry say who calls it, and the pages and the
// security policy say so.
var runTelemetry = []string{
	companyWorkloadGo, companyExposureGo, companyTypesGo, engineClassGo, exposureMd, runtimeMd,
	"docs/concepts/overview.md", credentialsConceptMd, "SECURITY.md", "README.md",
	adr0008Md,
}

// boxWrites is every operator file that copies where a Direct run's writes
// land outside its box: the scratch volume the controller bounds on every
// engine pod, the defaults and the API docs of its limit and of the boxes',
// and the pages and the record that say what each limit bounds.
var boxWrites = []string{
	topologyGo, bootstrapGo, companyWorkloadGo, engineClassGo,
	runtimeMd, harnessesMd, "docs/guides/multi-tenancy.md", troubleshootingMd,
	adr0008Md,
}

// sessionSpools is every operator file that copies what the executor
// sessions Crewlet keeps take of an engine pod's scratch volume: where its
// sandbox coordinator spools them, how large each is at most, and that a seat
// can hold several at once, so that the volume is sized with room for them
// rather than held to a figure for one a seat.
var sessionSpools = []string{
	engineClassGo, topologyGo, companyWorkloadGo, runtimeMd, troubleshootingMd,
	"docs/guides/multi-tenancy.md",
}

// directRunEnds is every operator file that copies what becomes of a seat's
// Direct run whose pod stops: the run is lost with its host, never collected
// as its result, and Crewlet carries its round on in a fresh run, at most
// twice a round. A restart of the engine's container keeps its box on the
// sandboxes emptyDir, and the fresh run carries on in it (engine_restarted);
// a pod that is replaced comes back without the box, and the fresh run starts
// in a fresh one (box_gone); and a pod gone for the box timeout has its runs
// carried on by the seat's next holder (launch_node_gone). The render's
// rationale for the emptyDir, the API docs of the pods' grace periods, and the
// pages and the record that say so.
var directRunEnds = []string{
	bootstrapGo, companyTypesGo, engineClassGo, runtimeMd, harnessesMd, "docs/guides/topology.md",
	troubleshootingMd, adr0008Md,
}

// deepWorkBox is every operator file that copies where a deep-work run placed
// on a data pod keeps its confined box: on the pod's sandboxes volume, under
// the state_dir the render writes, which data.sandboxSizeLimit bounds. The
// render's rationale for the volume and its default limit, the API docs of
// deep work and of the limit, and the pages and the record that say so.
var deepWorkBox = []string{
	bootstrapGo, topologyGo, companyTypesGo, engineClassGo, harnessesMd, runtimeMd, adr0008Md,
}

// parkedBoxes is every operator file that copies how many boxes, and how many
// runs' processes, an engine pod holds: one running run for each agent seat,
// and beside it each run parked on a person's question, which frees its seat
// and keeps its box with its processes stopped until it resumes or its pause
// TTL expires. The render's rationale for the sandboxes volume's default
// limit, the API docs of that limit, of the pods' resources and of the turns
// a seat pod runs at once, and the pages that size a pod by them.
var parkedBoxes = []string{
	topologyGo, engineClassGo, companyTypesGo, runtimeMd, harnessesMd,
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
// the sign-in a provider's settings give it and the harnesses it admits a run_in
// on (test/crd's TestHarnessSignInFollowsTheEngine and
// TestHarnessRunInFollowsTheEngine), the deployment its validation reads a
// Jira or Confluence address on (TestContractAtlassianDeploymentIsTheEngines),
// and the lists TestExecutorSignInFollowsTheEngineSource,
// TestExecutorHarnessesFollowTheEngineSource,
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
// config reconciler reports an empty value by, is decided by eight more:
// startSharedServers, seatSpecs and Grants (which instances run, the company's
// and the seats'), Reach with the seat facts it reads, placeSeats (the units a
// seat sits in) and markAccessLanes (the integrations it is on at), which
// decide whom a server reaches, inheritMCPEnv (a unit's block under its
// members') and sandboxMCP (an executor's box), which gives no box a server
// not every tool of which runs at allow (MCPToolPolicy.AllowsEveryTool), a
// listing of which the engine refuses (validateBoxToolLevels) and the render
// refuses on the Seat; the levels themselves are a closed set (MCPToolLevels)
// keyed by a tool's own name (MCPToolPolicy.validate), which the API holds
// to, and a tool nothing sets starts at the level its hints give it
// (StartingLevel, reading them with RiskOf), which the API and the guide
// state. Whom a
// server reaches is held at the shapes it tries by
// TestContractAToolServerReachesWhomTheEngineGrants too. And
// validateHeaderSpellings, with
// spelledUnlike, refuses a header a seat spells unlike its unit, which the
// render refuses too (headerSpellings).
//
// Most of them are the launch's own code: the member it runs on (member,
// executorRunners), the cell it places the run in (agentPlacement,
// buildSandbox and its round cap, defaultSandboxMaxTurns), the address the
// box dials (launch, with the bridge's own Build, Reaches and the
// node's boxHostURL, reachedOnLoopback and loopbackURL), and the login it hands the box
// (newExecutorRun, entryEnv, entrySignInName, sandboxCredentials and OnEngineHost). Some are
// a sandbox backend's: what an E2B box is created from, (*E2BProvider).Create,
// since the launch hands a run's credential files to whatever box it is placed
// in and each backend decides what to do with them; where a direct box lives
// (NewLocal, DefaultBoxRoot, localOptions) and what of the pod's environment it
// is given (childEnv, PassthroughEnv). And the rest are deep work's, which no seat
// launches: where it runs (DeepWorkPlacement), where it keeps its box on the
// engine host (boxRootOf, hostCell, BoxRoot) and the address it dials
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
// So are the addresses handed to sandboxes after them, which the engine
// refuses to boot on by a grammar of its own (ParseBoxURL and its helpers),
// and no validation reads, since they are a pod's environment:
// TestContractBoxURLShapes holds the render's verdict to the one `crewlet llm
// doctor` reports at every shape it tries, and test/crd's
// TestAddressShapesFollowTheEngine the API's on a publicBaseURL. Which
// variables the boot reads and refuses on is ReadBoxURLs' and BoxURLs', whose
// call in engine.New is not read, for engine.New's reason below.
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
// does after /health answers 200 state. One more is a wait the engine bounds
// by no time, which the windows count as a boot meets it: the agreement of
// the object store's fleet record, bounded by a number of attempts
// (casRetry, fleetCASAttempts), counted at the one attempt a boot meets
// unless someone removes the record meanwhile. How long a read a step makes
// may wait is read down to the function that bounds it, for every read the
// engine asks again while nobody answers it (hedge): ReadBoundedWith holds
// one whose caller set no deadline to the bound it is handed, and every one
// to its timing's Lookup. A leader read is handed its client's
// DefaultTimeout ((*Leader).get, ask, askTimed, NewLeader), as are the pages
// of a pass's key index (keysUnder, (*Leader).Subjects, page, sorted) and
// the broker's limit read ((*Queue).budget); a read of a log's state or
// record is handed Lookup itself (ReadWith, with the queue's timing,
// (*Queue).provisioning and Clustered.Timing), which a boot's read waits
// out, its context having no deadline. Each read of one key the windows
// count, of the key's live value ((*FleetStore).get, getLatest,
// (*leaderReader).live), in a round of a create ((*FleetStore).create,
// createKey) or as a certified listing's read back, is one leader read of
// the key's newest message ((*leaderReader).last, (*Leader).Last). The write
// each round of that create makes before its read is read down to its bound
// as well: it is a settled update, which waits out the leader's answer that
// another write to the key is still in flight, no longer than the client's
// DefaultTimeout where its caller set no deadline (settled,
// settledBucket.Update, settledBucket.settle, Decide). The objects bucket it
// writes is settled where the fleet store opens it (OpenFleet), which is not
// read here, since Crewlet's
// TestAConditionalWriteIsAnsweredByWhatTheLeaderDecided requires that bucket
// to be settled. So a bound moved below a step's read or write, whose own
// code need not change for it, sends somebody to the windows. And
// how many registrations a step makes is read down to the queue, for every
// step that registers what a node answers: the steer answerer and the fleet
// broker register in their step's own code (armSteer, serveFleetBroker);
// the history answerer, the live relay and the presence answerer each
// register one subject in the function their step calls (eventfan's Serve,
// (*liveRelay).serve, presence's (*Hub).Start); the estate service
// registers each of its lanes in turn (estate's Serve and lanes); and every
// registration is one flush ((*Queue).Serve and flush). So a registration
// added below a step, whose own code need not change for it, sends somebody
// to the windows too. And the listing the windows count, the join's of the
// fleet's positions, is read down to the certified listing it is, and that
// listing to the functions that bound its waits: a class of the positions
// register is one walk of the register ((*FleetStore).eachPositionKey and
// the walk under a prefix it hands the class to, eachPositionPrefix), a
// filtered walk of a fleet bucket one certified listing, its leader reader
// addressed without a request ((*FleetStore).eachUnder, eachEntryUnder,
// newLeaderReader), and a certified listing a pass begun beside its key
// index, walked to its end, stopped, certified and closed (listUnder,
// beginPass, (*pass).walk, passOver, passIdle, (*pass).stop,
// (*pass).certify, certify, (*pass).close), whose certifying read of a key
// is one leader read ((*leaderReader).last, (*Leader).Last). So a request
// added below that listing, whose own code need not change for it, sends
// somebody to the windows as well.
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
			"alone, with its default_run_in as the cell of a run whose entry names none, its default_max_turns as " +
			"every run's round cap, uncapped where it names none, its commit_attribution, on unless false, " +
			"as whether every run's own " +
			"commits credit Crewlet, and its setup steps as the setup every box is given; a company with no " +
			"sandbox has none, and runs no executor",
		digest: "17378c552d7b3434d9b28e7e770c1fcf674ef2e2472911702bdde4452e922874",
		read:   commitAttributionRead,
	},
	{
		file: "internal/config/sandbox.go", name: "(*SandboxProvider).MaxTurns",
		against: []string{companyTypesGo, seatTypesGo, harnessesMd},
		decides: "an executor run on a seat and a catalogue that name no round cap is uncapped: default_max_turns " +
			"left out or 0 caps nothing",
		digest: "5f4b921bb6655891c825a9fe835ca28284286970a5bce857c353e4846b42c601",
		read:   uncappedRead,
	},
	{
		file: sandboxGo, name: "newExecutorRun",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a run on the member a launch picked is handed that member's model as its runs in the engine's pod " +
			"pass it, its environment the seat's over the entry's (entryEnv, judged on the seat's alone) over an " +
			"integration's box settings, lowest, none of which under one of the harness's CredentialEnvNames " +
			"reaches the box, its sign-in judged on the seat's over the entry's alone (run.signIn), and its login, " +
			"the harness's credential files or the document OpenCode 2's export printed, as the run's credential " +
			"files, for the box to seed or to ignore ((*E2BProvider).Create)",
		digest: "aa8b4c4ae4e359c654ea69b78af47591f22c34a99656cae7de89e07e134e3d05",
		read:   landingRead,
	},
	{
		file: e2bGo, name: "(*E2BProvider).Create",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a box in the Company's E2B sandbox is created from its template, the run's timeout and the run's " +
			"environment alone, never from the run's credential files, so the member's login (its credential " +
			"files, or the document OpenCode 2's export printed) never reaches it, and only a variable of the " +
			"run's environment signs its harness in there",
		digest: "65305f3ecc87afac5544b7bddb91062389e5a0e0b76a63481f6f191e778e2a03",
		read:   e2bCreateRead,
	},
	{
		file: sandboxGo, name: "entryEnv",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a run is handed its member's environment (SandboxEnv) beneath the seat's, and a run of a harness " +
			"whose profile names sign_in_env (ClaudeCode's alone) whose seat's environment signs it in " +
			"(CredentialEnvNames: its token_env, api_key_env and sign_in_env, which " +
			"TestExecutorSignInFollowsTheEngineSource holds) none of its member's sign-in (entrySignInName) and " +
			"the rest of that environment",
		digest: "02d4e1e61f45a3c81cef18dd6687bc299718736540e279b85ac1e47756d2f996",
		read:   harnessRead,
	},
	{
		file: sandboxGo, name: "entrySignInName",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a member's sign-in, which a seat that signs ClaudeCode in itself keeps out of its run, is every " +
			"variable that signs ClaudeCode in, the harness's token and key variables and every credential-named " +
			"one, a gateway's bearer token among them, and nothing else, so a gateway's address stays",
		digest: "9e6175afb6ff10da515464ff7747aa3e25e0f2b142ef562915c56cf117b22009",
		read:   harnessRead,
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
		against: append([]string{commonTypesGo, troubleshootingMd}, executorLogin...),
		decides: "the login of the member a launch picked is refused only in a box off the engine host (OnEngineHost: " +
			"a Direct box is seeded with it), and there unless the run's environment signs its harness in " +
			"(entrySignsIn): SandboxLoginNotPortable, a refusal that fails the launch rather than moving it down " +
			"the chain",
		digest: "363fcae93a8502774c80b5680f1b188e3245c95edbdfb7b79b367360ba26fe3c",
		read:   harnessRead,
	},
	{
		file: sandboxGo, name: "entrySignsIn",
		against: append(append([]string{harnessesMd}, executorLogin...), executorBox...),
		decides: "a run's environment signs its harness in, in a box off the engine host, when it holds the harness's " +
			"token or key variable, for ClaudeCode any variable that signs it in, and for an env sign-in harness any " +
			"credential-named one: the rule a seat's E2B run and the assistant's deep work, whose environment " +
			"is its provider's alone, are both held to before a box exists",
		digest: "7b9d56ad932882d328442e62b56c7100365144eb3bbc7f6c7d0120432e760d09",
		read:   harnessRead,
	},
	{
		file: sandboxGo, name: "(*Engine).runEnvironment",
		against: append([]string{
			signinGo, renderCompanyGo, renderOptionalGo, configOptionalGo, seatTypesGo, companyTypesGo, harnessesMd,
		}, executorBox...),
		decides: "a run's environment is the agent's identity, then the env of the catalogue's and the seat's " +
			"setup steps, then the seat's role.sandbox.env, each later layer winning a name, and a value that " +
			"resolves to nothing is left out: quietly where the optional_keys beside the value that wins lists " +
			"its key (a setup file's path likewise), as a fact the launch notes where the identity supplied it, " +
			"and as the run's gap otherwise, which launch logs and starts the run without; an integration's box " +
			"settings for a lane the seat has on are returned apart, each dropped where a layer above sets its " +
			"name and a fact where it resolves to nothing",
		digest: "59091985b7a10e62568c9a86cb46b0b3b7d2f3860563e1490f725fa94059a661",
		read:   boxDefaultsRead,
	},
	{
		file: toolCredentialsGo, name: "resolveServer",
		against: []string{renderOptionalGo, configOptionalGo, mcpServerTypesGo, toolServersMd},
		decides: "a tool server starts with its env (stdio) or headers (http), a seat's mcp_env for it over them key " +
			"by key, merged by the key the server reads (a header in any case) before anything resolves, so a seat's " +
			"key replaces the server's whole; a value that resolves to nothing, or to an empty value, is left out " +
			"where the server's optional_keys lists its key, whichever layer wrote it, and holds the server back " +
			"otherwise; and a url that resolves to nothing holds it back whatever optional_keys says (and, for a " +
			"server signed in to with OAuth, which the operator does not render, its client secret too)",
		digest: "4bbbc9ef6bbe9d2f83a692b07b352845e65dd8bb5dba034141922c77851fce31",
		read:   mcpOAuthRead,
	},
	{
		file: toolCredentialsGo, name: "(*refGap).hold",
		against: []string{renderCompanyGo, signinGo, mcpServerTypesGo, seatTypesGo},
		decides: "a value a provisioning pass mints or the engine issues for the seat holds a tool server back, and " +
			"is logged as awaited where it is missing from an executor run, whatever optional_keys says, and a " +
			"declared key's value that nothing mints or issues is left out",
		digest: "1aa6cffd1765e774ef6b019ed89ebfefcd385c60a585307f017e631d33e369bd",
		read:   seatAppTokenRead,
	},
	{
		file: engineMCPGo, name: "(*Engine).startSharedServers",
		against: []string{renderOptionalGo, configOptionalGo, toolServersMd},
		decides: "a shared server is started once for the company, with its own values alone (no seat's block), " +
			"unless it is turned off, which starts nothing (or signed in to with OAuth, by a person or as its " +
			"service identity, and not yet able to get a token, which the operator does not render)",
		digest: "c00b0a0520700b1f8b08d7187e323417c24d0bc74d9a91330eb40b9629be278c",
		read:   serviceIdentityRead,
	},
	{
		file: engineMCPGo, name: "seatSpecs",
		against: []string{renderOptionalGo, configOptionalGo, toolServersMd},
		decides: "a seat's instance is started of every per-seat server it is granted (Grants), whether or not the " +
			"seat holds a block of values for it, with the seat's block for it where it holds one",
		digest: "d8e12f522b6ea44fc231bb81b07cca8f43900333742901aa433d2713e1cf65f6",
		read:   seatAppTokenRead,
	},
	{
		file: mcpConfigGo, name: "(*MCPServer).Grants",
		against: []string{renderOptionalGo, configOptionalGo},
		decides: "a server is granted to the agent seats its grant reaches (Reach) while it is on, and to none once " +
			"it is turned off, nor to a seat a limit of its own or of a unit above it turns the server off for; a " +
			"seat's block of values for it plays no part",
		digest: "099af6924ade74dae8ff6475c6a1144503cc737bf397153b96f378093fb8590a",
		read:   mcpLimitsRead,
	},
	{
		file: mcpAccessGo, name: "(*MCPServer).Reach",
		against: []string{renderOptionalGo, configOptionalGo, mcpServerTypesGo, toolServersMd},
		decides: "a server reading a vendor's per-seat credential reaches the agent seats on at a lane of that " +
			"integration, and any other server every agent seat when it has no access, else a seat access.seats " +
			"names, else none access.except names, else one in or beneath a unit access.units names; seats wins " +
			"over except, and a person's seat is reached by nothing",
		digest: "9f3dedc242744c73900474381223ea831e139004e724c0e9436c7995e5b2a5a3",
		read:   mcpAccessRead,
	},
	{
		file: orgGo, name: "(*Organization).placeSeats",
		against: []string{renderOptionalGo, mcpServerTypesGo},
		decides: "a seat sits in its own unit and in every unit enclosing it, each by its key, the chain a grant to " +
			"a unit is matched against",
		digest: "5eb18739035ebf9838d2fc3009bd01d0ea02384fbcf296c1686da494e35f3bb7",
		read:   mcpAccessRead,
	},
	{
		file: mcpAccessGo, name: "(*Company).markAccessLanes",
		against: []string{renderOptionalGo, mcpServerTypesGo},
		decides: "an agent seat's lanes are the integrations it is on at (roles[].integrations.<lane>.enabled true), " +
			"which a server reading a vendor's per-seat credential is granted by",
		digest: "d204f143399fa25cdf09eabec42dd4b472dd87ace95dd07f20bbb1b6fecaea62",
		read:   mcpAccessRead,
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
		against: []string{renderOptionalGo, configOptionalGo, renderOrgGo},
		decides: "an executor's box is given every server its seat's sandbox scopes and the seat is granted, shared " +
			"or not, with the seat's block for it, none turned off (on its entry or by a limit), none at an address " +
			"Crewlet would not dial, and none that holds a tool back from the agents or a tool the seat's limits " +
			"hold back, nor one signed in to with OAuth, a listing the render refuses on the Seat",
		digest: "c494735a15b33fcabd1eed304c104dca323f53e17ff086dd4e93f201d4f6bd66",
		read:   sessionKeepRead,
	},
	{
		file: orgLimitsGo, name: "(*Organization).resolveMCPLimits",
		against: []string{renderOptionalGo, unitTypesGo, seatTypesGo, toolServersMd},
		decides: "an agent seat is held to its own mcp_limits and to every enclosing unit's, outward to the top, " +
			"and a person's seat to none",
		digest: "a33b83de86828dca3e3ecff1be462831295c1a1d6ca623cd219b14e65b66b833",
		read:   mcpLimitsRead,
	},
	{
		file: orgLimitsGo, name: "narrowed",
		against: []string{renderOptionalGo, unitTypesGo, toolServersMd},
		decides: "the strictest limit wins: a server any block turns off is off for the seat, and a tool any block " +
			"names is held back",
		digest: "9030e6da81e0392999a793049c0b2d674598a72abe884ee166918159bcf12cee",
		read:   mcpLimitsRead,
	},
	{
		file: mcpLimitsGo, name: "(*Company).validateMCPLimits",
		against: []string{renderOrgGo, unitTypesGo, toolServersMd},
		decides: "a limit holding nothing back, turning a server on, or holding a tool at a level other than " +
			"approve or blocked is refused, and so is a sandbox listing of a server a tool of which the seat's " +
			"limits hold back",
		digest: "6939956ff21765bd9d625c4d043bf14348ca6a553ef512cf03a7cfdc9123149e",
		read:   mcpLimitsRead,
	},
	{
		file: mcpToolsGo, name: "MCPToolLevels",
		against: []string{mcpServerTypesGo, renderEnumsGo, toolServersMd},
		decides: "a tool of a server runs at allow, approve or blocked",
		digest:  "92c79f9ba3098ddc56ea61ffc87f3756a5574123492b1565352b087ab10349f0",
		read:    toolApprovalRead,
	},
	{
		file: mcpToolsGo, name: "MCPToolPolicy.validate",
		against: []string{mcpServerTypesGo, toolServersMd},
		decides: "a tool's level is keyed by a name that is not empty and holds no space and no control character",
		digest:  "4d58be618fca743acee5ba382a443ad46848a6b0275857b20023bd5b83a68f39",
		read:    mcpLimitsRead,
	},
	{
		file: mcpToolsGo, name: "MCPToolPolicy.AllowsEveryTool",
		against: []string{renderCompanyGo, renderOptionalGo},
		decides: "a server runs every tool at allow only when its default is allow and every level it sets is " +
			"allow, an unset default leaving each tool at the level its hints give it",
		digest: "62135469f009d5fd8ddf19e2dcd58cfa7cd2a5e1516452e41d1069f76aabd2cc",
		read:   toolApprovalRead,
	},
	{
		file: mcpToolsGo, name: "(*Company).validateBoxToolLevels",
		against: []string{renderOrgGo, toolServersMd},
		decides: "a sandbox listing of a server not every tool of which runs at allow is refused at the listing",
		digest:  "b52e95de3ac718e384e8e67f9658cf60ec177ec5b92bf7fcfc7623bcb2d935e1",
		read:    toolApprovalRead,
	},
	{
		file: mcpRiskGo, name: "StartingLevel",
		against: []string{mcpServerTypesGo, renderCompanyGo, toolServersMd},
		decides: "a tool nothing sets runs at allow when its hints read read-only, and at approve otherwise",
		digest:  "68e92649740c227c8c724238ca1ef8bda47377a816d629e98f8c1eefb2288479",
		read:    toolApprovalRead,
	},
	{
		file: mcpRiskGo, name: "RiskOf",
		against: []string{mcpServerTypesGo, toolServersMd},
		decides: "a tool reads as read-only only when it asserts so, and a tool whose hints say nothing as " +
			"destructive",
		digest: "c43d6964244fe179032a1557eb26272bd195d789336dce2b245fcd1861436d79",
		read:   toolApprovalRead,
	},
	{
		file: mcpNamesGo, name: "mcpServerName",
		against: []string{mcpServerTypesGo},
		decides: "a tool server's name is lowercase letters, digits and hyphens, starting and ending with a letter " +
			"or a digit",
		digest: "ba9e38f2e23fe0e81dba24db66dab87fc899b80a5d53afc0980e723e8afa6943",
		read:   entryRulesRead,
	},
	{
		file: mcpNamesGo, name: "MaxMCPServerName",
		against: []string{mcpServerTypesGo},
		decides: "a tool server's name is at most 32 characters",
		digest:  "46cb2d0fc055c6520bbeaba7c4076e98e0dab8cde92a8d5c0e7b8003ca355269",
		read:    entryRulesRead,
	},
	{
		file: mcpNamesGo, name: "(*MCPServer).validateToolPrefix",
		against: []string{mcpServerTypesGo},
		decides: "a tool prefix is letters, digits, _ and -, and leaves room for a name in the longest tool name " +
			"(its bound read from the engine by TestContractMCPServerEntryRulesAreTheEngines)",
		digest: "4cc7f7426386159a15e40feb1ff664cc5faed301be4d9af86c7728b51d569d46",
		read:   entryRulesRead,
	},
	{
		file: mcpNamesGo, name: "(*MCPServer).keyProblem",
		against: []string{renderOptionalGo, mcpServerTypesGo, unitTypesGo},
		decides: "an HTTP server's values are named as headers and a Stdio server's as variables, its own, a " +
			"seat's and a unit's alike",
		digest: "157aa379a0b42d2fe72a766b9703fd0bbb37f79b8803ac65048170363072cf17",
		read:   entryRulesRead,
	},
	{
		file: mcpNamesGo, name: "headerName",
		against: []string{mcpServerTypesGo, unitTypesGo},
		decides: "a header's name is RFC 9110's token, which the API holds every value's name to less the backtick",
		digest:  "2ddfda23f093e956c8a0c57846950be055869ebee6ebeef9a7a79ef8f851b574",
		read:    entryRulesRead,
	},
	{
		file: mcpNamesGo, name: "MCPAddressProblem",
		against: []string{mcpServerTypesGo},
		decides: "a tool server's url is https, or plain http to a host on no public network, with no user in it",
		digest:  "1da77eba150454d25a97504c9a7c4d6f6f8e90eab98016065af51e3a8fab13e7",
		read:    serverHealthRead,
	},
	{
		file: mcpNamesGo, name: "MCPPrivateHost",
		against: []string{mcpServerTypesGo},
		decides: "a host on no public network is an address httpx.Public does not route, a one-label name, " +
			"localhost, or a name under .localhost, .local, .home.arpa or .internal; the API accepts the loopback, " +
			"private, link-local and shared (RFC 6598) ones of those addresses and refuses the rest",
		digest: "1495bb79fe2b2de1183690c3ac04945d0e38b4f48e6f51cdc72fb3b0e374a978",
		read:   serverHealthRead,
	},
	{
		file: mcpConfigGo, name: "breaksALine",
		against: []string{mcpServerTypesGo},
		decides: "a description is one line: no control character, line separator or paragraph separator",
		digest:  "cb440c2ff26ea87b89269c7b122dbbc73cc6afeb0d4969fd0546140ca2d4ece4",
		read:    entryRulesRead,
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
		decides: "the variables that sign a harness in inside a box: its token and key variables, the names its profile " +
			"signs it in by from env (sign_in_env: ClaudeCode's gateway token and cloud switches), and for an env " +
			"sign-in harness the credential-named variables of its env",
		digest: "32735810b5842041d5533d6193b7c1eebae75691e7b78dd945748d4afadf9e9b",
		read:   harnessRead,
	},
	{
		file: doctorGo, name: "(*Provider).SandboxCredentials",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a Direct box is seeded with its provider's login, its credential files, only where the provider " +
			"signs in with one: an APIKey or Env provider's box, and one handed a key, is seeded with none, as its " +
			"key or its env signs the run in",
		digest: "dc8db6274961434364726c08ce591b7fb73d2dc78bbebc02a21603107b34f084",
		read:   envSignInRead,
	},
	{
		file: doctorGo, name: "(*Provider).SandboxCredentialDocument",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "a Direct box of an OpenCode provider is seeded with the document its export printed only where " +
			"the provider signs in with a login, as SandboxCredentials seeds credential files",
		digest: "43e00f3286f884c3623679e523446928c51068b5754f904cdd5c697bed69a689",
		read:   envSignInRead,
	},
	{
		file: "internal/providers/llm/harness/signin.go", name: "(*Provider).signsInWithKey",
		against: append(slices.Clone(executorLogin), executorBox...),
		decides: "an entry signs in with no login under auth mode api-key and env, whatever else it holds, which " +
			"SandboxCredentials and SandboxCredentialDocument seed its Direct box with none of",
		digest: "1ce030093a2da737623dc5ce2342eb217bba9996902258afd8d71c250737d63d",
		read:   envSignInRead,
	},
	{
		file: doctorGo, name: "(*Provider).SandboxEnv",
		against: append(append([]string{engineClassGo, "docs/guides/multi-tenancy.md"}, executorLogin...), executorBox...),
		decides: "a provider's whole env follows an executor run into its box, Direct or E2B, with the sign-in its " +
			"auth mode sets, less what the mode removes, the isolation variables and blank values: a gateway's " +
			"address and bearer token, a cloud switch and its region reach every executor run, and a ClaudeCode " +
			"sign-in there signs an E2B run in (signin.go's remoteSignIn); and the engine's zone, TZ, reaches every " +
			"run, E2B included",
		digest: "6d6ac1557077e802661fd342638f344f12994505872e3fc439e30c1299150562",
		read:   harnessRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).LaunchExecutor",
		against: append([]string{renderCompanyGo, configOptionalGo}, executorLogin...),
		decides: "every executor launch past the turn's fence is launch's, whose refusal is returned as it was " +
			"unless the seat moved meanwhile",
		digest: "b0a3beef9e317cb42c85290aefb40f2cc5c19b5ad7153576c675aea864220788",
		read:   chatLogRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).launch",
		against: append([]string{renderCompanyGo, configOptionalGo}, executorLogin...),
		decides: "a launch, on a node with a sandbox runtime and a bridge something reaches, " +
			"picks its member (member), places the run in the member's cell (memberCell: agentPlacement, else the " +
			"catalogue's default), refuses a cell whose box cannot dial the bridge (Reaches: a Direct box dials " +
			"its node's own " +
			"listener, which every engine pod serves its own boxes, every other the fleet's address; NotExposed), " +
			"starts the run without a value of its environment that resolves to nothing, and refuses the picked " +
			"member's login only off the engine host (sandboxCredentials: SandboxLoginNotPortable), judged on the " +
			"run's environment without an integration's box settings",
		digest: "e7a38968ef5e982adbdbdb57dddb9f04c43d45ebfa5aa342239368325e71bde3",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/launch.go", name: "withTelemetry",
		against: runTelemetry,
		decides: "every executor launch is given the run's OTel environment, an endpoint with a token signed for " +
			"the run under the receiver address its cell reaches: a Direct box its own node's listener, on " +
			"loopback, where the node has one, an E2B box the address the node was given " +
			"(CREWLET_SANDBOX_OTEL_RECEIVER_URL), the Company's public address",
		digest: "5d15ea5b0c97e36290b2d93b9dd98f63caeb27859e46a500a7db255895b1bf3b",
		read:   executorFinalRead,
	},
	{
		file: otelGo, name: "BuildOtelReceiver",
		against: runTelemetry,
		decides: "a node builds the receiver from the address its boot read from CREWLET_SANDBOX_OTEL_RECEIVER_URL " +
			"through ParseBoxURL, refusing to boot on one that is set and unusable, and with its own listener's " +
			"loopback address only where it forwards to a collector (OTEL_EXPORTER_OTLP_ENDPOINT), which the " +
			"operator sets on every engine pod with the receiver's address, so every pod that hands a box an " +
			"endpoint hands a Direct box its own listener, and builds none with neither address",
		digest: "d4eef0c7cc35890e793efe576286abe5777c425ac0dc6be9c643761335e13b00",
		read:   elevenLogsRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).member",
		against: []string{executorGo, headsGo, seatTypesGo, llmProviderTypes, harnessesMd},
		decides: "a launch runs on the first member of the seat's executor chain with an in-box runner that is not " +
			"sitting out its spent plan on the node, passing over every other harness silently, and when every runner " +
			"member is sitting out runs on none",
		digest: "69c971a1269d6e4a9ec03db3d897092b1fe408cb102468f51220286b6ebf0573",
		read:   harnessRead,
	},
	{
		file: agentRunGo, name: "executorRunners",
		against: []string{executorGo, headsGo, seatTypesGo, llmProviderTypes, harnessesMd},
		decides: "the members a launch picks among are the executor chain's members, in order, whose harness has an " +
			"in-box runner (executor.Names, held by TestExecutorHarnessesFollowTheEngineSource), and a chain with none " +
			"launches nothing",
		digest: "db7701f2022f0956af0cb4f6f5b1a10d7d99f938c48e7485b15c29d103dd09fe",
		read:   harnessRead,
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
		file: agentRunGo, name: "memberCell",
		against: []string{executorGo, renderCompanyGo, llmProviderTypes},
		decides: "the cell a launch places its run in, and the cell a relaunch target names, is the " +
			"member's own run_in, and with none the catalogue's default_run_in (agentPlacement, else the " +
			"manager's default)",
		digest: "1af1b40591da54cf7c6a4e709dab5dfdd06d82a2a95c1badfc52a411aa0de849",
		read:   sessionContinueRead,
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
		decides: "a direct box is made under its box root (BoxRoot), made absolute, so the state_dir the render " +
			"writes puts every box on the pod's sandboxes volume",
		digest: "d6cb3ebf814b985796d8bad68856e584021fcd855a1aa2c37aa54275b08780e7",
		read:   harnessRead,
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
		file: localBoxGo, name: "(*directBox).childEnv",
		against: directBox,
		decides: "a direct box's processes are given the engine's environment through hostbox.Inherit alone, the " +
			"box's home, XDG and temporary directories, and the run's environment, so of the pod's environment only " +
			"what PassthroughEnv names reaches a Direct run",
		digest: "2ba9f4a8443ffe0d5fbbc823b3179c8194e7b837e74032f125f799d84aa45afa",
		read:   executorRead,
	},
	{
		file: localGo, name: "(*Local).reattach",
		against: directRunEnds,
		decides: "a box whose directory is no longer on its node's host is gone (ErrBoxGone), as every box of a " +
			"pod replaced with its sandboxes emptyDir is, so its lost run's round is carried on in a fresh box, " +
			"and one whose directory is there is read back as it is, as every box of a pod whose engine's " +
			"container alone restarted is, so the round is carried on in it",
		digest: "e110c16af07de262bc9cf2412503245704619a8d0150b77312a5da40afafffb3",
		read:   executorFinalRead,
	},
	{
		file: "internal/sandbox/local_witness.go", name: "jobEnd",
		against: directRunEnds,
		decides: "a direct box's job that is not running was lost, not ended, once the engine process that last " +
			"saw it running is gone, as a restart of an engine pod's container takes it, so its round is " +
			"carried on rather than collected as its result",
		digest: "79b1d6c93cc645fb43b5b6afc40111f646e0e60e78fa6e043993ed2cd8b526fb",
		read:   executorFinalRead,
	},
	{
		file: "internal/sandbox/lost.go", name: "(*Coordinator).carryOnLost",
		against: directRunEnds,
		decides: "a job lost with its host is never collected as the run's result: what it spent is read off its " +
			"box where the box is left, and the seat's turn resumes, with what this returns, to carry its round " +
			"on, in the kept box after its engine restarted — continuing the lost job's own session there where " +
			"Crewlet kept it — and in a fresh box where the box went with its pod or its node, continuing there " +
			"the session the turn's conversation's last turn left where the turn has kept none of its own and " +
			"its first launch was handed one, and a fresh session otherwise",
		digest: "9c0d3caef321b7d15e3937e91f050b52aa29123d636db520e2a215116adaed35",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/lost.go", name: "MaxRoundLosses",
		against: directRunEnds,
		decides: "a round is carried on after two losses at most, and the third ends the seat's turn",
		digest:  "3348ab90d8005a621166bb4d0023764e94f1de531aec0dc5476945797f382508",
		read:    executorFinalRead,
	},
	{
		file: "internal/sandbox/waiterhost.go", name: "(*Waiter).goneAfter",
		against: directRunEnds,
		decides: "a running job whose box is on the host of a node absent for the box timeout " +
			"(default_timeout_seconds) is recorded lost (launch_node_gone), and its round carried on by the " +
			"seat's next holder, and one whose node is back within it is not",
		digest: "fe9306cd8f560af34ee8a78abee8a609e07f9e88d3599da12df7e297dc058561",
		read:   executorFinalRead,
	},
	{
		file: "internal/sandbox/pending.go", name: "Holding",
		against: parkedBoxes,
		decides: "a run holds its seat while it launches, runs or resumes, and not while it is parked on a " +
			"person's question, so the seat's next run gets a box of its own beside the parked run's",
		digest: "a06607cd7ae9b1818f9c00edbb72df7914713fe7334712b0d70c049256eb7137",
		read:   executorRead,
	},
	{
		file: localBoxGo, name: "(*directBox).Pause",
		against: parkedBoxes,
		decides: "a Direct box parked on a person's question keeps its processes, stopped, and so their memory, " +
			"until it resumes or the pause reaper reclaims it at its pause TTL",
		digest: "607863a3b9692b8b1085feaeb2fd0a6d82bbccdf68de3006d5fbd3f43d0be601",
		read:   executorRead,
	},
	{
		file: localBoxGo, name: "TempRootEnv",
		against: boxWrites,
		decides: "a direct box's temporary directories are pointed into the box by these variables alone, and a " +
			"seat's Direct run is confined to nothing else, so a write that names /tmp or the engine's home " +
			"outright lands on the pod's scratch volume, and one under the data volume's path on a data pod's " +
			"data volume, not on the sandboxes volume",
		digest: "4d3c2d2b1c696693b9b574b7bb04e587964f778e89330e0031e327f78f2458f3",
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
		against: []string{companyTypesGo, executorGo, harnessesMd, runtimeMd,
			adr0008Md},
		decides: "deep work runs in its provider's run_in, else in E2B where the company configures it, else in a " +
			"container with an image, else on the engine host under the engine's own confinement, never by " +
			"default_run_in",
		digest: "5c900a1eb6c7e0484c2f60c289bdec274be0aca2d2f7bee1154b12312140b225",
		read:   executorRead,
	},
	{
		file: assistantDeepGo, name: "boxRootOf",
		against: deepWorkBox,
		decides: "a deep-work run placed on the engine host keeps its confined box under the company's box root, " +
			"providers.sandbox.local.state_dir, which the render writes whenever deep work can be placed on the " +
			"pod, so the data pods' sandboxSizeLimit bounds it",
		digest: "68d2a128ab0409ecb1d68c42913322b2662cb7b76c502a9cf8ddab141abc1701",
		read:   executorFinalRead,
	},
	{
		file: assistantDeepGo, name: "hostCell",
		against: deepWorkBox,
		decides: "a deep-work box is made in a directory of its own under the box root " +
			"(/var/lib/crewlet-sandboxes/assistant), which a seat's backend never reaches",
		digest: "564bf34e6cf2448eaade7ac077f8f0d7338c63f12fe8d0c615365255e861a87b",
		read:   executorFinalRead,
	},
	{
		file: localGo, name: "BoxRoot",
		against: append([]string{renderCompanyGo, companyWorkloadGo, seatPoolsGo}, deepWorkBox...),
		decides: "every box the engine makes on its host, a seat's and deep work's, is made under the state_dir as " +
			"written, and only with none under DefaultBoxRoot",
		digest: "63f5a8ac99347ea69e69ca315d9199267b3433213b05f3a22e19f66b77f9e6b8",
		read:   executorFinalRead,
	},
	{
		file: assistantDeepGo, name: "(*assistantDeep).target",
		against: []string{companyTypesGo, engineClassGo, companyExposureGo, harnessesMd, exposureMd, runtimeMd,
			"SECURITY.md", adr0008Md},
		decides: "deep work reaches Crewlet at the fleet's bridge address in whichever cell it runs, never at its " +
			"node's own listener, so a Company with no public address runs no deep work, the data pods alone need " +
			"egress to the public route (dataPodEgress), and a run placed on the engine host needs the host's " +
			"confinement",
		digest: "38abb71688f6ee7f1f1542b2d808d6f772c5b033b602e5d9cf52e2070e4930a8",
		read:   harnessRead,
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
		digest: "8627214fb2abd0b1200a4878b6a61e474152251f2cb5c033fde198bdce89ac8a",
		read:   chatLogRead,
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
		decides: "a node builds its tool bridge from the base its boot read from CREWLET_MCP_BRIDGE_URL through " +
			"ParseBoxURL (ReadBoxURLs), from the loopback address of its own listener, or from both, and given " +
			"neither builds none, so every launch of a box that would dial it is refused",
		digest: "c085f28a6035c1cb5f6588bbf9c12a9108733cf38cd5f6b353e488ec28c46598",
		read:   bridgeBaseRead,
	},
	{
		file: listenersGo, name: "(*nodeListeners).boxHostURL",
		against: append([]string{companyWorkloadGo, seatPoolsGo, companyExposureGo}, bridgeVerdict...),
		decides: "every node that runs seats and binds a listener its boxes reach has a loopback address of its own " +
			"bridge and telemetry receiver: an ingress node at api.public where it binds one, and a node without " +
			"the role, a seat pod, at api.port where loopback reaches it, so no engine pod's direct boxes dial " +
			"CREWLET_MCP_BRIDGE_URL, the operator gives every engine pod the Company's public address for E2B runs " +
			"and deep work alone and, under Isolated, lets the data pods alone reach the public route's proxies " +
			"(dataPodEgress), and ExecutorOf reports a Direct seat as runnable in any pool with no address",
		digest: "24f42453fc8d7c6a4366c1ff8feeb748dbffb20a389dbba35546a550afadbb0d",
		read:   executorFinalRead,
	},
	{
		file: listenersGo, name: "reachedOnLoopback",
		against: append([]string{companyWorkloadGo}, bridgeVerdict...),
		decides: "a listener bound on every interface, as a seat pod's api.port is, is one loopback reaches, so a " +
			"seat pod derives its own boxes' address",
		digest: "4a505982fd19d6c0aba6b35135e59f124cc70777a2b141b0e848d80cc042119c",
		read:   executorFinalRead,
	},
	{
		file: listenersGo, name: "loopbackURL",
		against: append([]string{companyWorkloadGo}, bridgeVerdict...),
		decides: "a listener bound on every interface, as a data pod's api.public and a seat pod's api.port are, is " +
			"dialled at 127.0.0.1 and its port over plain http, so an engine pod's direct boxes reach its bridge " +
			"with no address of the operator's, and ExecutorOf reports a Direct seat as runnable whether or not the " +
			"Company has one",
		digest: "3858556fc7840e1e4d62f1a9b73a8888c265c299aa8c766886017d23db43dc0b",
		read:   executorRead,
	},
	{
		file: bridgeGo, name: "(*Bridge).Reaches",
		against: []string{companyWorkloadGo, executorGo},
		decides: "a box on its node's own host dials the node's own listener wherever the node has one, ahead of " +
			"CREWLET_MCP_BRIDGE_URL, and every other box the variable alone, so the public address an engine pod " +
			"is given moves none of its direct boxes off loopback",
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
		file: engineBoxURLGo, name: "ReadBoxURLs",
		against: boxURLCopies,
		decides: "a node reads the tool bridge's, the telemetry receiver's and the code host's addresses, " +
			"CREWLET_MCP_BRIDGE_URL, CREWLET_SANDBOX_OTEL_RECEIVER_URL and CREWLET_BOX_GIT_URL, each through " +
			"ParseBoxURL",
		digest: "7b282fd465e5691eef2c90769071f45c96b6225695dc0dc6de0b4e16cf2fb834",
		read:   boxGitURLRead,
	},
	{
		file: engineBoxURLGo, name: "BoxURLs.All",
		against: boxURLCopies,
		decides: "the addresses a node's boot refuses on are the receiver's, the bridge's and the code host's",
		digest:  "3733d06f883b8ca05113b53f214d68c90d79e6f910154968e8db740b926ded9b",
		read:    boxGitURLRead,
	},
	{
		file: engineBoxURLGo, name: "BoxURLs.Refusal",
		against: boxURLCopies,
		decides: "a boot is refused on any of those addresses that is set and unusable, whatever the company runs",
		digest:  "da110da53cf23febfa9a9c8b51d4191e4a9c5050c52f37e490a7244408feb73c",
		read:    codeHostRead,
	},
	{
		file: boxURLGo, name: "ParseBoxURL",
		against: boxURLCopies,
		decides: "an address a sandbox dials, with the space around it dropped and unset when nothing is left, is " +
			"an absolute http or https URL with no user information, a host boxHostRefusal takes, a port from 1 to " +
			"65535 where it names one, and no query or fragment",
		digest: "14386a77a82a7374c7e52953c60e84e5d09de8d6ce0774bf2bbb132a6ed7672e",
		read:   codeHostRead,
	},
	{
		file: boxURLGo, name: "boxHostRefusal",
		against: boxURLCopies,
		decides: "that host has no IPv6 zone, is UTF-8 once its escapes are decoded, holds no forbidden domain code " +
			"point, and ends in a number only as an IPv4 address the WHATWG URL Standard reads",
		digest: "35d2dde9a7a5b8bf7df716d11e55e25e442d9dc158d2787bf9782d50a594652f",
		read:   elevenLogsRead,
	},
	{
		file: boxURLGo, name: "forbiddenInADomain",
		against: boxURLCopies,
		decides: "the forbidden domain code points are the C0 controls, space, DEL and # % / : < > ? @ [ \\ ] ^ |",
		digest:  "efc942efcd201819bbc4b389a782c324385ee3577f156b81a713532118ace2be",
		read:    codeHostRead,
	},
	{
		file: boxURLGo, name: "endsInANumber",
		against: boxURLCopies,
		decides: "a name ends in a number when its last label, one trailing dot ignored, is decimal digits or a " +
			"part ipv4Number reads",
		digest: "195fefaf6020f2c47e36752f235648edb47ee260937a173c43a50085427c7bdd",
		read:   codeHostRead,
	},
	{
		file: boxURLGo, name: "isIPv4Address",
		against: boxURLCopies,
		decides: "an IPv4 address is at most four parts ipv4Number reads, every one but the last at most 255 and the " +
			"last below 256 to the power of the bytes left for it",
		digest: "d9cd518b5a95dc4a62d7293975ef4c119a077054101a6049b2f321a448915465",
		read:   codeHostRead,
	},
	{
		file: boxURLGo, name: "ipv4Number",
		against: boxURLCopies,
		decides: "a part of an IPv4 address is hexadecimal after 0x, octal after a leading 0, and decimal otherwise",
		digest:  "8eb7861dd55c978ff9b4b4abd409cf7202f6beceddfa30cc1af229cf2e8276de",
		read:    codeHostRead,
	},
	{
		file: "internal/coord/kv/objects.go", name: "(*FleetStore).AgreeObjectBackend",
		against: bootWindows,
		decides: "the object store's fleet record is created, or read back, in casRetry's attempts, each a create and " +
			"a leader read at the client's 5 s default, and an attempt is lost only to a record removed between the " +
			"create's refusal and the read",
		digest: "22378d242561f7e5c397fe5d5e9a184f947ec8144ba0908b33aa85e5d2281d95",
		read:   codeHostRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).Floor",
		against: bootWindows,
		decides: "a domain's trim floor is one leader read of its key, at the client's 5 s default",
		digest:  "b706cb84a50391e17bad24e514ba31001e05fd7809f4bad660e945dd448b0795",
		read:    codeHostRead,
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
		digest:  "88fe79922377dee667b5bc655b010eea3cf9ad394583c0c2f2fd92ddd297c87e",
		read:    clockSkewRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).eachPositionKey",
		against: bootWindows,
		decides: "a key class of the positions register is eachPositionPrefix's walk of that one class",
		digest:  "d1e13631f8da420b4bbd17cd2bbfd93ca05a39099806764df47bed2818002e22",
		read:    tenLogsRead,
	},
	{
		file: positionsGo, name: "(*FleetStore).eachPositionPrefix",
		against: bootWindows,
		decides: "the keys of the positions register under a prefix of key segments are one filtered walk of the " +
			"register, each of its entries handed on with the instant the broker stored it, which the entry " +
			"already carries",
		digest: "931e071ff0dd6ba13ebdbcfd446bf396b0f86468a0c9aaaec3eed0c28ddaee56",
		read:   tenLogsRead,
	},
	{
		file: fleetGo, name: "(*FleetStore).eachUnder",
		against: bootWindows,
		decides: "a filtered walk of a fleet bucket is one eachEntryUnder on the store's client",
		digest:  "24d6c44892f1ec779606075259970a7c3a60f278428c6486661c3fb1df98884f",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "eachEntryUnder",
		against: bootWindows,
		decides: "a filtered walk addresses its certifying reads without a request, then is one certified listing",
		digest:  "42b80859a1b27b6cbdc702d5859deab28e3b5b9f744269034eab0ce203c7a1cb",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "newLeaderReader",
		against: bootWindows,
		decides: "a bucket's leader reads are addressed by NewLeader for the bucket's stream, without a request",
		digest:  "b2fde2aad29b0b030cb4c761da926af5c2eb5c307915e46c21df67666edc1ebb",
		read:    clockSkewRead,
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
		file: walkGo, name: "(*pass).walk",
		against: bootWindows,
		decides: "a pass is walked to its end by one passOver, keeping the key index when it answered during the walk",
		digest:  "5ee4064276f1fda1bf68996064462051a00f969c4baf4c6345e86d874ee03d06",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "(*pass).stop",
		against: bootWindows,
		decides: "a pass's watcher is stopped once, by its own Stop",
		digest:  "bf2ef70d2391df82d61fb4a08234c45a354c82ab959dbc5a281ef27d988de124",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "(*pass).certify",
		against: bootWindows,
		decides: "a pass is certified by one certify against its key index, which it waits for only when the pass " +
			"ended before the index answered",
		digest: "ce99a60a0adb4f6e09e5811d1a773dc2545f9d39693b5b0c3565e14cd6fd120e",
		read:   clockSkewRead,
	},
	{
		file: walkGo, name: "(*pass).close",
		against: bootWindows,
		decides: "a pass's close cancels its key index read and, where the pass has not read its answer, waits " +
			"for it to return, asking nothing",
		digest: "d167d12a0cd1285afaadea7ea94d9a456b57cfbbf908841a784c1e48d1ad197f",
		read:   clockSkewRead,
	},
	{
		file: backendsGo, name: "attachCoordination",
		against: bootWindows,
		decides: "the coordination buckets are opened, and each seat's fencing epoch floored above its sandbox " +
			"runs, under one sequence ceiling of their own",
		digest: "2f72ff42c018781caf2954d7fad78fcb41a101a58075fa621ff9277b92528eb2",
		read:   tenLogsRead,
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
		decides: "sizing the nats bucket reads the broker's limit under LookupBudget, and carries on from a limit " +
			"made of the volume's free space past it",
		digest: "cfbb9b3c4231a834f970a45b379af7f50d573b5d8876dc39e936fe82f4a2864a",
		read:   codeHostRead,
	},
	{
		file: backendsGo, name: "openObjects",
		against: bootWindows,
		decides: "the object store agrees the fleet's backend, then opens the nats bucket under a sequence ceiling" +
			" of its own or probes the S3 bucket",
		digest: "25c0df1a1666eadb808e825fddb5ac10bf2327da631b5b92070c1e7446d22945",
		read:   codeHostRead,
	},
	{
		file: "internal/engine/core.go", name: "(*Engine).startCore",
		against: bootWindows,
		decides: "a data node starts its state logs before it reports itself recovered",
		digest:  "8f65af0c3c56e5d39f9df259bcd694270235eaa73adc20514de54f2e97e00a97",
		read:    executorLogRead,
	},
	{
		file: "internal/engine/evicted.go", name: "(*stateLog).fleetGenerations",
		against: bootWindows,
		decides: "the join's generations are judged from the floors the join read, and read a log only to judge a " +
			"node ahead of this one evicted, which a lone data node has none of",
		digest: "0e28bf714bf8c255a8beaf4f84dc122aa95270afbf16bc81b2fc9d00dda24f3a",
		read:   codeHostRead,
	},
	{
		file: "internal/engine/fleetbroker.go", name: "(*Engine).serveFleetBroker",
		against: bootWindows,
		decides: "a broker member registers its fleet broker answerer, one flush",
		digest:  "f77b2a24359ede0f1fbc0ab79168871ce395401314d9863c388ce53a61a5a5c5",
		read:    codeHostRead,
	},
	{
		file: "internal/engine/history.go", name: "(*Engine).armHistory",
		against: bootWindows,
		decides: "a data node registers its history answerer, one flush",
		digest:  "4d7b4387544fd890a46a25702eaf0d1faa63a7dd56957e840f7dd63f156761f8",
		read:    tenLogsRead,
	},
	{
		file: "internal/engine/liverelay.go", name: "(*Engine).armLiveRelay",
		against: bootWindows,
		decides: "a data node registers its live relay, one flush",
		digest:  "add17732d6cf4258c0b3a932d7473c3170243d201f7c10a35d4c2119b39c30ac",
		read:    tenLogsRead,
	},
	{
		file: "internal/engine/liverelay.go", name: "(*liveRelay).serve",
		against: bootWindows,
		decides: "the live relay a data node arms (armLiveRelay) registers one subject, its node's own, through " +
			"the queue's Serve",
		digest: "b8c94c5c260c551689bb659960f2531241dbaa7a8bb22b7824ec95a8a3094cb3",
		read:   tenLogsRead,
	},
	{
		file: "internal/engine/maintenance_mode.go", name: "(*Engine).admit",
		against: bootWindows,
		decides: "a data node's admission writes its record and reads each log's maintenance window",
		digest:  "053e5edea1b55b86e8eb47fda76435d074ff5c1ae42b15c542d177293b116d20",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/maintenance_mode.go", name: "(*Engine).handshake",
		against: bootWindows,
		decides: "a boot's half of the capacity window is admit alone in normal mode, every engine pod's: what " +
			"supersedes a previous process's admission and acknowledges a window runs only in a maintenance mode",
		digest: "7f7d99f653c446ba44eaf13aff9e3b3b44be2096844c46663c519eda87c49633",
		read:   chatLogRead,
	},
	{
		file: "internal/engine/presence.go", name: "(*Engine).armPresence",
		against: bootWindows,
		decides: "every node registers its presence answerer, one flush",
		digest:  "20edef0a7bc81e1c24cc9670cee699a0646ebefb538943d803afbc5768a68159",
		read:    tenLogsRead,
	},
	{
		file: "internal/engine/remote.go", name: "(*Engine).serveEstate",
		against: bootWindows,
		decides: "a data node registers its estate service, and hands it a git half only where the node has the git " +
			"role, which no pod the operator runs has",
		digest: "7b2a9fbfee94698df1708f53198f434b9d3120c48a875f2b06f247a48f9e54ce",
		read:   gitLaneRead,
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
		digest:  "dab32de2f85b86a945e0dbc8e6e646615a6eb67b66a59e832109c9788fa2b914",
		read:    tenLogsRead,
	},
	{
		file: stateLogGo, name: "(*Engine).startStateLog",
		against: bootWindows,
		decides: "the state logs read their ceilings, are provisioned under one sequence ceiling, joined, and " +
			"started under another",
		digest: "9f8a3713ab21351a36fb77cd5d273e385b0e77cf193d956c6f741a4c44da9fe1",
		read:   elevenLogsRead,
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
		decides: "the join lists the fleet's positions and reads every log's trim floor at once, then reads each " +
			"log's stats and judges its floor from that one read",
		digest: "8df5a23499b2d22cd15b742e83cf3f49478392e30008eed1d5d35ff129b5ed4f",
		read:   codeHostRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).start",
		against: bootWindows,
		decides: "a log's start reads its stats, attaches its consumer and verifies its checkpoint",
		digest:  "8ffa32374d4b19690f338bd7f8d62bbd8ee54a969840fd0fd84da3fa8fd42ed1",
		read:    tenLogsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).startLog",
		against: bootWindows,
		decides: "each log is started in turn, under the consumers' sequence ceiling",
		digest:  "501906b3addfef47df47b6da4a96209f955b9da2788e709d41f9d3c34a748efe",
		read:    bootBoundsRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).readFloors",
		against: bootWindows,
		decides: "the join reads the trim floor of every registered domain in one read of them all",
		digest:  "1acf7c934f5bf9c9fd523ca54345b6ba7a4a37fdaaf7f1db5529f82f6b49299f",
		read:    codeHostRead,
	},
	{
		file: stateLogGo, name: "readFloorsOf",
		against: bootWindows,
		decides: "the floors of the domains a read names are read all at once, each by its key",
		digest:  "7801568deb89f4c30dc12954adefd594b07127b236c4b0c599e195862b981d16",
		read:    codeHostRead,
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
		digest:  "34db69352bc8adc9f9674878dad7db6d59b7000f5b2a14b5fbbb54e10cc846c4",
		read:    codeHostRead,
	},
	{
		file: "internal/estate/server.go", name: "Serve",
		against: bootWindows,
		decides: "the estate service registers its lanes one after the other, each through the queue's Serve, and " +
			"the git lanes beside them only when it is handed a git half",
		digest: "fc36b2d8ad8f921624379f67f2130537f0f0b8033f5f7df7d120b3481bbe28e7",
		read:   bootStanceRead,
	},
	{
		file: "internal/estate/wire.go", name: "lanes",
		against: bootWindows,
		decides: "every node that serves the estate registers two lanes, the shared one and the proposals'",
		digest:  "90f732344404e8d6f55ea3da000d0630598a0f724861fea1e46afa81220d5e38",
		read:    gitLaneRead,
	},
	{
		file: "internal/eventfan/fanwire.go", name: "Serve",
		against: bootWindows,
		decides: "the history answerer a data node arms (armHistory) registers one subject, through the queue's Serve",
		digest:  "ca0feed0ded0e8803a7ddc32187a6f8f713460a4f2cc0d3d4a1340598f1a85a1",
		read:    gitLaneRead,
	},
	{
		file: jsprovisionGo, name: "LookupBudget",
		against: bootWindows,
		decides: "a lookup of a stream or bucket, the broker's limit read, and every read asked again while nobody " +
			"answers it, a boot's read of a state log among them, are bounded at 30 s",
		digest: "fa87cb9b864068dc2bf52d7f9d17a79b008f788acbf791b1f3737e298adf9de0",
		read:   bootBoundsRead,
	},
	{
		file: jsprovisionGo, name: "Clustered.Timing",
		against: bootWindows,
		decides: "a topology's provisioning timing takes its budgets, and bounds a read at LookupBudget whatever " +
			"the topology",
		digest: "1cb4c07de297eb8d8cd99cc50d7136670dc9a6b3a4599791b14a8066da4ffea3",
		read:   hedgeCeilingRead,
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
		file: "internal/presence/hub.go", name: "(*Hub).Start",
		against: bootWindows,
		decides: "the presence answerer every node starts (armPresence) registers one subject, through the queue's " +
			"Serve, and runs its loops on goroutines the boot does not wait for",
		digest: "ac17cd3be72b09e76034e3d261e4fb64f92503582df2a445958c9b2425810e61",
		read:   tenLogsRead,
	},
	{
		file: "internal/queue/jetstream/attach.go", name: "(*Queue).attach",
		against: bootWindows,
		decides: "attaching a durable group looks up, creates and reads back its consumer, then looks it up and " +
			"reads it back again, and an agent's inbox its lane's consumer too",
		digest: "17c1001b72e34f1d49be6db7cb3ee885c721098d6eaee5e52acc9eb8caeb0274",
		read:   inboxLaneRead,
	},
	{
		file: "internal/queue/jetstream/budget.go", name: "(*Queue).budget",
		against: bootWindows,
		decides: "the broker's limit is a read asked again while nobody answers it, held to the client's " +
			"DefaultTimeout where its caller set no deadline",
		digest: "d3b790a95154bbfb5effdd81becaa86d1f61fc3378f626cacf1dba19e0526119",
		read:   hedgeCeilingRead,
	},
	{
		file: embeddedGo, name: "(*embeddedServer).awaitLeafReady",
		against: bootWindows,
		decides: "a leaf asks for the fleet's JetStream until clusterReadyTimeout, checking the deadline only " +
			"before each ask",
		digest: "af83b256e8110005765fdda32e45006f0ff7bee4a7d2a6a1b8955c69770d9a5d",
		read:   brokerAuthRead,
	},
	{
		file: embeddedGo, name: "(*embeddedServer).connect",
		against: bootWindows,
		decides: "a connection to the node's own broker is a handshake bounded by listenTimeout, then held to the " +
			"contract",
		digest: "31e5318acb11098be612c0c0fe615973a1941957318f27e859be28a110809364",
		read:   brokerAuthRead,
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
		file: queueGo, name: "(*Queue).provisioning",
		against: bootWindows,
		decides: "the queue's reads, a log's among them, are asked on its topology's provisioning timing",
		digest:  "ec4ea5f0aec0a8a3b05770e91aa9ad013d80ae070cd1317d02a1863c58d65de1",
		read:    hedgeCeilingRead,
	},
	{
		file: "internal/queue/jetstream/scatter.go", name: "(*Queue).Serve",
		against: bootWindows,
		decides: "registering what a node answers subscribes to its subject, then flushes the subscription once",
		digest:  "3723b2e3ed09ba3f1a8e462b1a15351e3ecb4679adb1959a2955ee7965887980",
		read:    gitLaneRead,
	},
	{
		file: "internal/queue/jetstream/scatter.go", name: "(*Queue).flush",
		against: bootWindows,
		decides: "a flush waits for the broker's answer for at most flushBudget",
		digest:  "8fa5c9ae81c1b7fac5457e1385c4925d19030696eb6017d0d608ff275fb7fe0e",
		read:    gitLaneRead,
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
		file: fleetGo, name: "(*FleetStore).get",
		against: bootWindows,
		decides: "the fleet store's read of one key is one getLatest on the store's client",
		digest:  "6b976d59a8c3cb725e30ff4cd913f2fe317c36a912e0ba804d73f8ef979d1a41",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "getLatest",
		against: bootWindows,
		decides: "a read of one key addresses its leader reads without a request, then is one read of the key's live " +
			"value (live)",
		digest: "6e289db52cad44133ae891fd3ab23152bf91e349c8cf700ccc84cc12c3061d39",
		read:   clockSkewRead,
	},
	{
		file: walkGo, name: "(*leaderReader).live",
		against: bootWindows,
		decides: "a key's live value is one read of its newest message (last)",
		digest:  "49c30248ccfd5bdbf1fac4931841df6886e4526e165ada4155bde1d16bd0c567",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "(*leaderReader).last",
		against: bootWindows,
		decides: "a key's newest message is one leader read of its subject (Last)",
		digest:  "1c37b5838fdc14b39f539128e1fe0c15efe5d8cf7ebe482c209b95b3f7d66b3a",
		read:    clockSkewRead,
	},
	{
		file: walkGo, name: "keysUnder",
		against: bootWindows,
		decides: "a pass's key index is one listing of the bucket's subjects from its leader",
		digest:  "390fdd0fae2295dea55d380c4e12d71fa7443c26e05626c5d77dbad61c138012",
		read:    hedgeCeilingRead,
	},
	{
		file: jsapiLeaderGo, name: "(*Leader).get",
		against: bootWindows,
		decides: "a leader read is asked through ask with its reader's term and bound, so one on a context with no " +
			"deadline is held to the client's DefaultTimeout, and asked again inside it while nobody answers",
		digest: "0cedea9143c6f0f7761ae11d2a5c25783d052005b4a52a679f40e7c1981cb08d",
		read:   hedgeCeilingRead,
	},
	{
		file: jsapiLeaderGo, name: "(*Leader).Last",
		against: bootWindows,
		decides: "a subject's newest message is one leader read, through get",
		digest:  "04a4497229210e557d517813166a097d48c082f725f297328f75798747c4a43e",
		read:    clockSkewRead,
	},
	{
		file: jsapiLeaderGo, name: "NewLeader",
		against: bootWindows,
		decides: "a leader reader's bound is its client's own DefaultTimeout, and its term the read term",
		digest:  "399059045e1b688fdca2fcceef4b843098de46a4858c328472834413f907aa7a",
		read:    hedgeCeilingRead,
	},
	{
		file: jsapiListingGo, name: "ask",
		against: bootWindows,
		decides: "a leader request is asked as askTimed asks it, with the term and the bound it is handed",
		digest:  "1ce0f6238e140b6ca0c55bdaef465adbe4f2a40b5522b41963bffd216d35c892",
		read:    hedgeCeilingRead,
	},
	{
		file: jsapiListingGo, name: "askTimed",
		against: bootWindows,
		decides: "a leader request is asked again at the term it is handed while nobody answers it, held to the " +
			"bound it is handed (ReadBoundedWith)",
		digest: "79b13c39c613883c04a1b2c775116d86c6838bf6e9b3ff624e0ad133280a6eb6",
		read:   hedgeCeilingRead,
	},
	{
		file: jsapiListingGo, name: "page",
		against: bootWindows,
		decides: "a page of a listing is one leader request, asked as askTimed asks it, with the term and the " +
			"bound it is handed",
		digest: "468cd0e20919633f78ea1061aca5adc65eb3ebb5ba0b4be95ada3228ffacca37",
		read:   hedgeCeilingRead,
	},
	{
		file: jsapiListingGo, name: "sorted",
		against: bootWindows,
		decides: "a listing asks no page past the first that reaches the end of its list, so a short list is one page",
		digest:  "13c9c190383fc71842f410477aacaee4a66d630cd215fbc0b9a7441f62f09286",
		read:    hedgeCeilingRead,
	},
	{
		file: jsapiListingGo, name: "(*Leader).Subjects",
		against: bootWindows,
		decides: "a listing of a stream's subjects asks its leader a page at a time, each page held to the " +
			"reader's bound, the client's DefaultTimeout",
		digest: "574c367ad336c710b98a82c8252b437e4f15f2ef2342f62ea26c00be5a4fee08",
		read:   hedgeCeilingRead,
	},
	{
		file: hedgeGo, name: "ReadWith",
		against: bootWindows,
		decides: "a read hedged from the start, a log's state and its record among them, is handed its timing's " +
			"Lookup as its bound, which a boot's read waits out, its context having no deadline",
		digest: "72eb5391be043593b8c06e4b59bc4b470eeff3015db304f9191c4a77de5684fc",
		read:   hedgeCeilingRead,
	},
	{
		file: hedgeGo, name: "ReadBoundedWith",
		against: bootWindows,
		decides: "a read asked again while nobody answers it is held to the bound it is handed where its caller set " +
			"no deadline, before it is asked, and every such read to its timing's Lookup",
		digest: "660ea8947b4504f311ee63fd0faaa49c6cb656b93ea3d841694be67061202613",
		read:   hedgeCeilingRead,
	},
	{
		file: hedgeGo, name: "hedge",
		against: bootWindows,
		decides: "a read nobody answers is asked again, every request left waiting for its answer, until one is " +
			"answered or its context ends",
		digest: "ea86d8197c380b8b8fbb29a107c87f21d9799573eb9762108bf8cab63c2ee34c",
		read:   codeHostRead,
	},
	{
		file: domainLogGo, name: "(*DomainLog).Stats",
		against: bootWindows,
		decides: "a log's stats are one read of its state, on its caller's context",
		digest:  "153fe4d84bad2026a4bc4ea4bfdb72ecf60a61606d4523281767a02f9b2f7e57",
		read:    codeHostRead,
	},
	{
		file: domainLogGo, name: "(*DomainLog).stateInfo",
		against: bootWindows,
		decides: "a log's state is read through ReadWith",
		digest:  "6a1ef157513db6222b46319f5540f8b7f9062633b7068b6af6492db8f67d50b8",
		read:    bootStanceRead,
	},
	{
		file: domainLogGo, name: "(*DomainLog).At",
		against: bootWindows,
		decides: "a log's record at a sequence is one read through ReadWith, on its caller's context, given a longer " +
			"term only across a link and the same bound",
		digest: "051c726fd957beac0e523ed0e0d7562e7573784dc507b96f97c6e85f38f9d255",
		read:   hedgeCeilingRead,
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
		file: casLineGo, name: "casRetry",
		against: bootWindows,
		decides: "a fleet write is attempted again only after it lost a race, at most fleetCASAttempts times, after a " +
			"wait that grows with every attempt lost, and no time bounds the whole",
		digest: "f91023d706284d3861d8e54503a9dc13fd1be098323df660d89dbe02cd10cfb1",
		read:   codeHostRead,
	},
	{
		file: casLineGo, name: "fleetCASAttempts",
		against: bootWindows,
		decides: "the engine's one bound on a fleet write it attempts again is a number of attempts",
		digest:  "b614f3a6b04f29bc7eabb16fdd28b9fe29f2dc0b0448e3f0fa47908d72b73505",
		read:    codeHostRead,
	},
	{
		file: fleetGo, name: "(*FleetStore).create",
		against: bootWindows,
		decides: "the fleet store's create of one key is one createKey on the store's client",
		digest:  "0ad4ffe5e3400dc8a40ac8751ab9748bf16880e4bb7ea58b063fcebfb7cc5bab",
		read:    clockSkewRead,
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
		digest: "b96958987c5ddc851225e253ce05db9f6b1af539b9b1af60a3551ef146425dee",
		read:   tenLogsRead,
	},
	{
		file: settleGo, name: "settled",
		against: bootWindows,
		decides: "a settled bucket's conditional writes fall back to its client's own DefaultTimeout",
		digest:  "d574c24c954f803deccf9622e5a8cdd97039cc6d5161e6438aa0f0e2c6c88acf",
		read:    clockSkewRead,
	},
	{
		file: settleGo, name: "settledBucket.Update",
		against: bootWindows,
		decides: "a settled bucket's conditional update is one settle of the client's own update",
		digest:  "bd17315473d3f04657610788c4961e3f029918ad04533f466534c5dab2882c90",
		read:    clockSkewRead,
	},
	{
		file: settleGo, name: "settledBucket.settle",
		against: bootWindows,
		decides: "a settled write is one Decide, with the bucket's timeout as its fallback",
		digest:  "211820629cbab521ec48f901c6a2f829b9161d761254ee384faee9f8cb3dadda",
		read:    clockSkewRead,
	},
	{
		file: "internal/jsinflight/jsinflight.go", name: "Decide",
		against: bootWindows,
		decides: "a publish the leader answers in flight is asked again within its caller's deadline or, where it " +
			"set none, the fallback it is handed, each attempt handed that budget and no wait outlasting it",
		digest: "7c7e9741b58d504b4ad9693aede7c91f9baadfd3fb96b0e5e4432fdece9805aa",
		read:   clockSkewRead,
	},
	{
		file: natsObjGo, name: "open",
		against: bootWindows,
		decides: "the nats bucket is looked up, created and read back, then bound, under the window it is handed",
		digest:  "e77af5243967860f1886fd619e03808868860e88c2c4a5666660a3969e85f9ee",
		read:    hedgeCeilingRead,
	},
	{
		file: natsObjGo, name: "lookupBucket",
		against: bootWindows,
		decides: "the bucket's lookup is a read through ReadWith, bounded by the timing's Lookup",
		digest:  "3b0dbfbf074b7375a71a0869d86d5ee024216abc6bda6e3142d3189b577806c7",
		read:    hedgeCeilingRead,
	},
	{
		file: natsObjGo, name: "createBucket",
		against: bootWindows,
		decides: "the bucket's create is bounded by the timing's Budget, and read back under ReadBack",
		digest:  "684a164cd0e747e24c77ad15708c7b266a66cae0994c3eefb6e8ef626798ed35",
		read:    codeHostRead,
	},
	{
		file: natsObjGo, name: "boundBucket",
		against: bootWindows,
		decides: "a bucket with no ceiling is given one only when the ceiling is named explicitly",
		digest:  "07f0675b5fe636195f957377fe60140f3158d85b2fdabfe55a2a4bdc2f6cb79c",
		read:    bootBoundsRead,
	},
	{
		file: "internal/seataccess/seataccess.go", name: "Kinds",
		against: []string{renderCompanyGo},
		decides: "the integration kinds whose per-seat access the engine manages, each of which an operator " +
			"document names in seat_access_migrated (seatAccessKinds): the kind of each of Lanes, once",
		digest: "1f2dab9b9537b12d69e4e8f480d005042edab86e2879dfb9860223158a894bb4",
		read:   seatAccessMergedRead,
	},
	{
		// KINDS IS DERIVED FROM LANES, so a lane added moves this digest and
		// not that one: without it, the table could not tell that a kind was
		// added to seat_access_migrated.
		file: "internal/seataccess/seataccess.go", name: "Lanes",
		against: []string{renderCompanyGo, seatTypesGo},
		decides: "a seat holds per-seat access on the lanes slack, mattermost, jira, confluence, github, gitlab, " +
			"datadog, cloudflare and expo, whose kinds (atlassian for jira and confluence) seat_access_migrated names",
		digest: "16f9cfa3c4b90b51d6c532186bbd92f3aa2ba95738900f116810e4a5aa44062f",
		read:   landingRead,
	},
	{
		file: "internal/config/toolbinding.go", name: "SeatCredentialKinds",
		against: []string{mcpServerTypesGo, renderEnumsGo, toolServersMd},
		decides: "a tool server reads the seat credential of atlassian, cloudflare, datadog, expo, github, gitlab, " +
			"mattermost or slack",
		digest: "e8002eec0e4187974ee2700a4e0eeeffc469fc2f7b40ec77c9ad451f8582fb86",
		read:   landingRead,
	},
	{
		file: "internal/config/toolbinding.go", name: "seatCredentialFields",
		against: []string{mcpServerTypesGo, renderEnumsGo, toolServersMd},
		decides: "each integration's seat credential is one field, but an Atlassian account's two, TOKEN and EMAIL, " +
			"so seat_credentials names a field for Atlassian alone",
		digest: "9a68989722fa22c2076f40c7d7d444a816a682bc43012aa23d06b07b1b89bebe",
		read:   chatLogRead,
	},
	{
		file: "internal/envref/envref.go", name: "SealedName",
		against: []string{"internal/secretsync/name.go", commonTypesGo, credentialsConceptMd},
		decides: "the engine seals a credential it mints for a seat under the vendor, the field and the seat's " +
			"handle joined by underscores and upper-cased, every character but letters, digits and underscores " +
			"turned into an underscore, and under no other name, which secretsync.ProvisionedName copies",
		digest: "9e2cc88e4ff0ae4dcd23a9f29b9e6c77350897bc292dc4350e616912b680a659",
		read:   seatAccessRead,
	},
	{
		file: roleAccessGo, name: "GitHubPermissionKeys",
		against: seatAccess,
		decides: "a seat's GitHub App holds contents, issues, pull_requests, checks, actions, deployments and " +
			"workflows, by GitHub's keys, each read or write, and Metadata is not offered",
		digest: "d1c35603ab547e5e0743594de58f49192e4b8928038c14d24eb810178e236fe9",
		read:   seatAccessMergedRead,
	},
	{
		file: roleAccessGo, name: "GitLabRoles",
		against: seatAccess,
		decides: "a seat's GitLab role is Guest (10), Planner (15), Reporter (20), Developer (30), Maintainer (40) " +
			"or Owner (50), by the integer GitLab's Members API takes, and neither Security Manager nor Minimal Access",
		digest: "cf643b0d20691a83511683191dd8a37260d52748266d439cfc03ea06e8077af2",
		read:   seatAccessRead,
	},
	{
		file: roleAccessGo, name: "MattermostRoleSets",
		against: seatAccess,
		decides: "a seat's Mattermost bot holds system_user alone or with one of system_post_all_public, " +
			"system_post_all and system_admin, each set written in Mattermost's own order",
		digest: "ed925957ab3912395840a7991620f0660ca0a918fa694dc0602a318f167bec58",
		read:   seatAccessRead,
	},
	{
		file: "internal/config/cloudflare.go", name: "CloudflareLevels",
		against: seatAccess,
		decides: "a seat's Cloudflare token is minted at read, operate or configure, an unset level reading as read",
		digest:  "03767a3751ac46e0d12056e647f99f322ac425196bf1b84a08b795940a38bd5f",
		read:    cloudflareConfigureRead,
	},
	{
		file: "internal/config/expo.go", name: "ExpoLevels",
		against: seatAccess,
		decides: "a seat's Expo robot user holds viewer, runner, developer or release_manager, an unset level " +
			"reading as viewer",
		digest: "0bd7e5dfbab3b52882c70397f497d2be7331c107c46ba1a0161119040c2e6614",
		read:   expoHandoffRead,
	},
	{
		file: "internal/executorkeep/limits.go", name: "SessionMaxBytes",
		against: sessionSpools,
		decides: "a kept executor session's archive is 32 MiB at most",
		digest:  "8d113607e37d1be8d26c9abb8546c790f930d5f9a1ade8ba74f8736b8baf50d0",
		read:    landingRead,
	},
	{
		file: "internal/executorkeep/limits.go", name: "SealedMaxBytes",
		against: sessionSpools,
		decides: "a kept session is spooled and stored as its archive sealed, so a spool is a sealed archive of " +
			"32 MiB at most",
		digest: "993d15134f59fd998ececf6b9b8d65886740dbe9233f047f5493d0c5b90811f1",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/session.go", name: "(*Coordinator).spoolDirectory",
		against: sessionSpools,
		decides: "a node spools the sessions it keeps, and the ones a relaunch fetches back, in its temporary " +
			"directory (os.TempDir), /tmp on an engine pod, unless its coordinator is given a directory of its own",
		digest: "2a049246fdbad3959b0cbf2a44a73fafbf2bd194bb65654576138046fb02bd7a",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/session.go", name: "(*Coordinator).keepLater",
		against: sessionSpools,
		decides: "a capture's upload runs off the read that took it, on the coordinator's own lifetime, so its " +
			"spool stays past the read until the upload is done, and nothing bounds how many of a seat's are " +
			"under way at once",
		digest: "253c613b2d667e52931a45d69ba1bc4ceeaae3882871161b6d8f631fdfd64c9e",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/restore.go", name: "(*Coordinator).ContinuedCapture",
		against: sessionSpools,
		decides: "a relaunch continues the session of the job it follows only once that job's capture is no " +
			"longer being kept, waiting relaunchCaptureWait at most, and starts a session of its own beside " +
			"one still being kept rather than wait it out",
		digest: "3efa75ce59db1665348501f73679985ed2017aa39e10b9f8eb140103c6a06822",
		read:   landingRead,
	},
	{
		file: "internal/sandbox/restore.go", name: "relaunchCaptureWait",
		against: sessionSpools,
		decides: "a relaunch waits 15 seconds for the upload of the session it would continue",
		digest:  "f21b520275d382724e510bed7f4fe23221c983af58aad05ac1f63efbb5051f1f",
		read:    landingRead,
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
