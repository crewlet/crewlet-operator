package engine_test

import (
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

// TestEngineHealthReportsTheAppliedEpoch pins what the config reconciler
// waits for after a write: the engine applies the revision asynchronously,
// never reading a diverged posture while it does, and its /health then
// reports it applied, converged (posture serve) and ok, with an applied epoch
// that reaches the epoch the write returned; and the node's record in /fleet
// names that epoch, its revision and a clean outcome.
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
	fleet, err := client.Fleet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if record, want := fleet.LastApply(), (engine.ApplyRecord{Epoch: applied.Epoch, RevisionID: applied.RevisionID, Status: engine.ApplyOK}); record != want {
		t.Fatalf("LastApply() = %+v, want %+v", record, want)
	}
}

// TestEngineFleetNamesTheSeatsHolder pins the read of /fleet against the
// engine: the answering node, its roles, broker and labels, the seat it holds
// because its labels match the seat's placement, and a seat whose placement
// no node matches, which no node holds and the engine calls unplaceable.
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
// with its provenance and author, a delete says whether it removed anything.
func TestEngineSecretsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := enginetest.Start(t).Client()
	const name = "K8S_SECRET_SLACK_BOTTOKEN_ABCD"

	if err := client.PutSecret(ctx, name, []byte("xoxb-not-a-real-token\n"), "operator"); err != nil {
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
	if s := listed[i]; s.Source != "operator" || s.UpdatedBy != engine.TokenLogin(enginetest.OperatorTokenID) || s.KeyID == "" ||
		s.UpdatedAt.IsZero() {
		t.Fatalf("listed secret = %+v, want source operator, written by %s, sealed and timestamped",
			s, engine.TokenLogin(enginetest.OperatorTokenID))
	}

	for _, want := range []bool{true, false} {
		removed, err := client.DeleteSecret(ctx, name)
		if err != nil || removed != want {
			t.Fatalf("DeleteSecret() = %v, %v, want %v, nil", removed, err, want)
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
	if err := access.PutSecret(ctx, "ENGINETEST_ROTATED", []byte("rotated"), ""); err != nil {
		t.Fatalf("writing the secret store with the access token: %v", err)
	}

	second, err := operator.ApplyConfig(ctx, engine.ApplyRequest{Document: edited, Summary: "enginetest: the writer edits", IfMatch: first.RevisionID})
	if err != nil {
		t.Fatalf("an edit with the operator token: %v", err)
	}
	// Waited for rather than read once: the write marks its revision this
	// node's active one, and the node's apply of the first revision, still
	// finishing, can mark that one active again a moment later (Crewlet's
	// engine/reconcile.go, holdLocalCopy, compares no epochs), until the
	// node applies the second and marks it active once more.
	eventually(t, "the engine running the operator's edit", func() error {
		active, err := operator.ActiveConfig(ctx)
		if err != nil {
			return err
		}
		if active.RevisionID != second.RevisionID {
			return fmt.Errorf("it runs revision %s, want %s", active.RevisionID, second.RevisionID)
		}
		return nil
	})
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
