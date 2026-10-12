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
| `External` (the default) | made by someone else: a person, or a platform's own flow | no administrator credential, so the engine creates, changes and removes nothing at the vendor | a Secret its Seat references |
| `Engine` | made by the engine, and healed on every reconcile pass | the administrator credential the engine makes them with | a **provisioned** value: the engine mints it into its own store |

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

A seat names a credential the engine provisions with `provisioned` where it
would otherwise name a Secret key, in its tool servers' credentials
(`spec.mcp`), its sandbox environment (`spec.sandbox.env`) and its
Mattermost bot (`spec.integrations.mattermost.botToken`):

```yaml
valueFrom:
  provisioned:
    name: gitlab-token     # one value wherever this seat names it
```

The engine finds the value where its provisioning looks for a seat's
credential, which differs by vendor (below). A provisioned value anywhere
else is never filled, and so is one no Integration of the seat's company
creates: while none has `provisioning: Engine`, the seat is applied and
reports `ResolvedRefs=False NoProvisioner`, and its tool starts without the
value. A Unit's credentials always come from Secrets.

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
reports `Healthy=False NotExposed`; Mattermost and Atlassian need none.
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
      accessLevel: Developer
      accessLevels:
        - {name: lead, accessLevel: Maintainer}                 # by Seat name
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
  service account per agent seat, adds it to the group and to `projects`,
  and registers the webhook.

A seat's GitLab identity is its token for the `MCPServer` named `gitlab`
([`glab mcp serve`](tool-servers.md#gitlab), in the engine image; Crewlet
looks for the seat's GitLab credential there, under `GITLAB_TOKEN`,
`GITLAB_PERSONAL_ACCESS_TOKEN`, `Private-Token` or `Authorization`):

```yaml
spec:                       # Seat lead
  mcp:
    - name: gitlab
      env:
        - name: GITLAB_TOKEN
          valueFrom:
            provisioned: {name: gitlab-token}   # under Engine; a secretKeyRef under External
  sandbox:
    env:
      - name: GITLAB_TOKEN                      # the engine looks here too, for git
        valueFrom:
          provisioned: {name: gitlab-token}
```

A provisioned value is the credential alone, so provision a variable, such
as `GITLAB_TOKEN` of a Stdio server, rather than an `Authorization` header,
which would need a scheme in front of it.

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

Each agent seat speaks as its own bot, whose one token covers the websocket,
the REST calls and the Mattermost tool server:

```yaml
spec:                       # Seat lead
  integrations:
    mattermost:
      botToken:
        provisioned: {name: mattermost}   # under Engine; a secretKeyRef under External
      channel: leads                      # a channel the engine adds the bot to
  mcp:
    - name: mattermost
      env:
        - name: MATTERMOST_TOKEN
          valueFrom:
            provisioned: {name: mattermost}   # the same token
```

Under `External` a bot made by hand, or by a platform, is a Secret key, and
`username` names it when it is not the seat's handle.

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

With the organization connected, the engine creates a service account per
agent seat, grants it the Jira and Confluence sites, and mints its token; it
also discovers the sites, so the Jira and Confluence Integrations may leave
`url` and `cloudID` out. Each seat names the minted token and the account's
address as two provisioned values:

```yaml
spec:                       # Seat lead
  mcp:
    - name: atlassian
      env:
        - {name: JIRA_API_TOKEN, valueFrom: {provisioned: {name: atlassian-token}}}
        - {name: JIRA_USERNAME, valueFrom: {provisioned: {name: atlassian-email}}}
        - {name: CONFLUENCE_API_TOKEN, valueFrom: {provisioned: {name: atlassian-token}}}
        - {name: CONFLUENCE_USERNAME, valueFrom: {provisioned: {name: atlassian-email}}}
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
    role: Datadog Read Only Role
```

The engine registers the webhook at Datadog with the organization's keys, so
monitors name it as `@webhook-crewlet` (`webhookName`). An alert wakes the
seat its monitor's `crewlet:<handle>` tag names (`handleTag`), and
`routeTo` the one an alert naming nobody wakes; a Human seat cannot be one.
The engine also creates a service account, with `role`, for every agent seat
whose credentials for the `MCPServer` named `datadog` name an application
key (`DD_APP_KEY`, `DATADOG_APP_KEY` or `app_key`), and mints the key there:
name it as provisioned. Crewlet names no Datadog tool server: the server you
choose must read its key from one of those
([Datadog's server](tool-servers.md#datadog)).

## Slack and GitHub

Slack has no company-wide credential: each agent seat is its own app, in its
Seat's `spec.integrations.slack`. GitHub's Integration holds the webhook
secret of the company-wide route and an optional organization token, and
each agent seat is its own GitHub App, in `spec.integrations.github`.
[Supplying credentials](credentials.md#an-agents-slack-app) shows both.

A Slack agent posts through the `slack` tool server, `slack-mcp-server`, on
its app's bot token, which its Seat names a second time for the server; the
server offers its posting tool only when told to
([Slack's server](tool-servers.md#slack)). GitHub's tool server is GitHub's
own remote one, which needs nothing in the image
([GitHub's server](tool-servers.md#github)).

## A person's accounts

The engine creates no account for a person. A Human seat is tied to the
person's own account on each surface by its `spec.contact`, which renders to
the role's `contact` in Crewlet: agents mention the person there and message
them directly, and what the person does there, a post, a comment or a
review, reaches the engine as theirs. A Human seat needs at least one, and
takes one field for each surface it is reached on:

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
Jira and Confluence on, which the operator reads as Crewlet does: a product connected by `cloudID`, by a `url` whose
host is under `atlassian.net` or `jira.com` (or `atlassian.com`, for Jira),
or by neither, on the sites the Atlassian organization discovers, is on
Cloud, and any other `url` is a Data Center instance. Where either product
is on Cloud, which names a person by account id alone, a value that is no
account id, such as an email address, is applied and reported on the Seat as
`ResolvedRefs=False` with the reason `NotAnAccountID`, naming the products
on Cloud. Jira and Confluence share the one value, so a company with one
product on Cloud and the other on Data Center reaches the person on only
one of them.

One account belongs to one person: a Seat claiming an account another Seat
of the company claims reports `Accepted=False` with the reason `Conflict`,
naming the other, and holds the company's document until one of them gives
it up. GitHub logins and GitLab usernames are compared without case, as
those platforms compare them; the others exactly.

Crewlet's `crewlet_operator_id`, which binds one of the engine's API tokens
to a person so that whoever writes with it acts as them, has no field: the
engines the operator runs hold two tokens, the operator's own and the one
`Company.status.apiAccessSecretRef` hands every consumer of the company, and
neither is one person's.
[Humans in the Org Chart](https://github.com/crewlet/crewlet/blob/main/docs/concepts/humans-in-the-org.md)
describes what Crewlet does with each account.

## See also

- [Supplying Credentials](credentials.md) — writing and rotating each Secret
- [Exposure and Network Policy](exposure.md) — the public address vendors deliver to
- [Crewlet's integrations](https://github.com/crewlet/crewlet/blob/main/docs/index.md#integrations) — what each one does once connected
