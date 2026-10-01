// Package jobs runs background work with BOUNDED concurrency.
//
// House rule: never a bare `go func()`. Everything asynchronous goes through this
// pool, so the process cannot run out of resources because someone forgot a
// limit. A queue that can grow without bound is not a queue, it is a slow leak.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job is a unit of background work. It must respect the context: when the
// process is shutting down the context is cancelled and the job returns.
type Job interface {
	Name() string
	Run(ctx context.Context) error
}

// JobFunc adapts a function to the Job interface.
type JobFunc struct {
	JobName string
	Fn      func(ctx context.Context) error
}

func (j JobFunc) Name() string                  { return j.JobName }
func (j JobFunc) Run(ctx context.Context) error { return j.Fn(ctx) }

// Hook is called after every job finishes (or panics). It is how metrics and
// alerting get attached without the pool knowing about either.
type Hook func(name string, took time.Duration, err error)

// Options configures a Pool. The zero value is usable: 1 worker, 1024 slots.
type Options struct {
	// Size is how many jobs run in parallel.
	Size int
	// QueueSize is how many jobs may wait. When it is full, Enqueue returns
	// ErrQueueFull instead of blocking the caller (a web request that enqueues
	// work must not hang because the queue is deep).
	QueueSize int
	// Logger defaults to slog.Default().
	Logger *slog.Logger
	// OnDone runs after each job, in the worker goroutine.
	OnDone Hook
}

// Pool executes jobs with a fixed number of workers.
type Pool struct {
	size   int
	logger *slog.Logger
	queue  chan Job
	onDone Hook
	once   sync.Once
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	stopped bool
}

// New builds a pool. Prefer NewWithOptions when you need to tune it.
func New(size int, logger *slog.Logger) *Pool {
	return NewWithOptions(Options{Size: size, Logger: logger})
}

// NewWithOptions builds a pool from explicit options.
func NewWithOptions(o Options) *Pool {
	if o.Size < 1 {
		o.Size = 1
	}
	if o.QueueSize < 1 {
		o.QueueSize = 1024
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Pool{
		size:   o.Size,
		logger: o.Logger,
		queue:  make(chan Job, o.QueueSize),
		onDone: o.OnDone,
	}
}

// Start launches the workers. Call it once; a second call is a no-op.
func (p *Pool) Start(ctx context.Context) {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()

	for i := range p.size {
		p.wg.Add(1)
		go func(worker int) {
			defer p.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-p.queue:
					if !ok {
						return
					}
					p.run(ctx, worker, job)
				}
			}
		}(i)
	}
	p.logger.Info("job pool started", "workers", p.size, "queue", cap(p.queue))
}

func (p *Pool) run(ctx context.Context, worker int, job Job) {
	started := time.Now()
	var err error

	defer func() {
		if r := recover(); r != nil {
			// A panicking job must not take the process down with it.
			p.logger.Error("panic in job", "job", job.Name(), "panic", r)
			if p.onDone != nil {
				p.onDone(job.Name(), time.Since(started), err)
			}
		}
	}()

	p.logger.Debug("job started", "job", job.Name(), "worker", worker)
	err = job.Run(ctx)
	took := time.Since(started)

	switch {
	case err != nil:
		p.logger.Error("job failed", "job", job.Name(), "worker", worker, "took", took, "error", err)
	default:
		p.logger.Debug("job finished", "job", job.Name(), "worker", worker, "took", took)
	}
	if p.onDone != nil {
		p.onDone(job.Name(), took, err)
	}
}

// Enqueue adds a job without blocking: it returns ErrQueueFull when the queue is
// saturated and ErrStopped after Stop, so the caller decides what to do.
func (p *Pool) Enqueue(job Job) error {
	p.mu.RLock()
	stopped := p.stopped
	p.mu.RUnlock()
	if stopped {
		return ErrStopped
	}

	select {
	case p.queue <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Stop stops accepting jobs and waits for the running ones to finish. It is
// idempotent, so a deferred Stop plus an explicit Stop is safe.
func (p *Pool) Stop() {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()

	p.once.Do(func() { close(p.queue) })
	p.wg.Wait()
	p.logger.Info("job pool stopped")
}

// Size reports the worker count (mostly for tests and startup logs).
func (p *Pool) Size() int { return p.size }
