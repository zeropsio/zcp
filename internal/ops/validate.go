package ops

import (
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"gopkg.in/yaml.v3"
)

const (
	fileTypeImport = "import.yml"
	fileTypeZerops = "zerops.yml"
)

// ValidateResult contains the result of YAML validation.
type ValidateResult struct {
	Valid    bool              `json:"valid"`
	File     string            `json:"file"`
	Type     string            `json:"type"`
	Errors   []ValidationError `json:"errors,omitempty"`
	Warnings []string          `json:"warnings"`
	Info     []string          `json:"info"`
}

// ValidationError describes a single validation issue.
type ValidationError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
	Fix   string `json:"fix"`
}

// Validate performs offline validation of zerops.yml or import.yml content.
func Validate(content, filePath, fileType string) (*ValidateResult, error) {
	if content == "" && filePath == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide either content or filePath", "")
	}
	if content != "" && filePath != "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidUsage,
			"Provide either content or filePath, not both", "")
	}

	source := content
	file := ""
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, platform.NewPlatformError(platform.ErrFileNotFound,
				"File not found: "+filePath, "Check the file path")
		}
		source = string(data)
		file = filePath
	}

	detectedType := fileType
	if detectedType == "" {
		detectedType = detectType(filePath, source)
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(source), &raw); err != nil {
		code := platform.ErrInvalidZeropsYml
		if detectedType == fileTypeImport {
			code = platform.ErrInvalidImportYml
		}
		return nil, platform.NewPlatformError(code,
			"Invalid YAML syntax: "+err.Error(), "Fix the YAML syntax")
	}

	result := &ValidateResult{
		Valid: true,
		File:  file,
		Type:  detectedType,
	}

	if detectedType == fileTypeImport {
		if err := validateImportYml(raw, result); err != nil {
			return nil, err
		}
	} else {
		validateZeropsYml(raw, result)
	}

	return result, nil
}

func detectType(filePath, content string) string {
	if filePath != "" && strings.Contains(strings.ToLower(filePath), "import") {
		return fileTypeImport
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err == nil {
		if _, ok := raw["services"]; ok {
			return fileTypeImport
		}
		if _, ok := raw["zerops"]; ok {
			return fileTypeZerops
		}
	}
	return fileTypeZerops
}

func validateImportYml(raw map[string]any, result *ValidateResult) error {
	if _, ok := raw["project"]; ok {
		return platform.NewPlatformError(platform.ErrImportHasProject,
			"import.yml must not contain a project: section",
			"Remove the project: section; projects are managed separately")
	}
	if _, ok := raw["services"]; !ok {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:  "services",
			Error: "import.yml must contain a services: key",
			Fix:   "Add a services: array with at least one service definition",
		})
	}
	return nil
}

func validateZeropsYml(raw map[string]any, result *ValidateResult) {
	z, ok := raw["zerops"]
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:  "zerops",
			Error: "zerops.yml must contain a zerops: key",
			Fix:   "Add a zerops: array with service configurations",
		})
		return
	}
	arr, ok := z.([]any)
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:  "zerops",
			Error: "zerops: must be an array",
			Fix:   "Use zerops: followed by array items (- run: ...)",
		})
		return
	}
	if len(arr) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Path:  "zerops",
			Error: "zerops: array must not be empty",
			Fix:   "Add at least one service configuration",
		})
	}
}
