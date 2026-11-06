package redis_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redisadapter "github.com/abdullahsamir74/cinema-booking/internal/adapters/redis"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newMiniStore(t *testing.T) (*redisadapter.Store, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{
		Addr:         server.Addr(),
		MaxRetries:   -1,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	t.Cleanup(func() { _ = client.Close() })
	return redisadapter.NewStore(client), server
}

func miniBooking(seat, user string, ttl time.Duration) domain.Booking {
	now := time.Now().UTC()
	return domain.Booking{
		ID:        "id-" + seat,
		MovieID:   "inception",
		SeatID:    seat,
		UserID:    user,
		Status:    domain.StatusHeld,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

func TestMiniRedisHoldConfirmRelease(t *testing.T) {
	store, _ := newMiniStore(t)
	ctx := context.Background()

	if _, err := store.Hold(ctx, miniBooking("A1", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if _, err := store.Hold(ctx, miniBooking("A1", "bob-22", time.Minute), time.Minute); !errors.Is(err, domain.ErrSeatUnavailable) {
		t.Fatalf("expected ErrSeatUnavailable, got %v", err)
	}

	if _, err := store.Confirm(ctx, "inception", "A1", "bob-22", time.Now()); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner, got %v", err)
	}

	confirmed, err := store.Confirm(ctx, "inception", "A1", "alice-1", time.Now())
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != domain.StatusConfirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("unexpected confirmed booking: %+v", confirmed)
	}

	if err := store.Release(ctx, "inception", "A1", "alice-1"); !errors.Is(err, domain.ErrBookingConfirmed) {
		t.Fatalf("expected ErrBookingConfirmed, got %v", err)
	}
}

func TestMiniRedisReleaseAndGet(t *testing.T) {
	store, _ := newMiniStore(t)
	ctx := context.Background()

	if _, err := store.Hold(ctx, miniBooking("B2", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Get(ctx, "inception", "B2"); err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}

	if err := store.Release(ctx, "inception", "B2", "alice-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok, err := store.Get(ctx, "inception", "B2"); err != nil || ok {
		t.Fatalf("expected empty seat, ok=%v err=%v", ok, err)
	}
	if err := store.Release(ctx, "inception", "B2", "alice-1"); !errors.Is(err, domain.ErrHoldNotFound) {
		t.Fatalf("expected ErrHoldNotFound, got %v", err)
	}
}

func TestMiniRedisTTLAndList(t *testing.T) {
	store, server := newMiniStore(t)
	ctx := context.Background()

	if _, err := store.Hold(ctx, miniBooking("C3", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Hold(ctx, miniBooking("C4", "bob-22", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListByMovie(ctx, "inception")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].SeatID != "C3" || list[1].SeatID != "C4" {
		t.Fatalf("unexpected list: %+v", list)
	}

	server.FastForward(61 * time.Second)
	list, err = store.ListByMovie(ctx, "inception")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected expired holds to disappear, got %+v", list)
	}
	if _, ok, _ := store.Get(ctx, "inception", "C3"); ok {
		t.Fatal("expected expired seat to be gone")
	}
}

func TestMiniRedisConcurrentHolds(t *testing.T) {
	store, _ := newMiniStore(t)
	ctx := context.Background()

	const goroutines = 50
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		conflicts atomic.Int64
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, err := store.Hold(ctx, miniBooking("D4", "user-"+string(rune('A'+n%26)), time.Minute), time.Minute)
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
		t.Fatalf("expected exactly 1 winner, got %d", got)
	}
	if got := conflicts.Load(); got != goroutines-1 {
		t.Fatalf("expected %d conflicts, got %d", goroutines-1, got)
	}
}

func TestMiniRedisPing(t *testing.T) {
	store, server := newMiniStore(t)
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	server.Close()
	if err := store.Ping(context.Background()); err == nil {
		t.Fatal("expected ping to fail after server shutdown")
	}
}
