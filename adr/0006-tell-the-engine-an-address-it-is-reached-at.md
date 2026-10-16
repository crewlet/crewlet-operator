# ADR-0006 — Tell the engine an address it is reached at, always

- **Status:** accepted
- **Authority:** `internal/controller` — `engineAddressesOf` and
  `reconcileExposure`; `docs/guides/exposure.md`, "Where people reach the
  dashboard"
- **Enforced-by:** `internal/render.TestContractBootstrap`,
  `internal/render.TestContractExternalURLShapes`,
  `internal/render.TestBootstrapNeedsTheEnginesAddresses`,
  `internal/controller.TestEngineAddressesFollowWhereTheEngineIsReached`,
  the runtime suite's exposure specs

## Context

Crewlet refuses to start a node that serves its API without
`api.external_url`, where a browser reaches it, and refuses one that opens its
public listener (`api.public`) without `api.public.external_url`, where a
vendor and a sandbox reach that listener (Crewlet's `internal/config/api.go`).
The first is no longer only the base of a link: the engine takes from it the
origin a person's write must come from, whether the session cookie is Secure
and its `__Host-` scope, and every link a person follows, an invitation and a
password link among them. The two addresses have no fallback between them.
Both are Tier A, which the engine reads only at start.

An operator-run data pod serves the API and opens its public listener on every
start, whatever its class routes (`render.PublicPort`), so both addresses are
needed on every data pod. A class may route neither the API nor the webhooks,
the getting-started class among them, and a Company may state neither address.

## Decision

The data pod's `crewlet.yaml` always names both addresses, chosen in this
order (`engineAddressesOf`):

- **Where a browser reaches the engine** (`api.external_url`, the Company's
  `status.endpoints.dashboardBaseURL`): the Company's
  `spec.exposure.dashboardBaseURL`, else the address of the class's API route
  (`https` behind a Gateway listener or an Ingress that terminates TLS,
  `http` otherwise), else `http://localhost:<APIPort>`, the address
  `kubectl port-forward` serves the engine Service's API port at.
- **Where the public listener is reached** (`api.public.external_url`): the
  Company's `status.endpoints.publicBaseURL` (its `spec.exposure.publicBaseURL`,
  or `https://<webhooks.hostname>`), else the listener's address inside the
  cluster, `http://<company>-engine.<namespace>.svc:<PublicPort>`.
  `status.endpoints.publicBaseURL` itself stays unset then, since it is the
  address a vendor is given, and the render's verdicts on a run that needs a
  public address keep reading it.
- **The route as an origin**: when the Company states an address whose origin
  is not the route's, the route's origin goes into `api.auth.allowed_origins`,
  so that a person who signs in through the route is not refused every write.

The API refuses an address with a query or a fragment on both fields, as the
engine does, so a Company is refused rather than its data pod crash-looping.
`render.Bootstrap` refuses to render a data pod's `crewlet.yaml` without both
(`render.ErrNoAddress`).

## Consequences

A change of either address, including a Gateway's listener turning HTTPS,
changes the bootstrap Secret and restarts the data pod: the engine reads
neither later. The addresses do not follow a route's acceptance, which would
restart the engine on every flip of it.

With no route and no stated address, every link the engine gives a person
leads to `localhost`, which only a browser forwarding the port reaches, and
the Exposed condition says so. The in-cluster address the engine is told for
its public listener is one no vendor can reach: a vendor app's manifest the
engine builds on it does not work, and a remote sandbox's telemetry has
nowhere to go. The alternatives are worse: an in-cluster address for the
browser fails the origin check for every port-forwarded browser and draws
Crewlet's plain-http advisory; not opening the public listener when nothing
routes to it moves the vendors' routes onto the API port, so a class that
starts routing webhooks would expose the admin port's address; and requiring
an address from every Company would refuse the getting-started company.

This record does not decide where an engine is reached from outside the
cluster, which the EngineClass's exposure does, nor which proxies' forwarded
headers the engine believes (Crewlet's `api.trusted_proxies`), which no field
of the operator's sets.
