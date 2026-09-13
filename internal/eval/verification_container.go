package eval

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
)

// execSSHFunc mirrors platform.SystemSSHDeployer.ExecSSH's shape as a plain
// func value (RuntimeInputs.ExecSSH) rather than an interface, so these
// evaluators carry no platform.Client dependency and unit tests can inject
// a bare closure.
type execSSHFunc func(ctx context.Context, hostname, command string) ([]byte, error)

// containerFamilyRowID builds the stable row id for a container-side
// oracle family entry: "<family>/<n>", one-based (docs/spec-eval-farm.md
// §4.1 FM-61).
func containerFamilyRowID(family string, n int) string {
	return fmt.Sprintf("%s/%d", family, n)
}

// notImplementedStub builds a not-run row declaring that family/id's oracle
// body has not landed yet — S3/S4 fill these in; this slice only declares
// the row and threads the inputs the real bodies will need (ctx, workDir,
// execSSH, httpDoer).
func notImplementedStub(family string, n int, now time.Time) RequiredCheck {
	return RequiredCheck{
		ID: containerFamilyRowID(family, n), Check: family,
		Result: CheckNotRun, ObservedAt: now,
		Message: "oracle not implemented (S3/S4)",
	}
}

// evaluateInternalLivenessRows emits the O2' internalLiveness stub row
// (docs/spec-eval-farm.md §4.1 FM-61: a GET over the project network, no
// subdomain). probe == nil emits nothing. Body lands in S3/S4.
func evaluateInternalLivenessRows(_ context.Context, probe *InternalLivenessProbe, _ string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	if probe == nil {
		return nil
	}
	return []RequiredCheck{notImplementedStub("internalLiveness", 1, now)}
}

// evaluateContainerCheckRows emits one O10 containerCheck stub row per
// declared entry (docs/spec-eval-farm.md §4.1 FM-61: one SSH command,
// exact/regex match). Body lands in S3/S4.
func evaluateContainerCheckRows(_ context.Context, entries []ContainerCheckEntry, _ string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i := range entries {
		rows = append(rows, notImplementedStub("containerCheck", i+1, now))
	}
	return rows
}

// evaluateMetaRows emits one O11 meta stub row per declared entry
// (docs/spec-eval-farm.md §4.1 FM-61: a field read from the candidate's
// .zcp/state). Body lands in S3/S4.
func evaluateMetaRows(_ context.Context, entries []MetaCheckEntry, _ string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i := range entries {
		rows = append(rows, notImplementedStub("meta", i+1, now))
	}
	return rows
}

// evaluateSchemaValidRow emits the O12 schemaValid stub row
// (docs/spec-eval-farm.md §4.1 FM-61: schema-validate a produced artifact).
// check == nil emits nothing. Body lands in S3/S4.
func evaluateSchemaValidRow(_ context.Context, check *SchemaValidCheck, _ string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	if check == nil {
		return nil
	}
	return []RequiredCheck{notImplementedStub("schemaValid", 1, now)}
}
