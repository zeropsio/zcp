package recipe

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

// FactRecord is a structured observation from a recipe run. Records are
// written at discovery time — classification is a record-time decision
// by the sub-agent, not a consume-time decision by the writer (plan
// §5 P4).
type FactRecord struct {
	Topic       string            `json:"topic"`
	Symptom     string            `json:"symptom"`
	Mechanism   string            `json:"mechanism"`
	SurfaceHint string            `json:"surfaceHint"`
	Citation    string            `json:"citation"`
	Scope       string            `json:"scope,omitempty"`
	RecordedAt  string            `json:"recordedAt,omitempty"`
	Author      string            `json:"author,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// Validate returns an error if any required field is empty.
func (f FactRecord) Validate() error {
	switch "" {
	case f.Topic:
		return errors.New("fact record missing required field \"topic\"")
	case f.Symptom:
		return errors.New("fact record missing required field \"symptom\"")
	case f.Mechanism:
		return errors.New("fact record missing required field \"mechanism\"")
	case f.SurfaceHint:
		return errors.New("fact record missing required field \"surface_hint\"")
	case f.Citation:
		return errors.New("fact record missing required field \"citation\"")
	}
	return nil
}

// FactsLog is a JSONL file of fact records scoped to one run. Safe for
// concurrent Append/Read; serializes writes through an instance mutex.
type FactsLog struct {
	path string
	mu   sync.Mutex
}

// OpenFactsLog returns a FactsLog bound to the given path. The file does
// not need to exist yet — Append creates it on first write.
func OpenFactsLog(path string) *FactsLog {
	return &FactsLog{path: path}
}

// Path returns the underlying file path.
func (l *FactsLog) Path() string { return l.path }

// Append validates the record, stamps RecordedAt if empty, then writes one
// JSON line to the log file. Invalid records are rejected before any I/O
// so a partial fact never lands on disk.
func (l *FactsLog) Append(f FactRecord) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if f.RecordedAt == "" {
		f.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal fact: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open facts log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append fact: %w", err)
	}
	return nil
}

// Read returns all records in the log in write order. A missing file
// returns (nil, nil).
func (l *FactsLog) Read() ([]FactRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open facts log: %w", err)
	}
	defer file.Close()

	var out []FactRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec FactRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("decode fact: %w", err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan facts log: %w", err)
	}
	return out, nil
}

// FilterByHint returns the subset of records whose SurfaceHint matches.
// Used by writer brief composition to deliver only facts the owning
// surface needs.
func FilterByHint(records []FactRecord, hint string) []FactRecord {
	var out []FactRecord
	for _, r := range records {
		if r.SurfaceHint == hint {
			out = append(out, r)
		}
	}
	return out
}
