package chat

import (
	"reflect"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
)

type ChannelWriter struct {
	Chan chan []byte
}

func (w *ChannelWriter) Write(data []byte) (n int, err error) {
	w.Chan <- data
	return len(data), nil
}

func (w *ChannelWriter) Close() error {
	close(w.Chan)
	return nil
}

func TestRoomServe(t *testing.T) {
	ch := NewRoom()
	ch.Send(message.NewAnnounceMsg("hello"))

	received := <-ch.broadcast
	actual := received.String()
	expected := " * hello"

	if actual != expected {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func expectOutput(t *testing.T, buffer []byte, expected string) {
	bytes := []byte(expected)
	if !reflect.DeepEqual(buffer, bytes) {
		t.Errorf("Got: %q; Expected: %q", buffer, expected)
	}
}

func TestRoomJoin(t *testing.T) {
	var expected, actual []byte

	s := &ChannelWriter{
		Chan: make(chan []byte),
	}
	u := message.PipedScreen("foo", s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.NewSystemMsg("hello", u))
	expected = []byte("-> hello" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/me says hello.", u))
	expected = []byte("** foo says hello." + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestIgnore(t *testing.T) {
	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	addUser := func(name string) (message.Author, <-chan []byte) {
		s := &ChannelWriter{
			Chan: make(chan []byte, 3),
		}
		u := message.PipedScreen(name, s)
		u.SetConfig(message.UserConfig{
			Quiet: true,
		})
		ch.Join(u)
		return u, s.Chan
	}

	u_foo, m_foo := addUser("foo")
	u_bar, m_bar := addUser("bar")
	u_quux, m_quux := addUser("quux")

	var expected, actual []byte

	// foo ignores bar, quux hears both
	ch.Send(message.ParseInput("/ignore bar", u_foo))
	expected = []byte("-> Ignoring: bar" + message.Newline)
	actual = <-m_foo
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	// bar and quux sends a message, quux hears bar, foo only hears quux
	ch.Send(message.ParseInput("i am bar", u_bar))
	ch.Send(message.ParseInput("i am quux", u_quux))

	expected = []byte("bar: i am bar" + message.Newline)
	actual = <-m_quux
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	expected = []byte("quux: i am quux" + message.Newline)
	actual = <-m_bar
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
	actual = <-m_foo
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	// foo sends a message, both quux and bar hear it
	ch.Send(message.ParseInput("i am foo", u_foo))
	expected = []byte("foo: i am foo" + message.Newline)

	actual = <-m_quux
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
	actual = <-m_bar
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	// Confirm foo's message queue is still empty
	select {
	case actual = <-m_foo:
		t.Errorf("foo's message queue is not empty: %q", actual)
	default:
		// Pass.
	}

	// Unignore and listen to bar again.
	ch.Send(message.ParseInput("/unignore bar", u_foo))
	expected = []byte("-> No longer ignoring: bar" + message.Newline)
	actual = <-m_foo
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("i am bar again", u_bar))
	expected = []byte("bar: i am bar again" + message.Newline)
	actual = <-m_foo
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestRoomDoesntBroadcastAnnounceMessagesWhenQuiet(t *testing.T) {
	msgs := make(chan message.Message)
	u := message.HandledScreen("foo", func(m message.Message) error {
		msgs <- m
		return nil
	})
	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for msg := range msgs {
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
	// Try an ignored one again just in case
	ch.HandleMsg(message.NewAnnounceMsg("Once more for fun"))
}

func TestRoomQuietToggleBroadcasts(t *testing.T) {
	msgs := make(chan message.Message)
	u := message.HandledScreen("foo", func(m message.Message) error {
		msgs <- m
		return nil
	})
	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.SetConfig(message.UserConfig{
		Quiet: false,
	})

	expectedMsg := message.NewAnnounceMsg("Ignored")
	go ch.HandleMsg(expectedMsg)
	msg := <-msgs
	if _, ok := msg.(*message.AnnounceMsg); !ok {
		t.Errorf("Got: `%T`; Expected: `%T`", msg, expectedMsg)
	}

	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	go func() {
		ch.HandleMsg(message.NewAnnounceMsg("Ignored"))
		ch.HandleMsg(message.NewSystemMsg("hello", u))
	}()
	msg = <-msgs
	if _, ok := msg.(*message.AnnounceMsg); ok {
		t.Errorf("Got unexpected `%T`", msg)
	}
}

func TestQuietToggleDisplayState(t *testing.T) {
	var expected, actual []byte

	s := &ChannelWriter{
		Chan: make(chan []byte),
	}
	u := message.PipedScreen("foo", s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	expected = []byte("-> Quiet mode is toggled ON" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	expected = []byte("-> Quiet mode is toggled OFF" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestRoomNames(t *testing.T) {
	var expected, actual []byte

	s := &ChannelWriter{
		Chan: make(chan []byte),
	}
	u := message.PipedScreen("foo", s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/names", u))

	expected = []byte("-> 1 connected: foo" + message.Newline)
	actual = <-s.Chan
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}
