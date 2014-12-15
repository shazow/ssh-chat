package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const (
	// Reset resets the color
	Reset = "\033[0m"

	// Bold makes the following text bold
	Bold = "\033[1m"

	// Dim dims the following text
	Dim = "\033[2m"

	// Italic makes the following text italic
	Italic = "\033[3m"

	// Underline underlines the following text
	Underline = "\033[4m"

	// Blink blinks the following text
	Blink = "\033[5m"

	// Invert inverts the following text
	Invert = "\033[7m"
)

var colors = []string{"31", "32", "33", "34", "35", "36", "37", "91", "92", "93", "94", "95", "96", "97"}

// RandomColor256 returns a random (of 256) color
func RandomColor256() string {
	return fmt.Sprintf("38;05;%d", rand.Intn(256))
}

// RandomColor returns a random color
func RandomColor() string {
	return colors[rand.Intn(len(colors))]
}

// ColorString returns a message in the given color
func ColorString(color string, msg string) string {
	return Bold + "\033[" + color + "m" + msg + Reset
}

// RandomColorInit initializes the random seed
func RandomColorInit() {
	rand.Seed(time.Now().UTC().UnixNano())
}

// ContinuousFormat is a horrible hack to "continue" the previous string color
// and format after a RESET has been encountered.
//
// This is not HTML where you can just do a </style> to resume your previous formatting!
func ContinuousFormat(format string, str string) string {
	return systemMessageFormat + strings.Replace(str, Reset, format, -1) + Reset
}
