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
	"sort"
	"strings"
	"time"
)

const (
	// ManifestFormatPrototype1 remains readable for captures produced by the
	// initial raw prototype. New captures use ManifestFormat1.
	ManifestFormatPrototype1 = "zcp-capture-prototype-1"
	ManifestFormat1          = "zcp-capture-1"
	ManifestFileProvider     = "provider"
	ManifestFileMCP          = "mcp"
	ManifestFileLifecycle    = "lifecycle"
	ManifestFileProvenance   = "provenance"
	ManifestFileEval         = "eval"
	manifestFilename         = "manifest.json"
)

// CaptureBuildInfo identifies the capture-enabled ZCP binary. These fields are
// diagnostic metadata, not a compatibility promise for derived views.
type CaptureBuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

// ProviderManifestInfo declares the fixed provider origin and local observer.
type ProviderManifestInfo struct {
	Origin   string `json:"origin"`
	ProxyURL string `json:"proxyUrl"`
}

// ManifestFile identifies one canonical raw file without embedding its data.
type ManifestFile struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

// SessionManifestDocument is self-describing capture-window metadata stored
// beside canonical raw records.
type SessionManifestDocument struct {
	FormatVersion string               `json:"formatVersion"`
	SessionID     string               `json:"sessionId"`
	Label         string               `json:"label,omitempty"`
	Plaintext     bool                 `json:"plaintext"`
	StartedAt     time.Time            `json:"startedAt"`
	EndedAt       *time.Time           `json:"endedAt,omitempty"`
	Status        string               `json:"status"`
	Command       []string             `json:"command"`
	Build         CaptureBuildInfo     `json:"captureBuild"`
	Provider      ProviderManifestInfo `json:"provider"`
	ChildExitCode *int                 `json:"childExitCode,omitempty"`
	Files         []ManifestFile       `json:"files"`
}

// SessionManifestConfig supplies immutable session metadata before child start.
type SessionManifestConfig struct {
	SessionDir string
	SessionID  string
	Label      string
	Command    []string
	Build      CaptureBuildInfo
	Provider   ProviderManifestInfo
}

// SessionManifest owns atomic rewrites of one manifest.json. Raw JSONL files
// remain canonical; this document only inventories and links them.
type SessionManifest struct {
	sessionDir string
	path       string
	document   SessionManifestDocument
}

// NewSessionManifest writes a running manifest before the observed child
// starts. The capture directory must already exist and remain private.
func NewSessionManifest(cfg SessionManifestConfig) (*SessionManifest, error) {
	if cfg.SessionDir == "" {
		return nil, errors.New("manifest session directory is required")
	}
	if cfg.SessionID == "" {
		return nil, errors.New("manifest session ID is required")
	}
	info, err := os.Stat(cfg.SessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat manifest session directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("manifest session path %q is not a directory", cfg.SessionDir)
	}
	manifest := &SessionManifest{
		sessionDir: cfg.SessionDir,
		path:       filepath.Join(cfg.SessionDir, manifestFilename),
		document: SessionManifestDocument{
			FormatVersion: ManifestFormat1,
			SessionID:     cfg.SessionID,
			Label:         cfg.Label,
			Plaintext:     true,
			StartedAt:     time.Now().UTC(),
			Status:        CaptureRunning,
			Command:       append([]string(nil), cfg.Command...),
			Build:         cfg.Build,
			Provider:      cfg.Provider,
			Files:         []ManifestFile{},
		},
	}
	if err := manifest.write(); err != nil {
		return manifest, fmt.Errorf("write running capture manifest: %w", err)
	}
	return manifest, nil
}

// Finalize inventories closed evidence files and durably records terminal
// wrapper status. A pointer exit code preserves an explicit successful zero.
func (m *SessionManifest) Finalize(status string, childExitCode int) error {
	exitCode := childExitCode
	return m.finalize(status, &exitCode)
}

// FinalizeDaemon closes a persistent/scoped daemon window, which has no child
// process exit code of its own.
func (m *SessionManifest) FinalizeDaemon(status string) error {
	return m.finalize(status, nil)
}

func (m *SessionManifest) finalize(status string, childExitCode *int) error {
	m.document.Files = nil
	providerPath := filepath.Join(m.sessionDir, recordsFilename)
	if err := m.addFile(providerPath, ManifestFileProvider); err != nil {
		return fmt.Errorf("inventory provider capture: %w", err)
	}
	lifecyclePath := filepath.Join(m.sessionDir, lifecycleFilename)
	if _, err := os.Lstat(lifecyclePath); err == nil {
		if err := m.addFile(lifecyclePath, ManifestFileLifecycle); err != nil {
			return fmt.Errorf("inventory lifecycle capture: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat lifecycle capture: %w", err)
	}
	mcpPaths, err := filepath.Glob(filepath.Join(m.sessionDir, "mcp", "*.jsonl"))
	if err != nil {
		return fmt.Errorf("find MCP captures: %w", err)
	}
	sort.Strings(mcpPaths)
	for _, path := range mcpPaths {
		if err := m.addFile(path, ManifestFileMCP); err != nil {
			return fmt.Errorf("inventory MCP capture: %w", err)
		}
	}
	provenancePaths, err := filepath.Glob(filepath.Join(m.sessionDir, "provenance", "*.jsonl"))
	if err != nil {
		return fmt.Errorf("find composition provenance: %w", err)
	}
	sort.Strings(provenancePaths)
	for _, path := range provenancePaths {
		if err := m.addFile(path, ManifestFileProvenance); err != nil {
			return fmt.Errorf("inventory composition provenance: %w", err)
		}
	}
	evalRoot := filepath.Join(m.sessionDir, "eval")
	if _, err := os.Lstat(evalRoot); err == nil {
		walkErr := filepath.WalkDir(evalRoot, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			return m.addFile(path, ManifestFileEval)
		})
		if walkErr != nil {
			return fmt.Errorf("inventory eval artifacts: %w", walkErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat eval artifacts: %w", err)
	}
	sort.Slice(m.document.Files, func(i, j int) bool {
		return m.document.Files[i].Path < m.document.Files[j].Path
	})
	endedAt := time.Now().UTC()
	m.document.EndedAt = &endedAt
	m.document.ChildExitCode = childExitCode
	m.document.Status = status
	if err := m.write(); err != nil {
		return fmt.Errorf("write terminal capture manifest: %w", err)
	}
	return nil
}

func (m *SessionManifest) addFile(path, kind string) error {
	relative, err := filepath.Rel(m.sessionDir, path)
	if err != nil {
		return fmt.Errorf("resolve raw file path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("raw file %q escapes capture session %q", path, m.sessionDir)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat raw file %q: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("raw file %q is not a regular file", relative)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open raw file %q: %w", relative, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("hash raw file %q: %w", relative, err)
	}
	m.document.Files = append(m.document.Files, ManifestFile{
		Kind:      kind,
		Path:      filepath.ToSlash(relative),
		SizeBytes: info.Size(),
		SHA256:    hex.EncodeToString(hash.Sum(nil)),
	})
	return nil
}

func (m *SessionManifest) write() error {
	data, err := json.MarshalIndent(m.document, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(m.sessionDir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create manifest temp file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set manifest temp permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write manifest temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync manifest temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close manifest temp file: %w", err)
	}
	if err := os.Rename(tempPath, m.path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	removeTemp = false
	directory, err := os.Open(m.sessionDir)
	if err != nil {
		return fmt.Errorf("open manifest directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync manifest directory: %w", err)
	}
	return nil
}

// RecoverUncleanSessionManifest inventories the durable prefix left by a dead
// daemon without rewriting any raw file. Already-terminal manifests are left
// untouched.
func RecoverUncleanSessionManifest(sessionDir string) (bool, error) {
	path := filepath.Join(sessionDir, manifestFilename)
	document, err := ReadSessionManifest(path)
	if err != nil {
		return false, err
	}
	if document.Status != CaptureRunning || document.EndedAt != nil {
		return false, nil
	}
	manifest := &SessionManifest{sessionDir: sessionDir, path: path, document: *document}
	if err := manifest.FinalizeDaemon(CaptureUnclean); err != nil {
		return false, fmt.Errorf("finalize crashed capture manifest: %w", err)
	}
	return true, nil
}

// Path returns the manifest.json path.
func (m *SessionManifest) Path() string { return m.path }

// ReadSessionManifest decodes one local capture manifest.
func ReadSessionManifest(path string) (*SessionManifestDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capture manifest: %w", err)
	}
	var document SessionManifestDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode capture manifest: %w", err)
	}
	return &document, nil
}
