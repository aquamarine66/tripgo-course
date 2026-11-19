package addposition

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

// обрабатывает POST /api/v1/trips/{tripId}/positions
type Handler struct {
	useCase useCase
	logger  *slog.Logger
	schema  *openapi3.Schema
}

// собираем хендлер и достаём схему PositionData из встроенного контракта
func New(uc useCase, logger *slog.Logger) (*Handler, error) {
	spec, err := api.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}

	ref, ok := spec.Components.Schemas["PositionData"]
	if !ok || ref.Value == nil {
		return nil, errors.New("openapi spec: schema PositionData not found")
	}

	return &Handler{
		useCase: uc,
		logger:  logger,
		schema:  ref.Value,
	}, nil
}

// проверяем тело запроса, сохраняем координату и отдаём 201 Created
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	in, err := decodeRequest(w, r, tripID, h.schema)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	pos, err := h.useCase.Execute(r.Context(), in)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.PositionFromModel(pos))
}
