package workflow

// WorkflowDevelop is the workflow name for develop sessions.
const WorkflowDevelop = "develop"

// Deploy step constants.
const (
	DeployStepPrepare = "prepare"
	DeployStepExecute = "execute"
	DeployStepVerify  = "verify"
)

// Deploy target role constants.
const (
	DeployRoleDev    = "dev"
	DeployRoleStage  = "stage"
	DeployRoleSimple = "simple"
)
