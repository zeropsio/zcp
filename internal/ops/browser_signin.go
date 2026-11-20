package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The kept sign-in (docs/spec-mate.md §2.6c): the cookies and local storage
// of the warm browser's default context, kept in one file zcp owns so a
// replaced browser starts signed in. zcp restores it into a browser started
// for a tab or new-browser call and merges what the browser holds into it at
// the end of every call that hands the browser back warm, both through
// Chrome's DevTools protocol itself (browser_cdp.go): no site is loaded for
// either, so no slow or dead site holds one up. The browser's start restores
// nothing. The file keeps agent-browser's state format.
const (
	browserSignInFile = "zcp-default.json"
	// browserSignInAside is a kept file zcp could not read, set aside for a
	// look and never restored again; one copy at most.
	browserSignInAside = browserSignInFile + ".aside"
	// browserStateTmp holds a save's candidate until zcp renames it.
	browserStateTmp = ".tmp"
	// browserSignInMaxAge: a starting browser removes a kept or set-aside
	// file no save touched for a week.
	browserSignInMaxAge = 7 * 24 * time.Hour
	// browserSignInRestoreTimeout bounds the whole restore: the cookies and
	// every kept origin's local storage (20 origins took 0.2 s, measured
	// with Chrome 157).
	browserSignInRestoreTimeout = 5 * time.Second
	// browserSignInSaveTimeout bounds the save at a call's end, inside the
	// hand-back's own bound.
	browserSignInSaveTimeout = 5 * time.Second
	// browserSignInOriginMaxAge drops a kept origin no call used for a week.
	browserSignInOriginMaxAge = 7 * 24 * time.Hour
	// browserSignInOriginCap keeps the 20 most recently used origins.
	browserSignInOriginCap = 20
)

// browserKeepsSignIn reports whether a call keeps the sign-in across browser
// restarts. A fresh-context call never does: its context starts and stays
// signed out. Nor does any call whose home, its symlinks resolved, would put
// the saved cookies outside a private home: a relative one, /, or the
// workspace, which holds git checkouts and is mounted into dev services.
func browserKeepsSignIn(isolation, home string) bool {
	if isolation == BrowserIsolationFreshContext || !filepath.IsAbs(home) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	home = filepath.Clean(resolved)
	return home != "/" && home != mountBase && !strings.HasPrefix(home, mountBase+"/")
}

// browserStateDir is the folder of the kept sign-in under the browser's
// home, agent-browser's own sessions folder.
func browserStateDir(home string) string {
	return filepath.Join(home, ".agent-browser", "sessions")
}

// readyBrowserState readies stateDir before a browser that keeps the
// sign-in starts. ~/.agent-browser, the folder and its temporary folder are
// made one by one, each the user's alone (0700, a looser mode tightened),
// and refused when one is a symlink. zcp's own files in it (the kept file,
// its set-aside copy, a save's candidate) are 0600, a symlink in their place
// is removed, never followed, and one no save touched for
// browserSignInMaxAge is removed. Other programs' files in the folder are
// left alone.
func readyBrowserState(stateDir string, now time.Time) error {
	tmp := filepath.Join(stateDir, browserStateTmp)
	for _, dir := range []string{filepath.Dir(stateDir), stateDir, tmp} {
		if err := privateBrowserDir(dir); err != nil {
			return err
		}
	}
	for _, file := range []string{
		filepath.Join(stateDir, browserSignInFile),
		filepath.Join(stateDir, browserSignInAside),
		filepath.Join(tmp, browserSignInFile),
	} {
		if err := privateBrowserFile(file, now); err != nil {
			return err
		}
	}
	return nil
}

// privateBrowserDir makes dir, or takes from its mode every bit outside
// 0700; it refuses a symlink or anything but a directory.
func privateBrowserDir(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return fmt.Errorf("browser state: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("browser state: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("browser state: %s is a symlink or not a directory", dir)
	}
	if info.Mode().Perm()&^0o700 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("browser state: %w", err)
		}
	}
	return nil
}

// privateBrowserFile removes file when it is anything but a regular file
// (a symlink is removed itself) or older than browserSignInMaxAge, and
// takes from a kept one's mode every bit outside 0600.
func privateBrowserFile(file string, now time.Time) error {
	info, err := os.Lstat(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("browser state: %w", err)
	}
	if !info.Mode().IsRegular() || now.Sub(info.ModTime()) > browserSignInMaxAge {
		if err := os.Remove(file); err != nil {
			return fmt.Errorf("browser state: %w", err)
		}
		return nil
	}
	if info.Mode().Perm()&^0o600 != 0 {
		if err := os.Chmod(file, 0o600); err != nil {
			return fmt.Errorf("browser state: %w", err)
		}
	}
	return nil
}

// browserSavedOrigin is one origin of a kept state: its name, its entry as
// kept (agent-browser's fields and zcp's last use), whether it keeps any
// local storage (only those are restored), and when a call last used it,
// zero when the entry says nothing.
type browserSavedOrigin struct {
	name     string
	raw      json.RawMessage
	storage  bool
	lastUsed time.Time
}

// browserLastUsedField is zcp's own field on a kept origin: when a call last
// opened the origin or saved on its page. agent-browser 0.39.1, which kept
// the file before zcp did, ignores it: its OriginStorage
// (cli/src/native/state.rs) derives Deserialize without deny_unknown_fields.
const browserLastUsedField = "zcpLastUsed"

// parseBrowserState splits a kept state into its cookies and its origins, as
// listed; ok is false for any other content (validBrowserState).
func parseBrowserState(data []byte) (cookies json.RawMessage, origins []browserSavedOrigin, ok bool) {
	if !validBrowserState(data) {
		return nil, nil, false
	}
	var state struct {
		Cookies json.RawMessage   `json:"cookies"`
		Origins []json.RawMessage `json:"origins"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil, nil, false
	}
	for _, raw := range state.Origins {
		var o struct {
			Origin       string            `json:"origin"`
			LocalStorage []json.RawMessage `json:"localStorage"`
			LastUsed     time.Time         `json:"zcpLastUsed"`
		}
		if json.Unmarshal(raw, &o) != nil {
			return nil, nil, false
		}
		origins = append(origins, browserSavedOrigin{name: o.Origin, raw: raw, storage: len(o.LocalStorage) > 0, lastUsed: o.LastUsed})
	}
	return state.Cookies, origins, true
}

// usedAt is o with its last use set to at.
func (o browserSavedOrigin) usedAt(at time.Time) browserSavedOrigin {
	var fields map[string]json.RawMessage
	if json.Unmarshal(o.raw, &fields) != nil {
		return o
	}
	stamp, err := json.Marshal(at.UTC().Truncate(time.Second))
	if err != nil {
		return o
	}
	fields[browserLastUsedField] = stamp
	raw, err := json.Marshal(fields)
	if err != nil {
		return o
	}
	o.raw, o.lastUsed = raw, at.UTC().Truncate(time.Second)
	return o
}

// encodeBrowserState is a state of these cookies and origins.
func encodeBrowserState(cookies json.RawMessage, origins []browserSavedOrigin) []byte {
	if len(cookies) == 0 {
		cookies = json.RawMessage("[]")
	}
	raws := make([]string, 0, len(origins))
	for _, o := range origins {
		raws = append(raws, string(o.raw))
	}
	// Both parts came out of a parsed state, so they are JSON as they stand.
	return []byte(`{"cookies":` + string(cookies) + `,"origins":[` + strings.Join(raws, ",") + `]}`)
}

// pruneBrowserOrigins keeps the origins a call used within
// browserSignInOriginMaxAge, the most recently used first and at most
// browserSignInOriginCap of them, so a restore stays bounded and a site no
// call opens any more — gone, renamed, never answering — leaves the kept
// file. An origin whose entry names no use counts as used now.
func pruneBrowserOrigins(origins []browserSavedOrigin, now time.Time) []browserSavedOrigin {
	kept := make([]browserSavedOrigin, 0, len(origins))
	for _, o := range origins {
		if o.lastUsed.IsZero() {
			o = o.usedAt(now)
		}
		if now.Sub(o.lastUsed) <= browserSignInOriginMaxAge {
			kept = append(kept, o)
		}
	}
	slices.SortStableFunc(kept, func(a, b browserSavedOrigin) int { return b.lastUsed.Compare(a.lastUsed) })
	if len(kept) > browserSignInOriginCap {
		kept = kept[:browserSignInOriginCap]
	}
	return kept
}

// browserPresence is what zcp knows of the kept sign-in in the running
// browser, kept in its runtime folder, which dies with it. CookiesHeld says
// the browser holds the kept cookies — their restore succeeded, or none were
// kept — so a save may replace them. Opened lists the origins an open step's
// page ended on in this browser: a save reads the local storage of exactly
// those and of the call's own page, never of an origin it only restored.
// Unrestored lists the kept origins with storage this browser did not get
// restored: an empty read of one says nothing of its kept storage.
// BaseTarget is the base tab's CDP target when the kept sign-in was
// restored: agent-browser relaunches a Chrome whose process exited or whose
// connection died (an OOM kill) on the next command, with a new, empty
// profile and a new base tab, inside the same service (cdp/chrome.rs,
// actions.rs), so another base tab means the kept sign-in is no longer in
// the browser.
type browserPresence struct {
	CookiesHeld bool     `json:"cookiesHeld"`
	Opened      []string `json:"opened"`
	Unrestored  []string `json:"unrestored,omitempty"`
	BaseTarget  string   `json:"baseTarget"`
}

// browserTab is a tab of agent-browser's tab list: its id and its page's
// origin ("" for about:blank and the like).
type browserTab struct {
	ID, Origin string
}

// browserTabs reads agent-browser's tab list: the base tab's CDP target —
// the first tab listed, the one the browser started with; a tab opened later,
// a stub tab among them, is listed after it — and every tab.
func browserTabs(ctx context.Context, run browserStepExec) (baseTarget string, tabs []browserTab, err error) {
	out, _, err := run(ctx, []string{"tab", "list"})
	if err != nil {
		return "", nil, fmt.Errorf("tab list: %w", err)
	}
	var answer struct {
		Data struct {
			Tabs []struct {
				TabID    string `json:"tabId"`
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
			} `json:"tabs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); err != nil {
		return "", nil, fmt.Errorf("tab list: %w", err)
	}
	if len(answer.Data.Tabs) == 0 {
		return "", nil, errors.New("tab list: no tabs")
	}
	for _, tab := range answer.Data.Tabs {
		tabs = append(tabs, browserTab{ID: tab.TabID, Origin: browserOrigin(tab.URL)})
	}
	return answer.Data.Tabs[0].TargetID, tabs, nil
}

// keptSignInLetsReuse reports whether a call that keeps the sign-in may run
// on the warm browser: its presence says it holds the kept cookies and its
// base tab is still the one the kept sign-in was restored into, so Chrome
// was not relaunched empty since. Otherwise the call starts a fresh browser
// and restores the kept sign-in into it.
func keptSignInLetsReuse(ctx context.Context, run browserStepExec, presencePath string) bool {
	p, ok := readBrowserPresence(presencePath)
	if !ok || !p.CookiesHeld {
		return false
	}
	base, _, err := browserTabs(ctx, run)
	return err == nil && base != "" && base == p.BaseTarget
}

// readBrowserPresence reads the running browser's presence; ok is false
// when it has none — a browser that never restored the kept sign-in.
func readBrowserPresence(path string) (browserPresence, bool) {
	data, err := readBrowserFile(path)
	if err != nil {
		return browserPresence{}, false
	}
	var p browserPresence
	if json.Unmarshal(data, &p) != nil {
		return browserPresence{}, false
	}
	return p, true
}

// writeBrowserPresence writes the running browser's presence, 0600, never
// through a symlink.
func writeBrowserPresence(path string, p browserPresence) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("browser presence: %w", err)
	}
	return writeBrowserFile(path, data)
}

// recordBrowserOpened adds to the running browser's presence the origin each
// open step of a walk ended on, read from the step's answer (stdout is the
// walk's []BrowserStepResult). A browser with no presence keeps no sign-in
// and gets none.
func recordBrowserOpened(path, stdout string) error {
	p, ok := readBrowserPresence(path)
	if !ok {
		return nil
	}
	var steps []BrowserStepResult
	if err := json.Unmarshal([]byte(stdout), &steps); err != nil {
		return fmt.Errorf("record the origins a walk opened: %w", err)
	}
	changed := false
	for _, s := range steps {
		if !s.Success || !isCommand(s.Command, browserCmdOpen) {
			continue
		}
		var page struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(s.Result, &page) != nil {
			continue
		}
		if origin := browserOrigin(page.URL); origin != "" && !slices.Contains(p.Opened, origin) {
			p.Opened = append(p.Opened, origin)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeBrowserPresence(path, p)
}

// browserOrigin is an http(s) URL's origin as a page states it, "" for any
// other URL (about:blank and the like).
func browserOrigin(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" && (u.Scheme == httpScheme || u.Scheme == httpsScheme) {
		return u.Scheme + "://" + u.Host
	}
	return ""
}

// restoreBrowserSignIn restores the kept sign-in into a browser just started
// for the call and writes the browser's presence (browserPresence) to
// presencePath. The kept origins are pruned first (pruneBrowserOrigins). A
// kept file zcp cannot read is set aside, never restored again, and the call
// runs signed out. Otherwise, through Chrome's DevTools protocol under one
// bound (browserSignInRestoreTimeout): every kept cookie, set browser-wide —
// one Chrome refuses costs only itself —, then each origin's local storage,
// most recently used first, written on one stub tab whose requests zcp
// answers itself, so no site is loaded and none can stall the restore. An
// origin whose write fails is dropped from the kept file and named; one the
// bound cuts off is neither, stays kept and is named as not restored, as is
// every origin when the cookies did not get in: the browser then does not
// hold the kept cookies, so its saves are refused and they stay kept. note is
// what the call's result says, "" when everything was restored; err is a
// presence write that failed.
func restoreBrowserSignIn(ctx context.Context, run browserStepExec, stateDir, presencePath string, now time.Time) (note string, err error) {
	var p browserPresence
	done := func(note string) (string, error) {
		// The base tab the kept sign-in is in now; none read leaves the
		// browser holding nothing a save may replace.
		if base, _, err := browserTabs(ctx, run); err == nil {
			p.BaseTarget = base
		} else {
			logBrowserSignIn(fmt.Errorf("read the base tab after the restore: %w", err))
			p.CookiesHeld = false
		}
		return note, writeBrowserPresence(presencePath, p)
	}
	file := filepath.Join(stateDir, browserSignInFile)
	data, err := readBrowserFile(file)
	if errors.Is(err, os.ErrNotExist) {
		p.CookiesHeld = true
		return done("")
	}
	cookies, origins, ok := parseBrowserState(data)
	var kept []browserCookie
	if ok {
		ok = json.Unmarshal(cookies, &kept) == nil
	}
	if err != nil || !ok {
		logBrowserSignIn(fmt.Errorf("the kept sign-in %s is not a kept state zcp can read: %w", file, err))
		setBrowserSignInAside(stateDir)
		p.CookiesHeld = true
		return done(browserSignInUnreadable)
	}
	if pruned := pruneBrowserOrigins(origins, now); len(pruned) != len(origins) {
		origins = pruned
		keepBrowserSignInWithout(stateDir, cookies, origins)
	} else {
		origins = pruned
	}
	rctx, cancel := context.WithTimeout(ctx, browserSignInRestoreTimeout)
	defer cancel()
	var stored []browserSavedOrigin
	for _, o := range origins {
		if o.storage {
			stored = append(stored, o)
		}
	}
	names := func(os []browserSavedOrigin) []string {
		out := make([]string, 0, len(os))
		for _, o := range os {
			out = append(out, o.name)
		}
		return out
	}
	notHeld := func(err error) (string, error) {
		logBrowserSignIn(fmt.Errorf("restore the cookies of the kept sign-in %s: %w", file, err))
		p.Unrestored = names(stored)
		return done(browserSignInNotHeld)
	}
	cdp, err := connectBrowserCDP(rctx, run)
	if err != nil {
		return notHeld(err)
	}
	defer cdp.close()
	refused, err := cdp.setCookies(rctx, kept)
	if err != nil {
		return notHeld(err)
	}
	if refused > 0 {
		logBrowserSignIn(fmt.Errorf("%d of the %d kept cookies of %s were refused by Chrome", refused, len(kept), file))
	}
	p.CookiesHeld = true
	dropped, missing := restoreBrowserStorage(rctx, cdp, stored)
	if len(dropped) > 0 {
		keepBrowserSignInWithout(stateDir, cookies, slices.DeleteFunc(slices.Clone(origins), func(o browserSavedOrigin) bool {
			return slices.Contains(dropped, o.name)
		}))
	}
	p.Unrestored = missing
	return done(browserSignInNote(dropped, missing))
}

// restoreBrowserStorage writes each origin's kept local storage on one stub
// tab. dropped are the origins whose write failed while the
// bound held; missing those it did not reach or whose write the bound cut
// off — every origin when the stub tab does not open.
func restoreBrowserStorage(ctx context.Context, cdp *browserCDP, origins []browserSavedOrigin) (dropped, missing []string) {
	if len(origins) == 0 {
		return nil, nil
	}
	err := cdp.withStub(ctx, func(stub *browserStub) error {
		for _, o := range origins {
			if ctx.Err() != nil {
				missing = append(missing, o.name)
				continue
			}
			items, err := o.localStorage()
			if err == nil {
				err = stub.writeStorage(ctx, o.name, items)
			}
			if err == nil {
				continue
			}
			logBrowserSignIn(fmt.Errorf("restore the local storage of %s: %w", o.name, err))
			if bounded := ctx.Err() != nil; bounded {
				missing = append(missing, o.name)
			} else {
				dropped = append(dropped, o.name)
			}
		}
		return nil
	})
	if err != nil {
		logBrowserSignIn(fmt.Errorf("open the stub tab to restore local storage: %w", err))
		for _, o := range origins {
			missing = append(missing, o.name)
		}
	}
	return dropped, missing
}

// localStorage is the origin's kept local storage.
func (o browserSavedOrigin) localStorage() ([]browserStorageItem, error) {
	var entry struct {
		LocalStorage []browserStorageItem `json:"localStorage"`
	}
	if err := json.Unmarshal(o.raw, &entry); err != nil {
		return nil, fmt.Errorf("kept origin %s: %w", o.name, err)
	}
	return entry.LocalStorage, nil
}

// keepBrowserSignInWithout rewrites the kept file with these cookies and
// origins.
func keepBrowserSignInWithout(stateDir string, cookies json.RawMessage, origins []browserSavedOrigin) {
	tmp := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := writeBrowserFile(tmp, encodeBrowserState(cookies, origins)); err != nil {
		logBrowserSignIn(fmt.Errorf("rewrite the kept sign-in: %w", err))
		return
	}
	if err := os.Rename(tmp, filepath.Join(stateDir, browserSignInFile)); err != nil {
		logBrowserSignIn(fmt.Errorf("rewrite the kept sign-in: %w", err))
	}
}

// setBrowserSignInAside renames the kept file over the set-aside copy; one
// it cannot rename is removed, so it is never restored again.
func setBrowserSignInAside(stateDir string) {
	file := filepath.Join(stateDir, browserSignInFile)
	if err := os.Rename(file, filepath.Join(stateDir, browserSignInAside)); err != nil {
		logBrowserSignIn(fmt.Errorf("set the kept sign-in %s aside: %w", file, err))
		_ = os.Remove(file)
	}
}

// logBrowserSignIn logs a restore or save of the kept sign-in that failed on
// zcp's stderr: the error names the file or the origin and carries Chrome's
// own message, never a value.
func logBrowserSignIn(err error) {
	fmt.Fprintf(os.Stderr, "zerops_browser: kept sign-in: %v\n", err)
}

// What a call whose new browser could not restore all of the kept sign-in
// says (BrowserBatchResult.KeptSignIn).
const (
	browserSignInUnreadable = "The kept sign-in could not be restored: its file could not be read, so it was set aside. " +
		"This call ran signed out; signing in again keeps the new sign-in."
	browserSignInNotHeld = "The kept sign-in's cookies were not restored in this call. " +
		"They are still kept for the next new browser; a sign-in made in this browser is not kept."
	browserSignInLostInTheCall = "Chrome restarted empty during this call, so the kept sign-in was not in it: " +
		"it is still kept for the next call, and a sign-in made in this call was not kept."
)

// browserSignInNote says what a restore that set the cookies could not bring
// back: the origins whose saved storage it dropped and those it did not
// restore in this call and still keeps; "" when everything was restored.
func browserSignInNote(dropped, missing []string) string {
	var parts []string
	if len(dropped) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was dropped: Chrome refused to take it back while this call restored the kept sign-in. "+
			"Sign in there again if it needs it.", browserSites(dropped)))
	}
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was not restored in this call, whose restore ran out of time; it is still kept.", browserSites(missing)))
	}
	if len(parts) > 0 && len(missing) == 0 {
		parts = append(parts, "The rest of the kept sign-in was restored.")
	}
	return strings.Join(parts, " ")
}

// browserSites names up to three sites and how many more.
func browserSites(sites []string) string {
	if len(sites) > 3 {
		return fmt.Sprintf("%s and %d more", strings.Join(sites[:3], ", "), len(sites)-3)
	}
	return strings.Join(sites, ", ")
}

// errBrowserSignInNotHeld refuses a save from a browser that does not hold
// the kept cookies: its cookies would replace them.
var errBrowserSignInNotHeld = errors.New("the browser does not hold the kept cookies, so nothing was saved; they stay kept")

// errBrowserRelaunched refuses a save from a browser whose Chrome agent-
// browser relaunched, empty, since the kept sign-in was restored into it.
var errBrowserRelaunched = errors.New("the browser's Chrome was relaunched with an empty profile since the kept sign-in was restored, so nothing was saved; it stays kept")

// saveBrowserSignIn merges what the browser holds into the kept sign-in,
// when it holds the kept cookies (browserPresence; errBrowserSignInNotHeld
// otherwise) in the Chrome they were restored into: a browser whose base tab
// is another one was relaunched empty, so nothing is saved
// (errBrowserRelaunched) and its presence then says it holds nothing, so no
// later call reuses it. ownTab is the call's own tab, whose page's origin the
// save reads with the origins the browser opened (browserSaveRead). Through
// Chrome's DevTools protocol: every cookie of the default context, and the
// local storage of each origin read, on one stub tab whose requests zcp
// answers itself, so no site is loaded. An origin whose read fails is left
// out of the merge and keeps its kept entry. Session storage is never read.
// The result (mergeBrowserState) is written into the folder's temporary
// folder (any file or symlink there is removed first) and renamed over the
// kept file once it is a kept state; a save cut short leaves the kept file as
// it was.
func saveBrowserSignIn(ctx context.Context, run browserStepExec, stateDir, presencePath, ownTab string, now time.Time) error {
	presence, ok := readBrowserPresence(presencePath)
	if !ok || !presence.CookiesHeld {
		return errBrowserSignInNotHeld
	}
	candidate := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear the candidate %s: %w", candidate, err)
	}
	base, tabs, err := browserTabs(ctx, run)
	if err != nil {
		return err
	}
	if base != presence.BaseTarget {
		if err := writeBrowserPresence(presencePath, browserPresence{}); err != nil {
			logBrowserSignIn(err)
		}
		return errBrowserRelaunched
	}
	ownPage := ""
	if i := slices.IndexFunc(tabs, func(t browserTab) bool { return t.ID == ownTab }); i > 0 {
		ownPage = tabs[i].Origin
	}
	cdp, err := connectBrowserCDP(ctx, run)
	if err != nil {
		return err
	}
	defer cdp.close()
	cookies, err := cdp.cookies(ctx)
	if err != nil {
		return err
	}
	origins, read, err := readBrowserStorage(ctx, cdp, browserSaveRead(presence, ownPage))
	if err != nil {
		return err
	}
	cookieJSON, err := json.Marshal(cookies)
	if err != nil {
		return fmt.Errorf("encode the saved cookies: %w", err)
	}
	saved := encodeBrowserState(cookieJSON, origins)
	if !validBrowserState(saved) {
		return errors.New("the saved state is not a kept state")
	}
	old, _ := readBrowserFile(filepath.Join(stateDir, browserSignInFile))
	if err := writeBrowserFile(candidate, mergeBrowserState(old, saved, read, now)); err != nil {
		return fmt.Errorf("write the saved state %s: %w", candidate, err)
	}
	if err := os.Rename(candidate, filepath.Join(stateDir, browserSignInFile)); err != nil {
		return fmt.Errorf("keep the saved state %s: %w", candidate, err)
	}
	return nil
}

// readBrowserStorage reads each origin's local storage on one stub tab:
// origins are those holding any, as kept entries, and read
// those whose read succeeded. A read that fails is logged and leaves its
// origin out of read; err is a stub tab that did not open, or ctx's end.
func readBrowserStorage(ctx context.Context, cdp *browserCDP, names []string) (origins []browserSavedOrigin, read []string, err error) {
	if len(names) == 0 {
		return nil, nil, nil
	}
	err = cdp.withStub(ctx, func(stub *browserStub) error {
		for _, name := range names {
			items, err := stub.readStorage(ctx, name)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("read the local storage of %s: %w", name, ctxErr)
			}
			if err != nil {
				logBrowserSignIn(fmt.Errorf("read the local storage of %s: %w", name, err))
				continue
			}
			read = append(read, name)
			if len(items) == 0 {
				continue
			}
			raw, err := json.Marshal(struct {
				Origin       string               `json:"origin"`
				LocalStorage []browserStorageItem `json:"localStorage"`
			}{name, items})
			if err != nil {
				return fmt.Errorf("encode the local storage of %s: %w", name, err)
			}
			origins = append(origins, browserSavedOrigin{name: name, raw: raw, storage: true})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return origins, read, nil
}

// browserSaveRead lists the origins a save reads: those an open step ended
// on in this browser, and ownPage, the call's own page's. An origin this
// browser did not get restored is left out: an empty read of it must not
// erase its kept storage.
func browserSaveRead(p browserPresence, ownPage string) []string {
	var read []string
	for _, name := range append(slices.Clone(p.Opened), ownPage) {
		if name != "" && !slices.Contains(p.Unrestored, name) && !slices.Contains(read, name) {
			read = append(read, name)
		}
	}
	return read
}

// mergeBrowserState is the kept sign-in after a save. The saved cookies
// replace the kept ones: the browser holds every kept cookie, so its
// cookies are the whole truth. read lists the origins the save read — those
// an open step ended on in this browser and the call's own page —, each used
// now: each is what the save holds for it, nothing when the save lacks it
// (a save holds only origins with storage, so a clear sticks wherever the
// page went afterwards). An origin the save holds that it did not read
// through them keeps its last use. A kept origin the save did not read keeps
// its kept entry: the save never reads an origin the browser only restored.
// The result is pruned (pruneBrowserOrigins). A kept file that is not a kept
// state adds nothing.
func mergeBrowserState(old, saved []byte, read []string, now time.Time) []byte {
	cookies, savedOrigins, ok := parseBrowserState(saved)
	if !ok {
		return saved
	}
	_, keptOrigins, _ := parseBrowserState(old)
	kept := map[string]browserSavedOrigin{}
	for _, o := range keptOrigins {
		kept[o.name] = o
	}
	seen := map[string]bool{}
	for _, name := range read {
		seen[name] = true
	}
	var origins []browserSavedOrigin
	for _, o := range savedOrigins {
		switch prev, wasKept := kept[o.name]; {
		case seen[o.name] || !wasKept || prev.lastUsed.IsZero():
			o = o.usedAt(now)
		default:
			o = o.usedAt(prev.lastUsed)
		}
		origins = append(origins, o)
		seen[o.name] = true
	}
	for _, o := range keptOrigins {
		if !seen[o.name] {
			origins = append(origins, kept[o.name])
		}
	}
	return encodeBrowserState(cookies, pruneBrowserOrigins(origins, now))
}

// validBrowserState reports whether data is a kept state: agent-browser
// 0.39.1's state format, its cookies and origins, each cookie with its name,
// value, domain and path, each origin with its origin and local storage,
// every field of the type agent-browser reads.
func validBrowserState(data []byte) bool {
	type entry struct {
		Name  *string `json:"name"`
		Value *string `json:"value"`
	}
	var state struct {
		Cookies *[]struct {
			Name     *string `json:"name"`
			Value    *string `json:"value"`
			Domain   *string `json:"domain"`
			Path     *string `json:"path"`
			Expires  float64 `json:"expires"`
			Size     int64   `json:"size"`
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
			Session  bool    `json:"session"`
			SameSite *string `json:"sameSite"`
		} `json:"cookies"`
		Origins *[]struct {
			Origin         *string  `json:"origin"`
			LocalStorage   *[]entry `json:"localStorage"`
			SessionStorage []entry  `json:"sessionStorage"`
		} `json:"origins"`
	}
	if json.Unmarshal(data, &state) != nil || state.Cookies == nil || state.Origins == nil {
		return false
	}
	for _, c := range *state.Cookies {
		if c.Name == nil || c.Value == nil || c.Domain == nil || c.Path == nil {
			return false
		}
	}
	for _, o := range *state.Origins {
		if o.Origin == nil || o.LocalStorage == nil {
			return false
		}
		for _, e := range append(slices.Clone(*o.LocalStorage), o.SessionStorage...) {
			if e.Name == nil || e.Value == nil {
				return false
			}
		}
	}
	return true
}
