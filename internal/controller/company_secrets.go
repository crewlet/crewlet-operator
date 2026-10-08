package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// The keyring Secret holds one key per keyring key, named by the key's id,
// whose value is the key's material in the form Crewlet's `material` field
// takes: base64 of 32 random bytes, as `crewlet secrets keygen` prints it.
// Which key seals new writes is not secret and is not a key of the Secret,
// so it is an annotation; the data then holds key ids only, which is what
// render.Keyring expects.
const (
	// KeyringActiveKeyAnnotation names the keyring key that seals new
	// writes.
	KeyringActiveKeyAnnotation = "crewlet.ai/active-key-id"
	// keyringKeyID is the id of the key a new keyring is created with:
	// Crewlet's own default (`crewlet secrets keygen`). It is stamped into
	// every envelope the key seals, so it never changes once used.
	keyringKeyID = "key-1"
	// keyringKeyBytes is the size of an AES-256 key.
	keyringKeyBytes = 32
	// apiTokenBytes is the entropy of each API token: as much as an AES-256
	// key, because a token opens the company's configuration and secret
	// store.
	apiTokenBytes = 32
)

// The keys of the Secret <company>-platform beyond the S3 keys (which keep
// the names of the class's Secret, v1alpha1.S3AccessKeyIDKey and
// v1alpha1.S3SecretAccessKeyKey, as render.BootstrapSecrets.Objects expects).
const (
	platformKeyOTLPHeaders = "otlpHeaders"
	// platformKeyEnvPrefix is followed by the name of a class env variable
	// whose value comes from a Secret. Variable names hold only letters,
	// digits and '_', so the key is always a valid Secret key.
	platformKeyEnvPrefix = "env."
)

// ensureKeyring returns the company's keyring Secret, creating it with one
// fresh key if it does not exist.
//
// A keyring is generated once and never rewritten: every value the engine
// has sealed (its secret store, its configuration revisions) opens only
// with the keys it was sealed under, so a regenerated keyring loses them all.
func (r *CompanyReconciler) ensureKeyring(ctx context.Context, c *crewletaiv1alpha1.Company) (*corev1.Secret, error) {
	return r.ensureGenerated(ctx, c, keyringSecretName(c), componentKeyring, func() (*corev1.Secret, error) {
		key := make([]byte, keyringKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generating a keyring key: %w", err)
		}
		return &corev1.Secret{
			Annotations: map[string]string{KeyringActiveKeyAnnotation: keyringKeyID},
			Data:        map[string][]byte{keyringKeyID: []byte(base64.StdEncoding.EncodeToString(key))},
		}, nil
	})
}

// ensureAPITokens returns the company's API tokens Secret, creating it with
// two fresh tokens if it does not exist. It is never rewritten either: the
// consumers of the access token hold a copy of it.
func (r *CompanyReconciler) ensureAPITokens(ctx context.Context, c *crewletaiv1alpha1.Company) (*corev1.Secret, error) {
	return r.ensureGenerated(ctx, c, apiTokensSecretName(c), componentAPI, func() (*corev1.Secret, error) {
		data := map[string][]byte{}
		for _, id := range []string{render.TokenOperator, render.TokenAccess} {
			token := make([]byte, apiTokenBytes)
			if _, err := rand.Read(token); err != nil {
				return nil, fmt.Errorf("generating the %s API token: %w", id, err)
			}
			data[id] = []byte(base64.RawURLEncoding.EncodeToString(token))
		}
		return &corev1.Secret{Data: data}, nil
	})
}

// ensureGenerated returns the Secret of the name given, creating it from
// generate when it does not exist, and never changes the data of a Secret
// that does.
//
// An existing Secret that no one controls is adopted: it is what a Retain
// deletion of an earlier Company of this name kept, and the company's data
// volumes need it. One that another object controls is used as it is,
// which lets a platform supply the Secret from its own store (an
// ExternalSecret, for example) instead of having the operator generate it.
func (r *CompanyReconciler) ensureGenerated(
	ctx context.Context, c *crewletaiv1alpha1.Company, name, component string,
	generate func() (*corev1.Secret, error),
) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, secret)
	switch {
	case apierrors.IsNotFound(err):
		secret, err = generate()
		if err != nil {
			return nil, err
		}
		secret.Namespace, secret.Name = c.Namespace, name
		secret.Labels = labelsFor(c, component)
		secret.Type = corev1.SecretTypeOpaque
		if err := controllerutil.SetControllerReference(c, secret, r.Scheme()); err != nil {
			return nil, fmt.Errorf("owning Secret %s/%s: %w", c.Namespace, name, err)
		}
		// Create, never apply: a create fails if the Secret appeared in the
		// meantime, where an apply would overwrite it.
		if err := r.Create(ctx, secret, client.FieldOwner(FieldOwner)); err != nil {
			return nil, fmt.Errorf("creating Secret %s/%s: %w", c.Namespace, name, err)
		}
		return secret, nil
	case err != nil:
		return nil, fmt.Errorf("reading Secret %s/%s: %w", c.Namespace, name, err)
	}

	if metav1.GetControllerOf(secret) != nil {
		return secret, nil
	}
	base := secret.DeepCopy()
	if err := controllerutil.SetControllerReference(c, secret, r.Scheme()); err != nil {
		return nil, fmt.Errorf("adopting Secret %s/%s: %w", c.Namespace, name, err)
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	maps.Copy(secret.Labels, labelsFor(c, component))
	// The patch carries the metadata only; the optimistic lock makes it fail
	// rather than apply to a Secret someone changed since it was read.
	patch := client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
	if err := r.Patch(ctx, secret, patch, client.FieldOwner(FieldOwner)); err != nil {
		return nil, fmt.Errorf("adopting Secret %s/%s: %w", c.Namespace, name, err)
	}
	return secret, nil
}

// missingTokens says which API tokens a tokens Secret lacks.
func missingTokens(tokens *corev1.Secret) []string {
	var missing []string
	for _, id := range []string{render.TokenOperator, render.TokenAccess} {
		if len(tokens.Data[id]) == 0 {
			missing = append(missing, fmt.Sprintf("Secret %s/%s has no key %s", tokens.Namespace, tokens.Name, id))
		}
	}
	return missing
}

// keyringOf reads the keys a keyring Secret holds. render.Bootstrap refuses
// a keyring it cannot reference, which is reported as KeyringInvalid.
func keyringOf(secret *corev1.Secret) render.Keyring {
	return render.Keyring{
		ActiveKeyID: secret.Annotations[KeyringActiveKeyAnnotation],
		KeyIDs:      slices.Sorted(maps.Keys(secret.Data)),
	}
}

// applyAPIAccess writes the Secret the Company's status.apiAccessSecretRef
// names: the access token and the API's in-cluster URL, for the company's
// consumers. It is a copy rather than a reference to the tokens Secret so
// that a consumer allowed to read it never sees the operator's own token.
func (r *CompanyReconciler) applyAPIAccess(ctx context.Context, c *crewletaiv1alpha1.Company, tokens *corev1.Secret) error {
	secret := corev1ac.Secret(apiAccessSecretName(c), c.Namespace).
		WithLabels(labelsFor(c, componentAPI)).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(map[string][]byte{
			crewletaiv1alpha1.APIAccessTokenKey: tokens.Data[render.TokenAccess],
			crewletaiv1alpha1.APIAccessURLKey:   []byte(apiURL(c)),
		})
	return r.apply(ctx, secret, "Secret", apiAccessSecretName(c), c.Namespace)
}

// platformValue is one value the engine's pods read from the Secret
// <company>-platform, copied from a Secret the EngineClass references in the
// platform's namespace.
type platformValue struct {
	// key is the key of <company>-platform that holds it.
	key string
	// from is the Secret key it is copied from.
	from crewletaiv1alpha1.NamespacedSecretKeyReference
	// field is the class field that references it, for messages.
	field string
}

// platformValues lists what a class copies into each of its companies'
// namespaces: the S3 credentials, the telemetry headers and the env values
// that come from Secrets, except a variable the Company's own runtime.env
// replaces. They are copied rather than referenced because a pod reads Secrets
// from its own namespace only, and the platform's namespace is not readable
// from a tenant's.
func platformValues(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) []platformValue {
	var values []platformValue
	if s3 := class.Spec.Objects.S3; class.Spec.Objects.Backend == crewletaiv1alpha1.ObjectBackendS3 &&
		s3.CredentialsSecretRef.Name != "" {
		ref := s3.CredentialsSecretRef
		for _, key := range []string{crewletaiv1alpha1.S3AccessKeyIDKey, crewletaiv1alpha1.S3SecretAccessKeyKey} {
			values = append(values, platformValue{
				key:   key,
				from:  crewletaiv1alpha1.NamespacedSecretKeyReference{Namespace: ref.Namespace, Name: ref.Name, Key: key},
				field: "spec.objects.s3.credentialsSecretRef",
			})
		}
	}
	if ref := class.Spec.Telemetry.HeadersSecretRef; ref.Name != "" {
		values = append(values, platformValue{key: platformKeyOTLPHeaders, from: ref, field: "spec.telemetry.headersSecretRef"})
	}
	for _, env := range class.Spec.PodTemplate.Spec.Env {
		if env.ValueFrom.SecretKeyRef.Name == "" || overridden(c, env.Name) {
			continue
		}
		values = append(values, platformValue{
			key:   platformKeyEnvPrefix + env.Name,
			from:  env.ValueFrom.SecretKeyRef,
			field: fmt.Sprintf("spec.podTemplate.spec.env[name=%s]", env.Name),
		})
	}
	return values
}

// overridden reports whether the Company's runtime.env replaces the class's
// variable of that name.
func overridden(c *crewletaiv1alpha1.Company, name string) bool {
	return slices.ContainsFunc(c.Spec.Runtime.Env, func(e crewletaiv1alpha1.EnvVar) bool { return e.Name == name })
}

// applyPlatformSecret copies the class's values into the Secret
// <company>-platform, or deletes that Secret when the class has none.
//
// It returns whether the Secret exists afterwards, its resourceVersion (which
// changes exactly when a copied value does, and rolls the pods that read
// them), and what it could not copy. When something is missing it changes
// nothing: the copy the pods run with is kept rather than emptied, and the
// caller holds the pods back until the platform repairs its Secret.
func (r *CompanyReconciler) applyPlatformSecret(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (version string, missing []string, err error) {
	values := platformValues(c, class)
	if len(values) == 0 {
		return "", nil, r.deleteControlled(ctx, c, secretGVK, platformSecretName(c))
	}

	data := map[string][]byte{}
	sources := map[types.NamespacedName]*corev1.Secret{}
	for _, v := range values {
		at := types.NamespacedName{Namespace: v.from.Namespace, Name: v.from.Name}
		source, read := sources[at]
		if !read {
			source = &corev1.Secret{}
			if err := r.APIReader.Get(ctx, at, source); apierrors.IsNotFound(err) {
				source = nil
			} else if err != nil {
				return "", nil, fmt.Errorf("reading Secret %s, which EngineClass %s references: %w", at, class.Name, err)
			}
			sources[at] = source
		}
		if source == nil {
			missing = append(missing, fmt.Sprintf("Secret %s (EngineClass %s, %s) does not exist", at, class.Name, v.field))
			continue
		}
		value, ok := source.Data[v.from.Key]
		if !ok {
			missing = append(missing, fmt.Sprintf("Secret %s (EngineClass %s, %s) has no key %s", at, class.Name, v.field, v.from.Key))
			continue
		}
		data[v.key] = value
	}
	// Two keys of one missing Secret say the same thing twice.
	missing = slices.Compact(missing)
	if len(missing) > 0 {
		return "", missing, nil
	}

	secret := corev1ac.Secret(platformSecretName(c), c.Namespace).
		WithLabels(labelsFor(c, componentPlatform)).
		WithOwnerReferences(ownerReference(c)).
		WithType(corev1.SecretTypeOpaque).
		WithData(data)
	if err := r.apply(ctx, secret, "Secret", platformSecretName(c), c.Namespace); err != nil {
		return "", nil, err
	}
	return *secret.ResourceVersion, nil, nil
}

// orphanKeyring removes the Company from the owners of its keyring Secret, so
// that the Secret outlives the Company with the data volumes a Retain
// deletion keeps. A keyring someone else controls is not the operator's to
// keep or drop.
func (r *CompanyReconciler) orphanKeyring(ctx context.Context, c *crewletaiv1alpha1.Company) error {
	secret := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: keyringSecretName(c)}, secret)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the keyring Secret %s/%s: %w", c.Namespace, keyringSecretName(c), err)
	}
	if !metav1.IsControlledBy(secret, c) {
		return nil
	}
	base := secret.DeepCopy()
	secret.OwnerReferences = slices.DeleteFunc(secret.OwnerReferences, func(o metav1.OwnerReference) bool { return o.UID == c.UID })
	patch := client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
	if err := r.Patch(ctx, secret, patch, client.FieldOwner(FieldOwner)); err != nil {
		return fmt.Errorf("keeping the keyring Secret %s/%s: %w", c.Namespace, secret.Name, err)
	}
	return nil
}

// secretsVersion is the pod template annotation that rolls the data pods when
// a Secret their environment reads changes: Kubernetes reads a secretKeyRef
// once, when the container starts. It names resourceVersions, never values.
func secretsVersion(keyring, tokens *corev1.Secret, platform string) string {
	parts := []string{"keyring=" + keyring.ResourceVersion, "api-tokens=" + tokens.ResourceVersion}
	if platform != "" {
		parts = append(parts, "platform="+platform)
	}
	return strings.Join(parts, ",")
}
