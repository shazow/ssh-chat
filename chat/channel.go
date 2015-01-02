package chat

import (
	"errors"
	"fmt"
	"sync"
)

const historyLen = 20
const channelBuffer = 10

// The error returned when a message is sent to a channel that is already
// closed.
var ErrChannelClosed = errors.New("channel closed")

// Channel definition, also a Set of User Items
type Channel struct {
	topic     string
	history   *History
	users     *Set
	ops       *Set
	broadcast chan Message
	commands  Commands
	closed    bool
	closeOnce sync.Once
}

// NewChannel creates a new channel.
func NewChannel() *Channel {
	broadcast := make(chan Message, channelBuffer)

	return &Channel{
		broadcast: broadcast,
		history:   NewHistory(historyLen),
		users:     NewSet(),
		ops:       NewSet(),
		commands:  *defaultCommands,
	}
}

// SetCommands sets the channel's command handlers.
func (ch *Channel) SetCommands(commands Commands) {
	ch.commands = commands
}

// Close the channel and all the users it contains.
func (ch *Channel) Close() {
	ch.closeOnce.Do(func() {
		ch.closed = true
		ch.users.Each(func(u Item) {
			u.(*User).Close()
		})
		ch.users.Clear()
		close(ch.broadcast)
	})
}

// HandleMsg reacts to a message, will block until done.
func (ch *Channel) HandleMsg(m Message) {
	switch m := m.(type) {
	case *CommandMsg:
		cmd := *m
		err := ch.commands.Run(ch, cmd)
		if err != nil {
			m := NewSystemMsg(fmt.Sprintf("Err: %s", err), cmd.from)
			go ch.HandleMsg(m)
		}
	case MessageTo:
		user := m.To()
		user.Send(m)
	default:
		fromMsg, skip := m.(MessageFrom)
		var skipUser *User
		if skip {
			skipUser = fromMsg.From()
		}

		ch.users.Each(func(u Item) {
			user := u.(*User)
			if skip && skipUser == user {
				// Skip
				return
			}
			err := user.Send(m)
			if err != nil {
				ch.Leave(user)
				user.Close()
			}
		})
	}
}

// Serve will consume the broadcast channel and handle the messages, should be
// run in a goroutine.
func (ch *Channel) Serve() {
	for m := range ch.broadcast {
		go ch.HandleMsg(m)
	}
}

// Send message, buffered by a chan.
func (ch *Channel) Send(m Message) {
	ch.broadcast <- m
}

// Join the channel as a user, will announce.
func (ch *Channel) Join(u *User) error {
	if ch.closed {
		return ErrChannelClosed
	}
	err := ch.users.Add(u)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s joined. (Connected: %d)", u.Name(), ch.users.Len())
	ch.Send(NewAnnounceMsg(s))
	return nil
}

// Leave the channel as a user, will announce.
func (ch *Channel) Leave(u *User) error {
	err := ch.users.Remove(u)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s left.", u.Name())
	ch.Send(NewAnnounceMsg(s))
	return nil
}

// Topic of the channel.
func (ch *Channel) Topic() string {
	return ch.topic
}

// SetTopic will set the topic of the channel.
func (ch *Channel) SetTopic(s string) {
	ch.topic = s
}

// NamesPrefix lists all members' names with a given prefix, used to query
// for autocompletion purposes.
func (ch *Channel) NamesPrefix(prefix string) []string {
	users := ch.users.ListPrefix(prefix)
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.(*User).Name()
	}
	return names
}
