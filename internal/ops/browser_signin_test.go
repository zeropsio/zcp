package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// A state with a cookie and local storage, as agent-browser 0.39.1 saves it.
const signedInState = `{"cookies":[{"name":"sid","value":"abc123","domain":"127.0.0.1","path":"/","expires":1795087979.0,"size":9,"httpOnly":false,"secure":false,"session":false}],` +
	`"origins":[{"origin":"http://127.0.0.1:18731","localStorage":[{"name":"token","value":"xyz789"}],"sessionStorage":[]}]}`

// readyStateDir is a readied kept sign-in folder under a home of its own.
func readyStateDir(t *testing.T) string {
	t.Helper()
	dir := browserStateDir(t.TempDir())
	if err := readyBrowserState(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeKept writes signedInState as the kept sign-in file into dir.
func writeKept(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, browserSignInFile), []byte(signedInState), 0o600); err != nil {
		t.Fatal(err)
	}
}

// keptState is the file name in dir, "" when there is none.
func keptState(t *testing.T, dir string, name ...string) string {
	t.Helper()
	file := browserSignInFile
	if len(name) > 0 {
		file = name[0]
	}
	data, err := os.ReadFile(filepath.Join(dir, file))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// savingBrowser answers a hand-back's steps as agent-browser does: the tab
// list, and a state save that writes saved to the path it is given (a
// failure when saved is ""). It records each command.
func savingBrowser(saved string, seen *[][]string) browserStepExec {
	return func(_ context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		switch {
		case slices.Equal(args, []string{"tab", "list"}):
			return `{"success":true,"data":{"tabs":[{"tabId":"t1","url":"about:blank"},{"tabId":"t2","url":"http://127.0.0.1:18731/"}]},"error":null}`, "", nil
		case len(args) == 3 && args[0] == "state" && args[1] == "save":
			if saved == "" {
				return `{"success":false,"data":null,"error":"Failed to write state"}`, "", errors.New("exit status 1")
			}
			if err := os.WriteFile(args[2], []byte(saved), 0o600); err != nil {
				return "", "", err
			}
			return `{"success":true,"data":{"saved":true,"path":"` + args[2] + `"},"error":null}`, "", nil
		}
		return `{"success":true,"data":{},"error":null}`, "", nil
	}
}

// loadingBrowser answers a load's steps: state load as load says ("ok",
// "fails", or "hangs" until the step's context ends), every other step a
// success. It records each command.
func loadingBrowser(load string, seen *[][]string) browserStepExec {
	return func(ctx context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		if len(args) > 1 && args[0] == "state" && args[1] == "load" {
			switch load {
			case "fails":
				return `{"success":false,"data":null,"error":"Invalid state file: expected value at line 1 column 1"}`, "", errors.New("exit status 1")
			case "hangs":
				<-ctx.Done()
				return "", "", ctx.Err()
			}
		}
		return `{"success":true,"data":{},"error":null}`, "", nil
	}
}

// The sign-in survives browser restarts for tab and new-browser calls; a
// fresh-context call never keeps it, and neither does a call whose home,
// its symlinks resolved, would put the saved cookies anywhere but a private
// home: none, a relative one, /, or the workspace that holds git checkouts
// and is mounted into dev services.
// witness: internal/ops/browser_signin.go | if isolation == BrowserIsolationFreshContext || !filepath.IsAbs(home) { → if !filepath.IsAbs(home) {
func TestBrowserKeepsSignIn_TabAndNewBrowserKeepFreshContextNever(t *testing.T) {
	home := t.TempDir()
	linked := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(home, linked); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		isolation, home string
		want            bool
	}{
		{BrowserIsolationTab, home, true},
		{BrowserIsolationNewBrowser, home, true},
		{BrowserIsolationTab, linked, true},
		{BrowserIsolationFreshContext, home, false},
		{BrowserIsolationTab, "", false},
		{BrowserIsolationTab, "home/zerops", false},
		{BrowserIsolationTab, "/", false},
		{BrowserIsolationTab, "/var/www", false},
		{BrowserIsolationTab, "/var/www/home", false},
	}
	for _, tt := range tests {
		if got := browserKeepsSignIn(tt.isolation, tt.home); got != tt.want {
			t.Errorf("browserKeepsSignIn(%q, %q) = %v, want %v", tt.isolation, tt.home, got, tt.want)
		}
	}
}

// The kept sign-in's folders end up 0700 and its own files 0600 before the
// browser starts: created so, or tightened from a looser mode another tool
// left. Another program's file in the folder keeps its mode.
// witness: internal/ops/browser_signin.go | if info.Mode().Perm()&^0o600 != 0 { → if false {
func TestReadyBrowserState_LeavesTheSignInPrivate(t *testing.T) {
	fresh := browserStateDir(t.TempDir())
	loose := browserStateDir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(loose, browserStateTmp), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Dir(loose), loose, filepath.Join(loose, browserStateTmp)} {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	own := []string{filepath.Join(loose, browserSignInFile), filepath.Join(loose, browserSignInAside)}
	other := filepath.Join(loose, "myapp-default.json")
	for _, f := range append(slices.Clone(own), other) {
		if err := os.WriteFile(f, []byte(signedInState), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{fresh, loose} {
		if err := readyBrowserState(dir, time.Now()); err != nil {
			t.Fatalf("readyBrowserState(%s): %v", dir, err)
		}
		for _, d := range []string{filepath.Dir(dir), dir, filepath.Join(dir, browserStateTmp)} {
			if info, err := os.Stat(d); err != nil || info.Mode().Perm() != 0o700 {
				t.Errorf("%s must be 0700: %v %v", d, info.Mode(), err)
			}
		}
	}
	for _, f := range own {
		if info, err := os.Stat(f); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s must be 0600: %v %v", f, info.Mode(), err)
		}
	}
	if info, err := os.Stat(other); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("another program's file must keep its mode: %v %v", info.Mode(), err)
	}
}

// A symlink where ~/.agent-browser or the state folder belongs is refused,
// so the kept cookies never land wherever it points — the workspace, a
// checkout — and no mode is changed through it.
// witness: internal/ops/browser_signin.go | if !info.IsDir() { → if false {
func TestReadyBrowserState_RefusesASymlinkedDirectory(t *testing.T) {
	for _, linked := range []string{".agent-browser", filepath.Join(".agent-browser", "sessions")} {
		t.Run(linked, func(t *testing.T) {
			home := t.TempDir()
			elsewhere := t.TempDir()
			if err := os.Chmod(elsewhere, 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, linked)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, link); err != nil {
				t.Fatal(err)
			}
			if err := readyBrowserState(browserStateDir(home), time.Now()); err == nil {
				t.Error("a symlinked folder must be refused")
			}
			if info, err := os.Stat(elsewhere); err != nil || info.Mode().Perm() != 0o755 {
				t.Errorf("the symlink's target must be left as it was: %v %v", info.Mode(), err)
			}
		})
	}
}

// A symlink where the kept file belongs is removed, never followed: its
// target keeps its content and mode, and no load reads through it.
// witness: internal/ops/browser_signin.go | if !info.Mode().IsRegular() || now.Sub(info.ModTime()) > browserSignInMaxAge { → if now.Sub(info.ModTime()) > browserSignInMaxAge {
func TestReadyBrowserState_ASymlinkedKeptFileIsRemovedNotFollowed(t *testing.T) {
	dir := readyStateDir(t)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(signedInState), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, browserSignInFile)); err != nil {
		t.Fatal(err)
	}
	if err := readyBrowserState(dir, time.Now()); err != nil {
		t.Fatalf("readyBrowserState: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, browserSignInFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the symlink must be removed: %v", err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the symlink's target must be left as it was: %v %v", info.Mode(), err)
	}
}

// zcp's kept file and its set-aside copy expire after 7 days unsaved; other
// programs' saved sessions in the same folder are never touched, however old.
// witness: internal/ops/browser_signin.go | if !info.Mode().IsRegular() || now.Sub(info.ModTime()) > browserSignInMaxAge { → if !info.Mode().IsRegular() {
func TestReadyBrowserState_ExpiresOnlyItsOwnFiles(t *testing.T) {
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour)
	stale := readyStateDir(t)
	recent := readyStateDir(t)
	for _, name := range []string{browserSignInFile, browserSignInAside, "myapp-default.json", "auth.json"} {
		for dir, at := range map[string]time.Time{stale: old, recent: now.Add(-6 * 24 * time.Hour)} {
			f := filepath.Join(dir, name)
			if err := os.WriteFile(f, []byte(signedInState), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(f, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, dir := range []string{stale, recent} {
		if err := readyBrowserState(dir, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{browserSignInFile, browserSignInAside} {
		if keptState(t, stale, name) != "" {
			t.Errorf("%s unsaved for 8 days must be removed", name)
		}
		if keptState(t, recent, name) == "" {
			t.Errorf("%s saved 6 days ago must stay", name)
		}
	}
	for _, name := range []string{"myapp-default.json", "auth.json"} {
		if keptState(t, stale, name) == "" {
			t.Errorf("another program's %s must never be removed", name)
		}
	}
}

// A load that does not finish within its bound — a saved site that never
// answers, or loops on load — sets the kept file aside and asks for a clean
// browser, so the call runs signed out instead of failing, and its result
// names the saved site.
// witness: internal/ops/browser_signin.go | return browserRestoreNote(origins, finished), true → return browserRestoreNote(origins, finished), false
func TestRestoreBrowserSignIn_ALoadThatDoesNotFinish_SetsItAsideAndStartsClean(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	synctest.Test(t, func(t *testing.T) {
		var seen [][]string
		note, restart := restoreBrowserSignIn(context.Background(), loadingBrowser("hangs", &seen), dir)
		if !restart {
			t.Error("a load that did not finish must ask for a clean browser")
		}
		if !strings.Contains(note, "http://127.0.0.1:18731") {
			t.Errorf("the result must name the saved site the load opens: %q", note)
		}
	})
	if keptState(t, dir) != "" || keptState(t, dir, browserSignInAside) != signedInState {
		t.Errorf("the kept file must be set aside: kept %q, aside %q", keptState(t, dir), keptState(t, dir, browserSignInAside))
	}
}

// A load agent-browser refuses sets the kept file aside too, replacing an
// older set-aside copy — one at most — and the copy is never loaded again:
// the next browser loads nothing until a clean save writes a new kept file.
// witness: internal/ops/browser_signin.go | if err := os.Rename(file, filepath.Join(stateDir, browserSignInAside)); err != nil { → if err := os.Rename(file, file+".old"); err != nil {
func TestRestoreBrowserSignIn_AFailedLoad_SetsItAsideOneCopyAtMostNeverLoadedAgain(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	if err := os.WriteFile(filepath.Join(dir, browserSignInAside), []byte(`{"cookies":[],"origins":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen [][]string
	if note, restart := restoreBrowserSignIn(t.Context(), loadingBrowser("fails", &seen), dir); !restart || note == "" {
		t.Errorf("a failed load must ask for a clean browser and say so: restart=%v note=%q", restart, note)
	}
	if keptState(t, dir) != "" || keptState(t, dir, browserSignInAside) != signedInState {
		t.Errorf("the kept file must replace the older set-aside copy: kept %q, aside %q", keptState(t, dir), keptState(t, dir, browserSignInAside))
	}
	seen = nil
	if _, restart := restoreBrowserSignIn(t.Context(), loadingBrowser("ok", &seen), dir); restart || len(seen) != 0 {
		t.Errorf("a set-aside file must never be loaded again: restart=%v sent %v", restart, seen)
	}
}

// A kept sign-in that loads leaves the base tab back on about:blank, so no
// restored page keeps running — and using CPU — while the browser idles.
// witness: internal/ops/browser_signin.go | err := walkBrowser(lctx, [][]string{{"state", "load", file}, {browserCmdOpen, "about:blank"}}, run) → err := walkBrowser(lctx, [][]string{{"state", "load", file}}, run)
func TestRestoreBrowserSignIn_ALoadedSignInLeavesNoRestoredPageRunning(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	note, restart := restoreBrowserSignIn(t.Context(), loadingBrowser("ok", &seen), dir)
	want := [][]string{{"state", "load", filepath.Join(dir, browserSignInFile)}, {"open", "about:blank"}}
	if note != "" || restart || !slices.EqualFunc(seen, want, slices.Equal) {
		t.Errorf("a load that worked: note=%q restart=%v sent %v, want %v", note, restart, seen, want)
	}
	if keptState(t, dir) != signedInState {
		t.Error("a kept sign-in that loaded must stay kept")
	}
}

// A save agent-browser could not load back never replaces the kept sign-in.
// witness: internal/ops/browser_signin.go | if err != nil || !validBrowserState(kept) { → if err != nil {
func TestSaveBrowserSignIn_ACandidateAgentBrowserCannotLoad_NeverReplacesTheSave(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(`{"cookies":[]}`, &seen), dir); err == nil {
		t.Error("a save agent-browser cannot load must be reported")
	}
	if got := keptState(t, dir); got != signedInState {
		t.Errorf("a broken save must not replace the kept sign-in: %q", got)
	}
}

// Session storage never reaches the kept file: a load never usefully
// restores it and the way out does not clear it, so a token a page keeps
// there would only sit on disk. Cookies and local storage are kept.
// witness: internal/ops/browser_signin.go | kept, err := withoutSessionStorage(data) → kept, err := data, error(nil)
func TestSaveBrowserSignIn_SessionStorageNeverReachesTheKeptFile(t *testing.T) {
	dir := readyStateDir(t)
	saved := strings.Replace(signedInState, `"sessionStorage":[]`, `"sessionStorage":[{"name":"stok","value":"SESSIONSECRET"}]`, 1)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(saved, &seen), dir); err != nil {
		t.Fatalf("saveBrowserSignIn: %v", err)
	}
	kept := keptState(t, dir)
	if strings.Contains(kept, "SESSIONSECRET") {
		t.Errorf("session storage reached the kept file: %s", kept)
	}
	if !strings.Contains(kept, `"abc123"`) || !strings.Contains(kept, `"xyz789"`) || !validBrowserState([]byte(kept)) {
		t.Errorf("the cookie and local storage must be kept: %s", kept)
	}
}
