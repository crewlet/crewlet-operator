package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// The keys of the bootstrap Secret beyond configFile that hold the values
// the engine's environment reads from it.
const (
	platformKeyOTLPHeaders = "otlpHeaders"
	// platformKeyEnvPrefix is followed by the name of a class env variable
	// whose value comes from a Secret. Variable names hold only letters,
	// digits and '_', so the key is always a valid Secret key.
	platformKeyEnvPrefix = "env."
)

// held is why the data pods are held back: RuntimeReady's reason and
// message.
type held struct {
	reason, message string
}

// ensureKeyring returns the company's keyring Secret, creating it with one
// fresh key if it does not exist and the company has no data yet.
//
// A keyring is generated once and never rewritten: every value the engine
// has sealed (its secret store, its configuration revisions) opens only
// with the keys it was sealed under, so a regenerated keyring loses them all.
// That is also why a keyring that disappears while the company's data does
// not is never generated again: the pods would roll onto keys that cannot
// open what is on the volumes. The pods are held back instead, until the
// Secret is restored or the data discarded.
func (r *CompanyReconciler) ensureKeyring(ctx context.Context, c *crewletaiv1alpha1.Company) (*corev1.Secret, *held, error) {
	return r.ensureGenerated(ctx, c, keyringSecretName(c), componentKeyring, func() (*corev1.Secret, *held, error) {
		if h, err := r.dataExists(ctx, c); h != nil || err != nil {
			return nil, h, err
		}
		key := make([]byte, keyringKeyBytes)
		if _, err := rand.Read(key); err != nil {
			return nil, nil, fmt.Errorf("generating a keyring key: %w", err)
		}
		return &corev1.Secret{
			Annotations: map[string]string{KeyringActiveKeyAnnotation: keyringKeyID},
			Data:        map[string][]byte{keyringKeyID: []byte(base64.StdEncoding.EncodeToString(key))},
		}, nil, nil
	})
}

// dataExists reports, as the hold it puts on the pods, that the company has
// a data StatefulSet or data volume claims, which a new keyring could not
// open. It reads the API server, not the cache: a cache that has not seen a
// claim yet must not cost the data on it. A claim being deleted does not
// count; its data goes with it.
func (r *CompanyReconciler) dataExists(ctx context.Context, c *crewletaiv1alpha1.Company) (*held, error) {
	var found []string
	sts := &appsv1.StatefulSet{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: dataStatefulSetName(c)}, sts)
	switch {
	case err == nil:
		found = append(found, "StatefulSet "+sts.Name)
	case !apierrors.IsNotFound(err):
		return nil, fmt.Errorf("reading StatefulSet %s/%s: %w", c.Namespace, dataStatefulSetName(c), err)
	}
	claims, err := r.dataClaims(ctx, c)
	if err != nil {
		return nil, err
	}
	for i := range claims {
		if claims[i].DeletionTimestamp.IsZero() {
			found = append(found, "PersistentVolumeClaim "+claims[i].Name)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &held{
		reason: crewletaiv1alpha1.ReasonKeyringMissing,
		message: fmt.Sprintf("The keyring Secret %s/%s does not exist, and the company's data does (%s): a new keyring could not open "+
			"the secrets sealed on it, so none is generated. Restore the Secret from a backup, with its %s annotation. To discard "+
			"the data instead, delete the data volume claims first; the operator then generates a new keyring.",
			c.Namespace, keyringSecretName(c), strings.Join(found, ", "), KeyringActiveKeyAnnotation),
	}, nil
}

// dataClaims lists a Company's data volume claims from the API server.
func (r *CompanyReconciler) dataClaims(ctx context.Context, c *crewletaiv1alpha1.Company) ([]corev1.PersistentVolumeClaim, error) {
	claims := &corev1.PersistentVolumeClaimList{}
	if err := r.APIReader.List(ctx, claims, client.InNamespace(c.Namespace), client.MatchingLabels(selectorFor(c, componentData))); err != nil {
		return nil, fmt.Errorf("listing the data volume claims of Company %s/%s: %w", c.Namespace, c.Name, err)
	}
	return claims.Items, nil
}

// ensureAPITokens returns the company's API tokens Secret, creating it with
// two fresh tokens if it does not exist. It is never rewritten either: the
// consumers of the access token hold a copy of it.
func (r *CompanyReconciler) ensureAPITokens(ctx context.Context, c *crewletaiv1alpha1.Company) (*corev1.Secret, *held, error) {
	return r.ensureGenerated(ctx, c, apiTokensSecretName(c), componentAPI, func() (*corev1.Secret, *held, error) {
		data := map[string][]byte{}
		for _, id := range []string{render.TokenOperator, render.TokenAccess} {
			token := make([]byte, apiTokenBytes)
			if _, err := rand.Read(token); err != nil {
				return nil, nil, fmt.Errorf("generating the %s API token: %w", id, err)
			}
			data[id] = []byte(base64.RawURLEncoding.EncodeToString(token))
		}
		return &corev1.Secret{Data: data}, nil, nil
	})
}

// ensureGenerated returns the Secret of the name given, creating it from
// generate when it does not exist, and never changes the data of a Secret
// that does. generate may refuse, with the hold it puts on the pods.
//
// An existing Secret that no one controls is adopted: it is what a Retain
// deletion of an earlier Company of this name kept, and the company's data
// volumes need it. One that another object controls is used as it is,
// which lets a platform supply the Secret from its own store (an
// ExternalSecret, for example) instead of having the operator generate it.
//
// The exception is a Secret an earlier Company of this name still controls:
// one deleted with deletionPolicy Delete, whose objects the garbage
// collector has not removed yet, as happens when a platform deletes a tenant
// and provisions it again at once. That Secret is about to be deleted, and
// pods started on it would roll onto a regenerated one as soon as it is, so
// the pods wait for it to go; its deletion requeues the Company.
func (r *CompanyReconciler) ensureGenerated(
	ctx context.Context, c *crewletaiv1alpha1.Company, name, component string,
	generate func() (*corev1.Secret, *held, error),
) (*corev1.Secret, *held, error) {
	secret := &corev1.Secret{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: c.Namespace, Name: name}, secret)
	switch {
	case apierrors.IsNotFound(err):
		secret, h, err := generate()
		if h != nil || err != nil {
			return nil, h, err
		}
		secret.Namespace, secret.Name = c.Namespace, name
		secret.Labels = labelsFor(c, component)
		secret.Type = corev1.SecretTypeOpaque
		if err := controllerutil.SetControllerReference(c, secret, r.Scheme()); err != nil {
			return nil, nil, fmt.Errorf("owning Secret %s/%s: %w", c.Namespace, name, err)
		}
		// Create, never apply: a create fails if the Secret appeared in the
		// meantime, where an apply would overwrite it.
		if err := r.Create(ctx, secret, client.FieldOwner(FieldOwner)); err != nil {
			return nil, nil, fmt.Errorf("creating Secret %s/%s: %w", c.Namespace, name, err)
		}
		return secret, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf("reading Secret %s/%s: %w", c.Namespace, name, err)
	}

	if owner := metav1.GetControllerOf(secret); owner != nil {
		if isPreviousCompany(owner, c) {
			return nil, &held{
				reason: crewletaiv1alpha1.ReasonPreviousCompanyObjects,
				message: fmt.Sprintf("Secret %s/%s belongs to an earlier Company %s, which was deleted; the garbage collector "+
					"deletes it, and the pods wait until it has.", c.Namespace, name, c.Name),
			}, nil
		}
		return secret, nil, nil
	}
	base := secret.DeepCopy()
	if err := controllerutil.SetControllerReference(c, secret, r.Scheme()); err != nil {
		return nil, nil, fmt.Errorf("adopting Secret %s/%s: %w", c.Namespace, name, err)
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	maps.Copy(secret.Labels, labelsFor(c, component))
	// The patch carries the metadata only; the optimistic lock makes it fail
	// rather than apply to a Secret someone changed since it was read.
	patch := client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
	if err := r.Patch(ctx, secret, patch, client.FieldOwner(FieldOwner)); err != nil {
		return nil, nil, fmt.Errorf("adopting Secret %s/%s: %w", c.Namespace, name, err)
	}
	return secret, nil, nil
}

// isPreviousCompany reports whether an owner reference names an earlier
// Company of the same name as c: one that was deleted, since names are
// unique among the objects that exist.
func isPreviousCompany(owner *metav1.OwnerReference, c *crewletaiv1alpha1.Company) bool {
	gv, err := schema.ParseGroupVersion(owner.APIVersion)
	return err == nil && gv.Group == crewletaiv1alpha1.GroupVersion.Group && owner.Kind == "Company" &&
		owner.Name == c.Name && owner.UID != c.UID
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
// a keyring the engine could not use, which is reported as KeyringInvalid.
func keyringOf(secret *corev1.Secret) render.Keyring {
	keys := make(map[string]string, len(secret.Data))
	for id, material := range secret.Data {
		keys[id] = string(material)
	}
	return render.Keyring{ActiveKeyID: secret.Annotations[KeyringActiveKeyAnnotation], Keys: keys}
}

// tokensOf reads the API tokens a tokens Secret holds.
func tokensOf(secret *corev1.Secret) render.APITokens {
	return render.APITokens{
		Operator: string(secret.Data[render.TokenOperator]),
		Access:   string(secret.Data[render.TokenAccess]),
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
	return r.apply(ctx, c, secretGVK, apiAccessSecretName(c), secret)
}

// platformValue is one value the engine's pods are given from a Secret the
// EngineClass references in the platform's namespace.
type platformValue struct {
	// key is the key of the bootstrap Secret that holds it.
	key string
	// from is the Secret key it is copied from.
	from crewletaiv1alpha1.NamespacedSecretKeyReference
	// field is the class field that references it, for messages.
	field string
}

// platformValues lists what a class gives each of its companies' engines
// from the platform's Secrets: the telemetry headers and the env values that
// come from Secrets, except a variable the Company's own
// runtime.env replaces. They are copied rather than referenced because a pod
// reads Secrets from its own namespace only. The copy is the tenant's to
// read: whatever reaches an engine pod reaches code the tenant runs there.
func platformValues(c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass) []platformValue {
	var values []platformValue
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

// readPlatformValues reads the values the class gives each of its companies'
// engines from the platform's Secrets, keyed as platformValue.key: the
// bootstrap Secret holds them, so the pods never read the platform's
// namespace. It returns what it could not read; the caller holds the pods
// back then, rather than start them with a value missing.
func (r *CompanyReconciler) readPlatformValues(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (data map[string][]byte, missing []string, err error) {
	data = map[string][]byte{}
	sources := map[types.NamespacedName]*corev1.Secret{}
	for _, v := range platformValues(c, class) {
		at := types.NamespacedName{Namespace: v.from.Namespace, Name: v.from.Name}
		source, read := sources[at]
		if !read {
			source = &corev1.Secret{}
			if err := r.APIReader.Get(ctx, at, source); apierrors.IsNotFound(err) {
				source = nil
			} else if err != nil {
				return nil, nil, fmt.Errorf("reading Secret %s, which EngineClass %s references: %w", at, class.Name, err)
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
	return data, slices.Compact(missing), nil
}

// readObjectCredentials reads the keys the Company's
// spec.objects.credentialsSecretRef names, for its class's S3 bucket: nil
// when the class keeps files elsewhere or the Company names no Secret, which
// leaves the engine to the pod's own identity. It returns what it could not
// read; the caller holds the pods back then.
func (r *CompanyReconciler) readObjectCredentials(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (creds *render.S3Credentials, missing []string, err error) {
	ref := c.Spec.Objects.CredentialsSecretRef
	if class.Spec.Objects.Backend != crewletaiv1alpha1.ObjectBackendS3 || ref.Name == "" {
		return nil, nil, nil
	}
	at := types.NamespacedName{Namespace: c.Namespace, Name: ref.Name}
	secret := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, at, secret); apierrors.IsNotFound(err) {
		return nil, []string{fmt.Sprintf("Secret %s (spec.objects.credentialsSecretRef) does not exist", at)}, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("reading Secret %s, which spec.objects.credentialsSecretRef names: %w", at, err)
	}
	for _, key := range []string{crewletaiv1alpha1.S3AccessKeyIDKey, crewletaiv1alpha1.S3SecretAccessKeyKey} {
		if len(secret.Data[key]) == 0 {
			missing = append(missing, fmt.Sprintf("Secret %s (spec.objects.credentialsSecretRef) has no key %s", at, key))
		}
	}
	if len(missing) > 0 {
		return nil, missing, nil
	}
	return &render.S3Credentials{
		AccessKeyID:     string(secret.Data[crewletaiv1alpha1.S3AccessKeyIDKey]),
		SecretAccessKey: string(secret.Data[crewletaiv1alpha1.S3SecretAccessKeyKey]),
	}, nil, nil
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
