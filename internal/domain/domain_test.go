package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

func TestParseSeat(t *testing.T) {
	movie := domain.Movie{ID: "test", Rows: 3, SeatsPerRow: 5}

	cases := []struct {
		seat      string
		wantRow   int
		wantNum   int
		wantError bool
	}{
		{"A1", 0, 1, false},
		{"a5", 0, 5, false},
		{" B3 ", 1, 3, false},
		{"C5", 2, 5, false},
		{"A0", 0, 0, true},
		{"A6", 0, 0, true},
		{"D1", 0, 0, true},
		{"1A", 0, 0, true},
		{"AA1", 0, 0, true},
		{"", 0, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.seat, func(t *testing.T) {
			row, number, err := domain.ParseSeat(movie, tc.seat)
			if tc.wantError {
				if !errors.Is(err, domain.ErrInvalidSeatID) {
					t.Fatalf("expected ErrInvalidSeatID, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if row != tc.wantRow || number != tc.wantNum {
				t.Fatalf("expected (%d,%d), got (%d,%d)", tc.wantRow, tc.wantNum, row, number)
			}
		})
	}
}

func TestSeatIDs(t *testing.T) {
	if got := domain.RowLabel(0); got != "A" {
		t.Fatalf("expected A, got %q", got)
	}
	if got := domain.RowLabel(25); got != "Z" {
		t.Fatalf("expected Z, got %q", got)
	}
	if got := domain.SeatID(1, 4); got != "B4" {
		t.Fatalf("expected B4, got %q", got)
	}
	if got := domain.NormalizeSeatID(" c12 "); got != "C12" {
		t.Fatalf("expected C12, got %q", got)
	}
}

func TestBookingIsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	held := domain.Booking{Status: domain.StatusHeld, ExpiresAt: now.Add(time.Minute)}
	if held.IsExpired(now) {
		t.Fatal("active hold should not be expired")
	}
	if !held.IsExpired(now.Add(2 * time.Minute)) {
		t.Fatal("hold past expiry should be expired")
	}

	confirmed := domain.Booking{Status: domain.StatusConfirmed, ExpiresAt: now.Add(-time.Hour)}
	if confirmed.IsExpired(now) {
		t.Fatal("confirmed bookings never expire")
	}

	zero := domain.Booking{Status: domain.StatusHeld}
	if zero.IsExpired(now) {
		t.Fatal("a hold without an expiry is treated as active")
	}
}

func TestNormalizeMovieSlug(t *testing.T) {
	movie := domain.NormalizeMovie(domain.Movie{
		ID:          "  My Movie!  ",
		Title:       "  My Movie  ",
		Description: "  A description.  ",
		Genre:       "  Drama ",
		Rating:      " PG-13 ",
	})
	if movie.ID != "my-movie" {
		t.Fatalf("expected my-movie, got %q", movie.ID)
	}
	if movie.Title != "My Movie" || movie.Description != "A description." {
		t.Fatalf("fields not trimmed: %+v", movie)
	}
	if movie.Genre != "Drama" || movie.Rating != "PG-13" {
		t.Fatalf("fields not trimmed: %+v", movie)
	}
}

func TestValidateMovie(t *testing.T) {
	valid := domain.Movie{
		ID:             "my-movie",
		Title:          "My Movie",
		Description:    "A description.",
		Genre:          "Drama",
		Rating:         "PG-13",
		RuntimeMinutes: 120,
		Rows:           5,
		SeatsPerRow:    8,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid movie, got %v", err)
	}

	tests := []struct {
		name  string
		movie domain.Movie
	}{
		{"short id", func() domain.Movie { m := valid; m.ID = "x"; return m }()},
		{"invalid id", func() domain.Movie { m := valid; m.ID = "bad_id"; return m }()},
		{"empty title", func() domain.Movie { m := valid; m.Title = ""; return m }()},
		{"runtime too high", func() domain.Movie { m := valid; m.RuntimeMinutes = 601; return m }()},
		{"no rows", func() domain.Movie { m := valid; m.Rows = 0; return m }()},
		{"too many rows", func() domain.Movie { m := valid; m.Rows = 27; return m }()},
		{"too many seats", func() domain.Movie { m := valid; m.SeatsPerRow = 21; return m }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.movie.Validate(); !errors.Is(err, domain.ErrInvalidMovie) {
				t.Fatalf("expected ErrInvalidMovie, got %v", err)
			}
		})
	}
}
