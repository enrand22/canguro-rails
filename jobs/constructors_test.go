package jobs

import (
	"context"
	"testing"
	"time"
)

func TestNewUsesShlogDefaultsAndExposesItsSize(t *testing.T) {
	p := New(3, quiet())
	if p.Size() != 3 {
		t.Errorf("Size() = %d, want 3", p.Size())
	}

	// A zero worker count is clamped instead of producing a pool that never runs.
	if got := New(0, nil).Size(); got != 1 {
		t.Errorf("New(0, nil).Size() = %d, want 1", got)
	}
}

func TestPoolRunsWithTheDefaultQueueSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := New(2, quiet())
	p.Start(ctx)

	done := make(chan struct{})
	if err := p.Enqueue(JobFunc{JobName: "uno", Fn: func(context.Context) error {
		close(done)
		return nil
	}}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the job never ran")
	}
	p.Stop()
}

func TestStartTwiceIsANoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := New(1, quiet())
	p.Start(ctx)
	p.Start(ctx) // must not spawn a second set of workers

	p.Stop()
}
