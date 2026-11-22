package apron

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/apron-chat/apron-server-go/wire"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

// The room's log, as Apron clients see it, is its history buffer: each
// message there is a record, with a message_id derived from its time as
// the gateway first sees it, in the buffer's order. Joins and leaves,
// which ssh-chat announces, are membership records.

// logState is what the gateway keeps about the log. Guarded by Gateway.mu.
type logState struct {
	// memberships are the join and leave announcements, by log_id.
	memberships map[string]*wire.Membership
	// replyTo holds what messages from Apron clients reply to, which
	// ssh-chat has no place for, by message key, while they are in the
	// buffer.
	replyTo map[msgKey]*wire.Ref
	// known are the members as last seen, as current user objects, and
	// departed those who have left since, latest last, whose leave may not
	// be announced yet.
	known    map[*message.User]wire.User
	departed []wire.User
	// oldest is the log_id of the buffer's oldest record at the last sync;
	// evicted is set once the buffer has dropped one, after which the log
	// starts at the buffer's oldest.
	oldest  string
	evicted bool
}

func (l *logState) init() {
	l.memberships = make(map[string]*wire.Membership)
	l.replyTo = make(map[msgKey]*wire.Ref)
	l.known = make(map[*message.User]wire.User)
}

// msgKey identifies one message of the room's: the room hands the same
// message to every member, and its timestamp is taken when it is made.
type msgKey struct {
	kind string
	from *message.User
	when int64
	body string
}

func keyOf(m message.Message) msgKey {
	k := msgKey{kind: fmt.Sprintf("%T", m), when: m.Timestamp().UnixNano(), body: m.String()}
	if f, ok := m.(message.MessageFrom); ok {
		k.from = f.From()
	}
	return k
}

// maxDeparted bounds the members kept after they leave, for their leave's
// announcement.
const maxDeparted = 64

// What ssh-chat's room announces when a member joins or leaves.
var (
	joinedRE = regexp.MustCompile(`^(.+) joined\. \(Connected: \d+\)$`)
	leftRE   = regexp.MustCompile(`^(.+) left\. \(After .+\)$`)
)

// syncLocked brings the gateway's view of the log and the members up to
// date with the room: it gives message_ids to new messages in the buffer, in
// its order, recognizes joins and leaves, and returns the members whose user
// objects changed.
func (g *Gateway) syncLocked() []wire.User {
	entries := g.history.Get(g.history.Len())
	members := g.roomMembers()
	present := make(map[string]bool, len(entries))
	referenced := make(map[*message.User]bool)
	for i, m := range entries {
		key := keyOf(m)
		_, seen := g.ids.Lookup(key)
		id := g.ids.ID(key, m.Timestamp())
		present[id] = true
		if i == 0 {
			// The buffer drops only its oldest, so a new oldest means it
			// dropped one: from now on, the log starts at the buffer's
			// oldest (§4.3.3).
			if g.log.oldest != "" && g.log.oldest != id {
				g.log.evicted = true
			}
			g.log.oldest = id
		}
		if f, ok := m.(message.MessageFrom); ok {
			referenced[f.From()] = true
		}
		if a, ok := m.(*message.AnnounceMsg); ok && !seen {
			g.classifyLocked(id, a.Body(), members)
		}
	}
	for id, membership := range g.log.memberships {
		if !present[id] {
			delete(g.log.memberships, id)
		} else {
			referenced[g.userByIDLocked(membership.Members[0].User.UserID)] = true
		}
	}
	for key := range g.log.replyTo {
		// Kept until the message has left the buffer, or, for one that
		// never enters it, been delivered.
		if id, ok := g.ids.Lookup(key); ok && !present[id] {
			delete(g.log.replyTo, key)
		}
	}

	var changed []wire.User
	known := make(map[*message.User]wire.User, len(members))
	for _, member := range members {
		u := g.userLocked(member.User)
		if old, ok := g.log.known[member.User]; ok && !sameUser(old, u) {
			changed = append(changed, u)
		}
		known[member.User] = u
		referenced[member.User] = true
	}
	for user, u := range g.log.known {
		if _, ok := known[user]; !ok {
			g.log.departed = append(g.log.departed, u)
		}
	}
	if extra := len(g.log.departed) - maxDeparted; extra > 0 {
		g.log.departed = g.log.departed[extra:]
	}
	g.log.known = known
	// A user_id is kept while its user is in the room or the log.
	for user := range g.userIDs {
		if !referenced[user] {
			delete(g.userIDs, user)
		}
	}
	return changed
}

// classifyLocked records an announcement that is a join or a leave as a
// membership.
func (g *Gateway) classifyLocked(id, text string, members []*chat.Member) {
	var user wire.User
	var joined bool
	if match := joinedRE.FindStringSubmatch(text); match != nil {
		for _, member := range members {
			if member.Name() == match[1] {
				user, joined = g.userLocked(member.User), true
			}
		}
	} else if match := leftRE.FindStringSubmatch(text); match != nil {
		for _, u := range g.log.known {
			if u.Name == match[1] {
				user = u
			}
		}
		for i, u := range slices.Backward(g.log.departed) {
			if user.UserID == "" && u.Name == match[1] {
				user = u
				g.log.departed = slices.Delete(g.log.departed, i, i+1)
			}
		}
	}
	if user.UserID == "" {
		return // An announcement of something else, or of someone unknown.
	}
	g.log.memberships[id] = &wire.Membership{LogID: id, RoomID: Room, Members: []wire.Member{{
		User: wire.User{UserID: user.UserID, Name: user.Name}, Joined: joined,
	}}}
}

// recordLocked returns the record of a message in the log.
func (g *Gateway) recordLocked(m message.Message, id string) (wire.Record, bool) {
	logID, _ := strconv.ParseInt(id, 10, 64)
	snapshot := func(from wire.User, body *wire.Body, ext map[string]any) (wire.Record, bool) {
		return wire.Record{LogID: logID, Message: &wire.Message{
			MessageID: id, LogID: id, RoomID: Room, From: from, Body: body,
			ReplyTo: g.log.replyTo[keyOf(m)], Ext: ext,
		}}, true
	}
	switch m := m.(type) {
	case message.PublicMsg:
		return snapshot(g.recordedLocked(m.From()), &wire.Body{Text: m.Body()}, nil)
	case *message.EmoteMsg:
		return snapshot(g.recordedLocked(m.From()), &wire.Body{Text: "_" + m.Body() + "_", Format: wire.Markdown},
			map[string]any{"ssh-chat": map[string]any{"emote": true}})
	case *message.AnnounceMsg:
		if membership := g.log.memberships[id]; membership != nil {
			return wire.Record{LogID: logID, Membership: membership}, true
		}
		return snapshot(wire.RoomNotice, &wire.Body{Text: plain(m.Body())}, nil)
	}
	return wire.Record{}, false
}

// logLocked returns the log's records, its effective lower bound, and its
// latest log_id.
func (g *Gateway) logLocked() (records []wire.Record, lower, latest int64) {
	roomLogID, _ := strconv.ParseInt(g.roomLogID, 10, 64)
	room := g.roomLocked(false)
	records = append(records, wire.Record{LogID: roomLogID, Room: &room})
	for _, m := range g.history.Get(g.history.Len()) {
		id, _ := g.ids.Lookup(keyOf(m))
		if record, ok := g.recordLocked(m, id); ok && record.LogID > 0 {
			records = append(records, record)
		}
	}
	slices.SortFunc(records, func(a, b wire.Record) int { return int(a.LogID - b.LogID) })
	lower = roomLogID
	if g.log.evicted && len(records) > 1 {
		lower = records[1].LogID
	}
	return records, lower, records[len(records)-1].LogID
}

// roomMembers returns the room's members.
func (g *Gateway) roomMembers() []*chat.Member {
	var members []*chat.Member
	g.host.Members.Each(func(_ string, item set.Item) error {
		if member, ok := item.Value().(*chat.Member); ok {
			members = append(members, member)
		}
		return nil
	})
	return members
}
