package sshchat

import (
	"sync"
	"time"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

type client struct {
	Member
	sync.Mutex
	conns []sshd.Connection
}

func (cl *client) Connections() []sshd.Connection {
	return cl.conns
}

func (cl *client) Close() error {
	// TODO: Stack errors?
	for _, conn := range cl.conns {
		conn.Close()
	}
	return nil
}

type Member interface {
	chat.Member

	Joined() time.Time
	Prompt() string
	ReplyTo() message.Author
	SetHighlight(string) error
	SetReplyTo(message.Author)
}

type User interface {
	Member

	Connections() []sshd.Connection
	Close() error
}
