package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

// raw and wrapper are tiny templ.Components written by hand: a test layout does
// not need the templ compiler, it needs to prove what the renderer chose to send.
type raw string

func (r raw) Render(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, string(r))
	return err
}

func page() templ.Component {
	return raw(`<h1>Página</h1>`)
}

func fragment() templ.Component {
	return raw(`<tbody id="rows"><tr><td>Solo el fragmento</td></tr></tbody>`)
}

type wrapper struct {
	title string
	page  templ.Component
}

func (w wrapper) Render(ctx context.Context, out io.Writer) error {
	if _, err := io.WriteString(out, `<html data-layout="`+w.title+`">`); err != nil {
		return err
	}
	if err := w.page.Render(ctx, out); err != nil {
		return err
	}
	_, err := io.WriteString(out, `</html>`)
	return err
}

// layout marks itself so tests can prove the shell was (or wasn't) rendered.
func layout(title string, content templ.Component) templ.Component {
	return wrapper{title: title, page: content}
}

func newTestRenderer() (*Renderer, *echo.Echo) {
	e := echo.New()
	return NewRenderer(layout, nil), e
}

func TestRenderFullPageWithoutHTMX(t *testing.T) {
	r, e := newTestRenderer()
	h := func(c echo.Context) error { return r.Render(c, http.StatusOK, View{Title: "Inicio", Page: page()}) }
	e.GET("/", h)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-layout="Inicio"`) {
		t.Errorf("a normal request must render inside the layout, got: %s", body)
	}
	if !strings.Contains(body, "<h1>Página</h1>") {
		t.Errorf("the page content is missing: %s", body)
	}
	if ct := rec.Header().Get(echo.HeaderContentType); !strings.Contains(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
}

func TestRenderHTMXRequestReturnsOnlyTheFragment(t *testing.T) {
	r, e := newTestRenderer()
	h := func(c echo.Context) error {
		return r.Render(c, http.StatusOK, View{Title: "Inicio", Page: page(), Partial: fragment()})
	}
	e.GET("/", h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequest, "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "data-layout") {
		t.Errorf("an htmx request must NOT get the layout: %s", body)
	}
	if !strings.Contains(body, "Solo el fragmento") {
		t.Errorf("the fragment is missing: %s", body)
	}
}

func TestRenderHTMXWithoutPartialFallsBackToThePage(t *testing.T) {
	r, e := newTestRenderer()
	h := func(c echo.Context) error { return r.Render(c, http.StatusOK, View{Page: page()}) }
	e.GET("/", h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequest, "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "<h1>Página</h1>") {
		t.Errorf("with no Partial, htmx should get the page content: %s", rec.Body.String())
	}
}

func TestRenderInvalidUses422(t *testing.T) {
	r, e := newTestRenderer()
	h := func(c echo.Context) error {
		return r.RenderInvalid(c, View{Title: "Formulario", Page: page(), Partial: fragment()})
	}
	e.POST("/transferencias", h)

	req := httptest.NewRequest(http.MethodPost, "/transferencias", nil)
	req.Header.Set(HeaderRequest, "true")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 (the form comes back with the errors)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Solo el fragmento") {
		t.Errorf("the form fragment is missing: %s", rec.Body.String())
	}
}

func TestRedirectIsHTMXAware(t *testing.T) {
	r, e := newTestRenderer()
	h := func(c echo.Context) error { return r.Redirect(c, "/otra") }
	e.GET("/", h)

	// Normal navigation: a real 303 the browser follows.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("normal redirect status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/otra" {
		t.Errorf("Location = %q, want /otra", loc)
	}

	// htmx: a header, because fetch() would silently follow a 302 and swap a
	// whole page into a fragment's place.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequest, "true")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if got := rec.Header().Get(HeaderRedirect); got != "/otra" {
		t.Errorf("HX-Redirect = %q, want /otra", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("htmx redirect status = %d, want 200", rec.Code)
	}
}

func TestHXHelpers(t *testing.T) {
	e := echo.New()

	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)

	if err := HXTrigger(c, "toast", map[string]string{"message": "listo"}); err != nil {
		t.Fatalf("HXTrigger: %v", err)
	}
	got := rec.Header().Get(HeaderTrigger)
	if got != `{"toast":{"message":"listo"}}` {
		t.Errorf("HX-Trigger = %q, want the JSON payload", got)
	}

	rec = httptest.NewRecorder()
	c = e.NewContext(httptest.NewRequest(http.MethodPost, "/", nil), rec)
	if err := HXTrigger(c, "reload", nil); err != nil {
		t.Fatalf("HXTrigger without payload: %v", err)
	}
	if got := rec.Header().Get(HeaderTrigger); got != "reload" {
		t.Errorf("HX-Trigger = %q, want the bare event name", got)
	}

	rec = httptest.NewRecorder()
	c = e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	HXPushURL(c, "/?page=2")
	if got := rec.Header().Get(HeaderPushURL); got != "/?page=2" {
		t.Errorf("HX-Push-Url = %q", got)
	}

	rec = httptest.NewRecorder()
	c = e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	if err := HXRefresh(c); err != nil {
		t.Fatalf("HXRefresh: %v", err)
	}
	if got := rec.Header().Get(HeaderRefresh); got != "true" {
		t.Errorf("HX-Refresh = %q, want true", got)
	}
}

func TestIsHTMXAndTarget(t *testing.T) {
	e := echo.New()

	plain := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	if IsHTMX(plain) {
		t.Error("IsHTMX() = true for a normal request")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequest, "true")
	req.Header.Set(HeaderTarget, "#tabla")
	htmxReq := e.NewContext(req, httptest.NewRecorder())
	if !IsHTMX(htmxReq) {
		t.Error("IsHTMX() = false for an htmx request")
	}
	if got := Target(htmxReq); got != "#tabla" {
		t.Errorf("Target() = %q, want #tabla", got)
	}
}

func TestStaticAssetsAreEmbedded(t *testing.T) {
	e := echo.New()
	RegisterStatic(e, "")

	for _, path := range []string{"/js/htmx.min.js", "/css/design-system.css"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s → %d, want 200 (the asset must be embedded)", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served an empty body", path)
		}
	}
}
