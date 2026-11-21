package controller

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/egresstoken"
)

// platformKeyProxyURL is the bootstrap Secret's key for the egress proxy's
// address with the namespace's credential in it.
const platformKeyProxyURL = "egress.proxyURL"

// The variables every engine pod is given under an egress proxy. Both
// spellings: curl and many language runtimes read only the lower case, Go
// and others the upper.
var (
	proxyVariables   = []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"}
	noProxyVariables = []string{"NO_PROXY", "no_proxy"}
)

// noProxy are the names reached without the proxy: the pod's own loopback,
// where a Direct box calls its seat's tools, and the cluster's own Services,
// which the NetworkPolicy alone governs and the proxy refuses anyway.
const noProxy = "localhost,127.0.0.1,::1,.svc,.cluster.local"

// quarantineOf is the quarantine the Company's namespace is under: "" for
// none, QuarantineEgress, or QuarantineFull. A value the operator does not
// know is read as full, so that a typo in an abuse case cuts more rather
// than less.
func (r *CompanyReconciler) quarantineOf(ctx context.Context, c *crewletaiv1alpha1.Company) (string, error) {
	ns, err := r.namespaceMeta(ctx, c.Namespace)
	if err != nil {
		return "", err
	}
	switch v, set := ns.Labels[crewletaiv1alpha1.QuarantineLabel]; {
	case !set:
		return "", nil
	case v == crewletaiv1alpha1.QuarantineEgress:
		return crewletaiv1alpha1.QuarantineEgress, nil
	default:
		return crewletaiv1alpha1.QuarantineFull, nil
	}
}

// namespaceMeta reads a namespace's metadata from the cache the operator
// already keeps of namespaces, for their labels and UID.
func (r *CompanyReconciler) namespaceMeta(ctx context.Context, name string) (*metav1.PartialObjectMetadata, error) {
	ns := &metav1.PartialObjectMetadata{}
	ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
	if err := r.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
		return nil, fmt.Errorf("reading Namespace %s: %w", name, err)
	}
	return ns, nil
}

// quarantinedCondition is the Company's Quarantined condition.
func quarantinedCondition(quarantine string) metav1.Condition {
	switch quarantine {
	case "":
		return metav1.Condition{Type: crewletaiv1alpha1.ConditionQuarantined, Status: metav1.ConditionFalse,
			Reason: crewletaiv1alpha1.ReasonNotQuarantined, Message: "The namespace carries no " +
				crewletaiv1alpha1.QuarantineLabel + " label."}
	case crewletaiv1alpha1.QuarantineEgress:
		return metav1.Condition{Type: crewletaiv1alpha1.ConditionQuarantined, Status: metav1.ConditionTrue,
			Reason: crewletaiv1alpha1.ReasonQuarantineEgress, Message: "The namespace is quarantined: the engine pods " +
				"connect nowhere outside the Company, and keep running as they are."}
	default:
		return metav1.Condition{Type: crewletaiv1alpha1.ConditionQuarantined, Status: metav1.ConditionTrue,
			Reason: crewletaiv1alpha1.ReasonQuarantineFull, Message: "The namespace is quarantined: the engine pods " +
				"connect nowhere outside the Company and serve no one but the operator, and keep running as they are."}
	}
}

// proxyURLValue is the egress proxy's address with the namespace's
// credential, as the bootstrap Secret holds it, or nil when the class names
// no proxy. missing says what could not be read; the pods are held back then
// rather than started with direct egress denied and no proxy to use.
//
// A quarantine does not withdraw it: rewriting the bootstrap Secret rolls
// every engine pod, and a quarantine keeps the pods as they are. The proxy
// refuses a quarantined namespace's credential instead, which is where a
// revocation belongs: at the one that checks it.
func (r *CompanyReconciler) proxyURLValue(
	ctx context.Context, c *crewletaiv1alpha1.Company, class *crewletaiv1alpha1.EngineClass,
) (value []byte, missing string, err error) {
	proxy := class.Spec.NetworkPolicy.Egress.Proxy
	if class.Spec.NetworkPolicy.Mode != crewletaiv1alpha1.NetworkPolicyIsolated || proxy.Host == "" {
		return nil, "", nil
	}
	ref := proxy.SigningKeySecretRef
	at := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	secret := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, at, secret); apierrors.IsNotFound(err) {
		return nil, fmt.Sprintf("Secret %s (EngineClass %s, spec.networkPolicy.egress.proxy.signingKeySecretRef) does not exist",
			at, class.Name), nil
	} else if err != nil {
		return nil, "", fmt.Errorf("reading Secret %s, which EngineClass %s references: %w", at, class.Name, err)
	}
	key := secret.Data[ref.Key]
	if len(key) == 0 {
		return nil, fmt.Sprintf("Secret %s (EngineClass %s, spec.networkPolicy.egress.proxy.signingKeySecretRef) has no key %s",
			at, class.Name, ref.Key), nil
	}
	ns, err := r.namespaceMeta(ctx, c.Namespace)
	if err != nil {
		return nil, "", err
	}
	u := url.URL{
		Scheme: "http",
		User:   url.UserPassword(c.Namespace, egresstoken.Token(key, c.Namespace, string(ns.UID))),
		Host:   net.JoinHostPort(proxy.Host, strconv.Itoa(int(proxy.Port))),
	}
	return []byte(u.String()), "", nil
}

// proxyEnv are the variables that send an engine pod's connections through
// the class's egress proxy, from the bootstrap Secret, or none.
func proxyEnv(class *crewletaiv1alpha1.EngineClass, secret string) []*corev1ac.EnvVarApplyConfiguration {
	if class.Spec.NetworkPolicy.Mode != crewletaiv1alpha1.NetworkPolicyIsolated || class.Spec.NetworkPolicy.Egress.Proxy.Host == "" {
		return nil
	}
	env := make([]*corev1ac.EnvVarApplyConfiguration, 0, len(proxyVariables)+len(noProxyVariables))
	for _, name := range proxyVariables {
		env = append(env, corev1ac.EnvVar().WithName(name).WithValueFrom(corev1ac.EnvVarSource().
			WithSecretKeyRef(corev1ac.SecretKeySelector().WithName(secret).WithKey(platformKeyProxyURL))))
	}
	for _, name := range noProxyVariables {
		env = append(env, corev1ac.EnvVar().WithName(name).WithValue(noProxy))
	}
	return env
}

// isProxyVariable reports whether a variable is one proxyEnv sets, which
// neither the class's env nor the Company's may replace under a proxy:
// pointing them elsewhere would only fail, since direct egress is denied,
// but blanking NO_PROXY would send the pod's own loopback through the proxy.
func isProxyVariable(name string) bool {
	return slices.Contains(proxyVariables, name) || slices.Contains(noProxyVariables, name)
}
