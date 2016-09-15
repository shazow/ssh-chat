package sshchat

import (
	"sync"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

type client struct {
	chat.Member
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

type User interface {
	chat.Member

	Connections() []sshd.Connection
	Close() error
}
