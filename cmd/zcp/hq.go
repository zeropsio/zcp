package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// hqTimeout bounds one `zcp hq` run end to end: a member-list read, HQ's
// challenge and credential calls, and the wait for the challenge env to show
// in the project search before it is deleted.
const hqTimeout = 60 * time.Second

// hqCallTimeout bounds each call to HQ.
const hqCallTimeout = 15 * time.Second

const hqUsage = "usage: zcp hq <status|enroll> [--json]"

// The `zcp hq` verbs, as constants: goconst counts the package's other
// "status" verbs (capture, telemetry, mate) with these.
const (
	hqVerbStatus = "status"
	hqVerbEnroll = "enroll"
)

// hqService is what `zcp hq` asks of internal/hq.
type hqService interface {
	Status(ctx context.Context) hq.Status
	Enroll(ctx context.Context) (hq.Result, error)
}

// newHQService builds the enroller from this container's own Zerops key.
// Package-level so tests can stub it.
var newHQService = defaultHQService

func defaultHQService(ctx context.Context) (hqService, error) {
	injectMCPEnvIfUnset(".")
	creds, err := auth.ResolveCredentials()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if info.ProjectID == "" {
		return nil, errors.New("no project: zcp hq runs with a Mate project's key")
	}
	return hq.Enroller{
		Zerops:    client,
		HTTP:      &http.Client{Timeout: hqCallTimeout},
		OrgID:     info.ClientID,
		ProjectID: info.ProjectID,
		Path:      hq.EnrollmentPath(),
	}, nil
}

// runHQCmd implements `zcp hq status [--json]` (exit 0 always; a failed read
// is its error field) and `zcp hq enroll [--json]` (exit 1 unless enrolled).
// Neither ever prints the credential.
func runHQCmd(args []string) int {
	if len(args) == 0 || (args[0] != hqVerbStatus && args[0] != hqVerbEnroll) {
		log.Print(hqUsage)
		return 1
	}
	asJSON := slices.Contains(args[1:], "--json")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, hqTimeout)
	defer cancel()

	svc, err := newHQService(ctx)
	if args[0] == hqVerbStatus {
		status := hq.Status{HQ: hq.Official{Verdict: hq.VerdictUnknown}}
		if err != nil {
			status.Error = err.Error()
		} else {
			status = svc.Status(ctx)
		}
		printHQ(status, asJSON, hqStatusLine(status))
		return 0
	}

	result := hqEnrollResult{}
	if err == nil {
		var enrolled hq.Result
		enrolled, err = svc.Enroll(ctx)
		result.HQ, result.Changed = enrolled.HQ, enrolled.Changed
	}
	if err != nil {
		result.Error = err.Error()
	}
	printHQ(result, asJSON, hqEnrollLine(result))
	if err != nil {
		return 1
	}
	return 0
}

// hqEnrollResult is `zcp hq enroll --json`'s answer.
type hqEnrollResult struct {
	HQ      string `json:"hq,omitempty"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

func printHQ(v any, asJSON bool, line string) {
	if !asJSON {
		fmt.Fprintln(os.Stdout, line)
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("hq: marshal result: %v", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func hqStatusLine(s hq.Status) string {
	switch {
	case s.Error != "":
		return fmt.Sprintf("hq: %s, unknown: %s", hqName(s.HQ), s.Error)
	case s.HQ.Verdict != hq.VerdictOfficial:
		return "hq: " + hqName(s.HQ)
	case s.Enrolled:
		return fmt.Sprintf("hq: %s, enrolled", s.HQ.Address)
	default:
		return fmt.Sprintf("hq: %s, not enrolled", s.HQ.Address)
	}
}

func hqName(o hq.Official) string {
	switch o.Verdict {
	case hq.VerdictOfficial:
		return o.Address
	case hq.VerdictUnclear:
		return "unclear (" + strings.Join(o.ProjectIDs, ", ") + ")"
	case hq.VerdictNone, hq.VerdictUnknown:
		return string(o.Verdict)
	}
	return string(o.Verdict)
}

func hqEnrollLine(r hqEnrollResult) string {
	switch {
	case r.Error != "":
		return "hq: not enrolled: " + r.Error
	case r.Changed:
		return "hq: enrolled with " + r.HQ
	default:
		return "hq: already enrolled with " + r.HQ
	}
}
