package sshchat

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
)

func stripPrompt(s string) string {
	pos := strings.LastIndex(s, "\033[K")
	if pos < 0 {
		return s
	}
	return s[pos+3:]
}

func TestHostGetPrompt(t *testing.T) {
	var expected, actual string

	u := message.NewUser(&Identity{id: "foo"})

	actual = GetPrompt(u)
	expected = "[foo] "
	if actual != expected {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}

	u.SetConfig(message.UserConfig{
		Theme: &message.Themes[0],
	})
	actual = GetPrompt(u)
	expected = "[\033[38;05;88mfoo\033[0m] "
	if actual != expected {
		t.Errorf("Got: %q; Expected: %q", actual, expected)
	}
}

func TestHostNameCollision(t *testing.T) {
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

	done := make(chan struct{}, 1)

	// First client
	go func() {
		err := sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)

			// Consume the initial buffer
			scanner.Scan()
			actual := stripPrompt(scanner.Text())
			expected := " * foo joined. (Connected: 1)\r"
			if actual != expected {
				t.Errorf("Got %q; expected %q", actual, expected)
			}

			// Ready for second client
			done <- struct{}{}

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
			close(done)
			return nil
		})
		if err != nil {
			done <- struct{}{}
			t.Fatal(err)
		}
	}()

	// Wait for first client
	<-done

	// Second client
	err = sshd.ConnectShell(s.Addr().String(), "foo", func(r io.Reader, w io.WriteCloser) error {
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
	if err != nil {
		t.Fatal(err)
	}

	<-done
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

	connected := make(chan struct{})
	kicked := make(chan struct{})
	done := make(chan struct{})

	go func() {
		// First client
		err := sshd.ConnectShell(addr, "foo", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)

			// Consume the initial buffer
			scanner.Scan()

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

			scanner.Scan()
			if actual, expected := stripPrompt(scanner.Text()), " * bar was kicked by quux.\r"; actual != expected {
				t.Errorf("Got %q; expected %q", actual, expected)
			}

			kicked <- struct{}{}

			return nil
		})
		if err != nil {
			connected <- struct{}{}
			close(connected)
			t.Fatal(err)
		}
	}()

	go func() {
		// Second client
		err := sshd.ConnectShell(addr, "bar", func(r io.Reader, w io.WriteCloser) error {
			scanner := bufio.NewScanner(r)
			<-connected
			scanner.Scan()

			<-kicked

			scanner.Scan()
			return scanner.Err()
		})
		if err != nil {
			close(done)
			t.Fatal(err)
		}
		close(done)
	}()

	<-done
}
