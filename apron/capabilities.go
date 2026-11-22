package apron

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/apron-chat/apron-server-go/wire"

	"github.com/shazow/ssh-chat/chat/message"
)

// Users.

// userIDLocked returns a member's user_id, giving one to a user the gateway
// has not seen before.
func (g *Gateway) userIDLocked(user *message.User) string {
	id, ok := g.userIDs[user]
	if !ok {
		g.nextID++
		id = fmt.Sprintf("u%d", g.nextID)
		g.userIDs[user] = id
	}
	return id
}

func (g *Gateway) userByIDLocked(userID string) *message.User {
	for user, id := range g.userIDs {
		if id == userID {
			return user
		}
	}
	return nil
}

// userLocked returns a member's complete current user object (§3.3.1).
func (g *Gateway) userLocked(user *message.User) wire.User {
	u := wire.User{UserID: g.userIDLocked(user), Name: user.Name(), Status: wire.StatusOnline}
	if away, _, _ := user.GetAway(); away {
		u.Status = statusAway
	}
	if member, ok := g.host.Member(user); ok && member.IsOp.Load() {
		u.Roles = []string{"op"}
	}
	return u
}

// recordedLocked returns the user object a message records as its sender.
func (g *Gateway) recordedLocked(user *message.User) wire.User {
	return wire.User{UserID: g.userIDLocked(user), Name: user.Name()}
}

func sameUser(a, b wire.User) bool {
	return a.Name == b.Name && a.Status == b.Status && slices.Equal(a.Roles, b.Roles)
}

// membersLocked returns the room's members' current user objects.
func (g *Gateway) membersLocked() []wire.User {
	var users []wire.User
	for _, member := range g.roomMembers() {
		users = append(users, g.userLocked(member.User))
	}
	return users
}

// announce sends changed user objects: as you to the user's own
// connection, and as new to the others (§3.3.3).
func (g *Gateway) announce(changed []wire.User) {
	for _, u := range changed {
		frame, err := wire.Encode("user", map[string]any{"new": u})
		if err != nil {
			continue
		}
		for _, c := range g.server.Conns() {
			if c.UserID() == u.UserID {
				c.SetUser(u)
				c.Notify("user", map[string]any{"you": u})
			} else if c.UserID() != "" {
				c.Send(frame)
			}
		}
	}
}

// Rooms (§4.2): the one room, whose members are everyone connected.

func (g *Gateway) roomLocked(withMembers bool) wire.Room {
	room := wire.Room{RoomID: Room, LogID: g.roomLogID, Description: g.host.Topic()}
	if withMembers {
		room.Members = g.membersLocked()
	}
	return room
}

func (g *Gateway) RoomList(_ context.Context, c *wire.Conn, p wire.RoomListParams) (wire.RoomListResult, error) {
	if p.RoomID != "" && p.RoomID != Room {
		return wire.RoomListResult{}, wire.InvalidParams("This server has one room, " + Room)
	}
	if p.ParentRoomID != "" && p.ParentRoomID != Room {
		return wire.RoomListResult{}, wire.InvalidParams("This server has one room, " + Room)
	}
	g.mu.Lock()
	changed := g.syncLocked()
	_, lower, latest := g.logLocked()
	room := g.roomLocked(p.Members)
	g.mu.Unlock()
	g.announce(changed)
	room.LatestLogID = strconv.FormatInt(latest, 10)
	h := strconv.FormatInt(lower, 10)
	room.HistoryLogID = &h

	result := wire.RoomListResult{Joined: []wire.Room{}}
	if p.ParentRoomID == "" {
		// The room has no threads.
		result.Joined = append(result.Joined, room)
	}
	if p.Members {
		result.Users = room.Members
		if p.UserID != "" {
			room.Members = slices.DeleteFunc(room.Members, func(u wire.User) bool { return u.UserID != p.UserID })
			result.Users = room.Members
		}
	}
	return result, nil
}

func (g *Gateway) RoomJoin(_ context.Context, _ *wire.Conn, roomID, userID string) error {
	if roomID != Room {
		return wire.InvalidParams("This server has one room, " + Room)
	}
	if userID != "" {
		return wire.Unsupported("Everyone connected is in the room")
	}
	return nil // Every connection is in the room.
}

// RoomLeave of another user is an op's /kick; a connection leaves the room
// by closing.
func (g *Gateway) RoomLeave(ctx context.Context, c *wire.Conn, roomID, userID string) error {
	if roomID != Room {
		return wire.InvalidParams("This server has one room, " + Room)
	}
	s := sessionOf(c)
	if userID == "" || userID == c.UserID() {
		return wire.Denied("Everyone connected is in the room; disconnect to leave")
	}
	g.mu.Lock()
	target := g.userByIDLocked(userID)
	g.mu.Unlock()
	if target == nil {
		return wire.InvalidParams("No such user in the room")
	}
	if !g.host.IsOp(s.user) {
		return wire.Denied("Only ops can remove people")
	}
	return g.run(ctx, s, "/kick "+target.ID())
}

func (g *Gateway) RoomSet(_ context.Context, _ *wire.Conn, p wire.RoomSetParams) (string, error) {
	if p.RoomID == "" && p.Private != nil && *p.Private {
		return "", wire.Unsupported("This server has no private rooms")
	}
	return "", wire.Denied("This server has one room, which can't be changed")
}

// History (§4.3): the history buffer.

// historyLimit bounds a history page.
const historyLimit = 200

func (g *Gateway) History(_ context.Context, _ *wire.Conn, p wire.HistoryParams) (wire.HistoryResult, error) {
	if p.RoomID != "" && p.RoomID != Room {
		return wire.HistoryResult{}, wire.InvalidParams("This server has one room, " + Room)
	}
	g.mu.Lock()
	changed := g.syncLocked()
	records, lower, latest := g.logLocked()
	g.mu.Unlock()
	g.announce(changed)
	return wire.Page(records, p, historyLimit, lower, latest), nil
}

// Status (§4.5): members are online, or away. ssh-chat has no
// notifications to hold back, so mutes and idle reports change nothing.

func (g *Gateway) Status(_ context.Context, c *wire.Conn, p wire.StatusParams) error {
	if p.RoomID != "" && p.RoomID != Room {
		return wire.InvalidParams("This server has one room, " + Room)
	}
	if p.Mute != nil {
		update := map[string]any{"mute": *p.Mute}
		if p.RoomID != "" {
			update["room_id"] = p.RoomID
		}
		c.Notify("status", update)
	}
	return nil
}
