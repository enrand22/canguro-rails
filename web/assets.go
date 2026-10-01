package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/labstack/echo/v4"
)

// static holds the starter design system and the vendored htmx. They are
// embedded so a deploy is one binary: no CSS/JS directory to copy to a server,
// and no chance of running a binary with yesterday's stylesheet.
//
// htmx is vendored on purpose instead of loaded from a CDN: a page that depends
// on a third-party origin cannot run offline, breaks under a strict CSP, and
// hands a stranger the ability to change your app's behaviour.
//
//go:embed static
var static embed.FS

// StaticFS returns the embedded assets rooted at static/ (so `/css/...` and
// `/js/...` work directly).
func StaticFS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		// Impossible unless the embed directive above is wrong; failing loudly
		// at startup is better than serving 404s.
		panic("web: embedded static directory is missing: " + err.Error())
	}
	return sub
}

// RegisterStatic mounts the assets on the given prefix. An empty prefix serves
// them from the root (`/css/design-system.css`, `/js/htmx.min.js`).
func RegisterStatic(e *echo.Echo, prefix string) {
	handler := http.StripPrefix(prefix, http.FileServer(http.FS(StaticFS())))
	e.GET(prefix+"/*", echo.WrapHandler(handler))
}
