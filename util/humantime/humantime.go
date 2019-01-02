package humantime

import (
	"fmt"
	"time"
)

// humanSince returns a human-friendly relative time string
func humanSince(d time.Duration) string {
	switch {
	case d < time.Minute*2:
		return fmt.Sprintf("%0.f seconds", d.Seconds())
	case d < time.Hour*2:
		return fmt.Sprintf("%0.f minutes", d.Minutes())
	case d < time.Hour*48:
		return fmt.Sprintf("%0.f hours", d.Hours())
	}
	return fmt.Sprintf("%0.f days", d.Hours()/24)
}

func HumanSince(d time.Duration) string {
	return humanSince(d)
}

