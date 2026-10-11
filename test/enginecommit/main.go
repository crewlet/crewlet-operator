// Command enginecommit prints the crewlet commit an engine image was built
// from, as the image's org.opencontainers.image.revision label names it in
// its registry:
//
//	go run ./test/enginecommit ghcr.io/crewlet/crewlet:main
//
// It prints the commit's forty hex digits on stdout and, on stderr, the image
// it read them from, by digest. `make crewlet` installs the engine binary at
// that commit, so that every suite that runs the binary runs the engine the
// e2e suite runs as an image (CREWLET_IMG): one engine, named by the image.
//
// WHY THE IMAGE AND NOT THE GO MODULE PROXY'S @main. crewlet's main moves
// before its image does: the image is built minutes after a merge, and not at
// all for a merge that starts no workflow (a Dependabot bump) or one whose
// build fails. `go install …@main` would then build a commit no image of main
// carries, and the envtest suites would pass or fail on another engine than
// the one the e2e suite ran.
//
// WHY THE REGISTRY'S API AND NOT A CONTAINER TOOL. `make test` runs where no
// container tool does (an IDE's test run, a laptop without docker), and the
// image is public: its registry answers an anonymous read over HTTPS, which
// the standard library speaks. The read is the distribution API's: a manifest
// at the tag or digest, the token its challenge asks for, a platform's
// manifest when the tag names an index of them, and the image's config,
// which holds the labels. Of an index's platforms, this machine's on linux is
// read, the one `make crewlet-image` pulls for the e2e suite, so the binary is
// the commit of the image this machine runs even should a build ever label
// its platforms apart; with none for this machine, the first real platform.
// An index also lists attestations, under the platform unknown/unknown, which
// are not images. Each manifest and config read by digest is checked against
// it, so a registry or a proxy cannot hand back another image's labels
// unnoticed. A registry on this machine (localhost, or a loopback address) is
// read over plain HTTP, as Docker reads one, since that is how one runs
// unless somebody gave it a certificate for localhost.
package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// revisionLabel is the OCI annotation an image's build records its source
// commit under; crewlet's image workflows set it to the full commit.
const revisionLabel = "org.opencontainers.image.revision"

// timeout bounds the whole read: four small requests and a token, which take
// well under a second; the margin is for a slow registry or proxy, and a
// registry that does not answer within it fails the make target rather than
// holding it.
const timeout = time.Minute

// maxBody bounds a manifest, an index or a config the program reads: each is
// a few kilobytes, and a response past the bound is not one of them.
const maxBody = 4 << 20

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	code := run(ctx, &http.Client{}, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

// run is the command, over client, so a test can point it at a registry of
// its own. A plain http.Client uses the default transport, which honours
// HTTPS_PROXY and drops the Authorization header on a redirect to another
// host, as a registry's redirect of a blob to its storage needs.
func run(ctx context.Context, client *http.Client, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr,
			"usage: enginecommit <registry>/<repository>:<tag>, or <registry>/<repository>@sha256:<digest>")
		return 2
	}
	ref, err := parseReference(args[0])
	if err == nil {
		var img image
		if img, err = read(ctx, client, ref); err == nil {
			_, _ = fmt.Fprintf(stderr, "%s is %s, built from crewlet commit %s\n", ref, img.digest, img.revision)
			_, _ = fmt.Fprintln(stdout, img.revision)
			return 0
		}
	}
	_, _ = fmt.Fprintf(stderr, "reading the crewlet commit of %s: %v\n"+
		"Set CREWLET_VERSION to a crewlet commit or module version to run that engine instead.\n", args[0], err)
	return 1
}

// reference is an image reference: a registry host, a repository on it, and
// a tag or a digest. A digest, when both are given, is what is read.
type reference struct {
	host, repository, tag, digest string
}

func (r reference) String() string {
	s := r.host + "/" + r.repository
	if r.tag != "" {
		s += ":" + r.tag
	}
	if r.digest != "" {
		s += "@" + r.digest
	}
	return s
}

// target is what the reference's manifest is read at.
func (r reference) target() string {
	if r.digest != "" {
		return r.digest
	}
	return r.tag
}

var (
	pathComponent = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	tagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// parseReference reads an image reference that names its registry and its
// tag or digest. Docker's shorthands are refused rather than guessed: a name
// with no registry host would be Docker Hub's, which publishes no crewlet
// engine, and a name with no tag would be latest, which names no commit of
// main, so either is more likely a mistake than a choice.
func parseReference(s string) (reference, error) {
	var ref reference
	name, digest, hasDigest := strings.Cut(s, "@")
	if hasDigest {
		if !digestPattern.MatchString(digest) {
			return ref, fmt.Errorf("%q is not a sha256 digest", digest)
		}
		ref.digest = digest
	}
	host, path, ok := strings.Cut(name, "/")
	if !ok || (!strings.ContainsAny(host, ".:") && host != "localhost") {
		return ref, fmt.Errorf("%q names no registry host: write it as <registry>/<repository>:<tag>", s)
	}
	ref.host = host
	if i := strings.LastIndex(path, ":"); i >= 0 {
		ref.tag, path = path[i+1:], path[:i]
		if !tagPattern.MatchString(ref.tag) {
			return ref, fmt.Errorf("%q is not a tag", ref.tag)
		}
	}
	for component := range strings.SplitSeq(path, "/") {
		if !pathComponent.MatchString(component) {
			return ref, fmt.Errorf("%q is not a repository", path)
		}
	}
	ref.repository = path
	if ref.tag == "" && ref.digest == "" {
		return ref, fmt.Errorf("%q names no tag or digest", s)
	}
	return ref, nil
}

// image is what was read: the digest of the manifest the reference names,
// an index or an image's, and the commit the image was built from.
type image struct {
	digest, revision string
}

// The manifest types a reference can name: an index of platforms' images, in
// OCI's type or Docker's, or one image's manifest.
const (
	ociIndex        = "application/vnd.oci.image.index.v1+json"
	dockerList      = "application/vnd.docker.distribution.manifest.list.v2+json"
	ociManifest     = "application/vnd.oci.image.manifest.v1+json"
	dockerManifest  = "application/vnd.docker.distribution.manifest.v2+json"
	unknownPlatform = "unknown"
)

var manifestTypes = []string{ociIndex, dockerList, ociManifest, dockerManifest}

// manifest is the part of an index or an image manifest read here.
type manifest struct {
	MediaType string `json:"mediaType"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
}

// read reads the revision label of the image ref names.
func read(ctx context.Context, client *http.Client, ref reference) (image, error) {
	r := &registry{client: client, ref: ref}
	body, kind, err := r.manifest(ctx, ref.target())
	if err != nil {
		return image{}, err
	}
	img := image{digest: digestOf(body)}
	if ref.digest != "" && img.digest != ref.digest {
		return image{}, fmt.Errorf("the registry answered %s with a manifest whose digest is %s", ref.digest, img.digest)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return image{}, fmt.Errorf("decoding the manifest of %s: %w", ref, err)
	}
	if kind == "" {
		kind = m.MediaType
	}
	switch kind {
	case ociIndex, dockerList:
		platform, err := pickPlatform(m)
		if err != nil {
			return image{}, fmt.Errorf("%s: %w", ref, err)
		}
		if body, err = r.verified(ctx, "manifests", platform, manifestTypes); err != nil {
			return image{}, err
		}
		m = manifest{}
		if err := json.Unmarshal(body, &m); err != nil {
			return image{}, fmt.Errorf("decoding the manifest %s of %s: %w", platform, ref, err)
		}
	case ociManifest, dockerManifest:
	default:
		return image{}, fmt.Errorf("%s is a %q, which is no image manifest or index", ref, kind)
	}
	if !digestPattern.MatchString(m.Config.Digest) {
		return image{}, fmt.Errorf("the manifest of %s names no sha256 config (%q)", ref, m.Config.Digest)
	}
	config, err := r.verified(ctx, "blobs", m.Config.Digest, nil)
	if err != nil {
		return image{}, err
	}
	var labels struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(config, &labels); err != nil {
		return image{}, fmt.Errorf("decoding the config of %s: %w", ref, err)
	}
	revision, ok := labels.Config.Labels[revisionLabel]
	if !ok {
		return image{}, fmt.Errorf("%s carries no %s label", ref, revisionLabel)
	}
	if !commitPattern.MatchString(revision) {
		return image{}, fmt.Errorf("%s labels its %s %q, which is no full commit (forty lowercase hex digits)",
			ref, revisionLabel, revision)
	}
	img.revision = revision
	return img, nil
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pickPlatform is the digest of the index's image for this machine's
// architecture on linux, else of its first real platform's.
func pickPlatform(index manifest) (string, error) {
	first := ""
	for _, m := range index.Manifests {
		p := m.Platform
		if p.OS == "" || p.OS == unknownPlatform || p.Architecture == unknownPlatform {
			continue
		}
		if p.OS == "linux" && p.Architecture == runtime.GOARCH {
			return m.Digest, nil
		}
		if first == "" {
			first = m.Digest
		}
	}
	if first == "" {
		return "", errors.New("the index lists no image of a real platform")
	}
	return first, nil
}

// registry reads one repository, anonymously.
type registry struct {
	client *http.Client
	ref    reference
	// token is the bearer token the registry's challenge was answered with,
	// once it challenged.
	token string
}

// manifest is the manifest at target, a tag or a digest, and its media type.
func (r *registry) manifest(ctx context.Context, target string) (body []byte, kind string, err error) {
	resp, err := r.get(ctx, "manifests", target, manifestTypes)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if body, err = readBody(resp); err != nil {
		return nil, "", err
	}
	kind, _, _ = mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(kind, "application/vnd.") {
		// A registry that labels the body plainly leaves its own
		// mediaType field to say what it is.
		kind = ""
	}
	return body, kind, nil
}

// verified is the manifest or blob at digest, checked against it.
func (r *registry) verified(ctx context.Context, kind, digest string, accept []string) ([]byte, error) {
	resp, err := r.get(ctx, kind, digest, accept)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	if got := digestOf(body); got != digest {
		return nil, fmt.Errorf("the registry answered %s with content whose digest is %s", digest, got)
	}
	return body, nil
}

// get requests /v2/<repository>/<kind>/<target>, answering the registry's
// bearer challenge once with an anonymous token for a pull.
func (r *registry) get(ctx context.Context, kind, target string, accept []string) (*http.Response, error) {
	scheme := "https"
	if loopback(r.ref.host) {
		scheme = "http"
	}
	u := fmt.Sprintf("%s://%s/v2/%s/%s/%s", scheme, r.ref.host, r.ref.repository, kind, target)
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if len(accept) > 0 {
			req.Header.Set("Accept", strings.Join(accept, ", "))
		}
		if r.token != "" {
			req.Header.Set("Authorization", "Bearer "+r.token)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.Header.Get("WWW-Authenticate")
			_ = resp.Body.Close()
			if err := r.authenticate(ctx, challenge); err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("GET %s: %s%s", u, resp.Status, registryError(resp))
			_ = resp.Body.Close()
			return nil, err
		}
		return resp, nil
	}
}

// loopback reports whether a registry host, with its port or without, is
// this machine.
func loopback(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// authenticate takes the anonymous pull token a bearer challenge names.
func (r *registry) authenticate(ctx context.Context, challenge string) error {
	params, ok := bearerChallenge(challenge)
	if !ok {
		return fmt.Errorf("the registry refused an anonymous read and offers no bearer token (%q)", challenge)
	}
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return fmt.Errorf("the registry's token realm %q is no https URL", params["realm"])
	}
	query := realm.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + r.ref.repository + ":pull"
	}
	query.Set("scope", scope)
	realm.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm.String(), nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asking %s for an anonymous token: %s%s", realm.Host, resp.Status, registryError(resp))
	}
	body, err := readBody(resp)
	if err != nil {
		return err
	}
	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return fmt.Errorf("decoding the token from %s: %w", realm.Host, err)
	}
	if r.token = cmp.Or(token.Token, token.AccessToken); r.token == "" {
		return fmt.Errorf("%s answered with no token", realm.Host)
	}
	return nil
}

// bearerChallenge is the parameters of a WWW-Authenticate header's Bearer
// challenge (RFC 6750): realm, service and scope, each a quoted string, which
// may hold a comma (a scope of several actions does).
func bearerChallenge(header string) (map[string]string, bool) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return nil, false
	}
	params := map[string]string{}
	for {
		rest = strings.TrimLeft(rest, " ,")
		if rest == "" {
			return params, params["realm"] != ""
		}
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			return nil, false
		}
		value, tail, ok := challengeValue(after)
		if !ok {
			return nil, false
		}
		params[strings.ToLower(strings.TrimSpace(key))] = value
		rest = tail
	}
}

// challengeValue reads one parameter's value off the front of s: a quoted
// string, with its backslash escapes, or a token up to the next comma.
func challengeValue(s string) (value, rest string, ok bool) {
	if !strings.HasPrefix(s, `"`) {
		value, rest, _ = strings.Cut(s, ",")
		return strings.TrimSpace(value), rest, true
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i++; i < len(s) {
				b.WriteByte(s[i])
			}
		case '"':
			return b.String(), s[i+1:], true
		default:
			b.WriteByte(s[i])
		}
	}
	return "", "", false
}

// registryError is the first message of a registry's error body, which says
// what a bare status does not (MANIFEST_UNKNOWN, DENIED), or nothing.
func registryError(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return ""
	}
	var answer struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &answer) != nil || len(answer.Errors) == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s: %s)", answer.Errors[0].Code, answer.Errors[0].Message)
}

// readBody reads a response's body, up to maxBody.
func readBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", resp.Request.URL, err)
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("%s answered with more than %d bytes, which is no manifest or config",
			resp.Request.URL, maxBody)
	}
	return body, nil
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
