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

// non-parallel: these tests replace browserCDPDial with an in-process Chrome.

// A kept sign-in with a cookie and local storage, in agent-browser 0.39.1's
// state format.
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
	writeState(t, dir, signedInState)
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

// browserOn answers agent-browser's steps around a save or restore: the
// tab list — the base tab t1 on about:blank, then each of tabs as
// "<id> <url>" — and the DevTools URL of its Chrome, which a test reaches
// through fakeChrome. It records each command.
func browserOn(seen *[][]string, tabs ...string) browserStepExec {
	return func(_ context.Context, args []string) (string, string, error) {
		*seen = append(*seen, args)
		switch {
		case slices.Equal(args, []string{"tab", "list"}):
			listed := make([]string, 0, 1+len(tabs))
			listed = append(listed, `{"tabId":"t1","targetId":"`+baseTarget+`","url":"about:blank"}`)
			for _, tab := range tabs {
				id, url, _ := strings.Cut(tab, " ")
				listed = append(listed, fmt.Sprintf(`{"tabId":%q,"targetId":"T%s","url":%q}`, id, id, url))
			}
			return `{"success":true,"data":{"tabs":[` + strings.Join(listed, ",") + `]},"error":null}`, "", nil
		case slices.Equal(args, []string{"get", "cdp-url"}):
			return `{"success":true,"data":{"cdpUrl":"ws://127.0.0.1:9222/devtools/browser/0b5c"},"error":null}`, "", nil
		}
		return `{"success":true,"data":{},"error":null}`, "", nil
	}
}

// keptOrigins lists the origins of the kept sign-in in dir.
func keptOrigins(t *testing.T, dir string) []string {
	t.Helper()
	_, origins, ok := parseBrowserState([]byte(keptState(t, dir)))
	if !ok {
		t.Fatalf("the kept sign-in is not a kept state: %q", keptState(t, dir))
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

// usedSites is a kept sign-in whose sites were last used at these ages.
func usedSites(now time.Time, ages ...time.Duration) string {
	entries := make([]string, 0, len(ages))
	for i, age := range ages {
		entries = append(entries, fmt.Sprintf(`{"origin":"http://127.0.0.1:%d","localStorage":[{"name":"token","value":"v"}],"zcpLastUsed":%q}`,
			18740+i, now.Add(-age).UTC().Format(time.RFC3339)))
	}
	return `{"cookies":[],"origins":[` + strings.Join(entries, ",") + `]}`
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

// A kept file that is no kept state zcp can read is set aside, never
// restored again, and the call runs signed out, saying so; Chrome is not
// asked to take any of it.
// witness: internal/ops/browser_signin.go | 	if err != nil || !ok { → 	if err != nil {
func TestRestoreBrowserSignIn_AFileThatIsNoKeptStateIsSetAside(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeState(t, dir, `{"cookies":[{"name":"sid"`)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	var seen [][]string
	note, err := restoreBrowserSignIn(t.Context(), browserOn(&seen), dir, presence, time.Now())
	if err != nil || note == "" {
		t.Errorf("a file that is no kept state: note=%q err=%v", note, err)
	}
	if keptState(t, dir) != "" || keptState(t, dir, browserSignInAside) != `{"cookies":[{"name":"sid"` {
		t.Errorf("the file must be set aside: kept %q", keptState(t, dir))
	}
	if len(chrome.cookieNames()) != 0 || !presenceAfter(t, presence).CookiesHeld {
		t.Errorf("nothing of it may reach Chrome, and the clean browser holds what is kept, nothing: %v", chrome.cookieNames())
	}
	if note, _ := restoreBrowserSignIn(t.Context(), browserOn(&seen), dir, presence, time.Now()); note != "" {
		t.Errorf("a set-aside file must never be restored again: note=%q", note)
	}
}

// A new browser gets every kept cookie and every kept site's local storage
// back, and no site is loaded for it: no agent-browser open or state load,
// and the browser is left as agent-browser had it: no tab of zcp's, its
// base tab back on about:blank.
// witness: internal/ops/browser_signin.go | 	refused, err := cdp.setCookies(rctx, kept) → 	refused, err := cdp.setCookies(rctx, nil)
func TestRestoreBrowserSignIn_EveryKeptCookieAndSiteComesBackWithNoSiteLoaded(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	var seen [][]string
	note, err := restoreBrowserSignIn(t.Context(), browserOn(&seen), dir, presence, time.Now())
	if note != "" || err != nil {
		t.Fatalf("a restore that worked: note=%q err=%v", note, err)
	}
	if got := chrome.cookieNames(); !slices.Equal(got, []string{"sid"}) {
		t.Errorf("the kept cookie must be in Chrome: %v", got)
	}
	for origin, want := range map[string]string{"http://127.0.0.1:18731": "a", "http://127.0.0.1:18735": "b", "http://127.0.0.1:18732": "c"} {
		if got := chrome.storageOf(origin); !slices.Equal(got, []browserStorageItem{{"token", want}}) {
			t.Errorf("the local storage of %s = %v, want token=%s", origin, got, want)
		}
	}
	for _, step := range seen {
		if step[0] == "open" || step[0] == "state" {
			t.Errorf("the restore loaded a site: %v", step)
		}
	}
	if left := chrome.leftBehind(); len(left) != 0 {
		t.Errorf("the browser must be left as agent-browser had it: %v", left)
	}
	if p := presenceAfter(t, presence); !p.CookiesHeld || p.BaseTarget != baseTarget || len(p.Unrestored) != 0 {
		t.Errorf("the browser holds the whole kept sign-in: %+v", p)
	}
}

// A kept site whose local storage Chrome refuses to take back loses only
// its own saved storage: it is dropped from the kept file and named, and the
// other sites and the cookie are restored and stay kept.
// witness: internal/ops/browser_signin.go | 				dropped = append(dropped, o.name) → 				missing = append(missing, o.name)
func TestRestoreBrowserSignIn_ASiteChromeRefusesLosesOnlyItsOwnStorage(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.refuse["http://127.0.0.1:18735"] = true
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	var seen [][]string
	note, err := restoreBrowserSignIn(t.Context(), browserOn(&seen), dir, presence, time.Now())
	if err != nil || !strings.Contains(note, "http://127.0.0.1:18735") || !strings.Contains(note, "dropped") {
		t.Errorf("one site Chrome refuses: note=%q err=%v", note, err)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731", "http://127.0.0.1:18732"}) {
		t.Errorf("the kept file must lose only the refused site: %v", got)
	}
	if !strings.Contains(keptState(t, dir), `"abc123"`) || len(chrome.storageOf("http://127.0.0.1:18732")) != 1 {
		t.Errorf("the cookie must stay kept and the next site restored: %q", keptState(t, dir))
	}
}

// A restore that runs out of its bound — a stub write that never comes back
// — drops nothing: the sites it did not get to stay kept, are named as not
// restored, and an empty read of them in this browser cannot erase them; the
// cookies it set are held, and the base tab is given back.
// witness: internal/ops/browser_signin.go | 			if refused := errors.Is(err, errBrowserStorageRefused); refused { → 			if refused := true; refused {
func TestRestoreBrowserSignIn_ARestoreThatRunsOutDropsNothing(t *testing.T) {
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	synctest.Test(t, func(t *testing.T) {
		chrome := newFakeChrome().dialedBy(t)
		chrome.stall["http://127.0.0.1:18735"] = true
		var seen [][]string
		note, err := restoreBrowserSignIn(context.Background(), browserOn(&seen), dir, presence, time.Now())
		if err != nil || !strings.Contains(note, "18735") || !strings.Contains(note, "18732") || strings.Contains(note, "dropped") {
			t.Errorf("a restore that ran out: note=%q err=%v", note, err)
		}
		if left := chrome.leftBehind(); len(left) != 0 {
			t.Errorf("the browser must be left as agent-browser had it: %v", left)
		}
	})
	if got := keptOrigins(t, dir); len(got) != 3 {
		t.Errorf("every site must stay kept: %v", got)
	}
	if p := presenceAfter(t, presence); !p.CookiesHeld || !slices.Equal(p.Unrestored, []string{"http://127.0.0.1:18735", "http://127.0.0.1:18732"}) {
		t.Errorf("the cookies are held and the sites not reached unrestored: %+v", p)
	}
}

// Cookies that do not get into the new browser — its Chrome not reached —
// leave it without them: its presence says so, so no save can replace the
// kept cookies, and the result says the kept sign-in is still kept and a new
// one would not be.
// witness: internal/ops/browser_signin.go | 		return done(browserSignInNotHeld) → 		p.CookiesHeld = true; return done(browserSignInNotHeld)
func TestRestoreBrowserSignIn_CookiesThatDoNotGetInStopTheSaves(t *testing.T) {
	newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	unreached := func(ctx context.Context, args []string) (string, string, error) {
		if slices.Equal(args, []string{"get", "cdp-url"}) {
			return `{"success":false,"data":null,"error":"Browser not launched"}`, "", errors.New("exit status 1")
		}
		return browserOn(new([][]string))(ctx, args)
	}
	note, err := restoreBrowserSignIn(t.Context(), unreached, dir, presence, time.Now())
	if err != nil || note != browserSignInNotHeld {
		t.Errorf("cookies that did not get in: note=%q err=%v", note, err)
	}
	if presenceAfter(t, presence).CookiesHeld {
		t.Error("a browser without the kept cookies must not be marked as holding them")
	}
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18731/"), dir, presence, "t2", time.Now()); err == nil || keptState(t, dir) != threeSites {
		t.Errorf("its save must be refused and the kept sign-in stay: err=%v kept %q", err, keptState(t, dir))
	}
}

// A Chrome that dies during the restore — the connection closes at the
// first site — drops nothing: every site stays kept and is named as not
// restored, and the browser agent-browser relaunched, empty, is not taken
// for one holding the kept sign-in, so its save cannot replace the kept
// cookies.
// witness: internal/ops/browser_signin.go | 		if restoredInto != "" && base != restoredInto { → 		if false {
func TestRestoreBrowserSignIn_AChromeThatDiesMidRestoreKeepsEverything(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.dies = true
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	lists := 0
	relaunched := func(ctx context.Context, args []string) (string, string, error) {
		if slices.Equal(args, []string{"tab", "list"}) {
			if lists++; lists > 1 {
				return `{"success":true,"data":{"tabs":[{"tabId":"t1","targetId":"5D1E0A","url":"about:blank"}]},"error":null}`, "", nil
			}
		}
		return browserOn(new([][]string))(ctx, args)
	}
	note, err := restoreBrowserSignIn(t.Context(), relaunched, dir, presence, time.Now())
	if err != nil || note != browserSignInNotHeld {
		t.Errorf("a Chrome that died: note=%q err=%v", note, err)
	}
	if keptState(t, dir) != threeSites {
		t.Errorf("nothing may be dropped: %s", keptState(t, dir))
	}
	if p := presenceAfter(t, presence); p.CookiesHeld || len(p.Unrestored) != 3 {
		t.Errorf("the relaunched browser holds nothing kept: %+v", p)
	}
	if err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18731/"), dir, presence, "t2", time.Now()); err == nil || keptState(t, dir) != threeSites {
		t.Errorf("its save must be refused and the kept sign-in stay: err=%v", err)
	}
}

// A kept http:// site whose stub Chrome upgrades to https:// is neither
// written into the https:// site's storage nor dropped: it stays kept and is
// named as not restored.
// witness: internal/ops/browser_cdp.go | 	if elsewhere != "" { → 	if false {
func TestRestoreBrowserSignIn_ASiteChromeUpgradesToHTTPSIsKeptNotWrittenElsewhere(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.upgrade["http://legacy.example.test"] = true
	dir := readyStateDir(t)
	state := `{"cookies":[],"origins":[{"origin":"http://legacy.example.test","localStorage":[{"name":"token","value":"v"}]}]}`
	writeState(t, dir, state)
	note, err := restoreBrowserSignIn(t.Context(), browserOn(new([][]string)), dir, filepath.Join(t.TempDir(), "p.json"), time.Now())
	if err != nil || !strings.Contains(note, "http://legacy.example.test") || strings.Contains(note, "dropped") {
		t.Errorf("an upgraded site: note=%q err=%v", note, err)
	}
	if len(chrome.storageOf("https://legacy.example.test")) != 0 || !slices.Equal(keptOrigins(t, dir), []string{"http://legacy.example.test"}) {
		t.Errorf("the site must stay kept and nothing go to its https twin: %v", chrome.storageOf("https://legacy.example.test"))
	}
}

// A kept cookie Chrome refuses costs only itself: the other cookies and
// the sites are restored, the browser holds the kept sign-in, and the result
// names the refused cookie, which the next save no longer keeps.
// witness: internal/ops/browser_cdp.go | 	if !errors.As(err, &refusal) { → 	if true {
func TestRestoreBrowserSignIn_ACookieChromeRefusesCostsOnlyItself(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.refuseCookie = "bad"
	dir := readyStateDir(t)
	writeState(t, dir, strings.Replace(signedInState, `"cookies":[`, `"cookies":[{"name":"bad","value":"x","domain":"","path":"/"},`, 1))
	presence := filepath.Join(t.TempDir(), "zcp-signin.json")
	note, err := restoreBrowserSignIn(t.Context(), browserOn(new([][]string)), dir, presence, time.Now())
	if !strings.Contains(note, "bad") || strings.Contains(note, "sid") || err != nil || !presenceAfter(t, presence).CookiesHeld {
		t.Errorf("one refused cookie: note=%q err=%v", note, err)
	}
	if got := chrome.cookieNames(); !slices.Equal(got, []string{"sid"}) || len(chrome.storageOf("http://127.0.0.1:18731")) != 1 {
		t.Errorf("the other cookie and the site must be restored: cookies %v", got)
	}
}

// A kept site no call used for 7 days — gone, renamed, never answering — is
// dropped before the restore, so it stops costing every new browser a write.
// witness: internal/ops/browser_signin.go | 		if now.Sub(o.lastUsed) <= browserSignInOriginMaxAge { → 		if true {
func TestRestoreBrowserSignIn_ASiteUnusedForAWeekIsDropped(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	now := time.Now()
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, time.Hour, 8*24*time.Hour))
	if _, err := restoreBrowserSignIn(t.Context(), browserOn(new([][]string)), dir, filepath.Join(t.TempDir(), "p.json"), now); err != nil {
		t.Fatal(err)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18740"}) || slices.Contains(chrome.sites(), "http://127.0.0.1:18741") {
		t.Errorf("a site unused for 8 days must be dropped, not restored: kept %v, restored %v", got, chrome.sites())
	}
}

// At most the 20 most recently used sites are kept, so a restore stays
// bounded however many sites the Mate ever opened.
// witness: internal/ops/browser_signin.go | 	if len(kept) > browserSignInOriginCap { → 	if false {
func TestRestoreBrowserSignIn_KeepsTheTwentyMostRecentlyUsedSites(t *testing.T) {
	newFakeChrome().dialedBy(t)
	now := time.Now()
	ages := make([]time.Duration, 25)
	for i := range ages {
		ages[i] = time.Duration(25-i) * time.Minute
	}
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, ages...))
	if _, err := restoreBrowserSignIn(t.Context(), browserOn(new([][]string)), dir, filepath.Join(t.TempDir(), "p.json"), now); err != nil {
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
	chrome := newFakeChrome().dialedBy(t)
	chrome.cookies = []map[string]any{{"name": "sid", "value": "new", "domain": "127.0.0.1", "path": "/"}}
	chrome.storage["http://127.0.0.1:18731"] = []browserStorageItem{{"token", "a2"}}
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18731/"), dir, presenceIn(t, browserPresence{CookiesHeld: true}), "t2", time.Now()); err != nil {
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
// witness: internal/ops/browser_signin.go | 	for _, name := range append(slices.Clone(p.Opened), ownPage) { → 	for _, name := range slices.Clone(p.Opened) {
func TestSaveBrowserSignIn_TheWayOutEmptiesWhatItClears(t *testing.T) {
	newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18731/"), dir, presenceIn(t, browserPresence{CookiesHeld: true}), "t2", time.Now()); err != nil {
		t.Fatalf("saveBrowserSignIn: %v", err)
	}
	if kept := keptState(t, dir); kept != `{"cookies":[],"origins":[]}` {
		t.Errorf("the way out must leave nothing kept: %s", kept)
	}
}

// A clear sticks wherever the page went afterwards: a site an open step
// ended on whose storage the save finds empty — its own sign-out cleared it,
// then sent the page to an identity provider — keeps nothing.
// witness: internal/ops/browser_signin.go | 		seen[name] = true → 		_ = name
func TestSaveBrowserSignIn_AClearSticksWhereverThePageWent(t *testing.T) {
	newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18731"}})
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 https://login.example.test/authorize"), dir, presence, "t2", time.Now()); err != nil {
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
	newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeKept(t, dir)
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18731/"), dir, presenceIn(t, browserPresence{}), "t2", time.Now()); err == nil {
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
	chrome := newFakeChrome().dialedBy(t)
	chrome.storage["http://127.0.0.1:18741"] = []browserStorageItem{{"token", "v"}}
	now := time.Now()
	dir := readyStateDir(t)
	writeState(t, dir, usedSites(now, 6*24*time.Hour, 6*24*time.Hour))
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18741"}})
	var seen [][]string
	if err := saveBrowserSignIn(t.Context(), browserOn(&seen, "t2 http://127.0.0.1:18741/"), dir, presence, "t2", now); err != nil {
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

// A save that runs out of its bound — a read on the base tab that never
// comes back — leaves the kept sign-in exactly as it was and gives the base
// tab back; it never waits past its bound.
// witness: internal/ops/browser_signin.go | 			if ctxErr := ctx.Err(); ctxErr != nil { → 			if ctxErr := ctx.Err(); ctxErr == context.Canceled {
func TestSaveBrowserSignIn_ASaveThatRunsOutKeepsTheLastSave(t *testing.T) {
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18731", "http://127.0.0.1:18736"}})
	synctest.Test(t, func(t *testing.T) {
		chrome := newFakeChrome().dialedBy(t)
		chrome.stall["http://127.0.0.1:18736"] = true
		ctx, cancel := context.WithTimeout(context.Background(), browserSignInSaveTimeout)
		defer cancel()
		start := time.Now()
		if err := saveBrowserSignIn(ctx, browserOn(new([][]string), "t2 http://127.0.0.1:18731/"), dir, presence, "t2", time.Now()); err == nil {
			t.Error("a save that ran out must be reported")
		}
		if waited := time.Since(start); waited > browserSignInSaveTimeout+browserStubCloseTimeout {
			t.Errorf("the save waited %s", waited)
		}
		if left := chrome.leftBehind(); len(left) != 0 {
			t.Errorf("the browser must be left as agent-browser had it: %v", left)
		}
	})
	if got := keptState(t, dir); got != signedInState {
		t.Errorf("a save that ran out must not replace the kept sign-in: %q", got)
	}
}

// A save opens no tab of its own and gives the borrowed base tab back on
// about:blank with no DevTools session of zcp's left on it: agent-browser
// tracks every tab it finds, and one opened and closed behind its back left
// the hand-back closing a tab agent-browser no longer had, which stopped the
// warm browser after every call.
// witness: internal/ops/browser_cdp.go | 	if err := s.cdp.call(cctx, "", "Target.detachFromTarget", map[string]any{"sessionId": s.session}, nil); err != nil { → 	if err := error(nil); err != nil {
func TestSaveBrowserSignIn_OpensNoTabAndGivesTheBaseTabBack(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.storage["http://127.0.0.1:18731"] = []browserStorageItem{{"token", "app"}}
	dir := readyStateDir(t)
	err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18731/"), dir,
		presenceIn(t, browserPresence{CookiesHeld: true}), "t2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731"}) {
		t.Errorf("the save must read the site on the base tab: %v", got)
	}
	if left := chrome.leftBehind(); len(left) != 0 {
		t.Errorf("the browser must be left as agent-browser had it: %v", left)
	}
}

// A save whose connection reached another Chrome than the one agent-browser
// listed — relaunched empty in between — saves nothing, so an empty Chrome's
// cookies never replace the kept ones.
// witness: internal/ops/browser_signin.go | 	if err := cdp.holds(ctx, base); err != nil { → 	if err := error(nil); err != nil {
func TestSaveBrowserSignIn_AnotherChromeThanTheListedOneSavesNothing(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	delete(chrome.pages, baseTarget)
	dir := readyStateDir(t)
	writeKept(t, dir)
	if err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 about:blank"), dir, presenceIn(t, browserPresence{CookiesHeld: true}), "t2", time.Now()); err == nil {
		t.Error("a save from another Chrome must be refused")
	}
	if keptState(t, dir) != signedInState {
		t.Errorf("the kept sign-in must stay as it was: %s", keptState(t, dir))
	}
}

// A kept site whose read fails on the base tab keeps its kept storage: a
// failed read says nothing of what the site holds.
// witness: internal/ops/browser_signin.go | 			items, err := stub.readStorage(ctx, name) → 			items, _ := stub.readStorage(ctx, name)
func TestSaveBrowserSignIn_ASiteWhoseReadFailsKeepsItsStorage(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.fail["http://127.0.0.1:18731"] = true
	dir := readyStateDir(t)
	writeKept(t, dir)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18731"}})
	if err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18731/"), dir, presence, "t2", time.Now()); err != nil {
		t.Fatal(err)
	}
	if kept := keptState(t, dir); !strings.Contains(kept, `"xyz789"`) {
		t.Errorf("a site whose read failed must keep its storage: %s", kept)
	}
}

// The save reads the call's own page, never a popup's: a popup still open
// on an identity provider as the call ends does not put the provider's
// storage in the kept file.
// witness: internal/ops/browser_signin.go | 	if i := slices.IndexFunc(tabs, func(t browserTab) bool { return t.ID == ownTab }); i > 0 { → 	if i := len(tabs) - 1; i > 0 {
func TestSaveBrowserSignIn_ReadsTheCallsOwnPageNotAPopups(t *testing.T) {
	chrome := newFakeChrome().dialedBy(t)
	chrome.storage["http://127.0.0.1:18731"] = []browserStorageItem{{"token", "app"}}
	chrome.storage["https://login.example.test"] = []browserStorageItem{{"pkce", "provider"}}
	dir := readyStateDir(t)
	err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18731/", "t3 https://login.example.test/authorize"), dir,
		presenceIn(t, browserPresence{CookiesHeld: true}), "t2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := keptOrigins(t, dir); !slices.Equal(got, []string{"http://127.0.0.1:18731"}) {
		t.Errorf("the save must read the call's own page alone: %v", got)
	}
}

// A site this browser did not get restored — its restore ran out — and a
// call then opened is not erased by the save: an empty read of it says
// nothing of its kept storage.
// witness: internal/ops/browser_signin.go | 		if name != "" && !slices.Contains(p.Unrestored, name) && !slices.Contains(read, name) { → 		if name != "" && !slices.Contains(read, name) {
func TestSaveBrowserSignIn_ASiteNotRestoredIsNotErasedByOpeningIt(t *testing.T) {
	newFakeChrome().dialedBy(t)
	dir := readyStateDir(t)
	writeState(t, dir, threeSites)
	presence := presenceIn(t, browserPresence{CookiesHeld: true, Opened: []string{"http://127.0.0.1:18735"}, Unrestored: []string{"http://127.0.0.1:18735"}})
	if err := saveBrowserSignIn(t.Context(), browserOn(new([][]string), "t2 http://127.0.0.1:18735/"), dir, presence, "t2", time.Now()); err != nil {
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
	run := browserOn(new([][]string))
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
