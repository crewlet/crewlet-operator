package render

import (
	"slices"
	"testing"
)

// TestPlacementOfARenderedDocument reads back the pins Company rendered:
// every pool a seat runs in, data included, and every PerSeat seat, and
// nothing for a person.
func TestPlacementOfARenderedDocument(t *testing.T) {
	doc, err := Company(companyCaseNamed(t, caseSeatPools).input(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := PlacementOf(doc.YAML)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{poolBatch, poolData, poolGeneral}; !slices.Equal(got.Pools, want) {
		t.Errorf("pools = %v, want %v", got.Pools, want)
	}
	if want := []string{seatAna}; !slices.Equal(got.Seats, want) {
		t.Errorf("seats = %v, want %v", got.Seats, want)
	}
}

// TestPlacementOfAnEngineDocument reads a document the engine owns (under
// Observe or Seed): roles in units nested to any depth count, a role pinned
// by other labels or by a node pins nothing the operator runs, and a pool
// two roles share is named once.
func TestPlacementOfAnEngineDocument(t *testing.T) {
	document := []byte(`
name: Nimbus
roles:
  - name: CEO
    handle: ceo
    placement: {labels: {crewlet.ai/pool: general}}
  - name: Ops
    handle: ops
    placement: {node: nimbus-data-0}
units:
  - name: Engineering
    roles:
      - name: Lead
        handle: lead
        placement: {labels: {crewlet.ai/seat: lead}}
    children:
      - name: Platform
        roles:
          - name: SRE
            handle: sre
            placement: {labels: {crewlet.ai/pool: general, zone: a}}
          - name: Writer
            handle: writer
            placement: {labels: {crewlet.ai/pool: batch}}
          - name: Person
            handle: person
`)
	got, err := PlacementOf(document)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"batch", "general"}; !slices.Equal(got.Pools, want) {
		t.Errorf("pools = %v, want %v", got.Pools, want)
	}
	if want := []string{"lead"}; !slices.Equal(got.Seats, want) {
		t.Errorf("seats = %v, want %v", got.Seats, want)
	}
	if _, err := PlacementOf([]byte("roles: {")); err == nil {
		t.Error("a document that is not YAML was read")
	}
}

// TestHumanSeatsOfReadsEveryUnit reads the human seats of a document the
// engine owns, as IdentityClaimed names them: the handles of the roles of
// kind human, at the top level and in units nested to any depth, sorted, and
// no agent seat, whether its kind is written or left to the default.
func TestHumanSeatsOfReadsEveryUnit(t *testing.T) {
	document := []byte(`
name: Nimbus
roles:
  - name: Founder
    handle: founder
    kind: human
  - name: CEO
    handle: ceo
units:
  - name: Engineering
    roles:
      - name: Lead
        handle: lead
        kind: agent
    children:
      - name: Platform
        roles:
          - name: Bea
            handle: bea
            kind: human
`)
	got, err := HumanSeatsOf(document)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bea", "founder"}; !slices.Equal(got, want) {
		t.Fatalf("HumanSeatsOf() = %v, want %v", got, want)
	}
	if got, err := HumanSeatsOf([]byte("name: Acme\nroles:\n  - name: Ana\n    handle: ana\n")); err != nil || got != nil {
		t.Fatalf("HumanSeatsOf() of a company of agents = %v, %v; want none", got, err)
	}
}
