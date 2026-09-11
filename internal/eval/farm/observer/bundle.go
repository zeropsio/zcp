package observer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/eval"
)

// Bundle is the observer's read-only view over one pulled run bundle
// (docs/spec-eval-farm.md §7.2). Every path is relative to the bundle root
// and forward-slash separated, regardless of host OS. Implementations never
// write.
type Bundle interface {
	// ReadFile returns the bytes at rel. Returns an error when rel does not
	// exist or cannot be read.
	ReadFile(rel string) ([]byte, error)
	// ListFiles returns every regular file's path (relative to the bundle
	// root) whose path has the given prefix. Order is unspecified; callers
	// that need a stable order sort the result themselves.
	ListFiles(prefix string) ([]string, error)
}

// DirBundle is a Bundle backed by a local directory — the layout
// `zcp eval farm pull <runId> --out <dir>` produces (docs/spec-eval-farm.md
// §1.1, §3.3): <dir>/<runId>/{done.json,results/…,capture/…}.
type DirBundle struct {
	Root string
}

// NewDirBundle returns a Bundle rooted at root (a pulled run's own
// directory, i.e. <run-dir>).
func NewDirBundle(root string) *DirBundle {
	return &DirBundle{Root: root}
}

func (b *DirBundle) ReadFile(rel string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(b.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return data, nil
}

func (b *DirBundle) ListFiles(prefix string) ([]string, error) {
	base := filepath.Join(b.Root, filepath.FromSlash(prefix))
	var out []string
	err := filepath.Walk(base, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(b.Root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	sort.Strings(out)
	return out, nil
}

// ErrResultsNotFound is returned by ResultsDir when the bundle carries no
// results/<suite>/<scenario>/meta.json.
var ErrResultsNotFound = errors.New("results/<suite>/<scenario>/meta.json not found in bundle")

// ResultsDir locates the run's single results/<suite>/<scenario>/ directory
// (docs/spec-eval-farm.md §7.2) and returns it relative to the bundle root,
// e.g. "results/20260911-104838/recover-failed-buildfromgit-missing-dep".
// It is found by locating meta.json under results/, since a run bundle
// carries exactly one scenario result (§2.3 FM-13: "a run project executes
// exactly one scenario").
func ResultsDir(b Bundle) (string, error) {
	files, err := b.ListFiles("results/")
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if dir, ok := strings.CutSuffix(f, "/meta.json"); ok {
			return dir, nil
		}
	}
	return "", ErrResultsNotFound
}

// CaptureScenarioMD locates
// capture/<window>/eval/<suite>/<scenario>/scenario.md, given
// suiteScenario in "<suite>/<scenario>" form (ResultsDir's return value with
// its "results/" prefix stripped). Returns "" with a nil error when the
// bundle carries no capture window at all — scenario.md is optional
// (§7.2) — but propagates a real listing error.
func CaptureScenarioMD(b Bundle, suiteScenario string) (string, error) {
	files, err := b.ListFiles("capture/")
	if err != nil {
		return "", err
	}
	suffix := "/eval/" + suiteScenario + "/scenario.md"
	for _, f := range files {
		if strings.HasSuffix(f, suffix) {
			return f, nil
		}
	}
	return "", nil
}

// LoadTaskPrompt reads results/<suite>/<scenario>/task-prompt.txt (§7.2).
func LoadTaskPrompt(b Bundle, resultsDir string) (string, error) {
	data, err := b.ReadFile(resultsDir + "/task-prompt.txt")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// LoadTranscript reads results/<suite>/<scenario>/transcript.jsonl — a
// required input (§7.2): its absence makes the observation status "error".
func LoadTranscript(b Bundle, resultsDir string) ([]byte, error) {
	return b.ReadFile(resultsDir + "/transcript.jsonl")
}

// LoadMeta reads and parses results/<suite>/<scenario>/meta.json — a
// required input (§7.2): its absence makes the observation status "error".
func LoadMeta(b Bundle, resultsDir string) (eval.BehavioralResult, error) {
	data, err := b.ReadFile(resultsDir + "/meta.json")
	if err != nil {
		return eval.BehavioralResult{}, err
	}
	var meta eval.BehavioralResult
	if err := json.Unmarshal(data, &meta); err != nil {
		return eval.BehavioralResult{}, fmt.Errorf("parse meta.json: %w", err)
	}
	return meta, nil
}

// LoadVerification reads and parses
// results/<suite>/<scenario>/verification.json. Absence renders an empty
// CHECKS section in the digest (§7.3) rather than an error.
func LoadVerification(b Bundle, resultsDir string) (eval.VerificationDocument, error) {
	data, err := b.ReadFile(resultsDir + "/verification.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return eval.VerificationDocument{}, nil
		}
		return eval.VerificationDocument{}, err
	}
	var doc eval.VerificationDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return eval.VerificationDocument{}, fmt.Errorf("parse verification.json: %w", err)
	}
	return doc, nil
}

// LoadSelfReview reads results/<suite>/<scenario>/self-review.md — optional
// (§7.2): "" with a nil error when absent.
func LoadSelfReview(b Bundle, resultsDir string) (string, error) {
	data, err := b.ReadFile(resultsDir + "/self-review.md")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// LoadPlatformSnapshot reads and parses
// results/<suite>/<scenario>/platform-snapshot.json — optional (§7.2): nil
// with a nil error when absent.
func LoadPlatformSnapshot(b Bundle, resultsDir string) (*eval.PlatformSnapshot, error) {
	data, err := b.ReadFile(resultsDir + "/platform-snapshot.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil //nolint:nilnil // absence is a legitimate third state, distinct from an error
		}
		return nil, err
	}
	var snap eval.PlatformSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse platform-snapshot.json: %w", err)
	}
	return &snap, nil
}
