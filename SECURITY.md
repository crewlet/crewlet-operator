# Security Policy

## Supported versions

The project is in alpha. Security fixes go to `main` and ship in the next
release; there are no backports to older releases. Once a `v1` API exists,
this section will name the release lines that receive fixes.

## Reporting a vulnerability

Please report vulnerabilities **privately** via
[GitHub's private vulnerability reporting](https://github.com/crewlet/crewlet-operator/security/advisories/new)
("Report a vulnerability" on the repository's Security tab). Do not open a
public issue, discussion or pull request for anything you believe is
exploitable.

Include what you can: the affected version or commit, the Kubernetes version,
a reproduction or proof of concept, and the impact you believe it has. You
should receive an acknowledgement within a few days; we will keep you updated
as we triage and fix, and credit you in the advisory unless you ask us not to.

A vulnerability in the Crewlet engine itself, rather than in how this
operator deploys or configures it, belongs to
[crewlet/crewlet](https://github.com/crewlet/crewlet/security/advisories/new).

## Scope notes for operators

The properties below are design rules of the operator. A behaviour that
breaks one of them is a vulnerability and should be reported as above.

- **Credentials are handled by reference, never by value.** No spec field
  of any `crewlet.ai` kind accepts a secret value; every credential is a
  reference to a Kubernetes `Secret`. The operator reads a referenced value
  only when it applies it, pushes it into the engine's encrypted secret
  store, and puts nothing but the store's name into the company document.
  The engine's own start-up credentials (its keyring, API tokens and
  object-store keys, and the class's telemetry headers and env values) go
  into `<company>-bootstrap`, the Secret the data pods mount as their
  `crewlet.yaml`, and into one `<company>-bootstrap-<pool>-<hash>` Secret per
  seat pool, which that pool's seat pods mount as theirs (the keyring and the
  object-store keys included). The operator never copies a secret value
  into a pod template, an environment variable, an annotation, a status
  field, an event or a log line.
- **Who may write a `crewlet.ai` object decides what an engine can reach.**
  A `Seat` that references a Secret makes that Secret's value available to
  the engine, so RBAC on these kinds is as sensitive as RBAC on the Secrets
  in the same namespace. References are same-namespace only; the one
  exception is the cluster-scoped `EngineClass`, which may reference a
  Secret in any namespace and so is as sensitive as reading every Secret:
  only platform administrators should be able to write one, and each should
  admit only the namespaces it is for (`spec.allowedNamespaces`).
- **The operator reads Secrets cluster-wide.** A Company may live in any
  namespace, so the manager has `get`, `list` and `watch` on Secrets in
  every namespace; there is no namespaced mode, and tenants are kept apart
  by namespace. It caches metadata only (`PartialObjectMetadata`), so the
  values of Secrets it was never pointed at are not held in its memory, and
  it fetches a value only for a Secret some object references. What it may
  write is narrower than its ClusterRole: a ValidatingAdmissionPolicy the
  chart installs holds it to the objects it makes
  ([Installing](docs/guides/installing.md#what-the-operator-may-do)).
- **The operator generates the engine's root secrets.** Each company's
  keyring and API tokens are Secrets the operator creates once and never
  regenerates. Anyone who can read them can decrypt that engine's secret
  store or act as its API clients; treat them like any other root
  credential, including in backups of etcd.
- **The engine's admin API is not exposed.** Only the engine's
  `/webhooks/` path is routed publicly, and `/mcp/`, the tool bridge, while
  an `LLMProvider` of the company runs a coding CLI in `mode: Agent`: each
  call there carries a token the engine signed for that one run. The
  engine's configuration, secrets and setup endpoints stay on the cluster
  network, where a NetworkPolicy (when the
  engine class enables isolation) limits who can reach them, unless the
  EngineClass routes the whole API (`exposure.api`) through a Gateway or an
  Ingress of your own, which should be internal. Your CNI must enforce
  NetworkPolicy for that layer to exist.
- **The data pods' leaf listener is unauthenticated.** While a company may
  run seat pools, its data pods listen on port 7422 for the seat pods to
  join the event stream, with no authentication and no TLS; whoever connects
  reads and writes the company's whole event stream. The NetworkPolicy
  `<company>-broker` admits the port only from pods carrying the company's
  seat-pod labels, so the listener is closed to other namespaces only where
  the CNI enforces NetworkPolicy, and within a namespace only where nobody
  but the operator creates pods: one company per namespace, or a namespace
  no tenant can create pods in
  ([Network policy](docs/concepts/runtime.md#network-policy)).
- **The manager's metrics endpoint requires authentication.** It is served
  over HTTPS and every request is authorised against the Kubernetes API,
  using a self-signed certificate unless one is provided.
- **Images are signed.** Release images are signed keylessly with cosign and
  carry a GitHub build-provenance attestation; [RELEASING.md](RELEASING.md)
  shows how to verify both before you deploy.
