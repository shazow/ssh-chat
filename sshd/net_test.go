package sshd

import (
	"bytes"
	"io"
	"testing"
)

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
	signer, err := NewRandomSigner(512)
	config := MakeNoAuth()
	config.AddHostKey(signer)

	s, err := ListenSSH(":0", config)
	if err != nil {
		t.Fatal(err)
	}

	terminals := make(chan *Terminal)
	s.HandlerFunc = func(term *Terminal) {
		terminals <- term
	}
	go s.Serve()

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

	err = ConnectShell(host, name, func(r io.Reader, w io.WriteCloser) error {
		// Consume if there is anything
		buf := new(bytes.Buffer)
		w.Write([]byte("hello\r\n"))

		buf.Reset()
		_, err := io.Copy(buf, r)

		expected := "> hello\r\necho: hello\r\n"
		actual := buf.String()
		if actual != expected {
			if err != nil {
				t.Error(err)
			}
			t.Errorf("Got %q; expected %q", actual, expected)
		}
		s.Close()
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
}
