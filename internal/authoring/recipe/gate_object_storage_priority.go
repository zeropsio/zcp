package recipe

import "fmt"

func gateRequireObjectStoragePriority(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	siblingsHavePriority := false
	for _, svc := range ctx.Plan.Services {
		if svc.Kind == ServiceKindManaged && svc.Priority > 0 {
			siblingsHavePriority = true
			break
		}
	}
	if !siblingsHavePriority {
		return nil
	}
	var out []Violation
	for _, svc := range ctx.Plan.Services {
		if svc.Type != "object-storage" {
			continue
		}
		if svc.Priority > 0 {
			continue
		}
		out = append(out, Violation{
			Code:     "object-storage-missing-priority",
			Path:     fmt.Sprintf("services[%s]", svc.Hostname),
			Severity: SeverityBlocking,
			Message: fmt.Sprintf(
				"object-storage service %q has no priority while sibling managed services declare priority > 0. Object-storage is a boot-order peer of managed databases — the runtime depends on it. Set `Priority: 10` on the service entry (mirrors db/cache/broker/search); rerun `complete-phase phase=env-content`.",
				svc.Hostname,
			),
		})
	}
	return out
}
