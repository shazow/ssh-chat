package sshchat

import (
	"net"

	humanize "github.com/dustin/go-humanize"
	"github.com/shazow/ssh-chat/chat/message"
	"github.com/shazow/ssh-chat/sshd"
)

// Helpers for printing whois messages

func whoisPublic(u User) string {
	fingerprint := "(no public key)"
	// FIXME: Use all connections?
	conn := u.Connections()[0]
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.Joined())
}

func whoisAdmin(u User) string {
	// FIXME: Use all connections?
	conn := u.Connections()[0]
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	fingerprint := "(no public key)"
	if conn.PublicKey() != nil {
		fingerprint = sshd.Fingerprint(conn.PublicKey())
	}
	return "name: " + u.Name() + message.Newline +
		" > ip: " + ip + message.Newline +
		" > fingerprint: " + fingerprint + message.Newline +
		" > client: " + SanitizeData(string(conn.ClientVersion())) + message.Newline +
		" > joined: " + humanize.Time(u.Joined())
}
