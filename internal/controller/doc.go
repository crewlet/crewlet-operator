// Package controller holds the operator's reconcilers.
//
// The Company controller decides today whether a Company is one the operator
// can act on, and reports it as the Accepted condition: its EngineClass
// resolves, and its Seats and MCPServers stay within the class's limits. In the
// operator's design the other namespaced kinds in crewlet.ai (Unit, Seat,
// LLMProvider, MCPServer, Integration) are parts of a company, reconciled
// through the Company they reference rather than by controllers of their own;
// none of that runs yet, and nothing starts an engine.
package controller
