package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CompositionComponent attributes an exact output byte span to one static or
// dynamic owner. Start is inclusive; End is exclusive.
type CompositionComponent struct {
	Kind   string `json:"kind"`
	Owner  string `json:"owner"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	SHA256 string `json:"sha256"`
}

// CompositionRecord is a capture-only rendering side channel. Output is used
// to verify/hash spans and is deliberately not duplicated into provenance raw;
// the actual text remains canonical in MCP/provider raw evidence.
type CompositionRecord struct {
	Time         time.Time              `json:"time"`
	CaptureID    string                 `json:"captureId"`
	ProcessID    int                    `json:"processId"`
	Surface      string                 `json:"surface"`
	Output       string                 `json:"-"`
	OutputBytes  int                    `json:"outputBytes"`
	OutputSHA256 string                 `json:"outputSha256"`
	Components   []CompositionComponent `json:"components"`
}

// RecordCompositionFromEnvironment appends one process-local provenance line
// only under complete capture opt-in. It never changes or returns replacement
// output bytes.
func RecordCompositionFromEnvironment(record CompositionRecord) (bool, error) {
	sessionID := os.Getenv(EnvSessionID)
	sessionDir := os.Getenv(EnvSessionDir)
	if sessionID == "" && sessionDir == "" {
		return false, nil
	}
	if sessionID == "" || sessionDir == "" {
		return false, fmt.Errorf("composition capture requires both %s and %s", EnvSessionID, EnvSessionDir)
	}
	if record.Surface == "" {
		return false, errors.New("composition surface is required")
	}
	output := []byte(record.Output)
	previousEnd := 0
	components := make([]CompositionComponent, len(record.Components))
	for index, component := range record.Components {
		if component.Kind == "" || component.Owner == "" {
			return false, fmt.Errorf("composition component %d requires kind and owner", index)
		}
		if component.Start < previousEnd || component.Start < 0 || component.End < component.Start || component.End > len(output) {
			return false, fmt.Errorf("composition component %d has invalid span %d:%d for %d-byte output", index, component.Start, component.End, len(output))
		}
		sum := sha256.Sum256(output[component.Start:component.End])
		component.SHA256 = hex.EncodeToString(sum[:])
		components[index] = component
		previousEnd = component.End
	}
	outputSum := sha256.Sum256(output)
	record.Time = time.Now().UTC()
	record.CaptureID = sessionID
	record.ProcessID = os.Getpid()
	record.OutputBytes = len(output)
	record.OutputSHA256 = hex.EncodeToString(outputSum[:])
	record.Components = components
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, fmt.Errorf("encode composition provenance: %w", err)
	}
	encoded = append(encoded, '\n')
	provenanceDir := filepath.Join(sessionDir, "provenance")
	if err := os.Mkdir(provenanceDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return false, fmt.Errorf("create composition provenance directory: %w", err)
	}
	if err := os.Chmod(provenanceDir, 0o700); err != nil {
		return false, fmt.Errorf("secure composition provenance directory: %w", err)
	}
	path := filepath.Join(provenanceDir, fmt.Sprintf("zcp-%d.jsonl", os.Getpid()))
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return false, fmt.Errorf("composition provenance path %q is not a regular file", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat composition provenance: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("open composition provenance: %w", err)
	}
	_, writeErr := file.Write(encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return false, fmt.Errorf("append composition provenance: %w", err)
	}
	return true, nil
}

func ReadCompositionRecords(path string) ([]CompositionRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open composition provenance: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var records []CompositionRecord
	for {
		var record CompositionRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode composition provenance record %d: %w", len(records)+1, err)
		}
		records = append(records, record)
	}
	return records, nil
}
