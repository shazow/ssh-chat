package sshchat

import "regexp"

var reStripData = regexp.MustCompile("[^[:ascii:]]")

// SanitizeData returns a string with only allowed characters for client-provided metadata inputs.
func SanitizeData(s string) string {
	return reStripData.ReplaceAllString(s, "")
}
