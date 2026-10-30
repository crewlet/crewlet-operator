package engine_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// These tests run the client against the real engine every suite runs
// (see enginetest), so a change in the engine's API that the client does not
// follow fails here rather than in a cluster.

// eventually polls check until it returns nil or the deadline passes.
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: still failing after 30s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestEngineReadyFollowsTheFirstApply pins what Ready means on an engine: an
// engine with no company answers, and is not ready, until a company has been
// applied.
func TestEngineReadyFollowsTheFirstApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()

	err := client.Ready(ctx)
	var notReady *engine.NotReadyError
	if !errors.Is(err, engine.ErrNotReady) || !errors.As(err, &notReady) || notReady.Reason != unconfigured {
		t.Fatalf("Ready() before the first apply = %v, want ErrNotReady with reason unconfigured", err)
	}
	if _, err := client.ActiveConfig(ctx); !errors.Is(err, engine.ErrNoActiveRevision) {
		t.Fatalf("ActiveConfig() before the first apply = %v, want ErrNoActiveRevision", err)
	}

	if _, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: first company", CreateOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Ready() after the first apply", func() error { return client.Ready(ctx) })
}

// TestEngineReloadActivatesTheDocumentAgain pins the gesture the config
// reconciler makes after it removes a value from the engine's store while the
// next document cannot be applied: the active document is activated again,
// unchanged, as a new revision at a later epoch, so the engine reads its
// store again; with no document there is none to activate.
func TestEngineReloadActivatesTheDocumentAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()

	if _, err := client.ReloadConfig(ctx, "enginetest: nothing to reload"); !errors.Is(err, engine.ErrNoActiveRevision) {
		t.Fatalf("ReloadConfig() before the first apply = %v, want ErrNoActiveRevision", err)
	}
	applied, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: reload", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const summary = "enginetest: read the store again"
	reloaded, err := client.ReloadConfig(ctx, summary)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RevisionID == "" || reloaded.RevisionID == applied.RevisionID || reloaded.Epoch <= applied.Epoch {
		t.Fatalf("ReloadConfig() = %+v after the apply's %+v, want a new revision at a later epoch", reloaded, applied)
	}
	// READ ONCE: GET /config answers the fleet's activation pointer, which
	// the reload moved before it answered, whatever the node has applied.
	after, err := client.ActiveConfig(ctx)
	switch {
	case err != nil:
		t.Fatal(err)
	case after.RevisionID != reloaded.RevisionID:
		t.Fatalf("the active revision right after the reload is %s, want %s", after.RevisionID, reloaded.RevisionID)
	case !bytes.Equal(after.Document, before.Document):
		t.Fatalf("the reloaded document differs:\n%s\nwant\n%s", after.Document, before.Document)
	}
}

// TestEngineHealthReportsTheAppliedEpoch pins what the config reconciler
// waits for after a write: the engine applies the revision asynchronously,
// never reading a diverged posture while it does, and its /health then
// reports it applied, converged (posture serve) and ok, with an applied epoch
// that reaches the epoch the write returned; and the node's record in /fleet,
// which follows /health and is waited for as it is, names that epoch, its
// revision and a clean outcome, and that revision as the one it runs and, a
// data node's store outliving a restart, the one it would start on: the
// engine moves what it starts on before it records the apply, so no record
// names the applied revision as run and not as started on.
func TestEngineHealthReportsTheAppliedEpoch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()

	before, err := client.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != unconfigured || before.Configured {
		t.Fatalf("Health() before the first apply = %+v, want status unconfigured and not configured", before)
	}

	applied, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: health", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var diverged []engine.Health
	eventually(t, "the engine reporting the write's epoch applied", func() error {
		h, err := client.Health(ctx)
		switch {
		case err != nil:
			return err
		case h.AppliedEpoch < applied.Epoch && h.Posture.Diverged():
			// The config reconciler reads a diverged posture as a failed
			// apply: one that applies reads wait until it has.
			diverged = append(diverged, h)
			return fmt.Errorf("applied epoch %d is behind the write's %d, posture %q", h.AppliedEpoch, applied.Epoch, h.Posture)
		case h.AppliedEpoch < applied.Epoch:
			return fmt.Errorf("applied epoch %d is behind the write's %d", h.AppliedEpoch, applied.Epoch)
		case h.Status != "ok" || !h.Configured || h.Posture != engine.PostureServe:
			return fmt.Errorf("Health() = %+v, want status ok, configured, posture serve", h)
		}
		return nil
	})
	if len(diverged) > 0 {
		t.Errorf("Health() read a diverged posture during an apply that succeeded: %+v", diverged)
	}
	// THE PEERS' RECORD FOLLOWS /health, waited for as /health was. The
	// engine moves its own progress, which /health reads, then writes its
	// boot row to its own store, and only then the record its peers read in
	// /fleet to the coordination store (Crewlet's internal/engine
	// Reconciler.apply), so a read between finds the record not yet written.
	want := engine.ApplyRecord{
		Epoch: applied.Epoch, RevisionID: applied.RevisionID, Status: engine.ApplyOK, Serving: applied.RevisionID,
		Boots: applied.RevisionID,
	}
	eventually(t, "the engine's /fleet record of the apply", func() error {
		fleet, err := client.Fleet(ctx)
		if err != nil {
			return err
		}
		record := fleet.LastApply()
		settled, err := recordSettled(&record, &want)
		if err != nil {
			t.Fatal(err)
		}
		if !settled {
			return fmt.Errorf("LastApply() = %+v, not yet the apply's record", record)
		}
		return nil
	})
}

// recordSettled reports whether record, a node's /fleet record of its last
// apply, is want, the record of the apply the case made: false while the
// record is behind want's epoch, which is a record not yet written (the zero
// record) or one of an earlier apply, and an error naming the record for one
// at want's epoch or later that is not want, which waiting will not change.
// FAILING FAST THERE IS ON PURPOSE: an error record at the apply's epoch is the
// engine's account of a failed apply, not a lag, so do not turn it into a wait.
func recordSettled(record, want *engine.ApplyRecord) (bool, error) {
	switch {
	case *record == *want:
		return true, nil
	case record.Epoch < want.Epoch:
		return false, nil
	}
	return false, fmt.Errorf("LastApply() = %+v, want %+v", *record, *want)
}

// A RECORD NOT YET WRITTEN IS WAITED FOR, AND A WRONG ONE FAILS AT ONCE: the
// zero record and an earlier apply's are the lag between the engine's own
// progress and its peers' record; a record at the apply's epoch that names
// another revision, or a failure, is not, and fails naming itself.
func TestARecordOfTheApplyIsWaitedForAndAWrongOneFails(t *testing.T) {
	t.Parallel()
	const applied, before, another = "applied-revision", "earlier-revision", "another-revision"
	want := engine.ApplyRecord{Epoch: 3, RevisionID: applied, Status: engine.ApplyOK, Serving: applied, Boots: applied}
	earlier := engine.ApplyRecord{Epoch: 2, RevisionID: before, Status: engine.ApplyOK, Serving: before, Boots: before}
	other := want
	other.RevisionID, other.Serving, other.Boots = another, another, another
	failed := want
	failed.Status, failed.Serving, failed.Boots = engine.ApplyError, before, before
	for _, tc := range []struct {
		name    string
		record  engine.ApplyRecord
		settled bool
		wrong   bool
	}{
		{"not yet written", engine.ApplyRecord{}, false, false},
		{"an earlier apply's", earlier, false, false},
		{"the apply's", want, true, false},
		{"another revision at the apply's epoch", other, false, true},
		{"a failure at the apply's epoch", failed, false, true},
	} {
		settled, err := recordSettled(&tc.record, &want)
		if settled != tc.settled || (err != nil) != tc.wrong {
			t.Errorf("%s: recordSettled = (%v, %v), want settled %v, wrong %v", tc.name, settled, err, tc.settled, tc.wrong)
		}
		if tc.wrong && (err == nil || !strings.Contains(err.Error(), tc.record.RevisionID)) {
			t.Errorf("%s: the failure %v does not name the record", tc.name, err)
		}
	}
}

// TestEngineFleetNamesTheSeatsHolder pins the read of /fleet against the
// engine: the answering node, its roles, broker and labels, the seat it holds
// because its labels match the seat's placement, a seat whose placement no
// node matches, which no node holds and the engine calls unplaceable, and the
// fleet's activation, which names the fleet's revision at its epoch.
func TestEngineFleetNamesTheSeatsHolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const node = "nimbus-data-0"
	labels := map[string]string{poolLabel: dataPool}
	company := enginetest.MinimalCompany + `    placement:
      labels: {crewlet.ai/pool: data}
  - name: Bo
    goal: "Answer what Ana cannot"
    placement:
      labels: {crewlet.ai/pool: general}
`
	client := enginetest.Start(t, enginetest.WithNode(node, labels), enginetest.WithCompany([]byte(company))).Client()

	var fleet engine.Fleet
	eventually(t, "the node holding ana", func() error {
		var err error
		if fleet, err = client.Fleet(ctx); err != nil {
			return err
		}
		if lease, ok := fleet.Holder("ana", ""); !ok || lease.Node != node || lease.Epoch < 1 || lease.AcquiredAt.IsZero() {
			return fmt.Errorf("Holder(ana) = %+v, %t, want %s with an epoch and a tenure", lease, ok, node)
		}
		return nil
	})
	self, ok := fleet.Node(node)
	if fleet.ThisNode != node || !ok || self.Broker != engine.BrokerMember || self.Labels[poolLabel] != dataPool ||
		!slices.Contains(self.Roles, "seats") || self.Seats != 1 {
		t.Errorf("Fleet() = this node %q, node %+v (%t); want %s, a member labelled for data that holds one seat",
			fleet.ThisNode, self, ok, node)
	}
	if _, held := fleet.Holder("bo", ""); held || !slices.Equal(fleet.Unplaceable, []string{"bo"}) {
		t.Errorf("bo: held %t, unplaceable %v; want held by nobody and unplaceable, as no node is labelled for its pool",
			held, fleet.Unplaceable)
	}
	// The fleet's activation names the revision GET /config answers, at its
	// epoch: the one read that says which epoch a revision the operator did
	// not activate itself activated as, and that settles a write the engine
	// could not confirm.
	active, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a := fleet.Activation; a == nil || a.RevisionID != active.RevisionID || a.Epoch < 1 {
		t.Errorf("Fleet().Activation = %+v, want the fleet's revision %s at its epoch", a, active.RevisionID)
	}
}

// TestEngineApplyActivatesTheRevision pins the write's answer to what the engine
// then runs: the revision it names is the active one, at the epoch it names.
func TestEngineApplyActivatesTheRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()

	applied, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: apply", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if applied.RevisionID == "" || applied.Epoch == 0 || applied.Warnings == nil {
		t.Fatalf("ApplyConfig() = %+v, want a revision, an epoch and a non-nil warning list", applied)
	}
	eventually(t, "the engine applying the revision", func() error {
		active, err := client.ActiveConfig(ctx)
		switch {
		case err != nil:
			return err
		case active.RevisionID != applied.RevisionID:
			t.Fatalf("active revision = %q, want the applied %q", active.RevisionID, applied.RevisionID)
		case !strings.Contains(string(active.Document), "name: Acme"):
			t.Fatalf("active document does not carry the company's name:\n%s", active.Document)
		case active.Epoch < applied.Epoch:
			return fmt.Errorf("applied epoch %d is behind the write's %d", active.Epoch, applied.Epoch)
		}
		return nil
	})

	if _, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: again", CreateOnly: true,
	}); !errors.Is(err, engine.ErrAlreadyConfigured) {
		t.Fatalf("a second create-only apply = %v, want ErrAlreadyConfigured", err)
	}
}

// TestEngineStaleIfMatchIsRevisionAdvanced pins the compare-and-set the operator
// relies on to never overwrite a revision it has not seen.
func TestEngineStaleIfMatchIsRevisionAdvanced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t, enginetest.WithCompany([]byte(enginetest.MinimalCompany))).Client()

	seeded, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany + "mission: \"Ship\"\n"),
		Summary:  "enginetest: on top of the seed", IfMatch: seeded.RevisionID,
	})
	if err != nil {
		t.Fatalf("ApplyConfig() with a current If-Match = %v", err)
	}

	_, err = client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: stale", IfMatch: seeded.RevisionID,
	})
	if !errors.Is(err, engine.ErrRevisionAdvanced) {
		t.Fatalf("ApplyConfig() with a stale If-Match = %v, want ErrRevisionAdvanced", err)
	}
	var apiErr *engine.APIError
	if !errors.As(err, &apiErr) || apiErr.CurrentRevisionID != next.RevisionID {
		t.Fatalf("ApplyConfig() with a stale If-Match = %v, want the current revision %q named", err, next.RevisionID)
	}
}

// TestEngineDryRunLocatesProblems pins what attribution is built on: the engine
// refuses an invalid document with each problem's path and seat, and a dry run
// changes nothing.
func TestEngineDryRunLocatesProblems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t, enginetest.WithCompany([]byte(enginetest.MinimalCompany))).Client()
	before, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := client.ApplyConfig(ctx, engine.ApplyRequest{Document: []byte(enginetest.MinimalCompany), DryRun: true})
	if err != nil {
		t.Fatalf("dry run of a valid document = %v", err)
	}
	if valid.BaseRevisionID != before.RevisionID || valid.RevisionID != "" {
		t.Fatalf("dry run = %+v, want it checked against %q and nothing stored", valid, before.RevisionID)
	}

	invalid := enginetest.MinimalCompany + `  - name: Bob
    goal: "Review the work"
    llm: nowhere
`
	_, err = client.ApplyConfig(ctx, engine.ApplyRequest{Document: []byte(invalid), DryRun: true})
	var verr *engine.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("dry run of an invalid document = %v, want a *ValidationError", err)
	}
	i := slices.IndexFunc(verr.Problems, func(p engine.Problem) bool { return p.Path == "roles[1].llm" })
	if i < 0 {
		t.Fatalf("problems %+v, want one at roles[1].llm", verr.Problems)
	}
	p := verr.Problems[i]
	if p.Seat != "bob" || !slices.Equal(p.Segments, rolesOneLLM) {
		t.Fatalf("problem = %+v, want it on seat bob at %v", p, rolesOneLLM)
	}

	after, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.RevisionID != before.RevisionID {
		t.Fatalf("the active revision moved from %q to %q on dry runs", before.RevisionID, after.RevisionID)
	}
}

// TestEngineSecretsRoundTrip pins the secret store's surface: a put is listed
// with its author and the credential it authenticated with, a delete says
// whether it removed anything.
func TestEngineSecretsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()
	const name = "K8S_SECRET_SLACK_BOTTOKEN_ABCD"

	if err := client.PutSecret(ctx, name, []byte("xoxb-not-a-real-token\n")); err != nil {
		t.Fatal(err)
	}
	listed, err := client.ListSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(listed, func(s engine.SecretInfo) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("ListSecrets() = %+v, want %s listed", listed, name)
	}
	self := engine.TokenLogin(enginetest.OperatorTokenID)
	if s := listed[i]; s.Source != "api" || s.UpdatedBy != self || s.OperatorID != self || s.KeyID == "" ||
		s.UpdatedAt.IsZero() {
		t.Fatalf("listed secret = %+v, want source api, written by and with %s, sealed and timestamped", s, self)
	}

	for _, want := range []bool{true, false} {
		removed, err := client.DeleteSecret(ctx, name)
		if err != nil || removed.Removed != want || removed.Reload != nil {
			t.Fatalf("DeleteSecret() = %+v, %v, want removed %v, nothing re-activated, nil", removed, err, want)
		}
	}
	listed, err = client.ListSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(listed, func(s engine.SecretInfo) bool { return s.Name == name }) {
		t.Fatalf("ListSecrets() after the delete = %+v, still lists %s", listed, name)
	}
}

// TestEngineASecretWriteReactivatesTheRevisionThatReadsIt pins what the
// operator's secret sync and withdrawals rest on (Crewlet's ADR-0070): a
// batch write of values the active revision reads is one re-activation of it,
// unchanged, which the 200 names and the engine then runs; a write or a
// removal of a value nothing reads re-activates nothing; and a removal of a
// value the revision reads re-activates it as a write does.
func TestEngineASecretWriteReactivatesTheRevisionThatReadsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()
	const read, unread = "ENGINETEST_LLM_API_KEY", "ENGINETEST_READ_BY_NOTHING"

	first, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: reads " + read, CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reactivated := func(what string, reload *engine.Reload, epoch int64) {
		t.Helper()
		if !reload.Landed() || reload.Epoch != epoch {
			t.Fatalf("%s: reload = %+v, want one re-activation, at epoch %d", what, reload, epoch)
		}
		active, err := client.ActiveConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if active.RevisionID != reload.RevisionID || !bytes.Equal(active.Document, before.Document) {
			t.Fatalf("%s: the engine runs revision %s, want the re-activation %s of the same document",
				what, active.RevisionID, reload.RevisionID)
		}
	}

	written, err := client.PutSecrets(ctx, map[string][]byte{read: []byte("sk-enginetest-not-a-key"), unread: []byte("x")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(written.Names, []string{read, unread}) || len(written.ReadBy[read]) == 0 ||
		written.ReadBy[unread] == nil || len(written.ReadBy[unread]) != 0 {
		t.Fatalf("PutSecrets() = %+v, want both written, %s read and %s read by nothing", written, read, unread)
	}
	reactivated("the batch", written.Reload, first.Epoch+1)

	written, err = client.PutSecrets(ctx, map[string][]byte{unread: []byte("y")}, false)
	if err != nil || written.Reload != nil {
		t.Fatalf("PutSecrets() of a value nothing reads = %+v, %v; want nothing re-activated", written, err)
	}
	removed, err := client.DeleteSecret(ctx, unread)
	if err != nil || !removed.Removed || removed.Reload != nil || len(removed.ReadBy) != 0 {
		t.Fatalf("DeleteSecret() of a value nothing reads = %+v, %v; want it removed and nothing re-activated", removed, err)
	}

	removed, err = client.DeleteSecret(ctx, read)
	if err != nil || !removed.Removed || len(removed.ReadBy) == 0 {
		t.Fatalf("DeleteSecret() of %s = %+v, %v; want it removed and read", read, removed, err)
	}
	reactivated("the removal", removed.Reload, first.Epoch+2)
}

// TestEngineDeferredWritesActivateNothingUntilTheLiveOne pins, against the
// real engine, what a secret sync's one activation rests on (Crewlet's
// ADR-0070, PUT /secrets/{name}?defer_reload=true): a batch written with
// defer_reload and a value written alone, as raw bytes, with defer_reload,
// both of a name the active revision reads, re-activate nothing and say so,
// and the one live write after them re-activates the revision exactly once,
// an epoch past where the deferred ones left it.
func TestEngineDeferredWritesActivateNothingUntilTheLiveOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()
	const read = "ENGINETEST_LLM_API_KEY"

	first, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: reads " + read, CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	deferred := func(what string, written engine.SecretsWritten) {
		t.Helper()
		if written.Reload == nil || !written.Reload.Deferred || written.Reload.Landed() || len(written.ReadBy[read]) == 0 {
			t.Fatalf("%s = %+v, want it deferred, nothing re-activated, and %s read", what, written, read)
		}
	}
	batch, err := client.PutSecrets(ctx, map[string][]byte{read: []byte("sk-enginetest-deferred")}, true)
	if err != nil {
		t.Fatal(err)
	}
	deferred("the deferred batch", batch)
	alone, err := client.WriteSecret(ctx, read, []byte{0xff, 0xfe, 's', 'k'}, true)
	if err != nil {
		t.Fatal(err)
	}
	deferred("the deferred single write", alone)
	if active, err := client.ActiveConfig(ctx); err != nil || active.RevisionID != first.RevisionID {
		t.Fatalf("after the deferred writes the engine runs %+v (%v), want revision %s still", active, err,
			first.RevisionID)
	}

	live, err := client.PutSecrets(ctx, map[string][]byte{read: []byte("sk-enginetest-live")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !live.Reload.Landed() || live.Reload.Epoch != first.Epoch+1 {
		t.Fatalf("the live write = %+v, want one re-activation, at epoch %d", live.Reload, first.Epoch+1)
	}
	active, err := client.ActiveConfig(ctx)
	if err != nil || active.RevisionID != live.Reload.RevisionID {
		t.Fatalf("after the live write the engine runs %+v (%v), want its re-activation %s", active, err,
			live.Reload.RevisionID)
	}
}

// TestEngineIntegrationsOfACompanyWithoutAny pins the empty answers: no company
// reports no tools, and a company with no integration reports every tool as
// not in use.
func TestEngineIntegrationsOfACompanyWithoutAny(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()

	tools, err := client.Integrations(ctx)
	if err != nil || len(tools) != 0 {
		t.Fatalf("Integrations() with no company = %+v, %v, want none", tools, err)
	}
	if _, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: company", CreateOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
	// The engine reports on the company it RUNS, which it does once it has
	// applied the revision, not when the write returns.
	eventually(t, "Ready() after the apply", func() error { return client.Ready(ctx) })
	tools, err = client.Integrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(tools))
	for _, tool := range tools {
		keys = append(keys, tool.Key)
		if tool.State != engine.IntegrationNotInUse || len(tool.Surfaces) != 0 {
			t.Errorf("tool %+v, want not in use with no surfaces", tool)
		}
	}
	for _, want := range []string{"slack", "github"} {
		if !slices.Contains(keys, want) {
			t.Errorf("Integrations() reports tools %v, want %s among them", keys, want)
		}
	}
}

// TestEngineRefusesAWrongToken pins ErrUnauthorized against the engine's guard,
// and that the second token the harness configures is accepted.
func TestEngineRefusesAWrongToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t, enginetest.WithCompanyWriters(enginetest.OperatorTokenID))
	if _, err := engine.New(e.URL, "not-"+e.OperatorToken).ListSecrets(ctx); !errors.Is(err, engine.ErrUnauthorized) {
		t.Fatalf("ListSecrets() with a wrong token = %v, want ErrUnauthorized", err)
	}
	if _, err := engine.New(e.URL, e.AccessToken).ListSecrets(ctx); err != nil {
		t.Fatalf("ListSecrets() with the access token = %v", err)
	}
}

// TestEngineRefusesAManagedDocumentToEveryOtherToken pins what the operator
// relies on when it renders api.auth.company_writers under Enforce: the token
// the list names writes the company document as before, while every other
// token is refused any change to it, a dry run included, with
// ErrConfigManaged naming the writers, and the document stays as it was. The
// same token still reads the document and writes the secret store, which a
// person keeps for rotating a leaked credential.
func TestEngineRefusesAManagedDocumentToEveryOtherToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t, enginetest.WithCompanyWriters(enginetest.OperatorTokenID))
	operator := e.Client()
	access := engine.New(e.URL, e.AccessToken)

	refused := func(what string, err error) {
		t.Helper()
		var apiErr *engine.APIError
		if !errors.Is(err, engine.ErrConfigManaged) || !errors.As(err, &apiErr) ||
			!slices.Equal(apiErr.ManagedBy, []string{enginetest.OperatorTokenID}) {
			t.Fatalf("%s with the access token = %v, want ErrConfigManaged naming %s as the writer", what, err, enginetest.OperatorTokenID)
		}
	}
	_, err := access.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: not a writer", CreateOnly: true,
	})
	refused("the first apply", err)

	first, err := operator.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: the writer", CreateOnly: true,
	})
	if err != nil {
		t.Fatalf("the first apply with the operator token: %v", err)
	}

	edited := []byte(strings.Replace(enginetest.MinimalCompany, "name: Acme", "name: Edited", 1))
	_, err = access.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, DryRun: true})
	refused("a dry run", err)
	_, err = access.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, Summary: "enginetest: an edit", IfMatch: first.RevisionID})
	refused("an edit", err)

	active, err := access.ActiveConfig(ctx)
	if err != nil {
		t.Fatalf("reading the managed document with the access token: %v", err)
	}
	if active.RevisionID != first.RevisionID || strings.Contains(string(active.Document), "Edited") {
		t.Fatalf("the engine runs revision %s:\n%s\nwant the operator's %s, unchanged", active.RevisionID, active.Document, first.RevisionID)
	}
	if err := access.PutSecret(ctx, "ENGINETEST_ROTATED", []byte("rotated")); err != nil {
		t.Fatalf("writing the secret store with the access token: %v", err)
	}

	second, err := operator.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, Summary: "enginetest: the writer edits", IfMatch: first.RevisionID})
	if err != nil {
		t.Fatalf("an edit with the operator token: %v", err)
	}
	// READ ONCE, right after the write: GET /config answers the fleet's
	// activation pointer, which the write moved, never the revision this
	// node is still applying, so the read after a write is the write.
	active, err = operator.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if active.RevisionID != second.RevisionID {
		t.Fatalf("GET /config right after the operator's edit answers revision %s, want %s", active.RevisionID, second.RevisionID)
	}
}

// TestEngineARefusedRevisionKeepsServingTheOneBefore pins what the secret sync
// keeps for: a node that refuses the fleet's revision goes on running the
// revision it ran before, says so in /fleet, and, its store outliving the
// restart as a data pod's does, says it would start on that revision (Boots)
// and boots on it again when it restarts, which it can only while the secret
// store holds what that revision references. GET /config answers the fleet's
// revision all along, and GET /config/revisions/{id} the document of the one
// the node runs, its references whole, which is how the operator learns what
// that node needs.
func TestEngineARefusedRevisionKeepsServingTheOneBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t)
	client := e.Client()
	const kept, unset = "ENGINETEST_E2B_SERVED", "ENGINETEST_E2B_NEVER_SET"

	if err := client.PutSecret(ctx, kept, []byte(enginetest.SandboxKeyValue)); err != nil {
		t.Fatal(err)
	}
	served, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: enginetest.SandboxCompany(kept), Summary: "enginetest: the revision that runs", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the engine applying the first revision", func() error { return client.Ready(ctx) })

	// The next revision drops the value the first one references, and
	// references one the store does not hold, which the dry run accepts
	// and the apply refuses.
	refused, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: enginetest.SandboxCompany(unset), Summary: "enginetest: the revision it refuses", IfMatch: served.RevisionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := func(record engine.ApplyRecord) error {
		if record.RevisionID != refused.RevisionID || record.Epoch != refused.Epoch || record.Status != engine.ApplyError ||
			record.Serving != served.RevisionID || record.Boots != served.RevisionID {
			return fmt.Errorf("LastApply() = %+v, want revision %s refused at epoch %d, serving and booting on %s",
				record, refused.RevisionID, refused.Epoch, served.RevisionID)
		}
		return nil
	}
	eventually(t, "the node refusing the second revision", func() error {
		fleet, err := client.Fleet(ctx)
		if err != nil {
			return err
		}
		return want(fleet.LastApply())
	})
	active, err := client.ActiveConfig(ctx)
	if err != nil || active.RevisionID != refused.RevisionID {
		t.Fatalf("ActiveConfig() = %s, %v; want the fleet's revision %s, which the node refused", active.RevisionID, err, refused.RevisionID)
	}
	running, err := client.Revision(ctx, served.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if running.ID != served.RevisionID || !bytes.Contains(running.Document, []byte("${"+kept+"}")) {
		t.Fatalf("Revision(%s) = {%s, %s}, want its document, referencing ${%s}", served.RevisionID, running.ID, running.Document, kept)
	}

	// Restarted, the node starts on the revision it ran, and refuses the
	// fleet's again: it is ready, and says it runs the first.
	if err := e.Restart(t); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the restarted node ready", func() error { return client.Ready(ctx) })
	eventually(t, "the restarted node refusing the second revision again", func() error {
		fleet, err := client.Fleet(ctx)
		if err != nil {
			return err
		}
		return want(fleet.LastApply())
	})

	// Without the value the revision it runs references, it does not start
	// at all: the value the secret sync keeps for it.
	if _, err := client.DeleteSecret(ctx, kept); err != nil {
		t.Fatal(err)
	}
	if err := e.Restart(t); err == nil {
		t.Fatalf("the node started on revision %s with %s gone from its secret store, which its document references",
			served.RevisionID, kept)
	}
}

// TestEngineANodeThatRefusedEveryRevisionRunsNoCompany pins the row the secret
// sync reads as a node that runs no company, an attempt recorded (Status) and
// no revision served or started on (Serving and Boots ""), and what such a node
// boots on. keepServing's branch for that row (internal/controller) lets every
// delete from the store go ahead beside the node, because the node needs
// nothing of the store but the fleet's revision. That holds only while the
// engine keeps both halves: a node that has run nothing and refuses the fleet's
// revision records the attempt, rather than staying the zero record of a node
// that has not said, which keeps every value for as long as it runs; and
// restarted, with no revision it applied, it boots on nothing, not on the
// fleet's revision it refused, and tries that one again. This is the node that
// joined on an empty store, which one engine can be; a seat pod, whose store is
// scratch, boots on nothing at every start whatever it ran.
func TestEngineANodeThatRefusedEveryRevisionRunsNoCompany(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t)
	client := e.Client()
	const unset = "ENGINETEST_E2B_NEVER_SET"

	// The first revision references a value the store does not hold,
	// which the dry run accepts and the apply refuses.
	refused, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: enginetest.SandboxCompany(unset), Summary: "enginetest: the revision it refuses", CreateOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runsNone := func() error {
		fleet, err := client.Fleet(ctx)
		if err != nil {
			return err
		}
		if record := fleet.LastApply(); record.RevisionID != refused.RevisionID || record.Epoch != refused.Epoch ||
			record.Status != engine.ApplyError || record.Serving != "" || record.Boots != "" {
			return fmt.Errorf("LastApply() = %+v, want revision %s refused at epoch %d, serving and booting on none",
				record, refused.RevisionID, refused.Epoch)
		}
		health, err := client.Health(ctx)
		if err != nil {
			return err
		}
		if health.Configured {
			return fmt.Errorf("Health() = %+v, want a node that runs no company", health)
		}
		return nil
	}
	eventually(t, "the node refusing the first revision", runsNone)

	// Restarted, it starts, which it would not on the revision it refused,
	// runs nothing, and says so beside its attempt at that revision again.
	if err := e.Restart(t); err != nil {
		t.Fatalf("the node that runs no company did not start again: %v", err)
	}
	eventually(t, "the restarted node running no company", runsNone)
}

// founderCompany is MinimalCompany with a human seat, founder, which a test
// puts a person on.
const founderCompany = enginetest.MinimalCompany + `  - name: Founder
    handle: founder
    kind: human
`

// TestEngineRefusesToTakeAHeldSeat pins what the operator reports as
// SeatHeld: once a person holds a human seat, a document that drops the seat,
// or makes it an agent's, is refused, a dry run included, as a
// *SeatHeldError that names the seat and a person on it, and the engine keeps
// the revision it runs.
func TestEngineRefusesToTakeAHeldSeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := enginetest.Start(t, enginetest.WithCompany([]byte(founderCompany)))
	client := e.Client()
	eventually(t, "Ready() on the seeded company", func() error { return client.Ready(ctx) })
	seeded, err := client.ActiveConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.AddPerson(t, "founder@acme.example", seatFounder)

	agent := strings.Replace(founderCompany, "    kind: human\n", "    kind: agent\n    goal: \"Found the company\"\n", 1)
	for _, c := range []struct{ name, document string }{
		{"a document without the seat", enginetest.MinimalCompany},
		{"a document that makes it an agent's", agent},
	} {
		for _, dryRun := range []bool{true, false} {
			_, err := client.ApplyConfig(ctx, engine.ApplyRequest{
				Document: []byte(c.document), Summary: "enginetest: " + c.name, DryRun: dryRun, IfMatch: seeded.RevisionID,
			})
			var held *engine.SeatHeldError
			if !errors.As(err, &held) {
				t.Fatalf("%s (dry run %v) = %v, want a *SeatHeldError", c.name, dryRun, err)
			}
			want := map[string]engine.SeatHolder{seatFounder: {Kind: engine.HolderPerson, Stage: stageActive}}
			if !maps.Equal(held.Held, want) {
				t.Fatalf("%s (dry run %v) names %v as held, want %v", c.name, dryRun, held.Held, want)
			}
		}
	}
	if active, err := client.ActiveConfig(ctx); err != nil || active.RevisionID != seeded.RevisionID {
		t.Fatalf("ActiveConfig() = %+v, %v; want the seeded revision %s, unchanged", active, err, seeded.RevisionID)
	}
}
