package main

import (
	"bytes"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
)

var logger *golog.Logger

func init() {
	// Set a default null logger
	var b bytes.Buffer
	logger = golog.New(&b, log.Debug)
}
