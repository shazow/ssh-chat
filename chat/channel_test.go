package chat

import (
	"reflect"
	"testing"
)

func TestChannel(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := NewUser("foo")

	ch := NewChannel()
	defer ch.Close()

	err := ch.Join(u)
	if err != nil {
		t.Error(err)
	}

	u.ConsumeOne(s)
	expected = []byte(" * foo joined. (Connected: 1)")
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	m := NewMessage("hello").From(u)
	ch.Send(*m)

	u.ConsumeOne(s)
	expected = []byte("foo: hello")
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
