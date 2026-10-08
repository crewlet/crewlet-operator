package render

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
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
// the token in the Secret BootstrapSecrets.APITokens names.
const (
	// TokenOperator is the operator's own token. It is never handed out, so
	// revoking a consumer's token never locks the operator out.
	TokenOperator = "operator"
	// TokenAccess is the token for the company's consumers, published in
	// the Secret the Company's status.apiAccessSecretRef names.
	TokenAccess = "access"
)

// Environment variables a data pod's Tier A refers to.
//
//nolint:gosec // G101 reads the names as credentials; they are the names of variables whose values come from Secrets.
const (
	EnvNodeID            = "CREWLET_NODE_ID"
	EnvTokenOperator     = "CREWLET_TOKEN_OPERATOR"
	EnvTokenAccess       = "CREWLET_TOKEN_ACCESS"
	EnvS3AccessKeyID     = "CREWLET_S3_ACCESS_KEY_ID"
	EnvS3SecretAccessKey = "CREWLET_S3_SECRET_ACCESS_KEY"
	// envSecretKeyPrefix is followed by a keyring key's id, upper-cased,
	// with '.' and '-' turned into '_'.
	envSecretKeyPrefix = "CREWLET_SECRET_KEY_"
)

// BootstrapInput is what a data pod's Tier A is rendered from.
type BootstrapInput struct {
	Company *v1alpha1.Company
	Class   *v1alpha1.EngineClass
	// Keyring is the keys the company's keyring Secret holds.
	Keyring Keyring
	// Secrets names the Secrets, in the Company's namespace, that the pod's
	// environment reads the document's references from.
	Secrets BootstrapSecrets
}

// Keyring is the engine's secret-store keyring: the keys it can open, and
// the one it seals new writes with. Crewlet decrypts with every key and
// encrypts with the active one, which is what lets a key be rotated without
// a restart.
type Keyring struct {
	ActiveKeyID string
	// KeyIDs are the keys' ids, each also the key that holds the key's
	// material (base64 of 32 random bytes) in the Secret
	// BootstrapSecrets.Keyring names.
	KeyIDs []string
}

// BootstrapSecrets names the Secrets the operator owns in a Company's
// namespace that a data pod's environment is read from.
type BootstrapSecrets struct {
	// Keyring holds the keyring, one key per Keyring.KeyIDs entry.
	Keyring string
	// APITokens holds the API tokens under TokenOperator and TokenAccess.
	APITokens string
	// Objects holds the S3 credentials under v1alpha1.S3AccessKeyIDKey and
	// v1alpha1.S3SecretAccessKeyKey, copied from the class's
	// spec.objects.s3.credentialsSecretRef. Read only when the class sets
	// one.
	Objects string
}

// BootstrapDocument is a rendered Tier A.
type BootstrapDocument struct {
	// YAML is the crewlet.yaml of the data pods. It holds ${VAR}
	// references only, never a credential.
	YAML []byte
	// Hash is the hex-encoded SHA-256 of YAML, for the pod template
	// annotation that rolls the pods when the document changes.
	Hash string
	// Env is every environment variable YAML refers to, sorted by name:
	// exactly what the data pod's engine container must be given for the
	// references to resolve. None carries a literal credential; each reads
	// a Secret or the pod's own metadata.
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
	Type     string `json:"type"`
	StoreDir string `json:"store_dir"`
	Replicas int    `json:"replicas"`
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
// It fails only when the keyring cannot be referenced: no keys, an active key
// it does not hold, or ids that are not valid key ids or that name one
// environment variable twice.
func Bootstrap(in *BootstrapInput) (*BootstrapDocument, error) {
	keyring, keyEnv, err := keyringOf(in.Keyring, in.Secrets.Keyring)
	if err != nil {
		return nil, err
	}

	env := []corev1.EnvVar{
		{Name: EnvNodeID, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"},
		}},
		secretEnv(EnvTokenOperator, in.Secrets.APITokens, TokenOperator),
		secretEnv(EnvTokenAccess, in.Secrets.APITokens, TokenAccess),
	}
	env = append(env, keyEnv...)

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
					{ID: TokenOperator, Token: "${" + EnvTokenOperator + "}"},
					{ID: TokenAccess, Token: "${" + EnvTokenAccess + "}"},
				},
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
			Replicas: 1,
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
			Prefix:    expandTemplate(cmp.Or(s3.Prefix, defaultS3Prefix), in.Company),
			PathStyle: s3.PathStyle != nil && *s3.PathStyle,
		}
		// Without credentials the engine uses the SDK's default chain: the
		// pod's workload identity or the node's role.
		if s3.CredentialsSecretRef.Name != "" {
			doc.Store.Objects.S3.AccessKeyID = "${" + EnvS3AccessKeyID + "}"
			doc.Store.Objects.S3.SecretAccessKey = "${" + EnvS3SecretAccessKey + "}"
			env = append(env,
				secretEnv(EnvS3AccessKeyID, in.Secrets.Objects, v1alpha1.S3AccessKeyIDKey),
				secretEnv(EnvS3SecretAccessKey, in.Secrets.Objects, v1alpha1.S3SecretAccessKeyKey))
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
	slices.SortFunc(env, func(a, b corev1.EnvVar) int { return strings.Compare(a.Name, b.Name) })
	sum := sha256.Sum256(out)
	return &BootstrapDocument{YAML: out, Hash: hex.EncodeToString(sum[:]), Env: env}, nil
}

// keyringOf renders the keyring block and the environment its references
// read. The ids come from a Secret, which anyone allowed to edit it can
// change, so they are checked here rather than trusted.
func keyringOf(k Keyring, secret string) (keyringDoc, []corev1.EnvVar, error) {
	if len(k.KeyIDs) == 0 {
		return keyringDoc{}, nil, fmt.Errorf("the keyring Secret %s holds no key: the engine needs one to seal its secret store", secret)
	}
	if !slices.Contains(k.KeyIDs, k.ActiveKeyID) {
		return keyringDoc{}, nil, fmt.Errorf("the keyring Secret %s holds no key %q, which is the active one", secret, k.ActiveKeyID)
	}
	ids := slices.Sorted(slices.Values(k.KeyIDs))
	doc := keyringDoc{ActiveKeyID: k.ActiveKeyID}
	env := make([]corev1.EnvVar, 0, len(ids))
	owner := map[string]string{}
	for _, id := range ids {
		if !keyIDPattern.MatchString(id) {
			return keyringDoc{}, nil, fmt.Errorf("the keyring Secret %s holds a key %q: a key id holds only letters, digits, '.', '_' and '-'", secret, id)
		}
		name := envSecretKeyPrefix + strings.NewReplacer(".", "_", "-", "_").Replace(strings.ToUpper(id))
		if other, taken := owner[name]; taken {
			return keyringDoc{}, nil, fmt.Errorf("the keyring Secret %s holds keys %q and %q, which would both be read from %s: rename one", secret, other, id, name)
		}
		owner[name] = id
		doc.Keys = append(doc.Keys, secretKeyDoc{ID: id, Material: "${" + name + "}"})
		env = append(env, secretEnv(name, secret, id))
	}
	return doc, env, nil
}

func secretEnv(name, secret, key string) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{Name: secret, Key: key},
	}}
}

// expandTemplate replaces the variables of an EngineClass template with a
// Company's namespace, name and UID.
func expandTemplate(template string, c *v1alpha1.Company) string {
	return strings.NewReplacer(
		"{{namespace}}", c.Namespace,
		"{{name}}", c.Name,
		"{{uid}}", string(c.UID),
	).Replace(template)
}
