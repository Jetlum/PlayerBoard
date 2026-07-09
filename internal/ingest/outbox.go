package ingest

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/platform/bus"
	"github.com/jetlum/playerboard/internal/query"
)

// Relay is the transactional-outbox publisher: it drains unpublished rows to the bus and
// stamps them published. Publish-then-mark gives at-least-once delivery (consumers are idempotent).
type Relay struct {
	q        *query.Queries
	bus      *bus.Bus
	interval time.Duration
	batch    int32
}

func NewRelay(pool *pgxpool.Pool, b *bus.Bus) *Relay {
	return &Relay{q: query.New(pool), bus: b, interval: 250 * time.Millisecond, batch: 100}
}

// Run polls until the context is cancelled.
func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := r.drain(ctx); n > 0 {
				slog.Debug("outbox drained", "published", n)
			}
		}
	}
}

func (r *Relay) drain(ctx context.Context) int {
	rows, err := r.q.ListUnpublishedOutbox(ctx, r.batch)
	if err != nil {
		slog.Warn("outbox read failed", "err", err)
		return 0
	}
	published := 0
	for _, row := range rows {
		if err := r.bus.Publish(row.Subject, row.Payload); err != nil {
			slog.Warn("outbox publish failed", "id", row.ID, "err", err)
			break // preserve ordering; retry next tick
		}
		if err := r.q.MarkOutboxPublished(ctx, row.ID); err != nil {
			slog.Warn("outbox mark failed", "id", row.ID, "err", err)
			break
		}
		published++
	}
	return published
}
