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
//   - The room is the server's one room, Config.Room, and its log is the room's
//     history buffer: history pages it, and it ends where the buffer does,
//     with a membership record of everyone in the room once the buffer
//     has dropped the joins.
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
//     ext:command_list lists those that ssh-chat's /help does;
//     me sets the name with /nick and status away with /away, and an op's
//     room_leave of another user is /kick.
//
// Sign-in is as a guest, only while ssh-chat lets in anonymous users: an
// allowlist, a passphrase, or --require-pubkey keeps Apron clients out.
//
// A bot of the server's own, of package bot of wire, runs on the gateway's
// Server: a Local connection is a member from the start, named as it asks,
// or as a guest when the name is taken, with a user_id of the gateway's
// own, which wire tells it of (Bot.User); its Data is the gateway's.
//
//	echo, err := bot.Start(gateway.Server(), bot.Options{
//		User: wire.User{UserID: "echo", Name: "echo"},
//		OnMessage: func(ctx context.Context, b *bot.Bot, m wire.Message) { ... },
//	})
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
	"github.com/apron-chat/apron-server-go/wire/commands"
	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"

	sshchat "github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/sanitize"
)

// DefaultRoom is the room_id of ssh-chat's room, unless Config.Room names
// another.
const DefaultRoom = "ssh-chat"

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
	// Room is the room_id of ssh-chat's room: DefaultRoom when empty, and
	// else a valid ID (wire.ValidID).
	Room string
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
	ids     wire.MessageIDs[msgKey]
	// room is the room_id of ssh-chat's room, and errOneRoom the error of a
	// request that names another.
	room       string
	errOneRoom *wire.Error
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
	// beforeDeliver, a test's, runs as a message from the room is about to
	// be delivered to a connection, before deliver takes g.mu.
	beforeDeliver func(*wire.Conn, message.Message)

	mu      sync.Mutex
	userIDs map[*message.User]string
	// members are the users of the Apron connections that have joined the
	// room, with their connections.
	members map[*message.User]*wire.Conn
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
		members: make(map[*message.User]*wire.Conn),
		limiter: func() rateio.Limiter { return rateio.NewSimpleLimiter(3, 3*time.Second) },
	}
	if g.history == nil {
		g.history = message.NewHistory(0)
	}
	g.room = config.Room
	if g.room == "" {
		g.room = DefaultRoom
	}
	if !wire.ValidID(g.room) {
		panic("apron: Config.Room is not a valid room_id: " + g.room)
	}
	g.errOneRoom = wire.InvalidParams("This server has one room, " + g.room)
	g.ids.Clock = &g.clock
	g.roomLogID = g.clock.Next()
	g.log.init()
	checkOrigin := wire.OriginPatterns(config.Origins...)
	if slices.Contains(config.Origins, "*") {
		checkOrigin = wire.AnyOrigin
	}
	router := g.newRouter(g.host.Commands())
	g.server = wire.NewServer(g, wire.Config{
		Messages:     g,
		Commands:     router,
		Profiles:     g,
		Rooms:        g,
		History:      g,
		Status:       g,
		OnConnect:    g.connected,
		OnDisconnect: g.disconnected,
		CheckOrigin:  checkOrigin,
	})
	g.server.Handle(commands.ListMethod, router.ListHandler())
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

// Server is the gateway's protocol server, made with its configuration
// (wire.NewServer), for Local connections, such as bots' (see the package
// documentation), and methods of a program's own (wire.Server.Handle).
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
	user, err := g.admit(ctx, c, conn)
	if err != nil {
		return err
	}

	// The reply is sent under g.mu, which signs the connection in as the
	// result is queued, so that every user notification sent after it
	// reaches the connection too.
	g.mu.Lock()
	you, others, err := g.signInLocked(user)
	if err == nil {
		err = reply.Send(wire.AuthResult{You: &wire.CurrentUser{User: you}}, others...)
	}
	g.mu.Unlock()
	if g.afterAuth != nil {
		g.afterAuth(c)
	}
	return err
}

// connected admits a Local connection (wire.Server.Local), such as a bot's
// (wire/bot), which opens signed in as the user it asks to be: its member
// joins the room as an Apron client's does when it signs in, named as the
// user is, or as a guest when the name is taken, and the connection
// becomes the member's user, with the gateway's user_id, which wire tells
// it of. The program that opens it is the server's own, so an allowlist or
// a ban does not keep it out. A network connection signs in with auth
// instead.
func (g *Gateway) connected(ctx context.Context, c *wire.Conn) error {
	if !c.Local() {
		return nil
	}
	u := c.User()
	name := u.Name
	if name == "" {
		name = u.UserID
	}
	user, err := g.admit(ctx, c, &connection{conn: c, name: name, client: "apron local"})
	if err != nil {
		return err // OnDisconnect undoes what admit did.
	}
	// What a sign-in would send: frames sent while wire opens the
	// connection go after its server frame, and the user, which wire
	// tells it of.
	g.mu.Lock()
	defer g.mu.Unlock()
	you, others, err := g.signInLocked(user)
	if err == nil {
		c.SetUser(you)
		c.Send(others...)
	}
	return err
}

// admit joins a connection's member to the room, as a guest when the name
// it asks for is taken, and starts delivering to the connection what the
// room sends the member.
func (g *Gateway) admit(ctx context.Context, c *wire.Conn, conn *connection) (*message.User, error) {
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
	// it sends now goes out after the auth result (§3.2.2), or, when the
	// sign-in fails, not at all. Once the member has joined, its end, as by
	// /kick or /exit, ends the connection; a member that never joined
	// leaves the connection as it was, for another auth.
	joined := make(chan bool, 1)
	delivered := make(chan struct{})
	go func() {
		defer close(delivered)
		var last wire.LogID
		user.ConsumeFunc(func(m message.Message) { g.deliver(c, user, &last, m) })
		if <-joined {
			c.CloseWithError(errDisconnected)
		}
	}()
	if motd := g.host.Motd(); motd != "" {
		c.Notify(wire.Notice(g.room, plain(motd)))
	}
	_, err := g.host.Join(user)
	if err != nil {
		id.SetName(guest)
		_, err = g.host.Join(user)
	}
	if err != nil {
		joined <- false
		user.Close()
		// Nothing more is delivered once Auth returns.
		<-delivered
		return nil, wire.Denied("Could not join the chat: " + err.Error())
	}
	joined <- true
	c.SetData(&session{user: user, limit: g.limiter()})
	g.mu.Lock()
	g.members[user] = c
	g.mu.Unlock()
	if err := g.settle(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// signInLocked returns what a sign-in tells the member: its user object,
// and the others in the room, with their statuses (§4.5.4).
func (g *Gateway) signInLocked(user *message.User) (wire.User, []wire.Frame, error) {
	g.syncLocked()
	you, ok := g.log.known[user]
	if !ok {
		return wire.User{}, nil, errDisconnected
	}
	var others []wire.Frame
	for _, other := range g.membersLocked() {
		if other.UserID == you.UserID {
			continue
		}
		frame, err := wire.Encode(wire.UserUpdate{New: &other})
		if err != nil {
			return wire.User{}, nil, err
		}
		others = append(others, frame)
	}
	return you, others, nil
}

func (g *Gateway) disconnected(c *wire.Conn) {
	s, ok := c.Data().(*session)
	if !ok {
		return
	}
	g.mu.Lock()
	delete(g.members, s.user)
	g.mu.Unlock()
	_ = g.host.Leave(s.user)
	s.user.Close()
}

func (g *Gateway) Message(ctx context.Context, c *wire.Conn, p wire.MessageParams, reply wire.Reply[wire.MessageResult]) error {
	if p.RoomID != "" && p.RoomID != g.room {
		return g.errOneRoom
	}
	if p.ReplyTo != nil {
		if _, ok := g.ids.Key(p.ReplyTo.MessageID); !ok {
			return wire.InvalidParams("reply_to names no recent message")
		}
	}
	s, _ := c.Data().(*session)
	// Every new message's result has its message_id, so one ssh-chat
	// would post nothing of, as with no text or only blank lines, is
	// refused (§3.5.1).
	lines := strings.Split(strings.ReplaceAll(g.withNames(p.Body), "\r\n", "\n"), "\n")
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.TrimRight(line, " \t") != "" }) {
		return wire.InvalidParams("A new message needs text")
	}
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
	// The member's own copy is sent before the result (§1), and so is
	// every other Apron member's, so that what the poster does once it has
	// the result, such as tell another member, comes after the message.
	if len(posted) > 0 {
		g.flushOthers(ctx, s.user)
	}
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

// run runs a command line as the member typed it, and waits for what it
// sends the member.
func (g *Gateway) run(ctx context.Context, s *session, line string) error {
	if err := g.start(s, line); err != nil {
		return err
	}
	// Replies to the command are sent before the result (§1, §4.1).
	return g.settle(ctx, s.user)
}

// start runs a command line as the member typed it.
func (g *Gateway) start(s *session, line string) error {
	cmd, ok := message.NewPublicMsg(line, s.user).ParseCommand()
	if !ok {
		return wire.InvalidParams("A command starts with /")
	}
	g.host.HandleMsg(cmd)
	return nil
}

func (g *Gateway) Me(ctx context.Context, c *wire.Conn, p wire.MeParams, reply wire.Reply[wire.CurrentUser]) error {
	s, _ := c.Data().(*session)
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
	return reply.Send(you.Complete(*before))
}

// check applies ssh-chat's limits on input.
func (g *Gateway) check(s *session, line string) error {
	if len(line) > maxInputLength {
		return wire.TooLarge(fmt.Sprintf("A line is at most %d bytes", maxInputLength))
	}
	if s.limit.Count(1) != nil {
		return wire.RetryAfter("Rate limiting is in effect", 3*time.Second)
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

// flushOthers waits until the queue of every Apron member but one has been
// delivered up to now, as flush does a member's own. The room hands a
// message to every member before HandleMsg returns, but only to their
// queues, which goroutines of the gateway's deliver to the connections
// later: until then, a member that has seen the poster's result may ask
// for the room's history, and find the message there before it arrives
// live (§1, §3.5.3). A member that goes away is not waited for.
func (g *Gateway) flushOthers(ctx context.Context, except *message.User) {
	type waiting struct {
		b    *barrier
		conn *wire.Conn
	}
	g.mu.Lock()
	others := make(map[*message.User]*wire.Conn, len(g.members))
	for user, conn := range g.members {
		if user != except {
			others[user] = conn
		}
	}
	g.mu.Unlock()
	// Under g.mu no more: delivering takes it.
	var queued []waiting
	for user, conn := range others {
		b := &barrier{done: make(chan struct{})}
		if user.Send(b) == nil {
			queued = append(queued, waiting{b, conn})
		}
	}
	for _, w := range queued {
		select {
		case <-w.b.done:
		case <-w.conn.Done():
		case <-ctx.Done():
			return
		}
	}
}

// deliver sends a message from the room to an Apron connection, its
// member's, which has had the records up to log_id last. It sends under
// g.mu, as every other change the gateway tells clients of, so that the
// user notifications a sync sends and the records keep their order.
func (g *Gateway) deliver(c *wire.Conn, user *message.User, last *wire.LogID, m message.Message) {
	if b, ok := m.(*barrier); ok {
		close(b.done)
		return
	}
	if g.beforeDeliver != nil {
		g.beforeDeliver(c, m)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch m := m.(type) {
	case *message.SystemMsg:
		c.Notify(wire.Notice(g.room, plain(m.Body())))
	case *message.PrivateMsg:
		from, to := g.userIDLocked(m.From()), g.userIDLocked(m.To())
		c.Notify(wire.Message{
			RoomID: g.room, From: wire.User{UserID: from, Name: m.From().Name()}, Body: &wire.Body{Text: m.Body()},
			Ext: wire.Ext{}.With("ssh-chat", map[string]any{"private": true, "to": to}),
		})
	case message.PublicMsg, *message.EmoteMsg, *message.AnnounceMsg:
		g.syncLocked()
		// A muted member's own message is not in the history, and gets
		// its message_id here.
		id := g.ids.ID(keyOf(m), m.Timestamp())
		if id <= *last {
			return // Delivered already, ahead of its turn.
		}
		// The room adds a message to the buffer, the log, and then hands
		// it to the members, in the poster's goroutine or one of its own,
		// so two at once may reach a member in the other order. Those
		// the buffer has before this one, which the member is due but has
		// not had, go first, and are skipped when they come (§3.5.3).
		for _, e := range g.log.entries {
			if eid, ok := g.ids.Lookup(keyOf(e)); ok && eid > *last && eid < id && due(user, e) {
				g.liveLocked(c, e, eid)
			}
		}
		g.liveLocked(c, m, id)
		*last = id
	}
	// Commands are never delivered to members; anything new is skipped.
}

// liveLocked sends a connection the record of a message, with log_id id.
func (g *Gateway) liveLocked(c *wire.Conn, m message.Message, id wire.LogID) {
	record, ok := g.recordLocked(m, id)
	switch {
	case !ok:
	case record.Membership != nil:
		c.Notify(wire.RoomUpdate{Memberships: []wire.Membership{*record.Membership}})
	default:
		c.Notify(*record.Message)
	}
}

// due returns whether the room hands a message of its buffer to a member,
// as Room.HandleMsg decides: not one from someone the member ignores, nor
// an announcement while the member is quiet.
func due(user *message.User, m message.Message) bool {
	if f, ok := m.(message.MessageFrom); ok && user.Ignored.In(f.From().ID()) {
		return false
	}
	if _, ok := m.(*message.AnnounceMsg); ok && user.Config().Quiet {
		return false
	}
	return true
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
