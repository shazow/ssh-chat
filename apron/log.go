package apron

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"

	"github.com/apron-chat/apron-server-go/wire"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

// The room's log, as Apron clients see it, is its history buffer: each
// message there is a record, with a message_id derived from its time as
// the gateway first sees it, in the buffer's order. Joins and leaves,
// which ssh-chat announces, are membership records. The buffer keeps only
// the latest messages, so the log starts where the buffer does, once it
// has dropped one (§4.3.3).

// logState is what the gateway keeps about the log. Guarded by Gateway.mu.
type logState struct {
	// entries is the buffer as of the last sync, oldest first.
	entries []message.Message
	// memberships are the join and leave announcements in the buffer, by
	// log_id.
	memberships map[wire.LogID]*wire.Membership
	// replyTo holds what messages from Apron clients reply to, which
	// ssh-chat has no place for, by message key, while they are in the
	// buffer.
	replyTo map[msgKey]*wire.Ref
	// known are the members as last seen, as current user objects: what
	// every client has been told of them. departed are those who have left
	// since, latest last, whose leave may not be announced yet.
	known    map[*message.User]wire.User
	departed []wire.User
	// oldest is the log_id of the buffer's oldest message at the last
	// sync; evicted is set once the buffer has dropped one, after which the
	// log starts at the buffer's oldest.
	oldest  wire.LogID
	evicted bool
}

func (l *logState) init() {
	l.memberships = make(map[wire.LogID]*wire.Membership)
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

// keyOf returns a message's key. It holds the message's body, not how it
// renders, which names the sender as they are called now: a key must not
// change when they rename.
func keyOf(m message.Message) msgKey {
	k := msgKey{kind: fmt.Sprintf("%T", m), when: m.Timestamp().UnixNano(), body: m.String()}
	if b, ok := m.(interface{ Body() string }); ok {
		k.body = b.Body()
	}
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
// date with the room: it gives message_ids to new messages in the buffer,
// in its order, recognizes joins and leaves, and tells clients of the
// members whose user objects changed (§3.3.3). It sends while g.mu is
// held, so that every connection gets the changes in the order they were
// seen.
func (g *Gateway) syncLocked() {
	entries := g.history.Get(g.history.Len())
	members := g.roomMembers()
	present := make(map[wire.LogID]bool, len(entries))
	referenced := make(map[*message.User]bool)
	var oldest wire.LogID
	for i, m := range entries {
		key := keyOf(m)
		_, seen := g.ids.Lookup(key)
		id := g.ids.ID(key, m.Timestamp())
		present[id] = true
		if i == 0 {
			oldest = id
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
	if len(entries) > 0 {
		if g.log.oldest != 0 && g.log.oldest != oldest {
			// The buffer drops only its oldest, so a new oldest means it
			// dropped one: from now on, the log starts at the buffer's
			// oldest (§4.3.3).
			g.log.evicted = true
		}
		g.log.oldest = oldest
	}
	for key := range g.log.replyTo {
		// Kept until the message has left the buffer, or, for one that
		// never enters it, been delivered.
		if id, ok := g.ids.Lookup(key); ok && !present[id] {
			delete(g.log.replyTo, key)
		}
	}
	g.log.entries = entries

	known := make(map[*message.User]wire.User, len(members))
	for _, member := range members {
		u := g.userLocked(member.User)
		if old, ok := g.log.known[member.User]; ok {
			if _, changed := u.Patch(old); changed {
				// A patch, which clears what was taken away, such as
				// roles: [] for a member who is no longer an op.
				g.server.PublishUser(old, u, nil)
			}
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
}

// classifyLocked records an announcement that is a join or a leave as a
// membership.
func (g *Gateway) classifyLocked(id wire.LogID, text string, members []*chat.Member) {
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
func (g *Gateway) recordLocked(m message.Message, id wire.LogID) (wire.Record, bool) {
	snapshot := func(from wire.User, body *wire.Body, ext wire.Ext) (wire.Record, bool) {
		return wire.Record{LogID: id, Message: &wire.Message{
			MessageID: id, LogID: id, RoomID: Room, From: from, Body: body,
			ReplyTo: g.log.replyTo[keyOf(m)], Ext: ext,
		}}, true
	}
	switch m := m.(type) {
	case message.PublicMsg:
		return snapshot(g.recordedLocked(m.From()), &wire.Body{Text: m.Body()}, nil)
	case *message.EmoteMsg:
		return snapshot(g.recordedLocked(m.From()), &wire.Body{Text: "_" + m.Body() + "_", Format: wire.Markdown},
			wire.MustExt("ssh-chat", map[string]any{"emote": true}))
	case *message.AnnounceMsg:
		if membership := g.log.memberships[id]; membership != nil {
			return wire.Record{LogID: id, Membership: membership}, true
		}
		return snapshot(wire.RoomNotice, &wire.Body{Text: plain(m.Body())}, nil)
	}
	return wire.Record{}, false
}

// logLocked returns the log as of the last sync: the room's record, the
// buffer's records, in log_id order, with its bounds.
func (g *Gateway) logLocked() wire.Log {
	room := g.roomLocked()
	records := []wire.Record{{LogID: g.roomLogID, Room: &room}}
	for _, m := range g.log.entries {
		id, ok := g.ids.Lookup(keyOf(m))
		if !ok {
			continue
		}
		if record, ok := g.recordLocked(m, id); ok {
			records = append(records, record)
		}
	}
	slices.SortFunc(records, func(a, b wire.Record) int { return cmp.Compare(a.LogID, b.LogID) })
	log := wire.Log{Records: records, Lower: g.roomLogID, Latest: records[len(records)-1].LogID}
	if g.log.evicted {
		// The room's record went with the first message the buffer
		// dropped.
		log.Lower = g.log.oldest
	}
	return log
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
