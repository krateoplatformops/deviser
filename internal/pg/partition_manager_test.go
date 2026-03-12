package pg

import (
	"testing"
	"time"
)

func TestParsePartitionBound(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantStart   time.Time
		wantEnd     time.Time
		expectError bool
	}{
		{
			name:  "valid postgres UTC bound",
			input: "FOR VALUES FROM ('2026-02-17 00:00:00+00') TO ('2026-02-18 00:00:00+00')",
			wantStart: time.Date(2026, 2, 17, 0, 0, 0, 0,
				time.FixedZone("UTC", 0)),
			wantEnd: time.Date(2026, 2, 18, 0, 0, 0, 0,
				time.FixedZone("UTC", 0)),
			expectError: false,
		},
		{
			name:  "valid non-UTC offset",
			input: "FOR VALUES FROM ('2026-02-17 00:00:00+02') TO ('2026-02-18 00:00:00+02')",
			wantStart: time.Date(2026, 2, 17, 0, 0, 0, 0,
				time.FixedZone("", 2*3600)),
			wantEnd: time.Date(2026, 2, 18, 0, 0, 0, 0,
				time.FixedZone("", 2*3600)),
			expectError: false,
		},
		{
			name:        "invalid format string",
			input:       "INVALID STRING",
			expectError: true,
		},
		{
			name:        "missing TO section",
			input:       "FOR VALUES FROM ('2026-02-17 00:00:00+00')",
			expectError: true,
		},
		{
			name:        "invalid timestamp",
			input:       "FOR VALUES FROM ('2026-02-17') TO ('2026-02-18')",
			expectError: true,
		},
		{
			name:        "postgres with timezone minutes",
			input:       "FOR VALUES FROM ('2026-02-17 00:00:00+02:00') TO ('2026-02-18 00:00:00+02:00')",
			expectError: true, // attualmente non supportato dal layout
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parsePartitionBound(tt.input)

			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !start.Equal(tt.wantStart) {
				t.Errorf("start mismatch\n got:  %v\n want: %v", start, tt.wantStart)
			}

			if !end.Equal(tt.wantEnd) {
				t.Errorf("end mismatch\n got:  %v\n want: %v", end, tt.wantEnd)
			}
		})
	}
}
