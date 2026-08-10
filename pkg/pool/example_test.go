// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package pool_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tickraft/tickraft/pkg/pool"
)

// ExampleNew shows how to create a pool, submit jobs as [pool.Lambda]
// values, and shut it down. A single worker is used so the output
// order is deterministic.
func ExampleNew() {
	p, err := pool.New(pool.WithWorkers(1), pool.WithQueueSize(4))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			fmt.Println("shutdown:", err)
		}
	}()

	for i := 1; i <= 3; i++ {
		if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
			fmt.Println("task", i)
			return nil
		})); err != nil {
			fmt.Println("submit lambda job:", err)
			return
		}
	}
	// Output:
	// task 1
	// task 2
	// task 3
}

// ExampleLambda shows the canonical way to run a closure as a pool
// job: wrap the func with [pool.Lambda] and pass it to [pool.Submit].
// No SubmitFunc helper exists; Lambda is the only adapter needed.
func ExampleLambda() {
	p, err := pool.New(pool.WithWorkers(1), pool.WithQueueSize(2))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() {
		// Shutdown error is intentionally ignored: Example output
		// must be deterministic and a graceful shutdown cannot fail.
		_ = p.Shutdown(context.Background())
	}()

	job := pool.Lambda(func(ctx context.Context) error {
		fmt.Println("lambda ran")
		return nil
	})
	if err := p.Submit(context.Background(), job); err != nil {
		fmt.Println("submit:", err)
		return
	}
	// Output:
	// lambda ran
}

// printJob is a custom [pool.Job] implementation used by
// ExampleJob_struct to demonstrate how a struct-based job can carry
// metadata that the error and panic handlers can inspect.
type printJob struct {
	msg string
}

func (j *printJob) Run(ctx context.Context) error {
	fmt.Println("running:", j.msg)
	switch j.msg {
	case "bad":
		return fmt.Errorf("failed: %s", j.msg)
	case "panic":
		panic("boom")
	}
	return nil
}

// ExampleJob_struct demonstrates a custom struct-based [pool.Job] with
// [pool.ErrorHandler] and [pool.PanicHandler] configured. The handlers
// receive the original Job instance so they can identify the task by
// type and inspect its fields.
func ExampleJob_struct() {
	p, err := pool.New(
		pool.WithWorkers(1),
		pool.WithQueueSize(4),
		pool.WithErrorHandler(func(job pool.Job, err error) {
			if pj, ok := job.(*printJob); ok {
				fmt.Println("error:", err, "(", pj.msg, ")")
				return
			}
			fmt.Println("error:", err)
		}),
		pool.WithPanicHandler(func(job pool.Job, r interface{}) {
			if pj, ok := job.(*printJob); ok {
				fmt.Println("panic:", r, "(", pj.msg, ")")
				return
			}
			fmt.Println("panic:", r)
		}),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			fmt.Println("shutdown:", err)
		}
	}()

	for _, msg := range []string{"hello", "bad", "panic", "world"} {
		if err := p.Submit(context.Background(), &printJob{msg: msg}); err != nil {
			fmt.Println("submit:", err)
			return
		}
	}
	// Output:
	// running: hello
	// running: bad
	// error: failed: bad ( bad )
	// running: panic
	// panic: boom ( panic )
	// running: world
}

// ExampleRejectionPolicy demonstrates combining the
// [pool.RejectionCallerRuns] policy with [pool.WithStallDetection].
// Once the queue is full, additional submits run synchronously in the
// caller's goroutine, providing backpressure without dropping work.
// The stall handler would fire if the queue stayed full for the
// configured threshold; here it never does because caller-runs keeps
// the queue drained.
func ExampleRejectionPolicy() {
	p, err := pool.New(
		pool.WithWorkers(1),
		pool.WithQueueSize(1),
		pool.WithRejectionPolicy(pool.RejectionCallerRuns),
		// Use a long threshold so the handler never fires during
		// this short example; in real code pick a value that
		// reflects your SLO for queue saturation.
		pool.WithStallDetection(10*time.Second, func(s pool.Stats) {
			fmt.Println("stall detected, pending:", s.Pending)
		}),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() {
		if err := p.Shutdown(context.Background()); err != nil {
			fmt.Println("shutdown:", err)
		}
	}()

	release := make(chan struct{})
	// Block the single worker and wait until it is actually running the
	// blocking job. Without this barrier the worker may or may not have
	// dequeued the blocking job yet, so whether a later submit is
	// enqueued or runs caller-runs becomes a goroutine-scheduling race
	// and the example output would be non-deterministic.
	started := make(chan struct{})
	if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		close(started)
		<-release
		return nil
	})); err != nil {
		fmt.Println("submit lambda job:", err)
		return
	}
	<-started
	// Fill the queue. The worker is now blocked inside the job above, so
	// this job stays queued and the queue remains full below.
	if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
		return nil
	})); err != nil {
		fmt.Println("submit lambda job:", err)
		return
	}

	// Subsequent submits run in the caller's goroutine.
	for i := 1; i <= 2; i++ {
		if err := p.Submit(context.Background(), pool.Lambda(func(ctx context.Context) error {
			fmt.Println("caller-runs", i)
			return nil
		})); err != nil {
			fmt.Println("submit lambda job:", err)
			return
		}
	}
	close(release)
	// Output:
	// caller-runs 1
	// caller-runs 2
}
