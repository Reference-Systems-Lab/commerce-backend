// Package clock is the only place the backend reads the system clock. Business code takes a Clock,
// so billing can run on a moved clock and tests can fix the time (AGENTS.md: the clock abstraction).
package clock

import "time"

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// System reads the real clock, in UTC.
type System struct{}

// Now returns the current time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed always returns the same instant. It is for tests.
type Fixed time.Time

// Now returns the fixed instant.
func (f Fixed) Now() time.Time { return time.Time(f) }
