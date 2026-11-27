package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
)

// runPreviewCmd is the Mate's on-demand Draft opening request.
func runPreviewCmd(args []string) int {
	flags := flag.NewFlagSet("preview", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var req tools.MatePreviewRequest
	flags.StringVar(&req.Lease, "lease", "", "")
	flags.StringVar(&req.AcquiredAt, "acquired-at", "", "")
	flags.StringVar(&req.Service, "service", "", "")
	flags.StringVar(&req.Branch, "branch", "", "")
	asJSON := flags.Bool("json", false, "")
	fail := func(reason string) int {
		whereJSON(tools.MatePreviewAnswer{Status: "failed", Reason: reason})
		return 1
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !*asJSON {
		return fail("invalid draft request")
	}
	rt := runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath))
	if !rt.InContainer || !rt.MateEnabled {
		return fail("drafts are unavailable in this container")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, devsup.ListenWait)
	defer cancel()
	d, err := resolveLiveLeaseDeps(ctx)
	if err != nil {
		return fail("could not connect to the draft's project")
	}
	answer := tools.EnsureMatePreview(ctx, d, &http.Client{Timeout: ops.LeaseCallTimeout}, req)
	whereJSON(answer)
	if answer.Status != "running" {
		return 1
	}
	return 0
}
