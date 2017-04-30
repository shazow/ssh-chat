package sshchat

import (
	"io"
	"sync"
	"time"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

type multiTerm interface {
	Connections() []sshd.Connection
	Add(*sshd.Terminal)
	ReadLine() (string, error)
	io.WriteCloser
}

type client struct {
	Member
	sync.Mutex
	multiTerm
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
