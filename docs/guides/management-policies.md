# Choosing a management policy

Crewlet's company document can be changed two ways: through the objects in
Kubernetes, which the operator renders and applies, and live, through the
engine's dashboard and API. `Company.spec.config.managementPolicy` says which
of the two owns the document. This guide helps you choose, and shows how to
move between policies. [How the document is applied](../concepts/applying.md#management-policies)
describes exactly what each one writes and reports.

| Policy | Who owns the document | Choose it when |
|---|---|---|
| `Enforce` (default) | The objects. An edit made in the dashboard is overwritten. | The objects are the source of truth: GitOps, or a platform that writes them for its tenants. |
| `Observe` | The engine. The operator compares and reports, and writes nothing. | You are bringing an engine that already runs under the operator, or trying the operator out on one. |
| `Seed` | The objects once, then the engine. | You want a company started from YAML and run from the dashboard afterwards. |

```yaml
spec:                       # Company
  config:
    managementPolicy: Enforce
```

## Enforce

Every change to an object becomes a revision. Every five minutes, and
whenever anything changes, the operator also compares the document the
engine runs with the rendered one. When someone changed it in the dashboard,
the operator applies the rendered document over it, records a `Drifted` event
on the Company naming the places that differed, and reports
`ConfigApplied=True` with the reason `DriftCorrected` until its next write.
The edit is not lost: it stays in the engine's revision history.

```bash
kubectl get events -n acme --field-selector involvedObject.name=acme,reason=Drifted
```

To keep a change made in the dashboard, make it in the objects too.

## Observe

The operator renders the document, has the engine dry-run it, and compares
it with what the engine runs, and changes nothing: no revision, no value in
the secret store. It reports the comparison:

```bash
kubectl get company acme -n acme \
  -o jsonpath='{.status.conditions[?(@.type=="ConfigApplied")].reason}: {.status.conditions[?(@.type=="ConfigApplied")].message}{"\n"}'
kubectl get seats -n acme \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.status.conditions[?(@.type=="Applied")].reason}{"\n"}{end}'
```

`InSync` means the engine runs exactly what the objects render; `Drifted`
names the places that differ, on the Company and on each object whose part
differs.

### Moving an engine under Enforce

Observe is the way to hand a running company to the operator without a
moment where the engine runs something else:

1. Create the Company and its objects with `managementPolicy: Observe`, and
   the Secrets they reference.
2. Edit the objects until the Company reports `ConfigApplied=True InSync`.
3. Switch the Company to `Enforce`. The operator pushes the Secret values
   into the engine's secret store and applies the same document once, as a
   revision of its own.

## Seed

The operator writes the document once, into an engine that has none, with
the Secret values it references, and from then on leaves the engine alone.
The Company reports `ConfigApplied=True Seeded`; every object reports
`Applied=False NotManaged`, because a change to it is no longer applied, and
`SecretsSynced` is `Unknown`. An engine that already had a document is never
written to.

Rotating a Secret is not applied under Seed either: rotate the credential
through the engine's own secret store (`crewlet secrets set`, or the
dashboard).

## Switching policies

The policy may change at any time:

| From → to | What happens |
|---|---|
| `Observe` → `Enforce` | The rendered document is applied once, with its Secret values; anything the engine had that the objects do not say is replaced. |
| `Seed` → `Enforce` | The same: the dashboard's changes since seeding are replaced by the rendered document. Copy what you want to keep into the objects first; `Observe` first shows you the differences. |
| `Enforce` → `Observe` or `Seed` | Nothing is written. The engine keeps the last revision, and the objects stop being applied. |
