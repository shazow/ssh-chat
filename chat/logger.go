package chat

import (
	"io"
	stdlog "log"
)

var logger *stdlog.Logger

// SetLogger changes the logger used for logging inside the package
func SetLogger(w io.Writer) {
	flags := stdlog.Flags()
	prefix := "[chat] "
	logger = stdlog.New(w, prefix, flags)
}

type nullWriter struct{}

func (nullWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func init() {
	SetLogger(nullWriter{})
}
