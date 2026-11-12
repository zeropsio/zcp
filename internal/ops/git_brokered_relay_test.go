// Tests for: ops/git_brokered_relay.go — a relayed `zcp git` call answers
// git's credential requests over the ssh session itself, from zcp-side
// records and the call's own argv only.
//
// Unit tests drive the pure pieces (frames, the stdin readers, the refused
// arguments). Offline tests run the real remote script through a runner that
// stands in for ssh: the script runs locally under /bin/sh, and again under
// dash and busybox's sh where installed (an alpine dev image's /bin/sh is
// busybox ash), against git's own http-backend over net/http/cgi behind
// Basic auth — a fake HQ (its git under /git/, the Mate API minting tokens)
// and a GitHub stand-in serving repositories behind the tokens HQ minted.
package ops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: git names objects by SHA-1
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: the fakes serve git's own http-backend through CGI, in a test
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/zeropsio/zcp/internal/hq"
)

// --- unit: frames on the ssh stderr ----------------------------------------

const frameTestNonce = "0123456789abcdef"

// framePrefix is how every frame of frameTestNonce starts.
const framePrefix = "ZCPCRED-" + frameTestNonce + " "

// relayFrames is relayStream for a stream read without a done marker's
// receiver: a marker still ends the answering.
func relayFrames(r io.Reader, nonce string, stderr, answers io.Writer, answer func(protocol, host, path string) (string, string)) {
	relayStream(r, nonce, stderr, answers, answer, nil)
}

// relayFramesOf runs relayFrames over input read through wrap (nil: whole),
// answering with answer; it returns what reached stderr and the answers.
func relayFramesOf(input string, wrap func(io.Reader) io.Reader, answer func(protocol, host, path string) (string, string)) (relayed, answers string) {
	var r io.Reader = strings.NewReader(input)
	if wrap != nil {
		r = wrap(r)
	}
	var stderr, out bytes.Buffer
	relayFrames(r, frameTestNonce, &stderr, &out, answer)
	return stderr.String(), out.String()
}

func declineAll(string, string, string) (string, string) { return "", "" }

// TestBrokeredGitFrames_OnlyAtLineStartAfterOptionalCR: a frame is a line
// that starts with the call's prefix, after at most one carriage return;
// the same text anywhere else on a line is git's output, relayed as it is.
func TestBrokeredGitFrames_OnlyAtLineStartAfterOptionalCR(t *testing.T) {
	t.Parallel()
	input := "before\n" +
		framePrefix + "11 https hq.example git/a.git\n" +
		"\r" + framePrefix + "12 https hq.example -\n" +
		"mid " + framePrefix + "13 https hq.example x\n" +
		" " + framePrefix + "14 https hq.example x\n" +
		"\r\r" + framePrefix + "15 https hq.example x\n" +
		"after\n"
	relayed, answers := relayFramesOf(input, nil, declineAll)
	wantRelayed := "before\n" +
		"mid " + framePrefix + "13 https hq.example x\n" +
		" " + framePrefix + "14 https hq.example x\n" +
		"\r\r" + framePrefix + "15 https hq.example x\n" +
		"after\n"
	if relayed != wantRelayed {
		t.Errorf("relayed = %q\nwant      %q", relayed, wantRelayed)
	}
	if want := "11 hq.example - -\n12 hq.example - -\n"; answers != want {
		t.Errorf("answers = %q, want %q", answers, want)
	}
}

// TestBrokeredGitFrames_SplitReadsAndProgress_ParsedNeverRelayed: however
// the stream is split into reads, git's progress (carriage returns, no
// newline) and a trailing partial line reach stderr unchanged, and every
// frame is answered and never relayed.
func TestBrokeredGitFrames_SplitReadsAndProgress_ParsedNeverRelayed(t *testing.T) {
	t.Parallel()
	input := "Receiving objects:  10% (1/10)\rReceiving objects: 100% (10/10), done.\n" +
		"\n" + framePrefix + "41 https hq.example git/a.git\n" +
		"Resolving deltas:  50% (1/2)\r" +
		"\n" + framePrefix + "42 https hq.example git/b.git\n" +
		"remote: Counting objects\r\n" +
		"a partial tail with no newline"
	want := "Receiving objects:  10% (1/10)\rReceiving objects: 100% (10/10), done.\n" +
		"Resolving deltas:  50% (1/2)\r\n" +
		"remote: Counting objects\r\n" +
		"a partial tail with no newline"
	for name, wrap := range map[string]func(io.Reader) io.Reader{
		"whole":    nil,
		"one byte": iotest.OneByteReader,
		"halves":   iotest.HalfReader,
	} {
		relayed, answers := relayFramesOf(input, wrap, declineAll)
		if relayed != want {
			t.Errorf("%s: relayed = %q\nwant %q", name, relayed, want)
		}
		if strings.Contains(relayed, "ZCPCRED") {
			t.Errorf("%s: a frame reached stderr: %q", name, relayed)
		}
		if wantAnswers := "41 hq.example - -\n42 hq.example - -\n"; answers != wantAnswers {
			t.Errorf("%s: answers = %q, want %q", name, answers, wantAnswers)
		}
	}
}

// TestBrokeredGitFrames_WrongNonce_RelayedAsText: a line carrying another
// call's nonce — or none — is git's output, relayed and never answered.
func TestBrokeredGitFrames_WrongNonce_RelayedAsText(t *testing.T) {
	t.Parallel()
	input := "ZCPCRED-fedcba9876543210 51 https hq.example x\n" +
		"ZCPCRED- 52 https hq.example x\n" +
		"ZCPCRED-" + frameTestNonce + "53 https hq.example x\n"
	relayed, answers := relayFramesOf(input, nil, declineAll)
	if relayed != input {
		t.Errorf("relayed = %q, want every line as it is %q", relayed, input)
	}
	if answers != "" {
		t.Errorf("answers = %q, want none", answers)
	}
}

// TestBrokeredGitFrames_OverlongLine_RelayedNotParsed: a line longer than
// 64 KiB is relayed as it is and never parsed, even one starting with the
// prefix; the frames around it are still answered.
func TestBrokeredGitFrames_OverlongLine_RelayedNotParsed(t *testing.T) {
	t.Parallel()
	long := framePrefix + "61 https hq.example " + strings.Repeat("a", 70<<10) + "\n"
	noID := framePrefix + "x9 https hq.example " + strings.Repeat("b", 70<<10) + "\n"
	input := framePrefix + "60 https hq.example x\n" + long + noID + framePrefix + "62 https hq.example y\n"
	for name, wrap := range map[string]func(io.Reader) io.Reader{"whole": nil, "halves": iotest.HalfReader, "one byte": iotest.OneByteReader} {
		relayed, answers := relayFramesOf(input, wrap, func(string, string, string) (string, string) { return "mate", "never-FAKE" })
		if relayed != long+noID {
			t.Errorf("%s: relayed %d bytes, want the overlong lines alone (%d bytes)", name, len(relayed), len(long+noID))
		}
		// The helper behind an overlong frame waits for an answer with its
		// id: it gets a decline, never a credential.
		if want := "60 hq.example mate never-FAKE\n61 - - -\n62 hq.example mate never-FAKE\n"; answers != want {
			t.Errorf("%s: answers = %q, want %q", name, answers, want)
		}
	}
}

// TestBrokeredGitFrames_EveryFrameWithReadableId_GetsExactlyOneAnswer: the
// helper's read of its answer has no timeout, so every frame line with a
// readable id gets exactly one answer line — an allowed one the credential, a
// declined one `<id> <host> - -`, a malformed one `<id> - - -` — and the
// path is the rest of the line, spaces included. A frame without a readable
// id has no one to answer.
func TestBrokeredGitFrames_EveryFrameWithReadableId_GetsExactlyOneAnswer(t *testing.T) {
	t.Parallel()
	answer := func(protocol, host, path string) (string, string) {
		switch {
		case protocol == httpsScheme && host == "hq.example" && path == "a b/c d.git":
			return "mate", "spaced-path-FAKE"
		case protocol == httpsScheme && host == "hq.example" && path == "git/space.git":
			return "mate", "pass word"
		case protocol == httpsScheme && host == "hq.example":
			return "mate", "s3cret-FAKE"
		}
		return "", ""
	}
	for _, tc := range []struct {
		name, frame, want string
	}{
		{"well-formed, allowed", "21 https hq.example git/a.git", "21 hq.example mate s3cret-FAKE\n"},
		{"well-formed, declined", "22 https other.example x", "22 other.example - -\n"},
		{"no path", "20 https hq.example -", "20 hq.example mate s3cret-FAKE\n"},
		{"empty protocol", "23  hq.example x", "23 - - -\n"},
		{"empty host", "24 https  x", "24 - - -\n"},
		{"path with spaces", "25 https hq.example a b/c d.git", "25 hq.example mate spaced-path-FAKE\n"},
		{"host missing", "26 https", "26 - - -\n"},
		{"id alone", "27", "27 - - -\n"},
		{"unreadable host", "28 https hq!example x", "28 - - -\n"},
		{"path field missing", "29 https hq.example", "29 - - -\n"},
		{"answer holding a space", "30 https hq.example git/space.git", "30 hq.example - -\n"},
		{"host with a port", "31 https hq.example:8443 x", "31 hq.example:8443 - -\n"},
		{"unreadable id", "x9 https hq.example x", ""},
		{"empty id", " https hq.example x", ""},
		{"overlong id", "123456789012 https hq.example x", ""},
	} {
		relayed, answers := relayFramesOf(framePrefix+tc.frame+"\n", nil, answer)
		if answers != tc.want {
			t.Errorf("%s: answers = %q, want %q", tc.name, answers, tc.want)
		}
		if relayed != "" {
			t.Errorf("%s: relayed %q, want a frame never relayed", tc.name, relayed)
		}
	}
	// Many frames in one stream: one answer line each, in order.
	var input strings.Builder
	for i := range 50 {
		fmt.Fprintf(&input, "%s%d https hq.example git/%d.git\n", framePrefix, 100+i, i)
		fmt.Fprintf(&input, "%s%d  hq.example x\n", framePrefix, 200+i)
	}
	_, answers := relayFramesOf(input.String(), iotest.OneByteReader, answer)
	lines := strings.Split(strings.TrimSuffix(answers, "\n"), "\n")
	if len(lines) != 100 {
		t.Fatalf("answers = %d lines, want 100 (one per frame)", len(lines))
	}
	for i := range 50 {
		if want := fmt.Sprintf("%d hq.example mate s3cret-FAKE", 100+i); lines[2*i] != want {
			t.Errorf("answer %d = %q, want %q", 2*i, lines[2*i], want)
		}
		if want := fmt.Sprintf("%d - - -", 200+i); lines[2*i+1] != want {
			t.Errorf("answer %d = %q, want %q", 2*i+1, lines[2*i+1], want)
		}
	}
}

// TestBrokeredGitFrames_EmptyLineBeforeFrame_Dropped: the helper writes a
// newline before its frame so the frame starts a line; that empty line,
// directly before a frame, is dropped — every other empty line is git's.
func TestBrokeredGitFrames_EmptyLineBeforeFrame_Dropped(t *testing.T) {
	t.Parallel()
	input := "a\n\n" + framePrefix + "31 https hq.example x\n" +
		"\n\n" + framePrefix + "32 https hq.example x\n" +
		"\n\nb\n\n"
	want := "a\n" + "\n" + "\n\nb\n\n"
	for name, wrap := range map[string]func(io.Reader) io.Reader{"whole": nil, "one byte": iotest.OneByteReader} {
		relayed, answers := relayFramesOf(input, wrap, declineAll)
		if relayed != want {
			t.Errorf("%s: relayed = %q, want %q", name, relayed, want)
		}
		if answers != "31 hq.example - -\n32 hq.example - -\n" {
			t.Errorf("%s: answers = %q", name, answers)
		}
	}
}

// TestBrokeredGitDone_MarkerEndsAnsweringAndCarriesTheCode: the script's
// `ZCPDONE-<nonce> <exit>` line, once git ended, is taken out of the
// stream and handed on with git's exit code, once; a frame after it gets
// no answer; a marker with another nonce or none is git's output; one with
// this nonce but no exit code is dropped and ends nothing.
func TestBrokeredGitDone_MarkerEndsAnsweringAndCarriesTheCode(t *testing.T) {
	t.Parallel()
	done := "ZCPDONE-" + frameTestNonce + " "
	input := framePrefix + "71 https hq.example x\n" +
		"ZCPDONE-fedcba9876543210 0\n" +
		"ZCPDONE- 0\n" +
		done + "abc\n" +
		done + "999\n" +
		"\n" + done + "7\n" +
		framePrefix + "72 https hq.example x\n" +
		done + "0\n" +
		"after\n"
	for name, wrap := range map[string]func(io.Reader) io.Reader{"whole": nil, "one byte": iotest.OneByteReader} {
		var r io.Reader = strings.NewReader(input)
		if wrap != nil {
			r = wrap(r)
		}
		var stderr, answers bytes.Buffer
		var codes []int
		relayStream(r, frameTestNonce, &stderr, &answers, func(string, string, string) (string, string) { return "mate", "s3cret-FAKE" }, func(code int) { codes = append(codes, code) })
		if want := "71 hq.example mate s3cret-FAKE\n"; answers.String() != want {
			t.Errorf("%s: answers = %q, want only the frame before the marker answered %q", name, answers.String(), want)
		}
		if !slices.Equal(codes, []int{7}) {
			t.Errorf("%s: done with %v, want once with git's exit code 7", name, codes)
		}
		if want := "ZCPDONE-fedcba9876543210 0\nZCPDONE- 0\nafter\n"; stderr.String() != want {
			t.Errorf("%s: relayed %q, want %q", name, stderr.String(), want)
		}
	}
}

// relayReceiptBuffer reports when a sentry following the input reached the
// caller. The test reads the buffer only after the copy has finished.
type relayReceiptBuffer struct {
	bytes.Buffer
	sentry   string
	received chan struct{}
	once     sync.Once
}

func (b *relayReceiptBuffer) Write(p []byte) (int, error) {
	n, err := b.Buffer.Write(p)
	if strings.Contains(b.String(), b.sentry) {
		b.once.Do(func() { close(b.received) })
	}
	return n, err
}

// TestRelayStdout_EndMark_TakenOutWhereverReadsSplitIt: the script's end mark
// is taken out of stdout however reads split it, ended closes once every
// byte before it was passed on, and what follows it is dropped; another
// nonce's mark, or a tail that only begins the mark when the stream ends, is
// git's output, passed on unchanged.
func TestRelayStdout_EndMark_TakenOutWhereverReadsSplitIt(t *testing.T) {
	t.Parallel()
	mark := relayEndMark(frameTestNonce)
	for _, tc := range []struct {
		name, input, want string
		ended             bool
	}{
		{"mark after output", "git output\nno newline at the end" + mark + "late-from-a-holder\n", "git output\nno newline at the end", true},
		{"another nonce's mark", "a ZCPEND-fedcba9876543210 b\n", "a ZCPEND-fedcba9876543210 b\n", false},
		{"only the start of a mark at the end", "output ZCPEND-0123", "output ZCPEND-0123", false},
		{"mark alone", mark, "", true},
	} {
		for split := 1; split < len(tc.input); split++ {
			const sentry = "!zcp-test-receipt!\n"
			dst := &relayReceiptBuffer{sentry: sentry, received: make(chan struct{})}
			s, err := newRelayStdout(dst, frameTestNonce)
			if err != nil {
				t.Fatal(err)
			}
			go s.copy()
			if _, err := s.w.WriteString(tc.input[:split]); err != nil {
				t.Fatal(err)
			}
			// Do not write the second part until the pipe has given the first
			// to copy; two writes alone can be combined in a single read.
			for s.read.Load() == 0 {
				runtime.Gosched()
			}
			rest := tc.input[split:]
			if !tc.ended {
				rest += sentry
			}
			if _, err := s.w.WriteString(rest); err != nil {
				t.Fatal(err)
			}
			ended := false
			if tc.ended {
				<-s.ended
				ended = true
			} else {
				// Either the sentry reached the caller or the stream ended
				// prematurely; both are observable while the pipe stays open.
				select {
				case <-s.ended:
					ended = true
				case <-dst.received:
				}
			}
			_ = s.w.Close()
			<-s.done
			want := tc.want
			if !tc.ended {
				want += sentry
			}
			if dst.String() != want || ended != tc.ended {
				t.Errorf("%s, split at %d: passed %q, ended before EOF %v; want %q, %v", tc.name, split, dst.String(), ended, want, tc.ended)
			}
			if _, err := s.r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
				t.Errorf("%s, split at %d: a read of the pipe once the copy ended = %v, want it closed (no descriptor left open per call)", tc.name, split, err)
			}
		}
		if !tc.ended {
			// Without a sentry, EOF itself must pass a held partial mark.
			var dst bytes.Buffer
			s, err := newRelayStdout(&dst, frameTestNonce)
			if err != nil {
				t.Fatal(err)
			}
			go s.copy()
			if _, err := s.w.WriteString(tc.input); err != nil {
				t.Fatal(err)
			}
			_ = s.w.Close()
			<-s.done
			if dst.String() != tc.want {
				t.Errorf("%s at EOF: passed %q, want %q", tc.name, dst.String(), tc.want)
			}
		}
	}
}

// --- unit: what a relayed call refuses and forwards -------------------------

// TestBrokeredGit_StdinReaders_ExplicitAndImplicitForwarded: zcp forwards its
// stdin only when the argv asks git to read it — explicitly (`-`, --stdin…,
// -F -) or by the subcommand's nature — so an open stdin never holds a call
// that does not read it.
func TestBrokeredGit_StdinReaders_ExplicitAndImplicitForwarded(t *testing.T) {
	t.Parallel()
	forward := [][]string{
		{"hash-object", "-w", "-"},
		{"apply", "-"},
		{"update-ref", "--stdin"},
		{"check-ignore", "--stdin"},
		{"hash-object", "--stdin-paths"},
		{"pack-objects", "--stdin-packs", "out"},
		{"commit", "-F", "-"},
		{"commit", "-F-"},
		{"commit", "--file=-"},
		{"commit", "--file", "-"},
		{"tag", "-a", "v1", "-F", "-"},
		{"cat-file", "--batch"},
		{"cat-file", "--batch-check=%(objectname)"},
		{"cat-file", "--batch-command"},
		{"commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904"},
		{"commit-tree", "-p", "HEAD", "4b825dc642cb6eb9a060e54bf8d69288fbee4904"},
		{"apply"},
		{"apply", "--check", "--index"},
		{"am"},
		{"am", "-3"},
		{"hash-object", "--stdin"},
		{"mktree"},
		{"fast-import"},
		{"interpret-trailers"},
		{"interpret-trailers", "--trailer", "Signed-off-by: A <a@example.invalid>"},
		{"patch-id"},
		{"stripspace"},
		{"update-index", "--index-info"},
	}
	for _, rest := range forward {
		args, err := ParseBrokeredGitArgs(rest)
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(%q): %v", rest, err)
		}
		if !BrokeredGitReadsStdin(args) {
			t.Errorf("BrokeredGitReadsStdin(%q) = false, want stdin forwarded", rest)
		}
	}
	keep := [][]string{
		{"commit", "-m", "x"},
		{"status"},
		{"commit", "-m", "-j"},
		{"commit", "-F", "msg.txt"},
		{"cat-file", "-p", "HEAD"},
		{"commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-m", "msg"},
		{"commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-F", "msg.txt"},
		{"apply", "fix.patch"},
		{"apply", "--exclude", "vendor/*", "fix.patch"},
		{"am", "0001-fix.patch"},
		{"am", "--continue"},
		{"am", "--abort"},
		{"interpret-trailers", "msg.txt"},
		{"update-index", "--refresh"},
		{"push", "origin", "HEAD"},
		{"checkout", "-"},
		{"switch", "-"},
		{"merge", "-"},
		{"log", "-p"},
	}
	for _, rest := range keep {
		args, err := ParseBrokeredGitArgs(rest)
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(%q): %v", rest, err)
		}
		if BrokeredGitReadsStdin(args) {
			t.Errorf("BrokeredGitReadsStdin(%q) = true, want git's stdin /dev/null", rest)
		}
	}
}

// TestBrokeredGit_StdinReaders_PlumbingStdinPathsAndSeparatedValues: the
// plumbing that reads stdin by its nature — mktag, pack-objects,
// unpack-objects, show-index and mailinfo always, fmt-merge-msg without
// -F/--file, mailsplit without an mbox — a file option or operand naming
// stdin's own path (/dev/stdin, /dev/fd/0), and an apply or am whose only
// non-option argument is the separated value of -p or -C, all read stdin.
func TestBrokeredGit_StdinReaders_PlumbingStdinPathsAndSeparatedValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		rest []string
		want bool
	}{
		{[]string{"mktag"}, true},
		{[]string{"mktag", "--strict"}, true},
		{[]string{"pack-objects", "--stdout"}, true},
		{[]string{"pack-objects", "--revs", "/tmp/pack"}, true},
		{[]string{"unpack-objects", "-q"}, true},
		{[]string{"show-index"}, true},
		{[]string{"mailinfo", "/tmp/msg", "/tmp/patch"}, true},
		{[]string{"fmt-merge-msg"}, true},
		{[]string{"fmt-merge-msg", "-m", "merge"}, true},
		{[]string{"fmt-merge-msg", "-F", "/tmp/merged"}, false},
		{[]string{"fmt-merge-msg", "--file=/tmp/merged"}, false},
		{[]string{"fmt-merge-msg", "--file", "/tmp/merged"}, false},
		{[]string{"mailsplit", "-o/tmp/out"}, true},
		{[]string{"mailsplit", "-o/tmp/out", "/tmp/mbox"}, false},
		{[]string{"commit", "-F", "/dev/stdin"}, true},
		{[]string{"commit", "-F/dev/stdin"}, true},
		{[]string{"commit", "--file=/dev/stdin"}, true},
		{[]string{"tag", "-a", "v1", "--file", "/dev/fd/0"}, true},
		{[]string{"commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-F", "/dev/stdin"}, true},
		{[]string{"apply", "/dev/stdin"}, true},
		{[]string{"commit", "-F", "/tmp/dev/stdin-notes"}, false},
		{[]string{"apply", "-p", "1"}, true},
		{[]string{"apply", "-C", "3", "--check"}, true},
		{[]string{"am", "-p", "1"}, true},
		{[]string{"am", "-C", "3"}, true},
		{[]string{"apply", "-p", "1", "fix.patch"}, false},
		{[]string{"am", "-C", "3", "0001-fix.patch"}, false},
		{[]string{"apply", "-p1", "fix.patch"}, false},
	} {
		args, err := ParseBrokeredGitArgs(tc.rest)
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(%q): %v", tc.rest, err)
		}
		if got := BrokeredGitReadsStdin(args); got != tc.want {
			t.Errorf("BrokeredGitReadsStdin(%q) = %v, want %v", tc.rest, got, tc.want)
		}
	}
}

// TestBrokeredGit_UserConfig_SecurityKeysRefused: on a relayed call a user
// -c may not set anything that decides credentials, transports, URLs,
// remotes, included config, upload-pack, aliases or command hooks git runs
// for transport — any case. A guardrail, not the boundary.
func TestBrokeredGit_UserConfig_SecurityKeysRefused(t *testing.T) {
	t.Parallel()
	refused := []string{
		"credential.helper=!evil", "Credential.https://github.com.helper=x", "credential.useHttpPath=false",
		"http.extraHeader=Authorization: x", "HTTP.sslVerify=false", "http.https://github.com.proxy=x",
		"url.https://evil.example/.insteadOf=https://github.com/", "protocol.ext.allow=always",
		"remote.origin.url=https://evil.example/x", "include.path=/tmp/x", "includeIf.gitdir:/var/.path=/tmp/x",
		"uploadpack.packObjectsHook=x", "alias.st=!sh", "ALIAS.x=status",
		"core.sshCommand=sh", "core.askPass=x", "core.gitProxy=x", "core.fsmonitor=x",
		"fetch.parallel=8", "submodule.fetchJobs=8", "credential.helper",
	}
	for _, c := range refused {
		args, err := ParseBrokeredGitArgs([]string{"-c", c, "status"})
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(-c %q): %v", c, err)
		}
		err = CheckRelayedGitArgs(args)
		if err == nil {
			t.Errorf("CheckRelayedGitArgs(-c %q) = nil, want refused", c)
			continue
		}
		key, value, _ := strings.Cut(c, "=")
		if !strings.Contains(err.Error(), key) {
			t.Errorf("CheckRelayedGitArgs(-c %q) = %q, want it to name the key", c, err)
		}
		if value != "" && strings.Contains(err.Error(), "="+value) {
			t.Errorf("CheckRelayedGitArgs(-c %q) = %q, must not echo the value", c, err)
		}
	}
	accepted := []string{
		"user.name=A B", "user.email=a@example.invalid", "core.hooksPath=/dev/null", "core.editor=true",
		"commit.gpgSign=false", "color.ui=never", "core.pager=cat", "advice.detachedHead=false",
	}
	for _, c := range accepted {
		args, err := ParseBrokeredGitArgs([]string{"-c", c, "-C", "/tmp/wt-b", "log", "-j", "--jobs=4"})
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(-c %q): %v", c, err)
		}
		if err := CheckRelayedGitArgs(args); err != nil {
			t.Errorf("CheckRelayedGitArgs(-c %q) = %v, want accepted", c, err)
		}
	}
}

// TestBrokeredGit_CredentialSubcommand_Refused: zcp answers a relayed call's
// credential requests itself, so git's own credential subcommands are refused.
func TestBrokeredGit_CredentialSubcommand_Refused(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"credential", "credential-store", "credential-cache", "credential-osxkeychain", "credential-cache--daemon"} {
		args, err := ParseBrokeredGitArgs([]string{sub, "get"})
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(%q): %v", sub, err)
		}
		if err := CheckRelayedGitArgs(args); err == nil || !strings.Contains(err.Error(), sub) {
			t.Errorf("CheckRelayedGitArgs(%s) = %v, want refused naming it", sub, err)
		}
	}
	for _, sub := range []string{"status", "fetch", "push", "credentials", "log"} {
		args, err := ParseBrokeredGitArgs([]string{sub})
		if err != nil {
			t.Fatalf("ParseBrokeredGitArgs(%q): %v", sub, err)
		}
		if err := CheckRelayedGitArgs(args); err != nil {
			t.Errorf("CheckRelayedGitArgs(%s) = %v, want accepted", sub, err)
		}
	}
}

// TestBrokeredGit_ArgvURLs_LiteralURLsOnly: the URLs a call's argv writes
// literally — https, or HQ's own scheme — count; a remote name, a path, and
// an address of any other scheme do not.
func TestBrokeredGit_ArgvURLs_LiteralURLsOnly(t *testing.T) {
	t.Parallel()
	args, err := ParseBrokeredGitArgs([]string{"-c", "user.name=https://not.argv/x", "fetch",
		"upstream", "https://github.com/acme/lib.git", "main",
		"http://hq.local:8080/git/o/a/x.git", "ssh://github.com/acme/ssh.git", "git@github.com:acme/scp.git",
		"/tmp/x.bundle", "HTTPS://GitHub.com/Acme/Upper"})
	if err != nil {
		t.Fatal(err)
	}
	got := BrokeredGitArgvURLs(args, "http://hq.local:8080")
	want := []string{"https://github.com/acme/lib.git", "http://hq.local:8080/git/o/a/x.git", "HTTPS://GitHub.com/Acme/Upper"}
	if !slices.Equal(got, want) {
		t.Errorf("BrokeredGitArgvURLs = %q, want %q", got, want)
	}
	if got := BrokeredGitArgvURLs(args, "https://hq.example"); slices.Contains(got, "http://hq.local:8080/git/o/a/x.git") {
		t.Errorf("BrokeredGitArgvURLs with an https HQ = %q, want no http address", got)
	}
}

// TestBrokeredGit_ArgvURLs_OnlyTransportPositionals: a URL — or an absolute
// .bundle path, which allows the file transport — counts only as a
// positional argument of fetch, pull, push, ls-remote or clone; never in a
// commit message, a grep, a config value, or as an option's value.
func TestBrokeredGit_ArgvURLs_OnlyTransportPositionals(t *testing.T) {
	t.Parallel()
	const u = "https://github.com/acme/lib"
	for _, tc := range []struct {
		argv []string
		urls []string
		file bool
	}{
		{[]string{"commit", "-m", u}, nil, false},
		{[]string{"log", "--grep", u}, nil, false},
		{[]string{"config", "remote.x.url", u}, nil, false},
		{[]string{"tag", "-m", "/tmp/x.bundle", "v1"}, nil, false},
		{[]string{"fetch", "-o", u, "origin"}, nil, false},
		{[]string{"fetch", "--upload-pack", u, "--depth", "1", u, "main"}, []string{u}, false},
		{[]string{"fetch", "--", u}, []string{u}, false},
		{[]string{"pull", "--rebase", u, "main"}, []string{u}, false},
		{[]string{"push", "-u", u, "HEAD:main"}, []string{u}, false},
		{[]string{"push", "--push-option", u, "origin"}, nil, false},
		{[]string{"ls-remote", "--sort", "version:refname", u}, []string{u}, false},
		{[]string{"clone", "-b", "main", "--origin", "up", u, "/tmp/wt-x"}, []string{u}, false},
		{[]string{"fetch", "/tmp/x.bundle", "main:refs/heads/b"}, nil, true},
		{[]string{"ls-remote", "/tmp/x.bundle"}, nil, true},
		{[]string{"fetch", "--refmap", "/tmp/x.bundle", "origin"}, nil, false},
	} {
		args, err := ParseBrokeredGitArgs(tc.argv)
		if err != nil {
			t.Fatal(err)
		}
		if got := BrokeredGitArgvURLs(args, "https://hq.example"); !slices.Equal(got, tc.urls) {
			t.Errorf("BrokeredGitArgvURLs(%q) = %q, want %q", tc.argv, got, tc.urls)
		}
		if got := strings.Contains(brokeredGitAllowProtocol(args, "https://hq.example"), "file"); got != tc.file {
			t.Errorf("file transport for %q = %v, want %v", tc.argv, got, tc.file)
		}
	}
}

// --- offline: the real script, real git, git's http-backend -----------------

// relayShells are the shells the remote script runs under: /bin/sh, and dash
// and busybox's sh where installed. Each is the login shell too.
func relayShells() [][]string {
	shells := [][]string{{"/bin/sh"}}
	binSh, _ := filepath.EvalSymlinks("/bin/sh")
	if p, err := exec.LookPath("dash"); err == nil {
		if resolved, _ := filepath.EvalSymlinks(p); resolved != binSh {
			shells = append(shells, []string{p})
		}
	}
	if p, err := exec.LookPath("busybox"); err == nil {
		shells = append(shells, []string{p, "sh"})
	}
	return shells
}

func shellName(sh []string) string { return strings.Join(sh, "_") }

// relayShellsFor is relayShells for an offline test, which each runs as its
// own parallel subtest; it skips where git is not installed.
func relayShellsFor(t *testing.T) [][]string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	return relayShells()
}

// shellRunner stands in for ssh: it runs the remote command in the login
// shell sh, the command's own `/bin/sh -c` turned into sh too, with env.
type shellRunner struct {
	sh  []string
	env []string

	mu       sync.Mutex
	commands []string
}

func (r *shellRunner) ExecSSHStdio(ctx context.Context, _, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	r.mu.Lock()
	r.commands = append(r.commands, command)
	r.mu.Unlock()
	line := command
	if r.sh[0] != "/bin/sh" {
		rest, ok := strings.CutPrefix(command, "/bin/sh -c ")
		if !ok {
			return -1, fmt.Errorf("remote command %q does not start with /bin/sh -c", command)
		}
		line = strings.Join(r.sh, " ") + " -c " + rest
	}
	// stdout and stderr go through pipes of the runner's own, and the call
	// ends only when the shell exited and every process holding them closed
	// them — as sshd keeps a session open until its stdout and stderr reach
	// EOF. A stopped call ends the whole remote side, as a dropped ssh
	// session does: the script, git and whatever its hooks started, even
	// after the shell itself exited.
	outR, outW, err := os.Pipe()
	if err != nil {
		return -1, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		return -1, err
	}
	cmd := exec.Command(r.sh[0], append(slices.Clone(r.sh[1:]), "-c", line)...) //nolint:gosec,noctx // the command under test, in a test; ctx is honoured below, by process group
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = stdin, outW, errW, r.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	_ = outW.Close()
	_ = errW.Close()
	pgid := cmd.Process.Pid
	copied := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(stdout, outR); copied <- struct{}{} }()
	go func() { _, _ = io.Copy(stderr, errR); copied <- struct{}{} }()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var waitErr error
	stopped := false
	done := ctx.Done()
	for pending := 3; pending > 0; {
		select {
		case waitErr = <-exited:
			pending--
		case <-copied:
			pending--
		case <-done:
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			stopped, done = true, nil
		}
	}
	_ = outR.Close()
	_ = errR.Close()
	if stopped {
		return -1, ctx.Err()
	}
	if waitErr == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), nil
	}
	return -1, waitErr
}

func (r *shellRunner) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.commands)
}

const (
	relayLabKey       = "project-key-FAKE-91c2"
	relayLabAppID     = "app-1"
	relayTokenUser    = "x-access-token"
	relayFixtureToken = "ghs_FAKEfixture"
)

// relayMint is one token the fake HQ was asked for.
type relayMint struct{ path, access string }

// relayLab is a fake HQ and a GitHub stand-in. HQ serves git under /git/
// behind `mate:<project key>`, mints a read token for any repository the
// stand-in holds (as HQ mints for any repository the application mounts) and
// refuses a Mate one that can write, and the stand-in serves each repository
// behind the tokens HQ minted for it, refusing a push with any of them.
type relayLab struct {
	hq, gh *httptest.Server
	root   string
	home   string

	mu         sync.Mutex
	mints      []relayMint
	tokens     map[string]string // token → repository key
	tokenUse   map[string]int    // token → requests the stand-in accepted with it
	challenges int               // 401s the stand-in answered
	sawAuth    []string          // every Authorization header either server saw

	// GitHub's API on the stand-in: DELETE /installation/token.
	revokeAsked  []string        // every token a revoke named
	revokeStatus int             // what a revoke answers; 0 = 204, the token gone
	revokeWait   chan struct{}   // optional receipt for a revoke that never answers
	revokeDone   chan struct{}   // optional receipt after a token is removed
	mintHold     <-chan struct{} // optional gate before minting a token
}

// serveRevoke is GitHub's DELETE /installation/token, authenticated by the
// token it revokes.
func (l *relayLab) serveRevoke(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	l.mu.Lock()
	wait, status := l.revokeWait, l.revokeStatus
	if ok {
		l.revokeAsked = append(l.revokeAsked, token)
	}
	l.mu.Unlock()
	if wait != nil {
		close(wait)
		<-r.Context().Done()
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	l.mu.Lock()
	delete(l.tokens, token)
	done := l.revokeDone
	l.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	w.WriteHeader(http.StatusNoContent)
}

// revokes is every token a revoke named so far.
func (l *relayLab) revokes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.revokeAsked)
}

// mintedTokens is every token HQ minted so far, in order.
func (l *relayLab) mintedTokens() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	tokens := make([]string, len(l.mints))
	for i := range l.mints {
		tokens[i] = fmt.Sprintf("ghs_FAKE%04d", i+1)
	}
	return tokens
}

func newRelayLab(t *testing.T) *relayLab {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	l := &relayLab{
		root: t.TempDir(), home: t.TempDir(), tokens: map[string]string{relayFixtureToken: "*"}, tokenUse: map[string]int{},
	}
	backend := func(root, prefix string) *cgi.Handler {
		return &cgi.Handler{
			Path: gitPath, Args: []string{"http-backend"}, Root: prefix,
			Env: []string{
				"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=lab",
				"HOME=" + l.home, "GIT_CONFIG_NOSYSTEM=1",
			},
			InheritEnv: []string{"PATH"},
			Stderr:     io.Discard,
		}
	}
	hqGit := backend(filepath.Join(l.root, "hq"), "/git")
	ghGit := backend(filepath.Join(l.root, "gh"), "")
	l.hq = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.saw(r)
		switch {
		case r.URL.Path == "/api/mate/git-credential":
			l.serveMint(t, w, r)
		case strings.HasPrefix(r.URL.Path, "/git/"):
			if user, pass, ok := r.BasicAuth(); !ok || user != hq.GitUser || pass != relayLabKey {
				w.Header().Set("WWW-Authenticate", `Basic realm="hq"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			hqGit.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(l.hq.Close)
	l.gh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/installation/token" {
			l.serveRevoke(w, r)
			return
		}
		l.saw(r)
		key := relayRepoKeyOf(r.URL.Path)
		user, pass, ok := r.BasicAuth()
		l.mu.Lock()
		owner, minted := l.tokens[pass]
		good := ok && user == relayTokenUser && minted && (owner == key || owner == "*")
		pushing := strings.HasSuffix(r.URL.Path, "/git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"
		// Only the fixture's own token writes: every token HQ mints reads.
		if good && pushing && pass != relayFixtureToken {
			l.mu.Unlock()
			http.Error(w, "a read token cannot push", http.StatusForbidden)
			return
		}
		if good {
			l.tokenUse[pass]++
		} else {
			l.challenges++
		}
		l.mu.Unlock()
		if !good {
			w.Header().Set("WWW-Authenticate", `Basic realm="github"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ghGit.ServeHTTP(w, r)
	}))
	t.Cleanup(l.gh.Close)
	return l
}

// relayRepoKeyOf is the `<owner>/<repo>` a stand-in request path names.
func relayRepoKeyOf(path string) string {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"))
}

func (l *relayLab) saw(r *http.Request) {
	if a := r.Header.Get("Authorization"); a != "" {
		l.mu.Lock()
		l.sawAuth = append(l.sawAuth, a)
		l.mu.Unlock()
	}
}

// serveMint is HQ's GET /api/mate/git-credential: a read token for any
// repository the stand-in holds, recorded with the access it was asked for;
// a Mate asking for one that can write is refused (read_only_remote).
func (l *relayLab) serveMint(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Header.Get("Authorization") != "Project "+relayLabKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	if q.Get("host") != hq.GitHubHost {
		t.Errorf("HQ asked for a token for host %q", q.Get("host"))
	}
	path := q.Get("path")
	key := strings.ToLower(strings.TrimSuffix(path, ".git"))
	if _, err := os.Stat(filepath.Join(l.root, "gh", key+".git")); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"git_credential_refused","reason":"not_mounted"}`)
		return
	}
	access := q.Get("access")
	l.mu.Lock()
	hold := l.mintHold
	l.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	l.mu.Lock()
	l.mints = append(l.mints, relayMint{path: path, access: access})
	if access == "write" || access == "workflows" {
		l.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"git_credential_refused","reason":"read_only_remote"}`)
		return
	}
	token := fmt.Sprintf("ghs_FAKE%04d", len(l.mints))
	l.tokens[token] = key
	l.mu.Unlock()
	writeJSON(w, map[string]string{"username": relayTokenUser, "password": token, "expiresAt": "2026-10-29T13:00:00Z"})
}

// writeJSON answers v as JSON, or 500 when it cannot be encoded.
func writeJSON(w http.ResponseWriter, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// minted is every token HQ was asked for so far.
func (l *relayLab) minted() []relayMint {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.mints)
}

// env is the dev service's environment the script runs in.
func (l *relayLab) env(extraPath ...string) []string {
	path := os.Getenv("PATH")
	if len(extraPath) > 0 {
		path = strings.Join(extraPath, string(os.PathListSeparator)) + string(os.PathListSeparator) + path
	}
	return []string{"HOME=" + l.home, "GIT_CONFIG_NOSYSTEM=1", "PATH=" + path, "LC_ALL=C"}
}

// git runs a fixture git command in dir, as no relayed call.
func (l *relayLab) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Dir = dir
	cmd.Env = append(l.env(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// relayHistory is a repository's three commits: app.txt in each version,
// lib.txt from the second — a partial clone of the third lacks the blobs of
// the first two.
type relayHistory struct{ first, second, third string }

// repo makes the bare repository name (`<owner>/<repo>` on the stand-in,
// `<org>/<app>/<name>` under HQ's /git/) and answers its URL and history.
func (l *relayLab) repo(t *testing.T, onGitHub bool, name string) (string, relayHistory) {
	t.Helper()
	work := t.TempDir()
	l.git(t, work, "init", "-q", "-b", "main")
	var h relayHistory
	for i, files := range []map[string]string{
		{"app.txt": "first contents\n"},
		{"app.txt": "second contents\n", "lib.txt": "lib one\n"},
		{"app.txt": "third contents\n", "lib.txt": "lib two\n"},
	} {
		for f, body := range files {
			if err := os.WriteFile(filepath.Join(work, f), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		l.git(t, work, "add", "-A")
		l.git(t, work, "commit", "-q", "-m", fmt.Sprintf("version %d", i+1))
		sha := l.git(t, work, "rev-parse", "HEAD")
		switch i {
		case 0:
			h.first = sha
		case 1:
			h.second = sha
		default:
			h.third = sha
		}
	}
	side, url := filepath.Join(l.root, "hq"), l.hq.URL+"/git/"+name+".git"
	if onGitHub {
		side, url = filepath.Join(l.root, "gh"), l.gh.URL+"/"+name+".git"
	}
	bare := filepath.Join(side, name+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	l.git(t, work, "clone", "-q", "--bare", work, bare)
	for _, kv := range [][2]string{{"uploadpack.allowFilter", "true"}, {"uploadpack.allowAnySHA1InWant", "true"}, {"http.receivepack", "true"}} {
		l.git(t, bare, "config", kv[0], kv[1])
	}
	return url, h
}

// checkout is the dev service's checkout of url: a `--filter=blob:none`
// partial clone, origin url, the fixture's own credential never kept.
func (l *relayLab) checkout(t *testing.T, url string) string {
	t.Helper()
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(hq.GitUser+":"+relayLabKey))
	if strings.HasPrefix(url, l.gh.URL) {
		header = "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(relayTokenUser+":"+relayFixtureToken))
	}
	dir := filepath.Join(t.TempDir(), "checkout")
	l.git(t, filepath.Dir(dir), "-c", "http.extraHeader="+header, "clone", "-q", "--filter=blob:none", url, dir)
	l.git(t, dir, "config", "user.name", "mate")
	l.git(t, dir, "config", "user.email", "mate@example.invalid")
	return dir
}

// relay is the relay of a call whose records name repos.
func (l *relayLab) relay(repos ...string) BrokeredGitRelay {
	return BrokeredGitRelay{
		Identity:     hq.Identity{Origin: l.hq.URL, Key: relayLabKey, ProjectID: "p1"},
		HTTPClient:   l.hq.Client(),
		Repos:        repos,
		githubOrigin: l.gh.URL,
		githubAPI:    l.gh.URL,
	}
}

// relayRun is what one relayed call did.
type relayRun struct {
	code           int
	err            error
	stdout, stderr string
	commands       []string
}

// run runs `zcp git appdev <gitArgs…>` relayed in dir under sh, with stdin.
func (l *relayLab) run(t *testing.T, sh []string, dir string, relay BrokeredGitRelay, stdin string, gitArgs ...string) relayRun {
	t.Helper()
	return l.runEnv(t, sh, l.env(), dir, relay, stdin, gitArgs...)
}

func (l *relayLab) runEnv(t *testing.T, sh, env []string, dir string, relay BrokeredGitRelay, stdin string, gitArgs ...string) relayRun {
	t.Helper()
	parsed, err := ParseBrokeredGitArgs(gitArgs)
	if err != nil {
		t.Fatalf("ParseBrokeredGitArgs(%q): %v", gitArgs, err)
	}
	runner := &shellRunner{sh: sh, env: env}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code, err := RunBrokeredGitRelayed(ctx, runner, "appdev", dir, parsed, relay, strings.NewReader(stdin), &stdout, &stderr)
	if ctx.Err() != nil {
		t.Fatalf("zcp git %q did not end within its bound; stderr:\n%s", gitArgs, stderr.String())
	}
	return relayRun{code: code, err: err, stdout: stdout.String(), stderr: stderr.String(), commands: runner.seen()}
}

// mustSucceed fails the test unless the relayed call ended 0.
func (r relayRun) mustSucceed(t *testing.T, what string) relayRun {
	t.Helper()
	if r.err != nil || r.code != 0 {
		t.Fatalf("%s: exit %d, err %v\nstdout:\n%s\nstderr:\n%s", what, r.code, r.err, r.stdout, r.stderr)
	}
	return r
}

// writePreCommitHook installs an executable pre-commit hook in the checkout.
func writePreCommitHook(t *testing.T, checkout, body string) {
	t.Helper()
	path := filepath.Join(checkout, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// missingBlobs is how many objects the checkout lacks: a partial clone's
// deferred contents.
func (l *relayLab) missingBlobs(t *testing.T, checkout string) int {
	t.Helper()
	return strings.Count(l.git(t, checkout, "rev-list", "--objects", "--all", "--missing=print"), "?")
}

// TestBrokeredGit_Relay_PushFetchLsRemote_AgainstAuthServer: ls-remote,
// fetch and push against an HQ behind Basic auth, each answered with the
// project key over the ssh session.
func TestBrokeredGit_Relay_PushFetchLsRemote_AgainstAuthServer(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			relay := l.relay()

			run := l.run(t, sh, checkout, relay, "", "ls-remote", "origin").mustSucceed(t, "ls-remote")
			if !strings.Contains(run.stdout, h.third+"\trefs/heads/main") {
				t.Errorf("ls-remote stdout = %q, want main at %s", run.stdout, h.third)
			}

			bare := filepath.Join(l.root, "hq", "org/app-1/appdev.git")
			elsewhere := t.TempDir()
			l.git(t, elsewhere, "clone", "-q", bare, "w")
			w := filepath.Join(elsewhere, "w")
			l.git(t, w, "commit", "-q", "--allow-empty", "-m", "pushed from elsewhere")
			l.git(t, w, "push", "-q", "origin", "main")
			moved := l.git(t, w, "rev-parse", "HEAD")
			l.run(t, sh, checkout, relay, "", "fetch", "origin").mustSucceed(t, "fetch")
			if got := l.git(t, checkout, "rev-parse", "origin/main"); got != moved {
				t.Errorf("origin/main = %s after fetch, want %s", got, moved)
			}

			l.git(t, checkout, "commit", "-q", "--allow-empty", "-m", "mate's work")
			mine := l.git(t, checkout, "rev-parse", "HEAD")
			l.run(t, sh, checkout, relay, "", "push", "origin", "HEAD:refs/heads/feature").mustSucceed(t, "push")
			if got := l.git(t, bare, "rev-parse", "refs/heads/feature"); got != mine {
				t.Errorf("HQ's feature = %s, want the pushed %s", got, mine)
			}
		})
	}
}

// TestBrokeredGit_Relay_PartialClone_LazyShowCheckoutWorktreeAnswered: on a
// blob:none partial clone, `show <old>:<path>`, `log -p`, `checkout <old>`
// and `worktree add` fetch contents lazily, each fetch answered.
func TestBrokeredGit_Relay_PartialClone_LazyShowCheckoutWorktreeAnswered(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			if l.missingBlobs(t, checkout) == 0 {
				t.Fatal("the fixture checkout is no partial clone: no object missing")
			}
			relay := l.relay()

			if run := l.run(t, sh, checkout, relay, "", "show", h.first+":app.txt").mustSucceed(t, "show"); run.stdout != "first contents\n" {
				t.Errorf("show %s:app.txt = %q", h.first, run.stdout)
			}
			if run := l.run(t, sh, checkout, relay, "", "log", "-p", "--format=%H").mustSucceed(t, "log -p"); !strings.Contains(run.stdout, "+second contents") || !strings.Contains(run.stdout, "+lib one") {
				t.Errorf("log -p = %q, want every version's diff", run.stdout)
			}
			wt := filepath.Join(t.TempDir(), "wt-old")
			l.run(t, sh, checkout, relay, "", "worktree", "add", "--detach", wt, h.second).mustSucceed(t, "worktree add")
			if got := readFile(t, filepath.Join(wt, "lib.txt")); got != "lib one\n" {
				t.Errorf("worktree lib.txt = %q", got)
			}
			l.run(t, sh, checkout, relay, "", "checkout", "-q", h.first).mustSucceed(t, "checkout")
			if got := readFile(t, filepath.Join(checkout, "app.txt")); got != "first contents\n" {
				t.Errorf("checked-out app.txt = %q", got)
			}
			if n := l.missingBlobs(t, checkout); n != 0 {
				t.Errorf("%d objects still missing after reading every version", n)
			}
		})
	}
}

// TestBrokeredGit_Relay_PartialClone_GitHubLazyFetchesShareOneMintedToken:
// a GitHub-kept partial clone asks for a credential on each lazy fetch; HQ
// mints one token for the repository and the call answers every request
// with it.
func TestBrokeredGit_Relay_PartialClone_GitHubLazyFetchesShareOneMintedToken(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			l.mu.Lock()
			before := l.challenges
			l.mu.Unlock()
			run := l.run(t, sh, checkout, l.relay(url), "", "log", "-p", "--format=%H").mustSucceed(t, "log -p")
			if !strings.Contains(run.stdout, "+first contents") {
				t.Errorf("log -p = %q, want the first version's diff", run.stdout)
			}
			l.mu.Lock()
			asked := l.challenges - before
			l.mu.Unlock()
			if asked < 2 {
				t.Fatalf("git asked for a credential %d times, want at least two lazy fetches", asked)
			}
			if mints := l.minted(); len(mints) != 1 || mints[0].path != "acme/shop" {
				t.Errorf("HQ minted %+v, want one token minted with the canonical path acme/shop", mints)
			}
			l.mu.Lock()
			used := l.tokenUse["ghs_FAKE0001"]
			l.mu.Unlock()
			if used < 2 {
				t.Errorf("the minted token was used %d times, want every lazy fetch", used)
			}
		})
	}
}

// TestBrokeredGit_Relay_PartialClone_HQPairWithoutGitToken_Answered: a pair
// not wired through holds no GIT_TOKEN, and the helper persisted in its
// checkout would answer an empty password; the relay answers its lazy
// fetches with the project key instead.
func TestBrokeredGit_Relay_PartialClone_HQPairWithoutGitToken_Answered(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "config", "credential.helper", `!f() { echo username=mate; echo "password=$GIT_TOKEN"; }; f`)
			if run := l.run(t, sh, checkout, l.relay(), "", "show", h.second+":lib.txt").mustSucceed(t, "show"); run.stdout != "lib one\n" {
				t.Errorf("show = %q", run.stdout)
			}
			empty := "Basic " + base64.StdEncoding.EncodeToString([]byte("mate:"))
			l.mu.Lock()
			defer l.mu.Unlock()
			if slices.Contains(l.sawAuth, empty) {
				t.Error("HQ was sent an empty password")
			}
		})
	}
}

// TestBrokeredGit_Relay_HQFrame_AnsweredWithTheProjectKeyAnyPath: HQ is
// answered for any path with the Mate's project key under HQ's git user —
// the key HQ asks Zerops about on the push, covering every repository of the
// application.
// witness: internal/hq/gitcredential.go | return GitAnswer{Username: GitUser, Password: id.Key}, nil → return GitAnswer{Username: GitUser, Password: id.ProjectID}, nil
func TestBrokeredGit_Relay_HQFrame_AnsweredWithTheProjectKeyAnyPath(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			other, otherHistory := l.repo(t, false, "org/app-1/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "add", "sibling", other)
			run := l.run(t, sh, checkout, l.relay(), "", "ls-remote", "sibling").mustSucceed(t, "ls-remote sibling")
			if !strings.Contains(run.stdout, otherHistory.third) {
				t.Errorf("ls-remote sibling = %q, want its main", run.stdout)
			}
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte(hq.GitUser+":"+relayLabKey))
			l.mu.Lock()
			defer l.mu.Unlock()
			for _, a := range l.sawAuth {
				if a != want {
					t.Errorf("HQ was sent %q, want only the project key", a)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_FetchByURLInArgv_Answered: a URL the call's argv
// names is answered — HQ mints for that repository — though no record names
// it.
func TestBrokeredGit_Relay_FetchByURLInArgv_Answered(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			lib, libHistory := l.repo(t, true, "acme/lib")
			checkout := l.checkout(t, url)
			args := []string{"fetch", lib, "main:refs/remotes/lib/main"}
			parsed, err := ParseBrokeredGitArgs(args)
			if err != nil {
				t.Fatal(err)
			}
			relay := l.relay(append([]string{url}, BrokeredGitArgvURLs(parsed, l.hq.URL)...)...)
			l.run(t, sh, checkout, relay, "", args...).mustSucceed(t, "fetch by URL")
			if got := l.git(t, checkout, "rev-parse", "refs/remotes/lib/main"); got != libHistory.third {
				t.Errorf("lib/main = %s, want %s", got, libHistory.third)
			}
			if mints := l.minted(); len(mints) != 1 || mints[0].path != "acme/lib" {
				t.Errorf("HQ minted %+v, want one token minted with the canonical path acme/lib", mints)
			}
		})
	}
}

// TestBrokeredGit_Relay_NoRecordedOrigin_AnswersHQsLiveOriginReadOnce: while
// the record names no origin, the first GitHub request reads the Mate's state
// for the record's repository once; its origin is answered for every lazy
// fetch, anything else declined.
func TestBrokeredGit_Relay_NoRecordedOrigin_AnswersHQsLiveOriginReadOnce(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			other, _ := l.repo(t, true, "acme/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "add", "evil", other)
			path := filepath.Join(t.TempDir(), "state.json")
			release, err := hq.LockMateState(filepath.Join(filepath.Dir(path), "state.lock"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
			appID := relayLabAppID
			state := hq.MateState{ProjectID: "p1", AppID: &appID, Repos: []hq.MateRepo{{Name: "appdev", Origin: &hq.RepoOrigin{URL: url, Mode: hq.OriginOnGitHub}}}, Changes: []hq.MateChange{}}
			raw, err := json.Marshal(map[string]any{"link": "linked", "mate": state})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			relay := l.relay()
			relay.Identity.State = hq.NewFileMateStateSource(path, "p1")
			relay.LiveOrigin = &BrokeredGitHQRepo{AppID: relayLabAppID, Repo: "appdev"}
			l.run(t, sh, checkout, relay, "", "log", "-p", "--format=%H").mustSucceed(t, "log -p")
			if mints := l.minted(); len(mints) != 1 || mints[0].path != "acme/shop" {
				t.Errorf("HQ minted %+v, want one token for the live origin", mints)
			}
			assertDeclined(t, l, l.run(t, sh, checkout, relay, "", "fetch", "evil"))
		})
	}
}

// assertDeclined checks a relayed call git could not authenticate: a
// failure, zcp naming the remote it does not answer, and no token minted for
// acme/other, the repository no record names.
func assertDeclined(t *testing.T, l *relayLab, run relayRun) {
	t.Helper()
	if run.code == 0 {
		t.Errorf("exit 0, want git failing without a credential\nstderr:\n%s", run.stderr)
	}
	if !strings.Contains(run.stderr, "is not a remote zcp records for appdev") {
		t.Errorf("stderr = %q, want zcp naming the remote it does not answer", run.stderr)
	}
	for _, m := range l.minted() {
		if strings.EqualFold(strings.TrimSuffix(m.path, ".git"), "acme/other") {
			t.Errorf("HQ minted a token for %s", m.path)
		}
	}
}

// TestBrokeredGit_Relay_RemoteAddedInCheckoutConfig_Declined: the checkout's
// config is the dev service's to write; a remote added there is answered for
// nothing, though HQ would mint for it.
func TestBrokeredGit_Relay_RemoteAddedInCheckoutConfig_Declined(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			other, _ := l.repo(t, true, "acme/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "add", "evil", other)
			assertDeclined(t, l, l.run(t, sh, checkout, l.relay(url), "", "fetch", "evil"))
			assertDeclined(t, l, l.run(t, sh, checkout, l.relay(url), "", "ls-remote", "evil"))
		})
	}
}

// TestBrokeredGit_Relay_RemoteURLRecordIgnored: the record's RemoteURL is
// written from the checkout's live origin, so an origin pointed at another
// repository is answered for nothing: the relay is built from the record's
// HQ origin, which zcp writes from HQ's own state.
func TestBrokeredGit_Relay_RemoteURLRecordIgnored(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			other, _ := l.repo(t, true, "acme/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "set-url", "origin", other)
			assertDeclined(t, l, l.run(t, sh, checkout, l.relay(url), "", "fetch", "origin"))
		})
	}
}

// TestBrokeredGit_Relay_InsteadOfRewriteToOtherRepository_Declined: a
// url.insteadOf in the checkout's config rewriting the recorded repository
// to another is answered for the repository git asks about, which no record
// names.
func TestBrokeredGit_Relay_InsteadOfRewriteToOtherRepository_Declined(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			other, _ := l.repo(t, true, "acme/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "config", "url."+other+".insteadOf", url)
			assertDeclined(t, l, l.run(t, sh, checkout, l.relay(url), "", "fetch", "origin"))
		})
	}
}

// TestBrokeredGit_Relay_StdinPayloadOver64KiB_WithLazyFetchingPreCommitHook_NeverLeaksAnswer:
// `commit -F -` with a message over 64 KiB while the pre-commit hook fetches
// lazily: the message is the payload byte for byte, the hook's fetch is
// answered, and no answer reaches the commit.
func TestBrokeredGit_Relay_StdinPayloadOver64KiB_WithLazyFetchingPreCommitHook_NeverLeaksAnswer(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			marker := filepath.Join(t.TempDir(), "hook-saw")
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt > "+ShellQuote(marker)+"\n")
			var payload strings.Builder
			payload.WriteString("subject of a long message\n\n")
			for i := 0; payload.Len() < 100<<10; i++ {
				fmt.Fprintf(&payload, "line %05d of the body, with 'quotes', $dollars and \\backslashes\n", i)
			}
			l.run(t, sh, checkout, l.relay(url), payload.String(), "commit", "--allow-empty", "--cleanup=verbatim", "-F", "-").mustSucceed(t, "commit -F -")
			raw := l.git(t, checkout, "cat-file", "commit", "HEAD")
			_, message, _ := strings.Cut(raw, "\n\n")
			if message != strings.TrimSpace(payload.String()) {
				t.Errorf("commit message differs from the payload (%d bytes vs %d)", len(message), len(payload.String()))
			}
			for _, leak := range []string{"ghs_FAKE", relayTokenUser, "ZCPCRED", relayLabKey} {
				if strings.Contains(raw, leak) {
					t.Errorf("the commit carries %q", leak)
				}
			}
			if got := readFile(t, marker); got != "first contents\n" {
				t.Errorf("the hook's lazy fetch read %q", got)
			}
		})
	}
}

// TestBrokeredGit_Relay_HookRedirectingStderr_LazyFetchCompletes: frames go
// to their own descriptor, so a hook's `2>/dev/null` swallows none.
func TestBrokeredGit_Relay_HookRedirectingStderr_LazyFetchCompletes(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			marker := filepath.Join(t.TempDir(), "hook-saw")
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt > "+ShellQuote(marker)+" 2>/dev/null\n")
			run := l.run(t, sh, checkout, l.relay(), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			if got := readFile(t, marker); got != "first contents\n" {
				t.Errorf("the hook's lazy fetch read %q", got)
			}
			if strings.Contains(run.stdout+run.stderr, "ZCPCRED") {
				t.Errorf("a frame reached the caller: stdout %q stderr %q", run.stdout, run.stderr)
			}
		})
	}
}

// TestBrokeredGit_Relay_FrameWriteFails_AnswersNothingAtOnce: a helper that
// cannot write its frame — descriptor 4 closed — answers nothing at once
// rather than waiting on an answer that never comes.
func TestBrokeredGit_Relay_FrameWriteFails_AnswersNothingAtOnce(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			result := filepath.Join(t.TempDir(), "result")
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt >/dev/null 2>&1 4>&-; echo \"$?\" > "+ShellQuote(result)+"; exit 0\n")
			l.run(t, sh, checkout, l.relay(), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			if code := strings.TrimSpace(readFile(t, result)); code == "" || code == "0" {
				t.Fatalf("the hook's show = %q, want a failure without a credential", code)
			}
		})
	}
}

// TestBrokeredGit_Relay_HelperDescriptorsNotTheSessions_AnswersNothingAtOnce:
// a helper whose descriptor 4 or 3 is open but no longer the session's — a
// hook pointed it elsewhere, or git reused the number for a socket of its own
// once a hook closed it — writes no frame there and waits on nothing there:
// it answers nothing at once, and the lazy fetch fails without a credential.
func TestBrokeredGit_Relay_HelperDescriptorsNotTheSessions_AnswersNothingAtOnce(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct{ name, redirect string }{
				{"descriptor 4 to a file", `4>"$elsewhere"`},
				{"descriptor 3 from a fifo nobody writes", `3<>"$fifo"`},
			} {
				l := newRelayLab(t)
				url, h := l.repo(t, false, "org/app-1/appdev")
				checkout := l.checkout(t, url)
				dir := t.TempDir()
				result, elsewhere, fifo := filepath.Join(dir, "result"), filepath.Join(dir, "elsewhere"), filepath.Join(dir, "fifo")
				if err := syscall.Mkfifo(fifo, 0o600); err != nil {
					t.Fatal(err)
				}
				writePreCommitHook(t, checkout, "elsewhere="+ShellQuote(elsewhere)+"; fifo="+ShellQuote(fifo)+"; s=$(date +%s); "+
					"git show "+h.first+":app.txt >/dev/null 2>&1 "+tc.redirect+"; echo \"$? $(( $(date +%s) - s ))\" > "+ShellQuote(result)+"; exit 0\n")
				parsed, err := ParseBrokeredGitArgs([]string{"commit", "--allow-empty", "-m", "x"})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				var stderr bytes.Buffer
				code, err := RunBrokeredGitRelayed(ctx, &shellRunner{sh: sh, env: l.env()}, "appdev", checkout, parsed, l.relay(), strings.NewReader(""), io.Discard, &stderr)
				stopped := ctx.Err() != nil
				cancel()
				if stopped {
					t.Errorf("%s: the call did not end within 30s: the helper waited on a descriptor that is not the session's", tc.name)
					continue
				}
				if err != nil || code != 0 {
					t.Errorf("%s: exit %d, err %v\n%s", tc.name, code, err, stderr.String())
				}
				fields := strings.Fields(readFile(t, result))
				if len(fields) != 2 || fields[0] == "0" {
					t.Errorf("%s: the hook's show = %q, want a failure without a credential", tc.name, fields)
				} else if took, _ := strconv.Atoi(fields[1]); took > 10 {
					t.Errorf("%s: the show took %ds, want at once", tc.name, took)
				}
				if b, _ := os.ReadFile(elsewhere); bytes.Contains(b, []byte("ZCPCRED-")) {
					t.Errorf("%s: the helper wrote its frame to a descriptor that is not the session's: %q", tc.name, b)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_ConcurrentHelpers_SerialisedByLock: a hook that
// starts two lazy fetches at once gets both answered right, one token
// shared — while HQ takes its time minting, one helper finds the lock held
// and waits its turn (a mkdir on PATH logs each attempt the lock refused).
func TestBrokeredGit_Relay_ConcurrentHelpers_SerialisedByLock(t *testing.T) {
	t.Parallel()
	realMkdir, err := exec.LookPath("mkdir")
	if err != nil {
		t.Skip("no mkdir on PATH")
	}
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			dir := t.TempDir()
			one, two := filepath.Join(dir, "one"), filepath.Join(dir, "two")
			held := filepath.Join(dir, "held")
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				t.Fatal(err)
			}
			if err := watcher.Add(dir); err != nil {
				t.Fatal(err)
			}
			mintHold := make(chan struct{})
			var release sync.Once
			l.mu.Lock()
			l.mintHold = mintHold
			l.mu.Unlock()
			watched := make(chan error, 1)
			go func() {
				defer release.Do(func() { close(mintHold) })
				for {
					select {
					case event, ok := <-watcher.Events:
						if !ok {
							watched <- errors.New("lock receipt watcher closed")
							return
						}
						if event.Name == held && event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
							watched <- nil
							return
						}
					case err := <-watcher.Errors:
						watched <- err
						return
					}
				}
			}()
			t.Cleanup(func() {
				_ = watcher.Close()
				release.Do(func() { close(mintHold) })
			})
			stub := t.TempDir()
			body := "#!/bin/sh\n" + ShellQuote(realMkdir) + " \"$@\" 2>/dev/null; rc=$?\ncase \"$*\" in *zcp-relay-*) [ $rc -eq 0 ] || echo held >> " + ShellQuote(held) + ";; esac\nexit $rc\n"
			if err := os.WriteFile(filepath.Join(stub, "mkdir"), []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt > "+ShellQuote(one)+" &\ngit show "+h.second+":lib.txt > "+ShellQuote(two)+" &\nwait\n")
			l.runEnv(t, sh, l.env(stub), checkout, l.relay(url), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			if err := <-watched; err != nil {
				t.Fatalf("watch lock contention: %v", err)
			}
			if _, err := os.Stat(held); err != nil {
				t.Errorf("no helper ever found the lock held: the two fetches did not contend (%v)", err)
			}
			if got := readFile(t, one); got != "first contents\n" {
				t.Errorf("first lazy fetch read %q", got)
			}
			if got := readFile(t, two); got != "lib one\n" {
				t.Errorf("second lazy fetch read %q", got)
			}
			if mints := l.minted(); len(mints) != 1 {
				t.Errorf("HQ minted %+v, want one token shared", mints)
			}
		})
	}
}

// TestBrokeredGit_Relay_RepoConfigProtocolExtAllow_StillRefused: the
// checkout's `protocol.ext.allow=always` loses to GIT_ALLOW_PROTOCOL — an
// ext:: remote fails without running its command.
func TestBrokeredGit_Relay_RepoConfigProtocolExtAllow_StillRefused(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "config", "protocol.ext.allow", "always")
			marker := filepath.Join(t.TempDir(), "ext-ran")
			run := l.run(t, sh, checkout, l.relay(), "", "ls-remote", "ext::sh -c touch% "+marker)
			if run.code == 0 {
				t.Errorf("ls-remote ext:: exit 0, want refused")
			}
			if !strings.Contains(run.stderr, "transport 'ext' not allowed") {
				t.Errorf("stderr = %q, want the ext transport refused", run.stderr)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Error("the ext:: command ran")
			}
		})
	}
}

// TestBrokeredGit_Relay_BundlePathInArgv_FileTransportAllowed: an absolute
// .bundle path in argv allows the file transport, so a bundle copied into the
// service is fetched; a plain local path stays refused.
func TestBrokeredGit_Relay_BundlePathInArgv_FileTransportAllowed(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			src := t.TempDir()
			l.git(t, src, "init", "-q", "-b", "main")
			l.git(t, src, "commit", "-q", "--allow-empty", "-m", "from a bundle")
			head := l.git(t, src, "rev-parse", "HEAD")
			bundle := filepath.Join(t.TempDir(), "brought.bundle")
			l.git(t, src, "bundle", "create", bundle, "main")
			l.run(t, sh, checkout, l.relay(), "", "fetch", bundle, "main:refs/heads/from-bundle").mustSucceed(t, "fetch bundle")
			if got := l.git(t, checkout, "rev-parse", "refs/heads/from-bundle"); got != head {
				t.Errorf("from-bundle = %s, want %s", got, head)
			}
			run := l.run(t, sh, checkout, l.relay(), "", "fetch", src, "main:refs/heads/from-path")
			if run.code == 0 || !strings.Contains(run.stderr, "transport 'file' not allowed") {
				t.Errorf("fetch from a local path: exit %d, stderr %q; want the file transport refused", run.code, run.stderr)
			}
		})
	}
}

// TestBrokeredGit_Relay_DeclinedHost_FailsWithoutEmptyPassword: a remote on
// a host zcp does not answer fails with nothing sent — never an empty
// password.
func TestBrokeredGit_Relay_DeclinedHost_FailsWithoutEmptyPassword(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			var mu sync.Mutex
			var sent []string
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				sent = append(sent, r.Header.Get("Authorization"))
				mu.Unlock()
				w.Header().Set("WWW-Authenticate", `Basic realm="other"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(other.Close)
			l.git(t, checkout, "remote", "add", "elsewhere", other.URL+"/team/app.git")
			run := l.run(t, sh, checkout, l.relay(), "", "ls-remote", "elsewhere")
			if run.code == 0 {
				t.Error("ls-remote elsewhere exit 0, want a failure")
			}
			if !strings.Contains(run.stderr, "is not a remote zcp records for appdev") {
				t.Errorf("stderr = %q, want zcp naming the remote it does not answer", run.stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, a := range sent {
				if a != "" {
					t.Errorf("the declined host was sent %q, want no Authorization", a)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_GitHubPath_MintedCanonicalOddDeclined: HQ is asked
// for a GitHub repository by its canonical path — owner/name, lower-cased,
// no .git or trailing slash, as HQ keys it — and a path that is no
// owner/name of letters, digits, dot, underscore and hyphen is declined
// before HQ is asked, said once, quoted.
func TestBrokeredGit_Relay_GitHubPath_MintedCanonicalOddDeclined(t *testing.T) {
	t.Parallel()
	l := newRelayLab(t)
	url, _ := l.repo(t, true, "acme/shop")
	ghHost := strings.TrimPrefix(l.gh.URL, "http://")
	var stderr bytes.Buffer
	call := newRelayCall(l.relay(url), "appdev", false, &stderr)
	for _, path := range []string{"acme/shop.git", "Acme/Shop", "acme/shop/", "ACME/SHOP.GIT"} {
		if user, pass := call.answer(t.Context(), "http", ghHost, path); user == "" || pass == "" {
			t.Errorf("answer(%q) declined, want the token for acme/shop", path)
		}
	}
	if mints := l.minted(); len(mints) != 1 || mints[0].path != "acme/shop" {
		t.Errorf("HQ minted %+v, want one token, asked by the canonical path acme/shop", mints)
	}
	for _, path := range []string{"acme", "acme/shop/extra", "acme/../shop", "a b/shop", "acme/sh%6fp", "acme/shop?x=1", "./shop", "acme/" + strings.Repeat("x", 300) + "\x1b[2J"} {
		if user, pass := call.answer(t.Context(), "http", ghHost, path); user != "" || pass != "" {
			t.Errorf("answer(%q) answered, want declined", path)
		}
	}
	if mints := l.minted(); len(mints) != 1 {
		t.Errorf("HQ minted %+v, want nothing asked for an odd path", mints)
	}
	said := stderr.String()
	if !strings.Contains(said, `"acme/shop/extra"`) || strings.Contains(said, "\x1b") {
		t.Errorf("stderr = %q, want each odd path quoted, never raw", said)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(said), "\n") {
		if len(line) > 400 {
			t.Errorf("stderr line of %d bytes, want a dev-controlled path capped: %q", len(line), line[:80])
		}
	}
}

// TestBrokeredGit_Relay_SaidLines_CappedPerCall: code in the dev service
// that asks for many remotes zcp does not record makes zcp say at most 32
// distinct lines in a call — and every request is still declined.
func TestBrokeredGit_Relay_SaidLines_CappedPerCall(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	call := newRelayCall(BrokeredGitRelay{Identity: hq.Identity{Origin: "https://hq.example"}}, "appdev", false, &stderr)
	for i := range 100 {
		if user, _ := call.answer(t.Context(), "https", fmt.Sprintf("other%d.example", i), "x"); user != "" {
			t.Fatalf("request %d answered", i)
		}
	}
	if n := strings.Count(stderr.String(), "\n"); n != relaySayCap {
		t.Errorf("said %d lines, want the cap of %d", n, relaySayCap)
	}
}

// TestBrokeredGit_Relay_GitHubFrameWithoutPath_DeclinedWithoutHelperConfigText:
// a GitHub request naming no repository is declined with zcp's own reason —
// never the helper config the mount fallback prints — and HQ is not asked.
func TestBrokeredGit_Relay_GitHubFrameWithoutPath_DeclinedWithoutHelperConfigText(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			ghHost := strings.TrimPrefix(l.gh.URL, "http://")
			result := filepath.Join(t.TempDir(), "fill")
			writePreCommitHook(t, checkout, "printf 'protocol=http\\nhost="+ghHost+"\\n\\n' | git credential fill > "+ShellQuote(result)+" 2>/dev/null; echo \"rc=$?\" >> "+ShellQuote(result)+"; exit 0\n")
			run := l.run(t, sh, checkout, l.relay(url), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			if got := readFile(t, result); strings.Contains(got, "password=") || strings.Contains(got, "rc=0") {
				t.Errorf("a pathless GitHub request was answered: %q", got)
			}
			if !strings.Contains(run.stderr, "zcp git: ") || !strings.Contains(run.stderr, "no repository") {
				t.Errorf("stderr = %q, want zcp's own reason", run.stderr)
			}
			for _, banned := range []string{"useHttpPath", "credential.https://github.com", "zcp hq git-credential", hq.GitHubHelperConfig} {
				if strings.Contains(run.stderr, banned) {
					t.Errorf("stderr carries the helper config text %q", banned)
				}
			}
			if mints := l.minted(); len(mints) != 0 {
				t.Errorf("HQ minted %+v for a pathless request", mints)
			}
		})
	}
}

// TestBrokeredGit_Relay_NoSecretInArgvStdoutStderr: neither the remote
// command (ssh's argv), nor the caller's stdout or stderr carries the Mate
// credential, a minted token or the project key, across a fetch from GitHub
// and a lazy read, a read and a push at HQ — nor, after the calls, any file
// under the dev service's $HOME, the checkout's .git, or what the relay
// makes in /tmp.
func TestBrokeredGit_Relay_NoSecretInArgvStdoutStderr(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			hqURL, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, hqURL)
			relay := l.relay(url)
			l.git(t, checkout, "commit", "-q", "--allow-empty", "-m", "work")
			runs := make([]relayRun, 0, 4)
			for _, args := range [][]string{
				{"fetch", url, "main"},
				{"show", h.first + ":app.txt"},
				{"ls-remote", "origin"},
				{"push", "origin", "HEAD:refs/heads/feature"},
			} {
				runs = append(runs, l.run(t, sh, checkout, relay, "", args...).mustSucceed(t, strings.Join(args, " ")))
			}
			if len(l.minted()) == 0 {
				t.Fatal("no token minted: the GitHub path was not exercised")
			}
			secrets := []string{relayLabKey, "ghs_FAKE0"}
			for _, run := range runs {
				for _, s := range secrets {
					for _, c := range run.commands {
						if strings.Contains(c, s) {
							t.Errorf("the remote command carries %q", s)
						}
					}
					if strings.Contains(run.stdout, s) || strings.Contains(run.stderr, s) {
						t.Errorf("the caller's output carries %q", s)
					}
				}
			}
			left, _ := filepath.Glob("/tmp/zcp-relay-*")
			stdinDirs, _ := filepath.Glob("/tmp/zcp-stdin.*")
			roots := append(append([]string{l.home, filepath.Join(checkout, ".git")}, left...), stdinDirs...)
			for _, root := range roots {
				_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil //nolint:nilerr // a file another test removes meanwhile is no leak
					}
					raw, err := os.ReadFile(p)
					if err != nil {
						return nil //nolint:nilerr // as above
					}
					for _, s := range secrets {
						if bytes.Contains(raw, []byte(s)) {
							t.Errorf("%s holds %q after the calls", p, s)
						}
					}
					return nil
				})
			}
		})
	}
}

// TestBrokeredGit_Relay_PersistedHelperNeverRuns: helpers persisted in the
// checkout's config — generic and URL-specific — never run on a relayed call.
func TestBrokeredGit_Relay_PersistedHelperNeverRuns(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			marker := filepath.Join(t.TempDir(), "persisted-ran")
			l.git(t, checkout, "config", "credential.helper", "!f() { echo generic >> "+ShellQuote(marker)+"; }; f")
			l.git(t, checkout, "config", "credential."+l.gh.URL+".helper", "!f() { echo url >> "+ShellQuote(marker)+"; }; f")
			l.run(t, sh, checkout, l.relay(url), "", "show", h.first+":app.txt").mustSucceed(t, "show")
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("a persisted helper ran: %q", readFile(t, marker))
			}
		})
	}
}

// TestBrokeredGit_Relay_WorktreeUnderTmp_UsesRecordAllowlist: a call in a
// dev-side worktree below /tmp/ answers by the same records — its origin is
// the checkout's — and declines what they do not name.
func TestBrokeredGit_Relay_WorktreeUnderTmp_UsesRecordAllowlist(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			other, _ := l.repo(t, true, "acme/other")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "add", "evil", other)
			parent, err := os.MkdirTemp("/tmp", "zcp-relay-test-") //nolint:usetesting // -C takes only a path below /tmp/; t.TempDir() is elsewhere on macOS
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(parent) })
			wt := filepath.Join(parent, "wt-feature")
			l.git(t, checkout, "worktree", "add", "-q", "--detach", wt, "HEAD")
			if run := l.run(t, sh, checkout, l.relay(url), "", "-C", wt, "show", h.second+":lib.txt").mustSucceed(t, "show in worktree"); run.stdout != "lib one\n" {
				t.Errorf("show in worktree = %q", run.stdout)
			}
			assertDeclined(t, l, l.run(t, sh, checkout, l.relay(url), "", "-C", wt, "fetch", "evil"))
		})
	}
}

// TestBrokeredGit_Relay_StdinTempDirRemoved: the directory the stdin payload
// is read into is removed when the call ends, whether git succeeded or not.
func TestBrokeredGit_Relay_StdinTempDirRemoved(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			for _, fail := range []bool{false, true} {
				probe := "zcp-stdin-probe-" + relayRandHex(t)
				rec := filepath.Join(t.TempDir(), "dir")
				exit := "exit 0"
				if fail {
					exit = "exit 1"
				}
				writePreCommitHook(t, checkout, "for d in /tmp/zcp-stdin.*; do if [ -f \"$d/in\" ] && grep -q "+probe+" \"$d/in\"; then echo \"$d\" > "+ShellQuote(rec)+"; fi; done\n"+exit+"\n")
				run := l.run(t, sh, checkout, l.relay(), probe+"\n", "commit", "--allow-empty", "-F", "-")
				if (run.code != 0) != fail {
					t.Fatalf("commit (hook fails %v): exit %d\n%s", fail, run.code, run.stderr)
				}
				dir := strings.TrimSpace(readFile(t, rec))
				if !strings.HasPrefix(dir, "/tmp/zcp-stdin.") {
					t.Fatalf("the hook found the payload in %q, want /tmp/zcp-stdin.*", dir)
				}
				if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s still there after the call (hook fails %v): %v", dir, fail, err)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_HelperText_SurvivesBothQuotingLayers: the one-line
// helper, ShellQuoted into its -c value and again into the script the login
// shell hands /bin/sh, reaches git byte for byte — and answers.
func TestBrokeredGit_Relay_HelperText_SurvivesBothQuotingLayers(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			dir := t.TempDir()
			helpers, read := filepath.Join(dir, "helpers"), filepath.Join(dir, "read")
			writePreCommitHook(t, checkout, "git config --get-all credential.helper > "+ShellQuote(helpers)+"\ngit show "+h.first+":app.txt > "+ShellQuote(read)+"\n")
			l.run(t, sh, checkout, l.relay(), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			lines := strings.Split(strings.TrimSuffix(readFile(t, helpers), "\n"), "\n")
			if len(lines) != 2 || lines[0] != "" {
				t.Fatalf("credential.helper values = %q, want the empty reset then the relay helper", lines)
			}
			nonce := regexp.MustCompile(`ZCPCRED-([0-9a-f]{16}) `).FindStringSubmatch(lines[1])
			if nonce == nil {
				t.Fatalf("helper %q names no nonce", lines[1])
			}
			if want := relayHelper(nonce[1]); lines[1] != want {
				t.Errorf("git's helper = %q\nwant byte for byte %q", lines[1], want)
			}
			if got := readFile(t, read); got != "first contents\n" {
				t.Errorf("the helper's answer read %q", got)
			}
		})
	}
}

// shellRunsFromPath reports whether the shell sh runs the command name from
// PATH: a busybox built to prefer its own applets (Ubuntu's 1.30.1 is) runs
// its head, mktemp or date whatever PATH says, so no stub on PATH stands in
// for them there.
func shellRunsFromPath(t *testing.T, sh []string, name string) bool {
	t.Helper()
	stub, tmp := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, name), []byte("#!/bin/sh\necho zcp-stub-ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), sh[0], append(slices.Clone(sh[1:]), "-c", name+" </dev/null")...) //nolint:gosec // a fixed probe in the shell under test
	cmd.Env = []string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR=" + tmp, "LC_ALL=C"}
	out, _ := cmd.Output()
	return strings.Contains(string(out), "zcp-stub-ran")
}

// shortPayloadRunner is a session whose forwarded stdin arrives short: it
// passes the payload's length line on, then half the bytes it announces,
// then the end of the session's stdin.
type shortPayloadRunner struct{ session *shellRunner }

func (r shortPayloadRunner) ExecSSHStdio(ctx context.Context, host, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return r.session.ExecSSHStdio(ctx, host, command, &shortPayload{r: bufio.NewReader(stdin)}, stdout, stderr)
}

type shortPayload struct {
	r      *bufio.Reader
	header string
	left   int
	read   bool
}

func (s *shortPayload) Read(p []byte) (int, error) {
	if !s.read {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		n, _ := strconv.Atoi(strings.TrimSpace(line))
		s.header, s.left, s.read = line, n/2, true
	}
	if s.header != "" {
		n := copy(p, s.header)
		s.header = s.header[n:]
		return n, nil
	}
	if s.left == 0 {
		return 0, io.EOF
	}
	n, err := s.r.Read(p[:min(len(p), s.left)])
	s.left -= n
	return n, err
}

// TestBrokeredGit_Relay_StdinStagingFails_SaysSoInOneLine: a forwarded
// stdin the service cannot stage — mktemp failing, or the payload arriving
// short of the length announced — ends the call with 128 and one line saying
// so, git never run.
func TestBrokeredGit_Relay_StdinStagingFails_SaysSoInOneLine(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name   string
				runner func(l *relayLab, stub string) SSHStdioRunner
				stub   bool
			}{
				{"mktemp fails", func(l *relayLab, stub string) SSHStdioRunner {
					return &shellRunner{sh: sh, env: l.env(stub)}
				}, true},
				{"the payload arrives short", func(l *relayLab, _ string) SSHStdioRunner {
					return shortPayloadRunner{session: &shellRunner{sh: sh, env: l.env()}}
				}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if tc.stub && !shellRunsFromPath(t, sh, "mktemp") {
						t.Skip(shellName(sh) + " runs its own mktemp applet whatever PATH says, so no failing mktemp can stand in; the payload arriving short exercises the same failure there")
					}
					l := newRelayLab(t)
					url, _ := l.repo(t, false, "org/app-1/appdev")
					checkout := l.checkout(t, url)
					stub := t.TempDir()
					if err := os.WriteFile(filepath.Join(stub, "mktemp"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
						t.Fatal(err)
					}
					parsed, err := ParseBrokeredGitArgs([]string{"commit", "--allow-empty", "-F", "-"})
					if err != nil {
						t.Fatal(err)
					}
					before := l.git(t, checkout, "rev-parse", "HEAD")
					ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
					defer cancel()
					var stderr bytes.Buffer
					code, err := RunBrokeredGitRelayed(ctx, tc.runner(l, stub), "appdev", checkout, parsed, l.relay(), strings.NewReader(strings.Repeat("message line\n", 64)), io.Discard, &stderr)
					if ctx.Err() != nil {
						t.Fatalf("the call did not end; stderr:\n%s", stderr.String())
					}
					if err != nil || code != 128 {
						t.Errorf("exit %d, err %v; want 128", code, err)
					}
					if got := strings.TrimSpace(stderr.String()); strings.Count(got, "\n") != 0 || !strings.HasPrefix(got, "zcp git: ") || !strings.Contains(got, "stdin") {
						t.Errorf("stderr = %q, want one zcp git line about the stdin", stderr.String())
					}
					if after := l.git(t, checkout, "rev-parse", "HEAD"); after != before {
						t.Errorf("git ran: HEAD moved to %s", after)
					}
				})
			}
		})
	}
}

// TestBrokeredGit_Relay_StdinStep_FallsBackToDdWithoutHeadC: where `head`
// takes no -c (a busybox build option), the payload is read with dd, still
// byte for byte. A head without -c is stood in on PATH, so a shell that runs
// its own head applet whatever PATH says is skipped.
func TestBrokeredGit_Relay_StdinStep_FallsBackToDdWithoutHeadC(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			if !shellRunsFromPath(t, sh, "head") {
				t.Skip(shellName(sh) + " runs its own head applet, which takes -c, whatever PATH says, so no head without -c can stand in; the dd fallback runs under the other shells")
			}
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			realHead, err := exec.LookPath("head")
			if err != nil {
				t.Skip("no head on PATH")
			}
			stub := t.TempDir()
			refused := filepath.Join(t.TempDir(), "head-c-refused")
			body := "#!/bin/sh\nfor a; do case \"$a\" in -c*) echo refused >> " + ShellQuote(refused) + "; echo 'head: illegal option -- c' >&2; exit 1;; esac; done\nexec " + ShellQuote(realHead) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(stub, "head"), []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			var payload strings.Builder
			payload.WriteString("subject\n\n")
			for i := 0; payload.Len() < 12<<10; i++ {
				fmt.Fprintf(&payload, "body line %04d with 'quotes' and $x\n", i)
			}
			l.runEnv(t, sh, l.env(stub), checkout, l.relay(), payload.String(), "commit", "--allow-empty", "--cleanup=verbatim", "-F", "-").mustSucceed(t, "commit -F - via dd")
			if _, err := os.Stat(refused); err != nil {
				t.Fatal("the stub head was never asked for -c: the fallback was not exercised")
			}
			_, message, _ := strings.Cut(l.git(t, checkout, "cat-file", "commit", "HEAD"), "\n\n")
			if message != strings.TrimSpace(payload.String()) {
				t.Errorf("commit message differs from the payload (%d bytes vs %d)", len(message), len(payload.String()))
			}
		})
	}
}

// --- offline: nothing outlives the call -------------------------------------

// looseObjectsUnder17 writes n loose blobs whose names start with 17 — the
// directory `gc --auto` samples — so gc.auto=1 makes git want to pack.
func (l *relayLab) looseObjectsUnder17(t *testing.T, checkout string, n int) {
	t.Helper()
	dir := t.TempDir()
	for i, found := 0, 0; found < n; i++ {
		content := fmt.Sprintf("zcp gc probe %d\n", i)
		sum := sha1.Sum(fmt.Appendf(nil, "blob %d\x00%s", len(content), content)) //nolint:gosec // G401: git's own object naming
		if sum[0] != 0x17 {
			continue
		}
		file := filepath.Join(dir, fmt.Sprintf("probe-%d", i))
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := l.git(t, checkout, "hash-object", "-w", file); !strings.HasPrefix(got, "17") {
			t.Fatalf("hash-object = %s, want a name under 17/", got)
		}
		found++
	}
}

// TestBrokeredGit_Relay_AutoGC_RunsInTheForeground_HoldsNoSession: git's
// detached auto-gc (and auto-maintenance) closes only descriptors 0-2, so it
// would keep 3 and 4 — the session's stdin and stderr — and hold the call
// open until it finished; a relayed call runs it in the foreground, whatever
// the checkout's config says, and ends when git ends. The checkout enables
// maintenance's gc task, which newer git's default auto maintenance leaves
// out (2.56 runs neither gc --auto nor the pre-auto-gc hook without it), so
// the auto-gc runs on every git version.
func TestBrokeredGit_Relay_AutoGC_RunsInTheForeground_HoldsNoSession(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			for _, kv := range [][2]string{{"gc.auto", "1"}, {"gc.autoDetach", "true"}, {"maintenance.autoDetach", "true"}, {"maintenance.gc.enabled", "true"}} {
				l.git(t, checkout, "config", kv[0], kv[1])
			}
			l.looseObjectsUnder17(t, checkout, 3)
			seen := filepath.Join(t.TempDir(), "detach")
			hook := filepath.Join(checkout, ".git", "hooks", "pre-auto-gc")
			body := "#!/bin/sh\necho \"gc=$(git config --get gc.autoDetach) maintenance=$(git config --get maintenance.autoDetach)\" >> " + ShellQuote(seen) + "\n"
			if err := os.WriteFile(hook, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			run := l.run(t, sh, checkout, l.relay(), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			for line := range strings.SplitSeq(strings.TrimSpace(readFile(t, seen)), "\n") {
				if line != "gc=false maintenance=false" {
					t.Errorf("the auto-gc saw %q, want detaching off for both", line)
				}
			}
			if !strings.Contains(run.stderr, "Auto packing the repository for optimum performance") || strings.Contains(run.stderr, "in background") {
				t.Errorf("stderr = %q, want git's auto-gc run in the foreground", run.stderr)
			}
		})
	}
}

// --- offline: the call ends when git does -----------------------------------

// heldDescriptorHook starts a holder of the session descriptors, and waits
// until it started. The holder cannot finish until the session is stopped.
func heldDescriptorHook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ready, hold := filepath.Join(dir, "ready"), filepath.Join(dir, "hold")
	for _, path := range []string{ready, hold} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return "(printf 'ready\\n' > " + ShellQuote(ready) + "; read -r ignored < " + ShellQuote(hold) + ") >/dev/null 2>&1 </dev/null &\n" +
		"read -r ignored < " + ShellQuote(ready) + "\n"
}

// TestBrokeredGit_Relay_DoneMarker_DetachedHolderDoesNotHoldTheCall: a hook
// leaving a process in the background that holds the session's descriptors
// no longer holds the call: it returns once git ended with git's own exit
// code — 0, 1 or 128 — from the script's marker, though zcp ends the session.
func TestBrokeredGit_Relay_DoneMarker_DetachedHolderDoesNotHoldTheCall(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name string
				hook string
				code int
			}{
				{"git succeeds", "exit 0\n", 0},
				{"the hook refuses", "exit 1\n", 1},
				{"git dies after the hook", ": > .git/HEAD.lock\nexit 0\n", 128},
			} {
				l := newRelayLab(t)
				url, _ := l.repo(t, false, "org/app-1/appdev")
				checkout := l.checkout(t, url)
				writePreCommitHook(t, checkout, heldDescriptorHook(t)+tc.hook)
				run := l.run(t, sh, checkout, l.relay(), "", "commit", "--allow-empty", "-m", "x")
				if run.err != nil || run.code != tc.code {
					t.Errorf("%s: exit %d, err %v; want git's %d\n%s", tc.name, run.code, run.err, tc.code, run.stderr)
				}
				if strings.Contains(run.stderr, "ZCPDONE") {
					t.Errorf("%s: the marker reached the caller: %q", tc.name, run.stderr)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_DoneMarker_BackgroundFrameAfterGitGetsNothing: a
// process a hook leaves behind that asks for a credential after git ended
// gets none — zcp answers nothing once the marker came — and its helper
// does not keep the lock.
func TestBrokeredGit_Relay_DoneMarker_BackgroundFrameAfterGitGetsNothing(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			dir := t.TempDir()
			ready, gate, done, out := filepath.Join(dir, "ready"), filepath.Join(dir, "gate"), filepath.Join(dir, "done"), filepath.Join(dir, "out")
			for _, path := range []string{ready, gate, done} {
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			gateFile, err := os.OpenFile(gate, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer gateFile.Close()
			doneFile, err := os.OpenFile(done, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer doneFile.Close()
			hook := "(printf 'ready\\n' > " + ShellQuote(ready) + "; read -r ignored < " + ShellQuote(gate) +
				"; git show " + h.first + ":app.txt > " + ShellQuote(out) + " 2>/dev/null; printf '%s\\n' \"$?\" > " + ShellQuote(done) +
				") >/dev/null 2>&1 </dev/null &\nread -r ignored < " + ShellQuote(ready) + "\n"
			writePreCommitHook(t, checkout, hook)
			parsed, err := ParseBrokeredGitArgs([]string{"commit", "--allow-empty", "-m", "x"})
			if err != nil {
				t.Fatal(err)
			}
			runner := &windowRunner{session: &shellRunner{sh: sh, env: l.env()}, marked: make(chan struct{})}
			stdout := &heldReader{started: make(chan struct{}), release: make(chan struct{})}
			var stderr bytes.Buffer
			ctx, cancel := context.WithCancel(t.Context())
			var code int
			var runErr error
			finished := make(chan struct{})
			var release sync.Once
			free := func() { release.Do(func() { close(stdout.release) }) }
			t.Cleanup(func() { free(); cancel(); <-finished })
			go func() {
				code, runErr = RunBrokeredGitRelayed(ctx, runner, "appdev", checkout, parsed, l.relay(), nil, stdout, &stderr)
				close(finished)
			}()
			select {
			case <-stdout.started:
			case <-finished:
				t.Fatalf("call ended before git's output was held: exit %d, err %v", code, runErr)
			}
			select {
			case <-runner.marked:
			case <-finished:
				t.Fatalf("call ended before git's done frame: exit %d, err %v", code, runErr)
			}
			// EOF proves the done frame was processed and answers closed before
			// the held background process asks for its credential.
			if _, err := io.Copy(io.Discard, runner.answers); err != nil {
				t.Fatalf("read answer closure: %v", err)
			}
			if _, err := io.WriteString(gateFile, "go\n"); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(doneFile).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			exit, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || exit == 0 {
				t.Errorf("background lazy fetch exit %q: it must not receive a credential", strings.TrimSpace(line))
			}
			if b, _ := os.ReadFile(out); len(b) > 0 {
				t.Errorf("the background process read %q", b)
			}
			commands := runner.session.seen()
			lock := regexp.MustCompile(`/tmp/zcp-relay-[a-f0-9]+\.lock`).FindString(commands[0])
			if lock == "" {
				t.Fatal("call's relay lock path missing")
			}
			if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("background helper still holds %s: %v", lock, err)
			}
			free()
			<-finished
			if runErr != nil || code != 0 {
				t.Errorf("commit exit %d, err %v; want git's 0: %s", code, runErr, stderr.String())
			}
		})
	}
}

// heldRelayExit records whether revocation was received before the session
// ended, then holds its return until the test has inspected that ordering.
type heldRelayExit struct {
	session  *shellRunner
	revoked  <-chan struct{}
	finished chan bool
	release  <-chan struct{}
}

func (r *heldRelayExit) ExecSSHStdio(ctx context.Context, host, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	code, err := r.session.ExecSSHStdio(ctx, host, command, stdin, stdout, stderr)
	before := false
	select {
	case <-r.revoked:
		before = true
	default:
	}
	r.finished <- before
	<-r.release
	return code, err
}

// TestBrokeredGit_Relay_DoneMarker_RevokesRightAfterTheMarker: the call's
// tokens are revoked as soon as git ended — not when a process the hook
// left behind finally lets the session close.
func TestBrokeredGit_Relay_DoneMarker_RevokesRightAfterTheMarker(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt >/dev/null\n"+heldDescriptorHook(t))
			l.revokeDone = make(chan struct{}, 1)
			release := make(chan struct{})
			runner := &heldRelayExit{session: &shellRunner{sh: sh, env: l.env()}, revoked: l.revokeDone, finished: make(chan bool, 1), release: release}
			parsed, err := ParseBrokeredGitArgs([]string{"commit", "--allow-empty", "-m", "x"})
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				code int
				err  error
			}
			ended := make(chan result, 1)
			var stdout, stderr bytes.Buffer
			go func() {
				code, err := RunBrokeredGitRelayed(t.Context(), runner, "appdev", checkout, parsed, l.relay(url), nil, &stdout, &stderr)
				ended <- result{code, err}
			}()
			beforeSessionEnd := <-runner.finished
			close(release)
			got := <-ended
			if got.err != nil || got.code != 0 {
				t.Fatalf("exit %d, err %v; want git's 0\n%s", got.code, got.err, stderr.String())
			}
			if !beforeSessionEnd {
				t.Error("the token was not revoked before the held session ended")
			}
			if want := l.mintedTokens(); len(want) != 1 || !slices.Equal(l.revokes(), want) {
				t.Fatalf("revoked %q, want the minted %q", l.revokes(), want)
			}
		})
	}
}

// windowBuffer is an ssh client's channel buffer: it takes in whatever the
// session writes at once and hands it out in chunks.
type windowBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	data   []byte
	closed bool
	stop   bool
}

func newWindowBuffer() *windowBuffer {
	b := &windowBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *windowBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	b.cond.Broadcast()
	return len(p), nil
}

// next is the next chunk, at most 32 KiB, waiting for one; nil once the
// buffer is closed and empty, or stopped.
func (b *windowBuffer) next() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.data) == 0 && !b.closed && !b.stop {
		b.cond.Wait()
	}
	if b.stop || len(b.data) == 0 {
		return nil
	}
	n := min(len(b.data), 32<<10)
	chunk := slices.Clone(b.data[:n])
	b.data = b.data[n:]
	return chunk
}

func (b *windowBuffer) end(stop bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.stop = b.stop || stop
	b.cond.Broadcast()
}

// windowRunner stands in for an ssh client over the shellRunner's session:
// what the remote side writes to stdout it takes in at once, into a buffer
// of its own — an ssh client reads a channel's data as far as its window
// allows, ahead of the caller — and hands it to the caller's stdout at the
// caller's pace. A call that ends by itself returns once all of it reached
// the caller, as ssh writes out what it holds before it exits; a call its
// context stops loses what the buffer still holds, as a killed ssh client
// does.
type windowRunner struct {
	session *shellRunner
	marked  chan struct{}
	answers io.Reader
}

func (r *windowRunner) ExecSSHStdio(ctx context.Context, host, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	r.answers = stdin
	buf := newWindowBuffer()
	forwarded := make(chan struct{})
	go func() {
		defer close(forwarded)
		for chunk := buf.next(); chunk != nil; chunk = buf.next() {
			if _, err := stdout.Write(chunk); err != nil {
				return
			}
		}
	}()
	code, err := r.session.ExecSSHStdio(ctx, host, command, stdin, buf, &doneFrameReceipt{dst: stderr, marked: r.marked})
	buf.end(false)
	select {
	case <-forwarded:
		return code, err
	case <-ctx.Done():
		buf.end(true)
		<-forwarded
		return -1, ctx.Err()
	}
}

// doneFrameReceipt signals once the session emitted git's done frame.
type doneFrameReceipt struct {
	dst    io.Writer
	marked chan struct{}
	tail   []byte
	once   sync.Once
}

func (w *doneFrameReceipt) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.tail = append(w.tail, p[:n]...)
	if bytes.Contains(w.tail, []byte("ZCPDONE-")) {
		w.once.Do(func() { close(w.marked) })
	}
	if len(w.tail) > len("ZCPDONE-") {
		w.tail = slices.Clone(w.tail[len(w.tail)-len("ZCPDONE-"):])
	}
	return n, err
}

// heldReader does not accept its first write until the test releases it.
type heldReader struct {
	started, release chan struct{}
	once             sync.Once
	mu               sync.Mutex
	got              bytes.Buffer
}

func (w *heldReader) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started); <-w.release })
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.got.Write(p)
}

func (w *heldReader) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.got.Bytes())
}

// TestEndAfterGrace_OutputHeld_GraceStartsAfterItEnds: elapsed grace before
// the caller received git's output must never end the session.
func TestEndAfterGrace_OutputHeld_GraceStartsAfterItEnds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		gitEnded, outputEnded, sshEnded := make(chan struct{}), make(chan struct{}), make(chan struct{})
		stopped := make(chan struct{})
		go endAfterGrace(gitEnded, outputEnded, sshEnded, func() { close(stopped) })
		close(gitEnded)
		synctest.Wait()
		time.Sleep(2 * relayDoneGrace)
		select {
		case <-stopped:
			t.Fatal("the session ended while git's output was still held")
		default:
		}
		close(outputEnded)
		time.Sleep(relayDoneGrace)
		<-stopped
	})
}

// TestBrokeredGit_Relay_DoneMarker_SlowReaderGetsEveryByte: git's output the
// ssh client still holds when the done marker arrives — the caller reading
// slowly — all reaches the caller, and the exit code is git's.
func TestBrokeredGit_Relay_DoneMarker_SlowReaderGetsEveryByte(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			var want strings.Builder
			for i := range 300_000 {
				fmt.Fprintf(&want, "line %08d\n", i)
			}
			l.commitFiles(t, checkout, "big", map[string]string{"big.txt": want.String()})
			parsed, err := ParseBrokeredGitArgs([]string{"cat-file", "-p", "HEAD:big.txt"})
			if err != nil {
				t.Fatal(err)
			}
			runner := &windowRunner{session: &shellRunner{sh: sh, env: l.env()}, marked: make(chan struct{})}
			stdout := &heldReader{started: make(chan struct{}), release: make(chan struct{})}
			var stderr bytes.Buffer
			var code int
			var runErr error
			finished := make(chan struct{})
			var release sync.Once
			free := func() { release.Do(func() { close(stdout.release) }) }
			t.Cleanup(func() { free(); <-finished })
			go func() {
				code, runErr = RunBrokeredGitRelayed(t.Context(), runner, "appdev", checkout, parsed, l.relay(), strings.NewReader(""), stdout, &stderr)
				close(finished)
			}()
			select {
			case <-stdout.started:
			case <-finished:
				t.Fatalf("call ended before the reader was held: exit %d, err %v", code, runErr)
			}
			select {
			case <-runner.marked:
			case <-finished:
				t.Fatalf("call ended before git's done frame: exit %d, err %v", code, runErr)
			}
			free()
			<-finished
			if runErr != nil || code != 0 {
				t.Errorf("exit %d, err %v; want git's 0\n%s", code, runErr, stderr.String())
			}
			if got := stdout.bytes(); !bytes.Equal(got, []byte(want.String())) {
				t.Errorf("the caller got %d of git's %d bytes", len(got), want.Len())
			}
		})
	}
}

// holderOutput observes the end mark and the holder's later write before
// forwarding the done frame; this lets the test control their order.
type holderOutput struct {
	dst        io.Writer
	seen       bytes.Buffer
	mark, late chan struct{}
	markOnce   sync.Once
	lateOnce   sync.Once
}

func (w *holderOutput) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	_, _ = w.seen.Write(p[:n])
	if strings.Contains(w.seen.String(), "ZCPEND-") {
		w.markOnce.Do(func() { close(w.mark) })
	}
	if strings.Contains(w.seen.String(), "late-from-the-holder\n") {
		w.lateOnce.Do(func() { close(w.late) })
	}
	return n, err
}

// holderStderr holds the done frame until the holder's late stdout has
// reached the relay. This call uses a local alias and has no credential frames.
type holderStderr struct {
	dst  io.Writer
	late <-chan struct{}
	done <-chan struct{}
}

func (w *holderStderr) Write(p []byte) (int, error) {
	select {
	case <-w.late:
		return w.dst.Write(p)
	case <-w.done:
		return 0, context.Canceled
	}
}

type holderRunner struct {
	session    *shellRunner
	mark, late chan struct{}
}

func (r *holderRunner) ExecSSHStdio(ctx context.Context, host, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return r.session.ExecSSHStdio(ctx, host, command, stdin,
		&holderOutput{dst: stdout, mark: r.mark, late: r.late},
		&holderStderr{dst: stderr, late: r.late, done: ctx.Done()})
}

// TestBrokeredGit_Relay_DoneMarker_StdoutHolderBoundedByTheGrace: a process
// git started that keeps the session's stdout open too — a shell alias of the
// checkout's config leaving one in the background — holds the call no longer
// than the grace after git's output reached the caller, and what it writes
// to stdout after git ended is not git's output: it never reaches the caller.
func TestBrokeredGit_Relay_DoneMarker_StdoutHolderBoundedByTheGrace(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			dir := t.TempDir()
			releasePath, holdPath := filepath.Join(dir, "release"), filepath.Join(dir, "hold")
			for _, path := range []string{releasePath, holdPath} {
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			release, err := os.OpenFile(releasePath, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release.Close()
			hold, err := os.OpenFile(holdPath, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Close()
			l.git(t, checkout, "config", "alias.hold", "!(read -r ignored < "+ShellQuote(releasePath)+"; printf 'late-from-the-holder\\n'; read -r ignored < "+ShellQuote(holdPath)+") 2>/dev/null </dev/null & printf 'own-output\\n'")
			parsed, err := ParseBrokeredGitArgs([]string{"hold"})
			if err != nil {
				t.Fatal(err)
			}
			runner := &holderRunner{session: &shellRunner{sh: sh, env: l.env()}, mark: make(chan struct{}), late: make(chan struct{})}
			type result struct {
				code int
				err  error
			}
			ended := make(chan result, 1)
			var stdout, stderr bytes.Buffer
			go func() {
				code, err := RunBrokeredGitRelayed(t.Context(), runner, "appdev", checkout, parsed, l.relay(), nil, &stdout, &stderr)
				ended <- result{code, err}
			}()
			select {
			case <-runner.mark:
			case got := <-ended:
				t.Fatalf("the call ended before its stdout mark: exit %d, err %v\n%s", got.code, got.err, stderr.String())
			}
			if _, err := fmt.Fprintln(release, "go"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-runner.late:
			case got := <-ended:
				t.Fatalf("the call ended before the held process wrote: exit %d, err %v\n%s", got.code, got.err, stderr.String())
			}
			got := <-ended // the holder cannot exit on its own until hold is released
			if got.err != nil || got.code != 0 {
				t.Errorf("exit %d, err %v; want git's 0\n%s", got.code, got.err, stderr.String())
			}
			if stdout.String() != "own-output\n" {
				t.Errorf("stdout = %q, want git's own output alone", stdout.String())
			}
		})
	}
}

// closedStdout is a caller's stdout whose reader is gone.
type closedStdout struct{}

func (closedStdout) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestBrokeredGit_Relay_CallerStdoutGone_EndsWithGitsExitCode: a caller whose
// stdout reader went away (`zcp git … | head -1` after head exited) holds
// nothing: zcp's copy ends and closes its pipe, and the call ends with its
// tokens revoked as ever. The stand-in ssh here outlives the closed pipe, so
// the session ends by itself and the call returns git's exit code from the
// marker. A real ssh keeps SIGPIPE's default (zcp git catches it, never
// ignores it) and dies writing to the closed pipe, taking the remote git
// with it; the call then returns git's code only when the marker came
// first, else ssh's failure.
func TestBrokeredGit_Relay_CallerStdoutGone_EndsWithGitsExitCode(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			parsed, err := ParseBrokeredGitArgs([]string{"show", h.first + ":app.txt"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			var stderr bytes.Buffer
			code, err := RunBrokeredGitRelayed(ctx, &shellRunner{sh: sh, env: l.env()}, "appdev", checkout, parsed, l.relay(url), strings.NewReader(""), closedStdout{}, &stderr)
			if ctx.Err() != nil {
				t.Fatalf("the call did not end; stderr:\n%s", stderr.String())
			}
			if err != nil || code != 0 {
				t.Errorf("exit %d, err %v; want git's 0\n%s", code, err, stderr.String())
			}
			if want := l.mintedTokens(); len(want) != 1 || !slices.Equal(l.revokes(), want) {
				t.Errorf("revoked %q, want the minted %q", l.revokes(), want)
			}
		})
	}
}

// --- offline: tokens revoked when the call ends -----------------------------

// TestBrokeredGit_Relay_RevokesEachCachedTokenOnce: when a call ends, each
// GitHub token it minted is revoked once — however many requests it
// answered — and the project key HQ is answered with is never revoked.
func TestBrokeredGit_Relay_RevokesEachCachedTokenOnce(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, _ := l.repo(t, true, "acme/shop")
			lib, _ := l.repo(t, true, "acme/lib")
			hqURL, _ := l.repo(t, false, "org/app-1/appdev")
			checkout := l.checkout(t, url)
			l.git(t, checkout, "remote", "add", "hq", hqURL)

			l.run(t, sh, checkout, l.relay(url), "", "log", "-p", "--format=%H").mustSucceed(t, "log -p")
			l.mu.Lock()
			used := l.tokenUse["ghs_FAKE0001"]
			l.mu.Unlock()
			if used < 2 {
				t.Fatalf("the token answered %d requests, want several lazy fetches", used)
			}
			if got := l.revokes(); !slices.Equal(got, []string{"ghs_FAKE0001"}) {
				t.Errorf("revoked %q after one mint, want the one token once", got)
			}

			writePreCommitHook(t, checkout, "git ls-remote "+ShellQuote(url)+" >/dev/null && git ls-remote "+ShellQuote(lib)+" >/dev/null && git ls-remote "+ShellQuote(url)+" >/dev/null\n")
			l.run(t, sh, checkout, l.relay(url, lib), "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit reading two repositories")
			got := l.revokes()
			slices.Sort(got)
			if want := l.mintedTokens(); len(want) != 3 || !slices.Equal(got, want) {
				t.Errorf("revoked %q, want each minted token %q once", got, want)
			}

			before := len(l.revokes())
			l.run(t, sh, checkout, l.relay(url), "", "ls-remote", "hq").mustSucceed(t, "ls-remote hq")
			if after := l.revokes(); len(after) != before {
				t.Errorf("an HQ-only call revoked %q", after[before:])
			}
		})
	}
}

// TestBrokeredGit_Relay_CancelledCall_StillRevokesOnDetachedContext: a call
// stopped — its context cancelled while git still runs — still revokes the
// token it minted, on a context of its own.
func TestBrokeredGit_Relay_CancelledCall_StillRevokesOnDetachedContext(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			url, h := l.repo(t, true, "acme/shop")
			checkout := l.checkout(t, url)
			hold := filepath.Join(t.TempDir(), "hold")
			if err := syscall.Mkfifo(hold, 0o600); err != nil {
				t.Fatal(err)
			}
			const fetched = "zcp-test-fetched"
			writePreCommitHook(t, checkout, "git show "+h.first+":app.txt >/dev/null || exit $?\nprintf '"+fetched+"\\n' >&2\nread -r ignored < "+ShellQuote(hold)+"\n")
			parsed, err := ParseBrokeredGitArgs([]string{"commit", "--allow-empty", "-m", "x"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			l.revokeDone = make(chan struct{}, 1)
			stderrReader, stderrWriter := io.Pipe()
			var stdout, stderr bytes.Buffer
			scanned := make(chan error, 1)
			go func() {
				scanner := bufio.NewScanner(stderrReader)
				for scanner.Scan() {
					line := scanner.Text()
					fmt.Fprintln(&stderr, line)
					if line == fetched {
						cancel()
					}
				}
				scanned <- scanner.Err()
			}()
			ended := make(chan error, 1)
			go func() {
				_, runErr := RunBrokeredGitRelayed(ctx, &shellRunner{sh: sh, env: l.env()}, "appdev", checkout, parsed, l.relay(url), nil, &stdout, stderrWriter)
				_ = stderrWriter.Close()
				ended <- runErr
			}()
			revoked := false
			select {
			case <-l.revokeDone:
				revoked = true
				err = <-ended
			case err = <-ended:
				select {
				case <-l.revokeDone:
					revoked = true
				default:
				}
			}
			if scanErr := <-scanned; scanErr != nil {
				t.Fatalf("read stderr: %v", scanErr)
			}
			if ctx.Err() == nil {
				t.Fatalf("the call ended before it was stopped: %v\n%s", err, stderr.String())
			}
			if err == nil {
				t.Error("a stopped call answered no error")
			}
			if !revoked {
				t.Error("the minted token was not revoked")
			}
			if got, want := l.revokes(), l.mintedTokens(); len(want) != 1 || !slices.Equal(got, want) {
				t.Errorf("revoked %q after the call was stopped, want the minted %q", got, want)
			}
		})
	}
}

// TestBrokeredGit_Relay_RevokeFailure_KeepsExitCode_NoTokenPrinted: a revoke
// GitHub refuses or never answers is best effort — git's exit code stands,
// the call ends within the revoke's 5 s bound, and one stderr line says so
// without the token.
func TestBrokeredGit_Relay_RevokeFailure_KeepsExitCode_NoTokenPrinted(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name   string
				status int
				wait   bool
				args   []string
				code   int
			}{
				{"refused, git succeeding", http.StatusInternalServerError, false, []string{"log", "-p", "--format=%H"}, 0},
				{"refused, git failing", http.StatusForbidden, false, []string{"fetch", "origin", "refs/heads/no-such-branch"}, 128},
				{"never answered", 0, true, []string{"log", "-p", "--format=%H"}, 0},
			} {
				l := newRelayLab(t)
				url, _ := l.repo(t, true, "acme/shop")
				checkout := l.checkout(t, url)
				l.mu.Lock()
				l.revokeStatus = tc.status
				if tc.wait {
					l.revokeWait = make(chan struct{})
				}
				l.mu.Unlock()
				deadlines := make(chan time.Duration, 1)
				expire := make(chan context.CancelFunc, 1)
				relay := l.relay(url)
				relay.revokeContext = func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
					deadline := &deadlineOnWait{Context: ctx, waiting: make(chan struct{}), done: make(chan struct{})}
					var once sync.Once
					cancel := func() { once.Do(func() { close(deadline.done) }) }
					deadlines <- d
					expire <- cancel
					return deadline, cancel
				}
				if tc.wait {
					go func() {
						select {
						case <-l.revokeWait:
							(<-expire)()
						case <-t.Context().Done():
						}
					}()
				}
				run := l.run(t, sh, checkout, relay, "", tc.args...)
				if run.err != nil || run.code != tc.code {
					t.Errorf("%s: exit %d, err %v; want git's %d\n%s", tc.name, run.code, run.err, tc.code, run.stderr)
				}
				if len(l.mintedTokens()) != 1 || len(l.revokes()) != 1 {
					t.Fatalf("%s: minted %q, revoked %q; want one of each", tc.name, l.mintedTokens(), l.revokes())
				}
				var said []string
				for line := range strings.SplitSeq(run.stderr, "\n") {
					if strings.Contains(line, "revoke") {
						said = append(said, line)
					}
				}
				if len(said) != 1 || !strings.HasPrefix(said[0], "zcp git: ") {
					t.Errorf("%s: stderr lines about the revoke = %q, want one zcp git line", tc.name, said)
				}
				if strings.Contains(run.stderr, "ghs_FAKE") {
					t.Errorf("%s: stderr carries the token: %q", tc.name, run.stderr)
				}
				if got := <-deadlines; got != 5*time.Second {
					t.Errorf("%s: revoke deadline = %s, want 5s", tc.name, got)
				}
			}
		})
	}
}

// countingRunner is an ssh seam that only counts the calls it was asked for.
type countingRunner struct{ calls atomic.Int32 }

func (c *countingRunner) ExecSSHStdio(context.Context, string, string, io.Reader, io.Writer, io.Writer) (int, error) {
	c.calls.Add(1)
	return 0, nil
}

// observedReader signals when a read starts and when that held read ends.
type observedReader struct {
	io.Reader
	started, finished chan struct{}
	once              sync.Once
}

func (r *observedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	n, err := r.Reader.Read(p)
	close(r.finished)
	return n, err
}

// TestBrokeredGit_Relay_StdinNeverClosing_StoppedByCancel: a call that reads
// stdin — apply with no file — on a stdin that never ends is stopped by its
// context at once, before anything runs in the service.
func TestBrokeredGit_Relay_StdinNeverClosing_StoppedByCancel(t *testing.T) {
	t.Parallel()
	stdin, never := io.Pipe()
	t.Cleanup(func() { _ = never.Close() })
	reader := &observedReader{Reader: stdin, started: make(chan struct{}), finished: make(chan struct{})}
	runner := &countingRunner{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := RunBrokeredGitRelayed(ctx, runner, "appdev", "/var/www", BrokeredGitArgs{Subcommand: "apply"}, BrokeredGitRelay{}, reader, io.Discard, io.Discard)
		done <- err
	}()
	<-reader.started
	cancel()
	err := <-done
	_ = never.Close()
	<-reader.finished
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrBrokeredGitStdinStopped) {
		t.Errorf("RunBrokeredGitRelayed = %v, want stopped while reading stdin", err)
	}
	if n := runner.calls.Load(); n != 0 {
		t.Errorf("%d ssh calls, want none", n)
	}
}

// --- offline: GitHub is answered for reads alone -----------------------------

// accesses is the access every GitHub mint so far asked for.
func (l *relayLab) accesses() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.mints))
	for i, m := range l.mints {
		out[i] = m.access
	}
	return out
}

// commitFiles writes files in dir and commits them (a fixture commit).
func (l *relayLab) commitFiles(t *testing.T, dir, message string, files map[string]string) {
	t.Helper()
	for file, body := range files {
		writeLabFile(t, filepath.Join(dir, file), body)
	}
	l.git(t, dir, "add", "-A")
	l.git(t, dir, "commit", "-q", "-m", message)
}

// assertAccesses checks every access HQ was asked for, in order.
func (l *relayLab) assertAccesses(t *testing.T, want ...string) {
	t.Helper()
	if got := l.accesses(); !slices.Equal(got, want) {
		t.Errorf("HQ was asked for access %q, want %q", got, want)
	}
}

// accessLab is a lab with a partial clone of acme/shop whose origin is the
// GitHub stand-in — a checkout an older zcp pointed there — and the relay
// its record names.
func accessLab(t *testing.T) (*relayLab, relayHistory, string, BrokeredGitRelay) {
	t.Helper()
	l := newRelayLab(t)
	url, h := l.repo(t, true, "acme/shop")
	return l, h, l.checkout(t, url), l.relay(url)
}

// TestBrokeredGit_Access_NonPushCallAsksRead: every token a call that is no
// push obtains is a read token — a lazy fetch inside checkout included.
func TestBrokeredGit_Access_NonPushCallAsksRead(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l, h, checkout, relay := accessLab(t)
			l.run(t, sh, checkout, relay, "", "checkout", "-q", h.first).mustSucceed(t, "checkout")
			l.run(t, sh, checkout, relay, "", "fetch", "origin").mustSucceed(t, "fetch")
			l.run(t, sh, checkout, relay, "", "ls-remote", "origin").mustSucceed(t, "ls-remote")
			l.assertAccesses(t, "read", "read", "read")
		})
	}
}

// TestBrokeredGit_Relay_PushToGitHub_RefusedWithoutAskingHQ: a push that
// reaches a GitHub repository the record names — by the checkout's origin, a
// URL in argv, no refspec, or a force — is refused in zcp's words before HQ
// is asked for any token, in the one ssh call the push is; nothing reaches
// GitHub. HQ alone pushes there.
//
// witness: internal/ops/git_brokered_relay.go | if c.push { → if false {
func TestBrokeredGit_Relay_PushToGitHub_RefusedWithoutAskingHQ(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l, _, checkout, relay := accessLab(t)
			url := l.gh.URL + "/acme/shop.git"
			l.git(t, checkout, "checkout", "-q", "-b", "feature")
			l.git(t, checkout, "config", "branch.feature.remote", "origin")
			l.git(t, checkout, "config", "branch.feature.merge", "refs/heads/feature")
			l.commitFiles(t, checkout, "work", map[string]string{"app.txt": "mate's work\n"})
			for _, args := range [][]string{
				{"push", "origin", "feature"},
				{"push", url, "HEAD:refs/heads/feature"},
				{"push"},
				{"push", "--force", "origin", "feature:main"},
			} {
				run := l.run(t, sh, checkout, relay, "", args...)
				if run.code == 0 || run.err != nil {
					t.Errorf("git %q: exit %d, err %v; want git failing on a declined credential", args, run.code, run.err)
				}
				for _, want := range []string{
					"zcp git: this push asked for ", "/acme/shop, which zcp git answers for reads only and never inside a push, so no token was asked for it",
					"HQ alone does", "`zcp git appdev push origin <branch>`", `action="open-change"`,
					"GitHub's default branch moves only by a merge",
					"Should origin itself still name GitHub — status's `checkoutOrigin` says so — develop's start points it at HQ",
				} {
					if !strings.Contains(run.stderr, want) {
						t.Errorf("git %q: stderr = %q, want %q in it", args, run.stderr, want)
					}
				}
				if len(run.commands) != 1 {
					t.Errorf("git %q made %d ssh calls, want the push alone", args, len(run.commands))
				}
			}
			if minted := l.minted(); len(minted) != 0 {
				t.Errorf("HQ was asked for %+v, want no token asked for a push", minted)
			}
			bare := filepath.Join(l.root, "gh", "acme/shop.git")
			if got := gitOr(t, bare, "rev-parse", "--verify", "-q", "refs/heads/feature"); got != "" {
				t.Errorf("the stand-in took a push: feature at %s", got)
			}
			l.run(t, sh, checkout, relay, "", "fetch", "origin").mustSucceed(t, "a fetch after the refused pushes")
			l.assertAccesses(t, "read")
		})
	}
}

// TestBrokeredGit_Relay_Realignment_GitHubsTipReadByAddress_PushedToHQ: the
// one change that brings a diverged GitHub and HQ back in step, as zcp's
// words give it (tools.divergedWords), in a checkout whose origin is HQ:
// GitHub's default branch fetched by its address — the one GitHub token of
// it all, a read token — HQ's `main` fetched by HQ's address, a branch from
// GitHub's tip with HQ's merged in, pushed to HQ. Nothing is pushed to
// GitHub.
//
// witness: internal/ops/git_brokered_relay.go | call := newRelayCall(relay, host, args.Subcommand == "push", stderr) → call := newRelayCall(relay, host, true, stderr)
func TestBrokeredGit_Relay_Realignment_GitHubsTipReadByAddress_PushedToHQ(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l := newRelayLab(t)
			hqURL, _ := l.repo(t, false, "org/app-1/appdev")
			hqBare := filepath.Join(l.root, "hq", "org/app-1/appdev.git")
			ghBare := filepath.Join(l.root, "gh", "acme/shop.git")
			if err := os.MkdirAll(filepath.Dir(ghBare), 0o755); err != nil {
				t.Fatal(err)
			}
			l.git(t, l.root, "clone", "-q", "--bare", hqBare, ghBare)
			l.git(t, ghBare, "config", "http.receivepack", "true")
			ghURL := l.gh.URL + "/acme/shop.git"
			checkout := l.checkout(t, hqURL)
			// Each side takes a commit the other lacks.
			tips := map[string]string{}
			for bare, file := range map[string]string{ghBare: "on-github.txt", hqBare: "in-hq.txt"} {
				work := filepath.Join(t.TempDir(), "w")
				l.git(t, filepath.Dir(work), "clone", "-q", bare, work)
				l.commitFiles(t, work, "only here", map[string]string{file: "diverged\n"})
				l.git(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")
				tips[bare] = l.git(t, work, "rev-parse", "HEAD")
			}
			relay := l.relay(ghURL)

			for _, args := range [][]string{
				{"fetch", ghURL, "+HEAD:refs/remotes/github/default"},
				{"fetch", hqURL, "+main:refs/remotes/hq/main"},
				{"checkout", "-q", "-b", "realign-github", "refs/remotes/github/default"},
				{"merge", "-q", "--no-edit", "refs/remotes/hq/main"},
				{"push", hqURL, "realign-github"},
			} {
				l.run(t, sh, checkout, relay, "", args...).mustSucceed(t, strings.Join(args, " "))
			}

			if got := l.git(t, checkout, "rev-parse", "refs/remotes/github/default"); got != tips[ghBare] {
				t.Errorf("GitHub's default branch was fetched as %s, want its tip %s", got, tips[ghBare])
			}
			// Both tips are in what HQ took: the fixture git fails the test otherwise.
			for _, tip := range tips {
				l.git(t, hqBare, "merge-base", "--is-ancestor", tip, "refs/heads/realign-github")
			}
			if got := l.git(t, hqBare, "rev-parse", "refs/heads/realign-github"); got != l.git(t, checkout, "rev-parse", "HEAD") {
				t.Errorf("HQ's realign-github = %s, want the pushed branch", got)
			}
			if got := gitOr(t, ghBare, "rev-parse", "--verify", "-q", "refs/heads/realign-github"); got != "" {
				t.Errorf("the branch reached GitHub from the container: %s", got)
			}
			l.assertAccesses(t, "read")
		})
	}
}

// TestBrokeredGit_Access_PushInsideNonPushCall_GetsReadAndFails: a push a
// hook starts inside a call that is no push gets the call's read token, and
// GitHub refuses it.
func TestBrokeredGit_Access_PushInsideNonPushCall_GetsReadAndFails(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			l, _, checkout, relay := accessLab(t)
			result := filepath.Join(t.TempDir(), "auto-push")
			hook := filepath.Join(checkout, ".git", "hooks", "post-commit")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\ngit push origin HEAD:refs/heads/auto > "+ShellQuote(result)+" 2>&1; echo \"rc=$?\" >> "+ShellQuote(result)+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			l.run(t, sh, checkout, relay, "", "commit", "--allow-empty", "-m", "x").mustSucceed(t, "commit")
			if got := readFile(t, result); strings.Contains(got, "rc=0") {
				t.Errorf("the hook's push succeeded with a read token: %q", got)
			}
			if got := gitOr(t, filepath.Join(l.root, "gh", "acme/shop.git"), "rev-parse", "--verify", "-q", "refs/heads/auto"); got != "" {
				t.Errorf("the stand-in took the hook's push: auto at %s", got)
			}
			for _, a := range l.accesses() {
				if a != "read" {
					t.Errorf("HQ was asked for %q inside a commit, want read only", a)
				}
			}
		})
	}
}

// --- offline: the helper alone, under each shell ----------------------------

func relayRandHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// helperDefaultInterrupt gives every helper SIGINT's default disposition,
// as git has it under sshd. A test binary a non-interactive shell started in
// the background inherits SIGINT ignored, so would every helper it starts,
// and a shell cannot trap a signal ignored at its entry: the helper would
// hand git the answer after SIGINT. A signal the binary catches is default
// again in a child it execs.
var helperDefaultInterrupt sync.Once

func catchInheritedIgnoredInterrupt() {
	if signal.Ignored(syscall.SIGINT) {
		signal.Notify(make(chan os.Signal, 1), syscall.SIGINT)
	}
}

// helperProc is the relay helper running as git runs it — `<helper> get`
// in sh — its request on stdin, answers on descriptor 3, frames on 4.
type helperProc struct {
	cmd    *exec.Cmd
	stdout bytes.Buffer
	fd3    *os.File // the test's end: answers
	frames chan string
	done   chan error
}

// startHelper starts the helper of nonce under sh with env, asking for
// helperRequest.
func startHelper(t *testing.T, sh []string, nonce string, env []string) *helperProc {
	t.Helper()
	helperDefaultInterrupt.Do(catchInheritedIgnoredInterrupt)
	ansR, ansW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	frR, frW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	helper := strings.TrimPrefix(relayHelper(nonce), "!")
	p := &helperProc{fd3: ansW, frames: make(chan string, 16), done: make(chan error, 1)}
	p.cmd = exec.CommandContext(t.Context(), sh[0], append(slices.Clone(sh[1:]), "-c", helper+" get")...) //nolint:gosec // the helper under test
	p.cmd.Stdin = strings.NewReader(helperRequest)
	p.cmd.Stdout = &p.stdout
	p.cmd.Env = env
	// Descriptors 3 and 4 as the script hands them git, and their copies
	// 8 and 9 that a helper checks them against.
	p.cmd.ExtraFiles = []*os.File{ansR, frW, nil, nil, nil, ansR, frW}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = ansR.Close()
	_ = frW.Close()
	go func() {
		scanner := bufio.NewScanner(frR)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "ZCPCRED-") {
				p.frames <- line
			}
		}
		close(p.frames)
		_ = frR.Close()
	}()
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() {
		_ = ansW.Close()
		_ = p.cmd.Process.Kill()
		<-p.done
		_ = os.Remove("/tmp/zcp-relay-" + nonce + ".lock")
	})
	return p
}

// frame waits for the helper's frame and answers its id and host.
func (p *helperProc) frame(t *testing.T) (id, host string) {
	t.Helper()
	line, ok := <-p.frames
	if !ok {
		t.Fatal("the helper wrote no frame")
	}
	f := strings.SplitN(line, " ", 5)
	if len(f) < 5 {
		t.Fatalf("frame %q: want ZCPCRED-<nonce> <id> <protocol> <host> <path>", line)
	}
	return f[1], f[3]
}

// wait waits for the helper to end and answers its exit code.
func (p *helperProc) wait(t *testing.T) int {
	t.Helper()
	err := <-p.done
	p.done <- err
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}

const helperRequest = "protocol=https\nhost=hq.example\npath=git/o/a/appdev.git\n\n"

// TestBrokeredGit_Relay_MalformedFrame_ReleasesTheHelperAtOnce: the real
// helper, its frames read by relayFrames and its answers written back, asks
// for a host zcp cannot read (an IDN), an empty host, or a path that makes
// its frame overlong: zcp's decline for its id ends it at once — no
// credential, the lock free — rather than leaving it reading descriptor 3.
func TestBrokeredGit_Relay_MalformedFrame_ReleasesTheHelperAtOnce(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct{ name, request string }{
				{"unreadable host", "protocol=https\nhost=bücher.example\npath=acme/shop.git\n\n"},
				{"empty host", "protocol=https\nhost=\npath=acme/shop.git\n\n"},
				{"empty protocol", "protocol=\nhost=hq.example\npath=x\n\n"},
				{"overlong frame", "protocol=https\nhost=hq.example\npath=" + strings.Repeat("a", 70<<10) + "\n\n"},
			} {
				nonce := relayRandHex(t)
				answersR, answersW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				framesR, framesW, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				helper := strings.TrimPrefix(relayHelper(nonce), "!")
				cmd := exec.CommandContext(ctx, sh[0], append(slices.Clone(sh[1:]), "-c", helper+" get")...) //nolint:gosec // the helper under test
				var stdout bytes.Buffer
				cmd.Stdin, cmd.Stdout, cmd.Env = strings.NewReader(tc.request), &stdout, helperEnv()
				cmd.ExtraFiles = []*os.File{answersR, framesW}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				_ = answersR.Close()
				_ = framesW.Close()
				relayed := make(chan struct{})
				go func() {
					defer close(relayed)
					relayFrames(framesR, nonce, io.Discard, answersW, func(string, string, string) (string, string) { return "mate", "never-FAKE" })
				}()
				err = cmd.Wait()
				cancel()
				_ = answersW.Close()
				<-relayed
				_ = framesR.Close()
				if err != nil {
					t.Errorf("%s: helper exit %v, want 0", tc.name, err)
				}
				if stdout.Len() != 0 {
					t.Errorf("%s: the helper handed git %q", tc.name, stdout.String())
				}
				if _, err := os.Stat("/tmp/zcp-relay-" + nonce + ".lock"); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s: the lock is still held: %v", tc.name, err)
					_ = os.Remove("/tmp/zcp-relay-" + nonce + ".lock")
				}
			}
		})
	}
}

func helperEnv() []string { return []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"} }

// credAnswer is what the helper hands git for user and pass, in git's
// credential protocol.
func credAnswer(user, pass string) string {
	return "username=" + user + "\npassword=" + pass + "\n"
}

// TestBrokeredGit_Relay_StaleAnswerDiscardedWhileHoldingLock: an answer for
// another id — one a helper that gave up left — is discarded while the lock
// is held, and the helper's own answer is the one it hands git.
func TestBrokeredGit_Relay_StaleAnswerDiscardedWhileHoldingLock(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			nonce := relayRandHex(t)
			p := startHelper(t, sh, nonce, helperEnv())
			id, host := p.frame(t)
			if _, err := os.Stat("/tmp/zcp-relay-" + nonce + ".lock"); err != nil {
				t.Errorf("the helper wrote its frame without holding the lock: %v", err)
			}
			other, _ := strconv.Atoi(id)
			fmt.Fprintf(p.fd3, "%d %s stale-user stale-pass\n%s %s mate own-FAKE\n", other+1, host, id, host)
			if code := p.wait(t); code != 0 {
				t.Errorf("helper exit %d", code)
			}
			if got := p.stdout.String(); got != credAnswer("mate", "own-FAKE") {
				t.Errorf("helper answered %q, want its own answer", got)
			}
			if _, err := os.Stat("/tmp/zcp-relay-" + nonce + ".lock"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the lock is still held after the helper ended: %v", err)
			}
		})
	}
}

// TestBrokeredGit_Relay_AnswerForOtherHost_IgnoredByHelper: an answer with
// the helper's id but another host — a reused PID's — is never handed git.
func TestBrokeredGit_Relay_AnswerForOtherHost_IgnoredByHelper(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, then := range []string{"its own", "nothing"} {
				nonce := relayRandHex(t)
				p := startHelper(t, sh, nonce, helperEnv())
				id, host := p.frame(t)
				fmt.Fprintf(p.fd3, "%s other.example wrong-user wrong-FAKE\n", id)
				if then == "its own" {
					fmt.Fprintf(p.fd3, "%s %s mate right-FAKE\n", id, host)
				}
				_ = p.fd3.Close()
				if code := p.wait(t); code != 0 {
					t.Errorf("then %s: helper exit %d", then, code)
				}
				want := ""
				if then == "its own" {
					want = credAnswer("mate", "right-FAKE")
				}
				if got := p.stdout.String(); got != want {
					t.Errorf("then %s: helper answered %q, want %q", then, got, want)
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_HelperSignalled_LockRemovedExit129: a helper stopped
// while it holds the lock removes it and exits 129, never handing git the
// answer that then arrives. zcp answers every frame, so the answer arrives:
// bash leaves a signal that lands just before its `read` blocks pending until
// the read returns, and runs the trap then.
func TestBrokeredGit_Relay_HelperSignalled_LockRemovedExit129(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
				nonce := relayRandHex(t)
				p := startHelper(t, sh, nonce, helperEnv())
				id, host := p.frame(t)
				lock := "/tmp/zcp-relay-" + nonce + ".lock"
				if _, err := os.Stat(lock); err != nil {
					t.Fatalf("%s: the helper waits without holding the lock: %v", sig, err)
				}
				// Hold the process, queue the signal and the answer, then resume it.
				// The stop receipt makes signal delivery independent of scheduling.
				if err := p.cmd.Process.Signal(syscall.SIGSTOP); err != nil {
					t.Fatal(err)
				}
				var state syscall.WaitStatus
				// On BSD Go classifies the SIGSTOP status as Continued; only
				// stopped or exited children are returned with WUNTRACED.
				if _, err := syscall.Wait4(p.cmd.Process.Pid, &state, syscall.WUNTRACED, nil); err != nil || (!state.Stopped() && !state.Continued()) {
					t.Fatalf("wait for helper to stop: state %v, %v", state, err)
				}
				if err := p.cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				_, _ = fmt.Fprintf(p.fd3, "%s %s mate late-FAKE\n", id, host)
				if err := p.cmd.Process.Signal(syscall.SIGCONT); err != nil {
					t.Fatal(err)
				}
				if code := p.wait(t); code != 129 {
					t.Errorf("%s: helper exit %d, want 129", sig, code)
				}
				if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s: the lock survived the signal: %v", sig, err)
				}
				if p.stdout.Len() != 0 {
					t.Errorf("%s: helper answered %q", sig, p.stdout.String())
				}
			}
		})
	}
}

// TestBrokeredGit_Relay_LockBoundIsClockTimed: a helper waiting on another's
// lock gives up 90 s by the clock after it started — `date +%s`, however few
// times it has slept — writing no frame and leaving the lock alone. The clock
// is stood in on PATH, so a shell that runs its own date applet whatever PATH
// says is skipped rather than waited on for 90 real seconds.
func TestBrokeredGit_Relay_LockBoundIsClockTimed(t *testing.T) {
	t.Parallel()
	for _, sh := range relayShellsFor(t) {
		t.Run(shellName(sh), func(t *testing.T) {
			t.Parallel()
			if !shellRunsFromPath(t, sh, "date") {
				t.Skip(shellName(sh) + " runs its own date applet whatever PATH says, so no stub clock can stand in; the same bound is clocked under the other shells, and this shell's date +%s runs in every helper of the other relay tests")
			}
			nonce := relayRandHex(t)
			lock := "/tmp/zcp-relay-" + nonce + ".lock"
			if err := os.Mkdir(lock, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(lock) })
			stub := t.TempDir()
			count := filepath.Join(t.TempDir(), "date-calls")
			date := "#!/bin/sh\nn=$(cat " + ShellQuote(count) + " 2>/dev/null || echo 0)\nn=$((n + 1))\necho $n > " + ShellQuote(count) + "\ncase $n in 1) echo 1000;; 2) echo 1089;; *) echo 1091;; esac\n"
			if err := os.WriteFile(filepath.Join(stub, "date"), []byte(date), 0o755); err != nil {
				t.Fatal(err)
			}
			env := []string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH"), "LC_ALL=C"}
			p := startHelper(t, sh, nonce, env)
			if code := p.wait(t); code != 0 {
				t.Errorf("helper exit %d", code)
			}
			calls, _ := os.ReadFile(count)
			if got := strings.TrimSpace(string(calls)); got != "3" {
				t.Errorf("the helper read the clock %s times, want 3: start, 89 s (still waiting), 91 s (gives up)", got)
			}
			if p.stdout.Len() != 0 {
				t.Errorf("helper answered %q", p.stdout.String())
			}
			select {
			case f, ok := <-p.frames:
				if ok {
					t.Errorf("the helper wrote a frame without the lock: %q", f)
				}
			default:
			}
			if _, err := os.Stat(lock); err != nil {
				t.Errorf("the helper removed a lock it never held: %v", err)
			}
		})
	}
}
