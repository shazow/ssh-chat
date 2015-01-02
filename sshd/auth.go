package sshd

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

type Auth interface {
	AllowAnonymous() bool
	Check(string) (bool, error)
}

func MakeAuth(auth Auth) *ssh.ServerConfig {
	config := ssh.ServerConfig{
		NoClientAuth: false,
		// Auth-related things should be constant-time to avoid timing attacks.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := Fingerprint(key)
			ok, err := auth.Check(fingerprint)
			if !ok {
				return nil, err
			}
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": fingerprint}}
			return perm, nil
		},
		KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			if !auth.AllowAnonymous() {
				return nil, errors.New("public key authentication required")
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
			perm := &ssh.Permissions{Extensions: map[string]string{"fingerprint": Fingerprint(key)}}
			return perm, nil
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
