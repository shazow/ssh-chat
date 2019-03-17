package humantime

import (
	"testing"
	"time"
)

func TestHumanSince(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{
			time.Second * 42,
			"42 seconds",
		},
		{
			time.Second * 60 * 5,
			"5 minutes",
		},
		{
			time.Minute * 185,
			"3.1 hours",
		},
		{
			time.Hour * 49,
			"2.0 days",
		},
		{
			time.Hour * (24*900 + 12),
			"900.5 days",
		},
	}

	for _, test := range tests {
		absolute := time.Now().Add(test.input * -1)
		if actual, expected := Since(absolute), test.expected; actual != expected {
			t.Errorf("Got: %q; Expected: %q", actual, expected)
		}
	}
}
