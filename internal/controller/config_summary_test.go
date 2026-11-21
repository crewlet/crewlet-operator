package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
	"github.com/crewlet/crewlet-operator/internal/secretref"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// TestTheOperatorsSummariesFitWhatTheActivationPointerCarries holds the
// summaries the operator composes, at their widest, to what the real engine
// takes. The engine refuses a summary past what the activation pointer
// carries (crewlet's coord.MaxSummaryBytes, measured as the pointer encodes
// it) with 400 summary_too_long, and never cuts it, so a composer that crossed
// it would leave the company with no write the engine takes. Each composer is
// driven to its widest, every name at the most the API server lets it be and
// more objects than it lists, and sent on its own route: a write's summary
// with a dry run, which checks it as the write does, and a re-activation's
// with a reload. A summary at the bound is taken and one a byte past it is
// refused, so the engine this runs holds summaries to the bound its docs name.
func TestTheOperatorsSummariesFitWhatTheActivationPointerCarries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	client := enginetest.Start(t, enginetest.WithCompany([]byte(enginetest.MinimalCompany))).Client()
	// A change, so the dry run checks the write rather than answering that it
	// changes nothing.
	changed := []byte(enginetest.MinimalCompany + "  - name: Bob\n    goal: \"Review the work\"\n")
	dryRun := func(summary string) error {
		_, err := client.ApplyConfig(ctx, engine.ApplyRequest{Document: changed, Summary: summary, DryRun: true})
		return err
	}

	// A Secret's reference is the widest object a write's summary names:
	// "Secret " and a namespace of 63 and a name of 253, past any other kind's
	// name of at most 253. A thousand of them, eight listed and the rest
	// counted; a further digit of the count is a byte against a kilobyte of
	// room. The revision it replaces is a UUID, as every engine names one.
	var refs []secretref.Key
	var written []string
	for i := range 1000 {
		k := secretref.Key{Namespace: fmt.Sprintf("%03d", i) + strings.Repeat("n", 60), Name: strings.Repeat("s", 253),
			Key: "token"}
		refs = append(refs, k)
		written = append(written, secretsync.StoreName(k))
	}
	run := &configRun{company: &crewletaiv1alpha1.Company{Namespace: "ns", Name: strings.Repeat("c", 47)},
		doc: &render.Document{Refs: refs}, unsealed: "ffffffff-ffff-ffff-ffff-ffffffffffff"}
	write := run.summary(&engine.ActiveConfig{}, nil, written)
	if !strings.Contains(write, "stored without a seal") || !strings.HasSuffix(write, " and 992 more") {
		t.Fatalf("the write's summary %q is not the composer's widest", write)
	}
	t.Logf("a write's widest summary is %d bytes", len(write))
	if err := dryRun(write); err != nil {
		t.Errorf("the engine refused the write's widest summary, %d bytes: %v", len(write), err)
	}

	// A re-activation lists four names of each kind, each a store name at its
	// longest, and counts the rest.
	var names []string
	for i := range 5 {
		name := secretsync.StoreName(secretref.Key{Namespace: fmt.Sprintf("ns%d", i), Name: strings.Repeat("s", 253),
			Key: strings.Repeat("k", 253)})
		if len(name) != secretsync.MaxStoreNameLength {
			t.Fatalf("store name %q is %d bytes, want the longest, %d", name, len(name), secretsync.MaxStoreNameLength)
		}
		names = append(names, name)
	}
	reactivation := reactivationSummary(storeChanges{withdrawn: names, written: names})
	t.Logf("a re-activation's widest summary is %d bytes", len(reactivation))
	if _, err := client.ReloadConfig(ctx, reactivation); err != nil {
		t.Errorf("the engine refused the re-activation's widest summary, %d bytes: %v", len(reactivation), err)
	}

	if err := dryRun(strings.Repeat("a", 4096)); err != nil {
		t.Errorf("the engine refused a summary of 4096 bytes, the bound: %v", err)
	}
	var apiErr *engine.APIError
	if err := dryRun(strings.Repeat("a", 4097)); !errors.As(err, &apiErr) ||
		apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "summary_too_long" {
		t.Errorf("a summary a byte past the bound = %v, want 400 summary_too_long", err)
	}
}
