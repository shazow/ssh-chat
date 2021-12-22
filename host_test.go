package sshchat

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	mathRand "math/rand"
	"strings"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/errgroup"
)

func stripPrompt(s string) string {
	// FIXME: Is there a better way to do this?
	if endPos := strings.Index(s, "\x1b[K "); endPos > 0 {
		return s[endPos+3:]
	}
	if endPos := strings.Index(s, "\x1b[2K "); endPos > 0 {
		return s[endPos+4:]
	}
	if endPos := strings.Index(s, "\x1b[K-> "); endPos > 0 {
		return s[endPos+6:]
	}
	if endPos := strings.Index(s, "] "); endPos > 0 {
		return s[endPos+2:]
	}
	return s
}

func TestStripPrompt(t *testing.T) {
	tests := []struct {
		Input string
		Want  string
	}{
		{
			Input: "\x1b[A\x1b[2K[quux] hello",
			Want:  "hello",
		},
		{
			Input: "[foo] \x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[D\x1b[K * Guest1 joined. (Connected: 2)\r",
			Want:  " * Guest1 joined. (Connected: 2)\r",
		},
		{
			Input: "[foo] \x1b[6D\x1b[K-> From your friendly system.\r",
			Want:  "From your friendly system.\r",
		},
	}

	for i, tc := range tests {
		if got, want := stripPrompt(tc.Input), tc.Want; got != want {
			t.Errorf("case #%d:\n got: %q\nwant: %q", i, got, want)
		}
	}
}

func TestHostGetPrompt(t *testing.T) {
	var expected, actual string

	// Make the random colors consistent across tests
	mathRand.Seed(1)

	u := message.NewUser(&Identity{id: "foo"})

	actual = GetPrompt(u)
	expected = "[foo] "
	if actual != expected {
		t.Errorf("Invalid host prompt:\n Got: %q;\nWant: %q", actual, expected)
	}

	u.SetConfig(message.UserConfig{
		Theme: &message.Themes[0],
	})
	actual = GetPrompt(u)
	expected = "[\033[38;05;88mfoo\033[0m] "
	if actual != expected {
		t.Errorf("Invalid host prompt:\n Got: %q;\nWant: %q", actual, expected)
	}
}

func getHost(t *testing.T, auth *Auth) (*sshd.SSHListener, *Host) {
	key, err := sshd.NewRandomSigner(1024)
	if err != nil {
		t.Fatal(err)
	}
	var config *ssh.ServerConfig
	if auth == nil {
		config = sshd.MakeNoAuth()
	} else {
		config = sshd.MakeAuth(auth)
	}
	config.AddHostKey(key)

	s, err := sshd.ListenSSH("localhost:0", config)
	if err != nil {
		t.Fatal(err)
	}
	return s, NewHost(s, auth)
}

func TestHostNameCollision(t *testing.T) {
	s, host := getHost(t, nil)
	defer s.Close()

	newUsers := make(chan *message.User)
	host.OnUserJoined = func(u *message.User) {
		newUsers <- u
	}
	go host.Serve()

	g := errgroup.Group{}

	// First client
	g.Go(func() error {
		return sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)

			// Consume the initial buffer
			scanner.Scan()
			actual := stripPrompt(scanner.Text())
			expected := " * foo joined. (Connected: 1)\r"
			if actual != expected {
				t.Errorf("Got %q; expected %q", actual, expected)
			}

			// wait for the second client
			<-newUsers

			scanner.Scan()
			actual = scanner.Text()
			// This check has to happen second because prompt doesn't always
			// get set before the first message.
			if !strings.HasPrefix(actual, "[foo] ") {
				t.Errorf("First client failed to get 'foo' name: %q", actual)
			}
			actual = stripPrompt(actual)
			expected = " * Guest1 joined. (Connected: 2)\r"
			if actual != expected {
				t.Errorf("Got %q; expected %q", actual, expected)
			}

			return nil
		})
	})

	// Second client
	g.Go(func() error {
		// wait for the first client
		<-newUsers
		return sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)
			// Consume the initial buffer
			scanner.Scan()
			scanner.Scan()
			scanner.Scan()

			actual := scanner.Text()
			if !strings.HasPrefix(actual, "[Guest1] ") {
				t.Errorf("Second client did not get Guest1 name: %q", actual)
			}
			return nil
		})
	})

	if err := g.Wait(); err != nil {
		t.Error(err)
	}
}

func TestHostWhitelist(t *testing.T) {
	auth := NewAuth()
	s, host := getHost(t, auth)
	defer s.Close()
	go host.Serve()

	target := s.Addr().String()

	err := sshd.ConnectShell(target, "foo", func(r io.Reader, w io.WriteCloser) error { return nil })
	if err != nil {
		t.Error(err)
	}

	clientkey, err := rsa.GenerateKey(rand.Reader, 512)
	if err != nil {
		t.Fatal(err)
	}

	clientpubkey, _ := ssh.NewPublicKey(clientkey.Public())
	auth.Whitelist(clientpubkey, 0)
	auth.SetWhitelistMode(true)

	err = sshd.ConnectShell(target, "foo", func(r io.Reader, w io.WriteCloser) error { return nil })
	if err == nil {
		t.Error("Failed to block unwhitelisted connection.")
	}
}

func TestHostAllowlistCommand(t *testing.T) {
	s, host := getHost(t, NewAuth())
	defer s.Close()
	go host.Serve()

	users := make(chan *message.User)
	host.OnUserJoined = func(u *message.User) {
		users <- u
	}

	sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
		<-users
		m, ok := host.MemberByID("foo")
		if !ok {
			t.Fatal("can't get member foo")
		}

		scanner := bufio.NewScanner(r)
		scanner.Scan() // Joined

		assertLineEq := func(expected string) {
			if !scanner.Scan() {
				t.Error("no line available")
			}
			if actual := stripPrompt(scanner.Text()); actual != expected {
				t.Errorf("expected %q, got %q", expected, actual)
			}
		}
		sendCmd := func(cmd string, formatting ...interface{}) {
			host.HandleMsg(message.ParseInput(fmt.Sprintf(cmd, formatting...), m.User))
		}

		sendCmd("/allowlist")
		assertLineEq("Err: must be op\r")
		m.IsOp = true
		sendCmd("/allowlist")
		for _, expected := range [...]string{"Usage", "help", "on, off", "add, remove", "import", "reload", "reverify", "status"} {
			if !scanner.Scan() {
				t.Error("no line available")
			}
			if actual := stripPrompt(scanner.Text()); !strings.HasPrefix(actual, expected) {
				t.Errorf("Unexpected help message order: have %q, want prefix %q", actual, expected)
			}
		}

		sendCmd("/allowlist on")
		if !host.auth.WhitelistMode() {
			t.Error("allowlist not enabled after /allowlist on")
		}
		sendCmd("/allowlist off")
		if host.auth.WhitelistMode() {
			t.Error("allowlist not disabled after /allowlist off")
		}

		// TODO: can we pass a public key when connecting?
		// useful for add, remove, import, reverify, status

		testKey1 := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPUiNw0nQku4pcUCbZcJlIEAIf5bXJYTy/DKI1vh5b+P"
		testKey1FP := "SHA256:GJNSl9NUcOS2pZYALn0C5Qgfh5deT+R+FfqNIUvpM9s="
		testKey2 := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINDnlvlhBf4Jx7RlqTO6C5iOUhsBk2CHOpwPgPUbo8vb"
		testKey2FP := "SHA256:tMBXmUCPMxbSNj1pzQlGR+N2RiAIvcnqT18vX0r2rrM="

		sendCmd("/allowlist add ssh-invalid blah ssh-rsa wrongAsWell invalid foo %s %s", testKey1, testKey2)
		assertLineEq("users without a public key: [foo]\r")
		assertLineEq("invalid users: [invalid]\r")
		assertLineEq("invalid keys: [ssh-invalid blah ssh-rsa wrongAsWell]\r")
		if !host.auth.whitelist.In(testKey1FP) || !host.auth.whitelist.In(testKey2FP) {
			t.Error("failed to add keys to allowlist")
		}
		sendCmd("/allowlist remove invalid %s", testKey1)
		assertLineEq("invalid users: [invalid]\r")
		if host.auth.whitelist.In(testKey1FP) {
			t.Error("failed to remove key from allowlist")
		}
		if !host.auth.whitelist.In(testKey2FP) {
			t.Error("removed wrong key")
		}

		// TODO: to test the AGE arg, we need another connection and possibly a sleep
		sendCmd("/allowlist import")
		assertLineEq("users without a public key: [foo]\r")

		// TODO: test reload with files?
		sendCmd("/allowlist reload keep")
		if !host.auth.whitelist.In(testKey2FP) {
			t.Error("cleared allowlist to be kept")
		}
		sendCmd("/allowlist reload flush")
		if host.auth.whitelist.In(testKey2FP) {
			t.Error("kept allowlist to be cleared")
		}
		sendCmd("/allowlist reload thisIsWrong")
		assertLineEq("Err: must specify whether to keep or flush current entries\r")
		sendCmd("/allowlist reload")
		assertLineEq("Err: must specify whether to keep or flush current entries\r")

		sendCmd("/allowlist reverify")
		assertLineEq("allowlist is disabled, so nobody will be kicked\r")

		sendCmd("/allowlist add " + testKey1)
		sendCmd("/allowlist status")
		assertLineEq("The allowlist is currently disabled.\r")
		assertLineEq(fmt.Sprintf("The following keys of not connected users are on the allowlist: [%s]\r", testKey1FP))

		sendCmd("/allowlist invalidSubcommand")
		assertLineEq("Err: invalid subcommand: invalidSubcommand\r")
		return nil
	})
}

func TestHostKick(t *testing.T) {
	s, host := getHost(t, NewAuth())
	defer s.Close()
	go host.Serve()

	g := errgroup.Group{}
	connected := make(chan struct{})
	kicked := make(chan struct{})

	g.Go(func() error {
		// First client
		return sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)

			// Consume the initial buffer
			scanner.Scan() // Joined

			// Make op
			member, _ := host.Room.MemberByID("foo")
			if member == nil {
				return errors.New("failed to load MemberByID")
			}
			member.IsOp = true

			// Change nicks, make sure op sticks
			w.Write([]byte("/nick quux\r\n"))
			scanner.Scan() // Prompt
			scanner.Scan() // Nick change response

			// Block until second client is here
			connected <- struct{}{}
			scanner.Scan() // Connected message

			w.Write([]byte("/kick bar\r\n"))
			scanner.Scan() // Prompt

			scanner.Scan() // Kick result
			if actual, expected := stripPrompt(scanner.Text()), " * bar was kicked by quux.\r"; actual != expected {
				t.Errorf("Failed to detect kick:\n Got: %q;\nWant: %q", actual, expected)
			}

			kicked <- struct{}{}
			return nil
		})
	})

	g.Go(func() error {
		// Second client
		return sshd.ConnectShell(s.Addr().String(), "bar", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)
			<-connected
			scanner.Scan()

			<-kicked

			if _, err := w.Write([]byte("am I still here?\r\n")); err != io.EOF {
				return errors.New("expected to be kicked")
			}

			scanner.Scan()
			if err := scanner.Err(); err == io.EOF {
				// All good, we got kicked.
				return nil
			} else {
				return err
			}
		})
	})

	if err := g.Wait(); err != nil {
		t.Error(err)
	}
}

func TestTimestampEnvConfig(t *testing.T) {
	cases := []struct {
		input      string
		timeformat *string
	}{
		{"", strptr("15:04")},
		{"1", strptr("15:04")},
		{"0", nil},
		{"time +8h", strptr("15:04")},
		{"datetime +8h", strptr("2006-01-02 15:04:05")},
	}
	for _, tc := range cases {
		u := connectUserWithConfig(t, "dingus", map[string]string{
			"SSHCHAT_TIMESTAMP": tc.input,
		})
		userConfig := u.Config()
		if userConfig.Timeformat != nil && tc.timeformat != nil {
			if *userConfig.Timeformat != *tc.timeformat {
				t.Fatal("unexpected timeformat:", *userConfig.Timeformat, "expected:", *tc.timeformat)
			}
		}
	}
}

func strptr(s string) *string {
	return &s
}

func connectUserWithConfig(t *testing.T, name string, envConfig map[string]string) *message.User {
	s, host := getHost(t, nil)
	defer s.Close()

	newUsers := make(chan *message.User)
	host.OnUserJoined = func(u *message.User) {
		newUsers <- u
	}
	go host.Serve()

	clientConfig := sshd.NewClientConfig(name)
	conn, err := ssh.Dial("tcp", s.Addr().String(), clientConfig)
	if err != nil {
		t.Fatal("unable to connect to test ssh-chat server:", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		t.Fatal("unable to open session:", err)
	}
	defer session.Close()

	for key := range envConfig {
		session.Setenv(key, envConfig[key])
	}

	err = session.Shell()
	if err != nil {
		t.Fatal("unable to open shell:", err)
	}

	for u := range newUsers {
		if u.Name() == name {
			return u
		}
	}
	t.Fatalf("user %s not found in the host", name)
    return nil
}
