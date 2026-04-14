package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const developMarkerDir = "develop"

// DevelopMarker is a lightweight per-process file indicating an active develop workflow.
// Unlike sessions, markers are non-exclusive — multiple processes can have active develop
// workflows simultaneously. Each process writes its own PID-keyed marker file.
// The PID is encoded in the filename ({pid}.json), not in the JSON body.
type DevelopMarker struct {
	ProjectID string `json:"projectId"`
	Intent    string `json:"intent,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// WriteDevelopMarker creates a PID-based develop marker at {stateDir}/develop/{pid}.json.
func WriteDevelopMarker(stateDir, projectID, intent string) error {
	if stateDir == "" {
		return fmt.Errorf("write develop marker: empty state dir")
	}
	dir := filepath.Join(stateDir, developMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create develop marker dir: %w", err)
	}

	pid := os.Getpid()
	marker := DevelopMarker{
		ProjectID: projectID,
		Intent:    intent,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshal develop marker: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%d.json", pid))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write develop marker: %w", err)
	}
	return nil
}

// HasDevelopMarker checks if a develop marker exists for the current process.
func HasDevelopMarker(stateDir string) bool {
	if stateDir == "" {
		return false
	}
	path := filepath.Join(stateDir, developMarkerDir, fmt.Sprintf("%d.json", os.Getpid()))
	_, err := os.Stat(path)
	return err == nil
}

// CleanStaleDevelopMarkers removes markers for processes that are no longer running.
func CleanStaleDevelopMarkers(stateDir string) error {
	dir := filepath.Join(stateDir, developMarkerDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read develop marker dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		pidStr := strings.TrimSuffix(entry.Name(), ".json")
		pid, parseErr := strconv.Atoi(pidStr)
		if parseErr != nil {
			continue
		}
		if !isProcessAlive(pid) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}
