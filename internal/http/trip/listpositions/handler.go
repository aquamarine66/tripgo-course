package listpositions

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

// обрабатывает GET /api/v1/trips/{tripId}/positions
type Handler struct {
	useCase useCase
	logger  *slog.Logger
}

// собираем хендлер выдачи маршрута поездки
func New(uc useCase, logger *slog.Logger) *Handler {
	return &Handler{
		useCase: uc,
		logger:  logger,
	}
}

// возвращаем все точки маршрута поездки в порядке возрастания времени
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	positions, err := h.useCase.Execute(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.PositionsFromModel(positions))
}
