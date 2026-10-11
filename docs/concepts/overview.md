# How the operator models a company

[Crewlet](https://github.com/crewlet/crewlet) runs one company per engine, and
the company is a single versioned document: its name and mission, its org
chart, the models its agents run on, their tools and the vendors they talk to.
The operator lets you write that document as Kubernetes objects instead, and
runs the engine for it.

This page explains the objects, how they fit together, how credentials reach
the engine, and who owns the document once the engine runs. Every field is in
the [API reference](../reference/api.md).

> The `crewlet.ai/v1alpha1` API is alpha and may change without notice.

## The kinds

| Kind | Scope | What it is | Becomes in the company document |
|---|---|---|---|
| `EngineClass` | Cluster | A platform's defaults and guard rails: the engine image, the size of the data pod, how companies are exposed, limits, models the platform provides | Nothing directly; it shapes the pods, the routes and the limits of every Company that names it |
| `Company` | Namespace | The company itself, and the engine that runs it | `name`, `mission`, `vision`, `policies`, `timezone`, `tracker`, `knowledge`, `token_budget`, `providers.embeddings`, `providers.sandbox`, and the engine tuning blocks in `engineConfig` |
| `Unit` | Namespace | One node of the org chart: a division, a team, a squad | One entry of `units`, nested under its parent |
| `Seat` | Namespace | One seat: an AI agent, or a person agents work with | One entry of `roles`, inside its unit |
| `LLMProvider` | Namespace | A model a company's seats can run on | One entry of `providers.llm` |
| `MCPServer` | Namespace | A tool server agents call | One entry of `mcp_servers` |
| `Integration` | Namespace | A vendor the company is connected to (Slack, GitHub, GitLab, Mattermost, Jira, Confluence, Datadog, an Atlassian organization), and who creates the seats' accounts there | One block of `integrations` |
| `Schedule` | Namespace | Recurring work of a seat or a unit, fired on a cron schedule: the company's CronJob | One entry of its seat's or unit's `schedules` |

All eight are in the `crewlet` category, so one command lists a company:

```bash
kubectl get crewlet -n tenant-nimbus
```

## How they relate

Every namespaced object names its company in `spec.companyRef`. The org chart
is a graph of objects: a Seat points at its Unit, a Unit at its parent, and
each can be added, reviewed, permission-scoped and deleted on its own, the way
Gateway API routes attach to a Gateway.

```mermaid
graph LR
  EC["EngineClass standard<br/>(cluster-scoped)"]
  C[Company nimbus]
  U1[Unit engineering]
  U2[Unit platform]
  S1[Seat ana]
  S2[Seat cto]
  S3["Seat jane<br/>(Human)"]
  P[LLMProvider anthropic]
  M[MCPServer github]
  I1[Integration slack]
  I2[Integration github]
  SCH1[Schedule platform-standup]
  SCH2[Schedule ci-health]
  SEC[("Secret ana-slack")]

  C -- engineClassName --> EC
  U1 -- companyRef --> C
  U2 -- parentRef --> U1
  S1 -- unitRef --> U2
  S2 -- unitRef --> U1
  S3 -- companyRef --> C
  U2 -. leadRef .-> S1
  P -- companyRef --> C
  M -- companyRef --> C
  I1 -- companyRef --> C
  I2 -- companyRef --> C
  S1 -. llm .-> P
  S1 -. mcp .-> M
  S1 -.->|integrations.slack| SEC
  SCH1 -- target.unitRef --> U2
  SCH2 -- target.seatRef --> S1
```

References stay inside the namespace. Several companies may share one, which
suits a self-hoster; a platform that hosts tenants gives each company a
namespace of its own. Only the cluster-scoped `EngineClass` refers to objects
in another namespace: the platform's own Secrets, in a namespace tenants
cannot read.

[`config/samples`](../../config/samples) is one complete example, Nimbus: a
class, a company, two units, three seats (one a person), a model provider,
two tool servers, the Slack, GitHub and GitLab integrations and two
schedules. GitLab's accounts are the engine's to make, so a seat names its
GitLab token as a provisioned value rather than a Secret.

### Identities that never change

Crewlet derives each seat's identity, and with it the seat's memory, from the
company's name and the seat's handle. The API makes both immutable:

- `Company.spec.companyName` cannot change once set.
- A Seat's handle is `spec.handle`, or its `metadata.name` when that is
  unset, and cannot change. A Seat deleted and created again with the same
  handle gets its memory back.
- A Unit's `metadata.name` is its stable id in Crewlet. Its `displayName` is
  what people read, and may change freely.
- Every object's `spec.companyRef` is immutable: an object does not move
  between companies.

Crewlet also refuses some collisions between objects, which the API server
cannot see because each object is validated on its own: two Seats with one
handle, one `displayName` or one contact account; two Units that answer to
one key (a Unit answers to its `displayName` and its name, compared without
case); two Integrations for one vendor; and an `LLMProvider` named
like a platform provider of the class. The operator reports each on the
object as `Accepted=False` with the reason `Conflict`, naming the other
object.

## What the engine looks like

A Company runs on one data pod in v1alpha1: a StatefulSet with one replica
and a volume, which holds the company's estate and also serves the API, runs
the background workers and runs the seats placed on it. Its size, image and
storage come from the `EngineClass`; `Company.spec.topology.data.replicas` is
1, the only value v1alpha1 admits. Where the class allows it, the Company's
`spec.topology.seatPools` add stateless seat pods beside the data pod, which
run the seats placed in them and hold no data
([Seat pools](runtime.md#seat-pools); [Choosing where seats
run](../guides/topology.md) says when a company needs them). A company can be stopped with `spec.suspend` (its volume is kept), and
`spec.deletionPolicy` says whether its volume outlives the Company (`Retain`,
the default) or goes with it (`Delete`). [How a company's engine
runs](runtime.md) lists every object the operator creates for it.

A seat can be paused without leaving the chart: `Seat.spec.paused: true`
stops it taking work until it is set back, the way a person pauses it in the
engine's dashboard. A pause is not part of the document, and the operator
owns it only for a Seat that sets the field; see
[Pausing a seat](applying.md#pausing-a-seat).

Vendors deliver webhooks to the company's public address, which the class
derives from a host name template (`{{uid}}.hooks.example.com`) or which the
Company states itself (`spec.exposure.publicBaseURL`). That address reaches
only the engine's public listener, a port of its own that serves the routes
outside parties call (the webhooks, and the endpoints a remote sandbox calls
back: its telemetry and, for an agent-mode coding CLI, the seat's tools) and
nothing else ([Exposure](../guides/exposure.md)); the engine's admin API and
dashboard are on another port, which stays inside the cluster unless the
class routes it through an internal gateway.

## Credentials: by reference, never by value

No field of the API accepts a secret. Every position Crewlet treats as a
credential — an API key, a bot token, a webhook signing secret, a tool
server's token — is a reference to a Kubernetes `Secret`:

- `SecretKeyReference` (`{name, key}`) selects one key, for a single value
  such as an API key.
- `SecretReference` (`{name}`) names a whole Secret where one credential is
  several values issued together. The field documents the keys the Secret must
  hold: a seat's Slack app is `botToken` and `signingSecret`; its GitHub App
  is `privateKey` and `webhookSecret`.
- Lists of variables or headers (`env`, `headers`) take
  `valueFrom.secretKeyRef` for every entry. Even a value that is not secret
  comes from a Secret there, because the API cannot tell a token from a
  setting.

The operator pushes each referenced value into the engine's encrypted secret
store, and the company document refers to it by name only. Rewriting the
Secret rotates the value in the engine without a restart. Who writes the
Secret is up to you: a person with `kubectl`, External Secrets, or a control
plane after a vendor's install flow only it knows about.

`Company.spec.engineConfig` passes engine tuning through as written, one
entry per top-level key of the document (`turn_engine`, `learning`,
`notification_rate_limit`, ...). The API server refuses an entry for any key
another field or object owns, and those are the only blocks that hold
credentials, so engineConfig can never carry one. [Credentials](credentials.md)
lists every credential position Crewlet has and the field that covers it,
including the ones v1alpha1 does not offer yet.

## Who owns the document: management policies

Crewlet's document can also be edited live, from its dashboard and its API.
`Company.spec.config.managementPolicy` says what happens to an edit the
operator did not make:

| Policy | What the operator does | For |
|---|---|---|
| `Enforce` (default) | It is the document's only writer: the engine refuses every other token a change to it, and its dashboard shows it read-only. A revision made some other way all the same is reported and overwritten with the rendered one. | GitOps; a platform that hosts tenants |
| `Observe` | It renders the document and compares it with the engine's, and never writes. A difference is reported only. | Taking back a company run from its dashboard; trying the operator out |
| `Seed` | It writes the document once, into an engine that has none, and then leaves it to the engine and its dashboard. Later changes to the Company's units, seats and other objects are ignored, and their status says so. | Self-hosters who prefer to work in the dashboard |

[How the document is applied](applying.md) says what each policy writes and
reports, and how drift is told from the engine's own encoding of a document.
Whatever the policy, a document is applied whole or not at all. When one
object is wrong — a Seat whose Secret is missing, a Unit whose parent does not
exist — the next revision waits, the object reports why in its conditions,
and the engine keeps running the last revision that worked.
[What the engine is given](company-document.md) shows the document the
objects render to, and what keeps one from rendering.

## Status

Every object reports `status.conditions` with `observedGeneration`.
`ResolvedRefs=False` on a part with `Applied=True` holds nothing back: the
part is in the running revision, and only it suffers what is reported
([What blocks a document](company-document.md#what-blocks-a-document)).

| Kind | Conditions |
|---|---|
| `Company` | `Ready` (the summary), `Accepted`, `RuntimeReady`, `SecretsSynced`, `ConfigApplied`, `Exposed`, `EngineHealthy`, `SeatsPlaced` |
| `EngineClass` | `Accepted` |
| `Unit`, `MCPServer`, `Schedule` | `Accepted`, `ResolvedRefs`, `Applied` |
| `LLMProvider` | `Accepted`, `ResolvedRefs`, `Applied`, and on a `CLIAgent` provider in `mode: Agent` `AgentModeRunnable` (whether its runs can call a seat's tools back: `False`, `NotExposed`, while the Company has no public address) |
| `Seat` | `Accepted`, `ResolvedRefs`, `Applied`, and on an Agent seat `Ready` (whether it runs: `True`, `LeaseHeld`, while an engine pod holds it; `False` with `Unplaced` while none does, `PoolNotFound` when its pool is not one the Company declares, or `AgentModeBridgeUnsupported` or `NotExposed` when the engine refuses every one of its turns), once the engine runs it `Paused`, and with `spec.sandbox` in a Company with one `CodeWorkRunnable` (whether its coding agent can run on its sandbox provider; not part of `Ready`) |
| `Integration` | `Accepted`, `ResolvedRefs`, `Applied`, `Healthy` |

A Company also reports the revision the engine runs, its endpoints and the
Secret holding a token for its API (`status.apiAccessSecretRef`, key
`token`). The endpoints are base URLs to append a path to: `api` inside the
cluster, `exposedAPI` through the class's API route when it has one, and
`publicBaseURL` for vendors' webhooks. A Seat reports its handle, its identity
in the engine, the pod that runs it and the addresses its vendor apps must deliver to
(`<publicBaseURL>/webhooks/slack/<handle>` and
`<publicBaseURL>/webhooks/github/<handle>`). They are set as soon as the
company has the vendor's Integration and a public address, before the seat
has any credentials, so a platform that creates a seat's Slack app reads the
request URL for the app's manifest from `status.integrations.slack.requestURL`
instead of building it, and they never change for a seat.

## Writing the objects from a program

A control plane imports the types from the Go module
`github.com/crewlet/crewlet-operator/api`, which depends on `k8s.io/api` and
`k8s.io/apimachinery` only. The Secret keys and labels the API documents are
constants there (`SlackBotTokenKey`, `APIAccessTokenKey`,
`CompanyLabel`, ...), so a program never repeats the strings.

The module has no generated apply configurations, because they would bring in
`k8s.io/client-go`. Server-side apply works with the typed objects
themselves: every optional field is omitted when unset, so the object sent
holds only the fields the caller sets, and those are the fields its field
manager owns. Lists that more than one writer may share are keyed, so that
each entry has its own owner: a Seat's or a Unit's `spec.mcp` by server name
(one manager can own the GitHub server's credentials and another the rest),
`spec.engineConfig` by key, and `env` and `headers` by name.
