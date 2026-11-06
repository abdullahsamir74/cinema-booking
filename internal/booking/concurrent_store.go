package booking

import "sync"

type ConcurrentStore struct {
	sync.RWMutex
	bookings map[string]Booking // SeatID -> Booking{}
}

func NewConcurrentStore() *ConcurrentStore {
	return &ConcurrentStore{bookings: make(map[string]Booking)}
}

func (s *ConcurrentStore) Book(b Booking) error {
	s.Lock()
	defer s.Unlock()

	if _, exists := s.bookings[b.SeatID]; exists {
		return ErrSeatAlreadyBooked
	}
	s.bookings[b.SeatID] = b
	return nil
}

func (s *ConcurrentStore) ListBookings(movieID string) []Booking {
	s.RLock()
	defer s.RUnlock()

	var result []Booking
	for _, b := range s.bookings {
		if b.MovieID == movieID {
			result = append(result, b)
		}
	}
	return result
}
