package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// defaultKitVersion is what a scaffolded project requires when the CLI itself has
// no version (running from a checkout). It is bumped with the toolkit.
const defaultKitVersion = "v0.3.0"

// templates holds the project skeleton. Embedding it keeps the CLI a single file:
// `go run github.com/enrand22/canguro-rails/cmd/canguro@latest new ...` works
// without cloning anything.
//
// The `all:` prefix matters: without it, embed skips the files starting with a dot
// (.gitignore, .env.example, .github/…) and the generated project would be missing
// exactly the files nobody notices until the first commit.
//
//go:embed all:templates
var templates embed.FS

type scaffoldOptions struct {
	Dir        string // target directory
	Module     string // Go module path
	Title      string // human title, used in the README and templates
	KitVersion string // version of canguro-rails the new project depends on
	Tidy       bool   // run `go mod tidy` at the end
}

// scaffold renders the template into opts.Dir. It refuses to touch a directory
// that already has files: silently merging into someone's project is how a tool
// destroys work.
func scaffold(opts scaffoldOptions) error {
	target, err := filepath.Abs(opts.Dir)
	if err != nil {
		return err
	}
	if err := ensureEmpty(target); err != nil {
		return err
	}

	data := templateData{
		Module:     opts.Module,
		Title:      opts.Title,
		App:        lastSegment(opts.Module),
		KitVersion: opts.KitVersion,
	}

	written, err := renderTree(target, data)
	if err != nil {
		return err
	}

	fmt.Printf("created %s (%d files)\n", opts.Dir, written)
	for _, f := range []string{"README.md", "Makefile", "cmd/" + data.App + "/main.go"} {
		fmt.Printf("  %s\n", f)
	}
	fmt.Println()
	fmt.Println("next steps:")
	fmt.Printf("  cd %s\n", opts.Dir)
	if opts.Tidy {
		fmt.Println("  (running `go mod tidy` so the project is ready to build)")
	}
	fmt.Println("  make db-up && make migrate")
	fmt.Println("  make test && make run")

	if !opts.Tidy {
		return nil
	}
	return tidy(target)
}

type templateData struct {
	Module     string // github.com/enrand22/rndc_go
	App        string // rndc_go (last segment of the module)
	Title      string // RNDC
	KitVersion string // v0.3.0
}

func lastSegment(module string) string {
	module = strings.TrimSuffix(module, "/")
	if idx := strings.LastIndex(module, "/"); idx >= 0 {
		return module[idx+1:]
	}
	return module
}

// ensureEmpty fails when the target exists and is not empty, and creates it when
// it does not exist.
func ensureEmpty(target string) error {
	info, err := os.Stat(target)
	switch {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s already exists and is not a directory", target)
	case err == nil:
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("%s is not empty: refusing to write over an existing project", target)
		}
		return nil
	case os.IsNotExist(err):
		return os.MkdirAll(target, 0o755)
	default:
		return err
	}
}

// renderTree walks the embedded templates and writes them into target.
//
// Two rules, and they are deliberate:
//
//   - The PATH is templated, so a directory can be named after the app
//     (cmd/{{.App}}/).
//   - Only files ending in .tmpl are RENDERED. Everything else is copied byte for
//     byte, which is what lets the template carry files full of `{{ }}` that are
//     not ours (GitHub Actions) without escaping gymnastics.
func renderTree(target string, data templateData) (int, error) {
	count := 0
	root := "templates/app"

	err := fs.WalkDir(templates, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		renderedPath, err := render(rel, data)
		if err != nil {
			return fmt.Errorf("rendering the path %q: %w", rel, err)
		}
		outPath := filepath.Join(target, renderedPath)

		if entry.IsDir() {
			return os.MkdirAll(outPath, 0o755)
		}

		content, err := templates.ReadFile(path)
		if err != nil {
			return err
		}

		body := string(content)
		if strings.HasSuffix(rel, ".tmpl") {
			outPath = strings.TrimSuffix(outPath, ".tmpl")
			if body, err = render(body, data); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		// Executable files keep their mode after generation.
		mode := os.FileMode(0o644)
		if strings.HasSuffix(outPath, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(outPath, []byte(body), mode); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func render(content string, data templateData) (string, error) {
	// Placeholders use [[ ]] instead of {{ }} on purpose: generated files are Go
	// code, and Go is full of `{{ }}` composite literals ([]Item{{ID: 1}}). With the
	// default delimiters every template author would have to remember to escape
	// them; with these, nobody ever has to think about it.
	tpl, err := template.New("file").Delims("[[", "]]").Option("missingkey=error").Parse(content)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// tidy runs `go mod tidy` in the new project so it can be built immediately.
// The generated go.mod pins the toolkit; tidy resolves the rest.
func tidy(dir string) error {
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed (the project was created; fix go.mod and run it yourself): %w", err)
	}
	fmt.Println("go mod tidy: ok")
	return nil
}
