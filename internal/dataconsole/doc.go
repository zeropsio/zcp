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
//	watch/       the platform websocket watcher behind `zcp studio watch`.
//	extension/   the Zerops Studio VS Code extension subtree (embed + materialize)
//	             — the desktop host that loads the console SPA.
//
// On extraction to its own repo, only zcpadapter/ is rewritten; console/ moves
// byte-for-byte. The model: built INSIDE the zcp binary now (local-mode-first),
// liftable later. Rationale of record: ../../../zerops-studio/data-console/.
//
// The OUTER boundary: no core package imports this subsystem except a handful of
// enumerated composition points (the `zcp studio` CLI, the dcseed seed tool, and
// the init studio install adapter). So the whole tree lifts out without unpicking
// core. Pinned by depguard `core-not-dataconsole` +
// TestDataConsoleBoundary_CoreDoesNotImportSubsystem (the two boundaries above
// pin the engine↔adapter seam INSIDE the subsystem; this one pins the
// core↔subsystem seam AROUND it).
package dataconsole
