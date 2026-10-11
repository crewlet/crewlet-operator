package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// commit is the revision the fake registry's images carry.
const commit = "a20ed7519ba7b183fd8c2c4ff54e3dcc782cab0e"

// The fake registry and the host its blobs are redirected to. Both resolve to
// the test's own servers (fakeClient), under names their certificate holds,
// and the two are different hosts, as a registry and its storage are, so the
// client must not carry the registry's token across the redirect.
const (
	registryHost = "registry.example.com"
	blobsHost    = "blobs.example.com"
	token        = "anonymous-pull"
	repository   = "crewlet/crewlet"
	tagMain      = "main"
	keyMediaType = "mediaType"
)

// The platforms of the fake images: os, architecture and the digest of the
// platform's manifest in an index.
const (
	linux = "linux"
	amd64 = "amd64"
	arm64 = "arm64"
)

// at is the fake registry's repository at tag or digest, which suffix
// spells as Docker does, with its separator.
func at(suffix string) string {
	return registryHost + "/" + repository + suffix
}

// fakeRegistry serves one repository the way GHCR does: every read behind an
// anonymous bearer token, manifests by tag and by digest, and blobs by a
// redirect to another host.
type fakeRegistry struct {
	t *testing.T
	// manifests are served by tag and by their digest, with their type.
	manifests map[string]served
	// blobs are served by digest, from blobsHost.
	blobs map[string][]byte
	// tokenStatus, when set, is how the token endpoint answers instead.
	tokenStatus int
	// challenge, when set, is the WWW-Authenticate a read without the
	// token is answered with instead of the registry's own.
	challenge string
	// local is a registry as one runs on a developer's machine: open to
	// anybody, and serving its blobs itself.
	local bool
}

type served struct {
	kind string
	body []byte
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	return &fakeRegistry{t: t, manifests: map[string]served{}, blobs: map[string][]byte{}}
}

// image adds an image whose config carries labels, under tag when it is not
// empty, and returns its manifest's digest.
func (f *fakeRegistry) image(tag string, labels map[string]string) string {
	f.t.Helper()
	config := marshal(f.t, map[string]any{
		"architecture": amd64, "os": linux,
		"config": map[string]any{"Labels": labels},
	})
	f.blobs[digestOf(config)] = config
	manifest := marshal(f.t, map[string]any{
		"schemaVersion": 2, keyMediaType: ociManifest,
		"config": map[string]any{keyMediaType: "application/vnd.oci.image.config.v1+json", "digest": digestOf(config)},
	})
	return f.manifest(tag, ociManifest, manifest)
}

// index adds an index of the platforms given as os/architecture and their
// manifests' digests, under tag, and returns its digest.
func (f *fakeRegistry) index(tag string, platforms ...[3]string) string {
	f.t.Helper()
	manifests := make([]map[string]any, 0, len(platforms))
	for _, p := range platforms {
		manifests = append(manifests, map[string]any{
			keyMediaType: ociManifest, "digest": p[2],
			"platform": map[string]string{"os": p[0], "architecture": p[1]},
		})
	}
	return f.manifest(tag, ociIndex, marshal(f.t, map[string]any{
		"schemaVersion": 2, keyMediaType: ociIndex, "manifests": manifests,
	}))
}

func (f *fakeRegistry) manifest(tag, kind string, body []byte) string {
	digest := digestOf(body)
	f.manifests[digest] = served{kind: kind, body: body}
	if tag != "" {
		f.manifests[tag] = served{kind: kind, body: body}
	}
	return digest
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" {
		if f.tokenStatus != 0 {
			http.Error(w, `{"errors":[{"code":"DENIED","message":"no anonymous pulls"}]}`, f.tokenStatus)
			return
		}
		q := r.URL.Query()
		if q.Get("service") != registryHost || q.Get("scope") != "repository:"+repository+":pull" {
			f.t.Errorf("a token was asked for service %q, scope %q", q.Get("service"), q.Get("scope"))
		}
		_, _ = fmt.Fprintf(w, `{"token":%q}`, token)
		return
	}
	if !f.local && r.Header.Get("Authorization") != "Bearer "+token {
		w.Header().Set("WWW-Authenticate", cmp.Or(f.challenge, fmt.Sprintf(
			`Bearer realm="https://%s/token",service=%q,scope="repository:%s:pull"`, registryHost, registryHost, repository)))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/"+repository+"/")
	kind, ref, _ := strings.Cut(rest, "/")
	switch {
	case ok && kind == "manifests":
		m, found := f.manifests[ref]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`))
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), m.kind) {
			f.t.Errorf("the manifest %s was asked for as %q, which leaves out its type %s", ref, r.Header.Get("Accept"), m.kind)
		}
		w.Header().Set("Content-Type", m.kind)
		w.Header().Set("Docker-Content-Digest", digestOf(m.body))
		_, _ = w.Write(m.body)
	case ok && kind == "blobs" && f.local:
		blob, found := f.blobs[ref]
		if !found {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(blob)
	case ok && kind == "blobs":
		http.Redirect(w, r, "https://"+blobsHost+"/storage/"+ref+"?signature=x", http.StatusTemporaryRedirect)
	default:
		http.NotFound(w, r)
	}
}

// blobServer is the storage a blob is redirected to.
func (f *fakeRegistry) blobServer(w http.ResponseWriter, r *http.Request) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		f.t.Errorf("the registry's token reached its storage on another host: %q", auth)
	}
	blob, ok := f.blobs[strings.TrimPrefix(r.URL.Path, "/storage/")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(blob)
}

// fakeClient is a client of the fake registry's two hosts, served by two TLS
// servers it dials whatever port a URL names.
func (f *fakeRegistry) fakeClient() *http.Client {
	f.t.Helper()
	registry := httptest.NewTLSServer(f)
	f.t.Cleanup(registry.Close)
	blobs := httptest.NewTLSServer(http.HandlerFunc(f.blobServer))
	f.t.Cleanup(blobs.Close)
	client := registry.Client()
	transport := client.Transport.(*http.Transport).Clone()
	addrs := map[string]string{registryHost: registry.Listener.Addr().String(), blobsHost: blobs.Listener.Addr().String()}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		target, ok := addrs[host]
		if !ok {
			return nil, fmt.Errorf("the test dials no %s", host)
		}
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
	client.Transport = transport
	return client
}

// TestReadsTheRevisionOfAnIndex: GHCR's main tag names an index of two
// platforms' images and the attestations buildx adds beside them, under the
// platform unknown/unknown. The revision is read from this machine's
// platform's image, never an attestation's, through the redirect of its
// config.
func TestReadsTheRevisionOfAnIndex(t *testing.T) {
	f := newFakeRegistry(t)
	here := f.image("", map[string]string{revisionLabel: commit, "org.opencontainers.image.version": "main-a20ed7519ba7"})
	// The other platform's image is labelled apart, and listed first, so a
	// read of the first real platform rather than this machine's prints
	// the wrong commit.
	elsewhere := f.image("", map[string]string{revisionLabel: strings.Repeat("e", 40)})
	// An attestation is no image, and carries no revision: picked, it
	// would fail the read.
	attestation := f.image("", nil)
	digest := f.index(tagMain, [3]string{unknownPlatform, unknownPlatform, attestation},
		[3]string{linux, otherArchitecture(), elsewhere}, [3]string{linux, runtime.GOARCH, here})
	// An index with no image for this machine's architecture is read from
	// its first real platform's, which is no attestation either.
	foreign := f.index("foreign",
		[3]string{unknownPlatform, unknownPlatform, attestation}, [3]string{linux, otherArchitecture(), here})

	for ref, digest := range map[string]string{
		at(":main"):      digest,
		at("@" + digest): digest,
		at(":foreign"):   foreign,
	} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), f.fakeClient(), []string{ref}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%s) = %d: %s", ref, code, stderr.String())
		}
		if stdout.String() != commit+"\n" {
			t.Errorf("run(%s) printed %q, want the commit alone", ref, stdout.String())
		}
		if !strings.Contains(stderr.String(), digest) || !strings.Contains(stderr.String(), commit) {
			t.Errorf("run(%s) says %q, which names neither the image's digest nor its commit", ref, stderr.String())
		}
	}
}

// otherArchitecture is an architecture other than this machine's.
func otherArchitecture() string {
	if runtime.GOARCH == arm64 {
		return amd64
	}
	return arm64
}

// TestReadsALoopbackRegistryOverHTTP: a registry on this machine, as one runs
// for kind, answers plain HTTP, with no token and its blobs served in place.
func TestReadsALoopbackRegistryOverHTTP(t *testing.T) {
	f := newFakeRegistry(t)
	f.local = true
	f.image(tagMain, map[string]string{revisionLabel: commit})
	registry := httptest.NewServer(f)
	t.Cleanup(registry.Close)
	_, port, err := net.SplitHostPort(registry.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"localhost:" + port, registry.Listener.Addr().String()} {
		img, err := read(context.Background(), registry.Client(), mustParse(t, host+"/"+repository+":main"))
		if err != nil || img.revision != commit {
			t.Errorf("read of %s = %+v, %v; want %s", host, img, err, commit)
		}
	}
	for host, want := range map[string]bool{
		"localhost": true, "localhost:5000": true, "127.0.0.1:5001": true, "[::1]:5000": true,
		"ghcr.io": false, "registry.example.com:5000": false, "localhost.example.com": false, "10.0.0.1:5000": false,
	} {
		if got := loopback(host); got != want {
			t.Errorf("loopback(%s) = %v, want %v", host, got, want)
		}
	}
}

// TestReadsTheRevisionOfAnImage: a tag may name one platform's image rather
// than an index of them.
func TestReadsTheRevisionOfAnImage(t *testing.T) {
	f := newFakeRegistry(t)
	f.image("single", map[string]string{revisionLabel: commit})
	img, err := read(context.Background(), f.fakeClient(), mustParse(t, at(":single")))
	if err != nil || img.revision != commit {
		t.Fatalf("read = %+v, %v; want %s", img, err, commit)
	}
}

// TestRefusesWhatNamesNoCommit: an image read whole that names no full
// commit, or one that cannot be read at all, fails the read with what was
// wrong, and the command says how to run an engine all the same.
func TestRefusesWhatNamesNoCommit(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(f *fakeRegistry) string
		want  string
	}{
		"no label": {func(f *fakeRegistry) string {
			f.image("unlabelled", map[string]string{"org.opencontainers.image.version": tagMain})
			return "unlabelled"
		}, "carries no " + revisionLabel + " label"},
		"a short commit": {func(f *fakeRegistry) string {
			f.image("short", map[string]string{revisionLabel: commit[:12]})
			return "short"
		}, "no full commit"},
		"an uppercase commit": {func(f *fakeRegistry) string {
			f.image("upper", map[string]string{revisionLabel: strings.ToUpper(commit)})
			return "upper"
		}, "no full commit"},
		"an index of attestations alone": {func(f *fakeRegistry) string {
			f.index("attested",
				[3]string{unknownPlatform, unknownPlatform, f.image("", map[string]string{revisionLabel: commit})})
			return "attested"
		}, "no image of a real platform"},
		"a tag the registry does not have": {func(*fakeRegistry) string { return "gone" },
			"404 Not Found (MANIFEST_UNKNOWN: manifest unknown)"},
		"a token refused": {func(f *fakeRegistry) string {
			f.image(tagMain, map[string]string{revisionLabel: commit})
			f.tokenStatus = http.StatusForbidden
			return tagMain
		}, "asking " + registryHost + " for an anonymous token: 403 Forbidden (DENIED: no anonymous pulls)"},
		"a registry that wants a password": {func(f *fakeRegistry) string {
			f.image(tagMain, map[string]string{revisionLabel: commit})
			f.challenge = `Basic realm="registry"`
			return tagMain
		}, "offers no bearer token"},
		"a token realm over plain HTTP": {func(f *fakeRegistry) string {
			f.image(tagMain, map[string]string{revisionLabel: commit})
			f.challenge = `Bearer realm="http://` + registryHost + `/token",service="` + registryHost + `"`
			return tagMain
		}, "is no https URL"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeRegistry(t)
			tag := c.setup(f)
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), f.fakeClient(), []string{at(":" + tag)}, &stdout, &stderr)
			if code != 1 || stdout.Len() != 0 {
				t.Fatalf("run = %d, printing %q; want 1 and nothing on stdout", code, stdout.String())
			}
			for _, want := range []string{c.want, "CREWLET_VERSION"} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("run says %q, which does not say %q", stderr.String(), want)
				}
			}
		})
	}
}

// TestRefusesContentThatIsNotItsDigest: a manifest or config read by digest
// is that digest's content, or the read fails, so neither a registry nor a
// proxy can hand back another image's labels.
func TestRefusesContentThatIsNotItsDigest(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		f := newFakeRegistry(t)
		digest := f.image(tagMain, map[string]string{revisionLabel: commit})
		var m manifest
		if err := json.Unmarshal(f.manifests[digest].body, &m); err != nil {
			t.Fatal(err)
		}
		f.blobs[m.Config.Digest] = []byte(`{"config":{"Labels":{"` + revisionLabel + `":"` + strings.Repeat("0", 40) + `"}}}`)
		if _, err := read(context.Background(), f.fakeClient(), mustParse(t, at(":main"))); err == nil ||
			!strings.Contains(err.Error(), "answered "+m.Config.Digest+" with content whose digest is") {
			t.Errorf("read of a config that is not its digest = %v, want a refusal", err)
		}
	})
	t.Run("manifest", func(t *testing.T) {
		f := newFakeRegistry(t)
		digest := f.image(tagMain, map[string]string{revisionLabel: commit})
		other := "sha256:" + strings.Repeat("1", 64)
		f.manifests[other] = f.manifests[digest]
		if _, err := read(context.Background(), f.fakeClient(), mustParse(t, at("@"+other))); err == nil ||
			!strings.Contains(err.Error(), "answered "+other+" with a manifest whose digest is "+digest) {
			t.Errorf("read of a manifest that is not the digest asked for = %v, want a refusal", err)
		}
	})
	t.Run("platform manifest", func(t *testing.T) {
		f := newFakeRegistry(t)
		img := f.image("", map[string]string{revisionLabel: commit})
		other := "sha256:" + strings.Repeat("2", 64)
		f.manifests[other] = f.manifests[img]
		f.index(tagMain, [3]string{linux, amd64, other}, [3]string{linux, arm64, other})
		if _, err := read(context.Background(), f.fakeClient(), mustParse(t, at(":main"))); err == nil ||
			!strings.Contains(err.Error(), "answered "+other+" with content whose digest is "+img) {
			t.Errorf("read of a platform's manifest that is not its digest = %v, want a refusal", err)
		}
	})
}

// TestParseReference: a reference names its registry, and its tag or digest;
// Docker's shorthands for either are refused rather than guessed.
func TestParseReference(t *testing.T) {
	const ghcr = "ghcr.io"
	digest := "sha256:" + strings.Repeat("ab", 32)
	for in, want := range map[string]reference{
		"ghcr.io/crewlet/crewlet:main":           {host: ghcr, repository: repository, tag: tagMain},
		"localhost:5000/crewlet:v0.2.0":          {host: "localhost:5000", repository: "crewlet", tag: "v0.2.0"},
		"localhost/a/b/c:x":                      {host: "localhost", repository: "a/b/c", tag: "x"},
		"registry.example.com/crewlet@" + digest: {host: registryHost, repository: "crewlet", digest: digest},
		"ghcr.io/crewlet/crewlet:main@" + digest: {host: ghcr, repository: repository, tag: tagMain, digest: digest},
	} {
		got, err := parseReference(in)
		if err != nil || got != want {
			t.Errorf("parseReference(%s) = %+v, %v; want %+v", in, got, err, want)
		}
		if err == nil && got.String() != in {
			t.Errorf("%+v prints as %s, want %s", got, got.String(), in)
		}
	}
	for in, want := range map[string]string{
		"crewlet/crewlet:main":            "names no registry host",
		"crewlet:main":                    "names no registry host",
		"ghcr.io/crewlet/crewlet":         "names no tag or digest",
		"ghcr.io/crewlet/crewlet@sha256:": "is not a sha256 digest",
		"ghcr.io/crewlet/crewlet@md5:abc": "is not a sha256 digest",
		"ghcr.io/Crewlet/crewlet:main":    "is not a repository",
		"ghcr.io/crewlet//crewlet:main":   "is not a repository",
		"ghcr.io/crewlet/crewlet:-main":   "is not a tag",
	} {
		if _, err := parseReference(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseReference(%s) = %v, want an error saying %q", in, err, want)
		}
	}
}

// TestBearerChallenge reads the parameters of the challenges registries send,
// a comma inside a quoted scope and an escaped quote included.
func TestBearerChallenge(t *testing.T) {
	const realm, scope = "realm", "scope"
	for in, want := range map[string]map[string]string{
		`Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:crewlet/crewlet:pull"`: {
			realm: "https://ghcr.io/token", "service": "ghcr.io", scope: "repository:crewlet/crewlet:pull",
		},
		`bearer realm="https://r/token", scope="repository:a:pull,push"`: {
			realm: "https://r/token", scope: "repository:a:pull,push",
		},
		`Bearer realm="https://r/t\"oken",error=insufficient_scope`: {
			realm: `https://r/t"oken`, "error": "insufficient_scope",
		},
	} {
		got, ok := bearerChallenge(in)
		if !ok || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("bearerChallenge(%s) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{
		``, `Basic realm="r"`, `Bearer service="s"`, `Bearer realm="unterminated`, `Bearer realm`,
	} {
		if got, ok := bearerChallenge(in); ok {
			t.Errorf("bearerChallenge(%s) = %v, want no challenge", in, got)
		}
	}
}

// TestUsage: the command takes one reference.
func TestUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), http.DefaultClient, nil, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "usage") {
		t.Errorf("run with no reference = %d, %q; want 2 and its usage", code, stderr.String())
	}
}

func mustParse(t *testing.T, s string) reference {
	t.Helper()
	ref, err := parseReference(s)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
