// Package config loads and validates application configuration exactly once, at
// boot, so a missing variable fails the process instead of a request later on.
//
// The app declares its variables; this package validates them, applies defaults
// and hands back typed values. No os.Getenv calls scattered through the code.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Var declares one configuration variable.
//
//	Name:     environment variable name, used verbatim in error messages.
//	Required: the app must not start without it.
//	Default:  used when the variable is missing (ignored when Required).
//	Validate: extra rule, applied to the final value (default included).
type Var struct {
	Name     string
	Required bool
	Default  string
	Validate func(string) error
}

// Config is the validated configuration. Values are read-only through getters.
type Config struct {
	values map[string]string
}

// Load reads a .env file when present and then loads vars from the environment.
//
// .env is a developer convenience only: in production the environment is
// injected by the process supervisor (systemd EnvironmentFile, Kamal secrets).
func Load(vars []Var) (*Config, error) {
	_ = godotenv.Load()
	return LoadFrom(vars, os.LookupEnv)
}

// LoadFrom is Load with an injectable environment, which is what makes it
// testable without touching the real process environment.
func LoadFrom(vars []Var, lookup func(string) (string, bool)) (*Config, error) {
	values := make(map[string]string, len(vars))
	var missing, invalid []string

	for _, v := range vars {
		raw, ok := lookup(v.Name)
		value := strings.TrimSpace(raw)

		switch {
		case !ok || value == "":
			if v.Required {
				missing = append(missing, v.Name)
				continue
			}
			value = v.Default
		}

		if v.Validate != nil {
			if err := v.Validate(value); err != nil {
				invalid = append(invalid, fmt.Sprintf("%s: %v", v.Name, err))
				continue
			}
		}
		values[v.Name] = value
	}

	// Report everything that is wrong at once: fixing one variable per restart
	// is a bad way to spend an afternoon.
	if len(missing) > 0 || len(invalid) > 0 {
		var problems []string
		if len(missing) > 0 {
			problems = append(problems, "missing: "+strings.Join(missing, ", "))
		}
		if len(invalid) > 0 {
			problems = append(problems, "invalid: "+strings.Join(invalid, ", "))
		}
		return nil, fmt.Errorf("configuration error — %s", strings.Join(problems, "; "))
	}

	return &Config{values: values}, nil
}

// Get returns the value as a string ("" when the variable was optional and empty).
func (c *Config) Get(name string) string { return c.values[name] }

// Int returns the value as an int. The variable should have been declared with
// IntVar so a bad number fails at boot instead of here.
func (c *Config) Int(name string) int {
	n, err := strconv.Atoi(c.values[name])
	if err != nil {
		panic(fmt.Sprintf("config: %s is not a valid integer (%q) — declare it with IntVar", name, c.values[name]))
	}
	return n
}

// Bool parses a boolean flag. Only the values accepted by BoolVar are valid, so
// anything else panics: boot validation should have caught it already, and a
// silent default here is how a config typo becomes a production incident.
func (c *Config) Bool(name string) bool {
	b, err := parseBool(c.values[name])
	if err != nil {
		panic(fmt.Sprintf("config: %s: %v — declare it with BoolVar", name, err))
	}
	return b
}

// parseBool is the single source of truth for boolean parsing: BoolVar validates
// with it and Bool reads with it, so the two can never disagree.
func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("must be a boolean like true/false (got %q)", v)
	}
}

// Duration parses a Go duration string ("30s", "10m").
func (c *Config) Duration(name string) time.Duration {
	d, err := time.ParseDuration(c.values[name])
	if err != nil {
		panic(fmt.Sprintf("config: %s is not a valid duration (%q) — declare it with DurationVar", name, c.values[name]))
	}
	return d
}

// IsProd reports whether ENV is "production". Declare it with EnvVar.
func (c *Config) IsProd() bool {
	return strings.EqualFold(c.Get("ENV"), "production")
}

// Has reports whether the variable was declared (useful for optional extras).
func (c *Config) Has(name string) bool {
	_, ok := c.values[name]
	return ok
}

// --- Declarations for the common cases -------------------------------------
//
// These keep app code short and put the validation rules in one place.

// EnvVar declares ENV (development | production) with a default of development.
func EnvVar() Var {
	return Var{
		Name:    "ENV",
		Default: "development",
		Validate: func(v string) error {
			switch strings.ToLower(v) {
			case "development", "production", "test":
				return nil
			default:
				return fmt.Errorf("must be development, production or test (got %q)", v)
			}
		},
	}
}

// IntVar declares an integer variable with bounds. A value outside the range
// fails at boot, which is the point: the limit is a property of the system, not
// of the value that happened to arrive.
func IntVar(name string, def string, min, max int) Var {
	return Var{
		Name:    name,
		Default: def,
		Validate: func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("must be an integer (got %q)", v)
			}
			if n < min || n > max {
				return fmt.Errorf("must be between %d and %d (got %d)", min, max, n)
			}
			return nil
		},
	}
}

// RequiredVar declares a variable that must be present and non-empty.
func RequiredVar(name string) Var {
	return Var{Name: name, Required: true}
}

// MinLenVar declares a variable with a minimum length (secrets, keys).
func MinLenVar(name string, min int) Var {
	return Var{
		Name:     name,
		Required: true,
		Validate: func(v string) error {
			if len(v) < min {
				return fmt.Errorf("must be at least %d characters", min)
			}
			return nil
		},
	}
}

// BoolVar declares a boolean flag with a default ("true"/"false").
func BoolVar(name, def string) Var {
	return Var{
		Name:    name,
		Default: def,
		Validate: func(v string) error {
			_, err := parseBool(v)
			return err
		},
	}
}

// DurationVar declares a Go duration string ("600s", "10m").
func DurationVar(name, def string) Var {
	return Var{
		Name:    name,
		Default: def,
		Validate: func(v string) error {
			if _, err := time.ParseDuration(v); err != nil {
				return fmt.Errorf("must be a duration like 600s or 10m (got %q)", v)
			}
			return nil
		},
	}
}
