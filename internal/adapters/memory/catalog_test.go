package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdullahsamir74/cinema-booking/internal/adapters/memory"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

func TestCatalogSeededAndCaseInsensitiveGet(t *testing.T) {
	catalog := memory.NewCatalog()
	ctx := context.Background()

	movies, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(movies) < 3 {
		t.Fatalf("expected a populated catalogue, got %d movies", len(movies))
	}

	movie, ok, err := catalog.Get(ctx, "INCEPTION")
	if err != nil || !ok {
		t.Fatalf("get inception: ok=%v err=%v", ok, err)
	}
	if movie.Title != "Inception" {
		t.Fatalf("unexpected movie: %+v", movie)
	}

	if _, ok, err := catalog.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("expected missing movie, ok=%v err=%v", ok, err)
	}
}

func TestCatalogCreate(t *testing.T) {
	catalog := memory.NewCatalog()
	ctx := context.Background()

	movie := domain.Movie{
		ID:             "My Movie",
		Title:          "My Movie",
		Description:    "A custom title.",
		Genre:          "Drama",
		Rating:         "PG",
		RuntimeMinutes: 95,
		Rows:           3,
		SeatsPerRow:    5,
	}
	if err := catalog.Create(ctx, movie); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, ok, err := catalog.Get(ctx, "my movie")
	if err != nil || !ok {
		t.Fatalf("get created movie: ok=%v err=%v", ok, err)
	}
	if got.ID != "my movie" || got.Title != "My Movie" {
		t.Fatalf("unexpected created movie: %+v", got)
	}

	if err := catalog.Create(ctx, movie); !errors.Is(err, domain.ErrMovieExists) {
		t.Fatalf("expected ErrMovieExists for duplicate, got %v", err)
	}

	seed := movie
	seed.ID = "inception"
	if err := catalog.Create(ctx, seed); !errors.Is(err, domain.ErrMovieExists) {
		t.Fatalf("expected ErrMovieExists for seed collision, got %v", err)
	}

	movies, err := catalog.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, m := range movies {
		if m.ID == "my movie" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("created movie missing from list")
	}
}

func TestCatalogHonoursContext(t *testing.T) {
	catalog := memory.NewCatalog()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := catalog.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from List, got %v", err)
	}
	if _, _, err := catalog.Get(ctx, "inception"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from Get, got %v", err)
	}
	if err := catalog.Create(ctx, domain.Movie{ID: "new"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from Create, got %v", err)
	}
}
