package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	redisadapter "github.com/abdullahsamir74/cinema-booking/internal/adapters/redis"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newMovieStore(t *testing.T) (*redisadapter.MovieStore, *miniredis.Miniredis) {
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
	return redisadapter.NewMovieStore(client), server
}

func customMovie(id, title string) domain.Movie {
	return domain.Movie{
		ID:             id,
		Title:          title,
		Description:    "Created by an admin.",
		Genre:          "Drama",
		Rating:         "PG",
		RuntimeMinutes: 100,
		Rows:           4,
		SeatsPerRow:    6,
	}
}

func TestMovieStoreCreateAndGet(t *testing.T) {
	store, _ := newMovieStore(t)
	ctx := context.Background()

	if err := store.Create(ctx, customMovie("my-movie", "My Movie")); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, ok, err := store.Get(ctx, "MY-MOVIE")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Title != "My Movie" || got.Rows != 4 {
		t.Fatalf("unexpected movie: %+v", got)
	}

	seed, ok, err := store.Get(ctx, "inception")
	if err != nil || !ok {
		t.Fatalf("seed fallback: ok=%v err=%v", ok, err)
	}
	if seed.ID != "inception" {
		t.Fatalf("unexpected seed movie: %+v", seed)
	}

	if _, ok, err := store.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("expected missing movie, ok=%v err=%v", ok, err)
	}
}

func TestMovieStoreListMergesSeedAndCustom(t *testing.T) {
	store, _ := newMovieStore(t)
	ctx := context.Background()

	if err := store.Create(ctx, customMovie("zeta", "Zeta")); err != nil {
		t.Fatalf("create zeta: %v", err)
	}
	if err := store.Create(ctx, customMovie("alpha", "Alpha")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}

	movies, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(movies) != 7 {
		t.Fatalf("expected 5 seed + 2 custom movies, got %d", len(movies))
	}
	if movies[0].ID != "inception" {
		t.Fatalf("expected seed order first, got %q", movies[0].ID)
	}
	if movies[5].ID != "alpha" || movies[6].ID != "zeta" {
		t.Fatalf("expected custom movies sorted by title, got %+v", movies[5:])
	}
}

func TestMovieStoreRejectsDuplicates(t *testing.T) {
	store, _ := newMovieStore(t)
	ctx := context.Background()

	if err := store.Create(ctx, customMovie("my-movie", "My Movie")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Create(ctx, customMovie("my-movie", "Another")); !errors.Is(err, domain.ErrMovieExists) {
		t.Fatalf("expected ErrMovieExists for duplicate, got %v", err)
	}
	if err := store.Create(ctx, customMovie("inception", "Inception")); !errors.Is(err, domain.ErrMovieExists) {
		t.Fatalf("expected ErrMovieExists for seed collision, got %v", err)
	}
}

func TestMovieStorePersistsAcrossInstances(t *testing.T) {
	store, server := newMovieStore(t)
	ctx := context.Background()

	if err := store.Create(ctx, customMovie("my-movie", "My Movie")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A new store over the same Redis data must still see the movie.
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	reopened := redisadapter.NewMovieStore(client)

	got, ok, err := reopened.Get(ctx, "my-movie")
	if err != nil || !ok {
		t.Fatalf("get after reopen: ok=%v err=%v", ok, err)
	}
	if got.Title != "My Movie" {
		t.Fatalf("unexpected movie after reopen: %+v", got)
	}
}
