// Package controller holds the operator's reconcilers. Company and
// EngineClass have controllers of their own; the other kinds in crewlet.ai
// (Unit, Seat, LLMProvider, MCPServer, Integration) are parts of a company
// and are reconciled through the Company they reference.
package controller
