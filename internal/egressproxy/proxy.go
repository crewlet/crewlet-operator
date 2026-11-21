package egressproxy

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/crewlet/crewlet-operator/internal/egresstoken"
)

// Resolver resolves a destination name; *net.Resolver is one.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Keys hands out the current signing key; *KeyFile is one.
type Keys interface {
	Key() ([]byte, error)
}

// DialFunc opens a connection to a checked address.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Proxy is the HTTP handler that serves CONNECT tunnels and plain HTTP
// requests in absolute form.
type Proxy struct {
	Policy    *Policy
	Directory Directory
	Keys      Keys
	Resolver  Resolver
	Dial      DialFunc
	Log       *AccessLog
	// DialTimeout bounds name resolution and the connection to the
	// destination.
	DialTimeout time.Duration
}

// refusal is a request the proxy turns away, with the status and the reason
// both the client and the log see.
type refusal struct {
	status int
	reason string
}

func refuse(status int, reason string) *refusal { return &refusal{status: status, reason: reason} }

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	e := &Entry{Time: start.UTC(), Method: r.Method, Decision: DecisionDenied}
	defer func() {
		e.DurationMs = time.Since(start).Milliseconds()
		p.Log.Write(e)
	}()

	host, port, ref := p.admit(r, e)
	if ref != nil {
		p.writeRefusal(w, e, ref)
		return
	}
	upstream, ref := p.connect(r.Context(), host, port, e)
	if ref != nil {
		p.writeRefusal(w, e, ref)
		return
	}
	counted := &countingConn{Conn: upstream}
	defer func() {
		e.BytesUp, e.BytesDown = counted.up.Load(), counted.down.Load()
	}()
	end := start.Add(p.Policy.maxConnection)
	e.Decision = DecisionAllowed
	if r.Method == http.MethodConnect {
		p.tunnel(w, counted, end, e)
		return
	}
	p.forward(w, r, counted, end, e)
}

// admit names the source, checks its credential and namespace, and checks
// the destination's form, port and host. It fills e as it learns each.
func (p *Proxy) admit(r *http.Request, e *Entry) (string, int, *refusal) {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return "", 0, refuse(http.StatusForbidden, "unknown source")
	}
	src, ok := p.Directory.Lookup(addrPort.Addr())
	if !ok {
		return "", 0, refuse(http.StatusForbidden, "unknown source")
	}
	e.Namespace, e.Pod = src.Namespace, src.Pod
	e.Tenant = src.NamespaceLabels[egresstoken.LabelTenant]
	e.TrustTier = src.NamespaceLabels[egresstoken.LabelTrustTier]
	e.Company = src.PodLabels["crewlet.ai/company"]
	e.Component = src.PodLabels["app.kubernetes.io/component"]
	e.SeatPool = src.PodLabels["crewlet.ai/seat-pool"]

	if ref := p.authenticate(r, src); ref != nil {
		return "", 0, ref
	}
	if e.Tenant == "" {
		return "", 0, refuse(http.StatusForbidden, "not a tenant namespace")
	}
	if _, quarantined := src.NamespaceLabels[egresstoken.LabelQuarantine]; quarantined {
		return "", 0, refuse(http.StatusForbidden, "namespace quarantined")
	}
	if !p.Policy.tiers[e.TrustTier] {
		return "", 0, refuse(http.StatusForbidden, "trust tier not served")
	}

	host, port, ref := target(r)
	if ref != nil {
		return "", 0, ref
	}
	e.Host, e.Port = host, port
	if !p.Policy.ports[port] {
		return "", 0, refuse(http.StatusForbidden, "port not allowed")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		if !p.Policy.allowIPLiterals {
			return "", 0, refuse(http.StatusForbidden, "address destination not allowed")
		}
	} else if reason := p.Policy.hostRefusal(host); reason != "" {
		return "", 0, refuse(http.StatusForbidden, reason)
	}
	return host, port, nil
}

// authenticate checks the Basic credential: the source pod's own namespace
// as the user name, and that namespace's token as the password. Requiring
// the source's namespace, and not just any valid pair, means a credential
// copied out of one tenant does nothing from another.
func (p *Proxy) authenticate(r *http.Request, src Source) *refusal {
	user, token, ok := proxyBasicAuth(r.Header.Get("Proxy-Authorization"))
	if !ok {
		return refuse(http.StatusProxyAuthRequired, "no credential")
	}
	if user != src.Namespace {
		return refuse(http.StatusProxyAuthRequired, "credential is not the source namespace's")
	}
	key, err := p.Keys.Key()
	if err != nil {
		return refuse(http.StatusServiceUnavailable, "signing key unavailable")
	}
	if !egresstoken.Verify(key, src.Namespace, src.NamespaceUID, token) {
		return refuse(http.StatusProxyAuthRequired, "bad credential")
	}
	return nil
}

func proxyBasicAuth(header string) (user, pass string, ok bool) {
	scheme, encoded, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", "", false
	}
	return strings.Cut(string(raw), ":")
}

// target is the destination host and port of a CONNECT (its authority) or
// of a plain http request in absolute form. https in absolute form is not
// served: a client tunnels it with CONNECT.
func target(r *http.Request) (string, int, *refusal) {
	var hostport string
	switch {
	case r.Method == http.MethodConnect:
		hostport = r.Host
	case r.URL.IsAbs() && r.URL.Scheme == "http":
		hostport = r.URL.Host
		if r.URL.Port() == "" {
			hostport = net.JoinHostPort(r.URL.Hostname(), "80")
		}
	default:
		return "", 0, refuse(http.StatusBadRequest, "not a proxy request")
	}
	h, ps, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, refuse(http.StatusBadRequest, "destination needs a host and port")
	}
	port, err := strconv.Atoi(ps)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, refuse(http.StatusBadRequest, "bad destination port")
	}
	h = normalizeHost(h)
	if h == "" {
		return "", 0, refuse(http.StatusBadRequest, "destination needs a host and port")
	}
	return h, port, nil
}

// connect resolves host once, refuses it if any address it resolves to is
// denied, and dials the checked addresses themselves, so a second lookup
// (DNS rebinding) cannot swap in an address that was never checked.
func (p *Proxy) connect(ctx context.Context, host string, port int, e *Entry) (net.Conn, *refusal) {
	ctx, cancel := context.WithTimeout(ctx, p.DialTimeout)
	defer cancel()
	var addrs []netip.Addr
	if addr, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{addr}
	} else {
		resolved, err := p.Resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return nil, refuse(http.StatusBadGateway, "name did not resolve")
		}
		addrs = resolved
	}
	for _, a := range addrs {
		if p.Policy.addressDenied(a) {
			e.IP = a.Unmap().String()
			return nil, refuse(http.StatusForbidden, "destination address denied")
		}
	}
	for _, a := range addrs {
		e.IP = a.Unmap().String()
		conn, err := p.Dial(ctx, "tcp", netip.AddrPortFrom(a.Unmap(), uint16(port)).String()) //nolint:gosec // G115: port was checked to be 1 to 65535.
		if err == nil {
			return conn, nil
		}
	}
	return nil, refuse(http.StatusBadGateway, "connection failed")
}

func (p *Proxy) writeRefusal(w http.ResponseWriter, e *Entry, ref *refusal) {
	e.Decision, e.Reason = DecisionDenied, ref.reason
	if ref.status == http.StatusProxyAuthRequired {
		w.Header().Set("Proxy-Authenticate", `Basic realm="crewlet-egress"`)
	}
	http.Error(w, ref.reason, ref.status)
}

// tunnel answers a CONNECT and copies bytes both ways until either side
// closes, the connection idles out, or it reaches its end.
func (p *Proxy) tunnel(w http.ResponseWriter, upstream net.Conn, end time.Time, e *Entry) {
	defer func() { _ = upstream.Close() }()
	hj, ok := w.(http.Hijacker)
	if !ok {
		e.Decision, e.Reason = DecisionDenied, "tunnel unsupported"
		http.Error(w, "tunnel unsupported", http.StatusInternalServerError)
		return
	}
	client, rw, err := hj.Hijack()
	if err != nil {
		e.Reason = "hijack failed"
		return
	}
	defer func() { _ = client.Close() }()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		e.Reason = "client gone"
		return
	}
	// Bytes the client sent right after the CONNECT (a pipelined TLS
	// hello) are already in the server's buffer.
	if n := rw.Reader.Buffered(); n > 0 {
		buffered, _ := rw.Peek(n)
		if _, err := upstream.Write(buffered); err != nil {
			e.Reason = "upstream write failed"
			return
		}
	}
	e.Reason = splice(client, upstream, p.Policy.idleTimeout, end)
}

// splice copies both ways and closes both connections when either direction
// ends. It returns why the tunnel ended when that was the proxy's limit.
func splice(a, b net.Conn, idle time.Duration, end time.Time) string {
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	var reason atomic.Value
	reason.Store("")
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = a.Close(); _ = b.Close() }) }
	var wg sync.WaitGroup
	pump := func(dst, src net.Conn) {
		defer wg.Done()
		defer closeBoth()
		buf := make([]byte, 32*1024)
		for {
			deadline := time.Now().Add(idle)
			if deadline.After(end) {
				deadline = end
			}
			_ = src.SetReadDeadline(deadline)
			n, err := src.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				_ = dst.SetWriteDeadline(deadline)
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err == nil {
				continue
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				now := time.Now()
				if !now.Before(end) {
					reason.Store("maximum connection time reached")
					return
				}
				// The other direction may be busy: idle means neither moved.
				if now.Sub(time.Unix(0, last.Load())) < idle {
					continue
				}
				reason.Store("idle timeout")
			}
			return
		}
	}
	wg.Add(2)
	go pump(b, a)
	go pump(a, b)
	wg.Wait()
	s, _ := reason.Load().(string)
	return s
}

// hopByHop are the headers that describe one connection and are not
// forwarded (RFC 9110, section 7.6.1), besides the proxy's own credential.
var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func stripHopByHop(h http.Header) {
	for _, f := range h.Values("Connection") {
		for name := range strings.SplitSeq(f, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopByHop {
		h.Del(name)
	}
}

// forward sends a plain http request over the checked connection and copies
// the response back. It never follows a redirect: the client does, through
// the proxy again, so every hop is checked.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, upstream net.Conn, end time.Time, e *Entry) {
	ctx, cancel := context.WithDeadline(r.Context(), end)
	defer cancel()
	_ = upstream.SetDeadline(end)
	var used atomic.Bool
	tr := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			if used.Swap(true) {
				return nil, errors.New("the checked connection is already used")
			}
			return upstream, nil
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: p.Policy.idleTimeout,
	}
	defer tr.CloseIdleConnections()
	out := r.Clone(ctx)
	out.RequestURI = ""
	stripHopByHop(out.Header)
	resp, err := tr.RoundTrip(out)
	if err != nil {
		_ = upstream.Close()
		e.Decision, e.Reason = DecisionDenied, "upstream request failed"
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	stripHopByHop(resp.Header)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// countingConn counts the bytes sent to and received from the destination.
type countingConn struct {
	net.Conn
	up, down atomic.Int64
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.down.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.up.Add(int64(n))
	return n, err
}

// HealthHandler answers 200 once ready reports true, and 503 before.
func HealthHandler(ready func() bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready() {
			http.Error(w, "informers not synced", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})
}
