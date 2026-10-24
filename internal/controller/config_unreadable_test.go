package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
)

// These tests hold what the operator does with an engine whose keyring does
// not open the fleet's revision of the company document: the engine answers
// every read of the document, and every write, which reads the revision it
// replaces, 409 with no Retry-After, and nothing clears it but somebody giving
// the engine what opens the revision. So it is reported as what blocks the
// document, in the engine's words and with the remedy, and looked at again at
// the resync interval, not ten seconds on with a promise of a retry that
// changes nothing.

// keyringLost and sealMissing are the engine's answers to a read of revision,
// sealed under a key its keyring no longer holds or stored without a seal, as
// crewlet's configapi words them (refuseUnreadable) and its httpjson.Envelope
// sends them, the detail and the hint each ended as a sentence.
func keyringLost(revision string) string {
	return fmt.Sprintf(`{"error":"unreadable_revision","revision_id":%q,`+
		`"message":"That revision is sealed under a key this deployment no longer holds, so it cannot be read. `+
		`Put the key back in the node's keyring first.",`+
		`"detail":"secrets: open document: secret could not be decrypted.",`+
		`"hint":"It is sealed under a key that is no longer in the keyring; restore that key to the node's `+
		`secrets.keys first."}`, revision)
}

func sealMissing(revision string) string {
	return fmt.Sprintf(`{"error":"unsealed_revision","revision_id":%q,`+
		`"message":"That revision is stored without a seal, which a build older than the mandatory keyring wrote, `+
		`and a node reads only sealed revisions because the seal is what authenticates one. The hint says what `+
		`brings its document back.",`+
		`"detail":"secrets: this document is stored unsealed, and a node holding a keyring reads only sealed ones `+
		`— a plaintext payload is one anything that can write the store or the coordination bucket could have `+
		`authored.",`+
		`"hint":"It is stored without a seal, so nothing vouches for who wrote it and it is neither shown nor `+
		`reverted to; to have its document again, import it from your own copy with PUT /config or `+
		"`crewlet config import`"+`, which stores it sealed."}`, revision)
}

// TestARevisionTheEngineCannotOpenBlocksTheDocument holds an engine whose
// keyring does not open the fleet's revision to ConfigApplied=False
// RevisionUnreadable: the message names the revision, quotes the engine's
// account and its remedy, and says what restores it in the Company's terms;
// the operator writes nothing; and the next look is at the resync interval.
// Reported as an engine that is unavailable, it promised a retry in ten
// seconds that no retry could keep.
func TestARevisionTheEngineCannotOpenBlocksTheDocument(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(revision string) string
		says   []string
	}{
		{
			name: "a key the keyring lost", answer: keyringLost,
			says: []string{"revision r1", "secrets: open document: secret could not be decrypted",
				"restore that key to the node's secrets.keys first", "Secret acme-keyring"},
		},
		{
			name: "a revision stored without a seal", answer: sealMissing,
			says: []string{"revision r1", "stored unsealed", "import it from your own copy",
				"the engine's hint says what brings its document back"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			w.engine.unreadable = c.answer
			_, err := w.run.converge(ctx, w.client)
			if err == nil {
				t.Fatal("converge() = nil over a revision the engine's keyring does not open")
			}
			result := w.run.engineFailed(ctx, w.client, err)

			got := w.run.configApplied
			if got == nil || got.Reason != crewletaiv1alpha1.ReasonRevisionUnreadable {
				t.Fatalf("ConfigApplied = %+v, want False RevisionUnreadable", got)
			}
			for _, s := range c.says {
				if !strings.Contains(got.Message, s) {
					t.Errorf("ConfigApplied does not say %q: %s", s, got.Message)
				}
			}
			if strings.Contains(got.Message, "tries again") || strings.Contains(got.Message, ".).") ||
				!endsOnce(got.Message) {
				t.Errorf("ConfigApplied promises a retry, or ends the engine's words or its own with a second "+
					"full stop: %s", got.Message)
			}
			if result.RequeueAfter != w.run.resyncInterval() {
				t.Errorf("the next look is %s on, want the resync interval, %s", result.RequeueAfter, w.run.resyncInterval())
			}
			if ids := w.engine.ids(); len(ids) != 1 {
				t.Errorf("the engine holds revisions %v: the operator wrote over a revision the engine cannot open, "+
					"want nothing written", ids)
			}
			if recorded := drained(w.events); !warned(recorded, crewletaiv1alpha1.ReasonRevisionUnreadable) {
				t.Errorf("events %q, want a Warning RevisionUnreadable", recorded)
			}

			// The next reconcile meets the same answer with the condition
			// already recorded: it is not told again.
			meta.SetStatusCondition(&w.run.company.Status.Conditions, *got)
			w.again(t, w.run.company)
			if _, err := w.run.converge(ctx, w.client); err != nil {
				w.run.engineFailed(ctx, w.client, err)
			}
			if recorded := drained(w.events); warned(recorded, crewletaiv1alpha1.ReasonRevisionUnreadable) {
				t.Errorf("events %q, want the refusal ConfigApplied already reports told once", recorded)
			}
		})
	}
}

// TestAWithdrawalOverARevisionTheEngineCannotOpenWaitsForIt holds the
// withdrawal of a deleted Secret's value to the same next look: the engine
// cannot take its revision again while its keyring does not open it either,
// so the operator says so and looks again at the resync interval.
func TestAWithdrawalOverARevisionTheEngineCannotOpenWaitsForIt(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	w.engine.unreadable = keyringLost
	result, err := w.run.withdrawHeld(ctx, w.resolve(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != w.run.resyncInterval() {
		t.Errorf("the next look is %s on, want the resync interval, %s", result.RequeueAfter, w.run.resyncInterval())
	}
	recorded := drained(w.events)
	if !warned(recorded, "SecretWithdrawalFailed") ||
		!strings.Contains(strings.Join(recorded, "\n"), "unreadable_revision") {
		t.Errorf("events %q, want SecretWithdrawalFailed quoting the engine's refusal", recorded)
	}
	if !w.engine.holds(w.registry) || len(w.engine.ids()) != 1 {
		t.Errorf("the store holds the registry: %t, and the revisions are %v; want both as they were",
			w.engine.holds(w.registry), w.engine.ids())
	}
}
