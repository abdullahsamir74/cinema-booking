package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/adapters/memory"
	"github.com/abdullahsamir74/cinema-booking/internal/booking"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	"github.com/abdullahsamir74/cinema-booking/internal/httpapi"
)

const testAdminToken = "test-admin-token"

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return newTestHandlerWithToken(t, testAdminToken)
}

func newTestHandlerWithToken(t *testing.T, adminToken string) http.Handler {
	t.Helper()
	store := memory.New()
	svc := booking.NewService(store, memory.NewCatalog(), time.Minute)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewServer(svc, logger, "test", []string{"*"}, adminToken).Handler()
}

func doRequest(t *testing.T, handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(payload)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return value
}

func holdSeat(t *testing.T, handler http.Handler, movie, seat, user string) domain.Booking {
	t.Helper()
	rec := doRequest(t, handler, http.MethodPost,
		"/api/v1/movies/"+movie+"/seats/"+seat+"/hold",
		map[string]string{"user_id": user}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("hold %s: expected 201, got %d (%s)", seat, rec.Code, rec.Body.String())
	}
	return decode[domain.Booking](t, rec)
}

func TestListMoviesAndGetMovie(t *testing.T) {
	handler := newTestHandler(t)

	rec := doRequest(t, handler, http.MethodGet, "/api/v1/movies", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var payload struct {
		Movies []domain.Movie `json:"movies"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Movies) < 3 {
		t.Fatalf("expected several movies, got %d", len(payload.Movies))
	}

	rec = doRequest(t, handler, http.MethodGet, "/api/v1/movies/inception", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	movie := decode[domain.Movie](t, rec)
	if movie.ID != "inception" {
		t.Fatalf("unexpected movie: %+v", movie)
	}

	rec = doRequest(t, handler, http.MethodGet, "/api/v1/movies/does-not-exist", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestBookingLifecycle(t *testing.T) {
	handler := newTestHandler(t)

	held := holdSeat(t, handler, "inception", "A1", "alice-1")
	if held.Status != domain.StatusHeld || held.SeatID != "A1" {
		t.Fatalf("unexpected hold: %+v", held)
	}

	rec := doRequest(t, handler, http.MethodGet, "/api/v1/movies/inception/seats", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("seat map: expected 200, got %d", rec.Code)
	}
	seatMap := decode[domain.SeatMap](t, rec)
	if seatMap.Stats.Held != 1 || seatMap.Stats.Available != 59 {
		t.Fatalf("unexpected stats: %+v", seatMap.Stats)
	}

	rec = doRequest(t, handler, http.MethodPost,
		"/api/v1/movies/inception/seats/A1/confirm", nil,
		map[string]string{"X-User-ID": "alice-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	confirmed := decode[domain.Booking](t, rec)
	if confirmed.Status != domain.StatusConfirmed {
		t.Fatalf("expected confirmed status, got %q", confirmed.Status)
	}

	rec = doRequest(t, handler, http.MethodDelete,
		"/api/v1/movies/inception/seats/A1/hold", nil,
		map[string]string{"X-User-ID": "alice-1"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("release confirmed: expected 409, got %d", rec.Code)
	}
	errPayload := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if errPayload.Error.Code != "booking_confirmed" {
		t.Fatalf("unexpected error code: %q", errPayload.Error.Code)
	}
}

func TestSeatConflictAndRelease(t *testing.T) {
	handler := newTestHandler(t)
	holdSeat(t, handler, "dune-part-two", "B2", "alice-1")

	rec := doRequest(t, handler, http.MethodPost,
		"/api/v1/movies/dune-part-two/seats/B2/hold",
		map[string]string{"user_id": "bob-22"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	conflict := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if conflict.Error.Code != "seat_unavailable" {
		t.Fatalf("unexpected code: %q", conflict.Error.Code)
	}

	rec = doRequest(t, handler, http.MethodDelete,
		"/api/v1/movies/dune-part-two/seats/B2/hold", nil,
		map[string]string{"X-User-ID": "alice-1"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("release: expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	held := holdSeat(t, handler, "dune-part-two", "B2", "bob-22")
	if held.UserID != "bob-22" {
		t.Fatalf("unexpected booking after release: %+v", held)
	}
}

func TestReleaseAnotherUsersHoldIsForbidden(t *testing.T) {
	handler := newTestHandler(t)
	holdSeat(t, handler, "inception", "C3", "alice-1")

	rec := doRequest(t, handler, http.MethodDelete,
		"/api/v1/movies/inception/seats/C3/hold", nil,
		map[string]string{"X-User-ID": "bob-22"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestValidationErrors(t *testing.T) {
	handler := newTestHandler(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
		status int
		code   string
	}{
		{
			name:   "invalid user",
			method: http.MethodPost,
			path:   "/api/v1/movies/inception/seats/A1/hold",
			body:   map[string]string{"user_id": "x"},
			status: http.StatusBadRequest,
			code:   "invalid_user_id",
		},
		{
			name:   "invalid seat",
			method: http.MethodPost,
			path:   "/api/v1/movies/inception/seats/Z9/hold",
			body:   map[string]string{"user_id": "alice-1"},
			status: http.StatusBadRequest,
			code:   "invalid_seat_id",
		},
		{
			name:   "unknown movie",
			method: http.MethodPost,
			path:   "/api/v1/movies/nope/seats/A1/hold",
			body:   map[string]string{"user_id": "alice-1"},
			status: http.StatusNotFound,
			code:   "movie_not_found",
		},
		{
			name:   "malformed json",
			method: http.MethodPost,
			path:   "/api/v1/movies/inception/seats/A1/hold",
			body:   map[string]any{"user_id": 42},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, handler, tc.method, tc.path, tc.body, nil)
			if rec.Code != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, rec.Code, rec.Body.String())
			}
			payload := decode[struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}](t, rec)
			if payload.Error.Code != tc.code {
				t.Fatalf("expected code %q, got %q", tc.code, payload.Error.Code)
			}
		})
	}
}

func TestHealthReadinessAndNotFound(t *testing.T) {
	handler := newTestHandler(t)

	for _, path := range []string{"/healthz", "/readyz", "/api/v1/health", "/api/v1/ready"} {
		rec := doRequest(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
	}

	rec := doRequest(t, handler, http.MethodGet, "/api/v1/nope", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON 404, got %q", contentType)
	}

	rec = doRequest(t, handler, http.MethodPut, "/api/v1/movies", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	payload := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if payload.Error.Code != "method_not_allowed" {
		t.Fatalf("unexpected code: %q", payload.Error.Code)
	}
}

func TestRequestIDAndCORS(t *testing.T) {
	handler := newTestHandler(t)

	rec := doRequest(t, handler, http.MethodGet, "/healthz", nil,
		map[string]string{"X-Request-ID": "trace-123"})
	if got := rec.Header().Get("X-Request-ID"); got != "trace-123" {
		t.Fatalf("expected trace-123, got %q", got)
	}

	rec = doRequest(t, handler, http.MethodOptions, "/api/v1/movies", nil,
		map[string]string{"Origin": "https://resume.example"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://resume.example" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Fatalf("admin authorization header missing from CORS allow-list: %q", got)
	}

	rec = doRequest(t, handler, http.MethodGet, "/healthz", nil,
		map[string]string{"Origin": "https://resume.example"})
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected CORS methods header")
	}
}

func TestBookingsEndpoint(t *testing.T) {
	handler := newTestHandler(t)
	holdSeat(t, handler, "inception", "A1", "alice-1")
	holdSeat(t, handler, "inception", "A2", "bob-22")

	rec := doRequest(t, handler, http.MethodGet, "/api/v1/movies/inception/bookings", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	payload := decode[struct {
		Bookings []domain.Booking `json:"bookings"`
	}](t, rec)
	if len(payload.Bookings) != 2 {
		t.Fatalf("expected 2 bookings, got %d", len(payload.Bookings))
	}
}

func adminMoviePayload() map[string]any {
	return map[string]any{
		"id":              "My Movie",
		"title":           "My Movie",
		"description":     "An admin-created title.",
		"genre":           "Drama",
		"rating":          "PG",
		"runtime_minutes": 101,
		"rows":            3,
		"seats_per_row":   4,
	}
}

func adminHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestAdminCreateMovieRequiresToken(t *testing.T) {
	handler := newTestHandler(t)

	for _, headers := range []map[string]string{
		nil,
		adminHeaders("wrong-token"),
		{"Authorization": "Basic " + testAdminToken},
	} {
		rec := doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", adminMoviePayload(), headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for headers %v, got %d (%s)", headers, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="admin"` {
			t.Fatalf("unexpected WWW-Authenticate: %q", got)
		}
		payload := decode[struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}](t, rec)
		if payload.Error.Code != "unauthorized" {
			t.Fatalf("unexpected error code: %q", payload.Error.Code)
		}
	}
}

func TestAdminCreateMovie(t *testing.T) {
	handler := newTestHandler(t)

	rec := doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", adminMoviePayload(), adminHeaders(testAdminToken))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/api/v1/movies/my-movie" {
		t.Fatalf("unexpected Location: %q", got)
	}

	created := decode[domain.Movie](t, rec)
	if created.ID != "my-movie" || created.Title != "My Movie" {
		t.Fatalf("unexpected created movie: %+v", created)
	}
	if created.Rows != 3 || created.SeatsPerRow != 4 {
		t.Fatalf("unexpected room size: %+v", created)
	}

	// The new title must be readable and bookable.
	rec = doRequest(t, handler, http.MethodGet, "/api/v1/movies/my-movie", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get created movie: expected 200, got %d", rec.Code)
	}

	rec = doRequest(t, handler, http.MethodGet, "/api/v1/movies/my-movie/seats", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("seat map: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	seatMap := decode[domain.SeatMap](t, rec)
	if seatMap.Stats.Total != 12 {
		t.Fatalf("expected 12 seats, got %d", seatMap.Stats.Total)
	}

	// It must also appear in the catalogue.
	rec = doRequest(t, handler, http.MethodGet, "/api/v1/movies", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list movies: expected 200, got %d", rec.Code)
	}
	payload := decode[struct {
		Movies []domain.Movie `json:"movies"`
	}](t, rec)
	found := false
	for _, movie := range payload.Movies {
		if movie.ID == "my-movie" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("created movie missing from catalogue")
	}
}

func TestAdminCreateMovieValidation(t *testing.T) {
	handler := newTestHandler(t)

	duplicate := adminMoviePayload()
	duplicate["id"] = "inception"
	rec := doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", duplicate, adminHeaders(testAdminToken))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for seed collision, got %d (%s)", rec.Code, rec.Body.String())
	}
	if payload := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec); payload.Error.Code != "movie_exists" {
		t.Fatalf("unexpected error code: %q", payload.Error.Code)
	}

	invalid := adminMoviePayload()
	invalid["id"] = "!"
	rec = doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", invalid, adminHeaders(testAdminToken))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d (%s)", rec.Code, rec.Body.String())
	}

	invalid = adminMoviePayload()
	invalid["rows"] = 0
	rec = doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", invalid, adminHeaders(testAdminToken))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid rows, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Create once, then collide on the same id.
	rec = doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", adminMoviePayload(), adminHeaders(testAdminToken))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", adminMoviePayload(), adminHeaders(testAdminToken))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminCreateMovieDisabledWithoutToken(t *testing.T) {
	handler := newTestHandlerWithToken(t, "")

	rec := doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies", adminMoviePayload(), adminHeaders("anything"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	payload := decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if payload.Error.Code != "admin_disabled" {
		t.Fatalf("unexpected error code: %q", payload.Error.Code)
	}
}

func TestAdminCreateMovieRejectsMalformedBody(t *testing.T) {
	handler := newTestHandler(t)

	rec := doRequest(t, handler, http.MethodPost, "/api/v1/admin/movies",
		map[string]any{"id": "my-movie", "unknown": true}, adminHeaders(testAdminToken))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}
