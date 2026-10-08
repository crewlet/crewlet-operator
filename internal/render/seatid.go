package render

import "github.com/google/uuid"

// seatIDNamespace is the UUID namespace Crewlet derives every seat's identity
// in (agentIDNamespace in Crewlet's internal/org). Crewlet has frozen it: a
// different value would derive different ids and orphan every seat's memory.
// It is restated here because Crewlet's package is internal and cannot be
// imported; TestSeatIDMatchesTheEngine pins the result against ids read from
// a running engine.
var seatIDNamespace = uuid.MustParse("c1ea9c6e-3f5d-4c9c-9e9f-1d2c3b4a5e6f")

// SeatID is the identity Crewlet derives for a seat (its ADR-0013): a UUIDv5
// over "<company name>:<handle>". Every engine node computes it from the
// configuration alone, which is why the company name and a seat's handle can
// never change; the operator publishes it in Seat.status.seatID so that no
// caller has to derive it again.
//
// companyName is Company.spec.companyName exactly as written, and handle is
// the seat's handle (spec.handle, or its metadata.name). Both are non-empty
// in any object the API server admitted.
func SeatID(companyName, handle string) string {
	return uuid.NewSHA1(seatIDNamespace, []byte(companyName+":"+handle)).String()
}
