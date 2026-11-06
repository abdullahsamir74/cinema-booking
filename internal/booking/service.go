package booking

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/catalog"
	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	"github.com/google/uuid"
)

const defaultHoldTTL = 2 * time.Minute

var userIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// Service orchestrates the booking use cases. It depends only on the domain,
// a movie catalogue and the Repository port.
type Service struct {
	repo    Repository
	catalog catalog.Repository
	holdTTL time.Duration
	now     func() time.Time
}

// Option customises the service. It is mainly useful for deterministic tests.
type Option func(*Service)

// WithClock replaces the service clock.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService constructs a booking service.
func NewService(repo Repository, movies catalog.Repository, holdTTL time.Duration, opts ...Option) *Service {
	if holdTTL <= 0 {
		holdTTL = defaultHoldTTL
	}
	s := &Service{
		repo:    repo,
		catalog: movies,
		holdTTL: holdTTL,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListMovies returns every movie in the catalogue.
func (s *Service) ListMovies(ctx context.Context) ([]domain.Movie, error) {
	return s.catalog.List(ctx)
}

// GetMovie returns one movie.
func (s *Service) GetMovie(ctx context.Context, movieID string) (domain.Movie, error) {
	movie, ok, err := s.catalog.Get(ctx, movieID)
	if err != nil {
		return domain.Movie{}, fmt.Errorf("get movie: %w", err)
	}
	if !ok {
		return domain.Movie{}, domain.ErrMovieNotFound
	}
	return movie, nil
}

// CreateMovie validates and adds a movie to the catalogue. It is used by the
// admin API and is intentionally separated from the public read paths.
func (s *Service) CreateMovie(ctx context.Context, movie domain.Movie) (domain.Movie, error) {
	movie = domain.NormalizeMovie(movie)
	if err := movie.Validate(); err != nil {
		return domain.Movie{}, err
	}
	if err := s.catalog.Create(ctx, movie); err != nil {
		return domain.Movie{}, err
	}
	return movie, nil
}

// SeatMap combines the movie's physical layout with current reservations.
func (s *Service) SeatMap(ctx context.Context, movieID string) (domain.SeatMap, error) {
	movie, err := s.GetMovie(ctx, movieID)
	if err != nil {
		return domain.SeatMap{}, err
	}

	bookings, err := s.repo.ListByMovie(ctx, movie.ID)
	if err != nil {
		return domain.SeatMap{}, fmt.Errorf("list bookings: %w", err)
	}

	now := s.now()
	bySeat := make(map[string]domain.Booking, len(bookings))
	for _, b := range bookings {
		if b.IsExpired(now) {
			continue
		}
		bySeat[domain.NormalizeSeatID(b.SeatID)] = b
	}

	seatMap := domain.SeatMap{
		MovieID:     movie.ID,
		Rows:        movie.Rows,
		SeatsPerRow: movie.SeatsPerRow,
		Seats:       make([]domain.Seat, 0, movie.Rows*movie.SeatsPerRow),
	}
	seatMap.Stats.Total = movie.Rows * movie.SeatsPerRow

	for row := 0; row < movie.Rows; row++ {
		for number := 1; number <= movie.SeatsPerRow; number++ {
			id := domain.SeatID(row, number)
			seat := domain.Seat{
				SeatID: id,
				Row:    domain.RowLabel(row),
				Number: number,
				Status: domain.SeatAvailable,
			}

			if b, ok := bySeat[id]; ok {
				seat.BookingID = b.ID
				expires := b.ExpiresAt
				if b.Status == domain.StatusConfirmed {
					seat.Status = domain.SeatConfirmed
					seatMap.Stats.Confirmed++
				} else {
					seat.Status = domain.SeatHeld
					seat.HeldBy = b.UserID
					seat.ExpiresAt = &expires
					seatMap.Stats.Held++
				}
			} else {
				seatMap.Stats.Available++
			}
			seatMap.Seats = append(seatMap.Seats, seat)
		}
	}

	return seatMap, nil
}

// Hold atomically reserves a seat for a user for the configured TTL.
func (s *Service) Hold(ctx context.Context, movieID, seatID, userID string) (domain.Booking, error) {
	movie, seat, user, err := s.validate(ctx, movieID, seatID, userID)
	if err != nil {
		return domain.Booking{}, err
	}

	now := s.now()
	booking := domain.Booking{
		ID:        uuid.NewString(),
		MovieID:   movie.ID,
		SeatID:    seat,
		UserID:    user,
		Status:    domain.StatusHeld,
		CreatedAt: now.UTC(),
		ExpiresAt: now.Add(s.holdTTL).UTC(),
	}

	held, err := s.repo.Hold(ctx, booking, s.holdTTL)
	if err != nil {
		return domain.Booking{}, err
	}
	return held, nil
}

// Confirm turns an active hold into a permanent booking.
func (s *Service) Confirm(ctx context.Context, movieID, seatID, userID string) (domain.Booking, error) {
	movie, seat, user, err := s.validate(ctx, movieID, seatID, userID)
	if err != nil {
		return domain.Booking{}, err
	}

	booking, err := s.repo.Confirm(ctx, movie.ID, seat, user, s.now().UTC())
	if err != nil {
		return domain.Booking{}, err
	}
	return booking, nil
}

// Release gives up a hold so another user can book the seat.
func (s *Service) Release(ctx context.Context, movieID, seatID, userID string) error {
	movie, seat, user, err := s.validate(ctx, movieID, seatID, userID)
	if err != nil {
		return err
	}
	return s.repo.Release(ctx, movie.ID, seat, user)
}

// ListBookings returns all active reservations for a movie.
func (s *Service) ListBookings(ctx context.Context, movieID string) ([]domain.Booking, error) {
	movie, err := s.GetMovie(ctx, movieID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListByMovie(ctx, movie.ID)
}

// Ready reports whether the backing store is reachable.
func (s *Service) Ready(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

// HoldTTL exposes the configured hold duration.
func (s *Service) HoldTTL() time.Duration {
	return s.holdTTL
}

func (s *Service) validate(ctx context.Context, movieID, seatID, userID string) (domain.Movie, string, string, error) {
	movie, err := s.GetMovie(ctx, movieID)
	if err != nil {
		return domain.Movie{}, "", "", err
	}

	user := userID
	if !userIDPattern.MatchString(user) {
		return domain.Movie{}, "", "", domain.ErrInvalidUserID
	}

	seat := domain.NormalizeSeatID(seatID)
	if _, _, err := domain.ParseSeat(movie, seat); err != nil {
		return domain.Movie{}, "", "", err
	}

	return movie, seat, user, nil
}
