package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func newSession() *Session {
	return &Session{Secret: []byte(strings.Repeat("k", 32)), TTL: time.Hour}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSessionIssuesASignedCookieWithACSRFToken(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.GET("/", func(c echo.Context) error {
		if CSRFToken(c) == "" {
			t.Error("the session should carry a CSRF token after Load")
		}
		if IsLoggedIn(c) {
			t.Error("a fresh session must be anonymous")
		}
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie was set: the CSRF token would never come back and every POST would 403")
	}
	c := cookies[0]
	if c.Name != DefaultCookieName {
		t.Errorf("cookie name = %q, want %q", c.Name, DefaultCookieName)
	}
	if !c.HttpOnly {
		t.Error("the session cookie must be HttpOnly")
	}
	if c.Secure {
		t.Error("Secure should be false unless the Session was configured for HTTPS")
	}
}

func TestSessionKeepsTheUserAcrossRequests(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.POST("/login", func(c echo.Context) error { return s.SetUser(c, 42) })
	e.GET("/whoami", func(c echo.Context) error {
		if got := UserID(c); got != 42 {
			t.Errorf("UserID = %d, want 42", got)
		}
		return c.NoContent(http.StatusOK)
	})

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/login", nil))

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	for _, c := range login.Result().Cookies() {
		req.AddCookie(c)
	}
	e.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSessionRejectsATamperedCookie(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.GET("/", func(c echo.Context) error {
		if IsLoggedIn(c) {
			t.Error("a tampered cookie must not authenticate anyone")
		}
		return c.NoContent(http.StatusOK)
	})

	// A cookie with a valid JSON payload but a garbage signature.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: DefaultCookieName, Value: "eyJ1aWQiOjF9.no-es-la-firma"})
	e.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSessionIgnoresAnExpiredCookie(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.GET("/", func(c echo.Context) error {
		if IsLoggedIn(c) {
			t.Error("an expired session must be anonymous")
		}
		return c.NoContent(http.StatusOK)
	})

	// Sign a payload that expired an hour ago, using the same signing code.
	payload := `{"uid":7,"exp":` + itoa(time.Now().Add(-time.Hour).Unix()) + `}`
	value := payload + "." + s.sign(payload)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: DefaultCookieName, Value: b64(value)})
	e.ServeHTTP(httptest.NewRecorder(), req)
}

func TestFlashIsOneShot(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.POST("/set", func(c echo.Context) error { return s.SetFlash(c, "guardado") })

	var first, second string
	e.GET("/read", func(c echo.Context) error {
		first = s.TakeFlash(c)
		second = s.TakeFlash(c)
		return c.NoContent(http.StatusOK)
	})

	set := httptest.NewRecorder()
	e.ServeHTTP(set, httptest.NewRequest(http.MethodPost, "/set", nil))

	req := httptest.NewRequest(http.MethodGet, "/read", nil)
	for _, c := range set.Result().Cookies() {
		req.AddCookie(c)
	}
	e.ServeHTTP(httptest.NewRecorder(), req)

	if first != "guardado" {
		t.Errorf("first read = %q, want the flash message", first)
	}
	if second != "" {
		t.Errorf("second read = %q, want it consumed (flash is one-shot)", second)
	}
}

// The session payload is JSON and the signature is appended after a dot. If any
// text in the session contains a dot — a flash like "saved: total 3222.00", a date,
// an abbreviation — splitting on the FIRST dot cuts the payload in half, the
// signature no longer matches and the session reads back EMPTY: the user is logged
// out on the very next click. Found in production (payhub, 2-oct-2026): the record
// saved fine and the redirect landed on the login page.
func TestSessionSurvivesAFlashWithADot(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load())
	e.POST("/login", func(c echo.Context) error { return s.SetUser(c, 42) })
	e.POST("/save", func(c echo.Context) error {
		return s.SetFlash(c, "Registro cargado: 2 transferencia(s) por 3222.00.")
	})

	var seenUser int64
	var seenFlash string
	e.GET("/panel", func(c echo.Context) error {
		seenUser = UserID(c)
		seenFlash = s.TakeFlash(c)
		return c.NoContent(http.StatusOK)
	})

	login := httptest.NewRecorder()
	e.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/login", nil))

	save := httptest.NewRequest(http.MethodPost, "/save", nil)
	for _, c := range login.Result().Cookies() {
		save.AddCookie(c)
	}
	saved := httptest.NewRecorder()
	e.ServeHTTP(saved, save)

	panel := httptest.NewRequest(http.MethodGet, "/panel", nil)
	for _, c := range saved.Result().Cookies() {
		panel.AddCookie(c)
	}
	e.ServeHTTP(httptest.NewRecorder(), panel)

	if seenUser != 42 {
		t.Errorf("the session lost its user after a flash containing a dot: UserID = %d, want 42", seenUser)
	}
	if seenFlash == "" {
		t.Error("the flash message should still be readable")
	}
}

func TestCSRFProtectsStateChangingMethods(t *testing.T) {
	s := newSession()
	csrf := NewCSRF()
	e := echo.New()
	e.Use(s.Load(), csrf.Protect())
	e.POST("/guardar", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/ver", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	// GET passes with no token at all.
	first := httptest.NewRecorder()
	e.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/ver", nil))
	if first.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200", first.Code)
	}
	var sessionCookie *http.Cookie
	for _, c := range first.Result().Cookies() {
		sessionCookie = c
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie to reuse")
	}

	// We need the token that lives inside the session: ask the session itself.
	// (The request must hit the handler that captures it, of course.)
	var token string
	e.GET("/token", func(c echo.Context) error {
		token = CSRFToken(c)
		return c.NoContent(http.StatusOK)
	})
	tokenReq := httptest.NewRequest(http.MethodGet, "/token", nil)
	tokenReq.AddCookie(sessionCookie)
	e.ServeHTTP(httptest.NewRecorder(), tokenReq)
	if token == "" {
		t.Fatal("could not read the session's CSRF token")
	}

	// POST without the token: 403.
	bad := httptest.NewRequest(http.MethodPost, "/guardar", nil)
	bad.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, bad)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without a token = %d, want 403", rec.Code)
	}

	// POST with the header (what htmx sends): allowed.
	good := httptest.NewRequest(http.MethodPost, "/guardar", nil)
	good.AddCookie(sessionCookie)
	good.Header.Set(csrf.HeaderName, token)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, good)
	if rec.Code != http.StatusOK {
		t.Errorf("POST with a valid token = %d, want 200", rec.Code)
	}

	// POST with the hidden field (what a plain form sends): allowed too.
	form := httptest.NewRequest(http.MethodPost, "/guardar",
		strings.NewReader(csrf.FieldName+"="+token))
	form.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	form.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, form)
	if rec.Code != http.StatusOK {
		t.Errorf("POST with the form field = %d, want 200", rec.Code)
	}
}

func TestCSRFRejectsAWrongToken(t *testing.T) {
	s := newSession()
	e := echo.New()
	e.Use(s.Load(), NewCSRF().Protect())
	e.POST("/guardar", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/guardar", nil)
	req.Header.Set("X-CSRF-Token", "inventado")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestRequestIDIsGeneratedAndPropagated(t *testing.T) {
	e := echo.New()
	e.Use(RequestID())
	e.GET("/", func(c echo.Context) error {
		if RequestIDFrom(c) == "" {
			t.Error("the request id should be available in the context")
		}
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get(requestIDHeader) == "" {
		t.Error("the request id should be echoed in the response header")
	}

	// A proxy that already set one must be respected, so logs from both services
	// can be joined.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "abc123")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if got := rec.Header().Get(requestIDHeader); got != "abc123" {
		t.Errorf("request id = %q, want the incoming abc123", got)
	}
}

func TestRecoverTurnsAPanicInto500(t *testing.T) {
	e := echo.New()
	e.Use(Recover(quiet()))
	e.GET("/boom", func(c echo.Context) error { panic("algo se rompió") })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (and the process must survive)", rec.Code)
	}
}

func TestRequestLoggerDoesNotChangeTheResponse(t *testing.T) {
	e := echo.New()
	e.Use(RequestLogger(quiet()))
	e.GET("/", func(c echo.Context) error { return c.String(http.StatusTeapot, "hola") })

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", rec.Code)
	}
	if rec.Body.String() != "hola" {
		t.Errorf("body = %q, want hola", rec.Body.String())
	}
}

// Asserting only that the response is unchanged is not enough: the line has to say
// something useful when there is an incident. An empty request_id (the middleware
// wrote its id under a DIFFERENT key than the logger reads) and a duration in
// nanoseconds both went unnoticed until this test existed.
func TestRequestLoggerWritesWhatMatters(t *testing.T) {
	var buf bytes.Buffer
	e := echo.New()
	e.Use(RequestID())
	e.Use(RequestLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	e.GET("/banco", func(c echo.Context) error { return c.NoContent(http.StatusTeapot) })

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/banco", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("the log line is not JSON: %v — %s", err, buf.String())
	}
	if line["msg"] != "request" {
		t.Errorf("msg = %v, want request", line["msg"])
	}
	if line["method"] != http.MethodGet || line["path"] != "/banco" {
		t.Errorf("method/path = %v/%v, want GET//banco", line["method"], line["path"])
	}
	if line["status"] != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want 418", line["status"])
	}

	// The request id is what ties a user's report to this exact line.
	if id, _ := line["request_id"].(string); id == "" {
		t.Errorf("request_id is empty: the line cannot be matched to a report (%v)", line["request_id"])
	}

	// Milliseconds as a number: a Duration would be serialized as nanoseconds.
	took, ok := line["took_ms"].(float64)
	if !ok {
		t.Fatalf("took_ms = %v (%T), want a number of milliseconds", line["took_ms"], line["took_ms"])
	}
	if took < 0 {
		t.Errorf("took_ms = %v, want a sane duration", took)
	}
}

// small helpers so the tests above stay readable.
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
