package log

import (
	"os"

	"github.com/alexcesaro/log"
	"github.com/alexcesaro/log/golog"
	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

var logLevels = []log.Level{
	log.Warning,
	log.Info,
	log.Debug,
}

// Logger Global Logger
var Logger *golog.Logger

// SetLogger Set the global logger
func SetLogger(l *golog.Logger) {
	Logger = l
}

// Init Initialize the global logger
func Init(numVerbose int) {
	// Figure out the log level
	if numVerbose > len(logLevels) {
		numVerbose = len(logLevels) - 1
	}

	logLevel := logLevels[numVerbose]
	SetLogger(golog.New(os.Stderr, logLevel))

	if logLevel == log.Debug {
		// Enable logging from submodules
		chat.SetLogger(os.Stderr)
		sshd.SetLogger(os.Stderr)
	}
}
