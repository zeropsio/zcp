//go:build !unix

package ops

// acquireLeaseAliasLock is a no-op on non-Unix platforms, where there is no
// sudo and no ssh_config.d to install the alias file into.
func acquireLeaseAliasLock(string) (func(), error) {
	return func() {}, nil
}
