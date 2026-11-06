// Package domain contains the core business entities and rules of the
// cinema booking system. It has no dependencies on transport, storage or
// infrastructure packages.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors returned by the domain, service and repositories. The HTTP
// layer maps these to status codes without knowing which storage backend is
// in use.
var (
	ErrMovieNotFound    = errors.New("movie not found")
	ErrMovieExists      = errors.New("movie already exists")
	ErrInvalidMovie     = errors.New("invalid movie")
	ErrSeatNotFound     = errors.New("seat not found")
	ErrSeatUnavailable  = errors.New("seat is already held or booked")
	ErrHoldNotFound     = errors.New("hold not found or expired")
	ErrNotHoldOwner     = errors.New("seat is held by another user")
	ErrBookingConfirmed = errors.New("booking is already confirmed")
	ErrInvalidUserID    = errors.New("invalid user id")
	ErrInvalidSeatID    = errors.New("invalid seat id")
)

// BookingStatus is the lifecycle state of a seat reservation.
type BookingStatus string

const (
	StatusHeld      BookingStatus = "held"
	StatusConfirmed BookingStatus = "confirmed"
)

// SeatState is the state exposed to clients on a seat map.
type SeatState string

const (
	SeatAvailable SeatState = "available"
	SeatHeld      SeatState = "held"
	SeatConfirmed SeatState = "confirmed"
)

// Movie is a title that can be booked.
type Movie struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Genre          string `json:"genre"`
	Rating         string `json:"rating"`
	RuntimeMinutes int    `json:"runtime_minutes"`
	Rows           int    `json:"rows"`
	SeatsPerRow    int    `json:"seats_per_row"`
}

var movieIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// slugify turns free-form user input such as "My Movie!" into "my-movie".
// It only keeps ASCII letters and digits and collapses runs of other
// characters into single hyphens.
func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	b.Grow(len(value))
	lastHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case b.Len() > 0 && !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// NormalizeMovie trims user input and canonicalises the slug.
func NormalizeMovie(m Movie) Movie {
	m.ID = slugify(m.ID)
	m.Title = strings.TrimSpace(m.Title)
	m.Description = strings.TrimSpace(m.Description)
	m.Genre = strings.TrimSpace(m.Genre)
	m.Rating = strings.TrimSpace(m.Rating)
	return m
}

// Validate checks that a movie has a URL-safe slug and a bookable room.
func (m Movie) Validate() error {
	if len(m.ID) < 2 || len(m.ID) > 64 || !movieIDPattern.MatchString(m.ID) {
		return fmt.Errorf("%w: id must be 2-64 lowercase letters, numbers or single hyphens", ErrInvalidMovie)
	}
	if title := utf8.RuneCountInString(m.Title); title < 1 || title > 120 {
		return fmt.Errorf("%w: title must be between 1 and 120 characters", ErrInvalidMovie)
	}
	if utf8.RuneCountInString(m.Description) > 1000 {
		return fmt.Errorf("%w: description must be at most 1000 characters", ErrInvalidMovie)
	}
	if utf8.RuneCountInString(m.Genre) > 80 {
		return fmt.Errorf("%w: genre must be at most 80 characters", ErrInvalidMovie)
	}
	if utf8.RuneCountInString(m.Rating) > 20 {
		return fmt.Errorf("%w: rating must be at most 20 characters", ErrInvalidMovie)
	}
	if m.RuntimeMinutes < 1 || m.RuntimeMinutes > 600 {
		return fmt.Errorf("%w: runtime_minutes must be between 1 and 600", ErrInvalidMovie)
	}
	if m.Rows < 1 || m.Rows > 26 {
		return fmt.Errorf("%w: rows must be between 1 and 26", ErrInvalidMovie)
	}
	if m.SeatsPerRow < 1 || m.SeatsPerRow > 20 {
		return fmt.Errorf("%w: seats_per_row must be between 1 and 20", ErrInvalidMovie)
	}
	return nil
}

// Booking is a held or confirmed reservation for one seat.
type Booking struct {
	ID          string        `json:"id"`
	MovieID     string        `json:"movie_id"`
	SeatID      string        `json:"seat_id"`
	UserID      string        `json:"user_id"`
	Status      BookingStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	ExpiresAt   time.Time     `json:"expires_at,omitzero"`
	ConfirmedAt *time.Time    `json:"confirmed_at,omitempty"`
}

// IsExpired reports whether a hold has passed its expiry time.
func (b Booking) IsExpired(now time.Time) bool {
	return b.Status == StatusHeld && !b.ExpiresAt.IsZero() && !now.Before(b.ExpiresAt)
}

// Seat is one position in a screening room.
type Seat struct {
	SeatID    string     `json:"seat_id"`
	Row       string     `json:"row"`
	Number    int        `json:"number"`
	Status    SeatState  `json:"status"`
	HeldBy    string     `json:"held_by,omitempty"`
	BookingID string     `json:"booking_id,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SeatStats summarises a seat map.
type SeatStats struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Held      int `json:"held"`
	Confirmed int `json:"confirmed"`
}

// SeatMap is the complete layout of a movie's room.
type SeatMap struct {
	MovieID     string    `json:"movie_id"`
	Rows        int       `json:"rows"`
	SeatsPerRow int       `json:"seats_per_row"`
	Stats       SeatStats `json:"stats"`
	Seats       []Seat    `json:"seats"`
}

var seatPattern = regexp.MustCompile(`^([A-Z])([0-9]{1,2})$`)

// ParseSeat validates a seat ID against a movie's dimensions and returns the
// row index (0-based) and seat number (1-based).
func ParseSeat(m Movie, seatID string) (rowIndex int, number int, err error) {
	seatID = strings.ToUpper(strings.TrimSpace(seatID))
	match := seatPattern.FindStringSubmatch(seatID)
	if match == nil {
		return 0, 0, fmt.Errorf("%w: expected a value like A1", ErrInvalidSeatID)
	}

	rowIndex = int(match[1][0] - 'A')
	number, err = strconv.Atoi(match[2])
	if err != nil || rowIndex < 0 || rowIndex >= m.Rows || number < 1 || number > m.SeatsPerRow {
		return 0, 0, fmt.Errorf("%w: %s is outside this room", ErrInvalidSeatID, seatID)
	}
	return rowIndex, number, nil
}

// RowLabel returns the letter used to identify a row.
func RowLabel(rowIndex int) string {
	return string(rune('A' + rowIndex))
}

// SeatID builds the canonical seat identifier for a row and number.
func SeatID(rowIndex, number int) string {
	return fmt.Sprintf("%s%d", RowLabel(rowIndex), number)
}

// NormalizeSeatID upper-cases and trims a client-provided seat ID.
func NormalizeSeatID(seatID string) string {
	return strings.ToUpper(strings.TrimSpace(seatID))
}
