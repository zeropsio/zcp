package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// codeVolumeFilled and codeVolumeAlready close FillCodeVolume's answer,
// codeVolumeContent prefixes CodeVolumeContent's.
const (
	codeVolumeFilled  = "@@zcp code-volume filled"
	codeVolumeAlready = "@@zcp code-volume already"
	codeVolumeContent = "@@zcp code-volume content "
)

// volumeContentShell sets $content to what the folder $vol holds — one of
// topology.VolumeContent's values — the one reading of a volume both the
// adopt probe and the fill trust: a checkout is a git work tree sharing a
// root commit with the runtime's own repository in /var/www.
func volumeContentShell() string {
	return `if [ ! -d "$vol" ]; then content=` + string(topology.VolumeAbsent) + `; ` +
		`elif [ -z "$(ls -A "$vol")" ]; then content=` + string(topology.VolumeEmpty) + `; ` +
		`else content=` + string(topology.VolumeForeign) + `; ` +
		`if [ -e "$vol/.git" ]; then own=$(git -C ` + shellQuote(defaultWorkingDir) + ` rev-list --max-parents=0 HEAD 2>/dev/null || true); ` +
		`for r in $(git -C "$vol" rev-list --max-parents=0 HEAD 2>/dev/null || true); do ` +
		`if printf '%s\n' "$own" | grep -qxF "$r"; then content=` + string(topology.VolumeCheckout) + `; fi; done; fi; fi; `
}

// CodeVolumeContent is what hostname finds at mountPath — the proof adopt
// needs before it takes a shared local-storage volume for a variant-B code
// volume (docs/spec-mate.md §2.8a.4a).
func CodeVolumeContent(ctx context.Context, ssh SSHDeployer, hostname, mountPath string) (topology.VolumeContent, error) {
	out, err := ssh.ExecSSH(ctx, hostname, "vol="+shellQuote(mountPath)+"; "+volumeContentShell()+`echo "`+codeVolumeContent+`$content"`)
	if err != nil {
		return "", fmt.Errorf("read the volume at %s on %s: %w", mountPath, hostname, err)
	}
	for line := range strings.Lines(string(out)) {
		if c, ok := strings.CutPrefix(strings.TrimSpace(line), codeVolumeContent); ok {
			return topology.VolumeContent(c), nil
		}
	}
	return "", fmt.Errorf("read the volume at %s on %s: no answer: %s", mountPath, hostname, strings.TrimSpace(string(out)))
}

// FillCodeVolume puts the checkout of hostname's /var/www onto the variant-B
// code volume mounted at mountPath (docs/spec-mate.md §2.8a), once: a volume
// that already holds this repository is left as it is, and the volume's own
// folder is never copied into itself. Only an empty volume is written: one
// holding anything else — data, another repository — is refused, naming the
// volume. Reports whether this call filled it.
func FillCodeVolume(ctx context.Context, ssh SSHDeployer, hostname, volume, mountPath string) (bool, error) {
	rel, under := strings.CutPrefix(mountPath, defaultWorkingDir+"/")
	folder, _, _ := strings.Cut(rel, "/")
	if !under || folder == "" {
		return false, fmt.Errorf("fill the code volume on %s: %q is no folder under %s", hostname, mountPath, defaultWorkingDir)
	}
	cmd := fmt.Sprintf(`set -e; vol=%[1]s; %[6]s`+
		`case $content in `+
		`%[7]s) echo "no $vol: the dev setup mounts no code volume there" >&2; exit 3;; `+
		`%[8]s) echo %[3]s; exit 0;; `+
		`%[9]s) echo "$content"; exit 0;; esac; `+
		`cd %[2]s && find . -mindepth 1 -maxdepth 1 ! -name %[4]s -exec cp -a {} "$vol/" \; && echo %[5]s`,
		shellQuote(mountPath), shellQuote(defaultWorkingDir), shellQuote(codeVolumeAlready), shellQuote(folder), shellQuote(codeVolumeFilled),
		volumeContentShell(), topology.VolumeAbsent, topology.VolumeCheckout, topology.VolumeForeign)
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return false, fmt.Errorf("fill the code volume %s on %s: %w", mountPath, hostname, err)
	}
	switch {
	case strings.Contains(string(out), codeVolumeFilled):
		return true, nil
	case strings.Contains(string(out), codeVolumeAlready):
		return false, nil
	case strings.TrimSpace(string(out)) == string(topology.VolumeForeign):
		return false, fmt.Errorf("the code volume %s at %s holds files that are no checkout of this repository, so zcp copies nothing onto it: only an empty volume receives the repository", volume, mountPath)
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

// CodeVolumeDataRoot is where a local-storage service's own container holds
// the volume: a variant-B pair's checkout reads there once no dev service is
// left to mount it (zcp's mount of the volume service shows none of it).
const CodeVolumeDataRoot = "/data"

// codeVolumeConfig frames the config file CodeVolumeMountingSetups reads.
const codeVolumeConfig = "zcp-code-volume-config 1"

// CodeVolumeMountingSetups names every setup of the checkout's config file at
// root on hostname — zerops.yaml, else zerops.yml, the working tree's — whose
// run.volume mounts volume, with the file read. The pool migration's retire
// reads it before it deletes a variant-B pair's dev services: a service
// running such a setup still mounts the volume, and the platform refuses the
// volume's delete until none does (docs/spec-workflows.md §13 PM-13). A file
// it cannot read or parse is an error, never a volume nothing mounts.
func CodeVolumeMountingSetups(ctx context.Context, ssh SSHDeployer, hostname, root, volume string) (string, []string, error) {
	cmd := "cd " + shellQuote(root) + " || exit 3; " +
		`for f in zerops.yaml zerops.yml; do if [ -f "$f" ]; then printf '%s %s\n' ` + shellQuote(codeVolumeConfig) + ` "$f"; cat "$f"; exit 0; fi; done; ` +
		"echo " + shellQuote(codeVolumeConfig+" -")
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return "", nil, fmt.Errorf("read the config file at %s on %s: %w", root, hostname, err)
	}
	text := string(out)
	i := strings.Index(text, codeVolumeConfig+" ")
	if i < 0 {
		return "", nil, fmt.Errorf("read the config file at %s on %s: no answer: %s", root, hostname, strings.TrimSpace(text))
	}
	header, body, _ := strings.Cut(text[i+len(codeVolumeConfig)+1:], "\n")
	file := strings.TrimSpace(header)
	if file == "-" {
		return "", nil, fmt.Errorf("%s holds no zerops.yaml or zerops.yml at %s", hostname, root)
	}
	doc, err := ParseZeropsYmlContent([]byte(body), file)
	if err != nil {
		return file, nil, fmt.Errorf("read %s at %s on %s: %w", file, root, hostname, err)
	}
	var setups []string
	for _, e := range doc.Zerops {
		if e.Setup != "" && e.Run.Volume.Hostname == volume {
			setups = append(setups, e.Setup)
		}
	}
	return file, setups, nil
}

// volumeStillMountedCode and volumeStillMountedText are how the platform
// refuses a local-storage volume's delete while a service still mounts it:
// the delete answers a process that fails with the error containerAction
// "This Local Storage volume is still mounted by other services…" — naming no
// service — which the process read folds into FailReason (live-verified).
const (
	volumeStillMountedCode = "containerAction"
	volumeStillMountedText = "still mounted"
)

// VolumeStillMounted reports a volume delete's process the platform failed
// because a service still mounts the volume (some service, it does not say
// which): the volume stays — FAILED, still working — until a later delete
// finds nothing mounting it.
func VolumeStillMounted(proc *platform.Process) bool {
	if proc == nil || proc.Status != platform.ProcessStatusFailed || proc.FailReason == nil {
		return false
	}
	reason := *proc.FailReason
	return strings.HasPrefix(reason, volumeStillMountedCode) && strings.Contains(reason, volumeStillMountedText)
}
