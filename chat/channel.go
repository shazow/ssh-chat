package chat

import (
	"errors"
	"fmt"
)

const historyLen = 20
const channelBuffer = 10

var ErrChannelClosed = errors.New("channel closed")

// Channel definition, also a Set of User Items
type Channel struct {
	topic     string
	history   *History
	users     *Set
	broadcast chan Message
	commands  Commands
	closed    bool
}

// Create new channel and start broadcasting goroutine.
func NewChannel() *Channel {
	broadcast := make(chan Message, channelBuffer)

	return &Channel{
		broadcast: broadcast,
		history:   NewHistory(historyLen),
		users:     NewSet(),
		commands:  defaultCmdHandlers,
	}
}

func (ch *Channel) Close() {
	ch.closed = true
	ch.users.Each(func(u Item) {
		u.(*User).Close()
	})
	ch.users.Clear()
	close(ch.broadcast)
}

// Handle a message, will block until done.
func (ch *Channel) handleMsg(m Message) {
	switch m.(type) {
	case CommandMsg:
		cmd := m.(CommandMsg)
		err := ch.commands.Run(cmd)
		if err != nil {
			m := NewSystemMsg(fmt.Sprintf("Err: %s", err), cmd.from)
			go ch.handleMsg(m)
		}
	case MessageTo:
		user := m.(MessageTo).To()
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
		go ch.handleMsg(m)
	}
}

func (ch *Channel) Send(m Message) {
	ch.broadcast <- m
}

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

func (ch *Channel) Leave(u *User) error {
	err := ch.users.Remove(u)
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s left.", u.Name())
	ch.Send(NewAnnounceMsg(s))
	return nil
}

func (ch *Channel) Topic() string {
	return ch.topic
}

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
