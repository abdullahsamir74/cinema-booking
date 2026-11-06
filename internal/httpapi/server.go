// Package httpapi is the HTTP transport layer. It translates requests into
// booking service calls and domain errors into JSON responses.
package httpapi

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/booking"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

//go:embed assets/index.html
var assets embed.FS

// Server owns the HTTP handlers and their dependencies.
type Server struct {
	svc         *booking.Service
	logger      *slog.Logger
	version     string
	corsOrigins []string
	adminToken  string
}

// NewServer builds the HTTP layer.
func NewServer(svc *booking.Service, logger *slog.Logger, version string, corsOrigins []string, adminToken string) *Server {
	return &Server{
		svc:         svc,
		logger:      logger,
		version:     version,
		corsOrigins: corsOrigins,
		adminToken:  adminToken,
	}
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.middleware(s.routes())
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/movies", s.listMovies)
	mux.HandleFunc("GET /api/v1/movies/{movieID}", s.getMovie)
	mux.HandleFunc("GET /api/v1/movies/{movieID}/seats", s.seatMap)
	mux.HandleFunc("GET /api/v1/movies/{movieID}/bookings", s.listBookings)
	mux.HandleFunc("POST /api/v1/movies/{movieID}/seats/{seatID}/hold", s.holdSeat)
	mux.HandleFunc("POST /api/v1/movies/{movieID}/seats/{seatID}/confirm", s.confirmSeat)
	mux.HandleFunc("DELETE /api/v1/movies/{movieID}/seats/{seatID}/hold", s.releaseSeat)

	mux.Handle("POST /api/v1/admin/movies", s.requireAdmin(http.HandlerFunc(s.createMovie)))

	// Methodless fallbacks keep 405 responses in the same JSON envelope as the
	// rest of the API instead of ServeMux's plain-text default.
	mux.HandleFunc("/api/v1/movies", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/movies/{movieID}", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/movies/{movieID}/seats", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/movies/{movieID}/bookings", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/movies/{movieID}/seats/{seatID}/hold", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/movies/{movieID}/seats/{seatID}/confirm", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/admin/movies", s.methodNotAllowed)

	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/ready", s.ready)
	mux.HandleFunc("/healthz", s.methodNotAllowed)
	mux.HandleFunc("/readyz", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/health", s.methodNotAllowed)
	mux.HandleFunc("/api/v1/ready", s.methodNotAllowed)

	mux.HandleFunc("/", s.root)

	return mux
}

func (s *Server) listMovies(w http.ResponseWriter, r *http.Request) {
	movies, err := s.svc.ListMovies(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"movies": movies})
}

func (s *Server) getMovie(w http.ResponseWriter, r *http.Request) {
	movie, err := s.svc.GetMovie(r.Context(), r.PathValue("movieID"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, movie)
}

func (s *Server) createMovie(w http.ResponseWriter, r *http.Request) {
	var movie domain.Movie
	if err := decodeJSON(w, r, &movie); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: %v", errInvalidBody, err))
		return
	}

	created, err := s.svc.CreateMovie(r.Context(), movie)
	if err != nil {
		s.writeError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/api/v1/movies/%s", created.ID))
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) seatMap(w http.ResponseWriter, r *http.Request) {
	seatMap, err := s.svc.SeatMap(r.Context(), r.PathValue("movieID"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, seatMap)
}

func (s *Server) listBookings(w http.ResponseWriter, r *http.Request) {
	bookings, err := s.svc.ListBookings(r.Context(), r.PathValue("movieID"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": bookings})
}

func (s *Server) holdSeat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.readUserID(w, r)
	if !ok {
		return
	}

	booking, err := s.svc.Hold(r.Context(), r.PathValue("movieID"), r.PathValue("seatID"), userID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/movies/%s/seats", booking.MovieID))
	writeJSON(w, http.StatusCreated, booking)
}

func (s *Server) confirmSeat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.readUserID(w, r)
	if !ok {
		return
	}

	booking, err := s.svc.Confirm(r.Context(), r.PathValue("movieID"), r.PathValue("seatID"), userID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, booking)
}

func (s *Server) releaseSeat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.readUserID(w, r)
	if !ok {
		return
	}

	if err := s.svc.Release(r.Context(), r.PathValue("movieID"), r.PathValue("seatID"), userID); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "cinema-booking",
		"version": s.version,
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.svc.Ready(ctx); err != nil {
		s.logger.WarnContext(r.Context(), "readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		s.apiNotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		body := errorBody{RequestID: RequestIDFromContext(r.Context())}
		body.Error.Code = "method_not_allowed"
		body.Error.Message = "method not allowed"
		writeJSON(w, http.StatusMethodNotAllowed, body)
		return
	}
	s.serveIndex(w, r)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		s.logger.ErrorContext(r.Context(), "read embedded frontend", "error", err)
		http.Error(w, "frontend unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) apiNotFound(w http.ResponseWriter, r *http.Request) {
	body := errorBody{RequestID: RequestIDFromContext(r.Context())}
	body.Error.Code = "not_found"
	body.Error.Message = "no API route matches this request"
	writeJSON(w, http.StatusNotFound, body)
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET, POST, DELETE, OPTIONS")
	body := errorBody{RequestID: RequestIDFromContext(r.Context())}
	body.Error.Code = "method_not_allowed"
	body.Error.Message = "method not allowed for this route"
	writeJSON(w, http.StatusMethodNotAllowed, body)
}

// readUserID accepts either the X-User-ID header or a JSON body with a
// user_id field. The header form allows clients to send bodyless DELETE
// requests through proxies that strip DELETE bodies.
func (s *Server) readUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if header := r.Header.Get("X-User-ID"); header != "" {
		return header, true
	}

	var req userRequest
	if err := decodeJSON(w, r, &req); err != nil {
		s.writeError(w, r, fmt.Errorf("%w: %v", errInvalidBody, err))
		return "", false
	}
	if req.UserID == "" {
		s.writeError(w, r, domain.ErrInvalidUserID)
		return "", false
	}
	return req.UserID, true
}

var (
	errInvalidBody   = errors.New("invalid request body")
	errUnauthorized  = errors.New("admin authorization required")
	errAdminDisabled = errors.New("admin API disabled")
)
