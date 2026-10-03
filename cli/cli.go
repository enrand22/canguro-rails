// Package cli is the small surface every daemon-shaped program in this house
// shares: parse the standard flags, handle SIGTERM/SIGINT, log a clean shutdown
// and return a process exit code.
//
// A daemon that ignores SIGTERM gets SIGKILLed mid-cycle and loses work; a daemon
// that swallows its own errors exits 0 and reports success to systemd. Neither is
// acceptable, so both are handled here instead of in every main().
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// Options are the flags every daemon understands.
type Options struct {
	// Once runs a single cycle and exits (used to switch a service over with a
	// manual run before enabling the timer).
	Once bool
	// DryRun does everything except the writes.
	DryRun bool
	// Version says the program was asked for its version. Parse does NOT print it
	// (it must stay callable from tests): the program prints it and returns, e.g.
	//
	//	if opts.Version {
	//		fmt.Println(version)
	//		return
	//	}
	Version bool
	// Args holds any remaining positional arguments.
	Args []string
}

// Parse reads the standard flags. name is used for -h output; version is printed
// only if the program asks for it (see Options.Version).
func Parse(name, version string, args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.BoolVar(&o.Once, "once", false, "run a single cycle and exit")
	fs.BoolVar(&o.DryRun, "dry-run", false, "read everything, write nothing")
	fs.BoolVar(&o.Version, "version", false, "print the version and exit")
	fs.Usage = func() {
		// La salida de un texto de ayuda no deja un error accionable.
		_, _ = fmt.Fprintf(fs.Output(), "%s %s — usage:\n", name, version)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.Args = fs.Args()
	return o, nil
}

// NotifyContext returns a context cancelled by SIGTERM or SIGINT. Every long
// loop should take this context and stop when it is done.
func NotifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// Run wires the whole lifecycle: build the context, call fn, log the outcome and
// return an exit code. A daemon whose fn returns nil exits 0; an error exits 1
// (systemd then sees a failed unit instead of a silent success).
func Run(name string, fn func(ctx context.Context) error) int {
	ctx, stop := NotifyContext()
	defer stop()

	slog.Info("starting", "program", name, "pid", os.Getpid())
	err := fn(ctx)

	switch {
	case err == nil:
		slog.Info("stopped cleanly", "program", name)
		return 0
	case errors.Is(err, context.Canceled):
		// Cancellation is how a well-behaved daemon stops: not a failure.
		slog.Info("stopped on signal", "program", name)
		return 0
	default:
		slog.Error("exited with error", "program", name, "error", err)
		return 1
	}
}
