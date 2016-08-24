package chat

import (
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

func TestSet(t *testing.T) {
	var err error
	s := set.New()
	u := message.NewUser(message.SimpleId("foo"))

	if s.In(u.Id()) {
		t.Errorf("Set should be empty.")
	}

	err = s.Add(set.Itemize(u.Id(), u))
	if err != nil {
		t.Error(err)
	}

	if !s.In(u.Id()) {
		t.Errorf("Set should contain user.")
	}

	u2 := message.NewUser(message.SimpleId("bar"))
	err = s.Add(set.Itemize(u2.Id(), u2))
	if err != nil {
		t.Error(err)
	}

	err = s.Add(set.Itemize(u2.Id(), u2))
	if err != set.ErrCollision {
		t.Error(err)
	}

	size := s.Len()
	if size != 2 {
		t.Errorf("Set wrong size: %d (expected %d)", size, 2)
	}
}
