package main

import (
	"fmt"
	"net"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/sshd"
)

// Identity is a container for everything that identifies a client.
type Identity struct {
	sshd.Connection
	id chat.Id
}

// NewIdentity returns a new identity object from an sshd.Connection.
func NewIdentity(conn sshd.Connection) *Identity {
	id := chat.Id(conn.Name())
	return &Identity{
		Connection: conn,
		id:         id,
	}
}

func (i Identity) Id() chat.Id {
	return chat.Id(i.id)
}

func (i *Identity) SetId(id chat.Id) {
	i.id = id
}

func (i *Identity) SetName(name string) {
	i.SetId(chat.Id(name))
}

func (i Identity) Name() string {
	return string(i.id)
}

func (i Identity) Whois() string {
	ip, _, _ := net.SplitHostPort(i.RemoteAddr().String())
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	return fmt.Sprintf("name: %s"+chat.Newline+
		" > ip: %s"+chat.Newline+
		" > fingerprint: %s", i.Name(), ip, fingerprint)
}
