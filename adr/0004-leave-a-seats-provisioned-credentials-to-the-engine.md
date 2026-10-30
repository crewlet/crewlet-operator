# ADR-0004: Leave a seat's provisioned credentials to the engine, under the engine's own name

- **Status:** accepted; that a seat names a provisioned value only while its
  vendor's Integration provisions is superseded by ADR-0005
- **Authority:** `internal/secretsync` — `ProvisionedName` and the package
  doc; `docs/concepts/credentials.md` for the API's side
- **Enforced-by:** `internal/secretsync.TestProvisionedNameIsTheEnginesAlone`,
  `internal/secretsync.TestProvisionedNameIsDeterministic`,
  `internal/render.TestMirroredEngineDeclarationsAreUnchanged` (envref's
  `SealedName`), `internal/render.TestCompanyRefsAreExactlyThePointers`,
  `internal/render.TestEveryReferenceInADocumentIsTheOperators`

## Context

Under its own provisioning, Crewlet creates the account of each agent seat
that is on a vendor (a GitLab service account, a Mattermost bot, a Datadog
service account, an Atlassian Cloud account) and mints the account's
credential into its secret store. It seals that credential under a name it
derives for the seat, `<VENDOR>_<FIELD>_<HANDLE>`, and under no other: a
document that pointed the credential at a variable of its own choosing let
one seat's provisioning seal over the company's organization key, so Crewlet
no longer reads where to seal from the document at all, and hands the
credential to the vendor's tool server itself. The operator, for its part,
writes every credential it renders from a Secret into the store under
`secretsync.StoreName` and deletes the entries of that shape nothing
references (ADR-0001). The value the engine mints exists in no Secret, so a
`SecretKeyReference` cannot name it, and a seat's executor runs, which get no
tool server's environment, still need some of them, such as the GitLab token
git uses in the seat's box.

## Decision

A seat names a credential the engine mints, in its `spec.sandbox.env`, as
`provisioned: {vendor, field}` (`v1alpha1.ProvisionedCredential`): GitLab's
Token, Atlassian's Token and Email, Datadog's AppKey and Mattermost's
BotToken. The renderer writes it as `${secretsync.ProvisionedName(vendor,
field, handle)}`, a copy of the engine's own derivation (envref's
`SealedName`), which the mirror table fingerprints, so a change to the
engine's naming fails the operator's next run rather than pointing seats at
entries nothing fills. The operator never writes, rewrites or deletes such an
entry: every one starts with the vendor's name, never `K8S_`, so
`secretsync.Owns` refuses it and a sync cannot collect it, and the rendered
document lists the names in `Document.Provisioned`, apart from the `Refs` the
sync fills. Nothing else of the seat names a minted credential: the engine
hands each to the vendor's tool server and its own tools itself, and refuses
one a seat names in `spec.mcp` or `spec.integrations` for a vendor it
provisions.

## Consequences

A minted credential survives every render and restart of the operator,
because its name is a pure function of the vendor, the field and the seat's
handle, which never changes. The cost is a copied function, which the mirror
table holds to the engine's, and that the engine's own conventions decide
which of a seat's credentials it mints: a provisioned value of a vendor whose
Integration does not provision stays empty, which the Seat reports
(`NoProvisioner`, ADR-0005). The obvious alternative, a name the operator
derives and the engine mints into because the document points there, is what
let a seat's sink be a company variable, and Crewlet refuses it. This record
does not decide which vendors the engine provisions, nor how it reconciles
their accounts; that is Crewlet's.
