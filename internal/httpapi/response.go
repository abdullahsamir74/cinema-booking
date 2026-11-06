package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

type userRequest struct {
	UserID string `json:"user_id"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if value == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := mapError(err)
	if status >= http.StatusInternalServerError {
		s.logger.ErrorContext(r.Context(), "request failed",
			"error", err,
			"path", r.URL.Path,
			"request_id", RequestIDFromContext(r.Context()),
		)
	}

	body := errorBody{RequestID: RequestIDFromContext(r.Context())}
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

func mapError(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, domain.ErrMovieNotFound):
		return http.StatusNotFound, "movie_not_found", "movie not found"
	case errors.Is(err, domain.ErrMovieExists):
		return http.StatusConflict, "movie_exists", "a movie with this id already exists"
	case errors.Is(err, domain.ErrInvalidMovie):
		return http.StatusBadRequest, "invalid_movie", err.Error()
	case errors.Is(err, domain.ErrInvalidUserID):
		return http.StatusBadRequest, "invalid_user_id", "user_id must be 3-64 letters, numbers, hyphens or underscores"
	case errors.Is(err, domain.ErrInvalidSeatID):
		return http.StatusBadRequest, "invalid_seat_id", err.Error()
	case errors.Is(err, domain.ErrSeatUnavailable):
		return http.StatusConflict, "seat_unavailable", "seat is already held or confirmed"
	case errors.Is(err, domain.ErrHoldNotFound):
		return http.StatusGone, "hold_expired", "hold no longer exists or has expired"
	case errors.Is(err, domain.ErrNotHoldOwner):
		return http.StatusForbidden, "not_hold_owner", "seat is held by another user"
	case errors.Is(err, domain.ErrBookingConfirmed):
		return http.StatusConflict, "booking_confirmed", "confirmed bookings cannot be released"
	case errors.Is(err, errInvalidBody):
		return http.StatusBadRequest, "invalid_request", "invalid request body: expected a JSON object"
	case errors.Is(err, errUnauthorized):
		return http.StatusUnauthorized, "unauthorized", "a valid admin bearer token is required"
	case errors.Is(err, errAdminDisabled):
		return http.StatusServiceUnavailable, "admin_disabled", "the admin API is disabled; set ADMIN_TOKEN to enable it"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout", "the request timed out"
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}
