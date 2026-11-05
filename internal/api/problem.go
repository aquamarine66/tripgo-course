package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// problemKind описывает вид ошибки из контракта: для одного code всегда одни и
// те же type, title и HTTP-статус. Меняется только detail.
type problemKind struct {
	status int
	code   string
	typ    string
	title  string
}

var (
	problemInvalidRequest = problemKind{
		status: http.StatusBadRequest,
		code:   "invalid_request",
		typ:    "https://tripgo.example/problems/invalid-request",
		title:  "Invalid request",
	}
	problemTripNotFound = problemKind{
		status: http.StatusNotFound,
		code:   "trip_not_found",
		typ:    "https://tripgo.example/problems/trip-not-found",
		title:  "Trip not found",
	}
	problemTripCompleted = problemKind{
		status: http.StatusConflict,
		code:   "trip_completed",
		typ:    "https://tripgo.example/problems/trip-completed",
		title:  "Trip completed",
	}
	problemDriverBusy = problemKind{
		status: http.StatusConflict,
		code:   "driver_busy",
		typ:    "https://tripgo.example/problems/driver-busy",
		title:  "Driver busy",
	}
	problemNotImplemented = problemKind{
		status: http.StatusNotImplemented,
		code:   "not_implemented",
		typ:    "https://tripgo.example/problems/not-implemented",
		title:  "Not implemented",
	}
	problemInternal = problemKind{
		status: http.StatusInternalServerError,
		code:   "internal_error",
		typ:    "https://tripgo.example/problems/internal-error",
		title:  "Internal Server Error",
	}
)

// writeProblem отдаёт ошибку по RFC 9457 в application/problem+json.
// Тело — сгенерированный из контракта тип Problem.
func writeProblem(w http.ResponseWriter, r *http.Request, kind problemKind, detail string) {
	instance := r.URL.Path
	prob := Problem{
		Type:     kind.typ,
		Title:    kind.title,
		Status:   int32(kind.status),
		Detail:   &detail,
		Instance: &instance,
		Code:     kind.code,
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(kind.status)
	_ = json.NewEncoder(w).Encode(prob)
}

// writeError — единственное место, где доменные ошибки переводятся в HTTP.
// Неизвестные ошибки логируются целиком, а клиенту уходит только общий текст:
// тексты ошибок бд, SQL и прочие внутренние детали наружу не попадают.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, model.ErrTripNotFound):
		writeProblem(w, r, problemTripNotFound, "Trip was not found")
	case errors.Is(err, model.ErrTripAlreadyFinished):
		writeProblem(w, r, problemTripCompleted, "Operation is not allowed for a completed trip")
	case errors.Is(err, model.ErrDriverAlreadyHasActiveTrip):
		writeProblem(w, r, problemDriverBusy, "Driver already has an active trip")
	default:
		slog.ErrorContext(r.Context(), "internal error",
			"error", err,
			"method", r.Method,
			"path", r.URL.Path,
		)
		writeProblem(w, r, problemInternal, "Internal server error")
	}
}

// ParamErrorHandler передаётся в сгенерированный роутер: ошибки разбора
// параметров (например, tripId не UUID) отдаются как 400 invalid_request
// в том же формате problem+json, что и остальные ошибки.
func ParamErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	var detail string
	var paramErr *InvalidParamFormatError
	switch {
	case errors.As(err, &paramErr):
		detail = "Parameter " + paramErr.ParamName + " has invalid format"
	default:
		detail = "Request parameters are invalid"
	}
	writeProblem(w, r, problemInvalidRequest, detail)
}
