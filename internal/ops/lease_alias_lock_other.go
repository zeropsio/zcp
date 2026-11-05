//go:build !unix

package ops

import "context"

// acquireLeaseAliasLock is a no-op on non-Unix platforms, where there is no
// sudo and no ssh_config.d to install the alias file into.
func acquireLeaseAliasLock(context.Context, string) (func(), error) {
	return func() {}, nil
}
