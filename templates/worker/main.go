// Command worker is a starter background worker: a pool of goroutines
// consuming a job queue plus a periodic cron task. Swap the channel for your
// broker (Kafka, NATS, RabbitMQ, Redis streams, SQS); the logging stays the same.
//
//	LOG_FORMAT=console go run .          # Ctrl-C to drain and stop
//
// What it shows: one context per job carrying job_id, retry warnings with the
// attempt number, panics recovered with a stack, timings as duration_ms, and
// sampling so a hot "polling" message cannot flood the log pipeline.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/techpartners-asia/logger-go"
)

var version = "dev"

type Job struct {
	ID       string
	Kind     string
	Attempts int
}

var errTemporary = errors.New("upstream unavailable")

func main() {
	log := logger.MustNew(
		logger.WithService("orders-worker"),
		logger.WithVersion(version),
		logger.WithRedaction(),
		logger.WithStackTrace(logger.LevelError),
		logger.WithSampling(logger.Sampling{Initial: 5, Thereafter: 50, Tick: time.Second}),
		logger.WithConfig(logger.ConfigFromEnv()),
	)
	defer log.Close()
	logger.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	jobs := make(chan Job)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		produce(ctx, log.Named("producer"), jobs)
	}()

	const workers = 3
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consume(ctx, log.Named("consumer").With("worker", i), jobs)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		cron(ctx, log.Named("cron"), 5*time.Second, cleanupExpired)
	}()

	log.Info("worker started", "workers", workers)
	<-ctx.Done()
	log.Info("draining")
	wg.Wait()
	log.Info("worker stopped")
}

func produce(ctx context.Context, log *logger.Logger, jobs chan<- Job) {
	defer close(jobs)
	kinds := []string{"send_receipt", "sync_inventory", "charge_card"}
	for n := 1; ; n++ {
		job := Job{ID: fmt.Sprintf("job-%04d", n), Kind: kinds[rand.IntN(len(kinds))]}
		select {
		case <-ctx.Done():
			return
		case jobs <- job:
			log.Debug("job enqueued", "job_id", job.ID) // sampled: first 5 per second, then 1 in 50
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func consume(ctx context.Context, log *logger.Logger, jobs <-chan Job) {
	for job := range jobs {
		// Every log call made with jobCtx carries job_id and job_kind, including
		// calls deep inside handlers and DB or HTTP adapters.
		jobCtx := logger.ContextWith(ctx, "job_id", job.ID, "job_kind", job.Kind)
		runJob(jobCtx, log, job)
	}
}

func runJob(ctx context.Context, log *logger.Logger, job Job) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.ErrorContext(ctx, "job panicked", "panic", r) // stack added by WithStackTrace
		}
	}()

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := handle(ctx, job)
		if err == nil {
			log.InfoContext(ctx, "job done", "attempt", attempt, logger.Duration(time.Since(start)))
			return
		}
		if !errors.Is(err, errTemporary) || attempt == maxAttempts {
			log.ErrorContext(ctx, "job failed", err, "attempt", attempt, logger.Duration(time.Since(start)))
			return
		}
		log.WarnContext(ctx, "job retrying", err, "attempt", attempt)
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
	}
}

// handle stands in for real work. It fails sometimes so the log shows retries,
// failures and a panic.
func handle(_ context.Context, job Job) error {
	time.Sleep(time.Duration(50+rand.IntN(200)) * time.Millisecond)
	switch n := rand.IntN(20); {
	case n == 0:
		panic("nil inventory record")
	case n < 4:
		return errTemporary
	case n == 4 && job.Kind == "charge_card":
		return errors.New("card declined")
	}
	return nil
}

func cron(ctx context.Context, log *logger.Logger, every time.Duration, task func(context.Context) (int, error)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for run := 1; ; run++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		runCtx := logger.ContextWith(ctx, "run", run)
		start := time.Now()
		n, err := task(runCtx)
		if err != nil {
			log.ErrorContext(runCtx, "cron task failed", err)
			continue
		}
		log.InfoContext(runCtx, "cron task done", "deleted", n, logger.Duration(time.Since(start)))
	}
}

func cleanupExpired(context.Context) (int, error) {
	return rand.IntN(10), nil
}
