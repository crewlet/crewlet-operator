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
	desired, err := Resolve(context.Background(), r, keys, nil)
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

	desired, err := Resolve(context.Background(), r, []secretref.Key{present, noSecret2, noKey, noSecret1, noKey}, nil)
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

	if _, err := Resolve(context.Background(), r, []secretref.Key{atLimit}, nil); err != nil {
		t.Fatalf("a value of exactly MaxValueBytes was refused: %v", err)
	}
	_, err := Resolve(context.Background(), r, []secretref.Key{atLimit, over}, nil)
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
	_, err := Resolve(context.Background(), r, []secretref.Key{{Namespace: "ns", Name: slackSecret, Key: botTokenKey}}, nil)
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
	desired, err := Resolve(context.Background(), r, []secretref.Key{k}, nil)
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

// TestResolveTakesAMissingOptionalReferenceForNoValue: a Secret or key that
// does not exist under an optional reference is no error but an absent value,
// as a missing optional secretKeyRef is to Kubernetes, reported by Missing and
// pushed by nothing; the same one required is the error it always was. A
// value over the store's limit is refused either way, since it is there, and
// an empty one is pushed as it is and reported by Empty, optional or not.
func TestResolveTakesAMissingOptionalReferenceForNoValue(t *testing.T) {
	r, _ := countingReader(secret(nimbusNS, slackSecret, map[string]string{botTokenKey: "xoxb-3", tokenKey: ""}))
	present := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: botTokenKey}
	empty := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: tokenKey}
	noKey := secretref.Key{Namespace: nimbusNS, Name: slackSecret, Key: signingSecretKey}
	noSecret := secretref.Key{Namespace: nimbusNS, Name: "deleted", Key: "a"}
	keys := []secretref.Key{present, empty, noKey, noSecret}

	desired, err := Resolve(context.Background(), r, keys, []secretref.Key{empty, noKey, noSecret})
	if err != nil {
		t.Fatalf("Resolve refused missing optional references: %v", err)
	}
	if want := []string{StoreName(present), StoreName(empty)}; !slices.Equal(desired.Names(), slices.Sorted(slices.Values(want))) {
		t.Errorf("Resolve pushes %v, want %v", desired.Names(), want)
	}
	want := []*MissingError{{Key: noSecret, Reason: SecretNotFound}, {Key: noKey, Reason: KeyNotFound}}
	slices.SortFunc(want, func(a, b *MissingError) int { return secretref.Compare(a.Key, b.Key) })
	got := desired.Missing()
	if len(got) != len(want) {
		t.Fatalf("Missing() = %v, want %v", got, want)
	}
	for i := range want {
		if *got[i] != *want[i] {
			t.Errorf("Missing()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if !slices.Equal(desired.Empty(), []secretref.Key{empty}) {
		t.Errorf("Empty() = %v, want [%v]", desired.Empty(), empty)
	}

	// Required, the same references are the errors they always were, and
	// the Desired beside them pushes nothing but still says what was read
	// of the others: which optional one is absent, which value is empty.
	partial, err := Resolve(context.Background(), r, keys, []secretref.Key{noKey})
	if err == nil || !strings.Contains(err.Error(), "Secret nimbus/deleted does not exist") || strings.Contains(err.Error(), "signingSecret") {
		t.Errorf("Resolve with only noKey optional answered %v, want the missing Secret alone", err)
	}
	if !partial.partial || len(partial.Names()) != 0 {
		t.Errorf("the Desired beside the error pushes %v, partial %t", partial.Names(), partial.partial)
	}
	if got := partial.Missing(); len(got) != 1 || *got[0] != (MissingError{Key: noKey, Reason: KeyNotFound}) {
		t.Errorf("Missing() beside the error = %v, want %v alone", got, noKey)
	}
	if !slices.Equal(partial.Empty(), []secretref.Key{empty}) {
		t.Errorf("Empty() beside the error = %v, want [%v]", partial.Empty(), empty)
	}
}

// TestAbsentNamesTheKeysWhoseSecretOrKeyIsGone: Absent is what the running
// revision goes without of the keys it lets go: a Secret or a key that does
// not exist, never one that holds a value, an empty or too large one
// included, and an API server's error is returned rather than read as a
// Secret that is gone.
func TestAbsentNamesTheKeysWhoseSecretOrKeyIsGone(t *testing.T) {
	const held, value = "held", "value"
	big := strings.Repeat("x", MaxValueBytes+1)
	r, _ := countingReader(secret("ns", held, map[string]string{value: "v", "empty": "", "big": big}))
	keys := []secretref.Key{
		{Namespace: "ns", Name: held, Key: value},
		{Namespace: "ns", Name: held, Key: "empty"},
		{Namespace: "ns", Name: held, Key: "big"},
		{Namespace: "ns", Name: held, Key: "missing"},
		{Namespace: "ns", Name: "deleted", Key: value},
	}
	absent, err := Absent(context.Background(), r, keys)
	if err != nil {
		t.Fatalf("Absent: %v", err)
	}
	got := make([]string, len(absent))
	for i, m := range absent {
		got[i] = m.Key.String() + " " + string(m.Reason)
	}
	if want := []string{"ns/deleted[value] SecretNotFound", "ns/held[missing] KeyNotFound"}; !slices.Equal(got, want) {
		t.Errorf("Absent = %v, want %v", got, want)
	}
	if absent, err := Absent(context.Background(), r, nil); absent != nil || err != nil {
		t.Errorf("Absent of no key = %v, %v", absent, err)
	}

	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, held, errors.New("no"))
	failing := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return forbidden
		},
	}).Build()
	if _, err := Absent(context.Background(), failing, keys); !apierrors.IsForbidden(err) {
		t.Errorf("Absent() error = %v, want the API server's Forbidden", err)
	}
}
