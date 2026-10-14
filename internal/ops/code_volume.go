package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
)

// codeVolumeFilled and codeVolumeAlready close FillCodeVolume's answer.
const (
	codeVolumeFilled  = "@@zcp code-volume filled"
	codeVolumeAlready = "@@zcp code-volume already"
)

// FillCodeVolume puts the checkout of hostname's /var/www onto the variant-B
// code volume mounted at mountPath (docs/spec-mate.md §2.8a), once: a volume
// that already holds a repository is left as it is, and the volume's own
// folder is never copied into itself. Reports whether this call filled it.
func FillCodeVolume(ctx context.Context, ssh SSHDeployer, hostname, mountPath string) (bool, error) {
	rel, under := strings.CutPrefix(mountPath, defaultWorkingDir+"/")
	folder, _, _ := strings.Cut(rel, "/")
	if !under || folder == "" {
		return false, fmt.Errorf("fill the code volume on %s: %q is no folder under %s", hostname, mountPath, defaultWorkingDir)
	}
	cmd := fmt.Sprintf(`set -e; vol=%[1]s; `+
		`if [ ! -d "$vol" ]; then echo "no $vol: the dev setup mounts no code volume there" >&2; exit 3; fi; `+
		`if [ -e "$vol/.git" ]; then echo %[3]s; exit 0; fi; `+
		`cd %[2]s && find . -mindepth 1 -maxdepth 1 ! -name %[4]s -exec cp -a {} "$vol/" \; && echo %[5]s`,
		shellQuote(mountPath), shellQuote(defaultWorkingDir), shellQuote(codeVolumeAlready), shellQuote(folder), shellQuote(codeVolumeFilled))
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return false, fmt.Errorf("fill the code volume %s on %s: %w", mountPath, hostname, err)
	}
	switch {
	case strings.Contains(string(out), codeVolumeFilled):
		return true, nil
	case strings.Contains(string(out), codeVolumeAlready):
		return false, nil
	}
	return false, fmt.Errorf("fill the code volume %s on %s: no answer: %s", mountPath, hostname, strings.TrimSpace(string(out)))
}

// ReloadSupervisor is `zcp-devsup ctl reload` on hostname: its supervisor
// re-reads the declaration — what a variant-B stand-up runs on every dev
// service once the code volume holds the repository (docs/spec-mate.md
// §2.8a). A *NoSupervisorError says nothing there answers.
func ReloadSupervisor(ctx context.Context, ssh SSHDeployer, hostname string) (devsup.State, error) {
	return supervisorCtl(ctx, ssh, hostname, "reload")
}
