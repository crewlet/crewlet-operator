# ADR-0007: Open the artifact origin only where it is reached

- **Status:** accepted
- **Authority:** `internal/controller`: `engineAddressesOf` and
  `reconcileExposure`; `docs/guides/exposure.md`, "Serve artifacts from an
  origin of their own"
- **Enforced-by:** `internal/render.TestBootstrapServesOutsidePartiesOnTheirOwnListener`,
  `internal/render.TestContractArtifactAddressShapes`,
  `test/crd.TestArtifactsBaseURLFollowsTheEngine`,
  `internal/controller.TestEngineAddressesFollowWhereTheEngineIsReached`,
  the runtime suite's artifact exposure specs

## Context

Crewlet serves the artifacts a company's people and agents publish on a
third listener (`api.artifacts`), which serves their bytes and nothing else:
no session, no cookie, no API route. An artifact is a page somebody wrote, so
the dashboard frames it from that listener's origin rather than its own, and
the engine refuses to start on an artifact origin a browser would treat as
the API's (the dashboard's or the public listener's origin, an allowed
origin, a parent of the API's host) or one reached over plain http on a
routable host (Crewlet's `internal/config/artifacts.go`). Without the
listener, the engine still starts and serves every artifact the dashboard
draws itself (documents, slides, dashboards, sheets), and refuses to frame
the rest (HTML pages and designs), saying `api.artifacts.external_url` is
unset.

ADR-0006 opens the public listener on every data pod and always tells the
engine an address for it, because the engine requires one for each listener
it opens and a route that appears later must not change which routes a port
serves. The artifact listener could follow the same rule, with
`http://localhost:8082`, a port-forward, as the address of last resort.

## Decision

The data pod opens the artifact listener, on port 8082, only while its
Company has an address for it, in this order (`engineAddressesOf`):

- the Company's `spec.exposure.artifactsBaseURL`, which may be the
  port-forward address, `http://localhost:8082`, stated on purpose;
- the address of the class's artifacts route (`spec.exposure.artifacts`),
  while it serves https: `https://{artifact}.<hostname>` under `origins`
  `PerArtifact`, its default, which routes `*.<hostname>`, and
  `https://<hostname>` under `Shared`.

With neither, the data pod's `crewlet.yaml` names no `api.artifacts`, its
pod template declares no artifact port, and the Company's `Exposed` message
says which artifacts the dashboard cannot open. A Gateway route with no
HTTPS listener is `Exposed=False RouteNotHTTPS` and opens nothing. The
engine Service names the port whatever the Company has, so that a route
written before the pods open the listener resolves its backend.

The API refuses what it can see of an address Crewlet refuses (the shape of
`artifactsBaseURL`; an artifacts host name equal to, or a parent of, the
class's API or webhooks host name), and `render.Bootstrap` refuses the rest
(`artifactsURLRefusal`, Crewlet's own checks copied and fingerprinted), so a
Company whose address meets its dashboard's is held back with
`RuntimeReady=False AddressRefused` rather than started to crash-loop.

## Consequences

A company whose class routes no artifacts and that states no address cannot
open an HTML page or a design until it does. The alternative, the
port-forward address on every data pod, is worse on each count: localhost is
the port-forwarded dashboard's own site, where Crewlet serves every artifact
with an opaque origin and advises against it at every start, so every
default company would boot with an advisory about a choice nobody made; and
adding the block and the port to every data pod would restart every running
data pod on the operator's upgrade, for an origin only a browser that
port-forwards a second port reaches. A platform that wants that origin states
it, and gets the advisory it chose.

Unlike the public listener, a route that appears later does change what the
data pod serves: the listener opens, which restarts the data pod, as any
change of an address the engine reads only at start does.

This record does not decide where people reach the dashboard or the public
listener (ADR-0006), nor the plan quotas on artifacts (Crewlet's Tier A
`artifacts.limits`), which the operator leaves at the engine's defaults.
