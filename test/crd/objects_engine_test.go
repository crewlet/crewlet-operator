package crd_test

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// s3Shape is an EngineClass's spec.objects.s3 that TestS3SettingsFollowTheEngine
// offers the API server and an engine. In endpoint, {bucket} is replaced by
// recorderHost, which the engine reaches only through the test's own proxy,
// and {loopback} and {port} by the address and the port that proxy listens
// on, on the loopback address, so that a request the engine sends to either
// is seen.
type s3Shape struct {
	region, bucket, endpoint string
	pathStyle                bool
}

// The values most s3Shapes share: a region and a bucket of plain names, the
// endpoint of the test's own server, and a region and a bucket holding a '.'.
const (
	plainRegion  = "us-east-1"
	plainBucket  = "crewlet"
	atRecorder   = "http://{bucket}"
	dottedRegion = "eu.central"
	dottedBucket = "crewlet.tenants"
)

// recorderHost is the host and port of atRecorder: an address of TEST-NET-1
// (RFC 5737), which no network routes, so that the engine's request for it
// reaches the test's proxy and nothing else. It is an IP address, as the
// loopback address it stands in for was, so that the S3 client addresses a
// bucket there as it did; and it is not the loopback address, which the API
// refuses (s3RefusesMore) and a request to which skips the proxy.
const recorderHost = "192.0.2.1:9000"

// s3Shapes are the S3 settings TestS3SettingsFollowTheEngine tries: those
// Crewlet's S3 client (the AWS SDK for Go) refuses at start before it sends
// a request, an endpoint past the 2048 characters Crewlet's config takes, and
// their neighbours it sends one for.
var s3Shapes = []s3Shape{
	{region: plainRegion, bucket: plainBucket},
	{region: " eu-central-1 ", bucket: " crewlet "},
	{region: "EU-CENTRAL-1", bucket: plainBucket},
	{region: "auto", bucket: plainBucket, endpoint: atRecorder},
	{region: strings.Repeat("a", 63), bucket: plainBucket},
	{region: "eu central 1", bucket: plainBucket},
	{region: "eu_central_1", bucket: plainBucket},
	{region: "eu-central-1.", bucket: plainBucket},
	{region: "eu..central", bucket: plainBucket},
	{region: "-eu", bucket: plainBucket},
	{region: "eu-", bucket: plainBucket},
	{region: strings.Repeat("a", 64), bucket: plainBucket},
	{region: "eü-central-1", bucket: plainBucket},
	{region: "eu-central-1/x", bucket: plainBucket},
	{region: dottedRegion, bucket: plainBucket},
	{region: dottedRegion, bucket: plainBucket, pathStyle: true},
	{region: dottedRegion, bucket: dottedBucket},
	{region: dottedRegion, bucket: plainBucket, endpoint: atRecorder},
	{region: dottedRegion, bucket: plainBucket, endpoint: atRecorder, pathStyle: true},
	{region: "fips-us-east-1", bucket: plainBucket},
	{region: "us-east-1-fips", bucket: plainBucket},
	{region: "fips-us-east-1", bucket: plainBucket, endpoint: atRecorder},
	{region: "us-fipsy-1", bucket: plainBucket, endpoint: atRecorder},
	{region: "fips-us-gov-west-1", bucket: plainBucket},
	{region: "fips-cn-north-1", bucket: plainBucket},
	{region: "cn-north-fips-1", bucket: plainBucket},
	{region: "cn-north-1", bucket: plainBucket},
	{region: "fipsfips-cn-north-1", bucket: plainBucket},
	{region: "fips-cn-a-b-1", bucket: plainBucket},
	{region: plainRegion, bucket: "Crewlet_Tenants"},
	{region: plainRegion, bucket: dottedBucket},
	{region: plainRegion, bucket: "arn:aws:s3:::crewlet"},
	{region: plainRegion, bucket: "arn:aws:s3:us-east-1:123456789012:accesspoint/crewlet"},
	{region: plainRegion, bucket: "crewlet--x-s3"},
	{region: plainRegion, bucket: "crewlet--use1-az4--x-s3", endpoint: atRecorder},
	{region: plainRegion, bucket: "crewlet--use1-az4--xa-s3"},
	{region: plainRegion, bucket: "crewlet--op-s3"},
	{region: plainRegion, bucket: "ARN:crewlet"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{bucket}/?tenant=nimbus"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{bucket}?"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{bucket}/base"},
	{region: plainRegion, bucket: plainBucket, endpoint: endpointOf(2048)},
	{region: plainRegion, bucket: plainBucket, endpoint: endpointOf(2049)},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{bucket}/a\u0080b\u0085c"},
	{region: plainRegion, bucket: "crew\u0080let"},
	{region: "us-east-1\u0085", bucket: plainBucket},
	{region: plainRegion, bucket: plainBucket, endpoint: "HTTP://{bucket}"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://127.0.0.1:0"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://127.0.0.1:65536"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://127.0.0.1:99999"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{bucket}:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://minio.storage.example"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://bücher.example"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://[fe80::1%25eth0]:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://[fe80::1%25e%20th0]:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://[fe80::1%25eth0]]:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://[fe80::1%25eth0]]/"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://minio.storage.example:9000:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://minio.storage.example::9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://::1:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://x[::1]:9000"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://"},
	{region: plainRegion, bucket: plainBucket, endpoint: "https://"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{loopback}"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://localhost:{port}"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://LOCALHOST:{port}"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://0.0.0.0:{port}"},
	{region: plainRegion, bucket: plainBucket, endpoint: "http://:{port}"},
}

// endpointOf is an endpoint at recorderHost that is n characters long once
// {bucket} is replaced: a base path of a's fills it out.
func endpointOf(n int) string {
	at := "http://" + recorderHost + "/"
	return "http://{bucket}/" + strings.Repeat("a", n-len(at))
}

// podsOwnAddress is why the API refuses an endpoint on the pod's own address,
// which Crewlet takes and sends its request to.
const podsOwnAddress = "the pod's own address: no pod the operator runs carries a second container, so nothing " +
	"listens on its loopback, and the engine would wait out its probe of the bucket there before it refused to start"

// s3RefusesMore are the shapes the API refuses although Crewlet's S3 client
// would send their request, and why it does on purpose.
var s3RefusesMore = map[s3Shape]string{
	{region: dottedRegion, bucket: dottedBucket}: "a region holding a '.' without path style: the client refuses " +
		"it only for a bucket it addresses by host name, which is no rule a person can be told",
	{region: plainRegion, bucket: "arn:aws:s3:us-east-1:123456789012:accesspoint/crewlet"}: "an access point's ARN: " +
		"the client resolves only some, and the API asks for the bucket itself",
	{region: plainRegion, bucket: "crewlet--use1-az4--x-s3", endpoint: atRecorder}: "an S3 Express directory " +
		"bucket: the client resolves only some, and the API asks for a general purpose bucket",
	{region: plainRegion, bucket: "crewlet--use1-az4--xa-s3"}: "an S3 Express directory bucket in a Local Zone: the " +
		"client resolves only some, and the API asks for a general purpose bucket",
	{region: plainRegion, bucket: "crewlet--op-s3"}: "an Outposts access point alias: the client resolves only some, " +
		"and the API asks for a general purpose bucket",
	{region: plainRegion, bucket: plainBucket, endpoint: "http://{loopback}"}:       podsOwnAddress,
	{region: plainRegion, bucket: plainBucket, endpoint: "http://localhost:{port}"}: podsOwnAddress,
	{region: plainRegion, bucket: plainBucket, endpoint: "http://LOCALHOST:{port}"}: podsOwnAddress,
	{region: plainRegion, bucket: plainBucket, endpoint: "http://0.0.0.0:{port}"}:   podsOwnAddress,
	{region: plainRegion, bucket: plainBucket, endpoint: "http://:{port}"}:          podsOwnAddress,
}

// TestS3SettingsFollowTheEngine holds the API's verdict on an EngineClass's
// spec.objects.s3 to what an engine does with it at start, both ways where
// that is honest: every shape the API admits renders to a crewlet.yaml on
// which the engine sends its first request to the bucket, so no Company of
// the class crash-loops its pods on it; and every shape it refuses is one on
// which the engine refuses to start before it sends any, but for
// s3RefusesMore. Each shape is offered to both API servers, as a Company's
// addresses are (TestAddressShapesFollowTheEngine).
//
// `crewlet validate` cannot say so: it checks only that a region and a bucket
// are given. The engine reaches the bucket (HeadBucket) as it starts, and
// the client it is built with refuses some settings before it sends the
// request, however the bucket is set up. So the engine is run: on a data
// pod's crewlet.yaml for the shape, with every request it sends going
// through a proxy of the test's own, recorderHost's among them, or straight
// to it for an endpoint on the loopback address, which the API refuses. A
// request arriving is the shape the engine sends for; the engine exiting
// first, on its object store, is one it refuses.
func TestS3SettingsFollowTheEngine(t *testing.T) {
	prefix := strings.ToLower(namespacePrefix(newNamespace(t)))
	for i, shape := range s3Shapes {
		t.Run(fmt.Sprintf("%q/%q/%q/%v", shape.region, shape.bucket, shape.endpoint, shape.pathStyle), func(t *testing.T) {
			t.Parallel()
			bucket := newBucketServer(t)
			_, port, err := net.SplitHostPort(bucket.addr())
			if err != nil {
				t.Fatal(err)
			}
			endpoint := strings.NewReplacer("{bucket}", recorderHost, "{loopback}", bucket.addr(), "{port}", port)
			settings := v1.S3ObjectSettings{
				Region: shape.region, Bucket: shape.bucket, Endpoint: endpoint.Replace(shape.endpoint),
			}
			if shape.pathStyle {
				settings.PathStyle = new(true)
			}
			class := engineClass("", fmt.Sprintf("%s-s3-%02d", prefix, i))
			class.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendS3, S3: settings}
			admits := admissions(t, class)
			sent := engineSendsToBucket(t, class, bucket)
			why, more := s3RefusesMore[shape]
			if more && !sent {
				t.Errorf("s3RefusesMore lists %+v (%s), and the engine refuses to start on it", settings, why)
			}
			for _, a := range admits {
				switch {
				case a.admitted && !sent:
					t.Errorf("%s admits %+v, and the engine refuses to start on it", a.server, settings)
				case !a.admitted && sent && !more:
					t.Errorf("%s refuses %+v, and the engine sends its request on it", a.server, settings)
				case more && a.admitted:
					t.Errorf("s3RefusesMore lists %+v (%s), and %s admits it", settings, why, a.server)
				}
			}
		})
	}
}

// bucketServer is a TCP server that records whether anything sent it a
// request, as the bucket's endpoint or as the engine's proxy. It reads the
// first bytes and closes the connection: what the request said, and whether
// an HTTP server would have taken it (one does not take a CONNECT to an IPv6
// address with a zone, which the engine sends), is not the question.
type bucketServer struct {
	listener net.Listener
	once     sync.Once
	got      chan struct{}
}

func newBucketServer(t *testing.T) *bucketServer {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &bucketServer{listener: l, got: make(chan struct{})}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				if n, _ := conn.Read(make([]byte, 1)); n > 0 {
					b.once.Do(func() { close(b.got) })
				}
			}()
		}
	}()
	return b
}

// addr is the server's address, host:port.
func (b *bucketServer) addr() string { return b.listener.Addr().String() }

// objectStoreWait is how long engineSendsToBucket waits for an engine to send
// a request toward its bucket or to exit: the steps a fresh node's boot takes
// before its probe of the bucket sends its first request, on a broker of its
// own, which take seconds. Every shape of s3Shapes is settled by then: an
// engine sends its first request for each one it takes, and refuses each
// other before it sends one, its bootstrap config's check refusing every
// endpoint no dial takes or no request reaches (crewlet/crewlet#298) and its
// S3 client the rest as it is built. Measured on two CPUs, two engines at a
// time, the slowest shape is settled 2.7 s after its engine starts, and the
// whole test takes 22 s: a minute is twenty times the slowest, for a runner
// under load.
//
// NOT THE PROBE'S BOUND. A shape the engine refused only once a request
// failed would need it: Crewlet's S3 client asks a request the bucket keeps
// failing again for up to a minute before it gives it up (s3obj's
// judge.giveUpAfter), and the probe's delete runs after it under its own
// 30 s (callBudget), so such a shape is refused 80 to 90 s after the engine
// starts, and the wait would be the probe's whole bound as the data pod's
// startup window counts it (the controller's s3ProbeSeconds, 240 s) and this
// minute. None of s3Shapes is one now; a shape added that is fails here at a
// minute, saying so, rather than passing slowly.
const objectStoreWait = time.Minute

// engineSendsToBucket is whether an engine started on a data pod's
// crewlet.yaml for class sends a request toward its bucket, through bucket
// as its proxy or to bucket as the endpoint, before it exits; and fails the
// test when it exits for anything but its object store, or does neither
// within objectStoreWait.
func engineSendsToBucket(t *testing.T, class *v1.EngineClass, bucket *bucketServer) bool {
	t.Helper()
	in := bootstrapInput()
	in.Class = class
	in.Objects = &render.S3Credentials{AccessKeyID: "test-access-key-id", SecretAccessKey: "test-secret-access-key"}
	doc, err := render.Bootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var tierA map[string]any
	if err := yaml.Unmarshal(doc.YAML, &tierA); err != nil {
		t.Fatal(err)
	}
	api := tierA["api"].(map[string]any)
	api["host"], api["port"] = "127.0.0.1", freePort(t)
	// The public listener's port is fixed, which another engine may hold.
	delete(api, "public")
	tierA["store"].(map[string]any)["path"] = filepath.Join(dir, "store", "crewlet.db")
	tierA["stream"].(map[string]any)["store_dir"] = filepath.Join(dir, "stream")
	file := filepath.Join(dir, "crewlet.yaml")
	if err := os.WriteFile(file, []byte(yamlFlow(tierA)), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), objectStoreWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, enginetest.Binary(t), "run", "-config", file)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, render.EnvNodeID + "=nimbus-data-0",
		"HTTP_PROXY=http://" + bucket.addr(), "HTTPS_PROXY=http://" + bucket.addr(), "AWS_EC2_METADATA_DISABLED=true",
	}
	var log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &log, &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case <-bucket.got:
		_ = cmd.Process.Kill()
		<-exited
		return true
	case err := <-exited:
		if !refusedOnObjectStore(log.String()) {
			t.Fatalf("the engine exited (%v) for another reason than its object store:\n%s", err, tail(log.String()))
		}
		return false
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-exited
		t.Fatalf("the engine neither sent a request toward its bucket nor exited within %s:\n%s", objectStoreWait,
			tail(log.String()))
		return false
	}
}

// refusedOnObjectStore is whether an engine's log says it refused to start on
// its object store: at the boot's probe of the bucket, or before the boot,
// where the bootstrap config's check refuses a field of store.objects.s3
// outright, as it does an endpoint no request can reach (an endpoint with no
// host, a query or a port no dial takes, or a zone no dial can use) and one
// past its length.
func refusedOnObjectStore(log string) bool {
	return strings.Contains(log, "the object store (store.objects.s3)") ||
		strings.Contains(log, ": store.objects.s3.")
}

// yamlFlow is v, a document decoded from YAML, written as YAML in flow
// style: every string double-quoted in ASCII with Go's escapes, each of which
// YAML 1.2 reads as Go wrote it, so that a string reaches the engine exactly
// as the test gives it.
func yamlFlow(v any) string {
	switch v := v.(type) {
	case map[string]any:
		parts := make([]string, 0, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			parts = append(parts, strconv.QuoteToASCII(k)+": "+yamlFlow(v[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = yamlFlow(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = strconv.QuoteToASCII(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case string:
		return strconv.QuoteToASCII(v)
	case nil:
		return "null"
	default:
		return fmt.Sprint(v)
	}
}

// freePort is a TCP port on the loopback address that nothing listened on a
// moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// tail is the last lines of an engine's log.
func tail(log string) string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	return strings.Join(lines[max(0, len(lines)-30):], "\n")
}

// widestCompany is a Company whose namespace and name are the longest the API
// admits, 63 and 47 characters, with a UID: what each variable of an S3
// prefix stands for at its longest.
func widestCompany() *v1.Company {
	c := company(strings.Repeat("n", 63), "w"+strings.Repeat("a", 46))
	c.UID = types.UID("6f1d4c2e-0b7a-4d6e-9c3f-2a5b8e7d1c04")
	return c
}

// The heads of the prefixes TestS3PrefixBoundFollowsTheEngine and
// TestEngineClassObjects try, each naming {{uid}} so that the prefix is a
// company's own, and each made mostly of one variable, so that the bound
// holds that variable at the most it stands for.
var (
	namesHead      = strings.Repeat("{{name}}", 20) + "{{uid}}"
	namespacesHead = strings.Repeat("{{namespace}}", 15) + "{{uid}}"
	uidsHead       = strings.Repeat("{{uid}}", 28)
)

// prefixComingTo is an S3 prefix template that comes to n characters once
// expanded for widestCompany: head, then filler repeated, then the '/' every
// prefix ends with.
func prefixComingTo(head, filler string, n int) string {
	expanded := utf8.RuneCountInString(render.ExpandTemplate(head, widestCompany()))
	return head + strings.Repeat(filler, n-expanded-1) + "/"
}

// pathReferences is a prefix of a Company's own that Crewlet expands, from
// the pod's environment, past its bound: thirty-four ${PATH} references,
// each standing for the PATH the engine runs with.
var pathReferences = strings.Repeat("${PATH}", 34) + "{{uid}}/"

// TestS3PrefixBoundFollowsTheEngine holds the API's verdict on an
// EngineClass's spec.objects.s3.prefix, by how long it comes to once
// expanded, to `crewlet validate`'s on the crewlet.yaml the render writes for
// the widest Company: every prefix the API admits comes to a
// store.objects.s3.prefix the engine takes, and every one it refuses for its
// length, or for a ${NAME} reference the engine expands past it, to one the
// engine refuses, so no Company of the class crash-loops its pods on it, and
// the API refuses no prefix of the variables alone any Company's engine
// would take. Each bound is tried for the variable that fills most of the
// prefix, and in characters past ASCII, which both count as characters, not
// bytes.
func TestS3PrefixBoundFollowsTheEngine(t *testing.T) {
	if n := 34*utf8.RuneCountInString(os.Getenv("PATH")) + 37; n <= 1024 {
		t.Fatalf("pathReferences comes to %d characters on this PATH, within the bound it is meant to pass", n)
	}
	prefix := strings.ToLower(namespacePrefix(newNamespace(t)))
	type shape struct{ name, template string }
	bounds := []struct{ name, head, filler string }{
		{"names", namesHead, "a"},
		{"names past ASCII", namesHead, "ü"},
		{"namespaces", namespacesHead, "a"},
		{"uids", uidsHead, "a"},
	}
	shapes := make([]shape, 0, 1+2*len(bounds))
	shapes = append(shapes, shape{"a reference to PATH", pathReferences})
	for _, s := range bounds {
		for _, n := range []int{1024, 1025} {
			shapes = append(shapes, shape{fmt.Sprintf("%s/%d", s.name, n), prefixComingTo(s.head, s.filler, n)})
		}
	}
	for i, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			class := engineClass("", fmt.Sprintf("%s-s3-prefix-%02d", prefix, i))
			class.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendS3, S3: v1.S3ObjectSettings{
				Region: tenantsRegion, Bucket: tenantsBucket, Prefix: shape.template,
			}}
			in := bootstrapInput()
			in.Company, in.Class = widestCompany(), class
			in.Objects = &render.S3Credentials{AccessKeyID: "test-access-key-id", SecretAccessKey: "test-secret-access-key"}
			doc, err := render.Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			problems, out := validate(t, doc.YAML)
			for _, p := range problems {
				if p != "store.objects.s3.prefix" {
					t.Fatalf("crewlet validate refused the Tier A at %s, not only at store.objects.s3.prefix:\n%s",
						p, out)
				}
			}
			engine := len(problems) == 0
			for _, a := range admissions(t, class) {
				if a.admitted != engine {
					t.Errorf("%s admits prefix %q: %v; crewlet validate takes what it comes to for the "+
						"widest Company: %v\n%s", a.server, shape.template, a.admitted, engine, out)
				}
			}
		})
	}
}
