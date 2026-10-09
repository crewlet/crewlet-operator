# crewlet-operator documentation

The operator is in alpha and its `crewlet.ai/v1alpha1` API is still changing,
so these pages describe what exists today and grow with it.

## Start here

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Running the operator and a small company on a local kind cluster, from source, and removing it all again |

## Guides

| Page | What it covers |
|---|---|
| [Installing the operator](guides/installing.md) | A production install with the Helm chart: prerequisites, the values that matter, the RBAC roles it adds, upgrades |
| [Supplying credentials](guides/credentials.md) | Writing the Secret for each credential, rotating it, and what an object reports when one is missing |
| [Connecting integrations](guides/integrations.md) | Slack, GitHub, GitLab, Mattermost, Jira, Confluence, Datadog and an Atlassian organization: each Integration, the seats' own credentials, and the accounts the engine creates |
| [Running seats on a coding CLI](guides/cli-agents.md) | `CLIAgent` providers: an engine image with the CLI, a subscription login in a pod, text mode and agent mode |
| [Exposure and network policy](guides/exposure.md) | Webhook routes through a Gateway, an Ingress or your own proxy, the public listener and the public address vendors and sandboxes use, load balancer health checks, the internal API route, where people reach the dashboard, the network policy around each engine |
| [Choosing a management policy](guides/management-policies.md) | Enforce, Observe and Seed: which to use, and how to move an engine under the operator |
| [Choosing where seats run](guides/topology.md) | Data pods and stateless seat pools, Shared and PerSeat pools, what a pod of its own isolates and what it does not, what pools cost, where each seat runs now, and what the current engine does not do |
| [Hosting many tenants on the operator](guides/multi-tenancy.md) | A control plane that provisions tenants: a namespace each, server-side apply with a field manager, the status to read, credentials from an OAuth flow |
| [Troubleshooting](guides/troubleshooting.md) | Every condition and reason an object reports, and what to do about each |

## Concepts and reference

| Page | What it covers |
|---|---|
| [How the operator models a company](concepts/overview.md) | The kinds and how they relate, the identities that never change, credentials, management policies and status |
| [Credentials](concepts/credentials.md) | Every position in a Crewlet company that holds a credential, and the field that fills it |
| [What the engine is given](concepts/company-document.md) | The company document and the data pod's and the seat pods' `crewlet.yaml` the operator renders from the objects, and what blocks a document |
| [How the document is applied](concepts/applying.md) | What the operator does on every change: coalescing, the secret store, the dry run and the apply; the management policies and drift; what every object reports |
| [How a company's engine runs](concepts/runtime.md) | The objects the operator creates for a Company, the data pod, seat pools, exposure and network policy, versions, suspension, deletion, and who writes which status field |
| [API reference](reference/api.md) | Every kind and field of `crewlet.ai/v1alpha1`, generated from the types with `make api-docs` |

For working on the operator itself, see [CONTRIBUTING.md](../CONTRIBUTING.md);
for publishing it, [RELEASING.md](../RELEASING.md).
