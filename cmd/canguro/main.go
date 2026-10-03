// Command canguro is the toolkit's CLI. For now it does one thing well: create a
// new project with the house conventions already in place.
//
//	canguro new mi-app --module github.com/enrand22/mi_app
//
// The point is that a new service starts in minutes instead of days: the
// scaffolding is not copied by hand from the last project (which is how the last
// project's mistakes spread), it is generated from a template that the toolkit
// keeps in one place.
package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

const usage = `canguro — the canguro-rails toolkit CLI

Usage:
  canguro new <dir> --module <module path> [--title "Nombre"] [--kit <version>] [--no-tidy]

Commands:
  new       Create a new project with the house conventions
  version   Print the toolkit version

Examples:
  canguro new rndc_go --module github.com/enrand22/rndc_go --title "RNDC"
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "new":
		opts, err := parseNew(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		if err := scaffold(opts); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Println(Version())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

// Version reports the toolkit version. When the CLI was installed with
// `go install` the module version is in the build info; running from a checkout
// has no version, so we say so instead of inventing one.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}

// parseNew reads the flags of `canguro new`. Hand-rolled on purpose: three flags
// do not need a flag library, and the error messages here are the ones a human
// reads first.
func parseNew(args []string) (scaffoldOptions, error) {
	opts := scaffoldOptions{
		KitVersion: Version(),
		Title:      "",
		Tidy:       true,
	}
	if opts.KitVersion == "devel" {
		opts.KitVersion = defaultKitVersion
	}

	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opts.Dir = args[0]
		i = 1
	}

	for ; i < len(args); i++ {
		arg := args[i]
		value := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", arg)
			}
			i++
			return args[i], nil
		}

		switch arg {
		case "--module", "-m":
			v, err := value()
			if err != nil {
				return opts, err
			}
			opts.Module = v
		case "--title", "-t":
			v, err := value()
			if err != nil {
				return opts, err
			}
			opts.Title = v
		case "--kit":
			v, err := value()
			if err != nil {
				return opts, err
			}
			opts.KitVersion = v
		case "--no-tidy":
			opts.Tidy = false
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown flag %q", arg)
		}
	}

	if opts.Dir == "" {
		return opts, fmt.Errorf("missing the target directory: canguro new <dir> --module <path>")
	}
	if opts.Module == "" {
		return opts, fmt.Errorf("missing --module: the Go module path of the new project (e.g. github.com/enrand22/my_app)")
	}
	if opts.Title == "" {
		// A sane default: the last segment of the directory name. Trim the trailing
		// slashes BEFORE slicing, or `canguro new apps/giftcards/` produces the title
		// "giftcards/" and the README shows a stray slash.
		name := strings.TrimRight(opts.Dir, "/")
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" {
			name = "app"
		}
		opts.Title = name
	}
	return opts, nil
}
