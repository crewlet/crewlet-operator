# ADR-0007: Open the artifact origin only where it is reached

- **Status:** accepted
- **Authority:** `internal/controller`: `engineAddressesOf`;
  `docs/guides/exposure.md`, "Serve artifacts from an origin of their own"
- **Enforced-by:** `internal/render.TestBootstrapServesOutsidePartiesOnTheirOwnListener`,
  `internal/render.TestContractArtifactAddressShapes`,
  `test/crd.TestArtifactsBaseURLFollowsTheEngine`,
  `internal/controller.TestEngineAddressesFollowWhereTheEngineIsReached`

## Context

Crewlet serves artifacts on a listener of their own (`api.artifacts`), and
the dashboard frames HTML pages and designs from its origin. Unlike the API
and the public listener (ADR-0006), the engine starts without it. The one
address every company has, a port-forward to localhost, is on the
dashboard's own site, where Crewlet serves every artifact opaque and warns
at every start.

## Decision

The data pod opens the artifact listener (port 8082) only while its Company
has an address for it: `spec.exposure.artifactsBaseURL`, else the class's
artifacts route while it serves https. Otherwise its `crewlet.yaml` names no
`api.artifacts` and its pod declares no artifact port. An address Crewlet
would refuse holds the pods back with `RuntimeReady=False AddressRefused`.

## Consequences

A company with neither cannot open HTML pages or designs until it states or
routes one. Opening the port-forward address everywhere would warn on every
default company and restart every running data pod on the operator's
upgrade. Artifact plan quotas (`artifacts.limits`) stay at the engine's
defaults.
