// Command dcseed is a THROWAWAY seeder: it fills every managed service in the
// adopted project with a little sample data so the Data Console shows something
// in each. It reuses the Data Console's verified connection resolution
// (zcpadapter) — same credentials path as `zcp studio console serve` — then
// hands off to console/seed (the island-safe fixture library) for the actual
// per-engine writes, in static mode (Options{} — the unprefixed, human-
// browsable dataset). Idempotent + resilient (per-service log+continue). Run
// it FROM the zcp container (internal network reaches every service).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/seed"
	"github.com/zeropsio/zcp/internal/dataconsole/zcpadapter"
	"github.com/zeropsio/zcp/internal/platform"
)

func main() {
	ctx := context.Background()
	creds, err := auth.ResolveCredentials()
	must("resolve creds", err)
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	must("client", err)
	info, err := auth.Resolve(ctx, client)
	must("auth resolve", err)
	ad := zcpadapter.New(client, info)
	svcs, err := ad.ManagedServices(ctx)
	must("list managed", err)

	fmt.Printf("project=%s services=%d\n", info.ProjectName, len(svcs))
	for _, s := range svcs {
		ci, err := ad.ConnectionInfo(ctx, s.ID)
		if err != nil {
			fmt.Printf("[%s] connInfo: %v\n", s.Hostname, err)
			continue
		}
		fmt.Printf("--- %s (%s) ---\n", s.Hostname, ci.Type)
		if os.Getenv("DCSEED_DEBUG") != "" {
			fmt.Printf("    descriptor %s\n", redactedDescriptor(ci.Descriptor))
		}
		if err := seed.Service(ctx, ci.Type, ci.Descriptor, seed.Options{}); err != nil {
			fmt.Printf("[%s] seed: %v\n", s.Hostname, err)
		} else {
			fmt.Printf("[%s] seeded ✓\n", s.Hostname)
		}
	}
}

func redactedDescriptor(desc provider.ConnectionDescriptor) string {
	if desc == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T family=%s", desc, desc.ConnectionFamily())
}

func must(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		os.Exit(1)
	}
}
