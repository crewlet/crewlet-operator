# Running Tool Servers

An `MCPServer` is a tool server a company's agents call: a program the engine
starts beside itself, or a remote server it connects to. It is how an agent
acts anywhere outside the engine, its chat replies included under Mattermost
and Slack.

Each renders one entry of Crewlet's `mcp_servers`; Crewlet's
[Tools & MCP](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#mcp-integration)
describes what the engine does with it. This guide covers what changes when
the engine runs in a pod: a server the engine starts has to be in its image,
and Crewlet's image carries none; and each integration names the server its
agents use.

## Stdio and HTTP

- **`transport: HTTP`** connects to a server that runs elsewhere, at `url`.
  Nothing goes into the image.
- **`transport: Stdio`**, the default, starts `command` with `args` as a
  process of the engine's own container, as the engine's user (uid 10001),
  and speaks MCP over its standard input and output. The process does not
  get the engine's environment, only the part Crewlet hands a tool server:
  `PATH`, the locale, the TLS and proxy variables and the engine user's
  `HOME`, among a few others
  ([Crewlet's list](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#what-a-stdio-servers-environment-is)).
  The server's `env` goes on top of that, and, for a server with
  `shared: false`, the values of the seat it serves on top again.

A variable the image sets with `ENV`, or the class's `podTemplate.spec.env`
or the Company's `runtime.env` sets on the pod, reaches a stdio server only
when that list names it, as it names the proxy variables. Give the server
every other variable it reads in its `env`
([Settings and credentials](#settings-and-credentials)): one that relies on
anything else of the engine's starts without it, and fails or behaves as
though it were unset.

A server with `shared: false` is a template rather than a process: each agent
seat it reaches ([Choosing which agents get a server](#choosing-which-agents-get-a-server))
gets a process of its own, on the pod that holds the seat, which is how each
agent acts as itself. The process starts with the values the seat gives the
server in `spec.mcp`, its own laid over its unit's, and with the server's own
where the seat gives none; one started without the seat's credentials has no
identity of the seat's to act as. So a setting every seat shares goes in the
server's own `env`, and a seat's credential in its own `spec.mcp`. Every
engine pod of a company runs the class's image, a
[seat pool](topology.md)'s pods too, so the image has to carry every server
any seat may need.

## Turning a server off, and saying what it is for

`enabled: false` turns a server off and keeps it. The engine starts no
process of it, the company's, a seat's or a box's, and grants it to no
seat, while the MCPServer and every Seat's and Unit's `spec.mcp` and
`spec.sandbox.mcpServerRefs` that name it stay as they are. A value it would
be started with is read by nothing while it is off, so a Secret key holding an
empty value is no failure of the object that gives it; a Secret or key that
does not exist still holds the document, as every reference does. Set it back to `true`
(the default) and the next revision starts it again with the values its seats
kept.

`description` is one line of at most 200 characters, with no line break or
tab, saying what the server is for. Each agent's prompt names its tool
servers before it shows any of their tools, and prints the description beside
the name, which is what tells an agent which server to look in:

```yaml
spec:                       # MCPServer handbook
  description: Searches the engineering handbook and the runbooks
```

Both are Crewlet's own fields
([Crewlet's guide](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#turning-a-server-off)).

## Choosing which agents get a server

`access` is which agent seats a server reaches, and nothing else grants one
([Crewlet's guide](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#choosing-which-agents-get-a-server)):

```yaml
spec:                       # MCPServer linear
  transport: HTTP
  url: https://mcp.linear.app/mcp
  access:
    unitRefs: [{name: engineering}]     # every agent seat in engineering and beneath it
    seatRefs: [{name: pm}]              # and this agent seat
    exceptSeatRefs: [{name: intern}]    # but not this one, though engineering holds it
```

- **Unset, every agent seat** of the company gets the server, one added later
  too. Set, exactly the seats it names. `access: {}` names nobody, which
  Crewlet warns of; a shared server's one instance still starts, so turn the
  server off with `enabled: false` if nothing should run it.
- **A Unit stands for every agent seat beneath it**, in it and in every Unit
  under it at any depth, so a seat that joins the team gets what the team
  has. A Seat stands for itself, and `exceptSeatRefs` leaves out a seat a
  named Unit holds. A person's seat runs no tools and is reached by nothing.
- **One grant for both kinds of server.** A shared server's tools are offered
  to the seats it reaches and to no other; with `shared: false`, each seat it
  reaches gets a process of its own, and no other seat does. A box a seat's
  `spec.sandbox.mcpServerRefs` lists is given the server only where it
  reaches the seat.
- **A Seat's or a Unit's `spec.mcp` holds values and grants nothing.** An
  entry for a server that does not reach the Seat, or that reaches none of
  the Unit's own agent seats, is refused, and so is a `mcpServerRefs` entry
  for one: a change to a grant fails the change that would leave an entry
  that reads like one, so the entry goes, or the grant takes the seat in, in
  the same change. A server turned off is held to the same rule. An entry for
  a server named `github`, `gitlab`, `datadog`, `atlassian`, `jira` or
  `confluence` is the engine's to read as well and is not held to it.
- **A server reading a vendor's per-seat credential is granted by the
  integration instead**: the agent seats on at it
  (`spec.integrations.<vendor>.enabled` on the Seat, Jira's or Confluence's
  for Atlassian) get it. That is a server with `seatCredentials`, and one the
  engine recognises by its command or address
  ([Each integration's server](#each-integrations-server)). It takes no
  `access`.

Each mistake is refused where it first shows, and reported on the object that
holds it:

| Mistake | Refused by | Reported as |
|---|---|---|
| `access` beside `seatCredentials`; a Seat both in `seatRefs` and in `exceptSeatRefs`; a name listed twice | the API server, when the MCPServer is applied | the apply's error |
| `access` naming a Unit or a Seat that does not exist, or a Human seat, which includes removing a Seat or a Unit a grant still names | the operator, which holds the company's document, as Crewlet refuses it | `ResolvedRefs=False`, `RefNotFound` or `HumanSeat`, on the MCPServer |
| `access` on a server the engine recognises as reading a vendor's per-seat credential, which declares no `seatCredentials` | the engine's dry run | `Applied=False Invalid` on the MCPServer |
| a Seat's `spec.mcp` or `mcpServerRefs` entry for a server that does not reach it, or a Unit's `spec.mcp` entry for one that reaches none of its own agent seats | the engine's dry run | `Applied=False Invalid` on the Seat or the Unit |

So take a Seat or a Unit out of every grant before deleting it: until then
the company's document is held, since a grant naming a handle or a key nobody
holds would reach whichever Seat or Unit took it later.

## Names, prefixes and addresses

An MCPServer's name is the server's name in the company document, and the
API holds it, its `toolPrefix` and its `url` to what Crewlet holds them to
([Crewlet's guide](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#naming-a-server-and-its-tools)),
so an object Crewlet would refuse is refused when it is applied, rather than
holding back the document every other object of the Company is in:

- **The name** is lowercase letters, digits and `-`, with no `.`, at most 32
  characters, and not `crewlet`. It is the origin of every tool the server
  serves, the key a Seat's or a Unit's `spec.mcp` names it by, and part of
  every tool name an executor's box hands its model for a server it runs.
- **`toolPrefix`** is letters, digits, `_` and `-`, at most 49 characters. It
  goes in front of each of the server's tool names, and Crewlet offers a tool
  only where its prefixed name is at most 50 characters of those, what the
  longest prefix a harness gives the engine's tools leaves of a model's 64.
  A tool it cannot offer is never shortened: Crewlet reports it on the
  server's row (`refused_tools` on `GET /mcp-servers`), with what would offer
  it, as it reports one another server's tool of the same name is offered in
  place of (`shadowed_tools`).
- **`url`** is `https`, or plain `http` only to a host on no public network,
  since every request carries the server's credentials: a loopback, private,
  link-local or shared (RFC 6598) address, a one-label name such as a Service's own name in its
  namespace, `localhost`, or a name under `.localhost`, `.local` (a Service's
  `<name>.<namespace>.svc.cluster.local`), `.home.arpa` or `.internal`.

## Setting each tool's level

`tools` sets the level each tool the server lists runs at: `Allow` offers it
to every agent the server reaches and runs each call, `Approve` offers it and
holds each call until a person allows or denies it, and `Blocked` never offers
it. `default` is the level of every tool `levels` does not name, a tool the
server adds later included. `levels` names a tool by the server's own name
for it, the one its listing gives before `toolPrefix`:

```yaml
spec:                       # MCPServer billing
  command: billing-mcp
  tools:
    default: Blocked        # every tool levels does not name
    levels:
      - tool: list_invoices
        level: Allow
      - tool: void_invoice
        level: Approve
```

**A tool nothing sets starts by its hints.** With no `default` and no level
of its own, a tool the server marks read-only (`readOnlyHint`, with the
server's `toolAnnotations` applied) runs at `Allow`, and every other tool at
`Approve`, a tool whose hints say nothing included. So a server with no
`tools` asks a person before each call to any tool it does not mark
read-only; set `default: Allow` to run every tool without asking.

Crewlet never files a blocked tool, so no agent is offered it, lists it or
can call it, on the engine or from an executor's sandbox through the engine's
bridge, on a shared server and on every seat's instance of a per-seat one. A
call to a tool at `Approve` is not made until a person answers it: an agent's
executor run parks on the question with its seat freed, an administrator or
a lead of the seat's unit allows it once, always (which sets the tool to
`allow` in Crewlet's own configuration) or denies it through Crewlet
(`POST /mcp-approvals/{id}`), and the run carries on with what the call
returned; any other part of a turn is refused the call. A name the server
does not list sets nothing, and Crewlet names every such level on the
server's page. A tool name holds no space and no control character, and each
tool is named once; the API refuses anything else.

**A sandbox reaches a server whole.** A server a Seat's
`spec.sandbox.mcpServerRefs` lists is dialled by the harness inside the
sandbox itself, past the engine, so a level could not hold there and nobody
would be asked before a call. A Seat may list only a server whose every tool
runs at `Allow`: `default: Allow` and no other level. Any other listing holds
the document, reported on the Seat at `spec.sandbox.mcpServerRefs[i].name`
(`Accepted=False`, reason `Conflict`), since Crewlet refuses it: take the
server out of the list, where the seat still reaches its tools over the
bridge, each at its level, or set its `tools` to `default: Allow`. Crewlet's
own field
([Crewlet's guide](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#setting-each-tools-level)).

## Narrowing a server for a team

An MCPServer's `access` and `tools` are the grant. A Unit or a Seat narrows it
with `mcpLimits`, one entry per server: `enabled: false` turns the server off
for the seat, or for every agent seat beneath the unit at any depth, and
`tools` holds a tool, by the server's own name for it, at `Approve` or
`Blocked`.

```yaml
spec:                       # Unit platform
  mcpLimits:
    - name: search          # nothing of it starts for, or is offered to, the team
      enabled: false
    - name: billing
      tools:
        - tool: void_invoice
          level: Approve
        - tool: delete_invoice
          level: Blocked
```

**A limit only narrows.** Crewlet holds each agent seat to the strictest of
its own limits, every enclosing unit's and the MCPServer's own `tools`: a
server any of them turns off is off, and a tool runs at the strictest level
any of them sets, so no limit runs a tool the MCPServer blocks or holds for
approval. A limit has no `Allow` and cannot turn a server on (`enabled:
true` is refused by the API), and one that holds nothing back (no `enabled:
false` and no tool) is refused too. A server turned off keeps its grant
and the seat's `mcp` values for it. A limit names an MCPServer in the
namespace, reported on the object otherwise, and a Human seat takes none.

**A sandbox reaches a server whole**, so Crewlet refuses a Seat's
`spec.sandbox.mcpServerRefs` naming a server its limits, or its units', hold a
tool of back; its dry run reports the listing as Invalid on the Seat. A
server a limit turns off is kept in the listing and given to no sandbox.

On Crewlet a unit's lead may write their teams' limits through its API and
only an administrator may lift one. Under the operator the company document
is managed, so whoever may edit the Unit and Seat objects sets them, and the
Kubernetes RBAC on those objects is the line between granting and narrowing.
Crewlet's own field
([Crewlet's guide](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#narrowing-a-server-for-a-team)).

## Settings and credentials

A server's `env` (an HTTP server's `headers`) and the values a Seat or a Unit
gives it in `spec.mcp` take two kinds of entry: a credential, read from a
Secret with `valueFrom`, and a setting that is no credential, such as an
address, a switch or a path, written out as `value`:

```yaml
spec:                       # MCPServer atlassian
  env:
    - name: JIRA_URL                    # a setting
      value: https://jira.acme.example
---
spec:                       # Seat lead
  mcp:
    - name: atlassian
      env:
        - name: JIRA_API_TOKEN          # a credential
          valueFrom:
            secretKeyRef: {name: lead-jira, key: token}
```

Crewlet cannot tell the two apart: it treats a server's whole environment,
and every value a seat gives it, as credentials, and masks each on every
read ([A tool server's token](credentials.md#a-tool-servers-token)). The
name can, and the API reads it as Crewlet reads a credential's name: a
`value` is refused under a name one of whose parts, cut at every character
that is not a letter or a digit, is, in any case, `KEY`, `KEYS`, `APIKEY`,
`ACCESSKEY`, `SECRETKEY`, `TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`,
`CREDENTIAL`, `CREDENTIALS`, `AUTH`, `OAUTH` or `PAT`, and under the
`Authorization`, `Proxy-Authorization` and `Cookie` headers. So
`MATTERMOST_TOKEN`, `DD_APP_KEY` and `X-Api-Key` come from Secrets, and
`MATTERMOST_URL`, `GITLAB_HOST` or `SLACK_MCP_ADD_MESSAGE_TOOL` may be written
out. The rule keeps a token from being typed where a setting goes; it cannot
keep every secret out, and a credential under a name that says nothing, such
as a database address with a password in it, is still yours to put in a
Secret. A `value` lands in the company document as written, where anyone who
can read the object reads it. It may not hold a `${NAME}` reference: the
engine resolves one from its own secret store and then from its own
environment, which is how Crewlet passes one of the engine's own variables
on to a server, and the API does not offer that, since both hold credentials
that are not the object's to name. So a variable beyond what Crewlet's
allowlist hands every server is written out here or read from a Secret,
never named from the engine's secret store or environment.

A setting is the server's when every seat runs with it, and a seat's when
each seat needs its own: Slack's channel cache is one, a path no two seats
may share ([below](#slack)).

### A key that may go without its value

Crewlet starts a server only once every value it is started with holds
something: its own `env` or `headers`, and what a Seat or its Unit gives it.
A value read from a Secret that does not exist, holds no such key or holds an
empty value keeps the server from starting, shared or a seat's, rather than
handing it an empty token it would refuse or act as nobody with. A value the
server can run without, such as the key Context7's server takes for higher
rate limits and serves without, is listed by its key in `optionalKeys`:

```yaml
spec:                       # MCPServer context7
  transport: HTTP
  url: https://mcp.context7.com/mcp
  headers:
    - name: Authorization             # Bearer and the key
      valueFrom:
        secretKeyRef: {name: context7, key: authorization}
  optionalKeys: [Authorization]
```

Under a listed key, a value whose Secret or key does not exist, or that holds
an empty value, is left out whole: the server starts without the variable, or
its requests go without the header, as Kubernetes starts a container without
an `optional` `secretKeyRef`. Nothing holds the company's document, and the
object that gives the value reports `ResolvedRefs=True`, its message naming
each value it goes without and the list that lets it: the MCPServer for its
own `env` or `headers`, a Seat or a Unit for what its `spec.mcp` gives. When
the Secret appears, the operator pushes its value and applies the document
again, as it does for a rotated credential
([Rotating a credential](credentials.md#rotating-a-credential)), and the
engine starts the server with it. When it is deleted, the operator takes the
value out of the engine's secret store and has the engine read the store
again: by applying the next document, or, while that document is held for
something else, by activating the revision the engine runs again, unchanged,
so the server is not started again with a deleted Secret's value. That is the
operator's under the `Enforce` policy, under which it keeps the store in step
with the Secrets ([Management policies](../concepts/applying.md#management-policies)),
and it holds where the revision the engine runs goes without the value too.
A revision that reads the key as required, as the one from before you listed
the key does, another document somebody else activated, whose reading the
operator does not know, or a revision the engine stores without a seal, which
nobody can read while its nodes run the one before it, keeps the value it
holds until the next document runs, as the last good revision keeps every
value it requires; the Company's
`SecretsSynced`, the object's `ResolvedRefs` message and a `SecretKept` event
say so (a document the render holds is told by the event alone). Once no
engine pod runs, or would start on, a revision that requires it, the operator
deletes the value and activates the new revision again without it, so that
revision may start the server with the deleted Secret's value in between. A
pod that refused the next document keeps running the revision before it, which
keeps the value while it does; only a data pod starts on that revision again
if it restarts, while a seat pod starts on nothing and tries the fleet's
revision again, and a pod that runs no company and would start on none holds
nothing back. A pod whose revision the operator cannot learn keeps every
value, as a data pod does while it is down or cut off from the engine's fleet
([Applying a document](../concepts/applying.md#what-one-reconcile-does)). A
Secret key that is also read where it is required, by this server or anything
else, is not one the company goes without: its deletion holds the document
(`SecretNotFound`), and the revision the engine runs keeps the value until
the next one applies if it requires it too, and goes without it if it lets it
go.

- **The list covers every value the server is started with**: its own `env`
  (a Stdio server, by the variable's name, as written) or `headers` (an HTTP
  server, a header in any case), and every Seat's and Unit's `spec.mcp` entry
  for it, which may give a Stdio server a key outside a variable's grammar. A
  seat's value for a key replaces the server's own whole, so one that goes
  missing leaves the key out rather than handing the server's own value back.
  It is the server's settings the list is for: a seat's own credential is the
  identity its instance acts as, and one started without it acts as nobody.
- **A provisioned value is waited for whatever the list says**: an
  Integration mints it for the seat, and the server starts once it has. One
  nothing mints, while no Integration of the company has
  `provisioning: Engine`, is left out.
- **An entry must leave something out.** The API refuses one spelling an
  HTTP server's header twice, in two cases, and a key no Seat or Unit could
  write. One under which no value is read from a Secret, or provisioned, by
  the server or by any Seat or Unit for it, every value there being a literal
  `value`, which is never missing, or there being none, holds the company's
  document: the MCPServer reports `ResolvedRefs=False OptionalKeyUnused`,
  naming the objects that write the literals, since Crewlet refuses it. A
  change to a Seat or a Unit can make it so: deleting the only Seat that
  gives the key from a Secret holds the document, the deletion included,
  until the entry is removed or another value is.
- **A Seat and its Unit spell a header alike.** Crewlet lays a Unit's
  `spec.mcp` under each agent member's own by the name as written, so a
  member's `authorization` beside its Unit's `Authorization` would send
  either value; the Seat reports `Accepted=False Conflict`, and the document
  is held, as Crewlet refuses it.
- **A required value that holds nothing is reported.** Where the key is not
  listed, a value read from a Secret key that holds an empty value does not
  hold the document: the engine applies it and does not start what the value
  is given to. The object that gives the value reports `ResolvedRefs=False
  SecretValueEmpty`, with a Warning event, naming the Secret, the key, the
  variable and what does not start; it says so only of a value the engine
  would start something with, so a server's own value that every seat
  the server reaches replaces is not one. A missing Secret or key holds the
  document, as anywhere else (`SecretNotFound`).

A Seat's `spec.sandbox` and a setup step take an `optionalKeys` of their own
for a seat's executor run's environment and files
([A value a run may go without](harnesses.md#a-value-a-run-may-go-without)).
Crewlet's
[A key that may go without its value](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#a-key-that-may-go-without-its-value)
has the engine's side.

## Signing in with OAuth

A remote server that takes no static token, or one whose vendor prefers a
sign-in to a key, is signed in to with OAuth instead: `spec.oauth` says how,
and the engine sends the access token it is granted in the `Authorization`
header of every request, and has a new one before it runs out. Its `grant`
says who is granted it. Left out, or `AuthorizationCode`, a person signs the
company in once, which the rest of this section is about; `ClientCredentials`
signs the engine in as a service identity, with nobody signing in
([Signing in as a service identity](#signing-in-as-a-service-identity)):

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: tickets
spec:
  companyRef:
    name: nimbus
  transport: HTTP
  url: https://mcp.tickets.example.com/mcp
  oauth:
    client: Own
    clientID: crewlet-nimbus
    clientSecret:
      secretKeyRef: {name: tickets-oauth, key: clientSecret}
    scopes: [read, write]
```

Crewlet's
[curated catalogue](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#the-curated-catalogue-and-a-community-registry)
names the servers whose vendors offer a sign-in. `client` is how the engine
registers at the server's authorization server, and the engine's check of
the server's address in its dashboard says which the server supports:

- **`CIMD`**: the engine's client id is the address of a client metadata
  document it publishes, which the authorization server reads. Nothing is
  registered beforehand.
- **`DCR`**: the engine registers a client at the authorization server when
  a sign-in starts, which issues the id.
- **`Own`**: a client an administrator registered at the vendor beforehand,
  whose id is `clientID`, with its secret in `clientSecret` where the vendor
  made it confidential, read from a Secret key as every credential is
  ([Credential Positions](../concepts/credentials.md)). Leave `clientSecret`
  out for a public client.

`scopes` are what the sign-in asks for; left out, it asks for the ones the
server's metadata lists. The API refuses what Crewlet refuses, at the
object:

- A person's sign-in with no `client`. `clientID` and `clientSecret` go with
  `Own` alone: with `CIMD` or `DCR` the id is the engine's or the
  registration's, and the engine signs in as a public client.
- A scope that is not one scope token as RFC 6749 spells it (printable ASCII
  with no space, double quote or backslash), one listed twice, and more than
  50.
- `oauth` on a `Stdio` server, which is started with its credentials in
  `env`; on a server with `shared: false`, since a sign-in is one grant for
  the whole company and a per-seat server acts as each seat's own identity;
  beside an `Authorization` header in `headers`, which the sign-in sends
  itself; and beside a header named `oauth.client_secret`, in any case,
  which is the key the engine's dashboard stores the OAuth client's secret
  under. Another header rides beside it.

A Seat's `spec.sandbox.mcpServerRefs` may not name such a server: a box is
handed a server's credentials once, at its launch, and could not renew the
access token. The Seat reports `Accepted=False Conflict` and the document is
held, as Crewlet refuses it; the box reaches the server's tools through its
tool bridge instead.

### Who signs in, and where the grant lives

An administrator signs the company in from the server's page in the
dashboard (Settings › MCP & tools). The vendor sends the person's browser
back to the engine's public address, at `/webhooks/mcp-oauth`, which is the
redirect address to register for an `Own` client, and a `CIMD` client's
metadata document is published under the same path. So a sign-in needs the
Company exposed ([Choose how webhooks arrive](exposure.md#choose-how-webhooks-arrive)):
with `exposure.webhooks.mode: None` the engine has no address to send the
person back to and starts no sign-in. Until somebody signs in, and again
once the vendor refuses to renew the grant, the engine starts no instance of
the server and reports it as needing a sign-in (`needs_sign_in`), and its
tools go unserved. A grant is sent only to the address it was issued for:
change `url` to another address and the server waits for a sign-in there,
rather than taking the company's token with it.

The grant itself, the refresh token and the client a registration issued,
is sealed in the engine's own store under a name no Secret, document or
`${NAME}` reaches, and nothing reads it back out. The operator neither
writes nor sees it, so signing in changes nothing in the company document
and works under every management policy, `Enforce` included. Only the
`clientSecret` of an `Own` client is the operator's to push, from its Secret
key, and it is rotated as any credential is
([Rotating a credential](credentials.md#rotating-a-credential)). The engine
sends it at every renewal of the sign-in, so it starts the server only once
the secret holds a value: a Secret key that holds an empty value holds no
document back, and the MCPServer reports `ResolvedRefs=False
SecretValueEmpty`, with a Warning event, until it holds one. No
`optionalKeys` entry lets the server go without it. A Secret or key that
does not exist holds the document (`SecretNotFound`), as anywhere else.

Crewlet's
[Signing in with OAuth](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#signing-in-with-oauth)
has the engine's side: what the server's page shows of the grant, the alarms
a grant that stops working raises, and signing out.

### Signing in as a service identity

Some vendors offer a **service identity**: an OAuth client you register at
the vendor that signs in as itself, with its client id and secret and no
person behind it (OAuth's client credentials grant). With
`grant: ClientCredentials` the engine signs in as that client whenever the
server needs an access token:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: vendor
spec:
  companyRef:
    name: nimbus
  transport: HTTP
  url: https://mcp.vendor.example.com/mcp
  oauth:
    grant: ClientCredentials
    clientID: crewlet-agents
    clientSecret:
      secretKeyRef: {name: vendor-oauth, key: clientSecret}
    scopes: [read]
```

`ClientCredentials` requires `clientID` and `clientSecret`, and refuses
`client`: a service identity is always the company's own client, so nothing
is registered. `scopes` are what each token asks for; left out, the vendor
issues the client's default ones. Every other rule of a sign-in holds for it
too: a shared `HTTP` server, neither an `Authorization` header nor one named
`oauth.client_secret` beside it, and no Seat's `spec.sandbox.mcpServerRefs`
naming it.

Nobody signs in, so the server needs no sign-in from the dashboard and no
address for a vendor to send a browser back to: the engine starts it as soon
as the document is applied, and asks the server's authorization server for
an access token with the client's id and secret as the server is used. Each
pod holds the tokens it is issued in its memory alone; nothing is stored,
and a sign-out has nothing of a service identity's to revoke. A vendor that
refuses the client's credentials (a secret revoked or rotated at the vendor,
a client not allowed the grant, a scope it will not issue) fails the
server's instances in the vendor's own words and raises the engine's
`mcp_server_failing` alarm; the engine asks again on its restart schedule,
and at once when the client's id, secret or scopes change.

The client's secret is the operator's to push from its Secret key, as an
`Own` client's is, and rotated the same way
([Rotating a credential](credentials.md#rotating-a-credential)). The engine
starts the server only once the secret holds a value: an empty one holds no
document back, and the MCPServer reports `ResolvedRefs=False
SecretValueEmpty`, with a Warning event, until it holds one, while a Secret
or key that does not exist holds the document (`SecretNotFound`).

Crewlet's
[Signing in as a service identity](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#signing-in-as-a-service-identity)
has the engine's side: where the engine reads the token endpoint from, how
it sends the secret, and what the server's page shows of the identity.

## The image needs the server

Crewlet's image is Debian with `ca-certificates`, `tini`, the engine and
git, which the image builds from the source release Crewlet pins
(`scripts/git-pin.env`) and installs under `/usr/local`, in place of
Debian's `git` package: no Python, Node.js, `uv`, `npm` or `curl`. Its
[Dockerfile](https://github.com/crewlet/crewlet/blob/main/Dockerfile) says so
on purpose: a stdio server whose runtime is not there needs a derived image.

A `command` the image does not have never starts. The engine logs
`mcp_server_failed`, the seats run without that server's tools, and nothing
else is affected; the MCPServer still reports `Applied=True`, since the
engine runs the document that names it
([A tool server does not start](troubleshooting.md#a-tool-server-does-not-start)).
Under Mattermost and Slack the server is the agent's only way to post, so on
the plain image an agent receives every message, runs its turn and answers
none.

Crewlet's own docs and examples start these servers through a package runner:
`uvx` for a Python server such as `mcp-server-mattermost`, and `npm exec` for
`slack-mcp-server`, a Go binary npm delivers. The runner downloads the server,
and for `uvx` a Python, as it starts. In a pod that does not work: the runner
is not in the image, and in an image that had it, everything it downloaded
would land in `HOME`, an `emptyDir` emptied whenever the pod starts. Every pod
would download its servers again at every start and need egress to PyPI or
npm to start them at all. Even with the server pinned, each start would also
resolve again every dependency the package leaves unbounded, so a pod could
start a different set from the one you tested. Install every server when you
build the image, pinned, and name the installed command.

## Building the image

Build the image from the engine image you run, named by digest: the engine of
the operator's release, or the copy of Crewlet's `main` image that
[Installing](installing.md#engine-image) makes in your registry. This
Dockerfile installs the four stdio servers Crewlet names for its
integrations ([below](#each-integrations-server)); delete the lines of any
your company does not run.

```dockerfile
# The engine image you run, by digest, and uv, which installs the Python
# servers and is not kept.
ARG ENGINE_IMAGE
ARG UV_IMAGE=ghcr.io/astral-sh/uv:0.13.1@sha256:4a1f3666816dafadd20ceb71eac9a7d56d7ef30279f7d64048519ab901ada336

FROM ${UV_IMAGE} AS uv

# The two Go servers are downloaded and checked here, with a curl the engine
# image does not ship: only the two binaries leave this stage.
FROM ${ENGINE_IMAGE} AS fetch
ARG TARGETARCH
# GitLab: glab, whose `glab mcp serve` is the server. The digests are the
# release's checksums.txt.
ARG GLAB_VERSION=1.123.0
ARG GLAB_SHA256_AMD64=9c9ac4c2b74ec8709dec4819a4041050f3680c6aaa18f0ed30434081f4fe9bef
ARG GLAB_SHA256_ARM64=1f2d0a885b28d08de341a597b2eb7204ce03d4e67b008efecab9951e4b014f0c
# Slack: slack-mcp-server, a Go program npm delivers. The binary is taken from
# its platform package, checked against the registry's dist.integrity, so no
# Node.js is needed to run it.
ARG SLACK_MCP_SERVER_VERSION=1.3.0
ARG SLACK_MCP_SERVER_INTEGRITY_AMD64=sha512-ud5BqvPq6GE2wazMX+qq+PeSuhKRzLBhYhoDVyrShGe5bguxoE+/olffsnUMdFLKwLaLxs/DMPdHprRUVGqJzw==
ARG SLACK_MCP_SERVER_INTEGRITY_ARM64=sha512-7yLIg4u6tlhk1TIsrayUgqMvJdvZ+Wp80JHy83pAx0pYCyo7Inwt80QNWKem/xMFC8RSOH//YHrlAWhATFQa+w==
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
WORKDIR /fetch
RUN case "${TARGETARCH}" in \
      amd64) sum=${GLAB_SHA256_AMD64} integrity=${SLACK_MCP_SERVER_INTEGRITY_AMD64} ;; \
      arm64) sum=${GLAB_SHA256_ARM64} integrity=${SLACK_MCP_SERVER_INTEGRITY_ARM64} ;; \
      *) echo "no digest for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo glab.tar.gz "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    echo "${sum}  glab.tar.gz" | sha256sum -c -; \
    tar -xzf glab.tar.gz --strip-components=1 bin/glab; \
    package=slack-mcp-server-linux-${TARGETARCH}; \
    curl -fsSLo slack.tgz "https://registry.npmjs.org/${package}/-/${package}-${SLACK_MCP_SERVER_VERSION}.tgz"; \
    test "sha512-$(openssl dgst -sha512 -binary slack.tgz | base64 -w0)" = "${integrity}"; \
    tar -xzf slack.tgz --strip-components=2 "package/bin/${package}"; \
    mv "${package}" slack-mcp-server

FROM ${ENGINE_IMAGE}
# Python 3.13, not 3.14 or 3.15: both servers accept any Python from 3.10,
# but at these releases their CI tests 3.10 to 3.13 only, and
# mcp-atlassian's own image runs 3.13.
ARG PYTHON_VERSION=3.13.16
ARG MCP_SERVER_MATTERMOST_VERSION=0.6.1
ARG MCP_ATLASSIAN_VERSION=0.23.1
# The servers' own dependencies are resolved as PyPI had them on this date,
# so a rebuild installs what was tested. Move it with the versions.
ARG RESOLVED_AT=2026-10-19T00:00:00Z
USER root
# The interpreter, its standard library compiled, and each server's
# environment go under /opt/uv, the servers' commands into /usr/local/bin.
# Compiled now, because the root filesystem is read-only at run time: nothing
# can write the bytecode then, so every start would compile it again.
RUN --mount=from=uv,source=/uv,target=/bin/uv \
    export UV_NO_CACHE=1 UV_PYTHON_INSTALL_DIR=/opt/uv/python \
      UV_TOOL_DIR=/opt/uv/tools UV_TOOL_BIN_DIR=/usr/local/bin \
      UV_EXCLUDE_NEWER="${RESOLVED_AT}"; \
    uv python install --no-bin --compile-bytecode "${PYTHON_VERSION}" \
    && uv tool install --managed-python --compile-bytecode --python "${PYTHON_VERSION}" \
      "mcp-server-mattermost==${MCP_SERVER_MATTERMOST_VERSION}" \
    && uv tool install --managed-python --compile-bytecode --python "${PYTHON_VERSION}" \
      "mcp-atlassian==${MCP_ATLASSIAN_VERSION}"
COPY --from=fetch /fetch/glab /fetch/slack-mcp-server /usr/local/bin/
USER crewlet
```

- **The Python servers** each get a virtual environment of their own from
  `uv`, on one Python that `uv` installs under `/opt/uv`; `uv` itself is
  mounted for that step and does not stay in the image. Python writes the
  bytecode of every module it imports beside the module, and on a read-only
  root it cannot, so without the two `--compile-bytecode` flags a server
  would compile its standard library and its dependencies again at every
  start, in every seat's process.
- **`RESOLVED_AT`** holds the servers' own dependencies, which their pins do
  not name, to what PyPI held on that date (`uv`'s `--exclude-newer`): a
  rebuild next month installs the set that was tested, not whatever was
  released since. A version released after it cannot be resolved, so move
  the date with the versions.
- **The Go servers** need no runtime: GitLab's release archive is checked
  against its published SHA-256, and Slack's binary is taken out of the npm
  package `npm exec` would have run, checked against the integrity npm's
  registry records for it.

Build it from the engine image,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet@sha256:<digest> \
  -t <your registry>/crewlet-tool-servers:<crewlet commit> .
```

check that each server starts in it as the engine's pod would start it: as
uid 10001 on a read-only root, with an empty `HOME` and `/tmp`, no
capabilities and no network, and with no environment but `PATH`, `HOME`
and the values it needs to start, as its `MCPServer` and a seat would give
them. That is stricter than the pod, which also hands a server each variable
on Crewlet's allowlist that the image or the pod sets
([Stdio and HTTP](#stdio-and-http)). And check
that it answers what the engine first asks a server: MCP's `initialize`,
then `tools/list`, which has to name a tool the integration's agents act
with. The client is the image's own `bash`, so your machine needs nothing but
`docker`, and the settings are placeholders, since nothing is reached:

```bash
image=<your registry>/crewlet-tool-servers:<crewlet commit>
pod=(--rm --read-only --network none --user 10001:10001 --cap-drop ALL
  --tmpfs /tmp --tmpfs /home/crewlet:uid=10001,gid=10001 -e HOME=/home/crewlet)
# lists [-q] TOOL [NAME=VALUE...] COMMAND [ARG...]: COMMAND, started with
# PATH, HOME and each NAME=VALUE and nothing else, answers MCP on stdio and
# lists TOOL; with -q, it also writes nothing to its standard error.
lists() {
  docker run -i "${pod[@]}" --entrypoint bash "$image" -s -- "$@" <<'CHECK'
quiet=; if [[ $1 == -q ]]; then quiet=1; shift; fi
tool=$1; shift
mkfifo /tmp/requests /tmp/answers
env -i PATH="$PATH" HOME="$HOME" "$@" </tmp/requests >/tmp/answers 2>/tmp/stderr &
exec 3>/tmp/requests 4</tmp/answers
trap '' PIPE
printf '%s\n' >&3 \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
while IFS= read -r -t 60 line <&4 && [[ $line != *'"id":2'* ]]; do :; done
exec 3>&-
wait
if [[ $line != *"\"name\":\"$tool\""* ]]; then
  echo "lists no $tool:" >&2; tail -n 20 /tmp/stderr >&2; exit 1
elif [[ $quiet && -s /tmp/stderr ]]; then
  echo "lists $tool, and wrote:" >&2; cat /tmp/stderr >&2; exit 1
fi
echo "lists $tool"
CHECK
}
lists post_message MATTERMOST_URL=https://chat.example.invalid \
  MATTERMOST_TOKEN=placeholder /usr/local/bin/mcp-server-mattermost
lists jira_add_comment JIRA_URL=https://jira.example.invalid \
  JIRA_USERNAME=check JIRA_API_TOKEN=placeholder /usr/local/bin/mcp-atlassian
lists -q glab_mr_note GLAB_CHECK_UPDATE=false GLAB_SHOW_WHATS_NEW=false \
  GLAB_SEND_TELEMETRY=false /usr/local/bin/glab mcp serve
docker run "${pod[@]}" --entrypoint /usr/local/bin/slack-mcp-server "$image" --help 2>/dev/null &&
  echo "runs slack-mcp-server"
```

A failing check prints the last lines the server wrote to its standard error,
as the engine's `server_stderr_tail` does. Each line hands its server the
values it needs to start, as the `MCPServer` below and a seat would give
them, and `env -i` starts the server with nothing else but `PATH` and
`HOME`: a server that relied on a variable of the image's `ENV` that
Crewlet's allowlist does not name, or on one Docker sets, would pass a check
that handed it the container's environment and fail in the pod. `glab`'s
line is `-q` because `glab` puts whatever a tool's command writes to its
standard error into the agent's answer ([GitLab](#gitlab)), and as
`glab mcp serve` itself stops, it writes to its own standard error what a
command would add to an answer: without `GLAB_CHECK_UPDATE` the update check
that cannot reach gitlab.com, without `GLAB_SHOW_WHATS_NEW`
`What's new in glab`, so the check fails. Without `GLAB_SEND_TELEMETRY` it
writes nothing there: what that switch stops is a request to the GitLab
instance, which a check with no network cannot see. The requests go in
through a pipe kept open until the answer is read: a server that sees the
end of its input stops, and may stop before it answers what it read.
`--help` would prove much less: a Python server parses its arguments before
it imports its own code, so its `--help` succeeds with none of its
dependencies installed. `slack-mcp-server` gets only `--help`, since it
checks its token with Slack as it starts, which needs the network: it shows
that the binary runs in the image, and no more.

Then name the image in the class; every company of the class runs it:

```yaml
# EngineClass with-tool-servers, excerpt
spec:
  engine:
    image: <your registry>/crewlet-tool-servers@sha256:<digest>   # crewlet <commit>
```

The four add about 480 MB to the image. Each running server is a process of
the engine's container, charged to its memory: started and asked for their
tools, `mcp-server-mattermost` held about 95 MB, `mcp-atlassian` 130 MB and
`glab` 50 MB. A server with `shared: false` runs once for every agent seat
it reaches, on the pod that holds the seat, so size the class's
`data.resources` (and `seatPools.defaults.resources`) for those processes.

### One image with a harness

A class names one engine image, so a company that also runs a seat on a
harness needs one image with both. Build one on the other: build the image
of [The image needs the harness](harnesses.md#the-image-needs-the-harness) for each
harness the company runs (Claude Code's, OpenCode's, or one built on the other)
and push it, then build this Dockerfile with `ENGINE_IMAGE` naming it,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet-cli@sha256:<digest> \
  -t <your registry>/crewlet-agents:<crewlet commit> .
```

run the checks above against the result, and [OpenCode's](harnesses.md#opencode)
too where OpenCode is in it, and name it in the class. Each Dockerfile takes
the image it extends as `ENGINE_IMAGE`, so none is edited. Pasting one into
another does not work: the second file's `ARG`s land inside the first one's
last stage, where the second's `FROM` lines cannot see them, and a build
produces only its last stage, so the paste either fails on a blank base image
or leaves out the first file's additions.

## Each integration's server

Which server each integration's agents use is Crewlet's choice, made in its
own pages; the object below is that server as an `MCPServer`, named as
Crewlet looks for it.

| Integration | Tool server | Runtime | In the image |
|---|---|---|---|
| Mattermost | [`mcp-server-mattermost`](https://pypi.org/project/mcp-server-mattermost/), from PyPI | Python | `/usr/local/bin/mcp-server-mattermost` |
| Jira, Confluence, an Atlassian organization | [`mcp-atlassian`](https://pypi.org/project/mcp-atlassian/), from PyPI, one server for both products | Python | `/usr/local/bin/mcp-atlassian` |
| Slack | [`slack-mcp-server`](https://www.npmjs.com/package/slack-mcp-server), from npm | none: a Go binary | `/usr/local/bin/slack-mcp-server` |
| GitLab | [`glab`](https://gitlab.com/gitlab-org/cli), GitLab's CLI, as `glab mcp serve` | none: a Go binary | `/usr/local/bin/glab` |
| GitHub | GitHub's remote server, `https://api.githubcopilot.com/mcp/` | none: `transport: HTTP` | nothing |
| Datadog | none named ([below](#datadog)) | — | — |
| Cloudflare | Cloudflare's remote server, `https://mcp.cloudflare.com/mcp` | none: `transport: HTTP` | nothing |
| Expo | none: an agent uses Expo through the EAS CLI in its executor runs ([Expo](integrations.md#expo)) | none | the EAS CLI, in the seat's box |
| DigitalOcean | DigitalOcean's remote MCP router, `https://mcp.digitalocean.com/mcp`, and doctl in its executor runs ([DigitalOcean](integrations.md#digitalocean)) | none: `transport: HTTP` | doctl, in the seat's box |
| Scaleway | [`scw`](https://github.com/scaleway/scaleway-cli), Scaleway's CLI, as `scw mcp server serve` | none: a Go binary | `/usr/local/bin/scw` |
| AWS | AWS's MCP Server, `https://aws-mcp.<region>.api.aws/mcp`, and the AWS CLI in its executor runs ([AWS](integrations.md#aws)) | none: `transport: HTTP` | the AWS CLI v2, in the seat's box |
| Google Cloud | Google's managed remote servers, one per product ([below](#google-cloud)) | none: `transport: HTTP` | nothing; the Google Cloud CLI in the seat's box, where its runs use it |

The servers below write their settings out as values
([Settings and credentials](#settings-and-credentials)). Each seat's own
credential reaches its instance of a server one of two ways, as
[Connecting Integrations](integrations.md) shows for each vendor: where the
engine provisions the vendor (GitLab or Mattermost under `provisioning:
Engine`, Datadog, Cloudflare, Scaleway, Google Cloud, an Atlassian
organization) the engine hands the server the credential it minted or issued
for the seat, and the Seat names none; everywhere else the seat gives its own
Secret in its `spec.mcp`. AWS's server is the exception to both: the engine
signs each request to it with the seat's own session and hands it no
credential at all ([AWS](#aws)).

The engine knows which variable or header each server below reads a seat's
credential under, by its command or its address. A per-seat server it does
not recognise, or one that reads it under another key, says so in
`seatCredentials`, which replaces what the engine would assume:

```yaml
spec:                       # MCPServer atlassian
  shared: false
  command: /usr/local/bin/mcp-atlassian
  seatCredentials:
    - {kind: Atlassian, field: Token, key: JIRA_API_TOKEN}
    - {kind: Atlassian, field: Email, key: JIRA_USERNAME}
```

Each entry names the integration (`Atlassian`, `Cloudflare`, `Datadog`,
`DigitalOcean`, `Expo`, `GitHub`, `GitLab`, `GoogleCloud`, `Mattermost`, `Scaleway` or
`Slack`), for Atlassian which of the account's two credentials (`Token` or
`Email`) and for Scaleway which of its key's (`SecretKey` or `AccessKey`), the
variable (Stdio) or
header (HTTP) the server reads it under, and an optional `prefix` such as
`"Bearer "`. It names no credential: the engine gives each seat's instance
that seat's own. A shared server acts as no seat and takes none. Such a server, declared or
recognised, reaches the agent seats on at its integration and takes no
`access` ([Choosing which agents get a server](#choosing-which-agents-get-a-server)).

### Mattermost

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: mattermost
spec:
  companyRef: {name: acme}
  shared: false                         # one per seat, on the seat's own bot token
  command: /usr/local/bin/mcp-server-mattermost
  env:
    - name: MATTERMOST_URL              # the address the engine reaches Mattermost at
      value: https://chat.acme.example
  toolPrefix: mattermost_
```

`MATTERMOST_URL` takes the Mattermost Integration's `url`, the address the
engine reaches the server at, and the same rule holds for it
([Mattermost](integrations.md#mattermost)): Mattermost has to answer its API
there without a redirect, on the scheme the address is served on and at the
Site URL's path. `mcp-server-mattermost` sends every call to that address
with `httpx`, which follows no redirect unless told to, and it never tells
it, so at an address Mattermost redirects no agent can post.

The prefix makes the server's tools `mattermost_post_message`,
`mattermost_update_message`, `mattermost_list_my_channels`,
`mattermost_list_public_channels` and `mattermost_search_users`, the names
Crewlet's bundled [tool skills](https://github.com/crewlet/crewlet/tree/main/examples/tool-skills)
trigger on: its [mentions skill](https://github.com/crewlet/crewlet/blob/main/examples/tool-skills/platform-mentions.md)
among them, which teaches a seat Mattermost's `@username` mentions and thread
replies before its first post. Under another prefix, or a release that
renames one of those tools, the skills go quiet without a word. Each seat
gives it `MATTERMOST_TOKEN`, the bot token of its
`spec.integrations.mattermost`
([Mattermost](integrations.md#mattermost); Crewlet's
[Mattermost](https://github.com/crewlet/crewlet/blob/main/docs/integrations/mattermost.md#configure-in-yaml)).

### Jira and Confluence

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: atlassian                       # where Crewlet reads a seat's Atlassian account
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/mcp-atlassian
  env:
    - name: JIRA_URL
      value: https://jira.acme.example
    - name: CONFLUENCE_URL
      value: https://wiki.acme.example
```

One server serves both products, so set the URL of each the company uses: the
engine does not derive them from the Integrations. Each seat gives it its
Atlassian account ([Jira and Confluence](integrations.md#jira-and-confluence);
Crewlet's [Jira](https://github.com/crewlet/crewlet/blob/main/docs/integrations/jira.md#mcp-server-agents-control-jira)).

Behind an egress proxy, `mcp-atlassian` takes the pod's `HTTPS_PROXY`,
which the engine hands it, spelled in capitals: signed in with a personal
access token it reads no `https_proxy`. A proxy for one product alone goes in
the server's `env`, as `JIRA_HTTPS_PROXY` or `CONFLUENCE_HTTPS_PROXY`, and so
does a proxy auto-config file, `ATLASSIAN_PROXY_WPAD_ENABLE` and
`ATLASSIAN_PROXY_WPAD_URL`, which also needs the server installed with its
`wpad` extra (`"mcp-atlassian[wpad]==${MCP_ATLASSIAN_VERSION}"` in the
Dockerfile above): the engine hands it none of those from the pod.
`mcp-atlassian` reads that file only when no `HTTP_PROXY`, `HTTPS_PROXY` or
`SOCKS_PROXY` reaches it, the pod's included, and otherwise ignores it
without a word. So where the pod sets a proxy, give `JIRA_HTTP_PROXY` and
`JIRA_HTTPS_PROXY`, and `CONFLUENCE_HTTP_PROXY` and `CONFLUENCE_HTTPS_PROXY`,
an empty `value` in the server's `env` beside the two settings: it takes a
product's variable, empty too, in place of the pod's, and the engine names
the four in its `empty_env_vars` warning, which here is expected
([its source at 0.23.1](https://github.com/sooperset/mcp-atlassian/blob/v0.23.1/src/mcp_atlassian/utils/proxy.py)).

### Slack

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: slack
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/slack-mcp-server
  args: [--transport, stdio]
  env:
    - name: SLACK_MCP_ADD_MESSAGE_TOOL  # without it, the server offers no tool that posts
      value: "true"
  toolPrefix: slack_
```

The prefix does here what it does on Mattermost: it makes the server's tools
`slack_conversations_add_message`, `slack_channels_list`,
`slack_channels_me` and `slack_users_search`, the names the same bundled
skills trigger on, so a seat posting on Slack is taught Slack's
`<@member-id>` mentions and thread replies before its first post. Under
another prefix, or a release that renames one of those tools, they go quiet
without a word (Crewlet's
[Slack](https://github.com/crewlet/crewlet/blob/main/docs/integrations/slack.md#configure-in-yaml)).

`slack-mcp-server` registers `conversations_add_message`, its only tool that
posts, when `SLACK_MCP_ADD_MESSAGE_TOOL` is set, or when
`SLACK_MCP_ENABLED_TOOLS`, the list of tools it offers, names it; a list that
leaves it out hides it whatever `SLACK_MCP_ADD_MESSAGE_TOOL` says. `true` lets
it post anywhere the bot is, and a list of channel IDs limits it to those. Its
reaction tools wait on `SLACK_MCP_REACTION_TOOL` the same way
([its settings](https://github.com/korotovsky/slack-mcp-server#environment-variables-quick-reference)).
Each seat gives it the bot token of its Slack app, the same key its
`spec.integrations.slack` names, and a channel cache of its own:

```yaml
spec:                       # Seat lead
  integrations:
    slack:
      enabled: true
      credentialsSecretRef: {name: lead-slack}    # keys botToken, signingSecret
  mcp:
    - name: slack
      env:
        - name: SLACK_MCP_XOXB_TOKEN
          valueFrom:
            secretKeyRef: {name: lead-slack, key: botToken}
        - name: SLACK_MCP_CHANNELS_CACHE          # this seat's own: no other seat names it
          value: /home/crewlet/.cache/slack-mcp-server/lead-channels.json
```

**Each seat names a channel cache of its own.** `slack-mcp-server` keeps the
channels its bot can see, the private channels and direct messages it is in
among them, in a file, and unless `SLACK_MCP_CHANNELS_CACHE` names one it
names that file after the Slack workspace alone,
`<team id>_channels_cache_v2.json` under `$HOME/.cache/slack-mcp-server`, and
reads it back for 24 hours before it asks Slack again
([its source at 1.3.0](https://github.com/korotovsky/slack-mcp-server/blob/v1.3.0/pkg/provider/api.go)).
Every seat's server on one engine pod runs as the engine's user, with one
`HOME`, so the seats whose bots are in one workspace share that file, and
whichever wrote it last decides what `channels_list` tells the others: that
bot's private channels and direct messages, named to seats whose bots are not
in them, and none of their own. So the path is each Seat's own, and never a
Unit's or the server's `env`, which every seat they reach shares. It is a
setting, not a credential, so it is a `value`
([Settings and credentials](#settings-and-credentials)). The server makes
`$HOME/.cache/slack-mcp-server` itself as it starts, for the other cache it
keeps there, so a file in it is written; `HOME` is emptied when the pod
starts, so a new pod's server asks Slack again, for its own bot alone.

**Behind an egress proxy, give it `SLACK_MCP_PROXY`.** `slack-mcp-server`
reads no `HTTPS_PROXY`: it makes its own connections, through the proxy
`SLACK_MCP_PROXY` names and otherwise none, so the pod's proxy, which the
engine hands it, does not reach its calls, and it cannot reach Slack. Set it
in the server's `env`, as a `value` unless the address holds a password. A
CA it trusts beside the system's, such as a proxy's that reads the traffic,
goes in `SLACK_MCP_SERVER_CA`, the path of a file in the image
([its source at 1.3.0](https://github.com/korotovsky/slack-mcp-server/blob/v1.3.0/pkg/transport/transport.go)).

### GitLab

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: gitlab
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/glab
  args: [mcp, serve]
  env:
    - name: GLAB_CHECK_UPDATE           # no update notice (or failed check) in an answer
      value: "false"
    - name: GLAB_SHOW_WHATS_NEW         # no "What's new" banner after an upgrade
      value: "false"
    - name: GLAB_SEND_TELEMETRY         # no usage report to the instance per call
      value: "false"
```

Each seat's instance gets the seat's GitLab token: from the engine under
`provisioning: Engine`, and from the Seat's `spec.mcp`, under `GITLAB_TOKEN`,
under `External` ([GitLab](integrations.md#gitlab)). On a self-managed
instance, give the server `GITLAB_HOST` as well, in its `env`,
and, when that instance serves plain HTTP, `GLAB_API_PROTOCOL` with the
value `http` beside it: `glab` takes the API's scheme from that setting,
`https` unless it says otherwise, never from the address in `GITLAB_HOST`.
GitLab marks `glab mcp serve` experimental, liable to change or go away, so
read `glab`'s release notes before you move its pin (Crewlet's
[GitLab](https://github.com/crewlet/crewlet/blob/main/docs/integrations/gitlab.md#mcp-tool-server)).

**`GLAB_CHECK_UPDATE` and `GLAB_SHOW_WHATS_NEW` keep `glab`'s own notices
out of an agent's answers, and `GLAB_SEND_TELEMETRY` keeps its usage reports
off the instance.**
`glab mcp serve` runs every tool call as a `glab` command of its own and
answers with everything that command wrote, its standard error as well as
its output, and after a command `glab` writes two notices to its standard
error unasked. Once a day, by a timestamp it keeps under `HOME`, which a new
pod starts without, it asks gitlab.com for a newer release and writes
`A new version of glab is available`, or, where gitlab.com cannot be reached,
`ERROR: failed checking for glab updates`, once the call has waited out the
connection; `GLAB_CHECK_UPDATE` turns that off. And the first command it runs
in a `HOME` it has not run in, which is every new pod's, and the first after
an upgrade, write `What's new in glab` and `Run: glab whatsnew`, which only
`GLAB_SHOW_WHATS_NEW` turns off. Either lands after the JSON the agent asked
for, so the answer is no longer that JSON. `glab` also reports every command
it runs to the GitLab instance, under the seat's token, and waits up to two
seconds for the instance to answer, so against a slow instance each tool call
takes two seconds longer, and so does the server's stop, which then runs
into the two seconds the engine allows a server before it signals it;
`GLAB_SEND_TELEMETRY` turns that off, and leaving it out lets GitLab's usage
statistics count your agents' commands. The three go in the server's `env`,
which every seat's process starts with: the engine hands a stdio server none
of the image's `ENV` but what Crewlet's allowlist names, and it names no
`GLAB_` variable ([Stdio and HTTP](#stdio-and-http)), so an `ENV` in the
Dockerfile above would reach a shell in the container and no `glab` the
engine starts. Spell each `"false"`: `glab` reads two of them as Go reads a
boolean, and a value it cannot read, such as `"no"` or `"off"`, puts a
warning of its own into every answer.

### GitHub

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: github
spec:
  companyRef: {name: acme}
  transport: HTTP
  shared: false
  url: https://api.githubcopilot.com/mcp/
```

A remote server: nothing goes into the image. The engine sends each seat's
instance an `Authorization` header with the installation token it issues for
the seat's own GitHub App, narrowed to the seat's `permissions` and `repos`
([Slack and GitHub](integrations.md#slack-and-github); Crewlet's
[GitHub](https://github.com/crewlet/crewlet/blob/main/docs/integrations/github.md#mcp-tools)).

### Datadog

Crewlet names no Datadog tool server. The engine hands each seat's instance
of the per-seat `MCPServer` named `datadog` the application key it minted
for the seat, as `DD_APPLICATION_KEY` ([Datadog](integrations.md#datadog)):
Datadog's remote server (`https://mcp.datadoghq.com/v1/mcp` on US1, over
`transport: HTTP`) reads it as a header, beside the organization's API key in
the server's own `DD_API_KEY` header. Name the server `datadog`, give it
`shared: false`, and, for a stdio server, install it in the image as above.

### Cloudflare

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: cloudflare
spec:
  companyRef: {name: acme}
  transport: HTTP
  shared: false
  url: https://mcp.cloudflare.com/mcp
```

A remote server: nothing goes into the image. The engine recognises it by its
address and sends each seat's instance an `Authorization: Bearer` header with
the account API token it minted for the seat, at the seat's level
([Cloudflare](integrations.md#cloudflare)). The server has no read-only mode
of its own, so what an agent can do is what its token's permissions allow,
and it refuses a token with an IP address filter, which the engine never sets.

### Scaleway

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: scaleway
spec:
  companyRef: {name: acme}
  transport: Stdio
  shared: false
  command: /usr/local/bin/scw
  args: [mcp, server, serve, "namespaces=account,audit-trail,block,cockpit,dns,instance,ipam,k8s,lb,object,rdb,redis,registry,vpc,vpc-gw"]
  env:
    - {name: SCW_DISABLE_CHECK_VERSION, value: "true"}
```

The engine image carries the `scw` binary at the release Crewlet pins, checked
against its published SHA-256 (Crewlet's
[Scaleway page](https://github.com/crewlet/crewlet/blob/main/docs/integrations/scaleway.md#the-agents-scaleway-tools)
names both). The engine recognises the server by its command and hands each
seat's instance the API key it minted for the seat as `SCW_SECRET_KEY` and
`SCW_ACCESS_KEY`, at the seat's level on its projects
([Scaleway](integrations.md#scaleway)). The `namespaces=` filter serves the
products the levels reach and nothing else, and the server has no project of
its own, so an agent names the project in a command that needs one.

### AWS

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: aws
spec:
  companyRef: {name: acme}
  transport: HTTP
  shared: false
  url: https://aws-mcp.eu-west-1.api.aws/mcp
```

A remote server: nothing goes into the image. Name the endpoint in the
company's home Region where AWS runs one (us-east-1, us-west-2, eu-west-1,
eu-west-2, eu-central-1, ap-southeast-1, ap-southeast-2 or ap-northeast-1),
else us-east-1. The engine recognises it by its address and signs each
request as the seat's own IAM role, with the session it issues for the seat
([AWS](integrations.md#aws)), so the server takes no header and no value, and
the Seat names none. It reaches only the agent seats that are on at AWS. An
executor's box is never given it, since the session never enters the box:
the AWS CLI there is the box's way to AWS. AWS authorizes every call a tool
makes as the seat's role, exactly as it authorizes the CLI's.

### Google Cloud

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: google-cloud-monitoring
spec:
  companyRef: {name: acme}
  transport: HTTP
  shared: false
  url: https://monitoring.googleapis.com/mcp
```

Google runs one remote server per product, so nothing goes into the image. The
engine recognises each by its host and sends each seat's instance an
`Authorization: Bearer` header with the access token it issues the seat's
service account at use, for an hour ([Google Cloud](integrations.md#google-cloud)).
Declare a per-seat `MCPServer` for each product the agents use, named as
Crewlet's catalogue names it:

| `MCPServer` | `url` |
|---|---|
| `google-cloud-monitoring` | `https://monitoring.googleapis.com/mcp` |
| `google-cloud-logging` | `https://logging.googleapis.com/mcp` |
| `google-cloud-compute` | `https://compute.googleapis.com/mcp` |
| `google-cloud-gke` | `https://container.googleapis.com/mcp` |
| `google-cloud-run` | `https://run.googleapis.com/mcp` |
| `google-cloud-sql` | `https://sqladmin.googleapis.com/mcp` |
| `google-cloud-storage` | `https://storage.googleapis.com/storage/mcp` |
| `google-cloud-resource-manager` | `https://cloudresourcemanager.googleapis.com/mcp` |

A server answers on a project only where the product's API is enabled there,
and only for an agent holding MCP Tool User on it, which every level grants.
What an agent can do through it is what its level grants on the project.

## A community registry

Crewlet's Settings › MCP & tools adds a server from a curated catalogue
compiled into the engine. A Company may also name a community registry, which
the same add flow then searches beside the catalogue:

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Company
metadata:
  name: nimbus
spec:
  companyName: Nimbus
  mcpRegistry:
    url: https://registry.modelcontextprotocol.io
```

`spec.mcpRegistry.url` is rendered as `mcp_registry.url`. It is the address of
a registry that speaks the MCP Registry v0.1 API, usually a mirror the company
curates, and it must be an `https` address with no credentials, query or
fragment; a private host is fine. The API server refuses anything else, as the
engine does. Unset, there is no community search.

A result is only ever a draft for Crewlet's add flow, marked as not reviewed,
and the engine dials nothing a listing names. A server added from one is
written into the company document by the dashboard, which a Company whose
`spec.config.managementPolicy` is `Enforce` refuses: on such a Company, an
MCPServer object is still how a server is added. See
[Crewlet's catalogue and community registry](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#the-curated-catalogue-and-a-community-registry).

## See also

- [Connecting Integrations](integrations.md) — each vendor's Integration and the seats' credentials for its server
- [Running Seats on a Harness](harnesses.md) — the other program a derived engine image carries
- [Crewlet's Tools & MCP](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md) — shared and per-seat servers, and what a server that fails costs
