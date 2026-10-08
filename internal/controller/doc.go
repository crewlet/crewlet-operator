// Package controller holds the operator's reconcilers.
//
// The Company reconciler decides whether a Company is one the operator can act
// on (Accepted: its EngineClass resolves, and its Seats and MCPServers stay
// within the class's limits), and for an accepted one makes its engine exist
// and be reachable: the keyring and API token Secrets, the data pods' bootstrap
// document, the data StatefulSet and its Services, the routes the class asks
// for and the NetworkPolicy around the engine (RuntimeReady, Exposed). It owns
// the Ready summary and the Company's finalizer, which drains the engine and
// settles its data volumes on deletion.
//
// The other namespaced kinds in crewlet.ai (Unit, Seat, LLMProvider,
// MCPServer, Integration) are parts of a company, rendered with it into the
// company document the engine runs; they have no reconciler of their own. The
// config reconciler, keyed by Company like the first, keeps that document
// equal to the objects as the Company's management policy says: it renders it,
// pushes the Secret values it references into the engine's secret store, has
// the engine dry-run it and applies it as one revision, or reports on the
// object at fault why it cannot. It owns the Company's SecretsSynced and
// ConfigApplied conditions and status.config, and the status of every part.
package controller
