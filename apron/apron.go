// Package apron serves ssh-chat's room over the Apron chat protocol
// (https://github.com/shazow/apron), so that Apron apps can join the same
// chat as SSH users.
//
// ssh-chat stays the authority: an Apron connection becomes one more
// member of the Host's room, with an Identity like an SSH connection's, and
// what it posts goes through the room's HandleMsg, so commands, mutes,
// ignores, bans, and ops apply to it as to anyone. The protocol itself is
// package wire of github.com/apron-chat/apron-server-go.
//
// What maps where:
//
//   - The room is the server's one room, Room.
//   - A member is a user, with a user_id that stays the same across renames
//     for as long as the member is connected; the name is the member's name.
//   - Public messages and emotes are logged messages, with message_ids
//     assigned as the gateway first sees them.
//   - Announcements (joins, leaves, renames) are logged ~room notices, and
//     system messages to one user (command replies, errors) are ~private
//     notices.
//   - A private message is a transient message from its sender, marked in
//     ext.
//   - Commands arrive as command requests, and run as if typed over SSH.
//
// Sign-in is as a guest, only while ssh-chat lets in anonymous users: an
// allowlist, a passphrase, or --require-pubkey keeps Apron clients out.
package apron

import (
	"container/list"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apron-chat/apron-server-go/wire"
	"github.com/shazow/rateio"
	sshchat "github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/sanitize"
	"golang.org/x/crypto/ssh"
)

// Room is the room_id of ssh-chat's room.
const Room = "chat"

// maxInputLength is ssh-chat's limit on one line of input.
const maxInputLength = 1024

// Gateway is an Apron server for an ssh-chat Host.
type Gateway struct {
	host    *sshchat.Host
	auth    *sshchat.Auth
	version string
	server  *wire.Server
	clock   wire.Clock
	// limiter makes each member's input limiter, as for SSH users.
	limiter func() rateio.Limiter

	mu      sync.Mutex
	userIDs map[*message.User]string
	nextID  int
	guests  int
	// ids holds the message_ids of the latest messages, by their key.
	ids    map[msgKey]string
	idList *list.List
	// replyTo holds what the latest messages from Apron clients reply to,
	// which ssh-chat has no place for, by message_id.
	replyTo map[string]*wire.Ref
}

// New returns a Gateway that joins Apron clients to host's room. version
// is ssh-chat's, for the server frame.
func New(host *sshchat.Host, auth *sshchat.Auth, version string) *Gateway {
	g := &Gateway{
		host:    host,
		auth:    auth,
		version: version,
		userIDs: make(map[*message.User]string),
		ids:     make(map[msgKey]string),
		idList:  list.New(),
		replyTo: make(map[string]*wire.Ref),
		limiter: func() rateio.Limiter { return rateio.NewSimpleLimiter(3, 3*time.Second) },
	}
	g.server = &wire.Server{Backend: g}
	return g
}

// Server is the gateway's protocol server, for its settings, such as
// OriginPatterns.
func (g *Gateway) Server() *wire.Server { return g.server }

// Handler serves the WebSocket endpoint at /ws, and a line for browsers at
// /.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/ws", g.server)
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ssh-chat speaks the Apron protocol at /ws")
	})
	return mux
}

// Shutdown closes every Apron connection.
func (g *Gateway) Shutdown(ctx context.Context) error { return g.server.Shutdown(ctx) }

// session is an Apron connection's member of the room.
type session struct {
	user  *message.User
	limit rateio.Limiter
}

func (g *Gateway) Hello(*wire.Conn) wire.ServerInfo {
	info := wire.ServerInfo{
		Auth:  []string{"guest"},
		Agent: "ssh-chat/" + g.version,
		Title: "ssh-chat",
		Welcome: "Join as a guest, with the name you ask for. You chat with everyone " +
			"connected over SSH too; `/help` lists the commands.",
	}
	if motd := g.host.Motd(); motd != "" {
		info.Description = plain(motd)
	}
	return info
}

func (g *Gateway) Auth(ctx context.Context, c *wire.Conn, p wire.AuthParams) (wire.AuthResult, error) {
	if c.Data() != nil {
		return wire.AuthResult{}, wire.Denied("Already signed in")
	}
	if !g.auth.AllowAnonymous() {
		return wire.AuthResult{}, wire.Denied("This server lets in only SSH users with an allowed key")
	}
	client := "apron"
	if p.Agent != "" {
		client += " " + p.Agent
	}
	conn := &connection{conn: c, name: p.Name, client: client}
	if err := g.auth.CheckBans(conn.RemoteAddr(), nil, client); err != nil {
		return wire.AuthResult{}, wire.Denied("You are banned from this server")
	}

	id := sshchat.NewIdentity(conn)
	user := message.NewUser(id)
	cfg := user.Config()
	cfg.Theme = nil // Text goes out unrendered.
	user.SetConfig(cfg)
	g.mu.Lock()
	g.nextID++
	userID := fmt.Sprintf("u%d", g.nextID)
	g.userIDs[user] = userID
	g.guests++
	guest := fmt.Sprintf("Guest%d", g.guests)
	g.mu.Unlock()
	if id.ID() == "" {
		id.SetName(guest)
	}

	// Delivery starts before the join, which sends the room's history; what
	// it sends now goes out after the auth result.
	go func() {
		user.ConsumeFunc(func(m message.Message) { g.deliver(c, user, m) })
		// The member was closed, as by /kick or /exit.
		c.CloseWithError(wire.Denied("Disconnected from the chat"))
	}()
	if motd := g.host.Motd(); motd != "" {
		c.SendMessage(wire.Notice(Room, plain(motd)))
	}
	_, err := g.host.Join(user)
	if err != nil {
		id.SetName(guest)
		_, err = g.host.Join(user)
	}
	if err != nil {
		user.Close()
		g.forget(user)
		return wire.AuthResult{}, wire.Denied("Could not join the chat: " + err.Error())
	}
	c.SetData(&session{user: user, limit: g.limiter()})
	// The history and the announcement of the join are sent right after the
	// result, which they are held for.
	_ = g.settle(ctx, user)
	you := g.userOf(user)
	return wire.AuthResult{You: &you}, nil
}

func (g *Gateway) Disconnected(c *wire.Conn) {
	s, ok := c.Data().(*session)
	if !ok {
		return
	}
	_ = g.host.Leave(s.user)
	s.user.Close()
	g.forget(s.user)
}

func (g *Gateway) forget(user *message.User) {
	g.mu.Lock()
	delete(g.userIDs, user)
	g.mu.Unlock()
}

func (g *Gateway) Message(ctx context.Context, c *wire.Conn, p wire.MessageParams) (wire.MessageResult, error) {
	if p.RoomID != "" && p.RoomID != Room {
		return wire.MessageResult{}, wire.InvalidParams("This server has one room, " + Room)
	}
	if p.ReplyTo != nil && !g.known(p.ReplyTo.MessageID) {
		return wire.MessageResult{}, wire.InvalidParams("reply_to names no recent message")
	}
	s := c.Data().(*session)
	text := g.withNames(p.Body)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var first string
	for _, line := range lines {
		if line = strings.TrimRight(line, " \t"); line == "" {
			continue
		}
		if err := g.check(s, line); err != nil {
			return wire.MessageResult{MessageID: first}, err
		}
		m := message.NewPublicMsg(line, s.user)
		id := g.idOf(m)
		if first == "" {
			first = id
			if p.ReplyTo != nil {
				g.mu.Lock()
				g.replyTo[id] = p.ReplyTo
				g.mu.Unlock()
			}
		}
		g.host.HandleMsg(m)
	}
	// The member's own copy is sent before the result (§1).
	return wire.MessageResult{MessageID: first}, g.flush(ctx, s.user)
}

func (g *Gateway) Command(ctx context.Context, c *wire.Conn, p wire.MessageParams) (map[string]any, error) {
	s := c.Data().(*session)
	text := strings.TrimSpace(g.withNames(p.Body))
	if strings.ContainsAny(text, "\r\n") {
		return nil, wire.InvalidParams("A command is one line")
	}
	if err := g.check(s, text); err != nil {
		return nil, err
	}
	cmd, ok := message.NewPublicMsg(text, s.user).ParseCommand()
	if !ok {
		return nil, wire.InvalidParams("A command starts with /")
	}
	g.host.HandleMsg(cmd)
	// Replies to the command are sent before the result (§1, §4.1).
	err := g.settle(ctx, s.user)
	g.syncSelf(c, s.user)
	return nil, err
}

func (g *Gateway) Me(ctx context.Context, c *wire.Conn, p wire.MeParams) (wire.User, error) {
	s := c.Data().(*session)
	if p.Name != nil && sanitize.Name(*p.Name) != s.user.Name() {
		// ssh-chat's names are one short word; the server may normalize
		// what a client asks for (§1.1).
		name := sanitize.Name(*p.Name)
		if name == "" {
			return wire.User{}, wire.InvalidParams("A name needs letters, digits, '.', '_', or '-'")
		}
		cmd, _ := message.NewPublicMsg("/nick "+name, s.user).ParseCommand()
		g.host.HandleMsg(cmd)
		if err := g.settle(ctx, s.user); err != nil {
			return wire.User{}, err
		}
	}
	you := g.userOf(s.user)
	g.announce(c, you)
	return you, nil
}

// check applies ssh-chat's limits on input.
func (g *Gateway) check(s *session, line string) error {
	if len(line) > maxInputLength {
		return wire.TooLarge(fmt.Sprintf("A line is at most %d bytes", maxInputLength))
	}
	if s.limit.Count(1) != nil {
		return wire.RetryAfter("Rate limiting is in effect", 3)
	}
	return nil
}

// withNames returns a body's text with each mention written as the
// member's name, which is how SSH users see one another.
func (g *Gateway) withNames(body *wire.Body) string {
	text := body.Text
	for _, userID := range body.Mentions {
		if name := g.nameOf(userID); name != "" {
			text = strings.ReplaceAll(text, "@"+userID, name)
		}
	}
	return text
}

func (g *Gateway) nameOf(userID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	for user, id := range g.userIDs {
		if id == userID {
			return user.Name()
		}
	}
	return ""
}

// userOf returns a member's user object; one the gateway has not seen
// before gets a user_id.
func (g *Gateway) userOf(user *message.User) wire.User {
	g.mu.Lock()
	id, ok := g.userIDs[user]
	if !ok {
		g.nextID++
		id = fmt.Sprintf("u%d", g.nextID)
		g.userIDs[user] = id
	}
	g.mu.Unlock()
	return wire.User{UserID: id, Name: user.Name()}
}

// syncSelf tells a connection about a change to its own name, such as from
// /nick, or an op's /rename.
func (g *Gateway) syncSelf(c *wire.Conn, user *message.User) {
	you := g.userOf(user)
	if old := c.User(); old != nil && old.Name != you.Name {
		g.announce(c, you)
	}
}

// announce sends a connection's new user object to it and to the others.
func (g *Gateway) announce(c *wire.Conn, you wire.User) {
	c.SetUser(you)
	c.Notify("user", map[string]any{"you": you})
	if frame, err := wire.Encode("user", map[string]any{"new": you}); err == nil {
		g.server.Broadcast(frame, func(other *wire.Conn) bool { return other != c })
	}
}

// barrier is a message that marks a place in a member's queue.
type barrier struct {
	message.Msg
	to   *message.User
	done chan struct{}
}

// To makes a barrier a message to one member, which the room passes on.
func (b *barrier) To() *message.User { return b.to }

// settle waits for what the room is still sending a member, as well as it
// can. Replies to commands and announcements go through the room's queue,
// which hands each to a goroutine of its own, so they are not ordered: once
// a barrier sent the same way arrives, everything queued before it has a
// goroutine, which has most likely run after a moment's grace.
func (g *Gateway) settle(ctx context.Context, user *message.User) error {
	b := &barrier{to: user, done: make(chan struct{})}
	g.host.Send(b)
	select {
	case <-b.done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Second):
	}
	select {
	case <-time.After(settleGrace):
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.flush(ctx, user)
}

// settleGrace is how long settle gives the room's goroutines to deliver.
const settleGrace = 10 * time.Millisecond

// flush waits until a member's queue has been delivered up to now: the
// room hands a message to every member before HandleMsg returns, so what a
// request caused comes before its result.
func (g *Gateway) flush(ctx context.Context, user *message.User) error {
	b := &barrier{done: make(chan struct{})}
	if err := user.Send(b); err != nil {
		return wire.Denied("Disconnected from the chat")
	}
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliver sends a message from the room to an Apron connection.
func (g *Gateway) deliver(c *wire.Conn, user *message.User, m message.Message) {
	switch m := m.(type) {
	case *barrier:
		close(m.done)
		return
	case message.PublicMsg:
		g.sendSnapshot(c, m, g.userOf(m.From()), &wire.Body{Text: m.Body()}, nil)
	case *message.EmoteMsg:
		g.sendSnapshot(c, m, g.userOf(m.From()), &wire.Body{Text: "_" + m.Body() + "_", Format: wire.Markdown},
			map[string]any{"ssh-chat": map[string]any{"emote": true}})
	case *message.AnnounceMsg:
		g.sendSnapshot(c, m, wire.RoomNotice, &wire.Body{Text: plain(m.Body())}, nil)
	case *message.SystemMsg:
		c.SendMessage(wire.Notice(Room, plain(m.Body())))
	case *message.PrivateMsg:
		from := g.userOf(m.From())
		c.SendMessage(wire.Message{
			RoomID: Room, From: from, Body: &wire.Body{Text: m.Body()},
			Ext: map[string]any{"ssh-chat": map[string]any{"private": true, "to": g.userOf(m.To()).UserID}},
		})
	default:
		// Commands are never delivered to members; anything new is skipped.
		return
	}
	if fromMsg, ok := m.(message.MessageFrom); ok && fromMsg.From() == user {
		g.syncSelf(c, user)
	}
}

func (g *Gateway) sendSnapshot(c *wire.Conn, m message.Message, from wire.User, body *wire.Body, ext map[string]any) {
	id := g.idOf(m)
	g.mu.Lock()
	replyTo := g.replyTo[id]
	g.mu.Unlock()
	c.SendMessage(wire.Message{MessageID: id, LogID: id, RoomID: Room, From: from, Body: body, ReplyTo: replyTo, Ext: ext})
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

// recentIDs bounds the message_ids kept, well beyond ssh-chat's history.
const recentIDs = 4096

// idOf returns a message's message_id, assigning the next log_id the first
// time.
func (g *Gateway) idOf(m message.Message) string {
	k := keyOf(m)
	g.mu.Lock()
	defer g.mu.Unlock()
	if id, ok := g.ids[k]; ok {
		return id
	}
	id := g.clock.Next()
	g.ids[k] = id
	g.idList.PushBack(k)
	for g.idList.Len() > recentIDs {
		oldest := g.idList.Remove(g.idList.Front()).(msgKey)
		delete(g.replyTo, g.ids[oldest])
		delete(g.ids, oldest)
	}
	return id
}

func (g *Gateway) known(messageID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range g.ids {
		if id == messageID {
			return true
		}
	}
	return false
}

// plain turns ssh-chat's terminal line breaks into newlines.
func plain(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, message.Newline, "\n"))
}

// connection is the sshd.Connection of an Apron client, for its Identity.
type connection struct {
	conn   *wire.Conn
	name   string
	client string
}

func (c *connection) PublicKey() ssh.PublicKey { return nil }
func (c *connection) Name() string             { return c.name }
func (c *connection) ClientVersion() []byte    { return []byte(c.client) }
func (c *connection) Close() error             { c.conn.Close(); return nil }

// RemoteAddr is the client's address, or a placeholder for a transport
// without one.
func (c *connection) RemoteAddr() net.Addr {
	if addr := c.conn.RemoteAddr(); addr != nil {
		return addr
	}
	return &net.TCPAddr{IP: net.IPv4zero}
}
