package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// CheckZeropsYmlFields validates zerops.yaml field names against the live
// JSON schema. Catches import.yaml-only fields (e.g. verticalAutoscaling)
// that agents incorrectly add to zerops.yaml, plus any hallucinated field
// names.
//
// Returns nil when validFields is nil (shim mode without a schema cache)
// or when the yaml file is absent from ymlDir — the file-existence check
// is the upstream surface for that case.
func CheckZeropsYmlFields(_ context.Context, ymlDir string, validFields *schema.ValidFields) []workflow.StepCheck {
	if validFields == nil {
		return nil
	}

	// Read raw content — ParseZeropsYml uses typed structs which silently drop unknown fields.
	raw, err := ops.ReadZeropsYmlRaw(ymlDir)
	if err != nil {
		return nil // file-not-found already reported by the existence check
	}

	fieldErrs := schema.ValidateZeropsYmlRaw(raw, validFields)
	if len(fieldErrs) == 0 {
		return []workflow.StepCheck{{
			Name:   "zerops_yml_schema_fields",
			Status: StatusPass,
		}}
	}

	details := make([]string, len(fieldErrs))
	for i, e := range fieldErrs {
		details[i] = e.Error()
	}
	return []workflow.StepCheck{{
		Name:   "zerops_yml_schema_fields",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"zerops.yaml contains fields not in the platform schema (these belong in import.yaml or don't exist): %s",
			strings.Join(details, "; "),
		),
	}}
}
