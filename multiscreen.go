package sshchat

import (
	"io"
	"sync"

	"github.com/shazow/ssh-chat/chat/message"
)

type multiScreen struct {
	*message.User

	mu      sync.Mutex
	writers []io.WriteCloser
}

func (s *multiScreen) add(w io.WriteCloser) {
	s.mu.Lock()
	s.writers = append(s.writers, w)
	s.mu.Unlock()
}

func (s *multiScreen) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.writers {
		n, err = w.Write(p)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		if err == nil {
			continue
		}
		if err != nil && len(s.writers) == 1 {
			// Once we're out of writers, fail.
			return len(p), err
		}

		// Remove faulty writer
		w.Close()
		s.writers[i] = s.writers[len(s.writers)-1]
		s.writers[len(s.writers)-1] = nil
		s.writers = s.writers[:len(s.writers)-1]

		// TODO: Emit error to a callback or something?
	}
	return len(p), nil
}
