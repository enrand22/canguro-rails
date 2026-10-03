// Package middleware holds the cross-cutting HTTP pieces: session, CSRF, request
// id, panic recovery and request logging.
//
// The session is a SIGNED COOKIE (HMAC-SHA256), not a server-side store: it needs
// no extra table, it survives restarts, and it keeps stateless deploys possible.
// What travels inside is not secret (a user id, a flash message, a CSRF token) —
// it only has to be impossible to forge.
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// DefaultCookieName is used when Session.CookieName is empty.
const DefaultCookieName = "session"

// Session errors.
var (
	ErrSessionInvalid = errors.New("middleware: invalid session")
	ErrSessionExpired = errors.New("middleware: expired session")
)

// Session configures the session middleware.
type Session struct {
	// Secret is the HMAC key. Must be at least 32 bytes: a short signing key is
	// a forgeable one.
	Secret []byte
	// TTL is how long a session lives. Defaults to 12h.
	TTL time.Duration
	// Secure marks cookies HTTPS-only. It MUST be true in production and false on
	// plain-HTTP development, otherwise the browser silently drops the cookie and
	// every POST fails with a CSRF error that looks like a bug in your code.
	Secure bool
	// CookieName defaults to DefaultCookieName.
	CookieName string
}

// sessionData is what travels signed inside the cookie.
type sessionData struct {
	UserID  int64  `json:"uid,omitempty"`
	Flash   string `json:"fl,omitempty"`
	CSRF    string `json:"cs,omitempty"`
	Expires int64  `json:"exp,omitempty"` // epoch seconds
}

const ctxKey = "canguro.session"

// Load reads and verifies the cookie and leaves the data in the context. A
// missing or expired cookie is NOT an error: the request continues anonymous.
func (s *Session) Load() echo.MiddlewareFunc {
	if s.TTL == 0 {
		s.TTL = 12 * time.Hour
	}
	if s.CookieName == "" {
		s.CookieName = DefaultCookieName
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			data := s.read(c)
			isNew := data.Expires == 0
			if isNew {
				data.Expires = time.Now().Add(s.TTL).Unix()
			}
			if data.CSRF == "" {
				token, err := RandomToken()
				if err != nil {
					return err
				}
				data.CSRF = token
				isNew = true
			}
			c.Set(ctxKey, data)

			// A brand-new session must be PERSISTED here. Without this the CSRF
			// token only lives in the context: the form paints it, but the next
			// request does not carry it back and the double-submit check fails —
			// every POST answers 403 and it looks like a bug in the handler.
			if isNew {
				if err := s.write(c, data); err != nil {
					return err
				}
			}
			return next(c)
		}
	}
}

// read returns the session from the cookie, or an empty one when it is missing,
// malformed or has a bad signature.
func (s *Session) read(c echo.Context) sessionData {
	cookie, err := c.Cookie(s.CookieName)
	if err != nil || cookie.Value == "" {
		return sessionData{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return sessionData{}
	}
	// The payload is JSON and CAN contain dots — a flash like "total 3222.00", a
	// date, an abbreviation. Splitting on the FIRST dot cut the payload in half, the
	// signature stopped matching and the session read back empty: the user was
	// logged out on the next click (found in production, payhub, 2-oct-2026).
	// The signature is base64url (no dots), so the separator is the LAST dot.
	idx := strings.LastIndex(string(raw), ".")
	if idx < 0 {
		return sessionData{}
	}
	payload, signature := string(raw)[:idx], string(raw)[idx+1:]
	if !hmac.Equal([]byte(signature), []byte(s.sign(payload))) {
		return sessionData{} // tampered cookie
	}
	var data sessionData
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return sessionData{}
	}
	if data.Expires > 0 && time.Now().Unix() > data.Expires {
		return sessionData{} // expired
	}
	return data
}

func (s *Session) sign(payload string) string {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Session) write(c echo.Context, data sessionData) error {
	if data.Expires == 0 {
		data.Expires = time.Now().Add(s.TTL).Unix()
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	value := string(payload) + "." + s.sign(string(payload))
	cookie := &http.Cookie{
		Name:     s.CookieName,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(value)),
		Path:     "/",
		HttpOnly: true,                 // not reachable from JavaScript
		Secure:   s.Secure,             // HTTPS only in production
		SameSite: http.SameSiteLaxMode, // stops third-party CSRF
		Expires:  time.Unix(data.Expires, 0),
	}

	// Exactly ONE cookie per response. Load() writes the anonymous session and a
	// handler may then log the user in: two Set-Cookie headers with the same name
	// are ambiguous (browsers keep the last one, but curl-based debugging and
	// other clients see a different session), so the previous one is replaced.
	header := c.Response().Header()
	previous := header.Values("Set-Cookie")
	header.Del("Set-Cookie")
	for _, v := range previous {
		if !strings.HasPrefix(v, s.CookieName+"=") {
			header.Add("Set-Cookie", v)
		}
	}
	http.SetCookie(c.Response(), cookie)
	c.Set(ctxKey, data)
	return nil
}

// SetUser logs a user in.
func (s *Session) SetUser(c echo.Context, userID int64) error {
	data := current(c)
	data.UserID = userID
	data.Expires = time.Now().Add(s.TTL).Unix()
	return s.write(c, data)
}

// Clear logs out: the cookie is deleted and a fresh CSRF token is issued.
func (s *Session) Clear(c echo.Context) error {
	http.SetCookie(c.Response(), &http.Cookie{
		Name: s.CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
	data := sessionData{Expires: time.Now().Add(s.TTL).Unix()}
	if token, err := RandomToken(); err == nil {
		data.CSRF = token
	}
	c.Set(ctxKey, data)
	return nil
}

// SetFlash stores a one-shot message for the next render.
func (s *Session) SetFlash(c echo.Context, message string) error {
	data := current(c)
	data.Flash = message
	return s.write(c, data)
}

// TakeFlash reads the flash message and consumes it.
func (s *Session) TakeFlash(c echo.Context) string {
	data := current(c)
	if data.Flash == "" {
		return ""
	}
	msg := data.Flash
	data.Flash = ""
	_ = s.write(c, data)
	return msg
}

// ── Accessors used by handlers and views ────────────────────────────────────

func current(c echo.Context) sessionData {
	if v, ok := c.Get(ctxKey).(sessionData); ok {
		return v
	}
	return sessionData{}
}

// UserID returns the logged-in user id, or 0 when anonymous.
func UserID(c echo.Context) int64 { return current(c).UserID }

// IsLoggedIn reports whether there is a user in the session.
func IsLoggedIn(c echo.Context) bool { return UserID(c) != 0 }

// CSRFToken returns the session's CSRF token (what forms paint).
func CSRFToken(c echo.Context) string { return current(c).CSRF }

// RandomToken generates a 32-byte base64url token, used for CSRF.
func RandomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := randRead(buf); err != nil {
		return "", errors.New("middleware: cannot generate a random token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
