package topology

import "strings"

// RuntimeClassFor maps a service type to its RuntimeClass. Pure string-in,
// enum-out classification — no platform calls, no per-instance state.
//
// Classification rules:
//   - managed services (postgresql, mariadb, valkey, ...) → RuntimeManaged
//   - php-apache / php-nginx → RuntimeImplicitWeb (webserver auto-starts)
//   - static / nginx → RuntimeStatic (auto-serves deployFiles)
//   - everything else (nodejs, go, python, rust, bun, ...) → RuntimeDynamic
//   - empty input → RuntimeUnknown
//
// Lives in topology because the classification is foundational vocabulary
// shared by workflow/ (envelope rendering), ops/ (deploy classification),
// and tools/ (subdomain auto-enable probe gating). Previously duplicated as
// `workflow.classifyEnvelopeRuntime`; promoted to topology per the
// "shared types live in topology, never workflow" rule.
func RuntimeClassFor(typeVersion string) RuntimeClass {
	if typeVersion == "" {
		return RuntimeUnknown
	}
	if IsManagedService(typeVersion) {
		return RuntimeManaged
	}
	lower := strings.ToLower(typeVersion)
	if strings.HasPrefix(lower, "php-apache") || strings.HasPrefix(lower, "php-nginx") {
		return RuntimeImplicitWeb
	}
	if strings.HasPrefix(lower, "static") || strings.HasPrefix(lower, "nginx") {
		return RuntimeStatic
	}
	return RuntimeDynamic
}

// IsDeferredStart returns true when a runtime's start command is the
// "wait for dev_server" placeholder (zsc noop --silent) instead of the
// real application command. HTTP probes against such a runtime's
// subdomain return 502 by design until the agent invokes
// `zerops_dev_server action=start` to launch the actual server.
//
// True for: dev-mode dynamic runtimes — (ModeDev || ModeStandard) +
// RuntimeDynamic. These follow the SSHFS-mount + zsc-noop +
// zerops_dev_server lifecycle, where deploy lands code but no app
// process exists yet.
//
// False for:
//   - ModeStage / ModeSimple / ModeLocalStage — run.start runs the real
//     app immediately on container boot.
//   - RuntimeImplicitWeb (php-nginx, php-apache) — webserver auto-starts.
//   - RuntimeStatic — auto-serves whatever's in deployFiles.
//   - RuntimeManaged — no L7 anyway.
//
// Used by subdomain auto-enable + explicit-enable handlers to skip the
// HTTP-readiness probe when 502 is the expected steady state. Probing in
// this case generates a warning that agents misread as a deploy failure
// (eval: 4-of-9 scenarios in suite 20260503-211240 flagged the noise).
func IsDeferredStart(mode Mode, class RuntimeClass) bool {
	if class != RuntimeDynamic {
		return false
	}
	return mode == ModeDev || mode == ModeStandard
}
