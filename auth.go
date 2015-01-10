package main

import (
	"errors"
	"sync"

	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
)

// The error returned a key is checked that is not whitelisted, with whitelisting required.
var ErrNotWhitelisted = errors.New("not whitelisted")

// The error returned a key is checked that is banned.
var ErrBanned = errors.New("banned")

// AuthKey is the type that our lookups are keyed against.
type AuthKey string

// NewAuthKey returns an AuthKey from an ssh.PublicKey.
func NewAuthKey(key ssh.PublicKey) AuthKey {
	// FIXME: Is there a way to index pubkeys without marshal'ing them into strings?
	return AuthKey(string(key.Marshal()))
}

// Auth stores fingerprint lookups
type Auth struct {
	sshd.Auth
	sync.RWMutex
	whitelist map[AuthKey]struct{}
	banned    map[AuthKey]struct{}
	ops       map[AuthKey]struct{}
}

// NewAuth creates a new default Auth.
func NewAuth() *Auth {
	return &Auth{
		whitelist: make(map[AuthKey]struct{}),
		banned:    make(map[AuthKey]struct{}),
		ops:       make(map[AuthKey]struct{}),
	}
}

// AllowAnonymous determines if anonymous users are permitted.
func (a Auth) AllowAnonymous() bool {
	a.RLock()
	ok := len(a.whitelist) == 0
	a.RUnlock()
	return ok
}

// Check determines if a pubkey fingerprint is permitted.
func (a Auth) Check(key ssh.PublicKey) (bool, error) {
	authkey := NewAuthKey(key)

	a.RLock()
	defer a.RUnlock()

	if len(a.whitelist) > 0 {
		// Only check whitelist if there is something in it, otherwise it's disabled.

		_, whitelisted := a.whitelist[authkey]
		if !whitelisted {
			return false, ErrNotWhitelisted
		}
	}

	_, banned := a.banned[authkey]
	if banned {
		return false, ErrBanned
	}

	return true, nil
}

// Op will set a fingerprint as a known operator.
func (a *Auth) Op(key ssh.PublicKey) {
	authkey := NewAuthKey(key)
	a.Lock()
	a.ops[authkey] = struct{}{}
	a.Unlock()
}

// Whitelist will set a fingerprint as a whitelisted user.
func (a *Auth) Whitelist(key ssh.PublicKey) {
	authkey := NewAuthKey(key)
	a.Lock()
	a.whitelist[authkey] = struct{}{}
	a.Unlock()
}

// Ban will set a fingerprint as banned.
func (a *Auth) Ban(key ssh.PublicKey) {
	authkey := NewAuthKey(key)
	a.Lock()
	a.banned[authkey] = struct{}{}
	a.Unlock()
}
