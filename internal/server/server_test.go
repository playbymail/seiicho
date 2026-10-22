// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/server"
	"github.com/playbymail/seiicho/internal/store"
)

const publicHost = "htmx-app.localhost:8443"

var now = time.Unix(1_800_000_000, 0)

func newServer(t *testing.T, env string) (http.Handler, *store.Store) {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	s, err := store.OpenPrivateMemory(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := server.New(server.Config{
		Env:     env,
		Store:   s,
		Version: "0.0.0-test",
		Now:     func() time.Time { return now },
	})
	return h, s
}

func addAccount(t *testing.T, s *store.Store, email, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(t.Context(), email, hash, false, now); err != nil {
		t.Fatal(err)
	}
}

// do sends a request as a same-origin browser on the public origin would.
func do(h http.Handler, method, target string, form url.Values, cookies ...*http.Cookie) *http.Response {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "https://"+publicHost+target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Origin", "https://"+publicHost)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Result()
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sessionCookie(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-seiicho_session" {
			if !c.Secure || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
				t.Fatalf("session cookie is not secure: %+v", c)
			}
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestLandingShowsLoginForm(t *testing.T) {
	h, _ := newServer(t, "test")
	resp := do(h, http.MethodGet, "/", nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `action="/login"`) || strings.Contains(body, "Welcome") {
		t.Fatalf("landing page for anonymous user:\n%s", body)
	}
}

func TestLoginAndLogout(t *testing.T) {
	h, s := newServer(t, "test")
	addAccount(t, s, "player@example.com", "secret")

	resp := do(h, http.MethodPost, "/login", url.Values{"email": {"Player@Example.com"}, "password": {"secret"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("login = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := sessionCookie(t, resp)

	body := readBody(t, do(h, http.MethodGet, "/", nil, cookie))
	if !strings.Contains(body, "Welcome, player@example.com") || strings.Contains(body, `action="/login"`) {
		t.Fatalf("landing page for authenticated user:\n%s", body)
	}

	resp = do(h, http.MethodPost, "/logout", nil, cookie)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	body = readBody(t, do(h, http.MethodGet, "/", nil, cookie))
	if strings.Contains(body, "Welcome") {
		t.Fatal("session survived logout")
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	h, s := newServer(t, "test")
	addAccount(t, s, "player@example.com", "secret")
	for _, form := range []url.Values{
		{"email": {"player@example.com"}, "password": {"wrong"}},
		{"email": {"nobody@example.com"}, "password": {"secret"}},
		{"email": {"player@example.com"}},
	} {
		resp := do(h, http.MethodPost, "/login", form)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
			t.Errorf("login %v = %d with %d cookies", form, resp.StatusCode, len(resp.Cookies()))
		}
		if !strings.Contains(body, "Invalid email or password") {
			t.Errorf("login %v: no error message", form)
		}
	}
}

func TestLoginEscapesEmail(t *testing.T) {
	h, _ := newServer(t, "test")
	body := readBody(t, do(h, http.MethodPost, "/login", url.Values{"email": {`"><script>x</script>`}, "password": {"x"}}))
	if strings.Contains(body, "<script>") {
		t.Fatal("rendered email is not escaped")
	}
}

func TestInvalidSessionCookie(t *testing.T) {
	h, _ := newServer(t, "test")
	body := readBody(t, do(h, http.MethodGet, "/", nil, &http.Cookie{Name: "__Host-seiicho_session", Value: "forged"}))
	if strings.Contains(body, "Welcome") {
		t.Fatal("forged session accepted")
	}
}

func TestHealthz(t *testing.T) {
	h, _ := newServer(t, "test")
	resp := do(h, http.MethodGet, "/api/healthz", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("healthz = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var got struct{ Status, Version string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Version != "0.0.0-test" {
		t.Fatalf("healthz = %+v", got)
	}
}

func TestCrossOriginProtection(t *testing.T) {
	h, s := newServer(t, "test")
	addAccount(t, s, "player@example.com", "secret")
	form := url.Values{"email": {"player@example.com"}, "password": {"secret"}}

	for name, headers := range map[string]map[string]string{
		"cross-site fetch metadata": {"Sec-Fetch-Site": "cross-site"},
		"mismatched origin":         {"Origin": "https://evil.example"},
		"origin with wrong port":    {"Origin": "https://htmx-app.localhost:9443"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "https://"+publicHost+"/login", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for k, v := range headers {
				r.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status = %d, want 403 without a session", w.Code)
			}
		})
	}

	t.Run("origin fallback with public host and port", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "https://"+publicHost+"/login", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://"+publicHost)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303", w.Code)
		}
	})
}

func TestHelpersAreEnvironmentGated(t *testing.T) {
	for _, tc := range []struct {
		env       string
		available string
	}{
		{"development", "/__dev"},
		{"agent", "/__agent"},
		{"test", ""},
		{"production", ""},
	} {
		t.Run(tc.env, func(t *testing.T) {
			h, _ := newServer(t, tc.env)
			for _, prefix := range []string{"/__dev", "/__agent"} {
				resp := do(h, http.MethodPost, prefix+"/create-account/a@example.com", nil)
				want := http.StatusNotFound
				if prefix == tc.available {
					want = http.StatusCreated
				}
				if resp.StatusCode != want {
					t.Errorf("%s create-account = %d, want %d", prefix, resp.StatusCode, want)
				}
			}
		})
	}
}

func TestCreateAccountHelper(t *testing.T) {
	h, s := newServer(t, "agent")

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if resp := do(h, method, "/__agent/create-account/a@example.com", nil); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, resp.StatusCode)
		}
	}
	if _, err := s.AccountByEmail(t.Context(), "a@example.com"); err == nil {
		t.Fatal("safe method created an account")
	}

	for _, q := range []string{"?isAdmin=", "?isAdmin=TRUE", "?isAdmin=1", "?isAdmin=true&isAdmin=false"} {
		if resp := do(h, http.MethodPost, "/__agent/create-account/a@example.com"+q, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("isAdmin %q = %d, want 400", q, resp.StatusCode)
		}
	}
	if resp := do(h, http.MethodPost, "/__agent/create-account/not-an-email", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid email = %d, want 400", resp.StatusCode)
	}

	if resp := do(h, http.MethodPost, "/__agent/create-account/"+url.PathEscape("a@example.com")+"?isAdmin=true", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	a, err := s.AccountByEmail(t.Context(), "a@example.com")
	if err != nil || !a.IsAdmin {
		t.Fatalf("account = %+v, %v", a, err)
	}
	if auth.CheckPassword(a.PasswordHash, "") {
		t.Fatal("account has an empty password")
	}

	if resp := do(h, http.MethodPost, "/__agent/create-account/a@example.com?isAdmin=false", nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", resp.StatusCode)
	}
	b, err := s.AccountByEmail(t.Context(), "a@example.com")
	if err != nil || !b.IsAdmin || string(b.PasswordHash) != string(a.PasswordHash) {
		t.Fatalf("duplicate create modified the account")
	}

	if resp := do(h, http.MethodPost, "/__agent/create-account/b@example.com", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	if b, _ := s.AccountByEmail(t.Context(), "b@example.com"); b.IsAdmin {
		t.Fatal("isAdmin did not default to false")
	}
}

func TestLogMeInHelper(t *testing.T) {
	h, s := newServer(t, "development")
	addAccount(t, s, "a@example.com", "secret")

	if resp := do(h, http.MethodGet, "/__dev/log-me-in/a@example.com", nil); resp.StatusCode != http.StatusMethodNotAllowed || len(resp.Cookies()) != 0 {
		t.Fatalf("GET = %d, want 405 without a session", resp.StatusCode)
	}
	if resp := do(h, http.MethodPost, "/__dev/log-me-in/nobody@example.com", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing account = %d, want 404", resp.StatusCode)
	}
	if _, err := s.AccountByEmail(t.Context(), "nobody@example.com"); err == nil {
		t.Fatal("log-me-in created an account")
	}

	for returnTo, want := range map[string]string{
		"":            "/",
		"/":           "/",
		"/games?id=7": "/games?id=7",
		"/a/b":        "/a/b",
	} {
		resp := do(h, http.MethodPost, "/__dev/log-me-in/a@example.com?returnTo="+url.QueryEscape(returnTo), nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("returnTo %q = %d %q, want %q", returnTo, resp.StatusCode, resp.Header.Get("Location"), want)
			continue
		}
		body := readBody(t, do(h, http.MethodGet, "/", nil, sessionCookie(t, resp)))
		if !strings.Contains(body, "Welcome, a@example.com") {
			t.Errorf("returnTo %q: session not authenticated", returnTo)
		}
	}

	for _, returnTo := range []string{
		"https://evil.example/",
		"//evil.example",
		"/\\evil.example",
		"\\\\evil.example",
		"/%2Fevil.example",
		"/%5Cevil.example",
		"/a\nb",
		"javascript:alert(1)",
		"relative",
		"/%zz",
	} {
		resp := do(h, http.MethodPost, "/__dev/log-me-in/a@example.com?returnTo="+url.QueryEscape(returnTo), nil)
		if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Location") != "" || len(resp.Cookies()) != 0 {
			t.Errorf("returnTo %q = %d %q, want 400", returnTo, resp.StatusCode, resp.Header.Get("Location"))
		}
	}

	r := httptest.NewRequest(http.MethodPost, "https://"+publicHost+"/__dev/log-me-in/a@example.com", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatalf("cross-site log-me-in = %d, want 403", w.Code)
	}
}
