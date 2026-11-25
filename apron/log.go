package apron

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"

	"github.com/apron-chat/apron-server-go/apron"
	"github.com/apron-chat/apron-server-go/apron/server"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/set"
)

// The room's log, as Apron clients see it, is its history buffer: each
// message there is a record, with a message_id derived from its time as
// the gateway first sees it, in the buffer's order. Joins and leaves,
// which ssh-chat announces, are membership records. The buffer keeps only
// the latest messages, so the log starts where the buffer does, once it
// has dropped one (§4.3.3); when what it drops held memberships, the
// gateway appends a membership record of its own first, which lists every
// member, so that the log still tells who is in the room.

// logState is what the gateway keeps about the log. Guarded by Gateway.mu.
type logState struct {
	// entries is the buffer as of the last sync, oldest first.
	entries []message.Message
	// memberships are the join and leave announcements in the buffer, by
	// log_id.
	memberships map[apron.LogID]*apron.Membership
	// snapshots are the membership records the gateway appended, each
	// listing every member, oldest first, while they are in the log.
	snapshots []apron.Membership
	// replyTo holds what messages from Apron clients reply to, which
	// ssh-chat has no place for, by message key, while they are in the
	// buffer.
	replyTo map[msgKey]*apron.MessageRef
	// known are the members as last seen, as current user objects: what
	// every client has been told of them. departed are those who have left
	// since, latest last, whose leave may not be announced yet.
	known    map[*message.User]apron.User
	departed []apron.User
	// oldest and newest are the log_ids of the buffer's oldest and newest
	// messages at the last sync; evicted is set once the buffer has dropped
	// one, after which the log starts at the buffer's oldest.
	oldest, newest apron.LogID
	evicted        bool
}

func (l *logState) init() {
	l.memberships = make(map[apron.LogID]*apron.Membership)
	l.replyTo = make(map[msgKey]*apron.MessageRef)
	l.known = make(map[*message.User]apron.User)
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
// in its order, recognizes joins and leaves, appends a membership record
// of every member before the log loses memberships, and tells clients of
// the members whose user objects changed (§3.3.3). It sends while g.mu is
// held, so that every connection gets the changes in the order they were
// seen.
func (g *Gateway) syncLocked() {
	// The whole buffer, read at once: Get takes at most what there is, and
	// one added between Len and Get would drop the oldest from the view,
	// as if the buffer had dropped it.
	entries := g.history.Get(math.MaxInt)
	members := g.roomMembers()
	present := make(map[apron.LogID]bool, len(entries))
	referenced := make(map[*message.User]bool)
	var oldest, newest apron.LogID
	for i, m := range entries {
		key := keyOf(m)
		_, seen := g.ids.Lookup(key)
		id := g.ids.ID(key, m.Timestamp())
		present[id] = true
		if i == 0 {
			oldest = id
		}
		newest = id
		if f, ok := m.(message.MessageFrom); ok {
			referenced[f.From()] = true
		}
		if a, ok := m.(*message.AnnounceMsg); ok && !seen {
			g.classifyLocked(id, a.Body(), members)
		}
	}
	// Whether the log loses memberships: a join or leave left the buffer,
	// or one of the gateway's membership records fell below its oldest.
	lost := false
	for id, membership := range g.log.memberships {
		if !present[id] {
			delete(g.log.memberships, id)
			lost = true
		} else {
			referenced[g.userByIDLocked(membership.Members[0].User.UserID)] = true
		}
	}
	// A buffer that turned over entirely since the last sync may have
	// dropped memberships the gateway never saw, newer than any it keeps.
	unseen := g.log.newest != 0 && !present[g.log.newest]
	if len(entries) > 0 {
		if g.log.oldest != 0 && g.log.oldest != oldest {
			// The buffer drops only its oldest, so a new oldest means it
			// dropped one: from now on, the log starts at the buffer's
			// oldest (§4.3.3).
			g.log.evicted = true
		}
		g.log.oldest, g.log.newest = oldest, newest
	}
	if g.log.evicted {
		before := len(g.log.snapshots)
		g.log.snapshots = slices.DeleteFunc(g.log.snapshots, func(s apron.Membership) bool { return s.LogID < g.log.oldest })
		lost = lost || len(g.log.snapshots) < before
	}
	// What the log drops is older than what it keeps, so a membership
	// record of every member that it keeps still tells who is in the room;
	// otherwise the gateway appends one (§4.3.3).
	if (unseen || lost && len(g.log.snapshots) == 0) && len(members) > 0 {
		// Its log_id is the latest yet, after every message of the
		// buffer's, so the log's records stay in order, and its lower
		// bound, the buffer's oldest, never moves back.
		snapshot := apron.Membership{LogID: g.clock.Next(), RoomID: g.room}
		for _, member := range members {
			snapshot.Members = append(snapshot.Members, apron.Member{User: g.recordedLocked(member.User), Joined: true})
		}
		g.log.snapshots = append(g.log.snapshots, snapshot)
	}
	for key := range g.log.replyTo {
		// Kept until the message has left the buffer, or, for one that
		// never enters it, been delivered.
		if id, ok := g.ids.Lookup(key); ok && !present[id] {
			delete(g.log.replyTo, key)
		}
	}
	g.log.entries = entries

	known := make(map[*message.User]apron.User, len(members))
	for _, member := range members {
		u := g.userLocked(member.User)
		if old, ok := g.log.known[member.User]; ok {
			// A patch, which clears what was taken away, such as roles: []
			// for a member who is no longer an op; nothing if unchanged.
			g.server.PublishUser(server.UserChange{Old: old, New: u}, nil)
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
	// A user_id is kept while its user is in the room or the log. One that
	// goes is never given again, so the server forgets the requests it kept
	// of it for deduplication (§1.2).
	for user, id := range g.userIDs {
		if !referenced[user] {
			g.server.ForgetUser(id)
			delete(g.userIDs, user)
		}
	}
}

// classifyLocked records an announcement that is a join or a leave as a
// membership.
func (g *Gateway) classifyLocked(id apron.LogID, text string, members []*chat.Member) {
	var user apron.User
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
	g.log.memberships[id] = &apron.Membership{LogID: id, RoomID: g.room, Members: []apron.Member{{
		User: apron.User{UserID: user.UserID, Name: user.Name}, Joined: joined,
	}}}
}

// recordLocked returns the record of a message in the log.
func (g *Gateway) recordLocked(m message.Message, id apron.LogID) (apron.Record, bool) {
	snapshot := func(from apron.User, body *apron.Body, ext apron.Ext) (apron.Record, bool) {
		return apron.Record{LogID: id, Message: &apron.Message{
			MessageID: id, LogID: id, RoomID: g.room, From: from, Body: body,
			ReplyTo: g.log.replyTo[keyOf(m)], Ext: ext,
		}}, true
	}
	switch m := m.(type) {
	case message.PublicMsg:
		return snapshot(g.recordedLocked(m.From()), &apron.Body{Text: m.Body()}, nil)
	case *message.EmoteMsg:
		return snapshot(g.recordedLocked(m.From()), &apron.Body{Text: "_" + m.Body() + "_", Format: apron.Markdown},
			apron.Ext{}.With("ssh-chat", map[string]any{"emote": true}))
	case *message.AnnounceMsg:
		if membership := g.log.memberships[id]; membership != nil {
			return apron.Record{LogID: id, Membership: membership}, true
		}
		return snapshot(apron.RoomNotice, &apron.Body{Text: plain(m.Body())}, nil)
	}
	return apron.Record{}, false
}

// logLocked returns the log as of the last sync: the room's record, the
// buffer's records, and the gateway's membership records, in log_id order,
// with its bounds.
func (g *Gateway) logLocked() server.Log {
	room := g.roomLocked()
	records := []apron.Record{{LogID: g.roomLogID, Room: &room}}
	for _, m := range g.log.entries {
		id, ok := g.ids.Lookup(keyOf(m))
		if !ok {
			continue
		}
		if record, ok := g.recordLocked(m, id); ok {
			records = append(records, record)
		}
	}
	for i := range g.log.snapshots {
		records = append(records, apron.Record{LogID: g.log.snapshots[i].LogID, Membership: &g.log.snapshots[i]})
	}
	slices.SortFunc(records, func(a, b apron.Record) int { return cmp.Compare(a.LogID, b.LogID) })
	log := server.Log{Records: records, Lower: g.roomLogID, Latest: records[len(records)-1].LogID}
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
