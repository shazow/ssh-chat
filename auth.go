package main

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
)

// The error returned a key is checked that is not whitelisted, with whitelisting required.
var ErrNotWhitelisted = errors.New("not whitelisted")

// The error returned a key is checked that is banned.
var ErrBanned = errors.New("banned")

// NewAuthKey returns string from an ssh.PublicKey.
func NewAuthKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	// FIXME: Is there a way to index pubkeys without marshal'ing them into strings?
	return sshd.Fingerprint(key)
}

// NewAuthAddr returns a string from a net.Addr
func NewAuthAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}

// Auth stores fingerprint lookups
// TODO: Add timed auth by using a time.Time instead of struct{} for values.
type Auth struct {
	sync.RWMutex
	bannedAddr *Set
	banned     *Set
	whitelist  *Set
	ops        *Set
}

// NewAuth creates a new default Auth.
func NewAuth() *Auth {
	return &Auth{
		bannedAddr: NewSet(),
		banned:     NewSet(),
		whitelist:  NewSet(),
		ops:        NewSet(),
	}
}

// AllowAnonymous determines if anonymous users are permitted.
func (a Auth) AllowAnonymous() bool {
	return a.whitelist.Len() == 0
}

// Check determines if a pubkey fingerprint is permitted.
func (a *Auth) Check(addr net.Addr, key ssh.PublicKey) (bool, error) {
	authkey := NewAuthKey(key)

	if a.whitelist.Len() != 0 {
		// Only check whitelist if there is something in it, otherwise it's disabled.
		whitelisted := a.whitelist.In(authkey)
		if !whitelisted {
			return false, ErrNotWhitelisted
		}
		return true, nil
	}

	banned := a.banned.In(authkey)
	if !banned {
		banned = a.bannedAddr.In(NewAuthAddr(addr))
	}
	if banned {
		return false, ErrBanned
	}

	return true, nil
}

// Op sets a public key as a known operator.
func (a *Auth) Op(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	authkey := NewAuthKey(key)
	if d != 0 {
		a.ops.AddExpiring(authkey, d)
	} else {
		a.ops.Add(authkey)
	}
	logger.Debugf("Added to ops: %s (for %s)", authkey, d)
}

// IsOp checks if a public key is an op.
func (a *Auth) IsOp(key ssh.PublicKey) bool {
	if key == nil {
		return false
	}
	authkey := NewAuthKey(key)
	return a.ops.In(authkey)
}

// Whitelist will set a public key as a whitelisted user.
func (a *Auth) Whitelist(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	authkey := NewAuthKey(key)
	if d != 0 {
		a.whitelist.AddExpiring(authkey, d)
	} else {
		a.whitelist.Add(authkey)
	}
	logger.Debugf("Added to whitelist: %s (for %s)", authkey, d)
}

// Ban will set a public key as banned.
func (a *Auth) Ban(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	a.BanFingerprint(NewAuthKey(key), d)
}

// BanFingerprint will set a public key fingerprint as banned.
func (a *Auth) BanFingerprint(authkey string, d time.Duration) {
	if d != 0 {
		a.banned.AddExpiring(authkey, d)
	} else {
		a.banned.Add(authkey)
	}
	logger.Debugf("Added to banned: %s (for %s)", authkey, d)
}

// Ban will set an IP address as banned.
func (a *Auth) BanAddr(addr net.Addr, d time.Duration) {
	key := NewAuthAddr(addr)
	if d != 0 {
		a.bannedAddr.AddExpiring(key, d)
	} else {
		a.bannedAddr.Add(key)
	}
	logger.Debugf("Added to bannedAddr: %s (for %s)", key, d)
}
