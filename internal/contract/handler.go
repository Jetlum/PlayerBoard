package contract

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jetlum/playerboard/internal/auth"
	"github.com/jetlum/playerboard/internal/platform/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the contract endpoints under an already-authenticated /me group.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/contracts", h.listContracts)
	r.Get("/contracts/{id}/clauses", h.listClauses)
}

func (h *Handler) listContracts(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	contracts, err := h.svc.Contracts(r.Context(), athleteID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load contracts")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"contracts": contracts})
}

func (h *Handler) listClauses(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	contractID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid contract id")
		return
	}
	clauses, err := h.svc.Clauses(r.Context(), contractID, athleteID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load clauses")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"clauses": clauses})
}
