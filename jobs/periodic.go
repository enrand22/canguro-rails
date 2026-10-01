package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Periodic runs a function on an interval, and NEVER overlaps it with itself.
//
// This is the difference between a scheduler and a landmine. If a run takes
// longer than the interval, a naive ticker starts a second copy, then a third,
// and the queue of runs grows until the database (or the client's platform)
// falls over. It has happened: a service that re-enqueued itself at the START of
// its own run accumulated millions of jobs and took a production platform down.
//
// So: while a run is in flight, ticks are skipped and counted.
type Periodic struct {
	name   string
	every  time.Duration
	fn     func(ctx context.Context) error
	logger *slog.Logger
	onDone Hook

	ticker  *time.Ticker
	done    chan struct{}
	started sync.Once
	stopped sync.Once

	running atomic.Bool
	skipped atomic.Int64
	runs    atomic.Int64

	wg sync.WaitGroup
}

// PeriodicOptions configures a Periodic.
type PeriodicOptions struct {
	// Every is the interval between runs (must be > 0).
	Every time.Duration
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// OnDone receives the duration and error of every run.
	OnDone Hook
	// RunImmediately makes the first run happen right away instead of after
	// Every. Useful for daemons that must not wait 10 minutes to do anything.
	RunImmediately bool
}

// NewPeriodic builds a periodic task. Call Start to begin scheduling.
func NewPeriodic(name string, fn func(ctx context.Context) error, o PeriodicOptions) *Periodic {
	if o.Every <= 0 {
		o.Every = time.Minute
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Periodic{
		name:   name,
		every:  o.Every,
		fn:     fn,
		logger: o.Logger,
		onDone: o.OnDone,
		done:   make(chan struct{}),
	}
}

// Start begins scheduling in its own goroutine and returns immediately.
func (p *Periodic) Start(ctx context.Context) {
	p.started.Do(func() {
		p.ticker = time.NewTicker(p.every)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.logger.Info("periodic task started", "task", p.name, "every", p.every)
			for {
				select {
				case <-ctx.Done():
					p.logger.Info("periodic task stopping (context done)", "task", p.name)
					return
				case <-p.done:
					p.logger.Info("periodic task stopped", "task", p.name)
					return
				case <-p.ticker.C:
					p.RunNow(ctx)
				}
			}
		}()
	})
}

// RunNow executes the function once, unless a previous run is still going (in
// which case the tick is skipped and logged). It is also what `--once` calls.
func (p *Periodic) RunNow(ctx context.Context) {
	if !p.running.CompareAndSwap(false, true) {
		n := p.skipped.Add(1)
		p.logger.Warn("skipping tick: the previous run is still in flight",
			"task", p.name, "skipped_total", n, "every", p.every)
		return
	}
	defer p.running.Store(false)

	started := time.Now()
	err := p.fn(ctx)
	took := time.Since(started)
	p.runs.Add(1)

	if err != nil {
		p.logger.Error("periodic run failed", "task", p.name, "took", took, "error", err)
	} else {
		p.logger.Info("periodic run finished", "task", p.name, "took", took)
	}
	if p.onDone != nil {
		p.onDone(p.name, took, err)
	}
}

// Stop ends the schedule and waits for the run in flight, so a shutdown does not
// kill a cycle halfway through.
func (p *Periodic) Stop() {
	p.stopped.Do(func() {
		close(p.done)
		if p.ticker != nil {
			p.ticker.Stop()
		}
	})
	p.wg.Wait()
}

// Stats reports how many runs happened and how many ticks were skipped because
// the previous run was still going. A skip count that keeps growing is a signal
// that the interval is too short or the job too slow.
func (p *Periodic) Stats() (runs, skipped int64) {
	return p.runs.Load(), p.skipped.Load()
}

// Guard checks the interval against an expected run duration and returns a
// warning-worthy error when the schedule is too tight. Encode the lesson in a
// function so nobody has to remember it.
func Guard(every, expectedRun time.Duration) error {
	if expectedRun >= every {
		return errors.New("scheduling error: the run can take longer than the interval, " +
			"so ticks would be skipped every time (raise the interval or make the run faster)")
	}
	return nil
}
