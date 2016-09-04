package message

import "regexp"

var reStripName = regexp.MustCompile("[^\\w.-]")

const maxLength = 16

// SanitizeName returns a name with only allowed characters and a reasonable length
func SanitizeName(s string) string {
	s = reStripName.ReplaceAllString(s, "")
	nameLength := maxLength
	if len(s) <= maxLength {
		nameLength = len(s)
	}
	s = s[:nameLength]
	return s
}
