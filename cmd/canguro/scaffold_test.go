package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testOptions(t *testing.T) scaffoldOptions {
	t.Helper()
	return scaffoldOptions{
		Dir:        filepath.Join(t.TempDir(), "demo_app"),
		Module:     "github.com/enrand22/demo_app",
		Title:      "Demo",
		KitVersion: "v0.3.0",
		Tidy:       false,
	}
}

func TestScaffoldWritesTheWholeSkeleton(t *testing.T) {
	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	// A representative file of every part of the skeleton. If one of these is
	// missing, the project does not build or does not follow the conventions.
	want := []string{
		"go.mod",
		"Makefile",
		"Dockerfile",
		".gitignore", // starts with a dot: needs the `all:` embed prefix
		".env.example",
		".github/workflows/ci.yml",
		"README.md",
		"CONVENTIONS.md",
		"config/deploy.yml",
		"migrations/00001_init.sql",
		"cmd/demo_app/main.go", // the directory is named after the app
		"internal/config/config.go",
		"internal/models/store.go",
		"internal/services/item.go",
		"internal/controllers/home.go",
		"internal/views/layout.templ",
		"internal/views/home.templ",
		"internal/routes/routes.go",
		"internal/health/health.go",
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(opts.Dir, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
}

func TestScaffoldLeavesNoPlaceholders(t *testing.T) {
	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	err := filepath.WalkDir(opts.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "[[.") {
			t.Errorf("%s still has an unrendered placeholder", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the scaffold: %v", err)
	}
}

func TestScaffoldSubstitutesModuleTitleAndApp(t *testing.T) {
	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	gomod, err := os.ReadFile(filepath.Join(opts.Dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "module github.com/enrand22/demo_app") {
		t.Errorf("go.mod does not carry the module path:\n%s", gomod)
	}
	if !strings.Contains(string(gomod), "github.com/enrand22/canguro-rails v0.3.0") {
		t.Errorf("go.mod does not pin the toolkit:\n%s", gomod)
	}

	readme, err := os.ReadFile(filepath.Join(opts.Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "# Demo") {
		t.Errorf("the README does not use the title")
	}

	mainGo, err := os.ReadFile(filepath.Join(opts.Dir, "cmd", "demo_app", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainGo), `"github.com/enrand22/demo_app/internal/routes"`) {
		t.Errorf("the binary does not import the module's own packages:\n%s", mainGo)
	}
}

func TestDoubleBracesInTemplatesSurviveRendering(t *testing.T) {
	// The trap this guards: generated files are Go code, and Go is full of `{{ }}`
	// composite literals. Rendering them with the default delimiters breaks the
	// template ("function ID not defined"), which is why placeholders use [[ ]].
	// Here is the proof that a composite literal comes out intact.
	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	testFile, err := os.ReadFile(filepath.Join(opts.Dir, "internal", "services", "item_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(testFile), "[]models.Item{{ID: 1") {
		t.Errorf("the Go composite literal did not survive:\n%s", testFile)
	}
}

func TestFilesWithoutTheTmplSuffixAreCopiedVerbatim(t *testing.T) {
	// Files that need no substitution (.gitignore, the migration, CONVENTIONS.md)
	// are copied byte for byte: what is in the template is what lands in the project.
	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join(opts.Dir, "migrations", "00001_init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migration), "-- +goose Up") {
		t.Errorf("the migration should be copied as-is:\n%s", migration)
	}

	// And the CI workflow IS a template (it carries the app name), so it must come
	// out with the name substituted while keeping whatever braces GitHub needs.
	ci, err := os.ReadFile(filepath.Join(opts.Dir, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ci), "demo_app") {
		t.Error("the CI workflow should be adapted to the app name (it is a .tmpl)")
	}
	if strings.Contains(string(ci), "[[.") {
		t.Error("the CI workflow still has an unrendered placeholder")
	}
}

func TestScaffoldRefusesANonEmptyDirectory(t *testing.T) {
	opts := testOptions(t)
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.Dir, "important.txt"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := scaffold(opts)
	if err == nil {
		t.Fatal("scaffolding over an existing project must fail")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("the error should explain why: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(opts.Dir, "important.txt")); statErr != nil {
		t.Error("the existing files must be untouched")
	}
}

// TestEveryTemplateFileUsesTheTmplSuffix is the mirror of the bug that cost the most
// time here: the `cmd/{{.App}}` DIRECTORY was templated but the placeholders stopped
// being rendered when their delimiters changed, and the CLI quietly wrote a literal
// `{{.App}}` folder. A file carrying placeholders without the .tmpl suffix would ship
// them raw, so this test makes that impossible to miss.
func TestEveryTemplateFileUsesTheTmplSuffix(t *testing.T) {
	err := fs.WalkDir(templates, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := templates.ReadFile(path)
		if err != nil {
			return err
		}
		hasPlaceholders := strings.Contains(string(content), "[[.") ||
			strings.Contains(path, "[[.")
		if hasPlaceholders && !strings.HasSuffix(path, ".tmpl") {
			t.Errorf("%s has placeholders but not the .tmpl suffix: they would be written literally", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the templates: %v", err)
	}
}

// TestScaffoldedProjectBuilds is the test that keeps the template honest: it
// generates a project and compiles it. It needs the Go toolchain and the toolkit
// resolvable, so it runs only when CANGURO_KIT_PATH points at a toolkit checkout
// (CI sets it to the repository itself, so the template is verified on every push).
func TestScaffoldedProjectBuilds(t *testing.T) {
	kitPath := os.Getenv("CANGURO_KIT_PATH")
	if kitPath == "" {
		t.Skip("set CANGURO_KIT_PATH to a canguro-rails checkout to compile the template")
	}

	opts := testOptions(t)
	if err := scaffold(opts); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	// Point the new project at the checkout instead of at a published version.
	if err := run(t, opts.Dir, "go", "mod", "edit",
		"-replace=github.com/enrand22/canguro-rails="+kitPath); err != nil {
		t.Fatalf("go mod edit: %v", err)
	}

	// templ generates the *_templ.go files: without them the views package does not
	// exist and nothing compiles. The project's Makefile does this too.
	templBin, err := exec.LookPath("templ")
	if err != nil {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(os.Getenv("HOME"), "go")
		}
		candidate := filepath.Join(gopath, "bin", "templ")
		if _, statErr := os.Stat(candidate); statErr != nil {
			t.Skip("templ is not installed: go install github.com/a-h/templ/cmd/templ@latest")
		}
		templBin = candidate
	}
	if err := run(t, opts.Dir, templBin, "generate"); err != nil {
		t.Fatalf("templ generate: %v", err)
	}

	if err := run(t, opts.Dir, "go", "build", "./..."); err != nil {
		t.Fatalf("go build: %v", err)
	}
	if err := run(t, opts.Dir, "go", "test", "./...", "-count=1"); err != nil {
		t.Fatalf("go test: %v", err)
	}
}

// run executes a command in dir with the environment the generated project needs
// to resolve it offline: the toolkit's dependencies are always in the module cache
// of anyone who can build the toolkit itself, and `-mod=mod` fills in go.mod from
// there. Without this the test would hang on `go mod tidy` waiting for the network
// (or for the module cache lock) instead of proving that the template compiles.
func run(t *testing.T, dir, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(scrubbedEnv(),
		"GOFLAGS=-mod=mod",
		"GOPROXY=off",
		"GOSUMDB=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("%s %s:\n%s", name, strings.Join(args, " "), out)
	}
	return err
}

// scrubbedEnv drops the parent's database settings. The generated project must
// decide its own: inheriting TEST_DATABASE_URL from the toolkit's test run would
// point the new app at the toolkit's database, and its tests would fail looking
// for tables that belong to another project.
func scrubbedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TEST_DATABASE_URL=") || strings.HasPrefix(kv, "REQUIRE_DB=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}
