package pg

import (
	"testing"
	"time"
)

func TestStartOfUTCDayIncludesCurrentUTCDate(t *testing.T) {
	now := time.Date(2026, 5, 21, 23, 59, 59, 0, time.UTC)

	got := startOfUTCDay(now)
	want := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	if !got.Equal(want) {
		t.Fatalf("startOfUTCDay() = %s, want %s", got, want)
	}
}

func TestStartOfUTCDayNormalizesInputTimezone(t *testing.T) {
	loc := time.FixedZone("UTC-7", -7*60*60)
	now := time.Date(2026, 5, 21, 23, 30, 0, 0, loc)

	got := startOfUTCDay(now)
	want := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	if !got.Equal(want) {
		t.Fatalf("startOfUTCDay() = %s, want %s", got, want)
	}
}

func TestPartitionTimestampLayoutIncludesUTCOffset(t *testing.T) {
	ts := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	got := ts.Format(partitionTimestampLayout)
	want := "2026-05-21 00:00:00+00"

	if got != want {
		t.Fatalf("formatted timestamp = %q, want %q", got, want)
	}
}
