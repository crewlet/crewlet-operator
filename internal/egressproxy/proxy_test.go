package egressproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/internal/egresstoken"
)

const (
	tenantNS   = "t-abc"
	tenantUID  = "uid-abc"
	tenantID   = "abc"
	company    = "acme"
	apiHost    = "api.example.com"
	apiDest    = apiHost + ":443"
	addrDenied = "destination address denied"
	// publicIP stands for a destination on the internet; the test dialer
	// sends it to a local server.
	publicIP = "203.0.113.10"
)

var signingKey = []byte("test-signing-key")

type staticKeys struct{}

func (staticKeys) Key() ([]byte, error) { return signingKey, nil }

type fakeDirectory map[netip.Addr]Source

func (d fakeDirectory) Lookup(a netip.Addr) (Source, bool) {
	s, ok := d[a.Unmap()]
	return s, ok
}

// fakeResolver answers only fully qualified names, as a resolver with a
// search list would answer a short one differently.
type fakeResolver map[string][]netip.Addr

func (r fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	name, fqdn := strings.CutSuffix(host, ".")
	if a, ok := r[name]; ok && fqdn {
		return a, nil
	}
	return nil, errors.New("no such host")
}

func tenantSource() Source {
	return Source{
		Namespace:    tenantNS,
		NamespaceUID: tenantUID,
		NamespaceLabels: map[string]string{
			egresstoken.LabelTenant:    tenantID,
			egresstoken.LabelTrustTier: "t1",
		},
		Pod: "acme-seats-0",
		PodLabels: map[string]string{
			"crewlet.ai/company":          company,
			"app.kubernetes.io/component": "seats",
			"crewlet.ai/seat-pool":        "builders",
		},
	}
}

type harness struct {
	t       *testing.T
	proxy   *httptest.Server
	logBuf  *syncBuffer
	backend string // address the test dialer sends publicIP to
	dialed  []string
	// dial, when set, replaces the dialer for every address.
	dial DialFunc
	mu   sync.Mutex
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newHarness(t *testing.T, cfg *Config, src *Source, backend string) *harness {
	t.Helper()
	pol, err := NewPolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, logBuf: &syncBuffer{}, backend: backend}
	dir := fakeDirectory{}
	if src != nil {
		dir[netip.MustParseAddr("127.0.0.1")] = *src
	}
	p := &Proxy{
		Policy:    pol,
		Directory: dir,
		Keys:      staticKeys{},
		Resolver: fakeResolver{
			apiHost:              {netip.MustParseAddr(publicIP)},
			"other.example.org":  {netip.MustParseAddr(publicIP)},
			"internal.example":   {netip.MustParseAddr("10.0.0.5")},
			"mixed.example.com":  {netip.MustParseAddr(publicIP), netip.MustParseAddr("192.168.1.1")},
			"metadata.example":   {netip.MustParseAddr("169.254.169.254")},
			"nat64.example.com":  {netip.MustParseAddr("64:ff9b::a00:5")},
			"mapped.example.com": {netip.MustParseAddr("::ffff:127.0.0.1")},
			"zoned.example.com":  {netip.MustParseAddr("fe80::1%eth0")},
		},
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			h.mu.Lock()
			h.dialed = append(h.dialed, address)
			h.mu.Unlock()
			if h.dial != nil {
				return h.dial(ctx, network, address)
			}
			host, _, _ := net.SplitHostPort(address)
			if host != publicIP || h.backend == "" {
				return nil, errors.New("unreachable in the test")
			}
			var d net.Dialer
			return d.DialContext(ctx, network, h.backend)
		},
		Log:         NewAccessLog(h.logBuf),
		DialTimeout: 5 * time.Second,
	}
	h.proxy = httptest.NewServer(p)
	t.Cleanup(h.proxy.Close)
	return h
}

func credential(ns, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(ns+":"+token))
}

func ownCredential() string {
	return credential(tenantNS, egresstoken.Token(signingKey, tenantNS, tenantUID))
}

// connect sends a CONNECT for dest and returns the response and the
// connection, positioned after the response head.
func (h *harness) connect(dest, auth string) (*http.Response, net.Conn, *bufio.Reader) {
	h.t.Helper()
	conn, err := net.Dial("tcp", h.proxy.Listener.Addr().String())
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	req := "CONNECT " + dest + " HTTP/1.1\r\nHost: " + dest + "\r\n"
	if auth != "" {
		req += "Proxy-Authorization: " + auth + "\r\n"
	}
	if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
		h.t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		h.t.Fatal(err)
	}
	return resp, conn, br
}

// entries reads the access log, waiting briefly for the first line, which
// a connection writes as it closes.
func (h *harness) entries() []Entry {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var out []Entry
		for line := range strings.SplitSeq(strings.TrimSpace(h.logBuf.String()), "\n") {
			if line == "" {
				continue
			}
			var e Entry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				h.t.Fatalf("log line %q: %v", line, err)
			}
			out = append(out, e)
		}
		if len(out) > 0 || time.Now().After(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln.Addr().String()
}

func TestTunnelIsServedAndLogged(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}}, &src, echoServer(t))
	resp, conn, br := h.connect("API.example.com.:443", ownCredential())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status %d", resp.StatusCode)
	}
	if _, err := io.WriteString(conn, "hello"); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "hello" {
		t.Fatalf("echo = %q, %v", got, err)
	}
	_ = conn.Close()
	es := h.entries()
	if len(es) != 1 {
		t.Fatalf("log entries = %d, want 1", len(es))
	}
	e := es[0]
	want := Entry{
		Decision: DecisionAllowed, Tenant: tenantID, Namespace: tenantNS, Company: company,
		Pod: "acme-seats-0", Component: "seats", SeatPool: "builders", TrustTier: "t1",
		Method: http.MethodConnect, Host: apiHost, Port: 443, IP: publicIP,
	}
	if e.Decision != want.Decision || e.Tenant != want.Tenant || e.Namespace != want.Namespace ||
		e.Company != want.Company || e.Pod != want.Pod || e.Component != want.Component ||
		e.SeatPool != want.SeatPool || e.TrustTier != want.TrustTier || e.Method != want.Method ||
		e.Host != want.Host || e.Port != want.Port || e.IP != want.IP {
		t.Errorf("entry = %+v, want fields of %+v", e, want)
	}
	if e.BytesUp != 5 || e.BytesDown != 5 {
		t.Errorf("bytes up/down = %d/%d, want 5/5", e.BytesUp, e.BytesDown)
	}
	if e.Time.IsZero() {
		t.Error("entry has no time")
	}
}

func TestPlainHTTPIsForwardedWithoutTheCredential(t *testing.T) {
	var gotAuth, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Proxy-Authorization"), r.URL.Path
		_, _ = io.WriteString(w, "body")
	}))
	t.Cleanup(backend.Close)
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}}, &src, backend.Listener.Addr().String())
	proxyURL, _ := url.Parse(h.proxy.URL)
	proxyURL.User = url.UserPassword(tenantNS, egresstoken.Token(signingKey, tenantNS, tenantUID))
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := c.Get("http://" + apiHost + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "body" || gotPath != "/x" {
		t.Fatalf("status %d body %q path %q", resp.StatusCode, body, gotPath)
	}
	if gotAuth != "" {
		t.Error("the proxy credential reached the destination")
	}
	e := h.entries()[0]
	if e.Decision != DecisionAllowed || e.Method != http.MethodGet || e.Port != 80 || e.BytesDown == 0 {
		t.Errorf("entry = %+v", e)
	}
}

func TestRefusals(t *testing.T) {
	other := egresstoken.Token(signingKey, "t-other", "uid-other")
	withLabels := func(mod func(map[string]string)) *Source {
		s := tenantSource()
		labels := maps.Clone(s.NamespaceLabels)
		mod(labels)
		s.NamespaceLabels = labels
		return &s
	}
	own := tenantSource()
	literals := Config{AllowIPLiterals: true}
	const zoned, clusterName = "zoned address not allowed", "cluster name not allowed"
	cases := []struct {
		name   string
		cfg    Config
		src    *Source
		dest   string
		auth   string
		status int
		reason string
	}{
		{"unknown source", Config{}, nil, apiDest, ownCredential(), 403, "unknown source"},
		{"no credential", Config{}, &own, apiDest, "", 407, "no credential"},
		{"another namespace's credential", Config{}, &own, apiDest, credential("t-other", other), 407,
			"credential is not the source namespace's"},
		{"another namespace's token", Config{}, &own, apiDest, credential(tenantNS, other), 407, "bad credential"},
		{"not a tenant", Config{}, withLabels(func(l map[string]string) { delete(l, egresstoken.LabelTenant) }),
			apiDest, ownCredential(), 403, "not a tenant namespace"},
		{"quarantined", Config{}, withLabels(func(l map[string]string) { l[egresstoken.LabelQuarantine] = "egress" }),
			apiDest, ownCredential(), 403, "namespace quarantined"},
		{"tier not served", Config{}, withLabels(func(l map[string]string) { l[egresstoken.LabelTrustTier] = "t2" }),
			apiDest, ownCredential(), 403, "trust tier not served"},
		{"port", Config{}, &own, "api.example.com:25", ownCredential(), 403, "port not allowed"},
		{"host not in allowHosts", Config{AllowHosts: []string{"*.example.org"}}, &own, apiDest,
			ownCredential(), 403, "host not allowed"},
		{"suffix pattern needs a subdomain", Config{AllowHosts: []string{"*.api.example.com"}}, &own,
			apiDest, ownCredential(), 403, "host not allowed"},
		{"denied host", Config{DenyHosts: []string{"api.example.com"}}, &own, apiDest,
			ownCredential(), 403, "host denied"},
		{"IP literal", Config{}, &own, publicIP + ":443", ownCredential(), 403, "address destination not allowed"},
		{"name resolving to a private address", Config{}, &own, "internal.example:443", ownCredential(), 403,
			addrDenied},
		{"name with one private address", Config{}, &own, "mixed.example.com:443", ownCredential(), 403,
			addrDenied},
		{"metadata name", Config{}, &own, "metadata.example:80", ownCredential(), 403, addrDenied},
		{"metadata literal even with literals allowed", Config{AllowIPLiterals: true}, &own, "169.254.169.254:80",
			ownCredential(), 403, addrDenied},
		{"IPv6 metadata literal", Config{AllowIPLiterals: true}, &own, "[fd00:ec2::254]:443",
			ownCredential(), 403, addrDenied},
		{"NAT64 of a private address", Config{}, &own, "nat64.example.com:443", ownCredential(), 403,
			addrDenied},
		{"IPv4-mapped loopback", Config{}, &own, "mapped.example.com:443", ownCredential(), 403,
			addrDenied},
		{"configured denied CIDR", Config{DeniedCIDRs: []string{"203.0.113.0/24"}}, &own, apiDest,
			ownCredential(), 403, addrDenied},
		{"unresolvable", Config{}, &own, "nowhere.example.com:443", ownCredential(), 502, "name did not resolve"},
		{"zoned link-local literal", literals, &own, "[fe80::1%25eth0]:443", ownCredential(), 403, zoned},
		{"zoned metadata literal", literals, &own, "[fd00:ec2::254%25eth0]:443", ownCredential(), 403, zoned},
		{"name resolving to a zoned address", Config{}, &own, "zoned.example.com:443", ownCredential(), 403, addrDenied},
		{"single-label name", Config{}, &own, "kubernetes:443", ownCredential(), 403, "single-label name not allowed"},
		{"name under the cluster domain", Config{}, &own, "kubernetes.default.svc.cluster.local:443", ownCredential(),
			403, clusterName},
		{"the cluster domain itself", Config{}, &own, "cluster.local:443", ownCredential(), 403, clusterName},
		{"name under a configured cluster domain", Config{ClusterDomain: "corp.internal"}, &own,
			"db.team.svc.corp.internal:443", ownCredential(), 403, clusterName},
		{"IPv4-compatible IPv6", literals, &own, "[::a00:5]:443", ownCredential(), 403, addrDenied},
		{"IPv6 discard-only", literals, &own, "[100::1]:443", ownCredential(), 403, addrDenied},
		{"IPv6 documentation", literals, &own, "[2001:db8::1]:443", ownCredential(), 403, addrDenied},
		{"6to4", literals, &own, "[2002:a00:5::1]:443", ownCredential(), 403, addrDenied},
		{"Teredo", literals, &own, "[2001:0:a00:5::1]:443", ownCredential(), 403, addrDenied},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.cfg.TrustTiers == nil {
				c.cfg.TrustTiers = []string{"t1"}
			}
			h := newHarness(t, &c.cfg, c.src, echoServer(t))
			resp, _, br := h.connect(c.dest, c.auth)
			body, _ := io.ReadAll(io.LimitReader(br, resp.ContentLength))
			if resp.StatusCode != c.status {
				t.Fatalf("status %d, want %d (%s)", resp.StatusCode, c.status, body)
			}
			if !strings.Contains(string(body), c.reason) {
				t.Errorf("body %q, want %q", body, c.reason)
			}
			if c.status == 407 && resp.Header.Get("Proxy-Authenticate") == "" {
				t.Error("a 407 without Proxy-Authenticate")
			}
			if len(h.dialed) != 0 {
				t.Errorf("dialed %v for a refused request", h.dialed)
			}
			es := h.entries()
			if len(es) != 1 || es[0].Decision != DecisionDenied || es[0].Reason != c.reason {
				t.Fatalf("log = %+v, want one denial for %q", es, c.reason)
			}
			if c.src != nil && es[0].Namespace != tenantNS {
				t.Errorf("denial logged namespace %q", es[0].Namespace)
			}
			if raw := h.logBuf.String(); c.auth != "" && strings.Contains(raw, strings.TrimPrefix(c.auth, "Basic ")) {
				t.Error("the credential was logged")
			}
		})
	}
}

func TestAllowedHostsAdmitTheirNames(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, AllowHosts: []string{"*.example.com", "other.example.org"}},
		&src, echoServer(t))
	for _, dest := range []string{apiDest, "other.example.org:443"} {
		if resp, _, _ := h.connect(dest, ownCredential()); resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", dest, resp.StatusCode)
		}
	}
}

func TestIPLiteralAllowedWhenConfigured(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, AllowIPLiterals: true}, &src, echoServer(t))
	if resp, _, _ := h.connect(publicIP+":443", ownCredential()); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestIdleTunnelIsClosed(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, IdleTimeoutSeconds: 1}, &src, echoServer(t))
	resp, conn, br := h.connect(apiDest, ownCredential())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("read after idling = %v, want EOF", err)
	}
	if e := h.entries()[0]; e.Reason != "idle timeout" {
		t.Errorf("reason %q, want idle timeout", e.Reason)
	}
}

func TestNewPolicyRefusesBadConfig(t *testing.T) {
	for _, c := range []*Config{
		{},
		{TrustTiers: []string{"t1"}, Ports: []int{0}},
		{TrustTiers: []string{"t1"}, AllowHosts: []string{"*"}},
		{TrustTiers: []string{"t1"}, DenyHosts: []string{"a.*.com"}},
		{TrustTiers: []string{"t1"}, DeniedCIDRs: []string{"not-a-cidr"}},
	} {
		if _, err := NewPolicy(c); err == nil {
			t.Errorf("NewPolicy(%+v) accepted", c)
		}
	}
}

func TestHealth(t *testing.T) {
	ready := false
	h := HealthHandler(func() bool { return ready })
	for _, want := range []int{http.StatusServiceUnavailable, http.StatusOK} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != want {
			t.Errorf("ready=%v: status %d, want %d", ready, rec.Code, want)
		}
		ready = true
	}
}

// rawRequest sends one request head as written and returns the response
// head; the connection stays open, its body unread, until the test ends.
func (h *harness) rawRequest(head string) *http.Response {
	h.t.Helper()
	conn, err := net.Dial("tcp", h.proxy.Listener.Addr().String())
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, head); err != nil {
		h.t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// awaitReason waits for the first log line and returns its reason.
func (h *harness) awaitReason(within time.Duration) (string, bool) {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if raw := strings.TrimSpace(h.logBuf.String()); raw != "" {
			var e Entry
			if err := json.Unmarshal([]byte(strings.SplitN(raw, "\n", 2)[0]), &e); err != nil {
				h.t.Fatal(err)
			}
			return e.Reason, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "", false
}

func TestZonedPlainHTTPIsRefused(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, AllowIPLiterals: true}, &src, echoServer(t))
	resp := h.rawRequest("GET http://[fd00:ec2::254%25eth0]/ HTTP/1.1\r\nHost: [fd00:ec2::254%25eth0]\r\n" +
		"Proxy-Authorization: " + ownCredential() + "\r\n\r\n")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
	if len(h.dialed) != 0 {
		t.Errorf("dialed %v", h.dialed)
	}
	if e := h.entries()[0]; e.Reason != "zoned address not allowed" {
		t.Errorf("reason %q", e.Reason)
	}
}

func TestConnectionCaps(t *testing.T) {
	src := tenantSource()
	t.Run("per namespace", func(t *testing.T) {
		h := newHarness(t, &Config{TrustTiers: []string{"t1"}, MaxConnectionsPerNamespace: 1}, &src, echoServer(t))
		first, conn, _ := h.connect(apiDest, ownCredential())
		if first.StatusCode != http.StatusOK {
			t.Fatalf("first: status %d", first.StatusCode)
		}
		second, _, _ := h.connect(apiDest, ownCredential())
		if second.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("second: status %d, want 429", second.StatusCode)
		}
		if e := h.entries()[0]; e.Reason != "namespace connection limit reached" {
			t.Errorf("reason %q", e.Reason)
		}
		_ = conn.Close()
		// The first tunnel's slot is free once its handler returns.
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, _, _ := h.connect(apiDest, ownCredential())
			if resp.StatusCode == http.StatusOK {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("after the first closed: status %d", resp.StatusCode)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	t.Run("overall", func(t *testing.T) {
		h := newHarness(t, &Config{TrustTiers: []string{"t1"}, MaxConnections: 1}, &src, echoServer(t))
		if first, _, _ := h.connect(apiDest, ownCredential()); first.StatusCode != http.StatusOK {
			t.Fatalf("first: status %d", first.StatusCode)
		}
		second, _, _ := h.connect(apiDest, ownCredential())
		if second.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("second: status %d, want 503", second.StatusCode)
		}
		if e := h.entries()[0]; e.Reason != "proxy connection limit reached" {
			t.Errorf("reason %q", e.Reason)
		}
	})
}

func TestNewServerBoundsIdleClients(t *testing.T) {
	s := NewServer(":0", http.NotFoundHandler())
	if s.IdleTimeout != time.Minute || s.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("idle %v, read header %v; want 1m and 10s", s.IdleTimeout, s.ReadHeaderTimeout)
	}
}

// plainGet sends a plain http GET through the proxy and returns once the
// response head is read, leaving the body unread.
func (h *harness) plainGet() {
	h.t.Helper()
	resp := h.rawRequest("GET http://" + apiHost + "/ HTTP/1.1\r\nHost: " + apiHost + "\r\n" +
		"Proxy-Authorization: " + ownCredential() + "\r\n\r\n")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestForwardEndsWhenTheClientStopsReading(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, 64*1024)
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(backend.Close)
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, IdleTimeoutSeconds: 1}, &src, backend.Listener.Addr().String())
	h.plainGet()
	reason, ok := h.awaitReason(20 * time.Second)
	if !ok || reason != "client write failed" {
		t.Fatalf("reason %q (logged %v), want client write failed", reason, ok)
	}
}

func TestForwardEndsWhenTheDestinationStalls(t *testing.T) {
	stall := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "start")
		_ = http.NewResponseController(w).Flush()
		<-stall
	}))
	t.Cleanup(backend.Close)
	t.Cleanup(func() { close(stall) })
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, IdleTimeoutSeconds: 1}, &src, backend.Listener.Addr().String())
	h.plainGet()
	reason, ok := h.awaitReason(10 * time.Second)
	if !ok || reason != "idle timeout" {
		t.Fatalf("reason %q (logged %v), want idle timeout", reason, ok)
	}
}

func TestBufferedConnectBytesDoNotHoldTheHandler(t *testing.T) {
	src := tenantSource()
	h := newHarness(t, &Config{TrustTiers: []string{"t1"}, IdleTimeoutSeconds: 1}, &src, "")
	// A destination that accepts and never reads: a pipe whose far end
	// nobody drains blocks every write until its deadline.
	near, far := net.Pipe()
	t.Cleanup(func() { _ = far.Close() })
	h.dial = func(context.Context, string, string) (net.Conn, error) { return near, nil }
	conn, err := net.Dial("tcp", h.proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	// The request head and the first tunnelled bytes in one write, as a
	// client that pipelines its TLS hello sends them.
	if _, err := io.WriteString(conn, "CONNECT "+apiDest+" HTTP/1.1\r\nHost: "+apiDest+"\r\n"+
		"Proxy-Authorization: "+ownCredential()+"\r\n\r\nhello"); err != nil {
		t.Fatal(err)
	}
	reason, ok := h.awaitReason(10 * time.Second)
	if !ok || reason != "upstream write failed" {
		t.Fatalf("reason %q (logged %v), want upstream write failed", reason, ok)
	}
}
