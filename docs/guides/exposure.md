# Exposure and network policy

A company's engine serves everything on one port, 8080: its API, its
dashboard, and the routes vendors deliver webhooks to. Only the webhooks
need to be reachable from the internet, and only they are routed there. This
guide sets up the webhook routes, the public address vendors are given, an
internal route to the API, and the network policy around each engine. The
objects involved are described in
[How a company's engine runs](../concepts/runtime.md#exposure).

All of it is the platform's choice, made once per `EngineClass` in
`spec.exposure` and `spec.networkPolicy`; a Company only states its own
address when the class leaves that to it.

## Choose how webhooks arrive

| `exposure.webhooks.mode` | Use it when | The Company's public address |
|---|---|---|
| `Gateway` | The cluster runs a Gateway API implementation | `https://<hostname>`, or the Company's `spec.exposure.publicBaseURL` |
| `Ingress` | The cluster has an Ingress controller | the same |
| `External` | Something outside the operator (a CDN, a tunnel, a load balancer you manage) forwards to the engine | the Company's `spec.exposure.publicBaseURL`, which is then required |
| `None` (the default) | Nothing reaches the engine from outside: development, or companies without Slack or GitHub | none; integrations that need inbound delivery report `Healthy=False NotExposed` |

Whatever the mode, only the path prefix `/webhooks/` is routed. The address
appears in the Company's `status.endpoints.publicBaseURL`, and each Seat's
and Integration's status carries the exact URL to register with the vendor.

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
        controllerNamespaces: [ingress-nginx]   # required with networkPolicy: Isolated
      hostname: "{{uid}}.hooks.example.com"
```

The Ingress `<company>-webhooks` is accepted once the controller publishes an
address for it. Its TLS Secret lives in the Company's namespace, so it is
issued per company, here by a certificate controller from the Ingress's
annotations.

### From outside the operator

With `mode: External` the operator creates no route. Forward
`https://<your address>/webhooks/` to the Service
`<company>-engine.<namespace>.svc:8080`, keeping the path, and give each
Company its address:

```yaml
spec:                       # Company
  exposure:
    publicBaseURL: https://acme.hooks.example.com
```

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

The route `<company>-api` carries every path, so put it only on a listener
that is not public. `status.endpoints.exposedAPI` is its address once
accepted. Every request still needs a token.

## Network policy

With `networkPolicy: Isolated`, the default, the operator creates a
NetworkPolicy `<company>-engine` that admits traffic to the engine's port only from:

- the operator's own namespace, so it can apply documents;
- namespaces labelled `crewlet.ai/api-consumer: "true"`, for your platform's
  components that call the engines' APIs;
- the namespaces routed traffic arrives from: each Gateway route's
  `gateway.proxyNamespaces` (the Gateways' own namespaces when unset) and each
  Ingress route's `ingress.controllerNamespaces`.

Everything else, other tenants included, is refused. Label the namespaces of
your own consumers:

```bash
kubectl label namespace platform crewlet.ai/api-consumer=true
```

Set `gateway.proxyNamespaces` when your Gateway implementation runs its proxy
somewhere other than the Gateway's namespace:

```yaml
gateway:
  parentRefs:
    - {namespace: gateway-system, name: public}
  proxyNamespaces: [envoy-gateway-system]
```

The policy restricts ingress only; engines reach model providers and vendors
directly. It holds only on a CNI that enforces NetworkPolicy (Calico, Cilium,
Antrea; not kind's default). Some Gateway implementations deliver traffic from
the node rather than from a pod, which no NetworkPolicy can admit by
namespace; with those, set `networkPolicy: None` and isolate the engines with
the CNI's own policy.

## Check it

```bash
kubectl get company acme -n acme -o jsonpath='{.status.endpoints}{"\n"}'
kubectl get company acme -n acme \
  -o jsonpath='{.status.conditions[?(@.type=="Exposed")].reason}: {.status.conditions[?(@.type=="Exposed")].message}{"\n"}'
kubectl get httproutes,ingresses,networkpolicies -n acme -l crewlet.ai/company=acme
```
