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

	"go.yaml.in/yaml/v3"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// s3Shape is an EngineClass's spec.objects.s3 that TestS3SettingsFollowTheEngine
// offers the API server and an engine. In endpoint, {bucket} is replaced by
// the address of a server of the test's own, so that a request the engine
// sends there is seen.
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

// s3Shapes are the S3 settings TestS3SettingsFollowTheEngine tries: those
// Crewlet's S3 client (the AWS SDK for Go) refuses at start before it sends
// a request, and their neighbours it sends one for.
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
}

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
}

// TestS3SettingsFollowTheEngine holds the API's verdict on an EngineClass's
// spec.objects.s3 to what an engine does with it at start, both ways where
// that is honest: every shape the API admits renders to a crewlet.yaml on
// which the engine sends its first request to the bucket, so no Company of
// the class crash-loops its pods on it; and every shape it refuses is one on
// which the engine refuses to start before it sends any, but for
// s3RefusesMore.
//
// `crewlet validate` cannot say so: it checks only that a region and a bucket
// are given. The engine reaches the bucket (HeadBucket) as it starts, and
// the client it is built with refuses some settings before it sends the
// request, however the bucket is set up. So the engine is run: on a data
// pod's crewlet.yaml for the shape, with every request it sends going
// through a proxy of the test's own, or straight to it for an endpoint on
// the loopback address. A request arriving is the shape the engine sends
// for; the engine exiting first, on its object store, is one it refuses.
func TestS3SettingsFollowTheEngine(t *testing.T) {
	prefix := strings.ToLower(namespacePrefix(newNamespace(t)))
	for i, shape := range s3Shapes {
		t.Run(fmt.Sprintf("%q/%q/%q/%v", shape.region, shape.bucket, shape.endpoint, shape.pathStyle), func(t *testing.T) {
			t.Parallel()
			bucket := newBucketServer(t)
			settings := v1.S3ObjectSettings{
				Region: shape.region, Bucket: shape.bucket,
				Endpoint: strings.ReplaceAll(shape.endpoint, "{bucket}", bucket.addr()),
			}
			if shape.pathStyle {
				settings.PathStyle = new(true)
			}
			class := engineClass("", fmt.Sprintf("%s-s3-%02d", prefix, i))
			class.Spec.Objects = v1.ObjectSettings{Backend: v1.ObjectBackendS3, S3: settings}
			ok := admitted(t, class.DeepCopy())
			sent := engineSendsToBucket(t, class, bucket)
			why, more := s3RefusesMore[shape]
			switch {
			case ok && !sent:
				t.Errorf("the API admits %+v, and the engine refuses to start on it", settings)
			case !ok && sent && !more:
				t.Errorf("the API refuses %+v, and the engine sends its request on it", settings)
			case more && (ok || !sent):
				t.Errorf("s3RefusesMore lists %+v (%s), and the API admits it: %v, the engine sends its request: %v",
					settings, why, ok, sent)
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

// engineSendsToBucket is whether an engine started on a data pod's
// crewlet.yaml for class sends a request toward its bucket, through bucket
// as its proxy or to bucket as the endpoint, before it exits; and fails the test when it exits for anything but
// its object store, or does neither within a minute.
func engineSendsToBucket(t *testing.T, class *v1.EngineClass, bucket *bucketServer) bool {
	t.Helper()
	in := bootstrapInput(takenExternal, takenPublic, nil)
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

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
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
		if !strings.Contains(log.String(), "the object store (store.objects.s3)") {
			t.Fatalf("the engine exited (%v) for another reason than its object store:\n%s", err, tail(log.String()))
		}
		return false
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-exited
		t.Fatalf("the engine neither sent a request toward its bucket nor exited within a minute:\n%s", tail(log.String()))
		return false
	}
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
