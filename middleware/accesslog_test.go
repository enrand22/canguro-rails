package middleware

// El logger de accesos no debe registrar el health check del balanceador: pega cada
// pocos segundos y se come el archivo (medido en mcp_go: 94 % de las líneas).

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestRequestLoggerSkipOmiteElHealthCheck(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	e.Use(RequestLoggerSkip(logger, "/healthz"))
	e.GET("/healthz", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	e.POST("/mcp", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	// El health check se atiende pero NO se registra.
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d: tiene que seguir respondiendo", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("/healthz no debería registrarse y escribió: %q", buf.String())
	}

	// Un request real sí aparece.
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	linea := buf.String()
	if !strings.Contains(linea, "path=/mcp") || !strings.Contains(linea, "msg=request") {
		t.Errorf("el request real no se registró: %q", linea)
	}
	if !strings.Contains(linea, "took_ms=") {
		t.Errorf("falta took_ms en la línea: %q", linea)
	}
}

func TestRequestLoggerSkipSinRutasRegistraTodo(t *testing.T) {
	// Sin rutas es equivalente al RequestLogger de siempre: nada que saltear.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	e := echo.New()
	e.Use(RequestLoggerSkip(logger))
	e.GET("/healthz", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if buf.Len() == 0 {
		t.Error("sin rutas que saltear, /healthz tenía que registrarse")
	}
}
