// Package dataconsole is the bounded, extractable Data Console module: a
// universal viewer + basic editor for a Zerops project's managed services,
// launched via `zcp studio console serve`.
//
// Layout (the extraction boundary):
//
//	console/     the engine — provider contracts, family providers, safety,
//	             server, embedded SPA. Imports ZERO zcp core (pinned by depguard
//	             `dataconsole-core-isolated` + TestDataConsoleBoundary_CoreIsolated).
//	zcpadapter/  the ONLY bridge to zcp core (auth/ops/platform/topology),
//	             implementing console.Host.
//
// On extraction to its own repo, only zcpadapter/ is rewritten; console/ moves
// byte-for-byte. The model: built INSIDE the zcp binary now (local-mode-first),
// liftable later. Rationale of record: ../../../zerops-studio/data-console/.
package dataconsole
