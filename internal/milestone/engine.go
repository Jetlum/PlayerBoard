// Package milestone contains the worker-side engine that turns performance events into
// milestone progress + payouts, and the player-facing read handler.
package milestone

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/milestone/domain"
	"github.com/jetlum/playerboard/internal/platform/bus"
	"github.com/jetlum/playerboard/internal/query"
)

// Engine applies a PerformanceObserved event to the athlete's milestones.
// Everything for one event happens in a single transaction so the stat upsert, milestone
// update, payout, and outbox emission commit atomically. Idempotent under redelivery.
type Engine struct {
	pool *pgxpool.Pool
}

func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{pool: pool}
}

// Handle processes one bus message. Returning an error causes a NAK (redelivery).
func (e *Engine) Handle(ctx context.Context, data []byte) error {
	var evt events.PerformanceObserved
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil // unparseable payloads are dropped, not retried forever
	}
	athleteID, err := uuid.Parse(evt.AthleteID)
	if err != nil {
		return nil
	}

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := query.New(tx)

	// Idempotent stat write (UNIQUE source_event_id).
	if err := q.InsertPerformanceStat(ctx, query.InsertPerformanceStatParams{
		ID:            uuid.New(),
		AthleteID:     athleteID,
		Metric:        evt.Metric,
		Value:         evt.Value,
		SourceEventID: evt.SourceEventID,
	}); err != nil {
		return err
	}

	// FOR UPDATE row-locks the athlete's milestones -> per-athlete serialization.
	milestones, err := q.ListMilestonesByAthleteMetric(ctx, query.ListMilestonesByAthleteMetricParams{
		AthleteID: athleteID,
		Metric:    evt.Metric,
	})
	if err != nil {
		return err
	}

	for _, m := range milestones {
		res := domain.Advance(m.Progress, evt.Value, m.Target, m.Tranche, m.MaxRepeats)
		// Skip stale/duplicate deliveries that neither advance progress nor cross a boundary,
		// preserving a previously-fulfilled state.
		if res.Progress == m.Progress && len(res.Crossed) == 0 {
			continue
		}

		if err := q.UpdateMilestoneProgress(ctx, query.UpdateMilestoneProgressParams{
			ID:       m.ID,
			Progress: res.Progress,
			State:    res.State,
		}); err != nil {
			return err
		}

		for _, boundary := range res.Crossed {
			if err := q.InsertPayoutEvent(ctx, query.InsertPayoutEventParams{
				ID:          uuid.New(),
				AthleteID:   athleteID,
				MilestoneID: m.ID,
				Boundary:    boundary,
				Amount:      m.Amount,
				Currency:    m.Currency,
			}); err != nil {
				return err
			}
		}

		changed := events.MilestoneChanged{
			Type:        events.TypeMilestoneAdvanced,
			AthleteID:   evt.AthleteID,
			MilestoneID: m.ID.String(),
			Metric:      m.Metric,
			Progress:    res.Progress,
			Target:      m.Target,
			State:       res.State,
		}
		if n := len(res.Crossed); n > 0 {
			changed.Type = events.TypeMilestoneFulfilled
			changed.Boundary = res.Crossed[n-1]
			changed.Amount = m.Amount
			changed.Currency = m.Currency
		}
		payload, _ := json.Marshal(changed)
		if _, err := q.InsertOutbox(ctx, query.InsertOutboxParams{
			Aggregate: "athlete:" + evt.AthleteID,
			EventType: changed.Type,
			Subject:   bus.SubjectMilestone,
			Payload:   payload,
		}); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
