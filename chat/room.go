package chat

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/shazow/ssh-chat/chat/message"
)

const historyLen = 20
const roomBuffer = 10

// The error returned when a message is sent to a room that is already
// closed.
var ErrRoomClosed = errors.New("room closed")

// The error returned when a user attempts to join with an invalid name, such
// as empty string.
var ErrInvalidName = errors.New("invalid name")

// Member is a User with per-Room metadata attached to it.
type Member struct {
	*message.User
}

// Room definition, also a Set of User Items
type Room struct {
	topic     string
	history   *message.History
	broadcast chan message.Message
	commands  Commands
	closed    bool
	closeOnce sync.Once

	Members *idSet
	Ops     *idSet
}

// NewRoom creates a new room.
func NewRoom() *Room {
	broadcast := make(chan message.Message, roomBuffer)

	return &Room{
		broadcast: broadcast,
		history:   message.NewHistory(historyLen),
		commands:  *defaultCommands,

		Members: newIdSet(),
		Ops:     newIdSet(),
	}
}

// SetCommands sets the room's command handlers.
func (r *Room) SetCommands(commands Commands) {
	r.commands = commands
}

// Close the room and all the users it contains.
func (r *Room) Close() {
	r.closeOnce.Do(func() {
		r.closed = true
		r.Members.Each(func(m identified) {
			m.(*Member).Close()
		})
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
		user := m.To()
		user.Send(m)
	default:
		fromMsg, skip := m.(message.MessageFrom)
		var skipUser *message.User
		if skip {
			skipUser = fromMsg.From()
		}

		r.history.Add(m)
		r.Members.Each(func(u identified) {
			user := u.(*Member).User

			if fromMsg != nil && user.IsIgnoring(fromMsg.From().Id()) {
				// Skip because ignored
				return
			}

			if skip && skipUser == user {
				// Skip
				return
			}
			if _, ok := m.(*message.AnnounceMsg); ok {
				if user.Config.Quiet {
					// Skip
					return
				}
			}
			user.Send(m)
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
func (r *Room) History(u *message.User) {
	for _, m := range r.history.Get(historyLen) {
		u.Send(m)
	}
}

// Join the room as a user, will announce.
func (r *Room) Join(u *message.User) (*Member, error) {
	// TODO: Check if closed
	if u.Id() == "" {
		return nil, ErrInvalidName
	}
	member := Member{u}
	err := r.Members.Add(&member)
	if err != nil {
		return nil, err
	}
	r.History(u)
	s := fmt.Sprintf("%s joined. (Connected: %d)", u.Name(), r.Members.Len())
	r.Send(message.NewAnnounceMsg(s))
	return &member, nil
}

// Leave the room as a user, will announce. Mostly used during setup.
func (r *Room) Leave(u message.Identifier) error {
	err := r.Members.Remove(u)
	if err != nil {
		return err
	}
	r.Ops.Remove(u)
	s := fmt.Sprintf("%s left.", u.Name())
	r.Send(message.NewAnnounceMsg(s))
	return nil
}

// Rename member with a new identity. This will not call rename on the member.
func (r *Room) Rename(oldId string, identity message.Identifier) error {
	if identity.Id() == "" {
		return ErrInvalidName
	}
	err := r.Members.Replace(oldId, identity)
	if err != nil {
		return err
	}

	s := fmt.Sprintf("%s is now known as %s.", oldId, identity.Id())
	r.Send(message.NewAnnounceMsg(s))
	return nil
}

// Member returns a corresponding Member object to a User if the Member is
// present in this room.
func (r *Room) Member(u *message.User) (*Member, bool) {
	m, ok := r.MemberById(u.Id())
	if !ok {
		return nil, false
	}
	// Check that it's the same user
	if m.User != u {
		return nil, false
	}
	return m, true
}

func (r *Room) MemberById(id string) (*Member, bool) {
	m, err := r.Members.Get(id)
	if err != nil {
		return nil, false
	}
	return m.(*Member), true
}

// IsOp returns whether a user is an operator in this room.
func (r *Room) IsOp(u *message.User) bool {
	return r.Ops.In(u)
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
	members := r.Members.ListPrefix(prefix)
	names := make([]string, len(members))
	for i, u := range members {
		names[i] = u.(*Member).User.Name()
	}
	return names
}
