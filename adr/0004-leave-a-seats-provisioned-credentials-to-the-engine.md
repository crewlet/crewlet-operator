# ADR-0004 — Leave a seat's provisioned credentials to the engine, under a name the operator derives

- **Status:** accepted
- **Authority:** `internal/secretsync` — `ProvisionedName` and the package
  doc; `docs/concepts/credentials.md` for the API's side
- **Enforced-by:** `internal/secretsync.TestProvisionedNameIsTheEnginesAlone`,
  `internal/secretsync.TestProvisionedNameIsDeterministic`,
  `internal/render.TestCompanyRefsAreExactlyThePointers`,
  `internal/render.TestEveryReferenceInADocumentIsTheOperators`

## Context

Under its own provisioning, Crewlet creates each agent seat's account at a
vendor (a GitLab service account, a Mattermost bot, a Datadog service
account, an Atlassian Cloud account) and mints the account's credential into
the engine's secret store, under whatever `${NAME}` the seat's configuration
holds where the engine looks for one: a tool server's variable in the seat's
`mcp_env`, its `sandbox.env`, its Mattermost `bot_token`. Several of those
passes re-mint a value that does not authenticate as the seat's own account.
The operator, for its part, writes every credential it renders from a
Secret into the store under `secretsync.StoreName` and deletes the entries
of that shape nothing references (ADR-0001). Pointed at a store entry the
operator fills, the engine would overwrite the operator's value, and the
operator, which never reads a value back, would overwrite the engine's on
the Secret's next change. The value the engine mints exists in no Secret at
all, so a `SecretKeyReference` cannot name it.

## Decision

A seat names a credential the engine provisions as `provisioned: {name}`
(`v1alpha1.ProvisionedCredential`) wherever it may name a Secret key for its
own credentials. The renderer writes it as
`${secretsync.ProvisionedName(handle, name)}`: `PROVISIONED_`, the readable
handle and name, and a hash of both. The operator never writes, rewrites or
deletes an entry of that shape; `secretsync.Owns` refuses every one, so a
sync cannot collect it, and the rendered document lists the names in
`Document.Provisioned`, apart from the `Refs` the sync fills. A Unit's
credentials, which every member shares, cannot be provisioned, and a seat
names a provisioned value only while some Integration of its company has
`provisioning: Engine`.

## Consequences

A minted credential survives every render, restart and upgrade of the
operator, because its name is a pure function of the seat's handle (which
never changes) and of a name the Seat states, as `StoreName` is of a Secret
reference. One provisioned name is one value wherever a seat names it, which
is how a seat's GitLab tool server and its sandbox share one token. The cost
is that `PROVISIONED_` is reserved in the same way `K8S_` is, and that the
engine's own conventions decide which of a seat's provisioned values it
fills: a provisioned value in a place no provisioning pass reads stays empty,
which the vendor guides say where to put. The obvious alternative, rendering
a Secret reference and leaving it to the engine to overwrite, makes two
writers of one entry that cannot see each other's writes. This record does
not decide which vendors the engine provisions, nor how it reconciles their
accounts; that is Crewlet's.
