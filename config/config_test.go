package config

import (
	"strings"
	"testing"
)

// setenv builds a lookup function from a map, so tests never touch the real
// process environment.
func setenv(kv map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := kv[name]
		return v, ok
	}
}

func TestLoadFromAppliesDefaults(t *testing.T) {
	cfg, err := LoadFrom([]Var{
		RequiredVar("DATABASE_URL"),
		IntVar("WORKERS", "4", 1, 8),
		EnvVar(),
	}, setenv(map[string]string{"DATABASE_URL": "user:pass@tcp(db:3306)/app"}))

	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.Int("WORKERS"); got != 4 {
		t.Errorf("WORKERS default = %d, want 4", got)
	}
	if got := cfg.Get("ENV"); got != "development" {
		t.Errorf("ENV default = %q, want development", got)
	}
	if cfg.IsProd() {
		t.Error("IsProd() = true with ENV=development")
	}
}

func TestLoadFromNamesEveryMissingVariable(t *testing.T) {
	_, err := LoadFrom([]Var{
		RequiredVar("DATABASE_URL"),
		RequiredVar("SESSION_SECRET"),
	}, setenv(nil))

	if err == nil {
		t.Fatal("expected an error when required variables are missing")
	}
	msg := err.Error()
	for _, name := range []string{"DATABASE_URL", "SESSION_SECRET"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error does not name %s: %s", name, msg)
		}
	}
}

func TestLoadFromRejectsOutOfRangeIntAndSaysTheRange(t *testing.T) {
	_, err := LoadFrom([]Var{
		IntVar("SYNC_THREADS", "4", 1, 8),
	}, setenv(map[string]string{"SYNC_THREADS": "99"}))

	if err == nil {
		t.Fatal("expected an error for SYNC_THREADS=99 (the pool cap is 8)")
	}
	if !strings.Contains(err.Error(), "between 1 and 8") {
		t.Errorf("error should state the valid range, got: %v", err)
	}
}

func TestLoadFromRejectsNonNumericInt(t *testing.T) {
	_, err := LoadFrom([]Var{IntVar("WORKERS", "4", 1, 8)},
		setenv(map[string]string{"WORKERS": "muchos"}))
	if err == nil {
		t.Fatal("expected an error for a non-numeric integer")
	}
}

func TestLoadFromRequiredBeatsDefault(t *testing.T) {
	_, err := LoadFrom([]Var{{Name: "TOKEN", Required: true, Default: "no-sirve"}}, setenv(nil))
	if err == nil {
		t.Fatal("a required variable must not fall back to its default")
	}
}

func TestLoadFromMinLen(t *testing.T) {
	if _, err := LoadFrom([]Var{MinLenVar("SESSION_SECRET", 32)},
		setenv(map[string]string{"SESSION_SECRET": "corta"})); err == nil {
		t.Fatal("expected an error for a short secret")
	}
	if _, err := LoadFrom([]Var{MinLenVar("SESSION_SECRET", 32)},
		setenv(map[string]string{"SESSION_SECRET": strings.Repeat("s", 32)})); err != nil {
		t.Fatalf("32 characters should be enough: %v", err)
	}
}

func TestBoolAndDuration(t *testing.T) {
	cfg, err := LoadFrom([]Var{
		BoolVar("DEBUG", "false"),
		BoolVar("FEATURE", "false"),
		DurationVar("SYNC_INTERVAL", "600s"),
	}, setenv(map[string]string{"FEATURE": "yes"}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Bool("DEBUG") {
		t.Error("DEBUG should default to false")
	}
	if !cfg.Bool("FEATURE") {
		t.Error("FEATURE=yes should be true")
	}
	if got := cfg.Duration("SYNC_INTERVAL").Seconds(); got != 600 {
		t.Errorf("SYNC_INTERVAL = %vs, want 600s", got)
	}
}

func TestBoolVarAndBoolAgree(t *testing.T) {
	// The trap this test exists for: BoolVar accepting a value that Bool then
	// misreads (or the other way around), which turns a typo into a silent default.
	for _, v := range []string{"true", "false", "1", "0", "yes", "no", "on", "off"} {
		cfg, err := LoadFrom([]Var{BoolVar("FLAG", "false")}, setenv(map[string]string{"FLAG": v}))
		if err != nil {
			t.Errorf("BoolVar rejected %q: %v", v, err)
			continue
		}
		want := v == "true" || v == "1" || v == "yes" || v == "on"
		if got := cfg.Bool("FLAG"); got != want {
			t.Errorf("Bool(%q) = %v, want %v", v, got, want)
		}
	}
	if _, err := LoadFrom([]Var{BoolVar("FLAG", "false")}, setenv(map[string]string{"FLAG": "si"})); err == nil {
		t.Error("BoolVar should reject an unknown boolean at boot")
	}
}

func TestDurationVarRejectsGarbage(t *testing.T) {
	_, err := LoadFrom([]Var{DurationVar("SYNC_INTERVAL", "600s")},
		setenv(map[string]string{"SYNC_INTERVAL": "diez minutos"}))
	if err == nil {
		t.Fatal("expected an error for a duration without units")
	}
}

func TestEnvVarRejectsUnknownEnvironment(t *testing.T) {
	_, err := LoadFrom([]Var{EnvVar()}, setenv(map[string]string{"ENV": "produccion"}))
	if err == nil {
		t.Fatal("expected an error for a typo in ENV")
	}
}

func TestIsProdAndHas(t *testing.T) {
	cfg, err := LoadFrom([]Var{EnvVar(), {Name: "OPTIONAL"}},
		setenv(map[string]string{"ENV": "production"}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.IsProd() {
		t.Error("IsProd() = false with ENV=production")
	}
	if !cfg.Has("OPTIONAL") {
		t.Error("Has(OPTIONAL) = false for a declared optional variable")
	}
	if cfg.Has("NOPE") {
		t.Error("Has(NOPE) = true for an undeclared variable")
	}
}
