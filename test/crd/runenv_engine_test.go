package crd_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// reservedRunEnvFile is the engine's file whose ReservedSandboxEnv names the
// variables Crewlet sets in a coding run itself, which its config refuses in
// every env a run's environment is assembled from.
const reservedRunEnvFile = "internal/config/sandbox.go"

// TestReservedRunVariablesFollowTheEngine holds the API's refusal of the
// variables Crewlet sets in a coding run itself to Crewlet's, both ways, in
// each env a run's environment is assembled from (runEnvPlaces): the API
// refuses a variable there exactly when crewlet validate refuses the
// document it renders at that variable. The names offered are the ones the
// engine's source reserves (its config.ReservedSandboxEnv), so a name the
// engine comes to reserve fails here until the API refuses it too; the ones
// the API's rules state (reservedRunEnv, which
// TestReservedRunVariablesValidation tries each rule with), so a name the
// engine reserves no longer fails until the rules let it be; and the ones
// beside them (ownRunEnv), so a rule that refuses a name the engine takes
// fails as well. The two lists are held to each other besides.
func TestReservedRunVariablesFollowTheEngine(t *testing.T) {
	source, err := os.OpenRoot(enginetest.Source(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	src, err := source.ReadFile(reservedRunEnvFile)
	if err != nil {
		t.Fatal(err)
	}
	reserved := stringsOf(t, reservedRunEnvFile, src, "ReservedSandboxEnv")
	if !slices.Equal(reserved, reservedRunEnv) {
		t.Errorf("the engine reserves %v in a coding run's environment, and TestReservedRunVariablesValidation "+
			"tries the API's rules with %v", reserved, reservedRunEnv)
	}
	names := slices.Concat(reserved, reservedRunEnv, ownRunEnv)
	slices.Sort(names)
	names = slices.Compact(names)

	// Crewlet's verdict, on one document giving every name in each place:
	// each refusal names the variable it refuses, at its own path.
	in := sandboxCompany(setupStepWithEnv(names...), &v1.SeatSandbox{
		Env:   seatSandboxEnv(names...),
		Setup: setupStepWithEnv(names...),
	})
	crewletRefuses := make([]map[string]bool, len(runEnvPlaces))
	for i := range crewletRefuses {
		crewletRefuses[i] = map[string]bool{}
	}
	for _, p := range validateProblems(t, in) {
		place, name, ok := runEnvPlaceOf(p.Segments)
		if !ok {
			t.Fatalf("crewlet validate refused the document elsewhere than this test expects (%s): %s",
				render.PathOf(p.Segments), p.Message)
		}
		crewletRefuses[place][name] = true
	}

	// The API's, on one object for each place and name.
	namespace := newNamespace(t)
	refused, offered := 0, 0
	for i, place := range runEnvPlaces {
		for j, name := range names {
			offered++
			apiAdmits := admitted(t, k8sClient, place.give(namespace, fmt.Sprintf("e%d-%02d", i, j), name))
			if !apiAdmits {
				refused++
			}
			switch crewlet := crewletRefuses[i][name]; {
			case apiAdmits && crewlet:
				t.Errorf("%s in %s: the API admits it, and crewlet validate refuses it, so the company is applied "+
					"nowhere", name, place.name)
			case !apiAdmits && !crewlet:
				t.Errorf("%s in %s: the API refuses it, and crewlet validate takes it", name, place.name)
			}
		}
	}
	if refused == 0 || refused == offered {
		t.Errorf("the API refuses %d of %d variables: its rules are tried in one direction alone", refused, offered)
	}
}

// runEnvPlaceOf is which of runEnvPlaces a refusal of crewlet validate at
// path s is in, by its index, and the variable it names there: the place
// whose crewlet path is s up to its last step, a map key naming the
// variable.
func runEnvPlaceOf(s []engine.Segment) (place int, name string, ok bool) {
	if len(s) == 0 || s[len(s)-1].IsIndex {
		return 0, "", false
	}
	shape := make([]string, len(s)-1)
	for i, step := range s[:len(s)-1] {
		shape[i] = step.Key
		if step.IsIndex {
			shape[i] = "#"
		}
	}
	at := strings.Join(shape, ".")
	for i, p := range runEnvPlaces {
		if p.crewlet == at {
			return i, s[len(s)-1].Key, true
		}
	}
	return 0, "", false
}
