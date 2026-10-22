// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package server implements the browser-facing HTTP handlers.
package server

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/playbymail/seiicho/internal/auth"
	"github.com/playbymail/seiicho/internal/store"
)

// sessionCookie is the session cookie's name. The __Host- prefix requires the
// cookie to be Secure, host-only, and scoped to the whole site.
const sessionCookie = "__Host-seiicho_session"

// sessionLifetime is how long a session lasts after login.
const sessionLifetime = 14 * 24 * time.Hour

//go:embed templates/*.html
var templateFS embed.FS

var pages = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Config configures the server.
type Config struct {
	// Env is the application environment. It selects the testing helpers:
	// "development" serves them under /__dev and "agent" under /__agent.
	// Every other environment serves none.
	Env     string
	Store   *store.Store
	Version string
	Logger  *slog.Logger
	// Now returns the current time. It defaults to time.Now.
	Now func() time.Time
}

type server struct {
	Config
}

// New returns the application's HTTP handler, protected against CSRF.
func New(cfg Config) http.Handler {
	s := &server{Config: cfg}
	s.Logger = cmp.Or(s.Logger, slog.Default())
	if s.Now == nil {
		s.Now = time.Now
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleLanding)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)

	if prefix := helperPrefix(cfg.Env); prefix != "" {
		mux.HandleFunc("POST "+prefix+"/create-account/{email}", s.handleCreateAccount)
		mux.HandleFunc("POST "+prefix+"/log-me-in/{email}", s.handleLogMeIn)
	}

	return http.NewCrossOriginProtection().Handler(mux)
}

// helperPrefix returns the path prefix for the testing helpers in env, or ""
// if env has none.
func helperPrefix(env string) string {
	switch env {
	case "development":
		return "/__dev"
	case "agent":
		return "/__agent"
	}
	return ""
}

type landingData struct {
	Account *store.Account
	Email   string
	Error   string
}

func (s *server) handleLanding(w http.ResponseWriter, r *http.Request) {
	account, ok := s.currentAccount(r)
	if !ok {
		s.render(w, http.StatusOK, landingData{})
		return
	}
	s.render(w, http.StatusOK, landingData{Account: &account})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	email := normalizeEmail(r.PostFormValue("email"))
	password := r.PostFormValue("password")

	account, err := s.Store.AccountByEmail(r.Context(), email)
	var valid bool
	switch {
	case err == nil:
		valid = auth.CheckPassword(account.PasswordHash, password)
	case errors.Is(err, store.ErrNotFound):
		valid = auth.CheckNoPassword(password)
	default:
		s.serverError(w, "login", err)
		return
	}
	if !valid {
		s.render(w, http.StatusUnauthorized, landingData{Email: email, Error: "Invalid email or password."})
		return
	}
	if err := s.startSession(w, r, account); err != nil {
		s.serverError(w, "login", err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Store.DeleteSession(r.Context(), auth.HashSessionToken(c.Value)); err != nil {
			s.serverError(w, "logout", err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status, code := "ok", http.StatusOK
	if err := s.Store.Ping(r.Context()); err != nil {
		s.Logger.Error("healthz", "err", err)
		status, code = "unavailable", http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{status, s.Version})
}

// handleCreateAccount creates an account with a random password. It is a
// development and agent testing helper; see HTTP_TESTING.md.
func (s *server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	email, ok := parseEmail(r.PathValue("email"))
	if !ok {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	isAdmin := false
	if values, ok := r.URL.Query()["isAdmin"]; ok {
		if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			http.Error(w, "isAdmin must be true or false", http.StatusBadRequest)
			return
		}
		isAdmin = values[0] == "true"
	}

	hash, err := auth.HashPassword(auth.RandomPassword())
	if err != nil {
		s.serverError(w, "create account", err)
		return
	}
	_, err = s.Store.CreateAccount(r.Context(), email, hash, isAdmin, s.Now())
	if errors.Is(err, store.ErrAccountExists) {
		http.Error(w, "account already exists", http.StatusConflict)
		return
	} else if err != nil {
		s.serverError(w, "create account", err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte("account created\n"))
}

// handleLogMeIn starts a session for an existing account without a password.
// It is a development and agent testing helper; see HTTP_TESTING.md.
func (s *server) handleLogMeIn(w http.ResponseWriter, r *http.Request) {
	email, ok := parseEmail(r.PathValue("email"))
	if !ok {
		http.Error(w, "invalid email", http.StatusBadRequest)
		return
	}
	returnTo, ok := safeReturnTo(r.URL.Query().Get("returnTo"))
	if !ok {
		http.Error(w, "invalid returnTo", http.StatusBadRequest)
		return
	}
	account, err := s.Store.AccountByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.serverError(w, "log me in", err)
		return
	}
	if err := s.startSession(w, r, account); err != nil {
		s.serverError(w, "log me in", err)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

// startSession creates a session for the account and sets the session cookie.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, account store.Account) error {
	token, hash := auth.NewSessionToken()
	now := s.Now()
	if err := s.Store.CreateSession(r.Context(), hash, account.ID, now, now.Add(sessionLifetime)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// currentAccount returns the account for the request's session, if any.
func (s *server) currentAccount(r *http.Request) (store.Account, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.Account{}, false
	}
	account, err := s.Store.SessionAccount(r.Context(), auth.HashSessionToken(c.Value), s.Now())
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.Logger.Error("session lookup", "err", err)
		}
		return store.Account{}, false
	}
	return account, true
}

func (s *server) render(w http.ResponseWriter, code int, data landingData) {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, "landing.html", data); err != nil {
		s.serverError(w, "render", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = buf.WriteTo(w)
}

func (s *server) serverError(w http.ResponseWriter, op string, err error) {
	s.Logger.Error(op, "err", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// parseEmail accepts a bare email address, without a display name.
func parseEmail(s string) (string, bool) {
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return "", false
	}
	return normalizeEmail(s), true
}

// safeReturnTo validates a redirect target, accepting only relative paths on
// this origin. An empty value means "/".
func safeReturnTo(s string) (string, bool) {
	if s == "" {
		return "/", true
	}
	// Check both the value and its decoded form, so that encoded slashes and
	// backslashes cannot smuggle in a scheme-relative URL.
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return "", false
	}
	for _, v := range []string{s, decoded} {
		if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsRune(v, '\\') {
			return "", false
		}
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return "", false
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "", false
	}
	return s, true
}
