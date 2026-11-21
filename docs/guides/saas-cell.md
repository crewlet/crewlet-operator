# Running a Hosted Cell

A hosted cell is a cluster where a platform runs one engine per customer and
customers reach their engines only through the platform: a gateway that signs
each request as the person who makes it, and a relay that forwards webhooks.
No engine has a hostname, a certificate or a route of its own. This guide
installs the operator for such a cell on top of
[Hosting Many Tenants](multi-tenancy.md), which describes the control plane's
side in general.

What the cell adds to a plain multi-tenant install:

| Concern | Where it is held |
|---|---|
| Who writes tenants' namespaces | The tenant provisioner's role and admission policy, bound to the control plane's ServiceAccount alone |
| Which labels a namespace carries | The same policy: the cell's labels are set once and never change, the mutable ones hold values from a closed set, and the namespace is named after its tenant |
| Which engines a tenant runs | Classes per tier and ring, each pinning its image by digest, held by the cell policy to internal-only exposure and isolation |
| Which version a tenant runs | Its ring's class; a Company in a cell may not name a version of its own |

---

## Install

The values below turn on the provisioner and the cell policy. The labels are
the cell's naming standard: the cell, its id, the environment, the tenant and
its organization never change once a namespace exists; the engine tier, the
trust tier, the ring and the quarantine state do, within the values listed.

```bash
cat > cell-values.yaml <<'EOF'
tenantProvisioner:
  enabled: true
  subjects:
    - kind: ServiceAccount
      name: provisioner
      namespace: platform
  namespaceLabels:
    tenant: crewlet.ai/tenant
    required:
      pod-security.kubernetes.io/enforce: restricted
    protectedPrefixes:
      - pod-security.kubernetes.io/
    immutable:
      - crewlet.ai/cell
      - crewlet.ai/cell-id
      - crewlet.ai/env
      - crewlet.ai/org
    requiredKeys:
      - crewlet.ai/cell
      - crewlet.ai/cell-id
      - crewlet.ai/env
      - crewlet.ai/org
      - crewlet.ai/engine-tier
      - crewlet.ai/trust-tier
      - crewlet.ai/ring
    allowedValues:
      crewlet.ai/env: [prod, staging, dev]
      crewlet.ai/engine-tier: [team, business]
      crewlet.ai/trust-tier: [t0, t1, t2, t3]
      crewlet.ai/ring: [r0, r1, r2, r3]
      crewlet.ai/quarantine: [egress, full]
    namePrefix: t-
cellPolicy:
  enabled: true
  classLabel: crewlet.ai/cell-class
  tenantPods:
    nodeSelector:
      crewlet.ai/pool: tenant
    tolerations:
      - {key: crewlet.ai/pool, value: tenant, effect: NoSchedule}
EOF
helm install crewlet-operator oci://ghcr.io/crewlet/charts/crewlet-operator \
  --namespace crewlet-system --create-namespace -f cell-values.yaml
```

`tenantPods` holds every pod created in a tenant's namespace, by anyone, to
the tenant node pool: its `nodeSelector` and its taint's toleration. A tenant
pod on a platform node would leave the cluster through the platform's egress
path instead of the tenants'. The classes below give their pods both.

The provisioner then creates a tenant's namespace only as `t-<tenant>`, with
every required label and the Pod Security level `restricted`. It may move a
tenant between rings, trust tiers and engine tiers and set or lift a
quarantine by relabelling, and nothing else about the labels: it can neither
re-home a namespace to another tenant, organization or cell nor lift its Pod
Security level. Inside a tenant's namespace, once it has bound the
tenant-access role there, it writes the namespace's ResourceQuota, LimitRange
and default-deny NetworkPolicies besides the Secrets and `crewlet.ai`
objects.

---

## Classes per tier and ring

Each engine tier has one class per ring, and the ring label on a tenant's
namespace selects it through `allowedNamespaces`. A ring's upgrade is one
image change on its classes; a tenant changes rings by relabelling its
namespace and naming the next ring's class. The cell policy holds every class
labelled `crewlet.ai/cell-class: "true"` to an image pinned by digest, the API
inside the cluster (`ClusterInternal`), no artifact route, webhooks through
the platform's relay (`External`) or none, `networkPolicy` `Isolated`, and
`allowedNamespaces` selecting on the tenant label.

| Class | Tier | Ring | Who is on it |
|---|---|---|---|
| `team-r0` | team | r0 | The platform's own workspaces, first to take a release |
| `team-r1` | team | r1 | Trials and workspaces that opted in to early releases |
| `team-r2` | team | r2 | Paying team workspaces |
| `business-r3` | business | r3 | Business workspaces, last |

```bash
kubectl apply -f - <<'EOF'
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: team-r0
  labels:
    crewlet.ai/cell-class: "true"
    crewlet.ai/engine-tier: team
    crewlet.ai/ring: r0
spec:
  allowedNamespaces:
    matchLabels: {crewlet.ai/engine-tier: team, crewlet.ai/ring: r0}
    matchExpressions: [{key: crewlet.ai/tenant, operator: Exists}]
  engine:
    image: ghcr.io/crewlet/crewlet@sha256:753d4a3a74f36c2e49ab16e463bec239e6cec7dd7e06a5cd69897ab88b13c1ba
  data:
    storage: {size: 20Gi}
  exposure:
    api: {mode: ClusterInternal}
    artifacts: {mode: None}
    webhooks:
      mode: External
      external:
        forwarders:
          - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
            podSelector: {matchLabels: {app.kubernetes.io/name: webhook-relay}}
  networkPolicy:
    mode: Isolated
    apiConsumers:
      - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
        podSelector: {matchLabels: {app.kubernetes.io/name: gateway}}
  podTemplate:
    spec:
      nodeSelector: {crewlet.ai/pool: tenant}
      tolerations: [{key: crewlet.ai/pool, value: tenant, effect: NoSchedule}]
  retention:
    backupOwner: platform-oncall
---
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: team-r1
  labels:
    crewlet.ai/cell-class: "true"
    crewlet.ai/engine-tier: team
    crewlet.ai/ring: r1
spec:
  allowedNamespaces:
    matchLabels: {crewlet.ai/engine-tier: team, crewlet.ai/ring: r1}
    matchExpressions: [{key: crewlet.ai/tenant, operator: Exists}]
  engine:
    image: ghcr.io/crewlet/crewlet@sha256:753d4a3a74f36c2e49ab16e463bec239e6cec7dd7e06a5cd69897ab88b13c1ba
  data:
    storage: {size: 20Gi}
  exposure:
    api: {mode: ClusterInternal}
    artifacts: {mode: None}
    webhooks:
      mode: External
      external:
        forwarders:
          - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
            podSelector: {matchLabels: {app.kubernetes.io/name: webhook-relay}}
  networkPolicy:
    mode: Isolated
    apiConsumers:
      - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
        podSelector: {matchLabels: {app.kubernetes.io/name: gateway}}
  podTemplate:
    spec:
      nodeSelector: {crewlet.ai/pool: tenant}
      tolerations: [{key: crewlet.ai/pool, value: tenant, effect: NoSchedule}]
  retention:
    backupOwner: platform-oncall
---
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: team-r2
  labels:
    crewlet.ai/cell-class: "true"
    crewlet.ai/engine-tier: team
    crewlet.ai/ring: r2
spec:
  allowedNamespaces:
    matchLabels: {crewlet.ai/engine-tier: team, crewlet.ai/ring: r2}
    matchExpressions: [{key: crewlet.ai/tenant, operator: Exists}]
  engine:
    image: ghcr.io/crewlet/crewlet@sha256:753d4a3a74f36c2e49ab16e463bec239e6cec7dd7e06a5cd69897ab88b13c1ba
  data:
    storage: {size: 20Gi}
  exposure:
    api: {mode: ClusterInternal}
    artifacts: {mode: None}
    webhooks:
      mode: External
      external:
        forwarders:
          - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
            podSelector: {matchLabels: {app.kubernetes.io/name: webhook-relay}}
  networkPolicy:
    mode: Isolated
    apiConsumers:
      - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
        podSelector: {matchLabels: {app.kubernetes.io/name: gateway}}
  podTemplate:
    spec:
      nodeSelector: {crewlet.ai/pool: tenant}
      tolerations: [{key: crewlet.ai/pool, value: tenant, effect: NoSchedule}]
  retention:
    backupOwner: platform-oncall
---
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: business-r3
  labels:
    crewlet.ai/cell-class: "true"
    crewlet.ai/engine-tier: business
    crewlet.ai/ring: r3
spec:
  allowedNamespaces:
    matchLabels: {crewlet.ai/engine-tier: business, crewlet.ai/ring: r3}
    matchExpressions: [{key: crewlet.ai/tenant, operator: Exists}]
  engine:
    image: ghcr.io/crewlet/crewlet@sha256:753d4a3a74f36c2e49ab16e463bec239e6cec7dd7e06a5cd69897ab88b13c1ba
  data:
    resources:
      requests: {cpu: "1", memory: 2Gi}
      limits: {memory: 8Gi}
    storage: {size: 50Gi}
  exposure:
    api: {mode: ClusterInternal}
    artifacts: {mode: None}
    webhooks:
      mode: External
      external:
        forwarders:
          - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
            podSelector: {matchLabels: {app.kubernetes.io/name: webhook-relay}}
  networkPolicy:
    mode: Isolated
    apiConsumers:
      - namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: platform}}
        podSelector: {matchLabels: {app.kubernetes.io/name: gateway}}
  podTemplate:
    spec:
      nodeSelector: {crewlet.ai/pool: tenant}
      tolerations: [{key: crewlet.ai/pool, value: tenant, effect: NoSchedule}]
  retention:
    backupOwner: platform-oncall
EOF
```

The digest above is an example; pin each ring to the release it runs. The
gateway's pods are the only API consumers besides the operator, and the
relay's pods the only peers admitted to the public port. Each class still
lets its engines connect out as `networkPolicy.egress` says
([Exposure and Network Policy](exposure.md)).

---

## A tenant

What the provisioner writes for one tenant, after binding the tenant-access
role in the namespace: the namespace, a default-deny policy for every pod in
it, and the Company on the class its labels select. The operator's own
policies then admit only what the engine needs.

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: t-w7k2m9q4xa
  labels:
    crewlet.ai/tenant: w7k2m9q4xa
    crewlet.ai/cell: aws-use2-sh-prod-0001
    crewlet.ai/cell-id: cel-7f3k9q2mxa
    crewlet.ai/env: prod
    crewlet.ai/org: q4n8t2c6pb
    crewlet.ai/engine-tier: team
    crewlet.ai/trust-tier: t1
    crewlet.ai/ring: r1
    pod-security.kubernetes.io/enforce: restricted
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
  namespace: t-w7k2m9q4xa
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: crewlet.ai/v1alpha1
kind: Company
metadata:
  name: w7k2m9q4xa
  namespace: t-w7k2m9q4xa
spec:
  engineClassName: team-r1
  companyName: w7k2m9q4xa
EOF
```

NetworkPolicies are additive: `default-deny` closes every pod the operator's
policies do not select, and those policies still admit what the engine needs.
The Company reports `Ready` with an endpoint inside the cluster alone
(`<company>-engine.<namespace>.svc`), and no Ingress, HTTPRoute or DNS record
exists for it.

---

## Mutual TLS inside the cell

With cert-manager installed, `internalTLS.enabled` adds the cell's internal
certificate authority: a ClusterIssuer named
`<release>-crewlet-operator-internal-ca`, whose CA key stays in
`internalTLS.clusterResourceNamespace` (cert-manager's namespace). The
platform's gateway and webhook relay request their client certificates from
it with a `Certificate` of their own. An engine verifies a client
certificate and serves TLS on its API port only once Crewlet supports it;
until then the class's `networkPolicy.apiConsumers` is what admits the
gateway alone, and the gateway's signed request assertion is what the engine
checks. The chart installs no cert-manager object unless this is on, so a
cluster without cert-manager installs it as before.

---

## See also

- [Hosting Many Tenants on the Operator](multi-tenancy.md): the control plane's side, in general
- [Installing the Operator](installing.md): the chart's values and the roles it adds
- [Exposure and Network Policy](exposure.md): what each exposure mode and policy admits
