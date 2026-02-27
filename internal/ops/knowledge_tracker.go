package ops

import (
	"strings"
	"sync"
)

// KnowledgeTracker records zerops_knowledge calls so the bootstrap workflow
// can detect when knowledge is already loaded and skip redundant calls.
type KnowledgeTracker struct {
	mu            sync.Mutex
	briefingCalls []string // e.g., ["php-nginx@8.4+postgresql@16,valkey@7.2"]
	scopeLoaded   bool
}

// NewKnowledgeTracker creates a new empty tracker.
func NewKnowledgeTracker() *KnowledgeTracker {
	return &KnowledgeTracker{}
}

// RecordBriefing records a briefing-mode knowledge call.
func (kt *KnowledgeTracker) RecordBriefing(runtime string, services []string) {
	kt.mu.Lock()
	defer kt.mu.Unlock()
	entry := runtime
	if len(services) > 0 {
		entry += "+" + strings.Join(services, ",")
	}
	kt.briefingCalls = append(kt.briefingCalls, entry)
}

// RecordScope records that the infrastructure scope was loaded.
func (kt *KnowledgeTracker) RecordScope() {
	kt.mu.Lock()
	defer kt.mu.Unlock()
	kt.scopeLoaded = true
}

// IsLoaded returns true if both briefing and scope knowledge have been loaded.
func (kt *KnowledgeTracker) IsLoaded() bool {
	kt.mu.Lock()
	defer kt.mu.Unlock()
	return len(kt.briefingCalls) > 0 && kt.scopeLoaded
}

// Summary returns a human-readable summary of what knowledge has been loaded.
func (kt *KnowledgeTracker) Summary() string {
	kt.mu.Lock()
	defer kt.mu.Unlock()

	var parts []string
	if len(kt.briefingCalls) > 0 {
		parts = append(parts, "Briefing: "+strings.Join(kt.briefingCalls, "; "))
	}
	if kt.scopeLoaded {
		parts = append(parts, "Scope: infrastructure")
	}
	if len(parts) == 0 {
		return "No knowledge loaded"
	}
	return strings.Join(parts, " | ")
}
