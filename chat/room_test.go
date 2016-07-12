package chat

import (
	"reflect"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
)

// Used for testing
type MockScreen struct {
	buffer []byte
}

func (s *MockScreen) Write(data []byte) (n int, err error) {
	s.buffer = append(s.buffer, data...)
	return len(data), nil
}

func (s *MockScreen) Read(p *[]byte) (n int, err error) {
	*p = s.buffer
	s.buffer = []byte{}
	return len(*p), nil
}

func TestRoomServe(t *testing.T) {
	ch := NewRoom()
	ch.Send(message.NewAnnounceMsg("hello"))

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
	u := message.NewUser(message.SimpleId("foo"))

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(message.NewSystemMsg("hello", u))
	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte("-> hello" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(message.ParseInput("/me says hello.", u))
	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte("** foo says hello." + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestRoomDoesntBroadcastAnnounceMessagesWhenQuiet(t *testing.T) {
	u := message.NewUser(message.SimpleId("foo"))
	u.Config = message.UserConfig{
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
		for msg := range u.ConsumeChan() {
			if _, ok := msg.(*message.AnnounceMsg); ok {
				t.Errorf("Got unexpected `%T`", msg)
			}
		}
	}()

	// Call with an AnnounceMsg and all the other types
	// and assert we received only non-announce messages
	ch.HandleMsg(message.NewAnnounceMsg("Ignored"))
	// Assert we still get all other types of messages
	ch.HandleMsg(message.NewEmoteMsg("hello", u))
	ch.HandleMsg(message.NewSystemMsg("hello", u))
	ch.HandleMsg(message.NewPrivateMsg("hello", u, u))
	ch.HandleMsg(message.NewPublicMsg("hello", u))
}

func TestRoomQuietToggleBroadcasts(t *testing.T) {
	u := message.NewUser(message.SimpleId("foo"))
	u.Config = message.UserConfig{
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

	expectedMsg := message.NewAnnounceMsg("Ignored")
	ch.HandleMsg(expectedMsg)
	msg := <-u.ConsumeChan()
	if _, ok := msg.(*message.AnnounceMsg); !ok {
		t.Errorf("Got: `%T`; Expected: `%T`", msg, expectedMsg)
	}

	u.ToggleQuietMode()

	ch.HandleMsg(message.NewAnnounceMsg("Ignored"))
	ch.HandleMsg(message.NewSystemMsg("hello", u))
	msg = <-u.ConsumeChan()
	if _, ok := msg.(*message.AnnounceMsg); ok {
		t.Errorf("Got unexpected `%T`", msg)
	}
}

func TestQuietToggleDisplayState(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := message.NewUser(message.SimpleId("foo"))

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte("-> Quiet mode is toggled ON" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte("-> Quiet mode is toggled OFF" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}

func TestRoomNames(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := message.NewUser(message.SimpleId("foo"))

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}

	ch.Send(message.ParseInput("/names", u))

	u.HandleMsg(<-u.ConsumeChan(), s)
	expected = []byte("-> 1 connected: foo" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: `%s`; Expected: `%s`", actual, expected)
	}
}
