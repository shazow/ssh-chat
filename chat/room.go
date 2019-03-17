package chat

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/humantime"
	"github.com/shazow/ssh-chat/set"
)

const historyLen = 20
const roomBuffer = 10

// ErrRoomClosed is the error returned when a message is sent to a room that is already
// closed.
var ErrRoomClosed = errors.New("room closed")

// ErrInvalidName is the error returned when a user attempts to join with an invalid name,
// such as empty string.
var ErrInvalidName = errors.New("invalid name")

// Member is a User with per-Room metadata attached to it.
type Member struct {
	*message.User
	IsOp bool
}

// Room definition, also a Set of User Items
type Room struct {
	topic     string
	history   *message.History
	broadcast chan message.Message
	commands  Commands
	closed    bool
	closeOnce sync.Once

	Members *set.Set
}

// NewRoom creates a new room.
func NewRoom() *Room {
	broadcast := make(chan message.Message, roomBuffer)

	return &Room{
		broadcast: broadcast,
		history:   message.NewHistory(historyLen),
		commands:  *defaultCommands,

		Members: set.New(),
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
		r.Members.Each(func(_ string, item set.Item) error {
			item.Value().(*Member).Close()
			return nil
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
		fromMsg, _ := m.(message.MessageFrom)
		r.history.Add(m)
		r.Members.Each(func(_ string, item set.Item) (err error) {
			user := item.Value().(*Member).User

			if fromMsg != nil && user.Ignored.In(fromMsg.From().ID()) {
				// Skip because ignored
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
func (r *Room) History(u *message.User) {
	for _, m := range r.history.Get(historyLen) {
		u.Send(m)
	}
}

// Join the room as a user, will announce.
func (r *Room) Join(u *message.User) (*Member, error) {
	// TODO: Check if closed
	if u.ID() == "" {
		return nil, ErrInvalidName
	}
	member := &Member{User: u}
	err := r.Members.Add(set.Itemize(u.ID(), member))
	if err != nil {
		return nil, err
	}
	r.History(u)
	s := fmt.Sprintf("%s joined. (Connected: %d)", u.Name(), r.Members.Len())
	r.Send(message.NewAnnounceMsg(s))
	return member, nil
}

// Leave the room as a user, will announce. Mostly used during setup.
func (r *Room) Leave(u *message.User) error {
	err := r.Members.Remove(u.ID())
	if err != nil {
		return err
	}
	s := fmt.Sprintf("%s left. (After %s)", u.Name(), humantime.Since(u.Joined()))
	r.Send(message.NewAnnounceMsg(s))
	return nil
}

// Rename member with a new identity. This will not call rename on the member.
func (r *Room) Rename(oldID string, u message.Identifier) error {
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
func (r *Room) Member(u *message.User) (*Member, bool) {
	m, ok := r.MemberByID(u.ID())
	if !ok {
		return nil, false
	}
	// Check that it's the same user
	if m.User != u {
		return nil, false
	}
	return m, true
}

// MemberByID Gets a member by an id / name
func (r *Room) MemberByID(id string) (*Member, bool) {
	m, err := r.Members.Get(id)
	if err != nil {
		return nil, false
	}
	return m.Value().(*Member), true
}

// IsOp returns whether a user is an operator in this room.
func (r *Room) IsOp(u *message.User) bool {
	m, ok := r.Member(u)
	if !ok {
		return false
	}
	return m.IsOp
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
// for autocompletion purposes. Sorted by which user was last active.
func (r *Room) NamesPrefix(prefix string) []string {
	items := r.Members.ListPrefix(prefix)

	// Sort results by recently active
	users := make([]*message.User, 0, len(items))
	for _, item := range items {
		users = append(users, item.Value().(*Member).User)
	}
	sort.Sort(message.RecentActiveUsers(users))

	// Pull out names
	names := make([]string, 0, len(items))
	for _, user := range users {
		names = append(names, user.Name())
	}
	return names
}
