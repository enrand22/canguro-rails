package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/labstack/echo/v4"
)

const requestIDHeader = "X-Request-ID"
const requestIDKey = "canguro.request_id"

// RequestID ensures every request has an id, so a log line in one service can be
// matched with the same request in another. It reads the id from the incoming
// header when a proxy already set one, and generates it otherwise.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id := c.Request().Header.Get(requestIDHeader)
			if id == "" {
				if token, err := RandomToken(); err == nil {
					id = token[:16]
				} else {
					id = "unknown"
				}
			}
			c.Response().Header().Set(requestIDHeader, id)
			c.Set(requestIDKey, id)
			return next(c)
		}
	}
}

// RequestIDFrom returns the request id, if any.
func RequestIDFrom(c echo.Context) string {
	if v, ok := c.Get(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// Recover turns a panic into a 500 instead of a dead process. It logs the panic
// WITH the stack: a recovered panic that leaves no trace is worse than a crash,
// because you keep serving traffic with a broken invariant.
func Recover(logger *slog.Logger) echo.MiddlewareFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("panic recovered",
						"panic", r,
						"path", c.Request().URL.Path,
						"method", c.Request().Method,
						"request_id", RequestIDFrom(c),
						"stack", string(debug.Stack()),
					)
					err := echo.NewHTTPError(http.StatusInternalServerError, "internal error")
					c.Error(err) //nolint:errcheck // we are already handling a panic
				}
			}()
			return next(c)
		}
	}
}

// RequestLoggerSkip es RequestLogger pero **sin registrar** las rutas indicadas.
//
// Existe por el health check del balanceador: pega cada pocos segundos y con el log
// completo se come el archivo. Medido en `mcp_go` el 18-oct-2026: **33 352 de 35 560
// líneas (94 %)** eran `GET /healthz`, así que con `max-size=10m` la historia real
// rotaba y se perdía. El health check **sí se atiende**, sólo no se registra.
//
//	// una app con /healthz esperando al proxy
//	e.Use(middleware.RequestLoggerSkip(logger, "/healthz"))
func RequestLoggerSkip(logger *slog.Logger, skip ...string) echo.MiddlewareFunc {
	omitir := make(map[string]struct{}, len(skip))
	for _, ruta := range skip {
		omitir[ruta] = struct{}{}
	}
	registra := RequestLogger(logger)
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		conLog := registra(next)
		return func(c echo.Context) error {
			if _, ok := omitir[c.Request().URL.Path]; ok {
				return next(c)
			}
			return conLog(c)
		}
	}
}

// RequestLogger logs one line per request with what actually matters when
// something is slow: method, path, status and duration. The request id ties it to
// the client's report.
func RequestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)

			status := c.Response().Status
			if err != nil {
				if httpErr, ok := err.(*echo.HTTPError); ok {
					status = httpErr.Code
				} else {
					status = http.StatusInternalServerError
				}
			}

			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			}

			logger.Log(c.Request().Context(), level, "request",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", status,
				// Milliseconds as a number, not a time.Duration: slog serializes a
				// Duration as NANOSECONDS (json.Marshal of a Duration), so a 1.5 ms
				// page logged as "took":1500000 — unreadable, and rounded to whole
				// milliseconds a fast page logged as a useless 0.
				"took_ms", float64(time.Since(start).Microseconds())/1000.0,
				"request_id", RequestIDFrom(c),
			)
			return err
		}
	}
}
