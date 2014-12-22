package sshd

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

var errBanned = errors.New("banned")
var errNotWhitelisted = errors.New("not whitelisted")
var errNoInteractive = errors.New("public key authentication required")

type Auth interface {
	IsBanned(ssh.PublicKey) bool
	IsWhitelisted(ssh.PublicKey) bool
}

func MakeAuth(auth Auth) *ssh.ServerConfig {
	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if auth.IsBanned(key) {
				return nil, errBanned
			}
			if !auth.IsWhitelisted(key) {
				return nil, errNotWhitelisted
			}
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": Fingerprint(key)}}
			return perm, nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			if auth.IsBanned(nil) {
				return nil, errNoInteractive
			}
			if !auth.IsWhitelisted(nil) {
				return nil, errNotWhitelisted
			}
			return nil, nil
		},
	}

	return &config
}

func MakeNoAuth() *ssh.ServerConfig {
	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			return nil, nil
		},
	}

	return &config
}

func Fingerprint(k ssh.PublicKey) string {
	hash := sha1.Sum(k.Marshal())
	r := fmt.Sprintf("% x", hash)
	return strings.Replace(r, " ", ":", -1)
}
