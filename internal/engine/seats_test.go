package engine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
)

// seatAna is the handle of MinimalCompany's one agent seat.
const seatAna = "ana"

// pausable starts an engine that runs MinimalCompany and returns its client
// once the seat exists.
func pausable(t *testing.T) *engine.Client {
	t.Helper()
	ctx := context.Background()
	client := enginetest.Start(t).Client()
	if _, err := client.ApplyConfig(ctx, engine.ApplyRequest{
		Document: []byte(enginetest.MinimalCompany), Summary: "enginetest: pauses", CreateOnly: true,
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the engine listing its seat", func() error {
		pauses, err := client.SeatPauses(ctx)
		if err != nil {
			return err
		}
		if _, ok := pauses[seatAna]; !ok {
			return fmt.Errorf("SeatPauses() = %v, want seat %s", pauses, seatAna)
		}
		return nil
	})
	return client
}

// TestEnginePausesAndResumesASeat pins the pause tools to the engine: a pause
// the operator takes is the fleet's record and the engine's roster reports
// it, a second pause and a second resume change nothing, a stop added to a
// pause amends it, and a resume lifts it.
func TestEnginePausesAndResumesASeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := pausable(t)

	before, err := client.SeatPauses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before[seatAna] != (engine.SeatPause{}) {
		t.Fatalf("SeatPauses()[%s] before any pause = %+v, want not paused", seatAna, before[seatAna])
	}

	const reason = "Rolling the deploy pipeline back"
	start := time.Now().Add(-time.Second)
	paused, err := client.PauseSeat(ctx, seatAna, reason, false)
	if err != nil {
		t.Fatal(err)
	}
	if !paused.Changed || !paused.Paused || paused.Reason != reason || paused.StopRunning ||
		paused.By != enginetest.OperatorTokenID || paused.At.Before(start) {
		t.Fatalf("PauseSeat() = %+v, want a changed pause by %s with the reason, taken now", paused, enginetest.OperatorTokenID)
	}
	eventually(t, "the roster reporting the pause", func() error {
		pauses, err := client.SeatPauses(ctx)
		if err != nil {
			return err
		}
		if got := pauses[seatAna]; got != paused.SeatPause {
			return fmt.Errorf("SeatPauses()[%s] = %+v, want %+v", seatAna, got, paused.SeatPause)
		}
		return nil
	})

	again, err := client.PauseSeat(ctx, seatAna, "Another reason", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed || again.SeatPause != paused.SeatPause {
		t.Fatalf("a second PauseSeat() = %+v, want the first pause, unchanged: %+v", again, paused.SeatPause)
	}

	stopped, err := client.PauseSeat(ctx, seatAna, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Changed || !stopped.StopRunning || stopped.Reason != reason {
		t.Fatalf("PauseSeat(stopRunning) of a pause without a stop = %+v, want the pause amended with the stop", stopped)
	}

	resumed, err := client.ResumeSeat(ctx, seatAna)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Changed || resumed.SeatPause != (engine.SeatPause{}) {
		t.Fatalf("ResumeSeat() = %+v, want a changed resume", resumed)
	}
	eventually(t, "the roster reporting the resume", func() error {
		pauses, err := client.SeatPauses(ctx)
		if err != nil {
			return err
		}
		if got := pauses[seatAna]; got.Paused {
			return fmt.Errorf("SeatPauses()[%s] = %+v, want not paused", seatAna, got)
		}
		return nil
	})
	if again, err := client.ResumeSeat(ctx, seatAna); err != nil || again.Changed || again.Paused {
		t.Fatalf("a second ResumeSeat() = %+v, %v; want nothing changed", again, err)
	}
}

// TestEngineRefusesAPauseItCannotTake pins the refusals the reconciler
// reports: a handle the company has no agent seat of, and a reason past the
// engine's bound, which is the API's bound on spec.pauseReason.
func TestEngineRefusesAPauseItCannotTake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := pausable(t)

	for _, c := range []struct {
		name   string
		call   func() (engine.PauseResult, error)
		phrase string
	}{
		{
			name:   "a seat the company does not have",
			call:   func() (engine.PauseResult, error) { return client.PauseSeat(ctx, "nobody", "", false) },
			phrase: "nobody",
		},
		{
			name:   "a resume of a seat the company does not have",
			call:   func() (engine.PauseResult, error) { return client.ResumeSeat(ctx, "nobody") },
			phrase: "nobody",
		},
		{
			name: "a reason past the bound",
			call: func() (engine.PauseResult, error) {
				return client.PauseSeat(ctx, seatAna, strings.Repeat("é", engine.MaxPauseReasonRunes+1), false)
			},
			phrase: fmt.Sprint(engine.MaxPauseReasonRunes),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.call()
			var refused *engine.ToolError
			if !errors.As(err, &refused) || !strings.Contains(refused.Message, c.phrase) {
				t.Fatalf("= %+v, %v; want a *ToolError naming %q", got, err, c.phrase)
			}
		})
	}

	// The bound itself is admitted, counted in characters.
	if _, err := client.PauseSeat(ctx, seatAna, strings.Repeat("é", engine.MaxPauseReasonRunes), false); err != nil {
		t.Fatalf("a reason of exactly %d characters: %v", engine.MaxPauseReasonRunes, err)
	}
}
