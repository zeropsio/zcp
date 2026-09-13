package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/schema"
)

// execSSHFunc mirrors platform.SystemSSHDeployer.ExecSSH's shape as a plain
// func value (RuntimeInputs.ExecSSH) rather than an interface, so these
// evaluators carry no platform.Client dependency and unit tests can inject
// a bare closure.
type execSSHFunc func(ctx context.Context, hostname, command string) ([]byte, error)

// truncateObserved caps an Observed/Message-bound string at 200 characters
// (docs/spec-eval-farm.md §4.1 row conventions) so a large body/stdout/error
// join never blows up a result row. Byte-based; not rune-boundary aware —
// acceptable for the ASCII-heavy diagnostic text these rows carry.
func truncateObserved(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// internalLivenessRowID builds the O2' internalLiveness row id
// (docs/spec-eval-farm.md §4.1 FM-61 row conventions).
func internalLivenessRowID(service string) string {
	return fmt.Sprintf("internal_liveness/%s", service)
}

// evaluateInternalLivenessRows evaluates the O2' internalLiveness oracle: a
// GET over the project network (no subdomain involved), never skipped — an
// unreachable service is `failed`, never `blocked` (docs/spec-eval-farm.md
// §4.1 FM-61). probe == nil emits nothing. The request carries its own 10s
// timeout independent of httpDoer's own configuration; the body read is
// capped at 1MB (maxLivenessBodyBytes, verification.go) before the Observed
// field is further truncated to 200 chars.
func evaluateInternalLivenessRows(ctx context.Context, probe *InternalLivenessProbe, _ string, _ execSSHFunc, httpDoer ops.HTTPDoer, now time.Time) []RequiredCheck {
	if probe == nil {
		return nil
	}
	id := internalLivenessRowID(probe.Service)
	if httpDoer == nil {
		// No HTTP transport wired (an offline harness) — the probe could not
		// run, which is blocked, not a verdict about the service.
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckBlocked, ObservedAt: now, Source: "internal-liveness", Message: "no HTTP transport available to the evaluator"}}
	}
	expected := "2xx"
	if probe.Marker != "" {
		expected = fmt.Sprintf("2xx with marker %q", probe.Marker)
	}
	url := "http://" + net.JoinHostPort(probe.Service, strconv.Itoa(probe.Port)) + probe.Path

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckFailed, Expected: expected, ObservedAt: now, Source: "internal-liveness", Message: fmt.Sprintf("build request for %s: %v", url, err)}}
	}
	resp, err := httpDoer.Do(req)
	if err != nil {
		// FM-61: an unreachable service is failed, never blocked — unlike
		// the subdomain-facing O2 liveness row, there is no ambiguity here
		// about whether the probe ran; the project network is always
		// reachable from inside the run container.
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckFailed, Expected: expected, ObservedAt: now, Source: "internal-liveness", Message: fmt.Sprintf("GET %s: %v", url, err)}}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxLivenessBodyBytes))
	if readErr != nil {
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckFailed, Expected: expected, ObservedAt: now, Source: "internal-liveness", Message: fmt.Sprintf("read body from %s: %v", url, readErr)}}
	}
	observed := fmt.Sprintf("%d %s", resp.StatusCode, truncateObserved(string(body)))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckFailed, Expected: expected, Observed: observed, ObservedAt: now, Source: "internal-liveness", Message: fmt.Sprintf("GET %s returned %d, expected 2xx", url, resp.StatusCode)}}
	}
	if probe.Marker != "" && !strings.Contains(string(body), probe.Marker) {
		return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckFailed, Expected: expected, Observed: observed, ObservedAt: now, Source: "internal-liveness", Message: fmt.Sprintf("GET %s returned 2xx but body does not contain marker %q", url, probe.Marker)}}
	}
	msg := fmt.Sprintf("GET %s returned %d", url, resp.StatusCode)
	if probe.Marker != "" {
		msg = fmt.Sprintf("GET %s returned %d with marker %q present", url, resp.StatusCode, probe.Marker)
	}
	return []RequiredCheck{{ID: id, Check: "internalLiveness", Scope: probe.Service, Result: CheckPassed, Expected: expected, Observed: observed, ObservedAt: now, Source: "internal-liveness", Message: msg}}
}

// containerCheckRowID builds the O10 containerCheck row id
// (docs/spec-eval-farm.md §4.1 FM-61 row conventions).
func containerCheckRowID(service string, n int) string {
	return fmt.Sprintf("container_check/%s/%d", service, n)
}

// evaluateContainerCheckRows evaluates one O10 containerCheck oracle entry
// per declared entry: a single command over SSH via
// platform.SystemSSHDeployer.ExecSSH (injected as execSSH), matched exact
// (Expect) or regex (Match) against stdout. cmd is passed through verbatim —
// the scenario author owns quoting, never composed via fmt.Sprintf here. A
// non-zero exit is `failed`, never `blocked` (docs/spec-eval-farm.md §4.1
// FM-61); ExecSSH's combined stdout+stderr output rides in Observed either
// way (platform.SystemSSHDeployer.ExecSSH returns it alongside the error).
func evaluateContainerCheckRows(ctx context.Context, entries []ContainerCheckEntry, _ string, execSSH execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for i, entry := range entries {
		rows = append(rows, evaluateOneContainerCheck(ctx, entry, i+1, execSSH, now))
	}
	return rows
}

func evaluateOneContainerCheck(ctx context.Context, entry ContainerCheckEntry, n int, execSSH execSSHFunc, now time.Time) RequiredCheck {
	id := containerCheckRowID(entry.Service, n)
	if execSSH == nil {
		return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckBlocked, ObservedAt: now, Source: "container-check", Message: "no SSH executor available to the evaluator"}
	}
	out, err := execSSH(ctx, entry.Service, entry.Cmd)
	observed := truncateObserved(string(out))
	if err != nil {
		return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckFailed, Observed: observed, ObservedAt: now, Source: "container-check", Message: fmt.Sprintf("command exited non-zero: %v", err)}
	}
	stdout := string(out)
	switch {
	case entry.Expect != "":
		expected := fmt.Sprintf("stdout == %q", entry.Expect)
		if strings.TrimSpace(stdout) == entry.Expect {
			return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckPassed, Expected: expected, Observed: observed, ObservedAt: now, Source: "container-check", Message: fmt.Sprintf("stdout exactly matched %q", entry.Expect)}
		}
		return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckFailed, Expected: expected, Observed: observed, ObservedAt: now, Source: "container-check", Message: "stdout did not match"}
	case entry.Match != "":
		expected := fmt.Sprintf("stdout ~= /%s/", entry.Match)
		re, reErr := regexp.Compile(entry.Match)
		if reErr != nil {
			return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckFailed, Expected: expected, Observed: observed, ObservedAt: now, Source: "container-check", Message: fmt.Sprintf("invalid regex %q: %v", entry.Match, reErr)}
		}
		if re.MatchString(stdout) {
			return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckPassed, Expected: expected, Observed: observed, ObservedAt: now, Source: "container-check", Message: fmt.Sprintf("stdout matched regex %q", entry.Match)}
		}
		return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckFailed, Expected: expected, Observed: observed, ObservedAt: now, Source: "container-check", Message: "stdout did not match"}
	default:
		// scenario.go's validate() is expected to reject an entry with
		// neither expect nor match (exactly one required); this branch is
		// an evaluator-side safety net, not a documented row shape.
		return RequiredCheck{ID: id, Check: "containerCheck", Scope: entry.Service, Result: CheckFailed, Observed: observed, ObservedAt: now, Source: "container-check", Message: "neither expect nor match declared"}
	}
}

// metaRowID builds the O11 meta row id (docs/spec-eval-farm.md §4.1 FM-61
// row conventions).
func metaRowID(hostname, field string) string {
	return fmt.Sprintf("meta/%s/%s", hostname, field)
}

// evaluateMetaRows evaluates one O11 meta oracle entry per declared entry: a
// field read from the candidate's `.zcp/state/services/<hostname>.json`
// (workflow.ServiceMeta's on-disk shape, internal/workflow/service_meta.go)
// under workDir — the evaluator shares the container filesystem, so the
// file is read directly rather than over SSH. Decoded into a generic
// map[string]any (not workflow.ServiceMeta) since Field names an arbitrary
// top-level key, not a fixed Go struct field; compared via fmt.Sprint so a
// non-string JSON value (bool/number) still compares textually. A missing
// file or absent field is `failed`, never `blocked` (docs/spec-eval-farm.md
// §4.1 FM-61).
func evaluateMetaRows(_ context.Context, entries []MetaCheckEntry, workDir string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	rows := make([]RequiredCheck, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, evaluateOneMetaCheck(entry, workDir, now))
	}
	return rows
}

func evaluateOneMetaCheck(entry MetaCheckEntry, workDir string, now time.Time) RequiredCheck {
	id := metaRowID(entry.Hostname, entry.Field)
	path := filepath.Join(workDir, ".zcp", "state", "services", entry.Hostname+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return RequiredCheck{ID: id, Check: "meta", Scope: entry.Hostname, Result: CheckFailed, Expected: entry.Expect, ObservedAt: now, Source: "meta", Message: fmt.Sprintf("read %s: %v", path, err)}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return RequiredCheck{ID: id, Check: "meta", Scope: entry.Hostname, Result: CheckFailed, Expected: entry.Expect, ObservedAt: now, Source: "meta", Message: fmt.Sprintf("parse %s: %v", path, err)}
	}
	value, ok := doc[entry.Field]
	if !ok {
		return RequiredCheck{ID: id, Check: "meta", Scope: entry.Hostname, Result: CheckFailed, Expected: entry.Expect, Observed: "absent", ObservedAt: now, Source: "meta", Message: fmt.Sprintf("field %s absent", entry.Field)}
	}
	observed := truncateObserved(fmt.Sprint(value))
	if observed != entry.Expect {
		return RequiredCheck{ID: id, Check: "meta", Scope: entry.Hostname, Result: CheckFailed, Expected: entry.Expect, Observed: observed, ObservedAt: now, Source: "meta", Message: fmt.Sprintf("field %s: expected %q, observed %q", entry.Field, entry.Expect, observed)}
	}
	return RequiredCheck{ID: id, Check: "meta", Scope: entry.Hostname, Result: CheckPassed, Expected: entry.Expect, Observed: observed, ObservedAt: now, Source: "meta", Message: fmt.Sprintf("field %s matched %q", entry.Field, observed)}
}

// schemaValidRowID builds the O12 schemaValid row id (docs/spec-eval-farm.md
// §4.1 FM-61 row conventions).
func schemaValidRowID(artifact string) string {
	return fmt.Sprintf("schema_valid/%s", filepath.Base(artifact))
}

// evaluateSchemaValidRow evaluates the O12 schemaValid oracle: schema-
// validate a produced artifact against the live schema. check == nil emits
// nothing. Artifact is resolved against workDir when relative (the
// evaluator shares the container filesystem — same base as the meta
// family's state read); a missing file is `failed`, never `blocked`
// (docs/spec-eval-farm.md §4.1 FM-61). Dispatches by basename: a
// zerops.yaml/zerops.yml artifact validates via
// schema.ValidateZeropsYmlRaw against schema.Embedded()'s extracted valid
// fields; any other artifact (import.yaml, an export bundle's project
// definition, …) validates via schema.ValidateImportYAML.
func evaluateSchemaValidRow(_ context.Context, check *SchemaValidCheck, workDir string, _ execSSHFunc, _ ops.HTTPDoer, now time.Time) []RequiredCheck {
	if check == nil {
		return nil
	}
	id := schemaValidRowID(check.Artifact)
	path := check.Artifact
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []RequiredCheck{{ID: id, Check: "schemaValid", Scope: filepath.Base(check.Artifact), Result: CheckFailed, Expected: "schema-valid artifact", Observed: "file missing", ObservedAt: now, Source: "schema", Message: fmt.Sprintf("read %s: %v", path, err)}}
	}

	var violations []string
	base := strings.ToLower(filepath.Base(check.Artifact))
	if base == "zerops.yaml" || base == "zerops.yml" {
		vf := schema.ExtractValidFields(schema.Embedded().ZeropsYml)
		for _, fe := range schema.ValidateZeropsYmlRaw(data, vf) {
			violations = append(violations, fe.Error())
		}
	} else {
		for _, ve := range schema.ValidateImportYAML(string(data)) {
			violations = append(violations, ve.Error())
		}
	}

	if len(violations) > 0 {
		return []RequiredCheck{{ID: id, Check: "schemaValid", Scope: filepath.Base(check.Artifact), Result: CheckFailed, Expected: "no schema violations", Observed: truncateObserved(strings.Join(violations, "; ")), ObservedAt: now, Source: "schema", Message: fmt.Sprintf("%d schema violation(s)", len(violations))}}
	}
	return []RequiredCheck{{ID: id, Check: "schemaValid", Scope: filepath.Base(check.Artifact), Result: CheckPassed, Expected: "no schema violations", Observed: "valid", ObservedAt: now, Source: "schema", Message: fmt.Sprintf("%s schema-valid", filepath.Base(check.Artifact))}}
}
