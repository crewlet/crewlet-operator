# Getting started

This page runs the operator on a local [kind](https://kind.sigs.k8s.io/)
cluster, first as a process on your machine and then as a Deployment in the
cluster. Every command is a make target or a tool the Makefile pins into
`bin/`, so nothing beyond Go, Docker, make, git and curl has to be installed.

> The `crewlet.ai/v1alpha1` API is defined and the API server enforces it.
> The operator accepts each Company and runs its engine, but does not yet give
> the engine its company document: the engine starts unconfigured, and the
> Company stays `Ready=False` with the reason `ConfigPending`.

## Create a cluster

```bash
make kind kubectl
bin/kind create cluster --name crewlet
bin/kubectl cluster-info --context kind-crewlet
```

Any cluster in your current kubeconfig context works the same way; kind is
only the quickest to throw away.

## Run the manager from your machine

```bash
make install    # install the CRDs into the cluster
make run        # build and run the manager against the current kubeconfig
```

`make run` regenerates the CRDs and RBAC first, so it always runs the code in
your working tree. The manager runs with your kubeconfig's credentials rather
than its own ServiceAccount, which is convenient while developing and means
RBAC mistakes do not show up here; the in-cluster deployment below catches
them.

In another terminal, list the kinds the CRDs added:

```bash
bin/kubectl api-resources --api-group=crewlet.ai
```

## Run the manager in the cluster

Build the image, load it into kind (nothing is pushed to a registry), and
deploy it either with kustomize or with the Helm chart:

```bash
make docker-build IMG=crewlet-operator:dev
bin/kind load docker-image crewlet-operator:dev --name crewlet

make deploy IMG=crewlet-operator:dev        # kustomize: config/default
# or
make helm-deploy IMG=crewlet-operator:dev   # Helm: dist/chart
```

The manager runs in `crewlet-operator-system`:

```bash
bin/kubectl -n crewlet-operator-system get pods
bin/kubectl -n crewlet-operator-system logs deploy/crewlet-operator-controller-manager
```

`make deploy` records the image in `config/manager/kustomization.yaml`; do
not commit that change.

## Apply the example company

[`config/samples`](../config/samples) is one complete company, Nimbus: an
`EngineClass`, the `Company`, two `Unit`s, three `Seat`s, an `LLMProvider`,
an `MCPServer` and two `Integration`s. Apply it into a namespace of its own:

```bash
bin/kubectl create namespace nimbus
bin/kubectl apply -n nimbus -k config/samples
bin/kubectl get crewlet -n nimbus
```

With the operator running, the Company reports that it is accepted: its
EngineClass exists and its seats and tool servers are within the class's
limits.

```bash
bin/kubectl get company nimbus -n nimbus \
  -o jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}'
# The Company runs with EngineClass standard and is within its limits.
```

The operator then creates the company's engine: its keyring and API token
Secrets, the data StatefulSet and its Services and, as the sample class asks, a
NetworkPolicy and a webhook route through the Gateway `gateway-system/public`
([How a company's engine runs](concepts/runtime.md) lists them all). A kind
cluster has no Gateway API, and the engine has no document to run yet, so
expect the conditions to say that the routes are not exposed and the pod is
not ready:

```bash
bin/kubectl get statefulsets,services,secrets -n nimbus -l crewlet.ai/company=nimbus
bin/kubectl get company nimbus -n nimbus \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} {.reason}{"\n"}{end}'
```

The objects reference Secrets (API keys, each agent's Slack and GitHub
credentials) that the samples do not include, because a credential is never
written into an object; [Credentials](concepts/credentials.md) lists which
field reads which key. Try changing an object so that it breaks a rule, and the
API server refuses it with the rule's message:

```bash
bin/kubectl patch seat ana -n nimbus --type merge -p '{"spec":{"handle":"anna"}}'
# The Seat "ana" is invalid: spec.handle: Invalid value: the seat's handle is
# immutable: the seat's identity and memory are derived from it
```

## Clean up

Delete the Company while the operator still runs: its finalizer waits for the
engine to stop and, with the sample's `deletionPolicy: Retain`, keeps the data
volume claim and the keyring Secret, which the namespace's deletion then
removes.

```bash
bin/kubectl delete company nimbus -n nimbus
make undeploy            # or: make helm-uninstall
bin/kubectl delete namespace nimbus
bin/kubectl delete engineclass standard
make uninstall           # the CRDs, and with them every crewlet.ai object
bin/kind delete cluster --name crewlet
```
