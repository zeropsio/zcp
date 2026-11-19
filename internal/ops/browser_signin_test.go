package ops

import (
	"context"
	"errors"
	"fmt"
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

// baseTarget is the base tab's CDP target in the fake browsers below.
const baseTarget = "8CFDBD9D2FB7066D95420DA2712DC6D2"

// presenceIn is a browser presence file in a runtime folder of its own,
// written as p.
func presenceIn(t *testing.T, p browserPresence) string {
	t.Helper()
	if p.BaseTarget == "" {
		p.BaseTarget = baseTarget
	}
	path := filepath.Join(t.TempDir(), "zcp-signin.json")
	if err := writeBrowserPresence(path, p); err != nil {
		t.Fatal(err)
	}
	return path
}

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

// savingBrowser answers a hand-back's steps as agent-browser does, the
// call's tab active on the first site (savingBrowserOn).
func savingBrowser(saved string, seen *[][]string) browserStepExec {
	return savingBrowserOn("http://127.0.0.1:18731/", saved, seen)
}

// savingBrowserOn answers a hand-back's steps as agent-browser does: the tab
// list (the call's tab active on active), and a state save that writes saved
// to the path it is given (a failure when saved is ""). It records each
// command.
func savingBrowserOn(active, saved string, seen *[][]string) browserStepExec {
	return func(_ context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		switch {
		case slices.Equal(args, []string{"tab", "list"}):
			return `{"success":true,"data":{"tabs":[{"tabId":"t1","targetId":"` + baseTarget + `","url":"about:blank"},{"tabId":"t2","targetId":"3DF3","url":"` + active + `","active":true}]},"error":null}`, "", nil
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

// threeSites is a kept sign-in with a cookie and the local storage of three
// sites, the second of which misbehaves in the tests that use it.
const threeSites = `{"cookies":[{"name":"sid","value":"abc123","domain":"127.0.0.1","path":"/"}],"origins":[` +
	`{"origin":"http://127.0.0.1:18731","localStorage":[{"name":"token","value":"a"}]},` +
	`{"origin":"http://127.0.0.1:18735","localStorage":[{"name":"token","value":"b"}]},` +
	`{"origin":"http://127.0.0.1:18732","localStorage":[{"name":"token","value":"c"}]}]}`

// restoringBrowser answers a restore's steps as agent-browser does. A state
// load reads the part it is given: one holding the origin bad is answered
// as how says ("hangs" until the step's context ends, "fails"), cookies as
// cookies says — one answer per load in turn, the last one repeated ("ok",
// "fails", "hangs") — anything else a success. It records what each load
// held — "cookies" or the origin — and every other command.
func restoringBrowser(bad, how string, cookies []string, seen *[]string) browserStepExec {
	answer := func(ctx context.Context, mode string) (string, string, error) {
		switch mode {
		case "hangs":
			<-ctx.Done()
			return "", "", ctx.Err()
		case "fails":
			return `{"success":false,"data":null,"error":"Invalid state file: expected value at line 1 column 1"}`, "", errors.New("exit status 1")
		}
		return `{"success":true,"data":{},"error":null}`, "", nil
	}
	return func(ctx context.Context, args []string) (string, string, error) {
		if len(args) == 3 && args[0] == "state" && args[1] == "load" {
			_, origins, _ := parseBrowserState(mustRead(args[2]))
			if len(origins) == 0 {
				*seen = append(*seen, "cookies")
				mode := cookies[0]
				if len(cookies) > 1 {
					cookies = cookies[1:]
				}
				return answer(ctx, mode)
			}
			*seen = append(*seen, origins[0].name)
			if origins[0].name == bad {
				return answer(ctx, how)
			}
			return answer(ctx, "ok")
		}
		if slices.Equal(args, []string{"tab", "list"}) {
			return `{"success":true,"data":{"tabs":[{"tabId":"t1","targetId":"` + baseTarget + `","url":"about:blank","active":true}]},"error":null}`, "", nil
		}
		*seen = append(*seen, strings.Join(args, " "))
		return answer(ctx, "ok")
	}
}

var cookiesLoad = []string{"ok"}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

// keptOrigins lists the origins of the kept sign-in in dir.
func keptOrigins(t *testing.T, dir string) []string {
	t.Helper()
	_, origins, ok := parseBrowserState([]byte(keptState(t, dir)))
	if !ok {
		t.Fatalf("the kept sign-in is not loadable: %q", keptState(t, dir))
	}
	names := make([]string, 0, len(origins))
	for _, o := range origins {
		names = append(names, o.name)
	}
	return names
}

// writeState writes state as the kept sign-in in dir.
func writeState(t *testing.T, dir, state string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, browserSignInFile), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
}

// presenceAfter reads the presence a restore wrote.
func presenceAfter(t *testing.T, path string) browserPresence {
	t.Helper()
	p, ok := readBrowserPresence(path)
	if !ok {
		t.Fatal("the restore wrote no presence")
	}
	return p
}

// noRestart is a restart that only counts.
func noRestart(count *int) func(context.Context) error {
	return func(context.Context) error { *count++; return nil }
}

// A kept sign-in whose cookies agent-browser cannot load — or one that is
// not a state at all — is set aside, never loaded again, and the call runs
// signed out in a clean browser, saying so.
// witness: internal/ops/browser_signin.go | 		if rctx.Err() == nil { → 		if false {
func TestRestoreBrowserSignIn_AFileAgentBrowserCannotLoadIsSetAside(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	var seen []string
	restarts := 0
	note, err := restoreBrowserSignIn(t.Context(), restoringBrowser("", "", []string{"fails"}, &seen), dir, presence, time.Now(), noRestart(&restarts))
	if err != nil || note == "" || restarts != 1 {
		t.Errorf("a file whose cookies do not load: note=%q err=%v restarts=%d", note, err, restarts)
	}
	if keptState(t, dir) != "" || keptState(t, dir, browserSignInAside) != threeSites {
		t.Errorf("the file must be set aside: kept %q", keptState(t, dir))
	}
	if !slices.Equal(seen, []string{"cookies"}) {
		t.Errorf("nothing but the cookies may be loaded from it: %v", seen)
	}
	seen = nil
	if note, _ := restoreBrowserSignIn(t.Context(), restoringBrowser("", "", cookiesLoad, &seen), dir, presence, time.Now(), noRestart(&restarts)); note != "" || len(seen) != 0 {
		t.Errorf("a set-aside file must never be loaded again: note=%q loads %v", note, seen)
	}
}

// A saved site that does not answer within its own bound loses only its own
// saved storage: it is dropped from the kept file and named, and the other
// sites and the cookie stay kept.
// witness: internal/ops/browser_signin.go | 			dropped = append(dropped, o) →
func TestRestoreBrowserSignIn_ASiteThatDoesNotAnswerLosesOnlyItsOwnStorage(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		var seen []string
		restarts := 0
		note, err := restoreBrowserSignIn(context.Background(), restoringBrowser("http://127.0.0.1:18735", "hangs", cookiesLoad, &seen), dir, presence, time.Now(), noRestart(&restarts))
		if err != nil || restarts != 1 || !strings.Contains(note, "http://127.0.0.1:18735") {
			t.Errorf("one site that does not answer: note=%q err=%v restarts=%d", note, err, restarts)
		}
	})
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731", "http://127.0.0.1:18732"}) {
		t.Errorf("the kept file must lose only the site that did not answer: %v", got)
	}
	if !strings.Contains(keptState(t, dir), `"abc123"`) || keptState(t, dir, browserSignInAside) != "" {
		t.Errorf("the cookie must stay kept and nothing set aside: %q", keptState(t, dir))
	}
}

// A restart during the restore brings back the cookies and the sites
// already restored before the next site, so none is silently lost and the
// result names nothing but the site that did not answer.
// witness: internal/ops/browser_signin.go | 			back = append(back, o) →
func TestRestoreBrowserSignIn_ARestartBringsBackTheSitesAlreadyRestored(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		var seen []string
		restarts := 0
		note, err := restoreBrowserSignIn(context.Background(), restoringBrowser("http://127.0.0.1:18735", "hangs", cookiesLoad, &seen), dir, presence, time.Now(), noRestart(&restarts))
		want := []string{"cookies", "http://127.0.0.1:18731", "http://127.0.0.1:18735", "cookies", "http://127.0.0.1:18731", "http://127.0.0.1:18732", "open about:blank"}
		if err != nil || !slices.Equal(seen, want) {
			t.Errorf("loads %v, want %v (err %v)", seen, want, err)
		}
		if strings.Contains(note, "18731") || strings.Contains(note, "18732") {
			t.Errorf("the sites brought back must not be named: %q", note)
		}
	})
}

// Cookies that do not come back after a restart leave the browser without
// them: its presence says so, so no save can replace the kept cookies, and
// the result says the kept sign-in is still kept and a new one would not be.
// witness: internal/ops/browser_signin.go | 			p.CookiesHeld, restored = false, nil → 			restored = nil
func TestRestoreBrowserSignIn_CookiesThatDoNotComeBackStopTheSaves(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		var seen []string
		restarts := 0
		note, err := restoreBrowserSignIn(context.Background(), restoringBrowser("http://127.0.0.1:18735", "hangs", []string{"ok", "fails"}, &seen), dir, presence, time.Now(), noRestart(&restarts))
		if err != nil || note == "" {
			t.Errorf("cookies that do not come back: note=%q err=%v", note, err)
		}
	})
	if presenceAfter(t, presence).CookiesHeld {
		t.Error("a browser without the kept cookies must not be marked as holding them")
	}
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(`{"cookies":[],"origins":[]}`, &seen), dir, presence, time.Now()); err == nil || keptState(t, dir) == `{"cookies":[],"origins":[]}` {
		t.Errorf("its save must be refused and the kept sign-in stay: err=%v kept %q", err, keptState(t, dir))
	}
}

// A slow site that still answers within its own bound — a dev server's
// first request after a restart — keeps its storage.
// witness: internal/ops/browser_signin.go | 	browserSignInOriginTimeout = 4 * time.Second → 	browserSignInOriginTimeout = 2 * time.Second
func TestRestoreBrowserSignIn_ASlowSiteThatAnswersInTimeIsKept(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		quick := restoringBrowser("", "", cookiesLoad, new([]string))
		run := func(ctx context.Context, args []string) (string, string, error) {
			if len(args) == 3 && args[1] == "load" && strings.Contains(string(mustRead(args[2])), "18735") {
				select {
				case <-time.After(3 * time.Second):
				case <-ctx.Done():
					return "", "", ctx.Err()
				}
			}
			return quick(ctx, args)
		}
		restarts := 0
		if note, err := restoreBrowserSignIn(context.Background(), run, dir, presence, time.Now(), noRestart(&restarts)); note != "" || err != nil {
			t.Errorf("a slow site that answers in time: note=%q err=%v", note, err)
		}
	})
	if got := keptOrigins(t, dir); len(got) != 3 {
		t.Errorf("every site must stay kept: %v", got)
	}
}

// A call with little time left restores the cookies and leaves the sites'
// storage kept for the next new browser instead of failing or dropping it.
// witness: internal/ops/browser_signin.go | budget = min(budget, time.Until(deadline)/3) → budget = min(budget, time.Until(deadline))
func TestRestoreBrowserSignIn_ACallWithLittleTimeLeftRestoresTheCookiesOnly(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		defer cancel()
		var seen []string
		restarts := 0
		note, err := restoreBrowserSignIn(ctx, restoringBrowser("", "", cookiesLoad, &seen), dir, presence, time.Now(), noRestart(&restarts))
		if err != nil || !slices.Equal(seen, []string{"cookies"}) || note == "" {
			t.Errorf("little time left: loads %v note=%q err=%v", seen, note, err)
		}
	})
	if got := keptOrigins(t, dir); len(got) != 3 || !presenceAfter(t, presence).CookiesHeld || len(presenceAfter(t, presence).Unrestored) != 3 {
		t.Errorf("the sites must stay kept, unrestored, and the cookies held: %v %+v", got, presenceAfter(t, presence))
	}
}

// A kept sign-in that loads leaves the base tab back on about:blank, so no
// restored page keeps running — and using CPU — while the browser idles.
// witness: internal/ops/browser_signin.go | 	if onSite { → 	if onSite && false {
func TestRestoreBrowserSignIn_ALoadedSignInLeavesNoRestoredPageRunning(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	var seen []string
	restarts := 0
	note, err := restoreBrowserSignIn(t.Context(), restoringBrowser("", "", cookiesLoad, &seen), dir, presence, time.Now(), noRestart(&restarts))
	want := []string{"cookies", "http://127.0.0.1:18731", "open about:blank"}
	if note != "" || err != nil || !slices.Equal(seen, want) {
		t.Errorf("a load that worked: note=%q err=%v loads %v, want %v", note, err, seen, want)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731"}) || !presenceAfter(t, presence).CookiesHeld {
		t.Errorf("a kept sign-in that loaded must stay kept: %v", got)
	}
}

// usedSites is a kept sign-in whose sites were last used at these ages.
func usedSites(now time.Time, ages ...time.Duration) string {
	entries := make([]string, 0, len(ages))
	for i, age := range ages {
		entries = append(entries, fmt.Sprintf(`{"origin":"http://127.0.0.1:%d","localStorage":[{"name":"token","value":"v"}],"zcpLastUsed":%q}`,
			18740+i, now.Add(-age).UTC().Format(time.RFC3339)))
	}
	return `{"cookies":[],"origins":[` + strings.Join(entries, ",") + `]}`
}

// A kept site no call used for 7 days — gone, renamed, never answering — is
// dropped before the restore loads anything, so it stops costing every new
// browser a load.
// witness: internal/ops/browser_signin.go | 		if now.Sub(o.lastUsed) <= browserSignInOriginMaxAge { → 		if true {
func TestRestoreBrowserSignIn_ASiteUnusedForAWeekIsDropped(t *testing.T) {
	now := time.Now()
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, time.Hour, 8*24*time.Hour))
	var seen []string
	restarts := 0
	if _, err := restoreBrowserSignIn(t.Context(), restoringBrowser("", "", cookiesLoad, &seen), dir, filepath.Join(t.TempDir(), "p.json"), now, noRestart(&restarts)); err != nil {
		t.Fatal(err)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18740"}) || slices.Contains(seen, "http://127.0.0.1:18741") {
		t.Errorf("a site unused for 8 days must be dropped, not loaded: kept %v, loads %v", got, seen)
	}
}

// At most the 20 most recently used sites are kept, so a restore stays
// bounded however many sites the Mate ever opened.
// witness: internal/ops/browser_signin.go | 	if len(kept) > browserSignInOriginCap { → 	if false {
func TestRestoreBrowserSignIn_KeepsTheTwentyMostRecentlyUsedSites(t *testing.T) {
	now := time.Now()
	ages := make([]time.Duration, 25)
	for i := range ages {
		ages[i] = time.Duration(25-i) * time.Minute
	}
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, ages...))
	var seen []string
	restarts := 0
	if _, err := restoreBrowserSignIn(t.Context(), restoringBrowser("", "", cookiesLoad, &seen), dir, filepath.Join(t.TempDir(), "p.json"), now, noRestart(&restarts)); err != nil {
		t.Fatal(err)
	}
	got := keptOrigins(t, dir)
	if len(got) != 20 || got[0] != "http://127.0.0.1:18764" || slices.Contains(got, "http://127.0.0.1:18740") {
		t.Errorf("the 20 most recently used sites must be kept, the latest first: %v", got)
	}
}

// The origin an open step's page ended on — after a redirect, say — is one a
// save reads; a failed open adds nothing, and a browser with no presence
// keeps no sign-in and gets none.
// witness: internal/ops/browser_signin.go | 			p.Opened = append(p.Opened, origin) →
func TestRecordBrowserOpened_TheOriginAnOpenEndedOnIsRead(t *testing.T) {
	stdout := `[{"command":["set","viewport","1280","577"],"success":true},` +
		`{"command":["open","http://127.0.0.1:18731/sign-out"],"success":true,"result":{"title":"Sign in","url":"http://127.0.0.1:18732/login"}},` +
		`{"command":["open","http://127.0.0.1:18733/"],"success":false,"error":"Navigation failed"}]`
	path := presenceIn(t, browserPresence{CookiesHeld: true})
	if err := recordBrowserOpened(path, stdout); err != nil {
		t.Fatal(err)
	}
	if got := presenceAfter(t, path).Opened; !slices.Equal(got, []string{"http://127.0.0.1:18732"}) {
		t.Errorf("opened origins = %v, want the page the open ended on", got)
	}
	none := filepath.Join(t.TempDir(), "zcp-signin.json")
	if err := recordBrowserOpened(none, stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(none); !errors.Is(err, os.ErrNotExist) {
		t.Error("a browser that keeps no sign-in must get no presence")
	}
}

// A save keeps the saved storage of every kept site the browser did not
// read — one it only restored, or never opened — and replaces the cookies
// and the sites it read.
// witness: internal/ops/browser_signin.go | 			origins = append(origins, kept[o.name]) →
func TestSaveBrowserSignIn_KeepsTheSitesTheBrowserDidNotRead(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	saved := `{"cookies":[{"name":"sid","value":"new","domain":"127.0.0.1","path":"/"}],"origins":[{"origin":"http://127.0.0.1:18731","localStorage":[{"name":"token","value":"a2"}]}]}`
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(saved, &seen), dir, presenceIn(t, browserPresence{CookiesHeld: true}), time.Now()); err != nil {
		t.Fatalf("saveBrowserSignIn: %v", err)
	}
	kept := keptState(t, dir)
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731", "http://127.0.0.1:18735", "http://127.0.0.1:18732"}) {
		t.Errorf("sites the browser did not read must stay kept, the saved ones first: %v", got)
	}
	if !strings.Contains(kept, `"a2"`) || strings.Contains(kept, `"value":"a"`) || !strings.Contains(kept, `"new"`) || strings.Contains(kept, `"abc123"`) {
		t.Errorf("the saved cookies and sites must replace the kept ones: %s", kept)
	}
}

// The way out holds through the merge: after cookies clear and storage local
// clear on the signed-in page, the save has no cookies and nothing for that
// page's site, and the kept file keeps neither.
// witness: internal/ops/browser_signin.go | 	for _, name := range append(slices.Clone(p.Opened), active) { → 	for _, name := range slices.Clone(p.Opened) {
func TestSaveBrowserSignIn_TheWayOutEmptiesWhatItClears(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(`{"cookies":[],"origins":[]}`, &seen), dir, presenceIn(t, browserPresence{CookiesHeld: true}), time.Now()); err != nil {
		t.Fatalf("saveBrowserSignIn: %v", err)
	}
	if kept := keptState(t, dir); kept != `{"cookies":[],"origins":[]}` {
		t.Errorf("the way out must leave nothing kept: %s", kept)
	}
}

// A clear sticks wherever the page went afterwards: a site an open step
// ended on whose storage the save lacks — its own sign-out cleared it, then
// sent the page to an identity provider — keeps nothing.
// witness: internal/ops/browser_signin.go | 		seen[name] = true → 		_ = name
func TestSaveBrowserSignIn_AClearSticksWhereverThePageWent(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18731"}})
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowserOn("https://login.example.test/authorize", `{"cookies":[],"origins":[]}`, &seen), dir, presence, time.Now()); err != nil {
		t.Fatalf("saveBrowserSignIn: %v", err)
	}
	if kept := keptState(t, dir); strings.Contains(kept, "18731") {
		t.Errorf("a cleared site must not come back: %s", kept)
	}
}

// A browser that does not hold the kept cookies saves nothing, so a call
// that restored them only in part never writes its own cookies over them.
// witness: internal/ops/browser_signin.go | 	if !ok || !presence.CookiesHeld { → 	if !ok {
func TestSaveBrowserSignIn_ABrowserWithoutTheKeptCookiesSavesNothing(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(`{"cookies":[],"origins":[]}`, &seen), dir, presenceIn(t, browserPresence{}), time.Now()); err == nil {
		t.Error("a save from a browser without the kept cookies must be refused")
	}
	if keptState(t, dir) != signedInState {
		t.Errorf("the kept sign-in must stay as it was: %s", keptState(t, dir))
	}
}

// Only use renews a kept site: the site a call opened is used now, and one
// the browser only restored keeps its last use, so it expires when no call
// opens it any more.
// witness: internal/ops/browser_signin.go | 			origins = append(origins, kept[o.name]) → 			origins = append(origins, kept[o.name].usedAt(now))
func TestSaveBrowserSignIn_OnlyUseRenewsAKeptSite(t *testing.T) {
	now := time.Now()
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, 6*24*time.Hour, 6*24*time.Hour))
	saved := `{"cookies":[],"origins":[{"origin":"http://127.0.0.1:18741","localStorage":[{"name":"token","value":"v"}]}]}`
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18741"}})
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowserOn("http://127.0.0.1:18741/", saved, &seen), dir, presence, now); err != nil {
		t.Fatal(err)
	}
	_, origins, _ := parseBrowserState([]byte(keptState(t, dir)))
	used := map[string]time.Duration{}
	for _, o := range origins {
		used[o.name] = now.Sub(o.lastUsed)
	}
	if used["http://127.0.0.1:18741"] > time.Minute || used["http://127.0.0.1:18740"] < 5*24*time.Hour {
		t.Errorf("last uses after the save: %v", used)
	}
}

// A save agent-browser could not load back never replaces the kept sign-in.
// witness: internal/ops/browser_signin.go | if err != nil || !validBrowserState(saved) { → if err != nil {
func TestSaveBrowserSignIn_ACandidateAgentBrowserCannotLoad_NeverReplacesTheSave(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(`{"cookies":[]}`, &seen), dir, presenceIn(t, browserPresence{CookiesHeld: true}), time.Now()); err == nil {
		t.Error("a save agent-browser cannot load must be reported")
	}
	if got := keptState(t, dir); got != signedInState {
		t.Errorf("a broken save must not replace the kept sign-in: %q", got)
	}
}

// Session storage never reaches the kept file: a load never usefully
// restores it and the way out does not clear it, so a token a page keeps
// there would only sit on disk. Cookies and local storage are kept.
// witness: internal/ops/browser_signin.go | saved, err := withoutSessionStorage(data) → saved, err := data, error(nil)
func TestSaveBrowserSignIn_SessionStorageNeverReachesTheKeptFile(t *testing.T) {
	dir := readyStateDir(t)
	saved := strings.Replace(signedInState, `"sessionStorage":[]`, `"sessionStorage":[{"name":"stok","value":"SESSIONSECRET"}]`, 1)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), savingBrowser(saved, &seen), dir, presenceIn(t, browserPresence{CookiesHeld: true}), time.Now()); err != nil {
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

// A site this browser did not get restored — no time was left — and a call
// then opened is not erased by the save: an empty read of it says nothing of
// its kept storage.
// witness: internal/ops/browser_signin.go | 		if name != "" && !slices.Contains(p.Unrestored, name) { → 		if name != "" {
func TestSaveBrowserSignIn_ASiteNotRestoredIsNotErasedByOpeningIt(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18735"}, Unrestored: []string{"http://127.0.0.1:18735"}})
	var seen [][]string
	saved := `{"cookies":[{"name":"sid","value":"abc123","domain":"127.0.0.1","path":"/"}],"origins":[]}`
	if err := saveBrowserSignIn(t.Context(), savingBrowserOn("http://127.0.0.1:18735/", saved, &seen), dir, presence, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := keptOrigins(t, dir); !slices.Contains(got, "http://127.0.0.1:18735") {
		t.Errorf("a site never restored must keep its storage: %v", got)
	}
}

// A call reuses the warm browser only while it holds the kept cookies in the
// Chrome they were restored into: one agent-browser relaunched empty — an
// OOM kill — or one that does not hold them gets a fresh browser and a
// restore.
// witness: internal/ops/browser_signin.go | 	return err == nil && base != "" && base == p.BaseTarget → 	return err == nil && base != ""
func TestKeptSignInLetsReuse_OnlyTheChromeTheSignInWasRestoredInto(t *testing.T) {
	run := savingBrowser("", new([][]string))
	tests := []struct {
		name     string
		presence *browserPresence
		want     bool
	}{
		{"the same Chrome, holding the cookies", &browserPresence{CookiesHeld: true}, true},
		{"Chrome relaunched since the restore", &browserPresence{CookiesHeld: true, BaseTarget: "A7E47918804A4FDA"}, false},
		{"the cookies not held", &browserPresence{}, false},
		{"no restore in this browser", nil, false},
	}
	for _, tt := range tests {
		path := filepath.Join(t.TempDir(), "zcp-signin.json")
		if tt.presence != nil {
			path = presenceIn(t, *tt.presence)
		}
		if got := keptSignInLetsReuse(t.Context(), run, path); got != tt.want {
			t.Errorf("%s: keptSignInLetsReuse = %v, want %v", tt.name, got, tt.want)
		}
	}
}
