package middleware

import (
	"crypto/rand"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
)

// randRead is a seam for tests: crypto/rand cannot be made to fail on demand, and
// "what happens when the token cannot be generated" deserves a test.
var randRead = rand.Read

// CSRF protects state-changing methods with the double-submit technique: the
// token lives in the signed session cookie, and the form repeats it in a hidden
// field or in a header (which is what htmx sends).
type CSRF struct {
	HeaderName string
	FieldName  string
}

// ErrCSRF is returned for a missing or invalid token.
var ErrCSRF = errors.New("middleware: invalid CSRF token")

// NewCSRF builds the middleware with the usual names.
func NewCSRF() *CSRF {
	return &CSRF{HeaderName: "X-CSRF-Token", FieldName: "_csrf"}
}

// Protect validates the token on POST, PUT, PATCH and DELETE.
func (m *CSRF) Protect() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			switch c.Request().Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return next(c)
			}
			expected := CSRFToken(c)
			if expected == "" {
				return echo.NewHTTPError(http.StatusForbidden, "session without a CSRF token (is the session middleware mounted before this one?)")
			}
			got := c.Request().Header.Get(m.HeaderName)
			if got == "" {
				got = c.FormValue(m.FieldName)
			}
			if got != expected {
				return echo.NewHTTPError(http.StatusForbidden, "invalid CSRF token")
			}
			return next(c)
		}
	}
}

// TokenField renders the hidden input a plain HTML form needs. Templates call it
// instead of hardcoding the field name in every form.
func TokenField(c echo.Context) string {
	token := CSRFToken(c)
	if token == "" {
		return ""
	}
	return `<input type="hidden" name="_csrf" value="` + token + `"/>`
}
