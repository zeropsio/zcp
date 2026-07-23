package skillpacks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// markerFileName is the per-copy ownership record every installed skill
// directory carries, one per physical copy.
const markerFileName = ".zcp-skillpack.json"

const markerSchemaVersion = 2

// Marker is one installed skill copy's ownership record.
type Marker struct {
	SchemaVersion int    `json:"schemaVersion"`
	PackID        string `json:"packId"`
	Generation    string `json:"generation"`
	Target        string `json:"target"`
	SkillName     string `json:"skillName"`
	SourcePath    string `json:"sourcePath"`
	Commit        string `json:"commit"`
	Digest        string `json:"digest"`
}

func validateMarker(m *Marker) error {
	if m.SchemaVersion != markerSchemaVersion {
		return fmt.Errorf("marker schemaVersion %d, want %d", m.SchemaVersion, markerSchemaVersion)
	}
	if err := validatePortableName(m.PackID); err != nil {
		return fmt.Errorf("invalid marker packId: %w", err)
	}
	if _, err := uuid.Parse(m.Generation); err != nil {
		return fmt.Errorf("invalid marker generation %q: %w", m.Generation, err)
	}
	if m.Target != string(TargetAgents) && m.Target != string(TargetClaude) {
		return fmt.Errorf("invalid marker target %q", m.Target)
	}
	if err := validatePortableName(m.SkillName); err != nil {
		return fmt.Errorf("invalid marker skillName: %w", err)
	}
	if err := validateSourcePath(m.SourcePath); err != nil {
		return fmt.Errorf("invalid marker sourcePath: %w", err)
	}
	if !commitPattern.MatchString(m.Commit) {
		return fmt.Errorf("invalid marker commit %q", m.Commit)
	}
	if !digestPattern.MatchString(m.Digest) {
		return fmt.Errorf("invalid marker digest %q", m.Digest)
	}
	return nil
}

// writeMarker writes m into relDir/markerFileName.
func writeMarker(root *os.Root, relDir string, m Marker) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal marker for %s: %w", relDir, err)
	}
	data = append(data, '\n')
	if err := root.WriteFile(filepath.Join(relDir, markerFileName), data, 0o644); err != nil {
		return wrapCoded(CodeFilesystem, err, "write marker in %s", relDir)
	}
	return nil
}

// markerIdentity is what an owned marker in relDir must exactly match.
type markerIdentity struct {
	packID     string
	generation string
	target     string
	skillName  string
}

// markerOutcome classifies a marker read for the remove-preflight and
// status-drift logic — every case except markerHardError is a normal,
// expected shape, not a failure.
type markerOutcome int

const (
	markerMissing markerOutcome = iota
	// markerUnusable: the marker path is a symlink/special file, or its
	// content is malformed/schema-invalid — it must never be trusted as
	// ownership proof, but its presence is not itself an error.
	markerUnusable
	// markerForeign: a well-formed, schema-valid marker whose identity
	// fields don't match what the caller expected (wrong pack, generation,
	// target, or skill name) — e.g. belongs to a different pack entirely.
	markerForeign
	markerOwned
)

// readCopyMarker inspects the marker at relDir against want and classifies
// it. Only a genuine I/O failure (permission, escape-detection) returns a
// non-nil error — every other case, including "missing" and "present but
// unusable/foreign", is a normal outcome the caller branches on.
//
// The marker path is Lstat'd (not simply opened) so a symlink sitting there
// — potentially pointing at ANOTHER copy's genuine marker, which stays
// inside the workspace and so would not trip os.Root's own escape
// detection — can never be read as if it were legitimate content.
func readCopyMarker(root *os.Root, relDir string, want markerIdentity) (markerOutcome, Marker, error) {
	markerRel := filepath.Join(relDir, markerFileName)
	info, err := root.Lstat(markerRel)
	if errors.Is(err, os.ErrNotExist) {
		return markerMissing, Marker{}, nil
	}
	if err != nil {
		return markerUnusable, Marker{}, wrapCoded(CodeFilesystem, err, "check marker at %s", relDir)
	}
	if !info.Mode().IsRegular() {
		return markerUnusable, Marker{}, nil
	}

	data, err := root.ReadFile(markerRel)
	if err != nil {
		return markerUnusable, Marker{}, wrapCoded(CodeFilesystem, err, "read marker at %s", relDir)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Marker
	if err := dec.Decode(&m); err != nil {
		return markerUnusable, Marker{}, nil //nolint:nilerr // the decode failure IS the markerUnusable classification, not a Go error to propagate
	}
	if err := validateMarker(&m); err != nil {
		return markerUnusable, Marker{}, nil //nolint:nilerr // same classification contract as above
	}
	if m.PackID != want.packID || m.Generation != want.generation || m.Target != want.target || m.SkillName != want.skillName {
		return markerForeign, m, nil
	}
	return markerOwned, m, nil
}
