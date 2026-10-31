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

// mirror is a declaration of the engine whose decision the operator copies
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
// the address a data pod's engine is told is on loopback, and whether it is
// one the engine refuses to start on at all, or to boot on where a pod hands
// it to sandboxes, which the render holds a data pod back on (and the API
// refuses where a Company or its class states it) and the engine shows only
// for the addresses a test tries. A contract test
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

	// optionalKeysRead is crewlet#293's engine, which every mirror it changed
	// was read again at, as crewlet#291's had been before it. A run's
	// environment is laid by runEnvironment, which replaced sandboxEnv and
	// boxSetup: the setup steps, then the seat's role.sandbox.env, each later
	// layer winning a name, and a setup file whose body is one whole ${VAR}
	// (every one the render writes) written as that value, as before; what is
	// new is that a value that resolves to nothing is left out where the
	// layer that wins lists its key in optional_keys, and is otherwise a gap
	// that refuses a run_sandbox launch before a box exists (Launch) and is
	// said, the run starting without it, in an agent-mode executor's run
	// (LaunchExecutor). A value a provisioning pass mints for the seat is
	// waited for whatever the declaration says (refGap.hold). The render
	// follows it: the code-work verdict reads the winning layer and counts an
	// optional sign-in that is missing for nothing, and the NoProvisioner
	// report says which gestures go without the value. The coding agent a
	// launch runs, the head it runs on and every other refusal before a box
	// exists are as they were.
	optionalKeysRead = "v0.1.1-0.20261021054645-37bd6f2e0c50"
	// cliOnlyRead is the engine every model of which is a coding CLI, read
	// on top of crewlet's main at this version, before that change merged:
	// a providers.llm entry has no type, the cli block's fields are its own,
	// and API entries are gone. newCodingRun hands every head's run the
	// model as its text calls pass it, and its sign-in under what the seat's
	// environment sets, but a ClaudeCode run none of it once the seat's
	// environment signs ClaudeCode in (entrySignIn); claudeCodeOnForeignEntry
	// and SandboxModelUnsupported went with the API entries; and
	// sandboxCredentials passes a ClaudeCode run any variable that signs it
	// in. Launch and LaunchExecutor hand the box the run's model in place of
	// an API entry's endpoint, which moved no verdict.
	cliOnlyRead = "v0.1.1-0.20261023214803-bbedf7ecebd7"
	// bridgeStopRead is crewlet/crewlet#298's engine whose agent-mode launch
	// opens its run's session on the tool bridge with the seat's stop
	// (mcpbridge.Session.Stopped), read at the head of its branch, where
	// LaunchExecutor was read again. The launch builds the session in
	// bridgeSession now, with the fields it set before and the stop check,
	// stoppedBy, the one reading the code host refuses a run's box's pushes
	// by: a person's pause that stops the seat's running work refuses every
	// bridged call of the run to a tool not proven read-only, until the seat
	// is resumed, and answers it paused, naming who stopped the seat; a read
	// still runs. That is no verdict on a launch: bridgeSession always sets
	// the check, so the bridge's refusal of a session without one is never
	// met, and the launch refuses what it refused before, in the same order.
	// What a stop refuses a run is said where the operator says what a stop
	// does, the Seat API's stopRunning and the applying page's seat pauses.
	// Both launches were read before at crewlet/crewlet#298's engine whose
	// launches hand the run row they open the code holds of their turn
	// (v0.1.1-0.20261029165728-4ee3f06a5fde), the run_sandbox launch read
	// again since at runReposRead. A turn holds each scratch repository of
	// the company's code host it names, so that the host's
	// expiry of an idle one leaves it be while the turn may still use it, and
	// a run_sandbox launch (Launch) and an agent-mode one (LaunchExecutor)
	// alike seal that set from its read until the row they open exists, so
	// that a hold another call of the turn takes meanwhile waits, then have
	// the row keep it, every later hold reaching the row before the request
	// it is taken for (launchHolds). The one refusal the holds add to a
	// launch is the row's, of a set past the most one run may carry
	// (sandbox.MaxCodeHolds), which only the repositories a turn names reach
	// and no document foresees; and a company the operator runs hosts no
	// code (its engineConfig may not set code), so its turns hold nothing.
	// An agent-mode launch also resolves its run before it opens the run's
	// session on the tool bridge, so that the session carries the run's
	// model for every bridged call to name (mcpbridge.Session.Model); it
	// refuses a node with no bridge before it assembles the run's
	// environment, in the words the bridge's empty endpoint was refused in,
	// and opens no session for a run executorRun refuses. Before that, both
	// launches were read at the engine that hands an agent-mode run the seat's
	// system prompt as its standing instructions, on the coding agent's
	// append channel, apart from the brief, names the run by the turn's task
	// (LaunchExecutor's System and Label), and frames a wider task after a
	// run_sandbox brief alone (Launch's Wider), an agent-mode brief being the
	// turn's ask already. None of it is a verdict: the head a launch runs
	// on, its coding agent, every other refusal before a box exists and the
	// sign-in that must reach the box are as at cliOnlyRead.
	bridgeStopRead = "v0.1.1-0.20261030230815-ab6d97e67b58"
	// runReposRead is crewlet/crewlet#298's engine whose run_sandbox launch
	// gives a run the repositories its call names on the company's own code
	// host, read at the head of its branch, where the launch was read again.
	// The launch takes the call's request where it took its brief, resolves
	// the repositories the call names last of its checks, after every
	// refusal a document foresees and before the turn's holds are sealed for
	// the row, and hands a run given some its credential on the code host,
	// the base its clones and pushes reach and git's configuration, in its
	// launch's own environment and never on the row (ADR-0063). A company
	// the operator runs hosts no code (its engineConfig may not set code), so
	// no call of its seats names a repository: the tool offers repos only
	// where the node serves the code host, refusing the argument elsewhere
	// before the launcher is asked, and the launch refuses a repository
	// where the company hosts no code. So none of it is a verdict: the head
	// a launch runs on, its coding agent, every other refusal before a box
	// exists and the sign-in that must reach the box are as they were where
	// both launches were read for the code holds (bridgeStopRead says how).
	// The variables that environment carries are the engine's own
	// (config.ReservedSandboxEnv), which Crewlet refuses in every env a run's
	// environment is assembled from, and so do the API's rules on a Seat's
	// spec.sandbox.env and a setup step's env, which test/crd holds to the
	// engine (TestReservedRunVariablesFollowTheEngine).
	runReposRead = "v0.1.1-0.20261030022059-2887e830ccb5"
	// refusalByModeRead is the engine whose remote-run refusal is worded by
	// the entry's auth mode (credentialRefusal): an api-key entry whose key
	// resolved to nothing is told to give the seat its own key, and an
	// inherit-env one the variables it forwards. sandboxCredentials refuses
	// the same runs as at cliOnlyRead. The operator renders no inherit-env,
	// and an APIKey provider always carries auth.apiKeySecretRef, so its
	// SandboxLoginNotPortable report still names the remedies a
	// Subscription or OpenCode head's refusal does.
	refusalByModeRead = "v0.1.1-0.20261024180236-65ae4e109ab1"
	// toolCredentialsGo is where the engine decides what a value that
	// resolves to nothing does to a tool server or a coding run.
	toolCredentialsGo = "internal/engine/toolcredentials.go"
	// renderCompanyGo is the render's NoProvisioner report.
	renderCompanyGo = "internal/render/company.go"
	// renderOptionalGo is the render's start values and its check of an
	// MCPServer's optionalKeys against the company.
	renderOptionalGo = "internal/render/optional.go"
	// configOptionalGo is the config reconciler's report of a start value
	// whose Secret holds nothing: left out under an optional key, and
	// otherwise an empty value that keeps a server or a run from starting.
	configOptionalGo = "internal/controller/config_optional.go"
	// seatTypesGo is the Seat API.
	seatTypesGo = "api/v1alpha1/seat_types.go"
	// mcpServerTypesGo is the MCPServer API.
	mcpServerTypesGo = "api/v1alpha1/mcpserver_types.go"
	// toolServersMd and cliAgentsMd are the guides that state what a tool
	// server and a coding run are started with.
	toolServersMd = "docs/guides/tool-servers.md"
	cliAgentsMd   = "docs/guides/cli-agents.md"
	// engineMCPGo, mcpConfigGo, orgGo and fileRulesGo are where the engine
	// decides which instances of a tool server it starts and with which
	// values, and which spellings of a header it refuses.
	engineMCPGo = "internal/engine/mcp.go"
	mcpConfigGo = "internal/config/mcp.go"
	orgGo       = "internal/org/organization.go"
	fileRulesGo = "internal/config/filerules.go"

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

	// offersSandboxRead is crewlet#271's engine, whose runner was first read
	// for which seats it offers run_sandbox: never one whose executor runs in
	// agent mode, which the render reports no code work for.
	offersSandboxRead = "v0.1.1-0.20261020054845-2b4e0d71bce5"

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

	// inboxLiftRead is crewlet/crewlet#298's engine that lifts a person's
	// inbox out of the tracker (crewlet/crewlet@1a61828c7), read at the head
	// of its branch, where the inbox relay a data node arms was read again
	// against the windows. armInboxRelay hands the relay every domain whose
	// applier announces a person's movements (inboxSources) and the check of
	// which of them this node's copy holds out of service
	// ((*localEstate).outOfService), where it handed it the tracker's copy's
	// alone, and the relay delivers the movements as the inbox's own, of any
	// domain (deliverInbox). Neither is a request on the boot: the relay
	// still serves every domain on its node's one subject, registered once
	// through (*inboxRelay).serve, whose digest holds, so a data pod's boot
	// counts it as the one flush it was, and a node without data registers
	// none.
	inboxLiftRead = "v0.1.1-0.20261027063930-66e8ad593665"

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
// platform provider APIs' over a CLI's sign-in, and the tool-server API's
// refusal of a literal value), the render's copy of their pattern, and the
// guide and the page that state the rule of a literal value.
var credentialNames = []string{
	"api/v1alpha1/llmprovider_types.go", engineClassGo, mcpServerTypesGo, "api/v1alpha1/unit_types.go",
	codeWorkGo, "docs/guides/tool-servers.md", "docs/concepts/credentials.md",
}

// agentLogin is every operator file that copies how an Agent-mode run's CLI
// signs in inside the remote sandbox, and is refused when nothing does: the
// LLMProvider API's rule on a provider of mode Agent, the guide that states
// it, and the CRD suite's reading of the engine's verdicts on it (its
// holdVerdicts). The renderer's code-work verdict copies the same functions
// for a run_sandbox launch.
var agentLogin = []string{
	"api/v1alpha1/llmprovider_types.go", "docs/guides/cli-agents.md", "test/crd/cliagent_engine_test.go",
}

// codingBox is every operator page that states what a coding box carries of
// the provider its run is on, its token, key or credential-named variables of
// its env and never its credential bundle:
// the credentials guide's "What a coding box carries" and the credentials
// concept page.
var codingBox = []string{"docs/guides/credentials.md", "docs/concepts/credentials.md"}

// agentModeCodeWork is every operator file that copies which seats Crewlet
// offers run_sandbox, and so which have code work of their own: the render's
// code-work verdicts (CodeWorkOf), which leave an agent-mode seat out, and
// agentModeExecutor, which finds the executor they leave it out by; the Seat
// API, whose spec.llm.sandbox, spec.sandbox.codingAgent and status say what
// is read for such a seat, and the condition and reason docs beside it; and
// the guide that states the rule (Code work) and the troubleshooting row that
// gives CodingAgentMismatch's remedy for either kind of seat.
var agentModeCodeWork = []string{
	codeWorkGo, executorGo, seatTypesGo, "api/v1alpha1/common_types.go",
	"docs/guides/cli-agents.md", troubleshootingMd,
}

// bootWindows is every operator file that copies the bounds the engine puts
// on its own boot: the controller's data pod and seat pod startup windows,
// which add them up, the test that pins their sums, the EngineClass API's
// floor on data.startupSeconds, and the pages that quote them.
var bootWindows = []string{
	"internal/controller/company_workload.go", "internal/controller/company_pools.go",
	"internal/controller/company_workload_test.go", engineClassGo,
	"docs/concepts/runtime.md", troubleshootingMd,
}

// mirrored is every engine declaration the operator's run-time verdicts copy
// a decision of, and no other: a declaration changed here sends somebody to
// read it, so one that copies nothing would only cost a read. What the engine
// shows without a run is held by a contract test instead, never here: the
// head of a seat's chain (TestContractChainlessSeatRunsOnTheFirstProviderAlone),
// the CodingAgentMismatch it refuses on a write, over a seat's sandbox chain
// or an agent-mode seat's coding agent (TestContractCodeWorkOnWrite),
// the sign-in a provider's settings give it (test/crd's
// TestCLIAgentSignInFollowsTheEngine), the deployment its validation reads a
// Jira or Confluence address on (TestContractAtlassianDeploymentIsTheEngines),
// and the lists TestCodeWorkFollowsTheEngineSource,
// TestAtlassianDeploymentFollowsTheEngineSource and
// TestCredentialNameFollowsTheEngineSource read.
//
// Two decide what a value that resolves to nothing costs a seat:
// runEnvironment, which lays a run's environment and leaves out a key the
// layer that wins declares optional, and refGap.hold, which waits for a value
// a provisioning pass mints for the seat whatever the declaration says. The
// code-work verdict and the render's NoProvisioner report copy them; what a
// tool server is started with is shown by a running engine instead
// (TestContractAnOptionalKeyIsLeftOut). Which values each instance of a tool
// server is started with, which the render's StartValue.Started copies and the
// config reconciler reports an empty value by, is decided by five more:
// startSharedServers, seatSpecs and Grants (which instances run, the company's
// and the seats'), inheritMCPEnv (a unit's block under its members') and
// sandboxMCP (a coding run's box). And validateHeaderSpellings, with
// spelledUnlike, refuses a header a seat spells unlike its unit, which the
// render refuses too (headerSpellings).
//
// Most of them are the launch's own code; one is the runner's, which seats it
// offers run_sandbox at all ((*Runner).offersSandbox). One is a sandbox
// backend's: what an E2B box is created from, (*E2BProvider).Create, since the
// launch hands a run's credential files to whatever box it is placed in and
// each backend decides what to do with them. Two are config's write-time
// twins of a launch decision, read because the launch's own is spread through
// a registry or a manager: (*Company).sandboxProvider, which crewlet's
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
// the history answerer, the inbox relay and the presence answerer each
// register one subject in the function their step calls (eventfan's Serve,
// (*inboxRelay).serve, presence's (*Hub).Start); the estate service
// registers each of its lanes in turn (estate's Serve and lanes); and every
// registration is one flush ((*Queue).Serve and flush). So a registration
// added below a step, whose own code need not change for it, sends somebody
// to the windows too. And the listing the windows count, the join's of the
// fleet's positions, is read down to the certified listing it is, and that
// listing to the functions that bound its waits: a class of the positions
// register is one walk of the register ((*FleetStore).eachPositionKey), a
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
		file: sandboxGo, name: "(*launcher).Launch",
		against: []string{codeWorkGo, renderCompanyGo, configOptionalGo},
		decides: "a run_sandbox launch runs the head of the seat's sandbox phase with role.sandbox.coding_agent or " +
			"the catalogue's default, and refuses before a box exists what newCodingRun and sandboxCredentials " +
			"refuse, and a run whose environment holds a value that resolves to nothing under a key nothing " +
			"declares optional (runEnvironment's gap)",
		digest: "8c4ebcadecdfbb3d9c29f765184ac06aa4ea1079f8c9cb3e6a8cf5708fb073e8",
		read:   runReposRead,
	},
	{
		file: sandboxGo, name: "buildSandbox",
		against: []string{codeWorkGo},
		decides: "the catalogue a launch reads is built from providers.sandbox, with its default_coding_agent as the " +
			"default coding agent and its setup steps as the setup every run is given, and a company with no " +
			"sandbox has none, so offers no seat run_sandbox",
		digest: "73a2a28ba0a87e759b935cb74009831d5c1c1e89a2d9c1eb008e574287b3f687",
		read:   firstRead,
	},
	{
		file: "internal/sandbox/manager.go", name: "NewManager",
		against: []string{codeWorkGo},
		decides: "a catalogue that names no default coding agent runs DefaultCodingAgent, Claude Code, whose value " +
			"TestCodeWorkFollowsTheEngineSource holds",
		digest: "124cf3ef185dfc577ccf34d74b499d71c99dfb42414c5fecf4cd1802f6a38d89",
		read:   firstRead,
	},
	{
		file: sandboxGo, name: "newCodingRun",
		against: append(append([]string{codeWorkGo}, agentLogin...), codingBox...),
		decides: "a head another CLI's coding agent cannot run (CodingAgentMismatch), and a head's run, a " +
			"run_sandbox launch or an agent-mode run alike, carrying its model as the head's text calls pass it " +
			"and its provider's own sign-in (entrySignIn) under what the seat's environment sets, and its login, " +
			"the CLI's credential files or the document a CLI that keeps its login in a database of its own " +
			"exported (OpenCode 2's), as the run's credential files, whatever box the run is placed in: what the " +
			"box does with them is its backend's to decide ((*E2BProvider).Create)",
		digest: "7b0a60f2c5cfd8f40aee57595c11ac54033f68faabe9b35af836ad6a76b02b48",
		read:   cliOnlyRead,
	},
	{
		file: e2bGo, name: "(*E2BProvider).Create",
		against: append(append([]string{codeWorkGo}, agentLogin...), codingBox...),
		decides: "a box in the Company's E2B sandbox is created from its template, the run's timeout and the run's " +
			"environment alone, never from the run's credential files, so a head's login (its credential " +
			"files, or the document OpenCode 2's export printed) never reaches it, and only a variable of the " +
			"run's environment signs its CLI in there",
		digest: "65305f3ecc87afac5544b7bddb91062389e5a0e0b76a63481f6f191e778e2a03",
		read:   e2bCreateRead,
	},
	{
		file: sandboxGo, name: "entrySignIn",
		against: append(append([]string{codeWorkGo}, agentLogin...), codingBox...),
		decides: "a ClaudeCode run whose seat's environment signs ClaudeCode in (codingagent.ClaudeCodeCredentialEnv, " +
			"which TestCodeWorkFollowsTheEngineSource holds) is handed none of its provider's sign-in, and every " +
			"other run its provider's own (SandboxEnv), beneath the seat's environment",
		digest: "d6ee05cb147a156a49123b6108de6bc49f9cb42d98e8f0eeca52749fd3797b2f",
		read:   cliOnlyRead,
	},
	{
		file: sandboxGo, name: "signsIn",
		against: []string{codeWorkGo},
		decides: "a variable of the seat's environment signs ClaudeCode in when it holds a value",
		digest:  "d67892b7c02c1053a5a38c588fe26d93a39b9525b01879a0d6dc3ef923280798",
		read:    firstRead,
	},
	{
		file: sandboxGo, name: "sandboxCredentials",
		against: append([]string{codeWorkGo}, agentLogin...),
		decides: "a head whose login cannot follow the run into a remote sandbox is refused unless the run's " +
			"environment signs its CLI in (its token or key variable, for ClaudeCode any variable that signs it " +
			"in, and for an env sign-in CLI any credential-named one): a run_sandbox launch " +
			"(SandboxLoginNotPortable), and at phase Execute an agent-mode run, which the LLMProvider API refuses " +
			"on a Subscription login no variable carries",
		digest: "130c50af56ebe42106c847fb1e06cbe9886d5e8a2ddbb19586e93d3ca63beebc",
		read:   refusalByModeRead,
	},
	{
		file: sandboxGo, name: "(*Engine).runEnvironment",
		against: append([]string{
			codeWorkGo, renderCompanyGo, renderOptionalGo, configOptionalGo, seatTypesGo, companyTypesGo, cliAgentsMd,
		}, codingBox...),
		decides: "a run's environment is the env of the catalogue's and the seat's setup steps, then the seat's " +
			"role.sandbox.env, each later layer winning a name, and a value that resolves to nothing is left out " +
			"where the optional_keys beside the value that wins lists its key (a setup file's path likewise), " +
			"and is the run's gap otherwise",
		digest: "413112e3769c5d2a4a1a84d84bf6bb3e7f7439acabbe210cb3c9c818b9452189",
		read:   optionalKeysRead,
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
		against: []string{renderCompanyGo, codeWorkGo, mcpServerTypesGo, seatTypesGo},
		decides: "a value a provisioning pass mints for the seat holds a tool server or a run back whatever " +
			"optional_keys says, and a declared key's value that nothing mints is left out",
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
		decides: "a coding run's box is given every server its seat's sandbox scopes, shared or not, with the seat's " +
			"block for it",
		digest: "7cd2c2e82ba70b3327284d26e450b09b34549baacc50b22e8927405c3e9fc0a6",
		read:   optionalKeysRead,
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
		against: append(append([]string{codeWorkGo}, agentLogin...), codingBox...),
		decides: "the variables that sign a CLI in inside a box: its token and key variables, and for an env " +
			"sign-in CLI the credential-named variables of its env",
		digest: "091501e8498192be36fc8619232c358a2276c32bae8f412e7197fcc0a8b89667",
		read:   firstRead,
	},
	{
		file: doctorGo, name: "(*Provider).SandboxEnv",
		against: append(append([]string{codeWorkGo}, agentLogin...), codingBox...),
		decides: "a provider's own sign-in follows the run into the box when it is one of those variables",
		digest:  "f81f2bc4b7fca996eaac300c1ab530d2e357500bd4b76145551412788cc9ddb9",
		read:    firstRead,
	},
	{
		file: companyGo, name: "(*Company).sandboxProvider",
		against: []string{codeWorkGo},
		decides: "config's twin of the head the phase registry gives a seat's code work, which crewlet's " +
			"TestBothPathsToTheSandboxEntryAgree holds to it: the first configured provider of its llm_sandbox, " +
			"else of its llm, else the company's fallback",
		digest: "e0d59baf916e2892840471b03a0461b7d2e3149c76ab07975606b6f8bec57f04",
		read:   firstRead,
	},
	{
		file: companyGo, name: "(*Company).codingAgent",
		against: []string{codeWorkGo},
		decides: "config's twin, on a write, of the coding agent a launch runs, by which it refuses " +
			"CodingAgentMismatch: the seat's own coding_agent, else the catalogue's default, else Claude Code; " +
			"nothing in crewlet holds it to the launch's (Launch, NewManager)",
		digest: "ea194ad5e34803abb6f736efecb3fea988605855d66d9e7a5dfc2daecf3e9873",
		read:   firstRead,
	},
	{
		file: "internal/agent/runner/phases.go", name: "(*Runner).offersSandbox",
		against: agentModeCodeWork,
		decides: "a seat whose executor runs in agent mode is never offered run_sandbox, so it has no code work " +
			"of its own: what it codes, it codes in its executor's run",
		digest: "7dd4fb18d5bc2aaf097938b4b13a600c5a631c3e43d1f0dfd3cc9283ba893f77",
		read:   offersSandboxRead,
	},
	{
		file: agentRunGo, name: "(*Company).executorRuntime",
		against: []string{executorGo, headsGo},
		decides: "a seat's executor runs as an agentic run of a CLI when the head of its executor's chain alone is a " +
			"provider in agent mode",
		digest: "b03aa59b6706b0dcad0a4c2fb167e5cf22243dffa6a2034ddb6b10a3a640a8dd",
		read:   firstRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).LaunchExecutor",
		against: append([]string{executorGo, renderCompanyGo, configOptionalGo}, agentLogin...),
		decides: "an agent-mode turn on a node that serves no tool bridge is refused, never run on the engine's own " +
			"tool loop (AgentModeBridgeUnsupported, NotExposed), and so is one whose CLI login cannot follow it " +
			"into the remote sandbox (sandboxCredentials at phase Execute); a run whose environment holds a " +
			"value that resolves to nothing starts without it",
		digest: "4855c9c523e090c8f93dcac8905db9b1b3638955d5569fa7d5ad73337c1397e9",
		read:   bridgeStopRead,
	},
	{
		file: agentRunGo, name: "(*agentLauncher).executorRun",
		against: append([]string{executorGo}, agentLogin...),
		decides: "an agent-mode run is the executor itself: it runs on the head of the seat's executor chain " +
			"(phase Execute), never on its llm_sandbox, so that head's sign-in is the one that must reach the box",
		digest: "49679ff36a054ab3dea460a4a5c850c4fafea190b74e37c3a6e72637b25df448",
		read:   firstRead,
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
		against: []string{executorGo},
		decides: "a node builds no tool bridge without a base, which its boot reads from CREWLET_MCP_BRIDGE_URL " +
			"through ParseBoxURL, refusing to start on a value that is set and unusable",
		digest: "7ccbfd8407f92b705d9a5ff6382016aa7f1360b8c214a438fd9613709ae1f38f",
		read:   codeHostRead,
	},
	{
		file: mcpbridgeGo, name: "Configured",
		against: []string{executorGo, bootstrapGo},
		decides: "a node has a tool bridge only when CREWLET_MCP_BRIDGE_URL is set to more than spaces, which the " +
			"operator gives the data pods alone",
		digest: "8cb9750e7647eb66a8474b39608e911a2975c99bbc36c1362e88ee30431b6950",
		read:   bootBindRead,
	},
	{
		file: bootGo, name: "PublicListener",
		against: []string{bootstrapGo},
		decides: "a node without the ingress role binds api.public only where it serves its tool bridge, so a seat " +
			"pod, given no bridge address, is given no api.public block",
		digest: "a83eb8f588e04b36bb5f222173246f63bb564bb521f5a0d9eddf34ba6a1b4b11",
		read:   bootBindRead,
	},
	{
		file: bootGo, name: "servesBridge",
		against: []string{bootstrapGo, executorGo},
		decides: "a node without the ingress role serves a tool bridge only when it runs seats and has a bridge address",
		digest:  "53129c12fa32a9938afa3d793112e1c700d3ae679f3683f9957cb522b5effa8c",
		read:    bootBindRead,
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
		digest: "c02b646fdb22072ec1ae077fe14ab33f4ec40eccafb45bde4965fbd546f11171",
		read:   codeHostRead,
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
		decides: "a key class of the positions register is one filtered walk of the register, each of its entries " +
			"handed on with the instant the broker stored it, which the entry already carries",
		digest: "ff039de2ad4de929638d75f4ed4592d3450b09c2fb6308c135615902b7ef711d",
		read:   clockSkewRead,
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
		digest:  "390b726e7489f221da2395b93fcac08ac91d63f213772b0afc1dda556b6a884d",
		read:    bootBoundsRead,
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
		digest:  "a9ece00e3267829897fa33ad1a020dae4cc28592402905630304cc9f44dd97a6",
		read:    bootBoundsRead,
	},
	{
		file: "internal/engine/inboxrelay.go", name: "(*Engine).armInboxRelay",
		against: bootWindows,
		decides: "a data node registers its inbox relay, one flush",
		digest:  "f51056d70715262b7ad2a9a4d489552726857565b25405d5a28acd9cca7a43c6",
		read:    inboxLiftRead,
	},
	{
		file: "internal/engine/inboxrelay.go", name: "(*inboxRelay).serve",
		against: bootWindows,
		decides: "the inbox relay a data node arms (armInboxRelay) registers one subject, its node's own, through " +
			"the queue's Serve",
		digest: "cce917b34fdcc2b5c6f26f1b85ee9ace995b3a0b43dd159137a001a01bc7745a",
		read:   gitLaneRead,
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
		decides: "the join lists the fleet's positions and reads every log's trim floor at once, then reads each " +
			"log's stats and judges its floor from that one read",
		digest: "8df5a23499b2d22cd15b742e83cf3f49478392e30008eed1d5d35ff129b5ed4f",
		read:   codeHostRead,
	},
	{
		file: stateLogGo, name: "(*stateLog).start",
		against: bootWindows,
		decides: "a log's start reads its stats, attaches its consumer and verifies its checkpoint",
		digest:  "62f930ac3079f85e8704ca3ff2ff4ea7b8771af963dfd8e4c8ecaa3c723b4094",
		read:    codeHostRead,
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
		digest: "a8fb9d8a6c8a3b24aa93f81c43e81e770a9d1a15180766f72fdd183bdf1972ea",
		read:   gitLaneRead,
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
		digest: "73aa622c3dc1403f8811d251f7ac819722b3e785ee6da02fb68b7358a4864839",
		read:   gitLaneRead,
	},
	{
		file: "internal/queue/jetstream/attach.go", name: "(*Queue).attach",
		against: bootWindows,
		decides: "attaching a durable group looks up, creates and reads back its consumer, then looks it up and " +
			"reads it back again",
		digest: "b6a9e9419ad2f3f2f6ddbfd14c2bb8d08c8f828f4661c5d1118b53521a56cef5",
		read:   codeHostRead,
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
		digest:  "366d004324711b563b2016377bf7d1523f91474683b542a1ee79fbf9462fb73a",
		read:    codeHostRead,
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
		digest: "d52d5def68b8d33f4b3f0e48d94386f5632a0f24fd0c5e470bd98032c32ce6c0",
		read:   bootBoundsRead,
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
	read := mirror{file: file, name: "pick", against: []string{codeWorkGo, executorGo}, decides: decides,
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
