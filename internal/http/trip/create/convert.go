package create

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

func decodeRequest(w http.ResponseWriter, r *http.Request, schema *openapi3.Schema) (model.CreateTripInput, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return model.CreateTripInput{}, domaintrip.NewValidationError("", "request body is too large")
		}
		return model.CreateTripInput{}, domaintrip.NewValidationError("", "failed to read request body")
	}

	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return model.CreateTripInput{}, domaintrip.NewValidationError("", "request body must be valid JSON")
	}

	if err := schema.VisitJSON(generic); err != nil {
		return model.CreateTripInput{}, schemaValidationError(err)
	}

	var body api.TripData
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return model.CreateTripInput{}, domaintrip.NewValidationError("", "user_id and driver_id must be valid UUIDs")
	}

	if body.UserId == uuid.Nil {
		return model.CreateTripInput{}, domaintrip.NewValidationError("user_id", "must be a non-empty UUID")
	}
	if body.DriverId == uuid.Nil {
		return model.CreateTripInput{}, domaintrip.NewValidationError("driver_id", "must be a non-empty UUID")
	}

	return model.CreateTripInput{
		UserID:   body.UserId,
		DriverID: body.DriverId,
		Price:    body.Price,
		StartLat: body.StartPoint.Latitude,
		StartLng: body.StartPoint.Longitude,
		EndLat:   body.EndPoint.Latitude,
		EndLng:   body.EndPoint.Longitude,
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
