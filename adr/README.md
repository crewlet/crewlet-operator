# Architecture decision records

A decision that binds **more than one package** — or the API and the
controllers together — is recorded here. A decision that lives inside one
package is recorded in that package's doc comment, where `go doc` puts it
beside the code, and gets no record here. That single rule keeps this
directory from becoming a second, drifting copy of the package docs.

## The fields

| Field | What it is for |
|---|---|
| **Status** | `proposed`, `accepted`, or `superseded by ADR-NNNN`. A superseded record keeps its number and its text; numbers are never reused, and a record is never edited into a different decision, because the reasoning somebody acted on has to stay readable. |
| **Authority** | The one package or page whose own doc holds the detail. The record states the decision and why; the detail stays at the authority, and the authority's doc names the record back (`ADR-NNNN`), so the decision is in front of whoever is about to change the code. |
| **Enforced-by** | A named test, a compile error, or the literal `nothing`. A rule held by prose alone decays; this field makes it visible which rules have a gate. `nothing` is an allowed and honest answer, but it has to be written down. |
| **Context** | The forces that make the decision necessary. |
| **Decision** | What is decided and what it forbids. |
| **Consequences** | What it costs, the obvious alternative and why it is wrong here, and what the record does not decide. |

## Writing one

Copy [`0000-template.md`](0000-template.md), take the next free number, name
the file `NNNN-a-short-imperative-title.md`, add the record's id to its
authority's doc, and add a row below. A record is proposed in a pull request
like any other change.

## The records

None yet.

| # | Decision | Enforced by |
|---|---|---|
