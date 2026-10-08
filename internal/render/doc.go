// Package render turns the crewlet.ai custom resources into the two documents
// a Crewlet engine reads: the company document (Crewlet's Tier B, applied over
// the engine's /config API) and a data pod's crewlet.yaml (Tier A, mounted
// into the pod).
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
// sync knows which values to push. A Tier A document refers to environment
// variables the pod receives from Secrets; BootstrapDocument.Env lists exactly
// those.
//
// # What render refuses, and what it leaves to the engine
//
// The API server has already enforced every OpenAPI and CEL rule of the
// objects, and render trusts that rather than repeating it. What no single
// object's validation can see is a reference to another object: a Seat whose
// Unit does not exist, a chain of parent Units that loops. Those are refused
// here with a *RefError naming the object and field that hold the reference,
// so the controller can report it on that object. Everything else Crewlet
// refuses is left to the engine's dry run, whose problems name a path in the
// rendered document; Document.Attribute maps such a path back to the object
// that produced it.
package render
