// export_test.go exposes package internals to external tests (package init_test).
// This file is compiled only during testing — it does not exist in production builds.
package init

import (
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// Command runner overrides.

func SetCommandRunner(fn func(string, ...string) error) { commandRunner = fn }
func ResetCommandRunner()                               { commandRunner = defaultCommandRunner }

// VS Code workspace directory overrides.

func SetVSCodeWorkDir(dir string) { vsCodeWorkDir = dir }
func ResetVSCodeWorkDir()         { vsCodeWorkDir = defaultVSCodeWorkDir }

// Nginx config overrides.

// RunNginxWithRotationPath keeps tests away from the real distribution policy.
func RunNginxWithRotationPath(path string) error { return runNginx(path) }

func SetNginxOutputPath(path string) { nginxOutputPath = path }
func ResetNginxOutputPath()          { nginxOutputPath = defaultNginxOutputPath }

func SetNginxDirs(dirs []string) { nginxDirs = dirs }
func ResetNginxDirs()            { nginxDirs = append([]string{}, defaultNginxDirs...) }

func DefaultNginxDirs() []string { return append([]string{}, defaultNginxDirs...) }

func SetNginxLogFiles(files []string) { nginxLogFiles = files }
func ResetNginxLogFiles()             { nginxLogFiles = append([]string{}, defaultNginxLogFiles...) }

// Nginx chown-target overrides — tests run as a non-root, non-zerops user, so
// they point the chown target at themselves (chown-to-self always succeeds).
func SetNginxOwner(uid, gid int) { nginxOwnerUID, nginxOwnerGID = uid, gid }
func ResetNginxOwner()           { nginxOwnerUID, nginxOwnerGID = zeropsUID, zeropsGID }

// SSHFS mount base overrides.

func SetSSHFSMountBase(dir string) { sshfsMountBase = dir }
func ResetSSHFSMountBase()         { sshfsMountBase = defaultSSHFSMountBase }

// mate step overrides.

func SetMateEnsureInstalled(fn func(mate.EnsureOptions) (mate.Result, error)) {
	mateEnsureInstalled = fn
}
func ResetMateEnsureInstalled() { mateEnsureInstalled = mate.EnsureInstalled }

func SetMateLockWait(d time.Duration) { mateLockWait = d }
func ResetMateLockWait()              { mateLockWait = defaultMateLockWait }

func SetMateUnitFilePath(path string) { mateUnitFilePath = path }
func ResetMateUnitFilePath()          { mateUnitFilePath = mate.UnitFilePath }

// RegisteredAdapterNames lists the container adapters `zcp init` dispatches,
// in dispatch order.
func RegisteredAdapterNames() []string {
	adapters := registeredAdapters()
	names := make([]string, 0, len(adapters))
	for _, a := range adapters {
		names = append(names, a.Name())
	}
	return names
}
