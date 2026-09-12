package console

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// cookieName and sessionTTL implement FM-50: farm_session, 30 days.
// loginPath is the open GET/POST route every unauthenticated HTML request
// redirects to.
const (
	cookieName = "farm_session"
	sessionTTL = 30 * 24 * time.Hour
	loginPath  = "/login"
)

// defaultLoginFailDelay is the production floor on a failed login/bearer
// attempt (FM-50: "no sooner than 1 s"). Config.LoginFailDelay overrides it
// (tests use a small value so the delay mechanism is exercised without a
// real sleep).
const defaultLoginFailDelay = time.Second

// authResult is how one request's credentials resolved.
type authResult int

const (
	authNone      authResult = iota // no credential presented, or a stale cookie
	authOK                          // a valid bearer or a valid, unexpired cookie
	authBadBearer                   // an Authorization: Bearer header present but wrong
)

// constantTimeEqual compares a and b without leaking their length through
// timing (FM-50: "both compared in constant time") — a and b are first
// hashed to a fixed 32-byte digest, since subtle.ConstantTimeCompare itself
// short-circuits on differing input lengths.
func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// sessionCookieValue builds "<expiry-unix>.<hex HMAC-SHA256(token, expiry)>"
// (FM-50). Keying the MAC on the current console token is what makes a
// token rotation invalidate every existing cookie: a recomputation under the
// new token never matches a signature made under the old one.
func sessionCookieValue(token string, expiry time.Time) string {
	exp := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(exp))
	return exp + "." + hex.EncodeToString(mac.Sum(nil))
}

// verifySessionCookie reports whether value is a well-formed, correctly
// signed, unexpired session cookie for token as of now.
func verifySessionCookie(token, value string, now time.Time) bool {
	expPart, _, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	expUnix, err := strconv.ParseInt(expPart, 10, 64)
	if err != nil {
		return false
	}
	expiry := time.Unix(expUnix, 0)
	if !constantTimeEqual(value, sessionCookieValue(token, expiry)) {
		return false
	}
	return now.Before(expiry)
}

// newSessionCookie builds the Set-Cookie header value for a fresh login
// (FM-50: HttpOnly, Secure, SameSite=Strict, Path=/, 30 days).
func newSessionCookie(token string, now time.Time) *http.Cookie {
	expiry := now.Add(sessionTTL)
	return &http.Cookie{
		Name:     cookieName,
		Value:    sessionCookieValue(token, expiry),
		Path:     "/",
		Expires:  expiry,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header, or "" when the header is absent or not Bearer-scheme.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

// authenticate resolves one request's credentials against the console
// token, in FM-50's order: a bearer header (if present) is authoritative —
// right or wrong, it decides the request; only absent a bearer header is
// the session cookie consulted (a present-but-stale cookie reads as
// authNone, not authBadLogin/authBadBearer — no delay applies to it).
func (s *Server) authenticate(r *http.Request) authResult {
	if tok, present := bearerToken(r); present {
		if constantTimeEqual(tok, s.cfg.Token) {
			return authOK
		}
		return authBadBearer
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return authNone
	}
	if verifySessionCookie(s.cfg.Token, c.Value, s.now()) {
		return authOK
	}
	return authNone
}

// requireAuth is the middleware every route but the open ones (FM-50: GET
// /login, POST /login, GET /healthz, GET /static/app.css) runs through. An
// unauthenticated HTML request gets a 303 to /login?next=<its path and
// query> (§8.3 FM-51: "an unauthenticated HTML request is sent to
// /login?next=<its path>"); an unauthenticated /api/* request gets 401. A
// wrong bearer is delayed (no fast path — FM-50) before answering 401
// either way.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch s.authenticate(r) {
		case authOK:
			next(w, r)
		case authBadBearer:
			s.sleep(s.failDelay())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case authNone:
			if isAPIPath(r.URL.Path) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(actionNextPath(r)), http.StatusSeeOther)
		}
	}
}

// actionStateChangingSuffix is every POST action route's own suffix,
// appended to an otherwise GET-able page path (§8.3 FM-51's action
// routes: POST /r/<id>/observe, POST /b/<batch>/observe) — stripping it
// recovers the GET page underneath.
const actionStateChangingSuffix = "/observe"

// actionNextPath builds requireAuth's own ?next= value (item 7, FIX3): a
// GET request's own path and query replay safely as-is; a POST has no GET
// twin at its own exact path (the post-login 303 becomes a browser GET,
// and a GET to a POST-only action route is a 404 — the live bug an
// expired cookie turns a Re-assess click into) — an action route's own
// path names the page underneath instead (stripping "/observe"), and any
// other POST (only /logout) has no page underneath at all, so next is "/".
func actionNextPath(r *http.Request) string {
	if r.Method != http.MethodPost {
		return r.URL.RequestURI()
	}
	if page, ok := strings.CutSuffix(r.URL.Path, actionStateChangingSuffix); ok {
		return page
	}
	return "/"
}

// validNextPath reports whether next is safe to redirect to after login
// (§8.3 FM-51): it must start with "/" and not "//" — "//" is browser-
// parsed as a protocol-relative URL to another host, and anything not
// starting with "/" (an absolute URL like "https://x", a bare relative
// path like "relative") could send the browser off the console entirely.
func validNextPath(next string) bool {
	return strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//")
}

// safeNext returns next when validNextPath allows it, else "/" (§8.3
// FM-51: "else /").
func safeNext(next string) string {
	if validNextPath(next) {
		return next
	}
	return "/"
}

// handleLogout implements POST /logout (§8.3 FM-51): clears the session
// cookie and sends the browser back to /login. Like every other state-
// changing route, a cookie-authenticated request must carry a matching
// Origin (§8.2 FM-50) — a bearer-authenticated one needs none.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.checkActionOrigin(w, r) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
	s.respondAction(w, r, loginPath)
}

func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

func (s *Server) failDelay() time.Duration {
	if s.cfg.LoginFailDelay > 0 {
		return s.cfg.LoginFailDelay
	}
	return defaultLoginFailDelay
}

func (s *Server) sleep(d time.Duration) {
	if s.cfg.Sleep != nil {
		s.cfg.Sleep(d)
		return
	}
	time.Sleep(d)
}

// handleLoginGET serves the static login page (assets/login.html) — no
// auth required (FM-50). ?next=<path> (set by requireAuth's redirect, or a
// bookmarked deep link) is sanitized and carried through as the form's
// hidden field (§8.3 FM-51).
func (s *Server) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	failed := r.URL.Query().Get("error") == "1"
	renderLoginPage(w, failed, safeNext(r.URL.Query().Get("next")))
}

// handleLoginPOST implements FM-50's login: constant-time compare against
// the console token; a wrong token is delayed (no global lockout — "a
// correct login right after 20 failures succeeds") before a redirect back
// to the login page (carrying next along, so a failed attempt does not
// lose the original deep link); a correct token sets the session cookie
// and redirects to next when it is a safe path (§8.3 FM-51), else "/".
func (s *Server) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	submitted := r.PostFormValue("token")
	next := safeNext(r.PostFormValue("next"))
	if !constantTimeEqual(submitted, s.cfg.Token) {
		s.sleep(s.failDelay())
		http.Redirect(w, r, loginPath+"?error=1&next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}
	http.SetCookie(w, newSessionCookie(s.cfg.Token, s.now()))
	http.Redirect(w, r, next, http.StatusSeeOther)
}
