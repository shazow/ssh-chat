package humantime

import (
	"fmt"
	"time"
)

// humanSince returns a human-friendly relative time string
func Since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Second*2:
		//e.g. "516.971µs", "535.412009ms", "1.880689686s"
		return d.String()
	case d < time.Minute*2:
		return fmt.Sprintf("%0.f seconds", d.Seconds())
	case d < time.Hour*2:
		return fmt.Sprintf("%0.f minutes", d.Minutes())
	case d < time.Hour*48:
		return fmt.Sprintf("%0.f hours", d.Hours())
	}
	return fmt.Sprintf("%0.f days", d.Hours()/24)
}


