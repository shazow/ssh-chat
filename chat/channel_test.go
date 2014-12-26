package chat

import (
	"os"
	"reflect"
	"testing"
)

func TestChannelServe(t *testing.T) {
	ch := NewChannel()
	ch.Send(NewAnnounceMsg("hello"))

	received := <-ch.broadcast
	actual := received.String()
	expected := " * hello"

	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestChannelJoin(t *testing.T) {
	var expected, actual []byte

	SetLogger(os.Stderr)

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

	ch.Send(NewSystemMsg("hello", u))
	u.ConsumeOne(s)
	expected = []byte("-> hello" + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(ParseInput("/me says hello.", u))
	u.ConsumeOne(s)
	expected = []byte("** foo says hello." + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
