// Package memory provides an in-memory implementation of booking.Repository.
// It is safe for concurrent use and is intended for tests, local development
// and single-instance deployments without Redis.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

type seatKey struct {
	movieID string
	seatID  string
}

// Store keeps reservations in a map guarded by a RWMutex.
type Store struct {
	mu       sync.RWMutex
	bookings map[seatKey]domain.Booking
	now      func() time.Time
}

// New returns an empty store using the system clock.
func New() *Store {
	return NewWithClock(time.Now)
}

// NewWithClock returns an empty store with an injectable clock.
func NewWithClock(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{
		bookings: make(map[seatKey]domain.Booking),
		now:      now,
	}
}

// Hold reserves a seat if it is currently free.
func (s *Store) Hold(ctx context.Context, booking domain.Booking, _ time.Duration) (domain.Booking, error) {
	if err := ctx.Err(); err != nil {
		return domain.Booking{}, err
	}

	key := seatKey{booking.MovieID, domain.NormalizeSeatID(booking.SeatID)}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(key)
	if _, exists := s.bookings[key]; exists {
		return domain.Booking{}, domain.ErrSeatUnavailable
	}

	booking.SeatID = key.seatID
	s.bookings[key] = booking
	return booking, nil
}

// Confirm converts a hold owned by userID into a confirmed booking.
func (s *Store) Confirm(ctx context.Context, movieID, seatID, userID string, at time.Time) (domain.Booking, error) {
	if err := ctx.Err(); err != nil {
		return domain.Booking{}, err
	}

	key := seatKey{movieID, domain.NormalizeSeatID(seatID)}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(key)
	booking, ok := s.bookings[key]
	if !ok {
		return domain.Booking{}, domain.ErrHoldNotFound
	}
	if booking.UserID != userID {
		return domain.Booking{}, domain.ErrNotHoldOwner
	}
	if booking.Status == domain.StatusConfirmed {
		return booking, nil
	}

	confirmed := at.UTC()
	booking.Status = domain.StatusConfirmed
	booking.ConfirmedAt = &confirmed
	booking.ExpiresAt = time.Time{}
	s.bookings[key] = booking
	return booking, nil
}

// Release deletes a hold owned by userID.
func (s *Store) Release(ctx context.Context, movieID, seatID, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	key := seatKey{movieID, domain.NormalizeSeatID(seatID)}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.expireLocked(key)
	booking, ok := s.bookings[key]
	if !ok {
		return domain.ErrHoldNotFound
	}
	if booking.UserID != userID {
		return domain.ErrNotHoldOwner
	}
	if booking.Status == domain.StatusConfirmed {
		return domain.ErrBookingConfirmed
	}

	delete(s.bookings, key)
	return nil
}

// Get returns the active reservation for a seat.
func (s *Store) Get(ctx context.Context, movieID, seatID string) (domain.Booking, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.Booking{}, false, err
	}

	key := seatKey{movieID, domain.NormalizeSeatID(seatID)}
	s.mu.RLock()
	defer s.mu.RUnlock()

	booking, ok := s.bookings[key]
	if !ok || booking.IsExpired(s.now()) {
		return domain.Booking{}, false, nil
	}
	return booking, true, nil
}

// ListByMovie returns all active reservations for a movie, sorted by seat.
func (s *Store) ListByMovie(ctx context.Context, movieID string) ([]domain.Booking, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	result := make([]domain.Booking, 0, len(s.bookings))
	for key, booking := range s.bookings {
		if booking.IsExpired(now) {
			delete(s.bookings, key)
			continue
		}
		if key.movieID == movieID {
			result = append(result, booking)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].SeatID < result[j].SeatID
	})
	return result, nil
}

// Ping always succeeds for the in-memory store.
func (s *Store) Ping(ctx context.Context) error {
	return ctx.Err()
}

func (s *Store) expireLocked(key seatKey) {
	booking, ok := s.bookings[key]
	if ok && booking.IsExpired(s.now()) {
		delete(s.bookings, key)
	}
}
