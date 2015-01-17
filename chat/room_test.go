package chat

import (
	"reflect"
	"testing"
)

func TestRoomServe(t *testing.T) {
	ch := NewRoom()
	ch.Send(NewAnnounceMsg("hello"))

	received := <-ch.broadcast
	actual := received.String()
	expected := " * hello"

	if actual != expected {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestRoomJoin(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := NewUser("foo")

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
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

func TestRoomDoesntBroadcastAnnounceMessagesWhenQuiet(t *testing.T) {
	u := NewUser("foo")
	u.Config = UserConfig{
		Quiet: true,
	}

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	go func() {
		for msg := range u.msg {
			if _, ok := msg.(*AnnounceMsg); ok {
				t.Errorf("Got unexpected `%T`", msg)
			}
		}
	}()

	// Call with an AnnounceMsg and all the other types
	// and assert we received only non-announce messages
	ch.HandleMsg(NewAnnounceMsg("Ignored"))
	// Assert we still get all other types of messages
	ch.HandleMsg(NewEmoteMsg("hello", u))
	ch.HandleMsg(NewSystemMsg("hello", u))
	ch.HandleMsg(NewPrivateMsg("hello", u, u))
	ch.HandleMsg(NewPublicMsg("hello", u))
}

func TestRoomQuietToggleBroadcasts(t *testing.T) {
	u := NewUser("foo")
	u.Config = UserConfig{
		Quiet: true,
	}

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	u.ToggleQuietMode()

	expectedMsg := NewAnnounceMsg("Ignored")
	ch.HandleMsg(expectedMsg)
	msg := <-u.msg
	if _, ok := msg.(*AnnounceMsg); !ok {
		t.Errorf("Got: `%T`; Expected: `%T`", msg, expectedMsg)
	}

	u.ToggleQuietMode()

	ch.HandleMsg(NewAnnounceMsg("Ignored"))
	ch.HandleMsg(NewSystemMsg("hello", u))
	msg = <-u.msg
	if _, ok := msg.(*AnnounceMsg); ok {
		t.Errorf("Got unexpected `%T`", msg)
	}
}

func TestQuietToggleDisplayState(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := NewUser("foo")

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	ch.Send(ParseInput("/quiet", u))
	u.ConsumeOne(s)
	expected = []byte("-> Quiet mode is toggled ON" + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(ParseInput("/quiet", u))
	u.ConsumeOne(s)
	expected = []byte("-> Quiet mode is toggled OFF" + Newline)

	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestRoomNames(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := NewUser("foo")

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	ch.Send(ParseInput("/names", u))
	u.ConsumeOne(s)
	expected = []byte("-> 1 connected: foo" + Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
