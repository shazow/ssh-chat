package chat

import (
	"reflect"
	"testing"
)

func TestChannel(t *testing.T) {
	s := &MockScreen{}
	out := make(chan Message)

	go func() {
		for msg := range out {
			t.Logf("Broadcasted: %s", msg.String())
			s.Write([]byte(msg.Render(nil)))
		}
	}()

	u := NewUser("foo", s)
	ch := NewChannel("", out)
	err := ch.Join(u)

	if err != nil {
		t.Error(err)
	}

	expected := []byte(" * foo joined. (Connected: 1)")
	if !reflect.DeepEqual(s.received, expected) {
		t.Errorf("Got: `%s`, Expected: `%s`", s.received, expected)
	}
}
