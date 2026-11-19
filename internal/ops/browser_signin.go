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
// saved, and whether it keeps any local storage (only those are loaded).
type browserSavedOrigin struct {
	name    string
	raw     json.RawMessage
	storage bool
}

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
		}
		if json.Unmarshal(raw, &o) != nil {
			return nil, nil, false
		}
		origins = append(origins, browserSavedOrigin{name: o.Origin, raw: raw, storage: len(o.LocalStorage) > 0})
	}
	return state.Cookies, origins, true
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

// restoreBrowserSignIn loads the kept sign-in into a browser just started
// for the call, part by part, on its base tab; restart replaces that browser
// with a clean one. The cookies go first and alone: they open no site, so a
// load of them that fails or runs out marks a file agent-browser cannot
// load — it is set aside, never loaded again, and the call runs signed out
// in a clean browser. Then each origin's local storage alone, as listed (the
// last save lists the origins it read first), under its own bound: one whose
// load fails or runs out — a site that is slow, paused, looping or never
// answering — is dropped from the kept file and named, the browser restarted
// clean and the cookies loaded again before the next. The whole restore has
// browserSignInRestoreTimeout and at most a third of what the call has left:
// an origin it does not reach, or one cut by that budget rather than by its
// own bound, is neither loaded nor dropped and stays kept for the next new
// browser. The base tab goes back to about:blank after the last origin, so
// no restored page keeps running. note is what the call's result says, ""
// when everything was restored; err is a restart that failed.
func restoreBrowserSignIn(ctx context.Context, run browserStepExec, stateDir string, restart func(context.Context) error) (note string, err error) {
	file := filepath.Join(stateDir, browserSignInFile)
	data, err := readBrowserFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		logBrowserSignIn(fmt.Errorf("read the kept sign-in: %w", err))
		setBrowserSignInAside(stateDir)
		return browserSignInUnloadable, nil
	}
	cookies, origins, ok := parseBrowserState(data)
	if !ok {
		logBrowserSignIn(fmt.Errorf("the kept sign-in %s is not a state agent-browser can load", file))
		setBrowserSignInAside(stateDir)
		return browserSignInUnloadable, nil
	}
	budget := browserSignInRestoreTimeout
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)/3)
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	loadCookies := func(parent context.Context) error {
		cctx, cancel := context.WithTimeout(parent, browserSignInCookiesTimeout)
		defer cancel()
		return loadBrowserSignInPart(cctx, run, stateDir, encodeBrowserState(cookies, nil))
	}
	if err := loadCookies(rctx); err != nil {
		logBrowserSignIn(fmt.Errorf("load the cookies of the kept sign-in %s: %w", file, err))
		note := browserSignInNoTime
		if rctx.Err() == nil {
			setBrowserSignInAside(stateDir)
			note = browserSignInUnloadable
		}
		return note, restart(ctx)
	}
	var dropped, unreached []string
	kept := origins[:0:0]
	onSite := false
	for at, o := range origins {
		if !o.storage {
			kept = append(kept, o)
			continue
		}
		if deadline, _ := rctx.Deadline(); time.Until(deadline) < browserSignInOriginLeast || rctx.Err() != nil {
			for _, rest := range origins[at:] {
				if rest.storage {
					unreached = append(unreached, rest.name)
				}
			}
			kept = append(kept, origins[at:]...)
			break
		}
		octx, cancel := context.WithTimeout(rctx, browserSignInOriginTimeout)
		err := loadBrowserSignInPart(octx, run, stateDir, encodeBrowserState(nil, []browserSavedOrigin{o}))
		cancel()
		if err == nil {
			onSite = true
			kept = append(kept, o)
			continue
		}
		logBrowserSignIn(fmt.Errorf("load the local storage of %s from the kept sign-in %s: %w", o.name, file, err))
		// Its own bound ran out, or agent-browser answered a failure: the
		// site is at fault. The restore's budget running out is not.
		broken := rctx.Err() == nil
		if broken {
			dropped = append(dropped, o.name)
		} else {
			kept = append(kept, o)
		}
		// The browser is still busy with the site; a clean one goes on.
		if err := restart(ctx); err != nil {
			return "", err
		}
		onSite = false
		// The cookies again, from the call's own time: they loaded once and
		// open no site.
		if err := loadCookies(ctx); err != nil {
			logBrowserSignIn(fmt.Errorf("load the cookies of the kept sign-in %s again: %w", file, err))
			if err := restart(ctx); err != nil {
				return "", err
			}
			if len(dropped) > 0 {
				keepBrowserSignInWithout(stateDir, cookies, append(kept, origins[at+1:]...))
			}
			return strings.TrimSpace(browserSignInDroppedNote(dropped, nil) + " " + browserSignInNoTime), nil
		}
		if !broken {
			kept = append(kept, origins[at+1:]...)
			for _, rest := range origins[at:] {
				if rest.storage {
					unreached = append(unreached, rest.name)
				}
			}
			break
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
			_ = loadCookies(ctx)
		}
	}
	if len(dropped) > 0 {
		keepBrowserSignInWithout(stateDir, cookies, kept)
	}
	return browserSignInDroppedNote(dropped, unreached), nil
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
// origins: the ones a restore dropped are gone.
func keepBrowserSignInWithout(stateDir string, cookies json.RawMessage, origins []browserSavedOrigin) {
	tmp := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := writeBrowserFile(tmp, encodeBrowserState(cookies, origins)); err != nil {
		logBrowserSignIn(fmt.Errorf("drop origins from the kept sign-in: %w", err))
		return
	}
	if err := os.Rename(tmp, filepath.Join(stateDir, browserSignInFile)); err != nil {
		logBrowserSignIn(fmt.Errorf("drop origins from the kept sign-in: %w", err))
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
	browserSignInNoTime = "The kept sign-in was not restored: this call had too little time left. It is still kept for the next new browser."
)

// browserSignInDroppedNote names the origins whose saved local storage a
// restore dropped, and those it had no time left for, "" when none.
func browserSignInDroppedNote(dropped, unreached []string) string {
	var parts []string
	if len(dropped) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was dropped: the site did not answer within %s while this call restored the kept sign-in. "+
			"Sign in there again if it needs it; the rest of the kept sign-in was restored.", browserSites(dropped), browserSignInOriginTimeout))
	}
	if len(unreached) > 0 {
		parts = append(parts, fmt.Sprintf("The saved local storage of %s was not restored in this call, which had too little time left; it is still kept.", browserSites(unreached)))
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

// saveBrowserSignIn merges the active tab's context — the call's own tab,
// the default context's — into the kept sign-in. agent-browser's state save
// writes a candidate in the folder's temporary folder (any file or symlink
// there is removed first); zcp drops its session storage, which a load never
// usefully restores and the way out does not clear, merges it with the kept
// file (mergeBrowserState) and renames the result over the kept file once it
// is a state agent-browser can load.
func saveBrowserSignIn(ctx context.Context, run browserStepExec, stateDir string) error {
	candidate := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear the candidate %s: %w", candidate, err)
	}
	active, err := browserActiveOrigin(ctx, run)
	if err != nil {
		return err
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
	old, _ := readBrowserFile(filepath.Join(stateDir, browserSignInFile))
	if err := writeBrowserFile(candidate, mergeBrowserState(old, saved, active)); err != nil {
		return fmt.Errorf("write the saved state %s: %w", candidate, err)
	}
	if err := os.Rename(candidate, filepath.Join(stateDir, browserSignInFile)); err != nil {
		return fmt.Errorf("keep the saved state %s: %w", candidate, err)
	}
	return nil
}

// mergeBrowserState is the kept sign-in after a save: the saved cookies,
// which are the whole truth (every kept cookie was restored), and the saved
// origins first, then each kept origin the save holds nothing for. agent-
// browser saves only origins with storage, and only those its pages opened
// in this browser, so a kept origin the save lacks is one this browser did
// not read — it keeps its saved storage — unless it is read, the origin of
// the page the save ran on: its storage is empty, after a storage local clear
// for one, and stays so. A kept file that is not a loadable state adds
// nothing.
func mergeBrowserState(old, saved []byte, read string) []byte {
	cookies, origins, ok := parseBrowserState(saved)
	if !ok {
		return saved
	}
	_, keptOrigins, ok := parseBrowserState(old)
	if !ok {
		return saved
	}
	have := map[string]bool{read: true}
	for _, o := range origins {
		have[o.name] = true
	}
	for _, o := range keptOrigins {
		if !have[o.name] {
			origins = append(origins, o)
			have[o.name] = true
		}
	}
	return encodeBrowserState(cookies, origins)
}

// browserActiveOrigin is the origin of the active tab, the page a save reads
// directly, as the page states it ("" for about:blank and the like).
func browserActiveOrigin(ctx context.Context, run browserStepExec) (string, error) {
	out, _, err := run(ctx, []string{"tab", "list"})
	if err != nil {
		return "", fmt.Errorf("tab list: %w", err)
	}
	var answer struct {
		Data struct {
			Tabs []struct {
				Active bool   `json:"active"`
				URL    string `json:"url"`
			} `json:"tabs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &answer); err != nil {
		return "", fmt.Errorf("tab list: %w", err)
	}
	for _, tab := range answer.Data.Tabs {
		if !tab.Active {
			continue
		}
		if u, err := url.Parse(tab.URL); err == nil && u.Host != "" && (u.Scheme == httpScheme || u.Scheme == httpsScheme) {
			return u.Scheme + "://" + u.Host, nil
		}
		return "", nil
	}
	return "", nil
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
