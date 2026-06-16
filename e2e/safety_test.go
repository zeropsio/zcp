// Tests for: e2e — orphan-cleanup hostname matching (untagged so the C5 safety
// guard runs under the DEFAULT build, not only behind -tags e2e). The matcher
// is pure (no Zerops API), so it must execute in plain `go test ./e2e/`.
// hasTestPrefix is consumed BOTH here and by the e2e-tagged TestMain cleanup in
// helpers_test.go — an untagged symbol is visible in the e2e-tagged build, so a
// single definition serves both.

package e2e_test

import "strings"

// testServicePrefixes lists all hostname prefixes used by e2e tests.
var testServicePrefixes = []string{
	"bs", "in", "inc", // bootstrap_workflow_test.go
	"b6", "b8", "ba", "bb", "bad", // bootstrap_advanced_test.go, bootstrap_git_init_test.go
	"zcpdb",                                          // env_generate_test.go
	"zcppf", "zcpdpl", "zcpddev", "zcpdstg", "zcpld", // deploy_test, deploy_local_test, deploy_prepare_fail_test, deploy_failure_classification_e2_test, env_generate_test
	"zcpvrt", "zcpvdb", // verify_test.go
	"zcpsub", "zcpbl", // subdomain_test.go, deploy_failure_classification_e2_test.go (BuildPhase)
	"zcpmnt", "zcpapp", // import_provenance_test.go
	"zcpsl",          // subdomain_lifecycle_test.go
	"zcpex", "zcped", // export_multi_test.go
	"zcpstl", // launch_single_token_test.go
	"lrv",    // laravel_recipe_test.go
}

// hasTestPrefix reports whether a hostname is a generated e2e test service:
// one of the known prefixes IMMEDIATELY followed by at least 4 hex characters
// (every test hostname appends a randomSuffix()/4-hex tag). The hex-suffix
// requirement is the safety guard (C5): a bare HasPrefix matched real-word
// hostnames — `in`→inventory, `ba`→backend, `bs`→basket — and would delete a
// user's services if the token ever pointed at the wrong project. Real words
// fail because the chars after the prefix are not 4 consecutive hex digits.
func hasTestPrefix(hostname string) bool {
	for _, prefix := range testServicePrefixes {
		if rest, ok := strings.CutPrefix(hostname, prefix); ok && startsWithHexRun(rest, 4) {
			return true
		}
	}
	return false
}

// startsWithHexRun reports whether s begins with at least n hex digits.
func startsWithHexRun(s string, n int) bool {
	if len(s) < n {
		return false
	}
	for i := range n {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
