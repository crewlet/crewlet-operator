package controller

import (
	"context"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// These tests hold the operator to the engine's re-activation of its active
// revision at a write of a value that revision reads (Crewlet's ADR-0070):
// where it lands on the rendered document, the operator records it as its
// own apply and makes no second one; where it does not, the operator applies
// the document itself, as it did before the engine re-activated at all.

// newRotation is a reconcile of optionalRun's Company, under Enforce, whose
// engine runs the rendered document as r1, every value it reads pushed; and
// then the tavily Secret's value is rotated.
func newRotation(t *testing.T) *withdrawal {
	t.Helper()
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// The registry's Secret is back, unchanged: no value is withdrawn.
	if err := w.cluster.Create(ctx, valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})); err != nil {
		t.Fatal(err)
	}
	tavily := &corev1.Secret{}
	if err := w.cluster.Get(ctx, client.ObjectKey{Namespace: "ns", Name: requiredSecret}, tavily); err != nil {
		t.Fatal(err)
	}
	tavily.Data[requiredKey] = []byte("tvly-rotated")
	if err := w.cluster.Update(ctx, tavily); err != nil {
		t.Fatal(err)
	}
	return w
}

// tavilyName is the store name of the tavily Secret's key.
var tavilyName = secretsync.StoreName(secretref.Key{Namespace: "ns", Name: requiredSecret, Key: requiredKey})

// TestARotationIsTheEnginesOneActivation: the sync writes the rotated value,
// the engine re-activates the rendered document with it, and the operator
// records that revision as its own apply, in status.config and in the secret
// sync's state, rather than applying the same document again; the next
// reconcile finds it in step.
func TestARotationIsTheEnginesOneActivation(t *testing.T) {
	w := newRotation(t)
	if err := w.enforce(t); err != nil {
		t.Fatal(err)
	}
	recorded := drained(w.events)
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) {
		t.Fatalf("the revisions are %v, want r1 and the engine's re-activation r2 alone", got)
	}
	if got := w.engine.summaryOf(); !strings.HasPrefix(got, "reload: ") {
		t.Fatalf("r2's summary is %q, want the engine's re-activation", got)
	}
	w.engine.mu.Lock()
	stored := w.engine.store[tavilyName]
	w.engine.mu.Unlock()
	if got := stored; got != "tvly-rotated" {
		t.Fatalf("the store holds %q for tavily, want the rotated value", got)
	}
	config := w.run.config
	if config == nil || config.RevisionID != "r2" || config.Epoch == nil || *config.Epoch != 2 || config.AppliedHash == "" ||
		config.AppliedAt == nil {
		t.Fatalf("status.config = %+v, want r2 at epoch 2 recorded as applied", config)
	}
	if state := w.state(t); state.Recorded() != "r2" || !slices.Equal(state.LetGo("r2"), []string{w.registry}) {
		t.Fatalf("the state records %q letting %v go, want r2 read as the rendered document", state.Recorded(), state.LetGo("r2"))
	}
	if !slices.ContainsFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Normal Activated Activated revision r2 ") }) {
		t.Errorf("events %q, want r2's activation", recorded)
	}

	w.again(t, w.statusWritten())
	if err := w.enforce(t); err != nil {
		t.Fatal(err)
	}
	if got := w.engine.ids(); !slices.Equal(got, []string{"r1", "r2"}) {
		t.Fatalf("the next reconcile leaves the revisions %v, want r1 and r2 still", got)
	}
}

// TestARotationIsAppliedWhereTheEngineDidNotReactivate: an engine whose
// write re-activated nothing, or failed to, or whose re-activation somebody
// else's write overtook before the operator read it back, leaves the apply
// to the operator, which writes the rendered document itself.
func TestARotationIsAppliedWhereTheEngineDidNotReactivate(t *testing.T) {
	for _, tc := range []struct {
		name string
		// reload is what the engine answers of its re-activation instead
		// of making one; nil for one it makes.
		reload *map[string]any
		// overtaken is whether somebody else activates the rendered
		// document again right after the engine's re-activation.
		overtaken bool
		want      []string
	}{
		{name: "nothing read the value", reload: new(map[string]any(nil)), want: []string{"r1", "r2"}},
		{name: "the re-activation lost a race", reload: failedReload("revision_advanced", "another configuration write activated first"),
			want: []string{"r1", "r2"}},
		{name: "the re-activation could not be confirmed", reload: failedReload("unavailable", "the coordination store did not answer"),
			want: []string{"r1", "r2"}},
		{name: "somebody else's write overtook it", overtaken: true, want: []string{"r1", "r2", "r3", "r4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newRotation(t)
			w.engine.reloadAnswer = tc.reload
			if tc.overtaken {
				w.engine.afterSecretsReload = func(e *storeEngine) { e.activateOther(e.revisions[len(e.revisions)-1].document) }
			}
			if err := w.enforce(t); err != nil {
				t.Fatal(err)
			}
			last := tc.want[len(tc.want)-1]
			if got := w.engine.ids(); !slices.Equal(got, tc.want) {
				t.Fatalf("the revisions are %v, want %v, the operator's apply %s last", got, tc.want, last)
			}
			if got := w.engine.summaryOf(); got != "crewlet-operator: Secret ns/"+requiredSecret {
				t.Fatalf("%s's summary is %q, want the operator's apply for the rotated Secret", last, got)
			}
			if config := w.run.config; config == nil || config.RevisionID != last {
				t.Fatalf("status.config = %+v, want the operator's apply %s", config, last)
			}
			if state := w.state(t); state.Recorded() != last {
				t.Fatalf("the state records %q, want the operator's apply %s", state.Recorded(), last)
			}
		})
	}
}

// failedReload is the engine's answer on a re-activation that did not land,
// as a storeEngine's reloadAnswer.
func failedReload(code, detail string) *map[string]any {
	failed := refusal(code, detail)
	return &failed
}
