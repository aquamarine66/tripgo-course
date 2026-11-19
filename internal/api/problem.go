package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/aquamarine66/tripgo-course/internal/model"
)

// шаблон для ошибок по контракту, меняется только detail под конкретный случай
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
	problemNotFound = problemKind{
		status: http.StatusNotFound,
		code:   "not_found",
		typ:    "https://tripgo.example/problems/not-found",
		title:  "Not Found",
	}
	problemMethodNotAllowed = problemKind{
		status: http.StatusMethodNotAllowed,
		code:   "method_not_allowed",
		typ:    "https://tripgo.example/problems/method-not-allowed",
		title:  "Method Not Allowed",
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

// пишет ответ с ошибкой в едином формате problem+json
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

// переводит ошибки сервиса в http-ответы, при 500 детали пишем только в лог
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

// обрабатывает ошибки парсинга параметров роута, например битый uuid в пути
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

// отдаёт 404 в problem+json, если запрос пришёл на несуществующий путь
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, problemNotFound, "Route not found")
}

// отдаёт 405 в problem+json, если для пути не поддерживается такой метод
func MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, problemMethodNotAllowed, "Method is not allowed")
}

// обёртка над ResponseWriter, чтобы понять, успел ли хендлер уже начать писать ответ
type statusWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// ловит паники и отдаёт 500 в том же формате problem+json, что и остальные ошибки
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"panic", p,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				if !sw.wroteHeader {
					writeProblem(sw, r, problemInternal, "Internal server error")
				}
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
