// export_test.go exposes package internals to external tests (package z3_test).
// This file is compiled only during testing — it does not exist in production
// builds.
package z3

import (
	"context"
	"net/http"
)

// EnsureInstalled seam overrides — tests stub these so the download, npm
// install and post-stage version-probe steps never touch a real network, npm
// binary, or z3 binary.

func SetDownloadVerified(fn func(ctx context.Context, client *http.Client, releaseURL, expectedSHA256 string) (tarballPath string, cleanup func(), err error)) {
	downloadVerified = fn
}
func ResetDownloadVerified() { downloadVerified = defaultDownloadVerified }

func SetNpmInstallTarball(fn func(ctx context.Context, prefix, tarballPath string) error) {
	npmInstallTarball = fn
}
func ResetNpmInstallTarball() { npmInstallTarball = defaultNpmInstallTarball }

func SetSmokeTestBinary(fn func(ctx context.Context, bin string) error) {
	smokeTestBinary = fn
}
func ResetSmokeTestBinary() { smokeTestBinary = defaultSmokeTestBinary }
