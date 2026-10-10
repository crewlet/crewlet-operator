# Exposure and network policy

A company's engine listens on two ports. Port 8080 serves its API, its
dashboard and its probes; port 8081, its **public listener**, serves only the
routes outside parties call, each of which authenticates itself without an
operator credential: vendors' webhooks (`/webhooks/`), and a remote
sandbox's telemetry (`/otlp/{token}`) and tool calls (`/mcp/{token}`), with a
token signed for its one run in the path. Only the public listener needs to
be reachable from the internet, and only it is routed there. This guide sets
up the webhook routes, the public address vendors are given, the address
people reach the dashboard at, an internal route to the API, and the network
policy around each engine. The objects involved are described in
[How a company's engine runs](../concepts/runtime.md#exposure).

The split is the engine's own (Crewlet's `api.public`): the public listener
answers every other path, `/config` and `/health` included, as a path
nothing serves, whatever credential is sent, and the API port answers a
public route with `404` and a hint naming the public listener. Every data
pod opens both ports, whatever its class routes.

All of it is the platform's choice, made once per `EngineClass` in
`spec.exposure` and `spec.networkPolicy`; a Company only states its own
address when the class leaves that to it.

## Choose how webhooks arrive

| `exposure.webhooks.mode` | Use it when | The Company's public address |
|---|---|---|
| `Gateway` | The cluster runs a Gateway API implementation | `https://<hostname>`, or the Company's `spec.exposure.publicBaseURL` |
| `Ingress` | The cluster has an Ingress controller | the same |
| `External` | Something outside the operator (a CDN, a tunnel, a load balancer you manage) forwards to the engine | the Company's `spec.exposure.publicBaseURL`, which is then required |
| `None` (the default) | Nothing reaches the engine from outside: development, or companies whose integrations deliver no webhooks (Mattermost, an Atlassian organization) | none; integrations that need inbound delivery report `Healthy=False NotExposed` |

Whatever the mode, the route sends every path of its host to the public
listener (the engine Service's port `public`, 8081), and never to the API
port. It names no path prefix: the listener itself is the boundary, and
Crewlet decides which routes are public from one list of its own, so a
route that repeated `/webhooks/`, `/otlp/` and `/mcp/` would add no
protection the listener does not give (the admin API is not on that port to
be reached by any path) and would be a second list to keep in step, which a
Crewlet release serving a new public route would outgrow with nothing to
say so. What the public listener serves depends on what the engine is given:

- `/webhooks/`: always. A delivery for a vendor the company has not
  connected is refused (`503`).
- `/mcp/{token}`, the tool bridge, while an `LLMProvider` of the company runs
  a CLI in `mode: Agent`: that run happens in a remote sandbox, which calls
  the seat's tools back through it ([Running seats on a coding
  CLI](cli-agents.md)). The operator then gives the engine the public address
  as the bridge's (`CREWLET_MCP_BRIDGE_URL`); without one the engine mounts
  no bridge and `/mcp/` is a path nothing serves, and every agent-mode turn
  fails: the provider reports `AgentModeRunnable=False`, `NotExposed`, and
  each seat whose executor's chain it heads `Ready=False`, `NotExposed`,
  while the rest of the company's document applies.
- `/otlp/{token}`, the sandbox telemetry receiver, while the class sends the
  engine's telemetry to a collector (`spec.telemetry`): a remote sandbox's
  coding agent exports its traces there, and the engine forwards them to the
  collector with the class's headers, which never enter the sandbox. The
  operator gives the engine the public address as the receiver's
  (`CREWLET_SANDBOX_OTEL_RECEIVER_URL`).

The address appears in the Company's `status.endpoints.publicBaseURL`, and
each Seat's and Integration's status carries the exact URL to register with
the vendor.

### Through a Gateway

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: EngineClass
metadata:
  name: standard
spec:
  # engine, data: as for any class
  exposure:
    webhooks:
      mode: Gateway
      gateway:
        parentRefs:
          - namespace: gateway-system
            name: public
            sectionName: https        # optional: one listener
      hostname: "{{uid}}.hooks.example.com"
```

The operator creates an HTTPRoute `<company>-webhooks` in each company's
namespace, attached to the Gateway, for the expanded host name. The template
must contain `{{uid}}`, or `{{name}}` and `{{namespace}}` joined by a `.`, so
that no two companies share a host. A `-` between them is refused: names and
namespaces hold dashes, so `{{namespace}}-{{name}}` is `team-a-b-web` for both
`team-a`/`b-web` and `team-a-b`/`web`, and whichever route came first would
receive the other tenant's deliveries. `{{uid}}` keeps the address stable for
the Company's lifetime and reveals nothing about the tenant. Point a wildcard DNS record
(`*.hooks.example.com`) and a wildcard certificate at the Gateway.

The Gateway's listener must admit routes from the tenants' namespaces
(`allowedRoutes.namespaces`), for example by a label every tenant namespace
carries:

```yaml
listeners:
  - name: https
    protocol: HTTPS
    port: 443
    hostname: "*.hooks.example.com"
    allowedRoutes:
      namespaces:
        from: Selector
        selector:
          matchLabels:
            platform.example.com/tenant: "true"
```

The Company reports `Exposed=True RouteAccepted` once every Gateway the route
names reports it accepted, and `Exposed=False RouteNotAccepted` with the
Gateway's reason until then. The operator looks for the Gateway API when it
starts: if you install it later, restart the operator, or every company of a
Gateway class reports `GatewayAPINotInstalled`.

### Through an Ingress

```yaml
spec:
  exposure:
    webhooks:
      mode: Ingress
      ingress:
        className: nginx
        annotations:
          cert-manager.io/cluster-issuer: letsencrypt
        tls:
          secretName: "{{name}}-webhooks-tls"     # in the Company's namespace
        controllerNamespaces: [ingress-nginx]   # required with networkPolicy.mode: Isolated
      hostname: "{{uid}}.hooks.example.com"
```

The Ingress `<company>-webhooks` is accepted once the controller publishes an
address for it. Its TLS Secret lives in the Company's namespace, so it is
issued per company, here by a certificate controller from the Ingress's
annotations.

### Load balancer health checks

The public port answers `404` on every path it does not serve, `/` and the
probe paths included, so a load balancer that health-checks a backend on the
port it routes to marks every engine unhealthy and drops every delivery.
Controllers that run in the cluster (ingress-nginx, Envoy Gateway and most
Gateway implementations) send no such check by default; cloud load balancers
do. Point their check at port `http` (8080), path `/ready`, which every engine
pod answers `200` once it serves:

- **AWS Load Balancer Controller** (ALB, `target-type: ip`): checks `GET /`
  and expects `200`. Set the class's `ingress.annotations`:

  ```yaml
  alb.ingress.kubernetes.io/target-type: ip
  alb.ingress.kubernetes.io/healthcheck-port: "8080"
  alb.ingress.kubernetes.io/healthcheck-path: /ready
  ```

- **GKE Ingress**: takes its check from the pod's readiness probe only when
  that probe is on the port the Ingress sends to, and otherwise checks
  `GET /` there. Create a `BackendConfig` in each company's namespace whose
  `healthCheck` is `{type: HTTP, port: 8080, requestPath: /ready}`, and name
  it on the engine Service through the class's `service.annotations`:

  ```yaml
  spec:                       # EngineClass
    service:
      annotations:
        cloud.google.com/backend-config: '{"ports": {"public": "crewlet-public"}}'
  ```

- **GKE Gateway**: create a `HealthCheckPolicy` in each company's namespace
  whose `targetRef` is the Service `<company>-engine`, with the same check.

These load balancers deliver to the pods from addresses outside the pod
network, which a NetworkPolicy cannot admit by namespace: run their classes
with `networkPolicy.mode: None` and isolate the engines with your cloud's or
CNI's own policy.

### From outside the operator

With `mode: External` the operator creates no route. Forward everything
under `https://<your address>/` to the public port of the Service
`<company>-engine.<namespace>.svc:8081` (port name `public`), keeping the
path, and give each Company its address:

```yaml
spec:                       # Company
  exposure:
    publicBaseURL: https://acme.hooks.example.com
```

Never forward to port 8080: that is the admin API, and it answers a webhook
`404` anyway. Under `networkPolicy.mode: Isolated`, name the pods that
forward (a tunnel's or a relay's pods in the cluster) in the class, so the
policy admits them to the public port and to nothing else. The class is
refused without them, since the policy would then refuse everything
forwarded, and refused with them under any other mode, which has no policy
to admit them to:

```yaml
spec:                       # EngineClass
  exposure:
    webhooks:
      mode: External
      external:
        forwarders:
          - namespaceSelector:
              matchLabels: {kubernetes.io/metadata.name: tunnel}
            podSelector:
              matchLabels: {app: cloudflared}
```

A forwarder whose traffic does not come from a pod (a load balancer of your
own that delivers from the nodes or from outside the cluster) cannot be
admitted by namespace; isolate those engines with your CNI's own policy and
`networkPolicy.mode: None`.

A Company may set `publicBaseURL` under the other modes too, when the
address vendors use differs from the route's host (a CDN in front of the
Gateway, say).

## Reach the API from elsewhere

`status.endpoints.api` is always the in-cluster address,
`http://<company>-engine.<namespace>.svc:8080`, and that is all most
consumers need: see [Reaching the engine](../concepts/runtime.md#reaching-the-engine).
To reach the API from outside the cluster, for a dashboard behind your own
single sign-on for instance, route it through an internal Gateway or
Ingress:

```yaml
spec:
  exposure:
    api:
      mode: Gateway
      gateway:
        parentRefs:
          - {namespace: gateway-system, name: internal}
      hostname: "{{name}}.{{namespace}}.crewlet.internal.example.com"
```

The route `<company>-api` carries every path of the API port (8080): the
API, the dashboard and the probes, and none of the public routes, which
that port does not serve. Put it only on a listener that is not public. Its
host name must differ from the webhooks' (the class is refused otherwise):
both routes take every path of their host, so on one host the Gateway or
Ingress controller would pick which port answers, and the public host could
serve the admin API.
`status.endpoints.exposedAPI` is its address once accepted. Every request
still needs a token.

## Where people reach the dashboard

The engine's agents put links to the dashboard in what they write for
people (a tool skill's `${crewlet_base_url}`, the link in a page change's
notification), and build them on Crewlet's
`integrations.dashboard_base_url`. That is never the public address, whose
listener serves no dashboard. The operator writes it from, in order:

1. the Company's `spec.exposure.dashboardBaseURL`, an `http` or `https` URL,
   for an address people reach the dashboard at that the class does not
   route (a single sign-on proxy in front of the API's route, a VPN
   address);
2. the API route's address: its expanded `api.hostname`, on `https` when
   the Ingress has `tls.secretName` or a Gateway listener the route can
   attach to is HTTPS, and on `http` otherwise. Unlike `exposedAPI`, it
   does not wait for the route to be accepted. It is rendered into the
   company document, so an address that followed acceptance would apply a
   new revision on every engine each time a Gateway controller re-evaluated
   the route, and would never be known behind an Ingress controller that
   publishes no address.

The result is `status.endpoints.dashboardBaseURL`. With neither, the engine
composes no link for a person (a skill shows `${crewlet_base_url}` as
written), and the Company's `Exposed` condition says so in its message.

A link is the base followed by one of the engine dashboard's own routes,
such as `<base>/#/work/ENG-42` for a work item and
`<base>/#/knowledge/pages/<page id>` for a page. So an address other than
the API route's must serve the engine's dashboard there, or redirect those
routes to a view of its own: the part after `#` never reaches a server, so
that view reads it in the browser.

```yaml
spec:                       # Company
  exposure:
    dashboardBaseURL: https://crewlet.sso.example.com/acme
```

## Network policy

With `networkPolicy.mode: Isolated`, the default, the operator creates a
NetworkPolicy `<company>-engine` around the company's data pods. It admits
each of the engine's two ports from its own sources and from nothing else.
The API port, 8080, only from:

- the operator's own pods (its namespace, and the pod labels its
  `--operator-pod-labels` flag names), so it can apply documents;
- the class's `networkPolicy.apiConsumers`, for your platform's components
  that call the engines' APIs;
- the namespaces the API's route arrives from: `exposure.api`'s
  `gateway.proxyNamespaces` (its Gateways' own namespaces when unset) or
  `ingress.controllerNamespaces`.

The public port, 8081, only from where webhooks arrive: `exposure.webhooks`'
`gateway.proxyNamespaces` (its Gateways' own namespaces when unset),
`ingress.controllerNamespaces`, or `external.forwarders`. Under
`webhooks.mode: None` nothing is admitted to it.

So a webhook's proxy reaches no admin route, and an API consumer cannot
reach the port published to the internet. Everything else, other tenants and
the tenant's own other pods included, is refused. Name your consumers in the
class, by namespace and pod labels:

```yaml
spec:
  networkPolicy:
    mode: Isolated
    apiConsumers:
      - namespaceSelector:
          matchLabels: {kubernetes.io/metadata.name: platform}
        podSelector:
          matchLabels: {app: control-plane}
```

Only whoever writes EngineClasses decides this; no label a tenant or a
namespace editor can set admits anything.

The engine runs code its tenant supplies: stdio MCP servers, sandbox commands,
whatever its agents write. So the policy also limits where the engine
connects to. It may reach DNS (port 53), the pods of its own namespace, and
every address except the cloud metadata endpoints (`169.254.169.254`,
`fd00:ec2::254`), which hand the node's credentials to whatever asks. A
NetworkPolicy cannot tell an address inside the cluster from one outside it,
so list the cluster's pod, Service and node networks in `egress.exceptCIDRs`;
without them, an engine reaches other tenants' pods, the Kubernetes API
server and the nodes. Admit what the engine needs inside the cluster (an
in-cluster model gateway, the telemetry collector, an object store) with
`egress.to`:

```yaml
spec:
  networkPolicy:
    mode: Isolated
    egress:
      exceptCIDRs: [10.244.0.0/16, 10.96.0.0/12, 172.18.0.0/16]
      to:
        - namespaceSelector:
            matchLabels: {kubernetes.io/metadata.name: observability}
          podSelector:
            matchLabels: {app: otel-collector}
```

On AWS, also require IMDSv2 with a hop limit of 1 on the nodes, so that a
pod that gets past the policy still cannot read the node's role.

Set `gateway.proxyNamespaces` when your Gateway implementation runs its proxy
somewhere other than the Gateway's namespace:

```yaml
gateway:
  parentRefs:
    - {namespace: gateway-system, name: public}
  proxyNamespaces: [envoy-gateway-system]
```

A class that allows seat pools (`seatPools.policy: Allowed`) also has the
operator create two more policies per company, under either mode:
`<company>-broker`, which admits the data pods' leaf listener (port 7422)
from the company's own seat pods and from nothing else, since Crewlet's
broker port takes any connection without authentication or TLS (under `None`
it also admits ports 8080 and 8081 from anywhere, so the data pods stay as
open as they are without a policy); and, while the company runs seat pods,
`<company>-seats`, which admits no connection to a seat pod from any pod
and, under `Isolated`, holds the seat pods to the egress above. A seat pod's
probes (port 8080, `/health` and `/ready`) still answer its kubelet, whose
traffic comes from the pod's own node, which a NetworkPolicy does not
police. NetworkPolicies are additive, so this isolation of the seat pods
holds only while no other policy selects them: do not add one that selects
every pod of a tenant's namespace. A policy selects pods by label, so it
cannot keep apart companies that share a namespace, whose pods anyone who
creates pods there can label as either's: give each company a namespace of
its own where that matters, and see [Network
policy](../concepts/runtime.md#network-policy) for what no NetworkPolicy
governs (the node's own traffic, `hostNetwork` pods) and for the leaf link's
lack of encryption.

The policy holds only on a CNI that enforces NetworkPolicy, egress included
(Calico, Cilium, Antrea, and kind's kindnet on a kernel with nftables' queue
support). Some Gateway implementations
deliver traffic from the node rather than from a pod, which no NetworkPolicy
can admit by namespace; with those, set `networkPolicy.mode: None` and isolate
the engines with the CNI's own policy.

## Check it

```bash
kubectl get company acme -n acme -o jsonpath='{.status.endpoints}{"\n"}'
kubectl get company acme -n acme \
  -o jsonpath='{.status.conditions[?(@.type=="Exposed")].reason}: {.status.conditions[?(@.type=="Exposed")].message}{"\n"}'
kubectl get httproutes,ingresses,networkpolicies -n acme -l crewlet.ai/company=acme
```
