package main

import (
    "math/rand"
    "time"
)

const RESET string =     "\033[0m"
const BOLD string =      "\033[1m"
const DIM string =       "\033[2m"
const UNDERLINE string = "\033[4m"
const BLINK string =     "\033[5m"
const INVERT string =    "\033[7m"

var colors = []string { "31", "32", "33", "34", "35", "36", "37", "91", "92", "93", "94", "95", "96", "97" }

func RandomColor() string {
	rand.Seed(time.Now().UTC().UnixNano())
	return colors[rand.Intn(len(colors))]
}

func ColorString(format string, msg string) string {
	return BOLD + "\033[" + format + "m" + msg + RESET
}
