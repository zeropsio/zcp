package ops

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// GetWorkflowCatalog returns a static catalog listing all available workflows.
func GetWorkflowCatalog() string {
	workflows := content.ListWorkflows()
	var b strings.Builder
	b.WriteString("Available Zerops workflows:\n\n")
	for _, name := range workflows {
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	b.WriteString("\nUse zerops_workflow with a workflow name to get step-by-step guidance.")
	return b.String()
}

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
