//go:build linux

package eval

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// observeProcessIdentity implements the Linux reader for §10.4 "Observed
// process identity": it lists mcp/zcp-<pid>.jsonl files written by the
// candidate's `serve` process under the owned capture window, and for each
// pid not already in seen reads /proc/<pid>/environ to decide whether the
// observation counts (carries this window's ZCP_CAPTURE_SESSION_ID) and, if
// so, hashes /proc/<pid>/exe and reads projectId/serviceId/ZCP_AUTO_UPDATE.
func observeProcessIdentity(_ context.Context, sessionDir, windowID, candidateSHA256 string, seen map[int]bool) []ProcessIdentity {
	mcpDir := filepath.Join(sessionDir, "mcp")
	entries, err := os.ReadDir(mcpDir)
	if err != nil {
		return nil
	}
	var out []ProcessIdentity
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "zcp-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		pidStr := strings.TrimSuffix(strings.TrimPrefix(name, "zcp-"), ".jsonl")
		pid, err := strconv.Atoi(pidStr)
		if err != nil || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, observeOneProcess(pid, windowID, candidateSHA256))
	}
	return out
}

func observeOneProcess(pid int, windowID, candidateSHA256 string) ProcessIdentity {
	rec := ProcessIdentity{PID: pid, ObservedAt: time.Now().UTC()}
	environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		rec.Classification = processIdentityUnobserved
		return rec
	}
	env := parseProcEnviron(environ)
	if env["ZCP_CAPTURE_SESSION_ID"] != windowID {
		rec.Classification = processIdentityUnobserved
		return rec
	}
	digest, err := sha256File(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		rec.Classification = processIdentityUnobserved
		return rec
	}
	rec.Digest = digest
	rec.MatchesCandidate = digest == candidateSHA256
	rec.ProjectID = env["projectId"]
	rec.ServiceID = env["serviceId"]
	rec.AutoUpdate = env["ZCP_AUTO_UPDATE"]
	rec.Classification = processIdentityCounted
	return rec
}

func parseProcEnviron(data []byte) map[string]string {
	env := make(map[string]string)
	for _, kv := range bytes.Split(data, []byte{0}) {
		if len(kv) == 0 {
			continue
		}
		parts := bytes.SplitN(kv, []byte{'='}, 2)
		if len(parts) != 2 {
			continue
		}
		env[string(parts[0])] = string(parts[1])
	}
	return env
}
