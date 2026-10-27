package render

import (
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// What an engine pod looks like from the inside. The runtime reconciler
// mounts the volume and opens the ports the documents name, so they are
// constants the two share rather than strings each spells.
const (
	// DataDir is where a data pod's volume is mounted, and a seat pod's
	// scratch emptyDir. A data pod keeps its store, its snapshots and the
	// event stream under it; a seat pod its scratch store.
	DataDir = "/var/lib/crewlet"
	// SandboxDir is where every engine pod's sandboxes emptyDir is mounted,
	// a data pod's and a seat pod's alike: where Crewlet makes the boxes of
	// the executor runs it places on the pod itself (its direct cell,
	// providers.sandbox.local.state_dir), a directory each, holding the
	// run's home, its checkout and whatever it installs and builds. It is a
	// volume of its own, so that the boxes have a limit of their own
	// (DefaultSandboxSizeLimit) that bounds nothing else and that nothing
	// else shares, and it is under neither of the obvious places:
	//   - DataDir: on a data pod that is the company's estate, three
	//     quarters of it reserved for the event stream (store_max_bytes)
	//     and the rest the store's and its snapshots', so a run that filled
	//     it would stop the store and the broker, and the whole company with
	//     them. A box is scratch besides: its run is a process of the pod
	//     and ends with it, so a volume that outlived the pod would keep
	//     boxes nothing runs in.
	//   - HOME, under which Crewlet keeps its boxes by default
	//     (~/.crewlet/sandboxes): a seat pod's HOME is on its scratch
	//     volume, whose limit is sized for the scratch store and the
	//     processes the engine runs, and a box's checkout would evict the
	//     pod at it.
	SandboxDir = "/var/lib/crewlet-sandboxes"
	// APIPort is the port of an engine's private HTTP listener, api.port. On
	// a data pod, which has the ingress role, it carries the API, the
	// dashboard and the probes, and none of the routes outside parties call,
	// which are PublicPort's; on a seat pod, which does not have the role,
	// only the probes (/health and /ready). It is 8080. Crewlet's own
	// default, and the port its image documents with EXPOSE, is 8000
	// (config.DefaultAPIPort), but nothing reads an image's EXPOSE once a pod
	// names its ports: the port is the operator's, and every Service, probe,
	// NetworkPolicy and address it publishes is built on this constant.
	APIPort = 8080
	// PublicPort is the port of a data pod's public listener, api.public:
	// the routes outside parties call without an operator credential, and
	// nothing else — the vendors' webhooks and app landings (/webhooks/),
	// and a coding run's per-run telemetry (/otlp/{token}) and tool bridge
	// (/mcp/{token}), each admitted by a provider signature or a
	// token signed for its one run. Crewlet decides which routes are public
	// from one list of its own, and answers every other route there as a
	// path nothing serves, so the address published to the internet cannot
	// reach the admin API whatever path a route or a proxy forwards.
	//
	// Every data pod opens it, whether or not its class routes anything to
	// it: which routes the engine serves where is then a fact about the pod
	// that the class's exposure cannot change, so the API's route never
	// serves /webhooks/, and a pod nothing routes to serves its public
	// routes to nobody, which is what the class's NetworkPolicy and the
	// absence of a route already make of them. Crewlet requires the
	// listener's address beside it (api.public.external_url, which
	// BootstrapInput.PublicURL is), and reads it only at start, so a change
	// of where the listener is reached restarts the engine.
	//
	// It is 8081: plain HTTP like APIPort, beside it, and none of the ports
	// an engine pod binds otherwise (APIPort, LeafPort, the embedded
	// broker's cluster route port 6222, which no pod opens in v1alpha1).
	// Crewlet's validator refuses a Tier A whose listeners share a port, and
	// the contract tests run it over every document rendered here.
	PublicPort = 8081
	// ArtifactsPort is the port of a data pod's artifact listener,
	// api.artifacts, which serves artifacts and nothing else. It is opened
	// only for a Company with an artifact origin (ADR-0007).
	ArtifactsPort = 8082
	// LeafPort is the port of a data pod's leaf listener, which the seat
	// pods join the company's event stream through. It is open only when
	// the class's seatPools.policy is Allowed. Like every port of
	// Crewlet's embedded broker it accepts any connection that reaches it,
	// with no authentication and no TLS, so only a NetworkPolicy keeps
	// another tenant's pods off it.
	LeafPort = 7422
)

// The API tokens of an engine. Crewlet records every write made with a token
// under its login, token:<id> (a revision's author, a pause's), and the id is
// also the key that holds the token in the company's API tokens Secret.
const (
	// TokenOperator is the operator's own token. It is never handed out, so
	// revoking a consumer's token never locks the operator out.
	TokenOperator = "operator"
	// TokenAccess is the token for the company's consumers, published in
	// the Secret the Company's status.apiAccessSecretRef names.
	TokenAccess = "access"
)

// The environment variables a Tier A refers to. Every credential of Tier A is
// written into the document itself instead, which is kept in a Secret; see
// Bootstrap for why none is an environment variable.
const (
	// EnvNodeID is the pod's name, which the Downward API gives it: the
	// node's identity, on every engine pod.
	EnvNodeID = "CREWLET_NODE_ID"
	// EnvSeat is the handle of the one seat a PerSeat pool's pod runs. Its
	// Tier A carries it in node.labels as a reference, so the pool's pods
	// share one document and each StatefulSet gives its pods their seat.
	EnvSeat = "CREWLET_SEAT"
)

// BootstrapInput is what a data pod's Tier A is rendered from.
type BootstrapInput struct {
	Company *v1alpha1.Company
	Class   *v1alpha1.EngineClass
	// Keyring is the company's keyring, read from its keyring Secret.
	Keyring Keyring
	// Tokens are the engine's API tokens, read from the company's API
	// tokens Secret.
	Tokens APITokens
	// Objects are the keys of the class's S3 bucket, or nil to have the
	// engine use the pod's own credentials (a workload identity). Read only
	// when the class's objects.backend is S3.
	Objects *S3Credentials
	// VolumeSize is the capacity the data pod's volume claim requests: the
	// data StatefulSet's own claim template once it exists, which a later
	// change of the class does not move, and the class's data.storage.size
	// before.
	VolumeSize resource.Quantity

	// ExternalURL is where a browser reaches the engine, Crewlet's
	// api.external_url: the origin a person's write must come from, the
	// scope and Secure flag of the session cookie, and the base of every
	// link a person follows, an invitation's included. The Company's
	// status.endpoints.dashboardBaseURL.
	ExternalURL string
	// PublicURL is where the data pod's public listener (PublicPort) is
	// reached, Crewlet's api.public.external_url: the base of every webhook
	// address, app manifest and sandbox endpoint the engine builds.
	PublicURL string
	// AllowedOrigins are the browser origins the engine is reached on
	// beside ExternalURL's own, Crewlet's api.auth.allowed_origins.
	AllowedOrigins []string
	// ArtifactsURL is Crewlet's api.artifacts.external_url, the Company's
	// status.endpoints.artifactsBaseURL. Empty opens no artifact listener.
	ArtifactsURL string
}

// ErrNoAddress is a data pod's Tier A asked for without an address the engine
// is reached at. Crewlet refuses to start a node that serves the API without
// api.external_url, or that opens its public listener without
// api.public.external_url, so the render refuses it first.
var ErrNoAddress = errors.New("a data pod's crewlet.yaml needs the addresses the engine is reached at")

// ErrSignInNotAcknowledged is a data pod's Tier A asked for with a class that
// lets people sign in with a password alone (spec.signIn.secondFactor
// Optional), an address the engine is reached at that is not on loopback, and
// no acknowledgement of the two together (spec.signIn.acceptInsecure).
// Crewlet refuses to start a node that serves the API so
// (config.APIAuth.validatePasswords), so the render refuses it first, and the
// caller holds the pods back on it rather than start one that crash-loops.
var ErrSignInNotAcknowledged = errors.New("password-only sign-in off loopback needs the class's acknowledgement")

// ErrKeyringInvalid is a Tier A asked for with a keyring the engine could not
// use: no key, an active key it does not hold, an id that is not a valid key
// id, or material that is not base64 of 32 bytes. The keyring is a Secret's,
// which the operator never rewrites, so the caller reports it and holds the
// pods back rather than start an engine that would refuse it; it is the only
// failure of Bootstrap that is the keyring's, so it is told apart from every
// other by this error alone.
var ErrKeyringInvalid = errors.New("the keyring is not one the engine can use")

// Keyring is the engine's secret-store keyring: the keys it can open, and
// the one it seals new writes with. Crewlet decrypts with every key and
// encrypts with the active one, which is what lets a key be rotated without
// a restart.
type Keyring struct {
	ActiveKeyID string
	// Keys maps each key's id to its material: base64 of 32 random bytes,
	// as `crewlet secrets keygen` prints it.
	Keys map[string]string
}

// APITokens are the engine's two bearer tokens (TokenOperator, TokenAccess).
type APITokens struct {
	Operator, Access string
}

// S3Credentials are the keys the engine reaches an S3 bucket with.
type S3Credentials struct {
	AccessKeyID, SecretAccessKey string
}

// BootstrapDocument is a rendered Tier A.
type BootstrapDocument struct {
	// YAML is the crewlet.yaml of the data pods. It holds the keyring, the
	// API tokens and the object store's keys, so it is kept in a Secret and
	// never anywhere else.
	YAML []byte
	// Hash is the hex-encoded SHA-256 of YAML.
	Hash string
	// Env is every environment variable YAML refers to: exactly what the
	// data pod's engine container must be given for the references to
	// resolve. It carries no credential; it is the pod's own name.
	Env []corev1.EnvVar
}

type bootstrapDoc struct {
	Node         nodeDoc         `json:"node"`
	API          *apiDoc         `json:"api,omitempty"`
	Store        storeDoc        `json:"store"`
	Stream       streamDoc       `json:"stream"`
	Coordination coordinationDoc `json:"coordination"`
	Secrets      keyringDoc      `json:"secrets"`
	Retention    retentionDoc    `json:"retention,omitzero"`
}

type nodeDoc struct {
	ID            string            `json:"id"`
	Roles         []string          `json:"roles"`
	Labels        map[string]string `json:"labels"`
	MaxConcurrent int32             `json:"max_concurrent,omitempty"`
}

type apiDoc struct {
	Host           string           `json:"host"`
	Port           int              `json:"port"`
	ExternalURL    string           `json:"external_url,omitempty"`
	TrustedProxies []string         `json:"trusted_proxies,omitempty"`
	Public         *apiPublicDoc    `json:"public,omitempty"`
	Artifacts      *apiArtifactsDoc `json:"artifacts,omitempty"`
	Auth           *apiAuthDoc      `json:"auth,omitempty"`
}

// apiPublicDoc is api.public. Its host is left to the engine's default,
// every interface, which a pod needs to be reached at its own address.
type apiPublicDoc struct {
	Port        int    `json:"port"`
	ExternalURL string `json:"external_url"`
}

// apiArtifactsDoc is api.artifacts; its host is the engine's default.
type apiArtifactsDoc struct {
	Port        int    `json:"port"`
	ExternalURL string `json:"external_url"`
}

// artifactsOf is api.artifacts for an artifact origin at address, or nil.
func artifactsOf(address string) *apiArtifactsDoc {
	if address == "" {
		return nil
	}
	return &apiArtifactsDoc{Port: ArtifactsPort, ExternalURL: address}
}

// ArtifactsOpen reports whether a data pod opens the artifact listener.
func ArtifactsOpen(address string) bool { return artifactsOf(address) != nil }

// apiAuthDoc is api.auth. TOTP is left out for a class that requires a
// second factor, which is Crewlet's own reading of an unset totp: written
// out, it would change the crewlet.yaml of every data pod whose class says
// nothing about sign-in, and so restart each of them on the operator's
// upgrade, for no change in what any of them does. AcceptInsecure is left out
// unless it is set, as Crewlet leaves it.
type apiAuthDoc struct {
	Admin          string        `json:"admin"`
	Tokens         []apiTokenDoc `json:"tokens"`
	CompanyWriters []string      `json:"company_writers,omitempty"`
	AllowedOrigins []string      `json:"allowed_origins,omitempty"`
	TOTP           string        `json:"totp,omitempty"`
	AcceptInsecure bool          `json:"accept_insecure,omitempty"`
}

type apiTokenDoc struct {
	ID     string   `json:"id"`
	Token  string   `json:"token"`
	Grants []string `json:"grants"`
}

// adminServe is api.auth.admin on a data pod: the node grants admin to those
// who hold it. Crewlet requires the posture on every node that serves the
// API and refuses a token granted admin on a node that refuses it. The one
// data pod of a v1alpha1 company is its only API node, and the node the
// operator administers the company through, so it serves admin.
const adminServe = "serve"

// grantAdmin is Crewlet's admin grant. Both tokens hold it, and it is the
// least the operator's own can: every route the operator calls is admin's in
// Crewlet's authority table (the company document, the secret store, the
// fleet, the integrations, and the pause tools for a token bound to no
// seat), and admin cannot be split. The token the Company publishes holds it
// so that a platform can do with it what the operator does, and put a
// company's first person on their seat, which a person's own machine token
// never can.
const grantAdmin = "admin"

type storeDoc struct {
	Path    string     `json:"path"`
	Scratch bool       `json:"scratch,omitempty"`
	Objects objectsDoc `json:"objects"`
}

type objectsDoc struct {
	Backend string `json:"backend"`
	S3      *s3Doc `json:"s3,omitempty"`
}

type s3Doc struct {
	Endpoint        string `json:"endpoint,omitempty"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	PathStyle       bool   `json:"path_style,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
}

type streamDoc struct {
	Type          string   `json:"type"`
	StoreDir      string   `json:"store_dir,omitempty"`
	StoreMaxBytes int64    `json:"store_max_bytes,omitempty"`
	Replicas      int      `json:"replicas"`
	Leaf          *leafDoc `json:"leaf,omitempty"`
}

// leafDoc is stream.leaf: a member's listener (port) or a leaf's members
// (urls), never both.
type leafDoc struct {
	Port int      `json:"port,omitempty"`
	URLs []string `json:"urls,omitempty"`
}

// streamReplicas is how many copies of each stream the broker keeps: one, on
// the one data pod v1alpha1 runs. A seat pod names the same count, because a
// leaf that uses a stream first creates it with its own count on the
// members, and a node configured for more replicas than an existing stream
// has refuses to start.
const streamReplicas = 1

// The engine's event stream is given streamShareNumerator /
// streamShareDenominator of the data volume: three quarters, the share
// Crewlet's embedded broker takes of the free space it finds when nothing
// sets one. The rest is the store and its snapshots.
const (
	streamShareNumerator   = 3
	streamShareDenominator = 4
)

// storeMaxBytesCeiling is the largest stream.store_max_bytes Crewlet starts
// on (config.StoreMaxBytesCeiling, 64 TiB), a guard against a unit mistake
// rather than a policy: it is more than an order of magnitude above what the
// engine's own logs can be configured to hold. TestContractStreamCeiling
// holds it to the engine's.
const storeMaxBytesCeiling int64 = 64 << 40

// streamShare is the stream's share of a data volume of the given size,
// stream.store_max_bytes: three quarters of it, and no more than
// storeMaxBytesCeiling. A volume over about 85 TiB is not wrong, and the
// engine refuses to start on a larger share, so what the share would take
// past the ceiling is left to the store and its snapshots.
//
// The size is compared before its Value is read: a quantity at or past the
// largest int64 has no exact one (Value saturates at it, wraps below zero or
// reads 0, by how the quantity is written), and the API admits such a size.
func streamShare(volume resource.Quantity) int64 {
	if volume.CmpInt64(math.MaxInt64) >= 0 {
		return storeMaxBytesCeiling
	}
	return min(volume.Value()/streamShareDenominator*streamShareNumerator, storeMaxBytesCeiling)
}

type coordinationDoc struct {
	Type string `json:"type"`
}

type keyringDoc struct {
	ActiveKeyID string         `json:"active_key_id"`
	Keys        []secretKeyDoc `json:"keys"`
}

type secretKeyDoc struct {
	ID       string `json:"id"`
	Material string `json:"material"`
}

type retentionDoc struct {
	BackupOwner string `json:"backup_owner,omitempty"`
}

// keyIDPattern is Crewlet's rule for a keyring key's id, which is stamped
// into every envelope the key seals; it is also the rule for a Secret's key.
var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// defaultS3Prefix is the API's default for spec.objects.s3.prefix.
const defaultS3Prefix = "{{namespace}}/{{name}}/"

// keyringKeyBytes is the size of an AES-256 key, the only size Crewlet's
// keyring accepts.
const keyringKeyBytes = 32

// Bootstrap renders the Tier A of a company's data pods in the v1alpha1
// topology: one data pod that holds the estate, serves the API, runs the
// workers and runs the seats placed in the pool data. It serves the API on
// APIPort, the routes outside parties call on PublicPort alone, and artifacts
// on ArtifactsPort where the Company has an artifact origin. It serves
// admin, and both API tokens hold admin (see grantAdmin); every other
// credential is a person's, which Crewlet's own identity directory holds.
//
// The document is shaped for a fleet from the first start, so that seat pods
// can join without migrating anything: coordination is the replicated
// embedded-kv (Crewlet refuses `local` once a second node exists), the stream
// is persisted on the data volume, and the pod carries the data pool's
// placement label. It also opens the leaf listener the seat pods join through
// (LeafPort) when LeafOpen says so: the class, not the Company, decides it, so
// a company that adds its first pool restarts nothing.
//
// The credentials are written into the document as literals, never as
// ${VAR} references to the pod's environment. Crewlet resolves a ${NAME} in
// the company document (Tier B) from its secret store and then from its
// process environment, in fields that are not credentials too (an MCP
// server's URL, a provider's base URL), so a credential in the environment
// is one anyone who can write such a field, or hold the API's access token,
// can have the engine send anywhere. A file is read once, at start.
//
// How people sign in is the class's (spec.signIn): api.auth.totp and
// api.auth.accept_insecure.
//
// It fails when an address is missing (ErrNoAddress); when an address is one
// Crewlet refuses to start on (an *AddressError, ErrAddressRefused); when the
// class lets people sign in with a password alone at an ExternalURL that is
// not on loopback without acknowledging it (ErrSignInNotAcknowledged); and
// when the keyring cannot be used (ErrKeyringInvalid): no keys, an active key
// it does not hold, an id that is not a valid key id, or material that is not
// base64 of 32 bytes.
func Bootstrap(in *BootstrapInput) (*BootstrapDocument, error) {
	if in.ExternalURL == "" || in.PublicURL == "" {
		return nil, fmt.Errorf("%w: api.external_url is %q and api.public.external_url %q", ErrNoAddress, in.ExternalURL, in.PublicURL)
	}
	if err := refusedAddress(in.ExternalURL, in.PublicURL, in.ArtifactsURL, in.AllowedOrigins); err != nil {
		return nil, err
	}
	signIn := in.Class.Spec.SignIn
	acknowledged := signIn.AcceptInsecure != nil && *signIn.AcceptInsecure
	if signIn.SecondFactor == v1alpha1.SecondFactorOptional && !acknowledged && !loopback(in.ExternalURL) {
		return nil, fmt.Errorf("%w: EngineClass %s sets spec.signIn.secondFactor Optional, and api.external_url is %s",
			ErrSignInNotAcknowledged, in.Class.Name, in.ExternalURL)
	}
	keyring, err := keyringOf(in.Keyring)
	if err != nil {
		return nil, err
	}

	doc := bootstrapDoc{
		Node: nodeDoc{
			// The pod's name, stable across restarts in a StatefulSet, is
			// the node's identity: everything the node registers under
			// its id would be orphaned by an id that changed.
			ID:     "${" + EnvNodeID + "}",
			Roles:  []string{"data", "ingress", "seats", "workers"},
			Labels: map[string]string{PoolLabel: v1alpha1.DataPool},
		},
		API: &apiDoc{
			Host:           "0.0.0.0",
			Port:           APIPort,
			ExternalURL:    in.ExternalURL,
			TrustedProxies: in.Class.Spec.Exposure.TrustedProxies,
			Public:         &apiPublicDoc{Port: PublicPort, ExternalURL: in.PublicURL},
			Artifacts:      artifactsOf(in.ArtifactsURL),
			Auth: &apiAuthDoc{
				Admin: adminServe,
				Tokens: []apiTokenDoc{
					{ID: TokenOperator, Token: in.Tokens.Operator, Grants: []string{grantAdmin}},
					{ID: TokenAccess, Token: in.Tokens.Access, Grants: []string{grantAdmin}},
				},
				CompanyWriters: companyWriters(ManagementPolicyOf(in.Company)),
				AllowedOrigins: in.AllowedOrigins,
				TOTP:           secondFactor(signIn.SecondFactor),
				AcceptInsecure: acknowledged,
			},
		},
		Store: storeDoc{
			Path:    DataDir + "/store/crewlet.db",
			Objects: objectsOf(in.Company, in.Class, in.Objects),
		},
		Stream: streamDoc{
			Type: "embedded",
			// A member that will serve leaves must persist its stream;
			// setting it from the first boot means adding seat pods later
			// changes nothing on the data pod's disk.
			StoreDir: DataDir + "/stream",
			// Declared rather than left to the engine, which takes three
			// quarters of the free space it finds on the filesystem at
			// boot. That is the volume on a host with one engine, but not
			// under a provisioner that carves claims out of one node
			// filesystem (local-path, hostPath, kind's default): there
			// every engine on the node sees the node's whole free space,
			// they over-commit it between them, and the engine whose
			// stream is created last is refused with "insufficient
			// storage resources available". The claim's size is the space
			// this engine was promised, so the share is taken of that.
			StoreMaxBytes: streamShare(in.VolumeSize),
			Replicas:      streamReplicas,
		},
		Coordination: coordinationDoc{Type: "embedded-kv"},
		Secrets:      keyring,
		Retention:    retentionDoc{BackupOwner: in.Class.Spec.Retention.BackupOwner},
	}
	if LeafOpen(in.Company, in.Class) {
		doc.Stream.Leaf = &leafDoc{Port: LeafPort}
	}
	return encodeBootstrap(&doc, []corev1.EnvVar{nodeIDEnv()})
}

// LeafOpen reports whether a company's data pods open the leaf listener its
// seat pods join through: while its class allows seat pools, whether or not
// the company has one yet, so that a company's first pool restarts nothing;
// and, once the class stops allowing them, for as long as the revision the
// engine runs pins a seat to a seat pod (the Company's
// status.config.placement). The engine keeps running that revision while the
// Company's next one is refused for its pools, and closing the listener under
// it would leave every seat it pins held by nobody until each tenant edited
// its Seats.
func LeafOpen(c *v1alpha1.Company, class *v1alpha1.EngineClass) bool {
	return SeatPoolsAllowed(class) || PinsSeatPods(c)
}

// SeatPoolsAllowed reports whether a class lets its companies run seat pods
// at all.
func SeatPoolsAllowed(class *v1alpha1.EngineClass) bool {
	return class.Spec.SeatPools.Policy == v1alpha1.SeatPoolsAllowed
}

// PinsSeatPods reports whether the revision the Company's engine runs, as its
// status.config.placement records it, pins a seat to a pod other than a data
// pod: to a Shared pool's, or to a seat's own.
func PinsSeatPods(c *v1alpha1.Company) bool {
	pins := c.Status.Config.Placement
	return len(pins.Seats) > 0 || slices.ContainsFunc(pins.Pools, func(pool string) bool { return pool != v1alpha1.DataPool })
}

// SeatPodInput is what the Tier A of a seat pool's pods is rendered from.
type SeatPodInput struct {
	Company *v1alpha1.Company
	Class   *v1alpha1.EngineClass
	// Pool is the pool, as PlanTopology planned it.
	Pool *PoolPlan
	// Seat is the handle of the seat the pods run, for a PerSeat pool, and
	// empty for a Shared pool. It is not in the document, which a PerSeat
	// pool's pods share, but in the environment it gives them (EnvSeat).
	Seat string
	// Keyring is the company's keyring, as for the data pods.
	Keyring Keyring
	// Objects are the keys of the class's S3 bucket, as for the data pods.
	Objects *S3Credentials
	// DataMembers are the DNS names of every data pod, such as
	// nimbus-data-0.nimbus-data.tenant-nimbus.svc: the pods' names in the
	// data StatefulSet's headless Service, which resolve while the pod
	// runs, ready or not.
	DataMembers []string
}

// SeatPodBootstrap renders the Tier A of a seat pool's pods: a node with the
// seats role alone, which holds no data. Its store is scratch, deleted at
// every start; it joins the company's event stream as a NATS leaf of the data
// pods, with JetStream off, holding nothing and voting in nothing; and it
// takes the company document from coordination, so it is given none.
//
// What it shares with the company it must share exactly: the keyring, since it
// opens the company document and the secret store itself; the object store,
// which every node checks against the one the company recorded and refuses
// to start on another; the stream's replica count; and the backup owner the
// data pods name. Those blocks are rendered by the same code as the data
// pods'.
//
// Its API block binds APIPort on every interface and holds no token: a node
// without the ingress role serves only its probes there, /health and /ready,
// which the kubelet reaches at the pod's own address and which are exempt
// from authentication, and refuses every other route as it would an unknown
// one. Leaving api.port unset would bind nothing at all, and leave the pod
// with no probe to tell a seat pod that has joined the fleet and claims
// seats from one that is still starting, or has lost the data pods. It is
// given no public listener (api.public), which a node without the ingress
// role never binds: its seats' executor runs, a box on the pod's own host
// included, call their tools through the data pods' bridge at the Company's
// public address, relayed back over the broker, so nothing a box dials is a
// route on a seat pod.
//
// A Shared pool's pods carry the pool's own placement label. A PerSeat pool's
// carry the seat label with the seat's handle, which the document refers to
// as ${CREWLET_SEAT}, so one document serves the whole pool and each seat's
// StatefulSet gives its pods their seat through Env. Like the data pods',
// the document holds the keyring and the object store's keys as values, and
// is kept in a Secret.
//
// It fails when the keyring cannot be used, as Bootstrap does, when no data
// member is given, and when Seat does not match the pool's mode.
func SeatPodBootstrap(in *SeatPodInput) (*BootstrapDocument, error) {
	keyring, err := keyringOf(in.Keyring)
	if err != nil {
		return nil, err
	}
	if len(in.DataMembers) == 0 {
		return nil, errors.New("a seat pod joins the company's event stream through a data pod, and no data pod was given")
	}
	env := []corev1.EnvVar{nodeIDEnv()}
	labels := map[string]string{PoolLabel: in.Pool.Name}
	switch {
	case in.Pool.Mode == v1alpha1.SeatPoolPerSeat && in.Seat == "":
		return nil, fmt.Errorf("the pods of PerSeat pool %s run one seat each, and no seat was given", in.Pool.Name)
	case in.Pool.Mode == v1alpha1.SeatPoolPerSeat:
		labels = map[string]string{SeatLabel: "${" + EnvSeat + "}"}
		env = append(env, corev1.EnvVar{Name: EnvSeat, Value: in.Seat})
	case in.Seat != "":
		return nil, fmt.Errorf("the pods of Shared pool %s share its seats, and were given seat %s of their own", in.Pool.Name, in.Seat)
	}

	urls := make([]string, len(in.DataMembers))
	for i, host := range in.DataMembers {
		// Any one that answers will do; listing every member lets the
		// leaf rejoin through another when one is down.
		urls[i] = fmt.Sprintf("nats-leaf://%s:%d", host, LeafPort)
	}
	doc := bootstrapDoc{
		Node: nodeDoc{
			ID:            "${" + EnvNodeID + "}",
			Roles:         []string{"seats"},
			Labels:        labels,
			MaxConcurrent: in.Pool.MaxConcurrentTurns,
		},
		API: &apiDoc{Host: "0.0.0.0", Port: APIPort},
		Store: storeDoc{
			Path: DataDir + "/store/crewlet.db",
			// Required on a node without the data role: what it keeps is
			// a cache of its seats' memory and its working rows, rebuilt
			// from the stream when it claims a seat.
			Scratch: true,
			Objects: objectsOf(in.Company, in.Class, in.Objects),
		},
		Stream: streamDoc{
			Type:     "embedded",
			Replicas: streamReplicas,
			Leaf:     &leafDoc{URLs: urls},
		},
		Coordination: coordinationDoc{Type: "embedded-kv"},
		Secrets:      keyring,
		Retention:    retentionDoc{BackupOwner: in.Class.Spec.Retention.BackupOwner},
	}
	return encodeBootstrap(&doc, env)
}

// nodeIDEnv gives an engine container its pod's name, its node id.
func nodeIDEnv() corev1.EnvVar {
	return corev1.EnvVar{Name: EnvNodeID, ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"},
	}}
}

// objectsOf is store.objects, which every node of a company must render
// alike: Crewlet records the store the company's files are in and refuses to
// start a node that names another.
func objectsOf(c *v1alpha1.Company, class *v1alpha1.EngineClass, creds *S3Credentials) objectsDoc {
	doc := objectsDoc{Backend: objectBackend(class.Spec.Objects.Backend)}
	if class.Spec.Objects.Backend != v1alpha1.ObjectBackendS3 {
		return doc
	}
	s3 := class.Spec.Objects.S3
	doc.S3 = &s3Doc{
		Endpoint:  s3.Endpoint,
		Region:    s3.Region,
		Bucket:    s3.Bucket,
		Prefix:    ExpandTemplate(cmp.Or(s3.Prefix, defaultS3Prefix), c),
		PathStyle: s3.PathStyle != nil && *s3.PathStyle,
	}
	// Without credentials the engine uses the SDK's default chain: the
	// pod's workload identity or the node's role.
	if creds != nil {
		doc.S3.AccessKeyID = creds.AccessKeyID
		doc.S3.SecretAccessKey = creds.SecretAccessKey
	}
	return doc
}

// encodeBootstrap encodes a Tier A, with the environment it refers to.
func encodeBootstrap(doc *bootstrapDoc, env []corev1.EnvVar) (*BootstrapDocument, error) {
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encoding the bootstrap document: %w", err)
	}
	out, err := yamlOf(encoded)
	if err != nil {
		return nil, fmt.Errorf("converting the bootstrap document to YAML: %w", err)
	}
	sum := sha256.Sum256(out)
	return &BootstrapDocument{YAML: out, Hash: hex.EncodeToString(sum[:]), Env: env}, nil
}

// companyWriters is api.auth.company_writers for a management policy: the
// token ids the engine lets change the company document, nil for every
// token.
//
// Under Enforce the operator is the document's only writer and overwrites any
// other revision, so an edit made with the access token, or in the dashboard
// it is signed into, would last until the next reconcile and nothing would
// say so. Naming the operator's own token as the only writer has the engine
// refuse that edit when it is made (403 config_managed), and its dashboard
// draws the document read-only. The engine's own writes (a provisioning pass
// recording what it found at a vendor) are not judged by the list, and drift
// correction still overwrites them.
//
// Observe names no writer: the operator never writes there, and the people
// running the engine are the document's owners, so a list would lock out the
// only writers it has. Seed names none either: it hands the document to the
// engine's own users after the first write, which is the whole point of it.
//
// The engine reads Tier A only at start, so a change of policy is a change
// of the bootstrap Secret, whose hash rolls the data pod: the new list holds
// from the restart on.
func companyWriters(policy v1alpha1.ManagementPolicy) []string {
	if policy == v1alpha1.ManagementPolicyEnforce {
		return []string{TokenOperator}
	}
	return nil
}

// loopback reports whether address, a data pod's api.external_url, names a
// host on the loopback interface: localhost in any case, which a browser
// reads as its own localhost, or a loopback IP (127.0.0.0/8, ::1). An address
// that does not parse is not on loopback.
//
// It is Crewlet's own judgement, copied (config.API.externalLoopback, over
// config.API.ExternalBase, which TestMirroredEngineDeclarationsAreUnchanged
// holds): the engine refuses to start with a second factor optional on any
// other address unless the acknowledgement is given, and the operator has to
// know the verdict before the engine does, to hold the pods back rather than
// start one that crash-loops. It is made on where the engine is told a browser
// reaches it, never on where the engine binds, which is every interface of
// the pod: the bind says who can open a socket, and the address where a
// person types their password.
func loopback(address string) bool {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(address), "/"))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ManagementPolicyOf is a Company's spec.config.managementPolicy, with the
// API's default for a Company read before the API server defaulted it.
func ManagementPolicyOf(c *v1alpha1.Company) v1alpha1.ManagementPolicy {
	if p := c.Spec.Config.ManagementPolicy; p != "" {
		return p
	}
	return v1alpha1.ManagementPolicyEnforce
}

// keyringOf renders the keyring block. The keys come from a Secret, which
// anyone allowed to edit it can change, or a platform supplies, so they are
// checked here rather than trusted: a key the engine could not use would be
// found only when the engine refuses to start, or, for a short key, never.
func keyringOf(k Keyring) (keyringDoc, error) {
	if len(k.Keys) == 0 {
		return keyringDoc{}, fmt.Errorf("%w: it holds no key, and the engine needs one to seal its secret store", ErrKeyringInvalid)
	}
	if _, ok := k.Keys[k.ActiveKeyID]; !ok {
		return keyringDoc{}, fmt.Errorf("%w: it holds no key %q, which is the active one", ErrKeyringInvalid, k.ActiveKeyID)
	}
	doc := keyringDoc{ActiveKeyID: k.ActiveKeyID}
	for _, id := range slices.Sorted(maps.Keys(k.Keys)) {
		if !keyIDPattern.MatchString(id) {
			return keyringDoc{}, fmt.Errorf("%w: it holds a key %q, and a key id holds only letters, digits, '.', '_' and '-'",
				ErrKeyringInvalid, id)
		}
		material := strings.TrimSpace(k.Keys[id])
		if key, err := base64.StdEncoding.DecodeString(material); err != nil || len(key) != keyringKeyBytes {
			return keyringDoc{}, fmt.Errorf("%w: its key %q is not base64 of %d bytes, the form `crewlet secrets keygen` prints",
				ErrKeyringInvalid, id, keyringKeyBytes)
		}
		doc.Keys = append(doc.Keys, secretKeyDoc{ID: id, Material: material})
	}
	return doc, nil
}

// ExpandTemplate replaces the variables of an EngineClass template (an S3
// prefix, a host name, a TLS Secret's name) with a Company's namespace, name
// and UID. The API server has already refused a template with any other
// variable, so nothing else needs replacing.
func ExpandTemplate(template string, c *v1alpha1.Company) string {
	return strings.NewReplacer(
		"{{namespace}}", c.Namespace,
		"{{name}}", c.Name,
		"{{uid}}", string(c.UID),
	).Replace(template)
}
