package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
)

// hqTimeout bounds one `zcp hq status` run end to end.
const hqTimeout = 30 * time.Second

// hqCallTimeout bounds each call to HQ.
const hqCallTimeout = 15 * time.Second

const hqUsage = "usage: zcp hq status [--json]"

const hqVerbStatus = "status"

// hqService is what `zcp hq status` asks of internal/hq.
type hqService func(context.Context) error

// newHQService opens HQ under this container's project key, as the live env
// store holds it now. Package-level so tests can stub it.
var newHQService = defaultHQService

func defaultHQService(context.Context) (hqService, string, error) {
	injectMCPEnvIfUnset(".")
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	id := mate.HQIdentity(lookup)
	httpClient := &http.Client{Timeout: hqCallTimeout}
	if !runtime.DetectFrom(lookup).MateEnabled {
		client, err := hq.OpenProject(httpClient, id)
		if err != nil {
			return nil, id.Origin, fmt.Errorf("open HQ status: %w", err)
		}
		return func(ctx context.Context) error { _, err := client.Self(ctx); return err }, id.Origin, nil
	}
	client, err := hq.Open(httpClient, id)
	if err != nil {
		return nil, id.Origin, fmt.Errorf("open HQ status: %w", err)
	}
	return func(ctx context.Context) error { _, err := client.Once().Self(ctx); return err }, id.Origin, nil
}

// hqStatus is `zcp hq status --json`'s answer: whether HQ answers for this
// container's key. Never the key.
type hqStatus struct {
	HQ     string `json:"hq,omitempty"`
	Linked bool   `json:"linked"`
	Error  string `json:"error,omitempty"`
}

// runHQCmd implements `zcp hq status [--json]`: exit 0 always, a failed read
// is its error field.
func runHQCmd(args []string) int {
	if len(args) == 0 || args[0] != hqVerbStatus {
		log.Print(hqUsage)
		return 1
	}
	asJSON := slices.Contains(args[1:], "--json")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, hqTimeout)
	defer cancel()

	var status hqStatus
	svc, origin, err := newHQService(ctx)
	status.HQ = origin
	if err == nil {
		err = svc(ctx)
	}
	if err != nil {
		status.Error = err.Error()
	} else {
		status.Linked = true
	}
	printHQ(status, asJSON, hqStatusLine(status))
	return 0
}

// isHQGitCredential is `zcp hq git-credential`, which run answers before the
// CLI's telemetry: git runs the helper on every fetch and push to HQ or
// GitHub from the Mate's shell (ops.hqCredentialHelperShell) and reads its
// whole stdout as credential attributes — the telemetry's one-time notice on
// stdout, or its flush, would break or hold every one of them.
func isHQGitCredential(args []string) bool {
	return len(args) >= 2 && args[0] == "hq" && args[1] == "git-credential"
}

// runHQGitCredential answers git's credential protocol: `get` prints what
// hq.AnswerGit answers for the request on stdin — the Git credential the
// file at path holds under HQ's git user for the HQ the identity names, the token HQ mints
// at this call for a repository on GitHub — and declines with nothing on
// stdout and exit 1 otherwise, saying why on stderr when there is a reason a
// person can act on; `store` and `erase` keep nothing — the link and HQ are the
// store.
func runHQGitCredential(args []string, stdin io.Reader, stdout, stderr io.Writer, id hq.Identity, httpClient hq.Doer) int {
	if len(args) == 0 {
		return 1
	}
	if args[0] != "get" {
		return 0
	}
	answer, err := hq.AnswerGit(context.Background(), stdin, id, httpClient)
	if err != nil {
		var declined *hq.DeclinedError
		if errors.As(err, &declined) && declined.Reason != "" {
			_, _ = fmt.Fprintln(stderr, "zcp hq git-credential: "+declined.Reason)
		}
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "username=%s\npassword=%s\n", answer.Username, answer.Password)
	return 0
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

func hqStatusLine(s hqStatus) string {
	if s.Error != "" {
		return "hq: not linked: " + s.Error
	}
	return "hq: linked"
}
