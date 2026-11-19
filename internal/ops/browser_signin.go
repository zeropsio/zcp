package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The kept sign-in (docs/spec-mate.md §2.6c): the cookies and local storage
// of the warm browser's default context, kept in one file zcp owns so a
// replaced browser starts signed in. zcp loads it into a browser started for
// a tab or new-browser call and saves it at the end of every call that hands
// the browser back warm, with agent-browser's own state load and state save;
// the browser's start restores nothing, so no saved site can hold it up.
const (
	browserSignInFile = "zcp-default.json"
	// browserSignInAside is the kept file a load could not restore, set
	// aside for a look and never loaded again; one copy at most.
	browserSignInAside = browserSignInFile + ".aside"
	// browserStateTmp holds a save's candidate until zcp renames it over
	// the kept file.
	browserStateTmp = ".tmp"
	// browserSignInMaxAge: a starting browser removes a kept or set-aside
	// file no save touched for a week.
	browserSignInMaxAge = 7 * 24 * time.Hour
	// browserSignInLoadTimeout bounds the load and the base tab's way back
	// to about:blank, out of the call's own budget: a healthy load takes
	// about half a second per saved origin with local storage, and a saved
	// site that is slow, paused, looping or never answering must not spend
	// the call (zerops_verify's render check has 30 s in all).
	browserSignInLoadTimeout = 10 * time.Second
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

// restoreBrowserSignIn loads the kept sign-in into a browser just started
// for the call, on its base tab, and sends the base tab back to about:blank
// so no restored page keeps running. Both share browserSignInLoadTimeout. A
// load that fails or does not finish sets the kept file aside — never loaded
// again; the next clean save writes a new one — and asks for a clean browser
// (restart): one that did not finish is still busy with a saved site, and
// one that failed may hold the cookies of a half load, which the next save
// would keep. note is what the call's result then says, naming the saved
// sites the load opens when it did not finish. A call whose own budget ran
// out sets nothing aside: the call fails anyway.
func restoreBrowserSignIn(ctx context.Context, run browserStepExec, stateDir string) (note string, restart bool) {
	file := filepath.Join(stateDir, browserSignInFile)
	if _, err := os.Lstat(file); err != nil {
		return "", false
	}
	lctx, cancel := context.WithTimeout(ctx, browserSignInLoadTimeout)
	err := walkBrowser(lctx, [][]string{{"state", "load", file}, {browserCmdOpen, "about:blank"}}, run)
	finished := lctx.Err() == nil
	cancel()
	if err == nil {
		return "", false
	}
	logBrowserSignIn(fmt.Errorf("load the kept sign-in %s: %w", file, err))
	if ctx.Err() != nil {
		return "", false
	}
	origins := browserStateOrigins(file)
	if err := os.Rename(file, filepath.Join(stateDir, browserSignInAside)); err != nil {
		logBrowserSignIn(fmt.Errorf("set the kept sign-in %s aside: %w", file, err))
		_ = os.Remove(file)
	}
	return browserRestoreNote(origins, finished), true
}

// logBrowserSignIn logs a load or save of the kept sign-in that failed on
// zcp's stderr: the error names the file and carries agent-browser's own
// message, never a value.
func logBrowserSignIn(err error) {
	fmt.Fprintf(os.Stderr, "zerops_browser: kept sign-in: %v\n", err)
}

// browserRestoreNote says, in a few words and naming no value, that the
// kept sign-in could not be restored and the call ran signed out.
func browserRestoreNote(origins []string, finished bool) string {
	what := "agent-browser could not load it"
	if !finished {
		sites := "loading it"
		switch {
		case len(origins) == 1:
			sites = "opening its saved site " + origins[0]
		case len(origins) > 3:
			sites = fmt.Sprintf("opening its saved sites %s and %d more", strings.Join(origins[:3], ", "), len(origins)-3)
		case len(origins) > 1:
			sites = "opening its saved sites " + strings.Join(origins, ", ")
		}
		what = fmt.Sprintf("%s did not finish within %s", sites, browserSignInLoadTimeout)
	}
	return "The kept sign-in could not be restored: " + what + ". This call ran signed out; signing in again keeps the new sign-in."
}

// browserStateOrigins lists the origins a load of file opens: those it keeps
// local storage for. A file it cannot read lists none.
func browserStateOrigins(file string) []string {
	data, err := readBrowserFile(file)
	if err != nil {
		return nil
	}
	var state struct {
		Origins []struct {
			Origin       string            `json:"origin"`
			LocalStorage []json.RawMessage `json:"localStorage"`
		} `json:"origins"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	var origins []string
	for _, o := range state.Origins {
		if len(o.LocalStorage) > 0 && o.Origin != "" {
			origins = append(origins, o.Origin)
		}
	}
	return origins
}

// saveBrowserSignIn saves the active tab's context — the call's own tab, the
// default context's — as the kept sign-in. agent-browser's state save writes
// a candidate in the folder's temporary folder (any file or symlink there is
// removed first); zcp drops its session storage, which a load never usefully
// restores and the way out does not clear, and renames it over the kept file
// once it is a state agent-browser can load.
func saveBrowserSignIn(ctx context.Context, run browserStepExec, stateDir string) error {
	candidate := filepath.Join(stateDir, browserStateTmp, browserSignInFile)
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear the candidate %s: %w", candidate, err)
	}
	if err := walkBrowser(ctx, [][]string{{"state", "save", candidate}}, run); err != nil {
		return err
	}
	data, err := readBrowserFile(candidate)
	if err != nil {
		return fmt.Errorf("read the saved state %s: %w", candidate, err)
	}
	kept, err := withoutSessionStorage(data)
	if err != nil || !validBrowserState(kept) {
		_ = os.Remove(candidate)
		return fmt.Errorf("the saved state %s is not one agent-browser can load", candidate)
	}
	if err := writeBrowserFile(candidate, kept); err != nil {
		return fmt.Errorf("write the saved state %s: %w", candidate, err)
	}
	if err := os.Rename(candidate, filepath.Join(stateDir, browserSignInFile)); err != nil {
		return fmt.Errorf("keep the saved state %s: %w", candidate, err)
	}
	return nil
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
