package apron

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apron-chat/apron-server-go/wire"
	"github.com/apron-chat/apron-server-go/wire/bot"
	"github.com/apron-chat/apron-server-go/wire/wiretest"
	"github.com/coder/websocket"
	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"

	sshchat "github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/sshd"
)

// testChat is an ssh-chat Host with the gateway in front of it.
type testChat struct {
	t       *testing.T
	auth    *sshchat.Auth
	host    *sshchat.Host
	gateway *Gateway
	sshAddr string
	wsURL   string
}

func newChat(t *testing.T, origins ...string) *testChat {
	t.Helper()
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	auth := sshchat.NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(key)
	listener, err := sshd.ListenSSH("127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	host := sshchat.NewHost(listener, auth)
	go host.Serve()
	gateway := New(Config{Host: host, Auth: auth, History: host.HistoryBuffer(), Version: "test", Origins: origins})
	gateway.limiter = func() rateio.Limiter { return rateio.NewSimpleLimiter(100, time.Second) }
	web := httptest.NewServer(gateway.Handler())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		gateway.Shutdown(ctx)
		web.Close()
		listener.Close()
	})
	return &testChat{
		t: t, auth: auth, host: host, gateway: gateway,
		sshAddr: listener.Addr().String(), wsURL: "ws" + strings.TrimPrefix(web.URL, "http") + "/ws",
	}
}

// within returns a context that ends a wait for a frame or a line.
func within(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// sshUser is an SSH client in bot mode: no echo, no colors, one line per
// message.
type sshUser struct {
	t     *testing.T
	in    interface{ Write([]byte) (int, error) }
	lines chan string
}

func (c *testChat) ssh(name string, key ssh.Signer) *sshUser {
	c.t.Helper()
	config := sshd.NewClientConfig(name)
	if key != nil {
		config = sshd.NewClientConfigWithKey(name, key)
	}
	conn, err := ssh.Dial("tcp", c.sshAddr, config)
	if err != nil {
		c.t.Fatal(err)
	}
	session, err := conn.NewSession()
	if err != nil {
		c.t.Fatal(err)
	}
	if err := session.RequestPty("bot", 80, 40, ssh.TerminalModes{}); err != nil {
		c.t.Fatal(err)
	}
	in, _ := session.StdinPipe()
	out, _ := session.StdoutPipe()
	if err := session.Shell(); err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { conn.Close() })
	u := &sshUser{t: c.t, in: in, lines: make(chan string, 100)}
	go func() {
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			u.lines <- strings.TrimSpace(scanner.Text())
		}
		close(u.lines)
	}()
	return u
}

func (u *sshUser) say(line string) { fmt.Fprintf(u.in, "%s\r\n", line) }

// expect waits for a line that contains want.
func (u *sshUser) expect(want string) string {
	u.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-u.lines:
			if !ok {
				u.t.Fatalf("ssh connection closed waiting for %q", want)
			}
			if strings.Contains(line, want) {
				return line
			}
		case <-timeout:
			u.t.Fatalf("no ssh line with %q", want)
		}
	}
}

// apronUser is an Apron client.
type apronUser struct {
	t      *testing.T
	c      *wiretest.Client
	you    wire.User
	nextID atomic.Int64
}

// dial connects an Apron client, which has not signed in.
func (c *testChat) dial() *apronUser {
	c.t.Helper()
	client, err := wiretest.Dial(within(c.t), c.wsURL)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { client.Close() })
	return &apronUser{t: c.t, c: client}
}

// apron connects an Apron client and signs it in as a guest.
func (c *testChat) apron(name string) *apronUser {
	c.t.Helper()
	u := c.dial()
	if err := u.auth(name); err != nil {
		c.t.Fatalf("auth as %s: %v", name, err)
	}
	return u
}

func (u *apronUser) auth(name string) error {
	result, err := u.c.Auth(within(u.t), map[string]any{"scheme": "guest", "name": name})
	if err != nil {
		return err
	}
	u.you = result.You.User
	return nil
}

// call sends a request and returns its result, or its error reply.
func (u *apronUser) call(method string, params any) (map[string]any, *wire.Error) {
	u.t.Helper()
	raw, err := u.c.Call(within(u.t), method, params)
	var e *wire.Error
	if errors.As(err, &e) {
		return nil, e
	} else if err != nil {
		u.t.Fatalf("%s: %v", method, err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		u.t.Fatalf("%s: %s: %v", method, raw, err)
	}
	return result, nil
}

// ok sends a request and returns its result, failing on an error reply.
func (u *apronUser) ok(method string, params any) map[string]any {
	u.t.Helper()
	result, e := u.call(method, params)
	if e != nil {
		u.t.Fatalf("%s: %v", method, e)
	}
	return result
}

// send sends a request without waiting for its reply, and returns its id.
func (u *apronUser) send(method string, params any) string {
	u.t.Helper()
	id := fmt.Sprintf("t%d", u.nextID.Add(1))
	frame, _ := json.Marshal(map[string]any{"method": method, "id": id, "params": params})
	if err := u.c.Send(within(u.t), frame); err != nil {
		u.t.Fatal(err)
	}
	return id
}

// expect waits for the first frame that match accepts, as a JSON object,
// and fails when the connection closes first. The frames before it stay
// for later expects.
func (u *apronUser) expect(match func(map[string]any) bool) map[string]any {
	u.t.Helper()
	f, err := u.c.Expect(within(u.t), func(f wiretest.Frame) bool { return match(object(f)) })
	if err != nil {
		u.t.Fatalf("no matching apron frame: %v", err)
	}
	return object(f)
}

func object(f wiretest.Frame) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(f.Raw, &m)
	return m
}

// text matches a message notification whose text contains want.
func text(want string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		if f["method"] != "message" {
			return false
		}
		body, _ := params(f)["body"].(map[string]any)
		s, _ := body["text"].(string)
		return strings.Contains(s, want)
	}
}

// userUpdate matches a user notification whose role ("you" or "new") is
// of userID and has field.
func userUpdate(role, userID, field string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		u, _ := params(f)[role].(map[string]any)
		_, has := u[field]
		return f["method"] == "user" && u["user_id"] == userID && has
	}
}

// membership waits for a room_update with a membership of the user named
// name, and returns the user.
func membership(u *apronUser, name string, joined bool) map[string]any {
	u.t.Helper()
	var user map[string]any
	u.expect(func(f map[string]any) bool {
		if f["method"] != "room_update" {
			return false
		}
		for _, m := range params(f)["memberships"].([]any) {
			for _, e := range m.(map[string]any)["members"].([]any) {
				entry := e.(map[string]any)
				who := entry["user"].(map[string]any)
				if who["name"] == name && entry["joined"] == joined {
					user = who
					return true
				}
			}
		}
		return false
	})
	return user
}

func params(f map[string]any) map[string]any { p, _ := f["params"].(map[string]any); return p }
func from(f map[string]any) map[string]any   { p, _ := params(f)["from"].(map[string]any); return p }

func TestChatBothWays(t *testing.T) {
	c := newChat(t)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")

	ada := c.apron("ada")
	if ada.you.Name != "ada" || ada.you.Status != "online" {
		t.Fatalf("auth: %+v", ada.you)
	}
	// The others in the room, with their statuses, follow the result
	// (§4.5.4).
	ada.expect(func(f map[string]any) bool {
		u, _ := params(f)["new"].(map[string]any)
		return f["method"] == "user" && u["name"] == "alice" && u["status"] == "online"
	})
	alice.expect("ada joined")
	// A join is a membership, not a message.
	membership(ada, "ada", true)

	// Apron to SSH.
	result := ada.ok("message", map[string]any{"body": map[string]any{"text": "hello from apron"}})
	if id, _ := result["message_id"].(string); id == "" {
		t.Fatalf("message: %v", result)
	}
	alice.expect("ada: hello from apron")

	// SSH to Apron, with the sender as a stable user.
	alice.say("hello from ssh")
	got := ada.expect(text("hello from ssh"))
	if from(got)["name"] != "alice" || params(got)["message_id"] == nil || params(got)["room_id"] != DefaultRoom {
		t.Fatalf("snapshot: %v", got)
	}
	aliceID := from(got)["user_id"]

	// A rename keeps the user_id.
	alice.say("/nick alicia")
	ada.expect(text("alice is now known as alicia"))
	// (A third line: ssh-chat lets in three in three seconds.)
	alice.say("/me waves")
	got = ada.expect(text("waves"))
	if from(got)["user_id"] != aliceID || from(got)["name"] != "alicia" {
		t.Fatalf("after rename: %v", from(got))
	}
}

func TestCommandsAndPrivateMessages(t *testing.T) {
	c := newChat(t)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")
	ada := c.apron("ada")

	// A command reply is a ~private notice, before the result.
	id := ada.send("command", map[string]any{"body": map[string]any{"text": "/names"}})
	notice := ada.expect(func(f map[string]any) bool {
		return (text("alice")(f) && from(f)["user_id"] == "~private") || f["id"] == id
	})
	if f := from(notice); f == nil || f["user_id"] != "~private" {
		t.Fatalf("expected the notice before the result, got %v", notice)
	}

	// An Apron client's private message reaches the SSH user.
	ada.ok("command", map[string]any{"body": map[string]any{"text": "/msg alice psst"}})
	alice.expect("[PM from ada] psst")

	// And an SSH user's reaches the Apron client, marked private.
	alice.say("/msg ada hi back")
	pm := ada.expect(text("hi back"))
	ext, _ := params(pm)["ext"].(map[string]any)["ssh-chat"].(map[string]any)
	if from(pm)["name"] != "alice" || ext["private"] != true || params(pm)["message_id"] != nil {
		t.Fatalf("private message: %v", pm)
	}

	// Too much input is refused, as over SSH.
	long := strings.Repeat("x", maxInputLength+1)
	if _, e := ada.call("message", map[string]any{"body": map[string]any{"text": long}}); e == nil || e.Code != wire.CodeTooLarge {
		t.Fatalf("long line: %v", e)
	}

	// Unknown commands fail as ssh-chat says.
	ada.ok("command", map[string]any{"body": map[string]any{"text": "/nope"}})
	ada.expect(text("Err:"))

	// me renames through ssh-chat, which normalizes the name.
	you, _ := ada.ok("me", map[string]any{"name": "Ada L"})["you"].(map[string]any)
	if you["name"] != "AdaL" || you["user_id"] != ada.you.UserID || you["status"] != "online" {
		t.Fatalf("me: %v", you)
	}
	alice.expect("ada is now known as AdaL")

	// /nick sent as a command updates the connection's identity too.
	ada.send("command", map[string]any{"body": map[string]any{"text": "/nick ada2"}})
	ada.expect(func(f map[string]any) bool {
		you, _ := params(f)["you"].(map[string]any)
		return f["method"] == "user" && you["name"] == "ada2"
	})

	// Mentions are written as names for SSH users.
	alice.say("ping")
	aliceID, _ := from(ada.expect(text("ping")))["user_id"].(string)
	ada.ok("message", map[string]any{"body": map[string]any{"text": "@" + aliceID + " pong", "mentions": []string{aliceID}}})
	alice.expect("ada2: alice pong")
}

func TestOpsAndPolicy(t *testing.T) {
	c := newChat(t)
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Op(key.PublicKey(), 0)
	op := c.ssh("op", key)
	op.expect("op joined")

	bob := c.ssh("bob", nil)
	bob.expect("bob joined")

	// Muted: the room shows a muted member's messages only to them.
	ada := c.apron("ada")
	bob.expect("ada joined")
	op.say("/mute ada")
	op.expect("Muted: ada")
	ada.ok("message", map[string]any{"body": map[string]any{"text": "can anyone hear me"}})
	ada.expect(text("can anyone hear me"))
	op.say("marker")
	if line := bob.expect("op: "); !strings.Contains(line, "marker") {
		t.Fatalf("bob saw a muted member's message: %q", line)
	}

	// Kicked: the connection closes with an error.
	op.say("/kick ada")
	op.expect("ada was kicked by op")
	f, err := ada.c.Expect(within(t), func(f wiretest.Frame) bool { return f.Error != nil && f.ID == "" && f.Method == "" })
	if err != nil || f.Error.Code != wire.CodeDenied {
		t.Fatalf("no error frame before the kick closed the connection: %v %v", f.Error, err)
	}
	if _, err := ada.c.Expect(within(t), func(wiretest.Frame) bool { return false }); !errors.Is(err, wire.ErrClosed) {
		t.Fatalf("the connection stayed open after the kick: %v", err)
	}

	// Banned by address: a new Apron client from it is refused.
	c.auth.BanAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, time.Minute)
	if err := c.dial().auth("ada"); wire.CodeOf(err) != wire.CodeDenied {
		t.Fatalf("banned client signed in: %v", err)
	}
}

func TestAllowlistKeepsApronOut(t *testing.T) {
	c := newChat(t)
	c.auth.SetAllowlistMode(true)
	if err := c.dial().auth("ada"); wire.CodeOf(err) != wire.CodeDenied {
		t.Fatalf("allowlist: %v", err)
	}
}

func TestRoomsHistoryAndStatus(t *testing.T) {
	c := newChat(t)
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Op(key.PublicKey(), 0)
	op := c.ssh("op", key)
	op.expect("op joined")
	op.say("said before anyone used Apron")

	ada := c.apron("ada")
	if !strings.Contains(fmt.Sprint(ada.ok("room_list", map[string]any{})), DefaultRoom) {
		t.Fatal("room_list without the room")
	}

	// The room lists everyone, SSH users too, with their roles and status.
	list := ada.ok("room_list", map[string]any{"filter": "joined", "members": true})
	joined := list["joined"].([]any)
	room := joined[0].(map[string]any)
	if len(joined) != 1 || room["room_id"] != DefaultRoom || room["latest_log_id"] == nil || room["history_log_id"] == nil {
		t.Fatalf("room_list: %v", list)
	}
	users := map[string]map[string]any{}
	for _, u := range list["users"].([]any) {
		users[u.(map[string]any)["name"].(string)] = u.(map[string]any)
	}
	if users["op"]["status"] != "online" || fmt.Sprint(users["op"]["roles"]) != "[op]" || users["ada"] == nil {
		t.Fatalf("users: %v", users)
	}
	opID := users["op"]["user_id"].(string)

	// History has what was said before ada came, with the same message_ids
	// as live delivery.
	op.say("said while ada is here")
	live := ada.expect(text("said while ada is here"))
	page := ada.ok("history", map[string]any{"room_id": DefaultRoom})
	var texts []string
	for _, m := range page["messages"].([]any) {
		msg := m.(map[string]any)
		texts = append(texts, msg["body"].(map[string]any)["text"].(string))
		if strings.Contains(fmt.Sprint(msg["body"]), "said while ada is here") && msg["message_id"] != params(live)["message_id"] {
			t.Fatalf("history's message_id %v, live %v", msg["message_id"], params(live)["message_id"])
		}
	}
	if !strings.Contains(strings.Join(texts, "|"), "said before anyone used Apron") || len(page["memberships"].([]any)) < 2 {
		t.Fatalf("history: %v", page)
	}

	// Away is a status, both ways; a change carries what changed.
	op.say("/away lunch")
	ada.expect(func(f map[string]any) bool {
		u, _ := params(f)["new"].(map[string]any)
		return userUpdate("new", opID, "status")(f) && u["status"] == "away" && u["name"] == nil
	})
	you := ada.ok("me", map[string]any{"status": "away"})["you"].(map[string]any)
	if you["status"] != "away" || you["name"] != "ada" {
		t.Fatalf("me status: %v", you)
	}
	op.expect("ada has gone away")
	// A status the server does not offer is not an error: the server
	// keeps one it does (§4.5.1), here back from away.
	if you := ada.ok("me", map[string]any{"status": "dnd"})["you"].(map[string]any); you["status"] != "online" {
		t.Fatalf("an unoffered status was kept: %v", you)
	}
	// Mutes change nothing here, but are told back (§4.5.3).
	ada.ok("status", map[string]any{"mute": true})
	if mute := ada.expect(func(f map[string]any) bool { return f["method"] == "status" }); params(mute)["mute"] != true {
		t.Fatalf("status: %v", mute)
	}

	// room_leave of another user is /kick, for ops only.
	bob := c.ssh("bob", nil)
	bob.expect("bob joined")
	bobUser := membership(ada, "bob", true)
	if _, e := ada.call("room_leave", map[string]any{"room_id": DefaultRoom, "user_id": bobUser["user_id"]}); e == nil || e.Code != wire.CodeDenied {
		t.Fatalf("a non-op removed someone: %v", e)
	}
	time.Sleep(3 * time.Second) // ssh-chat lets an SSH user say three lines in three seconds.
	op.say("/op ada")
	ada.expect(text("Made op"))
	ada.ok("room_leave", map[string]any{"room_id": DefaultRoom, "user_id": bobUser["user_id"]})
	op.expect("bob was kicked by ada")
	membership(ada, "bob", false)

	// One room: creating others is refused; leaving it is disconnecting.
	for method, p := range map[string]map[string]any{
		"room_set":   {"title": "another"},
		"room_leave": {"room_id": DefaultRoom},
	} {
		if _, e := ada.call(method, p); e == nil || e.Code != wire.CodeDenied {
			t.Errorf("%s: %v", method, e)
		}
	}
	if _, e := ada.call("room_set", map[string]any{"private": true}); e == nil || e.Code != wire.CodeUnsupported {
		t.Errorf("private room_set: %v", e)
	}
	if _, e := ada.call("room_join", map[string]any{"room_id": "elsewhere"}); e == nil || e.Code != wire.CodeInvalidParams {
		t.Errorf("room_join of another room: %v", e)
	}
	ada.ok("room_join", map[string]any{"room_id": DefaultRoom})
}

func TestJoinsWhileNobodyIsConnectedOverApron(t *testing.T) {
	c := newChat(t)
	bob := c.ssh("bob", nil)
	bob.expect("bob joined")
	time.Sleep(2 * watchInterval)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")
	time.Sleep(2 * watchInterval)
	ada := c.apron("ada")
	page := ada.ok("history", map[string]any{})
	var joined []string
	for _, m := range page["memberships"].([]any) {
		for _, e := range m.(map[string]any)["members"].([]any) {
			joined = append(joined, e.(map[string]any)["user"].(map[string]any)["name"].(string))
		}
	}
	if fmt.Sprint(joined) != "[bob alice ada]" {
		t.Fatalf("memberships: %v (history %v)", joined, page)
	}
	messages, _ := page["messages"].([]any)
	for _, m := range messages {
		if strings.Contains(fmt.Sprint(m), "joined") {
			t.Fatalf("a join as a message: %v", m)
		}
	}
}

// logIDs returns the log_ids of a history result's records, in order.
func logIDs(page map[string]any) []string {
	var ids []string
	for _, kind := range []string{"rooms", "messages", "memberships", "reactions"} {
		records, _ := page[kind].([]any)
		for _, r := range records {
			ids = append(ids, r.(map[string]any)["log_id"].(string))
		}
	}
	slices.SortFunc(ids, compareLogIDs)
	return ids
}

// compareLogIDs compares two log_ids as numbers.
func compareLogIDs(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func TestHistoryFollowsTheBuffer(t *testing.T) {
	c := newChat(t)
	ada := c.apron("ada")
	first := ada.ok("history", map[string]any{})
	lower := first["history_log_id"].(string)
	for i := range 25 {
		ada.ok("message", map[string]any{"body": map[string]any{"text": fmt.Sprintf("line %d", i)}})
	}
	page := ada.ok("history", map[string]any{"limit": 5})
	if !page["more"].(bool) || len(logIDs(page)) != 5 {
		t.Fatalf("a limited page: %v", page)
	}
	full := ada.ok("history", map[string]any{})
	messages := full["messages"].([]any)
	newLower := full["history_log_id"].(string)
	if compareLogIDs(newLower, lower) <= 0 || full["first_log_id"] != newLower || len(messages) != 20 {
		t.Fatalf("the log follows ssh-chat's 20-message buffer: lower %s -> %s, first %v, %d messages", lower, newLower, full["first_log_id"], len(messages))
	}
	last := messages[len(messages)-1].(map[string]any)
	if last["body"].(map[string]any)["text"] != "line 24" || compareLogIDs(full["latest_log_id"].(string), last["log_id"].(string)) < 0 {
		t.Fatalf("latest: %v %v", full["latest_log_id"], last)
	}

	// Paging forward from the start, a few records at a time, gives the
	// whole log once, in order, as one page does.
	var paged []string
	after := newLower
	for {
		next := ada.ok("history", map[string]any{"after": after, "limit": 3})
		ids := logIDs(next)
		if len(ids) == 0 {
			break
		}
		if next["first_log_id"] != ids[0] || next["last_log_id"] != ids[len(ids)-1] || next["history_log_id"] != newLower {
			t.Fatalf("forward: %v", next)
		}
		paged = append(paged, ids...)
		if !next["more"].(bool) {
			break
		}
		var n int64
		fmt.Sscan(ids[len(ids)-1], &n)
		after = fmt.Sprint(n + 1)
	}
	if want := logIDs(full); !slices.Equal(paged, want) {
		t.Fatalf("paged %v, one page %v", paged, want)
	}
	for i := 1; i < len(paged); i++ {
		if compareLogIDs(paged[i-1], paged[i]) >= 0 {
			t.Fatalf("log_ids out of order or repeated: %v", paged)
		}
	}
}

// The history buffer drops joins as it fills; before the log loses them,
// it gets a membership record of everyone in the room (§4.3.3).
func TestHistoryKeepsMembersAsTheBufferDrops(t *testing.T) {
	c := newChat(t)
	bob := c.ssh("bob", nil)
	bob.expect("bob joined")
	ada := c.apron("ada")
	membership(ada, "ada", true)
	for i := range 25 {
		ada.ok("message", map[string]any{"body": map[string]any{"text": fmt.Sprintf("line %d", i)}})
	}
	full := ada.ok("history", map[string]any{})
	lower := full["history_log_id"].(string)
	memberships, _ := full["memberships"].([]any)
	if len(memberships) != 1 {
		t.Fatalf("memberships after the joins left the buffer: %v", memberships)
	}
	record := memberships[0].(map[string]any)
	var names []string
	for _, e := range record["members"].([]any) {
		entry := e.(map[string]any)
		if entry["joined"] != true {
			t.Fatalf("a member not joined: %v", record)
		}
		names = append(names, entry["user"].(map[string]any)["name"].(string))
	}
	slices.Sort(names)
	if fmt.Sprint(names) != "[ada bob]" || compareLogIDs(record["log_id"].(string), lower) < 0 {
		t.Fatalf("membership record %v, history_log_id %s", record, lower)
	}

	// The record stays in the log as long as it is newer than the
	// buffer's oldest, and the bound never moves back; once the buffer
	// passes it, another takes its place.
	for i := range 25 {
		ada.ok("message", map[string]any{"body": map[string]any{"text": fmt.Sprintf("more %d", i)}})
		page := ada.ok("history", map[string]any{})
		if compareLogIDs(page["history_log_id"].(string), lower) < 0 {
			t.Fatalf("history_log_id moved back: %s -> %v", lower, page["history_log_id"])
		}
		lower = page["history_log_id"].(string)
		if m, _ := page["memberships"].([]any); len(m) == 0 {
			t.Fatalf("no membership record after %d more lines: %v", i+1, page)
		}
	}
}

func TestRenameKeepsMessageIDs(t *testing.T) {
	c := newChat(t)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")
	ada := c.apron("ada")
	alice.say("before the rename")
	live := ada.expect(text("before the rename"))
	alice.say("/nick alicia")
	ada.expect(text("alice is now known as alicia"))
	page := ada.ok("history", map[string]any{})
	found := 0
	for _, m := range page["messages"].([]any) {
		msg := m.(map[string]any)
		if strings.Contains(fmt.Sprint(msg["body"]), "before the rename") {
			found++
			if msg["message_id"] != params(live)["message_id"] {
				t.Fatalf("the message's id changed with its sender's name: %v, live %v", msg["message_id"], params(live)["message_id"])
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d copies of the message in history: %v", found, page)
	}
}

// Taking op away is a change too: clients merge user objects (§3.3.1), so
// it reaches them as roles: [].
func TestOpRemovalReachesClients(t *testing.T) {
	c := newChat(t)
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Op(key.PublicKey(), 0)
	op := c.ssh("op", key)
	op.expect("op joined")
	ada := c.apron("ada")
	bea := c.apron("bea")

	roles := func(u *apronUser, role string) any {
		t.Helper()
		update := params(u.expect(userUpdate(role, ada.you.UserID, "roles")))[role].(map[string]any)
		return update["roles"]
	}
	op.say("/op ada")
	ada.expect(text("Made op"))
	if you, others := roles(ada, "you"), roles(bea, "new"); fmt.Sprint(you, others) != "[op] [op]" {
		t.Fatalf("op: you %v, others %v", you, others)
	}
	op.say("/op ada remove")
	ada.expect(text("Removed op"))
	for _, got := range []any{roles(ada, "you"), roles(bea, "new")} {
		if r, ok := got.([]any); !ok || len(r) != 0 {
			t.Fatalf("op taken away: roles %v, not []", got)
		}
	}
}

// Every client gets a user's changes in the order they were made, however
// many goroutines see them: the gateway sends them as it sees them, under
// its lock.
func TestUserUpdatesInOrder(t *testing.T) {
	c := newChat(t)
	c.ssh("bob", nil).expect("bob joined")
	ada := c.apron("ada")
	bobID := params(ada.expect(func(f map[string]any) bool {
		u, _ := params(f)["new"].(map[string]any)
		return f["method"] == "user" && u["name"] == "bob"
	}))["new"].(map[string]any)["user_id"].(string)
	bob, _ := c.host.MemberByID("bob")

	// bob goes away and comes back, over and over, while connections'
	// deliveries and the watch sync at once.
	stop := make(chan struct{})
	var syncing sync.WaitGroup
	for range 4 {
		syncing.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				c.gateway.mu.Lock()
				c.gateway.syncLocked()
				c.gateway.mu.Unlock()
			}
		})
	}
	const toggles = 60
	for i := range toggles {
		if i%2 == 0 {
			bob.User.SetAway("brb")
		} else {
			bob.User.SetAway("")
		}
		time.Sleep(200 * time.Microsecond)
	}
	close(stop)
	syncing.Wait()
	c.gateway.mu.Lock()
	c.gateway.syncLocked()
	c.gateway.mu.Unlock()

	// Every frame sent before the reply has arrived with it.
	_, before, err := ada.c.CallFrames(within(t), "history", map[string]any{"limit": 1})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var statuses []string
	for _, f := range before {
		if m := object(f); userUpdate("new", bobID, "status")(m) {
			statuses = append(statuses, params(m)["new"].(map[string]any)["status"].(string))
		}
	}
	// Each change is sent once, so the statuses alternate, and the last is
	// bob's.
	for i, status := range statuses {
		if i > 0 && status == statuses[i-1] {
			t.Fatalf("bob's statuses out of order: %v", statuses)
		}
	}
	if len(statuses) == 0 || statuses[len(statuses)-1] != "online" {
		t.Fatalf("bob's statuses end wrong: %v", statuses)
	}
}

// A connection is signed in as its auth result is queued, so a change made
// while the sign-in finishes reaches it, after the result (§3.2.2).
func TestSignInSeesChangesMadeDuringAuth(t *testing.T) {
	c := newChat(t)
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Op(key.PublicKey(), 0)
	op := c.ssh("op", key)
	op.expect("op joined")
	c.gateway.afterAuth = func(*wire.Conn) {
		member, _ := c.host.MemberByID("op")
		member.SetOp(false)
		c.gateway.mu.Lock()
		c.gateway.syncLocked()
		c.gateway.mu.Unlock()
	}
	ada := c.apron("ada")
	opUser := params(ada.expect(func(f map[string]any) bool {
		u, _ := params(f)["new"].(map[string]any)
		return f["method"] == "user" && u["name"] == "op"
	}))["new"].(map[string]any)
	if fmt.Sprint(opUser["roles"]) != "[op]" {
		t.Fatalf("op as of the sign-in: %v", opUser)
	}
	update := params(ada.expect(userUpdate("new", opUser["user_id"].(string), "roles")))["new"].(map[string]any)
	if roles, ok := update["roles"].([]any); !ok || len(roles) != 0 {
		t.Fatalf("the change made during auth: %v", update)
	}
}

// A guest who cannot join, as when the name and the guest name are both
// taken, gets the error as the auth reply, and nothing of the sign-in: the
// connection stays open for another try.
func TestFailedJoinLeavesTheConnectionOpen(t *testing.T) {
	c := newChat(t)
	c.host.SetMotd("welcome")
	c.ssh("ada", nil).expect("ada joined")
	c.ssh("Guest1", nil).expect("Guest1 joined")

	ada := c.dial()
	if err := ada.auth("ada"); wire.CodeOf(err) != wire.CodeDenied {
		t.Fatalf("auth with both names taken: %v", err)
	}
	// Nothing the failed sign-in sent goes out: not the welcome, not the
	// history; a request before sign-in is refused.
	if _, e := ada.call("history", map[string]any{}); e == nil {
		t.Fatal("history before sign-in")
	}
	if err := ada.c.Sync(within(t)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if pending := ada.c.Pending(); len(pending) != 0 {
		t.Fatalf("frames of a failed sign-in: %s", pending[0].Raw)
	}
	if err := ada.auth("ada_too"); err != nil {
		t.Fatalf("a second auth: %v", err)
	}
	ada.expect(text("welcome"))
	ada.ok("message", map[string]any{"body": map[string]any{"text": "made it"}})
	ada.expect(text("made it"))
}

func TestOrigins(t *testing.T) {
	for _, tc := range []struct {
		origins []string
		origin  string
		ok      bool
	}{
		{nil, "https://web.example", false},
		{[]string{"https://web.example"}, "https://web.example", true},
		{[]string{"https://web.example"}, "https://evil.example", false},
		{[]string{"*"}, "https://evil.example", true},
	} {
		c := newChat(t, tc.origins...)
		ws, _, err := websocket.Dial(within(t), c.wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {tc.origin}}})
		if ws != nil {
			ws.CloseNow()
		}
		if (err == nil) != tc.ok {
			t.Errorf("origins %v, origin %s: %v", tc.origins, tc.origin, err)
		}
	}
}

// commandNames returns the names of the commands ext:command_list lists.
func commandNames(u *apronUser) []string {
	u.t.Helper()
	var names []string
	for _, e := range u.ok("ext:command_list", nil)["commands"].([]any) {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	return names
}

func TestCommandList(t *testing.T) {
	c := newChat(t)
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	c.auth.Op(key.PublicKey(), 0)
	op := c.ssh("op", key)
	op.expect("op joined")
	ada := c.apron("ada")

	// The commands of ssh-chat's /help, with its usage and help, op
	// commands only for ops.
	names := commandNames(ada)
	if names[0] != "help" || !slices.Contains(names, "msg") || !slices.Contains(names, "nick") ||
		slices.Contains(names, "kick") || slices.Contains(names, "quit") || slices.Contains(names, "me") {
		t.Fatalf("command list: %v", names)
	}
	for _, e := range ada.ok("ext:command_list", nil)["commands"].([]any) {
		if entry := e.(map[string]any); entry["name"] == "msg" &&
			(entry["hint"] != "USER MESSAGE" || entry["description"] != "Send MESSAGE to USER." || fmt.Sprint(entry["uses"]) != "[body.mentions]") {
			t.Fatalf("msg: %v", entry)
		}
	}
	op.say("/op ada")
	ada.expect(text("Made op"))
	if names := commandNames(ada); !slices.Contains(names, "kick") {
		t.Fatalf("an op's command list: %v", names)
	}

	// /help is ssh-chat's, and commands it does not list still run.
	ada.ok("command", map[string]any{"body": map[string]any{"text": "/help"}})
	ada.expect(text("Operator commands"))
	ada.ok("command", map[string]any{"body": map[string]any{"text": "/me waves"}})
	op.expect("ada waves")

	// An op command is ssh-chat's to refuse, as over SSH.
	bea := c.apron("bea")
	bea.ok("command", map[string]any{"body": map[string]any{"text": "/kick ada"}})
	bea.expect(text("must be op"))

	// Every command line is checked as a line of input.
	if _, e := ada.call("command", map[string]any{"body": map[string]any{"text": "/msg op " + strings.Repeat("x", maxInputLength)}}); e == nil || e.Code != wire.CodeTooLarge {
		t.Fatalf("long command: %v", e)
	}
	if _, e := ada.call("command", map[string]any{"room_id": "elsewhere", "body": map[string]any{"text": "/names"}}); e == nil || e.Code != wire.CodeInvalidParams {
		t.Fatalf("another room: %v", e)
	}
}

// A bot of wire/bot runs on the gateway as a member of the room, on a
// Local connection: it sees what SSH users say, and they see its replies.
func TestBot(t *testing.T) {
	c := newChat(t)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")
	ada := c.apron("ada")

	heard := make(chan wire.Message, 10)
	echo, err := bot.Start(c.gateway.Server(), bot.Options{
		User: wire.User{UserID: "echo", Name: "echo"},
		OnMessage: func(ctx context.Context, b *bot.Bot, m wire.Message) {
			said, ok := strings.CutPrefix(m.Body.Text, "echo: ")
			if !b.Addressed(m) || !ok {
				return
			}
			heard <- m
			if _, err := b.Reply(ctx, m, "you said "+said); err != nil {
				t.Errorf("reply: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { echo.Close() })
	alice.expect("echo joined")
	if bot := membership(ada, "echo", true); bot["user_id"] != echo.UserID() || echo.User().Name != "echo" {
		t.Fatalf("the bot's user: %v, %+v", bot, echo.User())
	}

	alice.say("echo: hello")
	var m wire.Message
	select {
	case m = <-heard:
	case <-within(t).Done():
		t.Fatal("the bot heard nothing")
	}
	if m.From.Name != "alice" || m.RoomID != DefaultRoom {
		t.Fatalf("the bot heard %+v", m)
	}
	alice.expect("echo: you said hello")
	reply := ada.expect(text("you said hello"))
	replyTo, _ := params(reply)["reply_to"].(map[string]any)
	if from(reply)["user_id"] != echo.UserID() || replyTo["message_id"] != fmt.Sprint(m.MessageID) {
		t.Fatalf("the bot's reply: %v", reply)
	}

	// A Local connection's commands are ssh-chat's, with their replies
	// before the result; ssh-chat knows it as a local Apron client.
	var notices []string
	got := make(chan string, 10)
	probe, err := bot.Dial(c.gateway.Server(), wire.LocalOptions{
		User: wire.User{UserID: "probe", Name: "probe"},
		OnFrame: func(f wire.Frame) {
			var n struct {
				Params wire.Message `json:"params"`
			}
			if json.Unmarshal(f, &n) == nil && n.Params.From.UserID == wire.Private.UserID {
				got <- n.Params.Body.Text
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Command(within(t), wire.CommandParams{Body: &wire.Body{Text: "/whois echo"}}); err != nil {
		t.Fatal(err)
	}
	for len(got) > 0 {
		notices = append(notices, <-got)
	}
	if !strings.Contains(strings.Join(notices, "\n"), "apron local") {
		t.Fatalf("/whois echo: %q", notices)
	}
	probe.Close()

	// A Local connection gets a user_id of the gateway's, and a guest's
	// name when its name is taken.
	twin, err := bot.Dial(c.gateway.Server(), wire.LocalOptions{User: wire.User{UserID: "echo", Name: "echo"}})
	if err != nil {
		t.Fatal(err)
	}
	if u := twin.User(); u.UserID == "echo" || u.UserID == echo.UserID() || !strings.HasPrefix(u.Name, "Guest") {
		t.Fatalf("the second echo is %+v", u)
	}
	twin.Close()

	// Closing the bot leaves the room.
	echo.Close()
	alice.expect("echo left")
}
