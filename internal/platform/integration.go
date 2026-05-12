package platform

// IntegrationState is the canonical pipeline-integration state surfaced by
// ProjectAdminClient.GetServiceStackIntegrationStatus. Maps the SDK's
// HTTP-400-as-state behavior onto a state enum so callers branch on a
// value, not on error-code string-matching.
//
// Phase A spike finding (docs/spec-launch-production-platform-spike.md §B.1):
// the platform expresses "no integration configured yet" as HTTP 400 with
// `code: noExternalRepositoryIntegration`, NOT a 200 with state field.
// ZCP treats this code as a state read, not as a failure.
type IntegrationState string

const (
	// IntegrationNotConfigured signals that the service-stack has no
	// external-repository integration set up (fresh state). Wrapper maps
	// HTTP 400 with code `noExternalRepositoryIntegration` to this state.
	IntegrationNotConfigured IntegrationState = "not-configured"

	// IntegrationConfigured signals that the service-stack has an active
	// integration (github or gitlab) configured.
	IntegrationConfigured IntegrationState = "configured"
)

// IntegrationProvider names which git provider's integration is configured.
// Empty when State == IntegrationNotConfigured.
type IntegrationProvider string

const (
	IntegrationProviderGitHub IntegrationProvider = "github"
	IntegrationProviderGitLab IntegrationProvider = "gitlab"
)

// IntegrationEventType is the trigger event class on the configured integration.
// "BRANCH" — build on push to a branch. "TAG" — build on a tag matching a regex.
type IntegrationEventType string

const (
	IntegrationEventBranch IntegrationEventType = "BRANCH"
	IntegrationEventTag    IntegrationEventType = "TAG"
)

// IntegrationStatus is the wrapper-level view of an integration's state.
// When State == IntegrationNotConfigured all other fields are zero-valued.
// When State == IntegrationConfigured the Provider + RepositoryFullName +
// EventType fields are guaranteed populated; the others depend on
// EventType:
//   - EventType == BRANCH → BranchName populated, TagRegex empty.
//   - EventType == TAG → TagRegex populated, BranchName empty.
type IntegrationStatus struct {
	State              IntegrationState
	Provider           IntegrationProvider
	RepositoryFullName string
	EventType          IntegrationEventType
	BranchName         string
	TagRegex           string
	ZeropsYamlSetup    string
	IsActive           bool
}

// apiCodeNoExternalRepositoryIntegration is the platform error code that the
// integration-status endpoint returns when the service-stack has no
// integration configured. Treated as a state read by
// GetServiceStackIntegrationStatus rather than propagated as a failure.
const apiCodeNoExternalRepositoryIntegration = "noExternalRepositoryIntegration"
