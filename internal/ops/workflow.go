package ops

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// GetWorkflow returns the content of a named workflow.
// Returns an error with available workflow names if the workflow is not found.
func GetWorkflow(workflowName string) (string, error) {
	wf, err := content.GetWorkflow(workflowName)
	if err != nil {
		return "", fmt.Errorf("workflow %q not found: available workflows: %s",
			workflowName, strings.Join(content.ListWorkflows(), ", "))
	}
	return wf, nil
}
