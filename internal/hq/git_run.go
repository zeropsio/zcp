package hq

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
)

// RunGit runs the caller's Git command in its current directory, passing
// streams and arguments through. HQ's project authorization is scoped to its
// origin in the child's environment; no credential is written to Git config.
// Git's exit code is returned unchanged, with setup failures returning 1.
func RunGit(ctx context.Context, id Identity, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	safeOut := &gitSecretWriter{dst: stdout, secret: []byte(id.Key)}
	safeErr := &gitSecretWriter{dst: stderr, secret: []byte(id.Key)}
	defer safeOut.flush()
	defer safeErr.flush()
	project, err := OpenProject(http.DefaultClient, id)
	if err != nil {
		_, _ = fmt.Fprintf(safeErr, "zcp hq git: %v\n", err)
		return 1
	}
	gitArgs := append([]string{"-c", "http.followRedirects=false", "-c", "core.askPass="}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = append(project.GitEnv(), "LC_ALL=C", "GIT_ASKPASS=", "SSH_ASKPASS=")
	var failure bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, safeOut, io.MultiWriter(safeErr, &failure)
	if err := cmd.Run(); err != nil {
		// Git does not expose the response body and may report only the
		// origin. An authentication failure at this HQ asks Self for the
		// verdict on the same key; its client owns the one recheck when HQ
		// confirms key_refused. Git itself is never repeated.
		if remote := gitAuthenticationFailureURL(failure.String()); sameGitOrigin(remote, id.Origin) {
			if _, probeErr := project.Self(ctx); probeErr != nil {
				var refused *RefusedError
				if errors.As(probeErr, &refused) {
					_, _ = fmt.Fprintf(safeErr, "zcp hq git: %v\n", probeErr)
				}
			}
		}
		var exited *exec.ExitError
		if errors.As(err, &exited) && exited.ExitCode() >= 0 {
			return exited.ExitCode()
		}
		_, _ = fmt.Fprintf(safeErr, "zcp hq git: %v\n", err)
		return 1
	}
	return 0
}

// gitSecretWriter keeps at most a key-length suffix between writes so even
// a key split across chunks cannot escape through Git's output. This covers
// commands such as git config --list that print the transient extraHeader.
type gitSecretWriter struct {
	dst     io.Writer
	secret  []byte
	pending []byte
}

func (w *gitSecretWriter) Write(p []byte) (int, error) {
	if len(w.secret) == 0 {
		return w.dst.Write(p)
	}
	w.pending = append(w.pending, p...)
	data := w.pending
	w.pending = nil
	for {
		index := bytes.Index(data, w.secret)
		if index < 0 {
			keep := min(len(w.secret)-1, len(data))
			if _, err := w.dst.Write(data[:len(data)-keep]); err != nil {
				return 0, err
			}
			w.pending = bytes.Clone(data[len(data)-keep:])
			return len(p), nil
		}
		if _, err := w.dst.Write(data[:index]); err != nil {
			return 0, err
		}
		if _, err := io.WriteString(w.dst, "<redacted>"); err != nil {
			return 0, err
		}
		data = data[index+len(w.secret):]
	}
}

func (w *gitSecretWriter) flush() {
	_, _ = w.dst.Write(w.pending)
	w.pending = nil
}

// gitAuthenticationFailureURL reads only Git's C-locale HTTP authentication
// failures. Other failures carry no trustworthy failed remote to probe.
func gitAuthenticationFailureURL(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if remote, ok := strings.CutPrefix(line, "fatal: Authentication failed for '"); ok {
			if remote, ok = strings.CutSuffix(remote, "'"); ok {
				return strings.TrimRight(remote, "/")
			}
		}
		if remote, ok := strings.CutPrefix(line, "fatal: unable to access '"); ok {
			if remote, ok = strings.CutSuffix(remote, "': The requested URL returned error: 401"); ok {
				return strings.TrimRight(remote, "/")
			}
		}
		for _, kind := range []string{"Username", "Password"} {
			if remote, ok := strings.CutPrefix(line, "fatal: could not read "+kind+" for '"); ok {
				if remote, ok = strings.CutSuffix(remote, "': terminal prompts disabled"); ok {
					return strings.TrimRight(remote, "/")
				}
			}
		}
	}
	return ""
}

func sameGitOrigin(remote, origin string) bool {
	target, err := url.Parse(remote)
	if err != nil || target.User != nil || target.Host == "" {
		return false
	}
	hq, err := url.Parse(origin)
	return err == nil && target.Scheme == hq.Scheme && target.Host == hq.Host
}
