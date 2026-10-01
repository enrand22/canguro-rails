package web

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

// The htmx request/response headers this package speaks. They are constants so a
// typo becomes a compile error instead of a silent behaviour change.
const (
	HeaderRequest  = "HX-Request"
	HeaderTarget   = "HX-Target"
	HeaderTrigger  = "HX-Trigger"
	HeaderRedirect = "HX-Redirect"
	HeaderRefresh  = "HX-Refresh"
	HeaderPushURL  = "HX-Push-Url"
	HeaderReswap   = "HX-Reswap"
	HeaderRetarget = "HX-Retarget"

	// HeaderTriggerAfterSwap runs an event after the swap, which is what you use
	// when the script needs the new DOM to exist.
	HeaderTriggerAfterSwap = "HX-Trigger-After-Swap"
)

// IsHTMX reports whether the request came from htmx. It is the single check the
// whole framework agrees on: a handler, the renderer and the redirect helper all
// ask this question instead of peeking at headers themselves.
func IsHTMX(c echo.Context) bool {
	return c.Request().Header.Get(HeaderRequest) == "true"
}

// HXRedirect tells the browser to navigate somewhere else. htmx does it with a
// header instead of a 3xx on purpose: fetch() would follow a 302 transparently
// and swap an entire page into a fragment's place.
func HXRedirect(c echo.Context, url string) error {
	c.Response().Header().Set(HeaderRedirect, url)
	return c.NoContent(http.StatusOK)
}

// HXRefresh forces a full page reload — the honest answer when the server cannot
// know what state the client is in ("their session expired", "someone else
// changed the record").
func HXRefresh(c echo.Context) error {
	c.Response().Header().Set(HeaderRefresh, "true")
	return c.NoContent(http.StatusOK)
}

// HXTrigger fires a client-side event after the response is swapped in. The
// optional payload is JSON-serialised into the header.
//
//	HXTrigger(c, "toast", map[string]string{"message": "Transferencia registrada"})
func HXTrigger(c echo.Context, event string, payload any) error {
	value, err := triggerValue(event, payload)
	if err != nil {
		return err
	}
	c.Response().Header().Set(HeaderTrigger, value)
	return nil
}

// HXTriggerAfterSwap is HXTrigger for listeners that need the updated DOM.
func HXTriggerAfterSwap(c echo.Context, event string, payload any) error {
	value, err := triggerValue(event, payload)
	if err != nil {
		return err
	}
	c.Response().Header().Set(HeaderTriggerAfterSwap, value)
	return nil
}

// triggerValue builds the header value htmx expects: a bare event name when
// there is no payload, or {"event": payload} when there is.
func triggerValue(event string, payload any) (string, error) {
	if payload == nil {
		return event, nil
	}
	raw, err := json.Marshal(map[string]any{event: payload})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// HXPushURL updates the address bar without a navigation, so a filtered list
// stays shareable and the back button behaves.
func HXPushURL(c echo.Context, url string) {
	c.Response().Header().Set(HeaderPushURL, url)
}

// HXRetarget sends the response to a different element than the one that
// triggered it, and HXReswap changes how it is inserted.
func HXRetarget(c echo.Context, selector string) {
	c.Response().Header().Set(HeaderRetarget, selector)
}

func HXReswap(c echo.Context, strategy string) {
	c.Response().Header().Set(HeaderReswap, strategy)
}

// HXLocation tells htmx to do a client-side redirect to a URL with optional
// context, without closing the connection first.
func HXLocation(c echo.Context, url string) error {
	raw, err := json.Marshal(map[string]any{"path": url})
	if err != nil {
		return err
	}
	c.Response().Header().Set("HX-Location", string(raw))
	return c.NoContent(http.StatusOK)
}

// Target reports which element the htmx request came from (useful when one
// endpoint serves several buttons).
func Target(c echo.Context) string {
	return c.Request().Header.Get(HeaderTarget)
}

// SwapOutOfBand wraps a component that must be replaced outside the swap target.
// It is how "update the counter in the header too" is done without extra requests.
func SwapOutOfBand(selector string) map[string]string {
	return map[string]string{"hx-swap-oob": selector}
}
