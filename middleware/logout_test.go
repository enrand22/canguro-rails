package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestClearLogsTheUserOutAndKeepsAToken(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.POST("/login", func(c echo.Context) error { return s.SetUser(c, 9) })
	e.POST("/logout", func(c echo.Context) error { return s.Clear(c) })

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/login", nil))

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	// The logout response must expire the cookie...
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == DefaultCookieName && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("Clear should expire the session cookie")
	}
}

func TestClearLeavesAFreshCSRFTokenForTheLoginForm(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.POST("/logout", func(c echo.Context) error { return s.Clear(c) })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/logout", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("Clear should leave a usable session cookie (the login form needs a CSRF token)")
	}
}

func TestTokenFieldRendersTheHiddenInput(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.GET("/", func(c echo.Context) error {
		html := TokenField(c)
		if !strings.Contains(html, `name="_csrf"`) {
			t.Errorf("TokenField should name the field _csrf: %s", html)
		}
		if !strings.Contains(html, CSRFToken(c)) {
			t.Errorf("TokenField should carry the session token: %s", html)
		}
		return c.NoContent(http.StatusOK)
	})

	// Without the session middleware there is no token, and the helper must not
	// emit an empty field (a form that posts an empty CSRF token is a 403 waiting
	// to happen, and an empty input is worse than none: it looks fine).
	bare := echo.New()
	bare.GET("/", func(c echo.Context) error {
		if got := TokenField(c); got != "" {
			t.Errorf("TokenField without a session = %q, want empty", got)
		}
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	bare.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
