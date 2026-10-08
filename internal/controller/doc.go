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
// company document the engine runs. Applying that document is the config
// reconciler's job, which owns SecretsSynced, ConfigApplied and status.config;
// it does not run yet, so no engine is given a document, and a Company's Ready
// stays False with the reason ConfigPending.
package controller
