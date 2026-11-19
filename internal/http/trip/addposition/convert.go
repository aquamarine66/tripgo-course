package addposition

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	api "github.com/aquamarine66/tripgo-course/internal/generated"
	"github.com/aquamarine66/tripgo-course/internal/model"
)

const maxBodyBytes = 1 << 20

func decodeRequest(
	w http.ResponseWriter,
	r *http.Request,
	tripID uuid.UUID,
	schema *openapi3.Schema,
) (model.CreatePositionInput, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return model.CreatePositionInput{}, domaintrip.NewValidationError("", "request body is too large")
		}
		return model.CreatePositionInput{}, domaintrip.NewValidationError("", "failed to read request body")
	}

	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return model.CreatePositionInput{}, domaintrip.NewValidationError("", "request body must be valid JSON")
	}

	if err := schema.VisitJSON(generic); err != nil {
		return model.CreatePositionInput{}, schemaValidationError(err)
	}

	var body api.PositionData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return model.CreatePositionInput{}, domaintrip.NewValidationError("recorded_at", "recorded_at must be a valid RFC3339 timestamp")
	}

	return model.CreatePositionInput{
		TripID:     tripID,
		Latitude:   body.Coordinates.Latitude,
		Longitude:  body.Coordinates.Longitude,
		RecordedAt: body.RecordedAt,
	}, nil
}

func schemaValidationError(err error) error {
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		field := strings.Join(schemaErr.JSONPointer(), "/")
		if field != "" {
			return domaintrip.NewValidationError("/"+field, schemaErr.Reason)
		}
		return domaintrip.NewValidationError("", fmt.Sprintf("request body: %s", schemaErr.Reason))
	}
	return domaintrip.NewValidationError("", "request body does not match the contract")
}
