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
// replaced browser starts signed in. zcp loads it into a browser started for
// a tab or new-browser call — the cookies first, then each origin's local
// storage on its own — and merges what the browser holds into it at the end
// of every call that hands the browser back warm, with agent-browser's own
// state load and state save; the browser's start restores nothing.
const (
	browserSignInFile = "zcp-default.json"
	// browserSignInAside is a kept file agent-browser could not load, set
	// aside for a look and never loaded again; one copy at most.
	browserSignInAside = browserSignInFile + ".aside"
	// browserStateTmp holds a save's candidate and the parts a restore
	// loads, until zcp renames or removes them.
	browserStateTmp = ".tmp"
	// browserSignInLoadCopy is the part of the kept file a restore loads
	// next: its cookies alone, or one origin's local storage alone.
	browserSignInLoadCopy = "zcp-load.json"
	// browserSignInMaxAge: a starting browser removes a kept or set-aside
	// file no save touched for a week.
	browserSignInMaxAge = 7 * 24 * time.Hour
	// browserSignInCookiesTimeout bounds the load of the cookies alone. It
	// opens no site, so one that runs this long is a file agent-browser
	// cannot load.
	browserSignInCookiesTimeout = 5 * time.Second
	// browserSignInOriginTimeout bounds the load of one origin's local
	// storage: agent-browser opens the origin's root and waits half a second
	// there, so a site that answers within about 3 s keeps its storage.
	browserSignInOriginTimeout = 4 * time.Second
	// browserSignInOriginLeast is the least time left worth starting an
	// origin's load in.
	browserSignInOriginLeast = time.Second
	// browserSignInRestoreTimeout bounds the whole restore: 20 healthy
	// origins took 10.7 s (measured, agent-browser 0.39.1). A call never
	// gives it more than a third of the time it has left, so one with
	// little left restores the cookies and leaves the origins kept.
	browserSignInRestoreTimeout = 20 * time.Second
	// browserSignInBaseTabTimeout bounds the base tab's way back to
	// about:blank after the last origin's load.
	browserSignInBaseTabTimeout = 3 * time.Second
	// browserSignInSaveTimeout bounds the save at a call's end, inside the
	// hand-back's own bound.
	browserSignInSaveTimeout = 5 * time.Second
	// browserSignInOriginMaxAge drops a kept origin no call used for a week.
	browserSignInOriginMaxAge = 7 * 24 * time.Hour
	// browserSignInOriginCap keeps the 20 most recently used origins, which
	// a restore loads in about 11 s.
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
		filepath.Join(tmp, browserSignInLoadCopy),
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
// local storage (only those are loaded), and when a call last used it, zero
// when the entry says nothing.
type browserSavedOrigin struct {
	name     string
	raw      json.RawMessage
	storage  bool
	lastUsed time.Time
}

// browserLastUsedField is zcp's own field on a kept origin: when a call last
// opened the origin or saved on its page. agent-browser 0.39.1 ignores it:
// its OriginStorage (cli/src/native/state.rs) derives Deserialize without
// deny_unknown_fields.
const browserLastUsedField = "zcpLastUsed"

// parseBrowserState splits a state agent-browser can load into its cookies
// and its origins, as listed; ok is false for any other content.
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
// the browser holds the kept cookies — their load, or their reload after the
// restore's last restart, succeeded, or none were kept — so a save may
// replace them. Opened lists the origins an open step's page ended on in
// this browser: agent-browser's save reads the storage of exactly those
// (and of the page it runs on), never of an origin it only restored
// (state.rs collects its visited origins, which only open adds, browser.rs).
// Unrestored lists the kept origins with storage this browser did not get
// restored: an empty read of one says nothing of its kept storage. BaseTarget
// is the base tab's CDP target when the kept sign-in was restored: agent-
// browser relaunches a Chrome whose process exited or whose connection died
// (an OOM kill) on the next command, with a new, empty profile and a new
// base tab, inside the same service (cdp/chrome.rs, actions.rs), so another
// base tab means the kept sign-in is no longer in the browser.
type browserPresence struct {
	CookiesHeld bool     `json:"cookiesHeld"`
	Opened      []string `json:"opened"`
	Unrestored  []string `json:"unrestored,omitempty"`
	BaseTarget  string   `json:"baseTarget"`
}

// browserTabs is what a save needs of agent-browser's tab list: the base
// tab's CDP target and the active tab's origin ("" for about:blank and the
// like).
func browserTabs(ctx context.Context, run browserStepExec) (baseTarget, activeOrigin string, err error) {
	out, _, err := run(ctx, []string{"tab", "list"})
	if err != nil {
		return "", "", fmt.Errorf("tab list: %w", err)
	}
	var answer struct {
		Data struct {
			Tabs []struct {
				TargetID string `json:"targetId"`
				Active   bool   `json:"active"`
				URL      string `json:"url"`
			} `json:"tabs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); err != nil {
		return "", "", fmt.Errorf("tab list: %w", err)
	}
	if len(answer.Data.Tabs) == 0 {
		return "", "", errors.New("tab list: no tabs")
	}
	for _, tab := range answer.Data.Tabs {
		if tab.Active {
			activeOrigin = browserOrigin(tab.URL)
		}
	}
	return answer.Data.Tabs[0].TargetID, activeOrigin, nil
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

// restoreBrowserSignIn loads the kept sign-in into a browser just started
// for the call, part by part, on its base tab, and writes the browser's
// presence (browserPresence) to presencePath; restart replaces that browser
// with a clean one. The kept origins are pruned first (pruneBrowserOrigins).
// The cookies go first and alone: they open no site, so a load of them that
// fails or runs out marks a file agent-browser cannot load — it is set
// aside, never loaded again, and the call runs signed out in a clean
// browser. Then each origin's local storage alone, most recently used first,
// under its own bound: one whose load fails or runs out — a site that is
// slow, paused, looping or never answering — is dropped from the kept file
// and named. Its browser is still busy with the site, so it is replaced, and
// the cookies and the origins already restored are loaded again before the
// next. The whole restore has browserSignInRestoreTimeout and at most a
// third of what the call has left: an origin it does not reach or does not
// get back, or one cut by that budget rather than by its own bound, is
// neither loaded nor dropped, stays kept and is named. Cookies that do not
// come back after a restart leave the browser without them: its saves are
// refused, so they stay kept. The base tab goes back to about:blank after
// the last origin, so no restored page keeps running. note is what the
// call's result says, "" when everything was restored; err is a restart or a
// presence write that failed.
func restoreBrowserSignIn(ctx context.Context, run browserStepExec, stateDir, presencePath string, now time.Time, restart func(context.Context) error) (note string, err error) {
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
	if err != nil {
		logBrowserSignIn(fmt.Errorf("read the kept sign-in: %w", err))
		setBrowserSignInAside(stateDir)
		p.CookiesHeld = true
		return done(browserSignInUnloadable)
	}
	cookies, origins, ok := parseBrowserState(data)
	if !ok {
		logBrowserSignIn(fmt.Errorf("the kept sign-in %s is not a state agent-browser can load", file))
		setBrowserSignInAside(stateDir)
		p.CookiesHeld = true
		return done(browserSignInUnloadable)
	}
	if pruned := pruneBrowserOrigins(origins, now); len(pruned) != len(origins) {
		origins = pruned
		keepBrowserSignInWithout(stateDir, cookies, origins)
	} else {
		origins = pruned
	}
	budget := browserSignInRestoreTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)/3)
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	timeLeft := func() bool {
		deadline, _ := rctx.Deadline()
		return rctx.Err() == nil && time.Until(deadline) >= browserSignInOriginLeast
	}
	loadCookies := func(parent context.Context) error {
		cctx, cancel := context.WithTimeout(parent, browserSignInCookiesTimeout)
		defer cancel()
		return loadBrowserSignInPart(cctx, run, stateDir, encodeBrowserState(cookies, nil))
	}
	loadOrigin := func(o browserSavedOrigin) error {
		octx, cancel := context.WithTimeout(rctx, browserSignInOriginTimeout)
		defer cancel()
		return loadBrowserSignInPart(octx, run, stateDir, encodeBrowserState(nil, []browserSavedOrigin{o}))
	}
	if err := loadCookies(rctx); err != nil {
		logBrowserSignIn(fmt.Errorf("load the cookies of the kept sign-in %s: %w", file, err))
		if rctx.Err() == nil {
			setBrowserSignInAside(stateDir)
			p.CookiesHeld = true
			if err := restart(ctx); err != nil {
				return "", err
			}
			return done(browserSignInUnloadable)
		}
		if err := restart(ctx); err != nil {
			return "", err
		}
		return done(browserSignInNotHeld)
	}
	p.CookiesHeld = true
	var restored, dropped []browserSavedOrigin
	onSite := false
	// reinstate replaces the browser and loads into the clean one the
	// cookies — from the call's own time: they loaded once and open no site
	// — and the origins already restored, each just proved fast, within the
	// restore's budget. What does not come back is no longer restored.
	reinstate := func() error {
		if err := restart(ctx); err != nil {
			return err
		}
		onSite = false
		if err := loadCookies(ctx); err != nil {
			logBrowserSignIn(fmt.Errorf("load the cookies of the kept sign-in %s after a restart: %w", file, err))
			p.CookiesHeld, restored = false, nil
			return nil
		}
		back := restored[:0:0]
		for _, o := range restored {
			if !timeLeft() {
				break
			}
			if err := loadOrigin(o); err != nil {
				logBrowserSignIn(fmt.Errorf("load the local storage of %s again: %w", o.name, err))
				if err := restart(ctx); err != nil {
					return err
				}
				back = back[:0]
				if err := loadCookies(ctx); err != nil {
					p.CookiesHeld = false
				}
				onSite = false
				break
			}
			back = append(back, o)
			onSite = true
		}
		restored = back
		return nil
	}
	for _, o := range origins {
		if !o.storage || !timeLeft() || !p.CookiesHeld {
			continue
		}
		err := loadOrigin(o)
		if err == nil {
			restored = append(restored, o)
			onSite = true
			continue
		}
		logBrowserSignIn(fmt.Errorf("load the local storage of %s from the kept sign-in %s: %w", o.name, file, err))
		// Its own bound ran out, or agent-browser answered a failure: the
		// site is at fault. The restore's budget running out is not.
		if siteAtFault := rctx.Err() == nil; siteAtFault {
			dropped = append(dropped, o)
		}
		if err := reinstate(); err != nil {
			return "", err
		}
	}
	if onSite {
		bctx, cancel := context.WithTimeout(ctx, browserSignInBaseTabTimeout)
		err := walkBrowser(bctx, [][]string{{browserCmdOpen, "about:blank"}}, run)
		cancel()
		if err != nil {
			logBrowserSignIn(fmt.Errorf("send the base tab back to about:blank: %w", err))
			if err := restart(ctx); err != nil {
				return "", err
			}
			restored = nil
			if err := loadCookies(ctx); err != nil {
				logBrowserSignIn(fmt.Errorf("load the cookies of the kept sign-in %s again: %w", file, err))
				p.CookiesHeld = false
			}
		}
	}
	if len(dropped) > 0 {
		keepBrowserSignInWithout(stateDir, cookies, slices.DeleteFunc(slices.Clone(origins), func(o browserSavedOrigin) bool {
			return slices.ContainsFunc(dropped, func(d browserSavedOrigin) bool { return d.name == o.name })
		}))
	}
	var droppedNames, missing []string
	for _, o := range dropped {
		droppedNames = append(droppedNames, o.name)
	}
	for _, o := range origins {
		if o.storage && !slices.Contains(droppedNames, o.name) && !slices.ContainsFunc(restored, func(r browserSavedOrigin) bool { return r.name == o.name }) {
			missing = append(missing, o.name)
		}
	}
	p.Unrestored = missing
	return done(browserSignInNote(droppedNames, missing, p.CookiesHeld))
}

// loadBrowserSignInPart writes part into the temporary folder and loads it.
func loadBrowserSignInPart(ctx context.Context, run browserStepExec, stateDir string, part []byte) error {
	copyPath := filepath.Join(stateDir, browserStateTmp, browserSignInLoadCopy)
	if err := writeBrowserFile(copyPath, part); err != nil {
		return err
	}
	defer func() { _ = os.Remove(copyPath) }()
	return walkBrowser(ctx, [][]string{{"state", "load", copyPath}}, run)
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
// it cannot rename is removed, so it is never loaded again.
func setBrowserSignInAside(stateDir string) {
	file := filepath.Join(stateDir, browserSignInFile)
	if err := os.Rename(file, filepath.Join(stateDir, browserSignInAside)); err != nil {
		logBrowserSignIn(fmt.Errorf("set the kept sign-in %s aside: %w", file, err))
		_ = os.Remove(file)
	}
}

// logBrowserSignIn logs a load or save of the kept sign-in that failed on
// zcp's stderr: the error names the file and carries agent-browser's own
// message, never a value.
func logBrowserSignIn(err error) {
	fmt.Fprintf(os.Stderr, "zerops_browser: kept sign-in: %v\n", err)
}

// What a call whose new browser could not restore all of the kept sign-in
// says (BrowserBatchResult.KeptSignIn).
const (
	browserSignInUnloadable = "The kept sign-in could not be restored: agent-browser could not load it, so it was set aside. " +
		"This call ran signed out; signing in again keeps the new sign-in."
	browserSignInNotHeld = "The kept sign-in's cookies were not restored in this call. " +
		"They are still kept for the next new browser; a sign-in made in this browser is not kept."
	browserSignInLostInTheCall = "Chrome restarted empty during this call, so the kept sign-in was not in it: " +
		"it is still kept for the next call, and a sign-in made in this call was not kept."
)

// browserSignInNote says what a restore could not bring back: the origins
// whose saved storage it dropped, those it did not restore in this call and
// still keeps, and the cookies when the browser is left without them; ""
// when everything was restored.
func browserSignInNote(dropped, missing []string, cookiesHeld bool) string {
	var parts []string
	if len(dropped) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was dropped: the site did not answer within %s while this call restored the kept sign-in. "+
			"Sign in there again if it needs it.", browserSites(dropped), browserSignInOriginTimeout))
	}
	if !cookiesHeld {
		parts = append(parts, browserSignInNotHeld)
	} else if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was not restored in this call, which had too little time left; it is still kept.", browserSites(missing)))
	}
	if len(parts) > 0 && cookiesHeld && len(missing) == 0 {
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

// saveBrowserSignIn merges the active tab's context — the call's own tab,
// the default context's — into the kept sign-in, when the browser holds the
// kept cookies (browserPresence; errBrowserSignInNotHeld otherwise) in the
// Chrome they were restored into. A browser whose base tab is another one
// was relaunched empty: nothing is saved (errBrowserRelaunched), and its
// presence then says it holds nothing, so no later call reuses it.
// agent-browser's state save writes a candidate in the folder's temporary
// folder (any file or symlink there is removed first); zcp drops its session
// storage, which a load never usefully restores and the way out does not
// clear, merges it with the kept file (mergeBrowserState) and renames the
// result over the kept file once it is a state agent-browser can load.
func saveBrowserSignIn(ctx context.Context, run browserStepExec, stateDir, presencePath string, now time.Time) error {
	presence, ok := readBrowserPresence(presencePath)
	if !ok || !presence.CookiesHeld {
		return errBrowserSignInNotHeld
	}
	candidate := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear the candidate %s: %w", candidate, err)
	}
	base, active, err := browserTabs(ctx, run)
	if err != nil {
		return err
	}
	if base != presence.BaseTarget {
		if err := writeBrowserPresence(presencePath, browserPresence{}); err != nil {
			logBrowserSignIn(err)
		}
		return errBrowserRelaunched
	}
	if err := walkBrowser(ctx, [][]string{{"state", "save", candidate}}, run); err != nil {
		return err
	}
	data, err := readBrowserFile(candidate)
	if err != nil {
		return fmt.Errorf("read the saved state %s: %w", candidate, err)
	}
	saved, err := withoutSessionStorage(data)
	if err != nil || !validBrowserState(saved) {
		_ = os.Remove(candidate)
		return fmt.Errorf("the saved state %s is not one agent-browser can load", candidate)
	}
	read := browserSaveRead(presence, active)
	old, _ := readBrowserFile(filepath.Join(stateDir, browserSignInFile))
	if err := writeBrowserFile(candidate, mergeBrowserState(old, saved, read, now)); err != nil {
		return fmt.Errorf("write the saved state %s: %w", candidate, err)
	}
	if err := os.Rename(candidate, filepath.Join(stateDir, browserSignInFile)); err != nil {
		return fmt.Errorf("keep the saved state %s: %w", candidate, err)
	}
	return nil
}

// browserSaveRead lists the origins a save read: those an open step ended on
// in this browser, and active, the page it ran on. An origin this browser did
// not get restored is left out: an empty read of it must not erase its kept
// storage, and a save that holds storage for it replaces its entry anyway.
func browserSaveRead(p browserPresence, active string) []string {
	var read []string
	for _, name := range append(slices.Clone(p.Opened), active) {
		if name != "" && !slices.Contains(p.Unrestored, name) {
			read = append(read, name)
		}
	}
	return read
}

// mergeBrowserState is the kept sign-in after a save. The saved cookies
// replace the kept ones: the browser holds every kept cookie, so its
// cookies are the whole truth. read lists the origins the save read — those
// an open step ended on in this browser and the page it ran on —, each used
// now: each is what the save holds for it, nothing when the save lacks it
// (agent-browser saves only origins with storage, so a clear sticks wherever
// the page went afterwards). An origin the save holds that it did not read
// through them keeps its last use. A kept origin the save did not read keeps
// its kept entry: agent-browser never reads an origin it only restored. The
// result is pruned (pruneBrowserOrigins). A kept file that is not a loadable
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

// withoutSessionStorage is a saved state with every origin's session
// storage dropped and every other field kept as it came.
func withoutSessionStorage(data []byte) ([]byte, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("saved state: %w", err)
	}
	if raw, ok := state["origins"]; ok {
		var origins []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &origins); err != nil {
			return nil, fmt.Errorf("saved state origins: %w", err)
		}
		for _, o := range origins {
			delete(o, "sessionStorage")
		}
		out, err := json.Marshal(origins)
		if err != nil {
			return nil, fmt.Errorf("saved state origins: %w", err)
		}
		state["origins"] = out
	}
	out, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("saved state: %w", err)
	}
	return out, nil
}

// validBrowserState reports whether data is a state agent-browser 0.39.1
// loads: its cookies and origins, each cookie with its name, value, domain
// and path, each origin with its origin and local storage, every field of
// the type agent-browser reads.
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
