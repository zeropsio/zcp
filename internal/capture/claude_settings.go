package capture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ClaudeSettingRestore records the exact pre-capture JSON value and the string
// installed by ZCP. Restore uses compare-and-swap semantics.
type ClaudeSettingRestore struct {
	Existed   bool            `json:"existed"`
	Value     json.RawMessage `json:"value,omitempty"`
	Installed string          `json:"installed"`
}

// ClaudeSettingsPatch is persisted in manager state so a later `off` process
// can reverse only the values owned by `on`.
type ClaudeSettingsPatch struct {
	FileExisted bool                            `json:"fileExisted"`
	FileMode    uint32                          `json:"fileMode"`
	EnvExisted  bool                            `json:"envExisted"`
	Entries     map[string]ClaudeSettingRestore `json:"entries"`
}

func InstallClaudeCaptureSettings(path string, values map[string]string) (ClaudeSettingsPatch, error) {
	patch, err := PrepareClaudeCaptureSettings(path, values)
	if err != nil {
		return ClaudeSettingsPatch{}, err
	}
	if err := ApplyClaudeCaptureSettings(path, patch); err != nil {
		return ClaudeSettingsPatch{}, err
	}
	return patch, nil
}

// PrepareClaudeCaptureSettings captures the reversible compare-and-swap state
// without changing Claude configuration. Persistent lifecycle management must
// journal this patch before calling ApplyClaudeCaptureSettings.
func PrepareClaudeCaptureSettings(path string, values map[string]string) (ClaudeSettingsPatch, error) {
	if path == "" {
		return ClaudeSettingsPatch{}, errors.New("claude settings path is required")
	}
	if len(values) == 0 {
		return ClaudeSettingsPatch{}, errors.New("claude capture settings are empty")
	}
	document, fileExisted, mode, err := readClaudeSettings(path)
	if err != nil {
		return ClaudeSettingsPatch{}, err
	}
	env, envExisted, err := decodeClaudeSettingsEnv(document)
	if err != nil {
		return ClaudeSettingsPatch{}, err
	}
	patch := ClaudeSettingsPatch{
		FileExisted: fileExisted,
		FileMode:    uint32(mode.Perm()),
		EnvExisted:  envExisted,
		Entries:     make(map[string]ClaudeSettingRestore, len(values)),
	}
	for _, key := range sortedStringMapKeys(values) {
		if key == "" {
			return ClaudeSettingsPatch{}, errors.New("claude capture setting key is empty")
		}
		previous, existed := env[key]
		patch.Entries[key] = ClaudeSettingRestore{Existed: existed, Value: append(json.RawMessage(nil), previous...), Installed: values[key]}
	}
	return patch, nil
}

// ApplyClaudeCaptureSettings installs a previously prepared patch only when
// every owned key still has the value observed by Prepare. Unrelated settings
// may change concurrently and are preserved.
func ApplyClaudeCaptureSettings(path string, patch ClaudeSettingsPatch) error {
	if path == "" {
		return errors.New("claude settings path is required")
	}
	if len(patch.Entries) == 0 {
		return errors.New("claude capture settings patch is empty")
	}
	document, _, mode, err := readClaudeSettings(path)
	if err != nil {
		return err
	}
	env, _, err := decodeClaudeSettingsEnv(document)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(patch.Entries))
	for key := range patch.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		restore := patch.Entries[key]
		current, exists := env[key]
		if exists != restore.Existed || (exists && !equalClaudeJSON(current, restore.Value)) {
			return fmt.Errorf("claude setting env.%s changed before capture settings could be installed", key)
		}
		encoded, err := json.Marshal(restore.Installed)
		if err != nil {
			return fmt.Errorf("encode Claude setting %s: %w", key, err)
		}
		env[key] = encoded
	}
	encodedEnv, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode Claude settings env: %w", err)
	}
	document["env"] = encodedEnv
	return writeClaudeSettings(path, document, mode)
}

func equalClaudeJSON(left, right json.RawMessage) bool {
	var compactLeft, compactRight bytes.Buffer
	if json.Compact(&compactLeft, left) != nil || json.Compact(&compactRight, right) != nil {
		return bytes.Equal(left, right)
	}
	return bytes.Equal(compactLeft.Bytes(), compactRight.Bytes())
}

// RestoreClaudeCaptureSettings restores entries that still equal ZCP's
// installed values. Conflicting user edits are preserved and returned as
// warnings rather than overwritten.
func RestoreClaudeCaptureSettings(path string, patch ClaudeSettingsPatch) ([]string, error) {
	document, exists, mode, err := readClaudeSettings(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		if patch.FileExisted {
			return []string{"Claude settings file disappeared while capture was active; no values were restored"}, nil
		}
		return nil, nil
	}
	env, _, err := decodeClaudeSettingsEnv(document)
	if err != nil {
		return nil, err
	}
	var warnings []string
	keys := make([]string, 0, len(patch.Entries))
	for key := range patch.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		restore := patch.Entries[key]
		currentRaw, currentExists := env[key]
		var current string
		if !currentExists || json.Unmarshal(currentRaw, &current) != nil || current != restore.Installed {
			warnings = append(warnings, fmt.Sprintf("Claude setting %s changed while capture was active; preserving current value", key))
			continue
		}
		if restore.Existed {
			env[key] = append(json.RawMessage(nil), restore.Value...)
		} else {
			delete(env, key)
		}
	}
	if len(env) == 0 && !patch.EnvExisted {
		delete(document, "env")
	} else {
		encodedEnv, err := json.Marshal(env)
		if err != nil {
			return warnings, fmt.Errorf("encode restored Claude settings env: %w", err)
		}
		document["env"] = encodedEnv
	}
	if !patch.FileExisted && len(document) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return warnings, fmt.Errorf("remove empty capture-created Claude settings: %w", err)
		}
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return warnings, err
		}
		return warnings, nil
	}
	originalMode := os.FileMode(patch.FileMode)
	if originalMode == 0 {
		originalMode = mode
	}
	if err := writeClaudeSettings(path, document, originalMode); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func ClaudeCaptureSettingsInstalled(path string, values map[string]string) (bool, error) {
	document, exists, _, err := readClaudeSettings(path)
	if err != nil || !exists {
		return false, err
	}
	env, _, err := decodeClaudeSettingsEnv(document)
	if err != nil {
		return false, err
	}
	for key, expected := range values {
		raw, ok := env[key]
		if !ok {
			return false, nil
		}
		var actual string
		if err := json.Unmarshal(raw, &actual); err != nil {
			return false, fmt.Errorf("decode claude setting env.%s: %w", key, err)
		}
		if actual != expected {
			return false, nil
		}
	}
	return true, nil
}

func ClaudeSettingString(path, key string) (string, bool, error) {
	document, exists, _, err := readClaudeSettings(path)
	if err != nil || !exists {
		return "", false, err
	}
	env, _, err := decodeClaudeSettingsEnv(document)
	if err != nil {
		return "", false, err
	}
	raw, ok := env[key]
	if !ok {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("claude setting env.%s is not a string: %w", key, err)
	}
	return value, true, nil
}

func readClaudeSettings(path string) (map[string]json.RawMessage, bool, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]json.RawMessage), false, 0o600, nil
	}
	if err != nil {
		return nil, false, 0, fmt.Errorf("read Claude settings: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, 0, fmt.Errorf("stat Claude settings: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, fmt.Errorf("claude settings %q is not a regular file", path)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, false, 0, fmt.Errorf("decode Claude settings: %w", err)
	}
	if document == nil {
		document = make(map[string]json.RawMessage)
	}
	return document, true, info.Mode().Perm(), nil
}

func decodeClaudeSettingsEnv(document map[string]json.RawMessage) (map[string]json.RawMessage, bool, error) {
	raw, exists := document["env"]
	if !exists {
		return make(map[string]json.RawMessage), false, nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, true, fmt.Errorf("claude settings env must be an object: %w", err)
	}
	if env == nil {
		env = make(map[string]json.RawMessage)
	}
	return env, true, nil
}

func writeClaudeSettings(path string, document map[string]json.RawMessage, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Claude settings directory: %w", err)
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Claude settings: %w", err)
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".zcp-capture-settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create Claude settings temp file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if mode == 0 {
		mode = 0o600
	}
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set Claude settings permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write Claude settings temp file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync Claude settings temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close Claude settings temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Claude settings: %w", err)
	}
	removeTemp = false
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func sortedStringMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
