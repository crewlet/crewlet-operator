package secretsync

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// storeGrammar is the engine's own name rule (envref.ValidName in Crewlet):
// PUT /secrets/{name} refuses anything else.
var storeGrammar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// TestStoreNameReadsLikeTheReference pins the readable form: a person
// scanning the engine's listing recognises the Secret and the key.
func TestStoreNameReadsLikeTheReference(t *testing.T) {
	tests := []struct {
		key        secretref.Key
		wantPrefix string
	}{
		{secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey}, "K8S_ANA_SLACK_BOT_TOKEN_"},
		{secretref.Key{Namespace: nimbusNS, Name: "anthropic", Key: apiKeyKey}, "K8S_ANTHROPIC_API_KEY_"},
		{secretref.Key{Namespace: nimbusNS, Name: "s3", Key: "accessKeyID"}, "K8S_S3_ACCESS_KEY_ID_"},
		{secretref.Key{Namespace: nimbusNS, Name: "github-mcp", Key: "header"}, "K8S_GITHUB_MCP_HEADER_"},
		{secretref.Key{Namespace: nimbusNS, Name: "tls.v2", Key: "tls.key"}, "K8S_TLS_V2_TLS_KEY_"},
		{secretref.Key{Namespace: nimbusNS, Name: "1password", Key: "token2Use"}, "K8S_1PASSWORD_TOKEN2_USE_"},
		{secretref.Key{Namespace: nimbusNS, Name: "x", Key: "--__.."}, "K8S_X_"},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			got := StoreName(tt.key)
			if !strings.HasPrefix(got, tt.wantPrefix) || len(got) != len(tt.wantPrefix)+hashLen {
				t.Fatalf("StoreName(%v) = %q, want %q and a %d-character hash", tt.key, got, tt.wantPrefix, hashLen)
			}
		})
	}
}

// TestStoreNameIsInTheStoreGrammar: a name outside the grammar is refused
// by the engine, so every reference the API admits must map into it. Secret
// names are DNS subdomains and keys are [-._a-zA-Z0-9]+, each up to 253
// characters.
func TestStoreNameIsInTheStoreGrammar(t *testing.T) {
	long := strings.Repeat("a", 253)
	keys := []secretref.Key{
		{Namespace: "ns", Name: "a", Key: "b"},
		{Namespace: "ns", Name: "0", Key: "9"},
		{Namespace: "ns", Name: "a.b-c", Key: "-._"},
		{Namespace: "ns", Name: "a", Key: "_"},
		{Namespace: "ns", Name: long, Key: long},
		{Namespace: "ns", Name: strings.Repeat("a-", 126) + "a", Key: strings.Repeat("aB", 126)},
		{Namespace: "ns", Name: strings.Repeat("a.", 126) + "a", Key: strings.Repeat("-", 253)},
	}
	for _, k := range keys {
		got := StoreName(k)
		if !storeGrammar.MatchString(got) {
			t.Errorf("StoreName(%v) = %q is not an environment-variable name", k, got)
		}
		if strings.Contains(got, "__") {
			t.Errorf("StoreName(%v) = %q has an empty word", k, got)
		}
		if len(got) > MaxStoreNameLength {
			t.Errorf("StoreName(%v) is %d characters, more than MaxStoreNameLength %d", k, len(got), MaxStoreNameLength)
		}
		if !Owns(got) {
			t.Errorf("Owns(StoreName(%v) = %q) = false: the sync would never collect it", k, got)
		}
	}
	if got := StoreName(secretref.Key{Namespace: "ns", Name: long, Key: long}); len(got) != MaxStoreNameLength {
		t.Errorf("the longest reference gives %d characters, want exactly MaxStoreNameLength %d", len(got), MaxStoreNameLength)
	}
}

// TestStoreNameIsDeterministic: the rendered document and the sync compute
// the name separately, in any operator process, and must agree. A golden
// value catches a name that differs between processes, such as one seeded per
// process, which calling StoreName twice in one process cannot see.
func TestStoreNameIsDeterministic(t *testing.T) {
	k := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey}
	// A golden value: changing it renames every entry in every engine.
	const want = "K8S_ANA_SLACK_BOT_TOKEN_MNW3BP2PYC3D"
	if got := StoreName(k); got != want {
		t.Fatalf("StoreName(%v) = %q, want %q", k, got, want)
	}
}

// TestStoreNameKeepsReferencesApart: references that sanitise to the same
// readable parts, or differ only in namespace, or move a separator between
// the Secret name and the key, must still name different entries; otherwise
// one Secret's value would be served for another's reference.
func TestStoreNameKeepsReferencesApart(t *testing.T) {
	groups := [][]secretref.Key{
		{
			{Namespace: "a", Name: slackName, Key: botTokenKey},
			{Namespace: "b", Name: slackName, Key: botTokenKey},
		},
		{
			{Namespace: "ns", Name: slackSecret, Key: botTokenKey},
			{Namespace: "ns", Name: "ana.slack", Key: botTokenKey},
			{Namespace: "ns", Name: slackSecret, Key: "bot-token"},
			{Namespace: "ns", Name: slackSecret, Key: "bot_token"},
			{Namespace: "ns", Name: slackSecret, Key: "BOT_TOKEN"},
		},
		{
			{Namespace: "ns", Name: "a-b", Key: "c"},
			{Namespace: "ns", Name: "a", Key: "b-c"},
			{Namespace: "ns-a", Name: "b", Key: "c"},
		},
		{
			// Equal once truncated.
			{Namespace: "ns", Name: strings.Repeat("a", 60) + "1", Key: "k"},
			{Namespace: "ns", Name: strings.Repeat("a", 60) + "2", Key: "k"},
		},
	}
	for _, group := range groups {
		seen := map[string]secretref.Key{}
		for _, k := range group {
			name := StoreName(k)
			if other, ok := seen[name]; ok {
				t.Errorf("StoreName(%v) = StoreName(%v) = %q", k, other, name)
			}
			seen[name] = k
		}
	}

	// Many references sharing their readable parts, so only the hash keeps
	// them apart.
	seen := map[string]int{}
	for i := range 50000 {
		name := StoreName(secretref.Key{Namespace: fmt.Sprintf("tenant-%d", i), Name: slackName, Key: botTokenKey})
		if j, ok := seen[name]; ok {
			t.Fatalf("tenants %d and %d share %q", i, j, name)
		}
		seen[name] = i
	}
}

// TestOwnsOnlyTheOperatorsNames: the sync deletes what Owns accepts, so it
// must refuse every name a person or a provisioner could have chosen,
// including ones that merely start with the prefix.
func TestOwnsOnlyTheOperatorsNames(t *testing.T) {
	notOwned := []string{
		"",
		foreignName,
		"ANTHROPIC_API_KEY",
		"K8S_",
		"K8S_SLACK",
		"K8S_SLACK_TOKEN",
		"K8S_MY_OWN_TOKEN",
		"K8S__MNW3BP2PYC3D",
		"K8S_SLACK__MNW3BP2PYC3D",
		"k8s_ana_slack_bot_token_mnw3bp2pyc3d",
		"K8S_ANA_SLACK_BOT_TOKEN_MNW3BP2PYC3",   // hash too short
		"K8S_ANA_SLACK_BOT_TOKEN_MNW3BP2PYC3D1", // a digit outside base32
		"X_K8S_ANA_SLACK_BOT_TOKEN_MNW3BP2PYC3D",
	}
	for _, name := range notOwned {
		if Owns(name) {
			t.Errorf("Owns(%q) = true", name)
		}
	}
	if !Owns("K8S_ANA_SLACK_BOT_TOKEN_MNW3BP2PYC3D") {
		t.Error("Owns refuses a name StoreName returns")
	}
}

// seatAna is the handle of the seat the provisioned names are made for.
const seatAna = "ana"

// TestProvisionedNameIsTheEnginesAlone: a provisioned credential's entry is
// written by the engine's provisioning and by nothing of the operator's, so
// Owns must refuse every such name, or the next Sync would delete the token
// the engine just minted. Each name is in the store's grammar, reads like
// the seat and the value, and is as long as MaxProvisionedNameLength at most.
func TestProvisionedNameIsTheEnginesAlone(t *testing.T) {
	long := strings.Repeat("a", 63)
	for _, c := range []struct{ handle, name, wantPrefix string }{
		{seatAna, "gitlab-token", "PROVISIONED_ANA_GITLAB_TOKEN_"},
		{"sre-lead", "atlassian-email", "PROVISIONED_SRE_LEAD_ATLASSIAN_EMAIL_"},
		// A name whose readable part ends like a hash still is not one
		// the operator owns: the prefix differs.
		{"k8s", "abcdefghijkl", "PROVISIONED_K8S_ABCDEFGHIJKL_"},
		{long, strings.Repeat("b", 32), ""},
	} {
		got := ProvisionedName(c.handle, c.name)
		if !storeGrammar.MatchString(got) || strings.Contains(got, "__") {
			t.Errorf("ProvisionedName(%q, %q) = %q is not a store name of single-underscore words", c.handle, c.name, got)
		}
		if c.wantPrefix != "" && (!strings.HasPrefix(got, c.wantPrefix) || len(got) != len(c.wantPrefix)+hashLen) {
			t.Errorf("ProvisionedName(%q, %q) = %q, want %q and a %d-character hash", c.handle, c.name, got, c.wantPrefix, hashLen)
		}
		if len(got) > MaxProvisionedNameLength {
			t.Errorf("ProvisionedName(%q, %q) is %d characters, more than %d", c.handle, c.name, len(got), MaxProvisionedNameLength)
		}
		if Owns(got) {
			t.Errorf("Owns(%q) = true: the sync would delete a credential the engine minted", got)
		}
	}
}

// TestProvisionedNamesDoNotCollide: values that read the same once
// sanitised are different entries, so one seat's minted token is never
// another seat's, or another value of the same seat.
func TestProvisionedNamesDoNotCollide(t *testing.T) {
	const handle, name = "a-b", "token"
	pairs := [][2][2]string{
		{{handle, "c"}, {"a", "b-c"}},
		{{handle, name}, {"a--b", name}},
		{{"ana", name}, {"ana", name + "-"}},
	}
	for _, p := range pairs {
		x, y := ProvisionedName(p[0][0], p[0][1]), ProvisionedName(p[1][0], p[1][1])
		if x == y {
			t.Errorf("ProvisionedName%v and ProvisionedName%v are both %q", p[0], p[1], x)
		}
	}
}

// TestProvisionedNameIsDeterministic pins one name: changing it would point
// every rendered document at an entry the engine never minted into, and leave
// every seat's credential behind under the old name.
func TestProvisionedNameIsDeterministic(t *testing.T) {
	// A golden value, as StoreName's is.
	const want = "PROVISIONED_ANA_GITLAB_TOKEN_ZXUWOCASQLKO"
	if got := ProvisionedName(seatAna, "gitlab-token"); got != want {
		t.Fatalf("ProvisionedName(%s, gitlab-token) = %q, want %q", seatAna, got, want)
	}
}
