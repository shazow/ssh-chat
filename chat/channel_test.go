package chat

import (
	"reflect"
	"testing"
)

func TestChannel(t *testing.T) {
	s := &MockScreen{}
	out := make(chan Message)
	defer close(out)

	go func() {
		for msg := range out {
			s.Write([]byte(msg.Render(nil)))
		}
	}()

	u := NewUser("foo", s)
	ch := NewChannel("", out)
	ch.Join(u)

	expected := []byte(" * foo joined. (Connected: 1)")
	if !reflect.DeepEqual(s.received, expected) {
		t.Errorf("Got: %s, Expected: %s", s.received, expected)
	}
}
