package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// quiet returns a logger that writes nowhere, so tests don't spam the output.
func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPoolRunsEveryEnqueuedJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := NewWithOptions(Options{Size: 2, Logger: quiet()})
	pool.Start(ctx)

	var done atomic.Int64
	total := 20
	for i := range total {
		err := pool.Enqueue(JobFunc{
			JobName: "probe",
			Fn: func(context.Context) error {
				done.Add(1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}
	pool.Stop()

	if got := done.Load(); got != int64(total) {
		t.Errorf("ran %d jobs, want %d — Stop must drain everything enqueued", got, total)
	}
}

func TestPoolReturnsQueueFullInsteadOfBlocking(t *testing.T) {
	block := make(chan struct{})
	pool := NewWithOptions(Options{
		Size:      1,
		QueueSize: 1,
		Logger:    quiet(),
	})

	// Not started on purpose: nothing consumes the queue, so the second enqueue
	// must fail instead of blocking the caller forever.
	if err := pool.Enqueue(JobFunc{JobName: "first", Fn: func(context.Context) error {
		<-block
		return nil
	}}); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	close(block)

	err := pool.Enqueue(JobFunc{JobName: "second", Fn: func(context.Context) error { return nil }})
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("second Enqueue error = %v, want ErrQueueFull", err)
	}
}

func TestPoolRejectsWorkAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := NewWithOptions(Options{Size: 1, Logger: quiet()})
	pool.Start(ctx)
	pool.Stop()

	if err := pool.Enqueue(JobFunc{JobName: "late", Fn: func(context.Context) error { return nil }}); !errors.Is(err, ErrStopped) {
		t.Errorf("Enqueue after Stop = %v, want ErrStopped", err)
	}
}

func TestPoolSurvivesAPanickingJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool := NewWithOptions(Options{Size: 1, Logger: quiet()})
	pool.Start(ctx)

	if err := pool.Enqueue(JobFunc{JobName: "boom", Fn: func(context.Context) error {
		panic("algo se rompió")
	}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// A following job must still run: a panic cannot poison the worker.
	done := make(chan struct{})
	if err := pool.Enqueue(JobFunc{JobName: "after", Fn: func(context.Context) error {
		close(done)
		return nil
	}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker died with the panicking job")
	}
	pool.Stop()
}

func TestPoolReportsErrorsThroughTheHook(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs := make(chan error, 1)
	pool := NewWithOptions(Options{
		Size:   1,
		Logger: quiet(),
		OnDone: func(name string, took time.Duration, err error) {
			if name == "fail" {
				errs <- err
			}
		},
	})
	pool.Start(ctx)

	sentinel := errors.New("se cayó")
	_ = pool.Enqueue(JobFunc{JobName: "fail", Fn: func(context.Context) error { return sentinel }})
	pool.Stop()

	select {
	case err := <-errs:
		if !errors.Is(err, sentinel) {
			t.Errorf("hook received %v, want the job error", err)
		}
	default:
		t.Error("the OnDone hook was never called")
	}
}

func TestPeriodicNeverOverlapsItself(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var inFlight, maxInFlight atomic.Int64
	var runs atomic.Int64

	p := NewPeriodic("slow", func(context.Context) error {
		now := inFlight.Add(1)
		for {
			max := maxInFlight.Load()
			if now <= max || maxInFlight.CompareAndSwap(max, now) {
				break
			}
		}
		runs.Add(1)
		time.Sleep(40 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	}, PeriodicOptions{Every: 5 * time.Millisecond, Logger: quiet()})

	p.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	p.Stop()

	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("max concurrent runs = %d, want 1 (periodic tasks must not overlap)", got)
	}
	if got := runs.Load(); got < 2 {
		t.Errorf("ran %d times, want at least 2 (the task should keep running)", got)
	}
	_, skipped := p.Stats()
	if skipped == 0 {
		t.Log("note: no ticks were skipped in this run (timing dependent)")
	}
}

func TestPeriodicRunNowIsSingleFlight(t *testing.T) {
	var runs atomic.Int64
	p := NewPeriodic("manual", func(context.Context) error {
		runs.Add(1)
		time.Sleep(30 * time.Millisecond)
		return nil
	}, PeriodicOptions{Every: time.Hour, Logger: quiet()})

	done := make(chan struct{})
	go func() {
		p.RunNow(context.Background()) // takes 30ms
		close(done)
	}()
	time.Sleep(5 * time.Millisecond)

	p.RunNow(context.Background()) // must be skipped, not queued
	<-done
	time.Sleep(50 * time.Millisecond)

	if got := runs.Load(); got != 1 {
		t.Errorf("ran %d times, want 1: a second RunNow while one is in flight must be skipped", got)
	}
	_, skipped := p.Stats()
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

func TestPeriodicStopWaitsForTheRunInFlight(t *testing.T) {
	finished := make(chan struct{})
	p := NewPeriodic("drain", func(context.Context) error {
		time.Sleep(60 * time.Millisecond)
		close(finished)
		return nil
	}, PeriodicOptions{Every: 10 * time.Millisecond, Logger: quiet()})

	p.Start(context.Background())
	time.Sleep(15 * time.Millisecond) // let a run start
	p.Stop()

	select {
	case <-finished:
	default:
		t.Error("Stop returned while a run was still in flight: a shutdown must drain")
	}
}

func TestGuardWarnsWhenTheIntervalIsTooTight(t *testing.T) {
	if err := Guard(600*time.Second, 535*time.Second); err != nil {
		t.Errorf("600s interval with a 535s run should be fine: %v", err)
	}
	if err := Guard(300*time.Second, 535*time.Second); err == nil {
		t.Error("a 300s interval with a 535s run must be rejected: ticks would always be skipped")
	}
}
