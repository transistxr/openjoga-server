package store

import "time"

const TimeFormat = "2006-01-02T15:04:05Z"

func Now() string { return time.Now().UTC().Format(TimeFormat) }

// FormatTime renders a time for storage, normalising to UTC.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTime reads a stored timestamp.
func ParseTime(s string) (time.Time, error) { return time.Parse(TimeFormat, s) }

// ParseTimeOr reads a stored timestamp, falling back to the zero time
func ParseTimeOr(s string, fallback time.Time) time.Time {
	t, err := ParseTime(s)
	if err != nil {
		return fallback
	}
	return t
}
