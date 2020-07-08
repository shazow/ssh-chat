package sshchat

import (
	"net"
	"time"

	"github.com/shazow/ssh-chat/chat"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/internal/humantime"
	"github.com/shazow/ssh-chat/internal/sanitize"
	"github.com/shazow/ssh-chat/sshd"
)

// Identity is a container for everything that identifies a client.
type Identity struct {
	sshd.Connection
	id      string
	created time.Time
}

// NewIdentity returns a new identity object from an sshd.Connection.
func NewIdentity(conn sshd.Connection) *Identity {
	return &Identity{
		Connection: conn,
		id:         sanitize.Name(conn.Name()),
		created:    time.Now(),
	}
}

// ID returns the name for the Identity
func (i Identity) ID() string {
	return i.id
}

// SetID Changes the Identity's name
func (i *Identity) SetID(id string) {
	i.id = id
}

// SetName Changes the Identity's name
func (i *Identity) SetName(name string) {
	i.SetID(name)
}

// Name returns the name for the Identity
func (i Identity) Name() string {
	return i.id
}

// Whois returns a whois description for non-admin users.
func (i Identity) Whois() string {
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	return "name: " + i.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + sanitize.Data(string(i.ClientVersion()), 64) + message.Newline +
		" > joined: " + humantime.Since(i.created) + " ago"
}

// WhoisAdmin returns a whois description for admin users.
func (i Identity) WhoisAdmin(room *chat.Room, host *Host) string {
	ip, _, _ := net.SplitHostPort(i.RemoteAddr().String())
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}

	isOp := ""
	user, ok := host.GetUser(i.id)
	if ok && room.IsOp(user) {
		isOp = " > Op" + message.Newline
	}

	return "name: " + i.Name() + message.Newline +
		isOp +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + sanitize.Data(string(i.ClientVersion()), 64) + message.Newline +
		" > joined: " + humantime.Since(i.created) + " ago"
}
