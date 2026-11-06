package catalog_test

import (
	"testing"

	"github.com/abdullahsamir74/cinema-booking/internal/catalog"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

func TestSeed(t *testing.T) {
	movies := catalog.Seed()
	if len(movies) < 3 {
		t.Fatalf("expected a populated catalogue, got %d", len(movies))
	}

	for _, movie := range movies {
		if err := movie.Validate(); err != nil {
			t.Fatalf("seed movie %q is invalid: %v", movie.ID, err)
		}
	}

	// Seed must return a copy so callers cannot mutate shared data.
	movies[0].Title = "changed"
	if catalog.Seed()[0].Title == "changed" {
		t.Fatal("Seed returned a shared slice")
	}
}

func TestFindSeed(t *testing.T) {
	movie, ok := catalog.FindSeed("INCEPTION")
	if !ok || movie.ID != "inception" {
		t.Fatalf("case-insensitive lookup failed: %+v ok=%v", movie, ok)
	}
	if _, ok := catalog.FindSeed("missing"); ok {
		t.Fatal("expected missing movie to be absent")
	}
}

func TestMerge(t *testing.T) {
	seed := []domain.Movie{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	custom := []domain.Movie{
		{ID: "c", Title: "C"},
		{ID: "a", Title: "Duplicate A"},
		{ID: "d", Title: "D"},
	}

	merged := catalog.Merge(seed, custom)
	if len(merged) != 4 {
		t.Fatalf("expected 4 movies, got %d: %+v", len(merged), merged)
	}
	if merged[0].ID != "a" || merged[1].ID != "b" {
		t.Fatalf("seed order not preserved: %+v", merged)
	}
	if merged[2].ID != "c" || merged[3].ID != "d" {
		t.Fatalf("custom movies not sorted: %+v", merged)
	}
}
