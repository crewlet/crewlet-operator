// Package egresstoken mints and checks the credential a tenant namespace's
// pods present to the tier egress proxy. The operator writes it into the
// namespace and the proxy recomputes it, so neither keeps a table of
// credentials: rotating the signing key revokes every one at once, and a
// namespace recreated under the same name gets a new UID and so a new token.
package egresstoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// The namespace labels the proxy reads. The tenant label marks a namespace
// as a tenant's; the trust tier selects which proxy serves it; any value of
// the quarantine label cuts the namespace off.
const (
	LabelTenant     = "crewlet.ai/tenant"
	LabelTrustTier  = "crewlet.ai/trust-tier"
	LabelQuarantine = "crewlet.ai/quarantine"
)

// domain separates these MACs from any other use of the same key.
const domain = "crewlet-egress-v1\n"

// Token is the credential of the namespace with this name and UID under key.
func Token(key []byte, namespace, uid string) string {
	return base64.RawURLEncoding.EncodeToString(mac(key, namespace, uid))
}

// Verify reports whether token is the namespace's credential under key, in
// time that does not depend on how much of it matches.
func Verify(key []byte, namespace, uid, token string) bool {
	got, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	return hmac.Equal(got, mac(key, namespace, uid))
}

func mac(key []byte, namespace, uid string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(domain + namespace + "\n" + uid))
	return h.Sum(nil)
}
