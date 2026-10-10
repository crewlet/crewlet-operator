package secretsync

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"strings"
	"unicode"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

// Prefix starts every store name the operator owns. It is reserved: a person
// who writes an entry of this shape into the store will see the operator
// delete it once no Secret reference produces its name.
const Prefix = "K8S_"

const (
	// maxSecretPart and maxKeyPart bound the readable parts of a store name.
	// A Secret name may be 253 characters and so may a key; the readable
	// parts are there for a person scanning `crewlet secrets list`, who needs
	// the start of each, and the hash keeps a truncated name unique.
	maxSecretPart = 40
	maxKeyPart    = 32

	// hashLen is the number of base32 characters of the reference's hash:
	// 60 bits, so two references whose readable parts coincide collide with
	// a probability no company's few hundred references come near.
	hashLen = 12

	// MaxStoreNameLength is the longest name StoreName returns.
	MaxStoreNameLength = len(Prefix) + maxSecretPart + 1 + maxKeyPart + 1 + hashLen
)

// hashEncoding is base32 in upper case without padding, whose alphabet
// (A-Z, 2-7) is inside the store's name grammar, so the hash needs no
// further escaping.
var hashEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// owned is the grammar of every name StoreName returns: the prefix, one or
// two upper-case readable parts of letters, digits and single underscores,
// and the hash.
var owned = regexp.MustCompile(`^` + Prefix + `[A-Z0-9]+(?:_[A-Z0-9]+)*_[A-Z2-7]{12}$`)

// StoreName returns the name of the engine secret-store entry that holds the
// value of k, and that the company document refers to as ${StoreName(k)}.
//
// The name is Prefix, the Secret's name, the key, and a hash, joined by
// underscores: a reference to key botToken of Secret ana-slack is
// K8S_ANA_SLACK_BOT_TOKEN_ followed by twelve hash characters. The readable
// parts are upper-cased, with every run of characters the store's grammar
// does not allow (letters, digits and underscores, not starting with a digit)
// turned into one underscore and a word boundary in camelCase marked by one
// too, and they are truncated to bound the name's length. The hash is of the
// full reference, namespace included, so references that read the same once
// sanitised — ana-slack and ana.slack, botToken and bot-token, the same
// Secret name in two namespaces — still name different entries.
//
// The name is a pure function of k, so a rendered document depends on
// nothing but its input, and every render, the sync and every operator
// process name the same entry for a reference. A name that differed between
// them would rename every entry: every value would be pushed again under the
// new names, and each engine would resolve references to entries the next
// sync has only just written.
func StoreName(k secretref.Key) string {
	var b strings.Builder
	b.Grow(MaxStoreNameLength)
	b.WriteString(Prefix)
	b.WriteString(readable(k.Name, maxSecretPart))
	if key := readable(k.Key, maxKeyPart); key != "" {
		b.WriteByte('_')
		b.WriteString(key)
	}
	b.WriteByte('_')
	b.WriteString(referenceHash(k))
	return b.String()
}

// Owns reports whether name is one StoreName could have returned, and so an
// entry the operator may overwrite and delete.
func Owns(name string) bool {
	return owned.MatchString(name)
}

// readable turns s into upper-case words of letters and digits joined by
// single underscores, at most limit characters long, with no underscore at
// either end. It returns "" when s has no letter or digit, which a Secret
// key such as "-" or "_" does not.
func readable(s string, limit int) string {
	var b strings.Builder
	pendingSep := false
	var prev rune
	for _, r := range s {
		isAlnum := r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
		if !isAlnum {
			pendingSep = true
			prev = r
			continue
		}
		// A lower-case letter or digit followed by an upper-case letter is a
		// camelCase word boundary: botToken reads BOT_TOKEN, not BOTTOKEN.
		if unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			pendingSep = true
		}
		if pendingSep && b.Len() > 0 {
			if b.Len()+1 >= limit {
				break
			}
			b.WriteByte('_')
		}
		pendingSep = false
		if b.Len() >= limit {
			break
		}
		b.WriteRune(unicode.ToUpper(r))
		prev = r
	}
	return b.String()
}

// referenceHash is the first hashLen characters of the base32 SHA-256 of
// the full reference. NUL separates the fields because no namespace, Secret
// name or key can contain it, so two different references never hash the
// same input.
func referenceHash(k secretref.Key) string {
	sum := sha256.Sum256([]byte(k.Namespace + "\x00" + k.Name + "\x00" + k.Key))
	return hashEncoding.EncodeToString(sum[:])[:hashLen]
}

// ProvisionedPrefix starts the name of every store entry a seat's provisioned
// credential is kept in. It is not Prefix: those entries are the engine's
// own, written by its provisioning when it mints a seat's credential, and
// Sync must never overwrite or delete one.
const ProvisionedPrefix = "PROVISIONED_"

// MaxProvisionedNameLength is the longest name ProvisionedName returns.
const MaxProvisionedNameLength = len(ProvisionedPrefix) + maxSecretPart + 1 + maxKeyPart + 1 + hashLen

// ProvisionedName returns the name of the engine secret-store entry that
// holds the credential the engine provisions for the seat with handle under
// the provisioned name, and that the company document refers to as
// ${ProvisionedName(handle, name)}.
//
// The engine's provisioning mints a seat's credential into whatever ${NAME}
// the seat's own configuration holds where it looks for one, so the name is
// the contract between the document and the engine: the operator chooses it
// and the engine fills it. It is ProvisionedPrefix, the readable handle and
// name, and a hash of both, so two seats, or two values of one seat, never
// share an entry (handles a-b and a--b read the same once sanitised), and it
// is a pure function of its arguments, so every render names the same entry
// and a minted credential survives every render and restart of the operator:
// a different name would point every seat at an entry the engine never
// minted into, and the seat would lose its minted credential, such as a
// GitLab token. Owns refuses every such name.
func ProvisionedName(handle, name string) string {
	var b strings.Builder
	b.Grow(MaxProvisionedNameLength)
	b.WriteString(ProvisionedPrefix)
	b.WriteString(readable(handle, maxSecretPart))
	b.WriteByte('_')
	b.WriteString(readable(name, maxKeyPart))
	b.WriteByte('_')
	sum := sha256.Sum256([]byte(handle + "\x00" + name))
	b.WriteString(hashEncoding.EncodeToString(sum[:])[:hashLen])
	return b.String()
}
