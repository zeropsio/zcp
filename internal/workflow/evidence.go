package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServiceResult holds per-service verification status within evidence.
type ServiceResult struct {
	Hostname string `json:"hostname"`
	Status   string `json:"status"` // "pass", "fail", "skip"
	Detail   string `json:"detail,omitempty"`
}

// Evidence represents attestation evidence for gate checks.
type Evidence struct {
	SessionID        string          `json:"sessionId"`
	Timestamp        string          `json:"timestamp"`
	VerificationType string          `json:"verificationType"` // always "attestation"
	Service          string          `json:"service,omitempty"`
	Attestation      string          `json:"attestation"`
	Type             string          `json:"type"` // recipe_review, discovery, dev_verify, deploy_evidence, stage_verify
	Passed           int             `json:"passed"`
	Failed           int             `json:"failed"`
	ServiceResults   []ServiceResult `json:"serviceResults,omitempty"`
}

// SaveEvidence atomically writes evidence to dir/<sessionID>/<type>.json.
func SaveEvidence(dir, sessionID string, ev *Evidence) error {
	sessDir := filepath.Join(dir, sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return fmt.Errorf("save evidence mkdir: %w", err)
	}

	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return fmt.Errorf("save evidence marshal: %w", err)
	}

	target := filepath.Join(sessDir, ev.Type+".json")

	// Atomic write: temp file + rename.
	tmp, err := os.CreateTemp(sessDir, ".ev-*.tmp")
	if err != nil {
		return fmt.Errorf("save evidence temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save evidence write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save evidence close: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save evidence rename: %w", err)
	}
	return nil
}

// LoadEvidence loads evidence by type from dir/<sessionID>/<type>.json.
func LoadEvidence(dir, sessionID, evidenceType string) (*Evidence, error) {
	path := filepath.Join(dir, sessionID, evidenceType+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load evidence: %w", err)
	}
	var ev Evidence
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("load evidence unmarshal: %w", err)
	}
	return &ev, nil
}

// ListEvidence lists all evidence files for a session.
func ListEvidence(dir, sessionID string) ([]*Evidence, error) {
	sessDir := filepath.Join(dir, sessionID)
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list evidence: %w", err)
	}

	var result []*Evidence
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		evType := strings.TrimSuffix(e.Name(), ".json")
		ev, err := LoadEvidence(dir, sessionID, evType)
		if err != nil {
			continue // skip corrupt files
		}
		result = append(result, ev)
	}
	return result, nil
}

// ArchiveEvidence moves all evidence for a session to iterations/{n}/.
func ArchiveEvidence(dir, sessionID string, iteration int) error {
	sessDir := filepath.Join(dir, sessionID)
	archiveDir := filepath.Join(sessDir, "iterations", fmt.Sprintf("%d", iteration))

	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("archive evidence readdir: %w", err)
	}

	// Collect JSON files to move (not directories).
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("archive evidence mkdir: %w", err)
	}

	for _, name := range files {
		src := filepath.Join(sessDir, name)
		dst := filepath.Join(archiveDir, name)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("archive evidence move %s: %w", name, err)
		}
	}
	return nil
}
