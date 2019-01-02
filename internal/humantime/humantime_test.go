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
			time.Hour * 3,
			"3 hours",
		},
		{
			time.Hour * 49,
			"2 days",
		},
		{
			time.Hour * 24 * 900,
			"900 days",
		},
	}

	for _, test := range tests {
		absolute := time.Now().Add(test.input * -1)
		if actual, expected := Since(absolute), test.expected; actual != expected {
			t.Errorf("Got: %q; Expected: %q", actual, expected)
		}
	}
}
