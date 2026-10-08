# ADR-0000 — The decision, as an imperative sentence

- **Status:** proposed | accepted | superseded by ADR-NNNN
- **Authority:** `internal/somepackage` — the one package or page whose doc
  holds the detail
- **Enforced-by:** `internal/somepackage.TestTheRuleHolds`, a compile error,
  or `nothing`

<!--
Copy this file, take the next free number, name it after the decision
(NNNN-a-short-imperative-title.md), add it to the table in README.md, and
delete this comment.

Before writing anything: is this decision inside ONE package? Then it belongs
in that package's doc comment, not here. This directory is for decisions that
bind several packages, or the API and the controllers together.
-->

## Context

What forces the decision: the facts about Kubernetes, the Crewlet engine or
the API's consumers that make it necessary, and what goes wrong without it.
Keep it to what a reader needs to judge the decision; the detail stays at the
authority.

## Decision

One or two paragraphs. State what is decided and what it forbids, in terms a
reader can act on, and name the type, package or file that carries it.

## Consequences

What the decision costs and what it rules out, including the obvious
alternative and why it is wrong here. Name what it does **not** decide, so
the record is not cited for a decision it never made.
