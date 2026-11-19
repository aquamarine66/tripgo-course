package create

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"

	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/http/domainerror"
	"github.com/aquamarine66/tripgo-course/internal/http/trip/response"
)

// Handler обрабатывает запрос POST /api/v1/trips.
type Handler struct {
	useCase useCase
	logger  *slog.Logger
	schema  *openapi3.Schema
}

// New создаёт хендлер создания поездки и загружает схему TripData из встроенной спецификации OpenAPI.
func New(uc useCase, logger *slog.Logger) (*Handler, error) {
	spec, err := api.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}

	ref, ok := spec.Components.Schemas["TripData"]
	if !ok || ref.Value == nil {
		return nil, errors.New("openapi spec: schema TripData not found")
	}

	return &Handler{
		useCase: uc,
		logger:  logger,
		schema:  ref.Value,
	}, nil
}

// Handle валидирует тело запроса, вызывает юзкейс и пишет 201 Created с заголовком Location.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	in, err := decodeRequest(w, r, h.schema)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	trip, err := h.useCase.Execute(r.Context(), in)
	if err != nil {
		domainerror.Write(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+trip.ID.String())
	response.WriteJSON(w, http.StatusCreated, response.FromModel(trip))
}
