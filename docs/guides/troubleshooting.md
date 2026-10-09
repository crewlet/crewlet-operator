# Troubleshooting

Every object the operator reconciles says what is wrong with it in its
conditions. Start at the Company: its `Ready` condition, when `False`, carries
the reason of the first of `Accepted`, `RuntimeReady`, `Exposed` and
`ConfigApplied` that is not `True`, and the message says what to do.

```bash
kubectl get crewlet -n acme
kubectl get company acme -n acme \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
kubectl get seats,units,llmproviders,mcpservers,integrations -n acme \
  -o jsonpath='{range .items[*]}{.kind}/{.metadata.name}{range .status.conditions[?(@.status!="True")]} {.type}={.status} {.reason}: {.message}{end}{"\n"}{end}'
kubectl events -n acme --for company/acme
```

Each condition's `observedGeneration` says which generation of the object it
describes; a condition older than `metadata.generation` has not caught up
with your last change yet.

## Conditions and reasons

### Company

| Condition | Status | Reason | What it means, and what to do |
|---|---|---|---|
| `Ready` | True | `Available` | The engine runs the current revision and is reachable as the class asks. |
| | False | `Suspended` | `spec.suspend` is set: the engine is stopped, its volume kept. |
| | False | `ConfigPending` | The engine runs and is exposed; the document of the Company's current generation has not been reported applied yet. Wait a few seconds. |
| | False | any other | The reason of the first condition below that is not True. |
| `Accepted` | True | `Valid` | The class exists and the company is within its limits. |
| | False | `EngineClassNotFound` | `spec.engineClassName` names no class, or it is unset and there is not exactly one class annotated `crewlet.ai/is-default-class: "true"`. Create the class or name one. |
| | False | `ReplicasNotAllowed` | `spec.topology.data.replicas` is not in the class's `data.allowedReplicas`. |
| | False | `LimitExceeded` | The company has more Seats or MCPServers than the class's `limits` allow. Remove some or move to a larger class. Nothing new is applied meanwhile. |
| `RuntimeReady` | True | `PodsReady` | The data pod runs the current template and is ready. |
| | False | `PodsNotReady` | The data pod is missing, starting or failing its readiness check. See [The data pod is not ready](#the-data-pod-is-not-ready). |
| | False | `Suspended` | `spec.suspend` is set. |
| | False | `DowngradeRefused` | The image asked for is an older version than the data was last written by; the engine migrates forward only. The pod keeps the newer image; every other change still applies. |
| | False | `VersionOverrideNotAllowed` | `spec.runtime.version` is set and the class's `engine.allowVersionOverride` is not. |
| | False | `KeyringInvalid` | The `<company>-keyring` Secret is not a keyring the engine can open. The operator never rewrites it: repair it from a backup, keeping the key ids, and set `crewlet.ai/active-key-id` to the key that seals new writes. The pod is held as it is. |
| | False | `KeyringMissing` | The `<company>-keyring` Secret is gone while the company's data StatefulSet or volume claims exist. A new keyring could not open the data, so none is generated. Restore the Secret from a backup, with its `crewlet.ai/active-key-id` annotation; or, to discard the data, delete the data volume claims, after which a new keyring is generated. The pod is held as it is. |
| | False | `PreviousCompanyObjects` | The keyring or tokens Secret of an earlier Company of the same name, deleted with `deletionPolicy: Delete`, has not been removed by the garbage collector yet. The pod starts once it is gone; nothing to do unless it stays, in which case check that the garbage collector runs. |
| | False | `NameConflict` | An object the operator writes for the Company (a Service, Secret, ConfigMap, ServiceAccount, StatefulSet or NetworkPolicy named after it) exists and is someone else's. The message names it. The operator never changes or adopts it: rename or delete it, or give the Company another name. The pod is held as it is. |
| | False | `SecretNotFound` | A Secret the pods are given values from is missing: one the class references (telemetry headers, `podTemplate.spec.env`) in the platform's namespace, the Company's own object-store keys (`spec.objects.credentialsSecretRef`), or a token in `<company>-api-tokens`. The pod is held as it is. |
| `Exposed` | True | `RouteAccepted` | Every route the class asks for was accepted. |
| | True | `NoRoutes` | The class routes nothing (webhooks `None` or `External`, API `ClusterInternal`). |
| | False | `RouteNotAccepted` | A Gateway has not attached the HTTPRoute, or the Ingress controller has not published an address. The message carries the controller's own reason; a Gateway listener's `allowedRoutes` that does not admit the tenant's namespace is the usual cause. |
| | False | `NameConflict` | An HTTPRoute or Ingress of the route's name (`<company>-webhooks`, `<company>-api`) exists and is someone else's; rename or delete it. |
| | False | `GatewayAPINotInstalled` | The class routes through Gateways and the cluster had no Gateway API when the operator started. Install it and restart the operator. |
| | False | `PublicBaseURLMissing` | The class's webhooks mode is `External` and the Company has no `spec.exposure.publicBaseURL`. |
| `SecretsSynced` | True | `Synced` | The engine's secret store holds every referenced value. |
| | False | `SecretNotFound` | A referenced Secret or key is missing; the message names each. |
| | False | `SecretTooLarge` | A value is over the 64 KiB the store accepts. |
| | False | `EngineUnavailable` | Writing to the store failed; the message says why. Retried. |
| | False | `NameConflict` | A Secret `<company>-secret-sync`, where the operator keeps what it pushed, exists and is someone else's. Rename or delete it; nothing is applied meanwhile. |
| | Unknown | `NotManaged` | The management policy keeps the operator out of the store (`Observe`; `Seed` once seeded). |
| `ConfigApplied` | True | `Applied` | The engine runs the rendered document; `status.config.revisionID` names the revision. |
| | True | `DriftCorrected` | Under `Enforce`, someone changed the document in the engine and the operator applied the rendered one over it. A `Drifted` event names what differed. |
| | True | `InSync` | Under `Observe`, the engine runs exactly the rendered document. |
| | True | `Seeded` | Under `Seed`, the engine has a document and owns it from now on. |
| | False | `RefNotFound`, `Cycle`, `Conflict`, `ReferenceNotAllowed`, `SecretNotFound`, `SecretTooLarge` | An object of the company keeps the document from rendering. The object at fault reports the same reason; fix it there. |
| | False | `Invalid` | The engine's dry run refused the document, in its own words. Each problem is also reported on the object that rendered it, as `Applied=False Invalid`. |
| | False | `Drifted` | Under `Observe`, the engine runs another document; the message names the places that differ. |
| | False | `EngineUnavailable` | The operator cannot reach the engine or is refused by it. Retried every ten seconds; see [The operator cannot reach the engine](#the-operator-cannot-reach-the-engine). |

### Unit, Seat, LLMProvider, MCPServer, Integration

| Condition | Status | Reason | What it means, and what to do |
|---|---|---|---|
| `Accepted` | True | `Valid` | Its Company exists and nothing collides with it. |
| | False | `CompanyNotFound` | `spec.companyRef` names no Company in the namespace. |
| | False | `CompanyNotAccepted` | Its Company is not `Accepted` (its class is gone, or it is over its class's limits); the message carries why. Nothing of the company is applied until it is. |
| | False | `Conflict` | Crewlet would read it and another object as one: two Seats with one handle, display name or contact account; two Units with one display name; two Integrations for one vendor; an LLMProvider named like a platform provider of the class. The message names the other object. |
| `ResolvedRefs` | True | `Resolved` | Every object and Secret key it references exists. |
| | False | `RefNotFound` | It references an object that does not exist (a Unit, a Seat, an LLMProvider, an MCPServer); the message names it. |
| | False | `Cycle` | A Unit's `parentRef` chain leads back to itself. |
| | False | `ReferenceNotAllowed` | A string it renders into the document holds a `${NAME}` reference, which the engine would resolve from its own secret store and environment. The message names the place in the document. Give a credential with a Secret reference; in a sandbox command, write a variable as `$NAME`. |
| | False | `SecretNotFound` | A Secret or key it references is missing. |
| | False | `SecretTooLarge` | A value it references is over 64 KiB. |
| `Applied` | True | `InRevision` | It is in the revision the engine runs, which the message names; the condition's `observedGeneration` is the generation that revision holds. |
| | False | `Invalid` | The engine refused its part of the document; the message is the engine's. |
| | False | `Blocked` | It is fine, and another object keeps the document from being applied (a document is applied whole). Fix the object the Company's `ConfigApplied` names. A part whose current generation already runs keeps `True` meanwhile. |
| | False | `Drifted` | Under `Observe`, the engine's document differs from this object's part. |
| | False | `NotManaged` | Under `Seed`, the engine owns the document; changes to this object are not applied. |
| `Healthy` (Integration) | True | `CheckPassed` | The engine reports the vendor connected. |
| | False | `CheckFailed` | The engine's check against the vendor failed; the message carries why (a revoked token, a missing scope). |
| | False | `NotExposed` | The Company has no public address, so no webhook can reach it: the class's webhooks mode is `None`. |

## The data pod is not ready

An engine is ready only once it runs a document, so a new company's pod is
not ready until `ConfigApplied` is `True`. When `ConfigApplied` is `True` and
`RuntimeReady` stays `False`, read the engine's log:

```bash
kubectl describe pod acme-data-0 -n acme
kubectl logs acme-data-0 -n acme | grep -E 'WARN|ERROR' | tail
```

- **`insufficient storage resources available`**: the engine could not
  reserve its event stream's logs. The stream is given three quarters of the
  data volume and needs at least 5 GiB of it; the class's `data.storage.size`
  admits no less than `7Gi`, but a volume created by an older operator, or a
  provisioner that hands out less than it was asked for, can be too small.
  Give the company a larger volume.
- **`config_apply_failed`**: the revision was stored but this node could not
  start it; the message says why. The engine keeps running the previous
  revision.
- **The pod restarts** (`kubectl get pod` shows restarts): the engine refused
  its own configuration at boot, and the first lines of the previous log say
  why (`kubectl logs acme-data-0 -n acme --previous`).
- **`Pending`**: the volume claim is not bound (no default StorageClass, or
  the class's `storageClassName` does not exist), or no node fits the class's
  resources, tolerations or affinity.

## The operator cannot reach the engine

`ConfigApplied=False EngineUnavailable` while the pod runs means the operator's
requests to `http://<company>-data-0.<company>-data.<namespace>.svc:8080` fail:

- A NetworkPolicy of your own refuses the operator's namespace. The operator's
  own policy admits it; check others selecting the data pods.
- The operator runs outside the cluster (`make run`): it cannot resolve the
  pod's DNS name.
- The `<company>-api-tokens` Secret was changed by hand and the pod has not
  restarted yet: it rolls on its own once the operator sees the change.

## Changes are not applied

- The object's `Applied` condition is `Blocked`: another object keeps the
  document back; the Company's `ConfigApplied` message names it.
- The Company is under `Observe` or `Seed`
  ([Choosing a management policy](management-policies.md)).
- A Secret change that leaves the value as it was writes nothing; a change to
  a value is applied as a new revision.
- Changes are coalesced: the operator waits for two quiet seconds after the
  last one, and ten seconds at most for a burst that never pauses.

## Webhooks do not arrive

Check `Exposed` on the Company and `Healthy` on the Integration, then that the
vendor has the address the status gives (`Seat.status.integrations`,
`Integration.status.webhookURL`) — they never change for a seat, and
`publicBaseURL` changes only with the class's host name template or the
Company's own `spec.exposure.publicBaseURL`.
[Exposure and network policy](exposure.md) covers the routes.

## Collecting what to report

```bash
kubectl get crewlet,statefulsets,pods,pvc,services,networkpolicies -n acme -o wide
kubectl get company acme -n acme -o yaml
kubectl events -n acme
kubectl logs acme-data-0 -n acme --tail=300
kubectl logs -n crewlet-operator-system deploy/crewlet-operator-controller-manager --tail=300
```

Conditions, events and logs name Secrets and keys, never their values, so
they are safe to paste into an issue. Do not paste Secrets.
