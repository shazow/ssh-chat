package humantime

import (
	"fmt"
	"time"
)

// since returns a human-friendly relative time string
func Since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute*2:
		return fmt.Sprintf("%0.f seconds", d.Seconds())
	case d < time.Hour*2:
		return fmt.Sprintf("%0.f minutes", d.Minutes())
	case d < time.Hour*48:
		return fmt.Sprintf("%0.1f hours", d.Minutes()/60)
	}
	days := d.Minutes() / (24 * 60)
	return fmt.Sprintf("%0.1f days", days)
}
