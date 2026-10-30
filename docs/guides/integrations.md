# Connecting Integrations

An Integration connects a company to one vendor: Slack, GitHub, GitLab,
Mattermost, Jira, Confluence, Datadog, or an Atlassian Cloud organization.

Each renders one block of Crewlet's `integrations`, and a company has at
most one per vendor. This guide shows each vendor's object, the credentials
it takes, what the seats bring of their own, where the engine wants to be
told about the vendor, and how a person is tied to their account there.
Crewlet's own pages
([Integrations](https://github.com/crewlet/crewlet/blob/main/docs/index.md#integrations))
describe what each integration does once connected.

Every credential is a Secret reference ([Supplying credentials](credentials.md));
[Credential Positions](../concepts/credentials.md) lists every position and
the field that fills it.

An Integration connects the engine to the vendor. An agent acts there, its
chat replies included, through a tool server: an `MCPServer` its Seat gives
its own credentials. Most of those servers are programs the engine starts in
its own container, and Crewlet's image carries none of them:
[Running Tool Servers](tool-servers.md) builds an engine image with each
vendor's server and shows its `MCPServer`.

## Who creates the seats' accounts

`spec.provisioning` says who creates each agent seat's account at the vendor:

| `provisioning` | The seats' accounts | The Integration holds | A seat's credential |
|---|---|---|---|
| `External` (the default) | made by someone else: a person, or a platform's own flow | no administrator credential, so the engine creates, changes and removes nothing at the vendor | a Secret its Seat references, the seat's alone |
| `Engine` | made by the engine for each seat that is on the vendor, at the access its Seat names, and healed on every reconcile pass | the administrator credential the engine makes them with | minted by the engine into its own store, under a name it gives it for the seat; the Seat names none |

The engine can create accounts at GitLab, Mattermost, Datadog and in an
Atlassian Cloud organization, and only there: `Engine` on Slack, GitHub,
Jira or Confluence is refused, since the engine mints no credential there.
Datadog and Atlassian take `Engine` only: the engine creates the seats'
accounts with the keys they reference whenever it holds them.

> **Caution: an administrator credential is readable by everything the
> company runs.** Under `Engine` the Integration's administrator token or
> organization keys go into the company's secret store, which every engine pod
> of the company opens (the data pod and each seat pod hold its keyring), and
> so does every process that runs as the engine: its stdio MCP servers, a
> coding CLI, the code a seat runs. A seat steered by a prompt injection, or a
> compromised MCP package, is then one step from administering the vendor.
> Give the provisioner the narrowest credential it accepts: a GitLab group
> Owner's token rather than an instance administrator's, an Atlassian API key
> scoped to the organization's user management, Datadog keys of a service
> account limited to what provisioning needs.
> [Credential Positions](../concepts/credentials.md) says where each one goes.

Under `Engine` the engine hands the seat's credential to the vendor's tool
server and its own tools itself, so a Seat names none of that vendor's, and
Crewlet refuses one it names ([An agent's access](#an-agents-access)). A
seat's executor runs get no tool server's environment: a value they need,
such as the GitLab token git uses in the seat's box, is named in the seat's
`spec.sandbox.env` as **provisioned**, by the vendor and the field:

```yaml
valueFrom:
  provisioned: {vendor: GitLab, field: Token}   # the engine's GITLAB_TOKEN_<HANDLE>
```

While the company has no Integration of that vendor with
`provisioning: Engine`, nothing mints it: the seat is applied, its runs start
without the value, and it reports `ResolvedRefs=False NoProvisioner`
([Credentials the engine provisions](../concepts/credentials.md#credentials-the-engine-provisions)).

## An agent's access

Each agent Seat says, per vendor, whether it is on there, what it may do,
and where, in its `spec.integrations`. Each block renders to the role's
block of the same vendor in Crewlet's `integrations`. Where the engine
provisions the vendor it holds the vendor to the block on every reconcile
pass: it creates the account, gives it the access, narrows it to the scope,
and puts back a role or scope someone changed at the vendor, reporting that
it did.

```yaml
spec:                       # Seat sre
  integrations:
    datadog:
      enabled: true
      role: 4f0e2b3c-1a2b-11ef-9c2b-da7ad0900002   # a role id from GET /api/v2/roles
    github:
      enabled: true
      permissions: {contents: Read, issues: Write, pullRequests: Write}
      repos: [acme/backend]
    gitlab:
      enabled: true
      accessLevel: Reporter
      projects: [acme/infra]
    jira:
      enabled: true
      projectRole: "10101"                         # a role id from GET /rest/api/3/role
      projects: [OPS]
    confluence:
      enabled: true
      spaceRole: 8f3b6c1e-2d4a-4e5f-9a0b-1c2d3e4f5a6b
      spaces: [ENG]
    mattermost:
      enabled: true
      roles: [SystemUser, SystemPostAllPublic]
      channels: [town-square, incidents]
```

- **`enabled` is a stored setting, and every block states it.** A vendor
  with no block is off for the seat, which is how a new agent starts. `false`
  turns the seat off and keeps its account at the vendor, disabled, so its
  history keeps its author and turning it back on needs nobody (but on
  Slack, where a person approves the app's install again). Removing the
  account is a separate, confirmed action in the engine's dashboard, offered
  only while the seat is off.
- **Access is in the vendor's own terms.** Each field takes the identifier
  the vendor's API takes, shown in the engine's dashboard under the vendor's
  exact name:

  | Vendor | Access | Scope |
  |---|---|---|
  | Datadog | `role`: a role id, managed (Datadog Read Only Role, Standard, Admin) or custom | none |
  | GitHub | `permissions`: Contents, Issues, Pull requests, Checks, Actions and Workflows, each `Read` or `Write`; unset is No access | `repos`, the repositories the seat's tokens are narrowed to, within the Integration's `provisioning.repos` where it lists them |
  | GitLab | `accessLevel`: Guest, Planner, Reporter, Developer, Maintainer or Owner; or `memberRoleID`, a custom role on Ultimate | `projects`, within the Integration's `engineProvisioning.projects` where it lists them; unset is the whole group |
  | Jira | `projectRole`: a project role id of the site; what it may do is each project's permission scheme | `projects`, by key, among those a Unit's or a Seat's `spec.project` names |
  | Confluence | `spaceRole`: a space role id, where the site has role-based access; `operations` (`read:space`, `create:page`) where it does not | `spaces`, by key, among those a Unit's or a Seat's `spec.space` names |
  | Mattermost | `roles`: `SystemUser` (Member), alone or with one of `SystemPostAllPublic` (post:channels), `SystemPostAll` (post:all) and `SystemAdmin` | `channels`, within the Integration's `engineProvisioning.channels` where it lists them |
  | Slack | none: the bot token scopes of the app's manifest | none: the channels the bot is invited to |

  While a seat is on, its access is required: a Datadog `role`, a GitLab
  `accessLevel` or `memberRoleID`, a Jira `projectRole`, a Confluence
  `spaceRole` or `operations`, and Mattermost `roles`. A fixed set (GitLab's
  roles, Mattermost's roles, GitHub's permissions and levels) is checked when
  the Seat is written, and so is each required field; a scope outside the
  company's reach is refused by the engine's dry run, `Invalid` on the Seat. A role read live from the
  vendor (Datadog's roles, Jira's project roles, Confluence's space roles,
  GitLab's custom roles) is checked by the engine on every pass, and one the
  vendor no longer has is reported, never replaced by another.
- **The Seat is where access changes.** Under the Company's `Enforce`
  policy the engine's dashboard shows each agent's access read-only, like
  the rest of the document ([Applying](../concepts/applying.md#enforce-the-default)),
  and its "Keep the vendor's setting" and "Turn back on" offers are changes
  to the Seat. Whoever may write a Seat grants any role it names, the
  administration ones included (Datadog Admin Role, GitLab Maintainer or
  Owner, Jira Administrators, Confluence Admin, Mattermost System Admin,
  GitHub Contents or Workflows at `Write`), which the dashboard gates
  behind a fresh sign-in: hold the Seat to the same review with RBAC.
- **A credential is the engine's where the engine provisions the vendor,
  and the seat's own Secret where it does not.** Under an Integration with
  `provisioning: Engine` (GitLab, Mattermost, Datadog, an Atlassian
  organization), the engine mints the seat's credential, seals it under a
  name it gives it for the seat (such as `GITLAB_TOKEN_SRE`), and hands it to
  the vendor's per-seat tool server itself, under the variable or header the
  server reads. The Seat names none, in `spec.mcp` or in
  `spec.integrations`, and Crewlet refuses one it names there: the refusal
  comes from the engine's dry run, `Invalid` on the Seat. For every other
  vendor (Slack, GitHub, and GitLab, Mattermost, Jira and Confluence under
  `External`) the seat's credential is a Secret its Seat references, in
  `spec.integrations` or `spec.mcp`, and the Secret key is the seat's alone:
  Crewlet refuses one another seat or an Integration names too. There
  `enabled` turns the seat's own tools on and off, and the engine reports
  its access as managed outside Crewlet.

## Where webhooks arrive

Most vendors deliver webhooks to the company's public address
(`Company.status.endpoints.publicBaseURL`, see
[Exposure](exposure.md)). Each Integration's `status.webhookURL` is the
exact address its vendor delivers to, `<publicBaseURL>/webhooks/<vendor>`:

| Vendor | Webhooks | Registered by |
|---|---|---|
| Slack | per seat: `Seat.status.integrations.slack.requestURL` | you, in the seat's app |
| GitHub | `status.webhookURL`, and per seat: `Seat.status.integrations.github.webhookURL` | the engine, with `tokenSecretRef`; each seat's app |
| GitLab | `status.webhookURL` | the engine under `Engine`; you under `External` |
| Jira | `status.webhookURL` | the engine, with the administrator token |
| Confluence | `status.webhookURL` (Data Center), `status.webhookURL/<event>` (Cloud) | the engine, with the administrator token |
| Datadog | `status.webhookURL` | the engine, with the organization's keys |
| Mattermost | none: the engine holds a websocket per seat's bot | — |
| Atlassian | none: the organization is where accounts are made | — |

Without a public address an Integration whose vendor delivers webhooks
reports `Healthy=False NotExposed`; Mattermost and Atlassian need none. The
engine still registers the webhooks it registers, at the only address its
public listener has then, `http://<company>-engine.<namespace>.svc:8081`: no
vendor reaches it, and it names the company's Service and namespace to the
vendor. Give the company a public address before an Integration holds the
credentials the engine registers with.
`Healthy` is otherwise the engine's own check of the vendor. Jira,
Confluence and the Atlassian organization are one tool to the engine,
`atlassian`, so their three Integrations report one verdict.

## GitLab

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: gitlab
spec:
  companyRef: {name: acme}
  provisioning: Engine
  gitlab:
    url: https://gitlab.com
    signingSecretRef: {name: acme-gitlab, key: signingSecret}   # whsec_...
    tokenSecretRef: {name: acme-gitlab, key: readToken}         # optional, read_api
    engineProvisioning:
      group: acme
      adminTokenSecretRef: {name: acme-gitlab-admin, key: token}
      projects: [acme/api, acme/infra]                          # what a seat's projects may name
      groupWebhook: Auto
```

- `signingSecretRef` verifies every delivery. GitLab signs with a
  `whsec_` token over 32 random bytes; its *Generate signing token* button
  makes one, as does `printf 'whsec_%s' "$(openssl rand -base64 32)"`.
- `tokenSecretRef`, a personal access token with `read_api`, lets the engine
  read who takes part in an issue or a merge request. Without it a comment
  reaches only the people its delivery names.
- Under `Engine`, `adminTokenSecretRef` is a group Owner's personal access
  token with the `api` scope (an instance administrator's, with
  `accountOwner: Instance` on a self-managed instance). The engine creates a
  service account for each agent seat that is on GitLab, makes it a member
  of the group, or of the seat's own `projects`, at the seat's role, and
  registers the webhook. `projects` lists the projects a seat's `projects`
  may name.

An agent is on GitLab by its Seat's `spec.integrations.gitlab`
([An agent's access](#an-agents-access)). Under `Engine` the engine mints the
account's token, seals it as `GITLAB_TOKEN_<HANDLE>`, and hands it to the
seat's instance of the per-seat `MCPServer` named `gitlab`
([`glab mcp serve`](tool-servers.md#gitlab), in the engine image) under the
variable or header the server reads; the token follows the role, so below
Developer it reads the API and the repositories and cannot push:

```yaml
spec:                       # Seat lead
  integrations:
    gitlab:
      enabled: true
      accessLevel: Developer
      projects: [acme/api]
  sandbox:
    env:
      - name: GITLAB_TOKEN                      # the same token, for git in the box
        valueFrom:
          provisioned: {vendor: GitLab, field: Token}
```

Under `External` the seat's GitLab account is made outside the engine, which
changes nothing at GitLab, and its token is a Secret the Seat gives the
`gitlab` server in `spec.mcp`, under `GITLAB_TOKEN`, the seat's alone.

A person is tied to their GitLab account by their Seat's
`contact.gitlabUsername` ([A person's accounts](#a-persons-accounts)).

## Mattermost

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: mattermost
spec:
  companyRef: {name: acme}
  provisioning: Engine
  mattermost:
    url: https://chat.acme.example
    team: acme
    typingStatus: Addressed
    engineProvisioning:
      adminTokenSecretRef: {name: acme-mattermost-admin, key: token}
      usernamePrefix: agent-
      channels: [town-square]
      displayNameSuffix: " (AI)"
```

`url` is the address the engine reaches the server at: each seat's
websocket and the engine's REST calls, those that create the bots under
`Engine` included, go to it, and each seat's Mattermost tool server is given
the same address ([Mattermost's server](tool-servers.md#mattermost)). It
need not be where people go. They browse the server's Site URL
(`ServiceSettings.SiteURL`), from which Mattermost builds every link it hands
out; the engine builds no Mattermost link from `url`. Any route to the
server serves, the Site URL or the server's in-cluster Service while people
browse a public name: the engine's websockets carry no `Origin` header, so
Mattermost's origin check, which holds a browser's `Origin` to the Site
URL's scheme and host, never applies to them.

The server has to answer its API at `url` itself, without a redirect. The
engine follows none, and fails each call the server redirects with an error
naming where the server sent it; nor does the tool server, which is how an
agent posts. So `url` has:

- **the Site URL's path.** Mattermost serves its API at that path, whatever
  host a request arrives at, and redirects a request outside the path into
  it. A longer path is not redirected but reaches its web app, which answers
  a read with its page and a write with `405 Method Not Allowed`. A Site URL
  of `https://example.com/chat` takes a `url` ending in `/chat`, and a Site
  URL with no path a `url` with none.
- **the scheme its address is served on.** The other scheme is redirected
  or refused: Mattermost's own `Forward80To443` redirects a read from
  `http://` to `https://` and answers a write `400 Use HTTPS`, and a proxy
  that moves `http://` to `https://` does so with a redirect.

A connection that stays in the cluster has to pass the class's network
policy too ([Network policy](exposure.md#network-policy)). Under
`networkPolicy.mode: Isolated` the engine reaches every pod of the
Company's own namespace already, so a server running there needs nothing
more at its Service's address. Where `networkPolicy.egress.exceptCIDRs`
covers the addresses a connection to `url` reaches, `egress.to` has to
admit the pods it lands on: the server's pods for its Service, and, for a
Site URL served by an Ingress controller or a Gateway in the cluster, that
proxy's pods, wherever the server runs, because kube-proxy delivers a pod's
connection to a load balancer's address in the cluster straight to the pods
behind it.

Each agent seat speaks as its own bot, whose one token covers the websocket,
the REST calls and the Mattermost tool server. Under `Engine` the engine
creates the bot for each seat that is on Mattermost and mints its token, so
the Seat names none:

```yaml
spec:                       # Seat lead
  integrations:
    mattermost:
      enabled: true
      roles: [SystemUser]                 # Member; see An agent's access
      channels: [town-square, leads]      # within engineProvisioning.channels
```

Under `External` a bot made by hand, or by a platform, is a Secret key the
Seat names twice, for the seat's bot and for its `mattermost` tool server,
and `username` names the bot when it is not the seat's handle:

```yaml
spec:                       # Seat lead
  integrations:
    mattermost:
      enabled: true
      roles: [SystemUser]
      botToken:
        secretKeyRef: {name: lead-mattermost, key: botToken}
  mcp:
    - name: mattermost
      env:
        - name: MATTERMOST_TOKEN
          valueFrom:
            secretKeyRef: {name: lead-mattermost, key: botToken}   # the same token
```

The seat's websocket only listens: the agent posts through the `mattermost`
tool server, `mcp-server-mattermost`, which has to be in the engine image.
Without it the agent receives every message and answers none
([Mattermost's server](tool-servers.md#mattermost)).

A person is tied to their Mattermost account by their Seat's
`contact.mattermostUsername` ([A person's accounts](#a-persons-accounts)).

## Jira and Confluence

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: jira
spec:
  companyRef: {name: acme}
  jira:
    url: https://jira.acme.example          # or cloudID, with siteURL
    tokenSecretRef: {name: acme-jira, key: token}
    webhookSecretRef: {name: acme-jira, key: webhookSecret}
---
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: confluence
spec:
  companyRef: {name: acme}
  confluence:
    url: https://wiki.acme.example
    tokenSecretRef: {name: acme-confluence, key: token}
    webhookSecretRef: {name: acme-confluence, key: webhookSecret}        # Data Center
    webhookTokenSecretRef: {name: acme-confluence, key: webhookToken}    # Cloud
```

`tokenSecretRef` is an administrator account's API token (Cloud, with
`email` for basic authentication) or personal access token (Data Center):
the engine reads an issue's watchers with Jira's and searches with
Confluence's for a seat that has no credential of its own. A Data Center
instance signs deliveries with `webhookSecretRef`, which it then requires;
Confluence Cloud signs nothing, so its hooks carry `webhookTokenSecretRef`,
at least 26 random characters, in their address. The Company runs them as
its tracker and knowledge base with `spec.tracker.backend: Jira` and
`spec.knowledge.backend: Confluence`.

A seat's Jira and Confluence identity is one Atlassian account: its token
for the `MCPServer` named `atlassian`
([`mcp-atlassian`](tool-servers.md#jira-and-confluence), in the engine
image), or a product's own server, `jira` or `confluence`, under
`JIRA_API_TOKEN`, `CONFLUENCE_API_TOKEN` or
`ATLASSIAN_API_TOKEN` (a Data Center personal access token under
`JIRA_PERSONAL_TOKEN` or `CONFLUENCE_PERSONAL_TOKEN`), with its address
under `JIRA_USERNAME`, `CONFLUENCE_USERNAME` or `ATLASSIAN_EMAIL`. Without an
Atlassian Integration those come from Secrets.

A person is tied to their Atlassian account, on Jira and Confluence both, by
their Seat's `contact.atlassianAccount`: their account id on Cloud, their
username on Data Center ([A person's accounts](#a-persons-accounts)).

## An Atlassian Cloud organization

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: atlassian
spec:
  companyRef: {name: acme}
  provisioning: Engine
  atlassian:
    orgID: 1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d     # from admin.atlassian.com
    apiKeySecretRef: {name: acme-atlassian, key: apiKey}   # created without scopes
```

With the organization connected, the engine creates one service account for
each agent seat that is on Jira or Confluence, grants it each product the
seat is on, gives it the seat's project role in each of its Jira projects and
its space role (or space permissions) in each of its Confluence spaces, and
mints its token; it also discovers the sites, so the Jira and Confluence
Integrations may leave `url` and `cloudID` out. The engine's own tracker and
knowledge tools sign in as the seat with the token it sealed for it, so the
Seat names no Atlassian credential:

```yaml
spec:                       # Seat lead
  integrations:
    jira:
      enabled: true
      projectRole: "10101"      # the site's Developers role
      projects: [OPS]
    confluence:
      enabled: true
      spaceRole: 8f3b6c1e-2d4a-4e5f-9a0b-1c2d3e4f5a6b
      spaces: [ENG]
```

A Data Center deployment has no organization: leave this Integration out
and give each seat its token from a Secret.

## Datadog

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Integration
metadata:
  name: datadog
spec:
  companyRef: {name: acme}
  provisioning: Engine
  datadog:
    webhookTokenSecretRef: {name: acme-datadog, key: webhookToken}   # 26+ random characters
    routeTo:
      seatRef: {name: sre-lead}       # or dismiss: true
    site: EU1
    apiKeySecretRef: {name: acme-datadog, key: apiKey}
    appKeySecretRef: {name: acme-datadog, key: appKey}
```

The engine registers the webhook at Datadog with the organization's keys, so
monitors name it as `@webhook-crewlet` (`webhookName`). An alert wakes the
seat its monitor's `crewlet:<handle>` tag names (`handleTag`), and
`routeTo` the one an alert naming nobody wakes; a Human seat cannot be one.
The engine also creates a service account for every agent seat that is on
Datadog, with the role its Seat names by id, and mints the account's
application key:

```yaml
spec:                       # Seat sre
  integrations:
    datadog:
      enabled: true
      role: 4f0e2b3c-1a2b-11ef-9c2b-da7ad0900002   # GET /api/v2/roles
```

It seals the key under a name it gives it for the seat, `DATADOG_APP_KEY_<HANDLE>`,
and hands it to the seat's instance of the per-seat `MCPServer` named
`datadog` as `DD_APPLICATION_KEY`, the header Datadog's remote server reads;
the organization's API key is the server's own `DD_API_KEY` header, the same
for every seat ([Datadog's server](tool-servers.md#datadog)).

## Slack and GitHub

Slack has no company-wide credential: each agent seat is its own app, in its
Seat's `spec.integrations.slack`. GitHub's Integration holds the webhook
secret of the company-wide route and an optional organization token, and
each agent seat is its own GitHub App, in `spec.integrations.github`.
[Supplying credentials](credentials.md#an-agents-slack-app) shows both.

A Slack agent posts through the `slack` tool server, `slack-mcp-server`, on
its app's bot token, which its Seat names a second time for the server; the
server offers its posting tool only when told to
([Slack's server](tool-servers.md#slack)). Each Slack Seat also gives the
server a channel cache of its own, `SLACK_MCP_CHANNELS_CACHE`, as a literal
path in its `spec.mcp`: the server otherwise keeps the channels its bot can
see in one file per workspace, which every seat on an engine pod shares for a
day, and shows each seat the private channels and direct messages of
whichever bot wrote it last. GitHub's tool server is GitHub's
own remote one, which needs nothing in the image; the engine hands the seat's
instance of it the installation token it issues for the seat's app, narrowed
to the seat's `permissions` and `repos`
([GitHub's server](tool-servers.md#github)).

## A person's accounts

The engine creates no account for a person. A Human seat is tied to the
person's own account on each surface by its `spec.contact`, which renders to
the role's `contact` in Crewlet: agents mention the person there and message
them directly, and what the person does there, a post, a comment or a
review, reaches the engine as theirs. A Human seat takes one field for each
chat surface or tool it is reached on, and needs none: a person who works
only through the engine's dashboard has no account to name, and signs in on
their seat through the engine's identity directory
([Bringing people in](people.md)). The engine warns of a
Human seat with no chat contact, since a message addressed to it there
reaches nobody, and the operator records the warning as an `EngineWarnings`
event on the Company:

| Field | Surface | The value | Where to find it |
|---|---|---|---|
| `slackUserID` | Slack | the member id, such as `U0123ABCD` | the person's Slack profile: *More*, then *Copy member ID* |
| `mattermostUsername` | Mattermost | the username, such as `jane.doe`, without the `@`; never the 26-character user id | the person's Mattermost profile, under *Username* |
| `atlassianAccount` | Jira and Confluence | on Atlassian Cloud, the account id, such as `557058:f58131cb-b67d-43c7-b30d-6b58d40bd077` or `5b10ac8d82e05b22cc7d4ef5`, never the email address; on Data Center, the username, such as `jane.doe` | on Cloud, the `accountId` of Jira's `GET /rest/api/3/myself` called as the person, or of `GET /rest/api/3/user/search?query=<email address>`; on Data Center, the `name` of Jira's `GET /rest/api/2/myself`, or the `username` of Confluence's `GET /rest/api/user/current` |
| `githubLogin` | GitHub | the login, such as `jane-founder` | the person's profile address, `https://github.com/<login>` |
| `gitlabUsername` | GitLab | the username, without the `@` | the person's GitLab profile, after the `@` |

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: Seat
metadata:
  name: jane
spec:
  companyRef: {name: acme}
  kind: Human
  displayName: Jane Founder
  contact:
    mattermostUsername: jane.doe
    atlassianAccount: 557058:f58131cb-b67d-43c7-b30d-6b58d40bd077
    gitlabUsername: jane-doe
```

Each value is checked in its platform's own form when the Seat is written: a
Mattermost username in capitals, with an `@`, one Mattermost reserves
(`all`, `channel`, `matterbot`, `system`), and `here`, whose mention, like
`all` and `channel`, notifies the whole channel rather than the person, are
refused.

An Atlassian account takes any value of up to 255 characters without
whitespace or `${`, since a Data Center username may hold almost any
character, and it is checked against the deployment the company reaches
Jira and Confluence on, which the operator reads as Crewlet does, by
[one rule for both products](https://github.com/crewlet/crewlet/blob/main/docs/integrations/jira.md#configuration).
A product connected by `cloudID`, or by neither, on the sites the Atlassian
organization discovers, is on Cloud, and so is one whose `url` is on a host
of Atlassian's own Clouds: a site under `atlassian.net` or `jira.com`, or
under Atlassian Government Cloud's `atlassian-us-gov-mod.net`, or the API
gateway itself, `api.atlassian.com` or `api.atlassian-us-gov-mod.com`. Any
other `url` is a Data Center instance, other hosts under `atlassian.com`
among them: no customer's site is served there.

Where either product is on Cloud, which names a person by account id alone,
a value that is no account id, such as an email address, is applied and
reported on the Seat as `ResolvedRefs=False` with the reason
`NotAnAccountID`, naming the products on Cloud. Jira and Confluence share
the one value, so a company with one product on Cloud and the other on Data
Center reaches the person on only one of them.

Crewlet places a host that is not Atlassian's own by the deployment a
company declares in `integrations.atlassian.deployment`, which the operator
does not render. So Crewlet reads a site of
[Atlassian Isolated Cloud](https://support.atlassian.com/organization-administration/docs/ip-addresses-and-domains-for-atlassian-cloud-products/),
which is served at the customer's own base domain, as a Data Center
instance. For such a site:

- the product needs its `webhookSecretRef`, which Crewlet requires of a Data
  Center instance;
- Jira is spoken to over `/rest/api/2`, which Jira Cloud serves too, rather
  than `/rest/api/3`;
- Confluence's webhook is registered as on Data Center, one hook for every
  event, signed with that secret. Confluence Cloud signs no delivery and
  names no event in one, so no Confluence event reaches the company. Give
  Confluence's `url` with its `/wiki`, which Crewlet adds only to a site on a
  host it recognises;
- a person's `atlassianAccount` is not reported as `NotAnAccountID`,
  although the site names people by account id alone.

One account belongs to one person: a Seat claiming an account another Seat
of the company claims reports `Accepted=False` with the reason `Conflict`,
naming the other, and holds the company's document until one of them gives
it up. GitHub logins, GitLab usernames and Mattermost usernames are compared
without case, as those platforms compare them; Slack member ids and
Atlassian accounts exactly.

[Humans in the Org Chart](https://github.com/crewlet/crewlet/blob/main/docs/concepts/humans-in-the-org.md)
describes what Crewlet does with each account.

## See also

- [Supplying Credentials](credentials.md) — writing and rotating each Secret
- [Exposure and Network Policy](exposure.md) — the public address vendors deliver to
- [Crewlet's integrations](https://github.com/crewlet/crewlet/blob/main/docs/index.md#integrations) — what each one does once connected
