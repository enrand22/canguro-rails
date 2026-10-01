package dbx

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseRubyDBURL translates a Rails-style URL into a go-sql-driver DSN:
//
//	mysql2://user:pass@host:3306/database
//	  -> user:pass@tcp(host:3306)/database?parseTime=true&loc=UTC&charset=utf8mb4,utf8
//
// Why it exists: when you take over a service from another stack, the
// credentials you inherit are usually URLs like this one, sitting in an env file
// you would rather not rewrite. The app should speak both.
//
// The query parameters are not decoration:
//
//   - parseTime=true  → DATETIME columns arrive as time.Time instead of []byte
//   - loc=UTC         → a datetime goes in and comes back out identical
//   - charset=utf8mb4,utf8 → utf8mb4 with a fallback for pre-5.5.3 servers
//   - timeouts        → a hung database doesn't hang your request forever
func ParseRubyDBURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("dbx: empty database URL")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("dbx: unparsable database URL: %w", err)
	}
	if !strings.HasPrefix(u.Scheme, "mysql") {
		return "", fmt.Errorf("dbx: unsupported scheme %q (only mysql/mysql2)", u.Scheme)
	}

	user := u.User.Username()
	pass, hasPass := u.User.Password()
	if user == "" {
		return "", fmt.Errorf("dbx: database URL has no user")
	}

	host := u.Host
	if host == "" {
		return "", fmt.Errorf("dbx: database URL has no host")
	}
	if !strings.Contains(host, ":") {
		host += ":3306"
	}

	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", fmt.Errorf("dbx: database URL has no database name")
	}

	credentials := user
	if hasPass {
		credentials += ":" + pass
	}

	const params = "timeout=10s&readTimeout=30s&parseTime=true&loc=UTC&charset=utf8mb4,utf8"
	return fmt.Sprintf("%s@tcp(%s)/%s?%s", credentials, host, name, params), nil
}
