package ops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
)

// A relayed `zcp git` call: every git command of an HQ-wired pair runs in
// its dev service with a get-only credential helper that asks zcp over the
// ssh session itself. A Mate checkout is a blob:none partial clone, so any
// subcommand — checkout, show, log -p, worktree add — may fetch contents
// lazily and ask for a credential, from a hook too. The helper writes a
// frame on descriptor 4 (a dup of the session's stderr, so a hook's
// `2>/dev/null` cannot swallow it) and reads its answer on descriptor 3 (the
// session's stdin); git itself reads its stdin from a file the script filled
// before git started, or /dev/null, so descriptor 3 carries answers only.
//
// zcp answers a frame only for a host and path built from zcp-side records
// and the call's own argv — HQ's host from the identity (any path, the Mate
// credential), the record's HQ origin or HQ's live origin of the record's
// repository, and URLs the argv writes literally — never from the dev
// checkout's config, which the dev service writes, and never from the
// record's RemoteURL, written from the checkout's live origin: HQ mints a
// token for any repository the application mounts. An allowed remote is
// answered as often as git asks within the call, from one token per
// (repository, access) per call. GIT_ALLOW_PROTOCOL, the TLS pins and the
// environment scrub are guardrails, not the boundary.

// BrokeredGitHQRepo is a repository in HQ: the application and its name, as
// zcp's record of a wired pair holds them.
type BrokeredGitHQRepo struct {
	AppID string
	Repo  string
}

// BrokeredGitRelay is what a relayed call answers credential requests from,
// every value zcp's own: the record lookups are the caller's.
type BrokeredGitRelay struct {
	// Identity is the Mate's: HQ's origin, answered for any path with the
	// Mate credential in the file it names, and the key HQ mints with.
	Identity hq.Identity
	// HTTPClient reaches HQ.
	HTTPClient hq.Doer
	// Repos are the repositories on GitHub the call answers for: the
	// record's HQ origin (zcp writes it from HQ's own state) and the URLs the
	// call's argv writes literally (BrokeredGitArgvURLs). Never the record's
	// RemoteURL, never anything the checkout's config names.
	Repos []string
	// LiveOrigin is the record's repository when the record names no
	// origin: HQ's live state is asked for its origin once, on the first
	// GitHub request none of Repos matches.
	LiveOrigin *BrokeredGitHQRepo

	// githubOrigin is where the tokens HQ mints are good, https://github.com
	// when empty; a test points it at a stand-in.
	githubOrigin string
	// githubAPI is where the minted tokens are revoked, hq.GitHubAPIOrigin
	// when empty; a test points it at a stand-in.
	githubAPI string
}

func (r BrokeredGitRelay) api() string {
	if r.githubAPI != "" {
		return r.githubAPI
	}
	return hq.GitHubAPIOrigin
}

// gitHubOrigin is the origin GitHub requests are recognized by.
const gitHubOrigin = "https://" + hq.GitHubHost

func (r BrokeredGitRelay) github() string {
	if r.githubOrigin != "" {
		return r.githubOrigin
	}
	return gitHubOrigin
}

// AllowsRepo reports whether repo is one of the relay's repositories:
// compared by repository — scheme, host with its scheme's default port
// dropped, and path, case aside, with no trailing slash or .git — and never
// a remote name or anything else that is no URL.
func (r BrokeredGitRelay) AllowsRepo(repo string) bool {
	key := brokeredGitRepoKey(repo)
	if key == "" {
		return false
	}
	for _, allowed := range r.Repos {
		if brokeredGitRepoKey(allowed) == key {
			return true
		}
	}
	return false
}

// brokeredGitRepoKey is repo as a repository identity (topology.RepoIdentity:
// no credential, query or fragment, canonical suffix) with its scheme and
// host lower-cased and the scheme's default port dropped, and its path
// lower-cased — hosts like GitHub ignore case there; "" for no URL with a
// scheme and a host.
func brokeredGitRepoKey(repo string) string {
	identity := topology.RepoIdentity(repo)
	if identity == "" {
		return ""
	}
	u, err := url.Parse(identity)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme + "://" + brokeredGitHostPort(u.Host, scheme) + strings.ToLower(strings.TrimSuffix(u.Path, "/"))
}

// brokeredGitHostPort is host lower-cased with its scheme's own port
// dropped, so "github.com:443" and "GitHub.com" are one https host.
func brokeredGitHostPort(host, scheme string) string {
	host = strings.ToLower(host)
	switch strings.ToLower(scheme) {
	case httpScheme:
		return strings.TrimSuffix(host, ":80")
	case httpsScheme:
		return strings.TrimSuffix(host, ":443")
	}
	return host
}

// httpScheme is plain http's scheme: an HQ in development speaks it.
const httpScheme = "http"

// sameOrigin reports whether a request's protocol and host are origin's
// scheme and host, the default port aside.
func sameOrigin(protocol, host, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || protocol == "" || host == "" {
		return false
	}
	return strings.EqualFold(protocol, u.Scheme) && brokeredGitHostPort(host, protocol) == brokeredGitHostPort(u.Host, u.Scheme)
}

// BrokeredGitArgvURLs are the URLs the call's argv writes literally — each
// argument after the subcommand that is an https address, or one of HQ's
// own scheme, with a host. A remote name, a path, an scp-style address and
// a leading -c value count for nothing.
func BrokeredGitArgvURLs(args BrokeredGitArgs, hqOrigin string) []string {
	schemes := brokeredGitSchemes(hqOrigin)
	var urls []string
	for _, a := range args.Rest {
		u, err := url.Parse(a)
		if err != nil || u.Host == "" {
			continue
		}
		for _, s := range schemes {
			if strings.EqualFold(u.Scheme, s) && len(a) > len(s)+3 && strings.EqualFold(a[:len(s)+3], s+"://") {
				urls = append(urls, a)
				break
			}
		}
	}
	return urls
}

// brokeredGitSchemes are the schemes a relayed call speaks: https, and HQ's
// own when it is another (an HQ in development).
func brokeredGitSchemes(hqOrigin string) []string {
	schemes := []string{httpsScheme}
	if s := originScheme(hqOrigin); s != "" && s != httpsScheme {
		schemes = append(schemes, s)
	}
	return schemes
}

// originScheme is origin's scheme lower-cased when it is a plain scheme
// name, "" otherwise.
func originScheme(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" {
		return ""
	}
	s := strings.ToLower(u.Scheme)
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return ""
		}
	}
	return s
}

// relayedGitRefusedKeys are the user -c keys (lower-cased) a relayed call
// refuses by prefix, relayedGitRefusedExact those it refuses whole: what
// decides credentials, transports, URLs, remotes, included config,
// upload-pack, aliases, and the commands git runs for transport — a
// guardrail, since core.hooksPath, core.editor, core.pager, diff.external,
// filter.* and gpg.program still run what the agent names.
var (
	relayedGitRefusedKeys = []string{
		"credential.", "http.", "url.", "protocol.", "remote.", "include.",
		"includeif.", "uploadpack.", "alias.",
	}
	relayedGitRefusedExact = []string{
		"core.sshcommand", "core.askpass", "core.gitproxy", "core.fsmonitor",
		"fetch.parallel", "submodule.fetchjobs",
	}
)

// CheckRelayedGitArgs refuses what a relayed call may not carry: git's own
// credential subcommands — zcp answers the call's requests itself — and a
// user -c whose key is one relayedGitRefusedKeys or relayedGitRefusedExact
// names, any case. The refusal names the key, never its value.
func CheckRelayedGitArgs(args BrokeredGitArgs) error {
	if args.Subcommand == "credential" || strings.HasPrefix(args.Subcommand, "credential-") {
		return fmt.Errorf("git %s is refused: zcp answers this call's credential requests itself", args.Subcommand)
	}
	for _, c := range args.Config {
		key, _, _ := strings.Cut(c, "=")
		lower := strings.ToLower(key)
		refused := false
		for _, prefix := range relayedGitRefusedKeys {
			refused = refused || strings.HasPrefix(lower, prefix)
		}
		for _, exact := range relayedGitRefusedExact {
			refused = refused || lower == exact
		}
		if refused {
			return fmt.Errorf("-c %s is refused: zcp decides this call's credentials, transports and remotes", key)
		}
	}
	return nil
}

// previousBranchDash are the subcommands that read a standalone `-` as the
// previous branch (@{-1}), not as stdin.
var previousBranchDash = map[string]bool{
	"checkout": true, "switch": true, "merge": true, "rebase": true,
	"cherry-pick": true, "revert": true, "worktree": true,
}

// patchValueOptions are apply's and am's options that take their value as
// the next argument, so it is no patch file.
var patchValueOptions = map[string]bool{
	"--exclude": true, "--include": true, "--directory": true, "--whitespace": true,
	"--build-fake-ancestor": true, "--patch-format": true, "--resolvemsg": true,
	"--empty": true, "--quoted-cr": true,
}

// amNoPatch are am's options that read no patch: they act on the session
// in progress.
var amNoPatch = map[string]bool{
	"--continue": true, "-r": true, "--resolved": true, "--skip": true, "--abort": true,
	"--quit": true, "--retry": true, "--allow-empty": true, "--show-current-patch": true,
}

// trailerValueOptions are interpret-trailers' options that take their value
// as the next argument.
var trailerValueOptions = map[string]bool{
	"--trailer": true, "--where": true, "--if-exists": true, "--if-missing": true,
}

// BrokeredGitReadsStdin reports whether the call's argv asks git to read its
// stdin, so zcp forwards its own — explicitly (a standalone `-`, --stdin…,
// -F -, --file=-) or by the subcommand's nature (cat-file --batch…,
// commit-tree without -m/-F, apply/am/interpret-trailers without a file,
// mktree, fast-import, patch-id, stripspace, update-index --index-info).
// Anything else reads /dev/null, so an open stdin never holds a call that
// does not read it; a `-` that names the previous branch is no stdin.
func BrokeredGitReadsStdin(args BrokeredGitArgs) bool {
	rest := args.Rest
	for _, a := range rest {
		switch {
		case a == "-" && !previousBranchDash[args.Subcommand]:
			return true
		case strings.HasPrefix(a, "--stdin"), a == "-F-", a == "--file=-":
			return true
		}
	}
	switch args.Subcommand {
	case "mktree", "fast-import", "patch-id", "stripspace":
		return true
	case "cat-file":
		return slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "--batch") })
	case "commit-tree":
		return !slices.ContainsFunc(rest, func(a string) bool { return strings.HasPrefix(a, "-m") || strings.HasPrefix(a, "-F") })
	case "update-index":
		return slices.Contains(rest, "--index-info")
	case "apply":
		return !hasOperand(rest, patchValueOptions)
	case "am":
		return !slices.ContainsFunc(rest, func(a string) bool { return amNoPatch[a] || strings.HasPrefix(a, "--show-current-patch") }) &&
			!hasOperand(rest, patchValueOptions)
	case "interpret-trailers":
		return !hasOperand(rest, trailerValueOptions)
	}
	return false
}

// hasOperand reports whether args hold a non-option argument — a file —
// skipping the next argument after each option valueOptions names.
func hasOperand(args []string, valueOptions map[string]bool) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return i+1 < len(args)
		case valueOptions[a]:
			i++
		case strings.HasPrefix(a, "-") && a != "-":
		default:
			return true
		}
	}
	return false
}

// brokeredGitStdinCap is the most stdin zcp forwards to git.
const brokeredGitStdinCap = 1 << 20

// ErrBrokeredGitStdinOverCap is a call whose stdin is over the 1 MiB zcp
// forwards; ErrBrokeredGitStdinUnread one whose stdin could not be read;
// ErrBrokeredGitStdinStopped one stopped while its stdin was being read.
// Each ends the call before anything runs.
var (
	ErrBrokeredGitStdinOverCap = errors.New("stdin is over the 1 MiB zcp git forwards to git")
	ErrBrokeredGitStdinUnread  = errors.New("read stdin")
	ErrBrokeredGitStdinStopped = errors.New("stopped while reading stdin")
)

// readBrokeredGitStdin reads all of stdin, refusing more than the cap — in
// a goroutine of its own, so ctx stops the call even on a stdin that never
// ends (the read itself is left to the process's end).
func readBrokeredGitStdin(ctx context.Context, stdin io.Reader) ([]byte, error) {
	if stdin == nil {
		return []byte{}, nil
	}
	type read struct {
		payload []byte
		err     error
	}
	done := make(chan read, 1)
	go func() {
		payload, err := io.ReadAll(io.LimitReader(stdin, brokeredGitStdinCap+1))
		done <- read{payload, err}
	}()
	var r read
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", ErrBrokeredGitStdinStopped, ctx.Err())
	case r = <-done:
	}
	if r.err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBrokeredGitStdinUnread, r.err)
	}
	if len(r.payload) > brokeredGitStdinCap {
		return nil, ErrBrokeredGitStdinOverCap
	}
	return r.payload, nil
}

// relayLockBound is how long, by the clock, a helper waits for the call's
// lock before it answers git nothing: longer than zcp's worst-case answer
// (5 s to connect to HQ, 30 s for its mint, 15 s for its live state).
const relayLockBound = 90

// relayLockPath is the call's lock: a helper holds it from writing its
// frame until it read its answer, so concurrent helpers — a hook's git
// processes each fetching lazily — take turns on descriptors 3 and 4.
func relayLockPath(nonce string) string { return "/tmp/zcp-relay-" + nonce + ".lock" }

// relayHelper is the call's credential helper, one line for git's
// `-c credential.helper=` (POSIX sh, get only, GNU and busybox userland):
// it reads git's request, takes the call's lock (giving up relayLockBound
// seconds by the clock after it began), writes its frame to descriptor 4 —
// answering nothing at once when it cannot — and reads descriptor 3 until
// the answer carrying its own id and its host, or `-` for a frame zcp could
// not read, discarding any other (one a helper that gave up left, or a
// reused PID's), then hands git the user and password, or nothing for a
// declined one (`-`). The lock is removed when it ends, also when it is
// signalled (exit 129).
func relayHelper(nonce string) string {
	return "!f() { test \"$1\" = get || return 0; pr=; h=; p=; " +
		"while IFS= read -r l && [ -n \"$l\" ]; do case \"$l\" in protocol=*) pr=${l#protocol=};; host=*) h=${l#host=};; path=*) p=${l#path=};; esac; done; " +
		"L=" + relayLockPath(nonce) + "; end=$(( $(date +%s) + " + fmt.Sprint(relayLockBound) + " )); " +
		"until mkdir \"$L\" 2>/dev/null; do [ \"$(date +%s)\" -lt \"$end\" ] || return 0; sleep 0.1 2>/dev/null || sleep 1; done; " +
		"trap 'rmdir \"$L\" 2>/dev/null' EXIT; trap 'rmdir \"$L\" 2>/dev/null; exit 129' INT TERM HUP; " +
		"printf '\\n" + relayFramePrefix(nonce) + "%s %s %s %s\\n' \"$$\" \"$pr\" \"$h\" \"${p:--}\" >&4 || return 0; " +
		"id=; while IFS=' ' read -r id ah u w <&3; do [ \"$id\" = \"$$\" ] && { [ \"$ah\" = \"$h\" ] || [ \"$ah\" = - ]; } && break; [ \"$(date +%s)\" -lt \"$end\" ] || return 0; done; " +
		"[ \"$id\" = \"$$\" ] && [ \"$ah\" = \"$h\" ] && [ -n \"$u\" ] && [ \"$u\" != - ] || return 0; " +
		"printf 'username=%s\\npassword=%s\\n' \"$u\" \"$w\"; }; f"
}

// relayFramePrefix is how every frame of the call begins on its line.
func relayFramePrefix(nonce string) string { return "ZCPCRED-" + nonce + " " }

// relayStdinStep reads the payload zcp writes first — `<len>\n<bytes>` — on
// descriptor 3 into a file of a fresh directory, all of it before git
// starts, and checks its size; `head -c` where it takes -c (a busybox build
// option), else `dd bs=1`. Neither can read past the payload: zcp writes
// nothing more until a frame arrives, and none can before git starts.
const relayStdinStep = `d=$(mktemp -d /tmp/zcp-stdin.XXXXXX) && f=$d/in && IFS= read -r n <&3 && ` +
	`if head -c 0 </dev/null >/dev/null 2>&1; then head -c "$n" <&3 >"$f"; else dd bs=1 count="$n" <&3 >"$f" 2>/dev/null; fi && ` +
	`n2=$(( $(wc -c < "$f") )) && [ "$n2" -eq "$n" ] || exit 128`

// relayCleanup removes the stdin directory and a lock a helper left.
const relayCleanup = `[ -z "$d" ] || rm -rf "$d"; rmdir "$L" 2>/dev/null`

// brokeredGitAllowProtocol is the call's GIT_ALLOW_PROTOCOL, which overrides
// every protocol.* setting the checkout's config holds: https; file only
// when argv names an absolute .bundle path; HQ's scheme when it is not
// https.
func brokeredGitAllowProtocol(args BrokeredGitArgs, hqOrigin string) string {
	protocols := brokeredGitSchemes(hqOrigin)
	if slices.ContainsFunc(args.Rest, func(a string) bool { return path.IsAbs(a) && strings.HasSuffix(a, ".bundle") }) {
		protocols = append(protocols, "file")
	}
	return strings.Join(protocols, ":")
}

// buildRelayedGitCommand is a relayed call's remote command: /bin/sh runs a
// script that scrubs the environment, keeps the session's stdin as
// descriptor 3 (answers) and its stderr as 4 (frames), removes what it made
// when it ends, reads a forwarded stdin into a file, changes into dir and
// runs git with zcp's helper alone — the empty helper first clears every
// persisted one — the protocol pin and the TLS and proxy pins after the
// user's -c, git's stdin the file or /dev/null. Every value is ShellQuoted.
// Auto-gc and auto-maintenance run in the foreground: detached, they close
// only descriptors 0-2, keep 3 and 4 — the session's — and hold the call
// open until they finish. A passthrough call needs no such setting: its git
// holds only 0-2.
func buildRelayedGitCommand(dir string, args BrokeredGitArgs, nonce string, stdin bool, allowProtocol string) string {
	var b strings.Builder
	b.WriteString("unset " + brokeredGitScrubbedEnv + "; ")
	b.WriteString("exec 3<&0 4>&2; ")
	b.WriteString("L=" + ShellQuote(relayLockPath(nonce)) + "; d=; ")
	b.WriteString("trap " + ShellQuote(relayCleanup) + " EXIT; ")
	b.WriteString("trap " + ShellQuote(relayCleanup+"; exit 129") + " INT TERM HUP; ")
	if stdin {
		b.WriteString(relayStdinStep + "; ")
	}
	b.WriteString("cd " + ShellQuote(dir) + " || exit 128; ")
	b.WriteString("GIT_ALLOW_PROTOCOL=" + ShellQuote(allowProtocol) + " GIT_TERMINAL_PROMPT=0 git")
	argv := make([]string, 0, 2*len(args.Config)+2+14+1+len(args.Rest))
	for _, c := range args.Config {
		argv = append(argv, "-c", c)
	}
	if args.Dir != "" {
		argv = append(argv, "-C", args.Dir)
	}
	argv = append(argv,
		"-c", "credential.helper=",
		"-c", "credential.helper="+relayHelper(nonce),
		"-c", "credential.useHttpPath=true",
		"-c", "fetch.parallel=1",
		"-c", "submodule.fetchJobs=1",
		"-c", "http.sslVerify=true",
		"-c", "http.proxy=",
		"-c", "gc.autoDetach=false",
		"-c", "maintenance.autoDetach=false",
		args.Subcommand)
	argv = append(argv, args.Rest...)
	for _, a := range argv {
		b.WriteByte(' ')
		b.WriteString(ShellQuote(a))
	}
	if stdin {
		b.WriteString(` < "$f"`)
	} else {
		b.WriteString(" < /dev/null")
	}
	b.WriteString("; exit $?")
	return "/bin/sh -c " + ShellQuote(b.String())
}

// relayLineCap bounds a frame line zcp parses: a longer line is relayed as
// git's output, never parsed.
const relayLineCap = 64 << 10

// relayDrainBound is how long zcp keeps reading the session's stderr once
// ssh ended.
const relayDrainBound = 2 * time.Second

// relayFrames copies the ssh stderr r to stderr line by line as it arrives,
// taking out every frame of nonce — a line that begins with its prefix after
// at most one carriage return — and writing to answers exactly one answer
// line per frame with a readable id (answerFrame). The empty line directly
// before a frame, the frame's own leading newline, is dropped; every other
// byte reaches stderr unchanged: split reads, carriage-return progress, a
// trailing partial line, and a line longer than relayLineCap.
func relayFrames(r io.Reader, nonce string, stderr, answers io.Writer, answer func(protocol, host, path string) (string, string)) {
	s := frameStream{prefix: []byte(relayFramePrefix(nonce)), stderr: stderr, onFrame: func(frame string) {
		if line, ok := answerFrame(frame, answer); ok {
			_, _ = io.WriteString(answers, line)
		}
	}}
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		s.write(buf[:n])
		if err != nil {
			break
		}
	}
	s.flush()
}

// frameStream modes: at a line's start (deciding whether it is a frame),
// passing a line through, or collecting a frame.
const (
	frameModeStart = iota
	frameModePass
	frameModeFrame
)

// frameStream is relayFrames' line state.
type frameStream struct {
	prefix  []byte
	stderr  io.Writer
	onFrame func(frame string)

	mode      int
	line      []byte // the current line while it may be, or is, a frame
	heldEmpty bool   // an empty line not yet relayed: a frame may follow it
}

func (s *frameStream) relay(p []byte) {
	if len(p) > 0 {
		_, _ = s.stderr.Write(p)
	}
}

// release relays a held empty line and the bytes collected so far.
func (s *frameStream) release() {
	if s.heldEmpty {
		s.relay([]byte{'\n'})
		s.heldEmpty = false
	}
	s.relay(s.line)
	s.line = s.line[:0]
}

func (s *frameStream) write(p []byte) {
	for len(p) > 0 {
		switch s.mode {
		case frameModePass:
			i := bytes.IndexByte(p, '\n')
			if i < 0 {
				s.relay(p)
				return
			}
			s.relay(p[:i+1])
			p = p[i+1:]
			s.mode = frameModeStart
		case frameModeFrame:
			i := bytes.IndexByte(p, '\n')
			take := p
			if i >= 0 {
				take = p[:i]
			}
			if len(s.line)+len(take) > relayLineCap {
				s.declineOverlong(take)
				s.release()
				s.mode = frameModePass
				continue
			}
			s.line = append(s.line, take...)
			if i < 0 {
				return
			}
			p = p[i+1:]
			start := len(s.prefix)
			if s.line[0] == '\r' {
				start++
			}
			frame := s.line[start:]
			s.heldEmpty = false
			s.onFrame(string(frame))
			s.line = s.line[:0]
			s.mode = frameModeStart
		default:
			c := p[0]
			p = p[1:]
			if len(s.line) == 0 && c == '\n' {
				if s.heldEmpty {
					s.relay([]byte{'\n'})
				}
				s.heldEmpty = true
				continue
			}
			s.line = append(s.line, c)
			body := s.line
			if body[0] == '\r' {
				body = body[1:]
			}
			switch {
			case len(body) > len(s.prefix) || !bytes.HasPrefix(s.prefix, body):
				s.release()
				if c == '\n' {
					s.mode = frameModeStart
				} else {
					s.mode = frameModePass
				}
			case len(body) == len(s.prefix):
				s.mode = frameModeFrame
			}
		}
	}
}

// declineOverlong answers a frame grown past relayLineCap — collected so
// far, then next — with a decline for its id when the id is readable: its
// helper waits for an answer with that id, and the line itself is relayed,
// never parsed.
func (s *frameStream) declineOverlong(next []byte) {
	start := len(s.prefix)
	if s.line[0] == '\r' {
		start++
	}
	head := slices.Clone(s.line[start:])
	if len(head) < 16 {
		head = append(head, next[:min(len(next), 16-len(head))]...)
	}
	if id, _, ok := bytes.Cut(head, []byte{' '}); ok && readableFrameID(string(id)) {
		s.onFrame(string(id))
	}
}

// flush relays what is held at the end of the stream, unchanged.
func (s *frameStream) flush() { s.release() }

// answerFrame is the one answer line for frame — the line after the prefix:
// `<id> <protocol> <host> <path…>` — and whether it has one: `<id> <host>
// <user> <password>` for an allowed request, `<id> <host> - -` for a
// declined one or an answer git's credential protocol cannot carry, `<id>
// - - -` for a malformed frame (an empty protocol or host, an unreadable
// host, a missing field); none when the id is unreadable, as no helper can
// be named. The path is the rest of the line and may hold spaces; `-` is
// none.
func answerFrame(frame string, answer func(protocol, host, path string) (string, string)) (string, bool) {
	id, rest, _ := strings.Cut(frame, " ")
	if !readableFrameID(id) {
		return "", false
	}
	protocol, rest, ok1 := strings.Cut(rest, " ")
	host, path, ok2 := strings.Cut(rest, " ")
	if !ok1 || !ok2 || protocol == "" || !readableFrameHost(host) {
		return id + " - - -\n", true
	}
	user, password := answer(protocol, host, path)
	if !credentialToken(user) || !credentialToken(password) || user == "-" {
		return id + " " + host + " - -\n", true
	}
	return id + " " + host + " " + user + " " + password + "\n", true
}

// readableFrameID is a helper's PID: one to ten digits.
func readableFrameID(id string) bool {
	if id == "" || len(id) > 10 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// readableFrameHost is a host[:port] git can name: letters, digits, dots,
// hyphens, underscores, a port's colon, an IPv6 literal's brackets.
func readableFrameHost(host string) bool {
	if host == "" {
		return false
	}
	for _, c := range host {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == '_', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}

// credentialToken is a user or password git's credential protocol and the
// helper's line can carry: non-empty, with no space or control byte.
func credentialToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] <= ' ' || s[i] == 0x7f {
			return false
		}
	}
	return true
}

// relayCall is one relayed call's answering: its caches live as long as
// the call, and everything it says goes to the caller's stderr once.
type relayCall struct {
	relay  BrokeredGitRelay
	host   string
	stderr io.Writer
	// access is what every GitHub token the call obtains carries, decided
	// once from its argv (brokeredGitAccess).
	access string

	answers  map[string]relayAnswer
	said     map[string]bool
	liveRead bool
	// minted are the GitHub tokens the call obtained, each once, to be
	// revoked when it ends.
	minted []relayMinted
}

// relayAnswer is a cached answer: a credential, or a decline.
type relayAnswer struct{ user, password string }

// relayMinted is a GitHub token a call obtained, and the repository it was
// minted for (host/path, as git named it) — what a failed revoke names.
type relayMinted struct{ repo, token string }

func newRelayCall(relay BrokeredGitRelay, host, access string, stderr io.Writer) *relayCall {
	return &relayCall{relay: relay, host: host, access: access, stderr: stderr, answers: map[string]relayAnswer{}, said: map[string]bool{}}
}

// say tells the caller msg once per call.
func (c *relayCall) say(msg string) {
	if c.said[msg] {
		return
	}
	c.said[msg] = true
	fmt.Fprintf(c.stderr, "zcp git: %s\n", msg)
}

// notRecorded says a remote is none the records name.
func (c *relayCall) notRecorded(host, path string) {
	remote := host
	if path != "-" {
		remote += "/" + path
	}
	c.say(fmt.Sprintf("%s is not a remote zcp records for %s", remote, c.host))
}

// answer answers one request: HQ by its scheme and host, any path, with the
// Mate credential (read once per call); GitHub for a repository the relay
// allows, with the token HQ mints for it (once per call, reused); anything
// else declined, said once.
func (c *relayCall) answer(ctx context.Context, protocol, host, path string) (string, string) {
	switch {
	case sameOrigin(protocol, host, c.relay.Identity.Origin):
		return c.cached(ctx, "hq", fmt.Sprintf("protocol=%s\nhost=%s\n", protocol, host), "")
	case sameOrigin(protocol, host, c.relay.github()):
		if path == "-" {
			c.say(fmt.Sprintf("git named no repository on %s, and zcp git answers %s only for a repository it names", host, host))
			return "", ""
		}
		repo := strings.TrimRight(c.relay.github(), "/") + "/" + path
		if !c.allows(ctx, repo) {
			c.notRecorded(host, path)
			return "", ""
		}
		return c.cached(ctx, brokeredGitRepoKey(repo)+" "+c.access, "protocol="+httpsScheme+"\nhost="+hq.GitHubHost+"\npath="+path+"\n", host+"/"+path)
	}
	c.notRecorded(host, path)
	return "", ""
}

// allows reports whether repo is the relay's, asking HQ's live state for
// the record's origin once when none of Repos matches.
func (c *relayCall) allows(ctx context.Context, repo string) bool {
	if c.relay.AllowsRepo(repo) {
		return true
	}
	if c.relay.LiveOrigin == nil || c.liveRead {
		return false
	}
	c.liveRead = true
	if origin := c.liveOrigin(ctx); origin != "" {
		c.relay.Repos = append(slices.Clone(c.relay.Repos), origin)
		return c.relay.AllowsRepo(repo)
	}
	return false
}

// relayLiveConnectBound and relayLiveAnswerBound bound the read of HQ's
// live state.
const (
	relayLiveConnectBound = 5 * time.Second
	relayLiveAnswerBound  = 15 * time.Second
)

// liveOrigin is the origin HQ's live state names for the record's
// repository, "" when it names none or cannot be read (said once).
func (c *relayCall) liveOrigin(ctx context.Context) string {
	repo := c.relay.LiveOrigin
	client, err := hq.Open(c.relay.HTTPClient, c.relay.Identity)
	if err != nil {
		c.say(fmt.Sprintf("HQ's origin of %s is unknown: %v", repo.Repo, err))
		return ""
	}
	state, err := client.Once().Bounded(relayLiveConnectBound, relayLiveAnswerBound).Self(ctx)
	if err != nil {
		c.say(fmt.Sprintf("HQ's origin of %s is unknown: %v", repo.Repo, err))
		return ""
	}
	if state.AppID == nil || *state.AppID != repo.AppID {
		return ""
	}
	for _, r := range state.Repos {
		if r.Name == repo.Repo && r.Origin != nil {
			return r.Origin.URL
		}
	}
	return ""
}

// cached answers request (git's credential attributes, as hq.AnswerGit
// reads them) once per key per call, a decline included, saying a
// declined one's reason once. A token obtained for a GitHub repository
// (minted names it, "" for HQ) is kept to be revoked when the call ends.
func (c *relayCall) cached(ctx context.Context, key, request, minted string) (string, string) {
	if a, ok := c.answers[key]; ok {
		return a.user, a.password
	}
	got, err := hq.AnswerGitFor(ctx, strings.NewReader(request+"\n"), c.relay.Identity, c.relay.HTTPClient, c.access)
	var a relayAnswer
	if err == nil {
		a = relayAnswer{user: got.Username, password: got.Password}
		if minted != "" && got.Password != "" {
			c.minted = append(c.minted, relayMinted{repo: minted, token: got.Password})
		}
	} else {
		var declined *hq.DeclinedError
		if errors.As(err, &declined) && declined.Reason != "" {
			c.say(declined.Reason)
		}
	}
	c.answers[key] = a
	return a.user, a.password
}

// relayRevokeBound bounds each token's revoke.
const relayRevokeBound = 5 * time.Second

// revoke revokes each GitHub token the call minted, once, when the call has
// ended — on a context detached from the call's, which may already be
// cancelled, each bounded relayRevokeBound. Best effort: the token expires
// within the hour regardless, so a failure is one stderr line naming the
// repository, never the token, and changes nothing else.
func (c *relayCall) revoke(ctx context.Context) {
	for _, m := range c.minted {
		revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), relayRevokeBound)
		err := hq.RevokeGitHubToken(revokeCtx, c.relay.HTTPClient, c.relay.api(), m.token)
		cancel()
		if err != nil {
			fmt.Fprintf(c.stderr, "zcp git: the GitHub token minted for %s in this call was not revoked; it expires by itself within the hour: %v\n", m.repo, err)
		}
	}
	c.minted = nil
}

// relayNonce is a call's 16 random hex digits.
func relayNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("relay nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// lockedWriter serialises writes to the session's stdin: the forwarded
// stdin, then answers.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("answer: %w", err)
	}
	return n, nil
}

// RunBrokeredGitRelayed runs git with args in host's checkout at dir over
// ssh like RunBrokeredGit, with zcp answering git's credential requests
// over the session from relay: git's stdout and the rest of its stderr pass
// through, frames never do, and the exit code is git's. The caller's stdin
// is read — whole, at most 1 MiB, else ErrBrokeredGitStdinOverCap before
// anything runs — and forwarded only when the argv asks git to read it
// (BrokeredGitReadsStdin). No credential is printed, put in argv, or kept
// past the call: each GitHub token it minted is revoked when it ends, also
// when ctx stopped it. Every GitHub token carries only what the call needs
// (brokeredGitAccess). The caller refuses what CheckRelayedGitArgs refuses
// first.
func RunBrokeredGitRelayed(ctx context.Context, ssh SSHStdioRunner, host, dir string, args BrokeredGitArgs, relay BrokeredGitRelay, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if relay.HTTPClient == nil {
		relay.HTTPClient = &http.Client{}
	}
	forward := BrokeredGitReadsStdin(args)
	var payload []byte
	if forward {
		var err error
		if payload, err = readBrokeredGitStdin(ctx, stdin); err != nil {
			return -1, err
		}
	}
	access := brokeredGitAccess(ctx, ssh, host, dir, args, relay)
	nonce, err := relayNonce()
	if err != nil {
		return -1, err
	}
	command := buildRelayedGitCommand(dir, args, nonce, forward, brokeredGitAllowProtocol(args, relay.Identity.Origin))

	answersR, answersW, err := os.Pipe()
	if err != nil {
		return -1, fmt.Errorf("relay pipe: %w", err)
	}
	framesR, framesW, err := os.Pipe()
	if err != nil {
		_ = answersR.Close()
		_ = answersW.Close()
		return -1, fmt.Errorf("relay pipe: %w", err)
	}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	answers := lockedWriter{mu: &mu, w: answersW}
	var wg sync.WaitGroup
	if forward {
		// One write, so nothing can come between the length and the bytes.
		wg.Go(func() { _, _ = answers.Write(append(fmt.Appendf(nil, "%d\n", len(payload)), payload...)) })
	}
	call := newRelayCall(relay, host, access, stderr)
	relayed := make(chan struct{})
	go func() {
		defer close(relayed)
		relayFrames(framesR, nonce, stderr, answers, func(protocol, host, path string) (string, string) {
			return call.answer(callCtx, protocol, host, path)
		})
	}()

	exit, runErr := ssh.ExecSSHStdio(ctx, host, command, answersR, stdout, framesW)
	_ = framesW.Close()
	_ = answersR.Close()
	select {
	case <-relayed:
	case <-time.After(relayDrainBound):
		cancel()
		_ = framesR.Close()
		<-relayed
	}
	cancel()
	_ = answersW.Close()
	wg.Wait()
	_ = framesR.Close()
	call.revoke(ctx)

	if runErr != nil {
		return exit, fmt.Errorf("run git over ssh: %w", runErr)
	}
	if exit == sshTransportExit {
		return exit, fmt.Errorf("ssh to %s failed (exit %d): the message above is ssh's own, not git's answer", host, exit)
	}
	return exit, nil
}

// brokeredGitAccess is the access every GitHub token of the call carries,
// decided once from its argv: read for any call that is no push — the lazy
// fetches inside it included; for a push, write, or workflows when a commit
// it would send touches .github/workflows/ or what it sends cannot be
// resolved (brokeredGitPushAccess). A push a hook or alias starts inside a
// call that is no push gets read, and GitHub refuses it.
func brokeredGitAccess(ctx context.Context, ssh SSHStdioRunner, host, dir string, args BrokeredGitArgs, relay BrokeredGitRelay) string {
	if args.Subcommand != "push" {
		return hq.GitHubAccessRead
	}
	return brokeredGitPushAccess(ctx, ssh, host, dir, args, relay)
}

// brokeredGitPushPlan is what a push's argv says it sends: the repository
// it names ("" for the default) and each refspec's source (none for a push
// naming no refspec), or unresolvable when zcp cannot tell the commits it
// sends.
type brokeredGitPushPlan struct {
	remote       string
	srcs         []string
	unresolvable bool
}

// pushUnresolvableFlags are push options whose sent commits zcp does not
// resolve: every branch or ref, tags, a force, a deletion.
var pushUnresolvableFlags = map[string]bool{
	"--all": true, "--branches": true, "--mirror": true, "--tags": true, "--follow-tags": true,
	"--force": true, "--force-if-includes": true, "--delete": true, "--prune": true, "--repo": true,
}

// pushValueOptions are push options whose value is the next argument.
var pushValueOptions = map[string]bool{"--push-option": true, "--receive-pack": true, "--exec": true}

// planBrokeredGitPush reads a push's argv (`git push [<options>]
// [<repository> [<refspec>...]]`): a forced refspec (`+…`), a deletion (an
// empty source), a pattern, a negative refspec, the `tag <name>` form, a
// source that reads as an option, and the options pushUnresolvableFlags
// names — short ones in a cluster too — are unresolvable.
func planBrokeredGitPush(args BrokeredGitArgs) brokeredGitPushPlan {
	unresolvable := brokeredGitPushPlan{unresolvable: true}
	var positional []string
	rest := args.Rest
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		long, _, _ := strings.Cut(a, "=")
		switch {
		case a == "--":
			positional = append(positional, rest[i+1:]...)
			i = len(rest)
		case pushUnresolvableFlags[long], strings.HasPrefix(a, "--force-with-lease"):
			return unresolvable
		case pushValueOptions[a]:
			i++
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'f', 'd':
					return unresolvable
				case 'o':
					if j == len(a)-1 {
						i++
					}
					j = len(a)
				}
			}
		default:
			positional = append(positional, a)
		}
	}
	var plan brokeredGitPushPlan
	if len(positional) == 0 {
		return plan
	}
	plan.remote = positional[0]
	for _, spec := range positional[1:] {
		src, _, _ := strings.Cut(spec, ":")
		if spec == "tag" || src == "" || strings.HasPrefix(spec, "+") || strings.HasPrefix(spec, "^") ||
			strings.HasPrefix(src, "-") || strings.Contains(spec, "*") {
			return unresolvable
		}
		plan.srcs = append(plan.srcs, src)
	}
	return plan
}

// localMainRef is local main, which zcp keeps on HQ's main.
const localMainRef = "refs/heads/main"

// pushCheckCommitCap is how many commits the workflows check reads at
// most: more is unresolvable.
const pushCheckCommitCap = 1000

// brokeredGitPushAccess is a push's access: write, or workflows when a
// commit the push would send touches .github/workflows/ or what it sends
// cannot be resolved — never less than today's token. The check runs before
// the push as relayed read calls in the same checkout, with the push's -C
// and -c: push.followTags set is unresolvable; a push naming no refspec
// sends the current branch to @{push}; the commits the remote lacks are
// `rev-list <src>… --not --remotes=<remote>`, or `--not refs/heads/main`
// when the remote has no tracking refs at all (zcp keeps local main on HQ's
// main), else every commit of the sources; more than 1000 is unresolvable;
// each is read by `diff-tree --root -m --no-commit-id -r --name-only
// --no-renames -- .github/workflows/` — commits and trees only, which a
// blob:none clone holds. Any check that fails is unresolvable.
func brokeredGitPushAccess(ctx context.Context, ssh SSHStdioRunner, host, dir string, args BrokeredGitArgs, relay BrokeredGitRelay) string {
	plan := planBrokeredGitPush(args)
	if plan.unresolvable {
		return hq.GitHubAccessWorkflows
	}
	check := func(stdin string, gitArgs ...string) (string, int, bool) {
		var stdout bytes.Buffer
		code, err := RunBrokeredGitRelayed(ctx, ssh, host, dir, BrokeredGitArgs{
			Config: args.Config, Dir: args.Dir, Subcommand: gitArgs[0], Rest: gitArgs[1:],
		}, relay, strings.NewReader(stdin), &stdout, io.Discard)
		return stdout.String(), code, err == nil
	}
	out, code, ok := check("", "config", "--bool", "--get", "push.followTags")
	if followTags, _ := strconv.ParseBool(strings.TrimSpace(out)); !ok || code > 1 || followTags {
		return hq.GitHubAccessWorkflows
	}
	srcs, remote := plan.srcs, plan.remote
	if len(srcs) == 0 {
		out, code, ok := check("", "rev-parse", "--symbolic-full-name", "HEAD", "@{push}")
		lines := strings.Fields(out)
		if !ok || code != 0 || len(lines) != 2 || !strings.HasPrefix(lines[0], "refs/heads/") || !strings.HasPrefix(lines[1], "refs/remotes/") {
			return hq.GitHubAccessWorkflows
		}
		srcs = []string{lines[0]}
		if remote == "" {
			remote, _, _ = strings.Cut(strings.TrimPrefix(lines[1], "refs/remotes/"), "/")
		}
	}
	patterns := []string{localMainRef}
	named := remote != "" && !strings.ContainsAny(remote, "/:*?[\\") && !strings.HasPrefix(remote, "-")
	if named {
		patterns = append(patterns, "refs/remotes/"+remote+"/")
	}
	out, code, ok = check("", append([]string{"for-each-ref", "--format=%(refname)"}, patterns...)...)
	if !ok || code != 0 {
		return hq.GitHubAccessWorkflows
	}
	var tracking, localMain bool
	for ref := range strings.FieldsSeq(out) {
		tracking = tracking || named && strings.HasPrefix(ref, "refs/remotes/"+remote+"/")
		localMain = localMain || ref == localMainRef
	}
	revList := append([]string{"rev-list", fmt.Sprintf("--max-count=%d", pushCheckCommitCap+1)}, srcs...)
	switch {
	case tracking:
		revList = append(revList, "--not", "--remotes="+remote)
	case localMain:
		revList = append(revList, "--not", localMainRef)
	}
	out, code, ok = check("", revList...)
	commits := strings.Fields(out)
	if !ok || code != 0 || len(commits) > pushCheckCommitCap {
		return hq.GitHubAccessWorkflows
	}
	if len(commits) == 0 {
		return hq.GitHubAccessWrite
	}
	out, code, ok = check(strings.Join(commits, "\n")+"\n",
		"diff-tree", "--stdin", "--root", "-m", "--no-commit-id", "-r", "--name-only", "--no-renames", "--", ".github/workflows/")
	if !ok || code != 0 || strings.TrimSpace(out) != "" {
		return hq.GitHubAccessWorkflows
	}
	return hq.GitHubAccessWrite
}
