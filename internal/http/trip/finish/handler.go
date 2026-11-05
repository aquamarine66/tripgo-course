package finish

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

// Handler обрабатывает запрос POST /api/v1/trips/{tripId}/finish.
type Handler struct {
	useCase useCase
	logger  *slog.Logger
}

// New создаёт хендлер завершения поездки.
func New(uc useCase, logger *slog.Logger) *Handler {
	return &Handler{
		useCase: uc,
		logger:  logger,
	}
}

// Handle завершает поездку и возвращает её обновлённое представление.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	trip, err := h.useCase.Execute(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.FromModel(trip))
}
