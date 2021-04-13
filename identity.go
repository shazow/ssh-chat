package sshchat

import (
	"fmt"
	"net"
	"strings"
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
	symbol  string // symbol is displayed as a prefix to the name
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

func (i *Identity) SetSymbol(symbol string) {
	i.symbol = symbol
}

// Name returns the name for the Identity
func (i Identity) Name() string {
	if i.symbol != "" {
		return i.symbol + " " + i.id
	}
	return i.id
}

// Whois returns a whois description for non-admin users.
func (i Identity) Whois(room *chat.Room) string {
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}
	// TODO: Rewrite this using strings.Builder like WhoisAdmin

	awayMsg := ""
	if m, ok := room.MemberByID(i.ID()); ok {
		isAway, awaySince, awayMessage := m.GetAway()
		if isAway {
			awayMsg = fmt.Sprintf("%s > away: (%s ago) %s", message.Newline, humantime.Since(awaySince), awayMessage)
		}
	}
	return "name: " + i.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + sanitize.Data(string(i.ClientVersion()), 64) + message.Newline +
		" > joined: " + humantime.Since(i.created) + " ago" +
		awayMsg
}

// WhoisAdmin returns a whois description for admin users.
func (i Identity) WhoisAdmin(room *chat.Room) string {
	ip, _, _ := net.SplitHostPort(i.RemoteAddr().String())
	fingerprint := "(no public key)"
	if i.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(i.PublicKey())
	}

	out := strings.Builder{}
	out.WriteString("name: " + i.Name() + message.Newline +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + sanitize.Data(string(i.ClientVersion()), 64) + message.Newline +
		" > joined: " + humantime.Since(i.created) + " ago")

	if member, ok := room.MemberByID(i.ID()); ok {
		// Add room-specific whois
		if isAway, awaySince, awayMessage := member.GetAway(); isAway {
			fmt.Fprintf(&out, message.Newline+" > away: (%s ago) %s", humantime.Since(awaySince), awayMessage)
		}
		// FIXME: Should these always be present, even if they're false? Maybe
		// change that once we add room context to Whois() above.
		if !member.LastMsg().IsZero() {
			out.WriteString(message.Newline + " > room/messaged: " + humantime.Since(member.LastMsg()) + " ago")
		}
		if room.IsOp(member.User) {
			out.WriteString(message.Newline + " > room/op: true")
		}
	}

	return out.String()
}
