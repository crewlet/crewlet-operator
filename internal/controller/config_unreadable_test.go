package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// These tests hold what the operator does with an engine that cannot open the
// fleet's revision of the company document: the engine answers every read of
// the document 409 with no Retry-After, and every write built from it too, and
// no wait clears it. A revision sealed in a way the engine cannot open (under a
// key its keyring lacks, or in a form its build does not read) is somebody's
// document, which no write replaces: it is reported as what blocks the
// document, in the engine's words and with the remedy, and looked at again at
// the resync interval, not ten seconds on with a promise of a retry that
// changes nothing. A revision stored without a seal is the one a whole document
// replaces, reading nothing from it: under Enforce the operator replaces it
// with the rendered document, and under Seed and Observe, which write no such
// thing, it blocks the document as the others do.

// keyringLost, sealMissing and unknownSeal are the engine's answers to a read
// of revision, sealed under a key its keyring no longer holds, stored without a
// seal, or sealed in a form its build does not read, as crewlet's configapi
// words them (RefuseUnreadable, UnreadableRevisionError) and its
// httpjson.Envelope sends them, the detail and the hint each ended as a
// sentence.
func keyringLost(revision string) string {
	return fmt.Sprintf(`{"error":"unreadable_revision","revision_id":%q,`+
		`"message":"That revision is sealed under a key this deployment no longer holds, so it cannot be read. `+
		`Put the key back in the node's keyring first.",`+
		`"detail":"This node's keyring doesn't hold the key that revision is sealed under.",`+
		`"hint":"It is sealed under a key that is no longer in the keyring; restore that key to the node's `+
		`secrets.keys first."}`, revision)
}

func sealMissing(revision string) string {
	return fmt.Sprintf(`{"error":"unsealed_revision","revision_id":%q,`+
		`"message":"That revision is stored without a seal, which a build older than the mandatory keyring wrote, `+
		`and a node reads only sealed revisions because the seal is what authenticates one. The hint says what `+
		`brings its document back.",`+
		`"detail":"This node's copy of that revision is stored without a seal, so nothing vouches for who wrote it.",`+
		`"hint":"It is stored without a seal, so nothing vouches for who wrote it and it is neither shown, edited nor `+
		`reverted to; to replace it, an admin sends the whole document from your own copy with PUT /config, or `+
		`imports it with `+"`crewlet config import`"+`, either of which stores it sealed."}`, revision)
}

func unknownSeal(revision string) string {
	return fmt.Sprintf(`{"error":"unknown_seal_revision","revision_id":%q,`+
		`"message":"That revision is sealed in a form this build of Crewlet doesn't read, most likely by a newer `+
		`build, so this node can't read or replace it. The hint says what fixes it.",`+
		`"detail":"This node's copy of that revision is sealed in a form this build of Crewlet doesn't read.",`+
		`"hint":"A newer build of Crewlet most likely sealed it here, and no write here replaces it, since that `+
		`would discard it unread; run that build on this node again."}`, revision)
}

// fleetKeyMissing, fleetSealMissing and fleetUnknownSeal are the engine's
// answers to a read of revision met through the fleet's activation pointer,
// sealed under a key this node's keyring was never given, carrying no seal, or
// sealed in a form this node's build does not read, worded and ended as the
// three above. A seal the build does not read is one code for either origin.
func fleetKeyMissing(revision string) string {
	return fmt.Sprintf(`{"error":"unreadable_fleet_revision","revision_id":%q,`+
		`"message":"This node doesn't hold the key the fleet sealed its revision under, so it can't read it. `+
		`The hint says what fixes it.",`+
		`"detail":"This node doesn't hold the key the fleet sealed this revision under.",`+
		`"hint":"Add the key the fleet seals under to this node's secrets.keys and restart it."}`, revision)
}

func fleetSealMissing(revision string) string {
	return fmt.Sprintf(`{"error":"unsealed_fleet_revision","revision_id":%q,`+
		`"message":"The fleet's revision arrived without a seal, so this node won't read it. The hint says `+
		`what to do.",`+
		`"detail":"The fleet's revision arrived without a seal, so this node won't read it.",`+
		`"hint":"Every node seals what it activates, so this body came from outside the fleet and is neither shown `+
		`nor edited; to replace it, an admin sends the whole document with PUT /config, or imports it with `+
		"`crewlet config import`"+`, either of which stores it sealed and activates it."}`, revision)
}

func fleetUnknownSeal(revision string) string {
	return fmt.Sprintf(`{"error":"unknown_seal_revision","revision_id":%q,`+
		`"message":"That revision is sealed in a form this build of Crewlet doesn't read, most likely by a newer `+
		`build, so this node can't read or replace it. The hint says what fixes it.",`+
		`"detail":"The fleet's revision is sealed in a form this build of Crewlet doesn't read.",`+
		`"hint":"A newer build of Crewlet most likely sealed it, and no write here replaces it, since that would `+
		`discard it unread; upgrade this node to the build the rest of the fleet runs."}`, revision)
}

// cannotOpen makes the fleet's revision one the engine cannot open, answering
// every request that reads it with answer of its id.
func (e *storeEngine) cannotOpen(answer func(revision string) string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revisions[len(e.revisions)-1].unopened = answer
}

// readsActive reports whether r is a request the engine answers by reading
// active, which it cannot open, and so refuses as it refuses a read of it: a
// read of the document; a reload, which activates it again; and a write
// (a dry run included) over any but one stored without a seal, which a whole
// document replaces, reading nothing from it.
func readsActive(r *http.Request, active storeRevision) bool {
	switch {
	case r.URL.Path == configRoute && r.Method == http.MethodGet:
		return true
	case r.URL.Path == configRoute && r.Method == http.MethodPut:
		return !replaceable(active)
	}
	return r.URL.Path == reloadRoute && r.Method == http.MethodPost
}

// replaceable reports whether a write replaces revision, which the engine
// cannot open, as crewlet's configapi decides it (Service.prepare): a whole
// document goes on over a revision stored without a seal, and over no other.
func replaceable(revision storeRevision) bool {
	var answer struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(revision.unopened(revision.id)), &answer); err != nil {
		panic(err)
	}
	return answer.Error == "unsealed_revision" || answer.Error == "unsealed_fleet_revision"
}

// What the RevisionUnreadable message says, as more than one case expects
// it: what the keyring does not do and which Secret it is, what the engine
// does not do with a revision without a seal, and what replaces one under a
// policy that does not.
const (
	saysKeyring       = "keyring does not open"
	saysKeyringSecret = "Secret acme-keyring"
	saysUnsealed      = "which it stores without a seal"
	saysEnforce       = "set to Enforce"
)

// withPolicy makes w's reconcile one of its Company under policy.
func withPolicy(t *testing.T, w *withdrawal, policy crewletaiv1alpha1.ManagementPolicy) {
	t.Helper()
	company := w.run.company.DeepCopy()
	company.Spec.Config.ManagementPolicy = policy
	w.again(t, company)
}

// TestARevisionTheEngineCannotOpenBlocksTheDocument holds an engine that cannot
// open the fleet's revision, where no write of the operator's may replace it,
// to ConfigApplied=False RevisionUnreadable: the message names the revision,
// quotes the engine's account and its remedy, and says what restores it, or
// replaces it, in the Company's terms; the operator writes nothing, neither
// the document nor the store; and the next look is at the resync interval.
// Reported as an engine that is unavailable, it promised a retry in ten
// seconds that no retry could keep. A revision sealed in a way the engine
// cannot open blocks under every policy, Enforce included, and one stored
// without a seal under Seed and Observe, which write no document over the
// fleet's revision.
func TestARevisionTheEngineCannotOpenBlocksTheDocument(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(revision string) string
		policy crewletaiv1alpha1.ManagementPolicy
		says   []string
	}{
		{
			name: "a key the keyring lost", answer: keyringLost, policy: crewletaiv1alpha1.ManagementPolicyEnforce,
			says: []string{saysKeyring, "doesn't hold the key that revision is sealed under",
				"restore that key to the node's secrets.keys first", saysKeyringSecret},
		},
		{
			name: "the fleet's revision under a key this node lacks", answer: fleetKeyMissing,
			policy: crewletaiv1alpha1.ManagementPolicyEnforce,
			says: []string{saysKeyring, "doesn't hold the key the fleet sealed this revision under",
				"Add the key the fleet seals under", saysKeyringSecret, "takes it when it restarts"},
		},
		{
			name: "a seal this node's build does not read", answer: unknownSeal,
			policy: crewletaiv1alpha1.ManagementPolicyEnforce,
			says: []string{"does not read the seal of revision r1", "sealed in a form this build of Crewlet doesn't read",
				"run that build on this node again", "newer than the one the data pod runs",
				"keyring Secret acme-keyring is not at fault", "no write replaces it",
				"the engine image of EngineClass standard", "takes it when it restarts"},
		},
		{
			name: "the fleet's revision under a seal this node's build does not read", answer: fleetUnknownSeal,
			policy: crewletaiv1alpha1.ManagementPolicyEnforce,
			says: []string{"does not read the seal of revision r1", "The fleet's revision is sealed in a form",
				"upgrade this node to the build the rest of the fleet runs", "keyring Secret acme-keyring is not at fault",
				"the engine image of EngineClass standard"},
		},
		{
			name: "a key the keyring lost, under Seed", answer: keyringLost, policy: crewletaiv1alpha1.ManagementPolicySeed,
			says: []string{saysKeyring, saysKeyringSecret},
		},
		{
			name: "a revision stored without a seal, under Seed", answer: sealMissing,
			policy: crewletaiv1alpha1.ManagementPolicySeed,
			says: []string{saysUnsealed, "stored without a seal, so nothing vouches",
				"an admin sends the whole document from your own copy with PUT /config",
				"stored by an engine older than the keyring", "Under the Company's Seed policy",
				"writes only into an engine with no document", saysEnforce},
		},
		{
			name: "a revision stored without a seal, under Observe", answer: sealMissing,
			policy: crewletaiv1alpha1.ManagementPolicyObserve,
			says: []string{saysUnsealed, "an admin sends the whole document from your own copy",
				"Under the Company's Observe policy the operator writes nothing", saysEnforce},
		},
		{
			name: "the fleet's revision without a seal, under Seed", answer: fleetSealMissing,
			policy: crewletaiv1alpha1.ManagementPolicySeed,
			says: []string{saysUnsealed, "arrived without a seal", "came from outside the fleet",
				"put there from outside the fleet", "Under the Company's Seed policy", saysEnforce},
		},
		{
			name: "the fleet's revision without a seal, under Observe", answer: fleetSealMissing,
			policy: crewletaiv1alpha1.ManagementPolicyObserve,
			says: []string{"arrived without a seal", "put there from outside the fleet",
				"Under the Company's Observe policy the operator writes nothing", saysEnforce},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			withPolicy(t, w, c.policy)
			w.engine.cannotOpen(c.answer)
			_, err := w.run.converge(ctx, w.client)
			if err == nil {
				t.Fatal("converge() = nil over a revision the engine cannot open")
			}
			result := w.run.engineFailed(ctx, w.client, err)

			got := w.run.configApplied
			if got == nil || got.Reason != crewletaiv1alpha1.ReasonRevisionUnreadable {
				t.Fatalf("ConfigApplied = %+v, want False RevisionUnreadable", got)
			}
			// Every answer names the fleet's revision, r1.
			for _, s := range append([]string{"revision r1"}, c.says...) {
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
			if written, refused := w.engine.writes(); len(written) != 0 || refused != 0 {
				t.Errorf("the operator wrote %v (and %d refused) over a revision the engine cannot open, want nothing",
					written, refused)
			}
			if !w.engine.holds(w.registry) || !slices.Equal(w.engine.ids(), []string{"r1"}) {
				t.Errorf("the store holds the registry: %t, and the revisions are %v; want both as they were",
					w.engine.holds(w.registry), w.engine.ids())
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

// TestEnforceReplacesARevisionStoredWithoutASeal holds the operator, under
// Enforce, to the engine's remedy for a revision it stores without a seal,
// this node's copy or the fleet's: the rendered document replaces it, as one
// write conditioned on that revision (If-Match), which is still the base
// whatever nobody can read of it, and the Company ends Applied, the engine
// having applied the epoch, with what was replaced said in the condition, the
// new revision's summary and a Warning event. Nothing is read from the
// revision, so nothing the store holds is deleted before the write, the value
// of a deleted Secret included: the engine's nodes run, and boot again, the
// revision before it, whose reading nobody knows. The sync after the write
// removes it, the replacement letting it go, and the replacement is activated
// again at once for the engine to read the store without it. The next
// reconcile finds the engine running the rendered document and writes
// nothing.
func TestEnforceReplacesARevisionStoredWithoutASeal(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(revision string) string
	}{
		{"this node's copy without a seal", sealMissing},
		{"the fleet's revision without a seal", fleetSealMissing},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			// The state records no reading of r1, so nothing says which of
			// the store's names r1 reads.
			w := newWithdrawal(t, runningNoRecord)
			w.engine.cannotOpen(c.answer)
			var heldAtWrite bool
			w.engine.beforeWrite = func(e *storeEngine) { _, heldAtWrite = e.store[w.registry] }
			result, err := w.run.converge(ctx, w.client)
			if err != nil {
				t.Fatalf("converge() = %v, want the rendered document written over a revision without a seal", err)
			}
			written, refused := w.engine.writes()
			if conditions := w.engine.writtenOn(); !slices.Equal(written, []string{"r2"}) || refused != 0 ||
				!slices.Equal(conditions, []string{"r1"}) {
				t.Fatalf("the operator wrote %v on the conditions %v (and %d were refused), want r2 alone, "+
					"conditioned on r1", written, conditions, refused)
			}
			if !heldAtWrite {
				t.Error("the store lost the value of a deleted Secret before the write, while the nodes run a revision " +
					"nobody can read")
			}
			if summary := w.engine.summaryOfRevision("r2"); !strings.HasPrefix(summary,
				"crewlet-operator: replaced revision r1, stored without a seal, with Company/acme") {
				t.Errorf("r2 is summarised %q, want it saying it replaced r1, stored without a seal", summary)
			}
			// The engine activates r2 again itself as the value r2 reads is
			// removed after it.
			if ids := w.engine.ids(); !slices.Equal(ids, []string{"r1", "r2", "r3"}) ||
				w.engine.summaryOf() != "reload: secret "+w.registry+" removed" {
				t.Errorf("the engine's revisions are %v, the last summarised %q; want r1, the replacement r2, and r2 "+
					"activated again as r3 at the removal of the deleted Secret's value", ids, w.engine.summaryOf())
			}
			if w.engine.holds(w.registry) {
				t.Error("the store keeps the value of a deleted Secret the replacement lets go")
			}
			if withdrawn := w.state(t).Withdrawn(); len(withdrawn) != 0 {
				t.Errorf("the state owes an activation without %v, want none: r3 read the store without it", withdrawn)
			}

			health, healthErr := w.client.Health(ctx)
			w.run.verifyApplied(ctx, w.client, health, healthErr, result)
			got := w.run.configApplied
			if got == nil || got.Status != metav1.ConditionTrue || got.Reason != crewletaiv1alpha1.ReasonApplied ||
				!strings.Contains(got.Message, "as revision r2, replacing revision r1, which the engine stores without a seal") {
				t.Errorf("ConfigApplied = %+v, want True Applied, saying r2 replaced r1, stored without a seal", got)
			}
			if w.run.applied.kind != appliedInRevision || w.run.applied.revision != "r3" {
				t.Errorf("the parts are %+v, want in revision r3, r2 activated again", w.run.applied)
			}
			recorded := drained(w.events)
			i := slices.IndexFunc(recorded, func(e string) bool {
				return strings.HasPrefix(e, "Warning "+crewletaiv1alpha1.ReasonRevisionUnreadable+" ")
			})
			if i < 0 || !strings.Contains(recorded[i], "active revision r1 was one it stores without a seal") ||
				!strings.Contains(recorded[i], "revision r2, the rendered document, replaced it") {
				t.Errorf("events %q, want a Warning RevisionUnreadable saying r2 replaced r1", recorded)
			}
			if !told(recorded, "Reactivated") || warned(recorded, "SecretWithdrawalFailed") || told(recorded, "SecretKept") {
				t.Errorf("events %q, want r2 activated again, and nothing kept or failed", recorded)
			}

			w.again(t, w.statusWritten())
			if _, err := w.run.converge(ctx, w.client); err != nil {
				t.Fatal(err)
			}
			if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2"}) {
				t.Errorf("the operator wrote %v, want r2 alone: the next reconcile finds it running", written)
			}
		})
	}
}

// TestAReplacementOverARevisionWithoutASealKeepsWhatTheNodesRun holds the
// replacement of a revision the engine stores without a seal, under Enforce,
// to what the fleet says its nodes run: having refused that revision, each
// runs the one before it, which the operator reads (GET
// /config/revisions/{id}) and keeps the names of, beside every name the store
// lists, while the rendered document is written over the revision without a
// seal, conditioned on it, all the same. The value of a deleted Secret the
// revision before reads as required stays in the store after the write, for
// as long as a node runs that revision, and goes once the nodes run the
// replacement. Deciding by the fleet's revision alone, the operator would
// delete a value a running node reads at its next apply attempt.
func TestAReplacementOverARevisionWithoutASealKeepsWhatTheNodesRun(t *testing.T) {
	ctx := context.Background()
	// r1 requires the registry, as the state records, and the rendered
	// document lets it go. r2, on the fleet's pointer, carries no seal, so
	// the node refused it and runs r1.
	w := newWithdrawal(t, runningRequires)
	w.engine.mu.Lock()
	w.engine.revisions = append(w.engine.revisions, storeRevision{id: w.engine.mint(), unopened: fleetSealMissing})
	w.engine.serving = map[string]string{storeNode: "r1"}
	w.engine.mu.Unlock()
	w.engine.takeRequests()

	if _, err := w.run.converge(ctx, w.client); err != nil {
		t.Fatalf("converge() = %v, want the rendered document written over a revision without a seal", err)
	}
	written, _ := w.engine.writes()
	if conditions := w.engine.writtenOn(); !slices.Equal(written, []string{"r3"}) ||
		!slices.Equal(conditions, []string{"r2"}) {
		t.Fatalf("the operator wrote %v on the conditions %v, want r3 alone, conditioned on r2", written, conditions)
	}
	if requests := w.engine.takeRequests(); !slices.Contains(requests, "GET /config/revisions/r1") {
		t.Errorf("the engine answered %q, want r1, which the node runs, read", requests)
	}
	if !w.engine.holds(w.registry) {
		t.Error("the store lost the value of a deleted Secret r1 requires while a node runs r1")
	}

	// The node applies the replacement.
	w.engine.mu.Lock()
	w.engine.serving = nil
	w.engine.mu.Unlock()
	w.again(t, w.statusWritten())
	if _, err := w.run.converge(ctx, w.client); err != nil {
		t.Fatal(err)
	}
	if w.engine.holds(w.registry) {
		t.Error("the store keeps the value of a deleted Secret once the nodes run the replacement, which lets it go")
	}
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r3"}) {
		t.Errorf("the operator wrote %v, want r3 alone: the next reconcile finds the nodes running it", written)
	}
}

// TestAReplacementRefusedOverARevisionWithoutASealKeepsTheStore holds a
// document the engine refuses over a revision it stores without a seal to the
// store as it was and to an activation owed and not asked for. Nothing the
// store holds is deleted: the nodes run, and boot again, the revision before
// the unsealed one, which may read a name the rendered document does not. And
// the engine neither shows nor activates again a revision without a seal, so
// asking for one is refused every time: asked, the reconcile came back every
// ten seconds over a refusal no retry clears, each time with a
// SecretWithdrawalFailed. The write that replaces it, once the document is
// valid, settles what is owed and removes what it lets go.
func TestAReplacementRefusedOverARevisionWithoutASealKeepsTheStore(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// r1 let the registry go, and an earlier sync withdrew it, the
	// engine's re-activation at the removal failing: an activation without
	// it is owed.
	operatorActivates(w)
	read := w.resolve(t)
	read.Keep("r1", w.registry)
	ref := types.NamespacedName{Namespace: w.run.company.Namespace, Name: secretSyncStateName(w.run.company)}
	owed, _, err := secretsync.Sync(ctx, w.client, read, w.state(t), operatorLogin, discardState)
	if err != nil {
		t.Fatal(err)
	}
	if err := secretsync.SaveState(ctx, w.cluster, ref, w.run.company, owed); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owed.Withdrawn(), []string{w.registry}) {
		t.Fatalf("the state owes %v, want the registry", owed.Withdrawn())
	}
	// A value the store holds that the rendered document does not
	// reference: one the revision the nodes run may.
	other := secretsync.StoreName(secretref.Key{Namespace: "ns", Name: "earlier", Key: optionalKey})
	w.engine.mu.Lock()
	w.engine.store[other] = "earlier"
	w.engine.mu.Unlock()
	w.engine.cannotOpen(sealMissing)
	w.engine.refusal = invalidDocument
	w.engine.takeRequests()

	result, err := w.run.converge(ctx, w.client)
	if err != nil {
		t.Fatalf("converge() = %v, want the refusal reported", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("the next look is %s on, want none: a refused document waits for its objects", result.RequeueAfter)
	}
	requests := w.engine.takeRequests()
	if i := slices.Index(requests, "PUT "+configRoute); i < 0 ||
		slices.ContainsFunc(requests[i+1:], func(r string) bool { return strings.HasSuffix(r, configRoute) || strings.HasSuffix(r, reloadRoute) }) {
		t.Errorf("the engine answered %q, want the dry run, and no read or reload of the document after it", requests)
	}
	if !w.engine.holds(other) || len(w.engine.ids()) != 1 {
		t.Errorf("the store holds %s: %t, and the revisions are %v; want it kept and r1 alone", other,
			w.engine.holds(other), w.engine.ids())
	}
	if withdrawn := w.state(t).Withdrawn(); !slices.Equal(withdrawn, []string{w.registry}) {
		t.Errorf("the state owes %v, want the registry still owed", withdrawn)
	}
	if recorded := drained(w.events); warned(recorded, "SecretWithdrawalFailed") {
		t.Errorf("events %q, want no SecretWithdrawalFailed over a revision nothing activates again", recorded)
	}

	w.again(t, w.statusWritten())
	w.engine.refusal = nil
	if _, err := w.run.converge(ctx, w.client); err != nil {
		t.Fatal(err)
	}
	written, _ := w.engine.writes()
	if conditions := w.engine.writtenOn(); !slices.Equal(written, []string{"r2"}) || !slices.Equal(conditions, []string{"r1"}) {
		t.Fatalf("the operator wrote %v on the conditions %v, want r2, conditioned on r1", written, conditions)
	}
	if withdrawn := w.state(t).Withdrawn(); len(withdrawn) != 0 || w.engine.holds(other) {
		t.Errorf("the state owes %v and the store holds %s: %t; want r2's activation to settle both", withdrawn,
			other, w.engine.holds(other))
	}
}

// TestAHeldDocumentOverARevisionWithoutASealKeepsTheStore holds a document
// held for a missing Secret, over a revision the engine stores without a seal,
// to what enforce does before the write that replaces it: the store keeps the
// value of a deleted Secret, told as kept for a revision nobody can read.
func TestAHeldDocumentOverARevisionWithoutASealKeepsTheStore(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	w.engine.cannotOpen(fleetSealMissing)
	w.engine.takeRequests()
	result, err := w.run.withdrawHeld(ctx, w.resolve(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("the next look is %s on, want none: the hold's own reason brings the reconcile back", result.RequeueAfter)
	}
	if requests := w.engine.takeRequests(); slices.Contains(requests, "POST "+reloadRoute) ||
		slices.ContainsFunc(requests, func(r string) bool { return strings.HasPrefix(r, "DELETE ") }) {
		t.Errorf("the engine answered %q, want no reload and no deletion", requests)
	}
	if !w.engine.holds(w.registry) {
		t.Error("the store lost the value of a deleted Secret while the nodes run a revision nobody can read")
	}
	recorded := drained(w.events)
	if warned(recorded, "SecretWithdrawalFailed") {
		t.Errorf("events %q, want no SecretWithdrawalFailed over a revision nothing activates again", recorded)
	}
	i := slices.IndexFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Normal SecretKept ") })
	if i < 0 || !strings.Contains(recorded[i], "revision r1, the engine's active revision, is stored without a seal") ||
		strings.Contains(recorded[i], "which the engine runs") {
		t.Errorf("events %q, want SecretKept calling r1, stored without a seal, the engine's active revision, which its "+
			"nodes do not run", recorded)
	}
	// And what SecretsSynced and a part's ResolvedRefs line say of it.
	if got := w.run.retainedSentence(); !strings.Contains(got, ": the engine's active revision is stored without a seal") {
		t.Errorf("SecretsSynced's sentence is %q, want it calling r1 the engine's active revision", got)
	}
	registry := secretref.Key{Namespace: "ns", Name: optionalSecret, Key: optionalKey}
	if got := w.run.leftOutLines([]leftOutValue{{key: registry, line: registry.String()}}); len(got) != 1 ||
		!strings.Contains(got[0], "the value it held for its active revision, which is stored without a seal") {
		t.Errorf("ResolvedRefs's line is %q, want it calling r1 the engine's active revision", got)
	}
}

// TestAHeldDocumentOverARevisionWithoutASealOwesItsActivation holds a
// document held for a missing Secret, over a revision the engine stores
// without a seal, with an activation owed for a value an earlier sync
// withdrew, to the withdrawal owed and not asked for: the engine neither shows
// nor activates again a revision without a seal, so asking for one is refused
// every time, and the reconcile came back every ten seconds over a refusal no
// retry clears, each time with a SecretWithdrawalFailed. Nor does the
// revision without a seal settle it, although the fleet's pointer names it
// at an epoch above the one the withdrawal was marked by: the nodes run the
// revision before it, which activated before the withdrawal. The write that
// replaces the revision settles it.
func TestAHeldDocumentOverARevisionWithoutASealOwesItsActivation(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// r1 let the registry go, and an earlier sync withdrew it, the
	// engine's re-activation at the removal failing: an activation without
	// it is owed.
	operatorActivates(w)
	read := w.resolve(t)
	read.Keep("r1", w.registry)
	ref := types.NamespacedName{Namespace: w.run.company.Namespace, Name: secretSyncStateName(w.run.company)}
	owed, _, err := secretsync.Sync(ctx, w.client, read, w.state(t), operatorLogin, discardState)
	if err != nil {
		t.Fatal(err)
	}
	// Marked by r1's epoch, the pointer's when the withdrawal was made.
	owed = owed.Observed(1)
	if err := secretsync.SaveState(ctx, w.cluster, ref, w.run.company, owed); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(owed.Withdrawn(), []string{w.registry}) {
		t.Fatalf("the state owes %v, want the registry", owed.Withdrawn())
	}
	// Then somebody's revision r2, stored without a seal, takes the
	// pointer, at epoch 2.
	w.engine.mu.Lock()
	w.engine.revisions = append(w.engine.revisions, storeRevision{id: w.engine.mint(), document: w.engine.active().document})
	w.engine.mu.Unlock()
	w.engine.cannotOpen(fleetSealMissing)
	w.engine.takeRequests()
	result, err := w.run.withdrawHeld(ctx, w.resolve(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("the next look is %s on, want none: the hold's own reason brings the reconcile back", result.RequeueAfter)
	}
	requests := w.engine.takeRequests()
	if i := slices.Index(requests, "GET "+configRoute); i < 0 || slices.ContainsFunc(requests[i+1:], func(r string) bool {
		return strings.HasSuffix(r, configRoute) || strings.HasSuffix(r, reloadRoute)
	}) {
		t.Errorf("the engine answered %q, want the one read of the document and no reload after it", requests)
	}
	if withdrawn := w.state(t).Withdrawn(); !slices.Equal(withdrawn, []string{w.registry}) {
		t.Errorf("the state owes %v, want the registry still owed", withdrawn)
	}
	if recorded := drained(w.events); warned(recorded, "SecretWithdrawalFailed") {
		t.Errorf("events %q, want no SecretWithdrawalFailed over a revision nothing activates again", recorded)
	}
}

// TestUnderEnforceAnUnsealedRevisionAfterTheWriteIsLookedAtSoon holds the one
// report of a revision without a seal under Enforce, one that lands on the
// fleet's pointer after the operator's read (from outside the fleet), to the
// operator's own remedy and its timing: ConfigApplied says the next look
// replaces it, and that look comes ten seconds on, not at the resync interval,
// since nothing but that look is waited for.
func TestUnderEnforceAnUnsealedRevisionAfterTheWriteIsLookedAtSoon(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// r1 is the rendered document, so a rotated value, whose re-activation
	// by the engine fails, is what has the operator write it.
	w.rotate(t)
	operatorActivates(w)
	w.engine.afterApply = func(e *storeEngine) {
		e.revisions = append(e.revisions, storeRevision{id: e.mint(), unopened: fleetSealMissing})
	}
	_, err := w.run.converge(ctx, w.client)
	if _, ok := engine.Unsealed(err); !ok {
		t.Fatalf("converge() = %v, want the read after the write refused over a revision without a seal", err)
	}
	result := w.run.engineFailed(ctx, w.client, err)
	if result.RequeueAfter != engineRetryInterval {
		t.Errorf("the next look is %s on, want %s", result.RequeueAfter, engineRetryInterval)
	}
	got := w.run.configApplied
	if got == nil || got.Reason != crewletaiv1alpha1.ReasonRevisionUnreadable ||
		!strings.Contains(got.Message, "the operator replaces it with the rendered document at its next look") {
		t.Errorf("ConfigApplied = %+v, want RevisionUnreadable with the Enforce remedy", got)
	}
}

// TestAReplacementTheEngineCouldNotConfirmIsKept holds a write over a
// revision stored without a seal whose activation the engine could not
// confirm to the record every such write keeps (config_unconfirmed.go): while
// the engine still runs that revision, the write may land, so it is kept, and
// the write sent again on the same condition is recorded beside it; once one
// lands, the reconcile records it as the operator's, reports what it replaced
// as a confirmed replacement is reported, and writes nothing more. Settled
// against no revision, the first would be dropped as one that can never land,
// and its landing read as somebody else's document; confirmed from a record
// that did not keep what it replaced, it was reported as a plain Applied, and
// nothing said a revision nobody could read had gone.
func TestAReplacementTheEngineCouldNotConfirmIsKept(t *testing.T) {
	w := newWithdrawal(t, runningNoRecord)
	w.engine.cannotOpen(sealMissing)
	for range 2 {
		w.engine.activation = pending
		reconcileOnce(t, w)
		if c := w.run.configApplied; c == nil || c.Reason != crewletaiv1alpha1.ReasonEngineUnavailable ||
			!strings.Contains(c.Message, "which would replace revision r1, stored without a seal") {
			t.Errorf("ConfigApplied is %+v, want EngineUnavailable saying the write would replace r1", c)
		}
		w.again(t, w.statusWritten())
	}
	got := w.run.company.Status.Config.Unconfirmed
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2", "r3"}) || len(got) != 2 ||
		got[0].RevisionID != "r2" || got[0].BaseRevisionID != "r1" || got[1].RevisionID != "r3" ||
		got[1].BaseRevisionID != "r1" || got[0].Replaced != crewletaiv1alpha1.ReplacedUnsealed {
		t.Fatalf("the operator wrote %v and keeps %+v unconfirmed; want r2 and r3, each built on r1, kept, "+
			"recorded as replacing a revision without a seal", written, got)
	}
	drained(w.events)
	w.engine.mu.Lock()
	w.engine.land("r2")
	w.engine.mu.Unlock()
	w.engine.activation = confirmed
	reconcileOnce(t, w)
	if written, _ := w.engine.writes(); !slices.Equal(written, []string{"r2", "r3"}) {
		t.Errorf("the operator wrote %v, want nothing beyond r2 and r3: r2 landed", written)
	}
	if config := w.statusWritten().Status.Config; config.AppliedHash != got[0].AppliedHash || len(config.Unconfirmed) != 0 {
		t.Errorf("status.config is %+v, want r2's write recorded, with its hash, and nothing unconfirmed", config)
	}
	if c := w.run.configApplied; c == nil || c.Status != metav1.ConditionTrue || c.Reason != crewletaiv1alpha1.ReasonApplied ||
		!strings.Contains(c.Message, "as revision r2, replacing revision r1, which the engine stores without a seal") {
		t.Errorf("ConfigApplied is %+v, want True Applied, saying r2 replaced r1, stored without a seal", c)
	}
	recorded := drained(w.events)
	if i := slices.IndexFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Normal Activated ") }); i < 0 ||
		!strings.Contains(recorded[i], "runs revision r2 as epoch") {
		t.Errorf("events %q, want an Activated event saying the engine runs r2", recorded)
	}
	i := slices.IndexFunc(recorded, func(e string) bool {
		return strings.HasPrefix(e, "Warning "+crewletaiv1alpha1.ReasonRevisionUnreadable+" ")
	})
	if i < 0 || !strings.Contains(recorded[i], "active revision r1 was one it stores without a seal") ||
		!strings.Contains(recorded[i], "revision r2, the rendered document, replaced it") {
		t.Errorf("events %q, want a Warning RevisionUnreadable saying r2 replaced r1", recorded)
	}
}

// TestAWriteIsConditionedOnTheRevisionTheReconcileRead holds the operator's
// write to the revision its reconcile read, which its diff, its drift and the
// names it kept in the store were decided against. Another writer's revision
// that lands between that read and the dry run is not written over unseen, nor
// reported as the revision the reconcile read: the reconcile starts over from
// a fresh read, and writes over what it read then. Over a revision the engine
// stores without a seal, that is what keeps the operator from saying it
// replaced one when it replaced another writer's document.
func TestAWriteIsConditionedOnTheRevisionTheReconcileRead(t *testing.T) {
	for _, c := range []struct {
		name string
		// unsealed makes r1 a revision the engine stores without a seal.
		unsealed bool
	}{
		{name: "the reconcile read the engine's document"},
		{name: "the reconcile read a revision stored without a seal", unsealed: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			w := newWithdrawal(t, runningLetsGo)
			if c.unsealed {
				// The operator writes over a revision it cannot read.
				w.engine.cannotOpen(sealMissing)
			} else {
				// r1 is the rendered document, so a rotated value, whose
				// re-activation by the engine fails, is what has the
				// operator write it.
				w.rotate(t)
				operatorActivates(w)
			}
			// Somebody activates another document, which the engine opens,
			// after the operator read r1 and before its dry run.
			w.engine.beforeCheck = func(e *storeEngine) { e.activateOther(w.requiring) }
			if _, err := w.run.converge(ctx, w.client); err != nil {
				t.Fatalf("converge() = %v, want the reconcile started over and the rendered document written", err)
			}
			written, _ := w.engine.writes()
			if conditions := w.engine.writtenOn(); !slices.Equal(written, []string{"r3"}) ||
				!slices.Equal(conditions, []string{"r2"}) {
				t.Fatalf("the operator wrote %v on the conditions %v, want r3 alone, conditioned on r2, the revision "+
					"its second read found", written, conditions)
			}
			got := w.run.configApplied
			if got == nil || got.Reason != crewletaiv1alpha1.ReasonDriftCorrected ||
				!strings.HasPrefix(got.Message, "Revision r2, which the operator did not write") {
				t.Errorf("ConfigApplied = %+v, want DriftCorrected over r2, the revision the write replaced", got)
			}
			if summary := w.engine.summaryOfRevision("r3"); strings.Contains(summary, "without a seal") {
				t.Errorf("r3 is summarised %q, which says it replaced a revision without a seal; it replaced r2", summary)
			}
			if recorded := drained(w.events); warned(recorded, crewletaiv1alpha1.ReasonRevisionUnreadable) {
				t.Errorf("events %q, want none saying a revision without a seal was replaced", recorded)
			}
		})
	}
}

// TestASeatHeldOverAReplacementSaysTheDocumentDoesNotKeepIt holds the SeatHeld
// report on a document replacing a revision stored without a seal to what the
// engine can say of it: the document does not keep those seats as human seats,
// since nobody can read whether the revision it replaces had them, and not that
// it takes them out of the company.
func TestASeatHeldOverAReplacementSaysTheDocumentDoesNotKeepIt(t *testing.T) {
	held := asReturned(&engine.SeatHeldError{Held: map[string]engine.SeatHolder{
		seatFounder: {Kind: engine.HolderPerson, Stage: "active"},
	}})
	for _, c := range []struct {
		name, unsealed, says, not string
	}{
		{name: "a document replacing the engine's", says: "it takes human seats out of the company",
			not: "does not keep"},
		{name: "a document replacing a revision without a seal", unsealed: "r1",
			says: "it replaces revision r1, which the engine does not open, and does not keep as human seats these " +
				"seats its identity directory holds: founder (held by a person, active)",
			not: "takes human seats out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			run, _, _, _ := refusalRun()
			run.unsealed = c.unsealed
			if !run.seatHeld(held) {
				t.Fatal("seatHeld() = false for a *SeatHeldError")
			}
			if got := run.configApplied.Message; !strings.Contains(got, c.says) || strings.Contains(got, c.not) {
				t.Errorf("ConfigApplied says %q; want it saying %q, and not %q", got, c.says, c.not)
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
	w.engine.cannotOpen(keyringLost)
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
