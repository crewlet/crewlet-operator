# ADR-0001 — Name every store entry the operator owns by its Secret reference

- **Status:** accepted
- **Authority:** `internal/secretsync` — `StoreName`, `Owns` and the package
  doc
- **Enforced-by:** `internal/render.TestCompanyRefsAreExactlyThePointers`,
  `internal/secretsync.TestStoreNameIsDeterministic`,
  `internal/secretsync.TestOwnsOnlyTheOperatorsNames`,
  `internal/secretsync.TestSyncCollectsOnlyTheOperatorsNames`

## Context

A credential never appears in the company document. Each credential position
holds a `${NAME}` reference, the engine resolves it from its encrypted secret
store, and the operator writes the Secret's value into the store under NAME.
Three packages meet at NAME: `internal/render` writes it into the document,
`internal/secretsync` writes and deletes the store entry, and the config
reconciler (`internal/controller`) keeps the entries the running document
still references until the next document is applied. If they disagree on a
name, the engine resolves a reference to an entry nobody wrote, or the sync
deletes one the engine still reads. The store is also shared with whatever
else writes into it (a person with `crewlet secrets set`, a vendor's
provisioning), so the operator has to know which entries are its own.

## Decision

The name of the store entry holding Secret key `k` is
`secretsync.StoreName(k)`, and nothing else: a pure function of the
reference (namespace, Secret, key), `K8S_` followed by the readable Secret
and key and a hash of the whole reference. `render.Input.StoreName` is that
function in every caller outside tests. The operator owns exactly the names
`secretsync.Owns` accepts, which is the shape `StoreName` produces, and
deletes no other entry. A change to `StoreName` that renames an existing
entry is a breaking change and is called out as one.

## Consequences

Renders are reproducible: the same objects give the same document on every
operator replica and version, which is what lets the reconciler compare the
rendered document with the running one. The cost is that the `K8S_` prefix is
reserved: an entry a person writes in that shape is deleted once no
reference produces it. The obvious alternative, names the operator allocates
and records (a counter, a random id), would need that record to survive the
operator and to be read before every render, and two replicas could allocate
differently. This record does not decide which document positions are
credentials; that is the API's (`docs/concepts/credentials.md`).
