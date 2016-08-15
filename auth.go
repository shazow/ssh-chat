package sshchat

import (
	"errors"
	"net"
	"time"

	"github.com/shazow/ssh-chat/set"
	"github.com/shazow/ssh-chat/sshd"
	"golang.org/x/crypto/ssh"
)

// The error returned a key is checked that is not whitelisted, with whitelisting required.
var ErrNotWhitelisted = errors.New("not whitelisted")

// The error returned a key is checked that is banned.
var ErrBanned = errors.New("banned")

// newAuthKey returns string from an ssh.PublicKey used to index the key in our lookup.
func newAuthKey(key ssh.PublicKey) string {
	if key == nil {
		return ""
	}
	// FIXME: Is there a better way to index pubkeys without marshal'ing them into strings?
	return sshd.Fingerprint(key)
}

func newAuthItem(key ssh.PublicKey) set.Item {
	return set.StringItem(newAuthKey(key))
}

// newAuthAddr returns a string from a net.Addr used to index the address the key in our lookup.
func newAuthAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, _ := net.SplitHostPort(addr.String())
	return host
}

// Auth stores lookups for bans, whitelists, and ops. It implements the sshd.Auth interface.
type Auth struct {
	bannedAddr *set.Set
	banned     *set.Set
	whitelist  *set.Set
	ops        *set.Set
}

// NewAuth creates a new empty Auth.
func NewAuth() *Auth {
	return &Auth{
		bannedAddr: set.New(),
		banned:     set.New(),
		whitelist:  set.New(),
		ops:        set.New(),
	}
}

// AllowAnonymous determines if anonymous users are permitted.
func (a *Auth) AllowAnonymous() bool {
	return a.whitelist.Len() == 0
}

// Check determines if a pubkey fingerprint is permitted.
func (a *Auth) Check(addr net.Addr, key ssh.PublicKey) (bool, error) {
	authkey := newAuthKey(key)

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
		banned = a.bannedAddr.In(newAuthAddr(addr))
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
	authItem := newAuthItem(key)
	if d != 0 {
		a.ops.Add(set.Expire(authItem, d))
	} else {
		a.ops.Add(authItem)
	}
	logger.Debugf("Added to ops: %s (for %s)", authItem.Key(), d)
}

// IsOp checks if a public key is an op.
func (a *Auth) IsOp(key ssh.PublicKey) bool {
	if key == nil {
		return false
	}
	authkey := newAuthKey(key)
	return a.ops.In(authkey)
}

// Whitelist will set a public key as a whitelisted user.
func (a *Auth) Whitelist(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	authItem := newAuthItem(key)
	if d != 0 {
		a.whitelist.Add(set.Expire(authItem, d))
	} else {
		a.whitelist.Add(authItem)
	}
	logger.Debugf("Added to whitelist: %s (for %s)", authItem.Key(), d)
}

// Ban will set a public key as banned.
func (a *Auth) Ban(key ssh.PublicKey, d time.Duration) {
	if key == nil {
		return
	}
	a.BanFingerprint(newAuthKey(key), d)
}

// BanFingerprint will set a public key fingerprint as banned.
func (a *Auth) BanFingerprint(authkey string, d time.Duration) {
	authItem := set.StringItem(authkey)
	if d != 0 {
		a.banned.Add(set.Expire(authItem, d))
	} else {
		a.banned.Add(authItem)
	}
	logger.Debugf("Added to banned: %s (for %s)", authItem.Key(), d)
}

// Ban will set an IP address as banned.
func (a *Auth) BanAddr(addr net.Addr, d time.Duration) {
	authItem := set.StringItem(addr.String())
	if d != 0 {
		a.bannedAddr.Add(set.Expire(authItem, d))
	} else {
		a.bannedAddr.Add(authItem)
	}
	logger.Debugf("Added to bannedAddr: %s (for %s)", authItem.Key(), d)
}
