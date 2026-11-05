package domainerror

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	domaintrip "github.com/aquamarine66/tripgo-course/internal/domain/trip"
	api "github.com/aquamarine66/tripgo-course/internal/generated"
)

type problemKind struct {
	status int32
	code   string
	typ    string
	title  string
}

var (
	kindInvalidRequest = problemKind{
		status: http.StatusBadRequest,
		code:   "invalid_request",
		typ:    "https://tripgo.example/problems/invalid-request",
		title:  "Invalid request",
	}
	kindTripNotFound = problemKind{
		status: http.StatusNotFound,
		code:   "trip_not_found",
		typ:    "https://tripgo.example/problems/trip-not-found",
		title:  "Trip not found",
	}
	kindTripCompleted = problemKind{
		status: http.StatusConflict,
		code:   "trip_completed",
		typ:    "https://tripgo.example/problems/trip-completed",
		title:  "Trip completed",
	}
	kindDriverBusy = problemKind{
		status: http.StatusConflict,
		code:   "driver_busy",
		typ:    "https://tripgo.example/problems/driver-busy",
		title:  "Driver busy",
	}
	kindNotImplemented = problemKind{
		status: http.StatusNotImplemented,
		code:   "not_implemented",
		typ:    "https://tripgo.example/problems/not-implemented",
		title:  "Not implemented",
	}
	kindInternal = problemKind{
		status: http.StatusInternalServerError,
		code:   "internal_error",
		typ:    "https://tripgo.example/problems/internal-error",
		title:  "Internal Server Error",
	}
)

// Problem переводит доменную или валидационную ошибку в HTTP-статус и тело
// api.Problem (RFC 9457). Это единственное место транспортного слоя, где
// классифицируются ошибки: тексты ошибок БД и SQL наружу никогда не попадают.
func Problem(err error, instance string) (int, api.Problem) {
	var valErr *domaintrip.ValidationError
	var paramErr *api.InvalidParamFormatError

	switch {
	case errors.As(err, &valErr):
		return buildProblem(kindInvalidRequest, valErr.Error(), instance)
	case errors.As(err, &paramErr):
		detail := "Parameter " + paramErr.ParamName + " has invalid format"
		return buildProblem(kindInvalidRequest, detail, instance)
	case errors.Is(err, domaintrip.ErrTripNotFound):
		return buildProblem(kindTripNotFound, "Trip was not found", instance)
	case errors.Is(err, domaintrip.ErrTripCompleted):
		return buildProblem(kindTripCompleted, "Operation is not allowed for a completed trip", instance)
	case errors.Is(err, domaintrip.ErrDriverBusy):
		return buildProblem(kindDriverBusy, "Driver already has an active trip", instance)
	default:
		return buildProblem(kindInternal, "Internal server error", instance)
	}
}

// Write классифицирует ошибку через Problem, логирует 5xx ошибки и отправляет
// ответ в application/problem+json.
func Write(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, body := Problem(err, r.URL.Path)
	if status >= http.StatusInternalServerError && logger != nil {
		logger.ErrorContext(r.Context(), "internal error",
			"error", err,
			"method", r.Method,
			"path", r.URL.Path,
		)
	}
	writeBody(w, status, body)
}

// WriteNotImplemented отдаёт 501 в формате problem+json для заглушек будущих работ.
func WriteNotImplemented(w http.ResponseWriter, r *http.Request, detail string) {
	status, body := buildProblem(kindNotImplemented, detail, r.URL.Path)
	writeBody(w, status, body)
}

// ParamErrorHandler — обработчик ошибок разбора path/header-параметров для
// сгенерированного chi-роутера (например, когда tripId не является UUID).
func ParamErrorHandler(logger *slog.Logger) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		var paramErr *api.InvalidParamFormatError
		if errors.As(err, &paramErr) {
			Write(w, r, logger, err)
			return
		}
		Write(w, r, logger, domaintrip.NewValidationError("", "Request parameters are invalid"))
	}
}

func buildProblem(kind problemKind, detail, instance string) (int, api.Problem) {
	d := detail
	inst := instance
	return int(kind.status), api.Problem{
		Type:     kind.typ,
		Title:    kind.title,
		Status:   kind.status,
		Detail:   &d,
		Instance: &inst,
		Code:     kind.code,
	}
}

func writeBody(w http.ResponseWriter, status int, body api.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
