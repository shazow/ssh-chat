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
	go ch.Serve()
	defer ch.Close()

	err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.ConsumeOne(s)
	expected = []byte(" * foo joined. (Connected: 1)" + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
	// XXX
	t.Skip()

	m := NewPublicMsg("hello", u)
	ch.Send(m)

	u.ConsumeOne(s)
	expected = []byte("foo: hello" + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
