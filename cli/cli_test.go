package cli

import (
	"context"
	"errors"
	"testing"
)

func TestParseUnderstandsTheStandardFlags(t *testing.T) {
	o, err := Parse("syncer", "v0.1.0", []string{"-once", "-dry-run"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !o.Once || !o.DryRun {
		t.Errorf("Once=%v DryRun=%v, want both true", o.Once, o.DryRun)
	}
}

func TestParseDefaultsToTheLongRunningMode(t *testing.T) {
	o, err := Parse("syncer", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if o.Once || o.DryRun {
		t.Errorf("with no flags both should be false (run as a daemon), got Once=%v DryRun=%v", o.Once, o.DryRun)
	}
}

func TestParseKeepsPositionalArgs(t *testing.T) {
	o, err := Parse("syncer", "v0.1.0", []string{"-once", "extra"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(o.Args) != 1 || o.Args[0] != "extra" {
		t.Errorf("Args = %v, want [extra]", o.Args)
	}
}

func TestParseRejectsUnknownFlags(t *testing.T) {
	if _, err := Parse("syncer", "v0.1.0", []string{"-turbo"}); err == nil {
		t.Error("an unknown flag must be an error, not a silent no-op")
	}
}

func TestRunReturnsZeroOnSuccess(t *testing.T) {
	if code := Run("test", func(context.Context) error { return nil }); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRunReturnsZeroOnCancellation(t *testing.T) {
	// Stopping because the context was cancelled is how a good daemon stops.
	if code := Run("test", func(ctx context.Context) error { return context.Canceled }); code != 0 {
		t.Errorf("exit code = %d, want 0 for a clean cancellation", code)
	}
}

func TestRunReturnsOneOnFailure(t *testing.T) {
	if code := Run("test", func(context.Context) error { return errors.New("boom") }); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
