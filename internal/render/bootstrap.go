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
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/yaml"

	"github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// What a data pod looks like from the inside. The runtime reconciler mounts
// the data volume and opens the port the document names, so both are
// constants the two share rather than strings each spells.
const (
	// DataDir is where a data pod's volume is mounted. The store, its
	// snapshots and the event stream all live under it.
	DataDir = "/var/lib/crewlet"
	// APIPort is the port the engine serves its API, dashboard and
	// webhooks on.
	APIPort = 8080
)

// The API tokens of an engine. Each id is recorded by Crewlet as the author
// of the revisions written with the token, and is also the key that holds
// the token in the company's API tokens Secret.
const (
	// TokenOperator is the operator's own token. It is never handed out, so
	// revoking a consumer's token never locks the operator out.
	TokenOperator = "operator"
	// TokenAccess is the token for the company's consumers, published in
	// the Secret the Company's status.apiAccessSecretRef names.
	TokenAccess = "access"
)

// EnvNodeID is the one environment variable a data pod's Tier A refers to:
// the pod's name, which the Downward API gives it. Every credential of Tier A
// is written into the document itself instead, which is kept in a Secret;
// see Bootstrap for why none is an environment variable.
const EnvNodeID = "CREWLET_NODE_ID"

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
}

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
	API          apiDoc          `json:"api"`
	Store        storeDoc        `json:"store"`
	Stream       streamDoc       `json:"stream"`
	Coordination coordinationDoc `json:"coordination"`
	Secrets      keyringDoc      `json:"secrets"`
	Retention    retentionDoc    `json:"retention,omitzero"`
}

type nodeDoc struct {
	ID     string            `json:"id"`
	Roles  []string          `json:"roles"`
	Labels map[string]string `json:"labels"`
}

type apiDoc struct {
	Host string     `json:"host"`
	Port int        `json:"port"`
	Auth apiAuthDoc `json:"auth"`
}

type apiAuthDoc struct {
	AllowAnonymousRead bool          `json:"allow_anonymous_read"`
	Tokens             []apiTokenDoc `json:"tokens"`
	CompanyWriters     []string      `json:"company_writers,omitempty"`
}

type apiTokenDoc struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type storeDoc struct {
	Path    string     `json:"path"`
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
	Type          string `json:"type"`
	StoreDir      string `json:"store_dir"`
	StoreMaxBytes int64  `json:"store_max_bytes"`
	Replicas      int    `json:"replicas"`
}

// The engine's event stream is given streamShareNumerator /
// streamShareDenominator of the data volume: three quarters, the share
// Crewlet's embedded broker takes of the free space it finds when nothing
// sets one. The rest is the store and its snapshots.
const (
	streamShareNumerator   = 3
	streamShareDenominator = 4
)

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
// topology: one data pod that holds the estate, serves the API, runs every
// seat and runs the workers.
//
// The document is already shaped for a fleet, so that stateless seat pods can
// join later without migrating anything: coordination is the replicated
// embedded-kv (Crewlet refuses `local` once a second node exists), the stream
// is persisted on the data volume, and the pod carries the data pool's
// placement label that every agent seat is pinned to.
//
// The credentials are written into the document as literals, never as
// ${VAR} references to the pod's environment. Crewlet resolves a ${NAME} in
// the company document (Tier B) from its secret store and then from its
// process environment, in fields that are not credentials too (an MCP
// server's URL, a provider's base URL), so a credential in the environment
// is one anyone who can write such a field, or hold the API's access token,
// can have the engine send anywhere. A file is read once, at start.
//
// It fails only when the keyring cannot be used: no keys, an active key it
// does not hold, an id that is not a valid key id, or material that is not
// base64 of 32 bytes.
func Bootstrap(in *BootstrapInput) (*BootstrapDocument, error) {
	keyring, err := keyringOf(in.Keyring)
	if err != nil {
		return nil, err
	}

	env := []corev1.EnvVar{
		{Name: EnvNodeID, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"},
		}},
	}

	doc := bootstrapDoc{
		Node: nodeDoc{
			// The pod's name, stable across restarts in a StatefulSet, is
			// the node's identity: everything the node registers under
			// its id would be orphaned by an id that changed.
			ID:     "${" + EnvNodeID + "}",
			Roles:  []string{"data", "ingress", "seats", "workers"},
			Labels: map[string]string{PoolLabel: DataPool},
		},
		API: apiDoc{
			Host: "0.0.0.0",
			Port: APIPort,
			Auth: apiAuthDoc{
				// Crewlet serves reads without a token by default, and its
				// reads carry the seats' transcripts and memory. Every
				// consumer of an operator-run engine has a token, so none
				// needs the anonymous reads; probes and webhooks are
				// exempt from authentication either way.
				AllowAnonymousRead: false,
				Tokens: []apiTokenDoc{
					{ID: TokenOperator, Token: in.Tokens.Operator},
					{ID: TokenAccess, Token: in.Tokens.Access},
				},
				CompanyWriters: companyWriters(ManagementPolicyOf(in.Company)),
			},
		},
		Store: storeDoc{
			Path:    DataDir + "/store/crewlet.db",
			Objects: objectsDoc{Backend: objectBackend(in.Class.Spec.Objects.Backend)},
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
			StoreMaxBytes: in.VolumeSize.Value() / streamShareDenominator * streamShareNumerator,
			Replicas:      1,
		},
		Coordination: coordinationDoc{Type: "embedded-kv"},
		Secrets:      keyring,
		Retention:    retentionDoc{BackupOwner: in.Class.Spec.Retention.BackupOwner},
	}

	if in.Class.Spec.Objects.Backend == v1alpha1.ObjectBackendS3 {
		s3 := in.Class.Spec.Objects.S3
		doc.Store.Objects.S3 = &s3Doc{
			Endpoint:  s3.Endpoint,
			Region:    s3.Region,
			Bucket:    s3.Bucket,
			Prefix:    ExpandTemplate(cmp.Or(s3.Prefix, defaultS3Prefix), in.Company),
			PathStyle: s3.PathStyle != nil && *s3.PathStyle,
		}
		// Without credentials the engine uses the SDK's default chain: the
		// pod's workload identity or the node's role.
		if creds := in.Objects; creds != nil {
			doc.Store.Objects.S3.AccessKeyID = creds.AccessKeyID
			doc.Store.Objects.S3.SecretAccessKey = creds.SecretAccessKey
		}
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encoding the bootstrap document: %w", err)
	}
	out, err := yaml.JSONToYAML(encoded)
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
		return keyringDoc{}, errors.New("the keyring holds no key: the engine needs one to seal its secret store")
	}
	if _, ok := k.Keys[k.ActiveKeyID]; !ok {
		return keyringDoc{}, fmt.Errorf("the keyring holds no key %q, which is the active one", k.ActiveKeyID)
	}
	doc := keyringDoc{ActiveKeyID: k.ActiveKeyID}
	for _, id := range slices.Sorted(maps.Keys(k.Keys)) {
		if !keyIDPattern.MatchString(id) {
			return keyringDoc{}, fmt.Errorf("the keyring holds a key %q: a key id holds only letters, digits, '.', '_' and '-'", id)
		}
		material := strings.TrimSpace(k.Keys[id])
		if key, err := base64.StdEncoding.DecodeString(material); err != nil || len(key) != keyringKeyBytes {
			return keyringDoc{}, fmt.Errorf("the keyring's key %q is not base64 of %d bytes, the form `crewlet secrets keygen` prints", id, keyringKeyBytes)
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
