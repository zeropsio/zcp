package workflow

import (
	"fmt"
	"time"
)

// UpdateContextDelivery loads the current state, applies the update function to
// the bootstrap ContextDelivery, and saves the state.
func (e *Engine) UpdateContextDelivery(fn func(*ContextDelivery)) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("update context delivery: %w", err)
	}
	if state.Bootstrap == nil {
		return nil
	}
	if state.Bootstrap.Context == nil {
		state.Bootstrap.Context = &ContextDelivery{GuideSentFor: make(map[string]int)}
	}
	fn(state.Bootstrap.Context)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
}
