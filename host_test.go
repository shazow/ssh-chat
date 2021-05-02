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

func TestHostNameCollision(t *testing.T) {
	t.Skip("Test is flakey on CI. :(")

	key, err := sshd.NewRandomSigner(512)
	if err != nil {
		t.Fatal(err)
	}
	config := sshd.MakeNoAuth()
	config.AddHostKey(key)

	s, err := sshd.ListenSSH("localhost:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host := NewHost(s, nil)
	go host.Serve()

	ready := make(chan struct{})
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

			// Ready for second client
			ready <- struct{}{}

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

			// Wrap it up.
			close(ready)
			return nil
		})
	})

	// Wait for first client
	<-ready

	// Second client
	g.Go(func() error {
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
	key, err := sshd.NewRandomSigner(512)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(key)

	s, err := sshd.ListenSSH("localhost:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	host := NewHost(s, auth)
	go host.Serve()

	target := s.Addr().String()

	err = sshd.ConnectShell(target, "foo", func(r io.Reader, w io.WriteCloser) error { return nil })
	if err != nil {
		t.Error(err)
	}

	clientkey, err := rsa.GenerateKey(rand.Reader, 512)
	if err != nil {
		t.Fatal(err)
	}

	clientpubkey, _ := ssh.NewPublicKey(clientkey.Public())
	auth.Whitelist(clientpubkey, 0)

	err = sshd.ConnectShell(target, "foo", func(r io.Reader, w io.WriteCloser) error { return nil })
	if err == nil {
		t.Error("Failed to block unwhitelisted connection.")
	}
}

func TestHostKick(t *testing.T) {
	key, err := sshd.NewRandomSigner(512)
	if err != nil {
		t.Fatal(err)
	}

	auth := NewAuth()
	config := sshd.MakeAuth(auth)
	config.AddHostKey(key)

	s, err := sshd.ListenSSH("localhost:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addr := s.Addr().String()
	host := NewHost(s, nil)
	go host.Serve()

	g := errgroup.Group{}
	connected := make(chan struct{})
	kicked := make(chan struct{})

	g.Go(func() error {
		// First client
		return sshd.ConnectShell(addr, "foo", func(r io.Reader, w io.WriteCloser) error {
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
		return sshd.ConnectShell(addr, "bar", func(r io.Reader, w io.WriteCloser) error {
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
		u, err := connectUserWithConfig("dingus", map[string]string{
			"SSHCHAT_TIMESTAMP": tc.input,
		})
		if err != nil {
			t.Fatal(err)
		}
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

func connectUserWithConfig(name string, envConfig map[string]string) (*message.User, error) {
	key, err := sshd.NewRandomSigner(512)
	if err != nil {
		return nil, fmt.Errorf("unable to create signer: %w", err)
	}
	config := sshd.MakeNoAuth()
	config.AddHostKey(key)

	s, err := sshd.ListenSSH("localhost:0", config)
	if err != nil {
		return nil, fmt.Errorf("unable to create a test server: %w", err)
	}
	defer s.Close()
	host := NewHost(s, nil)

	newUsers := make(chan *message.User)
	host.OnUserJoined = func(u *message.User) {
		newUsers <- u
	}
	go host.Serve()

	clientConfig := sshd.NewClientConfig(name)
	conn, err := ssh.Dial("tcp", s.Addr().String(), clientConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to test ssh-chat server: %w", err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("unable to open session: %w", err)
	}
	defer session.Close()

	for key := range envConfig {
		session.Setenv(key, envConfig[key])
	}

	err = session.Shell()
	if err != nil {
		return nil, fmt.Errorf("unable to open shell: %w", err)
	}

	for u := range newUsers {
		if u.Name() == name {
			return u, nil
		}
	}
	return nil, fmt.Errorf("user %s not found in the host", name)
}
