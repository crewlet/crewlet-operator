package crd_test

import (
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// msgReservedRunEnv is the message of the rules that refuse, in an env a
// coding run's environment is assembled from, a variable Crewlet sets in the
// run itself.
const msgReservedRunEnv = "are the engine's: Crewlet sets them in a coding run itself"

var (
	// reservedRunEnv are the variables Crewlet sets in a coding run itself,
	// its config.ReservedSandboxEnv, which
	// TestReservedRunVariablesFollowTheEngine reads from the engine's source.
	reservedRunEnv = []string{
		"CREWLET_GIT_TOKEN", "CREWLET_GIT_URL", "CREWLET_GIT_TRAILER_SEAT", "CREWLET_GIT_TRAILER_RUN",
		"CREWLET_GIT_TRAILER_WORK_ITEM",
	}
	// ownRunEnv are names beside them that a run's environment may hold: a
	// name Crewlet's own test of its rule takes, a prefix of the trailers'
	// names and an extension of the token's, the variable that tells a node
	// its boxes' git address (CREWLET_BOX_GIT_URL, which the engine reads from
	// the node's environment, never a run's), the count git reads its
	// configuration from the environment by, which Crewlet keeps and appends
	// to, the token's name in another case, and a token for another git host.
	ownRunEnv = []string{
		"CREWLET_GIT_PROXY", "CREWLET_GIT_TRAILER", "CREWLET_GIT_TOKEN_FILE", "CREWLET_BOX_GIT_URL",
		"GIT_CONFIG_COUNT", "crewlet_git_token", "GITLAB_TOKEN",
	}
)

// runEnvPlace is one env a coding run's environment is assembled from: the
// field of a variable there that the API refuses one of reservedRunEnv at,
// the place the render writes the env to in the company document, where
// crewlet validate refuses one (its path up to the variable, a list index
// written #), and the object that gives one variable there.
type runEnvPlace struct {
	name    string
	at      string
	crewlet string
	give    func(namespace, object, variable string) client.Object
}

// runEnvPlaces are the envs a coding run's environment is assembled from: a
// Company's setup step's, a Seat's setup step's and the Seat's own, each of
// which Crewlet refuses a variable it sets in the run itself in.
var runEnvPlaces = []runEnvPlace{
	{
		name: "a Company's setup step", at: "spec.sandbox.setup[0].env[0].name",
		crewlet: "providers.sandbox.setup.#.env",
		give: func(namespace, object, variable string) client.Object {
			c := company(namespace, object)
			c.Spec.Sandbox = v1.SandboxSettings{
				E2B:   v1.E2BSandbox{APIKeySecretRef: secretKey("e2b", apiKeyKey)},
				Setup: setupStepWithEnv(variable),
			}
			return c
		},
	},
	{
		name: "a Seat's setup step", at: "spec.sandbox.setup[0].env[0].name",
		crewlet: "roles.#.sandbox.setup.#.env",
		give: func(namespace, object, variable string) client.Object {
			s := seat(namespace, object)
			s.Spec.Sandbox = &v1.SeatSandbox{Setup: setupStepWithEnv(variable)}
			return s
		},
	},
	{
		name: "a Seat's sandbox env", at: "spec.sandbox.env[0].name",
		crewlet: "roles.#.sandbox.env",
		give: func(namespace, object, variable string) client.Object {
			s := seat(namespace, object)
			s.Spec.Sandbox = &v1.SeatSandbox{Env: seatSandboxEnv(variable)}
			return s
		},
	},
}

// setupStepWithEnv is one setup step whose env gives each of variables, read
// from a Secret.
func setupStepWithEnv(variables ...string) []v1.SandboxSetupStep {
	env := make([]v1.SandboxEnvVar, len(variables))
	for i, name := range variables {
		env[i] = v1.SandboxEnvVar{Name: name, ValueFrom: fromSecret("company-sandbox", "value")}
	}
	return []v1.SandboxSetupStep{{Name: "git", Env: env}}
}

// seatSandboxEnv is a Seat's spec.sandbox.env giving each of variables, read
// from a Secret.
func seatSandboxEnv(variables ...string) []v1.SeatEnvVar {
	env := make([]v1.SeatEnvVar, len(variables))
	for i, name := range variables {
		env[i] = v1.SeatEnvVar{Name: name, ValueFrom: seatFromSecret("coder-sandbox", "value")}
	}
	return env
}

// TestReservedRunVariablesValidation covers the API's refusal of the
// variables Crewlet sets in a coding run itself, in each env a run's
// environment is assembled from: each is refused, with the rule's message, at
// the variable's own name, where Crewlet refuses it too (at
// roles[i].sandbox.env.<name>, for one), and every name beside them is taken
// (TestReservedRunVariablesFollowTheEngine holds the rules to the engine).
func TestReservedRunVariablesValidation(t *testing.T) {
	namespace := newNamespace(t)
	objects := 0
	object := func() string {
		objects++
		return fmt.Sprintf("r%02d", objects)
	}
	for _, place := range runEnvPlaces {
		for _, variable := range reservedRunEnv {
			t.Run(variable+" in "+place.name+" is refused", func(t *testing.T) {
				err := k8sClient.Create(ctx, place.give(namespace, object(), variable))
				expect(t, err, msgReservedRunEnv)
				if !strings.Contains(err.Error(), place.at+": ") {
					t.Errorf("refused elsewhere than at %s: %v", place.at, err)
				}
			})
		}
		for _, variable := range ownRunEnv {
			t.Run(variable+" in "+place.name+" is accepted", func(t *testing.T) {
				expect(t, k8sClient.Create(ctx, place.give(namespace, object(), variable)), "")
			})
		}
	}
}
