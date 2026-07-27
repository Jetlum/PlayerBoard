package contract

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/auth"
	"github.com/jetlum/playerboard/internal/platform/httpx"
	"github.com/jetlum/playerboard/internal/query"
)

// DocHandler serves the contract document upload/analysis endpoints.
type DocHandler struct {
	q *query.Queries
}

func NewDocHandler(pool *pgxpool.Pool) *DocHandler {
	return &DocHandler{q: query.New(pool)}
}

// Routes mounts the contract-document endpoints under an already-authenticated /me group.
func (h *DocHandler) Routes(r chi.Router) {
	r.Post("/contract-documents", h.upload)
	r.Get("/contract-documents", h.list)
	r.Get("/contract-documents/{id}", h.get)
	r.Put("/contract-documents/{id}/text", h.updateText)
	r.Delete("/contract-documents/{id}", h.remove)
}

// upload handles multipart file upload of a contract document (max 10 MiB).
func (h *DocHandler) upload(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	// 10 MiB limit.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httpx.Error(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	label := r.FormValue("label")
	contractIDStr := r.FormValue("contract_id")
	rawText := r.FormValue("raw_text") // client may send pre-extracted text

	var contractID pgtype.UUID
	if contractIDStr != "" {
		if parsed, err := uuid.Parse(contractIDStr); err == nil {
			contractID = pgtype.UUID{Bytes: parsed, Valid: true}
		}
	}

	docID := uuid.New()
	status := "uploaded"
	if rawText != "" {
		status = "analyzed"
	}

	if err := h.q.InsertContractDocument(r.Context(), query.InsertContractDocumentParams{
		ID:          docID,
		AthleteID:   athleteID,
		ContractID:  contractID,
		Filename:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		RawText:     rawText,
		FileData:    data,
		Label:       label,
		Status:      status,
	}); err != nil {
		slog.Error("insert contract document", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to save document")
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{
		"id":       docID.String(),
		"filename": header.Filename,
		"status":   status,
	})
}

func (h *DocHandler) list(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	docs, err := h.q.ListContractDocumentsByAthlete(r.Context(), athleteID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to load documents")
		return
	}
	type docView struct {
		ID         string `json:"id"`
		ContractID string `json:"contract_id,omitempty"`
		Filename   string `json:"filename"`
		Label      string `json:"label"`
		Status     string `json:"status"`
		RawText    string `json:"raw_text"`
		CreatedAt  string `json:"created_at"`
	}
	out := make([]docView, 0, len(docs))
	for _, d := range docs {
		cid := ""
		if d.ContractID.Valid {
			cid = uuid.UUID(d.ContractID.Bytes).String()
		}
		out = append(out, docView{
			ID:         d.ID.String(),
			ContractID: cid,
			Filename:   d.Filename,
			Label:      d.Label,
			Status:     d.Status,
			RawText:    d.RawText,
			CreatedAt:  d.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"documents": out})
}

func (h *DocHandler) get(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	docID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid document id")
		return
	}
	doc, err := h.q.GetContractDocument(r.Context(), query.GetContractDocumentParams{
		ID: docID, AthleteID: athleteID,
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "document not found")
		return
	}
	cid := ""
	if doc.ContractID.Valid {
		cid = uuid.UUID(doc.ContractID.Bytes).String()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id":          doc.ID.String(),
		"contract_id": cid,
		"filename":    doc.Filename,
		"label":       doc.Label,
		"status":      doc.Status,
		"raw_text":    doc.RawText,
		"created_at":  doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *DocHandler) updateText(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	docID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid document id")
		return
	}
	var body struct {
		RawText string `json:"raw_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.q.UpdateContractDocumentText(r.Context(), query.UpdateContractDocumentTextParams{
		ID: docID, AthleteID: athleteID, RawText: body.RawText,
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update document text")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "analyzed"})
}

func (h *DocHandler) remove(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	docID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid document id")
		return
	}
	if err := h.q.DeleteContractDocument(r.Context(), query.DeleteContractDocumentParams{
		ID: docID, AthleteID: athleteID,
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to delete document")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
