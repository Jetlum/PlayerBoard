// Package club exposes the club-side console — the ScoreBoard/ScoreAlerts side of the
// pitch. In production this would be ScoreBoard's own backend calling PlayerBoard's signed
// webhook when a stats provider confirms a match; here the club console plays that role
// directly, so the demo shows both sides of the same event: the club's action and the
// specific player's board reacting to it in real time.
//
// Deferred: these routes have no auth (see README-run.md "Deferred"). A real deployment
// would gate them behind an agent/club-role JWT scoped to that club's own roster.
package club

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/ingest"
	"github.com/jetlum/playerboard/internal/platform/httpx"
	"github.com/jetlum/playerboard/internal/query"
)

type Handler struct {
	q             *query.Queries
	webhookSecret string
	selfURL       string
}

func NewHandler(pool *pgxpool.Pool, webhookSecret, selfURL string) *Handler {
	return &Handler{q: query.New(pool), webhookSecret: webhookSecret, selfURL: selfURL}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/athletes", h.roster)
	r.Post("/record-appearance", h.recordAppearance)
	r.Get("/events", h.recentEvents)
}

type rosterEntry struct {
	AthleteID   string `json:"athlete_id"`
	DisplayName string `json:"display_name"`
	Metric      string `json:"metric"`
	Progress    int64  `json:"progress"`
	Target      int64  `json:"target"`
	Tranche     int64  `json:"tranche"`
	State       string `json:"state"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
}

func (h *Handler) roster(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q.ListRoster(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load roster")
		return
	}
	out := make([]rosterEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, rosterEntry{
			AthleteID:   row.AthleteID.String(),
			DisplayName: row.DisplayName,
			Metric:      row.Metric,
			Progress:    row.Progress,
			Target:      row.Target,
			Tranche:     row.Tranche,
			State:       row.State,
			Amount:      row.Amount,
			Currency:    row.Currency,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"roster": out})
}

type recordAppearanceReq struct {
	AthleteID string `json:"athlete_id"`
}

// recordAppearance is the club clicking "this player just played a match": it atomically
// advances that athlete's appearance counter by one and sends the new value through the exact
// same signed-webhook path a real stats provider (Opta/Wyscout via ScoreAlerts) would use.
//
// The counter increment is a single SQL UPSERT (see IncrementAppearanceCounter) rather than a
// read-current-progress-then-add-one in Go: two rapid clicks (or two club operators acting at
// once) used to both read the same milestone.progress before either webhook had been processed
// by the async worker, and both would compute the same "next" value — the second was then
// silently dropped downstream as a no-op advance. Postgres now serializes the increment itself.
func (h *Handler) recordAppearance(w http.ResponseWriter, r *http.Request) {
	var req recordAppearanceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	athleteID, err := uuid.Parse(req.AthleteID)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid athlete_id")
		return
	}

	next, err := h.q.IncrementAppearanceCounter(r.Context(), query.IncrementAppearanceCounterParams{
		AthleteID: athleteID,
		Metric:    "appearances",
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to advance appearance counter")
		return
	}

	status, body, err := ingest.ForwardSigned(r.Context(), h.selfURL, h.webhookSecret,
		"club-appearance", athleteID.String(), "appearances", next)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// recentEvents backs the ClubBoard live-feed panel across a page refresh: the WebSocket only
// ever delivers events that happen after it connects, so on load the frontend replays recent
// history from here (the outbox is already a durable, ordered event log) before switching to
// live pushes.
func (h *Handler) recentEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := h.q.ListRecentMilestoneEvents(r.Context(), 50)
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
