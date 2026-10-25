<div align="center">
<img src="assets/crewlet-operator-mascot.svg" alt="The Crewlet character on a stack of shipping containers, mug in hand, a ship's wheel beside it" width="240">
</div>

# Crewlet Operator Documentation

The Crewlet Operator runs Crewlet AI agent companies on Kubernetes: declare a
company and its org chart as custom resources, and the operator runs its
engine.

You write the `Company`, its units and seats, its model providers, tool
servers, integrations and schedules as `crewlet.ai` objects. The operator
renders them into the one company document [Crewlet](https://github.com/crewlet/crewlet)
runs on and applies it whole or not at all, pushes the credentials they
reference into the engine's encrypted store, and runs the engine's pods,
routes and network policy beside them.

New here? Read in this order: **[Quickstart](getting-started.md) →
[Installing the Operator](guides/installing.md) →
[How the Operator Models a Company](concepts/overview.md)**, then the guide
for what you are setting up.

> **Caution: alpha.** The `crewlet.ai/v1alpha1` API changes without notice.
> Each version of these pages describes the operator it was published with.

---

## Getting Started

- **[Quickstart](getting-started.md)** — Run the operator and a two-agent company on a local kind cluster: change it, break it on purpose, open its dashboard and remove it all again
- **[Installing the Operator](guides/installing.md)** — A production install with the Helm chart: prerequisites, the engine image to run, the values that matter, and the roles and admission policies it adds

## Core Concepts

How the operator turns objects into a running engine, one part per page:

- **[How the Operator Models a Company](concepts/overview.md)** — The eight kinds and how they relate, the identities that never change, credentials by reference, management policies and what every object reports
- **[What the Engine Is Given](concepts/company-document.md)** — The company document and each engine pod's `crewlet.yaml` the operator renders from the objects, where every seat is placed, and what blocks a document
- **[How the Document Is Applied](concepts/applying.md)** — What happens on every change: coalescing, the secret store, the dry run and the apply, management policies and drift, seat pauses, and every condition an object reports
- **[How a Company's Engine Runs](concepts/runtime.md)** — Every object the operator creates for a Company: the data pod, seat pools, exposure and network policy, versions, suspension, deletion and backups
- **[Credential Positions](concepts/credentials.md)** — Every position in a Crewlet company that holds a credential and the field that fills it, who can read a value, and how the engine's store entries are named

## Guides

- **[Supplying Credentials](guides/credentials.md)** — Write the Secret for each credential, rotate it without a restart, and find out why one did not arrive
- **[Connecting Integrations](guides/integrations.md)** — Slack, GitHub, GitLab, Mattermost, Jira, Confluence, Datadog and an Atlassian organization: each Integration, the seats' own credentials and the accounts the engine creates
- **[Running Tool Servers](guides/tool-servers.md)** — `MCPServer` objects: an engine image with each stdio server installed, the server each integration's agents use, and how to tell that one did not start
- **[Running Seats on a Coding CLI](guides/cli-agents.md)** — every model is a coding CLI: an engine image with the CLI, a subscription login or a key in a pod, host mode and box mode, and the provider a seat's code work runs on
- **[Exposure and Network Policy](guides/exposure.md)** — The public listener and the webhook routes through a Gateway, an Ingress or your own proxy, the internal API route, the dashboard's address and the policy around each engine
- **[Bringing People In](guides/people.md)** — A Human seat for each person, where people sign in, the two-step verification a class may make optional, the company's first person and everyone after, and why the operator's tokens hold no seat
- **[Choosing a Management Policy](guides/management-policies.md)** — `Enforce`, `Observe` and `Seed`: who owns the company document, which to choose, and how to move a running engine under the operator
- **[Choosing Where Seats Run](guides/topology.md)** — Data pods and stateless seat pools, Shared and PerSeat, what a pod of its own isolates and what it costs, and where each seat runs now
- **[Hosting Many Tenants on the Operator](guides/multi-tenancy.md)** — A control plane that provisions tenants: a namespace each, server-side apply, the status to read, credentials as Secrets, tiers as classes, offboarding
- **[Troubleshooting](guides/troubleshooting.md)** — Every condition and reason an object reports, what each one means, and what to do about it

## Reference

- **[API Reference](reference/api.md)** — Every kind and field of `crewlet.ai/v1alpha1`, generated from the API types with `make api-docs`

For working on the operator itself, see [CONTRIBUTING.md](../CONTRIBUTING.md);
for publishing it, [RELEASING.md](../RELEASING.md).
