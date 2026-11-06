package memory

import (
	"context"
	"strings"
	"sync"

	"github.com/abdullahsamir74/cinema-booking/internal/catalog"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

// Catalog is an in-memory movie catalogue seeded with the default titles.
type Catalog struct {
	mu     sync.RWMutex
	order  []string
	movies map[string]domain.Movie
}

// NewCatalog returns a catalogue seeded with the built-in movies.
func NewCatalog() *Catalog {
	return NewCatalogWithSeed(catalog.Seed())
}

// NewCatalogWithSeed returns a catalogue seeded with the supplied movies.
func NewCatalogWithSeed(seed []domain.Movie) *Catalog {
	c := &Catalog{
		order:  make([]string, 0, len(seed)),
		movies: make(map[string]domain.Movie, len(seed)),
	}
	for _, movie := range seed {
		id := strings.ToLower(movie.ID)
		if _, exists := c.movies[id]; exists {
			continue
		}
		c.order = append(c.order, id)
		c.movies[id] = movie
	}
	return c
}

// List returns movies in insertion order.
func (c *Catalog) List(ctx context.Context) ([]domain.Movie, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]domain.Movie, 0, len(c.order))
	for _, id := range c.order {
		result = append(result, c.movies[id])
	}
	return result, nil
}

// Get finds a movie by slug, case-insensitively.
func (c *Catalog) Get(ctx context.Context, id string) (domain.Movie, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.Movie{}, false, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	movie, ok := c.movies[strings.ToLower(strings.TrimSpace(id))]
	return movie, ok, nil
}

// Create adds a movie. The caller is responsible for validation.
func (c *Catalog) Create(ctx context.Context, movie domain.Movie) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	id := strings.ToLower(movie.ID)
	if _, exists := c.movies[id]; exists {
		return domain.ErrMovieExists
	}

	movie.ID = id
	c.order = append(c.order, id)
	c.movies[id] = movie
	return nil
}
