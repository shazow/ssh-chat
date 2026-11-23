package apron

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/apron-chat/apron-server-go/wire"

	"github.com/shazow/ssh-chat/chat/message"
)

// Users.

// userIDLocked returns a member's user_id, giving one to a user the gateway
// has not seen before: the one a Local connection's member names, or the
// next of the gateway's own that nobody has.
func (g *Gateway) userIDLocked(user *message.User) string {
	if id, ok := g.userIDs[user]; ok {
		return id
	}
	id, ok := g.pinned[user]
	for !ok {
		g.nextID++
		id = fmt.Sprintf("u%d", g.nextID)
		ok = g.userByIDLocked(id) == nil && !slices.Contains(slices.Collect(maps.Values(g.pinned)), id)
	}
	g.userIDs[user] = id
	return id
}

// userByIDLocked returns the user with a user_id, the member in the room
// when another, who has left, had it before.
func (g *Gateway) userByIDLocked(userID string) *message.User {
	var found *message.User
	for user, id := range g.userIDs {
		if id != userID {
			continue
		}
		if _, ok := g.host.Member(user); ok {
			return user
		}
		found = user
	}
	return found
}

// takenLocked reports whether a user_id is a member's in the room, or a
// Local connection's.
func (g *Gateway) takenLocked(userID string) bool {
	if slices.Contains(slices.Collect(maps.Values(g.pinned)), userID) {
		return true
	}
	user := g.userByIDLocked(userID)
	if user == nil {
		return false
	}
	_, ok := g.host.Member(user)
	return ok
}

// userLocked returns a member's complete current user object (§3.3.1).
func (g *Gateway) userLocked(user *message.User) wire.User {
	u := wire.User{UserID: g.userIDLocked(user), Name: user.Name(), Status: wire.StatusOnline}
	if away, _, _ := user.GetAway(); away {
		u.Status = statusAway
	}
	if member, ok := g.host.Member(user); ok && member.IsOp() {
		u.Roles = []string{"op"}
	}
	return u
}

// recordedLocked returns the user object a record keeps of a member.
func (g *Gateway) recordedLocked(user *message.User) wire.User {
	return wire.User{UserID: g.userIDLocked(user), Name: user.Name()}
}

// membersLocked returns the room's members as clients were last told of
// them, as complete user objects.
func (g *Gateway) membersLocked() []wire.CurrentUser {
	users := []wire.CurrentUser{}
	for _, member := range g.roomMembers() {
		if u, ok := g.log.known[member.User]; ok {
			users = append(users, wire.CurrentUser{User: u})
		}
	}
	return users
}

// Rooms (§4.2): the one room, whose members are everyone connected.

func (g *Gateway) roomLocked() wire.Room {
	return wire.Room{RoomID: g.room, LogID: g.roomLogID, Description: g.host.Topic()}
}

func (g *Gateway) RoomList(_ context.Context, _ *wire.Conn, p wire.RoomListParams, reply wire.Reply[wire.RoomListResult]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.ParentRoomID != "" && p.ParentRoomID != g.room {
		return g.errOneRoom
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.syncLocked()
	log := g.logLocked()
	room := wire.RoomView{Room: g.roomLocked(), LatestLogID: log.Latest, HistoryLogID: &log.Lower}
	result := wire.RoomListResult{Joined: []wire.RoomView{}}
	if p.Members {
		room.Members = g.membersLocked()
		if p.UserID != "" {
			room.Members = slices.DeleteFunc(room.Members, func(u wire.CurrentUser) bool { return u.UserID != p.UserID })
		}
		result.Users = room.Members
	}
	if p.ParentRoomID == "" {
		// The room has no threads.
		result.Joined = append(result.Joined, room)
	}
	// Under g.mu, so that the room is as the notifications before the
	// result left it, and those after it change it (§1).
	return reply.Send(result)
}

func (g *Gateway) RoomJoin(_ context.Context, _ *wire.Conn, p wire.RoomJoinParams, _ wire.Reply[wire.Empty]) error {
	if p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.UserID != "" {
		return wire.Unsupported("Everyone connected is in the room")
	}
	return nil // Every connection is in the room.
}

// RoomLeave of another user is an op's /kick; a connection leaves the room
// by closing.
func (g *Gateway) RoomLeave(ctx context.Context, c *wire.Conn, p wire.RoomLeaveParams, _ wire.Reply[wire.Empty]) error {
	if p.RoomID != g.room {
		return g.errOneRoom
	}
	s, _ := wire.DataOf[*session](c)
	if p.UserID == "" || p.UserID == c.UserID() {
		return wire.Denied("Everyone connected is in the room; disconnect to leave")
	}
	g.mu.Lock()
	target := g.userByIDLocked(p.UserID)
	g.mu.Unlock()
	if target == nil {
		return wire.InvalidParams("No such user in the room")
	}
	if !g.host.IsOp(s.user) {
		return wire.Denied("Only ops can remove people")
	}
	return g.run(ctx, s, "/kick "+target.ID())
}

func (g *Gateway) RoomSet(_ context.Context, _ *wire.Conn, p wire.RoomSetParams, _ wire.Reply[wire.RoomSetResult]) error {
	if p.RoomID == "" && p.Private != nil && *p.Private {
		return wire.Unsupported("This server has no private rooms")
	}
	return wire.Denied("This server has one room, which can't be changed")
}

// History (§4.3): the history buffer.

// historyLimit bounds a history page.
const historyLimit = 200

func (g *Gateway) History(_ context.Context, _ *wire.Conn, p wire.HistoryParams, reply wire.Reply[wire.HistoryResult]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.syncLocked()
	return reply.Send(wire.Page(g.logLocked(), p, historyLimit))
}

// Status (§4.5): members are online, or away. ssh-chat has no
// notifications to hold back, so mutes and idle reports change nothing.

func (g *Gateway) Status(_ context.Context, c *wire.Conn, p wire.StatusParams, _ wire.Reply[wire.Empty]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.Mute != nil {
		c.Notify(wire.StatusUpdate{RoomID: p.RoomID, Mute: *p.Mute})
	}
	return nil
}
