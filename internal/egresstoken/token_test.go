package egresstoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestTokenIsTheDocumentedMAC(t *testing.T) {
	key := []byte("k")
	h := hmac.New(sha256.New, key)
	h.Write([]byte("crewlet-egress-v1\nt-abc\nuid-1"))
	want := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	if got := Token(key, "t-abc", "uid-1"); got != want {
		t.Fatalf("Token = %q, want %q", got, want)
	}
}

func TestVerify(t *testing.T) {
	const ns, uid = "t-abc", "uid-1"
	key := []byte("signing-key")
	tok := Token(key, ns, uid)
	cases := []struct {
		name               string
		key                []byte
		namespace, uid, tk string
		want               bool
	}{
		{"own token", key, ns, uid, tok, true},
		{"another namespace", key, "t-abd", uid, tok, false},
		{"a recreated namespace", key, ns, "uid-2", tok, false},
		{"a rotated key", []byte("other"), ns, uid, tok, false},
		{"not base64", key, ns, uid, "%%%", false},
		{"empty", key, ns, uid, "", false},
		// The separator keeps a name and UID from being re-split.
		{"shifted boundary", key, "t-abc\nuid", "-1", tok, false},
	}
	for _, c := range cases {
		if got := Verify(c.key, c.namespace, c.uid, c.tk); got != c.want {
			t.Errorf("%s: Verify = %v, want %v", c.name, got, c.want)
		}
	}
}
