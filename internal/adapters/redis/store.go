// Package redis adapts the booking repository to Redis. Holds use SET NX with
// a TTL, while confirm/release run as Lua scripts so ownership checks and state
// transitions are atomic across multiple application instances.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/abdullahsamir74/cinema-booking/internal/domain"
	goredis "github.com/redis/go-redis/v9"
)

const defaultHoldTTL = 2 * time.Minute

var confirmScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return {-1, ''} end
local ok, b = pcall(cjson.decode, raw)
if not ok then return {-3, ''} end
if b['user_id'] ~= ARGV[1] then return {-2, ''} end
if b['status'] == 'confirmed' then return {1, raw} end
b['status'] = 'confirmed'
b['confirmed_at'] = ARGV[2]
b['expires_at'] = '0001-01-01T00:00:00Z'
local encoded = cjson.encode(b)
redis.call('SET', KEYS[1], encoded)
return {1, encoded}
`)

var releaseScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local ok, b = pcall(cjson.decode, raw)
if not ok then return -3 end
if b['user_id'] ~= ARGV[1] then return -2 end
if b['status'] == 'confirmed' then return 0 end
redis.call('DEL', KEYS[1])
return 1
`)

// Store is a Redis-backed booking repository.
type Store struct {
	client *goredis.Client
	now    func() time.Time
}

// NewStore wraps an existing Redis client.
func NewStore(client *goredis.Client) *Store {
	return &Store{client: client, now: time.Now}
}

// WithClock injects a clock, mainly for tests.
func (s *Store) WithClock(now func() time.Time) *Store {
	if now != nil {
		s.now = now
	}
	return s
}

// Hold stores a reservation using an atomic SET NX. Only one caller can win.
func (s *Store) Hold(ctx context.Context, booking domain.Booking, ttl time.Duration) (domain.Booking, error) {
	if ttl <= 0 {
		ttl = defaultHoldTTL
	}

	booking.SeatID = domain.NormalizeSeatID(booking.SeatID)
	payload, err := json.Marshal(booking)
	if err != nil {
		return domain.Booking{}, fmt.Errorf("marshal booking: %w", err)
	}

	ok, err := s.client.SetNX(ctx, bookingKey(booking.MovieID, booking.SeatID), payload, ttl).Result()
	if err != nil {
		return domain.Booking{}, fmt.Errorf("redis setnx: %w", err)
	}
	if !ok {
		return domain.Booking{}, domain.ErrSeatUnavailable
	}
	return booking, nil
}

// Confirm atomically converts the caller's hold into a confirmed booking.
func (s *Store) Confirm(ctx context.Context, movieID, seatID, userID string, at time.Time) (domain.Booking, error) {
	values, err := confirmScript.Run(ctx, s.client,
		[]string{bookingKey(movieID, seatID)},
		userID, at.UTC().Format(time.RFC3339Nano),
	).Slice()
	if err != nil {
		return domain.Booking{}, fmt.Errorf("redis confirm: %w", err)
	}
	if len(values) != 2 {
		return domain.Booking{}, fmt.Errorf("redis confirm: unexpected reply %v", values)
	}

	code, _ := values[0].(int64)
	switch code {
	case 1:
		encoded, _ := values[1].(string)
		var booking domain.Booking
		if err := json.Unmarshal([]byte(encoded), &booking); err != nil {
			return domain.Booking{}, fmt.Errorf("decode confirmed booking: %w", err)
		}
		return booking, nil
	case -1:
		return domain.Booking{}, domain.ErrHoldNotFound
	case -2:
		return domain.Booking{}, domain.ErrNotHoldOwner
	default:
		return domain.Booking{}, fmt.Errorf("redis confirm: corrupted booking payload")
	}
}

// Release atomically removes a hold owned by userID.
func (s *Store) Release(ctx context.Context, movieID, seatID, userID string) error {
	code, err := releaseScript.Run(ctx, s.client,
		[]string{bookingKey(movieID, seatID)}, userID,
	).Int64()
	if err != nil {
		return fmt.Errorf("redis release: %w", err)
	}

	switch code {
	case 1:
		return nil
	case 0:
		return domain.ErrBookingConfirmed
	case -1:
		return domain.ErrHoldNotFound
	case -2:
		return domain.ErrNotHoldOwner
	default:
		return fmt.Errorf("redis release: unexpected reply %d", code)
	}
}

// Get returns one reservation.
func (s *Store) Get(ctx context.Context, movieID, seatID string) (domain.Booking, bool, error) {
	raw, err := s.client.Get(ctx, bookingKey(movieID, seatID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.Booking{}, false, nil
	}
	if err != nil {
		return domain.Booking{}, false, fmt.Errorf("redis get: %w", err)
	}

	var booking domain.Booking
	if err := json.Unmarshal(raw, &booking); err != nil {
		return domain.Booking{}, false, fmt.Errorf("decode booking: %w", err)
	}
	if booking.IsExpired(s.now()) {
		return domain.Booking{}, false, nil
	}
	return booking, true, nil
}

// ListByMovie scans the movie's seat keys and returns active reservations.
func (s *Store) ListByMovie(ctx context.Context, movieID string) ([]domain.Booking, error) {
	pattern := fmt.Sprintf("booking:%s:*", movieID)
	keys := make([]string, 0, 64)

	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan: %w", err)
	}

	result := make([]domain.Booking, 0, len(keys))
	for start := 0; start < len(keys); start += 100 {
		end := min(start+100, len(keys))
		values, err := s.client.MGet(ctx, keys[start:end]...).Result()
		if err != nil {
			return nil, fmt.Errorf("redis mget: %w", err)
		}

		for _, value := range values {
			raw, ok := value.(string)
			if !ok {
				continue
			}
			var booking domain.Booking
			if err := json.Unmarshal([]byte(raw), &booking); err != nil {
				continue
			}
			if booking.IsExpired(s.now()) {
				continue
			}
			result = append(result, booking)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].SeatID < result[j].SeatID
	})
	return result, nil
}

// Ping checks connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func bookingKey(movieID, seatID string) string {
	return fmt.Sprintf("booking:%s:%s", movieID, domain.NormalizeSeatID(seatID))
}
