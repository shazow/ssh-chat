package main

import (
	"errors"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// The error returned a key is checked that is not whitelisted, with whitelisting required.
var ErrNotWhitelisted = errors.New("not whitelisted")

// The error returned a key is checked that is banned.
var ErrBanned = errors.New("banned")

// AuthKey is the type that our lookups are keyed against.
type AuthKey string

// NewAuthKey returns string from an ssh.PublicKey.
func NewAuthKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	// FIXME: Is there a way to index pubkeys without marshal'ing them into strings?
	return string(key.Marshal())
}

// NewAuthAddr returns a string from a net.Addr
func NewAuthAddr(addr net.Addr) string {
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}

// Auth stores fingerprint lookups
// TODO: Add timed auth by using a time.Time instead of struct{} for values.
type Auth struct {
	sync.RWMutex
	bannedAddr map[string]struct{}
	banned     map[string]struct{}
	whitelist  map[string]struct{}
	ops        map[string]struct{}
}

// NewAuth creates a new default Auth.
func NewAuth() *Auth {
	return &Auth{
		bannedAddr: make(map[string]struct{}),
		banned:     make(map[string]struct{}),
		whitelist:  make(map[string]struct{}),
		ops:        make(map[string]struct{}),
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
func (a Auth) Check(addr net.Addr, key ssh.PublicKey) (bool, error) {
	authkey := NewAuthKey(key)

	a.RLock()
	defer a.RUnlock()

	if len(a.whitelist) > 0 {
		// Only check whitelist if there is something in it, otherwise it's disabled.

		_, whitelisted := a.whitelist[authkey]
		if !whitelisted {
			return false, ErrNotWhitelisted
		}
		return true, nil
	}

	_, banned := a.banned[authkey]
	if !banned {
		_, banned = a.bannedAddr[NewAuthAddr(addr)]
	}
	if banned {
		return false, ErrBanned
	}

	return true, nil
}

// Op will set a fingerprint as a known operator.
func (a *Auth) Op(key ssh.PublicKey) {
	if key == nil {
		// Don't process empty keys.
		return
	}
	authkey := NewAuthKey(key)
	a.Lock()
	a.ops[authkey] = struct{}{}
	a.Unlock()
}

// IsOp checks if a public key is an op.
func (a Auth) IsOp(key ssh.PublicKey) bool {
	if key == nil {
		return false
	}
	authkey := NewAuthKey(key)
	a.RLock()
	_, ok := a.ops[authkey]
	a.RUnlock()
	return ok
}

// Whitelist will set a public key as a whitelisted user.
func (a *Auth) Whitelist(key ssh.PublicKey) {
	if key == nil {
		// Don't process empty keys.
		return
	}
	authkey := NewAuthKey(key)
	a.Lock()
	a.whitelist[authkey] = struct{}{}
	a.Unlock()
}

// Ban will set a public key as banned.
func (a *Auth) Ban(key ssh.PublicKey) {
	if key == nil {
		// Don't process empty keys.
		return
	}
	authkey := NewAuthKey(key)

	a.Lock()
	a.banned[authkey] = struct{}{}
	a.Unlock()
}

// Ban will set an IP address as banned.
func (a *Auth) BanAddr(addr net.Addr) {
	key := NewAuthAddr(addr)

	a.Lock()
	a.bannedAddr[key] = struct{}{}
	a.Unlock()
}
