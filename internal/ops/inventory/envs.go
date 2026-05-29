// Package inventory is the Layer 2 entry point for stable env + service
// reads. It re-exports the SDK-shaped types from `platform` under names
// callable from `envclass` (Layer 3) and other layers per the layer rule
// (plans/workflow-family-architecture-2026-05-14.md §9.4 + §9.7) — so
// upper layers depend on a stable Layer-2 name, not directly on the
// raw `platform` wrapper.
//
// Types are Go aliases (not duplicate definitions). One canonical struct
// shape; multiple callable names. SDK shape changes ripple through the
// `platform` definition without source-level coupling on `envclass` or
// `ops/bundle`.
package inventory

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// ProjectEnvVar is the Layer 2 alias for `platform.ProjectEnvVar`.
// Carries SDK-decoded `Type` (`USER` | `SYSTEM`), `Sensitive`, `Editable`.
type ProjectEnvVar = platform.ProjectEnvVar

// ServiceEnvVar is the Layer 2 alias for `platform.ServiceEnvVar`.
// Carries SDK-decoded `Type` (`READ_ONLY` | `EDITABLE` | `SECRET` |
// `INTERNAL` | `ENV`) and `Sensitive`. No `Editable` field — the SDK
// `ServiceStackEnv` DTO doesn't expose it.
type ServiceEnvVar = platform.ServiceEnvVar

// FetchProjectEnvs reads project-level env vars from the platform API.
// Layer-2 wrapper around `client.GetProjectEnv`; provides a stable entry
// point for upper layers (envclass, ops/bundle, handlers).
func FetchProjectEnvs(ctx context.Context, client platform.Client, projectID string) ([]ProjectEnvVar, error) {
	return client.GetProjectEnv(ctx, projectID)
}
