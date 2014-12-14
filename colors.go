package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const RESET string = "\033[0m"
const BOLD string = "\033[1m"
const DIM string = "\033[2m"
const ITALIC string = "\033[3m"
const UNDERLINE string = "\033[4m"
const BLINK string = "\033[5m"
const INVERT string = "\033[7m"

var colors = []string{"31", "32", "33", "34", "35", "36", "37", "91", "92", "93", "94", "95", "96", "97"}

func RandomColor256() string {
	return fmt.Sprintf("38;05;%d", rand.Intn(256))
}

func RandomColor() string {
	return colors[rand.Intn(len(colors))]
}

func ColorString(color string, msg string) string {
	return BOLD + "\033[" + color + "m" + msg + RESET
}

func RandomColorInit() {
	rand.Seed(time.Now().UTC().UnixNano())
}

// Horrible hack to "continue" the previous string color and format
//  after a RESET has been encountered.
// This is not HTML where you can just do a </style> to resume your previous formatting!
func ContinuousFormat(format string, str string) string {
	return SYSTEM_MESSAGE_FORMAT + strings.Replace(str, RESET, format, -1) + RESET
}
