package console

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
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
// /login, POST /login, GET /healthz, GET /static/*) runs through. An
// unauthenticated HTML request gets a 303 to /login; an unauthenticated
// /api/* request gets 401. A wrong bearer is delayed (no fast path — FM-50)
// before answering 401 either way.
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
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
		}
	}
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
// auth required (FM-50).
func (s *Server) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	failed := r.URL.Query().Get("error") == "1"
	renderLoginPage(w, failed)
}

// handleLoginPOST implements FM-50's login: constant-time compare against
// the console token; a wrong token is delayed (no global lockout — "a
// correct login right after 20 failures succeeds") before a 401; a correct
// token sets the session cookie and redirects to "/".
func (s *Server) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	submitted := r.PostFormValue("token")
	if !constantTimeEqual(submitted, s.cfg.Token) {
		s.sleep(s.failDelay())
		http.Redirect(w, r, loginPath+"?error=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, newSessionCookie(s.cfg.Token, s.now()))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
