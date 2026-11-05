package get

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

// Handler обрабатывает запрос GET /api/v1/trips/{tripId}.
type Handler struct {
	useCase useCase
	logger  *slog.Logger
}

// New создаёт хендлер получения поездки.
func New(uc useCase, logger *slog.Logger) *Handler {
	return &Handler{
		useCase: uc,
		logger:  logger,
	}
}

// Handle возвращает поездку по ID или переводит ошибку в Problem.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	trip, err := h.useCase.Execute(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.FromModel(trip))
}
