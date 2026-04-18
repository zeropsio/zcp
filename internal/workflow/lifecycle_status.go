package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
)

// BuildLifecycleStatus renders the canonical orientation block returned by
// zerops_workflow action="status" when no bootstrap/recipe is active.
//
// Sections: Phase, Services (with setup), Progress (develop only), Next.
// Vocabulary is strictly user-facing — internal state (work sessions, service
// metas, sessions directory, PIDs) is encapsulated behind concrete tool calls.
func BuildLifecycleStatus(
	ctx context.Context,
	client platform.Client,
	projectID, stateDir, selfHostname string,
) string {
	var (
		services []platform.ServiceStack
		metas    []*ServiceMeta
		ws       *WorkSession
		wg       sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); services, _ = fetchServices(ctx, client, projectID) }()
	go func() { defer wg.Done(); metas, _ = ListServiceMetas(stateDir) }()
	go func() { defer wg.Done(); ws, _ = CurrentWorkSession(stateDir) }()
	wg.Wait()

	var b strings.Builder
	b.WriteString("## Status\n")

	writeStatusPhase(&b, ws)
	scope := writeStatusServices(&b, ws, services, metas, selfHostname)
	writeStatusProgress(&b, ws)
	writeStatusNext(&b, ws, scope, services, metas, selfHostname)

	return b.String()
}

// fetchServices returns live services or an empty slice when the client is
// unconfigured. The error is surfaced so callers can distinguish "no services"
// from "can't reach API".
func fetchServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	if client == nil || projectID == "" {
		return nil, nil
	}
	return client.ListServices(ctx, projectID)
}

func writeStatusPhase(b *strings.Builder, ws *WorkSession) {
	if ws == nil {
		b.WriteString("Phase: idle\n")
		return
	}
	if ws.ClosedAt != "" && ws.CloseReason == CloseReasonAutoComplete {
		fmt.Fprintf(b, "Phase: develop — task complete (intent: %q)\n", ws.Intent)
		return
	}
	fmt.Fprintf(b, "Phase: develop — intent: %q (%s)\n", ws.Intent, formatWorkDuration(ws.CreatedAt))
}

// writeStatusServices renders the Services section and returns the scope the
// "Next" section should operate on.
//
// Scope rules:
//   - develop session open → scope = ws.Services
//   - idle                 → scope = every live runtime hostname (sans self)
func writeStatusServices(
	b *strings.Builder,
	ws *WorkSession,
	services []platform.ServiceStack,
	metas []*ServiceMeta,
	selfHostname string,
) []string {
	typeMap := indexServiceTypes(services)
	statusMap := indexServiceStatuses(services)
	metaByHost := indexMetas(metas)

	var scope []string
	if ws != nil && ws.ClosedAt == "" {
		scope = ws.Services
	} else {
		scope = projectScope(services, selfHostname)
	}

	if len(scope) == 0 {
		b.WriteString("Services: none\n")
		return scope
	}

	fmt.Fprintf(b, "Services: %s\n", strings.Join(scope, ", "))
	for _, h := range scope {
		writeStatusServiceLine(b, h, typeMap[h], statusMap[h], metaByHost[h])
	}
	return scope
}

// writeStatusServiceLine describes one service: type, mode+strategy (if
// bootstrapped), "managed", or "not bootstrapped".
func writeStatusServiceLine(b *strings.Builder, hostname, svcType, status string, meta *ServiceMeta) {
	typeStr := svcType
	if typeStr == "" {
		typeStr = "unknown"
	}

	setup := describeServiceSetup(svcType, meta)
	statusSuffix := ""
	if status != "" && status != "ACTIVE" {
		statusSuffix = " [" + status + "]"
	}
	fmt.Fprintf(b, "  - %s (%s): %s%s\n", hostname, typeStr, setup, statusSuffix)
}

func describeServiceSetup(svcType string, meta *ServiceMeta) string {
	if meta != nil && meta.IsComplete() {
		parts := []string{"mode=" + modeOrDefault(meta.Mode)}
		if s := meta.EffectiveStrategy(); s != "" {
			parts = append(parts, "strategy="+s)
		} else {
			parts = append(parts, "strategy unset")
		}
		if meta.StageHostname != "" {
			parts = append(parts, "stage="+meta.StageHostname)
		}
		return strings.Join(parts, ", ")
	}
	if IsManagedService(svcType) {
		return "managed"
	}
	return "not bootstrapped — auto-adopted on develop start"
}

func modeOrDefault(mode string) string {
	if mode == "" {
		return "standard"
	}
	return mode
}

// writeStatusProgress writes deploy+verify state per service in the current
// work session. No-op when there is no active task.
func writeStatusProgress(b *strings.Builder, ws *WorkSession) {
	if ws == nil || ws.ClosedAt != "" {
		return
	}
	if len(ws.Deploys) == 0 && len(ws.Verifies) == 0 {
		return
	}
	b.WriteString("Progress:\n")
	for _, h := range ws.Services {
		fmt.Fprintf(b, "  - %s: %s\n", h, describeProgressPair(ws.Deploys[h], ws.Verifies[h]))
	}
}

func describeProgressPair(deploys []DeployAttempt, verifies []VerifyAttempt) string {
	deploy := formatDeployStatus(deploys)
	if deploy == "" {
		deploy = "deploy pending"
	} else {
		deploy = "deployed " + deploy
	}
	verify := formatVerifyStatus(verifies)
	if verify == "" {
		verify = "verify pending"
	} else {
		verify = "verified " + verify
	}
	return deploy + ", " + verify
}

// writeStatusNext writes the concrete next action. Four branches:
//
//	auto-closed develop → close + start next
//	develop active      → deploy/verify pending (delegates to SuggestNext)
//	empty project       → start bootstrap
//	idle with services  → start develop, optional bootstrap for more services
func writeStatusNext(
	b *strings.Builder,
	ws *WorkSession,
	scope []string,
	services []platform.ServiceStack,
	metas []*ServiceMeta,
	selfHostname string,
) {
	if ws != nil && ws.ClosedAt != "" && ws.CloseReason == CloseReasonAutoComplete {
		b.WriteString("Next:\n")
		b.WriteString(`  - Close: zerops_workflow action="close" workflow="develop"` + "\n")
		b.WriteString(`  - Start next: zerops_workflow action="start" workflow="develop" intent="..."` + "\n")
		return
	}
	if ws != nil && ws.ClosedAt == "" {
		fmt.Fprintf(b, "Next: %s\n", SuggestNext(ws))
		return
	}

	// Idle branch.
	b.WriteString("Next:\n")
	hasBootstrapped := countBootstrappedMetas(metas, services) > 0
	hasUnmanaged := hasUnmanagedRuntimes(services, metas, selfHostname)

	if len(scope) == 0 {
		b.WriteString(`  - Create services: zerops_workflow action="start" workflow="bootstrap"` + "\n")
		return
	}
	if hasBootstrapped {
		b.WriteString(`  - Start a task: zerops_workflow action="start" workflow="develop" intent="..."` + "\n")
	}
	if hasUnmanaged {
		b.WriteString(`  - Adopt runtimes: zerops_workflow action="start" workflow="develop" intent="..."` + "\n")
	}
	b.WriteString(`  - Add services: zerops_workflow action="start" workflow="bootstrap"` + "\n")
}

// projectScope returns every non-system, non-self hostname from live services.
// Stage hostnames of bootstrapped standard-mode services are included so they
// appear in the listing — the setup line attributes them to their dev pair.
func projectScope(services []platform.ServiceStack, selfHostname string) []string {
	var out []string
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if selfHostname != "" && svc.Name == selfHostname {
			continue
		}
		out = append(out, svc.Name)
	}
	return out
}

func indexServiceTypes(services []platform.ServiceStack) map[string]string {
	m := make(map[string]string, len(services))
	for _, svc := range services {
		m[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	}
	return m
}

func indexServiceStatuses(services []platform.ServiceStack) map[string]string {
	m := make(map[string]string, len(services))
	for _, svc := range services {
		m[svc.Name] = svc.Status
	}
	return m
}

func indexMetas(metas []*ServiceMeta) map[string]*ServiceMeta {
	m := make(map[string]*ServiceMeta, len(metas))
	for _, meta := range metas {
		if meta == nil {
			continue
		}
		m[meta.Hostname] = meta
	}
	return m
}

func countBootstrappedMetas(metas []*ServiceMeta, services []platform.ServiceStack) int {
	live := make(map[string]bool, len(services))
	for _, svc := range services {
		live[svc.Name] = true
	}
	n := 0
	for _, m := range metas {
		if m.IsComplete() && live[m.Hostname] {
			n++
		}
	}
	return n
}

// hasUnmanagedRuntimes returns true when a live runtime service has no
// complete meta and isn't the stage of a bootstrapped pair. These get
// auto-adopted on the next develop start.
func hasUnmanagedRuntimes(services []platform.ServiceStack, metas []*ServiceMeta, selfHostname string) bool {
	metaByHost := indexMetas(metas)
	stageOf := make(map[string]bool)
	for _, m := range metas {
		if m.IsComplete() && m.StageHostname != "" {
			stageOf[m.StageHostname] = true
		}
	}
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if selfHostname != "" && svc.Name == selfHostname {
			continue
		}
		if IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			continue
		}
		if m, ok := metaByHost[svc.Name]; ok && m.IsComplete() {
			continue
		}
		if stageOf[svc.Name] {
			continue
		}
		return true
	}
	return false
}
