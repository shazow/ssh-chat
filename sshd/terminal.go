package sshd

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// Extending ssh/terminal to include a closer interface
type Terminal struct {
	*terminal.Terminal
	Conn    ssh.Conn
	Channel ssh.Channel
}

// Make new terminal from a session channel
func NewTerminal(conn ssh.Conn, ch ssh.NewChannel) (*Terminal, error) {
	if ch.ChannelType() != "session" {
		return nil, errors.New("terminal requires session channel")
	}
	channel, requests, err := ch.Accept()
	if err != nil {
		return nil, err
	}
	term := Terminal{
		terminal.NewTerminal(channel, "Connecting..."),
		conn,
		channel,
	}

	go term.listen(requests)
	return &term, nil
}

// Find session channel and make a Terminal from it
func NewSession(conn ssh.Conn, channels <-chan ssh.NewChannel) (term *Terminal, err error) {
	for ch := range channels {
		if t := ch.ChannelType(); t != "session" {
			ch.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			continue
		}

		term, err = NewTerminal(conn, ch)
		if err == nil {
			break
		}
	}

	return term, err
}

// Close terminal and ssh connection
func (t *Terminal) Close() error {
	return MultiCloser{t.Channel, t.Conn}.Close()
}

// Negotiate terminal type and settings
func (t *Terminal) listen(requests <-chan *ssh.Request) {
	hasShell := false

	for req := range requests {
		var width, height int
		var ok bool

		switch req.Type {
		case "shell":
			if !hasShell {
				ok = true
				hasShell = true
			}
		case "pty-req":
			width, height, ok = parsePtyRequest(req.Payload)
			if ok {
				// TODO: Hardcode width to 100000?
				err := t.SetSize(width, height)
				ok = err == nil
			}
		case "window-change":
			width, height, ok = parseWinchRequest(req.Payload)
			if ok {
				// TODO: Hardcode width to 100000?
				err := t.SetSize(width, height)
				ok = err == nil
			}
		}

		if req.WantReply {
			req.Reply(ok, nil)
		}
	}
}
