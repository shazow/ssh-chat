package chat

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
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

func (s *MockScreen) Close() error {
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

type ScreenedUser struct {
	user   *message.User
	screen *MockScreen
}

func TestIgnore(t *testing.T) {
	var buffer []byte

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	// Create 3 users, join the room and clear their screen buffers
	users := make([]ScreenedUser, 3)
	for i := 0; i < 3; i++ {
		screen := &MockScreen{}
		user := message.NewUserScreen(message.SimpleID(fmt.Sprintf("user%d", i)), screen)
		users[i] = ScreenedUser{
			user:   user,
			screen: screen,
		}

		_, err := ch.Join(user)
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, u := range users {
		for i := 0; i < 3; i++ {
			u.user.HandleMsg(u.user.ConsumeOne())
			u.screen.Read(&buffer)
		}
	}

	// Use some handy variable names for distinguish between roles
	ignorer := users[0]
	ignored := users[1]
	other := users[2]

	// test ignoring unexisting user
	if err := sendCommand("/ignore test", ignorer, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Err: user not found: test"+message.Newline)

	// test ignoring existing user
	if err := sendCommand("/ignore "+ignored.user.Name(), ignorer, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Ignoring: "+ignored.user.Name()+message.Newline)

	// ignoring the same user twice returns an error message and doesn't add the user twice
	if err := sendCommand("/ignore "+ignored.user.Name(), ignorer, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Err: user already ignored: user1"+message.Newline)
	if ignoredList := ignorer.user.Ignored.ListPrefix(""); len(ignoredList) != 1 {
		t.Fatalf("should have %d ignored users, has %d", 1, len(ignoredList))
	}

	// when an emote is sent by an ignored user, it should not be displayed for ignorer
	ch.HandleMsg(message.NewEmoteMsg("is crying", ignored.user))
	if ignorer.user.HasMessages() {
		t.Fatal("should not have emote messages")
	}

	other.user.HandleMsg(other.user.ConsumeOne())
	other.screen.Read(&buffer)
	expectOutput(t, buffer, "** "+ignored.user.Name()+" is crying"+message.Newline)

	// when a message is sent from the ignored user, it is delivered to non-ignoring users
	ch.HandleMsg(message.NewPublicMsg("hello", ignored.user))
	other.user.HandleMsg(other.user.ConsumeOne())
	other.screen.Read(&buffer)
	expectOutput(t, buffer, ignored.user.Name()+": hello"+message.Newline)

	// ensure ignorer doesn't have received any message
	if ignorer.user.HasMessages() {
		t.Fatal("should not have messages")
	}

	// `/ignore` returns a list of ignored users
	if err := sendCommand("/ignore", ignorer, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> 1 ignored: "+ignored.user.Name()+message.Newline)

	// `/unignore [USER]` removes the user from ignored ones
	if err := sendCommand("/unignore "+ignored.user.Name(), users[0], ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> No longer ignoring: user1"+message.Newline)

	if err := sendCommand("/ignore", users[0], ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> 0 users ignored."+message.Newline)

	if ignoredList := ignorer.user.Ignored.ListPrefix(""); len(ignoredList) != 0 {
		t.Fatalf("should have %d ignored users, has %d", 0, len(ignoredList))
	}

	// after unignoring a user, its messages can be received again
	ch.HandleMsg(message.NewPublicMsg("hello again!", ignored.user))

	// ensure ignorer has received the message
	if !ignorer.user.HasMessages() {
		t.Fatal("should have messages")
	}
	ignorer.user.HandleMsg(ignorer.user.ConsumeOne())
	ignorer.screen.Read(&buffer)
	expectOutput(t, buffer, ignored.user.Name()+": hello again!"+message.Newline)
}

func TestMute(t *testing.T) {
	var buffer []byte

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	// Create 3 users, join the room and clear their screen buffers
	users := make([]ScreenedUser, 3)
	members := make([]*Member, 3)
	for i := 0; i < 3; i++ {
		screen := &MockScreen{}
		user := message.NewUserScreen(message.SimpleID(fmt.Sprintf("user%d", i)), screen)
		users[i] = ScreenedUser{
			user:   user,
			screen: screen,
		}

		member, err := ch.Join(user)
		if err != nil {
			t.Fatal(err)
		}
		members[i] = member
	}

	for _, u := range users {
		for i := 0; i < 3; i++ {
			u.user.HandleMsg(u.user.ConsumeOne())
			u.screen.Read(&buffer)
		}
	}

	// Use some handy variable names for distinguish between roles
	muter := users[0]
	muted := users[1]
	other := users[2]

	members[0].IsOp = true

	// test muting unexisting user
	if err := sendCommand("/mute test", muter, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Err: user not found"+message.Newline)

	// test muting by non-op
	if err := sendCommand("/mute "+muted.user.Name(), other, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Err: must be op"+message.Newline)

	// test muting existing user
	if err := sendCommand("/mute "+muted.user.Name(), muter, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Muted: "+muted.user.Name()+message.Newline)

	if got, want := members[1].IsMuted(), true; got != want {
		t.Error("muted user failed to set mute flag")
	}

	// when an emote is sent by a muted user, it should not be displayed for anyone
	ch.HandleMsg(message.NewPublicMsg("hello!", muted.user))
	ch.HandleMsg(message.NewEmoteMsg("is crying", muted.user))

	if muter.user.HasMessages() {
		muter.user.HandleMsg(muter.user.ConsumeOne())
		muter.screen.Read(&buffer)
		t.Errorf("muter should not have messages: %s", buffer)
	}
	if other.user.HasMessages() {
		other.user.HandleMsg(other.user.ConsumeOne())
		other.screen.Read(&buffer)
		t.Errorf("other should not have messages: %s", buffer)
	}

	// test unmuting
	if err := sendCommand("/mute "+muted.user.Name(), muter, ch, &buffer); err != nil {
		t.Fatal(err)
	}
	expectOutput(t, buffer, "-> Unmuted: "+muted.user.Name()+message.Newline)

	ch.HandleMsg(message.NewPublicMsg("hello again!", muted.user))
	other.user.HandleMsg(other.user.ConsumeOne())
	other.screen.Read(&buffer)
	expectOutput(t, buffer, muted.user.Name()+": hello again!"+message.Newline)
}

func expectOutput(t *testing.T, buffer []byte, expected string) {
	t.Helper()

	bytes := []byte(expected)
	if !reflect.DeepEqual(buffer, bytes) {
		t.Errorf("Got: %q; Expected: %q", buffer, expected)
	}
}

func sendCommand(cmd string, mock ScreenedUser, room *Room, buffer *[]byte) error {
	msg, ok := message.NewPublicMsg(cmd, mock.user).ParseCommand()
	if !ok {
		return errors.New("cannot parse command message")
	}

	room.Send(msg)
	mock.user.HandleMsg(mock.user.ConsumeOne())
	mock.screen.Read(buffer)

	return nil
}

func TestRoomJoin(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := message.NewUserScreen(message.SimpleID("foo"), s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(u.ConsumeOne())
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.NewSystemMsg("hello", u))
	u.HandleMsg(u.ConsumeOne())
	expected = []byte("-> hello" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/me says hello.", u))
	u.HandleMsg(u.ConsumeOne())
	expected = []byte("** foo says hello." + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestRoomDoesntBroadcastAnnounceMessagesWhenQuiet(t *testing.T) {
	u := message.NewUser(message.SimpleID("foo"))
	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	go func() {
		/*
			for {
				msg := u.ConsumeChan()
				if _, ok := msg.(*message.AnnounceMsg); ok {
					t.Errorf("Got unexpected `%T`", msg)
				}
			}
		*/
		// XXX: Fix this
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
	u := message.NewUser(message.SimpleID("foo"))
	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	ch := NewRoom()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	// Drain the initial Join message
	<-ch.broadcast

	u.SetConfig(message.UserConfig{
		Quiet: false,
	})

	expectedMsg := message.NewAnnounceMsg("Ignored")
	ch.HandleMsg(expectedMsg)
	msg := u.ConsumeOne()
	if _, ok := msg.(*message.AnnounceMsg); !ok {
		t.Errorf("Got: `%T`; Expected: `%T`", msg, expectedMsg)
	}

	u.SetConfig(message.UserConfig{
		Quiet: true,
	})

	ch.HandleMsg(message.NewAnnounceMsg("Ignored"))
	ch.HandleMsg(message.NewSystemMsg("hello", u))
	msg = u.ConsumeOne()
	if _, ok := msg.(*message.AnnounceMsg); ok {
		t.Errorf("Got unexpected `%T`", msg)
	}
}

func TestQuietToggleDisplayState(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := message.NewUserScreen(message.SimpleID("foo"), s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(u.ConsumeOne())
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	u.HandleMsg(u.ConsumeOne())
	expected = []byte("-> Quiet mode is toggled ON" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/quiet", u))

	u.HandleMsg(u.ConsumeOne())
	expected = []byte("-> Quiet mode is toggled OFF" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestRoomNames(t *testing.T) {
	var expected, actual []byte

	s := &MockScreen{}
	u := message.NewUserScreen(message.SimpleID("foo"), s)

	ch := NewRoom()
	go ch.Serve()
	defer ch.Close()

	_, err := ch.Join(u)
	if err != nil {
		t.Fatal(err)
	}

	u.HandleMsg(u.ConsumeOne())
	expected = []byte(" * foo joined. (Connected: 1)" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	ch.Send(message.ParseInput("/names", u))

	u.HandleMsg(u.ConsumeOne())
	expected = []byte("-> 1 connected: foo" + message.Newline)
	s.Read(&actual)
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestRoomNamesPrefix(t *testing.T) {
	r := NewRoom()

	s := &MockScreen{}
	members := []*Member{
		&Member{User: message.NewUserScreen(message.SimpleID("aaa"), s)},
		&Member{User: message.NewUserScreen(message.SimpleID("aab"), s)},
		&Member{User: message.NewUserScreen(message.SimpleID("aac"), s)},
		&Member{User: message.NewUserScreen(message.SimpleID("foo"), s)},
	}

	for _, m := range members {
		if err := r.Members.Add(set.Itemize(m.ID(), m)); err != nil {
			t.Fatal(err)
		}
	}

	sendMsg := func(from *Member, body string) {
		// lastMsg is set during render of self messags, so we can't use NewMsg
		from.HandleMsg(message.NewPublicMsg(body, from.User))
	}

	// Inject some activity
	sendMsg(members[2], "hi") // aac
	sendMsg(members[0], "hi") // aaa
	sendMsg(members[3], "hi") // foo
	sendMsg(members[1], "hi") // aab

	if got, want := r.NamesPrefix("a"), []string{"aab", "aaa", "aac"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got: %q; want: %q", got, want)
	}

	sendMsg(members[2], "hi") // aac
	if got, want := r.NamesPrefix("a"), []string{"aac", "aab", "aaa"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got: %q; want: %q", got, want)
	}

	if got, want := r.NamesPrefix("f"), []string{"foo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got: %q; want: %q", got, want)
	}

	if got, want := r.NamesPrefix("bar"), []string{}; !reflect.DeepEqual(got, want) {
		t.Errorf("got: %q; want: %q", got, want)
	}
}
