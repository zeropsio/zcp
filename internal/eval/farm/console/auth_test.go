package console

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testToken = "farm-console-token-0123456789abcdef"

func TestConsole_LoginUI_PreservesSafeNextAndAuth(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/login?next=%2Ffindings", nil))
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `/static/vendor/tabler-1.5.1.min.css`) ||
		!strings.Contains(body, `<label class="form-label" for="token">`) ||
		!strings.Contains(body, `id="token"`) || !strings.Contains(body, `name="next" value="/findings"`) {
		t.Fatalf("login UI: status=%d body=%s", rr.Code, body)
	}

	const rejectedToken = "wrong-token-must-never-be-echoed"
	wantNext := "/findings?since=30d"
	form := url.Values{"token": {rejectedToken}, "next": {wantNext}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	wantRejectedLocation := "/login?error=1&next=" + url.QueryEscape(wantNext)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != wantRejectedLocation || len(rr.Result().Cookies()) != 0 {
		t.Fatalf("rejected login lost safe next or authenticated: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, wantRejectedLocation, nil))
	body = rr.Body.String()
	for _, want := range []string{
		`name="next" value="/findings?since=30d"`,
		`aria-invalid="true"`,
		`aria-describedby="token-error"`,
		`id="token-error"`,
		`role="alert"`,
		"Wrong token.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rejected login UI missing associated state %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, rejectedToken) {
		t.Errorf("rejected login echoed submitted token:\n%s", body)
	}

	form = url.Values{"token": {testToken}, "next": {wantNext}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != wantNext || len(rr.Result().Cookies()) != 1 {
		t.Fatalf("successful login did not return to safe next: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}

	form = url.Values{"token": {testToken}, "next": {"https://evil.example"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("unsafe next redirect: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}
}

// TestConsole_NoAuth_APIIs401AndHTMLRedirectsToLogin pins FM-50: "A request
// without auth gets a 303 to /login (HTML routes) or 401 (/api/*)."
// Independent oracle: the exact statuses are the spec's own words.
func TestConsole_NoAuth_APIIs401AndHTMLRedirectsToLogin(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/runs.md", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/runs.md unauthenticated: got %d, want 401", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("GET / unauthenticated: got %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login?next=%2F" {
		t.Errorf("GET / unauthenticated Location: got %q, want /login?next=%%2F", loc)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/missing-page?from=test", nil))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?next=%2Fmissing-page%3Ffrom%3Dtest" {
		t.Errorf("unknown HTML route bypassed auth: status=%d location=%q", rr.Code, rr.Header().Get("Location"))
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/not-a-route", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unknown API route bypassed auth: got %d, want 401", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/missing-page", nil)
	req.Header.Set("Authorization", "Bearer definitely-wrong")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unknown route with wrong bearer bypassed auth: got %d, want 401", rr.Code)
	}
}

// TestConsole_UnauthenticatedActionNextStripsStateChangingSuffix pins item
// 7 (FIX3): an unauthenticated (e.g. expired-cookie) POST to an action
// route (…/observe) must not send the post-login browser back to that
// same POST-only path via a GET — the 303 after POST /login turns into a
// GET, and a GET to an action route is a 404, not the run/batch page the
// user meant to act on. next names the page underneath the action instead.
func TestConsole_UnauthenticatedActionNextStripsStateChangingSuffix(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	cases := []struct {
		path, wantNext string
	}{
		{"/r/gate1-a/observe", "/r/gate1-a"},
		{"/b/gate11/observe", "/b/gate11"},
		{"/logout", "/"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, c.path, nil))
			if rr.Code != http.StatusSeeOther {
				t.Fatalf("POST %s unauthenticated: got %d, want 303", c.path, rr.Code)
			}
			want := "/login?next=" + url.QueryEscape(c.wantNext)
			if loc := rr.Header().Get("Location"); loc != want {
				t.Errorf("POST %s unauthenticated Location: got %q, want %q", c.path, loc, want)
			}
		})
	}
}

// TestConsole_OpenRoutesNeedNoAuth pins FM-50's open-route list: GET
// /login, POST /login, GET /healthz, GET /static/app.css.
func TestConsole_OpenRoutesNeedNoAuth(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("GET /login: got %d, want 200", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("GET /healthz: got %d, want 200", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("GET /static/app.css: got %d, want 200", rr.Code)
	}

	// A POST /login with a wrong token is still an open route (it must not
	// require prior auth to reach the handler at all) — it redirects back
	// to /login rather than 401/303-to-login-loop.
	rr = httptest.NewRecorder()
	form := url.Values{"token": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("POST /login (open route, wrong token): got %d, want 303", rr.Code)
	}
}

// TestConsole_BearerTokenAuthorizes pins FM-50's bearer path for both an
// API route and an HTML route.
func TestConsole_BearerTokenAuthorizes(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/runs.md", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("GET /api/runs.md with bearer: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("GET / with bearer: got %d, want 200", rr.Code)
	}
}

// TestConsole_LoginSetsSecureStrictHttpOnlyCookie pins FM-50's exact cookie
// attributes. Independent oracle: literal attribute values quoted straight
// from the spec, not derived from auth.go's own constants.
func TestConsole_LoginSetsSecureStrictHttpOnlyCookie(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	form := url.Values{"token": {testToken}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("POST /login correct token: got %d, want 303, body=%s", rr.Code, rr.Body.String())
	}
	resp := rr.Result()
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no farm_session cookie set")
	}
	if !cookie.HttpOnly {
		t.Error("cookie not HttpOnly")
	}
	if !cookie.Secure {
		t.Error("cookie not Secure")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", cookie.Path)
	}
	wantMaxAge := int((30 * 24 * time.Hour).Seconds())
	if cookie.MaxAge != wantMaxAge {
		t.Errorf("cookie MaxAge = %d, want %d (30 days)", cookie.MaxAge, wantMaxAge)
	}
}

// TestConsole_WrongTokenLoginRejectedAndDelayed pins FM-50: every failed
// login is delayed (no sooner than the configured floor — 1s in
// production), and there is no global lockout: "a correct login right
// after 20 failures succeeds."
func TestConsole_WrongTokenLoginRejectedAndDelayed(t *testing.T) {
	srv, _, sleeps := testServer(t)
	h := srv.Handler()

	for i := range 20 {
		rr := httptest.NewRecorder()
		form := url.Values{"token": {"wrong-token"}}
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("failure %d: got %d, want 303", i, rr.Code)
		}
		if len(rr.Result().Cookies()) != 0 {
			t.Fatalf("failure %d: a cookie was set on a wrong-token login", i)
		}
	}
	if len(*sleeps) != 20 {
		t.Fatalf("got %d delayed failures, want 20 (no lockout short-circuit)", len(*sleeps))
	}
	for i, d := range *sleeps {
		if d < time.Millisecond {
			t.Errorf("failure %d: delay %v below the configured floor", i, d)
		}
	}

	// The 21st attempt, with the correct token, still succeeds.
	rr := httptest.NewRecorder()
	form := url.Values{"token": {testToken}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("correct login after 20 failures: got %d Location=%q, want 303 to /", rr.Code, rr.Header().Get("Location"))
	}
	if len(rr.Result().Cookies()) == 0 {
		t.Error("correct login after 20 failures set no cookie")
	}
}

// TestConsole_BearerMismatchDelayedNoFastPath pins FM-50's "both compared
// in constant time" for the bearer header too: a wrong bearer of any
// length still incurs the delay — no short-circuit for an
// obviously-wrong-length header.
func TestConsole_BearerMismatchDelayedNoFastPath(t *testing.T) {
	srv, _, sleeps := testServer(t)
	h := srv.Handler()

	wrongTokens := []string{"", "x", "way-too-short", strings.Repeat("z", 500)}
	for _, tok := range wrongTokens {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/runs.md", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("bearer %q: got %d, want 401", tok, rr.Code)
		}
	}
	if len(*sleeps) != len(wrongTokens) {
		t.Fatalf("got %d delayed bearer failures, want %d (one per attempt, regardless of length)", len(*sleeps), len(wrongTokens))
	}
}

// TestConsole_TokenRotationInvalidatesCookie pins FM-50: "rotating the
// token ends every session" — the cookie's HMAC is keyed on the console
// token, so a session made under the old token fails verification under
// the new one.
func TestConsole_TokenRotationInvalidatesCookie(t *testing.T) {
	store := newFakeStore()
	now := fixedNow(t)

	oldSrv := NewServer(Config{Store: store, Token: "old-token-0123456789abcdef", Now: now})
	rr := httptest.NewRecorder()
	form := url.Values{"token": {"old-token-0123456789abcdef"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oldSrv.Handler().ServeHTTP(rr, req)
	var cookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie from the old-token login")
	}

	newSrv := NewServer(Config{Store: store, Token: "new-token-fedcba9876543210", Now: now})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	newSrv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?next=%2F" {
		t.Errorf("stale cookie after rotation: got %d Location=%q, want 303 to /login?next=%%2F", rr.Code, rr.Header().Get("Location"))
	}
}

// TestConsole_ExpiredCookieRejected pins FM-50's 30-day cookie expiry.
func TestConsole_ExpiredCookieRejected(t *testing.T) {
	now := fixedNow(t)
	srv := NewServer(Config{Store: newFakeStore(), Token: testToken, Now: now})

	expired := sessionCookieValue(testToken, now().Add(-time.Minute))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: expired})
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?next=%2F" {
		t.Errorf("expired cookie: got %d Location=%q, want 303 to /login?next=%%2F", rr.Code, rr.Header().Get("Location"))
	}
}

// TestConsole_LoginNextValidation pins §8.3 FM-51's /login?next= rule: a
// correct login goes to next only when it starts with "/" and not "//",
// else "/". Independent oracle: the spec's own four example shapes —
// "//evil" (protocol-relative, rejected), "https://x" (absolute, rejected),
// "relative" (no leading slash, rejected), "/r/x#s1" (a real deep link,
// accepted verbatim including its fragment).
func TestConsole_LoginNextValidation(t *testing.T) {
	cases := []struct {
		next string
		want string
	}{
		{"//evil", "/"},
		{"https://x", "/"},
		{"relative", "/"},
		{"/r/x#s1", "/r/x#s1"},
		{"", "/"},
		{"/b/pb1", "/b/pb1"},
	}
	for _, c := range cases {
		t.Run(c.next, func(t *testing.T) {
			srv, _, _ := testServer(t)
			h := srv.Handler()

			rr := httptest.NewRecorder()
			form := url.Values{"token": {testToken}, "next": {c.next}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusSeeOther {
				t.Fatalf("POST /login next=%q: got %d, want 303", c.next, rr.Code)
			}
			if loc := rr.Header().Get("Location"); loc != c.want {
				t.Errorf("POST /login next=%q: Location = %q, want %q", c.next, loc, c.want)
			}
		})
	}
}

// TestConsole_LoginGETCarriesNextIntoHiddenField pins that the login page
// itself (GET /login?next=<path>) carries a validated next through as a
// hidden field, so the value survives the round trip through the form.
func TestConsole_LoginGETCarriesNextIntoHiddenField(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/login?next=%2Fr%2Fx", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /login?next=/r/x: got %d, want 200", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `name="next" value="/r/x"`) {
		t.Errorf("login page missing the hidden next field:\n%s", body)
	}

	// An unsafe next is sanitized to "/" before it ever reaches the page.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/login?next=https://evil", nil))
	if body := rr.Body.String(); !strings.Contains(body, `name="next" value="/"`) {
		t.Errorf("login page did not sanitize an unsafe next:\n%s", body)
	}
}

// TestConsole_LogoutClearsSessionCookie pins §8.3 FM-51's POST /logout: it
// clears farm_session and sends the browser to /login.
func TestConsole_LogoutClearsSessionCookie(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionCookieValue(testToken, fixedNow(t)().Add(time.Hour))})
	req.Header.Set("Origin", "http://example.com")
	req.Host = "example.com"
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Fatalf("POST /logout: got %d Location=%q, want 303 to /login", rr.Code, rr.Header().Get("Location"))
	}
	var cleared *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == cookieName {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatal("POST /logout set no farm_session cookie")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("logout cookie MaxAge = %d, want negative (cleared)", cleared.MaxAge)
	}

	// The cleared cookie no longer authenticates a later request.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: cookieName, Value: cleared.Value})
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Errorf("GET / with the cleared cookie: got %d, want 303 to login", rr2.Code)
	}
}

// TestConsole_LogoutRequiresOriginForCookieAuth pins §8.2 FM-50: logout is
// a state-changing POST, so a cookie-authenticated request without a
// matching Origin is refused (403) — the same rule actions.go's
// checkActionOrigin already enforces for /observe.
func TestConsole_LogoutRequiresOriginForCookieAuth(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: sessionCookieValue(testToken, fixedNow(t)().Add(time.Hour))})
	// No Origin header at all.
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /logout without Origin: got %d, want 403", rr.Code)
	}
}

// TestConsole_SecurityHeadersOnEveryResponse pins FM-50's exact header set
// on every response, including a 401 and a 404. Independent oracle: the
// header values are copied verbatim from the spec text.
func TestConsole_SecurityHeadersOnEveryResponse(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	routes := []struct {
		name   string
		method string
		path   string
		bearer string
	}{
		{"open login page", http.MethodGet, "/login", ""},
		{"unauthenticated api (401)", http.MethodGet, "/api/runs.md", ""},
		{"unknown route auth redirect", http.MethodGet, "/nope", ""},
		{"authenticated root", http.MethodGet, "/", testToken},
	}
	for _, rt := range routes {
		t.Run(rt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(rt.method, rt.path, nil)
			if rt.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+rt.bearer)
			}
			h.ServeHTTP(rr, req)

			want := map[string]string{
				"Content-Security-Policy": "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'",
				"X-Content-Type-Options":  "nosniff",
				"Referrer-Policy":         "same-origin",
				"Cache-Control":           "no-store",
			}
			for k, v := range want {
				if got := rr.Header().Get(k); got != v {
					t.Errorf("%s: header %s = %q, want %q", rt.name, k, got, v)
				}
			}
		})
	}
}
