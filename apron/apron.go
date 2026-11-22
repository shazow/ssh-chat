// Package apron serves ssh-chat's room over the Apron chat protocol
// (https://github.com/shazow/apron), so that Apron apps can join the same
// chat as SSH users.
//
// ssh-chat stays the authority: an Apron connection becomes one more
// member of the Host's room, with an Identity like an SSH connection's, and
// what it posts goes through the room's HandleMsg, so commands, mutes,
// ignores, bans, ops, and rate limits apply to it as to anyone. The protocol
// itself is package wire of github.com/apron-chat/apron-server-go.
//
// What maps where:
//
//   - The room is the server's one room, Room, and its log is the room's
//     history buffer: history pages it, and it ends where the buffer does.
//   - A member is a user, with a user_id that stays the same across renames
//     for as long as the member is around; the name is the member's name,
//     ops have role "op", and away members have status "away".
//   - Public messages and emotes are logged messages, with message_ids
//     derived from their times as the gateway first sees them.
//   - Joins and leaves are memberships; other announcements are logged
//     ~room notices; system messages to one user, such as command replies,
//     are ~private notices.
//   - A private message is a transient message from its sender, marked in
//     ext.
//   - Commands arrive as command requests, and run as if typed over SSH;
//     me sets the name with /nick and status away with /away, and an op's
//     room_leave of another user is /kick.
//
// Sign-in is as a guest, only while ssh-chat lets in anonymous users: an
// allowlist, a passphrase, or --require-pubkey keeps Apron clients out.
package apron

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apron-chat/apron-server-go/wire"
	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"

	sshchat "github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/sanitize"
)

// Room is the room_id of ssh-chat's room.
const Room = "chat"

// maxInputLength is ssh-chat's limit on one line of input.
const maxInputLength = 1024

// statusAway is the status of a member who is /away.
const statusAway = "away"

// Config is what a Gateway serves.
type Config struct {
	Host *sshchat.Host
	Auth *sshchat.Auth
	// History is the room's history buffer, such as Host.HistoryBuffer():
	// the log that Apron clients page with history.
	History *message.History
	// Version is ssh-chat's, for the server frame.
	Version string
	// Origins are the web origins whose pages may open an Apron WebSocket,
	// such as https://web.apron.chat, as patterns of wire.OriginPatterns;
	// "*" allows any. Clients that are not browsers send no origin, and
	// are always let in.
	Origins []string
}

// Gateway is an Apron server for an ssh-chat Host.
type Gateway struct {
	host    *sshchat.Host
	auth    *sshchat.Auth
	history *message.History
	version string
	server  *wire.Server
	clock   wire.Clock
	ids     wire.IDs[msgKey]
	// roomLogID is the log_id of the room's record, issued when the gateway
	// started, before any message's.
	roomLogID wire.LogID
	// limiter makes each member's input limiter, as for SSH users.
	limiter func() rateio.Limiter
	// stop ends watch.
	stop     chan struct{}
	stopOnce sync.Once
	// afterAuth, a test's, runs once a sign-in's result is queued, before
	// Auth returns.
	afterAuth func(*wire.Conn)

	mu      sync.Mutex
	userIDs map[*message.User]string
	nextID  int
	guests  int
	log     logState
}

// New returns a Gateway that joins Apron clients to the host's room.
func New(config Config) *Gateway {
	g := &Gateway{
		host:    config.Host,
		auth:    config.Auth,
		history: config.History,
		version: config.Version,
		userIDs: make(map[*message.User]string),
		limiter: func() rateio.Limiter { return rateio.NewSimpleLimiter(3, 3*time.Second) },
	}
	if g.history == nil {
		g.history = message.NewHistory(0)
	}
	g.ids.Clock = &g.clock
	g.roomLogID = g.clock.Next()
	g.log.init()
	checkOrigin := wire.OriginPatterns(config.Origins...)
	if slices.Contains(config.Origins, "*") {
		checkOrigin = wire.AnyOrigin
	}
	g.server = &wire.Server{
		Backend:      g,
		Messages:     g,
		Commands:     g,
		Profiles:     g,
		Rooms:        g,
		History:      g,
		Status:       g,
		OnDisconnect: g.disconnected,
		CheckOrigin:  checkOrigin,
	}
	g.stop = make(chan struct{})
	g.mu.Lock()
	g.syncLocked()
	g.mu.Unlock()
	go g.watch()
	return g
}

// watchInterval is how often the gateway looks at the room while no Apron
// client gives it a reason to.
const watchInterval = time.Second

// watch keeps up with the room while nobody is connected over Apron, so that
// a join or leave is recognized while its member is still known.
func (g *Gateway) watch() {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			g.mu.Lock()
			g.syncLocked()
			g.mu.Unlock()
		}
	}
}

// Server is the gateway's protocol server, for its settings, which are
// set before it serves its first connection.
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
func (g *Gateway) Shutdown(ctx context.Context) error {
	g.stopOnce.Do(func() { close(g.stop) })
	return g.server.Shutdown(ctx)
}

// session is an Apron connection's member of the room, the connection's
// Data once it has signed in.
type session struct {
	user  *message.User
	limit rateio.Limiter
}

// Errors the gateway answers with in more than one place.
var (
	errOneRoom      = wire.InvalidParams("This server has one room, " + Room)
	errDisconnected = wire.Denied("Disconnected from the chat")
)

func (g *Gateway) Hello(context.Context, *wire.Conn) wire.ServerInfo {
	info := wire.ServerInfo{
		Auth:  []string{"guest"},
		Agent: "ssh-chat/" + g.version,
		Title: "ssh-chat",
		Welcome: "Join as a guest, with the name you ask for. You chat with everyone " +
			"connected over SSH too; `/help` lists the commands.",
		Status: []string{statusAway},
	}
	if motd := g.host.Motd(); motd != "" {
		info.Description = plain(motd)
	}
	return info
}

func (g *Gateway) Auth(ctx context.Context, c *wire.Conn, p wire.AuthParams, reply wire.Reply[wire.AuthResult]) error {
	if c.Data() != nil {
		return wire.Denied("Already signed in")
	}
	if !g.auth.AllowAnonymous() {
		return wire.Denied("This server lets in only SSH users with an allowed key")
	}
	client := "apron"
	if p.Agent != "" {
		client += " " + p.Agent
	}
	conn := &connection{conn: c, name: p.Name, client: client}
	if err := g.auth.CheckBans(conn.RemoteAddr(), nil, client); err != nil {
		return wire.Denied("You are banned from this server")
	}

	id := sshchat.NewIdentity(conn)
	user := message.NewUser(id)
	cfg := user.Config()
	cfg.Theme = nil // Text goes out unrendered.
	user.SetConfig(cfg)
	g.mu.Lock()
	g.guests++
	guest := fmt.Sprintf("Guest%d", g.guests)
	g.mu.Unlock()
	if id.ID() == "" {
		id.SetName(guest)
	}

	// Delivery starts before the join, which sends the room's history; what
	// it sends now goes out after the auth result (§3.2.2).
	go func() {
		user.ConsumeFunc(func(m message.Message) { g.deliver(c, m) })
		// The member was closed, as by /kick or /exit.
		c.CloseWithError(errDisconnected)
	}()
	if motd := g.host.Motd(); motd != "" {
		c.Notify(wire.Notice(Room, plain(motd)))
	}
	_, err := g.host.Join(user)
	if err != nil {
		id.SetName(guest)
		_, err = g.host.Join(user)
	}
	if err != nil {
		user.Close()
		return wire.Denied("Could not join the chat: " + err.Error())
	}
	c.SetData(&session{user: user, limit: g.limiter()})
	if err := g.settle(ctx, user); err != nil {
		return err
	}

	g.mu.Lock()
	err = g.signInLocked(user, reply)
	g.mu.Unlock()
	if g.afterAuth != nil {
		g.afterAuth(c)
	}
	return err
}

// signInLocked answers a sign-in with the member's user object, followed by
// the others in the room, with their statuses (§4.5.4). It replies under
// g.mu, which signs the connection in as the result is queued, so that
// every user notification sent after it reaches the connection too.
func (g *Gateway) signInLocked(user *message.User, reply wire.Reply[wire.AuthResult]) error {
	g.syncLocked()
	you, ok := g.log.known[user]
	if !ok {
		return errDisconnected
	}
	var others []wire.Frame
	for _, other := range g.membersLocked() {
		if other.UserID == you.UserID {
			continue
		}
		frame, err := wire.Encode(wire.UserUpdate{New: &wire.CurrentUser{User: other}})
		if err != nil {
			return err
		}
		others = append(others, frame)
	}
	return reply.Send(wire.AuthResult{You: &you}, others...)
}

func (g *Gateway) disconnected(c *wire.Conn) {
	s, ok := wire.DataOf[*session](c)
	if !ok {
		return
	}
	_ = g.host.Leave(s.user)
	s.user.Close()
}

func (g *Gateway) Message(ctx context.Context, c *wire.Conn, p wire.MessageParams, reply wire.Reply[wire.MessageResult]) error {
	if p.RoomID != "" && p.RoomID != Room {
		return errOneRoom
	}
	if p.ReplyTo != nil {
		if _, ok := g.ids.Key(p.ReplyTo.MessageID); !ok {
			return wire.InvalidParams("reply_to names no recent message")
		}
	}
	s, _ := wire.DataOf[*session](c)
	lines := strings.Split(strings.ReplaceAll(g.withNames(p.Body), "\r\n", "\n"), "\n")
	var posted []message.PublicMsg
	var err error
	for _, line := range lines {
		if line = strings.TrimRight(line, " \t"); line == "" {
			continue
		}
		if err = g.check(s, line); err != nil {
			break
		}
		m := message.NewPublicMsg(line, s.user)
		if p.ReplyTo != nil && len(posted) == 0 {
			g.mu.Lock()
			g.log.replyTo[keyOf(m)] = p.ReplyTo
			g.mu.Unlock()
		}
		posted = append(posted, m)
		g.host.HandleMsg(m)
	}
	// The member's own copy is sent before the result (§1).
	if flushErr := g.flush(ctx, s.user); err == nil {
		err = flushErr
	}
	if err != nil {
		return err
	}
	var result wire.MessageResult
	if len(posted) > 0 {
		result.MessageID, _ = g.ids.Lookup(keyOf(posted[0]))
	}
	return reply.Send(result)
}

func (g *Gateway) Command(ctx context.Context, c *wire.Conn, p wire.CommandParams, reply wire.Reply[wire.CommandResult]) error {
	s, _ := wire.DataOf[*session](c)
	text := strings.TrimSpace(g.withNames(p.Body))
	if strings.ContainsAny(text, "\r\n") {
		return wire.InvalidParams("A command is one line")
	}
	if err := g.check(s, text); err != nil {
		return err
	}
	if err := g.run(ctx, s, text); err != nil {
		return err
	}
	return reply.Send(wire.CommandResult{})
}

// run runs a command line as the member typed it, and waits for what it
// sends the member.
func (g *Gateway) run(ctx context.Context, s *session, line string) error {
	cmd, ok := message.NewPublicMsg(line, s.user).ParseCommand()
	if !ok {
		return wire.InvalidParams("A command starts with /")
	}
	g.host.HandleMsg(cmd)
	// Replies to the command are sent before the result (§1, §4.1).
	return g.settle(ctx, s.user)
}

func (g *Gateway) Me(ctx context.Context, c *wire.Conn, p wire.MeParams, reply wire.Reply[wire.CurrentUser]) error {
	s, _ := wire.DataOf[*session](c)
	before := c.User()
	if p.Name != nil && sanitize.Name(*p.Name) != s.user.Name() {
		// ssh-chat's names are one short word; the server may normalize
		// what a client asks for (§1.1).
		name := sanitize.Name(*p.Name)
		if name == "" {
			return wire.InvalidParams("A name needs letters, digits, '.', '_', or '-'")
		}
		if err := g.run(ctx, s, "/nick "+name); err != nil {
			return err
		}
	}
	if p.Status != nil {
		away, _, _ := s.user.GetAway()
		switch {
		case *p.Status == statusAway && !away:
			if err := g.run(ctx, s, "/away away"); err != nil {
				return err
			}
		case *p.Status != statusAway && away:
			if err := g.run(ctx, s, "/back"); err != nil {
				return err
			}
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.syncLocked()
	you, ok := g.log.known[s.user]
	if !ok {
		return errDisconnected
	}
	// The complete object, with what the request cleared (§3.3.2).
	patch, _ := you.Patch(*before)
	return reply.Send(wire.CurrentUser{User: you, Cleared: patch.Cleared})
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
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, userID := range body.Mentions {
		for user, id := range g.userIDs {
			if id == userID {
				text = strings.ReplaceAll(text, "@"+userID, user.Name())
			}
		}
	}
	return text
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
		return errDisconnected
	}
	select {
	case <-b.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliver sends a message from the room to an Apron connection. It sends
// under g.mu, as every other change the gateway tells clients of, so that
// the user notifications a sync sends and the records keep their order.
func (g *Gateway) deliver(c *wire.Conn, m message.Message) {
	if b, ok := m.(*barrier); ok {
		close(b.done)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch m := m.(type) {
	case *message.SystemMsg:
		c.Notify(wire.Notice(Room, plain(m.Body())))
	case *message.PrivateMsg:
		from, to := g.userIDLocked(m.From()), g.userIDLocked(m.To())
		c.Notify(wire.Message{
			RoomID: Room, From: wire.User{UserID: from, Name: m.From().Name()}, Body: &wire.Body{Text: m.Body()},
			Ext: wire.MustExt("ssh-chat", map[string]any{"private": true, "to": to}),
		})
	case message.PublicMsg, *message.EmoteMsg, *message.AnnounceMsg:
		g.syncLocked()
		// A muted member's own message is not in the history, and gets
		// its message_id here.
		record, ok := g.recordLocked(m, g.ids.ID(keyOf(m), m.Timestamp()))
		switch {
		case !ok:
		case record.Membership != nil:
			c.Notify(wire.RoomUpdate{Memberships: []wire.Membership{*record.Membership}})
		default:
			c.Notify(*record.Message)
		}
	}
	// Commands are never delivered to members; anything new is skipped.
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
