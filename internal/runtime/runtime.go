// Package runtime detects whether ZCP is running inside a Zerops container.
// The Info struct is resolved once at startup and passed down as a value parameter.
package runtime

import "os"

// Info holds runtime environment detection results.
type Info struct {
	InContainer bool   // true when running in a Zerops container
	ServiceName string // hostname from container env (empty when not in container)
	ServiceID   string // Zerops service ID (only in container)
	ProjectID   string // Zerops project ID (only in container)
}

// Detect reads Zerops container env vars and returns runtime info.
// The serviceId env var is injected by Zerops into every container — its
// presence is the definitive signal for container detection.
func Detect() Info {
	serviceID := os.Getenv("serviceId")
	if serviceID == "" {
		return Info{}
	}
	return Info{
		InContainer: true,
		ServiceName: os.Getenv("hostname"),
		ServiceID:   serviceID,
		ProjectID:   os.Getenv("projectId"),
	}
}
