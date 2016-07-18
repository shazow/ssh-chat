package sshd

import (
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

var keepaliveInterval = time.Second * 30
var keepaliveRequest = "keepalive@ssh-chat"

// Connection is an interface with fields necessary to operate an sshd host.
type Connection interface {
	PublicKey() ssh.PublicKey
	RemoteAddr() net.Addr
	Name() string
	Close() error
}

type sshConn struct {
	*ssh.ServerConn
}

func (c sshConn) PublicKey() ssh.PublicKey {
	if c.Permissions == nil {
		return nil
	}

	s, ok := c.Permissions.Extensions["pubkey"]
	if !ok {
		return nil
	}

	key, err := ssh.ParsePublicKey([]byte(s))
	if err != nil {
		return nil
	}

	return key
}

func (c sshConn) Name() string {
	return c.User()
}

// Extending ssh/terminal to include a closer interface
type Terminal struct {
	terminal.Terminal
	Conn    Connection
	Channel ssh.Channel
}

// Make new terminal from a session channel
func NewTerminal(conn *ssh.ServerConn, ch ssh.NewChannel) (*Terminal, error) {
	if ch.ChannelType() != "session" {
		return nil, errors.New("terminal requires session channel")
	}
	channel, requests, err := ch.Accept()
	if err != nil {
		return nil, err
	}
	term := Terminal{
		*terminal.NewTerminal(channel, "Connecting..."),
		sshConn{conn},
		channel,
	}

	go term.listen(requests)
	go func() {
		// FIXME: Is this necessary?
		conn.Wait()
		channel.Close()
	}()

	go func() {
		for range time.Tick(keepaliveInterval) {
			_, err := channel.SendRequest(keepaliveRequest, true, nil)
			if err != nil {
				// Connection is gone
				conn.Close()
				return
			}
		}
	}()

	return &term, nil
}

// Find session channel and make a Terminal from it
func NewSession(conn *ssh.ServerConn, channels <-chan ssh.NewChannel) (term *Terminal, err error) {
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

	if term != nil {
		// Reject the rest.
		// FIXME: Do we need this?
		go func() {
			for ch := range channels {
				ch.Reject(ssh.Prohibited, "only one session allowed")
			}
		}()
	}

	return term, err
}

// Close terminal and ssh connection
func (t *Terminal) Close() error {
	return t.Conn.Close()
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
