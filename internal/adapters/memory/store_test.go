package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/adapters/memory"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

func heldBooking(movieID, seatID, userID string, ttl time.Duration) domain.Booking {
	now := time.Now().UTC()
	return domain.Booking{
		ID:        "booking-" + seatID,
		MovieID:   movieID,
		SeatID:    seatID,
		UserID:    userID,
		Status:    domain.StatusHeld,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

func TestHoldConflictAndGet(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	booking := heldBooking("inception", "A1", "alice-1", time.Minute)
	if _, err := store.Hold(ctx, booking, time.Minute); err != nil {
		t.Fatalf("hold: %v", err)
	}

	if _, err := store.Hold(ctx, heldBooking("inception", "a1", "bob-22", time.Minute), time.Minute); !errors.Is(err, domain.ErrSeatUnavailable) {
		t.Fatalf("expected ErrSeatUnavailable, got %v", err)
	}

	got, ok, err := store.Get(ctx, "inception", "A1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.UserID != "alice-1" {
		t.Fatalf("unexpected booking: %+v", got)
	}

	if _, ok, _ := store.Get(ctx, "inception", "B1"); ok {
		t.Fatal("expected B1 to be free")
	}
}

func TestConfirmAndReleaseRules(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	if _, err := store.Hold(ctx, heldBooking("inception", "B2", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Confirm(ctx, "inception", "B2", "bob-22", time.Now()); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner, got %v", err)
	}
	if err := store.Release(ctx, "inception", "B2", "bob-22"); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner, got %v", err)
	}

	confirmed, err := store.Confirm(ctx, "inception", "B2", "alice-1", time.Now())
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != domain.StatusConfirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("unexpected confirmed booking: %+v", confirmed)
	}
	if err := store.Release(ctx, "inception", "B2", "alice-1"); !errors.Is(err, domain.ErrBookingConfirmed) {
		t.Fatalf("expected ErrBookingConfirmed, got %v", err)
	}

	// Confirming again is idempotent.
	again, err := store.Confirm(ctx, "inception", "B2", "alice-1", time.Now())
	if err != nil || again.Status != domain.StatusConfirmed {
		t.Fatalf("idempotent confirm: %+v err=%v", again, err)
	}
}

func TestReleaseReturnsSeat(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	if _, err := store.Hold(ctx, heldBooking("inception", "C3", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(ctx, "inception", "C3", "alice-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := store.Release(ctx, "inception", "C3", "alice-1"); !errors.Is(err, domain.ErrHoldNotFound) {
		t.Fatalf("expected ErrHoldNotFound on second release, got %v", err)
	}
	if _, err := store.Hold(ctx, heldBooking("inception", "C3", "bob-22", time.Minute), time.Minute); err != nil {
		t.Fatalf("expected seat to be reusable: %v", err)
	}
}

func TestExpiryWithClock(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := memory.NewWithClock(func() time.Time { return now })
	ctx := context.Background()

	booking := heldBooking("inception", "D4", "alice-1", time.Minute)
	booking.CreatedAt = now
	booking.ExpiresAt = now.Add(time.Minute)
	if _, err := store.Hold(ctx, booking, time.Minute); err != nil {
		t.Fatal(err)
	}

	now = now.Add(30 * time.Second)
	if _, ok, _ := store.Get(ctx, "inception", "D4"); !ok {
		t.Fatal("hold should still be active")
	}

	now = now.Add(31 * time.Second)
	if _, ok, _ := store.Get(ctx, "inception", "D4"); ok {
		t.Fatal("hold should be expired")
	}
	if _, err := store.Confirm(ctx, "inception", "D4", "alice-1", now); !errors.Is(err, domain.ErrHoldNotFound) {
		t.Fatalf("expected ErrHoldNotFound, got %v", err)
	}

	if _, err := store.Hold(ctx, heldBooking("inception", "D4", "bob-22", time.Minute), time.Minute); err != nil {
		t.Fatalf("expired seat should be reusable: %v", err)
	}
}

func TestListByMovieSortedAndScoped(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	for _, seat := range []string{"B2", "A1", "A2"} {
		if _, err := store.Hold(ctx, heldBooking("inception", seat, "alice-1", time.Minute), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Hold(ctx, heldBooking("dune-part-two", "A1", "bob-22", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListByMovie(ctx, "inception")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 bookings, got %d", len(list))
	}
	if list[0].SeatID != "A1" || list[1].SeatID != "A2" || list[2].SeatID != "B2" {
		t.Fatalf("unexpected order: %+v", list)
	}
}

func TestContextCancellationAndPing(t *testing.T) {
	store := memory.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.Hold(ctx, heldBooking("inception", "A1", "alice-1", time.Minute), time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if _, err := store.ListByMovie(ctx, "inception"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
