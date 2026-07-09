package milestone

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/auth"
	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/platform/httpx"
	"github.com/jetlum/playerboard/internal/query"
)

// ReadHandler serves the player's milestone progress.
type ReadHandler struct {
	q *query.Queries
}

func NewReadHandler(pool *pgxpool.Pool) *ReadHandler {
	return &ReadHandler{q: query.New(pool)}
}

func (h *ReadHandler) Routes(r chi.Router) {
	r.Get("/milestones", h.list)
	r.Get("/events", h.recentEvents)
}

type milestoneView struct {
	ID       string `json:"id"`
	Metric   string `json:"metric"`
	Target   int64  `json:"target"`
	Tranche  int64  `json:"tranche"`
	Progress int64  `json:"progress"`
	State    string `json:"state"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func (h *ReadHandler) list(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	rows, err := h.q.ListMilestonesByAthlete(r.Context(), athleteID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load milestones")
		return
	}
	out := make([]milestoneView, 0, len(rows))
	for _, m := range rows {
		out = append(out, milestoneView{
			ID:       m.ID.String(),
			Metric:   m.Metric,
			Target:   m.Target,
			Tranche:  m.Tranche,
			Progress: m.Progress,
			State:    m.State,
			Amount:   m.Amount,
			Currency: m.Currency,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"milestones": out})
}

// recentEvents backs the PlayerBoard live-feed panel across a page refresh: the WebSocket only
// ever delivers events that happen after it connects, so on load the frontend replays this
// athlete's recent history from the outbox (already a durable, ordered event log) before
// switching to live pushes.
func (h *ReadHandler) recentEvents(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	rows, err := h.q.ListRecentMilestoneEventsForAthlete(r.Context(), query.ListRecentMilestoneEventsForAthleteParams{
		Column1: athleteID.String(),
		Limit:   50,
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load event history")
		return
	}
	// rows are newest-first (as queried); walk backwards so the response is oldest-first — the
	// frontend replays in order and ends up with the newest entry on top, exactly like a live push.
	out := make([]events.HistoricalEvent, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		var evt events.MilestoneChanged
		_ = json.Unmarshal(rows[i].Payload, &evt)
		out = append(out, events.HistoricalEvent{MilestoneChanged: evt, At: rows[i].CreatedAt.Format(time.RFC3339)})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"events": out})
}
