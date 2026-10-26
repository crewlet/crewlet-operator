// Package crd_test installs the crewlet.ai CustomResourceDefinitions into a
// real kube-apiserver (envtest) and checks that the API server enforces what
// the types promise: every CEL rule refuses what it must with its own
// message, valid objects are accepted, defaults are applied, and every sample
// in config/samples is accepted. The tests of an address the engine is
// handed also ask a second API server of the same release, whose URL parser
// is Go's looser one (looseURLParser).
//
// These are tests of the generated CRDs, not of Go code: a marker with a typo,
// a rule controller-gen dropped, or a rule whose cost the API server refuses
// all fail here, and nowhere else before a cluster does.
package crd_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/envtestbin"
)

var (
	ctx       = context.Background()
	k8sClient client.Client
	// installedCRDs is what envtest installed, read back from the API
	// server: a case that has to cover every value of an enum reads the
	// enum here, so a value added later is covered without a second list.
	installedCRDs []*apiextensionsv1.CustomResourceDefinition
	// looseClient talks to a second API server of the same release, run
	// with looseURLParser: the tests of an address the engine is handed
	// (addresses_engine_test.go, objects_engine_test.go) hold its verdict
	// to the engine's as well as k8sClient's, so that what Crewlet refuses
	// in an address's host and port is refused by a rule of the CRD rather
	// than by how the API server's Go net/url was built and is run.
	looseClient client.Client
)

// looseURLParser is the GODEBUG setting under which Go's net/url, and so a
// CEL rule's isURL() and url(), take an authority with a second ':' after
// its host (http://h.example:80:80, http://::1:8080), as it did before Go
// 1.26: Go's own way back to that parser, which the API server's
// environment can carry.
const looseURLParser = "urlstrictcolons=0"

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run starts the API servers, runs the tests and stops them again; it is a
// function of its own so that the deferred Stops run before os.Exit.
func run(m *testing.M) int {
	strict, loose := newEnvironment(), newEnvironment()
	// Outside make (an IDE, a bare go test) KUBEBUILDER_ASSETS is unset;
	// fall back to the binaries `make setup-envtest` put into bin/k8s.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		dir, err := envtestbin.Dir(filepath.Join("..", "..", "bin", "k8s"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		strict.BinaryAssetsDirectory, loose.BinaryAssetsDirectory = dir, dir
	}

	// Stop is deferred before Start is checked: a Start that fails after
	// launching etcd or the API server leaves them running otherwise.
	defer stop(strict)
	defer stop(loose)
	cfg, err := strict.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting envtest:", err)
		return 1
	}
	looseCfg, err := startWithGODEBUG(loose, looseURLParser)
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting envtest with GODEBUG=%s: %v\n", looseURLParser, err)
		return 1
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := crewletv1alpha1.AddToScheme(scheme); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// For the one test that installs the EngineClass CRD with other rules
	// for a while (storeUnderLowerFloors).
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if k8sClient, err = client.New(cfg, client.Options{Scheme: scheme}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if looseClient, err = client.New(looseCfg, client.Options{Scheme: scheme}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	installedCRDs = strict.CRDs
	return m.Run()
}

// stop stops env, saying so when it cannot.
func stop(env *envtest.Environment) {
	if err := env.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "stopping envtest:", err)
	}
}

// newEnvironment is an envtest environment with the generated CRDs.
func newEnvironment() *envtest.Environment {
	return &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
}

// startWithGODEBUG starts env with setting added to GODEBUG in the
// environment of its processes. envtest starts etcd and the API server with
// this process's own environment and offers no other, so the setting is
// put there for the Start alone and taken out again. It reaches this
// process's own GODEBUG settings meanwhile, which govern nothing then but
// how envtest reads its loopback addresses, and never `crewlet validate`'s
// or an engine's: those are run with an environment of their own.
func startWithGODEBUG(env *envtest.Environment, setting string) (cfg *rest.Config, err error) {
	old, had := os.LookupEnv("GODEBUG")
	value := setting
	if old != "" {
		value = old + "," + setting
	}
	if err := os.Setenv("GODEBUG", value); err != nil {
		return nil, err
	}
	defer func() {
		restore := func() error { return os.Unsetenv("GODEBUG") }
		if had {
			restore = func() error { return os.Setenv("GODEBUG", old) }
		}
		err = errors.Join(err, restore())
	}()
	return env.Start()
}

// newNamespace creates a namespace for one test, so that the names a test
// chooses never collide with another test's.
func newNamespace(t *testing.T) string {
	t.Helper()
	ns := &corev1.Namespace{GenerateName: "crd-"}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("creating a namespace: %v", err)
	}
	return ns.Name
}

// enumOf is the enum the installed CRD named crd declares at path, a field
// path under its first version's schema root (such as "spec", "cli",
// "agent"). It fails the test where the field or its enum is missing, so a
// case built over it never runs over nothing.
func enumOf(t *testing.T, crd string, path ...string) []string {
	t.Helper()
	for _, c := range installedCRDs {
		if c.Name != crd {
			continue
		}
		node := c.Spec.Versions[0].Schema.OpenAPIV3Schema
		for _, field := range path {
			child, ok := node.Properties[field]
			if !ok {
				t.Fatalf("%s has no field %s", crd, strings.Join(path, "."))
			}
			node = &child
		}
		var values []string
		for _, v := range node.Enum {
			var s string
			if err := json.Unmarshal(v.Raw, &s); err != nil {
				t.Fatalf("%s %s: enum value %s: %v", crd, strings.Join(path, "."), v.Raw, err)
			}
			values = append(values, s)
		}
		if len(values) == 0 {
			t.Fatalf("%s %s declares no enum", crd, strings.Join(path, "."))
		}
		return values
	}
	t.Fatalf("no CRD %s was installed", crd)
	return nil
}

// expect checks the API server's answer: want "" means the object must be
// accepted; otherwise it must be refused as invalid with a message that
// contains want.
func expect(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Fatalf("refused, want accepted: %v", err)
	case want == "":
	case err == nil:
		t.Fatalf("accepted, want refused with %q", want)
	case !apierrors.IsInvalid(err):
		t.Fatalf("refused for another reason than validation, want %q: %v", want, err)
	case !strings.Contains(err.Error(), want):
		t.Fatalf("refused with the wrong message:\n got: %v\nwant: %q", err, want)
	}
}

// createCase is one object built from a valid base and refused (or accepted)
// on create.
type createCase[T client.Object] struct {
	name   string
	mutate func(T)
	want   string
}

// runCreateCases creates base, changed by each case's mutate, and checks the
// API server's answer. A case with no mutate checks the base itself is
// accepted, which every table starts with: a rule that refuses everything
// would otherwise pass every refusal case.
func runCreateCases[T client.Object](t *testing.T, base func(namespace, name string) T, cases []createCase[T]) {
	t.Helper()
	namespace := newNamespace(t)
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := base(namespace, fmt.Sprintf("%s-c%02d", strings.ToLower(namespacePrefix(namespace)), i))
			if c.mutate != nil {
				c.mutate(obj)
			}
			expect(t, k8sClient.Create(ctx, obj), c.want)
		})
	}
}

// updateCase is a valid object, created, then changed by mutate and updated.
type updateCase[T client.Object] struct {
	name   string
	before func(T)
	mutate func(T)
	want   string
}

// runUpdateCases creates base (changed by before) and updates it with mutate,
// checking the update's answer; it exercises transition rules, which only
// run on an update.
func runUpdateCases[T client.Object](t *testing.T, base func(namespace, name string) T, cases []updateCase[T]) {
	t.Helper()
	namespace := newNamespace(t)
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := base(namespace, fmt.Sprintf("%s-u%02d", strings.ToLower(namespacePrefix(namespace)), i))
			if c.before != nil {
				c.before(obj)
			}
			if err := k8sClient.Create(ctx, obj); err != nil {
				t.Fatalf("creating the object to update: %v", err)
			}
			c.mutate(obj)
			expect(t, k8sClient.Update(ctx, obj), c.want)
		})
	}
}

// namespacePrefix gives cluster-scoped objects names unique to the test that
// creates them: the generated part of the test's namespace.
func namespacePrefix(namespace string) string {
	return "t" + strings.TrimPrefix(namespace, "crd-")
}

func equal(got, want any) bool { return reflect.DeepEqual(got, want) }
