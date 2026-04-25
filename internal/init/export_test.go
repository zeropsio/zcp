// export_test.go exposes package internals to external tests (package init_test).
// This file is compiled only during testing — it does not exist in production builds.
package init

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

func SetNginxLogFiles(files []string) { nginxLogFiles = files }
func ResetNginxLogFiles()             { nginxLogFiles = append([]string{}, defaultNginxLogFiles...) }

// SSHFS mount base overrides.

func SetSSHFSMountBase(dir string) { sshfsMountBase = dir }
func ResetSSHFSMountBase()         { sshfsMountBase = defaultSSHFSMountBase }
