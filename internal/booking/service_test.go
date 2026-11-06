package booking_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/adapters/memory"
	"github.com/abdullahsamir74/cinema-booking/internal/booking"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

func newService(t *testing.T, ttl time.Duration, clock func() time.Time) (*booking.Service, *memory.Store) {
	t.Helper()
	store := memory.NewWithClock(clock)
	svc := booking.NewService(store, memory.NewCatalog(), ttl, booking.WithClock(clock))
	return svc, store
}

func TestHoldAndSeatMap(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	held, err := svc.Hold(ctx, "inception", "a1", "alice-1")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if held.Status != domain.StatusHeld {
		t.Fatalf("expected held status, got %q", held.Status)
	}
	if held.SeatID != "A1" || held.MovieID != "inception" {
		t.Fatalf("unexpected booking: %+v", held)
	}
	if held.ID == "" || held.ExpiresAt.IsZero() {
		t.Fatalf("expected an id and expiry, got %+v", held)
	}

	seatMap, err := svc.SeatMap(ctx, "inception")
	if err != nil {
		t.Fatalf("seat map: %v", err)
	}
	if seatMap.Stats.Total != 60 {
		t.Fatalf("expected 60 seats, got %d", seatMap.Stats.Total)
	}
	if seatMap.Stats.Held != 1 || seatMap.Stats.Available != 59 {
		t.Fatalf("unexpected stats: %+v", seatMap.Stats)
	}

	var seat domain.Seat
	for _, candidate := range seatMap.Seats {
		if candidate.SeatID == "A1" {
			seat = candidate
			break
		}
	}
	if seat.Status != domain.SeatHeld || seat.HeldBy != "alice-1" {
		t.Fatalf("unexpected seat: %+v", seat)
	}
}

func TestConcurrentHoldsExactlyOneWins(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	const goroutines = 500
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		conflicts atomic.Int64
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, err := svc.Hold(ctx, "dune-part-two", "C4", "user-"+string(rune('A'+n%26)))
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, domain.ErrSeatUnavailable):
				conflicts.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("expected exactly 1 successful hold, got %d", got)
	}
	if got := conflicts.Load(); got != goroutines-1 {
		t.Fatalf("expected %d conflicts, got %d", goroutines-1, got)
	}
}

func TestConfirmIsFinal(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	if _, err := svc.Hold(ctx, "inception", "B2", "alice-1"); err != nil {
		t.Fatalf("hold: %v", err)
	}

	confirmed, err := svc.Confirm(ctx, "inception", "B2", "alice-1")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != domain.StatusConfirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("unexpected confirmed booking: %+v", confirmed)
	}
	if !confirmed.ExpiresAt.IsZero() {
		t.Fatalf("confirmed booking should not expire, got %s", confirmed.ExpiresAt)
	}

	if err := svc.Release(ctx, "inception", "B2", "alice-1"); !errors.Is(err, domain.ErrBookingConfirmed) {
		t.Fatalf("expected ErrBookingConfirmed, got %v", err)
	}
	if _, err := svc.Hold(ctx, "inception", "B2", "bob-22"); !errors.Is(err, domain.ErrSeatUnavailable) {
		t.Fatalf("expected ErrSeatUnavailable for another user, got %v", err)
	}

	seatMap, _ := svc.SeatMap(ctx, "inception")
	if seatMap.Stats.Confirmed != 1 || seatMap.Stats.Available != 59 {
		t.Fatalf("unexpected stats: %+v", seatMap.Stats)
	}
}

func TestReleaseFreesSeat(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	if _, err := svc.Hold(ctx, "inception", "C3", "alice-1"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if err := svc.Release(ctx, "inception", "C3", "alice-1"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, err := svc.Hold(ctx, "inception", "C3", "bob-22"); err != nil {
		t.Fatalf("expected bob to take the released seat: %v", err)
	}
}

func TestCannotActOnAnotherUsersHold(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	if _, err := svc.Hold(ctx, "inception", "D4", "alice-1"); err != nil {
		t.Fatalf("hold: %v", err)
	}

	if _, err := svc.Confirm(ctx, "inception", "D4", "bob-22"); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner on confirm, got %v", err)
	}
	if err := svc.Release(ctx, "inception", "D4", "bob-22"); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner on release, got %v", err)
	}
}

func TestHoldExpires(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc, _ := newService(t, time.Minute, clock)
	ctx := context.Background()

	if _, err := svc.Hold(ctx, "inception", "E5", "alice-1"); err != nil {
		t.Fatalf("hold: %v", err)
	}

	now = now.Add(30 * time.Second)
	seatMap, _ := svc.SeatMap(ctx, "inception")
	if seatMap.Stats.Held != 1 {
		t.Fatalf("hold should still be active at 30s: %+v", seatMap.Stats)
	}

	now = now.Add(31 * time.Second)
	seatMap, _ = svc.SeatMap(ctx, "inception")
	if seatMap.Stats.Held != 0 || seatMap.Stats.Available != 60 {
		t.Fatalf("hold should be expired: %+v", seatMap.Stats)
	}

	if _, err := svc.Hold(ctx, "inception", "E5", "bob-22"); err != nil {
		t.Fatalf("expected expired seat to be reusable: %v", err)
	}
}

func TestValidation(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	cases := []struct {
		name    string
		movieID string
		seatID  string
		userID  string
		want    error
	}{
		{"unknown movie", "nope", "A1", "alice-1", domain.ErrMovieNotFound},
		{"invalid user", "inception", "A1", "x", domain.ErrInvalidUserID},
		{"bad format", "inception", "1A", "alice-1", domain.ErrInvalidSeatID},
		{"row out of range", "inception", "Z1", "alice-1", domain.ErrInvalidSeatID},
		{"number out of range", "inception", "A99", "alice-1", domain.ErrInvalidSeatID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Hold(ctx, tc.movieID, tc.seatID, tc.userID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestCreateMovie(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	created, err := svc.CreateMovie(ctx, domain.Movie{
		ID:             "  My Movie ",
		Title:          "  My Movie  ",
		Description:    "A brand new title.",
		Genre:          "Drama",
		Rating:         "PG",
		RuntimeMinutes: 95,
		Rows:           3,
		SeatsPerRow:    5,
	})
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	if created.ID != "my-movie" || created.Title != "My Movie" {
		t.Fatalf("expected normalized movie, got %+v", created)
	}

	got, err := svc.GetMovie(ctx, "MY-MOVIE")
	if err != nil || got.ID != "my-movie" {
		t.Fatalf("get created movie: %+v err=%v", got, err)
	}

	seatMap, err := svc.SeatMap(ctx, "my-movie")
	if err != nil {
		t.Fatalf("seat map: %v", err)
	}
	if seatMap.Stats.Total != 15 {
		t.Fatalf("expected 15 seats, got %d", seatMap.Stats.Total)
	}

	movies, err := svc.ListMovies(ctx)
	if err != nil {
		t.Fatalf("list movies: %v", err)
	}
	if len(movies) < 6 {
		t.Fatalf("expected the new movie in the catalogue, got %d movies", len(movies))
	}

	if _, err := svc.CreateMovie(ctx, created); !errors.Is(err, domain.ErrMovieExists) {
		t.Fatalf("expected ErrMovieExists, got %v", err)
	}

	invalid := created
	invalid.ID = "x"
	if _, err := svc.CreateMovie(ctx, invalid); !errors.Is(err, domain.ErrInvalidMovie) {
		t.Fatalf("expected ErrInvalidMovie, got %v", err)
	}
}

func TestListBookingsOnlyForMovie(t *testing.T) {
	svc, _ := newService(t, time.Minute, time.Now)
	ctx := context.Background()

	if _, err := svc.Hold(ctx, "inception", "A1", "alice-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Hold(ctx, "dune-part-two", "A1", "bob-22"); err != nil {
		t.Fatal(err)
	}

	bookings, err := svc.ListBookings(ctx, "inception")
	if err != nil {
		t.Fatal(err)
	}
	if len(bookings) != 1 || bookings[0].MovieID != "inception" {
		t.Fatalf("unexpected bookings: %+v", bookings)
	}
}
