package booking

import (
	"context"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
)

// Repository is the write/read port the booking service needs. Implementations
// must make Hold atomic: under concurrent calls for the same seat exactly one
// caller may succeed.
type Repository interface {
	Hold(ctx context.Context, booking domain.Booking, ttl time.Duration) (domain.Booking, error)
	Confirm(ctx context.Context, movieID, seatID, userID string, at time.Time) (domain.Booking, error)
	Release(ctx context.Context, movieID, seatID, userID string) error
	Get(ctx context.Context, movieID, seatID string) (domain.Booking, bool, error)
	ListByMovie(ctx context.Context, movieID string) ([]domain.Booking, error)
	Ping(ctx context.Context) error
}
