package config

import (
	"os"
	"path/filepath"
	"testing"
)

// unset removes a variable for the duration of the test and restores it after.
//
// Note: t.Setenv(k, "") is NOT the same thing. godotenv (like dotenv in every
// language) refuses to override a variable that is already present — even when it
// is present but empty — so blanking it would make the .env file invisible and the
// test would pass for the wrong reason.
func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, existed := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(k, old)
				return
			}
			_ = os.Unsetenv(k)
		})
	}
}

func TestLoadReadsADotEnvFile(t *testing.T) {
	// Load() is the only function that touches the real environment, so it gets a
	// test that proves the .env convenience actually works (the rest of the
	// package is tested through LoadFrom).
	dir := t.TempDir()
	content := "DATABASE_URL=user:pw@tcp(127.0.0.1:3306)/app\nPORT=9090\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
	unset(t, "DATABASE_URL", "PORT")
	t.Chdir(dir)

	cfg, err := Load([]Var{RequiredVar("DATABASE_URL"), {Name: "PORT", Default: "8080"}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Get("DATABASE_URL"); got != "user:pw@tcp(127.0.0.1:3306)/app" {
		t.Errorf("DATABASE_URL = %q, want the value from .env", got)
	}
	if got := cfg.Get("PORT"); got != "9090" {
		t.Errorf("PORT = %q, want 9090", got)
	}
}

func TestTheRealEnvironmentWinsOverDotEnv(t *testing.T) {
	// The precedence that keeps production safe: what the supervisor injects
	// (systemd EnvironmentFile, Kamal secrets) always beats a stray .env left in
	// the working directory.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=1111\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
	t.Setenv("PORT", "2222")
	t.Chdir(dir)

	cfg, err := Load([]Var{{Name: "PORT", Default: "8080"}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Get("PORT"); got != "2222" {
		t.Errorf("PORT = %q, want the environment value 2222", got)
	}
}

func TestLoadFailsWhenRequiredVariablesAreMissing(t *testing.T) {
	unset(t, "DATABASE_URL")
	t.Chdir(t.TempDir()) // no .env here

	if _, err := Load([]Var{RequiredVar("DATABASE_URL")}); err == nil {
		t.Error("Load must fail when a required variable is missing")
	}
}
