package controller

import (
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// EngineResolver finds the API of a Company's engine and the credential the
// operator reaches it with.
//
// It is an interface so that the config reconciler's tests can hand it an
// engine of their own, a real one started on the test's machine, where no
// data pod runs.
type EngineResolver interface {
	// Engine returns a client for the Company's engine, authenticated as the
	// operator. An error says the engine cannot be reached yet (its token
	// does not exist, for one) and is reported on the Company; the reconcile
	// tries again later.
	Engine(ctx context.Context, c *crewletaiv1alpha1.Company) (*engine.Client, error)
}

// InClusterEngines resolves a Company's engine to its first data pod, through
// the headless data Service, with the operator token from the Company's API
// tokens Secret. It is the resolver the manager runs with.
type InClusterEngines struct {
	// Reader reads the API tokens Secret. It must be an uncached reader:
	// the manager caches Secrets as metadata only.
	Reader client.Reader

	// HTTPClient carries every engine request, so that the clients built
	// for each reconcile share one pool of connections.
	HTTPClient *http.Client
}

// Engine implements EngineResolver.
//
// The address is the data pod's own, not the engine Service's, because the
// Service routes only to ready pods and an engine with no company document
// is not ready: it answers /ready with 503 "unconfigured" until a document
// is applied, so the first document could never be applied through the
// Service. The pod's name in the headless Service resolves while the pod
// runs, ready or not (publishNotReadyAddresses).
func (e *InClusterEngines) Engine(ctx context.Context, c *crewletaiv1alpha1.Company) (*engine.Client, error) {
	ref := types.NamespacedName{Namespace: c.Namespace, Name: apiTokensSecretName(c)}
	tokens := &corev1.Secret{}
	if err := e.Reader.Get(ctx, ref, tokens); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("the engine's API tokens Secret %s does not exist yet", ref)
		}
		return nil, fmt.Errorf("reading the engine's API tokens Secret %s: %w", ref, err)
	}
	token := tokens.Data[render.TokenOperator]
	if len(token) == 0 {
		return nil, fmt.Errorf("the engine's API tokens Secret %s has no %q token", ref, render.TokenOperator)
	}
	return engine.New(dataPodURL(c), string(token), engine.WithHTTPClient(e.HTTPClient)), nil
}

// dataPodURL is the base URL of the API of a Company's first data pod.
func dataPodURL(c *crewletaiv1alpha1.Company) string {
	return fmt.Sprintf("http://%s-0.%s.%s.svc:%d", dataStatefulSetName(c), dataServiceName(c), c.Namespace, render.APIPort)
}
