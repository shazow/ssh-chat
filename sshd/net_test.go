package sshd

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"io"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TODO: Move some of these into their own package?

func MakeKey(bits int) (ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

func NewClientSession(host string, name string, handler func(r io.Reader, w io.WriteCloser)) error {
	config := &ssh.ClientConfig{
		User: name,
		Auth: []ssh.AuthMethod{
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) (answers []string, err error) {
				return
			}),
		},
	}

	conn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return err
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	in, err := session.StdinPipe()
	if err != nil {
		return err
	}

	out, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	err = session.Shell()
	if err != nil {
		return err
	}

	handler(out, in)

	return nil
}

func TestServerInit(t *testing.T) {
	config := MakeNoAuth()
	s, err := ListenSSH(":badport", config)
	if err == nil {
		t.Fatal("should fail on bad port")
	}

	s, err = ListenSSH(":0", config)
	if err != nil {
		t.Error(err)
	}

	err = s.Close()
	if err != nil {
		t.Error(err)
	}
}

func TestServeTerminals(t *testing.T) {
	signer, err := MakeKey(512)
	config := MakeNoAuth()
	config.AddHostKey(signer)

	s, err := ListenSSH(":0", config)
	if err != nil {
		t.Fatal(err)
	}

	terminals := s.ServeTerminal()

	go func() {
		// Accept one terminal, read from it, echo back, close.
		term := <-terminals
		term.SetPrompt("> ")

		line, err := term.ReadLine()
		if err != nil {
			t.Error(err)
		}
		_, err = term.Write([]byte("echo: " + line + "\r\n"))
		if err != nil {
			t.Error(err)
		}

		term.Close()
	}()

	host := s.Addr().String()
	name := "foo"

	err = NewClientSession(host, name, func(r io.Reader, w io.WriteCloser) {
		// Consume if there is anything
		buf := new(bytes.Buffer)
		w.Write([]byte("hello\r\n"))

		buf.Reset()
		_, err := io.Copy(buf, r)
		if err != nil {
			t.Error(err)
		}

		expected := "> hello\r\necho: hello\r\n"
		actual := buf.String()
		if actual != expected {
			t.Errorf("Got `%s`; expected `%s`", actual, expected)
		}
		s.Close()
	})

	if err != nil {
		t.Fatal(err)
	}
}
