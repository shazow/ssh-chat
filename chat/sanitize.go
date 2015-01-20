package chat

import "regexp"

var reStripName = regexp.MustCompile("[^\\w.-]")

// SanitizeName returns a name with only allowed characters.
func SanitizeName(s string) string {
	return reStripName.ReplaceAllString(s, "")
}

var reStripData = regexp.MustCompile("[^[:ascii:]]")

// SanitizeData returns a string with only allowed characters for client-provided metadata inputs.
func SanitizeData(s string) string {
	return reStripData.ReplaceAllString(s, "")
}
