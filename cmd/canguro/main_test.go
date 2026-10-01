package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNewReadsTheFlags(t *testing.T) {
	opts, err := parseNew([]string{"rndc_go", "--module", "github.com/enrand22/rndc_go", "--title", "RNDC", "--kit", "v0.9.9", "--no-tidy"})
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if opts.Dir != "rndc_go" {
		t.Errorf("Dir = %q", opts.Dir)
	}
	if opts.Module != "github.com/enrand22/rndc_go" {
		t.Errorf("Module = %q", opts.Module)
	}
	if opts.Title != "RNDC" {
		t.Errorf("Title = %q", opts.Title)
	}
	if opts.KitVersion != "v0.9.9" {
		t.Errorf("KitVersion = %q", opts.KitVersion)
	}
	if opts.Tidy {
		t.Error("--no-tidy must turn tidy off")
	}
}

func TestParseNewTidiesByDefault(t *testing.T) {
	opts, err := parseNew([]string{"app", "-m", "github.com/enrand22/app"})
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if !opts.Tidy {
		t.Error("tidy is the default: a new project should be ready to build")
	}
}

func TestParseNewDefaultsTheTitleToTheLastDirectorySegment(t *testing.T) {
	// Nobody should have to repeat the name in --title for it to look right.
	cases := map[string]string{
		"rndc_go":             "rndc_go",
		"apps/mi-app":         "mi-app",
		"apps/giftcards/":     "giftcards",
		"/tmp/proyectos/rndc": "rndc",
		"./relativo":          "relativo",
	}
	for dir, want := range cases {
		opts, err := parseNew([]string{dir, "--module", "github.com/enrand22/x"})
		if err != nil {
			t.Fatalf("parseNew(%q): %v", dir, err)
		}
		if opts.Title != want {
			t.Errorf("Title for %q = %q, want %q", dir, opts.Title, want)
		}
	}
}

func TestParseNewUsesAConcreteKitVersionWhenRunningFromACheckout(t *testing.T) {
	// `Version()` answers "devel" in a checkout, which is not a version anyone can
	// put in a go.mod: the CLI falls back to defaultKitVersion.
	opts, err := parseNew([]string{"app", "--module", "github.com/enrand22/app"})
	if err != nil {
		t.Fatalf("parseNew: %v", err)
	}
	if strings.HasPrefix(opts.KitVersion, "v") == false {
		t.Errorf("KitVersion = %q, want something like v0.3.0", opts.KitVersion)
	}
	if opts.KitVersion == "devel" {
		t.Error("a go.mod cannot require `devel`: the fallback is missing")
	}
}

func TestParseNewRejectsIncompleteOrUnknownInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // what the error should mention (the message is read by a human)
	}{
		{"sin directorio", []string{"--module", "github.com/enrand22/x"}, "target directory"},
		{"sin módulo", []string{"app"}, "--module"},
		{"flag desconocida", []string{"app", "--module", "x", "--wat"}, "unknown flag"},
		{"flag sin valor", []string{"app", "--module"}, "needs a value"},
		{"título sin valor", []string{"app", "--module", "x", "--title"}, "needs a value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNew(tc.args)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestVersionNeverReturnsAnEmptyString(t *testing.T) {
	if Version() == "" {
		t.Error("Version() must always answer something printable")
	}
}

func TestLastSegment(t *testing.T) {
	cases := map[string]string{
		"github.com/enrand22/rndc_go": "rndc_go",
		"rndc_go":                     "rndc_go",
		"github.com/enrand22/x/":      "x",
	}
	for module, want := range cases {
		if got := lastSegment(module); got != want {
			t.Errorf("lastSegment(%q) = %q, want %q", module, got, want)
		}
	}
}

func TestRenderUsesTheBracketDelimiters(t *testing.T) {
	data := templateData{Module: "github.com/enrand22/app", App: "app", Title: "App", KitVersion: "v0.3.0"}

	got, err := render("module [[.Module]]", data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "module github.com/enrand22/app" {
		t.Errorf("render = %q", got)
	}

	// And this is the point of the decision: braces are left alone.
	braces := "items := []Item{{ID: 1, Name: \"uno\"}}"
	got, err = render(braces, data)
	if err != nil {
		t.Fatalf("render (braces): %v", err)
	}
	if got != braces {
		t.Errorf("render mangled the Go code: %q", got)
	}
}

func TestRenderFailsOnAnUnknownPlaceholder(t *testing.T) {
	// missingkey=error: a typo in a template must break the build, not produce a
	// file with a hole in it that only fails much later.
	if _, err := render("[[.Typpo]]", templateData{}); err == nil {
		t.Error("an unknown placeholder must be an error")
	}
}

func TestScaffoldRefusesToOverwriteAFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "archivo.txt")
	if err := os.WriteFile(target, []byte("importante"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := scaffold(scaffoldOptions{Dir: target, Module: "github.com/enrand22/app", Title: "App", KitVersion: "v0.3.0"})
	if err == nil {
		t.Fatal("scaffolding over a file must fail")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %q", err)
	}
	if content, readErr := os.ReadFile(target); readErr != nil || string(content) != "importante" {
		t.Error("the existing file must be untouched")
	}
}
