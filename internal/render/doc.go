// Package render turns the crewlet.ai custom resources into the documents a
// Crewlet engine reads: the company document (Crewlet's Tier B, applied over
// the engine's /config API) and each engine pod's crewlet.yaml (Tier A,
// mounted into the pod), a data pod's or a seat pool's. It also decides where
// each agent seat runs (PlanTopology), which both the company document and
// the seat pools' workloads are made from.
//
// Everything here is a pure function over values. The package holds no
// client, reads no Secret and does no I/O, so the same objects always render
// to the same bytes: that is what lets the config reconciler compare a render
// with the last applied one by hash, and what lets the tests pin the output in
// golden files and hand it to the engine's own validator.
//
// # Credentials
//
// No credential value ever reaches a rendered document. Every position Crewlet
// tags as a credential is rendered as a whole ${NAME} reference, where NAME is
// the engine secret-store entry the caller chose for the referenced Secret
// key (Input.StoreName), and the key is listed in Document.Refs so the secret
// sync knows which values to push. A Tier A document is the exception: it
// holds the node's own credentials as values and is kept in a Secret (see
// Bootstrap for why), and refers only to the pod's name and a PerSeat pod's
// seat, which BootstrapDocument.Env lists.
//
// # What render refuses, what it reports, and what it leaves to the engine
//
// The API server has already enforced every OpenAPI and CEL rule of the
// objects, and render trusts that rather than repeating it. What no single
// object's validation can see is a reference to another object: a Seat whose
// Unit does not exist, a chain of parent Units that loops, a Seat placed in a
// pool its Company does not declare or its EngineClass does not admit, a
// person's Atlassian account that the company's Jira or Confluence, on
// Atlassian Cloud, names nobody by. Each
// is a *RefError naming the object and field that hold the reference, so the
// controller can report it on that object, and each names its Basis
// (ADR-0005): a document is held only for what the engine refuses, what
// would harm more than the object at fault, or what the class forbids.
// Anything else is rendered as its spec says and reported (ReportedOf), as
// are the statements about a seat that the engine applies and refuses only at
// run time (ExecutorOf). Everything else Crewlet
// refuses is left to the engine's dry run, whose problems name a path in the
// rendered document; Document.Attribute maps such a path, read with PathOf
// from the engine client's *engine.ValidationError, back to the object that
// produced it.
package render
