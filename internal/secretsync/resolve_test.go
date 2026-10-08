package secretsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/crewlet/crewlet-operator/internal/secretref"
)

func secret(namespace, name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{Data: map[string][]byte{}}
	s.Namespace, s.Name = namespace, name
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

// countingReader is a fake client that counts the Secrets read through it.
func countingReader(objs ...client.Object) (client.Reader, map[client.ObjectKey]int) {
	gets := map[client.ObjectKey]int{}
	c := fake.NewClientBuilder().WithObjects(objs...).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			gets[key]++
			return c.Get(ctx, key, obj, opts...)
		},
	}).Build()
	return c, gets
}

// TestResolveReadsEachSecretOnce: Secrets are read uncached, one API call
// each, so a Secret referenced by many keys must cost one read, and a key
// referenced twice must be one entry.
func TestResolveReadsEachSecretOnce(t *testing.T) {
	r, gets := countingReader(
		secret(nimbusNS, slackSecret, map[string]string{botTokenKey: "xoxb-1", signingSecretKey: "sign-1"}),
		secret("platform", "anthropic", map[string]string{apiKeyKey: "sk-ant-1"}),
	)
	keys := []secretref.Key{
		{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey},
		{Namespace: nimbusNS, Name: slackSecret, Key: signingSecretKey},
		{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey},
		{Namespace: "platform", Name: "anthropic", Key: apiKeyKey},
	}
	desired, err := Resolve(context.Background(), r, keys)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := map[string]string{
		StoreName(keys[0]): "xoxb-1",
		StoreName(keys[1]): "sign-1",
		StoreName(keys[3]): "sk-ant-1",
	}
	if len(desired.values) != len(want) {
		t.Fatalf("Resolve returned %d entries, want %d: %v", len(desired.values), len(want), desired)
	}
	for name, value := range want {
		if got, ok := desired.values[name]; !ok || string(got) != value {
			t.Errorf("entry %s is wrong or missing", name)
		}
	}
	if !slices.IsSorted(desired.Names()) {
		t.Errorf("Names() = %v, not sorted", desired.Names())
	}
	for key, n := range gets {
		if n != 1 {
			t.Errorf("Secret %s was read %d times, want once", key, n)
		}
	}
	if len(gets) != 2 {
		t.Errorf("read %d Secrets, want 2", len(gets))
	}
}

// TestResolveReportsEveryMissingReference: every missing Secret and key is
// reported once, naming its reference, so the controller can report each on
// the object that made it; and nothing is resolved, because a document is
// applied whole.
func TestResolveReportsEveryMissingReference(t *testing.T) {
	r, _ := countingReader(secret(nimbusNS, slackSecret, map[string]string{botTokenKey: "xoxb-2"}))
	noSecret1 := secretref.Key{Namespace: nimbusNS, Name: "gone", Key: "a"}
	noSecret2 := secretref.Key{Namespace: nimbusNS, Name: "gone", Key: "b"}
	noKey := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: signingSecretKey}
	present := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey}

	desired, err := Resolve(context.Background(), r, []secretref.Key{present, noSecret2, noKey, noSecret1, noKey})
	if err == nil {
		t.Fatal("Resolve succeeded with missing references")
	}
	if len(desired.values) != 0 {
		t.Errorf("Resolve returned entries alongside its error: %v", desired)
	}

	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatalf("Resolve's error %T does not carry one error per reference", err)
	}
	want := []MissingError{
		{Key: noKey, Reason: KeyNotFound},
		{Key: noSecret1, Reason: SecretNotFound},
		{Key: noSecret2, Reason: SecretNotFound},
	}
	errs := joined.Unwrap()
	if len(errs) != len(want) {
		t.Fatalf("Resolve reported %d problems, want %d: %v", len(errs), len(want), err)
	}
	for i, e := range errs {
		var missing *MissingError
		if !errors.As(e, &missing) || *missing != want[i] {
			t.Errorf("problem %d = %v, want %+v", i, e, want[i])
		}
	}
	if msg := err.Error(); !strings.Contains(msg, `Secret nimbus/ana-slack has no key "signingSecret"`) ||
		!strings.Contains(msg, "Secret nimbus/gone does not exist") {
		t.Errorf("the error does not say which reference is missing: %q", msg)
	}
}

// TestResolveRefusesAValueTheStoreWouldRefuse: the engine refuses a value
// over 64 KiB; Resolve must refuse it first, naming the reference, and
// accept one of exactly 64 KiB.
func TestResolveRefusesAValueTheStoreWouldRefuse(t *testing.T) {
	atLimit := secretref.Key{Namespace: "ns", Name: "files", Key: "atLimit"}
	over := secretref.Key{Namespace: "ns", Name: "files", Key: "over"}
	r, _ := countingReader(secret("ns", "files", map[string]string{
		"atLimit": strings.Repeat("a", MaxValueBytes),
		"over":    strings.Repeat("b", MaxValueBytes+1),
	}))

	if _, err := Resolve(context.Background(), r, []secretref.Key{atLimit}); err != nil {
		t.Fatalf("a value of exactly MaxValueBytes was refused: %v", err)
	}
	_, err := Resolve(context.Background(), r, []secretref.Key{atLimit, over})
	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Key != over || tooLarge.Size != MaxValueBytes+1 {
		t.Fatalf("Resolve() error = %v, want a TooLargeError for %v", err, over)
	}
	if strings.Contains(err.Error(), "bbbb") {
		t.Fatal("the error carries the value")
	}
}

// TestResolveReturnsAPIErrors: an error that is not "not found" is the API
// server's, not the reference's, and must not be reported as a missing
// Secret (which would blame the object that made the reference).
func TestResolveReturnsAPIErrors(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, slackSecret, errors.New("no"))
	r := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return forbidden
		},
	}).Build()
	_, err := Resolve(context.Background(), r, []secretref.Key{{Namespace: "ns", Name: slackSecret, Key: botTokenKey}})
	var missing *MissingError
	if !apierrors.IsForbidden(err) || errors.As(err, &missing) {
		t.Fatalf("Resolve() error = %v, want the API server's Forbidden", err)
	}
}

// TestDesiredNeverPrintsAValue: a Desired that reaches a log line or an
// error by mistake must show names only, under every verb.
func TestDesiredNeverPrintsAValue(t *testing.T) {
	const value = "xoxb-very-secret"
	k := secretref.Key{Namespace: "ns", Name: slackName, Key: botTokenKey}
	r, _ := countingReader(secret("ns", slackName, map[string]string{botTokenKey: value}))
	desired, err := Resolve(context.Background(), r, []secretref.Key{k})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Exactly the names, under every verb: then no encoding of the value
	// (plain, hex, a byte list) can be in it.
	want := "[" + StoreName(k) + "]"
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if got := fmt.Sprintf(verb, desired); got != want {
			t.Errorf("Sprintf(%q, desired) = %q, want %q", verb, got, want)
		}
	}
	if got := fmt.Sprint(desired.MarshalLog()); strings.Contains(got, value) {
		t.Errorf("MarshalLog() = %q carries the value", got)
	}
}
