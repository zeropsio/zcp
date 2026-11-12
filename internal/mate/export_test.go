// export_test.go exposes package internals to external tests (package mate_test).
// This file is compiled only during testing — it does not exist in production
// builds.
package mate

// DefaultSmokeTestInstall exposes the probe so a test can run it against a
// staged directory.
var DefaultSmokeTestInstall = smokeTestInstall

// CarriesParts exposes carriesParts to the external test package.
var CarriesParts = carriesParts

// PruneOldVersions exposes pruneOldVersions, which Start.up runs once a
// version came up.
var PruneOldVersions = pruneOldVersions
