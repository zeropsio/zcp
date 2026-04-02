package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const jsonExt = ".json"

// RecipeMeta records recipe creation metadata.
// Stored at .zcp/state/recipes/{slug}.json.
type RecipeMeta struct {
	Slug        string `json:"slug"`
	Framework   string `json:"framework"`
	Tier        string `json:"tier"`
	RuntimeType string `json:"runtimeType"`
	CreatedAt   string `json:"createdAt"`
	OutputDir   string `json:"outputDir,omitempty"`
}

// WriteRecipeMeta writes recipe metadata to baseDir/recipes/{slug}.json.
func WriteRecipeMeta(baseDir string, meta *RecipeMeta) error {
	dir := filepath.Join(baseDir, "recipes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create recipes dir: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal recipe meta: %w", err)
	}

	path := filepath.Join(dir, meta.Slug+jsonExt)
	tmp, err := os.CreateTemp(dir, meta.Slug+"*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename recipe meta: %w", err)
	}
	return nil
}

// ReadRecipeMeta reads recipe metadata from baseDir/recipes/{slug}.json.
// Returns nil, nil if the file does not exist.
func ReadRecipeMeta(baseDir, slug string) (*RecipeMeta, error) {
	path := filepath.Join(baseDir, "recipes", slug+jsonExt)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil // nil,nil = not found, by design
		}
		return nil, fmt.Errorf("read recipe meta: %w", err)
	}

	var meta RecipeMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal recipe meta: %w", err)
	}
	return &meta, nil
}

// ListRecipeMetas reads all recipe metadata files from baseDir/recipes/.
func ListRecipeMetas(baseDir string) ([]*RecipeMeta, error) {
	dir := filepath.Join(baseDir, "recipes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list recipes dir: %w", err)
	}

	var metas []*RecipeMeta
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != jsonExt {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("read recipe meta %s: %w", entry.Name(), readErr)
		}
		var meta RecipeMeta
		if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal recipe meta %s: %w", entry.Name(), unmarshalErr)
		}
		metas = append(metas, &meta)
	}
	return metas, nil
}
