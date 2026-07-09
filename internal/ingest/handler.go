package ingest

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/platform/bus"
	"github.com/jetlum/playerboard/internal/platform/httpx"
	"github.com/jetlum/playerboard/internal/query"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Handler ingests signed performance webhooks. No user JWT — authenticity comes from the signature.
type Handler struct {
	pool     *pgxpool.Pool
	verifier Verifier
}

func NewHandler(pool *pgxpool.Pool, v Verifier) *Handler {
	return &Handler{pool: pool, verifier: v}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/webhooks/scoreboard", h.receive)
}

func (h *Handler) receive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unreadable body")
		return
	}

	eventID := r.Header.Get("X-Event-Id")
	ts := r.Header.Get("X-Timestamp")
	sig := r.Header.Get("X-Signature")
	if eventID == "" || ts == "" || sig == "" {
		httpx.Error(w, http.StatusBadRequest, "missing signature headers")
		return
	}

	// Replay guard, then signature — both before touching the payload.
	if err := CheckTimestamp(ts, time.Now()); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "stale timestamp")
		return
	}
	if err := h.verifier.Verify(ts, raw, sig); err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	var evt events.PerformanceObserved
	if err := json.Unmarshal(raw, &evt); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := uuid.Parse(evt.AthleteID); err != nil || evt.Metric == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid event fields")
		return
	}
	evt.SourceEventID = eventID

	// Dedupe + outbox write in one transaction. No business logic on the request path.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "tx begin failed")
		return
	}
	defer tx.Rollback(ctx)
	q := query.New(tx)

	if _, err := q.InsertInboundEvent(ctx, query.InsertInboundEventParams{
		SourceEventID: eventID,
		Kind:          "performance",
		Raw:           raw,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Duplicate delivery: idempotent no-op.
			_ = tx.Rollback(ctx)
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "persist failed")
		return
	}

	payload, _ := json.Marshal(evt)
	if _, err := q.InsertOutbox(ctx, query.InsertOutboxParams{
		Aggregate: "athlete:" + evt.AthleteID,
		EventType: events.TypePerformanceObserved,
		Subject:   bus.SubjectPerformance,
		Payload:   payload,
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "outbox write failed")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "commit failed")
		return
	}

	slog.Info("webhook accepted", "source_event_id", eventID, "metric", evt.Metric, "value", evt.Value)
	httpx.JSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
