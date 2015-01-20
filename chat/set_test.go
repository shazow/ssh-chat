package chat

import (
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
)

func TestSet(t *testing.T) {
	var err error
	s := NewSet()
	u := message.NewUser(message.SimpleId("foo"))

	if s.In(u) {
		t.Errorf("Set should be empty.")
	}

	err = s.Add(u)
	if err != nil {
		t.Error(err)
	}

	if !s.In(u) {
		t.Errorf("Set should contain user.")
	}

	u2 := message.NewUser(message.SimpleId("bar"))
	err = s.Add(u2)
	if err != nil {
		t.Error(err)
	}

	err = s.Add(u2)
	if err != ErrIdTaken {
		t.Error(err)
	}

	size := s.Len()
	if size != 2 {
		t.Errorf("Set wrong size: %d (expected %d)", size, 2)
	}
}
