// Package web is the HTTP layer: one renderer that speaks both worlds (full page
// and htmx fragment), plus the helpers that make htmx responses explicit.
//
// The rule this package exists to enforce: a handler should be written ONCE. The
// same function answers a normal navigation (full page) and an htmx request
// (just the fragment), and the difference is decided here instead of being
// duplicated in every handler.
package web

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// Layout wraps page content in the site shell (head, navigation, flash).
type Layout func(title string, page templ.Component) templ.Component

// Renderer renders pages with the app's layout.
type Renderer struct {
	layout Layout
	logger *slog.Logger
}

// NewRenderer builds the renderer. A nil layout means "render the page as-is",
// which is what an API-ish service or a test wants.
func NewRenderer(layout Layout, logger *slog.Logger) *Renderer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Renderer{layout: layout, logger: logger}
}

// View is what a handler returns to be rendered.
//
//	Page    — the content that goes inside the layout (full navigation).
//	Partial — what an htmx request gets instead. When nil, htmx gets Page too
//	          (correct for pages that are already a single fragment).
type View struct {
	Title   string
	Page    templ.Component
	Partial templ.Component
}

// Render writes the right thing for the request: the fragment for htmx, the full
// page otherwise.
func (r *Renderer) Render(c echo.Context, status int, v View) error {
	if IsHTMX(c) {
		fragment := v.Partial
		if fragment == nil {
			fragment = v.Page
		}
		return r.write(c, status, fragment)
	}
	if r.layout == nil {
		return r.write(c, status, v.Page)
	}
	return r.write(c, status, r.layout(v.Title, v.Page))
}

// RenderInvalid re-renders a form after a validation error, with status 422.
//
// This is the htmx answer to "the user sent bad data": the server returns the
// form with the errors painted, htmx swaps it in place, and no JavaScript had to
// validate anything. Non-htmx clients (and curl) get the full page with a 422.
func (r *Renderer) RenderInvalid(c echo.Context, v View) error {
	return r.Render(c, http.StatusUnprocessableEntity, v)
}

// RenderFragment writes a component with no layout at all. Use it for responses
// that are *by definition* a fragment (a table body, a row after a mutation).
func (r *Renderer) RenderFragment(c echo.Context, status int, fragment templ.Component) error {
	return r.write(c, status, fragment)
}

// Redirect sends a browser redirect, or an htmx-aware one when the request comes
// from htmx: a normal 302 would be silently followed by fetch() and the client
// would inject the whole target page into the fragment's place.
func (r *Renderer) Redirect(c echo.Context, url string) error {
	if IsHTMX(c) {
		return HXRedirect(c, url)
	}
	return c.Redirect(http.StatusSeeOther, url)
}

func (r *Renderer) write(c echo.Context, status int, component templ.Component) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(status)
	if err := component.Render(c.Request().Context(), c.Response().Writer); err != nil {
		r.logger.Error("render failed", "error", err, "path", c.Request().URL.Path)
		return err
	}
	return nil
}
