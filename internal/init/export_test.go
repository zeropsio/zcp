// export_test.go exposes package internals to external tests (package init_test).
// This file is compiled only during testing — it does not exist in production builds.
package init

import "github.com/zeropsio/zcp/internal/z3"

// Command runner overrides.

func SetCommandRunner(fn func(string, ...string) error) { commandRunner = fn }
func ResetCommandRunner()                               { commandRunner = defaultCommandRunner }

// VS Code workspace directory overrides.

func SetVSCodeWorkDir(dir string) { vsCodeWorkDir = dir }
func ResetVSCodeWorkDir()         { vsCodeWorkDir = defaultVSCodeWorkDir }

// Nginx config overrides.

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

// z3 step overrides.

func SetZ3Install(fn func() error) { z3Install = fn }
func ResetZ3Install()              { z3Install = z3.Install }

func SetZ3UnitFilePath(path string) { z3UnitFilePath = path }
func ResetZ3UnitFilePath()          { z3UnitFilePath = z3.UnitFilePath }
