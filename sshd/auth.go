package sshd

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"time"

	"github.com/shazow/ssh-chat/internal/sanitize"
	"golang.org/x/crypto/ssh"
)

// Auth is used to authenticate connections.
type Auth interface {
	// Whether to allow connections without a public key.
	AllowAnonymous() bool
	// If passphrase authentication is accepted
	AcceptPassphrase() bool
	// Given address and public key and client agent string, returns nil if the connection is not banned.
	CheckBans(net.Addr, ssh.PublicKey, string) error
	// Given a public key, returns nil if the connection should be allowed.
	CheckPublicKey(ssh.PublicKey) error
	// Given a passphrase, returns nil if the connection should be allowed.
	CheckPassphrase(string) error
	// BanAddr bans an IP address for the specified amount of time.
	BanAddr(net.Addr, time.Duration)
}

// MakeAuth makes an ssh.ServerConfig which performs authentication against an Auth implementation.
// TODO: Switch to using ssh.AuthMethod instead?
func MakeAuth(auth Auth) *ssh.ServerConfig {
	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			err := auth.CheckBans(conn.RemoteAddr(), key, sanitize.Data(string(conn.ClientVersion()), 64))
			if err != nil {
				return nil, err
			}
			err = auth.CheckPublicKey(key)
			if err != nil {
				return nil, err
			}
			perm := &ssh.Permissions{Extensions: map[string]string{
				"pubkey": string(key.Marshal()),
			}}
			return perm, nil
		},

		// We use KeyboardInteractiveCallback instead of PasswordCallback to
		// avoid preventing the client from including a pubkey in the user
		// identification.
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			err := auth.CheckBans(conn.RemoteAddr(), nil, sanitize.Data(string(conn.ClientVersion()), 64))
			if err != nil {
				return nil, err
			}
			if auth.AcceptPassphrase() {
				var answers []string
				answers, err = challenge("", "", []string{"Passphrase required to connect: "}, []bool{true})
				if err == nil {
					if len(answers) != 1 {
						err = errors.New("didn't get passphrase")
					} else {
						err = auth.CheckPassphrase(answers[0])
						if err != nil {
							auth.BanAddr(conn.RemoteAddr(), time.Second*2)
						}
					}
				}
			} else if !auth.AllowAnonymous() {
				err = errors.New("public key authentication required")
			}
			return nil, err
		},
	}

	return &config
}

// MakeNoAuth makes a simple ssh.ServerConfig which allows all connections.
// Primarily used for testing.
func MakeNoAuth() *ssh.ServerConfig {
	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			perm := &ssh.Permissions{Extensions: map[string]string{
				"pubkey": string(key.Marshal()),
			}}
			return perm, nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			return nil, nil
		},
	}

	return &config
}

// Fingerprint performs a SHA256 BASE64 fingerprint of the PublicKey, similar to OpenSSH.
// See: https://anongit.mindrot.org/openssh.git/commit/?id=56d1c83cdd1ac
func Fingerprint(k ssh.PublicKey) string {
	hash := sha256.Sum256(k.Marshal())
	return "SHA256:" + base64.StdEncoding.EncodeToString(hash[:])
}
