package skillpacks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// skillPacksStateDir is where one manifest v2 per installed pack lives,
// relative to the project workspace root.
const skillPacksStateDir = ".zcp/state/skill-packs"

// manifestSchemaVersion is the only schemaVersion loadManifest accepts as
// "valid" — anything else (including a missing field, which is what every
// v1 manifest has) is legacy and refused without mutation. This is a clean
// break, not a migration: a v1 manifest has no per-skill digest, so hashing
// its current on-disk bytes would bless whatever the user has since edited
// as ZCP-owned content.
const manifestSchemaVersion = 2

// SourceRef pins exactly where a manifest's content came from.
type SourceRef struct {
	Repo     string `json:"repo"`
	CloneURL string `json:"cloneUrl"`
	Ref      string `json:"ref"`
	Commit   string `json:"commit"`
}

// SkillEntry records one installed skill's provenance and content digest.
type SkillEntry struct {
	Name       string `json:"name"`
	SourcePath string `json:"sourcePath"`
	Digest     string `json:"digest"`
}

// Manifest is the durable record of one installed pack. Every path
// pack-remove ever deletes is DERIVED from Targets (a closed enum) plus a
// validated SkillEntry.Name — the manifest itself never carries an
// arbitrary filesystem path.
type Manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	ID            string       `json:"id"`
	Generation    string       `json:"generation"`
	Source        SourceRef    `json:"source"`
	Targets       []string     `json:"targets"`
	Skills        []SkillEntry `json:"skills"`
}

// manifestState classifies what loadManifest found on disk for one id.
type manifestState int

const (
	manifestAbsent manifestState = iota
	manifestValid
	manifestLegacy  // missing/unsupported schemaVersion — a pre-v2 install
	manifestCorrupt // schemaVersion==2 but otherwise malformed
)

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$|^[0-9a-f]{64}$`)
)

func manifestRelPath(id string) string {
	return filepath.Join(skillPacksStateDir, id+".json")
}

func manifestTempRelPath(id, generation string) string {
	return filepath.Join(skillPacksStateDir, "."+id+"."+generation+".tmp")
}

// manifestProbe reads only the field needed to tell "legacy" (missing/
// unsupported schemaVersion) apart from "worth fully decoding".
type manifestProbe struct {
	SchemaVersion int `json:"schemaVersion"`
}

// loadManifest reads and classifies the manifest for id. A nil Manifest
// with state manifestAbsent/manifestLegacy/manifestCorrupt is not itself an
// error — the caller decides what each state means for the operation in
// progress (Status reports it; Add/Remove refuse to mutate on
// legacy/corrupt). err is non-nil only for an unexpected I/O failure.
func loadManifest(root *os.Root, id string) (*Manifest, manifestState, error) {
	data, err := root.ReadFile(manifestRelPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, manifestAbsent, nil
	}
	if err != nil {
		return nil, manifestAbsent, wrapCoded(CodeFilesystem, err, "read manifest for %q", id)
	}

	var probe manifestProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, manifestCorrupt, nil //nolint:nilerr // the decode failure IS the corrupt-state classification (manifestState), not a Go error to propagate
	}
	if probe.SchemaVersion != manifestSchemaVersion {
		return nil, manifestLegacy, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, manifestCorrupt, nil //nolint:nilerr // same classification contract as above
	}
	if err := validateManifest(&m, id); err != nil {
		return nil, manifestCorrupt, nil //nolint:nilerr // same classification contract as above
	}
	return &m, manifestValid, nil
}

// validateManifest enforces every structural rule a manifest v2 must
// satisfy: exact schema version, filename/body id agreement, portable id
// syntax, a valid generation UUID, a nonempty/well-shaped source, the exact
// target set, and — per skill — a portable name, a safe sourcePath, and a
// well-formed digest, sorted and unique by name.
func validateManifest(m *Manifest, filenameID string) error {
	if m.SchemaVersion != manifestSchemaVersion {
		return fmt.Errorf("schemaVersion %d, want %d", m.SchemaVersion, manifestSchemaVersion)
	}
	if err := validatePortableName(m.ID); err != nil {
		return fmt.Errorf("invalid manifest id: %w", err)
	}
	if m.ID != filenameID {
		return fmt.Errorf("manifest id %q does not match filename id %q", m.ID, filenameID)
	}
	if _, err := uuid.Parse(m.Generation); err != nil {
		return fmt.Errorf("invalid generation %q: %w", m.Generation, err)
	}
	if strings.TrimSpace(m.Source.Repo) == "" {
		return fmt.Errorf("source.repo must not be empty")
	}
	if strings.TrimSpace(m.Source.CloneURL) == "" {
		return fmt.Errorf("source.cloneUrl must not be empty")
	}
	if strings.TrimSpace(m.Source.Ref) == "" {
		return fmt.Errorf("source.ref must not be empty")
	}
	if !commitPattern.MatchString(m.Source.Commit) {
		return fmt.Errorf("invalid source.commit %q", m.Source.Commit)
	}
	if err := validateTargetSet(m.Targets); err != nil {
		return err
	}
	if len(m.Skills) == 0 {
		return fmt.Errorf("manifest has no skills")
	}
	prevName := ""
	for i, s := range m.Skills {
		if err := validatePortableName(s.Name); err != nil {
			return fmt.Errorf("skill[%d] name: %w", i, err)
		}
		if i > 0 && s.Name <= prevName {
			return fmt.Errorf("skills must be sorted by unique name (got %q after %q)", s.Name, prevName)
		}
		prevName = s.Name
		if err := validateSourcePath(s.SourcePath); err != nil {
			return fmt.Errorf("skill %q sourcePath: %w", s.Name, err)
		}
		if !digestPattern.MatchString(s.Digest) {
			return fmt.Errorf("skill %q has invalid digest %q", s.Name, s.Digest)
		}
	}
	return nil
}

// validateTargetSet requires exactly the closed two-element target set,
// sorted — this both rejects a duplicate/missing target and (being a
// literal equality check after sorting) rejects any target name outside
// the enum.
func validateTargetSet(got []string) error {
	want := []string{string(TargetAgents), string(TargetClaude)}
	if len(got) != len(want) {
		return fmt.Errorf("targets = %v, want exactly %v", got, want)
	}
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	for i := range want {
		if sorted[i] != want[i] {
			return fmt.Errorf("targets = %v, want exactly %v", got, want)
		}
	}
	return nil
}

// writeManifest atomically creates/replaces the manifest for m.ID: it
// writes to a generation-scoped temp file in the same directory then
// renames it into place (both through root), so a concurrent reader never
// observes a partial write.
func writeManifest(root *os.Root, m Manifest) error {
	if err := root.MkdirAll(skillPacksStateDir, 0o755); err != nil {
		return wrapCoded(CodeFilesystem, err, "create skill-packs state dir")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest for %q: %w", m.ID, err)
	}
	data = append(data, '\n')

	tempRel := manifestTempRelPath(m.ID, m.Generation)
	if err := root.WriteFile(tempRel, data, 0o600); err != nil {
		return wrapCoded(CodeFilesystem, err, "write manifest temp file for %q", m.ID)
	}
	if err := root.Rename(tempRel, manifestRelPath(m.ID)); err != nil {
		_ = root.Remove(tempRel)
		return wrapCoded(CodeFilesystem, err, "replace manifest for %q", m.ID)
	}
	return nil
}

// removeManifestFile deletes the manifest for id. A missing manifest is not
// an error — removal is idempotent at the manifest level.
func removeManifestFile(root *os.Root, id string) error {
	if err := root.Remove(manifestRelPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapCoded(CodeFilesystem, err, "remove manifest for %q", id)
	}
	return nil
}

// listManifestIDs returns, sorted, every pack id with a manifest file on
// disk — installed under a known catalog id, retired (manifest exists, id
// no longer in the catalog), or in any manifestState including legacy/
// corrupt. A missing state directory (no pack ever installed) is not an
// error; it yields an empty list.
func listManifestIDs(root *os.Root) ([]string, error) {
	names, err := readDirNames(root, skillPacksStateDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapCoded(CodeFilesystem, err, "list %s", skillPacksStateDir)
	}
	var ids []string
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}
