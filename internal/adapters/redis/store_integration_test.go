//go:build integration

package redis_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redisadapter "github.com/abdullahsamir74/cinema-booking/internal/adapters/redis"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

func newStore(t *testing.T) (*redisadapter.Store, *goredis.Client, string) {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	client, err := redisadapter.NewClient(context.Background(), redisadapter.Options{Addr: addr})
	if err != nil {
		t.Fatalf("connect redis (%s): %v\nstart it with: docker compose up -d redis", addr, err)
	}

	prefix := "it-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		ctx := context.Background()
		iter := client.Scan(ctx, 0, "booking:"+prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
		_ = client.Close()
	})

	return redisadapter.NewStore(client), client, prefix
}

func booking(prefix, seat, user string, ttl time.Duration) domain.Booking {
	now := time.Now().UTC()
	return domain.Booking{
		ID:        uuid.NewString(),
		MovieID:   prefix,
		SeatID:    seat,
		UserID:    user,
		Status:    domain.StatusHeld,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

func TestRedisHoldConfirmRelease(t *testing.T) {
	store, _, prefix := newStore(t)
	ctx := context.Background()

	held, err := store.Hold(ctx, booking(prefix, "A1", "alice-1", time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if held.SeatID != "A1" {
		t.Fatalf("unexpected hold: %+v", held)
	}

	if _, err := store.Hold(ctx, booking(prefix, "A1", "bob-22", time.Minute), time.Minute); !errors.Is(err, domain.ErrSeatUnavailable) {
		t.Fatalf("expected ErrSeatUnavailable, got %v", err)
	}

	if _, err := store.Confirm(ctx, prefix, "A1", "bob-22", time.Now()); !errors.Is(err, domain.ErrNotHoldOwner) {
		t.Fatalf("expected ErrNotHoldOwner, got %v", err)
	}

	confirmed, err := store.Confirm(ctx, prefix, "A1", "alice-1", time.Now())
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if confirmed.Status != domain.StatusConfirmed || confirmed.ConfirmedAt == nil {
		t.Fatalf("unexpected confirmed booking: %+v", confirmed)
	}

	if err := store.Release(ctx, prefix, "A1", "alice-1"); !errors.Is(err, domain.ErrBookingConfirmed) {
		t.Fatalf("expected ErrBookingConfirmed, got %v", err)
	}

	list, err := store.ListByMovie(ctx, prefix)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != domain.StatusConfirmed {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestRedisReleaseAndTTL(t *testing.T) {
	store, _, prefix := newStore(t)
	ctx := context.Background()

	if _, err := store.Hold(ctx, booking(prefix, "B2", "alice-1", time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(ctx, prefix, "B2", "alice-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok, err := store.Get(ctx, prefix, "B2"); err != nil || ok {
		t.Fatalf("expected seat to be gone, ok=%v err=%v", ok, err)
	}

	if _, err := store.Hold(ctx, booking(prefix, "B3", "alice-1", 40*time.Millisecond), 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, ok, err := store.Get(ctx, prefix, "B3"); err != nil || ok {
		t.Fatalf("expected hold to expire, ok=%v err=%v", ok, err)
	}
	if _, err := store.Hold(ctx, booking(prefix, "B3", "bob-22", time.Minute), time.Minute); err != nil {
		t.Fatalf("expected expired seat to be reusable: %v", err)
	}
}

func TestRedisConcurrentHoldsExactlyOneWins(t *testing.T) {
	store, _, prefix := newStore(t)
	ctx := context.Background()

	const goroutines = 100
	var (
		wg        sync.WaitGroup
		successes atomic.Int64
		conflicts atomic.Int64
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_, err := store.Hold(ctx, booking(prefix, "C4", fmt.Sprintf("user-%d", n), time.Minute), time.Minute)
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
