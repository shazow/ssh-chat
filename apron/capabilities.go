package apron

import (
	"context"
	"fmt"
	"slices"

	"github.com/apron-chat/apron-server-go/apron"
	"github.com/apron-chat/apron-server-go/apron/server"

	"github.com/shazow/ssh-chat/chat/message"
)

// Users.

// userIDLocked returns a member's user_id, giving one to a user the gateway
// has not seen before: the next of the gateway's own that nobody has.
func (g *Gateway) userIDLocked(user *message.User) string {
	if id, ok := g.userIDs[user]; ok {
		return id
	}
	var id string
	for id == "" || g.userByIDLocked(id) != nil {
		g.nextID++
		id = fmt.Sprintf("u%d", g.nextID)
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

// userLocked returns a member's complete current user object (§3.3.1).
func (g *Gateway) userLocked(user *message.User) apron.User {
	u := apron.User{UserID: g.userIDLocked(user), Name: user.Name(), Status: apron.StatusOnline}
	if away, _, _ := user.GetAway(); away {
		u.Status = statusAway
	}
	if member, ok := g.host.Member(user); ok && member.IsOp() {
		u.Roles = []string{"op"}
	}
	return u
}

// recordedLocked returns the user object a record keeps of a member.
func (g *Gateway) recordedLocked(user *message.User) apron.User {
	return apron.User{UserID: g.userIDLocked(user), Name: user.Name()}
}

// membersLocked returns the room's members as clients were last told of
// them, as complete user objects.
func (g *Gateway) membersLocked() []apron.CurrentUser {
	users := []apron.CurrentUser{}
	for _, member := range g.roomMembers() {
		if u, ok := g.log.known[member.User]; ok {
			users = append(users, apron.CurrentUser{User: u})
		}
	}
	return users
}

// Rooms (§4.2): the one room, whose members are everyone connected.

func (g *Gateway) roomLocked() apron.Room {
	return apron.Room{RoomID: g.room, LogID: g.roomLogID, Description: g.host.Topic()}
}

func (g *Gateway) RoomList(_ context.Context, _ *server.Conn, p apron.RoomListParams, reply server.Reply[apron.RoomListResult]) error {
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
	room := apron.RoomView{Room: g.roomLocked(), LatestLogID: log.Latest, HistoryLogID: &log.Lower}
	result := apron.RoomListResult{Joined: []apron.RoomView{}}
	if p.Members {
		room.Members = g.membersLocked()
		if p.UserID != "" {
			room.Members = slices.DeleteFunc(room.Members, func(u apron.CurrentUser) bool { return u.UserID != p.UserID })
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

func (g *Gateway) RoomJoin(_ context.Context, _ *server.Conn, p apron.RoomJoinParams, _ server.Reply[apron.OK]) error {
	if p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.UserID != "" {
		return apron.Unsupported("Everyone connected is in the room")
	}
	return nil // Every connection is in the room.
}

// RoomLeave of another user is an op's /kick; a connection leaves the room
// by closing.
func (g *Gateway) RoomLeave(ctx context.Context, c *server.Conn, p apron.RoomLeaveParams, _ server.Reply[apron.OK]) error {
	if p.RoomID != g.room {
		return g.errOneRoom
	}
	s, _ := c.Data().(*session)
	if p.UserID == "" || p.UserID == c.UserID() {
		return apron.Denied("Everyone connected is in the room; disconnect to leave")
	}
	g.mu.Lock()
	target := g.userByIDLocked(p.UserID)
	g.mu.Unlock()
	if target == nil {
		return apron.InvalidParams("No such user in the room")
	}
	if !g.host.IsOp(s.user) {
		return apron.Denied("Only ops can remove people")
	}
	return g.run(ctx, s, "/kick "+target.ID())
}

func (g *Gateway) RoomSet(_ context.Context, _ *server.Conn, p apron.RoomSetParams, _ server.Reply[apron.RoomSetResult]) error {
	if p.RoomID == "" && p.Private != nil && *p.Private {
		return apron.Unsupported("This server has no private rooms")
	}
	return apron.Denied("This server has one room, which can't be changed")
}

// History (§4.3): the history buffer.

// historyLimit bounds a history page.
const historyLimit = 200

func (g *Gateway) History(_ context.Context, _ *server.Conn, p apron.HistoryParams, reply server.Reply[apron.HistoryResult]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.syncLocked()
	return reply.Send(server.Page(g.logLocked(), p, historyLimit))
}

// Status (§4.5): members are online, or away. ssh-chat has no
// notifications to hold back, so mutes and idle reports change nothing.

func (g *Gateway) Status(_ context.Context, c *server.Conn, p apron.StatusParams, _ server.Reply[apron.OK]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.Mute != nil {
		c.Notify(apron.StatusUpdate{RoomID: p.RoomID, Mute: *p.Mute})
	}
	return nil
}
