package chat

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

const historyLen = 20
const roomBuffer = 10

// The error returned when a message is sent to a room that is already
// closed.
var ErrRoomClosed = errors.New("room closed")

// The error returned when a user attempts to join with an invalid name, such
// as empty string.
var ErrInvalidName = errors.New("invalid name")

// Room definition, also a Set of User Items
type Room struct {
	topic     string
	history   *message.History
	broadcast chan message.Message
	commands  Commands
	closed    bool
	closeOnce sync.Once

	Members *set.Set
	Ops     *set.Set
}

// NewRoom creates a new room.
func NewRoom() *Room {
	broadcast := make(chan message.Message, roomBuffer)

	return &Room{
		broadcast: broadcast,
		history:   message.NewHistory(historyLen),
		commands:  *defaultCommands,

		Members: set.New(),
		Ops:     set.New(),
	}
}

// SetCommands sets the room's command handlers.
func (r *Room) SetCommands(commands Commands) {
	r.commands = commands
}

// Close the room
func (r *Room) Close() {
	r.closeOnce.Do(func() {
		r.closed = true
		r.Members.Clear()
		close(r.broadcast)
	})
}

// SetLogging sets logging output for the room's history
func (r *Room) SetLogging(out io.Writer) {
	r.history.SetOutput(out)
}

// HandleMsg reacts to a message, will block until done.
func (r *Room) HandleMsg(m message.Message) {
	switch m := m.(type) {
	case *message.CommandMsg:
		cmd := *m
		err := r.commands.Run(r, cmd)
		if err != nil {
			m := message.NewSystemMsg(fmt.Sprintf("Err: %s", err), cmd.From())
			go r.HandleMsg(m)
		}
	case message.MessageTo:
		user := m.To().(Member)
		user.Send(m)
	default:
		fromMsg, skip := m.(message.MessageFrom)
		var skipUser Member
		if skip {
			skipUser = fromMsg.From().(Member)
		}

		r.history.Add(m)
		r.Members.Each(func(k string, item set.Item) (err error) {
			roomMember := item.Value().(*roomMember)
			user := roomMember.Member

			if fromMsg != nil && fromMsg.From() != nil && roomMember.Ignored.In(fromMsg.From().ID()) {
				// Skip because ignored
				return
			}

			if skip && skipUser == user {
				// Skip self
				return
			}
			if _, ok := m.(*message.AnnounceMsg); ok {
				if user.Config().Quiet {
					// Skip announcements
					return
				}
			}
			user.Send(m)
			return
		})
	}
}

// Serve will consume the broadcast room and handle the messages, should be
// run in a goroutine.
func (r *Room) Serve() {
	for m := range r.broadcast {
		go r.HandleMsg(m)
	}
}

// Send message, buffered by a chan.
func (r *Room) Send(m message.Message) {
	r.broadcast <- m
}

// History feeds the room's recent message history to the user's handler.
func (r *Room) History(m Member) {
	for _, msg := range r.history.Get(historyLen) {
		m.Send(msg)
	}
}

// Join the room as a user, will announce.
func (r *Room) Join(m Member) (*roomMember, error) {
	// TODO: Check if closed
	if m.ID() == "" {
		return nil, ErrInvalidName
	}
	member := &roomMember{
		Member:  m,
		Ignored: set.New(),
	}
	err := r.Members.AddNew(set.Itemize(m.ID(), member))
	if err != nil {
		return nil, err
	}
	r.History(m)
	s := fmt.Sprintf("%s joined. (Connected: %d)", m.Name(), r.Members.Len())
	r.Send(message.NewAnnounceMsg(s))
	return member, nil
}

// Leave the room as a user, will announce. Mostly used during setup.
func (r *Room) Leave(u Member) error {
	err := r.Members.Remove(u.ID())
	if err != nil {
		return err
	}
	r.Ops.Remove(u.ID())
	s := fmt.Sprintf("%s left.", u.Name())
	r.Send(message.NewAnnounceMsg(s))
	return nil
}

// Rename member with a new identity. This will not call rename on the member.
func (r *Room) Rename(oldID string, u Member) error {
	if u.ID() == "" {
		return ErrInvalidName
	}
	err := r.Members.Replace(oldID, set.Itemize(u.ID(), u))
	if err != nil {
		return err
	}

	s := fmt.Sprintf("%s is now known as %s.", oldID, u.ID())
	r.Send(message.NewAnnounceMsg(s))
	return nil
}

// Member returns a corresponding Member object to a User if the Member is
// present in this room.
func (r *Room) Member(u message.Author) (*roomMember, bool) {
	m, ok := r.MemberByID(u.ID())
	if !ok {
		return nil, false
	}
	// Check that it's the same user
	if m.Member != u {
		return nil, false
	}
	return m, true
}

func (r *Room) MemberByID(id string) (*roomMember, bool) {
	m, err := r.Members.Get(id)
	if err != nil {
		return nil, false
	}
	rm, ok := m.Value().(*roomMember)
	return rm, ok
}

// IsOp returns whether a user is an operator in this room.
func (r *Room) IsOp(u message.Author) bool {
	return r.Ops.In(u.ID())
}

// Topic of the room.
func (r *Room) Topic() string {
	return r.topic
}

// SetTopic will set the topic of the room.
func (r *Room) SetTopic(s string) {
	r.topic = s
}

// NamesPrefix lists all members' names with a given prefix, used to query
// for autocompletion purposes.
func (r *Room) NamesPrefix(prefix string) []string {
	items := r.Members.ListPrefix(prefix)
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Value().(*roomMember).Name()
	}
	return names
}
