// Package crd_test installs the crewlet.ai CustomResourceDefinitions into a
// real kube-apiserver (envtest) and checks that the API server enforces what
// the types promise: every CEL rule refuses what it must with its own
// message, valid objects are accepted, defaults are applied, and every sample
// in config/samples is accepted.
//
// These are tests of the generated CRDs, not of Go code: a marker with a typo,
// a rule controller-gen dropped, or a rule whose cost the API server refuses
// all fail here, and nowhere else before a cluster does.
package crd_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	crewletv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/test/envtestbin"
)

var (
	ctx       = context.Background()
	k8sClient client.Client
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run starts the API server, runs the tests and stops it again; it is a
// function of its own so that the deferred Stop runs before os.Exit.
func run(m *testing.M) int {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	// Outside make (an IDE, a bare go test) KUBEBUILDER_ASSETS is unset;
	// fall back to the binaries `make setup-envtest` put into bin/k8s.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		dir, err := envtestbin.Dir(filepath.Join("..", "..", "bin", "k8s"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		testEnv.BinaryAssetsDirectory = dir
	}

	// Stop is deferred before Start is checked: a Start that fails after
	// launching etcd or the API server leaves them running otherwise.
	defer func() {
		if err := testEnv.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, "stopping envtest:", err)
		}
	}()
	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting envtest:", err)
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
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
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
