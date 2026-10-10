# ADR-0003 — Own a seat's pause only where its Seat sets spec.paused

- **Status:** accepted
- **Authority:** `internal/controller` — `config_pause.go`
- **Enforced-by:** `internal/controller.TestSyncPauseChangesNoPauseItDoesNotOwn`,
  `internal/controller.TestAnOwnedPauseFollowsTheRecordNotTheRoster`,
  and the config suite's spec that pauses and resumes a Seat as
  `spec.paused` asks

## Context

Crewlet pauses a seat outside its company document: the pause is a record of
the engine's coordination store, which a person takes and lifts from the
dashboard (the operator catalogue's `pause_seat` and `resume_seat` tools)
when an agent misbehaves, and which the engine deliberately keeps out of the
document so that a revision never takes or lifts one. The operator's
management policies are about the document: under `Enforce` the operator is
its only writer. A Seat can ask for a pause too (`spec.paused`), so the
operator and the people in the dashboard can both write one record, and the
API has to say whose a pause is.

## Decision

A Seat that sets `spec.paused` (to `true` or to `false`) owns its seat's
pause under the Company's `Enforce` policy: the operator pauses or resumes
the seat to match on every look at the engine, whatever became of the
document, and undoes a change made in the dashboard with a `Drifted` event on
the Seat. A Seat that leaves `spec.paused` unset owns nothing, under any
policy: the operator reports the pause (the `Paused` condition and
`status.pause`) and never changes it. `Observe` and `Seed` change no pause.
The operator changes pauses only through the two tools, over MCP, with its
own token, and calls the one a Seat that owns its pause asks for on every
look rather than when the engine's roster shows a difference: both tools
change nothing on a seat already in the state asked for and answer with the
coordination store's record, while the roster is a projection moved by the
announcement of each change, which a node that missed one gets wrong. The
roster is read to report the pauses no Seat owns and to know which seats the
engine runs. A call the engine may or may not have recorded, or refused, is
made again ten seconds later.

## Consequences

A person can always pause an agent of an operator-run company, and the pause
holds unless the Seat itself says otherwise. The obvious alternative, reading
an unset `spec.paused` as `false` under `Enforce` as the rest of the spec is
read, would lift every dashboard pause within minutes: the brake would stop
working exactly on the companies the operator runs. The cost is that the
same field has two meanings (set: the operator's; unset: the people's), and
that a platform that wants to own every pause has to set the field on every
Seat. Two gaps follow from a pause being a record of the engine's rather than
part of the document. A Seat created with `paused: true` runs from the moment
the revision that adds it is applied until the operator's next look pauses
it, two seconds at most while the operator polls that apply; a re-created
Seat whose durable inbox kept messages can start on them in that window. And
a pause outlives its Seat: deleting a Seat that owns a pause leaves the
record, which never expires, so a later Seat of the same handle starts
paused while it leaves `spec.paused` unset. The operator does not lift it on
deletion: a Seat is gone from the document before its pause could be
resumed, and a resume of a seat about to go would let it take work in
between. A Seat created with `paused: false` lifts it. A dashboard change to an owned pause runs until the operator's next
look, at most the reconciler's resync interval. This record does not decide
how pauses are read once a company runs more than one data pod; the roster
the operator reads is the engine's own, whichever pod answers.
