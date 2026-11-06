// Package catalog defines the movie catalogue port and the default seed
// catalogue. Concrete storage lives in the memory and Redis adapters.
package catalog

import (
	"context"
	"sort"
	"strings"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

// Repository stores the movie catalogue.
type Repository interface {
	List(ctx context.Context) ([]domain.Movie, error)
	Get(ctx context.Context, id string) (domain.Movie, bool, error)
	Create(ctx context.Context, movie domain.Movie) error
}

// Seed returns the built-in catalogue. The slice is a fresh copy, so callers
// cannot mutate the shared seed data.
func Seed() []domain.Movie {
	return []domain.Movie{
		{
			ID:             "inception",
			Title:          "Inception",
			Description:    "A thief who steals corporate secrets through dream-sharing technology is given an inverse task.",
			Genre:          "Sci-Fi / Thriller",
			Rating:         "PG-13",
			RuntimeMinutes: 148,
			Rows:           6,
			SeatsPerRow:    10,
		},
		{
			ID:             "dune-part-two",
			Title:          "Dune: Part Two",
			Description:    "Paul Atreides unites with the Fremen to wage war against the House Harkonnen.",
			Genre:          "Sci-Fi / Adventure",
			Rating:         "PG-13",
			RuntimeMinutes: 166,
			Rows:           5,
			SeatsPerRow:    8,
		},
		{
			ID:             "interstellar",
			Title:          "Interstellar",
			Description:    "Explorers travel through a wormhole in search of a new home for humanity.",
			Genre:          "Sci-Fi / Drama",
			Rating:         "PG-13",
			RuntimeMinutes: 169,
			Rows:           6,
			SeatsPerRow:    9,
		},
		{
			ID:             "the-dark-knight",
			Title:          "The Dark Knight",
			Description:    "Batman faces the Joker, a criminal mastermind determined to plunge Gotham into chaos.",
			Genre:          "Action / Crime",
			Rating:         "PG-13",
			RuntimeMinutes: 152,
			Rows:           5,
			SeatsPerRow:    10,
		},
		{
			ID:             "oppenheimer",
			Title:          "Oppenheimer",
			Description:    "The story of J. Robert Oppenheimer and the creation of the atomic bomb.",
			Genre:          "Biography / Drama",
			Rating:         "R",
			RuntimeMinutes: 180,
			Rows:           4,
			SeatsPerRow:    8,
		},
	}
}

// FindSeed looks up a seed movie by slug, case-insensitively.
func FindSeed(id string) (domain.Movie, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, movie := range Seed() {
		if movie.ID == id {
			return movie, true
		}
	}
	return domain.Movie{}, false
}

// Merge combines seed and custom movies, preserving seed order and appending
// custom titles alphabetically. Seed entries win on duplicate IDs.
func Merge(seed, custom []domain.Movie) []domain.Movie {
	seen := make(map[string]struct{}, len(seed)+len(custom))
	result := make([]domain.Movie, 0, len(seed)+len(custom))

	for _, movie := range seed {
		if _, ok := seen[movie.ID]; ok {
			continue
		}
		seen[movie.ID] = struct{}{}
		result = append(result, movie)
	}

	sort.Slice(custom, func(i, j int) bool {
		return strings.ToLower(custom[i].Title) < strings.ToLower(custom[j].Title)
	})
	for _, movie := range custom {
		if _, ok := seen[movie.ID]; ok {
			continue
		}
		seen[movie.ID] = struct{}{}
		result = append(result, movie)
	}
	return result
}
