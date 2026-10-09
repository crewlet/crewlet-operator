# Getting started

This page runs the operator on a local [kind](https://kind.sigs.k8s.io/)
cluster and starts a small company on it: an engine, a team of two agents and
a model provider. Every command is a make target or a tool the Makefile pins
into `bin/`, so nothing beyond Go, Docker, make, git and curl has to be
installed. It takes about ten minutes, most of them spent building two
images.

> The `crewlet.ai/v1alpha1` API is alpha and may change without notice.

## Create a cluster

```bash
make kind kubectl
bin/kind create cluster --name crewlet
bin/kubectl cluster-info --context kind-crewlet
```

kind's default CNI, kindnet, enforces NetworkPolicy, so the isolation the
operator sets up around each engine applies here as in production; see
[Exposure and network policy](guides/exposure.md).

## Build and load the images

The operator's image, and the Crewlet engine's. The engine is built from
source at the version the operator is pinned to (`CREWLET_VERSION` in the
Makefile), with Crewlet's own Dockerfile; `make crewlet-image` downloads
that version's module, checked against the Go checksum database, to do so.
Nothing is pushed to a registry: both images are loaded into the kind node.

```bash
make docker-build IMG=crewlet-operator:dev
make crewlet-image CREWLET_IMG=crewlet:dev
bin/kind load docker-image crewlet-operator:dev crewlet:dev --name crewlet
```

## Install the operator

With kustomize (`config/default`) or with the Helm chart (`dist/chart`):

```bash
make install deploy IMG=crewlet-operator:dev
# or
make helm-deploy IMG=crewlet-operator:dev HELM_EXTRA_ARGS="--set manager.replicas=1"
```

`make deploy` records the image in `config/manager/kustomization.yaml`; do
not commit that change. The chart runs two replicas with leader election by
default; one is plenty on a laptop.

```bash
bin/kubectl -n crewlet-operator-system rollout status deploy/crewlet-operator-controller-manager
bin/kubectl api-resources --api-group=crewlet.ai
```

## Create an EngineClass

An `EngineClass` is a platform's defaults and limits: the engine image, the size of each
company's data pod, and how companies are reached. This one runs the image
you loaded, exposes no webhooks (kind has no public address), and gives each
company the smallest data volume the API admits.

```bash
bin/kubectl apply -f - <<'EOF'
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: local
  annotations:
    crewlet.ai/is-default-class: "true"
spec:
  engine:
    image: crewlet:dev
  data:
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
    storage:
      size: 7Gi
  exposure:
    webhooks:
      mode: None
EOF
```

## Start a company

A namespace for the company, the Secret holding its model provider's API key,
and the objects: the `Company`, a `Unit`, two `Seat`s in it and the
`LLMProvider` they run on. The key is only read when an agent works, so any
value brings the company up; put a real Anthropic key in
`ANTHROPIC_API_KEY` for agents that can do something.

```bash
bin/kubectl create namespace acme
bin/kubectl create secret generic anthropic -n acme \
  --from-literal=apiKey="${ANTHROPIC_API_KEY:-not-a-real-key}"

bin/kubectl apply -n acme -f - <<'EOF'
apiVersion: crewlet.ai/v1alpha1
kind: Company
metadata:
  name: acme
spec:
  companyName: Acme
  mission: Make widgets people love.
  deletionPolicy: Delete
---
apiVersion: crewlet.ai/v1alpha1
kind: LLMProvider
metadata:
  name: anthropic
spec:
  companyRef:
    name: acme
  type: Anthropic
  model: claude-sonnet-5
  apiKeySecretRefs:
    - name: anthropic
      key: apiKey
---
apiVersion: crewlet.ai/v1alpha1
kind: Unit
metadata:
  name: engineering
spec:
  companyRef:
    name: acme
  displayName: Engineering
  type: Department
  purpose: Build the widgets.
---
apiVersion: crewlet.ai/v1alpha1
kind: Seat
metadata:
  name: lead
spec:
  companyRef:
    name: acme
  displayName: Engineering Lead
  unitRef:
    name: engineering
  goal: Decide what Acme builds next and keep the team unblocked.
  llm:
    default:
      - name: anthropic
---
apiVersion: crewlet.ai/v1alpha1
kind: Seat
metadata:
  name: dev
spec:
  companyRef:
    name: acme
  displayName: Developer
  unitRef:
    name: engineering
  goal: Build what the lead decides.
  llm:
    default:
      - name: anthropic
EOF
```

The operator creates the engine (a StatefulSet with one data pod and its
volume), pushes the API key into the engine's encrypted secret store, and
applies the company document. Wait for the Company to be ready, then look at
what it reports:

```bash
bin/kubectl wait company/acme -n acme --for=condition=Ready --timeout=5m
bin/kubectl get crewlet -n acme
bin/kubectl get company acme -n acme \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}{"\n"}{end}'
```

```
Accepted=True Valid
Exposed=True NoRoutes
RuntimeReady=True PodsReady
Ready=True Available
ConfigApplied=True Applied
SecretsSynced=True Synced
```

Each Seat reports its identity in the engine and the revision it is in:

```bash
bin/kubectl get seats -n acme \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.status.seatID} {.status.conditions[?(@.type=="Applied")].message}{"\n"}{end}'
```

## Change it

Edit an object and the operator applies the whole document again as a new
revision, once two seconds have passed without another change. The Seat's
`Applied` condition then names the new revision and the Seat's new
generation:

```bash
bin/kubectl patch seat dev -n acme --type merge \
  -p '{"spec":{"goal":"Build what the lead decides, with tests."}}'
bin/kubectl wait seat dev -n acme --timeout=1m \
  --for=jsonpath='{.status.conditions[?(@.type=="Applied")].observedGeneration}'=2
bin/kubectl get seat dev -n acme \
  -o jsonpath='{.status.conditions[?(@.type=="Applied")].message}{"\n"}'
```

Break a rule and the API server refuses the change with the rule's message:

```bash
bin/kubectl patch seat dev -n acme --type merge -p '{"spec":{"handle":"developer"}}'
```

Reference a Secret that does not exist and the next revision waits, while
the engine keeps running the last good one; the object at fault says why:

```bash
bin/kubectl patch llmprovider anthropic -n acme --type merge \
  -p '{"spec":{"apiKeySecretRefs":[{"name":"missing","key":"apiKey"}]}}'
bin/kubectl wait llmprovider anthropic -n acme --for=condition=ResolvedRefs=false --timeout=1m
bin/kubectl get llmprovider anthropic -n acme \
  -o jsonpath='{.status.conditions[?(@.type=="ResolvedRefs")].message}{"\n"}'
bin/kubectl patch llmprovider anthropic -n acme --type merge \
  -p '{"spec":{"apiKeySecretRefs":[{"name":"anthropic","key":"apiKey"}]}}'
```

[Troubleshooting](guides/troubleshooting.md) lists every condition and
reason an object can report.

## Open the dashboard

The engine serves its dashboard and API inside the cluster, behind a token.
The Company publishes a token for its consumers in the Secret
`status.apiAccessSecretRef` names:

```bash
bin/kubectl get secret acme-api-access -n acme -o jsonpath='{.data.token}' | base64 -d; echo
bin/kubectl port-forward -n acme svc/acme-engine 8080:8080
```

Open <http://localhost:8080>; when the dashboard asks for a token, paste the
one printed above.

## The full example

[`config/samples`](../config/samples) is one complete company, Nimbus: an
`EngineClass` that routes webhooks through a Gateway, two `Unit`s, three
`Seat`s (one of them a person) with their own Slack and GitHub credentials,
an `MCPServer` and two `Integration`s. On kind it shows what an object reports
when what it needs is missing: there is no Gateway API, and none of the
Secrets the samples reference exist. Point its class at the image you loaded
first:

```bash
bin/kubectl create namespace nimbus
bin/kubectl apply -n nimbus -k config/samples
bin/kubectl patch engineclass standard --type merge -p '{"spec":{"engine":{"image":"crewlet:dev"}}}'
bin/kubectl get company nimbus -n nimbus \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}: {.message}{"\n"}{end}'
```

## Run the manager from your machine

While working on the operator, `make run` runs the manager in your terminal
against the current kubeconfig, after `make install`. It regenerates the CRDs
and RBAC first, so it always runs the code in your working tree. It runs
with your kubeconfig's credentials rather than its own ServiceAccount, so
RBAC mistakes do not show up, and it cannot reach the engines it runs: it
addresses each by its pod's DNS name inside the cluster, so every Company
reports `ConfigApplied=False` with the reason `EngineUnavailable`. Stop the
in-cluster manager first (`make undeploy`), or the two compete.

## Clean up

Delete the companies while the operator still runs: each Company's finalizer
stops its engine and settles its volume as `deletionPolicy` says (`Delete`
here; the Nimbus sample's `Retain` keeps the volume claim and the keyring,
which the namespace's deletion then removes).

```bash
bin/kubectl delete company --all -n acme
bin/kubectl delete company --all -n nimbus --ignore-not-found
bin/kubectl delete namespace acme nimbus --ignore-not-found
bin/kubectl delete engineclass --all
make undeploy            # or: make helm-uninstall
make uninstall           # the CRDs, and with them every crewlet.ai object
bin/kind delete cluster --name crewlet
```
