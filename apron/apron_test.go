package apron

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shazow/rateio"
	"golang.org/x/crypto/ssh"

	sshchat "github.com/shazow/ssh-chat"
	"github.com/shazow/ssh-chat/sshd"
)

// chat is an ssh-chat Host with the gateway in front of it.
type testChat struct {
	t       *testing.T
	auth    *sshchat.Auth
	host    *sshchat.Host
	sshAddr string
	wsURL   string
}

func newChat(t *testing.T) *testChat {
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
	gateway := New(Config{Host: host, Auth: auth, History: host.HistoryBuffer(), Version: "test"})
	gateway.limiter = func() rateio.Limiter { return rateio.NewSimpleLimiter(100, time.Second) }
	web := httptest.NewServer(gateway.Handler())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		gateway.Shutdown(ctx)
		web.Close()
		listener.Close()
	})
	return &testChat{t: t, auth: auth, host: host, sshAddr: listener.Addr().String(), wsURL: "ws" + strings.TrimPrefix(web.URL, "http") + "/ws"}
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
	ws     *websocket.Conn
	frames chan map[string]any
	nextID atomic.Int64
	you    map[string]any
	// seen holds the frames expect passed over, for later expects.
	seen []map[string]any
}

func (c *testChat) apron(name string) (*apronUser, map[string]any) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, c.wsURL, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { ws.CloseNow() })
	u := &apronUser{t: c.t, ws: ws, frames: make(chan map[string]any, 100)}
	go func() {
		defer close(u.frames)
		for {
			_, data, err := ws.Read(context.Background())
			if err != nil {
				return
			}
			var frame map[string]any
			if json.Unmarshal(data, &frame) == nil {
				u.frames <- frame
			}
		}
	}()
	u.expect(func(f map[string]any) bool { return f["method"] == "server" })
	reply := u.call("auth", map[string]any{"scheme": "guest", "name": name})
	if result, ok := reply["result"].(map[string]any); ok {
		u.you, _ = result["you"].(map[string]any)
	}
	return u, reply
}

func (u *apronUser) send(method string, params any) string {
	id := fmt.Sprintf("r%d", u.nextID.Add(1))
	data, _ := json.Marshal(map[string]any{"method": method, "id": id, "params": params})
	if err := u.ws.Write(context.Background(), websocket.MessageText, data); err != nil {
		u.t.Fatal(err)
	}
	return id
}

// call sends a request and returns its reply.
func (u *apronUser) call(method string, params any) map[string]any {
	u.t.Helper()
	id := u.send(method, params)
	return u.expect(func(f map[string]any) bool { return f["id"] == id })
}

// expect waits for a frame that match accepts, and fails when the
// connection closes first.
func (u *apronUser) expect(match func(map[string]any) bool) map[string]any {
	u.t.Helper()
	for i, f := range u.seen {
		if match(f) {
			u.seen = append(u.seen[:i:i], u.seen[i+1:]...)
			return f
		}
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-u.frames:
			if !ok {
				u.t.Fatal("apron connection closed")
			}
			if match(f) {
				return f
			}
			u.seen = append(u.seen, f)
		case <-timeout:
			u.t.Fatal("no matching apron frame")
		}
	}
}

// message matches a message notification whose text contains text.
func text(want string) func(map[string]any) bool {
	return func(f map[string]any) bool {
		if f["method"] != "message" {
			return false
		}
		body, _ := f["params"].(map[string]any)["body"].(map[string]any)
		s, _ := body["text"].(string)
		return strings.Contains(s, want)
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

	ada, reply := c.apron("ada")
	if ada.you["name"] != "ada" {
		t.Fatalf("auth: %v", reply)
	}
	alice.expect("ada joined")
	// A join is a membership, not a message.
	membership(ada, "ada", true)

	// Apron to SSH.
	result := ada.call("message", map[string]any{"body": map[string]any{"text": "hello from apron"}})
	id, _ := result["result"].(map[string]any)["message_id"].(string)
	if id == "" {
		t.Fatalf("message: %v", result)
	}
	alice.expect("ada: hello from apron")

	// SSH to Apron, with the sender as a stable user.
	alice.say("hello from ssh")
	got := ada.expect(text("hello from ssh"))
	if from(got)["name"] != "alice" || params(got)["message_id"] == nil || params(got)["room_id"] != Room {
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
	ada, _ := c.apron("ada")

	// A command reply is a ~private notice, before the result.
	id := ada.send("command", map[string]any{"body": map[string]any{"text": "/names"}})
	notice := ada.expect(func(f map[string]any) bool {
		return (text("alice")(f) && from(f)["user_id"] == "~private") || f["id"] == id
	})
	if f := from(notice); f == nil || f["user_id"] != "~private" {
		t.Fatalf("expected the notice before the result, got %v", notice)
	}

	// An Apron client's private message reaches the SSH user.
	ada.call("command", map[string]any{"body": map[string]any{"text": "/msg alice psst"}})
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
	if e, _ := ada.call("message", map[string]any{"body": map[string]any{"text": long}})["error"].(map[string]any); e == nil || e["code"].(float64) != -32003 {
		t.Fatalf("long line: %v", e)
	}

	// Unknown commands fail as ssh-chat says.
	ada.call("command", map[string]any{"body": map[string]any{"text": "/nope"}})
	ada.expect(text("Err:"))

	// me renames through ssh-chat, which normalizes the name.
	reply := ada.call("me", map[string]any{"name": "Ada L"})
	you, _ := reply["result"].(map[string]any)["you"].(map[string]any)
	if you["name"] != "AdaL" || you["user_id"] != ada.you["user_id"] {
		t.Fatalf("me: %v", reply)
	}
	alice.expect("ada is now known as AdaL")

	// /nick sent as a command updates the connection's identity too.
	ada.send("command", map[string]any{"body": map[string]any{"text": "/nick ada2"}})
	ada.expect(func(f map[string]any) bool {
		you, _ := params(f)["you"].(map[string]any)
		return f["method"] == "user" && you["name"] == "ada2"
	})

	// Mentions are written as names for SSH users.
	aliceID := ""
	alice.say("ping")
	aliceID, _ = from(ada.expect(text("ping")))["user_id"].(string)
	ada.call("message", map[string]any{"body": map[string]any{"text": "@" + aliceID + " pong", "mentions": []string{aliceID}}})
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
	ada, _ := c.apron("ada")
	bob.expect("ada joined")
	op.say("/mute ada")
	op.expect("Muted: ada")
	ada.call("message", map[string]any{"body": map[string]any{"text": "can anyone hear me"}})
	ada.expect(text("can anyone hear me"))
	op.say("marker")
	if line := bob.expect("op: "); !strings.Contains(line, "marker") {
		t.Fatalf("bob saw a muted member's message: %q", line)
	}

	// Kicked: the connection closes with an error.
	op.say("/kick ada")
	op.expect("ada was kicked by op")
	closed := false
	for f := range ada.frames {
		if e, ok := f["error"].(map[string]any); ok && f["id"] == nil {
			closed = e["code"].(float64) == -32001
		}
	}
	if !closed {
		t.Fatal("no error frame before the kick closed the connection")
	}

	// Banned by address: a new Apron client from it is refused.
	c.auth.BanAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, time.Minute)
	if _, reply := c.apron("ada"); reply["error"] == nil {
		t.Fatalf("banned client signed in: %v", reply)
	}
}

func TestAllowlistKeepsApronOut(t *testing.T) {
	c := newChat(t)
	c.auth.SetAllowlistMode(true)
	_, reply := c.apron("ada")
	e, _ := reply["error"].(map[string]any)
	if e == nil || e["code"].(float64) != -32001 {
		t.Fatalf("allowlist: %v", reply)
	}
}

func result(reply map[string]any) map[string]any {
	r, _ := reply["result"].(map[string]any)
	return r
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

	ada, _ := c.apron("ada")
	if !strings.Contains(fmt.Sprint(ada.call("room_list", map[string]any{})), Room) {
		t.Fatal("room_list without the room")
	}

	// The room lists everyone, SSH users too, with their roles and status.
	list := result(ada.call("room_list", map[string]any{"filter": "joined", "members": true}))
	joined := list["joined"].([]any)
	room := joined[0].(map[string]any)
	if len(joined) != 1 || room["room_id"] != Room || room["latest_log_id"] == nil || room["history_log_id"] == nil {
		t.Fatalf("room_list: %v", list)
	}
	users := map[string]map[string]any{}
	for _, u := range list["users"].([]any) {
		users[u.(map[string]any)["name"].(string)] = u.(map[string]any)
	}
	if users["op"]["status"] != "online" || fmt.Sprint(users["op"]["roles"]) != "[op]" || users["ada"] == nil {
		t.Fatalf("users: %v", users)
	}

	// History has what was said before ada came, with the same message_ids
	// as live delivery.
	op.say("said while ada is here")
	live := ada.expect(text("said while ada is here"))
	page := result(ada.call("history", map[string]any{"room_id": Room}))
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

	// Away is a status, both ways.
	op.say("/away lunch")
	ada.expect(func(f map[string]any) bool {
		u, _ := params(f)["new"].(map[string]any)
		return f["method"] == "user" && u["name"] == "op" && u["status"] == "away"
	})
	you := result(ada.call("me", map[string]any{"status": "away"}))["you"].(map[string]any)
	if you["status"] != "away" {
		t.Fatalf("me status: %v", you)
	}
	op.expect("ada has gone away")
	// A status the server does not offer is not an error: the server
	// keeps one it does (§4.5.1), here back from away.
	if you := result(ada.call("me", map[string]any{"status": "dnd"}))["you"].(map[string]any); you["status"] == "dnd" {
		t.Fatalf("an unoffered status was kept: %v", you)
	}

	// room_leave of another user is /kick, for ops only.
	bob := c.ssh("bob", nil)
	bob.expect("bob joined")
	bobUser := membership(ada, "bob", true)
	if e, _ := ada.call("room_leave", map[string]any{"room_id": Room, "user_id": bobUser["user_id"]})["error"].(map[string]any); e == nil || e["code"].(float64) != -32001 {
		t.Fatalf("a non-op removed someone: %v", e)
	}
	time.Sleep(3 * time.Second) // ssh-chat lets an SSH user say three lines in three seconds.
	op.say("/op ada")
	ada.expect(text("Made op"))
	if reply := ada.call("room_leave", map[string]any{"room_id": Room, "user_id": bobUser["user_id"]}); reply["error"] != nil {
		t.Fatalf("op room_leave: %v", reply)
	}
	op.expect("bob was kicked by ada")
	membership(ada, "bob", false)

	// One room: creating others is refused; leaving it is disconnecting.
	for method, p := range map[string]map[string]any{
		"room_set":   {"title": "another"},
		"room_leave": {"room_id": Room},
	} {
		if e, _ := ada.call(method, p)["error"].(map[string]any); e == nil || e["code"].(float64) != -32001 {
			t.Errorf("%s: %v", method, e)
		}
	}
	if e, _ := ada.call("room_set", map[string]any{"private": true})["error"].(map[string]any); e == nil || e["code"].(float64) != -32601 {
		t.Errorf("private room_set: %v", e)
	}
}

func TestJoinsWhileNobodyIsConnectedOverApron(t *testing.T) {
	c := newChat(t)
	bob := c.ssh("bob", nil)
	bob.expect("bob joined")
	time.Sleep(2 * watchInterval)
	alice := c.ssh("alice", nil)
	alice.expect("alice joined")
	time.Sleep(2 * watchInterval)
	ada, _ := c.apron("ada")
	page := result(ada.call("history", map[string]any{}))
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

func TestHistoryFollowsTheBuffer(t *testing.T) {
	c := newChat(t)
	ada, _ := c.apron("ada")
	first := result(ada.call("history", map[string]any{}))
	lower := first["history_log_id"].(string)
	for i := range 25 {
		ada.call("message", map[string]any{"body": map[string]any{"text": fmt.Sprintf("line %d", i)}})
	}
	page := result(ada.call("history", map[string]any{"limit": 5}))
	if !page["more"].(bool) || len(page["messages"].([]any)) != 5 {
		t.Fatalf("a limited page: %v", page)
	}
	full := result(ada.call("history", map[string]any{}))
	messages := full["messages"].([]any)
	newLower := full["history_log_id"].(string)
	if newLower <= lower || full["first_log_id"] != newLower || len(messages) != 20 {
		t.Fatalf("the log follows ssh-chat's 20-message buffer: lower %s -> %s, first %v, %d messages", lower, newLower, full["first_log_id"], len(messages))
	}
	last := messages[len(messages)-1].(map[string]any)
	if last["body"].(map[string]any)["text"] != "line 24" || full["latest_log_id"] != last["log_id"] {
		t.Fatalf("latest: %v %v", full["latest_log_id"], last)
	}
	// Paging forward from the start.
	next := result(ada.call("history", map[string]any{"after": newLower, "limit": 10}))
	if next["first_log_id"] != newLower || !next["more"].(bool) {
		t.Fatalf("forward: %v", next)
	}
}
