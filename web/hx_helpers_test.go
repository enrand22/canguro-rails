package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func newCtx(method, target string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	rec := httptest.NewRecorder()
	return e.NewContext(httptest.NewRequest(method, target, nil), rec), rec
}

func TestRemainingHXHelpers(t *testing.T) {
	// Each helper writes exactly one header: if the name is wrong, htmx silently
	// does nothing, which is the worst kind of bug to debug in the browser.
	cases := []struct {
		name   string
		header string
		want   string
		call   func(echo.Context) error
	}{
		{"after-swap trigger", HeaderTriggerAfterSwap, "recargarTabla", func(c echo.Context) error {
			return HXTriggerAfterSwap(c, "recargarTabla", nil)
		}},
		{"retarget", HeaderRetarget, "#otro", func(c echo.Context) error {
			HXRetarget(c, "#otro")
			return nil
		}},
		{"reswap", HeaderReswap, "outerHTML", func(c echo.Context) error {
			HXReswap(c, "outerHTML")
			return nil
		}},
		{"location", "HX-Location", `{"path":"/lista"}`, func(c echo.Context) error {
			return HXLocation(c, "/lista")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newCtx(http.MethodPost, "/")
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := rec.Header().Get(tc.header); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestHXTriggerReportsAnUnserialisablePayload(t *testing.T) {
	c, _ := newCtx(http.MethodPost, "/")
	// A channel cannot be marshalled: the helper must say so instead of sending a
	// half-written header.
	if err := HXTrigger(c, "evento", make(chan int)); err == nil {
		t.Error("HXTrigger should return the marshalling error")
	}
}

func TestSwapOutOfBand(t *testing.T) {
	attrs := SwapOutOfBand("#contador")
	if attrs["hx-swap-oob"] != "#contador" {
		t.Errorf("SwapOutOfBand = %v, want hx-swap-oob=#contador", attrs)
	}
}

func TestRenderFragmentIgnoresTheLayout(t *testing.T) {
	r, e := newTestRenderer()
	e.GET("/fila", func(c echo.Context) error {
		// No htmx header here: a fragment is a fragment regardless of who asks.
		return r.RenderFragment(c, http.StatusOK, fragment())
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fila", nil))

	if strings.Contains(rec.Body.String(), "data-layout") {
		t.Errorf("RenderFragment must not wrap the component in the layout: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Solo el fragmento") {
		t.Errorf("the fragment is missing: %s", rec.Body.String())
	}
}
