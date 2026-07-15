// Package projection projects immutable capture evidence into a versioned,
// read-only query model. It is compiler-private to the captureinspector domain
// and is never used by recorder, proxy, or MCP server hot paths.
package projection

import (
	"encoding/json"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

const (
	FormatVersion1            = "zcp-capture-view-1"
	statusError               = "error"
	toolCategoryMCP           = "mcp"
	toolCategoryBuiltin       = "builtin"
	propagationMissing        = "missing"
	propagationDifferent      = "different"
	propagationAmbiguous      = "ambiguous"
	blockTypeText             = "text"
	blockTypeToolUse          = "tool_use"
	blockTypeToolResult       = "tool_result"
	blockTypeServerToolUse    = "server_tool_use"
	blockTypeRedactedThinking = "redacted_thinking"
	mcpMessageRequest         = "request"
	mcpMessageNotification    = "notification"
	statusRunning             = capture.CaptureRunning
	statusComplete            = capture.CaptureComplete
)

type View struct {
	FormatVersion       string                 `json:"formatVersion"`
	Capture             CaptureSummary         `json:"capture"`
	Integrity           IntegritySummary       `json:"integrity"`
	Overview            Overview               `json:"overview"`
	EvalRuns            []EvalRun              `json:"evalRuns"`
	Sessions            []ClientSession        `json:"sessions"`
	ClientRuns          []ClientRun            `json:"clientRuns"`
	Conversation        []ConversationEvent    `json:"conversation"`
	Exchanges           []ProviderExchange     `json:"exchanges"`
	ProviderEvents      []ProviderEvent        `json:"-"`
	ProviderEventTotal  int                    `json:"providerEventTotal"`
	ProviderBlocks      []ProviderBlock        `json:"providerBlocks"`
	Contexts            []ContextSnapshot      `json:"contexts"`
	Tools               []ToolExecution        `json:"tools"`
	MCPProcesses        []MCPProcess           `json:"mcpProcesses"`
	MCPCalls            []MCPCall              `json:"mcpCalls"`
	Sources             []SourceOwner          `json:"sources"`
	Timeline            []TimelineEvent        `json:"timeline"`
	RawFiles            []RawFile              `json:"rawFiles"`
	RawRecords          []RawRecordSummary     `json:"rawRecords"`
	RawRecordTotal      int                    `json:"rawRecordTotal"`
	RawRecordTotalKnown bool                   `json:"rawRecordTotalKnown"`
	RawRecordsTruncated bool                   `json:"rawRecordsTruncated"`
	Artifacts           []Artifact             `json:"artifacts"`
	Diagnostics         []StructuralDiagnostic `json:"diagnostics"`
	Metrics             []Metric               `json:"metrics"`
	Edges               []Edge                 `json:"edges"`
}

type CaptureSummary struct {
	ID             string    `json:"id"`
	Label          string    `json:"label,omitempty"`
	Status         string    `json:"status"`
	Plaintext      bool      `json:"plaintext"`
	StartedAt      time.Time `json:"startedAt"`
	EndedAt        time.Time `json:"endedAt,omitzero"`
	DurationMs     int64     `json:"durationMs,omitempty"`
	FormatVersion  string    `json:"formatVersion"`
	BuildVersion   string    `json:"buildVersion,omitempty"`
	BuildCommit    string    `json:"buildCommit,omitempty"`
	BuildTime      string    `json:"buildTime,omitempty"`
	ProviderOrigin string    `json:"providerOrigin,omitempty"`
	ChildExitCode  *int      `json:"childExitCode,omitempty"`
}

type IntegritySummary struct {
	State                 string `json:"state"`
	Valid                 bool   `json:"valid"`
	Complete              bool   `json:"complete"`
	ManifestPresent       bool   `json:"manifestPresent"`
	ManifestFilesVerified int    `json:"manifestFilesVerified"`
	ProviderRecords       int    `json:"providerRecords"`
	LifecycleRecords      int    `json:"lifecycleRecords"`
	MCPRecords            int    `json:"mcpRecords"`
	ProvenanceRecords     int    `json:"provenanceRecords"`
	WarningCount          int    `json:"warningCount"`
}

type Overview struct {
	BundleBytes              int64            `json:"bundleBytes"`
	BytesByKind              map[string]int64 `json:"bytesByKind"`
	ProviderExchanges        int              `json:"providerExchanges"`
	UnattributedExchanges    int              `json:"unattributedExchanges"`
	ClientSessions           int              `json:"clientSessions"`
	EvalRuns                 int              `json:"evalRuns"`
	Scenarios                int              `json:"scenarios"`
	Invocations              int              `json:"invocations"`
	MCPProcesses             int              `json:"mcpProcesses"`
	ToolExecutions           int              `json:"toolExecutions"`
	ToolErrors               int              `json:"toolErrors"`
	PropagationExact         int              `json:"propagationExact"`
	PropagationMissing       int              `json:"propagationMissing"`
	TotalRequestBytes        int64            `json:"totalRequestBytes"`
	LargestRequestBytes      int              `json:"largestRequestBytes"`
	TotalSystemBytes         int64            `json:"totalSystemBytes"`
	TotalToolSchemaBytes     int64            `json:"totalToolSchemaBytes"`
	TotalMessageBytes        int64            `json:"totalMessageBytes"`
	InputTokens              int64            `json:"inputTokens"`
	CacheCreationInputTokens int64            `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int64            `json:"cacheReadInputTokens"`
	OutputTokens             int64            `json:"outputTokens"`
	MCPInputBytes            int64            `json:"mcpInputBytes"`
	MCPOutputBytes           int64            `json:"mcpOutputBytes"`
	ClientTurns              int              `json:"clientTurns"`
	ClientDurationMs         int64            `json:"clientDurationMs"`
	ClientTTFTMs             int64            `json:"clientTtftMs"`
	RateLimitEvents          int              `json:"rateLimitEvents"`
	ThinkingEvents           int              `json:"thinkingEvents"`
	PermissionDenials        int              `json:"permissionDenials"`
	ReportedCostUSD          float64          `json:"reportedCostUsd"`
}

type EvidenceRef struct {
	ID            string    `json:"id"`
	File          string    `json:"file"`
	SeqStart      uint64    `json:"seqStart,omitempty"`
	SeqEnd        uint64    `json:"seqEnd,omitempty"`
	StreamOffset  int64     `json:"streamOffset,omitempty"`
	DecodedOffset int64     `json:"decodedOffset,omitempty"`
	ByteLength    int64     `json:"byteLength,omitempty"`
	ExchangeID    string    `json:"exchangeId,omitempty"`
	ObservedAt    time.Time `json:"observedAt,omitzero"`
}

type TimelineEvent struct {
	ID              string        `json:"id"`
	Kind            string        `json:"kind"`
	Lane            string        `json:"lane"`
	Title           string        `json:"title"`
	Status          string        `json:"status,omitempty"`
	Basis           string        `json:"basis"`
	StartedAt       time.Time     `json:"startedAt"`
	EndedAt         time.Time     `json:"endedAt,omitzero"`
	DurationMs      int64         `json:"durationMs,omitempty"`
	EvalRunID       string        `json:"evalRunId,omitempty"`
	ScenarioRunID   string        `json:"scenarioRunId,omitempty"`
	InvocationID    string        `json:"invocationId,omitempty"`
	Phase           string        `json:"phase,omitempty"`
	ClientSessionID string        `json:"clientSessionId,omitempty"`
	ExchangeID      string        `json:"exchangeId,omitempty"`
	Evidence        []EvidenceRef `json:"evidence"`
}

type EvalRun struct {
	ID        string     `json:"id"`
	Status    string     `json:"status,omitempty"`
	Scenarios []Scenario `json:"scenarios"`
}

type Scenario struct {
	ID          string       `json:"id"`
	Status      string       `json:"status,omitempty"`
	Invocations []Invocation `json:"invocations"`
	Artifacts   []string     `json:"artifacts,omitempty"`
}

type Invocation struct {
	ID                string        `json:"id"`
	Phase             string        `json:"phase"`
	ClientSessionID   string        `json:"clientSessionId,omitempty"`
	Status            string        `json:"status,omitempty"`
	StartedAt         time.Time     `json:"startedAt"`
	EndedAt           time.Time     `json:"endedAt,omitzero"`
	DurationMs        int64         `json:"durationMs,omitempty"`
	TimingObserved    bool          `json:"timingObserved"`
	ProviderExchanges int           `json:"providerExchanges"`
	ExchangeIDs       []string      `json:"exchangeIds,omitempty"`
	MCPProcesses      int           `json:"mcpProcesses"`
	MCPFiles          []string      `json:"mcpFiles,omitempty"`
	Evidence          []EvidenceRef `json:"evidence"`
}

type ClientSession struct {
	ID                string        `json:"id"`
	ProviderExchanges int           `json:"providerExchanges"`
	Models            []string      `json:"models,omitempty"`
	FirstObservedAt   time.Time     `json:"firstObservedAt"`
	LastObservedAt    time.Time     `json:"lastObservedAt"`
	DurationMs        int64         `json:"durationMs,omitempty"`
	TimingObserved    bool          `json:"timingObserved"`
	Evidence          []EvidenceRef `json:"evidence"`
}

type ClientRun struct {
	ArtifactPath      string        `json:"artifactPath"`
	Kind              string        `json:"kind"`
	ClientSessionID   string        `json:"clientSessionId,omitempty"`
	Model             string        `json:"model,omitempty"`
	ClientVersion     string        `json:"clientVersion,omitempty"`
	AssistantEvents   int           `json:"assistantEvents"`
	UserEvents        int           `json:"userEvents"`
	ThinkingEvents    int           `json:"thinkingEvents"`
	RateLimitEvents   int           `json:"rateLimitEvents"`
	ResultEvents      int           `json:"resultEvents"`
	Turns             int           `json:"turns"`
	DurationMs        int64         `json:"durationMs"`
	TTFTMs            int64         `json:"ttftMs"`
	ReportedCostUSD   float64       `json:"reportedCostUsd"`
	CostReports       int           `json:"costReports"`
	DurationReports   int           `json:"durationReports"`
	TTFTReports       int           `json:"ttftReports"`
	TurnReports       int           `json:"turnReports"`
	PermissionDenials int           `json:"permissionDenials"`
	ResultStatus      string        `json:"resultStatus,omitempty"`
	StopReason        string        `json:"stopReason,omitempty"`
	TerminalReason    string        `json:"terminalReason,omitempty"`
	Evidence          []EvidenceRef `json:"evidence"`
}

type ConversationEvent struct {
	ID              string        `json:"id"`
	ArtifactPath    string        `json:"artifactPath"`
	ArtifactKind    string        `json:"artifactKind"`
	Line            uint64        `json:"line"`
	Type            string        `json:"type"`
	Subtype         string        `json:"subtype,omitempty"`
	Role            string        `json:"role,omitempty"`
	ClientSessionID string        `json:"clientSessionId,omitempty"`
	RequestID       string        `json:"requestId,omitempty"`
	ContentTypes    []string      `json:"contentTypes,omitempty"`
	ContentBytes    int           `json:"contentBytes"`
	TextBytes       int           `json:"textBytes"`
	ThinkingBytes   int           `json:"thinkingBytes"`
	ToolUses        int           `json:"toolUses"`
	ToolResults     int           `json:"toolResults"`
	IsError         bool          `json:"isError"`
	ObservedAt      time.Time     `json:"observedAt,omitzero"`
	Evidence        []EvidenceRef `json:"evidence"`
}

type ProviderExchange struct {
	ID                   string        `json:"id"`
	Method               string        `json:"method,omitempty"`
	Path                 string        `json:"path,omitempty"`
	StatusCode           int           `json:"statusCode,omitempty"`
	Status               string        `json:"status"`
	StartedAt            time.Time     `json:"startedAt"`
	RequestEndedAt       time.Time     `json:"requestEndedAt,omitzero"`
	ResponseAt           time.Time     `json:"responseAt,omitzero"`
	EndedAt              time.Time     `json:"endedAt,omitzero"`
	DurationMs           int64         `json:"durationMs,omitempty"`
	TimingObserved       bool          `json:"timingObserved"`
	ProviderWaitMs       int64         `json:"providerWaitMs,omitempty"`
	ProviderWaitObserved bool          `json:"providerWaitObserved"`
	RequestBytes         int64         `json:"requestBytes"`
	ResponseBytes        int64         `json:"responseBytes"`
	ErrorPresent         bool          `json:"errorPresent"`
	ClientSessionID      string        `json:"clientSessionId,omitempty"`
	Model                string        `json:"model,omitempty"`
	Evidence             []EvidenceRef `json:"evidence"`
}

type TraceFilter struct {
	SessionID    string
	InvocationID string
}

type SessionTrace struct {
	FormatVersion string       `json:"formatVersion"`
	CaptureID     string       `json:"captureId"`
	SessionID     string       `json:"sessionId,omitempty"`
	InvocationID  string       `json:"invocationId,omitempty"`
	Summary       TraceSummary `json:"summary"`
	Steps         []TraceStep  `json:"steps"`
	Flow          SessionFlow  `json:"flow"`
}

type SessionFlow struct {
	Lanes   []FlowLane  `json:"lanes"`
	Phases  []FlowPhase `json:"phases"`
	Turns   []FlowTurn  `json:"turns"`
	Nodes   []FlowNode  `json:"nodes"`
	Edges   []FlowEdge  `json:"edges"`
	Summary FlowSummary `json:"summary"`
}

type FlowLane struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Order int    `json:"order"`
}

type FlowPhase struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	InvocationID   string        `json:"invocationId,omitempty"`
	StartTurnID    string        `json:"startTurnId"`
	EndTurnID      string        `json:"endTurnId"`
	StartTurnOrder int           `json:"startTurnOrder"`
	EndTurnOrder   int           `json:"endTurnOrder"`
	Basis          string        `json:"basis"`
	Evidence       []EvidenceRef `json:"evidence"`
}

type FlowTurn struct {
	ID              string    `json:"id"`
	Order           int       `json:"order"`
	ExchangeID      string    `json:"exchangeId"`
	InvocationID    string    `json:"invocationId,omitempty"`
	Phase           string    `json:"phase,omitempty"`
	Model           string    `json:"model,omitempty"`
	Status          string    `json:"status,omitempty"`
	HiddenByDefault bool      `json:"hiddenByDefault"`
	StartedAt       time.Time `json:"startedAt,omitzero"`
	EndedAt         time.Time `json:"endedAt,omitzero"`
	NodeIDs         []string  `json:"nodeIds"`
}

type FlowNode struct {
	ID                   string        `json:"id"`
	Order                int           `json:"order"`
	Lane                 string        `json:"lane"`
	Kind                 string        `json:"kind"`
	Title                string        `json:"title"`
	Subtitle             string        `json:"subtitle,omitempty"`
	Status               string        `json:"status,omitempty"`
	Propagation          string        `json:"propagation,omitempty"`
	HiddenByDefault      bool          `json:"hiddenByDefault"`
	TurnID               string        `json:"turnId,omitempty"`
	ExchangeID           string        `json:"exchangeId,omitempty"`
	InvocationID         string        `json:"invocationId,omitempty"`
	Phase                string        `json:"phase,omitempty"`
	ToolExecutionID      string        `json:"toolExecutionId,omitempty"`
	Model                string        `json:"model,omitempty"`
	StopReason           string        `json:"stopReason,omitempty"`
	PrimaryBytes         int           `json:"primaryBytes"`
	PrimaryBytesObserved bool          `json:"primaryBytesObserved"`
	DeltaBytes           int           `json:"deltaBytes,omitempty"`
	DeltaBytesObserved   bool          `json:"deltaBytesObserved"`
	TextBlockCount       int           `json:"textBlockCount,omitempty"`
	ThinkingBlockCount   int           `json:"thinkingBlockCount,omitempty"`
	ToolCount            int           `json:"toolCount,omitempty"`
	ContextReset         bool          `json:"contextReset"`
	HistoryRewritten     bool          `json:"historyRewritten"`
	StartedAt            time.Time     `json:"startedAt,omitzero"`
	EndedAt              time.Time     `json:"endedAt,omitzero"`
	DurationMs           int64         `json:"durationMs,omitempty"`
	TimingObserved       bool          `json:"timingObserved"`
	Dimensions           []TraceSize   `json:"dimensions"`
	StepIDs              []string      `json:"stepIds"`
	Evidence             []EvidenceRef `json:"evidence"`
}

type FlowEdge struct {
	ID                  string        `json:"id"`
	Kind                string        `json:"kind"`
	FromID              string        `json:"fromId"`
	ToID                string        `json:"toId"`
	Status              string        `json:"status"`
	Basis               string        `json:"basis"`
	HiddenByDefault     bool          `json:"hiddenByDefault"`
	Bytes               int           `json:"bytes,omitempty"`
	BytesObserved       bool          `json:"bytesObserved"`
	SourceBytes         int           `json:"sourceBytes,omitempty"`
	SourceBytesObserved bool          `json:"sourceBytesObserved"`
	TargetBytes         int           `json:"targetBytes,omitempty"`
	TargetBytesObserved bool          `json:"targetBytesObserved"`
	DeltaBytes          int           `json:"deltaBytes,omitempty"`
	DeltaBytesObserved  bool          `json:"deltaBytesObserved"`
	Evidence            []EvidenceRef `json:"evidence"`
}

type FlowSummary struct {
	TurnCount       int `json:"turnCount"`
	NodeCount       int `json:"nodeCount"`
	EdgeCount       int `json:"edgeCount"`
	MaxContextBytes int `json:"maxContextBytes"`
	MaxPayloadBytes int `json:"maxPayloadBytes"`
	BranchCount     int `json:"branchCount"`
	DifferenceCount int `json:"differenceCount"`
	ErrorCount      int `json:"errorCount"`
}

type TraceSummary struct {
	StepCount         int  `json:"stepCount"`
	PromptCount       int  `json:"promptCount"`
	ModelBlockCount   int  `json:"modelBlockCount"`
	ToolCount         int  `json:"toolCount"`
	ErrorCount        int  `json:"errorCount"`
	DifferenceCount   int  `json:"differenceCount"`
	ContentBytes      int  `json:"contentBytes"`
	ContentBytesKnown bool `json:"contentBytesKnown"`
}

type TraceStep struct {
	ID               string            `json:"id"`
	Order            int               `json:"order"`
	Kind             string            `json:"kind"`
	Actor            string            `json:"actor"`
	Title            string            `json:"title"`
	Status           string            `json:"status,omitempty"`
	Importance       string            `json:"importance"`
	HiddenByDefault  bool              `json:"hiddenByDefault"`
	GroupID          string            `json:"groupId,omitempty"`
	SessionID        string            `json:"sessionId,omitempty"`
	InvocationID     string            `json:"invocationId,omitempty"`
	Phase            string            `json:"phase,omitempty"`
	ExchangeID       string            `json:"exchangeId,omitempty"`
	ToolExecutionID  string            `json:"toolExecutionId,omitempty"`
	Propagation      string            `json:"propagation,omitempty"`
	StopReason       string            `json:"stopReason,omitempty"`
	StartedAt        time.Time         `json:"startedAt,omitzero"`
	EndedAt          time.Time         `json:"endedAt,omitzero"`
	DurationMs       int64             `json:"durationMs,omitempty"`
	TimingObserved   bool              `json:"timingObserved"`
	SizeBytes        int               `json:"sizeBytes"`
	SizeObserved     bool              `json:"sizeObserved"`
	Sizes            []TraceSize       `json:"sizes"`
	ContentRefs      []TraceContentRef `json:"contentRefs"`
	Evidence         []EvidenceRef     `json:"evidence"`
	CorrelationBasis string            `json:"correlationBasis"`
}

type TraceSize struct {
	Label    string `json:"label"`
	Bytes    int    `json:"bytes"`
	Observed bool   `json:"observed"`
}

type TraceContentRef struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Label          string      `json:"label"`
	Bytes          int         `json:"bytes"`
	BytesObserved  bool        `json:"bytesObserved"`
	FormatHint     string      `json:"formatHint"`
	RevealRequired bool        `json:"revealRequired"`
	Evidence       EvidenceRef `json:"evidence"`
}

type TraceContentDetail struct {
	Ref              string      `json:"ref"`
	Kind             string      `json:"kind"`
	Content          string      `json:"content"`
	Bytes            int         `json:"bytes"`
	FormatCandidates []string    `json:"formatCandidates"`
	Truncated        bool        `json:"truncated"`
	Evidence         EvidenceRef `json:"evidence"`
}

type ProviderEventDetail struct {
	ExchangeID    string      `json:"exchangeId"`
	Ordinal       int         `json:"ordinal"`
	DecodedOffset int64       `json:"decodedOffset"`
	Payload       string      `json:"payload"`
	Evidence      EvidenceRef `json:"evidence"`
}

type ProviderEventPage struct {
	FormatVersion string          `json:"formatVersion"`
	CaptureID     string          `json:"captureId"`
	Offset        int             `json:"offset"`
	Limit         int             `json:"limit"`
	Total         int             `json:"total"`
	Items         []ProviderEvent `json:"items"`
}

type ProviderEvent struct {
	ID             string      `json:"id"`
	ExchangeID     string      `json:"exchangeId"`
	Ordinal        int         `json:"ordinal"`
	Type           string      `json:"type"`
	Index          int         `json:"index,omitempty"`
	BlockType      string      `json:"blockType,omitempty"`
	DeltaType      string      `json:"deltaType,omitempty"`
	StopReason     string      `json:"stopReason,omitempty"`
	DecodedOffset  int64       `json:"decodedOffset"`
	PayloadBytes   int         `json:"payloadBytes"`
	TimestampBasis string      `json:"timestampBasis"`
	Evidence       EvidenceRef `json:"evidence"`
}

type ProviderBlock struct {
	ID              string      `json:"id"`
	ExchangeID      string      `json:"exchangeId"`
	Index           int         `json:"index"`
	Type            string      `json:"type"`
	ToolUseID       string      `json:"toolUseId,omitempty"`
	ToolName        string      `json:"toolName,omitempty"`
	TextBytes       int         `json:"textBytes"`
	ThinkingBytes   int         `json:"thinkingBytes"`
	InputJSONBytes  int         `json:"inputJsonBytes"`
	StartedOffset   int64       `json:"startedOffset"`
	CompletedOffset int64       `json:"completedOffset,omitempty"`
	Status          string      `json:"status"`
	Evidence        EvidenceRef `json:"evidence"`
}

type ContextSnapshot struct {
	ExchangeID                       string      `json:"exchangeId"`
	ClientSessionID                  string      `json:"clientSessionId,omitempty"`
	Model                            string      `json:"model,omitempty"`
	ProviderMessageID                string      `json:"providerMessageId,omitempty"`
	RequestBytes                     int         `json:"requestBytes"`
	SystemBlocks                     int         `json:"systemBlocks"`
	SystemBytes                      int         `json:"systemBytes"`
	ToolCount                        int         `json:"toolCount"`
	MCPToolCount                     int         `json:"mcpToolCount"`
	BuiltInToolCount                 int         `json:"builtInToolCount"`
	ToolBytes                        int         `json:"toolBytes"`
	MessageCount                     int         `json:"messageCount"`
	MessageBytes                     int         `json:"messageBytes"`
	OtherBytes                       int         `json:"otherBytes"`
	CommonPrefixMessages             int         `json:"commonPrefixMessages"`
	AddedMessages                    int         `json:"addedMessages"`
	AddedMessageBytes                int         `json:"addedMessageBytes"`
	RemovedMessages                  int         `json:"removedMessages"`
	RewrittenMessages                int         `json:"rewrittenMessages"`
	ContextReset                     bool        `json:"contextReset"`
	HistoryRewritten                 bool        `json:"historyRewritten"`
	SystemChanged                    bool        `json:"systemChanged"`
	ToolsChanged                     bool        `json:"toolsChanged"`
	InputTokens                      int64       `json:"inputTokens"`
	InputTokensObserved              bool        `json:"inputTokensObserved"`
	CacheCreationInputTokens         int64       `json:"cacheCreationInputTokens"`
	CacheCreationInputTokensObserved bool        `json:"cacheCreationInputTokensObserved"`
	CacheReadInputTokens             int64       `json:"cacheReadInputTokens"`
	CacheReadInputTokensObserved     bool        `json:"cacheReadInputTokensObserved"`
	OutputTokens                     int64       `json:"outputTokens"`
	OutputTokensObserved             bool        `json:"outputTokensObserved"`
	Evidence                         EvidenceRef `json:"evidence"`
}

type ContextDetail struct {
	ExchangeID   string                 `json:"exchangeId"`
	Model        string                 `json:"model,omitempty"`
	RequestBytes int                    `json:"requestBytes"`
	System       json.RawMessage        `json:"system,omitempty"`
	Tools        []ContextToolDetail    `json:"tools"`
	Messages     []ContextMessageDetail `json:"messages"`
	Metadata     json.RawMessage        `json:"metadata,omitempty"`
	RawRequest   json.RawMessage        `json:"rawRequest"`
	Evidence     EvidenceRef            `json:"evidence"`
}

type ContextToolDetail struct {
	Name  string          `json:"name"`
	Bytes int             `json:"bytes"`
	JSON  json.RawMessage `json:"json"`
}

type ContextMessageDetail struct {
	Role         string          `json:"role"`
	Bytes        int             `json:"bytes"`
	ContentTypes []string        `json:"contentTypes,omitempty"`
	JSON         json.RawMessage `json:"json"`
}

type ToolExecution struct {
	ID                  string        `json:"id"`
	InvocationID        string        `json:"invocationId,omitempty"`
	ClientSessionID     string        `json:"clientSessionId,omitempty"`
	Category            string        `json:"category"`
	ToolName            string        `json:"toolName"`
	ToolUseID           string        `json:"toolUseId,omitempty"`
	ClientArtifact      string        `json:"clientArtifact,omitempty"`
	ProposalExchangeID  string        `json:"proposalExchangeId,omitempty"`
	ResultExchangeID    string        `json:"resultExchangeId,omitempty"`
	MCPFile             string        `json:"mcpFile,omitempty"`
	ProviderToolName    string        `json:"providerToolName,omitempty"`
	MCPToolName         string        `json:"mcpToolName,omitempty"`
	MCPRequestID        string        `json:"mcpRequestId,omitempty"`
	ArgumentsBytes      int           `json:"argumentsBytes"`
	ArgumentsEqual      bool          `json:"argumentsEqual"`
	ResultBytes         int           `json:"resultBytes"`
	ProviderResultBytes int           `json:"providerResultBytes"`
	IsError             bool          `json:"isError"`
	Propagation         string        `json:"propagation"`
	CorrelationBasis    string        `json:"correlationBasis"`
	SourceOwners        []string      `json:"sourceOwners,omitempty"`
	CompositionOwners   []string      `json:"compositionOwners,omitempty"`
	StartedAt           time.Time     `json:"startedAt"`
	CompletedAt         time.Time     `json:"completedAt,omitzero"`
	DurationMs          int64         `json:"durationMs,omitempty"`
	TimingObserved      bool          `json:"timingObserved"`
	Evidence            []EvidenceRef `json:"evidence"`
}

type MCPCall struct {
	ID               string        `json:"id"`
	File             string        `json:"file"`
	RequestID        string        `json:"requestId,omitempty"`
	Kind             string        `json:"kind"`
	Method           string        `json:"method,omitempty"`
	ToolName         string        `json:"toolName,omitempty"`
	Status           string        `json:"status"`
	InvocationID     string        `json:"invocationId,omitempty"`
	Phase            string        `json:"phase,omitempty"`
	RequestBytes     int           `json:"requestBytes"`
	ResponseBytes    int           `json:"responseBytes"`
	StartedAt        time.Time     `json:"startedAt"`
	CompletedAt      time.Time     `json:"completedAt,omitzero"`
	DurationMs       int64         `json:"durationMs,omitempty"`
	TimingObserved   bool          `json:"timingObserved"`
	CorrelationBasis string        `json:"correlationBasis"`
	Evidence         []EvidenceRef `json:"evidence"`
}

type MCPProcess struct {
	File                  string `json:"file"`
	Status                string `json:"status"`
	EvalRunID             string `json:"evalRunId,omitempty"`
	ScenarioRunID         string `json:"scenarioRunId,omitempty"`
	InvocationID          string `json:"invocationId,omitempty"`
	Phase                 string `json:"phase,omitempty"`
	InputBytes            int64  `json:"inputBytes"`
	OutputBytes           int64  `json:"outputBytes"`
	ToolCalls             int    `json:"toolCalls"`
	ProgressNotifications int    `json:"progressNotifications"`
}

type SourceOwner struct {
	Kind         string   `json:"kind"`
	Owner        string   `json:"owner"`
	File         string   `json:"file,omitempty"`
	Occurrences  int      `json:"occurrences"`
	MatchedBytes int      `json:"matchedBytes"`
	ToolIDs      []string `json:"toolIds,omitempty"`
}

type RawFile struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type RawRecordPage struct {
	FormatVersion string             `json:"formatVersion"`
	CaptureID     string             `json:"captureId"`
	File          string             `json:"file"`
	After         uint64             `json:"after"`
	Limit         int                `json:"limit"`
	NextAfter     uint64             `json:"nextAfter"`
	HasMore       bool               `json:"hasMore"`
	Items         []RawRecordSummary `json:"items"`
}

type RawRecordSummary struct {
	ID            string    `json:"id"`
	File          string    `json:"file"`
	Seq           uint64    `json:"seq"`
	Time          time.Time `json:"time"`
	Kind          string    `json:"kind"`
	ExchangeID    string    `json:"exchangeId,omitempty"`
	ProcessID     int       `json:"processId,omitempty"`
	EvalRunID     string    `json:"evalRunId,omitempty"`
	ScenarioRunID string    `json:"scenarioRunId,omitempty"`
	InvocationID  string    `json:"invocationId,omitempty"`
	Phase         string    `json:"phase,omitempty"`
	Direction     string    `json:"direction,omitempty"`
	BodyBytes     int64     `json:"bodyBytes,omitempty"`
	StreamOffset  int64     `json:"streamOffset,omitempty"`
	StatusCode    int       `json:"statusCode,omitempty"`
	CaptureStatus string    `json:"captureStatus,omitempty"`
	HasBody       bool      `json:"hasBody"`
	HasError      bool      `json:"hasError"`
}

type Artifact struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	Type      string `json:"type"`
}

type ArtifactLineDetail struct {
	Path      string `json:"path"`
	Line      uint64 `json:"line"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type RawDetail struct {
	File          string                     `json:"file"`
	Record        *capture.Record            `json:"record,omitempty"`
	Lifecycle     *capture.LifecycleRecord   `json:"lifecycle,omitempty"`
	Composition   *capture.CompositionRecord `json:"composition,omitempty"`
	BodyText      string                     `json:"bodyText,omitempty"`
	BodyBase64    string                     `json:"bodyBase64,omitempty"`
	BodyTruncated bool                       `json:"bodyTruncated"`
}

type ArtifactDetail struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type ToolDetail struct {
	ID                      string                           `json:"id"`
	Category                string                           `json:"category"`
	ToolName                string                           `json:"toolName"`
	ArgumentsJSON           string                           `json:"argumentsJson"`
	ResultText              string                           `json:"resultText"`
	IsError                 bool                             `json:"isError"`
	MCPResultText           string                           `json:"mcpResultText,omitempty"`
	MCPIsError              bool                             `json:"mcpIsError"`
	ProviderResultText      string                           `json:"providerResultText,omitempty"`
	ProviderResultError     bool                             `json:"providerResultError"`
	ArgumentsTruncated      bool                             `json:"argumentsTruncated"`
	ResultTruncated         bool                             `json:"resultTruncated"`
	ProviderResultTruncated bool                             `json:"providerResultTruncated"`
	Propagation             string                           `json:"propagation"`
	SourceMatches           []capture.ContentSourceMatch     `json:"sourceMatches,omitempty"`
	Composition             []capture.CompositionSourceMatch `json:"compositionMatches,omitempty"`
	Evidence                []EvidenceRef                    `json:"evidence"`
}

type StructuralDiagnostic struct {
	Code     string        `json:"code"`
	Severity string        `json:"severity"`
	Summary  string        `json:"summary"`
	Basis    string        `json:"basis"`
	ScopeID  string        `json:"scopeId,omitempty"`
	Evidence []EvidenceRef `json:"evidence,omitempty"`
}

type CaptureIndexEntry struct {
	ID           string    `json:"id"`
	Label        string    `json:"label,omitempty"`
	Status       string    `json:"status"`
	Integrity    string    `json:"integrity"`
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt,omitzero"`
	DurationMs   int64     `json:"durationMs,omitempty"`
	SizeBytes    int64     `json:"sizeBytes"`
	BuildVersion string    `json:"buildVersion,omitempty"`
	BuildCommit  string    `json:"buildCommit,omitempty"`
	Plaintext    bool      `json:"plaintext"`
	SessionPath  string    `json:"-"`
}

type Edge struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	FromID   string        `json:"fromId"`
	ToID     string        `json:"toId"`
	Basis    string        `json:"basis"`
	Evidence []EvidenceRef `json:"evidence,omitempty"`
}

type Metric struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Unit          string        `json:"unit"`
	Scope         string        `json:"scope"`
	Value         *float64      `json:"value"`
	Denominator   *float64      `json:"denominator"`
	SampleCount   int           `json:"sampleCount"`
	MissingCount  int           `json:"missingCount"`
	EvidenceBasis string        `json:"evidenceBasis"`
	Description   string        `json:"description,omitempty"`
	Evidence      []EvidenceRef `json:"evidence,omitempty"`
}

type Comparison struct {
	LeftID  string        `json:"leftId"`
	RightID string        `json:"rightId"`
	Metrics []MetricDelta `json:"metrics"`
}

type MetricDelta struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Unit              string   `json:"unit"`
	Left              *float64 `json:"left"`
	Right             *float64 `json:"right"`
	Delta             *float64 `json:"delta"`
	Percent           *float64 `json:"percent"`
	LeftMissingCount  int      `json:"leftMissingCount"`
	RightMissingCount int      `json:"rightMissingCount"`
	EvidenceBasis     string   `json:"evidenceBasis"`
}
