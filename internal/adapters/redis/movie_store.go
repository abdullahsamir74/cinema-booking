package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/abdullahsamir74/cinema-booking/internal/catalog"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	goredis "github.com/redis/go-redis/v9"
)

const movieKeyPrefix = "movie:"

// MovieStore persists admin-created movies in Redis. The built-in catalogue is
// always merged in, so default titles survive a cache flush.
type MovieStore struct {
	client *goredis.Client
}

// NewMovieStore wraps an existing Redis client.
func NewMovieStore(client *goredis.Client) *MovieStore {
	return &MovieStore{client: client}
}

// List returns seed movies followed by custom movies sorted by title.
func (s *MovieStore) List(ctx context.Context) ([]domain.Movie, error) {
	keys := make([]string, 0, 32)
	iter := s.client.Scan(ctx, 0, movieKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan movies: %w", err)
	}

	custom := make([]domain.Movie, 0, len(keys))
	for start := 0; start < len(keys); start += 100 {
		end := min(start+100, len(keys))
		values, err := s.client.MGet(ctx, keys[start:end]...).Result()
		if err != nil {
			return nil, fmt.Errorf("redis mget movies: %w", err)
		}
		for _, value := range values {
			raw, ok := value.(string)
			if !ok {
				continue
			}
			var movie domain.Movie
			if err := json.Unmarshal([]byte(raw), &movie); err != nil {
				continue
			}
			custom = append(custom, movie)
		}
	}

	return catalog.Merge(catalog.Seed(), custom), nil
}

// Get finds a custom movie first, then falls back to the seed catalogue.
func (s *MovieStore) Get(ctx context.Context, id string) (domain.Movie, bool, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	raw, err := s.client.Get(ctx, movieKey(id)).Bytes()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return domain.Movie{}, false, fmt.Errorf("redis get movie: %w", err)
	}
	if err == nil {
		var movie domain.Movie
		if err := json.Unmarshal(raw, &movie); err != nil {
			return domain.Movie{}, false, fmt.Errorf("decode movie: %w", err)
		}
		return movie, true, nil
	}

	movie, ok := catalog.FindSeed(id)
	return movie, ok, nil
}

// Create persists a new movie if its slug is unused.
func (s *MovieStore) Create(ctx context.Context, movie domain.Movie) error {
	id := strings.ToLower(strings.TrimSpace(movie.ID))
	if _, exists := catalog.FindSeed(id); exists {
		return domain.ErrMovieExists
	}
	movie.ID = id
	payload, err := json.Marshal(movie)
	if err != nil {
		return fmt.Errorf("marshal movie: %w", err)
	}

	ok, err := s.client.SetNX(ctx, movieKey(id), payload, 0).Result()
	if err != nil {
		return fmt.Errorf("redis setnx movie: %w", err)
	}
	if !ok {
		return domain.ErrMovieExists
	}
	return nil
}

func movieKey(id string) string {
	return movieKeyPrefix + id
}
