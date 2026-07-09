// Command worker consumes performance events and runs the milestone engine. It uses a
// bounded, partitioned worker pool: events for one athlete are pinned to a single worker
// (ordered), while different athletes run in parallel.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/milestone"
	"github.com/jetlum/playerboard/internal/platform/bus"
	"github.com/jetlum/playerboard/internal/platform/config"
	"github.com/jetlum/playerboard/internal/platform/db"
	logpkg "github.com/jetlum/playerboard/internal/platform/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := logpkg.New("worker")
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Pool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	b, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("bus: %w", err)
	}
	defer b.Close()

	engine := milestone.NewEngine(pool)

	n := cfg.WorkerPoolSize
	if n < 1 {
		n = 1
	}

	// One buffered channel + goroutine per partition. errgroup fans them out and returns the
	// first error; ctx cancellation drains them cleanly.
	g, gctx := errgroup.WithContext(ctx)
	partitions := make([]chan bus.Delivery, n)
	for i := 0; i < n; i++ {
		ch := make(chan bus.Delivery, 64)
		partitions[i] = ch
		g.Go(func() error {
			for {
				select {
				case <-gctx.Done():
					return nil
				case d, ok := <-ch:
					if !ok {
						return nil
					}
					if err := engine.Handle(gctx, d.Data); err != nil {
						log.Warn("engine failed; redelivering", "err", err)
						d.Nak()
					} else {
						d.Ack()
					}
				}
			}
		})
	}

	if err := b.Consume(bus.SubjectPerformance, "milestone-worker", func(d bus.Delivery) {
		idx := partitionFor(d.Data, n)
		select {
		case partitions[idx] <- d:
		case <-gctx.Done():
			d.Nak()
		}
	}); err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	log.Info("worker running", "pool_size", n, "subject", bus.SubjectPerformance)
	<-gctx.Done()
	log.Info("worker draining")
	return g.Wait()
}

// partitionFor pins all events for one athlete to the same worker (ordering guarantee).
func partitionFor(data []byte, n int) int {
	var evt events.PerformanceObserved
	if err := json.Unmarshal(data, &evt); err != nil || evt.AthleteID == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(evt.AthleteID))
	return int(h.Sum32() % uint32(n))
}
