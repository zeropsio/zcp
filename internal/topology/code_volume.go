package topology

import (
	"fmt"
	"path"
	"strings"
)

// CodeMountRoot is where a runtime container holds its code — the default
// working directory of a deploy — and what zcp mounts from each runtime
// (`<host>:/var/www` → `/var/www/<host>`).
const CodeMountRoot = "/var/www"

// CodeVolume is variant B of a dev/stage pair: the repository lives on a
// shared local-storage service that every dev service of the pair mounts at
// MountPath, so each part's dev server runs in a container of its own runtime
// while one checkout serves them all. The pair stays one: Hostname of its meta
// is the dev service the agent edits and commits from; DevServices are the
// pair's further dev services, each deploying its own dev setup. zcp sets up
// no new one — a part whose runtime cannot share the dev service gets a dev
// pool — and keeps an existing one working until pool migration moves the
// pair onto one dev pool per part (docs/spec-mate.md §2.8a.4a).
type CodeVolume struct {
	// Hostname is the local-storage service holding the repository.
	Hostname string `json:"hostname"`
	// MountPath is the volume's mount in every dev service — a folder under
	// /var/www, which the platform refuses as a mount itself, so zcp's mount
	// of a dev service shows the volume as /var/www/<host>/<folder>.
	MountPath string `json:"mountPath"`
	// DevServices are the pair's dev services beside its own dev half.
	DevServices []DevServiceRef `json:"devServices"`
}

// VolumeContent is what a runtime finds at a code volume's mount path — the
// proof zcp needs before it treats a local-storage volume as one holding code
// and before it writes the repository onto it.
type VolumeContent string

const (
	// VolumeAbsent: nothing is there yet — the volume is mounted by a deploy
	// that has not run.
	VolumeAbsent VolumeContent = "absent"
	// VolumeEmpty: an empty folder.
	VolumeEmpty VolumeContent = "empty"
	// VolumeCheckout: a git checkout sharing history with the runtime's own
	// repository in /var/www.
	VolumeCheckout VolumeContent = "checkout"
	// VolumeForeign: anything else — data zcp never writes code into.
	VolumeForeign VolumeContent = "foreign"
)

// DevServiceRef is a further dev service of a variant-B pair with the
// zerops.yml dev setup it deploys (empty until known).
type DevServiceRef struct {
	Hostname  string `json:"hostname"`
	SetupName string `json:"setupName,omitempty"`
}

// DevService returns the further dev service named hostname, and whether the
// volume's pair has one.
func (v *CodeVolume) DevService(hostname string) (DevServiceRef, bool) {
	if v == nil || hostname == "" {
		return DevServiceRef{}, false
	}
	for _, d := range v.DevServices {
		if d.Hostname == hostname {
			return d, true
		}
	}
	return DevServiceRef{}, false
}

// DevServiceHostnames returns the further dev services' hostnames in order.
func (v *CodeVolume) DevServiceHostnames() []string {
	if v == nil {
		return nil
	}
	out := make([]string, 0, len(v.DevServices))
	for _, d := range v.DevServices {
		out = append(out, d.Hostname)
	}
	return out
}

// MountSubpath is MountPath below /var/www ("ws" for /var/www/ws): where the
// volume shows inside zcp's mount of a dev service.
func (v *CodeVolume) MountSubpath() string {
	if v == nil {
		return ""
	}
	return strings.TrimPrefix(path.Clean(v.MountPath), CodeMountRoot+"/")
}

// CodeVolumeMountPathError says why mountPath cannot hold a variant-B pair's
// code, "" when it can: an absolute folder strictly under /var/www — the
// platform refuses /var/www itself, and zcp's mount of a dev service shows
// nothing outside it.
func CodeVolumeMountPathError(mountPath string) string {
	clean := path.Clean(mountPath)
	if !path.IsAbs(mountPath) || !strings.HasPrefix(clean, CodeMountRoot+"/") || strings.Contains(mountPath, "..") {
		return fmt.Sprintf("the code volume's mountPath %q must be a folder under %[2]s (for example %[2]s/ws), never %[2]s itself: the platform refuses that mount, and zcp's mount of a dev service shows nothing outside %[2]s", mountPath, CodeMountRoot)
	}
	return ""
}
