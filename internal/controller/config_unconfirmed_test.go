package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// These tests hold what the operator does with a write whose activation the
// engine sent to its fleet and could not confirm: it never sends the write
// again on top of it, and a write that did land is recorded as its own, under
// Enforce and under Seed alike. The engine gives that answer only when its
// coordination store loses the answer to a compare-and-set, which a real
// engine cannot be made to do at will, so the storeEngine stands in for it.

// outcome is how the activation of the storeEngine's next write goes.
type outcome int

const (
	// confirmed: it activates, and the engine says so.
	confirmed outcome = iota
	// landed: it activates, and the engine's answer is lost.
	landed
	// pending: the revision is stored and not activated, and the engine's
	// answer is lost.
	pending
)

// land activates the inert revision id, as a compare-and-set whose answer was
// lost lands after all. The caller holds e's lock.
func (e *storeEngine) land(id string) {
	i := slices.IndexFunc(e.inert, func(r storeRevision) bool { return r.id == id })
	e.revisions = append(e.revisions, e.inert[i])
	e.inert = slices.Delete(e.inert, i, i+1)
}

// r1Hash is the appliedHash a test records r1 under where r1 is the
// operator's own write.
const r1Hash = "sha256:r1"

// answerLost is the engine's answer to a write whose activation it sent and
// whose answer its coordination store did not give (crewlet's
// internal/api/configapi, RefuseActivationUnknown): 503 unavailable with the
// unknown outcome, the revision it stored, the node whose history alone holds
// it, the revision the write was built on, and a hint saying how to look
// first, ended as httpjson.Sentence ends it, under the Retry-After of an
// undecided answer (authz.RetryUndecidedSeconds).
func answerLost(w http.ResponseWriter, stored, base string) {
	hint := fmt.Sprintf("Revision %[1]s was stored on node %[2]s, and the coordination store did not answer its "+
		"activation, so whether the fleet runs it is unknown. GET /config names the active revision once the store "+
		"answers again. If it is not %[1]s, the revision is in node %[2]s's history alone: send GET "+
		"/config/revisions/%[1]s/diff, and POST /config/revisions/%[1]s/revert to make it the company, to that "+
		"node \u2014 by its own address where a shared one fronts several, since any other node holds no copy and "+
		"answers 404. Sending the write again makes another revision.", stored, storeNode)
	body := map[string]string{
		"error": "unavailable", "message": "Can't answer right now. Try again in a moment, or ask another node.",
		"outcome": "unknown", "stored_revision_id": stored, "node": storeNode, "hint": hint,
	}
	if base != "" {
		body["your_base"] = base
	}
	w.Header().Set("Retry-After", "2")
	answerJSON(w, http.StatusServiceUnavailable, body)
}

// unconfirmedWrite is the withdrawal's reconcile under Enforce after the
// operator's write of the rendered document, built on r1, came back
// unconfirmed as revision r2, which how lands or keeps inert; and the Company
// as that reconcile's status write leaves it. A reconcile that fails is
// reported as the reconciler reports it, and retried.
func unconfirmedWrite(t *testing.T, how outcome) (*withdrawal, *crewletaiv1alpha1.Company) {
	t.Helper()
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	// r1 is the rendered document and lets the registry go, so the
	// withdrawal alone is settled by activating r1 again; a rotated value
	// is what has the operator write the document, where the engine's own
	// re-activation at the sync's write fails.
	w.rotate(t)
	w.engine.activation = how
	operatorActivates(w)
	reconcileOnce(t, w)
	w.engine.activation, w.engine.reloadAnswer = confirmed, nil
	company := w.statusWritten()
	return w, company
}

// operatorActivates makes the engine's own re-activation at a sync's write or
// removal fail, as an engine whose coordination store does not answer it
// does, so that the activation is the operator's: the write or the reload
// whose answer these tests lose.
func operatorActivates(w *withdrawal) {
	w.engine.reloadAnswer = failedReload("unavailable", "the coordination store did not answer")
}

// reconcileOnce runs w's reconcile up to its status write, as the reconciler
// does: an error it fails with is reported on ConfigApplied.
func reconcileOnce(t *testing.T, w *withdrawal) {
	t.Helper()
	if _, err := w.run.converge(context.Background(), w.client); err != nil {
		t.Logf("the reconcile failed, and is retried: %v", err)
		w.run.unavailable(err)
	}
}

// TestAnActivationTheEngineCouldNotConfirmIsRecorded: the write the engine
// could not confirm is recorded with what a confirmed one records, and the
// reconcile asks again soon, reporting it.
func TestAnActivationTheEngineCouldNotConfirmIsRecorded(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	got := company.Status.Config.Unconfirmed
	if len(got) != 1 || got[0].RevisionID != "r2" || got[0].BaseRevisionID != "r1" || got[0].AppliedHash == "" ||
		got[0].WrittenAt.IsZero() {
		t.Fatalf("status.config.unconfirmed is %+v, want r2, built on r1, with its hash and when", got)
	}
	c := w.run.configApplied
	if c == nil || c.Status != metav1.ConditionFalse || c.Reason != crewletaiv1alpha1.ReasonEngineUnavailable ||
		!strings.Contains(c.Message, "revision r2 and could not confirm") {
		t.Errorf("ConfigApplied is %+v, want False, EngineUnavailable, saying r2 could not be confirmed", c)
	}
}

// TestAnActivationTheEngineCouldNotConfirmIsNotWrittenTwice: the write did
// land, so the next reconcile finds the engine running it, records it as the
// operator's at the epoch the fleet's activation names, and writes nothing;
// and its activation is recorded as a confirmed write's is, as the one that
// read the store without the withdrawal's registry, which the sync before it
// deleted, so nothing activates r2 again for it either. The reading recorded
// for it goes, nothing unconfirmed naming it any more.
func TestAnActivationTheEngineCouldNotConfirmIsNotWrittenTwice(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	claims := company.Status.Config.Unconfirmed
	if state := w.state(t); !slices.Equal(state.Withdrawn(), []string{w.registry}) {
		t.Fatalf("the write's sync withdrew %v, want the registry", state.Withdrawn())
	}
	w.again(t, company)
	reconcileOnce(t, w)
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
		t.Fatalf("the operator wrote %v, want r2 alone: the engine ran it", written)
	}
	if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2"}) {
		t.Errorf("the engine's revisions are %v, want r1 and r2: r2 activated without the registry", ids)
	}
	if state := w.state(t); len(state.Withdrawn()) != 0 || !state.Records("r2") ||
		(len(claims) == 1 && state.Pends(claims[0].AppliedHash)) {
		t.Errorf("the state owes %v and records %q, want nothing owed, r2's reading, and no reading left pending, "+
			"since nothing unconfirmed names it", state.Withdrawn(), state.Recorded())
	}
	config := w.statusWritten().Status.Config
	if len(claims) != 1 || config.AppliedHash != claims[0].AppliedHash || len(config.Unconfirmed) != 0 {
		t.Errorf("status.config is %+v, want r2's write recorded, with its hash, and nothing unconfirmed", config)
	}
	recorded := drained(w.events)
	if i := slices.IndexFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Normal Activated ") }); i < 0 ||
		!strings.Contains(recorded[i], "revision r2 as epoch 2") {
		t.Errorf("events %q, want an Activated event saying the engine runs r2 as epoch 2", recorded)
	}
}

// TestAnActivationThatLandsLateIsTheOperatorsOwn: the write had not landed by
// the reconcile that wrote it, and lands before the next one reads, which
// records it as the operator's write and says so, rather than adopting it as
// a revision of the rendered document somebody else activated.
func TestAnActivationThatLandsLateIsTheOperatorsOwn(t *testing.T) {
	w, company := unconfirmedWrite(t, pending)
	w.engine.mu.Lock()
	w.engine.land("r2")
	w.engine.mu.Unlock()
	w.again(t, company)
	reconcileOnce(t, w)
	written, _ := w.engine.writes()
	if config := w.statusWritten().Status.Config; !slices.Equal(written, []string{"r2"}) || len(config.Unconfirmed) != 0 {
		t.Errorf("the operator wrote %v and keeps %+v unconfirmed, want r2 alone, recorded", written, config.Unconfirmed)
	}
	if recorded := drained(w.events); !slices.ContainsFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r2 as epoch 2")
	}) {
		t.Errorf("events %q, want an Activated event saying the engine runs r2, the operator's write, as epoch 2", recorded)
	}
}

// TestAnActivationThatDidNotLandIsSentAgainOnItsCondition: the engine still
// runs r1, so the write is sent again on r1, and lands; and where the first
// one lands just ahead of it, the engine refuses it, and the reconcile finds
// the first one running and records it rather than writing a third.
func TestAnActivationThatDidNotLandIsSentAgainOnItsCondition(t *testing.T) {
	t.Run("it never lands", func(t *testing.T) {
		w, company := unconfirmedWrite(t, pending)
		w.again(t, company)
		reconcileOnce(t, w)
		config := w.statusWritten().Status.Config
		if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2", "r3"}) || len(config.Unconfirmed) != 0 {
			t.Errorf("the operator wrote %v and keeps %+v unconfirmed; want r3 written again on r1, and nothing "+
				"unconfirmed", written, config.Unconfirmed)
		}
	})
	t.Run("it lands ahead of the second", func(t *testing.T) {
		w, company := unconfirmedWrite(t, pending)
		w.engine.beforeWrite = func(e *storeEngine) { e.land("r2") }
		w.again(t, company)
		reconcileOnce(t, w)
		config := w.statusWritten().Status.Config
		if written, refused := w.engine.writes(); !slices.Equal(written, []string{"r2"}) || refused != 1 ||
			len(config.Unconfirmed) != 0 {
			t.Errorf("the operator wrote %v, %d refused, and keeps %+v unconfirmed; want the second write refused "+
				"and r2 recorded", written, refused, config.Unconfirmed)
		}
		if recorded := drained(w.events); !told(recorded, "Activated") {
			t.Errorf("events %q, want an Activated event for r2", recorded)
		}
	})
}

// TestAnUnconfirmedActivationIsNeverWrittenOverUnread: the engine runs the
// unconfirmed revision and its fleet cannot be read, so the operator cannot
// tell at what epoch: it writes nothing, keeps the record, and says so.
func TestAnUnconfirmedActivationIsNeverWrittenOverUnread(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	w.engine.fleetDown = true
	w.again(t, company)
	if _, err := w.run.converge(context.Background(), w.client); err == nil {
		t.Fatal("the reconcile succeeded with no read of the fleet's activation")
	}
	kept := w.statusWritten().Status.Config.Unconfirmed
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) || len(kept) != 1 {
		t.Errorf("the operator wrote %v and keeps %+v, want r2 alone, still unconfirmed", written, kept)
	}
}

// TestAnUnconfirmedActivationOtherwiseOvertakenIsNotSentAgain: somebody
// activated r3 since, so the write, conditioned on r1, can never land: it
// goes rather than being sent again, and not to status.config.overtaken,
// which keeps only the writes into an engine that ran no document; and the
// next write, r4 on r3, whose answer is lost too, is recorded unconfirmed.
func TestAnUnconfirmedActivationOtherwiseOvertakenIsNotSentAgain(t *testing.T) {
	w, company := unconfirmedWrite(t, pending)
	w.engine.mu.Lock()
	w.engine.activateOther(w.requiring)
	w.engine.mu.Unlock()
	w.again(t, company)
	w.engine.activation = pending
	reconcileOnce(t, w)
	config := w.statusWritten().Status.Config
	got := config.Unconfirmed
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2", "r4"}) || len(got) != 1 ||
		got[0].RevisionID != "r4" || got[0].BaseRevisionID != "r3" {
		t.Errorf("the operator wrote %v and keeps %+v unconfirmed, want r2 not sent again and r4, built on r3, kept",
			written, got)
	}
	if over := config.Overtaken; len(over) != 0 {
		t.Errorf("status.config.overtaken is %+v, want nothing: r2 was built on r1, and no Seed reads it", over)
	}
}

// TestALandedWriteActivatedAgainStaysInSyncThroughAReconcileThatFails: the
// write landed and was activated again (r3) before the next reconcile read it,
// and that reconcile failed before it decided anything with it: its save of
// the secret sync's state did not go through. Nothing records the write any
// more, r3 having overtaken it, and nothing needs to: the withdrawal its sync
// made and the values it wrote were marked with the fleet's epoch before it
// was sent, and r3 activated above it, so the next reconcile finds r3 in sync,
// records it at its own epoch, and owes nothing for it. Kept owed, the
// withdrawal had the operator activate r3 again, and the values had it write
// the same document again as r4.
func TestALandedWriteActivatedAgainStaysInSyncThroughAReconcileThatFails(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	claims := company.Status.Config.Unconfirmed
	if _, err := w.client.ReloadConfig(context.Background(), "reloaded by the engine"); err != nil {
		t.Fatal(err)
	}
	w.again(t, company)
	w.stateWrites.Store(0)
	w.failWrite.Store(1)
	reconcileOnce(t, w)
	company = w.statusWritten()
	if config := company.Status.Config; len(config.Unconfirmed) != 0 || len(config.Overtaken) != 0 {
		t.Fatalf("status.config after a reconcile that failed is %+v, want r2 gone: r3 overtook it", config)
	}
	if state := w.state(t); !slices.Equal(state.Withdrawn(), []string{w.registry}) {
		t.Fatalf("the state owes %v, want the registry: the reconcile that failed saved nothing", state.Withdrawn())
	}

	w.failWrite.Store(0)
	w.again(t, company)
	reconcileOnce(t, w)
	if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2", "r3"}) {
		t.Errorf("the engine's revisions are %v, want r1 to r3: the operator wrote, or activated, what already ran", ids)
	}
	config := w.statusWritten().Status.Config
	if len(claims) != 1 || config.RevisionID != "r3" || config.AppliedHash != claims[0].AppliedHash || config.Epoch == nil ||
		*config.Epoch != 3 {
		t.Errorf("status.config is %+v, want r3 at epoch 3 with r2's hash, the values it was written with", config)
	}
	if state := w.state(t); len(state.Withdrawn()) != 0 || len(state.Written()) != 0 {
		t.Errorf("the state owes withdrawals %v and writes %v, want nothing: r3 activated after both", state.Withdrawn(),
			state.Written())
	}
}

// TestAHeldDocumentSettlesAnUnconfirmedWriteThatRuns: the first reconcile
// after the write holds its document, and the engine runs the write, which
// landed, or a reload of it. Either read the store without the withdrawal's
// registry, which the sync before the write deleted, so the held reconcile
// settles it and activates nothing again for it. Settled only by a reconcile
// that applies a document, it was owed there, and the held reconcile activated
// the running revision again: an epoch every node applied for nothing. The
// record of the write, and the report, are left to the reconcile that applies
// a document.
func TestAHeldDocumentSettlesAnUnconfirmedWriteThatRuns(t *testing.T) {
	for _, reloaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "the write runs", true: "a reload of the write runs"}[reloaded], func(t *testing.T) {
			w, company := unconfirmedWrite(t, landed)
			want := []string{"r1", "r2"}
			if reloaded {
				if _, err := w.client.ReloadConfig(context.Background(), "reloaded by a person"); err != nil {
					t.Fatal(err)
				}
				want = append(want, "r3")
			}
			w.again(t, company)
			if _, err := w.run.withdrawHeld(context.Background(), w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			if ids := w.engine.ids(); !slices.Equal(ids, want) {
				t.Errorf("the engine's revisions are %v, want %v: what runs read the store without the registry", ids, want)
			}
			if recorded := drained(w.events); told(recorded, "Reactivated") {
				t.Errorf("events %q, want no Reactivated: nothing was owed", recorded)
			}
			state := w.state(t)
			if len(state.Withdrawn()) != 0 || (!reloaded && !state.Records("r2")) {
				t.Errorf("the state owes %v and records %q, want nothing owed, and r2's reading where r2 runs",
					state.Withdrawn(), state.Recorded())
			}
			if w.run.config != nil || w.run.configApplied != nil {
				t.Errorf("the held reconcile recorded %+v and reported %+v, want both left to the one that applies a "+
					"document", w.run.config, w.run.configApplied)
			}
		})
	}
}

// TestAConfirmationThatLosesItsRaceIsReadAgain: the engine runs the write as
// this reconcile reads it, and by the read of its fleet, which says as what
// epoch, a reload has activated after it. That is a lost race, read again
// from the start, which finds the reload running the write's document with its
// values: in sync, and recorded at its own epoch, with nothing written. A fleet
// answering without its activation is no race, and the reconcile fails over
// it without claiming one, and records nothing.
func TestAConfirmationThatLosesItsRaceIsReadAgain(t *testing.T) {
	t.Run("another revision activated", func(t *testing.T) {
		w, company := unconfirmedWrite(t, landed)
		claims := company.Status.Config.Unconfirmed
		w.engine.mu.Lock()
		w.engine.beforeFleet = func(e *storeEngine) { e.activateOther(e.active().document) }
		w.engine.mu.Unlock()
		w.again(t, company)
		if _, err := w.run.converge(context.Background(), w.client); err != nil {
			t.Fatalf("converge() = %v, want the reconcile to read the engine again and find it in sync", err)
		}
		if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2", "r3"}) {
			t.Errorf("the engine's revisions are %v, want r1 to r3: nothing written", ids)
		}
		config := w.statusWritten().Status.Config
		if len(claims) != 1 || config.RevisionID != "r3" || config.Epoch == nil || *config.Epoch != 3 ||
			config.AppliedHash != claims[0].AppliedHash {
			t.Errorf("status.config is %+v, want r3 at epoch 3 with r2's hash", config)
		}
	})
	t.Run("the fleet answers without its activation", func(t *testing.T) {
		w, company := unconfirmedWrite(t, landed)
		w.engine.mu.Lock()
		w.engine.noActivation = true
		w.engine.mu.Unlock()
		w.again(t, company)
		_, err := w.run.converge(context.Background(), w.client)
		if err == nil || errors.Is(err, engine.ErrRevisionAdvanced) || strings.Contains(err.Error(), "instead") {
			t.Errorf("converge() = %v, want an error that claims no race", err)
		}
		if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
			t.Errorf("the operator wrote %v, want r2 alone", written)
		}
		if got := w.statusWritten().Status.Config.Unconfirmed; len(got) != 1 || got[0].RevisionID != "r2" {
			t.Errorf("status.config.unconfirmed is %+v, want r2 kept", got)
		}
	})
}

// TestAConfirmedWriteIsReportedAppliedOnceTheEngineAppliesIt: the write's own
// reconcile reported it as one the engine could not confirm, and the next
// finds it running and its epoch applied: an Applied event names it, as one
// does for a write the engine confirmed, although the stored ConfigApplied is
// neither Applying nor ApplyFailed.
func TestAConfirmedWriteIsReportedAppliedOnceTheEngineAppliesIt(t *testing.T) {
	ctx := context.Background()
	w, company := unconfirmedWrite(t, landed)
	meta.SetStatusCondition(&company.Status.Conditions, *w.run.configApplied)
	w.again(t, company)
	reconcileOnce(t, w)
	h, err := w.client.Health(ctx)
	w.run.verifyApplied(ctx, w.client, h, err, ctrl.Result{})
	var applied []string
	for _, e := range drained(w.events) {
		if strings.HasPrefix(e, "Normal "+crewletaiv1alpha1.ReasonApplied+" ") {
			applied = append(applied, e)
		}
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "revision r2 (epoch 2)") {
		t.Errorf("Applied events %q, want one naming revision r2 at epoch 2", applied)
	}
}

// TestAValueWrittenBackIsSettledByTheWriteThatCarriedIt: the store lost the
// operator's value, so the sync wrote it back and the document was written
// again, as r3, whose activation the engine could not confirm. The engine runs
// r3, or a reload of it, either of which read the store with the value, so the
// next reconcile owes nothing for it and writes nothing. Kept owed, the value
// had the operator write the document once more.
func TestAValueWrittenBackIsSettledByTheWriteThatCarriedIt(t *testing.T) {
	for _, reloaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "the write runs", true: "a reload of the write runs"}[reloaded], func(t *testing.T) {
			w := newWithdrawal(t, runningLetsGo)
			operatorActivates(w)
			if err := w.enforce(t); err != nil {
				t.Fatal(err)
			}
			tavily := secretsync.StoreName(secretref.Key{Namespace: "ns", Name: requiredSecret, Key: requiredKey})
			w.engine.mu.Lock()
			delete(w.engine.store, tavily)
			w.engine.mu.Unlock()
			w.again(t, w.statusWritten())
			w.engine.activation = landed
			reconcileOnce(t, w)
			w.engine.activation = confirmed
			company := w.statusWritten()
			if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r3"}) ||
				!slices.Equal(w.state(t).Written(), []string{tavily}) {
				t.Fatalf("the operator wrote %v and the state owes %v; want r3 written for the value written back, "+
					"and the value owed", written, w.state(t).Written())
			}
			want := []string{"r1", "r2", "r3"}
			if reloaded {
				if _, err := w.client.ReloadConfig(context.Background(), "reloaded by a person"); err != nil {
					t.Fatal(err)
				}
				want = append(want, "r4")
			}
			w.again(t, company)
			reconcileOnce(t, w)
			if ids := w.engine.ids(); !slices.Equal(ids, want) {
				t.Errorf("the engine's revisions are %v, want %v: what runs read the value written back", ids, want)
			}
			if got := w.state(t).Written(); len(got) != 0 {
				t.Errorf("the state owes %v, want nothing", got)
			}
		})
	}
}

// TestASeedTheEngineCouldNotConfirmIsReportedAsTheSeed: under Seed, a write
// into an engine with no document that landed unconfirmed is the seed, not a
// document the engine had before the operator seeded one.
func TestASeedTheEngineCouldNotConfirmIsReportedAsTheSeed(t *testing.T) {
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	w.engine.mu.Lock()
	w.engine.revisions = nil
	w.engine.mu.Unlock()
	company := w.run.company.DeepCopy()
	company.Spec.Config.ManagementPolicy = crewletaiv1alpha1.ManagementPolicySeed
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{}
	w.again(t, company)
	w.engine.activation = landed
	reconcileOnce(t, w)
	written, _ := w.engine.writes()
	if len(written) != 1 {
		t.Fatalf("the operator wrote %v, want the seed", written)
	}
	seed := written[0]
	company = w.statusWritten()
	if got := company.Status.Config.Unconfirmed; len(got) != 1 || got[0].RevisionID != seed || got[0].BaseRevisionID != "" {
		t.Errorf("status.config.unconfirmed is %+v, want the seed %s, built on nothing", got, seed)
	}
	w.engine.activation = confirmed
	w.again(t, company)
	reconcileOnce(t, w)
	if c := w.run.configApplied; c == nil || c.Reason != crewletaiv1alpha1.ReasonSeeded ||
		!strings.Contains(c.Message, "seeded with the rendered document as revision "+seed) {
		t.Errorf("ConfigApplied is %+v, want Seeded, naming %s as the seed", c, seed)
	}
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{seed}) {
		t.Errorf("the operator wrote %v, want the seed alone", written)
	}
}

// TestALandedWriteActivatedAgainIsNotWrittenAgain: the write landed, and was
// activated again before the next reconcile read it, by the engine's own
// reload after it minted a credential or by a person's crewlet config reload,
// so the engine runs the rendered document as r3, with the values r2 was
// written with. Dropped as a write that can no longer land, it left
// status.config's hash the one before it, and the operator wrote the same
// document a third time. It is in sync instead: r3 is recorded at its own
// epoch with r2's hash, and the withdrawal r2's sync made, which r3 activated
// after, is settled, so nothing activates it again either.
func TestALandedWriteActivatedAgainIsNotWrittenAgain(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	claims := company.Status.Config.Unconfirmed
	if _, err := w.client.ReloadConfig(context.Background(), "reloaded by a person"); err != nil {
		t.Fatal(err)
	}
	w.again(t, company)
	reconcileOnce(t, w)
	if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2", "r3"}) {
		t.Fatalf("the engine's revisions are %v, want r1 to r3: the operator wrote, or activated, what already ran", ids)
	}
	config := w.statusWritten().Status.Config
	if len(claims) != 1 || config.RevisionID != "r3" || config.AppliedHash != claims[0].AppliedHash ||
		config.Epoch == nil || *config.Epoch != 3 || len(config.Unconfirmed) != 0 {
		t.Errorf("status.config is %+v, want r3 at epoch 3 with r2's hash, and nothing unconfirmed", config)
	}
	if c := w.run.configApplied; c == nil || c.Status != metav1.ConditionTrue {
		t.Errorf("ConfigApplied is %+v, want True: the engine runs the rendered document", c)
	}
	if state := w.state(t); len(state.Withdrawn()) != 0 {
		t.Errorf("the state owes %v, want nothing: r3 activated after the registry was deleted", state.Withdrawn())
	}
}

// TestASeedActivatedAgainIsReportedAsTheSeed: under Seed, the write into an
// engine with no document landed unconfirmed and was activated again before
// the next reconcile read it, so the engine runs another revision. It is
// still the seed, and the engine runs the revision that replaced it, not a
// document it had before the operator seeded one.
func TestASeedActivatedAgainIsReportedAsTheSeed(t *testing.T) {
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	w.engine.mu.Lock()
	w.engine.revisions = nil
	w.engine.mu.Unlock()
	company := w.run.company.DeepCopy()
	company.Spec.Config.ManagementPolicy = crewletaiv1alpha1.ManagementPolicySeed
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{}
	w.again(t, company)
	w.engine.activation = landed
	reconcileOnce(t, w)
	w.engine.activation = confirmed
	written, _ := w.engine.writes()
	if len(written) != 1 {
		t.Fatalf("the operator wrote %v, want the seed", written)
	}
	seed := written[0]
	reloaded, err := w.client.ReloadConfig(context.Background(), "reloaded by the engine")
	if err != nil {
		t.Fatal(err)
	}
	company = w.statusWritten()
	w.again(t, company)
	reconcileOnce(t, w)
	want := fmt.Sprintf("seeded with the rendered document as revision %s and owns the document since; it runs revision %s",
		seed, reloaded.RevisionID)
	if c := w.run.configApplied; c == nil || c.Reason != crewletaiv1alpha1.ReasonSeeded || !strings.Contains(c.Message, want) {
		t.Errorf("ConfigApplied is %+v, want Seeded, saying %q", c, want)
	}
	company = w.statusWritten()
	if config := company.Status.Config; config.RevisionID != seed || len(config.Unconfirmed) != 0 || len(config.Overtaken) != 0 {
		t.Errorf("status.config is %+v, want the seed %s recorded, and nothing unconfirmed or overtaken", config, seed)
	}
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{seed}) {
		t.Errorf("the operator wrote %v, want the seed alone", written)
	}

	// The engine owns the document since the seed: a write still recorded
	// as overtaken, as a status written before the seed was recorded may
	// hold, says nothing more, and goes.
	company.Status.Config.Overtaken = []crewletaiv1alpha1.UnconfirmedRevision{{
		RevisionID: "r9", AppliedHash: "sha256:other", WrittenAt: &metav1.Time{},
	}}
	w.again(t, company)
	reconcileOnce(t, w)
	if config := w.statusWritten().Status.Config; config.RevisionID != seed || len(config.Overtaken) != 0 {
		t.Errorf("status.config is %+v, want the seed %s recorded, and nothing overtaken", config, seed)
	}
}

// TestAWriteBuiltOnTheOneConfirmedStaysUnconfirmed: a write the operator
// built on r2 in the moment r2 landed, before any read showed it running, may
// land yet once r2 is confirmed: it stays unconfirmed, and is confirmed in
// turn once it runs. Taken for a write r2 overtook, it was adopted as a
// revision the operator had not written once it landed.
func TestAWriteBuiltOnTheOneConfirmedStaysUnconfirmed(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	w.engine.mu.Lock()
	w.engine.inert = append(w.engine.inert, storeRevision{id: w.engine.mint(), document: w.engine.active().document})
	w.engine.mu.Unlock()
	company.Status.Config.Unconfirmed = append(company.Status.Config.Unconfirmed, crewletaiv1alpha1.UnconfirmedRevision{
		RevisionID: "r3", BaseRevisionID: "r2", AppliedHash: company.Status.Config.Unconfirmed[0].AppliedHash,
		WrittenAt: &metav1.Time{},
	})
	w.again(t, company)
	reconcileOnce(t, w)
	company = w.statusWritten()
	if config := company.Status.Config; config.RevisionID != "r2" || len(config.Unconfirmed) != 1 ||
		config.Unconfirmed[0].RevisionID != "r3" || len(config.Overtaken) != 0 {
		t.Fatalf("status.config is %+v, want r2 recorded and r3, built on it, still unconfirmed", config)
	}
	w.engine.mu.Lock()
	w.engine.land("r3")
	w.engine.mu.Unlock()
	w.again(t, company)
	reconcileOnce(t, w)
	if config := w.statusWritten().Status.Config; config.RevisionID != "r3" || len(config.Unconfirmed) != 0 {
		t.Errorf("status.config is %+v, want r3 recorded as the operator's, and nothing unconfirmed", config)
	}
	if recorded := drained(w.events); !slices.ContainsFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r3 as epoch 3")
	}) {
		t.Errorf("events %q, want an Activated event saying the engine runs r3, the operator's write, as epoch 3", recorded)
	}
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
		t.Errorf("the operator wrote %v, want r2 alone", written)
	}
}

// TestAWriteAReactivationOvertookIsNotSentAgain: a held reconcile activates
// r1 again (r3), for the withdrawal of the registry, while a write built on it
// (r2) is unconfirmed. r2 can no longer land, and r3 activated after the
// changes r2's sync marked, so it read the values r2 was written with: the
// reload is recorded as the operator's, r2 is left for the next read to drop,
// and the next reconcile finds r3 in sync with the document and writes
// nothing.
func TestAWriteAReactivationOvertookIsNotSentAgain(t *testing.T) {
	w, company := unconfirmedWrite(t, pending)
	claims := company.Status.Config.Unconfirmed
	w.again(t, company)
	if _, err := w.run.withdrawHeld(context.Background(), w.resolve(t)); err != nil {
		t.Fatal(err)
	}
	company = w.statusWritten()
	if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r3"}) {
		t.Fatalf("the engine's revisions are %v, want r1 activated again as r3", ids)
	}
	if config := company.Status.Config; len(claims) != 1 || config.RevisionID != "r3" || len(config.Unconfirmed) != 1 {
		t.Fatalf("status.config is %+v, want r3 recorded and r2 left for the next read", config)
	}
	w.again(t, company)
	reconcileOnce(t, w)
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
		t.Errorf("the operator wrote %v, want r2 alone: r3 read the values r2 was written with", written)
	}
	config := w.statusWritten().Status.Config
	if config.RevisionID != "r3" || config.AppliedHash != claims[0].AppliedHash || len(config.Unconfirmed) != 0 ||
		len(config.Overtaken) != 0 {
		t.Errorf("status.config is %+v, want r3 with r2's hash, and r2 gone", config)
	}
}

// TestAConfirmationTheStateMissedIsSettledLater: the reconcile that found the
// write running recorded it in status.config and could not save the secret
// sync's state, so the next one reads what the state records of the write as
// the operator's, and settles the withdrawal its activation read the store
// without, rather than activating r2 again for it: whether that reconcile
// applies a document or holds it.
func TestAConfirmationTheStateMissedIsSettledLater(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "the next document is applied", true: "the next document is held"}[held], func(t *testing.T) {
			w, company := unconfirmedWrite(t, landed)
			w.again(t, company)
			w.stateWrites.Store(0)
			w.failWrite.Store(1)
			reconcileOnce(t, w)
			company = w.statusWritten()
			if company.Status.Config.RevisionID != "r2" || len(company.Status.Config.Unconfirmed) != 0 {
				t.Fatalf("status.config is %+v, want r2 recorded as the operator's", company.Status.Config)
			}
			if state := w.state(t); state.Records("r2") {
				t.Fatal("the state records r2's reading, which the failed write was to save")
			}
			w.failWrite.Store(0)
			w.again(t, company)
			if held {
				if _, err := w.run.withdrawHeld(context.Background(), w.resolve(t)); err != nil {
					t.Fatal(err)
				}
			} else {
				reconcileOnce(t, w)
			}
			if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2"}) {
				t.Errorf("the engine's revisions are %v, want r1 and r2: r2 activated without the registry", ids)
			}
			if state := w.state(t); len(state.Withdrawn()) != 0 || !state.Records("r2") {
				t.Errorf("the state owes %v and records %q, want nothing owed and r2's reading", state.Withdrawn(), state.Recorded())
			}
		})
	}
}

// TestAConfirmedWriteLeavesTheSeatsToTheRevisionThatRuns: the write a
// reconcile finds running is one an earlier reconcile made, of what it
// rendered then, so the human seats IdentityClaimed names are those of the
// revision the engine runs, here one with a seat ana, and not those of this
// reconcile's render, which the engine refuses. Taken for this reconcile's own
// write, a confirmation named seats the engine does not have.
func TestAConfirmedWriteLeavesTheSeatsToTheRevisionThatRuns(t *testing.T) {
	w, company := unconfirmedWrite(t, landed)
	w.engine.mu.Lock()
	w.engine.revisions[len(w.engine.revisions)-1].document = []byte("name: Acme\nroles:\n  - name: Ana\n    handle: ana\n    kind: human\n")
	w.engine.refusal = invalidDocument
	w.engine.mu.Unlock()
	w.again(t, company)
	reconcileOnce(t, w)
	if w.run.confirmed != "r2" {
		t.Fatalf("the reconcile confirmed %q, want r2", w.run.confirmed)
	}
	if got := w.run.humanSeats(context.Background()); !slices.Equal(got, []string{seatAna}) {
		t.Errorf("humanSeats() = %v, want [%s]: the engine runs r2", got, seatAna)
	}
}

// TestUnconfirmedActivationsAreBounded: a write whose answer is lost every
// time is recorded no more than maxUnconfirmed times, the newest kept, since
// the API server refuses a status that holds more: the generated CRD's
// maxItems, which this holds the constant to.
func TestUnconfirmedActivationsAreBounded(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "crewlet.ai_companies.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"unconfirmed", "overtaken"} {
		var bound *int64
		for _, v := range crd.Spec.Versions {
			if v.Name == "v1alpha1" {
				config := v.Schema.OpenAPIV3Schema.Properties["status"].Properties["config"]
				bound = config.Properties[field].MaxItems
			}
		}
		if bound == nil {
			t.Fatalf("the Company CRD does not bound status.config.%s; the operator does, at %d", field, maxUnconfirmed)
		}
		if *bound != maxUnconfirmed {
			t.Fatalf("the Company CRD bounds status.config.%s at %d items, the operator at %d", field, *bound, maxUnconfirmed)
		}
	}
	w, company := unconfirmedWrite(t, pending)
	for range maxUnconfirmed {
		w.again(t, company)
		w.engine.activation = pending
		reconcileOnce(t, w)
		company = w.statusWritten()
	}
	written, _ := w.engine.writes()
	got := make([]string, 0, len(company.Status.Config.Unconfirmed))
	for _, write := range company.Status.Config.Unconfirmed {
		got = append(got, write.RevisionID)
	}
	if len(written) != maxUnconfirmed+1 || !slices.Equal(got, written[1:]) {
		t.Errorf("after %d writes %v, status.config.unconfirmed holds %v; want the newest %d", len(written), written, got,
			maxUnconfirmed)
	}

	// Writes into an engine that ran no document move to overtaken once the
	// engine runs a revision none of them is, beside the ones already there,
	// and the oldest go: sixteen older ones here, which a list cut at its
	// other end would keep instead. The reconcile fails before it writes, as
	// the record is kept through one that does.
	company = w.run.company.DeepCopy()
	company.Status.Config.Unconfirmed, company.Status.Config.Overtaken = nil, nil
	want := make([]string, 0, maxUnconfirmed)
	for i := range maxUnconfirmed {
		company.Status.Config.Overtaken = append(company.Status.Config.Overtaken, crewletaiv1alpha1.UnconfirmedRevision{
			RevisionID: fmt.Sprintf("old%d", i), AppliedHash: "sha256:old", WrittenAt: &metav1.Time{},
		})
		seed := fmt.Sprintf("seed%d", i)
		company.Status.Config.Unconfirmed = append(company.Status.Config.Unconfirmed, crewletaiv1alpha1.UnconfirmedRevision{
			RevisionID: seed, AppliedHash: "sha256:seed", WrittenAt: &metav1.Time{},
		})
		want = append(want, seed)
	}
	w.again(t, company)
	w.stateWrites.Store(0)
	w.failWrite.Store(1)
	reconcileOnce(t, w)
	w.failWrite.Store(0)
	got = got[:0]
	for _, write := range w.statusWritten().Status.Config.Overtaken {
		got = append(got, write.RevisionID)
	}
	if config := w.statusWritten().Status.Config; !slices.Equal(got, want) || len(config.Unconfirmed) != 0 {
		t.Errorf("status.config.overtaken holds %v and unconfirmed %d writes; want the newest %d, %v, and none",
			got, len(config.Unconfirmed), maxUnconfirmed, want)
	}
}

// TestADriftCorrectionTheEngineCouldNotConfirmIsReportedOnceItRuns holds a
// write over a revision somebody else activated with other content, whose
// activation the engine could not confirm, to the report a confirmed one gets
// once a read shows the engine runs it: ConfigApplied DriftCorrected naming
// the revision it replaced and where it differed, and a Warning Drifted
// event, kept through the rest of that reconcile. Confirmed from the record
// as it was, it read Applied, then "The engine runs the rendered document",
// and nothing ever said a revision somebody else wrote had gone.
func TestADriftCorrectionTheEngineCouldNotConfirmIsReportedOnceItRuns(t *testing.T) {
	w := newWithdrawal(t, runningOther)
	operatorActivates(w)
	// The operator's last write was r0: r1 is somebody else's.
	company := w.run.company.DeepCopy()
	company.Status.Config.RevisionID = "r0"
	w.again(t, company)
	w.engine.activation = landed
	reconcileOnce(t, w)
	w.engine.activation = confirmed
	company = w.statusWritten()
	got := company.Status.Config.Unconfirmed
	if len(got) != 1 || got[0].RevisionID != "r2" || got[0].Replaced != crewletaiv1alpha1.ReplacedDrifted ||
		got[0].DriftedAt == "" {
		t.Fatalf("status.config.unconfirmed is %+v, want r2, recorded as replacing a Drifted r1, with where", got)
	}
	if recorded := drained(w.events); warned(recorded, crewletaiv1alpha1.ReasonDrifted) {
		t.Errorf("events %q, want no Drifted before the write is seen running: it may never land", recorded)
	}

	w.again(t, company)
	reconcileOnce(t, w)
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
		t.Fatalf("the operator wrote %v, want r2 alone", written)
	}
	want := fmt.Sprintf("Revision r1, which the operator did not write, differed from the rendered document at %s; "+
		"revision r2 replaced it.", got[0].DriftedAt)
	if c := w.run.configApplied; c == nil || c.Status != metav1.ConditionTrue ||
		c.Reason != crewletaiv1alpha1.ReasonDriftCorrected || c.Message != want {
		t.Errorf("ConfigApplied is %+v, want True DriftCorrected, %q", c, want)
	}
	recorded := drained(w.events)
	i := slices.IndexFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Warning "+crewletaiv1alpha1.ReasonDrifted+" ")
	})
	if i < 0 || !strings.Contains(recorded[i], "ran revision r1, which the operator did not write") ||
		!strings.Contains(recorded[i], "revision r2 replaced it") {
		t.Errorf("events %q, want a Warning Drifted saying r2 replaced r1", recorded)
	}
}

// TestAHeldReconcileReleasesWhatARunningUnconfirmedWriteLetsGo: r1 requires
// the registry, and the operator's write r2, which lets it go, landed with its
// answer lost. Then the registry's Secret is deleted while the document is
// held by a render that no longer reads it at all, so the held reconcile reads
// nothing missing and owes nothing for r1. It reads the engine all the same
// while a write of the operator's may run: r2 runs, and the reading the state
// recorded before r2 was sent says it lets the registry go, so the value is
// taken out of the store and r2 activated again (r3) for it. Returning before
// the read, it left the deleted Secret's value for r2's tool server to start
// with until the document was applied. The record of r2 moves on to r3, which
// the next reconcile confirms as the operator's, rather than taking r3 for a
// revision somebody else activated and reporting the write over it as drift.
func TestAHeldReconcileReleasesWhatARunningUnconfirmedWriteLetsGo(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningRequires)
	operatorActivates(w)
	if err := w.cluster.Create(ctx, valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})); err != nil {
		t.Fatal(err)
	}
	w.engine.activation = landed
	reconcileOnce(t, w)
	w.engine.activation = confirmed
	company := w.statusWritten()
	claims := company.Status.Config.Unconfirmed
	if len(claims) != 1 || claims[0].RevisionID != "r2" || !w.engine.holds(w.registry) || len(w.state(t).Retained()) != 0 {
		t.Fatalf("status.config.unconfirmed is %+v, the store holds the registry: %t, and the state retains %v; want "+
			"r2 recorded, the registry pushed, and nothing retained", claims, w.engine.holds(w.registry),
			w.state(t).Retained())
	}
	if err := w.cluster.Delete(ctx, valueSecret(optionalSecret, nil)); err != nil {
		t.Fatal(err)
	}
	w.again(t, company)
	if _, err := w.run.withdrawHeld(ctx, secretsync.Desired{}); err != nil {
		t.Fatal(err)
	}
	if w.engine.holds(w.registry) {
		t.Error("the engine's store still holds the registry, whose Secret is gone and which r2, which runs, lets go")
	}
	if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2", "r3"}) {
		t.Errorf("the engine's revisions are %v, want r2 activated again as r3 without the registry", ids)
	}
	state := w.state(t)
	if !state.Records("r3") || !slices.Equal(state.LetGo("r3"), []string{w.registry}) || len(state.Withdrawn()) != 0 {
		t.Errorf("the state records %q letting %v go and owes %v; want r3 letting the registry go and nothing owed",
			state.Recorded(), state.LetGo(state.Recorded()), state.Withdrawn())
	}
	company = w.statusWritten()
	if config := company.Status.Config; config.RevisionID != "r1" || len(config.Unconfirmed) != 1 ||
		config.Unconfirmed[0].RevisionID != "r3" || config.Unconfirmed[0].AppliedHash != claims[0].AppliedHash {
		t.Fatalf("status.config is %+v, want r1 recorded and r2's record moved on to r3", config)
	}
	drained(w.events)
	w.again(t, company)
	reconcileOnce(t, w)
	recorded := drained(w.events)
	if warned(recorded, crewletaiv1alpha1.ReasonDrifted) || !slices.ContainsFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r3 as epoch 3")
	}) {
		t.Errorf("events %q, want r3 confirmed as the operator's, at epoch 3, and no drift reported", recorded)
	}
}

// TestAReloadTheEngineCouldNotConfirmIsTheOperatorsOnceItRuns: a held
// reconcile takes the registry, which r1, the operator's revision, lets go,
// out of the store and activates r1 again; the engine stores the reload as r2
// and loses the answer to its activation. Where r2 runs by the read back, it
// activated after the withdrawal, which is settled, and it is recorded as the
// reactivation. Where it has not landed yet it may still: the withdrawal stays
// owed and reported, and r2 is recorded as a write of r1's document, so that
// once it lands the next reconcile records it as the operator's, at its epoch,
// and owes nothing for the withdrawal, rather than taking it for a revision
// somebody else activated.
func TestAReloadTheEngineCouldNotConfirmIsTheOperatorsOnceItRuns(t *testing.T) {
	for _, how := range []outcome{landed, pending} {
		t.Run(map[outcome]string{landed: "it landed", pending: "it lands later"}[how], func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			operatorActivates(w)
			company := w.run.company.DeepCopy()
			company.Status.Config.AppliedHash = r1Hash
			w.again(t, company)
			w.engine.reloadActivation = how
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			w.engine.reloadActivation = confirmed
			recorded := drained(w.events)
			state := w.state(t)
			company = w.statusWritten()
			if how == landed {
				if config := company.Status.Config; len(state.Withdrawn()) != 0 || config.RevisionID != "r2" ||
					len(config.Unconfirmed) != 0 || !told(recorded, "Reactivated") || warned(recorded, "SecretWithdrawalFailed") {
					t.Errorf("the state owes %v, status.config is %+v, events %q; want the withdrawal settled, r2 recorded "+
						"as the reactivation, and no failure reported", state.Withdrawn(), config, recorded)
				}
				return
			}
			config := company.Status.Config
			if !slices.Equal(state.Withdrawn(), []string{w.registry}) || !warned(recorded, "SecretWithdrawalFailed") ||
				config.RevisionID != "r1" || len(config.Unconfirmed) != 1 || config.Unconfirmed[0].RevisionID != "r2" ||
				config.Unconfirmed[0].BaseRevisionID != "r1" || config.Unconfirmed[0].AppliedHash != r1Hash {
				t.Fatalf("the state owes %v, status.config is %+v, events %q; want the registry owed, the failure reported, "+
					"and r2 recorded unconfirmed on r1 with r1's hash", state.Withdrawn(), config, recorded)
			}
			w.engine.mu.Lock()
			w.engine.land("r2")
			w.engine.mu.Unlock()
			w.again(t, company)
			reconcileOnce(t, w)
			recorded = drained(w.events)
			if !slices.ContainsFunc(recorded, func(e string) bool {
				return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r2 as epoch 2")
			}) {
				t.Errorf("events %q, want r2 confirmed as the operator's, at epoch 2", recorded)
			}
			if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2"}) {
				t.Errorf("the engine's revisions are %v, want r1 and r2: r2 read the store without the registry", ids)
			}
			if state := w.state(t); len(state.Withdrawn()) != 0 || !slices.Equal(state.LetGo("r2"), []string{w.registry}) {
				t.Errorf("the state owes %v and lets %v go for r2; want nothing owed and the registry let go",
					state.Withdrawn(), state.LetGo("r2"))
			}
		})
	}
}

// TestAWriteConfirmedLateIsHeldToTheDeadlineFromWhenItWasFound: the write's
// answer was lost three minutes before the reconcile that finds it running,
// and the engine is one epoch behind. Nothing says when the activation landed,
// so the engine is held to applying it from when the operator found it, as an
// adopted revision is: Applying, not an apply that failed. Dated by the write,
// the confirmation read as ApplyFailed, with a Warning, in the reconcile that
// found it.
func TestAWriteConfirmedLateIsHeldToTheDeadlineFromWhenItWasFound(t *testing.T) {
	ctx := context.Background()
	w, company := unconfirmedWrite(t, landed)
	written := metav1.NewTime(time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC))
	company.Status.Config.Unconfirmed[0].WrittenAt = &written
	found := written.Add(3 * time.Minute)
	w.again(t, company)
	w.run.now = func() time.Time { return found }
	w.engine.mu.Lock()
	w.engine.appliedLag = 1
	w.engine.mu.Unlock()
	reconcileOnce(t, w)
	if config := w.run.config; config == nil || config.RevisionID != "r2" || config.AppliedAt == nil ||
		!config.AppliedAt.Time.Equal(found) {
		t.Fatalf("status.config is %+v, want r2 confirmed, applied when the operator found it, %s", w.run.config, found)
	}
	h, err := w.client.Health(ctx)
	w.run.verifyApplied(ctx, w.client, h, err, ctrl.Result{})
	if c := w.run.configApplied; c == nil || c.Reason != crewletaiv1alpha1.ReasonApplying {
		t.Errorf("ConfigApplied is %+v, want Applying: the operator found r2 a moment ago", c)
	}
	if recorded := drained(w.events); warned(recorded, crewletaiv1alpha1.ReasonApplyFailed) {
		t.Errorf("events %q, want no ApplyFailed", recorded)
	}
}

// TestAWriteRecordsItsDocumentsReadingBeforeItIsSent: the state owes nothing,
// so no change of the store is marked before the write, and the reading of the
// document the write carries is saved before it is sent all the same, under
// the write's hash: its answer lost, nothing else says how the revision the
// engine stored reads the store, once the render has moved on.
func TestAWriteRecordsItsDocumentsReadingBeforeItIsSent(t *testing.T) {
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	reconcileOnce(t, w)
	company := w.statusWritten()
	if state := w.state(t); state.Unobserved() || !state.Records("r2") || company.Status.Config.RevisionID != "r2" {
		t.Fatalf("after the first write the state records %q, with changes unread: %t; want r2, and none",
			state.Recorded(), state.Unobserved())
	}
	w.engine.mu.Lock()
	w.engine.activateOther(w.requiring)
	w.engine.mu.Unlock()
	w.again(t, company)
	w.engine.activation = landed
	reconcileOnce(t, w)
	got := w.statusWritten().Status.Config.Unconfirmed
	if len(got) != 1 || got[0].RevisionID != "r4" {
		t.Fatalf("status.config.unconfirmed is %+v, want r4, written over the r3 somebody activated", got)
	}
	if !w.state(t).Pends(got[0].AppliedHash) {
		t.Errorf("the state records no reading under r4's hash, %s: saved after the write, its answer lost left none",
			got[0].AppliedHash)
	}
}

// TestAWriteWhoseAnswerIsLostWhileTheFleetIsUnreadableSettlesWhatItRead: the
// fleet's activation pointer cannot be read after the changes the write is
// sent after, so nothing marks the rotated value and the withdrawn registry,
// and the engine loses the write's answer. The changes await the revision it
// stored instead, and the next reconcile, finding that revision running,
// settles them: no second write of the same document, and no reload of it.
// Left unread, no epoch the pointer named later could settle them, and the
// operator wrote the document again over its own landed write. The fleet
// answers the read that says what every pod runs, before the changes: one it
// does not answer keeps every value, so the registry would stay.
func TestAWriteWhoseAnswerIsLostWhileTheFleetIsUnreadableSettlesWhatItRead(t *testing.T) {
	for _, down := range []bool{true, false} {
		t.Run(map[bool]string{true: "the fleet answers 503", false: "the fleet answers without its activation"}[down], func(t *testing.T) {
			w := newWithdrawal(t, runningLetsGo)
			operatorActivates(w)
			w.rotate(t)
			if down {
				w.engine.beforeFleet = func(e *storeEngine) {
					e.beforeFleet = func(e *storeEngine) { e.fleetDown = true }
				}
			}
			w.engine.noActivation = !down
			w.engine.activation = landed
			reconcileOnce(t, w)
			if state := w.state(t); !state.Unobserved() {
				t.Fatalf("the state owes %v and %v, all marked; want them unread, as the fleet could not be read",
					state.Withdrawn(), state.Written())
			}
			w.engine.fleetDown, w.engine.noActivation, w.engine.activation = false, false, confirmed
			w.again(t, w.statusWritten())
			reconcileOnce(t, w)
			if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
				t.Errorf("the operator wrote %v, want r2 alone: r2 ran, and read every change", written)
			}
			if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2"}) {
				t.Errorf("the engine's revisions are %v, want r1 and r2: nothing activated r2 again", ids)
			}
			if state := w.state(t); len(state.Withdrawn()) != 0 || len(state.Written()) != 0 {
				t.Errorf("the state owes %v and %v, want nothing", state.Withdrawn(), state.Written())
			}
		})
	}
}

// TestAFirstWriteWhoseAnswerIsLostIsNotWrittenAgain: under Enforce the engine
// runs no document, so the fleet names no activation to mark the values the
// sync pushed by, and the engine loses the answer to the first write. Once a
// read finds the revision it stored running, that revision read every value,
// and the operator writes nothing more. Marked by nothing, the values stayed
// owed, and the operator wrote the same document a second time.
func TestAFirstWriteWhoseAnswerIsLostIsNotWrittenAgain(t *testing.T) {
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	w.engine.mu.Lock()
	w.engine.revisions = nil
	w.engine.mu.Unlock()
	company := w.run.company.DeepCopy()
	company.Status.Config = crewletaiv1alpha1.ConfigStatus{}
	w.again(t, company)
	w.rotate(t)
	w.engine.activation = landed
	reconcileOnce(t, w)
	written, _ := w.engine.writes()
	if len(written) != 1 {
		t.Fatalf("the operator wrote %v, want the first write", written)
	}
	w.engine.activation = confirmed
	w.again(t, w.statusWritten())
	reconcileOnce(t, w)
	if again, _ := w.engine.writes(); !slices.Equal(again, written) {
		t.Errorf("the operator wrote %v, want %v alone", again, written)
	}
	if state := w.state(t); len(state.Written()) != 0 {
		t.Errorf("the state owes %v, want nothing: %s read every value", state.Written(), written[0])
	}
}

// TestAReloadWhoseAnswerIsLostIsTheOperatorsWhenItsReadBackFails: a held
// reconcile activates r1, the operator's, again for the registry it withdrew,
// the engine stores the reload as r2 and loses the answer, and the read back
// fails: GET /config, or the fleet with r2 running. r2 is recorded as the
// operator's from the answer, so once it runs the next reconcile confirms it
// as the operator's, at its epoch, and settles the withdrawal, which awaits
// r2 where no read of the fleet marked it before the reload. Recorded only
// where the read back found r1 still running, it was recorded nowhere, and r2
// was taken for a revision somebody else activated; and a withdrawal no read
// marked had r2 activated again for it. The fleet answers the read that says
// what every pod runs, before the withdrawal: one it does not answer keeps
// every value, and nothing is withdrawn to reload for.
func TestAReloadWhoseAnswerIsLostIsTheOperatorsWhenItsReadBackFails(t *testing.T) {
	for _, c := range []struct {
		name string
		how  outcome
		fail func(e *storeEngine)
		// fleetLost makes the fleet unreadable after the read that says
		// what every pod runs, so no read marks the withdrawal before the
		// reload is asked for.
		fleetLost bool
	}{
		{name: "GET /config fails, the reload landed", how: landed, fail: func(e *storeEngine) { e.configDown = true }},
		{name: "GET /config fails, the reload lands later", how: pending, fail: func(e *storeEngine) { e.configDown = true }},
		{name: "the fleet cannot be read, the reload landed", how: landed, fail: func(e *storeEngine) { e.fleetDown = true }},
		{
			name: "the fleet cannot be read across the reload, which landed", how: landed,
			fail: func(e *storeEngine) { e.configDown = true }, fleetLost: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			operatorActivates(w)
			company := w.run.company.DeepCopy()
			company.Status.Config.AppliedHash = r1Hash
			w.again(t, company)
			w.engine.reloadActivation = c.how
			w.engine.beforeReload = c.fail
			if c.fleetLost {
				w.engine.beforeFleet = func(e *storeEngine) {
					e.beforeFleet = func(e *storeEngine) { e.fleetDown = true }
				}
			}
			if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
				t.Fatal(err)
			}
			w.engine.mu.Lock()
			w.engine.configDown, w.engine.fleetDown, w.engine.reloadActivation = false, false, confirmed
			if c.how == pending {
				w.engine.land("r2")
			}
			r1 := w.engine.revisions[0].document
			w.engine.mu.Unlock()
			company = w.statusWritten()
			got := company.Status.Config.Unconfirmed
			if len(got) != 1 || got[0].RevisionID != "r2" || got[0].BaseRevisionID != "r1" ||
				got[0].DocumentSHA256 != documentDigest(r1) {
				t.Fatalf("status.config.unconfirmed is %+v, want r2, a reload of r1, with r1's document's digest", got)
			}
			drained(w.events)
			w.again(t, company)
			reconcileOnce(t, w)
			recorded := drained(w.events)
			if !slices.ContainsFunc(recorded, func(e string) bool {
				return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r2 as epoch 2")
			}) || warned(recorded, crewletaiv1alpha1.ReasonDrifted) {
				t.Errorf("events %q, want r2 confirmed as the operator's, at epoch 2, and no drift reported", recorded)
			}
			if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2"}) {
				t.Errorf("the engine's revisions are %v, want r1 and r2: r2 read the store without the registry", ids)
			}
			if state := w.state(t); len(state.Withdrawn()) != 0 {
				t.Errorf("the state owes %v, want the withdrawal settled by r2", state.Withdrawn())
			}
		})
	}
}

// TestAReloadOfAnotherRevisionIsNotTheOperators: the operator asks to activate
// r1 again, and somebody activates r2, another document, before the request
// lands, so the engine stores r3 as a reload of r2, loses the answer, and the
// read back fails. r3 is recorded as a reload of r1, with r1's document's
// digest; found running r2's document, it is not the operator's: nothing
// confirms it as the operator's write, and the next write over it reports it
// as the drift it is.
func TestAReloadOfAnotherRevisionIsNotTheOperators(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	operatorActivates(w)
	company := w.run.company.DeepCopy()
	company.Status.Config.AppliedHash = r1Hash
	w.again(t, company)
	w.engine.reloadActivation = landed
	w.engine.beforeReload = func(e *storeEngine) {
		e.activateOther(w.requiring)
		e.configDown = true
	}
	if _, err := w.run.withdrawHeld(ctx, w.resolve(t)); err != nil {
		t.Fatal(err)
	}
	w.engine.mu.Lock()
	w.engine.configDown, w.engine.reloadActivation = false, confirmed
	w.engine.mu.Unlock()
	company = w.statusWritten()
	if got := company.Status.Config.Unconfirmed; len(got) != 1 || got[0].RevisionID != "r3" {
		t.Fatalf("status.config.unconfirmed is %+v, want r3 recorded, as nothing said what it reloaded", got)
	}
	drained(w.events)
	w.again(t, company)
	reconcileOnce(t, w)
	recorded := drained(w.events)
	if slices.ContainsFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Normal Activated ") && strings.Contains(e, "revision r3")
	}) {
		t.Errorf("events %q, want r3 not confirmed as the operator's: it runs r2's document", recorded)
	}
	if c := w.run.configApplied; c == nil || c.Reason != crewletaiv1alpha1.ReasonDriftCorrected ||
		!strings.HasPrefix(c.Message, "Revision r3, which the operator did not write") {
		t.Errorf("ConfigApplied is %+v, want DriftCorrected over r3", c)
	}
	if config := w.statusWritten().Status.Config; len(config.Unconfirmed) != 0 || config.RevisionID != "r4" {
		t.Errorf("status.config is %+v, want r4 recorded and nothing unconfirmed", config)
	}
}

// TestAReloadThatLandsWhileTheNextOneFailsIsRecordedOnce: a held reconcile
// finds r2, a write of the operator's whose activation went unconfirmed,
// running, and its reload of r2 is stored as r3 and left unconfirmed too. In
// the next held reconcile r3 lands while that reconcile's own reload of r2
// fails, so the read back finds r3 running r2's document and records it as
// the reactivation. r3 is recorded once: as two entries of one revision, the
// API server refused the status, and every condition the reconcile decided
// was lost.
func TestAReloadThatLandsWhileTheNextOneFailsIsRecordedOnce(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningRequires)
	operatorActivates(w)
	if err := w.cluster.Create(ctx, valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})); err != nil {
		t.Fatal(err)
	}
	w.engine.activation = landed
	reconcileOnce(t, w)
	w.engine.activation = confirmed
	company := w.statusWritten()
	if err := w.cluster.Delete(ctx, valueSecret(optionalSecret, nil)); err != nil {
		t.Fatal(err)
	}
	w.again(t, company)
	w.engine.reloadActivation = pending
	if _, err := w.run.withdrawHeld(ctx, secretsync.Desired{}); err != nil {
		t.Fatal(err)
	}
	company = w.statusWritten()
	if config := company.Status.Config; len(config.Unconfirmed) != 2 || config.Unconfirmed[1].RevisionID != "r3" {
		t.Fatalf("status.config is %+v, want r2 unconfirmed and the reload r3 recorded on it", config)
	}
	w.again(t, company)
	w.engine.reloadActivation = confirmed
	w.engine.reloadDown = true
	w.engine.beforeReload = func(e *storeEngine) { e.land("r3") }
	if _, err := w.run.withdrawHeld(ctx, secretsync.Desired{}); err != nil {
		t.Fatal(err)
	}
	config := w.statusWritten().Status.Config
	ids := make([]string, len(config.Unconfirmed))
	for i := range config.Unconfirmed {
		ids[i] = config.Unconfirmed[i].RevisionID
	}
	if slices.Contains(ids, "r2") || !slices.Equal(slices.Compact(slices.Sorted(slices.Values(ids))), slices.Sorted(slices.Values(ids))) ||
		!slices.Contains(ids, "r3") {
		t.Errorf("status.config.unconfirmed names %v, want r3 once, as the reactivation of r2's record", ids)
	}
}

// TestAReloadOfAnUnconfirmedReplacementReportsWhatTheWriteReplaced: the
// operator's write r2 replaced r1, somebody else's document, and its answer
// was lost; a held reconcile then found r2 running and its reload of r2 was
// stored as r3, its answer lost too. Once r3 runs, its confirmation reports
// that the rendered document replaced r1, once, and never names r2, the
// operator's own write, as somebody else's: whether r2 was confirmed first or
// not at all.
func TestAReloadOfAnUnconfirmedReplacementReportsWhatTheWriteReplaced(t *testing.T) {
	for _, c := range []struct {
		name string
		// confirmFirst confirms r2 in a reconcile of its own before r3 lands.
		confirmFirst bool
	}{
		{name: "r3 lands first"},
		{name: "r2 confirmed first", confirmFirst: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningOther)
			operatorActivates(w)
			company := w.run.company.DeepCopy()
			company.Status.Config.RevisionID = "r0"
			w.again(t, company)
			if err := w.cluster.Create(ctx, valueSecret(optionalSecret, map[string]string{optionalKey: npmRegistry})); err != nil {
				t.Fatal(err)
			}
			w.engine.activation = landed
			reconcileOnce(t, w)
			w.engine.activation = confirmed
			company = w.statusWritten()
			if got := company.Status.Config.Unconfirmed; len(got) != 1 || got[0].RevisionID != "r2" || got[0].Replaced == "" {
				t.Fatalf("status.config.unconfirmed is %+v, want r2 recorded as replacing r1", got)
			}
			if err := w.cluster.Delete(ctx, valueSecret(optionalSecret, nil)); err != nil {
				t.Fatal(err)
			}
			w.again(t, company)
			w.engine.reloadActivation = pending
			if _, err := w.run.withdrawHeld(ctx, secretsync.Desired{}); err != nil {
				t.Fatal(err)
			}
			w.engine.reloadActivation = confirmed
			company = w.statusWritten()
			if got := company.Status.Config.Unconfirmed; len(got) != 2 || got[1].RevisionID != "r3" || got[1].Replaced != "" ||
				got[1].DriftedAt != "" {
				t.Fatalf("status.config.unconfirmed is %+v, want r3, built on r2, replacing nothing of its own", got)
			}
			var reports []string
			if c.confirmFirst {
				drained(w.events)
				w.again(t, company)
				reconcileOnce(t, w)
				reports = append(reports, drained(w.events)...)
				company = w.statusWritten()
			}
			w.engine.mu.Lock()
			w.engine.land("r3")
			w.engine.mu.Unlock()
			drained(w.events)
			w.again(t, company)
			reconcileOnce(t, w)
			reports = append(reports, drained(w.events)...)
			reason := crewletaiv1alpha1.ReasonDrifted
			var warnings []string
			for _, e := range reports {
				if strings.HasPrefix(e, "Warning "+reason+" ") {
					warnings = append(warnings, e)
				}
			}
			// What the event says of the revision replaced: "revision r1,
			// which the operator did not write".
			names := func(revision string) bool {
				return strings.Contains(warnings[0], "revision "+revision+", which")
			}
			if len(warnings) != 1 || !names("r1") || names("r2") {
				t.Errorf("Warning %s events %q, want one, naming r1 and never r2", reason, warnings)
			}
			if config := w.statusWritten().Status.Config; config.RevisionID != "r3" || len(config.Unconfirmed) != 0 {
				t.Errorf("status.config is %+v, want r3 recorded as the operator's and nothing unconfirmed", config)
			}
		})
	}
}

// TestAConfirmedWriteLeavesNothingOvertaken: a create the engine could not
// confirm was overtaken, and a later write of the operator's is confirmed
// running, by a reconcile that then ends before anything is in sync: a value
// rotated since needs a write (the engine's own re-activation at it failing),
// and the engine cannot decide the dry run.
// Nothing overtaken can land now, nor be the seed, so the record of it goes
// with the confirmation, as the API's own doc says it does once
// status.config records a write; kept, it stood beside that write until a
// later reconcile found the document in sync.
func TestAConfirmedWriteLeavesNothingOvertaken(t *testing.T) {
	ctx := context.Background()
	w, company := unconfirmedWrite(t, landed)
	now := metav1.NewTime(time.Unix(1, 0))
	company.Status.Config.Overtaken = []crewletaiv1alpha1.UnconfirmedRevision{
		{RevisionID: "r0", AppliedHash: "sha256:r0", WrittenAt: &now},
	}
	w.again(t, company)
	rotated := &corev1.Secret{}
	if err := w.cluster.Get(ctx, types.NamespacedName{Namespace: "ns", Name: requiredSecret}, rotated); err != nil {
		t.Fatal(err)
	}
	rotated.Data[requiredKey] = []byte(rotatedValue + "-again")
	if err := w.cluster.Update(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	operatorActivates(w)
	w.engine.dryRunDown = true
	reconcileOnce(t, w)
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
		t.Fatalf("the operator wrote %v, want r2 alone: the dry run after the confirmation was not decided", written)
	}
	if config := w.statusWritten().Status.Config; config.RevisionID != "r2" || len(config.Overtaken) != 0 {
		t.Errorf("status.config is %+v, want r2 recorded and nothing overtaken", config)
	}
}
