package jobs

import "errors"

var (
	// ErrQueueFull is returned when the queue is saturated, so the caller can
	// log it, retry or reject the request instead of hanging.
	ErrQueueFull = errors.New("job queue is full")

	// ErrStopped is returned when the pool is no longer accepting work.
	ErrStopped = errors.New("job pool is stopped")
)
