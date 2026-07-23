package skillpacks

import (
	"fmt"
	"strings"
)

// maxNameLen is the portable skill/pack-id name length cap: 1-64 lowercase
// ASCII letters/digits/hyphens.
const maxNameLen = 64

// windowsDeviceNames are reserved on Windows regardless of extension; a
// skill destined to become a directory name must never collide with one
// even though ZCP itself may run on a different OS — a pack installed on
// Linux still has to be safe to check out or copy on a teammate's Windows
// machine.
var windowsDeviceNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// reservedSkillName is the one destination name ZCP itself owns (the guided
// onboarding skill written by `zcp init --guided`); a pack must never be
// able to shadow it.
const reservedSkillName = "guided"

// validatePortableName enforces the portable skill/pack-id syntax: 1-64
// lowercase ASCII letters, digits, and hyphens, no leading/trailing/
// consecutive hyphen, and not a Windows reserved device name. This is the
// ONE charset skillpacks ever writes as a directory/file-stem name, so a
// name that passes is safe to use verbatim as a path component on every
// platform ZCP supports.
func validatePortableName(name string) error {
	if len(name) == 0 || len(name) > maxNameLen {
		return fmt.Errorf("name %q must be 1-%d characters", name, maxNameLen)
	}
	for _, r := range name {
		isLowerAlnumOrHyphen := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-'
		if !isLowerAlnumOrHyphen {
			return fmt.Errorf("name %q must contain only lowercase letters, digits, and hyphens", name)
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("name %q must not start or end with a hyphen", name)
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("name %q must not contain consecutive hyphens", name)
	}
	if windowsDeviceNames[name] {
		return fmt.Errorf("name %q is a reserved Windows device name", name)
	}
	return nil
}

// validateDestinationName runs validatePortableName plus the ZCP-reserved
// name check — the full rule for a name that will become a skill directory
// under a target root (as opposed to a pack id, which uses
// validatePortableName alone: "guided" is not a reserved pack id).
func validateDestinationName(name string) error {
	if err := validatePortableName(name); err != nil {
		return err
	}
	if name == reservedSkillName {
		return fmt.Errorf("%q is reserved for Zerops Guided and cannot be installed from a pack", name)
	}
	return nil
}

// validateSourcePath rejects an absolute path, a "." component's siblings
// mixed with traversal, empty segments, or ".." anywhere — the shape a
// manifest's per-skill sourcePath (and a marker's copy) must have. "." alone
// (the whole-repo/root-skill case) is valid. Unlike validatePortableName,
// segments may contain any upstream-repo characters (this mirrors a
// third-party source tree's own directory names, not a ZCP-chosen
// destination name).
func validateSourcePath(p string) error {
	if p == "." {
		return nil
	}
	if p == "" {
		return fmt.Errorf("sourcePath must not be empty")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("sourcePath %q must be relative", p)
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("sourcePath %q must use forward slashes", p)
	}
	for seg := range strings.SplitSeq(p, "/") {
		switch seg {
		case "":
			return fmt.Errorf("sourcePath %q must not contain an empty segment", p)
		case ".", "..":
			return fmt.Errorf("sourcePath %q must not contain a traversal segment", p)
		}
	}
	return nil
}
