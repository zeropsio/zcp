package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ConnectionFromEnvironment discovers a scoped wrapper capture. All
// coordinates are required together; half-configuration is a visible error.
func ConnectionFromEnvironment(ctx context.Context) (*Connection, bool, error) {
	captureID := strings.TrimSpace(os.Getenv(EnvSessionID))
	sessionDir := strings.TrimSpace(os.Getenv(EnvSessionDir))
	socket := strings.TrimSpace(os.Getenv(EnvControlSocket))
	token := strings.TrimSpace(os.Getenv(EnvControlToken))
	values := []string{captureID, sessionDir, socket, token}
	present := 0
	for _, value := range values {
		if value != "" {
			present++
		}
	}
	if present == 0 {
		return nil, false, nil
	}
	if present != len(values) {
		return nil, true, errors.New("scoped capture requires session ID, session directory, control socket, and control token")
	}
	client := NewControlClient(socket, token)
	healthCtx, cancel := context.WithTimeout(ctx, time.Second)
	status, err := client.Status(healthCtx)
	cancel()
	if err != nil {
		client.Close()
		return nil, true, fmt.Errorf("verify scoped capture control: %w", err)
	}
	if status.CaptureID != captureID || status.SessionDir != sessionDir || status.ProxyURL == "" {
		client.Close()
		return nil, true, fmt.Errorf("scoped capture environment differs from daemon status")
	}
	return &Connection{CaptureID: captureID, ProxyURL: status.ProxyURL, SessionDir: sessionDir, Control: client}, true, nil
}
