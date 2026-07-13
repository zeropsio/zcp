package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var evalArtifactNames = []string{
	"assessment.md",
	"capture-mcp.json",
	"log.jsonl",
	"meta.json",
	"metadata.json",
	"retrospective-prompt.txt",
	"retrospective.jsonl",
	"self-review.md",
	"task-prompt.txt",
	"tool-calls.json",
	"transcript.jsonl",
	"verification.json",
}

// BundleEvalScenario copies the current scenario's known eval artifacts into
// the active capture window without transforming their bytes. Missing optional
// artifacts are simply absent; every returned path is manifest-relative.
func BundleEvalScenario(sessionDir, evalRunID, scenarioRunID, scenarioSource, outputDir string) ([]string, error) {
	if err := validateCapturePathComponent("eval run ID", evalRunID); err != nil {
		return nil, err
	}
	if err := validateCapturePathComponent("scenario run ID", scenarioRunID); err != nil {
		return nil, err
	}
	info, err := os.Stat(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("stat capture session for eval bundle: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("capture session %q is not a directory", sessionDir)
	}
	destinationDir := filepath.Join(sessionDir, "eval", evalRunID, scenarioRunID)
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return nil, fmt.Errorf("create eval bundle directory: %w", err)
	}
	if err := os.Chmod(filepath.Join(sessionDir, "eval"), 0o700); err != nil {
		return nil, fmt.Errorf("secure eval bundle root: %w", err)
	}
	if err := os.Chmod(filepath.Join(sessionDir, "eval", evalRunID), 0o700); err != nil {
		return nil, fmt.Errorf("secure eval run bundle: %w", err)
	}
	if err := os.Chmod(destinationDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure eval scenario bundle: %w", err)
	}

	sources := make(map[string]string)
	if scenarioSource != "" {
		sources["scenario.md"] = scenarioSource
	}
	for _, name := range evalArtifactNames {
		if outputDir != "" {
			sources[name] = filepath.Join(outputDir, name)
		}
	}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	var relativePaths []string
	for _, name := range names {
		source := sources[name]
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return relativePaths, fmt.Errorf("stat eval artifact %s: %w", name, err)
		}
		destination := filepath.Join(destinationDir, name)
		if err := copyPrivateRegularFile(source, destination); err != nil {
			return relativePaths, fmt.Errorf("bundle eval artifact %s: %w", name, err)
		}
		relative, err := filepath.Rel(sessionDir, destination)
		if err != nil {
			return relativePaths, fmt.Errorf("resolve bundled eval artifact %s: %w", name, err)
		}
		relativePaths = append(relativePaths, filepath.ToSlash(relative))
	}
	if err := syncDirectory(destinationDir); err != nil {
		return relativePaths, err
	}
	return relativePaths, nil
}

func validateCapturePathComponent(name, value string) error {
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, "/\\\x00") {
		return fmt.Errorf("%s %q is not a safe path component", name, value)
	}
	return nil
}

func copyPrivateRegularFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source %q is not a regular file", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".artifact-*.tmp")
	if err != nil {
		return fmt.Errorf("create destination temp: %w", err)
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
		return fmt.Errorf("set destination permissions: %w", err)
	}
	_, copyErr := io.Copy(temp, input)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	removeTemp = false
	return nil
}
