package engine_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// These tests run the client against the real engine at the pinned version
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
// and its /health reports it applied, converged (posture serve) and ok, with
// an applied epoch that reaches the epoch the write returned; and the node's
// record in /fleet names that epoch, its revision and a clean outcome.
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
	eventually(t, "the engine reporting the write's epoch applied", func() error {
		h, err := client.Health(ctx)
		switch {
		case err != nil:
			return err
		case h.AppliedEpoch < applied.Epoch:
			// Its posture may read isolated meanwhile: the engine counts
			// the attempt before it makes it (see LastApply).
			return fmt.Errorf("applied epoch %d is behind the write's %d", h.AppliedEpoch, applied.Epoch)
		case h.Status != "ok" || !h.Configured || h.Posture != engine.PostureServe:
			return fmt.Errorf("Health() = %+v, want status ok, configured, posture serve", h)
		}
		return nil
	})
	record, err := client.LastApply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (engine.ApplyRecord{Epoch: applied.Epoch, RevisionID: applied.RevisionID, Status: engine.ApplyOK}); record != want {
		t.Fatalf("LastApply() = %+v, want %+v", record, want)
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
	if s := listed[i]; s.Source != "operator" || s.UpdatedBy != enginetest.OperatorTokenID || s.KeyID == "" ||
		s.UpdatedAt.IsZero() {
		t.Fatalf("listed secret = %+v, want source operator, written by %s, sealed and timestamped", s, enginetest.OperatorTokenID)
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
	e := enginetest.Start(t)
	if _, err := engine.New(e.URL, "not-"+e.OperatorToken).ListSecrets(ctx); !errors.Is(err, engine.ErrUnauthorized) {
		t.Fatalf("ListSecrets() with a wrong token = %v, want ErrUnauthorized", err)
	}
	if _, err := engine.New(e.URL, e.AccessToken).ListSecrets(ctx); err != nil {
		t.Fatalf("ListSecrets() with the access token = %v", err)
	}
}
