package message

import (
	"reflect"
	"testing"
)

func TestMakeUser(t *testing.T) {
	var actual, expected []byte

	s := &MockScreen{}
	u := PipedScreen("foo", s)

	m := NewAnnounceMsg("hello")

	defer u.Close()
	err := u.Send(m)
	if err != nil {
		t.Fatalf("failed to send: %s", err)
	}

	s.Read(&actual)
	expected = []byte(m.String() + Newline)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}
