package topology

import "strings"

// IsCommitID reports whether s is a whole git commit id: 40 (SHA-1) or 64
// (SHA-256) lowercase hex digits, as git prints it.
func IsCommitID(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}
