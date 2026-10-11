package render

import "testing"

// TestSeatIDMatchesTheEngine pins SeatID to ids read from the engine itself:
// each was read from GET /agents of a Crewlet build, running a
// company of that name with seats of those handles. A difference would give
// every Seat a status.seatID that names no seat in the engine.
func TestSeatIDMatchesTheEngine(t *testing.T) {
	const (
		nimbus = "Nimbus"
		// A name with spaces, a colon and characters outside ASCII is
		// hashed as its UTF-8 bytes, exactly as written.
		unicode = "Ünïcode Labs: GmbH"
	)
	for _, v := range []struct{ company, handle, id string }{
		{nimbus, seatCTO, "3fc60599-901a-5bd8-8e7b-7df9d04727ad"},
		{nimbus, seatAna, "38484498-8677-576c-b91d-73a700970e0c"},
		{nimbus, "a-1", "3beb30c2-7caa-5b7c-bbe6-97c401ec2433"},
		{unicode, seatAna, "89fa3d07-effa-5b84-91dc-ad7351e534e6"},
		{unicode, seatCTO, "ef050178-a50e-5ca9-8784-16b4e9c78db3"},
	} {
		if got := SeatID(v.company, v.handle); got != v.id {
			t.Errorf("SeatID(%q, %q) = %s, want %s", v.company, v.handle, got, v.id)
		}
	}
}
